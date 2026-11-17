//! The transport under the session: connect, and the error shape both
//! directions share.
//!
//! # Two kinds of target, one `Stream`
//!
//! `Target::Local` is the unix socket or named pipe `endpoint.rs` resolves —
//! unchanged from before this module learned a second kind. `Target::Remote`
//! is a TCP connection to a workspace's daemon elsewhere, speaking the same
//! newline-delimited JSON-RPC framing. `Stream` is an enum over the two so
//! `session.rs` never branches on which workspace it's holding open — it
//! calls `Read`/`Write`/`try_clone` exactly as it always has.
//!
//! **Nothing answers a `Target::Remote` dial today.** The daemon-side network
//! listener this would reach is deliberately deferred — see
//! `docs/aeon/S41-workspaces/design.md`. This client is written against that
//! note's assumed wire contract (same framing, full method parity) so it is
//! ready the moment that listener exists, and it is proven here against a
//! fake TCP JSON-RPC server in `tests/`, not against a real remote daemon.

use std::io::{Read, Write};
use std::net::TcpStream;
use std::path::{Path, PathBuf};
use std::time::Duration;

use serde::{Deserialize, Serialize};

/// How long the connection waits for things, and how it decides it is still
/// alive.
///
/// # Why the read timeout is a tick and not a deadline
///
/// A long-lived connection is idle almost all of the time: a loop parked at a
/// gate emits nothing for hours and is **perfectly current** while it does
/// (S9's T3, D6). The socket still needs a read timeout, because a thread
/// parked in `read` with none can never be woken — but that timeout is a
/// **wake-up interval**, and the error it produces is `EAGAIN`
/// (`WouldBlock`, "Resource temporarily unavailable (os error 11)"), which
/// means *nothing arrived*. It does not mean *the daemon is gone*.
///
/// One value used to serve both jobs, at 600s, and the reader thread treated
/// its error as fatal. The result was the defect this type exists to prevent:
/// an untouched window declared itself disconnected ten minutes after it last
/// spoke, quoting an OS error nobody could act on.
///
/// # Why a probe, when peer death needs no timer
///
/// On a unix socket the kernel reports EOF the instant the peer closes, so a
/// **stopped** daemon is seen immediately and for free — `session.rs` reads it
/// as `Ok(0)`. What no timer can catch is a daemon that is alive but wedged:
/// the socket stays open and nothing ever arrives. The probe fields answer that,
/// and they answer it by **asking the daemon a question**, not by counting
/// silence. That is the distinction S9's T3 insists on: a liveness probe
/// confirms the socket; it is not a way of reading state, which I4 forbids.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Timing {
    /// How often a parked reader wakes to look at the clock.
    ///
    /// Cost is one syscall a second while idle, which is why it can be short.
    /// Short matters for the opposite reason too: it bounds how long a
    /// connection takes to notice it has been retired.
    pub read_tick: Duration,
    /// Bound on a write, so a socket nobody is draining cannot wedge the
    /// calling thread forever. Generous because a streamed `ask` shares it.
    pub write_timeout: Duration,
    /// Silence beyond this earns a `ping`.
    pub probe_after: Duration,
    /// A `ping` unanswered for this long is one strike.
    pub probe_timeout: Duration,
    /// Strikes before the connection is called dead rather than merely quiet.
    ///
    /// Two, not one: a single unanswered probe against a daemon in a GC pause
    /// or a `SIGSTOP` under a debugger would cry wolf, and a warning people
    /// learn to ignore is worth nothing when it is finally true.
    pub probe_strikes: u32,
}

impl Default for Timing {
    fn default() -> Self {
        Timing {
            read_tick: Duration::from_secs(1),
            // The old single value, kept for the one job it was right for.
            write_timeout: Duration::from_secs(600),
            probe_after: Duration::from_secs(20),
            probe_timeout: Duration::from_secs(10),
            probe_strikes: 2,
        }
    }
}

impl Timing {
    /// How long an alive-but-wedged daemon takes to be called dead.
    ///
    /// Named so a test can assert against the policy instead of restating it,
    /// and so the worst case is a number somebody read rather than inferred.
    pub fn dead_after(&self) -> Duration {
        self.probe_after + self.probe_timeout * self.probe_strikes.max(1)
    }
}

/// The three parts and the fatal marker (T10, C9).
///
/// Optional on an error because a **transport** condition carries none:
/// the three-part shape is Aeon's, and inventing a `means` for a closed socket
/// would be fiction.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct ErrorData {
    pub what: String,
    #[serde(default)]
    pub means: String,
    #[serde(default)]
    pub todo: String,
    #[serde(default)]
    pub fatal: bool,
}

/// RpcError is a failure a call can report, from either side of the wire.
///
/// # One type, both directions, and why
///
/// It **deserialises** from the daemon's JSON-RPC error object and
/// **serialises** to the web layer. A Tauri command's error is a string, so
/// `Display` renders this as JSON — which is how the three parts survive a
/// boundary that would otherwise flatten them to a sentence.
///
/// `kind` separates a daemon refusal, a timeout, and a broken connection even
/// when the daemon's JSON-RPC error carries no structured `data`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct RpcError {
    #[serde(default = "rpc_kind")]
    pub kind: ErrorKind,
    pub code: i64,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub data: Option<ErrorData>,
}

#[derive(Serialize, Deserialize, Debug, Clone, Copy, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum ErrorKind {
    Transport,
    Timeout,
    Rpc,
}

fn rpc_kind() -> ErrorKind {
    ErrorKind::Rpc
}

/// A code outside JSON-RPC's range and Aeon's, for a failure that never reached
/// the wire. **Positive**, so it cannot be mistaken for either.
pub const CODE_TRANSPORT: i64 = 1;

impl RpcError {
    pub fn transport(message: impl Into<String>) -> Self {
        RpcError {
            kind: ErrorKind::Transport,
            code: CODE_TRANSPORT,
            message: message.into(),
            data: None,
        }
    }

    pub fn timeout(after: Duration) -> Self {
        RpcError {
            kind: ErrorKind::Timeout,
            code: CODE_TRANSPORT,
            message: format!("the daemon did not answer within {}s", after.as_secs()),
            data: None,
        }
    }

    /// A connection that has gone away, in words a person can act on.
    ///
    /// # Why this exists rather than passing the OS error through
    ///
    /// A failed write to a dead socket is `Broken pipe (os error 32)`, and a
    /// failed read is often nothing at all. Neither tells a reader that **the
    /// daemon is no longer there**, which is the only fact that matters and the
    /// only one they can act on. The OS text is kept, after the sentence, for
    /// whoever is debugging rather than using.
    pub fn connection_lost(detail: impl std::fmt::Display) -> Self {
        RpcError::transport(format!(
            "the connection to the daemon has closed ({detail}). \
             It may have stopped, or been restarted — reconnect and try again"
        ))
    }

    /// Build from a JSON-RPC error object, keeping the three parts where the
    /// daemon sent them.
    pub fn from_wire(err: &serde_json::Value) -> Self {
        serde_json::from_value(err.clone()).unwrap_or_else(|_| {
            RpcError::transport(format!(
                "the daemon returned an error this client could not read: {err}"
            ))
        })
    }
}

impl std::fmt::Display for RpcError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match serde_json::to_string(self) {
            Ok(json) => write!(f, "{json}"),
            Err(_) => write!(f, "{}", self.message),
        }
    }
}

impl std::error::Error for RpcError {}

/// The platform's local-endpoint connection type, named once so the enum
/// below does not branch per platform either.
#[cfg(not(windows))]
type LocalStream = std::os::unix::net::UnixStream;

#[cfg(windows)]
type LocalStream = std::fs::File;

/// Where a connection goes: the local endpoint `endpoint.rs` resolves, or a
/// remote workspace's host and port. `Display` is the label a `Negotiated`
/// and an error message show — a path on one side, `host:port` on the other.
pub enum Target {
    Local(PathBuf),
    Remote { host: String, port: u16 },
}

impl std::fmt::Display for Target {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Target::Local(path) => write!(f, "{}", path.display()),
            Target::Remote { host, port } => write!(f, "{host}:{port}"),
        }
    }
}

/// Stream is either kind of connection, named once so the layer above
/// (`session.rs`) never branches on which workspace it is holding open — it
/// reads, writes, and clones exactly as it did when there was only one kind.
#[derive(Debug)]
pub enum Stream {
    Local(LocalStream),
    Remote(TcpStream),
}

impl Read for Stream {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        match self {
            Stream::Local(s) => s.read(buf),
            Stream::Remote(s) => s.read(buf),
        }
    }
}

impl Write for Stream {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        match self {
            Stream::Local(s) => s.write(buf),
            Stream::Remote(s) => s.write(buf),
        }
    }
    fn flush(&mut self) -> std::io::Result<()> {
        match self {
            Stream::Local(s) => s.flush(),
            Stream::Remote(s) => s.flush(),
        }
    }
}

impl Stream {
    /// Dup the connection so a reader thread and the writer it shares with
    /// calls hold the same underlying socket object — `session.rs` relies on
    /// this to let a half-close on one handle end the other's parked `read`.
    pub fn try_clone(&self) -> std::io::Result<Stream> {
        match self {
            Stream::Local(s) => local_try_clone(s).map(Stream::Local),
            Stream::Remote(s) => s.try_clone().map(Stream::Remote),
        }
    }
}

#[cfg(not(windows))]
fn local_try_clone(s: &LocalStream) -> std::io::Result<LocalStream> {
    s.try_clone()
}

#[cfg(windows)]
fn local_try_clone(s: &LocalStream) -> std::io::Result<LocalStream> {
    s.try_clone()
}

/// Open a connection to a target with the production timings.
pub fn connect(target: &Target) -> Result<Stream, String> {
    connect_with(target, &Timing::default())
}

/// Open a connection with explicit timings.
///
/// Tests use this to make "idle" arrive in milliseconds. That is not a test
/// concession for its own sake: the defect this guards was invisible to every
/// test in the suite precisely because waiting ten minutes was not on offer.
pub fn connect_with(target: &Target, timing: &Timing) -> Result<Stream, String> {
    match target {
        Target::Local(endpoint) => connect_local(endpoint, timing).map(Stream::Local),
        Target::Remote { host, port } => connect_remote(host, *port, timing).map(Stream::Remote),
    }
}

#[cfg(not(windows))]
fn connect_local(endpoint: &Path, timing: &Timing) -> Result<LocalStream, String> {
    use std::os::unix::net::UnixStream;
    let stream = UnixStream::connect(endpoint).map_err(|e| format!("connect: {e}"))?;
    stream
        .set_read_timeout(Some(timing.read_tick))
        .map_err(|e| format!("set read timeout: {e}"))?;
    stream
        .set_write_timeout(Some(timing.write_timeout))
        .map_err(|e| format!("set write timeout: {e}"))?;
    Ok(stream)
}

/// On Windows the endpoint is a named pipe, which behaves as a file.
///
/// **Note what this cannot do yet.** The framework's Windows named-pipe
/// transport is `ErrPlatformNotImplemented` — deliberately deferred upstream —
/// so there is no endpoint to open on Windows at all. This compiles and will
/// work when the daemon side exists; today it fails with "not found", which is
/// the truth.
#[cfg(windows)]
fn connect_local(endpoint: &Path, _timing: &Timing) -> Result<LocalStream, String> {
    use std::fs::OpenOptions;
    OpenOptions::new()
        .read(true)
        .write(true)
        .open(endpoint)
        .map_err(|e| format!("open pipe: {e}"))
}

/// Dial a remote workspace's daemon. See this module's own doc comment: there
/// is nothing on the other end of this today, by design — this function is
/// exercised in `tests/` against a fake TCP JSON-RPC server standing in for
/// the listener `docs/aeon/S41-workspaces/design.md` defers.
fn connect_remote(host: &str, port: u16, timing: &Timing) -> Result<TcpStream, String> {
    let stream = TcpStream::connect((host, port)).map_err(|e| format!("connect: {e}"))?;
    stream
        .set_read_timeout(Some(timing.read_tick))
        .map_err(|e| format!("set read timeout: {e}"))?;
    stream
        .set_write_timeout(Some(timing.write_timeout))
        .map_err(|e| format!("set write timeout: {e}"))?;
    // Nagle's algorithm batches small writes to wait for more — exactly wrong
    // for a protocol that is itself all small, latency-sensitive frames.
    stream
        .set_nodelay(true)
        .map_err(|e| format!("set nodelay: {e}"))?;
    Ok(stream)
}

/// Wake a reader parked on the other half of this connection.
///
/// `try_clone` dups the descriptor, so both halves name the **same socket
/// object**: shutting the read side down over here is what lets `Session::drop`
/// retire its reader thread instead of abandoning it. Verified on the unix and
/// TCP paths: a parked `read` comes back `Ok(0)` — the very EOF a dead peer
/// produces — so the reader leaves through its ordinary path, running its own
/// cleanup, rather than being left parked until the next tick.
pub fn shutdown_read(stream: &Stream) -> std::io::Result<()> {
    match stream {
        Stream::Local(s) => local_shutdown_read(s),
        Stream::Remote(s) => s.shutdown(std::net::Shutdown::Read),
    }
}

#[cfg(not(windows))]
fn local_shutdown_read(s: &LocalStream) -> std::io::Result<()> {
    s.shutdown(std::net::Shutdown::Read)
}

/// A named pipe has no half-close; dropping the handle is what there is, and
/// the reader ends when its own handle sees EOF.
#[cfg(windows)]
fn local_shutdown_read(_s: &LocalStream) -> std::io::Result<()> {
    Ok(())
}

/// Assert at compile time that Stream is readable and writable, so a platform
/// branch returning the wrong type fails here rather than in a caller.
fn _stream_is_read_write<T: Read + Write>() {}
const _: () = {
    let _ = _stream_is_read_write::<Stream>;
};

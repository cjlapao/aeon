//! One long-lived connection to the daemon, shared by the whole window.
//!
//! # Why a session rather than a connection per call
//!
//! **Subscriptions are per-connection** (S2's T5). A UI that opened a fresh
//! socket per request could never receive a stream: it would subscribe on one
//! connection and `ask` on another, and the daemon would correctly refuse the
//! subscription id as belonging to somebody else.
//!
//! So one connection, held open, carrying both directions — which means this
//! file has to do the one thing a request/response client can avoid:
//! **correlate**. A reader thread demultiplexes by the presence of an `id`,
//! which is JSON-RPC's own rule: a response has one, a notification does not.
//!
//! # What reaches the webview
//!
//! Notifications are emitted as the Tauri event `aeon://event`, carrying the
//! protocol's event params verbatim. The two terminal notifications
//! (`terminal.output`, `terminal.exit`) are emitted as `aeon://terminal`,
//! params verbatim, and only those methods — an unknown one is logged and
//! dropped (S31 T8). The web layer subscribes to these and never sees a
//! socket — `ui/test/no-socket-in-web-layer.test.ts` asserts it stays that
//! way.
//!
//! # What keeps it honest
//!
//! Two separate questions, answered by two separate mechanisms, because
//! conflating them is the bug this file shipped with:
//!
//! - **Did the daemon go away?** The kernel answers this one for free: a unix
//!   socket reports EOF the instant its peer closes, so the reader sees `Ok(0)`
//!   and needs no timer.
//! - **Is it merely quiet?** A loop parked at a gate emits nothing for hours
//!   and is perfectly current (S9's T3, D6). Nothing arriving therefore proves
//!   nothing, so the reader asks instead of waiting: after `probe_after` of
//!   silence it sends a `ping`, and only unanswered pings retire the connection.
//!
//! The socket's read timeout is what lets the reader wake up to ask at all. It
//! is a **tick**, and the `EAGAIN` it produces means "nothing yet" — never
//! "gone".

use std::collections::HashMap;
use std::io::{self, Read, Write};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::mpsc::{channel, Receiver, Sender};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};
use serde_json::Value;
use tauri::{AppHandle, Emitter};

use crate::rpc;

/// The Tauri event name notifications arrive on.
pub const EVENT_CHANNEL: &str = "aeon://event";
/// The Tauri event name a lost connection arrives on, so the UI can go honest
/// rather than appearing to work.
pub const CLOSED_CHANNEL: &str = "aeon://closed";
/// The Tauri event name terminal notifications arrive on (S31 T8, D10).
///
/// A separate channel from `aeon://event` on purpose: terminal output is not
/// an event (LW7 — it never enters the `EventLog`), it carries no `kind`, and
/// the web layer listens to it only where a terminal is open.
pub const TERMINAL_CHANNEL: &str = "aeon://terminal";

/// How long a single call waits. Generous, because `ask` in buffered mode is a
/// model call — but bounded, because a UI that hangs forever is worse than one
/// that says it gave up.
const CALL_TIMEOUT: Duration = Duration::from_secs(300);

/// The ids the shell spends on its own liveness pings.
///
/// `call_raw` hands out ids from 1 upward, so the two ranges cannot collide, and
/// the reader recognises a pong by its range rather than by remembering every
/// id it ever sent. A ping is **not** a call: it never enters `pending`, and a
/// UI can neither send one nor block on one.
const PROBE_ID_BASE: u64 = u64::MAX - 1_000;

/// What a call is waiting for, and the map the reader hands it through.
///
/// Named because the compiler objected to the type's depth, and a six-level
/// type repeated at four call sites is how two of them quietly stop being the
/// same type — the kind of change that only shows up as a borrow error in code
/// nobody has written yet.
type Response = Result<Value, rpc::RpcError>;
type PendingMap = HashMap<u64, Sender<Response>>;
type Pending = Arc<Mutex<PendingMap>>;

/// The protocol version this shell needs.
pub const PROTOCOL_VERSION: u32 = 2;
/// What this client calls itself in `initialize`, and what another client sees
/// in `client.connected`.
pub const CLIENT_NAME: &str = "aeon-app";

#[derive(Serialize, Deserialize, Debug, Clone, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct Negotiated {
    /// The DAEMON's build, not this app's.
    pub daemon_version: String,
    /// The NEGOTIATED protocol version for this connection.
    pub protocol_version: u32,
    pub endpoint: String,
}

#[derive(Deserialize)]
struct InitializeResult {
    #[serde(rename = "protocolVersion")]
    protocol_version: u32,
    #[serde(rename = "daemonVersion")]
    #[allow(dead_code)]
    daemon_version: String,
}

/// Where notifications go.
///
/// # Why this is a trait and not just an `AppHandle`
///
/// `AppHandle` cannot be constructed outside a running app, so with it baked
/// in, **the entire Go → Rust → webview path had no test** — and the first bug
/// in it was exactly there: the UI sat at "Streaming…" and nothing arrived,
/// with no way to tell whether the daemon had not sent, the reader had not
/// read, or the emit had not landed.
///
/// With a sink, a test drives a real daemon through a real reader into a
/// recording sink, leaving only Tauri's own emit untested — and that is one
/// hop rather than three.
pub trait EventSink: Send + Sync + 'static {
    /// One `event` notification's params, verbatim.
    fn event(&self, params: Value);
    /// One `terminal.output` or `terminal.exit` notification's params,
    /// verbatim (S31 T8, D10).
    ///
    /// Required rather than defaulted to a no-op: a sink that has not decided
    /// what it does with terminal output should not compile, for the same
    /// reason `AppSink` reports a failed emit instead of discarding it — a
    /// stream that silently goes nowhere is indistinguishable from one that
    /// was never sent.
    fn terminal(&self, params: Value);
    /// The connection ended, with a reason.
    fn closed(&self, reason: String);
}

/// The production sink.
///
/// **It reports a failed emit rather than discarding it.** The first version
/// wrote `let _ = app.emit(...)`, which is how an event that never reaches the
/// webview looks identical to one that was never sent.
pub struct AppSink(pub AppHandle);

impl EventSink for AppSink {
    fn event(&self, params: Value) {
        if let Err(e) = self.0.emit(EVENT_CHANNEL, params) {
            // stderr, because a UI that silently receives nothing is the worst
            // possible failure mode for this path.
            eprintln!("aeon: could not emit {EVENT_CHANNEL} to the webview: {e}");
        }
        // S58 T7: the tray's count is stale the moment the world moves — and
        // this sink is the shell's ONE event path, so the tray refresh rides
        // it as a coalesced poke instead of a second subscription. The poke
        // is answered on the poller thread: this thread is the session's
        // reader, and a blocking call made here would wait forever for a
        // response only this thread could read.
        crate::background_tray::poke_tray(&self.0);
    }

    fn terminal(&self, params: Value) {
        if let Err(e) = self.0.emit(TERMINAL_CHANNEL, params) {
            // The same rule as `event`, for the same reason: a terminal whose
            // keystrokes echo nowhere reads as a hung shell, and stderr is the
            // one place that outlives a wedged webview. Report, never discard.
            eprintln!("aeon: could not emit {TERMINAL_CHANNEL} to the webview: {e}");
        }
    }

    fn closed(&self, reason: String) {
        if let Err(e) = self.0.emit(CLOSED_CHANNEL, reason) {
            eprintln!("aeon: could not emit {CLOSED_CHANNEL} to the webview: {e}");
        }
    }
}

/// What the reader thread and the connection's owner agree on, from two
/// threads at once.
///
/// Three separate atoms rather than one state enum, because they answer three
/// independent questions asked by three different people: *should this reader
/// still be running*, *did we decide to close it*, and *what did it die of*.
struct ConnState {
    /// Set while the connection is wanted, cleared by `Session::drop`.
    ///
    /// The reader tests it on every idle tick, so a retired connection ends
    /// within one `read_tick` even with nothing arriving — which is exactly the
    /// case that used to leak: replacing a session left its reader parked on a
    /// duped descriptor, holding the daemon's side of the connection open.
    run: AtomicBool,
    /// Set when **this side** closed the connection.
    ///
    /// The distinction the window needs: a connection the app retired is not a
    /// connection that was lost. Emitting `aeon://closed` for the former is how
    /// a routine reconnect reads as an outage — and how a second, zombie
    /// connection's death could mark a perfectly healthy window "not connected".
    retired: AtomicBool,
    /// Why the reader ended, once it has. This is the UI's "not connected"
    /// reason, and the reason is the part a person can act on.
    closed: Mutex<Option<String>>,
}

impl ConnState {
    fn new() -> Self {
        ConnState {
            run: AtomicBool::new(true),
            retired: AtomicBool::new(false),
            closed: Mutex::new(None),
        }
    }

    fn reason(&self) -> Option<String> {
        self.closed.lock().unwrap().clone()
    }
}

/// Session is one connection, its reader thread, and the calls in flight on it.
pub struct Session {
    /// Shared with the reader, which writes its own liveness pings on it.
    ///
    /// One mutex and one writer, so a ping can never be interleaved into the
    /// middle of a call's request: the frame boundary is taken inside the lock.
    writer: Arc<Mutex<rpc::Stream>>,
    pending: Pending,
    next_id: Arc<AtomicU64>,
    negotiated: Negotiated,
    state: Arc<ConnState>,
}

/// The calling half of a session: cheap to clone, `'static`, and `Send`, so
/// it can be moved to a worker thread and left there for as long as the
/// daemon takes to answer.
///
/// # Why the shell needs a handle rather than the `Session`
///
/// A call blocks: `call_raw` parks its thread until the response arrives or
/// `CALL_TIMEOUT` says it never will. On the webview's own thread that is not
/// a slow page, it is a frozen window — nothing repaints, no menu responds,
/// and the page the user clicked is indistinguishable from a crash
/// (S72's walk found the Configuration pages doing exactly this, because a
/// Tauri command that is not `async` runs on the main thread). The fix is to
/// run the wait on a blocking worker, and a worker needs an owned, `'static`
/// thing to wait with: `&Session` borrows the app's state, and app state
/// cannot cross into a spawned task. This can — every field is an `Arc`, and
/// the request-id counter is shared rather than per-handle precisely so two
/// handles can never mint the same id.
///
/// Dropping the `Session` still retires the connection (`Drop` below); a
/// handle that outlives it simply finds `state` closed and reports it, which
/// is what a call on a session the user disconnected should do.
#[derive(Clone)]
pub struct SessionHandle {
    writer: Arc<Mutex<rpc::Stream>>,
    pending: Pending,
    next_id: Arc<AtomicU64>,
    state: Arc<ConnState>,
}

impl Session {
    /// Open a connection, handshake, and start the reader.
    pub fn connect(
        target: rpc::Target,
        client_version: &str,
        sink: impl EventSink,
    ) -> Result<Self, String> {
        Self::connect_with(target, client_version, sink, &rpc::Timing::default())
    }

    /// Connect with explicit timings.
    ///
    /// Tests use this to make ten minutes of idle arrive in milliseconds —
    /// which is not a convenience for its own sake. The defect `Timing` exists
    /// to prevent survived every test in this suite precisely because nobody
    /// could afford to wait ten minutes in one.
    pub fn connect_with(
        target: rpc::Target,
        client_version: &str,
        sink: impl EventSink,
        timing: &rpc::Timing,
    ) -> Result<Self, String> {
        let stream = rpc::connect_with(&target, timing)
            .map_err(|e| format!("{e} (tried {target})"))?;
        let reader_stream = stream
            .try_clone()
            .map_err(|e| format!("could not clone the connection: {e}"))?;

        let pending: Arc<Mutex<HashMap<u64, Sender<Result<Value, rpc::RpcError>>>>> =
            Arc::new(Mutex::new(HashMap::new()));
        let state = Arc::new(ConnState::new());
        let writer = Arc::new(Mutex::new(stream));
        let next_id = Arc::new(AtomicU64::new(0));

        let session = Session {
            writer: Arc::clone(&writer),
            pending: Arc::clone(&pending),
            next_id: Arc::clone(&next_id),
            negotiated: Negotiated {
                daemon_version: String::new(),
                protocol_version: 0,
                endpoint: target.to_string(),
            },
            state: Arc::clone(&state),
        };

        spawn_reader(
            reader_stream,
            pending,
            Arc::clone(&state),
            writer,
            Box::new(sink),
            *timing,
        );

        // `initialize` first, always (T7) — before this function returns, so no
        // caller can send anything ahead of it.
        let mut session = session;
        let init_value = session
            .call_raw(
                "initialize",
                serde_json::json!({
                    "clientName": CLIENT_NAME,
                    "clientVersion": client_version,
                    "protocolVersion": PROTOCOL_VERSION,
                }),
            )
            .map_err(|e| e.to_string())?;
        let result: InitializeResult = serde_json::from_value(init_value)
            .map_err(|e| format!("decode initialize result: {e}"))?;

        // The negotiated version comes from the WIRE. Reporting the constant
        // would render identically on a working machine and prove nothing.
        let version_value = session
            .call_raw("version", Value::Null)
            .map_err(|e| e.to_string())?;
        let version: VersionResult = serde_json::from_value(version_value)
            .map_err(|e| format!("decode version result: {e}"))?;

        if version.protocol_version != result.protocol_version {
            return Err(format!(
                "the daemon negotiated protocol {} at initialize but reports {} from version; \
                 one of them is not reading the connection's state",
                result.protocol_version, version.protocol_version
            ));
        }

        session.negotiated.daemon_version = version.daemon_version;
        session.negotiated.protocol_version = version.protocol_version;
        Ok(session)
    }

    /// What the handshake negotiated: daemon version, protocol, endpoint.
    pub fn negotiated(&self) -> Negotiated {
        self.negotiated.clone()
    }

    /// A handle to this connection, cheap enough to hand to a worker thread.
    pub fn handle(&self) -> SessionHandle {
        SessionHandle {
            writer: Arc::clone(&self.writer),
            pending: Arc::clone(&self.pending),
            next_id: Arc::clone(&self.next_id),
            state: Arc::clone(&self.state),
        }
    }

    /// Call a method and wait for its response.
    ///
    /// The shipped shell never calls this — it holds the slot, takes a
    /// [`SessionHandle`] out of it, and calls through the handle, which is the
    /// type that may be held across a wait without a lock on the slot. This
    /// stays because the test bench holds a whole `Session` and speaks through
    /// it (`liveness.rs`, `live_stream.rs` and `remote_transport.rs` each
    /// `#[path]`-include this file and call it by name); an `allow` states that
    /// arrangement instead of leaving a dead-code warning to imply it.
    #[allow(dead_code)]
    pub fn call(&self, method: &str, params: Value) -> Result<Value, String> {
        // `initialize` is this file's to send, once, in `connect`. A second one
        // would be refused by the daemon anyway (-31003), but refusing here
        // names the reason instead of surfacing a protocol error to a UI.
        if method == "initialize" {
            return Err("the session performs `initialize` itself, once, when it connects".into());
        }
        self.handle().call_raw(method, params).map_err(|e| e.to_string())
    }

    fn call_raw(&self, method: &str, params: Value) -> Result<Value, rpc::RpcError> {
        self.handle().call_raw(method, params)
    }
}

impl SessionHandle {
    /// Call a method on the shared connection and wait for its response.
    ///
    /// This parks the calling thread until the daemon answers. Call it from a
    /// blocking worker, never from the window's thread (see the type's doc).
    pub fn call(&self, method: &str, params: Value) -> Result<Value, String> {
        if method == "initialize" {
            return Err("the session performs `initialize` itself, once, when it connects".into());
        }
        self.call_raw(method, params).map_err(|e| e.to_string())
    }

    /// The tray's count line, answered live (S58 T7): the SAME
    /// `inbox.summary` the person's `aeon inbox summary` speaks, read for its
    /// `total`. Any failure is `None` — the tray then says "Inbox
    /// unavailable", because a count this shell cannot read must never
    /// render as the reassuring zero (SV9, the same rule as the CLI's
    /// source census).
    ///
    /// It lives on the HANDLE, and not on the `Session` it came from, because
    /// of who calls it: the tray poller reads this every 60 s, and the answer
    /// can take as long as `CALL_TIMEOUT` on a daemon that has stopped
    /// draining. Asking for it through the `Session` meant asking while holding
    /// the session slot's mutex, so a wedged daemon turned that mutex into a
    /// barrier every `connect`, `disconnect` and `rpc_call` then queued behind —
    /// the same freeze, wearing a worker thread instead of the window. A handle
    /// is what a caller holds while it waits, with the lock already put down.
    pub fn inbox_count(&self) -> Option<u32> {
        match self.call("inbox.summary", serde_json::json!({})) {
            Ok(view) => view
                .get("total")
                .and_then(Value::as_u64)
                .and_then(|n| u32::try_from(n).ok()),
            Err(_) => None,
        }
    }

    /// The one request/response body, shared by every handle of one
    /// connection.
    pub(crate) fn call_raw(&self, method: &str, params: Value) -> Result<Value, rpc::RpcError> {
        if let Some(reason) = self.state.reason() {
            return Err(rpc::RpcError::transport(format!(
                "the connection to the daemon has closed: {reason}"
            )));
        }

        let id = self.next_id.fetch_add(1, Ordering::SeqCst) + 1;
        let (tx, rx): (
            Sender<Result<Value, rpc::RpcError>>,
            Receiver<Result<Value, rpc::RpcError>>,
        ) = channel();
        self.pending.lock().unwrap().insert(id, tx);

        let mut request = serde_json::json!({"jsonrpc": "2.0", "id": id, "method": method});
        if !params.is_null() {
            request["params"] = params;
        }
        let line = format!("{request}\n");

        let write_result = {
            let mut writer = self.writer.lock().unwrap();
            writer
                .write_all(line.as_bytes())
                .and_then(|_| writer.flush())
        };
        if let Err(e) = write_result {
            self.pending.lock().unwrap().remove(&id);
            // A write to a dead socket is `Broken pipe`, which tells a reader
            // nothing they can act on.
            return Err(rpc::RpcError::connection_lost(e));
        }

        match rx.recv_timeout(CALL_TIMEOUT) {
            Ok(result) => result,
            Err(_) => {
                self.pending.lock().unwrap().remove(&id);
                Err(rpc::RpcError::timeout(CALL_TIMEOUT))
            }
        }
    }
}

/// Retire the connection, and mean it.
///
/// # Why this needs a `Drop` at all
///
/// The reader thread owns a `try_clone`d descriptor, so dropping the `Session`
/// closed *nothing*: the daemon still saw a client, and the thread sat parked in
/// `read` holding it. That is where the second `client connected` line in the
/// daemon's log came from, and why a connection nobody owned any more could
/// later report itself lost — `aeon://closed` carries no session identity, so a
/// zombie's death marked a live window disconnected.
///
/// So: say we closed it, stop the reader, and wake it. `shutdown_read` on a dup
/// affects the shared socket, which turns the parked `read` into the same EOF a
/// dead peer gives — the reader runs its own exit path instead of being
/// abandoned in it.
impl Drop for Session {
    fn drop(&mut self) {
        self.state.retired.store(true, Ordering::SeqCst);
        self.state.run.store(false, Ordering::SeqCst);
        // `try_lock`, not `lock`: a call may be sitting in a 600s write because
        // the daemon stopped draining. Blocking a drop on that turns a leaked
        // thread into a hung window. The `run` flag above already guarantees the
        // reader stops by its next tick; this only makes it immediate.
        if let Ok(writer) = self.writer.try_lock() {
            let _ = rpc::shutdown_read(&writer);
        }
    }
}

#[derive(Deserialize)]
struct VersionResult {
    #[serde(rename = "daemonVersion")]
    daemon_version: String,
    #[serde(rename = "protocolVersion")]
    protocol_version: u32,
}

/// spawn_reader demultiplexes everything the daemon sends, and keeps the
/// connection's own liveness.
///
/// One routing rule, and it is JSON-RPC's rather than Aeon's: **a response has
/// an `id` and a notification does not.**
///
/// # Why the loop is written by hand rather than with `lines()`
///
/// `BufReader::lines()` cannot express the one distinction this connection
/// lives or dies by. On a read timeout it hands back `Err(WouldBlock)` — and
/// the loop's only move was to `break`, so **ten quiet minutes read as a fatal
/// error on a socket that was working perfectly**. That is the defect this
/// function was rewritten for, and `ui/src-tauri/tests/liveness.rs` is the
/// test whose absence let it ship.
///
/// The hand-written loop also keeps bytes it has already taken in `buf`, so a
/// tick landing mid-frame loses nothing: the next `read` continues the frame.
fn spawn_reader(
    mut stream: rpc::Stream,
    pending: Pending,
    state: Arc<ConnState>,
    writer: Arc<Mutex<rpc::Stream>>,
    sink: Box<dyn EventSink>,
    timing: rpc::Timing,
) {
    thread::spawn(move || {
        // Set by whichever `break` ends the loop, and deliberately without an
        // initialiser: the loop may only be exited **by giving a reason**, and a
        // default nobody can ever read is a claim that it might not.
        let end_reason: String;
        let mut buf: Vec<u8> = Vec::with_capacity(8 * 1024);
        let mut chunk = [0u8; 8 * 1024];

        // The liveness clock, measured in *inbound* bytes. A call that went out
        // and is being thought about is not an answer to anything.
        let mut last_inbound = Instant::now();
        let mut probe_id: Option<u64> = None;
        let mut probe_sent = Instant::now();
        let mut strikes: u32 = 0;
        let mut next_probe_id = PROBE_ID_BASE;

        loop {
            if !state.run.load(Ordering::SeqCst) {
                end_reason = String::from("the app closed the connection");
                break;
            }
            match stream.read(&mut chunk) {
                Ok(0) => {
                    // EOF. A unix socket gives this the instant the peer is
                    // gone, which is why a *stopped* daemon needs no timer at
                    // all — see `rpc::Timing`.
                    end_reason = String::from("the daemon closed the connection");
                    break;
                }
                Ok(n) => {
                    last_inbound = Instant::now();
                    // Any byte proves the daemon is there, which is the only
                    // question the probe asks. An answer to anything — an event,
                    // another call's response — stands the ping down.
                    probe_id = None;
                    strikes = 0;
                    buf.extend_from_slice(&chunk[..n]);
                    while let Some(pos) = buf.iter().position(|&b| b == b'\n') {
                        let frame: Vec<u8> = buf.drain(..=pos).collect();
                        handle_frame(&frame, &pending, &sink);
                    }
                }
                // The tick, not the failure. Nothing arrived, which is what an
                // idle connection *does*; the window stays connected.
                Err(ref e) if e.kind() == io::ErrorKind::WouldBlock => {
                    let owe_ping = match probe_id {
                        None => last_inbound.elapsed() >= timing.probe_after,
                        Some(_) if probe_sent.elapsed() >= timing.probe_timeout => {
                            strikes += 1;
                            if strikes >= timing.probe_strikes.max(1) {
                                end_reason = probe_dead_reason(&timing, strikes);
                                break;
                            }
                            probe_id = None;
                            true
                        }
                        // Still inside its own allowance. Wait for that answer.
                        Some(_) => false,
                    };
                    if owe_ping {
                        let id = next_probe_id;
                        next_probe_id += 1;
                        if let Err(e) = send_ping(&writer, id) {
                            end_reason = format!(
                                "the liveness probe could not be written to the daemon: {e}"
                            );
                            break;
                        }
                        probe_id = Some(id);
                        probe_sent = Instant::now();
                    }
                }
                Err(ref e) if e.kind() == io::ErrorKind::Interrupted => {}
                Err(e) => {
                    // Anything else genuinely is the connection failing, and it
                    // keeps the OS's own words after the sentence. `os error 11`
                    // was never the problem; *treating it as one* was.
                    end_reason = format!("read from the daemon: {e}");
                    break;
                }
            }
        }

        let retired = state.retired.load(Ordering::SeqCst);
        if !retired {
            *state.closed.lock().unwrap() = Some(end_reason.clone());
        }

        // Fail every call still waiting either way, rather than letting each
        // wait out its own timeout against a socket nobody is reading.
        let mut waiting = pending.lock().unwrap();
        for (_, tx) in waiting.drain() {
            let reason = if retired {
                "this connection was closed by the app".to_string()
            } else {
                end_reason.clone()
            };
            let _ = tx.send(Err(rpc::RpcError::transport(reason)));
        }
        drop(waiting);

        // A connection the app retired itself is not an outage — and this emit
        // carries no session identity to distinguish them with. Emitting anyway
        // is how a discarded second connection used to mark a healthy window
        // "not connected" some minutes after anybody stopped using it.
        if !retired {
            sink.closed(end_reason);
        }
    });
}

/// One frame off the wire, dispatched.
///
/// Out of the loop so the loop's only jobs are reading, and deciding whether
/// the connection is still alive.
fn handle_frame(
    frame: &[u8],
    pending: &Mutex<HashMap<u64, Sender<Result<Value, rpc::RpcError>>>>,
    sink: &Box<dyn EventSink>,
) {
    let value: Value = match serde_json::from_slice(frame) {
        Ok(v) => v,
        // A frame this client cannot parse is the daemon's problem, not a
        // reason to tear down a working connection — the same courtesy the
        // daemon extends to a malformed frame (O6).
        Err(_) => return,
    };

    match value.get("id").and_then(|v| v.as_u64()) {
        Some(id) => {
            if id >= PROBE_ID_BASE {
                // The shell's own pong. It was never a call, so it belongs to
                // nobody in `pending`; its whole value is that it arrived.
                return;
            }
            let sender = pending.lock().unwrap().remove(&id);
            if let Some(tx) = sender {
                let _ = tx.send(response_to_result(&value));
            }
        }
        None => {
            // An explicit allow-list, and it stays one: an unknown
            // notification method is logged and dropped, never forwarded
            // blindly. That is a security property, not tidiness — a daemon
            // (or anything that spoofs its side of the socket) must not be
            // able to mint a new webview event by inventing a method name.
            match value.get("method").and_then(|m| m.as_str()) {
                Some("event") => {
                    if let Some(params) = value.get("params") {
                        // The protocol's event params, verbatim. The web layer
                        // renders them; nothing is reinterpreted here, so a new
                        // event kind needs no Rust change.
                        sink.event(params.clone());
                    }
                }
                // The two terminal notifications T7 publishes (S31 T8, D10):
                // params verbatim onto `aeon://terminal`, under the sink's
                // report-don't-discard rule. Unlike event kinds these are a
                // closed set — a third terminal method is a deliberate Rust
                // change, which is exactly what the allow-list is for.
                Some("terminal.output") | Some("terminal.exit") => {
                    if let Some(params) = value.get("params") {
                        sink.terminal(params.clone());
                    }
                }
                other => {
                    // Logged, and the name is the point: a dropped frame whose
                    // method nobody printed is indistinguishable from one the
                    // daemon never sent.
                    eprintln!(
                        "aeon: dropped notification for unforwarded method {:?}",
                        other.unwrap_or("<missing>")
                    );
                }
            }
        }
    }
}

/// Ask the daemon one question that costs it nothing.
///
/// `ping` answers `PingResult{}` straight out of the method table — no store,
/// no provider, no loop behind it — which is what makes it safe to ask on a
/// timer. It is a **liveness probe, not polling**: I4 forbids an interval that
/// re-reads state, and this reads none. It establishes that the socket is still
/// attached to a daemon willing to answer, which is the fact silence cannot
/// give either way.
fn send_ping(writer: &Arc<Mutex<rpc::Stream>>, id: u64) -> io::Result<()> {
    let frame = format!("{{\"jsonrpc\":\"2.0\",\"id\":{id},\"method\":\"ping\"}}\n");
    let mut guard = writer.lock().map_err(|_| {
        io::Error::other("the writer was poisoned by a panicking call")
    })?;
    guard.write_all(frame.as_bytes())?;
    guard.flush()
}

/// Why a wedged daemon is being reported as gone, in words that say what to do.
///
/// The three parts the shell insists on everywhere (T10, C9), applied to the
/// connection: what happened, what it means, what now. What it used to be was
/// `Resource temporarily unavailable (os error 11)`, which is none of the three.
fn probe_dead_reason(timing: &rpc::Timing, strikes: u32) -> String {
    format!(
        "the daemon stopped answering the liveness probe: {strikes} pings went unanswered, after \
         {}s of silence and {}s allowed per answer. The socket is still open, so the daemon is \
         wedged rather than stopped — restart it with `aeon serve`, or `make dev-daemon` from \
         the repository",
        timing.probe_after.as_secs(),
        timing.probe_timeout.as_secs()
    )
}

fn response_to_result(value: &Value) -> Result<Value, rpc::RpcError> {
    if let Some(err) = value.get("error") {
        return Err(rpc::RpcError::from_wire(err));
    }
    Ok(value.get("result").cloned().unwrap_or(Value::Null))
}

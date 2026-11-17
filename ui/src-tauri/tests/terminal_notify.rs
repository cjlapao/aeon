//! Terminal notifications reach the sink — through the production reader.
//!
//! # Why this test exists
//!
//! S31 T8 (D10) put `terminal.output` and `terminal.exit` onto the wire beside
//! `event`, and the rules that make that safe are exactly the rules no compiler
//! can check: params forwarded **verbatim**, only the two allow-listed methods
//! forwarded at all, an unknown method logged and dropped rather than
//! forwarded blindly. A dispatch written as `if method starts with anything`
//! would pass every compile and hand the webview whatever a peer invented.
//!
//! So this drives the real `Session` — the same connect, reader thread and
//! `handle_frame` the app runs — against a fake daemon that pushes frames the
//! real T7 daemon will push, and asserts what the sink is handed. `Recorder`
//! mirrors `tests/live_stream.rs`; the fake mirrors `tests/session.rs`.
//!
//! **What is *not* covered here:** Tauri's own `emit` (an `AppHandle` cannot be
//! built outside a running app — that hop is `AppSink`, one line per channel),
//! and the drop *log*: `handle_frame` prints it to stderr, which `cargo test`
//! surfaces under `--nocapture` but a test cannot assert in-process. What a
//! test can and does assert is the security half — an unknown method reaches
//! **neither** sink.

use std::io::{BufRead, BufReader, Write};
use std::os::unix::net::UnixListener;
use std::path::PathBuf;
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use serde_json::{json, Value};

#[path = "../src/endpoint.rs"]
mod endpoint;
#[path = "../src/rpc.rs"]
mod rpc;
#[path = "../src/session.rs"]
mod session;
#[path = "support/background_tray.rs"]
mod background_tray;

use session::{EventSink, Session};

/// A temp directory that removes itself. Same hand-rolled trade as
/// `tests/session.rs`: three dependencies for six lines is a poor purchase.
struct TempDir(PathBuf);

impl TempDir {
    fn new(tag: &str) -> Self {
        let mut p = std::env::temp_dir();
        p.push(format!(
            "aeon-shell-{tag}-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        std::fs::create_dir_all(&p).expect("create temp dir");
        TempDir(p)
    }
}

impl Drop for TempDir {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

/// A sink that records each channel separately, standing in for the webview.
///
/// Separate vectors are the whole test: a dispatch that crossed the streams —
/// a terminal frame onto the event sink, or an unknown method onto either —
/// fails an equality here rather than reading as a pass.
#[derive(Clone, Default)]
struct Recorder {
    events: Arc<Mutex<Vec<Value>>>,
    terminals: Arc<Mutex<Vec<Value>>>,
    closed: Arc<Mutex<Vec<String>>>,
}

impl Recorder {
    fn events(&self) -> Vec<Value> {
        self.events.lock().unwrap().clone()
    }

    fn terminals(&self) -> Vec<Value> {
        self.terminals.lock().unwrap().clone()
    }

    /// Wait until the terminal sink holds at least `n` frames. The reader is
    /// another thread and frames arrive after the handshake, so a test says
    /// "by the time the marker frame is in, everything queued before it has
    /// been dispatched" — the reader consumes frames in order.
    fn wait_terminal(&self, n: usize, timeout: Duration) -> bool {
        let deadline = Instant::now() + timeout;
        while Instant::now() < deadline {
            if self.terminals.lock().unwrap().len() >= n {
                return true;
            }
            std::thread::sleep(Duration::from_millis(20));
        }
        false
    }

    fn wait_events(&self, n: usize, timeout: Duration) -> bool {
        let deadline = Instant::now() + timeout;
        while Instant::now() < deadline {
            if self.events.lock().unwrap().len() >= n {
                return true;
            }
            std::thread::sleep(Duration::from_millis(20));
        }
        false
    }
}

impl EventSink for Recorder {
    fn event(&self, params: Value) {
        self.events.lock().unwrap().push(params);
    }

    fn terminal(&self, params: Value) {
        self.terminals.lock().unwrap().push(params);
    }

    fn closed(&self, reason: String) {
        self.closed.lock().unwrap().push(reason);
    }
}

/// The notifications the fake pushes after answering `version`, in order.
/// Shaped from the brief (§5): `terminal.output {terminalId, seq, data}`,
/// `terminal.exit {terminalId, exitCode?, signal?}`.
struct Script {
    pushes: Vec<Value>,
}

impl Default for Script {
    fn default() -> Self {
        Script { pushes: Vec::new() }
    }
}

struct FakeDaemon {
    path: PathBuf,
    _dir: TempDir,
    _methods: mpsc::Receiver<String>,
}

/// The same handshake fake as `tests/session.rs` — `initialize` first,
/// `version` from the wire — plus any queued notifications written right
/// after the `version` response, each as its own line and **without an `id`**,
/// which is how a client tells a notification from a response.
fn start(tag: &str, script: Script) -> FakeDaemon {
    let dir = TempDir::new(tag);
    let path = dir.0.join("rpc.sock");
    let listener = UnixListener::bind(&path).expect("bind the fake endpoint");
    let (tx, rx) = mpsc::channel();

    thread::spawn(move || {
        let Ok((stream, _)) = listener.accept() else {
            return;
        };
        let mut writer = stream.try_clone().expect("clone");
        let reader = BufReader::new(stream);

        for line in reader.lines() {
            let Ok(line) = line else { return };
            let Ok(req): Result<Value, _> = serde_json::from_str(&line) else {
                return;
            };
            let method = req["method"].as_str().unwrap_or("").to_string();
            let id = req["id"].clone();
            let _ = tx.send(method.clone());

            let result = match method.as_str() {
                "initialize" => json!({
                    "protocolVersion": 1,
                    "daemonVersion": "9.9.9-fake",
                    "capabilities": ["subscribe"],
                }),
                "version" => json!({
                    "daemonVersion": "9.9.9-fake",
                    "protocolVersion": 1,
                }),
                _ => json!({"ok": true, "method": method}),
            };

            let resp = json!({"jsonrpc": "2.0", "id": id, "result": result});
            if writeln!(writer, "{resp}").is_err() {
                return;
            }

            if method == "version" {
                for push in &script.pushes {
                    if writeln!(writer, "{push}").is_err() {
                        return;
                    }
                }
            }
        }
    });

    FakeDaemon { path, _dir: dir, _methods: rx }
}

fn notify(method: &str, params: Value) -> Value {
    json!({"jsonrpc": "2.0", "method": method, "params": params})
}

/// Connect the real `Session` against the fake and keep both alive for the
/// body of the assertions.
fn connected(tag: &str, script: Script) -> (FakeDaemon, Recorder, Session) {
    let fake = start(tag, script);
    let recorder = Recorder::default();
    let session = Session::connect(rpc::Target::Local(fake.path.clone()), "0.1.0-test", recorder.clone())
        .expect("connect and handshake");
    (fake, recorder, session)
}

/// The frame T7 publishes per coalesced read — `{terminalId, seq, data}` with
/// `data` base64 — must arrive at the terminal sink **identical**: not
/// decoded, not re-keyed, not wrapped. A shell that "helpfully" decoded the
/// base64 would leave the web layer's xterm.js with double-decoded garbage.
#[test]
fn a_terminal_output_frame_reaches_the_terminal_sink_verbatim() {
    let params = json!({"terminalId": "t-1", "seq": 7, "data": "ZWNobyBoaXQK"});
    let (_fake, recorder, _session) = connected(
        "term-out",
        Script { pushes: vec![notify("terminal.output", params.clone())] },
    );

    assert!(
        recorder.wait_terminal(1, Duration::from_secs(5)),
        "no terminal.output reached the sink within 5s; events seen: {:?} (a frame on the \
         wrong sink is the bug this file is for)",
        recorder.events()
    );
    let got = recorder.terminals();
    assert_eq!(
        got,
        vec![params.clone()],
        "the sink was handed something other than the daemon's params verbatim"
    );
    assert_eq!(recorder.events(), Vec::<Value>::new(), "terminal output leaked to the event sink");
}

/// Exit is the frame a UI needs most — it is what turns a live shell pane into
/// an honest `exit 3` line — and it obeys the same verbatim rule. `signal`
/// rides out when the daemon sends one.
#[test]
fn a_terminal_exit_frame_reaches_the_terminal_sink_verbatim() {
    let code = json!({"terminalId": "t-1", "exitCode": 3});
    let signal = json!({"terminalId": "t-2", "signal": "SIGTERM"});
    let (_fake, recorder, _session) = connected(
        "term-exit",
        Script {
            pushes: vec![
                notify("terminal.exit", code.clone()),
                notify("terminal.exit", signal.clone()),
            ],
        },
    );

    assert!(
        recorder.wait_terminal(2, Duration::from_secs(5)),
        "only {:?} reached the terminal sink within 5s",
        recorder.terminals()
    );
    assert_eq!(recorder.terminals(), vec![code, signal]);
}

/// The event path is load-bearing for every screen that exists today, so the
/// same dispatch that added a branch must leave it byte-for-byte alone:
/// `event` params verbatim to the event sink, terminal params verbatim to the
/// terminal sink, and the two never crossing.
#[test]
fn event_frames_still_reach_the_event_sink_unchanged() {
    let event_params = json!({
        "subscriptionId": "s-1", "seq": 42, "kind": "step.tool.end",
        "at": "2026-10-24T00:00:00Z",
        "payload": {"stepId": "st-1", "tool": "write_file", "linesAdded": 12}
    });
    let term_params = json!({"terminalId": "t-1", "seq": 1, "data": "bG9hZA=="});
    let (_fake, recorder, _session) = connected(
        "both",
        Script {
            pushes: vec![
                notify("event", event_params.clone()),
                notify("terminal.output", term_params.clone()),
            ],
        },
    );

    assert!(
        recorder.wait_events(1, Duration::from_secs(5))
            && recorder.wait_terminal(1, Duration::from_secs(5)),
        "sinks saw events={:?} terminals={:?}",
        recorder.events(),
        recorder.terminals()
    );
    assert_eq!(recorder.events(), vec![event_params], "the event stream changed shape");
    assert_eq!(recorder.terminals(), vec![term_params]);
}

/// The security half of the allow-list. A method nobody compiled is a message
/// nobody forwarded: its params reach **neither** sink, while frames after it
/// still flow (the reader neither dies nor starts guessing). The log half —
/// `handle_frame` prints the dropped method to stderr — is verified under
/// `--nocapture`; this asserts the property a test can assert in-process.
#[test]
fn an_unknown_notification_method_reaches_neither_sink() {
    let smuggled = json!({"terminalId": "t-EVIL", "seq": 0, "data": "cm0gLXJmIC8=", "kind": "loop.finished"});
    let marker = json!({"terminalId": "t-1", "seq": 1, "data": "bWFya2Vy"});
    let (_fake, recorder, _session) = connected(
        "unknown",
        Script {
            pushes: vec![
                notify("daemon.mystery", smuggled.clone()),
                notify("terminal.output", marker.clone()),
            ],
        },
    );

    // The marker queues behind the mystery frame in one stream, so when it
    // lands the mystery frame has been dispatched — and dropped.
    assert!(
        recorder.wait_terminal(1, Duration::from_secs(5)),
        "the marker frame never arrived; the reader died on the unknown method \
         (events: {:?}, terminals: {:?})",
        recorder.events(),
        recorder.terminals()
    );
    let all = [recorder.terminals(), recorder.events()].concat();
    assert!(
        !all.contains(&smuggled),
        "an unforwarded method's params reached a sink — the allow-list forwarded blindly: {all:?}"
    );
    assert_eq!(recorder.terminals(), vec![marker], "the stream after the dropped frame must still flow");
    assert_eq!(recorder.events(), Vec::<Value>::new());
}

/// A frame with no `method` at all is not forwarded either — the `None` arm
/// shares the unknown method's fate (logged, dropped), and the test pins that
/// it at least does not reach a sink with `null` params.
#[test]
fn a_notification_without_a_method_reaches_neither_sink() {
    let marker = json!({"terminalId": "t-1", "seq": 1, "data": "bWFya2Vy"});
    let (_fake, recorder, _session) = connected(
        "no-method",
        Script {
            pushes: vec![
                json!({"jsonrpc": "2.0", "params": {"terminalId": "t-EVIL"}}),
                notify("terminal.output", marker.clone()),
            ],
        },
    );

    assert!(
        recorder.wait_terminal(1, Duration::from_secs(5)),
        "the marker never arrived; the reader died on a method-less frame"
    );
    assert_eq!(recorder.terminals(), vec![marker]);
    assert_eq!(recorder.events(), Vec::<Value>::new());
}

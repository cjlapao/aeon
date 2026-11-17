//! `rpc::Target::Remote` and the production `Session`, proven against a fake
//! TCP daemon — not a real one, because none exists to dial yet.
//!
//! # Why this test exists, and what it does and doesn't prove
//!
//! `docs/aeon/S41-workspaces/design.md` defers the daemon-side network
//! listener a Remote workspace would actually reach. This test exists so the
//! client half isn't written on faith in the meantime: it proves the same
//! `Session` — same handshake, same correlation, same liveness/EOF handling
//! `tests/session.rs` and `tests/liveness.rs` prove over the unix socket —
//! works unchanged over a `TcpStream`, against a fake that speaks the wire
//! contract that design note assumes (newline-delimited JSON-RPC, full
//! parity, nothing attempted at auth here since nothing attempts it yet).
//!
//! **What this does not prove:** that a real daemon will ever answer this
//! way, or that the auth scheme the deferred slice designs will fit the shape
//! assumed here. Those are exactly what that slice still has to settle.

use std::io::{BufRead, BufReader, Write};
use std::net::TcpListener;
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use serde_json::Value;

#[path = "../src/endpoint.rs"]
mod endpoint;
#[path = "../src/rpc.rs"]
mod rpc;
#[path = "../src/session.rs"]
mod session;
#[path = "support/background_tray.rs"]
mod background_tray;

use session::{EventSink, Session};

/// Same recording sink shape as `tests/terminal_notify.rs` and
/// `tests/live_stream.rs` — standing in for the webview.
#[derive(Clone, Default)]
struct Recorder {
    closed: Arc<Mutex<Vec<String>>>,
}

impl Recorder {
    fn wait_closed(&self, timeout: Duration) -> Option<String> {
        let deadline = Instant::now() + timeout;
        while Instant::now() < deadline {
            if let Some(reason) = self.closed.lock().unwrap().first().cloned() {
                return Some(reason);
            }
            thread::sleep(Duration::from_millis(20));
        }
        None
    }
}

impl EventSink for Recorder {
    fn event(&self, _params: Value) {}
    fn terminal(&self, _params: Value) {}
    fn closed(&self, reason: String) {
        self.closed.lock().unwrap().push(reason);
    }
}

/// A fake daemon on a loopback TCP port, speaking the same handshake as
/// `tests/session.rs`'s unix-socket fake: `initialize` first, `version` from
/// the wire — proving the negotiated version came over this connection and
/// not from a constant the shell already knew.
struct FakeDaemon {
    port: u16,
    methods: mpsc::Receiver<String>,
}

fn start(daemon_version: &str) -> FakeDaemon {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind a loopback port");
    let port = listener.local_addr().expect("local addr").port();
    let (tx, rx) = mpsc::channel();
    let daemon_version = daemon_version.to_string();

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
                "initialize" => serde_json::json!({
                    "protocolVersion": 2,
                    "daemonVersion": daemon_version,
                    "capabilities": ["subscribe"]
                }),
                "version" => serde_json::json!({
                    "daemonVersion": daemon_version,
                    "protocolVersion": 2,
                }),
                _ => serde_json::json!({"ok": true, "method": method}),
            };
            let resp = serde_json::json!({"jsonrpc": "2.0", "id": id, "result": result});
            if writeln!(writer, "{resp}").is_err() {
                return;
            }
        }
    });

    FakeDaemon { port, methods: rx }
}

fn next_method(fake: &FakeDaemon, timeout: Duration) -> Option<String> {
    fake.methods.recv_timeout(timeout).ok()
}

#[test]
fn a_remote_target_completes_the_same_handshake_as_local() {
    let fake = start("7.7.7-remote-fake");
    let recorder = Recorder::default();
    let target = rpc::Target::Remote {
        host: "127.0.0.1".to_string(),
        port: fake.port,
    };

    let session = Session::connect(target, "0.1.0-test", recorder.clone())
        .expect("connect and handshake over TCP");

    assert_eq!(next_method(&fake, Duration::from_secs(1)), Some("initialize".into()));
    assert_eq!(next_method(&fake, Duration::from_secs(1)), Some("version".into()));

    let negotiated = session.negotiated();
    // The version came from the wire, not a value this test's assertion could
    // have guessed from a constant anywhere in the client — same property
    // `tests/session.rs` proves for the unix socket.
    assert_eq!(negotiated.daemon_version, "7.7.7-remote-fake");
    assert_eq!(negotiated.protocol_version, 2);
    assert_eq!(negotiated.endpoint, format!("127.0.0.1:{}", fake.port));
}

#[test]
fn a_call_round_trips_over_the_remote_target() {
    let fake = start("1.0.0-fake");
    let recorder = Recorder::default();
    let target = rpc::Target::Remote {
        host: "127.0.0.1".to_string(),
        port: fake.port,
    };
    let session = Session::connect(target, "0.1.0-test", recorder).expect("connect");

    let result = session
        .call("ping", Value::Null)
        .expect("a generic method round-trips over TCP exactly as over the unix socket");
    assert_eq!(result["method"], "ping");
}

#[test]
fn the_daemon_closing_the_tcp_connection_is_seen_as_eof_not_silence() {
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind a loopback port");
    let port = listener.local_addr().unwrap().port();

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
            let id = req["id"].clone();
            let result = serde_json::json!({"protocolVersion": 2, "daemonVersion": "1.0.0-fake"});
            let resp = serde_json::json!({"jsonrpc": "2.0", "id": id, "result": result});
            let _ = writeln!(writer, "{resp}");
            if req["method"] == "version" {
                // The daemon side hangs up right after the handshake — the
                // same shape a stopped daemon produces, over TCP's own EOF
                // rather than the unix socket's.
                return;
            }
        }
    });

    let recorder = Recorder::default();
    let target = rpc::Target::Remote {
        host: "127.0.0.1".to_string(),
        port,
    };
    let _session =
        Session::connect(target, "0.1.0-test", recorder.clone()).expect("connect and handshake");

    let reason = recorder
        .wait_closed(Duration::from_secs(2))
        .expect("the reader notices TCP EOF without needing a liveness probe");
    assert!(
        reason.contains("closed the connection"),
        "unexpected reason: {reason}"
    );
}

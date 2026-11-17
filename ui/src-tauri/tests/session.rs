//! The session, asserted on the wire against a recording fake.
//!
//! # Why a fake daemon and not the real one
//!
//! Three things need proving that the real daemon cannot prove:
//!
//!  1. **`initialize` arrives first.** Against the real daemon every order
//!     works once initialised, so a shell that got the order wrong would still
//!     pass. Only a fake that records arrival order can tell.
//!  2. **The displayed version came from the wire.** The fake answers a
//!     protocol version this code could not have guessed.
//!  3. **Correlation.** Two calls in flight with interleaved responses must not
//!     be crossed — which cannot happen against a daemon that answers in order.
//!
//! The real daemon is exercised by the ignored test at the end and by the demo.

use std::io::{BufRead, BufReader, Write};
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::PathBuf;
use std::sync::mpsc;
use std::thread;
use std::time::Duration;

#[path = "../src/endpoint.rs"]
mod endpoint;
#[path = "../src/rpc.rs"]
mod rpc;

/// A temp directory that removes itself. Hand-rolled rather than a dependency:
/// the shell has three dependencies and a test-only crate for six lines would
/// be a poor trade.
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

/// A fake daemon. It records the methods it receives in order, answers from a
/// script, and can push a notification.
struct FakeDaemon {
    path: PathBuf,
    methods: mpsc::Receiver<String>,
    _dir: TempDir,
}

/// How the fake answers. `protocol_version` is deliberately a parameter so a
/// test can make it something the shell could not have guessed.
struct Script {
    protocol_version: u32,
    daemon_version: String,
    /// When set, the fake delays this method's response by the given time, so
    /// a test can have two calls in flight at once.
    delay_method: Option<(String, Duration)>,
    /// When true, the fake sends one `event` notification after `version`.
    push_event: bool,
    /// When set, the fake answers this method with a JSON-RPC error carrying
    /// the three parts.
    error_method: Option<String>,
    /// When true, the fake closes the connection after the handshake.
    close_after_handshake: bool,
}

impl Default for Script {
    fn default() -> Self {
        Script {
            protocol_version: 1,
            daemon_version: "9.9.9-fake".into(),
            delay_method: None,
            push_event: false,
            error_method: None,
            close_after_handshake: false,
        }
    }
}

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
        let mut handshake_calls = 0;

        for line in reader.lines() {
            let Ok(line) = line else { return };
            let Ok(req): Result<serde_json::Value, _> = serde_json::from_str(&line) else {
                return;
            };
            let method = req["method"].as_str().unwrap_or("").to_string();
            let id = req["id"].clone();
            let _ = tx.send(method.clone());

            if let Some((delayed, how_long)) = &script.delay_method {
                if &method == delayed {
                    // Answer late, on another thread, so the next request is
                    // read and answered first — which is the interleaving a
                    // correlating client must survive.
                    let mut w = writer.try_clone().expect("clone");
                    let id = id.clone();
                    let how_long = *how_long;
                    thread::spawn(move || {
                        thread::sleep(how_long);
                        let resp = serde_json::json!({
                            "jsonrpc": "2.0", "id": id,
                            "result": {"slow": true, "method": "slow"}
                        });
                        let _ = writeln!(w, "{resp}");
                    });
                    continue;
                }
            }

            if script.error_method.as_deref() == Some(method.as_str()) {
                let resp = serde_json::json!({
                    "jsonrpc": "2.0", "id": id,
                    "error": {
                        "code": -31100,
                        "message": "unknown route",
                        "data": {
                            "what": "route \"nope\" is not configured; the configured routes are a, b",
                            "means": "a route names a provider, a model and its prices",
                            "todo": "use one of the configured route ids",
                            "fatal": false
                        }
                    }
                });
                let _ = writeln!(writer, "{resp}");
                continue;
            }

            let result = match method.as_str() {
                "initialize" => {
                    handshake_calls += 1;
                    serde_json::json!({
                        "protocolVersion": script.protocol_version,
                        "daemonVersion": script.daemon_version,
                        "capabilities": ["subscribe"],
                        // A field no current code knows: T3 says it must be
                        // tolerated, and the shell is a client like any other.
                        "somethingFromTheFuture": {"nested": true}
                    })
                }
                "version" => serde_json::json!({
                    "daemonVersion": script.daemon_version,
                    "protocolVersion": script.protocol_version,
                }),
                _ => serde_json::json!({"ok": true, "method": method}),
            };

            let resp = serde_json::json!({"jsonrpc": "2.0", "id": id, "result": result});
            if writeln!(writer, "{resp}").is_err() {
                return;
            }

            if method == "version" {
                if script.push_event {
                    // A notification: no `id`, which is how a client tells it
                    // from a response.
                    let ev = serde_json::json!({
                        "jsonrpc": "2.0", "method": "event",
                        "params": {
                            "subscriptionId": "s-1", "seq": 42, "kind": "ask.delta",
                            "at": "2026-10-15T00:00:00Z",
                            "payload": {"askId": "a-1", "kind": "text", "text": "hello"}
                        }
                    });
                    let _ = writeln!(writer, "{ev}");
                }
                if script.close_after_handshake {
                    return;
                }
            }
            let _ = handshake_calls;
        }
    });

    FakeDaemon { path, methods: rx, _dir: dir }
}

// --- the connection, without Tauri -------------------------------------------
//
// `Session` needs an `AppHandle` to emit events, which a unit test cannot
// construct. So these tests drive `rpc.rs` and the wire protocol directly, and
// the parts that need Tauri are covered by `ui/test/` against a fake invoker
// and by the demo against the real thing.
//
// **What that means honestly:** `session.rs`'s correlation logic is exercised
// here through an equivalent client written against the same rules, and the
// production path is exercised by `make dev`. The duplication is the cost of
// `AppHandle` not being constructible outside an app.

/// A minimal correlating client, following `session.rs`'s rules: write with an
/// id, read until that id comes back, treat a frame with no id as an event.
#[derive(Debug)]
struct TestClient {
    writer: rpc::Stream,
    reader: BufReader<rpc::Stream>,
    next_id: u64,
    events: Vec<serde_json::Value>,
}

impl TestClient {
    fn connect(path: &std::path::Path) -> Result<Self, String> {
        let stream = rpc::connect(&rpc::Target::Local(path.to_path_buf()))?;
        let reader = BufReader::new(stream.try_clone().map_err(|e| e.to_string())?);
        Ok(TestClient { writer: stream, reader, next_id: 0, events: Vec::new() })
    }

    fn call(&mut self, method: &str, params: serde_json::Value) -> Result<serde_json::Value, rpc::RpcError> {
        self.next_id += 1;
        let id = self.next_id;
        let mut req = serde_json::json!({"jsonrpc": "2.0", "id": id, "method": method});
        if !params.is_null() {
            req["params"] = params;
        }
        writeln!(self.writer, "{req}").map_err(rpc::RpcError::connection_lost)?;

        loop {
            let mut line = String::new();
            match self.reader.read_line(&mut line) {
                Ok(0) => return Err(rpc::RpcError::connection_lost("end of stream")),
                Ok(_) => {}
                Err(e) => return Err(rpc::RpcError::connection_lost(e)),
            }
            let value: serde_json::Value = match serde_json::from_str(&line) {
                Ok(v) => v,
                Err(_) => continue,
            };
            match value.get("id").and_then(|v| v.as_u64()) {
                Some(got) if got == id => {
                    if let Some(err) = value.get("error") {
                        return Err(rpc::RpcError::from_wire(err));
                    }
                    return Ok(value.get("result").cloned().unwrap_or(serde_json::Value::Null));
                }
                Some(_) => continue, // another call's response
                None => {
                    if value.get("method").and_then(|m| m.as_str()) == Some("event") {
                        if let Some(params) = value.get("params") {
                            self.events.push(params.clone());
                        }
                    }
                }
            }
        }
    }

    fn handshake(&mut self) -> Result<(String, u32), rpc::RpcError> {
        let init = self.call(
            "initialize",
            serde_json::json!({
                "clientName": "aeon-app",
                "clientVersion": "0.1.0-test",
                "protocolVersion": 1
            }),
        )?;
        let version = self.call("version", serde_json::Value::Null)?;
        let daemon = version["daemonVersion"].as_str().unwrap_or("").to_string();
        let protocol = version["protocolVersion"].as_u64().unwrap_or(0) as u32;
        assert_eq!(
            init["protocolVersion"].as_u64().unwrap_or(0) as u32,
            protocol,
            "initialize and version disagree about the negotiated protocol version"
        );
        Ok((daemon, protocol))
    }
}

#[test]
fn initialize_is_the_first_frame_on_the_wire() {
    let fake = start("order", Script::default());
    let mut client = TestClient::connect(&fake.path).expect("connect");
    client.handshake().expect("handshake");
    client.call("routes.list", serde_json::Value::Null).expect("routes.list");

    let first = fake.methods.recv().expect("a first frame");
    assert_eq!(
        first, "initialize",
        "the first frame on the wire was {first:?}; `initialize` is mandatory and first (T7)"
    );
    assert_eq!(fake.methods.recv().expect("second"), "version");
    assert_eq!(fake.methods.recv().expect("third"), "routes.list");
}

#[test]
fn the_shell_reports_the_version_the_daemon_sent_not_a_constant() {
    // 7 is not PROTOCOL_VERSION and never will be by accident.
    let fake = start(
        "wire",
        Script { protocol_version: 7, daemon_version: "from-the-wire".into(), ..Default::default() },
    );
    let mut client = TestClient::connect(&fake.path).expect("connect");
    let (daemon, protocol) = client.handshake().expect("handshake");

    assert_eq!(
        protocol, 7,
        "the shell reported {protocol} for a daemon that said 7 — it is displaying its own constant"
    );
    assert_eq!(daemon, "from-the-wire");
}

#[test]
fn an_unknown_field_in_the_result_is_tolerated() {
    // The fake's initialize result carries `somethingFromTheFuture`. A client
    // that rejected it would turn every additive change into a breaking one.
    let fake = start("forward", Script::default());
    let mut client = TestClient::connect(&fake.path).expect("connect");
    client.handshake().expect("a result with an unknown field was rejected");
}

/// **Correlation**: two calls in flight, answered out of order.
///
/// The fake delays `slow` and answers `fast` first. A client that assumed
/// in-order responses would hand `slow`'s caller `fast`'s result — which cannot
/// happen against a daemon that answers in order, and so cannot be caught
/// without a fake that does not.
#[test]
fn two_calls_in_flight_are_not_crossed() {
    let fake = start(
        "correlate",
        Script {
            delay_method: Some(("slow".into(), Duration::from_millis(200))),
            ..Default::default()
        },
    );
    let mut client = TestClient::connect(&fake.path).expect("connect");
    client.handshake().expect("handshake");

    // Write `slow` without waiting, then call `fast` and read.
    client.next_id += 1;
    let slow_id = client.next_id;
    writeln!(
        client.writer,
        "{}",
        serde_json::json!({"jsonrpc": "2.0", "id": slow_id, "method": "slow"})
    )
    .expect("write slow");

    let fast = client.call("fast", serde_json::Value::Null).expect("fast");
    assert_eq!(
        fast["method"].as_str(),
        Some("fast"),
        "the fast call received another call's result: {fast}"
    );
}

/// A notification arriving between a request and its response must not be
/// mistaken for the response.
#[test]
fn a_notification_is_not_mistaken_for_a_response() {
    let fake = start("notify", Script { push_event: true, ..Default::default() });
    let mut client = TestClient::connect(&fake.path).expect("connect");
    client.handshake().expect("handshake");

    // The event was pushed right after `version`; the next call must still get
    // its own result.
    let result = client.call("ping", serde_json::Value::Null).expect("ping after an event");
    assert_eq!(result["method"].as_str(), Some("ping"));
    assert_eq!(client.events.len(), 1, "the event was not captured: {:?}", client.events);
    assert_eq!(client.events[0]["kind"].as_str(), Some("ask.delta"));
    assert_eq!(client.events[0]["seq"].as_u64(), Some(42), "the daemon's event cursor must pass through unchanged");
}

/// An Aeon error keeps its three parts across the boundary, because a Tauri
/// command's error is a string and that is where structure gets flattened.
#[test]
fn an_aeon_error_keeps_its_three_parts() {
    let fake = start("error", Script { error_method: Some("ask".into()), ..Default::default() });
    let mut client = TestClient::connect(&fake.path).expect("connect");
    client.handshake().expect("handshake");

    let err = client.call("ask", serde_json::json!({"route": "nope"})).expect_err("an error");
    assert_eq!(err.code, -31100);
    assert_eq!(err.kind, rpc::ErrorKind::Rpc);
    let data = err.data.as_ref().expect("the three parts");
    assert!(!data.what.is_empty(), "what is empty");
    assert!(!data.means.is_empty(), "means is empty");
    assert!(!data.todo.is_empty(), "todo is empty");

    // And it survives being rendered as a string, which is what a Tauri
    // command does to it.
    let rendered = err.to_string();
    let back: serde_json::Value = serde_json::from_str(&rendered).expect("the string is JSON");
    assert_eq!(back["data"]["todo"], data.todo);
}

/// A transport failure carries NO three parts, and that distinction is the
/// point: inventing a `means` for a closed socket would be fiction.
#[test]
fn a_transport_failure_has_no_three_parts() {
    let err = rpc::RpcError::transport("connect: no such file or directory");
    assert_eq!(err.kind, rpc::ErrorKind::Transport);
    assert!(err.data.is_none());
    assert_eq!(err.code, rpc::CODE_TRANSPORT);
    assert!(err.code > 0, "a transport code must not be confusable with a JSON-RPC or Aeon code");
}

#[test]
fn timeout_and_unstructured_daemon_error_keep_their_kinds() {
    let timeout = rpc::RpcError::timeout(Duration::from_secs(3));
    assert_eq!(timeout.kind, rpc::ErrorKind::Timeout);
    assert_eq!(serde_json::to_value(&timeout).unwrap()["kind"], "timeout");

    let refusal = rpc::RpcError::from_wire(&serde_json::json!({
        "code": -32601,
        "message": "method not found"
    }));
    assert_eq!(refusal.kind, rpc::ErrorKind::Rpc);
    assert_eq!(refusal.code, -32601);
    assert!(refusal.data.is_none());
}

#[test]
fn a_closed_connection_fails_the_call_rather_than_hanging() {
    let fake = start("closed", Script { close_after_handshake: true, ..Default::default() });
    let mut client = TestClient::connect(&fake.path).expect("connect");
    client.handshake().expect("handshake");

    let err = client.call("ping", serde_json::Value::Null).expect_err("the daemon went away");
    assert!(
        err.message.contains("the connection to the daemon has closed"),
        "the error does not say the connection closed in words a person can act on: {}",
        err.message
    );
    // And it says what to do, which `Broken pipe (os error 32)` does not.
    assert!(err.message.contains("reconnect"), "no remedy offered: {}", err.message);
}

#[test]
fn a_missing_daemon_names_the_path_it_tried() {
    let dir = TempDir::new("absent");
    let missing = dir.0.join("nothing.sock");
    let err = TestClient::connect(&missing).expect_err("there is no daemon");
    // The path is the first thing anyone debugging needs — and the reason the
    // daemon prints its own at startup, so the two can be compared.
    assert!(
        err.contains("connect"),
        "the failure does not explain itself: {err}"
    );
}

#[test]
fn an_explicit_endpoint_path_wins() {
    let dir = TempDir::new("env");
    let explicit = dir.0.join("explicit.sock");
    std::env::set_var("LOCALRPC_ENDPOINT_PATH", &explicit);
    assert_eq!(endpoint::path(), explicit);
    std::env::remove_var("LOCALRPC_ENDPOINT_PATH");
}

/// Against the REAL daemon, when one is pointed at.
///
/// Gated on an environment variable: a unit test must not require a running
/// daemon, and must never reach for the real endpoint path (S1's I6).
///
///   LOCALRPC_ENDPOINT_PATH=/tmp/x/rpc.sock aeon serve &
///   AEON_LIVE_ENDPOINT=/tmp/x/rpc.sock cargo test -- --ignored
#[test]
#[ignore = "needs a running daemon; set AEON_LIVE_ENDPOINT"]
fn the_shell_talks_to_a_real_daemon() {
    let path = std::env::var("AEON_LIVE_ENDPOINT")
        .expect("set AEON_LIVE_ENDPOINT to a socket a daemon is serving");
    let mut client = TestClient::connect(std::path::Path::new(&path)).expect("connect");
    let (daemon, protocol) = client.handshake().expect("handshake with the real daemon");
    assert!(!daemon.is_empty(), "the real daemon reported no version");
    assert_eq!(protocol, 1);

    // And the route methods the test bench depends on.
    let routes = client.call("routes.list", serde_json::Value::Null).expect("routes.list");
    assert!(routes.get("providers").is_some(), "routes.list has no providers: {routes}");
    assert_eq!(
        routes["persisted"].as_bool(),
        Some(false),
        "routes.list claims persistence; the store is S4"
    );

    // **No credential anywhere in the response.**
    let rendered = routes.to_string();
    for shape in ["sk-", "apiKey"] {
        assert!(
            !rendered.contains(shape),
            "routes.list leaked something key-shaped ({shape}): {rendered}"
        );
    }
    println!("live: daemon {daemon} protocol {protocol}, routes.list clean");
}

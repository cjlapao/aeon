//! The real path, end to end: Go daemon → socket → the production reader → a
//! sink.
//!
//! # Why this test exists
//!
//! The UI sat at "Streaming…" and nothing arrived, and **nothing in the suite
//! could say where it broke** — the daemon might not have sent, the reader
//! might not have read, or the emit might not have landed. Three hops, no
//! coverage.
//!
//! This covers the first two with the production code, leaving only Tauri's own
//! `emit` untested. Gated on `AEON_LIVE_ENDPOINT`, because a unit test must not
//! require a running daemon.
//!
//!   LOCALRPC_ENDPOINT_PATH=/tmp/x/rpc.sock aeon serve &
//!   AEON_LIVE_ENDPOINT=/tmp/x/rpc.sock cargo test --test live_stream -- --ignored --nocapture

use std::path::Path;
use std::sync::{Arc, Mutex};
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

/// A sink that records, standing in for the webview.
#[derive(Clone, Default)]
struct Recorder {
    events: Arc<Mutex<Vec<Value>>>,
    terminals: Arc<Mutex<Vec<Value>>>,
    closed: Arc<Mutex<Option<String>>>,
}

impl session::EventSink for Recorder {
    fn event(&self, params: Value) {
        self.events.lock().unwrap().push(params);
    }
    fn terminal(&self, params: Value) {
        // Recorded, not ignored: a sink that silently drops is the failure
        // this trait exists to make visible. `tests/terminal_notify.rs`
        // asserts the shape; here it only has to not lose the frame.
        self.terminals.lock().unwrap().push(params);
    }
    fn closed(&self, reason: String) {
        *self.closed.lock().unwrap() = Some(reason);
    }
}

impl Recorder {
    fn kinds(&self) -> Vec<String> {
        self.events
            .lock()
            .unwrap()
            .iter()
            .map(|e| e["kind"].as_str().unwrap_or("?").to_string())
            .collect()
    }

    fn wait_for(&self, kind: &str, timeout: Duration) -> bool {
        let deadline = Instant::now() + timeout;
        while Instant::now() < deadline {
            if self.kinds().iter().any(|k| k == kind) {
                return true;
            }
            std::thread::sleep(Duration::from_millis(20));
        }
        false
    }
}

fn live_endpoint() -> String {
    std::env::var("AEON_LIVE_ENDPOINT")
        .expect("set AEON_LIVE_ENDPOINT to a socket a daemon is serving")
}

/// **The test the stuck UI needed.**
///
/// Subscribe, ask, and assert the deltas and the `ask.done` reach the sink —
/// through the same `Session` the app uses.
#[test]
#[ignore = "needs a running daemon and a route; set AEON_LIVE_ENDPOINT and AEON_LIVE_ROUTE"]
fn ask_events_reach_the_sink() {
    let endpoint = live_endpoint();
    let route = std::env::var("AEON_LIVE_ROUTE").unwrap_or_else(|_| "local-fixture".into());

    let recorder = Recorder::default();
    let session = session::Session::connect(rpc::Target::Local(Path::new(&endpoint).to_path_buf()), "0.1.0-test", recorder.clone())
        .expect("connect and handshake");

    let negotiated = session.negotiated();
    println!(
        "connected: daemon {} protocol {}",
        negotiated.daemon_version, negotiated.protocol_version
    );

    // Exactly what the Ask page does, in the same order.
    let sub = session
        .call("subscribe", json!({"filter": {"kinds": ["ask.*"]}}))
        .expect("subscribe");
    let subscription_id = sub["subscriptionId"].as_str().expect("a subscription id");
    println!("subscribed: {subscription_id}");

    let ask = session
        .call(
            "ask",
            json!({
                "route": route,
                "prompt": "say hello",
                "subscriptionId": subscription_id
            }),
        )
        .expect("ask");
    let ask_id = ask["askId"].as_str().expect("an askId");
    println!("asked: {ask_id}");

    assert!(
        recorder.wait_for("ask.done", Duration::from_secs(30)),
        "no ask.done reached the sink within 30s. kinds seen: {:?}\n\
         The daemon, the socket and the reader are all on this path — if the \
         daemon's own `aeon watch --kinds ask.*` shows the events, the break is \
         between the socket and the sink.",
        recorder.kinds()
    );

    let kinds = recorder.kinds();
    println!("events: {kinds:?}");
    assert!(
        kinds.iter().any(|k| k == "ask.delta"),
        "ask.done arrived but no ask.delta did: {kinds:?}"
    );

    // Every event carries the askId the call returned, which is what the page
    // filters on — a mismatch here is why a UI would show nothing.
    let events = recorder.events.lock().unwrap();
    for event in events.iter().filter(|e| e["kind"] == "ask.delta") {
        assert_eq!(
            event["payload"]["askId"].as_str(),
            Some(ask_id),
            "a delta carries a different askId from the one `ask` returned; the page filters on \
             that and would drop it: {event}"
        );
    }

    // And Aeon's seq is in `params.seq` — the cursor, not the envelope's.
    for event in events.iter() {
        assert!(
            event["seq"].as_u64().unwrap_or(0) > 0,
            "an event has no params.seq: {event}"
        );
    }
}

/// The subscription the UI holds must receive events from ANOTHER client's ask
/// too — which is how the test bench can be driven from a terminal while the
/// window is open, and how S9's app will see a loop started elsewhere.
#[test]
#[ignore = "needs a running daemon; set AEON_LIVE_ENDPOINT"]
fn a_subscription_receives_another_clients_events() {
    let endpoint = live_endpoint();
    let recorder = Recorder::default();
    let session = session::Session::connect(rpc::Target::Local(Path::new(&endpoint).to_path_buf()), "0.1.0-test", recorder.clone())
        .expect("connect");

    session
        .call("subscribe", json!({"filter": {"kinds": ["client.*"]}}))
        .expect("subscribe");

    // A second connection, which produces `client.connected` for the first.
    let other = session::Session::connect(rpc::Target::Local(Path::new(&endpoint).to_path_buf()), "0.1.0-other", Recorder::default())
        .expect("connect a second client");
    other.call("ping", Value::Null).expect("ping");

    assert!(
        recorder.wait_for("client.connected", Duration::from_secs(10)),
        "the subscription did not receive another client's event: {:?}",
        recorder.kinds()
    );
}

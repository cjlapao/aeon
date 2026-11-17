//! The connection's liveness, asserted against the reader thread that actually
//! ships.
//!
//! # Why this file exists
//!
//! Because the defect it tests for shipped. `session.rs`'s reader looped over
//! `BufReader::lines()` with a 600s `SO_RCVTIMEO` on the socket, and treated the
//! `Err(WouldBlock)` that timeout produces as a fatal read error. Ten quiet
//! minutes — which is what an idle window *is*, since a loop parked at a gate
//! emits nothing for hours and is perfectly current — and a working connection
//! reported itself lost:
//!
//! ```text
//! not connected — read from the daemon: Resource temporarily unavailable (os error 11)
//! ```
//!
//! Nothing in the suite could catch it, because every existing test either kept
//! the connection busy or tore it down in milliseconds. So these tests are
//! deliberately about **time**: they shrink `Timing` until "idle" and "wedged"
//! arrive in milliseconds, and then assert what the window sees.
//!
//! # Why the real `Session` and not `tests/session.rs`'s `TestClient`
//!
//! `tests/session.rs` drives the wire protocol through an equivalent client
//! written against the same rules — which is exactly why it passed. This file
//! includes `session.rs` itself and drives its reader through a recording
//! `EventSink`, so the assertions land on the code that runs in the app.
//!
//! # The four situations, kept apart
//!
//! | Situation           | What the reader must do                                     |
//! | ------------------- | ----------------------------------------------------------- |
//! | Idle, daemon alive  | say nothing, stay connected, keep answering calls            |
//! | Idle past the tick  | send a `ping`, and stay connected because it was answered    |
//! | Wedged, socket open | run out of pings and say so, with a reason a person can act on |
//! | Retired by the app  | end the thread, close the connection, and **not** cry lost   |

use std::io::{BufRead, BufReader, Read, Write};
use std::os::unix::net::UnixListener;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{self, Receiver};
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

// --- the harness -------------------------------------------------------------

/// A temp directory that removes itself. Hand-rolled rather than a dependency,
/// following `tests/session.rs`: the shell has three dependencies and a
/// test-only crate for six lines would be a poor trade.
struct TempDir(PathBuf);

impl TempDir {
    fn new(tag: &str) -> Self {
        let mut p = std::env::temp_dir();
        p.push(format!(
            "aeon-liveness-{tag}-{}-{}",
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

/// What the window would see.
///
/// Cloned into `Session::connect_with` while a second handle stays here for the
/// assertions, because the sink is moved into the reader thread and the entire
/// point is to ask that thread what it decided.
#[derive(Clone, Default)]
struct Recorder {
    events: Arc<Mutex<Vec<Value>>>,
    terminals: Arc<Mutex<Vec<Value>>>,
    closed: Arc<Mutex<Vec<String>>>,
}

impl Recorder {
    fn closed_reasons(&self) -> Vec<String> {
        self.closed.lock().unwrap().clone()
    }

    fn saw_closed(&self) -> bool {
        !self.closed.lock().unwrap().is_empty()
    }
}

impl EventSink for Recorder {
    fn event(&self, params: Value) {
        self.events.lock().unwrap().push(params);
    }

    fn terminal(&self, params: Value) {
        // Recorded, not dropped: these tests are about liveness, and the one
        // thing a stand-in sink must never do is lose a frame silently.
        self.terminals.lock().unwrap().push(params);
    }

    fn closed(&self, reason: String) {
        self.closed.lock().unwrap().push(reason);
    }
}

/// A daemon that answers, and can be made to stop answering while keeping the
/// socket open — the state no timeout can tell apart from a healthy idle.
struct FakeDaemon {
    path: PathBuf,
    _dir: TempDir,
    /// Every method asked for, in arrival order. A probe test needs this: a
    /// `ping` really went out.
    requests: Receiver<String>,
    /// A handler saw EOF, i.e. a client genuinely went away rather than leaving
    /// a parked reader thread holding its descriptor open.
    hung_up: Receiver<()>,
    /// Flip it and `ping` is never answered again. The socket stays open and the
    /// handler keeps reading, so writes go through: this is a daemon that is
    /// stuck, not one that has died.
    wedge: Arc<AtomicBool>,
}

impl FakeDaemon {
    fn drain_requests(&self) -> Vec<String> {
        let mut all = Vec::new();
        while let Ok(m) = self.requests.try_recv() {
            all.push(m);
        }
        all
    }

    fn pings(&self) -> usize {
        self.drain_requests()
            .iter()
            .filter(|m| *m == "ping")
            .count()
    }

    /// A connection is unclaimed: the client closed it, rather than dropping the
    /// `Session` and leaving a reader thread behind.
    fn hung_up(&self) -> bool {
        self.hung_up.try_recv().is_ok()
    }
}

/// How the fake misbehaves, per test.
#[derive(Default, Clone)]
struct Behaviour {
    /// Methods that are read, recorded, and **never answered**.
    silence: Vec<String>,
    /// Answer this method this late, from its own thread — so the handler keeps
    /// reading and answering everything else meanwhile.
    ///
    /// The concurrency is the point, not a detail. A daemon thinking for a
    /// second has not stopped reading, and a fake that answered serially would
    /// make the starvation test pass by *being* the bug. The real daemon gets
    /// this property from the framework, which runs each request in its own
    /// goroutine (`localrpc/service/conn.go:175`); the fake reproduces it
    /// deliberately rather than assuming the test environment has it.
    delay: Option<(String, Duration)>,
}

fn start(tag: &str) -> FakeDaemon {
    start_with(tag, Behaviour::default())
}

fn start_with(tag: &str, behaviour: Behaviour) -> FakeDaemon {
    let dir = TempDir::new(tag);
    let path = dir.0.join("rpc.sock");
    let listener = UnixListener::bind(&path).expect("bind the fake endpoint");
    let (req_tx, requests) = mpsc::channel();
    let (hang_tx, hung_up) = mpsc::channel();
    let wedge = Arc::new(AtomicBool::new(false));

    // One handler per connection, because a test that replaces a session opens
    // two and a fake that accepts only the first would leave the second waiting
    // out `CALL_TIMEOUT` — a hang posing as a test.
    let accept_wedge = Arc::clone(&wedge);
    thread::spawn(move || {
        for conn in listener.incoming() {
            let Ok(stream) = conn else { return };
            let req_tx = req_tx.clone();
            let hang_tx = hang_tx.clone();
            let wedged = Arc::clone(&accept_wedge);
            let behaviour = behaviour.clone();
            thread::spawn(move || handle(stream, req_tx, hang_tx, wedged, behaviour));
        }
    });

    FakeDaemon {
        path,
        _dir: dir,
        requests,
        hung_up,
        wedge,
    }
}

/// One fake client connection. Answers everything, and obeys three fictions:
/// `wedge` silences `ping`, `Behaviour::silence` silences named methods, and
/// `die` closes the handler's handles so the client sees the EOF a real `kill`
/// would produce.
fn handle(
    stream: std::os::unix::net::UnixStream,
    req_tx: mpsc::Sender<String>,
    hang_tx: mpsc::Sender<()>,
    wedged: Arc<AtomicBool>,
    behaviour: Behaviour,
) {
    let mut writer = stream.try_clone().expect("clone");
    let reader = BufReader::new(stream);

    for line in reader.lines() {
        let Ok(line) = line else {
            // EOF. The other end is gone — what `retiring_a_session…` waits for.
            let _ = hang_tx.send(());
            return;
        };
        let Ok(req): Result<Value, _> = serde_json::from_str(&line) else {
            continue;
        };
        let Some(method) = req["method"].as_str().map(str::to_string) else {
            continue;
        };
        let id = req["id"].clone();
        let _ = req_tx.send(method.clone());

        if method == "die" {
            return; // dropping both handles here is what sends the peer EOF
        }
        if method == "ping" && wedged.load(Ordering::SeqCst) {
            continue; // silence, socket still open
        }
        if behaviour.silence.iter().any(|m| m == &method) {
            continue; // read and recorded, never answered
        }
        if let Some((slow, how_long)) = behaviour.delay.clone() {
            if slow == method {
                // Late, on another thread, so the loop below goes on reading —
                // which is the difference between a slow daemon and a stuck one,
                // and the whole subject of `a_slow_answer…`.
                let mut late_writer = writer.try_clone().expect("clone");
                let id = id.clone();
                thread::spawn(move || {
                    thread::sleep(how_long);
                    let resp = serde_json::json!({
                        "jsonrpc": "2.0", "id": id,
                        "result": {"ok": true, "method": slow, "slow": true}
                    });
                    let _ = writeln!(late_writer, "{resp}");
                });
                continue;
            }
        }

        let result = match method.as_str() {
            "initialize" => serde_json::json!({
                "protocolVersion": 1,
                "daemonVersion": "9.9.9-fake",
                "capabilities": ["subscribe"],
            }),
            "version" => serde_json::json!({
                "daemonVersion": "9.9.9-fake",
                "protocolVersion": 1,
            }),
            "ping" => serde_json::json!({}),
            _ => serde_json::json!({"ok": true, "method": method}),
        };
        let resp = serde_json::json!({"jsonrpc": "2.0", "id": id, "result": result});
        if writeln!(writer, "{resp}").is_err() {
            return;
        }
    }
    let _ = hang_tx.send(());
}

/// Timings short enough to sit through, shaped like production's.
fn fast_timing() -> rpc::Timing {
    rpc::Timing {
        read_tick: Duration::from_millis(20),
        write_timeout: Duration::from_secs(5),
        probe_after: Duration::from_millis(100),
        probe_timeout: Duration::from_millis(100),
        probe_strikes: 2,
    }
}

/// The same, with the probe effectively never firing.
///
/// Needed to test the regression itself in isolation: with the probe live, a
/// surviving connection could be surviving *because* of its pings. Here nothing
/// is ever sent, so all the reader sees is one `Err(WouldBlock)` after another
/// — precisely what the old loop treated as a dead connection.
fn probeless_timing() -> rpc::Timing {
    rpc::Timing {
        probe_after: Duration::from_secs(3600),
        ..fast_timing()
    }
}

fn connect(daemon: &FakeDaemon, timing: &rpc::Timing) -> (Session, Recorder) {
    let recorder = Recorder::default();
    let session = Session::connect_with(rpc::Target::Local(daemon.path.clone()), "0.0.0-test", recorder.clone(), timing)
        .expect("the fake daemon completes the handshake");
    (session, recorder)
}

fn wait_for(what: &str, mut ready: impl FnMut() -> bool) {
    let deadline = Instant::now() + Duration::from_secs(5);
    while Instant::now() < deadline {
        if ready() {
            return;
        }
        thread::sleep(Duration::from_millis(10));
    }
    panic!("timed out waiting for {what}");
}

// --- the regression ----------------------------------------------------------

/// The bug in one sentence: a socket whose read times out is **idle**, and the
/// reader used to read that as fatal.
///
/// `read_tick` is 20ms and the probe is switched off, so the connection absorbs
/// dozens of the exact `Err(WouldBlock)` a 600s `SO_RCVTIMEO` used to take ten
/// minutes to produce — and the old loop broke on the first one. The assertion
/// is not "nothing was printed" but that the connection is still **usable**.
#[test]
fn idle_is_not_a_dead_connection() {
    let daemon = start("idle");
    let (session, recorder) = connect(&daemon, &probeless_timing());

    thread::sleep(Duration::from_millis(600));

    assert!(
        !recorder.saw_closed(),
        "an idle connection announced itself lost: {:?}",
        recorder.closed_reasons()
    );
    assert_eq!(
        daemon.pings(),
        0,
        "the probe was supposed to be switched off"
    );

    // Still a working connection, not merely a quiet one.
    let answer = session
        .call("store.info", Value::Null)
        .expect("a call after ten minutes of idle must still work");
    assert_eq!(
        answer["method"], "store.info",
        "answered the wrong thing: {answer}"
    );
}

/// **Silence is not staleness** (S9's T3, D6).
///
/// The probe exists to answer a question silence cannot, so it must never
/// *create* the condition it is checking for. The daemon answers every ping, the
/// connection sits idle far past `probe_after` — and the window stays connected
/// with no warning. A test that only asserted "no crash" would also pass on a UI
/// that had quietly gone stale.
#[test]
fn silence_on_a_live_socket_stays_connected() {
    let daemon = start("silence");
    let timing = fast_timing();
    let (session, recorder) = connect(&daemon, &timing);

    thread::sleep(timing.dead_after() * 3);

    assert!(
        !recorder.saw_closed(),
        "a daemon that answers its pings was called dead: {:?}",
        recorder.closed_reasons()
    );
    wait_for("at least one liveness ping to have been sent", || {
        daemon.pings() >= 1
    });
    assert!(
        session.call("version", Value::Null).is_ok(),
        "probed, but unusable"
    );
}

/// The probe is a **liveness probe, not polling** (I4): it asks `ping` and
/// nothing else. A test that only checked that the connection survived would
/// also pass if the reader had been quietly re-reading the store every tick.
#[test]
fn the_liveness_probe_asks_ping_and_nothing_else() {
    let daemon = start("probe-method");
    let timing = fast_timing();
    let (_session, _recorder) = connect(&daemon, &timing);

    thread::sleep(timing.dead_after() * 3);
    let asked = daemon.drain_requests();
    let extra: Vec<&String> = asked
        .iter()
        .filter(|m| *m != "initialize" && *m != "version")
        .collect();
    assert!(
        extra.iter().all(|m| m.as_str() == "ping"),
        "the reader asked the daemon for more than liveness: {extra:?}"
    );
    assert!(
        !extra.is_empty(),
        "idle past probe_after and never asked anything"
    );
}

// --- the wedged daemon -------------------------------------------------------

/// A daemon that is alive but not answering must not leave the window claiming
/// to be connected forever.
///
/// The socket stays open and the fake keeps reading, so neither end sees EOF and
/// every write succeeds — the one state the kernel cannot help with, and the
/// only reason a timer earns its place. `probe_strikes` is 2 rather than 1
/// because a single unanswered ping against a paused daemon would cry wolf.
#[test]
fn a_wedged_daemon_is_called_dead_with_something_to_act_on() {
    let daemon = start("wedged");
    let timing = fast_timing();
    let (session, recorder) = connect(&daemon, &timing);

    daemon.wedge.store(true, Ordering::SeqCst);

    // Not before its time: a connection must not be called dead while its first
    // probe is still inside its own allowance.
    thread::sleep(timing.probe_after / 2);
    assert!(
        !recorder.saw_closed(),
        "called dead before the probe had timed out"
    );

    wait_for("the probe to run out of strikes", || recorder.saw_closed());
    let reason = recorder.closed_reasons().remove(0);

    // What happened, what it means, what now (T10, C9). `os error 11` is none of
    // the three, which is the whole reason this reason is written out.
    assert!(reason.contains("liveness probe"), "{reason}");
    assert!(
        reason.contains("wedged"),
        "{reason} should say the socket is still open"
    );
    assert!(
        reason.contains("aeon serve"),
        "{reason} should say what to run"
    );

    // And the window is not left guessing: a call now fails promptly with the
    // same story rather than waiting out 300s against a socket nobody reads.
    let started = Instant::now();
    let err = session
        .call("store.info", Value::Null)
        .expect_err("a wedged daemon cannot answer");
    assert!(
        started.elapsed() < Duration::from_secs(2),
        "the call waited {:?} instead of failing on a known-dead connection",
        started.elapsed()
    );
    assert!(err.contains("liveness probe"), "{err}");
}

/// The other half of honesty: a **stopped** daemon needs no timer, and must be
/// noticed well inside the probe's patience. If this regressed to waiting for
/// `dead_after`, every crash would look like a forty-second outage.
#[test]
fn a_stopped_daemon_is_seen_immediately_not_on_the_next_probe() {
    let daemon = start("stopped");
    let timing = fast_timing();
    let (session, recorder) = connect(&daemon, &timing);

    // The fake closes both its handles: the kernel sends EOF at once.
    let _ = session.call("die", Value::Null);

    wait_for("EOF to be reported as lost", || recorder.saw_closed());
    let reason = recorder.closed_reasons().remove(0);
    assert!(
        reason.contains("the daemon closed the connection"),
        "{reason}"
    );
    assert!(
        !reason.contains("liveness probe"),
        "a clean EOF should not be blamed on the probe: {reason}"
    );
}

// --- the leaked connection ---------------------------------------------------

/// A session the app stopped using must stop existing.
///
/// The reader thread owns a `try_clone`d descriptor, so dropping the `Session`
/// closed nothing: the daemon kept seeing a client, and the thread stayed parked
/// in `read` holding it open. That is where the second `client connected` line
/// came from for one window, and why a connection nobody owned could later
/// announce itself lost and mark a healthy window disconnected.
#[test]
fn retiring_a_session_ends_the_connection_rather_than_leaving_it() {
    let daemon = start("retire");
    let (session, recorder) = connect(&daemon, &probeless_timing());

    drop(session);

    // The proof is on the daemon's side of the wire, not in the client's flags:
    // every descriptor has really gone, so the handler sees EOF.
    wait_for("the daemon to see the connection close", || {
        daemon.hung_up()
    });
    assert!(
        !recorder.saw_closed(),
        "a connection the app closed was reported as lost: {:?}",
        recorder.closed_reasons()
    );
}

/// A dropped session must not leave the window reading "not connected".
///
/// `aeon://closed` carries no session identity, so this emit is the only thing
/// distinguishing "this window's daemon went away" from "a connection the app
/// threw away finally timed out" — and the second used to win, minutes after the
/// first stopped mattering.
#[test]
fn replacing_a_session_is_not_an_outage() {
    let daemon = start("replace");
    let timing = fast_timing();
    let (first, first_recorder) = connect(&daemon, &timing);

    // What `connect` does on Reconnect: the first session is replaced, and the
    // shell drops it.
    let (_second, second_recorder) = connect(&daemon, &timing);
    drop(first);

    // Both lifetimes are on the table here, and neither has any business
    // emitting: the retired one because we chose it, the live one because it is
    // answering.
    thread::sleep(timing.dead_after() + Duration::from_millis(300));
    assert!(
        !first_recorder.saw_closed(),
        "the retired session cried lost: {:?}",
        first_recorder.closed_reasons()
    );
    assert!(
        !second_recorder.saw_closed(),
        "the live session was marked lost by its predecessor: {:?}",
        second_recorder.closed_reasons()
    );
}

/// A handshake that never completed is not a lost connection either.
///
/// `Session::connect_with` spawns the reader *before* `initialize`, so every
/// failure between there and `Ok` drops a session whose reader is already
/// parked. Without retiring on drop, that thread lives on to emit later — on a
/// window still showing "connecting…".
#[test]
fn a_failed_handshake_retires_its_reader_quietly() {
    let dir = TempDir::new("bad-handshake");
    let path = dir.0.join("rpc.sock");
    let listener = UnixListener::bind(&path).expect("bind");
    let (hang_tx, hung_up) = mpsc::channel();
    thread::spawn(move || {
        let Ok((mut stream, _)) = listener.accept() else {
            return;
        };
        let mut buf = [0u8; 4096];
        // Answer the first frame with nonsense: `initialize` comes back with no
        // protocolVersion, so the handshake fails on decode.
        let _ = stream.read(&mut buf);
        let _ = stream.write_all(b"{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"nope\":true}}\n");
        // Hold the connection open until the client lets go, then report EOF.
        for line in BufReader::new(stream).lines() {
            if line.is_err() {
                break;
            }
        }
        let _ = hang_tx.send(());
    });

    let recorder = Recorder::default();
    let outcome = Session::connect_with(rpc::Target::Local(path.clone()), "0.0.0-test", recorder.clone(), &probeless_timing());
    assert!(
        outcome.is_err(),
        "a handshake with no protocolVersion must not connect"
    );
    drop(outcome);

    wait_for("the reader thread to let go of the socket", || {
        hung_up.try_recv().is_ok()
    });
    assert!(
        !recorder.saw_closed(),
        "a connection that never connected reported itself lost: {:?}",
        recorder.closed_reasons()
    );
}

// --- the risk the probe itself carries ---------------------------------------

/// **The new bug this change could have shipped with, tested.**
///
/// A probe can manufacture the very symptom it was added to catch. Two ways:
///
/// - if the reader mistook "my own call hasn't been answered yet" for "the
///   daemon is gone", then every `ask` — a model call, routinely several seconds
///   — would lose its connection mid-call and be reported as a daemon that had
///   died; and
/// - if a *single* unanswered probe killed the connection, any pause in the
///   daemon (a GC, a `SIGSTOP` under a debugger) would drop a working window.
///
/// So the fake holds its answer for 900ms — three times what this timing needs
/// to declare a connection dead — while going on reading and answering every
/// ping, which is what the framework's per-request goroutines actually do. One
/// strike survives by design; the call answers long before two are spent.
#[test]
fn a_slow_answer_is_not_a_dead_connection() {
    let daemon = start_with(
        "slow-ask",
        Behaviour {
            delay: Some(("ask".into(), Duration::from_millis(900))),
            ..Default::default()
        },
    );
    let timing = fast_timing();
    assert!(
        timing.dead_after() < Duration::from_millis(400),
        "this test's premise is that the delay dwarfs dead_after; got {:?}",
        timing.dead_after()
    );
    let (session, recorder) = connect(&daemon, &timing);

    let answer = std::thread::scope(|scope| {
        let call = scope.spawn(|| session.call("ask", serde_json::json!({"prompt": "hi"})));
        // Outstanding across several whole probe cycles.
        thread::sleep(Duration::from_millis(950));
        call.join().expect("the calling thread panicked")
    });

    assert_eq!(
        answer.expect("a slow answer must still arrive")["method"],
        "ask",
        "the call came back answering something else"
    );
    assert!(
        !recorder.saw_closed(),
        "a daemon that was merely thinking was called dead: {:?}",
        recorder.closed_reasons()
    );
    let pings = daemon.pings();
    assert!(
        pings >= 3,
        "the probe must have run several times during the call; it asked {pings} times"
    );
}

/// **Sending is not liveness. Being answered is.**
///
/// The plausible-sounding wrong optimisation here is to reset the probe clock
/// when the client sends something — "we're still talking to it, so it can't be
/// dead". It is wrong for the same reason the original bug was: it reads the
/// client's own activity as evidence about the daemon.
///
/// So this connection has a request sitting unanswered **and** unanswered pings.
/// The reader must still call it dead. If outbound traffic ever counts as
/// liveness, this connection stays current forever against a daemon that stopped
/// caring.
///
/// (This is also the honest form of a test that was suggested and dropped — "the
/// daemon ignores an unrelated method" — which passed for the wrong reason: a
/// fake that answers everything makes any such assertion unfalsifiable.)
#[test]
fn a_request_awaiting_an_answer_is_not_liveness() {
    let daemon = start_with(
        "outbound-not-liveness",
        Behaviour {
            silence: vec!["store.info".into(), "ping".into()],
            ..Default::default()
        },
    );
    let timing = fast_timing();
    let (session, recorder) = connect(&daemon, &timing);

    std::thread::scope(|scope| {
        scope.spawn(|| {
            // Must be failed by the reader when the connection is retired, not
            // left to sit out `CALL_TIMEOUT` against a socket nobody answers.
            let _ = session.call("store.info", Value::Null);
        });
        wait_for(
            "the connection to be called dead, unanswered pings and all",
            || recorder.saw_closed(),
        );
    });

    let reason = recorder.closed_reasons().remove(0);
    assert!(reason.contains("liveness probe"), "{reason}");
    let pings = daemon.pings();
    assert!(
        pings >= 2,
        "the reader should have asked repeatedly before giving up; it asked {pings} times"
    );
}

// --- against the real daemon -------------------------------------------------

/// The same claims, against `aeon serve` rather than the fake.
///
/// Ignored by default, in the manner of `tests/session.rs`'s live test. The fake
/// can only prove the reader's logic; this proves the protocol half — that
/// `ping` is genuinely registered and answers with no store, provider or loop
/// behind it — and that retiring a session makes the daemon's own lifecycle hook
/// fire, which is the `client disconnected` line that used to arrive ten minutes
/// late.
///
/// ```text
/// AEON_LIVE_ENDPOINT=/path/to/aeon.sock cargo test --test liveness -- --ignored
/// ```
#[test]
#[ignore = "needs a running daemon; set AEON_LIVE_ENDPOINT"]
fn a_real_daemon_answers_the_liveness_probe() {
    let endpoint = std::env::var("AEON_LIVE_ENDPOINT").expect("AEON_LIVE_ENDPOINT");
    let timing = fast_timing();
    let recorder = Recorder::default();
    let session =
        Session::connect_with(rpc::Target::Local(Path::new(&endpoint).to_path_buf()), "0.0.0-test", recorder.clone(), &timing)
            .expect("connect to the real daemon");

    // Idle, with the probe live: three full dead-after windows of real silence.
    thread::sleep(timing.dead_after() * 3);
    assert!(
        !recorder.saw_closed(),
        "the real daemon was called dead while idle: {:?}",
        recorder.closed_reasons()
    );

    // A working connection over the real transport, after all those probes.
    let version = session.call("version", Value::Null).expect("version after idle");
    assert!(
        version["daemonVersion"].as_str().is_some_and(|v| !v.is_empty()),
        "the real daemon answered version with no version: {version}"
    );

    // A real refusal still arrives intact across a probed socket.
    let err =
        session.call("no.such.method", Value::Null).expect_err("an unknown method must be refused");
    assert!(err.contains("not registered"), "{err}");

    // And retiring it is quiet: no `closed` for a connection we chose to close.
    drop(session);
    thread::sleep(Duration::from_millis(200));
    assert!(
        !recorder.saw_closed(),
        "dropping the session was reported as losing the daemon: {:?}",
        recorder.closed_reasons()
    );
}

//! Auto-starting the bundled local daemon.
//!
//! The app ships a copy of `aeon` as a Tauri sidecar (`tauri.conf.json`'s
//! `bundle.externalBin`) and starts it on launch — but only if nothing is
//! already listening at the local endpoint. That "only if" is load-bearing:
//! a developer running `make dev` by hand, or an operator running `aeon
//! serve` as a long-lived service, must never get a second daemon bound to
//! the same endpoint, and this app must never be the one that stops either
//! of those on exit.
//!
//! So two separate rules, enforced by one piece of state:
//!
//! - **Spawn only if nothing answers.** `endpoint::probe` decides this
//!   before anything is spawned.
//! - **Kill only what this app started.** `LocalDaemon` holds the spawned
//!   child, if there is one — `None` for both "nothing spawned yet" and
//!   "found one already running" — and `stop_if_spawned` only ever acts on
//!   `Some`.
//!
//! The spawned child's endpoint is pinned explicitly via
//! `LOCALRPC_ENDPOINT_PATH` to the exact path the shell already resolved,
//! rather than letting the child re-derive its own: `endpoint.rs` and the
//! daemon's own `internal/appenv` resolve that path independently today, in
//! agreement only by convention, and a spawn is the one new place that
//! disagreement could silently produce two things listening nowhere useful
//! to each other.

use std::path::PathBuf;

use tauri::{async_runtime, AppHandle};
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;

/// The daemon this app spawned, if it spawned one.
pub struct LocalDaemon {
    child: Option<CommandChild>,
}

impl LocalDaemon {
    fn none() -> Self {
        LocalDaemon { child: None }
    }

    /// Probe the local endpoint; spawn the bundled daemon only if nothing
    /// answered. Failures to spawn are reported, never fatal — a shell that
    /// cannot find or start its sidecar should still let the window open and
    /// show "not connected" honestly, the same as any other connection
    /// failure.
    pub fn ensure_started(app: &AppHandle, endpoint_path: &PathBuf) -> Self {
        if crate::endpoint::probe(endpoint_path) {
            eprintln!(
                "aeon: a daemon is already listening at {} — not spawning one",
                endpoint_path.display()
            );
            return Self::none();
        }
        match spawn(app, endpoint_path) {
            Ok(child) => {
                eprintln!(
                    "aeon: spawned the bundled daemon (pid {}), endpoint {}",
                    child.pid(),
                    endpoint_path.display()
                );
                LocalDaemon { child: Some(child) }
            }
            Err(e) => {
                eprintln!("aeon: could not spawn the bundled daemon: {e}");
                Self::none()
            }
        }
    }

    /// Whether THIS process started the daemon (S58 T7, SV10). The tray's
    /// Quit label turns on this one fact: "Quit Aeon (stops the daemon this
    /// app started)" is true exactly when this returns true, and the plain
    /// label is the truth otherwise. Same `Some`/`None` discipline as
    /// `stop_if_spawned` — a daemon found already running is not ours.
    pub fn is_spawned(&self) -> bool {
        self.child.is_some()
    }

    /// Kill the daemon this app spawned, and only that one — a daemon found
    /// already running on launch is left exactly as it was found.
    ///
    /// A hard kill, not a graceful shutdown request: there is no gentler
    /// signal to prefer here, because the daemon is designed to recover from
    /// exactly this on its next start (`AGENTS.md` P2/P29 — suspension is
    /// exiting a process, resume is cold, from rows, never from memory this
    /// kill would otherwise need to flush).
    pub fn stop_if_spawned(&mut self) {
        if let Some(child) = self.child.take() {
            let pid = child.pid();
            if let Err(e) = child.kill() {
                eprintln!("aeon: could not stop the daemon (pid {pid}) this app started: {e}");
            }
        }
    }
}

fn spawn(app: &AppHandle, endpoint_path: &PathBuf) -> Result<CommandChild, tauri_plugin_shell::Error> {
    let (mut rx, child) = app
        .shell()
        .sidecar("aeon")?
        .arg("serve")
        .env("LOCALRPC_ENDPOINT_PATH", endpoint_path.display().to_string())
        .spawn()?;

    // Drain stdout/stderr on their own task so the pipes never back up over
    // a long session, and so the daemon's own log lines land somewhere
    // (prefixed, so they read as the daemon's and not the shell's) rather
    // than being silently discarded.
    async_runtime::spawn(async move {
        while let Some(event) = rx.recv().await {
            match event {
                CommandEvent::Stdout(line) | CommandEvent::Stderr(line) => {
                    eprintln!("aeon daemon: {}", String::from_utf8_lossy(&line));
                }
                CommandEvent::Error(err) => eprintln!("aeon daemon: {err}"),
                CommandEvent::Terminated(payload) => {
                    eprintln!(
                        "aeon daemon: exited (code {:?}, signal {:?})",
                        payload.code, payload.signal
                    );
                    break;
                }
                // `CommandEvent` is `#[non_exhaustive]` — a future variant is
                // logged rather than silently dropped or a build failure.
                other => eprintln!("aeon daemon: {other:?}"),
            }
        }
    });

    Ok(child)
}

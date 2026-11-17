//! Where the daemon's endpoint is.
//!
//! **Resolved the same way `serve` resolves it** (T3), including the platform
//! differences — because an app that looks somewhere else is an app that
//! reports "not connected" while the daemon is running, and the path it tried
//! is the only thing that would reveal why.
//!
//! This duplicates `internal/endpoint.Path` deliberately: the Rust shell cannot
//! call Go. **If the resolution rules ever diverge, this is the bug**, which is
//! why both sides print the path they used.

use std::env;
use std::path::PathBuf;

const ENDPOINT_PATH_ENV: &str = "LOCALRPC_ENDPOINT_PATH";
const NAMESPACE_ENV: &str = "LOCALRPC_APP_NAMESPACE";
const DEFAULT_NAMESPACE: &str = "aeon";
const DEFAULT_NAME: &str = "rpc";

/// Resolve the endpoint path, mirroring the daemon's own resolution.
pub fn path() -> PathBuf {
    if let Ok(explicit) = env::var(ENDPOINT_PATH_ENV) {
        let trimmed = explicit.trim();
        if !trimmed.is_empty() {
            return PathBuf::from(trimmed);
        }
    }

    let ns = env::var(NAMESPACE_ENV)
        .ok()
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| DEFAULT_NAMESPACE.to_string());

    platform_path(&ns)
}

#[cfg(windows)]
fn platform_path(ns: &str) -> PathBuf {
    // A named pipe, not a file. There is nothing to stat.
    PathBuf::from(format!(r"\\.\pipe\{}-{}", ns, DEFAULT_NAME))
}

/// Is something already listening at the local endpoint?
///
/// A connect-and-drop, not a handshake — it exists only to decide whether the
/// app needs to spawn its own bundled daemon (`main.rs`'s `setup`). A
/// developer's own `make dev` daemon must be found exactly this way and left
/// alone: a second daemon bound to the same endpoint is a bug, not a feature.
/// Success says only that a peer accepted the connection; it says nothing
/// about what that peer is, which is why `Session::connect`'s full handshake
/// still runs either way, right after.
pub fn probe(path: &PathBuf) -> bool {
    #[cfg(not(windows))]
    {
        std::os::unix::net::UnixStream::connect(path).is_ok()
    }
    #[cfg(windows)]
    {
        std::fs::OpenOptions::new()
            .read(true)
            .write(true)
            .open(path)
            .is_ok()
    }
}

#[cfg(not(windows))]
fn platform_path(ns: &str) -> PathBuf {
    // XDG_RUNTIME_DIR is where a per-user socket belongs: it is already
    // owner-only and the kernel clears it on logout, so a stale endpoint does
    // not outlive the session.
    if let Ok(run) = env::var("XDG_RUNTIME_DIR") {
        if !run.trim().is_empty() {
            return PathBuf::from(run)
                .join(ns)
                .join(format!("{}.sock", DEFAULT_NAME));
        }
    }
    // The documented fallback, for a session with no runtime dir.
    let home = env::var("HOME").unwrap_or_else(|_| "/tmp".to_string());
    PathBuf::from(home)
        .join(".local/state")
        .join(ns)
        .join(format!("{}.sock", DEFAULT_NAME))
}

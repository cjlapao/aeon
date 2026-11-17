//! The stand-in `background_tray` for test crates that `#[path]`-include
//! `src/session.rs`.
//!
//! `AppSink::event` pokes the tray through `crate::background_tray`, and the
//! real module cannot be included here: it refers to `crate::AppState`, which
//! exists only in a running app — which is exactly what the header of
//! `src/background_tray.rs` says about itself. None of these tests construct
//! an `AppSink` (they hand `Session::connect` a recording sink), so this is
//! never called; it exists so `session.rs`'s one reference resolves in a test
//! crate the same way `mod rpc` and `mod endpoint` do.

#[allow(dead_code)] // referenced by the included session.rs, never by a test
pub fn poke_tray(_app: &tauri::AppHandle) {}

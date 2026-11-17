// The Rust shell.
//
// # What it is for, and what it is not
//
// The web layer cannot open a unix socket, so every byte to the daemon goes
// through here. S9 builds the real app; this is the bridge plus the **test
// bench** surfaces each slice adds as it goes — which is why the commands below
// are generic rather than one per method: a slice that adds a protocol method
// should not have to add Rust.
//
// # Every command is async — no exceptions, including the cheap ones
//
// A synchronous `#[tauri::command]` runs on the MAIN thread: the one thread
// that paints the window and dispatches every click. That is not a style
// preference, it is measured — S72 T5 found `seed.status` answering in 6.2 s
// on the main thread, and for those 6.2 s the app was a picture of an app: no
// navigation landed, no menu answered, no other page could be opened while
// one page's spinner turned. The same call as an `async fn` runs on
// `tauri::async_runtime` and the window behaves normally throughout.
//
// So the rule is **every command is `async fn`**, and it holds for the reads
// that only touch memory too. The asymmetry is what decides it: being async
// costs nothing here — the web layer already `await`s every command
// (`ui/src/lib/bridge.ts`), so a sync→async change on this side is invisible
// to the caller and needs no JS change at all — while being sync costs a
// frozen window the moment whatever is behind the command stops being free.
// A command written sync today is a freeze on the day someone makes it do
// real work, and the freeze is the expensive part to find.
//
// Two consequences of the rule, both mechanical:
//
// - An async command that takes a reference (`State`, `AppHandle` by ref)
//   **must return `Result`** — Tauri's macro refuses the other shape. The
//   memory-only reads below therefore return `Result<T, String>` whose `Err`
//   arm they never take; it is the required shape, not a failure claimed.
// - Anything that genuinely BLOCKS — the socket wait, a disk write, a lock a
//   worker may hold — goes inside `tauri::async_runtime::spawn_blocking`,
//   because an async command's thread is a tokio worker that must not sit on
//   a descriptor. What may stay directly in the async body is a memory read
//   that cannot block, and that only.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod background;
mod background_tray;
mod component_files;
mod endpoint;
mod media_permissions;
mod local_daemon;
mod rpc;
mod session;
mod workspace;

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc;
use std::sync::{Arc, Mutex};

use serde_json::Value;
use tauri::{AppHandle, Emitter, Manager, State};
use tauri_plugin_deep_link::DeepLinkExt;

use local_daemon::LocalDaemon;
use session::{AppSink, Negotiated, Session};
use workspace::{Workspace, WorkspaceStore};

/// The app's own version, stamped by Cargo.
///
/// Sent as `clientVersion` in the handshake — it is what another client sees in
/// `client.connected`, and it is NOT what the window displays as the daemon's
/// version. Those being different things is the point of S2's T7.
const APP_VERSION: &str = env!("CARGO_PKG_VERSION");

/// The one session, shared by the window; which workspace it's open against;
/// and the bundled local daemon, if this app process is the one that
/// started it (`local_daemon.rs`).
struct AppState {
    /// An `Arc` around the mutex because a command that has to WAIT on the
    /// daemon must move the session slot into a blocking worker, and borrowed
    /// app state cannot cross into a spawned task — see `connect` and
    /// `rpc_call` for what that wait is worth keeping off the main thread.
    session: Arc<Mutex<Option<Session>>>,
    /// Whether the slot above currently holds a session — **the same fact,
    /// readable without taking the mutex**, and that is the whole reason it
    /// exists. The window's close handler needs it, and a window-event handler
    /// runs on the main thread with no async form to move it out of: taking the
    /// slot's mutex there would mean a click on the X waits for however long
    /// the worker holding that mutex is waiting — which after S72 T5 is exactly
    /// the freeze this file's rule exists to prevent, arriving through a door
    /// that is not a command. `connect` and `disconnect` are the only two
    /// places the slot changes, and both write this beside the change.
    session_live: Arc<AtomicBool>,
    workspaces: Arc<Mutex<WorkspaceStore>>,
    local_daemon: Mutex<LocalDaemon>,
    // S58 T7 (SV9/SV10): the background half. `quitting` is set ONLY by the
    // tray's Quit, and it is what makes a closing window Close rather than
    // Hide for the rest of this process. `background_presence` is "tray" or
    // "unavailable" — the honest record of whether a tray host took the
    // builder, and `should_refresh` reads it so a headless session never
    // dials a tray that is not there. `daemon_ours` is the tray Quit label's
    // whole truth: this app started the daemon, or it did not. `tray_poke`
    // carries the one-slot "something changed" beat from the event sink and
    // the window-hide to the poller thread — the session reader thread must
    // never make a call of its own.
    quitting: AtomicBool,
    background_presence: Mutex<&'static str>,
    daemon_ours: AtomicBool,
    // The attention dot's single input, and the record of what is CURRENTLY
    // applied. `web_unread` is the notification store's unread count as last
    // reported by the web layer (`tray_unread` below — the bell's number,
    // mirrored onto the icon; reading them all always takes the dot away —
    // the operator's ruling after walking the first cut, which the daemon's
    // inbox count could hold lit over a fully-read bell). `tray_badge` is the
    // state applied to the icon, edge-triggering every repaint so a burst
    // never re-sets the OS icon (icon churn reads as flicker on some hosts),
    // and written only after a `set_icon` that actually succeeded — a failed
    // paint leaves the record honest, so the next refresh retries.
    web_unread: std::sync::atomic::AtomicU32,
    tray_badge: AtomicBool,
    tray_poke: Mutex<mpsc::SyncSender<()>>,
}

/// Connect and negotiate, replacing any existing session.
///
/// Returns the DAEMON's version and the NEGOTIATED protocol version. An app
/// showing its own numbers proves the window rendered and nothing about the
/// socket.
///
/// The old session is retired **before** the new one is opened, not after.
/// `Session`'s reader thread holds its own duplicated descriptor, so dropping a
/// replaced session closed nothing on its own: the daemon kept seeing a client,
/// and the window that replaced it inherited a connection nobody owned. That is
/// where the second `client connected` line in the daemon's log came from, and
/// why a later `aeon://closed` could arrive for a connection the app had already
/// thrown away. Retiring first means one window is one connection, and the
/// daemon's log reads as the app behaves.
///
/// # Why this waits off the main thread
///
/// A connect is a `connect(2)`, a handshake of two calls, and a workspace that
/// points at a host that is not answering — a dead remote in `host:port` form
/// — which is seconds of kernel-level waiting. A synchronous command runs on
/// the main thread, so that wait is a window that paints nothing and answers
/// no clicks. The session slot is therefore locked **across** the whole
/// connect on the worker: the retiring-then-opening order above is a
/// two-step, and on the main thread the old command boundary gave it for
/// free. Holding the lock is what keeps that order now that two connects can
/// genuinely be in flight.
#[tauri::command]
async fn connect(state: State<'_, AppState>, app: AppHandle) -> Result<Negotiated, String> {
    let slot = Arc::clone(&state.session);
    let live = Arc::clone(&state.session_live);
    let workspaces = Arc::clone(&state.workspaces);
    tauri::async_runtime::spawn_blocking(move || {
        let target = workspaces.lock().unwrap().active_target(endpoint::path());
        let mut slot = slot.lock().unwrap();
        let previous = slot.take();
        live.store(false, Ordering::SeqCst);
        drop(previous);
        let session = Session::connect(target, APP_VERSION, AppSink(app))?;
        let negotiated = session.negotiated();
        *slot = Some(session);
        live.store(true, Ordering::SeqCst);
        Ok(negotiated)
    })
    .await
    .map_err(|e| format!("the connect task did not finish: {e}"))?
}

/// Call any protocol method.
///
/// # Why one generic command rather than one per method
///
/// S9's U11 asks for exactly this: the method table lives in `internal/protocol`
/// and **nowhere else**. A command per method would be a second copy of the
/// table, in Rust, that every later slice has to remember to update — and the
/// one somebody forgets is the method that silently cannot be called from the
/// app.
///
/// The cost is that the web layer names methods as strings. That is the right
/// trade: a typo there is a `-32601` from the daemon, which is a clear failure,
/// where a missing Rust command is a feature that quietly does not exist.
///
/// # Why this runs on a worker, not on the main thread
///
/// `SessionHandle::call` parks its thread until the daemon answers — and the
/// daemon answers when the work is done, not when the page would like it.
/// A synchronous command runs on the main thread, so a slow method used to
/// stop the whole app: the Configuration pages that read a set's status
/// (seconds of real work) froze the window for their duration, which reads
/// exactly like a crash and cannot be navigated away from (S72, walked
/// 2026-11-08). An `async` command runs on the async runtime, and the wait is
/// then put where it belongs — a blocking worker — so the window keeps
/// painting, the menu keeps responding, and the user can go elsewhere while
/// the page waits.
///
/// Everything blocking lives inside the one `spawn_blocking`: the session
/// mutex too, not just the socket wait. A `std::sync::Mutex` acquired on an
/// async worker is a parked worker, and a connect to an unreachable host
/// holds that mutex for the length of its handshake — which would trade a
/// frozen window for a starved runtime. Inside the worker, a wait is just a
/// wait. And because the call itself holds no lock once the handle is cloned,
/// one slow method no longer queues every other caller behind it.
#[tauri::command]
async fn rpc_call(state: State<'_, AppState>, method: String, params: Value) -> Result<Value, String> {
    let slot = Arc::clone(&state.session);
    tauri::async_runtime::spawn_blocking(move || {
        let handle = {
            let guard = slot.lock().unwrap();
            guard
                .as_ref()
                .ok_or_else(|| "not connected to the daemon — call `connect` first".to_string())?
                .handle()
        };
        handle.call(&method, params)
    })
    .await
    .map_err(|e| format!("the call task did not finish: {e}"))?
}

/// Drop the session. Its subscriptions die with the connection (T5), so this is
/// also how the UI stops a stream.
///
/// Deliberately silent: a connection this app closed is not one it lost, and
/// `aeon://closed` is the emit that tells the window to go to "not connected".
/// `Session::drop` retires the reader without it.
///
/// Async for one reason only: the slot may be held by a connect still
/// handshaking, and this command takes that mutex (S72 — the main thread waits
/// for nothing). The `Result` is Tauri's rule for an async command that takes
/// a borrow (`State`), not a new failure the web layer must handle — the only
/// way this one fails is a worker that vanished, and `bridge.ts` already
/// treats a disconnect as something that cannot meaningfully fail.
#[tauri::command]
async fn disconnect(state: State<'_, AppState>) -> Result<(), String> {
    let slot = Arc::clone(&state.session);
    let live = Arc::clone(&state.session_live);
    tauri::async_runtime::spawn_blocking(move || {
        let previous = slot.lock().unwrap().take();
        live.store(false, Ordering::SeqCst);
        drop(previous);
    })
    .await
    .map_err(|e| format!("the disconnect task did not finish: {e}"))
}

/// Where the shell looked — the active workspace's target, local path or
/// `host:port`.
///
/// A separate command so the disconnected state can name the path **even when
/// the handshake failed before reporting one**. It is the first thing anyone
/// debugging needs, and the reason the daemon prints its own path at startup —
/// so the two can be compared without reading source.
///
/// Async under the rule at the top of this file, not because it waits: the
/// store is loaded at startup and `active_target` formats a string from it, so
/// there is nothing here to move to a worker — no `spawn_blocking`, and the
/// `Ok` is the only arm the lock can take.
#[tauri::command]
async fn endpoint_path(state: State<'_, AppState>) -> Result<String, String> {
    Ok(state
        .workspaces
        .lock()
        .unwrap()
        .active_target(endpoint::path())
        .to_string())
}

/// Whether the background half is running (S58 T7): "tray" when the tray
/// took, "unavailable" when there was no tray host. The app's doctor panel
/// reads THIS — the daemon's `aeon doctor` cannot know what the shell's
/// process did or did not build, and inventing a daemon-side line for it
/// would be a lie with a nicer layout.
///
/// Async for the rule, same as `endpoint_path`: it reads one `&'static str`
/// that was decided once at startup.
#[tauri::command]
async fn background_presence(state: State<'_, AppState>) -> Result<String, String> {
    // `to_string` on the guard (which derefs to the recorded word), not on a
    // deref of the String that is not there.
    Ok(state.background_presence.lock().unwrap().to_string())
}

/// Report the app's unread-notification count — the number the bell wears —
/// so the tray icon can wear the same truth. A DISPLAY mirror, not a door:
/// it reads no daemon state, calls nothing, and can decide only the dot;
/// the inbox half of the dot stays the poller's, asked over the one session.
/// The web layer reports on every unread-count change and once on mount (a
/// reload re-syncs a shell that never saw the pushes).
///
/// ASYNC on purpose: `set_icon` is main-thread-dispatched and WAITS on the
/// event loop, so it may never be called from the loop's own thread. Async
/// commands run off it (Tauri's own tray commands are async for the same
/// reason); a sync command here would deadlock the app on the first dot.
#[tauri::command]
async fn tray_unread(app: AppHandle, unread: u32) {
    app.state::<AppState>().web_unread.store(unread, Ordering::Relaxed);
    background_tray::refresh_badge(&app);
}

/// Every workspace this app knows about — `Local` first, then every saved
/// remote one.
///
/// Async for the rule, not for the work: the store is read into memory at
/// startup, so this is a clone of a `Vec` under a lock and the `Err` arm is
/// shape, not possibility. It is on every render, which is the other half of
/// why the window must not be the one answering it.
#[tauri::command]
async fn workspace_list(state: State<'_, AppState>) -> Result<Vec<Workspace>, String> {
    Ok(state.workspaces.lock().unwrap().list())
}

/// The currently active workspace. Async for the same two reasons as
/// `workspace_list`.
#[tauri::command]
async fn workspace_active(state: State<'_, AppState>) -> Result<Workspace, String> {
    Ok(state.workspaces.lock().unwrap().active())
}

/// Save a new remote workspace. Does not switch to it — that's a separate,
/// deliberate `workspace_select` (and the hard reload it implies on the web
/// side), not a side effect of adding one.
///
/// ASYNC because it is a `create_dir_all` and a `write` (WorkspaceStore::save)
/// — real syscalls, on a path that may sit on a network mount, so the work
/// goes to the blocking pool rather than staying in the async body the way the
/// memory-only `workspace_list` and `workspace_active` above do.
#[tauri::command]
async fn workspace_add(state: State<'_, AppState>, name: String, host: String, port: u16) -> Result<Workspace, String> {
    let store = Arc::clone(&state.workspaces);
    tauri::async_runtime::spawn_blocking(move || store.lock().unwrap().add(name, host, port))
        .await
        .map_err(|e| format!("the workspace task did not finish: {e}"))?
}

/// Remove a saved remote workspace. Refused for `local`. Removing the active
/// one falls back to `local` — see `WorkspaceStore::remove`.
#[tauri::command]
async fn workspace_remove(state: State<'_, AppState>, id: String) -> Result<(), String> {
    let store = Arc::clone(&state.workspaces);
    tauri::async_runtime::spawn_blocking(move || store.lock().unwrap().remove(&id))
        .await
        .map_err(|e| format!("the workspace task did not finish: {e}"))?
}

/// Mark a workspace active. Does not reconnect — the web layer does that
/// itself (a hard reload), the same way it always decides when to connect.
#[tauri::command]
async fn workspace_select(state: State<'_, AppState>, id: String) -> Result<Workspace, String> {
    let store = Arc::clone(&state.workspaces);
    tauri::async_runtime::spawn_blocking(move || store.lock().unwrap().select(&id))
        .await
        .map_err(|e| format!("the workspace task did not finish: {e}"))?
}

fn main() {
    // GNOME/Wayland matches a running window to its desktop file by app ID.
    // Set the program name without enabling GTK's single-instance app ID:
    // the QA walk deliberately opens two Aeon windows at once.
    #[cfg(target_os = "linux")]
    glib::set_prgname(Some(if cfg!(debug_assertions) {
        "dev.cjlapao.aeon.Dev"
    } else {
        "dev.cjlapao.aeon"
    }));

    let app = tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_notification::init())
        // S58 T7: the `aeon://` scheme. Handled Rust-side only — the web
        // layer gains no capability; `listen` already covers the two emits
        // (`tauri.conf.json` carries the scheme entry for the bundlers).
        .plugin(tauri_plugin_deep_link::init())
        .setup(|app| {
            // Orion's microphone: let the page open an audio-only capture (Linux webview only; see media_permissions.rs).
            if let Some(window) = app.get_webview_window("main") {
                media_permissions::allow_audio_capture(&window);
            }
            // Resolving the app's own config directory needs a running app
            // (it isn't available at `AppState::default()` time), so the
            // workspace store — and the state it lives in — is built here,
            // once, before any command can run.
            let workspaces_path = app
                .path()
                .app_config_dir()
                .map_err(|e| format!("resolve app config dir: {e}"))?
                .join("workspaces.json");
            // The bundled daemon starts here, independent of which workspace
            // ends up active — its lifecycle is the app's, not the
            // workspace selection's (S41).
            let local_daemon = LocalDaemon::ensure_started(app.handle(), &endpoint::path());
            let daemon_ours = local_daemon.is_spawned();
            let (poke_tx, poke_rx) = mpsc::sync_channel::<()>(1);
            app.manage(AppState {
                session: Arc::new(Mutex::new(None)),
                session_live: Arc::new(AtomicBool::new(false)),
                workspaces: Arc::new(Mutex::new(WorkspaceStore::load(workspaces_path))),
                local_daemon: Mutex::new(local_daemon),
                quitting: AtomicBool::new(false),
                // Decided once, below, and read ever after: "tray" or
                // "unavailable". No third state — a tray that half-built is
                // a tray that does not exist.
                background_presence: Mutex::new("unavailable"),
                daemon_ours: AtomicBool::new(daemon_ours),
                web_unread: std::sync::atomic::AtomicU32::new(0),
                tray_badge: AtomicBool::new(false),
                tray_poke: Mutex::new(poke_tx),
            });
            // The tray is worth having and never worth failing for: where no
            // tray host exists (most Linux sessions) the app behaves exactly
            // as it did before S58 T7, and says so in the record.
            match background_tray::build_tray(app.handle(), daemon_ours) {
                Ok(()) => {
                    *app.state::<AppState>().background_presence.lock().unwrap() = "tray";
                    background_tray::start_poller(app.handle().clone(), poke_rx);
                }
                Err(e) => eprintln!(
                    "aeon: no system tray — background presence unavailable: {e}"
                ),
            }
            // Deep links: show, then navigate the window with the CANONICAL
            // form of an accepted target; a refused link is refused by fixed
            // reason word, never quoted back. The window's `listen` (already
            // granted) is the whole web-layer half — nothing new crosses.
            let link_app = app.handle().clone();
            let refuse_app = app.handle().clone();
            app.handle().deep_link().on_open_url(move |event| {
                for url in event.urls() {
                    match background::classify_link(url.as_str()) {
                        Ok(target) => {
                            background_tray::show_main(&link_app);
                            let _ = link_app.emit("aeon://navigate", target.wire());
                        }
                        Err(refusal) => {
                            let _ = refuse_app.emit("aeon://navigate-refused", refusal.reason());
                        }
                    }
                }
            });
            Ok(())
        })
        // The close question (SV9), asked of the pure policy: a live session
        // that the person did not quit means hide-to-tray; everything else
        // falls through to the normal exit — whose RunEvent::Exit handler is
        // unchanged below and remains the ONLY place a daemon is stopped.
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                if window.label() != "main" {
                    return;
                }
                let state = window.state::<AppState>();
                // The mirror, not the mutex. This handler runs on the main
                // thread with no async form to run on, and the slot's mutex is
                // held across a whole `connect` — a dead remote is seconds of
                // kernel waiting. Reading the flag answers the same question
                // without that wait; the flag is false for exactly the seconds
                // the mutex would have returned `None` anyway.
                let has_live_session = state.session_live.load(Ordering::SeqCst);
                let quitting = state.quitting.load(Ordering::Relaxed);
                if background::close_policy(has_live_session, quitting) == background::ClosePolicy::Hide {
                    api.prevent_close();
                    let _ = window.hide();
                    // The count the person would have seen in the window is
                    // now the tray's job: one poke, coalesced.
                    let _ = state.tray_poke.lock().unwrap().try_send(());
                }
            }
        })
        .invoke_handler(tauri::generate_handler![
            connect,
            rpc_call,
            disconnect,
            endpoint_path,
            workspace_list,
            workspace_active,
            workspace_add,
            workspace_remove,
            workspace_select,
            background_presence,
            tray_unread,
            component_files::component_list,
            component_files::component_read
        ])
        .build(tauri::generate_context!())
        .expect("error while building the Aeon shell");

    app.run(|app_handle, event| {
        // The one guaranteed, non-cancelable point in the event loop's life
        // — unlike `ExitRequested`, which can be prevented and could in
        // principle fire more than once. Stopping the daemon this app
        // started belongs at the point exit is actually happening, once.
        if let tauri::RunEvent::Exit = event {
            app_handle
                .state::<AppState>()
                .local_daemon
                .lock()
                .unwrap()
                .stop_if_spawned();
        }
    });
}

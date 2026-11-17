// The tauri plumbing of the background half (S58 T7). The DECISIONS live in
// `background.rs` (pure, unit-tested without a display); everything in this
// file exists to feed those decisions to the OS and to act on them: the tray
// builder and its menu, the attention-dot painter, the window-show path, the
// count poller, and the event-sink poke. Split from the pure half precisely so
// `tests/background_policy.rs` can `#[path]`-include `background.rs` the way
// every integration test in this crate includes what it tests — the plumbing
// refers to `crate::AppState` and to tauri types, which no test crate can
// compile on its own.
//
// The one rule this file obeys above the rest: NO call to the daemon happens
// on the session's reader thread. The sink pokes a one-slot channel; this
// module's poller thread is the only caller, and `Session::call` from any
// other thread is what it was built for.

use std::sync::mpsc::Receiver;
use std::sync::atomic::Ordering;
use std::time::Duration;

use tauri::menu::{Menu, MenuBuilder, MenuItem, PredefinedMenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIcon, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Emitter, Manager};

use crate::background::{badge_dot, tray_badge, tray_items};

/// The tray's stable id — the handle the count refresh looks up by.
pub const TRAY_ID: &str = "aeon-tray";

/// Show the window and focus it — the ONE way back in (tray click, menu
/// item, deep link all route through here, so "bring me there" is one act).
pub fn show_main(app: &AppHandle) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.set_focus();
    }
}

/// The menu, built from the pure list plus a separator before Quit. Mixed
/// item kinds (items and a separator) ride `MenuBuilder` because the static
/// type of each row differs — the builder pushes each into one `Menu`.
fn build_menu(app: &AppHandle, inbox: Option<u32>, daemon_is_ours: bool) -> tauri::Result<Menu<tauri::Wry>> {
    let mut builder = MenuBuilder::new(app);
    let items = tray_items(inbox, daemon_is_ours);
    // The separator lives between the count line and Quit; the pure list
    // stays four lines (its test is about LABELS, not pixels).
    let mut parts: Vec<(&'static str, String, bool)> = Vec::with_capacity(items.len());
    for item in &items {
        parts.push(item.parts());
    }
    let mut built = Vec::new();
    for (id, label, enabled) in &parts {
        built.push(MenuItem::with_id(app, *id, label.clone(), *enabled, None::<&str>)?);
    }
    for b in &built[..built.len() - 1] {
        builder = builder.item(b);
    }
    builder = builder.item(&PredefinedMenuItem::separator(app)?);
    builder = builder.item(&built[built.len() - 1]);
    builder.build()
}

/// Build the tray. FAILURE IS REPORTED AND NON-FATAL (the caller in `main.rs`
/// logs it and records `background_presence = "unavailable"`): where there is
/// no app-indicator host — most Linux sessions included — the app behaves
/// exactly as it did before this file existed.
pub fn build_tray(app: &AppHandle, daemon_is_ours: bool) -> tauri::Result<()> {
    let menu = build_menu(app, None, daemon_is_ours)?;
    let mut builder = TrayIconBuilder::with_id(TRAY_ID)
        .menu(&menu)
        // Left click SHOWS; it does not also throw the menu up — one gesture,
        // one meaning.
        .show_menu_on_left_click(false)
        .on_menu_event(|app: &AppHandle, event| match event.id().as_ref() {
            "tray-open" => show_main(app),
            "tray-inbox" => {
                show_main(app);
                let _ = app.emit("aeon://navigate", crate::background::NavTarget::Inbox.wire());
            }
            "tray-quit" => {
                // Set the flag FIRST: the window's close handler consults it
                // and must answer Close, never Hide, for the rest of this
                // process — a Quit blocked by a hide is the test's name for
                // this bug.
                if let Some(state) = app.try_state::<crate::AppState>() {
                    state.quitting.store(true, Ordering::Relaxed);
                }
                // Exit, do not stop: RunEvent::Exit remains the ONLY place a
                // daemon is stopped.
                app.exit(0);
            }
            other => eprintln!("aeon: tray menu event {other:?} has no action"),
        })
        .on_tray_icon_event({
            let app = app.clone();
            move |tray: &TrayIcon, event| {
                if let TrayIconEvent::Click {
                    button: MouseButton::Left,
                    button_state: MouseButtonState::Up,
                    ..
                } = event
                {
                    let _ = tray; // the handle is already ours via the clone
                    show_main(&app);
                }
            }
        });
    if let Some(icon) = app.default_window_icon() {
        // The app's own PNG icon — the one the bundle already ships.
        builder = builder.icon(icon.clone());
    }
    builder.build(app)?;
    Ok(())
}

/// Paint the attention dot onto the tray icon — or repaint it plain. The
/// answer is whether the icon NOW honestly shows `on`: every caller stores
/// the answer rather than the wish, so a failed paint leaves the record
/// saying "not painted" and the next refresh retries it.
///
/// The base is the app's own `default_window_icon()`, freshly copied per
/// call — the dot is composited from the artwork every time, never onto a
/// previous result, so a burst of transitions can never accumulate dots.
/// `set_icon` reaches the OS through `run_item_main_thread!`, which WAITS on
/// the event loop — so this function may be called from any thread EXCEPT
/// the event loop's own (the poller and the async `tray_unread` command both
/// qualify; a sync command or a window-event handler would hang the app).
pub fn apply_badge(app: &AppHandle, on: bool) -> bool {
    static NO_BASE_LOGGED: std::sync::Once = std::sync::Once::new();
    static TOO_SMALL_LOGGED: std::sync::Once = std::sync::Once::new();
    let Some(tray) = app.tray_by_id(TRAY_ID) else {
        return false;
    };
    let Some(base) = app.default_window_icon() else {
        // Reported once, then silence: with no artwork there is nothing to
        // composite, and this is not a fact that changes mid-session.
        NO_BASE_LOGGED.call_once(|| {
            eprintln!("aeon: no default window icon to badge — the tray keeps its plain icon");
        });
        return false;
    };
    let (w, h) = (base.width(), base.height());
    let mut rgba = base.rgba().to_vec();
    if on && !badge_dot(&mut rgba, w, h) {
        TOO_SMALL_LOGGED.call_once(|| {
            eprintln!("aeon: tray icon {w}x{h} too small to badge — refusing to mispaint it");
        });
        return false;
    }
    match tray.set_icon(Some(tauri::image::Image::new_owned(rgba, w, h))) {
        Ok(()) => true,
        Err(e) => {
            eprintln!("aeon: could not set the tray icon: {e}");
            false
        }
    }
}

/// Re-decide the dot from the one input that may light it — the unread count
/// the web layer last reported (`tray_unread`) — and repaint on a real
/// change. Two callers reach it: the async command the moment the
/// notification store's number moves, and the poller after every refresh
/// (which keeps the applied record honest across a failed earlier paint).
/// Edge-triggered on the APPLIED record, so neither caller's burst re-sets
/// the OS icon without a real transition.
pub fn refresh_badge(app: &AppHandle) {
    let state = app.state::<crate::AppState>();
    let want = tray_badge(state.web_unread.load(Ordering::Relaxed));
    if want != state.tray_badge.load(Ordering::Relaxed) && apply_badge(app, want) {
        state.tray_badge.store(want, Ordering::Relaxed);
    }
}

/// Refresh the tray's count line from the live session. Runs on the poller
/// thread — NEVER from the session's reader thread (see this file's header).
fn refresh_tray(app: &AppHandle) {
    let state = app.state::<crate::AppState>();
    // The handle is cloned INSIDE the lock and the lock is put down BEFORE the
    // call — two statements, so the guard is really gone: `inbox_count` waits
    // on the daemon, and a slot lock held across that wait is a barrier every
    // `connect`, `disconnect` and `rpc_call` then queues behind for as long as
    // the daemon takes to answer. A handle is the type built for holding while
    // waiting; a mutex guard is not.
    let handle = state.session.lock().unwrap().as_ref().map(|session| session.handle());
    let count = handle.and_then(|handle| handle.inbox_count());
    let ours = state.daemon_ours.load(Ordering::Relaxed);
    match build_menu(app, count, ours) {
        Ok(menu) => {
            if let Some(tray) = app.tray_by_id(TRAY_ID) {
                if let Err(e) = tray.set_menu(Some(menu)) {
                    eprintln!("aeon: could not refresh the tray menu: {e}");
                }
                // The hover text carries the SAME sentence as the menu's
                // count line — one truth, three surfaces (icon, hover, menu).
                let label = tray_items(count, ours)
                    .into_iter()
                    .map(|item| item.parts())
                    .find(|(id, _, _)| *id == "tray-count")
                    .map(|(_, label, _)| label);
                if let Some(label) = label {
                    if let Err(e) = tray.set_tooltip(Some(label)) {
                        eprintln!("aeon: could not refresh the tray tooltip: {e}");
                    }
                }
            }
        }
        Err(e) => eprintln!("aeon: could not rebuild the tray menu: {e}"),
    }
    // The dot re-decides on the same beat: the menu/tooltip carry the inbox
    // truth, the icon carries the unread truth, and a paint that failed
    // earlier gets its retry here.
    refresh_badge(app);
}

/// The poke the event sink sends (`session.rs`): a beat saying "the world
/// changed; if the window is hidden, the tray's count is stale". `try_send`
/// into a length-one channel means a burst of events coalesces into one
/// refresh instead of a queue of them.
pub fn poke_tray(app: &AppHandle) {
    if let Some(state) = app.try_state::<crate::AppState>() {
        let _ = state.tray_poke.lock().unwrap().try_send(());
    }
}

/// Should a refresh happen right now? Tray up and not quitting. The window's
/// visibility is NOT a gate: the person watches the tray while using the app
/// too (the dot's other input — unread notifications — moves while the
/// window is open, and a tray that only refreshes in hiding is a tray that
/// visibly lies while watched). The cost is one coalesced `inbox.summary`
/// read per poke burst on the local socket, bounded by the one-slot poke
/// and the 60 s heartbeat.
fn should_refresh(app: &AppHandle) -> bool {
    let state = app.state::<crate::AppState>();
    if state.quitting.load(Ordering::Relaxed) {
        return false;
    }
    let up = *state.background_presence.lock().unwrap() == "tray";
    up
}

/// The 60-second heartbeat, woken early by every poke (event, window-hide).
/// One thread for the process's life; `app.exit(0)` takes it down with
/// everything else, and a disconnected channel ends it on its own.
pub fn start_poller(app: AppHandle, rx: Receiver<()>) {
    std::thread::spawn(move || loop {
        match rx.recv_timeout(Duration::from_secs(60)) {
            Ok(()) | Err(std::sync::mpsc::RecvTimeoutError::Timeout) => {
                if should_refresh(&app) {
                    refresh_tray(&app);
                }
            }
            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => break,
        }
    });
}

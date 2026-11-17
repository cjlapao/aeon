// The shell's threading rule, ENFORCED rather than requested.
//
// A synchronous `#[tauri::command]` runs on the main thread — the one thread
// that paints the window and dispatches every click — so its body *is* the
// window's availability. S72 T5 measured what that is worth: `seed.status`
// answered in 6.2 s, and for those 6.2 s the app was a picture of an app. No
// navigation landed, no menu answered, no other page could be opened while one
// page's spinner turned. Same call as an `async fn`: 140 ms of the daemon's
// work, and a window that behaves the whole way through.
//
// Requiring async costs almost nothing, which is why the rule can be absolute.
// The web layer already `await`s every command — `ui/src/lib/bridge.ts` hands
// back a Promise for each — so sync and async are the SAME call to the caller
// and the conversion needed zero JS change. What it changes is only who waits:
// a tokio worker, or the window.
//
// The scan WALKS `src/` instead of naming files, so a command added in a file
// that does not exist yet is covered by this test the day it is written. That
// is the point: the rule is "everything we add is async", not "these thirteen
// are".

use std::fs;
use std::path::{Path, PathBuf};

/// One `#[tauri::command]` found in the shell: where, what it is called, and
/// whether the macro was told to run it off the main thread.
struct Found {
    file: String,
    name: String,
    signature: String,
    is_async: bool,
}

fn rust_files(dir: &Path, out: &mut Vec<PathBuf>) {
    for entry in fs::read_dir(dir).expect("the shell's src/ is readable") {
        let path = entry.expect("a readable directory entry").path();
        if path.is_dir() {
            rust_files(&path, out);
        } else if path.extension().is_some_and(|ext| ext == "rs") {
            out.push(path);
        }
    }
}

/// The first code line after each `#[tauri::command]` attribute. Blank lines
/// and other attributes are skipped; that is the whole parse, and it is enough
/// because a command's `fn` line is the only place `async` could appear.
fn commands() -> Vec<Found> {
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("src");
    let mut files = Vec::new();
    rust_files(&root, &mut files);
    assert!(!files.is_empty(), "no Rust sources found under {root:?}");

    let mut found = Vec::new();
    for file in files {
        let src = fs::read_to_string(&file).expect("a readable source file");
        let lines: Vec<&str> = src.lines().collect();
        for (idx, line) in lines.iter().enumerate() {
            if !line.trim().starts_with("#[tauri::command]") {
                continue;
            }
            let signature = lines[idx + 1..]
                .iter()
                .map(|l| l.trim())
                .find(|l| !l.is_empty() && !l.starts_with('#') && !l.starts_with("///"))
                .unwrap_or_else(|| {
                    panic!(
                        "{}:{} has no fn after its command attribute",
                        file.display(),
                        idx + 1
                    )
                })
                .to_string();
            found.push(Found {
                file: file.file_name().unwrap().to_string_lossy().into_owned(),
                name: signature
                    .split("fn")
                    .nth(1)
                    .map(|rest| {
                        rest.trim_start()
                            .split(['(', '<'])
                            .next()
                            .unwrap_or("")
                            .to_string()
                    })
                    .unwrap_or_default(),
                is_async: signature.contains("async fn"),
                signature,
            });
        }
    }
    found
}

#[test]
fn every_command_is_async_because_a_sync_one_is_the_window() {
    let found = commands();
    // Non-vacuous: the scan sees the surface it claims to see.
    assert!(
        found.len() >= 13,
        "the scan found only {} commands — it has gone blind, and a passing \
         test here would then mean nothing",
        found.len()
    );

    let sync: Vec<String> = found
        .iter()
        .filter(|c| !c.is_async)
        .map(|c| format!("{}: {}", c.file, c.signature))
        .collect();
    assert!(
        sync.is_empty(),
        "these commands run on the MAIN THREAD, which is the window — the one \
         that paints and answers clicks (S72 T5: 6.2 s of `seed.status` there \
         meant 6.2 s of an app that answered nothing):\n  {}\n\
         Make them `async fn`. If the body blocks on a descriptor, a file or a \
         lock another thread holds, move that work inside \
         `tauri::async_runtime::spawn_blocking` — an async command's thread is \
         a tokio worker and must not sit waiting. If it takes `State`, it must \
         also return `Result`: Tauri's macro requires it, and for a read that \
         cannot fail the `Err` arm is shape, not possibility.",
        sync.join("\n  ")
    );
}

#[test]
fn the_scan_sees_exactly_the_registered_command_surface() {
    // Every name in `generate_handler![]` is a command this test has judged, and
    // every command found is registered. Without this, a command defined in a
    // file the scan somehow skipped — or a helper misread as a command — would
    // quietly change what the check above covers.
    let main = include_str!("../src/main.rs");
    let list = main
        .split("generate_handler![")
        .nth(1)
        .expect("the registered command list")
        .split("])")
        .next()
        .expect("the registered command list closes");
    let registered: Vec<String> = list
        .split(',')
        .map(|entry| {
            entry
                .lines()
                .find(|l| !l.trim().is_empty() && !l.trim().starts_with("//"))
                .unwrap_or("")
                .trim()
                .rsplit("::")
                .next()
                .unwrap_or("")
                .to_string()
        })
        .filter(|name| !name.is_empty())
        .collect();

    let found: Vec<Found> = commands().into_iter().collect();
    let names: Vec<&str> = found.iter().map(|c| c.name.as_str()).collect();

    for name in &registered {
        assert!(
            names.contains(&name.as_str()),
            "`{name}` is registered but the scan does not see it, so nothing \
             judges which thread it runs on"
        );
    }
    for command in &found {
        assert!(
            registered.contains(&command.name),
            "`{}` is a command but is not registered — the list above and the \
             sources have drifted, and the check that keeps the window free \
             covers the wrong set",
            command.name
        );
    }
    assert_eq!(registered.len(), found.len(), "one surface, one count");
}

/// The main thread appears OUTSIDE the command surface too: Tauri's window and
/// tray event handlers run on it, and — unlike a command — they have no async
/// form to be moved to. So the rule for them is stricter, not looser: read an
/// atomic, and never take a mutex that a worker may be holding across a wait.
///
/// This test exists because making all thirteen commands async did NOT, by
/// itself, free the window. The close handler read `state.session.lock()`, and
/// `connect` holds that very mutex across a whole connect — seconds of kernel
/// waiting when the workspace points at a host that is not answering. Every
/// command was async and the app still froze, one click on the X away. It reads
/// the `session_live` mirror now, which answers the same question without the
/// wait; this test is what keeps it that way.
#[test]
fn the_main_thread_event_handlers_wait_on_nothing_a_worker_holds() {
    let main = include_str!("../src/main.rs");
    let handler = main
        .split(".on_window_event(")
        .nth(1)
        .expect("the window-event handler")
        .split(".invoke_handler(")
        .next()
        .expect("the handler ends where the commands begin");

    assert!(
        handler.contains("session_live"),
        "the close handler must read the mirror, not the mutex: `connect` holds \
         the session slot across a whole connect, and this handler has no \
         worker thread to be moved to"
    );
    assert!(
        !handler.contains("session.lock()") && !handler.contains("workspaces.lock()"),
        "the close handler took a lock a worker can hold across a blocking wait"
    );
    // The one lock that stays, and why it is allowed: `tray_poke` is held only
    // long enough for a `try_send` on a one-slot channel, which cannot block and
    // is never held by anyone waiting on the daemon.
    assert_eq!(
        handler.matches(".lock()").count(),
        1,
        "the handler's only lock may be the tray poke — every lock added here \
         has to answer for how long it can be held"
    );
    assert!(
        handler.contains("tray_poke.lock()"),
        "the poke lock moved; this test's reasoning about it needs updating"
    );
}

/// The same rule one thread over, where it was also being broken: the tray
/// poller read `inbox.summary` — a call that waits up to `CALL_TIMEOUT` —
/// **while holding the session slot's mutex**, so a daemon that stopped draining
/// turned that mutex into a barrier every `connect`, `disconnect` and `rpc_call`
/// queued behind. The handle is the type meant to be held across a wait; a
/// mutex guard is not.
#[test]
fn no_lock_is_held_across_a_call_to_the_daemon() {
    let tray = include_str!("../src/background_tray.rs");
    assert!(
        tray.contains(".map(|session| session.handle())"),
        "the poller must clone a handle out of the session slot and put the \
         lock down BEFORE it calls the daemon"
    );
    let before = tray
        .split("fn refresh_tray")
        .nth(1)
        .expect("refresh_tray")
        .split("let count")
        .next()
        .expect("the part of refresh_tray that reaches for the session");
    assert!(
        !before.contains(".inbox_count()"),
        "refresh_tray is asking the daemon for the count while still inside the \
         lock's scope again — clone the handle, drop the guard, then ask"
    );
}

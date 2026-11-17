//! S58 T7 — the background half's policy, proven with no display, no tray
//! host, and no daemon.
//!
//! The decisions of `src/background.rs` are pure functions precisely so
//! this file can run them where the tray cannot: close-to-tray, the honest
//! Quit label, the deep-link grammar, and — the badge follow-up — the dot's
//! on/off rule and its pixel painting, decided over bytes no display is
//! needed to inspect. On top of the pure behaviour, two
//! SOURCE assertions hold promises a unit test cannot execute: the daemon
//! stop semantics did not move (`RunEvent::Exit` is still the only stopper),
//! and the capabilities file still holds exactly the recorded permission list
//! — the web layer gained nothing to make any of this work.

#[path = "../src/background.rs"]
mod background;

use background::{
    badge_dot, classify_link, close_policy, parse_link, tray_badge, tray_items, BADGE_DOT_RGB,
    BADGE_RING_RGB, ClosePolicy, NavTarget, TrayItem,
};

#[test]
fn close_policy_hides_only_with_a_live_session() {
    // All four combinations, named at each call site of the rule.
    assert_eq!(close_policy(true, false), ClosePolicy::Hide, "live session, not quitting — hide to tray");
    assert_eq!(close_policy(false, false), ClosePolicy::Close, "nothing running — closing is honest");
    assert_eq!(close_policy(true, true), ClosePolicy::Close, "quitting — see the next test");
    assert_eq!(close_policy(false, true), ClosePolicy::Close, "quitting with nothing running");
}

#[test]
fn close_policy_never_hides_while_quitting() {
    // The one failure this rule prevents: a Quit that a hide swallows.
    for has_live in [true, false] {
        assert_eq!(close_policy(has_live, true), ClosePolicy::Close);
    }
}

#[test]
fn tray_items_quit_label_is_honest() {
    let ours = tray_items(Some(3), true);
    assert_eq!(ours.len(), 4);
    assert_eq!(ours[0], TrayItem::Open);
    assert_eq!(ours[1], TrayItem::Inbox);
    assert_eq!(ours[3], TrayItem::QuitOurs);
    let (id, label, enabled) = ours[3].parts();
    assert_eq!(id, "tray-quit");
    assert!(enabled, "Quit is never a dead line");
    assert_eq!(label, "Quit Aeon (stops the daemon this app started)");

    let not_ours = tray_items(Some(3), false);
    let (_, label, _) = not_ours[3].parts();
    assert_eq!(label, "Quit Aeon", "we did not start a daemon; the label may not claim we stop one");
}

#[test]
fn tray_items_count_line_handles_unavailable() {
    let (_, label, enabled) = tray_items(None, false)[2].parts();
    assert_eq!(label, "Inbox unavailable");
    assert!(!enabled, "the count line is a statement, not a button");
    let (_, one_label, ..) = tray_items(Some(1), false)[2].parts();
    assert_eq!(one_label, "1 needs you");
    let (_, few_label, ..) = tray_items(Some(3), false)[2].parts();
    assert_eq!(few_label, "3 need you");
    // The rule the whole thing exists for: an unreadable count is the ONE
    // label above, and zero — a real, different fact — is never confused
    // with it.
    let (_, zero_label, ..) = tray_items(Some(0), false)[2].parts();
    assert_eq!(zero_label, "0 need you");
    assert_ne!(zero_label, label);
}

#[test]
fn tray_badge_is_the_bell_mirror_reading_them_all_ends_the_dot() {
    // The operator's ruling, walked twice on Linux: the dot mirrors the
    // bell's unread number and NOTHING else may hold it lit. Reading them
    // all — mark one, mark all, clear — must always take the indicator
    // away; a pending daemon inbox keeps its own surfaces (the menu's
    // "N need you" line and the tooltip) and keeps its own honesty rule
    // THERE, where an unreadable count can speak without pinning a dot.
    assert!(!tray_badge(0), "nothing unread — no dot, whatever else is true");
    assert!(tray_badge(1), "one unread — dot");
    assert!(tray_badge(42), "forty-two unread — dot");
    // The pairing the first cut pinned (dot == non-zero menu line) is
    // DELIBERATELY gone: the icon and the count line are now different
    // truths by decision — the bell's number versus the daemon's roster.
    let (_, zero_label, _) = tray_items(Some(0), false)[2].parts();
    assert_eq!(zero_label, "0 need you", "the roster line kept its sentence");
    assert!(!tray_badge(0), "and a read bell clears the dot beside it");
}

#[test]
fn badge_dot_paints_the_top_right_corner() {
    // A 32×32 mid-gray opaque base. After the paint: the dot's centre is the
    // dot colour, a ring pixel is ring colour, and the far corner is byte-
    // identical to the base. Properties, not a golden image — but pinned
    // enough that moving the dot, the colours, the untouched-pixels rule,
    // or the SIZE all fail here first.
    let (w, h) = (32u32, 32u32);
    let mut px = vec![0u8; (w as usize) * (h as usize) * 4];
    for chunk in px.chunks_exact_mut(4) {
        chunk.copy_from_slice(&[120, 120, 120, 255]);
    }
    let base = px.clone();
    let at = |px: &[u8], x: u32, y: u32| -> [u8; 4] {
        let i = ((y * w + x) * 4) as usize;
        px[i..i + 4].try_into().unwrap()
    };

    assert!(badge_dot(&mut px, w, h));

    // The geometry the doc promises: the WHOLE mark (disc + ring) is a
    // quarter of the icon's shorter edge — outer = max(3, 32/8) = 4, ring =
    // max(1, 32/32) = 1, disc r = 3; centre = (w-1-outer, outer) = (27, 4).
    // (The first cut's d/4 disc was enormous on the 512 px icon the Linux
    // tray paints from — the operator asked for less lid over the artwork.)
    assert_eq!(at(&px, 27, 4), [224, 62, 62, 255], "the dot's centre lost its colour");
    assert_eq!(
        at(&px, 27, 4),
        [BADGE_DOT_RGB.0, BADGE_DOT_RGB.1, BADGE_DOT_RGB.2, 255],
        "a constant drifted from the constant"
    );
    // The ring: on the outer radius line, one past the disc edge.
    assert_eq!(at(&px, 27, 4 + 4), [BADGE_RING_RGB.0, BADGE_RING_RGB.1, BADGE_RING_RGB.2, 255]);
    // The quarter-size promise, measured: the mark does not reach past a
    // quarter of the canvas from the corner.
    assert_eq!(&at(&px, 27 - 5, 4 + 5)[..3], &[120, 120, 120], "the mark outgrew its quarter");
    // The far bottom-left corner is untouched — the base, byte for byte.
    assert_eq!(at(&px, 0, 31), at(&base, 0, 31), "the dot smeared across the icon");
    // And a transparent base pixel far from the dot keeps its transparency:
    // untouched means alpha included.
    let mut with_hole = base.clone();
    with_hole[..4].copy_from_slice(&[0, 0, 0, 0]);
    assert!(badge_dot(&mut with_hole, w, h));
    assert_eq!(&with_hole[..4], &[0, 0, 0, 0], "alpha is not the dot's to touch");
}

#[test]
fn badge_dot_refuses_bad_buffers() {
    // Wrong length, and a canvas too small to hold a legible dot: refused,
    // every byte unchanged, no panic — the plumbing keeps the icon it has.
    let mut short = vec![7u8; 32 * 32 * 4 - 1];
    let untouched = short.clone();
    assert!(!badge_dot(&mut short, 32, 32));
    assert_eq!(short, untouched);

    let mut tiny = vec![7u8; 4 * 4 * 4];
    let tiny_before = tiny.clone();
    assert!(!badge_dot(&mut tiny, 4, 4), "4×4 is below the legibility floor");
    assert_eq!(tiny, tiny_before);

    // Oversize dimensions over a right-sized buffer is the same refusal in
    // another guise: the promise is bytes == w*h*4, and a lie about size is
    // refused, not walked.
    let mut liar = vec![7u8; 32 * 32 * 4];
    assert!(!badge_dot(&mut liar, 64, 64));
}

#[test]
fn badge_dot_survives_odd_and_large_sizes() {
    // No panic and no out-of-bounds write on the shapes real icon sets
    // contain — odd sizes included; every painted byte stays inside the
    // canvas, and an all-clear far corner proves the box scan respected them.
    for (w, h) in [(31u32, 31), (64, 64), (32, 40), (40, 32), (8, 8)] {
        let mut px = vec![9u8; (w as usize) * (h as usize) * 4];
        assert!(badge_dot(&mut px, w, h), "{w}x{h} refused");
        // Byte count is the proof nothing was appended; and the bottom-left
        // pixel is far outside any corner box on a canvas ≥ 8.
        let i = ((h - 1) * w) as usize * 4;
        assert_eq!(&px[i..i + 4], &[9, 9, 9, 9][..], "far corner moved at {w}x{h}");
    }
}

#[test]
fn parse_link_accepts_only_the_three_forms() {
    assert_eq!(parse_link("aeon://inbox"), Some(NavTarget::Inbox));
    assert_eq!(parse_link("aeon://loop/ab12_CD-9"), Some(NavTarget::Loop("ab12_CD-9".into())));
    assert_eq!(parse_link("aeon://root/x1"), Some(NavTarget::Root("x1".into())));

    let refused = [
        "",                          // nothing
        "aeon://",                   // bare scheme
        "aeon://inbox/",             // inbox takes no path
        "aeon://inbox?token=t",      // nor a query
        "aeon://settings",           // unknown door
        "http://aeon://inbox",       // wrong scheme outright
        "AEON://inbox",              // the form is the form (case included)
        "aeon://loop",               // door without its id
        "aeon://loop/",              // empty id
        "aeon://loop/a/b",           // deeper than one segment
        "aeon://loop/..%2f..",       // traversal, encoded
        "aeon://loop/../../etc/passwd", // traversal, plain
        "aeon://loop/a\\b",          // backslash
        "aeon://loop/with space",    // space is not in the set
        "aeon://loop/x?token=t",     // a query after a well-shaped id
        "aeon://loop/x#frag",        // a fragment likewise
        "aeon://loop/x%41",          // percent-encoding is not in the set
        &format!("aeon://loop/{}", "x".repeat(65)), // oversize
    ];
    for url in refused {
        assert_eq!(parse_link(url), None, "accepted a link it must refuse: {url:?}");
    }
    // 64 is in the set — the cap measured, not guessed.
    assert!(parse_link(&format!("aeon://root/{}", "x".repeat(64))).is_some());
}

#[test]
fn deep_link_never_carries_an_answer() {
    // Every accepted target re-renders to one canonical string with room for
    // exactly a place: no field survives a parse, so nothing can ride in.
    for url in ["aeon://inbox", "aeon://loop/ab-1", "aeon://root/zz_9"] {
        let t = parse_link(url).expect("accepted form parses");
        let wire = t.wire();
        assert!(
            wire == "aeon://inbox" || wire.starts_with("aeon://loop/") || wire.starts_with("aeon://root/"),
            "wire form drifted: {wire}"
        );
        assert!(!wire.contains('?') && !wire.contains('#') && !wire.contains('%'));
        assert_eq!(parse_link(&wire), Some(t.clone()), "the emitted form re-parses identical");
    }
    // The refusal vocabulary is CLOSED and each word is a whole answer —
    // nothing that could be a token, a path, or the raw URL itself.
    let words: Vec<&str> = [
        classify_link("x://inbox"),
        classify_link("aeon://nope"),
        classify_link("aeon://loop/"),
        classify_link(&format!("aeon://loop/{}", "x".repeat(65))),
        classify_link("aeon://loop/x?token=abc"),
    ]
    .iter()
    .map(|r| r.as_ref().err().expect("each input refuses").reason())
    .collect();
    assert_eq!(words, vec!["wrong-scheme", "unknown-door", "bad-target", "oversize-target", "carries-extra"]);
    for w in &words {
        assert!(!w.contains("token") && !w.contains("abc") && !w.contains('/'));
    }
}

#[test]
fn deeplink_fixture_and_parser_agree() {
    // THE shared table (S58 T8): the web parser runs the same file
    // (`ui/test/navigate-listener.test.tsx`), and the file's own comment
    // says what a one-sided edit means. The acceptance cases pin the
    // parsed parts; the refusal cases pin WHICH fixed word — the web toast
    // renders exactly these, so a word that changes here changes what the
    // person reads, and must fail this test first.
    let raw = include_str!("../../test/fixtures/deeplinks.json");
    let doc: serde_json::Value = serde_json::from_str(raw).expect("fixture parses");
    let cases = doc["cases"].as_array().expect("cases array");
    assert!(cases.len() >= 20, "the table may not shrink to nothing");
    for case in cases {
        let url = case["url"].as_str().expect("url");
        match case["accept"].as_object() {
            None => {
                let want = case["reason"].as_str().expect("a refusal names its reason");
                let got = classify_link(url).err().unwrap_or_else(|| panic!("accepted {url:?}")).reason();
                assert_eq!(got, want, "refusal word drifted for {url:?}");
            }
            Some(a) => {
                let want = match a["kind"].as_str().expect("kind") {
                    "inbox" => NavTarget::Inbox,
                    "loop" => NavTarget::Loop(a["id"].as_str().expect("loop id").to_string()),
                    "root" => NavTarget::Root(a["id"].as_str().expect("root id").to_string()),
                    other => panic!("unknown fixture kind {other}"),
                };
                assert_eq!(parse_link(url), Some(want), "acceptance drifted for {url:?}");
            }
        }
    }
}

#[test]
fn capabilities_file_unchanged() {
    // The permission list of `capabilities/default.json`, exactly as recorded
    // on disk before S58 T7 started. T7's own rule: all handling is
    // Rust-side and `listen` is already inside `core:event:default`, so this
    // test passing is the proof that the web layer gained NOTHING. (The
    // slice spec said "the recorded five"; the file on disk measures FOUR —
    // recorded honestly, and the drift is in the task Notes.)
    let raw = include_str!("../capabilities/default.json");
    let doc: serde_json::Value = serde_json::from_str(raw).expect("capabilities parse");
    let perms: Vec<&str> = doc["permissions"]
        .as_array()
        .expect("permissions array")
        .iter()
        .map(|p| p.as_str().expect("string permission"))
        .collect();
    assert_eq!(
        perms,
        vec![
            "core:event:default",
            "notification:allow-is-permission-granted",
            "notification:allow-request-permission",
            "notification:allow-notify",
        ]
    );
}

#[test]
fn exit_path_still_stops_only_spawned_daemon() {
    // A source assertion (the repo's own convention for "this branch must
    // not move"): the RunEvent::Exit handler in main.rs is unchanged, and
    // nothing NEW stops the daemon anywhere in the shell.
    let src = include_str!("../src/main.rs");
    assert!(src.contains("if let tauri::RunEvent::Exit = event"), "the Exit branch moved or was renamed");
    assert!(src.contains(".stop_if_spawned()"), "Exit no longer stops the spawned daemon");
    // The Exit branch contains stop_if_spawned, and it is the ONLY mention
    // of stopping outside local_daemon.rs itself:
    let exits = src.matches("stop_if_spawned").count();
    assert_eq!(exits, 1, "the stop call must appear exactly once in main.rs");
    let daemon_src = include_str!("../src/local_daemon.rs");
    assert_eq!(daemon_src.matches(".kill()").count(), 1, "one kill site, the one that was already there");
}

#[test]
fn tray_failure_is_nonfatal() {
    // Source assertion again: the builder's failure LOGS and records
    // "unavailable" — it never `?`s out of setup (which would abort the app)
    // and never panics.
    let src = include_str!("../src/main.rs");
    assert!(src.contains("match background_tray::build_tray(app.handle(), daemon_ours)"), "the builder is not guarded");
    assert!(src.contains("background presence unavailable"), "the failure says what it is");
    // The exact fatal shapes, refused:
    assert!(!src.contains("background_tray::build_tray(app.handle(), daemon_ours)?"), "the builder's error escapes setup");
    assert!(!src.contains("build_tray(app.handle(), daemon_ours).unwrap()"), "the builder's error panics");
    // background_presence STARTS "unavailable" and is only raised on Ok —
    // an app that never reaches the match arm is recorded honestly too.
    assert!(src.contains("background_presence: Mutex::new(\"unavailable\")"));
    assert!(src.contains("= \"tray\""));
}

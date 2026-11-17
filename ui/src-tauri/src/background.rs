// The shell that keeps living while the window is closed (S58 T7, SV9/SV10).
//
// # What this file is for
//
// With the window closed, the person is still supposed to hear from Aeon:
// OS notifications keep arriving (they were the web layer's all along), the
// tray menu is the way back in, and a deep link from a notification shows the
// window at the right place. Quitting stays explicit, and the Quit label says
// what quitting actually stops — the daemon this app started, when this app
// started one, and NOTHING when it did not (SV10).
//
// # The shape of the split
//
// The decisions are PURE functions (`close_policy`, `tray_items`,
// `parse_link`, and — the badge follow-up — `tray_badge` and `badge_dot`,
// pure over RGBA bytes for the same reason as the labels: pixels get decided
// where a test can see them) and everything around them is plumbing that
// feeds them and acts on their answers. The pure half is what
// `tests/background_policy.rs` runs with no display, no tray host, and no
// daemon — which matters because the tray itself cannot be unit-tested where
// it is not installed.
//
// # What is NOT here
//
// No new capability reaches the web layer: the navigate emits ride
// `core:event:default`, which the window already holds. No second subscriber:
// the count refresh is POKED by the event sink that already exists
// (`session.rs`'s `AppSink::event`), and the poke is answered on the poller
// thread — never on the session's reader thread, which is the one thread that
// must not make a blocking call of its own (the response it waits for is the
// one only it can read; a call from the sink would deadlock the session).
// No autostart (SV-D5). The daemon-stop semantics did not move: `RunEvent::
// Exit` in `main.rs` is still the one place a daemon is stopped, and Quit
// reaches it by exiting, not by calling anything new.

/// The two answers to "the person just closed the main window".
#[derive(Debug, PartialEq, Eq)]
pub enum ClosePolicy {
    /// Keep the process (and the daemon) alive; the tray is the way back.
    Hide,
    /// Let the window close — which, with no other window open, runs the
    /// normal exit path, and `RunEvent::Exit` stops only a spawned daemon.
    Close,
}

/// The one rule (SV9): hide ONLY when there is a live daemon session to be
/// the reason, and only when the person is not quitting. A window closed with
/// nothing running hides a shell around nothing — that is Close, and it is
/// why `Close` is the `else` and never a second condition.
pub fn close_policy(has_live_session: bool, quitting: bool) -> ClosePolicy {
    if has_live_session && !quitting {
        ClosePolicy::Hide
    } else {
        ClosePolicy::Close
    }
}

/// One line of the tray menu, as a decision — the labels are pinned here so
/// the honesty of the Quit line and the unavailable-count line is a unit
/// test's job, not a screenshot's.
#[derive(Debug, PartialEq, Eq)]
pub enum TrayItem {
    Open,
    Inbox,
    /// The count line. `None` inbox means exactly one label: "Inbox
    /// unavailable" — never a plausible zero (SV9: the same rule as the CLI's
    /// source census; an unavailable source must not read as an empty one).
    Count(String),
    /// The person started this app's daemon: quitting stops it, and says so.
    QuitOurs,
    /// We did not start a daemon; quitting stops only the shell.
    QuitPlain,
}

impl TrayItem {
    /// (menu id, label, enabled) — the mapping the builder consumes.
    pub fn parts(&self) -> (&'static str, String, bool) {
        match self {
            TrayItem::Open => ("tray-open", "Open Aeon".into(), true),
            TrayItem::Inbox => ("tray-inbox", "Inbox".into(), true),
            TrayItem::Count(s) => ("tray-count", s.clone(), false),
            TrayItem::QuitOurs => (
                "tray-quit",
                "Quit Aeon (stops the daemon this app started)".into(),
                true,
            ),
            TrayItem::QuitPlain => ("tray-quit", "Quit Aeon".into(), true),
        }
    }
}

/// The whole menu, in order: Open, Inbox, the count line, Quit.
pub fn tray_items(inbox: Option<u32>, daemon_is_ours: bool) -> Vec<TrayItem> {
    let count = match inbox {
        Some(n) if n == 1 => TrayItem::Count("1 needs you".into()),
        Some(n) => TrayItem::Count(format!("{n} need you")),
        None => TrayItem::Count("Inbox unavailable".into()),
    };
    vec![
        TrayItem::Open,
        TrayItem::Inbox,
        count,
        if daemon_is_ours {
            TrayItem::QuitOurs
        } else {
            TrayItem::QuitPlain
        },
    ]
}

/// Should the tray icon carry the attention dot? **One door opens it: the
/// app's own unread notifications** — the number the bell wears, mirrored.
/// The operator's ruling after walking the first cut: reading them all must
/// ALWAYS take the indicator away, so nothing else may hold it lit; the
/// daemon's inbox keeps its own surfaces (the menu's "N need you" line and
/// the tooltip), where an unreadable count can speak honestly without
/// pinning a dot over the icon.
pub fn tray_badge(unread: u32) -> bool {
    unread > 0
}

/// The dot's fill — a red the eye reads on any tray bar, menu-light or dark.
pub const BADGE_DOT_RGB: (u8, u8, u8) = (224, 62, 62);
/// The separation ring — one step of white between dot and artwork, so the
/// dot stays legible over red-heavy app icons.
pub const BADGE_RING_RGB: (u8, u8, u8) = (255, 255, 255);

/// Paint the attention dot into an RGBA8 icon, in place, at the top-right
/// corner. The whole mark (disc + ring) is a QUARTER of the icon's shorter
/// edge: outer radius `max(3, d/8)` with `d = min(w, h)`, ring thickness
/// `max(1, d/32)`, the disc what remains — a corner badge, not a lid over
/// the artwork (the first cut's `d/4` read as enormous on the 512 px icon
/// the Linux tray actually paints from). The mark is placed with a 1 px
/// margin so the ring touches the top and right edges. Pixels outside are
/// untouched, alpha included; the disc and ring paint opaque.
///
/// Refuses (returns `false`, bytes unchanged) unless the buffer is exactly
/// `w * h * 4` and both edges are at least 8 px — an icon too small to hold
/// a legible dot gets NO dot, and the plumbing keeps the icon it has. Pure
/// over bytes on purpose: this is the decision the no-display test file can
/// run, which is the same reason `tray_items` exists.
pub fn badge_dot(rgba: &mut [u8], w: u32, h: u32) -> bool {
    if rgba.len() != (w as usize) * (h as usize) * 4 || w < 8 || h < 8 {
        return false;
    }
    let d = w.min(h) as i64;
    let outer = (d / 8).max(3);
    let ring = (d / 32).max(1);
    let r = outer - ring;
    // Centre the dot so the ring rides 1 px inside the top-right corner.
    let cx = (w as i64 - 1).saturating_sub(outer);
    let cy = outer;
    let mut paint = |x: i64, y: i64, rgb: (u8, u8, u8)| {
        let i = ((y * w as i64 + x) * 4) as usize;
        rgba[i] = rgb.0;
        rgba[i + 1] = rgb.1;
        rgba[i + 2] = rgb.2;
        rgba[i + 3] = 255;
    };
    // Bound the scan to the ring's box so a 1024² icon does not walk its
    // every pixel for a dot of 64.
    let x0 = (cx - outer).max(0);
    let x1 = (cx + outer).min(w as i64 - 1);
    let y0 = (cy - outer).max(0);
    let y1 = (cy + outer).min(h as i64 - 1);
    for y in y0..=y1 {
        for x in x0..=x1 {
            let dx = x - cx;
            let dy = y - cy;
            if dx * dx + dy * dy <= r * r {
                paint(x, y, BADGE_DOT_RGB);
            } else if dx * dx + dy * dy <= outer * outer {
                paint(x, y, BADGE_RING_RGB);
            }
        }
    }
    true
}

/// Where a deep link is allowed to take the window. Exactly three places —
/// and a target is a place, never an answer: nothing a link carries can act
/// as a capability (the parse tests are the proof).
#[derive(Debug, PartialEq, Eq, Clone)]
pub enum NavTarget {
    Inbox,
    Loop(String),
    Root(String),
}

impl NavTarget {
    /// The canonical wire string emitted to the window. ALWAYS rebuilt from
    /// the parsed parts — the raw URL never crosses into the web layer,
    /// accepted or refused.
    pub fn wire(&self) -> String {
        match self {
            NavTarget::Inbox => "aeon://inbox".into(),
            NavTarget::Loop(id) => format!("aeon://loop/{id}"),
            NavTarget::Root(id) => format!("aeon://root/{id}"),
        }
    }
}

/// Why a link was refused. A fixed vocabulary — the refusal the window hears
/// is one of these words, never the URL (a hostile link must not be quoted
/// back into the page that was right to refuse it).
#[derive(Debug, PartialEq, Eq)]
pub enum LinkRefusal {
    WrongScheme,
    UnknownDoor,
    BadTarget,
    OversizeTarget,
    CarriesExtra,
}

impl LinkRefusal {
    pub fn reason(&self) -> &'static str {
        match self {
            LinkRefusal::WrongScheme => "wrong-scheme",
            LinkRefusal::UnknownDoor => "unknown-door",
            LinkRefusal::BadTarget => "bad-target",
            LinkRefusal::OversizeTarget => "oversize-target",
            LinkRefusal::CarriesExtra => "carries-extra",
        }
    }
}

/// The accepted id shape: `^[A-Za-z0-9_-]{1,64}$`, exactly as the spec
/// contracts it — a character set with no `.` in it is what makes path
/// traversal and percent-encoding unspellable rather than merely discouraged.
fn valid_id(s: &str) -> bool {
    !s.is_empty() && s.len() <= 64 && s.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}

/// Classify a link. The only function that reads a link's bytes; `parse_link`
/// and the refusal path are its two callers, so acceptance and refusal share
/// one grammar.
pub fn classify_link(url: &str) -> Result<NavTarget, LinkRefusal> {
    let rest = url.strip_prefix("aeon://").ok_or(LinkRefusal::WrongScheme)?;
    // No query, no fragment, no percent, no backslash — an accepted link can
    // carry a place and nothing else. Checked over the WHOLE remainder so a
    // `?token=` can never reach the id check looking like an id.
    if rest.contains(['?', '#', '%', '\\']) {
        return Err(LinkRefusal::CarriesExtra);
    }
    let mut segments = rest.split('/');
    match segments.next().unwrap_or("") {
        "inbox" => {
            if rest != "inbox" {
                return Err(LinkRefusal::CarriesExtra); // any path under inbox is extra
            }
            Ok(NavTarget::Inbox)
        }
        door @ ("loop" | "root") => {
            let id = segments.next().ok_or(LinkRefusal::BadTarget)?;
            if segments.next().is_some() {
                return Err(LinkRefusal::CarriesExtra); // deeper than one segment
            }
            if id.len() > 64 {
                return Err(LinkRefusal::OversizeTarget);
            }
            if !valid_id(id) {
                return Err(LinkRefusal::BadTarget);
            }
            Ok(if door == "loop" {
                NavTarget::Loop(id.to_string())
            } else {
                NavTarget::Root(id.to_string())
            })
        }
        _ => Err(LinkRefusal::UnknownDoor),
    }
}

/// The spec's signature: three forms parse, everything else is `None`.
pub fn parse_link(url: &str) -> Option<NavTarget> {
    classify_link(url).ok()
}


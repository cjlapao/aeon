# Brief — S58 Supervision: attention inbox, notification channels, background presence, mission status

Written 2026-10-30. Prefix **SV**. Task shapes cite [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (**P1–P11**). Covers
[`AUTONOMY-GAPS.md`](../AUTONOMY-GAPS.md) **G7** and the "mission control / unified inbox" rows of its UI table (the screens
are **specified here, built after**; this slice builds the daemon, the CLI, the agent reads and the minimum app/shell wiring).
**Depends on:** S44-extra (the component tree: `rootId`), S43-extra (the seed engine, for the `notify.events` set), S45 (shared UI
components — T8 only), S47 (surface classification of the new methods), and three **cross-slice requests** (§6) on S55 (loop
control), S57 (verification gates) and S27 (the scheduler). T0 re-checks what has merged.

## 1. Why

A loop that suspends with a question waits silently until a person opens the app; an unattended loop (S19/S20/S27: unattended by
construction, G14) has **no question at all** — it ends `blocked`. The autonomy slices make both states the *normal* outcome of
unattended work (a cap hit, a stuck loop, a failed gate, a scheduler item that needs a person). Supervising a project that runs
without you is the product; today the machine can only say "something happened" to a window that happens to be open.

## 2. What exists today (measured 2026-10-30; T0 re-walks every line)

| Fact | Where |
| --- | --- |
| A delivered event may carry an optional, **delivery-time-only** `Notify *NotifyHint{Title, Body, Tone}`; never persisted, never replayed | `internal/protocol/types.go:131-149` |
| Which events get a hint is a **closed switch of exactly three kinds** (`loop.finished`, `loop.blocked`, `loop.waiting`), fixed titles ("Loop finished", "Loop blocked", "Loop waiting on you"), no per-event detail | `internal/daemon/lifecycle.go:187-197`; applied at `internal/daemon/daemon.go:641-650` |
| `question.asked` and every other kind: **no hint** | `internal/protocol/step.go:620,760`, `internal/daemon/recover.go:288` |
| The app: `notifications.ts` (localStorage `aeon-notifications-v1`, 200 entries), `NotificationListener.tsx` (subscribes `*`, pushes when `event.notify` is set), `osNotify.ts` (**returns when the window is focused**; Tauri notification plugin), `NotificationBell.tsx` | `ui/src/lib/notifications.ts`, `ui/src/app/NotificationListener.tsx`, `ui/src/lib/osNotify.ts` |
| OS notifications exist **only while the web layer's subscription is live**: the Tauri shell has no tray, no hide-on-close, no background session | `ui/src-tauri/src/main.rs:170-215` (Builder, setup, `RunEvent::Exit`) |
| The web window is granted exactly `core:event:default` + three notification commands; "adding a permission widens what a compromised web layer could do … a reviewed diff with the reason written down" | `ui/src-tauri/capabilities/default.json`, `capabilities/README.md` |
| A pending question is surfaced by **deriving** it: `AwaitingSQL` over unanswered `question` rows (no `awaiting_user` column, no cached flag); the app derives `GateBanner`s from `loop.awaiting` + `question.pending` | `internal/store/chat.go:385-404`; `internal/protocol/memory.go:522` (`store.awaiting`); `ui/src/features/home/pages/HomeScreen.tsx:49-70,260-285` |
| Unattended loops get **no question**: an `ask` denies at once; the loop ends `blocked`, reason text in `LoopView.lastReason` (kinds: `action-denied`, `step-blocked`, `exhaustion`, `model-error`, `runner-error`, …) | `internal/loop/execute.go:89-93`; `internal/loop/advance.go:248,403`; `internal/daemon/blueprint.go:93,661`; `internal/loop/state.go:68` |
| `subscribe {filter{kinds}, after}` replays by `seq`; **`loop.watch` does not exist**; CLI `aeon watch` streams | `internal/protocol/subscribe.go:40`; `cmd/aeon/main.go:75` |
| **No outbound webhook, email or push from the daemon** (grep: none). Outbound HTTP today = model adapters, the forge, the MCP client, webfetch. `trigger/webhook.go` is **inbound** only (HMAC verify of an incoming payload) | `internal/forge/github.go:120`, `internal/trigger/webhook.go:95-108` |
| `internal/nettest` asserts a full loop makes **no connection except configured routes and forges**; "no telemetry" is permanent (N1) | `internal/nettest/nettest.go:1-60` |
| The narrow HTTP/MCP surface mounts **9** enumerated actions and **permanently cannot answer** or create a question (S19 T5); the guard test pins the count | `internal/narrow/actions.go`; `internal/narrow/records_guard_test.go:23-41` |
| The remote workspace listener (S41) is **design only**; the app side stores `Local`/`Remote` workspaces, no daemon listener exists | `docs/aeon/S41-workspaces/design.md`; `ui/src-tauri/src/workspace.rs` |
| Secrets precedent: a provider key is written to the secrets store **first**, the DB row carries none, a test reads the raw DB bytes to prove it | `internal/daemon/memory.go:680-735` (`SaveRouteKey`/`ReadRouteKey`/`DeleteRouteKey`) |

## 3. What this slice delivers

1. **An attention inbox, derived — never stored.** `inbox.list` / `inbox.summary` answer "what needs a person" from live state:
   pending questions, blocked loops (by reason kind), stopped steps, and — through registered *sources* the sibling slices supply —
   cap hits and stuck loops (S55), failed gates (S57), scheduler items that need you (S27). Every item carries the reason (the
   daemon's own sentence), the loop/item, its age, a severity, and the **exact door a person uses** (a method that exists).
   Sources that are not wired are **named** in the result, never silently empty. The only stored rows are **acknowledge/snooze**
   marks (append-only, attributed).
2. **A declared event-class table** (`notify.events`, a seeded inert record set): event kind (+ reason-kind variant) → tone, title
   and body templates built **only from ids and enums**. It replaces the three-case switch; the three existing hints stay
   byte-identical; `question.asked` and the `blocked` variants are added.
3. **Notification channels as records:** the built-in desktop path stays as is; **one generic outbound channel kind — a signed
   webhook** — is added. A channel is **standing authority to send data off the machine**, so it is human-door only, never seeded,
   carries a secret *reference* (never a value), is allow-listed into the `nettest` invariant, and is delivered by a bounded,
   SSRF-guarded dispatcher with a closed outbound envelope, dedup, rate limit, quiet hours and an append-only **delivery log**.
4. **Background presence in the Tauri shell:** a tray icon and close-to-tray so the existing in-webview listener keeps running
   and OS notifications arrive with the window closed; a deep link back into the app (`aeon://…`) that **navigates and never
   answers**. No new web-layer capability.
5. **A mission-control read** `mission.status {rootId?}` — per root component: running / waiting / blocked / idle loops,
   needs-you counts, spend vs cap, next ready item — with `aeon status` and agent read tools. UI screens are specified, not built.
6. **Daemon first, then CLI (`aeon inbox`, `aeon status`, `aeon notify`), then agent reads** (own-tree), per the standing rules.

## 4. Out of scope (recorded so nothing is lost)

- **Answering from anywhere but the local human door.** Remote answering needs S41's remote human door; the narrow surface stays
  unable to answer. Outbound notifications only *link back*; the link carries no answer right.
- **Email/SMTP, chat-app integrations, mobile push.** One generic signed webhook; a receiver can bridge to anything. A second
  channel kind later is a `CHECK` widening by table rebuild (the S37/S46 precedent).
- Autostart at login, a headless daemon-side notifier, end-of-quiet-window digests (open decisions, defaults in `design.md`).
- The Inbox / Mission / Channels **screens** (specified in `design.md` SV18; built with the UI work after S44-extra).
- Per-person desktop mute — an S44 setting, adopted after S44 merges.

## 5. Acceptance

1. `inbox.list` reproduces `store.awaiting`'s truth for questions and adds blocked/stopped loops; every item's `door` names a
   registered method; unwired sources appear in `sources` as `not-wired`; nothing about attention is cached.
2. The three existing hints are **byte-identical** (characterization goldens); `question.asked` and the `blocked` variants resolve
   from the `notify.events` rows; no template placeholder can carry user text (a sentinel test).
3. A webhook channel is creatable only on the human door; an agent, a loop, the narrow surface and the records catalog cannot
   create, enable, edit or test one (sibling guards with canaries); no secret value appears in any param, result, log, audit
   row or DB byte (sentinel test over the raw store file).
4. The outbound body is the closed envelope; a maximal event with sentinel text in every user-text field leaks none of it.
5. With **zero** channels a full loop makes **no notification connection**; with one, exactly that host; private, link-local and
   metadata addresses are refused at dial time (rebinding-safe); redirects are not followed.
6. Delivery outcomes are append-only and honest (`sent|failed|suppressed-*`, closed error kinds); a dead receiver is reported, not
   retried forever.
7. With the window hidden, an OS notification still arrives (walk); the tray's Quit stops the daemon the app started and says so;
   the capabilities file is unchanged or its diff is justified in `design.md` SV10.
8. `make protogen && make check-protogen && make guard` green; UI `vitest` + lint green; S47's surface table classifies every new
   method.

## 6. Cross-slice requests (adopted by the named slice, never edited from here)

| To | Request |
| --- | --- |
| **S55 LC** | (a) persist a **structured blocked kind** on the loop (today only free text `lastReason`; T0 confirms); (b) an `attention.Source` for `cap-hit` and `stuck` (S55's `loop.raise` is the door the item names); (c) a read seam for **spend and the cap in force** per root component (`BudgetReader`); (d) event hint rows via a `notify.events.loopcontrol` seed set. |
| **S57 VG** | an `attention.Source` for failed verification gates (the door: the gate's evidence view / re-run method) and a `gate.failed` event kind + `notify.events.verify` set. |
| **S27 CN** | an `attention.Source` for items that **need a person** (blocked item, ask-before-advance), a `nextReady` read seam, a `scheduler.needs-you` event kind + `notify.events.scheduler` set. |
| **S56 AL** | optional: when an item is blocked on "outward denied", the `door` names S56's grant method (absent source ⇒ the generic sentence). |
| **S47** | classify `inbox.*`, `mission.status`, `notify.*` in the surface table (`human-socket` / `read`; none `narrow`). |

## 7. Status

**Built 2026-11-03/04 (T0–T10, all eleven tasks; close-out T10 records the evidence).** The attention core shipped
against what exists: questions/blocked/stopped sources report, budget/stuck/gates/scheduler answer
`not-wired (S55/S57/S27)` — exactly the sentence this build-order note predicted, and still honest per source.
Hand-offs reached their siblings: the narrow surface is **annotated, not rewritten** (S19 design.md carries the
S58 annotation; the nine actions are byte-identical, pinned by four pre-amendment digests at base `25a304b0`);
the `hint.v1` field set was widened in place (`deepLink/rootSlug/loopSlug`) with T8 as the consumer; S47's
`tools.GrantedSet` is what proves a loop with every tool still cannot reach a supervision write. Open at hand-off:
the Rust shell (T7) is unbuilt and unverified on the Linux dev box (no Rust toolchain — never fabricated);
remote answering, SMTP and autostart remain unbuilt by design (AUTONOMY-GAPS G7 says what shipped and what did not).

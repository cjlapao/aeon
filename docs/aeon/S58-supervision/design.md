# Design — S58 Supervision

Written 2026-10-30. Rulings **SV1–SV18**. SV6 (what may leave the machine), SV7 (outbound channels are authority) and SV10 (background presence) are
operator-visible product calls; the rest are documented technical calls checked against the code (brief §2 lists the facts; T0 re-verifies).
Do not re-open a ruling without recording the reason in that task's Notes. Task shapes follow [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md); a deviation is recorded here.
Model: the org record is the S44-extra component **tree**; every scope in this slice is a **root component** (`rootId`), never a "project".

## Verified facts (2026-10-30; T0 re-checks)

| # | Fact | Where |
| --- | --- | --- |
| F1 | The event envelope carries `Notify *NotifyHint{Title, Body, Tone}` attached **after** `Events.Append`; the durable log and any replay never carry it. Tone is a closed set | `protocol/types.go:131-149`; `daemon/lifecycle.go:155-165` (`publishWithNotify`) |
| F2 | `notifyHintFor(kind)` is a three-case switch returning fixed titles/tones; `table.OnEvent` is the only caller | `daemon/lifecycle.go:187-197`; `daemon/daemon.go:641-650` |
| F3 | `loop.waiting` carries `questionId` and `prompt`; emitters at `loop/advance.go:193,282,301`, `loop/execute.go:215`, `loop/advance_parallel.go:307`. `question.asked` is published at `protocol/step.go:620,760` and `daemon/recover.go:288` | investigation 2026-10-30 |
| F4 | The pending-question truth is **derived** (`AwaitingSQL`); `store.awaiting {componentRoot?, all}` is the socket read; `question.pending` is the narrow-readable per-loop read | `store/chat.go:385-404`; `protocol/memory.go:522,539` |
| F5 | A blocked loop's reason is free text `LoopView.LastReason`; the reason **kinds** exist as arguments to `blockTo(kind, sentence)` and `BlockedReason` constructions, and runner errors emit `loop.blocked` with kind `runner-error` | `daemon/blueprint.go:93,661`; `loop/state.go:68`; `loop/execute.go:41`; `loop/advance.go:248,403`; `loop/runner.go:63` |
| F6 | A stopped step parks the loop `waiting` with `LastReason="stopped"` and **no** `QuestionID`; only `step.continue` resumes it | `loop/advance.go:285-302`; `protocol/step_stop.go` |
| F7 | Unattended + `ask` ⇒ deny at once, no question row; unattended `then: escalate` collapses to `blocked` | `runtime/loop.go:744`; `loop/escalate.go:105-137` |
| F8 | `AnswerEscalation` has **no socket door** (comment: "the socket's next wiring"); S55 owns wiring it | `loop/escalate.go:307,317-361` |
| F9 | App stack: store key `aeon-notifications-v1`, `MAX_HISTORY=200`; the listener acts only when `event.notify` is present; `notifyOS` returns when `document.hasFocus()` | `ui/src/lib/notifications.ts`; `ui/src/app/NotificationListener.tsx:31-36`; `ui/src/lib/osNotify.ts:36` |
| F10 | Shell: `Builder::default().plugin(shell).plugin(notification).setup(…)`; the app's own commands are not ACL-gated; `RunEvent::Exit` stops the daemon the app started; `tauri` has no `tray-icon` feature enabled; no `on_window_event` | `ui/src-tauri/src/main.rs:170-215`; `Cargo.toml:9-14` |
| F11 | Web window capability = `core:event:default` + `notification:allow-{is-permission-granted,request-permission,notify}`; README: any addition is a reviewed diff with the reason written | `capabilities/default.json`; `capabilities/README.md` |
| F12 | `nettest.Recorder` asserts connections only to an allowed host list (configured routes/forges); N1 "no telemetry" is permanent | `nettest/nettest.go:28-60,109-114` |
| F13 | Inbound webhook HMAC is SHA-256 over the exact payload bytes, hex (`VerifyWebhookSignature`); outbound has no code | `trigger/webhook.go:95-108` |
| F14 | Narrow surface: 9 actions, guard-pinned; `ForbiddenOperationalControl` words include `answer`, `cancel`, `retry`, `fire`; catalog/decide methods may not project onto it | `narrow/actions.go`; `narrow/records_guard_test.go:23-41` |
| F15 | Secret precedent: route key written to `secrets` first, row carries no key; the `secrets` package's exported API was **not readable** in this planning session (sandbox) — T0 reads it | `daemon/memory.go:680-735` |
| F16 | Authority-record precedent: `created_by_kind CHECK (created_by_kind = 'user')`, never seeded, three locks (no agent tool, socket refuses agent actor, CHECK) | S40 AU1; S49 AN2–AN3 |
| F17 | Seed engine contract: sets declared per area in `internal/seed/decl_<area>.go`, classes inert/guard, per-record version guarded by a golden | S43-extra SD2–SD5; MANAGEMENT-PATTERNS P8 |

## SV1 — Attention is derived, never stored

The inbox is a **query over live state**, never a table of "alerts". An alert table is a second truth that drifts from the loop it
describes (the DB6 argument that removed `awaiting_user`; N8: no cache). `inbox.list` evaluates a registry of **sources** on every
call. A source is a Go interface in `internal/attention`:

```go
type Source interface {
    Name() string                                         // "questions", "blocked", "stopped", and later "budget", "stuck", "gates", "scheduler"
    Items(ctx context.Context, q Query) ([]Item, error)   // live rows only
}
```

Built here: **`questions`** (unanswered rows via `store.AwaitingSQL`'s shape, one item per pending question), **`blocked`** (loops with
`status='blocked'`, classified by reason kind — SV2), **`stopped`** (loops `waiting` with `LastReason="stopped"` and no question — a
person stopped a step; the item says "step stopped; `step.continue` resumes it"). Sibling slices register their own sources by the same
interface (S55 `budget`/`stuck`, S57 `gates`, S27 `scheduler` — brief §6). An unregistered source appears in the result's
`sources: [{name, state: "reporting"|"not-wired"|"failed", error?}]`, so "nothing needs you" can never mean "nothing was looking". A
source that errors yields `failed` and does not hide the others.

`Item` (wire `AttentionItem`):

| Field | Meaning |
| --- | --- |
| `key` | stable subject key `<source>:<id>:<since>` — a **recurrence has a new `since`**, so it is a new key (SV3) |
| `kind` | closed per source (`question`, `blocked.<reasonKind>`, `stopped`, `cap-hit`, `stuck`, `gate-failed`, `needs-you`) |
| `severity` | closed: `needs-you`, `stuck`, `blocked`, `stopped`, `info` (SV2) |
| `rootId`, `componentId?`, `loopId?`, `loopSlug?`, `itemRef?` | where (ids/slugs only) |
| `reason` | **the daemon's own stored sentence**, verbatim (e.g. `LastReason`, the question prompt is *not* copied — see SV6; the inbox is the local, human door so it may show the prompt via a separate field `prompt?`, local-only) |
| `since`, `ageSeconds` | from the source row; age computed at read |
| `door` | `{method, sentence}` — the exact action a person takes (SV2) |
| `acked?` | `{by, at, until?}` when an ack/snooze applies (SV3) |

`inbox.summary {rootId?}` → `{total, bySeverity{…}, oldestSince?, sources[…]}`. Both honour a root component's **tree** (a root includes its
children; a child id is accepted and scopes to that child).

## SV2 — Severity and the door are fixed tables of *shape*, not records

Severity classes and their order are code (a closed vocabulary, like the policy vocabulary — "shape, not records", rule 6):
`needs-you` (a question, an escalation, a cap hit, a failed gate, a scheduler item) > `stuck` (S55 watchdog) > `blocked` > `stopped` > `info`.
Within a severity, **oldest first**. Each `kind` has one **door template** — a fixed sentence built from ids and enums, naming a method that exists:

| kind | door |
| --- | --- |
| `question` | `answer` — "Answer with `aeon answer <loop> <one of: …options>` or in the app" (options from the question row) |
| `blocked.action-denied` | "This loop is unattended, so outward actions are denied. Start an attended run, or (S56) grant the action." |
| `blocked.step-blocked` / `model-error` / `runner-error` | `loop.retry…` only where it exists today (`retry_handler_block` for missing-handler blocks, F5); otherwise "read the reason; start a new run" |
| `blocked.exhaustion` | the ceiling sentence + `loop.raise` **when S55 is wired**, else read-only |
| `stopped` | `step.continue {stepId}` |

**A door may name only a method that is registered** (S19's F-1 lesson: the boundary statement once named a phantom `aeon loop stop`). T2 ships
`TestEveryDoorNamesARegisteredMethod`, which resolves each template's method against `protocol.Table.Methods()` and, for a door gated on an
unbuilt sibling, asserts the **generic** sentence is used instead. The door templates are **data in `internal/attention/doors.go`** with a golden; adding
a kind without a door fails the test.

## SV3 — Acknowledge and snooze: the only stored attention rows

`attention_ack` (append-only **by trigger**, P3): `id`, `subject_key`, `op ∈ ('ack','snooze','unack')`, `until` (RFC3339, snooze only), `actor_kind`,
`actor_id`, `occurred_at`. Latest row per `subject_key` wins. An ack **only hides the item from the default view** (`includeAcked` shows it); it changes
nothing about the loop, resolves nothing, and is **never an agent tool** (an agent hiding problems from the person is the failure this slice exists to
prevent). The actor comes from `table.ActorFor` (kernel peer), never from params. Because `key` embeds `since`, a **recurrence is a new key** and an old
ack cannot hide it. A snooze has a maximum horizon (7 days, a named constant). Acked/snoozed items still count in `summary.total` under `acked` so the
number never lies. Audit summary text is a fixed template from ids/enums (SEC-033).

## SV4 — The event-class table: `notify.events`, a seeded inert record set

`notify_event_class(kind TEXT, variant TEXT NOT NULL DEFAULT '', tone, title_template, body_template, enabled INTEGER, origin, version, updated_at,
PRIMARY KEY (kind, variant))`. `variant` is the **reason kind** for `loop.blocked` (e.g. `action-denied`, `cap-hit`) and empty otherwise. Resolution is
`notify.Resolver.HintFor(kind, payload)`: the row for `(kind, variant)`, else `(kind, '')`, else **no hint**. It replaces `notifyHintFor` at the single
call site (`daemon.go:641-650`) — **no cache** (N8; the table is a handful of rows, one indexed read per event).

- **Templates** use a **closed placeholder set**, all from ids/enums: `{rootSlug}`, `{loopSlug}`, `{reasonKind}`, `{severity}`, `{count}`. **No
  placeholder can carry user text** (component *names*, goal text, a question prompt, model output, a file path). The validator refuses any
  other `{…}`; the **test supplies a sentinel in every user-text field and asserts it never reaches a rendered template** (SV6's rule, enforced at the
  source). Length ≤ 120 / 240 bytes.
- **Seed set `notify.events`** (class **inert**, P8): `loop.finished`/`loop.blocked`/`loop.waiting` rows **byte-identical to today's three titles and tones**
  (P2 characterization, T1/T4), plus `question.asked` (info, dedup-aware — SV7), and `loop.blocked` variants for the reason kinds that exist today
  (`action-denied`, `step-blocked`, `exhaustion`, `model-error`, `runner-error`, `stopped`). Siblings declare their own sets in their own files — `notify.events.loopcontrol`
  (S55: `cap-hit`, `stuck`), `notify.events.verify` (S57: `gate.failed`), `notify.events.scheduler` (S27: `scheduler.needs-you`) — so parallel worktrees add files, not edits (SD2).
  Rows for a kind no emitter produces yet are **not** seeded early.
- **Operator rights** `{Edit: enabled/tone/templates within the closed placeholders, Remove: disable}`; an operator edit is never overwritten by a re-seed (SD rule), a disabled class
  stays disabled. `notify.class.update` is the human-door write (SV13); every change is audited (`notify_change`, SV12).
- `NotifyHint` gains two optional fields — `attentionKey` and `deepLink` (SV9) — additive; `protogen` regenerates `contract.ts`.

## SV5 — Where hints go: three consumers, one resolution

One resolution per event, three consumers: (1) the **delivery-time hint** on the wire (today's path; the app toast + OS notification), (2) the **dispatcher**
(SV8) that feeds outbound channels, (3) nothing else. The hint stays delivery-time only (F1: a reconnect's catch-up must not re-toast). The dispatcher
runs in-process off `table.OnEvent` **after** `Events.Append`, never inside the loop's transaction — a slow or dead receiver cannot delay a loop (S19 T7's
"a slow reader holds nothing" rule: bounded queue, drop-oldest with a recorded `suppressed-overflow`).

## SV6 — What may leave the machine *(operator-visible product call)*

Aeon's trust basis is "reads your code and runs commands, sends nothing anywhere you did not configure" (N1/C11). A channel is the first
daemon-originated message to a person-configured host, so its content is a **closed envelope of ids and enums**:

```json
{"v":1,"id":"<delivery id>","at":"<RFC3339>","event":"loop.blocked","severity":"blocked","kind":"blocked.action-denied",
 "rootSlug":"shop","loopSlug":"…","title":"<template result>","deepLink":"aeon://loop/<id>"}
```

- **Never** in the envelope: a question prompt, a goal, a model message, a file path, a command, a cost, a component *name*, a branch, a token. (Prompts and
  commands can contain code or secrets.) `title` is the SV4 template output (ids/enums only).
- ≤ 1 KiB, a fixed key order, `Content-Type: application/json`, `User-Agent: aeon/<version>`; a body **signature** header `X-Aeon-Signature: sha256=<hex>` = HMAC-SHA256
  over the exact body bytes with the channel secret (the **outbound mirror** of `trigger.VerifyWebhookSignature`, F13) plus `X-Aeon-Delivery` and `X-Aeon-Timestamp` (replay window is the receiver's).
- A **sentinel test** (T5): a maximal event whose every user-text field holds a unique sentinel; the captured request body contains none of them and equals the golden envelope.
- Local surfaces (`inbox.list`'s `prompt?`, the in-app toast) may show more because they never leave the machine; the field is marked `local` in the contract and the outbound builder cannot read it (the builder takes the closed `Envelope` struct, not an `Item`).

## SV7 — Channels are authority records: human door, never seeded *(operator-visible)*

A webhook channel is **standing authority to send events off the machine** (an exfiltration path if an agent could author one). So it follows the S40/S49
authority pattern (F16): **three locks**, each tested — (1) **no agent tool** can create, edit, enable, disable, archive or test a channel; (2) the socket refuses an
actor of kind `agent` and an unresolvable actor (`table.ActorFor`); (3) `notify_channel.created_by_kind CHECK (created_by_kind = 'user')`. Channels are rows (rule 6) and are
**never seeded** (the named exception — AU1 lock 3 / S49 AN2). They are never deleted: `archived_at` with a reason; the delivery log outlives the channel (no FK from `notify_delivery`).

`notify_channel(id, kind CHECK (kind IN ('webhook')), name, enabled, url, secret_ref, allow_private INTEGER NOT NULL DEFAULT 0, created_by_kind, created_by_id, created_at, updated_at, archived_at)`;
`notify_subscription(channel_id, root_id NULL = every root, min_severity, kinds JSON NULL = all, quiet_start, quiet_end, quiet_zone, max_per_hour)` — one row per (channel, root).

- **Secret by reference.** `secret_ref` is `secret:notify/<channelId>`; the value lives only in the secrets store (F15). The value is supplied **from a file path** on the daemon host
  (`secretFile`, the `--key-file` precedent; no flag, stdin or prompt accepts a value — C17), read once, written to the store **first**, never returned, logged, audited or put in a row;
  a test reads the **raw database bytes** and the daemon log for the sentinel (the route-key test's shape). `notify.channel.show` reports `secretPresent: bool`, nothing else.
  *Open decision SV-D2: generate-once reveal.* T0 reads the `secrets` package's exported API (not readable in the planning session) and records whether a per-channel accessor is a small addition there.
- **URL rules.** `https` only (an `http` loopback URL is allowed solely when `allow_private=1`, for local receivers); no userinfo; ≤ 2 KiB; host must resolve.
- **Test.** `notify.channel.test {id}` sends one fixed envelope (`event: "test"`) — an outbound act, so human door, `--yes`, and logged like any delivery.

## SV8 — The dispatcher: bounded, SSRF-safe, honest

`internal/notify.Dispatcher` consumes resolved events and, per matching (channel, subscription): severity/kind filter → **dedup** → **rate limit** → **quiet hours** → send → log. Rules:

- **Dedup** by `(channelId, attentionKey)` within 10 minutes (a constant): `loop.waiting` and `question.asked` for the same question send once.
- **Rate limit** `max_per_hour` per subscription (default 30, 1–1000 — the S40 limit shape); over the limit ⇒ `suppressed-rate`, counted, and **one** summary delivery per hour says how many were suppressed.
- **Quiet hours** (`quiet_start/end` HH:MM + an explicit IANA zone, TR6's rule): suppress outbound sends; recorded `suppressed-quiet`. The inbox is unaffected.
- **Send** — one `http.Client` with `Timeout 10s`, **no redirects followed**, response body read ≤ 4 KiB and discarded, **no cookies**, a custom `DialContext` that resolves the host and **refuses loopback, link-local, private (RFC1918), CGNAT, multicast and cloud-metadata addresses (169.254.169.254, fd00:ec2::254) unless the channel has `allow_private=1`** — checked on the **dialled IP**, not the URL, so DNS rebinding and `http://127.0.0.1.evil` tricks fail. Up to 3 attempts, exponential backoff 1 s/4 s, **only** for transport errors, timeouts and 5xx/429 (honouring `Retry-After` ≤ 60 s); a 4xx is final. A bounded in-memory queue (256) ahead of the senders; overflow drops oldest and logs `suppressed-overflow`.
- **Failure honesty.** `notify_delivery(id, channel_id, event_seq, attention_key, outcome, error_kind, http_status, attempts, at)`, append-only by trigger, **no body, no URL, no response text**. `outcome ∈ ('sent','failed','suppressed-quiet','suppressed-rate','suppressed-dedup','suppressed-disabled','suppressed-overflow')`; `error_kind ∈ ('transport','timeout','status','refused-address','secret-missing','url-invalid')`. "Daemon was down" is not a row — it is unknowable — so on **daemon start**, if attention items exist, the daemon sends **one digest** ("N items need attention", kind `inbox.digest`) per enabled channel and records it; missed per-event messages are **never replayed** (stale).
- **Isolation.** A channel failure never blocks a loop, never raises an error on the event path, and never disables the channel by itself; `doctor` and `notify.channel.list` show `lastOutcome` and `consecutiveFailures`.
- **Allowlist.** The `nettest` allowed list gains "the host of every enabled, non-archived channel" — and **only** those (SV14).

## SV9 — Deep links back, never answers

`NotifyHint.deepLink` and the envelope `deepLink` are `aeon://loop/<loopId>`, `aeon://inbox`, `aeon://root/<rootId>` — **navigation targets only**. The shell registers the scheme (SV10) and routes it to the router; opening a link
shows the loop or the inbox with the **normal** local door. The link carries **no token, no answer, no capability** (S19 T5's permanence: nothing off the local human door answers). Unknown or malformed links are dropped with a visible "link not understood" toast; a link for a loop that no longer exists says so. Remote answering is out (SV15).

## SV10 — Background presence in the shell *(operator-visible product call)*

**Problem (F9/F10):** the OS notification is raised by JavaScript in the webview, and the webview dies when the window is closed — so with the window closed, nothing notifies.

**Decision: keep the webview alive, hidden.** Rust-side only:
1. `tauri = { version = "2", features = ["tray-icon", "image-png"] }` — a **Cargo feature**, not a new crate family; the tray icon is built in `setup` with a menu: *Open Aeon*, *Inbox*, a disabled *"N need you"* line (updated from the `inbox.summary` the existing session already reads), *Quit Aeon*.
2. `on_window_event`: `CloseRequested` on the `main` window ⇒ `api.prevent_close()` + `window.hide()` **when** a daemon session is live (`AppState.session` is `Some`); otherwise the window closes normally (a window the app cannot keep useful is not hidden). Pure function `close_policy(has_session, quitting) -> {Hide, Close}` — unit-tested.
3. The webview keeps running, so `NotificationListener` keeps its subscription; `document.hasFocus()` is false for a hidden window ⇒ `notifyOS` fires. **No change to the web layer's capabilities** (F11): tray, hide, show and quit are Rust-side calls, not web-granted commands.
4. **Quit** (tray or Cmd/Ctrl-Q) is the only way to stop background presence and runs the existing `RunEvent::Exit` path (stops the daemon this app started). The tray item reads **"Quit Aeon (stops the daemon this app started)"** when `local_daemon` is ours, "Quit Aeon" when it is not — the honesty rule: a quit that silently kills unattended work is a surprise.
5. **Deep link:** register `aeon://` via `tauri-plugin-deep-link` (a Rust crate + `tauri.conf.json` entry); handle `on_open_url` **in Rust**, show/focus the window, and `emit("aeon://navigate", target)` on the existing event channel family — the web layer already holds `core:event:default` (`listen`), so **no new capability**. The only new permission-shaped surface is the OS URL scheme, justified in T7's Notes and reviewed in the diff (F11's rule).

**Rejected — a Rust-side second subscriber** that sends OS notifications itself: it duplicates the subscription/reconnect/dedup logic in a second language, needs its own copy of the hint rules, and still would not cover the daemon-down case. **Rejected — autostart at login** for v1 (an operator decision; SV-D5). On Linux the tray needs an app-indicator host; where absent the shell falls back to normal close and says so in the doctor panel (T7). The hidden webview's memory cost is accepted and measured in T7.

## SV11 — Mission status

`mission.status {rootId?}` → `{evaluatedAt, roots: [{rootId, name, kind, childCount, loops{running, waiting, blocked, idle}, needsYou{total, bySeverity}, spend{state: "reporting"|"not-wired", usdAtLeast?, costState?, cap?{usd, source}}, nextReady{state, item?}, lastActivityAt?}], sources[…]}`.
Loop counts are derived from `loop.status` (the vocabulary pinned by the existing tests: `running|waiting|blocked|idle|done|aborted`); spend comes from the telemetry rollups of the **S44-extra component rows** (a root's row is the subtree total — CT7) — and the **cap** from S55's `BudgetReader` seam; `nextReady` from S27's seam. Each group carries `state` so an unwired seam is `not-wired`, never zero. **No composite "health score"** (an invented number). `needsYou` equals `inbox.summary` for the same scope (a consistency test). Cost is the P32 floor, shown as a floor.

## SV12 — Audit: `notify_change`, once, append-only

Every change to channels, subscriptions and event classes writes one `notify_change` row (append-only by trigger, P3) **in the same transaction**: `id, subject_kind ∈ ('channel','subscription','class'), subject_id, op, actor_kind, actor_id, occurred_at, summary` (a **fixed template** from ids and enums — "channel <id> enabled" — never a URL, host or template text). Reads, dry-runs and refusals write nothing; the delivery log is a separate table with its own purpose. History outlives the record (no FK).

## SV13 — Methods (daemon first; all `RequiresInitialize`, none `Debug`)

| Method | Kind | Door |
| --- | --- | --- |
| `inbox.list`, `inbox.summary` | read | human socket + agent read tools (own tree) |
| `inbox.ack`, `inbox.snooze`, `inbox.unack` | write (audit) | **human socket only** |
| `mission.status` | read | human socket + agent read tool (own tree) |
| `notify.vocabulary` | read (descriptor: kinds, severities, placeholders, error kinds, channel kinds, limits) | human socket |
| `notify.class.list`, `notify.class.update` | read / write | list: socket + agent read; update: **human only** |
| `notify.channel.list`, `notify.channel.show` | read (host only, `secretPresent`, last outcome) | socket + agent read (**host and counts only**) |
| `notify.channel.create`, `update`, `enable`, `disable`, `archive`, `test` | **authority writes** | **human socket only** (SV7 locks) |
| `notify.subscription.set` | authority write | human socket only |
| `notify.delivery.list {channelId?, outcome?, limit, before?}` | read | socket |

**Narrow surface: none** (default; SV-D3). The narrow monitors already have `question.pending` and `store.awaiting`'s truth; the inbox carries blocked-reason sentences and doors that widen what an outside key sees, and the narrow table is guard-pinned at 9. A later slice may mount a read-only `inbox.summary` by amending the guard with its argument.

## SV14 — No telemetry, still: the `nettest` invariant extended

`nettest`'s allowed list builder gains `notify.ChannelHosts(store)` (enabled, non-archived channels). Two permanent tests: (1) **zero channels ⇒ a full loop makes no notification connection** (the recorder sees only routes/forges); (2) **one channel ⇒ exactly that host**, once per delivery, with the SSRF dialer's refusals proven by tests against loopback/private/metadata fixtures. The N1 sentence in `AGENTS.md` gets one clause: *"Notification channels are person-configured destinations (S58 SV7), never telemetry; with none configured the daemon contacts nothing new."* A channel is configured by a person on the human door; no default channel exists in any seed.

## SV15 — Remote answering is explicitly out

S41's daemon listener is design-only (F: `S41-workspaces/design.md`); the narrow surface is permanently read-only for questions (S19 T5). This slice does **not** add a way to answer from a phone, a browser or a webhook reply. Outbound notifications link **back to the local app** (SV9). When S41 lands a remote human door, an inbox `door` may name it; until then the `door` text says "answer in the Aeon app on the machine running the daemon".

## SV16 — Agent tools (rule 2; one definition, two doors — P6)

Catalog specs in `internal/records/catalog.go`, family `supervision`, all **read-only** (`Mutates: false`, class `safe`), **own-view**: an agent sees attention and status for **its own root component's tree only** (the MC2/own-view rule; `Identify` supplies the root). `inbox_list`, `inbox_summary`, `mission_status`, `notify_class_list`, `notify_channel_list` (host and counts only — never URL path, secret ref or subscription detail). **No write tool exists**: acknowledging hides problems from the person, a channel is authority — stated in the spec comment and enforced by the T9 guards. The `TestRegistryExactNames` bump is counted and dated (+5); registered, not granted.

## SV17 — Storage plan (one timestamped migration)

`<date +%Y%m%d%H%M%S>_supervision.sql`, one transaction, with a leading `-- ` description line; tables `attention_ack`, `notify_event_class`, `notify_channel`, `notify_subscription`, `notify_delivery`, `notify_change`; append-only triggers on `attention_ack`, `notify_delivery`, `notify_change` (the `component_tree_*`/`setting_event` naming style); indexes on `notify_delivery(channel_id, at)`, `attention_ack(subject_key, id)`. No existing table is rebuilt. SQL lives only in `internal/store` (I2: `store/attention.go`, `store/notify.go`; `TestNoSQLLivesOutsideTheStore`). Fresh == upgraded (`TestAFreshSchemaAndAnUpgradedSchemaAreIdentical` extended); literal migration-count assertions bumped by **+1** (T0 lists them). `notify_channel.kind` widening later = table rebuild reading the **current** DDL.

## SV18 — UI specification (built after S44-extra; this slice builds only the adapters)

*Inbox* (route `/inbox`): one list from `inbox.list`, grouped by severity, each row = reason, root component, age, the door as a button or a copyable command; filters (`rootId`, severity, kinds from `notify.vocabulary`); ack/snooze with the daemon's own confirmation; a **sources strip** ("questions ✓ · blocked ✓ · stuck — not wired"); refusals verbatim. *Mission* (the home for a root): the `mission.status` row per root component with spend vs cap (floor labelled), next ready item, and a link per count into the filtered inbox. *Settings → Notifications*: event classes (list with details, edit within the placeholders), channels (list with host, enabled, last delivery outcome, failures; create with `secretFile` path; test; archive with confirm showing the daemon's statement that **events leave this machine**), delivery log. P7 rules apply (descriptor-driven, `useConfirm`, rows from the response, en/pt parity, `*Rpc.guard.test.ts`, no hardcoded vocabulary). T8 builds only: the hint consumer (`attentionKey` dedup + `deepLink` routing) and the `aeon://navigate` handler.

## Open decisions (recommended defaults)

| # | Decision | Default |
| --- | --- | --- |
| SV-D1 | Channel kinds in v1 | `webhook` only; SMTP/chat/push later by `CHECK` widening |
| SV-D2 | How the HMAC secret is supplied | from a **file path** on the daemon host; **not** generate-once-reveal (revisit if the UI needs it) |
| SV-D3 | `inbox.summary` on the narrow surface | **no** (guard pinned at 9; widen later with the argument) |
| SV-D4 | Quiet-hours behaviour | suppress outbound sends, record `suppressed-quiet`; **no** end-of-window digest in v1 |
| SV-D5 | Autostart at login | **no** (operator opt-in later; not in v1) |
| SV-D6 | Digest at daemon start | **yes**, one per enabled channel, only when items exist |
| SV-D7 | Deep-link scheme | `aeon://` via `tauri-plugin-deep-link`, handled in Rust, no new web capability |
| SV-D8 | Per-person desktop mute | an **S44 setting**, adopted after S44 merges (not here) |
| SV-D9 | Ack by agents | **never** |
| SV-D10 | Dedup window / rate default | 10 min / 30 per hour (named constants) |
| SV-D11 | Snooze horizon | 7 days maximum |
| SV-D12 | Structured blocked kind | S55 persists it; until then T0 decides between classifying from the durable `loop.blocked` payload or a `LastReason` prefix table — default: the event payload, fixture-pinned |

## Security notes

Human-door authority for anything that sends data out (SV7); closed envelope of ids/enums (SV6); SSRF-safe dialer with IP-level checks and no redirects (SV8); secrets by reference only, proven against raw DB bytes and logs (SV7);
append-only delivery/audit logs with fixed-template summaries (SV12); `nettest` invariant preserved (SV14); the narrow surface unchanged (SV13); no new web-layer capability (SV10). Any outbound text shown to a person as a channel-test preview is **external-facing and marked for human review** in the UI copy.

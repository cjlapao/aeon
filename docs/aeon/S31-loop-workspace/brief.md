# Brief — S31 Loop workspace (backend + shell)

**Implements:** the backend half of
[`docs/superpowers/specs/2026-10-24-loop-chat-design.md`](../../superpowers/specs/2026-10-24-loop-chat-design.md) —
read that first; it is the screen this slice feeds. **Built.** T1–T9 closed 2026-10-24 (backend/shell);
**T10** (added 2026-10-26, documenting work actually built 2026-10-25) gives the designed UI — N1's own
"next piece of work" — a home in this slice rather than leaving it as prose with no task entry; **T11**
(added 2026-10-26) is the spec for F-22/F-26, the two findings T9's own walk named and nobody had yet
picked up. **Demo:** [`../DEMO.md`](../DEMO.md) — walked at T9, with a second `## S31 (UI)` section for T10.
**Plan / tasks:** split into [`tasks/T1–T11.md`](tasks/) (§6 below is the original T1–T9 plan, kept verbatim
as written; T10/T11 are new entries, not a rewrite of it).
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — P2, P25, P26, P27, P28, P29, P31 and **P33** are
binding. **Prefix:** `LW`.
**Stage:** 5 · Fleet, but **buildable now** — it needs only S2–S9 (all built). It does **not** depend on S11, S13
or S16, and must not wait for them. **Feeds:** the loop-workspace UI (the next piece of work, not in this slice) and
**S32** (parallel agents), which inherits this slice's event shape unchanged.
**Repo:** `aeon/` (P24) · Go 1.26 · `cgo` on (B3) · Tauri shell in `aeon/ui/src-tauri/` (Rust).
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`) — trivially compliant: this slice has no CLI
surface at all; every capability (§5) was a socket method or notification from the first task, because it
exists specifically to feed the app's own loop-workspace screen.

---

## 1. What this is

**Everything the chat-first loop screen needs from the daemon and the Rust shell, so the UI work that follows is
UI work only.** The screen shows a running loop as a live chat — the orchestrator's moves and every agent's
thinking, tool calls, outputs and file edits — lets a person **steer** an agent mid-run, change its provider and
effort, stop it, see the project's git state and file diffs, and use a terminal in the workspace.

Today almost none of that reaches the UI while a loop runs. This slice closes the gaps, in the daemon and the
shell, with tests, and stops there.

## 2. What exists, and the gaps (measured 2026-10-24, HEAD `7550cbc` + working tree)

| Need | Today | Gap |
| --- | --- | --- |
| Live step events | `runtime.StepEvent` → `step.started/done`, `step.tool.start/end`, `model.delta`, `step.compacted`, published through `Table.OnEvent` → `Daemon.publish` → `EventLog` (bounded ring, durable `seq`) → subscriptions → Rust `session.rs` → Tauri event `aeon://event` | **Loop-run steps discard their events.** `daemon/blueprint.go:123` calls `RunStepForLoop` → `handleStep(ctx, nil, raw)` with no `SubscriptionID`, so `handleStep` uses `runtime.DiscardSink{}` (`protocol/step.go:311`). **`stepSink` omits `StepID`** (`protocol/step.go:646-662`; only the resume sink at `:532` sets it). **`Daemon.publish(kind, "", payload)`** passes an empty project root (`daemon/daemon.go:197`), so `Filter.ProjectRoot` cannot select a project's step events |
| Orchestrator events | `loop.started/advanced/waiting/skipped/blocked/finished` (`internal/loop/advance.go`, `daemon/blueprint.go`) | No event says **"step X dispatched to agent Y, stepId N"**, so the UI cannot link a loop move to the step stream it started |
| Stop | `step.stop` / `step.continue` (`protocol/step_stop.go`, `daemon/stepcancel.go`) with route + effort override on continue | **Does not work for loop steps** — the file's own doc says so (*"What this does NOT do"*). `step.continue` takes no message |
| Steer | — | **Nothing.** No queue, no injection point used |
| Provider/effort mid-run | `RunLoop` re-reads `st.Request.Route` and `ReasoningEffort` every iteration (`runtime/loop.go:181-190`) | No way to change them **between turns** of a running step |
| Agent identity | `StepParams.Agent`, `step.agent` column | Not on the event payload |
| Files | `loop.files` / `loop.file.read` (one directory level, hides dotfiles/`node_modules`/`vendor`) — **and** Rust `workspace_files.rs` (`workspace_list`/`workspace_read` Tauri commands) | No changed-file marker, **no diff**. `write_file`'s result reports bytes, not lines (`tools/files.go:103`) |
| Git state | — | **Nothing** (S16, git actions, is specced not built) |
| Terminal | `tools/spawn.go` is the bounded, non-interactive process layer (stdin closed, timeouts, process-group kill) | **No PTY, no interactive session**, and `session.rs:556` forwards only notifications whose method is `"event"` |

## 3. Definition of Done

| | |
| --- | --- |
| **D1** | **A step run by a loop streams live.** Every `step.*` and `model.delta` event of a loop step reaches a subscriber, carrying `stepId`, `loopId`, `lane`, `step` (the blueprint step id), `agent` and `projectRoot`, and is selectable by `Filter.ProjectRoot` |
| **D2** | **The orchestrator says what it dispatched.** A new `loop.step.dispatched` event (`loopId`, `lane`, `step`, `stepId`, `agent`, `route`, `reasoningEffort`) precedes the step's first event |
| **D3** | **Steer.** `step.steer` queues a message for a running step; it is injected at the next turn boundary, **including** when the turn ended in tool calls — those calls are then not run, each answered with a synthetic "not run: the person steered" result, and the model decides what to do next |
| **D4** | **A queued steer is durable.** It is a row (`pending` → `delivered` \| `cancelled`), so it survives a daemon crash, and its delivery is recorded **in the same transaction** as the checkpoint that contains it — never delivered twice, never lost |
| **D5** | **Provider and effort change from the next turn.** `step.steer` may carry `route` and/or `reasoningEffort` (with or without text); they apply from the next model call, stick for the rest of the step, are persisted so recovery replays with them, and are announced by an event |
| **D6** | **Stop and continue work on loop steps**, and `step.continue` accepts a `message`, so *stop-and-inject* is `step.stop` then `step.continue {message}`. A stopped loop step parks the loop (`loop.waiting`, reason `stopped`) — never `failed`, never `blocked` |
| **D7** | **Git status, read-only, three-state.** `workspace.git.status` answers `ok` (branch, upstream, ahead/behind, changed paths with status) \| `not-a-repo` \| `unavailable` (with the reason) — never a reassuring empty list for "could not tell" (P31) |
| **D8** | **Diffs.** `workspace.git.diff` returns a unified diff for one path (working tree vs `HEAD`; untracked files as all-added). `step.tool.end` for `write_file` carries `linesAdded`/`linesRemoved` |
| **D9** | **A terminal.** The daemon owns PTY shell sessions in a registered project root: open, input, resize, attach (with bounded scrollback), close, list. Output and exit reach the app as their own notifications, **not** through the event log |
| **D10** | **The Rust shell forwards terminal notifications** as a Tauri event `aeon://terminal`, with the same "report a failed emit, never discard it" rule `AppSink` already follows. **No new Tauri command** — the generic `rpc_call` bridge carries every new method (AGENTS §4b) |
| **D11** | **Additive protocol.** Every new method and event has conformance fixtures; `make protogen` regenerates `ui/src/proto/contract.ts`; `PROTOCOL_VERSION` is unchanged; `make ci` is green |
| **D12** | **P33-ready.** Nothing here assumes one running step per loop: steer, stop, continue and every event are keyed by `stepId`, never by loop |

**D1 and D4 are the two that matter.** D1 because without it the screen cannot be live at all. D4 because a
steer that is silently lost after a crash, or injected twice after a replay, is the kind of wrong nobody notices
until an agent does something twice.

## 4. Decisions (`LW`)

| # | Decision | Why |
| --- | --- | --- |
| **LW1** | **Steer injects at a turn boundary, never mid-stream.** The in-flight model call always finishes | `step_stop.go` already argues it: changing the ground under a running call is not "the next call reasons differently". Stop exists for "now" |
| **LW2** | **Pending tool calls are answered, not dropped.** Each unexecuted `tool_use` gets a `tool_result` (`isError: true`, text *"not run: the person steered before this tool ran — read their message below and decide whether to call it again"*), then the steer text follows **in the same user message**, after the results | Providers reject a `tool_use` with no matching `tool_result`. The model, not the runtime, decides whether the tool is still wanted — which is what the user asked for |
| **LW3** | **A `question` tool call in the same turn is answered the same way** (not run; the step does not suspend) | The person is present and talking; suspending to ask them something they may have just answered is wrong. Recorded so it is not rediscovered |
| **LW4** | **Steer is refused on a step that is not `running`** — `suspended` → use `answer`; stopped → use `step.continue {message}`; terminal → refused. Each refusal names the state and the method to use | DB19: a named, discriminated refusal, never a silent no-op |
| **LW5** | **Delivery and checkpoint are one transaction.** The runtime asks a `Steers` dependency for pending steers, appends them, and the checkpoint that persists the new transcript also flips those rows to `delivered` | The only way D4 holds under `kill -9` at any instruction. S7's replay then sees either "not delivered" (re-drain) or "in the transcript and delivered" — never both, never neither |
| **LW6** | **Route/effort overrides persist on the step's saved request** (what `LoadResume` returns as `saved.Request`), and `compile.go`'s tools-vs-reasoning rule runs again on the new pair | Recovery must replay with what the step was actually using; the compatibility rule exists for a reason and an override does not bypass it |
| **LW7** | **Terminal output never enters the `EventLog`.** It goes by `localrpc.Notify` to attached connections, as method `terminal.output` / `terminal.exit` | The log's `seq` is durable (one allocation per event) and its ring is shared by every subscriber; keystroke-rate output would evict real events and burn `seq`. A terminal is a stream, not history |
| **LW8** | **A terminal is not governed by policy.** It is the person's own shell, under their own uid, on a socket only they can open (P26) | Policy governs what *agents* may do (§3). Gating the user's shell behind policies the user writes is theatre. **And it must never be exposed on the narrow HTTP surface (S19)** — add a guard test mirroring `TestNoPolicyAuthoringReachesTheNarrowSurface` |
| **LW9** | **Terminals outlive a connection, not the daemon.** Detached sessions are reaped after 15 minutes with nobody attached; all are killed (whole process group, as `tools/spawn.go` does) on daemon shutdown | A window reload must not kill a running `make test`; a forgotten shell must not live forever (P28) |
| **LW10** | **Bounded everything.** Scrollback ring 1 MiB per terminal; at most 8 terminals per project root; output coalesced to one notification per 16 ms or 32 KiB, whichever first; steer text ≤ 32 KiB; at most 16 pending steers per step | P28. Each bound is a guess — record the measured reality in the task notes |
| **LW11** | **Git is read-only here, through the bounded process layer.** `git status --porcelain=v2 --branch -z` and `git diff`, via `tools.Run`-style bounded execution with a timeout. S16 later owns git *actions* and may absorb these reads | No second, unbounded process path. Read-only cannot conflict with S16's effect classes |
| **LW12** | **Windows terminals are `unsupported` in this slice** — `terminal.open` refuses with a named error on Windows. ConPTY is later work | Three-state honesty: say it is not there rather than ship a half-working one |

## 5. Protocol additions

All additive. Names follow the existing table (`internal/protocol/method.go`). Errors are named
(`ErrX.WithWhat(...)`), never bare strings.

### Methods

| Method | Params | Result |
| --- | --- | --- |
| `step.steer` | `stepId`, `text?`, `route?`, `reasoningEffort?` (at least one) | `{steerId, state: "pending"}` |
| `step.steers` | `stepId` | `[{steerId, text, route?, reasoningEffort?, state, createdAt, deliveredAt?, iteration?}]` |
| `step.steer.cancel` | `steerId` | `{state}` — only a `pending` steer can be cancelled |
| `step.continue` *(extended)* | adds `message?` | unchanged |
| `workspace.git.status` | `projectRoot` | `{state: "ok"\|"not-a-repo"\|"unavailable", reason?, branch?, detached?, upstream?, ahead?, behind?, changed?: [{path, index, worktree, origPath?}]}` |
| `workspace.git.diff` | `projectRoot`, `path` | `{state, diff?, binary?, truncated?}` — capped (e.g. 512 KiB), truncation reported |
| `terminal.open` | `projectRoot`, `cols`, `rows` | `{terminalId, shell, cwd}` |
| `terminal.input` | `terminalId`, `data` (UTF-8 string) | `{}` |
| `terminal.resize` | `terminalId`, `cols`, `rows` | `{}` |
| `terminal.attach` | `terminalId` | `{scrollback (base64), seq, exited?, exitCode?}` — attaches the calling connection |
| `terminal.detach` | `terminalId` | `{}` |
| `terminal.close` | `terminalId` | `{exitCode?}` |
| `terminal.list` | `projectRoot?` | `[{terminalId, projectRoot, cwd, shell, createdAt, attached, exited}]` |

`projectRoot` must be a **registered project root** (`project.list`) — anything else is refused with the path named.
Paths in `workspace.git.diff` go through the same resolver as `loop.file.read` (`tools.Resolve`) — no escaping the
root.

### Events (through the `EventLog`)

| Kind | Payload |
| --- | --- |
| every `step.*`, `model.delta` | existing `StepEventPayload` **plus** `stepId` (always), `loopId?`, `lane?`, `step?`, `agent`, published with the step's `projectRoot` |
| `loop.step.dispatched` | `loopId`, `lane`, `step`, `stepId`, `agent`, `route`, `reasoningEffort` |
| `step.steer.queued` | `stepId`, `steerId`, `hasText`, `route?`, `reasoningEffort?` |
| `step.steer.delivered` | `stepId`, `steerId`, `iteration`, `skippedTools: [toolUseId…]` |
| `step.steer.cancelled` | `stepId`, `steerId` |
| `step.tool.skipped` | `stepId`, `tool`, `toolUseId`, `reason: "steered"` |
| `step.route.changed` | `stepId`, `route`, `reasoningEffort`, `fromIteration` |
| `step.stopped` / `step.continued` | `stepId`, `loopId?` |
| `step.tool.end` *(write_file)* | adds `linesAdded`, `linesRemoved`, `path` |

### Notifications (direct, **not** through the `EventLog` — LW7)

| Method | Params |
| --- | --- |
| `terminal.output` | `terminalId`, `seq` (per-terminal, monotonic), `data` (base64) |
| `terminal.exit` | `terminalId`, `exitCode?`, `signal?` |

## 6. Work, in order

Each item lists where it lands and the tests that prove it. **Write the failing test first** (the repo's habit and
the reason its counts are trustworthy). Tests never touch the real store, a live provider, a real repository or a
real forge (AGENTS §5): use the existing fakes, recorded fixtures, and `t.TempDir()` git repos created by the test.

### T1 · Loop steps stream live (D1, D2)

- `RunStepForLoop` builds a real sink (not `DiscardSink`) that publishes through `OnEvent` with the step's
  `projectRoot` and the full identity (`stepId`, `loopId`, `lane`, `step`, `agent`). The step id is only known after
  `CreateRunningStep`; capture it before the first event is emitted (the `onCreated` callback is the seam).
- Fix `stepSink` to set `StepID` for the ad-hoc path too.
- `Daemon.publish` / `Table.OnEvent` gain a project-root argument (or a payload interface that exposes one) so the
  root is not `""`. Keep the old call sites compiling by passing `""` only where there genuinely is no project.
- Emit `loop.step.dispatched` from the loop's `Model` callback in `daemon/blueprint.go` once the step id exists.
- **Tests:** a daemon-level test runs a two-step blueprint against the fake model and asserts a subscriber with
  `Filter{ProjectRoot: root, Kinds: ["step.*","loop.*"]}` receives, in order: `loop.started`,
  `loop.step.dispatched`(stepId=N), `step.started`(stepId=N, loopId, agent), tool start/end, `step.done`,
  `loop.advanced`… A second subscriber with a different `ProjectRoot` receives none of them. Cursor replay
  (`After`) returns the same sequence. Conformance fixtures for the enriched payload.
- **Measure and record** (task notes): events per second and `seq` allocations for a step that streams a long
  `model.delta` reply — that number decides whether deltas need coalescing (do not coalesce speculatively).

### T2 · Stop and continue on loop steps (D6)

- Thread `TrackStep` into the loop path so `step.stop` reaches a loop step's context. The loop `Runner`'s model
  result gains a `stopped` kind; `AdvanceLoop` parks the loop as `waiting` with reason `stopped`
  (`loop.waiting {reason:"stopped", stepId}`), **not** `failed`/`blocked`.
- `step.continue` on a loop step resumes it and, on completion, hands the result back to `AdvanceLoop` exactly as a
  first run would. Remove the *"What this does NOT do"* paragraph from `step_stop.go` and replace it with what is
  now true.
- `step.continue` gains `message`: appended as a user text block before the resumed call, checkpointed before it.
- **Tests:** stop a loop step mid-turn (fake model that blocks until cancelled) → loop `waiting/stopped`, step
  `pause_reason` set, no `step.done(failed)`; continue with a message → transcript contains it once, loop advances
  to the next lane. Stop between tools (the `ctx.Err()` check inside the tool loop). Stop of an unknown or finished
  step → existing named result.

### T3 · The steer store (D4)

- New table `step_steer(id, step_id FK, text, route, reasoning_effort, state CHECK IN ('pending','delivered','cancelled'), created_at, delivered_at, delivered_iteration)` — **a new numbered migration, never an edit to a shipped one**; read the current count in `internal/store`'s tests and add the next. Index on `(step_id, state)`.
- Store methods: `QueueSteer`, `PendingSteers(stepID)`, `CancelSteer`, and — the important one —
  **`CheckpointWithSteers(stepID, messages, deliveredIDs, iteration)`**: writes the transcript checkpoint and marks
  the steers delivered in **one transaction**.
- **Tests:** CHECK rejects an unknown state; cancel of a delivered steer is refused (discriminated result, not an
  `error`); `CheckpointWithSteers` is atomic — inject a failure after the transcript write and assert neither the
  transcript nor the steer state changed. Migration count test updated.

### T4 · Steer injection in the runtime (D3, D5) — the core of the slice

In `internal/runtime/loop.go` `RunLoop`, with a new optional `LoopDeps.Steers` dependency
(`Pending(ctx) []Steer`, nil = feature off, which keeps every existing test valid):

1. **Before every model call** (top of the loop, after the bounds checks): drain pending steers. Apply any
   `route`/`reasoningEffort` to `st.Request` (re-running the compile rule, LW6), and append text: if the last
   message is a `user` message (tool results), append text blocks to it; otherwise append a new `user` message.
   Persist with `CheckpointWithSteers`; emit `step.steer.delivered` and, when changed, `step.route.changed`.
2. **After the assistant turn is saved and before any tool runs** (just after the `len(turn.toolUses) == 0`
   check): drain again. If anything is pending, **do not run the turn's tools**: build one `user` message with a
   synthetic `tool_result` for **every** `tool_use` in the turn (LW2, LW3 — ordinary and `question` alike), then the
   steer text blocks; persist with `CheckpointWithSteers`; emit `step.tool.skipped` per tool and
   `step.steer.delivered`; `continue` the loop.
3. Iteration bounds and deadlines apply unchanged — a steer does not buy extra iterations.

- `daemon` wires `Steers` to the store for both the ad-hoc and loop paths; `step.steer` validates (LW4, LW10) and
  emits `step.steer.queued`.
- **Recovery (S7):** a step resumed after a crash drains `pending` steers on its first iteration like any other;
  `delivered` ones are already in the checkpointed transcript.
- **Tests (table-driven, fake model + fake tools):**
  - steer during a text-only turn → appears at the start of the next request, exactly once;
  - steer while the model's turn ends in two `tool_use`s → neither tool runs (fake tool records zero calls), the
    next request carries two `tool_result`s with `isError` then the text, in that order;
  - turn with a `question` tool + pending steer → no suspension, question answered as not-run;
  - route-only steer → next `ModelRequest.Route` changed, transcript unchanged, `step.route.changed` emitted;
  - effort change on a route that refuses tools-with-reasoning → the compile rule's lowering applies;
  - two steers queued in one turn → both delivered in order, in one message;
  - cancelled steer → never delivered;
  - **crash points:** kill after drain but before checkpoint → on resume, steer is still `pending` and is delivered
    once; kill after checkpoint → steer `delivered`, present once, not re-delivered;
  - provider shape: the resulting canonical messages encode and are accepted by **both** adapters' recorded
    request fixtures (Anthropic: `tool_result` blocks first then `text` in one user turn; OpenAI-shaped: `tool`
    messages then a `user` message) — add recorded fixtures, never a live call.

### T5 · Agent identity and the `write_file` line counts (D1, D8)

- `agent` on every step event: the step's `Agent` (handler name), `"default"` when empty — one helper, not repeated
  at each call site.
- `write_file` computes `linesAdded`/`linesRemoved` against the previous content (a line diff count, not a full
  diff) and puts them on its result metadata; the sink copies them onto `step.tool.end`. **Does not change the
  tool's model-facing content** (S13 N9 / the nine tools' behaviour).
- **Tests:** new file → `+N −0`; overwrite → correct counts; payload fixture.

### T6 · Git status and diff (D7, D8)

- New `internal/workspace` (plain package) with `GitStatus(ctx, root)` and `GitDiff(ctx, root, path)`, running git
  through the bounded process layer (timeout ~5 s, output caps reported). Parse porcelain v2 with `-z` (paths with
  spaces/newlines). `git` missing → `unavailable: "git is not installed"`; not a repo → `not-a-repo`; timeout →
  `unavailable` naming the timeout.
- Untracked file diff → synthesised all-added diff; binary → `binary: true`, no body.
- **Tests:** temp-dir repos built in the test: clean repo; modified, added, deleted, renamed, untracked files;
  detached HEAD; branch with upstream ahead/behind (two local repos, one as remote); not-a-repo; path with spaces;
  path escaping the root refused; huge diff truncated with `truncated: true`. Fake `PATH` without git →
  `unavailable`.

### T7 · Terminal sessions in the daemon (D9)

- New `internal/terminal`: a registry owned by the daemon (like `stepCancels`), PTY via **`github.com/creack/pty`**
  (MIT; add to `go.mod`, record in `UPSTREAM.md` if that is where third-party pins are justified). Shell: `$SHELL`,
  fallback `/bin/sh`; `TERM=xterm-256color`; cwd = the project root; own process group (`Setsid`), killed as a group.
- One reader goroutine per terminal → ring buffer (LW10) + coalescer → `localrpc.Notify(connID, "terminal.output", …)`
  for each attached connection. Exit → `terminal.exit`, session kept (exited) until closed or reaped.
- Connection close detaches (localrpc's connection lifecycle); reaper per LW9; `Daemon` shutdown closes all.
- Windows: `terminal.open` → named `unsupported` refusal (LW12), build-tagged so the package compiles everywhere.
- **Tests:** open `/bin/sh` in a temp root, `input "echo hi\n"`, assert an output notification containing `hi`
  (recording notifier fake); resize changes `stty size` output; attach after output returns it in scrollback;
  scrollback is capped and keeps the **tail**; `exit 3` → `terminal.exit {exitCode:3}`; close kills a backgrounded
  child (measure as `tools/spawn.go` did — a freed port or a dead pid, not a return code); unregistered root
  refused; 9th terminal on one root refused; reaper removes a detached session after the (test-shortened) timeout;
  race detector clean (`make test-race`).

### T8 · Rust shell forwards terminal notifications (D10)

- `session.rs` (`:556`): today only `method == "event"` is emitted. Add `terminal.output` and `terminal.exit` →
  Tauri event **`aeon://terminal`**, params verbatim. Keep it an explicit allow-list (an unknown notification method
  is still logged and dropped, never forwarded blindly).
- No new `#[tauri::command]`. `ui/src/lib/bridge.ts` gets a typed `onTerminal(listener)` helper beside the existing
  event listener — **no UI components** in this slice.
- **Tests:** the existing recording-`EventSink` pattern in `session.rs`: a frame with `terminal.output` reaches the
  sink as a terminal event; `event` frames unchanged; an unknown method is dropped and logged. A vitest for the
  `bridge.ts` helper with a mocked Tauri `listen`.

### T9 · Protocol hygiene, guards and the demo (D11)

- Conformance fixtures for every method, event and notification above; `make protogen`; `make check-protogen` clean.
- Guard test: none of `terminal.*`, `step.steer*` is reachable from any narrow-surface action table (LW8) — written
  now even though S19 is unbuilt, the way the policy-authoring guard was.
- `docs/aeon/DEMO.md`: **append** the S31 section (AGENTS §4b) — walk the new methods from the running app. A
  minimal, clearly labelled raw bench page (JSON in, JSON out, the terminal as a `<pre>`) is acceptable for the walk
  and must say *"the designed loop workspace is the next piece of work"*. Do not start the designed UI here.
- Record the work per AGENTS §4a: STATUS (this slice's row, counts **re-measured**), task notes with what was
  measured and what deviated.

## 7. Non-goals

- **N1 — No designed UI, in T1–T9.** The three-pane loop workspace was the next piece of work, built on
  this — correct when written, and **it did get built, the next day (T10)**, just not through this slice's
  own nine tasks or this project's usual task-spec-before-code order (T10 documents it after the fact). N1's
  own reasoning (backend and shell first, so the UI work that follows is UI work only) held throughout.
- **N2 — No parallel execution.** S32. This slice only guarantees nothing here blocks it (D12).
- **N3 — No git actions** (commit, branch, push, PR). S16.
- **N4 — No Windows terminal** (LW12).
- **N5 — No change to what the nine tools do or return to the model.** Line counts are metadata only (T5).
- **N6 — No policy changes, no new policy type.** LW8.
- **N7 — No new Tauri command.** The generic bridge carries everything (AGENTS §4b).
- **N8 — No retiring `workspace_files.rs`.** It duplicates `loop.files` and predates the generic-bridge rule;
  record it as a finding for the UI work to resolve, do not remove it here.

## 8. Invariants (check before you call it done)

- **I1** — `make ci` exit 0; `make test-race` clean for `internal/runtime`, `internal/terminal`, `internal/daemon`.
- **I2** — With `LoopDeps.Steers == nil` every pre-existing runtime test passes unchanged.
- **I3** — No terminal byte is ever appended to the `EventLog` (assert in a test: open, produce output, compare
  `EventLog.Head()` before/after).
- **I4** — Every new refusal names the thing refused and the reason (AGENTS §5), and is a discriminated result.
- **I5** — No new method appears in a narrow-surface table (LW8 guard).
- **I6** — `PROTOCOL_VERSION` unchanged.

## 9. Read before starting

1. `aeon/AGENTS.md` in full — the rules are short and not negotiable.
2. The design doc linked at the top, for what the UI will do with each method.
3. `internal/runtime/loop.go` (`RunLoop`, `stream`, `runTool`, `finish`) — T4 lives here.
4. `internal/protocol/step.go` (`handleStep`, `RunStepForLoop`, `stepSink`) and `step_stop.go` — T1, T2.
5. `internal/daemon/blueprint.go`, `stepcancel.go`, `daemon.go` (`publish`, `OnEvent`) — T1, T2.
6. `internal/loop/advance.go`, `runner.go` — T2's `stopped` kind.
7. `internal/protocol/events.go` — why the log is a bounded ring with durable `seq` (LW7's reason).
8. `internal/tools/spawn.go` — the process-group discipline T6/T7 must match.
9. `ui/src-tauri/src/session.rs` — T8.
10. `docs/aeon/S7-recovery/` — the crash cases T3/T4's tests must respect.

## 10. Open questions (settle in the first task's notes, then proceed)

| # | Question | Default if nobody objects |
| --- | --- | --- |
| **Q1** | Do `model.delta` events need coalescing for the event log? | No — measure in T1 first |
| **Q2** | Should `workspace.git.status` push an event after a step's write tools end? | No — the UI refreshes on `step.tool.end` and on demand |
| **Q3** | Terminal input as UTF-8 string vs base64? | String for input, base64 for output (output may be non-UTF-8) |
| **Q4** | Does a steer reset the step's deadline? | No — bounds are the blueprint's, a steer buys nothing |

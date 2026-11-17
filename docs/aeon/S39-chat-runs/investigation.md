# Investigation — S39 Chat runs (T0)

**Ran 2026-10-27, from `aeon/`, on the S38-built tree (`10b6e133` + the S19 re-walk `c05d6480`).**
Every verdict quotes the command that produced it; trimmed output is verbatim. This closes **CR1, CR2, CR3**
(the design's provisional rules) and settles the facts T1–T13 must not re-derive. Where a command in
`tasks/T0.md` quoted line numbers "measured 2026-10-27, re-measure", they had drifted (S38 moved the
files) — the re-measured numbers below are the ones T1–T13 use.

---

## Q1 — Every reader of loop status (R1)

**Commands (run verbatim):**

```
grep -rnE '"(done|aborted|blocked|waiting|running|active)"' --include='*.go' internal cmd | grep -v _test.go     # 145 hits
grep -rnE "'(finished|done|aborted|blocked|waiting|running)'|\"finished\"" ui/src --include='*.ts' --include='*.tsx' | grep -v proto/contract   # 18 hits
grep -rn "ListLoops\|PinnedLoopsByStatus\|SetLoopStatus\|SavePinnedRuntime" --include='*.go' internal cmd | grep -v _test.go
grep -n "status" internal/store/archive.go internal/store/events.go | head -20
sed -n '/CREATE TABLE loop /,/^);/p' internal/store/schema.go | grep -n "status\|CHECK"
grep -n "loop.finished" internal/loop/*.go internal/daemon/*.go | grep -v _test
```

**Measured vocabulary.** A loop row's `status` today takes `active` (the create default —
`store/loops.go:143` `Status: orDefault(in.Status, "active")`, and `store/blueprint.go:147` likewise),
then the engine's `running | waiting | blocked | done | aborted`. **`failed` is never a loop status in
Go** — it is a *phase* close-status (`closePhase(…, "failed")` at `continue.go:87,122,141`,
`advance.go:150,248,315,352,393,401`); the UI's `ENDED` list carries it defensively. `idle` therefore
joins a set that is already free-form, and the DDL settles the whole question:

```
status             TEXT NOT NULL,        ← schema.go, CREATE TABLE loop — NO CHECK.
```

**`idle` needs no DDL. CR1's no-DDL claim is measured, not assumed.**

**The reader table.** (file:line as re-measured today | comparison | what `idle` does there | edit)

| file:line | comparison | what `idle` does there | edit |
| --- | --- | --- | --- |
| `internal/loop/decide.go:40` | `st.Status == "done" \|\| "aborted"` → `ActDone` | **the terminal gate.** `idle` is NOT terminal — decides must proceed | **T4** |
| `internal/loop/advance.go:213,409` | `if st.Status == "done" \|\| "aborted"` | same — treat with `idle` as non-terminal where a turn must be allowed | **T4** |
| `internal/loop/advance.go:114-123, 455-458` | sets `aborted`/`done` + `emit "loop.finished"` (also :214, :410; `continue.go:153-154`) | the walk's end; `after: converse` diverts to `idle` + `loop.run.finished`, skipping `loop.finished` | **T4** |
| `internal/loop/continue.go:187` | `pin.Loop.Status != "running" && != "waiting"` → refuse | an `idle` loop must keep refusing `continue` (nothing in flight) — correct as written once `idle` is its own value | none (verify T4) |
| `internal/loop/runner.go:23` | result status waiting/blocked/done/aborted stops the spin | an `idle` result must stop the runner (nothing to advance) — same arm | none (verify T4) |
| `internal/loop/escalate.go:287` | sets `done` | gate-escalation end: terminal path — `idle` never set here | none |
| `internal/daemon/blueprint.go:243,890` | `PinnedLoopsByStatus("waiting")`, `("running")` | an `idle` loop is neither — it must not be swept into either resume path | none (verify T4) |
| `internal/daemon/blueprint.go:275` | `fresh.Loop.Status == "running"` before kicking the runner | `idle` must NOT kick the runner | none (verify T4) |
| `internal/daemon/blueprint.go:623-659,673-721` | view projection: status/waiting/blocked/running arms | the view must show `idle` honestly (not `running`, not finished) | **T4** |
| `internal/protocol/goals.go:101-121` | `running`/`waiting,blocked`/`done`/`failed,aborted` → board bucket | needs an `idle` case (the board's answer for a resting loop) | **T4/T10** |
| `internal/trigger/overlap.go:43` | `case "running", "waiting", "blocked"` = busy | **decision point:** `idle` must be OVERLAPPABLE (a resting loop takes a new run) — verified by a T4 test | **T4** |
| `internal/agentsvc/task.go:436` | `pin.Status == "waiting" && LastStepID == caller` | turn steps are ordinary running steps; untouched | none |
| `internal/store/blueprint.go:256` (`PinnedLoopsByStatus`) | status IN (…) query | callers choose; no change | none |
| `internal/daemon/memory.go:148`, `protocol/memory.go` `LoopView` | pass-through string | renders whatever status; UI decides | none |
| `internal/store/archive.go:34,129,178` | pass-through | fine — archiving `idle` is T4's `loop.close` (idle→done→archive) | none |
| `ui/src/features/loop-workspace/LoopWorkspaceView.tsx:22,82,208` | `ENDED=['done','blocked','failed','aborted']`; `ended = finished\|blocked\|…`; `readOnly={ended}` | `idle` is NOT ended → composer stays. The S31 reversal lands **here**, and here only | **T11** |
| `ui/src/components/agent-chat/session.ts:35,66-69` | `loopStatus: unknown\|running\|waiting\|finished\|blocked` derived from events | `loop.run.finished` must set a RESTING status (`idle`), `loop.finished` still `finished` | **T11** |
| `ui/src/features/components/ComponentsDashboard.tsx:23-24,100,116,158-159` | `isActive=['running','waiting','suspended']`, `needsAttention=[…]` | `idle` is neither active nor attention-needed — renders as a plain pill; **the doc's "ProjectsDashboard.tsx:23-24" is really this file** (no ProjectsDashboard.tsx exists) | **T11** (cosmetic) |
| `ui/src/features/loop-workspace/…/ComponentsScreen.tsx:415`, `test-bench/pages/AgentChatPage.tsx:198,446` | pill tone; `ended = finished\|blocked` | `idle` not ended → composer stays in the bench too | **T11** |
| `ui/src/features/projects/ProjectsScreen.tsx` | **no status comparisons at all** (grep empty) — the doc's `:407` hit does not exist | nothing to edit | none |

**Q1 verdict: `settled` — CR1's default holds; no reader resists a one-line edit; the fallback
(`done` + `conversing` flag) is NOT needed.** The two real edit sites are the engine's terminal gate
(T4) and `ENDED`/`loopStatus` in the UI (T11). Deliberate non-edits (verified as tests in T4):
`PinnedLoopsByStatus("running")` must never return idle loops; overlap treats idle as free.

## Q2 — Where position lives; cost of a per-run pin (R2)

**Commands:** the two greps from T0. **Measured:** `loop_blueprint` is touched by **seven** statements
(doc said six at 62,120,157,183,220,252 — all drifted):

```
internal/store/blueprint.go:62,120  UPDATE loop_blueprint SET runtime_json … WHERE loop_id=?
internal/store/blueprint.go:161     INSERT INTO loop_blueprint(loop_id,…)          (CreatePinnedLoop)
internal/store/blueprint.go:187     SELECT … FROM loop_blueprint WHERE loop_id=?   (ReadPinnedLoop)
internal/store/blueprint.go:224     UPDATE … runtime_json (SavePinnedRuntime)
internal/store/blueprint.go:256     SELECT loop_id FROM loop_blueprint JOIN loop … WHERE loop.status IN (
internal/store/blueprint.go:303     SELECT lb.blueprint_id FROM loop_blueprint lb …
```

`PRIMARY KEY (loop_id)` at `schema.go:312` — one row per loop, hard. Recovery walks **steps**, not
loops: `internal/daemon/recover.go:66 ids, err := d.store.RunningStepIDs()` — the loop pin is never
consulted during recovery. `loop.Pinned`/`ReadPinnedLoop` readers: daemon/blueprint.go (275,387,402,432,561),
loop_files.go:50, question.go:132, records.go:878 — **all read the CURRENT pin, which is exactly what a
run's live position must be.**

**Q2 verdict: `settled` — the default holds.** No re-key: `loop_blueprint` stays one row per loop
meaning *the current run's pin and live position*; `loop_run` (T1) holds each run's immutable `content`
copy + end-snapshot `runtime_json`. Starting run N+1 = insert `loop_run` + overwrite the pin row, one
transaction (`BeginRun`, T2). The seven statements above are the complete write set the transaction must
respect. Recovery is untouched because it reads steps, not pins.

## Q3 — Chat today; which agent answers; blueprint-less loops (R4)

**Commands:** the four greps from T0. **Measured:**

- `func (s *Store) Thread` (`store/chat.go:427`) and `func (s *Store) Note` (`chat.go:403`) exist;
  grep for callers across `internal cmd` (excluding tests) returns **only the definitions** — no
  production caller. The loop-chat screen is built from events (`session.ts` derives from `ChatEventLike`).
- `ls internal/handler/bodies | grep -i "chat\|conver\|brainstorm"` → **`brainstormer.md`** only. No
  conversation body exists.
- `grep -rn '"chat"' internal/handler internal/blueprint` → **empty. The `chat` agent name is FREE**,
  and `ls internal/blueprint/blueprints/` (10 files: brainstorm, conversational-map, dev-loop,
  development-map-direct, development-map-pipeline, goal-to-brief, proposal-brief, research-map,
  tag-only, two-step) has **no `chat.yaml` — the blueprint name is FREE too.** No STOP required.
- `internal/handler/bodies/brainstorm.yaml` (the blueprint) already carries the comment: *"S39 adds the
  converse lane (after: converse); not here."* — the author's intent, recorded at S38.
- `CreatePinnedLoop` requires blueprint content and `ReadPinnedLoop` answers `ErrNotFound` without a
  pin row → **a blueprint-less loop cannot be represented** (confirmed as documented).

**Q3 verdict: `settled` as DECIDED BY THE AUTHOR (2026-10-27):** a dedicated **`chat` agent** (new
handler body, T6) and a built-in **`chat` blueprint** (one conversation lane, `after: converse`, T6).
Specialists (`brainstormer`, `brief-author`, …) are reached only through `run_start`; a blueprint's
`converse:` overrides the default. No blueprint-less loops in S39 — a plain chat starts through the
`chat` blueprint, so a pin always exists (CR2's readers all hold). The thread the screen draws today
comes from **events**; `chat_message` is the durable store and the new event **`loop.message.posted`**
(T5) is what the screen renders. **Design header amended** (it still said "the default conversation
agent is S38's `brainstormer`" — superseded by the author's ruling; CR3 already carried the correct text).

## Q4 — `chat_message` and `step_steer` shape (CR6)

**Commands:** the `sed` prints of both `CREATE TABLE`s + the `source` greps + the live ALTER drill.

**Measured — one trap found.** The `sed` of `CREATE TABLE chat_message` shows `project_root`, but the
**live table has `component_root`** — migration 0035 (OL1) renamed it. The first drill insert failed:
`Error: … table chat_message has no column named project_root`. Shipped DDL is immutable, so the
*document* shape and the *live* shape differ; T2/T5 code must speak `component_root`. Live
`chat_message`: `id, loop_slug, component_root, seq, author(human|agent|system),
kind(note|steer|question|answer|summary), body, question_id, created_at`, `UNIQUE (component_root,
loop_slug, seq)`. `step_steer`: `step_id, text, route, reasoning_effort, state(pending|delivered|cancelled)`,
delivery and content CHECKs — **no author, no source**. The only existing `source` CHECK vocabularies
are the policy tables' (`operator|agent|mcp_server|import`) — **unrelated**.

**Did S38 T12 add `step_steer.source`? NO.** S38 T12 is the emits gate; the `source` "measurements" in
its task file are about `source_step` counters. There is no vocabulary to reuse or STOP on — **T5
defines it**: `user|template|automation|blueprint`.

**The live ALTER drill (run on a populated store: a real daemon on a throwaway `XDG_STATE_HOME`, one
loop created by `aeon loop create`, one message row inserted) — both statements applied:**

```
ALTER TABLE chat_message ADD COLUMN source TEXT NOT NULL DEFAULT 'user'
  CHECK (source IN ('user','template','automation','blueprint'));
→ SELECT author,kind,body,source FROM chat_message;
  human|note|drill body|user

ALTER TABLE step_steer ADD COLUMN source TEXT NOT NULL DEFAULT 'user'
  CHECK (source IN ('user','template','automation','blueprint'));
→ step_steer ALTER applied
```

**Q4 verdict: `settled`.** The ALTERs are valid SQLite ADD COLUMNs on populated tables (constant
non-null default); T5's migration is two such statements (+ the `Migrations()` count bumps), NOT a table
rebuild. T5 must use `component_root` where the doc's `sed` output said `project_root`.

## Q5 — S11 `ask` for a tool call; seeding the `run_start` row (CR4)

**Commands:** the gate/branch reads + `go run ./cmd/aeon policy add --help`.

**(a) The exact command that writes the row** (syntax measured from the verb's own help; `--source`
defaults to `operator`, which `allow` needs and `ask` accepts):

```
aeon policy add --type tool_access --id run-start-ask-chat --target agent:chat \
  --action ask --tool run_start
```

`--tool` is the `tool_access` spec's `toolName` sugar (spec `{"toolName":"run_start"}`); targets are
`agent:NAME | loop:SLUG | lane-type:NAME | mcp:NAME | all-agents`.

**(b) Attended → question; unattended → immediate deny.** Read verbatim from `internal/runtime/loop.go`
(the region the task file numbered 610–700; re-measured **664–700**): `if dec.Asked { … if
st.Request.HasOperator { st.pendingPermission = &PendingQuestion{ToolUseID: use.ID, Kind: "permission",
… Options: []string{"approve","deny"}}; emit ToolEnd "suspended: awaiting a permission answer"; return } `
— and below it: *"Nobody can be vouched for answering … An ask answered by nobody is a deny"*, the
fail-closed immediate deny with **no question row**. `HasOperator` is
`daemon/step.go:284 OperatorPresentFor` — attendedness read from the loop's own declaration (S11 G14),
never from who happens to be chatting.

**(c) No policy code changes.** A new tool needs only a `ToolAccess` row; with no row the engine's
empty-set deny stands (PL5). `gateQuestion` (`daemon/policy_gate.go:218-231`) canonicalizes the tool
name — `run_start` is not `bash`, so `tool_access` it stays. **Tests to copy:**
`internal/runtime/policy_gate_test.go` (exists; the attended-suspension and unattended-deny halves) and
`internal/daemon/permission_ask_test.go` for the store-backed question-row shape.

**Q5 verdict: `settled`.** CR4 needs zero policy-engine change: one catalog row (T10), one operator
command, two copied test shapes.

## Q6 — The composer and the finished-loop rule (D8)

**Commands:** the four greps from T0. Measured edit sites for the reversal (T11):

```
ui/src/features/loop-workspace/LoopWorkspaceView.tsx:22  const ENDED = ['done','blocked','failed','aborted'];
ui/src/features/loop-workspace/LoopWorkspaceView.tsx:82  const ended = session.loopStatus==='finished' || …blocked… || (unknown && ENDED.includes(loop.status));
ui/src/features/loop-workspace/LoopWorkspaceView.tsx:208 readOnly={ended}
ui/src/components/agent-chat/AgentChat.tsx:46            {composer && !readOnly && <ChatComposer …/>}   ← keep; drive from props
```

**Tests that pin "finished hides the composer" (must keep passing for `done`):**
`ui/test/loop-workspace.test.tsx:617` *"a finished loop has no message box"* (emits `loop.finished`
`status:"done"`, expects `queryByTestId("agent-chat-composer")` null);
`ui/test/agent-chat-component.test.tsx:116` *"read-only hides the composer…"*;
`ui/test/agent-chat-adapters.test.ts:204,212,218` (the `Loop finished · done` divider) and `:275-279`
(parked-at-a-gate is waiting, **not** finished). **All four files T10/T11 extend exist**:
`loop-workspace.test.tsx`, `agent-chat-component.test.tsx`, `agent-chat-adapters.test.ts`,
`loop-workspace-model.test.ts`.

**Q6 verdict: `settled`.** The reversal is one predicate (`ended` must stay false for a resting loop
with a conversation agent) exercised through the adapter's new `loop.run.finished` → resting status;
the four tests above are the D8 guard for `done` loops.

## Q7 — A turn is a step with no walk position (R3)

**(1) YES — recovery completes a turn by step rows alone.** `daemon/recover.go:66`:
`ids, err := d.store.RunningStepIDs()` — Recover scans running **steps**; no loop-walk position is read.
T6 test: a turn step left `running`, daemon restart, `Recover` classifies and commits it (copy the
`daemon` recovery tests).
**(2) YES — `OnStepContinued` ignores a stranger step.** `daemon/blueprint.go:242-252`: it lists
`PinnedLoopsByStatus("waiting")` and per pin `if … saved.LastStepID != stepID { continue }` — a step id
that is no loop's `LastStepID` is skipped outright. (Note: while a turn SUSPENDS on a permission ask it
IS its loop's `LastStepID` and resumes through this same path — correct for free.) T6 test: a turn step
whose loop is not `waiting` + a foreign step id → zero wake effects.
**(3) YES — `RunStepForLoop` accepts `LoopStepIdentity{LoopID: id, Lane: "", Step: "turn"}`.** The
identity struct's fields are `omitempty` strings (`protocol/step.go:298-302`); the ONLY consumer
(`step.go:460`) selects `t.loopStepSink(stepID, root, id, handler)` which stamps `loopId/lane/step/agent`
onto events — no validation against the pinned blueprint, empty `Lane` and `Step:"turn"` flow. T6 test:
run a turn through `RunStepForLoop` and assert the stream carries `step:"turn"` and the events land.

**Q7 verdict: `settled` — yes/yes/yes, no design change.** CR3 holds: the turn is an ordinary step.

## Q8 — Tool identity for `run_start`

**Commands:** the two greps + `grep -rn "records.Catalog()" internal --include='*.go' | grep -v _test`.
Names **verified** (all expected names exist; one kind-correction):

```
internal/stepctx/stepctx.go — package stepctx; stepctx.StepID(ctx) (used at daemon/records.go:83)
internal/daemon/records.go:66  type RecordsService struct
internal/daemon/records.go:82  func (r *RecordsService) Identify(ctx) (Identity, *Failure)
internal/daemon/records.go:106 func (r *RecordsService) Do(ctx, by Actor, name, input) (any, *Failure, error)
internal/tools/read.go:78      type RecordsInvoker func(ctx, tool, input) (content string, isError bool)
                               ← a FUNC TYPE, not an interface (T0's wording said interface; kind fix, no consequence)
internal/records/catalog.go    — the catalog (S38 T9)
```

**Are socket methods also projected from the catalog? YES** — `records.Catalog()` has three projection
consumers: `tools/records_tools.go:23` (toolset), `daemon/records.go:520` (the daemon service), and
**`protocol/records.go:55`** (the socket projection, S38 T15). **T9's answer: register the run doors'
method specs beside the tool spec and both doors exist from one table** — and T10's `run_start` row
lands in the same catalog (S38's one-table, two-doors rule).

**Q8 verdict: `settled`.** S38 T8/T9 are merged (they are — T0 ran on the S38-built tree); T10 copies
the pattern verbatim.

## Q9 — The ground has moved (measured, not assumed)

```
grep -n '^\s*{[0-9]*, "' internal/store/schema.go | tail -2 →
  {35, "0035_org_layers", …}, {36, "0036_records", …}
```

**L = 36.** Therefore **T1's migration is `0037_runs` (37)** — *landed as `0037_loop_run`, the task file's canonical name* and **T5's is `0038_message_source` (38)**.
Count assertions to bump (measured — `want 36` sites): `internal/store/schema_test.go:909,922`;
`internal/store/policy_test.go:307-308,458,1318`; `internal/loop/counters_test.go:280,284` (the latter
also pins `last.ID != 36 || !strings.Contains(last.Name,"records")` — it must move to 37/`runs` at T1
and 38/`message_source` at T5). `internal/store/org_layers_test.go:26` slices `Migrations()[:34]` —
unaffected by appending. **S38 added TWO migrations after S18-S20's 34 — any document saying S38 lands
at 0027–0030 or S39 at 0035/0036 is stale.**

S20/S19 provenance columns confirmed present: `internal/store/loops.go:151-152` selects
`attended, allow_outward, … started_by, trigger_id`; `StartedBy` rides `CreatePinnedLoop`
(`blueprint.go:147`) — **CR2's `started_by_kind` for run 1 has its source.** `internal/trigger` exists,
plus the S38-era `internal/records` and `internal/stepctx` (full `ls internal` above).

**Q9 verdict: `settled`. L=36; T1=0037, T5=0038; the bump list is the eight sites above.**

---

## Closing

All nine verdicts are `settled`; **no default was overturned**, so the "If T0 decided otherwise"
paragraphs in T1–T13 stay unused. The findings that DID force text changes: (i) the design header's
"conversation agent is S38's `brainstormer`" → superseded by the author's `chat` ruling (Q3); (ii) the
live `chat_message` key column is `component_root` (Q4) — T2/T5 code speaks that name; (iii) S38 T12
did not pre-add `step_steer.source` — T5 owns both columns outright (Q4); (iv) the pinned-count list is
eight sites in three files, not "every assertion" vaguely (Q9). `design.md` CR1–CR3 are final, dated.

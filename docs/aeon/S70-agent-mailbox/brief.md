# Brief — S70 Agent↔agent mailbox

**Stage:** Fleet lineage (ruling C of the decomposition the operator approved at S68: A = right body (S68),
B = attachments (S69), C = agents talking (this slice)).
**Depends on:** S14/`internal/agentsvc` (Invoke, TaskMode, the suspension-not-wait Task — AS4/AS5/AS6),
S28 (the board — D8's addressed-ask shape), S6 (question-row suspension, `loop.CompleteSuspendedStep`),
S39 (steers — boundary-drain delivery, the window-row honesty template), S68 (`ModelRunner(words, frame)`,
the half-split, `steerWireText` one-join composition; this slice CLOSES S68's recorded deviation
"step.Skill (agent↔agent) carries no chosen lines (S70)"), S69 (`AppendUserTurn` — capability content as
its own block, B3), S11 (registered-not-granted tools), S10/S12 (frozen-set judgement, D12).

## 0. Why now

S68 made the person's words pure and put everything else in the system half; S69 let those words bring
files. The remaining deferred territory is the one both specs named and neither touched: **a running agent
has no way to say anything to another agent** except what a blueprint pre-declared (`step.Skill`) or what
the learnings board predicted (a learnings ask). Everything an agent might want to ask a peer must be
authored in advance. The industry moved the other way: OpenAI's handoffs are "represented as tools to the
LLM", Anthropic's orchestrator hands subagents explicit task texts into isolated contexts, and AutoGen
published the shared-thread group chat — with its own docs disclaiming it as a starting point, not a
product. aeon needs its own shape: addressed, row-persisted, suspension-not-wait, visible.

## 1. What this is

An agent mid-run calls a registered, never-granted `ask_agent` tool: `{to, text, thread?, wait?}`. `to`
must name an agent of the loop's own pinned blueprint lanes — a stranger is refused by name, and the
refusal lists what IS addressable. The ask is an **append-only row** in a new `agent_message` table; a
**thread** is a conversation (its id is the first ask's id); replies link via `result_of`; delivery reuses
**agentsvc's `TaskMode`**: a running callee drains the message at its next turn boundary (the steer's
shape, one drain join, a frame line above PURE words), while a waiting caller (`wait: true`, callee not
running) suspends as a row — question-row pair makes the wait visible (D16) — and resumes cold via
`loop.CompleteSuspendedStep` with the answer arriving as its own block beside the caller's next words.
The callee's turn gets the full S68 half-split: system half names caller/lane/goal (policy last),
transcript half holds ONLY the message's words; chosen-skill lines are unrepresentable in such a turn
(closing S68's deviation structurally — there is no operator, so there is nothing to resolve). The window
shows the exchange: `agent.message.posted/.delivered/.answered` events, a new `exchange` row kind
rendering `from → to · body` with its own tone and thread grouping, undeliverable threads rendering the
honest absence. Reads ride the socket (`agent.messages`); the CLI is a thin client (`aeon loop messages`).
Zero goroutines (AS5 pinned by stack dump); rows are the only state (AS4).

## 2. What exists today, measured — the reuse surface

| Piece | Where | What it is today |
| --- | --- | --- |
| Agent→agent ask (blueprint-declared) | `internal/daemon/blueprint.go` (`dispatchSkill`, `stepParamsFor`), `step.Skill` | Static wiring only; `agent.invoke` on the socket (Debug). Stays as-is; MB5 reframes ONLY mailbox-dispatched callee turns |
| The suspension machinery | `internal/agentsvc/task.go` | "A Task is a row, and an async Task is a SUSPENSION, not a concurrency model" — step row `suspended`, question row visible wait, cold resume via `loop.CompleteSuspendedStep`; `TaskMode` sync/async, immutable at invoke; AS5 stack-dump test |
| The addressed ask | `internal/daemon/learnings_board.go` (S28) | Named callee, never broadcast (D8), `relayed` provenance; a waiting task files nothing — completion files on resume |
| The model-initiated ask template | `internal/tools/question.go`, `toolset.go:116 Registry()` | Parser runs BEFORE anything persists; Run unreachable; registry pin `TestRegistryExactNames` (widens to eleven here) |
| Boundary-drain delivery | `internal/store/steer.go` + `steerDrainText` (S39) | Queued at the door, drained at the next turn boundary, one wire-text join |
| The half-split | `internal/runtime/loop` `ModelRunner(words, frame)`, S68 `TaskFrame` sections | The callee-turn template MB5 reuses |
| Capability blocks | `internal/model/message.go` `AppendUserTurn` (S69) | Own-block delivery, never woven (B3) |
| Frozen judgement | `internal/agentsvc/invoke.go` (`EvaluatedAt` required), S10/S12 | D12: expiry is a fact about a moment; a hidden clock revives dead rules |

## 3. Locked decisions (`MB`)

See `design.md` MB1–MB8 for the full reasoning. Summary: **MB1** registered never-granted `ask_agent`
tool, parser-before-persist, frozen-set judgement. **MB2** loop-scoped addressing only; D8 no broadcast;
card agents stay board territory. **MB3** append-only `agent_message` rows; thread = first ask's id;
`result_of` links replies; caps 32 KiB body / 16 per thread / 8 asks per step / 64 threads per loop-life,
refusal names both sides. **MB4** delivery reuses `TaskMode` — boundary drain for running callees,
suspend-as-a-row + cold resume with own-block answer for waiting callers; no second waiting path, ever.
**MB5** callee turn = full S68 half-split, no chosen lines by shape. **MB6** the window shows the
exchange (posted/delivered/answered, honest absence). **MB7** socket read `agent.messages`, CLI thin
client `aeon loop messages`; no authoring on the narrow surface. **MB8** D7 byte-identity, omitempty
everywhere.

## 4. Out of scope

Broadcast/group chat/speaker-picking managers (scheduler in disguise — AS5/P2; AutoGen's own disclaimer);
card-store (out-of-loop) callees from the mailbox (board territory, `relayed` stays singular); chosen
skills on agent↔agent turns (no operator — S68 B6, unrepresentable by shape); message edit/delete (rows
are facts); auto-granting the tool (S11); `step.Skill` semantics change (stays blueprint-static); any
daemon thread that waits.

## 5. Open questions — all resolved 2026-10-31

1. Entrance? Operator ruled: registered `ask_agent` tool (Recommended), industry-aligned.
2. Reachability? Operator ruled: own loop's lanes only; board keeps card agents.
3. Thread shape? Operator ruled: addressed rows, one thread, single named addressee, follow-ups ride it.
4. Delivery? Operator ruled: reuse `TaskMode` exactly as agentsvc spells it.
5. Window? Operator ruled: own `exchange` row kind, words visible, like steers.
6. Callee turn? Operator ruled: full S68 half-split — closing S68's `step.Skill` deviation structurally.
7. (spec self-review) Thread caps: per-loop-active would starve a long loop → lifetime bound 64 threads +
   8 asks per step. Recorded in MB3.

## 6. Acceptance criteria

AT1–AT15 of the spec (`docs/superpowers/specs/2026-10-31-agent-mailbox-design.md`), each naming its
mechanism: parser-before-persist with zero-row spy, registry eleven with the tool NOT granted, stranger
refusal naming the lane list, D12 expiry refusal, thread/link/third-state row tests, no update/delete verb
compiling, four cap refusals naming both sides, boundary drain with byte-identical words, suspend/restart
replay with the answer as an own block, wait:false queue-back, AS5 stack dump covering the mailbox, the
callee-frame half-split shape test, pinned event fixtures, socket read + no authoring method, the exchange
row + honest absence in the window, and the byte-identical D7 baseline capture.

## 7. Handoff status (2026-10-31, written by the planning session — not the builder)

Spec committed `ccddd34a` on `s70-agent-mailbox` (based on main `97a93188`). Rulings MB1–MB8 operator-ruled
2026-10-31 (all Recommended answers). **No implementation has been started.** The builder follows
`plan.md`'s T1–T5 and the S68/S69 ritual: gates measured with the preamble + reindex park/restore, baselines
against main at the time of the run, sweep reds proven pre-existing at root, records + SLICES row +
cumulative DEMO section, single merge, push. Unrun by design (named up front): live provider bytes
(fixtures/fakes pin request shapes), Tauri, Windows pipe, real-desktop exchange walk (bench scripts it).

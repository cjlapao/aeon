# Brief — S39 Chat runs

**Implements:** the conversation-first operating model of [`../S38-org-layers/brief.md`](../S38-org-layers/brief.md) §0.4 — *everything starts
from a chat; a blueprint runs inside it; when it ends the conversation does not.* Written 2026-10-27 as **S38's successor**
(S38 gives agents the management tools and blueprints their contracts; this slice gives the loop a conversation that outlives a run).
`design.md`, `plan.md` and `tasks/T0–T13.md` sit beside this brief. **The plan opens with an investigation (T0).**
**Stage:** 5 · Fleet. **Depends on:** **S38** (catalog, `emits:`, `requires:`, the review invariant), **S31** (steering, the loop chat screen),
S5/S6/S8 (step, suspend, blueprint walk), S11 (`ask`), S9 (the app). **Prefix:** `CR`. **Repo:** `aeon/` (P24) · Go 1.26 · `cgo` (B3)

---

## 1. What this is

**One sentence:** a loop is a conversation that can run blueprints, one after another, without ending — a message to a loop with no running step
starts a conversation turn, *run this blueprint* is a gated action the agent proposes and a person (or an `ask` row) confirms, a finished
blueprint leaves the loop **resting** (`idle`) instead of `done`, and the first message of a run is the same whether a person typed it or a
template wrote it.

| In this slice | Not in this slice |
| --- | --- |
| a non-terminal resting state; several blueprint **runs** in one loop | triggers that start loops from events (**S40**) |
| a conversation turn when a message arrives and no step is running | a project-scope loop with no component (later) |
| `run_start` tool and `loop.run` method, the `requires:` dialogue | `spawn`/`wait` fan-out (later) |
| kickoff templates and a `source` on every chat message | changing how steering works (S31 is untouched, §4) |
| the composer staying after a run, run cards, a *Run blueprint…* action | the kanban component |

## 2. What exists today, measured — so nobody re-derives it at seeding

- **`done` and `aborted` are terminal in the engine.** `internal/loop/decide.go:40` returns `ActDone` for either; `continue.go:130` emits
  `loop.finished` and stops; `advance.go:107/416` and `escalate.go:287` set `done`. There is no resting state: a loop is running, waiting,
  blocked or over.
- **A loop pins exactly one blueprint.** `loop_blueprint` (migration 0014) carries the pinned YAML; position and step states live in the loop's
  saved state. A second blueprint in the same loop has nowhere to live.
- **S31 hides the message box on a finished loop** (loop-chat design, variant 5; built as S31 T10). It is a *decided* behavior and this slice
  reverses it on purpose (§4).
- **Steering is keyed by step.** `step.steer` refuses a step that is not running and names the remedy (*use answer / step.continue*, LW4) — so
  today there is no door for "a message to a loop with nothing running".
- **Attendedness is declared at start and never inferred** (S11 G14; `loop.attended`, migration 0019). A chat the app opens declares it; a
  clock/event-started loop declares it unattended and its `ask`s deny at once.
- **`ask` already routes a tool call to a human** (S11): on an attended loop the step suspends with a question naming agent, tool and rule;
  `approve` delivers the answer to the model. This is the confirmation mechanism for starting a run — no new one is built.
- **`requires:` and the catalog arrive with S38** (OL9, OL3). A message that asks for a run whose prerequisite is unmet must produce a spoken
  answer, not a refusal blob.
- **Events already carry `stepId`, `loopId`, `agent`** (S31 D1/D2); a run adds `runSeq`.

## 3. Definition of Done

| | |
| --- | --- |
| **D1** | **A loop can rest.** A blueprint with `after: converse` ends its run with the loop `idle`, not `done`; `loop.run.finished` is published; `loop.finished` is **not**. A blueprint without the key behaves **exactly as today** (terminal `done`) — asserted over every shipped blueprint |
| **D2** | **A message to an idle loop is answered.** `loop.message` (or the chat path T0 selects) starts a conversation turn: a model step by the loop's conversation agent, streamed like any step, steerable while it runs |
| **D3** | **One composer, two meanings, no ambiguity.** A step is running and selected → the message is a **steer** (S31, unchanged). No step is running → it starts a turn. `step.steer` on a non-running step is still refused with its remedy |
| **D4** | **Several runs in one loop.** `loop_run` rows with `runSeq`; each pins its own blueprint+version; documents, chat and goal links stay on the loop; cost rolls up per run and per loop with P32 floors |
| **D5** | **Starting a run is confirmed.** `run_start` is a tool whose default S11 row is **`ask`**: attended → a question card, approve starts it; unattended → denied at once (S11's rule, zero new branches). The socket's `loop.run` is the same action behind the same checks. **Nothing infers a run from a sentence** |
| **D6** | **A missing prerequisite is a conversation.** `run_start`/`loop.run` for a `requires:` blueprint with nothing satisfying it returns the S38 sentence plus the candidates (approved goals/tasks in scope); the agent says it and asks |
| **D7** | **Typed and automated starts look the same.** A blueprint `kickoff:` template renders from the goal/task into the run's first message; every chat message carries `source` = `user | template | automation | blueprint` and the thread shows it |
| **D8** | **The composer stays after a run** when the loop has a conversation agent, and the thread shows a run card (blueprint, outcome, cost floor). A loop without one keeps S31's behavior |
| **D9** | **An idle loop holds nothing.** No process, no worktree lock, no memory beyond rows (P2, P28); recovery after a daemon crash does not try to recover it; a restart reads it back `idle` |
| **D10** | **Steering is untouched.** S31's steer/stop/continue conformance fixtures and tests pass byte-identical |
| **D11** | **Existing loops and clients are unharmed.** Old blueprints, `loop.start`, `loop.finished` consumers and the S25 history view behave as today for loops that never rest |

## 4. What this reverses, and what it must not touch

**Reverses (on the author's ruling, 2026-10-27):** S31's *"finished loop: no message box"*. Chat-after-run needs the composer. The reversal is
**limited to loops with a conversation agent**; a loop that ends `done` still hides it.

**Must not touch** (S38 OL15's table): steers keyed by `stepId`; the loop view's **To:** lists *running* agents, no broadcast; provider/effort
ride a steer; stop-then-`continue{message}`; LW4's refusal. The conversation turn is **a step**, so all of it applies to the turn too.

## 5. What this is not

- **No silent run.** A sentence like *"ok, create the goal"* never starts a blueprint by itself; the agent calls `run_start`, S11 asks.
- **No new messaging door that bypasses steering.** A message is a steer or a turn; there is no third path and no broadcast.
- **No second attendedness.** A loop's declaration at start stands; a human chatting into an event-started loop does not make it attended.
- **No approvals by chat.** Review decisions stay human doors (S38 carve-out); an agent cannot approve its own goal by conversation.

## 6. Risks, named where they live

- **R1 — `done` has readers.** The UI, S25 history, rollups and recovery all read the terminal states. A new `idle` status is a new state in every
  `switch`. T0-Q1 lists every reader; the plan does not proceed on a guess.
- **R2 — one pin per loop.** Runs need a pin each; T0-Q2 decides; the measured default is *a new `loop_run` table holding each run's pinned copy, `loop_blueprint` kept as the current run's pin* (six statements read it by `loop_id`), not a re-key.
  Nothing has shipped, so a rebuild is allowed — but recovery (S7) reads the pin, and that is where it will bite.
- **R3 — the turn that never ends.** A conversation agent that keeps calling tools is a step like any other (S5's iteration cap, budgets, S31 stop).
  T6 pins that a turn has the same bounds as a blueprint step (no iteration override).
- **R4 — which agent talks.** The conversation agent is blueprint-declared (`converse:`) else the project's default; T0-Q3 reads what S31's chat does
  today and what "default agent" means (decided by the author 2026-10-27: a **dedicated `chat` agent**, added in T6, not a specialist). A blueprint-less loop cannot exist (`loop_blueprint` is required by every reader), so a plain chat runs the built-in **`chat` blueprint** (one conversation lane, `after: converse`, added in T6); the specialists (`brainstormer`, `brief-author`, `project-planner`, …) are reached through `run_start`.
- **R5 — cost and context across runs.** One thread, many runs: compaction (P7/P23) and the P32 floor must hold across run boundaries.

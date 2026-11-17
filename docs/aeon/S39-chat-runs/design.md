# Design — S39 Chat runs

Fills in `brief.md`. Rules **CR1–CR11**, cited from `plan.md` and `tasks/`. **CR1, CR2 and CR3 are FINAL — closed by T0 on 2026-10-27** ([`investigation.md`](investigation.md), nine settled verdicts, no default overturned). Amended 2026-10-27 against the code (see the *Amended* lines): `idle` needs no DDL (measured: `status TEXT NOT NULL`, no CHECK); `loop_blueprint` is not re-keyed (measured: seven statements, all in `store/blueprint.go`; recovery reads steps, not pins); no blueprint-less loops; **the default conversation agent is a dedicated `chat` agent with a built-in `chat` blueprint (author's ruling 2026-10-27; both names measured FREE — T0-Q3; the header's earlier "S38's brainstormer" is superseded — specialists are reached through `run_start`, never the default).**

## CR1 — A resting state, opt-in (FINAL — T0-Q1 settled the reader table; the fallback is NOT needed)

Loop `status` gains **`idle`**, a non-terminal state: *no step running, no gate waiting, the conversation open.*
Blueprint key **`after: converse | finish`**, default `finish` (= today, terminal `done`). With `after: converse`, the walk's `ActDone` writes
`idle`, publishes `loop.run.finished {loopId, runSeq, outcome, cost}` and **skips** `loop.finished`. *Amended:* there is no blueprint-less loop (every reader of a loop needs its `loop_blueprint` row); a plain chat runs the built-in `chat` blueprint (T6), which rests (`after: converse`); `brainstorm` also rests, with its own specialist agent. `loop.status` has no CHECK, so `idle` is Go-only. `aborted` stays terminal; a run that **fails** leaves the loop `idle` with the run `failed` (the conversation is where a person decides what
next), unless the blueprint's `on_abort` says otherwise. `loop.close` (human, or an operator-granted `loop_close` tool) moves `idle`→`done`.
*What would change it:* if T0-Q1 finds `status` has more readers than a switch-update can safely cover, `idle` is represented as `done` + a
`conversing` flag on the loop — uglier, and listed as the fallback. **T0-Q1 (2026-10-27): every reader listed, none resists a one-line edit — the fallback is NOT needed. The engine's terminal gate (`decide.go:40`) and the UI's `ENDED`/`loopStatus` (T11) are the real edit sites; `overlap.go:43` must treat `idle` as OVERLAPPABLE (a resting loop takes a new run) — a named T4 test.**

## CR2 — Runs (FINAL — T0-Q2 settled: no re-key; seven `loop_blueprint` statements are the write set; recovery reads steps, not pins)

```sql
CREATE TABLE loop_run (
  loop_id TEXT NOT NULL, run_seq INTEGER NOT NULL,
  blueprint TEXT NOT NULL, blueprint_version INTEGER NOT NULL,
  content TEXT NOT NULL, runtime_json TEXT,                           -- the run's own pinned copy; snapshot at end
  state TEXT NOT NULL CHECK (state IN ('running','done','failed','cancelled')),   -- waiting/blocked derive from loop.status
  started_by_kind TEXT NOT NULL, started_by_id TEXT NOT NULL,        -- user | agent (via ask) | trigger (S40)
  kickoff_message_id INTEGER, goal_id TEXT, goal_version INTEGER, task_id TEXT,
  started_at TEXT NOT NULL, ended_at TEXT, PRIMARY KEY (loop_id, run_seq));
ALTER TABLE step ADD COLUMN run_seq INTEGER;   -- cost/time per run is a group-by (P32 floor), never a stored total
```

*Amended:* `loop_blueprint` stays one row per loop and means *the current run's pin and live position*; `loop_run` carries an immutable copy of each run's pinned YAML (`content`) and a `runtime_json` snapshot taken when the run ends. Starting run N+1 overwrites the `loop_blueprint` row in one transaction (`Store.BeginRun`). The run `state` is `running|done|failed|cancelled`; waiting/blocked is derived from `loop.status` (DB6: no stored flag that can disagree). `started_by_kind` for run 1 is derived from S20's `loop.started_by`. *T0-Q4:* the live `chat_message` key column is **`component_root`** (0035 renamed it; the shipped DDL still says `project_root` — the document and the live shape differ by design); T2/T5 speak `component_root`.
`Position`/`StepStates` are per run. `goal_id`/`goal_version` live on the **run** (S38 put them on the loop for the single-run case; the loop's
column is kept as *the first run's goal* so S38 reads still work, and `goal.show`'s derived state groups over runs).

## CR3 — The conversation turn (FINAL — T0-Q3 settled the `chat` agent/blueprint, both names free; T0-Q7 yes/yes/yes: a turn is an ordinary step)

A message arriving at an `idle` loop makes a **turn**: one model step by the loop's conversation agent (`converse:` on the blueprint of the latest run,
else the default **`chat` agent** — a new general-purpose handler body added in T6; specialists are never the default), context = the loop's thread (+ documents the agent reads), tools = that agent's grants (S38 catalog included).
It is a normal step: streams `step.*`, is steerable, stoppable, bounded by S5's iteration cap and S11's gates, and persists/suspends/recovers like any.
**The composer rule (D3):** the target is the selected running step → `step.steer` (S31, untouched); no running step → `loop.message` starts a turn.
There is no broadcast: with several steps running the person picks one (S31's **To:**) and with none the composer says *"message the loop"*.

## CR4 — Starting a run: a tool, a method, and S11

`run_start {blueprint, goal?, task?, with?}` — a first-party tool, **grantable per agent** (S38's catalog rules) — and `loop.run` — the socket method —
are one action (S38's one-table rule). **The default S11 row for `run_start` is `ask`** (seeded explicitly, in the way `seed-corpus` seeds rows;
never automatic): attended loop → the step suspends with a question *"Start blueprint X on goal Y?"*; `approve` returns the result to the model, which
continues; unattended → denied at once with no question row (S11's rule). A person can also click *Run blueprint…* in the app, which calls
`loop.run` directly (a human peer, no `ask`). **No code path infers a run from message text.** Starting appends the `loop_run` row, pins the blueprint,
renders the kickoff (CR6), sets the loop `running`.

## CR5 — Prerequisites as dialogue

`run_start`/`loop.run` evaluates the blueprint's `requires:` (S38 OL9) **before** creating the run. Unmet → no run row, and the **result** (tool result
or method error) carries the sentence *and* `candidates: [{kind, id, title, state}]` — approved goals/tasks in the loop's component/project — so the
agent can say *"dev-loop needs an approved goal or task; I see these: …"* and ask. Met → the entity is pinned (`goal_version`) on the run.

## CR6 — Kickoff templates and message source

Blueprint key **`kickoff:`** — Go `text/template` over `{Goal, Task, Component, Project, Params}` — renders the run's first message. It is written to the
thread as a **chat message with `source`**: `user` (typed), `template` (rendered by a person-started run), `automation` (S40), `blueprint` (engine
push-backs such as S38's `missingRecord` nudge). `chat_message` gains a nullable `source` (NULL for agent/system rows; `user` for typed human rows; T5), and `step_steer` gains `source NOT NULL DEFAULT 'user'`, so the engine's nudge reads as the blueprint's (T0-Q4 confirms the table and whether the
steer row needs the same). The thread shows the source (the loop screen draws from events, not `chat_message`, so T5 adds a `loop.message.posted` event the screen renders; agent-authored messages are not redrawn — the step stream already shows them); the model sees the same text either way — **the engine cannot tell, and does not need to**.
Missing `kickoff:` → the goal/task body, as the `goal` string is today.

## CR7 — Attendedness stays declared

A chat the app opens declares `attended = true` (the app sends it, as the CLI does today). Runs inherit the loop's declaration. A loop started
unattended (clock/event, S20/S40) stays unattended **even when a human chats into it**; the composer says so (three states, P31: *attended ·
unattended · never declared*) and `run_start`'s `ask` therefore denies, which is correct and is shown, not hidden.

## CR8 — The UI

The loop screen keeps S31's layout. Changes: the composer stays on an `idle` loop with a conversation agent and reads *"Message the loop"* (no *To:*
when nothing runs); a **run card** per run (blueprint, outcome, cost floor, link to the run's steps); a **Run blueprint…** action listing blueprints
that pin cleanly (there is no per-component blueprint filter in the code; *amended*) with `requires:` satisfiable, with the unmet ones disabled and saying why (S9's start-form precedent); the `ask`
card for `run_start` uses the existing question-card rendering. Finished (`done`) loops are unchanged: no composer.

## CR9 — Idle holds nothing

`idle` has no process (P2), no worktree hold (S32/P33 concurrency counts running steps, not idle loops), no memory beyond rows (P28). Recovery
(S7) selects `status='running'` steps; an idle loop has none, and a test pins that recovery neither touches nor mislabels it. Daemon restart reads
`idle` back.

## CR10 — What must not change (S38 OL15, restated as tests)

S31's steer/stop/continue fixtures byte-identical; `step.steer` refusal text unchanged; events keep `stepId`/`agent`; `loop.finished` still fires for
loops that finish; `done` rollups (S25) unchanged for non-resting loops.

## CR11 — Where the rest lands

**S40** starts runs from events with the same kickoff door (`source: automation`). Later: project-scope loops, `spawn`/`wait`, ApprovalPolicy.

## Test map

1. every shipped blueprint ends `done` and fires `loop.finished` as before (D1, D11)
2. `after: converse` blueprint ends `idle`, fires `loop.run.finished`, not `loop.finished` (D1)
3. message to idle loop → a turn streams, is steerable, bounded; message while a step runs → steer, byte-identical to S31 (D2, D3, D10)
4. two runs in one loop: pins per run, steps carry `run_seq`, cost per run and loop with a floor on a `partial` step (D4)
5. `run_start`: attended → ask card → approve starts; unattended → denied, no question row; `loop.run` from a human peer starts directly (D5)
6. unmet `requires:` → no run row, sentence + candidates (D6)
7. kickoff rendering parity: typed and templated first messages produce byte-identical model input; `source` differs in the thread (D7)
8. composer visibility: idle+conversation agent shows it; `done` hides it (D8)
9. idle loop and daemon restart/recovery: nothing recovered, state read back (D9)
10. a loop started unattended and chatted into stays unattended and `run_start` denies (CR7)

# Plan — S38 Organization layers

## What is settled before planning

`brief.md` §0's six rulings (loop keeps its name; no legacy, rename now; tools are capabilities and blueprints are
contracts; brainstorm is a blueprint whose chat is a step; the agent writes the file **and** the entry; a brief is
the goal before we start it) and `design.md`'s OL2–OL5, OL8, OL9, OL11, OL12, OL15. **Not settled, by design: OL1's
column rename, OL6's exit rule, OL7's column lists, OL10's names** — T0 closes them.

## Reality alignment (T0, run 2026-10-27 on HEAD `77a4c675` — every claim measured, the commands in [investigation.md](investigation.md))

- **R-1 · the Q2 table is stale; the real split gate is now written.** The `≈850 references (store 267,
  protocol 200, daemon 128, ui 108, loop 71, agentsvc 25, terminal 23, cmd 20)` table no longer matches any
  reproducible grep. Measured 2026-10-27: tier+wire+UI rename **≈423** refs (300 Go lines/64 files + 123
  UI/28), column rename **≈732** (623/109 + 109). Written thresholds, decided before measuring: split
  Phase A into S38a above **1,600 total** or **1,000 column-half**. Measured total ≈1,155 → **no split;
  Phase A stays one merge-commit.**
- **R-2 · T1–T4 land as ONE commit `(S38 T1–T4)`** — the plan's own line says the tree does not build
  between T1 and T3, and the house rule is green suites per commit; the four task files box together, each
  with its own Notes line. (Author-approved in plan review.)
- **R-3 · `aeon project …` does not exist.** `unknown command "project"` — the three `project.*` socket
  methods have no CLI verb (the P34 gap S20's R-22 predicts). T4, which touches the CLI anyway, adds
  `component`/`project` verbs (add/list at minimum); T16 extends them to the record entities.
- **R-4 · there is no blueprint-less row-kind.** `loop create` takes `-mode simple|standard|poc` (no
  `-blueprint` flag); the loop row is the chat's only container; `loop_blueprint` pins at **start**
  (store/blueprint.go:157). "No blueprint" = a mode; criterion 1's plain-chat half is S39's (its brief's
  own amendment), and S38 proves the grant gate inside a blueprint loop.
- **R-5 · `done`/`aborted` are terminal** (`decide.go:40`, `continue.go:40` refuse both) — so OL6's
  conversation-after-run needs the minimal engine delta: a **completed** (never aborted) conversation
  blueprint parks the loop at **`waiting`** (S6's existing status; no seventh status), and a message
  resumes it. Composer UI stays S31's exactly (S39's reversal).
- **R-6 · `step_steer` has no author column** (`id, step_id, text, route, reasoning_effort, state,
  created_at, delivered_at, delivered_iteration`), and OL15's suggested "in 0027/0028" is **invalid** —
  shipped migrations are immutable (AGENTS §3). `source TEXT NOT NULL DEFAULT 'person'` rides **0036**;
  `step.steer` writes only `'person'`, so LW fixtures stay byte-identical.
- **R-7 · the rename's DDL is proven, not assumed.** OL1's SQL + implicit backfill + the 0016-pattern NOT
  NULL rebuild ran on a populated store copy: integrity ok, `foreign_key_check` empty, FK survived the
  column rename. `project_id NOT NULL` lands **inside 0035**.
- **R-8 · the catalog is 33 tools, the registry pin goes 10 → 43.** Legacy MCP names measured from
  `packages/loop-state-mcp/src/tools/*.ts`: 25 total; 14 are engine-owned loop mutations (P29) and
  `loop_render` is dead (brief §0.5) — the catalog adds project 4 + component 4 + goal 5 + epic 4 + brief
  2 + task 6 + attach 2 + loop-reads 6 = **33**; `DefaultGrant()` stays `read, list, write`.
- **R-9 · `task_create`'s home is decided: surrogate `id` + partial UNIQUE.** All readers of the loop key
  are five `Scope`-taking functions in `store/tasks.go`; the engine never raw-keys the triple. `work_item`
  retired.
- **R-10 · gate answers commit inside `AnswerGate`'s tx** (`store/blueprint.go:83`, real `s.w.Begin()`) —
  `DecideBrief` joins there with the claimed-FROM guard (S35 TD1 pattern); the edge-follow reads after
  commit, so no race with `on_reject`.
- **R-11 · the brief §2 line refs drifted and three file paths moved**: `missingArtifact` is at
  `advance.go:296`/`continue.go:77` (not 283/70), the produces-prompt block at ~`advance.go:481`, and the
  `PolicyGateRequest{Tool: use.Name}` posture lives in **`internal/runtime/loop.go:635`** (brief says
  "loop.go:619"). Claims true; addresses stale — task files citing them take these.

## How we will do it

**Investigation first (T0), then strictly sequential** (the standing no-parallel-tasks direction of S34–S36). T0
produces **no code**: it writes `investigation.md` here — each question answered by running something, with the
command quoted, and closed as *settled / changed the design / still open and why* — and edits `design.md` where an
answer moves a provisional rule. Two phases follow: **A · the rename** (T1–T4) and **B · the records** (T5–T17).
Phase B does not depend on phase A's UI half, so if T0-Q2 says the rename is bigger than its written threshold the
slice **splits at that boundary** (S38a rename, S38b records) rather than the crux waiting behind a refactor.
Every task lands with tests and suites green, and measured numbers go in its Notes the day they are measured
(AGENTS §4a).

## T0 — Investigation (no code)

Closes Q1–Q10 below by running things; see [`tasks/T0.md`](tasks/T0.md). **Depends: nothing (first).** Exit: `investigation.md` written, provisional rules edited to final with dates, any plan amendment appended.

- **Q1** the blueprint-less chat today (rows, agent, tools). **Q2** the rename's blast radius and the **written split threshold** (≈850 references measured by grep: store 267, protocol 200, daemon 128, ui 108, loop 71, agentsvc 25, terminal 23, cmd 20). **Q3** field mapping from real briefs/plans. **Q4** chat before/during/after a run (is `done` terminal today — yes, `decide.go:40`/`continue.go:130`), whether a steer row can carry an engine author. **Q5** `BuildTools` plumbing and the registry-pin amendment text. **Q6** two kinds of task: every reader of `(component_root, loop_slug, taskid)`, surrogate id vs `work_item`. **Q7** dry-run of the DDL on a populated store. **Q8** where a gate answer resolves. **Q9** the legacy-MCP-to-catalog table from `packages/loop-state-mcp/src/tools/*.ts`. **Q10** spec gaps for the author.

## The tasks (implementation-grade; each has Goal / Prerequisites / Files / Steps / Tests / Commands / Definition of done / Do NOT)

### Phase A — the rename (T1–T4 merge as one unit; the tree does not build between T1 and T3)

| Task | Outcome |
| --- | --- |
| [T1](tasks/T1.md) | Migration `0035_org_layers`: `project`→`component`, `project_root`→`component_root` everywhere, new `project` tier, one implicit project per component |
| [T2](tasks/T2.md) | Store Go layer renamed (`Component`, `Project`, `ComponentRoot`), group CRUD, implicit-project creation |
| [T3](tasks/T3.md) | `component.*` and new `project.*` methods, fixtures, **`PROTOCOL_VERSION` 2**, protogen |
| [T4](tasks/T4.md) | CLI flag and the UI follow; a single implicit project's layer is hidden |

### Phase B — the records

| Task | Outcome |
| --- | --- |
| [T5](tasks/T5.md) | Migration `0036_records`: goal, goal_component, epic, brief, review (user-only), attachment, task_loop, loop columns, **task rebuilt** (surrogate id, `manual`, assignee, parent, approval) |
| [T6](tasks/T6.md) | Store functions for the entities; claimed-FROM decisions; attachments with currency |
| [T7](tasks/T7.md) | Task store: loop tasks unchanged, manual tasks, read-only guard, approval, the board read with floor-flagged cost |
| [T8](tasks/T8.md) | Step id in ctx, `ToolContext.Records`, `StepRunner.Records` — no signature change |
| [T9](tasks/T9.md) | `internal/records` catalog (pure), daemon `RecordsService`, tools projection, registry pins (non-task names) |
| [T10](tasks/T10.md) | Task family, handler grants, the `brainstormer` handler — a granted chat agent creates a task with no blueprint |
| [T11](tasks/T11.md) | Blueprint keys `output`, `emits`, `requires`, `decides` + validation against handler tools |
| [T12](tasks/T12.md) | `missingRecord`, the contract prompt block, `Records` reader in the loop |
| [T13](tasks/T13.md) | `loop.start` `goalId`/`taskId`, `requires:` enforcement with candidates, goal pin |
| [T14](tasks/T14.md) | `brainstorm` and `goal-to-brief` blueprints, the gate→`DecideBrief` join, replay walks |
| [T15](tasks/T15.md) | Socket surface: catalog methods (parity), `*.decide` (human only), `task.list`, `goal.show`, `task.changed` |
| [T16](tasks/T16.md) | `aeon goal|brief|task` CLI (socket only) |
| [T17](tasks/T17.md) | Close-out: regression proofs, measured counts, DEMO walk, deletion drill, record |

**Cross-slice contract.** S39 starts after **T13** (it needs the tools, `emits`/`requires` and `loop.start` links). S40 starts after **T7, T15** (it edits the task/goal store functions to write an outbox row, and consumes the surface). Both cite S38 tasks by these numbers.

## Sequencing summary

```
T0 ► T1 ► T2 ► T3 ► T4 ║ T5 ► T6 ► T7 ► T8 ► T9 ► T10 ► T11 ► T12 ► T13 ► T14 ► T15 ► T16 ► T17
```

Linear by ruling and by nature: investigate; rename the foundation; give the entities their tables; give agents the
tools; let a blueprint ask for what the tools make; walk the brainstorm and the plan; open the door; write it down.
Eighteen tasks; the catalog (about two dozen tools, **one** definition), **one** new package (`internal/records`), **one or two** migrations,
**three** optional YAML keys, **zero** new grants by default, and **zero** change for a loop or chat that holds no catalog tool.

## Amendments (dated; from T0)

- **2026-10-27 (T0):** (a) Q2's numbers replaced by R-1 with written thresholds — Phase A **not** split;
  (b) T1–T4 merge-commit ruling (R-2); (c) `component`/`project` CLI verbs assigned to **T4** (R-3);
  (d) OL6's `waiting`-park is S38's engine delta, OL15's steer `source` moves to **0036**; (e) the QA suite
  lands at `docs/test_suites/aeon-org-layers.md` (T17), ids `QAOL-01` upward, properties not counts;
  (f) T0's Q10 list handed to the spec's author (five items, in investigation.md).

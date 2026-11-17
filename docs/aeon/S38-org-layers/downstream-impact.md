# Downstream impact — what S38, S39 and S40 do to the open slices

**Written 2026-10-27, corrected the same day.** The first pass read a checkout that predated the merge of **S18, S19 and S20**; those three are now built and merged (§0 below), which changes several rows. Review of every slice not yet built against what
S38 (rename, task model, management catalog, blueprint keys), S39 (chat runs, `idle`) and S40 (automation) change. Four
read-only reviews read each slice's docs and grepped `aeon/`; **nothing in the slices below is built** (S31 is T10 UI built and T11 a spec; **S18, S19, S20 were reported "unbuilt" in error and are built — see §0**). This file is a report, not an amendment: no other slice's docs were edited.

**Provenance and limits.** Findings come from the reviewers' reading and grep, with `file:line` evidence where given. Items
marked *(reading)* are interpretations. S30's conclusion came from keyword matches on its brief/design/plan, not a full read.
S18 and S36 were reviewed from docs plus grep. Nothing was compiled or run.

## 0. Correction: S18, S19 and S20 are built (measured after your merge)

| Fact (2026-10-27, from `aeon/`) | Evidence | Consequence |
| --- | --- | --- |
| Highest migration is **0034** (S18: 0027–0030; S19: 0031 `loop.allow_outward`; S20: 0032 `trigger_schedule`+`trigger_fire`, 0033 `trigger_fire_inputs` + `loop.started_by`/`trigger_id`, 0034 `trigger_gap`) | `store/schema.go:79-86` | S38 takes **0035/0036**, S39 and S40 the next free; every migration **count assertion says 34** (`schema_test.go:888`, `policy_test.go:307,458,1318`, `loop/counters_test.go:280,284`) and must be bumped by whoever lands a migration |
| `internal/narrow` exists: **8 actions** (7 read/webhook + `aeon_loop_start`), kinds read/webhook/trigger, `attended` force-overwritten to false | `narrow/actions.go:117-190`, `narrow/trigger.go:39-65` | S38's catalog, `*.decide`, `loop.message`, `loop.run` must stay off it; the only `projectRoot` there is two test strings (`errors_test.go:166,211`) |
| `internal/trigger` exists (schedule, clock, DST, overlap, reconcile, webhook); **T7 TriggerAccess, T8 control/`aeon trigger` CLI, T9 demo are not built** | `Gate.Access` never assigned; `clock.go:253` "access NOT wired" | S40 cannot create base trigger tables — it extends S20's; S27 may now depend on S20 (but not on its unbuilt T7/T8) |
| A fire starts a loop through `trigger.Starter.StartFromTrigger` → `(*Daemon).StartFromTrigger` (`daemon/trigger_fire.go:87`) → the **same** `StartBlueprintLoop` closure a person uses, `Attended:&false`; provenance stamped in a **separate** write afterwards | `trigger/clock.go:35,201`, `store/loops.go:150-154` | S40 reuses `Starter`/`Fire` (adds event + goal/task fields), not a new `startLoop`; the separate provenance write is a known gap |
| `trigger_schedule.kind CHECK IN ('schedule','webhook')`; `trigger_fire` is FK'd to `trigger_schedule` with `UNIQUE(schedule_id, instant)` | migration 0032 | event fires need a new table (or a rebuild) |
| S20's overlap guard treats `running`/`waiting`/`blocked` as "still going"; **anything else — including S39's `idle` — does not block** | `trigger/overlap.go:45`, `store/trigger_rows.go:180` | S39 must decide whether a resting loop blocks a fire (probably **no**, but it must be written down) and update this reader |
| Fire outcomes are **five** (`fired, did_not_fire, could_not_say, skipped, refused`), with daemon-down gaps in `trigger_gap` | `trigger/record.go:27-47` | S40 docs said "three states"; align |
| Tool registry still **ten** names; `PROTOCOL_VERSION` still **1**; 164 conformance fixtures, **27** contain `projectRoot` | `toolset_test.go:231`, `version.go:43` | S38 pins and the v2 bump are unchanged; the rename touches 27 fixtures |
| Rename surface grew: `ProjectRoot|project_root|projectRoot` = store 267 · protocol 206 · daemon 151 · ui 109 · loop 74 · agentsvc 25 · terminal 23 · cmd 21 · narrow 2 | `grep -rc` | ≈ 480 occurrences |
| A `requires:` blueprint started by a schedule/webhook fire is **refused** (the fire carries no goal/task) | `trigger_fire.go:96` | correct; S40 event fires carry the goal/task id |

## 1. Verdict table

| Slice | Verdict | Build relative to S38–S40 | What changes |
| --- | --- | --- | --- |
| **S18** providers | **BUILT** — carries no `projectRoot`; adds 0027–0030 | done | nothing to refactor; its migrations shifted S38's numbers (§0) |
| **S19** narrow surface | **BUILT** (8 actions) | done | S38 must not add actions; update 2 test strings; keep `attended` forced false and `allowOutward` unexposed to goals; add the T15 guard test |
| **S21** remote agents | AMEND DOCS · **S19 dependency is now met** | **can go before or after S38–S40** | callback is a narrow action, never a tool or `*.decide`; remote agents get no catalog tools; any new methods use the v2 names if built after S38 |
| **S22** retrieval | AMEND DOCS + small refactor | after S38 | `projectRoot`→`componentRoot`; **`search` is the 11th tool, not the 10th**; do not index `chat_message` |
| **S23** first run | AMEND DOCS | **last** (reports on everything) | "project"→"component"; blueprint-count assertions; `doctor` reports dispatcher/triggers/protocol 2 |
| **S24** loop queue | AMEND DOCS, then build after | after S38–S40 | **claims migration 0026 (taken)**; columns on `project`→`component`; `idle` must not count as terminal; use S40 `startLoop`; relate to manual tasks |
| **S25** history | AMEND DOCS | either order | `idle` row; H1 groups **loop** tasks only; index `task(project_root, kind)` is wrong after the task rebuild |
| **S26** roadmap | **REDESIGN on goals** | after S38–S40 | `roadmap_item` duplicates goal/epic/manual task; "epic is a tag" conflicts with the `epic` table; ideation = `brainstorm` blueprint; `triggered` provenance = S40 |
| **S27** continuous | **REDESIGN on S40** | after S40 (S20 is built, but its T7/T8 are not) | a work-selector over ready goals/tasks feeding S40's dispatcher, not a second start path; fleet cap belongs in dispatcher limits |
| **S28** memory | AMEND DOCS | either order | `scope_key = project_root`→`component_root`; `memory.taskid` stays (loop-task key); no hard-coded migration number |
| **S29** triage | **REDESIGN in part on S38/S40** | after S38 + S40 | a triaged bug is a manual `bug` task: drop the `bug` table, `file_bug` and `triage_dispatch.go`; keep the three-way validation verdict, `bug_vec`, duplicate detection, D10 feed |
| **S30** code graph | AMEND DOCS | prefer after S38 | `graph_node/edge.project_root`→`component_root`; new `graph` tool grows the pinned list |
| **S31** T10 (built) / T11 (spec) | T11 AMEND; T10 REFACTOR AFTER S39 | **T11 before S38**; T10 after S39 | `LoopWorkspaceView.tsx:82` `ended`/`readOnly` rule vs `idle`; scenario text "completed loop: composer is gone"; 21 UI files read `projectRoot` |
| **S32** parallel agents | AMEND DOCS | after S39 | the cap is **per component** *(reading)*; idle loops hold no slot; S40 dispatcher respects it |
| **S33** telemetry | AMEND DOCS | design after S38 + S39 | "project tier" = component tier; stamp `run_seq`; protocol additions go in v2 |
| **S34** trust bench | AMEND DOCS (design addition) | **before** S38 is cheap | readiness view must group/collapse ~38 catalog tools; grants are two-part (handler + S11 row, no default grant) |
| **S35** trust desk | AMEND DOCS | before S38 preferred | add exclusions: no SQL in protocol handler files; `*.decide` are direct human-peer writes (not proposal kinds); migration number claimed at seeding |
| **S36** MCP dispatch | AMEND DOCS | **first** (smallest) | `BuildTools` edit overlaps S38 T8 (merge conflict, no logic conflict); reword "ten dot-less words" |
| **S37** agent admission *(a different slice from S38's org layers)* | **no code collision** | either order; cheap | stays CLI-only commit (it widens code capability); only competes for migration numbers; one new `kind` in S35's closed enum; `proposal` is also a word org layers uses in prose and in `proposal.md`/`proposal-author` — different layers |

## 2. Findings that matter most

1. **Roadmap/continuous vs goals/triggers (S26, S27).** The largest conceptual overlap. S26's ladder (`idea`…`done`), its epic-as-tag
   rule and "ideation is a loop ending in `brief.md`" are what S38's `goal`/`epic`/`brief` tables and the `brainstorm` blueprint now do;
   S27's work selection is what S40's outbox and dispatcher do. Keep from S26 only what S38 lacks: the forward Gantt, three-state
   priority (D2), the estimate range (D3), as columns or read models. Do **not** seed S26/S27 as written.
2. **Triage (S29)** overlaps the task model: `bug.validation_state` (reproduced / not-reproduced / could-not-tell) is a verdict on a
   manual `bug` task. Redesign after S38 + S40 so triage files a manual task through the catalog and starts from an S40 trigger.
3. **Migration numbers are a shared resource.** `0034` is the highest (as of 2026-10-27); S38 takes the next two (0035–0036), S39 the next, S40 the next. **S24 claims 0026
   (already taken), S35's `proposal` table claims "next".** Rule for every slice: *look up the highest number at seeding and take the next
   free one; if a slice lands first, the others renumber.* No migration number should be hard-coded in a task before it is seeded.
4. **The pinned tool list is a contended file.** Five places pin "ten": `internal/tools/toolset_test.go` (:230 and a copy ~:136-158),
   `class_test.go:91`, `testdata/rerun_classes.json`, `internal/agentsvc/skill_test.go:156`. S38 T9/T10 amend them; **S22 (`search`),
   S30 (`graph`) and S24 (`report_size`, check) also add tools** — each must cite the previous dated amendment, never "the tenth".
5. **`idle` is a new state every reader must classify.** S24 (queue advance), S25 (history), S31 T10 (composer), S32 (slots) and S19
   (`read` of loop status) all treat `done`/terminal in ways `idle` breaks; none yet knows it exists.
6. **Renames are mostly mechanical.** `project_root`→`component_root`, `project.*`→`component.*`, "per-project"→"per-component" in S22,
   S23, S24, S25, S28, S29, S30, S31, S33. The one *tier* decision to record: these are all **component** scoped; a project-group view
   is a later slice for each.
7. **Authority doors stay human.** S19 must not expose `goal.*`/`brief.*`/`task.*` writes, `*.decide`, `loop.message`, `loop.run` or
   trigger writes on the narrow surface; S21's callback is a narrow action; S40 trigger authoring stays a direct human-peer write
   (cite S35 §3; do **not** add a `trigger_add` proposal kind — S35's `kind` enum is closed).

## 3. Recommended order

```
S36 ─► S34 ─► S35 ─► S37 (admission) ─► S38 ─► S39 ─► S40 ─┬─► S24, S29, S30, S33, S22
 (S31 T11 before S38)                    │   (S21: S19 built — free to go any time)
 (S18, S25, S28 any time)                ├─► S32 (after S39)
                                         ├─► S26, S27 (redesigned on goals/S40; S27 also needs S20)
                                         └─► S23 last
```

**Why S36/S34/S35 first (the reviewers' advice, *(reading)* on cost):** they touch no `project` surface, so the rename never has to
cover them, and S36 holds `BuildTools` in a stable shape that S38 T8 then extends. Putting them *after* S38 costs only a mechanical
renumber of migrations and re-pinning the registry counts — an acceptable alternative if the trust trio is not ready.

## 4. Corrections to earlier answers

- I said S21 does not depend on S38–S40 (true) and later that it was blocked by S19. **Both were based on a stale checkout:** S19 is built,
  so S21 is **not** blocked and can run before or after S38–S40.
- I said S20 was "in flight"; it is merged except T7 (TriggerAccess), T8 (control + `aeon trigger`) and T9 (demo).

## 5. Concrete amendments to make (not yet made)

| Doc | Edit |
| --- | --- |
| S36 `brief.md:75`, `design.md` MD10 | reword "ten dot-less words"; pin #1 asserted against the whole `Registry()`; dated line |
| S34 `design.md` | readiness view groups the catalog family; note two-part grants |
| S35 `design.md`/`brief.md` | add "no SQL in protocol handler files"; state `*.decide` and trigger authoring are direct human-peer writes; naming note (`proposal` table vs `proposal.md`); migration claimed at seeding |
| S19 `design.md` | "Out of scope" lists the catalog, `*.decide`, `loop.message`, `loop.run`, trigger writes; enumeration test; T6 calls `startLoop` |
| S21 `design.md` | callback is a narrow action, not a tool/decision; no catalog tools for remote peers |
| S22 `design.md:229`, `T6` | `search` is the eleventh tool; cite S38's amendment; `componentRoot`; exclude `chat_message` |
| S23 | wording; blueprint-count-agnostic tests; `doctor` rows |
| S24 `plan.md`/`T1` | migration number at seeding; columns on `component`; `idle` not terminal; call `startLoop`; relation to manual tasks |
| S25 | `idle` row; loop-task wording; fix `task(project_root, kind)` index |
| S26, S27, S29 | redesign notes before seeding (§2.1, §2.2) |
| S28, S30, S33 | rename and tier wording; no hard-coded migration numbers |
| S31 `tasks/T10.md` notes, `T11.md`, `CODEMAP.md` | composer rule; `projectRoot` names |
| S32 `SKETCH.md` | per-component cap; idle slots; dispatcher respects the cap |
| **S38 itself** | add to `brief.md` "What this is not": a pointer to this file for S24/S26/S27/S29 overlaps; T15 and S40: "no SQL in protocol handler files" rule |

## 6. Decisions taken (2026-10-27, by the author)

1. **Slice numbering — done.** `S37-agent-admission` keeps S37. The org-layers trio is renumbered **S38 org layers · S39 chat runs · S40 automation**
   (`git`-untracked directories renamed `S38-org-layers`, `S39-chat-runs`, `S40-automation`; decision prefixes `OL`, `CR`, `AU` unchanged; the
   automation slice's "later" follow-up — ApprovalPolicy, spawn/wait, config inheritance — is now **S41**). Every reference inside the three folders
   was shifted in one pass; references to admission were protected and left as S37.
2. **A plain chat has its own agent and its own blueprint.** *"Write me a hello world in C++"* is answered by a dedicated general **`chat`** agent
   running a built-in **`chat`** blueprint (one conversation lane, `after: converse`). It may not exist yet and **must be added** (S39 T6); the
   specialists — `brainstormer`, `brief-author`, `project-planner`, … — are separate and are reached through `run_start`, never the default.
   Consequence: every loop still pins a blueprint (no blueprint-less loops); "plain chat" means *the loop's pinned blueprint is `chat`*, which is
   what S38 T10's `isPlainChat` will test.
3. **S20 stays in flight.** Its scheduler is not wired into `serve.go` yet and its T7 (TriggerAccess), T8 (control CLI) and T9 (demo) are open. S40
   T6 (wire the dispatcher beside the scheduler) and the wording of S40's relation to S35/PL15 (trigger authoring is a direct human-peer socket write,
   not a staged proposal) are **footnotes to revisit when S20 completes**, not blockers.

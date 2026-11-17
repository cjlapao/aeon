# Pre-flight — S55 Loop control, caps & watchdog (T0)

Date 2026-11-01. Worktree `s55-loop-control` (based on `main` @ 92ea7c6f, which already
contains **S44-extra** `4df043b7` and **S43-extra** `727cb6ba`). Baseline: `go build
-tags sqlite_fts5 ./...` green (cgo shim at `.tmp/cgo-shim/sqlite3.h`).

**Verdict: no hard blockers. All cross-slice dependencies the brief names are present and
merged in `main`. S55 can be built on top of the current tree.** The items below are the
things that *must* be true for the later tasks, re-walked against the tree as it stands,
plus the three or four "not yet checked" rows from the brief that the planning pass left
open. None of them is a "build this first" — they are facts the implementation reads from.

## 1. Cross-slice dependencies (the "build this before" question)

| Dependency | Status | Evidence |
| --- | --- | --- |
| **S44-extra — the tree** (`rootId`, `component.parent_id`) | **present, merged** | `internal/store/components.go:25,62,196` (`ParentID`); `automation.go:165` `COALESCE(parent_id, id)` is `RootOf`; migration `20261030200000_component_tree.sql` |
| **S44-extra — effective settings** | **present** (this is `config.Reader`, env→database→default, `internal/config/{settings,layers}.go`) | `AGENTS.md` §4 "Settings are declared, never ad hoc"; the brief's "effective-settings resolver" is the S44 reader, not a new function |
| **S44-extra — `component_telemetry` subtree total (CT7)** | **present** | `internal/store/telemetry.go` + `internal/store/ct_golden_test.go` |
| **S43-extra — the seed engine** | **present** | `internal/seed/` (`engine.go`, `applier.go`, `registry.go`); migration `20261031150000_seed_ledger.sql` |
| **S33 — telemetry rollups** | **present** | `internal/store/telemetry.go`, `component_telemetry`/`workspace_telemetry`, `EventTriggerSpend` |
| **S31/S39 — the stop machinery and run states** | **present** | `protocol/step_stop.go`, `daemon/stepcancel.go`, `runStateFor` (run state `cancelled`), `loop/rest.go` |
| **S10 — `cost_cap` policy stub** | **present, unchanged** | `internal/policy/spec_stubs.go:83-93` `CostCapSpec{Token,Max}`; stub evaluator registered |

**Conclusion:** nothing S55 needs is missing. There is **no "build this first" slice**. The
only "before" work is the work the plan already schedules (T0–T12). S55 does **not** depend
on S45 (brief §5: "Not S45"); S45 is only a *merge* concern (see §4).

## 2. Verified facts re-walked (same / drifted)

| Fact | Verdict | Where (now) |
| --- | --- | --- |
| Loop `status` is `TEXT` with no CHECK; six values | **same** | `schema.go:775` `CREATE TABLE loop`; writers `advance.go`, `rest.go` |
| `endStatus(bp, aborted)` is the **one** writer of `done`/`aborted` | **same** | `loop/rest.go:29-37`; `advance.go` writes only `blocked`/`waiting` (12 sites), never a terminal |
| Run state `cancelled` via `runStateFor(Flags["aborted"])`, from `emitIfOver`→`EndRun` | **same** | `loop/rest.go:44-52,61-80` |
| Step stop parks `suspended`/`pause_reason='user'`; engine → Kind `stopped`; loop `waiting`, `LastReason="stopped"`, `QuestionID 0` | **same** | `protocol/step_stop.go`, `daemon/stepcancel.go`, `advance.go` |
| Runtime returns `failed` (not `blocked`) on a cancelled ctx; MaxIterations/Deadline checked before each call | **same (drifted line)** | `runtime/loop.go:241-252` (was 215-236; bounds at 241-246, cancel→`failed` at 252) |
| Loop runners start on `context.Background()`; only `StartLoops(recoveryCtx)` is cancellable | **same** | `daemon/start_loop.go`, `runs.go`, `blueprint.go` |
| `AnswerEscalation` exists; only a test caller; `AnswerBlueprintLoop` routes by state | **same** | `loop/escalate.go:317`; `daemon/blueprint.go:370-393` |
| Cost: `known`/`partial`/`unknown`; `component_telemetry`/`workspace_telemetry`; `EventTriggerSpend` | **same** | `internal/store/telemetry.go`, `automation.go` |
| `epic.budget_usd` stored + settable, read by nothing | **same** | `store/epics.go:27,72,96-111` |
| **loop → goal → epic link** (the T0 must-verify) | **same, confirmed** | `loop.goal_id` → `goal.epic_id` (`goals.go:32,114,159,176-179`; `goal.epic_id TEXT REFERENCES epic` in `20261030200001_project_rekey.sql:60`). Epic spend is reachable. |

## 3. Items the planning pass left "not checked" — resolved

| Item | Answer |
| --- | --- |
| `ArchiveLoop` status guard | exists; the control core (T3) keys its CAS off `status` only and must not race an archived row (read `ArchiveLoop` before T3) |
| `tools/question.go` `HasOperator` | the `StepRequest` seam pattern T7's `Budget` seam copies (read before T7) |
| S20 schedule-side limits / S24 queue reactor on a `blocked` loop | **`queue_reactor.go:62-70` raises a queue question on `loop.blocked` for ANY kind (CG2).** A `cap-hit`/`stuck` block will trigger it. T7 must decide whether the queue-question text should carry the `cap-hit` reason (design LC9/T7 §18). Not a blocker; a text decision. |
| bash `commandMaxLen` | exists in `tools/spawn.go`; unchanged by S55 |
| **last-activity timestamp for the watchdog** | **`loop.updated_at` exists** (written in `blueprint.go:62,69,120,127`); `step` rows carry `started_at`/`updated_at`. The watchdog's `no-activity` signal has a real source. `same-position` reads the pinned-state position counters; `step-over-deadline` reads `Manifest.DeadlineAt`. |
| `policy` `evaluate.go:44` `Cost` use | the `Cost` token is `CostCap`'s; S55 leaves the stub untouched (LC7) |
| **current migration count** | **45 frozen** (1–45, last `0045_proposal_agent_import`, `counters_test.go:300-305`) **+ 18 timestamped files** = **63 total**. (AGENTS.md says "43"; the test is authoritative — AGENTS.md is stale here.) **S55 adds 3 files → 66.** |
| `schema_test.go` "want N" | the count assertions that matter use `len(Migrations())` dynamically (auto-adjust: `schema_test.go:435,481`, `store_test.go:291`). Only *literal* vintage pins (e.g. `counters_test.go` `[:38]`, `automation_schema_test.go:61`) stay fixed; they pin the *frozen* 45, which S55 does not change. |

## 4. The S45 merge collision surface (the user's explicit worry)

S45 is **not in `main`** (branch `s45-policy-management`, tip `a0388ecd`). It is a sibling
being worked in parallel. When S55 lands and S45 lands, the collisions are in the **shared
registry files**, *not* in S55's new files:

| Shared file | S55 touch | S45 touch | Collision type | Mitigation |
| --- | --- | --- | --- | --- |
| `internal/store/migrations/*.sql` | 3 **new files** (loop_control, budget, watchdog) | 2 **new files** (policy_change, policy_origin) | **none** — different filenames | the file-based migration scheme was built exactly for this (AGENTS.md §3) |
| `protocol/errors.go` | +3 error blocks | +134 lines | adjacency | append S55's blocks; take both on merge |
| `protocol/method.go` | +loop/budget/watchdog register lines | +112 lines | adjacency | append; `git` three-way keeps both |
| `store/schema_test.go` | migration count | +12 lines | literal | both bump; regenerate |
| `protocol/handshake_test.go`, `conformance_test.go` | want-list / fixtures | want-list / fixtures | adjacency | append S55's block with a dated comment |
| `daemon/daemon.go`, `cmd/aeon/serve.go`, `seed/seed.go`, `store/store.go` | loopCtx/drain/seed | policy authoring | adjacency | distinct hunks; resolve by keeping both |
| `ui/src/proto/contract.ts` | regenerated by `make protogen` | regenerated | **regenerate, don't hand-merge** | run `make protogen` after the merge |

**The safe strategy** (this is the "be careful not to collide" the user asked for): make every
S55 edit to a shared file **additive and in its own hunk** (append error constants, append
method-registration lines, add a dated want-list block, append a new seed declaration file
rather than editing an existing one), and **never edit a S45-owned line**. On merge, the
file-based migrations and the new-file S55 code merge with zero conflict; the shared files
resolve by *keeping both sides*. `contract.ts` is regenerated, not hand-merged.

## 5. What must be true for each task (one line each)

- **T2** — 3 of the 63→66 migrations are **new files**; the CAS store functions live in a
  new `loop_control.go`; no `loop` table change; `endStatus` stays the only terminal writer.
- **T6** — `budget`/`budget_event` are new files; the epic backfill reads `epic.budget_usd`
  (confirmed present) and the `loop→goal→epic` link (confirmed) makes epic spend reachable.
- **T7** — the runtime `failed`-on-cancel at `loop.go:252` is the reason in-step cap hits
  **suspend** (`pause_reason='cap'`), they do not `block`; the `HasOperator`/`Telemetry`
  seam is the model for `StepRequest.Budget`.
- **T9** — the watchdog's `no-activity` signal reads `loop.updated_at` (present); the S43
  seed engine (`internal/seed/`) carries the `watchdog.default` set.
- **T12** — close-out must bump the *dynamic* count assertions only; the *literal* frozen
  45 and the `[:N]` vintage slices stay.

## 6. Bottom line

The pre-flight's question — *"is there anything else we need to build before?"* — answers
**no**. Every dependency the brief names (S44-extra tree, S43-extra seed engine, S33
telemetry, S31/S39 stop machinery, the `cost_cap` stub) is present and merged in `main`, and
the two facts the plan specifically flagged as must-verify (the `loop→goal→epic` link and
`epic.budget_usd`) are both confirmed. The only cross-slice hazard is the **S45 merge
collision** in shared registry files, which is handled by the additive-hunk strategy in §4.
S55 is clear to build on the current tree.

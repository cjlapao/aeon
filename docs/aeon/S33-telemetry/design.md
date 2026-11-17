# Design — S33 Telemetry

Fills in `brief.md`'s §7 measurements and locks the schema, the write path, and the read surface. Rules are
numbered **TL1–TL13**, cited from `plan.md` the way S34's TB-rules and S36's MD-rules are.

## §7's three measurements, answered

**Usage reliability (measured against both live adapters).** `model.Usage`'s five fields are each `*int`
(`internal/model/types.go:244-252`, M9: "absent is not zero"). Both `anthropic.go`'s and `openai.go`'s
`accumulateUsage` already set a field **only when the provider's own payload carried it**
(`if u.InputTokens != nil { a.usage.InputTokens = u.InputTokens }`, confirmed in both files) — nil-safety is
already honored end to end. **Conclusion: the per-turn row's columns are nullable, mirroring `model.Usage`
exactly — no coercion to zero anywhere in this slice.**

**Category tagging's hook point (D4).** `internal/runtime/types.go:203`'s `SectionSpec` — the unit
`AssembleContext` (`internal/runtime/context.go:114`) renders into the frozen initial context — carries only
`Title`, `Text`, `DeclaredFile`. **No category concept exists anywhere today**, confirming the brief's "new
work" framing literally: there is nothing to read, only a field to add. Decision (TL6, below): this slice
adds `Category` to `SectionSpec` for everything assembled *before* the first model call, and tags transcript
turns by their already-distinct structural position (assistant text, tool_use, tool_result) at the existing
append points inside `RunLoop` — two hook points, not one, because "composition" spans both the frozen
opening context and the turns that follow it.

**Write volume.** No existing write-batching precedent exists in this codebase to reuse (`DB15`'s two-pool
question is still open and unrelated — it is about read/write pool separation, not row volume). A per-turn
row is one row per model call — the same order of magnitude as the `step` table's existing one-row-per-step-
attempt, not per-character or per-token. **Decision: no batching in this slice.** If a measured long loop
(S25's "months of loops" worry) later shows this wrong, that is a follow-up with a number attached, not a
speculative pool built now.

## TL1 — Four tiers, not two, because S38 landed between the brief and this design

The brief (written 2026-10-26) locked **Loop → Project → App**. **S38 landed since** (`STATUS.md`'s board):
`project` was renamed `component`, and a *new* `project` (a group of components) was added above it
(`internal/store/schema.go`'s migration `0035_org_layers`, confirmed this session — `component` now carries
`project_id TEXT NOT NULL REFERENCES project(id)`). The brief's two rollup tiers are now **three**:

```
LOOP           full fidelity, cascade-deleted when the loop is archived
  ↓ incremental, same write
COMPONENT      survives archival — one row per component.id
  ↓ incremental, same write (via component.project_id)
PROJECT        survives archival and component moves — one row per project.id
  ↓ incremental, same write
WORKSPACE      survives everything — one singleton row, the whole store
```

**"Workspace" is this store, for this person** — S28's own precedent ("person memory is cross-project and
lives at the store level, which S4 already made per-user") carried to cost/time instead of learnings. One
row, not a table keyed by anything, because a store has exactly one.

Every `brief.md` reference to "the app tier" means this design's **workspace** tier; every reference to
"the project tier" means this design's **component** tier (the brief's "project" is pre-S38 vocabulary for
what the code now calls a component). This substitution is total and is not re-stated per occurrence below.

## TL2 — One write, three bumps, same transaction

D7's rule, now three-deep instead of two: **a loop-tier write updates component, project, and workspace
sums in the same database transaction.** Never a batch job, never computed at archive time, never computed
on demand by summing the tier below. The chain is mechanical: loop write → `component_root` resolves to
`component.id` (the `component_root` table, confirmed this session: path → `component_id`, a component may
have several roots) → that component's row bumps → its `project_id` resolves the project row → that bumps →
the one workspace row bumps. Four table writes per telemetry-producing event, one transaction.

## TL3 — Component moves transfer, they do not trigger a recompute

**New finding, not in the brief**, because the brief was written before `UpdateComponent`'s move behavior
was confirmed load-bearing: `internal/store/components.go`'s `UpdateComponent` **already lets a component
change `project_id`** — *"ProjectID, when set, MOVES the component to another group — the loops ride along
unchanged, because the scope key is the component's ROOT, never the group above it"* (comment, confirmed
this session, pinned by `TestMovingAComponentKeepsItsLoops`). A move happens **after** some of the
component's loops may already be archived — their loop-tier detail is gone, so a "recompute the project's
sum from scratch" is not just expensive, it is **impossible** for archived history.

**The resolution the component tier's own survival makes possible**: the component tier is the stable unit
of transfer. `UpdateComponent`, in the **same transaction** as the `project_id` column change, reads the
moving component's **current** rollup row (its running totals, by TL1 already correct and already summed
across every loop that ever ran there, archived or not) and:
1. subtracts those totals from the **old** project's row,
2. adds them to the **new** project's row.

No loop-tier or archived data is touched or re-read. The workspace tier is untouched by a move (it sums
every component regardless of project). This is this slice's **second** write-path edit beyond the step
runtime — `UpdateComponent` gains one additional statement pair, named here so T-whichever-task implements
it does not discover the collision by surprise the way S31/this-slice's R1 discovered `loop.go`'s.

## TL4 — Time is a first-class metric, rolled up exactly like tokens and cost

**Not an afterthought, and not derived from token counts.** `step` already carries `wall_ms`/`agent_ms`
(`internal/store/schema.go:849`, confirmed this session) at step granularity — this slice adds the same
*shape* one level finer (per turn) and rolls durations up through all three tiers **alongside** tokens and
cost, as its own summed column, not something read off a timestamp range after the fact. **Why this
matters beyond the UI (the user's own stated reason): a future agent sizing work — "how long does a loop in
this component usually take" — needs a real number here, not a derivation from `started_at`/`ended_at`
on live rows that an archive will delete.** TL10's socket methods expose duration sums with the same
three-state honesty as cost, at every tier, for exactly that consumer.

## TL5 — Per-turn usage rows (D1)

```sql
CREATE TABLE step_turn (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id          INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  iteration        INTEGER NOT NULL,
  started_at       TEXT NOT NULL,
  ended_at         TEXT NOT NULL,
  wall_ms          INTEGER NOT NULL,
  input_tokens     INTEGER,   -- nullable, mirrors model.Usage (M9) — NULL means "not reported", never 0
  output_tokens    INTEGER,
  cache_read_tokens    INTEGER,
  cache_write_tokens   INTEGER,
  reasoning_tokens     INTEGER,
  cost_usd         REAL,
  cost_state       TEXT NOT NULL DEFAULT 'unknown' CHECK (cost_state IN ('known','partial','unknown')),
  route            TEXT NOT NULL   -- which route served this turn (routes can change mid-loop, S18)
);
CREATE INDEX idx_step_turn_step ON step_turn (step_id);
```

`ON DELETE CASCADE` to `step`, which already cascades from `loop` (confirmed this session,
`archive.go`'s `DELETE FROM loop` comment: *"Its cascades take its tasks, phases, steps, events, memory and
questions with it"*) — a `step_turn` row needs no explicit delete anywhere; archival already removes it
(D8, free).

**Written at `internal/daemon/step.go`'s... no** — the write is at the point `result.Usage` first exists,
which is `internal/runtime/loop.go:550` (confirmed this session: `st.Usage = addUsage(st.Usage,
result.Usage)`), **inside `RunLoop`, the same function S31 T4 edited for steer-draining** (confirmed: the
steer drain sits at `loop.go:226-234` and `:295-303`, a different point in the same function — not a line
collision, but a same-file merge risk real enough to name, per R1). The capture call goes immediately beside
the existing `addUsage` line, carrying `iteration`, `result.Usage`, `result.Cost`, and the turn's measured
wall time (bracket the model-call section already timed informally by the stream-consumption loop above
it — `VERIFY` whether a `time.Now()` pair already exists to reuse before adding a second clock read).

## TL6 — Category tagging (D4)

`SectionSpec` (`internal/runtime/types.go:203`) gains:
```go
type SectionCategory string
const (
    CategorySystemPrompt    SectionCategory = "system_prompt"
    CategoryInjectedContext SectionCategory = "injected_context"
)
type SectionSpec struct {
    Title        string
    Text         string
    DeclaredFile string
    Category     SectionCategory  // NEW — required, not optional-with-a-default (a spec with no category is a construction error, same discipline as DeclaredFile+Text exclusivity already enforced in AssembleContext)
}
```
Every existing caller that builds a `[]SectionSpec` (`VERIFY: grep -rn 'SectionSpec{' internal/runtime
cmd/aeon` for the full call-site list) gains the field — this is this slice's one call-site-fan-out edit,
named so the task that does it checks every site rather than letting the compiler catch only some
(a struct literal with unkeyed fields would silently compile wrong; `VERIFY` none exist via `go vet`'s
composite literal check or a grep for unkeyed `SectionSpec{` literals).

**Tool schema** is not a transcript category — it is a per-call request-shape fact (how many of this turn's
tokens were the tool definitions sent alongside the prompt). This slice does not attempt to recover it
retroactively from token counts (that would be an estimate pretending to be measured, the exact anti-pattern
D1 exists to kill for usage). If a provider's own usage payload ever separates it, capture it as another
nullable `step_turn` column then; until it does, it is **absent**, not zero, and the composition breakdown
(D4) reports system_prompt/injected_context/assistant_output/tool_result only, four categories, not five —
amending the brief's §3 list by one, here, rather than shipping a column nothing can ever fill.

**Assistant output / tool result** are tagged at the existing message-append points inside `RunLoop`'s turn
loop (the same loop TL5's capture sits in) by the message's own already-distinct role/kind at the moment
it's appended — no new classification logic, just recording what the code already knows structurally.

## TL7 — Compaction becomes rows (D3)

```sql
CREATE TABLE step_compaction (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id          INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  start_idx        INTEGER NOT NULL,
  end_idx          INTEGER NOT NULL,
  estimate_before  INTEGER NOT NULL,
  estimate_after   INTEGER NOT NULL,
  summary_len      INTEGER NOT NULL,
  input_tokens     INTEGER, output_tokens INTEGER,   -- nullable, same M9 discipline — this is a REAL model call
  cost_usd         REAL,
  cost_state       TEXT NOT NULL DEFAULT 'unknown' CHECK (cost_state IN ('known','partial','unknown')),
  occurred_at      TEXT NOT NULL
);
```
Populated from the existing `CompactionRecord` (`internal/runtime/compact.go:111-119`, confirmed this
session) at the same point it is appended to `Checkpoint.Compactions` — additive, the blob stays (nothing
reads it differently), this is a second, queryable home for the same fact. **Sideways seam**: per brief §5,
this slice is the first caller of `AppendEvent` for a step-level kind — correction: `AppendEvent` already
has **two** call sites today (`cmd/aeon/store_verbs.go:927`, `internal/daemon/memory.go:545`, confirmed this
session, both unrelated to step events), so this slice is accurately the **third** caller and the **first**
for a step-level compaction/inject/prune kind — the brief's "second call site" undercounts by one; the
substance (durable event rows for step-level events did not exist before this slice) is unchanged.

## TL8 — File and command activity (D5/D6)

```sql
CREATE TABLE step_file_activity (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  path TEXT NOT NULL, action TEXT NOT NULL CHECK (action IN ('read','write')),
  lines_added INTEGER, lines_removed INTEGER,   -- nullable: a read has neither; a write may not know (binary file)
  occurred_at TEXT NOT NULL
);
CREATE TABLE step_command_activity (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  command TEXT NOT NULL, exit_code INTEGER, wall_ms INTEGER NOT NULL,
  occurred_at TEXT NOT NULL
);
```
Rolled into component/project/workspace tiers as **counts and totals only** (brief §3's explicit limit) —
`files_touched`, `files_read`, `files_written`, `lines_added`, `lines_written`, `commands_run`,
`commands_failed`, `command_wall_ms_total`. No per-path, no per-command detail survives past the loop tier —
that would make the rollup tables grow with the SAME shape as the detail they're supposed to outlive.

## TL9 — The rollup tables, and P32 carried through

```sql
CREATE TABLE component_telemetry (
  component_id TEXT PRIMARY KEY REFERENCES component (id) ON DELETE CASCADE,
  loops_run INTEGER NOT NULL DEFAULT 0,
  wall_ms_total INTEGER NOT NULL DEFAULT 0,
  input_tokens_total INTEGER NOT NULL DEFAULT 0, output_tokens_total INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens_total INTEGER NOT NULL DEFAULT 0, cache_write_tokens_total INTEGER NOT NULL DEFAULT 0,
  reasoning_tokens_total INTEGER NOT NULL DEFAULT 0,
  cost_usd_total REAL NOT NULL DEFAULT 0, cost_state TEXT NOT NULL DEFAULT 'known' CHECK (cost_state IN ('known','partial','unknown')),
  files_touched_total INTEGER NOT NULL DEFAULT 0, files_read_total INTEGER NOT NULL DEFAULT 0, files_written_total INTEGER NOT NULL DEFAULT 0,
  lines_added_total INTEGER NOT NULL DEFAULT 0, lines_removed_total INTEGER NOT NULL DEFAULT 0,
  commands_run_total INTEGER NOT NULL DEFAULT 0, commands_failed_total INTEGER NOT NULL DEFAULT 0, command_wall_ms_total INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);
-- project_telemetry: identical columns, PK project_id REFERENCES project(id) ON DELETE CASCADE
-- workspace_telemetry: identical columns minus the PK (or PK fixed at a single literal id — pick one convention and apply it to this one singleton row only)
```

**`cost_state` at a rollup is `known` only if every contributing loop's cost was `known`; one `partial` or
`unknown` contributor floors the whole tier to that weaker state** — the exact discipline `S25-history`
uses structurally, applied here to cost (D9). The same three-state rule applies to `wall_ms_total`: a
crashed/recovered step whose `wall_ms` is itself unknown (S7's Case B/C, a reissued call whose original
cost went unmeasured) must not silently contribute 0 to the total — carry a `wall_ms_state` column beside
it if any such case exists in practice (`VERIFY` whether `step.wall_ms` can itself be NULL for a settled
step before deciding this needs its own three-state column or can trust NOT NULL throughout).

## TL10 — Socket methods are the primary surface, and agents are a named consumer

Per the brief's own instruction and this session's direction: **`telemetry.*` methods are not a UI
afterthought — a future agent asking "how big is this component, by cost/tokens/time" must be able to call
one of these the same way it calls any other read.** Four reads, one per tier, same socket, same
peer-credential gate as `policy.*`/S34's reads:

| Method | Answers |
| --- | --- |
| `telemetry.loop {id}` | the loop tier's live detail: per-turn rows (paged), compactions, file/command activity, current vs. cumulative context size (D2) |
| `telemetry.component {id}` | `component_telemetry`'s row, verbatim |
| `telemetry.project {id}` | `project_telemetry`'s row, verbatim |
| `telemetry.workspace {}` | the one `workspace_telemetry` row |

Every response carries `cost_state`/`wall_ms_state` honestly (TL9) — a caller (human or agent) asking for a
size estimate must be told when the number is a floor, not a fact, exactly as every other cost-bearing read
in this project already does. No new authorization concept: these are reads, public like `policy.*`, no
`Debug` marking, no narrow-surface exclusion beyond what any other read gets.

## TL11 — D2, concretely: two numbers, never collapsed

`telemetry.loop`'s response carries `currentContextTokens` (the live, current size — read from the step's
in-flight manifest/compaction state, not the sum) and `cumulativeTokensTotal` (the running total across
every turn so far, **the same number `step_turn`'s rows sum to**) as two distinct fields, always both
present, never one standing in for the other.

## Test map

| # | Test | Proves |
| --- | --- | --- |
| 1 | a scripted multi-turn step → `step_turn` has one row per model call, usage columns nullable exactly where the fixture provider omitted them | D1, M9 discipline |
| 2 | the same step → component/project/workspace rollups bump in the same transaction as the `step_turn` insert (spy on transaction boundaries, or a crash-mid-write test that proves atomicity) | TL2, D7 |
| 3 | archive the loop → component/project/workspace sums **unchanged** by the archive itself (D8, direct store-method call per the brief's own note that nothing calls `ArchiveLoop` yet) | D8 |
| 4 | move a component between two projects with existing telemetry → old project's sums decrease by exactly the moved component's totals, new project's increase by the same, workspace unchanged | TL3 |
| 5 | one `partial`-cost loop among several `known` ones → the component tier reads `partial`, never `known` | D9 |
| 6 | a compaction → `step_compaction` row exists, matches the existing `Checkpoint.Compactions` blob's values, and a durable `event` row exists for it (the third `AppendEvent` call site) | D3, TL7 |
| 7 | `telemetry.workspace {}` against an empty store → the legitimate zero-loops-ever state, not an error | three-state honesty at the top tier |
| 8 | every `SectionSpec{}` call site compiles with a required `Category` and none defaults silently | TL6 |
| 9 | `telemetry.loop` on a live, in-flight loop → `currentContextTokens` ≠ `cumulativeTokensTotal` on a loop that has compacted at least once, both present | TL11/D2 |

## TL12 — The capture seam: a narrow interface, nil-safe, matching `Steers`/`Policy`'s own precedent

**Answered, not left open**: `internal/runtime` never imports `internal/store` (confirmed this session —
`ReceiptWriter`, `CheckpointWriter`, `EventSink`, `SteerSource`, `PolicyGate` are all package-local
interfaces on `LoopDeps`/`RunOpts`, `internal/runtime/loop.go:145-165`, `runstep.go:41-61`). TL5/TL6/TL7's
capture call needs the same treatment — a new `TelemetrySink` interface on both `LoopDeps` and `RunOpts`
(every current caller constructs one of these; TL5's write happens inside `RunLoop`, reachable from both):

```go
// internal/runtime — a new, narrow interface, same shape discipline as SteerSource
type TelemetrySink interface {
    RecordTurn(ctx context.Context, stepID int64, iteration int, usage model.Usage, cost model.Cost, wallMs int64, category SectionCategory) error
    RecordCompaction(ctx context.Context, stepID int64, rec CompactionRecord) error
    RecordFileActivity(ctx context.Context, stepID int64, path, action string, linesAdded, linesRemoved *int) error
    RecordCommandActivity(ctx context.Context, stepID int64, command string, exitCode *int, wallMs int64) error
}
```
`Telemetry TelemetrySink` added to `LoopDeps` and `RunOpts`; **nil means the feature is off** — the exact
I2 discipline `Steers`/`Policy` already state verbatim ("every pre-existing caller runs exactly what it ran
before, and the injection rules can never surprise a caller that never asked for them"). The concrete
implementation (a thin adapter over `*store.Store`) lives in `internal/daemon` (which already imports
`internal/store` freely) and is wired at every site that builds a `LoopDeps`/`RunOpts` today — `VERIFY` the
full call-site list (`grep -rn 'LoopDeps{\|RunOpts{' internal/daemon internal/protocol`) before wiring, the
same discipline T1-of-S36 applied to its own composite-caller call sites.

## What design.md deliberately leaves to `plan.md`

The exact migration number (next free, measured at seeding — the ledger was at 39 as of this session and
will have moved again), and the exact file each new table's Go accessor and the `TelemetrySink` production
adapter live in.

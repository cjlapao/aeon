# Plan — S33 Telemetry

## What is settled before planning

Everything in `brief.md` (the locked three-tier shape, now four levels per `design.md` TL1; D1–D10) and
`design.md`'s TL1–TL13. In particular: the tier chain is Loop → Component → Project → Workspace (TL1, the
S38 correction); one write bumps all three rollups in the same transaction (TL2); a component move
transfers its current rollup, never recomputes from archived history (TL3); time is summed at every tier
exactly like tokens and cost, never derived after the fact (TL4); the capture seam is a new, nil-safe
`TelemetrySink` interface on `LoopDeps`/`RunOpts`, matching `SteerSource`/`PolicyGate`'s own precedent
(TL12); `telemetry.*` socket methods are the primary surface, agent-consumable, not a UI-only convenience
(TL10).

## How we will do it

Schema first (nothing else can be tested without it), then the seam (nil-safe, so every existing test keeps
passing the moment it exists and before anything calls it), then each capture point lands as its own task
because each touches a different file with a different risk (R1 names `loop.go` specifically), then the
rollup chain (which needs real rows to roll up), then the socket reads, then close-out. **File-and-command
activity (D5/D6) reuses `tools.Result`'s existing `Path`/`LinesAdded`/`LinesRemoved` fields (added for S31
D8) for the file half outright — no new plumbing there — and needs one small, S31-shaped addition
(`ExitCode`/`WallMs` fields on the same struct) for the command half.** This halves what looked like two
new capture mechanisms down to one small struct amendment plus a read of fields that already exist.

## Open questions this plan answers

- **Q1** (does `wall_ms` ever go NULL on a settled, non-crashed step?) — T1's own schema-design step: read
  every writer of `step.wall_ms` before deciding whether `component_telemetry.wall_ms_total` needs a
  three-state companion column or can trust `NOT NULL` throughout. Recorded in Notes with the writers found.
- **Q2** (the full call-site list for `LoopDeps{`/`RunOpts{` construction) — T2's first step, before wiring
  anything; every site gets `Telemetry: <adapter or nil>` explicitly, not left to a zero value by omission
  (a zero-value nil is correct here, but an *omitted* field in a struct literal that later gains a required
  field is a silent bug waiting for `go vet`'s unkeyed-fields check to not catch it either).
- **Q3** (is there an existing informal timer around the model-call section in `RunLoop` that T3 can reuse
  for per-turn wall time, or does it need its own)? — T3's first step.

## Task sequence

### T1 — The schema ([`tasks/T1.md`](tasks/T1.md))

Migration (next free number, measured at seeding) adding `step_turn`, `step_compaction`,
`step_file_activity`, `step_command_activity`, `component_telemetry`, `project_telemetry`,
`workspace_telemetry` exactly per `design.md` TL5/TL7/TL8/TL9. Store accessors: `RecordStepTurn`,
`RecordStepCompaction`, `RecordFileActivity`, `RecordCommandActivity` (each a single transaction that also
bumps the three rollup rows per TL2 — the rollup bump lives IN these functions, not a separate pass),
`ComponentTelemetry(id)`, `ProjectTelemetry(id)`, `WorkspaceTelemetry()`. Answers Q1. **Depends: nothing
(first).**

### T2 — The `TelemetrySink` seam ([`tasks/T2.md`](tasks/T2.md))

`internal/runtime` gains the `TelemetrySink` interface (TL12) on `LoopDeps` and `RunOpts`; a nil value is a
no-op at every call site inside the four capture-point functions (T3/T5/T6 call `d.Telemetry.RecordX` only
after a nil check, matching `Steers`'s own guard shape). The production adapter (`internal/daemon`, a thin
wrapper over T1's store methods) is wired at every real construction site — answers Q2, recorded in Notes.
**This task adds no capture logic** — every `RecordX` call T3/T5/T6 add is to a seam that, with no
production wiring yet, silently does nothing; the point is that nothing breaks when this task lands alone.
**Depends: T1** (the interface's method signatures need T1's real store function shapes to mirror).

### T3 — Per-turn capture (D1, D2) ([`tasks/T3.md`](tasks/T3.md))

Answers Q3. The capture call lands beside `internal/runtime/loop.go:550`'s existing `addUsage` line, inside
`RunLoop` — same function as S31 T4's steer drain, different lines (R1, named not just flagged). Carries
iteration, `result.Usage`, `result.Cost`, measured wall time, and the turn's category (assistant output —
T3 only tags the OUTPUT side; T4 tags the input/context side). D2's two-numbers rule: a small accessor
(`CurrentContextTokens`/`CumulativeTokensTotal`) added wherever `telemetry.loop` (T8) will read from.
**Depends: T2.**

### T4 — Category tagging (D4) ([`tasks/T4.md`](tasks/T4.md))

`SectionSpec` gains `Category` (TL6); every call site (`VERIFY` the full list) gains it, keyed, no silent
default. The opening-context sections tag as `system_prompt`/`injected_context` at `AssembleContext`'s own
call; `RunLoop`'s existing message-append points tag `tool_result` the same way T3 tags assistant output —
this task and T3 together close D4's classification for all four surviving categories (system_prompt,
injected_context, assistant_output, tool_result — the brief's fifth, tool_schema, is dropped per TL6's
finding). **Depends: T2, T3** (shares the capture call's plumbing).

### T5 — Compaction rows (D3) ([`tasks/T5.md`](tasks/T5.md))

`RecordStepCompaction` called at the exact point `CompactionRecord` is appended to `Checkpoint.Compactions`
(`internal/runtime/compact.go`); a durable `event` row via `AppendEvent` for the compaction (the third call
site in the codebase, confirmed in `design.md` TL7 — correcting the brief's "second" by one). **Depends:
T2.**

### T6 — File and command activity (D5, D6) ([`tasks/T6.md`](tasks/T6.md))

**File half**: at the loop's one tool-dispatch choke point (`internal/runtime/loop.go:739`'s `res :=
d.Tools.Call(ctx, use.Name, use.Input)`, confirmed this session), when `res.Path != ""` call
`RecordFileActivity` with the existing `res.Path`/`res.LinesAdded`/`res.LinesRemoved` fields (added for S31
D8 — **zero new fields needed for this half**). **Command half**: `tools.Result` gains `ExitCode *int` and
`WallMs int64` (same S31-precedent pattern: new fields, old fields untouched, N5's ten-tools-frozen
behavior preserved), populated by `internal/tools/bash.go`'s existing `RunResult` (`ExitCode`, `Duration`)
at the point it builds the final `Result`; the choke point calls `RecordCommandActivity` when `use.Name ==
"bash"` (or more generally, when the new fields are non-zero — decide and record which check is more
honest: a bash call that legitimately exits 0 must still be recorded, so "non-zero fields" is the WRONG
test; gate on the tool name, matching how `ToolClass` already type-switches by name nearby). **Depends:
T2.**

### T7 — The rollup chain and the component-move transfer ([`tasks/T7.md`](tasks/T7.md))

This is mostly already inside T1's `RecordX` functions (TL2's bump-in-the-same-transaction rule) — this
task is where that claim gets its own direct test (test map #2/#3) and where **TL3's component-move
transfer** lands: `internal/store/components.go`'s `UpdateComponent`, in the same transaction as its
existing `project_id` change, reads the moving component's current `component_telemetry` row and
transfers it (subtract from old project, add to new) — one additional statement pair, not a new function.
P32/D9's three-state floor logic (a `partial`/`unknown` contributor floors the tier) lives here too, as the
rule every `RecordX`/transfer statement obeys when updating `cost_state`. **Depends: T1, T3, T5, T6** (needs
real capture functions to roll up).

### T8 — The socket reads ([`tasks/T8.md`](tasks/T8.md))

`telemetry.loop {id}` / `telemetry.component {id}` / `telemetry.project {id}` / `telemetry.workspace {}`
(TL10), registered in `protocol/method.go` after S34–S36's blocks, handlers in `protocol/telemetry.go`,
each through a narrow store-reading interface (the same `ProposalStore`-style local-interface pattern
S35 T3 used — `internal/store` is not banned from `internal/protocol`, confirmed earlier this session, so a
small `TelemetryStore` interface plus a `internal/daemon` adapter is the right shape, not a func-seam).
Conformance fixtures ×4. D2's two-numbers rule (TL11) lands in `telemetry.loop`'s response shape here, using
T3's accessor. **Depends: T1, T7** (reads need real rolled-up data to answer against).

### T9 — Close-out ([`tasks/T9.md`](tasks/T9.md))

D8's archive-test (archiving a loop changes zero rollup rows — direct store-method call, same `ArchiveLoop`
gap S25 already named); the measured write-volume check (a long scripted loop, real row counts, confirming
or correcting T-plan's "no batching" call); P32/D9 honesty proven at all three rollup tiers in one test;
measured numbers into Notes; `STATUS.md`/`ROADMAP.md` rows flipped to built, and — since this slice did not
exist as a planned slice when `STATUS.md`'s at-a-glance board was last rebuilt — its row added to Stage 5's
table for the first time, not just flipped. **Depends: T1–T8.**

## Sequencing summary

```
T1 ─► T2 ─┬► T3 ─► T4 ─┐
          ├► T5 ───────┼► T7 ─► T8 ─► T9
          └► T6 ───────┘
```

T3/T5/T6 are independent of each other once T2's seam exists (each touches a different file: `loop.go`'s
usage line, `compact.go`, `loop.go`'s tool-dispatch line — close together but distinct edits, mergeable
without fighting). T4 depends on T3 only because it shares the same capture-call plumbing, not because the
category work is otherwise coupled to per-turn usage.

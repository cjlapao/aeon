# Brief — S33 Telemetry

> **Amended — `design.md`, `plan.md` and task files now exist.** §3/§5's "three tiers" below is
> **pre-S38 vocabulary**: S38 renamed this brief's "project" to `component` and added a new `project`
> (a group of components) above it, so the real rollup chain is **Loop → Component → Project →
> Workspace** — one tier deeper than this brief locked. `design.md` TL1 carries the full correction;
> read it, not this section, for the actual shape. Everything else below (D1–D10, the seams, §6's locked
> decisions) still holds with that one substitution applied throughout.

**Implements:** `design.md` — now written; this brief locked the architecture (the three tiers, §3/§5, since
corrected per the note above) and named what had to be measured before a schema could be written (§7, now
answered in `design.md`). **Stage:** 5 · Fleet — new this session,
not a promotion of a sketch; split out of the conversation that fleshed out S25 specifically so S25 could
stay a pure read (`S25-history/brief.md`'s own N1). **Depends on:** S4 (the store). **Touches the live step
runtime directly** — `internal/runtime/{compact,runstep,loop}.go`, the model adapters — which no other Fleet
slice (S24, S25, S30) does; see §8 R1. **Repo:** Aeon lives at `aeon/` in this repository (P24) · Go 1.26 ·
`cgo` enabled (B3)
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`): §0 already states the read boundary correctly
("read only through the same socket/CLI boundary every other table uses"). No code exists yet to audit, but
whoever writes `design.md` should treat `telemetry.*` socket methods as the primary read surface, not an
afterthought — D10 names S31's `Telemetry` tab as the first concrete consumer, and that tab reads through
the socket the way every other test-bench page does (`S31-loop-workspace/brief.md`'s own compliance note).

---

## 0. Before anything else: this is not what S23 forbids

**Say this first because the name invites exactly the wrong reading.** `S23-first-run/brief.md`'s **D13/N1**:
*"No telemetry, ever (C11, C19). Not opt-in, not anonymised, not for crash reports. Permanent."* — asserted
by a network trace over a full loop, with tools and a forge action, proving nothing phones home.

**That rule is about data leaving the machine.** This slice is about data that never does. Every row this
slice writes lives in the same local SQLite store every other table in this project lives in, read only
through the same socket/CLI boundary every other table uses, by the same project that generated it. **If
this slice's existence would make S23's network-trace test fail, this slice is wrong, not the test** — that
test is the standing arbiter, named here so nobody has to re-derive the boundary case by case later. The name
survives anyway, rather than inventing a different one, because `S31-loop-workspace`'s own app header
**already reserves a nav tab called `Telemetry`**, drawn disabled with the note *"Coming next"* — this slice
is what turns it on, so the name is already load-bearing elsewhere in the project, not a fresh coinage.

## 1. What this is

**Everything Aeon can know about what a loop actually cost to run, captured as it happens, kept at three
durable scopes.** Not S25's cross-loop *structural* projection (phases, tasks, steps, handoffs) — this is
the *resource* picture underneath it: tokens, their composition, timing, file and command activity, at a
granularity finer than anything currently persisted.

**The gap this fills, named in the project's own words already**: `STATUS.md` on S31's build: *"Not built,
because the daemon does not provide the data (no invented figures): tokens/sec and token counts in the
footprint (a `usage` field on `step.done` would enable them)."* **That sentence is this slice's mandate.**

## 2. What exists today, measured — the gap is real, not assumed

- **Tool calls are recoverable from `step_message`** (name, args — nothing new needed there).
- **Compaction already has a record shape** (`internal/runtime/compact.go`'s `CompactionRecord`:
  before/after token *estimates*, summary, cost) — but it lives inside one step's own JSON blob
  (`Checkpoint.Compactions` → `protocol.StepSnapshot`), not as queryable rows, and it is an estimate (4
  bytes/token heuristic), never a provider-reported count.
- **Per-turn token usage is thrown away.** `LoopState.Usage` sums every iteration's `model.Usage` into one
  running total for the whole step (`addUsage`, called at `loop.go:534` and `compact.go:171`) — the
  per-iteration delta is computed, used once, and discarded.
- **Compaction/inject/prune events are live-only.** `StepEvent`s go out through `d.publish(...)` —
  `localrpc.Notify` to whatever is connected right now — and are **never written to the durable `event`
  table** (`AppendEvent` has exactly one call site in the whole codebase, unrelated to step events). A
  client not watching at the moment of emission never sees it, and it is gone once the step ends.
- **No category tagging exists anywhere** — nothing marks a message or block as "system prompt" vs.
  "injected context" vs. "tool schema." The composition breakdown this slice's §3 asks for does not exist
  in any form today, estimated or otherwise.
- **File and command activity have no aggregate anywhere.** `ContextFile` (DB10) records files *read*, with
  a hash, nothing about writes, line deltas, or command exit codes/timing.

## 3. The three tiers (locked shape; design.md fills in columns)

**Loop tier — everything, at all times, while the loop is live.** The full-fidelity record: per-turn token
usage (not summed away), the live/current-context size alongside the running total, every compaction with
its real before/after, file writes with line deltas, command executions with timing and exit status. This
tier is allowed to be large and allowed to disappear when its loop is archived (S4's `ArchiveLoop`) — it is
the working record, not the durable one.

**Project tier — sums, updated incrementally from the loop tier, survives archival.** Not a query over the
loop tier computed on demand (`ArchiveLoop` deletes the loop tier's rows by cascade — a demand query would
silently lose everything an archived loop contributed, the exact failure mode `S25-history/design.md` §2
found and guarded against for structural history; this slice must not reintroduce it for cost history).
**Every write to the loop tier also updates the project tier's running sums in the same operation** — total
tokens, total cost (P32-floored, same discipline S25 uses), tool-call counts by name, file/command activity
sums — so that by the time a loop is archived, the project tier is already correct and archival needs zero
telemetry-specific logic.

**App tier — sums across every project, updated incrementally from the project tier, survives everything
(including a project itself being removed).** One row, or a small table, following the identical
incremental-update discipline the project tier uses.

**Consequence of "incremental, not computed-on-demand":** this slice's rollup tables are explicitly a second
place a number lives, which `S25-history/brief.md`'s "not a second source of truth" rule forbids *for
structural history*. **This slice is the deliberate exception** — the loop tier is authoritative for what
happened in detail; the project/app tiers are authoritative for *sums that must outlive the detail*, by
construction, because the detail is designed to go away. The two are allowed to describe the same event
because only one of them still exists after an archive.

## 4. Definition of Done

| | |
| --- | --- |
| **D1** | **Per-turn token usage is preserved, not summed away.** Every model call's `Usage` is written as its own row before it is folded into any running total |
| **D2** | **The current/live context size and the loop's cumulative total are both visible at once** — "how big is the context right now" and "how much has this loop spent in total" are different numbers and this slice never collapses them into one |
| **D3** | **Compaction becomes queryable rows**, not a blob inside one step's checkpoint — before/after size, summary length, cost, timestamped, joinable across a project's whole history |
| **D4** | **Category composition is a new, explicit classification** at the point a message/block is written — system prompt, injected context, tool schema, assistant output, tool result — named here as new work, never implied to already exist |
| **D5** | **File activity is aggregated**: files touched, read vs. written, line deltas — at the loop tier in full, rolled into the project/app tiers as counts and totals only |
| **D6** | **Command execution activity is aggregated**: per-command counts, failures, timing — same shape as D5 |
| **D7** | **The project and app tiers are updated incrementally, in the same write path that updates the loop tier** — never a batch job, never computed at archive time, never computed on demand from the loop tier (§3) |
| **D8** | **Archiving a loop (`ArchiveLoop`, S4) requires zero telemetry-specific code.** The project/app tiers are already correct before archival runs, by D7's construction — proven by a test that archives a loop and asserts the project tier's sums are unchanged by the archive itself. (Note: nothing in the CLI/daemon calls `ArchiveLoop` yet — `S25-history/brief.md`'s own N6/Q4 found the same gap — so this test, like S25's, calls the store method directly) |
| **D9** | **Every aggregate still carries P32's three-state honesty** — a project's "total tokens" when one loop's accounting is `partial` floors and says so, the same discipline `S25-history` uses structurally, applied here to cost |
| **D10** | **This produces exactly the `usage` field `step.done` is missing** (§1's quoted mandate) — S31's disabled `Telemetry` tab is this slice's first concrete consumer, named so "done" has a checkable target beyond its own tests |

## 5. The seams

**Downward — the live step runtime.** This is the one Fleet slice that edits `internal/runtime/{compact,
runstep,loop}.go` and the model adapters directly, rather than adding a disjoint package. Every edit adds a
call to this slice's own capture functions; none changes existing control flow or return shapes (R1 names
the collision risk this creates).

**Sideways — `event`.** Compaction/inject/prune events gain a real, durable path (D3) — this slice is the
first thing to call `AppendEvent` for step-level events, the second call site in the whole codebase.

**Upward — S31's `Telemetry` tab, S9's `LOOP FOOTPRINT`.** Both are waiting, named, for exactly this data
(§1, §4 D10). This slice does not build either UI; it makes them buildable.

**Explicitly not a seam — S25 History.** Read `S25-history/brief.md` §4's "explicitly not a seam" line:
the two slices read the same store, never each other's output. S25 shows *what happened, structurally*;
this slice shows *what it cost, at a resolution S25 deliberately does not carry*.

## 6. Locked decisions

- **Incremental rollup, not computed-on-demand** (§3). This is the single decision the rest of the slice's
  shape follows from, and it is why D7/D8 exist as their own DoD items rather than being implied by D1–D6.
- **The name stays `Telemetry`**, disambiguated explicitly (§0) rather than renamed — matching S31's
  existing nav label rather than inventing a second name for the same thing.
- **Category tagging (D4) is new work, asserted as such.** Nothing about this slice claims the composition
  breakdown is a read over existing data the way S25's drill-down is; it requires marking content at write
  time, which touches the message-construction path directly.
- **Per-turn usage rows (D1) are additive, never a replacement for `LoopState.Usage`'s running total.** The
  sum stays; this slice adds the detail underneath it, so nothing that reads the existing total needs to
  change.

## 7. What has to be measured before `design.md` can be written

- **Write volume at the loop tier.** Per-turn rows, per-compaction rows, per-file-touch rows, per-command
  rows — on a long loop (the kind S25's own `DEMO.md` worries about, "months of loops") this could be a lot
  of writes. Whether this needs its own pool/write-batching consideration (the way DB15's two-pool question
  is still open for S4 generally) is a measurement, not a guess, before schema is locked.
- **Where category tagging (D4) actually hooks in.** The candidate point is wherever `model.Message`/`Block`
  is assembled before a call — naming the exact function(s) is `design.md`'s job, grounded by reading
  `internal/runtime/context.go` and the model adapters directly, not assumed from this brief.
- **Whether `model.Usage` is reliably present per call across every provider adapter**, or whether some
  return nil/partial usage — D1's "preserved, not summed away" is only as good as what providers actually
  report; `internal/model/types.go`'s own `Usage` comment ("absent is not zero," M9) suggests this is
  already a known three-state concern elsewhere in the codebase worth reading before locking D1's row shape.

## 8. Risks

- **R1 — This slice edits files other slices also need, and the collision is now confirmed, not
  hypothetical.** Unlike S24/S25/S30 (new packages, additive only), this one's D1/D3 touch
  `internal/runtime/{compact,runstep,loop}.go` directly. **`S31-loop-workspace` is built (2026-10-26
  check) and its T4 already edits `internal/runtime/loop.go`'s `RunLoop`** — steer injection, drained
  before every model call and after each assistant turn (`S31-loop-workspace/brief.md` §6 T4). Whoever
  builds this slice's D1 (per-turn usage capture) is editing the same function T4 already changed, not a
  hypothetical future edit — read T4's actual diff first, and land per-turn capture as a call at the same
  seam T4 already drains steers from, rather than a second insertion point in the same loop.
- **R2 — Incremental rollup drifts from the detail it summarizes.** The guard is D8's own test: archiving a
  loop must not change the project tier's sums, which only holds if every loop-tier write genuinely also
  updates the project tier in the same operation, never "eventually," never "on the next read."
- **R3 — "Everything we can" becomes unbounded scope creep.** The three-tier shape (§3) and the ten DoD
  items (§4) are the boundary: if a future ask does not fit one of D1–D10, it is a new slice's problem, not
  a silent widening of this one.

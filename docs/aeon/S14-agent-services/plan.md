# Plan — S14 Agent services

> **Source material:** [`design.md`](design.md) · the roadmap's [§5](../ROADMAP.md) · A1 and A13 in
> [`../S13-agents/design.md`](../S13-agents/design.md) · S6's suspension, which AS4 reuses wholesale.

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Stage:** 3 · Car, fifth of eight. Depends on **S10**, **S12**, **S13**. **Blocks S15, S21.**

---

## What is settled before planning

**Async is a suspension** (AS4). S6 built suspend-by-exiting and S7 built cold resume, so **there is no
concurrency work in this plan** — no worker pool, no queue, no change to S5's serial execution. That is the
single largest thing this plan does not contain.

**Authorization is a policy.** S10's `SkillAccess` exists; A2A's `securitySchemes` is expressed through it,
not beside it (AS8). No ACL here.

**The 25 bodies do not change.** S13's `Handler` gains an **optional** card and skills (AS1).

**A1 is amended, not reversed.** Nothing spawns. The chain is recorded.

## The prerequisite that is not a question

**S8's step must carry `skill:` and `params:`.** A lane cannot target an agent's skill until a step can
express one — so **that amendment is a prerequisite for T4**, not a consequence of it.

**If it has not landed when T4 starts, T4 is blocked.** Report it; do not invent a free-text convention,
because a convention is a format nobody validated.

> **Decided 2026-10-25 (planning):** measured on the tree, `blueprint.Step` carries no `skill:`/`params:` —
> the amendment was promised and never landed. It is scheduled as **T0, a plan-level prerequisite** (the
> S31 "baseline repairs landed first" precedent — a prerequisite task, not a thirteenth task file), and
> it is validated against A2A's shape from T2, not against a private convention.

## How we will do it

**The optional things first** (T1, T2), because AS1 says they are optional and the cheapest way to keep
that true is to build them as additions to a `Handler` that already works without them.

**Then the Task** (T3), and **it is the slice's keystone.** Get the suspension right before anything
invokes anything: a Task that blocks instead of suspending passes a naive test and quietly destroys P2.

**Then invocation** (T4), **then the two chain properties** (T5 cycle and depth, T6 cost) — in that order,
because a rollup over a chain that can cycle is a rollup that can not terminate.

**Then authorization** (T7), **untrusted results** (T8), **discovery-without-dispatch** (T9), the transport
(T10), deletability (T11), the walk (T12).

**The ordering that matters most:** **T3 before T4.** An invocation built first will acquire whatever
waiting shape was convenient, and the convenient shape is a goroutine.

## Open questions this plan answers

| # | Question | How |
| --- | --- | --- |
| Q1 | The depth bound | T5, a number with reasoning. **Small** — the shape being built is a planner asking a specialist |
| Q2 | Card as file, socket method, or both | T1 |
| Q3 | Does a first-party result need the untrusted label? | T8 |
| Q4 | **Can a skill invocation suspend for a human question?** | T3, and it must be stated either way |

---

## Task sequence

| # | Task | Gate |
| --- | --- | --- |
| **T0** | the S8 amendment — a step carries `skill:` and `params:` (prerequisite, plan-level) | shipped blueprints unaffected; `loop.start` refuses a skill lane when agent services are unconfigured |
| **T1** | `card.go` — the A2A card, signed, optional | A tampered card is refused; an agent with no card still dispatches |
| **T2** | `skill.go` — typed requests | A request missing a field is refused before any model call |
| **T3** | `task.go` — a Task is a row; async is a suspension | The step row is `suspended`; no goroutine exists |
| **T4** | `invoke.go` — agent → agent | A skill invocation completes and is recorded |
| **T5** | `chain.go` — the cycle refusal and the depth bound | A cycle is refused **naming the cycle** |
| **T6** | `cost.go` — rollup along the chain | A parent includes its children; a partial child makes it partial |
| **T7** | `SkillAccess` wiring | Denying the policy makes the skill uninvokable |
| **T8** | `untrusted.go` — third-party skill results | A non-first-party result is labelled |
| **T9** | Discovery without dispatch | No path from a similarity score to an invocation |
| **T10** | `transport.go` — JSON-RPC over the local socket | A skill is invokable over the existing socket |
| **T11** | The level-0 invariant | The suite passes with the layer off |
| **T12** | Walk the demo, write the QA suite | `DEMO.md` runs as written |

### T0 — the S8 amendment: a step carries `skill:` and `params:` (prerequisite)
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~200
- detail: this section (a prerequisite, not a task file — the S31 baseline-repairs precedent)
- success_criteria:
  - `Step` gains `skill:` and `params:` in YAML and JSON, and they round-trip
  - validation keeps its three states: a `skill:` target is **checked** against the agentsvc registry when
    one is configured, **unverifiable** with an honest sentence when services are absent — never silently valid
  - `loop.start` preflight **refuses** a lane targeting a skill while agent services are unconfigured
    (the structured refusal T11 promises), naming the lane
  - the pass-through reaches dispatch (protocol `StepParams` → the daemon's step path); a lane with no
    `skill:` behaves byte-identically to before — the shipped blueprints and the S8/S13 suites are unaffected
- files_expected: `aeon/internal/blueprint/types.go`, `aeon/internal/blueprint/validate.go`,
  `aeon/internal/protocol/` (params), plus the pass-through the dispatch needs — **hunk-surgical** where a
  file is in someone else's in-flight set
- **Notes (2026-10-25, measured).** The shape chosen (and why): `skill: "<callee>/<skill-id>"` on a
  **model-step** whose `agent:` is the caller — the DEMO §4 rendering `(reddit-agent/summarise-sentiment)`
  is the target's address, and `params:` is a free-form mapping that **this layer never content-checks**:
  the skill's typed request (T2) validates before any model call, which is exactly the discipline the T2
  gate demands. Three states at validation: `SkillKnown` configured and true → clean; configured and
  false → **invalid naming the target** (the registry is closed — a real check, the D13 argument); not
  configured → **unverifiable with the named remedy**, and `loop.start` refuses the lane
  (`unavailable-skill`, names the lane and quotes the sentence — nothing durable written). Deviations:
  none from the amendment text; the pre-S14 wire stays byte-identical (`Params` is nil, never an empty
  map — asserted). `protocol/step.go` carries the two fields — **that file is in the S31 team's in-flight
  set; the commit staged a HEAD-plus-my-block blob (their 12 in-flight lines verified absent), their
  working copy untouched.** Counts dated 2026-10-25: blueprint **31** PASS, loop **29**, daemon **112**
  (includes the S31 team's in-flight daemon tests — measured, not recounted from memory); race clean on
  blueprint/loop/daemon/protocol; `tools/protogen` no-diff (contract.ts unchanged — StepParams is not
  embedded there); vet + build clean across the six targeted packages.

### T1 — `card.go`: the A2A card, signed and optional
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~280
- detail: tasks/T1.md
- success_criteria:
  - the A2A card shape, not an invented one
  - unit test `a valid signature verifies` passes
  - unit test `a tampered card is refused, not warned about` passes
  - unit test `an agent with no card still dispatches` passes (AS1)
  - Q2 answered: file, socket method, or both, with the reason
- files_expected: `aeon/internal/agentsvc/card.go`, `aeon/internal/agentsvc/card_test.go`, `aeon/internal/agentsvc/testdata/cards/**`

### T2 — `skill.go`: typed requests
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~240
- detail: tasks/T2.md
- success_criteria:
  - A2A's field set: `id`, `name`, `description`, `tags`, `examples`, `inputModes`, `outputModes`
  - unit test `a request missing a required field is refused before any model call` passes, with a `Client` spy at zero
  - unit test `an unsupported inputMode is refused with the supported set named` passes
  - unit test `an agent with no skills still dispatches` passes
- files_expected: `aeon/internal/agentsvc/skill.go`, `aeon/internal/agentsvc/skill_test.go`

### T3 — `task.go`: a Task is a row; async is a suspension
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~320
- detail: tasks/T3.md
- success_criteria:
  - **unit test `an async Task suspends the step` passes** — the step row is `suspended`
  - **unit test `no goroutine is created for an async Task` passes**
  - unit test `a completion resumes the loop cold` passes, with a daemon restart in between
  - unit test `a sync Task never becomes async` passes — no promotion on timeout
  - **Q4 answered and stated**: whether a caller's step suspends when the callee asks a human
- files_expected: `aeon/internal/agentsvc/task.go`, `aeon/internal/agentsvc/task_test.go`, `aeon/internal/store/schema.go`

### T4 — `invoke.go`: agent → agent
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3]
- est_changed_lines: ~280
- detail: tasks/T4.md
- success_criteria:
  - **S8's `skill:`/`params:` amendment is present, or this task reports itself blocked**
  - unit test `an agent invokes another agent's skill` passes
  - unit test `the invocation is recorded as a chain edge` passes
  - unit test `an unknown skill is refused with a structured reason` passes
- files_expected: `aeon/internal/agentsvc/invoke.go`, `aeon/internal/agentsvc/invoke_test.go`

### T5 — `chain.go`: the cycle refusal and the depth bound
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T4]
- est_changed_lines: ~260
- detail: tasks/T5.md
- success_criteria:
  - **unit test `a cycle is refused and the cycle is named` passes** — A → B → A reports the path
  - unit test `a cycle is not reported as a cost-cap failure` passes
  - unit test `a chain one deeper than the bound is refused at the boundary` passes
  - unit test `the chain is reconstructable after a restart` passes
  - Q1 answered: the depth bound as a number, with reasoning
- files_expected: `aeon/internal/agentsvc/chain.go`, `aeon/internal/agentsvc/chain_test.go`

### T6 — `cost.go`: rollup along the chain
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T5]
- est_changed_lines: ~240
- detail: tasks/T6.md
- success_criteria:
  - unit test `a parent's cost includes its children's` passes
  - **unit test `a partial child makes the parent partial` passes** (P32)
  - unit test `an unknown child makes the parent not-known` passes
  - unit test `a rollup over a restarted chain is complete` passes
  - **A1's amendment is recorded in `S13-agents/design.md`**
- files_expected: `aeon/internal/agentsvc/cost.go`, `aeon/internal/agentsvc/cost_test.go`, `docs/aeon/S13-agents/design.md`

### T7 — `SkillAccess` wiring
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T4]
- est_changed_lines: ~200
- detail: tasks/T7.md
- success_criteria:
  - unit test `denying SkillAccess makes the skill uninvokable` passes
  - unit test `there is exactly one authorization mechanism` passes, asserted by grep — no ACL in this package
  - unit test `the refusal carries S10's sentence` passes
- files_expected: `aeon/internal/agentsvc/invoke.go`, `aeon/internal/agentsvc/skillaccess_test.go`

### T8 — `untrusted.go`: third-party skill results
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T4]
- est_changed_lines: ~200
- detail: tasks/T8.md
- success_criteria:
  - unit test `a non-first-party skill result is labelled in the assembled prompt` passes
  - unit test `the label survives a result containing the label text` passes
  - **Q3 answered: where "first-party" is read from**, given S15 makes it a registry property
- files_expected: `aeon/internal/agentsvc/untrusted.go`, `aeon/internal/agentsvc/untrusted_test.go`

### T9 — Discovery without dispatch
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~180
- detail: tasks/T9.md
- success_criteria:
  - cards and skills are enumerable for discovery
  - **unit test `no path exists from a similarity score to an invocation` passes**, asserted by grep
  - unit test `invocation requires a name, never a score` passes
- files_expected: `aeon/internal/agentsvc/discover.go`, `aeon/internal/agentsvc/discover_test.go`

### T10 — `transport.go`: JSON-RPC over the local socket
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T4]
- est_changed_lines: ~220
- detail: tasks/T10.md
- success_criteria:
  - a skill is invokable over the socket Aeon already has (P25) — **no new transport**
  - unit test `no second listener is opened` passes, asserted by grep
  - unit test `the method table is enumerated in one place` passes
- files_expected: `aeon/internal/agentsvc/transport.go`, `aeon/internal/agentsvc/transport_test.go`

### T11 — The level-0 invariant
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T6, T7, T8, T9, T10]
- est_changed_lines: ~160
- detail: tasks/T11.md
- success_criteria:
  - **the whole suite passes with agent services switched off** (AS11)
  - unit test `one of S13's 25 bodies dispatches unchanged` passes
  - unit test `no package imports agentsvc unconditionally` passes
- files_expected: `aeon/internal/agentsvc/absent_test.go`, `aeon/internal/runtime/loop.go`

### T12 — Walk the demo, write the QA suite
- engineer: technical-writer-loop
- tech: docs
- depends_on: [T11]
- est_changed_lines: ~220
- detail: tasks/T12.md
- success_criteria:
  - `DEMO.md` runs end to end, **§1–11**; anything undocumented is added
  - `docs/test_suites/aeon-agents.md` is **amended**, not replaced
  - the suite covers suspension-not-blocking, the cycle refusal, uncertainty in the rollup, and deletability
  - **§11 walked too: the Test bench → Demos tab gains an "Agents · services (S14)" panel** — live steps
    that spend no model call (cards and skills enumerable with signature state; the missing-field refusal
    as data with the zero-call evidence; the cycle refusal naming the cycle; `SkillAccess` denial as data),
    a credential-gated invoke step greyed with the operator command, and **labeled replay** rows citing the
    Go test behind each verdict — the fourth panel of the bench demo runner spec, tests written first
- files_expected: `docs/aeon/S14-agent-services/DEMO.md`, `docs/test_suites/aeon-agents.md`,
  the bench demo-runner panel and its vitest file (hunk-surgical into the shared bench files)

---

## Files

| File | Task | Notes |
| --- | --- | --- |
| `internal/blueprint/types.go` (+ validate, protocol params) | T0 | the S8 amendment, prerequisite |
| `internal/agentsvc/card.go` | T1 | — |
| `internal/agentsvc/skill.go` | T2 | — |
| `internal/agentsvc/task.go` | T3 | **the keystone** |
| `internal/agentsvc/invoke.go` | T4, T7 | T4 first |
| `internal/agentsvc/chain.go` | T5 | — |
| `internal/agentsvc/cost.go` | T6 | — |
| `internal/agentsvc/untrusted.go` | T8 | — |
| `internal/agentsvc/discover.go` | T9 | — |
| `internal/agentsvc/transport.go` | T10 | — |
| `internal/store/schema.go` | T3 | the Task and chain tables |
| `internal/runtime/loop.go` | T11 | the optional seam |
| `docs/aeon/S13-agents/design.md` | T6 | **A1's amendment** |

**`invoke.go` is the only shared file**, and T4 precedes T7.

## Ordering rationale

**T3 before T4, and it is the one to get right.** An invocation built first acquires whatever waiting shape
was convenient, and the convenient shape is a goroutine — which passes a naive test and destroys P2. Build
the suspension first, assert the absence of a goroutine, then let something call it.

**T5 before T6.** A cost rollup over a chain that can cycle is a rollup that can fail to terminate.

**T5's two properties are separate tests on purpose.** Depth catches runaway; a cycle catches a structural
mistake. Conflating them means a cycle is diagnosed as a cost overrun — the right failure for the wrong
reason, which sends somebody to tune a cap instead of fixing a graph.

**T11 late, because deletability needs something to delete.**

## What this slice does not do

No remote agents (S21). No import (S15). No authoring UI (S9 amendment). No concurrency — AS4 means there
is none to add. No scheduling (S20). **No similarity dispatch, ever** (AS9, A13). No new transport (P25).

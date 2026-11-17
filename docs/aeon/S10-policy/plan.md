# Plan — S10 Policy

> **Source material:** [`design.md`](design.md) · the roadmap's [§3](../ROADMAP.md) ·
> G1–G13 in [`docs/_archive/aeon-v1-horizontal/04-permissions`](../../_archive/aeon-v1-horizontal/04-permissions/design.md), which this generalises.

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Stage:** 3 · Car, first of eight. Depends on S1–S9. **Blocks S11–S15.**

---

## What is settled before planning

**This is a promotion, not an addition.** S11's design already describes a pure, total, ordered,
deny-by-default evaluator whose verdict carries the rule that fired. This slice widens it so `bash` command
lines become one *type* among seven, and S11's 366-rule corpus becomes its largest consumer.

**Three things already exist and must not be rebuilt:** S4's `T2` created `policy`, `policy_acceptance` and
`policy_event` with their indexes and `CHECK` constraints — **no migration here**; `step.context_manifest`
is already a column; and S11's `W21` rescope means `ToolAccess` has something to govern beyond a fixed nine.

**`Spec` is data, not a language** (PL2), so there is no interpreter task and no sandbox task. That is the
single largest thing this plan does not contain.

## How we will do it

**The pure core first, and alone.** T1 and T2 are types and the evaluator: no database, no clock, no
environment. Everything after them is either a new spec type, a persistence concern, or an integration —
and all three are easier to review against a core that already holds.

**Then the authorship rule, at the write boundary** (T5). PL1 is the whole security argument and it is
enforced in exactly one place: a write that refuses a non-operator `Allow`. A rule enforced in two places
is a rule with a gap.

**Then the record** (T6, T7), **then the two integrations** (T8, T9), **then the surface** (T10, T11), then
the walk (T12).

**The ordering that matters:** T2 before T3. The evaluator must be right over a trivial type before a real
one exists, because a bug in deny-wins found while debugging `ToolAccess` is a bug attributed to
`ToolAccess`.

## Open questions this plan answers

| # | Question | How |
| --- | --- | --- |
| Q1 | The default blocklist | **Empty**, with `doctor` reporting it. Confirm in T3's notes |
| Q2 | The `policy_event` age cap | A concrete number, chosen and stated in T7 |
| Q3 | Does the evaluator need the store at all | **No** — T2 proves it by having no import of it |

---

## Task sequence

| # | Task | Gate |
| --- | --- | --- |
| **T1** | `types.go` — the closed set, targets, sources | Seven types exist; an unknown type cannot be constructed |
| **T2** | `evaluate.go` — pure, total, deny-wins | Deny beats allow at every level; empty denies; expiry exact at the boundary |
| **T3** | `ToolAccess` — the first real spec | A granted tool is refused by a system deny |
| **T4** | `verdict.go` — the sentence a refusal renders | The sentence names id, type, target, source |
| **T5** | `store.go` — and the authorship rule at the write boundary | A non-operator `Allow` is refused at write time |
| **T6** | `policy_acceptance` — recorded, and not a gate | An unaccepted deny still denies |
| **T7** | `policy_event` — the log, and the age cap | Events past the cap are gone; depth is reportable |
| **T8** | The runtime consults the evaluator before dispatch | A denied tool is proven never to have been called |
| **T9** | Policies into `context_manifest`, and the disagreement record | Replay reproduces the original step's policy set |
| **T10** | `aeon policy` — list, add, remove, why | `why` explains a verdict without changing anything |
| **T11** | `inforce.go` — the three-state answer | All three reachable and distinguishable |
| **T12** | Walk the demo, write the QA suite | `DEMO.md` runs as written |

### T1 — `types.go`: the closed set
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~240
- detail: tasks/T1.md
- success_criteria:
  - `PolicyType` has exactly seven values and an eighth cannot be constructed
  - `Target` covers `all-agents`, `agent`, `loop`, `lane-type`, `mcp`
  - `Source` covers `Operator`, `Agent`, `MCPServer`, `Import`
  - `NotAfter` is `*time.Time`; `nil` is documented as "no expiry", never a sentinel
  - unit test `an unknown policy type is refused` passes
  - unit test `a nil NotAfter is not an expiry` passes
- files_expected: `aeon/internal/policy/types.go`, `aeon/internal/policy/types_test.go`

### T2 — `evaluate.go`: pure, total, deny-wins
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~300
- detail: tasks/T2.md
- success_criteria:
  - `Evaluate(ps []Policy, req Request, at time.Time) Verdict` — no other parameters
  - unit test `deny beats allow regardless of target specificity` passes, both orderings
  - unit test `an empty policy set denies` passes, for every type
  - unit test `expiry is exact at the boundary` passes at `NotAfter-1ns`, `NotAfter`, `NotAfter+1ns`
  - unit test `an unimplemented type refuses rather than permits` passes
  - **`internal/policy` has no import of `store.go`'s database, no `time.Now`, no `os`** — asserted by grep
- files_expected: `aeon/internal/policy/evaluate.go`, `aeon/internal/policy/evaluate_test.go`

### T3 — `ToolAccess`: the first real spec
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~220
- detail: tasks/T3.md
- success_criteria:
  - `{toolName, state}` validated on write; an unknown field is refused
  - unit test `a tool granted to an agent is refused by an all-agents deny` passes
  - unit test `the default blocklist is empty` passes, and Q1 is confirmed in the notes
  - the six other types have their schema and a **refusing** evaluator stub
- files_expected: `aeon/internal/policy/spec_toolaccess.go`, `aeon/internal/policy/spec_toolaccess_test.go`, `aeon/internal/policy/spec_stubs.go`

### T4 — `verdict.go`: the sentence
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~160
- detail: tasks/T4.md
- success_criteria:
  - unit test `a refusal names its policy, type, target and source` passes
  - unit test `a refusal with no expiry says so` passes
  - a verdict is a **discriminated result**, not an `error` (S4's DB19)
- files_expected: `aeon/internal/policy/verdict.go`, `aeon/internal/policy/verdict_test.go`

### T5 — `store.go`, and the authorship rule
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3]
- est_changed_lines: ~280
- detail: tasks/T5.md
- success_criteria:
  - reads and writes S4's `policy` table; **no migration is added**
  - unit test `an Allow from Agent, MCPServer or Import is refused at write time` passes, all three
  - unit test `a Deny from any source is accepted` passes
  - the refusal names the source
  - unit test `the authorship rule is enforced in exactly one place` passes, asserted by grep
- files_expected: `aeon/internal/policy/store.go`, `aeon/internal/policy/store_test.go`

### T6 — acceptance, which is not a gate
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T5]
- est_changed_lines: ~200
- detail: tasks/T6.md
- success_criteria:
  - acceptance is recorded per target with its instant
  - unit test `an unaccepted policy is still enforced` passes — **the one that matters**
  - unit test `acceptance is idempotent` passes
- files_expected: `aeon/internal/policy/record.go`, `aeon/internal/policy/record_test.go`

### T7 — `policy_event`, and the age cap
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T5]
- est_changed_lines: ~220
- detail: tasks/T7.md
- success_criteria:
  - every refusal is recorded with its subject
  - the age cap is a named constant, and Q2 is answered in the notes
  - unit test `events past the cap are removed` passes
  - unit test `the depth and the cap are both reportable` passes
- files_expected: `aeon/internal/policy/record.go`, `aeon/internal/policy/retention.go`, `aeon/internal/policy/retention_test.go`

### T8 — the runtime consults the evaluator
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3, T4]
- est_changed_lines: ~260
- detail: tasks/T8.md
- success_criteria:
  - the tool loop evaluates **before** dispatch, structurally (G12 generalised)
  - unit test `a denied tool is never called` passes, with a spy at zero
  - unit test `the refusal reaching the model carries the sentence` passes
- files_expected: `aeon/internal/runtime/loop.go`, `aeon/internal/runtime/policy_gate.go`, `aeon/internal/runtime/policy_gate_test.go`

### T9 — the manifest, and the disagreement record
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T8]
- est_changed_lines: ~260
- detail: tasks/T9.md
- success_criteria:
  - the policies in force are written into `step.context_manifest`
  - unit test `replay reproduces the original step's policy set, not today's` passes
  - unit test `an attempt the runtime refused is recorded as a disagreement` passes
  - unit test `the runtime wins when prompt and policy disagree` passes
- files_expected: `aeon/internal/runtime/context.go`, `aeon/internal/policy/record.go`, `aeon/internal/runtime/disagreement_test.go`

### T10 — `aeon policy`
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T5, T4]
- est_changed_lines: ~280
- detail: tasks/T10.md
- success_criteria:
  - `list`, `add`, `remove`, `why` exist; **no HTTP and no MCP** (PL15)
  - unit test `why explains a verdict and changes nothing` passes
  - unit test `add refuses a non-operator Allow with the source named` passes
  - unit test `no policy authoring reaches the narrow surface` passes, asserted by grep
- files_expected: `aeon/cmd/aeon/policy.go`, `aeon/cmd/aeon/policy_test.go`

### T11 — the three-state answer
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T6]
- est_changed_lines: ~180
- detail: tasks/T11.md
- success_criteria:
  - three states: enforced-and-acknowledged, enforced-but-unacknowledged, could-not-determine
  - unit test `all three states are reachable` passes
  - unit test `unacknowledged is distinguishable from could-not-determine` passes
  - **unit test `no state collapses into another in rendering`** passes
- files_expected: `aeon/internal/policy/inforce.go`, `aeon/internal/policy/inforce_test.go`

### T12 — walk the demo, write the QA suite
- engineer: technical-writer-loop
- tech: docs
- depends_on: [T9, T10, T11]
- est_changed_lines: ~200
- detail: tasks/T12.md
- success_criteria:
  - `DEMO.md` runs end to end on a clean checkout, and anything undocumented is added
  - `docs/test_suites/aeon-policy.md` exists, status `draft`, ids `QAPL-01` upward
  - the suite covers deny-wins, the authorship rule, expiry, the three-state answer and the age cap
- files_expected: `docs/aeon/S10-policy/DEMO.md`, `docs/test_suites/aeon-policy.md`

---

## Files

| File | Task | Notes |
| --- | --- | --- |
| `internal/policy/types.go` | T1 | — |
| `internal/policy/evaluate.go` | T2 | **pure; no store import** |
| `internal/policy/spec_toolaccess.go` | T3 | — |
| `internal/policy/spec_stubs.go` | T3 | six refusing stubs |
| `internal/policy/verdict.go` | T4 | — |
| `internal/policy/store.go` | T5 | the only impure file |
| `internal/policy/record.go` | T6, T7, T9 | **three tasks touch it; T6 first** |
| `internal/policy/retention.go` | T7 | — |
| `internal/policy/inforce.go` | T11 | — |
| `internal/runtime/policy_gate.go` | T8 | — |
| `internal/runtime/loop.go` | T8 | edits S5's loop |
| `internal/runtime/context.go` | T9 | edits S5's manifest assembly |
| `cmd/aeon/policy.go` | T10 | — |

**`record.go` is the one serialization point.** T6 creates it, T7 adds events and T9 adds the disagreement
— in that order, and no two of them run concurrently.

## Ordering rationale

**T1, T2 before anything real.** The evaluator must be correct over a trivial type before `ToolAccess`
exists, because deny-wins debugged through a real spec is deny-wins attributed to that spec.

**T5 before T6 and T7** because both record against rows the store writes, and the authorship rule has to
exist before anything can store a policy at all.

**T8 before T9**: the gate has to refuse before there is a disagreement to record.

**T12 last, and it is a real walk** — not a proofread. Every slice's final task is the demo, because a
slice is done when someone else can follow it, not when its tests pass.

## What this slice does not do

No `CommandAccess` evaluator (S11 brings the corpus and the matcher). No authoring UI (an S9 amendment). No
authoring on the narrow surface, ever (PL15). No migration (S4's T2). No policy distribution (Stage 5).

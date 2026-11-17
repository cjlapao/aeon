# Plan — S15 Agent plugins

> **Source material:** [`design.md`](design.md) · the roadmap's [§3.1](../ROADMAP.md) for PL1 ·
> [`../S14-agent-services/design.md`](../S14-agent-services/design.md) for cards, skills and Tasks.

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Stage:** 3 · Car, sixth of eight. Depends on **S10**, **S12**, **S14**.

---

## What is settled before planning

**The trust model was built before there was anything to import.** PL1 means an imported agent's policy can
only narrow it; AS2 means cards are signed; MC2 means private tools are invisible. **So this plan contains
no cryptography task, no sandbox task and no trust-store task** — the last belongs to S21.

**The blocklist is a policy** (AP3), so there is no blocklist data structure to design. The import check
evaluates `ToolAccess` denies targeting `all-agents`, which makes **the import gate and the use gate the
same gate.**

**An imported agent is the same object as a first-party one** (AP1), so there is no second registry and no
second dispatch path.

## How we will do it

**The three-step path first** (T1), because AP2 is a property of the path's shape. An import that lands
`enabled` is a one-line mistake, and the cheapest way to prevent it is for `enabled` to be unreachable from
import at all.

**Then the check** (T2) — and it is a policy evaluation, not a list. **Then the states** (T3), because the
check needs somewhere to leave an agent that failed, and that somewhere is `disabled` with a reason.

**Then consent** (T4), **then revocation** (T5). Revocation is late deliberately: it needs a Task in flight
to be worth testing, and T5 is the first point where one can exist.

**Then provenance** (T6), **the CLI** (T7), **deletability** (T8), the walk (T9), **and the app** (T10).

**T10 is the 2026-10-25 amendment**, made the same way S14's was (the bench demo-runner spec): the Test
bench → Demos tab gains an **"Agents · plugins (S15)"** panel, so the slice's demo/QA ends **in the app** —
live reads only, refusals as data, labeled replay, and **no management button ever leaves the page** (R6:
import, enable, disable and revoke stay CLI).

**The ordering that matters:** **T2 before T4.** Consent must show what the check already evaluated — a
consent screen built first will invent its own notion of what an agent is asking for, and then the two
disagree about the thing the operator is being asked to approve.

## Open questions this plan answers

| # | Question | How |
| --- | --- | --- |
| Q1 | What is a package? | T1 — mirror how a skill is added today; **no executable code** |
| Q2 | Does revoke remove the grants? | T5 — **no**, and the consequence goes into consent |
| Q3 | **Can a disabled agent's in-flight Task complete?** | T5 — **yes**; revoke is the one that refuses |
| Q4 | Where does an imported body sit? | T1 — the same registry (AP1) |

---

## Task sequence

| # | Task | Gate |
| --- | --- | --- |
| **T1** | `import.go` — the three-step path | Import leaves the agent `disabled`; `enabled` is unreachable from import |
| **T2** | `check.go` — the security check, as a policy evaluation | A blocklisted request is refused, naming the policy; no list here |
| **T3** | `state.go` — four states | `revoked` is terminal and is not `not-installed` |
| **T4** | `consent.go` — requests, not capabilities | Rendered text says *requests*; requested ≠ granted |
| **T5** | `revoke.go` — revocation that revokes | An in-flight result is refused on arrival and recorded |
| **T6** | `provenance.go` — first-party is ours to set | A package claiming first-party is ignored |
| **T7** | `aeon agent` — import, list, enable, disable, revoke | Each state transition is reachable and recorded |
| **T8** | The level-0 invariant | The suite passes with no imported agents |
| **T9** | Walk the demo, write the QA suite | `DEMO.md` runs as written |
| **T10** | The demo runs from the app — the S15 panel | The panel's vitest cases pass **before** the panel exists; three states only |

### T1 — `import.go`: the three-step path
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~280
- detail: tasks/T1.md
- success_criteria:
  - **unit test `import leaves the agent disabled` passes**, asserted immediately after import
  - **unit test `enabled is unreachable from import` passes**, asserted by the type or by grep
  - unit test `an imported agent is registered beside the 25, unchanged` passes (AP1)
  - unit test `a package carrying executable code is refused` passes
  - Q1 and Q4 answered: the package shape, and where the body sits
- files_expected: `aeon/internal/plugin/import.go`, `aeon/internal/plugin/import_test.go`, `aeon/internal/plugin/testdata/packages/**`

### T2 — `check.go`: the security check, as a policy evaluation
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~260
- detail: tasks/T2.md
- success_criteria:
  - **unit test `a blocklisted tool request is refused at import, naming the policy` passes**
  - **unit test `no tool list exists in this package` passes**, asserted by grep — the check calls S10's evaluator
  - unit test `an Allow policy in the package is refused, naming the source` passes (AP6)
  - unit test `a Deny policy in the package is accepted` passes
  - **unit test `an unverifiable card is refused and registers no skills` passes** (AP8)
- files_expected: `aeon/internal/plugin/check.go`, `aeon/internal/plugin/check_test.go`

### T3 — `state.go`: four states
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~220
- detail: tasks/T3.md
- success_criteria:
  - four states, as a closed type: `not-installed`, `disabled`, `enabled`, `revoked`
  - unit test `revoked is terminal` passes
  - **unit test `revoked is not not-installed` passes** — the record survives
  - unit test `a re-import after revocation is a new record` passes
  - unit test `disable and enable are reversible` passes
- files_expected: `aeon/internal/plugin/state.go`, `aeon/internal/plugin/state_test.go`, `aeon/internal/store/schema.go`

### T4 — `consent.go`: requests, not capabilities
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~240
- detail: tasks/T4.md
- success_criteria:
  - **unit test `consent renders requests, not capabilities` passes** — the word, and the shape
  - **unit test `requested and granted differ` passes** — a request with nothing granted shows nothing granted
  - unit test `consent shows what granting would mean` passes, per request
  - unit test `a re-import shows pre-existing grants distinctly` passes (Q2's consequence)
- files_expected: `aeon/internal/plugin/consent.go`, `aeon/internal/plugin/consent_test.go`

### T5 — `revoke.go`: revocation that revokes
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3]
- est_changed_lines: ~280
- detail: tasks/T5.md
- success_criteria:
  - unit test `revoke stops new invocations immediately` passes
  - **unit test `revoke refuses an in-flight Task's result on arrival` passes** — with a real Task, not a stub
  - unit test `the refused in-flight result is recorded` passes
  - **unit test `a disabled agent's in-flight Task completes` passes** — Q3, the distinction that keeps AP12 meaningful
  - **Q2 answered: revoke does not delete the operator's grants**, and the notes say why
- files_expected: `aeon/internal/plugin/revoke.go`, `aeon/internal/plugin/revoke_test.go`

### T6 — `provenance.go`: first-party is ours to set
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~180
- detail: tasks/T6.md
- success_criteria:
  - **unit test `a package claiming first-party is ignored` passes**
  - **unit test `an imported agent's result is labelled untrusted` passes** (AS12, via S14's mechanism)
  - unit test `the first-party flag is set by the registry` passes, asserted by grep — never read from the package
  - **S14's Q3 is recorded as answered** in its design
- files_expected: `aeon/internal/plugin/provenance.go`, `aeon/internal/plugin/provenance_test.go`, `docs/aeon/S14-agent-services/design.md`

### T7 — `aeon agent`: the state transitions
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T4, T5, T6]
- est_changed_lines: ~280
- detail: tasks/T7.md
- success_criteria:
  - `import`, `list`, `enable`, `disable`, `revoke` exist; `list` shows state and provenance
  - unit test `import prints the consent and does not enable` passes
  - unit test `every transition is recorded with its reason` passes
  - **unit test `nothing fetches` passes**, asserted by grep — no HTTP client, no URL, no auto-update (AP10)
  - **unit test `no plugin management reaches the narrow surface` passes** (R6's reasoning)
- files_expected: `aeon/cmd/aeon/agent.go`, `aeon/cmd/aeon/agent_test.go`

### T8 — The level-0 invariant
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T7]
- est_changed_lines: ~160
- detail: tasks/T8.md
- success_criteria:
  - **the whole suite passes with no imported agents** (AP11)
  - unit test `the 25 bodies are unaffected by this machinery` passes
  - unit test `no package imports plugin unconditionally` passes
- files_expected: `aeon/internal/plugin/absent_test.go`

### T10 — The demo runs from the app: "Agents · plugins (S15)"
- engineer: ui-design-engineer-loop
- tech: react-ts
- depends_on: [T9]
- est_changed_lines: ~260
- detail: tasks/T10.md
- success_criteria:
  - **the panel's vitest cases pass BEFORE the panel exists** (written first, red, then green)
  - live rows read only `agent.plugins` — **zero model spend, nothing fetched, no management method ever sent**
  - the consent text renders from the record with the word REQUESTS and "none of these are granted yet"
  - recorded import refusals render **verbatim**; an empty registry reads `not runnable here` with the DEMO §1 remedy
  - the in-flight revocation, the disable contrast and the first-party claim are **labeled REPLAY** rows citing grep-verified Go tests
  - **QAC-26** (the S15 panel contract) added to `docs/test_suites/aeon-agents.md`, verified first by the shipped tests
- files_expected: `aeon/ui/test/bench-plugins-s15.test.tsx`, `aeon/ui/src/features/test-bench/pages/DemosPage.tsx`, `docs/aeon/S15-agent-plugins/DEMO.md` (§11 walk record)

### T9 — Walk the demo, write the QA suite
- engineer: technical-writer-loop
- tech: docs
- depends_on: [T8]
- est_changed_lines: ~200
- detail: tasks/T9.md
- success_criteria:
  - `DEMO.md` runs end to end against the fixture packages; anything undocumented is added
  - `docs/test_suites/aeon-agents.md` is amended again, after S14's
  - the suite covers the three-step path, consent-as-requests, in-flight revocation and the first-party claim
- files_expected: `docs/aeon/S15-agent-plugins/DEMO.md`, `docs/test_suites/aeon-agents.md`

---

## Files

| File | Task | Notes |
| --- | --- | --- |
| `internal/plugin/import.go` | T1 | — |
| `internal/plugin/check.go` | T2 | **calls S10's evaluator; holds no list** |
| `internal/plugin/state.go` | T3 | — |
| `internal/plugin/consent.go` | T4 | — |
| `internal/plugin/revoke.go` | T5 | — |
| `internal/plugin/provenance.go` | T6 | — |
| `internal/store/schema.go` | T3 | the plugin state table |
| `cmd/aeon/agent.go` | T7 | — |
| `docs/aeon/S14-agent-services/design.md` | T6 | **S14's Q3, answered** |
| `aeon/ui/test/bench-plugins-s15.test.tsx` | T10 | written **first** — the panel's contract |
| `aeon/ui/src/features/test-bench/pages/DemosPage.tsx` | T10 | additive hunks only; commit hunk-surgical |
| `internal/protocol/method.go` + the `agent.plugins` read | T7 | **one** read method — reads are fine; management stays CLI |

**No file is shared between tasks**, which is unusual for this plan set and worth noticing: the slice is
six small independent pieces plus a CLI, because the hard parts were solved in S10, S12 and S14.

## Ordering rationale

**T1 before everything**, and `enabled` must be **unreachable** from the import path rather than merely not
set. AP2's failure is a one-line mistake, and a type that cannot express it is cheaper than a test that
notices it.

**T2 before T4.** Consent must show what the check evaluated. Built the other way round, consent invents
its own notion of what the agent is asking for — and then the operator approves one thing while the gate
checks another.

**T5 late**, because revocation is only worth testing against a Task that can actually be in flight, and
T3's states are what make "in flight while revoked" expressible.

**T10 last**, because a panel must report the shipped truth: its live rows read only what T7's read method
answers, and its replay rows cite only tests that actually pass by then.

**T5 holds two tests that must disagree**: a *disabled* agent's in-flight Task completes; a *revoked*
agent's is refused. If both behaved the same, AP12's cheap-and-reversible distinction would be gone and
nobody would notice until they used `disable` expecting it to stop something.

## What this slice does not do

No marketplace, registry service or auto-update (AP10). No remote agents (S21). **No sandboxing — and the
reason is precise: an imported agent runs no code of its own, so its tools are the sandbox.** No dependency
resolution. No consent UI — the app **running** the demo (§11, T10: reads, refusals as data, labeled
replay) is the 2026-10-25 amendment, made as S14's was; the consent screen **with an approve-flow** is
still S9's. The CLI demonstrates consent; the panel displays what the CLI recorded. Nobody grants anything
from the app — a grant is `aeon policy add`, by an operator, deliberately (MC4).

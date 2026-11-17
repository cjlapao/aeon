# Brief — S8 Blueprint & the two-step loop

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Inherits:** L1–L10 from [`docs/_archive/aeon-v1-horizontal/07-loop-decide`](../../_archive/aeon-v1-horizontal/07-loop-decide/design.md).
**Stage:** 2 · Bicycle, third of four. Depends on S1–S5; **independent of S6 and S7**.

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief corrects it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** L1–L10 are **inherited and not open**. Read the superseded design for their rationale; do not re-derive
> them. New decisions begin at L11.

---

## 1. How to use this brief

> **Constant. Copy verbatim.**

This is a **brief, not a plan.** It states what must be true when the work is done, what must not change along
the way, and where to look to find out the rest. It contains no task breakdown and no implementation code.

1. **Read** the design and the demo, then §7 for where this brief overrides the design.
2. **Investigate** — §10 names what to read. Do not plan before you have those answers.
3. **Plan and decompose** — §12 gives the rules your task sequence must obey.
4. **Self-check** — §13, against your finished plan, before dispatching anything.

Where this brief states a decision, it is settled. Where it poses a question, answer it from the code.

**On the self-check:** every answer needs an artifact — a command's output, an enumeration, a quoted line.

---

## 2. Definition of Done

| # | Check |
| --- | --- |
| **D1** | **`Decide` is pure and total.** No store, clock or I/O in its imports; called twice on the same inputs it is deep-equal and mutates neither; a fuzz seed of malformed states returns an `Action` and **never panics** (L1). |
| **D2** | **The `Action` set is closed** and every member is reachable by a named test. An unreachable action is either dead or a case `Decide` cannot produce, and both are bugs. |
| **D3** | **A two-lane blueprint runs its steps in lane order and stops at a gate.** Lane order *is* ladder order; there is no sequencing field. |
| **D4** | **`produces:` is enforced.** A step reporting `ok` without writing its declared artifact becomes a **`reject` naming the file**. Asserted by a handler that deliberately writes nothing. |
| **D5** | **Validation reports three states** (L12), and an `unverifiable` diagnostic names **the missing registry and the slice that brings it**. `--strict` fails on them; the default does not. |
| **D6** | **All seven validation classes exist**, each tripped by a fixture that trips **exactly** it, asserted on **the diagnostic's path** — not on a substring of its message. |
| **D7** | **Validation reports every diagnostic, not the first.** Asserted with a fixture tripping all seven at once (L9). |
| **D8** | **An unknown key is `invalid`.** `agents:` for `agent:` fails, with a suggestion. A silently ignored key is how a blueprint comes to mean something other than what it says. |
| **D9** | **An unreachable step is `invalid`, not a warning.** The author believes something runs that never will. |
| **D10** | **A capped edge without `then:` is `invalid`, and a backward `goto` without a counter is `invalid`.** Each is an infinite loop waiting to happen. |
| **D11** | **An unknown `schemaVersion` is a loud refusal at load**, quoting what it found and what is supported (P15). |
| **D12** | **The real `dev-loop.yaml` validates with zero `invalid` diagnostics** (L16). If the real blueprint is invalid, the format is wrong — this is the only test that checks the format against the thing it exists for. |
| **D13** | **Both shipped blueprints validate in CI**, so they cannot rot as the registries grow. |
| **D14** | **The blueprint is pinned as content at loop start** (L6). A running loop finishes on its pinned copy after the source file changes; a **new** loop sees the edit (L19). Both halves asserted. |
| **D15** | **A gate and a step question reach different doors.** `aeon answer` on `kind: 'gate'` reaches `AdvanceLoop`; on a step question it reaches `ResumeStep`. Asserted with spies on **both**, each at zero for the other case (L11). |
| **D16** | **A gate question has a null `tool_use_id`** and never appears in a transcript as a `tool_result`. |
| **D17** | **A code action is refused, not skipped.** `blocked`, naming the action and S16, with the loop **not advanced** and nothing executed — spy at zero (L13). |
| **D18** | **`iterate` executes.** Zero items is a no-op; N items run the ladder N times; the position round-trips through the store (L17). |
| **D19** | **The manifest is one declaration.** A test asserts the condition evaluator and the template resolver read the **same** source (L18). |
| **D20** | **A condition type mismatch is caught at validation, not evaluation.** `gt` against a string field is `invalid` at load. |
| **D21** | **`not-needed`, `skipped` and `pending` are distinct** in the store and in `aeon loop show`. A conditional lane that will never run must not render as `pending`. |
| **D22** | **`AdvanceLoop` holds nothing between calls** (L2). Asserted by driving a loop to completion through repeated calls on **freshly constructed** dependencies each time. |
| **D23** | **The loop runner starts after S7's recovery scan** (L15), asserted on ordering. |
| **D24** | **`make test` and `make lint` are green**, `make fmt` leaves no diff. |

**D1, D12 and D17 are the three that matter.** D1 because everything dispatches on it. D12 because it is the only
check that tests the format rather than a fixture written to suit it. D17 because a skipped code action lets a loop
report success having never pushed.

**D1–D23 must each exist as named tests.**

D24 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

Everything so far runs **one step**. This slice is where a sequence becomes something you author.

L1 settles why the graph is data: there are **five** real protocol skills in this repository with different lanes —
`dev-loop-protocol` (793 lines), `proposal-brief-protocol` (806), `development-map` (525), `conversational-map`
(278), `hotfix-protocol` (115). Freezing one into code makes the other four second-class.

**What is new is a problem the horizontal plan never faced.** A blueprint names agents, task statuses, manifest
fields and **code actions** — and the code-action registry does not exist until S16. A validator that meets one has
two dishonest options: pass it silently, or fail a blueprint that is correct.

So validation is three-state (L12), and the reassuring option is the one explicitly rejected. This is the third
independent convergence on that shape, after cost and the `bash` receipt, which is why it is now P31.

It also buys the demo beat that proves the format: **the real `dev-loop.yaml` ships and validates**, naming exactly
what it cannot check.

## 4. Goal

A two-lane blueprint that runs its steps in order, stops at a gate, and can be **edited without touching code** —
plus a validator that says precisely what it checked, what it could not, and why.

## 5. Shape and the seam

```
daemon ──▶ runner (one goroutine per loop, after recovery) ──▶ AdvanceLoop
                                                                   │
                                       ┌───────────────────────────┤
                                  LoopState (one read)        Decide  ← PURE
                                                                   │
                                                                execute
                                                    ┌──────────────┼──────────────┐
                                              RunModelStep      AskGate      RunCodeAction
                                                (S5)         (question row)    REFUSED → S16
```

### The seams

**`Decide` is pure** (L1, D1) — the seam that makes the interpreter testable with a three-lane toy blueprint instead
of state constructed against the whole real graph.

**`LoopState` is one snapshot read.** If `Decide` ever needs something outside it, **the snapshot is wrong** — that
is the first thing to check when a case turns awkward, and it is inherited advice worth heeding.

**`AdvanceLoop` holds nothing between calls** (L2, D22). That is what makes a loop's crash recovery free: S7
recovers the *step*, and the loop resumes by being advanced again.

**Two doors for one question type** (L11, D15). `kind` is what keeps them apart.

**The handler registry** (L14) is new here and deliberately thin: a name-to-handler map with two entries. S13 fills
it.

## 6. Locked decisions

**Inherited, not open:** L1 (graph is data), L2 (step function), L4 (four outcomes), L5 (condition triples), L6
(pinned per loop), L8 (blueprints route, never grant), L9 (loud at load), L10 (two blueprints ship).

**New:** L11 (two doors), L12 (three-state validation), L13 (`decide` returns unimplemented actions; `execute`
refuses), L14 (handler registry here), L15 (runner after recovery), L16 (`dev-loop.yaml` ships), L17 (`iterate`
now), L18 (one manifest declaration), L19 (the pin, made visible).

**L3 and L7 are inherited but not exercised here** — they govern code actions, which arrive at S16.

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | `unverifiable` names the missing registry and its slice | It must also be **machine-readable**: a `kind` field on the diagnostic, so `--strict` and the app can act on it without parsing prose. A diagnostic whose only structure is its sentence is one nobody can filter. |
| O2 | Validation reports every diagnostic | Its output must be **stable-ordered** — by path, then by class. Non-deterministic diagnostic order makes CI output undiffable and turns a fixture test into a flake. |
| O3 | `produces:` becomes a `reject` when the artifact is missing | The reject's reason must name **the missing path**, and a lane with no `rework` target must have made that `invalid` at load. Otherwise D4's reject has nowhere to go and surfaces as a runtime surprise — which is exactly what L9 exists to prevent. |
| O4 | `Decide` is pure and total | Its `Blocked` action must carry a **structured reason**, like S7's `RecoveryFailure` — not a formatted string. S9 renders it and S17 routes on it. |
| O5 | The design does not say what happens if a pinned blueprint fails to parse | A loop whose **pinned** content will not load is `blocked` with a reason saying so. It cannot be re-pinned from the file — that would silently change what a running loop is doing, which is the whole point of L6. |
| O6 | The design does not say what a `gate` does with a non-answer | An answer outside the question's declared `answers` set is **refused at the answer boundary**, leaving the loop `waiting`. A gate whose closed set can be bypassed is not closed. |
| O7 | Two blueprints ship (L10) | Plus a **`broken-fixture`** blueprint tripping all seven classes, shipped as test data and used by the demo's §5. A fixture living only in a test file cannot be shown to anyone. |
| O8 | `aeon loop show` renders the ladder | It must render an **iterating lane's item count** and a conditional lane's **condition**, because "why did this not run" is the first question anyone asks of a ladder. |

## 8. Non-goals

- **N1 — No code actions, git, forge or templates.** **S16.** Blueprints name them; `execute` refuses them.
- **N2 — No agent bodies, no `by-tech-tag`.** **S13.** The registry gets two entries.
- **N3 — No phases, redo rounds, or the ladder's `verify`/`security`/`review`/QA lanes as behaviour.** **S17.** The
  vocabulary exists and `dev-loop.yaml` uses it; nothing here implements those semantics beyond routing.
- **N4 — No permissions work.** **S11.** L8 means this slice cannot widen anything.
- **N5 — No app.** **S9.**
- **N6 — No visual blueprint builder.** Never promised for the alpha.
- **N7 — No retrieval-resolved context.** S22.
- **N8 — No new predicate functions or resolvers.** Both are closed registries by design (L5); adding an escape
  hatch here would make the format a runtime-interpreted language.
- **N9 — No change to `RunStep`, `ResumeStep` or S7's recovery.** S8 calls them.

### Standing non-goals

- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never run `git clean`.**
- **No MCP tool for loop control** (R6). Starting and stopping loops is not something an agent gets to do.

## 9. Repo invariants

**I1 — `Decide` is pure** (D1). No store, no clock, no I/O. Asserted by its import list.

**I2 — `LoopState` is one read.** If `Decide` needs something outside the snapshot, fix the snapshot.

**I3 — `AdvanceLoop` holds nothing between calls** (D22).

**I4 — The `Action` set and the `kind` set are closed.** No default branch that swallows an unknown value; an
exhaustive switch with an explicit failing default.

**I5 — Blueprints route; they never grant** (L8). A blueprint cannot name a tool, widen a toolset, or reach code
outside the closed registries. Asserted by there being no field for it in the format.

**I6 — The manifest is one declaration** (L18, D19).

**I7 — Every diagnostic carries a machine-readable `kind` and a path** (O1, O2).

**I8 — A refused code action does not advance the loop** (D17).

**I9 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/07-loop-decide/design.md`** in full — L1–L10, the field reference, the seven validation
   classes, the outcome table. **This is the specification of the format**; S8 ports it to Go and adds L11–L19.
2. **`docs/_archive/aeon-v1-horizontal/08-loop-execute/`** — for what `execute` becomes, and which parts are S16's.
3. **`../ARCHITECTURE.md` §8 P15, P31** — `schemaVersion`, and the three-state rule.
4. **`internal/store/`** (S4) — the `loop`, `loop_phase`, `task`, `question` tables; the transition machine; and
   whether a place to pin blueprint content exists or must be added.
5. **`internal/runtime/`** (S5, S6) — `RunStep`, `ResumeStep`, and the literal handler L14 turns into a registry.
6. **`opencode/skills/dev-loop-protocol/SKILL.md`** — the prose `dev-loop.yaml` must express. **Read it to author
   the blueprint, not to implement it.**

### 10.2 The patterns to copy

- **Discriminated results over errors** (S4's DB19) — a domain refusal is not an infrastructure failure.
- **Tagged structs, not interfaces** (M20) — the `Action` and step-`kind` unions persist and round-trip.
- **S7's structured `RecoveryFailure`** as the model for O4's `Blocked` reason.
- **S2's conformance fixtures** for the diagnostic shapes, so the app and CI read one contract.
- **S1's committed-fixture pattern** for the shipped blueprints' expected diagnostics.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | Does S4's schema have somewhere to **pin blueprint content** per loop, or is a column/table needed? | `internal/store/schema.go`. **If it needs a migration, that is a task.** |
| Q2 | Does the `question` table's `tool_use_id` column allow NULL, as S6's design says it does? | `internal/store/schema.go`. D16 depends on it. |
| Q3 | What does S4's transition machine export, and can `sets:` be validated against it **including the POC overlay**? | `internal/store/`. If the overlay is absent, say so — a `sets:` check that ignores mode is wrong for POC loops. |
| Q4 | How does S5's literal handler get its system prompt and toolset, and what is the smallest registry that preserves that? | `internal/runtime/compile.go`. **The registry must not become a place to grant tools** (I5). |
| Q5 | Where does the daemon start long-lived work, and where does S7's recovery scan sit relative to it? | `internal/daemon/`. L15 and D23. |
| Q6 | Does a YAML library already arrive through the framework's dependency tree, and which? | `go.mod` and `go list -m all`. Two YAML libraries in one binary is the kind of thing nobody notices until the parse differs. |
| Q7 | Does S5's `StepResult.Outcome` already carry the four outcomes L4 needs (`ok`/`reject`/`blocked`/`failed`)? | `internal/runtime/`. **If it does, do not define a second set.** |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q8 | What is the `Position` shape in the store, and does a lane's item identity survive a task being added mid-loop (L17)? State the answer; a positional index that shifts is a bug. |
| Q9 | How does a gate's answer get validated against its closed `answers` set, and where (O6)? |
| Q10 | What happens to a loop whose pinned blueprint no longer parses (O5)? |
| Q11 | Which of the seven validation classes can be checked in S8, and which are wholly `unverifiable`? Produce the table before planning. |

---

## 11. Behavioural contract

- `Decide(blueprint, manifest, state) → Action`, pure and total, closed set (D1, D2).
- Lane order is ladder order; a gate stops the loop (D3).
- `produces:` missing → `reject` naming the path, with a resolvable target or an `invalid` at load (D4, O3).
- Validation: three states, machine-readable kinds, stable order, every diagnostic, each with a path (D5–D10, O1,
  O2).
- Unknown `schemaVersion` → loud refusal at load (D11).
- `dev-loop.yaml` validates with zero `invalid` (D12); both shipped blueprints validate in CI (D13).
- The pin is content, taken at start; a running loop keeps it, a new loop sees the edit (D14).
- Gate and step questions reach different doors; a gate has a null `tool_use_id` (D15, D16).
- A code action is refused with `blocked` naming it and S16; the loop does not advance (D17, I8).
- `iterate` executes; zero items is a no-op; position round-trips (D18).
- One manifest declaration serves conditions and templates (D19).
- A condition type mismatch is `invalid` at load (D20).
- `not-needed` / `skipped` / `pending` are distinct, in the store and in `loop show` (D21, O8).
- `AdvanceLoop` holds nothing between calls (D22); the runner starts after recovery (D23).
- An out-of-set gate answer is refused, loop stays `waiting` (O6).
- A pinned blueprint that will not parse → `blocked`, never re-pinned from the file (O5).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **`Decide` is its own task**, and it comes after the format, the manifest and the conditions, because it
  reads all three. It is pure, so its tests need no store.
- **R5** — **the validator is at least two tasks**, split by what they need: the classes checkable from the blueprint
  alone versus those needing S4's transition machine and the registries. They fail differently and one is where
  `unverifiable` lives.
- **R6** — **the manifest and the condition evaluator are one task.** L18 says one declaration serves both; splitting
  them is how two grow.
- **R7** — the two shipped blueprints and the broken fixture are **authoring**, and belong with the task that can
  validate them, not scattered.
- **R8** — no task over roughly 300 changed lines or 12 files without justification.
- **R9** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure", "FIXME".
  `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md` and amends the QA suites.
- **R12** — **no task edits `ARCHITECTURE.md` or `S5-one-step/design.md`.** P31 and L14's amendment are already
  landed.

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines or ~12 files without justification (R8) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | Paste Q11's table: which validation classes are checkable in S8, which are `unverifiable` | **A missing table blocks the plan.** It is the specification for D5 |
| A6 | Enumerate the `Action` set and name the test reaching each member | Every member reachable (D2) |
| A7 | Paste Q1's and Q7's answers | **If a pin needs a migration, name the task. If `StepResult.Outcome` already has the four outcomes, confirm no second set is defined** |
| A8 | Paste Q3's answer on the POC overlay | A `sets:` check that ignores mode is wrong for POC loops |
| A9 | `grep -rn "database/sql\|store\." ` over your planned `decide.go` | No hits (I1) |
| A10 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task implements a code action, git or forge call | N1 |
| B2 | Confirm no task writes an agent body or a resolver | N2, N8 |
| B3 | Confirm no task implements phase/redo/QA-round semantics | N3 |
| B4 | Confirm no task touches permissions, and that the format has **no field** that could grant a tool | N4, I5 |
| B5 | Confirm no task touches the app | N5 |
| B6 | Confirm no task adds a predicate or resolver escape hatch | N8 |
| B7 | Confirm no task modifies `RunStep`, `ResumeStep` or recovery | N9 |
| B8 | Confirm no task edits `ARCHITECTURE.md` or `S5-one-step/design.md` | R12 |
| B9 | Quote the `out_of_scope` lines covering N1–N9 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D23 to a task and a named test | §2 |
| C2 | For D6, confirm each fixture trips **exactly one** class, and the assertion is on the **path** | D6 |
| C3 | For D12, confirm the assertion is **zero `invalid`** on the real blueprint — not "it loads" | D12 |
| C4 | For D14, confirm **both** halves: the running loop keeps its pin, the new loop sees the edit | D14, L19 |
| C5 | For D15, confirm spies on **both** doors, each asserted at zero for the other case | D15 |
| C6 | For D17, confirm the loop is **not advanced** and the spy is at zero — not merely that a message was logged | D17, I8 |
| C7 | For D22, confirm the test constructs **fresh** dependencies per call | D22 |
| C8 | For D19, name the single declaration and the two readers | D19, L18 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm `Decide` follows the format, manifest and conditions | R4 |
| D2 | Confirm the validator is split by what it needs, and `unverifiable` lives in the second part | R5 |
| D3 | Confirm the manifest and the condition evaluator are one task | R6 |
| D4 | Confirm the shipped blueprints land with a task that can validate them | R7 |
| D5 | Confirm the runner task follows `AdvanceLoop` | — |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm `Decide` has no store, clock or I/O | I1 |
| E2 | Confirm `LoopState` is assembled in one read | I2 |
| E3 | Confirm the `Action` and `kind` switches are exhaustive with a failing default | I4 |
| E4 | Confirm the format has no field that names a tool or widens a toolset | I5, L8 |
| E5 | Confirm every diagnostic carries a `kind` and a path | I7, O1 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | State the `Position` shape and whether item identity survives a task added mid-loop (Q8). A shifting positional index is a bug. |
| F2 | State where and how a gate answer is validated against its closed set (Q9, O6). |
| F3 | State what happens to a loop whose pinned blueprint no longer parses (Q10, O5). |
| F4 | Name the YAML library, and confirm it is the **only** one in `go.mod` (Q6). |
| F5 | State how `not-needed`, `skipped` and `pending` are stored distinctly (D21). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it and fixes whatever does not match.

**Three of its sections are load-bearing.**

**§3** shows the pin from both sides — a running loop ignoring an edit, a new loop honouring it. It looks like a bug
until you understand it, which is exactly why it is demonstrated rather than documented.

**§4** validates the **real** `dev-loop.yaml`. If it produces a single `invalid` diagnostic, the format is wrong and
the slice is not done. And its `--strict` half must fail — a validator whose strict mode passes an unverifiable
blueprint has no strict mode.

**§7** refuses a code action. Confirm the loop **stops** rather than continuing: a skipped code action lets a loop
reach `merge` having never pushed, and report success.

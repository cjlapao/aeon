# Brief — S17 The full ladder

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Inherits:** L1–L10 and the ladder's shape from `docs/_archive/aeon-v1-horizontal/07-loop-decide` and
[`08-loop-execute`](../../_archive/aeon-v1-horizontal/08-loop-execute/plan.md). New decisions begin at **L27**.
**Stage:** 3 · Car, fourth of four — **the car**. Depends on S8, S11, S13, S16.

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief overrides it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** **This slice builds almost no new mechanism.** It is composition and authorship, and §12's rules are shaped
> around that: the risk is in round-cap arithmetic and task ordering, not in new code.

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
| **D1** | **A reject actually happens, and routes to `rework`.** `verify` returning `fail` re-enters `implement`, the round is consumed, and the lane continues. A ladder where nothing rejects has not been demonstrated (L30). |
| **D2** | **Counters are two-level** (L28). A five-task loop where tasks 1 and 2 each consume two `review_rounds`: **task 3 still has two of its own.** This test fails against a per-loop-only implementation and passes every other test here. |
| **D3** | **The loop-level ceiling stops a loop whose tasks are each individually inside their caps.** `total_failed_gates` is what prevents forty rounds being called progress. |
| **D4** | **`escalate` collapses to `block` unattended**, naming the counter, the step, **and that the loop is unattended** (L29) — so nobody debugs the wrong thing. |
| **D5** | **Tasks are topologically sorted at load.** A cycle, a self-dependency, a duplicate id, and a `depends_on` naming a nonexistent task are all **refused at load**, each naming the offending task (L31). |
| **D6** | **The position stays monotonic.** `iterate` never revisits an earlier item — asserted on the position sequence, because that is what the sort buys. |
| **D7** | **A redo carries the rejection's reason** into the re-entered step's input, asserted by a handler spy reading its own input (L32). |
| **D8** | **`verify` has three outcomes** — `pass`, `fail`, `unverified` — and **`unverified` is never recorded as `pass`** (L33). Asserted on the stored outcome, not on a log line. |
| **D9** | **`unverified` and `fail` produce different redo reasons.** One says fix the finding; the other says make the check run. Collapsing them wastes a round. |
| **D10** | **The merge gate reads review *state*.** A forge with no thread API still reaches the gate, and deferred findings land in the durable backlog file (L34). |
| **D11** | **Phases record per lane; an iterating lane records per-item task state** and has no single `build` phase status (L27). |
| **D12** | **A task cannot complete while a dependency is incomplete** — S4's transition machine, driven rather than validated. |
| **D13** | **The full ladder runs end to end against the fake forge**, every lane in order, with one gate. |
| **D14** | **The loop's own artefacts are committed** — `plan.md`, `tasks/`, `discovery.md` — so the record of what was asked lands with the work. |
| **D15** | **No new mechanism was added.** If a lane needed one, that is recorded as a finding rather than built (§8 N1). |
| **D16** | **`make test` and `make lint` green**, `make fmt` leaves no diff. |

**D1, D2 and D8 are the three that matter.** D1 because a ladder that only passes proves nothing. D2 because the wrong
implementation is invisible until the fifth task. D8 because reporting an unrun check as clean is the most plausible lie
in the system.

**D1–D14 must each exist as named tests**, D13 additionally as the demo.

D16 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

Lanes exist (S8), handlers exist (S13), code actions exist (S16), the guard exists (S11), the fourteen task states and
their transition machine exist (S4). **What does not exist is the method** — the sequence, the gates, the round caps and
the send-backs that make those parts a development process rather than a pile of capabilities.

**Read the protocol before planning.** `opencode/skills/dev-loop-protocol/SKILL.md` is 793 lines describing a method that
works, and it settles four questions this slice would otherwise invent: counters are already two-level, dependencies are
validated at plan load rather than discovered at merge, the review gate is on review *state* because not every forge can
resolve threads, and **verification has three outcomes** because an OOM-killed linter *"is unverified, which is not the
same as clean."*

**The one problem genuinely new here** is that S8's `iterate` is a monotonic forward walk and tasks have `depends_on`.
Settling the order at load keeps the walk; skipping and revisiting would not.

## 4. Goal

The full `dev-loop` ladder running: investigate, plan, one approval gate, then per task — issue, implement, verify,
security, push, PR, review, merge — with send-backs bounded by two-level caps, and a real feature merged at the end.

## 5. Shape and the seam

```
clarify ─▶ discovery ─▶ legal? ─▶ plan ─▶ [GATE] ─▶ build (iterate: tasks) ─▶ qa? ─▶ done
                                                          │
                                    issue → implement → verify → security? → push → pr → review → merge
                                                          │
                                              reject ─▶ rework: implement
                                              carrying the reason (L32)
                                              bounded per task at 2 (L28)
```

### The seams

**Nothing new goes below the blueprint.** Every lane is a handler dispatch (S13) or a code action (S16), sequenced by
data (S8). If a lane appears to need a mechanism, that is a finding (D15).

**The plan loader is the one new boundary** (L31): it validates `depends_on`, topologically sorts, and refuses a cycle —
before `iterate` sees anything.

**Counters live at two levels** (L28), and the level is a property of the counter rather than of the call site. A
counter whose level depends on who increments it will be incremented at both.

**`verify`'s third outcome is a handoff outcome**, not a new type. S13's T3 gave handlers four outcomes; `unverified`
routes as `reject` with a distinct reason rather than becoming a fifth.

## 6. Locked decisions

**Inherited:** L1 (the graph is data), L2 (step function), L4 (four universal outcomes), L6 (blueprints pinned per loop),
L7 (idempotent actions), L8 (blueprints route, never grant), L9 (validation loud and at load), and S16's L20–L26.

**New:** L27 (a phase is what a lane records), L28 (two-level counters, inherited from v1), L29 (`escalate` collapses to
`block` unattended), L30 (the demo carries a deliberate defect), L31 (topological sort at load), L32 (a redo carries its
reason), L33 (`verify` has three outcomes), L34 (review state, not threads).

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | `escalate` collapses to `block` unattended | The block's reason must distinguish **"the cap was reached"** from **"nobody could be asked"**. Both are true and they call for different responses — raise the cap, or run it attended. A single message conflating them sends someone to change the wrong thing. |
| O2 | Tasks are topologically sorted | The sort must be **stable**: two tasks with no dependency between them keep their plan order. An unstable sort makes a loop non-reproducible across runs from the same plan, which breaks L6's whole reason for pinning. |
| O3 | A redo carries the rejection's reason | It must also carry **which attempt this is** — "retry 2 of 2" — because a handler on its last attempt should behave differently from one on its first, and it cannot know unless told. |
| O4 | `unverified` routes as a reject | It must **not** consume the same counter as a `fail`. A tool that will not run is not the handler's failure, and spending its retries on an environment problem exhausts the budget before any real attempt. Use a separate counter or do not charge it — state which. |
| O5 | The design does not say what happens when a task's dependency **failed** | The dependent task is **`blocked`, not `failed`** — it never ran, and marking it failed would report work as attempted that was not. Its reason names the dependency. |
| O6 | The merge gate reads review state | It must record **which** state it read and when, in the step's evidence. A gate that passed on a state nobody can reconstruct is a gate nobody can audit — and this is the last gate before something irreversible. |
| O7 | Deferred findings go to a durable backlog | That file is **appended, never rewritten**, and each entry names its PR and date. A backlog that gets regenerated loses the history that makes it a backlog. |
| O8 | The loop's artefacts are committed | They are committed **by the loop's own `git.commitAndPush`**, not by a special path. A separate commit mechanism for Aeon's own files is a second implementation of S16's completeness assertion. |

## 8. Non-goals

- **N1 — No new mechanism.** If a lane appears to need one, **record it as a finding and stop** (D15). S8 through S16
  built the parts; discovering a gap here is information about those slices.
- **N2 — No new tools and no new code actions.** The nine and the eight are complete.
- **N3 — No second protocol as a blueprint.** `proposal-brief`, `development-map`, `conversational-map` stay unexpressed.
  `hotfix` becomes expressible; authoring it is not this slice's.
- **N4 — No dogfooding.** Aeon does not run this ladder on its own repository. A loop that breaks the daemon running it
  is a bad first demonstration.
- **N5 — No cleanup or branch deletion.** S16's N8 and N9 stand.
- **N6 — No retrieval.** S22.
- **N7 — No blueprint builder.** Never promised for the alpha.
- **N8 — No relaxing a gate to make the demo pass.** If `verify` rejects something correct, fix the handler or the
  criteria and say so. A gate loosened to green a demo is worse than a red demo.

### Standing non-goals

- **Never write to `opencode/`.** The protocol is read, not modified.
- **Never point an automated test at a real repository or token** (S16's O7). The fake forge, always.
- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never run `git clean`.**

## 9. Repo invariants

**I1 — No new mechanism** (D15, N1).

**I2 — Counters' levels are properties of the counter** (L28), not of the call site.

**I3 — `iterate`'s position is monotonic** (D6). The sort is what makes that possible.

**I4 — The sort is stable** (O2), so a loop is reproducible from its pinned plan.

**I5 — `unverified` is never stored as `pass`** (L33, D8).

**I6 — The backlog file is append-only** (O7).

**I7 — Aeon's artefacts are committed through S16's action**, not a special path (O8).

**I8 — P31 now names five instances**; that correction is already landed. Do not re-edit `ARCHITECTURE.md`.

**I9 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`opencode/skills/dev-loop-protocol/SKILL.md`**, all 793 lines. **This is the method being encoded.** It settles the
   counter levels, the `depends_on` validation, the review-state gate and the third verification outcome. Read it to
   author the ladder, not to implement it.
2. **`docs/_archive/aeon-v1-horizontal/07-loop-decide/design.md`** — the ladder's shape, the field reference, the outcome routing table.
3. **`docs/_archive/aeon-v1-horizontal/08-loop-execute/plan.md`** — the three constraints, and the task status walk
   `pr-open → in-review → merged → complete`.
4. **`internal/store/`** (S4) — the fourteen task states, `ALLOWED_TRANSITIONS`, `TRACKED_PHASES`, `PHASE_STATUS`, and
   whether the counters have columns.
5. **`internal/blueprint/blueprints/dev-loop.yaml`** (S8's T12) — the ladder as data, currently validating and not
   running.
6. **`internal/handler/bodies/verifier-engineer.md`** (S13's T6) — and its drop record, which kept the OOM consequence
   for exactly this slice's L33.
7. **`internal/action/`** (S16) — the eight, their effect classes, and what asks.

### 10.2 The patterns to copy

- **S4's transition machine** — driven here, not reimplemented.
- **S8's `on_reject` routing** — the mechanism exists; this slice supplies the counters and the reasons.
- **S13's handoff outcomes** — `unverified` routes as `reject`, not as a fifth outcome.
- **S16's fake forge** — every automated test runs against it.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | Do the task-level and loop-level counters have store columns, or is a migration needed? | `internal/store/schema.go`. **If a migration is needed, name the task** |
| Q2 | What exactly is in `TRACKED_PHASES`, and does an iterating lane appear in it? | `internal/store/`. L27 depends on the answer |
| Q3 | Does S4's machine already refuse a `complete` whose dependency is incomplete, or is that this slice's? | `internal/store/`. v1's `task_complete` enforced it |
| Q4 | Does `iterate` currently read tasks in any particular order? | `internal/loop/advance.go` (S8's T10). **The sort must go in front of it, not inside it** |
| Q5 | How does a handler receive a step's input, and is there a place for a rejection reason and an attempt number? | `internal/runtime/compile.go`. L32 and O3 depend on it |
| Q6 | Can a handoff carry an outcome the blueprint routes differently by reason, or does routing key only on the outcome? | `internal/loop/decide.go` (S8's T9). **If routing cannot see the reason, L33's two redos are indistinguishable — that is a finding** |
| Q7 | Which lanes in `dev-loop.yaml` have `sets:` values, and are all of them legal in every mode including POC? | S8's T5 validated this; confirm it still holds with every lane running |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q8 | Which counter does an `unverified` charge, if any (O4)? State it. |
| Q9 | What is the deliberate defect in the demo's plan, and which lane catches it (L30)? |
| Q10 | Where does the deferred-findings backlog live, and what is an entry's shape (O7)? |
| Q11 | What is the loop-level ceiling's value, and what happens at it (D3)? |

---

## 11. Behavioural contract

- A reject routes to `rework`, consumes a round, and the lane continues (D1).
- Counters are two-level; task-level caps are per task (D2, D3).
- `escalate` collapses to `block` unattended, distinguishing cap-reached from nobody-to-ask (D4, O1).
- Tasks are topologically and **stably** sorted at load; four classes of bad `depends_on` refused there (D5, O2).
- `iterate`'s position is monotonic (D6).
- A redo carries the reason **and the attempt number** (D7, O3).
- `verify` reports `pass`/`fail`/`unverified`; `unverified` is never stored as `pass` (D8, I5).
- `unverified` and `fail` produce different redo reasons and do not share a counter (D9, O4).
- A task whose dependency failed is **`blocked`, not `failed`** (O5).
- The merge gate reads review state and **records which state, and when** (D10, O6).
- Deferred findings append to a durable backlog naming PR and date (O7, I6).
- Phases record per lane; iterating lanes record per-item (D11).
- A task cannot complete with an incomplete dependency (D12).
- Aeon's artefacts are committed through S16's action (D14, O8).
- **No new mechanism**; a gap is a finding (D15, N1).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **the plan loader and its sort come first.** Everything downstream iterates over its output, and a sort added
  later means retrofitting the walk.
- **R5** — **counters are their own task**, and it carries D2. The two-level split is the one piece of arithmetic in the
  slice, and a per-loop-only implementation passes every other test.
- **R6** — **the three verification outcomes are their own task**, including the handler amendment. `unverified` touches
  the handler, the handoff, the routing and the record; spreading it makes the "never stored as `pass`" assertion
  homeless.
- **R7** — **the ladder's authorship is separate from the machinery.** Making `dev-loop.yaml` run is a different kind of
  work from making counters correct, and mixing them means reviewing prose and arithmetic in one diff.
- **R8** — no task over roughly 300 changed lines or 12 files without justification.
- **R9** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure",
  "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md` and amends the QA suites.
- **R12** — **no task writes to `opencode/`** or edits `ARCHITECTURE.md` (I8, already landed).

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines without justification (R8) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | Paste Q6's answer | **If routing cannot see a reason, L33's two redos are indistinguishable — record it as a finding before planning** |
| A6 | Paste Q1's and Q2's answers | Counter columns, and whether an iterating lane is in `TRACKED_PHASES` |
| A7 | Paste Q8's answer | Which counter an `unverified` charges, if any (O4) |
| A8 | Paste Q9's answer | The demo's deliberate defect and the lane that catches it (L30) |
| A9 | Confirm **no task adds a mechanism** — enumerate what each task adds and show it is composition, authorship, or arithmetic | D15, N1, I1 |
| A10 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task adds a tool or a code action | N2 |
| B2 | Confirm no task authors a second protocol as a blueprint | N3 |
| B3 | Confirm no task runs the ladder on Aeon's own repository | N4 |
| B4 | Confirm no task deletes a branch | N5 |
| B5 | Confirm no task implements retrieval | N6 |
| B6 | **Confirm no task relaxes a gate to make the demo pass** | N8 |
| B7 | Confirm no task writes to `opencode/` or edits `ARCHITECTURE.md` | R12, I8 |
| B8 | Quote the `out_of_scope` lines covering N1–N8 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D14 to a task and a named test | §2 |
| C2 | For D2, confirm the test uses **five tasks** and asserts task 3's budget is **untouched** | D2 |
| C3 | For D5, confirm **all four** bad-`depends_on` classes are covered | D5 |
| C4 | For D6, confirm the assertion is on the **position sequence** | D6 |
| C5 | For D7, confirm a handler **spy reads its own input** | D7 |
| C6 | For D8, confirm the assertion is on the **stored** outcome, not a log line | D8, I5 |
| C7 | For D9, confirm the two redo reasons are asserted **different** | D9 |
| C8 | For D10, confirm the evidence records **which state and when** | D10, O6 |
| C9 | For O5, confirm a dependency-failed task is `blocked`, not `failed` | O5 |
| C10 | Confirm **no** automated test touches a real repository or token | S16's O7 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm the plan loader and sort precede everything iterating | R4 |
| D2 | Confirm counters are their own task | R5 |
| D3 | Confirm the three verification outcomes are one task | R6 |
| D4 | Confirm the ladder's authorship is separate from the machinery | R7 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm no new mechanism, per A9's enumeration | I1 |
| E2 | Confirm a counter's level is a property of the counter | I2 |
| E3 | Confirm the sort is stable | I4, O2 |
| E4 | Confirm the backlog is append-only | I6 |
| E5 | Confirm Aeon's artefacts commit through S16's action | I7, O8 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | State the loop-level ceiling's value and what happens at it (Q11, D3). |
| F2 | State the backlog entry's shape and location (Q10, O7). |
| F3 | State the demo's deliberate defect (Q9, L30). |
| F4 | State which counter `unverified` charges (Q8, O4). |
| F5 | State whether a counter migration is needed (Q1). |
| F6 | Confirm every `sets:` in `dev-loop.yaml` is legal in every mode, POC included (Q7). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it and fixes whatever does not match.

**This is the first demo in the plan that is a product claim rather than a capability**, and three of its sections carry
that claim.

**§2 has a deliberate defect in it**, and that is the point. A ladder where `verify`, `security`, `review` and `qa` only
ever pass has not been demonstrated — they are decoration until one of them sends something back. If the defect is not
caught, the fix is the handler or the criteria, **never loosening the gate** (N8).

**§3 forces `unverified`** by starving the linter of memory. The stored outcome must never read `pass`. A linter that was
killed found nothing **because it did not run**, and reporting that as clean is the most plausible lie available — it
looks like a green check.

**§7 is the actual claim.** A competent person reading the repository in a year should be able to reconstruct what was
asked, what was decided, what came back and why. If they cannot, the method did not work regardless of what the loop
reported — and no test in this slice can tell you that.

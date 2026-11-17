# Brief — S16 Git & forge actions

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Inherits:** L3, L7 and the idempotency table from
[`docs/_archive/aeon-v1-horizontal/08-loop-execute`](../../_archive/aeon-v1-horizontal/08-loop-execute/plan.md). New decisions begin at **L20**.
**Stage:** 3 · Car, third of four. Depends on S8, S11, S13. **Blocks S17.**

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief overrides it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** **This is the first slice whose effects other people can see.** The rules below are stricter than earlier
> slices' for that reason, and §8's standing non-goals include a real repository.

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
| **D1** | **Every action run twice is a no-op the second time**, asserted on the **fake's call log** — not on a returned value. A test checking the return passes against code that acted twice and returned the same thing. |
| **D2** | **`nothing to commit` is a success.** An empty `git status` is not a failure; it means the work is already in. |
| **D3** | **The completeness assertion has no exclusion** (L24). Stage everything, then `git status --porcelain` must be empty. A stray file anywhere in the tree trips it. |
| **D4** | **`Closes #n` is proven not to fire**: a merge into a **feature** branch leaves the issue open, and `forge.closeIssue` closes it — asserted by **reading the issue back**, so a silent failure is caught. |
| **D5** | **The phantom-merge guard, both halves.** `base.ref != feature_branch` is refused **before** merging. After merging, the **squash SHA** is asserted an ancestor and the task branch's **original commits are asserted not to be** — a guard checking the originals reports every correct squash merge as a failure. |
| **D6** | **`existing-worktree` mode never creates and never deletes the feature branch**, and pushes it before the first task PR. |
| **D7** | **`forge.postReview` is the only receipt-first action**, and after a crash between post and record it reconciles **by id, never by body text**. Two reviews with identical bodies are two reviews. |
| **D8** | **Every action declares an effect class**, and `outward` and `irreversible` produce an `ask` (L22). An unattended loop **denies** them (G7, G14). |
| **D9** | **The governance gap is closed, asserted side by side**: a blueprint naming `forge.merge` and a `bash` step running `gh pr merge` both reach a human. This is the only test that would have caught the gap. |
| **D10** | **Template slots are validated at load** against the producing handler's handoff schema (L20). A slot the handler does not declare is `invalid` at load, not empty at render. |
| **D11** | **An unresolved slot fails the action** (L23). **No literal `{{slot}}` ever reaches a PR body.** |
| **D12** | **A stub `pr_summary` is refused** — empty, whitespace, or identical to the template's placeholder text. And a test **records that quality is not checked** (L21), so nobody later mistakes the stub check for a quality gate. |
| **D13** | **The registry has eight actions plus `audit`, and `render` is absent** (L25). |
| **D14** | **The registry is closed.** A blueprint cannot name code outside it, asserted by there being no mechanism to. |
| **D15** | **`dev-loop.yaml` validates with zero `unverifiable`** and passes `--strict` for the first time. S8's committed expected set is updated, **and the fixture diff is the evidence**. |
| **D16** | **S8's `RunCodeAction` refusal is retired**, and no code path still returns `blocked` naming S16. |
| **D17** | **A real pull request lands in a throwaway repository**, its body carrying one authored paragraph and correct identifiers throughout. |
| **D18** | **`make test` and `make lint` green**, `make fmt` leaves no diff. |

**D1, D9 and D5 are the three that matter.** D1 is the slice's central claim. D9 is the finding it exists to fix. D5
is the recorded incident most likely to recur, because the wrong implementation looks correct until a squash.

**D1–D17 must each exist as named tests**, D17 additionally as the demo's §1.

D18 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

Three things come due here. S8's `execute` refuses `RunCodeAction` naming this slice; `dev-loop.yaml` ships with nine
`unverifiable` code-action diagnostics; and S13's `git-loop` drop record dispositioned 372 lines into a list of actions
this slice must contain.

**And it is the first slice whose effects other people can see.** A pull request lands in a real repository and cannot
be un-seen by deleting a row.

**The gap worth stating plainly:** S11's evaluator guards `bash`; code actions do not go through it. The corpus shows
someone already decided merging deserves a human — `github-steward` marks `gh pr merge*` as `ask`, and `gh pr merge*`
carries `allow`, `ask` **and** `deny` across different agents. But those are command-line patterns. **A blueprint naming
`forge.merge` reaches the same effect through a path with no guard at all**, which would launder it past S11 entirely.

## 4. Goal

Eight idempotent code actions, governed by effect class, such that a loop branches, commits, and opens a real pull
request with a description a human would have written — and running it twice changes nothing.

## 5. Shape and the seam

```
blueprint ──▶ execute ──▶ action registry (CLOSED) ──▶ effect class
                                │                          │
                                │                    outward/irreversible → ask
                                │                          │
                         check-then-act ×8            git plumbing
                         receipt-first ×1  ──────────▶ Forge interface ──▶ GitHub
                                                            │
                                                          fake (built first, L26)
```

### The seams

**The `Forge` fake is built first** (L26, R4) because every later test depends on it, **and it must report a PR's
`base.ref`** — the phantom-merge guard is untestable without that one field.

**The registry is closed** (D14). That is what makes effect classes sufficient rather than a first line of defence: a
blueprint cannot name arbitrary code, so the classes govern a known set.

**Identifiers come from code; one paragraph comes from the model** (L3). Every branch name, issue number, PR number and
SHA is code's. That split removes an entire bug class, *including the recorded branch-inheritance bug*.

**Effect classes are code, not config** (deliberately): a config-defined class is a way to downgrade `irreversible` to
`local`.

## 6. Locked decisions

**Inherited:** L3 (code owns identifiers, prose stays a model concern), L7 (every action idempotent by check-then-act or
receipt-first), and the **idempotency table** — nine check-then-act, `forge.postReview` alone receipt-first.

**New:** L20 (slots validated at load), L21 (prose existence checked, quality not), L22 (effect classes; outward and
irreversible ask), L23 (unresolved slot fails), L24 (no staging exclusion), L25 (`render` retired; eight actions), L26
(the fake first, reporting `base.ref`).

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | `outward` and `irreversible` default to `ask` | The `ask` must carry **what will be affected**, not just the action name: the PR number, the branch, the base, the repository. S11's G18 established that a permission ask names the command *and* the rule; the same reasoning applies harder here, because *"approve forge.merge?"* tells a human nothing about what they are merging or where. |
| O2 | The registry is closed | Its closedness must be **asserted**, not merely true: a test proving no blueprint field, handler field, or config value can introduce an action. S8's I5 did this for tools and the same shape applies. |
| O3 | `forge.postReview` reconciles by id | It must also **bound the reconciliation**: listing every review on a PR with hundreds is a slow path at exactly the wrong moment. Reconcile within the attempt's window, and if the intent cannot be resolved, **fail naming it** rather than posting again. |
| O4 | A stub `pr_summary` is refused | The refusal is a **`reject`**, routed by the blueprint (S8's L4), not a hard failure. The handler can be asked again; a failed loop cannot. |
| O5 | The design does not say what happens when the forge is unreachable | **A `blocked` with the transport reason**, distinguishable from a refusal. "GitHub is down" and "GitHub said no" need different responses, and conflating them sends someone to check their permissions when their network is out. |
| O6 | Actions are idempotent | **Idempotency is asserted per action, not once for the registry.** A shared test over a loop of eight actions passes while one of them is subtly wrong, because the shared test can only check what all eight have in common. |
| O7 | The demo uses a throwaway repository | The **test suite must never require one.** Every automated test runs against the fake; the real repository appears only in the demo and the QA suite. A unit suite needing a network and a token is a suite people stop running. |
| O8 | `git.commitAndPush` checks the branch head | It must also verify the push **landed**, by re-reading the remote ref. A push that appears to succeed and did not is indistinguishable from success by exit code alone, and everything downstream assumes the commit is fetchable. |

## 8. Non-goals

- **N1 — No full ladder.** **S17.** Actions work here; phases, redo edges and round caps are next.
- **N2 — No second forge.** GitHub only. The typed interface keeps another possible.
- **N3 — No renderer.** Retired (L25).
- **N4 — No merge queues or protected-branch workarounds.** A repository that refuses the merge produces an honest
  failure.
- **N5 — No new tools.** The nine are complete.
- **N6 — No blueprint authoring.** `dev-loop.yaml` exists; S17 fills the ladder.
- **N7 — No effect-class configuration.** Code, deliberately.
- **N8 — No force-push, ever.** Not as an action, not as a fallback, not behind a flag. It is the one git operation
  whose damage is unbounded and unrecoverable from the loop's side.
- **N9 — No branch or tag deletion.** Cleanup is not this slice's, and v1's recorded incident is a cleanup that
  destroyed a Paseo-owned workspace.

### Standing non-goals

- **Never point a test at a real repository or a real token** (O7). The fake, always.
- **Never commit a token.** Redact to `<redacted>` in any capture.
- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never run `git clean`.**
- **Never delete the feature branch** in `existing-worktree` mode — it is a workspace Paseo owns (D6).

## 9. Repo invariants

**I1 — Every action is idempotent, asserted per action** (L7, D1, O6).

**I2 — The registry is closed, asserted** (D14, O2).

**I3 — Identifiers come from code, never from the model** (L3). Asserted: no action takes an identifier from tool
input or a handoff field.

**I4 — Effect classes are code** (L22, N7).

**I5 — No force-push and no branch deletion anywhere** (N8, N9), asserted by grep.

**I6 — The completeness assertion has no exclusion** (L24, D3).

**I7 — No automated test needs a network or a token** (O7).

**I8 — `render` does not exist** (L25, D13). S4's stale "(S11)" reference was corrected when this slice was specified;
do not re-edit it.

**I9 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/08-loop-execute/plan.md`** — the idempotency table with each action's **exact check**, the three
   constraints a naive implementation breaks, and the phantom-merge analysis. **This is the specification.**
2. **`internal/handler/drops/git-loop.md`** (S13's T9) — 372 lines dispositioned, with every range mapped to an action.
   **The `BECOMES A CODE ACTION` section is this slice's input**, and a range mapping to an action not planned here is a
   finding S13 was told to raise.
3. **`internal/loop/execute.go`** (S8's T10) — the `RunCodeAction` refusal this slice retires.
4. **`internal/policy/`** (S11) — the evaluator that guards `bash` and **not** these actions. L22 exists because of
   that asymmetry.
5. **`internal/handler/handoff.go`** (S13's T3) — where `pr_summary` is declared, and how a missing declared field
   becomes a `reject`.
6. **`opencode/agents/git-loop.md`** — 372 lines, read-only. The original whose behaviour this replaces.
7. **`paseo.json`** at the repository root, if present — `existing-worktree` mode's trigger.

### 10.2 The patterns to copy

- **S7's receipt table** — `forge.postReview`'s intent log is the same shape as the `bash` receipt, and the reasoning
  is identical: a record written before an effect, resolved after.
- **S11's `AllowToken`** as the model for making a guard structural rather than conventional.
- **S8's committed expected-diagnostics fixture**, whose diff is D15's evidence.
- **Discriminated results over errors** (S4's DB19) — a refusal, a transport failure and a domain conflict are three things.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | Does S13's `git-loop` drop record name any action **not** in the eight? | `drops/git-loop.md`. **If so, that is the finding it was written to surface** — resolve it before planning |
| Q2 | Is `paseo.json` present, and what exactly does `existing-worktree` mode change? | Repository root, and the superseded plan. D6 depends on it |
| Q3 | Does Aeon write **anything** into a working repository that must not be committed? | `internal/loop/`, the `produces:` paths. **L24 assumes not** — verify rather than inherit v1's exclusion |
| Q4 | How does S8's `execute` currently refuse a code action, and what replaces that branch? | `internal/loop/execute.go` |
| Q5 | Where does an `ask` originate today, and can a code action raise one through the same path? | `internal/tools/bash.go`, `internal/protocol/answer.go` (S11's T6). A **fourth** question kind, or a reuse of `permission`? |
| Q6 | Does S7's receipt table generalise to an action intent, or does `forge.postReview` need its own? | `internal/store/receipts.go`. Two tables holding intents would be two recovery paths |
| Q7 | What does the store hold for evidence — issue numbers, PR numbers, SHAs — and is there a place for it? | `internal/store/`. Every action returns evidence that lands in state and the transcript |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q8 | What are the template files, their slots, and which handler produces each slot (L20)? Produce the table before planning. |
| Q9 | How is a "stub" `pr_summary` detected, and what are its exact thresholds (L21, D12)? |
| Q10 | How is `forge.postReview`'s reconciliation bounded (O3)? |
| Q11 | How does the fake simulate a cross-host or unreachable forge, for O5? |

---

## 11. Behavioural contract

- Eight actions plus `audit`; no `render`; the registry closed and asserted (D13, D14, O2).
- Each idempotent, asserted **per action** on the fake's call log (D1, O6).
- `nothing to commit` succeeds; the push is verified by re-reading the remote ref (D2, O8).
- The completeness assertion has no exclusion (D3).
- `Closes #n` proven not to fire; `forge.closeIssue` reads back (D4).
- Phantom-merge guard, both halves, squash SHA only (D5).
- `existing-worktree` never creates or deletes the feature branch (D6).
- `forge.postReview` alone is receipt-first, reconciling by id, bounded, failing rather than reposting (D7, O3).
- Effect classes declared; `outward`/`irreversible` ask, carrying what is affected; unattended denies (D8, O1).
- Both governance routes reach a human, asserted side by side (D9).
- Slots validated at load; unresolved slots fail; no `{{slot}}` reaches a PR (D10, D11).
- A stub summary is a `reject`; quality is not checked and a test says so (D12, O4, L21).
- An unreachable forge is `blocked` with a transport reason, distinct from a refusal (O5).
- `dev-loop.yaml` validates clean and passes `--strict`; the fixture diff is the evidence (D15).
- No force-push, no branch deletion (N8, N9, I5).
- No automated test needs a network or a token (O7, I7).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **the `Forge` interface and its fake come first** (L26), and the fake reports `base.ref`. Every later task's
  tests depend on it.
- **R5** — **effect classes come before any `outward` action is implemented.** An action that works before it is
  governed is an action someone can call in the window between.
- **R6** — **`forge.merge` and the phantom-merge guard are one task.** The guard is not a check bolted beside the
  merge; it is what makes the merge correct.
- **R7** — **the real GitHub implementation is its own task, late**, and every other task's tests use the fake.
- **R8** — no task over roughly 300 changed lines or 12 files without justification.
- **R9** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure",
  "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md` and amends the QA suites.
- **R12** — **no task writes to `opencode/`** or edits `S4-store/design.md` (I8, already landed).

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines without justification (R8) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | Paste Q1's answer | **Any action in `git-loop`'s drop record but not in the eight is resolved before planning** |
| A6 | Paste Q3's answer | **L24 assumes Aeon writes nothing uncommittable.** Verify it; do not inherit v1's exclusion |
| A7 | Paste Q8's slot table — every template, every slot, the handler producing it | **A missing table blocks the plan.** L20 validates against it |
| A8 | Paste Q5's answer | Whether a code-action `ask` reuses `kind: 'permission'` or needs a fourth kind |
| A9 | Paste Q6's answer | Whether the intent log reuses S7's table. **Two intent tables are two recovery paths** |
| A10 | `grep -rn "force\|--force\|-f \|push -f\|branch -D\|branch -d"` over the plan | No force-push, no branch deletion (N8, N9) |
| A11 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task implements phases, redo edges or round caps | N1 |
| B2 | Confirm no task implements a second forge | N2 |
| B3 | Confirm no task implements a renderer | N3, I8 |
| B4 | Confirm no task works around a protected branch or merge queue | N4 |
| B5 | Confirm no task adds a tool | N5 |
| B6 | Confirm no task authors a blueprint | N6 |
| B7 | Confirm effect classes are not configurable | N7, I4 |
| B8 | **Confirm no task force-pushes or deletes a branch or tag** | N8, N9, I5 |
| B9 | Quote the `out_of_scope` lines covering N1–N9 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D17 to a task and a named test | §2 |
| C2 | For D1, confirm the assertion is on the **fake's call log**, per action, not a shared loop | D1, O6 |
| C3 | For D3, confirm a stray file **anywhere** trips it, with no exclusion | D3, L24 |
| C4 | For D4, confirm the assertion **reads the issue back** | D4 |
| C5 | For D5, confirm **both** halves, and that the original commits are asserted **not** ancestors | D5 |
| C6 | For D7, confirm reconciliation is by **id** and that identical bodies do not merge | D7 |
| C7 | For D9, confirm the test compares **both** routes in one assertion | D9 |
| C8 | For D11, confirm the assertion is that no `{{` reaches a rendered body | D11 |
| C9 | For D12, confirm a test **records** that quality is unchecked | D12, L21 |
| C10 | For D15, confirm the committed fixture is updated and the **diff** is asserted | D15 |
| C11 | Confirm **no** test requires a network or token | O7, I7 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm the `Forge` interface and fake precede every action | R4 |
| D2 | Confirm effect classes precede every `outward` action | R5 |
| D3 | Confirm `forge.merge` and the guard are one task | R6 |
| D4 | Confirm the GitHub implementation is late and separate | R7 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm no action takes an identifier from model output | I3, L3 |
| E2 | Confirm the registry's closedness is asserted | I2, O2 |
| E3 | Confirm effect classes have no config path | I4 |
| E4 | Confirm no force-push or deletion anywhere | I5 |
| E5 | Confirm `render` does not exist | I8 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | Paste the slot table (Q8) and confirm every slot's producer declares it (L20). |
| F2 | State the stub-detection thresholds (Q9, D12). |
| F3 | State how `postReview`'s reconciliation is bounded and what happens when it cannot resolve (Q10, O3). |
| F4 | State how the fake simulates an unreachable forge (Q11, O5). |
| F5 | State `existing-worktree` mode's exact behaviour (Q2, D6). |
| F6 | State where action evidence is stored (Q7). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it and fixes whatever does not match.

**It needs a throwaway GitHub repository and a token, and the automated suite must not** (O7). If a unit test grows a
network dependency, that is the finding.

**§2 is the slice's claim.** Run the whole loop twice: one issue, one PR, one commit. Check the repository, not the
logs — a loop that reports reuse and created a duplicate is the exact failure.

**§3 closes the gap.** Both `aeon policy check --agent github-steward "gh pr merge 15"` and `aeon action check
forge.merge` must return `ask`. If the code action does not, expressing an action as a code action rather than a command
launders it past S11's guard — and that asymmetry was invisible until it was written down.

**§4's second half is the one people get wrong.** After a squash merge, the **squash SHA** is an ancestor and the task
branch's **original commits are not**. Both results are correct. A guard checking the originals reports every successful
squash merge as a failure, and the fix looks like relaxing the guard.

---

**Amended 2026-11-04 by S60** — D8's closed action set gained five code actions
(`queue.enqueue` local/idempotent, `git.integrate` + `git.publishIntegration`
workspace-local, `forge.openLandingPR` outward, `forge.land` irreversible), and
`forge.land` consumes S57's merge-rule floor and re-binds head/verified/integration
shas or refuses `head-moved`; `forge.merge` and every G14 test stand unedited.
See `S60-merge-queue/tasks/T6.md`.

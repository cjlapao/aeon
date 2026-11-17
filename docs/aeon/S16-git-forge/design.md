# Design — S16 Git & forge actions (Stage 3 · Car)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 3 · Car, third of four
**Depends on:** S8 (blueprints, `execute`'s refusal), S11 (the guard), S13 (handlers and handoffs). **Blocks S17.**
**Inherits:** L3 and L7 and the idempotency table from
[`docs/_archive/aeon-v1-horizontal/08-loop-execute`](../../_archive/aeon-v1-horizontal/08-loop-execute/plan.md). New decisions start at **L20**.

---

## Context

S8's `execute` refuses `RunCodeAction` with `blocked` naming this slice. `dev-loop.yaml` ships with nine
`unverifiable` diagnostics, all of them code actions. S13's `git-loop` drop record dispositioned 372 lines into a list
of actions this slice must contain. **This is where all three come due.**

It is also the first slice in the plan where Aeon does something **other people can see.** Everything until now was
local: a file written, a step run, a window updated. A pull request lands in a real repository, possibly in front of a
real team, and it cannot be un-seen by deleting a row.

### The split is settled: code owns identifiers, prose stays a model concern

L3 is inherited and not re-litigated. Opening a pull request is **a mechanical call plus a writing task**, and
splitting them removes the identifier-integrity bug class — *including the recorded branch-inheritance bug* — while
keeping descriptions worth reading.

Measured against v1's `git-loop`, the authored surface is **one paragraph per PR plus the review body**. Everything
else is already a template.

### The gap this slice must close

**S11's evaluator guards `bash`. Code actions do not go through it.**

The corpus is explicit about intent: `github-steward` marks `gh pr merge*` and `gh pr close*` as **`ask`**, and
`gh pr merge*` carries `allow`, `ask` **and** `deny` across different agents. Someone thought carefully about who may
merge.

But those are *command-line patterns*. A blueprint naming `forge.merge` reaches the same effect through a path with
**no guard at all** — same outcome, two governance routes, and one of them empty. Expressing an action as a code
action rather than a command would silently launder it past the guard S11 built.

So every code action declares an **effect class**, and the outward-facing ones default to `ask` (L22):

| Class | Meaning | Default |
| --- | --- | --- |
| `local` | Effects confined to the working tree | run |
| `outward` | Visible to other people — a push, an issue, a PR, a review | **`ask`** |
| `irreversible` | Cannot be undone by another action — a merge | **`ask`** |

The registry being **closed** (L8) means this is not the privilege-escalation problem arbitrary `bash` has. Closed is
not the same as safe to run unattended, though: G7 and G14 then apply, so **an unattended loop cannot open a PR
unless someone opted in at start.** That is the correct default for a thing other people will see.

### Three constraints a naive implementation breaks

All three come from **recorded incidents**, which is why they are constraints rather than preferences.

**1 — The completeness assertion.** Stage everything, then assert `git status --porcelain` is **empty**. Never
hand-pick: the recorded incident is hand-picked files left behind and **dropped by the squash merge**. Code has no
excuse for hand-picking, so this is an assertion rather than advice.

**2 — `Closes #n` does not fire.** GitHub's keyword auto-closes only on a merge into the repository's **default**
branch. Task PRs target the *feature* branch, so the keyword never fires. `forge.closeIssue` is the only thing that
closes a task issue when its work lands, and it must **read the issue back** — a silently-failed close leaves the issue
open with nothing downstream noticing.

**3 — The phantom-merge guard.** Before merging, assert `base.ref == {feature_branch}`. After merging, assert
`git merge-base --is-ancestor {mergeSha} {feature_branch}` — and verify the **squash SHA**, never the task branch's
original commits, because a squash rewrites history and those are never ancestors even after a correct merge.

### Two things that turned out to be obsolete

**The renderer is retired.** S4 defers `renderAll` — writing `loop.md`, `events.jsonl` and `memory.md` — to "a separate
module (S11)". That reference is **stale**: in the vertical decomposition S11 is permissions, and the renderer was
never reassigned.

It should not be. S4 already says *"the mirror is disposable; the store is the source of truth"*, and S9 gives a live
app reading that store, with `aeon loop show` covering the terminal. A disposable markdown mirror's only remaining
audience is a tool that is neither — and none is in the alpha. So **`render` is not a code action** (L25), and the
registry is **eight actions, not ten**.

**The `.projects/loops` exclusion goes with it.** v1 staged `git add -A -- ':!.projects/loops'` because the loop's own
artefacts lived untracked inside the repository. Aeon's store lives at `$XDG_STATE_HOME/aeon/state.db`, **outside the
working tree**, and the only things Aeon writes into a repository are `produces:` artefacts — which **should** be
committed.

So the assertion loses its exclusion and becomes **stronger**: plain `git status --porcelain`, empty (L24). Verify it
rather than assuming — an exclusion carried forward out of habit would hide exactly the class of leftover it was
written to catch.

## Goal

Eight idempotent code actions, governed by effect class, such that a loop branches, commits, and opens a **real pull
request** with a description a human would have written — and running it twice changes nothing.

## Decisions

L3, L7 and the idempotency table are inherited. New:

| # | Decision | Why |
| --- | --- | --- |
| L20 | **Template slots are validated against the producing handler's handoff schema at load time** | S8's L9 says validation is loud and at load. Discovering `{{pr_summary}}` is empty while opening a real PR means a PR with a hole in it, in front of people |
| L21 | **Code checks that authored prose exists and is not a stub. It cannot check quality and does not pretend to** | A length floor and a not-the-template check are honest and cheap. A "quality score" would be a number nobody should trust guarding something outward-facing |
| L22 | **Every code action declares an effect class — `local`, `outward`, `irreversible` — and the last two default to `ask`** | Otherwise a code action launders the same effect past the guard S11 built for `bash`. The corpus shows someone already decided merging deserves a human |
| L23 | **An unresolved template slot fails the action.** It never renders a literal `{{slot}}` into outward-facing prose | A template engine's default is to emit the placeholder, and a PR body containing `{{pr_summary}}` is worse than no PR |
| L24 | **The completeness assertion has no exclusion**, because Aeon writes nothing uncommittable into a repository | v1's `':!.projects/loops'` was for artefacts Aeon does not create. Carrying it forward would hide the leftovers the assertion exists to catch |
| L25 | **`render` is retired; the registry is eight actions.** `audit` survives as an optional pre-merge check | S4 calls the mirror disposable, and S9 plus `aeon loop show` cover both audiences. S4's "(S11)" reference for the renderer is stale and is corrected |
| L26 | **The `Forge` fake is built before any action that uses it**, and it reports a PR's `base.ref` | Every later test depends on it, and the phantom-merge guard cannot be tested without that one field |

## Architecture

```
aeon/internal/forge/
  forge.go        the typed interface
  fake.go         the fake — built first (L26), reports base.ref
  github.go       the GitHub implementation

aeon/internal/git/
  plumbing.go     branch, stage, commit, push, merge-base
  worktree.go     existing-worktree mode

aeon/internal/action/
  registry.go     eight actions, closed and typed
  class.go        effect classes (L22)
  template.go     slots, load-time validation (L20), unresolved-slot failure (L23)
  git_actions.go  createBranch, commitAndPush, mergeBranch
  forge_actions.go openIssue, openPR, merge, postReview, closeIssue
  audit.go
```

### The eight actions and their strategies

Nine of v1's ten were check-then-act; one needs receipts. **That one exception is the point** — a plan applying one
strategy uniformly gets it wrong in exactly one place.

| Action | Class | Strategy | The check |
| --- | --- | --- | --- |
| `git.createBranch` | `local` | check-then-act | `show-ref --verify` local, `rev-parse --verify origin/…` remote. Absent → create; present → **reuse** |
| `git.commitAndPush` | **`outward`** | check-then-act | `git status --porcelain` **empty ⇒ nothing to commit, which is success**. Pushing an already-current ref is a git no-op |
| `git.mergeBranch` | `local` | check-then-act | `merge-base --is-ancestor {feature} {base}` exits 0 ⇒ already merged |
| `forge.openIssue` | **`outward`** | check-then-act | Search **open** issues for the exact title with the `dev-loop` label |
| `forge.openPR` | **`outward`** | check-then-act | An **open** PR whose `head` is the task branch. Also reuse an existing task branch rather than recreating |
| `forge.postReview` | **`outward`** | **receipt-first** | A review has **no natural business key** — same body, same verdict, two reviews. Intent keyed `(pr, step, attempt)`, post, record the id. On resume, an intent with no id means list reviews and reconcile **by id, never by body text** |
| `forge.merge` | **`irreversible`** | check-then-act | Read the PR; `merged == true` ⇒ return the recorded SHA. **Never merge twice, never fail** |
| `forge.closeIssue` | **`outward`** | check-then-act | Read the issue; `state == "closed"` ⇒ done. And it must **read back** |
| `audit` | `local` | naturally idempotent | Read-only over the store |

`git.commitAndPush` is worth naming: **a commit is idempotent by content only if you check the branch head first**,
because a second commit carrying the same message is not the same thing as no commit.

### The prose boundary (L3, L20, L21)

```
handler's handoff ──▶ pr_summary (authored)  ─┐
                                              ├──▶ template ──▶ PR body
manifest + evidence (numbers, refs, SHAs) ───┘        │
                                                  slots validated
                                                  at LOAD (L20)
```

Every identifier — branch name, issue number, PR number, SHA — comes from **code**. The one authored paragraph comes
from the upstream step's handoff, which S13's T3 made a declared field with a stated purpose, so a handler that
promises it and does not produce it is already a `reject`.

## The demo

See [`DEMO.md`](DEMO.md): a loop branches, commits, opens a **real pull request** against a throwaway GitHub
repository, and the body reads like a person wrote it; running the whole loop again **changes nothing** — no second
issue, no second PR, no second commit; `forge.merge` is **asked** before it merges and refused in an unattended loop;
the phantom-merge guard rejects a mis-based PR; and `kill -9` between "PR opened" and "PR recorded" recovers without
opening a second one. **§9 adds the app**: the Test-bench Demos panel runs the reads (validate, the ask rendered
verbatim, refusals as data, labeled replay) and **never answers an ask** — an approve button on a demo tab is the
one thing effect classes exist to prevent.

## Testing

| What | How |
| --- | --- |
| The fake reports `base.ref` | Built first (L26). The phantom-merge guard is untestable without it |
| Every action, twice | Each of the eight run twice against the fake: **the second is a no-op**, asserted on the fake's call log rather than on a returned value |
| Nothing to commit is success | An empty `git status` yields success, **not** failure — the case a naive implementation treats as an error |
| The completeness assertion | Files left unstaged **fail**. And **no exclusion** (L24): a stray file anywhere in the tree trips it |
| `Closes #n` does not fire | Against the fake: a merge into a **feature** branch leaves the issue open, and `forge.closeIssue` is what closes it. **Asserted by reading the issue back** |
| Phantom merge | A PR whose `base.ref` is not the feature branch is **refused before merging**; after a merge, the **squash SHA** is asserted an ancestor and the original commits are **not** |
| Worktree mode | `createBranch` in `existing-worktree` mode **never creates**, and nothing ever deletes the feature branch |
| Receipt-first, once | `forge.postReview` after a crash between post and record: reconciled **by id**, never by body text, and no second review |
| Effect classes | Each action's class asserted; `outward` and `irreversible` produce an `ask`; an unattended loop **denies** them (L22, G7, G14) |
| The governance gap is closed | A blueprint naming `forge.merge` and a `bash` step running `gh pr merge` both reach a human. **Asserted side by side**, because the gap was invisible until stated |
| Template slots at load | A template naming a slot the producing handler does not declare is **`invalid` at load**, not empty at render (L20) |
| Unresolved slot at render | Fails the action; **no literal `{{slot}}` reaches a PR body** (L23) |
| Stub prose | An empty or template-identical `pr_summary` is refused. And a test recording that **quality is not checked** (L21) |
| Eight, not ten | The registry has eight; `render` is absent (L25) |

Two rows carry the most weight. **"Every action, twice"** is the demo's claim and its assertion must be on the fake's
call log — a test checking the returned value passes against code that acted twice and returned the same thing. **"The
governance gap is closed"** is the finding this slice exists to fix, and it is the only test that would have caught it.

## Out of scope

- **No full ladder.** **S17.** This slice makes the actions work; S17 sequences them into phases with redo edges.
- **No second forge.** GitHub only. The typed interface is what keeps a second one possible, and *"a typed interface
  with per-forge implementations beats discovering a tool by function and hoping"*.
- **No `render`.** Retired (L25).
- **No new tools.** The nine are complete.
- **No blueprint authoring.** `dev-loop.yaml` exists (S8) and S17 fills the ladder.
- **No policy authoring UI.** Effect classes are code, not config — deliberately, since a config-defined class is a
  way to downgrade `irreversible` to `local`.
- **No merge-queue or protected-branch handling.** A repository that refuses the merge produces an honest failure.

## A finding from deriving the real protocols: two forge identities

`conversational-map` is explicit about something this design assumes away, and its reason is concrete:

> *"`code-reviewer` submits its verdict via the **`github-reviewer_*` MCP** (reviewer identity) … Because the reviewer account
> differs from the steward's author account, **the approval actually counts against branch protection.**"*

**`Forge` here is one interface, one implementation, one token** (T1, T11). So a loop that opens a PR and approves it does so
under one identity, and **a self-approved pull request does not satisfy branch protection** — the approval is recorded and
counts for nothing, and nothing in the current design would say so.

That interacts with S17's merge gate (L34), which requires *"the latest review state is `APPROVED`"*. On a protected branch
that state would exist and the merge would still be refused, producing a failure whose cause is two commits upstream of where
it appears.

*Extended 2026-10-30 by S57 VG: the gate is now `mergerule.Evaluate` over live facts; baseline unchanged. See S57-verification-gates/design.md.*

**Not resolved here**, because it is a real design question rather than an oversight: two tokens means two credentials (C17's
rule applies to both), a per-action identity mapping, and a decision about what happens when only one is configured. Recorded
so it is a decision someone makes rather than a surprise someone debugs — and noted in T11, which currently reads one token
from one place.

## Amendments to earlier slices

| Where | Change | Status |
| --- | --- | --- |
| `S4-store/design.md` | Its renderer note defers to "a separate module (S11)", which in this decomposition is permissions. **Corrected: the renderer is retired** (L25), with S4's own "the mirror is disposable" as the reason | **landed with this design** |
| S8 | `execute`'s `RunCodeAction` refusal is retired; `dev-loop.yaml`'s nine `unverifiable` code-action diagnostics become real checks, and the committed expected set is updated — **that diff is the evidence** | T9 |
| S13 | Handlers whose output reaches a PR body declare `pr_summary` (L3). S13's T3 built the mechanism; this adds the field | T8 |

## Open questions

None. Whether a repository's branch protection blocks `forge.merge` is a property of the repository, and the honest
answer is a failure naming it.

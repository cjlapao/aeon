---
slice: S16-git-forge
title: Git & forge actions
stage: 3
stage_name: Car
position: 3 of 4
brief: docs/aeon/S16-git-forge/brief.md
design: docs/aeon/S16-git-forge/design.md
demo: docs/aeon/S16-git-forge/DEMO.md
depends_on: [S8-blueprints, S11-permissions, S13-agents]
blocks: [S17-full-ladder]
task_count: 13
---

# Plan — S16 Git & forge actions

**The first slice whose effects other people can see.** Everything until now was local; a pull request lands in a real
repository and cannot be un-seen by deleting a row.

Three things come due: S8's `RunCodeAction` refusal, `dev-loop.yaml`'s nine `unverifiable` diagnostics, and S13's
`git-loop` drop record — 372 dispositioned lines naming the actions this slice must contain.

## Task sequence

| Task | Deliverable | Gate |
| --- | --- | --- |
| **T1** | The `Forge` interface and its fake | The fake reports `base.ref`; no test needs a network |
| **T2** | Effect classes, and the `ask` path for code actions | Both governance routes reach a human, side by side |
| **T3** | Git plumbing: worktree mode, staging, the completeness assertion | A stray file anywhere trips it; nothing deletes the feature branch |
| **T4** | `git.createBranch`, `git.commitAndPush`, `git.mergeBranch` | `nothing to commit` succeeds; the push is verified by re-reading the ref |
| **T5** | `forge.openIssue`, `forge.openPR` | Each run twice is a no-op, on the fake's call log |
| **T6** | `forge.merge` **and** the phantom-merge guard | Squash SHA an ancestor; original commits not |
| **T7** | `forge.postReview` (receipt-first) and `forge.closeIssue` | Reconciled by id; the close reads back |
| **T8** | Templates: slots, load-time validation, unresolved-slot failure | No `{{slot}}` reaches a body |
| **T9** | `pr_summary` on the handlers that reach a PR, and the stub checks | A stub is a `reject`; quality is unchecked and a test says so |
| **T10** | The registry, wiring, S8's refusal retired, validation flipped | `dev-loop.yaml` passes `--strict`; the fixture diff is the evidence |
| **T11** | The real GitHub implementation | Every other task's tests still use the fake |
| **T12** | Demo walk, QA suites | A real PR lands; running twice changes nothing |
| **T13** | The demo runs from the app — the S16 panel *(added 2026-10-25, same amendment as S15's T10)* | `npx vitest run test/bench-gitforge-s16.test.tsx` — red first |

## Ordering rationale

- **T1 first** (R4, L26). Every later task's tests depend on the fake, and **it must report `base.ref`** — the
  phantom-merge guard is untestable without that one field.
- **T2 second** (R5), before any `outward` action exists. An action that works before it is governed is an action
  someone can call in the window between, and this slice's actions are visible to other people.
- **T3 before T4** because the completeness assertion and worktree mode are the constraints the git actions must obey,
  and building the actions first means retrofitting them.
- **T4, T5 independent** of each other once T1–T3 land.
- **T6 as one task** (R6). The guard is not a check bolted beside the merge; it is what makes the merge correct, and
  splitting them creates a window where the merge works and the guard does not.
- **T7 after T5** because `forge.closeIssue` closes what `forge.openIssue` opened, and `postReview` is the one
  receipt-first action — worth doing when the eight-way pattern is already established, so the exception stands out.
- **T8 before T9** — slots must exist before a handler declares one, or the load-time validation has nothing to check
  against.
- **T10 after every action** — the serialization point for the registry, and where S8's expectations flip.
- **T11 late** (R7), so a network dependency cannot leak into earlier tests.
- **T12 last of the Go tasks**, and the only one needing a real repository.
- **T13 after T12** (the 2026-10-25 amendment, S14/S15 precedent): the panel's replay rows cite the walked
  DEMO and the shipped Go tests, so the walk must have happened; the live half reads only what the protocol
  table already carries (`blueprint.validate`, `loop.show`, `question.asked`) and **never answers** —
  `question.answer` on this panel would be an approve button in disguise, and approving an `irreversible`
  effect from a demo tab is exactly what effect classes exist to require a human for.

## Files

| File | Tasks | Reason for overlap |
| --- | --- | --- |
| `internal/forge/forge.go`, `fake.go` | T1 | — |
| `internal/forge/github.go` | T11 | — |
| `internal/action/class.go` | T2 | — |
| `internal/protocol/answer.go` | T2 | Sole change: resolving a code-action `ask` |
| `internal/git/plumbing.go`, `worktree.go` | T3 | — |
| `internal/action/git_actions.go` | T4 | — |
| `internal/action/forge_actions.go` | T5, T6, T7 | Disjoint action sets in one file; each task adds its own functions |
| `internal/action/template.go` | T8 | — |
| `internal/blueprint/validate.go` | T8, T10 | T8 adds slot validation; T10 flips the code-action reserve. Different check sets |
| `internal/handler/bodies/*.md`, `handoff.go` | T9 | `pr_summary` declared where it is produced |
| `internal/action/registry.go` | T10 | Serialization point |
| `internal/loop/execute.go` | T10 | Sole change: the refusal retired |
| `internal/store/` | T7 | Only if Q6 finds the intent log needs its own table |
| `docs/test_suites/aeon-loop.md` | T12, T13 | Amended, never rewritten; T13 appends the panel contract after T12's cases |
| `aeon/ui/src/features/test-bench/pages/DemosPage.tsx`, `aeon/ui/test/bench-gitforge-s16.test.tsx` | T13 | Additive hunks to the Demos tab (sixth panel); the test file is new |

## The eight actions

| Action | Class | Strategy |
| --- | --- | --- |
| `git.createBranch` | `local` | check-then-act |
| `git.commitAndPush` | `outward` | check-then-act |
| `git.mergeBranch` | `local` | check-then-act |
| `forge.openIssue` | `outward` | check-then-act |
| `forge.openPR` | `outward` | check-then-act |
| `forge.postReview` | `outward` | **receipt-first** — the only one |
| `forge.merge` | **`irreversible`** | check-then-act |
| `forge.closeIssue` | `outward` | check-then-act |
| `audit` | `local` | naturally idempotent |

**`render` is absent** (L25). S4's deferral of it to "a separate module (S11)" was a stale reference; the correction is
already landed.

## What this slice does not do

Phases, redo edges, round caps (S17) · a second forge · a renderer (retired) · merge-queue or protected-branch
workarounds · new tools · blueprint authoring · configurable effect classes · **force-push or branch deletion, ever**.

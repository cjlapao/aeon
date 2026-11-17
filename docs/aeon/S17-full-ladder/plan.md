---
slice: S17-full-ladder
title: The full ladder
stage: 3
stage_name: Car
position: 4 of 4 — the car
brief: docs/aeon/S17-full-ladder/brief.md
design: docs/aeon/S17-full-ladder/design.md
demo: docs/aeon/S17-full-ladder/DEMO.md
depends_on: [S8-blueprints, S11-permissions, S13-agents, S16-git-forge]
task_count: 10
---

# Plan — S17 The full ladder

**Almost no new mechanism.** Lanes, handlers, code actions, the guard and the transition machine all exist. This slice
supplies the **method** — the sequence, the gates, the round caps and the send-backs — and its risk sits in two places:
the arithmetic of counters, and the ordering of tasks that depend on each other.

Nine tasks, and one of them is authorship rather than code.

## Task sequence

| Task | Deliverable | Gate |
| --- | --- | --- |
| **T1** | The plan loader: `depends_on` validation and a **stable** topological sort | Four bad-`depends_on` classes refused at load; a cycle named |
| **T2** | Two-level counters and their caps | A five-task loop: task 3 keeps its own budget |
| **T3** | Redo routing that carries the reason and the attempt number | A handler spy reads the reason in its own input |
| **T4** | `escalate` / `fail-item` / `block`, and the unattended collapse | The block distinguishes cap-reached from nobody-to-ask |
| **T5** | Phases: lane → phase state; per-item state for iterating lanes | No single `build` phase status |
| **T6** | `verify`'s three outcomes, including the handler amendment | `unverified` is never stored as `pass` |
| **T7** | The ladder's authorship — `dev-loop.yaml` every lane running | Every lane runs in order with one gate |
| **T8** | The review-state merge gate and the deferred-findings backlog | A thread-less forge still reaches the gate |
| **T9** | Demo walk against a real repository, QA suites | The deliberate defect is caught |
| **T10** | The demo runs from the app — the S17 panel *(added 2026-10-26)* | `npx vitest run test/bench-s17-ladder.test.tsx — red first` |

## Ordering rationale

- **T1 first** (R4). Everything downstream iterates over its output, and adding a sort later means retrofitting S8's
  walk. It is also where four classes of bad plan get named against the plan rather than surfacing as a task that never
  completes.
- **T2 second** (R5), because the caps are what bound everything after them. It carries D2, and **a per-loop-only
  implementation passes every other test in this slice** — which is why it is isolated rather than folded into T3.
- **T3 after T2** — a redo is only meaningful once there is a counter to consume.
- **T4 after T3** — exhaustion is what a counter reaching its cap produces.
- **T5 independent** of T2–T4; it could run in parallel.
- **T6 as one task** (R6). `unverified` touches the handler, the handoff, the routing and the record — spread across
  tasks, the "never stored as `pass`" assertion has no home.
- **T7 after the machinery** (R7). Authorship and arithmetic are different kinds of work, and mixing them means
  reviewing prose and counter logic in one diff.
- **T8 after T7** because the merge gate is the last step of a lane that has to run first.
- **T9** is the last task needing a real repository.
- **T10 last** (added 2026-10-26, the user's "make sure the demo is also in the app"). The bench panel joins the slice before it starts, the way S16's T13 did: it is a reader of T2's counters and T6's stored outcomes, so it follows the suite it replays — and the standing no-answer contract means it corrupts nothing it reads.

## Files

| File | Tasks | Reason for overlap |
| --- | --- | --- |
| `internal/plan/derive.go`, `sort.go` | T1 | — |
| `internal/loop/counters.go` | T2 | — |
| `internal/store/schema.go`, `migrate.go` | T2 | Only if Q1 finds the counters need columns |
| `internal/loop/redo.go` | T3 | — |
| `internal/runtime/compile.go` | T3 | Sole change: the reason and attempt number reach a step's input |
| `internal/loop/escalate.go` | T4 | — |
| `internal/loop/phases.go` | T5 | — |
| `internal/handler/bodies/verifier-engineer.md`, `handoff.go` | T6 | The `unverified` outcome |
| `internal/loop/decide.go` | T3, T6 | T3 routes on the reason; T6 adds the third outcome's routing. Both touch reject handling — split by what they add |
| `internal/blueprint/blueprints/dev-loop.yaml` | T7 | Authorship; the serialization point for the ladder |
| `internal/action/forge_actions.go` | T8 | Sole change: the merge gate records which review state it read |
| `docs/unresolved-pr-comments.md` (template) | T8 | The append-only backlog's shape |
| `docs/test_suites/aeon-loop.md` | T9, T10 | Amended, never rewritten; T10 appends only the panel-contract case |
| `aeon/ui/test/bench-s17-ladder.test.tsx`, `aeon/ui/src/features/test-bench/pages/DemosPage.tsx` | T10 | — |

## What this slice does not do

**No new mechanism** — a gap discovered here is a finding about S8–S16, not something to build · no new tools or code
actions · no second protocol as a blueprint · **no dogfooding** · no cleanup or branch deletion · no retrieval · no
blueprint builder · **and no gate relaxed to make the demo pass**.

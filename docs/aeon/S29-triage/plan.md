# Plan — S29 Triage

**Brief:** [`brief.md`](brief.md). **Design:** [`design.md`](design.md). **Demo:** [`DEMO.md`](DEMO.md).

## What is settled

Everything in `design.md`'s §2 (T1–T3 there, numbered separately from this plan's tasks — read the design
before this plan to avoid confusing the two). In particular: validation state and priority are two separate
closed sets, not one crossed enum (and priority's shape is copied verbatim from `S26-roadmap`'s own
`priority_state`/`priority_label` columns); a validation attempt dispatches automatically on filing, no
project gate, because G14 already bounds what an unattended attempt may do; the hand-off at D8 targets
**S26 only**, through S26's ordinary `idea` entry point, requiring zero changes to S26 itself.

## How we will do it — most of this slice is free, two tasks have real chains behind them

**T1–T4 and T7–T8 need only S4, S5 and S13 — all built.** The bug table, the status machine, the
`file_bug` tool and automatic dispatch, duplicate detection, the CLI/socket surface and the UI are all
buildable today with nothing further to wait on.

**T5 (the S28 learning-feed hook, D10) needs `S28-memory`'s learnings core to exist as real code** — not
all of S28 (its board half, T7 there, stays irrelevant to this hook), just `internal/learnings`'s `Append`
(S28's own T1/T2). S28's learnings core is itself fully specced and buildable now, so this is a
"build-order," not a "waiting on an unbuilt slice" dependency — the same shape `S26-roadmap` has onto
`S24-loop-queue`.

**T6 (the hand-off to S26, D8) needs `S26-roadmap`'s core (`internal/roadmap`, its own T1/T2) to exist.**
S26 in turn needs S24 and S25 built first (`S26-roadmap/design.md`'s own finding). This is the deepest
chain in this plan: **T6 → S26's core → S24 and S25**. None of the three is blocked on anything unbuilt in
the "waiting on a slice with no spec" sense S27 or S28's T7 are — it is purely a matter of build order.

## Task sequence

| Task | What | Depends on | Needs real code from |
| --- | --- | --- | --- |
| **T1** | Migration: `bug`, `bug_vec` (the second `vec0` table in this schema, after S28's `learning_vec`). | — | — |
| **T2** | `internal/triage`: `Bug`, the validation-state and priority-state types, `Create`, status transitions (`unvalidated → {reproduced, not-reproduced, could-not-tell}`, none of which transition back). | T1 | — |
| **T3** | `file_bug` native tool (known shape, never accepting `validationState`/`priorityState` from the caller) and automatic validation-attempt dispatch — an ordinary S5 loop, G14-pinned. | T2 | — |
| **T4** | Duplicate detection: `bug_vec` population, project-scoped query, suggest-only (D5) — no code path may set `duplicate_of` without a human-originated call. | T2 | — |
| **T5** | S28 learning-feed hook (D10): a validation attempt's conclusion files as an `observed`, project-scoped `S28-memory` learning, automatic. | T3 | `S28-memory` T1/T2 (`internal/learnings`) |
| **T6** | S26 hand-off (D8): once validated and prioritized, create an `idea`-status `S26-roadmap` item, `provenance: 'triaged'`, and kick off its ideation (which transcribes the bug report into `brief.md` rather than holding a real conversation). | T2 | `S26-roadmap` T1/T2 (`internal/roadmap`) |
| **T7** | `aeon triage` CLI and `triage.*` socket methods (file, list, show, set-priority, confirm-duplicate). | T2, T3, T4 | — |
| **T8** | Test-bench UI: a triage page — validation state and priority rendered as separate, honest fields (never one combined badge, R4's guard). | T7 | — |
| **T9** | Demo: append this slice's section to `docs/aeon/DEMO.md`. T1–T4/T7–T8 walked for real; T5/T6 predicted until their real-code dependencies land, clearly labeled. | T1–T8 | — |

## Files this plan expects to touch or create

| File | Task |
| --- | --- |
| `internal/store/schema.go`, `schema_test.go` | T1 |
| `internal/triage/*.go` (new package) | T2, T4 |
| `internal/tools/file_bug.go`, `internal/daemon/triage_dispatch.go` | T3 |
| `internal/daemon/triage_learning_feed.go` | T5 |
| `internal/daemon/triage_handoff.go` | T6 |
| `cmd/aeon/triage.go`, `internal/protocol/triage.go` | T7 |
| `ui/src/features/test-bench/pages/TriagePage.tsx`, `routes.ts` | T8 |
| `docs/aeon/DEMO.md` (§S29 appended) | T9 |

## Ordering rationale

T1 → T2 is the one hard dependency most other tasks share. T3 and T4 are both independent of each other
once T2 exists. T5 and T6 are also independent of each other, each gated only by its own cross-slice
real-code dependency, not by one another. T7 depends on T2–T4 (the surface needs the mechanisms it
exposes). T8 depends on T7. T9 is last, and — like S28's own T8 — is explicitly allowed to be partial: its
Notes block should say precisely which of T5/T6 it could and could not walk for real.

## What this slice does not do

Everything in `brief.md` §6 (N1–N5) stands. No second fix-dispatch mechanism, no cross-project duplicate
detection, no auto-closing a bug as a duplicate under any threshold, no new embedding mechanism, no change
to S22's or S28's schema or invariants.

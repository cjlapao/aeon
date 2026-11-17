---
slice: S7-recovery
title: Crash recovery
stage: 2
stage_name: Bicycle
position: 2 of 4
brief: docs/aeon/S7-recovery/brief.md
design: docs/aeon/S7-recovery/design.md
demo: docs/aeon/S7-recovery/DEMO.md
depends_on: [S1-bootstrap, S2-protocol, S3-model-layer, S4-store, S5-one-step, S6-suspend]
independent_of: [S8-blueprints]
task_count: 8
---

# Plan — S7 Crash recovery

**Almost all of this slice already exists.** S6 built resume-cold; a crash is the same problem through a different
door. What is new is one sharp problem: a crash lands **anywhere**, and a `tool_use` with no `tool_result` looks
identical whether the tool never started, finished successfully, or died halfway.

Three states, one appearance. The receipt is what tells them apart.

## Task sequence

| Task | Deliverable | Gate |
| --- | --- | --- |
| **T1** | The `tool_receipt` table and `arg_digest` | Migration applies; fresh-equals-migrated holds; the unknown state is representable |
| **T2** | `RerunClass` on every tool, incl. the six absent, against a committed list | `bash` is the only `unsafe`; a classless tool fails a test |
| **T3** | Receipt writing around an unsafe tool, through the narrow writer | A receipt exists **before** the spawn, asserted on ordering |
| **T4** | `DetermineCase` — **pure** | Every case × every receipt state, no crash staged |
| **T5** | `RecoverStep` — the per-case actions | D2–D8 |
| **T6** | The daemon scan — automatic, observable, idempotent, `--no-recover` | D11, D13, D14 |
| **T7** | Protocol + CLI surfaces: recovery events, `aeon debug receipts`, the failure reason | A subscriber sees recovery; the reason names the tool |
| **T8** | Walk `DEMO.md` with real `kill -9`; amend the QA suites | Every §, including §4 and §5 |

**T4 is the one that matters.** It is pure, so its test needs nothing staged, and every task after it depends on
it being right. **T1 before T3 before T4** — the table, then the writing, then the reading.

## Ordering rationale

- **T1 first** because T3 has nowhere to write without it. The document corrections that used to sit here are
  already landed in the design (I2) — no task touches `ARCHITECTURE.md` or S4's documents.
- **T2 before T5** because Case C dispatches on the class.
- **T3 before T4** because `DetermineCase` reads receipts, and reading a shape nothing writes is how a mismatch
  between writer and reader survives to the demo.
- **T4 before T5** for the obvious reason, and because T4's exhaustive table is the specification T5 implements.
- **T6 after T5** because a scan with nothing to call is not testable.
- **T7 after T6** because the events describe what the scan does.
- **T8 last**, and it is the only task that kills a process.

## Files

| File | Tasks | Reason for overlap |
| --- | --- | --- |
| `internal/store/migrate.go` | T1 | — |
| `internal/store/receipts.go` | T1 | — |
| `internal/tools/*.go` | T2 | — |
| `internal/tools/class.go` | T2 | — |
| `internal/runtime/receipt.go` | T3 | — |
| `internal/runtime/loop.go` | T3 | Sole change: the receipt call around an unsafe tool |
| `internal/runtime/recover.go` | T4, T5 | T4 adds `DetermineCase` (pure), T5 adds `RecoverStep`. Split by purity — T4's file has no imports beyond the model |
| `internal/daemon/recover.go` | T6 | — |
| `internal/daemon/serve.go` | T6 | Sole change: the scan hook + `--no-recover` |
| `internal/protocol/events.go` | T7 | — |
| `cmd/aeon/debug.go` | T7 | — |
| `docs/test_suites/aeon-runtime.md`, `aeon-tools.md` | T8 | Amended, never rewritten |

## What this slice does not do

Redo or automatic retry (S8) · a bash policy (S11) · `(pid, host)` reconciliation (never — P29) · new tools (S11)
· the app (S9) · any change to compaction or `ResumeStep`.

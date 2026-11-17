---
slice: S11-permissions
title: Permissions & the nine tools
stage: 3
stage_name: Car
position: 1 of 4
brief: docs/aeon/S11-permissions/brief.md
design: docs/aeon/S11-permissions/design.md
demo: docs/aeon/S11-permissions/DEMO.md
depends_on: [S1-bootstrap, S2-protocol, S3-model-layer, S4-store, S5-one-step, S6-suspend, S7-recovery, S8-blueprints, S9-app]
blocks: [S13-agents]
task_count: 13
---

# Plan — S11 Permissions & the nine tools

**The slice has two halves that meet at one place.** The guard (T1–T6) and the tools (T7–T12). They are sequenced so
the guard is complete before `bash` is rewired (R4), because a half-built evaluator behind a live `bash` is the one
state where an over-permit ships.

The policy is already written, in this repository's own agent definitions: **38 of 75** files, **366 rules**, **50
distinct patterns**. The corpus **is** the specification.

## Task sequence

### The guard

| Task | Deliverable | Gate |
| --- | --- | --- |
| **T1** | The corpus extractor — reads `opencode/agents/*.md`, emits committed generated data | Properties asserted, never counts |
| **T2** | The matcher — `*` anywhere, exact without | `gh pr merge` denied against a bare `gh` rule |
| **T3** | Segment splitting, incl. newlines, and the documented limit | Any denied segment denies all; the limit is a documented expectation |
| **T4** | `Evaluate` — pure, total, first-match, verdict carries rule **and agent** | A fuzz seed never panics |
| **T5** | The `AllowToken` and the verdict→receipt→spawn ordering | `spawn` cannot be called without a token, at compile time |
| **T6** | `ask` resolution: attendedness declared and pinned; the question payload | Unattended denies with no question; a client connecting changes nothing |

### The tools

| Task | Deliverable | Gate |
| --- | --- | --- |
| **T7** | The one path-enforcement point | A symlink escaping the root is refused |
| **T8** | `write` and `edit` | `edit` fails loudly on a stale string |
| **T9** | `glob` and `grep` | Path-enforced through the same check |
| **T10** | `webfetch` | No credential header, ever; cross-host redirects explicit |
| **T11** | `todo` | In the transcript, never in the store |
| **T12** | Policy on handlers, the allow-list, `--allow-bash` retired, all nine classified | The stderr warning is gone; the class list has no pending entries |
| **T13** | Demo walk, QA suites | §1's rule on screen; §2's command proven never to have spawned |

## Ordering rationale

- **T1 first** (R5) because the matcher's tests are enumerated from the corpus, so the corpus must exist to write
  them against.
- **T2 alone** (R6) — the smallest, most-tested, highest-consequence code in the slice. D1's exact-match trap lives
  here and a matcher that gets it wrong passes almost every other test.
- **T3 after T2** because a segment is matched by the matcher; building splitting first means testing it against a
  matcher that does not exist.
- **T4 after both** — `Evaluate` composes them, and it is where purity and totality are asserted.
- **T5 after T4** (R7) because the token is produced *by* `Evaluate`, and the ordering it enforces is only
  meaningful once there is a verdict to order against.
- **T6 after T5** — `ask` resolution is the third branch of the same dispatch.
- **T7 before T8 and T9** because both pairs call it, and building it twice is exactly what W18 forbids.
- **T8 and T9 are independent** of each other and of T10 and T11.
- **T12 after every tool** — it is the wiring task and the serialization point for the registry.
- **T13 last.**

**T1–T6 must all be complete before T12 rewires `bash`.** That is R4, and it is the ordering constraint with a real
consequence rather than a tidiness one.

## Files

| File | Tasks | Reason for overlap |
| --- | --- | --- |
| `tools/corpusgen/main.go` | T1 | Reads `opencode/`, never writes it |
| `internal/policy/corpus.go`, `testdata/corpus.json` | T1 | Committed generated output, CI diff-checked |
| `internal/policy/pattern.go` | T2 | — |
| `internal/policy/segment.go` | T3 | — |
| `internal/policy/evaluate.go` | T4 | — |
| `internal/policy/token.go` | T5 | — |
| `internal/tools/bash.go` | T5, T12 | T5 gates the spawn and orders the receipt; T12 wires the handler's policy in. Split by concern — T5 has no registry knowledge |
| `internal/loop/attended.go`, `pin.go` | T6 | Attendedness pins alongside the blueprint |
| `internal/protocol/answer.go` | T6 | Sole change: resolving a permission ask |
| `internal/tools/paths.go` | T7 | The one enforcement point |
| `internal/tools/read.go` | T7 | Absorbs its existing path handling into the shared check rather than duplicating it |
| `internal/tools/write.go`, `edit.go` | T8 | — |
| `internal/tools/glob.go`, `grep.go` | T9 | — |
| `internal/tools/webfetch.go` | T10 | — |
| `internal/tools/todo.go` | T11 | — |
| `internal/tools/class.go`, `testdata/rerun_classes.json` | T12 | The six pending entries become present |
| `internal/handler/registry.go` | T12 | Policy attaches here (G17); still cannot grant a tool |
| `cmd/aeon/step.go`, `policy.go` | T12 | `--allow-bash` retired; `aeon policy check` and `corpus` added |
| `docs/test_suites/aeon-permissions.md`, `aeon-tools.md`, `aeon-ui.md` | T13 | Amended, never rewritten |

## What this slice does not do

A sandbox (never) · agent bodies and `by-tech-tag` (S13) · git and forge actions (S16) · new permission UI (S9
renders it) · `search` (S22) · a policy authoring UI (never) · network policy (never) · a shell parser beyond
segments and newlines.

**And it edits neither `opencode/`** (read by the extractor) **nor `docs/_archive/aeon-v1-horizontal/04-permissions/design.md`** (its
corpus re-measurement is already landed).

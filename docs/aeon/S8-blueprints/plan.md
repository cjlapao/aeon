---
slice: S8-blueprints
title: Blueprint & the two-step loop
stage: 2
stage_name: Bicycle
position: 3 of 4
brief: docs/aeon/S8-blueprints/brief.md
design: docs/aeon/S8-blueprints/design.md
demo: docs/aeon/S8-blueprints/DEMO.md
depends_on: [S1-bootstrap, S2-protocol, S3-model-layer, S4-store, S5-one-step]
independent_of: [S6-suspend, S7-recovery]
task_count: 12
---

# Plan — S8 Blueprint & the two-step loop

The largest slice in Stage 2: a format, its validator, an interpreter and a runner. L1–L10 are inherited from
`docs/_archive/aeon-v1-horizontal/07-loop-decide` and specify the format; this plan ports them to Go and adds L11–L19.

## Task sequence

| Task | Deliverable | Gate |
| --- | --- | --- |
| **T1** | The format types, the YAML loader, the `schemaVersion` gate | An unknown version is a loud refusal; every field in the reference table round-trips |
| **T2** | The state manifest **and** the condition evaluator — one declaration (L18) | One source, two readers, asserted |
| **T3** | The blueprint pin: store shape + pin-at-start (L6) | A running loop keeps its content after the file changes |
| **T4** | Validator part 1 — structural, referential-internal, unknown keys | Each class tripped by a fixture, asserted on the path |
| **T5** | Validator part 2 — machine (`sets:`), termination, routing completeness, reachability | The four classes needing S4's transition machine |
| **T6** | The three-state report, `kind`s, stable order, `--strict`, `aeon blueprint validate` | `unverifiable` names the registry and its slice |
| **T7** | The handler registry (L14) | Two entries; no field can grant a tool |
| **T8** | `LoopState` — one snapshot read | `Decide` needs nothing outside it |
| **T9** | `Decide` — pure, total, closed `Action` set | Every action reachable; a fuzz seed never panics |
| **T10** | `AdvanceLoop` + `execute`: model-step, gate, `iterate`; code-action **refused** | D3, D4, D15–D18 |
| **T11** | The runner, protocol events, `aeon loop show` | Starts after recovery; the ladder renders |
| **T12** | The three shipped blueprints, the demo walk, the QA suites | `dev-loop.yaml` validates with zero `invalid` |

**T9 is the one everything dispatches on**, and T12 carries the only test that checks the format against the thing
it exists for rather than a fixture written to suit it.

## Ordering rationale

- **T1 first** — nothing can be validated or interpreted before the format parses.
- **T2 before T4** because a `when:` field check needs the manifest to check against, and R6 keeps the manifest and
  its evaluator together so two field vocabularies cannot grow.
- **T3 early**, not late: the pin is a store shape, and if Q1 finds it needs a migration then everything touching
  loop rows wants it settled first.
- **T4 before T5** — the split is by what each needs. T4 checks the blueprint against itself; T5 needs S4's
  transition machine and the registries, which is also where `unverifiable` arises.
- **T6 after both** because the report's shape is determined by what the classes actually produce.
- **T7 before T10** because `execute` resolves `agent:` through it.
- **T8 before T9** — `Decide` reads the snapshot, and building the reader first is what keeps I2 honest.
- **T9 before T10** for the obvious reason: `execute` dispatches on what `Decide` returns.
- **T11 after T10** — a runner with nothing to advance is not testable.
- **T12 last**, and it is the only task that authors the real blueprint, because only by then can it be validated.

## Files

| File | Tasks | Reason for overlap |
| --- | --- | --- |
| `internal/blueprint/types.go` | T1 | — |
| `internal/blueprint/load.go` | T1 | — |
| `internal/blueprint/manifest.go`, `condition.go` | T2 | — |
| `internal/blueprint/validate.go` | T4, T5 | Split by what each class needs: T4 the blueprint alone, T5 the registries. One file, two disjoint check sets |
| `internal/blueprint/report.go` | T6 | — |
| `internal/blueprint/blueprints/*.yaml` | T12 | Authored last, when they can be validated |
| `internal/handler/registry.go` | T7 | — |
| `internal/loop/pin.go` | T3 | — |
| `internal/store/schema.go`, `migrate.go` | T3 | Only if Q1 finds the pin needs a column |
| `internal/loop/state.go` | T8 | — |
| `internal/loop/decide.go` | T9 | — |
| `internal/loop/advance.go`, `execute.go` | T10 | — |
| `internal/loop/runner.go` | T11 | — |
| `internal/daemon/serve.go` | T11 | Sole change: start the runner, after recovery |
| `internal/protocol/events.go` | T11 | — |
| `cmd/aeon/loop.go`, `blueprint.go` | T6, T11 | T6 adds `blueprint validate`; T11 adds `loop show`. Different files under one directory |
| `docs/test_suites/aeon-loop.md` | T12 | Amended, never rewritten |

## What this slice does not do

Code actions, git, forge, templates (S16) · agent bodies and `by-tech-tag` (S13) · phase/redo/QA semantics (S17) ·
permissions (S11) · the app (S9) · a visual builder (never promised) · retrieval (S22).

**And it does not edit `ARCHITECTURE.md` or `S5-one-step/design.md`** — P31 and L14's amendment are already landed.

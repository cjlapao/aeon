---
slice: S18-providers
title: Provider breadth
stage: 4
stage_name: Space shuttle
position: 1 of 4
brief: docs/aeon/S18-providers/brief.md
design: docs/aeon/S18-providers/design.md
demo: docs/aeon/S18-providers/DEMO.md
depends_on: [S3-model-layer, S6-suspend, S7-recovery]
independent_of: [S19-narrow-surface, S22-retrieval, S23-first-run]
task_count: 10
---

# Plan — S18 Provider breadth

S3 shipped two adapters and deferred the rest here. **This slice's job is to make good on M13** — the OpenAI-compatible
path is the OpenAI adapter plus capability flags, not a third adapter — **and to make that claim falsifiable.**

Its proof is the A→B→A round trip, and the third leg is what makes it a proof rather than a demonstration.

## Task sequence

| Task | Deliverable | Gate |
| --- | --- | --- |
| **T1** | Provenance: provider, model and route on every assistant turn | A transcript spanning two providers reports both |
| **T2** | Capability flags on the route, and the three states | A missing price is a configuration error, not silent |
| **T3** | The probe, cached per route, with its timestamp and model | A contradiction refuses at probe **and** at use |
| **T4** | The pre-call capability check | The client spy is at **zero** |
| **T5** | The thinking-replay rule | Replay keys on the producer, never on the target's flags |
| **T6** | The OpenAI-compatible path via flags — **no third adapter** | Two adapter files; `CAPABILITIES.md` started |
| **T7** | Cost: unmetered routes, and per-provider attribution | `$0.00` appears nowhere; a total over `unknown` is a floor |
| **T8** | The A→B→A round trip, end to end | The store is unmodified at every point |
| **T10** | The test bench — capability states + operator **Probe** on ProvidersPage, the S18 run in the bench demo runner, protocol types | The page drives and verifies the probe; no live-provider call ever leaves the app |
| **T9** | Demo walk, `CAPABILITIES.md` completed, QA suite | §1 works with keys genuinely unset |

## Ordering rationale

- **T1 first** (R4). M27 needs provenance, cost attribution needs it, and adding it after the adapters have branched means
  editing both.
- **T2 before T3** (R5) — declaring is a data shape; probing is a request and a cache. They fail differently, and a probe
  with nothing to verify against is not testable.
- **T4 after T2** because the check reads the route's declaration.
- **T5 after T1** — the replay rule is a function of provenance.
- **T6 after T5** because the OpenAI path's behaviour includes dropping thinking, and building the path first means
  retrofitting the drop.
- **T7 independent** of T4–T6, needing only T1's provenance for attribution.
- **T8 after T1, T5 and T6** (R6). It is the slice's claim and an integration test rather than a unit one.
- **T10 after T2, T3, T4 and T7** — its page shows what T2 declares and T3 probes, and its demo rows exercise
  T4's refusal and T7's unknown-cost. It is a react-ts task (`ui/` only) and touches no Go.
- **T9 last**, after T10 — it walks the terminal demo **and** the app's demo run — and the only task needing real keys.

## Reality alignment (2026-10-26)

The task files were drafted before two waves landed, and were amended on 2026-10-26 to match the shipped tree:
the adapters moved to `internal/model/adapters/` (with `openai_responses.go` — a second wire format behind the
one `openai` id, which re-words D10's "two adapter files" into a **file-list pin** with that precedent recorded);
the model-management wave shipped `Route.Capabilities` (the UI vocabulary, migration 0026), so S18's wire flags
became the **separate** `WireCapabilities` field with its own closed set; the CLI verb is plural
(`cmd/aeon/routes_verbs.go`, `aeon routes list|set|delete`), so the probe arrives as `aeon routes probe`; and the
P34 audit added `routes.probe`/D16/M32 plus per-route attribution on `LoopView` (T3, T7).

**`CAPABILITIES.md` is written across T6, T7 and T8** (R7), as differences are encountered — not assembled at the end,
which produces a record of what someone remembered.

## Files

| File | Tasks | Reason for overlap |
| --- | --- | --- |
| `internal/model/provenance.go` | T1 | — |
| `internal/model/types.go` | T1, T2 | T1 adds `Message.Provenance`; T2 adds `Route.WireCapabilities`. Disjoint fields, disjoint concerns |
| `internal/model/adapters/anthropic.go` | T1, T5 | T1 sets provenance at production; T5 adds the producer-keyed replay |
| `internal/model/adapters/openai.go`, `openai_responses.go` | T1, T5, T6 | T1 sets provenance; T5 drops and counts thinking; T6 adds capability gating. Three disjoint concerns across two wire formats of one provider |
| `internal/store/schema.go`, `migrate.go`, `transcript.go` | T1, T3 | T1 adds migration 0027 (`step_message.provenance`); T3 the next (0028, `route_probe`). Sequential by task order — the second names its number after the first lands |
| `internal/model/capability.go` | T2, T3 | T2 the flags and states; T3 the probe and cache. Split by what fails — a data shape versus a request |
| `internal/protocol/routes.go`, `method.go` | T2, T3 | T2 the view/params; T3 the `routes.probe` method and handler |
| `cmd/aeon/routes_verbs.go` | T2, T3 | T2 `routes set --capabilities/--no-price` and `routes list`'s states; T3 `routes probe` |
| `internal/runtime/precheck.go` | T4 | — |
| `internal/model/cost.go` | T7 | — |
| `internal/daemon/blueprint.go`, `cmd/aeon/loop_run.go` | T7 | One attribution computation, two renderers (CLI table, `LoopView`/S31 panel) |
| `internal/model/adapters/CAPABILITIES.md` | T6, T7, T8, T9 | **Written as differences are met** (R7); each task appends what it encountered |
| `internal/runtime/resume.go` | T8 | Only if Q6 finds resume assumes the original route |
| `ui/src/features/test-bench/pages/ProvidersPage.tsx`, `DemosPage.tsx`, `ui/src/lib/protocol.ts`, `ui/test/*` | T10 | The bench tier — react-ts, no Go |
| `docs/test_suites/aeon-model.md` | T9 | Amended, never rewritten |

## What this slice does not do

Gemini (v1.1) · a model catalogue (S23) · automatic failover (never — it would change what produced the work without
saying so) · model ranking · a caching strategy · embeddings (S22) · **a third adapter** · a new block kind · mid-step
route switching.

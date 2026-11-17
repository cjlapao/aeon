---
slice: S9-app
title: The app
stage: 2
stage_name: Bicycle
position: 4 of 4 — the halfway point of the plan
brief: docs/aeon/S9-app/brief.md
design: docs/aeon/S9-app/design.md
demo: docs/aeon/S9-app/DEMO.md
depends_on: [S1-bootstrap, S5-one-step, S6-suspend, S7-recovery, S8-blueprints]
task_count: 11
---

# Plan — S9 The app

The first slice whose work is mostly TypeScript and Rust. S1 built the window sixteen slices early so its
integration would never be a surprise; this is where it becomes the product.

**Its thesis is one line:** the app is where every three-state answer in the system survives or dies. Cost, the
`bash` receipt, blueprint validation and connection liveness all refuse to collapse an uncertain answer, and a UI is
where that gets thrown away by accident.

## Task sequence

| Task | Deliverable | Gate |
| --- | --- | --- |
| **T1** | The generic RPC bridge and the one event channel (Rust) | No protocol method name appears in `src-tauri/` |
| **T2** | The type generator from S2's conformance contract | CI fails if regenerating produces a diff |
| **T3** | Liveness: connection state, staleness, reconnect-and-re-read | Silence stays `current`; a drop goes stale with an age |
| **T4** | The three-state renderers | `$0.00` never appears for an unknown cost |
| **T5** | The port: components and pure helpers in, transport layer out | v1's transport-free component tests pass |
| **T6** | `Loops` — the list, and starting one | The ladder appears and updates without a click |
| **T7** | `Loop` — the ladder, the states, cost | `not-needed` shows its condition |
| **T8** | `Step` — the transcript, tools, manifest, compaction | Complete on expand; a changed file marked |
| **T9** | The thread — messages, step summaries, questions inline | A step question and a gate render identically |
| **T10** | `Blueprints` + `System` — validation in three states, recovery reports | `unverifiable` names its slice |
| **T11** | Demo walk, layout check, QA suite | Nothing needs a terminal |

**T1, T2, T3 and T4 all precede every screen** (R4, R5). A screen built before them is a screen built against a
mock that will not match the bridge.

## Ordering rationale

- **T1 first** — nothing can be read from the daemon without it, and it is the task where U11 either holds or a
  second method table is born.
- **T2 next** because every screen is typed against its output, and hand-written types that get replaced later mean
  every screen edited twice.
- **T3 before any screen** because staleness is not a badge added at the end: every rendered value is either
  confirmed or not, and retrofitting that is a change to every component.
- **T4 before the screens** (R5) — the renderers are the thesis, and a screen that formats a cost inline is a screen
  that will flatten it.
- **T5 after T2**, because the ported components must be re-typed against generated types, and before the screens
  that compose them.
- **T6–T10 are independent of each other** (R7). They share only the state T3 owns. `Blueprints` and `System` share
  one task because both are thin.
- **T11 last**, and it is the only task that walks the demo.

## Files

| File | Tasks | Reason for overlap |
| --- | --- | --- |
| `ui/src-tauri/bridge.rs`, `subscribe.rs` | T1 | — |
| `ui/src-tauri/conn.rs` | T1, T3 | T1 opens the socket; T3 adds the liveness clock and reconnection. Split by concern — T1 has no timing logic |
| `ui/src/proto/**` | T2 | Generated; never hand-edited |
| `tools/protogen/**` | T2 | The generator, checked in and hand-runnable (O3) |
| `ui/src/lib/rpc.ts` | T1, T2 | T1 the untyped call, T2 the typed wrapper over generated signatures |
| `ui/src/lib/live.ts` | T3 | — |
| `ui/src/lib/uncertain.tsx` | T4 | — |
| `ui/src/components/**`, `ui/src/platform/**` | T5 | Ported |
| `ui/src/screens/Loops.tsx` | T6 | — |
| `ui/src/screens/Loop.tsx` | T7, T9 | T7 the ladder, T9 the thread beneath it. One screen, two panels, disjoint components |
| `ui/src/screens/Step.tsx` | T8 | — |
| `ui/src/screens/{Blueprints,System}.tsx` | T10 | — |
| `Makefile`, `.github/workflows/ci.yml` | T2 | Sole change: the generator diff check |
| `docs/test_suites/aeon-ui.md` | T11 | Amended, never rewritten |

## What this slice does not do

Diffs and approval UI (S16) · permission prompts (S11) · an agent picker (S13) · a blueprint builder (never) ·
HTTP (S19) · retrieval (S22) · signing (S23) · a remote daemon (never — P29).

**And it edits neither `packages/loop-state-ui`** (read and ported from) **nor `docs/_archive/aeon-v1-horizontal/10-ui-chat`** (U10's
correction is already landed).

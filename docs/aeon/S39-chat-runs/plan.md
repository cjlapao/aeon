# Plan — S39 Chat runs

## What is settled before planning

`brief.md` and `design.md` CR4–CR11: starting a run is a tool+method over S11's `ask`; prerequisites are a dialogue; kickoff is a template with a message
`source`; attendedness stays declared; steering is untouched. **Not settled, by design: CR1 (the resting state), CR2 (run storage), CR3 (the turn and which
agent)** — T0 closes them; the recommended defaults (amended 2026-10-27 against the code) are in `design.md`, and **T1–T13 are written for those defaults**, each
with an "If T0 decided otherwise" paragraph where it matters.

## How we will do it

**Investigation first (T0), then strictly sequential** (the standing no-parallel-tasks direction). S39 touches the engine's most load-bearing
invariant — what ends a loop — so the plan is conservative: the **opt-in** `after: converse` key means every existing blueprint is byte-identical until a
blueprint asks. Each task changes roughly 300 lines or fewer, has one verifiable outcome, and is written so a lower-capability implementer can follow it
mechanically (exact files, signatures, SQL, test names, commands, guard rails). Measured numbers go in Notes (AGENTS §4a). S39 **cannot start before S38 T13**
(the catalog tools, `emits:`/`requires:`, goal/task start); T10 also needs S38 T9 and T10.

**Migration numbers are not fixed here.** The store stood at migration 34 on 2026-10-27 (S18-S20 landed); S38 adds its own. T1's migration is *last id + 1*, T5's is
*+2*; each task says to measure, and to bump every `len(Migrations())` assertion.

## T0 — Investigation (no code)
Closes CR1–CR3 by running things; nine questions, each with commands and a named deliverable: readers of loop status, per-run pin cost, chat today and the default
agent, `chat_message`/`step_steer` shape, S11 `ask` plumbing and the exact policy command, the composer code, a turn as a step (recovery), tool identity, and the
numbering/staleness check. Writes `investigation.md`; edits `design.md` CR1–CR3 to final. **Depends: S38 T13.**

## T1 — Migration: `loop_run`, `step.run_seq` (CR2)
One migration (L+1): the table, the column, a backfill of run 1 per pinned loop (provenance from S20's `loop.started_by`), count-test bumps, upgrade tests. No Go behavior.
**Outcome:** an old store upgrades with rows intact and every pinned loop has run 1. **Depends: T0.**

## T2 — Store layer for runs (CR2, CR9)
`BeginRun` (idle→running claim, re-pin), `EndRun`, `CurrentRun`, `ListRuns`, `SetStepRun`, `RunCost`/`LoopCost` (P32 floors); `CreatePinnedLoop` writes run 1.
**Outcome:** the store can run two runs in one loop and cost each. **Depends: T1.**

## T3 — Blueprint keys `after`, `converse`, `kickoff` (CR1, CR3, CR6)
Types, validation diagnostics, `KickoffData`/`ParseKickoff`; no shipped blueprint changes. **Outcome:** the keys load and validate; old blueprints unchanged. **Depends: T0, S38 T11.**

## T4 — The resting state and `loop.close` (CR1, CR9, CR10)
`idle` status, `ActIdle`, one `endStatus` and one `emitIfOver`, `loop.run.finished` event, `loop.close`; recovery/startup leave idle alone; steering fixtures pinned.
**Outcome:** `after: converse` ends idle; everything else ends `done` exactly as before. **Depends: T2, T3.**

## T5 — Message `source`, `loop.message.posted`, nudge labelling (CR6, CR10)
Migration (L+2) for `chat_message.source` and `step_steer.source`; store; the event and `publishMessage`; the engine nudge labelled `blueprint`; steer wire shapes byte-identical.
**Outcome:** every message knows where it came from and the screen can be told. **Depends: T4, S38 T12.**

## T6 — `loop.message` and the conversation turn (CR3, CR10)
Socket method, the turn as a model step by the conversation agent (`chat` agent default), composer-rule refusals with the `step.steer` remedy, one turn at a time,
`brainstorm` blueprint rests. **Outcome:** a message to an idle loop is answered; a message to a loop with a running step is refused with its remedy. **Depends: T4, T5, S38 T10, S38 T14.**

## T7 — Kickoff renderer and the run's first message (CR6)
`RenderKickoff`, `KickoffFor`, `RuntimeState.Kickoff`, `modelPrompt` reads it; golden test proves existing prompts byte-identical; parity test for typed vs templated.
**Outcome:** typed and templated starts give byte-identical model input. **Depends: T6.**

## T8 — Engine `StartRun` (CR2, CR6, CR7)
`pinSource` extraction, `PreviewBlueprint`, `loop.StartRun` (claim before write, first message, route inheritance, attendedness untouched), `MarkStepStarted` tags `run_seq`.
**Outcome:** an idle loop can pin and start a second blueprint. **Depends: T2, T4, T7.**

## T9 — `loop.run`, `loop.run.options`, the requires dialogue (CR4, CR5, CR7)
Socket methods and daemon `startRun`, S38's `CheckRequires` with candidates (refusal as data, no run row), `loop.run.started`, `runs` and `attended` on `loop.show`, grep-pins that
only the run doors start runs. **Outcome:** a human peer starts a run; an unmet requirement is a sentence plus candidates. **Depends: T8, S38 T13, S38 T15.**

## T10 — The `run_start` tool, its `ask` row, attendedness (CR4, CR5, CR7)
One catalog row (S38's one table, two doors), `Do` case, registry pins, `chat` grant; the `ask` row is an operator command, never seeded. Policy tests: attended asks, unattended denies, a chatting human
does not change attendedness. **Outcome:** an agent can propose a run and a person confirms it. **Depends: T9, S38 T9, S38 T10.**

## T11 — UI: composer on idle, message and run cards, *Run blueprint…* (CR3, CR8)
Event adapters, message/run cards, message-mode composer, run-options panel; `done` still hides the composer. **Outcome:** the screen can converse and start runs. **Depends: T9.**

## T12 — UI: the `run_start` ask card and the unattended notice (CR7, CR8)
Existing question card gets a heading; the three-state attendedness notice. **Outcome:** the confirmation is readable and honest about who can confirm. **Depends: T10, T11.**

## T13 — Close-out
Zero-regression (D1, D9-D11), `DEMO.md §S39` walked with a Findings table, STATUS/ROADMAP/PLAN/TASKS rows, `CR` prefix in ARCHITECTURE §11. Native desktop appearance named as unverified unless a desktop run is done.
**Depends: T1–T12.**

## Sequencing summary

```
S38 T13 ► T0 ► T1 ► T2 ► T3 ► T4 ► T5 ► T6 ► T7 ► T8 ► T9 ► T10 ► T11 ► T12 ► T13
          (S38 T9,T10 needed by T10; S38 T10,T14 by T6; S38 T12 by T5; S38 T15 by T9)
```
Fourteen tasks; **two** migrations, **one** new table, **one** new status, **three** optional blueprint keys (`after`, `converse`, `kickoff`), **zero** new steering paths, **zero**
change for a loop whose blueprint does not opt in.

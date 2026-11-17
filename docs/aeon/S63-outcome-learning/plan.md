# Plan — S63 Outcome learning

## What is settled before planning

`brief.md` and `design.md` RT1–RT16. Short version: a daemon reactor records **append-only outcome facts** at the
moment a loop finishes or blocks (`retro_outcome`, `retro_agent`); a **fixed-template** retrospective learning is filed
through S28's existing write path at tally milestones; **scorecards and the "what did we learn" digest are derived
reads** over those facts, history and telemetry, with sample sizes, a minimum-n rule and "cannot say" instead of zero;
**nothing that is measured ever changes dispatch, routing, grants, budgets or blueprint choice** (locked, guarded, with
a canary); a model-written retrospective is an opt-in, capped, `assumed`-only extra; model evals are explicitly parked.

## How we will do it

Facts first (T1 tables, T2 recorder + backfill), because every later read stands on them and a loop that finished
before the recorder is gone for good. Then the learning writer and its seeded templates (T3), which depends only on the
facts and S28. Scorecards (T4) are a pure read package built beside T3 and proven against a census store. The socket
surface, digest and the **import guard** land together (T5) so the locked rule has its test the day the first reader
exists. Agent tools (T6) and the CLI (T7) follow in parallel. The optional model retrospective (T8) waits on S55, S48,
S43-extra and S52 and is the only task that can slip without hurting the rest. Close-out last (T9).

## Task sequence

### T0 — Re-verify, inventory, baseline, measure what archives keep (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**

### T1 — The fact tables: `retro_outcome`, `retro_agent`, `retro_learning`, `retro_template`, and the store's SQL half ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.**

### T2 — The recorder: a reactor at the existing emission point, and the optional backfill ([`tasks/T2.md`](tasks/T2.md))
**Depends: T1.** Parallel-safe with T4's early work (fixtures).

### T3 — The retrospective learning: milestones, templates, the `retro.templates` seed set, reconcile ([`tasks/T3.md`](tasks/T3.md))
**Depends: T1, T2; S28 (built); S43-extra T3 (seed engine).**

### T4 — `internal/scorecard`: derived cards with n, states and floors ([`tasks/T4.md`](tasks/T4.md))
**Depends: T1** (fixtures from T2). Parallel-safe with T3.

### T5 — The socket surface, the digest, and the locked-rule guards ([`tasks/T5.md`](tasks/T5.md))
**Depends: T3, T4.**

### T6 — Agent tools: three read specs ([`tasks/T6.md`](tasks/T6.md))
**Depends: T5.** Parallel-safe with T7.

### T7 — The CLI: `aeon scorecard`, `aeon retro` ([`tasks/T7.md`](tasks/T7.md))
**Depends: T5.** Parallel-safe with T6.

### T8 — The optional model retrospective ([`tasks/T8.md`](tasks/T8.md))
**Depends: T3, T5; S55 (budget cap), S48 (blueprint), S43-extra (agent seed), S52 (`learning_add`).** Blocked until those merge.

### T9 — Close-out ([`tasks/T9.md`](tasks/T9.md))
**Depends: T0–T7** (T8 if built, else recorded "not built — blocked on …").

## Dependency graph

```text
T0 → T1 → T2 ──┬─ T3 ──┬─ T5 ─┬─ T6 ─┐
          └─ T4 ──────┘      └─ T7 ─┼─ T9
                       T3,T5 → T8 ───┘ (blocked on S55/S48/S43-extra/S52)
```

## Risks

| Risk | Mitigation |
| --- | --- |
| The recorder misses an event (crash, reactor error) and a scorecard silently reads high | `complete` flag, `retro_dropped_total`, coverage on every card (RT6/RT9); startup reconcile for the learning half; a card never claims more than it recorded |
| A score becomes a lever (someone wires it into routing "just this once") | RT3 import guard with canary in T5; method classification; no ranking views; agent tools ungranted by default |
| Learning flood or free-text leakage from retrospectives | milestones only; closed placeholder set; template-validated at seed and render; a test plants hostile loop text and asserts it never reaches a body |
| Small-sample rates mislead | `MinSample`, `n` on every metric, `too-few`/`cannot-say` states; no zero-for-unknown (P31) |
| Cost per item mistaken for an exact number | floors only, with the unknown/partial counts, same `Rollup` semantics as history (F10/F11) |
| Recorder adds latency to the emit path | goroutine, same as the three reactors (V18); a test with a blocked recorder asserts event delivery unchanged |
| Facts duplicate history | they hold only what history lacks (counters, per-agent steps, gates); status/time/cost are read from where they live when a card is computed? **No** — status/cost are captured too so archives and later edits cannot change a card; T4 documents which fields come from facts vs history and a test pins the split |
| S55/S57 shapes differ from the requests | those fields are nullable `not-recorded`; T0 records the real state; the recorder adds columns by migration when they land (`ALTER … ADD COLUMN`, additive) |

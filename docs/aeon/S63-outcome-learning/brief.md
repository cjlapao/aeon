# Brief — S63 Outcome learning: retrospectives and scorecards

Written 2026-10-30. Prefix **RT**. Covers [`AUTONOMY-GAPS.md`](../AUTONOMY-GAPS.md) **G9**. Task shapes cite
[`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (P1–P11). Model: the org record is the S44-extra component tree
(a root component and its children).

## 1. Why

An autonomous project produces hundreds of loops. Today almost nothing is **remembered** about how they went, and
nothing tells a person **which agent, blueprint or component is working well**:

- The only automatic learning writer is the triage validation feed (`daemon/triage_learning_feed.go:44-59`); nothing
  writes a learning when an ordinary loop finishes — the three reactors on `loop.finished`/`loop.blocked`
  (`daemon/blueprint.go:602,609,615`) move a queue item, a roadmap item or a bug, and stop.
- `internal/loop` and `internal/runtime` do not reference learnings at all.
- `internal/history` is a **read-only projection** of loops (status, floored cost, blocked reason —
  `history/query.go:43-67`); it carries no rework counts, no gate results, no per-agent step outcomes, and a merged
  loop's counters live inside `RuntimeState`, which is not part of an archive header.
- The numbers that would say "this agent needs three review rounds per task" already exist as facts —
  `retries`/`review_rounds`/`security_rounds` on the task row (`schema.go:1887-1889`), `qa_rounds` and
  `total_failed_gates` in `RuntimeState.Counters` (`loop/counters.go:55-62`), `step.agent`/`step.outcome`/`cost_*`
  (`schema.go:876,884`), the handoff `verification` verdict — and are never joined into anything a person can read.

## 2. What this slice delivers

1. **Outcome facts, recorded at the moment a loop finishes or blocks.** A daemon reactor (the queue reactor's
   precedent, same emission point) writes append-only rows: one per terminal event (`retro_outcome`) and one per agent
   that ran a step in that loop (`retro_agent`). Facts only — enums, counts, ids, a cost floor with its honesty state.
2. **A retrospective learning, fixed-template and bounded.** From those facts the daemon files an `observed` learning
   (the existing S28 write path) at **milestones** of a repeated outcome (1st, 3rd, 10th, 30th …), with the observed
   count set once at write time (S28 M3 respected), text rendered from a seeded template out of ids and enums only —
   **no model, no raw user text**. An **optional**, off-by-default, budget-capped model-written retrospective files
   an `assumed` learning only.
3. **Scorecards: derived reads.** Per agent (slug), per blueprint and per root component (its tree): outcomes, rework,
   gate pass rate, cost per completed item with **known/partial/unknown honesty**, wall time, escalations, and (when
   S57 lands) merged-then-reverted. Every metric carries its sample size and a **minimum-n rule**: too few samples
   read "too few", missing data reads "cannot say" — never zero, never a rate computed from two runs.
4. **The locked rule: scorecards and learnings never change anything by themselves.** They do not influence
   dispatch, routing, grants, budgets or blueprint choice. A person reads them and decides. A guard test proves the
   scorecard package is imported by no dispatch, policy, tool-registry or runtime code.
5. **A "what did we learn" digest** read per root component — the content spec S58 delivers weekly.
6. **Doors:** socket first (`scorecard.*`, `digest.learned`, `retro.*` reads), agent read tools, an `aeon scorecard`
   CLI client, and a UI specification (the screens are built after, on this contract).

## 3. What exists today (measured 2026-10-30; T0 re-walks every line)

| Fact | Where |
| --- | --- |
| One automatic learning writer: triage validation → `learnings.New(store).Append{scope=project, scopeKey=bug.ComponentRoot, provenance=observed, observedCount=1}` | `daemon/triage_learning_feed.go:44-59`, called `daemon/triage_dispatch.go:269` |
| Reactors on terminal loop events are invoked from `publishLoop` for `loop.finished` and `loop.blocked` only | `daemon/blueprint.go:602-616`; handler shape `daemon/queue_reactor.go:40-46` |
| Learning scopes `person|project|agent`; `project` scope_key is a **repository root path**, `agent` scope_key an agent name (a slug after S52) | `learnings/types.go:20-40`, S52 LM1 |
| Four provenances `told|observed|assumed|relayed`; `observed_count` meaningful only for `observed`, set once at write time (M3) | `learnings/types.go:56-81`, `learnings/store.go:44-85` |
| Learning rows: `LearningRow{Scope, ScopeKey, Body, Provenance, ObservedCount, SourceLoopSlug, SourceStepID, Tags}`; create is one transaction with its tags | `store/learnings.go:30-142` |
| Loop-wide and task counters, closed table; ceiling 10 | `loop/counters.go:30-62` |
| Task row counters `retries`, `security_rounds`, `review_rounds` | `store/schema.go:1887-1889` (0036 rebuild), `store/tasks.go` |
| Step row: `agent`, `status`, `outcome`, `cost_usd`, `cost_state`, `cost_detail`, `agent_ms`, `run_seq` | `store/schema.go:876-898`, `store/steps.go:335,402-439` |
| Handoff verification `pass|fail|unverified`; an unverified ok becomes a reject with an `unverified:` reason | `handler/handoff.go:96-123,175-178` |
| Blocked reason kinds (closed-ish list: `model-error`, `exhaustion`, `step-blocked`, `action-denied`, `action-failed`, `runner-error`, …) | `loop/action.go:21`, `loop/advance.go:248,403`, `loop/escalate.go:196` |
| History is read-only: `Entry{Kind, Slug, ComponentRoot, Status, StartedAt, EndedAt, Cost Rollup, Reason}`; `Rollup{Total, Floor, UnknownCount, StepCount}` (P32) | `history/query.go:43-67`, `history/rollup.go:12-35` |
| Telemetry rollups per component/project/workspace with `cost_state known|partial|unknown` | S33, `migrations/20261030084236_telemetry.sql` |
| Loop status vocabulary is Go-only (`running|waiting|blocked|done|aborted|idle`); `blocked` is **not** terminal (a retry or answer resumes it) | `loop/rest.go:24-37`; loop-control investigation 2026-10-30 |
| `learnings.append` refuses `relayed`; its refusal text says "relaxed" | `protocol/learnings.go` (~180-205) — logged for S52, not edited here |

## 4. Out of scope

- **Model evaluations.** This slice measures what happened in production loops; it does not build a harness that
  replays tasks against models. RT15 states what an eval would need and parks it.
- **Anything that acts on a score** — auto-routing, auto-grants, auto-budgets. Locked off (RT3).
- Delivery of the digest (S58); the UI screens (this slice specifies them, RT14); renaming learnings scopes (S52).
- Retrospectives for chat/idle runs (`loop.run.finished`), triage validation loops (they have their own feed) and
  loops started by the retrospective itself.

## 5. Cross-slice requests (recorded; none blocks the first three tasks)

| To | Request |
| --- | --- |
| **S55 LC** | cap-hit and stuck outcomes surfaced as blocked-reason kinds (`cap-hit`, `stuck`) and the per-loop cap facts readable by the recorder; a per-loop budget for the optional retrospective loop |
| **S57 VG** | gate results (`gate_run` rows: kind, outcome, attempt) and a durable `merged` outcome with a later `reverted` fact, both carrying `component_root` and `loop_slug`; rework count per item |
| **S27 CN** | an attempt record linking a roadmap/queue item to each loop it started, so "item" in a scorecard can mean the work item, not only the loop |
| **S48** | a seeded one-lane `retrospective` blueprint (T8) |
| **S43-extra** | a seeded `retrospective-author` agent row (T8) |
| **S52** | `learning_add` agent tool (LM7) used by T8; fix the "relaxed" wording in the `relayed` refusal |
| **S44** | setting declarations `retro.model.enabled`, `scorecard.minSample` (T8, T5 adopt after S44 merges, S44 T10's shape) |
| **S47** | classify the new methods in the surface table |
| **S58 SV** | consumes `digest.learned` for the weekly digest |
| **S44-extra** | root/children model used for tree roll-ups (T0 confirms merged state) |

## 6. Acceptance

1. Every `loop.finished` and `loop.blocked` for an ordinary loop produces exactly one `retro_outcome` row (and its
   `retro_agent` rows) and never delays the event path; a crash between event and write loses at most that row and is
   detected, not hidden (RT2, RT6).
2. The retrospective learning is written only at milestones, from a template, with no free text from a loop, a person
   or a model; the observed count equals the true tally at write time and is never changed afterwards (RT4).
3. Scorecards are derived on every read from `retro_*` facts, history and telemetry; no cache, no stored score;
   missing data says "cannot say", small samples say "too few", costs are floors with the unknown count (RT7–RT9).
4. No code in dispatch, routing, grants, budgets, policy, tool registry or runtime imports the scorecard package
   (guard with a canary) (RT3).
5. Socket methods, agent read tools and the CLI client agree both ways (parity tests); agents see no other agent's card
   (RT10–RT12).
6. The optional model retrospective is off by default, capped, unattended-local-only, and files `assumed` only (RT5).
7. `make protogen && make check-protogen && make guard` green; migration drills (fresh, upgrade, idempotent re-open,
   append-only proof).

## 7. Status

**Partial — 9/10 built, closed out 2026-11-05.** T0–T7 built and merged to main (T7, the CLI, landed as
`61d46c76`); T9 is this close-out. T8 (the model retrospective) is **not built — blocked on the two seeded
records**: the `retrospective` blueprint and the `retrospective-author` agent do not exist in any seed this
slice owns (S48's blueprintseed carries twelve ids, none of them; the author agent appears nowhere under
`internal/`), and those seeds belong to S48/S55/S44/S52 — this slice builds no reactor on fabricated seeds
and reports the honest `not enabled` word instead (`retro.status`, walked).
The slice's law holds provably: the recorder writes exactly one outcome per terminal (D1, walked), the
milestone writer files template bodies only under `observed` (RT4, walked at n=1 and n=3), every card and
digest is derived and answers `too-few` / `cannot say` / coverage honestly (D5, walked), no score reaches
dispatch, routing, grants, budgets or blueprint choice (RT3's guard `TestScorecardNeverReachedByDispatch`,
canary walked), and the CLI is a pure client of the socket (T7). Evidence: `aeon/scripts/qa-s63-outcome.sh`
rc=0 walked 2026-11-05; [tasks/T9.md](tasks/T9.md) carries the acceptance table.

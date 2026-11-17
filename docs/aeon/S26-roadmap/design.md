# Design — S26 Roadmap

**Brief:** [`brief.md`](brief.md). **Plan:** [`plan.md`](plan.md). **Demo:** [`DEMO.md`](DEMO.md).

**Written 2026-10-26**, against S8's own `design.md` (GAP-7, the ideation correction — brief §2), S29's
already-built brief (the priority constraint, D2), S27's sketch (the "ready" definition, D4), S13's real
`project-planner` handler body, and S24/S25's own just-written designs (the item-status and lane precedents
this reuses rather than re-derives).

## 1. Context

This sketch's four open questions were narrower than S25's or S24's — three of the four were really "which
existing mechanism does this reuse," answerable by reading the mechanism, not by inventing one. The one
genuine correction (§2 of the brief) came from reading S8's own `design.md` rather than trusting the
sketch's summary of it.

## 2. Decisions

### R1 (brief Q1) — item status, eight values, closed

```
idea → ideating → brief-agreed → planned → ready → running → done
  │        │           │            │        │                 │
  └────────┴───────────┴────────────┴────────┴─────────────────┴──→ cancelled
```

`idea` (created, ideation not started) · `ideating` (the ordinary loop from brief §2 is in flight) ·
`brief-agreed` (that loop's `produces: [brief.md]` step satisfied `missingArtifact`'s check — §3 Q3's
resolution) · `planned` (a `project-planner` step, dispatched the same way S24's T5 dispatches
`sizing-estimator`, produced `plan.md`) · `ready` (D4's definition: brief agreed, dependencies complete,
priority not `unknown` — all three, re-checked at the moment of transition, not cached from whenever each
became individually true) · `running` (materialized — §3's R4 below) · `done` · `cancelled` (from any
non-terminal state, D7).

**This mirrors S24's queue-item discipline deliberately**: an item's status is its own field, never read off
whatever loop/brief/plan it currently points to (D6), the same separation D10 in that slice's own brief
established first.

### R2 (brief Q2) — `epic` is a tag, not a row

A free-text field on the item, the same shape `project.tags` already uses (`schema.go`: `tags TEXT NOT NULL
DEFAULT '[]'`) — no new entity. An epic with its own lifecycle (a start date, a status of its own) is a
heavier concept nothing in this brief's DoD asks for; if that need arrives later it is a new table and a
migration, not a retrofit onto a tag field that was never meant to carry it.

### R3 (brief Q3) — where ideation's loop ends

`internal/loop/advance.go` and `continue.go` both call `missingArtifact(st.Root, step.Produces)` — a plain
file-existence check against the step's declared `produces:` paths, already wired into the engine for every
blueprint step. **Ideation's loop ends the same way any step with a `produces:` declaration already ends**:
the step (or the last step, if ideation genuinely needs more than one) declares `produces: [brief.md]`: no
new check, no new field, no narrow addition — §2's "no new engine" claim, now true for the actual mechanism
rather than the sketch's original, corrected guess about which mechanism.

### R4 — materializing a `ready` item: queue first, direct start as the fallback

**Not asked as a brief question, found while designing D10.** A `ready` item's transition to `running` calls
`internal/queue`'s `Append` (S24, if the project has a queue — D10's "renders S24's ordering... builds
none of its own") so the item enters the project's existing ordering rather than jumping the line through a
second path. **If the project has no queue** (S24 unused or not yet seeded for that project), the item calls
`loop.start` directly — the same existing entry point every other caller in this project uses, never a
second "start a loop" mechanism. Either branch ends at the identical `loop.start` call; the only choice is
whether something else orders it first.

## 3. Architecture

```
  idea → ideating (ordinary loop,         brief-agreed → planned (project-planner,
  R3's produces: check)                   S13's existing handler — dispatched like
         │                                any other step, D9)
         ▼                                        │
  internal/roadmap (new package)  ◄────────────────┘
         │
         ├── D2 priority: known | assumed | unknown
         ├── D3 estimate: {min, max, basis}
         ├── D4 "ready": brief-agreed ∧ planned ∧ priority ≠ unknown
         └── D5 provenance: authored | triggered | triaged
         │
         ▼
  ready → running (R4: Append to S24's queue if present,
           else loop.start directly — one materialization
           path either way)
         │
         ▼
  rendered: forward Gantt = S25's lane component, pointed
  at the future instead of the past (D10, no new component)
```

## 4. Testing this slice has to do

- **R1's transition table, exhaustive and closed** — the same discipline S24's T2 and S25's own tests use.
- **D2/D4 as properties**: a fixture with `priority: unknown` can reach every status except `ready` —
  asserted by driving the full transition table against it, not just the happy path.
- **R3's artifact check reuses the existing function, not a copy of it** — a call-site assertion, the same
  shape S25's T2 used to prove its live drill-down wasn't a second implementation.
- **R4's fallback**: a project with a queue routes through `internal/queue.Append`; a project without one
  calls `loop.start` directly — both produce the identical final loop, asserted by diffing the resulting
  `LoopStartParams`.
- **D8, directly**: `loop.start` succeeds against a project with zero roadmap items — the non-goal, tested
  as a property, not assumed from the fact that nothing currently requires it.

## 5. Amendments to earlier slices

**None required, one cross-reference worth recording.** `S29-triage/brief.md`'s D2 already states a
requirement on this slice in its own words, written before this slice had a brief. This design satisfies it
(§2 D2) rather than amending S29 to match something written after — the order of authorship ran the other
way, and that is worth the reader knowing rather than assuming S26 came first.

## 6. Open questions this design did not close

- **Whether `ideating` needs its own suspend/resume story beyond what S6 already provides.** A person
  closing the app mid-ideation and returning later is presumably just S6's ordinary suspend — not asked here
  because nothing about ideation's loop is special enough to need a different answer, but the task building
  T1 should confirm rather than assume.

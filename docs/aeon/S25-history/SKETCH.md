# Sketch — S25 Project history (Stage 5 · The fleet)

**Status:** sketch, not a design. **Deliberately shallower than S1–S23** — the alpha is not built, and a full brief for a
view over data that has never accumulated would be fiction.
**Date:** 2026-09-07 · **Depends on:** S4 (the store), S9 (the app).

---

## What it is

**The cross-loop, time-ordered projection of everything this project has done.** Every loop by recency, drilling down to
phases, tasks, steps, handoffs and costs.

**A read and a view. Not a subsystem.** S4's store already holds all of it — loops, `loop_phase`, `task`, `step`,
`step_message`, `event` with a global `seq`, costs with their states, handoffs with declared fields. S9 renders it **per
loop**. What is missing is the projection **across** loops and **through** time.

## Why it comes before the roadmap

Three reasons, and the third is the requirement:

**It is useful alone.** *"What has this thing actually done for me"* is answerable with no planning surface at all.

**The roadmap without it is a plan with no context.**

**And it is what makes the roadmap adoptable at any point.** A project can run feature-by-feature for six months and then
want a roadmap. If the roadmap owned the record of what happened, adopting it would mean backfilling six months of intent —
which nobody does. They start a fresh roadmap and lose the history. **A projection means the roadmap has content on day
one.**

## The shape

One Gantt-shaped surface. History occupies the past; S26 adds the future; **the present is wherever a loop is actually
running.** Same component, two directions.

Lanes are the natural groupings the store already has: by loop, by phase, by task, by agent.

## What it makes visible for the first time

Cost per feature over time. Which agents did what. **How many redo rounds a kind of task actually takes.** Where gates
stalled for days. Which lanes were bypassed and why.

**That is the data that says whether the *method* works** — S17's product claim, measured over months instead of once.
Nothing else in the plan produces it.

## The two costs worth naming now

**It is the first thing in the plan that reads a lot.** S4 chose two pools, a single writer, and `seq` as the `event`
rowid — deliberate choices for a workload of one loop at a time. *"Drill down to every task and result across two years"*
is a different load. **S4 already carries the amendment**: the query shape is named there so an index lands with the schema
rather than as a later migration.

**And an aggregate over uncertain values is itself uncertain** — now [P32](../ARCHITECTURE.md#8-product-decisions). S9
already floors a per-loop total when one step is `partial`; over a project's whole history the rule is **far more tempting to
break, because a dashboard wants a number.** A total spend over a year containing forty `unknown` costs is **at least $X**,
and a chart that draws it as a point is the eighth version of the same mistake.

## What it must not become

**Not a second source of truth.** It reads; it computes nothing that is not derivable from the store, and it stores nothing
of its own. The moment history has its own table, the store and the view can disagree — and the view is what people would
believe.

**Not a summary.** Six slices refused to collapse uncertain answers, and an aggregate view is where they all arrive at once
(P32). A history screen that reports "42 loops, $18.40, 91% success" over a dataset containing unknowns has thrown away more
than `doctor` could.

## Open questions for whoever specs it

- What are the lanes, exactly, and are they user-choosable?
- Does drill-down reach the transcript, and if so does S22's V15 rule apply — is a transcript readable in a history view when
  it is not indexable? (**Probably yes**: reading one deliberately is not the same as embedding all of them. But it should be
  decided, not assumed.)
- Is history exportable, and in what shape? A dataset people can query is more useful than a chart, and more dangerous.
- What does the view show for a loop that was **aborted**? S17 established that a declined plan is a working system, and a
  history that renders it as a failure would make the same metrics lie.

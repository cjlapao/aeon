# Sketch — S26 Roadmap (Stage 5 · The fleet)

**Status:** sketch, not a design. Deliberately shallower than S1–S23.
**Date:** 2026-09-07 · **Depends on:** S8 (blueprints), S9 (the app), S24 (queue), S25 (history).

---

## What it is

The surface where a project is **planned** rather than executed: an idea is discussed until it is agreed, becomes a brief,
becomes a plan, and then agents build it.

**The user spends time here as a product manager**, not as an operator — iterating on a feature until it is good, rather
than watching a loop run.

## Two halves, and only one is new

**Ideation → brief needs no new engine.** A chat that iterates toward an agreed brief is a `conversational-map`-shaped
blueprint whose `produces:` is `brief.md`. S8 built the format and said there are **five** protocol shapes in this
repository; S17 filled one ladder. This expresses a second, which is what L10's *"two blueprints prove the vocabulary
generalises"* was for.

**The forward Gantt is the new part**, and it is the same component S25 built pointed the other way.

## It sits beside the loop, not instead of it

**A project can start feature-by-feature and gain a roadmap in month six, or drop back to loops at any time.**

That constraint has one sharp consequence: **the roadmap cannot be the source of truth for what happened.** It is a view
over loops that already exist (S25). If it owned the record, adopting it later would mean backfilling six months of intent —
so nobody would, and they would start fresh and lose the history.

This is S22's optionality shape, strengthened: not just *"the product works fully without it"*, but **"and it can be adopted
at any point without a migration."**

**And it is a view over S24's queue, too, where one exists.** A project running feature-by-feature with a queue already
has ordering and sizing — the roadmap renders that, rather than inventing a second ordering to keep in step with the
first.

## What it must be able to render

**An item whose priority is honestly unknown.** This comes from bug triage's second settled point: an unreproduced bug is
**not low priority — it is unknown priority**, and collapsing those is how real bugs get closed. So `unknown` is a priority
value, not an absence, and it sorts somewhere deliberate.

**An estimate as a range with its basis**, never a figure. *"The times to build"* is the dangerous part: a number presented
as an estimate gets quoted as a commitment. Same shape as S22's reindex estimate — labelled, with its basis (task count,
comparable past loops), and P32 applies to any roll-up of them.

**Items that came from elsewhere** — a webhook-created issue, a bug from triage — marked as such, because an item someone
wrote and an item a forge created are different kinds of claim.

## The link to what already exists

An item points at its brief, its plan, its discovery, its tasks and — once built — its loops. Those are all files and rows
that S8, S13 and S17 already produce. **The roadmap adds the pointer and the ordering, not the artefacts.**

## What it must not become

**Not a requirement for running a loop.** The moment `aeon loop start` needs a roadmap item, the feature-by-feature path is
dead and the "sits beside" promise is broken.

**Not a second planner.** `project-planner` is a handler (S13) and `plan.md` has a schema. The roadmap holds *what is next*
and *why*; the plan holds *how*.

## Open questions for whoever specs it

- What are the lanes? By epic, by area, by agent, by priority — and is it user-choosable?
- How does an item become "ready"? That is S27's trigger, so the definition belongs here and the consumption there.
- Does the ideation chat share the loop chat's surface (S9's U18: *one thread per loop*), or is it its own thing? **If it is
  a blueprint, it has a loop, so it has a thread** — which suggests it shares, and that is worth confirming rather than
  assuming.
- What happens to an item whose brief was agreed and then the project changed direction? Cancelled is not failed, the same
  way S17's `aborted` is not failed.

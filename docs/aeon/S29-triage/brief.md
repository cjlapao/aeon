# Brief — S29 Triage

**Implements:** [`design.md`](design.md) — written 2026-10-26, resolving Q2–Q4 against S24's, S26's and
S28's now-real designs. **Q2 turned out to already be answered by D2** (validation and priority are two
separate closed sets, never one crossed enum — priority's exact shape copied verbatim from `S26-roadmap`'s
own `priority_state`/`priority_label`). **Q4's hand-off targets S26 only** (never S24 directly — S26 already
knows how to reach S24's queue when one exists), through S26's ordinary `idea` entry point, requiring **zero
changes to S26**. **Demo:** [`DEMO.md`](DEMO.md) — predictive. **Plan:** [`plan.md`](plan.md) — nine tasks,
`tasks/T1.md`–`T9.md`; T1–T4 and T7–T8 buildable now, T5 needs `S28-memory`'s learnings core as real code,
T6 needs `S26-roadmap`'s core as real code (which itself needs S24/S25 built first).
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — **P31** (this slice's three-way validation
verdict is the reserved seventh instance; `ARCHITECTURE.md`'s table now says eight) and **P32** are binding.
**A13** (embeddings serve discovery, never dispatch) governs duplicate detection the same way it already
governs S28's learnings and S12–S14's agent/tool discovery — not relaxed here.
**Stage:** 5 · Fleet, third of six to get a real brief (after S24, S28) — for a narrower reason than either:
*validating* a bug is mostly S5's one-step loop with a different prompt, and *fixing* a confirmed one is
S17's ladder with a different entry point. Both already exist. What is missing is small and nameable.
**Depends on:** S4 (store), S5 (one step — a validation attempt is an ordinary bounded loop), S13 (agents —
something has to run the attempt). **Scheduling a fix, once triaged, is S24's and S26's job, not this
slice's** — see §5. **Does not depend on S17 directly** — the fix loop it eventually feeds reuses S17's
ladder, but that happens after hand-off, outside this slice. **Does not depend on S22 or touch S28's
schema** — a bug report gets its own table and its own project-scoped embedding index, not a third corpus
inside either (see §5's corpus-boundary decision — the same shape S28 already drew against S22). **The
learning-feed hook (D10) is the one exception** — §7 Q1 resolved yes, so that hook alone additionally
depends on S28 existing, the same partial-dependency shape S28's own board half already has onto S14.
**Repo:** Aeon lives at `aeon/` in this repository (P24) · Go 1.26 · `cgo` enabled (B3)
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`) — already compliant: T7 builds `aeon triage` and
`triage.*` socket methods together (file, list, show, set-priority, confirm-duplicate), and `design.md`'s
own diagram already draws two filing paths — `file_bug` (agent, native tool) and a human report (CLI/socket,
not CLI-only); no fix needed.

---

## 1. What this is

**The part of bug-fixing that is genuinely missing, isolated from the parts that already exist.** Running a
loop that reproduces a bug is S5. Running a loop that fixes a confirmed one, reviewed and merged, is S17.
Neither needed inventing. What has no home yet is the question in between: **is this real, is it a
duplicate, how urgent is it** — and the honest majority answer to the first question is *"I couldn't tell."*

This slice makes that answer a first-class, queryable record instead of a closed GitHub issue that silently
meant "nobody reproduced it, so we gave up."

## 2. Definition of Done

| | |
| --- | --- |
| **D1** | **A bug's validation state is three-way**: `reproduced` / `not-reproduced` / **`could-not-tell`** — and a fourth, distinct state, `unvalidated` (no attempt has run yet), which must never collapse into `could-not-tell` (that means an attempt ran and was inconclusive; this means none has). |
| **D2** | **Priority and validation stay separate fields.** An unreproduced *or* unvalidated bug is **unknown** priority, never low — the constraint this slice already imposes on S26 (must render honest-unknown) and S27 (must not silently treat unknown as low). |
| **D3** | **A validation attempt is a durable record** — its own outcome, cost and transcript, the same shape a step already has — running through S5's ordinary loop path. No separate governance for "just a validation run." |
| **D4** | **Evidence is a loop.** Shape: `bug → attempts → loops`. An attempt's loop is an ordinary loop under G14's attendedness pin like any other — ordinary, not special-cased. |
| **D5** | **Duplicate detection suggests; a human confirms.** No verdict here ever auto-closes a bug by similarity alone. |
| **D6** | **Duplicate detection's embedding index is per-project, its own table** — not a third corpus inside S22's two, not a kind-discriminated row inside S28's learnings. |
| **D7** | **An agent may self-file a bug claim with no human present.** A self-filed bug starts `unvalidated` (D1) — never `could-not-tell`, never low priority — until an actual attempt runs. |
| **D8** | **Once validated and prioritized, the record is handed to S24's queue or S26's roadmap to schedule the fix.** This slice does not run or schedule a fix loop itself. |
| **D9** | **The fix loop, once scheduled elsewhere, reuses S17's full ladder unchanged.** This slice adds no second fix-dispatch mechanism. |
| **D10** | **A validation attempt's conclusion may be filed as an S28 Learning** — `observed` provenance (already in S28's set, no new value needed), project-scoped, automatic rather than gated by a second mechanism here: it inherits whatever trust/review gate S28's own Q3 settles for any self-logged, unattended learning. A producer into S28's existing store, never a second one. |

**D1 and D8 are the two that matter.** D1 because `unvalidated` versus `could-not-tell` is exactly the kind
of collapse P31 exists to prevent, and it is new to this slice — the original five-point sketch named the
three validation states but not the fourth, pre-attempt one. D8 because it is what stops triage from quietly
growing a second scheduler the moment someone notices "it's just one more loop to kick off."

## 3. Why it is cheaper than it looks

**Validating is mostly S5's one-step loop with a different prompt**: read the report, attempt to reproduce,
say what happened. Nothing about running that loop is new — S5 already bounds it, its tools are already
governed, G14 already pins its attendedness. **Fixing a confirmed bug is S17's ladder**, already designed,
entered from a bug record instead of a feature request — this slice does not touch it.

What is actually new is small: a bug table, a validation-state/priority split that is two fields instead of
one, a fourth pre-attempt state, and a duplicate-check embedding index scoped to one project. Everything that
makes validating or fixing *work* already exists; this slice only decides **whether a given claim is real,
and how urgent it is** — nothing about *doing* anything with that answer.

## 4. The seams

**Downward — S5 and S13.** A validation attempt runs as an ordinary bounded loop (S5); S13's registry
supplies who runs it. No second execution path.

**Sideways, outward — S24's queue and S26's roadmap.** A validated, prioritized bug record is handed off the
same way any other work item reaches scheduling. This slice materializes a loop for the *validation attempt*
only — never for the fix.

**Sideways, deliberately not inherited — S22's two corpora and S28's learnings index.** Duplicate detection
gets its own project-scoped index, for the same reason S28 already gave against S22: a bug report is neither
code, nor history, nor a cross-project learning, and squeezing it into an existing corpus would make that
corpus answer a question it was not designed to answer.

**Downward, out of this slice's hands once scheduled — S17's full ladder.** Fixing a confirmed bug is S17
with a different entry point; nothing here duplicates review, merge, or the rest of the ladder.

## 5. Locked decisions

- **A bug report is its own table, with its own project-scoped embedding index** — not a third corpus inside
  S22, not a kind-discriminated row inside S28's learnings. Same mechanism S28 already established (its own
  `sqlite-vec` corpus, reusing S4's statically-linked extension), applied a second time rather than reused by
  sharing a table.
- **Fix scheduling is not this slice's job.** Once a bug is validated and prioritized, the record moves to
  S24's queue or S26's roadmap — the same path every other work item is scheduled through. This slice
  produces the record; it does not run the fix.
- **Duplicate detection is per-project**, matching S22's existing per-root boundary. No second cross-project
  index — S28's agent-scoped learnings remains the only cross-project embedding corpus in the plan.
- **Self-filing is allowed, and it starts honestly unvalidated.** An agent may file a bug claim with no human
  present; it begins in the new `unvalidated` state (D1), distinct from `could-not-tell`, until an attempt
  actually runs. Collapsing "nobody has looked yet" into "somebody looked and couldn't tell" would make a
  flood of self-filed claims indistinguishable from a genuine triage backlog.
- **A validation attempt's conclusion feeds S28 as a Learning, not a second store.** Resolved from this
  slice's own Q1: `observed` provenance, project-scoped, automatic — gated by whatever S28's Q3 decides for
  any self-logged, unattended learning, not a separate gate invented here.

## 6. Out of scope

- **N1 — No second fix-dispatch mechanism.** S17 is invoked the normal way once a bug is scheduled elsewhere
  (D8, D9).
- **N2 — No cross-project duplicate detection.** A bug in one repo matching one in another is not caught by
  this slice.
- **N3 — No auto-closing a bug as a duplicate**, under any confidence threshold. D5 restated as a standing
  non-goal, not only a Definition of Done item, because it is the one thing most likely to be "simplified"
  back in during implementation.
- **N4 — No new embedding mechanism.** Reuses S4's statically-linked `sqlite-vec` the same way S28 does — a
  separate corpus, not a new extension or a new vector store.
- **N5 — No change to S22's or S28's schema, invariants, or corpora.**

## 7. Questions to answer before building

| # | Question | Where |
| --- | --- | --- |
| Q1 | **Resolved — yes.** A validation attempt's conclusion becomes an S28 Learning (`observed`, project-scoped, automatic), gated by whatever S28's own Q3 settles rather than a second gate here. See D10 and §5. | resolved |
| Q2 | The exact closed status vocabulary for a bug record — `unvalidated` / `reproduced` / `not-reproduced` / `could-not-tell` crossed with priority's own three-state (known / assumed / unknown), or a different shape entirely? | design.md |
| Q3 | What triggers a validation attempt to actually run — immediately on filing (even unattended), or does it wait on a human/gate decision, mirroring S24's `queueAdvance: auto \| ask` split? | design.md |
| Q4 | What exactly gets handed to S24/S26 at D8's hand-off — a fully-formed queue item in S24's own shape, or a triaged-bug record a person still has to turn into one by hand? | design.md |

## 8. Risks

- **R1 — Triage quietly grows its own scheduler.** "It's just one more loop to kick off" is exactly how a
  second dispatch path gets built by accident. **The guard is D8**: hand off to S24/S26, never a parallel
  path.
- **R2 — Self-filed bugs flood the record with unreviewed noise.** With no human gate at filing time (the
  locked decision), volume could look like urgency. **The guard is D1/D7**: a self-filed, unattempted claim
  renders as `unvalidated` and **unknown priority** — many unknowns, never many urgent bugs, until something
  actually validates them. The same shape of risk applies to D10's learning feed, guarded the same way — by
  inheriting S28's own Q3 gate rather than adding a second one here.
- **R3 — Duplicate suggestion becomes duplicate dispatch.** The first time someone wants to "reduce noise,"
  auto-closing looks tempting. **The guard is D5/N3**: a human confirms every single time, no threshold
  exception.
- **R4 — The two splits collapse somewhere downstream.** Validation-state-versus-priority (point 2 of the
  original sketch) and `unvalidated`-versus-`could-not-tell` (D1, new here) are both easy to flatten in a
  renderer that "just wants a status badge." **The guard**: assert both splits render distinctly, the same
  test shape S24 and S28 already use for their own three/four-state fields.

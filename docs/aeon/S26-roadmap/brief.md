# Brief — S26 Roadmap

**Implements:** [`design.md`](design.md). **Demo:** [`DEMO.md`](DEMO.md). **Plan:** [`plan.md`](plan.md) —
seven tasks, `tasks/T1.md`–`T7.md`. **Promoted from [`SKETCH.md`](SKETCH.md)** 2026-10-26. **One correction
to the sketch along the way** (§2): "ideation needs no new engine" assumed a `conversational-map`-shaped
*blueprint*; S8's own `design.md` (GAP-7) already found that shape's iterative phases are **not** a
blueprint's shape at all — "a chat with tools," which changes the mechanism, not the conclusion.
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — P31/P32 binding throughout (priority and
estimate are both multi-state, never bare). **Stage:** 5 · Fleet. **Depends on:** S8 (blueprints), S9 (the
app), S13 (the `project-planner` handler body, built), S24 (queue — specced, `design.md`/`plan.md`/9 tasks),
S25 (history — specced, `design.md`/`plan.md`/6 tasks). **Soft relationship, not a dependency**: S29 Triage
already locked a requirement on this slice before this slice had a brief (§3 D2) — this slice does not need
S29 to exist, but when it does, this slice's priority handling was designed to satisfy S29's own words.
**Repo:** Aeon lives at `aeon/` in this repository (P24) · Go 1.26 · `cgo` enabled (B3)
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`) — already compliant: T5 builds `aeon roadmap` and
`roadmap.*` socket methods combined, 1:1, with no independent validation logic in either; no CLI-only gap
found.

---

## 1. What this is

**The surface where a project is planned rather than executed**: an idea is discussed until it is agreed,
becomes a brief, becomes a plan, and agents build it. The user is a product manager here, iterating on a
feature until it is good — not an operator watching a loop run.

**Two halves.** Ideation → brief (§2 corrects how). The forward Gantt — the new part — is S25's own
component, pointed the other way: history occupies the past, this slice adds the future, and the present is
wherever a loop is actually running (S25's own framing, inherited unchanged).

## 2. The correction: ideation is an ordinary loop, not a blueprint

**The sketch's claim**: *"A chat that iterates toward an agreed brief is a `conversational-map`-shaped
blueprint whose `produces:` is `brief.md`."* **What S8's own investigation already found** (`design.md`
GAP-7, quoted in full): `conversational-map`'s iterative phases — *"when the user triggers a push" / "a
review"* — are an **open** set a blueprint's closed `answers` cannot hold, and the finding's own conclusion
is **"No extension proposed... they are a chat with tools, which S5, S6, S11 and S16 already provide."**

**Consequence**: ideation is not a multi-lane blueprint with gates. It is an ordinary loop — potentially a
single step, potentially several, using whatever existing lanes genuinely are sequential (if any) — that
ends when the person is satisfied, the same chat-with-tools shape GAP-7 already named. **This is simpler
than the sketch implied, not more complex**, and it resolves the sketch's own Q3 for free: *"does the
ideation chat share the loop chat's surface?"* — it is a loop, so by S9's U18 (*"the thread is per loop...
one stream per loop is what makes 'what happened, and what does it want' a single read"*) it already has
one, with no new UI surface needed for the conversation itself.

## 3. Definition of Done

| | |
| --- | --- |
| **D1** | **Ideation is an ordinary loop that ends by producing `brief.md`**, not a new blueprint shape — §2's correction, made concrete. No new engine, no new gate type |
| **D2** | **Priority is three-state — `known` / `assumed` / `unknown` — never collapsed to a number or an absence.** This satisfies S29's own locked words, quoted verbatim: *"An unreproduced or unvalidated bug is unknown priority, never low — the constraint this slice already imposes on S26 (must render honest-unknown)"* (`S29-triage/brief.md` D2) |
| **D3** | **An estimate is a range with its basis, never a bare figure** — the same shape S24's sizing (D8) and S22's reindex estimate already use. P32 applies to any roll-up of them, the same way S25's rollups floor on `unknown` cost |
| **D4** | **"Ready" is this slice's own definition, S27 only consumes it.** Adopted from S27's own sketch, which already wrote it attributed to this slice: *"a brief is agreed, dependencies are complete, and priority is not `unknown`."* This slice owns the definition; S27 (not yet specced) is the one and only consumer named so far |
| **D5** | **An item carries its provenance**, closed vocabulary — `authored` (a person wrote it), `triggered` (S19/S20's existing `trigger` action started the loop that produced it), `triaged` (S29's hand-off, D8 there) — never silently presented as if a person typed it when a trigger or a triage record did |
| **D6** | **An item has its own status, separate from whatever brief/plan/loop it points to** — the same separation S24's D10 already established for queue items, applied here: before a brief exists there is no brief to read status from, and the two must not collapse into one field |
| **D7** | **`cancelled` is not `failed`.** An item whose brief was agreed and then the project changed direction is `cancelled`, a distinct, honest outcome — never rendered as if the idea itself failed |
| **D8** | **The roadmap is never required to run a loop.** `aeon loop start` works identically with zero roadmap items in the project — asserted directly, not just claimed |
| **D9** | **Not a second planner.** `project-planner` (S13, built) owns `plan.md`'s actual content; this slice holds *what is next and why*, the plan holds *how*. An item points at a plan; it never contains one |
| **D10** | **The roadmap renders S24's queue ordering and S25's history, where either exists — it builds neither.** No second ordering to keep in step with S24's; no second record to keep in step with S25's |

## 4. The seams

**Downward — S8's blueprint format, unchanged.** Ideation uses existing lane/step machinery (D1); this
slice adds no new blueprint shape, no new gate type, no new `produces:` kind beyond what S8 already allows.

**Downward — S13's `project-planner`.** Dispatched exactly like any other handler body (the same pattern
S24's T5 sizing handler uses) once a brief is agreed; this slice does not reimplement planning.

**Sideways — S24's queue, S25's history, where they exist.** Rendered, not duplicated (D10). A project with
no queue and no history yet still has a working roadmap — S26 degrades to "just the forward Gantt," never
refuses to function.

**Sideways — S29 Triage, optionally.** D2/D5's `triaged` provenance is the hand-off point `S29-triage/brief.md`
already named (*"scheduling a fix... is S24's and S26's job"*). This slice does not require S29 to exist to
be built or used.

**Upward — S27 Continuous.** Consumes D4's "ready" definition and nothing else; this slice builds no
scheduler.

## 5. Locked decisions

- **Lanes: by epic, by priority, user-selectable — not by agent.** The sketch asked "by epic, by area, by
  agent, by priority." "By agent" is a *built-loop* concept (S25 already renders it, once an item becomes a
  loop); a not-yet-built roadmap item has no agent to group by. "By area" folds into "by epic" rather than
  being a second, overlapping grouping with no settled distinction from it — resolved here rather than left
  for whoever builds the query, the same "measure/decide once" discipline S25's H1 used.
- **D1 is the slice's one real engineering decision; everything else is composition.** Confirmed by reading
  S8's own GAP-7 rather than assumed from the sketch's optimistic framing.
- **D4's "ready" definition is owned here, worded exactly as S27's sketch already wrote it**, not
  independently re-derived — a second definition of "ready" arrived at separately would be exactly the
  disagreement-in-waiting this brief exists to prevent.
- **Provenance (D5) borrows existing closed vocabularies — `trigger` (S19/S20) and triage's hand-off — not
  new invented terms like "webhook-created."**

## 6. Out of scope

- **N1 — No new blueprint shape or gate type.** D1; ideation is a loop, not a new protocol.
- **N2 — No cross-project roadmap.** A project is a repo, the same locked decision S24/S25/S27 use.
- **N3 — No scheduler, no auto-start of anything.** S27's job, consuming D4 only.
- **N4 — No second ordering mechanism when S24's queue exists**, and no second record when S25's history
  exists (D10).
- **N5 — No plan content.** `project-planner`/`plan.md` stays S13's (D9).

## 7. Questions to answer before building

| # | Question | Where |
| --- | --- | --- |
| Q1 | Exact item status vocabulary and transition table (mirroring S24's seven-value closed set for queue items) — `idea` / `ideating` / `brief-agreed` / `planned` / `ready` / `running` / `done` / `cancelled`, or a narrower set? | design.md |
| Q2 | Does an `epic` need its own row (a lightweight grouping entity), or is it a free-text tag on an item, the same shape `project.tags` already uses? | design.md |
| Q3 | Where exactly does ideation's loop end — does reaching `brief-agreed` require a specific blueprint `produces:` declaration the daemon already checks, or a new, narrow check this slice adds? | design.md, grounded in S8's actual `produces:` enforcement |

## 8. Risks

- **R1 — Reinventing `conversational-map` as a real blueprint anyway**, because the sketch said to and GAP-7
  is easy to miss. **The guard is §2, read first by whoever builds T1.**
- **R2 — Priority silently becomes a number somewhere downstream** (a UI sort column, an export). **The
  guard is D2, tested the same way S25's H6 tests the cost floor**: a fixture with an `unknown`-priority
  item must render as unknown everywhere the list appears, not just in the primary view.
- **R3 — "Ready" drifts between this slice's definition and S27's eventual consumption of it.** **The guard
  is D4's verbatim adoption** — when S27 is specced, its own brief should quote this slice's definition
  rather than restate it from memory.

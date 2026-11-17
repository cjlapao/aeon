# Brief — S25 Project History

**Implements:** [`design.md`](design.md). **Demo:** [`DEMO.md`](DEMO.md). **Plan:** [`plan.md`](plan.md) —
six tasks, `tasks/T1.md`–`T6.md`. **Promoted from [`SKETCH.md`](SKETCH.md)** 2026-10-26, kept to the
sketch's own original scope — a pure read, nothing new stored. Rich per-turn telemetry (token composition,
timing, file/command activity) is **deliberately out of this slice**; it is **S33 Telemetry**'s job, a
separate slice created in the same sitting specifically so this one would not have to carry it (§6 N1).
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — P31/P32 (three-state honesty, an aggregate
over uncertain values is itself uncertain) are binding throughout. **Stage:** 5 · Fleet. **Depends on:** S4
(the store — including `loop_archive`/`loop_archive_file` and `ArchiveLoop`/`ListArchives`, which shipped
with S4's migration `0004_documents_and_archive`, **not S17** as the original sketch's phrasing would
suggest), S9 (the app, for the Gantt component this reuses). **One caveat, not a dependency**: nothing in
the CLI or daemon calls `ArchiveLoop` today (checked — no call site outside the store package), so a real
archived loop does not exist outside a test fixture yet; see §7 Q4. **Repo:** Aeon lives at `aeon/` in this
repository (P24) · Go 1.26 · `cgo` enabled (B3)
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`): T4 already builds `history.*` including
`history.export` as read-only socket twins of the CLI's `list`/`show`/`export`, matching D1/D2/D6. H3's
wording below is corrected to say so — it previously read "through the CLI, not the socket," which
understated what T4 already does.

---

## 1. What this is

**The cross-loop, time-ordered projection of everything a project has done.** Every loop by recency,
drilling down to phases, tasks, steps, handoffs and costs — the same data S9 already renders **per loop**,
projected **across** loops and **through** time. One Gantt-shaped surface: history occupies the past, S26
(Roadmap) adds the future, and the present is wherever a loop is actually running — same component, two
directions (the sketch's own framing, unchanged).

**A read and a view. Not a subsystem.** This slice stores nothing of its own (§5). Everything it shows
already lives in `loop`, `loop_phase`, `task`, `step`, `step_message`, `event`, and — the one fact the
original sketch could not have accounted for, since it predates archival's own design settling — **`loop_archive`/`loop_archive_file`**
(S4's cold-storage tables, dormant: built, but nothing calls them yet — §7 Q4). A loop archived out of the
live tables (`archive.go`: *"An archived loop is GONE from the live tables — the header plus its files are
the whole record"*) must still appear in a *project's* history, or this slice's own headline claim — *"the
cross-loop, time-ordered projection of everything this project has done"* — stops being true the day the
first loop is archived. §3's D7 is this finding made a requirement.

## 2. Why it comes before the roadmap, restated from the sketch

Unchanged from `SKETCH.md`: it is useful alone (*"what has this thing actually done for me"* needs no
planning surface at all); the roadmap without it is a plan with no context; and it is what lets a project
adopt a roadmap at any point without backfilling months of intent it never has to invent.

## 3. Definition of Done

| | |
| --- | --- |
| **D1** | **A project-wide, time-ordered view over every loop**, past and present, grouped by loop, phase, task or agent (H1, `design.md`) |
| **D2** | **Drill-down reaches the step transcript** the same way S9's per-loop view already does. Reading one deliberately is not embedding all of them — S22's V15 governs indexing, never plain reads (H2, quoted in full in `design.md`) |
| **D3** | **Every aggregate number carries its uncertainty**, never collapsed to a bare figure (P32). A total over a range containing `unknown`-cost steps renders "at least $X," the same floor S9 already applies per loop, now applied across many |
| **D4** | **A `blocked` loop renders its stored reason verbatim** (`LastReason`/`BlockedReason.Detail`), never a generic "failed" — S24's own finding that `blocked` conflates a retriable stall with an exhausted one applies here too: this slice does not invent a verdict the runtime itself does not assert |
| **D5** | **This slice computes nothing that is not derivable from existing rows.** No new table, no new column, no cached aggregate that could disagree with the store it was computed from |
| **D6** | **Export is a dataset, not a picture.** `aeon history export` emits the same rows the view renders, in a documented shape — a chart is a rendering choice downstream of this, never the other way round |
| **D7** | **History does not stop at the archive boundary.** A query over "this project's history" reads live `loop`/`step`/… rows AND `loop_archive`/`loop_archive_file` rows, merged into one time-ordered sequence — never two separate screens a person has to remember to check both of |

## 4. The seams

**Downward — S4's store, read-only.** Every query in this slice is a `SELECT`. `internal/store` gains no
write path on this slice's account.

**Downward — S4's archive tables.** `ReadArchive`/`ListArchives` are the second source D7 requires; this
slice adds no new archive mechanism, it reads the one S4 already built — and this slice's T1 fixtures are
likely the first thing in the whole project to actually call `ArchiveLoop` (§7 Q4).

**Sideways — S9's Gantt/lane rendering.** The per-loop lane component S9 built is reused, not reimplemented,
for the cross-loop view — the sketch's "same component, two directions" is literal reuse, not a lookalike.

**Upward — S26 Roadmap.** Consumes this slice's projection as the "past" half of its combined Gantt (the
sketch's own framing); S25 builds no forward-looking surface itself.

**Explicitly not a seam — S33 Telemetry.** The two slices read the same store but never each other's output.
S33's rollups answer "how much context/tokens/tool activity did this cost," which this slice does not
show and does not need to — see §6 N1.

## 5. Locked decisions

- **Not a second source of truth.** Carried from the sketch, unchanged and now binding: the moment history
  has its own table, the store and the view can disagree, and the view is what people would believe.
- **H1 — lanes are loop, phase, task, agent, user-selectable, one at a time.** All four groupings are
  already columns on rows this slice reads (`step.phase`, `step.taskid`, `step.agent`, `loop.slug`); no
  grouping requires a join this slice's read path does not already need.
- **H2 — drill-down to the transcript is in scope, V15 does not apply.** `S22-retrieval/design.md`'s own
  words: *"nothing is indexed that has not passed through something that could redact it... never
  `step_message`."* That is a rule about **indexing**. This slice indexes nothing — it reads a row on
  request, the same thing S9 already does for the loop currently open.
- **H3 — export is NDJSON, one row per line, the same shape the view's own query returns** — so the
  exported file and the on-screen table can never silently diverge. **Reachable from both doors** (P34,
  amended 2026-10-26): `aeon history export` and the socket's `history.export` call the identical
  `internal/history` function; T4 already builds the socket method (this brief's original wording claimed
  CLI-only — corrected, since the brief is written to match what T4 actually builds). An app-only user
  needs a way to get this file without a terminal.
- **H4 — `blocked` renders its stored reason, never a synthesized verdict.** See D4.
- **H5 — archived loops are read through `loop_archive`, not resurrected into `loop`.** No "undelete,"
  no copy-back; a second reader of a second table, merged at render time only.
- **H6 — P32 is binding here with no carve-out.** A rollup over `N` steps where any subset is `unknown`
  renders the three-state floor, labeled, every time — this is the slice's single most load-bearing rule,
  because an aggregate is exactly where six slices' worth of refused shortcuts would otherwise arrive at
  once (the sketch's own words).

## 6. Out of scope

- **N1 — No per-turn telemetry: token composition, timing breakdowns, file/command activity stats.**
  That is **S33 Telemetry**, a new slice, deliberately separated out so this one stays a pure read. See
  `S33-telemetry/brief.md`.
- **N2 — No forward Gantt or roadmap rendering.** S26 Roadmap.
- **N3 — No cross-project view.** A project is a repo (the same locked decision S24/S27 use); this slice
  is per-project, the same scope S4's own tables already enforce.
- **N4 — No new store writes, ever.** D5.
- **N5 — No summarization that collapses an uncertain value.** D3/H6.
- **N6 — No `ArchiveLoop` trigger.** This slice reads the archive mechanism S4 already built; it does not
  decide when a loop gets archived or add the CLI/daemon call path that would make that happen in ordinary
  use (§7 Q4) — that belongs to whichever slice owns "a loop is truly finished." The original guess here
  was "most likely S17"; **checked 2026-10-26, S17 is now built and did not add it** — genuinely unowned.

## 7. Questions to answer before building

| # | Question | Where |
| --- | --- | --- |
| Q1 | Does a cross-loop "group by task kind" or "group by agent" query need a new index (`task(project_root, kind)`, `step(project_root, agent, id)`), or do the existing `idx_step_loop`/`idx_step_status` indexes cover it well enough to measure before adding one? | design.md, measured in the task that builds the query |
| Q2 | Exported transcripts can contain whatever a tool run happened to echo — is a raw `step_message` export in scope for `aeon history export`, or does it need the same redaction-awareness §4c's credential rule implies for the UI, extended to a new surface? | design.md |
| Q3 | For an archived loop, does drill-down read the full `loop_archive_file` content (potentially large, decompressed) on every request, or does the view show only the archive header by default with content fetched on demand — the same "content read before the transaction, not during" discipline `ArchiveLoop` itself already follows? | design.md |
| Q4 | Nothing in the CLI or daemon calls `ArchiveLoop` today (**still true 2026-10-26, re-checked now that S17 is built** — S17 did not add it) — this slice can still build and test D7 against fixtures that call the store method directly, but a real end-to-end demo cannot show an archived loop produced through ordinary use. Resolved: out of scope — see `design.md`'s Q4. | resolved, design.md |

## 8. Risks

- **R1 — The archive merge (D7) silently becomes two screens.** The natural shortcut is "live loops in one
  view, archived loops in another, with a link between them." **The guard is D7 itself, read literally**: one
  time-ordered sequence, not two surfaces a person has to remember to check both of.
- **R2 — An aggregate ships without its uncertainty floor "just this once."** A dashboard wants a number,
  and P32 is most tempting to break exactly where this slice lives (the sketch's own warning). **The guard is
  H6**, and the task that builds any rollup must test the floor, not just the happy path.
- **R3 — Export becomes a second query path that can drift from the view's own.** **The guard is H3**: the
  exported rows and the rendered rows come from the identical query, asserted by a test that runs both and
  diffs.

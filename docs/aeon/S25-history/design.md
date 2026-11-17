# Design — S25 Project History

**Brief:** [`brief.md`](brief.md). **Plan:** [`plan.md`](plan.md). **Demo:** [`DEMO.md`](DEMO.md).

**Written 2026-10-26**, against the real codebase, including the archive mechanism
(`internal/store/archive.go`, S4's migration `0004_documents_and_archive` — **not S17**, corrected after an
initial misattribution while writing this design; the sketch's own 2026-09-07 phrasing invited the mistake,
naming S17 as where "a declined plan is a working system" was established, which is a real S17 fact about a
different topic) which is the source of this design's one genuinely new finding (§2).

## 1. Context

`SKETCH.md` asked four open questions. Three (lanes, transcript drill-down, exportability) this document
answers directly. The fourth (aborted-loop rendering) turned out to be a special case of something S24's own
investigation already found: `loop.status` has no `CHECK`, and `blocked` conflates a retriable stall with an
exhausted one. This design does not re-litigate that finding, it reuses it (D4/H4).

## 2. The finding the sketch could not have had: archival

`archive.go`'s own comment: *"An archived loop is GONE from the live tables — the header plus its files are
the whole record."* `ArchiveLoop` deletes `loop` (and, by cascade, every `step`/`step_message`/`event`-ish
row FK'd to it) in one transaction, after reading everything it needs into a JSON blob
(`loop_archive`/`loop_archive_file`). **A naive cross-loop query over `loop` alone silently stops showing a
project's older work the day its first loop is archived** — and nothing about that failure is loud. The
query still returns rows; it just returns fewer of them than the project's real history, forever, with no
error anywhere. This is why D7 exists and why it is phrased as "one time-ordered sequence," not "a view,
plus an archive tab."

**Mechanically:** `ListArchives(ps ProjectScope)` already returns `Archive` headers (`ID, Slug, ProjectRoot,
Title, Goal, Status, StartedAt, EndedAt, ArchivedAt, StateBytes, FileCount, FileBytes, Note`) — enough to
place an archived loop correctly in the time-ordered sequence and show its headline facts without touching
`loop_archive_file` at all. Drill-down into an archived loop's actual transcript is the one place this design
reads `loop_archive_file`, and only on request (Q3, resolved below).

## 3. Decisions

### H1 — lanes

Four groupings, one active at a time, switched by the viewer: **by loop** (default — `loop.slug`, recency
order, matching `idx_loop_updated`), **by phase** (`loop_phase`/`step.phase`), **by task** (`task.id`/
`step.taskid`), **by agent** (`step.agent`). All four are plain columns on rows this slice already reads for
the default view — no new join shape, only a different `GROUP BY`.

### H2 — transcript drill-down, V15 does not apply

Quoted in full in the brief: V15 is a rule about what gets embedded into S22's search corpus, stated as a
property specifically so indexing decisions never need a case-by-case re-litigation. This slice embeds
nothing. Reading `step_message.content` on an explicit drill-down request is the same action S9 already
performs for the currently-open loop; this slice performs it across more loops, which changes volume, not
kind.

### H3 — export, NDJSON via CLI

`aeon history export --project-root … [--since …] [--group-by …]` streams one JSON object per line, each
one exactly the row shape the view's own query produces — enforced by a test that runs the view's query and
the export's query against the same fixture and diffs the two outputs byte-for-byte (R3's guard, made
concrete).

### H4 — `blocked` renders its stored reason

A loop whose current or final status is `blocked` shows its `LastReason` (or, where the richer
`BlockedReason{Kind, Detail}` shape is available on the event stream, that) **verbatim** — never translated
into "failed," "aborted," or any other word this slice would have to invent. This mirrors the project's
standing one-formatter discipline (`Verdict.Sentence()`, `InForceAnswer.Render()`): a second place that
decides how to phrase an outcome is a second truth, and this slice declines to be it.

### H5 — archived loops via `loop_archive`, never resurrected

`ListArchives` and the live `ListLoops`/equivalent are two queries, merged by timestamp at render time into
one sequence. Nothing copies an archive row back into `loop` to make the query simpler — that would recreate
exactly the state `ArchiveLoop`'s own transaction exists to remove.

### H6 — P32's floor, applied across many loops

The existing per-loop floor (S9: *"cost per step and in total ('at least $…' when a step's accounting is
incomplete)"*) generalizes directly: a rollup over `N` steps across `M` loops with any `unknown`-cost member
renders "at least $X across N loops (K steps unknown)" — the count of unknowns travels with the number, not
a footnote a person has to go find.

### Q1 (brief §7) — indexing, resolved as "measure before adding"

`idx_step_loop (project_root, loop_slug, id)` and `idx_step_status (status, id)` exist today; neither
indexes `phase`, `taskid` or `agent`. **This design does not pre-emptively add an index.** The task that
builds the "group by task/agent" query runs it against a fixture sized like a real multi-month project first
and adds an index only if the query plan shows a table scan where one would help — matching this project's
"measure, don't assume" discipline (AGENTS.md §5, applied to the store elsewhere by S4/S22).

### Q2 (brief §7) — export redaction, resolved as "named, not solved, by this slice"

**This design does not add a redaction pass.** `§4c`'s credential rule governs *Aeon's own* route API keys
crossing the web layer — a different, narrower thing than "whatever a tool happened to print to stdout,"
which this project has never promised to scrub (no slice claims it, and `step_message` already stores it
unredacted today, read openly by S9 per-loop). Exporting what is already readable one loop at a time, now
many at once, changes volume, not a promise this slice would be the first to break. **Named explicitly so a
reader does not assume it was overlooked**: if the project later wants transcript redaction, it is a
store-wide concern above this slice, not something `history export` should invent on its own.

### Q3 (brief §7) — archived-loop drill-down, resolved as "header first, content on demand"

The time-ordered view shows an archived loop's `Archive` header (title, goal, status, started/ended) with no
`loop_archive_file` read at all — matching `ArchiveLoop`'s own "read content before the transaction, decode
it as little as possible" discipline, applied symmetrically on the way out. Fetching the actual archived
transcript is a second, explicit drill-down action, exactly mirroring D2's live-loop behaviour.

### Q4 (brief §7) — the dormant trigger, resolved as "out of scope, named plainly"

This slice does **not** add `aeon loop archive` or any other trigger for `ArchiveLoop`. Wiring that call is
a decision about *when a loop should be archived* — the original guess here was "almost certainly S17's,
since 'the full ladder' is what decides a loop is truly finished." **Checked, 2026-10-26: S17 is now built
and did not add it** — `ArchiveLoop` still has zero callers anywhere in the tree. The guess was wrong, not
just unconfirmed. This remains genuinely unowned, open to whichever slice actually wires it next, and is
not this slice's to make by accident while building a read surface.
**Consequence for T6's demo**: the archived-loop fixture is created by calling `ArchiveLoop` directly in the
walk's setup (the same thing T1's own tests do), not through any CLI verb, and the walked `DEMO.md` section
must say so plainly rather than imply a command exists that does not.

## 4. Architecture

```
            group-by: loop | phase | task | agent
                       │
                       ▼
              internal/history (new, read-only package)
                       │
        ┌──────────────┼───────────────────┐
        ▼                                   ▼
  live rows (loop, loop_phase,       archived rows (loop_archive,
  task, step, step_message,          loop_archive_file — header by
  event) — unchanged reads           default, content on drill-down)
        │                                   │
        └──────────────┬────────────────────┘
                        ▼
              merged, time-ordered projection
                        │
           ┌────────────┼─────────────┐
           ▼                          ▼
     aeon history (CLI,         history.* (socket,
     incl. export)              read-only, incl. export —
                                 S9's Gantt component, reused)
```

## 5. Testing this slice has to do

- **D7's merge, directly**: a project with one live loop and one archived loop returns both, correctly
  ordered, from one call — not two the caller has to combine itself.
- **H6's floor, as a property**: any rollup fixture containing an `unknown`-cost step renders the floored
  form; a fixture with none renders a bare number — both asserted, so the floor is proven present, not
  merely never triggered.
- **H3's parity**: the export and the view query the same fixture; outputs diff to nothing beyond format.
- **H4's verbatim-ness**: a `blocked` loop's rendered reason matches its stored `LastReason` byte-for-byte —
  no synthesized word appears anywhere in the rendering path.
- **D5 by absence**: no `CREATE TABLE` touches this slice's migration count — asserted the same way S30's T1
  asserted "no vector column," here asserting "no new table at all."

## 6. Amendments to earlier slices

None. S4's archive tables are read exactly as built; S9's Gantt component is reused exactly as built;
S22's V15 is cited, not changed.

## 7. Open questions this design did not close

- **The exact `--since`/date-range shape for `aeon history export`** is a CLI-ergonomics call the task
  building it should make and record, not a modelling question.

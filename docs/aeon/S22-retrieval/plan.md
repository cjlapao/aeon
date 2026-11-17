---
slice: S22-retrieval
title: Retrieval
stage: 4
stage_name: Space shuttle
position: 3 of 4
brief: docs/aeon/S22-retrieval/brief.md
design: docs/aeon/S22-retrieval/design.md
demo: docs/aeon/S22-retrieval/DEMO.md
depends_on: [S4-store, S5-one-step, S18-providers]
independent_of: [S19-narrow-surface, S23-first-run]
optional: true
task_count: 9
---

# Plan — S22 Retrieval

**The only optional slice in the plan**, and the claim is load-bearing: *"the product must work fully without it."* So the
deletability test lands with the **first** task rather than the last (R4) — a build configuration added at the end would
find imports accumulated across eight tasks.

Memory is the thing most likely to become quietly load-bearing, which is why §1 of the demo takes the slice away before
showing what it does.

## Task sequence

| Task | Deliverable | Gate |
| --- | --- | --- |
| **T1** | The index tables, the four availability reasons, **and the no-retrieval build** | The suite passes with the package deleted |
| **T2** | Chunking, and per-chunk staleness hashes | A chunk spanning two symbols retrieves for neither — and does not |
| **T3** | Embedding as a route, with cost through S18 | An unmetered embedding route is `unknown` with a reason |
| **T4** | The code corpus, FTS5, and hybrid fusion | Structural queries won by FTS5, conceptual by vectors |
| **T5** | The loop-history corpus — artefacts and handoffs only | `step_message` never indexed, asserted two ways |
| **T6** | The `search` tool, registered only when built | `file:line` only; the description says it complements `grep` |
| **T7** | Context resolution feeding the manifest | A resumed step's manifest is identical; the spy is at zero |
| **T8** | Mismatch refusal with a cost estimate; async reindex and staleness | A mismatch reindexes nothing |
| **T9** | Demo walk, QA suite | §1 passes with the package gone |

## Ordering rationale

- **T1 first, carrying the deletability test** (R4). D1 is the slice's claim, and it is cheapest to keep true from the
  first commit. **The index and its availability model are one task** (R5) — the four reasons are properties of the index's
  state, and a model bolted on afterwards is one shaped by whatever the index happened to do.
- **T2 before the corpora** because both chunk, and two chunkers is how V11's heuristic drifts.
- **T3 before T4** — a vector corpus needs an embedding path, and building the corpus first means a stub that gets kept.
- **T4 before T5** because hybrid fusion is proven on code, where both failure modes are visible, before being applied to
  prose.
- **T5 after T4** and separate, because V15's rule is the whole content of the task: **what stood between the content and
  the index.**
- **T6 after the corpora** (R7) — its conditional registration is D1's other half, and it needs something to search.
- **T7 after T4 and T5** (R6). It is the differentiator, and the place K13's freeze is either honoured or broken.
- **T8 after T3** for the cost estimate and after T4/T5 for staleness per corpus.
- **T9 last.**

## Files

| File | Tasks | Reason for overlap |
| --- | --- | --- |
| `internal/retrieval/index.go`, `availability.go` | T1 | One task; the reasons are the index's states (R5) |
| `internal/store/migrate.go` | T1 | The vec and FTS5 migration. **Q1: a virtual table may not compare equal under S4's normalised check** |
| `Makefile`, `.github/workflows/ci.yml` | T1 | The no-retrieval build configuration (O1) |
| `internal/retrieval/chunk.go`, `stale.go` | T2 | Chunking and its hash travel together |
| `internal/retrieval/embed.go` | T3 | Uses S18's route; no second provider path |
| `internal/retrieval/code.go`, `hybrid.go` | T4 | Fusion is proven on the code corpus |
| `internal/retrieval/history.go` | T5 | — |
| `internal/tools/search.go`, `class.go`, `testdata/rerun_classes.json` | T6 | The tenth tool; the committed list's `search` entry becomes present |
| `internal/retrieval/resolve.go` | T7 | — |
| `internal/runtime/compile.go` | T7 | Sole change: the manifest entry's resolution provenance (V14) |
| `internal/retrieval/reindex.go` | T8 | Async, per-corpus staleness, mismatch refusal |
| `docs/test_suites/aeon-retrieval.md` | T9 | Amended, never rewritten |

## The two permanent refusals

**No transcript indexing, ever** (V15, N6). Not behind a flag, not for a trusted project. `step_message` holds tool output
no redaction pass has seen.

**No re-resolution on resume or recovery** (D6, N7). It would make a manifest meaningless — a resumed step could get a
different context from the one that suspended.

## What this slice does not do

A per-language parser (V11's heuristic is most of the benefit) · a filesystem watcher (the loop knows it wrote) · an
external vector database (one install) · a reranker (unmeasured value, a model call per query) · cross-project search (per
root) · **transcript indexing** · **re-resolution on resume** · a second provider path · a new cost state.

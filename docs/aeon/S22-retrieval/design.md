# Design — S22 Retrieval (Stage 4 · Space shuttle)

**Date:** 2026-09-07
**Status:** approved (design); not yet implemented
**Stage:** 4 · Space shuttle, third of four
**Depends on:** S4 (the store and `sqlite-vec`), S5 (context declaration), S18 (routes and cost).
**Independent of S19, S23.**
**Inherits:** V1–V12 from [`docs/_archive/aeon-v1-horizontal/11-retrieval`](../../_archive/aeon-v1-horizontal/11-retrieval/design.md). New decisions start at **V13**.

---

## Context

**This is the only genuinely optional slice in the plan**, and that is a load-bearing claim rather than a modest one:
[`README.md`](../README.md) says *"S22 is the only genuinely optional slice, and the product must work fully without it."*

Everything else in Stage 4 extends reach. This one adds memory, and memory is the thing most likely to become quietly
load-bearing — a handler that reaches for `search` and gets nothing concludes the repository has nothing.

### V1's reasoning is superseded, and the failure model shrinks

V1 chose `sqlite-vec` in the store's own database, *"verified: `node:sqlite` on Node 24.12 exposes `enableLoadExtension`
and `loadExtension`."* That verification is a Node fact and no longer applies.

**S4's DB18 settled it better.** `sqlite-vec-go-bindings/cgo` vendors `sqlite-vec.c` and registers it through
`sqlite3_auto_extension()`, so it is **compiled into the binary and present on every connection.** There is no
extension-missing state.

So the availability model loses a reason, and S4 already enumerated what remains (V19):

| Availability reason | Means |
| --- | --- |
| `no-provider` | No embedding route configured — **keyword-only, and it says so** (V9) |
| `index-empty` | Nothing indexed yet |
| `model-mismatch` | The index was built with a different embedding model (V6) |
| `dimension-mismatch` | Same, by dimension |

**Four, not five.** V1's conclusion holds; its evidence does not.

### Absent is not the same as empty

The optionality claim needs a sharper statement than *"it degrades"*, because V9 already covers degradation — no embedding
provider means keyword-only, said out loud.

**Absence is a different thing.** If retrieval is not built in, or is switched off, the `search` tool must **not exist**
(V13) rather than exist and return nothing.

The reason is that **an empty result is a claim about the repository.** A handler that searches for *"where is the retry
policy"* and gets nothing reasonably concludes there isn't one, and moves on. A handler that has no `search` tool reaches
for `grep`, which is what V5 wanted anyway: *"semantic search complements `grep`; it never replaces it."*

This is the same reasoning S19's N14 used for operational control — absent rather than refusing — arriving in a different
place for the same underlying reason: **a capability that answers unhelpfully is worse than one that is legibly not
there.**

### The differentiator is context resolution, and it composes awkwardly

V7 names the real prize: *"retrieval can feed context declaration, not only answer a tool call… resolving 'this step needs
the code relevant to T3' at assembly time. This is the differentiator; the tool is the obvious half."*

That was written before context declaration was built. Now it is, and two decisions constrain it:

- **K13**: the manifest is **frozen** at declaration.
- **K14** and S7: a resumed or recovered step **never re-resolves** declared context; it reports what changed.

So retrieval feeds the manifest **once, at declaration, and never again.** That composes cleanly — but it creates a
problem the tool half does not have.

**A retrieval-resolved manifest can be wrong.** Similarity picked those files; the step may be missing the one it needs.
A declared manifest is a human's or a plan's assertion; a retrieved one is a guess.

**So a manifest entry records how it was resolved** (V14) — declared, or retrieved with the query that found it. Because a
model that knows its context was guessed will verify with `grep`; a model that thinks it was declared will trust it. Same
information, opposite behaviour.

### What may be indexed

`step_message` is **never indexed**. The inherited reason is unchanged and worth keeping verbatim: **transcripts hold tool
output no redaction pass has seen.**

Generalised into a property (V15): **nothing is indexed that has not passed through something that could redact it.**

| Indexed | Why it qualifies |
| --- | --- |
| Code files | The repository's own content, already visible |
| `produces:` artefacts — briefs, plans, discovery | Written to files a person reads and reviews |
| Handoffs | A declared schema with stated field purposes (S13's O6) |
| Chat messages | Human-authored |
| **Not** `step_message` | Raw tool output, unreviewed |

The distinction is not sensitivity in the abstract — it is whether anything stood between the content and the index.

## Goal

*"Have we decided this before?"* answered from past briefs and handoffs, and a step that arrives with its context already
resolved — with the product working **fully** when none of it is there.

## Decisions

V1–V12 are inherited. New:

| # | Decision | Why |
| --- | --- | --- |
| V13 | **Retrieval absent means the `search` tool does not exist**, not that it exists and returns nothing | An empty result is a claim about the repository. A handler that searches and finds nothing concludes there is nothing; one with no `search` reaches for `grep`, which is what V5 wanted |
| V14 | **A manifest entry records how it was resolved** — declared, or retrieved with its query | A retrieved manifest is a guess. A model that knows that verifies; one that thinks it was declared trusts it. Same information, opposite behaviour |
| V15 | **Nothing is indexed that no redaction pass has seen.** Artefacts, handoffs and chat messages; **never `step_message`** | The inherited rule, stated as a property so a future corpus is decided by the same test rather than case by case |
| V16 | **The embedding model is a route** (M12), so a local one is unmetered and reports `unknown` with a reason (M30) | Indexing a large repository costs real money and must be visible on the same path as everything else (V10). S18 already built that path |
| V17 | **A model or dimension mismatch refuses to query and reports what a reindex would cost.** It never silently reindexes | V6 is right that mixed embedding spaces give *"plausible, wrong results — the worst failure a retrieval system has"*, so refusing is correct. But spending money unasked is also wrong, and a cost estimate resolves both |
| V18 | **Reindexing is asynchronous and never blocks a step. The index reports its staleness** | An indexing pass that delays a step is a tax on every step. And a stale index claiming currency is worse than one that says how stale it is |
| V20 | **The benefit is measured, not assumed**: a step with resolved context must make **measurably fewer `grep`/`read` calls** than the same step without it | Adopted from the existing QA suite's QAV-09, which states it better than this design did: *"that is the benefit, and if it is absent the feature is not earning its cost."* For **the only optional slice in the plan**, that is the criterion for keeping it |
| V19 | **V1's evidence is superseded by S4's DB18**; the availability model has **four** reasons | The extension is statically linked, so `extension-missing` cannot happen. V1's conclusion stands; its Node-specific verification does not |

## Architecture

```
aeon/internal/retrieval/          ← the whole slice, removable (V13)
  index.go        the vec + FTS5 tables, per project root (V12)
  availability.go the four reasons (V19)
  chunk.go        symbol-aware where cheap, line-window otherwise (V11)
  stale.go        content hash per chunk; async reindex (V8, V18)
  embed.go        the embedding route (V16), cost through S18 (V10)
  code.go         the code corpus (V2)
  history.go      the loop-history corpus — artefacts and handoffs only (V15)
  hybrid.go       vector + FTS5 rank fusion (V3)
  resolve.go      context resolution feeding the manifest (V7, V14)

aeon/internal/tools/search.go     registered ONLY when retrieval is built (V13)
```

**The package boundary is the optionality boundary.** If `internal/retrieval` is absent, `search` is not registered and
nothing else changes — which is what makes V13 testable rather than aspirational.

### Two corpora, never merged (V2)

Code and loop history have *"different chunking, different staleness and different value"*, and merging them makes
**"search only past decisions" impossible** — which is precisely the demo's first question.

### Results are locations, never text (V4)

`file:line` ranges. Quoting *"burns context, hands the model a stale snapshot, and disconnects it from the real file"* —
locating makes the model `read` current truth.

That composes with V18's async reindex: a stale index may point at a line that moved, and a `read` of the current file is
how that self-corrects. A quoted chunk would have been silently wrong.

## The demo

See [`DEMO.md`](DEMO.md): *"have we decided this before?"* answered from past briefs; a step arriving with its context
already resolved and **its manifest saying so**; the whole product working with the package **removed**; keyword-only
degradation announcing itself; and a model mismatch **refusing with a cost estimate** rather than quietly spending money.

## Testing

| What | How |
| --- | --- |
| **Absent means absent** | With `internal/retrieval` removed, **the whole product's test suite passes** and `search` is not in the tool registry. Not degraded — absent (V13) |
| Empty is not absent | With retrieval present and the index empty, `search` **exists** and reports `index-empty` — distinguishable from having found nothing |
| Four availability reasons | All four representable; **no `extension-missing`** state exists, asserted by grep (V19) |
| Keyword-only says so | With no embedding route, results arrive and the response **states** it is keyword-only (V9) |
| Manifest provenance | A retrieval-resolved entry records the **query**; a declared one does not. Asserted on the stored manifest (V14) |
| Frozen once | A resumed step does **not** re-resolve. Asserted with the retrieval spy at **zero** on resume (K13, K14, S7) |
| `step_message` is never indexed | Asserted over the index's contents after a step with tool output, and by grep over the indexer's inputs (V15) |
| Locations, not text | No result carries chunk text (V4) |
| Mismatch refuses with a cost | A different embedding model makes a query **refuse**, reporting the reindex cost, and **reindexes nothing** (V17) |
| Async reindex | A step's timing is unchanged with a reindex running, and the index **reports its staleness** (V18) |
| Embedding cost | Through S18's three states; an unmetered embedding route is `unknown` with a reason (V16, M30) |
| Two corpora | "Search only past decisions" returns no code, and vice versa (V2) |
| Hybrid | A structural query FTS5 wins and a conceptual one vectors win, both present in fused results (V3) |
| **The benefit is real** | The same step run with and without resolved context: **the resolved run makes fewer `grep`/`read` calls**, counted. If it does not, the feature is not earning its cost (V20) |

**Two rows carry the slice.** *"Absent means absent"* is the optionality claim, and it is only true if the test suite
passes with the package deleted. *"Frozen once"* is what stops retrieval quietly making a resumed step behave differently
from the one that suspended.

## Out of scope

- **No language-aware parser per language.** V11: *"a brace/indent heuristic plus a line window is most of the benefit"*,
  and a parser per language is a project.
- **No filesystem watcher.** V8: *"a watcher is a daemon, a permissions problem and a source of phantom reindexes."* The
  loop already knows it wrote files.
- **No external vector database.** V1: pgvector or Qdrant each mean *"install a database server first"*, and P10 keeps the
  product one install.
- **No reranking model.** Hybrid fusion is the ranking. A reranker is a second model call per query and its value here is
  unmeasured.
- **No cross-project search.** V12: the index is per project root, and a worktree is a distinct root.
- **No indexing of transcripts, ever** (V15).

## A third corpus **kind**, for Stage 5

V2 gives two corpora — code and loop history — and forbids merging them. Stage 5's memory work adds a third **kind**, and
the distinction is worth recording now because it changes what a result means.

| Kind | A result is | Can it be wrong? |
| --- | --- | --- |
| **Retrieved** (code, history) | a `file:line` location (V4) | **No.** It exists or it does not |
| **Concluded** (person, project memory) | a belief the system wrote | **Yes, and it persists invisibly** |

A retrieved chunk cannot be wrong; **a learned belief can be, and nothing surfaces it.** So a concluded memory carries
**provenance and strength**, which are not equal:

| | Means |
| --- | --- |
| `told` | The user said so directly |
| `observed` | Inferred from N interactions, **with N recorded** |
| `assumed` | Carried from a default or another project |

That is [P31](../ARCHITECTURE.md#8-product-decisions)'s shape again, and here **the middle state is the majority.** A memory
system without it cannot be corrected, because nobody can find what to correct.

**Two stores, not three.** Person memory is cross-project and lives at the **store** level — S4 already puts one store per
user at `$XDG_STATE_HOME/aeon/state.db`, so V12's per-project-root index is not broken. Project memory is per root, like
everything else.

**"Language memory" collapses into these two.** *"Go errors are wrapped with `%w`"* is not learned — it is in the language
docs and already in `golang-engineer`'s body (S13's DOMAIN category). What is actually learned is *"this team wraps errors
with a custom type"*, which is **project** memory; and *"when this user asks for Go they want table tests"*, which is a
**facet of person** memory. Neither warrants a third store.

Nothing in this slice implements it. What this slice fixes is that **a corpus kind is a property of the corpus**, so a third
one is additive rather than a redefinition.

## Amendments to earlier slices

| Where | Change | Status |
| --- | --- | --- |
| `docs/_archive/aeon-v1-horizontal/11-retrieval/design.md` | V1's Node-specific verification is superseded by S4's DB18; the availability model has four reasons | **landed with this design** |
| `docs/test_suites/aeon-retrieval.md` | **QAV-09 supplied V20** — the measured-benefit criterion — which this design lacked. **QAV-01 is rewritten**: it tested `sqlite-vec` absent, which S25 makes impossible; its subject becomes the package's absence, and its wording — *"a failed step here would make this slice mandatory by the back door"* — is kept because it states the optionality concern better than this design did | **landed with this design** |
| S5 | A manifest entry gains its resolution provenance (V14) | T7 |
| S11 | `search` becomes the tenth tool — registered **only** when retrieval is present (V13, W1) | T6 |

## Open questions

None. Which artefacts the history corpus indexes is authorship, decided by V15's test — did anything stand between the
content and the index.

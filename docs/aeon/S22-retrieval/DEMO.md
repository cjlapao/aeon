# S22 Retrieval — the demo

**Stage 4 · Space shuttle, third of four.** *The only optional slice in the plan — so the demo starts by taking it away.*

Use a repository with some history in it: a few finished loops, some briefs, some plans. Retrieval is about memory, and a
demo on an empty repository shows nothing.

---

## 1. Take it away first

Before showing what it does, prove the product does not need it.

```
make build GO_TAGS="sqlite_fts5 noretrieval"   # both tags — fts5 is NOT optional (B6)
make test GO_TAGS="sqlite_fts5 noretrieval"
```

**Everything passes.** Not "passes with skips" — passes.

Then, as a client (the registry has no human-facing listing door — the
absence is asserted where the list is defined: `TestRegistryExactNames` under
this tag pins the 40-name registry **without** `search`, and the tagged build
runs its own absence subprocess, `TestSearchAbsentWithoutThePackage`, which
asks the live registry the question and fails if `search` answers):

```
aeon search --root . --corpus code "where is the retry policy"
```

**`search` is absent, not present-and-empty** (V13) — the tool is not in the
registry, so a step reaches for `grep`; the verb above reaches the socket
door, which does not answer either (no resolver is ever wired in this build).

That distinction is the whole reason this section comes first. **An empty result is a claim about the repository.** A
handler that searches for *"where is the retry policy"* and gets nothing reasonably concludes there isn't one, and moves
on. A handler with no `search` tool reaches for `grep` — which is what V5 wanted anyway: *"semantic search complements
`grep`; it never replaces it."*

**It failed if** `search` exists and returns empty. That is a capability answering unhelpfully, which is worse than one
that is legibly not there.

Now put it back.

---

## 2. "Have we decided this before?"

```
make dev      # the daemon, with the endpoint printed
aeon index build --root <the repository> --corpus history
```

It is a **client of the socket** (`retrieval.reindex`) — the daemon does the
work; with no embedding route configured the door says so and nothing moves
(the no-provider state V19 names):

```
aeon: retrieval.reindex refused at <root> — no embedding route is configured
(the no-provider state V19 names); a reindex embeds, and embedding without
a route is not a call
```

With a route configured, the summary is the pass's own figures:

```
build corpus "history" at <root>: 4 files, 6 chunks written, 0 deleted, 0 skips, 6 embedded (cost $0.0003)
```

**The `transcripts SKIPPED` line this demo predicted does not exist — and
that is the stronger shape.** V15 is enforced at the **input**, not at a
report: the history doors take only artefacts, handoffs and human chat
(`IndexedHistorySources` names the four, and the fifth row — the per-step
transcript table — is recorded there as the forbidden source with its
redactor listed as `NOTHING`), and `TestStepMessageIsNeverIndexed` asserts
both the index contents and a grep over the indexer's inputs. Nothing
SKIPPED, because nothing was ever offered.

**`transcripts SKIPPED` is not a limitation being apologised for.** `step_message` holds **tool output no redaction pass
has seen** — raw `bash` results, file contents, whatever a command printed. The rule generalised: **nothing is indexed
that has not passed through something that could redact it.** A brief was written to a file someone reads; a handoff has a
declared schema; a transcript is raw.

Now ask the question the slice exists for:

```
aeon search --root <the repository> --corpus history "did we decide how to handle a partial cost?"
```

**You should see locations, not text:**

```
  docs/aeon/S3-model-layer/design.md:68-74      0.81
  docs/aeon/S9-app/design.md:112-119            0.77
  handoff loop-4a17 step-verify                 0.71
  docs/aeon/ARCHITECTURE.md:454-456             0.68
```

**`file:line`, never chunk text** (V4). Quoting *"burns context, hands the model a stale snapshot, and disconnects it from
the real file"* — a location makes you `read` current truth.

Open the first one. **It should be the actual decision**, not something adjacent.

---

## 3. A step arrives with its context resolved — and says so

This is V7's differentiator, and the half people expect is the tool.

The retrieval declaration lives on the **blueprint's step**, not a flag —
`contextRetrievals: [{"corpus": "code", "query": "webfetch retry timeout bounded"}]`,
exactly as the committed conformance fixture `step/with-retrieval.json`
shows (an unbuildable declaration — no resolver wired, an unknown corpus —
is refused at the step door naming its missing piece, never skipped). Start
the loop from that blueprint:

```
aeon loop start --blueprint dev-loop
```

Let it reach an implement step, then read the manifest:

```
aeon step show <id> --manifest
```

**You should see how each entry got there:**

```
  internal/tools/webfetch.go     declared    (tasks/T3.md)
  internal/tools/read.go         retrieved   query: "webfetch retry timeout bounded"
  internal/model/transport.go    retrieved   query: "webfetch retry timeout bounded"
  docs/aeon/S11-permissions/…    declared    (tasks/T3.md)
```

**`declared` and `retrieved` are different facts, and the query is recorded** (V14).

**Why that matters more than it looks.** A declared entry is a human's or a plan's assertion. **A retrieved one is a
guess** — similarity picked it, and the file the step actually needs might not be there. A model that knows its context was
guessed will verify with `grep`; a model that thinks it was declared will trust it. Same information, opposite behaviour.

Now the property that stops this getting subtle. Suspend the step and resume it:

```
aeon answer <id> …
aeon step show <id> --manifest
```

**Identical manifest. Nothing re-resolved.**

The manifest is **frozen at declaration** (K13), and a resumed step **never re-resolves** (K14, S7) — it reports what
changed. If retrieval re-ran on resume, a resumed step could get a *different* context from the one that suspended, and
the whole manifest would stop meaning anything.

---

## 4. No embedding provider: keyword-only, said out loud

Remove the embedding route (`aeon routes delete <id>` — routes are verbs on
`aeon routes`), and query:

```
aeon search --root <the repository> --corpus code "where do we bound a retry"
```

**Results, and a statement** — the note rides IN the answer, not beside it
(V9 reaches the deciding audience, which for the tool means the model):

```
  internal/model/transport.go:41-58     0.0328
  internal/tools/webfetch.go:88-96      0.0164

keyword-only: no embedding route answered — this is the lexical half
speaking (V9/D4), read the files for truth
```

**Degraded, and it says so** (V9) — the same honesty as the cost model. *"Silent degradation to worse results is
indistinguishable from the index being empty."*

**And that is a different state from §1's absence.** Here `search` exists and works less well, and tells you. There it did
not exist. Conflating those is how an optional slice becomes load-bearing.

Check the four states:

```
aeon index status --root <the repository>
```

```
index status — root <the repository>
embedding route: none configured — vector search is absent; keyword answers carry the keyword-only note (D4)
  code         1 files   not built yet   availability: index-empty
  history      0 files   not built yet   availability: index-empty
```

(The four reasons are `no-provider`, `index-empty`, `model-mismatch` and
`dimension-mismatch`; the line above shows the never-built corpus speaking
`index-empty` — measured on a clean root, 2026-10-29.)

**Four, not five.** There is no `extension-missing` — `sqlite-vec` is **compiled into the binary** via
`sqlite3_auto_extension()` (S4's DB18), so it is present on every connection. Nothing to install, nothing to fail (V19).

---

## 5. A model mismatch refuses, and tells you what a reindex costs

Point the route at a different embedding model and query:

Configure the second embedding route (`aeon routes set other-embed …`,
model `text-embedding-3-large`) and query the corpus built by the first:

```
aeon search --root <the repository> --corpus code "where do we bound a retry"
```

**It refuses — and not reindex** (the sentence below is the refusal's real
shape, from `TestMismatchRefusesAndReindexesNothing`'s own assertion set):

```
retrieval: refused the query at <root> (code) — model-mismatch: the index
was built with "text-embedding-3-small" and route "other-embed" is
"text-embedding-3-large"

  Nothing was reindexed. A reindex would cost approximately $0.71
  (estimate: 1204 files, ~38000 chunks, ~480000 tokens at 4 bytes/token).

  To proceed:  aeon index rebuild --corpus code --embed-route other-embed
```

An unmetered route drops the money sentence for words instead — "cannot be
priced… no figure" — never `$0.00` (M30). The deliberate door itself:

```
aeon index rebuild --root <the repository> --corpus code --embed-route other-embed [--force]
```

`--force` is the V17 restatement (the wipe-then-build onto a foreign model);
without it the pass refuses exactly like the query.

**Two things are deliberate.**

**It refuses rather than answering.** V6's reason: comparing vectors from two embedding spaces returns **plausible, wrong
results — the worst failure a retrieval system has.** A wrong answer that looks right is worse than no answer.

**And it does not silently reindex.** Indexing a large repository costs real money, and spending it unasked is its own
failure. So it prices the work and waits (V17).

**It failed if** it either answered anyway or started reindexing. The first is the worse failure; the second is the more
annoying one. (On a clean box with no provider credential the walk stops
before the mismatch — building the first index needs a route. The refusal,
the pricing and the zero-spy are **asserted in the suite**
(`TestMismatchRefusesAndReindexesNothing`, `TestMismatchReportsAnEstimateWithItsBasis`,
`TestUnmeteredPortionsExcludedFromTheEstimate`); the live-money variant is
named, not verified.)

---

## 6. Indexing never slows a step

With a large repository, start a reindex and run a loop at the same time:

```
aeon index rebuild --root <the repository> --corpus code &
aeon loop start --blueprint two-step "survey internal"
```

**You should see the loop's step timings unchanged**, and:

```
aeon index status
```

```
index status — root <the repository>
embedding route openai-small (model text-embedding-3-small, 1536 dims)
  code         1204 files   reindex running, 61%
  history        71 files   current
```

The running figure is the daemon's tracker merging the pass's progress over
the seam's figures — and it is **per corpus**: a code pass never puts
`reindexing` on the history row (O4; `TestStatusMergesTheRunningFigures`).
Step timings with the pass in flight are asserted by
`TestReindexNeverDelaysAStep` on elapsed time, with the pass held open —
the live two-terminal variant is named, not verified.

**The index reports its staleness** (V18). A step may run against a slightly stale index — that is the price of not
blocking — but **a stale index claiming currency is worse than one that says how stale it is.**

And this is where V4 pays off a second time: results are **locations**. A stale index may point at a line that has moved,
and the `read` of the current file self-corrects. A quoted chunk would have been silently wrong.

---

## 7. Two corpora, never merged

```
aeon search --root <the repository> --corpus history "retry"   # decisions about retries
aeon search --root <the repository> --corpus code    "retry"   # where retries are implemented
```

A missing or invented `--corpus` is refused at the verb before the socket
sees it — measured: `search refuses corpus "wiki" — name "code" or
"history"; there is no default (O8)`.

**Two different answer sets, and neither leaks into the other** (V2).

They have *"different chunking, different staleness and different value"*, and merging them makes **"search only past
decisions" impossible** — which is §2's question, the one this slice exists for.

---

## What this demo does **not** show

- **No language-aware parsing.** A brace and indent heuristic plus a line window (V11); a parser per language is a
  project, and this is most of the benefit.
- **No filesystem watcher.** The loop knows when it wrote files (V8). *"A watcher is a daemon, a permissions problem and a
  source of phantom reindexes."*
- **No external vector database.** One install (P10); pgvector or Qdrant each mean *"install a database server first"*.
- **No reranking model.** Hybrid fusion is the ranking; a reranker is a second model call per query with unmeasured value
  here.
- **No cross-project search.** The index is per project root, and a worktree is a distinct root (V12).
- **No transcript indexing, ever** (V15).

---

## Walk record — S22 T9, 2026-10-29

Walked as far as a clean box reaches (no provider credential), with the daemon on
a private endpoint and `XDG_STATE_HOME` at a throwaway directory. The findings
are recorded as the rule demands — what the predictions got wrong:

| | Prediction | Reality |
| --- | --- | --- |
| **F-1** | `aeon tools list` shows the registry | **No such door exists.** Absence is asserted where the list is defined (the registry-lock test + the tagged-build absence subprocess). Named, not built — a listing door is not this slice's to add |
| **F-2** | `aeon route rm/add` | The verb is `aeon routes list · set · delete` |
| **F-3** | `aeon search --corpus …` | The verb did not exist at all; **T9 added it** as a socket client (`cmd/aeon/search.go` → `search.query`, locations-never-text), and `--root` is required (V12/I5 refuse the ambient guess) |
| **F-4** | `make build TAGS=noretrieval` | `GO_TAGS`, and **both** tags — `sqlite_fts5 noretrieval`; dropping fts5 kills the store open (T1's measured fact), so the prediction's line walked into a different failure |
| **F-5** | `index build` indexes and reports | Without a route the door **refuses and says why** — measured verbatim, quoted in §2. The pass embeds; embedding without a route is not a call |
| **F-6** | A `transcripts SKIPPED` line in the build output | **No such line exists — and that is the stronger shape** (V15 enforced at the input: the forbidden source is named in `IndexedHistorySources` and hunted by the ban test; nothing is ever offered to be skipped). The section now says so |
| **F-7** | `aeon index status` (no root) | Refused: `--root` names the project (staleness is a property of ONE root); measured output shape quoted in §4 |

**Measured** (this walk): `index status` figures and reasons on a clean root
(`not built yet` + `index-empty`, the none-route line speaking D4's
sentence); the no-provider reindex refusal; the corpus-naming refusals at
both verbs (`search refuses corpus "wiki"…`, `index rebuild refuses corpus
"wiki"…`); `aeon search` reaching the socket and passing the resolver's
`index-empty` sentence through **verbatim**.
**Named, not verified** (need a provider credential or a live loop): the
priced mismatch of §5 (suite-asserted at the spy), the running-percent
merge of §6 (suite-asserted on the tracker + timings), the frozen-manifest
identity of §3 (suite-asserted on both resume and recovery), and every
real-money figure in this document, which are shapes of sentences, not
quotes of receipts.

Next: **S23** — a stranger with one API key, and five minutes.

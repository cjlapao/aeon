# Brief — S30 Code Graph

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md) · **Plan:** [`plan.md`](plan.md)
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — **A13** (embeddings serve discovery, never
dispatch) governs this slice's dispatch boundary even though nothing here is embedding-based (D9). **P31**
is read, not extended — this slice's four-state query result (§2 D7) shares P31's discipline of never
collapsing distinct answers into one bucket, but it is not a ninth P31 instance: P31 is about verification
confidence (*checked-good / checked-bad / could-not-check*); this is about result cardinality (*found /
ambiguous / not-found / not-built*). `ARCHITECTURE.md`'s P31 table stays at eight.
**Stage:** 5 · Fleet — **a new addition, not a promotion.** S24, S28 and S29 each formalized a sketch the
roadmap had already named since the original split into six slices. This one did not exist in that roster
at all; it is a seventh, proposed and specced in one sitting. Fleet is now seven slices, four of them real
briefs.
**Depends on:** S4 (store — plain relational tables, no `sqlite-vec`; see §5), S9 (app — the test-bench
surface this slice's UI task needs). **Shares one open question with S22** (the write-signal hook, §7 Q2) as
infrastructure, not as a dependency — see §5's corpus-boundary decision. **Explicitly independent of S22's
corpora, S13's agent registry, and S20's clock/webhook mechanism** — none of this slice's dependencies run
through them, and §7 Q2 explains why the obvious-looking S20 reuse was considered and rejected.
**Repo:** Aeon lives at `aeon/` in this repository (P24) · Go 1.26 · `cgo` enabled (B3)
**Fixed 2026-10-26 against P34** (`ARCHITECTURE.md`): a real gap, found and fixed in `tasks/T5.md` and
`plan.md` — as originally written, the `graph` tool (T5) was a native tool only, callable from inside an
agent's loop step and nowhere else, while T7 built a test-bench UI with no door onto it at all (a native
tool cannot be called directly by the app). T5 now also registers `graph.explain`/`graph.path`/`graph.query`
as socket methods calling the identical `internal/graph/query.go` functions, and T7 is corrected to call
them rather than an unspecified path.

---

## 1. What this is

**A structural map of a codebase, queryable by an agent, kept separate from the content it describes.**
Running a loop that needs to understand a complex path through the code today has one option: read files,
or `grep`, and build the picture by hand — paying context for every line of every file it opens, whether or
not that line turns out to matter.

This slice parses the project with `tree-sitter` into **nodes** (functions, types, files — a symbol with a
location) and **edges** (`calls`, `imports`, `inherits`, `mixes_in`, `uses`, `references` — a relation
between two symbols), stores both as plain rows, and exposes them through a native tool. An agent asks "what
does this call, and what calls it" and gets back a short, structured list of references — not prose, not
file contents. It then decides, itself, which two or three of those references are worth the context cost
of actually reading — at which point it reaches for **S22's existing `search`**, on just those nodes,
instead of a whole file.

**Structure and content stay two different questions, answered by two different mechanisms.** This slice
answers the first; S22 already answers the second. Neither absorbs the other.

## 2. Definition of Done

| | |
| --- | --- |
| **D1** | **Own tables, `graph_node` and `graph_edge`, in S4's existing store.** No `sqlite-vec`, no embedding model, no new extension — plain relational rows, because nothing this slice answers needs similarity, only structure. Cheaper than S28's or S29's own index, which both needed one. |
| **D2** | **Parsing is `tree-sitter`, through Go `cgo` bindings, statically linked into the daemon binary** — same precedent as `sqlite-vec`'s DB18 (S4): no install step, no runtime extension loading. Which binding, and how many grammars it ships prebuilt, is **not asserted here** — it is §7 Q1, because Graphify's "~40 languages" describes Graphify's own tool, not evidence about what a Go binding ships. |
| **D3** | **Six relation kinds**: `calls`, `imports`, `inherits`, `mixes_in`, `uses`, `references` — Graphify's set, adopted as-is. **Not every kind applies to every language** (Go has no `inherits`; struct embedding is the closest match to `mixes_in`) — the per-language mapping is named in `design.md`, not improvised per task. |
| **D4** | **Every edge is tagged `extracted` or `inferred`.** `extracted`: the relation is literal in the AST (an import statement, a call expression). `inferred`: resolving it crossed files or required disambiguation (linking a call site to its definition). The tag is what lets an ambiguous resolution surface as ambiguous instead of as a silent guess. |
| **D5** | **Rebuild is incremental, triggered by the loop's own writes — no filesystem watcher.** Same reasoning S22 already gives for its own reindex (V8: *"the loop already knows it wrote files"*; N2: *"a watcher is a daemon, a permissions problem and a source of phantom reindexes"*). This slice does not invent a second hook — see §5 and §7 Q2 for why **S20 was considered and is not it**. |
| **D6** | **A changed file's rebuild cost scales with that file, not the project.** Proven by a test that edits one file in a fixture of at least 500 files and asserts parse work stays bounded to the changed file — a scaling proof, not a wall-clock number, because the latter varies by machine and the former does not. |
| **D7** | **A query returns raw structured data only** — node refs (file, line, symbol, kind) and the edges connecting them. **No model-generated prose.** The calling agent decides how to summarize; this tool does not run a model pass. |
| **D8** | **A query result is four-state: `found` / `ambiguous` / `not-found` / `not-built`.** `ambiguous` means multiple exact-name matches, not a confidence judgement. `not-built` means no graph exists yet for this project root, distinct from `not-found` (a graph exists; nothing in it matches). Collapsing the two would make "never parsed" indistinguishable from "parsed and absent" — the same collapse S29 just refused for `unvalidated` versus `could-not-tell`. |
| **D9** | **Scoped per-project-root. No cross-project graph.** Matches S22's V12 boundary, independently reasoned here since this slice does not depend on S22 existing. |
| **D10** | **Graph output never feeds dispatch.** No code path lets `by-tech-tag`, or any other resolver, read a graph result. Asserted by an import-boundary test: nothing under `internal/agents/` (or wherever the resolver lives) imports this slice's package. Mirrors A13's closed-registry reasoning even though nothing here is embedding-based — the risk A13 was written against is a similarity-flavoured shortcut into dispatch, and a graph edge is exactly similarity-shaped enough to tempt one. |
| **D11** | **Composition with S22 happens at use-time only, never at the schema or call level.** An agent calls this slice's tool for structure, then separately calls S22's `search` for content on the specific nodes it chose. No shared table, no inlined content field, no single call that does both. |

**D5 and D10 are the two that matter.** D5 because the obvious move — "just watch the filesystem," or "just
reuse S20" — is wrong twice over, and getting it wrong here means either a phantom-reindex problem S22
already rejected, or a dependency on a mechanism (S20) that solves a different problem (scheduled loop
starts, not file-change signals). D10 because a structural graph is similarity-shaped enough that routing
"this file touches a lot of auth-tagged nodes, send it to the auth agent" will look like a reasonable
shortcut to somebody, and it is exactly the shortcut A13 exists to refuse.

## 3. Why it is cheaper than it looks

**No embedding corpus at all.** S28 and S29 each needed their own `sqlite-vec` index because their questions
are fundamentally about similarity — "what does this project know," "is this a duplicate." This slice's
question is structural and deterministic: tree-sitter already produced an AST; walking it into rows is a
tree traversal, not a search problem. Plain `CREATE TABLE`, no new extension beyond the parser itself.

**The hard part — a statically-linked `cgo` dependency with no install step — has a working precedent
already.** `sqlite-vec` proved the pattern (DB18): a C library, registered through `cgo`, present in every
build with no load step and no extension-missing state. Each `tree-sitter` grammar is one more instance of
that same pattern, not a new kind of build concern.

**The rebuild trigger is not a new mechanism — it is the same one S22 already needs and has not yet built.**
S22's own brief asks, unanswered: *"how does the loop know it wrote files, and is there a hook?"* (its Q7).
This slice asks the identical question for the identical reason. Whichever of the two slices answers it
first builds a hook at the tools/daemon layer — not inside either slice's own package — and the other
subscribes to the same signal. Shared infrastructure, not a cross-slice dependency; S22 remains the one
deletable slice in the plan, and this slice must keep working if S22 is deleted, exactly as S28 and S29
already committed to.

## 4. The seams

**Downward — S4's store.** `graph_node` and `graph_edge` are plain tables, migrated the same way every other
slice's tables are. No new extension beyond the statically-linked parser.

**Sideways, shared infrastructure rather than a dependency — the write-signal hook.** Whatever resolves
S22's Q7 resolves this slice's §7 Q2 at the same time, at the same file:line. Neither slice owns the hook;
both subscribe to it.

**Sideways, use-time only — S22's `search` tool.** An agent chains this slice's tool into S22's itself; there
is no code-level coupling, no shared schema, no shared call.

**Sideways, deliberately walled off — S13's dispatch.** This slice's output is read by the agent that asked
for it, never by the mechanism that decided which agent runs a step. D10 is the test that keeps this true.

## 5. Locked decisions

- **Plain relational tables, not an embedding index.** This slice's question is structural, not
  similarity-based; `sqlite-vec` is not reused here the way S28 and S29 reuse it.
- **Tree-sitter via Go `cgo` bindings, statically linked, same precedent as `sqlite-vec`'s DB18.** Which
  binding and how many grammars it covers for free is verified during implementation (§7 Q1), not assumed
  from Graphify's own figure.
- **No filesystem watcher, ever.** The rebuild hook is the loop's own knowledge that it wrote a file, shared
  with whatever S22 builds for its identical need (§7 Q2) — **not** S20, which schedules loop starts and has
  nothing to do with file-change signals. This was considered during the discussion that produced this brief
  and explicitly rejected; see `design.md`'s amendment note.
- **A query result is four states, not three.** `found` / `ambiguous` / `not-found` / `not-built`, added
  during the writing of this brief — `not-built` was not part of the original three-state discussion and is
  called out here rather than folded in silently, the same way S29's `unvalidated` state was called out
  against `could-not-tell`.
- **Edges carry an `extracted`/`inferred` tag**, matching Graphify's own distinction, because it is what lets
  a cross-file resolution's ambiguity surface honestly instead of silently picking one candidate.
- **Dispatch boundary is enforced by a test, not by convention.** D10's import-boundary check is the guard;
  a prose rule nobody asserts is a rule nobody obeys.

## 6. Out of scope

- **N1 — No embedding index, no `sqlite-vec` dependency.** If fuzzy or semantic symbol matching is ever
  wanted on top of this slice, that is a use of S22's `search`, composed at use-time (D11) — it is never
  built into the graph itself.
- **N2 — No filesystem watcher.** Same reasoning as S22's V8/N2, restated here because it is the single most
  tempting shortcut once "live" rebuilding comes up again later.
- **N3 — No cross-project graph.** D9, restated as a standing non-goal.
- **N4 — No LLM-synthesized explanation from the query tool.** D7; the tool returns data, never prose.
- **N5 — No influence over dispatch, under any framing** — not "a hint," not "a tie-breaker," not "only when
  the resolver is otherwise stuck." D10 is the full and only exception process: none.
- **N6 — Near-miss: S22 Retrieval.** What it actually is: semantic-and-keyword search over two corpora (code,
  history) that returns *content*. Who consumes it: any agent that wants to read or understand text it has
  already located. What breaks if this slice's structure is folded into it: S22's V2 (*"two corpora, never
  merged"*) and V12 (*"index per project root, no cross-project search"*) would both be stretched to cover a
  third, structurally different kind of index — and the entire reason this slice exists, keeping structure
  and content as two different questions, collapses back into "one slower search."

### Standing non-goals

- **No scheduler or periodic-job infrastructure.** This slice reacts to writes; it does not poll, and it
  does not schedule anything — that remains S20's and S26's territory.
- **No new external infrastructure dependency.** `tree-sitter` is a vendored, statically-linked library, the
  same shape as `sqlite-vec` — not a service, not a cache tier, not something that can be down.

## 7. Questions to answer before building

| # | Question | Where |
| --- | --- | --- |
| Q1 | Which Go `tree-sitter` binding actually ships prebuilt grammars, for how many languages — and for any language it does not cover, is vendoring that grammar's C source into this repo's `cgo` build feasible at all? Graphify's "~40" is its own tool's figure, not evidence about any particular Go binding. | `design.md`, resolved concretely in `plan.md`'s T2/T3 |
| Q2 | **Shared with S22's own open Q7.** Where, concretely, does "the loop already knows it wrote files" resolve to a hook — and whichever slice is implemented first, does it build that hook at the tools/daemon layer (not inside its own package) so the other slice can subscribe to the identical signal? | `design.md`; `aeon/internal/tools/`, `aeon/internal/action/` once either exists |
| Q3 | Per-language edge granularity beyond the six relation kinds — function-level nodes only, or also block- or variable-level — and where does that line get drawn for a language with no obvious equivalent to `inherits` or `mixes_in`? Partially resolved in `design.md`'s taxonomy table; remaining per-language edge cases are expected to surface during T2/T3 and should be recorded there, not improvised silently. | `design.md`, T2/T3 |

## 8. Risks

- **R1 — The write-signal hook gets built twice**, once inside S22 and once inside this slice, because
  nobody checked the other brief before writing code. **The guard is the explicit cross-reference** — this
  brief's §7 Q2 and S22-retrieval/brief.md's Q7 now point at each other by name.
- **R2 — Grammar coverage gets overclaimed.** "~40 languages" is easy to repeat as if it were already true of
  this slice's binding. **The guard is D2 and §7 Q1**: the real count is measured in T3 and stated as what
  was measured, never assumed in advance.
- **R3 — The four-state result collapses to three in a renderer that "just wants a status."** The same
  collapse risk S24, S28 and S29 already guard against for their own multi-state fields. **The guard**: a
  test asserting all four states render distinctly, the same shape those three slices already use.
- **R4 — The graph quietly grows a content column** — a `body` or `snippet` field added "for convenience" so
  a caller does not have to make a second call to S22. **The guard is D7, D11 and N1**: structured references
  only, composition with S22 happens in the calling agent, never inside this slice's schema or tool.

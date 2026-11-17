# Design — S30 Code Graph (Stage 5 · Fleet)

## Context

### A context-reduction problem, not a search problem

An agent working on a complex path through a large codebase has exactly one tool today for "what does this
touch": read files, or `grep`, and build the picture by hand. Both pay context proportional to file size,
not to relevance. S22 (once built) makes *content* retrieval cheap — a semantic or keyword query returns the
right chunk instead of a whole file — but it still answers "what does this text say," not "what does this
connect to." An import, a call site, an inheritance relation: these are structural facts a parser already
knows for free, and today nothing records them anywhere queryable.

### Two different questions, on purpose

- **Structure**: what calls this, what does this import, what does this inherit from. Deterministic, falls
  straight out of an AST walk, no model needed.
- **Content**: what does this code actually say, read in full or by meaning. S22's job.

Keeping these separate is the whole design. An agent exploring a path asks the first question cheaply,
narrows to two or three nodes that actually matter, and only then pays for the second question on exactly
those nodes.

### Why now, and why this is not two more proposals

Every piece this slice needs already exists except the graph itself: a store with `cgo` already enabled
(S4), a precedent for a statically-linked C dependency with zero install cost (`sqlite-vec`, DB18), and two
sibling slices (S28, S29) that just finished proving "this slice gets its own index, not a third thing
folded into S22's two corpora" is a repeatable, locked pattern rather than a one-off. This slice's genuinely
new work is small: two tables, a parser wired through `cgo`, and a tool that reads them.

## Goal

Give an agent a way to ask "what is connected to this" and get back a short, structured answer — cheap
enough that asking costs less than reading the file would have.

## Decisions

Numbered independently of Aeon's lettered decision codes (A, D, G, P, V) — this slice introduces no new
lettered series, since every governing rule it touches (A13, P31, S22's V2/V12/V8) already belongs to an
existing one.

**CG1 — No embedding index.** The question this slice answers is deterministic and structural. Reaching for
`sqlite-vec` here would be solving a problem this slice does not have, the same over-reach S28 and S29 both
explicitly declined to make in the other direction (folding their indexes into S22's).

**CG2 — `tree-sitter`, not a hand-written parser per language.** Settled early in the conversation that
produced this brief: writing ~40 parsers by hand is a different, much larger project; `tree-sitter` already
has grammars for all of them, and wrapping it is a vendoring problem, not a parsing-theory problem.

**CG3 — Statically linked, no install step.** `cgo` is already enabled (B3) and the daemon already ships one
statically-linked C dependency (`sqlite-vec`, DB18). A `tree-sitter` core plus per-language grammars is one
more instance of the identical pattern — register through `cgo`, present in every build, nothing to install
on the machine running Aeon.

**CG4 — Six relation kinds, Graphify's set, adopted rather than invented.** `calls`, `imports`, `inherits`,
`mixes_in`, `uses`, `references`. Table of what each one maps to per language family, filled in as T2/T3
discover the real cases:

| Relation | Go | TypeScript |
| --- | --- | --- |
| `calls` | A function or method call expression | A call expression |
| `imports` | An `import` declaration | An `import` statement |
| `inherits` | **Not applicable** — Go has no inheritance | `extends` on a class |
| `mixes_in` | Struct embedding — the closest structural match | `implements`, or a mixin composition pattern |
| `uses` | A type reference in a signature or field | A type annotation |
| `references` | Any other named-symbol reference not covered above | Same |

Further languages add rows as T3 discovers their real cases; a language with no match for a given relation
simply never emits that edge kind — it is not forced to.

**CG5 — Every edge carries `extracted` or `inferred`.** `extracted`: literal in the AST — an import line, a
direct call expression. `inferred`: resolving it required crossing files or disambiguating between
candidates — linking a call site to the function it actually calls, when more than one symbol shares that
name. This tag is what makes an uncertain resolution visible instead of a silently-picked guess, and it is
what feeds D8's `ambiguous` state when a resolution has more than one candidate.

**CG6 — Rebuild hook: shared with S22, not reinvented, not S20.** This was the one real course-correction
during the conversation that produced this brief. The first instinct was "reuse S20's trigger mechanism so
we don't build a second file-watcher" — **wrong**: S20 is a clock and a webhook (*"at 03:00, start loop X
from blueprint Y"*), it schedules **loop starts**, and has no concept of a file changing mid-loop. The
correct precedent was already sitting in S22's own brief, unresolved: Q7, *"how does the loop know it wrote
files, and is there a hook?"* — V8's answer for S22's own reindex is *"the loop already knows it wrote
files"*, explicitly **not** a filesystem watcher (N2: *"a watcher is a daemon, a permissions problem and a
source of phantom reindexes"*). This slice asks the identical question for the identical reason, and the
answer should be built once, at the tools/daemon layer (`internal/tools/`, or `internal/action/` — neither
exists yet; S22's own Q7 already named these as the likely location), with both slices subscribing to the
same signal rather than either owning it.

**CG7 — Four-state query result, added while writing this brief.** The AskUserQuestion round that preceded
this brief settled a three-state result: `found` / `ambiguous` / `not-found`. Writing the behavioural
contract (below) surfaced a fourth, distinct case that three states cannot express honestly: a project whose
graph has never been built at all. Collapsing "never parsed" into "parsed, nothing matched" would mean an
agent cannot tell "this symbol doesn't exist" from "we have no idea, nothing has run yet" — exactly the kind
of collapse S29 just refused for `unvalidated` versus `could-not-tell`, for the same reason. **This is a
call made during brief-writing, not something confirmed in the earlier discussion** — flagged here so it is
visible rather than silently expanded scope.

**CG8 — Raw structured output, no model pass.** Confirmed directly in the discussion: the tool returns node
and edge data; the calling agent decides how to read and summarize it. No synthesized explanation field,
ever (D7, N4).

**CG9 — Dispatch boundary enforced by a test.** A13's reasoning — *"a generic handler writing Elixir
produces something plausible, the loop reports `ok`, and the badness surfaces three steps later attributed
to the model"* — applies just as well to a structural shortcut as to an embedding-similarity one: "this file
touches a lot of auth-tagged nodes, route it to the auth agent" is the same shape of mistake, dressed in
graph edges instead of cosine similarity. D10's import-boundary test is the enforcement; this decision is
why that test exists rather than a comment.

## Architecture

```text
    tool call: graph(action, args)      graph.explain/path/query
    (agent, inside a loop step)         (socket — the app, T7)
                    │                              │
                    └──────────────┬───────────────┘
                                    ▼
       ┌─────────────────────────┐        four-state result:
       │   internal/graph        │        found / ambiguous /
       │  query.go, build.go     │───────▶ not-found / not-built
       └───────────┬─────────────┘        (node refs + edges only)
                   │ reads/writes
                   ▼
       ┌─────────────────────────┐
       │  graph_node, graph_edge │   (S4's store; plain tables,
       │   (per project_root)    │    no sqlite-vec)
       └───────────┬─────────────┘
                   │ built/updated by
                   ▼
       ┌─────────────────────────┐
       │  tree-sitter (cgo, one  │
       │  grammar per language,  │
       │  statically linked)     │
       └─────────────────────────┘
                   ▲
                   │ invoked on
       ┌─────────────────────────┐
       │  write-signal hook      │  shared with S22's Q7 — built once,
       │  (location: open, Q2)   │  at the tools/daemon layer
       └─────────────────────────┘
```

**The two seams that matter.** Agent ↔ `graph` tool: four-state result, raw data, never prose (CG7, CG8).
Write-signal hook ↔ both S30 and S22: a shared subscription point, owned by neither slice's own package
(CG6). **A third, added 2026-10-26 (P34):** app ↔ `graph.*` socket methods, calling the identical
`internal/graph/query.go` functions the tool calls — without it, T7's UI has no door onto this slice at
all, since a native tool is reachable only from inside an agent's own loop step.

### Why not reuse S20

Covered above (CG6) because it is the one override worth stating plainly rather than letting a reader
wonder why the obvious-looking answer is absent: S20 schedules **loop starts**. This slice needs to know
about a file changing **during** an already-running loop. Different problem, different mechanism, and no
amount of generalizing S20's trigger concept makes it fit — the fire-control loop S20 designs around
(`schedule → tick → fire a blueprint`) has no notion of "a file changed" anywhere in it.

### Why four states and not three

Covered above (CG7). The practical consequence: the query tool's result type is a tagged union of four
variants, not a boolean-plus-nullable-list. A caller that only checks "is the list empty" cannot tell
`not-found` from `not-built` — which is exactly the ambiguity P31's broader discipline exists to prevent,
even though this is not a P31 instance (brief §0's header note explains why: cardinality, not verification
confidence).

### Per-project scope, enforced the same way S22 enforces it

`graph_node` and `graph_edge` both carry `project_root`. Every query names its project explicitly — the same
"no default corpus" reasoning S22's O8 already states for its own two corpora, applied here to one.

## The demo

Full walkthrough in [`DEMO.md`](DEMO.md). In outline: build the graph for a multi-file fixture project,
query it for a function several calls deep, get back the chain as structured references, chain one of those
references into S22's `search` for the actual content, then edit one file and show the graph update without
a full rebuild.

## Testing

- **Scaling proof (D6)**: a fixture with ≥500 files; edit one; assert the reparse touches only that file's
  nodes and edges, by counting rows written versus rows that could have been touched.
- **Four-state proof (D8, R3)**: one test per state, including a project with no graph built at all.
- **Dispatch-boundary proof (D10, CG9)**: an import-boundary test — nothing under the resolver's package
  imports `internal/graph`.
- **Edge-tag proof (D4, CG5)**: a fixture with one unambiguous call and one ambiguous one (two same-named
  candidates); assert the first is `extracted` and the second is `inferred` and surfaces as `ambiguous` on
  query.
- **Composition proof (D11)**: an integration test chaining a graph query's node ref into S22's `search`
  call, asserting no shared schema and no shared call — two tool calls, not one.

## Out of scope

See brief §6 for the full list (N1–N6) and standing non-goals. Nothing here extends that list.

## Amendments to earlier slices

- **S22-retrieval/brief.md, §7 (its Q7)** — cross-referenced, not changed. A one-line note pointing at this
  slice's §7 Q2 was added so whichever slice resolves the write-signal hook first is read by whoever builds
  the other.

## Open questions

Carried from brief §7 (Q1–Q3), unresolved here on purpose — each needs code written to answer, not more
discussion. T2 and T3 (plan.md) are where Q1 and part of Q3 get real answers; Q2 is answered wherever the
write-signal hook actually lands, inside this slice's T4 or inside S22's own work, whichever comes first.

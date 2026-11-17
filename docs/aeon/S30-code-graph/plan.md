# Plan — S30 Code Graph

## What is settled before planning

Everything in `brief.md` §2 (D1–D11) and §5, and `design.md`'s CG1–CG9. In particular: plain relational
tables, no embedding index (CG1); `tree-sitter` via statically-linked `cgo` bindings (CG2, CG3); six relation
kinds with the per-language mapping table in `design.md` (CG4); every edge tagged `extracted`/`inferred`
(CG5); the rebuild hook is shared with S22's own open Q7, and is **not** S20 (CG6); a four-state query result
(CG7); raw structured output only (CG8); the dispatch boundary enforced by a test (CG9).

## How we will do it

Prove the mechanism on two languages first — Go and TypeScript, because they are the two this very
repository is written in, so every fixture doubles as a real-world test. Only after the registry pattern is
proven on both does registering the rest of whatever the chosen binding ships for free become a bulk,
mechanical task — not eight more copies of the same investigation.

The rebuild hook (CG6) is the one piece of this plan that may already exist by the time it is picked up, if
S22 lands first and answers its own Q7. T4 is written to check for that before building a second one.

## Open questions this plan answers

- **Q1** (which binding, how many grammars) — T2 picks the binding and proves it on Go; T3 measures the real
  count against it, rather than assuming Graphify's figure.
- **Q2** (the write-signal hook) — T4, with an explicit check for whether S22 already built it.
- **Q3** (per-language edge granularity) — T2 and T3's own notes record what each language's grammar actually
  supports, filling in rows `design.md`'s table left open.

## Task sequence

### T1 — `schema.go`: `graph_node`, `graph_edge`

Two tables, migrated the standard way: `graph_node` (`project_root`, `file_path`, `start_line`, `end_line`,
`kind`, `symbol`, `language`) and `graph_edge` (`project_root`, `from_node_id`, `to_node_id`, `kind`,
`resolution` — `extracted` or `inferred`). No vector columns, no `sqlite-vec` reference anywhere in this
task (D1, CG1).

### T2 — `parse.go` and the grammar registry: Go, end to end

Selects the Go `tree-sitter` binding (resolving Q1's first half — which package, and does it in fact ship
without a separate install step once vendored), wires one grammar (Go) through a registry interface sized
for "add a grammar," and walks a Go AST into `graph_node`/`graph_edge` rows for all six relation kinds that
apply to Go (five, per `design.md`'s table — Go has no `inherits`). Proven against this very repository's
own `aeon/internal/` tree as the first real fixture.

### T3 — TypeScript, plus whatever else the binding ships for free

Adds the second grammar through the same registry — proving the registry generalizes rather than having
been shaped around Go's own AST shape — and then, mechanically, registers every other grammar the chosen
binding already ships prebuilt, each proven parseable against a small fixture file in that language. **The
real count is stated here as what was measured**, not assumed from Graphify's "~40" in advance (D2, R2).
Where the binding does not ship a language Graphify covers, that gap is named and left open rather than
papered over with a vendored grammar source unless doing so is genuinely cheap.

### T4 — Incremental rebuild: the write-signal hook

**First check whether S22 has already built this.** If `internal/tools/write.go` or
`internal/action/git_actions.go` (the two locations S22's own Q7 names) already carries a write-completion
signal, subscribe to it. If not, build it at that layer — not inside `internal/graph` — so S22 can subscribe
to the identical signal later without either slice owning the other's half. Once the signal exists, this
task does the actual incremental work: re-parse only the changed file, re-link only the edges touching it,
and prove the scaling bound (D6) against a ≥500-file fixture.

### T5 — The `graph` tool: `explain`, `path`, `query`

Registers the native tool. Three actions, mirroring Graphify's own CLI shape structurally rather than
literally: `explain <symbol>` (the node plus its direct edges), `path <symbol> <symbol>` (the shortest
connecting chain, if one exists), `query <filter>` (a broader structural search by kind, name prefix, or
file). Every action returns the four-state result (D8, CG7) and raw structured data only (D7, CG8) — no
action returns prose. **Also registers `graph.explain`/`graph.path`/`graph.query` as socket methods**
calling the identical `internal/graph/query.go` functions (P34, added 2026-10-26) — without this, T7's UI
task has no door onto the graph at all, since a native tool is callable only from inside an agent's loop
step, never directly by the app.

### T6 — Dispatch-boundary test

An import-boundary test: nothing under the resolver's package (wherever `by-tech-tag` lives, once S13 is
built) imports `internal/graph`, and nothing in `internal/graph` imports the resolver. Written now, against
whatever exists today, so it starts failing the moment the boundary is crossed in either slice's future
work — not discovered after the fact (D10, CG9).

### T7 — Test-bench UI: a `graph` tab

Adds `"graph"` to `BENCH_TABS` (`aeon/ui/src/app/routes.ts`) and a `GraphPage.tsx` modelled on the existing
`AskPage.tsx`/`PolicyPage.tsx` shape: a form for the three actions, a rendered result that visibly
distinguishes all four result states (R3's guard), and no free-text "ask the graph a question" box — the
tool takes structured arguments, and the UI should not invite the thing D7/N4 forbids.

### T8 — Walk the demo, write the QA suite

Walks `DEMO.md` end to end against a fresh local instance, records the §Findings table (AGENTS §4a), and
turns each Definition-of-Done item into a named integration test if the earlier tasks have not already named
one for it.

## Files

| File | Task |
| --- | --- |
| `aeon/internal/store/schema.go` | T1 |
| `aeon/internal/graph/schema.go` or equivalent migration block | T1 |
| `aeon/internal/graph/parse.go`, `registry.go` | T2 |
| `aeon/internal/graph/lang_go.go` | T2 |
| `aeon/internal/graph/lang_typescript.go`, further `lang_*.go` | T3 |
| `aeon/internal/tools/write.go` or `aeon/internal/action/git_actions.go` (whichever does not already exist) | T4 |
| `aeon/internal/graph/rebuild.go` | T4 |
| `aeon/internal/graph/query.go` | T5 |
| `aeon/internal/tools/graph.go` (tool registration) | T5 |
| `aeon/internal/protocol/graph.go`, `internal/protocol/method.go` (socket methods) | T5 |
| `aeon/internal/graph/boundary_test.go` | T6 |
| `aeon/ui/src/app/routes.ts` | T7 |
| `aeon/ui/src/features/test-bench/pages/GraphPage.tsx` | T7 |
| `docs/aeon/S30-code-graph/DEMO.md` (§Findings appended) | T8 |

## Ordering rationale

T1 before everything — nothing else can write a row without the tables. T2 before T3 — prove the registry on
one language before registering the rest mechanically. T4 after T2/T3 — incremental rebuild has nothing to
re-parse incrementally until parsing exists at all. T5 after T1–T4 — the tool needs both the schema and a
populated graph to be worth querying. T6 can run any time after T5 exists, placed here so the boundary test
exists before the UI task gives anyone a reason to wire a shortcut through it. T7 after T5 — the UI exercises
`graph.*` over the socket, the same functions the tool calls, never the tool itself (an app has no way to
call a native tool directly). T8 last, always.

## What this slice does not do

See `brief.md` §6 (N1–N6) and the standing non-goals. Restated once here because it is the most likely to be
"simplified" back in during implementation: **no filesystem watcher** (N2), under any framing, including "just
for local dev."

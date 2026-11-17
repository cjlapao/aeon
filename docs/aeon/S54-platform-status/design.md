# Design — S54 Platform status

Rulings **SP1–SP8**; shapes from [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md). T0 re-verifies.

## SP1 — A composed read, one implementation
`platform.status {}` → `{evaluatedAt, retrieval{state, availability, corpora[{component, name, kind, embedModel, embedDim, staleChunks, lastPassAt}]}, graph{state, roots[{root, nodes, edges, lastBuildAt, stale}]}, usage{state, workspace{loops, tokens…, costState}}}`. Each block **calls the existing read function** (the same seam `retrieval.status`, `graph.status/roots`, `telemetry.workspace` use) — no second computation. `state ∈ available | stale | building | unwired | error` with `error` carrying the subsystem's own sentence verbatim. `unwired` ⇔ the seam is nil (V13); a test pins it across build variants.

## SP2 — Previewing a rebuild's cost
`retrieval.reindexPlan {component, corpus}` → `{stale chunks, estimatedInputTokens, cost{usd?, state known|partial|unknown}, model, dim, willReplaceEmbeddings: bool}` computed from the staleness data and the route's price table **without calling the embedder** (T0 finds where the price comes from; if unknown the state is `unknown` and no figure is claimed — S33's three-state rule). A reindex result carries the same figures after the fact. Also `graph.plan`? **No** — graph builds spend no tokens; its confirmation states root, size and the fact it replaces derived rows (regenerable).

## SP3 — Operations: actor, audit, single-flight
`retrieval.reindex`, `graph.build`, `graph.reparse`, `graph.delete` take the stamped actor (`table.ActorFor`; unresolvable ⇒ named refusal; an `agent` actor refused), write one `platform_change` row (`op ∈ ('reindex','graph_build','graph_reparse','graph_delete')`, `subject`, `outcome_ref` (the pass/build id), `summary` fixed template, actor, time) **in the same transaction as the operation's start record**, and keep the existing single-flight tracker: a concurrent request answers `already running — started by <actor> at <time>` (read from the audit/start record) and writes nothing. Params/results are otherwise unchanged (fixtures byte-identical; additive `plan` on reindex results).

## SP4 — What agents get
One read: `index_status` (family `platform`, twin `platform.status` scoped read — **no corpora of other components the agent may not see?** corpora are per `component_root`; T0 decides whether the tool filters to the agent's component(s); if component access has no model, the tool returns freshness only (no names of corpora) — recorded). **No agent control tool and no proposal kind**: a reindex spends money and `graph.delete` discards derived data — a person's act. The existing `search` and `graph` tools are untouched (conditional registration, V13).

## SP5 — Settings are only for real knobs
T0 inventories the constants that govern these subsystems (embedding route/model choice, the S22 write-trigger that begins a pass automatically, staleness thresholds, graph parser limits). A knob becomes an S44 setting **only** if it is an operator choice today hard-coded as a constant **and** changing it is safe at runtime; each such key follows S50's pattern (declared in `settings_decl_platform.go`, read per use, checked at use, health-reported). If none qualifies the slice ships none and says so.

## SP6 — Methods and doors
Reads: `platform.status`, `retrieval.reindexPlan`, `platform.changes {subject?, limit?}` (the audit). Writes: the four existing operations (human door, P4). All `RequiresInitialize`, not `Debug`, off the narrow surface, classified in S47's table.

## SP7 — CLI (P5)
`aeon platform status|plan CORPUS|changes` new; `aeon index status|build|rebuild`, `aeon graph …`, `aeon telemetry …` keep flags/output (goldens) and become audited clients; `rebuild`/`graph delete`/`reparse` print the plan/statement and need `--yes`; `index rebuild` prints the cost plan first. Offline fallback via the same code.

## SP8 — UI (P7)
Settings → System with three panels fed by `platform.status` (state pill incl. `unwired`; per-corpus staleness table; graph roots; usage with three-state cost). Controls: **Reindex** (opens a confirm showing `reindexPlan` — chunks, tokens, cost with its state), **Build/Reparse/Delete graph root** (confirm naming root and that derived rows are replaced), disabled with the daemon's reason when already running; an **Activity** tab (`platform.changes`). The bench Graph/Telemetry pages stay. No hardcoded states/sentences.

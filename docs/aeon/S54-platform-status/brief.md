# Brief — S54 Platform status (retrieval index, code graph, usage)

Written 2026-10-30. **Stage:** 5 · Fleet — Tier 5 of [`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md): "closer to a diagnostics/status panel than something a person configures… don't force them into the same list/create/edit/delete shape".
Shapes from [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (**P1–P7, P9–P11**; no seed set, P8 does not apply). **Depends on:** S22 (retrieval), S30 (graph), S33 (telemetry), S45 (UI components, desk not needed), S47 (surface). T0 re-checks.

## 0. Rules carried — and the shape this slice deliberately is not

Daemon first (parity already holds); a list with details; **a status panel with controls, not a CRUD page** — corpora, graph nodes and telemetry rows are **derived and regenerable** ("the store is the source of truth; files are a disposable mirror"), so there is nothing to seed and nothing a person authors. The management surface is: *is it there, is it fresh, what would a rebuild cost, who rebuilt it*.

## 1. What exists today, measured (2026-10-30)

| Fact | Evidence |
| --- | --- |
| **Retrieval (S22):** socket `search.query`, `retrieval.status` (availability + **per-corpus staleness**), `retrieval.reindex` (one deliberate pass over one **named** corpus; single-flight; embedding spend); CLI `aeon index status|build|rebuild`; corpora are rows (`retrieval_corpus`, per `component_root`) created on demand; absent in a no-retrieval build (V13: the `search` tool and methods are **absent, never present-and-empty**). | `method.go:235-252`, `cmd/aeon/index.go`, `schema.go:2156` |
| **Graph (S30):** socket `graph.explain/path/query/build/reparse/delete/status/roots`; CLI `aeon graph build|reparse|delete|status|roots|explain|path|query`; the agent `graph` tool (read) joins the registry only while its service is wired. | `method.go:447-454`, `cmd/aeon/graph.go` |
| **Telemetry (S33):** four reads `telemetry.loop/component/project/workspace` (S44-extra removes `telemetry.project`; a project's total is `telemetry.component` on its root); CLI `aeon telemetry`; a bench `TelemetryPage`; three-state cost honesty. | `method.go:443-446`, `S33-telemetry/` |
| **No unified view** of "is my index/graph healthy" and **no audit** of who rebuilt, re-parsed or deleted what; **no preview of a rebuild's cost** before spending embedding tokens; UI is bench-only. | — |

## 2. What this slice is

1. **`platform.status`**: one composed read — retrieval (availability, per-corpus staleness, model/dimension, last pass), graph (roots, node/edge counts, last build, staleness), usage (workspace telemetry with its three cost states) — each subsystem with an explicit state (`available | stale | building | unwired | error`); **`unwired` is a first-class honest state** (V13), never "empty".
2. **`retrieval.reindexPlan`** (read-only): which chunks are stale for a named corpus and the **estimated embedding tokens/cost with its honesty state** — so the confirmation before `retrieval.reindex` shows what it will spend.
3. **Actor + audit** for every operation (`reindex`, `graph.build/reparse/delete`) in an append-only `platform_change`, single-flight preserved (a second request answers "already running" naming who started it).
4. **Agent read tool** `index_status` (freshness of the index/graph an agent is about to rely on). **No agent control tool** — operations stay a person's act (they cost money / rewrite derived data).
5. **CLI** `aeon platform status|plan|changes` (+ existing `index`/`graph`/`telemetry` unchanged, now audited) and **Settings → System** panels: Retrieval, Code graph, Usage.

## 3. Out of scope

Retrieval/graph/telemetry semantics, parsers, embedding pipeline, ranking, the capture seams (S22/S30/S33). No new corpus kind, no editing of derived rows, no seeding, no change to V13's absent-not-empty rule. Changing S22/S30/S33 beyond listed actor+audit threading.

## 4. Acceptance criteria

1. **`platform.status` equals its sources**: each subsystem block equals the existing `retrieval.status` / `graph.status+roots` / `telemetry.workspace` reads field-for-field (differential); `unwired` appears exactly when the seam is absent, in every build variant (retrieval/no-retrieval, graph on/off).
2. **`reindexPlan` is honest**: stale-chunk count and token estimate equal what a real pass would process (differential on a fixture corpus), cost carries known/partial/unknown (never a bare number), it writes nothing and embeds nothing.
3. **Operations are audited once**: reindex/build/reparse/delete each one `platform_change` row (peer actor, subject, outcome id) in the same transaction as the operation's start record; refusals, plans, reads and "already running" write none; append-only; single-flight unchanged.
4. **Human door only**: the four operations are absent from the narrow surface, agent services, tool registry and records catalog (guards with canaries); the agent `index_status` read is the only new agent tool; the S22/S30 tools' behaviour and V13 absence are unchanged.
5. **Parity** both ways for `aeon platform` and the existing verbs; **UI** through registered methods only (P34 grep); S22/S30/S33 suites unchanged except listed adaptations.

## 5. Handoff

Done: brief, design, plan, tasks T0–T6. Blocked on: S45 T9 (UI components). T0 first.

> **Amended 2026-10-30 by S44-extra (component tree, CT15):** `project` and `component` are one record — a two-level `component` tree (a root and its children; a root with children *is* a project, derived). Wherever this slice said project, read **root component** (`rootId`/`root_id`/`--root`); scope pickers read `component.list` (`rootsOnly`) / `component.show`; `project.*` methods and `telemetry.project` no longer exist. See [`S44-extra-component-tree/design.md`](../S44-extra-component-tree/design.md).

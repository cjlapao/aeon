# Plan — S47 Tools catalog

## What is settled before planning

`brief.md` and `design.md` TC1–TC13. Short version: a **read-only** catalog of every tool (system and MCP) and of the daemon's own methods, with the "to whom"
answered by one pure function checked against the gate, system tools' **catalog entries** seeded as records (executors and schemas stay code, drift-checked),
a closed capability vocabulary every tool must declare, a closed method classification every method must have, own-view agent tools, a CLI client, and two
Settings panels.

## How we will do it

Two foundations first, in parallel and independent of the seed engine: **tool metadata** (T1) and the **table** (T2). Then the **seed set** (T3, needs S43-extra's engine).
In parallel again: the **pure grants function** (T4) and the **method classification** (T6). Then the **read methods** (T5). Then the clients and guards (T7 tools,
T8 CLI, T9 guards), the UI (T10), close-out (T11).

## Task sequence

### T0 — Re-verify, inventories, settle the mechanical items (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**

### T1 — Tool metadata in code: capabilities, family, governed-by, with a completeness test ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.** Parallel-safe with T2, T6.

### T2 — The `tool` table and the store's SQL half ([`tasks/T2.md`](tasks/T2.md))
**Depends: T0.** Parallel-safe with T1, T6.

### T3 — The `tools.core` seed set (inert), versions golden, drift status ([`tasks/T3.md`](tasks/T3.md))
**Depends: T1, T2, S43-extra T3.**

### T4 — "To whom": the pure grants function, the shared `GrantedSet`, and the differential tests ([`tasks/T4.md`](tasks/T4.md))
**Depends: T1** (and S45/S46 for the policy and MCP inputs; core half can start after T1).

### T5 — The read methods: `tools.list/get/byAgent/status` ([`tasks/T5.md`](tasks/T5.md))
**Depends: T3, T4** (MCP half needs S46 T3).

### T6 — The surface classification and `surface.list/get` ([`tasks/T6.md`](tasks/T6.md))
**Depends: T0.** Parallel-safe with T1–T5 (touches `internal/protocol/surface.go` only).

### T7 — Agent tools: own-view `tools_mine`, `tool_show` ([`tasks/T7.md`](tasks/T7.md))
**Depends: T4, T5.**

### T8 — The CLI as a client: `aeon tools`, `aeon surface` ([`tasks/T8.md`](tasks/T8.md))
**Depends: T5, T6.**

### T9 — The guards: read-only, completeness, MC2, door truth, with canaries ([`tasks/T9.md`](tasks/T9.md))
**Depends: T5, T6, T7.**

### T10 — Settings → Tools and → Surface ([`tasks/T10.md`](tasks/T10.md))
**Depends: T5, T6, T9 and S45 T9** (shared components).

### T11 — Close-out ([`tasks/T11.md`](tasks/T11.md))
**Depends: T0–T10.**

## Sequencing summary

```
T0 ─▶ T1 (meta) ──┬─▶ T3 (seed set; needs S43-extra T3) ─┐
 ├──▶ T2 (table) ─┘                                      ├─▶ T5 (reads) ─┬─▶ T7 (tools) ─┐
 ├──▶ T4 (grants fn; after T1) ──────────────────────────┘               ├─▶ T8 (CLI)   ─┼─▶ T9 (guards) ─▶ T10 (UI) ─▶ T11
 └──▶ T6 (surface table + reads) ────────────────────────────────────────┘                └───────────────┘
```

## The numbers this slice must hold

| Fact | Count |
| --- | --- |
| New migration files | 1 (`tool`) |
| Production seed sets declared | 1 (`tools.core`, **inert**) |
| New socket methods | 7 (`tools.list/get/byAgent/status`, `surface.list/get`) + 2 agent-scoped twins (`tools.mine.list/show`) |
| New agent tools | 2 (`tools_mine`, `tool_show`) — granted to **0** by default |
| Write methods/tools/verbs added | **0** |
| Registry names without a `Meta` after T1 | **0** |
| Registered socket methods without a classification after T6 | **0** |
| Tools whose executor or schema is read from the database | **0** |
| Files in `internal/protocol` importing a new package for the surface table | **0** |
| S43/S43-extra/S44/S45/S46 files edited | **0** |

## Cross-slice coupling

- S43-extra supplies the engine (T3); S45 supplies `Describe`/policy reads and the shared UI components; S46 supplies MCP rows and the scoped read seam. T0 records what has merged; the core-tools half of S47 does not wait for S45/S46.
- `TestRegistryExactNames` is bumped by the slices that add tools; S47 adds two (`tools_mine`, `tool_show`) and counts them the same way.

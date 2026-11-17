# Plan — S46 MCP management

## What is settled before planning

`brief.md` and `design.md` MG1–MG15 (with the verified facts). Short version: the MCP configuration moves from a JSON file into four
tables with `Config.Validate` still the one validator and `internal/mcp` unchanged and store-free (pure `Assemble`/`Disassemble`);
the legacy file is adopted once, all-or-nothing, never silently; management is daemon-first human-door socket methods off the narrow
surface (extending S45's ruling to TD4); agents get **own-view** read tools and **narrowing-only** propose tools; seed sets are
declared (shipping none); a Settings → MCP section with live status, a grants matrix and the two-step grant flow.

## How we will do it

Schema and the pure assembly first, in parallel (T1, T2). Then the seam that switches every existing reader from file to rows with
**byte-identical** output (T3) — characterization-first. Then adoption of the legacy file (T4). Then writes (T5), proposals incl. the
one new kind (T6), the guard amendment (T7) **before** any client, then clients in parallel (T8 tools, T9 CLI, T10 seed declarations,
T11 UI), then close-out (T12).

## Task sequence

### T0 — Re-verify, inventory every config reader, parity table, mechanical items (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**

### T1 — The tables, the audit and import ledger, the store's SQL half ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.** Parallel-safe with T2.

### T2 — `internal/mcp`: pure `Assemble`/`Disassemble`, candidate validation, the round-trip law ([`tasks/T2.md`](tasks/T2.md))
**Depends: T0.** Parallel-safe with T1.

### T3 — The admin seam; every reader switches from file to rows, byte-identically; the read methods ([`tasks/T3.md`](tasks/T3.md))
**Depends: T1, T2.**

### T4 — Adopting the legacy file, and `mcp.importFile` ([`tasks/T4.md`](tasks/T4.md))
**Depends: T3.**

### T5 — Human-door writes, with candidate validation, audit, before/after, and agent-delete protection ([`tasks/T5.md`](tasks/T5.md))
**Depends: T3.** (Adapts S43's merged agent-delete refusal — S46's own change.)

### T6 — Proposals: the `mcp_grant_revoke` kind, `desk` validators and effects ([`tasks/T6.md`](tasks/T6.md))
**Depends: T5 and S45 T1/T5** (the shared registry and generic commit).

### T7 — The guard amendment: TD4 and the MC11 deletability, with canaries ([`tasks/T7.md`](tasks/T7.md))
**Depends: T5, T6.** Gates T8–T11.

### T8 — Agent tools: own-view reads and narrowing-only proposals ([`tasks/T8.md`](tasks/T8.md))
**Depends: T3, T7.** Parallel-safe with T9–T11.

### T9 — The CLI as a client ([`tasks/T9.md`](tasks/T9.md))
**Depends: T5, T7.** Parallel-safe with T8, T10, T11.

### T10 — Seed sets `mcp.servers` and `mcp.grants`, declared and empty ([`tasks/T10.md`](tasks/T10.md))
**Depends: T5, S43-extra T3.** Parallel-safe with T8, T9, T11.

### T11 — Settings → MCP ([`tasks/T11.md`](tasks/T11.md))
**Depends: T3, T5, T6, T7 and S45 T9** (the `AgentPicker`). Parallel-safe with T8–T10.

### T12 — Close-out ([`tasks/T12.md`](tasks/T12.md))
**Depends: T0–T11.**

## Sequencing summary

```
T0 ─▶ T1 (tables) ──┐
 └──▶ T2 (Assemble) ┴─▶ T3 (seam: file→rows, reads) ─┬─▶ T4 (adopt legacy file)
                                                      └─▶ T5 (writes) ─▶ T6 (proposals; needs S45 T1/T5) ─▶ T7 (guards) ─┬─▶ T8 (tools)
                                                                                                                         ├─▶ T9 (CLI)
                                                                                                          S43-extra T3 ─▶ T10 (seed sets) ─┼─▶ T11 (UI; needs S45 T9) ─▶ T12
```

## The numbers this slice must hold

| Fact | Count |
| --- | --- |
| New migration files | 2 (`mcp_tables` incl. audit and import ledger; `proposal` kind rebuild for `mcp_grant_revoke`) — T0 confirms |
| New tables | 6 (`mcp_server`, `mcp_server_tool`, `mcp_agent`, `mcp_declare`, `mcp_change`, `mcp_import`) |
| New socket methods | 13 (5 reads: `mcp.servers/server/grants/vocabulary/changes`; 8 writes: `mcp.server.add/update/remove/setEnabled`, `mcp.grant.declare/revoke`, `mcp.core.set`, `mcp.importFile`) |
| New agent tools | 5 (3 own-view reads, 2 narrowing proposals) — granted to **0** agents by default |
| New proposal kinds | 1 (`mcp_grant_revoke`) |
| Production seed records | **0** (two sets declared) |
| Files in `internal/mcp` importing `internal/store` | **0** |
| Packages other than `cmd/aeon` importing `internal/mcp` | **0** |
| S43/S44/S45 files edited | **0** (S46 adapts S43's merged delete refusal inside its own T5, listed) |

## Cross-slice coupling

- **S45** supplies `internal/desk`, generic `proposal.commit`, the guard-amendment pattern and the `AgentPicker`; S46 reuses them.
- **S43-extra** supplies the engine for T10. **S47** will list the MCP-provided tools in the catalog; S46 does not build that list.
- **S36/S34** keep their seams; only their config source moves.

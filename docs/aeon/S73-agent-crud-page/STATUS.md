# S73 · STATUS

**8/8 built 2026-11-06 — closed.** The paging convention (`docs/aeon/PAGING.md` + `internal/paging`, PG1–PG5),
`agent.rows`, the `agent_change` ledger with its two append-only triggers and the audited doors that replaced the
un-audited writers, the actor on every content road (seven, per deviation 15), `agent.changes`, and
`/config/agents`: kit `Table` with server-side paging/sorting/search, the `SidePanel` create/edit/import seeded from
`agent.view`, type-to-confirm delete, duplicate/export/migrate verbs, and a ledger tab. 20 deviations in
`design.md`, each measured. Evidence: `tasks/T8.md` (every `rc=`, the 44-step Chrome walk rc=0 both modes,
acceptance 1–12 with its proof, the SIZE1 census). Findings **G-1…G-6**: [S73-walk-findings.md](../S73-walk-findings.md).
The demo: [DEMO.md](DEMO.md). Open threads: nothing committed yet (~164 paths); `test/port.test.ts` and the nine
`features/settings` `tsc` errors stay red at base, not this slice's; G-3 (export↔import formats) awaits the agent
plane's own task; the Tauri window is the named gap (S72 T5's shell walk stands in for the shell, not the chrome).

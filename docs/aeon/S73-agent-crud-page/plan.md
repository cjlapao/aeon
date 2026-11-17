# Plan — S73 Agent CRUD page, and the paging convention

Properties live in [`design.md`](design.md): **PG1–PG5**, **AR1–AR8**, **CU1–CU8**. Eight tasks.

| Task | What | Depends on |
| --- | --- | --- |
| [T1](tasks/T1.md) | `internal/paging` (pure) + `docs/aeon/PAGING.md`. No daemon behaviour changes | none |
| [T2](tasks/T2.md) | `agent.rows`: the store's paged read + the protocol method + fixtures + surface + `make protogen` | T1 |
| [T3](tasks/T3.md) | The `agent_change` migration, `AgentChangeInsertTx`, the audited writers, the un-audited writers deleted, the source-walk guard | none |
| [T4](tasks/T4.md) | The actor reaches every path: the protocol stamp, `agentmgr`'s params, the CLI direct door, the desk effect, the seed engine, `ErrAgentNoActor` | T2, T3 |
| [T5](tasks/T5.md) | `agent.changes`: the paged ledger read + fixtures + surface + protogen | T1, T3 |
| [T6](tasks/T6.md) | The page: header with **New agent**, kit `Table` with server paging/sort/search, loading/empty states, i18n, the door guard | T2 |
| [T7](tasks/T7.md) | The `SidePanel` for add/edit (seeded from `agent.view`), the typed-confirm delete, the row actions, the History tab | T5, T6 |
| [T8](tasks/T8.md) | Browser walk, findings table, follow-ups, SLICES/STATUS/DEMO/README records | all |

T1 and T3 may run side by side — different files, no shared symbol. T2 follows T1; T5 follows T1 and T3. T4 follows
T2 and T3 because both edit the same `cmd/aeon/agent_management.go` switch it edits, and a merge there is a conflict
over a `case` body rather than a semantic one — the ordering buys that for one day of queue. T6 cannot start before T2
lands the contract, because the page is typed against `RpcMethods["agent.rows"]`. T8 waits for everything.

## Open questions the tasks answer, not the design

- **Q1** Which free error code is `ErrAgentNoActor`? T4 measures the block (`-31969` is the last of S61's, so
  `-31970` is the expectation) and records the measurement beside the declaration.
- **Q2** Does `Table`'s `pagination` render a page-size selector and a total in the shell's widths, un-clipped? T6
  answers in a browser, and its Notes carry the screenshot verdict — the app has never used this component.
- **Q3** Which `Table` column set fits the shell's content width without horizontal scroll at 1280 px? T6 decides the
  visible columns and which start `defaultHidden`, with the widths recorded.
- **Q4** Does `SearchBar` debounce, or does the page own the debounce? T6 reads the kit type and answers in code.
- **Q5** Does the seed applier's audit row land inside the corpus commit's tx on every apply path (adopt, update,
  refuse)? T4 answers per path, and a refusal must mint nothing.

## Out of scope, recorded for later

- Adopting the convention anywhere but `agent.rows` and `agent.changes` (PG5).
- The three shipped cursor shapes (`retro.list`, `charter.revisions`, `loop.transcript`) staying as they are.
- An `agent.export` ledger row, an undo, or a content-version history for agent rows.
- `agent.edit`'s `fields` blob accepting (and ignoring) identity-shaped keys — design deviation 8.
- `agent.import`'s consent text over the socket — design CU4's finding.
- S72 T4's F-1, F-2, F-4, F-5 and S72 T5's F-6, F-7, F-8.
- Making the Test bench's agent page something better than the legacy panels it keeps.

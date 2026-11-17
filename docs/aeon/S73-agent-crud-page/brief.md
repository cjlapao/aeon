# Brief — S73 Agent CRUD page, and the paging convention it needs

Written 2026-11-06. Prefixes **PG** (paging), **AR** (agent rows), **CU** (the CRUD page). Stage 5 · Fleet, UI + daemon.
**Depends on:** S72 (the Configuration shell this page lives in). It adds two daemon methods, one migration and no Rust.

## 1. Why

S72 gave agent management a home (`/config/agents`), and the operator walked it and said: *this is not a management page, it is a pile of API panels.* What is there today, measured:

- **The list is a `<ul>`.** `ui/src/features/agents/AgentList.tsx` renders the roster as an unordered list — no columns, no sort, no page, no search. There is no way to find one agent among thirty.
- **Nothing on the daemon pages, and nothing on the daemon, is paged.** `internal/store/agent.go:169` `AgentsAll` is `ORDER BY name, id` with no `LIMIT`; the roster read returns every row. Across the daemon's conformance fixtures **zero** carry a `page` key; 34 carry `limit`, `cursor` or `pageSize` — three different shapes (`limit` alone, `limit`+`nextCursor`, `hasOlder`/`hasNewer`) and no written rule for which a new read should use.
- **The verbs are hand-typed text inputs.** `AgentEditForm.tsx` and `AgentDeletePanel.tsx` ask the person to *type a slug* and then a JSON blob. There is no primary action in the header, no row selection, no confirm step, and one delete button that fires on a typed string nobody checked against the row being looked at.
- **Nothing records who changed an agent row.** The `agent` table has no audit companion. Every other plane that a person configures has one — `policy_change`, `mcp_change`, `roadmap_change`, `bug_change`, `learning_change`, `environment_change`, `platform_change`, `automation_change`, `setting_event`. Agent rows are the loaded thing that dispatches loops, and nine separate paths write them (design §AR4 lists all nine), so "who made this agent say this" is unanswerable today.

This slice does the ordinary thing properly: the page becomes a CRUD page — a table with a primary button on top, a side panel for add and edit, a confirm modal for delete — and the daemon gains the paged read the table needs, plus the ledger the writes deserve. The paging shape is written down as the daemon's convention, with the two S73 reads as its first adopters.

## 2. What it delivers

1. **A paging convention** (`docs/aeon/PAGING.md`) and its one implementation, `internal/paging`: `page`/`pageSize`/`sort`/`dir`/`q` in, rows + `page`/`pageSize`/`total`/`sort`/`dir`/`q` out, clamp 1–100 default 20, sort refused **by name** against a closed allow-list, `q` a bound parameter that never reaches SQL text.
2. **`agent.rows`** — the paged, sorted, searchable read the table asks for. Registered beside `agent.list`; the roster's shape does not change by one byte.
3. **The `agent_change` ledger** (one migration): append-only by trigger, one row per write, in the same transaction as the write it records.
4. **Every writing path carries an actor**, and the un-audited store writers stop existing as exported methods — so a path cannot be added later without naming someone (compiler-enforced, PG/AR).
5. **`agent.changes`** — the paged read of that ledger, and a History tab in the panel showing it.
6. **The CRUD page itself**: header with the primary **New agent** button, kit `Table` with server-side paging/sorting/search, `SidePanel` for create and edit (seeded from `agent.view`), type-to-confirm modal for delete, per-row Duplicate and Export, page-level Import and Migrate, internal rows read-only with the daemon's reason.
7. **`ui/src/features/agents/agentsRpc.guard.test.ts`** — the family's first door guard: the page asks exactly its methods, invents no parameter, and does nothing else.

## 3. What it does not do

- It does not paginate the daemon's other reads. The classification table carries 428 entries today (measured by counting `Method…: {` entries across `internal/protocol/surface_table_*.go`; 227 of them are reads). The convention is written and the first two adopters adopt; every other read answers byte-identically until its own slice adopts it (PG5).
- It does not migrate the three existing cursor shapes that are already shipped (`retro.list`, `charter.revisions`, `loop.transcript`). They are named in the convention doc as the prior art and left alone.
- It does not change the roster (`agent.list`), the agent-services enumeration, the tool registry or the narrow projection. The only classification change is a tightening (design §AR8).
- It does not give the Test bench the CRUD page. The bench keeps the raw verb panels as the legacy QA surface it is, and the door that renders them keeps working (operator ruling, 2026-11-06; the bench is slated to disappear). This ends S72's CF9 "one implementation, two doors" **for this page**, recorded as a deviation.
- It does not fix S72 T4's open findings (F-1, F-2, F-4–F-8) or S72 T5's follow-ups, and it does not touch the Rust shell.
- It does not add a version history or an undo for agent rows. The ledger records that a write happened and who did it; it is not a revision store, and the panel says so rather than implying rollback.
- It does not seed, migrate or bulk-edit agent rows beyond the `migrate` verb that already exists.

## 4. Acceptance

1. `agent.rows` answers `{page,pageSize,total,sort,dir,q}` beside its rows; page 2 of a 3-row query returns the right 3-minus-page-1, `total` counts the **filtered** set, and a page past the end returns `[]` with the honest `total` — pinned by tests, not asserted in prose.
2. `sort` accepts only the seven declared names; `sort=slug;DROP` — or any unknown name — is refused naming the name, and no caller string reaches SQL text (the store receives a column chosen from a literal map).
3. `q` matches slug, name and description; a query containing `%`, `_` or `\` matches those characters literally.
4. `agent.list`'s wire bytes are unchanged (its conformance fixtures are untouched and still pass).
5. Nine paths, nine stamps: every one of the writing paths in design §AR4 lands its `agent_change` row naming its actor, and `UPDATE`/`DELETE` on `agent_change` die at the trigger with its sentence.
6. Deleting the audited writers' actor parameter does not compile — the guard test walks the store package's SQL and fails an un-audited writer if one appears.
7. `/config/agents` is a table: a primary button in the header, page controls that drive the daemon, a header click that re-sorts server-side, a search that re-pages to 1, and an empty state that is the daemon's emptiness and not a zero.
8. Add and edit happen in a `SidePanel`; the edit form is seeded from `agent.view`, because `agentmgr.Edit` replaces nine fields wholesale and a form seeded from a table row would blank the other nine.
9. Delete requires typing the slug; the daemon's refusal — including the list of things that reference the agent — prints verbatim and the row stays.
10. The Test bench's `agent-management` page still renders the old panels and still passes its test.
11. `agent.rows` and `agent.changes` are absent from the narrow projection, the agent-services enumeration and the tool registry, and the CRUD family is marked `HumanDoorOnly` with that guard green.
12. Every string on the new page is in `en.json` and `pt.json`, parity test green. No file this slice writes passes 1000 lines (SIZE1, `wc -l` after).

## 5. Risks

- **R1 Nine paths is a wide perimeter.** A path left un-stamped is a silent gap in the ledger — the failure looks like success. Mitigated by making the un-audited writers unexported (the compiler refuses a new road), and by a source-walk guard beside per-road tests.
- **R2 `features/agents/` is shared by two doors.** The bench keeps the raw panels; the product page does not render them. The split is named file by file in design §CU1 so nobody "helpfully" re-merges them.
- **R3 The app has never used the kit `Table`.** `grep -r "<Table" ui/src` is empty today: this page is the first consumer, so the pagination/sorting prop wiring has no in-repo precedent and the layout must be looked at in a browser (T7), not only in jsdom.
- **R4 The contract regenerates.** A new method means `MethodName` consts, registration, conformance fixtures and `make protogen`; protogen **fails** on a missing fixture, so the fixtures are part of the design, not an afterthought.
- **R5 A new migration re-mints goldens.** 56 → 57 files, 101 → 102 applied, `frozenFileCount`/`frozenMigrationCount` (`internal/store/backup_run_test.go:505`), `bkDatabaseLine`'s "101 applied" (`cmd/aeon/bk_golden_test.go:130`). Each is updated in the same commit as the migration file, with the reason in its comment.
- **R6 `agent.edit` replaces nine fields wholesale.** Any form that shows fewer fields than the row has will silently clear the rest. The panel is seeded from `agent.view` and posts the full row back; a test plants a row with content in a field the form does not render and proves it survives.
- **R7 Pagination without an index is a sort of the whole set.** `agent` carries a unique index on `slug` and one on `name` only. Sorting by `createdAt` or `updatedAt` scans and sorts; at a few hundred rows that is invisible, and the design says so rather than adding an index nobody has measured the need for.
- **R8 SIZE1, measured at the base.** The files this slice writes into: `internal/store/agent.go` 597,
  `internal/agentmgr/agentmgr.go` 390, `cmd/aeon/agent_management.go` 235, `cmd/aeon/agent.go` 609,
  `cmd/aeon/proposal.go` 741, `internal/protocol/agent_management.go` 35, `ui/src/features/agents/types.ts` 76 — all
  with room. One is already past the line: **`internal/protocol/method.go` at 2,311**, a standing violation that
  S73 touches with two consts and two registrations. That is not the "substantial change" the rule makes a split
  obligation, and splitting a 2,311-line method table is its own slice's job — recorded here so nobody discovers it
  mid-PR and either ignores the rule or mass-splits the file.

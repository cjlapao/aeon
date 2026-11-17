# Design — S73 Agent CRUD page, and the paging convention

Properties **PG1–PG5** (the paging convention), **AR1–AR8** (agent rows and their ledger), **CU1–CU8** (the page). Every
file:line below was measured on 2026-11-06 on this tree; the measurement appendix lists them in one place.

Two decisions the operator settled before this document was written, so they are premises and not proposals: the
daemon-wide shape is the **page window** (`page`/`pageSize`/`total`), chosen over a keyset cursor because a management
table has to answer "page 3 of 17" and a jump-to-page; and the audit ledger is **in frame** with a read beside it
(`agent.changes` + a History tab), because a ledger nobody can read is a ledger nobody can audit.

---

## The shape on the wire

### `agent.rows` — the table's read

```jsonc
// params — every key optional; an absent key takes the default named here
{ "page": 1, "pageSize": 20, "sort": "name", "dir": "asc", "q": "review" }

// result — the rows, and the request AS THE DAEMON APPLIED IT
{
  "agents": [
    { "slug": "reviewer-plus", "name": "Reviewer Plus", "description": "…", "version": "1",
      "internal": false, "signed": false, "origin": "user",
      "createdAt": "2026-10-30T09:00:00Z", "updatedAt": "2026-10-30T09:00:00Z" }
  ],
  "page": 1, "pageSize": 20, "total": 37, "sort": "name", "dir": "asc", "q": "review"
}
```

- **Sort names, closed set (seven):** `name · slug · version · internal · signed · createdAt · updatedAt`. Default `name asc`.
  Every `ORDER BY` appends `, slug asc` as a tiebreak, so two rows sharing a name cannot shuffle across a page boundary.
- **`dir`:** exactly `asc` or `desc`. Any other word is refused naming it.
- **`pageSize`:** default 20, clamped into 1–100 (a value of 500 answers 100; a wrong JSON type is refused, never coerced).
- **`page`:** 1-based; below 1 becomes 1; past the last page answers `[]` with the honest `total`.
- **`q`:** trimmed, matched over `slug`, `name`, `description`; longer than 200 characters is refused naming the cap.
- **No filters the table does not show.** `internal`, `origin` and `signed` are columns in the answer, not query keys — a
  filter key is a control, and a control the page does not render is a control nobody can use. A later slice that adds
  the column filter adds the key beside it.

### `agent.changes` — the ledger's read

```jsonc
{ "page": 1, "pageSize": 20, "slug": "reviewer-plus" }   // slug optional; absent means every row
{
  "changes": [
    { "id": 9, "op": "edit", "slug": "reviewer-plus", "summary": "edit reviewer-plus (9 content fields)",
      "actorKind": "user", "actorId": "1000", "occurredAt": "2026-11-06T10:12:03Z" }
  ],
  "page": 1, "pageSize": 20, "total": 4, "slug": "reviewer-plus"
}
```

One order — newest first (`ORDER BY id DESC`) — so it takes no `sort`/`dir`, and no `q`: the ledger is small and
already narrowed by `slug`.

**Unknown keys are ignored, and one closed set is refused** — measured against `conformance/README.md`'s T3 rule
(an unknown field in params or a result must be IGNORED, `forward-compat.json` being the standing test of it), which
this design first got wrong: a paged read must not reject `pageFromTheFuture`, because a client that rejects unknown
fields turns every additive change into a breaking one. What *is* refused, naming it, is a closed vocabulary whose
silence would be a lie — `limit`, `cursor`, `offset`, `order`, `search`, and the actor pair (a client that sent
`limit: 5000` believes it asked a question the daemon never answered). That is S59's move with its identity vocabulary
(`rejectMCPIdentityKeys`), not a general unknown-key policy. Deviation 9.

### Refusals

Bad paging is **bad params** (`ErrBadParams`, `internal/protocol/handshake.go:104` — the same voice that already
says `"%q is not a parameter"`), refused before the store is asked. A write that reaches its door with no person is a
typed refusal in the family's own voice, `ErrAgentNoActor`, sibling-shaped to `ErrPolicyNoActor` / `ErrMcpNoActor` /
`ErrRoadmapNoActor` / `ErrPlatformNoActor` (−31956). Its code is measured free when T4 declares it — the highest block measured today is
S61's `−31964...−31969` (`internal/protocol/errors.go:1958`), so the expectation is −31970 — and it is registered in the same commit as the error's declaration, with the
measurement in the comment. An existing refusal is never re-used loosely: "no person" and "bad page" are two voices.

The paging refusals are the door's, not the store's: `cmd/aeon/agent_rows.go` resolves the sort name first so a client
typo answers −32602 with the allow-list, while the store keeps its own check because it is the one that knows the
columns. And a key the read does not take is ignored, unless it is in that closed set above (T3).

The seam returns `ErrBadParams` for a paging shape (the daemon maps it to JSON-RPC −32602, `internal/daemon/daemon.go:1799`,
and `internal/daemon/agent_mailbox.go:551` is the precedent of a **seam** returning it), and keeps the `refusal string`
channel — `ErrAgentRefused`, the app prints it verbatim — for what the domain refuses. A bad page and an AM4 delete
refusal are different facts, and the app renders them differently.

---

## PG · The paging convention

### PG1 Four keys and a search, one meaning everywhere

A paged read takes `page`, `pageSize`, `sort`, `dir`, and `q` where a search is meaningful. No method invents a fifth
key, and no method gives these four another meaning. Written down in `docs/aeon/PAGING.md`, which also names the prior
art it does not pretend away: 34 conformance fixtures carry `limit`, `cursor` or `pageSize` today, in three shapes —
`limit` alone (`mcp.changes`, `inbox.list`), `limit` + `nextCursor` (`retro.list`, `charter.revisions`), and
`hasOlder`/`hasNewer` (`loop.transcript`). Zero fixtures carry `page`. Those reads are not broken here; the doc says
which shape a *new* read uses and why the page window is the one a table needs.

### PG2 One implementation, in `internal/paging`

A new, pure, dependency-free package — request clamp, offset, sort resolution, LIKE escape, the answer's echo — because
"every endpoint does it" is only true if it is one function and not fifty copies. Its surface, all of it total and
testable without a database:

```go
type Request struct{ Page, PageSize int; Sort, Dir, Q string }            // as the wire asked
type Bounds  struct{ DefaultSize, MaxSize int; DefaultSort string; Sorts map[string]string }
func (r Request) Normalise(b Bounds) (Request, error)                     // clamp + default; refusal names the key
func (r Request) Offset() int
func (r Request) Column(b Bounds) (column, dir string, err error)          // the ONLY bridge wire-name → SQL word
func Like(q string) string                                                 // %…% with % _ \ escaped
type Answer struct{ Page, PageSize, Total int }                            // embedded in every paged result
```

`Bounds.Sorts` is a map whose **values are SQL column literals** (`"createdAt" → "created_at"`). A caller cannot
reach the `ORDER BY` with a string it sent; it reaches it by naming a key the map holds. Anything else is refused
naming the name (PG4). The plane's SQL stays in the plane's store file — `internal/paging` knows nothing about agents.

### PG3 The answer echoes the ask, and never lies about the end

`total` is the count of the **filtered** set, taken before the `LIMIT` in the same read. A page past the end answers
`[]` with that honest total — not an error, not a silent redirect to the last page. `pageSize` is clamped and the
**clamped** value is what the answer echoes, so the table's page count cannot disagree with the daemon's.

### PG4 Nothing the caller typed reaches SQL text

`sort`/`dir` are resolved through the literal map; `q` is a bound parameter and nothing else. LIKE's metacharacters are
escaped (`\` → `\\`, `%` → `\%`, `_` → `\_`) and the pattern carries `ESCAPE '\'`, applied **before** the count so
`total` counts what the table shows. A test plants a row named `100%_sure\thing` and proves a query of `%`, `_` and
`\` finds it and only it.

### PG5 Adoption is per endpoint, and a test is the proof

`agent.rows` and `agent.changes` adopt now, each with the tests that pin PG1–PG4 against the real store. Every other
read keeps answering byte-identically; each adopts in its own slice, by calling `internal/paging` and pinning the same
tests. The convention doc carries the adoption checklist and the list of who has adopted, so the next slice inherits a
rule and not a rumour. A method that does not adopt is not failing anything — today's claim is only "the machinery
exists, two endpoints use it, and the rule is written".

---

## AR · Agent rows and their ledger

### AR1 `agent_change`: one append-only ledger, one migration

Modelled on `mcp_change` (`internal/store/migrations/20261101131955_mcp_tables.sql:36-48`), the closest sibling:

```sql
CREATE TABLE agent_change (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  op          TEXT NOT NULL CHECK (op IN ('create','duplicate','edit','delete','import','migrate','seed')),
  -- one slug per row, except the migrate run, which is about the set and not
  -- about one agent: no other op may carry a NULL slug.
  slug        TEXT CHECK (slug IS NULL OR length(slug) BETWEEN 1 AND 256),
  CONSTRAINT ck_agent_change_slug_names_a_row CHECK (slug IS NOT NULL OR op = 'migrate'),
  summary     TEXT NOT NULL CHECK (length(summary) BETWEEN 1 AND 1024),
  actor_kind  TEXT NOT NULL CHECK (actor_kind IN ('user','cli','system')),
  actor_id    TEXT NOT NULL CHECK (length(actor_id) BETWEEN 1 AND 256),
  occurred_at TEXT NOT NULL
);
CREATE INDEX idx_agent_change_slug ON agent_change (slug, id);
CREATE TRIGGER agent_change_no_update BEFORE UPDATE ON agent_change BEGIN SELECT RAISE(ABORT, 'agent_change is append-only (S73 AR1)'); END;
CREATE TRIGGER agent_change_no_delete BEFORE DELETE ON agent_change BEGIN SELECT RAISE(ABORT, 'agent_change is append-only (S73 AR1)'); END;
```

- **No foreign key to `agent`.** The ledger must outlive the row: a `delete` row names a slug that no longer exists.
  `mcp_change` makes the same choice (`server TEXT`, nullable) for the same reason.
- **`slug` names the row, and only the `migrate` run has no single row to name** (the CHECK says so in SQL). A
  `agent.changes {slug}` read therefore never shows the run's row — it belongs to the unfiltered ledger — and the
  History tab renders a NULL slug as `every agent`, the truth rather than a fake key (T7).
- The store accessors are `AgentChangeInsertTx` and the `agent.changes` read. There is deliberately no update or delete
  accessor: the trigger is the enforcement and the test is the proof, exactly as `McpChangeInsertTx` argues.

### AR2 The row and its ledger move in one transaction, or nothing does

Every audited writer opens one tx, writes the row and the ledger row, and commits (`McpChangeInsertTx` +
`InsertAsyncWorkerAudited` are the two precedents; `TestBugAuditAndWriteAreOneTransaction` is the test to copy). A
refusal at either step rolls back both, so there is no state where an agent changed and the ledger does not know.

`agent.edit` always writes, so it always mints a ledger row: `Edit` touches `updated_at` whether or not a content
field moved, so the write is real and the row is honest. The `roadmap.update` no-op-with-`changed:false` shape is not
copied here — that would change `agent.edit`'s shipped result for a nicety. Recorded, not silently different.

### AR3 The un-audited writers stop existing

The plan this design replaces was "add audited twins and pin that only they are called". Measured against the code
that is weaker than it sounds: `CreateAgent` has four call sites today, three of them inside `internal/store` itself
(`agent.go:229` `DuplicateAgent`, `:252` and `:372` via `ImportAgentCard`/`MigrateFirstPartyAgents`), and `UpdateAgent`
has two — so "one caller each" is a claim that needs a walker to hold. Stronger and simpler: **the exported un-audited
writers are deleted.**

- `CreateAgent`, `UpdateAgent`, `DeleteAgentBySlug` become unexported tx primitives (`createAgentTx`, `updateAgentTx`,
  `deleteAgentTx`) taking a `*sql.Tx`, and the only exported ways to write an `agent` row are the audited doors:
  `CreateAgentAudited`, `DuplicateAgentAudited`, `UpdateAgentAudited`, `DeleteAgentAudited`, `ImportAgentCardAudited`,
  `MigrateFirstPartyAgentsAudited`, plus the seed pair `SeedAgentInsert`/`SeedAgentUpdate` (which gain the ledger row
  on their caller's tx).
- **The compiler is then the guard**: adding a road means touching a primitive, and the primitives take a `*sql.Tx`,
  so a road appears only inside an audited function. Beside it, `TestAgentRowsAreWrittenOnlyThroughTheAuditedPath`
  walks the package's SQL strings (`INSERT INTO agent`, `UPDATE agent SET`, `DELETE FROM agent`) and names any file
  that is not the audited owner — modelled on `TestMcpRowsAreWrittenOnlyThroughTheAdminSeam`, and canaried by planting
  a rogue writer in a `_test`-adjacent helper to prove the walker has teeth.
- The store cannot be trusted with a person it invented, so the actor arrives as a value: `store.AgentActor{Kind, ID}`
  with one validation (`Valid()`) refusing a blank kind, a blank id, an id of `"0"` and any kind outside the CHECK —
  refused **before SQL**, the law the MCP and platform doors already keep.

### AR4 Nine paths, nine stamps

Every path that changes an `agent` row, measured by walking the callers of the writers. Rows 7 and 8 were measured AGAIN at build time and failed (deviation 15): the table now shows seven content roads, and the two struck rows where the plan expected them, so a reader of the plan alone does not go looking for a stamp that has nothing to stamp.

| # | Path | Lands through | Actor stamped | Precedent copied |
| --- | --- | --- | --- | --- |
| 1 | socket `agent.create` | `agentmgr.Create` → `CreateAgentAudited` | `user:<peer uid>` | `mcpWriteEdge` (`mcp_admin.go:698`) |
| 2 | socket `agent.duplicate` | `agentmgr.Duplicate` → `DuplicateAgentAudited` | `user:<peer uid>` | as 1 |
| 3 | socket `agent.edit` | `agentmgr.Edit` → `UpdateAgentAudited` | `user:<peer uid>` | as 1 |
| 4 | socket `agent.delete` | `agentmgr.Delete` → `DeleteAgentAudited` | `user:<peer uid>` | as 1 |
| 5 | socket `agent.import` | `agentmgr.Import` → `ImportAgentCardAudited` | `user:<peer uid>` | as 1 |
| 6 | socket `agent.migrate` | `agentmgr.Migrate` → `MigrateFirstPartyAgentsAudited` | `user:<peer uid>` | as 1 |
| 7 | ~~CLI direct `aeon agent import`~~ | **measured: not a content road** — `importAgentPackage` runs `plugin.ImportChecked`, which writes `plugin_agent` and `plugin_transition` and never the `agent` table | none (deviation 15) | — |
| 8 | ~~desk commit of kind `agent_import`~~ | **measured: not a content road** — the same `importAgentPackage`, the same lifecycle tables | none (deviation 15); the `proposal <id>` summary stays as the formatter's reserved shape | — |
| 9 | the seed engine (a corpus commit applies `agents.firstParty`) | `agentseed.Applier` → `SeedAgentInsert`/`SeedAgentUpdate`/`SeedAgentRetire` | `system:seed-engine` | `mcpseed/applier.go:295-301` |

- **The mechanism is the one MCP uses, so `Options.AgentManage`'s signature does not change.** `agentManageCall`
  already receives the `*Session` and throws it away (`agent_management.go:22`); it keeps it, and for the **writes**
  runs an edge that stamps the peer into the context (`WithAgentActor` / `AgentActor`, copied from `WithMCPActor` /
  `MCPActor`, `mcp_admin.go:499-515`). The closure in `cmd/aeon` reads it back and refuses a missing stamp
  (`ErrAgentNoActor`), so a road that bypassed the door cannot write. Reads (`view`, and the new `rows` / `changes`)
  take no actor — SP6 draws the line at the acts, not the reads (`platform_door.go:53-57`).
- **Path 8 fails closed, in words.** `desk.CommitActor(ctx)` and `desk.ProposalIDFrom(ctx)` are the two channels
  `Commit` installs (`decide.go:619-627`, `proposal_ctx.go`); an effect that needs them and does not get them refuses
  with a sentence naming the missing half, and never guesses an identity. That is S46's and S59's rule, quoted.
- **Path 6 is a measured correction to what the chat design recorded.** Option (b) listed `system:migrate`. Measured:
  `MigrateFirstPartyAgents` has exactly one caller (`agentmgr.go:349`), reached only by the socket verb — a person
  pressed the button, so the row names that person. `system` is reserved for the one genuinely person-less writer, the
  seed engine. The migration's op is still `migrate`; only the actor differs from what was sketched in chat.
- **The `migrate` run mints one ledger row, not one per created row.** The write is the run; its summary is
  `migrate derived 33 rows from plugin_agent`, and each created row remains discoverable by its own `createdAt`. One
  row per derived agent would bury the ledger in entries that name nobody individually.
- **`agent.export` mints nothing.** It writes a file, not a row. `mcp_change` has no `export` op either; the ledger
  pairs with row writes, and inventing an op with no row would make AR2 unenforceable.

### AR5 The table's row is a summary; `agent.view` is still the content

The row shape carries the nine columns a list can honestly hold. It does not carry `prompt`, `skills`, `metadata` or
the dispatch fields, and `agent.view` therefore stays load-bearing: the panel opens a row by calling `agent.view`, not
by reading the table's cache. That is not a nicety — `agentmgr.Edit` replaces **nine fields wholesale**
(`agentmgr.go:222-230`: Name, Description, Functionality, Prompt, Skills, Metadata, AvatarType, AvatarColor,
AvatarMood), so a form seeded from a partial row would write the missing ones empty. R6's test plants content in a
field the form does not render and proves it survives an edit (brief R6).

The corollary is what the edit form **does not** offer: `version`, `tools`, `temperature`, `handoff`, `policyAgent`,
`origin`, `internal`, `signed` are not writable through this door at all, so the panel shows them read-only (or not at
all) rather than accepting an edit that would be silently dropped. Slug and id stay immutable (AM2). A name change
does not re-slug — the panel says so in the field's own helper text.

### AR6 `agent.changes` is the ledger's read

The four-key paged read in the wire section above, newest first, `slug` optional. Its second job is proving PG5 is not
a one-off: the same `internal/paging` call site answers both doors, and its tests are the same four properties over a
different store.

Filtering by `slug` cannot see a `migrate` run's row, because that row has no slug to match (AR1) — so the unfiltered
answer is the only place it appears, and the panel's History tab offers that view as "everything" rather than inventing
a key or a fake slug for it (CU4).

### AR7 The verb set the page uses is the set that already exists

`agent.view`, `agent.create`, `agent.edit`, `agent.delete`, `agent.duplicate`, `agent.export`, `agent.import`,
`agent.migrate` keep their params and their results exactly as shipped
(`cmd/aeon/agent_management.go:75-186`): `{slug}`, `{name,description,functionality}` → the row, `{slug,fields}` →
`{"edited":slug}`, `{slug}` → `{"deleted":slug}`, `{slug,name}` → the row, `{slug,path?}` → the path, `{path}` →
`{agent,result}`, `{}` → `{created:n}`. Refusals stay `ErrAgentRefused` with the daemon's sentence verbatim. A page
that needed a new verb to render would be a page that got the wrong daemon contract.

### AR8 The classification tightens, and the guard proves the claim

`surface_table_agents.go` today marks only `enable` / `disable` / `revoke` as `HumanDoorOnly`; the CRUD family
(`create · edit · delete · duplicate · import · export · migrate`) is `SurfaceWrite` unmarked. All seven gain the mark,
and the two new reads carry it too. The mark is a claim with a proof, not a wall: `surface.go:36-40` demands the method
be unreachable from the narrow projection, the agent-services enumeration and the tool registry, and
`TestHumanDoorOnlyMeansAbsentFromEveryNonHumanDoor` enforces it. Measured, the claim is already true —
`MethodAgentCreate` appears in exactly three non-test places (the dispatch switch, the registration, the table) and
nothing in `internal/tools` — which is why this is recorded as a **tightening of a shipped classification** with the
measurement attached, and why it needs no behavioural change to be honest. Reads marked `HumanDoorOnly` have siblings
(`history.export`, `platform.changes`, `retrieval.reindexPlan`).

---

## CU · The page

### CU1 One page, one door; the bench keeps its panels

The Configuration page at `/config/agents` is the CRUD page. The Test bench's `agent-management` page keeps rendering
today's raw verb panels (`AgentManagement.tsx` and its panel files) as the legacy QA surface it is. This is where S72's
CF9 "one implementation, two doors" stops for this page, by operator ruling — the bench door is slated to disappear, so
handing it a `LiveController` and a product frame buys nothing and costs a shared component two contracts.

The file split, so "shared" means something specific:

| File | Who renders it after this slice |
| --- | --- |
| `AgentsScreen.tsx`, `AgentPanel.tsx`, `agentsModel.ts`, `agentsRpc.guard.test.ts`, `AgentsScreen.test.tsx` | new; the Configuration page only |
| `types.ts`, `AgentAvatar.tsx`, `AgentDetail.tsx` | shared (shapes and the read-only content renderer) |
| `AgentManagement.tsx`, `AgentList.tsx`, `AgentViewPanel.tsx`, `AgentCreatePanels.tsx`, `AgentEditForm.tsx`, `AgentDeletePanel.tsx`, `AgentExportPanel.tsx`, `AgentImportPanel.tsx`, `AgentMigratePanel.tsx`, `useCall.tsx`, `AgentManagement.test.tsx` | the bench only (raw QA surface) |

`features/config/pages/agents.ts` points at `AgentsScreen`; the bench page keeps pointing at `AgentManagement`. Neither
imports the other. `AgentManagement.tsx`'s header records the ruling in one paragraph, because the next reader will
otherwise ask why two pages exist.

### CU2 The action bar carries the page's verbs — and renders no second title

S72's `ConfigPageFrame` already renders the group line, the `h1` (**Agents**) and `def.summary` as the subtitle
(`ConfigPageFrame.tsx`, CF6; R1 solved it by the frame standing down, not by editing a page). So the screen renders an
**action bar**, not another header: on the right **New agent** (primary), with **Import** and **Migrate** as secondary
buttons; the `SearchBar` on the left of the same row. `pages/agents.ts` gains the `summary` line the frame renders, in
English — the registry's summaries are English for all fifteen pages today, and translating the registry is not this
slice. Page-level verbs live in the action bar; a row's verbs live in the row.
`Import` opens a `SidePanel` with one field (the package path) and the daemon's own answer afterwards — created,
updated or no-op; `Migrate` opens the confirm modal and reports `{created:n}`. Writes are disabled while
`canWrite` is false, and the reason is the shell's existing not-connected sentence rather than a page-invented one.

### CU3 The table is the kit `Table`, and the daemon is the brain

First consumer of the kit's `Table` in this app (`grep -r "<Table" ui/src` is empty today), wired with
`manualSorting` + `sortState` + `onSortChange` + `pagination: TablePaginationState` — the kit's page state
(`{page,pageSize,total,onPageChange,onPageSizeChange}`) is the wire's page state, one field each way, so no page
count is computed in the browser (PG2's UI half). `q` is a `SearchBar` debounced 300 ms; a search or a sort change
resets to page 1; the controls go `loading` while a call is in flight. `useRpcQuery` is the fetch — its params are
keyed by JSON serialization, which is exactly what a paging object needs. The app wrapper module
(`src/components/kit.tsx`) gains a `Table` export following the file's own pattern — a thin component that passes this
app's surface default through `Table`'s own surface props (`panelVariant`/`panelTone`, whose value set T6 reads from
`SurfaceVariant` rather than guessing `"glass"`) — because every other surface component gets its default there.

The row's action menu holds `View`, `Edit`, `Duplicate`, `Export`, `Delete`. Empty means the daemon answered empty:
the kit `EmptyState` with the search term echoed back when one is set, and a different sentence when the store
genuinely has no rows.

### CU4 Add and edit live in one `SidePanel`

One panel, four modes (`mode: "create" | "edit" | "view" | "import"`) — the row's verbs all live in the one place a
person is already looking. Create posts
`agent.create` with the three fields it takes. Edit opens with `agent.view`, holds the whole row in its draft, and
posts `agent.edit {slug, fields}` with the full content back — AR5 is the reason it must be the whole row. The panel
shows the daemon's verdict after a write (the created slug, or the edit's `{edited}` answer, or the refusal verbatim)
and refetches the table on success; the table never mutates its own rows from a form's hope. `agent.changes` fills the
panel's History tab, which carries a **this agent / everything** toggle — `slug` simply absent for "everything", so the
toggle costs no new wire key and is the only way to see the rows that name no single agent (a `migrate` run, a seed
apply). The ledger is per row and per run at once, and the tab says which it is showing.

`agent.import`'s consent text does not reach the socket today — `importAgentPackage` prints it to an `io.Writer`
(a CLI/desk thing, `desk.Effect.Apply`'s writer) while the socket verb returns the row and the result word. The panel
therefore shows what the daemon actually answered and does not imply it showed a consent review. Recorded as a finding
for the follow-up list, not papered over.

### CU5 Delete is a typed confirmation, and the refusal is the copy

`useTypeToConfirm` (`src/lib/useConfirm.tsx`) — the house wrapper over the kit's `DeleteConfirmModal`, already the
shape `BlueprintDrawer` uses. `confirmValue` is the row's **slug**; the description is the daemon's own law in the
app's words; on refusal — which for a referenced agent names the blueprints, policies and MCP servers
(`agentmgr.go:247`'s AM4 refusals) — the sentence prints whole and the row stays in the table.

### CU6 Internal rows are read-only, visibly

`internal` refuses edit and delete (AM1, `agentmgr.go:214` and the delete path). The table keeps those rows visible —
they are most of the rows — and the row's menu renders Edit/Delete disabled with the daemon's reason as the tooltip.
The panel opens them read-only and offers Duplicate, which AM1 allows. Nothing is hidden, nothing is greyed out
without a sentence.

### CU7 Every string is in both catalogs

Every new label, helper, button, empty state, modal sentence and column header goes to `src/lib/i18n/en.json` and
`pt.json` in the same commit, and the existing parity test stays green. The raw bench panels keep their English
verbatim — they are QA surface and rewriting their text is not this slice.

### CU8 The door guard

`ui/src/features/agents/agentsRpc.guard.test.ts`, the family's first, in the house shape (`blueprintsRpc.guard.test.ts`
and its siblings): the census of `rpcTyped`/`useRpcQuery` calls in the **product files** (`AgentsScreen.tsx`,
`AgentPanel.tsx`, `agentsModel.ts`) equals the allow-list — the ten methods `agent.rows · agent.changes · agent.view ·
agent.create · agent.edit · agent.delete · agent.duplicate · agent.export · agent.import · agent.migrate` — no method
invented, no paging key invented beyond PG1's five, zero `localStorage`/`fetch`/Tauri-invoke references in the new
files, and en/pt parity for the keys the page uses. The census's scope is a **closed literal list, not a glob**: the
bench's legacy panels are named files in an exemption constant, because they call `agent.list` (the roster, via
`useFact`) which is not the product page's door — S59's `environment_propose_guard_test.go` "closed one-file literal
exemption" is the precedent, and a glob would let a new file walk in unexamined. The canary principle the sibling
guards use: a planted fake method makes it fail, so the guard is not vacuous.

---

## The migration, and the goldens it re-mints

One file, `internal/store/migrations/<14-digit-ts>_agent_change.sql`, its leading `-- ` line the description, the
timestamp being its ID (`internal/store/migrations/README.md`; §3 of `aeon/AGENTS.md`). 56 → 57 files. Four goldens
move with it, each with the reason in its own comment:

| Golden | Where | From → to |
| --- | --- | --- |
| `frozenFileCount` | `internal/store/backup_run_test.go:505` | 56 → 57, plus the running name list |
| `frozenMigrationCount` | same test, `:509` | 101 → 102 |
| `bkDatabaseLine` | `cmd/aeon/bk_golden_test.go:130` | `101 applied` → `102 applied` |
| the doctor/backup golden bodies | `cmd/aeon/bk_golden_test.go` fixtures | the same line, wherever it is embedded |

The `agent_change` fingerprint enters `internal/store/fingerprint.go`'s normalised shape, so a backup taken before
this slice and one after differ by exactly that table — which is the point, and is checked rather than asserted.

## The contract bill

`ui/src/proto/contract.ts` is generated from the Go `MethodName` consts plus **committed conformance fixtures**, and
protogen fails on a missing fixture. So, per new method (`agent.rows`, `agent.changes`):

1. the const in `internal/protocol/method.go` (beside `MethodAgentList`, ~:696);
2. the registration in the same file's agent block (~:2178-2199), `RequiresInitialize: true`, not `Debug`;
3. `internal/protocol/conformance/agent-management/rows.json` and `changes.json` — request/response pairs, and one more
   each for a refusal (`sort` named, `pageSize` clamp) so the wire's refusals are on the record;
4. every pinned method-name list the guards hold — found by grepping for `agent.list` outside the registration, each
   widening carrying a comment that names S73 and the reason (the S70 T3 precedent's "widened WITH reasoning");
5. the surface entries (§AR8);
6. `make protogen` then `make check-protogen` (both run in `make ci`).

No Rust changes: `lib/bridge.ts` already awaits every command and the shell's command surface is generic (S72 T5).

## Deviations, recorded where they will be found

1. **S72 CF9's "one implementation, two doors" ends for this page** (operator ruling 2026-11-06): the CRUD page belongs
   to Configuration; the bench keeps the raw verb panels as legacy QA surface and gets no `LiveController`. §CU1.
2. **The audited twins became replacements, not twins.** The chat design said "twins are the only writers, guarded by
   a caller count"; measured, `CreateAgent` already has three intra-store callers, so the count would not have held.
   The un-audited exported writers are deleted instead and the primitives take a `*sql.Tx`. §AR3.
3. **`agent.migrate` stamps the person, not `system:migrate`** — measured: its one caller is the socket verb. §AR4.
4. **`agent.edit` always mints a ledger row** (its `updated_at` always moves), rather than gaining a `changed:false`
   result to skip no-op audits. §AR2.
5. **`agent.changes` is added** — one read beyond the chat design's list, at the operator's confirmation, so the ledger
   is answerable and PG5 has a second adopter. §AR6.
6. **`features/agents/` gains the family's first `agentsRpc.guard.test.ts`.** §CU8.
7. **`agent.*` CRUD moves from plain `SurfaceWrite` to `HumanDoorOnly`** — a tightening of a shipped classification,
   with the three-place grep measured and stated. §AR8.
8. **`agent.edit`'s `fields` blob still accepts identity-shaped keys and ignores them** (`agentmgr.Edit` reads nine
   fields and no other). The new methods refuse identity keys by key; the shipped verb is left alone rather than
   silently narrowed, and the finding is on the follow-up list. §Refusals.
9. **Unknown keys are ignored, not rejected — one closed set is refused.** The first draft of this design had the
   paged reads rejecting any key they do not know, which `conformance/README.md`'s T3 forbids (it is the property that
   makes additive evolution safe, and `forward-compat.json` is its only standing test). The refused vocabulary is now
   closed and named in §"agent.changes". Recorded because the code and the earlier sentence disagreed, and the
   protocol's law won.
10. **The row type is `store.AgentRow`, and its bounds live beside it.** T2's step named the shape
    `AgentRowSummary` and put `AgentRowBounds` in the door. Shipped: `AgentRow` — it is the row's nine columns and not
    a summary of something richer, and a reader who expects "summary" to have a fuller sibling will go looking for one
    — and the bounds map is a `store` package literal (`AgentRowBounds`, `internal/store/agent_page.go:67`) that the
    door and the query both read, because the wire names and the column names have exactly one owner. `internal/paging`
    therefore stays dependency-free: it holds the arithmetic, never a plane's vocabulary.
11. **Six fixtures, not four.** T2 named `rows`, `rows-past-end`, `rows-bad-sort`, `rows-q-escape`. Two were added:
    `rows-clamp.json` (PG2 — `pageSize: 5000` answers `100`, and the answer's echo is what tells the caller) and
    `rows-refused-limit.json` (deviation 9's closed vocabulary, with the alternative inside the message). Both are the
    kind of contract a client otherwise guesses at from an error it received at 3 a.m.
12. **The seed plane has THREE content writers, not two.** Row 9 of AR4's table (and T3's step 4) named
    `SeedAgentInsert` and `SeedAgentUpdate`. Measured in `internal/store/agent_seed.go`: one `INSERT INTO agent` and
    **two** `UPDATE agent SET` — `SeedAgentRetire` is the third writer of the same table, on the same caller-owned
    transaction, and it mints `seed <slug> retired`. AR2's law holds for a retirement exactly as for a creation; row 9
    now names all three.
13. **`MigrateFirstPartyAgentsAudited` is all-or-nothing.** Pre-S73 the run looped, creating each derived row in its
    own transaction, so a mid-run refusal left a partial run committed behind it. The audited door wraps the whole run
    in one transaction with its one ledger row — S51's and S52's sweep law (refuse, name, move nothing) applied to the
    writer that predates that law. AR4's "one row for the run" therefore has a second, stronger reading: the run is one
    write.
14. **A table-level `CHECK` must be the LAST column-item of a SQLite `CREATE TABLE`.** `ck_agent_change_slug_names_a_row`
    written mid-list fails the migration at open with `near "summary": syntax error` (measured against `sqlite3` on three
    variants of the same DDL, 2026-11-06). The constraint ships last in the file, and the law is written in
    `internal/store/migrations/README.md` where the next migration author will read it, not only where it bit.
15. **AR4's paths 7 and 8 are not content roads.** The CLI's `aeon agent import` and a committed
    `agent_import` proposal both end in `importAgentPackage`, which runs `plugin.ImportChecked` — the lifecycle import
    (`plugin_agent` + `plugin_transition`), never the `agent` table. Measured twice: at design time by walking the
    writers' callers, and at build time by walking the callers of the two verbs (`cmd/aeon/agent.go:114`,
    `cmd/aeon/proposal.go:717` — the only two). So there is no `cli:cli` stamp to add and no committer to read from the
    desk: the tree has SEVEN content roads — the socket's six, all flowing through `cmd/aeon/agent_management.go`, and
    the seed engine — and `agent_actor_test.go` proves the two struck rows write their own plane and mint nothing here.
    The `user:<committer>` + `proposal <id>` summary stays written into `AgentChangeSummaryImport`, reserved rather than
    wired, and the door says in its own comment that it passes the empty string.

 16. **`agent.changes` refuses TEN keys, not the seven T5's step 3 lists.** Step 3 names the closed set
     (`limit · cursor · offset · order · search · actor · actorKind`) and step 4 then asks for a fixture in which an
     *invented `sort` key is refused naming it* — and a read that ignores `sort` cannot produce that fixture. The two
     sentences cannot both be literal, so the set ships as the seven plus **`sort` · `dir` · `q`**: this read has one
     order and one filter, so accepting those three and dropping them would let a History tab render a sort it never
     performed, which is the same lie the first seven exist to prevent. Unknown keys stay IGNORED, unconditionally
     (T3's forward-compat law, which outranks both readings here), and `agent_changes_test.go` proves the two halves side
     by side — `{"pageFromTheFuture":3}`, `{"reason":…}` and `{"slugFilter":…}` all answer page 1 at 20 rows, while each
     of the ten arrives as −32602 naming itself. Consequence for the generated contract, measured in T5's Notes: because
     `protogen` builds `params` from EVERY fixture including the refusal fixtures, `contract.ts` now lists
     `"sort"?: string` on `agent.changes` exactly as it already lists `"limit"?: number` on `agent.rows`. That is the
     generator's shape, not this plane's permission; changing it is a `tools/protogen` change whose blast radius covers
     every family that ships a bad-params fixture, and it is not taken here.

 17. **CU7's two catalogs became four: the page's strings live in `en-agents.json` / `pt-agents.json` fragments.** CU7
     says "every new string in `src/lib/i18n/en.json` and `pt.json` in the same commit". Measured before writing: both
     core catalogs are **970 lines**, and the page needs 58 — a commit that honoured the sentence would cross the
     repository's one non-negotiable rule (SIZE1, 1,000, `wc -l` after the change) in the act of obeying a slice rule.
     The fragment mechanism already exists for precisely this and was created by the slice that crossed the line
     (S57 T11 split `settings.gates.*` out for the same reason; ten fragments ride `src/lib/i18n/index.ts` today), and
     `test/i18n.test.tsx`'s whole-file parity assertion reads the two core files, so a fragment's parity is owned by
     its feature's guard — as memory, triage, roadmap and the rest do theirs. `agentsRpc.guard.test.ts` owns it here:
     equal key sets across the two fragments, every key the sources compose present in both, and the Portuguese
     actually translating. The user-visible behaviour of the two arrangements is identical, because `APP_LOCALES` is
     the merged object either way.
 18. **The name column carries no avatar.** T6's step 3 asked for "`name` (with the avatar via `AgentAvatar`)".
     Measured: `store.AgentRow` is `slug, name, description, version, internal, signed, origin, created_at, updated_at`
     — **no `avatar_type` / `avatar_color` / `avatar_mood`**, and deliberately so, because AR5 keeps content out of a
     window and `AgentAvatar` takes the whole `Agent`. Drawing an avatar would mean either inventing a colour the daemon
     never sent or fetching every row's content to decorate a list, and the second is the exact thing the window exists
     to prevent. The name cell therefore shows the name over the `description` — both real columns of the answer, and
     `description` had no column of its own — and the avatar appears where it has a source: the panel `agent.view`
     fills (CU4). `AgentAvatar.tsx` is untouched and still shared.
 19. **A refusal is quoted, not translated, and CU6's "tooltip" is a menu item's `description`.** CU6 wants the
     read-only rows' disabled items to carry "the daemon's reason as the tooltip". Two mechanical facts: the kit's
     `MenuItem` has no `tooltip` prop (it has `description`, its per-row second line, rendered by `MenuRow` at
     `index.js:76046`), and the reason is `agentmgr`'s English sentence — putting it in the catalogs would produce a
     Portuguese paraphrase of a law, which is a second law (RM9's one-string rule, and the reason S50/S51's guards
     refuse to let an authority sentence be DEFINED in code at all). So `agentsModel.ts` holds the two sentences
     byte-for-byte as `agentmgr` writes them, and `agentsRpc.guard.test.ts` reads
     `../../../../internal/agentmgr/agentmgr.go` and fails if either literal has moved, with a negative control proving
     a paraphrase is not the sentence. The consequence is a UI test that reads Go — a new kind of edge in this folder,
     bought deliberately so that "the tooltip is the daemon's reason" is a checked fact rather than an intention.

 20. **T7 widened two conformance fixtures, and `contract.ts` grew with them.** AR5 needs the form's field list to be
     type-checked against the row, and `contract.ts` is generated only from the conformance fixtures
     (`tools/protogen`, `make protogen`), so `AgentDraft` can only be as wide as `agent.view`'s fixture is. Measured
     before the change: `conformance/agent-management/view.json` carried a nine-key body — the S43 fixture predates
     S43-extra T4, which is the migration that gave `store.Agent` its dispatch columns — and `edit.json`'s `fields`
     arm carried the three fields a form would post if it were the hazard this task exists to close. Both fixtures now
     carry the whole 24-key row (`view.json`'s response, `edit.json`'s params), which is what the daemon actually
     answers and asks: `agentmgr.Edit` takes a row and re-reads the rest, and the verbs' real answers were measured in
     T1. Two consequences, both accepted deliberately. (a) `contract.ts`'s `agent.view.result` is 24 keys and
     `agent.edit.params.fields` is 24 keys, so every existing consumer still type-checks (measured:
     `npx tsc -b --noEmit --force` reports the same nine pre-existing errors, none under `src/features/`) — widening a
     response arm is additive for readers. (b) The fixture's avatar words were corrected to the migration's CHECK
     vocabulary (`minion`/`idle`, where the fixture had written `default`/`neutral` — words the database has never
     admitted); the fixture was describing a row the daemon could not store, and a page typed against it would have
     offered both. `make protogen`, `make check-protogen` and
     `go test -tags sqlite_fts5 ./internal/protocol -run 'Conformance|Fixture|Surface'` are green with the change, and
     the guard `agentsRpc.guard.test.ts` now compares the generated 24 keys against the page's two field lists in both
     directions, so the fixture and the form cannot silently separate again.

## Measurement appendix

| Fact | Where it was measured |
| --- | --- |
| roster read has no `LIMIT` | `internal/store/agent.go:169` (`AgentsAll`, `ORDER BY name, id`) |
| zero fixtures carry `page`; 34 carry `limit`/`cursor`/`pageSize` | `grep -rl '"page"' internal/protocol/conformance/` → 0; the three-shape list in PG1 |
| 428 classified surface entries, 227 reads | counted `Method…: {` entries in `internal/protocol/surface_table_*.go` |
| `agent.create` reachable in 3 non-test places, none in `internal/tools` | `grep -rn "MethodAgentCreate\|\"agent.create\"" --include=*.go .` |
| `Edit` writes exactly nine fields | `internal/agentmgr/agentmgr.go:222-230` |
| `CreateAgent` callers today | `agentmgr.go:114`, `store/agent.go:229/252/372` |
| the actor ctx channel + fail-closed refusal | `internal/protocol/mcp_admin.go:499-515`, `cmd/aeon/proposal.go:464-472`, `internal/desk/decide.go:619-627`, `proposal_ctx.go` |
| the seed engine's `system` stamp | `internal/mcpseed/applier.go:295-301` |
| `mcp_change`'s DDL + triggers | `internal/store/migrations/20261101131955_mcp_tables.sql:36-48` |
| migration count goldens | `internal/store/backup_run_test.go:505,509`; `cmd/aeon/bk_golden_test.go:130` |
| the kit's page/sort props (`TablePaginationState` 7754-7760, `TableSortState` 7750-7753, `pagination`/`sortState`/`manualSorting` inside `TableProps` at 7821+), and that the app has never used `Table` | `node_modules/@cjlapao/ui-kit/dist/index.d.ts`; `grep -r "<Table" aeon/ui/src` empty |
| the bench page and the Configuration page both mount `AgentManagement` today | `src/features/config/pages/agents.ts`, `src/features/test-bench/pages/AgentManagementPage.tsx` |

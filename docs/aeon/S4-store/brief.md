# Brief — S4 Store

> ## REDIRECTED — use the framework's database contracts, expand what is missing
>
> **Decision reversed on 2026-10-11, by the user.** The earlier note said Aeon owns the store outright
> because the framework's service is GORM-only. **That was the wrong conclusion from a correct fact.**
>
> > *"It's fine to add the direct access to SQLite, but we need to use the contracts of the database,
> > including the configurations and whatnot. So instead of building the connector, the configurations,
> > where the file is, etc — we use what we have in the framework and expand what we need."*
>
> **This is now a slice about expanding a contract, not about owning a subsystem.** Four things were
> verified rather than assumed, and together they remove most of what this slice was going to build.
>
> ### 1. sqlite-vec works through the framework's GORM path — proven
>
> `gorm.io/driver/sqlite v1.6.0` imports **`github.com/mattn/go-sqlite3`** — the same CGO driver DB18
> wanted — and `mattn/go-sqlite3` is already an indirect framework dependency. So the extension registers
> on the connections the framework opens. Measured:
>
> ```
> sqlite_vec.Auto()                                  ← MUST BE CALLED, not blank-imported
> gorm.Open(sqlite.Open(dsn), &gorm.Config{})         ← exactly how the framework opens it
>
>   vec_version() through GORM:     v0.1.6
>   journal_mode via raw *sql.DB:   wal
>   vec0 virtual table:             created
> ```
>
> **DB18 holds and gets cheaper**: there is still no load step and no extension-missing state, and it needs
> no bespoke connector to get there. **The one new requirement is ordering** — `sqlite_vec.Auto()` must run
> before the database service initialises, the same constraint `internal/secrets` and `internal/appenv`
> already have.
>
> ### 2. The raw `*sql.DB` is reachable through the contract
>
> `(*gorm.DB).DB()` returns `*sql.DB`. **This is the seam for the direct access this slice needs** — raw
> SQL, `last_insert_rowid()`, the `vec0` tables. So "direct SQLite access" and "use the framework's
> contract" were never in conflict; I presented them as a choice and they are not one.
>
> ### 3. The config already exists, and this slice must not re-invent it
>
> `framework/core/database/common` publishes `SQLiteConfig{StoragePath, FileName, DSN}`, `PoolConfig`
> `{MaxIdleConns, MaxOpenConns, ConnMaxLifetime}`, `DatabaseType`, and validation with diagnostics. Env
> names: `DATABASE_TYPE`, `DATABASE_SQLITE_STORAGE_PATH`, `DATABASE_SQLITE_FILE_NAME`,
> `DATABASE_SQLITE_DSN`, `DATABASE_POOL_MAX_*`.
>
> **Already landed outside the loop:** `internal/appenv` feeds Aeon's XDG state directory and `state.db`
> into exactly those variables, because the framework defaults `StoragePath` to `"."` — the process's
> current directory, which for a daemon means two shells open two different databases without saying so.
> **Do not add a second path resolver, a second DSN builder, or a second config struct.**
>
> `SQLiteConfig.DSN` is a documented raw-DSN escape hatch, opened as-is and overriding the composed path.
> **That is where DB16's pragmas belong.**
>
> ### 4. The DSN pragma syntax is driver-specific, and the wrong one fails SILENTLY
>
> Measured against `gorm.io/driver/sqlite` + mattn:
>
> | DSN | Result |
> | --- | --- |
> | `?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)` | **journal=delete, foreign_keys=0** — silently ignored |
> | `?_journal_mode=WAL&_foreign_keys=1&_busy_timeout=5000` | journal=wal, foreign_keys=1 ✓ |
>
> The first is `glebarez/sqlite` syntax. It does not error — it just does not apply, so a store that looks
> configured is running without WAL or foreign keys. **DB13's test must assert the pragmas are in force by
> querying them, never that the DSN contains them.**
>
> ### What this slice still owns
>
> - **The `Store` contract**: `Name() · Init(ctx, *gorm.DB) · Health(ctx) · IsEnabled() · Dependencies() · Migrate()`.
>   Aeon's tables implement it.
> - **The schema and the numbered migration ledger** (DB8, DB9), and **I3's fresh-equals-migrated
>   equivalence test** — the invariant that most needs to survive this redirection. Q1 for this slice is
>   now: **does the framework's own migration machinery subsume the ledger, or sit beside it?**
> - **`sqlite_vec.Auto()` before the database service initialises**, and a test asserting the ordering.
> - **The two-pool question, which is now genuinely open.** DB15 wanted a single-connection writer pool
>   plus a reader pool; `PoolConfig` is **one** pool. The honest options are: accept one pool with
>   `MaxOpenConns(1)` and measure, or expand the contract upstream. **Decide with a measurement, not from
>   the old design** — and note that `busy_timeout` working (above) weakens the original argument that
>   WAL contention had to be handled at the pool.
> - **Everything above the connection**: transcripts, questions, documents, `seq`, the cost columns.
>
> ### Decisions to re-read before planning
>
> **DB15** (two pools) — open, see above. **DB16** (pragmas via DSN) — holds, via `SQLiteConfig.DSN`, with
> the syntax above. **DB3** (one store per user at an XDG path) — holds, now expressed as the framework's
> config rather than as Aeon's own resolution. **DB18** (vectors always present) — holds, proven.
> **DB10** (the store never renders and never touches git) — unaffected.


**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Translates:** [`docs/_archive/aeon-v1-horizontal/02-store/`](../../_archive/aeon-v1-horizontal/02-store/) — `S1`–`S14` become `DB1`–`DB14`, keeping their numbers.
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — §3 and §3a are binding.
**Stage:** 1 · Skateboard, third of four. Depends on S1, S2, S3.

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief corrects it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** §10's primary reading is **v1's Go-free TypeScript store** in `packages/loop-state-mcp/`, which is
> being ported rather than changed, plus the superseded slice that already analysed it.

---

## 1. How to use this brief

> **Constant. Copy verbatim.**

This is a **brief, not a plan.** It states what must be true when the work is done, what must not change along
the way, and where to look to find out the rest. It contains no task breakdown and no implementation code.

1. **Read** the design and the demo, then §7 for where this brief overrides the design.
2. **Investigate** — §10 names what to read. Do not plan before you have those answers.
3. **Plan and decompose** — §12 gives the rules your task sequence must obey.
4. **Self-check** — §13, against your finished plan, before dispatching anything.

Where this brief states a decision, it is settled. Where it poses a question, answer it from the code.

**On the self-check:** every answer needs an artifact — a command's output, an enumeration, a quoted line.

---

## 2. Definition of Done

| # | Check |
| --- | --- |
| **D1** | **`OpenStore` creates, migrates and returns a working store** against a fresh temp directory, with both pools configured. |
| **D2** | **Migrations run exactly once per database.** Open the same file three times; the ledger holds one row per migration. |
| **D3** | **A prior-version database upgrades with rows intact**, through at least one `ALTER`. The prior DDL is held **in the test**, not committed as a binary. |
| **D4** | **A fresh schema and a fully migrated schema are identical**, comparing normalised `sqlite_master` including indexes and constraints. This is the invariant whose breach appears only on upgraded installs. |
| **D5** | **A failing migration rolls back, the open fails loudly, and the schema stays at the prior version.** A partially migrated database is worse than one that refuses to open. |
| **D6** | **A transcript round-trips**, all four block kinds, and `ValidateTranscript` accepts the result. |
| **D7** | **A signed `thinking` block survives write and read byte-identically** — asserted on the **exact** strings. S7 replays these rows. |
| **D8** | **Two inserts at the same `(step_id, seq)` fail on the constraint.** Concurrent append is detectably wrong, not silently lossy. |
| **D9** | **`seq` survives a restart.** Write events, close the store, reopen, and the next `seq` continues. **This is the gap S2 admitted**, and the test is written against it. |
| **D10** | **`foreign_keys` reached the connection**, proven by a **cascade actually happening** — not by reading the pragma back. Reading it proves the statement ran; a cascade proves it took effect. |
| **D11** | **The writer serialises and readers do not block.** Under sustained writes, reads answer promptly and nothing reports `SQLITE_BUSY` at the configured timeout. |
| **D12** | **All three cost states round-trip**, `partial` keeps its `Missing`, and a test proves `cost_state` is **read from the column** rather than inferred from a null `cost_usd`. |
| **D13** | **Scoping holds.** Two loops with the **same slug** in different project roots stay separate across every read, list and update. |
| **D14** | **Every document-path refusal is proven by attempt** — absolute, `..`, a directory outside the allowed set, a disallowed extension, empty. Each returns a named refusal; **none is normalised into range**. |
| **D15** | **There is no `awaiting_user` column**, asserted against `sqlite_master`, and `IsAwaiting` agrees with `PendingQuestion` across every state: none, one unanswered, one answered, several with one unanswered, several all answered. |
| **D16** | **`ask()` writes the question and its thread entry in one transaction.** A forced failure mid-call leaves **neither**. |
| **D17** | **A cold read works.** A child process opens the file and reads a transcript back byte-identically — the honest test of "resumes in a process that never saw the writer". |
| **D18** | **`vec_version()` answers on a fresh store**, with no extension-loading step (DB18). |
| **D19** | **No test touches a real store.** The suite runs with the state path pointed at a temp directory, and `$XDG_STATE_HOME/aeon` is never created. |
| **D20** | **`make test` and `make lint` are green**, `make fmt` leaves no diff. |

D4, D9 and D12 are the three that matter. D4 because its breach appears only on someone else's upgraded
install, months later, with no local reproduction. D9 because it is the specific promise S2 deferred to this
slice. D12 because collapsing `cost_state` into a nullable number destroys the lower-bound semantics four
slices have preserved.

**D1–D19 must each exist as named tests.**

D20 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

The slice that makes Aeon remember, and the one the spine rests on: a suspended step exits and resumes cold
(P2), a crashed one recovers the same way (§3a), and both read from here and nowhere else.

Its substance is inherited. `docs/_archive/aeon-v1-horizontal/02-store` established DB1–DB14, ported v1's twelve tables, and added the
two v1 never had — `step_message` for transcripts, `question` + `chat_message` for the conversation that
unblocks a waiting loop. That analysis is not repeated.

**Two things change, and one removes work from a later slice.**

`sqlite-vec` is no longer lazily loaded. S1's B3 settled on `sqlite-vec-go-bindings/cgo`, which vendors the C
and registers it through `sqlite3_auto_extension()` — so it is compiled into the binary and present on every
connection. **S22 loses its `extension-missing` availability reason**; four remain.

And **SQLite concurrency becomes a design decision rather than a pragma.** Go's `database/sql` pools
connections created lazily, and WAL permits one writer with many readers. That has to be arranged: a
single-connection writer pool and a reader pool, with the pragmas in the DSN because a connect hook registered
once can be missed by a connection created later.

**This slice also closes a gap S2 admitted.** S2's `seq` was an in-memory counter that reset on restart, said
so in a doc comment, and said so in its demo. Here it becomes the `event` table's rowid, so a reconnecting
client's cursor answers "did I miss anything" across a daemon upgrade.

## 4. Goal

A store sufficient on its own to reconstruct a step in a process that never saw the one that wrote it.

## 5. Shape and the seam

```
internal/model  ──▶  FormatVersion, ValidateTranscript, MigrateTranscript, Message   (and nothing else)
                            │
   open.go ─▶ migrate.go ─▶ schema.go
      │
      ├── writer pool (MaxOpenConns 1)  ──┐
      └── reader pool                  ──┴─▶ loops · tasks · steps · chat · events · documents · archive
                                                  │
                                            machine.go (pure)
```

### The seams

**Upward — `store.go`.** S5 persists transcripts, S6 writes questions, S7 reads `status='running'` steps.
None of them writes SQL.

**Downward — `mattn/go-sqlite3` only.** No driver abstraction. The superseded slice had one because v1 was
migrating off `better-sqlite3`; Aeon has one driver, and an interface with a single implementation is a cost
with no benefit.

**Sideways — `internal/model`.** Four names. If you find yourself wanting `model.Client` here, something has
been misplaced.

## 6. Locked decisions

- **DB2** — the store API is synchronous.
- **DB4** — scoped tables keep `project_root`.
- **DB6** — `awaiting_user` does **not** exist; the answer is derived.
- **DB8, DB9** — numbered forward-only migrations, one transaction each, may `ALTER`.
- **DB10** — no rendering, no git, no file writes outside the database.
- **DB11** — `cost_state` is a stored column.
- **DB15** — two pools; the writer is single-connection.
- **DB16** — pragmas via DSN, **not** a connect hook.
- **DB17** — `seq` is the `event` rowid; one global sequence.
- **DB18** — vectors are always present. **Do not write an extension-loading path.**
- **DB19** — domain refusals are results; `error` is infrastructure failure.
- **DB20** — only the store allocates `seq`.

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | The design lists thirteen files | Treat it as a target, not a contract. Merge two if a split leaves a file under ~40 lines — **except `machine.go`**, which stays alone because it is pure and its test enumerates its own tables. |
| O2 | Migrations may `ALTER` | Also: **the runner is tested against a database at the prior version**, built from a DDL held in the test rather than a committed binary. A runner exercised only on fresh databases has never run its `ALTER` path, and a committed binary becomes unmaintainable the first time it needs a column. |
| O3 | `chat_message.seq` is unique per loop | **The store assigns it, never the caller.** A caller-supplied sequence is a race, and this is a table two goroutines append to. |
| O4 | The design does not say what happens on a `format_version` mismatch | On read, a `step_message` whose version exceeds `FormatVersion` is a **loud refusal** via `MigrateTranscript`, not a best-effort parse. A newer Aeon wrote it, and a partial parse could silently drop a `thinking` signature. |
| O5 | `question.kind` is one of four values | Store it as `TEXT` with a `CHECK` constraint listing them. Same for `step.cost_state` and `step.status`. A typo must fail at write time, where the stack names the caller. |
| O6 | Two pools | **Reads must not use the writer pool**, even for convenience. A read on the single-connection writer serialises behind every write, and the symptom is a UI that stalls whenever a loop is busy — diagnosed as a UI problem. |
| O7 | The design does not bound `busy_timeout` or the reader pool | State both numbers **and that they are guesses**. D11's QA case is expected to change them rather than pass. |

## 8. Non-goals

- **N1 — No renderer.** `loop.md` / `events.jsonl` / `memory.md` are **retired**, not deferred — see the design's Out of scope. (The old horizontal plan deferred them to a separate module.)
- **N2 — No git.** Project identity and branches are S16's.
- **N3 — No vector tables.** S22 adds them; DB18 means it needs no load step.
- **N4 — No HTTP, no SSE, no MCP.**
- **N5 — No import of v1's database.** Aeon starts empty; both run on separate files.
- **N6 — No ORM or query builder.** Prepared statements, written out.
- **N7 — No caching layer.** WAL on a local disk is fast enough, and a cache is a second source of truth for
  two goroutines to disagree about.
- **N8 — No driver abstraction.** One driver (S1's B3).

### Standing non-goals

- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never point a test at the real store**, and never at a copy of it. Temp directories only.
- **Never run `git clean`.**

## 9. Repo invariants

**I1 — `internal/store` imports from `internal/model` only the four names**, and nothing else under
`internal/`.

**I2 — All SQL lives in `internal/store`.** A SQL string elsewhere is a defect. Name the mechanism that
catches it.

**I3 — One schema definition, proven equivalent.** The DDL in `schema.go` and the ordered steps in
`migrate.go` reach the same shape, and **a test proves it** (D4). This is the invariant whose violation
produces a bug that only appears on upgraded installs and is invisible in review, because both files look
correct alone.

**I4 — Every scoped query composes the scope helper** (`scope.go`). Discipline is not a mechanism: a query
that forgets `project_root` returns another project's rows, and slugs repeat.

**I5 — Only the store allocates `seq`** (DB20). Asserted by grep: no other package writes `event.id`.

**I6 — Reads use the reader pool** (O6), asserted by grep for the writer handle outside write paths.

**I7 — No ambient state.** The state path resolves once, in `open.go`, from an injectable environment.

**I8 — No test touches a real store or the network.**

**I9 — Timestamps are ISO 8601 UTC strings**, matching v1 so the two are comparable by eye during cut-over.

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/02-store/design.md`** and **`brief.md`** — DB1–DB14 and the DDL for the three new tables.
   This slice translates them; do not re-derive.
2. **`packages/loop-state-mcp/src/store.ts`** — v1's twelve `CREATE TABLE` statements, its `MIGRATIONS` array
   and the ledger runner. The shape being carried forward.
3. **`packages/loop-state-mcp/src/validation.ts`** — `ALLOWED_TRANSITIONS`, `CAPS`, `LOOP_PHASE`,
   `TASK_STATUS`, `POC_TRANSITION_OVERRIDES`. Port nearly verbatim; it encodes real operational experience.
4. **`packages/loop-state-mcp/src/loop-files.ts`** — `KNOWN_FILES`, `ALLOWED_DIRS` and the extension set that
   D14's refusals are written against.
5. **`internal/protocol/events.go`** (S2) — the in-memory counter this slice replaces, and its doc comment
   naming this slice.
6. **`internal/model/format.go`** (S3) — the four names this store imports.

### 10.2 The patterns to copy

- **v1's migration ledger** — numbered ids, one transaction each, recorded, run on every open. It works.
- **Composite keys on `(loop_slug, project_root)`.**
- **Prepared statements held on the store**, not re-prepared per call.
- **S3's discriminated results**, and DB19's split between those and `error`.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | Which `_pragma` DSN parameters does `mattn/go-sqlite3` support, and are `journal_mode`, `foreign_keys`, `busy_timeout` and `synchronous` all settable that way? | The driver. **Try it, then prove `foreign_keys` took effect with a cascade** — reading the pragma back proves only that the statement ran. |
| Q2 | Does `sqlite3_auto_extension()` registration via the cgo bindings apply to **every** connection in both pools, or only the first? | Try it: open both pools and call `vec_version()` on each. S25 depends on the answer. |
| Q3 | Exactly which of v1's twelve tables carry `project_root`, and which do not? | `store.ts`. Copy the answer; v1 is not uniform. |
| Q4 | Enumerate every column on v1's `loop` table, marking which Aeon carries. `awaiting_user` is **not** carried (DB6) — say which others are dropped and why. | `store.ts`. A port needs deliberate omissions. |
| Q5 | Is `POC_TRANSITION_OVERRIDES` still needed, and what does it override? | `validation.ts`. Port it or state why not. |
| Q6 | What does `database/sql` do when the single-connection writer pool is busy — block, or error? And does `SetMaxOpenConns(1)` serialise or fail? | Try it. DB15 depends on blocking. |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q7 | Where is `seq` allocated, given the writer is single-connection (DB20)? `last_insert_rowid()` on that connection is the expected answer; confirm it is safe with the pool at one. |
| Q8 | How does S2's in-memory counter get replaced without breaking a live subscription mid-flight? Name the sequencing. |
| Q9 | What are `busy_timeout` and the reader pool size, and on what basis (O7)? State that both are guesses. |
| Q10 | How is D3's prior-version DDL held, and how does someone add a migration later without invalidating the test? |

---

## 11. Behavioural contract

Inherited from `docs/_archive/aeon-v1-horizontal/02-store/brief.md` §11, with these additions and changes:

- Both pools carry the pragmas via DSN (DB16, Q1). `foreign_keys` is proven by a cascade (D10).
- Reads use the reader pool; writes the writer (O6, I6).
- `seq` comes from `event.id` (DB17) and only the store allocates it (DB20, Q7).
- `IsAwaiting` is an existence query; there is no column (DB6, D15).
- `Ask()` writes both rows in one transaction (D16).
- `AppendMessages` assigns `seq` (O3); `ReadTranscript` refuses a future `format_version` loudly (O4).
- `question.kind`, `step.cost_state` and `step.status` carry `CHECK` constraints (O5).
- Domain refusals are results; `error` means infrastructure (DB19).
- `vec_version()` answers with no load step (DB18, D18).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **`schema.go`, `migrate.go`, D4's equivalence test and D3's upgrade test are one task.** They bind a
  single invariant (I3) and splitting them leaves it unenforced in between, which is the failure that only
  appears on upgraded installs.
- **R5** — `machine.go` is its own task; its test enumerates `AllowedTransitions` rather than listing cases.
- **R6** — the transcript and the chat pair are **separate tasks**. They fail differently: one is fidelity, the
  other transactional pairing.
- **R7** — no task over roughly 300 changed lines or 12 files without justification, **except R4's**, whose
  size is the invariant.
- **R8** — the `seq` replacement is its own task and it **removes S2's doc comment** naming this slice.
- **R9** — task files contain no deliberation: no "hmm", "let me", "wait", "on reflection", "I think", "not
  sure", "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md` end to end and amends the QA suite.

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bwait\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines except R4's (R7) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | `grep -rn "SELECT\|INSERT\|UPDATE\|DELETE"` over your planned files | Hits only under `internal/store` (I2) |
| A6 | For every claim about v1's schema, paste the DDL line from `store.ts`. For D3, paste the prior-version DDL and the row counts before and after | Every claim carries evidence. **A D3 test that only opens a fresh database blocks the plan** — it proves the schema, not the runner, and the runner is what a future upgrade depends on |
| A7 | Paste the answers to Q1, Q2 and Q6 | Three, **from running code**. Q1 must include the cascade that proves `foreign_keys` took effect, and Q2 must show `vec_version()` answering on **both** pools |
| A8 | Paste the answers to Q3 and Q4 | The scoped-table list and the `loop` column inventory, from v1's source |
| A9 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task writes a file outside the database | DB10, N1 |
| B2 | Confirm no task touches git | N2 |
| B3 | Confirm no task adds a vector table | N3 |
| B4 | Confirm no task adds HTTP, SSE or MCP | N4 |
| B5 | Confirm no task reads v1's database | N5 |
| B6 | Confirm no task adds an ORM, a cache or a driver abstraction | N6, N7, N8 |
| B7 | **Confirm no task writes an extension-loading path** | DB18 |
| B8 | Quote the `out_of_scope` lines covering N1–N8 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D19 to a task and a named test | §2 |
| C2 | For D10, confirm the assertion is a **cascade**, not a pragma read-back | D10 |
| C3 | For D12, confirm a test proves `cost_state` is read rather than inferred — a `partial` and a `known` both with non-null `cost_usd` staying distinguishable | D12 |
| C4 | For D9, confirm the test **closes and reopens** the store | D9 |
| C5 | For D4, name the test and say how it normalises `sqlite_master` | I3 |
| C6 | For D17, confirm a real child process is spawned | D17 |
| C7 | For D13, confirm the test uses the **same slug** in two roots | D13 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm schema, migrations and both invariant tests are one task | R4 |
| D2 | Confirm `machine.go` is standalone and its test enumerates the table | R5 |
| D3 | Confirm the transcript and chat tasks are separate | R6 |
| D4 | Confirm the `seq` task removes S2's doc comment | R8 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Name the scope helper and confirm every scoped query composes it | I4 |
| E2 | Confirm no package outside the store writes `event.id` | I5, DB20 |
| E3 | Confirm reads use the reader pool | I6, O6 |
| E4 | Confirm the state path resolves only in `open.go` | I7 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | State where `seq` is allocated and why it is safe with the writer pool at one (Q7). |
| F2 | Name the sequencing that replaces S2's counter without breaking a live subscription (Q8). |
| F3 | State `busy_timeout` and the reader pool size, and that both are guesses (Q9, O7). |
| F4 | State how D3's prior DDL is held and how a later migration is added without invalidating the test (Q10). |
| F5 | Confirm `vec_version()` answers on **both** pools with no load step (Q2, D18). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it end to end and fixes whatever does not
match, recording which was wrong.

S4's demo has one step that exists to close a promise: **`seq` continuing across a daemon restart.** S2's demo
told the reader it would reset, and said why. If this step does not work, the earlier admission stands and both
demos must say so — a limitation quietly fixed in one place and still documented in another is worse than
either.

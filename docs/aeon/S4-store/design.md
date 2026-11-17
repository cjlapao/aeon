# Design — S4 Store (Stage 1 · Skateboard)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 1 · Skateboard, third of four
**Depends on:** S1, S2, S3.
**Translates:** [`docs/_archive/aeon-v1-horizontal/02-store/`](../../_archive/aeon-v1-horizontal/02-store/) — **`S1`–`S14` become `DB1`–`DB14`, keeping their numbers**; §Inherited records what changed.

> Decision ids are prefixed **DB** — the superseded slice's `S1`–`S14` become `DB1`–`DB14`, keeping their
> numbers. New decisions start at **DB15**. **The `S` prefix is reserved for slices**, corpus-wide.

---

## Context

The slice that makes Aeon remember, and the one the spine rests on.

Its substance is inherited: `docs/_archive/aeon-v1-horizontal/02-store` established DB1–DB14, the schema for twelve tables carried
forward from v1's domain core, and the two tables v1 never had — `step_message` for transcripts and
`question` + `chat_message` for the conversation that unblocks a waiting loop. The reasoning for each is in
that folder and is not re-derived here.

**It also closes a gap S2 left open, and admitted to.** S2's `seq` is an in-memory counter that resets when
the daemon restarts, stated in a doc comment and in its demo precisely so nobody would build on it. This slice
moves `seq` onto the `event` table, where it survives a restart — which is what makes a reconnecting client's
cursor answer "did I miss anything" across a daemon upgrade rather than only across a dropped connection.

Two decisions change from the superseded slice, and one of them **removes a failure mode from S22**.

**Vectors are no longer optional at the extension level.** DB12 said `sqlite-vec` loads lazily and the store must
work with it absent. That was right when the extension was a `.so` on the user's machine. S1's B3 settled on
`sqlite-vec-go-bindings/cgo`, which vendors `sqlite-vec.c` and registers it through `sqlite3_auto_extension()`
— so it is compiled into the binary and present on every connection. There is no extension-missing state, which
means S22's availability model loses a reason: what remains is no-provider, index-empty, model-mismatch and
dimension-mismatch.

**And SQLite concurrency becomes an explicit design decision** rather than a pragma. Go's `database/sql`
manages a pool of connections created lazily, and SQLite in WAL mode permits one writer with many readers. That
has to be arranged, not hoped for.

## Goal

A store that holds everything a resumed or recovered step needs, migrates itself forward on open, enforces the
status machine, and is sufficient on its own to reconstruct a step in a process that never saw the one that
wrote it.

## Inherited decisions

| # | Status | Note |
| --- | --- | --- |
| DB1 | **superseded by S1's B3** | The driver is `mattn/go-sqlite3` with `sqlite-vec-go-bindings/cgo`, decided at S1 so CI was built for it |
| DB2 | **holds trivially** | The store API is synchronous. In Go that is the default; DB15 is what makes it safe under concurrency |
| DB3 | **holds** | One store per user at `$XDG_STATE_HOME/aeon/state.db`, with the documented fallbacks |
| DB4 | **holds** | Every scoped table keeps `project_root`. It is what makes a git worktree a distinct project for free |
| DB5 | **holds** | Transcripts stored canonical and versioned, using S3's `FormatVersion` |
| DB6 | **holds** | A question is a row; `awaiting_user` does not exist and the answer is derived |
| DB7 | **holds** | `chat_message` is the thread; `question` is the suspension mechanism |
| DB8 | **holds** | Migrations numbered, forward-only, one transaction each, ledger-recorded, run on every open |
| DB9 | **holds** | Migrations may `ALTER` |
| DB10 | **holds** | The store never renders and never touches git |
| DB11 | **holds** | `cost_state` is a stored column, never inferred from a null `cost_usd` |
| DB12 | **superseded by DB18** | Vectors are always present: the extension is statically linked, not lazily loaded |
| DB13 | **holds**, mechanism changes | WAL, `foreign_keys=ON`, `busy_timeout` — DB16 is how they reach every connection in Go |
| DB14 | **holds** | No MCP. The store is a library |

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| DB15 | **Two pools: a single-connection writer and a multi-connection reader** | SQLite in WAL permits one writer and many readers. `MaxOpenConns(1)` on the writer makes that structural rather than something `busy_timeout` papers over — and a daemon serving many projects has many concurrent readers |
| DB16 | **Pragmas are set via DSN parameters**, not a connect hook | `database/sql` creates connections lazily, so a hook registered once can be missed on a connection created later. A DSN parameter cannot be — it is part of what opening a connection *means* |
| DB17 | **`seq` is the `event` table's autoincrement rowid**: one global monotonic sequence. A filtered subscription simply skips ids it does not match | Per-stream sequences would need a counter per project and a way to reconcile them; one global sequence survives a restart for free and makes the cursor a primary-key lookup. Answers S2's "scoped to its stream" with "one sequence, filters skip" |
| DB18 | **Vectors are always available** (supersedes DB12) | `sqlite-vec-go-bindings/cgo` registers the extension via `sqlite3_auto_extension()`, so it is in the binary. There is no extension-missing state, and **S22's availability model loses a reason** |
| DB19 | **Domain refusals are discriminated results; `error` is reserved for infrastructure failure** | "This transition is not allowed" and "the disk is full" need different handling, and a single `error` return makes every caller guess which it got |
| DB20 | **The store owns `seq` allocation, and nothing else may write to `event.id`** | Two allocators produce duplicate or regressing sequence numbers, which breaks every cursor silently rather than loudly |

## Architecture

```
aeon/internal/store/
  open.go        OpenStore, path resolution, the two pools (DB15), DSN pragmas (DB16)
  schema.go      DDL for every table
  migrate.go     the numbered ledger and its runner
  machine.go     AllowedTransitions, Caps, ValidateTransition — ported nearly verbatim
  scope.go       the project_root predicate; every scoped query composes it
  loops.go       project, loop, loop_phase
  tasks.go       task
  steps.go       step, step_message, the cost columns, context_manifest
  chat.go        question, chat_message, the derived isAwaiting
  events.go      event (the seq source — DB17), memory
  documents.go   loop_document, with path confinement
  archive.go     loop_archive, loop_archive_file
  store.go       the public surface
```

Imports `internal/model` for `Message`, `FormatVersion`, `ValidateTranscript`, `MigrateTranscript` — **and
nothing else** under `internal/`.

### The two pools, and why one writer is structural

```go
type Store struct {
    w *sql.DB   // MaxOpenConns(1) — the only writer
    r *sql.DB   // read pool
}
```

SQLite in WAL mode allows one writer and any number of concurrent readers. With a single `database/sql` pool,
two goroutines attempting a write both get connections and the second gets `SQLITE_BUSY`, which
`busy_timeout` then papers over — turning a design property into a retry loop whose latency nobody can
predict.

`MaxOpenConns(1)` on the writer makes the serialisation explicit: a second writer waits at the pool rather
than at SQLite. A daemon multiplexing many projects has many concurrent readers and a handful of writes, which
is exactly the shape this fits.

**Both pools carry the pragmas** (DB16), via DSN:

```
file:state.db?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000&_synchronous=NORMAL
```

`foreign_keys` is **per connection** and defaults off. A connect hook registered once can be missed by a
connection the pool creates later, and the symptom is a cascade that silently does not happen — so it goes in
the DSN, where it is part of what opening means.

### `seq`, and the gap it closes

S2 emitted events with an in-memory counter that reset on restart. That was correct for a slice with no store,
and its doc comment and demo both said so.

Here `seq` becomes the `event` table's rowid (DB17):

```sql
CREATE TABLE event (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,   -- this is `seq`
  loop_slug     TEXT,
  project_root  TEXT,
  kind          TEXT NOT NULL,
  payload       TEXT NOT NULL,
  created_at    TEXT NOT NULL
);
```

One global sequence, not one per project. A subscription filtered to `project_root = X` receives ids with gaps
in them, which is correct: the cursor's contract is *"everything after N that matches my filter"*, and gaps
carry no information a client needs.

The alternative — a sequence per project — needs a counter per project, a way to allocate it atomically, and a
reconciliation story when a client subscribes across projects. All to remove gaps nobody reads.

**Only the store allocates `seq`** (DB20). Two allocators produce duplicates or regressions, and a cursor
handed a duplicate silently delivers the same event twice.

### The two tables v1 never had

Carried forward from the superseded slice unchanged. `step_message` holds the transcript one message per row,
with `UNIQUE (step_id, seq)` making a concurrent append **detectably** wrong rather than silently lossy.
`question` holds the suspension mechanism, `chat_message` the human-facing thread, and they are written in one
transaction because a question with no thread entry is a loop waiting on something the user cannot see.

`awaiting_user` does not exist (DB6). Whether a loop is waiting is:

```sql
SELECT 1 FROM question
WHERE loop_slug = ? AND project_root = ? AND answered_at IS NULL LIMIT 1;
```

A stored flag and an unanswered question can disagree, and when they do nothing says which is right.

### Suspend, resume and recovery, in store terms

```
suspend:  INSERT step_message rows for the transcript as sent
          INSERT question (step_id, tool_use_id, kind, body, options)
          UPDATE step SET status='suspended', context_manifest=?

resume:   SELECT step, its context_manifest, its step_message rows in seq order
          SELECT the answered question

recover:  SELECT every step WHERE status='running'   ← the daemon died mid-step
          the last committed step_message is the recovery point
```

The third path is S7's, and it needs nothing from the store that the second does not — which is the point of
§3a: recovery is the spine with a different trigger. What S7 adds is the **receipt** for an unsafe tool, and
that is a column on `step_message` rather than a new table.

## The demo

See [`DEMO.md`](DEMO.md): `aeon db info`; a transcript written, the daemon restarted, and read back
byte-identically with its `thinking` signature; **`seq` surviving a restart**, which S2's demo said it did not;
two connections working concurrently; the derived awaiting query; and document paths refused.

## Testing

Every test runs against a **temp-directory database**. The superseded slice's rule was never to point a
migration test at the real store; here the rule is stronger, because there is no reason to touch a real one.

| What | How |
| --- | --- |
| Migration idempotence | Open the same file three times; the ledger holds one row per migration |
| Prior version upgrade | A database built from an earlier DDL held in the test upgrades with rows intact, through one `ALTER` |
| Fresh equals migrated | A fresh schema and a fully migrated one have identical normalised `sqlite_master` |
| Failing migration | Rolls back, the open fails loudly, the schema stays at the prior version |
| Transcript round-trip | All four block kinds survive; `ValidateTranscript` accepts the result |
| `thinking` fidelity | A signed block survives write and read **byte-identically** |
| Append safety | Two inserts at the same `(step_id, seq)` — the second fails on the constraint |
| `seq` durability | Write events, reopen the store, and the next `seq` continues rather than restarting |
| Status machine | Every allowed transition succeeds and every disallowed one is refused, enumerated from the table |
| Cost states | All three round-trip; `partial` keeps its `Missing`; a test proves `cost_state` is read rather than inferred |
| Concurrency | The writer serialises; readers do not block; no `SQLITE_BUSY` at the configured timeout |
| Foreign keys | A cascade actually happens — proving the pragma reached the connection |
| Scoping | Two loops with the **same slug** in different project roots stay separate |
| Path confinement | Absolute, `..`, wrong directory, wrong extension, empty — each refused **by attempt** |
| Vectors present | `vec_version()` answers on a fresh store, with no extension loading step (DB18) |
| Cold read | A child process opens the file and reads back a transcript byte-identically |

## Out of scope

- **The renderer.** v1's `renderAll` writing `loop.md`, `events.jsonl` and `memory.md` is **retired** — see S16's
  L25. This note originally deferred it to "a separate module (S11)"; in the vertical decomposition S11 is permissions
  and the renderer was never reassigned. It should not be: the sentence that follows is the reason. **The mirror is
  disposable; the store is the source of truth** — and S9's app plus `aeon loop show` cover both audiences.
- **Git.** Project identity and branch handling are S16's.
- **Vector tables.** S22 adds them. DB18 means it needs no load step, only a migration.
- **Reading v1's database.** Aeon starts empty; both systems run on separate files, which is what makes the
  cut-over reversible.
- **Any HTTP or MCP surface.** S19's.
- **An ORM.** Prepared statements, written out.

## Consequences for later slices

| Slice | Consequence |
| --- | --- |
| S2 | Its in-memory `seq` is replaced here; the doc comment naming this slice comes out |
| S5 | Persists transcripts through this API; the runtime still holds nothing (K6) |
| S6 | `question` and `chat_message` are already here; S6 adds the tool and the suspend path |
| S7 | Recovery reads `status='running'` steps; the receipt is its **own table** — it must exist *before* the tool runs, and the `step_message` row holding that tool's result does not exist yet |
| S22 | **Loses its `extension-missing` availability reason** (DB18); four remain |
| **S25 (Stage 5)** | **The history view is a cross-loop, time-ordered read with drill-down** — every loop by recency, then its phases, tasks, steps and costs. It is the first query in the plan that reads a lot, so **its index lands with this schema rather than as a later migration.** State the shape now: `loop` by last-touched, `event` by `seq` range, and `step` by loop. Nothing else changes — the history view is a **projection**, not a table |

## Open questions

None. DB15's pool sizing and `busy_timeout` are numbers to calibrate, not questions — the QA suite is expected
to change them.

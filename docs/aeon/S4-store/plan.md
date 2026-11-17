# Plan — S4 Store

> **For agentic workers:** written to the `dev-loop-protocol` `plan.md` + `tasks/{taskid}.md` schema.
> Each task's full spec is at `tasks/T{n}.md`.
>
> **Spec:** [`design.md`](design.md) · **Brief:** [`brief.md`](brief.md) · **Demo:** [`DEMO.md`](DEMO.md)
> **Source material:** [`docs/_archive/aeon-v1-horizontal/02-store/`](../../_archive/aeon-v1-horizontal/02-store/) — its `S1`–`S14` are `DB1`–`DB14` here.

## Understanding

### What we understood

The slice that makes Aeon remember, and the one the spine rests on. A suspended step exits its process and
resumes cold (P2); a crashed one recovers the same way (§3a); both read from here and nowhere else.

Its substance is inherited. `docs/_archive/aeon-v1-horizontal/02-store` established DB1–DB14, ported v1's twelve tables, and added the
two v1 never had: `step_message` for transcripts, and `question` + `chat_message` for the conversation that
unblocks a waiting loop. That analysis is not repeated — v1's own store is read as the shape being carried
forward, and `validation.ts` in particular is ported nearly verbatim because `ALLOWED_TRANSITIONS` and
`POC_TRANSITION_OVERRIDES` encode operational experience that re-deriving would discard.

**Two things change from the superseded slice, and one removes work from a later one.**

`sqlite-vec` is no longer lazily loaded. S1's B3 settled on `sqlite-vec-go-bindings/cgo`, which vendors the C
and registers it through `sqlite3_auto_extension()` — so it is in the binary and present on every connection.
**S22 loses its `extension-missing` availability reason**; four remain. That is a failure mode deleted rather
than deferred.

And **SQLite concurrency becomes an explicit design decision rather than a pragma.** Go's `database/sql` pools
connections created lazily, and WAL permits one writer with many readers. So: a single-connection writer pool
and a reader pool, with the pragmas in the **DSN** — because a connect hook registered once can be missed by a
connection the pool creates later, and the symptom of a missed `foreign_keys` is a cascade that silently does
not happen.

**This slice also closes a gap S2 admitted.** S2's `seq` was an in-memory counter that reset on restart, and
both its doc comment and its demo said so. Here it becomes the `event` table's rowid, so a reconnecting
client's cursor answers "did I miss anything" across a daemon upgrade rather than only a dropped connection.

### Probes to run and record before decomposing

| # | Probe |
| --- | --- |
| Q1 | Which `_pragma` DSN parameters `mattn/go-sqlite3` supports. **Then prove `foreign_keys` took effect with a cascade** — reading the pragma back proves only that the statement ran |
| Q2 | Does `sqlite3_auto_extension()` registration apply to **every** connection in **both** pools? Call `vec_version()` on each. DB18 depends on it |
| Q3 | Exactly which of v1's twelve tables carry `project_root`. v1 is not uniform; copy the answer |
| Q4 | Every column on v1's `loop` table, marked carried or dropped. `awaiting_user` is not carried (DB6); say which others go and why |
| Q5 | Is `POC_TRANSITION_OVERRIDES` still needed, and what does it override? |
| Q6 | Does `SetMaxOpenConns(1)` **block** a second writer or error? DB15 depends on blocking |

> Q1, Q2 and Q6 must be **run**. A pragma that appears set, an extension that works on the first pool only, and
> a writer pool that errors instead of queueing are three failures that all look like working code.

### How we will do it

Open first, because every test needs a database. Then the task that binds the slice's hardest invariant.

**Schema, migrations and both invariant tests are one task** (R4). There are two paths to the current shape —
`schema.go` creates it directly, `migrate.go` walks an old one forward — and when they drift the bug appears
**only on upgraded installs**, months later, on someone else's machine, invisible in review because both files
look right alone. A boundary between them leaves that unenforced. The same task holds D3's upgrade test, whose
prior DDL lives **in the test** rather than as a committed binary that goes stale the first time it needs a
column.

Then `machine.go`, standalone and table-driven so its test cannot drift from the table it tests. Then the ported
tables, grouped by how they fail rather than by size: the transcript is about **fidelity**, the chat pair about
**transactional pairing**, and those are different tests.

The `seq` replacement is its own task (R8) and it **removes S2's doc comment** — a limitation fixed in one place
and still documented in another is worse than either.

The surface comes last of the code, carrying the two proofs no single-table test can make: a **child process**
reading a transcript back, and two pools under sustained write.

### What you will have at the end

A store sufficient on its own to reconstruct a step in a process that never saw the one that wrote it, with
`seq` that survives a restart and a writer that serialises predictably rather than retrying.

## Objective

Build `internal/store` so S5 can persist a step, S6 can hold a question, and S7 can recover a crash.

## Tech tags in use

go, docs

## Test suite

aeon-store

## Tasks

### T1 — `open.go`: paths, two pools, DSN pragmas, vectors present
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~280
- detail: tasks/T1.md
- success_criteria:
  - unit test `the state path falls back through opts, AEON_STATE_DIR, XDG_STATE_HOME and ~/.local/state` passes
  - unit test `foreign_keys is ON, proven by a cascade rather than a pragma read-back` passes
  - unit test `both pools carry every pragma` passes, asserted per pool
  - unit test `the writer pool has MaxOpenConns 1 and a second writer blocks rather than erroring` passes
  - unit test `vec_version answers on both pools with no load step` passes
  - unit test `no test creates $XDG_STATE_HOME/aeon` passes
  - the reader pool size and `busy_timeout` are stated in a doc comment as guesses
- files_expected: `aeon/internal/store/open.go`, `aeon/internal/store/open_test.go`

### T2 — `schema.go`, `migrate.go`, and the two invariants that bind them
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~430
- detail: tasks/T2.md
- success_criteria:
  - unit test `a fresh schema and a fully migrated schema are identical` passes, comparing normalised `sqlite_master` including indexes and constraints
  - unit test `a prior-version database upgrades with rows intact, through one ALTER` passes, from a DDL held in the test
  - unit test `each migration runs exactly once across three opens` passes
  - unit test `migrations apply in id order` passes, with a deliberately misordered array
  - unit test `a failing migration rolls back, the open fails loudly, and the schema stays at the prior version` passes
  - unit test `CHECK constraints reject an invalid question kind, cost_state and step status` passes
  - unit test `there is no awaiting_user column` passes, asserted against `sqlite_master`
  - `est_changed_lines` over 300 is justified: schema and ledger bind one invariant (I3, R4)
- files_expected: `aeon/internal/store/schema.go`, `aeon/internal/store/migrate.go`, `aeon/internal/store/schema_test.go`, `aeon/internal/store/migrate_test.go`

### T3 — `machine.go`: the status machine, ported
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~270
- detail: tasks/T3.md
- success_criteria:
  - unit test `every pair in AllowedTransitions is accepted` passes, enumerated from the table
  - unit test `every pair absent from AllowedTransitions is refused` passes, enumerated from the cartesian product
  - unit test `a refusal names both states and the shortest legal path where one exists` passes
  - unit test `Caps matches v1` passes, against values quoted from `validation.ts`
  - unit test `POC_TRANSITION_OVERRIDES behaves as v1 does` passes, or the task states why it was dropped
  - unit test `ValidateTransition returns a result and never an error` passes over invalid input
  - `machine.go` imports nothing — it is pure
- files_expected: `aeon/internal/store/machine.go`, `aeon/internal/store/machine_test.go`

### T4 — `scope.go`, `loops.go`, `tasks.go`
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2, T3]
- est_changed_lines: ~330
- detail: tasks/T4.md
- success_criteria:
  - unit test `two loops with the same slug in different project roots stay separate` passes across read, list and update
  - unit test `every scoped query composes the scope helper` passes, asserted by grep
  - unit test `a task status change the machine refuses is returned as a refusal, not an error` passes
  - unit test `phases round-trip with their status` passes
  - unit test `timestamps are ISO 8601 UTC` passes against a fixed injected clock
  - unit test `reads use the reader pool and writes the writer` passes, asserted by grep
- files_expected: `aeon/internal/store/scope.go`, `aeon/internal/store/loops.go`, `aeon/internal/store/tasks.go`, `aeon/internal/store/loops_test.go`

### T5 — `steps.go`: the step row, cost columns, context manifest
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T4]
- est_changed_lines: ~290
- detail: tasks/T5.md
- success_criteria:
  - unit test `all three cost states round-trip` passes
  - unit test `partial keeps its Missing list` passes
  - unit test `cost_state is read from the column, not inferred from a null cost_usd` passes — a `partial` and a `known` both with non-null `cost_usd` stay distinguishable
  - unit test `context_manifest round-trips with a content hash per file, iterationsUsed and deadlineAt` passes
  - unit test `step.status distinguishes running, suspended, orphaned and done` passes
  - unit test `cost is written and read through one function each` passes, asserted by grep
- files_expected: `aeon/internal/store/steps.go`, `aeon/internal/store/steps_test.go`

### T6 — `step_message`: transcript fidelity
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T5]
- est_changed_lines: ~290
- detail: tasks/T6.md
- success_criteria:
  - unit test `a transcript with all four block kinds round-trips and ValidateTranscript accepts it` passes
  - unit test `a signed thinking block survives write and read byte-identically` passes, asserting the exact strings
  - unit test `two inserts at the same step_id and seq fail on the constraint` passes
  - unit test `AppendMessages assigns seq and the caller cannot supply it` passes
  - unit test `all rows for one append land in one transaction` passes — a forced mid-write failure leaves none
  - unit test `a format_version above FormatVersion is refused loudly` passes
  - unit test `deleting a step cascades to its step_message rows` passes, asserted by row count
  - unit test `no stored content contains a provider-shaped key` passes
- files_expected: `aeon/internal/store/steps.go`, `aeon/internal/store/transcript_test.go`

### T7 — `chat.go`: `question` and `chat_message`, the pair
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T4]
- est_changed_lines: ~310
- detail: tasks/T7.md
- success_criteria:
  - unit test `Ask writes the question and its thread entry in one transaction` passes
  - unit test `a forced failure mid-Ask leaves neither row` passes
  - unit test `answering an already-answered question is refused, naming the existing timestamp` passes
  - unit test `PendingQuestion returns the oldest unanswered question` passes with three outstanding
  - unit test `IsAwaiting agrees with PendingQuestion across every state` passes — none, one unanswered, one answered, several with one unanswered, several all answered
  - unit test `tool_use_id is null for a gate and set for a permission prompt` passes
  - unit test `the store assigns chat_message.seq and the caller cannot` passes
  - unit test `deleting a question NULLs chat_message.question_id` passes
- files_expected: `aeon/internal/store/chat.go`, `aeon/internal/store/chat_test.go`

### T8 — `events.go`: `seq` on the table, replacing S2's counter
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T4]
- est_changed_lines: ~270
- detail: tasks/T8.md
- success_criteria:
  - unit test `seq survives a close and reopen and continues` passes — **the gap S2 admitted**
  - unit test `seq is monotonic with no gaps and no duplicates over a run` passes
  - unit test `a filtered read returns matching events with gaps in seq, and the cursor still works` passes
  - unit test `only the store writes event.id` passes, asserted by grep
  - unit test `memory append and read round-trip` passes
  - S2's doc comment naming this slice is **removed**, and S2's demo text is updated
  - the sequencing that replaces the counter without breaking a live subscription is stated
- files_expected: `aeon/internal/store/events.go`, `aeon/internal/protocol/events.go`, `aeon/internal/store/events_test.go`

### T9 — `documents.go`, `archive.go`
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T4]
- est_changed_lines: ~300
- detail: tasks/T9.md
- success_criteria:
  - unit test `a document round-trips by friendly name and by an allowed path` passes
  - unit test `an absolute path is refused` passes, by attempt
  - unit test `a path containing .. is refused` passes, by attempt
  - unit test `a directory outside the allowed set is refused` passes, by attempt
  - unit test `a disallowed extension is refused` passes, by attempt
  - unit test `an empty name is refused` passes, by attempt
  - every refusal is a named result; **none is normalised into range**
  - unit test `archiving a loop moves its rows and documents in one transaction` passes
  - unit test `no store function outside open.go touches the filesystem` passes, asserted by grep
- files_expected: `aeon/internal/store/documents.go`, `aeon/internal/store/archive.go`, `aeon/internal/store/documents_test.go`, `aeon/internal/store/archive_test.go`

### T10 — `store.go`: the surface, and the two proofs no table can make
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T6, T7, T8, T9]
- est_changed_lines: ~260
- detail: tasks/T10.md
- success_criteria:
  - integration test `a child process opens the file and reads a transcript back byte-identically` passes, spawning a real process
  - integration test `the writer serialises and readers answer promptly under sustained writes, with no SQLITE_BUSY` passes
  - unit test `the surface exports exactly the documented set` passes
  - unit test `the store imports from internal/model only the four names` passes
  - `grep -rnE "\b(SELECT|INSERT|UPDATE|DELETE|CREATE TABLE)\b" aeon/internal aeon/cmd` shows hits only under `internal/store`
  - every exported function has a named consumer, listed in this task's notes
- files_expected: `aeon/internal/store/store.go`, `aeon/internal/store/resume_test.go`, `aeon/internal/store/concurrency_test.go`

### T11 — The CLI: `db`, `step`, `loop`
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T10]
- est_changed_lines: ~290
- detail: tasks/T11.md
- success_criteria:
  - unit test `aeon db info prints path, applied migrations and tables` passes
  - unit test `aeon db info --vectors reports the statically linked version` passes
  - unit test `aeon step show --json prints the canonical transcript` passes
  - unit test `aeon step verify reports byte-identical and the signature length` passes
  - unit test `aeon loop create, list --all and write work, and list is scoped by default` passes
  - unit test `aeon db query awaiting shows the derived query and its result` passes
  - unit test `aeon debug hammer drives sustained writes` passes
  - unit test `aeon ask --save-step persists the step and prints its id` passes
- files_expected: `aeon/cmd/aeon/db.go`, `aeon/cmd/aeon/step.go`, `aeon/cmd/aeon/loop.go`, `aeon/cmd/aeon/ask.go`, `aeon/cmd/aeon/db_test.go`

### T12 — Walk the demo, and amend the QA suite
- engineer: technical-writer-loop
- tech: docs
- depends_on: [T11]
- est_changed_lines: ~180
- detail: tasks/T12.md
- success_criteria:
  - `DEMO.md` is walked end to end on a clean checkout, including the restart steps
  - **S2's demo is updated** where it says `seq` resets, and this task's notes confirm both demos now agree
  - every mismatch is fixed and this task's notes record whether the code or the demo was wrong
  - `docs/test_suites/aeon-store.md` is **amended, not rewritten** — `QAS-` ids stay stable
  - appended cases cover a real upgrade between commits, two real processes, `seq` across a restart, and a corrupted file
- files_expected: `docs/aeon/S4-store/DEMO.md`, `docs/aeon/S2-protocol/DEMO.md`, `docs/test_suites/aeon-store.md`

## Priority order

**T2 and T8 are the deliverable**, and neither is a table.

T2 because it binds the invariant whose breach is **invisible until it is somebody else's problem**. There are
two paths to the current schema — created directly, or migrated forward — and when they drift the bug appears
only on an upgraded install, months later, with no local reproduction, and invisible in review because both
files look correct alone. That is why the schema, the ledger, the equivalence test and the upgrade test are one
task, and why its size exceeds the guidance: a boundary anywhere inside it leaves the invariant unenforced.
D3's prior DDL lives **in the test** for the same reason a committed binary would be worse — it goes stale the
first time somebody adds a column, and then quietly stops testing anything.

T8 because it closes a promise made in another slice. S2 shipped an in-memory `seq`, said so in a doc comment,
and said so in its demo — deliberately, so nobody would build on it. Moving it onto the `event` rowid is small;
the discipline is that the task also **deletes S2's doc comment and updates S2's demo**. A limitation fixed in
one place and still documented in another is worse than either, because a reader trusts whichever they find.

T6 and T7 are separate because they fail differently. T6's worst failure is a `thinking` signature that survives
storage *almost* intact and produces a rejected replay in S7, three slices away, with nothing pointing here.
T7's is a question written without its thread entry — a loop waiting on something the user cannot see, which
errors nowhere.

T1 carries two checks that look pedantic and are not: `foreign_keys` proven by a **cascade** rather than a
pragma read-back, because reading it back proves the statement ran and not that it took effect; and
`vec_version()` on **both** pools, because an extension registered for one is a retrieval slice that works in
tests and fails under load.

T10 is where the slice is proven rather than asserted. A **real child process** reading a transcript is the only
honest test of "resumes in a process that never saw the writer" — everything before it holds the same
connection and the same objects in memory.

A plan ordered by table size would have written `loops.go` first and reached the equivalence invariant somewhere
in the middle, having already migrated a database it never tested against an old shape.

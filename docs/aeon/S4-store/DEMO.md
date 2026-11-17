# S4 Store — the demo

**Stage 1 · Skateboard.** *Third of four. This is where Aeon starts remembering.*

S3 could talk to a model and forget. This slice gives it a memory — and closes a gap S2's demo openly
admitted: `seq` resetting whenever the daemon restarted.

> **Walked end to end on 2026-10-15**, including both restart steps, on a throwaway state directory with a
> replayed provider (no credential, no network). Every output below is **real**, pasted from that walk. Ten
> things did not match the prediction; §Findings at the end records each, and whether the code or the demo
> was wrong. **Finding 10 was found by a person opening the app**, not by the walk — the walk did not open
> the Store tab, and that is the gap §9 now closes.

---

## 0. A throwaway store, so this demo cannot touch yours

```
export AEON_STATE_DIR=/tmp/aeon-s4-demo/state
export LOCALRPC_ENDPOINT_PATH=/tmp/aeon-s4-demo/rpc.sock
mkdir -p /tmp/aeon-s4-demo
make build && cp aeon/aeon /tmp/aeon-s4-demo/ 2>/dev/null || go build -C aeon -o /tmp/aeon-s4-demo/aeon ./cmd/aeon
cd /tmp/aeon-s4-demo
```

`AEON_STATE_DIR` is read in one place (`internal/paths`) and sits above the XDG chain. Everything below runs
against that directory, so **nothing here can write to your real store**.

> **Do not re-paste this block later in the walk.** S2's demo learned this the hard way: a setup block
> containing `rm -rf` re-run against a live socket produced a store and an endpoint that both existed and
> neither worked. There is no `rm -rf` here for the same reason.

---

## 1. There is a database, and it says what is in it

**With no daemon running at all** — that is the point:

```
./aeon db info
```

**You should see** (real output):

```
store   /tmp/aeon-s4-demo/state/aeon/state.db
        wal, 524 KiB, foreign_keys ON, busy_timeout 5000ms
        pools: 1 writer (DB15 — WAL allows one), 4 readers
schema  6 migration(s) applied, latest 0006_step_context_manifest
vectors sqlite-vec v0.1.6 (statically linked)

tables  _migrations         chat_message        event               loop                loop_archive        loop_archive_file
        loop_document       loop_phase          memory              policy              policy_acceptance   policy_event
        project             project_root        question            step                step_message        task
```

**The path is first** because every later slice's debugging starts by finding the file (B10) — and this verb
answers **with the daemon down**, which is when you most need it.

Three things in that output are load-bearing:

- **`foreign_keys ON` is read from the connection**, not inferred from the DSN. The wrong DSN spelling
  produces a store that looks configured and silently performs no cascades — measured, see T1's notes.
- **`1 writer`** is DB15, structural rather than hoped for: WAL permits one writer, and `MaxOpenConns(1)`
  makes a second one wait at the pool rather than meet `SQLITE_BUSY`.
- **`+ N KiB in the -wal`** appears when there is a live write-ahead log. A busy store's main file can read
  as 4 KiB while everything is still in the log — see §Findings 1.

Now the one that proves the extension is compiled in, not loaded:

```
./aeon db info --vectors
```

**You should see:**

```
sqlite-vec  v0.1.6  (statically linked)

There is no extension to install and no way to be missing it: it is compiled
into the binary (B3). A failure here means the binary was built without cgo.
```

**No load step, and no way to be missing it** (DB18). That is the consequence of S1 choosing `cgo` — and it
means **S22 has one less failure mode** than the earlier plan gave it: there is no "extension absent" state
to handle. Both pools are asked, because an extension registered for one would be a retrieval slice that
works in tests and fails under load.

---

## 2. Two projects, one slug, no collision

Start the daemon in another terminal (`./aeon serve`), then:

```
mkdir -p repo-a repo-b
./aeon loop create --slug refactor --project $PWD/repo-a --goal "split the store"
./aeon loop create --slug refactor --project $PWD/repo-b
./aeon loop list --all
```

**You should see** two distinct loops with **the same slug**:

```
  refactor                 /tmp/aeon-s4-demo/repo-b           preflight  2026-10-15T16:46:56.368Z
  refactor                 /tmp/aeon-s4-demo/repo-a           preflight  2026-10-15T16:46:56.360Z

? = awaiting an answer, derived from the question table
```

And scoped by default:

```
./aeon loop list --project $PWD/repo-a
```

```
  refactor                 /tmp/aeon-s4-demo/repo-a           preflight  2026-10-15T16:46:56.360Z
```

**It failed if** the second create overwrote the first, or if the scoped list shows the other project's loop.
Every scoped row is keyed by `(loop_slug, project_root)` together (DB4) — and because a git worktree has a
different path, **it is a different project and gets its own loops for free**.

**Why `list` is scoped by default and `--all` is a flag:** a daemon multiplexes every project on the machine.
An unscoped default would show you every repository's loops whenever you were in one, and you would learn to
ignore the list.

---

## 3. Document paths are refused, not normalised

```
./aeon loop write refactor /etc/passwd            --content x --project $PWD/repo-a
./aeon loop write refactor ../../../etc/passwd    --content x --project $PWD/repo-a
./aeon loop write refactor scratch/notes.exe      --content x --project $PWD/repo-a
./aeon loop write refactor secrets/key.md         --content x --project $PWD/repo-a
./aeon loop write refactor ""                     --content x --project $PWD/repo-a
```

**You should see five refusals, each naming a different reason** — all three parts, every time:

```
aeon: the document name was refused  (-31106)
  what   a document name cannot contain a .. segment (got '../../../etc/passwd'). It is refused rather
         than resolved, because resolving it would write somewhere else
  means  names are confined to the loop: a known name (`plan`), a top-level file, or one level under
         tasks/ or scratch/, with a .md, .jsonl or .txt extension. An absolute path or a `..` segment is
         refused rather than resolved, because resolving it would write somewhere else
  todo   the message names the specific reason; use a known name or a path like `tasks/T1.md`
```

The five `what` lines, verbatim from the walk:

| Attempt | `what` |
| --- | --- |
| `/etc/passwd` | a document must be a name inside the loop, not an absolute path |
| `../../../etc/passwd` | a document name cannot contain a `..` segment |
| `scratch/notes.exe` | unsupported file type (allowed: .md, .jsonl, .txt) |
| `secrets/key.md` | only top-level files and `tasks/`, `scratch/` are addressable |
| `""` | a document needs a name |

Then the two successes:

```
./aeon loop write refactor plan        --content "# Plan" --project $PWD/repo-a
./aeon loop write refactor tasks/T1.md --content "# T1"   --project $PWD/repo-a
./aeon loop read  refactor                                --project $PWD/repo-a
```

```
plan.md  rev 1 · 6 bytes · 1ee55b7ae632
tasks/T1.md  rev 1 · 4 bytes · 08288ec03ae7
plan.md                  rev 1          6 bytes  2026-10-15T16:47:02.577Z
tasks/T1.md              rev 1          4 bytes  2026-10-15T16:47:02.584Z
```

**It failed if** any of the five succeeded, **or if any was silently normalised into range.**

**Why this is a security property and not a convenience:** a document name reaching the store **comes from a
model**. A clamp turns an escape attempt into a successful write somewhere unexpected — and it looks exactly
like the feature working. Try `tasks/../plan.md` too: it *cleans* to a legal name and is still refused,
because the check runs on the raw input before anything is normalised.

---

## 4. `seq` now survives a restart — the gap S2 admitted

This is the step worth doing carefully, because **S2's demo told you it would fail.**

Note where the sequence is:

```
sqlite3 state/aeon/state.db "SELECT max(id) FROM event;"
```

```
29
```

Now **stop the daemon and start it again** (Ctrl-C, then `./aeon serve`), and hold a cursor from *before* the
restart:

```
./aeon watch --after 29
```

**You should see the sequence CONTINUE**, and the events that happened while nothing was watching replayed
from the store:

```
watching s-8642fb40 · kinds=* project=* · from seq 30
  Ctrl-C to stop

    30  daemon.started         daemonVersion=0.0.0-dev endpoint=/tmp/aeon-s4-demo/rpc.sock protocolVersion=1
    31  client.connected       clientName=aeon-cli clientVersion=0.0.0-dev connId=conn-1 …
    32  client.disconnected    connId=conn-1
```

**In S2 this restarted at 1**, and `from seq 1` is what the cursor would have been handed. `seq` is now the
`event` table's rowid (DB17), and `AUTOINCREMENT` never reuses an id — so a cursor answers "did I miss
anything" across a daemon **upgrade** and not merely across a dropped connection.

**It failed if** the number restarts, or if `--after` returns events you already saw. A cursor that silently
redelivers is worse than one that admits it resets.

---

## 5. A transcript survives a restart, byte for byte

A step is saved by `ask --save-step`. To walk this without a credential, replay a recorded capture:

```
go build -C aeon -o /tmp/aeon-s4-demo/fixture-server ./scripts/fixture-server
./fixture-server -fixture aeon/internal/model/testdata/anthropic/signed-thinking.sse -addr 127.0.0.1:18411 &

# restart the daemon with the route pointed at the replay server
AEON_ANTHROPIC_BASE_URL=http://127.0.0.1:18411/v1 ANTHROPIC_API_KEY=unused ./aeon serve
```

Then:

```
./aeon ask --route anthropic --save-step --loop ask --project $PWD/repo-a "what is 17 times 23?"
```

**You should see** the reply, the accounting, and a step id:

```
cost    $0.000705
tokens  in 30 · out 41 · cache-read 0 · cache-write 0
stop    end_turn
step    2  ·  `aeon step show 2` · `aeon step verify 2`
        the saved thinking block is UNSIGNED — `ask`'s stream carries no signature (S3)
```

**Read that last line.** It is the honest limitation of this bridge: S3's streaming surface carries no
thinking signature, so a transcript saved through `ask` holds an **unsigned** thinking block, which S7 could
not replay to Anthropic. The store itself writes and reads a *signed* block byte-identically — that is
asserted on the exact bytes in `transcript_test.go` and across a process boundary in `resume_test.go`. It is
the bridge that cannot supply one. See §Findings 2.

Now **restart the daemon** and read it back:

```
./aeon step show 2
./aeon step verify 2
./aeon step list ask --project $PWD/repo-a
```

```
step 2  /tmp/aeon-s4-demo/repo-a/ask
  phase   build · agent ask:anthropic · status done
  started 2026-10-15T16:48:27.129Z
  ended   2026-10-15T16:48:27.129Z
  cost    $0.00071 (known)

  1. USER
       what is 17 times 23?
  2. ASSISTANT
       [thinking · UNSIGNED]
       The user wants the weather. I should call the tool rather than guess.
       Checking now.

transcript verified · 2 messages · no thinking signature in this transcript
digest 7a9869da9efa5413

1      done       ask:anthropic            unknown (stream-aborted-early)
2      done       ask:anthropic            $0.00071 (known)
```

Two things worth noticing in that last listing:

- **Step 1 is a call that FAILED** (the replay server was not up yet) and it is still in the store, with
  `unknown (stream-aborted-early)`. A failed ask that records nothing **loses the spend** (M15), and those
  tokens were still bought.
- **Two cost states side by side.** `unknown` is not rendered as `$0.00`, and a `partial` would read "at
  least $X". That distinction is a stored column (DB11), never inferred from whether the number is null —
  and `steps_test.go` proves it with a `partial` and a `known` that have the *same* non-null amount.

**It failed if** `step verify` reports a mismatch after the restart. Anthropic rejects a tool-use turn whose
prior thinking was altered, and **S7 recovers a crashed step by replaying exactly these rows** — so a byte
lost here surfaces three slices later as a provider 400 with nothing pointing at the store.

---

## 6. One writer, many readers, no stalling

Terminal A — a writer, hard:

```
./aeon debug hammer --writes 3000
```

Terminal B, **while that runs**:

```
./aeon db info
./aeon debug hammer --writes 50      # a SECOND writing process
./aeon db query awaiting
```

**You should see** every read answer promptly while the writes continue, and the second writer finish too.
Real numbers from the walk:

```
3000 writes in 87ms (34309/s) · worst single write 1.168ms
seq is now 3037

… and the concurrent second process:
seq is now 3087

three `db info` reads during both:  0.00s each
```

**It failed if** a read blocks for seconds, or if anything reports `SQLITE_BUSY`.

WAL allows one writer and many readers, and DB15 arranges that with a single-connection writer pool — so a
second writer **in the same process** waits at the pool, predictably, rather than at SQLite. Between
PROCESSES, `busy_timeout` and `_txlock=immediate` do the work: the writer takes its lock at BEGIN, because
`busy_timeout` does **not** cover a deferred transaction upgrading to a write.

---

## 7. There is no `awaiting_user` column

Anyone arriving from v1 will look for one. There isn't one, deliberately.

```
sqlite3 state/aeon/state.db "PRAGMA table_info(loop);" | grep -i awaiting
```

**You should see** nothing.

Instead:

```
./aeon db query awaiting
```

**You should see the QUERY as well as the answer:**

```
SELECT 1 FROM question
WHERE project_root = ? AND loop_slug = ? AND answered_at IS NULL LIMIT 1
→ no loops awaiting an answer

It is DERIVED, not stored: a flag and an unanswered question can disagree, and
when they do nothing says which is right (DB6).
```

**Why it matters:** a stored flag and an unanswered question can disagree, and when they do nothing tells you
which is right. Deriving the answer makes the disagreement **unrepresentable** (DB6). The SQL printed is the
store's own published constant — so what you are shown is what ran.

---

## 8. A prior version upgrades, and a broken one refuses

```
./aeon db upgrade --dry-run
```

```
no pending migrations — /tmp/aeon-s4-demo/state/aeon/state.db is up to date
  (opening the store applies them, so this is the state AFTER that)
```

**There is no apply command, deliberately.** Migrations run on **every open** (DB8), so an apply verb would
be a second way to do what starting the daemon already does — and the second way is the one that gets run
against the wrong file.

Then the case only a real upgrade exercises: check out the commit that landed the schema, create a store,
seed it, come back, and open it. `docs/test_suites/aeon-store.md`'s **QAS-30** is that procedure, and it is
marked required before every release because it is the only case that exercises a database this code did not
create.

**You should see** the migrations apply and **every seeded row survive**. If a migration fails partway, the
open must **fail loudly** with the schema still at the prior version — measured: the failing migration's
transaction rolls back, the run stops, the ledger records `failed`, and the open reports it.

---

## 9. The same thing, in the app

```
make dev-ui
```

Open the **Store** tab. You should see the path (copyable), `foreign_keys` as a pill, the pool shape, the
statically linked extension version, the ledger, the derived awaiting query **as SQL**, and a document-name
box. Type `../../etc/passwd` into it and watch the refusal arrive with all three parts, where you will
actually be reading it.

It is a **test bench**, and the app says so. S9 builds the real app; this exists so each slice is exercised
where it will be consumed rather than only from a terminal (AGENTS §4b).

---

## §Findings — what the walk corrected

| # | Step | Predicted | Actual | Which was wrong |
| --- | --- | --- | --- | --- |
| 1 | 1 | `(WAL, 48 KiB)` | `wal, 4 KiB` on a store with 18 tables and 3,000 events | **Code.** In WAL mode the data was in `state.db-wal`. One number reads as a broken store, so `Info` now reports `WALBytes` separately — named rather than summed, because where the bytes are is what somebody copying the file needs to know |
| 2 | 5 | `1 thinking block (signed)` | `the saved thinking block is UNSIGNED` | **Demo.** S3's streaming surface carries no signature, so `--save-step` cannot save one. The code now says so at the point of saving, and `step verify` says "no thinking signature in this transcript" rather than printing nothing |
| 3 | 5 | `aeon step show 4f2a1c` | `aeon step show 2` | **Demo.** Step ids are `AUTOINCREMENT` integers — which is what makes them never reused, which is what makes an id unambiguous |
| 4 | 5 | `--route anthropic-sonnet-thinking` | `--route anthropic`, plus `--loop` and `--project` | **Demo.** Route ids come from the environment (S3); a saved step needs a scope, and half a scope is refused rather than filed somewhere arbitrary |
| 5 | 1 | a four-line `db info` with a `schema 6 migrations` line | the layout above, with the pool shape and the pragmas | **Demo.** The real output adds what DB15 and DB16 decided, because those are the two things that silently go wrong |
| 6 | 4 | `subscribed s-2b81 · from seq 1425` | `watching s-8642fb40 · kinds=* project=* · from seq 30` | **Demo.** S2's own wording; the filter is echoed because an unexpected filter is the commonest reason a watch looks silent |
| 7 | 7 | the answer only | the answer **and** the explanation, every time | **Code.** The explanation was printed only when loops *were* awaiting, and it is needed most in the common case of "none" |
| 8 | 6 | `aeon debug hammer --writes 5000` | works; `--report-every` added | **Neither** — the flag existed as predicted. 5000 is reduced to 3000 in the text because 87ms makes the point |
| 9 | 2 | `cd ~/repo-a && aeon loop create --slug refactor` | works, `--project` defaults to the working directory | **Neither.** Kept, with `--project` shown explicitly so the demo can be pasted from one directory |
| 10 | 9 | the Store tab renders | **`TypeError: null is not an object (evaluating 'info.pending.length')`, and a blank tab** | **Code**, found by the user after the walk. Go marshals a nil slice as `null` and `pending` is empty on every healthy store, so it was `null` every time. The conformance fixture said `[]` — a shape nothing produced — which is why no test caught it. Fixed at the handler (the last gate before the wire), guarded by a test that marshals real handler output, and every tab now sits in a `TabBoundary` that shows the error instead of blanking |

---

## What this demo does **not** show

- **No step runs on its own.** `--save-step` persists what `ask` did; the runtime that assembles context and
  loops over tools is **S5**.
- **No suspension, no questions asked.** The `question` table exists and is empty. **S6** fills it — the
  store's `Ask`/`Answer`/`PendingQuestion` are built and tested, and nothing calls them yet.
- **No crash recovery.** `status='running'` steps are readable (`StepsByStatus`), and the thing that reads
  them on startup is **S7**.
- **No vector index.** `sqlite-vec` is present and unused. **S22** adds the tables, and needs no load step.
- **No policy evaluation.** The three policy tables exist with their CHECK constraints and nothing reads
  them. **S10** brings the evaluator; the shape is here because a table is free now and a migration on a
  live store later.
- **No renderer.** There is no `loop.md` on disk. The store is the source of truth; the mirror is retired
  (S16's L25).

The skateboard is S5, and it is one slice away: give this a runtime and two tools.

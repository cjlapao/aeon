# Design — S24 Loop Queue

**Brief:** [`brief.md`](brief.md) — read it first; this document answers the brief's §7 questions and
argues the shape, it does not restate the contract. **Plan:** [`plan.md`](plan.md). **Demo:**
[`DEMO.md`](DEMO.md).

**Written 2026-10-26, against the real codebase.** The brief (written earlier) flagged five open questions
as "needs reading from the runtime code, not guessed here." S11, S12 and S13 are now built — the vocabulary
and mechanisms this design leans on are measured, not assumed, and every claim below cites the file it came
from.

---

## 1. Context

A project's loops today start one at a time, by a person typing `loop.start`. This slice adds a table, two
project settings and a reactor that watches `loop.finished`/`loop.blocked` and decides whether to start the
next queued item — through the *same* `loop.start` path a person uses, never a second one (§4 of the
brief).

The brief's own Q-decision (§5) is the one piece that most needed outside confirmation: moving G14's
"declared, never inferred" attendedness check *earlier* (to add-time) rather than relaxing it. Nothing in
this design revisits that; it is treated as settled. What follows resolves the five questions the brief left
to this document, plus two more this investigation surfaced that the brief could not have anticipated.

## 2. What the runtime actually does — the measurements this design rests on

**Loop status is free `TEXT`, no `CHECK`.** `schema.go`'s `loop` table (`status TEXT NOT NULL`) has never
had a CHECK on it, in 25 migrations. The real vocabulary, read from every assignment site in
`internal/loop/*.go` (not test files): `running`, `blocked`, `waiting`, `done`. Nothing else. In particular,
**`failed` and `cancelled` do not exist as loop statuses** — there is no first-class "this loop is
permanently done, badly" signal.

**`loop.finished` is emitted exactly four times in the codebase, every one with `status: "done"`**
(`continue.go:105`, `advance.go:103,168,273`). There is no `loop.finished` for anything else. A loop that
cannot proceed does not finish — it sits at `blocked` or `waiting` indefinitely, unless a person acts on it.

**`blocked` is overloaded — it is not one thing.** `internal/loop/counters.go` defines `LoopCeiling = 10`
(a bound on `total_failed_gates`, Q11 in that file's own comment) and `LoopCeilingOutcome = "block"`:
reaching it is the engine's answer to "has the loop stopped converging," and the result is `st.Status =
"blocked"` with `BlockedReason{Kind: "unresolvable-reject", ...}` (`advance.go:260-262`). But the *same*
status, `blocked`, is also what a loop gets when it is waiting for a missing handler to register
(`retry.go`'s `RetryRefusal{Kind: "not-missing-handler", ...}` exists specifically because *that* kind of
block is retriable once the handler shows up) or a model error (`BlockedReason{Kind: "model-error", ...}`,
`advance.go:195`). **Reading `status == "blocked"` alone cannot tell "this will resolve itself" from "this
is exhausted and needs a person."**

**A `question` row requires an existing loop.** `schema.go`: `question.loop_slug TEXT NOT NULL`, with
`FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE`. A gate
question cannot exist without a loop row to attach to. For a project that already has a prior loop, this is
no obstacle — the just-finished loop's row still exists (until S17 archives it) and can hold the "advance?"
question. **For a project's very first queue item, with zero prior loops, there is nothing to attach a gate
question to.** This is a structural fact, not a corner this design can design around by trying harder.

**`loop.start`'s real shape** (`internal/protocol/blueprint_loop.go`, `LoopStartParams`): `blueprint`,
`goal`, `workspaceRoot`, `route`, `mode`, `slug`, `tech`, `with` (a `map[string]string`), `attended`
(`*bool`). No separate "context" or "files" field exists at this layer — `goal` is free text and `with` is
the durable per-loop tag channel S14/S16 already use.

## 3. Decisions

### CG1 (Q1) — the queue item status vocabulary, closed at seven values

```
pending → sizing → ready → running → done
   │         │                  │
   └─────────┴──────────────────┴──→ cancelled
                                  └─→ blocked
```

`pending` (just appended, not yet sized) · `sizing` (a sizing run is in flight) · `ready` (sized, or
`sizingMode: manual` so nothing to wait for — eligible to materialize) · `running` (a loop exists and is
in flight) · `done` (the loop reached `done`) · `blocked` (the loop reached a `blocked` status — see CG2) ·
`cancelled` (removed, at any point before `done`/`blocked`, per D12).

**The brief's own candidate list included `skipped` as an eighth value; this design drops it.** `D12` asks
for exactly one behaviour — a pending item can be taken out of the queue — and `skipped` vs `cancelled`
would be two labels for that one behaviour with no decision anywhere that reads one and not the other.
One status, not two, until something actually needs to tell them apart.

Transitions: `pending → sizing` (skill mode, automatic on append) · `pending → ready` (manual mode,
immediate) · `sizing → ready` (size reported) · `sizing → pending` (the sizing run itself failed — it is an
ordinary step and ordinary steps fail; CG5 says why this does not recurse) · `ready → running` (materialized
and started — CG4 says when) · `running → done` · `running → blocked` · `{pending, ready} → cancelled`.
`running` never transitions to `cancelled` — a loop in flight is not queue history, it is the loop's own
record (D11); cancelling the *queue item* once it is running has no defined meaning and is refused.

### CG2 (Q2) — "terminal," for advance purposes, is `done` XOR `blocked` — and `blocked` is never diagnosed further

Given §2's finding that `blocked` conflates retriable and exhausted loops, and that telling them apart from
outside `internal/loop` would mean duplicating its own `BlockedReason.Kind` taxonomy (a second place that
knows what each kind means, which drifts the day a kind is added there and not here) — **this design does
not try.** Any loop reaching `blocked` is terminal for the *queue's* purposes, full stop. The queue item's
status becomes `blocked`, advancing halts, and a pending question is raised (CG4) regardless of
`queueAdvance`'s setting — an exhausted loop is not a "proceed automatically" decision for anyone.

A person who believes the block is actually retriable can still use `internal/loop`'s own existing retry
path directly on the underlying loop — S24 neither blocks that nor automates it (D11: the queue is never the
source of truth for what a loop did). If the retry succeeds and the loop reaches `done`, the ordinary
`loop.finished` reaction fires and the queue advances normally. If the person instead cancels the queue item
(D12), the next item runs. **Nothing about this needs S24 to know which `BlockedReason.Kind` values are
retriable** — that knowledge stays exactly where it already lives.

### CG3 (Q3) — a queue item's materialization payload is `LoopStartParams`, verbatim

No new context shape. A queue item stores `blueprint`, `goal`, `workspaceRoot` (defaulting to the project's
root if absent — a queue is per-project, so this is almost always implicit), `tech`, `with`, and the
declared `attended` (D5). Materializing an item is calling `loop.start` with exactly these fields — the
brief's §4 "no second loop-creation path" made concrete. S8's blueprint inputs and S9's `contextManifest`
are the wrong layer entirely: both are runtime/step-internal state that exists only once a loop is already
pinned, not something a project author hands a not-yet-started item.

### CG4 — the cold-start gap (§2's finding): the first item in an empty queue needs an explicit kick

`queueAdvance` governs *reactive* advancement — what happens when a loop the daemon is already watching
reaches a terminal status. **It has nothing to react to for a project's first-ever queue item.** This design
does not invent a project-level question mechanism to paper over that (which would be a second kind of
"waiting for a person," the exact thing §4 rules out). Instead: **materializing the first item in an
otherwise-idle queue is always an explicit call** (`queue.advance` or the CLI equivalent), the same way
starting a project's very first loop today is always an explicit `loop.start` — nothing in Aeon spontaneously
starts the first unit of work in a project, and this slice does not become the exception (consistent with
A1's "nothing spawns" amendment). `queueAdvance: ask` has no extra meaning here: calling the verb at all
*is* the human decision.

Once a loop exists in the project (first item materialized, or any prior loop), every *subsequent* advance
is reactive: `queueAdvance: auto` starts the next item immediately on `loop.finished`; `queueAdvance: ask`
raises a `gate`-kind question attached to the just-finished loop's slug (§2 — its row still exists, so the
FK is satisfiable) and materializes the next item only once answered.

### CG5 (Q5) — a sizing run cannot recurse, by construction, not by a guard

A sizing run is dispatched as an ordinary loop/step (brief §5, "to an agent, not a bespoke execution path"),
assigned a handler the way S13's registry assigns any other step. **It is never itself appended to the
queue as a `queue_item` row.** Recursion would require a sizing run to be sizeable, which would require it
to exist as a queue item in `sizing` mode — and nothing in this design ever inserts one. The exemption is
structural (there is no code path that could produce it), not a runtime check that could be bypassed or
forgotten.

### CG6 (Q4) — failure stops the queue; CG2 is the mechanism, this is the restatement for clarity

Already covered by CG2: a `blocked` terminal status always halts advancement and always raises a question,
independent of `queueAdvance`. The brief called this "the safer default" without committing to it; this
design commits to it as the *only* behaviour, not a default a project can turn off — turning it off would
mean a project could configure itself to silently skip past an exhausted loop, which is the kind of quiet
widening D5/D6 exist to prevent one layer up.

### CG7 — two new `project` columns, not a settings table

`queueAdvance` (`'auto' | 'ask'`, default `'ask'` — R4) and `sizingMode` (`'manual' | 'skill'`, default
`'manual'`) are two columns on `project`, each with an inline `CHECK`, added by `ALTER TABLE ... ADD COLUMN
... CHECK (...)`. **Verified, not assumed**: this driver's SQLite (mattn/go-sqlite3, bundled, exercised at
3.50.4 locally) accepts an inline `CHECK` on an `ADD COLUMN` without the rebuild-and-copy dance migrations
`0016`/`0019`/`0024` needed for a table that already existed with rows depending on the old shape. A plain
`ALTER`, the same shape `0020` used for `step.pause_reason`, is sufficient here because `project` is not
being narrowed, only widened with two columns that have a safe default.

### CG8 — `queue_item` is a new table, not a widened `loop` or `task`

A queue item is not a loop — most of its life (`pending`/`sizing`/`ready`) it has no loop row at all (D10).
Columns: `id`, `project_root`, `position` (an integer the append/reorder/remove operations maintain — D1's
"ordered"), `status` (closed, CG1's seven values, `CHECK`), `blueprint`, `goal`, `workspace_root`, `tech`,
`with_json` (the `with` map, JSON-encoded — no existing table stores a map as columns, and `with` is
already JSON-shaped at the protocol layer), `attended` (nullable `INTEGER` as 0/1 — three states exactly
like `loop.attended`, D5/D6), `size_min`, `size_max`, `size_basis` (nullable until sized — D8's shape, never
a bare number), `loop_slug` (nullable FK to `loop`, set only once materialized — D10's "no loop status to
read before one exists" made literal: the column is absent, not a sentinel), `created_at`, `updated_at`.

### CG9 — authoring the queue is socket-writable, unlike policy

**This is a deliberate departure from PL15** ("policy authoring is CLI-only"), named so nobody reads CG9 and
assumes it is an oversight. PL15 exists because policy *is* the mechanism that constrains what the socket's
callers may do — a write path on the endpoint would hand a caller the means to widen the guard written to
constrain it. A queue item is ordinary project content, the same trust tier as a loop's `goal` or a
blueprint selection, both of which are already writable through the socket (`loop.start` itself). Append,
reorder, remove and settings-change all go through `queue.*` socket methods as well as `aeon queue` CLI
verbs — there is exactly one implementation underneath both, the same `internal/queue` package, so "CLI vs
socket" is a transport choice, never two authoring paths.

## 4. Architecture

```
            append/reorder/remove/settings
                       │
         ┌─────────────┼──────────────┐
         ▼                             ▼
   aeon queue (CLI)              queue.* (socket)
         │                             │
         └──────────────┬──────────────┘
                         ▼
                 internal/queue
            (QueueItem, Status, Store ops,
             the one place that owns position
             and the seven-value transition table)
                         │
          ┌──────────────┼───────────────────┐
          ▼                                   ▼
  sizingMode: skill                   the reactor (daemon)
  → dispatch a step/loop,             subscribes loop.finished /
    report_size tool (S11-shaped,     loop.blocked, applies CG2/CG4,
    native, one-shot) → writes        calls loop.start on the next
    size_min/size_max/size_basis      ready item (or raises a gate
    → item: sizing → ready            question first, per queueAdvance)
                                                   │
                                                   ▼
                                           loop.start (existing,
                                           unchanged — D11, §4)
```

## 5. Testing this slice has to do

- **The seven-value transition table is exhaustive and closed** — a test drives every listed transition and
  asserts every unlisted (from, to) pair is refused, the same discipline `internal/store/machine.go`'s
  `AllowedTransitions()` already uses for task status.
- **D5/D6 as a property, not a count**: appending an item with no explicit `attended` and no project default
  set materializes a loop with `attended: false` — never `nil`, never inferred from who happens to be
  present when the daemon actually starts it.
- **CG2's "any `blocked` halts, regardless of `queueAdvance`"**: a project set to `auto` still does not
  materialize the next item after a `blocked` terminal status — the one test that would catch a shortcut
  that reads `queueAdvance` before checking which terminal status this was.
- **CG4's cold-start**: an empty queue plus an appended item does not materialize anything until the
  explicit call — the daemon does not poll, watch, or otherwise notice an appendable queue on its own
  (mirrors S22's N2, no watcher, for the same reason).
- **CG5 by absence**: there is no code path under `internal/queue` that inserts a row for a sizing run —
  asserted the way S11's `todo.go` asserts no package-level state: a grep/AST check, not a runtime guard
  that could silently start passing because nothing exercises it.
- **CG9's single implementation**: CLI and socket paths both resolve to the same `internal/queue` calls —
  a test instantiates both entry points against one store and asserts identical resulting rows.
- **The scaling/ordering proof for D1**: appending three items while the first is `running` never reorders
  the two behind it — position is assigned once, at append, and nothing renumbers an already-materialized
  prefix.

## 6. Amendments to earlier slices

**None.** S24 extends G14 (S11) exactly as the brief's Q-decision described — the pin mechanism itself is
untouched — and reuses S6's question shape, `loop.start`'s existing params, and S13's handler assignment
without asking any of them to change shape. The one genuinely new fact this design surfaces (`blocked`'s
overload, §2) is a property of `internal/loop` worth the next slice that reads loop status knowing about,
but it requires no change there — S24 works around it by never trying to sub-classify `blocked`, which is
the correct boundary regardless of whether a future slice later adds a real `failed` status upstream.

## 7. Open questions this design did not close

- **Exact sizing cost/time bound.** A sizing run is an ordinary step and inherits the project's normal
  cost-cap policy (R3) — but whether a *default* cap exists for sizing specifically, or every project must
  set one explicitly, is a CLI/default-config question for the task that builds `aeon queue settings`, not
  a schema question.
- **Reorder semantics while `sizing` is in flight.** D12 says a pending item can be reordered; whether an
  item mid-`sizing` counts as "pending" for this purpose (probably not — its sizing run is already
  dispatched against its current position-independent identity, so reordering it is harmless) is a one-line
  call the task building `aeon queue reorder` should make and record, not a modelling question.

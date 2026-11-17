# S40 Automation — the demo

**Stage 5 · Fleet.** *The sixth ruling of S38 §0, made runnable: an event starts the loop, and the person stays in charge.*

Everything a person could already do — approve a goal, decide a task, pause a trigger — stayed where it was.
What is new is the other direction: `goal.approved`, `task.created`, `task.approved`, `task.completed` land in a
transactional outbox and a dispatcher turns matching events into unattended loops, with a fire row, a reason
sentence and five honest outcomes for every decision, including the ones where it cannot say.

**Walked 2026-10-28** on a clean checkout: a real `aeon serve` (built from HEAD at T10 time), a real socket,
real `socat` lines against it, real `sqlite3` reads against the walk's own store, real daemon restarts, and
deletions run in a scratch worktree. Every quoted line below is **measured output** from those commands, not an
aspiration. What the prediction got wrong is in [§Findings](#findings) at the bottom — and a deviation is a
finding, not a footnote (AGENTS §4b).

**Setup the walk used** (the house rule — `XDG_STATE_HOME` at a throwaway, never the real store):

```
ROOT=$(git rev-parse --show-toplevel); cd "$ROOT/aeon"
export GOPRIVATE='github.com/cjlapao/*' CGO_ENABLED=1 CGO_CFLAGS="-I$ROOT/.tmp/cgo-shim"
export XDG_STATE_HOME=$(mktemp -d)                 # /tmp/tmp.pgneMc2Rcw and /tmp/tmp.eVqc8d4O9I below
go build -o /tmp/aeon-t10 ./cmd/aeon && /tmp/aeon-t10 serve > serve.log 2>&1 &
# serve.log prints the two paths that matter:
#   endpoint  /run/user/3002/aeon/rpc.sock
#   store     /tmp/tmp.pgneMc2Rcw/aeon/state.db
```

**The handshake, learned the loud way** (T0 of the docs, not of the code): the socket speaks **JSON-RPC 2.0 at
protocol version 2 only** — a `protocolVersion: 1` initialize is refused (`"protocol version not supported
… speaks 2 (oldest supported: 2)"`, −31002) — and every socket call needs an `initialize` **on the same
connection first**; two lines burst into `socat` at once race the dispatcher and the second answers
−31001 `before initialize`, so the walk paces them (`sleep 1` between lines). The walk's door calls:

```
call() { { printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientName":"t10-demo","clientVersion":"0.1.0","protocolVersion":2}}\n'
             sleep 0.6
             printf '{"jsonrpc":"2.0","id":2,"method":"%s","params":%s}\n' "$1" "$2"
             sleep 0.8; } | socat -T6 STDIO,ignoreeof UNIX-CONNECT:"$SOCK"; }
```

The person-side doors the walk used are the CLI's (`project create`, `component` via `component.create` on the
socket — there is no CLI component verb, `goal create`, `goal decide`, `task create`) and the socket's
(`task.decide` — the S38 T15 person door for tasks; `automation.trigger.create`). The agent-kind actor is
refused on every trigger write (§Walk 7) — the walk's lines are all the human hand, uid 3002.

---

## Walk 1 — goal approved → `dev-loop`, unattended

Create the component (`component.create` over the socket — name, slug and root are all required), then the
trigger, then approve a goal through S38's surface:

```
call automation.trigger.create '{"name":"W1 intake","projectId":"$PRJ","componentId":"$CID",
  "match":{"event":"goal.approved","goal_type":["feature"]},"blueprint":"dev-loop",
  "kickoff":"Approved: {{.Goal}}","maxRunsPerHour":10}'
aeon goal create --project "$PRJ" --component "$CID" --title "Automate the intake" --type feature
aeon goal decide "$GID" --version 1 --decision approved
```

You should see: a fire row `started`; a loop with `attended=0`, `automation_depth=1`, started by the event; a
kickoff message with `source = automation`; and the loop then blocked by S11 where it wants to go outward.
Actual (`sqlite3` against the walk's store, four seconds after the decide — the dispatcher's `PollEvery` is
1 s):

```
--- fire row:
1|trg-e60e893f0ced86ace67e627e5a2da07f|started|||auto-trge60e893f0ced86ace67e627e5a2da07f-1|0
--- loop row (slug | attended | started_by | depth | status):
auto-trge60e893f0ced86ace67e627e5a2da07f-1|0|event|1|blocked
--- kickoff message (seq | author | kind | source | source_ref | body):
1|system|note|automation|trg-e60e893f0ced86ace67e627e5a2da07f|Approved: Automate the intake
```

The kickoff rode the pin's one transaction (`author=system`, `source=automation`, `source_ref=<trigger id>`,
body rendered from the trigger's template with the real goal title — `KickoffData.Goal`, S39's renderer).
`attended=0` and `automation_depth=1` are AU2 and AU5 as stored facts. `started_by=event` is the value 0039
added for this door — **not** `trigger`, which S20's schedule path keeps; the task text said `trigger`
(Finding 2).

**The first outward step, refused.** The scratch store has no model routes configured, and that is where this
walk's truth lives: the loop blocked **before any step row existed** (`SELECT … FROM step` returned nothing,
`policy_event` empty) — the runner needs a route for the plan agent, and an unroutable loop blocks at the first
agent dispatch. So the unattended-outward denial was **not observable in this environment**; its sentence is
quoted from the source that emits it (`internal/action/class.go`, `VerdictDeny` — the runner blocks with it
verbatim, `internal/loop/execute.go` "blocked AT ONCE … VERBATIM"):

```
`<step-name> denied — effect class 'outward' resolves to deny (loop is unattended)`
```

and it is pinned by name by `TestUnattendedOutwardDeniesVerbatim` (`internal/loop/execute_test.go:55`).
Finding 3 records the gap; the walk did not fake it.

**The door that refuses without a component** is worth its own lines, because the walk hit it first: the same
trigger created with `"componentId":""` against a goal that names no component answered, fire row and all:

```
1|trg-4497dd4091c2d16bfa23ad5f94dee20c|refused|no-target-component: the event names no single component; set component on the trigger|||0
```

A dispatcher that guessed a workspace is the one thing AU10 forbids; the refusal names the fix (Finding 1).

## Walk 2 — a manual `bug` task → the bug blueprint; `feature` does not

```
call automation.trigger.create '{"name":"W2 bugfix","projectId":"$PRJ",
  "match":{"event":"task.approved","task_type":["bug"]},"blueprint":"two-step","maxRunsPerHour":10}'
aeon task create --component "$CID" --title "Crash on launch" --type bug --require-approval
call task.decide '{"id":"$TID","decision":"approved"}'
#   {"jsonrpc":"2.0","id":2,"result":{"changed":true,"state":"approved","decidedBy":"3002",…}}
aeon task create --component "$CID" --title "Polish the bench" --type feature --require-approval
call task.decide '{"id":"$FID","decision":"approved"}'          # the negative control
```

Actual — the bug fired, the feature did not:

```
2|trg-252118e8ef6ec8ca96ac121dd645c9a2|tsk-052a49797fcc3527dab26904f7560e74|started|
0                                                                # SELECT COUNT(*) … WHERE subject_id = feature task
```

and the loop carries the trigger's blueprint, unattended and one deep:
`auto-trg252118e8…-3|1|two-step` (slug | automation_depth | pinned blueprint). The feature row's silence is
the trigger-with-no-match case: **no fire row at all** — matching happens against the event's closed grammar,
and a non-match produces no row to explain, which is exactly what the fire table is for (Finding 9 keeps the
honest version of that sentence).

## Walk 3 — limits

`maxRunsPerHour: 1` and two chore tasks created seconds apart. Actual:

```
3|trg-41e5e73a02c36f1f815bcd9d1746ab6c|tsk-787c…|started||
4|trg-41e5e73a02c36f1f815bcd9d1746ab6c|tsk-76c3…|limited|max-runs-per-hour: 1 started since 2026-10-28T13:42:48Z, limit 1|
```

The limit sentence is the dispatcher's own `fmt.Sprintf` (window start = now − 1 h, stored as written), and the
count it quotes counts `started` **and open claims** — a crash between claim and decide cannot smuggle a second
start through the rate gate (the rate query is `outcome = 'started' OR outcome IS NULL`).

**Cost.** `maxCostPerDayUsd: 5` on a trigger with the same store: the fire came back `started` with
`cost_note=''`. That is the truth of a provider-less store: `EventTriggerSpend` had no steps to aggregate, so
the cap was **checkable** (known spend $0.00), and `cost-unknown` is what the dispatcher writes only when a
step's cost is genuinely unknown (`sp.Unknown`) — which needs real model steps. The branch is pinned by the
dispatcher test that asserts `fires[0].CostNote == "cost-unknown"` (`internal/automation/dispatcher_test.go:755`).
Finding 4.

**The five pills.** What the page's log header reads is one query, and the zero is a zero:

```
limited|1
started|4
```

— and after Walks 4–6, on the same store, the same GROUP BY answers
`could-not-say|1  limited|1  refused|1  started|4` with `skipped` absent, i.e. **zero shown as zero**. The
Automation page (T9) renders exactly these five pills (`started`, `refused`, `limited`, `skipped`,
`could-not-say`) with the counts, and its vitest suite (`aeon/ui/test/automation.test.tsx`, 12 tests, all
green at T9's record) asserts the zero-as-zero state, the distinct tones and the `could-not-say` gloss
("we cannot prove whether this started"). **The page was verified as written-by-a-bench-page through
vitest only — the Tauri shell was never run, so no claim is made here about desktop appearance.**

## Walk 4 — crash between claim and decide

The plan asked for `kill -9` between claim and start via the `AEON_DEBUG` hook. Verified: the only crash hook
in `debug_crash_aeondebug.go` is `AEON_DEBUG_CRASH_DURING_RECOVERY` (S31's loop-recovery crash, for loop
recovery's own test) — there is **no** claim-window hook. So the walk used the task's own fallback: re-open the
crash's footprint with `sqlite3` — fire 3 (already `started`) back to an open claim, exactly as a `kill -9`
after the claim commit and before the decide would leave it:

```
sqlite3 … "UPDATE event_trigger_fire SET outcome=NULL, decided_at=NULL, loop_id=NULL, loop_slug=NULL WHERE id=3;"
/tmp/aeon-t10 serve            # the restart after the crash
```

You should see: Reconcile (which runs before the first poll, AU13) complete the record from the loop that
exists — and never a second loop. Actual:

```
3|started|auto-trg41e5e73a02c36f1f815bcd9d1746ab6c-6|2026-10-28T14:52:18.048Z     # re-decided, decided_at fresh
4                                                                                  # SELECT COUNT(*) FROM loop
4                                                                                  # … WHERE slug LIKE 'auto-trg%' — never twice
```

Row 1 of Reconcile: the loop already exists (found by `AutoSlug(trigger, outbox)`), so the Starter is **never
called again** — a starter that would now fail cannot undo an honest commit, and the count lines prove the
loop was not started twice.

## Walk 5 — trigger edited while the daemon was down → `could-not-say`

The edit itself is a socket write, so "while the daemon was down" is the operator editing the row and
restarting: an open claim whose trigger's `updated_at` moved past `claimed_at` — the definition that justified
the claim is no longer the definition on file.

```
sqlite3 … "INSERT INTO event_trigger_fire (trigger_id, outbox_id, subject_kind, subject_id, depth, claimed_at)
           VALUES ('$TRG3', 3, 'task', 'tsk-feature-approved', 0, '2026-10-28T14:00:00.000Z');"
sqlite3 … "UPDATE event_trigger SET updated_at='2026-10-28T14:00:01.000Z' WHERE id='$TRG3';"
/tmp/aeon-t10 serve
```

Actual — Reconcile row 3, AU12's honest third outcome:

```
6|could-not-say|trigger-edited-since-claim: the definition that justified this claim changed
```

Answering `started` would vouch for a match nobody wrote. (The sibling arms walked themselves into the code
review this walk re-read: row 2 `disabled` → `skipped`, row 5 nameless vanishings → `refused` naming both
names.)

## Walk 6 — the depth chain stops at 4

AU5's ceiling is `store.MaxAutomationDepth = 3` on the **event**; +1 is the depth the loop the fire starts
would carry — so the deepest loop is 4 and a fifth is never written. A depth-4 event was seeded directly (the
honest simulation of "an agent's loop, four deep, approved something") against a matching trigger:

```
INSERT INTO automation_event (event, subject_kind, subject_id, project_id, goal_type, depth, caused_by_trigger, created_at)
  VALUES ('goal.approved','goal','goal-deep','prj-…','investigation',4,'trg-deep-origin','…');
```

Actual:

```
7|trg-5a6cf5a7…|4|refused|depth-exceeded: event depth 4 > 3 (caused by trigger "trg-deep-origin")
4                                       # loop count unchanged — a refused depth writes no loop
```

The refusal lands **before** the target gate (a loop that will never exist needs no workspace) and names the
trigger that started the chain — AU5's full sentence: who, how deep, and where it came from.

## Walk 7 — human-only

The four trigger writes have no agent half: the socket's peer-credential session is the actor, an agent-kind
actor is refused at the store even if a handler regressed, and nothing outside the daemon's writer file may
call the write functions. Measured output of the two guards by name:

```
$ go test ./internal/daemon -run TestTheStoreStillRefusesAnAgentActorIfTheHandlerRegresses -v -count=1
=== RUN   TestTheStoreStillRefusesAnAgentActorIfTheHandlerRegresses
--- PASS: TestTheStoreStillRefusesAnAgentActorIfTheHandlerRegresses (0.17s)
ok  	github.com/cjlapao/aeon/internal/daemon

$ go test ./internal/daemon -run TestOnlyTheDaemonWriterCallsTheStoreTriggerWrites -v -count=1
--- PASS: TestOnlyTheDaemonWriterCallsTheStoreTriggerWrites (0.07s)
ok  	github.com/cjlapao/aeon/internal/daemon
```

The second is the grep test the slice keeps for its whole life (T8): it walks the whole `internal/` tree and
fails any call to `s.CreateEventTrigger` / `s.UpdateEventTrigger` / `s.SetEventTriggerEnabled` outside
`daemon/triggers.go` — the `\bs\.` boundary means a method named `d.CreateEventTrigger` cannot hide in plain
text. Its sibling `TestCreateTriggerRefusesWithTheGrammarsOwnSentence` + the conformance fixture
`trigger-create-bad-match.json` pin the −32602 refusal a malformed match earns.

## The zero-trigger daemon (the check that starts nothing)

A fresh store, no triggers at all, a goal decided and a manual task created through the S38 surface:

```
automation_event|2          # goal.approved + task.created — the outbox rides the domain transactions
event_trigger_fire|0        # the dispatcher matched against nothing
loop|0                      # and started nothing
dispatcher|2|2026-10-28T14:24:28.402Z      # the cursor advanced past BOTH rows — the frontier moves with no triggers
```

Outbox weight, R7's question, measured on that store: 2 rows carrying **274 bytes** of stored text
(≈137 B/event; the goal row alone was 127 B at 1 row), against a 4 KB database file. A year of 100 events a
day is tens of megabytes of rows the cursor makes cheap to prune — the R7 answer, in bytes.

## The deletion drill (what S40 costs when it is gone)

In a scratch **worktree** (the user's working tree untouched; branch `t10-deletion-drill`, commit `d5a3620f`,
**never merged**): `internal/automation/` (7 files), `daemon/{automation,event_fire,start_loop,triggers}.go`
(+ their tests), `protocol/triggers*.go`, the ten `conformance/automation` fixtures, the tools pin test, the
loop pin-check test, `AutomationPage.tsx`, `automation.test.tsx`, and the six wiring lines
(`serve.go` goroutine + join, `daemon.go` seams, `method.go` consts/registrations/table fields, the two
protocol test files' cases, the closure in `blueprint.go` reverted to its inline body, and the three UI
edits). Result:

```
GO BUILD: OK
GO VET: OK — incl. all test files
```

Nothing in core code imports `internal/automation` except the daemon's deleted files; the tables and the
`loop` columns stay (0039 is forward-only), and outbox rows accumulate harmlessly with nothing reading them.
Two things the drill **did** surface are Findings 10 and 11.

---

## Findings

| # | what was done | expected | observed | verdict |
| --- | --- | --- | --- | --- |
| 1 | trigger with empty `componentId`, goal with no component | fire or a named refusal | `refused` — `no-target-component: the event names no single component; set component on the trigger` | **pass** — AU10's target gate refusing rather than guessing; the plan's Walk-1 trigger needed the component set |
| 2 | read the event-started loop's provenance | plan text said `started_by=trigger` | `started_by=event` — the value 0039 added for this exact door (T0-Q2); `trigger` remains S20's schedule value | **finding** — the task text is stale; the code and migration are right; corrected in this document instead of the shipped migration |
| 3 | watched for the first outward step's S11 denial | the denial sentence on a step | loop `blocked` with **no step rows** (no model route in the scratch store); sentence exists in `action/class.go`, blocked verbatim by `execute.go` | **finding + follow-up** — the unattended-outward walk needs a routable model; `TestUnattendedOutwardDeniesVerbatim` holds the line meanwhile |
| 4 | `maxCostPerDayUsd` fire in a provider-less store | `cost-unknown` on the fire | `cost_note=''` (spend was known-zero, cap checkable) | **finding** — `cost-unknown` requires an unknown-cost step; pinned by `dispatcher_test.go:755`; live walk of it wants routes (same follow-up as 3) |
| 5 | `trigger.create` with `"task_type":["costy"]` | `−32602` invalid params (as every other grammar refusal) | `−32603` — `match: task_type "costy" is not one of bug, feature, chore, refactor, investigation (OL14's five)` | **finding + follow-up** — the sentence is right but arrives **untyped** (internal error) at the socket while the `event` refusal arrives typed −32602; the classification belongs on a fix commit of T7's surface, not here (T10 Do-Not) |
| 6 | re-opened claim + daemon restart (crash simulation) | `started` again, loop never twice | `started`, `decided_at` fresh, `loop` count 4→4 (auto-slugs 4→4) | **pass** — Reconcile row 1 completes the record without calling the Starter |
| 7 | open claim + trigger `updated_at` past `claimed_at` | `could-not-say` | `could-not-say` — `trigger-edited-since-claim: the definition that justified this claim changed` | **pass** — AU12's third outcome as a stored fact |
| 8 | seeded depth-4 event + matching trigger | `refused(depth-exceeded)`, no loop | `depth-exceeded: event depth 4 > 3 (caused by trigger "trg-deep-origin")`, loop count unchanged | **pass** — and it refuses before the target gate, so depth 4 is the ceiling |
| 9 | `feature` task against the `bug` trigger | no fire row | `COUNT(*) = 0` — no row at all | **pass** — the no-match case is row-less by design; the fire table records decisions, not near-misses |
| 10 | deleted `daemon/start_loop.go` | tree builds | it did **not** — `blueprint.go` calls the extracted `startLoopWith`; the plan foresaw this and named the revert-to-inline-body step | **pass with note** — the extraction is S40's only shared core surface; removing the slice re-inlines 80 lines (drill executed the revert, then built green) |
| 11 | same drill, HEAD alone in a clean worktree | HEAD compiles | `daemon.go:448` calls `notifyHintFor`/`PublishNotify` defined only in the **uncommitted** working-tree `lifecycle.go` (the user's S41-notify half); the drill borrowed the working copy to proceed | **finding (foreign)** — a half-committed notify surface; recorded honestly, not ours to fix |
| 12 | `aeon goal submit` after `goal create` | submit a draft to review | `refused: goal … is in_review, not a draft — only drafts submit` | **finding** — `goal create` arrives in `in_review` on this surface; the walk's door is `goal decide` alone; the task text implied a submit step |
| 13 | first socket lines, un-paced and version-1 | initialize, then call | −31002 `protocol version not supported … speaks 2` and −31001 `before initialize` on the burst | **finding** — the handshake facts (protocolVersion 2, one initialize per connection, pace the lines) belong in any future socket doc; the walk's `call()` above is the working form |
| 14 | zero-trigger daemon, R7 byte question | rows exist, nothing fires, cursor moves | counts `2|0|0`, cursor at row 2, 274 B/2 rows ≈ 137 B/event | **pass** — R7's number is now a measured number |

**Suite-wide numbers measured for this record** (all from commands run in T10, none recalled): `make ci`
exit 2 — Go side fully green (38/38 `ok`, the suite at **2912 PASS / 0 FAIL** in T8's official run, 2906 of
them without the foreign notify family), the failure is the **foreign ui vitest tree only**
(`port.test` "ui-kit release", `app-icon` "icon registry", three `product` "S9 product surfaces") — the same
foreign-red pattern S38's T17 recorded, named and left alone. `internal/automation` package wall, this
task: **8.38 s real** (`go test ./internal/automation -count=1`, 33 tests). Reconcile over 1,000 open
claims: **1.9 s** — T6's measurement, quoted from its record because the walk re-ran nothing that touches it.
The per-package before/after counts are in this slice's STATUS narrative and T10's Notes.

# S38 · The organization layers — the walk

Walked 2026-10-27 on a throwaway state dir
(`XDG_STATE_HOME="$(mktemp -d)"`, `LOCALRPC_ENDPOINT_PATH` pinned under it),
against the binary built at the T16 commit (`19ac3530` (T16), the T15 commit being `d3de91d1`) by
`/tmp/aeon-walk/walk.sh`. Every output below is copied from that run — none
typed by hand. The replay-model lane (b, c) is quoted from the model-free
replay tests running against the same tree (no cloud keys on this machine —
named again in "what was NOT verified"). The walk's own findings table is at
the end (F1–F8).

The machine's own preamble (once per box):

```
mkdir -p ../.tmp/cgo-shim && cp "$(go env GOMODCACHE)/github.com/mattn/go-sqlite3@v1.14.52/sqlite3-binding.h" ../.tmp/cgo-shim/sqlite3.h
export GOPRIVATE='github.com/cjlapao/*' CGO_ENABLED=1 CGO_CFLAGS="-I$ROOT/.tmp/cgo-shim"
```

## 0 — the socket comes up

```
$ aeon ping
pong · 0.1ms
```

## 1 — the group tier (T4's verbs, walked here as the goal's host)

```
$ aeon project create --name Aeon Org
prj-656ff13a8c7dcadd83e86cbfe67bf739 Aeon Org   created

$ aeon project list
prj-656ff13a8c7dcadd83e86cbfe67bf739 Aeon Org  [        ]
```

## 2 — registering the component (the door with NO CLI verb — finding F1)

`aeon loop create` runs a loop in a directory; it does NOT register a
component, and no CLI verb registers one — components arrive through the
socket's `component.create` (the app's door). The walk registers one through
the daemon's own store file (the PL15-sanctioned second access path, WAL
serialised):

```
registered component 5e7751508be1fe693e581cf6c28f29dd at /tmp/tmp.y1knut5BJs/repo in group prj-…
```

An unregistered root is refused BY NAME, which is the right answer (T16
Notes): `refused: component "/tmp/…/repo" is not one this daemon has —
aeon loop list --all shows the roots`.

## 3 — (b) `brainstorm` replay: a goal with BOTH files pinned

The capture lane replays on the fixture model — the step files two workspace
documents and calls `goal_create`; the records arms pin a document from bytes
that EXIST on the loop's disk (T14):

```
--- PASS: TestBrainstormReplayCreatesGoalAndAttachments (0.21s)
--- PASS: TestBrainstormWithoutGoalCreateRejects (0.21s)
```

The second line is the criterion: files alone do not satisfy the contract —
no `goal_create`, no lane (T12's enforcement, walked at T12, still green).

## 4 — the person's goal: filed by hand, judged by hand

```
$ aeon goal create --project Aeon Org --title "Org layers for every work item" --type feature --body-file …/goal-body.md
goal-0c59405037a97f2db0e9ce9a7f2cc9e8 v1  in_review  Org layers for every work item
filed for a person's judgement — `aeon goal decide` is that door

$ aeon goal list
goal-0c59405037a97f2db0e9ce9a7f2cc9e8 v1  in_review  feature      Org layers for every work item
```

A hand-filed goal is born `in_review` — there is no path by which a person's
own goal auto-approves (P35).

## 5 — (c) `goal-to-brief`: the brief lane refuses the unapproved

```
--- PASS: TestGoalToBriefRefusedWithoutAnApprovedGoal (0.20s)
```

and once approved, the gate ANSWER decides — claimed-FROM, after the answer
is durable, joining to the decision methods ONLY (T14):

```
--- PASS: TestJoinApproveAndAbortDecideTheBrief (0.37s)
--- PASS: TestDecideFromGateIsIdempotent (0.19s)
--- PASS: TestJoinDecidesThePinnedGoalVersion (0.18s)
```

## 6 — (d) the engineered ladder beside the board — `aeon task list`

The ladder rows (an engineered loop seeded with the machine's own statuses —
F3 records that `done` is not one of them; the terminal state is `complete`)
and the person's hand-made rung, side by side, cost with its honesty (P32):

```
$ aeon task list
tsk-3456c45b62673fd6db285239a9e6ff43 engine     pending               agent:golang-engineer    walk the demo
tsk-03b49b02b720a8c90ec6a08ec8a40613 engine     in-progress           agent:golang-engineer    wire the board  $0.1000≥
tsk-080aad5a14f0dc7c1be3abfc6d24436c engine     complete              agent:golang-engineer    shape the store  $0.4200
tsk-96555d55c3ef69ebee14747a338c297f manual     pending      chore    user:ana                 Write the board's help text [pending]
```

The `$0.1000≥` is the floor: that step's model was unpriced, and the board
says so rather than calling a bound a bill. The filters answer (e):

```
$ aeon task list --manual false      # engine rows only — the manual row cannot leak
$ aeon task list --kind user         # the person's half of the board
```

(the planner's cartesian itself predates S38 (S8) — untouched by the slice
and its suite is unchanged and green in the full measurement).

## 7 — (e,f) the board's event, watched

```
$ aeon watch --kinds task.changed        # running; then a task create in another shell
watching s-275f00d6 · kinds=task.changed component=* · from seq 42
  Ctrl-C to stop

    44  task.changed           componentId=5e7751508be1fe693e581cf6c28f29dd id=tsk-3385cf964148a83c01b1c97f4be1e6e4 manual=true status=pending
```

`task.changed` fires after commit — a watcher never learns of a row a
rollback then removed (T15's rule, walked on the real socket).

## 8 — (f) a person's decision, stamped by the KERNEL's uid

```
$ aeon goal decide goal-0c5940… --version 1 --decision approved --reason "the frame is right"
decided: approved

$ aeon goal show goal-0c5940…
goal-0c59405037a97f2db0e9ce9a7f2cc9e8  v1  [approved]  feature
  Org layers for every work item
  A goal is a wish until a person judges it.
  execution: not_started
  ledger:
    v1  approved  user/3002  the frame is right

$ aeon goal decide goal-0c5940… --version 1 --decision rejected
already decided: approved by user/3002 at 2026-10-27T21:29:39.765Z — this command changed nothing
```

`user/3002` is `os.Getuid()` — the peer the kernel reported on the socket,
not a field in the params (which carry no actor field at all; the actor
parameter the catalog once contemplated dies in decode — T15's inertness
test). A second decide names the first decider and changes nothing.

## 9 — the brief, read by name; the unknown refused by name

```
$ aeon brief show --loop walkloop
brf-e9603690503275e3f82004d73a6fce29  v1  in_review  (walkloop @ /tmp/tmp.y1knut5BJs/repo)
  scope:  walk the board end to end
  out:    the desktop app
  - task list shows both kinds
  - a decide lands in the ledger by peer uid

$ aeon brief show --loop no-such-loop
aeon: invalid params: refused: loop "no-such-loop" is not a loop this daemon has — its slug or id names it  (-32602)
```

A person names the loop (a person carries no loop); a step reads ITS OWN
loop's brief and is refused from naming another (T15/T16's mirror).

## 10 — (g) what cannot be done to the engine's rows and the person's records

Hand-made rungs and engineered rungs answer from the SAME board, but the
doors differ: a step may not bump the counters or decisions that are a
person's, and a person's `task create` is manual by definition (OL14). The
socket refusals are asserted verbatim by
`internal/daemon/t15_socket_test.go` — `TestProjectedDoorsRefuseStepActsToPeople`
(quoted refusals: `refused: task.bump_counter — this is the person's door,
not the step's (T15)`; the open-door half asserts the person's goal create
naming its group). The plain-chat lane (a): `isPlainChat` **ships false**
(`internal/daemon/records.go` — `var isPlainChat = func(ident Identity) bool
{ return false }`, "false until S39's chat exists"): no step files a manual
task in production; the door is walked by the stub that flips it (T10,
`records_test.go`), and that is said rather than hidden.

## Zero-regression proofs (T17 steps 1–2)

- `TestAllShippedBlueprintsLoadUnchanged` — green inside the full run (the
  four pre-S38 blueprints byte-unchanged).
- `TestOfferedToolSpecsForAGrantFreeHandlerAreByteStable` (added T17) — the
  ungranted offer (read-only handler) is pinned by sha256
  (`cedc1bb3…46ab`), names offered exactly `read, list`, no catalog tool.
- Steering fixtures: `git diff --stat main -- internal/protocol/conformance/step/*steer*` — **empty**.
- `make ci` (fmt-check, vet, guard, check-protogen, check-corpusgen, test,
  build): every gate green on the S38-owned tree. The one red is
  **`ui` typecheck in the foreign-dirty ui/src** (`router.tsx(40,10):
  Type '"home"' is not comparable to RouteId`) — an in-flight foreign edit
  (App.tsx, PageLayout.tsx, router.tsx, routes.ts, ModelEditor.tsx uncommitted
  at session start), the same family as the five foreign ui reds; S38 never
  touched ui/src except the committed contract regeneration.
- Rust: cargo PRESENT — `make -C ui rust-test`: `test result: ok. 5 passed;
  0 failed` (no skip owed).

## Measured (dated, never recalled)

- Go full suite: **2667 PASS / 0 FAIL** (`go test ./... -v -timeout 900s
  -count=1 -p 1`, exit 0, 2026-10-27). `-p 2` runs failed exactly one
  load-sensitive timing test each of three runs — `TestASteerQueuedMidCall…`,
  `TestSlowCallerNeverDelaysALoop`, `TestConnectionCloseDetachesButTheShellLives`
  — each green isolated and green in the `-p 1` official run (F6).
- ui: **488 / 493** — the 5 foreign reds (app-icon, port, product×3).
- Go gates: fmt-check ✓ vet ✓ guard ✓ check-protogen ✓ check-corpusgen ✓ build ✓.
- Rust: **5 passed / 0 failed** (cargo ran, so no skip to name).

## Deletion drill (criterion 13) — branch `t17-deletion-drill` at `/tmp/t17-drill`

Removed: `internal/records/`, `internal/tools/records_tools.go`,
`internal/protocol/records.go` (+parity test), the narrow records guard,
the YAML keys (`output/requires/emits/decides` out of the schema), the
catalog grants from four handler bodies, the brainstormer body (S38 T10's)
and the two S38 blueprints (`brainstorm`, `goal-to-brief`), and the
catalog-vocabulary tests. **The tree builds and the eight pre-S38
standard blueprints validate and run as before** (full drill-suite result
recorded in STATUS/Notes). What the drill FOUND — the coupling the tests
could not show:

1. **`handler.init` PANICS on the dangling grants** — the shipped bodies
   granted catalog tools (planner: `task_create,…`) and O1 validates grants
   against `tools.ToolNames` at STARTUP: the coupling was fatal at process
   start, not deferred to first use. (Good discipline; the drill pruned the
   grant lists.)
2. **The projection file was load-bearing for BOTH doors** —
   `protocol/records.go` also carried the person seam (`RecordsAPI`,
   `RecordsFailure`) and `registerRecords`; deleting it deleted the PERSON's
   methods too. Restoring the person doors required a hand-written
   registration list — at which point **it drifted immediately**
   (`attachments.list` vs the catalog's `attachment.list`): T9's own warning,
   "a hand-written copy of a catalog is a second catalog", reproduced on cue.
3. **The YAML grammar is schema-driven** (`analyze.go` reflects the struct's
   yaml tags): removing the keys made the shipped S38 blueprints invalid —
   "remove the YAML keys" necessarily removes the artifacts that declare
   them.
4. Pins that counted the catalog and re-measured: registry name set,
   conformance fixture totals, handshake want-list, handler body count
   (31→30), the drops gap (six→five).

## §Findings — prediction vs measured

| # | Prediction | Measured | Severity |
|---|---|---|---|
| F1 | `loop create` seeds a component; the board writes against it | It registers nothing (loops carry no FK) and NO CLI verb registers components — the person's `task create` refuses unregistered roots by name | Medium (discoverability; refusal is right) |
| F2 | `aeon goal decide ID --version 1` parses | The flag package stops at the first positional — every flag behind the id was lost | High→fixed (leading id taken before Parse) |
| F3 | Walk the ladder with `docs`/`done` | Refused: five types are `bug, feature, chore, refactor, investigation`; `done` is not a status (14 listed; terminal is `complete`) — my usage text carried the wrong vocabulary | Low (the machine corrected the walk; usage text fixed) |
| F4 | `aeon watch --event` filters kinds | The flag is `--kinds` | Low |
| F5 | The grep guard watches others | It caught its own file's comment (decide method's dot-name) — second time | Low (guard as designed) |
| F6 | Timing tests are stable | Three load-sensitive tests each failed once under `-p 2` while subprocess fixtures ran; green isolated and at `-p 1` | Medium (recorded; official run `-p 1`) |
| F7 | The catalog layer deletes cleanly | It is woven: startup panic on dangling grants, seam types inside the projection file, hand-written re-registration drifted on first write | High (architecture finding — the point of criterion 13) |
| F8 | YAML keys delete independently | Grammar is schema-reflected; the keys' own artifacts became invalid | Medium |

## What was NOT verified

- **Live provider calls** — every lane above is the replay/fixture model
  (no cloud keys on this machine).
- **The native desktop appearance** (the Tauri shell — and the foreign-dirty
  `session.rs`/`ui/src` ride the tree, untouched).
- The narrow/MCP mounting of the person doors — the narrow eight is unchanged
  and its T15 guard still counts them.

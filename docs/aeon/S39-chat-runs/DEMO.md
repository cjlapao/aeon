# S39 · Chat runs — the operator walk journal

Walked 2026-10-28 (05:46–06:25 Europe/London) on a throwaway daemon:
`XDG_STATE_HOME=/tmp/aeon-walk/state`, a fresh store (38 migrations, latest
`0038_message_source` — `aeon db info`), binaries built at the T12 commit
(`aeon/.dev/aeon`, `aeon/.dev/fixture-server`). Everything below is quoted
from that run. The lane is the **fixture replay model** — the
`fake-anthropic` route answers from Anthropic-shaped SSE fixtures
(`routes set … --base-url http://127.0.0.1:18733/v1 --api-key-file $W/key
--no-price`, key hint shown as `…only` only, AGENTS §4c). **Not walked:**
the native desktop shell (foreign-dirty `ui/src-tauri`, untouched) and any
real provider — both are Findings rows marked unverified, not claims.

Kit (same directory, re-runnable): `s39walk.sh` (the operator story),
`gen_fixtures.py` (SSE fixtures), `s39cli.py` (a newline-delimited JSON-RPC
client over `/run/user/3002/aeon/rpc.sock` — the four chat doors have no CLI
verbs, so the walk types them like the app would: `loop.message`,
`loop.run.options`, `loop.run`, `loop.answer`, `loop.close`, `step.steer`,
`question.pending`, `component.create`).

`aeon ping` · **0.1ms**. Nine loops carry the story to their ends
(s39a–s39v); the final ledger: **15 loops — 12 blocked, 2 done, 1 idle;
19 `loop_run` rows; 3 question rows** (2 permission, 1 gate).

## Operator setup

`aeon project create --name "s39 walk"`; policy rows for the agents the
lanes run (`aeon policy add --type tool_access --target agent:brainstormer
--action ask --spec '{"toolName":"run_start","state":"ask"}' --id
s39-ask-run-start` and `allow` rows for `goal_create`, `goal_update`,
`write`, `attach`, `brief_create`): each echoes
`policy … stored — the daemon's next read sees it (N8 forbids caching)`.

**F-1 (again):** no CLI door registers a component. The socket has one —
`component.create` — and the walk used it:

```
{"id":"cc0ae3660a80daabb62e0a209bd31254","name":"walk-ws",
 "root":"/tmp/aeon-walk/ws","createdAt":"2026-10-28T05:51:11.146Z"}
```

(a quirk: passing `project_id` attached it to nothing — the component
landed in its own implicit group `prj-cc0ae366…`; the param is `projectId`).

## (a) brainstorm, attended — capture runs, the run finishes `done`, the loop rests `idle`

```
$ aeon loop start --blueprint brainstorm --slug s39d --route fake-anthropic \
    --component /tmp/aeon-walk/ws "Make the walk print its findings as CSV"
loop 00516482552dfcef242ed5eec39898b5  brainstorm  running
```

Six fixture requests (write ×2, `goal_create`, `attach` ×2, handoff), then:

```
loop 00516482552dfcef242ed5eec39898b5  brainstorm@1  idle
  capture            ok           proposal.md, brief.md
```

and the event the UI's run card reads (live off the subscription):

```
EV loop.run.finished {"cost":{"usdAtLeast":0,"exact":false,"partial":0,
  "unknown":1,"steps":1},"outcome":"done", … "status":"idle"}
```

— P32's floor spoken, not billed (unmetered route; `cost by route` says
`unknown — route 'fake-anthropic' is unmetered`). The goal was **born
`in_review`** (`goal-edcf052… v1 [in_review] feature` + `nobody has judged
it yet — aeon goal decide is the person's door`). The capture contract is a
real gate, not a formality — the walk's first attempts earned its sentences
verbatim: `declared artifact was not written: proposal.md`,
`declared record was not created: lane step must create 1 goal record(s),
found 0 — call goal_create`, `…is not pinned — attach … with 'attach'`.

## (a2)(b)(c) the same thread — converse answers, a goal typed into being, a version bump

```
EV loop.message.posted {"author":"human","body":"create the goal: export
  findings to CSV","kind":"note","messageId":2}
```

the brainstormer called `goal_create` and the row appeared —
`goal-6c24e318… v1 [in_review] feature / Export findings to CSV`; then,
same thread, `goal_update` on that id:

```
goal-6c24e31814c3a7150a59dbcbb211e325  v2  [in_review]  feature
  Export findings to CSV
  readable by the walk reader
```

The thread carries the agent's replies as chat rows too
(`"author":"agent"` posts fire alongside the step stream; the UI adapter
drops them by design — the stream renders the reply). The converse steps
ride the loop's `runSeq 1` card (converse is not a run: no extra
`loop.run.started`).

## (f) the offers, before and after a person decides

```
$ python3 s39cli.py call loop.run.options '{"id":"…s39d…"}'
{"blueprint":"goal-to-brief","candidates":null,
 "reason":"blueprint \"goal-to-brief\" needs goal approved — none was given",
 "runnable":false}

$ aeon goal decide goal-6c24e318… --version 2 --decision approved \
    --reason "the walk reader needs the CSV"
decided: approved

{"blueprint":"goal-to-brief","candidates":[{"kind":"goal",
 "id":"goal-6c24e318…","title":"Export findings to CSV","state":"approved"}],
 "runnable":true}
```

The S38 sentence, verbatim, then the candidate row the panel enables on.
(dev-loop stayed `runnable:true` throughout — it declares no `requires:`;
the spec (f) named dev-loop, but goal-to-brief is the gated blueprint.
Findings row.)

## (d) the agent's door, both halves

**Ask half** (the `ask` row live). converse turn → `run_start` → the step
**suspends**, a question row exists naming agent, tool and deciding rule:

```
EV step.tool.start {"tool":"run_start","stepId":9,"toolUseId":"toolu_WALK_runstart"}
EV question.asked  {"id":1,"stepId":9,"kind":"permission",
  "body":"permission required: agent brainstormer asks to run the call to
   run_start · deciding rule agent:brainstormer · asked: agent:brainstormer ·
   tool_access \"s39-ask-run-start\" · target agent:brainstormer · source
   operator · no expiry"}
step 9  status suspended
```

`aeon answer 9 approve` → `step 9 · resumed` and the transcript line that
pins **F-21**:

```
[tool_result toolu_WALK_runstart] approve      (isError false)
```

— approve is DELIVERED to the model; **zero `loop.run.started` events
followed**. Approving an ask never starts a run (T10's parity test, live).

**Allow half** (the ask row removed — `aeon policy remove --id
s39-ask-run-start`; PL4 folds ask over allow, and the question body said
so itself: `(an allow exists: "s39-allow-run-start" ·
agent:brainstormer — asked anyway, PL4)`). The next converse call started
the run directly:

```
EV loop.run.started {"blueprint":"goal-to-brief","goalId":"goal-6c24e318…",
  "goalVersion":2,"loopId":"…s39j…"}
```

and the brief lane ran (write, brief_create, attach — policy-frozen rows
visible in the dispatch manifest, `policiesFrozen:true`).

## (e) the composer steers a running step; a gate parks a stepless question

Mid-run, brief-author step 22 running:

```
$ python3 s39cli.py steer 22 "quote the text fields in the CSV, please"
{"steerId": 2, "state": "pending"}
```

The next tool was skipped and said why — the model saw it as the tool's
result and re-issued (S31 semantics, byte-identical, D10):

```
[tool_result toolu_WALK_briefcreate · ERROR] not run: the person steered
  before this tool ran — read their message below and decide whether to
  call it again
  quote the text fields in the CSV, please
```

A re-issue must carry a FRESH `tool_use` id — when a fixture reused one the
checkpoint refused with a genuine integrity sentence:
`store: checkpoint transcript of step 22: tool_use id
"toolu_WALK_briefcreate" appears twice, at messages 4 and 8; a tool result
could match either`.

Steering a step that has no boundaries left is refused naming the remedy
(D3): `{"code":-31520,"message":"the step cannot be steered","data":{
"what":"step 19 is done — nothing will read a steer now", …
"todo":"…the method to use instead: `answer` behind a question,
`step.continue {message}` behind a person-stop"}}`.

The brief completed; the gate parked — `loop … goal-to-brief@1 waiting`,
and `question.pending {slug:"s39n",componentRoot:"/tmp/aeon-walk/ws"}`
(the refusal when asked without scope is itself a quote:
`a pending question needs both a slug and a componentRoot — 'refactor'
exists in two checkouts on most machines`):

```
{"pending":true,"question":{"id":3,"kind":"gate",
 "prompt":"Approve this brief and hand it to a planner?",
 "answers":["approve","revise","abort"]}}
```

The gate row has **`step_id = NULL`** — `aeon answer` addresses by step and
cannot reach it (`step 3 has no pending question`); the door is the socket
`loop.answer {id, answer}` — the same one the UI card posts:

```
$ python3 s39cli.py call loop.answer '{"id":"…s39n…","answer":"approve"}'
{"answer":"approve","questionId":3,"status":"running"}

loop …  goal-to-brief@1  done        brief ok · brief-gate ok
```

goal-to-brief declares no `after:` — so the run ending `done` took the
**loop** `done` (rest_test's invariant, live; the spec (e) said idle, and
the machine corrected the spec. Findings row).

## (g) unattended — refused at once, no question written

`loop start --blueprint brainstorm --slug s39u --attended=false …` — the
loop table carries `attended` as declared (`('s39n','done',1)`,
`('s39u','idle',0)`). converse turn, `run_start` called, S11's rule with
zero new branches:

```
[tool_use toolu_WALK_runstart · run_start] {"blueprint":"goal-to-brief",…}
[tool_result toolu_WALK_runstart · ERROR] asked: agent:brainstormer ·
  tool_access "s39-ask-run-start-2" · target agent:brainstormer · source
  operator · no expiry (an allow exists: "s39-allow-run-start" ·
  agent:brainstormer — asked anyway, PL4)
```

and `SELECT count(*) FROM question WHERE loop_slug='s39u'` → **0**. No
question to hide; the loop went on resting idle. (The UI's three-state
notice — `Unattended — asks are denied at once. run_start cannot be
confirmed here.` — is pinned character-exact in jsdom, T12.)

## (h) closing, and refusing to close

```
$ … loop.close {"id":"…s39u…"}        # idle
{"id":"664df88f…","status":"done"}
$ … loop.close {"id":"…s39n…"}        # already finished
{"refused":{"detail":"only an idle loop can be closed; this one is done",
            "kind":"not-idle"}}
```

Composer-gone-on-done is D8's UI half, pinned in jsdom (T11) — the walk
quotes the state, the tests pin the chrome.

## (i) the restart reads `idle` back and recovers nothing

`pkill -x aeon`; serve again — `pong · 0.1ms`, `protocol=2`, scheduler
tick 15s. Then: the resting loop shows `brainstorm@1 idle` with its
capture lane intact; `loop_run` rows **19 before, 19 after**;
`SELECT count(*) FROM step WHERE status='running'` → **0**; and the new
daemon log holds **zero** recovery/resume/redispatch lines. (The spec named
`TestRecoveryLeavesIdleAlone`, which does not exist; the equivalents —
`TestAnIdleLoopIsNeitherStartedNorRecovered` (daemon/idle_loop_test.go) and
`TestIdleLoopSurvivesAReopen` (store/runs_test.go) — pass, and the restart
above is the live proof.)

## Findings

| # | Sev | Finding | Where | Action |
| --- | --- | --- | --- | --- |
| W1 | med | **F-1 recurs**: no CLI door registers a component; the socket door exists (`component.create`) and its `project_id` param is read as `projectId` — the walk's first attempt silently landed the component in an implicit group | `internal/store/components.go`, cmd surface | row for the next slice (discoverability) |
| W2 | low | `aeon policy remove <id>` does not parse the positional — answers `no policy called ""`; `--id` is required (every other verb in the file takes the id positionally) | `cmd/aeon/policy.go` | row |
| W3 | low | `loop show SLUG` / `step list SLUG` resolve the slug against the CURRENT DIRECTORY's component; from anywhere else they answer `store: not found (-32603)` — `loop show` has no `--component` flag either | `cmd/aeon/loop.go` scope resolution | row |
| W4 | med | gate questions are **stepless** (`step_id NULL`) — `aeon answer` cannot reach them; the door is `loop.answer`, which has no CLI verb (P34 socket-only is deliberate; the operator's only CLI is `brief decide`, which judges the brief, not the gate) | `internal/daemon/answer*.go`, question table | row: an operator verb for gates |
| W5 | low | attach pairs are unique per `(subject, document, rev)` with **loop-scoped** revs — re-writing the doc from a later loop does not mint a new pair; the refusal names the point of the row, but the trap is real for agents (the walk earned five verbatim refusals) | store attachments | row |
| W6 | info | transcript checkpoint refused a reused `tool_use` id (`appears twice… could match either`) — an invariant doing its job (fixture slip, not product) | step checkpoint | none |
| W7 | info | a converse turn whose final text is not the body's handoff JSON ends `outcome failed` while the loop keeps resting (fixture slip; pinned behaviour) | model-step handoff | none |
| W8 | info | `loop.message.posted` fires for `author:"agent"` rows too; the human-posted messageIds count agent rows as well (16/19/22…) — the UI adapter dropping agent posts is load-bearing | daemon converse path | pinned by T11 adapter test |
| W9 | med | spec (d) `approve → run 2 starts` predates F-21's pinning — truth: approve is delivered as the tool's result; a run needs the ALLOW row (both halves walked); spec (d)'s `from template` first message is unavailable — no shipped blueprint declares a `kickoff:` (T7's renderer is test-pinned only); spec (e) ends `idle` but goal-to-brief has no `after:` → the rest invariant ends the loop `done`; spec (f) named dev-loop — the gated shipped blueprint is goal-to-brief | spec vs `internal/loop` | deviations recorded here, spec not rewritten |
| W10 | med | the gatedModel test helper carried a DATA RACE (`m.calls` written by `Stream`, read by tests) — fixed test-side with a `callCount()` accessor | `internal/daemon/blueprint_stop_test.go` | fixed in this commit |
| W11 | high | the **closed-store family (b)** recurred under `-race`: an `answer` handler goroutine outlives the test and panics `ReadTranscript` (`protocol/step.go:645 continueAnsweredStep`) during `TestApprovingStartsTheRun`/`TestDenyingReturnsToTheModelAndStartsNothing` — 132 PASS before the panic; production frames absent | `internal/protocol/answer.go:114` goroutine | **recorded, not fixed** (standing ruling; new site named) |
| W12 | med | flake families (recorded, not fixed): `cmd/aeon TestScheduledFireIsUnattended…`, the S20 `trigger_fire awaitSettled` pair, the S38 steers pair (`TestStartupAndDoctorSayItIsOpen`, `TestASteerQueuedMidCallSkipsTheTurnsToolsAndTheLoopCarriesOn`) | daemon/cmd timers | rows stand |
| W13 | low | pre-existing UI reds, all foreign or named: 6 vitest failures (icon registry 12 SVGs, 3 product surfaces, `port.test.ts` on the foreign-dirty `ui/package.json` ui-kit pin) and `PageLayout.tsx` 4×TS6133 — reproduce identically on a clean HEAD worktree | `aeon/ui` | none (foreign-dirty set) |
| W14 | — | **unverified**: native desktop appearance (no tauri run on this box); real provider responses (fixture replay only); `from template` thread labelling (no shipped `kickoff:`) | — | say it, don't claim it |

## Zero regression — measured today (2026-10-28)

| Gate | Result |
| --- | --- |
| `go test ./... -v -timeout 900s -count=1` (official) | **2800 PASS / 0 FAIL, exit 0** |
| `fmt-check` / `guard` / `check-protogen` / `go vet ./...` | **exit 0 each** |
| `make ci` | **exit 2 — solely** the foreign-dirty `PageLayout.tsx` typecheck (4×TS6133); every Go gate green |
| `make -C ui test` (from `aeon/ui`) | **519 passed / 6 failed** — all six pre-existing (clean-worktree proof, T11 Notes), +1 `it.skip` (adapters structured-input, StepEvent has no structured input) |
| `-race` internal/loop | ok |
| `-race` internal/daemon | **exit 1** — W11's family-(b) panic; 132 PASS before it |
| invariant tests | `TestProtocolVersionIsUnchangedByS38` (asserts **2** — the spec's "1" is stale since S38 Phase A `0da33b16`), `TestS38MethodsAreNotOnTheNarrowSurface`, `TestStepSteerFixturesAreUntouched`, `TestEveryShippedBlueprintWithoutAfterEndsDone` — all PASS |
| walk ledger | 15 loops (12 blocked / 2 done / 1 idle), 19 runs, 3 question rows, restart clean |

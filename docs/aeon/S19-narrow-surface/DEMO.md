# S19 The narrow surface — the demo

**Stage 4 · Space shuttle, second of four.** *Everything so far drove Aeon from its own CLI or its own app. This is where
something else drives it — and where you can see exactly how far that reaches.*

You need **Claude Code** installed, and a shell that can run `curl`.

> **Walked on this checkout 2026-10-27** (S19 T8). Every quoted output below is the output the walk produced, not the
> output the plan predicted; the differences are in the **Findings** table at the end, which is the part worth reading.
> The one section not fully walked is §2's model leg — named, with its re-walk command, in F-4.
> **Re-walked 2026-10-27** after the weekly quota reset: the kill-request leg is **walked with the real
> model in the loop** — F-4 closed in §2; the re-walk opened a new finding, **F-8** (the parameter-schema
> and dropped-`Data` gaps the same exchanges exposed).

---

## 1. HTTP is closed until you open it

Before anything else, confirm the default:

```
aeon serve
ss -ltnp | grep aeon    # or: lsof -iTCP -sTCP:LISTEN -P | grep aeon
```

**Nothing** — the walk measured `0` listeners, and `curl -s localhost:7777/loops` answered **`exit 7`** (connection
refused). The startup log says so too, in the framework's own voice: `service "api" disabled (forced-off)`.

**A daemon that listens on a port by default is a daemon somebody exposed by accident** (N15). The socket's access control
is filesystem permissions on a `0600` path (P26), and **that does not project onto a port** — so HTTP is off until asked
for.

Now try to open it without a key:

```
aeon serve --http 127.0.0.1:7777
→ aeon: refused: --http requires a key; a bound port with no key is open to anything on this host
  exit=1
```

**An address without a key refuses to start** — exit 1, not a warning, not a generated default. (F-6: the flag is
`--http-key-file` **or** `AEON_HTTP_KEY`, never `--http-key <value>`: a credential in `argv` is in the process list and
in shell history, and that failure is silent — the same rule as `git`'s `--password`-style flags, enforced here by
grepping the CLI's own source.)

```
aeon serve --http 127.0.0.1:7777 --http-key-file ./key
```

**Now it listens.** The startup sentence names both facts, and `ss` confirms the process, not the prose:

```
listening on 127.0.0.1:7777 (narrow surface, key configured)
LISTEN 127.0.0.1:7777 users:(("aeon",pid=…))
```

The log also shows **what** mounted: the seven routes of the table — `GET /v1/loop/list`, `/v1/loop/show`,
`/v1/step/show`, `/v1/blueprint/validate`, `/v1/questions/pending`, `/v1/questions/open`, `POST /v1/loops` — mounted
by service `"narrow"`, alongside six catch-all refusal routes (F-1's fix, §3).

---

## 2. Drive a loop from Claude Code, over MCP

Add Aeon to Claude Code's MCP servers — stdio, no port. The command the walk used (F-5: the plan's
`aeon mcp` needs its `serve` subcommand; and the env must point the server at the *same* daemon, F-7):

```
claude mcp add aeon \
  -e XDG_STATE_HOME=/tmp/aeon-s19t8/state6 \
  -e LOCALRPC_ENDPOINT_PATH=/tmp/aeon-s19t8/aeon6.sock \
  -- /path/to/aeon mcp serve

claude mcp list
→ aeon: /path/to/aeon mcp serve - ✔ Connected
```

Then, inside Claude Code:

> *List my Aeon loops and tell me what the last one cost.*

**You should see it work** — the read actions answer, and Claude Code renders them.

> *Start a loop on the two-step blueprint against this repo.*

**You should see the loop start.** Triggering is a decision a caller is entitled to make.

Then ask for the thing that matters:

> *That loop is stuck. Kill it.*

**You should see it discover there is no such tool — and find out why.** What the surface hands any client that asks —
`initialize.instructions`, the field the MCP spec feeds the model, and every tool's own description — is this, captured
from the walk's `tools/list`/`initialize` against the same binary Claude Code spawned:

```
aeon has no operational control actions, by design.

  Start is available (aeon_loop_start).
  Stop, kill, pause, resume, retry and answer are ABSENT — not restricted.

  Reason: handing an agent the means to start and stop loops is handing it a way
  around every bound placed on one. Use `aeon loop stop <id>` from a terminal,
  or the app.
```

and on the trigger tool specifically:

```
aeon_loop_start — trigger of loop.start over the daemon socket. It cannot: cannot steer, pause, resume,
retry, or stop the loop it starts — the trigger is one-way (TestTriggerIsOneWay); cannot answer gates the
started loop reaches (N16); cannot grant outward effects beyond what the start request itself declared
(T6, per-loop and per-moment). No operational control (stop/kill/pause/resume/retry/answer) exists on
this surface, by design — control loops from the app or the socket (P26).
```

The enumeration the walk ran over the client's own `tools/list`:

```
aeon_loop_list, aeon_loop_show, aeon_step_show, aeon_blueprint_validate,
aeon_questions_pending, aeon_questions_open, aeon_loop_start
```

**Control verbs found in the tool names: NONE.**

**Two things are deliberate about that.**

**The capability is absent, not refused.** A `kill` tool that exists and says no is still **discoverable** — it tells an
agent the capability is nearby, and R6's point is not handing an agent the means at all.

**But the absence is legible.** A bare *"unknown tool"* reads like a **version mismatch**, and someone goes looking for the
newer Aeon that has it. So the boundary is documented **in the surface** (N14): a client listing what exists finds the
stated absence and its reason, in the place it is already looking.

> **F-4 (live-owed):** the walk registered the server with the **real** Claude Code (2.1.311) and it reported
> `✔ Connected` — the real client completed the handshake against `aeon mcp serve`. The **model-mediated exchanges**
> (the kill request and the read-back of its reason) were blocked by the subscription's weekly limit at walk time
> (*"You've hit your weekly limit · resets 8pm (Europe/London)"*), so the claim *"Claude Code displays the boundary"*
> is **owed a re-run**, not asserted. The fields tried and captured protocol-side are `initialize.instructions` and
> `tools/list[].description` (both quoted above — they are what the spec feeds a client and a model respectively);
> the re-walk when quota returns: `claude -p "That loop is stuck. Kill it." --allowedTools "mcp__aeon"` in the
> project the server is registered to. If the statement does not surface in the client's reply, the finding is
> *which field* it needs to be in — the next client will differ too.

> **F-4 CLOSED — re-walked 2026-10-27 22:13 (Europe/London), quota reset, Claude Code 2.1.311, the walk's
> own `aeon mcp serve` against a throwaway daemon (`XDG_STATE_HOME` + `LOCALRPC_ENDPOINT_PATH` isolation,
> §7's discipline).** The boundary statement **surfaces** — asked to kill a loop, Claude Code answered:
>
> ```
> I can't kill the loop from here, and I haven't worked out which loop is stuck.
> 
> **Why I can't kill it:** aeon gives me no stop, kill or abort tool. That's on purpose, not a permissions gap: stopping loops is reserved for you, through the app or the daemon's socket. I could try to reach that socket from the shell, but that would get around the limit aeon sets on purpose, so I haven't.
> 
> **Why I don't know which loop:** I called `aeon_loop_list` to find it, and it failed twice with `aeon -31105: the call needs a complete scope`. I couldn't find documentation for that scope, and this repo directory is empty, so I can't tell you its ID or what it's stuck on.
> 
> ```
>
> — the absence stated as design ("on purpose, not a permissions gap"), and **the client refused to route
> around it through the shell unprompted** ("that would get around the limit aeon sets on purpose, so I
> haven't"). The trigger leg completed too: the model called `aeon_loop_start`, the daemon started
> `loop-dmfyxsypddcz` (confirmed store-side by `aeon loop list`), and the client's reply restated the
> trigger's own description back — unattended, no outward actions without `allowOutward`, "I can't stop,
> pause or answer it with these tools." Both fields the F-4 question named **do their work through the
> model**: `initialize.instructions` shapes the refusal's reasoning, and the trigger tool's `description`
> comes back quoted verbatim.
>
> **And the re-walk opened F-8**, because the same exchanges hit two walls the protocol-side capture could
> not show: `aeon_loop_list` answered `-31105` and the model could not recover (see Findings). It also
> corrected this section's own expectation — *"List my Aeon loops … you should see it work"* is **not**
> what the real client experienced; the reads answer over the socket (quoted above, store-side) but do
> not complete through the MCP tool without operator-supplied parameter knowledge.

---

## 3. Trigger a loop from CI, over HTTP

What a CI job would do (F-6: the header is a **bare** key, not `Bearer $KEY` — that was measured in T2 and re-measured
here; F-5: the params are `blueprint`/`goal`/`workspaceRoot`, not `prompt`):

```
curl -s -H "Authorization: $KEY" -X POST localhost:7778/v1/loops \
  -d '{"blueprint":"/tmp/…/gate.yaml","goal":"survey internal","workspaceRoot":"/tmp/…/ws"}'
→ {"blueprint":"gate-walk","id":"b15b56b7…","slug":"loop-dmfhi5ae2kq3","status":"running"}
```

**You should see a loop id back.** Then poll it the way CI would — the read is `GET /v1/loop/show?id=` (the polled
shape `GET /v1/loops/<id>` from the plan is **not** a route; it answers 404 with the boundary hint, which is itself
§5's point):

```
curl -s -H "Authorization: $KEY" "localhost:7778/v1/loop/show?id=$ID"
→ status: waiting
```

**Now the interesting part.** Let that loop reach a gate, then look at what it is waiting on, and try to unblock it:

```
curl -s -H "Authorization: $KEY" "localhost:7778/v1/questions/open?slug=loop-dmfhi5ae2kq3&projectRoot=/tmp/…/ws"
→ {"pending":true,"question":{"answers":["approve","abort"],"askedAt":"…","id":1,
   "kind":"gate","prompt":"Approve the survey and continue?"}}
```

**You can read the question** — id, kind, prompt, and the declared answer set. (There is a second question read:
`GET /v1/questions/pending?projectRoot=…&slug=…` answers the store's own awaiting scan — `{"loops":[{…}],"sql":…}` —
the DB6 fact of whether anything is open, with the SQL visible because the claim is the query.)

```
curl -s -H "Authorization: $KEY" -X POST localhost:7778/v1/questions/41/approve -d '{"answer":"approve"}'
→ 404 {"error":{"kind":"not-found","message":"Answering a question requires a person. A gate exists to
   reach one, and a key authenticates a caller rather than a human. Instead: answer in the app, or
   `aeon answer <id> <value>` from a terminal."}}
```

**You cannot answer it** (N16) — and the 404 **names the reason and the alternative** (O3). F-1 is the finding this
exchange produced: before the fix, this same call answered the framework's bare `404 page not found`, which is the
shape that sends people hunting. The alternative is real, and the walk ran it:

```
aeon answer b15b56b7… approve
→ loop b15b56b7… · gate 1 answered · running     (then: status done)
```

**A gate exists to reach a human, and an API key is not a human.** If CI can answer a gate, the gate is decoration — and
S11 already took the consistent position: an **unattended** loop *denies* an `ask` rather than auto-answering it. Letting a
credential answer would be the same mistake with a bearer token attached.

Creating one fails the same way for the adjacent reason (N6):

```
curl -s -H "Authorization: $KEY" -X POST localhost:7778/v1/questions/create -d '{"slug":"x","prompt":"help"}'
→ 404 {"error":{"kind":"not-found","message":"Questions are asked by the model or the loop driver, never
   posted. A fabricated prompt, once answered, feeds a value into a transcript as though the model had
   asked for it. …"}}
```

**There is no future key, flag, or trusted caller for which either changes.**

---

## 4. What CI actually gets, and why it is smaller than you expect

Let a CI-triggered loop run into an outward action:

```
curl -s -H "Authorization: $KEY" "localhost:7778/v1/loop/show?id=$ID"
→ blocked | forge.openPR denied — effect class 'outward' resolves to deny (loop is unattended)
```

**The prediction's sentence is the real one, byte for byte.** **A CI-triggered loop is unattended**, so S16's effect
classes apply: `outward` and `irreversible` deny. **CI cannot open or merge a pull request unless someone opted in at
loop start.** Nothing was configured for this section: no policy row, no flag on the daemon — the denial arrived by
composition (the trigger pinned `attended=0` at start; the decider reads the pin beside the class).

Combined with §3, the narrow surface is **read-mostly by construction** — not by policy. Nothing has to be configured
correctly for that to hold, which is a stronger property than a permission list.

To let CI open a PR you have to say so, at start, deliberately:

```
curl ... -d '{"blueprint":"…pr.yaml","goal":"…","workspaceRoot":"…","allowOutward":true}'
```

**And that is the whole opt-in.** One field, at the one moment someone is deciding what this run is allowed to do. The
walked difference: the opt-out row's reason is the **class deny** above; the opted-in row's reason moved *past* it —
`forge.openPR refuses: required argument "base" (supplied by code) is missing` — the deny is gone and the action's own
argument check answered (on this machine there is no forge wired and no identity `with`, so the honest live claim is
**"the class denial lifted"**, which is exactly the half this surface owns). `irreversible` is not on offer at all —
with `allowOutward:true` a merge still denies, and the block reason names the flag's insufficiency (T6's gate).
And the grant is **per loop**: two loops, one key, two outcomes. No per-key permission exists anywhere to grep for
(`TestNoKeyLevelGrantExists`).

---

## 5. The same table, both ways

```
aeon narrow list
→ aeon_loop_list
   aeon_loop_show
   aeon_step_show
   aeon_blueprint_validate
   aeon_questions_pending
   aeon_questions_open
   aeon_loop_start
```

(F-5: the plan's table named methods that were never in it — `aeon_loop_get`, `aeon_step_get`, `aeon_cost_get`,
`aeon_questions_list`, `aeon_blueprint_check`; the list above is the fixture-pinned truth, and
`testdata/actions.json` must match it **both directions** or the build fails.)

**Three things to check on that table.**

**Each action names a socket method, and every one exists.** The narrow surface is **derived**, not parallel (N13) —
the fixture entry carries its `SocketMethod` typed as `protocol.MethodName` and the tests assert it against **S2's own
`PublicMethods()`**, the table S2 built for this day.

**Every entry states what it cannot do**, and an empty `Cannot` **fails a test** (N12). The `cannot` sentences are
where the boundary is legible — they ride the stdio tool descriptions (quoted in §2), the HTTP `narrow list`'s
fixture-checked text, and every `tools/list` a model reads.

**And the set is identical over stdio and HTTP:**

```
diff <(aeon narrow list --mounting=stdio) <(aeon narrow list --mounting=http)
→ (no output)          # walk: DIFF EMPTY
```

**No output.** An action available over HTTP but not stdio would be a **third surface**, and P25 says there are two
(N18). The six catch-all **refusal** routes of §3 are not actions and appear on no listing — they are the door that
answers "no, and here is why", not a capability (F-1).

---

## 6. It works with neither

```
aeon serve      # no --http, no MCP mounting
```

The walk's §1 daemon was this daemon: `api disabled (forced-off)`, zero listeners. Against it, over the socket only:

```
aeon loop start --blueprint /tmp/…/pr.yaml "cli only over the socket, no mountings"
→ loop 493bdd35…  pr-walk  running
aeon loop list --all
→ loop-dmfhj4lvcrqe  …  preflight
```

**Everything still works** — the CLI, the app, the loops. Both mountings are **optional** (N1), and *"at the edge" has to
be true in the build, not only in the diagram.*

---

## Findings — what the walk corrected

| # | Finding | Where it went |
| --- | --- | --- |
| **F-1** | **The reasoned 404 never reached the wire.** §3's answering attempt answered the framework router's bare `404 page not found` — the mux's reasoned answer (T5's sentence) was true in the package and unreachable on the live server, because unmounted paths never reach it. T4 carried this to T7 and T7's package tests could not see past the router (they *are* the router's caller). Fixed in the walk's commit: the mounting now adds **six catch-all refusal routes** (POST/GET/DELETE × `/v1/questions/…`, `/v1/loops/…`) into the same `ServeHTTP`, and the live test `TestLiveRefusalsAnswerWithReason` pins the body **must** contain the sentence and **must not** contain `page not found`. A refusal route is not an action: `narrow list`, the table and stdio's parity are unchanged. | `internal/narrow/http.go`, `cmd/aeon/narrow_http_test.go` |
| **F-2** | **A refusal answered "retry".** The junk-params and invalid-blueprint attempts answered **transport/503 promising a retry** — resending unchanged fails identically. Both daemon errors were already **typed** (`protocol.ErrBadParams`, `loop.PinRefusal`); the classifier just didn't know them. Fixed the same way every other classification is done — **type identity**, not string sniffing; the live pin asserts 409/refused and forbids the word `retry` on a caller mistake. | `internal/narrow/errors.go`, `errors_test.go` |
| **F-3** | **The demo's own gate blueprint was invalid**: a gate offering `revise` needs a rework target (`on_reject`) — the plan's prediction file failed its own validation. The trigger's 409 says `1 invalid diagnostic(s)`; the *content* is the `blueprint.validate` read's (or `aeon blueprint validate PATH`, which printed it: `gate offers revise but has no rework target`). The §3 file uses `[approve, abort]`. | this document |
| **F-4** | **§2's model leg is live-owed**: the real Claude Code (2.1.311) connected and health-checked the server (`✔ Connected`), but the weekly quota closed before the kill-request exchange. Fields tried and captured protocol-side: `initialize.instructions`, `tools/list[].description`. Re-run command in §2. **Not asserted as walked.** | this document |
| **F-5** | The plan's §5 table and §3 params named verbs and shapes that do not exist (`aeon_loop_get`, `aeon_cost_get`, `prompt` on the trigger, `aeon loop start --goal/--workspace`). Reality: `--blueprint NAME-OR-PATH "GOAL"` (root defaults to cwd), trigger params `blueprint/goal/workspaceRoot`. | this document |
| **F-6** | **The credential never comes in on a command line, or as a Bearer token.** The key arrives by `--http-key-file` (a PATH) or `AEON_HTTP_KEY` — never `--http-key <value>`: a credential in `argv` is in the process list and in shell history, and that failure is silent (the rule, enforced by grepping the CLI's own source); and the middleware reads a **bare** `Authorization: <key>` — `Bearer` is refused at the second door (`ValidateToken`, T2). §3 says so where a CI author would paste it. | this document |
| **F-7** | Two daemons, one user: the endpoint claim is per-user — the walk used `LOCALRPC_ENDPOINT_PATH` (the daemon's own message names it) plus a throwaway `XDG_STATE_HOME` (§6's discipline, made executable). A stale claim prints the holder pid, and the reclaim worked after the holder died. | this document |
| **F-8** | **The open inputSchema and the dropped `Data` make the reads unusable by a real model.** Measured with Claude Code 2.1.311 at the 2026-10-27 re-walk: (1) stdio's tool-result on a daemon error sends **`err.Error()` only** (`stdio.go` — `"aeon -31105: the call needs a complete scope"`), so the three-part answer's `Data.Todo` — "send `componentRoot` or `all: true`", the very recovery instructions the CLI renders — **never reaches the model**; the client guessed `"scope": "all"`, `"scope": {}`, and failed. (2) `inputSchemaFor` is open **by design** (`additionalProperties: true`, no typed properties, "truth about params lives in docs/NARROW.md") — but a model given no types **stringifies scalars**: `{"all": true}` arrived as the string `"true"` and `allowOutward: false` as `"false"`, both refused by the daemon's typed decode; and the docs pointer is unreachable from a client whose working directory is the user's empty repo. The client itself diagnosed the fix: *"Give `allowOutward` the type `boolean` in the tool's input schema so clients send it correctly."* Where a refusal's guidance lives in the **message** itself (`blueprint, goal and workspaceRoot are required`) the model recovered and completed — the trigger leg started a real loop that way — so the finding is precise: **what/means/todo must ride the isError text, and the open schema needs at least typed properties for the params whose type decides acceptance.** Follow-up owed (not this docs-only re-walk): render `Data` in the stdio error text and type the action schemas, pinned by tests either way. | `internal/narrow/stdio.go`, this document |

---

## What this demo does **not** show

- **No MCP client.** Aeon exposes itself over MCP; it does not consume third-party MCP servers. That is out of the alpha
  (N17) — nothing in the ladder depends on one, and a client brings its own security surface: a third-party server's tools
  passing an allow-list, its descriptions landing in a prompt as untrusted text, its capability escalation. **S13's
  `DEFERRED` records name what wants one** — 24 of 26 handler bodies referenced MCP, mostly for browser automation.
- **No operational control, ever.** Not behind a flag, not with a stronger key, not for a trusted caller.
- **No streaming over HTTP.** The socket carries streaming; a second streaming path would need its own filtering, cursor
  and teardown, which S3's M23 already argued against.
- **No multi-tenant auth.** One key, one daemon, one user.
- **No remote discovery.** Configured, never discovered.
- **Not yet shown:** ~~the Claude Code display half of §2~~ — **walked 2026-10-27** (F-4 closed): the
  boundary statement surfaced in the model's reply, and the client refused the shell route-around
  unprompted. What §2 still does **not** show: the read leg completing through the MCP tool on its own —
  F-8's schema/`Data` gaps stop it; the reads are shown store-side and over the socket.

Next: **S22** gives Aeon memory — the only optional slice in the plan.

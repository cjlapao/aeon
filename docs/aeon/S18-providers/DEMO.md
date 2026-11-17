# S18 Provider breadth — the demo

**Stage 4 · Space shuttle, first of four.** *Everything so far ran against one provider at a time, with a cloud key.*

You need **Ollama installed with a small model pulled**, and — for §3 — an Anthropic key and an OpenAI key.

Every verb below is the verb the binary actually has (`aeon routes set/probe/list`, `aeon step`,
`aeon loop start/show`, `aeon answer`); the first draft of this page named planning verbs that were
never built, and the 2026-10-26 walk corrected them all — see the Findings table at the end. **CLI
flags go BEFORE the prompt**: the step verb treats everything after the first bare word as the
prompt itself, and a `--route` typed after it becomes part of the model's task (F-1).

Walked 2026-10-26 against **Ollama 0.18.0** with `qwen3.5:latest`, `ANTHROPIC_API_KEY` and
`OPENAI_API_KEY` genuinely absent from the environment, and a throwaway `XDG_STATE_HOME` (credentials
live beside the store, so a fresh one starts with none — that is the §1 guarantee made executable).

---

## 1. A loop on a laptop, with no cloud key at all

Unset your keys. Really unset them:

```
env -u ANTHROPIC_API_KEY -u OPENAI_API_KEY make dev
```

Point a route at Ollama:

```
aeon routes set --id local \
  --provider openai \
  --model qwen3.5:latest \
  --base-url http://127.0.0.1:11434/v1 \
  --openai-endpoint chat \
  --no-price
```

(`--provider openai` **is** the OpenAI-compatible provider — M13's whole point is that there is no
separate `openai-compatible` kind; `--base-url` moves it off OpenAI's endpoint. `--no-price` is
required, not decorative: a new route with no price and no `--no-price` is **refused**, because a
silently missing price is how a typo makes a metered model report `unknown` forever.)

Then ask it something — `ask` streams, offers no tools, and is the cleanest proof that a live model
answered with no credential anywhere:

```
aeon ask --route local "In two sentences, what is a loop-native agent harness?"
```

**And the part that matters — run a step and look at its cost:**

```
aeon step --workspace <a folder> --route local "read the files here and summarise them"
aeon step show <id>
```

```
  cost    unknown (no-price-for-route)
```

**`unknown` with a reason. Not `$0.00`.** (Measured 2026-10-26 on two steps and `step list`: every
cost line read `unknown (no-price-for-route)`; the string `$0.00` appears nowhere in any renderer.)

A local model's cost is genuinely unknown to Aeon — electricity and hardware are real, and Aeon has no idea what either
costs you. So it says what it knows, which is nothing about the price. **`$0.00` would be a claim**, and a false one
(M30).

**It failed if** the cost reads `$0.00`, or `—`, or is absent. This is the same rule that has held since S3, arriving at
the case it was written for.

**Two live realities a walker meets here, both working as designed (see Findings):**

- **A fresh store denies every step tool.** `aeon policy list` reads `no policies. default is DENY
  (PL5)` — so the first step's `list`/`read` calls come back `refused: agent:step-ad-hoc · no allow
  matched · default deny (PL5)` **rendered in the stream**, and the step's record says `outcome
  failed`, honestly. Granting is the S10 door and it is one line per tool:
  `aeon policy add --type tool_access --id allow-read-step --target agent:step-ad-hoc --action allow
  --spec '{"toolName":"read","state":"allow"}'` (the `--tool` sugar fills no `state` — the spec is
  exactly `{toolName, state}`; F-2). Or run the step with no tools at all: `--context-files main.go`
  freezes files into the opening context (the `state` in that flag's name belongs to the spec field,
  and an empty `--tools ""` silently means the **default** grant, not "none" — F-3).
- **A small local model may not produce the JSON handoff.** `qwen3.5:latest` rambling instead of
  closing with the ```json block ended two steps as `failed — the model did not produce a valid
  handoff, twice`, with iterations, reason and full transcript stored. That is the model being a
  small model; the record being **honest about it** is the thing under test (F-5).

---

## 2. A route that lies is contradicted, loudly

Declare the capabilities:

```
aeon routes set --id local --provider openai --model qwen3.5:latest \
  --base-url http://127.0.0.1:11434/v1 --openai-endpoint chat --no-price \
  --capabilities tools,streaming-usage,thinking,system-role,parallel-tools
```

Then probe it:

```
aeon routes probe local
```

**A probe is ONE request per route, run only when you type it** (M32), and a probe that cannot
complete is **not** a contradiction — which is exactly what the 2026-10-26 walk saw twice:
`qwen3.5:latest` spends more than `model.ProbeTimeout` (30 s) thinking before it answers, and the
CLI said, verbatim:

```
route "local": the probe could not check: probe of route "local" could not complete: context deadline exceeded
nothing was contradicted — every capability keeps its declaration and its `declared` state
```

**Every capability kept its `declared` state. No flag was silently demoted.** (QAM-38.)

**The CONTRADICTED half of this section is pinned by fixtures, not by this machine.** A contradiction
needs a server that answers and then fails to exhibit what was declared — and this walk had one
single-provider server and no second one to lie with (I7 keeps tests away from live providers
entirely). What was checked live is what a live machine can check: the probe's one-request manners,
the failure-sentence above, and the refusal when a step needs a declared-absent capability (§5).
The contradiction path — declared present, probe finds none, `CONTRADICTED` with both sides named,
and a needing-step refused at both probe and use — is `TestProbeAgainstFixtures`,
`TestContradictionSentence` and `runtime/precheck_test.go`.

**A step against a route missing what it needs is refused, not attempted:**

```
aeon step --workspace <folder> --route local-notools "read the files and summarise"

step 4 · failed
  refused: step requires capability 'tools'; route 'local-notools' does not have it (no model call was made)
```

**A contradiction is never silently accommodated** (M28). Quietly dropping `tools` because the probe found none is how a
loop runs three steps, discovers it cannot call a tool, and **blames the model for a route's mistake.**

**The same probe is a daemon method, not only a CLI verb** (D16, P34): `routes.probe {route}` over
the socket is the same function the CLI calls — the CLI *is* a socket client here. Over a raw socket
you must `initialize` first, and `protocolVersion` is an **int**, not a string; and the dispatcher
handles a connection's backlog concurrently, so the two lines must be **paced** — feed the probe
only after the initialize answer has come back, or it races ahead and is refused as pre-initialize
(F-7, both halves measured):

```
{ printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}\n'
  sleep 2
  printf '{"jsonrpc":"2.0","id":2,"method":"routes.probe","params":{"route":"local"}}\n'
  sleep 45; } \
  | socat -T80 STDIO,ignoreeof UNIX-CONNECT:.dev/aeon.sock
```

Now look at the three states:

```
aeon routes list
```

```
local            openai     qwen3.5:latest           stored       no key     unmetered cap 4096
                   http://127.0.0.1:11434/v1
```

**`verified` is not the same as `declared`**, and neither is the same as `contradicted`. Six places in this system have now
needed that distinction — cost, the `bash` receipt, blueprint validation, connection liveness, verification, and this
([P31](../ARCHITECTURE.md#8-product-decisions)).

---

## 3. Suspend on Anthropic, resume on OpenAI, resume again on Anthropic

**This is the section the canonical format was bought for.**

**Walk status 2026-10-26: NOT WALKED LIVE — this machine has no Anthropic or OpenAI key.** The plan's
first draft also described verbs that do not exist (`aeon loop route`, `aeon debug last-request`,
`aeon step show --dropped`); the commands below are the real ones, and the three legs are carried by
their Go gates until someone with keys walks this for real. Do not read this section as verified
against live providers; read the gates as the claim, and this section as how to verify it when you
have credentials (F-6, F-8; `CAPABILITIES.md` names the same gap rather than filling it from
memory).

Start a loop on Anthropic and let it suspend on a question (the `question` tool is granted with
`--allow-question` on a plain step; `loop start` routes every step through the loop):

```
aeon loop start --blueprint two-step --route claude "survey internal, and ask me which package to start with"
```

While it waits, look at what is in the transcript — `step show --json` prints the stored messages,
provenance included:

```
aeon step show <id> --json | jq '[.[] | select(.role=="assistant")] | .[0] | {provenance, blocks: [.content[].kind]}'
```

```json
{
  "provenance": { "route": "claude", "provider": "anthropic", "model": "claude-…" },
  "blocks": ["thinking", "text", "tool_use"]
}
```

Now **steer the route switch, then answer**. There is no `aeon loop route`: a route change is a
**steer** (`step.steer` over the socket — the app sends it; P34), and since T8 a steered switch is
**refused mid-step and applied at the step's hand-back**, announced by `loop.route.applied`:

```
printf '…{"method":"step.steer","params":{"stepId":<id>,"text":"use the other model","route":"gpt"}}…' | socat …
aeon answer <id> internal/store
```

**The refusal is the point:** the stream shows `route switch to "gpt" refused mid-step (step N) — it
will take effect at the next step: a tool call from one provider cannot be answered in another's
format`, and the loop then **applies it on the hand-back** — the answer resumes the step on gpt
(Q6: the loop's current route governs a cold resume; `loop.route.applied` is the audit line).

Then check three things, in order, because each proves something different.

**First — what OpenAI was actually sent.** (There is no `aeon debug last-request`; the wire-level
claim is pinned at the adapter: `adapters/crossprovider_wire_test.go` asserts the request JSON has
no `thinking` key, none of the foreign signature bytes, and counts every withheld block.) Dropped
**and counted** is M4's rule; `thinkingDropped` rides the step result JSON — the store-level proof is
right there in the same transcript:

**Second — what is still in the store:**

```
aeon step show <id> --json | jq '[.[] | select(.role=="assistant") | .content[] | select(.kind=="thinking")] | length'
```

**Still three.** **The drop happened at the wire, not in the record.** That single property is what makes the whole thing
work in both directions — if a drop ever reached the store, the trip would be one-way.
(`runtime/crossprovider_test.go` and `daemon.TestStoreUnmodifiedAtEveryPoint` read the block back
byte-identical — `text` and `signature` — after **each** of the three legs.)

**Third — go back:**

```
printf '… step.steer with route "claude" …'        # refused mid-step, applied at the hand-back
aeon step --route claude "continue the survey"     # or the loop's next dispatch
```

**You should see Anthropic accept it**, and the thinking blocks from the *first* Anthropic turn replayed **byte-identical,
signatures included** (M17).

**But not the OpenAI turn's content as thinking** — there was none, and if there had been, it would not be replayed.
**Thinking blocks go only to the provider that produced them** (M27), which is what M26's provenance is for.

Sending an Anthropic signature attached to a history Anthropic did not generate would be a bet on somebody else's
validator. Dropping is defined behaviour; the alternative is hope.

**It failed if** the round trip lost a thinking block from the *store*, or if Anthropic rejected the replay. The first
means a drop leaked into the record; the second means the replay rule is wrong.

---

## 4. Cost splits by provider

```
aeon loop show <slug>
```

(`loop show` prints the ledger unconditionally — there is no `--costs` flag; the ledger was T7's, and
a step's cost line is `loop show`'s own tail.)

**You should see the providers separately:**

```
  claude       anthropic  claude-…      1 step    $0.04120
  gpt          openai     gpt-…         1 step    $0.01800
  local        openai     qwen3.5:…     1 step    unknown (no-price-for-route)
                             total      at least $0.0592
```

**Note the total.** It reads as a **floor**, because one component is `unknown` — S9's rule, arriving at the case where a
loop legitimately mixes a metered model with an unmetered one. A step with no assistant provenance lands
`(unattributed)`, on its own line, last.

That table is only possible because of M26. Before provenance, a transcript spanning providers could report a total and
nothing more.

---

## 5. A step that needs tools is refused before the call

```
aeon routes set --id local-notools --provider openai --model qwen3.5:latest \
  --base-url http://127.0.0.1:11434/v1 --openai-endpoint chat --no-price \
  --capabilities streaming-usage        # 'tools' is deliberately NOT declared
aeon step --workspace <folder> --route local-notools "read the files and summarise"
```

**Walked live 2026-10-26 — verbatim:**

```
step 4 · failed
  refused: step requires capability 'tools'; route 'local-notools' does not have it (no model call was made)
```

**And nothing was sent:** `(no model call was made)` is the runtime's own sentence, emitted by the
check that runs **before the call** — the gate `TestCheckIsInTheRuntimeNotTheAdapter` plus
`TestClientSpyIsAtZero` (the client recorded zero requests) is the executable form of the server-side
"check the logs" you would do with a logging proxy.

**Discovering at the first tool call that the model cannot call tools wastes every step before it** (M29). This is the
same instinct as S8's load-time validation: fail where it is actionable.

---

## 6. No third adapter

```
ls aeon/internal/model/adapters/*.go | grep -v _test
```

**You should see two adapters** (the third wire file is the openai adapter's **second endpoint**, not
a third adapter — `openai_responses.go` answers `route.openaiEndpoint: responses` on the same
provider id):

```
  anthropic.go
  openai.go            ← the OpenAI-compatible path is this file plus flags
  openai_responses.go  ← the same adapter's /v1/responses wire
  registry.go          ← answers to exactly two provider ids
```

and the doctrine files beside them in `internal/model/`: `capability.go`, `provenance.go`, `cost.go`, `probe.go`.

M13 said it: *"the OpenAI-compatible path is the OpenAI adapter plus capability flags, not a third adapter — a
near-duplicate file drifts; the differences are absences, not behaviours."*

**And the claim is falsifiable.** Read the record of what was actually encountered:

```
cat aeon/internal/model/adapters/CAPABILITIES.md
```

**Every difference met while building this, and whether a flag expressed it.** If any could not be — a server whose tool
calls arrive in a genuinely different shape rather than simply absent — it is recorded there as **a finding about M13**,
not quietly fixed with a third file (M31).

**Walked 2026-10-26:** the file names **Ollama 0.18.0** with `qwen3.5:latest` **with its version and
measurement date**, says plainly that **no second server has been run**, carries the five-flag table
plus three findings (`delta-reasoning`, `tool-call-ids`, `store-false`) and the S18 T8 cross-provider
section naming the live-foreign-transcript gap. **M13 survived the slice: no third adapter file was
ever needed** — `TestNoThirdAdapterFile` and `TestEveryDifferenceIsAFlagOrAFinding` are the two
guards that make "survived" mean something.

---

## Findings — what the walk found that the plan did not predict

| # | What reality said | Where it went |
| --- | --- | --- |
| F-1 | CLI flags after the prompt are swallowed **into the prompt** (the parser takes everything after the first bare word as the task); a `--workspace` typed late silently ran the step against the **wrong folder** | Documented: flags before the prompt, everywhere in this page. CLI UX improvement recorded, not fixed at T9 |
| F-2 | `policy add --tool` sugar fills no `state` — the ToolAccess spec is exactly `{toolName, state}` and the boundary refuses a missing field with a sentence naming it (working as designed) | This section; `--spec '{"toolName":"read","state":"allow"}'` is the walked form |
| F-3 | `--tools ""` does **not** mean "no tools" — empty falls back to the default grant `read,list,write` | This section. (A step that truly needs no tools today: grant defaults and expect no calls, or use `ask`.) |
| F-4 | `qwen3.5:latest` cannot finish a probe inside `model.ProbeTimeout` (30 s fixed) — its reasoning alone runs past it — so this machine's route can never earn `verified`, and a live CONTRADICTED was unreachable (one server, no way to make it lie) | QAM-38 keeps the failure-keeps-declared case; the probe-not-disabling-thinking on reasoning routes is recorded as an improvement, deliberately not fixed in a docs task |
| F-5 | `qwen3.5:latest` handoff compliance is flaky — 2 of 3 walked steps ended `failed — the model did not produce a valid handoff, twice`, honestly recorded (iterations, reason, transcript) | §1 says so where it happens. The step record's honesty under a flaky small model is what the demo actually shows |
| F-6 | The planned commands `aeon route add/probe/show`, `aeon loop route`, `aeon debug last-request`, `aeon step show --dropped`, `aeon loop show --costs` were **never built** — a planning document had become the spec | Every section rewritten to real verbs; the switch is a `step.steer` with `loop.route.applied`; the wire checks moved to their adapter gates. **Process finding: task docs must be re-checked against the binary before a walk trusts them** |
| F-7 | `routes.probe` over a raw socket requires `initialize` first, and `protocolVersion` is an **int** — the planned socat one-liner could not have worked | Corrected above |
| F-8 | §3 was **not walkable live** (no Anthropic/OpenAI keys on this machine); the round-trip claim is carried by its Go gates, and `CAPABILITIES.md` names the live-foreign-transcript gap instead of filling it from memory | §3's walk-status banner; DoD boxes for the live legs stay unticked |
| F-9 | Walking T8's §3 against the implementation found a **real defect**: a deferred route switch was applied only on the step's `done` path — a step that **suspended** after a steered switch lost it forever (the steer had drained once), making the refusal's "takes effect at the next step" a lie exactly on the round trip's middle leg | Fixed same slice: `applyPendingRoute` now runs at **every** hand-back (advance + continue, all branches); `loop.TestDeferredRouteSurvivesASuspendingStep` is the regression gate |

---

## What this demo does **not** show

- **No Gemini.** v1.1, and M12 keeps it one adapter file plus a registry entry — which is the whole point of M12.
- **No model catalogue.** Routes are configuration; **S23** supplies sensible ones. Here you wrote them by hand.
- **No automatic failover.** A route is chosen deliberately. Silently retrying a failed provider on another one changes
  what produced the work without saying so — and M26 exists because that matters.
- **No model ranking.** Aeon does not tell you which model is better.
- **No prompt-caching strategy.** The adapter reports cache tokens; what to cache is the runtime's.
- **No embeddings.** S22 reuses this layer's transport and cost.
- **No live §3 yet** (F-8). The app half **is** built (T10): the Providers tab renders the three
  capability states as pills and carries the one operator-clicked **Probe** per route (M32 — nothing
  else fires it), and the bench demo runner runs this slice's local rows — probe, the refusal as
  verbatim data, the unmetered cost — with the A→B→A row `not runnable here` naming its Go test.
  What this walk record still does **not** cover: someone physically clicking Probe in the desktop
  app against a live Ollama — the six bench tests pin the page's behaviour against the socket, and
  that live click-through joins §3's owed legs.

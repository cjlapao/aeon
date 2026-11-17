# S3 Model layer — the demo

**Stage 1 · Skateboard.** *Second of four. The first slice that does something you would pay for.*

S1 built the garage. S2 made the daemon answerable. This one makes it **talk to a model**, over the protocol,
with honest cost.

> **Walked 2026-10-15 on `linux/amd64`, every step.** Nine predictions were wrong and are recorded in
> §Findings. **Two were real defects** — a provider failure that lost its three-part shape in exactly the mode
> a person uses, and a cost state the fixture set could not produce.
>
> **You do not need a provider key to walk this.** That is new, and it is the finding that mattered most: a
> demo requiring a credential and a real spend is a demo nobody walks. `scripts/fixture-server` replays a
> committed fixture as if it were a provider, so **the daemon, the protocol, the adapters, the SSE reader,
> the cost arithmetic and the CLI are all the real ones** — only the provider is substituted, which is the
> one part a demo cannot honestly include.

---

## Before you start — two terminals, no credentials

```sh
cd aeon
make build
go build -o dist/fixture-server ./scripts/fixture-server

export XDG_STATE_HOME=/tmp/aeon-s3/state
export LOCALRPC_ENDPOINT_PATH=/tmp/aeon-s3/run/rpc.sock
mkdir -p /tmp/aeon-s3/run /tmp/aeon-s3/state
chmod 700 /tmp/aeon-s3/run /tmp/aeon-s3/state
```

A helper, because every step below restarts the pair:

```sh
replay() {               # replay <fixture> [delay]
  pkill -x aeon; pkill -x fixture-server; sleep 1
  rm -f "$LOCALRPC_ENDPOINT_PATH"
  ./dist/fixture-server -fixture "$1" -delay "${2:-40ms}" &
  sleep 1
  AEON_ANTHROPIC_BASE_URL=http://127.0.0.1:8123/v1 ANTHROPIC_API_KEY=unused \
  AEON_OPENAI_BASE_URL=http://127.0.0.1:8123/v1    OPENAI_API_KEY=unused \
    ./dist/aeon serve &
  sleep 4
}
```

> **`pkill -x`, not `pkill -f`.** The `-f` form matches your own shell's command line and kills your
> terminal. Done three times during this slice's work, which is three more than necessary.

**With a real key instead**, if you have one: set `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` and leave the
`*_BASE_URL` variables unset. Everything below behaves the same; only the words in the reply change.

---

## 1. It streams

```sh
replay internal/model/testdata/anthropic/text-only.sse
./dist/aeon ask --route anthropic "what is a monorepo?"
```

**You should see** the answer **arrive a few words at a time**, then the accounting:

```
Hello, world 🌍

cost    $0.000111
tokens  in 12 · out 5 · cache-read 0 · cache-write 0
stop    end_turn
```

**It failed if** the whole reply appears at once after a pause. The replay server pauses between frames
deliberately, so buffering is visible — and since S6's questions, S5's tool output and S9's live view all
ride this path, a buffering bug here is one you will meet four more times.

**`--route` is required** and there is no default. Until S23's configuration, one route exists per provider
whose key is present, named after the provider: `anthropic`, `openai`. The daemon prints them at startup.

---

## 2. The same request, the other provider

```sh
replay internal/model/testdata/openai/text-only.sse
./dist/aeon ask --route openai "what is a monorepo?"
```

```
Hello, world 🌍

cost    $0.00008
tokens  in 12 · out 5
stop    stop
```

**This is the point of the slice.** Identical output shape, from a completely different wire format. The two
providers disagree about where the system prompt goes, whether tool arguments are a JSON string or an object,
where tool results live, whether the output cap is `max_tokens` or `max_completion_tokens`, and what a
streaming event looks like. **None of that is visible here**, which is what the canonical format bought — and
it is why two adapters land now rather than one here and one at S18.

**Two differences in that output are real and worth seeing:**

| | |
| --- | --- |
| `$0.000111` vs `$0.00008` | the same tokens at different prices. The arithmetic is `tokens ÷ 1,000,000 × price`, and it is checkable by hand: 12 in at $3/Mtok plus 5 out at $15/Mtok is $0.000111 |
| `cache-read 0` present for one, absent for the other | Anthropic **reported** zero; OpenAI reported **nothing**. *Absent is not zero* (M9), and here it is, visible in output rather than argued about in a design |

---

## 3. Cost, in three states

### known

§1 and §2 above. A figure, and nothing else to say.

### partial — a floor, not an estimate

```sh
replay internal/model/testdata/openai/usage-partial.sse
./dist/aeon ask --route openai "hello"
```

```
Only the prompt tokens were counted.

cost    at least $0.00003  (missing: outputTokens)
tokens  in 12
stop    stop
```

**"At least" is the whole claim**, and it names what was absent. A floor presented as a total is a number
somebody will add up.

### unknown — and never `$0.00`

```sh
replay internal/model/testdata/anthropic/usage-absent.sse
./dist/aeon ask --route anthropic "hello"
```

```
No message_start and no message_delta, so no usage at all.

cost    not reported  (the provider reported no token counts)
tokens  not reported
```

**It failed if any of these prints `$0.00`.** A zero that means *we do not know* is the specific dishonesty
this design exists to prevent, and it is the easiest thing in the slice to get wrong: one `omitempty` in the
wrong place does it. Two structural tests guard those two struct tags, and one of them was **not a guard**
until a negative control proved it — see T1's notes.

There are three reasons an `unknown` can carry, and they are different problems with different fixes:

| reason | what to do |
| --- | --- |
| the provider reported no token counts | ask the provider to send usage — for OpenAI that is `stream_options.include_usage`, which Aeon sets |
| the route has no prices configured | add prices to the route |
| the stream ended before any usage arrived | the call was cut; nothing is wrong with your configuration |

A UI that showed all three as `—` would send you to the wrong one.

---

## 4. Cancellation still reports the spend

```sh
replay internal/model/testdata/anthropic/text-only.sse 2s     # slow, so there is time to interrupt
./dist/aeon ask --route anthropic "count to a hundred"
```

Wait for a line, then `Ctrl-C`.

```
Hello

interrupted — the daemon is still accounting for what was spent;
run `aeon watch --kinds ask.done` to see it.
```

**And the accounting really does arrive.** In another terminal, before you interrupt:

```sh
./dist/aeon watch --kinds 'ask.done'
```

```
    12  ask.done   askId=a-1 cost={"state":"unknown","reason":"stream-aborted-early"} …
```

**The daemon emits `ask.done` whatever happened** — success, provider failure, or the client walking away
(M15). Those tokens were generated and will appear on a bill; a cancelled call that reports nothing loses
real money from the accounting, which is the same class of error as printing `$0.00`.

**The demo predicted the CLI would print the cost line itself.** It cannot: the client has stopped listening
by then, and inventing a figure would be worse than pointing at where the real one is. Finding 5 below.

---

## 5. Streaming goes through subscriptions, not a private channel

Terminal **A**:

```sh
./dist/aeon watch --kinds 'ask.*'
```

Terminal **B**:

```sh
./dist/aeon ask --route anthropic "count"
```

**Back in terminal A:**

```
watching s-7a48f6ad · kinds=ask.* project=* · from seq 3
  Ctrl-C to stop

     6  ask.delta   askId=a-1 kind=text text=Hello
     7  ask.delta   askId=a-1 kind=text text=, world 🌍
     8  ask.done    askId=a-1 cost={"state":"known","usd":0.000111} stopReason=end_turn usage={…}
```

Same envelope, same `seq`, same cursor as `client.connected` and every other event. **One streaming path for
the whole product** (M23) — a bespoke channel for model output would need its own filter, its own cursor and
its own teardown, and would then have to be reconciled with subscriptions at S9 when the app wants both.

**Try it with the wrong filter** to see that the filter really applies: `--kinds 'client.*'` receives nothing
from an `ask`.

---

## 6. A signed `thinking` block survives a round trip

```sh
replay internal/model/testdata/anthropic/signed-thinking.sse
./dist/aeon ask --route anthropic --show-transcript "think about the weather"
```

**You should see** the reasoning stream dimly, then the answer, then the canonical transcript as JSON — and
then a note you should read:

```
note: the thinking block above carries NO signature. A signature is replay
metadata and never reaches a client, so this transcript is for reading rather
than for replaying — S5 persists the signed version daemon-side.
```

**The demo predicted a signature here, and that was wrong** — finding 7. A signature is not display content:
it is replay metadata, and `signature_delta` is accumulated by the adapter and **never yielded**. A client
rendering it would show a reader a base64 blob; a client treating it as text would lose it on the way to
storage. The daemon keeps it, which is the whole point.

So round-trip a transcript that **has** one:

```sh
cat > /tmp/signed.json <<'JSON'
[
  {"role":"user","content":[{"kind":"text","text":"weather?","isError":false}]},
  {"role":"assistant","content":[
    {"kind":"thinking","text":"I should call the tool.",
     "signature":"ErUBCkYIBBgCKkB3+Zm3/Lx1TESTsignature==/abc+def==","isError":false},
    {"kind":"text","text":"Checking.","isError":false}
  ]}
]
JSON

./dist/aeon debug roundtrip --file /tmp/signed.json
```

```
transcript  2 message(s) from /tmp/signed.json
validation  ok
signature   block 0: 48 bytes

roundtrip   IDENTICAL — 1 signature(s) survived byte for byte
```

**It failed if the signature differs by one byte.** Anthropic rejects a tool-use turn whose prior thinking
was altered, and because **S7 recovers a crashed step by replaying a stored transcript**, a byte lost here
fails four slices later with an error about signatures and no hint that this layer was involved.

---

## 7. A provider failure says what to do

```sh
pkill -x aeon; pkill -x fixture-server; sleep 1
./dist/fixture-server -fixture internal/model/testdata/anthropic/text-only.sse -status 404 &
sleep 1
AEON_ANTHROPIC_BASE_URL=http://127.0.0.1:8123/v1 ANTHROPIC_API_KEY=unused ./dist/aeon serve &
sleep 4
./dist/aeon ask --route anthropic "hello"
```

```
cost    not reported  (the stream ended before any usage arrived)
tokens  not reported
aeon: the model provider failed  (-31102)
  what   model: anthropic answered 404: {"error":{"message":"…"}} (not retried: the provider will answer the same way)
  means  the provider refused the request, or the stream was cut; the daemon retried where
         retrying was safe and did not where a reply had already begun
  todo   the message carries the provider's own words — check the route's credential and model name first
```

**Three things in that output are deliberate.** The cost line comes **first**, because a failed call still
cost something. The provider's **own words** survive, because a wrong model name says so precisely and
paraphrasing would discard the only useful detail. And **"not retried"** is stated: a 404 is a schema or
content error, so another attempt produces the same answer — whereas a 503 is retried, with `Retry-After`
honoured in both its forms.

**This is finding 1, and it was a real defect.** In streaming mode the failure arrives inside `ask.done`
rather than as a JSON-RPC error, and it was a bare **string** there — so `aeon ask` printed the provider's
sentence and no `todo`, in exactly the mode a person uses. It now carries the same structured error the
synchronous path returns.

---

## 8. It never contacted a provider during `make test`

```sh
unset ANTHROPIC_API_KEY OPENAI_API_KEY
make test
```

**You should see** the suite pass.

Every adapter test runs against a committed fixture, and the integration tests **strip** the provider keys
from the environment rather than merely not setting them — a developer's real key would otherwise make these
tests call a provider, which would pass on their machine and fail in CI.

**Each fixture is labelled with its origin**, and every one in this slice is `hand-written` with a
`notVerified` field naming what was not checked against a live endpoint. A hand-written fixture presented as
a recording is a failed check (O4): the label is the only thing that tells you what a fixture is worth, and
that is invisible in the test that uses it.

```sh
cat internal/model/testdata/manifest.json | head -20
```

---

## What this demo does **not** show

- **Nothing is stored.** `ask` streams and forgets (M24). No history, no `aeon ask --list`. The store is S4,
  and it is what makes the cost line above worth recording rather than watching.
- **No tools run.** The model can *ask* for one — §6's fixtures include a tool call, and `aeon ask` prints
  `→ get_weather({"location":"Lisbon"})` — but nothing executes it. `read` and `bash` arrive at S5.
- **No loop, no step, no task.** `ask` is a single call, not a unit of work.
- **Two providers, not three.** Ollama and any OpenAI-compatible gateway are S18, with capability probing —
  though `AEON_*_BASE_URL` is already the mechanism they will use.
- **No real provider was contacted, at any point, by anybody.** Every number above comes from a committed
  fixture. **That is a limitation of this walk, not a claim about the code**: the adapters have never spoken
  to a live endpoint, and the first person to do so should expect to find something. The manifest says
  precisely what was never verified.

The skateboard is S5, and it is close: give this a store and two tools and it becomes a step.

---

## Findings from the walk

**Recorded rather than quietly fixed, except 1 and 2 — those were defects.**

| # | Finding | Resolution |
| :--: | --- | --- |
| 1 | **A provider failure lost its three-part shape in streaming mode.** `ask.done` carried the error as a bare string, so `aeon ask` printed the provider's sentence and no `todo` — in the mode a person actually uses | **DEFECT, FIXED.** `AskDonePayload.Error` is now the same structured `*Error` the synchronous path returns, with a conformance fixture for the shape |
| 2 | **No fixture could produce a `partial` cost**, so the third state was unreachable outside a unit test and §3 had to describe it in prose | **DEFECT IN THE FIXTURE SET, FIXED.** `openai/usage-partial.sse` added — deliberately synthetic, and the manifest says so. All three states are now walkable |
| 3 | The demo needed a provider key, and §1 called `aeon ask` with no `--route` | **Demo rewritten.** `--route` is required and there is no default; and the whole walk now runs against `scripts/fixture-server`, so it needs no credential and costs nothing |
| 4 | Route ids were predicted as `anthropic-sonnet`, `openai-gpt`, `local-unpriced`, `anthropic-sonnet-thinking` | **Demo corrected** to `anthropic` and `openai`. Until S23's configuration there is one route per provider, named after it |
| 5 | §4 predicted the CLI would print a cost line after `Ctrl-C` | **Demo corrected.** It cannot: the client has stopped listening, and inventing a figure would be worse. The daemon **does** emit `ask.done`, and the CLI now says where to see it |
| 6 | The cost line was predicted as one line: `anthropic-sonnet · in 24 · out 31 · $0.000516 (known)` | **Demo corrected** to the three lines it prints. The state is rendered as a *sentence* rather than a parenthesised word, because `(known)` reads as decoration and `not reported (the provider reported no token counts)` tells you what to do |
| 7 | **§6 asked for a transcript containing a signature. That is impossible** — a signature never reaches a client | **Demo corrected**, and the CLI says so where a reader will see it. The round-trip step now uses a transcript with a signature supplied directly, which is what S4 and S5 will persist |
| 8 | §5's event lines were predicted as `201 ask.delta a-7 "One,"` | **Demo corrected** to the `key=value` form `watch` prints |
| 9 | §6's round-trip output was predicted as one line naming "4 blocks" and a 512-byte signature | **Demo corrected.** It prints validation, each signature's length, and the verdict — and 512 bytes was a guess; the real length depends on the provider |

**Two steps were added**: the no-credentials setup (§Before you start), without which the walk cannot be done
at all by most readers; and §7, the provider failure, which the demo never covered and which is where the
first defect was found.

**And one warning earned the hard way**: `pkill -f` matches your own shell. It killed this session's terminal
three times across S2 and S3 — it is now in both demos and in S1's test spec.

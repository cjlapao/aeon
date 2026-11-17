# S5 One step, end to end — the demo

> **2026-10-17 follow-up:** The captured run below predates the workspace file
> tools added after a live demo. A default step now offers `list`, `read`, and
> `write_file`; `bash` still requires an explicit grant. `list` finds files when
> a guessed `README.md` is absent from the selected workspace. The app releases
> its event subscription when the Step tab closes and ignores repeated event
> sequence numbers, so revisiting the tab does not duplicate streamed text.
> The transcript excerpts below remain as the original fixture capture.

**Stage 1 · Skateboard. This slice completes it.**

Four slices ago there was a garage. Now there is a thing that gets somewhere: a model that reads your files,
runs your commands, reports back in a structured shape, and leaves a transcript you can replay.

**Read section 9 before you use `--allow-bash`.** The skateboard has no brakes yet.

---

## 0. Two ways to walk this

**With a real provider** — what a person will actually do:

```
export ANTHROPIC_API_KEY=…
make dev
cd ~/some-repo
aeon step "read the README and tell me what a newcomer would still not know"
```

**With no credential at all** — how this demo was walked, and how it can be walked on a machine with no key.
`scripts/fixture-server` replays a recorded SSE fixture as if it were a provider, one fixture per request:

```
# terminal 1 — the "provider"
cd aeon
go run ./scripts/fixture-server \
  -fixture internal/model/testdata/anthropic/step-read-tool.sse \
  -fixture internal/model/testdata/anthropic/step-handoff.sse

# terminal 2 — the daemon, pointed at it
AEON_STATE_DIR=/tmp/aeon-demo/state \
LOCALRPC_ENDPOINT_PATH=/tmp/aeon-demo/rpc.sock \
ANTHROPIC_API_KEY=unused \
AEON_ANTHROPIC_BASE_URL=http://127.0.0.1:8123/v1 \
  aeon serve

# terminal 3 — in a throwaway repository
LOCALRPC_ENDPOINT_PATH=/tmp/aeon-demo/rpc.sock aeon step "…"
```

**The daemon, the protocol, the loop, the tools, the store, the cost arithmetic and the CLI are all the real
ones.** Only the provider is replaced — which is the one part a demo cannot honestly include, because a test or
a walkthrough must never contact a live provider (S3's I7).

Every transcript below is **copied from the fixture walk**, so what you read is what the code printed. Where a
section needs a real model, it says so.

`AEON_STATE_DIR` points the store somewhere disposable. **Do not walk this demo against your real store.**

### If you are on an OpenAI reasoning model, you have two options

OpenAI's models with a reasoning dial **refuse function tools unless reasoning is off**, on their default
API:

```
Function tools with reasoning_effort are not supported for gpt-6-luna in /v1/chat/completions.
To use function tools, use /v1/responses or set reasoning_effort to 'none'.
```

Every Aeon step offers tools. The provider's own message names both ways out, and Aeon now supports both:

**Option A — do nothing.** A step on a `chat` route (the default) lowers the route's reasoning to `none` by
itself and says so in its opening line:

```
· 0 declared file(s), 1 tool(s) offered · reasoning lowered from "medium" to `none` for this step,
because it offers 1 tool(s) and this route's API refuses tools while reasoning is on. `aeon ask`
on route "openai" still uses "medium". To keep reasoning in a step, move the route to the API that
accepts both: `aeon routes set --id openai --provider openai --model gpt-6.1-sol --openai-endpoint responses`
```

`aeon ask` on the same route is unaffected — it reasons at whatever the route says, because a chat offers no
tools and the restriction never applies to it.

**Option B — switch the route to the other API**, if you want the step to reason too:

```
aeon routes set --id <your route> --provider openai --model <your model>   --openai-endpoint responses --reasoning-effort medium
```

`/v1/responses` accepts function tools and reasoning in the same request, which is the whole reason it
exists here. Section 10 below walks it.

**Either way, nothing is forced on you.** `--reasoning-effort none` still works exactly as before if you
would rather turn reasoning off explicitly, and an explicit `--reasoning-effort` on a single `aeon ask` or
`aeon step` call always overrides the route's, win or lose:

```
aeon step --reasoning-effort medium "…"     # takes the provider's refusal literally, on a chat route
aeon ask  --reasoning-effort low    "…"     # this call only; the route is untouched
```

`routes set` keeps what you do not name, so any of the commands above change only the field they mention and
leave the base URL, the cap, the prices and the credential alone.

---

## 1. A step reads a file and answers

```
aeon step "read the README and tell me what a newcomer would still not know"
```

**You should see** the step announce itself, use a tool, and finish:

```
step · read the README and tell me what a newcomer would still not know
workspace /tmp/aeon-demo/ws · tools read

· 0 declared file(s), 1 tool(s) offered
I will read the README first.
→ read README.md
← read README.md · 203 bytes
The README covers installing and the CLI verbs.

```json
{"status":"ok","summary":"Read README.md. It explains installation and the verbs, and says nothing about where configuration lives."}
```
· ok

step 1 · ok

{
  "status": "ok",
  "summary": "Read README.md. It explains installation and the verbs, and says nothing about where configuration lives."
}

  iterations 2
  cost       $0.00627 (known)

The transcript is stored. `aeon step show 1` reads it, and `aeon step verify 1` checks it round-trips.
```

**This is the skateboard.** A bounded loop, a real tool call, a structured outcome, a cost in one of three
honest states, and a stored transcript. Nothing about loops, blueprints, gates or agents — and it is already
useful.

Two things in that output are deliberate and easy to miss:

- **`0 declared file(s)`** — the ad-hoc handler declares no context files. The step *reads* what it needs,
  which is the point of having a tool loop. A handler that declared files would list them here.
- **the step id is an integer** — `1`, not a hash. It is the store's row id (S4), so `aeon step show 1` and a
  `WHERE id = 1` are the same identifier.

---

## 2. The transcript survives a restart, and verifies

```
# kill -9 the daemon, then start it again
aeon step list step
aeon step verify 1
```

**You should see** the step still there, and its transcript still byte-identical:

```
1      done       step:ad-hoc              $0.00627 (known)

transcript verified · 5 messages · no thinking signature in this transcript
digest 97b1a051783293c1
```

`no thinking signature in this transcript` is said rather than left blank: a transcript with no signature
verifies, and printing nothing would let a reader believe one had been checked. **S7 recovers a crashed step by
replaying exactly these rows**, so this check is the human-visible form of the thing that has to be true.

And the whole transcript, including the system prompt the step was given:

```
aeon step show 1
```

**You should see** five messages: the SYSTEM prompt with the handoff schema in it, the USER message with the
task first, the assistant's tool call, the tool result, and the final report. The task being **first** in that
message is deliberate (`## Task`, then `## Workspace`, then `## How to work`): a model reads top to bottom, and
a prompt buried under path conventions is a prompt the model weighs less than the conventions.

---

## 3. Tools run one at a time, in order

With a fixture whose turn asks for two reads at once
(`-fixture internal/model/testdata/anthropic/step-two-reads.sse`):

```
aeon step "read the README and the notes and tell me if they agree"
```

**You should see** each tool finish before the next one starts:

```
→ read README.md
← read README.md · 203 bytes

→ read docs/NOTES.md
← read docs/NOTES.md · 22 bytes
```

**It failed if** two `→` lines appear before either `←`. Serial execution (K9) is what makes a transcript
reproducible and what stops two tools writing the same file in an order nobody chose. It is also asserted
mechanically: the tool spy in `internal/runtime` records each call's entry and exit, and the test fails if any
two intervals overlap — a spy that only recorded ORDER would pass against a concurrent implementation.

---

## 4. Four outcomes, and `blocked` is not `failed`

```
aeon step --max-iterations 1 "read every file and summarise each"
```

**You should see:**

```
→ read README.md
← read README.md · 203 bytes

· blocked

step 1 · blocked
  exceeded MaxIterations (1)

  iterations 1
  cost       $0.00170 (known)
```

**Not `failed`.** The work did not fail; it ran out of room. A caller seeing `failed` files a bug report; one
seeing `blocked` raises the budget. The reason **names the bound and its value**, because "blocked" alone sends
somebody reading code to find out which limit it was.

The `failed` case — a model that will not produce a report
(`-fixture internal/model/testdata/anthropic/text-only.sse`):

```
aeon step "say hello and nothing else"
```

```
Hello, world 🌍Hello, world 🌍
· failed

step 1 · failed
  the model did not produce a valid handoff, twice. The second attempt: no JSON object was found in the
  model's final turn: no ```json block, no fenced block containing JSON, and no balanced {…} span

  iterations 2
```

Two things to read there. The text appears **twice** because the loop asked twice — one re-ask, exactly
(K4) — and the exit code is non-zero (`aeon: step 1 failed`) while `blocked` and `reject` exit zero: a
considered no is not a crash.

`reject` needs a model that answers no; with a fixture, a handoff of `{"status":"reject",…}` produces it. Ask a
real model `"verify that 2+2=5 and report whether it holds"`.

---

## 5. The frozen manifest

```
aeon step show 1 --manifest
```

**You should see** what the step was *declared* to have, and the bounds it was given:

```
iterations 2 of 12
deadline   2026-10-16T02:15:23Z

No declared files. This step read what it needed with tools rather than being
handed files at assembly, which is what the ad-hoc handler does.
```

`of 12` is the bound **the step was created with**, stored on the manifest rather than read from today's
default — which is what will let S6 bound a resumed step by what the original was bounded by. Walking this demo
is how that was found to be broken: it printed `iterations 2 of 0`, because the manifest went through a
narrower type on its way to the column and lost the field. The write now stores the bytes it was given.

A handler that declares files lists them here with a hash and a size per file. **Why that matters four slices
before anything resumes** (K23): S6 compares these hashes to tell a resumed step which files changed while it
waited. Freezing is cheap today and impossible to retrofit once callers assume the context is re-read.

And the model calls, reconstructed:

```
aeon step show 1 --requests
```

```
call 1 · 2 message(s) sent · the model asked for read
call 2 · 4 message(s) sent · the model answered with text

2 model call(s), reconstructed from the transcript.
What this cannot show: which tools each call OFFERED. The transcript stores the
conversation, not the request envelope — so a tool-free retry appears here as a
final text answer rather than as a request with no tools.
```

**That last paragraph is the point of the view.** The tool-free retry (K4) is asserted on the request object in
`internal/runtime`'s tests, which is the only place the object exists; a CLI view claiming to show it would be
claiming more than the stored data supports.

---

## 6. `read` cannot leave the workspace

Ask a real model to read `/etc/passwd`, or `../something`, and watch the refusal come back as a tool result the
model can act on:

```
✗ read refused "/etc/passwd": "/etc/passwd" is an absolute path. Paths are relative to the workspace root,
  which is the only thing a step can address
```

Three refusals, each with its own reason: an absolute path, a `..` segment, and **a symlink inside the
workspace that resolves outside it** — the last checked *after* resolution, because before it the path looks
like an ordinary one inside. The same resolver reads a handler's declared files, so a handler cannot escape the
workspace either.

**It failed if** a file outside the workspace reaches the transcript. `internal/tools` asserts this with a
sentinel: a secret outside the workspace, a symlink pointing at it, and a test that fails if the sentinel's
contents appear anywhere in the result.

---

## 7. A command cannot hang, and cannot wait for you

These are properties of the `bash` tool, so they need a model that asks for a command — a real key, or a
fixture carrying the command you want to see. All four are also asserted in `internal/tools`, against real
processes rather than fixtures:

| what | what you see | how it is tested |
|---|---|---|
| a command that never exits | `TIMED OUT after 600s`, with the output it had, marked partial | a real `sleep` killed at a 400 ms bound |
| a command waiting on stdin (`cat`) | returns in milliseconds, exit 0 | `cat`, `read x` and `head -1` all return under a 5 s bound |
| output past the cap | `--- stdout (first 65536 bytes of 195.3 KiB) ---` | a 200,000-byte stream against a 1 KiB cap, asserting the **true** size is reported |
| a killed process | `KILLED (exit -1, signal killed). That is what the kernel's OOM killer looks like, and also what kill -9 from anything else looks like — this layer cannot tell them apart.` | `kill -9 $$`, a genuine SIGKILL, plus `exit 137` for the indistinguishable case |

Two of those deserve a note:

- **65,536 is a guess.** If a real `go test ./...` on a mid-sized module exceeds it, the cap is wrong — and
  `aeon-tools` QAC-02 exists to change that number rather than to pass.
- **The kill message does not diagnose.** `kill -9` from a person, from a supervisor and from the OOM killer
  are the same signal, and claiming "out of memory" would be a diagnosis this layer cannot make.

And the one that is invisible until it matters: a timeout kills the **whole process group**. A command that
backgrounds a server (`./server & sleep 30`) leaves the server holding its port if only the parent is killed.
`internal/tools` proves the group kill with a **freed port** — the test starts a grandchild holding a real TCP
port, lets the timeout fire, and asserts the port becomes bindable; a second test kills only the parent and
asserts the port stays held, because without that control the first test would pass against an implementation
that kills one process.

---

## 8. The step streams while it runs, in the app too

The Step tab in the test bench (`make ui`) runs the same method and watches the same events: the model's text
arriving, each tool starting and finishing, and the outcome at the end.

**You should see** the stream fill in while the step runs, rather than everything appearing at once when it
finishes. That is K19 — events are emitted as they happen, because somebody is watching — and the first walk of
this demo found the CLI subscribing to `step.*` only, which silently dropped `model.delta` and printed nothing
between "started" and "ok".

The tab is also where the grant is a **decision**: `bash` is a checkbox that is off, with the consequence next
to it rather than discovered afterwards.

---

## 9. `--allow-bash`: the skateboard has no brakes

```
aeon step --help
```

**You should see**, on that flag:

```
-allow-bash
    also grant the shell. There is NO command policy at this slice: the command runs as
    given, in the workspace, with the daemon's environment — bounded by a timeout and the
    iteration limit and nothing else
```

And every run that grants it says so on **stderr**, before the step starts:

```
aeon: this step can run shell commands. There is no command policy at this slice: whatever the
model asks for runs, in the workspace, with your environment. It is bounded by a timeout and by
the iteration limit, and by nothing else.
```

**Read that literally.** At S5:

> **Superseded 2026-10-23 (S11, loop `aeon-s11-permissions`).** Everything in the two quoted blocks above was
> S5's help text and stderr warning, quoted as it read then — and both have since been **removed**: S11 shipped
> `command_access` over the measured corpus, so a command is judged before it starts (unruled denies, ruled
> `ask` suspends for a human on an attended loop, allowed spawns under a token bound to its exact string), and
> `--allow-bash` prints nothing on stderr any more. The blocks are kept because they are the record of what S5
> shipped; the walked current surface is [`DEMO.md`](../DEMO.md) §S11.

- **There is no command policy.** No pattern list, no denials, no `ask`, no prompt. `bash` runs what it is
  given. The pattern corpus, the authored `ask` rules and the human prompt are all **S11**.
- **`read` is confined and `bash` is not.** File tools resolve inside the workspace and refuse an absolute
  path, a `..` segment or a symlink escape. A shell has no such boundary.
- **The default step does not grant it.** Without the flag the step has workspace file tools only — which is why the flag
  exists rather than the capability being on.

### The one check this demo most needs to pass

With a fixture whose turn asks for `bash`
(`-fixture internal/model/testdata/anthropic/step-bash-tool.sse`), and **no** `--allow-bash`:

```
aeon step "run: touch PROOF-BASH-RAN"
ls PROOF-BASH-RAN
```

**You should see** the refusal reach the model as a tool result, and **no file**:

```
I will run that.
→ bash touch PROOF-BASH-RAN
✗ there is no tool called "bash" in this step. The tools you have are: read. Th…

ls: cannot access 'PROOF-BASH-RAN': No such file or directory
```

Then the control, which is what makes the check above mean anything:

```
aeon step --allow-bash "run: touch PROOF-BASH-RAN"
ls -l PROOF-BASH-RAN
```

```
→ bash touch PROOF-BASH-RAN
← bash "touch PROOF-BASH-RAN" · exit 0 · 4ms

-rw-rw-r-- 1 … 0 … PROOF-BASH-RAN
```

**It failed if the file exists after the first run.** The allow-list is the only thing standing between a step
and a shell at this stage: a step executing `bash` without the flag is the most serious failure this demo can
find. The refusal is **structural** — `BuildToolset(["read"], …)` constructs no bash tool at all, so there is
no code path from the model's request to a process — and the same proof-by-attempt runs in
`internal/tools` and `internal/daemon` with the same sentinel file.

Note also what the refusal says: **what IS available**. A model told only "unknown tool" tries the same name
again.

---

## 10. `/v1/responses` — reasoning and tools together

Everything above runs a step with reasoning off, because that is the only configuration
`/v1/chat/completions` accepts with tools. This section is the other option from section 0: a route pointed
at OpenAI's second API, which accepts both.

```
go run ./scripts/fixture-server \
  -fixture internal/model/testdata/openai/responses-tool-call.sse \
  -fixture internal/model/testdata/openai/responses-handoff.sse

aeon routes set --id openai --provider openai --model gpt-6.1-sol \
  --openai-endpoint responses --reasoning-effort medium

aeon step "read the README and tell me what is missing"
```

**You should see** the step reason — a reasoning summary line, not signed, because not every provider signs
(S3 permits one deliberately) — and still call a tool, with no lowering note anywhere in the output:

```
step · read the README and tell me what is missing
workspace /tmp/aeon-demo/ws · tools read

· 0 declared file(s), 1 tool(s) offered
Deciding which file to readI will read the README first.
→ read README.md
← read README.md · 203 bytes
The README covers installing and the CLI verbs.

```json
{"status":"ok","summary":"Read README.md on /v1/responses, with reasoning on and tools working."}
```
· ok
```

**It failed if** a "reasoning lowered" note appears anywhere in that output — on this route, nothing needed
lowering, and a note about a decision nobody made would be worse than no note at all.

Two things this section is deliberately honest about:

- **This fixture, and this whole adapter, is UNVERIFIED against a real `/v1/responses` call.** No credential
  was available while writing it (S3's I7: a test must never contact a live provider), so it is built from
  the documented streaming shape rather than a capture. `docs/test_suites/aeon-model.md` QAM-33 names exactly
  what to check on the first real call, in the order most likely to be wrong.
- **`aeon ask` on this same route still works**, unaffected — a chat offers no tools, so the restriction this
  section exists to avoid never applied to it in the first place:

  ```
  aeon ask --route openai "hello"
  ```

One provider, one id, two wire formats picked by `openaiEndpoint` — not two provider ids, because the
credential, the prices and `OPENAI_API_KEY` discovery are all the same thing either way.

---

## What this demo does **not** show

- **No policy on `bash`** — section 9. **S11.**
- **No suspension.** The step cannot ask you anything. `StepResult` has a `suspended` variant that nothing
  produces (K21), and a test asserts that by exhausting every scripted path. S6 fills it.
- **No crash recovery.** `kill -9` the daemon mid-step and that step's row stays `running` forever. The row is
  written **before** the work starts, which is what makes it findable — S7 is what finds it.
- **No compaction.** A step with thirty large tool results will exceed a context window and fail.
  `MaxIterations` and the deadline are the only bounds. S6.
- **Specialized tools missing** — `edit`, `glob`, `grep`, `webfetch`, `todo`, `question`.
  `write_file` now creates or replaces a workspace file; it does not offer a patch or line edit. S6 and S11.
- **No loop.** One step, run once, by hand, filed under a loop named `step`. No blueprint, no phases, no tasks,
  no gates. S8.
- **No agents.** The handler is a literal in the code, not a specialist chosen by tech tag. S13.
- **No cost ceiling.** A step can spend whatever twelve iterations cost. The cost is reported honestly; nothing
  stops it.

**Stage 1 is complete.** The bicycle is next: a step that can ask you something, survive being killed, and be
watched from an app.

---

## The walk, recorded

Walked 2026-10-16 against `scripts/fixture-server` — no credential, no live provider.

| section | walked | how |
|---|---|---|
| 1 · a step reads and answers | ✅ | fixture: `step-read-tool.sse` → `step-handoff.sse` |
| 2 · survives a restart, verifies | ✅ | `kill -9`, restart, `step list` + `step verify` |
| 3 · tools serial, in order | ✅ | fixture: `step-two-reads.sse` |
| 4 · blocked ≠ failed | ✅ | `--max-iterations 1`; and `text-only.sse` for `failed` |
| 4 · reject | ⬜ | needs a model, or a fixture whose handoff says `reject` |
| 5 · manifest and requests | ✅ | `step show --manifest`, `--requests` |
| 6 · `read` confined | ⬜ as a demo | needs a model that asks for a bad path; covered by `internal/tools` with a sentinel file |
| 7 · timeout, stdin, caps, kills | ⬜ as a demo | needs a model that asks for a command; covered by `internal/tools` against real processes |
| 8 · the app streams | ✅ | the Step tab's tests; the live tab needs a daemon and a provider |
| 9 · ungranted `bash` cannot run | ✅ | fixture: `step-bash-tool.sse`, with and without `--allow-bash` |
| 10 · `/v1/responses`, tools + reasoning | ✅ | fixture: `responses-tool-call.sse` → `responses-handoff.sse`; **adapter UNVERIFIED against a real call** |

**Nine findings from the first walk, all fixed in this slice:**

1. `step` subscribed to `step.*` only, so `model.delta` never reached the client — a step that worked and
   printed nothing between "started" and "ok".
2. The frozen manifest lost `maxIterations` and every file's `bytes` on the way to the column, because the
   adapter unmarshalled it into the store's narrower view and let the store re-encode. `step show --manifest`
   printed `iterations 2 of 0`.
3. The same loss existed on the READ path, for the same reason.
4. `step.tool.start`'s summary repeated the tool's name, rendering `→ read read README.md`.
5. `step.started` said `0 context section(s)` for a step that had a prompt — it counts declared FILES, and the
   wording now says so.
6. `aeon step --help` printed the flags and then exited 1 with `flag: help requested`.
7. `--allow-bash`'s flag description said only "grant the shell tool", with the consequence nowhere a person
   reads before using it.
8. `iterations 2 of the bound` named no number.
9. The demo text itself claimed `step show --requests` shows the retry's empty tool list, which the stored
   transcript cannot support; it claimed a `--handoff-schema` flag that does not exist; it used hash-shaped
   step ids; and it quoted output formats that no code produced.

**Five more, from actually running a step against an OpenAI reasoning model after the slice was reported
done** — see `STATUS.md` for the full account:

10. No `reasoning_effort` was ever sent, so the provider's own default applied and a model that refuses tools
    with reasoning on refused every step. Fixed with `Route.ReasoningEffort`, validated and never defaulted.
11. The command that error suggested would have **cleared the route's base URL** — `routes.set` was a full
    replace. Fixed: it now sets what you name and keeps what you do not.
12. The effort needed to differ between a chat and a step on the SAME route. Fixed with a per-call override
    (`ModelRequest.ReasoningEffort`) and a step that lowers a route's effort by itself, saying so on
    `step.started` — and the tool-free retry carrying the SAME effort as the call it retries, which an
    earlier version dropped.
13. The lowering rule fired for Anthropic, which has no such restriction, and would have fired for a
    `/v1/responses` route, which does not need it — caught by its own first test run. Fixed by asking the
    route (`ToolsRefuseReasoning()`) rather than deciding in the loop.
14. `/v1/chat/completions` cannot do tools-with-reasoning at all, at any setting. Fixed with a second OpenAI
    adapter for `/v1/responses`, selected per route (`openaiEndpoint`) behind one provider id.
15. Both the Ask and Step tabs subscribed on every call and never released it. The daemon fans out to every
    matching subscription, so four calls meant every event delivered four times. Fixed: one subscription per
    page lifetime, caught by a test that makes three calls and asserts exactly one `subscribe`.

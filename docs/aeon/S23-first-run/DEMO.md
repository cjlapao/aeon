# S23 First run & polish — the demo

**Stage 4 · Space shuttle, fourth of four. The last slice.**

**This demo is a measured test on a person**, and it is the only one in the plan that a machine cannot run.

You need: a **clean machine** (or a fresh container), **one API key**, a **stopwatch**, and **a person who has not read the
design.**

---

## 1. The five minutes

**Read this section before starting, then do not speak during it.**

Hand the tester:

- A clean machine with the binary available and nothing configured.
- One API key.
- The repository's `README.md`, or its URL.
- Nothing else. **No instructions, no hints, no "you probably want to…".**

Ask them to get a loop running against a small repository. **Start the stopwatch.**

**Record, as they go:**

- Where they hesitated, and for how long.
- Anything they typed that did not work.
- Anything they asked you. **Every question is a failure of the previous screen**, and the question is more valuable than
  the answer.
- The time on the clock when a loop is running.

**Under five minutes is a pass.**

**And the tester must not have read the design** (C14). If the only person available has, **the measurement is worthless** —
they know which flag exists, and their five minutes are not a stranger's. **This cannot be self-certified.** Every author of
every onboarding flow believes theirs is clear.

**If it takes longer, the finding is which question cost the time**, not that the target was optimistic.

For reference, the path should be roughly:

```
aeon init                     # picks a provider, pastes a key, probes it
cd ~/some-repo
aeon loop start "…"           # or: aeon step "…"
```

**Three commands.** Anything the tester had to do beyond that is time this slice spent badly.

---

## 2. `init` asks about providers, not wire formats

```
aeon init
```

**You should see it ask a question a stranger can answer:**

```
Which provider?
  1) Anthropic
  2) OpenAI
  3) Something OpenAI-compatible  (Ollama, vLLM, LiteLLM, OpenRouter)
```

**Not** *"what is your base URL, and does the server support `stream_options.include_usage`?"*

C6's reason: **a stranger picks a provider, not a wire format.** The third option covers four real things, and a preset is
what turns one choice into a route with capability declarations, a price, and a model.

Then paste a key when asked, and watch what happens next:

```
  probing route 'anthropic'…
    tools             verified
    streaming-usage   verified
    thinking          verified

  ready. try:  aeon step "summarise the Go files under internal"
```

**It probed what it configured** (C18). A preset is a **claim** about a server's capabilities, and the first run is when
verifying it costs nothing — a contradiction found three steps into a loop costs a confused user instead.

Now the same thing scripted, because a wizard nobody can automate is a wizard nobody uses in CI:

```
aeon init --provider anthropic --model claude-… --non-interactive
```

**Every prompt has a flag** (C5), and the whole flow runs **with no TTY** — over SSH, in a container, in CI.

---

## 3. A key in a config file is refused

Try to be helpful to yourself:

```
echo 'anthropic_api_key: sk-ant-...' >> ~/.config/aeon/config.yaml
aeon step "hello"
```

**You should see a refusal, not a warning:**

```
error: credential in configuration file

  what:  'anthropic_api_key' is set in ~/.config/aeon/config.yaml
  why:   a config file gets committed. Aeon reads credentials only from the
         environment or a dedicated key file
  do:    remove that line, then either
           export ANTHROPIC_API_KEY=...
         or
           aeon init --key-file ~/.config/aeon/anthropic.key
```

**A load-time error, not a warning** (C4). C4's reasoning is one sentence and it is enough: **a config file gets
committed.** A warning is something people learn to scroll past, and the failure is permanent and public.

Then try the other route:

```
aeon step --api-key sk-ant-... "hello"
→ error: no such flag
```

**Never in `argv`** (C17) — it is in the process list and in shell history, and that failure is silent.

**One rule across the whole product**: the environment, or a dedicated file path. Never config, never `argv`, never a log.

---

## 4. Every error says what to do

Break things deliberately and read what comes back.

```
aeon loop start --blueprint nonexistent "…"
```

```
error: no such blueprint

  what:  blueprint 'nonexistent' was not found
  why:   blueprints ship under <install>/blueprints/standard/ and are
         overridable per project in ./aeon/blueprints/
  do:    aeon blueprint list        # what is available
         aeon blueprint path dev-loop
```

**Three parts, always** (C9, C16): what happened, what it means, what to do.

**And the third part is structurally required** — an error missing `Do` **does not compile.** C9's audience *"cannot ask the
author"*, and the field that gets dropped in practice is always the third one, so a convention was never going to hold it.

Try a few more and confirm all three parts every time: a malformed blueprint, an unreachable daemon, a route with no price,
a step needing a capability its route lacks.

---

## 5. `doctor` tells the truth about a half-configured system

**Deliberately half-configure it**: one working route, one route whose preset over-declares, no embedding route, no HTTP, no
forge token.

```
aeon doctor
```

**You should see every uncertain thing in its own state:**

```
  routes
    anthropic   tools verified · streaming-usage verified
                price $3.00/$15.00 per million        cost: known
    local       tools CONTRADICTED  (probed today: no tool_calls support)
                unmetered — cost will report 'unknown', not $0.00

  credentials   ANTHROPIC_API_KEY present   (environment)
                forge token       MISSING
                                            no credential is printed here

  retrieval     no-provider     keyword-only until an embedding route is added

  narrow surface
    mcp-stdio   available
    http        NOT LISTENING   (no --http address given)

  forge         github          token MISSING — S16 actions will refuse
```

**Nothing on that screen is summarised** (C15). Read it and check each one:

- `CONTRADICTED` is **not** "check your config".
- `unverified` would **not** be "ok".
- `unmetered` is **not** `$0.00` — it says what the cost will report.
- `no-provider` is **not** "retrieval unavailable" — it names which of four reasons.
- `NOT LISTENING` is **not** absent from the screen.

**This is where the whole product's honesty either holds or is thrown away.** Six independent slices refused to collapse an
uncertain answer into a certain one — cost, the `bash` receipt, blueprint validation, connection liveness, verification, and
provider capabilities — and `doctor` is the one screen reporting all of them.

**It failed if any line reads "ok" or "all good" over something uncertain.** That would look helpful while undoing six
slices of design work at the last possible moment.

**And no credential appears anywhere on that screen** — presence, never value.

---

## 6. Nothing leaves the machine

The product reads your source code, runs commands, and holds a forge token. So prove it:

```
sudo tcpdump -i any -n 'not port 22' -w /tmp/aeon.pcap &
aeon step "summarise the Go files under internal"
aeon loop start --blueprint two-step "survey internal"
# answer the gate, let it finish
kill %1
tcpdump -r /tmp/aeon.pcap -n | awk '{print $3, $5}' | sort -u
```

**You should see connections to your configured provider, and to nothing else.**

No analytics host. No crash reporter. No version check. **No telemetry** (C11, C19) — not opt-in, not anonymised, not for
crash reports.

C11's reason is the whole basis of the product: **trust is why anyone installs a thing that reads their code and runs their
commands.** And a claim like that is worth nothing unstated, which is why it is a test rather than a sentence in a README.

---

## 7. The shipped blueprint is a default, not an imposition

```
aeon blueprint list
```

```
  dev-loop     (shipped)
  two-step     (shipped)
```

Now override one:

```
mkdir -p aeon/blueprints && cp "$(aeon blueprint path dev-loop)" aeon/blueprints/dev-loop.yaml
# edit it — remove the security lane, say
aeon blueprint list
```

```
  dev-loop     (this project)   ← overrides the shipped one
  two-step     (shipped)
```

**"We ship a standard one but we do not impose", literally** (C12, P18).

That is the whole reason the graph is data (L1): there are **five** protocol shapes in this repository, and freezing one into
code would have made the other four second-class.

---

## What this demo does **not** show

- **No auto-update.** A tool that reads source code and runs commands does not update itself without being asked.
- **No cloud account, no login.** There is nothing to sign into.
- **No GUI installer.** A binary and a `PATH`.
- **No package-manager submissions.** Homebrew, apt and winget are distribution work, and they belong **after** the first
  five minutes are known to work.
- **No Gemini preset.** Gemini is v1.1 (S18's N1), and a preset for an adapter that does not exist is a preset that fails.
- **No dogfooding.** S17 deferred running the ladder on Aeon's own repository to *"after S23's first-run work"* — so it is
  **the next thing**, not this one.

---

## The shuttle, and the whole thing

Seventeen slices. Five stages. A claim that changed shape at each one:

| Stage | The claim |
| --- | --- |
| **Garage** | A module builds, a daemon starts, an app opens |
| **Skateboard** | One model step runs against a repository, with its cost known or honestly unknown |
| **Bicycle** | It asks, survives being killed, follows a blueprint, and does all of it from a window |
| **Car** | It builds real software, opens real pull requests, and **catches itself being wrong** |
| **Space shuttle** | Any provider, any client, memory if you want it — and a stranger in five minutes |

**The thread is narrower than the feature list suggests.** Six times, in six unrelated slices, the design refused to collapse
an uncertain answer into a certain one: a cost it did not know, a `bash` command that might have run, a blueprint it could
not fully check, a connection it could not confirm, a linter that never ran, a capability it had not probed.

Every one of those was cheaper to get wrong. **§5 is where they either hold or are quietly thrown away** — and that is why
`doctor` is the last thing this plan specifies.

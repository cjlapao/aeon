# Design — S23 First run & polish (Stage 4 · Space shuttle)

**Date:** 2026-09-07
**Status:** approved (design); not yet implemented
**Stage:** 4 · Space shuttle, fourth of four — **the last slice**
**Depends on:** everything. **Independent of S18, S19, S22** as work, but reports on all three.
**Inherits:** C1–C13 from [`docs/_archive/aeon-v1-horizontal/13-cli`](../../_archive/aeon-v1-horizontal/13-cli/design.md). New decisions start at **C14**.

---

## Context

Sixteen slices built a product. **This one is about the first five minutes**, and its success criterion is unlike every
other slice's:

> *A stranger on a clean machine, holding one API key, reaches a running loop in **under five minutes** — timed, by someone
> who has not read the design.*

That is a stopwatch and a person, and **it can fail for reasons no assertion catches.** A test can prove `aeon init`
completes; only a stranger can prove it was obvious what to type next.

### Five minutes is a budget, and everything spends from it

Reading the README, installing, running `init`, choosing a provider, pasting a key, starting a loop. **Every question asked
is time taken**, and the slice's job is to ask as few as possible while asking the ones that matter.

So the ordering that matters is not the code's — it is **what a stranger hits first**: README, install, `init`, first loop,
and `doctor` when something is wrong.

### The measurement cannot be self-certified — and the QA suite already said so, better

**C14 is not new.** `aeon-cli.md`'s QAC2-01 already required *"a tester who has not read the design documents"*, and it
states the rest more sharply than this design first did:

> ***"The list of stumbles is the real output** — a passing time with three stumbles is more useful than a passing time
> alone, because the stumbles are what the next hundred users hit. A twenty-minute result is not a slow tester; **it is a
> list of places the product failed to explain itself.**"*
>
> *"Automatable: no, **and it must not be.**"*

Three things there are better than the wording this design reached for. *"A list of places the product failed to explain
itself"* is the correct reading of a slow run — not that the target was optimistic. *"The stumbles are the real output"* is
why a pass gets recorded too. And *"it must not be"* automated is the reason no test may claim to cover D1: a test that does
has **replaced** the measurement rather than supported it.

**So C14 restates the suite's requirement rather than introducing it**, and adds one thing the suite has that this design
lacked: **repeat for each of the three providers, ideally with a different tester each time** (C21). One tester on one
provider is one data point, and the three presets have different friction — the OpenAI-compatible path asks a question the
other two do not.

### `doctor` is where the product's honesty converges

By now **six independent places** refuse to collapse an uncertain answer into a certain one
([P31](../ARCHITECTURE.md#8-product-decisions)): cost, the `bash` receipt, blueprint validation, connection liveness,
verification, and provider capabilities. Retrieval adds four availability reasons on top.

**`doctor` is the one screen that reports all of them**, and it is therefore the single place where the whole pattern can be
undone — by summarising into a green "ok".

**C15**: `doctor` reports every three-state answer **in its own three states**, and never flattens one. A `doctor` that says
"everything looks good" over an unverified capability and an unmetered route has thrown away six slices of design work at
the last possible moment, and it would look helpful doing it.

### Two inherited decisions are wrong now

**C7** — *"`aeon serve` starts the server **and** serves the UI on one URL. U1. One process, one port, no CORS."*

**U1 is superseded.** S1's T5 established that a browser cannot speak a unix socket, so the Tauri shell **is** the UI rather
than packaging around a served page. `aeon serve` starts a daemon on a socket; the app is a separate process; there is no
port and nothing is served.

**C8** — *"`aeon run` is the same runner the server spawns."*

**P29 removed the spawned runner.** The daemon owns loop execution; there is no detached process per loop. C8's *principle*
holds and matters — one code path, because *"a CLI-only path would diverge from the supervised one and only one would be
tested"* — but its mechanism is gone. The CLI and the app both talk to the same daemon over the same socket, which is a
stronger version of the same guarantee.

**And C1's "one published package"** is now one Go binary for daemon and CLI (P30), which was always the point: *"nobody
installs three things and wires them together."*

## Goal

A stranger with one API key reaching a running loop in under five minutes, and a `doctor` that tells the truth about every
optional part.

## Decisions

C1–C13 are inherited, with C7, C8 and C1's mechanism corrected (C20). New:

| # | Decision | Why |
| --- | --- | --- |
| C14 | **The five-minute measurement requires a tester who has not read the design.** Timed, recorded, and **not self-certifiable** — **restating `aeon-cli.md`'s QAC2-01**, which said it first and better | Every author of every onboarding flow believes theirs is clear. A tester who knows which flag exists is not measuring a stranger's five minutes. And a slow run is *"a list of places the product failed to explain itself"*, not a slow tester |
| C21 | **The measurement is repeated per provider, ideally with a different tester each time** | Adopted from QAC2-01. One tester on one provider is one data point, and the OpenAI-compatible path asks a question the other two do not |
| C15 | **`doctor` reports every three-state answer in its own three states**, and never summarises into "ok" | It is the one screen reporting all six of P31's instances plus retrieval's four reasons. Flattening them undoes six slices at the last moment, and looks helpful doing it |
| C16 | **The three-part error shape is structural**: a type with three **required** fields — what happened, what it means, what to do | C9 says *"the audience cannot ask the author"*, and a convention that errors should have three parts produces errors with two |
| C17 | **One credential rule across the product**: the environment, or a dedicated file path — **never a config file, never `argv`, never a log.** `doctor` reports which are present without printing any | C4's reason is that *"a config file gets committed"*; S19's O1 adds that a command-line value is in the process list and shell history. One rule beats three consistent ones |
| C18 | **`aeon init` probes what it configured**, and reports the result | A preset is a **claim** about a server's capabilities (S18's M28). The first run is when verifying it costs nothing, and a contradiction found later costs a confused loop |
| C19 | **No telemetry, asserted by test**: the binary makes no network call except to configured routes and forges | C11's *"trust is the whole basis of installing it"* is worth nothing unstated. The product reads source code, runs commands and holds forge tokens |
| C20 | **C7 and C8 are superseded**; C1's mechanism is a Go binary | U1 by S9, the spawned runner by P29, npm by P30. Their principles survive; their mechanisms do not |

## Architecture

```
aeon/internal/config/
  layers.go     defaults → user file → project file → environment → flags (C2)
  validate.go   names the offending key AND file (C3)
  creds.go      one credential rule (C4, C17)

aeon/internal/errs/
  errs.go       the three-part shape, structurally required (C16)

aeon/internal/presets/
  presets.go    Anthropic, OpenAI, OpenAI-compatible (C6)

aeon/cmd/aeon/
  init.go       interactive, every prompt with a flag (C5), probing (C18)
  doctor.go     every optional part, in its own states (C13, C15)

aeon/blueprints/standard/   shipped, overridable per project (C12)
README.md                   the first thing a stranger reads
```

### `doctor`, in full

The shape matters more than the content, because the content is what six slices decided:

```
aeon doctor

  daemon        running        socket /run/user/1000/aeon.sock  (0600)
  version       0.1.0

  routes
    anthropic   tools verified · streaming-usage verified · thinking declared
                price $3.00/$15.00 per million        cost: known
    local       tools CONTRADICTED (probed 2026-09-07: no tool_calls support)
                unmetered — cost will report 'unknown', not $0.00

  credentials   ANTHROPIC_API_KEY present   (from environment)
                forge token       present   (from ~/.config/aeon/forge.key)
                                            no credential is printed here

  bash policy   38 agent files · 366 rules · 50 patterns
                7 ask rules · 3 patterns with conflicting verdicts

  retrieval     available      code corpus current · history corpus stale (38 files)
                embedding route: openai-small

  narrow surface
    mcp-stdio   available
    http        NOT LISTENING     (no --http address given)

  forge         github         token present · scopes unverified
```

**Nothing on that screen is summarised.** `CONTRADICTED` is not "check your config"; `unverified` is not "ok"; `unmetered`
is not `$0.00`; `stale` is not "available". Each is the state its slice decided it should be.

### The error shape (C16)

```go
type Error struct {
    What string   // required — what happened
    Why  string   // required — what it means
    Do   string   // required — what to do
}
```

**All three required**, so an error missing `Do` fails to construct. C9's audience *"cannot ask the author"*, and the field
that gets dropped is always the third one.

## The demo

See [`DEMO.md`](DEMO.md): a stranger, a clean machine, one key, a stopwatch. Plus `doctor` telling the truth about a
half-configured system; an error that says what to do; a key in a config file **refused**; and a network trace proving
nothing leaves the machine.

## Testing

| What | How |
| --- | --- |
| **The five minutes** | A **timed run by someone who has not read the design** (C14). Recorded: where they hesitated, what they typed that did not work, what they asked. **Not self-certifiable** |
| Config layering | Each layer overrides the one before; a flag wins for one run (C2) |
| Validation names both | The offending **key and file**, not "invalid configuration" (C3) |
| A key in config is refused | A **load-time error**, not a warning (C4, C17) |
| A key in `argv` is refused | (C17, S19's O1) |
| No credential is printed | `doctor` and every error path, grepped (C17) |
| `init` is scriptable | **Every** prompt has a flag; the whole flow runs with no TTY (C5) |
| `init` probes | And reports `verified` or `CONTRADICTED` before finishing (C18) |
| Three presets | Anthropic, OpenAI, OpenAI-compatible — the third covering Ollama, vLLM, LiteLLM, OpenRouter (C6) |
| **`doctor` flattens nothing** | Every three-state answer rendered in its own states; the literal strings "ok" and "all good" appear over **no** uncertain value (C15) |
| Errors have three parts | Asserted **structurally** — an error missing `Do` does not compile (C16) |
| Exit codes | `failed`, `blocked` and `waiting` are distinguishable, and documented (C10) |
| **No telemetry** | A network trace over a full loop: **no connection except to configured routes and forges** (C19) |
| Blueprints overridable | A project's own blueprint takes precedence over the shipped one (C12) |
| C7 and C8 are gone | No served UI, no port from `serve`, no spawned runner (C20) |

**The first row is the slice**, and it is the only test in the whole plan that a person passes or fails rather than a
machine. The rest exist so that it can.

## Out of scope

- **No telemetry, ever** (C11, C19). Not opt-in, not anonymised, not for crash reports.
- **No auto-update.** A tool that reads source code and runs commands does not update itself without being asked.
- **No cloud account, no login.** There is nothing to sign into.
- **No GUI installer.** A binary and a `PATH`.
- **No package-manager submissions** in this slice — Homebrew, apt, winget are distribution work and belong after the first
  five minutes are known to work.
- **No Gemini preset.** S18's N1: Gemini is v1.1, and a preset for an adapter that does not exist is a preset that fails.
- **No dogfooding.** S17's N4 deferred running the ladder on Aeon's own repository to *"after S23's first-run work"* — so it
  is the **next** thing, not this thing.

## Amendments to earlier slices

| Where | Change | Status |
| --- | --- | --- |
| `docs/test_suites/aeon-cli.md` | **QAC2-01 already contained C14**, and stated it better — *"the list of stumbles is the real output"*, *"a twenty-minute result is… a list of places the product failed to explain itself"*, *"automatable: no, and it must not be"*. C14 restates it; **C21 adopts its per-provider repetition** | **landed with this design** |
| `docs/test_suites/aeon-runcontrol.md` | **QAC-04's expectation is inverted by P29.** It asserts a loop keeps advancing when the control plane stops, *"the reason for a detached child rather than an in-process task"* — and P29 removed the detached child: **a daemon crash stops every loop**, recovered on restart (§3a). QAC-02, 03, 05 and 06 are superseded in mechanism, their concerns redirected to S7 | **landed with this design** |
| `docs/_archive/aeon-v1-horizontal/13-cli/design.md` | **C7 and C8 are superseded** — U1 by S9's Tauri shell, the spawned runner by P29's daemon-owned execution. C1's npm package is a Go binary (P30). Their principles survive | **landed with this design** |
| S18 | Its hand-written routes gain presets; its capability declarations are probed at `init` (C6, C18) | T4 |
| S19 | Its HTTP bind is reported by `doctor` (its O8) | T5 |
| S22 | Its four availability reasons and per-corpus staleness are reported by `doctor` | T5 |

## What the plan amounts to

Seventeen slices, five stages, and a claim that changed shape at each one:

| Stage | The claim |
| --- | --- |
| **Garage** | A module builds, a daemon starts, an app opens |
| **Skateboard** | One model step runs against a repository, with its cost known or honestly unknown |
| **Bicycle** | It asks, survives being killed, follows a blueprint, and does all of it from a window |
| **Car** | It builds real software, opens real pull requests, and **catches itself being wrong** |
| **Space shuttle** | Any provider, any client, memory if you want it — and a stranger in five minutes |

The thread through all of it is narrower than the feature list suggests. **Six times, in six unrelated slices, the design
refused to collapse an uncertain answer into a certain one** — and `doctor` is where that either holds or is thrown away.

## Open questions

None. The five-minute figure is a target the measurement either meets or does not; if it does not, the finding is which
question cost the time.

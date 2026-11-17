# Design — S5 One step, end to end (Stage 1 · Skateboard)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 1 · Skateboard — **this slice completes it**
**Depends on:** S1, S2, S3, S4.
**Converges:** [`docs/_archive/aeon-v1-horizontal/03-step-runtime/`](../../_archive/aeon-v1-horizontal/03-step-runtime/) (K-series) and [`docs/_archive/aeon-v1-horizontal/05-tools/`](../../_archive/aeon-v1-horizontal/05-tools/) (W-series).

> Decision ids continue two inherited series: **K** for the runtime, **W** for tools. New runtime decisions
> start at **K21**, new tool decisions at **W11**.

---

## Context

**The skateboard.** A model that can read a file, run a command, and produce a bounded structured report — with
the transcript in a store you can replay afterwards. Not half a bicycle: a smaller thing that gets somewhere.

Two superseded folders converge here for the first time. `docs/_archive/aeon-v1-horizontal/03-step-runtime` established K1–K20 —
declared context, the bounded tool loop, the four outcomes, the handoff with exactly one tool-free retry — and
`docs/_archive/aeon-v1-horizontal/05-tools` established the W-series, including the bounded-command discipline that turns three lines
of protocol prose into three properties of a tool.

**And this slice ships a deliberately incomplete version of both, which needs saying plainly rather than
discovering.**

### The skateboard has no brakes

`bash` lands here **with bounds but without a policy.** The 50-pattern policy evaluator, the `ask` verdict and
the human prompt are all S11. So a step that is granted `bash` at S5 can run anything the user can.

Three things make that acceptable *for a skateboard*, and none of them makes it acceptable to ship:

- **`bash` is not granted by default.** The tool list is an allow-list (inherited W/G decisions), and the
  default step declares only `read`. Granting it takes an explicit `--allow-bash`, whose help text says what it
  means.
- **The bounds land now** (W13): a mandatory timeout, stdin closed, output caps, and `oomKilled` reported as a
  fact. Those are about a step not hanging, not about permission — and a hung skateboard is as useless as an
  unsafe one.
- **The demo says so**, in a section that names S11 as where the brakes are fitted.

The alternative — deferring `bash` to S11 — was considered and rejected: a step that cannot run anything cannot
demonstrate the tool loop, the serial-execution guarantee, or the output caps, and those three are most of what
the runtime is. Building the loop against one read-only tool would mean discovering its shape at S11 with a
policy layer as a confounding variable.

## Goal

`aeon step "summarise README.md and tell me what is missing"` assembles a declared context, runs a bounded tool
loop against a real provider, returns one of four outcomes with a validated handoff, persists the transcript,
and replays from the store afterwards.

## Inherited decisions

| # | Status | Note |
| --- | --- | --- |
| K1 | **holds** | Code owns transitions; the model owns the work inside a step |
| K3 | **holds** | Every step is a fresh context plus one bounded handoff. Nothing accumulates across steps |
| K4 | **holds** | The handoff is requested as JSON, validated, and re-asked **exactly once**, tool-free |
| K5 | **holds** | `RunStep` is a function over persisted state, not a run handle |
| K6 | **holds** | The runtime **persists nothing**; the caller does (K22 names the caller) |
| K9 | **holds** | Tools execute serially, in the order the model emitted them |
| K13 | **holds** | The declared-context manifest is frozen at assembly, with a content hash per file |
| K19 | **holds** | The event sink is synchronous and lossless; `RunStep` returns exactly once |
| K20 | **holds** | Programmer errors panic; step failures are returned |
| K11, K12, K14–K18 | **deferred** | Suspension is S6; recovery and re-run classes are S7; compaction is S6 |
| W1 | **narrowed by W11** | Nine tools eventually; **two** here |
| W2 | **holds** | Paths are workspace-relative and enforced by one resolver |
| W3–W5 | **hold** — this is W13 | Mandatory timeout, stdin closed, `oomKilled` reported |
| W7 | **deferred** | `evaluateBash` and the enforcement point are **S11** |
| W8 | **deferred** | `todo` is not among the two |

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| K21 | **`StepResult` ships as a tagged union now**, with a `suspended` variant present but **never produced**, and a test asserting so | S6 fills the variant in rather than reshaping a struct every caller switches on. Shipping the shape costs a field; retrofitting it costs a mechanical sweep through code written in between |
| K22 | **The `step` protocol handler is the caller that persists.** `RunStep` returns a transcript; the handler writes it through S4 | K6 says the runtime persists nothing, and this names who does. Keeping it out of the runtime is what lets S7 recover by calling the same function with rows it read itself |
| K23 | **The manifest is frozen at assembly even though nothing resumes yet** | S4 already has the column, freezing is cheap now, and retrofitting a freeze after callers have assumed re-resolution is not. It also makes `aeon step show` able to display what the step was given |
| K24 | **`step` streams through S2's subscriptions**, exactly as `ask` does | One streaming path for the whole product. A second would need its own filter, cursor and teardown |
| W11 | **Two tools: `read` and `bash`.** Named as a deliberate subset, not an implied one | The loop, serial execution and the output caps need something that reads and something that runs. The other seven arrive at S11 with the policy that governs them |
| W12 | **`bash` has bounds but no policy, and is not granted by default.** `aeon step --allow-bash` grants it | The policy evaluator is S11. An allow-list default of `read` only means the unguarded capability requires a deliberate act, and the flag's help text says what that act is |
| W13 | **The bounded-command discipline lands now**: mandatory timeout, stdin closed, output caps with truncation reported, `oomKilled` distinguishable from a non-zero exit | These are about a step not hanging rather than about permission, and they are the properties that turn protocol prose into tool behaviour. A hung skateboard is as useless as an unsafe one |
| W14 | **A tool result is a string with an `IsError` flag**, matching S3's `tool_result` block | The model must be able to tell a tool that failed from one that returned the text "error" |

## Architecture

```
aeon/internal/runtime/
  types.go       StepRequest, StepResult (union — K21), StepHandler, ContextSection, Handoff, StepEvent
  context.go     assemble sections, freeze the manifest, hash declared files (K13, K23)
  compile.go     CompileRequest — handler + input → StepRequest
  transcript.go  canonical messages from a StepRequest
  handoff.go     extract, validate, the one tool-free retry (K4)
  loop.go        the bounded loop; serial execution (K9); the four outcomes
  runstep.go     RunStep — the entry point (K5, K6)

aeon/internal/tools/
  paths.go       the one resolver (W2)
  limits.go      timeouts, output caps — all guesses, stated as such
  read.go        the `read` tool
  bash.go        the `bash` tool — bounds, no policy (W12, W13)
  toolset.go     BuildToolset over an allow-list

aeon/internal/protocol/
  step.go        the `step` method: calls RunStep, persists via S4 (K22), streams via S2 (K24)
```

`runtime` imports `model` and `store` types. `tools` imports `runtime` for `ToolContext`. Neither imports the
other's internals.

### The bounded loop

```
assemble context, freeze manifest
        │
        ▼
   ┌── ModelClient.Stream ──┬──▶ deltas ──▶ event sink (K19) ──▶ S2 subscription (K24)
   │                        │
   │                 tool_use blocks?
   │                   │ no          │ yes
   │                   ▼             ▼
   │            extract handoff   execute SERIALLY (K9), append tool_result
   │                   │             │
   │             ok ───┤ invalid     └──▶ next iteration
   │                   ▼
   │            retry once, TOOL-FREE (K4)
   │                   │
   └──── return complete ◀──┘        bounded by MaxIterations and DeadlineAt
```

**Four outcomes** (inherited): `ok`, `reject`, `blocked`, `failed`. Exceeding a bound yields **`blocked`, not
`failed`** — the work did not fail, it ran out of room, and a caller seeing `failed` reports a defect while one
seeing `blocked` raises the budget.

**The retry is tool-free, and that is the load-bearing half.** One retry bounds the tail; tool-free means a
retry **cannot start new work**. A step that already did its job could otherwise do it again, differently, while
ostensibly just restating its report. So the assertion is not "the retry happened once" but "the retry request
carried no tools".

### `bash`, and exactly what it does and does not do

**Does** (W13): runs with a mandatory timeout; closes stdin so a command waiting on input fails promptly rather
than blocking; caps stdout and stderr and **reports truncation with the true size**; distinguishes
`oomKilled` from an ordinary non-zero exit; terminates the whole process group and escalates `SIGTERM` to
`SIGKILL` after a grace period; honours the step's `context`.

**Does not** (W12): consult any policy. There is no pattern list, no `ask`, no prompt. `bash` at S5 runs what
it is given.

Which is why it is not in the default tool list. `aeon step` grants `read`; `aeon step --allow-bash` grants
both, and the flag's help says:

> `--allow-bash`  Grant the step an unrestricted shell. There is no command policy until S11 — the model can
> run anything you can. Use it on a repository you can throw away.

### `aeon step`, and the shape of a demo

```
→ subscribe {"filter":{"kinds":["step.*"]}}     ← {"subscriptionId":"s-1","from":412}
→ step {"prompt":"…","tools":["read"],"subscriptionId":"s-1"}
                                                 ← {"stepId":"4f2a1c"}
← event step.started      seq 413
← event step.delta        seq 414  "Looking at the README…"
← event step.tool.start   seq 415  read README.md
← event step.tool.end     seq 416  read README.md · 2.1 KiB
← event step.delta        seq 417  "It covers install but not…"
← event step.done         seq 418  outcome=ok · $0.0021 (known)
```

Then the transcript is in the store, and `aeon step show 4f2a1c` replays it.

## The demo

See [`DEMO.md`](DEMO.md): a step reads a file and answers; the transcript replays after a daemon restart; tools
run serially; a hung command is bounded; output truncation is reported; the four outcomes are each producible;
and `--allow-bash` says what it grants.

## Testing

**No unit test contacts a provider.** `ModelClient` is injected, and this slice's fake replays canonical deltas
— one level above S3's SSE fixtures.

| What | How |
| --- | --- |
| Four outcomes | One test each, driven by a scripted fake client |
| Serial execution | An interleaving spy that would **detect** concurrency — entry and exit timestamps, asserting no overlap |
| Handoff retry | Invalid JSON retries exactly once, and the retry request carries **no tools**; a second failure yields `failed` with the transcript intact |
| Bounds | Exceeding `MaxIterations` and exceeding `DeadlineAt` each yield `blocked` naming which |
| Manifest frozen | Assembly hashes every declared file; a second call does not re-read |
| Missing declared file | A construction error, **not** an empty section |
| `read` confinement | Absolute, `..`, symlink escape, empty — each refused **by attempt** |
| `bash` cannot hang | A command that never exits returns `timedOut` with partial output |
| `bash` cannot wait on input | A command reading stdin fails promptly |
| `oomKilled` | A SIGKILLed process reports it `true`; an ordinary failure `false`; both distinguishable |
| Truncation | Reported with the true size, never silent |
| Process group | A command spawning children is fully terminated — proven by a freed port |
| Allow-list | A step not granted `bash` cannot call it, proven by attempt |
| `suspended` never produced | A test asserts no code path returns it yet (K21) |
| Persistence | The handler writes; `RunStep` writes nothing, asserted with a store spy at zero |
| Cold replay | A child process reads the transcript back byte-identically |

## Out of scope

- **Suspension and the `question` tool.** S6. K11, K12 and K14–K18 are deferred, and `StepResult`'s
  `suspended` variant exists unused (K21).
- **Crash recovery, re-run classes, receipts.** S7.
- **Compaction.** S6. `MaxIterations` bounds the loop instead.
- **The bash policy, the `ask` verdict, the human prompt.** **S11.** This is the slice's largest gap and the
  demo names it.
- **Seven of the nine tools** — `write`, `edit`, `glob`, `grep`, `webfetch`, `todo`, `question`. S6 and S11.
- **Blueprints, phases, tasks, gates.** S8 onward. A step is not a loop.
- **Agents.** S13. The handler here is a literal, not a registry entry.

## Consequences for later slices

| Slice | Consequence |
| --- | --- |
| S6 | Fills `StepResult`'s `suspended` variant (K21) and adds `question`; the loop already detects tool calls |
| S7 | Calls `RunStep` with rows it read itself — possible because K6/K22 kept persistence outside |
| S11 | Fits the brakes: `evaluateBash` at `bash`'s spawn point, and `--allow-bash` is **removed** |
| S8 | Adds the **handler registry** and the literal becomes its first entry (L14) — a blueprint that can name only one handler demonstrates no routing |
| S13 | Fills that registry with the 25 bodies and the tech-tag resolver |

## Open questions

None. Every limit — `MaxIterations`, `DeadlineAt`, the timeout, the output caps — is a guess derived from prose,
stated as such, and the QA suite is expected to change the numbers rather than confirm them.

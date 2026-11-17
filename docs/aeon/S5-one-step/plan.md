# Plan — S5 One step, end to end

> **For agentic workers:** written to the `dev-loop-protocol` `plan.md` + `tasks/{taskid}.md` schema.
> Each task's full spec is at `tasks/T{n}.md`.
>
> **Spec:** [`design.md`](design.md) · **Brief:** [`brief.md`](brief.md) · **Demo:** [`DEMO.md`](DEMO.md)
> **Source material:** [`docs/_archive/aeon-v1-horizontal/03-step-runtime/`](../../_archive/aeon-v1-horizontal/03-step-runtime/) (K) and
> [`docs/_archive/aeon-v1-horizontal/05-tools/`](../../_archive/aeon-v1-horizontal/05-tools/) (W).

## Understanding

### What we understood

**The skateboard.** A model that reads a file, runs a command, reports in a bounded structured shape, and leaves
a transcript you can replay. Four slices of foundation converge into the first thing that gets somewhere.

Two superseded folders meet here. `docs/_archive/aeon-v1-horizontal/03-step-runtime` established K1–K20 — declared context, the
bounded loop, the four outcomes, the handoff requested as JSON and re-asked exactly once tool-free — and
`docs/_archive/aeon-v1-horizontal/05-tools` established the W-series, including the bounded-command discipline that turns three lines
of protocol prose into three properties of a tool. Neither is re-derived.

**This slice ships an incomplete version of both, and that is the thing to be careful about.**

`bash` lands with **bounds and no policy.** The 50-pattern corpus, the seven authored `ask` rules and the human
prompt are S11. Three things make that acceptable for a skateboard, and none makes it shippable:

- The tool list is an **allow-list**, and the default step grants `read` only. Granting a shell takes
  `--allow-bash`, which warns on stderr every time it is used.
- The **bounds** land now — timeout, stdin closed, caps, `oomKilled`. Those are about a step not hanging rather
  than about permission, and a hung skateboard is as useless as an unsafe one.
- The **demo says so**, in a section naming S11.

Deferring `bash` entirely was considered and rejected. A step that can run nothing cannot demonstrate the tool
loop, serial execution or the output caps — which are most of what the runtime is — and building the loop
against one read-only tool means discovering its shape at S11 with a policy layer as a confounding variable.

One structural decision is worth stating because it costs a field now and a sweep later: **`StepResult` ships as
a tagged union with a `suspended` variant nothing produces** (K21), and a test asserts that. S6 fills the
variant in rather than reshaping a type every caller switches on.

### Probes to run and record before decomposing

| # | Probe |
| --- | --- |
| Q1 | **Quote** `docs/_archive/aeon-v1-horizontal/03-step-runtime` T4's three handoff-extraction strategies. Port them; they encode what was learned about how models emit JSON |
| Q2 | The full event list, and what S9 expects later. Extend the vocabulary; do not rename |
| Q3 | `MaxIterations`, `DeadlineAt`, the bash timeout and every output cap — **and where each number came from.** The superseded slice derived 600s from agent bodies (`timeout 600` ×21). Every one is a guess; say so per number |
| Q4 | How Go terminates a **process group** so children die too, and how a **freed port** proves it |
| Q5 | How `read` detects a symlink escape **after** resolution, and what `filepath.EvalSymlinks` does with a path that does not exist |
| Q6 | What S3's `Client` reports for an unparseable tool-call argument, and what this slice owes in response |

> Q3 must produce four numbers **each with its provenance**. Scattered constants become folklore, and the QA
> suite is written to change them rather than confirm them.

### How we will do it

Types, then the pure parts, then the resolver, then the tools, then the loop, then the surface.

`types.go` opens because `StepResult`'s union shape (K21) is what every later task switches on. Then context
assembly and `CompileRequest` — both pure, both failing at **construction** rather than at first use, which is
the difference between an error naming the handler and a tool-call failure three turns in that the model tries
to work around.

The **path resolver lands before any file tool** (R5, I1). One resolver, and a second is a defect because the two
diverge and only one gets tested — the divergence being an escape that works.

`bash` is its own task (R6): its four bounded properties fail differently, and one of them —`oomKilled` — is the
assertion most likely to be written from a hand-built fixture that never actually killed anything.

**The allow-list is a separate task** (R8) carrying D1, because it is this slice's only safety boundary and
deserves not to be a bullet in a larger task's list. Its test must prove refusal **by attempt**: "the map does
not contain bash" is a weaker claim than "the model asked and could not".

Then the bounded loop as one task (R4, I4) — S6 and S7 build on it, and a second copy would drift, which is what
breaks resume. Then `RunStep`, the `step` method, the CLI, and the demo walk with real keys.

### What you will have at the end

`aeon step "read the README and tell me what a newcomer would still not know"` doing exactly that: a declared
context, a real tool call, four possible outcomes, a cost, and a transcript that replays after a restart.

## Objective

Assemble the skateboard: a runtime, two tools, and a `step` command that works end to end and persists.

## Tech tags in use

go, docs

## Test suite

aeon-runtime, aeon-tools

## Tasks

### T1 — `runtime/types.go`: the result union and the limits file
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~280
- detail: tasks/T1.md
- success_criteria:
  - unit test `StepResult narrows to complete or suspended` passes
  - unit test `there is no fifth outcome` passes
  - unit test `nothing produces suspended` passes, asserted by grep and by exhausting the fake client's scripts
  - unit test `DeadlineAt is an absolute timestamp, not a duration` passes
  - unit test `every limit is in limits.go and labelled a guess` passes, asserted against the source
  - the event vocabulary contains the superseded names unchanged plus `step.tool.start` and `step.tool.end`
- files_expected: `aeon/internal/runtime/types.go`, `aeon/internal/runtime/limits.go`, `aeon/internal/runtime/types_test.go`

### T2 — `context.go`: assembly, hashing, the frozen manifest
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~270
- detail: tasks/T2.md
- success_criteria:
  - unit test `sections assemble in declared order` passes
  - unit test `every declared file is hashed into the manifest` passes
  - unit test `a declared file that does not exist is a construction error, not an empty section` passes
  - unit test `a second assembly does not re-read` passes, asserted with a readFile spy
  - unit test `assembly reads no file outside the declared set` passes
  - unit test `assembly is deterministic under a fixed clock` passes
- files_expected: `aeon/internal/runtime/context.go`, `aeon/internal/runtime/context_test.go`

### T3 — `compile.go`: `CompileRequest`
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~250
- detail: tasks/T3.md
- success_criteria:
  - unit test `CompileRequest produces ordered sections and a frozen manifest` passes
  - unit test `a handler declaring a tool the toolset lacks fails at construction` passes, with the reason naming both
  - unit test `the failure happens before any model call` passes, with a Client spy at zero
  - unit test `MaxOutputTokens is required and a missing value is a construction error` passes
  - unit test `the handoff schema is derived once, not per retry` passes
- files_expected: `aeon/internal/runtime/compile.go`, `aeon/internal/runtime/compile_test.go`

### T4 — `handoff.go`: extraction and the one tool-free retry
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~280
- detail: tasks/T4.md
- success_criteria:
  - unit test `each of the three extraction strategies works` passes, one case per strategy quoted from the superseded T4
  - unit test `strategies are tried in order and stop at the first success` passes
  - unit test `a schema mismatch names the failing path` passes
  - unit test `extraction never panics` passes over nil, empty and malformed inputs
  - unit test `this module performs no model call` passes, with a Client spy at zero
- files_expected: `aeon/internal/runtime/handoff.go`, `aeon/internal/runtime/handoff_test.go`

### T5 — `tools/paths.go` and `read.go`
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~290
- detail: tasks/T5.md
- success_criteria:
  - unit test `an absolute path is refused` passes, by attempt
  - unit test `a path containing .. is refused` passes, by attempt
  - unit test `a symlink resolving outside the workspace is refused` passes, checked **after** resolution
  - unit test `an empty path is refused` passes, by attempt
  - unit test `every refusal is a named tool error, never a clamp into range` passes
  - unit test `read reports truncation with the true size` passes
  - unit test `no tool reads ambient state` passes, asserted by grep for `os.Getenv` and `os.Getwd`
  - unit test `there is exactly one path resolver` passes, asserted by grep
- files_expected: `aeon/internal/tools/paths.go`, `aeon/internal/tools/read.go`, `aeon/internal/tools/paths_test.go`, `aeon/internal/tools/read_test.go`

### T6 — `tools/bash.go`: bounds, and no policy
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T5]
- est_changed_lines: ~300
- detail: tasks/T6.md
- success_criteria:
  - unit test `a command that never exits returns timedOut with its partial output` passes
  - unit test `a command reading stdin fails promptly rather than blocking` passes, with a timeout
  - unit test `a SIGKILLed process reports oomKilled true and an ordinary failure reports false` passes — the test **actually SIGKILLs**
  - unit test `stdout and stderr truncation are reported with the true size` passes
  - unit test `a process group is fully terminated, children included` passes, proven by a **freed port**
  - unit test `SIGTERM escalates to SIGKILL after the grace period` passes
  - unit test `the tool honours the step's context` passes
  - unit test `bash consults no policy` passes — asserted by grep for pattern matching or a deny list (S11 owns it)
- files_expected: `aeon/internal/tools/bash.go`, `aeon/internal/tools/spawn.go`, `aeon/internal/tools/bash_test.go`

### T7 — `tools/toolset.go`: the allow-list, and D1
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T6]
- est_changed_lines: ~200
- detail: tasks/T7.md
- success_criteria:
  - unit test `a step granted only read receives no bash tool` passes
  - unit test `a model asking for bash in a step that lacks it gets a tool_result with IsError naming what is available` passes — **proven by attempt**
  - unit test `an unknown tool name in the grant list is a construction error` passes
  - unit test `a panicking tool is caught, returns IsError, and the step continues` passes
  - unit test `a programmer error is not swallowed by the panic boundary` passes
  - the grant list is the only thing determining what a step can call, asserted by grep
- files_expected: `aeon/internal/tools/toolset.go`, `aeon/internal/tools/toolset_test.go`

### T8 — `loop.go`: the bounded loop, serial tools, four outcomes
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3, T4, T7]
- est_changed_lines: ~330
- detail: tasks/T8.md
- success_criteria:
  - unit test `tools execute serially in emission order` passes, with a spy recording **entry and exit** and asserting no overlap
  - unit test `all four outcomes are produced` passes, one case each
  - unit test `exceeding MaxIterations yields blocked naming the bound and its value` passes
  - unit test `exceeding DeadlineAt yields blocked naming the bound` passes
  - unit test `an invalid handoff retries once with no tools key in the request` passes
  - unit test `a second handoff failure yields failed with the transcript and partial usage intact` passes
  - unit test `an unparseable tool argument becomes a tool_result with IsError and the loop continues` passes
  - unit test `the loop honours context cancellation promptly` passes, with a timeout
  - unit test `there is exactly one loop implementation` passes, asserted by grep
- files_expected: `aeon/internal/runtime/loop.go`, `aeon/internal/runtime/loop_test.go`

### T9 — `runstep.go`: the entry point and the event sink
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T8]
- est_changed_lines: ~250
- detail: tasks/T9.md
- success_criteria:
  - unit test `RunStep persists nothing` passes, with a store spy asserting **zero** calls
  - unit test `RunStep opens no store` passes, asserted by grep
  - unit test `events reach the injected sink as they happen, in order` passes
  - unit test `the sink is called during the step, not after` passes — asserted by observing iteration count at call time
  - unit test `a throwing sink does not kill the step and the failure is counted` passes
  - unit test `RunStep returns exactly once` passes for all four outcomes
  - unit test `a programmer error panics and a step failure returns` passes, one case each
- files_expected: `aeon/internal/runtime/runstep.go`, `aeon/internal/runtime/runstep_test.go`

### T10 — `protocol/step.go`: the method, persistence, streaming
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T9]
- est_changed_lines: ~290
- detail: tasks/T10.md
- success_criteria:
  - unit test `step returns a stepId and streams step.* events to a subscriber` passes
  - unit test `the handler persists the transcript and the step row` passes
  - unit test `the handler is the only writer` passes, asserted by grep
  - unit test `the manifest is persisted to context_manifest` passes
  - unit test `cost is persisted with its state, and a partial keeps its Missing list` passes
  - unit test `PROTOCOL_VERSION is unchanged by this addition` passes
  - the error codes this slice adds are allocated in S2's registry with all three parts
- files_expected: `aeon/internal/protocol/step.go`, `aeon/internal/protocol/method.go`, `aeon/internal/protocol/errors.go`, `aeon/internal/protocol/step_test.go`

### T11 — The CLI: `aeon step`, `aeon step show`
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T10]
- est_changed_lines: ~280
- detail: tasks/T11.md
- success_criteria:
  - unit test `aeon step streams tool calls and text incrementally` passes
  - unit test `--allow-bash grants bash and warns on stderr every time` passes
  - unit test `without --allow-bash the step has read only` passes
  - unit test `--allow-bash help text states there is no command policy` passes, asserted on content
  - unit test `the outcome line distinguishes blocked from failed` passes
  - unit test `step show prints the transcript, and --manifest prints the frozen manifest` passes
  - unit test `step show --requests shows the retry carrying no tools` passes
  - unit test `--max-iterations bounds the loop` passes
- files_expected: `aeon/cmd/aeon/step.go`, `aeon/cmd/aeon/step_test.go`

### T12 — Walk the demo, and amend both QA suites
- engineer: technical-writer-loop
- tech: docs
- depends_on: [T11]
- est_changed_lines: ~200
- detail: tasks/T12.md
- success_criteria:
  - `DEMO.md` is walked end to end **with real keys**, including every §8 check
  - **§8 is not softened** — this task's notes confirm it still states that `bash` has no policy and that the allow-list is the only boundary
  - every mismatch is fixed and this task's notes record whether the code or the demo was wrong
  - `docs/test_suites/aeon-runtime.md` is amended — `QAC-` ids stay stable
  - `docs/test_suites/aeon-tools.md` is amended — `QAC-` ids stay stable, and cases for the seven absent tools are marked **not yet applicable** rather than deleted
  - both suites' TypeScript-era commands are corrected for Go
- files_expected: `docs/aeon/S5-one-step/DEMO.md`, `docs/test_suites/aeon-runtime.md`, `docs/test_suites/aeon-tools.md`

## Priority order

**T7 and T8 are the deliverable**, and T7 is the one that matters most for a reason that has nothing to do with
features.

T7 is **the only safety boundary in this slice.** There is no command policy until S11, so the allow-list is
what stands between a step and an unrestricted shell. Its test has to prove refusal **by attempt** — the model
asks for `bash` and cannot get one — because "the toolset map does not contain bash" is a much weaker claim that
would pass against a loop which happened to construct the tool anyway. It is a small task and it carries D1,
which is why it is separate rather than a bullet inside T8.

T8 is the runtime's substance, and three of its properties are easy to get subtly wrong:

**Serial execution** needs a spy that would **detect** concurrency. One recording call order passes against a
`errgroup.Wait()` implementation; one recording entry and exit timestamps and asserting no overlap does not.
Serial execution is what makes transcripts reproducible and stops two tools writing one file in an order nobody
chose.

**The tool-free retry**, where the assertion is not that a retry happened but that **the retry request carried no
tools**. A retry that can call tools can start new work — a step that already did its job doing it again,
differently, while ostensibly restating its report.

**`blocked` rather than `failed`** on a bound, with the bound named. A caller seeing `failed` files a bug report;
one seeing `blocked` raises the budget. Conflating them sends every over-long step to the wrong response.

T6 carries the assertion most likely to be faked: **`oomKilled` from a test that actually SIGKILLs a process**,
not from a hand-built fixture. And its group-termination test is proven by a **freed port** rather than a return
code, because a return code tells you the parent died and says nothing about its children.

T5 comes before both tools because one resolver is an invariant, not a preference: two diverge, only one is
tested, and the divergence is an escape that works. Its symlink check runs **after** resolution — a
pre-resolution check passes for a symlink, which is the escape this class of bug always uses.

A plan ordered by visible progress would have written the loop first, because it is the interesting part, and
fitted the allow-list afterwards — at which point the slice would have shipped an unguarded shell granted by
default, and the demo would have read as an invitation.

# Brief — S5 One step, end to end

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Converges:** [`docs/_archive/aeon-v1-horizontal/03-step-runtime/`](../../_archive/aeon-v1-horizontal/03-step-runtime/) (K) and [`docs/_archive/aeon-v1-horizontal/05-tools/`](../../_archive/aeon-v1-horizontal/05-tools/) (W).
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — §3 is binding.
**Stage:** 1 · Skateboard — **this slice completes it.** Depends on S1–S4.

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief corrects it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** Two inherited decision series converge here, so §6 and §10 reference both.
> **(d)** §2 opens with a **negative** DoD item. That is unusual and deliberate: the single most serious
> failure this slice can have is a step executing `bash` without being granted it.

---

## 1. How to use this brief

> **Constant. Copy verbatim.**

This is a **brief, not a plan.** It states what must be true when the work is done, what must not change along
the way, and where to look to find out the rest. It contains no task breakdown and no implementation code.

1. **Read** the design and the demo, then §7 for where this brief overrides the design.
2. **Investigate** — §10 names what to read. Do not plan before you have those answers.
3. **Plan and decompose** — §12 gives the rules your task sequence must obey.
4. **Self-check** — §13, against your finished plan, before dispatching anything.

Where this brief states a decision, it is settled. Where it poses a question, answer it from the code.

**On the self-check:** every answer needs an artifact — a command's output, an enumeration, a quoted line.

---

## 2. Definition of Done

| # | Check |
| --- | --- |
| **D1** | **A step not granted `bash` cannot call it.** Proven by attempt: a step whose tool list is `["read"]` receives no bash tool, and no code path lets the model reach one. **This is the most serious failure available to this slice** — there is no command policy until S11, so the allow-list is the only boundary. |
| **D2** | **A step reads a file and answers**, end to end, against a real provider: declared context, one tool call, a validated handoff, one of four outcomes, a cost. |
| **D3** | **Tools execute serially in emission order** (K9), proven by a spy that would **detect** concurrency — entry and exit timestamps asserting no overlap. A spy recording only call order would pass against a parallel implementation. |
| **D4** | **The handoff retry is exactly one and carries no tools** (K4). Asserted on the **request object**: a retry offered tools could start new work while ostensibly restating a report. A second failure yields `failed` with the transcript intact. |
| **D5** | **Exceeding a bound yields `blocked`, not `failed`**, naming which bound. Two tests — iterations and deadline. |
| **D6** | **`bash` cannot hang.** A command that never exits returns `timedOut: true` with its partial output, within the budget. |
| **D7** | **`bash` cannot wait on input.** A command reading stdin fails **promptly** rather than blocking. |
| **D8** | **`oomKilled` is reported as a fact**, `true` for a SIGKILLed process and `false` for an ordinary non-zero exit, and the two are **distinguishable in what a model sees**. |
| **D9** | **Output truncation is reported with the true size**, never silent — for stdout, stderr and `read`. |
| **D10** | **A `bash` process group is fully terminated**, children included, with `SIGTERM` escalating to `SIGKILL`. Proven by a freed port, not by a return code. |
| **D11** | **`read` refuses by attempt** — absolute path, `..` segment, symlink resolving outside the workspace, empty path. Each a named tool error; **none silently clamped into range**. |
| **D12** | **The manifest is frozen at assembly**, with a content hash per declared file, and a second assembly does not re-read (K13, K23). |
| **D13** | **A missing declared file is a construction error, not an empty section.** An empty section reads to a model as "this file is empty", which is a different and more damaging claim. |
| **D14** | **`suspended` is never produced** (K21). A test asserts no code path returns it, so S6 fills the variant rather than reshaping the type. |
| **D15** | **`RunStep` persists nothing** (K6). Asserted with a store spy at zero calls; the `step` handler is the only writer (K22). |
| **D16** | **The transcript replays from the store in a child process**, byte-identically, signed `thinking` block included. |
| **D17** | **Streaming reaches clients as ordinary protocol events** (K24), and `PROTOCOL_VERSION` is unchanged. |
| **D18** | **No unit test contacts a provider.** The suite passes with no network and no keys. |
| **D19** | **`make test` and `make lint` are green**, `make fmt` leaves no diff. |

D1, D3 and D4 are the three that matter. D1 because the allow-list is the only safety boundary at this stage.
D3 because a parallel tool loop produces transcripts nobody can reproduce and file writes in an order nobody
chose. D4 because a retry that can act is a step that can do its work twice.

**D1–D18 must each exist as named tests.**

D19 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

The skateboard: a model that reads a file, runs a command, reports in a bounded structured shape, and leaves a
replayable transcript.

Two superseded folders converge. `docs/_archive/aeon-v1-horizontal/03-step-runtime` established K1–K20 — declared context, the
bounded loop, four outcomes, the handoff with one tool-free retry — and `docs/_archive/aeon-v1-horizontal/05-tools` established the
W-series, including the bounded-command discipline that turns three lines of protocol prose into three
properties of a tool.

**And this slice ships an incomplete version of both, deliberately.**

`bash` lands **with bounds and without a policy.** The 50-pattern evaluator, the seven authored `ask` rules and
the human prompt are S11. Three things make that acceptable for a skateboard and none makes it shippable: the
tool list is an **allow-list** and the default grants `read` only; the **bounds** land now, because a hung step
is as useless as an unsafe one; and the demo says so in a section naming S11.

Deferring `bash` entirely was considered and rejected — a step that can run nothing cannot demonstrate the tool
loop, serial execution or the output caps, which are most of what the runtime is, and building the loop against
one read-only tool means discovering its shape at S11 with a policy layer as a confounding variable.

## 4. Goal

`aeon step "…"` assembles a declared context, runs a bounded tool loop against a real provider, returns one of
four outcomes with a validated handoff, persists the transcript, and replays it afterwards.

## 5. Shape and the seam

```
protocol/step.go ──▶ runtime.CompileRequest ──▶ runtime.RunStep ──▶ model.Client   (S3)
       │                                              │
       │  persists (K22)                              ├──▶ tools.BuildToolset (allow-list)
       ▼                                              │       read · bash
   store (S4)                                         └──▶ event sink ──▶ S2 subscriptions (K24)
```

### The seams

**Upward — `RunStep`.** S6 adds suspension, S7 calls it with rows it read itself, S9's `execute` calls it per
step. All three are possible **because it persists nothing** (K6): the caller owns the transcript, and that is
what lets recovery hand it rows from a previous process.

**Downward — three injected dependencies.** `model.Client`, the toolset, the event sink. All constructor
parameters, which is what makes D18 achievable.

**Sideways — the allow-list.** `BuildToolset(granted []string)` returns only what was named. It is the seam S11
replaces `--allow-bash` at, and the reason a policy can be added without touching the loop.

## 6. Locked decisions

- **K5, K6** — `RunStep` is a function; it persists nothing.
- **K9** — tools serial, in emission order.
- **K4** — one retry, **tool-free**, asserted on the request.
- **K13, K23** — the manifest is frozen at assembly.
- **K21** — `StepResult` ships as a union; `suspended` exists and is never produced.
- **K22** — the `step` handler persists.
- **W2** — one path resolver, workspace-relative.
- **W11** — **two tools**: `read` and `bash`.
- **W12** — `bash` has **no policy** and is **not granted by default**.
- **W13** — mandatory timeout, stdin closed, caps reported, `oomKilled` reported.

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | `--allow-bash` grants `bash` | The flag also **prints a one-line warning to stderr on every use**, not only in `--help`. A flag whose danger is documented only in help is a flag people copy from a shell history six months later. |
| O2 | Exceeding a bound yields `blocked` | The reason string names **which** bound and its value — `exceeded MaxIterations (12)`. "Blocked" alone tells a caller to raise something without saying what. |
| O3 | `read` refuses an absolute path | Also refuse a path resolving **outside the workspace via a symlink**, checked after resolution rather than before. A pre-resolution check passes for a symlink and is the escape this class of bug always uses. |
| O4 | The design lists `MaxIterations` and `DeadlineAt` | Both, plus the bash timeout and every output cap, are **stated in one file as guesses**, with where the number came from. Scattered constants become folklore, and the QA suite is expected to change them. |
| O5 | Tool results are strings with `IsError` | A tool that **panics** is caught, converted to a tool result with `IsError: true`, and the step continues. A panicking tool taking down a daemon serving other projects is a worse outcome than a step reporting a bad result. |
| O6 | The design does not say what happens when the model emits a tool the step lacks | A `tool_result` with `IsError: true` naming the tool and listing what **is** available. Not a step failure — the model can recover from being told — and not silence, which it cannot. |

## 8. Non-goals

- **N1 — No bash policy, no `ask`, no prompt.** **S11.** The single largest gap in this slice.
- **N2 — No suspension, no `question` tool.** S6. `suspended` exists unused (K21, D14).
- **N3 — No crash recovery, re-run classes or receipts.** S7.
- **N4 — No compaction.** S6. `MaxIterations` is the bound.
- **N5 — Seven of nine tools absent** — `write`, `edit`, `glob`, `grep`, `webfetch`, `todo`, `question`. So a
  step **cannot change a file** at S5.
- **N6 — No blueprint, phase, task or gate.** S8. A step is not a loop.
- **N7 — No agent registry.** S13. The handler is a literal.
- **N8 — No sandbox.** Standing non-goal, inherited. No container, no seccomp, no user namespace.

### Standing non-goals

- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never run `git clean`.**
- **No test runs a command outside a temp directory**, and none targets a public URL.

## 9. Repo invariants

**I1 — One path resolver** (W2). Every file tool calls the same function. A second resolution path is a defect:
the two diverge and only one is tested.

**I2 — `bash` never spawns outside the toolset's grant.** Not "checks then spawns" as source ordering, but
structurally: the tool does not exist in a step that was not granted it. D1 asserts it by attempt.

**I3 — `RunStep` opens no store and performs no I/O beyond its injected dependencies** (K6). Asserted by grep
and by a store spy.

**I4 — One bounded loop.** S6 and S7 build on it; a second copy would drift, and drift is what breaks resume.

**I5 — No ambient state.** No `os.Getenv`, no `os.Getwd` inside a tool. Everything arrives in `ToolContext`.
This is the invariant whose violation reintroduces path drift and is invisible in review because it works on the
author's machine.

**I6 — Every limit is in one file** (O4), stated as a guess.

**I7 — No test touches the network** (D18), the real store, or the working repository.

**I8 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/03-step-runtime/design.md`** — K1–K20 in full. This slice implements a subset; §Inherited
   in its design says which.
2. **`docs/_archive/aeon-v1-horizontal/03-step-runtime/tasks/`** — ten task specs. The bounded loop, handoff extraction and the
   event sink port closely; the suspension tasks are S6's.
3. **`docs/_archive/aeon-v1-horizontal/05-tools/`** — the W-series, and in particular its `bash` tasks (T5, T6) for the bounded
   discipline. **Its policy content is S11's, not this slice's.**
4. **`internal/model/`** (S3) — `Client`, `ModelRequest`, `Block`, `Cost`.
5. **`internal/store/`** (S4) — `AppendMessages`, `ReadTranscript`, the step row and its manifest column.
6. **`internal/protocol/ask.go`** (S3) — the pattern `step` follows for streaming through subscriptions.
7. **`opencode/agents/golang-engineer-loop.md`** — a real handler body, for what a step's context contains and
   how large it is.

### 10.2 The patterns to copy

- **Discriminated results over exceptions**, as S1–S4 do.
- **Injected dependencies**, so D18 and D15 are achievable.
- **S3's fake-client discipline**, one level up: replay canonical deltas rather than SSE.
- **S4's temp-directory test hygiene.**

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | What exactly are `docs/_archive/aeon-v1-horizontal/03-step-runtime`'s three handoff-extraction strategies? **Quote them.** | Its T4. Port them; they encode what was learned about how models emit JSON. |
| Q2 | What is `docs/_archive/aeon-v1-horizontal/03-step-runtime`'s full event list, and what does S9 expect later? | Its design and `docs/_archive/aeon-v1-horizontal/09-server`. Extend the vocabulary; do not rename. |
| Q3 | What are the `MaxIterations`, `DeadlineAt`, bash-timeout and output-cap values, and **where did each number come from**? | `docs/_archive/aeon-v1-horizontal/05-tools` derived the timeout from agent bodies (`timeout 600` ×21). Every one is a guess — say so per number. |
| Q4 | How does Go terminate a **process group** so a command's children die too, and how is that verified? | Try it. D10 is proven by a freed port, so name the mechanism. |
| Q5 | How does `read` detect a symlink escape **after** resolution, and what does Go's `filepath.EvalSymlinks` do with a path that does not exist? | Try it. O3 depends on the answer. |
| Q6 | What does S3's `Client` report when a tool call's arguments were unparseable, and what does this slice owe in response? | `internal/model/client.go`. |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q7 | Where does the step's workspace root come from, and how does a tool receive it without reading ambient state (I5)? |
| Q8 | How is a panicking tool caught without swallowing a genuine programmer error (O5, K20)? Name the boundary. |
| Q9 | What does the default handler's context contain — which sections, in what order? A step with no declared context is a step with nothing to read. |
| Q10 | How does `aeon step` know the workspace root, given the daemon multiplexes projects (§1a)? |

---

## 11. Behavioural contract

- `CompileRequest` assembles ordered sections, hashes every declared file into a frozen manifest (D12), and
  **fails at construction** for a missing declared file (D13) or a tool the toolset lacks.
- `RunStep` runs the bounded loop: stream, collect `tool_use`, execute **serially** (D3), append results, check
  bounds, extract and validate the handoff, retry **once tool-free** (D4), return one of four outcomes.
- Exceeding a bound → `blocked`, naming the bound and its value (O2).
- A tool the step lacks → `tool_result` with `IsError` naming it and listing what is available (O6).
- A panicking tool → caught, `IsError`, step continues (O5).
- `RunStep` persists nothing (D15); the `step` handler writes through S4 (K22).
- `bash`: mandatory timeout, stdin closed, caps reported, `oomKilled` reported, process group killed
  (D6–D10). **No policy** (W12).
- `read`: workspace-relative, refusing absolute, `..`, symlink escape and empty (D11, O3).
- `BuildToolset` returns only granted tools (D1, I2).
- `suspended` is never produced (D14).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **the bounded loop is one task**, and `RunStep` builds on it in the same task or a later one — never
  in parallel (I4).
- **R5** — the path resolver lands **before** any file tool (I1).
- **R6** — `bash` is its own task. Its four bounded-discipline properties are four tests and they fail
  differently.
- **R7** — no task over roughly 300 changed lines or 12 files without justification.
- **R8** — the allow-list task is **separate and its test is D1**, proven by attempt. It is the slice's only
  safety boundary and deserves not to be a bullet in a larger task's list.
- **R9** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure",
  "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — limits land in one file, in the first task that needs one (O4, I6).
- **R12** — the final task walks `DEMO.md` with real keys and amends **both** QA suites — `aeon-runtime` and
  `aeon-tools`.

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines or ~12 files without justification (R7) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | `grep -rn "os.Getenv\|os.Getwd" ` over your planned tool files | No hits (I5) |
| A6 | Name the task carrying **D1**, and confirm its test proves refusal **by attempt** | **A D1 test that only reads the toolset's contents blocks the plan.** The allow-list is the only safety boundary in this slice, and "the map does not contain bash" is a weaker claim than "the model asked and could not" |
| A7 | Paste Q1's three strategies, and Q3's four numbers **with the provenance of each** | Every number's origin stated, and every one labelled a guess |
| A8 | Paste the answers to Q4 and Q5 | Both **from running code**. Q4 must name how a freed port proves group termination |
| A9 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | **Confirm no task implements a bash policy, an `ask` verdict or a prompt** | N1 |
| B2 | Confirm no task implements suspension or a `question` tool | N2 |
| B3 | Confirm no task implements recovery, re-run classes or receipts | N3 |
| B4 | Confirm no task implements compaction | N4 |
| B5 | Confirm no task adds a tool beyond `read` and `bash` | N5, W11 |
| B6 | Confirm no task adds a blueprint, phase, task or gate | N6 |
| B7 | Confirm no task adds an agent registry, container, seccomp profile or user namespace | N7, N8 |
| B8 | Quote the `out_of_scope` lines covering N1–N8 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D18 to a task and a named test | §2 |
| C2 | For D3, confirm the spy records **entry and exit** and asserts no overlap | D3 |
| C3 | For D4, confirm the assertion is that the retry request carries **no tools key** | D4 |
| C4 | For D8, name the test that **actually SIGKILLs** a process. An `oomKilled` claim from a hand-built fixture is not evidence | D8 |
| C5 | For D10, confirm the assertion is a **freed port**, not a return code | D10 |
| C6 | For D11, list the four refusal reasons and confirm each is by attempt | D11, O3 |
| C7 | For D15, name the store spy and confirm it asserts **zero** calls | D15 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm the path resolver precedes every file tool | R5 |
| D2 | Confirm one task owns the bounded loop | R4, I4 |
| D3 | Confirm `bash` is its own task | R6 |
| D4 | Confirm the allow-list is a separate task carrying D1 | R8 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm one path resolver | I1 |
| E2 | Confirm every limit is in one file, labelled a guess | I6, O4 |
| E3 | Confirm `RunStep` opens no store | I3 |
| E4 | Confirm no ambient state in any tool | I5 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | State where the workspace root comes from and how a tool receives it (Q7, Q10). |
| F2 | Name the boundary that catches a panicking tool without swallowing a programmer error (Q8, O5, K20). |
| F3 | List the default handler's context sections in order (Q9). |
| F4 | Confirm `--allow-bash` warns on **stderr on every use**, not only in `--help` (O1). |
| F5 | Confirm a step is refused a tool it was not granted, and that the refusal lists what is available (O6). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R12) walks it **with real keys** and fixes whatever
does not match, recording which was wrong.

S5's demo has a section that must not be softened: **§8, "the skateboard has no brakes."** It states that `bash`
has no policy, that `read` is confined and `bash` is not, and that the allow-list is the only boundary. It also
contains the demo's most important negative check — a step **without** the flag being unable to run a command.

A demo that presented `--allow-bash` as a convenience rather than a hazard would be the single most misleading
document in this plan, because everything else about the slice works and invites use.

# Plan — S6 Suspend, question, compaction

> **For agentic workers:** written to the `dev-loop-protocol` `plan.md` + `tasks/{taskid}.md` schema.
> Each task's full spec is at `tasks/T{n}.md`.
>
> **Spec:** [`design.md`](design.md) · **Brief:** [`brief.md`](brief.md) · **Demo:** [`DEMO.md`](DEMO.md)
> **Source material:** [`docs/_archive/aeon-v1-horizontal/03-step-runtime/`](../../_archive/aeon-v1-horizontal/03-step-runtime/) K11–K20, and its T6–T8.

## Understanding

### What we understood

The bicycle begins with what the skateboard cannot do: **stop and ask.**

S5 built a step that runs to completion or fails. This slice lets it suspend on a question, **exit its process
entirely**, and be resumed hours later when a human answers. That is P2 — and it is the property every
competitor structurally lacks, because they keep session state in a process. Close the terminal and their work is
gone; here the process is *expected* to be gone.

Most of the machinery already exists, which is the point of having sequenced it this way. S4 has `question`,
`chat_message`, the transactional pairing and the derived `IsAwaiting`. S5 shipped `StepResult` as a union with a
`suspended` variant **nothing produced** — deliberately, so this slice fills it in rather than reshaping a type
every caller switches on. What is new is the runtime path: detect a question, exit cleanly, reconstruct from
persisted state alone.

**Compaction is folded in rather than given its own slice** (K27). It was deferred from S5, where
`MaxIterations` was the only bound, and it belongs here because **suspension is what makes it necessary** — a
step spanning a human round-trip runs far more iterations than an unattended one. Folding also means the keystone
identity test covers a compacted step from the start rather than being extended later.

**The keystone is D1, and it is the only DoD item in this plan that cannot be satisfied in one process.** Every
other test here can pass against an implementation that quietly depends on something surviving in memory — a
closure over the original transcript, a cached manifest, a package-level counter. Those all work perfectly in a
single-process test and fail the first time a real step suspends overnight, which is the entire promise. Only a
test that serialises through JSON, rebuilds in a fresh object graph sharing no reference, and asserts deep
equality against a never-suspended run rules it out.

### Probes to run and record before decomposing

| # | Probe |
| --- | --- |
| Q1 | Exactly how `ValidateTranscript` accepts a suspended transcript, and what it rejects. **That rule exists for this slice** |
| Q2 | **Does `Route` carry a context window?** The estimate is a fraction of *something*; if it does not, this slice adds it |
| Q3 | The token estimate's heuristic and safety margin — yours to choose, and both stated as uncalibrated |
| Q4 | Whether S5's `Rehash` already returns `changed` and `missing` separately |
| Q5 | What S4's `Answer` refuses and returns. D15 reuses it rather than reimplementing |
| Q6 | Whether S5's `RunLoop` signature accommodates a resumed `LoopState` unchanged. **If not, say how** — a second loop is forbidden |

> Q6 is the one that could reshape the plan. I1 forbids a second loop, so if `RunLoop` needs a signature change
> that is a task, not a workaround.

### How we will do it

The tool, then the two doors, then compaction, then the keystone.

`question` comes first (R8) because suspension detection needs a tool to detect. It is small and unusual: **its
execute function panics** (W15), because it is unreachable by design and a panic says so rather than fabricating
an answer nobody gave.

Then suspension in the loop, carrying K25 — **siblings execute before the suspension.** Suspending immediately
would leave two unanswered `tool_use` blocks, which `ValidateTranscript` rejects and no resume path can repair.
Then `ResumeStep`, sharing S5's **one** loop (I1, O1), with the changed-file note as a **`user`**-role message
because Anthropic hoists `system` to the head and would deliver the note before the content it comments on.

Compaction splits in two (R5). The **selector** carries the three exclusions and a property-based test over a
generated corpus (R6) — the transcripts that break compaction are shapes nobody enumerates, so examples cannot
cover it. The **summary** carries the model call, the replacement and the cost attribution.

**The keystone is its own task** (R4), after both doors. Then the protocol and CLI surfaces, then the demo walk.

### What you will have at the end

A step that asks, exits, and continues hours later from exactly where it stopped — with a long step compacting
itself without producing a transcript the provider rejects, and a test that proves the resumed step is
indistinguishable from one that never stopped.

## Objective

Fill `StepResult`'s `suspended` variant, add `ResumeStep`, and fold in compaction — so a step can wait for a
human without holding anything.

## Tech tags in use

go, docs

## Test suite

aeon-runtime, aeon-tools

## Tasks

### T1 — `tools/question.go`: the ninth tool, never executed
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~190
- detail: tasks/T1.md
- success_criteria:
  - unit test `question produces a ToolSpec with text, optional options and a kind` passes
  - unit test `question's execute panics with a message naming K18` passes
  - unit test `the description tells the model when asking is appropriate and when it is not` passes, asserted on content
  - unit test `the description warns that asking suspends the step` passes, asserted on content
  - unit test `options present means structured, absent means free-form` passes
  - the tool is added to the grant vocabulary but **not** to the default grant list
- files_expected: `aeon/internal/tools/question.go`, `aeon/internal/tools/question_test.go`

### T2 — Suspension in the loop: detection, siblings-first, the return
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~280
- detail: tasks/T2.md
- success_criteria:
  - unit test `a question tool call is never passed to the toolset` passes, tool spy at zero
  - unit test `read plus question in one turn executes read first, then suspends` passes, asserted on the transcript's **final shape**
  - unit test `the suspended transcript has exactly one unanswered tool_use and it is the question` passes
  - unit test `ValidateTranscript accepts the suspended transcript` passes, using S3's validator
  - unit test `the suspended result carries the question and the manifest` passes
  - unit test `a suspension consumes an iteration` passes
  - unit test `there is still exactly one loop function` passes, asserted by grep
- files_expected: `aeon/internal/runtime/loop.go`, `aeon/internal/runtime/suspend_test.go`

### T3 — `resume.go`: `ResumeStep` on the shared loop
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T2]
- est_changed_lines: ~290
- detail: tasks/T3.md
- success_criteria:
  - unit test `ResumeStep calls AssembleContext zero times` passes, asserted with a spy
  - unit test `a changed file produces a user-role note naming it` passes, asserted on the **role**
  - unit test `a deleted file is reported as missing, distinct from changed` passes
  - unit test `the transcript still quotes the original content of a changed file` passes
  - unit test `the note caps the paths named and says how many were omitted` passes
  - unit test `a resumed step re-runs no tool` passes, tool spy at zero for tools already in the transcript
  - unit test `bounds come from the manifest and do not reset` passes
  - unit test `an empty answer is valid and passed through` passes
  - unit test `a second question suspends again, consuming an iteration` passes
- files_expected: `aeon/internal/runtime/resume.go`, `aeon/internal/runtime/resume_test.go`

### T4 — `compact.go`: the estimate and the range selector
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3]
- est_changed_lines: ~300
- detail: tasks/T4.md
- success_criteria:
  - property test `over a generated corpus, no selected range contains a thinking block from the last assistant turn` passes
  - property test `over a generated corpus, no selected range orphans a tool_use from its tool_result` passes
  - property test `over a generated corpus, the head sections are never selected` passes
  - the corpus size is recorded and is in the hundreds
  - unit test `a transcript with no admissible range refuses with the Client spy at zero` passes
  - unit test `EstimateTokens is a documented heuristic with a stated margin, both labelled uncalibrated` passes
  - unit test `Route carries a context window` passes — added if Q2 found it absent
  - unit test `no tokeniser is used` passes, asserted by grep
- files_expected: `aeon/internal/runtime/compact.go`, `aeon/internal/runtime/compact_test.go`, `aeon/internal/runtime/compact_property_test.go`, `aeon/internal/model/types.go`

### T5 — Compaction: the summary, the replacement, the cost
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T4]
- est_changed_lines: ~270
- detail: tasks/T5.md
- success_criteria:
  - unit test `the summary request goes through the injected Client` passes
  - unit test `the summary replaces the range with one user-role message` passes
  - unit test `the summary's cost is added to the step's total` passes
  - unit test `the summary's usage is recorded separately on the step` passes
  - unit test `a summary exceeding its own bound is treated as an inadmissible compaction` passes
  - unit test `a transcript that cannot be compacted yields blocked with a reason, never a truncation` passes
  - unit test `a step.compacted event carries the range and the estimate before and after` passes
- files_expected: `aeon/internal/runtime/compact.go`, `aeon/internal/runtime/loop.go`, `aeon/internal/runtime/compact_summary_test.go`

### T6 — The keystone: resume identity
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T5]
- est_changed_lines: ~220
- detail: tasks/T6.md
- success_criteria:
  - integration test `a suspended-and-resumed step is deep-equal to one that never suspended` passes — transcript, outcome and handoff
  - the test asserts the reconstructed objects **share no reference** with the originals, explicitly
  - the `ResumeRequest` is built in a scope with **no access** to the originals
  - integration test `identity holds when the step also compacted` passes
  - integration test `identity holds across two suspensions` passes
  - integration test `two suspensions exhaust MaxIterations at the original bound` passes
  - every excluded field is listed with the reason it must differ; `messages`, `outcome` and `handoff` are **not** excluded
- files_expected: `aeon/internal/runtime/identity_test.go`

### T7 — `protocol/answer.go`: the method, events, persistence order
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T6]
- est_changed_lines: ~290
- detail: tasks/T7.md
- success_criteria:
  - unit test `a suspended step persists transcript and manifest, then the question, then status` passes, asserted on write order
  - unit test `a forced failure between the transcript write and the question write leaves a recoverable state` passes
  - unit test `answer resumes the step` passes
  - unit test `answering twice is refused with the existing timestamp` passes, reusing S4's guarantee
  - unit test `answering when no question is pending is refused` passes
  - unit test `question.asked and question.answered reach a subscriber` passes
  - unit test `no process remains after a suspension` passes, asserted by observation
  - unit test `PROTOCOL_VERSION is unchanged` passes
- files_expected: `aeon/internal/protocol/answer.go`, `aeon/internal/protocol/step.go`, `aeon/internal/protocol/method.go`, `aeon/internal/protocol/answer_test.go`

### T8 — The CLI: `aeon answer`, suspension display, `--compactions`
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T7]
- est_changed_lines: ~250
- detail: tasks/T8.md
- success_criteria:
  - unit test `a suspended step prints the question and how to answer it` passes
  - unit test `aeon answer resumes and prints the continuation` passes
  - unit test `aeon answer with an option list validates the choice` passes
  - unit test `answering twice prints the existing answer's timestamp` passes
  - unit test `step show --compactions prints the replaced range and the summary` passes
  - unit test `the resumed cost line includes the resume and any compaction` passes
  - unit test `a changed-file note is displayed on resume` passes
- files_expected: `aeon/cmd/aeon/answer.go`, `aeon/cmd/aeon/step.go`, `aeon/cmd/aeon/answer_test.go`

### T9 — Walk the demo, and amend the QA suites
- engineer: technical-writer-loop
- tech: docs
- depends_on: [T8]
- est_changed_lines: ~200
- detail: tasks/T9.md
- success_criteria:
  - `DEMO.md` is walked end to end **with real keys**, including §1's `pgrep` check and every check inside §6
  - a suspension is answered **after a daemon restart**, and after at least an hour, and both are recorded
  - every mismatch is fixed and this task's notes record whether the code or the demo was wrong
  - `docs/test_suites/aeon-runtime.md` is amended — `QAC-` ids stable
  - `docs/test_suites/aeon-tools.md` has its `question` cases moved from **not yet applicable** to active
  - appended cases cover a reboot resume, a cross-provider resume, a real compaction, and an inadmissible one
- files_expected: `docs/aeon/S6-suspend/DEMO.md`, `docs/test_suites/aeon-runtime.md`, `docs/test_suites/aeon-tools.md`

## Priority order

**T6 is the deliverable, and T2 and T4 are where it is won or lost.**

T6 is the smallest task in the slice by line count and the largest by consequence. Every other test here can pass
against an implementation that quietly depends on something surviving in memory between suspension and resume — a
closure over the original transcript, a cached manifest, a package-level counter. Those work perfectly in a
single-process test and fail the first time a real step waits overnight, which is the entire promise of P2. Only a
test that serialises through JSON, rebuilds in a **fresh object graph sharing no reference**, and asserts deep
equality against a never-suspended run can rule that out — and its `ResumeRequest` must be built in a scope with
no access to the originals, because otherwise the test's own convenience defeats it.

T2 carries K25, which is subtle and consequential. A `question` arriving alongside a `read` looks like a case for
suspending straight away — and doing so leaves **two** unanswered `tool_use` blocks in the transcript.
`ValidateTranscript` rejects that, and no resume path can repair it: the answer has nowhere to attach. So the
siblings run first, their results are appended, and the transcript the answer arrives into has exactly one hole.
The assertion is on the **transcript's final shape**, not on call order, because call order would pass against an
implementation that ran `read` and then discarded its result.

T4 carries the exclusion that fails furthest from home. A compaction that alters the last assistant turn's
`thinking` block produces a provider 400 about signatures — during a **later** resume, in a different package,
with nothing pointing at the compactor. And its test shape matters: the exclusions must be enforced by the
**selector**, proven by a refusal with the `model.Client` spy at **zero**. A post-hoc check can only reject a
summary after the model was paid to write it, and the step is then still over budget facing the same wall.

T4's property-based corpus is the other reason it is early. The transcripts that break compaction are shapes
nobody enumerates — an orphaned `tool_use` two messages from the boundary, a `thinking` block that happens to be
last, a head section longer than the middle. A handful of examples would pass and ship a compactor that breaks
the first time a real step calls three tools in one turn.

T1 is first because suspension detection needs a tool to detect, and its content is almost entirely a
**description written for a model** — the only instruction it gets about when interrupting a human is
appropriate. A model that asks about everything is as useless as one that asks about nothing.

A plan ordered by visible progress would have written `aeon answer` early, because it is the part a person
touches. It would then have discovered at S9 that resumed steps differ subtly from unsuspended ones, with no test
able to say how.

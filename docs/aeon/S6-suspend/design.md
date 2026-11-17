# Design — S6 Suspend, question, compaction (Stage 2 · Bicycle)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 2 · Bicycle, first of four
**Depends on:** S1–S5.
**Implements deferred:** [`docs/_archive/aeon-v1-horizontal/03-step-runtime/`](../../_archive/aeon-v1-horizontal/03-step-runtime/) K11, K12, K14–K18.

> Decision ids continue **K** (runtime) and **W** (tools). New runtime decisions start at **K25**, new tool
> decisions at **W15**.

---

## Context

The bicycle begins with the thing the skateboard cannot do: **stop and ask.**

S5 built a step that runs to completion or fails. This slice gives it the ability to suspend on a question,
exit its process entirely, and be resumed hours later by a human answering — which is P2, the spine, and the
property every competitor structurally lacks because they keep session state in a process.

Most of the machinery already exists. S4 has the `question` and `chat_message` tables, the transactional pairing
and the derived `IsAwaiting`. S5 shipped `StepResult` as a union with a `suspended` variant nothing produced
(K21) precisely so this slice fills it in rather than reshaping a type. So what is new here is the **runtime
path**: detecting a question, exiting cleanly, and reconstructing a step from persisted state alone.

**Compaction is folded into this slice** rather than given its own (K27). It was deferred from S5, where
`MaxIterations` was the only bound, and it belongs with suspension for a concrete reason: a suspended step can
span a human round-trip and many more iterations than an unattended one, so **suspension is what makes
compaction necessary**. Building them together also means the keystone identity test covers a compacted step
from the start rather than being extended later.

Its demo is the honest difficulty. "A step that would have failed now doesn't" is not watchable, so compaction
is demonstrated by what it **records**: a `step.compacted` event carrying the range and the token estimate
before and after, and `aeon step show --compactions` showing the summary the model wrote.

## Goal

A step asks a question, its process exits, a human answers hours later, and the step continues from exactly
where it stopped — with a long step compacting itself without producing a transcript the provider rejects.

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| K11 | **A step suspends by returning**, never by blocking or throwing (implemented) | A blocked call pins a process, which is the design P2 rejects; a thrown suspension is indistinguishable from a failure at the call site |
| K12 | `StepResult`'s `suspended` variant is now produced (fills K21) | Suspension is not a fifth outcome: the four describe *work*, suspension describes a *pause*, and collapsing them makes `blocked` ambiguous |
| K14 | Changed files are reported on resume as a **`user`-role** note, never `system` (implemented) | Anthropic hoists every `system` message to a top-level parameter (M5), so a mid-transcript `system` note would be silently relocated to the head — arriving **before** the content it comments on, which inverts its meaning |
| K15 | **Resume re-runs no tool** (implemented) | Tool results are already in the transcript, which *is* the record of what happened |
| K16 | Compaction's **range selector** enforces the exclusions; a post-hoc check is not a substitute | A check after the fact can only reject a bad summary **after paying for it**, and paying for a discarded summary is a cost with no product |
| K17 | The token budget is **estimated**, never counted exactly (implemented) | An exact count needs a tokeniser per model. Compaction needs to know "close to the limit", and an estimate with a margin answers that |
| K18 | `question` is a tool, recognised by name (implemented) | Making it a special message type would need a provider-level concept none of the three have |
| K25 | **A `question` emitted alongside other tool calls: the siblings execute first, then the step suspends** | Suspending immediately would leave those `tool_use` blocks unanswered — **two** pending calls, which `ValidateTranscript` rejects and no resume path can repair. The answer must arrive into a transcript whose turn is otherwise complete |
| K26 | `ResumeStep` is a separate entry point **sharing S5's single loop** | Their preconditions genuinely differ — `RunStep` assembles context and may have no transcript; `ResumeStep` assembles **nothing** and requires both. Two loop implementations would drift, and drift is exactly what the identity test exists to catch |
| K27 | **Compaction ships here, not as its own slice** | Suspension is what makes it necessary, and folding it means the keystone identity test covers a compacted step from the start. Its demo is what it records, not a failure it averted |
| K28 | **A step that cannot be compacted without violating an exclusion yields `blocked`**, with a reason — never a truncation that breaks the transcript | A broken transcript fails at the provider, later, with an error about signatures. `blocked` fails here, legibly |
| W15 | `question`'s execute function **panics** with a message naming K18 | It is unreachable by design; if it is ever called that is a runtime bug, and a panic says so rather than fabricating an answer |

## Architecture

```
aeon/internal/tools/
  question.go     the ninth tool — a ToolSpec whose execute is unreachable (W15)

aeon/internal/runtime/
  loop.go         + question detection, siblings-first (K25), the suspended return
  resume.go       ResumeStep — rehash, the user-role note, append the answer (K26)
  compact.go      EstimateTokens, SelectRange (K16), Compact
  runstep.go      unchanged shape; both doors call RunLoop

aeon/internal/protocol/
  answer.go       the `answer` method; question.* events
  step.go         + persist a suspension, + resume on answer
```

### Suspension, step by step

```
loop iteration
  │
  ├── model emits tool_use blocks
  │      │
  │      ├── partition: `question` vs everything else
  │      ├── execute the others SERIALLY, append their tool_result   ← K25
  │      └── a `question` was present?
  │             │
  │             ▼
  │      return { Kind: suspended, Question: {…} }
  │
  └── caller (protocol/step.go):
         persist transcript + manifest   ← order matters
         store.Ask(question)             ← one transaction with its thread entry
         step.status = 'suspended'
         PROCESS EXITS
```

The returned transcript **ends on the `question`'s `tool_use` with no `tool_result`** — which is precisely the
state S3's `ValidateTranscript` accepts as suspended, and the reason that rule exists.

**Persist the transcript before the question.** If the process dies between them, a transcript with no question
is recoverable — the step re-runs its last turn. A question with no transcript is not.

### Resume

```
read step + manifest + transcript + the answered question     (caller, from S4)
   │
   ▼  ResumeStep
   Rehash(manifest) → changed, missing
   changed or missing non-empty → append a USER-role note naming them   ← K14
   append tool_result(question.toolUseID, answer)
   RunLoop with the manifest's existing IterationsUsed and DeadlineAt
```

**Nothing is re-resolved and nothing is re-run** (K13, K15). The iteration count and the absolute deadline come
from the manifest, so **a step cannot escape its bounds by suspending repeatedly** — which is why S5 stored
`DeadlineAt` as an absolute time rather than a remaining duration.

### Compaction, and the three exclusions

Triggered when the **estimated** token count exceeds a fraction of the route's context window (K17). The runtime
asks the model to summarise a **middle range**, then replaces it.

Three ranges are **structurally excluded** — the selector cannot return them (K16):

| Excluded | Why |
| --- | --- |
| The head sections | They are the step's instructions. Summarising them summarises the task away |
| The last assistant turn's `thinking` blocks | The provider rejects a tool-use turn whose prior thinking was altered (M17, P21) — and the failure surfaces as a 400 about signatures |
| Any `tool_use` whose `tool_result` falls outside the range | Orphaning one is a 400 on Anthropic and a **silent degradation** on OpenAI, which is worse |

**The selector enforces them, not a check afterwards** (K16). A post-hoc validation can only reject a summary
after the model was paid to write it — and a rejected summary leaves the step still over budget, so the next
attempt faces the same wall.

**The summary's cost is added to the step's.** A compaction call goes through the same `model.Client` and costs
real money; not counting it is a hole in exactly the accounting three slices have preserved.

If no admissible range exists, the step yields **`blocked`** with a reason (K28). That is a legible failure here
rather than an illegible one at the provider.

### What a person sees

```
$ aeon step "refactor the store's migration runner and explain the risk"
step 8b1e4c · handler: default · tools: read, question

  Reading migrate.go…
  ▸ read internal/store/migrate.go                    4.8 KiB
  I can see two approaches, and they differ in how much
  they change the ledger's contract.
  ▸ question  Which approach?  [in-place | new-table]

suspended · waiting for you
  answer with:  aeon answer 8b1e4c
```

The process is gone. `aeon answer 8b1e4c in-place` resumes it — possibly on another day.

## The demo

See [`DEMO.md`](DEMO.md): a step asks and exits with no process; answering resumes it; a file changed while it
waited is reported; siblings execute before a suspension; bounds do not reset; and compaction is shown by what
it records.

## Testing

| What | How |
| --- | --- |
| **Identity — the keystone** | A suspended-and-resumed step is deep-equal to one that never suspended: transcript, outcome, handoff. Reconstructed through JSON in a **fresh object graph** sharing no reference with the original |
| Identity with compaction | The same, for a step that also compacted |
| Identity across two suspensions | The same, plus bounds exhausting at the **original** limit |
| Suspension shape | The returned transcript ends on the `question`'s `tool_use` with no result, and `ValidateTranscript` accepts it |
| Siblings first | `read` + `question` in one turn: `read` runs, its result is appended, **then** suspension. Asserted on the transcript's final shape |
| `question` never executes | A tool spy at zero invocations; its execute panics if reached |
| No re-run | A resumed step's tool spy shows zero calls for tools already in the transcript |
| Changed file | A file mutated during suspension produces a **`user`**-role note naming it, and the transcript still quotes the original |
| Deleted file | Reported as missing, **distinct** from changed |
| Bounds survive | Suspending at iterations 3 and 6 of a `MaxIterations: 8` step exhausts at 8 |
| A second question | Suspends again, consuming an iteration |
| Compaction safety | Over a **generated corpus**: no `thinking` block in the last assistant turn altered; no `tool_use` orphaned; head sections survive |
| Selector, not check | A transcript with no admissible range returns a refusal **with the `Client` spy at zero** |
| Summary cost | Added to the step's total |
| Order of persistence | A forced failure between the transcript write and the question write leaves a recoverable state |

## Out of scope

- **Crash recovery, re-run classes, receipts.** S7. This slice handles a step that *chose* to stop; S7 handles
  one that was killed.
- **The bash policy and the `ask` verdict.** **S11.** `question` here is a tool a model calls deliberately, not
  a permission prompt.
- **An unattended answer policy** (P14). Every question at S6 is asked by a step a human started, so it simply
  waits. Unattended loops are S17.
- **The app.** S9. Answering is `aeon answer` for now.
- **Blueprints, phases, gates.** S8. A gate is a question, but there is no blueprint to place one in.
- **Seven of nine tools.** `question` makes three: `read`, `bash`, `question`. The rest are S11.

## Consequences for later slices

| Slice | Consequence |
| --- | --- |
| S7 | Recovery reaches `ResumeStep` through a second door; the machinery is already here |
| S8 | A phase gate is a `question` with `kind: 'gate'` and no `tool_use_id` — S4's nullable column exists for it |
| S9 | The app answers questions instead of `aeon answer`; the `question.*` events are already emitted |
| S11 | An `ask` verdict becomes a `question` with `kind: 'permission'`, reusing all of this |
| S17 | Adds the unattended answer policy; the question rows already carry `kind` |

## Open questions

None. The compaction margin and the token-estimate constant are numbers to calibrate, and the QA suite is
written to change them.

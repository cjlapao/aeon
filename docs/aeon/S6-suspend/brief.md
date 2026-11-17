# Brief — S6 Suspend, question, compaction

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Implements deferred:** [`docs/_archive/aeon-v1-horizontal/03-step-runtime/`](../../_archive/aeon-v1-horizontal/03-step-runtime/) K11, K12, K14–K18.
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — §3 is binding.
**Stage:** 2 · Bicycle, first of four. Depends on S1–S5.

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief corrects it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** §2's first item is the slice's **keystone**, and it is the only DoD item in this plan that cannot be
> satisfied by a test running in one process.

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
| **D1** | **Resume identity — the keystone.** A suspended-and-resumed step is **deep-equal** to one that never suspended: transcript, outcome, handoff. The transcript, manifest and answer are round-tripped **through JSON into a fresh object graph sharing no reference with the original**, and the `ResumeRequest` is built only from those. Every other test in this slice can pass against an implementation that depends on something surviving in memory; only this one cannot. |
| **D2** | **Identity holds when the step also compacted**, and **across two suspensions** — the second additionally asserting bounds exhaust at the **original** limit. |
| **D3** | **A suspended step's process exits.** Asserted by observing no process and no parked goroutine after the return, not by the absence of an error. |
| **D4** | **The suspended transcript ends on the `question`'s `tool_use` with no result**, and S3's `ValidateTranscript` **accepts** it. |
| **D5** | **Siblings execute before the suspension** (K25). `read` + `question` in one turn: `read` runs, its `tool_result` is appended, **then** the step suspends. Asserted on the transcript's final shape — exactly one unanswered `tool_use`, and it is the `question`. |
| **D6** | **`question` is never executed.** A tool spy at zero invocations, and its execute panics if reached (W15). |
| **D7** | **A resumed step re-runs no tool** (K15). A tool spy shows zero calls for every tool whose result is already in the transcript. |
| **D8** | **A changed file produces a `user`-role note**, and a deleted one is reported as **missing, distinctly**. The transcript still quotes the **original** content. `system` role is a failure — Anthropic hoists it to the head. |
| **D9** | **Bounds do not reset.** Suspending at iterations 3 and 6 of a `MaxIterations: 8` step exhausts at **8**, not 11 or 14. The deadline is unchanged. |
| **D10** | **Compaction never breaks a transcript.** Over a **generated corpus** in the hundreds: no `thinking` block in the last assistant turn altered; no `tool_use` orphaned from its `tool_result`; head sections survive. |
| **D11** | **The selector refuses without paying.** A transcript with no admissible range returns a refusal with the `model.Client` spy at **zero** calls — proving the exclusions are enforced by the selector and not by a check after the summary was bought (K16). |
| **D12** | **A step that cannot compact yields `blocked`** with a reason, never a truncation (K28). |
| **D13** | **The summary's cost is added to the step's total.** Asserted by comparing a compacted step's cost against the sum of its model calls. |
| **D14** | **Persistence order is transcript-then-question.** A forced failure between them leaves a recoverable state — a transcript with no question re-runs its last turn; the reverse is unrecoverable. |
| **D15** | **The `answer` method resumes the step**, and answering twice is refused with the existing timestamp (reusing S4's guarantee). |
| **D16** | **`question.*` events reach a subscriber**, and `PROTOCOL_VERSION` is unchanged. |
| **D17** | **No unit test contacts a provider.** The suite passes with no network and no keys. |
| **D18** | **`make test` and `make lint` are green**, `make fmt` leaves no diff. |

D1 is the slice. D5, D8 and D11 are the three most likely to be got subtly wrong, and each fails somewhere other
than where it is written.

**D1–D17 must each exist as named tests.**

D18 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

The bicycle begins with what the skateboard cannot do: **stop and ask.**

S5 built a step that runs to completion or fails. This slice lets it suspend on a question, exit its process
entirely, and be resumed hours later — which is P2, and the property every competitor structurally lacks because
they keep session state in a process.

Most of the machinery exists. S4 has `question`, `chat_message`, the transactional pairing and derived
`IsAwaiting`. S5 shipped `StepResult` as a union with a `suspended` variant nothing produced, precisely so this
slice fills it in. **What is new is the runtime path**: detect a question, exit cleanly, reconstruct from
persisted state alone.

**Compaction is folded in** (K27), deferred from S5 where `MaxIterations` was the only bound. It belongs here
because **suspension is what makes it necessary** — a step spanning a human round-trip runs many more iterations
than an unattended one — and because building them together means the keystone identity test covers a compacted
step from the start rather than being extended later.

## 4. Goal

A step asks, its process exits, a human answers hours later, and it continues from exactly where it stopped —
with a long step compacting itself without producing a transcript the provider rejects.

## 5. Shape and the seam

```
loop.go ──▶ partition tool calls: question vs the rest
              │
              ├── execute the rest SERIALLY, append results   (K25)
              └── question present → return { Kind: suspended }
                          │
protocol/step.go ─────────┴──▶ persist transcript+manifest, then store.Ask, then status='suspended', EXIT
                                             │
protocol/answer.go ──▶ store.Answer ──▶ ResumeStep ──▶ RunLoop   (the same loop — K26)
```

### The seams

**Upward — two doors, one loop** (K26). `RunStep` assembles context and may have no transcript; `ResumeStep`
assembles **nothing** and requires both. Two loop implementations would drift, and D1 is precisely the test a
drift would fail.

**Sideways — S4's `Ask` and `Answer`.** Already transactional, already refusing a second answer. This slice does
not reimplement either.

**Downward — `model.Client` for the compaction summary.** The same client, so the summary's cost lands on the
same path.

**Outward — `answer` is additive** to S2's method table, so `PROTOCOL_VERSION` does not move.

## 6. Locked decisions

- **K11** — suspend by **returning**. Never block, never throw.
- **K14** — the changed-file note is **`user`**-role.
- **K15** — resume re-runs no tool.
- **K16** — the **selector** enforces the exclusions; a post-hoc check is not a substitute.
- **K17** — the budget is estimated. **Do not add a tokeniser.**
- **K25** — siblings execute first, then suspend.
- **K26** — `ResumeStep` shares S5's single loop.
- **K28** — cannot compact → `blocked`, never a truncation.
- **W15** — `question`'s execute panics.

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | `ResumeStep` is a separate entry point | Both doors call **one** `RunLoop`, and a test asserts there is exactly one loop function. Two copies would drift, and D1 is the test that would fail months later without saying why. |
| O2 | The summary goes through the same `Client` | Its `Usage` is also **recorded separately** on the step, not only added to the total. A user asking "why did this step cost that" needs to see the compaction as a line, not a discrepancy. |
| O3 | Changed files produce a note | Also **cap the number of paths named**, and say how many were omitted. A step declaring two hundred files that all changed produces a note longer than the context it protects. |
| O4 | A second question suspends again | State the **iteration accounting**: a suspension consumes an iteration, so a model looping on questions exhausts `MaxIterations` rather than running forever. Assert it. |
| O5 | The design does not say what happens if the answer is empty | An empty answer is **valid** and passed through as an empty `tool_result`. Refusing it would make "I don't know" unexpressible, and a model handles an empty answer better than a step that will not resume. |
| O6 | The design does not bound the compaction summary's own output | Bound it, and if the summary itself exceeds the bound, treat it as an inadmissible compaction (K28) rather than storing a truncated summary. A truncated summary is a transcript that lies about what it replaced. |

## 8. Non-goals

- **N1 — No crash recovery, re-run classes or receipts.** **S7.** This slice handles a step that *chose* to stop.
- **N2 — No bash policy, no `ask` verdict, no permission prompt.** **S11.** `question` here is a tool a model
  chooses to call.
- **N3 — No unattended answer policy** (P14). Every question waits, because a human started the step. S17.
- **N4 — No app.** S9. Answering is `aeon answer`.
- **N5 — No blueprint, phase or gate.** S8. A gate is a question with no blueprint to hold it.
- **N6 — Six of nine tools still absent.** `read`, `bash`, `question`. A step still cannot change a file.
- **N7 — No tokeniser** (K17).
- **N8 — No compaction of the chat thread.** Only a step's transcript. The thread is the human's record and is
  never rewritten.

### Standing non-goals

- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never run `git clean`.**
- No test contacts a provider, the real store, or the working repository.

## 9. Repo invariants

**I1 — One loop** (O1, K26). Asserted by grep. This is the invariant D1 exists to protect.

**I2 — The exclusions live in the selector only** (K16). A post-hoc validation **in addition** is acceptable; one
**instead** is not, because it can only reject after paying.

**I3 — `question`'s execute is unreachable** (W15), and a test proves the panic rather than the absence of a call.

**I4 — The manifest is never mutated.** `Rehash` compares and reports (S5's T2). Freezing means freezing, and
D9 depends on it.

**I5 — `IterationsUsed` and `DeadlineAt` live on the manifest**, never in a local. That is what makes them
survive a suspension.

**I6 — No ambient state.** No clock read outside an injected one — D1's deep equality requires determinism.

**I7 — `ResumeStep` assembles no context.** Asserted with a spy on `AssembleContext` at zero calls; "does not
assemble" is otherwise invisible.

**I8 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/03-step-runtime/design.md`** — K11–K20, and its §Resume, concretely and §Compaction.
2. **`docs/_archive/aeon-v1-horizontal/03-step-runtime/tasks/T6.md` and `T7.md` and `T8.md`** — resume, compaction, and the
   **keystone identity test**. T8 is the closest thing to a specification of D1 that exists.
3. **`internal/runtime/`** as S5 left it — `loop.go`, `runstep.go`, `context.go`, and `StepResult`'s unused
   `suspended` variant.
4. **`internal/store/chat.go`** (S4 T7) — `Ask`, `Answer`, `PendingQuestion`, and their transactional guarantee.
5. **`internal/model/format.go`** (S3) — why `ValidateTranscript` accepts a transcript ending on an unanswered
   `tool_use`. That rule exists **for this slice**.
6. **`internal/model/adapters/anthropic.go`** (S3 T8) — M5's system hoisting, which is why K14 uses `user` role.
7. **`internal/protocol/ask.go`** and **`step.go`** — the streaming and persistence patterns.

### 10.2 The patterns to copy

- **S5's injected dependencies and fake client** — D1's determinism depends on them.
- **S4's transactional pairing** for `Ask`; do not reimplement it.
- **S3's exact-string assertions** for `thinking` signatures.
- **Property-based testing** for D10, which is new to this plan and the right shape for it.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | Exactly how does `ValidateTranscript` accept a suspended transcript, and what does it reject? | `internal/model/format.go`. D4 is written against it. |
| Q2 | Where does the route's context window come from, and does `Route` carry one? | `internal/model/types.go`. **If it does not, this slice adds it** — the estimate is a fraction of *something*. |
| Q3 | What is the token estimate's heuristic and its safety margin? | Yours to choose. State both, and state they are uncalibrated. |
| Q4 | Does `Rehash` (S5 T2) already return `changed` and `missing` separately? | `internal/runtime/context.go`. |
| Q5 | What exactly does S4's `Answer` refuse, and what does it return? | `internal/store/chat.go`. D15 reuses it rather than reimplementing. |
| Q6 | How does S5's `RunLoop` signature accommodate a resumed `LoopState` without change? | `internal/runtime/loop.go`. If it needs changing, say how — I1 forbids a second loop. |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q7 | How does D1 construct the `ResumeRequest` **only** from reconstructed values? Name the mechanism — a scope with no access to the originals is the expected answer. |
| Q8 | What does the generated corpus for D10 vary, and how many transcripts does it cover? |
| Q9 | What bounds the compaction summary's own output (O6), and what happens when it exceeds that? |
| Q10 | How many changed paths does the note name before summarising the rest (O3)? |

---

## 11. Behavioural contract

- A `question` tool call is **never executed** (D6, I3); siblings execute first (D5, K25); then the step returns
  `{Kind: suspended}` (K11).
- The returned transcript ends on the `question`'s `tool_use` with no result, and validates (D4).
- The caller persists **transcript, then question, then status** (D14).
- `ResumeStep` rehashes, appends a **`user`**-role note for changed and missing paths kept **distinct** (D8, O3),
  appends `tool_result(toolUseID, answer)`, and calls `RunLoop` with the manifest's existing bounds (D9, I5).
- An empty answer is valid (O5).
- A second question suspends again, consuming an iteration (O4).
- Compaction triggers on an **estimated** budget (K17); the **selector** excludes the head, the last assistant
  turn's `thinking`, and any `tool_use` whose result falls outside (K16, D10); no admissible range yields
  `blocked` (K28, D12); the summary's cost is added **and recorded separately** (D13, O2); a summary exceeding
  its own bound is inadmissible (O6).
- `answer` resumes; a second answer is refused with the existing timestamp (D15).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **the keystone identity test is its own task** (D1, D2), after both doors exist. It is the slice's
  reason to be trusted and deserves not to be a bullet in another task's list.
- **R5** — compaction's **selector** and its **summary** are separate tasks. They fail differently: one is
  exclusions, the other cost and replacement.
- **R6** — the selector's task carries D10's **property-based** test over a generated corpus, not examples. The
  transcripts that break compaction are shapes nobody enumerates.
- **R7** — no task over roughly 300 changed lines or 12 files without justification.
- **R8** — `question` the tool is its own task, before suspension detection needs it.
- **R9** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure",
  "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md` with real keys and amends the QA suites.

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines or ~12 files without justification (R7) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | `grep -c "for {" ` over your planned runtime files | Exactly one loop function (I1, O1) |
| A6 | For D1, state **how** the `ResumeRequest` is built only from reconstructed values | **A D1 test that resumes from the same in-memory objects blocks the plan.** It proves nothing about cold resume, which is the one property this slice exists to provide |
| A7 | For D11, confirm the assertion includes a `Client` spy at **zero** | A selector proven only by its output could be a post-hoc check that happened to reject early |
| A8 | Paste the answers to Q1, Q2, Q4 and Q6 | Four, from the source. **If Q2 found no context window on `Route`, name the task that adds it** |
| A9 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task implements crash recovery, re-run classes or receipts | N1 |
| B2 | Confirm no task implements a bash policy or a permission prompt | N2 |
| B3 | Confirm no task adds an unattended answer policy | N3 |
| B4 | Confirm no task touches the app | N4 |
| B5 | Confirm no task adds a blueprint, phase or gate | N5 |
| B6 | Confirm no task adds a tool beyond `question` | N6 |
| B7 | Confirm no task adds a tokeniser | N7, K17 |
| B8 | Confirm **no task compacts the chat thread** | N8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D17 to a task and a named test | §2 |
| C2 | For D1, confirm the reconstruction is asserted to **share no reference** with the originals | D1 |
| C3 | For D1 and D2, list every field **excluded** from the deep comparison, with the reason each must differ | Every exclusion is a hole in the assertion; `messages`, `outcome` and `handoff` may not be excluded |
| C4 | For D5, confirm the assertion is on the transcript's **final shape**, not on call order | D5 |
| C5 | For D8, confirm the assertion is on the message's **role** | D8 |
| C6 | For D9, confirm the test suspends **twice** and observes exhaustion at the original bound | D9 |
| C7 | For D10, describe the corpus and its size | R6, Q8 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm `question` the tool precedes suspension detection | R8 |
| D2 | Confirm the selector and the summary are separate tasks | R5 |
| D3 | Confirm the keystone is its own task, after both doors | R4 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm exactly one loop | I1 |
| E2 | Confirm the exclusions exist only in the selector | I2 |
| E3 | Confirm `ResumeStep` calls `AssembleContext` zero times, asserted with a spy | I7 |
| E4 | Confirm no clock is read outside an injected one | I6 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | Name the mechanism that builds the `ResumeRequest` only from reconstructed values (Q7). |
| F2 | State the corpus's variations and size (Q8). |
| F3 | State what bounds the summary's own output and what happens when it exceeds it (Q9, O6). |
| F4 | State how many changed paths the note names before summarising (Q10, O3). |
| F5 | Confirm an empty answer is valid and passed through (O5). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it **with real keys** and fixes whatever
does not match, recording which was wrong.

S6's demo has one step that cannot be faked and one that cannot be shown directly.

**§1's `pgrep` must return nothing.** That single check is the whole difference between Aeon and every tool that
keeps a session in a process, and it is the step a walker is most likely to skim.

**§6 demonstrates compaction by what it records**, because the thing compaction prevents is a failure you never
see. If the walk finds that harder to follow than the rest, that is expected — but the three checks inside it
(head sections intact, cost increased, signature unchanged) are what make it a demonstration rather than a claim.

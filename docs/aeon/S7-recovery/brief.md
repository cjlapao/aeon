# Brief — S7 Crash recovery

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Implements:** [`../ARCHITECTURE.md` §3a](../ARCHITECTURE.md#3a-crash-recovery-is-the-spine-with-a-different-trigger) and P27.
**Stage:** 2 · Bicycle, second of four. Depends on S1–S6. **Independent of S8.**

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief corrects it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** This slice's design **amended `ARCHITECTURE.md` §3a and two S4 documents before this brief was
> written** — those edits are already landed, not tasks. §9 I2 says what they were and why.

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
| **D1** | **Case determination is a pure function**, tested over constructed transcripts and receipt states — **every case and every receipt state, with no crash staged** (K30). Staging a crash at a precise instant is unreliable and staging every instant is impossible, so the hardest logic in the slice must be testable without one. |
| **D2** | **Case A resumes identically.** A step whose tail is complete finishes deep-equal to one never interrupted, reusing S6's identity machinery. |
| **D3** | **Case B re-issues, and reports both calls' cost.** The lost turn's tokens were billed; a recovery reporting only the second call under-reports real spend. |
| **D4** | **Case C, `safe` and `detectable`, re-runs.** And `edit` re-applied **fails loudly** rather than corrupting — the property that makes it `detectable` rather than `unsafe` (W17). |
| **D5** | **Case C, `unsafe` with no receipt, re-runs**, because nothing happened. |
| **D6** | **Case C, `unsafe` with a receipt **and** a completion: the captured result is appended and the step continues.** This is the receipt's reason to exist — without it this case is indistinguishable from D7, and a `go test ./...` that succeeded would be discarded. |
| **D7** | **Case C, `unsafe` with a receipt and no completion: the step fails**, with a reason naming the tool and the receipt state (K33). Never re-run, never marked `ok`, never left `running`. |
| **D8** | **A digest mismatch is not used.** A captured result whose `arg_digest` does not match its `tool_use` fails the step instead. Recovering a result onto the wrong call is worse than failing. |
| **D9** | **A suspended step is not scanned, resumed or failed.** Asserted by attempt: a `status='suspended'` step with an unanswered `question` survives a recovery pass untouched. |
| **D10** | **A receipt exists before the process spawns.** Asserted on ordering, not on presence afterwards — a receipt written after the spawn records nothing about the window it exists for. |
| **D11** | **Recovery is idempotent.** Run twice, the same outcomes. A crash **during** recovery leaves the remainder recoverable and the already-recovered untouched (K34). |
| **D12** | **Recovery never re-resolves context and never resets bounds.** Iterations and the absolute deadline come from the manifest, so a step cannot escape its budget by crashing repeatedly. |
| **D13** | **Recovery is observable.** Every step it considers is reported with its case before it acts. A silent multi-minute recovery on start is indistinguishable from a hang. |
| **D14** | **`--no-recover` reports the cases and changes nothing.** Seeing what recovery *would* do is the difference between debugging a recovery loop and guessing at one. |
| **D15** | **Every tool declares a `RerunClass`**, including the six that do not exist yet, and `bash` is the **only** `unsafe` one. |
| **D16** | **There is still exactly one loop and one resume path.** Recovery reaches `ResumeStep`; it does not call `RunLoop` directly. |
| **D17** | **No unit test stages a real crash.** `make test` passes with no `kill` and no process manipulation. |
| **D18** | **`make test` and `make lint` are green**, `make fmt` leaves no diff. |

D1, D6 and D7 are the three that matter. D1 because it makes the slice testable at all. D6 and D7 because they
are the same transcript state with opposite correct answers, and only the receipt separates them.

**D1–D17 must each exist as named tests.**

D18 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

**Almost all of this slice already exists.** A suspended step exits and resumes cold from persisted state alone
(S6); a crashed step is the same problem through a different door. So there is no recovery subsystem — a scan, a
decision, and S6's resume machinery.

What is new is one sharp problem. A suspension happens at a **clean point**; a crash lands **anywhere**, including
mid-`bash`. And at that point the transcript shows a `tool_use` with no `tool_result` — which is **the same shape**
whether the command never started, finished successfully, or died halfway.

Three states, one appearance. So the record must be made to say which, and that is the receipt.

**The receipt's middle state is the one worth building it for.** A crash in the window between a command finishing
and its result reaching the transcript is otherwise indistinguishable from a crash mid-command — and a `go test
./...` that took four minutes and passed would be discarded. With a receipt it is recovered.

**And the third state is where failing beats guessing.** `git commit` run twice is not recoverable by inspection.

## 4. Goal

A daemon that starts, finds every step its predecessor was running, and either resumes it, re-issues its last
call, recovers its captured result, or fails it honestly — naming which.

## 5. Shape and the seam

```
daemon start ──▶ scan status='running' ──▶ DetermineCase(transcript, receipts)   ← PURE (K30)
                                                │
                        ┌───────────────────────┼───────────────────────┐
                        A                       B                       C
                   ResumeStep            re-issue the call       by RerunClass (W16)
                        │                       │                  ┌────┴────┐
                        └───────────────────────┴──────────────────▶│ receipt │→ re-run
                                                                    │  three  │→ recover
                                                                    │ states  │→ fail
                                                                    └─────────┘
```

### The seams

**Upward — nothing.** Recovery is called by the daemon on start and by nothing else.

**Sideways — `ResumeStep`** (S6). Recovery reaches it rather than the loop, so there is still one loop **and one
resume path** (D16). A third path into the loop is how a resumed step and a recovered step start behaving
differently.

**Downward — a narrow `ReceiptWriter`** (K32). K6 says the runtime persists nothing and opens no store; a
one-method interface preserves that while letting the write happen where only the runtime knows a tool is about to
act.

**`DetermineCase` is pure** (K30, D1), which is the seam that makes the slice testable: every case and every
receipt state constructed directly, with no crash staged.

## 6. Locked decisions

- **K29** — recovery scans `status='running'`. `status` distinguishes crashed from suspended.
- **K30** — case determination is **pure**.
- **K31** — the receipt has **three** states.
- **K32** — receipts are written through a narrow injected writer; the runtime opens no store.
- **K33** — a recovery failure names the tool and the receipt state.
- **K34** — idempotent, committed per step atomically.
- **K35** — automatic, observable, refusable.
- **W16** — every tool declares a re-run class; `bash` alone is `unsafe`.
- **W17** — `edit` is `detectable`.

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | Case B re-issues the model call | It also **records that it did**, on the step, so the doubled cost has a cause. A step whose cost exceeds its visible turns with no explanation reads as a billing bug. |
| O2 | A receipt records `arg_digest` | The digest covers the **tool name and its arguments**, not the arguments alone. Two different tools called with identical arguments in one turn is unlikely and not impossible, and matching on arguments alone would cross them. |
| O3 | Recovery reports each step with its case | It also reports a **total and a running count** — "recovering 3 of 7" — because the useful question during a slow recovery is how much is left, not what the current one is. |
| O4 | `--no-recover` skips recovery | It must **leave the steps `running`**, not mark them anything. A flag that quietly re-labels state is a flag that loses information the next start needed. |
| O5 | The design does not say what happens to a step recovery cannot classify | An unclassifiable step — a corrupt transcript, a receipt referencing an absent `tool_use` — **fails with a reason saying so**, and recovery continues to the next step. One bad step must not stop recovery for the rest. |
| O6 | The six absent tools declare a class | Their declarations are **asserted against a committed list** (like S1's service fixture), so a tool arriving at S11 without a class fails a test written now rather than defaulting to something. |

## 8. Non-goals

- **N1 — No redo.** A step failed by recovery stays failed. S8's blueprint edges consume K33's reason.
- **N2 — No bash policy.** **S11.** A recovered `bash` is as unguarded as a fresh one.
- **N3 — No cross-machine orphan reconciliation.** The daemon owns loops (P29), so a `running` step whose daemon
  is gone is simply crashed. `(pid, host)` identity was v1's answer to a detached-child design.
- **N4 — No new tools.** Six get a class declared and nothing else.
- **N5 — No app.** S9.
- **N6 — No change to compaction.** A recovered step compacts as any other.
- **N7 — No change to `ResumeStep`.** Recovery reaches it; it is not modified.
- **N8 — No automatic retry of a failed step.** That is a policy decision and it belongs to a blueprint (S8).

### Standing non-goals

- **Never write outside `aeon/`**, except `docs/`, `docs/test_suites/` and the `ARCHITECTURE.md` amendment.
- **Never run `git clean`.**
- **No unit test kills a process** (D17). The QA suite does.

## 9. Repo invariants

**I1 — One loop, one resume path** (D16). Recovery calls `ResumeStep`. Asserted by grep.

**I2 — The receipt is a table, not a column on `step_message`.** A receipt must exist **before** the tool runs,
and the message row holding that tool's result does not exist until after it finishes. A column on an absent row
cannot record that something is about to happen.

`S4-store/design.md` said otherwise and **was wrong**; that line, S4's illustrative demo migration name, and
`ARCHITECTURE.md` §3a's two-state receipt rule were all **corrected when this slice's design was written**. They
are settled context, not work. Do not re-litigate the shape, and do not re-edit those files.

**I3 — `DetermineCase` is pure** — no store, no clock, no I/O. It is the only way the hardest logic here becomes
testable.

**I4 — The runtime opens no store** (K6, K32). Receipts go through the injected writer, asserted by grep.

**I5 — Each step's recovery outcome commits atomically** (K34). A crash mid-recovery is ordinary.

**I6 — Every tool's `RerunClass` is asserted against a committed list** (O6), so a new tool cannot arrive
classless.

**I7 — No test manipulates a real process** (D17).

**I8 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`../ARCHITECTURE.md` §3a** in full — the three cases and P27's classes. Note the receipt-as-column claim
   this slice corrects (I2).
2. **`internal/runtime/resume.go`** (S6 T3) — what recovery reaches, and must not modify.
3. **`internal/store/steps.go`** (S4 T5) — `step.status`'s values, which K29 depends on, and the manifest.
4. **`internal/store/migrate.go`** (S4 T2) — the ledger this slice adds a migration to, and the
   fresh-equals-migrated invariant it must not break.
5. **`internal/tools/`** (S5, S6) — the three existing tools, which gain `RerunClass()`.
6. **`internal/daemon/`** (S1, S2) — the start-up sequence recovery hooks into.

### 10.2 The patterns to copy

- **S4's migration discipline** — numbered, one transaction, ledger-recorded, and the equivalence test.
- **S6's identity machinery** for D2 — a recovered step is deep-equal to an uninterrupted one, same as a resumed
  one.
- **S1's committed-fixture pattern** for O6's re-run class list.
- **Discriminated results over errors**, as every slice does.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | Can a step be `running` with an **empty** transcript, and what does that mean? | `internal/protocol/step.go` (S5 T10) — it creates the row before calling `RunStep`, so yes. Say which case that is. |
| Q2 | Does `ResumeStep`'s signature accept a recovery's inputs unchanged, or does it need a parameter? | `internal/runtime/resume.go`. **If it needs one, that is a task** — N7 forbids reshaping it casually. |
| Q3 | What exactly distinguishes a suspended step's transcript from a Case C one, other than `status`? | Both have an unanswered `tool_use`. **If `status` is the only difference, say so explicitly** — it is load-bearing (K29). |
| Q4 | How does S4's fresh-equals-migrated test behave when a table is added? | `internal/store/schema_test.go`. This slice must not break it. |
| Q5 | What does the daemon's start sequence do, and where does recovery fit without delaying the socket? | `internal/daemon/`. A daemon that will not accept connections until recovery finishes is one nobody can inspect during a slow recovery. |
| Q6 | Is there an existing debug-crash hook, or does this slice add one? | The demo needs `AEON_DEBUG_CRASH_AFTER_TOOL` and `AEON_DEBUG_CRASH_DURING_RECOVERY`. |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q7 | What algorithm produces `arg_digest`, and what does it cover (O2)? |
| Q8 | Where do the debug-crash hooks live so they cannot fire in a release build? |
| Q9 | Does recovery run before or after the socket opens (Q5)? State the choice and its consequence. |
| Q10 | What happens to a step whose `context_manifest` is absent or unparseable — is that Case B, or unclassifiable (O5)? |

---

## 11. Behavioural contract

- Recovery scans **only** `status='running'` (K29, D9).
- `DetermineCase(transcript, receipts)` is **pure** and total (K30, I3), returning A, B, C-with-a-per-call plan,
  or unclassifiable (O5).
- Case A → `ResumeStep` (D2). Case B → re-issue, recording that it did (D3, O1).
- Case C, per unanswered `tool_use`, by `RerunClass`: `safe`/`detectable` re-run (D4); `unsafe` consults the
  receipt — absent → re-run (D5); present with completion → **append the captured result** (D6); present without →
  **fail naming the tool and the state** (D7).
- A digest mismatch fails the step (D8, O2).
- A receipt is written **before** the spawn (D10), through the injected writer (K32, I4).
- Bounds and context come from the manifest (D12).
- Each outcome commits atomically; recovery is idempotent (D11, K34).
- Every step considered is reported with its case, plus a total and running count (D13, O3).
- `--no-recover` reports and leaves `running` (D14, O4).
- An unclassifiable step fails with a reason and recovery **continues** (O5).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **`DetermineCase` is its own task**, and it is the one that carries D1's exhaustive table. It is pure,
  so its test needs nothing staged, and everything after depends on it being right.
- **R5** — the receipt **table** and the receipt **writing** are separate tasks. One is a migration, the other an
  ordering guarantee, and they fail differently.
- **R6** — **no task edits `ARCHITECTURE.md` or S4's documents.** Those corrections are already landed (I2). A
  task that re-edits them will conflict with settled text.
- **R7** — no task over roughly 300 changed lines or 12 files without justification.
- **R8** — the re-run classes are their own task, including the six absent tools and O6's committed list.
- **R9** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure",
  "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md`, which **requires real `kill -9`**, and amends the QA suites.

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines or ~12 files without justification (R7) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | `grep -rn "kill\|Signal\|os.Process"` over your planned **test** files | No hits (D17, I7) — a unit test staging a crash is unreliable and cannot be exhaustive |
| A6 | Enumerate D1's test table: every case × every receipt state | **A partial table blocks the plan.** This is the slice's hardest logic and the only place it can be tested completely |
| A7 | Paste Q3's answer | **If `status` is the only thing distinguishing a suspended step from Case C, say so explicitly** — the whole of K29 rests on it |
| A8 | Paste the answers to Q1, Q2 and Q5 | Three from the source. **If Q2 found `ResumeStep` needs a parameter, name the task** |
| A9 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task implements redo or automatic retry | N1, N8 |
| B2 | Confirm no task implements a bash policy | N2 |
| B3 | Confirm no task adds `(pid, host)` reconciliation | N3 |
| B4 | Confirm no task implements a new tool | N4 |
| B5 | Confirm no task touches the app | N5 |
| B6 | Confirm no task changes compaction | N6 |
| B7 | **Confirm no task modifies `ResumeStep`**, or names the task if Q2 requires it | N7, Q2 |
| B8 | Quote the `out_of_scope` lines covering N1–N8 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D17 to a task and a named test | §2 |
| C2 | For D6 and D7, confirm the **same** transcript state is used with different receipts | They are the same shape with opposite correct answers |
| C3 | For D9, confirm the assertion is **by attempt** — a suspended step put through a recovery pass | D9 |
| C4 | For D10, confirm the assertion is on **ordering**, not presence afterwards | D10 |
| C5 | For D11, state how a mid-recovery crash is staged **without killing a process** in a unit test | D11, D17 |
| C6 | For D15, name the committed list | O6, I6 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm `DetermineCase` is its own task and precedes the actions | R4 |
| D2 | Confirm the table and the writing are separate tasks | R5 |
| D3 | Confirm **no** task edits `ARCHITECTURE.md` or S4's design/demo | R6, I2 |
| D4 | Confirm the re-run classes are their own task | R8 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm recovery calls `ResumeStep`, not `RunLoop` | I1, D16 |
| E2 | Confirm `DetermineCase` has no store, clock or I/O | I3 |
| E3 | Confirm the runtime opens no store | I4, K6 |
| E4 | Confirm each outcome commits atomically | I5 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | State `arg_digest`'s algorithm and what it covers (Q7, O2). |
| F2 | State where the debug-crash hooks live and how they are excluded from a release build (Q8). |
| F3 | State whether recovery runs before or after the socket opens, and the consequence (Q9, Q5). |
| F4 | State what happens to a step with an absent or unparseable manifest (Q10, O5). |
| F5 | Confirm `--no-recover` leaves steps `running` rather than relabelling them (O4). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it and fixes whatever does not match.

**This demo cannot be done gently.** Every step involves `kill -9`, because a graceful shutdown is the case that
already worked. Two of its steps need the debug-crash hooks (Q6, Q8) to hit a window too short to hit by hand —
and §4 is the one that matters most, because it is the case the receipt exists for and the one whose absence would
throw away work that succeeded.

The walk must also confirm §5: **a suspended step is not touched.** That check looks like a formality and is the
one that prevents recovery from ruining every loop correctly waiting for a human.

# S9 The app — the demo

**Stage 2 · Bicycle, fourth of four. This is the bicycle**, and the halfway point of the plan.

Everything below is done **from the app**. If a step here needs a terminal, that is a finding, not a workaround.

```
make dev
```

The window opens. S1 built it; this is the slice where it does something.

---

## 1. Start a loop from the app and watch the ladder fill in

In **Loops**, choose **New loop**: pick `two-step`, type *"survey the Go files under internal"*, start it.

**You should see** the ladder appear immediately, then fill in **as it happens** — not on a refresh:

```
  survey      running     ⠋
  approve     pending
  summarise   pending
```

then

```
  survey      ok          SURVEY.md        $0.0312
  approve     waiting     approve-survey
  summarise   pending
```

**It failed if** you had to click anything to see it advance. Live updates come from **subscriptions only** (U3):
two update paths disagree, and the disagreement shows as a UI flickering between states.

**It also failed if** `pending` and a bypassed lane look the same. Run this again against `dev-loop` with
`mode: simple` and confirm a lane whose `when` was false reads **`not-needed`, with its condition shown** — not
`pending`, which reads as "still to come" for something that will never happen.

---

## 2. Answer a step question and a gate from the same thread, without being told which is which

Start a loop that will do both:

> *"survey internal, and ask me which package to start with"*

**You should see** two `ask` rows in the thread, at different times, **rendered identically**:

```
  step   survey       running
  ask    Which package should I start with?
         [ internal/store ] [ internal/runtime ] [ internal/loop ]
  you    internal/store
  step   survey       ok        SURVEY.md      $0.0312
  ask    Approve the survey and continue?
         [ approve ] [ revise ] [ abort ]
```

Click through both.

**The first resumed a step. The second advanced a loop.** Two different mechanisms — `ResumeStep` and
`AdvanceLoop` — reached by different doors, and **the app does not tell you which** (U14). That is the point: a
human answering something should not have to know what is asking.

**It failed if** the two are labelled differently, or if one opens a modal. A modal detaches the decision from
the context needed to make it (U4).

**Now the one that is easy to get wrong.** Answer a question, and while it is in flight answer it again (double
click, or two windows):

**You should see** the second refused, and **the thread unchanged until the daemon confirms**. No optimistic
render (U9, U19). An optimistic UI would show an accepted answer that was rejected, leaving you believing the
step resumed on your input.

---

## 3. Read a step's transcript

Click the `survey` step.

**You should see** the transcript as it happened — assistant text, each tool call with its input and result, the
declared context manifest, the iteration count, and the step's cost.

**Three things to check:**

- **A tool result that was long is truncated in the view but complete on expand.** A UI that silently shortens
  tool output is one you cannot debug from.
- **The manifest lists every declared file**, and a file that changed while the step was suspended is marked as
  changed (S6's K14).
- **If the step was compacted**, the view says so and shows the summary's cost as part of the total. A compaction
  that is invisible makes the cost look wrong.

---

## 4. Pull the socket out, and watch the UI go honest

This is the section that matters most, and it takes three parts.

**Part one — kill the daemon while a loop is running:**

```
pkill -9 -f "aeon serve"
```

**You should see the app go stale, not confident:**

```
  ⚠ not connected · last confirmed 12s ago · /run/user/1000/aeon.sock
     survey    running   (as of 12s ago)
```

The age keeps counting. **It failed if** the loop still reads `running` with no qualification. That is the most
believable lie in the product: a UI showing live state when it has not been able to confirm anything for forty
seconds (U13).

**It also failed if** the socket path is not shown. It is the first thing anyone debugging this needs.

**Part two — the wolf-crying case.** Restart the daemon, start a loop, and let it reach a **gate**. Now wait five
minutes and touch nothing.

**You should see it stay `current`.** No warning, no age, no stale badge.

**Silence is not staleness.** A loop waiting at a gate emits nothing for hours and is perfectly current. Staleness
is the connection being *unconfirmable*. Getting that backwards produces a UI that cries wolf until people stop
reading the warning — at which point part one's warning is worthless too.

**Part three — reconnect.** Restart the daemon.

**You should see** the app reconnect and **re-read**, arriving at the true current state — including anything that
happened while it was disconnected.

It does not resume from its last event `seq` alone. A filtered subscriber legitimately sees gaps in `seq` (S4), so
a gap the app cannot bound is a gap it must not paper over.

---

## 5. Cost, in three honest states

Open **Loop** and look at the cost column, then a step with each state.

**You should see three visibly different things:**

| State | Renders as |
| --- | --- |
| `known` | `$0.0312` |
| `partial` | **`at least $0.0180`** — a floor, worded as one |
| `unknown` | **the reason**, e.g. *"unknown — the provider returned no usage"* |

**It failed if** an unknown cost renders as `$0.00`, or as `—`, or as nothing at all.

That is the whole thesis of this slice. Four separate designs in Aeon paid to keep uncertainty visible — cost, the
`bash` receipt, blueprint validation, and connection liveness — and **the app is where that investment is
delivered or quietly discarded** ([P31](../ARCHITECTURE.md#8-product-decisions)). It gets discarded by accident,
because `unknown` becomes `—` and `—` becomes `0`, and nobody notices.

Check the total, too: a total over a `partial` step must itself read as a floor. A confident total over an
uncertain part is the same error one level up.

---

## 6. Validate a blueprint, in three states

Open **Blueprints**, select `dev-loop`, validate.

**You should see three counts, distinctly rendered:**

```
  valid          74 checks
  unverifiable   21        cannot verify — agent bodies arrive at S13; code actions at S16
  invalid         0
```

Click an `unverifiable` row: **it names the missing registry and the slice that brings it.** Toggle **strict**:
the same findings, a failing verdict.

**It failed if** `unverifiable` is styled as an error, styled as a pass, or omitted. The first two are the lies
S8 built three states to avoid.

Then select the broken fixture: **every diagnostic at once**, each locating its fault by path.

---

## 7. A recovery report, in the app

With a loop running, `pkill -9 -f "aeon serve"` mid-step, then `make dev`.

**You should see**, in **System**, what recovery did — per step, with its case:

```
  recovered 1 of 1
    4f2a1c   case A · tail complete · resumed
```

And kill it during a `bash` instead:

```
  recovered 0 of 1 · 1 failed
    9c14a2   bash receipt without completion — outcome unknown
             the step failed; nothing was re-run
```

**It failed if** the failed step reads as a generic failure. The tool name and the receipt state are the whole
point of S7 — *"it started and we cannot say what happened"* is a different fact from *"it failed"*, and only one
of them tells you to go look at your repository.

---

## 8. The app never reaches around the daemon

A check rather than a demo, and worth doing once:

```
lsof -p $(pgrep -f "aeon.*ui\|tauri") | grep -i "state.db\|aeon/state"
```

**You should see nothing.** The app has **no filesystem access to the store** (U16). Everything it knows arrived
over the bridge.

U8 said the UI never *writes* to the store. A second *reader* is just as bad — it is a second set of assumptions
about a schema that is not a shared contract, and it drifts silently.

---

## What this demo does **not** show

- **No diffs, no approval UI.** **S16.** U5's rule — approval flows through the question primitive, never a
  separate mechanism — is waiting for something to diff.
- **No permission prompts.** **S11.** A step attempting `git push` and the app asking is S11's demo.
- **No agent picker.** **S13.** Two handlers exist.
- **No blueprint builder.** Not promised for the alpha.
- **No remote daemon.** One per machine (P29).
- **Not signed or notarised.** S23.

---

## The bicycle

Stop here and take stock, because this is the halfway point and the claim is specific.

**Aeon is now genuinely useful for a narrow job:** run a scripted sequence of model steps against a repository,
watch it from a window, answer what it asks, and lose nothing to a crash. A person who has never seen the CLI can
do all of it.

**And nothing about method, agents, git or permissions exists yet.** That is Stage 3.

The thing to notice is what the bicycle already refuses to do: it will not tell you a cost it does not know, will
not claim a blueprint is fine when it could not check, will not guess whether a `bash` command ran, and will not
show you a state it cannot confirm. Those four refusals were built in four different slices for four different
reasons, and this is the first place you can see them as one decision.

---

## Replay-only acceptance · 2026-10-19

The user selected replay tests only. The product interactions below were checked against disposable state and a local fixture server; no provider key or live model call was used. Fixture setup and the diagnostic `lsof` check used a terminal; loop creation, inspection and answering used the app.

| Section | Evidence |
| --- | --- |
| 1. Loop and ladder | A real Tauri replay advanced through `survey`, gate and `summarise`; the list and ladder also have event-only update tests, including nested items and costs. |
| 2. Questions | A real replay showed the step question and gate in one thread, both answers retained in daemon sequence order. Two physical windows converged on one durable answer. Deterministic two-window UI replay shows the losing refusal and no optimistic `You` row. |
| 3. Transcript | Product replay tests cover full tool output on expand, changed and missing manifest observations, compaction summary and cost. |
| 4. Liveness | A Tauri window went stale with age, cause and socket path after daemon kill, then reread on restart. A waiting gate stayed `current` for 305 seconds of silence. |
| 5. Cost | Product replay tests cover known, unknown with reason, partial with “at least”, and a total that remains a floor. |
| 6. Blueprint | The desktop library was checked against seeded pinned blueprints. Tests cover strict verdicts and structured unverifiable/invalid diagnostics. |
| 7. Recovery | A physical SIGKILL after a `bash` receipt produced a case C report in System. Replay tests cover the failed receipt state and case B's uncertain prior cost plus reissue cost. |
| 8. Boundary | `lsof` on the live desktop process found no Aeon state file open. Port and capability tests forbid direct web filesystem access. |

All six product and bench surfaces were measured at 1024px with no horizontal overflow. The physical two-window click race did not display a refusal in the losing window because a live event may have removed its button first; the deterministic UI replay holds that view at the question and verifies the refusal path. This is the precise limit of the physical walk. `make ci` passed with 202 UI tests, Go and Rust suites, vet, generated contract check and build.

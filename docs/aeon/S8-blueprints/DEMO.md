# S8 Blueprint & the two-step loop — the demo

**Stage 2 · Bicycle, third of four.** *Everything until now ran one step. This is the first slice where a sequence
is something you author rather than something Aeon does.*

Use a repository you can throw away.

---

## 1. A three-lane blueprint runs, in order, and stops at a gate

```
make dev
aeon_source=$(pwd)
cd ~/throwaway-repo
cat $(aeon blueprint path two-step)
```

**You should see** the whole thing, and it should be short enough to read in one go:

```yaml
id: two-step
schemaVersion: 1
version: 1
lanes:
  - id: survey
    step: { kind: model-step, agent: default, produces: SURVEY.md }
  - id: approve
    step: { kind: gate, question: approve-survey }
    on_reject: { goto: survey, counter: survey_revisions, max: 2, then: block }
  - id: summarise
    step: { kind: model-step, agent: default, produces: SUMMARY.md }
questions:
  approve-survey:
    prompt: Approve the survey and continue?
    answers: [approve, revise, abort]
```

Now run it:

```
aeon loop start --blueprint two-step "survey the Go files under internal"
```

**You should see** `survey` run as a real model step, write `SURVEY.md`, and then the loop **stop**:

```
loop 3f81  two-step
  survey     ok        SURVEY.md          $0.0312
  approve    waiting   approve-survey
  summarise  pending
```

**It failed if** `summarise` ran. Lane order **is** the ladder order — there is no separate sequencing field — and a
gate that does not stop the loop is not a gate.

**It also failed if** `survey` reported `ok` without writing `SURVEY.md`. `produces:` is an **enforced
post-condition**, not documentation: the engine checks the path afterwards and turns a missing artifact into a
`reject` naming the file. That rule exists in the protocol prose today as *"no file on disk = the phase did not
happen"*, enforced by asking agents to be honest. Here it is enforced by the engine.

---

## 2. Answering the gate advances the loop

```
aeon loop show 3f81
aeon answer 3f81 approve
```

**You should see** `summarise` run and the loop reach `done`:

```
loop 3f81  two-step   done
  survey     ok        SURVEY.md          $0.0312
  approve    ok        approve            —
  summarise  ok        SUMMARY.md         $0.0288
                                   total  $0.0600
```

**The thing to notice:** you answered a **gate** with the same command that answers a **step's question** in S6.
Both are `question` rows. They resume through different doors — a step question re-enters the step, a gate advances
the loop — and `aeon answer` dispatches on `kind` so you never have to know which asked (L11).

Prove it:

```
aeon loop start --blueprint two-step "survey internal, and ask me which package to start with"
```

**You should see** the *step* suspend with a question mid-`survey`, answer it, and then later hit the *gate*. Two
questions, two mechanisms, one command.

**It failed if** the gate answer landed in the transcript as a `tool_result`. A gate has no `tool_use_id` — S6's
nullable column exists for exactly this — and attaching one to nothing is unrepairable.

---

## 3. Editing the YAML changes what runs — and leaves the running loop alone

This is the beat that looks like a bug until you understand it.

Start a loop and let it reach the gate:

```
aeon loop start --blueprint two-step "survey internal"
# wait for waiting/approve
```

Now edit the blueprint — add a fourth lane:

```yaml
  - id: verify
    step: { kind: model-step, agent: default, produces: VERIFY.md }
```

Answer the waiting loop:

```
aeon answer <id> approve
```

**You should see it finish with three lanes.** The new one does **not** appear.

Then start a fresh loop:

```
aeon loop start --blueprint two-step "survey internal"
```

**You should see four lanes.**

**Both halves are correct.** A blueprint is **pinned per loop, content and all**, at start (L6): a resumed loop must
run the blueprint it began with, or an edit could strand a half-built feature at a step that no longer exists.
Confirm it directly:

```
aeon loop show <old-id> --blueprint | head -5
```

**You should see the old content**, stored on the loop — not a reference to the file.

**And no code changed.** That is the whole reason the graph is data (L1): there are five protocol shapes in this
repository, and freezing one into code makes the other four second-class.

---

## 4. The real `dev-loop.yaml` validates — and says what it cannot check

```
aeon blueprint validate dev-loop
```

**You should see** something like:

```
dev-loop  schemaVersion 1  version 1

  valid          74 checks
  unverifiable   21 checks
    lanes[build].ladder[issue].action    forge.openIssue
      cannot verify — the code-action registry arrives at S16
    lanes[build].ladder[push].action     git.commitAndPush
      cannot verify — the code-action registry arrives at S16
    …
  invalid         0

OK (9 unverifiable — use --strict to fail on these)
```

**Two things matter here.**

**No `invalid` diagnostics.** This is the real blueprint — lanes, an iterating `build` ladder, capped reject edges,
conditional lanes, gates — and if the format cannot express it, the format is wrong. A toy blueprint proves the
interpreter; this proves the format.

**Twenty-one `unverifiable`, naming S13 or S16.** The validator does not pass a code action or future agent it cannot check, and it does not
fail a blueprint that is correct. It says which registry is missing and when it arrives (L12). Then:

```
aeon blueprint validate dev-loop --strict
```

**You should see it fail**, with those twenty-one findings. CI checks the committed exact diagnostic set; `two-step` passes `--strict`.

**It failed if** validation reported `OK` with no mention of the nine, or `FAILED` by default. Both are lies, and
the first is the dangerous one because it is reassuring. Aeon never reports "fine" for "I could not tell"
([P31](../ARCHITECTURE.md#8-product-decisions)) — the same three-state answer as cost and the `bash` receipt.

---

## 5. A broken blueprint produces every diagnostic at once, each with its path

```
aeon blueprint validate internal/blueprint/testdata/broken-fixture.yaml
```

That fixture trips several classes deliberately. **You should see all of them in one run**, not the first:

```
  invalid        10
    lanes[confirm].step                  gate cannot name agent, action or produces
    lanes[0].agents                      unknown key "agents"
    lanes[intake].on_reject.goto         target lane is absent
    lanes[intake].step.agent             unknown agent "absent-agent"
    lanes[intake].step.sets              status is not in the task machine
    lanes[intake].when                   invalid operator for mode
    lanes[work].iterate                  iterate must name a collection field
```

**Each one with a path.** L9's rule is that validation is loud and **at load** — an unknown agent or an uncapped
backward edge must fail before a loop starts, never mid-run with a half-built feature.

The two worth reading twice:

- **`agents:` for `agent:` is an error, not an ignored key.** The format has no extension point by design, because
  an unrecognised key silently ignored is how a blueprint comes to mean something other than what it says.
- **An unknown agent and an invalid task status both fail before execution.** The engine does not defer either error until a model step is already running.

---

## 6. `aeon loop show` renders the ladder

```
aeon loop show <id> --wide
```

**You should see** the shape, not just a list — lanes, the iterating lane's items, each step's state, its artifact,
its cost:

```
loop 7c02  dev-loop   waiting
  clarify      ok           prompt.md            $0.0104
  discovery    not-needed   (when: mode != simple)
  plan         ok           plan.md, tasks/      $0.1832
  plan-gate    waiting      approve-plan
  build        pending      iterate: tasks (3)
  qa           pending      (when: flags.integration_surface)
```

**`not-needed` and `pending` are different things**, and so are `not-needed` and `skipped`: a false `when` records
`not-needed`, a mode-driven skip records `skipped`. `PHASE_STATUS` already made that distinction and this is the
first slice where you can see it.

**It failed if** a conditional lane that did not run is shown as `pending` — that reads as "still to come" for
something that will never happen.

---

## 7. A code action is refused, loudly

Use the single-action fixture, which reaches the S16 boundary immediately without relying on future S13 agents:

```
aeon loop start --blueprint "$aeon_source/internal/blueprint/testdata/code-action.yaml" "open a pull request"
aeon loop show <id>
```

**You should see the loop stop with `blocked`**, naming the action:

```
loop 7c02  blocked
  reason: code action is not implemented until S16
```

**It failed if** the action was **skipped** and the loop continued. A skipped code action lets a loop reach `merge`
having never pushed, and report success. A refusal is not a skip (L13).

Notice `decide` still *returned* the action — its purity does not depend on what is built yet. The honest refusal
lives at the boundary that actually cannot act.

---

## What this demo does **not** show

- **No git, no forge, no PR.** **S16.** Named by blueprints, refused here.
- **No real agents.** **S13.** `agent: default` is the handler S5 built; the registry holds two entries, and 25
  arrive at S13 along with `by-tech-tag`.

As of the S9 follow-up on 2026-10-19, validation still reports these future agents as `unverifiable`, but starting a blueprint that names one now fails before a loop is saved. This prevents a new `conversational-map` run from reaching `triage` and blocking with `unknown handler "task-coordinator"`. The New loop form labels such blueprints **needs S13 agents**; `two-step` is the executable replay choice. Previously blocked loops remain in history.
- **No phases, redo rounds, or the full ladder.** **S17.** The vocabulary is here and `dev-loop.yaml` uses it.
- **No permissions.** **S11.** A blueprint routes; it never grants — so nothing here can widen what a step may do.
- **The Blueprints test bench is present.** **S9** builds the designed app; see [UI-QA-DEMO.md](UI-QA-DEMO.md) for this slice's tab.
- **No visual blueprint builder.** Not promised for the alpha. The manifest is what would make one tractable.

Next: **S9** puts all of S6–S8 in a window, and that is the bicycle.

## Findings from the replay walk (2026-10-18)

The repeatable, keyless walk is [`UI-QA-DEMO.md`](UI-QA-DEMO.md) and `aeon/scripts/qa-s8-blueprints.sh`. It passed through the real daemon and store with a local fixture server. The desktop visual and live provider prompts in that document remain manual checks.

| Prediction | Observed behavior and change |
| --- | --- |
| “Two-lane” example | The readable example has three ordered lanes: `survey`, `approve`, `summarise`. The heading and shipped YAML now say so. |
| `dev-loop` has nine S16 findings | It currently has **74 valid, zero invalid, 21 unverifiable** checks. The exact S13/S16 set is committed as `dev-loop-unverifiable.json`. Strict mode fails as intended. |
| Broken fixture has seven findings | The committed fixture reports **10 invalid** paths in one run. It is addressed by its testdata path rather than a standard blueprint name. |
| Run `dev-loop` to the first forge action | S13 agents precede that action. A single-action fixture proves the S16 refusal directly, with the position unchanged. |
| A `produces` step always has a reject route | The real derived blueprints include top-level artifact steps without one. An unhandled missing-artifact reject blocks at the same position; it cannot silently advance. This keeps D12's real-blueprint validation truthful. |
| Restarting a running loop can call its step again | S8 now links a newly created step ID to the loop before the model call. Startup waits for S7 recovery and consumes the saved result without a duplicate call. |

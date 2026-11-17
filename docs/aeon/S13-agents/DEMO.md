# S13 Agents — the demo

**Stage 3 · Car, second of four.** *Everything so far has run model steps. This is the slice where one of them writes
working software because a specialist was asked, not a generalist.*

> **Status: walked on this checkout 2026-10-25 (S13 T11)** through a live `aeon serve` on its own socket and state
> directory, plus the UI's own suite (431 vitest cases incl. the 4 retry cases). **§2, §3, §4, §6 walked fully**;
> §1's and §7's model-dependent claims — the Go the specialist writes, the answered gate, the live eligible-retry —
> **await a provider credential on this box** (the daemon's banner reads `routes none`); they are marked below with
> the exact operator commands, and nothing was claimed that did not run. Corrections the walk made to this file are
> marked *(walk correction)*. The UI half lives in [UI-QA-DEMO.md](UI-QA-DEMO.md). **Run it from the app too**: the test bench grew a **Demos** tab
(spec [2026-10-25-bench-demo-runner](../superpowers/specs/2026-10-25-bench-demo-runner-design.md); commits
`0ed0fc9` daemon/protocol, `a1df648` UI) that executes these steps through the socket — verdicts are
**met / refused-as-data verbatim / not runnable here**, the refusal sentences are the daemon's own, and the
MCP half is a labeled replay with a Go-test citation per row (`absent_test.go` stays true: the page never
sends a `tools.*` method).

For the manual product walk, use the persistent Go workspace, daemon, database, and provider route recorded in
[S9's UI QA guide](../S9-app/UI-QA-DEMO.md). Automated tests may use disposable repositories. Before running a
new S13 loop, reopen the saved S9 baseline and confirm its approved gate and two artifacts are still present.

---

## 1. A blueprint step dispatches to `golang-engineer`, and it writes working Go

Write a task spec — a real one, small:

> *Add a `--version` flag to `cmd/hello` that prints the module version and exits 0. No new dependencies.*

Start a loop on a blueprint whose build step is `agent: golang-engineer`:

```
aeon loop start --blueprint dev-loop --project "$HOME/aeon-qa-workspace" --tech go "add a --version flag to cmd/hello that prints the module version and exits 0, no new dependencies"
```

*(walk correction: there is no `task-build` blueprint — the shipped ladder is `dev-loop`, whose implement lane is the
`by-tech-tag` step; `--tech go` resolves it to `golang-engineer` at dispatch (T10 seam, commit `2832b7e`). Flags go
BEFORE the goal: the parser stops at the first positional argument. The QA guide uses a single-lane `by-tech-tag`
blueprint for a one-step rehearsal of just the dispatch.)*

**You should see** the step dispatch to that handler, and then the thing that has not happened before:

```
loop 4a17  task-build
  build   ok   golang-engineer   cmd/hello/main.go, cmd/hello/version_test.go   $0.42
```

**Then check the work:**

```
cd "$HOME/aeon-qa-workspace" && go test ./... && go run ./cmd/hello --version
```

**It should pass, and it should print a version.**

This is the first demo in the plan whose success condition is **software that runs**, rather than a mechanism behaving
correctly. Everything before it proved a capability; this proves the capability was worth having.

**It failed if** the step reported `ok` without the files existing — `produces:` is an enforced post-condition (S8) and
a missing artifact must have become a `reject` naming the file.

---

## 2. `by-tech-tag` picks a different handler, and refuses honestly

Give the same blueprint a Python task and watch the resolver:

```
aeon loop start --blueprint dev-loop --project "$HOME/aeon-qa-workspace" --tech python "add a --version flag to the CLI"
aeon loop show <id>
```

*(walk correction: `task-build` again → `dev-loop`; and `--wide` is not a flag — `loop show` already prints the
ladder with the agent column.)*

**Walked 2026-10-25 without a credential:** the resolver ran — a `--tech go` lane blocked at `unknown route`
(the provider layer), **not** at `unknown handler`, which proves `golang-engineer` resolved and was handed down;
the model's own output still needs a key.

**You should see `python-engineer`, not `golang-engineer`:**

```
  build   ok   python-engineer   cli/__main__.py, tests/test_version.py
```

`by-tech-tag` is a **named resolver** — a function from state to a handler name — and it is a **closed registry**, the
same reasoning as S8's conditions: adding one is a code change, deliberately, because a resolver that can be written in
config is a config file that runs code.

**Now the case that matters.** Ask for a tech nobody handles:

```
aeon loop start --blueprint dev-loop --project /tmp/aeon-ws --tech elixir "add a version flag"
```

*(walk correction: `task-build` again → `dev-loop`; any workspace you can point a loop at.)*

**You should see it stop, naming the tag and what does exist:**

```
loop 9b03  blocked
  reason: no handler for tech tag "elixir"
          available: go, javascript, python, react-ts, rust, sql, tauri-react,
                     tauri-vue, vue-ts, websocket
```

*(walk correction: the tag list is **sorted**, and prints double-quoted — as above, verbatim from the refusal; and
it lists **tags**, never handler names, which the operator never supplied.)*

**Walked 2026-10-25:** exactly this block, live — and the workspace stayed **empty**: no generic handler ran, no
`OUT.md`, no plausible Elixir. The same refusal reaches `loop.show` as `retryRefusal.kind = "not-missing-handler"`,
which is also §7's "an unrelated blocked loop must have no eligible retry action".

**It failed if a generic handler ran.** There is **no fallback** (A13), and the reason is worth understanding: a
generic handler writing Elixir produces something plausible, the loop reports `ok`, and the badness is discovered three
steps later and attributed to the model. A refusal here costs a minute; a fallback costs an afternoon and a wrong
conclusion.

---

## 3. A handler body is a fraction of its original — and the record says what went

```
wc -l opencode/agents/golang-engineer-loop.md
wc -l aeon/internal/handler/bodies/golang-engineer.md
```

**You should see the second is far smaller.** The original is 84 lines and roughly 60% of it is work the engine now
does.

Now read the drop record:

```
cat aeon/internal/handler/drops/golang-engineer.md
```

**You should see every removal categorised, in three categories** (A12):

```
RITUAL (deleted — the engine does this)
  caller guard checking who dispatched            → the blueprint dispatches
  pwd + loop_get + loop_readFile + loop_readTask  → context arrives resolved
  "re-read prior failures"                        → the transcript is the record
  event_append return checkpoint                  → the daemon emits events
  FINAL REPORT line                               → the handoff is structured
  loop_writeFile scratch note                     → no such tool

BECAME MECHANISM (deleted — a tool guarantees it now)
  L57a  "exit 137 is an OOM kill"                 → bash reports oomKilled (W5)
  L61   "never run git"                           → the allow-list has no git
  L63   "one task per turn"                       → the blueprint controls this

DOMAIN (kept)
  L57b  "...not a lint finding"  — an OOM-killed linter's silence
        is not a pass, and the fix is not in your code
  Go idiom, error-wrapping conventions, table-test shape,
  when a build tag is warranted, module hygiene
```

**Look at L57.** One line, **split across two categories.** *"exit 137 is an OOM kill"* is now W5's job — the tool
reports `oomKilled` and the model no longer decodes a signal number. But *"not a lint finding"* is the **consequence**,
and nothing reports that: an OOM-killed linter produced no findings, its silence is not a pass, and the fix is not in
the code.

Delete the decoding; keep the consequence. That is the judgement A4 said could not be automated, and it is why drop
records need line references granular enough to show **half a line** moving.

**The middle category is the point of this section**, and the split above is why it is subtle. Prose repeating a
mechanism is a second source of truth that drifts — and it drifts in the worse direction, because the prose is what the
model reads. But a line that is *half* mechanism cannot simply be deleted.

This repository's own QA suite caught that: `aeon-agents.md`'s QAC-05 requires the consequence be present and calls its
absence the worst of five possible losses, *"because a model treating an OOM kill as a lint finding will edit correct
code to satisfy a linter that never ran."* An earlier draft of A12 would have deleted the whole line.

**It failed if** the drop record does not account for the line-count difference. An unexplained gap is an omission
nobody reviewed, and A4's whole requirement was that a human diff each rewrite against its original.

---

## 4. The mechanical guard, pointed at an original

The one automatable check on 2,902 lines of prose rewriting:

```
aeon handlers verify --bodies aeon/internal/handler/bodies      # from the repo root
```

**You should see all 25 pass — exit 0 and silence.** *(walk correction: outside the handler directory the CLI
**refuses rather than verifying nothing** — name the directory; the refusal to verify nothing is itself the D1
behaviour.)* Then point it at an original — **with `--original`**, because originals carry pre-S13 front matter
(`mode`, `variant`) that a rewrite must not port (A1); without the flag the load itself refuses first:

```
aeon handlers verify --original --file opencode/agents/golang-engineer-loop.md
```

**You should see it fail, naming the tools that do not exist** *(the walk's actual lines:)*

```
refused: golang-engineer-loop · forbidden-tool · loop_get · no such tool exists or will — the capability it names has an Aeon mechanism named in the drop record
refused: golang-engineer-loop · forbidden-tool · memory_append · ...
refused: golang-engineer-loop · forbidden-tool · event_append · ...
refused: golang-engineer-loop · skill · dev-loop-protocol · nothing loads skills here; instructions the model needs are in the body or nowhere
refused: golang-engineer-loop · ritual · FINAL REPORT · the handoff is structured (T3), not a prose line
```

**Walked 2026-10-25:** corpus exits 0; the original exits **1** naming ≥3 forbidden tools, the three measured
tokens, the skill and the ritual — across all 26 originals in CI (`TestVerifyFailsOnAnOriginalBody`).

**That failure is the whole reason a verbatim port was never an option.** Measured across the originals: **all 26**
reference `loop_get`, **all 26** reference `memory_append`, **all 26** reference `event_append`, and **all 26** open by
loading a 793-line skill. Not most — every one.

So a port is not a quality choice with a cheap alternative. The cheap alternative **does not run**: the model would
call `loop_get`, fail, and spend its iterations discovering that.

**And this is why the guard is trustworthy.** A check that passes everything proves nothing; a check no original body
can pass is measuring something real (A11).

---

## 5. The handoff is structured

*(walk correction: there is no `--handoff` flag — the shown schema is what the model receives, carried in
`StepHandler.HandoffSchema`; the honest checks today are the body's `handoff:` front matter, the tests
(`TestHandoffSchemaReachesTheModel`, `TestShownSchemaAndValidatorSpecAgree`), and the completed step's saved
result. The `--handoff` view awaits a step that has completed — a credential matters.)*

**You should see** the four-outcome vocabulary plus the fields the handler declared:

```
outcome:  ok
reason:   flag added, tests pass
fields:
  summary:      Added --version to cmd/hello, reading the module version …
  tests_added:  cmd/hello/version_test.go
```

Then make a handler promise a field and not produce it.

**You should see a `reject`**, not a silent gap. A handler that declares a summary and returns none has not finished
its job, and the next step is entitled to what it was promised.

**No `pr_summary` yet** — that arrives with **S16**, which needs somewhere to put it. The mechanism is here; the field
is not. *(walked 2026-10-25: `grep -rn pr_summary aeon/internal/handler/bodies/` prints **nothing** — and the loader
**rejects** a body that declares it (`prematureFieldPrSummary`, N4), so the absence is enforced, not accidental.)*

---

## 6. `opencode/` is untouched

```
git status --porcelain opencode/
```

**Nothing.**

The 26 originals and the skills are still there, still working, still driving the loop that is building their
replacement (A10). They retire at cut-over, not here.

**Including `dev-loops-aeon`.** A2 says the 16 `mode: primary` agents become blueprints — and the sixteenth is the one
this project wrote to build Aeon. Retiring it in the slice it is building would be a memorable way to stop work
(A15).

```
ls aeon/internal/handler/bodies/*.md | wc -l   # 25
aeon handlers list | grep -c .                  # 27 — read the correction
ls opencode/agents/*-loop.md | wc -l            # 26
```

*(walk correction: `handlers list` counts the whole registry — the 25 bodies **plus the two pre-S13 presets**
`default` and `boss` — so it prints **27**. The 25 claim is about bodies; count the bodies directory, or read the
list knowing the presets are the daemon's own two old names, not S13 handlers.)*

**Twenty-five and twenty-six.** The difference is `git-loop`, which becomes **no handler at all** (A3) — its work
becomes S16's code actions, because opening a pull request is a mechanical call rather than a judgement.

---

## 7. Continue the same UI QA history

Use the same daemon endpoint, state database, configured provider route and QA workspace from
[S9's UI QA walkthrough](../S9-app/UI-QA-DEMO.md). This is a live manual walk; no fresh state directory is needed.
Record the new loop URLs beside the S9 baseline URL.

1. In **Loops**, reopen the saved S9 `two-step` loop. Its `survey → approve → summarise` ladder, confirmed
   `approve` answer, pinned source, `SURVEY.md` and `SUMMARY.md` must still be visible.
2. In **Blueprints**, inspect `conversational-map`. Four of its five specialists are real now
   (`debugger-engineer`, `code-reviewer`, `security-engineer`, `by-tech-tag`) — but its `triage` lane names
   `task-coordinator`, **which S13 did not ship**, and starting it is refused: `unavailable-handler:
   lanes[triage].step.agent cannot run: agent body not shipped — S13 named it a follow-up (no body file in the
   registry)`. *(walk correction, 2026-10-25: the earlier wording "arrives at S13" became a lie the day S13
   shipped; the refusal now names the follow-up honestly, and the five unshipped bodies — brief-author,
   product-manager, project-manager, proposal-author, task-coordinator — are recorded as a STATUS follow-up.)*
3. If your S9 notes include the historical loop blocked at `triage` by `unknown handler "task-coordinator"`, open
   **that saved loop**, inspect its pinned source and position, and use the explicit retry action from T10. The
   existing loop ID must advance from the blocked `triage` item; no completed receipt may be repeated. Reopen it
   in a second window to verify both windows converge on one outcome. An unrelated blocked loop must have no
   eligible retry action. If there is no historical loop, record “historical retry not walked” and use the now
   startable blueprint as the fresh-history check.
4. Start a small Go specialist build loop against the **same** workspace and route. Example goal:

   > [QA-S13-GO] Add a `--version` flag to cmd/hello that prints `aeon-qa 0.1.0` and exits 0. Add a Go test for the
   > flag. Keep the existing default hello output. Run `go test ./...` and report the result in the step handoff.

   The build step must show `golang-engineer`, its transcript and handoff. The changed source and test must exist
   in the QA workspace, and `go test ./...` must pass there. Save this loop URL for the next stage.

T11 writes the full `S13-agents/UI-QA-DEMO.md` when implementing this slice, with the actual blueprint and
field values available then. This section is the acceptance target until that implementation exists.

---

## What this demo does **not** show

- **No git, no PR.** **S16.** `git-loop` has no handler here precisely because its work is not a handler's.
- **No full ladder.** **S17.** One build step, dispatched correctly, is what this slice proves.
- **No blueprints for the 16 primaries.** S17 authors the ones the ladder needs; this slice retires none.
- **No skill retirement.** Their content was distributed into the 25 bodies; nothing was deleted (A10).
- **No `pr_summary`.** S16.
- **No spawning or delegation.** A1: nothing nests, so this is a **registry**, not a lifecycle manager — no spawn, no
  abort, no per-subtree cost rollup, because there is no tree.
- **No agent authoring UI.** Never promised for the alpha.

Next: **S16** gives the loop git and a forge, and a loop opens a real pull request.

## §S17 — the follow-up five, walked from outside (2026-10-26)

**What this section is.** S13's walk proved the twenty-five. Six tasks later
(T12–T17, the follow-up pass added 2026-10-26) the five bodies S13 named and
did not ship exist — this walk proves them the way a user meets them: the
socket, not the registry.

**Setup — isolated, as every walk here does.** A fresh binary, a throwaway
state and runtime root, and the operator's own daemon untouched:

```bash
ROOT=/tmp/opencode/s17-t17
mkdir -p $ROOT/{bin,runtime,state} && chmod 700 $ROOT/runtime
go build -o $ROOT/bin/aeon ./cmd/aeon
XDG_RUNTIME_DIR=$ROOT/runtime XDG_STATE_HOME=$ROOT/state $ROOT/bin/aeon serve &
export XDG_RUNTIME_DIR=$ROOT/runtime XDG_STATE_HOME=$ROOT/state
```

**The four starts (the walk, verbatim).** All four blueprints that ever
named a follow-up, on the same fresh state — the preflight that once refused
`triage` with `unknown handler "task-coordinator"` now admits everything:

```
$ aeon loop start --blueprint development-map-direct --with task=t17-proof "preflight proof for development-map-direct"
loop 6ecbc25167276952b1698e36c92d2719  development-map-direct  running
$ aeon loop start --blueprint conversational-map --with task=t17-proof "preflight proof for conversational-map"
loop dae7d5897434b43106484262278185a6  conversational-map  running
$ aeon loop start --blueprint proposal-brief --with task=t17-proof "preflight proof for proposal-brief"
loop 13a3ca2dca4d07fa646850f70805a852  proposal-brief  running
$ aeon loop start --blueprint development-map-pipeline --with task=t17-proof "preflight proof for development-map-pipeline"
loop 2578874731caa8bcac082a12350726ea  development-map-pipeline  running
```

Zero refusals — and the record insists on the honest half: this box's fresh
state has **no provider credential** (`routes none`), so the four loops sit
at `preflight`, which is exactly what preflight means. **What is walked:**
validation with the real registries, the pin, the durable rows, zero
agent-ground refusals. **What is OWED, per the standing ruling** (scripted
provider only; never claim what this box cannot run): the five bodies
producing real artifacts (proposal.md, brief.md, feature.md, tasks/, the
triage handoffs) against a live route. Re-walk commands, verbatim:

```bash
# after configuring a route (aeon routes add …):
aeon loop show 6ecbc25167276952b1698e36c92d2719   # triage → dispatch
aeon loop show 2578874731caa8bcac082a12350726ea   # feature.md → tasks/
```

**Findings.**

| # | Finding | Disposition |
|---|---|---|
| F-α | A lane being runnable is not the loop completing. Six tasks of body-shipping change what `loop start` will accept; only a model walking the ladder changes what a loop **does**. Every completion sentence above is marked OWED with its command. | Disclosed, per the standing ruling. `produces:` cannot assert the brief's verbatim constants either (blueprint GAP-6) — the body carries them so trimming is detectable by reading, and the drop record says so. |
| F-β | The four `loop start`s all passed `--with task=t17-proof`; nothing observed distinguishes loops whose `with` entries a later dispatch will demand. That check is a dispatch concern and was NOT walked (no model). | Recorded as the OWED half, not papered as preflight coverage. |

**The record this section closes.** STATUS's S13 walk paragraph keeps its
history (it was true on its walk day) with a dated **Closed 2026-10-26**
parenthetical naming all five commits; the slice banner in TASKS.md carries
the follow-up pass; the agents' QA suite gained QAC-34–40 (bodies under the
guards, the named-five floors 6/5/4/3/3, the retry flip, the third state
surviving an empty `futureAgents`, the four-blueprint zero/zero, the F-α
disclosure, the nil-versus-empty per-kind rule). `futureAgents` is empty —
and stays a mechanism, pinned by name on both sides of its door.

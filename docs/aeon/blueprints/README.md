# Blueprints derived from the real protocols

**Status:** reference artefacts. Not yet importable — `aeon/` does not exist. These are copied into
`aeon/blueprints/standard/` when S8's T12 and S17's T7 are built.
**Date:** 2026-09-07

---

## What these are, and why they exist now

S8 made the loop's graph **data** on the strength of one argument: there are several real protocol shapes in this
repository, and *"freezing one into code would make the others second-class"* (L1). S8's L10 then said two blueprints ship,
because *"one instance is indistinguishable from a hardcoded graph with extra ceremony."*

**That argument was never tested against the real protocols.** These files test it: each is derived from a skill that is
actually driving agents today, and **eight places where the format could not express something** came out of the exercise.
Finding them now costs a comment; finding them at S17 costs a format change with blueprints already pinned to loops.

## Which protocols, measured

| Skill | Lines | Agents referencing it |
| --- | --- | --- |
| **`dev-loop-protocol`** | **864** | **33** |
| **`development-map`** | **525** | **32** |
| **`proposal-brief-protocol`** | **833** | **2** — but see below |
| **`conversational-map`** | **278** | **1** — and it found the format's boundary |
| `hotfix-protocol` | 115 | 2 |
| `architecture-rfc-protocol` | 42 | 2 |
| `qa-audit-protocol` | 42 | 2 |
| `release-deployment-map` | 42 | 2 |

**Two are genuinely in use by count; six are referenced by the one or two primaries that route to them.** All four
`dev-loops*` primaries load `dev-loop-protocol`.

**`proposal-brief-protocol` is the exception to reading that table literally.** Two agents drive it — `brief-author` and
`proposal-author` — but **it produced every document in `docs/aeon/`**: seventeen designs, seventeen briefs, the plans, the
task files, the demo contracts. Agent count measures how many things *route* to a protocol, not how much it has shaped this
repository. So it is expressed here as the third blueprint, and it turned out to be the most structurally distinct of the
three.

**This corrects S8's L10, which paired `dev-loop` with `hotfix`.** `hotfix` is 115 lines and 2 agents, and — being a
compressed dev-loop — proves *less* about the vocabulary than a genuinely different shape would. `development-map` is 525
lines, 32 agents, and structurally different in ways listed below.

**And the line counts drifted again.** Earlier documents say 793 and 525; measured today, 864 and 525. The dev-loop protocol
has grown by 71 lines since it was last counted, which is the fourth time in this plan that a transcribed count has gone
stale — the same reason S11's G19 requires the policy corpus to be extracted rather than transcribed.

## The files

| File | From |
| --- | --- |
| [`dev-loop.yaml`](dev-loop.yaml) | `dev-loop-protocol`, phase table and per-task lifecycle |
| [`development-map-pipeline.yaml`](development-map-pipeline.yaml) | `development-map`, Pipeline mode |
| [`development-map-direct.yaml`](development-map-direct.yaml) | `development-map`, Direct mode |
| [`proposal-brief.yaml`](proposal-brief.yaml) | `proposal-brief-protocol` — **the protocol that produced every document in `docs/aeon/`** |
| [`conversational-map.yaml`](conversational-map.yaml) | `conversational-map`, **Phase 1 only — Phases 2 and 3 cannot be expressed** |

**One skill became two blueprints.** `development-map` declares *"Two Invocation Modes"*, and they are not conditional
variants: Pipeline creates issues, files and PRs; **Direct creates none of them** — *"No Issue or file is created."*
Expressing both as one blueprint would put a `when` on every lane and leave a reader unable to see either shape.

## What proves L10

The two are different in ways a compressed variant would not have been:

| | `dev-loop` | `development-map` | `proposal-brief` | `conversational-map` |
| --- | --- | --- | --- | --- |
| Lanes | 16 | Pipeline 8, Direct 7 | 5 | **7, and that is half the protocol** |
| Iterating lane | `build` over tasks | Pipeline `build`; Direct none | None | None |
| Code actions | 6 | Pipeline 6; Direct 2 | **Zero** | **Zero in the expressible half** |
| Per-task ladder | 8 steps, evidence at 3 status transitions | 4 steps, status by **labels** | No ladder | No ladder |
| Artefacts | five `.md` files | Pipeline: issues. **Direct: none** | `proposal.md`, `brief.md` | One debug report, conditionally |
| Terminal | **Branches** — merge now, or leave for review | **Asks which base** | **A document and a handoff** | **There is no terminal.** It waits for the user |
| Gates | Four | Pipeline two; **Direct zero** | Two | Two, both **offers** |
| Round caps | Capped at 2 | **Unbounded in the skill** | Capped at 2 | Capped at 2 |
| Expressible | Fully | Fully | Fully | **Phase 1 only** |

**Four shapes, three of which settle it — and the fourth does something better.**

**Direct mode** has no artefacts and no gates. **`proposal-brief`** has no code action anywhere and nothing to merge. Neither
has anything in common with `dev-loop` but the vocabulary, which is exactly what the vocabulary was supposed to demonstrate.

**`conversational-map` found the format's boundary.** Its Phase 1 is a lane sequence; **its Phases 2 and 3 are not a
blueprint's shape at all**, and saying so is worth more than forcing them (GAP-7). That is a better outcome than a fourth
confirmation: L1 argued the graph must be data because protocols differ, and the strongest version of that argument is
knowing **which** protocols a graph cannot describe.

`hotfix` would have shown none of this. It is 115 lines of compressed dev-loop: same shape, fewer lanes.

---

## The eight gaps

**S8 resolution record (2026-10-18).** The analysis below describes the gaps as they were found in the source protocols. The implementation decisions are:

| Gap | S8 outcome |
| --- | --- |
| GAP-1 · gate answer re-entry | Saved `answers.<question-id>` reaches the next model prompt; `on_reject.with` accepts `${question.<id>}` substitution. Gates still have closed answer sets. An open-ended follow-up uses S6's step question. |
| GAP-2 · non-gating memory | `advisory: true` records a reject and advances; `dev-loop` no longer carries a self-retry workaround. |
| GAP-3 · branch by answer | `answers.<question-id>` is in the published manifest; the two conditional terminal lanes remain explicit. |
| GAP-4 · terminal cleanup | `on_abort:` names a lane. An `abort` answer jumps there. Its forge cleanup action is visibly blocked until S16. |
| GAP-5 · request shape | The `flags.has_acceptance_criteria` field is declared, but an intake handler must set it from evidence. That producer belongs to S13; S8 never guesses from raw wording. |
| GAP-6 · content of artifacts | S8 enforces path existence. Literal/template content checks belong to S16, alongside its authored output checks. |
| GAP-7 · open conversation | The later conversational phases are a chat with tools, outside an ordered blueprint. No artificial closed gate was added. |
| GAP-8 · concurrency | The interpreter remains serial. A future per-lane opt-in would need its own safety and ordering tests. |


Each is marked in the YAML at the point it bites. **Six are fatal to nothing and cheaper to answer now. GAP-7 is different — it is a boundary, and the answer is probably not to
extend the format.**

### Keys added later — `after:`, `converse:`, `kickoff:` (S39 T3, beside `on_abort:` above)

Three optional top-level keys arrived with S39, the slice that answers GAP-7 without extending the
ordered format: `after: converse` leaves the loop **resting (idle) with the conversation open** when a
run finishes, instead of ending it (`""`/`finish` are today's behavior, byte-identical); `converse:`
names the agent that answers messages after the run — legal **only** beside `after: converse`, and it
must be a registered **agent body**, deliberately never a resolver (a resolver is not a conversation);
`kickoff:` is a `text/template` rendered into the run's first message, so a typed and a templated start
reach the model identically. All three are additive: no shipped blueprint uses one yet — the loader
still refuses typos (`afterr:`), absent keys stay empty, and old YAML validates exactly as before.
Validation is where they mean something: an `after:` value outside the closed set, a `converse:` without
its `after:`, or a `kickoff:` template that names a field that does not exist are invalid **at load**,
not a disappointment at the end of a run. The argument is S39's design, CR1/CR3/CR6.

### GAP-1 — A gate's answer cannot flow into the step it re-enters

`dev-loop`'s `clarify`, `discovery` and `legal` each have two dispatch shapes: fresh, and *"the orchestrator relays the
user's answers to its Open questions and re-dispatches to fold them in."*

That is a gate whose **answer text** must reach the re-entered step. `on_reject.with` exists and is the right mechanism —
but it is `map[string]string` **written statically in the blueprint**, so it cannot carry what a human just typed.

**Three lanes of the most-used protocol depend on this**, so it is not an edge case.

**Cheapest answer:** `with` values may reference the answering question — `with: { answers: "${question.clarify-open-questions}" }`
— which is one substitution rule, not an expression language. S8's L5 drew the line at *"conditions are triples, not
expressions"* precisely to avoid a runtime-interpreted format; a single named substitution stays on the safe side of it.

### GAP-2 — A lane cannot declare itself non-gating

`dev-loop`'s `memory` lane is explicitly *"(Iron Triage; **non-gating**)"* — it runs, and its outcome must not route.

The format has no way to say so. S8's validation requires any step that can reject to have a resolvable target, so
`memory` carries a self-referential `on_reject` purely to satisfy the validator — **which is a workaround visible in the
file, and workarounds in shipped examples get copied.**

**Cheapest answer:** an `advisory: true` on a lane, meaning its outcome is recorded and never routed. One field, one
validation rule.

### GAP-3 — A gate's answer cannot select between lanes

`dev-loop`'s terminal branches: `git-feature` merges now, or `git-feature-pr` opens the PR and `feature-review` reviews it.
**One question, two downstream paths.**

`on_reject` routes to **one** target, so the branch is expressed as two mutually exclusive `when` lanes reading
`answers.finish-how`. That works, and it depends on **answers being manifest fields**, which is not stated anywhere.

**Cheapest answer:** confirm `answers.<question-id>` is in the manifest (S8's L18 declaration), and the pattern needs no new
field. If it is not, add it — a gate that cannot influence anything but a reject is a gate that can only say no.

### GAP-4 — There is nowhere to put a terminal handler

`dev-loop`'s `git-abort`: *"on user abort: close the loop's open issues/PRs without merging; branches always survive."*

Every gate offers `abort`, and abort has **work to do** — cleanup that is the same wherever it was triggered. It is not a
lane, because it is not in the sequence.

**Cheapest answer:** an `on_abort:` at blueprint level naming a lane to run. Note that *"branches always survive"* already
agrees with S16's N9, so the handler's content is a code action that exists.

### GAP-5 — A condition cannot ask about the input

`development-map` step 2: *"If the request already includes acceptance criteria and definition of done, skip to step 3."*

That is a condition on **what the user supplied**, not on a manifest field. Expressed here as
`flags.has_acceptance_criteria`, which pushes the work to whatever sets the flag — and nothing yet does.

**Cheapest answer:** either the intake step sets such flags as part of its handoff (fine, and it makes the dependency
explicit), or the manifest gains a small set of input-shape fields. **The first is better**: a flag set by a step is a flag
someone can see being set.

### GAP-6 — `produces:` asserts existence, never content

`proposal-brief`'s brief template marks four of its fourteen sections **"Constant — copy verbatim"**: §1 how to use this
brief, §9 repo invariants, §12 how to decompose, §13 pre-dispatch self-check. And the skill says exactly why:

> ***"Do not trim them per project** — they are short, and each one exists because its absence caused a specific
> failure."*

`produces: brief.md` asserts the **path exists.** It cannot assert §1 was copied verbatim, or that §9's constant rows
survived — so **a trimmed brief passes**, and a trimmed brief is precisely the failure mode the rule was written against.

**This is S16's L21 boundary in a place where the boundary moves.** L21 says code checks that authored prose *exists and is
not a stub* and cannot check quality — correct for a `pr_summary`. But **verbatim-copy is not quality; it is a diff**, and a
diff is exactly what code checks well.

**Cheapest answer:** `produces:` gains an optional `contains:` — a list of literal strings or a reference to a template
fragment that must appear unmodified. It is not a content grader; it is `grep`, which is what the requirement actually is.

### GAP-7 — There is no way to wait for an open-ended user choice

**The largest finding, and it is a boundary rather than a missing field.**

`conversational-map` opens by saying *"there is no separate Planning phase. The flow is **one continuous conversation**."* Its
last two phases are user-triggered:

> *"**When the user triggers** a push (e.g., 'push this', 'branch and commit', 'PR')…"*
> *"**When the user triggers** a review (e.g. 'review this', 'review my PR #{N}')…"*

A blueprint is an **ordered sequence**; a gate stops and asks a **closed** question. Neither expresses *"sit idle until the
user says what happens next, from an open set"* — and the set is genuinely open. A gate with answers
`[push, review, more-work, done]` closes it, and *"also add rate limiting to the admin API"* appears in no answer list while
being an entirely ordinary next utterance.

**The honest conclusion is that those phases are not a blueprint's shape.** They are a **chat with tools** — S5's step
runtime, S6's suspension, S16's actions, S11's guard deciding what may run — with the human as the orchestrator and no
declared graph to interpret.

**That is the format's boundary, not its failure.** L1 argued the graph must be data because protocols differ; the strongest
form of that argument is knowing which protocols a graph cannot describe. **And it is worth knowing before someone tries to
make every interaction a lane** — which is the mistake this finding prevents.

**No answer is proposed**, because the right answer is probably not to extend the format. It is that Aeon has **two modes**:
blueprint-driven, and conversational. S9's app already has a chat per loop; a conversational loop is one with no blueprint
pinned.

### GAP-8 — Concurrency the engine deliberately refuses

`conversational-map`: *"Independent tasks can run **concurrently**."*

The engine is deliberately serial. S5 asserts serial execution **with entry and exit timestamps**, precisely because *order
alone passes against an `errgroup`*. And the other two protocols agree with the engine, explicitly:

- `proposal-brief`: *"There is no concurrent execution, and identifying opportunities for it is not useful."*
- `development-map`: *"Execute tasks one at a time in order (**never concurrently**)."*

**Two protocols forbid it, one wants it, and the engine forbids it.** So this is a real conflict rather than a gap, and the
blueprint runs sequentially with the divergence recorded in it.

**Worth noting which way to resolve it:** serial is the safer default and the other two protocols depend on it. If
concurrency ever arrives it is a per-lane opt-in, not a global change — and S5's timestamp assertion is what would have to
be relaxed, deliberately.

---

## One finding that is not about the format

**Two forge identities.** `conversational-map` is explicit, and the reason is concrete:

> *"`code-reviewer` submits its verdict via the **`github-reviewer_*` MCP** (reviewer identity) … Because the reviewer
> account differs from the steward's author account, **the approval actually counts against branch protection.**"*

**S16's `Forge` is one interface with one implementation and one token.** A self-approved pull request does not satisfy branch
protection, so a protocol that opens and approves under one identity produces an approval that does not count — and nothing
would say so.

**This is a finding about S16, not about the blueprint format.** A blueprint names `forge.postReview`; which identity it uses
is the forge layer's business. Recorded here because it was found here, and because S16's T11 currently reads one token from
one place.

---

## One divergence, recorded rather than silent

**`development-map`'s review loop is unbounded** — *"Return to step 6 — still for the same task. Loop until approved."*

`development-map-pipeline.yaml` caps it at 2, because S17's L28 caps task-level rounds and S8's validation makes an uncapped
backward edge an `invalid` diagnostic.

**That is a deliberate change to the protocol, not a translation of it.** It is in the YAML with a comment saying so,
because a blueprint that quietly disagrees with the skill it came from is worse than one that says where it does.

---

## What these are not

**Not validated.** No validator exists. Every one of these will produce diagnostics on first load, and **that is the point**
— S8's T12 requires `dev-loop.yaml` to validate with **zero `invalid`**, so the first run of the validator against this file
is the test of whether the format works.

**Not the whole skills.** The dispatch ritual, the FINAL REPORT line, the caller guard, `memory_append` and `event_append`
are all engine work now — S13's RITUAL and BECAME MECHANISM categories, deleted deliberately.

**Not a commitment to the remaining four protocols.** `hotfix-protocol` (115 lines) and the three 42-line skills
(`architecture-rfc-protocol`, `qa-audit-protocol`, `release-deployment-map`) are **compressed variants of shapes already
expressed here** — `hotfix` is dev-loop with fewer lanes, and the 42-line skills are too thin to test a format against.
Expressing them would add files and no findings.

**The superseded design's guess was right.** It said `conversational-map` was *"the likeliest to reveal a field the reference
lacks"* because it has no planning phase and no forge handoffs. It revealed something larger than a field: **a boundary**.

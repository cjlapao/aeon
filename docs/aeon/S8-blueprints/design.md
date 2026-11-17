# Design — S8 Blueprint & the two-step loop (Stage 2 · Bicycle)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 2 · Bicycle, third of four
**Depends on:** S1–S5. **Independent of S6 and S7** — different code, and S9 needs all three.
**Inherits:** L1–L10 from [`docs/_archive/aeon-v1-horizontal/07-loop-decide`](../../_archive/aeon-v1-horizontal/07-loop-decide/design.md). New decisions start at **L11**.

---

## Context

Everything so far runs **one step**. S8 is where a sequence of steps becomes a thing you can author, and the reason
that matters is stated plainly in the superseded design: there are **five** real protocol skills in this repository,
not one — `dev-loop-protocol` (793 lines), `proposal-brief-protocol` (806), `development-map` (525),
`conversational-map` (278), `hotfix-protocol` (115) — with different lanes. Freezing one into code makes the other
four second-class.

So the graph is data and the interpreter is code (L1). That decision is inherited and not re-litigated.

What is new is a problem the horizontal plan never had to face, because it built everything in dependency order and
validated at the end.

### The problem: validating against registries that do not exist yet

A blueprint names agents, code actions and task statuses. In S8:

| What a blueprint names | Can it be checked in S8? |
| --- | --- |
| `agent:` — a handler | **Yes.** S8 builds the handler registry (L14) |
| `sets:` — a task status | **Yes.** S4 ported the transition machine |
| `when:` — a manifest field | **Yes.** S8 publishes the manifest |
| `goto` / `rework` / `iterate` / step ids | **Yes.** All internal to the blueprint |
| `action:` — a code action | **No.** The registry arrives at **S16** |

L9 says validation is loud and at load. But a validator that cannot check `action:` has two dishonest options:
pass it silently, or fail a blueprint that is correct. Both are wrong, and the first is worse because it is
reassuring.

So validation has **three** outcomes, not two (L12):

| Outcome | Means |
| --- | --- |
| `valid` | checked, and correct |
| `unverifiable` | **the registry that would confirm this does not exist yet** — named, with the slice that brings it |
| `invalid` | checked, and wrong |

`--strict` treats `unverifiable` as failure, so CI can demand a fully checkable blueprint while `aeon blueprint
validate` still tells the truth about a partly-checkable one.

**This is the third time this shape has appeared** — cost (`known`/`partial`/`unknown`), the `bash` receipt (§3a),
and now this. Three independent convergences is a pattern rather than a coincidence, so it is now
[P31](../ARCHITECTURE.md#8-product-decisions): *wherever verification can be unavailable, the answer has three
states, and Aeon never reports "fine" for "I could not tell."*

It also makes a demo beat that would otherwise be impossible: **the real `dev-loop.yaml` ships in S8 and
validates** (L16), reporting exactly which steps cannot run yet and why. A toy blueprint proves the interpreter; the
real one proves the format.

## Goal

A two-lane blueprint that runs its steps in order, stops at a gate, and can be **edited without touching code** —
plus a validator that says precisely what it checked, what it could not, and why.

## Decisions

L1–L10 are inherited. New:

| # | Decision | Why |
| --- | --- | --- |
| L11 | **A step's question and a lane's gate are both `question` rows, distinguished by `kind`.** A step question resumes the **step** (S6); a gate advances the **loop**. `aeon answer` dispatches on kind | S6 already reserved `kind: 'gate'` with a null `tool_use_id`. Naming the two doors explicitly matters because they feel identical to a user and are different mechanisms — and confusing them is how a gate answer gets appended to a transcript as a `tool_result` with nothing to attach to |
| L12 | **Validation is three-state** (above), and `unverifiable` names the missing registry **and the slice that brings it** | A diagnostic saying "cannot verify" is an annoyance; one saying "cannot verify — code actions arrive at S16" is an answer |
| L13 | **`decide` returns `run-code-action` even though nothing implements it.** `execute` refuses it with `blocked`, naming S16 | `decide`'s purity and totality must not be conditioned on what is built yet, or every later slice edits it. The honest refusal belongs at the boundary that actually cannot act |
| L14 | **The handler registry lands here, not at S13** | A blueprint that can only ever name one handler demonstrates no routing at all. S8 needs the lookup; S13 fills it with 25 bodies and the tech-tag resolver. This amends S5's stated consequence |
| L15 | **The loop runner is a daemon-owned goroutine per running loop**, started **after** S7's recovery scan | P29 says the daemon owns loop execution. Ordering it after recovery is what makes a crashed loop resume rather than race its own half-finished step |
| L16 | **The real `dev-loop.yaml` ships in S8 as data that validates and does not fully run** | It is the proof that the format is the real one. A format demonstrated only by a toy is a format nobody has tested against the thing it exists for |
| L10 | **CORRECTED — the second shipped blueprint is `development-map`, not `hotfix`** | Measured: `dev-loop-protocol` is loaded by **33** agents and `development-map` by **32**; `hotfix-protocol` by **2**, and at 115 lines it is a compressed dev-loop, so it proves *less* about the vocabulary than a different shape would. `development-map` is 525 lines and structurally different — **two flows by mode, label-driven status, and a Direct mode that creates no artefacts and has no gates at all.** **A third protocol, `proposal-brief`, is expressed too** — it produced every document in `docs/aeon/` and has **no code action anywhere**, terminating in a document rather than a merge. **And `conversational-map` is expressed only in part**: its Phase 1 is a lane sequence, and its user-triggered Phases 2 and 3 are not a blueprint's shape at all. See [`../blueprints/README.md`](../blueprints/README.md) |
| — | **One finding is about S16, not the format**: `conversational-map` uses **two forge identities**, author and reviewer, *"because the reviewer account differs … the approval actually counts against branch protection."* S16's `Forge` is one interface, one implementation, one token — so a self-approved PR would not satisfy protection and nothing would say so | S16's T11 |
| L17 | **`iterate` is executed here, not deferred** | Its cost is the loop position's shape: `(lane, step)` versus `(lane, item, step)`. Adding it later is a **schema migration** on a table holding live loops. Doing it now is a wider tuple |
| L18 | **The state manifest is generated from one declaration**, and conditions, templates and the future UI builder all read it | Three copies of a field vocabulary drift, and the drift presents as a condition silently evaluating false — a loop skipping a lane for no visible reason |
| L19 | **A running loop keeps its pinned blueprint; only a new loop sees an edit** (L6, made visible) | This looks like a bug the first time you meet it, so the demo shows both halves deliberately rather than letting someone discover it |

## Architecture

```
aeon/internal/blueprint/
  types.go        the format, whole
  load.go         YAML → Blueprint, schemaVersion gate (P15)
  manifest.go     the published field vocabulary, one declaration (L18)
  condition.go    {field,op,value} + all/any/not (L5)
  validate.go     the seven classes
  report.go       three-state diagnostics (L12)
  blueprints/     two-step.yaml, dev-loop.yaml

aeon/internal/loop/
  state.go        LoopState — one snapshot read
  pin.go          the blueprint pinned at loop init (L6)
  decide.go       decide(blueprint, manifest, state) → Action   PURE (L1)
  execute.go      model-step, gate; code-action refused (L13)
  advance.go      AdvanceLoop — the step function (L2), incl. iterate (L17)
  runner.go       the daemon's goroutine per loop (L15)

aeon/internal/handler/
  registry.go     name → handler (L14). Two entries here; 25 at S13
```

### The two doors (L11)

```
  a step asks              a lane gates
      │                         │
  question kind='step'     question kind='gate'
  tool_use_id SET          tool_use_id NULL
      │                         │
  aeon answer ──dispatch on kind──┐
      │                         │
  ResumeStep (S6)           AdvanceLoop (S8)
  → the step continues      → the loop moves to the next lane
```

Both are `question` rows because a human answering something should not have to know which mechanism is asking.
They resume by different doors because they are different mechanisms, and `kind` is what keeps them apart.

### `decide` and `AdvanceLoop`

```go
// Pure and total. No store, no clock, no I/O.
func Decide(bp *blueprint.Blueprint, m blueprint.Manifest, st LoopState) Action

// Action is closed:
//   RunModelStep · RunCodeAction · AskGate · Skip · Waiting · Blocked · Done
```

`AdvanceLoop` is a **step function over persisted state** (L2): read a snapshot, decide, execute one thing, commit,
return. Nothing lives in memory between calls.

That is what makes a loop's crash recovery free. S7 recovers the *step*; the loop needs no recovery of its own,
because "resume" is calling `AdvanceLoop` again. **The loop is the one place in Aeon where the crash story cost
nothing to build** — and it cost nothing because L2 was chosen for a different reason.

### The loop position, and why `iterate` is here (L17)

```go
type Position struct {
    Lane string
    Item *string   // nil unless the lane iterates
    Step string    // "" for a single-step lane
}
```

A lane iterating zero items is a legitimate no-op, and testable. The alternative — `(lane, step)` now and a third
field later — is an `ALTER TABLE` on a table holding live loops, plus a `decide` that must interpret old rows.

### What `execute` will and will not do

| Action | S8 |
| --- | --- |
| `RunModelStep` | Runs it, through S5's `RunStep` and S8's handler registry |
| `AskGate` | Writes a `question` row with `kind: 'gate'`, returns `Waiting` |
| `Skip` | Records `not-needed` (false `when`) or `skipped` (mode-driven) — the distinction `PHASE_STATUS` already makes |
| `RunCodeAction` | **Refused**: `blocked`, with a reason naming the action and S16 (L13) |
| `Waiting` / `Blocked` / `Done` | Terminal for this call |

A refusal is not a skip. A skipped code action would let a loop reach `merge` having never pushed, and report
success.

## The demo

See [`DEMO.md`](DEMO.md): a two-lane blueprint runs two steps and stops at a gate; answering it finishes the loop;
`aeon loop show` renders the ladder with each step's state; **editing the YAML changes what a new loop runs and
leaves the running one alone** (L19); the **real `dev-loop.yaml` validates**, naming its unverifiable steps; and a
deliberately broken blueprint produces every diagnostic class at once, each with its path.

## Testing

| What | How |
| --- | --- |
| `Decide` purity | No store, clock or I/O in its imports; called twice, deep-equal, inputs unmutated |
| `Decide` totality | A table over every `Action`, and a fuzz seed of malformed states that must never panic |
| Conditions | Every operator × every type, plus a **type mismatch** (`gt` on a string) rejected at *validation*, not at evaluation |
| Manifest single-source | A test asserting the condition evaluator and the template resolver read the **same** declaration — the drift L18 exists to prevent |
| Validation, per class | Seven classes, each with a blueprint that trips exactly it, asserted on **the diagnostic's path** |
| Three-state report | A blueprint whose only problem is an unnamed code action: `unverifiable`, naming S16; `--strict` fails it; default does not |
| Unknown keys | A blueprint with a plausible typo (`agents:` for `agent:`) is **invalid**, not ignored |
| `schemaVersion` | An unknown version is a loud refusal at load, quoting what it found and what is supported |
| The pin | A loop runs its pinned content after the source file changes; a **new** loop sees the edit (L19) |
| `iterate` | Zero items is a no-op; N items run the ladder N times; the position round-trips through the store |
| Gate vs step question | `aeon answer` on each kind reaches the right door, asserted with spies on `ResumeStep` and `AdvanceLoop` |
| Code action refused | `blocked` naming the action and S16, and **nothing was executed** — spy at zero |
| Shipped blueprints | Both validate in CI, so they cannot rot as the registries grow |
| `dev-loop.yaml` | Validates with `unverifiable` diagnostics only — **no `invalid` ones**. If the real blueprint is invalid, the format is wrong |

The last row is the one that earns its keep. It is the only test that checks the format against the thing it exists
for rather than against a fixture written to suit it.

## Out of scope

- **Code actions, git, forge, templates.** **S16.** Named by blueprints, refused by `execute`.
- **The 25 agent bodies and `by-tech-tag`.** **S13.** S8 builds the registry and puts two handlers in it.
- **Phases, the full ladder, redo edges, `verify`/`security`/`review`/QA rounds.** **S17.** S8 has the vocabulary
  for them and ships `dev-loop.yaml` that uses it.
- **Permissions.** **S11.** A blueprint routes; it never grants (L8), so this slice cannot widen anything.
- **The app.** **S9** renders what S8 produces.
- **The visual blueprint builder.** Never promised for the alpha. L18's manifest is what would make it tractable.
- **Retrieval-resolved context.** S22.

## Amendments to earlier slices

| Where | Change | Status |
| --- | --- | --- |
| `../ARCHITECTURE.md` §8 | **P31 added** — the three-state pattern, now that three places have converged on it | **landed with this design** |
| `S5-one-step/design.md` | Its consequence table says S13 replaces the literal handler with a registry. **S8 does** (L14); S13 fills it | **landed with this design** |
| S6 | Confirmed, not changed: `kind: 'gate'` with a null `tool_use_id` is what S8 writes (L11) | — |
| S7 | Confirmed, not changed: recovery recovers steps; a loop resumes by being advanced again (L2) | — |

## Five gaps found by deriving the real blueprints

[`../blueprints/`](../blueprints/) holds `dev-loop.yaml` and both halves of `development-map`, derived from the skills that
are actually driving agents. **The exercise found eight places the format could not express something** — and one of them is a boundary rather than a gap, each marked in the
YAML where it bites and argued in that directory's README:

| | What the format cannot do | Cheapest answer |
| --- | --- | --- |
| **GAP-1** | A gate's **answer text** cannot reach the step it re-enters. `with:` is static, and three lanes of the most-used protocol need it | One named substitution — `${question.<id>}` — which stays on L5's safe side of "triples, not expressions" |
| **GAP-2** | A lane cannot declare itself **non-gating**. `dev-loop`'s `memory` lane is explicitly so, and carries a self-referential `on_reject` purely to pass validation | An `advisory: true` on a lane. **Workarounds in shipped examples get copied**, which is why this one matters |
| **GAP-3** | A gate's answer cannot **select between lanes** — one question, two downstream paths. Worked around with mutually exclusive `when`s reading `answers.<id>` | Confirm `answers.<id>` is in L18's manifest declaration. If it is not, add it: a gate that can only say no is not much of a gate |
| **GAP-4** | There is **nowhere for a terminal handler.** Every gate offers `abort`, and `dev-loop`'s abort has cleanup work to do | An `on_abort:` at blueprint level naming a lane |
| **GAP-5** | A condition cannot ask about **the input** — *"if the request already includes acceptance criteria, skip"* | The intake step sets a flag in its handoff, making the dependency visible |
| **GAP-6** | **`produces:` asserts existence, never content.** `proposal-brief`'s template marks four sections *"Constant — copy verbatim"* because *"each one exists because its absence caused a specific failure"* — and a trimmed brief passes `produces:` | An optional `contains:` on `produces:` — literal strings that must appear unmodified. **Not a content grader; `grep`**, which is what the requirement actually is. Note this is S16's L21 boundary moving: verbatim-copy is not quality, it is a diff |
| **GAP-7** | **There is no way to wait for an open-ended user choice.** `conversational-map`'s last two phases are *"when the user triggers a push"* / *"a review"* — an **open** set, so a gate's closed `answers` cannot hold it | **No extension proposed.** Those phases are not a blueprint's shape: they are a chat with tools, which S5, S6, S11 and S16 already provide. **This is the format's boundary**, and knowing it is worth more than forcing them — it prevents someone making every interaction a lane |
| **GAP-8** | **Concurrency the engine refuses.** `conversational-map` says *"independent tasks can run concurrently"*; S5 asserts serial execution with entry/exit timestamps because *order alone passes against an `errgroup`* | Stay serial. **Two of the three other protocols forbid concurrency explicitly** and depend on it. If it ever arrives it is a per-lane opt-in, and S5's timestamp assertion is what would have to be relaxed — deliberately |

**Six are fatal to nothing and cheaper now. GAP-7 is a boundary**, and its answer is that Aeon has two modes — blueprint-driven and conversational — rather than a bigger format. All of it is cheaper, before a blueprint is pinned to a running loop (L6).

**And one deliberate divergence:** `development-map`'s review loop is **unbounded** in the skill. The blueprint caps it at 2,
because S17's L28 caps task-level rounds and an uncapped backward edge is an `invalid` diagnostic. That is a change to the
protocol rather than a translation of it, recorded in the YAML rather than silent.

## Open questions

None. The `dev-loop.yaml` shipped here is expected to change at S17 as the ladder fills in; that is authorship, not
an open question about the format.

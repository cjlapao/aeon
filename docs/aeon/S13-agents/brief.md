# Brief — S13 Agents

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Inherits:** A1–A10 from [`docs/_archive/aeon-v1-horizontal/06-agents`](../../_archive/aeon-v1-horizontal/06-agents/design.md). New decisions begin at
**A11**.
**Stage:** 3 · Car, second of four. Depends on S8, S11 **and S12** (ROADMAP.md §7 — N17's reversal moved S12
ahead of this slice, since 24 of 26 bodies reference MCP as a granted tool rather than deferred-because-absent
content; **D4c's "deferred" instruction below is superseded for those 24 and needs re-reading against S12's
real tool set before this slice is planned**). **Blocks S16 and S17.**

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief overrides it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** **Most of this slice is prose, not code.** Twenty-five handler bodies rewritten by hand from 2,902 lines of
> originals. The mechanical rules below exist because prose has almost no other guard.

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
| **D1** | **No handler body names a tool outside the nine** (A11), asserted against the tool registry — **and the same check, pointed at an original `*-loop` body, must FAIL.** A guard that passes everything proves nothing; a guard no original can pass is measuring something. |
| **D2** | **No body loads a skill.** Nothing references `dev-loop-protocol` or any other skill (A5). |
| **D3** | **No ritual residue.** No body contains a `FINAL REPORT` line, a caller guard, or an instruction to append to memory or events. |
| **D4** | **All 25 bodies' front matter parses**, `tools` ⊆ the nine, `temperature` present, and `tools` is an allow-list — a `false` entry is a configuration error (A6, S11's T12). |
| **D4a** | **`tools` is translated, not ported** (A19): `task` **absent** (dropped, not renamed — a rename reintroduces delegation through a config file), `todowrite` → `todo`, and `codesearch`, `lsp`, `patch` gone. All 26 originals declare `task` and `todowrite`. |
| **D4b** | **`description` passes the same checks as the body** (A20). 25 of 26 originals describe a caller guard declining work from anyone but `dev-loops`; a rewritten body with a verbatim description ships that instruction anyway. |
| **D4c** | **Content needing an absent capability is recorded as deferred**, naming it (A18). **24 of 26 bodies reference MCP**, which is deferred — a `DEFERRED` section per affected handler, or that intent is lost. |
| **D5** | **Policy comes from S11's extractor, byte-identical per agent** (A16). Two parsers over the same front matter is how the policy the guard enforces and the policy the registry reports come to disagree. |
| **D6** | **Each handler ships a drop record** categorising every removal as **ritual**, **became-mechanism**, or **domain-kept**, and **accounting for the line-count difference** (A14). An unexplained gap is an omission nobody reviewed. |
| **D7** | **Nothing was invented.** Every line in a drop record's "kept" set is traceable to the original (A17). A rewrite is a port, not an authoring exercise. |
| **D8** | **The 25 are the right 25**: every `*-loop` original has a handler **except `git-loop`** (A3), asserted by enumeration against the directory rather than against a list. |
| **D9** | **`by-tech-tag` resolves known tags** to the right handler. |
| **D10** | **An unknown tag is a structured refusal** naming the tag and the available handlers (A13). |
| **D11** | **There is no fallback handler.** Asserted by a test that no code path substitutes a default. |
| **D12** | **A handler that declares a handoff field and does not produce it is a `reject`**, not a silent gap. The four outcomes round-trip. |
| **D13** | **S11's resolver reserve list becomes a real check**: `by-tech-tag` validates, and an unknown resolver name is now `invalid` rather than `unverifiable`. |
| **D14** | **`opencode/` is untouched**, asserted by a filesystem spy over the whole slice's test run (A10). |
| **D15** | **`dev-loops-aeon` is not in any retirement set** (A15). |
| **D16** | **A blueprint step dispatching to `golang-engineer` produces Go that compiles and passes its tests.** The slice's actual claim, and the first in the plan whose success condition is working software rather than a correct mechanism. |
| **D17** | **`make test` and `make lint` green**, `make fmt` leaves no diff. |

**D1, D6 and D16 are the three that matter.** D1 because it is the only mechanical guard on the prose. D6 because it
is what makes A4's human diff reviewable rather than a thing that happened once. D16 because it is the point.

**D1–D16 must each exist as named tests**, except D16, which is the demo's §1 and a checked-in integration test.

D17 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

S8 built the registry with two entries. S11 attached policy and left resolvers `unverifiable`. Nothing has written real
software yet.

**A verbatim port of the 26 `*-loop` bodies is not wasteful — it is broken**, and the measurement is unambiguous:

| Across all 26 originals | |
| --- | --- |
| Reference `loop_get` | **26 of 26** |
| Reference `memory_append` | **26 of 26** |
| Reference `event_append` | **26 of 26** |
| Load the `dev-loop-protocol` skill (793 lines) | **26 of 26** |
| Carry a `FINAL REPORT` line | **26 of 26** |

Not most. Every one. Each would instruct the model to call at least three tools Aeon does not have, and each would
begin by loading the skill whose length is the context injection this project exists to remove. **The cheap
alternative does not run.**

That is also the leverage: the failure is **mechanically detectable**, which is the only automatable guard on 2,902
lines of prose.

**And the ritual/domain split is three-way, not two.** A4's own example has gone stale in a revealing way: *"exit 137 is
an OOM kill, not a lint finding"* was domain, and S5's W5 made it a **fact the tool reports**. Prose duplicating a
mechanism is a second source of truth, and it drifts in the worse direction — the prose is what the model reads.

## 4. Goal

Twenty-five handlers carrying only what the model still needs to know, a resolver that picks between them by tech tag,
and a structured handoff — such that a blueprint step dispatches to `golang-engineer` and it writes working Go.

## 5. Shape and the seam

```
blueprint step ──▶ agent: golang-engineer ──────────────┐
                   agent: by-tech-tag ──▶ Resolve(tag) ─┴──▶ Handler
                                              │                 │
                                        no handler?         SystemPrompt (rewritten body)
                                        structured          Tools (⊆ nine, allow-list)
                                        refusal (A13)       Policy (from S11's extractor)
                                                            Handoff schema
```

### The seams

**The registry is a registry, not a lifecycle manager.** A1: nothing spawns, `task` is not a tool, blueprints replace
delegation. So there is **no spawn, no abort, no per-subtree cost rollup** — because there is no tree. Anything in the
plan resembling those is a sign A1 was forgotten.

**Policy comes from S11, not from a second parser** (A16, D5).

**The resolver is a closed registry** (A13), the same reasoning as S8's conditions and predicates: a resolver
expressible in config is a config file that runs code.

**The body is the only place model-facing prose lives.** No skill loading, no runtime assembly, no per-step injection.

## 6. Locked decisions

**Inherited:** A1 (nothing spawns; blueprints replace delegation), A2 (the primaries become blueprints), A3 (25
handlers, not 26 — `git-loop` becomes none), A4 (rewritten by hand, each diffed against its original), A5 (skills
retired, content distributed), A6 (`tools` is an allow-list), A7 (ordered bash policy), A8 (**superseded** — `ask` is a
real verdict, S11's G5), A9 (segment splitting), A10 (`opencode/` untouched).

**New:** A11 (no forbidden tool, asserted), A12 (three categories), A13 (no fallback; structured refusal), A14 (drop
records), A15 (`dev-loops-aeon` survives), A16 (policy from the extractor), A17 (nothing invented), A18 (deferred content
recorded), A19 (`tools` translated, `task` dropped), A20 (`description` is model-facing prose).

**A8 is inherited as superseded.** It held that `ask` evaluates as deny because nothing could suspend a step; P2, P6
and S11's G5 reversed it. Do not reimplement A8.

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | No body may name a tool outside the nine | The check must run on **tool names the registry actually holds**, not a hardcoded list of nine strings. A hardcoded list is a second source of truth about the toolset — the exact failure A16 avoids for policy — and it would go stale the moment S22 adds `search`. |
| O2 | Each rewrite ships a drop record | The record must be **diffable against the original**, meaning it quotes or line-references what it dropped rather than paraphrasing. "Removed the ritual preamble" is unreviewable; a line reference is not. |
| O3 | A rewrite may not add domain content (A17) | It **may** reword for clarity, and the drop record must say when it did. Otherwise A17 makes every improvement look like a violation, and the rule gets ignored rather than followed. |
| O4 | An unknown tech tag is a structured refusal | The refusal must list the **available tags**, not the available handler names. A user asked for `elixir` and needs to know which tags exist; handler names are an implementation detail they did not supply. |
| O5 | The design does not say what happens when two handlers claim the same tech tag | **A load-time error naming both.** A resolver with an ambiguous table silently picks one, and which one depends on map iteration order — the same class of bug G2 fixed by making policy an ordered array. |
| O6 | The handoff has declared fields | A field a handler declares must have a **stated purpose** in its schema, because S16 and S17 consume these and a field named `notes` with no contract is a field nobody can rely on. |
| O7 | The 25 bodies are rewritten | Every rewrite must be **verified by running its handler at least once** against a real task, not only by passing the mechanical checks. A body can satisfy every grep and still instruct the model to do something impossible. |
| O8 | `git-loop` becomes no handler | Its **domain content must still be accounted for**: a drop record for `git-loop` recording which of its 372 lines became S16's code actions, which became mechanism, and which are genuinely lost. It is the largest original and dropping it silently loses the most. |

## 8. Non-goals

- **N1 — No git or forge actions.** **S16.**
- **N2 — No blueprints for the 16 primaries.** **S17** authors what the ladder needs. This slice retires none.
- **N3 — No skill deletion.** Content is distributed; nothing in `opencode/` is removed (A10).
- **N4 — No `pr_summary`.** S16 adds the field; the mechanism is here.
- **N5 — No `search`.** S22.
- **N6 — No new tools.** The nine are complete after S11.
- **N7 — No spawning, delegation, or `task` tool.** A1. And therefore **no abort, no subtree cost rollup**.
- **N8 — No agent authoring UI.** Never promised for the alpha.
- **N9 — No changes to the nine tools' behaviour.** If a handler seems to need a tool to work differently, that is a
  finding to raise.

### Standing non-goals

- **Never write to `opencode/`** (A10, D14). It is read and never modified.
- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never run `git clean`.**

## 9. Repo invariants

**I1 — Nothing spawns** (A1). No spawn, abort, or subtree accounting anywhere.

**I2 — The tool list has one source**: the registry (O1). Not a hardcoded nine.

**I3 — Policy has one source**: S11's extractor (A16, D5).

**I4 — `opencode/` is read-only to this slice** (A10, D14).

**I5 — The resolver is a closed registry** (A13). No expression, no config-defined resolver.

**I6 — No fallback handler** (D11).

**I7 — Every rewrite has a drop record that accounts for its line-count difference** (A14, D6).

**I8 — The superseded design's counts were corrected when this slice was specified** (75 files, 51 `variant`, 16
primary, cause: `dev-loops-aeon`). That correction is **already landed**; do not re-edit it.

**I9 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/06-agents/design.md`** in full — A1–A10, the ritual/domain analysis, and the re-measurement note.
2. **`opencode/agents/golang-engineer-loop.md`** — 84 lines, and the design's worked example. **Read it before
   planning anything**, because the shape of one rewrite determines the shape of 25.
3. **`opencode/agents/qa-engineer-loop.md`** (293 lines) and **`git-loop.md`** (372) — the two largest. The first is a
   rewrite; the second is O8's accounting exercise.
4. **`opencode/skills/dev-loop-protocol/SKILL.md`** — 793 lines, loaded by all 26. A5 distributes its content; read it
   to know what is domain and what is engine.
5. **`internal/handler/registry.go`** (S8's T7) — the two existing entries and the assertion that **no blueprint field
   can grant a tool**.
6. **`internal/policy/corpus.go`** (S11's T1) — the extractor whose output A16 consumes.
7. **`internal/tools/`** — the nine, and their descriptions, which are what O1's check compares against.

### 10.2 The patterns to copy

- **S11's committed-and-diff-checked generated output**, for the handler list and the drop records.
- **S8's committed-fixture pattern** (`handlers.json`), growing from 2 to 25.
- **S7's and S8's structured reasons** — `RecoveryFailure`, `BlockedReason` — as the model for A13's refusal.
- **Discriminated results over errors** (S4's DB19) — an unresolved tag is a domain outcome.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | Is the front matter shape uniform across all 26 `*-loop` files, including `variant`? | `opencode/agents/*-loop.md`. **If not, enumerate the variants** — a file the loader mishandles is a handler that silently behaves differently |
| Q2 | What does `variant` mean, and does it affect a handler's behaviour or only its selection? | The originals, and any prose referencing it. **If it turns out to matter, say so** — 51 files declare it and no decision mentions it |
| Q3 | Which of the nine tools does each original actually need, once ritual tools are removed? | Per body. This is the `tools` allow-list, and over-granting is the easy mistake. **Note the translation first** (A19): all 26 declare `task` and `todowrite` |
| Q4 | Does S11's extractor expose policy in a form the registry can consume directly, or does it need an accessor? | `internal/policy/corpus.go`. A16 forbids a second parser, not a second accessor |
| Q5 | What does S8's `handlers.json` contain today, and what shape does it need at 25? | `internal/handler/testdata/handlers.json` |
| Q6 | How does a blueprint step name a resolver versus an agent today, and what did S11's T5 reserve? | `internal/blueprint/validate.go`, and the resolver reserve list |
| Q7 | Does any tool description currently duplicate prose that a body also carries? | `internal/tools/*.go`. A12's middle category applies to tool descriptions too — two places saying the same thing is one place too many |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q8 | What is the tech-tag → handler table, in full? Produce it before planning; O5 makes a duplicate a load-time error. |
| Q9 | What fields does each handler's handoff declare, and what is each for (O6)? |
| Q10 | For `git-loop`'s 372 lines: which become S16's code actions, which became mechanism, and which are lost (O8)? |
| Q11 | How is a rewrite verified by running it (O7) — what task, and what checks its output? |

---

## 11. Behavioural contract

- No body names a tool outside the registry's set; the check fails on originals (D1, O1).
- No skill loading, no ritual residue (D2, D3).
- Front matter parses; `tools` ⊆ the nine and is an allow-list (D4).
- Policy is byte-identical to S11's extractor output (D5).
- Every handler has a drop record: three categories, quoting or line-referencing, accounting for the line delta (D6,
  D7, O2, O3).
- 25 handlers; every `*-loop` except `git-loop` (D8).
- Known tags resolve; unknown tags are a structured refusal listing **available tags** (D9, D10, O4).
- No fallback handler (D11).
- A declared handoff field not produced is a `reject`; fields have stated purposes (D12, O6).
- Duplicate tag claims are a load-time error naming both (O5).
- `by-tech-tag` becomes a real validation check (D13).
- `opencode/` untouched; `dev-loops-aeon` survives (D14, D15).
- Every rewrite verified by running it once (O7).
- `git-loop`'s content accounted for (O8).
- A `golang-engineer` step produces Go that compiles and passes tests (D16).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **the mechanical guards come before the rewrites.** A11's check, the drop-record format, and the loader must
  exist first, or 25 rewrites land unverified and are re-reviewed by hand.
- **R5** — **the first rewrite batch is small** — four handlers at most — because the shape of one rewrite determines
  the shape of 25, and discovering the shape is wrong on the twenty-second is expensive.
- **R6** — **group rewrites by domain family**, so a reviewer holds one kind of judgement at a time.
- **R7** — the resolver and its table are one task, and it follows enough rewrites to have something to resolve to.
- **R8** — no task over roughly 300 changed lines or 12 files without justification. **A rewrite batch may exceed the
  file count** — five bodies plus five drop records is ten files by construction; say so.
- **R9** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure",
  "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md` and amends the QA suites.
- **R12** — **no task writes to `opencode/`** (D14) or edits `docs/_archive/aeon-v1-horizontal/06-agents/design.md` (I8, already landed).

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines without justification (R8) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | Paste Q8's table in full — every tech tag and its handler | **A missing table blocks the plan.** O5 makes a duplicate a load-time error, which requires knowing the table |
| A6 | Paste Q1's answer | **If the front matter is not uniform across all 26, enumerate the variants** |
| A7 | Paste Q2's answer on `variant` | 51 files declare it and no decision mentions it. **If it affects behaviour, that is a finding** |
| A8 | Paste Q10's accounting for `git-loop`'s 372 lines | Three categories, all lines accounted (O8) |
| A9 | Name the source of the tool list used by A11's check | The registry, **not** a hardcoded nine (O1, I2) |
| A10 | Confirm the 25 by enumerating `opencode/agents/*-loop.md` minus `git-loop` | Against the directory, not a list (D8) |
| A11 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task implements a git or forge action | N1 |
| B2 | Confirm no task authors a blueprint or retires a primary | N2 |
| B3 | Confirm no task deletes anything under `opencode/` | N3, D14 |
| B4 | Confirm no task adds `pr_summary` | N4 |
| B5 | Confirm no task implements `search` or a new tool | N5, N6 |
| B6 | Confirm **no task implements spawn, abort, or subtree cost accounting** | N7, A1, I1 |
| B7 | Confirm no task changes a tool's behaviour | N9 |
| B8 | Confirm no task writes to `opencode/` or edits `docs/_archive/aeon-v1-horizontal/06-agents` | R12, I8 |
| B9 | Quote the `out_of_scope` lines covering N1–N9 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D16 to a task and a named test | §2 |
| C2 | For D1, confirm the **negative control**: the check run against an original body, expected to fail | D1 |
| C3 | For D5, confirm the assertion is **byte-identical** per agent, not "equivalent" | D5 |
| C4 | For D6, confirm each drop record accounts for the **line-count difference** | D6, O2 |
| C5 | For D8, confirm the enumeration reads the **directory** | D8 |
| C6 | For D10, confirm the refusal lists **tags**, not handler names | D10, O4 |
| C7 | For D11, confirm a test asserts no default substitution exists | D11 |
| C8 | For D14, confirm the spy covers the **whole** test run, not one test | D14 |
| C9 | For D16, name the task, the task spec, and what verifies the Go compiles and passes | D16 |
| C10 | For O7, confirm **every** rewrite is run once, not a sample | O7 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm the guards and loader precede every rewrite batch | R4 |
| D2 | Confirm the first batch is four handlers or fewer | R5 |
| D3 | Confirm batches are grouped by domain family | R6 |
| D4 | Confirm the resolver task follows enough rewrites to resolve to | R7 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm nothing spawns, aborts, or rolls up subtree cost | I1, A1 |
| E2 | Confirm one source for the tool list | I2, O1 |
| E3 | Confirm one source for policy | I3, A16 |
| E4 | Confirm the resolver registry is closed | I5 |
| E5 | Confirm no fallback handler exists | I6 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | Paste the full tech-tag table (Q8) and confirm no tag appears twice (O5). |
| F2 | State each handler's handoff fields and each field's purpose (Q9, O6). |
| F3 | State `git-loop`'s accounting (Q10, O8). |
| F4 | State how each rewrite is verified by running it, and against what (Q11, O7). |
| F5 | State whether `variant` affects behaviour (Q2). |
| F6 | Name any tool description duplicating body prose (Q7, A12). |
| F7 | State the `tools` translation table, and confirm `task` is **dropped** rather than mapped (A19, D4a). |
| F8 | List which handlers get a `DEFERRED` section and for what capability (A18, D4c). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it and fixes whatever does not match.

**§1 is the first demo in the plan whose success condition is software that runs.** Everything before it proved a
mechanism behaving correctly; this proves the mechanism was worth having. `go test ./...` passes, or the slice is not
done.

**§3 is where the three-way split is shown, and the middle category is the whole reason it is a section.** *"exit 137 is
an OOM kill, not a lint finding"* reads exactly like domain knowledge — and it is now W5's job. Whoever walks the demo
should come away able to spot the next instance of that on their own.

**§4 points the mechanical guard at an original body and watches it fail.** That negative control is what makes the
guard trustworthy: all 26 originals reference `loop_get`, so a check that cannot fail on them is not measuring anything.

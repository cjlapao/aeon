# Brief — S23 First run & polish

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Inherits:** C1–C13 from [`docs/_archive/aeon-v1-horizontal/13-cli`](../../_archive/aeon-v1-horizontal/13-cli/design.md), with C1's mechanism corrected and C7
and C8 superseded. New decisions begin at **C14**.
**Stage:** 4 · Space shuttle, fourth of four — **the last slice.** Depends on everything.
**Amended 2026-10-26 for P34** (`ARCHITECTURE.md`): *"A capability that is not reachable through the daemon
surface is not finished."* As originally written, `doctor`'s aggregate health report and `init`'s
preset-driven setup were CLI-only — the exact thing a first-time **app** user would need, with no door onto
either. D10/D14/D16 below are the amendment. Not every CLI-touching task needed one: T3 (exit codes) is a
terminal-intrinsic concern with no app analogue, and `blueprint.list`/`blueprint.save` already have socket
doors from S8/S9 — T5 only needs its response to carry the shipped-vs-overridden distinction, an additive
field, not a new surface.

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief overrides it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** **D1 is a measured test on a person, not an assertion.** It is the only Definition-of-Done item in seventeen
> slices that a machine cannot evaluate, and §12's rules are shaped around not letting it be quietly replaced by one.

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
| **D1** | **A stranger reaches a running loop in under five minutes**, timed, by **someone who has not read the design** (C14 — restating `aeon-cli.md`'s QAC2-01). Recorded: where they hesitated, what they typed that failed, what they asked — because *"the list of stumbles is the real output"*. **Repeated per provider, ideally with a different tester each time** (C21). **Not self-certifiable**, and *"automatable: no, and it must not be"* — if no such person is available, the slice is not done. |
| **D2** | **Config layers**: defaults → user file → project file → environment → flags, each overriding the last (C2). |
| **D3** | **A validation failure names the offending key *and* file** (C3). "Invalid configuration" fails this check. |
| **D4** | **A credential in a config file is a load-time error**, not a warning (C4). |
| **D5** | **No flag accepts a credential value**; only a path (C17, S19's O1). |
| **D6** | **No credential is ever printed** — `doctor`, every error path, every log. Presence, never value (C17). |
| **D7** | **`init` is fully scriptable**: every prompt has a flag, and the whole flow runs with **no TTY** (C5). |
| **D8** | **`init` probes what it configured** and reports `verified` or `CONTRADICTED` before finishing (C18). |
| **D9** | **Three presets** — Anthropic, OpenAI, OpenAI-compatible, the third covering Ollama, vLLM, LiteLLM and OpenRouter (C6). |
| **D10** | **`doctor` flattens nothing** (C15). Every three-state answer in its own states, and the strings "ok" / "all good" appear over **no** uncertain value. **And it is a daemon method, not only a CLI command** (P34, added 2026-10-26): `doctor.check {projectRoot?}` returns the identical report `aeon doctor` renders, read-only, computed by calling the same per-slice state readers — not a second aggregation. A first-time app user has no terminal open; without this, "is my system healthy" is answerable only outside the product the person is actually using. |
| **D11** | **The three-part error shape is structural** (C16): an error missing `Do` **does not compile**. |
| **D12** | **Exit codes distinguish `failed`, `blocked` and `waiting`**, and are documented (C10). |
| **D13** | **No telemetry, asserted by a network trace** over a full loop: no connection except configured routes and forges (C19). |
| **D14** | **A project's blueprint overrides the shipped one** (C12), and `blueprint.list`'s existing response (S8/S9) states which — added 2026-10-26, P34: the fact was previously visible only to `aeon blueprint list`, even though the app already renders blueprints through the same method. |
| **D15** | **C7 and C8's mechanisms are gone**: `serve` binds no port and serves no UI; there is no spawned runner (C20). |
| **D16** | **Presets are reachable from the daemon surface, not only `aeon init`** (P34, added 2026-10-26). `presets.list` returns the three presets' route templates (provider, model, capability declarations, price, base URL — the same data `init`'s prompt reads). Applying one is **not** a new write method: the app calls the existing `routes.set` (S3) with the chosen template, then S18's new `routes.probe` to verify it — the same two calls `init` itself makes internally, composed by whichever door is driving (CLI prompt, or the app's first-run wizard). No new write surface, no second "apply" implementation — `presets.list` is the one genuinely new piece. |
| **D17** | **`make test` and `make lint` green**, `make fmt` leaves no diff. |

**D1 is the slice.** Everything else exists so that it can pass. D10 is second, because it is where six slices of honesty
either hold or are undone at the last moment.

**D2–D16 must each exist as named tests. D1 must not** — it is a recorded human measurement, and a test that claims to
cover it is a test that has replaced it.

D17 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

Sixteen slices built a product. This one is about **the first five minutes**, and its success criterion is a stopwatch and a
person.

**Five minutes is a budget** — reading, installing, `init`, choosing a provider, pasting a key, starting a loop — and every
question asked spends from it. The ordering that matters is not the code's but **what a stranger hits first.**

**The measurement cannot be self-certified** (C14). If the tester has read the design, they know which flag exists and their
five minutes are not a stranger's. This is the easiest corner to cut in the whole plan.

**And `doctor` is where the product's honesty converges.** Six independent places refuse to collapse an uncertain answer
(P31), and retrieval adds four availability reasons. `doctor` reports all of them, which makes it **the single place the
whole pattern can be undone** — by summarising into a green "ok" that would look helpful doing it.

**Two inherited decisions are wrong now.** C7 assumed U1's served UI, superseded by S9's Tauri shell. C8 assumed a spawned
runner, removed by P29. Both principles survive; both mechanisms are gone.

## 4. Goal

A stranger with one API key reaching a running loop in under five minutes, and a `doctor` that tells the truth about every
optional part.

## 5. Shape and the seam

```
README ──▶ install ──▶ aeon init ──▶ first loop ──▶ aeon doctor (when wrong)
              │            │                              │
         one binary    preset + probe (C18)      every optional part,
          (P30)                                  in its own states (C15)

config: defaults → user → project → env → flags        (C2)
creds:  environment | dedicated file  — NEVER config, argv, logs   (C17)
errors: {What, Why, Do} — all three required, structurally         (C16)
```

### The seams

**The five-minute path is the design surface**, and it is four steps. Anything a tester has to do beyond README → install →
`init` → run is time this slice spent badly.

**`doctor` reads from every slice and decides nothing.** It renders states other slices computed, which is why C15 is a
rendering rule rather than a logic one — and why flattening would be so easy.

**The error type is the enforcement point** (C16). A convention produces two-part errors; a struct with three required
fields produces three-part ones.

**One credential rule** (C17) means one place reads credentials, and every other path takes them from there.

## 6. Locked decisions

**Inherited:** C1 (one install — **mechanism now a Go binary**, P30), C2 (layered config), C3 (validation names key and
file), C4 (credentials from the environment only), C5 (`init` interactive with a flag for every prompt), C6 (three presets),
C9 (three-part errors), C10 (meaningful documented exit codes), C11 (no telemetry), C12 (blueprints shipped and
overridable), C13 (`doctor` explains what is configured, missing, and degraded).

**Superseded:** **C7** (served UI — U1, by S9) and **C8** (spawned runner — by P29). Their principles survive; do not
reimplement their mechanisms.

**New:** C14 (the measurement needs a stranger — restating QAC2-01), C15 (`doctor` flattens nothing), C16 (the error shape is
structural), C17 (one credential rule), C18 (`init` probes), C19 (no telemetry, asserted), C20 (C7, C8, C1's mechanism
corrected), C21 (repeated per provider, different testers).

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | The tester has not read the design | **Record their exact words** where they hesitated or asked, not a paraphrase. *"I didn't know if I needed a project first"* names the missing sentence; "confused about setup" does not, and the second is what a paraphrase becomes. |
| O2 | Five minutes is the target | **The run is recorded even when it passes**, because QAC2-01 is right that *"the list of stumbles is the real output"*. A four-minute run with a forty-second hesitation has found a problem, and a pass that records nothing throws it away. And a slow run is **a list of places the product failed to explain itself** — not evidence the target was optimistic. |
| O3 | `doctor` reports every optional part | It must also report **what is fine**, briefly. A screen showing only problems gives no way to tell a healthy system from an unchecked one, and someone reading it needs to know `doctor` looked. |
| O4 | Errors have three required fields | `Do` must be **an action, not advice.** "Check your configuration" satisfies a struct and fails C9. Where a command exists, `Do` contains the command. |
| O5 | The design does not say what `init` does when it finds an existing config | **It does not overwrite.** It reports what exists and what it would change, and requires `--force`. A first-run command that silently replaces a working configuration is one people run once and never trust again. |
| O6 | `init` probes what it configured | A **probe failure** must not fail `init`. An unreachable server at setup time leaves capabilities `declared` and says so — S18's own rule that *"could not check" and "checked and wrong" are different facts*, arriving here. |
| O7 | Presets cover four OpenAI-compatible servers | Each named server must be **actually tried**, and the preset's notes say which were and at what version — the same requirement S18's O7 put on `CAPABILITIES.md`. *"Works with OpenAI-compatible servers"* is unfalsifiable. |
| O8 | No telemetry, asserted by a network trace | The trace must cover **a full loop with tools and a forge action**, not a single step. The connection nobody expects appears on the path nobody exercised. |

## 8. Non-goals

- **N1 — No telemetry, ever** (C11, C19). Not opt-in, not anonymised, not for crash reports. **Permanent.**
- **N2 — No auto-update.** A tool that reads source code and runs commands does not update itself unasked.
- **N3 — No cloud account and no login.** There is nothing to sign into.
- **N4 — No GUI installer.** A binary and a `PATH`.
- **N5 — No package-manager submissions.** Homebrew, apt, winget are distribution work and belong **after** the first five
  minutes are known to work.
- **N6 — No Gemini preset.** Gemini is v1.1 (S18's N1); a preset for an absent adapter is a preset that fails.
- **N7 — No dogfooding.** S17's N4 deferred it to *"after S23's first-run work"* — the **next** thing, not this one.
- **N8 — No new capability, route type, tool or action.** If the five-minute path seems to need one, **that is a finding**
  about an earlier slice.
- **N9 — No reimplementation of C7 or C8.** No served UI, no port from `serve`, no spawned runner (C20, D15).

### Standing non-goals

- **Never print a credential** (D6). Presence, never value.
- **Never accept a credential as a flag value** (D5).
- **Never write outside `aeon/`**, except `docs/`, `docs/test_suites/` and the repository `README.md`.
- **Never run `git clean`.**

## 9. Repo invariants

**I1 — One credential reader** (C17). Every other path takes credentials from it.

**I2 — No credential in a config file, in `argv`, or in any output** (D4, D5, D6).

**I3 — `errs.Error` requires all three fields** (C16, D11), enforced by the type.

**I4 — `doctor` renders; it does not decide.** Every state it shows was computed by the slice that owns it (C15).

**I5 — `doctor` flattens nothing** (D10). No "ok" over an uncertain value.

**I6 — No network call except to configured routes and forges** (C19, D13).

**I7 — `serve` binds no port and serves no UI** (D15, C20).

**I8 — D1 is a human measurement**, and no automated test claims to cover it.

**I9 — Public API changes ship with their tests in the same task.**

**I10 — `doctor`, presets, and blueprint provenance are reachable from the daemon surface, not only
`cmd/aeon`** (P34, D10, D16). `doctor.check`'s socket handler calls the same collection function `aeon
doctor` calls; `presets.list` returns the same template data `init`'s prompt reads; `blueprint.list`'s
response carries the same shipped-vs-override provenance `aeon blueprint list` prints. None of the three
is a second implementation of what its CLI counterpart already does — and none of them needed a new
*write* method: applying a preset composes `routes.set` and `routes.probe`, both of which already exist.

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/13-cli/design.md`** — C1–C13, and the correction notes on C1, C7 and C8.
2. **`../ARCHITECTURE.md` §8 P31** — the six instances `doctor` must report without flattening. **This is the list D10 is
   checked against.**
3. **`../S18-providers/design.md`** — the three capability states and the route shape presets must produce (C6, C18).
4. **`../S19-narrow-surface/`** — its O8 requires `doctor` to report the HTTP bind; its O1 is the credential rule this slice
   generalises.
5. **`../S22-retrieval/design.md`** — the four availability reasons and per-corpus staleness.
6. **`../S1-bootstrap/`** — what the framework's config machinery provides, and B8's allow-list discipline.
7. **The repository `README.md`** as it stands. It is the first thing a stranger reads, and it is part of the five minutes.

### 10.2 The patterns to copy

- **S16's and S19's discriminated errors** — the four and five kinds this slice's shape must accommodate.
- **S18's route mechanism** for presets. Presets produce routes; they are not a second configuration path.
- **S11's committed-corpus discipline** for the preset notes O7 requires.
- **S22's both-build-configurations testing** — `doctor` must work with retrieval absent.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | What config machinery does the framework provide, and does it layer as C2 requires? | The scaffolding's own config service, and S1's B8. **If it provides more than needed, B8's removal discipline applies** |
| Q2 | How many distinct error types exist across the slices, and can they all carry three parts? | `internal/forge/`, `internal/narrow/`, `internal/policy/`, `internal/retrieval/`. **If any cannot, that is a finding** — C16 is structural or it is nothing |
| Q3 | Enumerate every three-state and multi-state value `doctor` must report. | P31's six, plus retrieval's four reasons, plus S19's mountings, plus forge state. **Produce the list; D10 is checked against it** |
| Q4 | Does `serve` currently bind any port, or serve anything? | `cmd/aeon/serve.go`. D15 and C20 |
| Q5 | What exit codes exist today, and do they distinguish `waiting`? | `cmd/aeon/`. C10 |
| Q6 | Where do credentials get read today — how many places? | Grep for key and token reads. **I1 requires one** |
| Q7 | Does the app's first run need anything `init` does not do? | `aeon/ui/` (S9). A stranger who runs `init` and opens the app should not have a second setup |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q8 | What does each of the three presets contain — model, capabilities, price, base URL (C6)? |
| Q9 | Which OpenAI-compatible servers will be tried, at which versions (O7)? |
| Q10 | What are the documented exit codes, and what does each mean (C10, D12)? |
| Q11 | What does `init` do with an existing config (O5)? |

---

## 11. Behavioural contract

- The five-minute path is README → install → `init` → run. Anything more is a finding (D1).
- Config layers five deep; a failure names key and file (D2, D3).
- Credentials: environment or a dedicated file; never config, `argv` or output (D4, D5, D6, I1, I2).
- `init` is fully scriptable with no TTY (D7); it probes (D8); a probe failure leaves `declared` and says so (O6); it does
  not overwrite an existing config without `--force` (O5).
- Three presets, each named server actually tried and recorded (D9, O7).
- `doctor` reports every state in its own states, flattens nothing, and also says what is fine (D10, O3, I4, I5) —
  and `doctor.check` returns the identical report over the socket (D10, I10, P34).
- Errors carry `What`, `Why`, `Do`; `Do` is an action, and a command where one exists (D11, O4, I3).
- Exit codes distinguish `failed`, `blocked`, `waiting`, documented (D12).
- No network call except configured routes and forges, traced over a **full loop** (D13, O8, I6).
- A project blueprint overrides the shipped one, and `blueprint.list`'s response states which — matching
  what `aeon blueprint list` prints (D14, I10, P34).
- `serve` binds no port and serves no UI; no spawned runner (D15, I7).
- `presets.list` returns the three presets' route templates over the socket; applying one composes the
  existing `routes.set` and `routes.probe` rather than a new write method (D16, I10, P34).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**, **except the task carrying D1**, which is gated by a recorded
  human measurement (I8).
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **the error shape comes first.** Every other task produces errors, and retrofitting a third required field across
  five packages is worse than starting with it.
- **R5** — **`doctor` is its own task, and it comes last among the code**, because it reports what every other task
  configures. A `doctor` written early reports a system that does not exist yet.
- **R6** — **presets and `init` are one task.** A preset with no `init` is data nobody reads; an `init` with no presets asks
  about base URLs.
- **R7** — **the timed measurement is the final task, and it must not be merged with anything.** A task that both polishes
  and measures will report the measurement it wanted.
- **R8** — no task over roughly 300 changed lines or 12 files without justification.
- **R9** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure",
  "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md`, runs the measurement, and amends the QA suites.
- **R12** — **no task edits `docs/_archive/aeon-v1-horizontal/13-cli/design.md`** (C1, C7, C8's corrections are already landed) and **no task
  adds a capability, route type, tool or action** (N8).

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines without justification (R8) |
| A3 | List every task with the **test name** that gates it | Every task has one **except D1's**, which names a recorded measurement (R1, I8) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | Paste Q3's enumeration of every state `doctor` must report | **A missing list blocks the plan.** D10 is checked against it |
| A6 | Paste Q2's answer | **If any existing error type cannot carry three parts, that is a finding** — C16 is structural or it is nothing |
| A7 | Paste Q6's answer | How many places read credentials. **I1 requires one** |
| A8 | Paste Q8's preset contents and Q9's server list with versions | O7 |
| A9 | Confirm **the measurement task is not merged with any other** | R7 |
| A10 | Confirm **no task adds a capability, route type, tool or action** | N8, R12 |
| A11 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | **Confirm no task adds telemetry of any kind** | N1 |
| B2 | Confirm no task adds auto-update | N2 |
| B3 | Confirm no task adds a login or account | N3 |
| B4 | Confirm no task builds an installer | N4 |
| B5 | Confirm no task submits to a package manager | N5 |
| B6 | Confirm no Gemini preset | N6 |
| B7 | Confirm no task runs the ladder on Aeon's own repository | N7 |
| B8 | Confirm no task reimplements a served UI, a port from `serve`, or a spawned runner | N9, D15 |
| B9 | Quote the `out_of_scope` lines covering N1–N9 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D2–D16 to a task and a named test | §2 |
| C2 | For D1, name **who** will run it and confirm they **have not read the design** | D1, C14 |
| C2a | For D1, confirm it is **repeated per provider**, ideally with different testers | C21 |
| C3 | For D1, confirm the record captures **exact words**, not a paraphrase | O1 |
| C4 | For D1, confirm the run is recorded **even on a pass** | O2 |
| C5 | For D10, confirm the check searches for the literal strings "ok" and "all good" over uncertain values | D10 |
| C6 | For D11, confirm the assertion is **compile-time** | D11, I3 |
| C7 | For D13, confirm the trace covers **a full loop with tools and a forge action** | D13, O8 |
| C8 | For D6, confirm **every** error path is exercised in the credential-printing check | D6 |
| C9 | Confirm `doctor` is tested in **both** build configurations, retrieval present and absent | S22 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm the error shape is the first task | R4 |
| D2 | Confirm `doctor` is last among the code tasks | R5 |
| D3 | Confirm presets and `init` are one task | R6 |
| D4 | Confirm the measurement is the final task, alone | R7 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm one credential reader | I1 |
| E2 | Confirm `errs.Error`'s three fields are required by the type | I3 |
| E3 | Confirm `doctor` computes nothing it reports | I4 |
| E4 | Confirm no network call outside configured routes and forges | I6 |
| E5 | Confirm `serve` binds no port | I7 |
| E6 | Confirm `doctor.check`, `presets.list`, and `blueprint.list`'s provenance field each call the same function their CLI counterpart calls — no second implementation | I10 |
| E7 | Confirm no socket method applies a preset directly; the app composes `routes.set` + `routes.probe` | D16 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | Paste Q3's full list of states `doctor` reports. |
| F2 | State the three presets' contents (Q8) and the servers tried with versions (Q9, O7). |
| F3 | State the documented exit codes and their meanings (Q10, D12). |
| F4 | State what `init` does with an existing config (Q11, O5). |
| F5 | State whether the app needs setup `init` does not do (Q7). |
| F6 | State what the framework's config machinery provides and what Aeon registers (Q1). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it and fixes whatever does not match.

**And in this slice the demo contract is the Definition of Done.** Every other slice's §14 said a demo proves the tests were
worth passing. Here the demo **is** the test, and it is the only one in seventeen slices that a machine cannot run.

**§1 must be run by someone who has not read the design** (C14). Every author of every onboarding flow believes theirs is
clear, and a tester who knows which flag exists is not measuring a stranger's five minutes. **Record their exact words**
(O1) — *"I didn't know if I needed a project first"* names a missing sentence; "confused about setup" does not.

**§5 is where the plan either holds or is undone.** Six independent slices refused to collapse an uncertain answer into a
certain one, and `doctor` is the one screen reporting all of them. **If any line reads "ok" over something uncertain, six
slices of design work have been discarded at the last possible moment** — and it would look helpful doing it.

**§6 traces a full loop**, not one step. The connection nobody expects appears on the path nobody exercised.

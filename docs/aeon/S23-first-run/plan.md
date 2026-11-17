---
slice: S23-first-run
title: First run & polish
stage: 4
stage_name: Space shuttle
position: 4 of 4 — the last slice
brief: docs/aeon/S23-first-run/brief.md
design: docs/aeon/S23-first-run/design.md
demo: docs/aeon/S23-first-run/DEMO.md
depends_on: [everything]
task_count: 9
---

# Plan — S23 First run & polish

**The last slice**, and the only one whose Definition of Done is a measured test on a person: *a stranger reaches a running
loop in under five minutes, timed, by someone who has not read the design.*

Five minutes is a **budget**. Every question asked spends from it, so the ordering that matters is not the code's — it is
**what a stranger hits first**: README, install, `init`, first loop, and `doctor` when something is wrong.

## Task sequence

| Task | Deliverable | Gate |
| --- | --- | --- |
| **T1** | The three-part error shape, structurally required | An error missing `Do` **does not compile** |
| **T2** | Config layering, validation, and the one credential rule | A key in a config file is a load-time error |
| **T3** | Exit codes: `failed`, `blocked`, `waiting`, documented | CI can tell a human is needed from a failure |
| **T4** | Presets **and** `aeon init`, with probing | Every prompt has a flag; the whole flow runs with no TTY |
| **T5** | Blueprints shipped and overridable | A project's own takes precedence |
| **T6** | No telemetry, asserted by a network trace | No connection except configured routes and forges |
| **T7** | The README and the four-step path | Nothing in the path beyond README → install → `init` → run |
| **T8** | `doctor` — every state in its own states | The strings "ok" and "all good" appear over nothing uncertain |
| **T9** | **The timed measurement**, and the QA suites | A stranger, under five minutes, recorded |

## Ordering rationale

- **T1 first** (R4). Every other task produces errors, and retrofitting a third **required** field across five packages is
  worse than starting with it. Its gate is compile-time, which is the only way C16 is more than a convention.
- **T2 second**, because `init` writes config and the credential rule is what it must obey.
- **T3 independent**, and small.
- **T4 as one task** (R6): a preset with no `init` is data nobody reads, and an `init` with no presets asks a stranger about
  base URLs.
- **T5, T6, T7 independent** of each other.
- **T8 last among the code** (R5), because it reports what every other task configures. **A `doctor` written early reports a
  system that does not exist yet** — and its job is rendering, not deciding.
- **T9 alone** (R7). A task that both polishes and measures will report the measurement it wanted.

## Files

| File | Tasks | Reason for overlap |
| --- | --- | --- |
| `internal/errs/errs.go` | T1 | The enforcement point |
| `internal/forge/`, `internal/narrow/`, `internal/policy/`, `internal/retrieval/` | T1 | Each existing error type adopts the shape. **Q2: if any cannot carry three parts, that is a finding** |
| `internal/config/layers.go`, `validate.go`, `creds.go` | T2 | Layering, validation and the one credential reader |
| `cmd/aeon/*.go` | T3 | Exit codes; a sweep across commands, hence one task |
| `internal/presets/presets.go`, `cmd/aeon/init.go` | T4 | One task (R6) |
| `aeon/blueprints/standard/` | T5 | Shipped defaults |
| `internal/blueprint/load.go` | T5 | Sole change: project override precedence |
| `cmd/aeon/doctor.go` | T8 | Renders; decides nothing |
| `README.md` | T7 | The repository's own, and part of the five minutes |
| `docs/test_suites/aeon-cli.md`, `aeon-runcontrol.md` | T9 | Amended, never rewritten |

## The one Definition of Done a machine cannot check

**D1 is not a test.** It is a recorded human measurement, and **no automated test may claim to cover it** — a test that does
has replaced it.

Three requirements that make it real rather than ceremonial:

- **The tester has not read the design** (C14). Every author of every onboarding flow believes theirs is clear, and a tester
  who knows which flag exists is not measuring a stranger's five minutes.
- **Their exact words are recorded** where they hesitated or asked (O1). *"I didn't know if I needed a project first"* names
  a missing sentence; "confused about setup" does not.
- **The run is recorded even on a pass** (O2). A four-minute run with a forty-second hesitation has found something.

**It cannot be self-certified.** If no such person is available, the slice is not done.

## What this slice does not do

**No telemetry, ever** — not opt-in, not anonymised, not for crash reports · no auto-update · no account or login · no GUI
installer · no package-manager submissions (distribution work, and it belongs **after** the five minutes are known to
work) · no Gemini preset (v1.1) · **no dogfooding** (S17 deferred it to *after* this slice — it is the next thing) · **no new
capability, route type, tool or action** — if the five-minute path seems to need one, that is a finding about an earlier
slice · **and no reimplementation of C7's served UI or C8's spawned runner.**

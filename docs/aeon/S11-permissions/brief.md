# Brief — S11 Permissions & the nine tools

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Inherits:** G1–G13 from [`docs/_archive/aeon-v1-horizontal/04-permissions`](../../_archive/aeon-v1-horizontal/04-permissions/design.md) and W1–W10 from
[`docs/_archive/aeon-v1-horizontal/05-tools`](../../_archive/aeon-v1-horizontal/05-tools/design.md). New decisions begin at **G14** and **W18**.
**Stage:** 3 · Car, first of four. Depends on S1–S9. **Blocks S13.**

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief overrides it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** G1–G13 and W1–W10 are **inherited and not open**. Read the two superseded designs for their rationale.
> The corpus counts in `04-permissions` were re-measured and corrected when this slice was specified (§9 I2).

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
| **D1** | **A pattern with no wildcard matches exactly.** `gh pr merge` against a policy whose only `gh` rule is bare `gh` is **denied**. This is the silent over-permit and the single most important matcher test — a matcher assuming a trailing `*` fails it while passing everything else. |
| **D2** | **`*` matches anywhere**, including mid-pattern: `docker buildx build --platform x --push` matches `docker buildx build*--push*`. |
| **D3** | **`*` is the only wildcard.** `?`, `[a-z]` and `**` are literal characters. |
| **D4** | **Every rule in the extracted corpus evaluates**, each pattern against a command it must match and one it must not. The corpus **is** the specification, so the tests are enumerated from it. |
| **D5** | **The corpus is extracted, and asserted as properties, not counts** (G19): mid-`*` patterns exist, no-wildcard patterns exist, three patterns carry conflicting verdicts. A count assertion is a **failing** check for this slice — this design's own re-measurement found three transcribed numbers stale. |
| **D6** | **First match wins**, over an ordered array (G2). |
| **D7** | **An unmatched command is denied, and an empty policy denies everything** (G3, G4). |
| **D8** | **`Evaluate` is pure and total** — no I/O, clock or environment in its imports; a fuzz seed of malformed commands and policies never panics (G1). |
| **D9** | **A verdict carries the rule that produced it** (G11). "Denied" is unactionable; "denied by `git push*`" is not. |
| **D10** | **Any denied segment denies the whole command** (G8). `cd sub && git push` is denied, and the verdict names **which** segment. |
| **D11** | **The stated limit is a documented expectation, not a bug.** `bash -c "git push"` is **not** caught, asserted as such, so nobody later reads the matcher as a sandbox or files it as a defect. |
| **D12** | **`spawn` cannot be called without an `AllowToken`** (G16), and no package outside `policy` can construct one. Asserted at compile time, not by convention. |
| **D13** | **The verdict precedes the receipt.** A denied `bash` leaves the receipt table **empty** (G15). |
| **D14** | **A denied command is proven never to have spawned.** A command that would create a file: the file does not exist. Asserting only a returned error passes against code that spawned and discarded the result. |
| **D15** | **An attended `ask` suspends and its question carries the command and the deciding rule** (G18). |
| **D16** | **An unattended `ask` denies immediately, naming the rule, and creates no question** (G7). |
| **D17** | **Attendedness is pinned at loop start** (G14) and is **unchanged** by a client connecting or disconnecting mid-run. Asserted by doing both. |
| **D18** | **One path-enforcement point, exercised through all four file tools** (W18): absolute refused, `..` refused, and a **symlink escaping the root** refused. The symlink case is what separates checking the written path from checking the resolved one. |
| **D19** | **`edit` is exact-match and fails loudly on a stale string** (W19) — with a test comment recording that **S7's `detectable` re-run class depends on it**. |
| **D20** | **`webfetch` sends no credential header, ever** — asserted on the recorded request, checking for `Authorization`, `Cookie` and any bearer token. A POST is refused; a cross-host redirect is handled explicitly. |
| **D21** | **`todo` leaves nothing in the store** (W20), asserted by querying the task and loop tables after a step that used it. |
| **D22a** | **The `bash` tool description the model receives names the matcher's limits** (G20): `bash -c`, `$( )`, backticks, and that evading the policy violates it regardless of whether the matcher catches it. **A missing string fails this check even when the bypasses behave as documented** — the mechanism cannot stop them, so the instruction is the control. |
| **D22** | **`--allow-bash`'s stderr warning is retired** and its help no longer says there is no command policy. A warning that outlives its cause trains people to ignore warnings. |
| **D23** | **All nine tools are registered with a re-run class** (S7's W16), and the committed class list is updated so the six that were *not yet applicable* now are. |
| **D24** | **`make test` and `make lint` green**, `make fmt` leaves no diff. |

**D1, D12 and D14 are the three that matter.** D1 is the one over-permit that would go unnoticed. D12 is what makes
"checks before spawning" a property rather than a convention. D14 is the demo's central claim.

**D1–D23 must each exist as named tests.**

D24 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

`bash` has bounds but **no policy** (S5's W12), is not granted by default, and `--allow-bash` warns on stderr every
single time. This slice retires that warning by making it unnecessary.

**The policy is already written**, in this repository's own agent definitions. Re-measured for this slice: **38 of
75** agent files carry a `permission.bash` block, **366 rules** over **50 distinct patterns** — 115 `allow`, 244
`deny`, 7 `ask`.

**The counts had drifted since the superseded design; every structural claim held.** That drift is the finding, and
it is why G19 requires extraction rather than transcription: a number is stale the day someone adds an agent, and
the properties the decisions rest on are not.

**`ask` now has a destination.** S9's thread renders questions inline. Which raises the tension this slice must
resolve: U14 says a step question and a gate render identically, and a permission ask is a third kind. U14 forbids
distinguishing the **mechanism** — it does not license withholding **what is being decided** (G18).

**And "unattended" needs redefining.** G7 resolves `ask` to deny when nobody can answer, and that used to be a
property of a CLI invocation. The daemon now owns loops. Inferring attendedness from whether a client is subscribed
would make the same command allowed or denied depending on whether someone had a window open — so it is **declared
and pinned** (G14).

## 4. Goal

Nine tools, and a guard that decides `bash` from real per-agent policy — where a denied command is proven never to
have spawned, and an `ask` reaches a human who can see what they are approving and which rule made it their
decision.

## 5. Shape and the seam

```
bash tool ──▶ policy.Evaluate(pol, cmd) ──▶ Verdict
                                              │
             ┌────────────────────────────────┼──────────────────────┐
          allow                             ask                    deny
             │                                │                      │
        AllowToken                    attended? suspend       tool error,
             │                        else      deny          no receipt,
      write receipt                                           no spawn
             │
          spawn(tok, bounds)   ← unreachable without the token
```

### The seams

**`Evaluate` is pure and total** (G1, D8). It is consulted on every `bash` call, and a policy decision that can fail
open is not a guard.

**The `AllowToken`** (G16, D12) turns G12's "structurally, not as source ordering" into a Go type: a `spawn` you
cannot reach without having asked, with unexported fields so no other package can forge one.

**The evaluator never asks** (G6). It **returns** `ask`; the caller resolves it — suspending in an attended loop,
denying in an unattended one. A pure function cannot suspend a step.

**One path-enforcement point** (W18) for `read`, `write`, `edit`, `glob`, `grep`. Four call sites, one check.

**Policy attaches to handlers here, agents at S13** (G17). The corpus ships now as committed test data, because it
is the specification and the thing that interprets it arrives now.

## 6. Locked decisions

**Inherited (G):** G1 (pure and total), G2 (ordered array, first match), G3 (unmatched denies), G4 (empty denies),
G5 (`ask` is real), G6 (the evaluator returns, the caller resolves), G7 (unattended `ask` → deny), G8 (segment
splitting, any denied segment denies all), G9 (`*` only, anywhere, exact without), G10 (`tools` is an allow-list),
G11 (a verdict carries its rule), G12 (consulted before any spawn, structurally), G13 (per-agent, nothing hoisted).

**Inherited (W):** W1 (nine tools), W2 (paths root-relative and enforced), W3 (mandatory timeout), W4 (stdin
closed), W5 (`oomKilled` as a fact), W6 (a denial is a tool error, not a step failure), W7 (this slice owns the
enforcement point), W8 (`todo` is within-step), W9 (`webfetch` GET-only, capped, no credential), W10 (every tool
honours `exec.signal`).

**New:** G14 (attendedness declared and pinned), G15 (verdict before receipt), G16 (the token), G17 (handlers now,
agents at S13), G18 (an ask carries command and rule), G19 (extracted, not transcribed), G20 (the model is told the
limits), W18 (one path check), W19 (`edit` exact-match, and S7 depends on it), W20 (`todo` never reaches the store).

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | A verdict carries the rule | It carries the rule **and the agent whose policy produced it** (G13's consequence). With the same pattern carrying different verdicts in different agents, "denied by `git push*`" is ambiguous about **whose** `git push*` — and that is what tells a user where to go and change it. |
| O2 | Segment splitting handles `&&`, `\|\|`, `;`, `\|` | It must also handle **newline-separated** commands, which the superseded design lists among the things it does *not* catch. A multi-line command string is ordinary, not adversarial, and a newline is a separator in every shell. Splitting on it costs nothing and closes an accident-shaped hole. |
| O3 | The stated limit is documented | It must be documented **where a user will meet it** — in `aeon policy check`'s output and in the policy documentation — not only in a design file. A limit recorded only in a spec is a limit nobody affected will read. |
| O4 | `webfetch` never sends a credential header | It must also **not follow a redirect to a different host** without an explicit decision, and must never send the original request's headers to a redirect target. Redirect-following is the classic way a "GET-only, no-credential" fetcher leaks. |
| O5 | The design does not say what happens when a policy fails to load | A handler whose policy will not parse **cannot run `bash` at all** — not "falls back to deny-everything silently", but a loud refusal naming the file. G4 says an empty policy denies everything, and an unparseable one must not be silently equivalent to it: one is a deliberate choice, the other is a broken file. |
| O6 | `ask` suspends in an attended loop | The question must be **answerable exactly once**, reusing S6's mechanism. And a *second* identical `ask` later in the same step is a **new** question — approving `git push` once does not approve it forever. Asserted, because caching the answer is the obvious optimisation and it is wrong. |
| O7 | The corpus is extracted at build time | The extractor **reads `opencode/agents/*.md` and never writes to it** (standing constraint). It is committed generated output with a CI diff check, exactly as S9's protocol types are. |
| O8 | `todo` is within-step | Its content must appear **in the transcript**, so it survives into the record even though it never reaches the store. A within-step device that leaves no trace is one nobody can review after the fact. |

## 8. Non-goals

- **N1 — No sandbox.** Standing non-goal, restated because this is the slice where someone would expect one. The
  tool allow-list is the boundary; the matcher is not.
- **N2 — No agent bodies, no `by-tech-tag`.** **S13.**
- **N3 — No git or forge actions.** **S16.** The demo's `git push` is a `bash` command, which is why it needs a
  guard.
- **N4 — No new permission UI.** S9's thread renders it. U5's rule is honoured by there being nothing new to build.
- **N5 — No `search`.** S22. Nine tools, not ten.
- **N6 — No policy authoring UI.** Never promised for the alpha.
- **N7 — No network policy.** A denied `curl` is denied by pattern, not by a namespace.
- **N8 — No shell parser.** Segment splitting plus O2's newline handling, and nothing more. A half-complete parser
  produces confident wrong answers.
- **N9 — No change to `Evaluate`'s purity for convenience.** If a case seems to need I/O, the policy structure is
  wrong.

### Standing non-goals

- **Never write to `opencode/`.** It is read by the corpus extractor and never modified (O7).
- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never run `git clean`.**
- **Never commit a token or secret.** `webfetch` captures must never include request headers with credentials —
  redact to `<redacted>`.

## 9. Repo invariants

**I1 — `Evaluate` is pure and total** (G1, D8). No I/O, clock or environment.

**I2 — The corpus is extracted, never transcribed** (G19). The superseded design's counts were corrected when this
slice was specified — that correction is **already landed**; do not re-edit it, and do not reintroduce a count
assertion.

**I3 — `spawn` is unreachable without an `AllowToken`** (G16, D12), enforced by unexported fields.

**I4 — The verdict precedes the receipt** (G15, D13).

**I5 — One path-enforcement point** (W18), and it checks the **resolved** path.

**I6 — `edit` is exact-match** (W19). S7's re-run classification depends on it, and breaking it corrupts files
during recovery.

**I7 — `todo` never reaches the store** (W20, D21), but always reaches the transcript (O8).

**I8 — Policy is per-agent and nothing is hoisted** (G13). No shared default, no "common denials" file.

**I9 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/04-permissions/design.md`** in full — G1–G13, the corpus analysis, the threat-model section,
   and the re-measurement note. **The corpus is the specification.**
2. **`docs/_archive/aeon-v1-horizontal/05-tools/design.md`** — W1–W10 and the nine tools' contracts.
3. **`opencode/agents/github-steward.md` and `git-loop.md`** — the seven `ask` rules, in the real files. Read them
   to understand what an `ask` is *for*.
4. **`docs/test_suites/aeon-permissions.md`** — **QAP-01 to QAP-08 already cover most of G1–G13.** Read it before
   planning: QAP-05 specified G20's control before the design did, QAP-08 already requires the extractor to be
   read-only, and QAP-03 requires an unattended denial to say *nobody was available* rather than *the policy denied
   it* — those are different facts and the message must not conflate them. **Plan the amendment around what is
   already there.**
4. **`internal/tools/bash.go`** (S5) — bounds without policy, and where the enforcement point goes.
5. **`internal/tools/class.go`** (S7's T2) — the re-run classes, including the six declared-but-absent tools this
   slice implements.
6. **`internal/store/receipts.go`** (S7's T1) — what G15 orders itself against.
7. **`internal/handler/registry.go`** (S8's T7) — where policy attaches (G17), and the assertion that **no
   blueprint field can grant a tool**.
8. **S9's thread and `Ask.tsx`** — the affordance an `ask` renders through, and U14's rule.

### 10.2 The patterns to copy

- **S9's generated-and-diff-checked** approach, for the corpus extractor (O7).
- **S7's structured `RecoveryFailure`** and S8's `BlockedReason` as the model for a structured verdict.
- **Discriminated results over errors** (S4's DB19) — a denial is a domain outcome, not an infrastructure failure.
- **S8's committed-fixture pattern** for the re-run class list.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | What exact YAML shape do the `permission.bash` blocks take, and is it uniform across all 38 files? | `opencode/agents/*.md` front matter. **If it is not uniform, the extractor must handle each shape and say so** — a silently skipped file is a silently ungoverned agent |
| Q2 | How does S5's `bash` currently reach `exec`, and what is the smallest change that makes the spawn token-gated? | `internal/tools/bash.go`. G16 is a refactor of an existing path, not a new one |
| Q3 | Does S6's question mechanism accept a question that is **not** tied to a `tool_use_id`, as a gate is? | `internal/store/schema.go`, S6's design. A permission ask **is** tied to a `tool_use_id` — the `bash` call — so confirm both shapes exist |
| Q4 | Where is a loop's pinned data written at start, and can attendedness join it? | `internal/loop/pin.go` (S8's T3). G14 pins alongside the blueprint |
| Q5 | What does S7's committed re-run class list say about the six absent tools, and what marks them present? | `internal/tools/testdata/rerun_classes.json` |
| Q6 | Does the handler registry have a place for policy, or does it need one? | `internal/handler/registry.go`. **It must not become a place to grant tools** (S8's I5) |
| Q7 | How does `read` currently resolve paths, and is there already a check W18 should absorb rather than duplicate? | `internal/tools/read.go` (S5) |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q8 | How is a symlink escaping the project root detected, and on which platforms does the method work (D18)? |
| Q9 | What is `webfetch`'s size cap and timeout, and how is truncation reported (W9)? |
| Q10 | What happens to a second identical `ask` in one step (O6)? State it; caching the answer is the obvious optimisation and it is wrong. |
| Q11 | Which of the nine tools honour `exec.signal` today, and which need it added (W10)? |

---

## 11. Behavioural contract

- The matcher: `*` only, anywhere; no wildcard means exact (D1–D3).
- Every corpus rule evaluates; the corpus is extracted and asserted as properties (D4, D5).
- First match wins; unmatched and empty both deny (D6, D7).
- `Evaluate` is pure and total; a verdict carries its rule **and agent** (D8, D9, O1).
- Any denied segment denies the whole; newlines are separators too (D10, O2).
- The stated limit is documented where a user meets it (D11, O3).
- `spawn` requires an `AllowToken`; nothing outside `policy` can construct one (D12).
- Verdict → receipt → spawn; a denial produces neither (D13, D14).
- Attended `ask` suspends with command and rule; unattended denies at once with no question (D15, D16).
- Attendedness is pinned and unaffected by client connections (D17).
- A second identical `ask` is a new question (O6).
- One path check, on the resolved path, through all file tools (D18, I5).
- `edit` is exact-match and fails loudly (D19, I6).
- `webfetch`: GET-only, capped, no credential header ever, cross-host redirects explicit (D20, O4).
- `todo` reaches the transcript and never the store (D21, O8).
- An unparseable policy is a loud refusal, not a silent deny-all (O5).
- `--allow-bash`'s warning retired; all nine tools carry a re-run class (D22, D23).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **the slice has two halves**: the guard and the tools. Sequence them so the guard is complete before
  `bash` is rewired, because a half-built evaluator behind a live `bash` is the one state where an over-permit ships.
- **R5** — **the corpus extractor is its own task**, and it precedes the matcher. The matcher's tests are enumerated
  from the corpus, so the corpus must exist first.
- **R6** — **the matcher is its own task**, carrying D1–D3. It is the smallest, most-tested, highest-consequence
  code in the slice.
- **R7** — **the token and the enforcement ordering are one task** (G15, G16). They are the same property viewed
  twice: nothing runs without a verdict, and nothing is recorded before one.
- **R8** — **`write` and `edit` share a task**; `glob` and `grep` share one. Each pair shares its path handling and
  its failure modes.
- **R9** — no task over roughly 300 changed lines or 12 files without justification.
- **R10** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure",
  "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R11** — a task carries no open question.
- **R12** — the final task walks `DEMO.md` and amends the QA suites.
- **R13** — **no task edits `opencode/`** or `docs/_archive/aeon-v1-horizontal/04-permissions/design.md` (I2, already landed).

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R10, R11) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines without justification (R9) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | Paste Q1's answer | **If the `permission.bash` shape is not uniform across all 38 files, enumerate the variants.** A silently skipped file is a silently ungoverned agent |
| A6 | Extract the corpus and paste its **properties** — mid-`*`, no-wildcard, conflicting verdicts | Present. **A count assertion in the plan is a failing check** (D5, I2) |
| A7 | Name the compile-time mechanism preventing `spawn` without a token | Not a convention, not a comment (D12) |
| A8 | Paste Q3's answer | Both question shapes — with and without a `tool_use_id` — confirmed to exist |
| A9 | Paste Q8's answer, per platform | Symlink escape detection (D18) |
| A10 | `grep -rn "database/sql\|os.Getenv\|time.Now"` over the planned `evaluate.go` | No hits (I1) |
| A11 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task builds a sandbox or a namespace | N1 |
| B2 | Confirm no task writes an agent body or a resolver | N2 |
| B3 | Confirm no task implements a git or forge action | N3 |
| B4 | Confirm no task builds new permission UI | N4 |
| B5 | Confirm no task implements `search` | N5 |
| B6 | Confirm no task writes a shell parser beyond segments and newlines | N8, O2 |
| B7 | Confirm no task adds I/O to `Evaluate` | N9, I1 |
| B8 | Confirm no task writes to `opencode/` or edits `docs/_archive/aeon-v1-horizontal/04-permissions` | R13, I2 |
| B9 | Quote the `out_of_scope` lines covering N1–N9 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D23 to a task and a named test | §2 |
| C2 | For D1, confirm the test is `gh pr merge` against a bare `gh` rule expecting **deny** | D1 |
| C3 | For D5, confirm **no** test asserts a corpus count | D5, I2 |
| C4 | For D12, confirm the assertion is compile-time and that a forging attempt from another package fails to build | D12 |
| C5 | For D14, confirm the assertion is a **filesystem effect absent**, not a returned error | D14 |
| C6 | For D17, confirm the test **connects and disconnects a client** mid-run | D17 |
| C7 | For D18, confirm all four file tools are exercised, and the **symlink** case is present | D18 |
| C8 | For D19, confirm the test comment records S7's dependency | D19, I6 |
| C9 | For D20, confirm the assertion is on the **recorded request headers**, and that a cross-host redirect is covered | D20, O4 |
| C10 | For D21, confirm the assertion **queries the store**, not the tool's return value | D21 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm the guard is complete before `bash` is rewired | R4 |
| D2 | Confirm the corpus extractor precedes the matcher | R5 |
| D3 | Confirm the matcher is its own task | R6 |
| D4 | Confirm the token and the ordering are one task | R7 |
| D5 | Confirm `write`+`edit` and `glob`+`grep` are paired | R8 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm `Evaluate` has no I/O, clock or environment | I1 |
| E2 | Confirm `AllowToken`'s fields are unexported and its only constructor is in `policy` | I3 |
| E3 | Confirm one path-enforcement function, called by all file tools, checking the **resolved** path | I5 |
| E4 | Confirm no shared policy default exists anywhere | I8, G13 |
| E5 | Confirm the corpus extractor only reads `opencode/` | O7 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | State the symlink-escape method and its platform coverage (Q8, D18). |
| F2 | State `webfetch`'s cap, timeout, and how truncation is reported (Q9). |
| F3 | State what a second identical `ask` in one step does (Q10, O6). **Caching the answer is wrong.** |
| F4 | Enumerate which tools need `exec.signal` added (Q11, W10). |
| F5 | State what happens when a policy will not parse (O5). |
| F6 | State where the matcher's limit is documented for a user, not a designer (O3). |
| F7 | Quote the limits string the model will receive in the `bash` tool description (G20, D22a). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R12) walks it and fixes whatever does not match.

**Three sections carry the slice.**

**§1** — the prompt must show the **deciding rule**. A prompt reading only *"approve `git push origin feature/x`?"*
is a FAIL, because the user cannot then tell whether **the rule is wrong or the command is** — and those call for
opposite actions.

**§2** — the denied command is **proven never to have spawned**, by a filesystem effect that is absent. And the
receipt table must be empty: a receipt for a refused command turns a clean denial into a step recovery must fail as
unknown.

**§4** — the evaluator run against the real corpus, ending in the two cases that matter: `gh pr merge` denied
against a bare `gh` rule (the silent over-permit that a plausible matcher gets wrong), and `bash -c "git push"`
allowed, **which is correct**. If the second reads as a bug to whoever walks the demo, §4's wording has failed and
the fix is the wording — nobody should leave this demo thinking the matcher is a sandbox.

# Brief — S18 Provider breadth

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Inherits:** M1–M25 from [`docs/_archive/aeon-v1-horizontal/01-model-layer`](../../_archive/aeon-v1-horizontal/01-model-layer/design.md) and
[`../S3-model-layer`](../S3-model-layer/design.md). New decisions begin at **M26**.
**Stage:** 4 · Space shuttle, first of four. Depends on S3, S6, S7. **Independent of S19, S22, S23.**
**Amended 2026-10-26 for P34** (`ARCHITECTURE.md`): *"The daemon surface is the primary interface — the CLI
is secondary… A capability that is not reachable through the daemon surface is not finished."* This brief
predates P34; D16/M32/I10 below are the amendment — found while auditing Stage 4+5 against the doctrine
`S34-trust-bench` established. Route CRUD itself (`routes.list/set/delete`) already has a socket door, built
at S3 — the gap this amendment closes is narrower: **capability declaration and probing**, which T2/T3
originally wired to `cmd/aeon/route.go` alone. **Re-aligned to the shipped tree the same day**: adapters live in
`internal/model/adapters/`; the CLI verb is plural (`aeon routes`, `cmd/aeon/routes_verbs.go`); S18's wire flags are
the separate `WireCapabilities` field beside the model-management wave's UI `Capabilities`; D10/I2 are re-worded as a
file-list pin recording the `openai_responses.go` precedent.

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief overrides it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** This slice's central job is to **make good on an inherited claim (M13)** and to make that claim falsifiable.
> §12's rules are shaped around not quietly disproving it.

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
| **D1** | **The A→B→A round trip works.** Suspend on Anthropic, resume on OpenAI, resume again on Anthropic. **The third leg is what makes it a proof** — A→B shows translation works; A→B→A shows the record survived, which is the property the canonical format was bought for. |
| **D2** | **A thinking block dropped at the wire is still in the store.** Asserted at every point of D1's round trip. If a drop ever reaches the record, the trip is one-way. |
| **D3** | **Thinking blocks are replayed only to the provider that produced them** (M27), and a cross-provider drop is **counted** (M4). |
| **D4** | **Every assistant turn carries its provider and model** (M26), and a transcript spanning two providers reports both. |
| **D5** | **Cost splits by provider**, and a total over an `unknown` component reads as a **floor**. |
| **D6** | **Three capability states** — `verified`, `declared`, `contradicted` — and **a contradiction is a refusal, not a log line** (M28). |
| **D7** | **A probe costs one request per route**, and its result is cached so it is not paid per step. |
| **D8** | **A step needing a capability its route lacks is refused before the model is called** (M29), asserted with the client spy at **zero**. |
| **D9** | **An unmetered route's cost is `unknown` with a reason naming it**, and the string `$0.00` appears nowhere (M30). |
| **D10** | **No third adapter** (M13). Asserted by a **file-list pin on `internal/model/adapters/`**: this slice adds no adapter file, and the registry keeps exactly two provider ids. *Amended 2026-10-26:* the directory already holds `openai_responses.go` — a second **wire format behind the one `openai` id** (measured 400 on `/v1/chat/completions`, recorded precedent), not a third provider and not a compatibility adapter. M13's claim is about the **OpenAI-compatible** path: Ollama/vLLM run on the existing adapter plus flags. |
| **D11** | **M13 is falsifiable** (M31). A committed record lists **every** difference encountered and whether a flag expressed it. One that could not is a **recorded finding**, not a third file. |
| **D12** | **Usage absent entirely yields `unknown`** and the step completes. M9's every-field-optional shape, exercised for real three slices later. |
| **D13** | **A tool call with an empty or non-unique id is handled or refused explicitly**, not left to surprise the reassembler. |
| **D14** | **A loop runs against a local model with no cloud key**, end to end. |
| **D15** | **`make test` and `make lint` green**, `make fmt` leaves no diff. |
| **D16** | **Capability state and probing are daemon methods, not CLI-only** (P34). `routes.list`'s response carries each route's capability states (declared/verified/contradicted, with the probe's timestamp — O2); `routes.probe {route}` triggers a probe, locked exactly like S34's `mcp.check` — operator-triggered only, one in-flight probe per route, bounded timeout, result cached with its timestamp. The CLI (`aeon routes probe`) becomes the secondary door onto the same method, not a separate implementation. |

**D1, D2 and D10 are the three that matter for M13's claim; D16 is the one that matters for P34.** D1 is the
claim. D2 is the property that makes it true rather than lucky. D10 is the inherited decision this slice
either honours or quietly breaks. D16 is what keeps this slice's new surface (capability states, probe
results) from becoming the next thing a future S37 has to retrofit.

**D1–D14 must each exist as named tests**, D14 additionally as the demo's §1.

D15 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

S3 shipped two adapters and deferred the rest here. **M13 is the claim to make good on:** the OpenAI-compatible path is
the OpenAI adapter plus capability flags, because *"the differences are absences, not behaviours."*

Mostly true. Some differences are behaviours — a tool call with an empty `id`, usage only in a final chunk — so M31 makes
M13 **falsifiable**: a difference that cannot be a flag is a finding, not a third file.

**And one thing makes cross-provider resume harder than it looks.** M17 replays thinking blocks verbatim with their
signatures because Anthropic rejects an altered history. OpenAI has no such concept, so they are dropped — which works,
**because the drop is at the wire and the store is untouched** (M1).

The second-order problem: after an OpenAI turn, the transcript holds an Anthropic signature followed by a turn Anthropic
did not produce. Replaying that is a bet on someone else's validator, so **thinking goes only to its producer** (M27) —
which needs provenance (M26).

## 4. Goal

Aeon running against a local model with no cloud key, and a loop suspended on Anthropic resumed on OpenAI — with every
capability difference expressed as a flag rather than a second code path.

## 5. Shape and the seam

```
canonical transcript (untouched)  ──▶ adapter ──▶ wire
     producedBy: anthropic/…            │
     [thinking(sig), text, tool_use]    ├─ anthropic: replay thinking verbatim (M17)
                                        └─ openai:    drop + COUNT (M4, M27)

route ── capabilities ─▶ declared │ verified │ contradicted (M28)
                             │
                    pre-call check (M29) ──▶ refuse before the call
```

### The seams

**The store is never modified by an adapter.** That is M1, and it is what makes D1's third leg possible.

**Provenance is on the turn, not on the loop** (M26). A loop that switches provider mid-run has turns from both, and a
loop-level field could not describe that.

**Capabilities are on the route** (M12: a route is data). The probe verifies; it does not overwrite. A probe that rewrote
the route would make the declaration meaningless and the contradiction invisible.

**The pre-call check is in the runtime, not the adapter** (M29). An adapter refusing a request it cannot serve is too
late — the step has already been compiled and its context resolved.

## 6. Locked decisions

**Inherited:** M1 (one canonical format, adapters both directions), M4 (four block kinds; unmapped content dropped **and
counted**), M9 (usage all-optional; absent is not zero), M10 (cost three-state), M11 (unknown fields dropped; `extraBody`
the one escape hatch), M12 (a provider is selected by a route, and a route is data), **M13 (no third adapter)**, M17
(thinking round-trips verbatim with signature), M25 (prices are USD per million tokens).

**New:** M26 (provenance per turn), M27 (thinking replayed only to its producer), M28 (declared and verified; three
states; contradiction is loud), M29 (pre-call capability check), M30 (unmetered is `unknown` with a reason), M31 (M13 is
falsifiable), **M32 (probing is a daemon method, locked exactly like S34's `mcp.check`: operator-triggered
only — no auto-probe on page load or route save; one in-flight probe per route; bounded timeout; result
cached with its timestamp, read back through `routes.list`. `tools/call`'s analogue here — an actual model
request beyond the minimal probe call — never happens outside the probe's own narrow request shape, asserted
by a test against a fake transport, the same discipline S34 used for `mcp.check` never reaching `tools/call`).**

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | A dropped block is counted | The count must reach **the step's record and the app**, not only a log. S9 renders three-state values; a dropped-block count that lives in a log line is invisible where it matters, and "why did this turn behave differently" is exactly the question it answers. |
| O2 | Capabilities are declared and probed | A probe result **carries its timestamp and the model it probed**. A cached `verified` from three weeks ago against a model that has since been replaced is a stale claim wearing a verified badge. |
| O3 | A contradiction is loud | It must be **loud at probe time and again at use time**. Someone who probes on Monday and runs on Friday should not have to remember; the refusal at use is what actually protects the loop. |
| O4 | Provenance records provider and model | It must also record the **route name**. Two routes can point at the same provider and model with different prices or base URLs, and cost attribution needs the route, not just the model. |
| O5 | The design does not say what happens when a route is switched **mid-step** | **Refuse it.** A route switch takes effect at the next step; changing provider between a `tool_use` and its `tool_result` produces a turn no adapter can construct honestly. |
| O6 | Thinking blocks are dropped for providers that lack them | The drop is **per-provider, not per-capability-flag**. A future provider that supports reasoning in a *different* format still cannot receive Anthropic's signatures — so the rule keys on **who produced it**, exactly as M27 says, and never on whether the target "supports thinking". |
| O7 | The OpenAI-compatible path uses capability flags | Its `CAPABILITIES.md` record must include the **servers actually tested against**, by name and version. "Works with OpenAI-compatible servers" is unfalsifiable; "tested against Ollama 0.x and vLLM y.z" is a claim someone can check. |
| O8 | An unmetered route reports `unknown` | The route must declare `--no-price` **explicitly**. A route with a missing price is a **configuration error**, not silently unmetered — otherwise a typo in a price makes a metered model report `unknown` forever, and nobody notices they stopped tracking cost. |

## 8. Non-goals

- **N1 — No Gemini.** v1.1. M12 keeps it one adapter file plus a registry entry.
- **N2 — No model catalogue.** Routes are configuration; **S23** supplies them.
- **N3 — No automatic failover.** A route is chosen deliberately. Silently retrying on another provider changes what
  produced the work without saying so, and M26 exists because that matters.
- **N4 — No model ranking or quality comparison.**
- **N5 — No prompt-caching strategy.** The adapter reports cache tokens; what to cache is the runtime's.
- **N6 — No embeddings.** S22.
- **N7 — No third adapter** (M13, D10). If one seems necessary, that is **a finding about M13** to record and raise.
- **N8 — No change to the canonical format's block kinds.** M4's four are closed. A provider with content that fits none
  is a drop, counted.
- **N9 — No mid-step route switching** (O5).

### Standing non-goals

- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never commit a key.** Redact to `<redacted>`; recorded captures carry no request headers.
- **Never run `git clean`.**
- **Never point an automated test at a live provider.** Recorded fixtures and a local server, always.

## 9. Repo invariants

**I1 — An adapter never modifies the canonical transcript** (M1, D2). Asserted by comparing the store before and after a
request is built.

**I2 — No adapter beyond the two providers, one file per wire format** (M13, D10). The non-test `.go` files under
`internal/model/adapters/` are **pinned by test** — `anthropic.go`, `openai.go`, `openai_responses.go` (the recorded
second-format precedent), `registry.go`. The registry registers two provider ids.

**I3 — Thinking replay keys on the producing provider** (M27, O6), never on the target's capabilities.

**I4 — Provenance is per turn, and includes the route** (M26, O4).

**I5 — A probe never rewrites a route's declaration** (M28).

**I6 — `$0.00` is never produced for an unpriced route** (M30, D9).

**I7 — No automated test calls a live provider.**

**I8 — P31 now names six instances**; that correction is already landed. Do not re-edit `ARCHITECTURE.md`.

**I9 — Public API changes ship with their tests in the same task.**

**I10 — Capability state is reachable from the daemon surface, not only `cmd/aeon/routes_verbs.go`** (P34, D16,
M32). `routes.probe`'s socket handler and the CLI's `aeon routes probe` reach the same underlying probe
function — one implementation, two doors, the same pattern `mcp.check` and `aeon mcp list --agent` share.

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/01-model-layer/design.md`** — M1–M18, and especially **M13's reasoning** and the adapter difference
   table. This slice's whole job is downstream of it.
2. **`../S3-model-layer/design.md`** — M19–M25, the two adapters, and what it explicitly left here.
3. **`internal/model/adapters/anthropic.go`, `openai.go`, `openai_responses.go`** — the adapters, one file per wire
   format. **Read them before deciding what a flag can
   express**; M31 depends on that judgement being informed.
4. **`internal/model/cost.go`** — M9's every-field-optional usage and M10's three states, which M30 reuses rather than
   extends.
5. **`internal/runtime/resume.go`** (S6) and **`recover.go`** (S7) — the resume paths D1's round trip goes through.
6. **An actual OpenAI-compatible server's docs** — Ollama's, and one other. O7 requires naming what was tested.

### 10.2 The patterns to copy

- **S3's recorded-fixture tests.** No live provider, ever (I7).
- **S9's three-state renderers** for the capability states and the dropped-block count (O1).
- **S11's committed-corpus discipline** for `CAPABILITIES.md`: a record generated from what was encountered, not
  transcribed from memory.
- **Discriminated results over errors** (S4's DB19) — a contradiction is a domain refusal, not an infrastructure failure.

### 10.3 Confirm each from the code

| # | Question | Where |
| --- | --- | --- |
| Q1 | Does S3's `Message` have room for provenance, or does it need a field? Does the store? | `internal/model/`, `internal/store/schema.go`. **If a migration is needed, name the task** |
| Q2 | How does S3's Anthropic adapter currently emit thinking blocks, and where would the producer check go? | `internal/model/adapters/anthropic.go` |
| Q3 | Does S3's OpenAI adapter already branch on anything route-shaped, or is this the first? | `internal/model/adapters/openai.go` (and `openai_responses.go`, which branches on `openaiEndpoint`). M13's difficulty depends on the answer |
| Q4 | Where does a route live, and is it already data rather than code (M12)? | `internal/model/`, and S23's expectations |
| Q5 | What does S3's cost path do today when usage is entirely absent? | `internal/model/cost.go`. **If it produces zero rather than `unknown`, that is a bug M9 already forbade** |
| Q6 | Does resume (S6) or recovery (S7) assume the same route as the original step? | `internal/runtime/`. D1 needs a route switch between suspend and resume |
| Q7 | How are dropped blocks counted today, and where does the count go (M4)? | `internal/model/`. O1 requires it reach the record |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q8 | What is the full capability flag set, and what does each absence change? Produce the table before planning. |
| Q9 | How does the probe work per capability, and what does it cost? |
| Q10 | Which OpenAI-compatible servers will be tested against, by name and version (O7)? |
| Q11 | What happens to a tool call with an empty or duplicate `id` (D13)? State it. |

---

## 11. Behavioural contract

- A→B→A round trip works; the store is unmodified throughout (D1, D2, I1).
- Thinking replays only to its producer; a cross-provider drop is counted and the count reaches the record (D3, O1, O6).
- Every turn carries provider, model **and route** (D4, O4).
- Cost splits by provider; a total over `unknown` is a floor (D5).
- Three capability states; a contradiction refuses at probe **and** at use (D6, O3).
- A probe result carries its timestamp and the model probed (O2); it never rewrites the declaration (I5).
- A probe is one request per route, cached (D7).
- A missing capability refuses **before** the call (D8).
- An unmetered route reports `unknown` with a reason; a **missing** price is a configuration error (D9, O8).
- Two adapter files; every difference is a flag or a recorded finding (D10, D11, O7).
- Absent usage yields `unknown` and the step completes (D12).
- An empty or duplicate tool-call id is handled explicitly (D13).
- A mid-step route switch is refused (O5).
- Capability state and the probe are reachable as daemon methods (`routes.list`'s carried state,
  `routes.probe`), not only through `aeon routes probe` (D16, I10, P34).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — **provenance comes first.** M27 needs it, cost attribution needs it, and adding it after the adapters have
  branched means editing both.
- **R5** — **the capability flags and the probe are separate tasks.** Declaring is a data shape; probing is a request and a
  cache, and they fail differently.
- **R6** — **the A→B→A round trip is its own task**, and it is the slice's claim. It comes after provenance and the replay
  rule, and its test is an integration test rather than a unit one.
- **R7** — **`CAPABILITIES.md` is written as the work happens**, not at the end (M31, O7). A record assembled afterwards
  is a record of what someone remembered.
- **R8** — no task over roughly 300 changed lines or 12 files without justification.
- **R9** — task files contain no deliberation: no "hmm", "let me", "on reflection", "I think", "not sure",
  "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the final task walks `DEMO.md` and amends the QA suites.
- **R12** — **no task adds a third adapter** (N7). If one seems necessary, record the finding and stop.

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines without justification (R8) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | Paste Q8's capability table in full | **A missing table blocks the plan.** M13's viability is exactly this table |
| A6 | Paste Q5's answer | **If absent usage produces zero today, that is a bug M9 already forbade** — name the task that fixes it |
| A7 | Paste Q6's answer | Whether resume assumes the original route. D1 needs a switch between suspend and resume |
| A8 | Paste Q1's answer | Provenance's storage, and whether a migration is needed |
| A9 | Confirm **no task adds an adapter file** — list the files each task creates | N7, D10, I2 |
| A10 | Paste Q10's list of servers to be tested, with versions | O7 |
| A11 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task implements Gemini | N1 |
| B2 | Confirm no task builds a model catalogue | N2 |
| B3 | Confirm no task implements failover | N3 |
| B4 | Confirm no task ranks models | N4 |
| B5 | Confirm no task decides a caching strategy | N5 |
| B6 | Confirm no task implements embeddings | N6 |
| B7 | **Confirm no task adds a third adapter** | N7, R12 |
| B8 | Confirm no task adds a block kind | N8 |
| B9 | Confirm no task permits a mid-step route switch | N9, O5 |
| B10 | Quote the `out_of_scope` lines covering N1–N9 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D14 to a task and a named test | §2 |
| C2 | For D1, confirm **all three legs** — A→B→A, not A→B | D1 |
| C3 | For D2, confirm the store is asserted **at every point** of the round trip | D2, I1 |
| C4 | For D3, confirm the drop is asserted **counted**, and the count reaches the record | D3, O1 |
| C5 | For D6, confirm a contradiction is a **refusal**, asserted at probe **and** at use | D6, O3 |
| C6 | For D8, confirm the client spy is at **zero** | D8 |
| C7 | For D9, confirm the literal `$0.00` is searched for and absent | D9, I6 |
| C8 | For D10, confirm the assertion is a **file count plus imports**, not a comment | D10, I2 |
| C9 | For D11, confirm `CAPABILITIES.md` is generated from what was encountered | D11, R7 |
| C10 | Confirm **no** test calls a live provider | I7 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm provenance precedes the replay rule and cost attribution | R4 |
| D2 | Confirm flags and probe are separate tasks | R5 |
| D3 | Confirm the round trip is its own task, after provenance and replay | R6 |
| D4 | Confirm `CAPABILITIES.md` is written across tasks, not at the end | R7 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm no adapter writes to the store | I1 |
| E2 | Confirm two adapter files | I2 |
| E3 | Confirm replay keys on the **producer**, not the target's flags | I3, O6 |
| E4 | Confirm a probe never rewrites a declaration | I5 |
| E5 | Confirm no path produces `$0.00` for an unpriced route | I6 |
| E6 | Confirm `routes.probe` is a registered socket method, and the CLI calls the same function the socket handler does (not a parallel implementation) | D16, I10, P34 |
| E7 | Confirm the probe never fires without an explicit `routes.probe` call — no page-load auto-probe, no probe-on-save | M32 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | Paste the capability table and what each absence changes (Q8). |
| F2 | State the probe's method and cost per capability (Q9). |
| F3 | Name the servers tested against, with versions (Q10, O7). |
| F4 | State the empty/duplicate tool-call-id behaviour (Q11, D13). |
| F5 | State whether provenance needs a migration (Q1). |
| F6 | State what a route with a **missing** price does (O8). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R11) walks it and fixes whatever does not match.

**§1 must work with the keys genuinely unset** — `env -u ANTHROPIC_API_KEY -u OPENAI_API_KEY`. A demo that works because a
key was lying around in a config file has not shown what it claims.

**§3 is the slice's proof, and it has three legs for a reason.** A→B shows the adapters translate. **A→B→A shows the record
survived** — and the middle check, that the thinking blocks are *still in the store* after the OpenAI turn, is the one that
makes the whole thing more than luck. If a drop ever reaches the record, the trip becomes one-way and nobody finds out until
they try to go back.

**§6 reads `CAPABILITIES.md`**, and that file is the honest version of "we tried and here is what we learned." It names the
servers tested and every difference met. *"Works with OpenAI-compatible servers"* is unfalsifiable; a list with versions is
a claim someone can check.

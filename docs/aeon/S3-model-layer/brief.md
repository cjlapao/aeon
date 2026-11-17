# Brief — S3 Model layer

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md)
**Translates:** [`docs/_archive/aeon-v1-horizontal/01-model-layer/`](../../_archive/aeon-v1-horizontal/01-model-layer/) — M1–M18 keep their numbers.
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — §5 is binding.
**Stage:** 1 · Skateboard, second of four. Depends on S1, S2.

> **Template deviations from `proposal-brief-protocol`, stated rather than silent:**
> **(a)** Implements a design, not a proposal — §7 records where this brief corrects it.
> **(b)** §14 is the demo contract, present in every slice of this plan.
> **(c)** §10's primary reading is a **superseded slice in this repository**, which holds the reasoning this
> slice translates rather than re-derives.

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
| **D1** | **`aeon ask` streams from both providers.** Text arrives incrementally, not in one buffered block, for Anthropic **and** OpenAI. Two tests, driven from recorded fixtures. |
| **D2** | **Round-trip is identity, per provider.** canonical → wire → canonical is deep-equal over a transcript containing all four block kinds. |
| **D3** | **A signed `thinking` block survives byte-identically** (M17). Asserted on the **exact string**, not on presence. This is what makes S7's recovery possible. |
| **D4** | **Fragments reassemble and no partial `tool_use` is ever yielded.** OpenAI's index-keyed deltas and Anthropic's `input_json_delta` both produce one complete block; every yielded delta is inspected. |
| **D5** | **SSE parsing is offset-independent.** A fixture replayed chunked at **every** byte offset — including inside a multi-byte rune and inside a `data:` line — yields output deep-equal to the unchunked parse. |
| **D6** | **Malformed tool arguments do not fail the call.** Accumulated arguments that are not valid JSON yield a `tool_result` with `IsError: true`, and the call continues. |
| **D7** | **Absent usage is never zero.** A fixture with no usage yields `State: unknown` with a reason, and a test asserts the result is **not** `{State: known, USD: 0}`. |
| **D8** | **Partial usage reports a floor.** `State: partial` with `USDAtLeast` and a non-empty `Missing` naming the absent fields. |
| **D9** | **The retry boundary holds.** A 500 before the first delta retries and succeeds; a 500 after it fails, and the test asserts the delta was yielded **exactly once**. |
| **D10** | **Cancellation reports the spend.** A cancelled `context` resolves with the usage seen, does not hang, and does not discard cost. |
| **D11** | **Anthropic's two structural rules hold**: `system` is hoisted out of the message slice, and two tool results for one turn become **exactly one** user message. |
| **D12** | **`Validate()` rejects a block carrying a field foreign to its kind** — a `text` block with a `ToolUseID`. This is what M20's tagged-struct trade is paid for with. |
| **D13** | **Streaming reaches clients as ordinary protocol events** (M23). `aeon watch --kinds 'ask.*'` receives the same deltas; no second streaming mechanism exists. |
| **D14** | **No provider shape escapes.** `grep` for `choices`, `tool_calls`, `content_block`, `x-api-key`, `stream_options` finds hits only under `internal/model/adapters/`. |
| **D15** | **No unit test contacts a provider.** The suite passes with no network and no keys in the environment. |
| **D16** | **Nothing is persisted** (M24). No table is created, no file is written outside a temp directory. |
| **D17** | **`make test` and `make lint` are green**, `make fmt` leaves no diff. |

D3, D5 and D7 are the three that matter, and each fails somewhere other than here: a mangled signature fails
in S7's recovery, an offset bug truncates a tool call under load, and a zero cost reaches the UI at S9 looking
like a cheap step.

**D1–D16 must each exist as named tests.**

D17 is a standing condition, never a task's sole acceptance criterion (R1).

---

## 3. The problem

The slice where Aeon first does something worth paying for, and the substance is inherited rather than
invented: `docs/_archive/aeon-v1-horizontal/01-model-layer` established M1–M18 and a thirteen-row table of places where the
provider families differ. Every row is a place where passing the caller's messages straight to the wire would
have failed.

**Two adapters, not one.** The canonical format exists *because* pass-through breaks at the second provider,
so validating it against one is the mistake the v1 plan made — the format looks correct until something with a
different shape arrives. One extra file removes the risk.

Three things change in Go, and one is a simplification: **the bug class that justified an SSE parsing
dependency in TypeScript is largely a JavaScript problem.** A `TextDecoder`'s state across chunk boundaries is
the hazard; working in `[]byte` and decoding once removes it. So M7 is superseded by M19 — parse it in-house,
and keep the offset-independence test that made the dependency worth having.

## 4. Goal

`aeon ask "…"` streams a real reply from Anthropic or OpenAI over the protocol, and reports what it cost in one
of three honest states.

## 5. Shape and the seam

```
protocol `ask` ──▶ model.Client ──▶ adapters/{openai,anthropic} ──▶ net/http ──▶ SSE
                        │                      │
                        │                      └── ToWire / FromWire
                        └── retry (M14) · context (M21) · cost once (M10)
                        │
                   deltas ──▶ S2 subscriptions (M23)
```

### The seams

**Upward — `Client`.** S5's runtime builds a `ModelRequest` and consumes deltas. It never constructs a wire
body and never learns which provider answered.

**Downward — the adapter interface.** `ToWire(req) (*http.Request, error)` and
`FromWire(frames) (<-chan Delta, error)`, plus `Finalise() (Usage, string, int)`. Adding the compatible path at
S18 must be one new file plus a registry entry — name the interface as its own task so that is provable.

**Sideways — `format.go`.** S4 imports `FormatVersion`, `ValidateTranscript`, `MigrateTranscript` and the
`Message` type. **Nothing else.** A wider surface couples the store to the wire.

**Outward — `ask` over the protocol.** Additive to S2's method table, so `PROTOCOL_VERSION` does not move.

## 6. Locked decisions

- **M3** — `content` is always a block slice.
- **M4** — four block kinds; unmapped content dropped and **counted**.
- **M6** — streaming only. No non-streaming convenience path.
- **M9** — every `Usage` field optional; absent is never coerced to zero.
- **M14** — no retry after the first delta.
- **M16** — `MaxOutputTokens` required, not defaulted at the wire.
- **M17** — `thinking` round-trips verbatim including `Signature`.
- **M19** — SSE parsed in-house. **Do not add a dependency.**
- **M20** — tagged structs with `Validate()`. **Do not build an interface-based union.**
- **M22** — `Parameters` is `json.RawMessage`; this layer never inspects it.
- **M23** — deltas ride S2's subscriptions. **No second streaming mechanism.**
- **M24** — nothing is persisted.

## 7. Overrides to the design

| # | Design says | This brief requires |
| --- | --- | --- |
| O1 | `cost.go` computes cost | Cost is computed **once**, in `client.go`, from `cost.go`'s pure helpers. An adapter must not compute it, so a third adapter at S18 cannot get it wrong. Asserted by grep. |
| O2 | `Cost` uses `omitempty` on `USD` | **`State` must have no `omitempty`.** A genuinely-known zero cost must still serialise its state, and this struct tag is the single place M9's "absent is not zero" can be undone silently. |
| O3 | The design shows `Block` with `omitempty` throughout | `IsError` must **not** be `omitempty`. A tool result that failed and one that succeeded would otherwise serialise identically, and the model would see no difference. |
| O4 | Fixtures are "captured once per provider" | If a real capture is unavailable, a hand-written fixture is acceptable **only if the manifest says so** and names what was not verified. A hand-written fixture presented as a recording is a failed check. |
| O5 | `ask` accepts a `subscriptionId` | It is **optional**. Without one, `ask` returns the whole reply in its result — synchronously, buffered. That path exists for a script and for testing, and it must be explicitly a different mode rather than an accident, because it is the one that would hide a streaming bug (D1). |
| O6 | The design does not say what happens on an unknown route id | A named error listing the configured route ids. "route not found" without the list is a support question, and at this stage the list is short enough to print. |

## 8. Non-goals

- **N1 — No OpenAI-compatible path, no capability flags.** S18 (M13).
- **N2 — No persistence.** S4 (M24).
- **N3 — No tools executed.** `ToolSpec` is carried to the wire; nothing runs. S5 and S6.
- **N4 — No prompt-caching strategy.** Report cache tokens; decide nothing.
- **N5 — No embeddings.** S22.
- **N6 — No model catalogue or discovery.** Routes are configuration; S23 supplies them.
- **N7 — No token counting.** An estimate is the runtime's, at S5.
- **N8 — No Gemini.** v1.1; M12 keeps it one file.

### Amended 2026-10-15 — the UI non-goal is superseded

**A slice now ships the UI surface that exercises it** (`aeon/AGENTS.md` §4b), at the user's direction. S3
added **T13**: a Providers page and an Ask page, in a labelled **test bench**.

**S9's scope is unchanged** — it builds the real app. What changed is that each slice no longer defers its
own surface to it: *"this is to test what we need in the place where it is consumed."*

### Standing non-goals

- **Never write outside `aeon/`**, except `docs/` and `docs/test_suites/`.
- **Never commit an API key.** A recorded SSE capture will contain headers — redact to `<redacted>`. This is
  the slice most likely to commit one by accident.
- **Never run `git clean`.**

## 9. Repo invariants

**I1 — `internal/model` is a leaf.** It imports nothing under `internal/` except `protocol`'s error helpers.
Name the mechanism that catches a violation.

**I2 — No provider shape escapes the adapters** (D14), asserted by grep rather than intent.

**I3 — One SSE reader.** Both providers go through it. A second framing implementation diverges and only one
gets tested — and in a stream parser, divergence means one path truncates.

**I4 — Cost is computed in exactly one place** (O1).

**I5 — No ambient state.** No `os.Getenv` inside an adapter; credentials and base URLs arrive on the `Route`.
This is the invariant whose violation makes the layer untestable without secrets, and it passes review because
it works on the author's machine.

**I6 — Fixtures are committed, redacted, and named for provider and scenario**, with a manifest.

**I7 — No test touches the network** (D15).

**I8 — Public API changes ship with their tests in the same task.**

---

## 10. Investigation brief

### 10.1 What to read

1. **`docs/_archive/aeon-v1-horizontal/01-model-layer/design.md`** in full — M1–M18 and the thirteen-row provider table. This
   slice translates it; do not re-derive it.
2. **`docs/_archive/aeon-v1-horizontal/01-model-layer/tasks/`** — twelve task specs. Several port almost directly (cost
   arithmetic, the retry boundary, fixture discipline); T6's pass-through is dead.
3. **`docs/_archive/aeon-v1-horizontal/01-model-layer/brief.md`** §11 — the behavioural contract, still accurate.
4. **The Anthropic Messages API streaming reference** — the event sequence, and the extended-thinking
   constraints on replaying `thinking` with tool use.
5. **The OpenAI Chat Completions streaming reference** — `tool_calls` delta accumulation and `stream_options`.
6. **`internal/protocol/`** (S2) — the method table this adds to, and the event envelope deltas ride in.

### 10.2 The patterns to copy

- **Discriminated results over exceptions**, as S1 and S2 do.
- **Injected dependencies** — an `http.Client` field, so D15 is achievable.
- **Table-driven tests** enumerated from data.
- **S2's error registry**: any error this slice returns over the protocol allocates a code there.

### 10.3 Confirm each from the code or the API

| # | Question | Where |
| --- | --- | --- |
| Q1 | Does each provider return usage on a **streaming** request, and in exactly what shape? | Live probe, one per provider. Paste the raw final chunk. A "no" blocks nothing but decides what the demo's cost line shows. |
| Q2 | What exactly does Anthropic require when replaying a `thinking` block alongside `tool_use`? | API reference. **Quote the constraint**; D3 is written against it. |
| Q3 | Does OpenAI accept `max_tokens`, `max_completion_tokens`, or both, at the model you will use? | Live probe. |
| Q4 | Which Go SSE approach — `bufio.Scanner` with a custom `SplitFunc`, or `bufio.Reader` line-wise? Which handles a `\r\n\r\n` separator and a final frame with no trailing blank line? | Try both against a fixture. D5 depends on the answer. |
| Q5 | How is a `tool_use` `Input` preserved byte-identically through `json.RawMessage` — does `encoding/json` renormalise it? | Test it. S7 replays these. |
| Q6 | What does S2's event envelope require, and which error codes must this slice allocate? | `internal/protocol/`. |

### 10.4 Project-specific questions

| # | Question |
| --- | --- |
| Q7 | How is a fixture captured and redacted? Name the script and the check that proves no key was committed. |
| Q8 | What are the route ids in this slice, and where do they come from before S23's config exists? |
| Q9 | For `ask` without a `subscriptionId` (O5), what bounds the buffered reply? An unbounded buffer is a memory hole with a prompt-shaped cause. |
| Q10 | Does `Validate()` run at every boundary that accepts a `Block` from outside? Enumerate those boundaries. |

---

## 11. Behavioural contract

Inherited from `docs/_archive/aeon-v1-horizontal/01-model-layer/brief.md` §11, with these additions:

- `Validate()` rejects a block whose fields are foreign to its `Kind` (D12), and runs at every external
  boundary (Q10).
- `Cost.State` always serialises (O2). `Block.IsError` always serialises (O3).
- An unknown route id is a named error listing the configured ids (O6).
- `ask` with a `subscriptionId` streams; without one it returns a bounded buffered reply (O5, Q9).
- Cancellation via `context` resolves with the usage seen (M15, D10).
- Retries only before the first delta, tracked by a **flag rather than an attempt counter** (M14, D9).

---

## 12. How to decompose

- **R1** — every task is gated by at least one **named test**.
- **R2** — a task that adds behaviour adds its test in the same task.
- **R3** — a file in more than one task's `files_expected` needs a stated reason.
- **R4** — the adapter interface is its **own task, before either adapter**, so M12 is structural rather than
  aspirational.
- **R5** — `sse.go`, `format.go` and `cost.go` are pure and precede the adapters. All three are depended on by
  everything after.
- **R6** — **one adapter per task.** Do not build both in one.
- **R7** — no task over roughly 300 changed lines or 12 files without justification.
- **R8** — the fixture task precedes the tests that consume its fixtures, and it is the task that states
  recorded-or-hand-written per fixture (O4).
- **R9** — task files contain no deliberation: no "hmm", "let me", "wait", "on reflection", "I think", "not
  sure", "FIXME". `TODO(engineer):` in a code skeleton is the one permitted form.
- **R10** — a task carries no open question.
- **R11** — the `ask` protocol method and the CLI verb are separate tasks. One is a contract, the other a
  client.
- **R12** — the final task walks `DEMO.md` end to end, with real keys, and fixes what does not match.

## 13. Pre-dispatch self-check

### A. Mechanical

| # | Run | Pass condition |
| --- | --- | --- |
| A1 | `grep -rniE "\bhmm\b\|\blet me\b\|\bon reflection\b\|\bFIXME\b\|\bwait\b\|\bI think\b\|\bnot sure\b" tasks/` then `grep -rn "TODO" tasks/ \| grep -v "TODO(engineer)"` | Both empty (R9, R10) |
| A2 | List every task with `est_changed_lines` and file count | Nothing over ~300 lines or ~12 files without justification (R7) |
| A3 | List every task with the **test name** that gates it | Every task has one (R1) |
| A4 | List every file in more than one task's `files_expected` | Each justified (R3) |
| A5 | `grep` your planned files for `choices`, `tool_calls`, `content_block`, `x-api-key`, `stream_options` | Hits only in adapter tasks (D14, I2) |
| A6 | For every fixture, state **recorded or hand-written**; for D3, paste the provider constraint it was written against | Every fixture labelled. **A `thinking` round-trip test written against no quoted constraint blocks the plan** — it is the assertion most likely to be written from assumption and to pass while being wrong |
| A7 | Paste the raw output of Q1 and Q3, per provider | Measured, not inferred |
| A8 | Paste the answers to Q4 and Q5 | Both from running code. **Q5 matters because S7 replays these bytes** |
| A9 | `make fmt` then `git status --porcelain` | No output |

### B. Scope

| # | Check | Ref |
| --- | --- | --- |
| B1 | Confirm no task adds the compatible adapter or capability flags | N1 |
| B2 | Confirm no task creates a table or writes a file outside a temp dir | N2, D16 |
| B3 | Confirm no task executes a tool | N3 |
| B4 | Confirm no task adds embeddings, a catalogue or a tokeniser | N5, N6, N7 |
| B5 | Confirm **no task creates a third adapter file** | M13 deferred |
| B6 | Confirm no task adds a non-streaming code path beyond O5's bounded buffer | M6, O5 |
| B7 | Confirm no task adds an SSE dependency | M19 |
| B8 | Quote the `out_of_scope` lines covering N1–N8 | §8 |

### C. Acceptance

| # | Check | Ref |
| --- | --- | --- |
| C1 | Map each of D1–D16 to a task and a named test | §2 |
| C2 | For D7, confirm the test asserts the result is **not** `{State: known, USD: 0}` | D7 |
| C3 | For D5, name the byte offsets, including one inside a multi-byte rune | D5 |
| C4 | For D9, confirm two tests and that the second asserts the delta was yielded exactly once | D9 |
| C5 | For D3, confirm the assertion is on the exact signature string | D3 |
| C6 | For O2 and O3, confirm `State` and `IsError` carry no `omitempty` | O2, O3 |

### D. Ordering

| # | Check | Ref |
| --- | --- | --- |
| D1 | Confirm the adapter interface precedes both adapters | R4 |
| D2 | Confirm `sse.go`, `format.go` and `cost.go` precede the adapters | R5 |
| D3 | Confirm the fixture task precedes its consumers | R8 |
| D4 | Confirm the `ask` method and the CLI verb are separate tasks | R11 |

### E. Structure

| # | Check | Ref |
| --- | --- | --- |
| E1 | Confirm one SSE reader | I3 |
| E2 | Confirm cost is computed in one place | I4, O1 |
| E3 | Confirm no `os.Getenv` inside an adapter | I5 |
| E4 | Confirm `internal/model` imports nothing under `internal/` but `protocol` errors | I1 |

### F. Project-specific

| # | Check |
| --- | --- |
| F1 | Name the file S4 will import from here, and confirm it exports only the format three plus types (§5). |
| F2 | State what bounds `ask`'s buffered reply without a subscription (Q9, O5). |
| F3 | Enumerate the boundaries where `Validate()` runs (Q10). |
| F4 | Confirm no fixture contains a key, and name the check (Q7). |
| F5 | State the route ids and where they come from before S23 (Q8). |

## 14. The demo contract

> **Present in every slice of this plan.** A slice is not done when its tests pass. It is done when someone who
> did not build it can follow `DEMO.md` and watch the thing work.

[`DEMO.md`](DEMO.md) is a deliverable, and the final task (R12) walks it **with real keys** and fixes whatever
does not match, recording which was wrong.

S3's demo has one property to preserve: **it runs the same prompt against both providers and expects the same
shape of output.** That step looks redundant and is the slice's whole claim — the two wire formats disagree
about seven things, and none of it should be visible. A demo that exercised one provider would prove the
adapter and prove nothing about the format.

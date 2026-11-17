# Plan — S3 Model layer

> **For agentic workers:** written to the `dev-loop-protocol` `plan.md` + `tasks/{taskid}.md` schema.
> Each task's full spec is at `tasks/T{n}.md`.
>
> **Spec:** [`design.md`](design.md) · **Brief:** [`brief.md`](brief.md) · **Demo:** [`DEMO.md`](DEMO.md)
> **Source material:** [`docs/_archive/aeon-v1-horizontal/01-model-layer/`](../../_archive/aeon-v1-horizontal/01-model-layer/) — M1–M18.

## Understanding

### What we understood

The slice where Aeon first does something worth paying for, and most of its substance is **inherited rather
than invented**. `docs/_archive/aeon-v1-horizontal/01-model-layer` established M1–M18 and a thirteen-row table of places where the
provider families differ: where the system prompt lives, whether tool arguments arrive as a JSON string or a
parsed object, where tool results go, whether `max_tokens` is required, and four unrelated shapes of streaming
event. Every row is a place where passing the caller's messages straight to the wire would have failed, and
together they are why the canonical format exists.

**Two adapters, not one.** The format exists *because* pass-through breaks at the second provider, so
validating it against one is the mistake the earlier plan made — it looks correct until something with a
different shape arrives. One extra file removes the risk entirely, and the OpenAI-compatible path with its
capability flags stays at S18 where it belongs.

Three decisions change in Go, and one of them is a simplification worth stating: **the bug class that justified
an SSE parsing dependency in TypeScript is largely a JavaScript problem.** The hazard is a `TextDecoder`'s state
across chunk boundaries; working in `[]byte` and decoding once removes it. So M7 is superseded by M19 — parse in
house — while keeping the offset-independence test that made the dependency worth having in the first place.

The other two are Go shape rather than Go convenience. **The unions become tagged structs** (M20) because Go has
no sum types and transcripts are persisted and replayed, so JSON round-trip fidelity outranks compile-time
exhaustiveness — a hand-written codec for an interface-based union is exactly where a lost `thinking` signature
would come from. And **streaming rides S2's subscriptions** (M23) rather than a private channel, so there is one
streaming path for the whole product instead of two to reconcile at S9.

### Probes to run and record before decomposing

| # | Probe |
| --- | --- |
| Q1 | **Does each provider return usage on a streaming request, and in what shape?** One probe per provider, raw final chunk pasted |
| Q2 | **What exactly does Anthropic require when replaying a `thinking` block alongside `tool_use`?** Quote the constraint — D3 is written against it |
| Q3 | Does OpenAI accept `max_tokens`, `max_completion_tokens`, or both, at the model you will use? |
| Q4 | Which Go SSE approach handles a `\r\n\r\n` separator **and** a final frame with no trailing blank line — `bufio.Scanner` with a custom `SplitFunc`, or line-wise `bufio.Reader`? Try both |
| Q5 | **Does `encoding/json` renormalise a `json.RawMessage` on round-trip?** S7 replays these bytes, so byte-identity is the requirement |
| Q6 | What does S2's event envelope require, and which error codes must this slice allocate in S2's registry? |

> Q2 and Q5 are the two that must be **run or quoted**, not assumed. Q2's constraint is what D3 asserts
> against; Q5 decides whether a stored tool call survives S7's recovery.

### How we will do it

Pure parts first, then the interface, then the adapters, then composition, then the surface.

`types.go` opens because the tagged-struct shape (M20) is what every later task builds on and is expensive to
change once adapters marshal through it. Then `format.go`, `sse.go` and `cost.go` — all pure, all independently
testable, all depended on by everything after.

**The adapter interface is its own task, before either adapter** (R4). An interface extracted after two
implementations exist tends to be the shape of whichever was written first, and M12's promise — that S18's
compatible adapter is one new file — is only provable if the interface predates them.

Then fixtures (R8), because a stream test is only as trustworthy as its fixture and the manifest's
recorded-or-hand-written label is what makes that judgeable later. Then **one adapter per task** (R6): they
share a specification and nothing else, and their failures are unrelated.

`client.go` composes, and is where the two rules that cannot live anywhere else land — cost computed once, and a
retry gated by a flag rather than an attempt counter. Then `ask` as a protocol method, then the CLI, then the
demo walk with real keys.

### What you will have at the end

`aeon ask` streaming from two providers that disagree about seven things, with none of the disagreement visible,
and a cost line that says `known`, `at least`, or `not reported` **with a reason** — never `$0.00`.

## Objective

Build `internal/model` and the `ask` method so a person can talk to a model through Aeon and see honestly what
it cost.

## Tech tags in use

go, docs

## Test suite

aeon-model

## Tasks

### T1 — `types.go`: the tagged unions, constructors, `Validate`
- engineer: golang-engineer-loop
- tech: go
- depends_on: []
- est_changed_lines: ~290
- detail: tasks/T1.md
- success_criteria:
  - unit test `Validate rejects a text block carrying a ToolUseID` passes, one case per foreign field
  - unit test `Cost.State always serialises, including for a known zero` passes
  - unit test `Block.IsError always serialises` passes
  - unit test `a Block round-trips through JSON byte-identically, Input included` passes
  - unit test `internal/model imports nothing under internal/ but protocol errors` passes, asserted by grep
  - constructors exist for all four block kinds and produce values that pass `Validate`
- files_expected: `aeon/internal/model/types.go`, `aeon/internal/model/types_test.go`

### T2 — `format.go`: version, validation, migration
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~280
- detail: tasks/T2.md
- success_criteria:
  - unit test `ValidateTranscript rejects two consecutive assistant messages and names the index` passes
  - unit test `ValidateTranscript rejects a transcript beginning with assistant` passes
  - unit test `ValidateTranscript rejects a tool_result whose ToolUseID matches no earlier tool_use` passes
  - unit test `ValidateTranscript accepts a suspended transcript ending on an unanswered tool_use` passes
  - unit test `ValidateTranscript never returns an error for a nil or empty input, only a result` passes
  - unit test `MigrateTranscript refuses a future formatVersion, naming both versions` passes
- files_expected: `aeon/internal/model/format.go`, `aeon/internal/model/format_test.go`

### T3 — `sse.go`: the in-house reader
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~250
- detail: tasks/T3.md
- success_criteria:
  - unit test `frames parse identically when the byte stream is chunked at every offset` passes
  - unit test `a data: line split inside a multi-byte rune reassembles correctly` passes
  - unit test `both \n\n and \r\n\r\n separate frames` passes
  - unit test `a final frame with no trailing blank line is delivered` passes
  - unit test `[DONE] terminates without yielding a frame, and its absence ends cleanly` passes
  - unit test `comment lines and retry fields are ignored` passes
  - unit test `the reader honours context cancellation between frames and releases the body` passes
  - unit test `an erroring underlying reader propagates rather than ending silently` passes
- files_expected: `aeon/internal/model/sse.go`, `aeon/internal/model/sse_test.go`

### T4 — `cost.go`: the three states
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T1]
- est_changed_lines: ~230
- detail: tasks/T4.md
- success_criteria:
  - unit test `every priced field present yields known` passes
  - unit test `some priced fields absent yields partial with Missing naming them` passes
  - unit test `no usable usage yields unknown with a reason` passes
  - unit test `a route with no prices yields unknown with a distinguishable reason` passes
  - unit test `absent usage never yields known with USD zero` passes
  - unit test `prices are USD per million, proven against a hand-computed example` passes
  - unit test `the returned value is not rounded` passes
- files_expected: `aeon/internal/model/cost.go`, `aeon/internal/model/cost_test.go`

### T5 — The adapter interface and registry
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T3]
- est_changed_lines: ~200
- detail: tasks/T5.md
- success_criteria:
  - unit test `the registry resolves an adapter for each provider id` passes
  - unit test `an unknown route id fails with a named error listing the configured ids` passes
  - unit test `a stub adapter for a fictional provider works end to end` passes, and its diff touches only `adapters/`
  - unit test `Delta has no representation for a partial tool_use` passes, as a type-level assertion
  - unit test `no adapter performs I/O` passes, asserted with an http.Client that panics
- files_expected: `aeon/internal/model/adapters/registry.go`, `aeon/internal/model/types.go`, `aeon/internal/model/adapters/registry_test.go`

### T6 — Fixture capture and redaction
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T5]
- est_changed_lines: ~190
- detail: tasks/T6.md
- success_criteria:
  - every fixture is labelled `recorded` or `hand-written` in a manifest, with what was not verified named for each hand-written one
  - unit test `no fixture contains a credential` passes, matching credential-shaped strings across the directory
  - the set covers, per provider: text-only, a tool call, usage present, usage absent; and for Anthropic a signed `thinking` block and two tool calls in one turn
  - unit test `every fixture parses through the SSE reader` passes
  - `scripts/capture-fixture.go` records a stream with headers redacted
- files_expected: `aeon/internal/model/testdata/**`, `aeon/internal/model/testdata/manifest.json`, `aeon/scripts/capture-fixture.go`, `aeon/internal/model/fixtures_test.go`

### T7 — The OpenAI adapter
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T6, T2, T4]
- est_changed_lines: ~300
- detail: tasks/T7.md
- success_criteria:
  - unit test `system stays in the message array` passes
  - unit test `tool_use Input is marshalled into function.arguments as a string` passes
  - unit test `the output cap uses the name the route specifies, and the other name is absent` passes
  - unit test `index-keyed tool_calls fragments reassemble into one complete tool_use` passes
  - unit test `no partial tool_use is ever yielded` passes, over every delta
  - unit test `accumulated arguments that are not valid JSON yield IsError and the call continues` passes
  - unit test `a fixture with no usage yields a Usage with no fields set, not zeroes` passes
  - unit test `an unrecognised delta key increments dropped` passes
- files_expected: `aeon/internal/model/adapters/openai.go`, `aeon/internal/model/adapters/openai_test.go`

### T8 — The Anthropic adapter
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T6, T2, T4]
- est_changed_lines: ~320
- detail: tasks/T8.md
- success_criteria:
  - unit test `system is hoisted to a top-level parameter and removed from the array` passes
  - unit test `two tool results for one turn become exactly one user message` passes
  - unit test `max_tokens is always present` passes
  - unit test `tool_use Input is sent as an object, not a string` passes
  - unit test `a signed thinking block round-trips byte-identically` passes, asserting the exact signature string, with the provider constraint quoted in the test file
  - unit test `signature_delta is accumulated but never yielded as content` passes
  - unit test `usage is accumulated from both message_start and message_delta` passes
  - unit test `x-api-key and anthropic-version are set and Authorization is absent` passes
- files_expected: `aeon/internal/model/adapters/anthropic.go`, `aeon/internal/model/adapters/anthropic_test.go`

### T9 — `client.go`: routing, retry, cancellation, cost once
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T7, T8]
- est_changed_lines: ~310
- detail: tasks/T9.md
- success_criteria:
  - unit test `a 500 before the first delta retries and succeeds` passes
  - unit test `a 500 after the first delta fails, and the delta was yielded exactly once` passes
  - unit test `a 400 is not retried` passes
  - unit test `Retry-After is honoured in seconds and HTTP-date form` passes
  - unit test `a cancelled context resolves with the usage seen and does not hang` passes, with a timeout
  - unit test `cost is constructed only in client.go and cost.go` passes, asserted by grep
  - unit test `the whole suite passes with no network and no keys` passes
- files_expected: `aeon/internal/model/client.go`, `aeon/internal/model/client_test.go`

### T10 — The `ask` protocol method
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T9]
- est_changed_lines: ~280
- detail: tasks/T10.md
- success_criteria:
  - unit test `ask with a subscriptionId streams deltas as event notifications` passes
  - unit test `ask.delta and ask.done are received by a subscriber to ask.*` passes
  - unit test `ask without a subscriptionId returns a bounded buffered reply` passes
  - unit test `the buffered reply's bound is enforced and the truncation reported` passes
  - unit test `PROTOCOL_VERSION is unchanged by this addition` passes
  - unit test `an unknown route id returns a named error listing the configured ids` passes
  - the error codes this slice adds are allocated in S2's registry
- files_expected: `aeon/internal/protocol/ask.go`, `aeon/internal/protocol/method.go`, `aeon/internal/protocol/errors.go`, `aeon/internal/protocol/ask_test.go`

### T11 — The CLI: `aeon ask`, `aeon debug roundtrip`
- engineer: golang-engineer-loop
- tech: go
- depends_on: [T10]
- est_changed_lines: ~260
- detail: tasks/T11.md
- success_criteria:
  - unit test `aeon ask prints text incrementally, not in one block` passes
  - unit test `aeon ask --route selects the provider` passes
  - unit test `the cost line renders known, partial and unknown distinctly` passes
  - unit test `unknown cost renders its reason and never $0.00` passes
  - unit test `Ctrl-C prints the partial answer and a cost line` passes
  - unit test `aeon debug roundtrip reports identical and preserves a thinking signature` passes
  - unit test `aeon ask --show-transcript prints canonical JSON` passes
- files_expected: `aeon/cmd/aeon/ask.go`, `aeon/cmd/aeon/debug.go`, `aeon/cmd/aeon/ask_test.go`

### T12 — Walk the demo, and the QA suite
- engineer: technical-writer-loop
- tech: docs
- depends_on: [T11]
- est_changed_lines: ~180
- detail: tasks/T12.md
- success_criteria:
  - `DEMO.md` is walked end to end **with real keys for both providers**, and every stated output matches
  - every mismatch is fixed and this task's notes record whether the code or the demo was wrong
  - `docs/test_suites/aeon-model.md` is **amended, not rewritten** — its `QAM-` ids are stable and append-only
  - the amended suite covers both providers live, the cost comparison against provider dashboards, a signed-`thinking` round trip, cancellation, and a real 429
- files_expected: `docs/aeon/S3-model-layer/DEMO.md`, `docs/test_suites/aeon-model.md`

## Priority order

**T7, T8 and T9 are the deliverable**, and T8 carries the property that fails furthest from home.

T7 and T8 are where the thirteen-row difference table stops being a document and becomes code. The inbound
halves are the hard ones: OpenAI keys tool-call fragments by **array index** with the id arriving only on the
first fragment, so accumulating by id loses everything after it and the symptom is a tool call with empty
arguments. Anthropic splits usage across `message_start` and `message_delta`, so a reader that looks at one
gets half the cost.

**T8's signature test is the one to get right.** Anthropic rejects a tool-use turn whose prior `thinking` was
altered, and S7 recovers a crashed step by **replaying a stored transcript** — so a signature that survives
almost intact fails four slices later, in a different package, with an error about signatures and no hint that
this layer was involved. The test asserts the exact string, and the provider's constraint is quoted in the test
file so the assertion is written against the rule rather than against an assumption.

T9 holds the two rules that cannot live anywhere else. Cost is computed **once**, so a third adapter at S18
cannot get it wrong. And the retry gate is a **flag**, not a counter: retrying a 429 is correct until a delta
has reached the client, after which a retry either duplicates output or asks the UI to un-render it. An attempt
counter cannot express that distinction and will happily retry after streaming half a response.

T1–T4 make the rest safe. T1's tagged-struct trade is the slice's one deliberate loss of type safety, bought
back with `Validate()` at every external boundary — and the two `omitempty` rules (O2, O3) are where that trade
could be quietly undone: a known-zero cost that fails to serialise its state, or a failed tool result
indistinguishable from a successful one.

T5 exists so that S18's compatible adapter is provably one file. T6 exists because a stream test is worth
exactly what its fixture is worth, and the manifest's recorded-or-hand-written label is the only thing that
makes that judgeable a year later.

A plan ordered by demo value would have written `ask` and the CLI first, because they are the visible part. It
would then have discovered at S7 that a stored transcript cannot be replayed.

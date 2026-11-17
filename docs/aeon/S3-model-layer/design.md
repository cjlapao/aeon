# Design — S3 Model layer (Stage 1 · Skateboard)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 1 · Skateboard, second of four
**Depends on:** S1, S2.
**Translates:** [`docs/_archive/aeon-v1-horizontal/01-model-layer/`](../../_archive/aeon-v1-horizontal/01-model-layer/) — **M1–M18 keep their numbers**; §Inherited records what changed.

> Decision ids are prefixed **M**, continuing the superseded slice so its numbering survives. New decisions
> start at **M19**.

---

## Context

The slice where Aeon first does something a person would pay for: it talks to a model.

Its substance is inherited. `docs/_archive/aeon-v1-horizontal/01-model-layer` established **M1–M18** and a thirteen-row table of
places where the three provider families differ — the system prompt's location, whether tool arguments arrive
as a JSON string or a parsed object, where tool results go, whether `max_tokens` is required, and four
distinct shapes of streaming event. Every row is a place where passing the caller's messages straight to the
wire would have failed, and together they are the reason the canonical format exists.

**Two adapters, not one.** Anthropic and OpenAI both land here. The canonical format exists precisely because
pass-through breaks at the *second* provider, so validating it against one is the mistake the v1 plan made:
the format looks fine until something with a different shape arrives. One extra file removes the risk
entirely. The OpenAI-compatible path — capability flags, Ollama, LiteLLM — is S18.

**Nothing is stored.** The store is S4. `ask` streams, reports its cost and forgets. That is a real
constraint on the demo and it is stated there, because a cost you cannot look up later is a cost you have to
watch arrive.

Three of the superseded slice's decisions change in Go, and one of them is a simplification worth naming: the
bug class that justified an SSE parsing dependency in TypeScript is largely a JavaScript problem.

## Goal

`aeon ask "…"` streams a real reply from Anthropic or OpenAI over the protocol, and reports what it cost in
one of three honest states.

## Inherited decisions

| # | Status | Note |
| --- | --- | --- |
| M1 | **holds** | One canonical format; adapters translate both ways |
| M2 | **holds** | The format carries a `formatVersion`; transcripts outlive releases |
| M3 | **holds** | `content` is always a block slice, never a bare string |
| M4 | **holds** | Exactly four block kinds; unmapped provider content is dropped and **counted** |
| M5 | **holds** | `system` is a role in the canonical slice; the Anthropic adapter hoists it |
| M6 | **holds** | Streaming only. No second code path |
| M7 | **superseded by M19** | `eventsource-parser` was a JavaScript answer to a JavaScript problem |
| M8 | **holds** | Tool-call fragments reassemble inside the adapter; no partial `tool_use` escapes |
| M9 | **holds** | Every `Usage` field optional; **absent is not zero** |
| M10 | **holds** | Cost is computed here, with a three-state result |
| M11 | **holds** | Unknown provider fields dropped; the route's `extraBody` is the one escape hatch |
| M12 | **holds** | Provider chosen by the route; a route is data |
| M13 | **deferred** | The compatible path plus capability flags is **S18**. This slice ships two real adapters |
| M14 | **holds** | Transport retries only, and only before the first delta |
| M15 | **holds** | Cancellation reports the usage it saw |
| M16 | **holds** | `MaxOutputTokens` required — Anthropic rejects a call without it |
| M17 | **holds** | `thinking` round-trips verbatim, signature included |
| M18 | **holds** | The wire-level type is `ModelRequest`; `StepRequest` belongs to the runtime |

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| M19 | **SSE is parsed in-house**, with `bufio` and a byte-oriented reader. No dependency (supersedes M7) | The bug that justified `eventsource-parser` — a `TextDecoder`'s state across chunk boundaries — is a JavaScript problem. Working in `[]byte` and decoding once removes it. The format is blank-line-delimited `field: value`; a dependency for a hundred lines we must exhaustively test anyway buys nothing |
| M20 | The unions are **tagged structs with a `Kind`/`State` field**, plus constructors and `Validate()` — not interfaces | Go has no sum types. An interface-based union needs hand-written `MarshalJSON`/`UnmarshalJSON`, and transcripts are persisted and replayed, so JSON round-trip fidelity is the property that matters most. Constructors and a validator recover most of the safety at a fraction of the risk |
| M21 | `context.Context` carries cancellation and deadlines throughout | It is Go's answer to `AbortSignal`, and every downstream caller already has one |
| M22 | A tool's `Parameters` is `json.RawMessage` holding a JSON Schema. **This layer never inspects it** | The schema is data authored where the tool is (S6). Parsing it here would mean validating it here, which is a second owner for a contract that already has one |
| M23 | Streaming reaches clients through **S2's subscription machinery**, not a second mechanism | `ask` accepts a `subscriptionId` and deltas arrive as `event` notifications. One streaming path for the whole product; a bespoke channel for model output would need its own filtering, cursor and teardown |
| M24 | **Nothing is persisted in this slice** | The store is S4. `ask` streams and forgets, and the demo says so — a cost you cannot look up later is one you have to watch arrive |
| M25 | Prices live on the route as **USD per million tokens**, and the type says so | A unit ambiguity produces figures wrong by a factor of a million that look plausible either way (inherited K7's reasoning) |

## Architecture

```
aeon/internal/model/
  types.go        Message, Block, Usage, Cost, Route, ModelRequest, ToolSpec, Delta
  format.go       FormatVersion, ValidateTranscript, MigrateTranscript
  sse.go          the in-house SSE reader (M19)
  cost.go         price arithmetic, the three states
  client.go       Client: route → adapter, retry, cancel, accumulate, cost once
  adapters/
    openai.go     ToWire + FromWire
    anthropic.go  ToWire + FromWire
    registry.go   provider id → adapter
```

Imports nothing else under `internal/` except `protocol`'s error helpers. It is a leaf.

### The unions, and the trade M20 makes

```go
type BlockKind string
const (
    BlockText       BlockKind = "text"
    BlockThinking   BlockKind = "thinking"
    BlockToolUse    BlockKind = "tool_use"
    BlockToolResult BlockKind = "tool_result"
)

type Block struct {
    Kind BlockKind `json:"kind"`

    Text      string          `json:"text,omitempty"`       // text, thinking
    Signature string          `json:"signature,omitempty"`  // thinking — M17
    ID        string          `json:"id,omitempty"`         // tool_use
    Name      string          `json:"name,omitempty"`       // tool_use
    Input     json.RawMessage `json:"input,omitempty"`      // tool_use
    ToolUseID string          `json:"toolUseId,omitempty"`  // tool_result
    Content   string          `json:"content,omitempty"`    // tool_result
    IsError   bool            `json:"isError,omitempty"`    // tool_result
}

func TextBlock(s string) Block { … }
func (b Block) Validate() error { … }   // rejects fields foreign to the kind
```

**The honest trade:** this permits a `text` block carrying a `ToolUseID`, which the TypeScript union made
impossible. In exchange, JSON marshalling is the standard library's and a transcript round-trip has no
hand-written codec to get wrong — and a transcript round-trip is the thing S4 persists, S4 replays and S7
recovers from. `Validate()` is called at every boundary that accepts a `Block` from outside, so the invalid
state exists in theory and not in a stored transcript.

`Input` is `json.RawMessage` rather than `any`: it round-trips byte-identically, which matters because a
provider's tool arguments are replayed on resume.

Same shape for `Cost`:

```go
type CostState string   // "known" | "partial" | "unknown"

type Cost struct {
    State      CostState `json:"state"`
    USD        float64   `json:"usd,omitempty"`        // known
    USDAtLeast float64   `json:"usdAtLeast,omitempty"` // partial
    Missing    []string  `json:"missing,omitempty"`    // partial
    Reason     string    `json:"reason,omitempty"`     // unknown
}
```

**`omitempty` is deliberate on `USD` and wrong on `State`.** A zero cost that is genuinely known must still
serialise its state, which is why `State` has no `omitempty` — and it is exactly the place M9's "absent is not
zero" could be undone by a struct tag.

### The two providers, and where they differ

The superseded slice's thirteen-row table is inherited in full and remains the substance of the adapters. The
rows that bite hardest in Go:

| Concern | OpenAI | Anthropic |
| --- | --- | --- |
| System prompt | `{"role":"system"}` in the array | top-level `system` (M5) |
| Message content | plain string | typed block array |
| Tool arguments on the wire | `function.arguments` — a **JSON string** | `input` — a **parsed object** |
| Tool result | `{"role":"tool","tool_call_id":…}` | a `tool_result` block inside a **user** message |
| Stream events | `choices[0].delta` | `content_block_start` / `_delta` / `_stop`, `message_delta` |
| Fragment keying | by array **index**; the id arrives on the first fragment only | `content_block_start` carries the complete id, then `input_json_delta` |
| Usage | on the final chunk, needs `stream_options` | `message_start` **and** `message_delta` |
| Output cap | `max_completion_tokens` | `max_tokens`, **required** |

Four consequences, each getting a test:

**Tool arguments cross a type boundary.** Canonical `Input` is raw JSON. OpenAI's adapter unmarshals a string
on the way in and marshals to a string on the way out; Anthropic's passes the object through. Malformed JSON
from a model becomes a `tool_result` with `IsError: true` rather than an error — a model emitting bad
arguments is a normal event and must not kill the call.

**Multiple tool results merge into one user message for Anthropic.** N separate `tool`-role messages are
rejected. This is the row most easily missed, because it works with one tool call and fails with two.

**Anthropic constrains ordering** — no two consecutive assistant messages, never starting with one.
`ValidateTranscript` catches it before the wire, so the error names the index rather than surfacing a 400.

**`thinking` replays verbatim** (M17). Anthropic rejects a tool-use turn whose prior thinking was altered or
dropped, and because S7 recovers a step by replaying a stored transcript, a signature mangled here fails much
later and somewhere else.

### `ask`, and why it reuses subscriptions

```
→ subscribe {"filter":{"kinds":["ask.*"]}}        ← {"subscriptionId":"s-1","from":91}
→ ask {"route":"anthropic-sonnet","prompt":"…","subscriptionId":"s-1"}
                                                  ← {"askId":"a-7"}
← event {"seq":92,"kind":"ask.delta","askId":"a-7","text":"The repo has…"}
← event {"seq":93,"kind":"ask.delta","askId":"a-7","text":" three packages…"}
← event {"seq":94,"kind":"ask.done","askId":"a-7","usage":{…},"cost":{"state":"partial",…}}
```

The client subscribes first, then passes the subscription id (M23). That ordering makes a missed early delta
impossible without buffering, and S2's cursor already handles a reconnect mid-stream.

The alternative — a bespoke streaming channel for model output — would need its own filtering, its own cursor
and its own teardown, and would then need reconciling with subscriptions at S9 when the app wants both.

## The demo

See [`DEMO.md`](DEMO.md): `aeon ask` streams from both providers; cost prints in three states with a reason
when unknown; cancellation still reports what was spent; and a transcript containing a signed `thinking`
block round-trips byte-identically.

## Testing

**No unit test contacts a provider.** Adapters are tested against **recorded SSE fixtures**, captured once per
provider and committed with a manifest saying whether each was recorded or hand-written.

| What | How |
| --- | --- |
| Round-trip identity | canonical → wire → canonical is deep-equal, per provider, over all four block kinds |
| `thinking` fidelity | signature survives a round-trip **byte-identically** — asserted on the exact string |
| Fragment reassembly | OpenAI index-keyed deltas and Anthropic `input_json_delta` both yield one complete `tool_use` |
| No partial tool call | every yielded delta inspected; `tool_use` always complete |
| Offset independence | a fixture replayed chunked at every byte offset, including inside a multi-byte rune and inside a `data:` line, yields identical output |
| Malformed tool JSON | becomes `IsError`, does not fail the call |
| Anthropic merge | two tool results become exactly one user message |
| Anthropic ordering | consecutive assistant messages rejected, naming the index |
| Usage absent | `unknown` with a reason; a test asserts it is **not** `{State: known, USD: 0}` |
| Usage partial | `partial` with `Missing` naming the absent fields |
| Retry boundary | a 500 before the first delta retries; after it, the call fails and the delta was yielded once |
| Cancellation | `context` cancelled mid-stream resolves with the usage seen, and does not hang |
| Price units | a worked example proves USD per million |
| `Validate()` | a `text` block carrying a `ToolUseID` is rejected |
| Leaf boundary | `internal/model` imports nothing under `internal/` but `protocol`'s errors |

One **live probe**, recorded in the plan: does each provider return usage on a streaming request, and in what
shape? A "no" blocks nothing — cost reports `unknown` — but it decides whether the demo's cost line is
interesting.

## Out of scope

- **The OpenAI-compatible path and capability flags.** S18 (M13). Two real adapters here.
- **Persistence.** S4 (M24).
- **Prompt caching strategy.** The adapter reports cache tokens; deciding what to cache is the runtime's.
- **Embeddings.** S22, reusing this layer's transport and cost.
- **A model catalogue.** Routes are configuration; S23 supplies them. This slice consumes a route.
- **Token counting.** Compaction needs an estimate and S4's runtime owns it.
- **Gemini.** v1.1. M12 keeps it one new file.

## Consequences for later slices

| Slice | Consequence |
| --- | --- |
| S4 | Persists `Message` as canonical JSON, using `FormatVersion` and `ValidateTranscript` from here |
| S5 | Builds a `ModelRequest`; it never constructs a wire body |
| S7 | Recovery replays a stored transcript, so M17's fidelity is what makes it possible |
| S18 | Adds the compatible adapter plus capability flags; M12 is what keeps it one file |
| S22 | Embeddings reuse this transport and `Cost` |

## Open questions

None. The provider usage shapes are a probe in the plan rather than a design question, because both answers
are handled.

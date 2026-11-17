# Design — S18 Provider breadth (Stage 4 · Space shuttle)

**Date:** 2026-09-06
**Status:** approved (design); not yet implemented
**Stage:** 4 · Space shuttle, first of four
**Depends on:** S3 (the model layer), S6 (suspension), S7 (recovery). **Independent of S19, S22, S23.**
**Inherits:** M1–M25 from [`docs/_archive/aeon-v1-horizontal/01-model-layer`](../../_archive/aeon-v1-horizontal/01-model-layer/design.md) and
[`S3-model-layer`](../S3-model-layer/design.md). New decisions start at **M26**.

---

## Context

S3 shipped **two real adapters** — Anthropic and OpenAI — and deferred the rest here: *"the OpenAI-compatible path and
capability flags. S18 (M13)."*

M13 is the inherited claim this slice has to make good on:

> **The OpenAI-compatible path is the OpenAI adapter plus capability flags, not a third adapter.** *A near-duplicate file
> drifts; the differences are absences, not behaviours.*

That is a strong claim and it is **falsifiable**. Most differences between OpenAI proper and an OpenAI-compatible server
genuinely are absences — no streaming usage, no `parallel_tool_calls`, no `logprobs`, sometimes no tool support at all.
But some are behaviours: a server that returns a tool call with an empty `id`, or streams usage only in its final chunk.

So M31 makes M13 testable rather than assumed: **a difference that cannot be expressed as a capability flag is a finding
about M13**, recorded and raised — not a third adapter added quietly.

### The hard problem: what happens to a thinking block on the way out

The demo's claim is *"a loop suspended on Anthropic is resumed on OpenAI — the proof the canonical format earned its
cost."* The canonical format makes that look easy. One thing makes it genuinely hard.

M17 says `thinking` blocks round-trip **verbatim, signature included**, because *"Anthropic rejects a tool-use turn whose
prior thinking was altered or dropped"* (P21). OpenAI has no concept of a thinking block or its signature.

So sending the transcript to OpenAI means **dropping** those blocks — which M4 already permits, provided they are
**dropped and counted**. Fine. And resuming back on Anthropic works, because **the drop happened at the wire, not in the
record**: the store still holds them.

**That is the insight worth stating plainly.** Cross-provider resume works because adapters translate at the boundary and
the canonical transcript is untouched (M1). If a drop ever reached the store, the round trip would be one-way.

But there is a second-order problem the naive version misses. After a turn on OpenAI, the transcript holds an **Anthropic
thinking block followed by a turn Anthropic did not produce.** Replaying that to Anthropic sends a signature attached to a
history it never generated, and whether that is accepted is not Aeon's to guarantee.

So: **thinking blocks are replayed only to the provider that produced them** (M27). Which requires knowing who produced
what — hence M26.

### Provenance is worth more than this slice needs

Recording the provider and model on each assistant turn (M26) is what M27 needs. It also answers two questions the system
could not answer before:

- **"Which model wrote this?"** — for a transcript spanning providers, which is now an ordinary case rather than an exotic
  one.
- **Cost per provider**, which matters the moment a loop uses a metered model for planning and a local one for
  boilerplate.

### Capabilities: declared, or probed?

Both, and neither alone (M28).

| Approach alone | Fails because |
| --- | --- |
| **Declared** in the route | A route copied from a colleague declares capabilities of a server it no longer points at |
| **Probed** at runtime | A probe costs a request, takes time, and answers about *a* model rather than *this* one |

So the route **declares** and a probe **verifies**, and the three outcomes are the shape this system keeps arriving at:

| Capability state | Means |
| --- | --- |
| `verified` | declared, and a probe confirmed it |
| `declared` | declared, not yet probed |
| **`contradicted`** | declared, and the probe says otherwise |

**A contradiction is loud and never silently accommodated.** Quietly dropping `tools` because a probe found none is how a
loop runs three steps, discovers it cannot call a tool, and blames the model.

Which is [P31](../ARCHITECTURE.md#8-product-decisions) again — its **sixth** appearance. At this point the pattern is
load-bearing rather than observed.

### And the check belongs before the call (M29)

A step whose handler grants `bash` needs a model that can call tools. Discovering otherwise at the **first tool call**
wastes every step before it.

So a step's required capabilities are checked against its route **before the model is called**, in the spirit of S8's L9:
loud, and early enough to be actionable.

## Goal

Aeon running against a local model with no cloud key, and a loop suspended on Anthropic resumed on OpenAI — with every
capability difference expressed as a flag rather than a second code path.

## Decisions

M1–M25 are inherited. New:

| # | Decision | Why |
| --- | --- | --- |
| M26 | **Each assistant turn records the provider and model that produced it** | M27 needs it. And it answers "which model wrote this?" for a transcript spanning providers, plus cost per provider — both of which become ordinary questions once a loop can switch |
| M27 | **`thinking` blocks are replayed only to the provider that produced them**; otherwise dropped and counted (M4) | Replaying an Anthropic signature attached to a history Anthropic did not generate is a bet on someone else's validator. Dropping is defined behaviour; the alternative is hope |
| M28 | **Capabilities are declared on the route and verified by a probe.** Three states: `verified`, `declared`, `contradicted` — and a contradiction is **loud** | A declaration alone lies when a route is copied; a probe alone answers about the wrong model. A silent accommodation is how a loop blames a model for a route's mistake |
| M29 | **A step's required capabilities are checked against its route before the model is called** | Discovering at the first tool call that the model cannot call tools wastes every step before it |
| M30 | **An unmetered model's cost is `unknown` with a reason**, never `$0.00` and never a new state | Electricity and hardware are real; the honest statement is that Aeon does not know the price, which is exactly what `unknown` already means |
| M31 | **M13 holds: no third adapter.** A difference that cannot be a capability flag is a **finding about M13**, recorded and raised | It makes an inherited claim falsifiable. A quietly added third adapter would prove M13 wrong without anyone noticing |

## Architecture

```
aeon/internal/model/
  capability.go     flags, the three states, the probe (M28) — a separate
                    WireCapabilities field, never merged with the model-
                    management Capabilities vocabulary
  provenance.go     provider + model + route on each turn (M26, O4)
  cost.go           + unmetered → unknown with a reason (M30)

aeon/internal/model/adapters/
  openai.go         + capability gating — NO new adapter file (M13, M31)
  openai_responses.go   same gating; one provider id, two wire formats
  anthropic.go      + thinking-replay rule (M27)
  CAPABILITIES.md   what differs on OpenAI-compatible servers, and how

aeon/internal/runtime/
  precheck.go       required capabilities vs the route (M29)
```

### Capabilities

```go
type Capability string

const (
    CapTools          Capability = "tools"
    CapStreamingUsage Capability = "streaming-usage"
    CapThinking       Capability = "thinking"
    CapSystemRole     Capability = "system-role"
    CapParallelTools  Capability = "parallel-tools"
)

type CapabilityState string   // verified | declared | contradicted
```

Flags express **absences**, which is M13's whole argument. `CapStreamingUsage` absent means usage arrives at the end or
not at all, and the cost path already handles "not at all" — that is M9's every-field-optional shape doing its job three
slices later.

### The replay rule (M27)

```
canonical transcript                     what each adapter sends
  assistant [thinking(sig), tool_use]      Anthropic: thinking replayed verbatim (M17)
    produced_by: anthropic/claude-…        OpenAI:    thinking dropped, counted (M4)

  assistant [text]                         Anthropic: no thinking to replay
    produced_by: openai/gpt-…              OpenAI:    sent as-is
```

**The store is never modified.** A drop is a wire-level translation, which is what makes the round trip work in both
directions.

## The demo

See [`DEMO.md`](DEMO.md): a loop runs against **Ollama on a laptop with no cloud key**, and reports its cost as `unknown`
with a reason rather than `$0.00`; a loop **suspended on Anthropic is resumed on OpenAI** and then resumed **back on
Anthropic**, with the thinking blocks intact in the record throughout; a route declaring `tools` against a server without
them is **contradicted loudly** rather than accommodated; and a step needing tools is refused **before** the call rather
than at the first tool use.

## Testing

| What | How |
| --- | --- |
| **A→B→A round trip** | Suspend on Anthropic, resume on OpenAI, resume again on Anthropic. **The thinking blocks are still in the store** at every point, and the Anthropic replay is byte-identical to the original |
| Thinking is not replayed cross-provider | An OpenAI request built from a transcript with Anthropic thinking blocks contains **none of them**, and the drop is **counted** (M4, M27) |
| Provenance | Every assistant turn carries its provider and model; a transcript spanning two providers reports both, and cost splits by provider (M26) |
| The three capability states | `verified`, `declared`, `contradicted` — and a **contradiction fails loudly**, asserted as a refusal rather than a log line (M28) |
| A probe costs one request | And its result is cached per route, so it is not paid per step |
| Pre-call check | A step needing `tools` against a route without them is refused **before** the model is called — asserted with the client spy at **zero** (M29) |
| Unmetered cost | `unknown` **with a reason naming the route as unmetered**; `$0.00` appears nowhere (M30) |
| **No third adapter** | A file count and an import check: the OpenAI-compatible path adds **no new adapter file** (M13, M31) |
| M13 is falsifiable | A test listing every difference encountered and asserting each is expressible as a flag. **A difference that is not becomes a recorded finding**, and the test names it |
| Usage absent entirely | Cost is `unknown`, the step completes, and nothing reports zero — M9's shape, exercised for real |
| A tool call with an empty id | Handled or refused explicitly; a server that omits ids is common enough to be a case rather than a surprise |

The first row is the slice's claim, and its **third leg is what makes it a proof.** A→B shows translation works; A→B→A
shows the record survived, which is the property the canonical format was bought for.

## Out of scope

- **Gemini.** v1.1. M12 keeps it one adapter file plus a registry entry, and that is the point of M12.
- **A model catalogue.** Routes are configuration; **S23** supplies them. This slice consumes a route.
- **Prompt-caching strategy.** The adapter reports cache tokens; deciding what to cache is the runtime's.
- **Embeddings.** S22, reusing this layer's transport and cost.
- **Automatic provider failover.** A route is chosen deliberately. Silently retrying a failed provider on another one
  changes what produced the work without saying so — and M26 exists precisely because that matters.
- **Model quality comparison.** Aeon does not rank models.

## Amendments to earlier slices

| Where | Change | Status |
| --- | --- | --- |
| S3 | Its `Message` gains provenance (M26); its Anthropic adapter gains the replay rule (M27). Both are additive | T1, T5 |
| `../ARCHITECTURE.md` §8 P31 | Records capability state as the **sixth** instance | **landed with this design** |
| `internal/model/adapters/openai_responses.go` | Shipped after this design was written — a second wire format behind the one `openai` provider id (measured 400: `/v1/chat/completions` refuses function tools while reasoning is on). Recorded here as the **M13 precedent**: D10/I2 are re-worded from "two adapter files" to a **file-list pin**, and M13's claim reads against the **OpenAI-compatible** path — Ollama/vLLM add no adapter | **landed 2026-10-26 (amendment)** |
| `model.Route` capability vocabulary | The model-management wave shipped `Route.Capabilities` (UI vocabulary, migration 0026). S18's wire flags are the **separate** `WireCapabilities` field with its own closed set, validated in `capability.go` — the two vocabularies share the string `"thinking"` meaning different things and never merge | **settled 2026-10-26 (amendment)** |

## Open questions

None. Which capability flags exist is discovered by implementing against a real OpenAI-compatible server, and M31 makes
the discovery process explicit rather than open.

# OpenAI-compatible: what differs, and how

**M13's claim, kept falsifiable** (M31): the OpenAI-compatible path is the OpenAI adapter plus
capability flags, not a third adapter. Every difference found gets a row here — expressed as a
**flag**, or listed under **Findings** as something a flag cannot express. A difference handled by
an inline `if` that appears in neither place is a failed check
(`TestEveryDifferenceIsAFlagOrAFinding`).

Tested against:

- **Ollama 0.18.0**, model `qwen3.5:latest` (9.7B Q4_K_M), OpenAI-compatible endpoint
  `/v1/chat/completions`, measured 2026-10-26 on this machine. Captures distilled into
  `internal/model/testdata/openai/ollama-*.sse`, labelled `recorded` in the fixture manifest.
- **No second server has been run for this record.** The spec's second name will be written from a
  run that happened, not from memory; until then no claim is made about vLLM, LM Studio, or any
  other "OpenAI-compatible" server, and none should be inferred.

Both wire formats behind the one `openai` provider id (`openai.go` dispatches to
`openai_responses.go` on `route.openaiEndpoint`) share every rule below.

| Difference | Expressed as | Notes |
| --- | --- | --- |
| Usage arrives only when asked, on a final chunk with an EMPTY `choices` array | flag: streaming-usage | Undeclared (nil) still asks — absence of a declaration is not a declaration of absence (I5). Declared present: ask `stream_options.include_usage`. Declared absent: the key is not sent at all; measured, no usage then arrives anywhere, and cost is honestly `unknown` (M9) |
| Some servers 400 on a `tools` key they do not implement | flag: tools | Declared absent: the key is ABSENT, never `tools: []` — the same shape S3 pinned for the handoff retry. A step that needs tools never reaches this wire on such a route anyway (T4's pre-call check refuses it first) |
| Some servers 400 on a `system` role they do not model | flag: system-role | Declared absent: the system text folds into the first user message (leading part on the responses wire); the canonical transcript keeps its system turn (M1) |
| `parallel_tool_calls` is not sent by this adapter at all | flag: parallel-tools | Documented no-op today: the absence behaviour is the standing behaviour; the flag reserves the name so a future "send it when present" has its home. Measured: Ollama answered a tools request correctly without ever seeing the key |
| Thinking arrives as `delta.reasoning` (OpenAI's own chat API has no such field) | finding: delta-reasoning | See Findings — a field mapping, not a flag |
| Tool-call ids: absent or duplicated on some servers (Q11) | finding: tool-call-ids | See Findings — a defence, not a flag. Ollama 0.18.0 issued well-formed `call_…` ids on every measured call |
| `store: false` on the responses wire | finding: store-false | Not a server difference — a deliberate Aeon choice; documented here so its absence from the flag table is not read as a gap |

## Findings about M13

Everything in this section is something a capability flag **cannot** express. None of it required
a new adapter file, which is what M13 actually claims; if one ever does, this file says so and the
claim is revised, not quietly dropped.

### finding: delta-reasoning

Ollama 0.18.0 streams the model's thinking as `delta.reasoning` while `delta.content` stays empty
(measured: `qwen3.5:latest` burned 1,484 reasoning tokens producing "Hello there"). OpenAI's own
chat API has no such field. A flag cannot express "read this extra field" — reading it is code.
It is handled **inside the existing adapter**: `delta.reasoning` yields thinking deltas, which are
event and display material only, because T5 already guarantees nothing read here ever returns on
this wire. M13 stands: this is the one adapter absorbing a field, not a third adapter.

### finding: tool-call-ids

The Q11 answer, stated once for both wire formats: **handled, not refused**. An id the provider
never issued is synthesised as `aeon-synthesized-<n>` — and the synthesis is recorded *in the id
itself*, so a transcript read years later says which ids no provider ever vouched for. A duplicate
id gets `-aeon-2`, `-aeon-3`: the reassembler keys by index (S3's M8), so calls were never merged
in flight, but the transcript pairs tool results by id, and two calls must stay addressable as
two. No server measured here emits these shapes (Ollama issued well-formed ids every time); the
fixture is labelled hand-written and says so.

### finding: store-false

The responses wire sends `store: false` — the provider-side conversation copy Aeon already owns.
Not a compatibility difference; listed so the table is the complete account of what this adapter
sends differently from the endpoint's defaults. `ExtraBody` can override it.

## Cross-provider transcripts (S18 T8)

The round trip — a loop suspended on Anthropic, resumed on OpenAI, returned to
Anthropic — is the slice's claim, and the least-documented part of it is how a
provider reacts to a transcript **it did not produce**. What this task
established, and what it deliberately did not:

**Established, at the wire level** (`crossprovider_wire_test.go`, fixtures and
hand-built canonical turns — no live provider, I7):

- The OpenAI wires receive **no thinking at all** from a foreign transcript:
  neither an Anthropic-signed block nor any other, and every withheld block is
  counted into the step's record (M27, M4). The drop is a wire behaviour; the
  store keeps every block, byte-identical (pinned at the daemon:
  `TestStoreUnmodifiedAtEveryPoint`).
- The Anthropic wire replays **its own** signed block verbatim — signature
  bytes intact — even with foreign turns beside it in the same transcript, and
  withholds the foreign block (M17, O6: the rule keys on the producer).
- A mid-step provider change is refused before it can produce the one shape no
  adapter can build honestly — a `tool_use` from one provider's format with
  its `tool_result` attached in another's (O5, N9); the switch applies at the
  next step, named.

**Not verified, and not claimed:** how a live server reacts to a foreign
transcript. The measured server for this record (Ollama 0.18.0) is a
single-provider view; the round trip was exercised through the adapters and
recorded fixtures, never against two live endpoints (I7 keeps it that way in
tests). What a second server *would* say to an Anthropic-made transcript is an
open observation, recorded here as a gap rather than filled from memory (O7).

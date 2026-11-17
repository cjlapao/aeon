# S68 — the right body (how a loop starts, and how the person's words stay theirs)

## The ask, in the operator's words

"the propper way anthropic and openai do — there are fields for that… the
user input should be what i put, the system prompt should be the rest… hidden
from the chat window… the skills and other injections they go into system
prompt… you can find inspiration on helper/plai-client-agent and
helper/plai-client-app."

Web investigation confirmed the convention before any code: the industry
shape is a typed system prompt (instructions + policy + workspace), a typed
messages array with real roles, and capability bodies (skills) rendered as
system-side instruction with only their NAMES visible client-side.

## What this slice is (ruling A of the A/B/C decomposition)

**A — the body**: where the loop-start block (Task/Workspace/How to work/
Policy) and the chosen skills are RENDERED, and what the person's message
carries. The decomposition was the operator's ruling: this slice first,
attachments (B) second, agent↔agent conversation (C) third.

## What was ruled before the first line

- **Split at assembly** — the daemon keeps storing pieces; the split into
  system-half/transcript-half happens at render. Stored rows keep the old
  shape (B12: no transcript rewrites — history written before S68 is not
  rewritten by S68).
- **The user message is the operator's words ONLY** — verbatim, undecorated.
  The kickoff/goal text rides the instruction half, spoken as a frame, never
  as a `Goal:` line inside the person's bubble.
- **Policy renders as spoken sentences LAST in the system half** (deviation:
  the design first said a fixed position; the renderer puts policy last
  because every other section composes around the contract).
- **Chosen skills are a structured field end-to-end** — names on the wire
  (`skills`), names on the row, resolution to instruction lines only at
  send/drain, at the ONE daemon door. Replaces S67's text-weave (the chips
  stay; the weave moves into the daemon).
- **Real alternating messages** on the conversation thread (user/assistant),
  not a flattened `[author] body` blob.
- **Turn-scoped choosing** — `loop.start` skills attach to the whole launch
  (cached, reload-from-rows after restart); `loop.message`/`step.steer`
  skills attach to ONE turn.

## Non-goals

- Attachments as fields (ruling B, next slice).
- `step.Skill` agent-to-agent bodies (ruling C) — the skill-step branch keeps
  `dispatchSkill`; chosen lines deliberately do NOT attach to another body's
  system prompt (deviation 8, S70 territory).
- Re-writing pre-S68 transcripts (B12 forbids it).
- HomeScreen as a live `MessageComposer` mount (the quick-starter gained the
  picker and the `skills` field; the full composer mount stays a known gap).

## Contract the rest of the slice had to honour

I5 determinism (same input, same bytes), K13 frozen order within each half,
P31 three states everywhere (no skills = NULL, never an empty choice that
reads as a choice), CR6 source-blindness (the prompt never names where
instruction came from), M26 provenance is assistant-only, B12 stored rows
keep the old shape, D7 byte-identity when nothing is chosen.

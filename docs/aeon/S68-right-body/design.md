# Design — where each word of a turn lives

## The two halves (B1–B5)

`runtime.AssembleResult` gained `SystemText` and `TranscriptText`: the
sections each carry a `Destination` (system | transcript), `Assemble`
renders both halves deterministically, and `Compile` hands the model
`OpeningTranscript(history, systemText, transcriptText)` — system first,
history as real alternating messages merged by role, the person's block
LAST. The transcript renderer emits one trailing `\n`; `OpeningTranscript`
trims exactly one (pin: two newlines would mean the renderer changed).

- Task section: NO heading (the frame speaks it), Title `""` → transcript
  side carries only the human words.
- Workspace/How-to-work: system half.
- Policy: spoken sentences, rendered LAST in the system half, capped (32),
  ID-sorted. Deviations (1)/(3) in the task notes.
- `loop.ContinueFallbackText` = "Continue the loop per the task instructions
  in the system prompt." — the exact sentence pinned on BOTH sides (daemon
  test + UI shared-sentences test). A turn with no human words says this;
  the window never shows it (deviation 2: a pinned sentence, not a
  provenance column — M26 says the transcript does not carry system-side
  provenance anyway).

## The thread (B4, B5)

`loop.TurnParts(st, thread, newWordsID, window)` builds real alternating
messages: the row being answered is excluded (it rides as the LAST user
block), system/automation/blueprint-authored rows never ride, the window
trims SURVIVORS (last 40), `agent` maps to assistant and anything else to
user, and the frame says "You are in a conversation… The loop's goal:
<goal>". `mergeAdjacentHistory` collapses same-role neighbours so two
human rows never arrive as two adjacent user messages.

## Chosen skills (B6–B8) — the door doctrine

- **ONE sentence, both sides**: `use the <name> skill — read
  <path>/SKILL.md before answering.` The daemon's `chosenSkillLine` and the
  UI's `skillInstructionLine` are the shared fixture; each side's test pins
  the copy (B11).
- **Doors REFUSE, the offer only logs** (deviation 6): unknown names ⇒
  `ErrBadParams` naming EVERY offender; no catalog wired or a scan fault at
  a door ⇒ refusal (a choice that cannot be checked never reads as made);
  the S66 OFFER keeps its skip-and-log shape.
- **Unpickable ≠ unknown**: known-but-invalid/shadowed ⇒ SKIPPED and
  reported (steer result `skipped`; loop start/message ⇒ a `step.started`
  note via the existing Notes channel — the opening line, never the prompt).
- **Caps refuse, never truncate**: 60 entries, 32 KiB rendered.
- **Storage keeps NAMES** (`chat_message.skills`, `step_steer.skills` —
  `20261031121500_chat_message_skills.sql`, `20261031121600_step_steer_skills.sql`);
  resolution to lines happens at send/drain against the LIVE catalog
  (deviation 5). NULL is the third state everywhere (P31); a corrupt
  receipt is an error naming itself, never an empty choice.
- **B7, the ONE join point**: the steer wire message is composed at
  `steerWireText` — lines, blank line, PURE text. A resolver fault delivers
  the pure text (the instruction outranks its rider; the person's words are
  never lost). `steerWireText`'s shape is B6/B11's shared fixture with the
  drain test.
- **B8**: bubbles read rows alone — the posted event carries `skills`
  beside the pure body (additive; absent when nothing chose, byte-identical
  pre-S68 — D7), and the chat window renders the receipt UNDER the bubble
  with the sentence "shown to the model, not the thread". Hidden-by-design,
  never fabricated.
- **Per-launch semantics**: `loop.start` files the names on the kickoff row
  and caches lines+note; a restart (`restart` path) FORGETS (a new launch
  is a new choice); a replay after restart RELOADS from the kickoff row
  (first human row's names), resolving against the live catalog — and a
  resolve fault there softens to a spoken note + log, never killing the
  replay (deviation 7). A socket-start with an empty goal caches only
  (deviation 9, documented).
- **Replay vs live bytes**: `TestTheReplayMatchesTheLiveBytesForOneTurn`
  (runtime) and the conversation tests keep live and replay agreeing.

## What the wire gained (fixtures are the contract)

`loop.start.skills` + `loop.message.skills` + `step.steer.skills` + result
`step.steer.skipped` + `SteerView.skills` + posted-event `skills` — each
observed in a conformance fixture (`loop-start-skills.json`,
`loop-message-skills.json`, `steer-skills.json`, `loop-message-posted-skills.json`,
the amended `steers.json`) so protogen carries them into `contract.ts`.
The steers.json amendment re-pins CR10's digest with its earning task named
in `steer_fixtures_t4_test.go` — the way the guard itself asks for it.

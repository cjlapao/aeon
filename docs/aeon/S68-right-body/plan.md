# Plan — five tasks, in the order they ran

- **T1 — runtime**: sections gain a Destination; Assemble splits
  SystemText/TranscriptText; `OpeningTranscript` with real alternating
  messages + verbatim tail; policy spoken LAST; `CompileInput` carries
  History/TaskFrame/ChosenSkills; caps and refusals.
- **T2 — loop + protocol + plumbing**: `ModelRunner` gains `words, frame`;
  `TaskFrame` per blueprint position; `TurnParts`/`TurnWords` for the
  conversation; StepParams `History`/`TaskFrame`/`Skills`/`ChosenSkillsNote`
  carried-but-not-interpreted; the ~21 model-fake widens.
- **T3 — stores, doors, fixtures**: the two migrations, store round-trips
  (names, NULL, corrupt), the daemon's chosen-skills door (resolver,
  per-launch cache + reload), the three protocol doors (start/message/steer),
  `steerWireText`, the five conformance fixtures + protogen, and the test
  battery (13 daemon + 3 store + 5 protocol + 5 runtime).
- **T4 — ui**: the picker attaches a `skills` FIELD (S67's weave retired),
  receipt chips under the bubble from the posted event, HomeScreen
  quick-starter gains the picker + `loop.start` skills, bench echoes the
  honest posted shape, shared-sentence pins.
- **T5 — gates, the record**: full batteries per package, gofmt on touched
  files only, protogen/check/guard, this record + SLICES + cumulative demo,
  one commit.

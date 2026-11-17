# S68 demo — one turn, its two halves, and the receipt

Run from `aeon/` (the scripted wire, as the tests drive it — the same doors
the socket carries). Prereq: the walk uses the store/daemon seams directly,
no provider key.

1. **Start a launch that chooses**: `loop.start {blueprint: <gate yaml>,
   goal: "audit the deps", workspaceRoot, slug, skills: ["caveman"]}` —
   answer is the loop row; the thread's kickoff row is the GOAL'S OWN WORDS
   with `skills: ["caveman"]` beside them (read it back with
   `aeon` store reads or the tests' `Thread`).
2. **Refusal, visible**: the same call with `skills: ["ghost"]` returns
   ErrBadParams naming `ghost` — and `GetLoop(scope)` says NO loop exists.
   A refused choice leaves nothing behind.
3. **The turn's body**: dispatch `loop.message {text: "check the tests",
   skills: ["caveman"]}` and read the model call the fake recorded: the
   LAST message is exactly `check the tests` — the person's words alone
   (B3). The system half contains `## Chosen skills` and the canonical
   sentence `use the caveman skill — read <path>/SKILL.md before answering.`
   plus the spoken policy, the frame naming the goal, and no `[human]` blob
   anywhere (real alternating roles).
4. **Turn-scoped honesty**: a second `loop.message` WITHOUT skills carries
   NO caveman line on its wire; the loop keeps talking without inheriting
   a choice made for one turn.
5. **The steer's drain**: `step.steer {stepId, text: "Also mention the Go
   version.", skills: ["caveman","legacy-notes"]}` answers `{steerId,
   state: pending, skipped: ["legacy-notes"]}` — the row keeps PURE text +
   riding names; the wire message at the next boundary is lines, blank
   line, text (daemon test pins it; a catalog fault delivers the PURE text,
   never dropping the person's words).
6. **Restart honesty**: restart the daemon on the same store → the running
   loop's steps still render the chosen lines (reloaded from the kickoff
   row, catalog drift reported not fatal); `restart` FORGETS — the new
   launch dispatches with nothing unless the operator chooses again.
7. **The app**: open the loop workspace, type `/`, pick a skill — the chip
   previews the same sentence the model will read; send — the bubble shows
   ONLY the typed words with the skill's name as a chip UNDER it wearing
   "shown to the model, not the thread". The composer on Home offers the
   same picker beside the quick starter, and the names ride `loop.start`.

## What this demo does NOT show

- Real provider bytes (fixtures/fakes only). The Tauri shell. The full
  `MessageComposer` on Home (quick-starter picker only — recorded gap).
  Live-vs-replay is pinned by tests, not re-walked here by eye.

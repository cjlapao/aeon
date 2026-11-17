# S67 demo — what to see with your own eyes

Daemon up, at least two skills installed (Test bench → Agent Skills is
the quickest proof of what the catalog holds — the picker shows the
same catalog the page lists).

1. Open a loop's chat (the loop workspace). Click into the composer and
   type `/`. A picker opens above the input listing every discovery:
   valid and warn rows are clickable (warn rows say "(warnings)");
   invalid and shadowed rows are **visible but greyed**, each with the
   reason — "not selectable — the validator refused it", "…shadowed;
   the other root's copy is the one that would run". Keep typing:
   `/doc` filters; `/zzz` answers `No skill matches "zzz".` — it never
   shows an empty list as the answer to an unread catalog.
2. Click two rows. The picker stays open (multi-pick); chips appear
   above the textarea, one per choice. Hover a chip: the tooltip is the
   **exact sentence** the model will read. Click a chip's ×: it falls
   off both the row and the send.
3. Type your own words and hit **Steer** (or **Send** with no agent
   running). The chat shows your message with the skill lines woven
   ahead of your text — `use the docx skill — read
   /home/dev/.agents/skills/docx/SKILL.md before answering.` — and the
   chips are gone (they cleared with the send, not before it: kill the
   daemon mid-send and the chips survive).
4. The same picker from the other door: click **+** in the composer →
   **Skills and prompts** opens the identical panel.
5. Test bench → **Agent chat** — both modes carry it: Live mode's
   steer composer and the scenario composer behave exactly like step
   1–3 (scenario playback itself stays scripted).
6. Stop the daemon and type `/` again (or bench without one): the
   picker says "The catalog could not be read: …" — the honesty rule
   (P31), never silence, never an empty list wearing the empty catalog's
   words.

## What it does not show

The row inside the native Tauri window (everything here is jsdom — the
repo's UI posture), and the HOME screen: it does not mount the shared
composer yet, and its loop-start weaving waits for the `loop.start`
parameters slice. Both facts are recorded in the brief, not hidden.

Evidence: [tasks/](tasks/) T1–T3 Notes — measured: 9 new vitest tests;
full suite 798 pass / 12 fail / 1 skip with the 12 = main's pre-existing
set, same files; tsc 32, diffed empty against the stashed base.

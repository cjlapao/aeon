# S67 — skills in the chat: `/` lists the catalog, the chips ride the send

## The problem

S66 made the catalog reach the MODEL (every step is offered the valid
skills) and made the CHAT show the injection after the fact. What the
operator still could not do: LOOK at the catalog from where they are
typing. The `AgentChat`/`MessageComposer` stack already shipped the
doors for exactly this — `/` as a picker trigger, the plus menu's
"Skills and prompts" row (`ChatAddAction`), the host-controlled picker
panel — all documented in the component README — but **no host ever
spread them**, so typing `/` did nothing and the plus rows rendered
disabled. The doors existed; nobody hung the door.

## What this slice is

Wire the catalog to every composer surface, with the meaning living in
ONE place:

1. **`/` opens the skills picker** (and the plus menu's skills row
   opens the same one). Rows: every discovery — valid and warn
   selectable; invalid and shadowed LISTED but not selectable, each
   saying why (P31 — a rendering, never a silent drop). Filtered by the
   trigger query; an unread catalog says it could not be read, never
   "empty".
2. **Chips** above the composer input: chosen skills, each removable,
   tooltip carrying the exact sentence the model will receive.
3. **The weave**: on send, one line per chosen skill —
   `use the <name> skill — read <path>/SKILL.md before answering.` —
   then the operator's own words. The chips clear only when the send
   resolved; a failed send keeps them (losing typed text to a socket
   error is not acceptable).

## Rulings actually run

- **The weaving rule lives in the hook, not in any host.** The first
  draft had each host wrap `onSteer` by hand — the test caught the
  drift it invites (the test's own host harness forgot the wrap, which
  is exactly how a real host would). Now hosts spread
  `skills.steerWith(send)` / `skills.messageWith(send)`; the sentence,
  the order, and the clear-on-success are one definition.
- **Chips are composer BASE furniture** (`MessageComposer.chips`,
  rendered in both variants), not per-host markup — the home-screen
  composer inherits the row the moment it appears. Labels/meaning
  remain the host's; the base only renders and hands back the remove.
- **The sentence mirrors S66's offer** (`read <path>/SKILL.md`) so what
  the operator CHOSE and what the catalog OFFERED sound like the same
  family — because they are the same catalog.
- **Picker stays open on select** (multi-pick; chips are the state) —
  the README example closes on select; chips change that math, so the
  host decides by keeping the panel.
- **Home-screen weaving is deferred**: the home composer does not mount
  `MessageComposer` yet (the quick-starter is still a loop-start form),
  and the next slice refactors `loop.start` to take parameters —
  weaving goal text before that would be weaving into an API about to
  change. The base chips row means that slice spreads, not builds.

## In scope / out

In: `MessageComposer.chips` (+ `ComposerChip`, exported);
`useSkillPicker` (+ `skillWeaveLine`, `skillIsPickable`) in
`lib/skills.ts`; wiring in `LoopWorkspaceView` (the real chat) and both
`AgentChatPage` modes (bench scenario + live).

Out (named): home-screen mount + goal weaving (next slice, with
`loop.start` parameters); commands/loops/agents picker rows (the other
`ChatAddAction`s — doors stay open, unhosted, until their data exists);
any daemon-side change (this is a pure UI slice — the socket already
carries everything).

# S67 design — one hook, one chip row, every composer

## The layering

```
MessageComposer (base, shared with the future home composer)
  chips?: ComposerChip[]        ← renders the row; owns no meaning
ChatComposer  = chat controls over MessageComposer (chips flow through)
AgentChat     = stream + panels + picker slot + composer
Hosts (LoopWorkspaceView, AgentChatPage ×2):
  const skills = useSkillPicker(connected);
  <AgentChat picker={skills.panel ?? undefined} onPickerDismiss={skills.close}
    composer={{ onSteer: skills.steerWith(send), onSpecialKeyChange:
      skills.onSpecialKeyChange, onAddAction: skills.onAddAction,
      chips: skills.chips, … }} />
```

`useSkillPicker` lives in `lib/skills.ts` — the catalog's home — and
imports NO component (its panel/chip shapes are structural twins of
`ChatPanel`/`ChatPanelItem`/`ComposerChip`; the dependency arrow points
components → lib, never back).

## The picker (`panel`)

- Opens only on the `/` trigger (`onSpecialKeyChange` closes on any
  other request, including null — a replaced token means moved on) and
  on `onAddAction("skills")` with an empty filter.
- Rows from `useSkills` (which already re-reads on `skills.changed`,
  S66 — the picker is live while it is open).
- `query` filters name+description (case-insensitive contains).
- Selectability line = S66's offer line: `skillIsPickable` = (valid OR
  warn) AND not shadowed. Invalid and shadowed render DISABLED rows
  whose description states the reason in the catalog's words ("not
  selectable — the validator refused it" / "…shadowed; the other root's
  copy is the one that would run").
- `emptyText` has three states: unread-with-error ⇒ "The catalog could
  not be read: <daemon's sentence>"; zero discoveries ⇒ "No skills
  installed — drop a folder into either skills root…"; filtered-away ⇒
  `No skill matches "<query>"`. Unknown is never rendered as empty.

## The chips

`chips` derives from `selected ∩ live catalog ∩ pickable` — so a skill
removed or revalidated-to-invalid between picking and sending simply
stops riding (both the chip and the weave line), with no toast and no
lie: the row shows what WILL ride, computed at render.

Selection order (not catalog order) decides line order — the operator's
emphasis is theirs.

## The weave (one definition)

```
use the <name> skill — read <path>/SKILL.md before answering.
<one line per chosen skill, selection order>
<blank line>
<operator's own text>
```

`skillWeaveLine(name, path)` is exported — the same sentence appears in
the chip tooltip (what the operator sees IS what the model gets), and
its phrasing is the S66 offer's phrasing (`read <path>/SKILL.md`), so
chosen and offered read as one family.

`steerWith(send)` / `messageWith(send)` wrap the host's send: weave →
await the real send → `clear()` only on success (a failed send keeps
its chips — nothing the operator chose dies to a socket error). If
nothing was chosen the text passes byte-identical (the no-choose path
is pinned by test).

## Deviations from the plan

- No `loop.start`-side weaving (home defers to the parameterization
  slice, per the ruling above) and no home mount — the base renders
  chips for both variants TODAY, so the future home gets it for free.
- The README's example closes the picker on select; we keep it open for
  multi-pick (chips are the selection state). Recorded, not silent.

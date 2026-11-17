# Demo — S29 Triage

**Predictive.** Written before the slice is built, per `AGENTS.md` §4a. T9 walks this for real — steps 1–6
fully; steps 7–8 (the S28/S26 hooks) only if those slices' real code has landed by then.

## Fixture

A project with no prior bugs filed, `aeon serve` on a throwaway `XDG_STATE_HOME`.

## Steps

**1. `aeon triage file --title "crash on empty input" --body "..."`.** **Predicted**: the bug appears
`unvalidated`, `unknown` priority — and a validation-attempt loop starts automatically, visible in the
ordinary loop list, with no separate trigger call (T2's own property).

**2. The validation loop completes, reproducing the bug.** **Predicted**: `aeon triage show <id>` reads
`validation_state: reproduced` — `unvalidated` and `could-not-tell` are never the same thing, demonstrated
by filing a second bug whose attempt is genuinely inconclusive and comparing the two side by side.

**3. `aeon triage set-priority <id> --state known --label P1`.** **Predicted**: succeeds independent of
validation state — settable even on an `unvalidated` bug, demonstrated by setting priority on a bug before
its attempt finishes.

**4. File a near-duplicate of the first bug, then `aeon triage suggest-duplicates <id>`.** **Predicted**:
the first bug appears as a candidate — and nothing about the bug's own state changed from the suggestion
alone (D5 made visible: a second `show` before confirming shows no `duplicate_of`).

**5. `aeon triage confirm-duplicate <id> --of <first-id>`.** **Predicted**: now `duplicate_of` is set — the
one and only write path, exercised explicitly.

**6. Call `file_bug` from inside a running agent's step (an agent self-filing, D7).** **Predicted**: the
resulting bug is `unvalidated`, `unknown` priority — identical shape to the human-filed one in step 1, never
pre-labeled by the filing agent.

**7. [If `S28-memory`'s learnings core has landed] Check `aeon learnings list --scope project` after step
2's attempt completes.** **Predicted**: one new learning, `provenance: observed`, summarizing what the
attempt found.

**8. [If `S26-roadmap`'s core has landed] After steps 2 and 3 (validated and prioritized), check `aeon
roadmap list`.** **Predicted**: one new item, status `idea`, `provenance: triaged` — and shortly after,
`brief-agreed` once its (fast, transcription-only) ideation completes.

## What this demo does not show

**Steps 7–8 are not walkable until `S28-memory` and `S26-roadmap` have real code**, respectively — if this
walk happens before either lands, the walked version should say so plainly rather than omit the steps
silently. **The UI half** is untested if this checkout's Tauri libraries are unavailable, the standing
headless-blocked note. **Duplicate-detection quality** — step 4 demonstrates the *mechanism* (suggest, never
auto-apply), not whether the embedding actually finds good matches, which is a model/provider question
outside this slice's own claims.

# Demo — S26 Roadmap

**Predictive.** Written before the slice is built, per `AGENTS.md` §4a. T7 walks this for real.

## Fixture

Two projects: one with an active S24 queue (so R4's queue-preferred branch is exercised), one without (so
the direct-`loop.start` fallback is exercised too — both, not just one). `aeon serve` on a throwaway
`XDG_STATE_HOME`.

## Steps

**1. `aeon roadmap add --title "dark mode" --epic ui --priority unknown`.** **Predicted**: the item appears
as `idea`, priority renders as the literal word "unknown" in `list` — never a blank, never sorted as if it
were low.

**2. Start ideation on it.** **Predicted**: the item moves to `ideating`; an ordinary loop starts (visible
in the existing loop list, nothing new), using S9's standard per-loop thread (U18) for the conversation —
no new chat surface.

**3. The ideation loop's step declares `produces: [brief.md]` and reaches `done`.** **Predicted**: the item
moves to `brief-agreed` automatically, detected through the existing `loop.finished`/artifact-check path
(`design.md` R3) — no new mechanism visibly fired.

**4. Planning dispatches automatically.** **Predicted**: a `project-planner` step runs (S13's real handler,
not a stand-in), and on success the item reaches `planned`, with `plan_path` populated.

**5. Set the item's priority to `known` / `high` and attempt `ready`.** **Predicted**: `IsReady` re-checks
all three conditions at the transition attempt (not from whenever each became true) and the item reaches
`ready`.

**6. Materialize it on the project *with* a queue.** **Predicted**: the item is appended to S24's queue
(visible in `aeon queue list`), not started directly — `aeon roadmap show <id>` reads `running` only once
S24 actually starts it, honestly reflecting that the item and the loop are separate records (D6).

**7. Materialize an equivalent item on the project *without* a queue.** **Predicted**: `loop.start` is
called directly; the item reads `running` immediately, with a `loop_slug` populated right away — the
fallback branch, genuinely exercised, not merely argued to exist.

**8. Cancel a `brief-agreed` item on the first project.** **Predicted**: it reads `cancelled`, still
listed (never deleted), rendered distinctly from `done` — D7 made visible.

## What this demo does not show

**Whether the ideation step in this walk used a real model or a replayed fixture** — the walked version
should say which, plainly. **The UI half** is untested if this checkout's Tauri libraries are unavailable,
the standing headless-blocked note. **A genuinely long-running ideation loop** (many back-and-forth turns
before the person is satisfied) — this fixture's ideation is short by construction, and the walk should not
imply it tested patience at scale.

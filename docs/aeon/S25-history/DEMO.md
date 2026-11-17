# Demo — S25 Project History

**Predictive.** Written before the slice is built, per `AGENTS.md` §4a. T6 walks this for real.

## Fixture

A project with three loops: one currently `running`, one `done` with an `unknown`-cost step among its
others, and one **archived** (so D7's merge is genuinely exercised). **The archived one is created by
calling `Store.ArchiveLoop` directly in the walk's setup** — there is no CLI verb for this yet (`design.md`'s
Q4: wiring one is out of this slice's scope — S17 was the original guess for who'd own it, checked and
built without adding it), so the walk says so rather than implying `aeon loop archive` exists. `aeon serve`
on a throwaway `XDG_STATE_HOME`.

## Steps

**1. `aeon history list --project-root <p>`.** **Predicted**: all three loops appear, time-ordered, the
archived one visually distinguishable (a `[archived]` tag or equivalent) and placed correctly relative to
the other two by its original timestamps — not pinned to the end just because it was archived last.

**2. `aeon history list --group-by agent`.** **Predicted**: the same three loops' steps regrouped by agent,
no new query shape — confirms H1's "existing columns only" claim in practice, not just in the schema.

**3. Drill into the `done` loop's `unknown`-cost step.** **Predicted**: the rollup for that loop reads "at
least $X (1 of N steps unknown)" — never a bare figure, D3/H6 made visible.

**4. Drill into the archived loop.** **Predicted**: the list view's entry for it triggered zero
`loop_archive_file` reads (Q3); opening it specifically triggers exactly one, and its transcript renders
the same way the live loop's did in step 3.

**5. `aeon history export --group-by loop` into a file, diff its rows against step 1's listing.**
**Predicted**: identical content modulo NDJSON formatting — H3's parity claim, proven by actually running
both and comparing, not merely argued.

**6. Force one loop to `blocked`** (any reason) **and re-list.** **Predicted**: the entry shows the stored
reason text verbatim — whatever `LastReason` actually says — never a generic "failed."

## What this demo does not show

**A genuinely large fixture** (months of loops) to validate Q1's "measure before indexing" call for real —
this demo's three-loop fixture is too small to produce a meaningful query plan either way; the walk should
say so plainly rather than claim performance it did not measure. **The UI half** is untested if this
checkout's Tauri libraries are unavailable, the standing headless-blocked note.

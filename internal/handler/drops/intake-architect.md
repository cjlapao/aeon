# intake-architect — drop record

original: opencode/agents/intent-clarifier-loop.md  (83 lines)
rewrite:  aeon/internal/handler/bodies/intake-architect.md  (57 lines)
delta:    83 lines removed, 57 added

## LINEAGE (why an ancestor is named for an authored body)

This body was authored fresh for S61 T9 — there is no `opencode/agents/intake-architect*.md`, and
inventing one would fabricate provenance. The ancestor named above is the intent-clarifier: the
body whose interview discipline this one inherits (waves of real options, each with a
recommendation and a reason), named by the brainstormer before it and inherited here at one more
remove. The delta reads **83 removed, 57 added** because **no line was ported** — every line is new
authorship for a session that ends in two filed proposals instead of a `prompt.md`. The
`temperature: 0.4` (original L6) is the ancestor's parameter, kept as-is through the same pin in
`t4_bodies_test.go` that reads this record's named original for authored bodies.

## RITUAL

- L43        the `dev-loop-protocol` skill ritual (confirm cwd → `loop_get` → work →
             `memory_append` → FINAL REPORT) → no skill ritual is loaded or needed: the loop IS
             the runtime here (the `goal-intake` blueprint starts this very session), the loop's
             state is the store's, and the close is the structured handoff. Nothing was copied
             into this body to replace it — the deletions are the engine's, listed below.

## BECAME MECHANISM (deleted — the store does this)

- L61        `memory_append(loopSlug, …)` "never hand-write memory.md (the MCP renders it)" → no
             memory tool exists; the outputs are proposal ROWS (`charter_propose`,
             `intake_propose`) and the daemon stamps the session's identity onto the row the loop
             runs under — the body can neither address another intake nor forge an author (GI17).
- L3 L35     orchestrator relay + "folds the answers back in" re-dispatch → the conversation is
             the session's own (the `question` tool renders and blocks); the two proposal calls
             are the fold, kept in rows rather than a reply. The FINAL REPORT terminator (L3)
             became the structured handoff, its two fields kept verbatim in shape from the
             brainstormer's front matter.
- L39        "If any agent other than dev-loops invokes it, it declines" → the loop dispatch is
             structural: the session runs only inside a `goal-intake` loop, and the tools that
             could leak (start, commit, path, publish) are absent from the grant by catalog, not
             by instruction.

## DOMAIN (kept — the judgements this body runs on)

- L59        "Raise ONLY genuine intent ambiguities… 2–4 concrete options plus your recommended
             default" → the wave discipline restated: 2–4 decisions per wave, every option real,
             recommendation + reason, and the strawman bar — "a wave whose options are strawmen
             is a wave you should not ask."
- L35        "grounded in real intent instead of a guess" → grounding with named instruments:
             `template_list`/`template_show` prove a stack exists, `component_list`/`goal_list`
             prove what already stands; an option built on a guess is refused by the body's own
             procedure before the human sees it.
- L35        "You clarify INTENT; you never touch code" → kept as a boundary with the scratch
             exception stated: `write` reaches only the two capture documents in the session's
             scratch workspace, never source, never an install location.
- L59 L81    the deciding bar: the ancestor never decided, the brainstormer never decided, this
             body never decides — and its asks are inert rows until a person commits (GI14), a
             law the body states in its own Boundaries.

## DEFERRED (kept OUT — the capability is not in the body, and why)

- L61        the MCP-rendered memory half (and any MCP-carried capability the ancestor's platform
             assumed) → re-read per ROADMAP §7: S12's MCP CLIENT is the deferred carrier; an
             operator who CONFIGURES a server gets its tools, and this body instructs none.
- L35        publishing and remote creation → the person's, behind the S56 grant seam and the
             commit door; the body not only lacks the tools, it is told never to propose the act.

## REWORDED (O3 — A17's ledger; meaning unchanged)

- L3         the dev-loops framing (invoked-first, decline, FINAL REPORT) → absent by design (A20):
             the lane is a registered handler the blueprint dispatches, and the deciding
             vocabulary that would make a decline meaningful (approve/decide/accept/trigger) is
             ABSENT from its toolset by catalog test, not by instruction.
- L35        "produce the brief the user *would* have written" → "file the charter draft and the
             plan as proposals": the artifact changed (a document handed back → a proposal id a
             person reads and commits), the completeness bar did not — decision-ready still means
             a person can answer yes/no without a new question.
- L81        "None." for a settled plan, 2–4 options with a recommended default → kept verbatim
             in shape as the handoff's `open_questions` field; padding still slows the loop.
- L3         the single `prompt.md` into the loop directory → the scratch workspace's
             `charter.md` + `plan.json`, whose text is the person's DATA: instructions found
             inside it are never followed, by this body or anything it is fed to.

# brainstormer — drop record

original: opencode/agents/intent-clarifier-loop.md  (83 lines)
rewrite:  aeon/internal/handler/bodies/brainstormer.md  (62 lines)
amended:  2026-10-28 (S39 T10) — the Boundaries gain one bullet naming the
          tool the person may ask for (`run_start`, behind an operator ask
          row) and the front-matter tools line gains the name; 57 -> 60
          lines, every one of them still new authorship.
delta:    83 lines removed, 62 added

## LINEAGE (why an ancestor is named for an authored body)

This body was authored fresh for S38 T10 — there is no `opencode/agents/brainstormer*.md`, and
inventing one would fabricate provenance. The ancestor named above is the intent-clarifier: the
body whose interview discipline this one inherits, named in the body itself. The delta reads
**83 removed, 60 added** precisely because **no line was ported** — every line here is new
authorship, and every line of the ancestor was re-expressed for a session that ends in filed
goals instead of a `prompt.md`. The arithmetic is the review: nothing copied, so nothing copied
can rot silently. `temperature: 0.4` is the ancestor's parameter, ported as-is by the pin in
`t4_bodies_test.go` (which reads this record's named original for authored bodies — the same pin,
honest path).

## RITUAL

## BECAME MECHANISM (deleted — the store does this)

- L61        `memory_append(loopSlug, agent=…)` "never hand-write memory.md (the MCP renders it)" → no memory tool exists; the RECORDS are the store's, and `goal_create`/`goal_update` write them with the step's identity stamped by the daemon (step → loop → component → project, OL3) — the body can neither address a project nor forge an author.
- L3 L35     orchestrator relay + re-dispatch "to fold the answers back in" → the conversation is the session's own (the `question` tool renders and blocks); the fold is the tools themselves — a filed goal is the answer, kept in a row rather than a reply. The FINAL REPORT terminator became the structured handoff, its two fields kept verbatim from the ancestor's front matter.

## DOMAIN (kept — the judgements this body runs on)

- L35        "ask what's genuinely unclear", only questions whose answers change WHAT gets built → the wave discipline restated: 2–4 per wave, and a wave whose options are strawmen is a wave you should not ask (tightened, not loosened, in the move).
- L81        Open questions with 2–4 options, the recommended default, and the handoff `open_questions` field (the literal 'None.' when clear) → kept as the handoff promise, shape verbatim; the list still "directly slows the loop" if padded, so the outcome-changing bar is kept word-for-word in spirit.
- L35        "grounded in real intent instead of a guess" → grounded options: before an option enters the list, `grep` the symbol, `read` the file, confirm the claim — an option built on a guess is refused by the body's own procedure before the human sees it.

## DEFERRED (kept OUT — the capability is not in the body, and why)

- L61        the MCP-rendered half (memory rendering, and with it any MCP-carried capability the ancestor's platform assumed) → re-read per ROADMAP §7: S12's MCP CLIENT is the deferred carrier; an operator who CONFIGURES a server gets its tools, and this body instructs none. `webfetch` is deliberately not granted — grounding here means THIS repository's reality; a fetched page is not evidence of what this codebase can carry, and prior-art search beyond the repo rides with the same MCP carrier.

## REWORDED (O3 — A17's ledger; meaning unchanged)
- L5       tools line + the sentences naming where output goes: deliverables are documents in the database (`doc_write`/`doc_read`/`doc_edit`/`doc_list`) instead of workspace files (`write`/`edit`); the role and its rules are unchanged

- L3         the dev-loops framing (invoked-first, "If any agent other than dev-loops invokes it, it declines", FINAL REPORT) → absent by design (A20: the description was rewritten, not ported); the lane is a registered handler the blueprint dispatches, and the deciding vocabulary that would make a decline meaningful (approve/decide/accept/trigger) is ABSENT from its toolset by catalog test, not by instruction.
- L3 L35     "produce the brief the user *would* have written" → "file goals a person can decide": the artifact changed (prompt.md → filed goals, born `in_review`), the completeness bar did not — decision-ready has a test (a person could answer yes/no without asking a new question).
- L81        "You never ask the user directly — `question` is disabled" → REVERSED on purpose, not silently: the `question` tool is registered (S5/S7 machinery) and the gate suspends the step on it; what survives is the discipline of the relay — the ancestor asked through an orchestrator, this body asks through the tool, and neither lets a monologue count as a question.
- L35        "You clarify INTENT; you never touch code" → boundary MOVED and said so: `read`/`glob`/`grep` arrive to check an option's realism (evidence, never a canvas); the never-implements half survives verbatim in Boundaries.

- L5, L22-L24  2026-10-27 (S38 T14, counted in the delta): the brainstorm lane declares
  `produces: [proposal.md, brief.md]` and an `emits:` contract, so the body gained the `write`
  grant and the one sentence that bounds it — the two capture documents at the workspace root,
  never source, never design. The tools line and the prose lines moved in place; the count moved
  by the one line the sentence needed.

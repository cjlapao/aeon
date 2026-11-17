# chat — drop record

original: opencode/agents/project-planner-loop.md  (183 lines)
rewrite:  aeon/internal/handler/bodies/chat.md  (63 lines)
delta:    183 lines removed, 63 added
amended:  2026-10-28 (S39 T10) — the body gains its one paragraph on calling
          `run_start` only when asked, and the front-matter gains the tool;
          56 -> 63 added, the removed side unmoved: still no ported line.

## LINEAGE (why an ancestor is named for an authored body)

This body was authored fresh for S39 T6 — there is no `opencode/agents/chat*.md`, and inventing
one would fabricate provenance. The ancestor named above is the project-planner-loop: the body
whose vocabulary this one carries in ORDER TO INVERT it. The planner is the purest "structure
before speech" agent in the corpus — it plans, never implements, and reports to an orchestrator
(L27, L33). `chat` is what a loop's voice looks like when none of that is asked for: it does the
work directly, it reports to the PERSON through the thread, and the structured process it knows
about it PROPOSES (`run_start`) instead of running. The temperature — 0.3, the planner's (L6) —
is ported as-is by the pin in `t4_bodies_test.go`: a conversation, like a plan, is not the place
for temperature; precision is. The delta reads **183 removed, 63 added** (the 63rd line and its
seven companions arrived with S39 T10's `run_start` paragraph and tool) precisely because **no
line was ported** — every line here is new authorship.

## RITUAL

## BECAME MECHANISM (deleted — the store/engine does this)

- L33        caller guard "Invoked only by dev-loops… decline… status=blocked" → the turn's caller IS the protocol: `loop.message` gates at the daemon (idle loop, no running step, one turn in flight — conversation.go), and a step that arrived is authorised by construction. A body that second-guessed its inviter would be a second gate with a worse view.
- L37        "Follow the `dev-loop-protocol` skill — load it first" → nothing loads skills here (A12); the plan schema, the loop dir and the task-id mechanics were the ritual, and a conversation has no schema to load. What remains of procedure is in the body or nowhere.
- L84-L85    `event_append(...)` checkpoint + "emit the FINAL REPORT line" → the thread IS the timeline: `Note` + `publishMessage` land the turn's reply as a row with an author and a source (CR6), and the structured `handoff` (summary/open_questions) replaced the terminator line. A report format for an orchestrator to parse has no reader in a conversation.

## DOMAIN (kept — the judgements this body runs on)

- L27        "You plan; you never implement" → INVERTED, deliberately and stated: the chat agent's first rule is "do the work directly" — `read`, `write`, `edit` in the turn that was asked. The inversion is the design (CR3: a turn is an ordinary step; the step's whole point is that it can act); what the ancestor kept — never touch the orchestrator's bookkeeping — survives as the store's own writes (rows carry the daemon-stamped identity; the body can forge neither).
- L72        "note it in an 'Unsupported' section… do not invent an engineer" → kept as "say plainly when something is missing… a guess is said to be a guess": the honest-unknown discipline survives the inversion intact, only its audience changed from a report section to the person.
- L29        "the detail files are the point… the engineer executes almost mechanically" → kept as grounding before speaking: `glob`/`grep` before speculation — an answer about this component is grounded in its files or it is not an answer. The ancestor spent context so the next agent needn't; chat spends it so the ANSWER needs no re-derivation.

## DEFERRED (kept OUT — the capability is not in the body, and why)

- L15        `codesearch: true` (and `webfetch` L18) → the MCP client is the future carrier of search-shaped tools (A18), the same deferral every record names; when it arrives the body gains the tool, not a new mechanism.
- L17        `todowrite: true` (the ancestor's plan checklist) → `todo` is kept as the ask-tracker, but the per-task spec machinery is deferred to the blueprints (`run_start`, T10) — a conversation proposes the process; it does not grow one.

## REWORDED (A17 — clarity edits on kept intent, line-referenced against the original)

- L3         the description's orchestrator-facing clauses ("dev-loops-ONLY", "FINAL REPORT", the decline sentence) → the description names the PERSON's conversation instead; same honesty rule (what it does, what it refuses), different audience — no ported line, since the audience changed every sentence's shape.

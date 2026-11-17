# intent-clarifier — drop record

original: opencode/agents/intent-clarifier-loop.md  (83 lines)
rewrite:  aeon/internal/handler/bodies/intent-clarifier.md  (49 lines)
delta:    34 lines removed, 0 added

## RITUAL (deleted — the engine does this)
- L37-L39  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L41-L43  "Follow the dev-loop-protocol skill" + ritual    → nothing loads skills here; the prompt.md schema it carried is written into this body (A5 — this handler owns prompt.md)
- L56      step 1: loop_get lessons read                    → the step's request carries the goal; a fresh loop's emptiness needs no instruction once there is no state to fetch
- L61      step 6: `memory_append` lesson                   → there is no memory tool; lessons are an operator file, not agent state
- L62      step 7: `event_append` return checkpoint         → the daemon emits step events; a body cannot write its own timeline row
- L63      step 8: FINAL REPORT line (half)                 → the handoff is structured; whether Open questions remain survives as the open_questions field (REWORDED)
- L80      "file lane is prompt.md and scratch/ inside the loop dir" (half) → the lane (you produce exactly one document) survives in Boundaries; the loop-dir address goes with produces:

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map (grep/codesearch/webfetch/question false) → allow-list `[read, write, edit, todo]`: the DISABLEMENTS were the point and they survive as absences — no search, no web, no direct asking (the orchestrator-relay design); task DROPPED (A1), todowrite→todo (A19)
- L22-L33  permission.bash deny table (git/rm rules)        → the corpus carries these for this agent; with bash absent the rows guard a door never opened
- L52      mode B's "append a memory note, and return"      → the handoff is the note

## DOMAIN (kept)
- L35      the role verbatim in substance: FIRST agent, BEFORE the investigator; the brief the user WOULD have written; "You clarify INTENT; you never touch code"; surface questions that change WHAT gets built, orchestrator relays, answers folded back → role line
- L45      the no-code lane with its stop-you sentence ("If you find yourself wanting to open a source file, stop — that is not your lane") and the division of labour → kept, re-expressed as tool absences
- L47-L52  the two dispatch modes verbatim in substance: FRESH vs INCORPORATE ANSWERS with the synthesis rules (delete resolved, weave substance in, NO Q&A/Decisions log, "as if the user had said it that clearly the first time", leave unanswered questions open, fast and surgical) → the modes section
- L57      separate literal from MEAN; problem behind the request; where the wording is ambiguous → Working method 3
- L58      the Prompt brief spec (prose paragraphs not a form; problem/outcome/scope/constraints-user-stated-never-invented) + atomic testable-in-principle Requirements + VERBATIM Original request provenance + "Intent only — no files, frameworks, or implementation approach" → the prompt.md schema, as its owner (A5)
- L59      Open questions earned ONLY by changing WHAT gets built or scope; 2–4 concrete options plus recommended default; "None." for a clear goal — do not manufacture; never implementation questions → Working method 5
- L65-L73  the quality bar verbatim: intent-not-implementation (a sentence naming a function/framework/file does not belong); brief-not-form with the weave rule and the no-Q&A-log ban; Original request verbatim as provenance; constraints are the user's not yours (no invented deadlines/stacks); requirements as acceptance intent; open questions change the outcome + assumptions instead of asking; THE FIXED SECTION ORDER (`Original request`, `Prompt`, `Requirements`, `Open questions` — the file is parsed) → Quality bar
- L77      never read/search code, never fetch web/docs; understanding-that-needs-code is the investigator's job → Boundaries
- L78      never investigate/plan/implement; exactly one document → Boundaries
- L81      never asks the user directly; Open questions are the relay surface; a vague or over-long list directly slows the loop → Boundaries
- L82      one brief per dispatch → Boundaries
- L83      cannot-write → blocked; never paste the clarified prompt into chat (a deliverable that exists only in chat is lost) → Boundaries

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L61      (also) "never hand-write memory.md — the MCP renders it" → the loop-memory MCP surface: re-read against S12 per ROADMAP §7 — an operator who CONFIGURES such a server gets it; the body instructs none of it. The harvest itself is the memory-steward's rewritten file job, not this handler's

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L5       tools line + the sentences naming where output goes: deliverables are documents in the database (`doc_write`/`doc_read`/`doc_edit`/`doc_list`) instead of workspace files (`write`/`edit`); the role and its rules are unchanged
- L39      "reply that you are a dev-loops-only agent and end with a status=blocked FINAL REPORT" — dropped with the guard
- L52      "the ask begins with INCORPORATE ANSWERS" restated as the request listing {Question → Answer} pairs
- L60      "Write `.projects/loops/{loopSlug}/prompt.md` using the schema" → write the brief at the path the step's request names (produces: decides placement)
- L63      FINAL REPORT evidence/notes restated: summary carries the intent paragraph; open_questions carries "None." or the questions themselves

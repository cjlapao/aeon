# verifier-engineer — drop record

original: opencode/agents/verifier-engineer-loop.md  (92 lines)
rewrite:  aeon/internal/handler/bodies/verifier-engineer.md  (38 lines)
delta:    56 lines removed, 2 added

## RITUAL (deleted — the engine does this)
- L40-L42  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L44-L46  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L54      step 1: loop_get/loop_readFile/loop_readTask/scratch reads → context arrives resolved in the step's request; the criteria and finer spec arrive with it (their roles — "the checklist you must prove", "the granular checklist — prove those too" — survive in Working method 1)
- L74      step 6: `memory_append` verdict line             → there is no memory tool; lessons are an operator file, not agent state
- L80      step 8: `event_append` return checkpoint         → the daemon emits step events; a body cannot write its own timeline row
- L80-L83  FINAL REPORT line (half) + the orchestrator-validates-one-against-the-other line → the handoff is structured; the evidence-carrying half survives in the evidence field
- L89      "You NEVER mutate loop state" boundary           → the handoff IS the only output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, bash, glob, grep, todo, question]`: task DROPPED (A1, never renamed), todowrite→todo, codesearch/lsp/patch retired (W1); write/edit withheld — a verifier that can fix the code it is judging is the conflict of interest this handler exists to avoid; webfetch not re-granted
- L22-L28  permission.bash deny table (git/rm rules)        → S11's corpus carries these patterns byte-identically; `aeon policy seed-corpus` turns them into rows
- L56      step 2: `todowrite` checklist = success_criteria, one item each → the `todo` tool carries it (A19 rename), wording otherwise traceable to this line
- L75-L79  step 7: recording PASS/FAIL via `task_setStatus` (verified/failed + evidence) → the handoff outcome is the verdict and the blueprint routes it (L4/T3); the evidence the original had to record twice (status + report) is recorded once, in the handoff

## DOMAIN (kept)
- L38      "You prove a task's success criteria with evidence, or you prove they fail. You never fix code" → role line; the re-dispatch half is routing (dropped with L4/T3)
- L48-L50  per-task verification only; feature-level integration QA belongs to the QA step → the one-sentence scope guard under the role
- L58-L63  the bounded-command rules command-for-command: `timeout 600` builds per stack; run-once tests with explicit timeout and the NEVER-bare-watch-mode clause; runtime/API probes detached with `$!`, bounded readiness poll ≤30s, ALWAYS kill pass or fail → Working method 3
- L64-L66  the verdict mapping: met → evidence of commands and real output; failed → which criterion + the failing output; cannot-run → blocked naming what's missing → Working method 4 (outcome vocabulary)
- L78      "verified REFUSED without evidence — give the real thing: commands and their ACTUAL output, never a paraphrase" → the evidence field's exact clause (its server-side enforcer is DEFERRED below; the instruction is kept)
- L88      "never edit or fix code — describe why, not fix it" → Hard boundaries
- L88/L91  "No green-washing — could not run says blocked, never pass; a false pass is the worst outcome in this loop" → Hard boundaries, verbatim in substance
- L90      redact secrets from all evidence → Hard boundaries

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L76-L78  the MCP-backed evidence gate — `task_setStatus` refusing `verified` without proof, "naming what to show" → re-read against S12 per ROADMAP §7: the loop-state MCP is the deferred capability (an operator who CONFIGURES such a server gets its refusals); the Aeon substitute is the handoff schema, which makes `evidence` a REQUIRED field so a verdict without it is structurally refused at the handoff — the schema can demand the field, only a server can judge its truth, and that gap is stated rather than papered over

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L3/L38   role line shortened; "the orchestrator decides whether to re-dispatch" restated as "the blueprint routes what happens next"
- L70-L73  the status=done verification=fail wording restated in outcome vocabulary: ok / rejected naming the criterion / blocked naming the missing tool — the original's parenthetical ("status=done = I finished verifying; the pass/fail is in verification") was the ritual this rewrite removes

## T6 AMENDMENT (S17, 2026-10-26) — verify's three outcomes (L33, D8, D9, O4, Q8)
- The handoff gains the `verification` field (pass | fail | unverified), its purpose PINNED in handoff.go the way pr_summary's is — the wording is the specification. `unverified` is NOT a fifth outcome: L4's four stand, and the field's verdict reports as status `reject` — work cannot proceed on an unknown. The engine reads only the canonical `unverified: ` reason this package's Check renders FROM the field: structured end to end, prose never decides (W5's oomKilled fact consumed, never re-derived from an exit-code reading).
- cannot-run moves from `blocked` to `unverified → reject`: the loop can act on it (the redo makes the check run — raise memory, narrow scope, install the tool); `blocked` remains for what only a person can settle (unreadable criteria). QAC-05's consequence survives and is restated in the body: an OOM-killed check's silence is not a pass and not a lint finding.
- O4/Q8, answered: an unverified gate charges NO task counter — only `total_failed_gates` — and no `then:` ever fires from it. The counter it does charge is the one that bounds it: an environment that never recovers ends in the loop-ceiling block, never an infinite redo.

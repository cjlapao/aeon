# brief-author — drop record

original: opencode/agents/brief-author.md  (275 lines)
rewrite:  aeon/internal/handler/bodies/brief-author.md  (485 lines)
delta:    41 lines removed, 251 added

second source: the ADDED 240 lines are the four skill-constant sections (§1/§9/§12/§13) transcribed from
opencode/skills/proposal-brief-protocol/SKILL.md — disclosed here because the parse reads
one original line; the RITUAL section names why the body must carry them (N1 — no other
carrier exists once skills cannot load). Everything else is rewrite, not new prose.

## RITUAL (deleted — the engine does this)

- L68-L74  "Follow the `proposal-brief-protocol` skill — load it before doing anything" (the skill held the full brief template, Constant/Fill status, the shared discipline, calibration findings, path conventions) → nothing loads skills (the guard's skill refusal is the hard line); THE BODY IS NOW THE TEMPLATE'S OWNER: §1, §9, §12, §13 are transcribed INTO this body ("copy exactly" blocks) because nowhere else in the installation carries them (N1 — a body that says "copy verbatim" without the text would instruct the model to invent). The Fill sections' guidance is carried in "The template" section. Recorded as a schema-port decision (project-planner's plan.md ownership, A5's pattern, scaled up because THIS schema carries constants).
- L76-L78  "Operation Mode: Primary … invoked by a human with a proposal in hand. You delegate to nobody" → the blueprint dispatches both lanes (brief :40, self-check :47 — the body names both roles in its opening); delegation dies with the platform (no task tool exists).
- L111-L147  "Render the Brief Summary in chat" with its exact markdown block (overrides N / locked decisions N / open questions N / self-check line, and the explicit-empty rule) → THE HAZARD'S OWN SHAPE BECAME THE HANDOFF: summary/overrides/locked_decisions/open_questions/selfcheck are the block's fields — the content rule survives verbatim (the "everything stands as written" sentence is mandated in the overrides field description precisely because "an absent list reads as an unfinished check"); the CHAT rendering is what the structured handoff replaced.
- L113-L117  the FINAL REPORT terminator line → structured handoff; `verification` values ride in the selfcheck field's honest reporting (L106-L110 kept as DOMAIN).
- L103-L105  `docs/brief/NN-slug.md` numbering, "keeping the proposal's number and slug", README index mechanics → the lane's `produces: brief.md` declares the path (blueprint pins it); the INDEX-LINE habit survives (Procedure 6 + Boundaries) because the human reads it.

## BECAME MECHANISM (deleted — a tool, a gate, or a row guarantees it)

- L13-L15  `mode: primary`, `variant: coder-thinking`, temperature → A1 removed spawning; variant zero consumers (Q2); temperature ports unchanged (0.2) — TestTemperaturePortsUnchangedOnRewrites enforces it.
- L16-L33  tools map (`patch: true` + the OpenCode merge comment, `codesearch`, `lsp`, `webfetch: false` — which is a DECISION not a default: L26 "the brief is grounded in this repo, not the web" → webfetch stays OUT and a test asserts it), five MCP-prefix `false` families → allow-list `[read, write, edit, glob, grep, bash, todo, question]` (task retired A1; codesearch/lsp/patch retired W1; todowrite→todo A19; MCP families DEFERRED — see below).
- L34-L55  permission.bash allow-table → the corpus block carries this agent's rules (measured by name in corpus.json 2026-10-26) and the body binds them (`policy-agent: brief-author`, binding asserted by test); seeding stays the operator's call; the body's own sentence keeps the read-only discipline visible.
- L199-L204  the ask-mechanics ("CALL the `question` tool … one wave … listing in your reply does not ask anyone") → the `question` tool renders the blocking prompt (S5/S7 machinery); the WHY-chain survives verbatim in Resolving-locked-decisions (quietly-assumed-then-discovered is the named failure), and the decision-block format (§0b show-before-ask) survives as DOMAIN.

## DOMAIN (kept)

- L60-L66  the frame: brief not plan; one file; "a planner handed tasks stops investigating and starts transcribing" → opening, verbatim in substance.
- L80-L88  why the brief beats its proposal — stale statements, the two-authorities reconciliation failure, "§7 Overrides is the highest-value part and exists only because you went and checked" → Why-the-brief-beats section.
- L92-L110  the procedure nearly whole: visible checklist, read proposal + architecture rules (L98-L99), read two existing briefs and match register (L100), verify claim-by-claim as the bulk of the work (L101), resolve locked decisions (L102), index update including implementation-order row and revealed dependencies (L104-L105), self-check with honest reporting and fix-the-brief-not-the-answer + the two defect-born rows (A6 production-call-site, C6 test-per-contract) with their "satisfiable on paper / shipped a defect anyway" reasons (L106-L110).
- L151-L190  the verification discipline whole: the three-outcome table (true/false-or-stale→override/unknowable→§9.4-question), all nine special cases with their stories — the queue library's distributed-single-firing audit failure (L167-L171), the open-form rule with the shortlist that hid the working mechanism (L172-L175), both-greps evidence (L176), pattern claims vs sibling packages (L177-L178), dependency direction as a boot-cycle override (L179-L180), migration advice confirm-with-user (L181-L182), count-the-vague-words (L183), interface-methods-with-no-caller as the wiring gap that compiles and passes tests (L184-L186), and closing the override list explicitly (L188-L190).
- L194-L228  locked decisions: owner-ownership + the mid-task-inconsistency reason (L196-L198), sparing-one-wave ask mechanics (kept as DOMAIN above), show-the-decision-before-asking block with its five lines (L206-L216), the four typical decision kinds (L219-L225), and the don't-ask list ending in "assume it and record it — visibly an assumption" (L227-L228).
- L232-L254  the quality bar all eight: evidence-quoted overrides ("asserted without evidence is just disagreement"), constants verbatim, no-tasks-no-code with the scope-priority exception (L236-L238), the case→outcome-table rule with the audited-brief failure (tabular implemented / prose missed) and "not yet a requirement" (L239-L243), one-vocabulary-per-column with the nearest-member consequence (L244-L247), point-at-a-test (you do not name them — the planner does) (L248-L249), genuine-forks §9.4 (L250-L251), §9 described-not-pre-located (L252-L253), dependencies-in-header-and-index (L254).
- L258-L275  boundaries all eight: no breakdown even as suggestion (L260-L261), never contradict silently (L262-L263), never trim a constant (L264), never invent a locked decision (the §9.4-vs-§6 test) (L265-L266), file lane (L267-L268), no git writes no rm (L269), one brief per session (L270), override-storm → stop naming failed claims (L271-L272), blocked-never-pasted with the map-not-stand-in sentence (L273-L275).
- L299-L326, L494-L552, L639-L752, L753-L833 of the skill  (the four constant sections §1/§9/§12/§13) → transcribed as "copy exactly" blocks: the brief-not-plan framing, I1–I10 with every reason clause, R1–R13 including the manual-validation-guide's full contract (prerequisites/external services/numbered copy-pasteable steps/DoD coverage/failure modes/appendix/walked-with-artifact), A1–A8/B/C/D/E/F with the evidence rules, the failure meanings, and "fix before dispatching". This is the largest port in the five bodies and the honest consequence of the constants having no other carrier once skills cannot load.

## DEFERRED (kept OUT — the capability is not in the body, and why)

- L29-L33  the five MCP-prefix families the original denied BY NAME (`filesystem_*`, `github_*`, `memory_*`, `playwright_*`, `postgresql_*`) → the S12 MCP client is the deferred carrier (ROADMAP §7): an operator who CONFIGURES a server gets its tools; there is nothing here to deny, and the absence is recorded rather than re-enacted.
- L70-L74  the skill's calibration findings and the proposal-side schema (Part 1) → the proposal halves shipped with T13's body (its own four-section schema); the calibration PROSES beyond what the constants carry stay in the original repository as history; nothing references a document the installation does not have.
- L639-L833 (of SKILL.md) plan-side consumers of §12/§13 (R1–R13 are executed by the PLANNER, not by this agent) → Aeon's planner body (project-planner) predates this port and pins its own ~500-line sizing convention; where §12's ~300-line guidance and the planner's measured budget differ, the planner's own text governs its output and the brief hands the planner the rules as DOCUMENTED in the brief. Recorded as a known seam (F-α family), not smoothed over.

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L5       tools line + the sentences naming where output goes: deliverables are documents in the database (`doc_write`/`doc_read`/`doc_edit`/`doc_list`) instead of workspace files (`write`/`edit`); the role and its rules are unchanged

- L494-L552 and L753-L833 (of SKILL.md) — the constants' repo-specific hardcodes generalised so a brief written for ANY target repo stays true: I8 `AGENTS.md` → "the project's own agent documentation"; I9 `internal/database/` → "the shared database location" (the "single sanctioned cross-boundary placement" clause kept); A8 `git status --porcelain` → "check the working tree is clean"; I5's `grep -rln` prose kept as the instruction; A1's literal regex re-expressed as its enumerated words; §13-C2's D4-specific grep generalised to "the removed path"; §13-R13's `docs/oauth-integration-test-guide.md` model-file pointer and `curl`/docker wording re-expressed as "a complete copy-pasteable request" / "run locally or hosted" — every INVARIANT and every reason clause survives; only other-repo file names go. (These are the only edits inside the "copy exactly" blocks; a trimmed constant is the named failure, so the disclosure is exact.)
- L106-L110  "the brief's own §13 self-check … the two rows (A) and (C)" → named precisely: A6 and C6, matching the transcribed table.
- L199-L204  the ask-paragraph split: mechanics to the tool half (BECAME MECHANISM), the WHY-chain and §0b block kept in place.

## NIL (recorded, not empty)

- None for the corpus: `brief-author` HAS a block (measured by name 2026-10-26) — the body DECLARES `policy-agent: brief-author` and the binding is test-asserted (empty CommandRules fails). The second follow-up binding after proposal-author; the three remaining kinds will record nil.
- L60  2026-10-27 (S38 T10, counted in the delta):(S38 T10, +7 counted): the records paragraph — the approved brief FILED via `goal_show`+`brief_create`+`attach`; a contract nobody can query is a contract nobody can hold the loop to. OL14 — the approved proposal becoming the brief; now the brief is also FILED (`brief_create`+`attach`). OL14.

- L70-L74  2026-10-27 (S38 T14, counted in the delta): the brief-contract sentences — file path
  named (`brief.md` at the workspace root), the goal coordinate read from the loop (`goal_show`;
  the (id, version) `brief_create` files against), and the revision rule (another `brief_create`,
  the next version, never an edit of the judged row). The T10 paragraph already instructed the
  filing; T14 made the contract's NAME and PLACE explicit, as the emitted contract asks by name.

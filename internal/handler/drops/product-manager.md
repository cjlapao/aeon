# product-manager — drop record

original: opencode/agents/product-manager.md  (240 lines)
rewrite:  aeon/internal/handler/bodies/product-manager.md  (133 lines)
delta:    108 lines removed, 1 added

## RITUAL (deleted — the engine does this)

- L4-L5, L24, L46-L51  "subagent" mode, the delegation trigger block ("task-coordinator delegates in step 2 … report back with the Issue number and URL") → the blueprint dispatches the lane (development-map-pipeline :23, produces feature.md); reporting is the structured handoff (summary/slug/ac_count/dod/questions_asked — the "Issue number and URL" became the slug and the document path the engine declares).
- L28-L34  "load the development-map skill; its Canonical Operational Rules are the binding source of truth (do not keep a private copy here, so they can never drift)" plus their five bullets → nothing loads skills; the rules that survive in Aeon are engine rulings (bounded verification commands are engine-enforced, checklist discipline lives in the todo tool, domain containment is the file-lane guard) and the residue that is genuinely this agent's craft — pre-task reads overriding defaults, stay-in-domain — is stated inline once, at its point of use (Procedure and tools paragraphs), which is exactly the drift risk the original accepted by inlining its ONE kept rule.
- L36-L40  the FINAL REPORT terminator "kept inline even if the skill fails to load" → the structured handoff is the machine-parseable contract now; its notes field carries the one-short-sentence rule.
- L129-L134  the Pull Requests table template → one pointer line in the document ("PR records ride the engine's rows"); PR state is engine/forge state, and a markdown table duplicating it is a second source of truth.
- L231-L240  Knowledge Retention — delegate JSON to memory-steward for roadmap-shaping decisions → no delegation (the task tool is retired; A1); lessons are operator files per the S13 ruling carried by the project-planner record; a handoff that names the decision is what persists it.

## BECAME MECHANISM (deleted — a tool, a gate, or a row guarantees it)

- L4-L18  mode/variant/permission → A1 removed spawning; temperature ports unchanged (0.7) — TestTemperaturePortsUnchangedOnRewrites enforces it.
- L6-L17  tools map (codesearch, task, todowrite→todo) plus **webfetch: true dropped** → allow-list [read, write, edit, glob, grep, question, todo]; the honest reduction is recorded, not hidden: no body instruction uses the web, and the over-grant guard would have to see the tool NAMED in the body to allow it — inventing a use to keep a grant is the fiction the guards exist to stop. Test asserts bash AND webfetch absence (the gh world is gone below).
- L58-L64  the gh label existence dance → the forge world is BECAME MECHANISM twice over: labels/Issue state live in forge rows (S16 forge.openIssue is the engine's action with an effect class — the pipeline's file lane produces feature.md, not an Issue), and a file-writing producer cannot run gh at all (bash not granted). Slug collision-checking survives as the craft-half: glob/grep the existing feature docs (Procedure 3).
- L66-L67  gh issue create + label apply + "report the number and URL" → write the declared produces path; handoff reports slug and counts. The Issue-anchored pipeline is now the file-anchored pipeline — disclosed in T17's F-α note, not papered.

## DOMAIN (kept)

- L22-L26  the role frame — strategic advisor whose ONE operational duty is the artifact anchoring the downstream pipeline; "NEVER implement code, create task breakdowns, or manage branches/PRs" → opening, restated at Boundaries.
- L55, L56  procedure steps 1–2 verbatim in substance: read the request (who/what/why), **ask before you write** — one or two targeted questions when value/criteria/edge-cases/scope are unclear, "do not invent acceptance criteria from thin air" (the handoff's questions_asked field makes the ask-when-unclear duty reviewable).
- L57  slug rules (kebab-case, short, descriptive, examples) → Procedure 3.
- L69-L98  Asking-the-user whole: the four typical question kinds verbatim (scope/platform/failure behaviour/CI gates), the shape (one-line header, 3–5 options with explanations, always "Other — I'll type it"), one-at-a-time with the stale-options reason, and the do-NOT-use-for-open-ended-intake rule → kept in full; only the worked YAML example compressed (the shape sentence carries what the example showed).
- L100-L127  the template's content half verbatim: Description (2–5 sentences, who asked, what problem), User Story (as-a/want/so-that exactly), AC checkboxes, DoD three defaults — every word of the three defaults survives.
- L136-L157  the acceptance-criteria quality bar whole: testable/atomic/observable with their one-line definitions and BOTH good examples and all three bad examples with their reasons ("not testable" / "prescribes implementation, not behaviour" / "not atomic").
- L155-L157  DoD defaults sacred — "add more if the feature warrants, never remove" with its two examples → the sacred section, additions-only, and the handoff field that states intactness.
- L159-L192  the three standalone operations (analysis headings; prioritization matrix P0–P3; spec headings) → compressed to their shapes, same quality bar applied.
- L206  "if they come back asking for clarification, update the document — do not create a new one" → the edit-tool rule.
- L228  already-supplied AC+DoD → "say so — do not rewrite what the human already produced."

## DEFERRED (kept OUT — the capability is not in the body, and why)

- L33  context7 docs lookup ("if down, proceed from best knowledge, flag it — never block") → external doc services have no Aeon carrier; the MCP client is the deferred home for exactly this class of capability (ROADMAP §7): the operator who CONFIGURES a docs server gets its tools; there is nothing here to pre-configure or deny.

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L5       tools line + the sentences naming where output goes: deliverables are documents in the database (`doc_write`/`doc_read`/`doc_edit`/`doc_list`) instead of workspace files (`write`/`edit`); the role and its rules are unchanged

- L56-L57  "prefer the question tool over inline text" → the tool IS the only ask-channel now; the craft rules that made it worth preferring (concrete options the user can override with Other) survive verbatim in Asking-the-user.
- L100-L134  the Issue template → the feature-document template: Description, User Story, Acceptance Criteria, Definition of Done survive verbatim in shape and wording; Tasks becomes a pointer to the task-breakdown lane (the sequential-[Task-N]-strictly-sequential clause kept verbatim in substance — it is the contract the breakdown lane inherits); Pull Requests collapses to its truth (engine rows own PR state).
- L194-L219  Coordination + What-You-Don't-Do → Boundaries: the five don'ts survive (no code, no task issues→breakdown lane, no branches/commits/PRs→forge actions, no reviews, no progress tracking); "update the Feature Issue, do not create a new one" (L206) survives as the edit-tool rule.
- L221-L229  the failure table: the three surviving rows re-expressed at their point of use — inventing requirements→ask-first with "silence is not consent" in the questions_asked field rule; non-atomic "and"→Quality bar; slug collision→Procedure 3; "user already supplied AC+DoD→tell coordinator to skip"→Boundaries ("say so — do not rewrite what the human already produced"); defaults-are-required→the sacred section.
- L159-L192  the three standalone operations: kept compressed (analysis sections named by heading, ranking, spec headings) — the shapes survive, the backtick invocation strings do not (nothing dispatches by string here).

## NIL (recorded, not empty)

- product-manager has NO corpus block (measured by name among the 38, 2026-10-26). The body declares no policy-agent; the loader's name-fallback finds no block and CommandRules is NIL — the test asserts nil (not empty): a nil recorded is a finding, an invented binding to nothing is the fiction the nil rule refuses. No command rules bind this agent; its discipline is the body's own text plus the guards.

# project-manager — drop record

original: opencode/agents/project-manager.md  (201 lines)
rewrite:  aeon/internal/handler/bodies/project-manager.md  (132 lines)
delta:    73 lines removed, 4 added

## RITUAL (deleted — the engine does this)

- L4-L5, L23, L43-L49  "subagent" mode, the trigger block (task-coordinator delegates step 3; "create one Task Issue per task, linked as a child"; "use label pending") → the blueprint dispatches the lane (development-map-pipeline :28, produces tasks/); child-linking is the engine's lineage rows; the handoff (task_count/task_files/specialists/depends) is the report-back.
- L25-L31  "load the development-map skill; Canonical Operational Rules binding" + five bullets → nothing loads skills; survivors stated once at point of use (pre-task reads in the tools sentence; checklist via todo); the rest are engine rulings (bounded commands, file-lane guard, no self-advancing state — the engine owns transitions).
- L33-L37  FINAL REPORT terminator → structured handoff.
- L104-L120  the six-label GitHub vocabulary TABLE with set-by column and the gh label mechanics → the STRINGS survive verbatim (they are the contract vocabulary downstream readers share) but as task-file state, not forge labels: pending is the body's at creation, the rest belong to the agents named in the table, and the two post-creation exceptions (cancelled → say why; split/merged → update both, notify) survive in the same section.
- L132-L138  Secondary Operation "update task state on request" (gh issue view, edit labels, report) → engine rows own transitions; the body keeps only the two exceptions above.
- L192-L201  Knowledge Retention → memory-steward delegation JSON → no delegation (task tool retired, A1); lessons are operator files (S13 ruling); the handoff naming the pattern is what persists it.

## BECAME MECHANISM (deleted — a tool, a gate, or a row guarantees it)

- L4-L17  mode/variant/permission → A1; temperature ports unchanged (0.7) — TestTemperaturePortsUnchangedOnRewrites enforces it.
- L6-L17  tools map (codesearch, task, todowrite→todo) → allow-list [read, write, edit, glob, grep, question, todo]; bash OUT (the gh world — a file producer runs no gh), task OUT. Both absences test-asserted; the over-grant guard would refuse to grant an unnamed tool anyway, so the honest shape is the allow-list the body actually uses.
- L55, L61, L63  gh issue view / gh issue create / issue-number reporting → read the declared feature path; write one file per task into the declared produces directory (the engine checks it exists and is non-empty — the produces gate IS the "files_changed={N}" contract); handoff reports count/paths/order.
- L60  "pick the canonical agent name … roster in task-coordinator.md" → the roster is the REGISTRY: a step naming an unregistered agent is refused at pin time (the preflight door) and at dispatch (handler.Build's miss) — assigning a name with no body is now structurally refused, which is stronger than the prose rule. The body points at the registry and the coordinator's roster; the never-unassigned rule survives (Common Mistakes L186).

## DOMAIN (kept)

- L21-L23  the role whole — task files as CONTRACTS between coordinator, specialist and reviewers; "NEVER implement code; plan, define, track through task files" → opening, verbatim in substance.
- L53, L69  the strictly-sequential mandate with its reason (each task its own branch; conflicts prevented) and the numbering-equals-execution-order rule (lower first, never shared) → "The order is the plan", the two statements merged into one honest section.
- L55-L63  procedure: extract every criterion (1), one-deliverable-one-specialist decomposition with all three rules of thumb verbatim (2), never-unassigned + escalate-if-no-fit (3, from Common Mistakes L186), kebab-case task names (4), Depends-on lines (5), report count+order+dependencies (6) → Procedure 1–6.
- L65-L102  the task template every-heading-required: Task/[Task N]-one-line-summary, Assigned Specialist canonical name, Parent Feature (path form), Depends on (comma-separated or none), task-specific-AC with the copied-feature-AC prohibition, DoD three exact defaults, Implementation Notes with its filler-owner line → the file template, every heading surviving.
- L106-L115  the six state strings verbatim (pending/in-progress/ready-for-review/reviewed/approved/merged) and "downstream agents read them — use these exact strings" → the vocabulary section; only the forge-label carrier changed.
- L117-L121  the two post-creation exceptions (cancelled → explain why; split/merged → update both and notify) verbatim → same section; everything else stays hands-off.
- L122-L130  the five-point decomposition quality bar verbatim → its own checklist.
- L140  the scope prohibition — never edit AC/DoD after creation, new task or split, never mutate a contract mid-flight → its own named section with the why stated.
- L144-L159  coordination craft surviving at point of use: vague-criteria → flag back, do NOT paper over (the invented-criteria propagation reason kept verbatim in substance); too-large → split with re-sequenced numbers; L163-L164 (steward updates only) and L166-L169 (reviewer reads AC/DoD; you produce no review artifacts) folded into Boundaries.
- L171-L177  What You Don't Do → Boundaries (code, feature document ownership, branches/PRs, reviews).

## DEFERRED (kept OUT — the capability is not in the body, and why)

- L30  context7 current-docs lookup ("down → proceed from best knowledge, flag it, never block") → no Aeon carrier for external doc services; the S12 MCP client is the deferred home (ROADMAP §7): configured server ⇒ its tools; nothing here to pre-configure or deny.

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L5       tools line + the sentences naming where output goes: deliverables are documents in the database (`doc_write`/`doc_read`/`doc_edit`/`doc_list`) instead of workspace files (`write`/`edit`); the role and its rules are unchanged

- L53, L69  the strictly-sequential mandate and the numbering-must-equal-execution-order rule (both stated twice in the original, once in procedure once in template) → one section "The order is the plan" carrying both, with the PR-branch conflict reason kept as history-cleanliness; the never-share-a-number and lower-first clauses verbatim.
- L56-L59  decomposition rules of thumb verbatim (one PR ~<500 lines; overlapping files merged-or-sequenced; tests-in-task-unless-distinct-deliverable).
- L65-L102  the Task Issue template → the task-file template: every heading survives (Task/Assigned Specialist/Parent Feature/Depends on/Acceptance Criteria/Definition of Done with the three exact defaults/Implementation Notes-with-its-filler-owner clause).
- L122-L130  the five-point decomposition quality bar → verbatim checklist.
- L140  the scope-mutation prohibition ("NEVER edit AC or DoD after creation — new Issue or split") → its own named section; "never mutate a contract an engineer is already working against" states the why.
- L142-L169  Coordination with four agents → Boundaries: vague-criteria→flag-back-not-paper-over (L153, verbatim in substance, handoff blocked naming criteria); too-large→split with re-sequenced numbers (L159); feature-document ownership; no reviews.
- L171-L177  What You Don't Do → Boundaries (code/feature-doc/branches-PRs/reviews); "PROJECT.md replaced by Task Issues" (L177) dropped with the file-world it named.
- L179-L190  Common Mistakes table → the four surviving rows re-expressed at point of use: multi-specialist→one-specialist rule; overlapping files→sequence-or-merge; copied feature-AC→the named mistake at the AC section; unassigned→never; undeclared dependency→always-declare; numbering→the order section; label vocabulary→the vocabulary section's "nothing outside is valid".

## NIL (recorded, not empty)

- project-manager has NO corpus block (measured by name among the 38, 2026-10-26). No policy-agent declared; loader name-fallback finds nothing; CommandRules NIL — the test asserts nil (not empty). Third of the five nils (task-coordinator, product-manager, project-manager); the two with blocks (proposal-author, brief-author) bind and their bindings are asserted.

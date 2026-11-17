# project-planner — drop record

original: opencode/agents/project-planner-loop.md  (183 lines)
rewrite:  aeon/internal/handler/bodies/project-planner.md  (85 lines)
delta:    106 lines removed, 8 added

## RITUAL (deleted — the engine does this)
- L35-L37  "Follow the dev-loop-protocol skill — load it first"  → nothing loads skills here; the schema it carried is written into this body (A5 — the planner is plan.md's owner)
- L41      `loopSlug` verbatim + loop_get + $ROOT framing  → context arrives resolved in the step's request
- L47      loop_readFile(prompt)/loop_readFile(discovery)/loop_get/loop state reads → the step carries the brief and the discovery; their ROLES (map straight into Objective; read in full) survive in Working method 1
- L83      `memory_append` planning lesson                  → there is no memory tool; lessons are an operator file, not agent state
- L84      `event_append` return checkpoint                 → the daemon emits step events; a body cannot write its own timeline row
- L85      FINAL REPORT line (half)                         → the handoff is structured; task-count evidence + detail-file-exists survive as handoff fields (REWORDED)
- L181     "file lane is plan.md/tasks/scratch... NEVER mutate loop state" (half) → the handoff IS the output; tasks_derive/orchestrator derivation is gone — the blueprint parses the plan the same way; the LANE (you write the plan and nothing else) survives in Boundaries

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, glob, grep, todo]`: task DROPPED (A1, never renamed), todowrite→todo, codesearch/lsp/patch retired (W1); bash withheld AS THE ORIGINAL HAD IT ("You have no bash/pwd", L41) — sizing comes from measured hotspots plus reads, which is the design; webfetch not re-granted; question not re-granted (the plan gate is the operator's surface)
- L22-L33  permission.bash deny table (git/rm rules)        → the corpus carries these byte-identically for this agent; with bash withheld the rows guard a door this handler never opens — seeding stays the operator's call
- L42-L46  mode detection from the dispatch `ask` (standard/simple/cleanup/qa-fix/split with their loop-file read lists) → the request names the mode; the JUDGEMENT in these modes (never renumber, append after the last task, cite finding/defect ids, supersede-don't-delete, chain continues) survives in Boundaries "Appends continue the chain"
- L76-L81  `loop_readFile("plan")`/`loop_readTask` as the write targets; `detail:` pointer resolved via loop_readTask → the plan and task specs are ordinary files written through `write`/`edit` at the paths the step's request names (produces: declares them); the detail-pointer field is kept as a plan field, its loop-dir resolution is gone
- L160     (half) "the loop-state MCP now REFUSES a batch whose dependency names no task..." → the enforcement server is DEFERRED (below); the SELF-CHECK that survives (every depends_on names an existing task) is the body-side twin

## DOMAIN (kept)
- L41      "Ground the plan in evidence — discovery OR a targeted read (never chat alone)" + the blocked-with-missing-discovery rule → Working method 1
- L48-L55  right-sizing nearly whole: big-enough-to-matter; the ~500-CHANGED-LINE BUDGET with its failure story (long run → compaction → drift); breadth vs DEPTH split with the 1,800-line single-file example and SECTION splitting; est_changed_lines on every task; the COPY/PORT recipe (mechanical copy then per-file/per-section adaptation, "copy + adapt everything" as a planning defect); the strictly-linear framing → Working method 3
- L56-L72  the tech roster and its routing judgements: frontend tag from the repo's ACTUAL framework never from habit; ui-design = visual layer with logic/state staying at the framework engineer, React+Tailwind kit caveat for Vue repos; docs task triggers (endpoints/CLI/config/user-facing; depends_on the code; when in doubt add it); sql preferred for DB-heavy with EXPLAIN-not-coverage criteria; websocket for dedicated real-time; terraform PLAN-ONLY criteria; docker build+scan+healthcheck+size never push; the "Unsupported" section, never invent an engineer; NO security tasks (automatic gate) → Working method 4
- L74-L75  explicit checkable criteria with the named examples, and pinning exact identifiers from package.json so a criterion can't be met by a self-invented near-miss → Working method 5
- L78-L80  the plan.md schema in full: Understanding as the user's comprehension gate with its three named subsections and their contents; "write it in prose, in your own words — a block that paraphrases its source back is worthless"; honest uncertainty at the gate; Objective as the executive summary + FIXED parseable fields + recovery anchor; Test suite slug rule; Design system line; the task-entry field list → the plan.md section, as its owner (A5)
- L82      ui-design criteria must be visual/observable, with the glass-variant example → Working method 6
- L87-L134 the linear-chain construction nearly line-for-line: why a graph is a defect; derive-true-graph-then-flatten; the seven tie-breakers in priority order; numbering IS position; depends_on = "runs after" with the fan-out/orphan worked example; never-[]-except-T1, one predecessor only, parallel-language ban with the translation rule; the one-line real-reason rule; the four-point self-check; the re-dispatch chain continuation → the order section + Boundaries
- L141-L153 the per-task detail schema verbatim in substance: mechanical-execution bar; File(s)/Context (read-first list, pinned identifiers)/What to do with the DEPTH POLICY (actual code vs annotated skeleton + mirror-the-pattern reference, code-is-a-guide-never-authority, never exceed est_changed_lines)/Validation (repeat criteria + finer checks); one task one file → the detail-file section
- L155-L167 the quality bar (minus the MCP half): every field mandatory; over-budget is defective; uncheckable criterion means vague task; paths relative; 3–10 tasks; end-to-end demonstrable last task
- L169-L177 the path discipline verbatim including the self-check sweep and "paths from evidence not guesses" (the one detail-pointer exception was loop plumbing — dropped with it)
- L182     never edit source code — spec code is guidance → Boundaries
- L183     one plan per dispatch; Modify re-dispatch rewrites index AND task specs in sync → Boundaries

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L160     the loop-state MCP that REFUSES bad batches (unknown dependency id, self-dependency, duplicate id) → re-read against S12 per ROADMAP §7: MCP tooling is the deferred capability; the body carries the self-check that made the MCP necessary (check every reference before finishing) so the class of defect is caught at authoring. A blueprint-side batch validator is the Aeon-side counterpart — named as follow-up, not invented here
- L150     "the engineer verifies against the real files + current docs (context7)" → the context7 MCP half rides with S12 (an operator who CONFIGURES the server gets it); "verifies against the real files" is kept

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L5       tools line + the sentences naming where output goes: deliverables are documents in the database (`doc_write`/`doc_read`/`doc_edit`/`doc_list`) instead of workspace files (`write`/`edit`); the role and its rules are unchanged
- L3       the description's dev-loops framing compressed to the blueprint
- L41/L47  dispatch/loop reads restated as "the step's request" carrying brief + discovery
- L56-L71  engineer names restated as tech tags the blueprint routes on (registry names, no `-loop` suffix); the `loop`-suffix examples in tie-breakers kept as-is
- L76/L81  "two coordinated artifacts (see the skill's schemas)" restated as the owner's own schema sections
- L85      evidence="plan.md + 6 task specs" restated as task_count + the detail-spec-exists claim in summary
- L174     the loop-dir detail-pointer exception dropped with the pointer plumbing (its safety intent — one exact form, no `..` — survives inside the sweep rule)
- L160  2026-10-27 (S38 T10, counted in the delta):(S38 T10, +8 counted): the records paragraph — plan rows filed through `task_create`/`attach` after the files exist, `task_show`/`task_list` before re-filing; `task_set_status` withheld from this handler on purpose (the engine owns the ladder). OL14 — the depends_on self-check whose MCP batch-refusal half the records doors now carry through `task_create`. OL14.

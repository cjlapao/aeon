# investigator — drop record

original: opencode/agents/investigator-loop.md  (82 lines)
rewrite:  aeon/internal/handler/bodies/investigator.md  (46 lines)
delta:    36 lines removed, 0 added

## RITUAL (deleted — the engine does this)
- L37-L39  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L41-L43  "Follow the dev-loop-protocol skill" + ritual    → nothing loads skills here; the discovery.md schema it carried is written into this body (A5 — this handler owns discovery.md)
- L54      loop_get + the "loop not found → blocked, orchestrator must loop_init" contract → the loop is the engine's concern; a step without its context arrives as a failed dispatch, not a research finding. The blocked-honesty half survives generically (REWORDED)
- L65      step 6: `memory_append` lesson                   → there is no memory tool; lessons are an operator file, not agent state
- L66      step 7: `event_append` return checkpoint         → the daemon emits step events; a body cannot write its own timeline row
- L67      step 8: FINAL REPORT line (half)                 → the handoff is structured; measured-hotspots evidence survives as the hotspots field (REWORDED)
- L79      "the `edit` tool is enabled... only because OpenCode's globstar matching blocked loop-file writes" → the quirk note dies with the quirk; `edit` is withheld from the allow-list (rewriting the discovery is a `write`), the lane itself survives in Boundaries

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, glob, grep, bash, webfetch, todo]`: task DROPPED (A1, never renamed), todowrite→todo (A19), codesearch retired (W1 — grep is the registry's search), lsp/patch retired; edit withheld per the quirk note above; question not re-granted (the orchestrator-relay design is kept); webfetch GRANTED — unlike the coder bodies, this original's webfetch use is the body's own (L60), kept below
- L22-L33  permission.bash deny table (git/rm rules)        → the corpus carries these byte-identically (this agent's rows include the `git checkout*` deny); seeding stays the operator's call
- L63      `loop_writeFile` for the discovery               → an ordinary file written through `write` at the path the step's request names (produces: declares it)

## DOMAIN (kept)
- L35      "research arm... turn a goal into a grounded discovery the planner can break into tasks. You investigate; you never implement" → role line
- L47-L50  the two dispatch modes verbatim in substance: FRESH vs INCORPORATE ANSWERS with the Decisions-section rule (verbatim decision + one line of rationale), the Constraints/Recommended updates, and "editing, not researching" → the modes section
- L55      prompt.md FIRST as the authoritative intent, then AGENTS.md/CLAUDE.md/project.md grounding rules, then Learnings.md → Working method 1 (a consumer's reference — the prompt.md schema stays with its owner)
- L56-L57  investigation with the prior-work/other-loop rule: read the code it produced and its artifacts, extract the concrete CONTRACTS (endpoints+shapes, types, auth, errors), synthesize FRESH — "do not assume the other loop's discovery covers it, and do not just point at it" → Working method 3
- L58      MEASURE the size of the work: wc -l/find/git ls-files per file the goal touches, flag ≳400-line HOTSPOTS, the copy/port source-location + adaptation-surface gauge, "the planner is blind without this (it has no shell)", "a missing hotspot is exactly how a giant copy-and-adapt task shipped, ran long, and drifted" → Working method 3, verbatim in substance (its bash framing kept: the planner's bash-less design is why this field is load-bearing)
- L60      external research with webfetch (docs/blog/RFCs/GitHub) + versions → Working method 4 (the context7 half is DEFERRED)
- L63      the discovery.md schema verbatim: summary / codebase findings / external research with versions / constraints & risks / effort & size hotspots (measured) / recommended approach / open questions; PROJECT-ROOT-RELATIVE paths with the strip-the-root rule → the schema, as its owner (A5)
- L64      the "## Legal review" flag with its full trigger list (dependency licenses, privacy, export/crypto, ToS, regulated domains), needed yes/no + why, the blueprint reads it for the CONDITIONAL review BEFORE planning, "when in doubt flag yes — a cheap clear beats a license/privacy problem discovered after the code ships", and "you do NOT do the legal analysis yourself; you only raise the flag" → Working method 6
- L69-L75  the quality bar verbatim: specific-not-generic with its gin example; planner-splittable approach; hotspots measured not guessed ("load-bearing"); open questions earn their place ONLY by changing the plan, options + recommended default, decide-from-evidence yourself never punt trivia, "None." ✓
- L80-L82  never git writes / never rm; one discovery per dispatch (do not plan — that is the planner); honest partial over silent stop → Boundaries

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L61      "For docs, prefer the context7 MCP tools (`resolve-library-id` then `query-docs`). Other configured MCP sources (reddit..., etc.) are fair game when relevant" → re-read against S12 per ROADMAP §7: with the S12 MCP client this is no longer fiction — an operator who CONFIGURES those servers gets those tools; the body instructs none of them. The ORIGINAL's own fallback is what the body keeps: `webfetch` official docs with versions noted

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L5       tools line + the sentences naming where output goes: deliverables are documents in the database (`doc_write`/`doc_read`/`doc_edit`/`doc_list`) instead of workspace files (`write`/`edit`); the role and its rules are unchanged
- L3       description's dev-loops framing compressed to the blueprint
- L54      the loop-not-found refusal restated as generic blocked-honesty (name what's missing, don't fake research) — the loop_init instruction was orchestrator plumbing
- L63      "Write `.projects/loops/{loopSlug}/discovery.md`" → write the discovery at the path the step's request names (produces: decides placement)
- L67      FINAL REPORT evidence="effort hotspots: N" restated as the required hotspots handoff field
- L82      "emit the anti-silent-stop line, and report status=incomplete" restated as report-what-remains-undone-in-the-summary (the status enum is the engine's now)

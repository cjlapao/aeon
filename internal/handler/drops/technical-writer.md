# technical-writer — drop record

original: opencode/agents/technical-writer-loop.md  (98 lines)
rewrite:  aeon/internal/handler/bodies/technical-writer.md  (55 lines)
delta:    43 lines removed, 0 added

## RITUAL (deleted — the engine does this)
- L41-L43  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L45-L47  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the 1000-line rule and FINAL REPORT shape it carried live in the coder bodies / the handoff
- L57      step 0: `pwd` + `loopSlug` + path discipline     → context arrives resolved in the step's request
- L58      step 1: loop_get + loop_readFile(plan) + loop_readTask(taskid) + scratch reads → the step carries the task, the documented tasks' specs, and the engineers' notes it names
- L70      step 6: scratch note via `loop_writeFile`        → the handoff summary + doc_validity is what the next step reads (its "verification commands + link-check results" survive as the doc_validity field)
- L71      step 7: `memory_append` doc lesson               → there is no memory tool; lessons are an operator file, not agent state
- L72      step 8: `event_append` return checkpoint         → the daemon emits step events; a body cannot write its own timeline row
- L73      step 9: FINAL REPORT line (half)                 → the handoff is structured; the verification=pass condition survives in doc_validity (REWORDED)
- L95      "You NEVER mutate loop state" boundary           → the handoff IS the only output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, bash, glob, grep, todo]`: task DROPPED (A1, never renamed), todowrite→todo (A19), codesearch/lsp/patch retired (W1), webfetch not re-granted (its only use was the AGENTS.md-sync context read the request carries); question not re-granted (blocked-naming-the-owner is the original's design)
- L22-L32  permission.bash deny table (git/rm rules)        → the corpus carries these byte-identically; `aeon policy seed-corpus` turns them into rows
- L61      step 4: `todowrite` checklist                    → the `todo` tool carries it (A19 rename), wording otherwise traceable to this line
- L93      "One task per turn"                              → the blueprint decides which step runs; a body policing turn count duplicates the scheduler
- L96      "Never run git writes/commits. `git-loop` owns git" (half) → the corpus denies git writes; "belongs to the blueprint" restates the owner

## DOMAIN (kept)
- L37      the role and THE ONE RULE verbatim: "documentation that isn't tested will lie" — every command runs, every sample real, every API entry built from actual code/schema not transcribed from memory → role line
- L39      AGENTS.md sync as a special request: the trigger, the change-list (service/module/endpoint pattern, build/test/lint commands, env var, folder, how-to-add X), APPEND-OR-AMEND-NEVER-REWRITE-WHOLESALE with its reason ("a bad rewrite misleads every future agent"), pointer-file caveat, verify-what-you-add + flag low-confidence, doesn't-exist → report and do nothing (creating it is not your job) → kept as its own section
- L51-L53  the doc set's CONTENT (API entry completeness list: purpose/auth/params types-required-defaults/examples/every error/since-version/stability; guides/config/concepts; README as entry point: what, who, how to start, where next; documents what the tasks changed, never source) → kept with the ADDRESSES removed — which files is what produces: decides (spec ruling); the CONTENT rules stay
- L59      read the actual code/API until writing from understanding, generate from schema/source never hand-transcribe → Working method 3
- L60      read existing docs + toolchain + style config first, extend the established structure not reinvent → Working method 4
- L62      write/update with the per-endpoint completeness list + README-affected rule + ONE Diataxis mode per page (tutorial/how-to/reference/explanation, don't mix) → Working method 5
- L64-L69  doc-validity as the gate, check for check: generator build (the five named toolchains verbatim), markdownlint/Vale, lychee/markdown-link-check no-dead-links, cspell-don't-silence, samples run with real output, the bounded/detached-then-killed rule for documented long-running commands, no orphans → Working method 6
- L77-L85  writing discipline verbatim: plain language/active/present/definite with the anti-marketing example list; front-loaded; show-don't-describe with paste-able examples and no "// ..." hiding setup; schema-as-source-of-truth; error conditions documented; consistent terminology + acronyms; nav depth ≤3 + cross-links; README ~≤200 lines entry-only; no PII/secrets synthetic-only → Writing discipline
- L87-L89  the coverage carve-out verbatim in substance: no unit logic to line-cover, ≥80% does NOT apply, the doc-validity gate IS the gate, and "the verifier applies this doc gate (not coverage) for docs tasks" → Coverage carve-out (the verifier named there is the S13 registry handler)

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L47/L97  "built-in tools; never the filesystem MCP" / "the filesystem MCP is disabled" → MCP-shaped configuration: with the S12 client an operator who CONFIGURES such a server gets it; the body's built-in-tools sentence carries the intent and the toolset carries the boundary
- L95      (also) "the orchestrator records it via the loop-state MCP" → the loop-state MCP surface; the handoff outcome + the daemon's own event rows replaced it

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L3       description's dev-loops framing compressed to the blueprint
- L52      "`$ROOT/README.md` — the entry point" → "the README — the entry point" (placement is produces: now; the ≤200-line entry-only rule is content and stays)
- L53/L49  "docs/" ownership restated as "the documents... the files you touch are the ones the step's request names" (spec ruling: what goes is any instruction about WHERE; the Diataxis/content rules all stay)
- L69      "Report verification=pass only if..." restated as the doc_validity handoff field naming each check
- L94      "return status=blocked naming the owning engineer" → report blocked naming the owning engineer (outcome vocabulary)

# debugger-engineer — drop record

original: opencode/agents/debugger-engineer-loop.md  (102 lines)
rewrite:  aeon/internal/handler/bodies/debugger-engineer.md  (72 lines)
delta:    31 lines removed, 1 added
## RITUAL (deleted — the engine does this)
- L39-L41  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L43-L45  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L49      step 0: `pwd` + `loopSlug` + scratch report path → context arrives resolved in the step's request; the report path is the request's (fallback named in the body)
- L50      step 1: loop_get + loop_readTask + scratch/qa-report reads → the step's request names the failure evidence (the read-anything-that-names-the-failure rule survives in Working method 1)
- L55      step 6: `memory_append` pointer for the re-dispatched engineer → there is no memory tool; the handoff's root_cause + recommended_owner ARE the pointer the next step reads
- L56      step 7: `event_append` return checkpoint         → the daemon emits step events; a body cannot write its own timeline row
- L57      FINAL REPORT line (half)                         → the handoff is structured; the evidence/owner/report fields survive (REWORDED)
- L99      "You NEVER mutate loop state" boundary           → the handoff IS the only output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, bash, glob, grep, todo, question]`: task DROPPED (A1, never renamed), todowrite→todo, codesearch/lsp/patch retired (W1), webfetch not re-granted; write withheld at the toolset — the diagnosis report is the single written deliverable the body itself names, everything else is the handoff
- L22-L32  permission.bash deny table (git/rm rules)        → S11's corpus carries these patterns byte-identically; `aeon policy seed-corpus` turns them into rows
- L100     "Never run git writes/commits" (half)            → the corpus denies git writes; the read-only-git permission half is kept (DOMAIN)

## DOMAIN (kept)
- L37      language-agnostic investigator, on demand when something fails; diagnose with evidence; NEVER fix; the engineer applies the fix → role line
- L45      "You do NOT build, so the ≥80% coverage mandate does not apply — your single deliverable is a diagnosis" → kept under the role
- L51      reproduce with a bounded command, timeout-wrapped, servers detached and killed; cannot reproduce → say so and work from captured evidence → Working method 3
- L52      isolate & hypothesize: narrow the path, test each hypothesis against evidence (file:line, log, metric, trace, read-only git bisect/log); RULE OUT as aggressively as you rule in; keep the dead ends in the report → Working method 4, verbatim in substance
- L53      pin the root cause with concrete evidence; insufficient → say so explicitly + name the data that would close the gap; never fabricate → Working method 5
- L59-L94  the diagnosis report schema verbatim: mode + status enums, symptom from the loop's point of view, show-your-work investigation, hypothesis for/against/verdict, evidence with `file:LINE` + trimmed command output, minimal bounded reproduction, the recommended-fix table with risk/files/owner, and Prevention (the guardrail that catches a recurrence) → kept in full
- L98      never fix or patch; if you changed a source file to test a hypothesis, REVERT IT before finishing → Boundaries
- L100     read-only `git log`/`git diff`/`git bisect` for regression hunting is fine → Boundaries + tool sentence
- L101     every root cause cites concrete evidence (file:line, log, metric, trace, commit SHA); no evidence → insufficient-evidence, never a guessed cause → Boundaries
- L102     name the owner so the orchestrator can route the fix → Boundaries + recommended_owner field

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L55/L57/L99 the loop-state MCP surface: `memory_append` rendering memory.md ("never hand-write memory.md — the MCP renders it"), `event_append` timeline rows, `loop_get` state reads → re-read against S12 per ROADMAP §7: MCP tooling is the deferred capability — an operator who CONFIGURES those servers gets those tools; the body instructs none. What the originals used them FOR (telling the next engineer the root cause and owner; recording the completion) is the handoff's job and is kept there

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L5       tools line + the sentences naming where output goes: deliverables are documents in the database (`doc_write`/`doc_read`/`doc_edit`/`doc_list`) instead of workspace files (`write`/`edit`); the role and its rules are unchanged
- L3/L37   role line shortened; the `*-engineer-loop` handoff restated as "the engineer the blueprint routes next"
- L57      the FINAL REPORT evidence/owner/report triple restated as the three handoff fields
- L59      report path: the loop scratch dir became the request-named path with a workspace-root fallback
- L90      owner column: the `{*-loop engineer}` enumeration (a dev-loops roster) restated as "which engineer"

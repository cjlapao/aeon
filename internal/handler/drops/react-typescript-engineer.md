# react-typescript-engineer — drop record

original: opencode/agents/react-typescript-engineer-loop.md  (84 lines)
rewrite:  aeon/internal/handler/bodies/react-typescript-engineer.md  (39 lines)
delta:    47 lines removed, 2 added
## RITUAL (deleted — the engine does this)
- L39-L41  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L43-L45  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L49      step 0: `pwd` + `loopSlug` + path discipline     → context arrives resolved in the step's request; a body never addresses loop state
- L50      step 1: memory.md + loop-read assembly           → the step carries the goal and the files expected
- L51      step 2: re-read prior failures via `loop_get`    → the transcript is the record; a redo arrives with the previous attempt visible
- L54      step 5: `todowrite` checklist publish            → the `todo` tool carries it (A19 rename), wording otherwise traceable to this line
- L64      step 8: scratch note via `loop_writeFile`        → the handoff summary is what the next step reads (its "component/state boundaries" and "a11y notes" became handoff fields the summary reports)
- L65      step 9: `memory_append` gotchas                  → there is no memory tool; lessons are an operator file, not agent state
- L66      step 10: `event_append` return checkpoint        → the daemon emits step events; a body cannot write its own timeline row
- L67      FINAL REPORT line (half)                         → the handoff is structured, not a prose line; the verification-gate half survives (REWORDED)
- L81      "NEVER mutate loop state" boundary               → the handoff IS the only output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, bash, glob, grep, todo, question]`: task DROPPED (A1, never renamed), todowrite→todo, codesearch/lsp/patch retired (W1), webfetch not re-granted (its only body use was the deferred doc resolution below)
- L23-L32  permission.bash deny table (git/rm rules)        → S11's corpus carries these patterns byte-identically; `aeon policy seed-corpus` turns them into rows
- L80      "One task per turn"                              → the blueprint decides which step runs; a body policing turn count duplicates the scheduler
- L82      "Never run git writes/commits"                   → the corpus denies git writes and the allow-list has no git
- L83-L84  status-blocked + notes vocabulary (half)         → S8's four outcomes + a Reason string; the "Stay in React/TS" and contradictory-criterion halves survive (REWORDED)

## DOMAIN (kept)
- L52      read package.json/framework/router/state layer/component dir/test setup/lint config; match the existing stack; no new styling/state library without a written reason → Working method 1
- L55      strict TS (no any without justification); accessible semantic markup; behavior-focused tests; the ~1000-line cap → Working method 3
- L56-L63  mandatory RTL tests with the ≥80% mandate; the WATCH-mode hang named as the reason commands run bounded and non-watch; typecheck/lint/coverage/build commands command-for-command; dev-server discipline (detached, bounded readiness wait, killed); timeout-is-FAIL → Working method 4
- L69-L76  React/TS craft: the state-layer ladder (useState to TanStack Query to router to global only cross-cutting), hook rules with cleanup, the accessibility list, RTL query rules and msw, client-bundle secrets/sanitization → kept verbatim in substance

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L53      context7 doc resolution (React/Next move fast — RSC, use(), actions) → re-read against S12 per ROADMAP §7: with the S12 client this is no longer fiction — an operator who CONFIGURES a context7 MCP server gets the tools; the body no longer instructs fetching docs from a server an installation may not have. Until then, version questions resolve from the repository's own pinned dependencies — the original's own advice when the repo shows the pattern

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L35-L37  role line shortened to one sentence
- L67      verification-gate rule restated as the report gate with the coverage % as evidence
- L83      "Stay in React/TS frontend" restated with outcome vocabulary: name the right handler and report blocked
- L84      contradictory-criterion restated as a question before coding around it

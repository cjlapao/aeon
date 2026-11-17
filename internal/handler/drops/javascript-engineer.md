# javascript-engineer — drop record

original: opencode/agents/javascript-engineer-loop.md  (83 lines)
rewrite:  aeon/internal/handler/bodies/javascript-engineer.md  (40 lines)
delta:    45 lines removed, 2 added
## RITUAL (deleted — the engine does this)
- L39-L41  caller guard checking who dispatched (and the sibling-routing aside) → the blueprint dispatches; the React/TS vs Vue/TS routing the aside describes is the resolver's, and the specialist boundary survives in the body
- L43-L45  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L49      step 0: `pwd` + `loopSlug` + path discipline     → context arrives resolved in the step's request; a body never addresses loop state
- L50      step 1: `loop_readFile`/`loop_readTask` assembly → the step carries the goal and the files expected
- L51      step 2: re-read prior failures via `loop_get`    → the transcript is the record; a redo arrives with the previous attempt visible
- L63      step 8: scratch note via `loop_writeFile`        → the handoff summary is what the next step reads (its "async shape, error model, ESM/CJS" decisions survive as craft decisions the summary reports)
- L64      step 9: `memory_append` gotchas                  → there is no memory tool; lessons are an operator file, not agent state
- L65      step 10: `event_append` return checkpoint        → the daemon emits step events; a body cannot write its own timeline row
- L66      FINAL REPORT line                                → the handoff is structured (front-matter schema + S8's spec), not a prose line
- L80      "NEVER mutate loop state" boundary               → the handoff IS the only output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, bash, glob, grep, todo, question]`: task DROPPED (A1, never renamed), todowrite→todo, codesearch/lsp/patch retired (W1), webfetch not re-granted (its body use was the deferred doc resolution below)
- L23-L32  permission.bash deny table (git/rm rules)        → S11's corpus carries these patterns byte-identically; `aeon policy seed-corpus` turns them into rows. Command policy is the mechanism, not body prose
- L79      "One task per turn"                              → the blueprint decides which step runs; a body policing turn count duplicates the scheduler
- L81      "Never run git writes/commits"                   → the corpus denies git writes and the allow-list has no git; prose forbidding what a rule refuses is decoration that reads as permission to try
- L82-L83  `status=blocked` + `notes` vocabulary (half)     → S8's four outcomes + a Reason string; the other half of each line survives (see DOMAIN/REWORDED)

## DOMAIN (kept)
- L52      read package.json/module system/build tool/test runner/lint config/targets before designing → Working method 1
- L55      implement only this step; ESM-first, const-not-var, JSDoc, Error subclasses with cause, 1000-line cap, dependency-with-reason, no console.log shipped → Working method 3
- L56-L62  mandatory tests with the repo runner, the coverage requirement, lint/format/run-once-never-watch/build/`node --check`/typecheck, servers detached-and-killed, timeout-as-FAIL with named hang suspects → Working method 4, command for command
- L68-L75  JavaScript craft: ESM/optional-chaining/private fields, async discipline (allSettled, no floating promises, AbortController/Signal.timeout), Error-subclass-and-cause plus Node process handlers, immutability, boundary validation and the security list (crypto, no secrets in bundles, DOM sanitization), streams and workers → kept verbatim in substance

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L53      context7 doc resolution (Node-versus-framework churn guidance) → re-read against S12 per ROADMAP §7: with the S12 client this is no longer fiction — an operator who CONFIGURES a context7 MCP server gets the tools; the body no longer instructs fetching docs from a server an installation may not have. Until doc servers are part of the expected installation, version questions resolve from the repository's own pinned dependencies — the original's own advice when the repo shows the pattern

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L35-L37  role line shortened; the vanilla-JS scope clause carries the sibling-routing boundary instead of an aside
- L66      verification-gate rule restated as the report gate ("Report `done` only when…") with the coverage percentage named as evidence
- L82      "Stay in vanilla JS / Node" restated with outcome vocabulary: name the specialist in the handoff reason and report `blocked`
- L83      contradictory-criterion restated as a `question` before writing code that pretends otherwise

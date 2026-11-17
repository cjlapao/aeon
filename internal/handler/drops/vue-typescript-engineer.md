# vue-typescript-engineer — drop record

original: opencode/agents/vue-typescript-engineer-loop.md  (86 lines)
rewrite:  aeon/internal/handler/bodies/vue-typescript-engineer.md  (41 lines)
delta:    47 lines removed, 2 added
## RITUAL (deleted — the engine does this)
- L39-L41  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L43-L45  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L49      step 0: `pwd` + `loopSlug` + path discipline     → context arrives resolved in the step's request; a body never addresses loop state
- L50      step 1: memory.md + loop-read assembly           → the step carries the goal and the files expected
- L51      step 2: re-read prior failures via `loop_get`    → the transcript is the record; a redo arrives with the previous attempt visible
- L54      step 5: `todowrite` checklist publish            → the `todo` tool carries it (A19 rename), wording otherwise traceable to this line
- L64      step 8: scratch note via `loop_writeFile`        → the handoff summary is what the next step reads (its "component/composable/state boundaries" and "a11y notes" became handoff fields the summary reports; its "context7 was down" note survives as the report-what-you-could-not-verify clause)
- L65      step 9: `memory_append` gotchas                  → there is no memory tool; lessons are an operator file, not agent state
- L66      step 10: `event_append` return checkpoint        → the daemon emits step events; a body cannot write its own timeline row
- L67      FINAL REPORT line (half)                         → the handoff is structured, not a prose line; the verification-gate half survives (REWORDED)
- L83      "NEVER mutate loop state" boundary               → the handoff IS the only output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, bash, glob, grep, todo, question]`: task DROPPED (A1, never renamed), todowrite→todo, codesearch/lsp/patch retired (W1), webfetch not re-granted (its only body use was the deferred doc resolution below)
- L23-L32  permission.bash deny table (git/rm rules)        → S11's corpus carries these patterns byte-identically; `aeon policy seed-corpus` turns them into rows
- L82      "One task per turn"                              → the blueprint decides which step runs; a body policing turn count duplicates the scheduler
- L84      "Never run git writes/commits"                   → the corpus denies git writes and the allow-list has no git
- L85-L86  status-blocked + notes vocabulary (half)         → S8's four outcomes + a Reason string; the "Stay in Vue/TS" and contradictory-criterion halves survive (REWORDED)

## DOMAIN (kept)
- L52      read package.json and the APP SHAPE — Vite SPA versus Nuxt 3/4, auto-imports/server routes/file conventions differ substantially — router, Pinia?, test setup, lint config, vite/nuxt config; match the stack, no new libraries without a written reason → Working method 1
- L55      strict TS; script-setup for all new components; accessible markup; behavior tests; the ~1000-line cap → Working method 3
- L56-L63  mandatory Vitest tests with the ≥80% mandate; the vitest WATCH-mode hang named; vue-tsc --noEmit with the plain-tsc-skips-SFC-templates reason; lint/coverage/build (nuxt build); dev-server discipline; timeout-is-FAIL → Working method 4, command for command
- L69-L78  Vue/TS craft: Composition API only for new code, type-based macros and generics, the reactivity list (never destructure reactive(), toRefs, shallowRef), the state ladder, composables with onScopeDispose, the accessibility list, testing rules, v-html guilty-until-proven-safe → kept verbatim in substance

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L53      context7 doc resolution (Vue moves fast — reactive-props destructure, defineModel, Vapor mode) → re-read against S12 per ROADMAP §7: with the S12 client this is no longer fiction — an operator who CONFIGURES a context7 MCP server gets the tools; the body no longer instructs fetching docs from a server an installation may not have. The line's OTHER half — never block on a docs lookup, proceed and flag uncertainty — is kept (REWORDED); what is deferred is the named server, not the honesty

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L35-L37  role line shortened to one sentence
- L53      (kept half) "do NOT block on a docs lookup — note it, write the most modern patterns you know, flag APIs you are less sure of" restated inside the report gate as state-plainly-what-you-could-not-verify
- L67      verification-gate rule restated as the report gate with the coverage % as evidence
- L85      "Stay in Vue/TS frontend" restated with outcome vocabulary, keeping the original's routing aside (Tauri work to the Tauri+Vue handler, React work to the React one)
- L86      contradictory-criterion restated as a question before quietly reinterpreting it

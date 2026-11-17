# tauri-vue-engineer — drop record

original: opencode/agents/tauri-vue-engineer-loop.md  (92 lines)
rewrite:  aeon/internal/handler/bodies/tauri-vue-engineer.md  (46 lines)
delta:    48 lines removed, 2 added
## RITUAL (deleted — the engine does this)
- L46-L48  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L50-L52  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L56      step 0: `pwd` + `loopSlug` + path discipline     → context arrives resolved in the step's request; a body never addresses loop state
- L57      step 1: memory.md + loop-read assembly           → the step carries the goal and the files expected
- L58      step 2: re-read prior failures via `loop_get`    → the transcript is the record; a redo arrives with the previous attempt visible
- L61      step 5: `todowrite` checklist publish            → the `todo` tool carries it (A19 rename), wording otherwise traceable to this line
- L73      step 8: scratch note via `loop_writeFile`        → the handoff summary is what the next step reads — and its content (the IPC contract, capabilities touched) became the handoff fields themselves, because on this handler the contract IS the reviewable artifact
- L74      step 9: `memory_append` gotchas                  → there is no memory tool; lessons are an operator file, not agent state
- L75      step 10: `event_append` return checkpoint        → the daemon emits step events; a body cannot write its own timeline row
- L76      FINAL REPORT line (half)                         → the handoff is structured, not a prose line; the per-layer gate and the dev-build-only honesty clause survive (REWORDED/DOMAIN)
- L89      "NEVER mutate loop state" boundary               → the handoff IS the only output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, bash, glob, grep, todo, question]`: task DROPPED (A1, never renamed), todowrite→todo, codesearch/lsp/patch retired (W1), webfetch not re-granted (its only body use was the deferred doc resolution below)
- L23-L32  permission.bash deny table (git/rm rules)        → S11's corpus carries these patterns byte-identically; `aeon policy seed-corpus` turns them into rows
- L88      "One task per turn"                              → the blueprint decides which step runs; a body policing turn count duplicates the scheduler
- L90      "Never run git writes/commits"                   → the corpus denies git writes and the allow-list has no git
- L91-L92  status-blocked vocabulary (half)                 → S8's four outcomes + a Reason string; the "Stay in the desktop domain" and missing-bundle halves survive (REWORDED/DOMAIN)

## DOMAIN (kept)
- L39-L43  the STACK MANDATE nearly verbatim: always Tauri v2 never v1 with the named incompatibilities; the framework monopoly; verify Cargo.toml/package.json versions before coding; never silently write old-style code — report it so the operator decides on migration → Stack mandate, bullets 1-3
- L59      read tauri.conf.json, Cargo.toml, capabilities/permissions, the command handlers, package.json, and the invoke shape before designing → Working method 1
- L62-L66  implement only this step across layers: Rust command handlers with no unwrap on fallible IPC paths, minimum permission in the right capability file, typed invoke + strict Vue, 1000-line cap split by layer → Working method 3
- L67-L72  mandatory tests on BOTH layers with per-layer ≥80%; the never-exit commands named as loop-hangers; the frontend and Rust command sets command-for-command; the bundle rule: build it when the environment allows, say so when it does not, and done covers only what was actually run → Working method 4
- L78-L84  craft: IPC as a typed contract documented for the verifier, least-privilege capabilities with the "**"-scope prohibition, structured serde errors not panics, work off the main thread with progress events → kept verbatim in substance

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L44/L60  context7 doc resolution (Tauri v2 + Vue 3.5+ config/capabilities/IPC; v1-and-Options-from-memory are the hard-to-spot failures) → re-read against S12 per ROADMAP §7: with the S12 client this is no longer fiction — an operator who CONFIGURES a context7 MCP server gets the tools; the body no longer instructs fetching docs from a server an installation may not have. L44's OTHER half — never block on a docs lookup, note it, flag what you are less sure of — is KEPT in the Stack mandate (its own REWORDED line); what is deferred is the named server, not the honesty

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L35-L37  role line shortened to one sentence
- L76      the two-gate rule restated: per-layer coverage as the condition, the dev-build clause as "done covers only what you actually ran"
- L44      (kept half) do-NOT-block-on-a-docs-lookup, note it, write the most modern v2/Vue-3 patterns, flag anything less sure — restated inside the Stack mandate without the named server
- L91      "Stay in the Vue desktop domain" restated with outcome vocabulary, keeping the original's routing aside (a React frontend belongs to the Tauri+React handler); name who should own it and report blocked

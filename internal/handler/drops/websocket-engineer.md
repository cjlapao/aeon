# websocket-engineer — drop record

original: opencode/agents/websocket-engineer-loop.md  (82 lines)
rewrite:  aeon/internal/handler/bodies/websocket-engineer.md  (39 lines)
delta:    45 lines removed, 2 added
## RITUAL (deleted — the engine does this)
- L39-L41  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L43-L45  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L49      step 0: `pwd` + `loopSlug` + path discipline     → context arrives resolved in the step's request; a body never addresses loop state
- L50      step 1: `loop_readFile`/`loop_readTask` assembly → the step carries the goal and the files expected
- L51      step 2: re-read prior failures via `loop_get`    → the transcript is the record; a redo arrives with the previous attempt visible
- L54      step 5: `todowrite` checklist publish            → the `todo` tool carries it (A19 rename), wording otherwise traceable to this line
- L62      step 8: scratch note via `loop_writeFile`        → the handoff summary is what the next step reads (its "wire format, auth flow, scale strategy, delivery semantics, backpressure, load numbers" became handoff fields the summary reports)
- L63      step 9: `memory_append` gotchas                  → there is no memory tool; lessons are an operator file, not agent state
- L64      step 10: `event_append` return checkpoint        → the daemon emits step events; a body cannot write its own timeline row
- L65      FINAL REPORT line (half)                         → the handoff is structured, not a prose line; the verification-gate half survives (REWORDED)
- L79      "NEVER mutate loop state" boundary               → the handoff IS the only output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, bash, glob, grep, todo, question]`: task DROPPED (A1, never renamed), todowrite→todo, codesearch/lsp/patch retired (W1), webfetch not re-granted (its only body use was the deferred doc resolution below)
- L23-L32  permission.bash deny table (git/rm rules)        → S11's corpus carries these patterns byte-identically; `aeon policy seed-corpus` turns them into rows
- L78      "One task per turn"                              → the blueprint decides which step runs; a body policing turn count duplicates the scheduler
- L80      "Never run git writes/commits"                   → the corpus denies git writes and the allow-list has no git
- L81-L82  status-blocked + notes vocabulary (half)         → S8's four outcomes + a Reason string; the "Stay in the real-time layer" and contradictory-criterion halves survive (REWORDED)

## DOMAIN (kept)
- L52      read the server language/framework (the named library list), broker, LB/proxy, auth shape, client lib, test setup before designing → Working method 1
- L55      the real-time checklist verbatim: auth at the handshake, Origin/Host check on upgrade, versioned wire format, heartbeats with dead-peer timeout, BOUNDED per-connection outbound queues, graceful drain, documented close codes → Working method 3
- L56-L61  mandatory tests bounded with servers detached and ALWAYS killed (hung socket / squatted port named); ≥80% coverage; the reconnect-after-forced-drop integration test; the small load check for fan-out with connections/msgs-per-sec/percentiles/memory; run-once lint/format/test; timeout-is-FAIL with the named suspects → Working method 4
- L67-L74  real-time craft: protocol choice with a reason, auth-before-upgrade with separate token refresh, sticky-or-broker with documented sharding key, delivery semantics said loudly, backpressure with close codes, client state machine with jittered backoff and unmount teardown → kept verbatim in substance

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L53      context7 doc resolution (the WS/SSE library or broker client at the pinned version) → re-read against S12 per ROADMAP §7: with the S12 client this is no longer fiction — an operator who CONFIGURES a context7 MCP server gets the tools; the body no longer instructs fetching docs from a server an installation may not have. Until then, version questions resolve from the repository's own pinned libraries — the original's own advice when the repo shows the pattern

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L35-L37  role line shortened to one sentence
- L65      verification-gate rule restated as the report gate, with the fan-out load evidence kept as a hard condition and the at-most-once default said out loud
- L81      "Stay in the real-time layer" restated with outcome vocabulary: name who should own it and report blocked
- L82      contradictory-criterion restated as a question first, "do not silently reinterpret" carried across

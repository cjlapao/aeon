# qa-engineer — drop record

original: opencode/agents/qa-engineer-loop.md  (293 lines)
rewrite:  aeon/internal/handler/bodies/qa-engineer.md  (250 lines)
delta:    44 lines removed, 1 added

## RITUAL (deleted — the engine does this)
- L63-L65  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L67-L69  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L73      step 0: `pwd` + `loopSlug` + path discipline     → context arrives resolved in the step's request; a body never addresses loop state
- L74      step 1: loop_readFile(plan/discovery) + loop_get → the step carries the goal; the reads that SURVIVE (existing suites, merged code, AGENTS.md) are folded into Working method 1
- L92      step 8: `memory_append` QA summary               → there is no memory tool; lessons are an operator file, not agent state
- L93      step 9: `event_append` return checkpoint         → the daemon emits step events; a body cannot write its own timeline row
- L94-L99  FINAL REPORT lines (half)                        → the handoff is structured; the verdict + evidence halves survive in verdict/summary (REWORDED)
- L290     "You NEVER mutate loop state" boundary           → the handoff IS the only output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, bash, glob, grep, todo, question]`: task DROPPED (A1, never renamed), todowrite→todo, codesearch/lsp/patch retired (W1), webfetch not re-granted (no body instruction uses it)
- L22-L33  permission.bash deny table (git/rm rules)        → S11's corpus carries these patterns byte-identically; `aeon policy seed-corpus` turns them into rows
- L76-L79  the pre-gate COMMANDS (timeout 600 go build / go test -timeout 300s / CI=true npm test) → still commands, still in the body verbatim — what became mechanism is the RECORDING of the gate's result (handoff verdict field)
- L90      defect routing "so project-planner-loop can turn it into a QAT task" → routing is the blueprint's; the defect shape (stable DEFECT-NNN id, repro, owner, severity) is kept verbatim because the fix step consumes it
- L291     "Never run git writes" + absolute-$ROOT paths + "the filesystem MCP is disabled for you" → the corpus denies git writes; workspace-relative paths are the toolset's boundary; the MCP disablement note goes with S11

## DOMAIN (kept)
- L38      the role: hard-gate first, then prove the feature end-to-end with real data; never fix code → role line (the QAT-task half is routing above)
- L40-L43  the two documents: suites are long-lived curated PLANS reflecting the CURRENT software (add/amend/remove, README index, curate ONLY on PASS); the run report is the per-run hard copy, timestamped, never overwritten, whose defect list is the routed input → "Two documents you work with", kept in full
- L45-L52  the `.secrets` discipline, all four clauses verbatim in substance: values in live requests ONLY and never in anything written; reference by NAME; redact echoed tokens to `<redacted>`; source-in the same bash invocation, never paste into recorded commands; missing key NAME → blocked; no file → blocked with the names the user must provide → Authentication section, and its L292 boundary twin
- L58-L60  the HTTP fallback: curl/fetch the route with real payloads and `.secrets` auth, assert status/body/markup; record the downgrade honestly as `pass (HTTP fallback — ... rendered-UI behavior not verified)`; rendered-UI expectations CANNOT pass via fallback → `blocked`, never green-washed → UI-cases section (its Playwright half is DEFERRED) + the L293 boundary verbatim
- L75-L79  the HARD PRE-GATE: any failing test is a QA FAIL even in an untouched package; pre-existing failure still recorded; red gate → report all failures as defects, verdict FAIL, no executing, no curating, stop → Working method 3, bullet for bullet
- L81-L85  execution discipline: DETACHED starts (`docker compose up -d` never attached, `echo $!`), bounded readiness ≤60s with log-tail blocked, real payloads/auth/DB/CLI with captured evidence, real data not mocks, ALWAYS tear down — orphaned apps squat ports and poison the next run → Working method 4
- L86-L89  the verdict rule, NO green-washing: code-inspection is never a pass for what needs execution; essential flow fail/blocked → whole-run FAIL; missing bootstrap is a DEFECT not a pass excuse; PASS = green gate + executed essentials + no green-wash → Working method 5 + boundaries
- L91      curate on PASS ONLY, reflecting current software, professionally followable → Working method 7
- L101-L250 the test-suite schema verbatim (it is SHARED with qa-test-spec-loop in BOTH originals — L107 says so); the stable-TC-ID rule (append at next free number, edit in place, mark `removed — {why}`, never renumber or delete); the observed-values clause ("you actually ran these cases — record real observed values, not the ones the code implies", L237); the README index + curation rules (L239-L250) → the schema sections, template intact
- L252-L282 the run-report schema — pre-gate table, results table, defect entries with repro/owner/severity, summary verdict → kept (loop-path header adapted to the step's report path — half REWORDED)

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L54-L61  "UI cases — Playwright MCP first, HTTP fallback second": driving browser-level cases through the Playwright MCP (navigate, interact, assert rendered state), its flakiness clause and one-retry rule → re-read against S12 per ROADMAP §7: with the S12 MCP client this is no longer fiction — an operator who CONFIGURES a Playwright MCP server gets browser automation; the body no longer instructs calls to a server an installation may not have. THE SPLIT (A18's exact judgement): the browser half is deferred; the HTTP fallback is keepable DOMAIN (kept, above) — and the fallback's honesty rule ("never green-wash a UI case through the HTTP fallback") is kept twice, in the section and the boundary. The body carries no Playwright instruction at all — deferred means OUT of the body, recorded here
- L291     (also) "the filesystem MCP is disabled for you" → MCP-shaped config note; rides with the deferral, its intent lives in the tool boundary

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L3/L38   role line shortened; "after all tasks are built, verified, reviewed, and merged" restated as the step's own position
- L90      run-report destination: the loop-dir path became the step-named report path (`qa-report-{YYYYMMDD-HHMM}.md`) — same timestamped-never-overwritten discipline
- L95-L99  the three FINAL REPORT shapes restated in outcome vocabulary: fail → rejected handoff with the defect summary; pass → ok with N/M + curated suites; cannot-run → blocked naming what's missing
- L107     the shared-schema note restated without the sibling's -loop suffix (registry names, not MCP agent names)

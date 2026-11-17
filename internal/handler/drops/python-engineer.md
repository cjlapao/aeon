# python-engineer — drop record

original: opencode/agents/python-engineer-loop.md  (84 lines)
rewrite:  aeon/internal/handler/bodies/python-engineer.md  (40 lines)
delta:    46 lines removed, 2 added
## RITUAL (deleted — the engine does this)
- L39-L41  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L43-L45  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L49      step 0: `pwd` + `loopSlug` + path discipline     → context arrives resolved in the step's request; a body never addresses loop state
- L50      step 1: `loop_readFile`/`loop_readTask` assembly → the step carries the goal and the files expected
- L51      step 2: re-read prior failures via `loop_get`    → the transcript is the record; a redo arrives with the previous attempt visible
- L64      step 8: scratch note via `loop_writeFile`        → the handoff summary is what the next step reads (its "key decisions async vs sync, Protocol vs ABC, dataclass vs Pydantic" survive as craft decisions the summary reports)
- L65      step 9: `memory_append` gotchas                  → there is no memory tool; lessons are an operator file, not agent state
- L66      step 10: `event_append` return checkpoint        → the daemon emits step events; a body cannot write its own timeline row
- L67      FINAL REPORT line                                → the handoff is structured (front-matter schema + S8's spec), not a prose line
- L81      "NEVER mutate loop state" boundary               → the handoff IS the only output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, bash, glob, grep, todo, question]`: task DROPPED (A1, never renamed), todowrite→todo, codesearch/lsp/patch retired (W1), webfetch not re-granted (its body use was the deferred doc resolution below)
- L23-L32  permission.bash deny table (git/rm rules)        → S11's corpus carries these patterns byte-identically; `aeon policy seed-corpus` turns them into rows. Command policy is the mechanism, not body prose
- L80      "One task per turn"                              → the blueprint decides which step runs; a body policing turn count duplicates the scheduler
- L82      "Never run git writes/commits"                   → the corpus denies git writes and the allow-list has no git; prose forbidding what a rule refuses is decoration that reads as permission to try
- L83-L84  `status=blocked` + `notes` vocabulary (half)     → S8's four outcomes + a Reason string; the other half of each line survives (see DOMAIN/REWORDED)

## DOMAIN (kept)
- L52      read pyproject/dependency manager/layout/pytest/ruff/mypy config before designing → Working method 1
- L55      implement only this step; type hints; custom exceptions; 1000-line cap; dependency-with-reason → Working method 3 and craft
- L56-L63  mandatory parametrized pytest, the coverage measurement line, ruff/black/mypy/bandit, bounded `timeout 300 pytest -q`, timeout-as-FAIL with named hang suspects → Working method 4, command for command
- L69-L76  Python craft: dataclass/Pydantic/Protocol split, asyncio discipline (TaskGroup, timeout, to_thread), context managers, parameterized SQL, secrets, no mutable defaults, logging shape → kept verbatim in substance

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L53      context7 doc resolution (FastAPI/Pydantic v2/SQLAlchemy 2.x examples) → re-read against S12 per ROADMAP §7: with the S12 client this is no longer fiction — an operator who CONFIGURES a context7 MCP server gets the tools; the body no longer instructs fetching docs from a server an installation may not have. Until doc servers are part of the expected installation, version questions resolve from the repository's own pinned code — the original's own advice when the repo shows the pattern

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L35-L37  role line shortened to one sentence
- L67      verification-gate rule restated as the report gate ("Report `done` only when…") with the coverage percentage named as evidence
- L83      "Stay in Python" restated with outcome vocabulary: name the specialist in the handoff reason and report `blocked`
- L84      contradictory-criterion restated as a `question` before writing code that pretends otherwise

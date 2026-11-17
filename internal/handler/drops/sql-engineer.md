# sql-engineer — drop record

original: opencode/agents/sql-engineer-loop.md  (82 lines)
rewrite:  aeon/internal/handler/bodies/sql-engineer.md  (40 lines)
delta:    44 lines removed, 2 added
## RITUAL (deleted — the engine does this)
- L39-L41  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L43-L45  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L49      step 0: `pwd` + `loopSlug` + path discipline     → context arrives resolved in the step's request; a body never addresses loop state
- L50      step 1: `loop_readFile`/`loop_readTask` assembly → the step carries the goal and the files expected
- L51      step 2: re-read prior failures via `loop_get`    → the transcript is the record; a redo arrives with the previous attempt visible
- L54      step 5: `todowrite` checklist publish            → the `todo` tool carries it (A19 rename), wording otherwise traceable to this line
- L62      step 8: scratch note via `loop_writeFile`        → the handoff summary is what the next step reads (its "index strategy, lock implications, backfill + rollback plan" survive as handoff content the summary reports)
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
- L81      status-blocked + notes vocabulary (half)         → S8's four outcomes + a Reason string; the "Stay in SQL" half survives (REWORDED)

## DOMAIN (kept)
- L52      read the DB platform/version, migration tool, ORM, schema+indexes+history, dev DB setup, seed conventions before designing → Working method 1
- L55      one logical change per migration; additive-first; FK indexed child-side; CONCURRENTLY/online DDL; no SELECT *; parameterized only → Working method 3
- L56-L61  bounded verification with the DB detached and torn down; up/down reversibility; EXPLAIN on every new/changed query with the plan as evidence; the ≥80% mandate on application-level query logic with the pure-DDL exemption named; squawk/sqlfluff; destructive changes rehearsed on production-sized data or documented with dry-run + blast radius → Working method 4, bullet for bullet
- L67-L74  SQL craft: index-for-the-query, set-based thinking, UTC timestamps and constraints, transaction/lock/isolation discipline, ORM-generated-SQL scrutiny, least-privilege roles and RLS → kept verbatim in substance
- L82      "Never run destructive SQL against a shared/production DB" — the irreversibility judgement this handler exists for → Working method 4, last bullet

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L53      context7 doc resolution (migration-tool and DB-feature syntax) → re-read against S12 per ROADMAP §7: with the S12 client this is no longer fiction — an operator who CONFIGURES a context7 MCP server gets the tools; the body no longer instructs fetching docs from a server an installation may not have. Until doc servers are part of the expected installation, syntax questions resolve from the repository's own migrations — the original's own advice when they already show the pattern

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L35-L37  role line shortened to one sentence
- L65      verification-gate rule restated as the report gate with the plan headline as evidence
- L81      "Stay in SQL / schema / migrations" restated with outcome vocabulary: name the specialist in the handoff reason and report blocked

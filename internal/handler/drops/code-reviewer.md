# code-reviewer — drop record

original: opencode/agents/code-reviewer-loop.md  (204 lines)
rewrite:  aeon/internal/handler/bodies/code-reviewer.md  (53 lines)
delta:    151 lines removed, 0 added

## RITUAL (deleted — the engine does this)
- L49-L51  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L71-L73  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L77      step 1: loop_readFile/loop_readTask/scratch reads + loop_get → context arrives resolved in the step's request; the transcript is the record
- L79-L81  step 1b: `task_setStatus in-review` position marker (and its skip-in-feature-review warning) → the daemon emits step events; a body cannot and need not write a position row — the gap it patched (an illegal state jump stranding a task) cannot exist here
- L115     step 7: scratch note via `loop_writeFile`        → the handoff findings ARE the reviewable artifact; the next step reads the handoff, not a scratch file
- L120     step 9: `memory_append` review note              → there is no memory tool; lessons are an operator file, not agent state
- L126     step 10: `event_append` return checkpoint        → the daemon emits step events; a body cannot write its own timeline row
- L127-L135 FINAL REPORT lines (half)                       → the handoff is structured; the verdict-carrying half survives in the handoff fields (REWORDED)
- L201     "You NEVER mutate loop state" boundary           → the handoff IS the only output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L25   tools map with true/false entries incl. `github_*: false` → allow-list `[read, bash, glob, grep, todo, question]`: task DROPPED (A1, never renamed), todowrite→todo, codesearch/lsp/patch retired (W1); write/edit withheld — the reviewer's findings are its only output and a reviewer that can edit the diff is not a reviewer; webfetch not re-granted
- L26-L42  permission.bash deny table (git writes; `gh` DENIED outright) → S11's corpus carries these patterns byte-identically (`gh` denies included); `aeon policy seed-corpus` turns them into rows
- L82      step 2: `todowrite` checklist publish            → the `todo` tool carries it (A19 rename), wording otherwise traceable to this line
- L121-L124 step 9b: recording verdict STATE via `task_setStatus` (changes-requested routes the fix loop; approve leaves in-review; "check the tool result") → the handoff outcome is the verdict and the blueprint routes it (L4/T3); the durable record is the daemon's, written once

## DOMAIN (kept)
- L53-L69  the two-modes table + "Why feature-review exists": the feature review is a real review, not a formality; re-litigating landed tasks blocks a ready feature on taste → the two-review-scopes section, bar per scope
- L94      read the changed files in FULL, not just the diff, for pattern/caller context → Working method 3
- L95-L104 the systematic review dimensions nearly bullet-for-bullet: success criteria as the primary bar; security/correctness/performance/maintainability/tests lists; the Completeness rule with the demo/safelist/companion examples and the "work left uncommitted" reading; CI green with no-checks-configured as N/A → Working method 4
- L97      the feature-level lens verbatim in substance: integration seams, contradictions between tasks, orphans (used-by-none, introduced-by-none), un-tasked changes; do NOT re-review per-task internals → Working method 4, feature-review bullets
- L163     "Every finding includes file:line and an exact fix — never 'improve this.'" → Working method 5
- L167-L177 the verdict table with the same thresholds: any critical → reject; 2+ important → reject; 1 important + green + criteria met → approve noted; suggestions never block → Working method 5
- L179-L190 the feature-review amendments: deferred findings are not findings, landed-task internals are not findings, cross-task break/contradiction/orphan/objective-not-delivered ARE findings; a feature reject is expensive — block on wrong, never on taste → Working method 5 + 6
- L198     "Never merge" — approving is not merging → Hard boundaries
- L203     "No rubber-stamping" — could not read the diff or run checks → blocked, never pass → Hard boundaries
- L204     redact secrets in all findings → Hard boundaries

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L83-L93  reading the PR through the `github-reviewer` MCP: two-identity rule, discover-by-function, the resource-probing warning, tool-discovery retry, base/head confirmation via `loop_get` → re-read against S12 per ROADMAP §7: with the S12 MCP client this is no longer fiction — an operator who CONFIGURES a github-reviewer MCP server gets those tools; the body no longer instructs calls to a server an installation may not have. The review method survives reading the change itself (git diff via `bash`, files via `read`); the wrong-base rule survives re-expressed as "the change handed to you is not the change the step describes — blocked naming what you see"
- L105-L114 submitting the verdict AS the reviewer identity via the MCP (one call, body with all findings), optional thread resolution with PRRT ids, `unresolved-pr-comments.md` as the durable deferral record → same ruling; the verdict is now the handoff outcome (findings field carries everything the review body carried), and deferral records are the blueprint's concern, not a body's
- L196-L199 the identity boundary (gh never, discover-by-function, no resource probing, blocked naming the missing capability) → the gh/corpus half is mechanism above; the identity half rides with the MCP above

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L3/L47   role line shortened to one sentence; GitHub submission verb gone with its deferral
- L63/L64 the mode table's bar column restated without plan.md path names (the step's request names the bar)
- L128-L131 the four FINAL REPORT shapes restated as handoff fields: verdict in the outcome, findings with severity, blocked naming what was missing
- L135     "say explicitly whether the PR is now mergeable" restated as the scope line saying what the approval does and does not assert
- L174     "PR targets main / wrong branch → blocked — engineer can't retarget" restated generically: mismatch is a routing problem the engineer cannot fix

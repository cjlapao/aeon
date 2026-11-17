# task-coordinator — drop record

original: opencode/agents/task-coordinator.md  (632 lines)
rewrite:  aeon/internal/handler/bodies/task-coordinator.md  (141 lines)
delta:    491 lines removed, 0 added

## RITUAL (deleted — the engine does this)

- L36-L42  "A delegation is a `task` TOOL CALL — never chat text" (the platform's most-emphasised rule, with its failure story "you just output the request") → the blueprint dispatches; a model step produces a handoff and the engine runs the next lane. The delegation-cannot-be-narration failure this rule polices cannot be committed here: there is no tool call to forget. The handoff's `brief` field is the Brief, handed to a lane, not printed (L41's CONTENT survives as DOMAIN).
- L48-L51  "You run on top of the `development-map` skill" → nothing loads skills; the invocation shapes the skill defined are the two blueprints themselves (`development-map-pipeline`, `development-map-direct`).
- L55-L59  SYSTEM_CHECK initialization phrase ("Do no other work until that phrase is emitted") → session incantation; the step's request arrives with its context resolved (project-planner drop record L41's reasoning).
- L61-L65  POST-COMPACTION RESET phrase ("compaction can drop frontmatter tool restrictions — the reset restores them") → an opencode platform workaround for a bug Aeon does not have: tools are rebuilt per step from the registry grant (tools.BuildToolset), never from prose that survives compaction. The boundary TEXT it restored survives as the body's opening Boundaries (DOMAIN), so the compaction defence is the body itself.
- L77-L92  FINAL REPORT terminator line (`status={triaged|delegated|...}` six-valued, detectable "when opencode drops middle-of-response text") → the handoff is structured (T3); the four triage outcomes and the waiting/advanced/blocked lifecycle become lane state and handoff fields. `status=delegated` specifically meant "a task call happened this turn" — the engine's dispatch is that fact now.
- L130, L147  hotfix-protocol skill switch → refused by the guard (skill findings measured in the probe); recorded as a real gap: `hotfix.yaml` does not exist anywhere (S16 QA already records QAC-10 OWED — the hotfix blueprint is still owed, and the coordinator without it matches the shipped file, which says nothing about hotfixes).
- L237-L344  Pipeline Workflow's 10-step table + every delegation template (Feature Issue, Task Issue, labels, feature branch, PR per task, merge ladder) → the blueprint's lanes dispatch these; `forge.openIssue`, `forge.openPR`, `forge.mergeBranch`, `forge.postReview` are engine actions with effect classes (S16), and task state lives in `sets:` state rows. L254-L344's templates are the CONTENT of those actions' params, carried by the blueprints' producers (feature.md/tasks/), not this prose.
- L470-L481  specialist roster tables (43 names incl. `github-steward`, `architect-reviewer`, `api-designer`, `ux-designer`, `scrum-master`, `business-analyst`…) → the shipped registry is the roster: 25 bodies + `by-tech-tag` + the follow-up five. Names with no Aeon body route nowhere; "report that to the user, never self-handle" (L481, L608) survives.
- L32  "NEVER delegate to `*-loop` agents" (14-name exclusion list for the closed-loop orchestrator) → there is no second orchestrator here; those loop bodies are the shipped 25's originals. The exclusion existed to keep two opencode platforms apart and died with the platform.
- L543-L551  parallel-vs-sequential dispatch table → dispatch order is the blueprint's (lanes, items, depends); the judgement survives implicitly — Briefs name dependencies and the "when in doubt, sequential" caution is the engine's default lane shape.
- L620  "Running `git` commands yourself — `github-steward` owns git" → bash is withheld; git is engine actions (S16); github-steward has no Aeon body because `forge.*` replaced the agent (S16's registry flip).
- L630-L632  PROJECT.md compatibility note → opencode-side artifact.

## BECAME MECHANISM (deleted — a tool, a gate, or a row guarantees it)

- L4-L6  `mode: primary`, `variant: coder-thinking`, temperature 0 → A1 removed spawning; variant measured to have zero consumers (Q2); temperature ports unchanged (0) — the pin (TestTemperaturePortsUnchangedOnRewrites) enforces it; the earlier draft of this record claimed a re-settle to 0.2 and the pin refused the claim as it refused the file. `mode: primary` and `variant` per A1/Q2.
- L7-L19  tools map (write/edit false, `patch: true # NEVER set false` — the OpenCode permission-merge trap, `task: true`, `todowrite`, `codesearch`) → allow-list `[read, glob, grep, question, todo]`. `task` RETIRED (dispatch is the blueprint); `patch`/`codesearch` retired (W1); todowrite→todo; write/edit withheld exactly as the original withheld them; bash withheld as the original had it (L28 "You NEVER run git, build, test, or lint commands").
- L103  `gh issue list --label in-progress/ready-for-review` as the Issue signal for continuation → loop and forge reads answer "is work in flight"; the KEYWORD signal (L102: "still broken", "same file", "after commit X") survives as DOMAIN in Triage's continuation check.
- L505  `gh issue view {N} --json labels` verification, L537-L541's Issue-based-handoff recovery special case → artifact verification of the same shape survives (files exist, deliverable is there — check with read/glob); the GitHub-specific probe is forge/sets state (S16/S17).
- L513-L531  degraded-delegation escalation via the `question` tool with six canned options → the `question` tool is registered; the body keeps the option-shape judgement (retry/narrower/switch/abandon + the "Other — I'll type it" rule) without the platform-specific option text.
- L555  "an inline text question reaches no prompt and blocks nothing" → the `question` tool renders the blocking prompt (S5/S7 machinery); the WHY survives verbatim in spirit in Asking-the-human.

## DOMAIN (kept)

- L23-L34  all six CRITICAL BOUNDARIES, nearly whole: never write/patch/ create (Briefs inline, L27), never run commands (L28), never self-implement on degraded delegation (L29/L74), never advance without evidence (L30/L70), never cross a specialist's domain (L31/L71) → Boundaries.
- L94-L152  four-mode triage, the decision table (L142-L150's examples kept close to verbatim), "When in doubt, start Direct — pipeline is expensive to unwind" (L152), continuation-checks-first (L98-L105), question mode stays read-only and is never pushed into implementation (L132-L136) → Triage.
- L158-L168  the five debugger-first triggers (symptom-only, spans layers, intermittent, prior fix did not hold, multiple hypotheses) and the clear-hypothesis skip → Deciding-on-an-investigation.
- L198-L214  the Brief contract: every field of L202-L212; "identifiers pinned from the REAL code (you READ it — never guess)" (L208); the quality gate "do NOT dispatch a thin Brief" with its full checklist and the re-derive/diverge rationale (L214) → The Brief.
- L220-L231  relay files/verification/surprises; the Docs-worthy-change offer routed to the technical-writer, append-only sections, never a wholesale rewrite, human may decline (L231) → Continuation tail.
- L487-L535  Degraded Delegation whole: the failure shapes (L489-L495), detect-by-checking-files-exist-with-glob/read (L503-L504), retry-once- narrower citing the previous failure (L511), stop-asking-never-third- retry (L513), the relay note naming the recovery (L533), and L535's "Even if the task is small, even if you could just do it" — that sentence is why this protocol exists.
- L537-L541  artifact-survived-text-lost → verify the artifact, advance, say the report degraded.
- L553-L603  asking-the-human discipline: tool-not-prose (L555), the shape (3-5 options, one-line explanations, always "Other — I'll type it", L559-L563), the use/don't-use lists (L567-L579), pacing (one at a time, chain them, L583-L585) → Asking the human.
- L605-L609  ambiguous domain → one targeted question, never guess; out-of-coverage → report closest matches, never self-handle.
- L611-L628  the Common Mistakes table: every row is a restatement of a rule kept above (thin Briefs, skipping investigation, treating follow-ups as new, mid-fix specialist switches, advancing without evidence); its git/PR/Issue-label rows ride with the RITUAL entries above.

## DEFERRED (kept OUT — the capability is not in the body, and why)

- L103, L505, L537-L541  the GitHub-Issue state machine as delegation proof (`gh issue list` continuation signal, `gh issue view --json labels` verification, Issue-survived-text-lost recovery) → the loop-state MCP is the deferred capability (S12's ruling, ROADMAP §7: an operator who CONFIGURES such a server gets its refusals); the Aeon substitutes are `sets:` state rows for task state and the handoff schema for proof — artifact-existence survives in the body (verify with read/glob), and the truth-judging a server could do is stated as a gap, not papered.
- L130, L147  the hotfix-protocol path → no skill porting (guard), and no hotfix blueprint exists: QAC-10 is OWED (S16 QA), the body therefore says nothing about hotfixes rather than promising a lane the engine cannot run.
- L470-L481  roster names with no Aeon body (`architect-reviewer`, `api-designer`, `ux-designer`, `ux-researcher`, `performance-engineer`, `test-automator`, `qa-expert`, `business-analyst`, `scrum-master`, `legal-advisor` ships but `business-analyst` does not, etc.) → each is a future follow-up or `by-tech-tag` work; the body's rule covers today: route to what the registry has, otherwise report closest matches, never self-handle.

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)

- L142-L150  the decision-table examples restated in Aeon's lane vocabulary (no GitHub-Issue column); classifications unchanged.
- L86-L91  the six FINAL REPORT statuses → the four handoff outcomes plus lane state: "delegated/advanced" became "the engine dispatched; the handoff carries the fact", the classification semantics (`triaged` with a question in flight → the `question` tool) kept verbatim in spirit in Asking-the-human.
- L511-L531  the escalation option list trimmed of platform wording ("Platform glitch", session-id mechanics); the four durable options and the Other-always rule survive.

## NIL (recorded, not empty)

- No corpus block: `task-coordinator` is absent from `internal/policy/testdata/corpus.json`'s 38 agents (measured by name, 2026-10-26) — the body declares no `policy-agent` because there is no block to bind; nil is recorded here as a finding, not written as an omission. (proposal-author and brief-author DO have blocks — their records bind; these three do not.)
- L11's permission.bash deny-table rows have no corpus twin for this name; with bash withheld the door they guarded is not opened.
- `hotfix-protocol` (L130): the skill cannot be ported (guard) and no hotfix blueprint exists — the gap rides in QA (QAC-10 OWED), not in silent body prose claiming a path the engine cannot run.

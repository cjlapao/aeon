# qa-test-spec — drop record

original: opencode/agents/qa-test-spec-loop.md  (287 lines)
rewrite:  aeon/internal/handler/bodies/qa-test-spec.md  (220 lines)
delta:    67 lines removed, 0 added

## RITUAL (deleted — the engine does this)
- L58-L60  caller guard checking who dispatched (phase qa-specs) → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L62-L64  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L74      step 0: `pwd` + `loopSlug` + path discipline     → context arrives resolved in the step's request; a body never addresses loop state
- L77-L78  steps 1a-1b: `loop_readFile(plan)` + `loop_readTask` per task → the step's request carries the task specs (their role — "the closest thing you have to a written contract" — survives in Working method 1)
- L79      step 1c: engineers' scratch notes                → the request points at the notes when there are any; "what they actually built, including the parts that diverged" survives there
- L109-L115 step 8: `event_append` checkpoint + FINAL REPORT line (half) → the daemon emits step events; the structured handoff replaces the prose line; the pass/fail semantics ("written and self-checked" / "could not determine what the loop touched") survive in self_check (REWORDED)
- L283     "Never mutate loop state" boundary               → the handoff IS the only output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L19-L37  tools map with true/false entries incl. the MCP disablements (`playwright_*`, `memory_*`, `postgresql_*`, `github_*`) → allow-list `[read, write, edit, bash, glob, grep, todo, question]`: task DROPPED (A1, never renamed), todowrite→todo, codesearch/lsp/patch retired (W1); webfetch stays false as the original had it; MCP disablement notes go with S11/S12
- L38-L49  permission.bash deny table (git writes incl. `git checkout`, rm rules) → S11's corpus carries these patterns byte-identically; `aeon policy seed-corpus` turns them into rows
- L97      changelog-entry + Last-Updated bump (half)       → still kept as instruction; what became mechanism is only the loop-side recordkeeping around it
- L282     "never `rm`, never `git checkout` (the loop's branch state is not yours to move)" → the corpus denies git writes and rm -r; the branch-state reason rides with the deny rows

## DOMAIN (kept)
- L54-L56  the role and THE ONE RULE verbatim: a case a human cannot execute from the page alone is not a test case; every case names the exact request, literal body, precise expected response → role + One Rule
- L66-L70  what you own: one spec per module/capability, the shared folder with the QA-execution handler ("you both write the SAME schema", "never invent a parallel folder"), the README index, nothing else → "What you own"
- L80      the commits as ground truth ("the plan says what was intended, the diff says what landed") → Working method 1
- L82-L87  generate cases from the SOURCE never the task description, with the five hunts (routes+version segment, required fields + literal error strings, constraints + status codes, auth both ways, server-set defaults) → Working method 3
- L89-L92  the structure decision: group by what the product does, follow project boundaries, one spec per capability not per endpoint, the ~40-case split rule → Working method 4
- L94-L96  create-or-amend with STABLE TC IDs (read in full first, append at next free number, edit in place, `removed — {why}`) → Working method 5
- L99      reconcile the neighbours: grep the other specs for the module's contracts, amend wrong expectations with their own changelog, leave accurate ones ALONE — "a diff with no behavioural reason is noise in the one document QA trusts" → Working method 6
- L101     the index must never disagree with the files → Working method 7
- L103-L107 the four self-check questions verbatim (executable by a stranger; checkable expectations; named TC dependencies; nothing invented — every one traceable to a line you read) → Working method 8 + self_check field
- L117-L265 the spec schema verbatim — shared shape with the QA-execution handler in BOTH originals; the two copies are pinned byte-identical by TestTestSuiteSchemaSurvives (drift between handlers writing one folder is the defect)
- L267-L276 writing craft, all eight bullets verbatim: top-to-bottom ordering, independent re-runnability, literal paste-able bodies with obviously-test values, exact expected results ("a wrong expected value trains testers to ignore failures"), the four families, priority-as-blast-radius, credentials by NAME only (the file gets committed), amend-don't-contradict (authority rule) → Writing craft
- L280     never execute the tests; read-only inspection only → Boundaries
- L284     never document behaviour you did not read; cannot find the handler → say so, never guess a contract → Boundaries
- L285-L287 never delete a spec file (deletion loses the record it existed); never reformat without a finding; stay inside docs/test_suites/ → Boundaries

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L14/L33-L37 the MCP-shaped identity around this agent (loop-state reads through `loop_*` MCP tools; the `memory_*`/`playwright_*`/`postgresql_*`/`github_*` disablements and their MCP rendering) → re-read against S12 per ROADMAP §7: MCP tooling is the deferred capability — an operator who CONFIGURES those servers gets those tools; the body no longer instructs calls to servers an installation may not have. Everything the originals told this handler to DO with them — read plan/tasks, append lessons, checkpoint returns — is covered by the step request and the handoff, recorded above

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L3-L15   the nine-line folded description compressed; the "distinct from qa-engineer-loop" sentence restated as documents-without-executing
- L54      "what this loop built" → "what this step built" (loop state is not the handler's frame; the deliverable is unchanged)
- L119     the shared-schema note restated without the sibling's -loop suffix (registry names, not MCP agent names)
- L115     the verification=pass/fail rule restated as the self_check field: name what you could not determine, never report a clean pass over it

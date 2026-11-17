# security-engineer — drop record

original: opencode/agents/security-engineer-loop.md  (130 lines)
rewrite:  aeon/internal/handler/bodies/security-engineer.md  (75 lines)
delta:    56 lines removed, 1 added

## RITUAL (deleted — the engine does this)
- L40-L42  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L44-L46  "Load the dev-loop-protocol skill first" + path discipline (confirm cwd via `pwd`; `$ROOT` shell-indirection; built-in tools; never the filesystem MCP) → nothing loads skills here; the project-root-relative rule survives in Boundaries
- L54      `pwd` + `loop_get` + `$ROOT` indirection step → context arrives resolved
- L55      loop_readFile(plan) + loop_readTask + scratch + loop_get lessons → the step's request carries the spec, the engineer's notes, and prior context
- L66      `memory_append` one-liner (half) → no memory tool; the FAILING-GATE half ("include the specific finding + owning file so the re-dispatched engineer knows exactly what to fix") survives as the findings mapping (file:line + concrete fix)
- L67-L72  "RECORD THE GATE RESULT YOURSELF" via `task_setStatus(secure/failed)` + "Check the tool result — a refusal comes back as `isError`, it does not throw" → the agent-side state write dies with the state tools; its INTENT ("you ran the scans, so the evidence comes from you") survives as the handoff rule — see BECAME MECHANISM. The isError quirk-note was MCP plumbing
- L72      `event_append` agent-return checkpoint → the daemon emits step events
- L73-L78  FINAL REPORT block (half) → key-values deleted; the DONE≠VERDICT distinction ("'done' means I finished the scan; the gate answer is the verdict, and only Critical/High fail it") survives verbatim in substance (REWORDED)
- L127     "You NEVER mutate loop state... state tools... loop_get" (half) → the handoff IS the record; "the only state tools" sentence dies with them

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-thinking`      → A1 removed spawning; `variant` measured to have zero consumers (Q2) — this agent's variant was the "thinking" flavour; recorded by the loader, never ported
- L7-L21   tools map incl. the L11 patch-merge quirk comment → allow-list `[read, write, edit, bash, glob, grep, todo]`: task DROPPED (A1), todowrite→todo (A19), codesearch/lsp/patch retired (W1), question not re-granted; the OpenCode permission-merge quirk dies with OpenCode
- L22-L33  permission.bash deny table (git/rm denies; the edit-not-deny-by-default note) → S11 corpus content byte-identically (`TestNoPolicyWasAdded` pins corpus==original); the note was OpenCode's
- L68-L69  `task_setStatus` "REFUSED without evidence — a gate that can be passed by asserting it is not a gate" → **the handoff schema is the Aeon equivalent**: `scanners` and `verdict` are REQUIRED handoff fields, so RunLoop refuses completion without the evidence, exactly as the MCP refused `secure` without proof. The body states the rule; the schema enforces it

## DOMAIN (kept)
- L38      the role: the per-task gate, after local verification before the PR, records every time, never fixes code → role line
- L48-L50  scope: what this task changed, not the whole codebase, bounded by notes + diff; "a full-system audit is a different engagement" (the standalone-agent sentence restated for the blueprint) → role
- L57      review mapped to `file:line`, category, severity, evidence, concrete fix → Procedure 3
- L63      the honesty law verbatim: "If a scanner isn't installed, note that in the record rather than claiming a clean result" → Procedure 4 (the named commands are DEFERRED; the law stays)
- L64      record-even-when-clean with its exact phrase "checked, no issues found" a required, valuable result + redact to `<redacted>` → Procedure 5
- L65      the durable register law verbatim in substance: Critical/High (and cleared-Critical/High) only into `docs/security.md`, dated entries, cross-loop posture log, skip for clean/Low/Med, never delete prior entries → Procedure 6
- L70      (half) the evidence philosophy: real scanner output with counts including Medium/Low — "a gate that can be passed by asserting it is not a gate" → handoff rule (REWORDED)
- L82-L89  the gate table VERBATIM: Critical 9.0–10.0 RCE/authn-bypass/data-loss/secret-leak → fail, blocks the PR, routes back to engineer; High 7.0–8.9 priv-esc/data-exposure/injection → fail; Medium 4.0–6.9 → recorded pass; Low/Info ≤3.9 → recorded pass; "Only Critical/High fail the gate. Everything is still recorded." → the gate section (this is the severity judgement A18 says survives the tooling)
- L93-L102 the security checklist VERBATIM (all ten dimensions), including the typosquat/mistyped-scope block with its `@cjhapao` vs `@cjlapao` worked example → the checklist section
- L106-L122 the security.md record schema verbatim (per-task block, scope/scanners/verdict lines, SEC-NNN finding shape, the none-case line) → the schema section; this body is its single owner
- L126     never fix code — the record is your write; blocking findings described and routed back → Boundaries
- L128     **No false clears** verbatim: never pass a check you didn't perform; missing scanner = stated gap, blocked if it prevents a responsible verdict; "a false clear ships a vulnerability" → Boundaries
- L129     redact; live secret → location not value, flagged Critical → Boundaries
- L130     never git writes; project-root-relative → Boundaries

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L59-L62  the named scanner surface: `npm audit`/`pnpm audit`, `govulncheck ./...`, `cargo audit`, `semgrep`, language-linter security rules, `gitleaks`/`trufflehog`, `trivy`, `checkov`, `tfsec` (+ `gosec` in the L70 evidence example) → capabilities an installation may not have (A18, named not waved-at): the body keeps the judgement (run what exists, capture output, note what's missing) and this record keeps the capability list so an installation that has them can wire them back — none is instructed from a body
- L67-L72  the loop-state MCP: `task_setStatus` with its secure-refused-without-proof server rule, `event_append`, `loop_get` → re-read against S12 per ROADMAP §7; an operator who CONFIGURES such a server gets it; the Aeon equivalent — required handoff fields enforced by RunLoop — is recorded above
- L104     the `loop_readFile(loopSlug, "security")` addressing → the record is an ordinary file at the produces-declared path; the SHAPE (the schema above) is kept, the address is delegated

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L5       tools line + the sentences naming where output goes: deliverables are documents in the database (`doc_write`/`doc_read`/`doc_edit`/`doc_list`) instead of workspace files (`write`/`edit`); the role and its rules are unchanged
- L3       description's dev-loops framing compressed; task_setStatus clause restated as the recorded-verdict discipline
- L50/L55  scratch/loop reads restated as the step's request and the reports it names
- L70      "Give the real output: `gosec: 0 critical...`" example restated generically (count-style evidence) — gosec itself deferred with the other scanners
- L78      "`status=done` = 'I finished the scan'; the gate verdict is in `verification`" restated as the done≠verdict handoff sentence

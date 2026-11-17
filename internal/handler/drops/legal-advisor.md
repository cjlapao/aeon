# legal-advisor — drop record

original: opencode/agents/legal-advisor-loop.md  (88 lines)
rewrite:  aeon/internal/handler/bodies/legal-advisor.md  (49 lines)
delta:    39 lines removed, 0 added

## RITUAL (deleted — the engine does this)
- L41-L43  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L45-L47  "Load the dev-loop-protocol skill first" + the `legal.md`/`docs/legal.md` schemas living there → nothing loads skills here; THE SCHEMAS LIVE HERE NOW (A5 — this handler owns its brief's shape)
- L58      step 0: `pwd` + `loopSlug` + `loop_writeFile(loopSlug,"legal")` + read-back + the durable-register sentence → the register sentence survives (Procedure 5); the addressing goes with produces:
- L59      loop_readFile(discovery) + loop_get + register read → the step's request carries the discovery (its "## Legal review" flag is still WHY — kept); prior-register reading survives
- L72      `memory_append` note + memory.md rendering → no memory tool; rendering DEFERRED
- L73      `event_append` agent-return checkpoint → the daemon emits step events
- L74-L79  FINAL REPORT lines (half) → the five outcome-shaped key-value lines deleted; all five ANSWERS survive as the `outcome` handoff field + the blocked-naming-what's-missing rule (REWORDED)
- L83      "file lane is ...legal.md, docs/legal.md, scratch/ (lessons via memory_append)" (half) → the deliverables list survives in Boundaries; the loop-dir addresses and scratch die with produces:
- L85      "You NEVER mutate loop state (the only state tools...)" (half) → the handoff IS the output; the git/license-tools-read-only half survives

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-thinking`      → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map incl. the L11 patch-merge quirk comment → allow-list `[read, write, edit, bash, glob, grep, todo, webfetch]`: task DROPPED (A1), todowrite→todo (A19), codesearch/lsp/patch retired (W1), question not re-granted (the never-ask-directly relay IS the design); webfetch granted for the one place the body genuinely needs an external source — reading a license at its source
- L22-L31  permission.bash deny table (incl. the blanket `rm *` deny) → S11 corpus content byte-identically (`TestNoPolicyWasAdded` pins corpus==original)
- L60      step 2 `todowrite` → the `todo` tool (A19)

## DOMAIN (kept)
- L35      the role verbatim in substance: run ONLY when the investigator flagged legal surface; assess licensing/privacy/export-crypto/ToS/compliance; "You advise; you never implement" → role line
- L37-L39  **THE ONE DISCLAIMER verbatim**: NOT licensed counsel; advisory, not a legal opinion; no attorney-client relationship; leads every document; real-risk cases (regulator/litigation/contract/sanctions) always → "escalate to qualified counsel in the relevant jurisdiction before acting" + `counsel review: yes` → its own section, uncut (A4's named risk: this is the text a lazy shortening pass deletes first)
- L49-L54  the two dispatch modes verbatim in substance: FRESH vs INCORPORATE ANSWERS (fold answers in, Decisions section with verbatim + rationale, update Findings/Recommended/constraints, recompute outcome, update the register entry, "Surgical editing, not research"); the relay never-asks-directly sentence → the modes section (shared boilerplate with the investigator/clarifier originals — three originals carry the pattern)
- L61-L65  the four assessment surfaces verbatim: licensing (ecosystem tools named incl. `npm ls --all --json`, `pip-licenses`, `go-licenses report`, `cargo license --json`, `syft`; SPDX classification; usage pattern vs copyleft reach; source-available→proprietary; AGPL-in-SaaS network clause), privacy (personal/special-category/children's data, basis, transfers, DSR/retention/consent), export/crypto (classification/notification — almost always counsel), ToS/compliance (SOC2/HIPAA/PCI/GDPR/CCPA/accessibility) → Procedure 3 (bounded, read-only kept)
- L66      the brief schema + the four outcome values verbatim: disclaimer-led, outcome/scope/Findings(severity+area+what-could-go-wrong+counsel review)/Open questions(ONLY plan-or-risk-changing, 2–4 options + recommended default, "None.")/empty Decisions/Recommended approach with the two worked constraint examples ("do NOT use dep X (AGPL)..."; "add a consent-capture task..."); clear/resolved/blocked/pending semantics with their routing sentences → Procedure 4
- L71      the durable register law: update ONLY on a real finding, skip for clear, dated entry (finding·area·decision·outcome·counsel-needed), never delete, amend on change → Procedure 5
- L83      never edit source code — brief and register only, nothing else ever → Boundaries
- L84      never ask the user directly; surface Open questions; re-dispatch mode B folds answers → Boundaries
- L86      never present advisory output as legal advice; consequential → counsel review + recommend counsel → Boundaries
- L87      never green-wash a blocker to clear/resolved to keep the loop moving — blocked is the correct, useful answer → Boundaries
- L88      redact secrets → Boundaries

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L72/L83  the loop-memory rendering (`memory_append`; "never hand-write memory.md — the MCP renders it") → re-read against S12 per ROADMAP §7: an operator who CONFIGURES such a server gets it; the body instructs none of it
- L58/L59  the loop-file addressing (`loop_writeFile(loopSlug,"legal")`, `loop_readFile(loopSlug,"discovery")`, loop_get) → per-loop documents arrive at / come from paths the step's request names (produces:); the brief's SHAPE is owned here, the address is not
- L73      `event_append` on the loop timeline → daemon step events

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L5       tools line + the sentences naming where output goes: deliverables are documents in the database (`doc_write`/`doc_read`/`doc_edit`/`doc_list`) instead of workspace files (`write`/`edit`); the role and its rules are unchanged
- L3       description's dev-loops framing compressed; "orchestrator to ask the user" restated as the blueprint relay
- L51      "(`question` is off)" — the front-matter disablement — restated as the tool absence + the relay sentence (the disablement survives as an absent grant)
- L54      "`ask` begins 'INCORPORATE ANSWERS'" restated as the request listing the pairs (the mode heading itself is restored to the originals' shared wording, which three originals carry)
- L74-L79  the five FINAL REPORT lines restated as outcome values + the blocked-naming rule (each answer's routing sentence kept: proceed-to-plan / resolve-with-user / honor-constraints / needs-decision-or-counsel)

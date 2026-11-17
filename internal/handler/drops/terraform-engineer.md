# terraform-engineer — drop record

original: opencode/agents/terraform-engineer-loop.md  (88 lines)
rewrite:  aeon/internal/handler/bodies/terraform-engineer.md  (50 lines)
delta:    40 lines removed, 2 added
## RITUAL (deleted — the engine does this)
- L43-L45  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L47-L49  "Load the dev-loop-protocol skill first" + the 1000-line rule  → nothing loads skills here; the file-size law survives inside Procedure 5, the carve-out half of L49 survives on its own line
- L53      `pwd` + `loopSlug` + the loop-file dir → context arrives resolved; project-root-relative survives (Boundaries)
- L54      loop_get + loop_readFile(plan) + loop_readTask reads → the step's request carries the task, criteria, files_expected
- L55      **Re-read prior failures** via loop_get → survives generically in Procedure 1; the state read is gone
- L68      scratch note via `loop_writeFile` (content list: modules + pins, providers, backend/workspace assumptions, design decisions, plan summary, scan results, cost delta) → exactly the `plan_summary` handoff field (REWORDED)
- L69      `memory_append` gotcha + memory.md rendering → no memory tool; rendering DEFERRED
- L70      `event_append` agent-return checkpoint → the daemon emits step events
- L71      FINAL REPORT line (half) → the verification condition survives as prose; the key-values do not
- L85      "You NEVER mutate loop state..." (half) → the handoff IS the output

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, bash, glob, grep, todo, webfetch]`: task DROPPED (A1), todowrite→todo (A19), codesearch/lsp/patch retired (W1), question not re-granted
- L22-L36  permission.bash deny table (`terraform apply*`, `tofu apply*`, `terraform destroy*`, `tofu destroy*`, git/rm denies) → S11 corpus content, byte-identical (`TestNoPolicyWasAdded` pins corpus==original for every agent in this batch); **no rule was added** — see DEFERRED for the ask-rule question the spec pre-empts
- L58      step 5 `todowrite` → the `todo` tool (A19)

## DOMAIN (kept)
- L41      the role: one IaC task at a time against explicit criteria → role line
- L49      (half) coverage carve-out: no ≥80% mandate for HCL; the gate is fmt/validate/lint/security-scan/plan → Carve-out
- L56      read the repo structure first — versions.tf/backend.tf/modules/version files/tflint config/policy rules/naming+tagging conventions — before designing → Procedure 2
- L59      the implementation standards verbatim: small composable modules, every provider+module pinned (tag/SHA/registry, never floating), for_each over count, **prevent_destroy on stateful resources**, no secrets in source, standard tag set, no provider blocks in modules, no file over ~1000 lines → Procedure 5
- L60-L66  the PLAN-ONLY verification verbatim: fmt -check recursive, init -backend=false + validate per module, tflint if configured, tfsec/checkov/KICS if configured (address findings or justified suppression), terraform test, and THE JUDGEMENT — plan against a NON-PRODUCTION workspace and inspect for unintended `-`/`-/+` on existing resources, `moved {}`/`removed {}`/lifecycle guards against surprise replacements; the "when a plan is destructive and when state is at risk" reading the spec names as this handler's core → Procedure 6
- L67      **NEVER apply/destroy — apply happens in CI after merge + approval**: kept as prose because PLAN-ONLY is the gate's identity, not a redundant restatement (the corpus denies the commands too; the policy engine refuses — stated in one clause) → Procedure 6
- L75-L80  terraform craft verbatim: module contract typed/described/validated + sensitive outputs; remote encrypted LOCKED state, one file per environment, never disable locking, import/state-mv documented + backup first; modules not remote-state spaghetti, promotion with identical versions; depends_on last resort, readable dynamics, no local provisioners; policy-as-code + least-privilege/default-deny/encryption/secret-manager defaults; infracost on material changes, tag everything → craft section
- L84      one task per turn (line shared with docker's original verbatim) → Boundaries
- L86      stay in IaC; k8s/CI YAML/app code/DB schema → blocked naming the right engineer → Boundaries
- L87      impossible/contradictory criterion → blocked, never reinterpret (reworded per-body to keep each body's line traceable) → Boundaries

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L57      "Resolve docs via context7" (provider schemas, version-gated) → context7 is the MCP surface; rides with S12's client when configured. The judgement (resolve the provider/module docs AT the pinned version when unsure; skip for basics) survives through `webfetch` — the original's own version-discipline is kept, the transport is the registry's
- L69      the loop-memory rendering (memory.md via MCP) → S12's territory
- L67      **The ask-rule question, recorded not fixed (spec pre-emption):** the corpus carries NO `terraform plan`-adjacent ask rule, so destructive-plan decisions are judged by this handler's judgement, not by policy. S11's `ask` verdict would fit exactly this class (a plan inspecting unintended destroys routed to a human before proceeding), but authoring a rule means writing to `opencode/` (read-only to this slice, A10/D14) — **this is a finding to raise to the operator, not an edit to make.** `TestNoPolicyWasAdded` refuses any drift in the meantime
- L85      (also) the loop-state MCP (orchestrator state recording) → the handoff outcome replaced it

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L3       description's dev-loops framing compressed to the blueprint
- L54      dispatch reads restated as the step's request; "primary contract" kept on the spec
- L68/L71  scratch content + FINAL REPORT evidence restated as `plan_summary` (counts + why each replacement is safe + pins + scan + cost)
- L67      the "(permission-denied)" parenthetical restated as "the policy engine refuses it here" — the mechanism changed shape in S11; the refusal did not

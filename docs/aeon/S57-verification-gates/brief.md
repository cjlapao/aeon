# Brief — S57 Verification gates and merge-on-green

Written 2026-10-30. Prefix **VG**. Covers [`AUTONOMY-GAPS.md`](../AUTONOMY-GAPS.md) **G3** and the "failures filed into triage" part of **G8**.
Task shapes cite [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) by section (P1–P11).
**Depends on:** S16 (git/forge actions), S44-extra (the component tree), S45 (`internal/desk`, the generic `proposal.commit`, the
human-door guard pattern), S48 (blueprints as rows — for the blueprint change, a cross-slice request), S43-extra (the seed engine,
for the template set), S29/S51 (triage — the filing path). **Cross-slice contracts:** S55 LC (loop stop, budgets), S56 AL (autonomy
grants — consumes this slice's merge rule), S27 CN (the scheduler that leans on these gates), S58 SV (notifications), S59 EV
(execution environment), S60 MQ (merge queue), S63 RT (scorecards). T0 re-checks what has merged.

## 1. Why

An autonomous loop is only as trustworthy as the thing that says "this is done and safe to land." Today that thing is not code:

- **Verification is a verifier agent's say-so.** The `dev-loop` blueprint's `verify` rung is a `model-step` run by `verifier-engineer`
  (`internal/blueprint/blueprints/dev-loop.yaml:87-90`); the agent runs `bash` and writes a handoff field `verification` ∈
  `pass|fail|unverified` (`internal/handler/handoff.go:96-123`). The engine routes the *reported* verdict. Nothing in Go runs a build or a
  test, no command is declared per repository, and no exit code is ever recorded as evidence. There are exactly three step kinds
  (`model-step`, `code-action`, `gate`); no kind runs a command and gates on its exit code.
- **The merge gate exists but is half-blind.** `mergeGate` (`internal/action/forge_actions.go:383`) refuses a merge with no review, a
  latest review that is not `approve`, or an unanswered `request_changes`, and it *would* refuse red CI — but only through the optional
  `forge.StatusLister` (`internal/forge/forge.go:132-138`), which neither `GitHub` nor `Fake` implements. So in production the gate always
  writes `checks = "not-configured (N/A …)"` (`forge_actions.go:~432`). Red CI does not stop a merge. `PR` carries no head SHA or check
  field; `Review.SubmittedAt` is declared but `github.go:454-475` never sets it, so the gate's `review_at` evidence is always `(unset)`.
- **A review can only come from a person.** The gate requires an approving *forge* review. `forge.postReview` is the only action that
  could post one, and it cannot run from a blueprint: its registry entry requires `step_id` and `attempt` (`internal/action/registry.go`,
  `ForgePostReview`), which `actionArgs` never supplies (`internal/loop/execute.go:224-…`). No embedded blueprint uses it. With one
  forge token (`docs/FORGE.md`'s identity limit) an agent cannot approve its own PR, so *autonomous* merge is impossible by construction,
  and the policy for "when may Aeon's own review count" does not exist.
- **Acceptance criteria are prose nobody checks.** They live in `store.Brief.Acceptance []string` (`internal/store/briefs.go:21-38`) and in
  planner task files; `task.success_summary` has no Go writer. Nothing relates a criterion to evidence.
- **Nothing is recorded after a merge.** The engine does not apply a step's `sets:` (validated but unused, `loop/advance.go:348` aside), so
  `forge.merge` success exists only as `loop.action.executed` evidence. No `merged` fact, no watch on the base branch, no reaction when
  `main` goes red afterwards. Terminal reactors exist for the queue, roadmap and triage (`internal/daemon/blueprint.go:567-612`), none for
  gates.
- **Verification would inherit the daemon's environment.** `tools/spawn.go:154` sets `cmd.Env = nil` (the child inherits everything,
  including provider keys and `AEON_FORGE_TOKEN`/`GITHUB_TOKEN`/`GH_TOKEN`), which the code calls "a stated limitation". Running
  declared commands automatically makes that limitation a real exposure.

## 2. What this slice delivers

1. **`verify.run`** — a new **local** code-action in the closed registry. It runs a component's declared gates in the loop's workspace
   under the **same bash policy gate and limits** as the `bash` tool, with a **scrubbed environment** (an allowlist; no secret reaches a
   gate), and records exit code, wall time, byte counts and a digest per command. Pass means every required gate exited 0 on the head SHA;
   a gate that did not run (denied, timed out, killed, no gates configured) is **unverified, never pass**.
2. **`verify_command` records** — per component (root or child; a child with none of its own inherits its root's, shown with source),
   `required|advisory`, timeout, a declared memory bound (enforcement deferred to S59 EV, stated honestly in every view). A managed record:
   list-with-details, daemon first, CLI client, propose-only agent tools, an append-only change log, archive never delete.
3. **Forge checks, for real** — `ReadChecks(sha)` and `ListStatuses` on GitHub (check-runs + combined status, honest `unknown`),
   `PR.HeadSHA`, `Review.SubmittedAt`; the `Fake` implements the same. `forge.postReview` is **explicitly retired from blueprints** with a
   validator sentence, so nothing pretends.
4. **A per-component merge rule** — required local gates, required CI checks, review mode (`forge | aeon | either | both`), whether
   unresolved threads block, definition-of-done requirement, merge method. The **baseline with no rule row is today's `mergeGate`
   behaviour, byte for byte** (characterized first). A rule loosening that baseline is authority-widening: human door only; agents may
   only propose a *tightening*. `mergerule.AutonomyFloor` gives S56 the single question it needs: *may an unattended loop's merge grant arm on
   this component?*
5. **Definition of done** — brief acceptance items become `dod_item` rows when the loop first verifies; an item is `met` only with a
   passing `verify_run` as evidence (or a person's explicit mark); a rule may require every item `met`.
6. **A `merge_event` record and a post-merge watch** — every successful `forge.merge` is recorded with the rule revision and gate evidence
   that allowed it; the daemon then watches the base branch's checks for the merge SHA (cold, rows decide) and on red **files a triage
   bug** and emits an event. No automatic revert (the S29 → S26 path turns a validated bug into an ordinary fix loop).
7. **Failures reach triage** — a terminal local-gate failure, a CI red at merge time and a post-merge red each file one **unvalidated**
   bug with a fixed-template body (ids and counts, no output text), deduped per (root, gate, kind).
8. **Seed set `verify.templates`** — inert per-stack gate templates (go, node, python, rust, terraform) a person applies to a component;
   `merge_rule` is **exempt** from seeding (authority record).

## 3. Out of scope

- The scheduler (S27 CN), grants (S56 AL), notifications (S58 SV), isolated environments, resource limits and app secrets (S59 EV), a
  merge queue and re-gating after a rebase (S60 MQ), automatic revert, GraphQL review threads, outcome scorecards (S63 RT).
- Editing `dev-loop` or any blueprint: the `verify-local` rung is a **cross-slice request to S48** (blueprints are rows there).
- Changing how the verifier *agent* works. It stays; `verify.run` runs *before* it and the two coexist.

## 4. Acceptance

1. With no rule and no gates configured, merge behaviour, evidence keys and every refusal sentence are byte-identical to today (T1 goldens).
2. `verify.run` on a component with required gates returns `pass` only when every required gate exited 0; each non-run is `unverified`
   with a stated reason; the engine routes `fail` to `on_reject`, `unverified` to the loop-ceiling-only path, and with no gates the
   action is `unverified: no-gates-configured`.
3. A gate process sees none of the daemon's secrets: a planted `AEON_FORGE_TOKEN`, `GITHUB_TOKEN` and a provider-key variable are absent
   from the child's environment (test, with a canary that fails if the scrub is removed).
4. A gate command denied by policy never spawns; the verdict sentence is recorded verbatim.
5. On GitHub (httptest server) `ReadChecks` maps check-runs and statuses to `success|failure|pending|unknown`; a PR with a red required
   check is refused by the gate; no checks reported with a non-empty required list refuses.
6. Merge rule: every loosening of the baseline is a human-door write; an agent proposal that loosens is refused at create with the field named.
7. Every successful `forge.merge` writes exactly one `merge_event`; a red base branch after merge files exactly one triage bug (deduped).
8. New socket methods are absent from `narrow.Actions()`, agent services, the tool registry and the records catalog except their named
   propose/read twins; guards have canaries.
9. `make protogen && make check-protogen && make guard` green; migrations drilled fresh/upgraded/idempotent; the demo walk run.

## 5. Status

**Built 2026-11-03 — T0–T12, 13/13** (per-task merges to main; T12's close-out re-ran the acceptance fresh, the four migration
drills, the demo walk on a real daemon (CLI lane; the model lane, the fake-forge GitHub leg and the app window name their
witnesses in the task Notes), the guards, the full suite against T0's baseline — the carried pair reproduced, three
deterministic reds proved pre-existing at `f6bf9c94` itself by running them on the untouched tree, the load-suspects
resolved one by one (three green solo; the `cmd/aeon` `database is locked` class reproduced identically on the
untouched tree — environmental, named not chased) — and fixed the one defect the walk found (a manual `verify.try` of an ADVISORY gate never spawned it; the close-out commit).
`SLICES.md` carries the row; `tasks/T12.md` carries the evidence table and the carried-forward findings.

Position in the order:
**S55 LC → S56 AL → S57 VG → S27 CN** ([`AUTONOMY-GAPS.md`](../AUTONOMY-GAPS.md) §7); S56's merge grant *needs* this slice's rule floor,
so S57 is built (at least T5) before S56 can ship the merge grant.

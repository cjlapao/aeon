# Design — S57 Verification gates and merge-on-green

Rules are **VG1–VG24**, cited from `plan.md` and the task files. Task shapes follow
[`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (P1–P11); a deviation is recorded here with its reason. Wherever the old model said
"project" this document says **root component** (`rootId`): the org record is the S44-extra component tree (a root and its children).

## Verified facts (read 2026-10-30; T0 re-walks every row)

| Fact | Where |
| --- | --- |
| Three step kinds only: `model-step`, `code-action`, `gate`; a code-action names a registered action | `internal/blueprint/types.go` (Step), validator `blueprint/validate.go:418-436` |
| The action registry is closed, package-private, one execution door; classes are code with no setter; 9 names | `internal/action/registry.go` (`registry`, `Dispatch`), `class.go:52-70` |
| `Dispatch` refuses a missing required arg naming it; `ArgSpec{Type, Required, Source: code\|manifest\|handoff\|evidence}`; evidence keys non-empty | `registry.go` (`ArgSpec`, `Dispatch`) |
| `Services{Git, Forge, Audit}`; `Forge` wired only for a github.com origin | `registry.go:142-146`, `internal/daemon/actions.go:33-55` |
| Unattended outward → deny unless `allow_outward`; irreversible (`forge.merge`) always deny unattended | `action/class.go:157-180`, `loop/execute.go:68-93` |
| `actionArgs`: `Dir = st.Root`, numbers from `st.Evidence` only; never fills `StepID`/`Attempt` | `loop/execute.go:224-…` |
| `mergeGate` reads reviews, optional `StatusLister`/`ThreadLister`, writes `review_state/review_by/review_at/checks/threads` evidence; called from `MergePR` between the base-ref guard and the irreversible call | `action/forge_actions.go:383-470`, `:293` |
| `Check{Name,State}`, `StatusLister`, `ThreadLister` declared; only the test double `gateForge` implements them | `forge/forge.go:113,132-138`; `action/gate_test.go:46` |
| `GitHub.ListReviews` maps ID/Body/State/User only — `SubmittedAt` never set; `PR{Number,Head,BaseRef,Merged,MergeSHA}` has no head SHA | `forge/github.go:454-475`, `forge/forge.go` (PR) |
| GitHub auth: env `AEON_FORGE_TOKEN` → `GITHUB_TOKEN` → `GH_TOKEN`; plain `net/http`; 403 ambiguity handled; retry on transport | `forge/github.go:80-91,199` |
| `bash` spawns `bash --noprofile --norc -c` in `StepParams.WorkspaceRoot`; 600 s ceiling; 64 KiB stdout/stderr caps; group kill (SIGTERM, 2 s, SIGKILL); `cmd.Env = nil` ⇒ full daemon env | `tools/bash.go:149`, `tools/spawn.go:128-160`, `tools/limits.go` |
| `Spawn(ctx, command, opts, verdict policy.AllowToken)` demands a policy-issued token for the exact command | `tools/spawn.go:106` |
| Command verdicts: `CommandVerdictFor(store, log)` → `func(agent, frozen, command) (policy.Verdict, bool)`; deny wins; empty set denies; an `ask` unattended is a deny with no question row | `daemon/policy_gate.go:293-310`, `runtime/loop.go:~744` |
| `Handoff.Verification()` ∈ pass/fail/unverified; `IsUnverified` = a reject; the engine charges unverified only against `LoopCeiling` | `handler/handoff.go:96-123`, `loop/advance.go:~520-535` |
| `routeReject` / `DecideExhaustion` / `handleExhaustion` own reject routing; `then: escalate\|fail-item\|block`; unattended escalate collapses to block | `loop/advance.go:537`, `loop/counters.go:215-262`, `loop/escalate.go:70-148` |
| `Brief{Acceptance, Constraints []string, TestPlan, ReviewState, Version}`; `task.success_summary` has no Go writer | `store/briefs.go:21-38`, `store/tasks.go:20-58` |
| Terminal reactors off `publishLoop` on `loop.finished\|blocked`: queue, roadmap, triage | `daemon/blueprint.go:567-612` |
| Triage filing: `TriageFileParams{componentRoot,title,body,filedBy}`; `bug.filed_by` is the one provenance; no model-set validation/priority | `protocol/triage.go:~50`, `store/bug.go:38-91` |
| `proposal.kind` is a closed CHECK widened by table rebuild (0045 precedent: copy guard + fk guard, no "must be empty" guard) | `store/schema.go:2420-2426` |
| Latest timestamped migration: `20261030130000_agent.sql`; legacy 1–45 frozen | `internal/store/migrations/` |

## VG1 — Words

- **gate** — one declared command on a component (`verify_command` row) the daemon runs to prove something about a working tree.
- **verify run** — one execution of a component's effective gate set for a loop (`verify_run` + one `verify_run_result` per gate).
- **head SHA** — the commit a run or a check refers to; a gate result is valid **for that SHA only**.
- **merge rule** — the per-component record the merge gate consumes (VG10). **Baseline** — the built-in rule when a component has none,
  equal to today's `mergeGate`.
- **dod item** — one acceptance criterion of the approved brief with a recorded state (VG13).

## VG2 — `verify.run`: a new local code-action

Registered in `internal/action` (constant `VerifyRun = "verify.run"`, class **local**, strategy "naturally idempotent" — a re-run
re-runs; the run record keyed by (loop, step position, attempt) is replaced only by a newer attempt, never edited).

| Arg | Type | Req | Source | Meaning |
| --- | --- | --- | --- | --- |
| `dir` | string | yes | code | the loop's root (`actionArgs` already sets it) |
| `loop` | string | yes | code | loop slug |
| `task` | string | no | code | the task being verified (recorded) |
| `gate` | string | no | code (step `params.gate`) | one gate name; empty ⇒ **every required and advisory gate** of the effective set |
| `agent` | string | no | code (step `params.agent`, default `verifier-engineer`) | the identity the policy gate evaluates each command as (VG4) |

Evidence (non-empty, scalar strings): `verify_state` (`pass|fail|unverified`), `verify_run_id`, `verify_summary` (fixed template: counts
by state), `verify_head_sha`, `verify_digest` (sha256 of the canonical result record). The model never supplies a number or a command.

## VG3 — `verify_command` records and the effective set

Table `verify_command` (migration M1, T3): `id TEXT PK ("vc-"+id)`, `component_id TEXT NOT NULL REFERENCES component(id)`, `name`
(slug, unique per component among non-archived), `command TEXT NOT NULL` (≤ 4096 bytes, no NUL), `requirement CHECK IN
('required','advisory')`, `timeout_s INTEGER NOT NULL CHECK BETWEEN 1 AND 600` (the bash ceiling), `memory_mb INTEGER NULL` (declared,
**not enforced in v1**; every view says so), `position INTEGER`, `enabled`, `origin CHECK IN ('user','template')`, `template_id`, `archived_at`,
`created_at`, `updated_at`. Append-only `verify_command_event` (P3: one row per changing write, same transaction, actor from
`table.ActorFor`, fixed-template summary from ids/enums, revision numbers never reset, no FK to the record).

**Effective set (one level, replace-not-merge).** `store.EffectiveGates(componentID)` returns: the component's own non-archived enabled rows if
it has any; otherwise, for a child, its root's. Each row carries `source: own|root`. Replace-not-merge is the rule because merging two
lists makes "what runs here" unanswerable from the child's own screen; a child that wants the root's gates *plus* one more copies them
(the UI offers "copy from root"). A root has no parent, so only `own`. A root with its own repository and children is a legitimate
holder: children that define nothing run the root's gates in **their own** root path.

Validation (`desk.Validate`, one function used by direct writes and proposals — P6 parity): name slug, command non-empty, size, timeout
range, and an **advisory policy preview** — the command evaluated now as the verifier agent; "currently denied by policy: SENTENCE" is a
*warning*, never a refusal (policy changes independently; the run-time gate is authoritative).

## VG4 — Running one gate (the contract)

`internal/verify.Runner.RunGate(ctx, in) (GateResult, error)` — the only place a gate is spawned:

1. **Policy.** Each command is evaluated through the existing seam `CommandVerdictFor` with `Agent = in.agent` and `Tool: "bash"`.
   `deny`/empty/`ask`-unattended ⇒ the gate is **unverified** with `reason = policy` and the verdict sentence verbatim; **nothing spawns**.
   An allow yields the `policy.AllowToken` that `tools.Spawn` demands (`tools/spawn.go:106`) — verification cannot bypass what `bash`
   cannot. *Why the verifier's identity:* the corpus already grants `verifier-engineer` the build/test commands it runs today
   (`handler/bodies/verifier-engineer.md:20-31`), so verification keeps the same permitted set; running as a fresh pseudo-agent would
   either deny everything (PL5) or need a new corpus. A deployment that disagrees sets the step param `agent` (Open decision OD3).
2. **Environment** — VG5.
3. **Limits.** `SpawnOptions{Dir: dir, Timeout: min(timeout_s, BashTimeout)}`, default stdout/stderr caps (64 KiB); stdin null; group kill.
4. **Result mapping.** exit 0 ⇒ `pass`; non-zero exit ⇒ `fail`; **timed out**, **killed** (`SIGKILL`/exit 137 — the same OOM heuristic
   `tools/bash.go` uses), **could not start**, **policy denied** ⇒ `unverified` (a check that did not complete proves nothing — the
   handoff's own definition, `handoff.go:96-123`). Every result carries exit code, signal text, wall ms, true byte counts, truncation
   flags, sha256 of each captured stream.
5. **Aggregate.** any *required* `fail` ⇒ run `fail`; else any *required* `unverified` ⇒ `unverified`; else `pass`. Advisory results are
   recorded and shown but never change the run state (`advisory-fail` is a flag on the result). **No enabled required gate in the effective set
   ⇒ the run is `unverified` with reason `no-gates-configured`** — never `pass` on emptiness. Gates run **sequentially in `position` order**
   (deterministic evidence; a required `fail` does not short-circuit — every gate runs so one round of rework sees every red; a
   **timeout budget** `verify.run` total ≤ the loop's remaining step deadline, T0 checks the runtime deadline seam).
6. **Cancellation.** The run's `ctx` is the loop's; S55 LC's stop cancels it and the group kill (SIGTERM → 2 s → SIGKILL) applies; a
   cancelled run records `unverified: cancelled`.

## VG5 — Environment scrubbing is a hard requirement

`tools.SpawnOptions` gains `Env []string` (additive). `nil` keeps today's behaviour exactly — the `bash` tool and terminals are **unchanged**
(characterized in T1); a non-nil value is the child's *complete* environment. `verify.Runner` always passes `tools.GateEnv(os.Environ())`:

- **Allowlist, not denylist.** Keep `PATH, HOME, USER, LOGNAME, SHELL, LANG, LC_*, TERM, TMPDIR, TZ, CI=1` and a closed set of toolchain
  locations (`GOPATH, GOCACHE, GOMODCACHE, GOFLAGS, CARGO_HOME, RUSTUP_HOME, NPM_CONFIG_CACHE, PNPM_HOME, VIRTUAL_ENV`). Everything else is
  dropped — including `AEON_*`, `GITHUB_TOKEN`, `GH_TOKEN`, provider keys and any `*TOKEN*|*KEY*|*SECRET*|*PASSWORD*` shape.
- A canary test plants each of those and asserts none reaches `env` output; a second canary removes the scrub and proves the first fails.
- A gate that needs a credential (a test hitting a service) is **not expressible here**: app secrets reach a command only through S59 EV's
  records. Cross-slice request to S59 EV (CR-EV1); until then such a gate fails honestly and the view says why.
- Network and filesystem isolation are not provided (S59 EV); the policy gate and the scrubbed env are the v1 boundary, stated in every
  `verify.show`.

## VG6 — Evidence tables (append-only)

`verify_run(id, loop_slug, component_root, component_id, task, position, attempt, head_sha, trigger CHECK IN ('loop','manual'), actor_kind,
actor_id, started_at, ended_at, state CHECK IN ('pass','fail','unverified'), reason, summary, digest)` and
`verify_run_result(run_id, seq, command_id NULL, name, requirement, command TEXT, exit_code NULL, signal TEXT, timed_out, wall_ms, stdout_bytes,
stderr_bytes, truncated, stdout_sha256, stderr_sha256, stdout_tail, stderr_tail, state CHECK IN ('pass','fail','unverified','skipped'), reason)`.
Both are **append-only by trigger** (P3). The command text is **snapshotted** so history survives a later edit or archive.

*Output text.* Digests and counts are always stored. **Tails** (≤ 2 KiB each) are stored only for non-`pass` results, passed through a
best-effort redaction of token-shaped strings, and flagged `redaction: best-effort` — an honest limit: Aeon cannot guarantee a failing test
did not print a secret, so tails are never copied into bug bodies, audit summaries, events or agent-visible lists by default (only
`verify_run_show` returns them, and that read is human-door plus an agent twin scoped to *its own loop*; Open decision OD6).

## VG7 — Engine routing for `verify.run`

`loop.executeCodeAction` today treats any Dispatch error as `action-failed`. `verify.run` returns a *result*, so after a successful Dispatch
the engine maps `verify_state`:

- `pass` → `moveNext` (as any action).
- `fail` → the step's `on_reject` through `routeReject` (`advance.go:537`) with reason `verify: <verify_summary>`; counters/caps exactly as a model
  step's reject (`DecideExhaustion`, `then: escalate|fail-item|block`).
- `unverified` → the existing unverified path: routes back like a fail but charges **only the loop ceiling** (`DecideUnverified`).
- No `on_reject` on the step → the loop blocks with kind `verify-failed` (a new `BlockedReason` kind; a string, no schema change — T0 confirms
  there is no CHECK on `blocked_reason.kind`).

The blueprint validator accepts `on_reject` on a code-action step only for `verify.run` (T0 confirms what it does today) and validates
`params.gate` against nothing (gate names are per-component data, `unverifiable` at load per P31, like `emits` against an absent handler).
The `dev-loop` change (a `verify-local` rung before the `verify` model-step) is **CR-BP1 to S48**; this slice ships the action and the engine
semantics and a test blueprint fixture.

## VG8 — Forge checks

- `PR` gains `HeadSHA string` (GitHub `pull.head.sha`); `Fake.SeedPR` accepts it. Additive; existing goldens unchanged.
- New optional interface `CheckReader { ReadChecks(ctx, sha string) (ChecksReport, error) }` keyed by **commit SHA** (so the same call serves a
  PR head and the base branch after merge). `ChecksReport{SHA, Checks []Check, State, Complete bool, Source}`. `Check.State` vocabulary
  (closed): `success | failure | pending | unknown`. GitHub implementation: `GET /commits/{sha}/check-runs` (paginated, 100/page, ≤ 10
  pages; beyond → `Complete=false`, State `unknown`) + `GET /commits/{sha}/status`; mapping — check-run `completed` + `success|neutral|skipped`
  ⇒ success; `failure|cancelled|timed_out|action_required|stale` ⇒ failure; `queued|in_progress` ⇒ pending; legacy status `success` ⇒ success,
  `failure|error` ⇒ failure, `pending` ⇒ pending. Aggregate `State`: any failure ⇒ failure; else any pending ⇒ pending; else any unknown or
  incomplete ⇒ unknown; else success; **zero checks ⇒ `unknown` with `Complete=true`, reason `none-reported`** (distinct from "not yet reported",
  which the caller cannot tell from none — so the rule decides, VG11).
- `StatusLister.ListStatuses(ctx, pr)` is implemented on `GitHub` and `Fake` as `ReadPR` → `ReadChecks(HeadSHA)` flattened, so the *existing*
  `mergeGate` becomes live without edit (and T1's goldens pin that the only change in production behaviour is checks becoming real).
- `Review.SubmittedAt` set from GitHub's `submitted_at`; absent stays `""` (the gate already records `(unset)`).
- Error kinds unchanged (O5): transport/auth never read as "no checks".
- `ThreadLister` stays unimplemented on GitHub (review threads are GraphQL-only; out of scope). A rule with `require_threads_resolved`
  **refuses** when the forge cannot report threads — *unverifiable is not green right before something irreversible* (the gate's own comment).

## VG9 — Reviews and `forge.postReview`

- `forge.postReview` stays registered (class outward, no deletion) but is **unrunnable from a blueprint**: the validator refuses a step naming it
  with `unrunnable-action: forge.postReview cannot be fired from a blueprint — it needs a step id and attempt only a model step has (S57 VG9)`.
  The test pins today's reality (the embedded blueprints do not use it) and prevents a silent half-working use. A later slice that wants Aeon to
  *post* its review for humans defines its own path (S56 outward grant; Cross-slice request CR-AL3).
- **Aeon's own review.** The `review` rung's `code-reviewer` verdict (handoff `ok`/`reject`) is already engine state. The merge rule's
  `review_mode` decides what counts: `forge` (a forge approval — baseline), `aeon` (the loop's review step finished `ok` for this head SHA),
  `either`, `both`. `forge.merge` gains one **optional** arg `aeon_review` (`Source: "evidence"`, a fixed token `ok@<sha>` written by the engine
  when the review step completes `ok`; the model cannot supply it). With a missing token and `review_mode ∈ {aeon, both}` the gate refuses.
  **AG4 default: Aeon's review counts only if the rule says so**; the baseline is `forge`.

## VG10 — Merge rule record

Table `merge_rule` (M2, T5), one row per component (root or child; child **replaces** root's whole row when it has one — the VG3 rule), columns:
`component_id PK`, `enabled`, `require_local` (0/1 — every enabled *required* gate of the effective set has a `pass` verify run on the **PR
head SHA**), `require_dod` CHECK IN ('none','all_met'), `required_checks` (JSON array of exact check names), `checks_policy CHECK IN
('reported_green','required_only')` (`reported_green`: every reported check must be success — today's behaviour; `required_only`: only the named
ones), `no_checks CHECK IN ('refuse','allow')` (what to do when the forge reports none — baseline `allow`, today's "N/A"), `review_mode CHECK IN
('forge','aeon','either','both')`, `aeon_review_agents` (JSON slugs; default `["code-reviewer"]`), `require_threads_resolved`, `method CHECK IN
('squash','merge','rebase')` (baseline `squash`, today's default), `updated_at`; append-only `merge_rule_event` (full before/after JSON of the
rule, actor, revision) — P3.

**Not seeded** (VG21). **Not inherited per field** (replace-not-merge).

## VG11 — Evaluation: one pure function, baseline = today

`internal/mergerule.Evaluate(rule, facts) Verdict` is pure (no I/O): facts = reviews, `ChecksReport` for the head SHA, the latest verify run for
that SHA + the effective required gates, dod items, aeon-review token, thread report. It returns `{Allowed, Refusals[]{Code, Sentence}, Evidence
map[string]string}`. `mergeGate` is refactored to *gather facts and call `Evaluate`* (the gathering stays in `action`, the decision in
`mergerule`), preserving every existing refusal sentence verbatim for the baseline rule (T1 goldens run through both the old and the refactored
gate). Evidence keys for the baseline stay `review_state, review_by, review_at, checks, threads`; a non-baseline rule *adds* `rule_revision`,
`verify_run_id`, `verify_head_sha`, `dod`, `checks_state`, `aeon_review`. **Staleness:** a gate result or an `aeon` token for a SHA other than the
PR head SHA at merge time does not count — new commits invalidate evidence (an honest merge never lands unverified code).

Required-check semantics: a name in `required_checks` that the forge did not report is **missing ⇒ refuse** (not green); `pending` ⇒ refuse
with "still running"; `unknown` ⇒ refuse (unverifiable). Zero reported checks follows `no_checks`.

## VG12 — The autonomy floor (the S56 contract)

`mergerule.AutonomyFloor(rule, effectiveGates) (ok bool, missing []string)` answers whether *any* unattended merge may be armed on a component:
`enabled`, `require_local` with ≥ 1 required gate, `require_dod = all_met` **or** an explicit documented waiver flag `dod_waived` (OD7 default: no
waiver — all_met), `no_checks = refuse` **and** (`required_checks` non-empty **or** `checks_policy = reported_green`), `review_mode ∈ {aeon, both,
either}` is permitted but `forge`-only is **not** autonomous (a person must approve), `require_threads_resolved` not required. The function is data
and sentences; **S56 AL decides policy**: its merge grant calls this and refuses to arm below the floor (CR-AL1). VG never grants anything.

## VG13 — Definition of done

Briefs stay as they are (`Acceptance []string` prose). Table `dod_item(id, loop_slug, component_root, brief_id, brief_version, idx, text,
state CHECK IN ('unchecked','met','unmet','unverifiable'), evidence_kind CHECK IN ('','verify_run','manual'), evidence_ref, actor_kind, actor_id,
updated_at)`, rows **materialized by the daemon** (never by an agent) the first time `verify.run` runs for a loop with an approved brief
(`review_state` approved; T0 names the exact field), one per acceptance item; idempotent on (brief_id, idx); a new brief version re-materializes
and carries over `met` only for items whose text is byte-equal. Append-only `dod_event`.

- `dod.mark {id, state, evidenceRef?}` human door: any state; `manual` evidence allowed.
- Agent twin `dod_mark` (records catalog): may set `unmet`/`unverifiable` freely; may set `met` **only** with `evidence_kind=verify_run` and an
  `evidence_ref` that is a `pass` run of the **same loop** on the **current head SHA** (the daemon validates — self-certification without
  evidence is refused with the sentence naming the missing run).
- Rule `require_dod=all_met` ⇒ every item `met` (zero items ⇒ refuse `no-acceptance-recorded` — a loop with no criteria has nothing to be done
  *against*; baseline `none`).

## VG14 — `merge_event`: the missing `merged` fact

The engine does not apply `sets:`; changing that is out of scope. Instead `loop.Deps` gains a seam `Merged func(ctx, MergeRecord) error`, called by
`executeCodeAction` after a **successful** `forge.merge` dispatch, wired in the daemon to the store. Table `merge_event(id, loop_slug,
component_root, component_id, pr, merge_sha, method, base_ref, head_sha, rule_revision, evidence JSON, verify_run_id, merged_at, actor_kind,
actor_id)`, append-only, unique on (component_root, pr, merge_sha). Recovery re-entry (the action is check-then-act and "already merged"
succeeds) writes at most one row (the unique key). History/scorecards (S53, S63) read it. The **task status machine is not touched**.

## VG15 — Post-merge watch

Table `merge_watch(merge_event_id PK, component_id, base_ref, sha, state CHECK IN ('pending','green','red','unknown','expired'), attempts,
next_check_at, last_checked_at, last_state_detail)`. A daemon component `internal/gatewatch` runs from the same startup point as the trigger
clock (`StartLoops(recoveryCtx)`), is **cold and row-driven** (nothing in memory decides; a restart resumes from `next_check_at`; P29 holds — no
process tree), and polls `CheckReader.ReadChecks(merge_sha)`:

- backoff 30 s → 5 min cap, up to 2 h (settings with S44 keys `gates.watchWindow`, `gates.watchInterval` — T0 checks whether S44 has merged; else
  constants with a comment), `pending` keeps waiting, `green` ends, `red` ends and fires VG16, `unknown`/no checks after the window ⇒ `expired`
  (honest; no event claims green).
- A forge that is not GitHub/has no `CheckReader` ⇒ no watch row is created and `merge_event.evidence` says `checks: not-reported`.
- Events: `merge.watch.red`, `merge.watch.green` (S58 SV subscribes; the notify-hint set is closed in `lifecycle.go:187-197` — a request, CR-SV1).

## VG16 — Failures reach triage

A reactor `onGateTerminal` joins `publishLoop`'s terminal fan-out (`daemon/blueprint.go:602-609`, the queue/roadmap/triage precedent: a goroutine,
never blocking the emit) and files **at most one unvalidated bug** per (component root, subject, kind) for:

1. **verify-terminal** — the loop went `blocked` with kind `verify-failed` or exhausted a `verify.run` reject edge (not every rework round: a red
   in a round that is later fixed is normal);
2. **ci-red-at-merge** — `forge.merge` refused because a required check was red;
3. **post-merge-red** — the watch ended `red`.

The bug: `triage.File` with `componentRoot`, `filedBy = "system:gates"` (a reserved system filer, **not** an agent slug — CR-TM1 asks S51 to
reserve the `system:*` namespace when it migrates `filed_by` to slugs), `title` and `body` from **fixed templates over ids and enums** (gate
name, state, exit code, loop slug, run id, head SHA, rule revision, a pointer `aeon verify runs show <id>`; no output text, no tails, no
command text, SEC-033), validation/priority left to the store defaults (`unvalidated`, `unknown` — the daemon cannot pre-label a claim, D7).
**Dedupe:** the store's existing exact-title dedupe (T0 reads `triage/dedupe.go`) plus a per-subject open-bug check; a flood of reds on one gate
is one bug with a counter in a *separate* `gate_failure_count` column only if T0 finds the bug row has no place for it — otherwise the template
carries "first seen" only and repeated reds are not re-filed.

## VG17 — Human door, proposals, guards (P4/P6)

Authority-shaping writes — `verify_command` add/update/archive, `merge_rule` set/clear, `dod.mark` with manual evidence, `verify.try` — are
**socket methods on the person's 0600 + peer-credential surface** and **absent** from `narrow.Actions()`/`BoundaryStatement()`, agent services,
the tool registry, and the records catalog **except** their named read/propose twins (VG19). Sibling guard tests in `cmd/aeon` — never an
edit of an existing guard body (S45 T6's method) — with a positive half and canaries.

Two new `proposal.kind` values (one table rebuild, one migration — P6): **`verify_command`** (payload `{op:"add", name, command, requirement,
timeoutS, componentId, reason}`; an agent may propose `add` only — adding a *required* or *advisory* gate is restrictive: it can only make
landing harder, and it executes only under the policy gate) and **`merge_rule`** (payload = the full proposed rule + `reason`;
`desk.Validate` computes `tightens(current, proposed)` field by field and **refuses any loosening naming the field**; for a component with no
row, "current" is the baseline). A person commits through S45's generic `proposal.commit`; the **committer** is the actor. Per-agent open
proposal limit = S45's constant. Never proposable by an agent: `remove/archive/disable` a gate, `no_checks=allow`, `review_mode` weaker than current,
`require_*` off, `method` change, `dod.mark met` without evidence.

## VG18 — The socket surface (daemon first)

Reads (open to the person's socket; each has an agent twin where marked): `verify.list {componentId?, effective?, includeArchived?, requirement?}`
→ gates with source/origin/last run; `verify.show {id}`; `verify.runs {componentId?, loop?, state?, limit?}`; `verify.runShow {id}` (tails —
human); `verify.vocabulary` (requirements, states, reasons, the env allowlist names — so no client types a set twice); `verify.templates`;
`mergerule.get {componentId, effective?}`; `mergerule.preview {componentId, pr?}` (read-only dry evaluation against live forge facts: *what would
the gate say now*, with every refusal sentence and the autonomy floor result); `mergerule.changes {componentId?, limit?}`;
`mergerule.vocabulary`; `dod.list {loop}`; `merge.events {componentId?, loop?, limit?}`; `merge.watches {state?}`.
Writes (human door): `verify.add`, `verify.update`, `verify.archive`, `verify.restore`, `verify.applyTemplate {componentId, templateId, names?}`,
`verify.try {componentId, id}` (runs one gate now in the component's primary root under the policy gate, records a `trigger='manual'` run —
the authoring loop for gates; it runs in the component's primary root even while a loop is active there, and the response says so — OD8), `mergerule.set`,
`mergerule.clear`, `dod.mark`.
Three new typed refusal codes from the next free block T0 records: `ErrVerifyRefused`, `ErrMergeRuleRefused`, `ErrDodRefused` (reason in `what`, three-part
data, one fixture per family). All methods `RequiresInitialize`, none `Debug`, absent when their service is nil (V13). S47's surface table
classifies each (CR-TC1).

## VG19 — Agent tools (one definition, two doors)

Catalog specs (`internal/records/catalog.go`, data only, registered-not-granted, descriptions ≥ 40 chars, strict `additionalProperties:false`, no
identity/status/kind fields): **reads** `verify_list`, `verify_show`, `verify_runs` (own-loop scoped list for agents), `mergerule_show`,
`mergerule_preview`, `dod_list`; **writes** `verify_propose_add` and `mergerule_propose_tighten` (inert proposals — VG17), `dod_mark` (direct
but evidence-bound — VG13). Nine specs; exactly three `Mutates:true`. `TestRegistryExactNames` bumped **deliberately and counted** from T0's real
number. No spec is twinned to `verify.add/update/archive/try`, `mergerule.set/clear`, `proposal.commit`.

## VG20 — The CLI (P5)

`aeon verify list|show|add|update|archive|restore|try|runs|run-show|apply-template|vocabulary|templates`, `aeon merge-rule get|set|clear|preview|changes`,
`aeon dod list|mark`, `aeon merge events`. Every verb is a client of a socket method (parity test both directions). Offline fallback (`directDoor`
over the same `internal/verify`/`internal/mergerule` code, actor `{cli,cli}`, the fixed stderr line) for the pure record verbs; **`verify.try`
requires a daemon** (it spawns a process under the daemon's policy gate; a CLI process running arbitrary commands with no daemon policy store
is exactly the second implementation P5 forbids). `--yes` is a seatbelt for `set` loosening and `archive`; no flag accepts a secret.

## VG21 — Seeds and exemptions (P8)

- **`verify.templates`** — an **inert** seed set (`internal/seed/decl_verify.go` + `internal/verify/seed`): table `verify_template(id, stack, name,
  command, requirement, timeout_s, origin, version…)`, system rows with `origin='system'`. A template **does nothing** until a person applies it to
  a component (`verify.applyTemplate` copies rows with `origin='template'`). Source content: go (`go build ./...`, `go vet ./...`,
  `go test ./... -count=1 -timeout 300s` — each wrapped in `timeout N` exactly as `golang-engineer.md:23-28` does), node (`npm run build`,
  `npm test -- --run`, `npm run lint`), python (`python -m pytest -q`, `ruff check .`), rust (`cargo build`, `cargo test`, `cargo clippy`), terraform
  (`terraform validate`, plan-only). Exact commands are T10's to pin from the handler bodies and verify; per-record `Version` guarded by a golden.
- **`merge_rule` is exempt from seeding**, like triggers (S40 AU1 lock 3): a default rule that loosens the baseline is guard-affecting, and a
  default that tightens it would silently change merge behaviour for every component. The baseline lives in code (`mergerule.Baseline()`), pinned
  by T1's goldens, never as rows.
- `verify_command` rows are never seeded (per-component, execution-shaping).

## VG22 — UI (a Settings panel, P7; the build follows S44-extra)

A **Gates** panel on the component's Settings page: effective gate list with source (`own|root`), requirement chips, timeout, "memory: declared, not
enforced", last run state and time; add/edit forms rendered from `verify.vocabulary`; "copy from root"; "apply template"; **Try** (confirms with the
daemon's own statement of what will run and as whom, `useConfirm`); a **Merge rule** section (`mergerule.get` + `preview` with every refusal sentence
verbatim and the autonomy-floor result as a checklist); a **Runs** list/detail (digests, exit codes, tails behind an "output may contain secrets"
notice); a **Definition of done** list on the loop screen; `rpc.guard.test.ts` per P7. Refusals verbatim with the draft kept; no vocabulary typed in the UI.

## VG23 — Security notes

No secret is stored, passed or logged: the scrubbed env, no tails in events/bugs/audit, token source untouched (`DefaultTokenSource`). A gate command is
authority (a command a loop will run): human-authored, policy-gated at run time, agents propose-only. `verify.try` is a human-door spawn. No new network
call other than the forge reads already allowed to the same host. Nothing here is added to the narrow surface (a sibling guard). Merge stays
irreversible-class: this slice adds *evidence and a rule*, never an unattended-merge permission (S56). `forge.postReview` is made unreachable, not widened.

## VG24 — Order and coexistence

T1–T2, T4 need only today's code. T3/T5 need S44-extra (`component` tree, `EffectiveComponent` pattern). T8/T9 need S45 (`desk`, generic
`proposal.commit`, guard pattern) and S47's surface table. T10's seed set needs S43-extra T3. S48 must edit `dev-loop` for the rung to be used
(CR-BP1); until then `verify.run` is exercised by a fixture blueprint and `verify.try`. S55 LC (cancel), S56 AL (floor consumer), S58 SV, S59 EV,
S60 MQ, S63 RT consume this slice's rows and events.

## Open decisions (default chosen; recorded so a reviewer can overrule)

| # | Question | Default and why |
| --- | --- | --- |
| OD1 | Gate lists: merge or replace down the tree? | **Replace** — "what runs here" must be answerable on the child's own screen; copy-from-root covers the merge case |
| OD2 | Does a required gate `fail` short-circuit later gates? | **No** — one rework round sees every red |
| OD3 | Identity the policy gate evaluates gate commands as | **`verifier-engineer`** (keeps today's permitted set), override via the step param `agent` |
| OD4 | Aeon's own review counts toward merge? (AG4) | **Only when the rule says so**; baseline `forge` |
| OD5 | Auto-revert on post-merge red? | **No** — file a triage bug and emit an event; the S29 → S26 path turns it into an ordinary fix loop. A revert needs a `git.revert` action and an outward grant (S56) |
| OD6 | Store output tails? | **Digest + counts always; tails ≤ 2 KiB only for non-pass, best-effort redacted, human-readable only** |
| OD7 | May `require_dod` be waived for autonomy? | **No** in v1 — the floor requires `all_met`; revisit with S61 GI (charters) |
| OD8 | `verify.try` while a loop is active in the same root | **Allowed, and the response says so** (it can race the loop's tree; refusing would block authoring); S59 EV isolates |
| OD9 | Memory bound enforcement | **Declared, not enforced** (no portable bound in `Spawn`; `ulimit -v` is unreliable on macOS) — request CR-EV2 |
| OD10 | Which failures file bugs | **Terminal only** (verify-terminal, ci-red-at-merge, post-merge-red), deduped per (root, subject, kind) |

## Cross-slice requests

| Id | To | Request |
| --- | --- | --- |
| CR-BP1 | S48 | Edit `dev-loop` (and `development-map-*`) to add a `verify-local` code-action rung (`verify.run`, `on_reject` → implement, `counter` + `max`, `then: escalate`) before the `verify` model-step; the validator learns `verify.run`'s param (`gate`, `agent`) and refuses `forge.postReview` |
| CR-AL1 | S56 | The merge grant consults `mergerule.AutonomyFloor(rule, gates)` and refuses to arm below it; the grant is separate from `allow_outward`, and S57 never grants |
| CR-AL3 | S56 | If Aeon should *post* its own review on the forge, that is an outward grant and a new action path; S57 retires `forge.postReview` from blueprints meanwhile |
| CR-LC1 | S55 | `verify.run` honours loop stop (ctx cancel + group kill → `unverified: cancelled`); its wall time counts toward `wall` budgets; the watchdog treats a running verify run as progress |
| CR-EV1 | S59 | App secrets and per-environment env records for gates; network/filesystem isolation; the allowlist in VG5 becomes the floor |
| CR-EV2 | S59 | Enforce `memory_mb` (cgroup/VM limit); until then views say "not enforced" |
| CR-MQ1 | S60 | The merge queue evaluates the merge rule on the **rebased head SHA** and re-runs required gates; `verify_run.head_sha` keyed evidence makes stale results detectable |
| CR-SV1 | S58 | Notify hints/events for `verify.failed`, `merge.refused`, `merge.watch.red`; the closed hint set (`daemon/lifecycle.go:187-197`) is S58's to widen |
| CR-TM1 | S51 | Reserve the `system:*` filer namespace when `filed_by` migrates to agent slugs; `system:gates` is S57's |
| CR-TC1 | S47 | Classify the new socket methods in the surface table |
| CR-RT1 | S63 | Gate pass/fail rates and merge outcomes come from `verify_run`, `merge_event`, `merge_watch` |

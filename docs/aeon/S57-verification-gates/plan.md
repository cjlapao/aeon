# Plan — S57 Verification gates and merge-on-green

## What is settled before planning

`brief.md` (the measured gap and acceptance) and `design.md` VG1–VG24. Short version: verification becomes **code** — a local `verify.run`
action runs a component's declared gates under the existing bash policy gate with a scrubbed environment and records exit codes as
append-only evidence; the forge learns to read CI checks (`ReadChecks` by commit SHA) and review timestamps; a per-component **merge rule**
(baseline = today's gate) decides what "green" means, evaluated by one pure function; acceptance criteria get a `dod_item` ledger bound to
evidence; every merge is recorded and watched cold on the base branch; terminal failures file one unvalidated triage bug. Authority-shaping
writes are human-door socket methods; agents read and propose tightenings only.

## How we will do it

**Characterize first** (T1, P2 Phase A): pin today's `mergeGate` (every branch), the GitHub request shapes against an httptest server, the
engine's code-action path, the `bash` tool's inherited environment, triage filing and the terminal reactors — all on untouched code. Then the
**foundations that need nothing else**: forge checks (T2) and the gate runner with the scrubbed environment (T4). Then the **records** (T3, T5) on
the S44-extra tree, the **engine join** (T6), the **watch and triage reactor** (T7), the **doors** (T8 socket + guards + proposals, T9 agent
tools, T10 CLI + seed set, T11 UI), and close-out (T12). T6 is the only task that changes the loop engine; its golden is T1's engine golden.

## Task sequence

### T0 — Re-verify, inventory, parity table, baseline (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**

### T1 — Characterization goldens: the gate, GitHub, the engine's action path, the bash environment, triage filing ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.**

### T2 — Forge checks: `ReadChecks`, `ListStatuses`, `PR.HeadSHA`, `Review.SubmittedAt`, the Fake ([`tasks/T2.md`](tasks/T2.md))
**Depends: T1.** Parallel-safe with T4.

### T3 — Verify records: migration M1, the store, the effective set, the change log ([`tasks/T3.md`](tasks/T3.md))
**Depends: T0; needs S44-extra merged** (component tree). Parallel-safe with T2, T4.

### T4 — The gate runner and the scrubbed environment (`internal/verify`, `tools.SpawnOptions.Env`) ([`tasks/T4.md`](tasks/T4.md))
**Depends: T1.** Parallel-safe with T2, T3.

### T5 — The merge rule, the DoD ledger, the merge event: migration M2, the pure evaluator, the autonomy floor ([`tasks/T5.md`](tasks/T5.md))
**Depends: T2, T3.**

### T6 — Engine integration: `verify.run`, routing, the refactored gate, `merge_event`, `forge.postReview` retired ([`tasks/T6.md`](tasks/T6.md))
**Depends: T4, T5.**

### T7 — Post-merge watch and failures-to-triage: migration M3, `internal/gatewatch`, the terminal reactor ([`tasks/T7.md`](tasks/T7.md))
**Depends: T2, T5, T6.**

### T8 — The daemon surface: socket methods, desk validators, two proposal kinds (migration M4), the human-door guards ([`tasks/T8.md`](tasks/T8.md))
**Depends: T3, T5, T7; S45 merged (desk, `proposal.commit`, guard pattern).** Gates T9–T11.

### T9 — Agent tools: nine catalog specs ([`tasks/T9.md`](tasks/T9.md))
**Depends: T8.** Parallel-safe with T10, T11.

### T10 — The CLI client and the `verify.templates` seed set ([`tasks/T10.md`](tasks/T10.md))
**Depends: T8; the seed set needs S43-extra T3.** Parallel-safe with T9, T11.

### T11 — The Gates panel in Settings ([`tasks/T11.md`](tasks/T11.md))
**Depends: T8; the shared UI components from S45 T9.** Parallel-safe with T9, T10.

### T12 — Close-out: acceptance re-run, migration drills, demo walk, the record ([`tasks/T12.md`](tasks/T12.md))
**Depends: T0–T11.**

## Dependency graph

```text
T0 → T1 ─┬─ T2 ──────────┐
         ├─ T4 ──────────┤
S44-extra ─ T3 ──────────┴─ T5 ─ T6 ─ T7 ─ T8 ─┬─ T9  ─┐
                                    (S45 merged)├─ T10 ─┼─ T12
                                                └─ T11 ─┘
```

## Risks

| Risk | Mitigation |
| --- | --- |
| Refactoring `mergeGate` changes a refusal sentence or an evidence key an existing test or loop depends on | T1 pins every branch and sentence first; T6's refactor must leave the baseline goldens byte-identical; the only intended production change is checks becoming live, listed in Notes |
| A gate leaks a secret through the inherited environment or a printed output | allowlist env with two canaries (T4); tails only for non-pass, redacted best-effort, never in events/bugs/audit; honest "best-effort" wording; S59 EV request |
| A required gate silently passes on nothing (no gates, denied, timed out) | the aggregate maps every non-run to `unverified` and empty to `no-gates-configured`; named tests per case (T4) |
| Stale evidence lets an unverified commit merge | every result and aeon-review token carries the head SHA; the evaluator ignores any other SHA (T5 `TestStaleEvidenceNeverCounts`) |
| `verify.run` as a code-action does not fit the engine's "action returns error or evidence" shape | VG7 maps `verify_state` after a successful dispatch; T6's tests drive a loop to each of pass/fail/unverified/no-on_reject |
| The check-runs API pagination or rate limits mislead the gate | `Complete=false` ⇒ `unknown` ⇒ refuse when required; retries already exist (O5); httptest fixtures for pages and 403/429 (T2) |
| The post-merge watch polls forever or double-files bugs | bounded window, `expired` state, unique key on `merge_event`, per-subject dedupe, rows not memory decide after restart (T7) |
| Tightening-only proposals are bypassed by a crafted payload | `desk.Validate` is the same function at create and commit; `tightens()` is field-complete with a test that adds a field to `merge_rule` and fails until `tightens` handles it |
| S44-extra, S45, S48 land late | T1, T2, T4 need none of them; T3/T5 need the tree; T8 needs S45; CR-BP1 keeps the blueprint edit out of this slice |

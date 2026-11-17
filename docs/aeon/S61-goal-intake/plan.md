# Plan — S61 Goal intake

## What is settled before planning

`brief.md` and `design.md` GI1–GI20. Short version: a **charter** is an append-only revision history per root component
and grants nothing; acceptance criteria become versioned goal data (`goal_criterion`); an **intake** is a person-driven
session in a scratch workspace that produces one inert `intake_plan` proposal; **one commit door**, `intake.commit`,
runs a resumable, ledgered sequence (scaffold → git → optional publish → register components → charter → epics/goals/
items) with every goal `in_review` and every item `idea`; `repo.scaffold` and `forge.createRepo` join the closed action
registry as **person-door only** (invisible to blueprints), the latter staged off behind three keys; templates are
seeded, revisioned records free of secrets and host paths; agents read and *propose* only; the wizard is a spec.

## How we will do it

Data first (T1), then the pure charter core (T2) and the template/scaffold machinery (T3) in parallel, then the two
bootstrap actions (T4). The proposal kinds (T5) join the charter and plan validators to the S45 desk. The commit engine
(T6) is the integration of everything before it and is the riskiest task, so it is characterized by a crash-matrix test
(kill between every pair of steps). Only then the doors: daemon methods and guards (T7), agent tools (T8), the seeded
agent/blueprint and scratch-workspace start (T9, after S43-extra and S48 have merged), the CLI (T10), the wizard
descriptor and spec (T11), close-out (T12).

## Task sequence

### T0 — Re-verify, inventory, parity table, baseline (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.** Re-proves V15 (a loop runs in a non-component directory) and the roadmap provenance vocabulary.

### T1 — Migration and store: charter, criteria, intake, templates, ledger ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0, S44-extra T3 merged.**

### T2 — `internal/charter`: the closed body, validation, revisions, descriptions ([`tasks/T2.md`](tasks/T2.md))
**Depends: T1.** Parallel-safe with T3.

### T3 — Templates and `internal/scaffold`: renderer, path guards, the `templates.scaffold` seed set ([`tasks/T3.md`](tasks/T3.md))
**Depends: T1, S43-extra T3 merged.** Parallel-safe with T2.

### T4 — Bootstrap actions: `repo.scaffold`, `forge.createRepo`, person-door only ([`tasks/T4.md`](tasks/T4.md))
**Depends: T3.**

### T5 — Proposal kinds `charter_save` and `intake_plan`, with validators and the desk apply ([`tasks/T5.md`](tasks/T5.md))
**Depends: T2, S45 desk merged.**

### T6 — The intake engine and `intake.commit`: ledger, resume, crash matrix ([`tasks/T6.md`](tasks/T6.md))
**Depends: T1, T2, T3, T4, T5.**

### T7 — Daemon methods, errors, conformance, and the human-door guard amendment ([`tasks/T7.md`](tasks/T7.md))
**Depends: T6.**

### T8 — Agent tools: reads and propose-only twins ([`tasks/T8.md`](tasks/T8.md))
**Depends: T7.** Parallel-safe with T9, T10, T11.

### T9 — The `intake-architect` agent, the `goal-intake` blueprint, `intake.start` and the scratch workspace ([`tasks/T9.md`](tasks/T9.md))
**Depends: T7, S43-extra merged, S48 merged.**

### T10 — The CLI: `aeon charter`, `aeon template`, `aeon intake` ([`tasks/T10.md`](tasks/T10.md))
**Depends: T7.** Parallel-safe with T8, T9, T11.

### T11 — The wizard descriptor and the UI spec (no UI code) ([`tasks/T11.md`](tasks/T11.md))
**Depends: T7.** Parallel-safe with T8–T10.

### T12 — Close-out ([`tasks/T12.md`](tasks/T12.md))
**Depends: everything.**

## Dependency graph

```text
T0 → T1 ─┬─ T2 ──────────────┐
         └─ T3 ─ T4 ─────────┼─ T6 → T7 ─┬─ T8 ──┐
              S45 desk ─ T5 ─┘           ├─ T9 ──┼─ T12
                                          ├─ T10 ─┤
                                          └─ T11 ─┘
S44-extra T3 ── T1     S43-extra ── T3, T9     S48 ── T9
```

## Risks

| Risk | Mitigation |
| --- | --- |
| A scaffold writes outside the target or clobbers existing work | new-path-only rule, `EvalSymlinks` on the parent, `O_EXCL`, per-write `Lstat`, a path-escape matrix test (T3), removal only of what the run created |
| A person-door action becomes reachable from a blueprint or an agent | `Entry.Door`, `Names()` excludes it, caller check, sibling guards with canaries (T4, T7) |
| Remote repo creation publishes confidential code | off by default, private by default, three keys, `confirmPublic`, never charter text in the request (T4) |
| Half-finished commit (crash between steps) | the `intake_step` ledger, evidence-verified skips, a crash-matrix test killing between every pair of steps (T6) |
| The `goal` rebuild in S44-extra T3 swallows the new child table | T1 starts only after S44-extra T3; its FK list is read from `pragma_foreign_key_list`, not assumed |
| A loop cannot run in a scratch directory that is no component's root | T0 proves it first; if false, the slice stops and records the alternative (register a hidden scratch component) |
| Template content leaks a host path or a secret shape | content-scan test + per-record version golden (T3) |
| The charter is mistaken for authority | the "grants nothing" import guard with a canary (T7); UI spec shows "not applied" (T11) |

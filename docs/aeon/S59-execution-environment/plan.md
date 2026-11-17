# Plan — S59 Execution environment

## What is settled before planning

`brief.md` (the measured leak, the goals, the acceptance) and `design.md` EV1–EV22 + ten open decisions with
defaults. Short version: an **environment record** (a row, bound at workspace / root component / child, resolved
per step, no code fallback) with a **backend** (`host-inherit` legacy, **`host-scrubbed`** default, **`container`**
built, `vm` a slot); the child environment is **built from an allowlist and injected secret references, never
inherited** (an opaque `ChildEnv` that `Spawn` demands positionally); **unattended loops never run inherited**; secrets
are **references** whose output is **redacted** at the one capture boundary; a **provision ledger** written ahead and
reconciled at startup; per-loop **services**; human-door writes with guards (including a no-inherit spawn guard),
agent reads and one restrictive-only proposal; daemon methods → CLI client → tools → a Settings panel.

## How we will do it

Characterize first (T1, on the untouched code), then lay the data and the pure core side by side (T2 schema/store,
T3 core), then secrets and redaction (T4), then the one risky change to existing behaviour — the execution seam
(T5), done **characterization-first** so the host path stays byte-identical where it should — then the doors (T6),
the agent surface (T7), the CLI (T8). The container backend (T9) and services (T10) build on the seam and the
ledger; the panel (T11) waits on S45's shared components. Close-out last (T12). Because the seam and the schema must
agree, **T2–T5 merge as one unit**; T1's goldens gate it.

## Task sequence

### T0 — Re-verify, inventory every spawn site and secret sink, prove the S32 finding, list the approvals (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing.**

### T1 — Characterization goldens: bash, git, terminal, MCP environments and the sink scan harness ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.**

### T2 — The migration and the store's SQL half ([`tasks/T2.md`](tasks/T2.md))
**Depends: T1.** Parallel-safe with T3. Merges with T3–T5.

### T3 — `internal/execenv` core: spec, validation, allow/deny, `ChildEnv`, `Restricts`, capabilities ([`tasks/T3.md`](tasks/T3.md))
**Depends: T0.** Parallel-safe with T2.

### T4 — Secret references and redaction ([`tasks/T4.md`](tasks/T4.md))
**Depends: T3** (the `Secret`/`Redactor` types live in the core; the store half needs T2 for the reference table).

### T5 — The execution seam: `Spawn` demands a `ChildEnv`, executors, resolution per step, the unattended rule, the other spawn sites ([`tasks/T5.md`](tasks/T5.md))
**Depends: T2, T3, T4.** Needs **S44-extra** (`parent → root` resolution).

### T6 — The daemon doors, the seed set and the guards ([`tasks/T6.md`](tasks/T6.md))
**Depends: T2, T3, T5**; **S43-extra T3** for the seed set.

### T7 — Agent tools and the `environment_restrict` proposal kind ([`tasks/T7.md`](tasks/T7.md))
**Depends: T6, S45** (`internal/desk`, generic `proposal.commit`). Parallel-safe with T8.

### T8 — The CLI: `aeon env` and `aeon secretref` ([`tasks/T8.md`](tasks/T8.md))
**Depends: T6.** Parallel-safe with T7, T9.

### T9 — The container backend, the provision ledger and reconcile ([`tasks/T9.md`](tasks/T9.md))
**Depends: T2, T5.** Parallel-safe with T7, T8.

### T10 — Services and the preview refusal ([`tasks/T10.md`](tasks/T10.md))
**Depends: T9.**

### T11 — The Environments panel in Settings ([`tasks/T11.md`](tasks/T11.md))
**Depends: T6, S45 T9** (shared UI components). Parallel-safe with T7–T10 once T6 is done.

### T12 — Close-out ([`tasks/T12.md`](tasks/T12.md))
**Depends: everything.**

## Dependency graph

```text
T0 → T1 ─┬─ T2 ─┐
         └─ T3 ─┴─ T4 ─ T5 ─┬─ T6 ─┬─ T7 ──┐
                            │      ├─ T8 ──┤
                            │      └─ T11 ─┤
                            └─ T9 ─ T10 ───┴─ T12
   S44-extra ─→ T5      S43-extra T3 ─→ T6      S45 ─→ T7, T11
```

## Risks

| Risk | Mitigation |
| --- | --- |
| Scrubbing breaks a command that relied on an inherited variable | unattended-only by default (EV6/D1); attended loops keep legacy on upgrade; one explicit `passthrough` entry fixes a name; goldens (T1) prove the host path is otherwise byte-identical |
| The refactor of `Spawn` changes timeout/kill/cap behaviour | P2 characterization first (T1); `runProcess` is extracted, not rewritten; every existing `spawn_test.go` case stays green with listed adaptations |
| Redaction gives false confidence | limits stated in the tool description and vocabulary (EV8); sentinel scan of every sink (T4/T12); redaction is documented as the second line |
| A secret leaks through a sink nobody listed | T0 inventories sinks (transcript rows, events, command-activity text, receipts, audit, logs, protocol responses, the DB file); the sentinel scan reads the raw store file and WAL |
| The container driver leaks resources on a crash | write-ahead ledger + labelled-only reconcile at start (T9); fake-runtime crash tests at every step boundary |
| A new spawn site appears later and inherits again | `TestNoSpawnInheritsTheDaemonEnvironment` with a canary (T6); `Spawn` cannot be called without a `ChildEnv` (compile-time) |
| `internal/secrets` API is unreadable here | `store` kind staged behind an explicit owner approval (T0 item, D3); nothing else waits |
| S44-extra not merged when T5 starts | T5's resolution step is written against the tree contract; T0 records merge state; T5 does not start before S44-extra T4 |
| S32's parallel-unit defect makes unit isolation look real when it is not | recorded as a finding (T0), stated in the panel's honesty line (EV15), fixed by S32's follow-up not here |
| An operator widens an environment mid-loop | applied next step, audited with before/after spec (EV17); restrictive agent proposals only |

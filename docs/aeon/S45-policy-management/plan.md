# Plan — S45 Policy management

## What is settled before planning

`brief.md` and `design.md` PM1–PM14 (with the verified facts). Read both first. Short version: the daemon gains
everything the CLI has for policy (authoring, removal, seeding, proposal commit/reject) as a **human-door socket
surface** that amends PL15/TD7 (operator ruling); one implementation (`internal/desk`) serves the daemon and the CLI;
agents get read tools and **propose-only, restrictive-only** tools through the records catalog; the authoring is
audited in its own append-only table; and the app gets a descriptor-driven Policy section with a reusable agent picker.

## How we will do it

**Characterize before moving** (T1): the CLI's byte output and the S35/S10 behaviour are pinned *first*, then the
effect code moves out of `cmd/aeon` into `internal/desk` without changing a byte. **Reads before writes** (T2, T3 in
parallel): the detail/vocabulary reads and the audit table are safe, additive and unblock everything. **Writes next**
(T4, T5): the authoring and proposal-decision methods, each with before/after and the audit. **Then lock the door
properly** (T6): the PL15/TD7 guards are rewritten positively and canary-tested *before* any client (CLI, tool, UI) is
built on the new methods. **Then the clients** (T7 tools, T8 CLI, T9 UI — parallel-safe), then the S43-dependent
`agent_import` commit (T10), then close-out (T11). This mirrors S35's sequencing (foundation → writes → guard
amendment → clients) and S38's "one catalog, two doors" for the tools.

## Open questions this plan answers

| Question | Answer | Where |
| --- | --- | --- |
| May authoring ride the socket? | Yes, as a human door; narrow/agent/tool/catalog surfaces stay closed, guard-pinned | PM1, PM10 *(operator ruling)* |
| Where does shared code live? | New `internal/desk`; func seams for kinds the daemon may not import | PM2 |
| Does the CLI keep an offline path? | Yes, through the same `internal/desk` code | PM4 *(product call)* |
| How is authoring audited? | New append-only `policy_change` (T0 confirms) | PM8 |
| What can an agent do? | Read; propose `deny`/`ask` only; never commit | PM9 |

| Are shipped policies records too? | Yes — seeded as `policy.corpus`, guard class, `origin='system'`, removable but never resurrected | PM15, PM16, T12 |

The T0 items (design "Open items left to T0") are mechanical.

## Task sequence

### T0 — Re-verify, parity inventory, settle the mechanical items (no code) ([`tasks/T0.md`](tasks/T0.md))

Drift check of facts 1–11, the full CLI×flag→socket parity table, the audit-table question, the actor question, a
read-only look at S43's state, the error-code block, the pre-amendment guard hashes. **Depends: nothing.**

### T1 — Characterize the CLI, then extract `internal/desk` ([`tasks/T1.md`](tasks/T1.md))

Golden-output tests for every `aeon policy`/`aeon proposal` verb first; then move the effect registry, validators,
seed-corpus runner and boundary constructor from `cmd/aeon` into `internal/desk`, behaviour-preserving, with func
seams for the kinds the daemon may not import. **Depends: T0.**

### T2 — Detail reads: `policy.get`, richer `policy.list`, `policy.vocabulary`, `Describe` ([`tasks/T2.md`](tasks/T2.md))

`Describe(Policy) string` on each per-type validator (the one formatter); the three read methods; fixtures, handshake,
protogen. **Depends: T0.** Parallel-safe with T3 (no shared file).

### T3 — The authoring audit: `policy_change` and its reads ([`tasks/T3.md`](tasks/T3.md))

One timestamped migration (append-only by trigger), store accessors, the `policy.changes` read. **Depends: T0.**
Parallel-safe with T2.

### T4 — Socket authoring: `policy.add`, `policy.remove`, `policy.seedCorpus` ([`tasks/T4.md`](tasks/T4.md))

The daemon's authoring service over `internal/desk`; peer-stamped actor; PL1 verbatim; before/after; audit; the
seed dry-run. **Depends: T1, T2, T3.**

### T5 — Socket proposal decisions: `proposal.get`, `proposal.commit`, `proposal.reject` ([`tasks/T5.md`](tasks/T5.md))

Digest-bound commit, effect-then-flip with crash-safe retry moved intact, reject with a mandatory reason, before/after,
audit. **Depends: T1, T3.** (Needs T4's service wiring if built first; otherwise they share it — see the task.)

### T6 — The guard amendment: PL15/TD7, positively, with canaries ([`tasks/T6.md`](tasks/T6.md))

Sibling guard functions, pre-amendment assertions byte-preserved by hash, the whitelist updated, canaries, the rule
text updated in `AGENTS.md` and annotated in S10/S35. **Depends: T4, T5.** Gates T7–T9.

### T7 — Agent tools: seven catalog specs, read + propose-only ([`tasks/T7.md`](tasks/T7.md))

`internal/records` additions with socket twins, daemon records-service implementations, the counted registry bump,
forced `source: "agent"`, restrictive-only. **Depends: T2, T6.**

### T8 — The CLI as a client ([`tasks/T8.md`](tasks/T8.md))

`aeon policy`/`aeon proposal` dial the daemon; offline fallback through `internal/desk`; byte-identical goldens from T1;
the parity guard. **Depends: T4, T5, T6.** Parallel-safe with T7, T9.

### T9 — The Policy section in Settings, and the agent picker ([`tasks/T9.md`](tasks/T9.md))

Descriptor-driven UI over the socket only. **Depends: T2, T4, T5, T6, and S43 merged** (the picker). Parallel-safe with
T7, T8.

### T10 — Commit an `agent_import` proposal over the socket ([`tasks/T10.md`](tasks/T10.md))

S45's own task, run after S43 merges: moves the `agent_import` effect behind the same registry. **Depends: T5 and
S43 merged.** Closed "not applicable" if S43 changes the kind away.

### T12 — The `policy.corpus` seed set: system policies become seeded, origin-marked rows ([`tasks/T12.md`](tasks/T12.md))

Applies the "every record is a row" rule to policy: migration for `policy.origin`, `internal/policyseed` (source +
guard-class applier), `Store.AddInTx`, the seed-corpus verb reduced to a printer over the engine. **Depends: T1, T3 and
S43-extra T3 (the engine).** Gates T4's `policy.seedCorpus` and T5's `seed_corpus` effect.

### T11 — Close-out ([`tasks/T11.md`](tasks/T11.md))

Re-run the thirteen acceptance criteria fresh, the migration drills, the DEMO §S45 walk, the record. **Depends: T0–T10
and T12** (T10 may close "not applicable"/deferred; the slice is then Partial, stated).

## Sequencing summary

```
S43-extra T3 (engine) ─┐
T0 ─▶ T1 (characterize + extract) ──┼─▶ T12 (policy.corpus seed set) ─┐
                                     │                                 ▼
T0 ─▶ T1 ───────────────────────────┬─▶ T4 (policy.add/remove/seed) ─┐
 ├──▶ T2 (reads, Describe) ─────────┤                                ├─▶ T6 (guards) ─┬─▶ T7 (tools) ─┐
 └──▶ T3 (policy_change) ───────────┴─▶ T5 (proposal get/commit/reject) ┘                ├─▶ T8 (CLI)   ─┼─▶ T11
                                                                                          └─▶ T9 (UI) ────┤
S43 merged ─▶ T10 (agent_import commit; also needs T5) ──────────────────────────────────────────────────────┘
```

## The numbers this slice must hold

| Fact | Count |
| --- | --- |
| New socket methods | 9: `policy.get`, `policy.vocabulary`, `policy.changes`, `policy.add`, `policy.remove`, `policy.seedCorpus`, `proposal.get`, `proposal.commit`, `proposal.reject` (+ `policy.proposeAdd`/`proposeRemove` as the propose tools' twins = 11) |
| `policy.list` | gains filters and `spec`/`describe` fields (additive) |
| New migration files | 2 (`policy_change`; `policy.origin`) |
| Production seed sets declared by this slice | 1 (`policy.corpus`, class **guard**) |
| Policy rows told apart by an id prefix after T12 | **0** (`origin` column) |
| New catalog specs / tools | 7 (`TestRegistryExactNames` bumps by 7, deliberately) |
| Tools granted by default | **0** of the 7 |
| Tools that can commit/reject/add/remove/seed | **0** |
| New typed protocol errors | a block reserved by T0 |
| Existing guard-test bodies edited | **0** (siblings added; pre-amendment assertions byte-preserved by hash) |
| S43 files edited | **0** |

## Cross-slice coupling

- **S43-extra** (seed engine): a dependency of T12 (T3 of that slice). S45 declares `policy.corpus` against it and adds
  `origin` to `policy`; it does not change the engine.
- **S43** (agents/slugs): read-only; a dependency for T9/T10 and for target/seed-id behaviour (PM3, PM13).
- **S44** (config store): not a dependency. If a later change wants a policy *default* as a setting, that is its slice.
- **S46** (MCP management) reuses T9's `AgentPicker` and the `internal/desk` registry; S45 moves the two MCP proposal
  kinds' effects behind the registry unchanged and leaves MCP's own surface to S46.
- **S47** (tools catalog) will list the seven new tools; S45 provides their catalog specs.

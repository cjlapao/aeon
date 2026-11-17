# Brief — S52 Memory (learnings) management

Written 2026-10-30. **Stage:** 5 · Fleet — Tier 3 of [`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md): "a learning is scoped to an agent, so it wants S43's slugs".
Shapes from [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (**P1–P11**). **Depends on:** S28 (learnings), S43 (agent slugs), S22 (retrieval index), S45 (desk/commit, UI), S47 (surface). T0 re-checks.

## 0. Why this one is different

A learning is **memory that shapes what agents do**: retrieved learnings reach prompts. A bad one (a wrong `told`, a poisoned `assumed`) steers an agent with nobody watching. S28's answer was **provenance honesty, no review gate** (M3) and `relayed` trusted below all others (M1). Management therefore
means making memory **visible, attributable and retractable** — not gating it. Retraction **narrows** what agents see (so agents may *propose* it); adding memory stays as S28 designed it.

## 1. What exists today, measured (2026-10-30)

| Fact | Evidence |
| --- | --- |
| **Socket `learnings.add/list/show/query`; CLI `aeon learnings add|list|show|query`**; bench `LearningsPage.tsx`; migration for `learning` + `learning_tag` (tags as a join table, M5). | `method.go`, `cmd/aeon/learnings.go:32-38`, `internal/store/migrations/*learning*.sql` |
| **Row:** `scope ∈ person|project|agent`, `scope_key`, `body`, `provenance ∈ told|observed|assumed|relayed`, `observed_count`, `source_loop_slug`, `source_step_id`, `created_at`. **No edit, delete, retract, or audit of who added it.** | `learning` DDL |
| **`relayed` is write-protected** (M1): only the board-answer hook and the triage feed may write it; the public `Append` refuses it. | `internal/learnings/types.go:56-73`, `daemon/triage_learning_feed.go` |
| **Agent scope keys are agent *names*,** validated by `handler.Lookup(name)` (first-party bodies) or a non-revoked `plugin_agent` row — not S43 slugs; **no S43 migration touched it**. | `internal/learnings/scope.go:24-60` |
| **Self-logging** (M3): an agent's own learning is retrievable immediately; there is no gate. The public door `learnings.add` (`protocol/learnings.go:204`) takes no actor. | S28 M3, `protocol/learnings.go` |
| **Nothing answers "what would agent X be shown?"** and nothing lists memory by *who/what/where* it came from beyond the raw rows. | — |

## 2. What this slice is

1. **Agent scope → slugs** (S43): migrate `scope_key` for `scope='agent'` name→slug (all-or-nothing, idempotent, unresolved refused by name); validation moves from `handler.Lookup`/`plugin_agent` to the agent rows; S43's agent-delete refusal names learnings scoped to the agent.
2. **Retract / restore** (human door, append-only, reasoned): a learning is never deleted — `retracted_at/_reason/_by` columns; retracted learnings are excluded from `query`, `list` and prompt retrieval by default and **removed from / skipped by the retrieval index** (T0 confirms the mechanism); `restore` brings one back. Both audited in `learning_change`.
3. **Actor and audit on `learnings.add`** (peer-stamped; an agent actor may add only `observed`/`assumed`; `told` requires a person; `relayed` stays refused) — closing the "no actor" gap without changing M3's no-gate rule.
4. **"What would this agent see?"** — `learnings.forAgent {agent, query?, limit?}`: the same ranking `query` uses for that agent's scopes (person + project(s) + agent), with each hit's provenance, strength and **why it ranked** (read-only; M1's ordering visible).
5. **Details and honesty in one list**: board read by scope/provenance/tag/source loop, one formatter, never collapsing the four provenances; vocabulary read.
6. **Agent tools** (`learning_query`, `learning_show` reads; `learning_add` self-log under M3; `learning_propose_retract` → new proposal kind `learning_retract`), CLI, and **Settings → Memory**.

## 3. Out of scope

M1/M2/M3/M4/M5/M6 as built (no review gate, no new provenance, board skill unchanged), the retrieval/embedding pipeline (S22), editing a learning's body (memory is append-only — correct it by adding a new one and retracting the old), changing S28/S22/S43 beyond listed adaptations.

## 4. Acceptance criteria

1. **Slugs:** after migration every `scope='agent'` learning's `scope_key` is a slug that resolves; person/project untouched; unresolved refused naming each; idempotent; writes validate against agent rows; deleting an agent with scoped learnings is refused naming the count.
2. **Retraction:** never a delete; excluded by default everywhere memory is read (query, list, prompt injection, board feed) and from the index; restore reverses it; each audited with the peer; reason required.
3. **Audit complete:** add/retract/restore each one `learning_change` row (actor kind person/cli/agent/system); append-only; an agent actor appears only on `add`.
4. **Provenance honesty holds:** an agent cannot add `told` or `relayed`; `relayed` stays refused on every public door; the four provenances render distinctly everywhere (property test); M1 ordering unchanged (golden).
5. **forAgent is faithful:** its ranking equals the real prompt-retrieval ranking for the same inputs (differential test), is read-only, and labels `relayed` below the others.
6. **Agent tools:** reads open; `learning_add` limited to `observed`/`assumed` with a rate limit; `learning_propose_retract` inert until a person commits; no tool retracts, restores, edits or adds `told`/`relayed` (guards + canary).
7. **Daemon first** (parity both ways; writes' doors per P4); **UI** through registered methods only; no regression in S28/S22/S43 suites.

## 5. Handoff

Done: brief, design, plan, tasks T0–T7. Blocked on: S43 (agent rows) for T1; S45 (desk/commit, UI) for T5/T7; S22 index details in T0. T0 first.

> **Amended 2026-10-30 by S44-extra (component tree, CT15):** `project` and `component` are one record — a two-level `component` tree (a root and its children; a root with children *is* a project, derived). Wherever this slice said project, read **root component** (`rootId`/`root_id`/`--root`); scope pickers read `component.list` (`rootsOnly`) / `component.show`; `project.*` methods and `telemetry.project` no longer exist. See [`S44-extra-component-tree/design.md`](../S44-extra-component-tree/design.md).
> Specific to this slice — **exception to the note above:** the learnings `scope='project'` is keyed by a **repository root path**, not a project id (`internal/learnings/types.go:31-33`); S44-extra does not touch it and this slice keeps it (renaming the scope is this slice's call). "The projects an agent has run in" means the **root components** of the loops it ran in.

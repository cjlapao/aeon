# Brief — S51 Triage management

Written 2026-10-30. **Stage:** 5 · Fleet — Tier 4 of [`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md): triage *hands off* into roadmap (S50) and blueprints (S48), so it comes after them.
Task shapes from [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md) (**P1–P11**). **Depends on:** S29 (triage), S26/S50 (hand-off target), S22 (embeddings for duplicates), S43 (agent slugs), S44 (settings), S45 (desk/commit/UI), S47 (surface), S48 (blueprints). T0 re-checks.

## 0. Standing rules carried

Daemon first (parity already holds: 8 verbs both ways); every management operation gets a tool (reads open; **an agent may file a bug — D7 designed self-filing**; nothing else); a list with details; every record a row (bugs are rows, never shipped — nothing to seed).

## 1. What exists today, measured (2026-10-30)

| Fact | Evidence |
| --- | --- |
| **Built, daemon-first.** Socket `triage.file/list/show/set-priority/confirm-duplicate/validate/handoff/suggest-duplicates`; CLI `aeon triage` with the same eight; bench `TriagePage.tsx`; migration 0044 (`bug`) + on-demand `bug_vec` (S22/0040 pattern). | `method.go`, `cmd/aeon/triage.go:33-47` |
| **Two closed sets, never one badge** (D1/D2): validation `unvalidated → {reproduced, not-reproduced, could-not-tell}` (all three terminal; a re-attempt is a *new attempt record*, D3) and priority `known|assumed|unknown` (+ label). `duplicate_of` is set **only by a human-originated call** (D5/N3); `bug_vec` shares no table with other vectors (D6/N5). | `internal/triage/transitions.go`, `types.go` |
| **`filed_by` is "an agent name or 'person'"** (free text) — not an S43 slug. | `internal/triage/store.go:42` |
| **A hidden code choice:** `validate` starts the **`brainstorm`** blueprint (const `triageValidationBlueprint`). Not configurable, not visible. | `internal/daemon/triage_dispatch.go:50` |
| **Hand-off** copies the two priority fields into a roadmap `idea` (`provenance='triaged'`, `bug.roadmap_item_id`). | S29 T3 |
| **No edit** of a bug's title/body after filing, **no audit** of who changed priority/duplicate/hand-off, **no agent tool**. | verbs, schema |

## 2. What this slice is

1. **Management completion**: `triage.update {id, title?, body?}` (a reporter's correction; never touches validation/priority/duplicate/hand-off), `triage.history` (an append-only `bug_change`), a **board** read with details (both state sets rendered separately by one formatter; attempts; duplicate candidates with scores and the human-confirmed link; hand-off status and the roadmap item's status), `triage.vocabulary`, `triage.health`.
2. **`filed_by` moves to slugs** (S43): agent filers stored as the agent's slug (migration: name→slug all-or-nothing; `'person'` stays; unresolved name refuses naming it).
3. **The hidden blueprint choice becomes an S44 setting** `triage.validationBlueprint` (default `brainstorm`), checked at use through S48, health-reported, UI picker.
4. **Agent tools**: `bug_list`, `bug_show` (reads) and **`bug_file`** (a direct catalog write: files an **`unvalidated` claim** with `filed_by = the stamped agent slug` — D7's self-filing; inert; no priority, no duplicate link, no hand-off). Nothing else.
5. **CLI** gains `update/history/board/health/vocabulary`; **Settings → Triage** (promoting the bench page) with the daemon's authority statement before `validate`/`handoff` (they start loops / create roadmap items).

## 3. Out of scope

D1's machine (no new state, no edge), D2's split, D5/N3 (an agent can **never** set `duplicate_of`), D6/N5, scheduling a fix (S26/S50's), the embedding pipeline (S22), changing S26/S29/S43/S44/S48 beyond listed adaptations.

## 4. Acceptance criteria

1. **Behaviour-identical by default**: defaults reproduce today's `validate` dispatch (goldens); S29 fixtures byte-identical.
2. **Edit is safe**: `triage.update` changes only title/body (length rules), preserves everything else, audits once, refuses control characters.
3. **Audit complete**: file/update/set-priority/confirm-duplicate/validate/handoff and reactor-driven validation transitions each one `bug_change` row (actor `system:reactor` for reactor transitions); append-only.
4. **Slugs**: after migration every agent `filed_by` is a slug that resolves; `'person'` unchanged; unresolved refused naming it; new filings store the slug; idempotent.
5. **D1/D2/D5/D6 hold**: the two sets render separately everywhere (property test), `unvalidated` never reads as `could-not-tell`, **no agent-reachable path** sets `duplicate_of` or priority (guards with canaries).
6. **Setting honest**: `triage.validationBlueprint` checked at use with a named refusal, never a silent fallback; health reports it.
7. **Agent tools**: reads open; `bug_file` creates only `unvalidated` claims; no tool validates, prioritizes, links duplicates or hands off.
8. **Daemon first** (parity both ways, new writes absent from narrow/agent services/tool registry); **UI** through registered methods only (P34 grep); no regression in S29/S26 suites.

## 5. Handoff

**Done:** brief, design, plan, tasks T0–T7. **Blocked on:** S43 (slugs) T2; S44 + S48 for T3; S45 for T4/T6. T0 first.

> **Amended 2026-10-30 by S44-extra (component tree, CT15):** `project` and `component` are one record — a two-level `component` tree (a root and its children; a root with children *is* a project, derived). Wherever this slice said project, read **root component** (`rootId`/`root_id`/`--root`); scope pickers read `component.list` (`rootsOnly`) / `component.show`; `project.*` methods and `telemetry.project` no longer exist. See [`S44-extra-component-tree/design.md`](../S44-extra-component-tree/design.md).
> Specific to this slice: bugs stay keyed by `component_root` (unchanged). A board filter that takes a component may name a **root**, and then means the root's whole tree.

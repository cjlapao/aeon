# Design — S51 Triage management

Rulings **TM1–TM11**; patterns from [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md). T0 re-verifies.

## TM1 — Edit is title and body only
`triage.update {id, title?, body?}`: title 1–200, body ≤ 16 KiB, no control characters except newline/tab in body; validation state, priority, `duplicate_of`, `roadmap_item_id`, `filed_by`, timestamps (except `updated_at`) are untouched. Unchanged ⇒ `changed:false`, no audit. Editing a bug that was already validated is allowed (a typo fix) and is noted in the audit summary field list, never by re-opening validation (D3: re-attempt = a new attempt record).

## TM2 — `bug_change` audit (P3)
`bug_change(id, bug_id, op ∈ ('file','update','priority','duplicate','validate','handoff','transition'), summary, actor_kind ∈ ('user','cli','agent','system'), actor_id, occurred_at)`; append-only; no FK. **Unlike policy/MCP/automation, `agent` is a legal actor kind here** because D7 lets an agent file a claim — and *only* `file` may carry it (a CHECK ties `actor_kind='agent'` to `op='file'`, a database net for TM6). Reactor transitions are `system:reactor`.

## TM3 — `filed_by` → slug
S29 stores `filed_by` as an agent **name** or `'person'`. S43's slug is the universal reference. Migration: for each distinct non-`'person'` `filed_by`, resolve against `agent.name` → `slug`; any unresolved value **refuses the whole migration**, naming every one (S43 T2's all-or-nothing); idempotent (a value already equal to a slug is skipped). New filings store `stamped agent slug` or `'person'`. `triage.file` params gain no `filedBy` (the actor decides: person ⇒ `'person'`, agent ⇒ its slug) — **an existing `filedBy` param, if any, is removed from the wire as a forged-identity vector** (T0 reads the current params; if it exists the removal is the one intentional wire change, listed).

## TM4 — The validation blueprint is a setting
`triage.validationBlueprint` (string, default `brainstorm`) declared in `internal/config/settings_decl_triage.go`; read per `validate` through `config.Reader`; existence + `Analyze`-clean checked at use through S48; missing/invalid ⇒ named refusal, **no fallback**; `triage.health` reports it (`ok|missing|invalid|unverifiable`). Not Sensitive. Default reproduces today.

## TM5 — Reads
`triage.board {componentRoot?, validation?, priority?, handedOff?}` (bugs with `Describe`, attempts count/last outcome, duplicate candidates with scores, the confirmed link, hand-off state + the roadmap item's status), `triage.history {id}`, `triage.vocabulary {}` (the two closed sets and D1's legal next states from `AllowedTransitions`, **not restated**), `triage.health {}`. Existing `list/show/suggest-duplicates` unchanged. `Describe(bug)` renders the two sets **separately** (never a combined badge) — fixed templates.

## TM6 — What an agent can do
Catalog specs (family `bug`): `bug_list`, `bug_show` (safe), **`bug_file`** (Mutates, detectable): files a bug via the same `Create` as `triage.file`, `validation_state='unvalidated'`, `priority_state='unknown'`, `filed_by` = the stamped agent slug; schema `{componentRoot, title, body}` only (no identity/state/priority/duplicate fields). **No tool** validates, sets priority, confirms/sets a duplicate, hands off or edits (D5/N3, D2). A per-agent open-claims rate limit (named constant, counted refusal) so a runaway loop cannot flood the backlog. Counted registry bump **+3**, not granted by default. No proposal kind is needed (filing is inert and designed).

## TM7 — Methods and doors
Human-door writes (P4): `triage.update` (new) and the existing `set-priority/confirm-duplicate/validate/handoff` gain actor+audit (params/results unchanged; fixtures byte-identical); `triage.file` stays open to person **and** agent actors (D7) — the one write that is not human-door, therefore guarded differently (TM8). Reads as TM5. Classified in S47's table.

## TM8 — Guards
Human-door guard for `update/set-priority/confirm-duplicate/validate/handoff` (absent from narrow/agent services/tools; the one allowed agent write is the `bug_file` twin and **only** `triage.file`'s agent path); `TestNoAgentReachablePathSetsDuplicateOrPriority` (D5/N3, D2) — walk every agent-reachable door; `TestBugChangeAgentActorOnlyForFile` (the CHECK + Go); the original S29 D1/D2/D5/D6 tests pinned by hash and re-run; canary.

## TM9 — CLI (P5)
`aeon triage` keeps its eight verbs (goldens) and gains `update`, `history`, `board`, `health`, `vocabulary`; `validate` and `handoff` print the daemon's authority statement and need `--yes` (they start a loop / create a roadmap item); `confirm-duplicate` needs `--yes` (a human's irreversible link).

## TM10 — UI (P7)
A Settings → Triage section (the bench `TriagePage` stays): board with the two state sets as **separate** pills and the duplicate candidates with scores; detail drawer with attempts, history, edit; a validation-blueprint picker card (S48 list, health dot, `settings.set`); `validate`/`handoff`/`confirm-duplicate` with the authority confirm; the roadmap link opens S50's screen.

## TM11 — What does not move
D1/D2/D3/D5/D6, the reactor, `bug_vec`, S22's pipeline, S26's `idea` entry point, S29's fixtures, and every sibling file beyond: filed_by migration (T1), dispatch reading the setting (T3), actor+audit on existing writes (T1).

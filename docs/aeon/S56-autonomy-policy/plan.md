# Plan — S56 Autonomy policy

## What is settled before planning

`brief.md` (the gap, the answer to AG1, acceptance) and `design.md` AL1–AL17. Short version: per-root-component **grant** rows (`allow`/`deny`, closed subjects from the
action registry with merge only by its own name, closed conditions, expiry) in their own authority table; schedule/webhook/event/scheduler starts are **pinned** with the
grants that applied (one immutable `loop_autonomy_pin` row, no new `loop` column, legacy `allow_outward` stays 0); at action time a pure `ResolveWithGrant` lets a pinned,
**still-live** grant run an action — **ceiling pinned, floor live**; every effect taken under a grant is logged begun/completed; human-door socket methods, propose-only
restrictive agent tools, an `aeon autonomy` client; descriptors for a later UI. Default: nothing granted, byte-identical behaviour.

## How we will do it

Characterize first (T1 — the whole `Resolve` matrix, the S19/S20/S40 pin tests and the existing guard bodies hashed). Then the pure core (T2) beside the tables and store
(T3) — independent, parallel. Then the two places behaviour actually changes: the start path that builds the pin (T4) and the decision site that reads it (T5) — each
proven against T1's goldens. Then the doors: socket human door (T6) → the guards that must exist **before** any client is built on it (T7) → agent tools and proposal kinds
(T8), the CLI (T9) and the descriptors/UI specification (T10) in parallel → close-out (T11).

The slice is buildable **before S55/S57**: their conditions are func seams that fail closed (AL4), so a merge grant is simply inert until S57 exists. T0 records the merge state of S45, S55, S57, S27.

## Task sequence

### T0 — Re-verify, inventory, parity, baseline (no code) ([`tasks/T0.md`](tasks/T0.md))
**Depends: nothing** (S44-extra and S45 merge state recorded).

### T1 — Characterization goldens and the pre-amendment guard pins ([`tasks/T1.md`](tasks/T1.md))
**Depends: T0.**

### T2 — `internal/autonomy` and `action.ResolveWithGrant`: the pure core ([`tasks/T2.md`](tasks/T2.md))
**Depends: T1.** Parallel-safe with T3.

### T3 — Migration and store: grants, audit, use log, the pin ([`tasks/T3.md`](tasks/T3.md))
**Depends: T1.** Parallel-safe with T2.

### T4 — The start path: `resolveAutonomy` and the pin ([`tasks/T4.md`](tasks/T4.md))
**Depends: T2, T3.**

### T5 — The decision site: live checks, conditions, the use log ([`tasks/T5.md`](tasks/T5.md))
**Depends: T2, T3.** Parallel-safe with T4 (they meet in T11's end-to-end).

### T6 — The socket human door: nine methods and a read set ([`tasks/T6.md`](tasks/T6.md))
**Depends: T3, T4, T5.**

### T7 — The guards: positive whitelist, negative absences, canaries, the amendments ([`tasks/T7.md`](tasks/T7.md))
**Depends: T6.** **Gates T8, T9, T10.**

### T8 — Agent tools and the two proposal kinds ([`tasks/T8.md`](tasks/T8.md))
**Depends: T7, S45 T4/T5/T7.** Parallel-safe with T9, T10.

### T9 — The CLI: `aeon autonomy` ([`tasks/T9.md`](tasks/T9.md))
**Depends: T7.** Parallel-safe with T8, T10.

### T10 — Descriptors, the effective-view contract and the UI specification ([`tasks/T10.md`](tasks/T10.md))
**Depends: T7.** Parallel-safe with T8, T9.

### T11 — Close-out ([`tasks/T11.md`](tasks/T11.md))
**Depends: everything.**

## Dependency graph

```text
T0 → T1 ─┬─ T2 ─┬─ T4 ─┐
         │      └─ T5 ─┼─ T6 → T7 ─┬─ T8  ─┐
         └─ T3 ────────┘           ├─ T9  ─┼─ T11
   (T3 also feeds T4 and T5)       └─ T10 ─┘
```

## Risks

| Risk | Mitigation |
| --- | --- |
| A grant quietly widens what `Resolve` always denied (an unattended irreversible) | `Resolve` is untouched; the only path to an unattended irreversible `Run` is `ResolveWithGrant` rule 5; exhaustive matrix test; T1 goldens run unedited at close-out |
| `allow_outward` flipped to 1 for policy loops would let **every** outward action run | AL8: legacy flag stays 0; a test asserts `allow_outward = 0` on every policy-pinned loop (keeps S40 AU2 literally true) |
| Revocation not seen by a running loop | live read at every unattended non-local action (AL6); end-to-end test revokes mid-run |
| A grant widens a running loop | pin is immutable (triggers); a test adds a grant after the pin and proves the loop's action is still denied |
| A condition seam missing ⇒ silent allow | `cannot-say ⇒ deny`; a test with every seam nil |
| An existing guard edited to make room | T1 hashes the guard bodies; T7 adds siblings only; `TestAmendmentDoesNotLoosenOtherGuards` |
| New code trips `TestClassesAreNotConfigurable` (substring grep over `internal/action`) | `grant.go` avoids the banned words including in comments; T2 runs the test first |
| Narrow surface gains a grant vocabulary | no edit under `internal/narrow`; `TestNoKeyLevelGrantExists` untouched; T7 sibling asserts no `autonomy.*` in `narrow.Actions()` |
| An agent obtains grant authority | no `propose_grant` twin; two propose kinds are restrictive-only; T7 canaries; schemas `additionalProperties:false` |
| A table rebuild of `component` later cascades the grant table | `autonomy_grant.root_id` FK listed in S44-extra's park list via R8; T0 checks S44-extra's final child list; drill in T11 |
| `proposal.kind` rebuild collides with another slice's | read the **current** DDL at T8 start; copy guard + fk guard (S37/S46 precedent) |
| Attach of a granted root under another root leaves a child with allows | trigger refuses `parent_id` change while active allows exist (AL10); tested |

# Plan — S37 Agent admission

## What is settled before planning

Everything in `brief.md` (AA1–AA5, the seven arbiters, the four risks) and `design.md`'s AD1–AD6. In
particular: the schema change is a six-character widening of S35's existing `CHECK`, never an edit to its
shipped migration (AD1); the payload is a bare path, re-read at both create and commit, never a snapshot
(AD2); create and commit both run through functions `internal/plugin` already exports except one
(`ReadPackagePreview`, AD6; and, because `internal/protocol` may not import `internal/plugin`, the preview composes in `cmd/aeon` behind a function seam, T3/T4); commit is `cmdAgentImport`'s existing body, called from a second entry point,
never a second implementation (AD4 — realised in T5 as one extracted function with two callers); a commit that fails on a moved filesystem sets `applied_error` and
leaves the row `proposed` (AD5) — a genuinely different case from S35's crash-retry story, not the same
test reused.

## How we will do it

**This slice is small because S35 is load-bearing and already built to take a fifth kind.** The store
migration first, the one export second, the preview/registry/dispatch entries third to fifth, the bench last —
mirroring S35's own sequential discipline (no parallel tasks; S35's T1→T9 was linear by the operator's
2026-10-26 direction, and this slice inherits that habit rather than re-deciding it). Seven tasks (plus the T0 investigation), not nine,
because four of S35's nine (the proposal table itself, the three plain-write agent methods, the grep-test
amendment, the menu grouping) need no change at all.

## Open questions this plan answers

- **Q1** (does the registry entry need a new combined export, or do three existing calls compose?) —
  **T2/T3's answer: compose, plus one rename-to-export** (`readPackage` → `ReadPackagePreview`, AD6; the
  composition lives in `cmd/aeon`, T3). No combined function in `internal/plugin`; composing the three calls is
  a few lines, and a combined
  function would be a fourth thing to keep in sync with `ImportChecked`'s own three steps.
- **Q2** (does `proposal.list` re-read the disk to show a proposal's current validity?) — **No.** `list`
  renders the `preview` captured at create time, labelled as a preview; AD5 already means the row's truth
  is only known for certain at commit. Re-reading on every `list` call would make an open desk tab an
  unbounded disk-read loop for no correctness gain AA3 does not already provide at the point it matters.
- **Q3** (absolute-path resolution — at create, or left to the CLI commit the way other paths in this
  project are?) — **At create (T4).** `proposal.create`'s handler resolves `pkgPath` to absolute before it
  touches the validator registry; a relative path is refused at creation with the reason (AD2), so the
  proposal row itself never carries an ambiguous path for a later process to misresolve.

## Task sequence

Each task has its own file in [`tasks/`](tasks/) written for a lower-capability implementer: exact files, steps with
signatures, a test table, commands and a Do-NOT list. **T0 is investigation (no code); T1–T7 are
implementation.** Every S35 symbol in the task files is S35's *design* name until T0 records the real one.

### T0 — Investigation: S35's real names, S15's signatures ([`tasks/T0.md`](tasks/T0.md))

Precondition check first (S35 T1–T5 merged), then fourteen quoted answers: the `proposal` migration number and
DDL, the `kind` CHECK and its Go mirror, the validator registry and `proposal.create/list/discard` shapes, which
package may import `internal/plugin`, the commit dispatch and `applied_error` path, the TD10 whitelist test, the
bench Trust group, the current migration count, and S15's exact signatures. **Depends: S35 T1–T5.**

### T1 — The migration (AD1) ([`tasks/T1.md`](tasks/T1.md))

One new numbered migration widening `proposal`'s `kind` CHECK by the 0016 rebuild pattern (copy, drop, rename;
rows and indexes preserved) — `'agent_import'` added to the enum, nothing else touched. Go-side closed-set
constant and every migration-count assertion bumped. **Depends: T0.**

### T2 — `ReadPackagePreview` (AD6) ([`tasks/T2.md`](tasks/T2.md))

Export `readPackage` as `ReadPackagePreview` — a one-line wrapper in `internal/plugin`, reviewed as its own
commit so the diff against an already-built slice's package is visibly exactly this, plus three tests. **Depends:
T0** (independent of T1).

### T3 — The create-time preview as a `cmd/aeon` function (AD3) ([`tasks/T3.md`](tasks/T3.md))

`agentImportPreviewFor(st)` composes `ReadPackagePreview` → `ReadCarriedPolicies` → `CheckRequests` →
`RenderConsent`, refusing with the CLI's own strings (validator parity, criterion 2) and writing nothing. It lives
in `cmd/aeon` because `TestNoUnconditionalImportOfPlugin` lets no other package import `internal/plugin`; the
protocol layer receives it through a function seam (S15's `Table.Plugins` pattern). Also the typed
`AgentImportPreview` wire structs. **Depends: T2.**

### T4 — The registry entry, the seam, and `proposal.create` for the new kind (AD2, AD3) ([`tasks/T4.md`](tasks/T4.md))

Absolute-path rule (Q3) and one-field payload at create; the `agent_import` registry entry calls the seam; the
stored payload is the bare path; the response carries `preview`; three conformance fixtures; `make protogen`;
the import-boundary grep test (criterion 5). No new method. **Depends: T1, T3.**

### T5 — Commit dispatch (AD4, AD5) ([`tasks/T5.md`](tasks/T5.md))

Extract `cmdAgentImport`'s body as `importAgentPackage` (existing tests prove byte-identical output), then one
`case "agent_import"` in S35's commit dispatch calling it: fresh re-read through `ImportChecked`,
`StoreCarriedPluginDenial` per carried `Deny`, fresh consent (AA4), flip on success. On refusal: `applied_error`
set, row stays `proposed` (AD5) — AD5's own tests, the real filesystem-mutation case, and the `plugin_agent_live`
second-commit case land here. **Depends: T1, T4.**

### T6 — The bench (one toggle, one existing page) ([`tasks/T6.md`](tasks/T6.md))

`ProposeImportForm` on the Agents page: path field → `proposal.create` → S35's exact toast and the labelled
preview. `ProposalsPage.tsx` needs **zero code changes**; the task's job is proving that genericity. No commit
affordance. **Depends: T4; S34/S35 bench tasks merged.**

### T7 — Close-out: the test map, the drills, the docs ([`tasks/T7.md`](tasks/T7.md))

The slice test (test map 1), socket-level validator parity (2), the confirming (not editing) run of S35's TD10
whitelist and the absent test with a zero-diff proof (7), the grep proofs (5), the deletion drill in git, measured
counts, the `DEMO.md` §S37 walk, STATUS/ROADMAP/PLAN/TASKS rows, and the S15 retrofit note closed. **Depends:
T1–T6.**

## Sequencing summary

```
T0 ► T1 ► T2 ► T3 ► T4 ► T5 ► T6 ► T7
```

Linear, the same discipline S35 used and for the same reason. The real dependencies are looser (T2 needs only T0;
T1 and T3 are independent of each other), but the run order is T0 → T7: T4's registry entry cannot precede T3's
preview or T1's widened CHECK, T5's commit cannot precede a row to commit (T4), and T6's form cannot precede
the response that feeds it. Seven implementation tasks plus one investigation, each ≤ ~250 changed lines with one
verifiable outcome; **zero new socket methods, zero new store tables, one new exported function that is a wrapper
around an existing one.** The smallest of the four P34-children slices, because the first three already built
everything this one needed except the one case nobody had pointed at yet.

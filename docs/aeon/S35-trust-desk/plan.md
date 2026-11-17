# Plan — S35 Trust desk

## What is settled before planning

Everything in `brief.md` (§3's line, §4's guard-blind invariant, §6's human door, §7's side-door rule, the
seven arbiters, §8's risks) and `design.md`'s TD1–TD12. In particular: the `kind` enum is closed (TD1);
create and commit run the *same* validator (TD3); commit is the existing effect path plus a flip, never a
new effect path (TD4); the bench has no commit affordance, in any form (TD7); the grep-test amendment is
additions-only (TD10).

## How we will do it

The store invariant first, the door before the bench, and the amended grep test **last among the backend
tasks** — a whitelist is meaningful only once the full socket write-surface exists to be whitelisted.
**Execution is strictly sequential (the operator's 2026-10-26 direction: no parallel tasks)** — the
dependency notes below say what *must* precede what; the run order is simply T1 → T9. S34 precedent holds:
each task lands with its conformance fixture / test and the suites green.

## Open questions this plan answers

- **Q1** (does `policy add`'s validation live in a call-able function, or inline in the cobra run?) —
  T2's first step: extract if inline, re-run `policy_test.go` green before registry work. The extract is
  mechanical and its own first commit.
- **Q2** (`SaveConfig`'s exact race posture — who else writes `mcp.json`?) — nobody today (§2 measured:
  zero writers), so temp+rename under a per-path lock is sufficient; T4 records the decision beside the
  function so the next writer inherits it.
- **Q3** (what does `created_by` hold exactly — uid, username, socket peer identity?) — T1 reads how
  `loop.create` and friends record authorship and uses the same token; the desk displays what the fixture
  carries, not a guess (S34's Q2 pattern).

## Task sequence

### T1 — The proposal table (TD1, TD2's store half) ([`tasks/T1.md`](tasks/T1.md))

`store/` migration for the `proposal` table exactly as `design.md` TD1; store functions
`CreateProposal / ListProposals / DiscardProposal / DecideProposal` — the last two guarded UPDATEs from
`proposed` only, decided rows immutable. Tests: double-decide returns `already <status> by <who> at
<when>`; discard of a committed row is the same error; CHECK violations on `kind`/`status` are
un-writable. Answers Q3. **Depends: nothing (first).**

### T2 — The validator registry (TD3) ([`tasks/T2.md`](tasks/T2.md))

First Q1: extract `aeon policy add/remove/seed-corpus`'s validation into call-able functions, existing
tests green. Then the registry: four kinds → four existing validator families (`mcp_server_add` builds the
would-be `Config` and runs `Config.Validate` verbatim — zero new validation logic, that is the point).
Test map #3 (parity) lands here. **Depends: T1.**

### T3 — `proposal.create / list / discard` on the socket (TD6) ([`tasks/T3.md`](tasks/T3.md))

Names in `protocol/method.go` after S34's block; handlers in `protocol/proposals.go`; every handler writes
**only** `proposal` (TD2); refusal payloads carry the validator's reason verbatim. Three conformance
fixtures. **Depends: T1, T2.**

### T4 — `agent.enable / disable / revoke` on the socket (TD5), and `SaveConfig` (TD4's birth) ([`tasks/T4.md`](tasks/T4.md))

Three plain-write methods calling `TransitionPluginAgent` verbatim — no store edits in this task, and the
test proves the teeth travel: socket revoke without reason is un-writable. Plus `mcp/config.go`'s one
writer, `SaveConfig(Config)` (validate → temp+rename → per-path lock; Q2's decision recorded); no
`aeon mcp add/remove` bypass verbs are created, ever (TD4). Fixtures for the three methods.
**Depends: T1 (tests only), conceptually after T3.**

### T5 — The human door: `aeon proposal commit / reject / list` (TD4) ([`tasks/T5.md`](tasks/T5.md))

CLI verbs as specified in `design.md` §2; commit re-validates, applies **through the existing path** (the
T2-extracted corpus insert; the T4 `SaveConfig`), then flips. Crash-retry idempotency (test map #5) and
the `applied_error` path land here. **Depends: T1–T4 — this task composes them and nothing else.**

### T6 — The amended grep test (TD10, TD7's grep half) ([`tasks/T6.md`](tasks/T6.md))

`TestNoPolicyAuthoringReachesTheNarrowSurface` gains the positive whitelist (only `proposal` is socket-
writable) and the no-`proposal.commit` pin; original assertions byte-preserved, hash-pinned fixture
(test map #2) plus the canary. **Lands last among backend tasks on purpose: the whitelist is only
meaningful with the full socket surface (T3–T5) in place to scan.** **Depends: T3, T4, T5.**

### T7 — The desk and the knobs (bench tier) ([`tasks/T7.md`](tasks/T7.md))

`ProposalsPage.tsx` (the desk: newest-first, payload-vs-live diff, copyable commands, only action =
Discard), AgentsPage toggles (`agent.*` live + grants → `proposal.create` + toast, TD7's toast text exact),
McpPage add/remove forms + the *no-Check-for-pending-server* rule, PolicyPage's Proposed table (TD8 —
never merged). `Fact<T>` everywhere (TD9). All of it consumes methods that already answer — thin clients,
P34, S34's build order replayed. **Depends: T3, T4, T5.**

### T8 — Menu: the Trust group (TD11) ([`tasks/T8.md`](tasks/T8.md))

`BENCH_PAGES` gains `proposals`; `SideMenuItem` group **Trust** = policy · agents · mcp · proposals;
`TestBench.tsx` dispatch branch; `product.test.tsx` nav pins. **Depends: T7** (the page exists to be grouped).

### T9 — Close-out: the one test, the drills, the docs ([`tasks/T9.md`](tasks/T9.md))

The slice test (test map #1) — table-driven over all four kinds, byte-identical verdicts before commit,
one flip after; the deletion drill (test map #7) in scratch branches, in git, not prose; brief's criteria
3–7 ticked in task notes; `STATUS.md` / `ROADMAP.md` rows flipped to built. **Depends: T1–T8.**

## Sequencing summary

```
T1 ► T2 ► T3 ► T4 ► T5 ► T6 ► T7 ► T8 ► T9
```

Linear by ruling (no parallel tasks), and the dependencies happen to make near-linearity honest — the
whitelist (T6) genuinely cannot precede the surface it whitelists, and the desk (T7) genuinely cannot
precede the methods it consumes. Nine tasks; the slice adds **one** table, **one** new config writer, and
**zero** new validation logic — everything else is doors over code the CLI already proved.

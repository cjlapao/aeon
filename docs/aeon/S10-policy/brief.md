# Brief — S10 Policy

**Implements:** [`design.md`](design.md) · **Demo:** [`DEMO.md`](DEMO.md) · **Plan:** [`plan.md`](plan.md)
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — P26 and P31 are binding.
**Stage:** 3 · Car, first of eight. Depends on S1–S9. **Blocks S11–S15.**
**Repo:** Aeon lives at `aeon/` in this repository (P24) · Go 1.26 · `cgo` enabled (B3)

---

## 1. What this is

**One sentence the whole system can say: *"this may not happen here."***

Everything in Stage 3 after this slice can only narrow what this one granted. That is not a style
preference — it is the security model, and it is why this slice is first.

## 2. Why it is cheaper than it looks

**The evaluator already exists in S11's design.** G1–G13 describe a pure, total, ordered, deny-by-default
function whose verdict carries the rule that fired, over a corpus of 366 `bash` patterns already extracted
from the real agent definitions (G19).

That is a policy engine with one type. This slice gives it seven, and S11's corpus becomes the largest
consumer under the name `CommandAccess`. **Most of the work is placement.**

**And three things already landed:** S4's `T2` created all three tables with their indexes and `CHECK`
constraints — **there is no migration in this slice**; `step.context_manifest` is already a column; and
S11's `W21` rescope gives `ToolAccess` something to govern.

## 3. Definition of Done

| | |
| --- | --- |
| **D1** | **Deny beats allow at every level, both orderings.** The most specific possible allow loses to the broadest possible deny, and **the verdict names the allow it overrode** |
| **D2** | **An `Allow` from `Agent`, `MCPServer` or `Import` is refused at write time**, with the source named. A `Deny` from any source is accepted |
| **D3** | **The authorship rule is enforced in exactly one place**, asserted by grep. A rule in two places is a rule with a gap |
| **D4** | **An empty policy set denies everything**, for every type, and `policy list` says so rather than printing nothing |
| **D5** | **The evaluator is pure**: `Evaluate(ps, req, at) Verdict` and nothing else. No `time.Now`, no `os`, no `database/sql` under `internal/policy/` except `store.go` — asserted by grep |
| **D6** | **Expiry is exact at the boundary** — evaluated at `NotAfter-1ns`, `NotAfter`, `NotAfter+1ns`. **No test sleeps** |
| **D7** | **An expired deny does not become an allow.** It stops existing and PL5 takes over |
| **D8** | **A `nil` `NotAfter` never expires**, asserted at a far-future instant |
| **D9** | **A refusal names its policy, type, target and source**, and says `no expiry` rather than omitting it |
| **D10** | **The six unimplemented types refuse rather than permit.** A `RouteAccess` policy evaluates to a refusal |
| **D11** | **A denied tool is proven never to have been called**, with a spy at zero |
| **D12** | **The policies in force are in `step.context_manifest`**, and S7's replay reproduces **that** set, not today's |
| **D13** | **An unaccepted policy is still enforced** (PL10) |
| **D14** | ***"Is this policy in force?"* renders three distinct states**, and none collapses into another |
| **D15** | **`policy_event` has an age cap**, and both the cap and the depth are reportable |
| **D16** | **No policy authoring on HTTP or MCP**, asserted by grep (PL15) |

**D1 and D2 are the two that matter.** D1 because specificity-wins is the intuitive rule and the wrong one.
D2 because it is the entire reason a third-party agent can be trusted with a policy at all.

## 4. The seams

**Downward — S4's three tables.** Already created, already constrained. `store.go` reads and writes them
and **adds no migration.**

**Upward — `Evaluate`.** S11 brings `CommandAccess`, S12 `ToolAccess` over real namespaces, S14
`SkillAccess`, S16 `EffectClass`, S17 `CostCap`, S18 `RouteAccess`, S20 `TriggerAccess`. **Each one is a
spec file and a registered evaluator, not a change to the core.**

**Sideways — S5's tool loop.** T8 inserts the gate before dispatch; T9 writes the manifest. These are the
only two files outside `internal/policy/` this slice touches.

## 5. Locked decisions

- **PL1** — **allow is operator-authored; deny may be authored by anyone; deny wins.** Do not relax this
  for convenience in any later slice.
- **PL2** — **`Spec` is data.** No expression language, no interpreter, no sandbox. Not even "just for
  conditions."
- **PL4** — **specificity never rescues an allow.**
- **PL6** — **the clock is a parameter.** Do not call `time.Now` inside the evaluator, ever.
- **PL13** — **first-match-wins governs inside a spec; deny-wins governs between policies.** These
  contradict if the scoping is dropped.
- **PL15** — **no authoring on the narrow surface.**

## 6. Out of scope

- **N1 — No `CommandAccess` evaluator.** Schema and constraint value only; S11 brings the corpus.
- **N2 — No authoring UI.** An S9 amendment.
- **N3 — No authoring over HTTP or MCP.** Ever (PL15).
- **N4 — No migration.** S4's T2 created the tables.

  **Amended 2026-10-19, at the user's direction (ruling R1).** Exactly **one** migration is authorized
  instead: `0016_policy_governance` in `aeon/internal/store/schema.go`, which rebuilds `policy` —
  create-new / copy / drop / rename, recreating `idx_policy_target` — and is the slice's only one; every
  later task asserts the ledger stays at 16.

  **Why the report became a migration.** Q3 below is the branch that fired: 0005's `CHECK` lists
  `type IN ('tool','path','network','spend','approval')` and `target_kind IN
  ('global','project','loop','agent','route')` — five and five, sharing **nothing** with the seven types
  and five targets `design.md` names, so no spec-shaped policy could be written at all. The report was
  made. The rule that then decided is the one at the head of `schema.go`: *a migration that has shipped is
  immutable — new shape, new number*. Editing 0005 in place would give every fresh database the new
  vocabulary and leave every database already on disk with the old one **and a ledger row swearing it is
  current**, which is undetectable from inside the process. A correction to S4's task is therefore not
  available, only a new number.

  **Why this cannot silently recur.** The drift class is two lists edited in separate commits: the CHECK
  in `schema.go` and the constants in `internal/policy/types.go`. T2's lock test compares the CHECK values
  against `AllPolicyTypes()` / `AllTargetKinds()` / `AllActions()` / `AllSources()`, and
  `aeon/internal/store/schema_test.go` compares them against the DDL's own value lists rather than a copy.
  A CHECK edited without its Go constants now fails a test instead of agreeing with itself.

  **What the amendment does NOT relax.** N4's intent stands for everything else: no second migration, ever,
  for this slice. `0016` changes `policy`'s CHECKs and nothing else — `policy_acceptance` keeps exactly
  `policy_id, target_kind, target_id, accepted_by, accepted_at`, `policy_event` keeps `detail`, and no new
  free-text column enters the schema anywhere in the rebuild. "Report it rather than migrate around it"
  remains the instinct; what changed is only who answers the report.
- **N5 — No policy distribution or bundles.** Stage 5.
- **N6 — No attendedness as policy.** G14 pins it at loop start; a mid-run policy would break the pin
  (PL14).
- **N7 — No re-run or effect classes as policy *structure*.** `EffectClass` overrides a class; it does not
  define what a class is.
- **N8 — No caching of verdicts.** The evaluator is cheap and pure; a cache is where a stale allow lives.

## 7. Questions to answer before building

| # | Question | Where |
| --- | --- | --- |
| Q1 | What is on the blocklist by default? **This design says empty**, with `doctor` reporting it, because a default blocklist is a policy decision disguised as a safe default. **Confirm in T3's notes** | T3 |
| Q2 | What is the `policy_event` age cap, as a number? | T7 |
| Q3 | Does S4's `policy` table's `CHECK` on `type` already list all seven? **If it does not, this slice has a problem it must report rather than migrate around** | T1 |
| Q4 | Does the `Request` shape the evaluator takes cover every one of the seven types without a per-type parameter? | T2 |

**Q3 is the one that can bite.** S4's T2 wrote the `CHECK` constraint months earlier in plan-time. **If its
seven values do not match T1's seven exactly, stop and report it** — a migration here would contradict N4,
and the right answer is a correction to S4's task, not a second migration.

> **Answered 2026-10-19, at T1 (measured against `schema.go`, not against this file): no.** 0005's `type`
> CHECK holds five values and shares **not one** of the seven — nearest neighbour `tool_access` vs `tool`,
> which is a near-miss, not a match. Its `target_kind` CHECK shares exactly two names, `loop` and `agent`,
> out of five. So the stop-and-report branch is what happened, and this is the record of it.
>
> What the report changed is the second half of the instruction, not the first. "A correction to S4's task"
> is not an operation this repository performs on a shipped migration — see the forward-only rule at the
> head of `schema.go`. Ruling R1 therefore authorized the one migration N4 otherwise forbids, with the
> justification recorded under N4 above.

## 8. Risks

- **R1 — The evaluator acquires the store.** The temptation is `store.Evaluate(req)`: one call instead of
  two. It makes every security test need a database, and the first time that is inconvenient somebody adds
  a cache. **D5's grep is the guard.**
- **R2 — Specificity creeps back.** Someone will find deny-wins inconvenient for a legitimate case and
  reach for "most specific wins". **D1 is the guard, and the verdict naming the overridden allow is what
  makes the behaviour visible rather than surprising.**
- **R3 — Acceptance becomes a gate.** It reads like one. **D13 is the guard.**
- **R4 — A stub type permits.** Six of seven types have no evaluator; a `default:` branch that returns
  allow would silently grant everything they govern. **D10 is the guard.**

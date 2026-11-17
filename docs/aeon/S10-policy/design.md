# Design — S10 Policy (Stage 3 · Car)

**Date:** 2026-10-14
**Status:** approved (design); not yet implemented
**Stage:** 3 · Car, first of eight
**Depends on:** S1–S9. **Blocks S11–S15** — every layer after this one can only narrow what it granted.
**Generalises:** G1–G13 from [`docs/_archive/aeon-v1-horizontal/04-permissions`](../../_archive/aeon-v1-horizontal/04-permissions/design.md), which S11 inherits as `CommandAccess`.

> Decision ids are prefixed **PL**. See the
> [prefix registry](../ARCHITECTURE.md#11-decision-prefix-registry).

---

## Context

The bicycle runs loops. It can assemble context, call tools, suspend for a human, recover from a crash and
follow a blueprint — and **nothing governs any of it.** `--allow-bash` is a boolean, the nine tools are
whatever a handler names, and there is no sentence anywhere in the system of the form *"this may not
happen here."*

This slice writes that sentence, once, in a form everything else reuses.

### It is a promotion, not an addition

**The evaluator already exists in S11's design.** G1–G13 describe a pure, total, ordered, deny-by-default
function whose verdict carries the rule that fired — over a corpus of 366 `bash` patterns already
extracted from the real agent definitions (G19).

That is a policy engine. It is simply **narrow**: it governs command lines and nothing else.

So this slice does not build a new mechanism. It **widens the one S11 was going to build anyway**, and
S11's corpus becomes its first and largest consumer under the name `CommandAccess`. Most of the cost here
is placement.

### Three things already landed for this slice

- **S4's `T2` created `policy`, `policy_acceptance` and `policy_event`**, with their indexes and `CHECK`
  constraints, and asserted by grep that nothing in S4 reads or writes them. **There is no migration to
  write.** That was deliberate: S4 is Stage 1 and this is Stage 3, so by now the store holds real loops.
- **S4's schema already has `step.context_manifest`**, which is where the *informational* half of a policy
  reaches a model (PL9). No column to add.
- **S11's `W21` was rescoped** so the nine tools are the *core* nine, extended at runtime in S12. That
  means `ToolAccess` has something to govern beyond a fixed set.

### Why not Rego, Cedar or OPA

They are the obvious answer and they are the wrong one. **Both are languages**, and a language in the
security path is an interpreter that has to be sandboxed, versioned and reasoned about — for a daemon whose
entire access-control story is filesystem permissions and peer credentials (P26).

S8 drew this exact line for blueprint conditions: **L5 made them triples, not expressions**, *"the line
that stops the format becoming a runtime-interpreted language."* `Spec` is the same decision arriving a
second time (PL2).

What a general engine would buy is expressiveness nobody asked for. What it would cost is the one property
that makes a guard a guard: **being able to read the whole of it.**

### The shape is conventional, and that is on purpose

`type + target + action + spec` with deny-wins is **AWS IAM** (`Effect` + `Action` + `Resource`) and
**Kubernetes CRDs** (`kind` + selector + `spec`). Nothing here is novel, which is the point — the novelty
budget is spent on the guarantees, not the vocabulary.

---

## Goal

**A policy interface every later slice reuses, with one authorship rule that makes third-party agents and
MCP servers safe without trusting them.**

Demonstrable: author a system-wide deny for `bash`, watch an agent that *is* granted `bash` be refused with
the policy named, see the acceptance record, expire the policy, and watch the same call succeed.

---

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| PL1 | **Allow is authored by an operator. Deny may be authored by anyone in the chain. Deny wins.** `Source` is recorded on every policy and an `Allow` from any source but `Operator` is **refused at write time** | This single rule is what makes a plugin or a third-party MCP server safe **without trusting it**: whatever policy it carries can only narrow it. The dangerous direction is closed at the type level rather than by review — and it collapses "grant" and "blocklist" into one mechanism |
| PL2 | **`Spec` is data, not a language.** Per-type schema, validated on write; no expressions, no interpreter | S8's L5 arriving a second time. A language in the security path is a runtime to sandbox, for expressiveness nobody asked for, at the cost of being able to read the whole guard |
| PL3 | **The type set is closed**, and adding one is a code change: a schema plus an evaluator | Same discipline as S8's predicates and S16's actions. An open set means a policy whose meaning nothing in the binary knows |
| PL4 | **`effective = (union of allows) − (union of denies)`. Specificity never rescues an allow** | "Most specific wins" would let an agent-level allow defeat a system deny — exactly the union-of-permissions escalation that private tool namespaces exist to dissolve. Deny is absolute at every level |
| PL5 | **Default is deny, and an empty policy set denies everything** | G3 and G4, generalised. A guard whose default is permit is not a guard; a wildcard allow is how a deployment opts into permissiveness, explicitly |
| PL6 | **The evaluator is pure and total, and the clock is a parameter:** `Evaluate(ps, req, at) Verdict` | G1 generalised, and lifetimes made it *better* rather than worse: expiry is testable at any instant with no clock injection and no sleeping tests. A boundary you cannot test is a boundary nobody checks |
| PL7 | **`NotAfter` is nullable and `nil` means no expiry — never a sentinel date** | A far-future sentinel is a policy that silently lapses on a day nobody chose, and the lapse looks like a bug in whatever it was guarding |
| PL8 | **A verdict names the policy, its type, its target and its source** | G11 generalised. *"Denied"* is unactionable; *"refused: bash · ToolAccess `no-shell-in-review` · target lane-type:review · source operator"* is the difference between filing a bug and changing a policy |
| PL9 | **Enforcement and information are two mechanisms with one declaration. The runtime enforces; the agent is informed so it can behave well, never so it can be trusted to. When they disagree the runtime wins and the attempt is recorded** | A model told not to do something may still try. Conflating the two would make a prompt into a security boundary — S11's G20 arriving where the mechanism *can* stop it, so the instruction is an optimisation rather than a control |
| PL10 | **Acceptance is a quality signal, not a security gate.** An unaccepted policy is still enforced | If acceptance gated enforcement, a plugin could evade policy by declining to accept it. What acceptance buys is *"which policies was this agent under, and since when"* — and proof it knew at the time it acted |
| PL11 | **`policy_event` is capped by age**, and both the cap and the current depth are reported | Every refusal, forever, is an unbounded table. Decided now rather than discovered at 2 GB |
| PL12 | ***"Is this policy in force?"* has three answers**: enforced and acknowledged · enforced but unacknowledged · could not determine | **P31's eighth instance.** A fleet that never acknowledged its policies is working but degraded, and that is invisible unless something asks |
| PL13 | **`CommandAccess` keeps first-match-wins *inside* a spec; deny-wins governs *between* policies** | G2 was an ordered array for a real reason — YAML mapping order is not guaranteed. Scoping it this way keeps that reasoning intact while PL4 holds globally. Without the scoping they contradict: a spec beginning `"*": allow` would defeat every system deny |
| PL14 | **Attendedness is not policy, and neither is structure** | G14 pins attendedness at loop start and forbids inferring it *so a verdict is reproducible*; a policy pushed mid-run breaks that pin. Re-run classes are a property of what a tool **is**. Policy governs what may be done; structure defines what exists — and `EffectClass` makes attendedness look like it belongs here, which is why this is written down |
| PL15 | **Policy authoring never appears on the narrow surface** (S19) | R6's reasoning: exposing it would hand a caller the means to widen the guards written to constrain it. Reading a verdict is fine; authoring is CLI and app only. **Amended 2026-10-30 by S45 PM1**: authoring (`policy.add` · `policy.remove` · `policy.seedCorpus`) and the proposal decision (`proposal.commit` · `proposal.reject`) are now socket methods on the **human** surface — the `0600` endpoint with peer credentials, registered iff the service is wired, `RequiresInitialize`, never `Debug`, the actor from the peer and never a param — and the CLI stays a caller of the one `internal/desk` implementation (PM4). What this row was *for* is unchanged and now machine-checked: the narrow HTTP/MCP-stdio projection, the agent-service enumeration, the tool registry and the records catalog name none of the five, and every authoring write is audited. Guards: `aeon/cmd/aeon/policy_amend_test.go` |

---

## Architecture

```
aeon/internal/policy/
  types.go        Policy, Target, Action, Source, PolicyType; the closed set (PL3)
  spec_*.go       one file per type: its schema, its validator, its evaluator
  evaluate.go     Evaluate(ps, req, at) — pure, total, deny-wins (PL4, PL6)
  verdict.go      the verdict, and the sentence it renders (PL8)
  store.go        reads and writes S4's three tables; the only impure part
  record.go       acceptance and events, and the age cap (PL10, PL11)
  inforce.go      the three-state answer (PL12)
```

**The seam is deliberate: everything except `store.go` is pure.** `Evaluate` takes a slice of policies and
returns a verdict, so the whole of the security logic is testable with no database, no clock and no
environment — which is what G1 bought and what this slice must not spend.

### The seven types

| Type | Spec | First consumer |
| --- | --- | --- |
| `ToolAccess` | `{toolName, state}` | this slice — the blocklist **and** tool grants, now one thing |
| `CommandAccess` | ordered `[{pattern, verdict}]` | **S11** — the 366-rule corpus, extracted not transcribed (G19) |
| `RouteAccess` | `{route, state}` | S18 — *"never send code to a non-local route"* |
| `EffectClass` | `{action, class}` | S16 — per-deployment override of an action's class |
| `CostCap` | `{scope, limitUSD}` | S17 — round caps, per-loop and per-agent budgets. *Annotated 2026-11-02 (S55 T12): `cost_cap` remains a reserved stub; spend/token/wall caps are the `budget` records of S55 (see S55-loop-control/design.md LC7).* |
| `SkillAccess` | `{agentID, skillID, state}` | S14 — A2A's per-skill `security`, expressed as policy |
| `TriggerAccess` | `{kind, state}` | S20 — *"no webhooks here"*, *"no unattended schedules"* |

**Only `ToolAccess` is implemented here.** The other six get their schema, their `CHECK`-constraint value
and a registered evaluator stub that **refuses rather than permits** — so a policy of a type nothing
implements yet cannot accidentally read as an allow.

### Targets

`all-agents` · `agent:<name>` · `loop:<slug>` · `lane-type:<type>` · `mcp:<server>`

**`lane-type` is the interesting one.** It governs by *role* rather than identity — *"no shell in any review
lane"* — which nothing else in the plan can express, and which is how a rule survives an agent being
renamed or replaced.

### Why the evaluator cannot be a method on the store

The temptation is `store.Evaluate(req)`, which reads the policies it needs and answers. It is one call
instead of two, and it is wrong: it makes every test of the security logic a test that needs a database,
and the first time that is inconvenient somebody will add a cache, and the cache is where a stale allow
lives. **Reading and deciding are separate, and the decision is the pure half.**

---

## The demo

Author a deny, watch a granted tool refused, read the record, let it expire. In full:
[`DEMO.md`](DEMO.md).

---

## Testing

| Property | How |
| --- | --- |
| Deny wins at every level | A table: allow at `agent:x` + deny at `all-agents` → **denied**. And the reverse order, same result |
| Specificity never rescues | The most specific possible allow against the broadest possible deny → **denied** |
| Default is deny | An empty policy set refuses every request, for every type |
| An `Allow` from a non-operator source is refused **at write time** | Attempt one from `Agent`, `MCPServer` and `Import`; all three refused, with the source named |
| The evaluator is pure | Asserted by review **and** by grep: no `time.Now`, no `os.`, no `net`, no `database/sql` under `internal/policy/` except `store.go` |
| Expiry is exact at the boundary | `Evaluate` at `NotAfter - 1ns`, at `NotAfter`, and at `NotAfter + 1ns`. **No sleeping** |
| `nil` `NotAfter` never expires | Evaluate at a far-future instant; still in force |
| A verdict names its policy | The rendered sentence contains the id, type, target and source |
| An unimplemented type refuses | A `RouteAccess` policy evaluates to a refusal, never to an allow |
| Acceptance does not gate enforcement | An unaccepted deny still denies |
| The three-state answer | All three reachable, and `could not determine` is distinguishable from `unacknowledged` |
| The age cap holds | Events older than the cap are gone; the cap and the depth are both reportable |
| `CommandAccess` ordering survives | A spec whose first rule is `"*": allow` is still defeated by a system deny — the PL13 contradiction, asserted |

---

## Out of scope

- **No `CommandAccess` evaluator.** Its schema and constraint value land here; S11 brings the corpus and the
  matcher, because the matcher is a measurement over real patterns and belongs with them.
- **No policy authoring UI.** S9 built the app; the editor is an S9 amendment, and the CLI is enough to
  demonstrate this slice.
- **No policy authoring on HTTP or MCP** (PL15), ever. **Amended 2026-10-30 by S45 PM1**: still true, and
  now proven rather than asserted — the five authoring/decision methods ride the **socket** (the surface a
  person holds) and are absent from the narrow HTTP / MCP-stdio projection, the agent-service enumeration,
  the tool registry and the records catalog, by
  `aeon/cmd/aeon/policy_amend_test.go`'s `TestAuthoringNeverReachesTheNarrowOrAgentSurfaces`. The bullet
  was never about the socket; S45 made that explicit instead of leaving it to be inferred.
- **No distribution.** A policy bundle importable the way S15 imports an agent is a Stage 5 idea; the shape
  allows it and nothing needs it.
- **No migration.** S4's `T2` created the tables.

---

## Amendments to earlier slices

| Slice | Change | State |
| --- | --- | --- |
| **S5** runtime | The tool loop consults the evaluator before dispatch, and the policies in force are written into `step.context_manifest` (PL9) | **this slice's T8, T9** |
| **S7** recovery | Replay must reproduce the **original step's** policy set, from the manifest — not today's | **this slice's T9** |
| **S9** app | A policy editor, and the verdict sentence rendered where a refusal appears | open — S9 amendment |
| **S23** first run | `doctor` reports unacknowledged policies, expired policies, and the `policy_event` depth against its cap | open — S23's T8 |

---

## Open questions

| # | Question | Where the answer comes from |
| --- | --- | --- |
| Q1 | What is on the blocklist by default? | **Empty**, is this design's answer, with `doctor` reporting it — because a default blocklist is a policy decision disguised as a safe default. A locked-down deployment authors its own. **Confirm before T3.** |
| Q2 | What is the `policy_event` age cap, concretely? | A number, not a principle. Pick one in T7 and state it in the task's notes; `doctor` makes it visible either way |
| Q3 | Does a first-party agent go through the same grant flow? | **Yes for anything it declares** — but an agent that declares nothing needs no grant, so the 25 bodies are unaffected either way (S13's level-0 invariant) |

## Amendment 2026-11-07 (S74): tool address patterns in ToolAccess

`ToolAccess.toolName` keeps its exact bare-name matching and gains one more form: an address pattern `<category>.<tool>.<access>` whose segments are literals or a whole `*` (`workspace.*.read`, `*.*.read`). This amends the earlier "no globs" ruling. The objection was a second rule language nobody audits; a whole-segment star over a closed vocabulary the daemon computes, with `tools.matchPattern` to list what a pattern names, is not one. Deny still wins (PL4). See `S74-policy-pickers/design.md`.

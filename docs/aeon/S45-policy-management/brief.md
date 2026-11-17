# Brief — S45 Policy management

Written 2026-10-30. **Stage:** 5 · Fleet — Tier 1 of [`CONFIGURATION-ORDER.md`](../CONFIGURATION-ORDER.md), the
second of "the two small, most load-bearing pieces" (agents first: S43; the config store beside it: S44).
**Depends on:** S10/S11 (the engine), S35 (the proposal desk), S38 (the records catalog — the "one definition, two
doors" pattern this slice reuses), **S43-extra T3** (the seed engine — T12 declares the `policy.corpus` set against it),
**S43 merged** (agents become slugs; policy's `AgentKind` target and the seeded
corpus ids move from name to slug in S43 T2/T3 — this slice builds the agent picker on that identity, and does **not**
change S43). S44 is *not* a dependency.

## 0. Why now, and the rule this slice carries

The engine is built and heavily tested; almost none of it is a product. A person cannot author, inspect or remove a
policy from the app, and the daemon cannot do most of what the CLI does. The operator's standing rule for every
slice from here on (2026-10-30):

1. **The daemon is first class and the CLI is second.** Anything the CLI can do that the daemon's socket cannot is a
   gap in the *daemon*, and this slice closes it for policy. The CLI becomes a client of the same methods.
2. **Every management operation gets an agent-visible tool**, projected from one definition with its socket twin
   (the S38 records-catalog pattern). Where an operation widens a guard the tool is **propose-only**.
3. **The surface lists what it exposes**: a list-with-details, not a CRUD form, so a person can see what is
   configured and to whom it applies. (The catalog of *tools themselves* is its own slice, S47.)
4. **Every record is a row, system ones included.** Policies the product ships (the 367-rule corpus) are **seeded into
   the database** by the S43-extra seed process, carry `origin='system'`, and the daemon reads only the row. Code is the
   seed source, never the source of truth. Because the corpus adds `allow` rows, its seed set is class **`guard`** —
   applied only by an explicit human-door action, never at daemon start (D4 stays true).

## 1. What exists today, measured (2026-10-30, `aeon/` at `57ea4f70`)

| Capability | CLI (`aeon policy` / `aeon proposal`) | Daemon socket | Gap |
| --- | --- | --- | --- |
| list policies | `policy list [--type]` | `policy.list` — **no `spec`**, no filters | detail + filters |
| explain a verdict | `policy why` (agent/loop/lane-type/mcp + tool/command/route/effect/cost/skill/trigger + `--at`) | `policy.why` — same params | none |
| what is in force for a target | `policy in-force` | `policy.inForce` | none |
| browse the refusal audit | `policy events [--subject] [--depth]` | `policy.events` (`limit`) | verify `subject` filter parity |
| **add a policy** | `policy add` (store-direct) | — | **missing** |
| **remove a policy** | `policy remove` (store-direct) | — | **missing** |
| **seed the shipped corpus** | `policy seed-corpus [--agent] [--dry-run]` | — | **missing** |
| propose a change | — | `proposal.create/list/discard` | none |
| **commit / reject a proposal** | `proposal commit/reject` (store-direct; effect code lives in `cmd/aeon`) | — **by design (S35 TD7)** | **missing** |
| the closed vocabulary and per-type spec shapes | the CLI help text | — | **missing** (a UI would hardcode it) |
| agent-callable policy tools | — | — | none exist; the S38 catalog explicitly excludes policy (T0-Q9) |
| **system policy as records** | the corpus is applied by the bespoke `seed-corpus` runner; seeded rows are told apart only by the `corpus.` id prefix; no `origin`, no ledger, no versioning, nothing says whether the shipped corpus is current | none | **gap vs the rule** — T12 |

The `policy` table, `policy_acceptance` and `policy_event` exist (S4). The engine: seven types, five target kinds,
four sources, three actions (`internal/policy/types.go`); the one write boundary `policy.Store.Add/Remove`
(`store.go`); PL1 (an `allow` needs `source: operator`) lives in `checkAuthorship` and nowhere else. The shipped
corpus (`internal/policy/testdata/corpus.json`) is **38 agents, 367 rules**, ids `corpus.<agent>.<hash>`.

## 2. The ruling this slice amends (operator, 2026-10-30)

PL15 (S10) and TD7 (S35) kept policy authoring and proposal commit/reject **off the socket**: CLI-only, store-direct,
"so no caller can widen the guards written to constrain it", pinned by `TestNoPolicyAuthoringReachesTheNarrowSurface`
and `TestOnlyProposalIsSocketWritable`. The operator ruled, when asked: **add them to the socket as a human door,
off the narrow/agent surface.** The reasoning that makes this consistent with P26 (the socket *is* the human surface:
0600 file plus peer credentials; the narrow HTTP/MCP surface is an enumerated allow-list that never names these
methods; the agent-services surface and the tool registry never will) is `design.md` PM1. This is a **security
invariant amendment** and is its own task (T6), with the guards rewritten positively, canary-tested, and the rule
text in `AGENTS.md`/S10/S35 annotated — never a quiet removal of a test.

## 3. What this slice is

1. **Daemon parity**: `policy.add`, `policy.remove`, `policy.seedCorpus`, `proposal.commit`, `proposal.reject`,
   `policy.get`, `policy.vocabulary`, and richer `policy.list` — all behind one implementation (`internal/desk`)
   that the CLI and the daemon both call.
2. **Agent tools** via the records catalog: read tools (`policy_list/show/why/in_force`) and **propose-only**
   tools (`policy_propose_add/remove`) that can only ever author a restrictive (`deny`/`ask`) row and can never
   commit.
3. **The CLI as a client**: dials the daemon; offline fallback through the same `internal/desk` code; a parity guard
   proving every CLI verb has a socket method.
4. **The app**: a Policy section in Settings (the real app, not a bench tab): a list with details and filters, a
   vocabulary-driven authoring form with an **agent picker** (slug identity from S43), remove, seed-corpus
   dry-run, the proposals desk with commit/reject behind a before/after confirmation, an in-force/why explorer,
   and the audit.
5. **System policy as seeded records (T12)**: the corpus becomes the `policy.corpus` seed set (class `guard`),
   rows carry `origin`, an operator's removal is never silently undone, and `policy.seedCorpus`/`aeon policy
   seed-corpus`/a committed `seed_corpus` proposal are three doors onto **one** engine path.
6. **The audit of authoring**: every add/remove/seed/commit/reject is recorded once, with the peer identity,
   append-only (design PM8).

## 4. Out of scope

- **The seed engine itself** (sets, ledger, classes, startup apply) — S43-extra. S45 only declares its own set.
- **Policy semantics.** No new type, action, source or target kind; the closed vocabulary (PL3) is untouched.
- **MCP management** (S46) — except that `proposal.commit/reject` is generic over kinds, so the two MCP proposal
  kinds' *effects* move behind the shared registry here (a behaviour-preserving move; S46 owns MCP's own surface).
- **The `agent_import` proposal kind's commit.** Its effect path is S37/S15/**S43** code, and S43 is being built and
  not changed from here. Its socket commit is S45's own **T10**, run after S43 merges.
- **The tools catalog** (S47) and **blueprint/automation management** (later slices).
- **Changing S43, S35's bench pages, or the enforcement gate.** The gate stays guard-blind to proposals (S35 TD2).
- **Trust-root/revocation, multi-user auth, or any identity beyond the kernel peer credential** (P26).

## 5. Open questions — decided in `design.md`, flagged where the operator may overrule

1. Where the shared implementation lives (a new `internal/desk` vs extending `internal/policy`) — PM2.
2. Does the CLI keep an offline path? — PM4 *(product call)*.
3. How the audit of authoring is stored — PM8 (T0 confirms against `policy_event`'s CHECK).
4. How a socket commit differs from a CLI commit given the socket cannot prove a human is present — PM7.
5. Which policy operations agents may see, and that none may commit — PM9.

## 6. Acceptance criteria

1. **Parity:** every verb of `aeon policy` and `aeon proposal` has a socket method; a guard test walks the CLI's verb
   table against the method table and fails on a verb with no twin.
2. **One implementation:** the CLI and the daemon call the same `internal/desk` functions; a spy test proves one
   boundary round-trip per accepted add through either door.
3. **PL1 holds on every door:** an `allow` authored by a non-operator source is refused with PL1's sentence over the
   socket, the CLI, a committed proposal and a tool-created proposal.
4. **A proposal never constrains anything** until committed: the arbiter test (S35 criterion 1) still passes
   byte-identically with proposals of every kind present.
5. **Commit and reject are human-door only:** absent from the narrow surface, the agent-services enumeration, the
   tool registry and the records catalog — pinned by the rewritten guards and a canary that fails if one is added.
6. **Agent tools are restrictive-only:** `policy_propose_*` refuse an `allow` and cannot name a `source` or an actor;
   a proposal they create carries `created_by` kind `agent`; no tool commits.
7. **Details are visible:** `policy.get` and the UI show every policy's type, target, action, source, expiry and a
   one-sentence description of its spec rendered by one formatter; the vocabulary the form offers comes from
   `policy.vocabulary`, not UI code.
8. **Before/after:** a socket commit, add or remove returns the in-force sentence for the affected target before and
   after, verbatim from the one formatter.
9. **Audit:** each authoring write leaves exactly one append-only audit row naming the actor (peer uid), the policy id
   and the action; the audit table cannot be rewritten or deleted from.
10. **The agent picker** lists agents by slug and name from the daemon (S43), refuses a target that matches no agent,
    and is the single component S46 reuses.
11. **The UI reaches policy only through registered socket methods** (the P34 grep, as a test).
12. **System policy is seeded, not special:** every shipped corpus rule is a `policy` row with `origin='system'` produced by
    the seed engine; `policy.corpus` is a **guard** set that **cannot** be applied at daemon start (test + canary); an
    operator-removed seeded rule is reported `removed` and not resurrected until an explicit restore; existing databases'
    seeded rows are adopted, hand-edited ones reported `diverged`.
13. **No regression:** the S10/S11/S35 suites pass unchanged except the amended guard tests, each amendment listed.

## 7. Handoff status (updated 2026-11-01 — slice **Built**, 13/13)

**Done:** all thirteen tasks T0–T12, each green and recorded in its own Notes; T11 (close-out) re-ran every
criterion **fresh** with its named test (all PASS), swept the six packages (14 reds, every one a **named**
pre-existing member — none new), ran the migration drills 1–6 + the origin-backfill half, and **walked the
demo on a real daemon with a real store**: the socket/CLI lane actually ran (D3–D6, D8–D10, D12–D15, D18–D20
with recorded outputs — `DEMO.md` §S45), and the app-lane steps (D1, D2, D5's confirm, D7, D11, D14, D17) are
recorded **"not run — no desktop shell in this lane (headless session; Tauri system libraries absent)"**,
with D16 refused by the S37 validator (pinned by T10's real-package test instead) and D17 unmeasurable
here. The walk caught two real defects: `seed.status` once reported the **planned** state (fixed in-commit,
teeth-proven regression test) and the startup log's `applied 0 (33 failed)` is a false counter (mechanism
recorded, not chased — the fix is an engine result-contract change, S43-extra territory). Three S45-own test
pins stale from intra-slice moves were reconciled to the final shape, strengthened, no pre-slice guard body
touched.
**Blocked on:** nothing — S43 and S43-extra are merged; the named seams (`agent_import`, both MCP kinds) are
exercised or refused by name exactly as planned.
**What next slices inherit:** `internal/desk` (the one implementation), the human-door door set on the socket,
`policy_change`'s append-only audit, `policy.origin` + the `policy.corpus` guard set on the seed engine, the
`AgentPicker`, and T6's positively-stated guard contract — S46/S47/S48 build on these as designed.

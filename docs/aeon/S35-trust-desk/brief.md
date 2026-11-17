# Brief — S35 Trust desk

**Implements:** a new slice, designed in the 2026-10-26 conversation that ruled the staging line, written as
**P34's second child** ([`ARCHITECTURE.md`](../ARCHITECTURE.md)) and the deliberate sequel to
[`S34-trust-bench`](../S34-trust-bench/brief.md) — S34 made the trust area *observable* from the socket;
this slice makes it *writable* without breaking the one ruling that says the most dangerous part of it must
never be written from the socket. `design.md` and `plan.md` are written alongside this brief.
**Stage:** 5 · Fleet. **Depends on:** S4 (the store), S9 (the app + test bench), S11 (the policy evaluator),
S12 (MCP config + its validators), S13 (agent roster), **and S34** — uniquely among the Stage-5 candidates,
this slice's bench tier edits S34's own pages and reuses its `Fact<T>` three-state rule and its
config-and-Check UX, so it seeds after S34's T1–T4 land (§8 R2). **Repo:** Aeon lives at `aeon/` in this
repository (P24) · Go 1.26 · `cgo` enabled (B3)

---

## 0. Before anything else: this slice is where P34 and PL15 collide, and the shape of their peace

**P34**: *"A capability that is not reachable through the daemon surface is not finished."* **PL15 /
R4 / R6**: policy authoring gets **no socket door** — enforced not by prose but by a test,
`TestNoPolicyAuthoringReachesTheNarrowSurface` (`aeon/cmd/aeon/policy_test.go:483`), and by a comment that
admits no reading: *"Authoring lives HERE and on no socket, ever"* (`aeon/cmd/aeon/policy.go:8`). A slice
that naively applied P34 to this domain would try to put `policy.add` on the socket and would be stopped by
CI, correctly.

P34's own text pre-named the only shape in which both survive: *"staged authoring — an inert proposal over
the socket, commit through a human-only door."* This slice is that sentence, built. The bench can **create,
list and discard** proposals — rows that constrain nothing. Promotion to live effect happens through one
door only: a human running `aeon proposal commit` at a terminal.

## 1. What this is

The trust area gets its knobs, and every knob is a daemon method (P34) — but a knob that **widens authority**
turns a knob only into a proposal row:

- **Agents page (S34's readiness view) gets working controls**: enable / disable / revoke — plain socket
  writes — and tool-grant changes — staged proposals (because *grants are policy*, S12).
- **MCP page gets add / remove**: staged proposals carrying the full server entry; the row's Check button
  (S34) becomes meaningful the moment a proposal commits.
- **Policy page renders live and proposed side by side, never merged** — a proposed rule is visibly a
  request, not a rule.
- **A new `proposals` page — the desk**: open proposals, payload-vs-live, and the exact CLI command to
  promote or reject each. The desk has **no commit button** (§7).
- **The bench menu earns its Trust grouping**: policy · agents · mcp · proposals.

**The one-line version:** every knob the operator wants to turn becomes a daemon method (P34) — but knobs
that widen authority turn it only into an **inert proposal row**, and only a human at the CLI promotes it
to live (PL15/R4/R6 stay literally true).

## 2. What exists today, measured — every write in this domain is CLI-direct *by design*

Verified 2026-10-26, so nobody re-derives it at seeding:

- **Policy authoring is the two-access-path exception, deliberately.** `aeon policy add/remove/seed-corpus`
  opens the store on its own handle: *"Authoring lives HERE and on no socket, ever: exposing it over the
  endpoint hands the caller the means to loosen the very guard written to constrain itself (rulings R4/R6 —
  T13 registers four read-only `policy.*` methods and never registers add or remove;
  `TestNoPolicyAuthoringReachesTheNarrowSurface` greps it)"* (`policy.go:4-16`), and `method.go:146` carries
  the matching comment on the socket side.
- **Agent state moves ride one guarded store path.** `aeon agent enable/disable/revoke` →
  `TransitionPluginAgent(name, fromState, toState, reason, at)` (`internal/store/plugin_state.go:147`):
  the UPDATE is guarded on the caller's claimed FROM state — *revoked-as-FROM and reasonless-revoke are
  CHECK violations **even for the store*** (`:142-155`), i.e. un-writable, not refused. This slice moves
  the door; it does not touch this mechanism.
- **MCP servers have no add path anywhere.** `internal/mcp/config.go` exposes `LoadConfigFile` (`:161`) and
  the validator family — `Validate` (`:194`), `validateServerSet` (`:217`), `validateTransport` (`:250`),
  `validateCoreGrant` (`:281`), `validateDeclares` (`:304`) — and **zero writers**. Adding a server today
  means hand-editing the file the daemon reads.
- **Grants are policy** (S12's ruling): any "grant toggle" is a policy-corpus write wearing a toggle
  costume, and must be treated as one.

## 3. The line (the 2026-10-26 ruling): widen-authority stages; move-state writes

The rule, in one sentence: *anything that widens what the daemon may ever do is staged; anything that moves
state within already-bounded authority is an ordinary write.*

- **Plain socket writes** — `agent.enable {name}`, `agent.disable {name}`, `agent.revoke {name, reason}`.
  The imported agent already passed blocklist + consent at import (S15); enabling it starts something
  already permitted, like `loop.create` starts a loop from an approved blueprint. The store's own teeth
  (reasonless-revoke un-writable, revoked-as-FROM CHECK) keep biting **through** the new door — the methods
  call the same `TransitionPluginAgent`, and a socket revoke without a reason fails exactly as a CLI
  revoke does.
- **Staged proposals + human commit** — policy add / remove / seed-corpus, grants (they are policy), and
  **MCP server add / remove**: a new server is a new command line the daemon will spawn, which is exactly
  what S34 made an operator *click* for — it does not become a socket write on the way to something else.

## 4. The proposal table — the load-bearing wall

One new table, `id, kind, payload (JSON), status (proposed | committed | rejected | discarded),
created_at, created_by, decided_at, decided_by, decide_reason` — full shape in `design.md` TD1. Deciding a
row is a guarded UPDATE from `proposed` only (`plugin_transition`'s claimed-FROM pattern); committed and
rejected are **immutable history**. Discard is proposed-rows-only.

The invariant that makes the whole slice safe to review in one glance:

> **No code in the guard's read path ever reads this table.** `internal/policy/evaluate.go`,
> `internal/mcp/grants.go`, and `mcp.LoadConfigFile` must not learn the word `proposal`. A socket handler
> may write this table and nothing else.

Asserted by the amended grep test (§8) and by the slice's one test (§7.1), not by trust.

## 5. The new daemon methods

| Method | Door | Notes |
| --- | --- | --- |
| `proposal.create {kind, payload}` | write | validated by the **same validator the CLI verb uses** (TD3) — a bad payload is refused at creation with the reason, not carried to commit; returns the inert row |
| `proposal.list {status?}` | read | newest first; every row carries the CLI command that decides it (text, P34-style) |
| `proposal.discard {id}` | write | proposed rows only — **discarding loosens nothing**, which is why discard is safe on the socket while commit is not |
| `agent.enable / disable / revoke` | write | `TransitionPluginAgent` verbatim; store invariants unchanged |

Same socket, same peer-credential gate as every other write (P25's split unchanged); each ships a
conformance fixture like S34's four reads did.

## 6. The human door (CLI-only, provably)

`aeon proposal commit <id>` · `aeon proposal reject <id> [--reason]` · `aeon proposal list` (the secondary
door, P34). Commit applies the effect **through the existing code path the direct verb already uses** —
`policy add`'s corpus insert, the MCP config write TD4 defines with the same `Config.Validate` — then flips
the row. **Commit is not a second authoring route; it is the same authoring, behind a human.** If commit
crashes between effect and flip, re-running it sees the effect already applied and completes the flip
(idempotent; tested). Double-deciding a row fails on the guarded UPDATE, loser sees
`already committed by <who> at <when>`.

## 7. The bench — thin client, and the side-door rule

- **No commit button anywhere in the app, in any form.** A bench button that promotes a proposal through
  the socket is exactly the side door R4 forbids. The bench's whole write-story ends at
  `proposal.create`; the toast says *"Proposed · commit with `aeon proposal commit #7`"* — the command is
  copied text, the human runs it (TB10's "CLI twin as text" pattern, promoted to a safety property).
- **Live and proposed never render merged** (TD8): two columns, two visual weights, distinct vocabulary —
  proposed rows are never in the same list as their live twins.
- **S34's three-state rule carries** (TD9): `Fact<T>`, no invented zeros, honest empty desk (*"no
  proposals"*, never *"no policy"*, MC11).

## 8. The grep test gets its missing distinction — carefully

`TestNoPolicyAuthoringReachesTheNarrowSurface` is **amended, not softened**. Its original assertions survive
character-for-character (`policy.add` / `policy.remove` appear in no method name, no handler, no dispatcher —
the diff review checks this against the pre-amendment file), and the amendment adds the one distinction it
cannot currently express: *inert proposal rows are not the corpus* — asserted positively as a whitelist:
**the only table a socket handler may write is `proposal`.** R4/R6 survive; the exempted thing is inert
rows, nothing else.

## Acceptance criteria — the arbiters

1. **The slice is one test.** Socket-create proposals of **every** kind → `policy.why` / `policy.inForce`
   verdicts **byte-identical** to before → CLI-commit exactly one → that verdict flips and no other moves.
2. **PL15 grep (amended) green, with the original assertions byte-preserved** — the amendment may add, and
   the review diffs it against the original.
3. **P34 grep.** Every knob in the bench traces to a daemon method; no renderer writes the store, the
   corpus, or the MCP config file by any other route.
4. **Validator parity.** For each kind, the create-time refusal and the commit-time refusal come from the
   same function on the same payload (TD3) — asserted by test, so "validated at create, re-validated by
   divergent logic at commit" cannot ship.
5. **Store teeth survive the new door.** `agent.revoke` over the socket without a reason is un-writable,
   exactly as from the CLI; `committed → anything` is a CHECK violation.
6. **MC11 + three states.** Empty desk, `unwired` daemon, refused payload — three renderings, no zeros
   masquerading as empties.
7. **Deletion property.** Drop the desk page and the toggle wiring in one commit; committed effects live in
   the corpus and the config file, not hostage to the proposal table.

## What this is not

- **No socket commit, in any form — including "with a confirmation dialog."** That is not a feature
  decision but a new ruling (it would make the human door a UI affordance, which is what R4 exists to
  forbid); if it is ever wanted, it arrives as an amendment to R4, not as a checkbox here.
- **No batch commit, no approval chains.** One row, one human decision, one command.
- **No new guard semantics.** S11/S12 own what a grant, a tier, a verdict *mean*; this slice moves doors
  and adds none.
- **No policy-corpus editing in the UI**, even as proposals of kinds not listed in §3 — the kinds are a
  closed enum, and a new kind is a design revision, not a constant.

## 8. Risks, named where they live

- **R1 — the amendment dulls the teeth.** Amending the grep test is the one place this slice could quietly
  lose R4. Mitigated by criterion 2 (byte-preserved originals, diff-reviewed against the pre-amendment
  file) and by keeping the whitelist *positive* — easier to audit than any negative.
- **R2 — S34 is in flight, and this slice lands on top of it.** Both edit `protocol/method.go`,
  `routes.ts`, `TestBench.tsx`, `AgentsPage.tsx`, `McpPage.tsx`. S35 seeds only after S34's T1–T4 (daemon)
  land, and its bench tier (T7) edits S34's pages, not parallel versions of them. The seeding pass runs
  the `files_expected` collision check S24 used.
- **R3 — the payload is a trojan-shaped field.** `proposal.create` accepts arbitrary JSON. Mitigated
  three ways: per-kind closed enum of kinds; validation at create through the *same function* the commit
  re-runs (criterion 4); and the payload being data no code path executes until a human promotes it
  (§4's guard-blind invariant).
- **R4 — two humans, one row.** Two terminals commit the same id; the guarded UPDATE makes the loser a
  clean error, but the commit's effect-application ordering (effect-then-flip, §6) is where the bug would
  actually live — that is why idempotency is a named test, not a footnote.

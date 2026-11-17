# Design — S45 Policy management

Written 2026-10-30. Rulings **PM1–PM14**; each was checked against the real code in the same session ("Verified
facts"). PM1 is an **operator ruling** (asked and answered 2026-10-30). PM4 is a flagged **product call**; the rest
are documented technical calls. Do not re-open a ruling without recording the reason in that task's Notes.

## Verified facts (2026-10-30, `aeon/` at `57ea4f70`) — T0 re-checks them for drift

1. **CLI verbs.** `aeon policy`: `list, add, remove, seed-corpus, why, in-force, events` (`cmd/aeon/policy.go:109-121`).
   `aeon proposal`: `list, commit, reject` (`cmd/aeon/proposal.go:62-66`). Policy authoring opens the store
   **directly** (`policyBoundary()`; the header comment calls it the second use of the justified two-access-path
   exception, after `db info`/`debug hammer`).
2. **Socket today.** `policy.list/why/inForce/events` (`internal/protocol/method.go:254-257`, registered 993-996);
   `proposal.create/list/discard` (381-383, registered 1053-1055). `MethodProposalCommit`/`Reject` are
   **deliberately absent** (`internal/protocol/proposals.go` header; `TestNoProposalCommitMethodExists`).
   `policy.why`'s params already cover every CLI `why` flag (`PolicyWhyParams`: type, agent, loop, laneType, mcp, tool,
   command, route, effect, cost, skill, trigger, at). `PolicyView` has **no `spec`** (`internal/protocol/policy.go`).
3. **The PL15 guards.** `cmd/aeon/policy_test.go:483 TestNoPolicyAuthoringReachesTheNarrowSurface` — whitelists by
   caller file: `allowedBoundary` = `cmd/aeon/policy.go`, `cmd/aeon/proposal.go`, `internal/policy/store.go`;
   `allowedStoreBridge` = `cmd/aeon/policy.go`, `internal/daemon/policy_gate.go`, `internal/policy/store.go`;
   `allowedAuthored` = `cmd/aeon/policy.go`; only the four read method literals `policy.list/why/inForce/events` may
   appear under `policy.`. `:690 TestOnlyProposalIsSocketWritable` — `store.DecideProposal` and `mcp.SaveConfig` may be
   called only from `cmd/aeon/proposal.go` (`allowedCommitEffect`), with a canary
   (`TestTheCommitEffectWhitelistHasTeeth`). R6's sibling: `cmd/aeon/agent_test.go:320 TestNoPluginManagementOnNarrowSurface`
   (import/install/uninstall/update stay absent — **untouched by this slice**).
4. **The write boundary.** `policy.Store.Add(ctx, Policy, now) *Reject`, `Remove(ctx, id) (bool, error)`
   (`internal/policy/store.go:443,484`); `ValidateForAdd` is the pure half (`:387`); **PL1** (an `Allow` needs
   `source: operator`) is `checkAuthorship` (`:335`) and appears in exactly one place
   (`TestTheAuthorshipRuleAppearsInExactlyOnePlace`). The closed vocabulary — 7 types, 5 target kinds
   (`all-agents, agent, loop, lane-type, mcp`), 4 sources (`operator, agent, mcp_server, import`), 3 actions — is in
   `internal/policy/types.go` and mirrored by CHECKs.
5. **Effect code lives in `cmd/aeon`.** `proposal.go:231 effectAlreadyApplied`, `:353 applyProposalEffect` (kinds
   `policy_add, policy_remove, seed_corpus, mcp_server_add, mcp_server_remove`, and `agent_import` at ~`:318`),
   `proposal_validators.go` (the per-kind validator registry), `policy.go:988 runPolicySeedCorpus`, `:900 seedPolicyID`
   (`corpus.<agent>.<hash>`). The daemon **cannot import `cmd/aeon`**, so a socket commit needs this code in an
   internal package — the largest refactor in the slice.
6. **Who may decide.** `store.DecideProposal` requires `Actor.Kind == "user"` and a non-empty ID, and the
   `proposal.decided_by_kind` column is `CHECK … = 'user'` (`schema.go:2266-2287`). The CLI's actor is the literal
   `{user, cli}` (`proposal.go:212`). On the socket the actor is stamped from the kernel's peer credential by
   `table.ActorFor` (`internal/daemon/daemon.go:495`) — **never from params**.
7. **The records catalog is the "one definition, two doors" precedent.** `internal/records/catalog.go` (`Spec{Name,
   Method, Family, Description, Schema, Class, Mutates}`), projected as tools and, via `registerRecords`
   (`internal/protocol/records.go`), as socket methods, with a parity test walking both directions. Its header states
   T0-Q9: **"NO decide/accept/approve/trigger/policy/mcp/proposal action exists here — those are human doors"**. The
   tool registry is pinned by `TestRegistryExactNames` (`internal/tools/toolset_test.go:236`) — adding a name is a
   *deliberate, counted* edit. `tools.DefaultGrant()` is `read, list, write`; "registered is not granted".
8. **The narrow surface is an enumerated allow-list** (`internal/narrow/actions.go`, entries name one `SocketMethod`
   with a mandatory non-empty `Cannot`); a method absent from it is unreachable from there.
9. **Corpus:** `internal/policy/testdata/corpus.json` — 38 agents, 367 rules.
10. **UI:** bench pages `PolicyPage.tsx` (612 lines, read-only through the generic `rpc()` bridge) and
    `ProposalsPage.tsx` (76 lines; "No commit affordance anywhere on this page, in any form (TD7)"). The Settings screen
    has no daemon-backed section yet (S44 adds a generic one).
11. **Highest typed protocol error code observed:** `-31801` (`ErrNoTelemetry`); proposals use `-31710`
    (`ErrProposalRefused`). S44 reserves `-31810/-31811`. T0 re-measures and reserves a block for S45.

## PM1 — Authoring and commit move to the socket as a human door (operator ruling; amends PL15 and TD7)

`policy.add`, `policy.remove`, `policy.seedCorpus`, `proposal.commit`, `proposal.reject` are registered socket
methods. **Why this is consistent with P26 and not a removal of the rule:** the socket is the human surface —
filesystem permissions on a 0600 endpoint plus peer credentials, no claim or role in the path. The danger PL15 named is
a *caller that is not the person* gaining the means to widen guards, and the callers that are not the person reach
the daemon only through (a) the narrow HTTP/MCP-stdio surface, an enumerated allow-list that never names these
methods; (b) the agent-services surface; (c) the tool registry and the records catalog. PM10 rewrites the guards so
those four **stay** absent and cannot regress silently. The CLI door was never stronger than the socket for a process
running as the same user (it opens the store directly); the new door adds what the CLI door lacked — a peer-stamped
audit (PM8) and a before/after confirmation (PM7).

The two-step is preserved: an `allow` still needs `source: operator` (PL1), a proposal is still inert until
committed (S35 TD2), and the enforcement gate still never learns the word `proposal`. What changes is *who can press
the button*: the app, not only a terminal.

## PM2 — One implementation, in a new `internal/desk` (seed logic lives with the seed engine, PM15)

The code that today lives in `cmd/aeon` (fact 5) moves to a new package **`internal/desk`**: the per-kind effect
registry (`Validate`, `AlreadyApplied`, `Apply`), the policy boundary constructor over the `policy.Repo` seam, and a
**thin printer** for the seed-corpus verb. The seed-corpus *logic* itself is **not** kept in `desk`: per PM15 it becomes the
`policy.corpus` seed set in `internal/policyseed` (T12) driven by the S43-extra engine; `desk.RunSeedCorpus` only formats
the engine's result so the CLI's bytes do not change. `cmd/aeon` and the daemon both call it ("one implementation, two transports"; the S22
`search.query` precedent). It is **new** rather than an extension of `internal/policy` because the registry also
carries non-policy kinds (MCP, and later agent import) and `internal/policy` must not learn MCP. Kinds whose effect
code needs packages the daemon must not import (`internal/mcp`'s config writer is importable; `agent_import`'s
plugin code is S43-adjacent) enter the registry through a **func seam** registered by `cmd/aeon serve` (the
`mcp_seam.go` pattern), so `internal/daemon` imports no new domain package. **A characterization test comes first:**
T1 pins the byte output of every `aeon policy`/`aeon proposal` verb before moving a line, and the move must keep
those bytes identical.

## PM3 — S43 is read-only to this slice; slug identity is a dependency, not a refactor target

S43 (agents as rows, slugs everywhere, including policy's `AgentKind` target and the seeded corpus ids) is being
built and is not changed from here. S45 **depends on it being merged** for the agent picker (T9) and for any task
that touches targets or seed ids; T0 verifies S43's state read-only. Nothing in T0–T9/T11 edits an S43 file. The one
S43-adjacent piece — committing an `agent_import` proposal over the socket — is **T10**, S45's own task, run after
S43 merges (the S44 T10 pattern). If S43 changes the shape S45's tasks assume (e.g. the slug migration of policy
targets), T0's Notes record it and the affected task is re-planned inside S45.

## PM4 — The CLI is a socket client; offline it falls back to the same code *(product call)*

`aeon policy …` and `aeon proposal …` dial the daemon and call the new methods (P34: the CLI is a secondary client).
**If no daemon is reachable** they run the same `internal/desk` functions against the store directly and say so on
their own line ("daemon not running — applied to the store directly"), because three real cases have no daemon:
first-run seeding (S23's `aeon init` path), a broken daemon, and `make` tooling. Overruling this (CLI requires a
daemon) deletes the fallback and the second store-opening path; the cost is that `aeon policy seed-corpus` fails with
the daemon down. Either way there is **one implementation**; the fallback is a second *caller*, not a second copy.

## PM5 — Details are visible, and described by one formatter

`policy.list` today omits the spec and accepts no filter. This slice adds: `policy.get {id}` (the full row: type,
target, action, source, `notAfter`, `createdAt`, the raw `spec`, **a one-sentence description of the spec**, and
whether an operator acceptance and an authored-audit row exist); `policy.list` filters (`type`, `target`, `source`,
`action`, `includeExpired`) and a `describe` sentence per row. The sentence comes from a new `Describe(Policy)
string` on each per-type validator (`internal/policy/spec_*.go`), **the one formatter** (D9) the app, the CLI and the
agent tools all print verbatim — never assembled client-side. Expired rows are listed with their expiry (D6/D8: an
audit answer that depends on when it is asked is not an audit).

## PM6 — The vocabulary is read from the daemon

`policy.vocabulary` returns the closed sets (types, target kinds, sources, actions), the **authorship matrix** (which
source may author which action — PL1, rendered from `checkAuthorship`'s own rule, not restated), and per-type spec
descriptors (field names, kinds, the closed `state` values each type accepts). The authoring form (T9) and
`aeon policy add --help` are generated from it; a UI that hardcodes the closed sets is the drift S44's CS13 forbids
for settings and PL3 forbids for policy.

## PM7 — What a socket commit is, given the socket cannot prove a human is present

The socket cannot distinguish a person from a script that holds the 0600 file; the design does not pretend to. It
adds what *can* be done honestly:

1. **Bound to what was read.** `proposal.commit {id, digest}` — `digest` is the SHA-256 of the canonical payload as
   returned by `proposal.list`/`proposal.get`; a mismatch is refused ("the proposal changed since you read it").
2. **Before/after.** The result carries `InForceAnswer.Render()` for the affected target **before and after**, and
   for `policy_add/remove` the changed row's `describe` sentence — verbatim (D14).
3. **The actor is the kernel peer.** `store.Actor{Kind: "user", ID: <peer uid>}` via `table.ActorFor`; the CLI door
   remains `{user, cli}` (fact 6). A connection whose peer cannot be resolved **refuses** rather than stamping a
   placeholder.
4. **Effect, then flip, with the crash-safe retry S35 TD4 specified:** re-validate → `AlreadyApplied` check → `Apply`
   → `DecideProposal`. An effect that applied while the flip was lost completes the flip on retry; a failed effect
   sets `applied_error` and leaves the row `proposed`. These semantics are *moved*, not redesigned.
5. **Reject** needs a non-empty reason (S35 TD5, the direction-of-risk rule) and writes no policy effect.

## PM8 — The audit of authoring: its own append-only table

Today nothing records *who added or removed a policy* beyond the row's own `created_at` (and `RecordAuthored` for the
seed verb). `policy_event`'s vocabulary is verdict refusals and retention (`PolicyEventTrim`, D15 — rows are
trimmed). An authoring audit must **not** be trimmed. A new migration adds **`policy_change`** (append-only by
trigger, the S44 `setting_event` precedent): `id, op CHECK IN ('add','remove','seed','commit','reject'), policy_id
(nullable for seed/reject), proposal_id (nullable), actor_kind, actor_id, summary, occurred_at`. One row per
authoring write, in the same transaction as the write where the store allows it, never for a read, a refusal, a
dry-run or a no-op. T0 confirms against `policy_event`'s actual CHECK before committing to the new table; if the
existing table can carry it without being trimmed, T0 records that and T2/T3 adapt.

## PM9 — Agents see policy; they can only propose restrictions

Through the records catalog (fact 7) the slice adds seven specs, each with a socket twin and each projected as a
tool: **reads** `policy_list`, `policy_show`, `policy_why`, `policy_in_force`, `proposal_list`; **propose-only**
`policy_propose_add`, `policy_propose_remove` (both create an inert `proposal` row through `proposal.create`'s code).
Rules, each a test: the schema carries **no** `source`, `actor`, `id`-of-author or `status` field (identity is
stamped, OL3); a tool-created `policy_add` is **forced to `source: "agent"`**, so PL1 refuses any `allow` at create
time with PL1's own sentence — an agent can propose only `deny`/`ask`, which can only *narrow*; **no tool commits,
rejects, adds, removes or seeds**; the new names join `TestRegistryExactNames` as a deliberate counted bump (+7) and
are **granted to no one by default** (registered is not granted, S11). The catalog's T0-Q9 header is amended to say
exactly this and nothing more.

## PM10 — The guards are rewritten positively, with canaries

T6 replaces "authoring is CLI-only" with the true statement, in the style of the S35 T6 amendment (a *sibling*
function for the new assertions; the pre-amendment assertions byte-preserved by fixture hash):
**positive** — the five methods are registered, `RequiresInitialize`, not `Debug`; the commit-effect callers whitelist
is `internal/desk` + the daemon's authoring seam files + `cmd/aeon`'s offline fallback; **negative** — no
`policy.add|remove|seedCorpus|…` or `proposal.commit|reject` literal in `internal/narrow`, `internal/agentsvc`,
`internal/tools`, `internal/records`; no `records.Catalog()` entry with `Mutates` that names a commit/reject/add/remove
/seed twin; `narrow.Actions()` projects none; the agent-services enumeration names none. Each negative has a canary
that proves it can fail. The R6 sibling (`TestNoPluginManagementOnNarrowSurface`) is not edited.

## PM11 — The enforcement gate and every guard-blind invariant are untouched

`internal/policy/evaluate.go`, `internal/mcp/grants.go` and `mcp/config.go` gain **zero** references to the proposal
or `policy_change` tables (TD2); the arbiter test passes byte-identically with proposals of every kind present. A
policy written through the socket is read by the very next decision with no cache (N8: the store is re-read per
decision) — a test writes through the socket and asks `policy.why` with no pause.

## PM12 — The app: a Policy section in the real Settings screen, built from descriptors

Not a bench page (the bench `PolicyPage`/`ProposalsPage` stay as diagnostics, unchanged; the desk page keeps its "no
commit affordance" doc because it *is* the bench). The section: a **list with details** (filters by type/target/source/
action/expiry; a detail drawer showing the `describe` sentence, the raw spec read-only, expiry, source, audit
history); an authoring form generated from `policy.vocabulary`; **remove** behind a confirmation naming the policy and
showing before/after in-force; **seed-corpus** with a dry-run preview (per-agent counts) before write; the
**proposals desk** with commit/reject behind the before/after confirmation (reject requires its reason); an
**in-force/why explorer**; the **authoring audit** (`policy.changes`). A reusable **`AgentPicker`** component lists
agents from the daemon by slug and display name (S43), refuses a target matching no agent, and is the one component
S46 reuses.

## PM13 — Seeded ids and agent targets are slugs after S43 — verified, not assumed

`seedPolicyID(agent, pattern)` builds `corpus.<agent>.<hash>`; after S43 T2/T3 the agent component is the slug. T0
reads what S43 actually did to seeded ids and to `AgentKind` targets and records it. The seed runner in `internal/desk`
takes whatever identity S43 settled on; S45 never re-migrates policy rows (that is S43 T2).

## PM14 — What does not move

The vocabulary (PL3), the evaluator, the gate, `policy_acceptance`'s role (PL10: enforcement never consults it),
S35's proposal table shape and kinds (closed — a new kind is a design revision), the bench pages, the narrow
surface's action list, R6 (agent import/install stays off the socket-and-narrow-surface *door*; T10 adds only the
**commit** of an already-validated `agent_import` proposal, and only after S43), and every S43 file.

## PM15 — System policy is seeded, origin-marked, and a *guard* set (operator rule 2026-10-30)

The standing rule (CONFIGURATION-ORDER rule 6): every record is a row; system records defined in code are seeded. Policy
already lives in a table, but its **shipped** rows are applied by a bespoke verb and are distinguishable only by the
`corpus.` id prefix. This slice brings policy under the S43-extra seed engine:
- a **seed set `policy.corpus`**, declared in `internal/policyseed` (S43-extra SD12's "each slice declares its own set"),
  one record per corpus rule, keyed by the existing `corpus.<agent>.<hash>` id;
- class **`guard`** (S43-extra SD5): the corpus adds `allow` rows, so it **never applies at daemon start** — D4's "no
  automatic seeder" is preserved by the engine's class gate, not by convention — and is applied only by an explicit
  human-door action (`seed.apply`, `policy.seedCorpus`, `aeon policy seed-corpus`, a committed `seed_corpus` proposal, or
  `aeon init`'s confirmed guard plan);
- a real **`origin`** column on `policy` (`system|user|import`), backfilled from the id prefix, visible in every read,
  **read by no enforcement code** (PM11);
- writes go through the one policy write boundary (`Store.AddInTx` shares `ValidateForAdd`/PL1 with `Add`), inside the
  engine's transaction so a rule and its ledger row commit together.

## PM16 — What an operator may do to a system policy row

An operator **may remove** a seeded rule and **may not edit** one (policies have no edit; the way to change one is to add
a rule — `deny` outranks `allow`, PL4). Removal is audited (`policy_change` `remove`) and the engine then reports the record
`removed`; `seed.apply` never resurrects it; `seed.restore` (human door) does, explicitly. This differs from agents
(S43 AM1: internal rows refuse edit **and** delete) on purpose: policy is the operator's to shape, an agent definition is
the product's. *Product call — overrule means system policy rows become undeletable, which would let a shipped `allow`
never be revoked and is not recommended.*

## Open items left to T0 — mechanical

1. Re-verify facts 1–11; pin hashes of the PL15 guard tests' pre-amendment bodies (PM10).
2. The parity inventory: every CLI verb × flag → its socket twin or the gap (the brief's table, with flags).
3. `policy_event`'s CHECK vocabulary and trimming — can it carry the authoring audit (PM8)?
4. `proposal.created_by_kind` values; what actor a records-catalog tool call stamps (agent slug? handler name?).
5. What S43 did to policy targets and seeded ids, read-only (PM3, PM13).
6. The real next free error-code block and the handshake/conformance touch points (as S44 T5).
7. `policy events --subject` parity with `policy.events`'s params.
8. Whether `agent_import` is the only kind whose effect imports a package the daemon may not (PM2's func-seam list).
9. **S43-extra's state** (read-only): is the engine (T3) merged? what `Applier`/`Tx` seam did it settle? what identity does
   S43 use for the agent component of `corpus.<agent>.<hash>`? Is `ALTER TABLE policy ADD COLUMN` safe given S11's rebuild
   notes (PM15)? T12's plan is re-checked against the answers.

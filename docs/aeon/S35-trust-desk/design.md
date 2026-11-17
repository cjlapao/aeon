# Design — S35 Trust desk

Fills in `brief.md`: the proposal table (§1), the methods (§2), the human door (§3), the bench (§4), the
test map (§5). Rules are numbered **TD1–TD12** and are cited from `plan.md` tasks the way S34's TB1–TB12
are.

## TD1 — The proposal table is the only thing this slice adds to the store

```sql
CREATE TABLE proposal (
  id            INTEGER PRIMARY KEY,
  kind          TEXT NOT NULL CHECK (kind IN
                  ('policy_add','policy_remove','seed_corpus','mcp_server_add','mcp_server_remove')),
  payload       TEXT NOT NULL,                    -- JSON, validated at create (TD3)
  status        TEXT NOT NULL DEFAULT 'proposed'
                  CHECK (status IN ('proposed','committed','rejected','discarded')),
  created_at    TEXT NOT NULL, created_by TEXT NOT NULL,   -- peer-credential uid
  decided_at    TEXT, decided_by TEXT, decide_reason TEXT,
  applied_error TEXT                               -- set by the CLI door if the effect failed
);
```

`decide` is `UPDATE … WHERE status = 'proposed'` — `plugin_transition`'s claimed-FROM pattern
(`plugin_state.go:142`): deciding an already-decided row changes zero rows and the caller gets
`already <status> by <who> at <when>`. **Discard is decide-to-`discarded` and is the only socket-reachable
status change**; committed/rejected are immutable history, never edited, never deleted.

`agent_*` verbs are **not** proposal kinds — §3's line made them plain writes (brief §3); the `kind` enum
is closed and a new kind is a design revision.

## TD2 — Guard-blind: the invariant the whole review hangs on

No read path in the guard learns the word `proposal`: `internal/policy/evaluate.go`, `internal/mcp/grants.go`
and `mcp/config.go` gain zero references to the table. Positively asserted: the whitelist in the amended
grep test (TD10) — **the only table a socket handler writes is `proposal`**. The arbiter test (brief
criterion 1) is the behavioral half: proposals of every kind exist, and `policy.why` answers byte-identical.

## TD3 — One validator per kind, create and commit run the same function

A registry `map[kind]func(payload) error`, each entry calling the code that already exists:
`policy_add`/`policy_remove` → the corpus row validators `aeon policy add` uses (`cmd/aeon/policy.go`'s
validation block, incl. DEF-038's unmatchable-target warning surfaced as the refusal reason);
`mcp_server_add` → build the *would-be* `Config` (current file + candidate) and run the real
`Config.Validate` (`config.go:194`) — the entry that refuses today's hand-edits refuses today's proposals;
`mcp_server_remove` → name ∈ current config; `seed_corpus` → whole-corpus validation, one row (commit is
all-or-nothing). A bad payload is refused **at create with the validator's reason verbatim** — never
carried inert to commit. Criterion 4 pins create-time and commit-time refusal to the same function.

## TD4 — Commit is the existing effect path plus a flip, never a new effect path

> **Amended 2026-10-30 by S46 MG6 (recorded 2026-11-02 at S46 T7): direct human-door authoring exists —**
> the `mcp.server.*`/`mcp.grant.*`/`mcp.core.set`/`mcp.importFile` socket methods (S46
> T5, the person's door, wired iff the seam) and the config moved into the database
> (S46 T1), so "the existing path" is now the audited row doors, not `SaveConfig` (retired
> by T6). **The proposal path remains** (T6: the three MCP kinds commit through this same
> TD4 machine); the **narrow-surface prohibition stands** and is now positively guarded
> (`cmd/aeon/mcp_amend_test.go`).

`aeon proposal commit <id>`: re-validate (TD3) → apply **through the same code the direct CLI verb calls**
(the corpus insert; the config write below) → flip the row. For MCP config the "existing path" is being
*born* here — `mcp/config.go` gains exactly one writer, `SaveConfig(Config)` that validates then writes
atomically (temp + rename) — and `aeon mcp add/remove` **do not exist as bypass verbs**: config authoring
exists only as proposal + commit. Idempotent recovery: effect applied but flip lost (crash) → re-run sees
the effect already present and completes the flip; effect failed → row gets `applied_error` and stays
`proposed` for retry (test map #5).

## TD5 — Reason discipline follows the direction of risk

`agent_revoke` proposals do not exist (revoke is a plain write), but its reason rule travels: **socket
revoke without a reason is un-writable, not refused** — `TransitionPluginAgent` unchanged (brief §3),
asserted through the new door (criterion 5). `proposal reject` requires no reason (rejecting loosens
nothing); `commit` accepts none; the `decide_reason` column exists for the human's record, not for a gate.

## TD6 — Methods are ordinary writes on the ordinary socket

`proposal.create` / `proposal.list` / `proposal.discard` / `agent.enable` / `agent.disable` /
`agent.revoke` join `protocol/method.go`; peer-credential-gated like `loop.create`, same socket (P25's
split unchanged); conformance fixture each, like S34's four reads. `proposal.list` rows carry
`command: "aeon proposal commit 7"` as **text** (TB10's pattern, safety-promoted by TD7).

## TD7 — The side-door rule: the bench has no commit affordance

> **Amended 2026-10-30 by S45 PM1 — the rule stands, its wording needed locating.** Nothing in the bench
> commits, and S45 added no affordance there: `proposal.commit`/`proposal.reject` landed on the **socket's
> human surface** (the `0600` endpoint with peer credentials, registered iff wired, `RequiresInitialize`,
> never `Debug`, the actor from the peer), which is the surface a person at a keyboard holds — **not** on
> the narrow HTTP / MCP-stdio projection this page is part of, and not on any agent-reachable surface. The
> toast's copyable command is now one of two ways the *same* implementation (`internal/desk`, PM4) is
> reached; the third — a page that commits — is still refused, and
> `aeon/cmd/aeon/policy_amend_test.go`'s `TestAuthoringNeverReachesTheNarrowOrAgentSurfaces` is what keeps
> it refused.

No button, no keyboard shortcut, no "confirm" dialog promotes a proposal. The write-story on screen ends at
`proposal.create`; the toast carries the exact command as copyable text:
*"Proposed · commit with `aeon proposal commit #7`"*. This is TD10's grep-test property rendered honestly:
if the app cannot commit, no page will ever offer to.

## TD8 — Live and proposed never render merged

Policy page: live corpus table; beneath it, visually distinct, a *Proposed* table (muting treatment,
status chips, decision command per row). Agents/MCP pages: a pending toggle on a row reads *"proposed ·
awaiting commit"* over the **unchanged live value** — never the anticipated value greyed in. Two facts,
two pixels, always (TB2's discipline extended to time, not just knowledge).

## TD9 — S34's `Fact<T>` carries; three states stay a type, not a convention

All new renders consume `Fact<T>` from S34 (`design.md` TB2) — proposal lists, toggles' current state,
form validation echoes. An old daemon answering `unknown method` for `proposal.list` renders the honest
`unwired`, never an empty desk (criterion 6).

## TD10 — The grep-test amendment: additions only

**Amended at T6, built 2026-10-29 — (b)'s original sketch below never shipped as written; this section
records what actually did, and why.** `TestNoPolicyAuthoringReachesTheNarrowSurface`
(`cmd/aeon/policy_test.go:483`, confirmed unchanged at T6's close) keeps its current assertions
byte-for-byte (reviewer diffs against the pre-amendment commit — criterion 2); **(a)** no method name,
handler or dispatcher references `policy.add`-equivalent corpus writes — already true, restated
positively on the new surfaces, via a new SIBLING function (`TestOnlyProposalIsSocketWritable`), not a
growth of the original.

**(b), the real mechanism — measured at T6, not the one originally sketched.** The original text said
"scan `internal/protocol/` for `Exec(`/`INSERT INTO`/`UPDATE`." That sketch predates the seam architecture
T3/T4 actually built and would have found NOTHING: `internal/protocol` handlers never contain SQL — they
call through package-local interfaces (`ProposalStore`) or `daemon.Options` func-seams
(`AgentTransition`), and the real writes live in `internal/store/*.go` and `cmd/aeon`'s seam files. There
is also no structural read/write marker on `protocol.Method` to enumerate "every write method" against
(`ReadOnly` exists only on the narrower, unrelated `AgentServiceMethod`). The mechanism that actually
shipped is the SAME whitelist-by-caller-file pattern the ORIGINAL (pre-T6) test already used for
`policy.Store.Add`/`Remove` (the `allowedBoundary` map) — extended to the two functions THIS slice adds
that actually commit an effect: `store.DecideProposal` (the flip) and `mcp.SaveConfig` (T4's one config
writer). Both must have exactly ONE caller anywhere in the repository: `cmd/aeon/proposal.go` (T5's
human-only door). `policy.Store.Add`/`Remove`'s own whitelist already covers the corpus-write half (T5
added `cmd/aeon/proposal.go` to that existing map, confirmed unchanged by T6) — T6's own, new work is
exactly these two functions' callers, plus **(c)**: `"proposal.commit"`/`"proposal.reject"` pinned absent
as method-name string literals in every production file (generalizing T3's own standalone
`TestNoProposalCommitMethodExists` into this file's permanent coverage), and a canary
(`commitEffectOffenses`, called with synthetic forbidden-call text) proving the whitelist check actually
rejects what it claims to, never shipped as a fixture.

The comment at `cmd/aeon/policy.go:8`'s own paragraph gained one sentence recording the distinction
(*inert proposal rows are not the corpus*) and the original test's name did not change — the ruling's
identity survives its amendment.

## TD11 — The menu earns its Trust group

`SideMenuItem` supports `type: 'group'` (the rail uses it; S34 deferred grouping to this slice, its brief
§5). Bench menu becomes: **Trust** — policy · agents · mcp · proposals — above the existing flat sections.
One new `BENCH_PAGES` row (`proposals`), one new `pages/ProposalsPage.tsx`, one dispatch branch: S34's
deletion property extended unchanged.

## TD12 — Deletion property, and what survives deletion

Drop the desk page + toggle wiring: one commit (menu row, page file, dispatch branch, tests, the toggle
call sites). Committed effects are unaffected — they live in the corpus and `mcp.json`, never hostage to
the table; the proposal table remains as history and is safe to `DROP` with the page (test map #7 proves
both halves).

## 2. The method shapes

```jsonc
// proposal.create — or the refusal, reason verbatim from the validator
{ "kind": "mcp_server_add",
  "payload": { "name": "github", "transport": "stdio", "command": "gh-mcp", "tier": "trusted" } }
// → { "id": 7, "kind": "mcp_server_add", "status": "proposed",
//     "created_at": "2026-10-26T15:04:02Z", "created_by": "cjlapao",
//     "command": "aeon proposal commit 7" }

// proposal.list {status?: "proposed"} → newest first
{ "proposals": [ /* rows as above, each with status + decided_* when set */ ] }

// proposal.discard {id} → { "id": 7, "status": "discarded" }
//   or { "ok": false, "error": "already committed by cjlapao at 2026-10-26T15:11:40Z" }   // TD1

// agent.enable {name} / agent.disable {name} — TransitionPluginAgent verbatim
// → { "agent": "reviewer", "state": "enabled" }
// agent.revoke {name, reason} — reason omitted → un-writable (TD5), not refused
```

The CLI door (TD4), secondary door though the only promoting one:

```
aeon proposal list [--status proposed]     # table, newest first
aeon proposal commit 7                     # re-validate → effect via existing path → flip
aeon proposal reject 7 [--reason "..."]
```

## 3. The bench pages, per page

- **AgentsPage** (S34's four strips) gains per-agent controls: enable/disable/revoke (with reason field,
  required, matching TD5) → `agent.*` methods, live immediately (plain writes) — and the grants strip's
  toggle → `proposal.create` + toast (TD7). Pending proposal over a toggle renders per TD8.
- **McpPage** gains *Add server* (fields = `ServerConfig`'s: name, transport, command|url, tier — the form
  validates locally, the daemon's validator is the authority) and per-row *Remove* → proposals; a server
  with an open add-proposal shows *"proposed · awaiting commit"* and **no Check button** (nothing to probe
  until commit — S34's chip `not checked` would lie).
- **PolicyPage**: live table unchanged; new Proposed table (TD8), rows deep-linked to the desk.
- **ProposalsPage — the desk**: open proposals newest-first, payload diffed against live (config-vs-adding
  row; corpus-row-vs-proposed-row), validator reason shown for anything refused at create, and the exact
  `aeon proposal …` command per row as copyable text. No action promotes (TD7); the only action is
  *Discard*.

## 4. Where each fact and effect comes from (P34 check)

| Surface fact/effect | Route |
| --- | --- |
| open proposals, statuses, decision records | `proposal.list` |
| creating / discarding a proposal | `proposal.create` / `proposal.discard` |
| agent state change (enable/disable/revoke) | `agent.*` methods → `TransitionPluginAgent` |
| live corpus / verdicts | existing `policy.*` reads (S34 pages already use them) |
| live MCP config truth | existing `mcp.status` (S34) |
| promoting a proposal | **CLI only** (TD7 — deliberately not in this table's socket column) |

## 5. The test map

1. **The slice test** (brief criterion 1): create one proposal of every kind via socket → `policy.why` /
   `policy.inForce` full output byte-identical → commit one via the CLI door → exactly that verdict
   flips. Table-driven over the four kinds.
2. **PL15 amended** (criterion 2): pre-amendment assertions byte-preserved (fixture hash of the original
   block), whitelist assertion green; a canary test *adds* a forbidden socket write in a fixture file and
   asserts the test fails.
3. **Validator parity** (criterion 4): a payload refused by `aeon mcp add`-equivalent validation is
   refused by `proposal.create` with the same reason string; same for policy kinds.
4. **Store teeth through the new door** (criterion 5): socket `agent.revoke` without reason → un-writable;
   `decide` on a decided row → `already <status>`; both are the same store functions the CLI uses.
5. **Crash-and-retry commit** (TD4): effect applied + flip faked-failed → re-commit completes; effect
   failed → `applied_error` set, row stays `proposed`.
6. **Three states + MC11** (criterion 6): idle controller → `unwired` desk; empty table → *"no
   proposals"*; refused payload renders validator's reason, never a silent form reset.
7. **Deletion drill** (TD12): scratch branch drops the desk + wiring in one commit; corpus and config
   byte-unchanged; `DROP TABLE proposal` + page removal still leaves every existing suite green.
8. **Side-door grep** (TD7): no `commit` string reachable from the UI protocol surface — there is no
   socket method to call even by mistake (the method table has no `proposal.commit` at all; the grep pins
   that it stays that way).

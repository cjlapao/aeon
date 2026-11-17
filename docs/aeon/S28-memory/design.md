# Design — S28 Memory

**Brief:** [`brief.md`](brief.md). **Plan:** [`plan.md`](plan.md). **Demo:** [`DEMO.md`](DEMO.md).

**Written 2026-10-26**, against the real codebase: S13's actual registry (`internal/handler`), S14's own
brief (not built, but its chain-depth/cycle mechanism is already specced in enough detail to reuse rather
than guess at), and S4's store (`sqlite-vec` wiring, confirmed working generically; no `vec0` table exists
in `schema.go` yet from any slice — this design is the first, not a follower of an established pattern the
brief's phrasing could be read to imply).

## 1. Context

The brief's five questions split into two kinds: three (Q1 provenance naming, Q3 the self-logging gate, Q5
tag storage) are this slice's own calls to make; two (Q2 depth bound, Q4 board scope) turned out to be
mostly answered by reading S14's brief more closely than the brief itself had occasion to. **Two more
findings surfaced that neither the brief nor any prior conversation anticipated**: this slice's natural
package name, `internal/memory`, is already taken by S20's real, built, differently-shaped mechanism (§2);
and agent-scoped memory cannot FK against a real `agent` table, because S13's 25 built-in bodies have no
database row at all (§3).

## 2. The correction: this slice is not `internal/memory` — that name is already taken

**Checked before naming a single file**: `internal/store/events.go` already has a real `Memory` type,
`AppendMemory`/`ReadMemory` — S20's own "memory harvest" (`store.go`'s own comment list names it exactly
that), a per-loop notes table (`memory`: `id, loop_slug, project_root, ts, agent, taskid, note`,
cascade-deleted with its loop). **It is a completely different thing**: ephemeral, loop-scoped, no
provenance, no tags, no cross-project survival — the opposite of what this slice builds. A package, CLI
verb or socket method prefix named `memory`/`memory.*` would sit one character away from a real, built,
differently-shaped mechanism already using that name.

**Resolution**: this slice's package is `internal/learnings`, its CLI verb is `aeon learnings`, its socket
methods are `learnings.*`. The slice's own title (`S28 Memory`) stays unchanged — it is already established
across `ROADMAP.md`/`STATUS.md`/this brief — but every actual code identifier uses "learnings," the word
the brief's own body text already prefers throughout. This is the same kind of near-miss M4 already named
for "skill" (three unrelated meanings in this project); naming it here rather than discovering it mid-T2
is cheaper.

## 3. The correction: "agent_id as a foreign key" doesn't work as written

**`brief.md` §4 says**: *"A learning's `agent_id`... is a foreign key into S13's registry."* **Checked**:
`internal/handler/registry.go` loads the 25 bodies from an embedded filesystem (`bodiesFS.ReadFile`) at
`init` — there is no `CREATE TABLE agent` anywhere in `schema.go`, and `grep` for one across the schema
confirms it. `Lookup(name) (Handler, bool)` is a Go-level, in-process check, not a SQL join target.

**A second kind of agent does have a row**: S15's `plugin_agent` (imported agents), keyed by `name`, with a
live-row partial unique index (`plugin_agent_live`) excluding `revoked` rows.

**Resolution**: a learning's agent scope is validated at write time by a Go-level check —
`handler.Lookup(name)` returns true, **or** a `plugin_agent` row exists for `name` with `state != 'revoked'`
— never a SQL foreign key. The column is plain `TEXT`, and `internal/learnings`'s own write path is the one
place this check lives, so a learning scoped to a nonexistent agent is refused there, not silently accepted
by a schema with nothing to enforce it.

## 4. Decisions

### M1 (brief Q1) — the fourth provenance value is `relayed`, trusted below all three others

`told` (a human said so — direct authority), `observed` (with N — direct empirical count), `assumed` (the
recording agent's own inference) are the original three, in descending but all *first-hand* trust. **A
board answer is second-hand**: the answering agent's own belief, whatever provenance *it* carries on their
side, arrives here stripped of that context — this slice does not chase provenance through another agent's
internal state. **`relayed` sits below `assumed`**, not between `told` and `observed` as the brief's
phrasing left open: `assumed` is at least the *receiving* agent's own reasoning, inspectable and
contestable; `relayed` is opaque trust in an unexamined claim. A query that surfaces a `relayed` learning
shows that label plainly — never silently promoted to `told` because a confident-sounding agent answered.
**`relayed` is write-protected to one caller**: the CLI and socket `Append` surfaces (T4) refuse it outright
— it is set only by T7's board-answer hook, never hand-authored, the same way a provenance a person could
freely claim would make the label meaningless.

### M2 (brief Q2) — no new depth bound; S14's chain mechanism is reused wholesale

`S14-agent-services/brief.md` **D7**: *"Every chain edge is a row, and the chain is reconstructable after a
restart."* **D8**: *"A cycle is refused with the cycle named — not as a cost-cap failure."* **D9**: *"A
chain one deeper than the bound is refused at the boundary."* **R2** there, worth repeating because it is
exactly S28's own D10 concern stated independently: *"The cycle check is a depth check. They are different:
depth catches runaway, a cycle catches a loop."*

**A board question is an ordinary A2A skill invocation (D11, brief) — it is already a chain edge S14's own
mechanism records, bounds and cycle-checks.** This slice adds no second bound, no second cycle detector, and
no number of its own. S14's own Q1 (*"the depth bound, as a number"*) is resolved in real, built code:
`agentsvc.ChainDepthBound = 4` (`internal/agentsvc/chain.go`, checked 2026-10-26) — a backstop against a
runaway, explicitly not a cap meant to be raised, per that constant's own comment. If board questions ever
need a *different* bound than ordinary skill chains, that is a parameter to S14's existing mechanism, a
real decision against real code now, not a hypothetical one.

### M3 (brief Q3) — no review gate; provenance-honesty is the safeguard, not a hold

**No gate.** A self-logged learning (an agent recording its own mistake, no human present) is stored and
immediately retrievable — the same moment any other learning becomes retrievable. **The guard against R4's
"unreviewed claim persists invisibly" is D3 itself, enforced strictly**: every retrieval surfaces provenance
and strength alongside the claim, never the claim alone. A caller sees *"assumed, logged by `golang-
engineer-loop`, never independently confirmed"* — not a bare fact indistinguishable from a `told` one. A
hold-for-review gate was considered and rejected: it would mean self-logging is cheap to *write* but not
cheap to *use* until someone reviews it, which defeats the brief's own §3 claim ("the learnings half needs
nothing unbuilt except the table itself") by quietly adding a review workflow as a second, unbuilt thing.
**This also settles `S29-triage/brief.md`'s own dependency on this answer** (its Q1, resolved to feed this
store automatically): an automatic feed with no gate is exactly what "automatic" there requires — a gate
here would have silently broken that resolution.

### M4 (brief Q4) — the board invokes one narrow, universal skill, not an agent's arbitrary skill list

S14's own `Skill` concept (A2A card, `securitySchemes`, `SkillAccess`) has no built-in distinction between
"answers from existing knowledge" and "does real work" — checked directly, no such field exists in S14's
design. Letting the board address *any* declared skill would make N6 ("no delegation wearing a question's
clothes") a matter of hoping the asker chooses responsibly, not something enforced.

**Resolution**: every agent exposes one additional, narrow, always-present skill — named `answer` — whose
only contract is *"respond using what you already know; take no other action."* The board can only invoke
`answer`, never an agent's working skills. Mechanically this is still D11's "ordinary S14 skill invocation,"
and N3's "no new tool, no new transport" holds — `answer` is a skill declaration convention this slice adds
to every body (a one-line addition per body, the same shape S13's own per-body frontmatter already uses),
not a new invocation mechanism. **This is the one place this slice's own scope reaches outside
`internal/learnings`** into the 25 bodies themselves, and it is named here so it is not a surprise mid-task.

**Found only once S14's real code existed to check (2026-10-26): a frontmatter declaration alone is not
enough.** `agentsvc.Invoker.Invoke` refuses before it reads any skill list at all if the callee has no real,
signed `AgentCard` — and none of the 25 bodies has one today (checked: only test fixtures exist under
`internal/agentsvc/testdata/cards/`). T6's scope grew to cover issuing real, self-signed cards (Ed25519,
no external ceremony) alongside the frontmatter declaration. A second, separate operational gap — no
`SkillAccess` policy rows grant anyone permission to ask `answer` yet, even once cards exist — is named but
deliberately not solved by this slice's own tasks (T6's Notes), matching the project's standing "authoring
is deliberate, never automatic" pattern (the same shape `CommandAccess`'s `seed-corpus` step has).

### M5 (brief Q5) — tags are a join table, not a JSON column

`project.tags` (S4) is a `TEXT NOT NULL DEFAULT '[]'` JSON array — fine for a handful of display-only tags
per project, never queried by tag directly. D2 promises *"queryable by exact match or prefix"* at
potentially high volume (every learning, across every agent and project) — a JSON column would mean either
full-table JSON parsing per query or reaching for SQLite's JSON1 functions on every read, neither indexed.
**`learning_tag(learning_id, tag)`, with an index on `tag`**, gives exact-match and `LIKE 'prefix%'` queries
a real index to use, the same reasoning that put `idx_step_loop`/`idx_loop_updated` on the schema at S4's
own design time rather than as a later migration once a slow query surfaced.

**Tag validation stays exactly "non-empty string," nothing more** — the brief's own locked decision (§5):
no canonical registry, no authoring step that blocks on inventing one.

### M6 — a board answer files as a project-scoped learning, not an agent-scoped one

**Not one of the brief's five questions, found while writing T7's task spec.** The brief's own §1 says a
board answer should mean *"the next time **any agent** needs the same answer, it is retrieved rather than
re-asked"* — that is project scope's job (any agent working that project can retrieve it), not agent
scope's (which follows one specific agent across projects, visible only to that one agent again). **M6
locks it**: `AskBoard`'s completion hook (T7) files the answer with `scope: 'project'`, `scope_key` the
asking loop's `project_root` — never `scope: 'agent'`. An agent-scoped `relayed` learning is not refused by
anything in this design (a future caller could still choose it), but the board's own default, and the only
path T7 builds, is project scope, because that is what the brief's own sentence promised.

## 5. Schema shape (T1's job to make literal)

**`learning`**: `id`, `scope` (`TEXT NOT NULL CHECK (scope IN ('person','project','agent'))` — D1's three,
closed), `scope_key` (`TEXT NOT NULL` — a user identity for `person`, a `project_root` for `project`, an
agent name for `agent`; which one is which is determined by `scope`, validated in Go, not a three-way FK
SQLite cannot express cleanly), `body` (`TEXT NOT NULL`), `provenance` (`TEXT NOT NULL CHECK (provenance IN
('told','observed','assumed','relayed'))` — M1's four), `observed_count` (`INTEGER`, nullable, meaningful
only when `provenance = 'observed'` — **set once, at write time, by the caller's own count; this slice does
not merge repeated observations into one row with an auto-incrementing count**, which would need claim
identity/deduplication logic far beyond this slice's scope, named here so nobody builds it by accident),
`source_loop_slug`, `source_step_id` (both nullable — traceability, never required), `created_at`.

**`learning_tag`**: `learning_id` (FK, `ON DELETE CASCADE`), `tag` (`TEXT NOT NULL CHECK (tag != '')`).
Index on `(tag, learning_id)` for prefix/exact queries; `(learning_id)` alone for "all tags on this
learning."

**`learning_vec`**: the `vec0` virtual table (sqlite-vec, S4's DB18 extension — the first `vec0` table
anywhere in this schema, §2's correction to the brief's implied precedent), one row per `learning.id`,
populated only where an embedding route is configured (D5: absent entirely is a valid, working state, not a
degraded one).

## 6. Architecture

```
  write path                              read path
  ─────────                               ─────────
  internal/learnings.Append                internal/learnings.Query
    │                                       │
    ├─ scope+scope_key validated           ├─ tag filter FIRST (D5 — always
    │  (Go-level: handler.Lookup           │  available, never depends on an
    │  or plugin_agent row, §2)            │  embedding route)
    │                                       │
    ├─ provenance in M1's four              ├─ embedding route, if configured,
    │                                       │  RANKS within the tag-filtered
    ├─ tags → learning_tag rows             │  set only (D6 — never chooses,
    │                                       │  never substitutes)
    └─ embedding, if route configured,      │
       → learning_vec                       └─ every result carries provenance
                                                + strength, unconditionally (M3)

  board question (S14 skill invocation, M4's `answer` skill)
    │
    └─ on completion → Append, provenance='relayed' (M1), same path as any write
```

## 7. Testing this slice has to do

- **M1 as a property**: a query returning a `relayed` learning never sorts or displays it above an
  `assumed` one from the same scope on strength alone — asserted on ordering, not just presence of the
  label.
- **M2 by absence**: this slice's diff touches no chain/depth/cycle logic anywhere — grep-asserted, the same
  "prove it by what's NOT there" discipline S24's T5 used for sizing-recursion.
- **M3 directly**: a freshly-written self-logged learning is retrievable in the same call sequence that
  wrote it — no second step, no pending state.
- **M4's boundary**: a board question targeting anything other than the `answer` skill is refused before any
  model call (mirroring S14's own D4 shape for a missing required field).
- **D5/D6 as properties**: a query with zero embedding route configured still returns tag-filtered results;
  a query with one configured never returns a result the tag filter alone would have excluded.
- **§2's validation**: a learning scoped to a nonexistent agent name is refused, tested against both a
  made-up name and a `revoked` `plugin_agent` row (which must also refuse — revoked is not merely disabled).

## 8. Amendments to earlier slices

**One, forward-only, not made yet.** M4 means every one of S13's 25 handler bodies eventually gains an
`answer` skill declaration. **Not done in this slice's own tasks** — S13 is already built, and retrofitting
25 files is this slice's own T-numbered task (see `plan.md`), not a silent edit made while writing this
design document. Recorded here so the scope is visible before anyone starts T-counting.

## 9. Open questions this design did not close

- **Whether `answer`'s result needs AS12's "non-first-party untrusted" label** (S14's own open Q3, inherited
  here unchanged) — this slice's board answers are always first-party (S13's own bodies, by construction of
  M4), so the label likely never fires for a board question specifically, but that is worth confirming once
  S14 actually exists, not assumed now.

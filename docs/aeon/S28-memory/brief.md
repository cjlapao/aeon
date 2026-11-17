# Brief — S28 Memory

**Implements:** [`design.md`](design.md) — written 2026-10-26, resolving all five §7 questions and finding
two the brief could not have: this slice's natural package name, `internal/memory`, is already a different,
real, built mechanism (S20's per-loop notes) — the actual code is `internal/learnings` throughout; and
§4's "agent_id... is a foreign key into S13's registry" does not work as written, since S13's 25 built-in
bodies have no database row to reference (Go-level validation instead, `design.md` §3). **Demo:**
[`DEMO.md`](DEMO.md) — predictive. **Plan:** [`plan.md`](plan.md) — eight tasks, `tasks/T1.md`–`T8.md`,
**T7 was blocked on S14 — S14 is now built (2026-10-26 check), and T6/T7 are updated against its real API**
(`agentsvc.Invoker.Invoke`, `ChainDepthBound = 4`). **All 8 tasks are buildable now**, with one real
prerequisite inside the slice itself: T7 needs T6's own card-generation piece (T6's scope grew once S14
landed — none of S13's 25 bodies has a real signed `AgentCard` yet, so T6 now issues them, not just
frontmatter).
**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — P31/P32 are binding. **A13** (dispatch stays
closed; embeddings serve discovery, never dispatch — already binding on S12, S13 and S14) governs this
slice's own use of embeddings; it is not relaxed here. **G14** (attendedness pinned, never inferred) and the
**A1 amendment** (delegation is a graph; cost rolls up along a recorded chain) govern the message-board half
exactly as they govern any other delegated step — no special-casing.
**Stage:** 5 · Fleet, second of six to get a real brief (after S24) — and for the same reason: most of what
it needs already exists. **Depends on:** S4 (store — `sqlite-vec` is already statically linked, DB18; no new
load step), S13 (agents — a learning or a board question is scoped to an agent that has to exist as an
entity first), S9 (app — a test-bench surface). **The message-board half additionally depends on S14** (A2A
skill invocation — **built**, 2026-10-26 check — see §4). **Explicitly independent of S22** (retrieval) — see
§5's corpus-boundary decision — and of S24, S25, S26, S27: none of this slice's dependencies run through them.
**Repo:** Aeon lives at `aeon/` in this repository (P24) · Go 1.26 · `cgo` enabled (B3)
**Checked 2026-10-26 against P34** (`ARCHITECTURE.md`): the learnings core (T4) already pairs `aeon
learnings` with `learnings.*` socket methods, correctly. **One real gap found, not fixed here**: nothing in
`design.md` or `plan.md` says what actually calls `AskBoard` (T7) — not a CLI verb, not a socket method, not
a native tool (N3 forbids the last). A capability with no stated caller at all is a smaller problem than a
CLI-only one, but it is still true that whichever entry point gets chosen — a step dispatched from a
blueprint, or a direct person/app action — must be socket-reachable from the start, never a CLI command
discovered after the fact. Recorded as a new §7 Q6, not guessed at here.

---

## 1. What this is

**Two capabilities, specced together because the second is mostly a particular use of the first.**

**A classified, queryable record of what an agent, a project, or a person has learned** — in the database,
not a markdown file that re-emits itself whole on every append (the exact bug `packages/loop-state-mcp`'s
`Learnings.md` still has — `ROADMAP.md` §9). Every learning carries free-form tag metadata — files,
functions, intentions, whatever the recording context found relevant — so a later query can narrow to "what
does this agent know about auth" instead of reading everything it has ever learned.

**A directed way for one agent to ask another a question**, addressed by name the way a blueprint step
already addresses an agent's skill. The answer is not a side channel: it becomes one more row in the same
learnings store, so the next time any agent needs the same answer, it is retrieved rather than re-asked.

## 2. Definition of Done

| | |
| --- | --- |
| **D1** | **Three scopes, not two.** Person (cross-project, at the store level — S4 already made it per-user) and project (per root) are the original sketch's two; **agent is a third, also cross-project** — a learning scoped to an agent follows that agent into every project it runs in next. |
| **D2** | **A learning carries free-form tag metadata** — arrays of strings (files, functions, intentions, or anything else the recording context found relevant), queryable by exact match or prefix, with no fixed vocabulary and no authoring step that blocks on inventing one. |
| **D3** | **Provenance and strength are carried on every learning** (the sketch's existing rule): `told` / `observed` (with N) / `assumed` / and a fourth value for an answer relayed from another agent — naming it, and deciding how much it is trusted relative to the other three, is this brief's own open question (§7 Q1). |
| **D4** | **A learning is retrieved, never concatenated into a system prompt** — A13's twin rule, already binding on S13's bodies, not relaxed for this slice's own content. |
| **D5** | **Tag filtering never depends on an embedding route existing.** A learnings query with no embedding model configured still narrows by tag and still returns results — the same shape as S22's V9 (keyword-only, said out loud), independently reasoned here because this slice does not depend on S22 existing. |
| **D6** | **Where an embedding route is configured, it ranks within the tag-filtered set — it never chooses which agent answers, and never substitutes for tag filtering.** A13's dispatch-stays-closed rule, restated for this slice's own embeddings rather than inherited by reference alone. |
| **D7** | **This slice's embedding index is its own, not a third corpus inside S22's.** S22's V2 locks "two corpora, never merged" and V12 locks "index per project root, no cross-project search" (N5) — both true of code and history, both wrong for an agent-scoped learning that has to survive moving between projects. Reusing `sqlite-vec` (already in the store, S4's DB18) is not a violation of S22's claim; a third corpus *inside* S22's index would be. |
| **D8** | **A posted board question always names its target agent.** No open or broadcast posting — decided in conversation, not inferred — which is what keeps the board inside A13 instead of reopening the dispatch-by-discovery problem A13 was written to close. |
| **D9** | **A board question's answer runs as an ordinary step** — same attendedness pin (G14), same cost-chain recording (A1's amendment), same `SkillAccess` policy gate S14 already defines for one agent invoking another's skill. No separate governance path for "it's just a question." |
| **D10** | **A bound exists on how deep a chain of agent-to-agent questions may go**, so A asking B asking C asking A back terminates rather than recursing forever — the same shape as S5's iteration bound on a tool loop. The number is an open question (§7 Q2), not a guess made here. |
| **D11** | **The board is a use of S14's existing skill-invocation mechanism, not a new tool or a new transport.** "Agent invokes another named agent's skill" is already S14's design; this slice's own contribution is the convention (ask a question, get an answer) and the hook that files the answer as a learning. |

**D7 and D8 are the two that matter.** D7 because it is the whole reason this slice does not have to wait for
or amend S22, and does not quietly grow a second, conflicting definition of "the corpora." D8 because it is
the one piece of this brief that keeps the board from becoming the kind of free dispatch A13 already refused
once, for the handler registry, and refused again for A2A skills.

## 3. Why it is cheaper than it looks

**The learnings half needs nothing unbuilt except the learnings table itself.** `sqlite-vec` is already
statically linked into the store (S4's DB18) — S22 proved that path works for code and history; this slice
reuses the same extension for a different corpus, not a different mechanism. Tag filtering is an ordinary
indexed column (or a join table for the arrays), no new query engine. Provenance and strength are already
decided by the original sketch; this slice adds a fourth value to a closed set, not a new concept.

**The board half needed S14 to exist, and now it does — almost nothing of its own beyond that.** "Agent
invokes another named agent's skill" is S14's real, built mechanism (`agentsvc.Invoker.Invoke`) — the
asking step suspends or waits exactly as any other A2A Task already does, with no new suspension reason and
no new transport. The only genuinely new pieces are: the hook connecting the two halves (when a Task
started as a directed question completes, its answer becomes a learnings row instead of a Task result
nobody files anywhere), and — found only once S14's real code could be read — **real signed cards for the
25 bodies**, which nothing before this slice issues (T6).

## 4. The seams

**Downward — S4's store and S13's agent registry.** A learning's `scope_key` (when scoped to an agent) is
validated against S13's registry **at write time, in Go** — not a SQL foreign key, since S13's built-in
bodies have no database row to reference (`design.md` §3 found this and corrected the framing here). A
learning cannot be scoped to an agent that does not exist, by that check. `sqlite-vec` is S4's, not
reimplemented here.

**Sideways — S14's A2A skill invocation, built.** A board question is an ordinary directed skill call.
This slice adds no new suspension reason and no new transport; it only decides what happens to the answer.

**Sideways, deliberately not inherited — S22's two-corpora model.** This slice's embedding index is a third,
separate thing, not a third corpus inside S22's. S22 is the one deletable slice in the plan, and this slice
must work whether or not S22 is even present — the opposite dependency direction "reuses the same mechanism"
might suggest at a glance.

**Sideways — a future producer, S29's validation attempts.** Once S29 exists, a validation attempt's
conclusion is filed here as an ordinary `observed` learning, project-scoped — no new provenance value, no
schema change on this side. This slice does not depend on S29 to exist; S29's own feed hook carries that
dependency, not this store (see S29-triage/brief.md D10).

## 5. Locked decisions

- **Agent-scoped memory is a third scope, cross-project**, alongside the original sketch's person
  (cross-project) and project (per-root) memory. The sketch's "two stores, not three" line is superseded by
  this decision and needs updating wherever it is repeated.
- **A board question is always directed — the asker names the agent it is asking.** No un-addressed posting,
  no open broadcast. An agent choosing *who* to ask is a decision a human or a closed registry makes
  reachable (the same shape as S13's `by-tech-tag`), never a decision resolved by similarity at ask time.
- **An answered board question becomes one more row in the same learnings store**, under a new provenance
  value, rather than living in a second, parallel store. One store stays the source of truth for "what does
  this agent know" — the same reasoning that keeps history (S25) from becoming a second source of truth for
  what a loop did.
- **Tags are free-form.** No canonical registry, no authoring step that blocks on choosing from a fixed list.
  Retrieval (D5/D6), when an embedding route exists, is what is expected to bridge "auth" and
  "authentication" — the tag layer itself does not try to.
- **"Skill" in this slice's board context is S14's A2A `Skill`**, not the opencode skill-loading mechanism
  S13 retired (`dev-loop-protocol`, A5). Worth restating because S24's brief already had to make the same
  distinction for a different reason (its sizing run) — this project now has three unrelated uses of the
  word "skill," and this is the second slice to have to say so.

## 6. Out of scope

- **N1 — No change to S22's corpora, invariants or schema.** This slice's embedding index is structurally
  separate (D7); S22's V2 and V12 are not amended.
- **N2 — No open or broadcast board.** Restated as a non-goal, not only a Definition of Done item (D8),
  because it is the one thing most likely to be "simplified" back in during implementation.
- **N3 — No new native tool, no new transport.** The board is a use of S14's existing skill invocation (D11).
- **N4 — No change to G14 or the A1 amendment.** This slice is a caller of both, the same relationship S24 has
  to G14, not a second implementation of either.
- **N5 — Project-scoped learning stays per-root.** Only agent-scoped learning crosses projects; this slice
  does not make project memory cross-project by extension.
- **N6 — No delegation wearing a question's clothes.** A board question's answer is text or guidance from what
  the answering agent already knows; it is never a mechanism for agent A to hand real work to agent B. That
  would be delegation, which A1 already forbids ("blueprints replace delegation") — see R1.

## 7. Questions to answer before building

| # | Question | Where |
| --- | --- | --- |
| Q1 | What is the fourth provenance value's name, and is it trusted more like `told` or more like `observed` when two learnings disagree? | design.md |
| Q2 | What is the actual depth/cycle bound on chained agent-to-agent questions, and where does it live — a fixed constant like S5's iteration bound, or a policy-governed cap? | design.md |
| Q3 | Does a self-logged learning (an agent recording its own mistake, no human in the loop) get stored and made retrievable immediately, or does it need a gate before it is trusted enough to retrieve? The original sketch already warns that a learned belief "can be [wrong], and it persists invisibly" — this slice makes logging one cheaper, which raises the volume of unreviewed claims unless this is answered deliberately rather than defaulted. **Now load-bearing for S29 too** — its own Q1 resolved to feed this store automatically, inheriting whatever this question settles rather than inventing a second gate. | design.md |
| Q4 | What exactly may be asked on the board — any skill an agent declares, or a narrower subset carved out specifically for answering from existing knowledge, distinct from a skill that does real work? Needed to keep N6 enforceable rather than aspirational. | design.md |
| Q5 | Where does tag metadata live structurally — a JSON array column, a join table, something else — and is a tag validated beyond "non-empty string"? | design.md |
| Q6 | What actually calls `AskBoard` — a blueprint step's own action (the daemon dispatches it the way S26's `project-planner` is dispatched), or a direct person/app action naming an agent and a question? Neither `design.md` nor `plan.md` says (P34, added 2026-10-26) | design.md — and whichever answer is chosen must be reachable from the socket, not only a CLI verb, since N3 already rules out a native tool as the trigger |

## 8. Risks

- **R1 — A question in disguise is delegation.** A1 forbids spawning or delegating work; a board that lets A
  hand B actual work rather than ask something B already knows is delegation with a friendlier name. **The
  guard is N6**: a board question's shape is a question, and its answer is never a commit, a PR, or any
  action taken on the asker's behalf.
- **R2 — The two embedding uses blur into one.** Because both this slice and S22 use `sqlite-vec`, it would
  be easy to let the learnings index become a third corpus inside S22's, or let S22 grow a learnings-shaped
  corpus of its own. **D7 is the guard**: structurally separate, on purpose, regardless of the shared
  extension underneath.
- **R3 — A learning becomes prompt-injected content instead of retrieved content.** The exact failure S13
  already guarded against for the 25 bodies (D2 there, D4 here). Convenient to get wrong the same way twice.
- **R4 — An unreviewed, wrong learning persists invisibly.** The original sketch's own warning, now sharper:
  this slice makes recording a learning a native capability instead of a manual markdown edit, which raises
  the volume of unreviewed claims unless Q3 is answered deliberately rather than left to default to
  "whatever is cheapest to implement."

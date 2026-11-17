# Aeon — the roadmap

> **Completion tracking moved to [SLICES.md](SLICES.md).** This document preserves historical planning and evidence. Its progress claims may be stale. Update task Notes and SLICES.md for new work; do not mirror completion here.

**The single source of truth for what gets built, in what order, and why.** This is the plan — for what is
actually built so far, see **[STATUS.md](STATUS.md)'s at-a-glance board**, not the dates below.
**Date approved:** 2026-09-16, through Stage 4; Stage 5 was sketches at that date and has since grown real
briefs, designs and task files for most of its slices (§2).

This document replaces `PLAN-day-one.md`, `PLAN-agent-services.md`, `NEXT.md` and `99-deferred-mcp-client.md`.
Everything else in `docs/aeon/` is either a slice (`S<n>-<name>/`) or a companion:

| Document | Holds |
| --- | --- |
| **ROADMAP.md** *(this)* | stages, slices, ordering, decisions, risks |
| [`VISION.md`](VISION.md) | **the picture** — what this is, diagrams, mockups, the end state. Start here to *see* it |
| [`TASKS.md`](TASKS.md) | **every task, in execution order**, plus the readiness audit and what the framework does and does not provide |
| [`PLAN.md`](PLAN.md) | **what to do and in what order** — prerequisites, framework needs, the ui-kit, the five phases, and **a dependency map of what can run in parallel** |
| `ARCHITECTURE.md` | the invariants — the principle set every slice argues from |
| `SEEDING.md` | how to execute a slice |
| `blueprints/` | five importable blueprints derived from the two real protocols |
| `REQUEST-localrpc-service.md` | the ask to `go-backend-scaffolding` |

---

## 1. Shape

**23 slices, 232 tasks, five stages plus the garage.** Every slice ends in a `DEMO.md` — something a person can run and watch work.

| Stage | Slices | Tasks | The thing that works at the end |
| --- | --- | --- | --- |
| **0 · Garage** | S1 | 7 | It builds, on six targets, with one command |
| **1 · Skateboard** | S2–S5 | 44 | It talks to a model, remembers, and runs a step end to end |
| **2 · Bicycle** | S6–S9 | 40 | It suspends, recovers, follows a blueprint, and has a UI |
| **3 · Car** | S10–S17 | 88 | It is governed, has agents with tools, touches git, runs the full ladder |
| **4 · Shuttle** | S18–S23 | 54 | Many providers, a narrow surface, triggers, remote agents, retrieval, first run |
| 5 · Fleet | S24–S37 | — | S24, S25, S26, S28, S29, S30, S32, S34, S35, S36 and S37 are fully planned, with tasks; S33 is **built 2026-10-30**; S27 is specced; the rest are ideas with a shape |

## 2. The slices

### Stage 0 · Garage — *it builds*

| # | Slice | Tasks | Delivers |
| --- | --- | --- | --- |
| **S1** | Bootstrap | 7 | One Go binary, daemon and CLI, six build targets, `make dev` |

### Stage 1 · Skateboard — *it remembers, and it runs a step*

| # | Slice | Tasks | Delivers |
| --- | --- | --- | --- |
| **S2** | Protocol | 8 | The socket, JSON-RPC, events with a cursor |
| **S3** | Model layer | 12 | A canonical transcript, one provider, honest cost |
| **S4** | Store | 12 | SQLite, migrations, the transcript survives a restart byte-for-byte |
| **S5** | One step | 12 | A runtime that assembles context and loops over tools |

### Stage 2 · Bicycle — *it works on its own*

| # | Slice | Tasks | Delivers |
| --- | --- | --- | --- |
| **S6** | Suspend | 9 | Suspension by exiting (P2) — a question is a row, not a held process |
| **S7** | Recovery | 8 | A daemon crash is survivable; a running step resumes cold |
| **S8** | Blueprints | 12 | The graph is data, the interpreter is code (L1); pinned per loop (L6) |
| **S9** | App | 11 | The Tauri UI |

### Stage 3 · Car — *it is governed, and it has minions*

| # | Slice | Tasks | Delivers |
| --- | --- | --- | --- |
| **S10** | **Policy** ✨ | **12 ✅** | §3 — the governance interface over everything |
| **S11** | Permissions | 13 | The 366-rule `bash` corpus, now `CommandAccess`; effect and re-run classes |
| **S12** | **MCP client** ✨ | **10 ✅** | Three tool tiers; private namespaces that are *invisible*, not merely unauthorized |
| **S13** | Agents | 11 | The 25 bodies — **unchanged** |
| **S14** | **Agent services** ✨ | **12 ✅** | A2A cards, typed skills, Tasks; agent-to-agent with the chain recorded |
| **S15** | **Agent plugins** ✨ | **9 ✅** | Import like a skill, security check, enable/disable, revocation that revokes |
| **S16** | Git / forge | 12 | Branches, commits, PRs, review comments |
| **S17** | Full ladder | 9 | The whole protocol runs end to end |

### Stage 4 · Shuttle — *it reaches out*

| # | Slice | Tasks | Delivers |
| --- | --- | --- | --- |
| **S18** | Providers | 10 | Many routes; capability verification with three states |
| **S19** | Narrow surface | 8 | The HTTP projection — enumerated, never a mirror of the socket (P25) |
| **S20** | **Triggers** ✨ | **9 ✅** | §4 — **the clock**. `trigger` and webhooks already designed in S19/S11 |
| **S21** | **Remote agents** | 9 | A2A over the wire: TLS, trust store, egress policy |
| **S22** | Retrieval | 9 | Embeddings and indexes; optional and deletable |
| **S23** | First run | 9 | `init`, `doctor`, the path from nothing to working |

### Stage 5 · The fleet — *S24, S25, S26, S28, S29, S30, S32, S33, S34, S35, S36 and S37 fully planned; S27 specced; the rest sketches, no tasks*

**Right now Aeon runs a loop. With these it runs a project.** S24, S28 and S29 were the first three
exceptions to the rest of this stage: a project-scoped backlog of loops, a classified cross-project memory,
and bug triage are all buildable today (at least in large part), over systems that already exist (S8's
blueprints, S4's store, S5's one-step loop, S13's agent registry, S9's app), so each got a real brief rather
than a sketch. **S24, S25, S28, S29 and S30 go one step further, each for its own reason.** S30 — a structural
code graph, queryable separately from S22's content
retrieval, needing only S4's store and S9's app — was never one of the original five sketches this stage
started with; it is a new addition, proposed and specced in one sitting straight to task files. **S24 was
re-examined on 2026-10-26, once S11–S13 were built**: its brief had flagged five open questions as needing
code that did not exist yet when it was written; with that code now real, `design.md` answers all five
(and two more the investigation surfaced — loop status has no `CHECK` and `blocked` conflates retriable and
exhausted loops) and the slice now has `design.md`, `plan.md` and nine task files. **S25 was promoted the
same day**, kept to its original sketch's scope exactly — a pure read, no new storage — once its one real
gap (a naive query over live loops alone would silently stop showing archived work the day a project's
first loop is archived — the archive tables themselves are S4's, built, though nothing yet calls them in
practice) was found and designed around.

**S33 Telemetry is new this session, split out of the S25 conversation on purpose.** The sketch's author
asked for rich per-turn capture (token composition, timing, file/command activity); that need is real but
is not a read over existing data the way S25 is — it requires new instrumentation in the live step runtime
and new durable storage, which would have made S25 a "second source of truth" the sketch itself explicitly
refused to become. So it is a separate slice. **Promoted to full depth 2026-10-28**: `design.md`, `plan.md`
and nine task files now exist. The brief's "three tiers (loop/project/app)" turned out to be **pre-S38
vocabulary** — S38 renamed "project" to `component` and added a real `project` (a group of components)
above it, so the actual rollup chain is **loop → component → project → workspace**, one tier deeper than
the brief locked; the project/component/workspace tiers are updated incrementally so they survive a loop's
archival, and — new since the brief — a component moving between projects (already a real, built
capability, `UpdateComponent`) **transfers** its current rollup totals between the two projects' rows
rather than triggering a recompute that archived history could not support. Time (wall-clock duration) is
rolled up as its own first-class metric at every tier, exactly like cost and tokens — not derived from
timestamps after the fact — because a future agent sizing work needs a real number there too, not just a
person reading a dashboard. And — because the name invites the wrong reading — an explicit first section
disambiguating it from `S23`'s permanent, asserted-by-network-trace "no telemetry, ever": this slice's data
never leaves the local store, and the name survives unchanged because `S31`'s own app header already
reserves a disabled `Telemetry` tab waiting for exactly this.

**S34 Trust bench is new the same day, and it is P34's first child.** When the user directed the doctrine —
*the daemon and its APIs are the main character, the CLI is secondary* — the MCP/agents area was exactly
where the inversion still held: who an agent is, what it may do, and whether its MCP servers actually answer
were facts reachable **only** from a terminal. So the slice inverts the usual order and its task sequence is
the doctrine: `agent.list`, `tools.grants`, `mcp.status` and `mcp.check` land as daemon methods first (T1–T4,
each a conformance fixture), and the two bench pages — an agents *readiness* view and an MCP
config-and-**Check** view — are built strictly as thin clients of them (T5–T7). The one genuinely new daemon
behavior, the operator-clicked probe (`initialize` + `tools/list`, single-flight, timeout-bounded, cached
with its timestamp, and structurally incapable of `tools/call`), reuses the exact dial path
`aeon mcp list --agent` already proved. Full depth on day one: `brief.md`, `design.md` (TB1–TB12) and
`plan.md`'s eight tasks, with a deletion property — each section is one menu row, one page file, one
dispatch branch — so sections that outlive their purpose delete in one commit.

**S35 Trust desk is its same-day sibling, and it is where P34 meets the oldest guard in the repo.** P34
says every capability gets a daemon door; **PL15/R4/R6** say policy authoring never touches the socket —
enforced by a test, not prose (`TestNoPolicyAuthoringReachesTheNarrowSurface`). The slice builds the peace
P34 itself pre-named: **inert proposals on the socket, commit through a human-only door.** Agent
enable/disable/revoke become plain socket writes (riding `TransitionPluginAgent` verbatim, so reasonless
revoke stays un-writable through the new door); everything that widens authority — policy add/remove/seed,
grants (*grants are policy*, S12), MCP server add/remove (none has an add path anywhere today) — becomes a
row in one new `proposal` table that the guard's read paths must never read: the invariant the whole review
hangs on. The only door that promotes a row to effect is `aeon proposal commit` at a terminal, and **no
commit button exists in the bench in any form** — grep-pinned, the app's write-story ends at
`proposal.create` with a toast quoting the command for the human. The grep test is amended with the one
distinction it could not express (inert proposal rows are not the corpus), original assertions
byte-preserved. Nine tasks, **strictly sequential** per the operator's 2026-10-26 no-parallel direction:
one new table, one new config writer, **zero new validation logic** — create and commit run the same
validators the CLI verbs already run. It seeds after S34's daemon tier, because its own bench tier edits
S34's pages.

**S36 MCP dispatch is the trio's load-bearing member: S34 sees it · S35 authorizes it · S36 runs it.**
The slice where an MCP tool a human added, checked and granted finally *executes inside a loop step* —
under the **double gate** by ruling: the agent must *see* the server (S12's tier map) **and** a
`ToolAccess` allow row must name the exact qualified `server.tool`; **no row, never runs**, and `ask` (by
row or by S12's own L22 effect rule) parks on the existing `pendingPermission` path. A dead server
**degrades, never hangs, never promises**: build-time failures withhold with `grants.go`'s own verbatim
sentences, dispatch failures land as error-text `tool_result`s, and the loop must never freeze because a
child process died. The architecture turned on one discovery — `absent_test.go`'s banned list *names
`internal/daemon`* — which killed the adapter-in-daemon sketch: instead the MCP caller enters as a
**function value in `daemon.Options`, implemented in `cmd/aeon`** (the sanctioned importer), because a
function value carries no import; MC11's guard test survives with **zero diff lines**, the offer *calls*
`GrantView` rather than copying its verdict logic, and the whole slice deletes in one commit — three
files and one wiring line. Six tasks, **strictly sequential**; zero socket methods, zero pages, and free
to start — every dependency is built code. It also caught, and amended, S34's design §5 importing the
layer directly: one seam pattern, both slices, MC11's wall unmoved.

**S37 Agent admission is the trio's fourth member, closing the one gap S35 named and left open on
purpose.** S35's own brief says it plainly: *"the imported agent already passed blocklist + consent at
import (S15); enabling it starts something already permitted"* — which treats import as a precondition,
never one more knob. It is the one widening action in S15's whole domain, so S37 gives it exactly S35's
own treatment: one new `kind` (`agent_import`) in S35's existing `proposal` enum, not a new mechanism. The
payload is a bare `pkgPath` — the smallest of any kind — because the daemon re-reads the package itself at
both `proposal.create` (a preview, refusing early on the real validator's reason) and `aeon proposal
commit` (the actual import, through `ImportChecked`, the unedited function `aeon agent import` already
calls). The one new risk this kind has that S35's other four do not: the filesystem can move between
propose and commit, so a commit that now refuses sets `applied_error` and stays `proposed` rather than
flipping on a stale preview — named and tested as its own case, not assumed covered by S35's crash-retry
story. Zero new socket methods, zero new bench pages — the desk and the Agents-page toggle are already
generic over `kind`; one new exported function, a rename of an existing one
(`readPackage`→`ReadPackagePreview`). Seven tasks T1–T7 + T0 investigation, sequential; **built** on branch
`s37-agent-admission` and merging to main; seeds after S35's T1–T5.

**S28 Memory was promoted to full depth too, the same day.** It originally split into two halves with very
different build-readiness: the learnings core (classified, tagged, embedded, provenance-tracked — six of
its eight tasks) needed only S4, built from the start; the message-board half needed S14, which was not
built, and its one task (T7) was written but explicitly marked not to start. **S14 landed 2026-10-26**, and
T6/T7 were re-checked against its real, built API (`agentsvc.Invoker.Invoke`, `ChainDepthBound = 4`) rather
than left as written against a brief — finding one real gap along the way: none of S13's 25 handler bodies
has a signed A2A card, so `Invoke` refuses before reading any skill declaration at all; T6's scope grew to
issue real, self-signed cards, not just frontmatter. **All 8 tasks are buildable now.** Two findings from
the original design pass: `design.md`
corrected the brief's own `internal/memory` package name — that name already belongs to S20's real, built,
unrelated per-loop notes mechanism (`AppendMemory`/`ReadMemory`), so the actual code is `internal/learnings`
throughout — and the brief's "agent_id is a foreign key into S13's registry" does not work as written, since
S13's 25 built-in bodies have no database row to reference (a Go-level check instead). A board answer files
as a **project**-scoped learning, not agent-scoped, following the brief's own words — *"the next time any
agent needs the same answer"* — more literally than an agent-scoped default would have.

**S29 Triage was promoted to full depth too, the same day as S28.** Q2 turned out to already be answered by
D2 (validation and priority were always two separate closed sets, never a crossed enum — the question's own
phrasing half-assumed otherwise). Priority's exact shape — `priority_state`/`priority_label` — is copied
verbatim from `S26-roadmap`'s own columns, not re-derived, so the D8 hand-off never needs translation. That
hand-off targets **S26 only**, through S26's ordinary `idea` entry point (a validated bug's report becomes
the starting context for a fast, transcription-only ideation step) — never S24 directly, and requiring
**zero changes to S26**, since `S26-roadmap/design.md`'s own `R4` already routes a `ready` item to S24's
queue when one exists. A validation attempt dispatches automatically on filing, unlike S24's queue — G14's
existing unattended-denies-outward rule already bounds what an attempt could do, so there is nothing a gate
would protect against. Five of its nine tasks (T1–T4, T7–T8) are buildable now; T5 needs `S28-memory`'s
learnings core as real code, T6 needs `S26-roadmap`'s core (which in turn needs S24/S25 built).

**S26 was promoted the same day as S25**, the third item in that sitting. The sketch's own worry — *"a full
brief for a roadmap renderer that has never run would be fiction"* — turned out not to apply: this slice's
one genuinely new mechanism (ideation producing a brief) is not a roadmap renderer at all, and its forward
Gantt reuses S25's component outright rather than inventing rendering logic to be fictional about. **One
correction found while specing it**: the sketch assumed ideation was a `conversational-map`-shaped
blueprint; `S8-blueprints/design.md`'s own GAP-7 had already found that shape's iterative phases are not a
blueprint's shape at all — "a chat with tools." The slice now has `design.md`, `plan.md` and seven task
files. **S27 is now specced too, deliberately to brief depth only** — asked explicitly, since its own sketch
called itself *"the largest change to the daemon's model in the whole plan"* and this slice's `design.md`
would need S16, S24 and S26 to exist as real code, not just designs, to verify against. The brief resolves
what it can against the now-real S24/S25/S26 (S26's own `D4` "ready" definition, used verbatim; S16's D8 on
effect classes, quoted) and names the rest plainly: a real loop-level stop/cancel does not exist yet (only
`step.stop`), and S26's `roadmap_item` will need an `attended` column added — not added now, since S26 does
not need it without a scheduler, but recorded as the first thing S27's own T1 would do.

| # | Slice | Status |
| --- | --- | --- |
| **S24** | **Queue** — an ordered, self-sustaining backlog of loops per project, with a human gate the project can turn on or off, and loops sized manually or by a skill | `brief.md`, `design.md`, `plan.md`, 9 tasks exist |
| **S25** | **History** — the cross-loop, time-ordered projection of what actually happened, past and archived, through one merged sequence | `brief.md`, `design.md`, `plan.md`, 6 tasks exist |
| **S26** | **Roadmap** — ideation to brief (an ordinary loop, not a new blueprint shape), the forward Gantt (S25's component, reused), honest-unknown priority and ranged estimates | `brief.md`, `design.md`, `plan.md`, 7 tasks exist |
| **S27** | **Continuous work** — work *selection*, generalizing S24's advance-on-done past one project, picked up by a scheduler instead of a person | `brief.md` exists (brief depth only, by choice — see the brief's own §1) |
| **S28** | **Memory** — a classified, queryable learnings store (person/project/agent, all cross-project except project memory itself, code is `internal/learnings`), plus a directed agent-to-agent question whose answer files as a project-scoped learning | `brief.md`, `design.md`, `plan.md`, 8 tasks exist — **all 8 buildable now** (S14 built 2026-10-26; T6's scope grew to cover issuing the 25 bodies' missing A2A cards, T7 cites S14's real API) |
| **S29** | **Triage** — is a bug real, is it a duplicate, how urgent — validated by an automatically-dispatched loop, scheduled through S26 (never S24 directly), never by a second dispatcher | `brief.md`, `design.md`, `plan.md`, 9 tasks exist |
| **S30** | **Code Graph** — a structural map of the codebase (`calls`/`imports`/`inherits`/`mixes_in`/`uses`/`references`), queryable by an agent, deliberately separate from S22's content retrieval | `brief.md`, `design.md`, `plan.md`, 8 tasks exist |
| **S31** | **Loop workspace** — the backend and shell for the chat-first loop screen: loop steps that stream live, steering a running agent (queued at the turn boundary, or stop-and-inject), provider/effort changes mid-run, agent identity on events, read-only git status, file diffs, and a daemon-owned terminal | **Built** — `brief.md`, 11 tasks (T1–T9 backend/shell, closed 2026-10-24; T10 the UI, T11 the F-22/F-26 fix spec, both added 2026-10-26 to give already-built or still-open work a home in this slice) |
| **S32** | **Parallel agents** — P33: a loop runs up to `component.maxParallelAgents` steps at once (default 1; corrected from the sketch's pre-S38 "project" naming). Two parallel shapes, one mechanism: an `iterate` lane's items by the existing `task.depends_on` graph, or sibling lanes sharing a declared `parallel_group`; both run in their own throwaway git worktree, fanned out as `go func()`s coordinated by a channel the loop's own goroutine already owns | `brief.md`, `design.md`, `plan.md`, 9 tasks exist |
| **S33** | **Telemetry** — per-turn token composition, timing, file and command activity, kept at four durable tiers (loop → component → project → workspace, corrected from the brief's pre-S38 naming); the component/project/workspace tiers survive a loop's archival and a component's move between projects (transfer, not recompute); time is a first-class, agent-queryable metric alongside cost. **Not** `S23`'s forbidden external telemetry — see the brief's §0 | **built 2026-10-30, T1–T9 ✅** (four `telemetry.*` reads + the `loop.archive` door, reads-only surface, three-state cost floored at every tier, D2's two numbers distinct on the wire; real-`RunLoop` rollup-consistency + socket-walked D8 archive tests; ≈605 µs/row capture measured — no-batching confirmed; the tiers, move and archive walked live, token detail fixture-lane: `DEMO.md` §S33 and `S33-telemetry/tasks/T9.md`) |
| **S34** | **Trust bench** — P34's first slice: daemon methods `agent.list` / `tools.grants` / `mcp.status` / `mcp.check` make agents and MCP observable from the socket; the bench renders them as two new sections (agents readiness view; MCP config + operator-clicked Check, never `tools/call`) | **built 2026-10-29, T1–T8 ✅** (measured 3093 Go PASS / 2 FAIL — both foreign, neither reproducible alone; ui 602/10-foreign; no `DEMO.md` — acceptance is unit/integration-test-only by design; the P34 grep and a real deletion drill's findings are in `S34-trust-bench/tasks/T8.md`) |
| **S35** | **Trust desk** — S34's sequel, P34 × PL15 reconciled: agent enable/disable/revoke as plain socket writes; policy/grants/MCP-server changes as inert `proposal` rows no guard path may read; promotion only via `aeon proposal commit` at a terminal — no commit button in the app, grep-pinned; the bench gains the desk and its Trust group | **built 2026-10-29, T1–T9 ✅** (table-driven slice test over all 5 proposal kinds proves TD2's guard-blind invariant and the commit-flips-exactly-one-verdict claim; PL15/validator-parity/store-teeth re-confirmed; deletion drill run as file surgery — see `S35-trust-desk/tasks/T9.md`) |
| **S36** | **MCP dispatch** — the trust trio's load-bearing member: granted MCP tools execute inside loop steps under the double gate (tier-visible **and** `ToolAccess` allow on the exact `server.tool`; no row, never runs; `ask` parks on S11's path); dead servers degrade, never hang; MC11 stays byte-unamended via the `cmd/aeon`-injected function seam | **built 2026-10-30, T1–T6 ✅** (E2E: a real loop step offers `server.tool` with the live schema, calls it, records the `tool_result`; `ask` parks on S11's path / denies at once; name spaces don't cross-canonicalize; `loop.go` unchanged by diff; MC11 0/4 and `absent_test.go` zero-diff; 2149 tests green — see `S36-mcp-dispatch/tasks/T6.md`) |
| **S37** | **Agent admission** — the trust trio's fourth member: closes the one gap S35 named and left open, `aeon agent import` as a staged proposal (one new `kind` in S35's enum, not a new mechanism); payload is a bare `pkgPath`, re-read at both propose and commit; a commit that now refuses (the filesystem moved) sets `applied_error` and stays `proposed`, never flips on a stale preview | `brief.md`, `design.md`, `plan.md` exist; **built** (T1–T7) on branch `s37-agent-admission`, merged to main |

**Queue comes first of all**, because it needs nothing that does not already exist — no cross-loop projection, no
scheduler. **History comes first of the rest**, which reverses how it was raised, and for a reason: it is useful
**alone** — *"what has this thing actually done for me"* — and it is what makes the roadmap adoptable at any point.
That requirement shapes all of them: **a project can start feature-by-feature and gain a roadmap in month six, or
drop back.** So the roadmap **cannot be the source of truth for what happened** — it is a view over loops (and,
where one exists, a queue) that already exist. If it owned the record, adopting it later would mean backfilling six
months of intent, which nobody does; they start fresh and lose the history.

**S28 Memory — now fully planned; see [`S28-memory/brief.md`](S28-memory/brief.md) and its `design.md`.** The scope model below is
**superseded by that brief: three scopes, not two.** Person memory is cross-project and lives at the store
level, which S4 already made per-user. Project memory is per root. **Agent memory is a third scope, also
cross-project** — a learning scoped to an agent follows it into every project it runs in next, which is the
whole point of an agent getting better rather than starting over each time. **"Language memory" still
collapses into the other two**: *"Go wraps errors with `%w`"* is not learned — it is in the docs and already
in `golang-engineer`'s body. What is learned is *"this team wraps errors with a custom type"* (project),
*"this user wants table tests"* (person), and *"this agent keeps forgetting to check X"* (agent).

> **The failure mode nothing else in the plan has: a retrieved chunk cannot be wrong; a learned belief can be, and it
> persists invisibly.** Hence provenance and strength — `told` / `observed` (with N) / `assumed`, the middle being the
> majority. A memory system without that cannot be corrected, because nobody can find what to correct.

**S29 Triage — now fully planned; see [`S29-triage/brief.md`](S29-triage/brief.md) and its `design.md`.** *Fixing* a bug is S17's
ladder with a different entry point and mostly exists. *Validating* one is mostly S5's one-step loop with a
different prompt. **Triage — is this real, is it a duplicate, what priority — is what was genuinely missing,
and the brief adds a sixth point to the five below: a fourth, pre-attempt `unvalidated` state, distinct from
`could-not-tell`, so "nobody has looked yet" cannot collapse into "somebody looked and couldn't tell."**
Scheduling the fix, once a bug is validated and prioritized, is handed to S24's queue or S26's roadmap —
this slice never runs or schedules a fix loop itself, and its duplicate-detection index is its own,
project-scoped table, not a corpus borrowed from S22 or S28.

1. **A bug is a claim, and validation is three-way**: `reproduced` · `not-reproduced` · **`could-not-tell`**. The
   third is the majority in practice, and it is the whole reason this is hard. *(P31's seventh place —
   `ARCHITECTURE.md`'s table now counts eight in all, the eighth being policy acknowledgement, §3.7.)*
2. **Priority and validation are separate fields** — the most important of the five. **An unreproduced bug is not low
   priority; it is *unknown* priority**, and collapsing those is precisely how real bugs get closed. A constraint on
   S26, which must render an item whose priority is honestly unknown — and on S27, which must not treat unknown as
   low and silently never run it.
3. **A validation attempt is a first-class durable record** — its own outcome, cost and transcript, the shape a step
   has. **This is the part nobody else does**: everywhere else validation is a comment thread, and making it a record
   is what lets a second attempt know what the first one tried.
4. **Evidence is a loop.** Reproduction means something ran. Shape: `bug → attempts → loops`.
5. **Duplicate detection suggests; a human confirms.** Same trap as agent dispatch — **a bug auto-closed as a
   duplicate by cosine distance is a real bug lost, invisibly.**

**S30 Code Graph — new this session, not a promotion of an existing sketch; see
[`S30-code-graph/brief.md`](S30-code-graph/brief.md).** An agent exploring a complex path through the code
today has one option — read files, or `grep` — and pays context proportional to file size, not to
relevance. This slice parses the project with `tree-sitter` into plain, relational `graph_node`/`graph_edge`
rows (no `sqlite-vec`, no embedding index — this slice's question is structural, not similarity) and exposes
them through a tool that returns raw references, never prose. **Structure and content stay two different
questions**: the graph answers "what is connected to this," cheaply; S22's `search` answers "what does it
say," on just the handful of nodes the graph pointed at. The two compose at use-time only — no shared
schema, no shared call. Its one real dependency is shared infrastructure rather than another slice: the
write-signal hook ("the loop already knows it wrote files") that S22's own brief already names and leaves
open as its Q7 — whichever slice resolves it first, the other subscribes to the identical signal rather than
building a second one. **Not an instance of S20's trigger mechanism**, which schedules loop starts and has
no concept of a file changing mid-loop; that reuse was considered while writing this brief and explicitly
rejected.

**Agents as database entries** — icons, personality, embeddings, learnings — is **absorbed into S13–S15** rather than
deferred. Three constraints survive the move: the **markdown stays the reviewable artefact** (an agent that exists
only in a database is one nobody can review in a pull request); **learnings are retrieved, never concatenated** into
a system prompt, *which is exactly the context injection this project exists to remove, arriving by a route nobody
was watching*; and **embeddings serve discovery, not dispatch** (A13).

---

## 3. Policy — the governance interface

### 3.1 The one rule

> ## Allow is authored by an operator. Deny may be authored by anyone in the chain. Deny wins.

This is what makes **plugins** and **third-party MCP servers** safe without trusting them. An imported agent may
carry policy; a third-party server may carry policy; **neither can widen anything.** The dangerous direction is
closed at the type level, not by review.

It also collapses two mechanisms into one: there is no separate "grant" and "blocklist" — there is a policy, default
deny, and the authorship rule.

### 3.2 The shape

```go
type Policy struct {
    ID       string
    Type     PolicyType      // closed set — §3.3
    Target   Target          // AllAgents | Agent | Loop | LaneType | MCPServer
    Action   Action          // Allow | Deny
    Spec     json.RawMessage // schema per Type
    Source   Source          // Operator | Agent | MCPServer | Import
    NotAfter *time.Time      // nil means no lifetime
}
```

`Spec` is **data, not a language** — the line S8's L5 draws for blueprint conditions, arriving a second time.
This is why we **reject Rego and Cedar**: both are languages, and a language in the security path is a runtime we
would have to sandbox.

### 3.3 Seven types, and the set is closed

| Type | Spec | Absorbs |
| --- | --- | --- |
| `ToolAccess` | `{toolName, state}` | the blocklist **and** tool grants — one thing now |
| `CommandAccess` | ordered `{pattern, verdict}` | **S11's 366-rule corpus — already extracted and measured** |
| `RouteAccess` | `{route, state}` | *"never send code to a non-local route"* |
| `EffectClass` | `{action, class}` | per-deployment override of an action's class |
| `CostCap` | `{scope, limitUSD}` | round caps, per-loop and per-agent budgets |
| `SkillAccess` | `{agentID, skillID, state}` | A2A's per-skill `security` |
| `TriggerAccess` | `{kind, state}` | *"no webhooks here"*, *"no unattended schedules"* — §4 |

**`CommandAccess` is why this fits in day one.** The policy interface is not new work bolted on; it is the
generalisation of an evaluator S11 was already building, over a corpus already extracted (G19). We are **promoting**
a concept, not adding one.

### 3.4 Precedence — deny wins, and specificity does not rescue an allow

```
effective = (union of allows) − (union of denies)
```

**Not "most specific wins."** That would let an agent-level allow defeat a system deny — the *union-of-permissions*
escalation the delegation research describes. Two consequences: **a policy can only ever narrow**, and **default is
deny**.

### 3.5 Lifetimes keep the evaluator pure

```go
func Evaluate(ps []Policy, req Request, at time.Time) Verdict   // pure, total — G1 unchanged
```

The clock is a **parameter, never a call inside**. Expiry is testable at any instant with no clock injection and no
sleeping tests — **better** than the no-lifetime version, where the boundary was untestable because there was none.

### 3.6 Enforcement and information are two mechanisms — and the disagreement is logged

> **The runtime enforces. The agent is informed so it can behave well — never so it can be trusted to.**

Both, always. The agent learns the policies in force through the step's **`context_manifest`** — a column S4's schema
**already has** — so *"which policies did this agent see when it acted"* is recoverable per step, and S7's replay
reproduces the original step's policy set rather than today's.

**When they disagree, the runtime wins and the attempt is recorded.** That log is not only an audit trail — it is
**prompt-quality telemetry.** A rising disagreement rate means the policy text is not landing in the model's context,
which is a failure you would otherwise never see, because the system keeps working. It is the cheapest available
signal that an instruction has stopped being read.

### 3.7 The record

| Table | Answers |
| --- | --- |
| `policy_acceptance` | *"which policies is this agent under, and since when?"* |
| `policy_event` | *"every refusal, with its subject"* — and every disagreement (§3.6) |

**Acceptance is a quality signal, not a security gate.** An unaccepted policy is **still enforced**; the agent merely
behaves worse. If acceptance gated enforcement, a plugin could evade policy by declining to accept it.

`policy_event` is **capped by age**, with the cap and the current depth reported by `doctor` — decided now rather
than discovered at 2 GB.

**P31's eighth instance:** *"is this policy in force?"* → **enforced and acknowledged** · **enforced but
unacknowledged** · **could not determine**. `doctor` reports the middle, because a fleet that never acknowledged its
policies is working but degraded, and that is invisible unless asked.

### 3.8 A refusal names its policy

```
refused: bash · ToolAccess "no-shell-in-review" · target lane-type:review · source operator · no expiry
```

Nearly free, and it is the difference between a user filing a bug and a user changing a policy.

### 3.9 What is *not* policy

**Policy governs what may be done; structure defines what exists.** Re-run classes are a property of what a tool
*is*. The nine tools existing. The canonical message format.

**And attendedness is not policy** — the sharp case. G14 pins it at loop start and forbids inferring it so a verdict
is reproducible. A policy pushed mid-run would break that pin. Worth writing down, because `EffectClass` makes
attendedness look like it belongs here.

---

## 4. Triggers — what causes work to start

**Most of this is already designed.** S19's action table already has `Kind = read | trigger`, and already decided
*"a webhook is a `trigger`, not a third surface"*. S11's design already anticipates *"a **scheduler** picking up a
ready roadmap item, and a **webhook**"* and already closes the inference path that would let either declare itself
attended.

**So S20 adds a clock to an existing model.** It does not invent triggers. That is why it is ~8 tasks and not ~20.

### 4.1 Two different things, deliberately separated

The existing sketch conflates them, and the user's ask needs only the first:

| | What it is | Needs |
| --- | --- | --- |
| **S20 Triggers** *(Stage 4)* | **A clock.** *"At 03:00, start loop X from blueprint Y"* | daemon, blueprint, the `trigger` kind |
| **S27 Continuous** *(Stage 5)* | **Work selection.** A worker picks up whatever roadmap item is *ready*, runs it, picks another | S20 **and** S26's roadmap, "ready", queueing, caps |

Separating them is what lets time-based triggers land in Stage 4 **without dragging the roadmap in**. A schedule
fires a **named blueprint**; it does not choose its own work.

### 4.2 A scheduled run is unattended by construction — and that is already guarded

Nobody is at the keyboard at 03:00. **G14 pins attendedness at loop start and forbids inferring it**, and S11
already ensures *"there is no inference path"* for exactly this case. So a cron-started loop is **unattended**, its
`outward` and `irreversible` actions are denied, and it **cannot open or merge a pull request**.

A 3am job that reaches such an action **does not silently proceed** — it suspends (S6), and someone finds a question
in the morning.

**This is why triggers are cheap: the hard part was solved two stages earlier, for other reasons.**

### 4.3 The four scheduling questions — the genuinely new work

| Question | Decision | Why decide now |
| --- | --- | --- |
| **Missed fires** — daemon down at 03:00 | Reconcile on startup: **at most one catch-up, never a burst.** A daily job missed for a week runs **once**. Skipped fires are **recorded, not dropped** | P31 again: *did this fire?* → yes · no · **we were down and cannot say.** The third state must be visible |
| **Overlap** — the previous run is still going | **Skip, and record the skip** | S5 is serial; a loop overrunning its period is a signal, not something to parallelise |
| **Timezone / DST** | Store an explicit **IANA timezone**. A wall-clock time that does not exist is **skipped**; one occurring twice fires **once** | Cheap now; otherwise a bug report nobody can reproduce more than twice a year |
| **Cost runaway** — an hourly paid loop | **`CostCap` policy**, already built in S10 | The policy work pays for itself the first time it is reused |

**And a working-hours window** — *"agents worked all weekend"* is a feature to some people and a surprise bill to
others. A schedule carries an optional window; absent one, it runs whenever it fires.

### 4.4 Webhooks — inherited, not new

- **Signature-verified (HMAC).** *A valid signature proves who sent it, not what they may cause.*
- The payload is **untrusted input** and carries W23's label into every prompt it reaches.
- **Off by default, explicit address, key required** (N15) — a webhook is a listening port.
- The endpoint is **another enumerated projection** on the narrow surface (P25). This is why S20 lands after S19.

### 4.5 Two different off switches, and they must not be confused

| | | Lives |
| --- | --- | --- |
| **`TriggerAccess` policy** | *Governance.* "No webhooks in this deployment", "no unattended schedules in production" | §3.3 — operator-authored |
| **Pause / disable a schedule** | *Operational control.* R6 | CLI and app — **never the narrow surface** |

R6 is why: a scheduler needs an off switch that is not "uninstall", and operational control on the narrow surface
would hand a caller the means to bypass the guards written to constrain it. Putting the governance half in config
instead of policy would reopen exactly what policy replaced.

## 5. Agents, MCP and A2A

### 5.1 The shape

```
                    ┌──── POLICY ───────────────────────────────┐
                    │  operator ALLOW   (the only widening      │
                    │  anyone   DENY     source)  narrowing only │
                    └───────────────┬───────────────────────────┘
                                    │ evaluated per request
TRIGGER (manual · schedule · webhook)│ injected into context_manifest
  └─▶ LOOP  (the organizer)         ▼
        └─ a lane invokes a SKILL ──▶ TASK  (A2A's unit of work)
                                       │
                             ┌─────────┴─────────┐
                       synchronous          asynchronous
                      (the step waits)   (the step SUSPENDS — S6)
                                       │
                                     AGENT  (a service)
                                       ├─ CARD: skills it publishes       ← optional
                                       ├─ TOOLS, three tiers              ← optional
                                       │   ├─ CORE    the nine
                                       │   ├─ PUBLIC  shared registry — context7
                                       │   └─ PRIVATE this agent only
                                       └─ may invoke another agent's skill ← optional
```

### 5.2 Everything below the agent is optional

**An agent with no tools, no card and no skills is markdown and a prompt** — exactly the 25 bodies S13 writes — and
the ladder runs on it unchanged. The agent layer is **additive**, like retrieval, and gets the same test: **the suite
passes with the agent layer switched off.**

That is the adoption story. A company with only MCP servers writes agents with no tools of their own, pointed at
public ones, and has a working system. A company that wants more builds private tools and published skills.
**Same system, two depths. Nobody buys the whole model to start.**

### 5.3 Three tiers

| Tier | Who | Example |
| --- | --- | --- |
| **Core** | any agent granted it | the nine |
| **Public** | any agent that declares it | context7 |
| **Private** | exactly one agent | a reddit server for the reddit agent |

**Public tools are granted freely**, because the **effect class carries the risk** — a read-only tool warrants no
consent screen; an `outward` or `irreversible` one still asks at use time. A deployment that disagrees writes
`ToolAccess` deny for `all-agents`: the public door closes with no code change.

**Private means invisible and uncallable to peers** — not merely unauthorized. That is what *dissolves* transitive
escalation rather than mitigating it: the union-of-permissions failure requires shared tools.

### 5.4 A capability can be a tool, an agent, or both

context7 as a public tool is a raw call; wrapped by an agent it publishes *"find the docs relevant to X and
summarise."* **A tool is a capability; an agent is a capability with judgement about how to use it.** Discovery (S22)
covers both surfaces; **dispatch stays closed** (A13).

### 5.5 Standards

| Concern | Standard | Verdict |
| --- | --- | --- |
| An agent's tools | **MCP** | Adopt — day one |
| Publishing capability | **A2A Agent Card**, signed | Adopt |
| A unit of delegated work | **A2A Task** | Adopt — maps onto our suspension model |
| Transport | **JSON-RPC 2.0** | Already ours (P25) |
| Per-skill authorization | A2A `securitySchemes` | Adopt, **expressed as `SkillAccess` policy** |
| A policy *language* | OPA/Rego, Cedar | **Reject** — §3.2 |
| A policy *shape* | IAM, K8s CRD | **Already what we have** |

**MCP vertical, A2A horizontal, policy orthogonal to both.**

---

## 6. Decisions changed by this roadmap

| ID | Was | Becomes |
| --- | --- | --- |
| **N17** | MCP client out of the alpha | **Reverses. Day one.** My error — true of the ladder, false of the product |
| **A1** | Nothing spawns; *"there is no tree"* | **Amended.** Still nothing spawns — delegation is a **graph**, so cost rolls up along a recorded chain. *"No tree"* was right about process, wrong about accounting |
| **P25** | Two transports, two surfaces | **One protocol, N projections.** Socket full; HTTP narrow; A2A and the webhook ingress enumerated the same way |
| **G2** | Ordered, first match wins | **Scoped.** Governs *inside* a spec; deny-wins governs *between* policies |
| **P31** | Six instances | **Eight** — triage's *could-not-tell*, and policy acknowledgement |
| **G14** | Attendedness pinned at loop start | **Unchanged, and load-bearing** — it is what makes scheduled runs safe (§4.1) |
| **B1** | Aeon is a **fork** of the scaffolding — forced, because `internal/` does not cross a module boundary | **Reverses.** The services Aeon needs are reachable through the framework's public API, so the fork solved a problem that did not exist. **The cost moves rather than vanishing**: an upgrade is now one command, so it happens often and can widen the daemon silently — which is why **B8 became an allow-list** and **B11 is new** |
| **NEW** | — | **Allow is operator-authored; deny may be authored by anyone; deny wins** |
| **P33** (2026-10-24) | One step of a loop at a time — S5 serial, AS4 *"no concurrency model"*, S14 N4 | **A loop may run up to `project.maxParallelAgents` steps concurrently, default 1.** The daemon dispatches; agents still never start agents (**A1 unchanged**), async Tasks stay suspensions (**AS4 unchanged**), tools inside a step stay serial (**K9 unchanged**), one running loop per project root (unchanged). S14's N4 *"no concurrency"* stays true **of S14** — delegation adds none; the concurrency P33 permits is the orchestrator's, built in S32 |

---

## 7. Amendments to existing slices

What the new slices change in the ones already specced. **Five landed on 2026-09-16**; the rest are open.

| Slice | Change | State |
| --- | --- | --- |
| **S4** store | `policy`, `policy_acceptance`, `policy_event` tables + indexes, with `CHECK`s and nullable `not_after`, landed in **T2** — *a table is free now and a migration on a live store later* | ✅ **landed** |
| **S5** runtime | Policies in force injected into `context_manifest`; the tool loop consults the evaluator | open · **medium — this is the enforcement point** |
| **S5** T4 | The three handoff strategies **inlined** from the v1 source, with the "LAST" reasoning | ✅ **landed** |
| **S6** suspension | An A2A Task in flight is a suspension reason | open · small |
| **S7** recovery | Replay reproduces the **policy set of the original step**, not today's | open · small, and important |
| **S8** blueprints | A step gains `skill:` and `params:`; a lane may target an agent's skill | open · medium |
| **S9** app | Policy editor; agent registry; consent and enable/disable; the delegation chain visible in a run | open · medium |
| **S11** permissions | **W21 rescoped** — the nine become the core nine, extended at runtime in S12, never here | ✅ **landed** |
| **S11** permissions | **W23 must extend to MCP tool *descriptions*, not only payloads** — N3 returns in full once extension is runtime | open · **the largest thing runtime extension costs us** |
| **S13** agents | **N17's reversal changes the `DEFERRED` treatment of MCP content.** 24 of 26 bodies reference MCP, and S12 now *precedes* S13 — so that content is no longer deferred-because-absent, it is a granted tool | open · **found while reversing N17; bigger than "the 25 bodies need no change" implied** |
| **S16** git/forge | Forge actions carry effect classes, overridable by `EffectClass` policy | open · small |
| **S18** providers | `RouteAccess` governs which routes an agent may use | open · small |
| **S19** narrow surface | **N17 reversed**; `99-deferred-mcp-client.md` deleted and its five references repointed | ✅ **landed** |
| **S19** narrow surface | Nothing about policy authoring appears on the HTTP projection — **and it must stay that way** | open · small |
| **S22** retrieval | Cards, skills **and public tools** indexed for discovery; **dispatch stays closed** (A13) | open · small |
| **S23** first run | `doctor` reports unclassified tools, unacknowledged and expired policies, and anything the blocklist suppresses | open · small |
| **S1 T5 · S9** | **The UI toolkit is `@cjlapao/ui-kit@^0.2.0`** — already v1's dependency, so S9's port brings components built on it. **Two gaps found against the published types:** `StepStatus` lacks `not-needed`/`skipped` (D13 needs both distinct) and there is no socket-liveness three-state (`ConnectionState` is for flow diagrams). S9 T4 settles both before writing the renderers | ✅ **specs amended 2026-10-14** |
| **Framework** | **Upgraded to `framework/v0.10.1`.** Nine new registerable services arrived and **the daemon did not widen by one line** — B8's allow-list and B11's fixture working on their first real upgrade. **Breaking: `cursor.signing_key` became mandatory and `aeon serve` stopped starting while `make ci` stayed green** — `internal/secrets` fixes it; the lesson is that an upgrade is not verified until something has run `serve` | ✅ **landed 2026-10-11** |
| **S19** narrow surface | **Q3 answered by v0.10.1: `framework/api` IS a registerable HTTP server, so this slice no longer builds one.** It is registered and `ForceOff`-pinned; S19 owns removing the pin plus the explicit address, the required key and the action table. **Q5 answered: `api_keys`/`role`/`claim` stay internal, so S19 supplies its own validator via `WithTokenValidator`** | ○ **open — scope reduced** |
| **S4** store | **REDIRECTED by the user: use the framework's database contracts and expand what is missing.** Proven that sqlite-vec works through the framework's GORM path (`gorm.io/driver/sqlite` imports `mattn/go-sqlite3`), that `(*gorm.DB).DB()` exposes the raw `*sql.DB`, and that `SQLiteConfig`/`PoolConfig` already cover path, file and pooling. **T1 no longer builds a connector.** Open: whether `PoolConfig`'s single pool satisfies DB15, and whether the framework's migrations subsume the ledger | ○ **open — scope reduced** |
| **Store defaults** | `internal/appenv` feeds Aeon's XDG state dir + `state.db` into `DATABASE_SQLITE_STORAGE_PATH`/`_FILE_NAME`, because the framework defaults the path to `"."` — the process's working directory, which for a daemon means two shells open two different databases | ✅ **landed 2026-10-11** |
| **S1** bootstrap | **S1 is built.** T1–T4, T6, T7 landed; **T5 partial** — the web layer is verified, `endpoint.rs`/`rpc.rs` complete a real round trip against the daemon, `main.rs` is uncompiled for want of Tauri's Linux packages. The demo walk found **6 discrepancies and 1 real bug** (`serve --print-services` started the daemon) | ✅ **landed 2026-10-14** |
| **S1** bootstrap | **The ground floor is built.** `aeon/` exists, builds, boots and is verified — framework pinned at `framework/v0.10.0`, the allow-list and its fixture, one binary with `serve`/`version`, the Makefile, parked CI, `AGENTS.md`, `UPSTREAM.md`. **T1 and T2 landed; T3 and T6 partial; T4, T5, T7 open** | ✅ **landed 2026-10-03** |
| **S1** T4 | **The cross-compile gap is measured: 1 of 6 targets** with a real cgo dependency (5 of 6 only while nothing needs C). The six-target job is parked non-blocking until this closes | ○ **open — the slice's real risk** |
| **S1** bootstrap | **B1 reversed** — Aeon *imports* `go-backend-scaffolding` as a pinned dependency instead of forking it. `aeon/` is created by hand; **T1 becomes a handshake** that verifies and pins, **T2 becomes an explicit allow-list** instead of a subtraction, and **new B11** requires a dependency upgrade to leave the registered set unchanged | ✅ **landed 2026-09-17** |
| **Docs** | `_superseded/` moved to `docs/_archive/aeon-v1-horizontal/`; 179 citations normalised, 39 link targets repaired | ✅ **landed** |
| **ARCHITECTURE** §1a, §8 | **P33** recorded: per-project `maxParallelAgents` (default 1) bounds concurrent steps inside one loop. Prefixes `LW` (S31) and `PA` (S32) reserved in §11 | ✅ **landed 2026-10-24** |
| **S4** store | `component` (not `project` — S38 renamed the table after this amendment was written) gains `max_parallel_agents INTEGER NOT NULL DEFAULT 1 CHECK (max_parallel_agents BETWEEN 1 AND 16)` — a new numbered migration, never an edit (§3 of AGENTS) | ○ **designed, S32 T1, 2026-10-28** — not yet built |
| **S7** recovery | A loop may own **several** running steps at a crash; recovery resumes each independently, plus a new reconciliation pass against `loop_worktree` (git may or may not have caught up with the ledger row at crash time) | ○ **designed, S32 T6, 2026-10-28** — not yet built |
| **S8** blueprints / `internal/loop` | `Position` becomes `Positions []Position` (decode-compatible with every existing stored row); `Lane` gains `Parallel`/`ParallelGroup`; validation rejects overlapping `produces` within a `parallel_group` at author time | ○ **designed, S32 T2/T4, 2026-10-28** — not yet built |
| **S9** app / S31 | Events carry `stepId`, `loopId`, `lane`, `agent` so the loop view can interleave and filter by agent — **S31 lands this now**, so S32 adds no event shape | open · S31 |
| **S13** agents | I1 *"nothing spawns"* and N7 stand unchanged. Add one line to `design.md`: P33's concurrency is daemon dispatch, not an agent capability, so it is not a reason to restore `task` | open · trivial |
| **S14** agent services | AS4 and N4 stand. Note that an async Task's suspension frees a parallel slot (it has no process), so a suspended step does not count against `maxParallelAgents` | open · trivial |
| **S16** git/forge | **Resolved without a code change**: `forge.merge` is already a remote API call, never a local `git merge` (confirmed reading `ForgeActions.MergePR`) — the only local collision was `git checkout`/file writes sharing one directory, solved by giving each concurrently-dispatched unit its own ephemeral `git worktree` (two new `GitActions` methods; S16's existing code needed no change, since each caller already gets its own `dir`) | ○ **designed, S32 T3/T5, 2026-10-28** — not yet built |
| **S17** full ladder | An iterating lane's items are the natural first parallel unit (per-item task state already exists, D11) — **and the readiness signal already exists too**: `task.depends_on`, already read by `escalate.go` for failure propagation, now also read for readiness | ○ **designed, S32 T5, 2026-10-28** — not yet built |
| **S20** triggers | *"S5 is serial; a loop overrunning its period is a signal, not something to parallelise"* stays true: P33 is **within** a loop, the overlap rule is **between** runs of a loop | unchanged — recorded so nobody re-reads P33 as permission |
| **P28** budgets | The per-loop memory budget covers all its concurrent steps together, not each step | open · small — not addressed by S32's design; still a real gap |

## 8. Prerequisites and risks

### 8.1 ✅ Resolved — the `S` namespace no longer collides

**Done on 2026-09-16.** `S4-store` used `S1`–`S20` as *its own decision IDs* while the corpus used `S<n>` for slices,
and both appeared in the same file — `design.md:62`'s `S16` meant *S4's decision 16* (the DSN pragma) while
`design.md:72`'s meant *slice 16, retrieval*. A `sed` would have silently corrupted the decision trail.

**The fix, in two phases:**

1. **Freed the `S` prefix.** `S4-store`'s 20 decisions became `DB1`–`DB20`. External references were the harder half:
   10 explicit `S4's S<n>` sites, **20 bare `(S19)` meaning *"discriminated results over errors"* spread across eight
   slices**, and 9 more in `docs/test_suites/`. Each was classified by reading it.
2. **Renumbered.** 2,616 substitutions across 200 files; 11 directories moved; 171 tasks intact.
   `docs/_archive/aeon-v1-horizontal/` was **excluded** — it has its own internal `S`-collisions and is frozen.

**Three stale references surfaced while classifying and were fixed rather than propagated:** `S10's rollups` (no
slice does cost rollups — S13 says there are none, *"because there is no tree"*), and two `(S10)` *"separate module"*
notes left over from the old horizontal plan.

**`S` now means "slice", corpus-wide.**

### 8.2 🟡 Open — the standards check

§5.5 is from training knowledge; the search hit a spend limit. **Agreed not to block on it.** If an agent-governance
standard has appeared since, aligning is cheap now and expensive once S10 is built. Recheck before S10 begins.

### 8.3 🟢 Settled

Renumber **yes** · disagreement log **yes** (§3.6) · remote agents **in the roadmap**, S21 · consent surface **both**,
app primary · `policy_event` **capped by age** · public tools **granted freely** · blocklist **empty by default**.

---

## 9. Parked — unrelated to this roadmap

Three diagnosed, unfixed issues in `packages/loop-state-mcp` (the v1 system still running this work):

- **`Learnings.md` re-emits whole on every append** — the actual cause of the size complaint. The 2 MB cap is on
  *output*, so reading to dedup is cheap and re-emitting is capped.
- **`renderMemory` runs on 20 call sites** though memory changes only via `memory_append`.
- **`loop-files.ts:438` silently skips** oversized files during backfill.
| **S38** | **Organization layers** — the records slice: `project`→`component` and a new `project` tier (rename proven on a store copy, NOT NULL inside 0035), then goal/epic/brief/review/attachment/task as tables and **one 33-action management catalog** projected as both socket methods and grantable agent tools (one definition, two doors; decides stay human doors), `emits:` as contract-never-grant, `requires:` as prerequisite, two kinds of task on one board (surrogate id; loop tasks read-only) — T0 closed every provisional OL rule by running things | `brief.md`, `design.md`, `plan.md`, `investigation.md` — **built 2026-10-27, T0–T17 ✅** (measured 2667 PASS / 0 FAIL; walk + F1–F8 findings + deletion-drill journal: `S38-org-layers/DEMO.md`) |
| **S39** | **Chat runs on the org layers** — the conversation loop the records were shaped behind; the composer stays S39's (OL6's `waiting`-park is the engine delta); `isPlainChat` **ships false until S39 exists** — T10 stub-tested the lane that flips it | **built 2026-10-28, T0–T13 ✅** (measured 2800 PASS / 0 FAIL, ui 519/6 all-pre-existing; walk + fifteen-row findings incl. the stepless gate, F-21 approve-delivers, unattended zero-question denial, and the three spec corrections — `S39-chat-runs/DEMO.md`. Note: no T ever asked for the `isPlainChat` flip — the seam still returns `false`, stub-tested lane only; recorded, not papered) |
| **S40** | **Automation on the org layers** — events fire carrying goal/task ids; edits the task/goal store functions S38 shipped | **built 2026-10-28, T0–T10 ✅** (measured **2912 PASS / 0 FAIL** official, ui 555/6-foreign; the seven-walk demo with a 14-row findings table is `S40-automation/DEMO.md` — zero-trigger daemon `2|0|0` with the cursor advancing, the deletion drill (`t10-deletion-drill`, never merged) strips the whole slice and the tree still builds with tests compiling, and the walk corrected the spec where it mattered: `started_by=event`, the untyped −32603 match refusal recorded for a follow-up, `cost-unknown` needing real cost data; `AU16` names the S41 seams) |
| **S41** | **Approval policy and spawn** — ApprovalPolicy/auto-approval (the `review.by_kind` widening), spawn/wait, config inheritance, project-scope loops; **S40 §AU16 lists the exact seams** | **one roadmap line only — not designed, not instructed** (the author's uncommitted `notify` surface in the working tree is S41-shaped and explicitly not this slice's record) |

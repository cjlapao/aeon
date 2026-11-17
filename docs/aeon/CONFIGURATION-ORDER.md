# Configuration order — turning the engines into a product

Written 2026-10-30. The Fleet stage (S24–S40) built the engines: policy, blueprints, MCP, automation,
roadmap, history, retrieval. Almost none of them have a real management surface — they're reachable from a
bench tab or a CLI verb, not something a person configures and makes their own. S43 (agent management,
built) is the first piece of turning that into a product. This file orders what comes
after it, and why that order, not just a list.

## Standing rules for every slice in this sequence (operator, 2026-10-30)

The task shapes that implement these rules (investigation task, characterize-then-move, the human-door guard amendment, the CLI client with offline fallback, agent tools, the descriptor-driven Settings panel, the seed-set declaration, the close-out) are written down once in [`MANAGEMENT-PATTERNS.md`](MANAGEMENT-PATTERNS.md); S50–S54 cite it by section.

1. **The daemon is first class; the CLI is second.** Anything the CLI can do that the daemon's socket cannot is a
   gap in the *daemon* and is closed by that slice. The CLI becomes a client of the same methods; where a verb must
   work with no daemon (first run, a broken daemon) it falls back to the **same shared code**, never a second copy.
2. **Every management operation gets an agent-visible tool**, projected from one definition with its socket twin (the
   S38 records catalog: "one definition, two doors"). Where an operation widens a guard the tool is **propose-only
   and restrictive-only**; commit/reject/remove/seed stay human-door. New tools are *registered, not granted*.
3. **Every management surface is a list with details** — what is configured, what it applies to, who changed it —
   not a bare CRUD form. The **catalog of tools themselves** (what the app exposes, to whom) is its own slice.
4. **Forms render from the daemon's own descriptors** (closed vocabularies, spec shapes, setting declarations) so the
   UI never types a vocabulary twice.
5. **A slice never edits another slice that is being built.** Adopting or refactoring another slice's code is a task
   inside the adopting slice, run after the other has merged (S44 T10 and S45 T10 are the two worked examples).
6. **Every record is a row — system ones included.** Agents, policies, MCP server definitions, blueprints, tool
   metadata, automation definitions and presets all live in the database, even when the product ships them from code.
   Code is the **seed source**, never the **source of truth**: a system record is put in the database by the **seed
   process** (S43-extra: declared, versioned sets with a ledger and an append-only audit), carries `origin='system'`, and
   at runtime the daemon reads **only the row**. What stays code is *shape*, not records: the closed policy vocabulary,
   S44's setting declarations, protocol methods, tool executors ("could a person want to list, read or copy it? then it
   is a record"). Two seed classes: **inert** sets (change nothing about what is permitted — agents, blueprints, tool
   metadata, MCP definitions shipped disabled) apply automatically at daemon start and by `aeon init`; **guard** sets
   (add `allow` rows, enable servers, widen grants — the policy corpus, MCP grants) are **never automatic** and are
   applied only by an explicit human-door action (D4/PL1 preserved). Every management slice **declares its own seed
   set** against the one engine; none invents another way to ship a default. An operator's change or removal of a
   system row is never silently undone by a re-seed (`removed`/`diverged`, explicit `restore`). **One named exception: authority records — triggers
   and schedules — are rows but are never seeded** (S40 AU1 lock 3 forbids any non-person author; see S49 AN2).

## Slice numbers (assigned 2026-10-30)

| Slice | What | Status |
| --- | --- | --- |
| **S43** | Agent management | **built and merged** (2026-10-30) — not edited from here; its gap (stub first-party rows, content still read from code) is closed by S43-extra |
| **S43-extra** | **Seed process** — the engine for rule 6, a complete `agents.firstparty` set, dispatch reading the row | planned ([`S43-extra-seed-process/`](S43-extra-seed-process/)) — **before S45** |
| **S44** | Config store (live, UI-writable settings) | **built 2026-10-30**; T10 adopts S43's setting |
| **S44-extra** | **Component tree** — `project` and `component` fold into one `component` record, two levels (a root and its children; a root with children *is* a project, derived); `project`/`implicit`/`project_telemetry` removed by lossless migrations; promote/attach/detach/archive/restore daemon-first + agent tools + `aeon component`; five settings inherit from the root | planned ([`S44-extra-component-tree/`](S44-extra-component-tree/)) — **next priority after S44**, before S43-extra and S45; the UI work starts on top of it |
| **S45** | Policy management — daemon authoring + commit (amends PL15/TD7), agent tools, CLI as client, Settings UI, agent picker; **T12 declares the `policy.corpus` guard seed set** | planned ([`S45-policy-management/`](S45-policy-management/)); T12 needs S43-extra T3 |
| S46 | MCP management (config **moved into the database**, per-agent grants by slug, proposals, an `mcp.servers` seed set) — reuses S45's `internal/desk` registry and `AgentPicker`. The MCP config fact now lives in the six `mcp_*` tables; the legacy `mcp.json` is adopted once and never read again (drift reported by hash only) | **built 2026-11-02** ([`S46-mcp-management/`](S46-mcp-management/)) |
| S47 | Tools catalog — read-only list with details of everything the app exposes (core, records, MCP-provided), who can call each and why not (checked against the gate), plus the daemon's own classified method surface; tool *catalog entries* are seeded records (`tools.core`), executors and schemas stay code; the daemon's ~270 methods are a classified table with door truth computed and proven | **built 2026-11-02** ([`S47-tools-catalog/`](S47-tools-catalog/)) |
| S48 | Blueprint management — **blueprints moved from state-dir files into the database** (verbatim YAML, append-only revisions, derived references), shipped ones seeded (`blueprints.standard`), daemon-first writes/usage/diff/lint, propose-only agent authoring (`blueprint_save` proposal kind). The blueprint fact now lives in the four `blueprint*` tables; the state-dir `blueprints/` directory is adopted once, never written and never read again (a stale mirror is reported by `doctor`, the database wins), and `blueprint export`/`edit` are the only file doors | **built 2026-11-03** ([`S48-blueprint-management/`](S48-blueprint-management/)) |
| S49 | Automation management — one view over schedules, webhooks and event triggers; the S40 human-only authority locks extended to schedules; audit, archive (never delete), health and previews; agents may only propose to *disable*; **triggers are authority records and are never seeded** | **built 2026-11-04** ([`S49-automation-management/`](S49-automation-management/)) |
| S50 | Roadmap management — edit, audit, board with honest states; the hidden ideation/plan blueprint choices become a seeded blueprint record and S44 settings; the roadmap plane is a **human door** (RM11: neither name ever reaches the narrow projection, the agent surface or the tool registry) and **dispatch is a person's act** (`--yes`/click on the daemon's own statement, RM9); agents keep only the `roadmap_idea` proposal, inert until committed | **built 2026-11-04** ([`S50-roadmap-management/`](S50-roadmap-management/)) |
| **S51** | Triage management — edit, audit, `filed_by` → slugs, validation blueprint as a setting; agents may only file an unvalidated claim | **built 2026-11-04** ([`S51-triage-management/`](S51-triage-management/)) |
| **S52** | Memory (learnings) management — agent scope → slugs, retract/restore (never delete), actor + audit, "what would this agent see?" | **built 2026-11-04** ([`S52-memory-management/`](S52-memory-management/)) |
| **S53** | History views — coverage honesty (shown / not shown / cannot say), one formatter, read-only tools, a real Projects → History screen | **built 2026-11-04** ([`S53-history-views/`](S53-history-views/)) |
| **S54** | Platform status — retrieval, code graph and usage as a status panel with previewed, audited operations (derived data: no seeds) | **built 2026-11-04** ([`S54-platform-status/`](S54-platform-status/)) |

## After the configuration slices: autonomy

S43–S54 make Aeon *manageable*. Making it able to run a project unattended is a separate sequence, planned 2026-10-30 in
[`AUTONOMY-GAPS.md`](AUTONOMY-GAPS.md): S55 loop control and budgets, S56 autonomy policy, S57 verification gates, S59 execution
environment, S60 merge queue, S27 continuous scheduler, S58 supervision, S61 goal intake, S62 backup and restore, S63 outcome
learning. They follow the same standing rules and cite [`MANAGEMENT-PATTERNS.md`](MANAGEMENT-PATTERNS.md). S62 and the S59 environment
scrub are small and independent and can be pulled forward.

**Status (2026-11-02):** **S55 is partial** — store layer (3 migrations + control-core + budget store) built, tested and merged to
main 2026-11-01 (SLICES 4/13); its daemon/service seams are what S56's conditions wait on. **S56 is built 2026-11-02 (12/12)** —
per-root autonomy grants with the merge grant separate and default-deny, pinned ceiling + live floor, the human door and the two
restrictive propose twins; because S55's daemon layer and S57 are still unwired, the `requireBudget`/`requireGates` conditions answer
cannot-say in production and a merge grant is inert until S57 lands (stated in `SLICES.md` and in the S56 brief's handoff, which names
S27's exact start call). **S62 is built 2026-11-03 (12/12)** — the ten `backup.*` settings are S44-declared (T10): env → row →
default, durations `TypeString`, the four keeps `Min 1`, none Sensitive, the interval floor and the dir-absoluteness kept in the
resolver, and the pre-migration hook parser-pinned on `NewEnvSettings` so it reads env and default only — never a row (T10's
Notes and the S62 `SLICES.md` row for the rulings and the guard). **S57 is built 2026-11-03 (13/13)** — the four migrations,
the gate plane as a human door (`verify.* · mergerule.* · dod.*` on the socket, absent from every non-human door), `verify.run`
as a code action whose pass/fail/unverified routes the loop, forge check-reading, the per-component merge-rule floor, the merge
record and the post-merge watch filing the triage bug. The gate plane landing is what un-inerts S56's merge grant: the rule floor
answers live facts from that day (`mergerule.Evaluate`, per the amended note in S16's design); `requireBudget`'s S55 daemon half
waits on that slice. **S58 is built 2026-11-04 (11/11)** — the supervision plane is pure human door:
the live inbox (`inbox.*`, evaluation only — nothing attention-shaped is stored, ack/snooze live in their own
append-only tables), the channel plane (`notify.*`: channels are authority records, a secret enters only through a
0600 file and leaves only as an HMAC key, the closed envelope cannot carry a URL path or a body, the SSRF guard
refuses metadata and link-local addresses even under `--allow-private`), and `status/doctor` honesty lines that
name every source that reports, failed, or was never wired. It ships **no new daemon settings** (quiet windows
and ceilings are per-subscription rows, not config) and widens `hint.v1` in place (`deepLink/rootSlug/loopSlug`).
The desktop shell (tray, close-to-tray, `aeon://`, OS notification when hidden) is written and pinned at the
TypeScript level but the Rust half is **unbuilt and unverified on the Linux dev box** — the standing hand-off S58
T7 and T10 record rather than fake. The nineteen supervision methods are guard-pinned out of the narrow surface,
the agent tool registry and the records catalog (S58 T9). **S60 is built 2026-11-04 (13/13)** — the per-repository
merge queue, landing **merge-forward**: one slot per base branch held by CAS, catch-up on `integrate/mq-<item>` refs
(nothing rebased, nothing force-pushed, no ref ever deleted), conflicts carried as **paths and counts only**, verification
bound to the integration sha and failing **closed**. The ten `mergequeue.*` methods are a **human door** (absent from the
narrow projection, the agent services, the tool registry and the records catalog; `mergequeue.proposeEnqueue` is the
agent's only door and its `mergequeue_enqueue` proposal kind is inert until a person commits), land answers only the
person who saw the row (`--yes` bound to the row's digest), and an unattended land **denies** without S56's merge grant —
the G14 floor re-pinned, not moved. The `mergequeue` settings (mode, conflict policy) are the queue's own rows with the
daemon's widening statement on every change, not new S44 config keys. Walked 2026-11-04:
`aeon/scripts/qa-s60-mergequeue.sh`, recorded at [DEMO §S60](DEMO.md). **S63 is partial-built 2026-11-05 (9/10)** —
the outcome plane is measurement, never authority: the recorder writes exactly one outcome per terminal event
from the daemon's own emissions, the milestone writer files `observed` learnings from one RT4 template, and the
scorecards, the compare and the learned digest are derived reads (`too-few` / `cannot say` / coverage-first, the
RT3 sentence quoted from the wire). Zero new settings and zero grants are derived from any score — RT3's law,
guarded by `TestScorecardNeverReachedByDispatch` with the scratch-import canary walked. `aeon retro` and
`aeon scorecard` are pure socket clients (the slice's one P5 deviation, recorded in T7's Notes). T8's model
retrospective stays unbuilt — blocked on the `retrospective` blueprint and `retrospective-author` seeds owned by
S48/S55/S44/S52 — and `retro.status` says `not enabled`, never a guess. Walked 2026-11-05:
`aeon/scripts/qa-s63-outcome.sh`, recorded at [DEMO §S63](DEMO.md).
**S61 is built 2026-11-05 (13/13)** — the goal-intake plane adds exactly **two settings keys, both default-off**:
`intake.allowRemoteCreate` and `intake.confirmPublic`, and creating a repository on a forge needs those **plus** a
workspace-scope autonomy grant — three independent conditions, each off alone, walked closed: a `--publish private`
commit walks past the door and stops mid-run at the gate, the refusal naming the key (DEMO §S61 step 10a). The
`intake.vocabulary` descriptor answers `remoteCreateEnabled` with the daemon's own reason per missing key, and the
wizard renders the daemon's publish STATEMENTS — no vocabulary, sentence or authority statement is ever typed in the
UI. Everything else the plane touches is a door, not a setting: fifteen human-door socket methods, two inert agent
proposes, person-door `repo.scaffold`/`forge.createRepo`. Walked 2026-11-05: `aeon/scripts/qa-s61-intake.sh`,
recorded at [DEMO §S61](DEMO.md).

## The organizing fact: slugs

S43 makes every agent a database row with an immutable `slug`, and migrates every existing by-name
reference to it — policy targets, blueprint lanes, MCP private-tier grants, task assignment, agent-chain
edges. That migration is the single most load-bearing piece of work in the whole sequence below: anything
built on top of it (a policy editor with an agent picker, a blueprint editor with an agent picker, an MCP
grants page) gets a stable identity to reference; anything built **before** it is built on sand that's about
to move. This is why agents go first and nothing else starts until S43 lands.

## Tier 0.5 — the config store, before anything with a UI-writable setting

**S44 — Config store** (built 2026-10-30; see SLICES.md). `internal/config`
(S23) is a five-layer, boot-time-merged system — read once at startup, immutable while the daemon runs,
nothing the app can write to live. Every tier below this one will eventually want at least one setting a
person flips in Settings without restarting the daemon (S43's own `agents.allowUnsignedImport` already
does). Build the mechanism once, here, rather than every later slice inventing its own ad hoc
database-backed flag. Depends on S43 for nothing structural, but sequenced right after it since S43 is the
first consumer.

## Tier 1 — the other reference target: policy (**built — S45, 2026-11-01**)

**Policy** (S10/S11 engine; **S45 built 2026-11-01: this entry's ask is delivered** — authoring and commit are
daemon doors (PL15 amended per PM1, the narrow/agent/tool/records surfaces still closed), the CLI is a client
of the socket, Settings → Policy exists with the shared slug-only `AgentPicker`, and the shipped corpus is the
`policy.corpus` guard seed set — all thirteen criteria re-run fresh with the socket/CLI demo walk recorded in
`DEMO.md` §S45 and the app-lane steps named not-run; `SLICES.md` S45 row 13/13). What follows is the planning
text as written 2026-10-30. The operator
named this as the second of the two "small pieces" to start with, and it's second for a structural reason
too: once S43 lands, policy is the other thing blueprints/MCP/triage will want to point at — "which agent,
under what rule" is one sentence, not two unrelated settings pages. Build a real settings surface for
authoring/editing policy rows (today CLI-only, PL15) with an agent-slug picker instead of a free-text name
field, and the per-target in-force/why views already built (S10) get folded into it rather than staying
bench-only.

**MCP management** (S12 client, S36 dispatch — both built; config/grants today are bench-tab only,
`McpPage.tsx`, and S35 already added staged `mcp_server_add/remove` proposals). Grouped with policy, not
after it, because MCP's private-tier grants are *also* keyed by agent name today (found while writing this
document — `internal/mcp/grants.go`, `config.go`, `registry.go` — not yet in S43's own file list; add it
before S43's T0 runs) and because a grants page is really "policy, scoped to one server" — the same
agent-picker and the same "what does this rule actually allow" sentence apply. Natural to build these two
settings surfaces side by side, reusing one agent-picker component.

## Tier 2 — the things that consume agents + policy + MCP

**Blueprints** (S8 engine, built; bench-tab editor exists but no Settings-grade authoring/duplication/
validation). A blueprint's lane names an agent (now a slug) and runs under policy; a good authoring UI
wants both pickers to already exist (Tier 1) rather than being built against raw strings first and
retrofitted. Also the natural home for a "validate this blueprint" pass (does every `Agent:` resolve, does
any lane's action collide with a `deny` policy) — exactly the kind of debugging surface the operator asked
for. — **S48 built 2026-11-03: this entry's ask is delivered, with one fact changed underneath it.**
Blueprints **moved into the database**: the YAML is stored verbatim in a `blueprint` row, every write appends
an immutable revision naming its actor, and `blueprint_ref` is derived from the row's content on each write,
so usage, delete-protection and agent-deletion all answer from one derived table instead of a text scan. The
state-dir directory is now a **disposable export** — adopted once at startup, never written, never read again;
`doctor` reports a stale mirror and the database wins. Settings → Blueprints is a daemon client over eleven
registered methods (details, editor, revisions with diff/revert/restore-shipped, usage, and the lint pass —
advisory and dated, quoting the policy door's sentence verbatim and never changing the validator's verdict),
shipped blueprints are an inert `blueprints.standard` seed (editable into `diverged`, never overwritten,
restored explicitly), and what an agent may do is read plus one `blueprint_save` proposal — the write doors
are human doors. A true authoring door (a blueprint that writes blueprints) remains unbuilt by design and
belongs to a later slice's authority discussion.

**Automation** (S40 engine, built; bench-tab only, `AutomationPage.tsx`). Triggers fire into blueprints and
often carry an agent/assignee — wants Tier 2's blueprint management to exist so "which blueprint does this
trigger kick off" is a real picker, not a typed slug.

## Tier 3 — lower urgency, already partly product-shaped

**Roadmap** (S26, built, and already the most advanced of this group — it has real bench CRUD, not just a
read view). Promoting it from bench to Settings is comparatively cheap whenever it's convenient; nothing
above blocks on it and it doesn't block anything above.

**History** (S25, built, bench read view). Lowest urgency — it's a read surface today and stays useful as
one; "management" here mostly means promoting the existing view out of the bench, not new CRUD.

**Memory** (S28, **built**; planned as S52 — correction 2026-10-30). A learning is scoped to an agent, so it wants S43's slugs, but it
doesn't depend on Tier 1/2's policy or blueprint work the way triage (below) does — safe to build any time
after S43, including in parallel with Tier 2.

## Tier 4 — new, and dependent on Tier 2/3 being real

**Triage** (S29, **built**; planned as S51 — correction 2026-10-30). Its own brief says scheduling a fix is S24/S26's job, not triage's own
— meaning triage *hands off* into roadmap and blueprints. Build it after Tier 2 (blueprints) and Tier 3
(roadmap) have real management surfaces, so a triaged issue hands off into something a person can actually
see and act on, not a bare row.

## Tier 5 — infrastructure, probably never wants a CRUD settings page

**Graph** (S30, **built**; planned with retrieval as S54 — correction 2026-10-30) and **embeddings/retrieval** (S22, built — the index, not a management surface).
Both are closer to a diagnostics/status panel than something a person configures: a graph rebuilds itself
from the repo, a retrieval corpus is populated by the pipeline, not hand-edited row by row. Worth a
"rebuild / last-indexed / coverage" status page eventually, but don't force them into the same
list/create/edit/delete shape the tiers above use — that shape doesn't fit what they are.

## The order

1. **S43 — Agents** (planned, not built). Everything below waits on its slug migration.
2. **S44 — Config store** (**built 2026-10-30**; see SLICES.md). Build once, before any tier below needs its own ad hoc setting.
2a. **S44-extra — Component tree** (operator, 2026-10-30: "our next priority … then we move to S45"). Every later
   slice scopes to components; it must speak the one-record, two-level model before any of them is built, and the app
   UI work starts on top of it. Then **S43-extra** (seed engine; disjoint code, may run in parallel), then S45.
3. **Policy settings** + **MCP grants settings** (one pass, shared agent-picker) — the other reference
   target, and the thing most directly unblocked by S43.
4. **Blueprint management**, then **Automation management** — the consumers, in that order since
   automation points at blueprints.
5. **Roadmap settings polish**, **History settings polish**, **Memory** — lower-urgency or independent;
   order among these three by whichever the operator wants to use soonest, not by a structural dependency.
6. **Triage** — after blueprints and roadmap have real management surfaces to hand off into.
7. **Graph** and **embeddings/retrieval status pages** — last, and possibly never as a full CRUD slice;
   revisit the framing (diagnostics, not management) when this tier comes up.

Each of 2–5 should get its own `brief.md`/`design.md`/`plan.md`/`tasks/` the way S43 did — this file orders
them, it doesn't replace planning each one.

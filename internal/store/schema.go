package store

import "embed"

// migrationsDirFS embeds migrations/ whole (not just `*.sql`) so the
// directory's own README.md keeps go:embed satisfied even when it is the
// only file present — parseTimestampedMigrations does its own `*.sql`
// filtering when walking it.
//
//go:embed migrations
var migrationsDirFS embed.FS

// The schema: every table, every index, every CHECK, in the order they are
// applied.
//
// # One DDL source, and what D4 therefore compares
//
// The design called for two paths to the current shape — `schema.go` creating
// it directly and `migrate.go` walking an old one forward — with a test
// proving them identical (I3). **That is one copy too many, and it cannot
// catch the bug it is for.** Two hand-maintained copies, both edited in the
// same commit, agree with each other and still break an upgraded install: the
// drift that matters is between a database built by TODAY's `CREATE TABLE` and
// one built by an older `CREATE` plus every `ALTER` since.
//
// So there is one source — the ordered set below — and D4's test compares a
// FRESH database against an UPGRADED one: a frozen prior-version DDL held in
// the test, its ledger seeded, walked forward by the runner. That fails
// exactly when somebody adds a column to an existing migration's `CREATE
// TABLE` instead of adding a new one, which is the real version of this
// mistake and the one two copies cannot see.
//
// Recorded as a deviation in T2's notes rather than silently taken.
//
// # Forward-only, and why that is a rule rather than a convention
//
// A migration that has shipped is immutable. Editing `0002` changes the shape
// a fresh database reaches and leaves every existing database at the old one,
// with a ledger row saying it is up to date — which is undetectable from
// inside the process. New shape, new number.

// Migration is one numbered, forward-only step (DB8).
//
// ID is the ordering key and the ledger identity. Name is what the ledger
// stores and what `aeon db info` prints; it carries the number so a person
// reading the ledger sees the order without sorting it.
type Migration struct {
	ID          int
	Name        string
	Description string
	SQL         string
}

// Migrations is the schema, in application order: the frozen legacy block
// (1–43, below) followed by whatever timestamp-named files live under
// migrations/ — see migrations/README.md for why new migrations go there
// instead of growing this slice literal.
func Migrations() []Migration {
	legacy := legacyMigrations()
	extra, err := parseTimestampedMigrations(migrationsDirFS, "migrations")
	if err != nil {
		// migrationsDirFS is compiled in (go:embed) — a bad filename here is
		// a mistake in THIS repository, caught by every test and build, never
		// a runtime condition a caller can hit.
		panic(err)
	}
	return append(legacy, extra...)
}

// legacyMigrations is the frozen block: migrations 1–43, numbered and
// hand-ordered, shipped before the migrations/ directory existed. Never add
// to this slice — see Migrations' own doc comment.
//
// Each entry runs in ONE transaction (the framework's SQL worker wraps it), so
// a failure leaves the database at the previous entry rather than part-way
// through this one (D5).
func legacyMigrations() []Migration {
	return []Migration{
		{1, "0001_core_tables", "project, project_root, loop, loop_phase, task", migration0001},
		{2, "0002_steps_and_events", "step, step_message, event, memory", migration0002},
		{3, "0003_chat", "question and chat_message — the pair", migration0003},
		{4, "0004_documents_and_archive", "loop_document, loop_archive, loop_archive_file", migration0004},
		{5, "0005_policy", "policy, policy_acceptance, policy_event — S10's shape, created early", migration0005},
		{6, "0006_step_context_manifest", "step.context_manifest, by ALTER", migration0006},
		{7, "0007_route", "route — the configuration, never the credential", migration0007},
		{8, "0008_route_reasoning_effort", "route.reasoning_effort, by ALTER", migration0008},
		{9, "0009_route_openai_endpoint", "route.openai_endpoint, by ALTER", migration0009},
		{10, "0010_route_context_window", "route.context_window, by ALTER", migration0010},
		{11, "0011_tool_receipt", "tool_receipt records unsafe tool execution before it starts", migration0011},
		{12, "0012_step_recovery", "step_recovery records classification and failure details", migration0012},
		{13, "0013_recovery_failure", "structured recovery failure stored with the recovery record", migration0013},
		{14, "0014_loop_blueprint", "pinned blueprint and durable loop position", migration0014},
		{15, "0015_recovery_costs", "separate pre-crash and reissued model accounting", migration0015},
		{16, "0016_policy_governance", "policy rebuilt to S10's closed vocabulary — the slice's only migration", migration0016},
		{17, "0017_project_icon", "project icon, optional presentation data", migration0017},
		{18, "0018_policy_authorship_net", "the PL1 authorship CHECK and the per-target acceptance index — the deferred-findings migration", migration0018},
		{19, "0019_policy_action_ask_and_loop_attended",
			"the policy table's action CHECK widened to admit ask; loop.attended declared at pin time — S11's only migration",
			migration0019},
		{20, "0020_step_pause_reason", "step.pause_reason, by ALTER — a user-initiated stop, distinct from a model-asked question", migration0020},
		{21, "0021_step_steer", "step_steer — the durable steer queue (S31 D4): pending, delivered in the checkpoint's own transaction", migration0021},
		{22, "0022_agentsvc_tasks", "agent_task and agent_chain — S14's Task is a row (async is a suspension, AS4/AS5); the chain edge is written before the callee runs", migration0022},
		{23, "0023_plugin_agents", "plugin_agent and plugin_transition — S15's four states as rows: revoked is terminal and the record survives (AP4); the reason-on-revoke CHECK makes an unexplained never-again unwritable", migration0023},
		{24, "0024_task_refused_and_refusals", "agent_task's status CHECK widened to admit refused (rebuild in the 0016 pattern — no table references agent_task) and plugin_refusal — S15 T5's arrival refusals: the work was paid for and the cost is RECORDED, not recovered (AP7)", migration0024},
		{25, "0025_action_intent", "action_intent — S16 T7's ONE intent table (D7): postReview's receipt keyed (step, attempt, action, pr); review_id IS NULL means posted-unknown, go-and-look; one table holds action intents so recovery has one path (Q6)", migration0025},
		{26, "0026_route_metadata", "route display metadata, capability and effort declarations", migration0026},
		{27, "0027_step_message_provenance", "step_message.provenance, by ALTER — S18's M26: who produced an assistant turn, per turn", migration0027},
		{28, "0028_route_wire_capabilities", "route.wire_capabilities and route.no_price, by ALTER — S18's Q8 wire flags and O8's explicit unmetered declaration", migration0028},
		{29, "0029_route_probe", "route_probe — S18 T3's probe cache: verdicts survive a restart, and `declared` is the absence of a row (I5)", migration0029},
		{30, "0030_step_thinking_dropped", "step.thinking_dropped, by ALTER — S18 T5's M4/O1: thinking blocks withheld from a wire are counted INTO the step's record, not only a log line", migration0030},
		{31, "0031_loop_allow_outward", "loop.allow_outward, by ALTER — S19 T6's D10/O2: the outward opt-in is the loop's own declaration made at start, per loop and per moment; no key or caller attribute anywhere carries it", migration0031},
		{32, "0032_trigger_schedule_and_fire", "trigger_schedule and trigger_fire, by CREATE — S20 T1's TR1/TR4: a schedule is a named blueprint plus a wall-clock contract (zone required, expression validated at write, no field selects work), and a fire is a RECORDED answer to \"did this happen?\" — the third answer, could-not-say, is the reason the table exists", migration0032},
		{33, "0033_trigger_fire_inputs", "trigger_schedule.workspace_root/goal and loop.started_by/trigger_id, by ALTER — S20 T2: a fire needs a working directory (workspace rules stay the loop's, not the scheduler's invention), and a loop row must say WHO started it — provenance written beside the pin, in the same transaction as attendedness, and never beside it in the same field (R-3)", migration0033},
		{34, "0034_trigger_gap", "trigger_gap, by CREATE — S20 T4’s TR3/TR4: the missed instants of a down daemon are ONE row per gap (range + count), not a row per instant — a week of a minutely schedule is 10,079 questions and the answer is one honest sentence: the third answer (could-not-say) matters, and the record must never be the slow part of an outage", migration0034},
		{35, "0035_org_layers", "the OL1 rename and the new tier — S38 T1: project→component, project_root→component_root on all 11 tables that carry it, a NEW project (group) table with one implicit project per component (T0-Q7's NOT NULL rebuild rides inside, executed on a populated copy before it was chosen), and component's presentation and repo fields — a rename, never a DROP; the children ride out, the FKs follow, the ledger gains its 35th row", migration0035},
		{36, "0036_records", "the records tier — S38 T5 (OL7's final lists from T0-Q3, OL14's surrogate id from Q6): goal, goal_component, epic, brief, review, attachment and task_loop as new tables; loop gains goal_id/goal_version/scope (component|project, default component); and task is rebuilt onto a surrogate id with manual/assignee/component/goal/parent/type/approval columns — loop tasks keep their (component_root, loop_slug, taskid) key through a PARTIAL unique index (manual tasks carry none), the copy is guarded by a CHECK'd row-count table that fails the migration if a row is lost, assignee_kind derives honestly from the old engineer column, and review.by_kind admits only 'user' because a review is a person's act (a widening is a later rebuild, never an edit)", migration0036},
		{37, "0037_loop_run", "loop_run + step.run_seq — S39 CR2: a loop runs blueprints one after another without ending; loop_blueprint stays ONE row per loop meaning the current run's pin and live position (T0-Q2: seven statements, recovery reads steps not pins — a re-key would change six statements for nothing the snapshot does not give), so loop_run holds each run's immutable pinned copy and its end-snapshot; run state is running|done|failed|cancelled and waiting/blocked are DERIVED from loop.status (DB6: no stored flag that can disagree); run 1 is backfilled from the pins, provenance from S20's loop.started_by/trigger_id, goal coords from S38's loop.goal_id/goal_version for the single-run case; step.run_seq rides ALTER — cost per run is a group-by (P32 floor), never a stored total", migration0037},
		{38, "0038_message_source", "chat_message.source and step_steer.source — where a message came from: user, template, automation or blueprint (S39 CR6: a typed start and a templated kickoff must be told apart in the thread, and an automation's steer must not read as a person's). Both arrive by ALTER (T0-Q4 measured them applying to a populated store): chat_message.source is NULLABLE — a row nobody typed (agent, system, legacy) has NO source rather than a borrowed one, and the one backfill is honest (author='human' rows were a person typing, so 'user'); step_steer.source is NOT NULL DEFAULT 'user' because a steer with no provenance row was today's only kind — a person's. The CHECK vocabularies are the four-word set on both columns; chat's adds IS NULL because its column allows it. Neither table is rebuilt (D10: a rebuild of an append-only row store is a rewrite of history the wire never asked for)", migration0038},
		{39, "0039_automation", "S40 AU3/AU4/AU5/AU7 — the automation outbox and the event triggers: automation_event (the transactional outbox — a domain write and its event commit together or not at all), automation_cursor (the dispatcher's single frontier), event_trigger and event_trigger_fire BESIDE S20's trigger_schedule/trigger_fire (an event trigger has a match, a kickoff and limits, and no zone; a fire is keyed by an outbox row and its undecided state IS the claim — outcome NULL means claimed-undecided, so a crash between claim and start cannot re-fire), the manual-only DB trigger (AU3's rule in the database itself; the WHEN clause keeps COALESCE OUTSIDE the subquery — S40 T0-Q7 measured that moving it inside lets a ghost subject through, so this text is not tidiable), loop.automation_depth (AU5's cascade counter; a person's loop is 0), chat_message.source_ref (which trigger wrote a templated kickoff — nullable, absence honest), and the loop_run CHECK rebuild admitting started_by_kind 'event' (S40 T0-Q2 measured today's three-word CHECK rejecting it; the copy is verbatim and idx_loop_run_state is dropped and recreated by name — AGENTS §3's 0019 precedent: a rebuild that forgets its index dies on 'already exists'). Reuses, never re-adds: loop.started_by/trigger_id (0033) and every S20 column", migration0039},
		{40, "0040_retrieval_index", "S22 T1's two index tables and one virtual table: retrieval_corpus (the kind is a COLUMN with a closed CHECK of 'retrieved'/'concluded' — Stage 5's memory corpus is a row and a validator entry, not a schema change; its provenance/observation_count columns are RESERVED NULLable, and the CHECK makes them present-when-concluded-and-absent-when-retrieved, so a belief without provenance is unwritable the day the kind ships) and retrieval_chunk (file:line ranges plus content hash plus the indexed body — V8's hash and V4's locations in one row), plus retrieval_chunk_fts as FTS5 over the chunk body with the three content-table sync triggers. NOT here: the vec0 virtual table — its embedding dimension is fixed at CREATE time and V6 makes model and dimension DATA (the corpus row answers a query, a migration cannot know them), so internal/store's retrieval statements create it on demand, name-derived and identifier-sanitised. FTS5 is compiled into the driver by the sqlite_fts5 build tag the Makefile carries (B6); a binary built without it fails HERE, loudly, at open — the honest shape, since D3's four availability reasons deliberately have no 'fts5-missing' state", migration0040},
		{41, "0041_loop_queue", "S24's loop queue: two component settings columns by plain ALTER — queue_advance ('auto'/'ask', default 'ask', R4: the safe default is the one nobody has to think about) and sizing_mode ('manual'/'skill', default 'manual') — plus queue_item, the per-repo backlog row. Three facts the SQL alone cannot say: (1) the slice's docs say `project`, written before S38 T1's OL1 rename (0035) moved the repo-with-a-root under the name `component`; the settings live on the row that OWNS roots and loops, and the queue is keyed by component_root, the post-rename name of the same key every loop already rides (a settings column on the group tier would let two repos in one group share an advance mode, which is not what a per-repo backlog means); (2) loop_slug is a plain column until materialization and then rides the same composite FK every loop child already uses — FOREIGN KEY (component_root, loop_slug) REFERENCES loop (component_root, slug), NO cascade: a queue row is the ledger of intent, and its product vanishing under it is the silent record loss D12 forbids; (3) the size is one fact or none — the CHECK makes size_min, size_max and size_basis all-present-or-all-absent with min <= max, so a range without its basis (D8's 'never a bare number') is unwritable at the database level, not discouraged in Go", migration0041},
		{42, "0042_proposal", "S35 T1's proposal table (design TD1, deviated to the OL7 actor pair — see the migration's own doc comment): the slice's only store addition, an inert row no guard read path may ever learn the name of. created_by_kind admits the general three authors (a proposal is a socket write, not inherently human); decided_by_kind is CHECK'd to 'user' alone, mirroring 0036's review.by_kind, because committing or rejecting a proposal is CLI-only by this slice's entire thesis; decided_by_* stay NULLable because a proposed row genuinely has no decider yet, unlike a review row which exists only once decided", migration0042},
		{43, "0043_roadmap_item", "S26 T1's forward Gantt row: roadmap_item, queue_item's sibling under the same discipline — R1's eight-value status CHECK (no ninth), D2's three-state priority_state (default 'unknown', the honest absence, never a blank or an implied-low), D3's estimate all-present-or-all-absent CHECK shared verbatim with 0041's size, D5's provenance with NO default (a person wrote it, a trigger produced it, or a triage handed it off — whoever writes the row answers, a default would answer 'authored' on their behalf), D7's cancelled as its own outcome, D9's brief_path/plan_path as pointers never content, D6's own status beside a loop_slug that stays NULL until materialization on 0041's no-cascade composite FK. Keyed by component_root, the post-OL1 name of the key the slice's docs still call project_root — the same reading S24's own migration recorded for the same pre-rename wording", migration0043},
		{44, "0044_bug", "S29 T1's triage row: bug, a validation-state and priority-state pair carried as TWO separate closed sets, never one crossed enum (D2 — priority's shape is copied verbatim from 0043's roadmap_item, so a hand-off copies the two fields with no translation). validation_state is the four D1 values (unvalidated, reproduced, not-reproduced, could-not-tell) with unvalidated as the DEFAULT — a self-filed claim (D7) arrives there, and it never collapses into could-not-tell; priority_state mirrors 0043's three-state (default 'unknown') with the same label-on-unknown CHECK. filed_by is an agent name or 'person' (D7's self-filing made literal); duplicate_of is a self-referencing FK set ONLY by a human confirmation (D5/N3 — the only write path, the detector never writes it); roadmap_item_id records the S26 hand-off (D8) once it lands. Keyed by component_root, the post-OL1 name the slice's docs still call project_root. NOT here: bug_vec — like 0040's vec0, its embedding dimension is fixed at CREATE time and is data (the route answers it), so the store creates it on demand, name-derived and gated; a bug with no embedding yet is a normal state, not a missing row.", migration0044},
		{45, "0045_proposal_agent_import", "S37 T1's widening of S35's proposal.kind CHECK by 'agent_import' — a one-word widening (AD1), never an edit to 0042's shipped migration. Renumbered from 0043 to 0045 on merge: main's 0043 (S26 roadmap_item) and 0044 (S29 bug) took the number first. 0016's rebuild shape (copy, drop, rename) but WITHOUT 0016's 'must be empty' guard: proposals may exist and must survive, so the copy moves every row and a copy-guard asserts the move was total. No index rides out (T0 Q1: proposal has none); the foreign-key-check end guard is kept (nothing references proposal, so it is a no-op, but it is the 0019 discipline, not a guess)", migration0045},
	}
}

// migration0024 is S15 T5's shape, and it lands as TWO facts in one number.
// (1) 'refused' joins agent_task's status vocabulary — the rebuild follows
// 0016's guarded pattern exactly (new table, copy, drop, rename; SAFE here
// because nothing REFERENCES agent_task — verified before choosing rebuild
// over ALTER, which SQLite cannot do to a CHECK anyway). A refused Task is
// terminal WITH its completed_at: the work happened, the clock is honest.
// (2) plugin_refusal: the arrival refusal record AP7 demands — agent, the
// revocation instant, the reason, the cost, and reached_prompt pinned false
// because a refusal that reached a prompt is not a thing this ledger
// records, it is a bug that must never be written down as normal.
// ── 0025 ─────────────────────────────────────────────────────────────────────
//
// `action_intent` — the single home of ACTION intents (D7, Q6). S7's
// tool_receipt records SPAWNS (command, token, outcome) and its recovery
// path reads that shape; an action intent is a different question —
// "did this outward post happen?" — keyed by (step, attempt, action, pr)
// and answered by the forge, not by a local outcome column. Two tables
// holding action intents would be two recovery paths, so exactly one does,
// and TestOneIntentTable in S16's action package pins it.
//
// review_id IS NULL is the crash window itself: the intent exists, the
// post may or may not have happened, and the resolver goes and LOOKS
// (reconcile by id against pre_review_id, never by body text — two
// identical-bodied reviews are two reviews).
const migration0025 = `
CREATE TABLE action_intent (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug     TEXT NOT NULL,
  step_id       INTEGER NOT NULL REFERENCES step(id) ON DELETE CASCADE,
  attempt       INTEGER NOT NULL CHECK (attempt >= 1),
  action        TEXT NOT NULL CHECK (length(action) > 0),
  pr            INTEGER NOT NULL CHECK (pr >= 1),
  pre_review_id INTEGER NOT NULL DEFAULT 0 CHECK (pre_review_id >= 0),
  review_id     INTEGER,
  opened_at     TEXT NOT NULL,
  recorded_at   TEXT,
  UNIQUE (step_id, attempt, action, pr)
);
CREATE INDEX idx_action_intent_pr ON action_intent(pr, step_id);
`

const migration0024 = `
CREATE TABLE agent_task_new (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  caller_loop_id   TEXT NOT NULL,
  caller_lane      TEXT NOT NULL,
  caller_step_id   INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  question_id      INTEGER REFERENCES question (id),
  callee_agent     TEXT NOT NULL,
  skill_id         TEXT NOT NULL,
  mode             TEXT NOT NULL CHECK (mode IN ('sync','async')),
  status           TEXT NOT NULL CHECK (status IN ('running','waiting','completed','failed','refused')),
  params_json      TEXT NOT NULL DEFAULT '{}',
  result_json      TEXT,
  policies_json    TEXT NOT NULL,
  policies_hash    TEXT NOT NULL,
  created_at       TEXT NOT NULL,
  completed_at     TEXT,
  CONSTRAINT ck_agent_task_completion CHECK (
       (status IN ('running','waiting') AND completed_at IS NULL)
    OR (status IN ('completed','failed','refused') AND completed_at IS NOT NULL)
  ),
  CONSTRAINT ck_agent_task_async_question CHECK (
       (mode = 'async' AND status <> 'running' AND question_id IS NOT NULL)
    OR (mode = 'sync')
  )
);
-- The copy moves every row as it stands; unlike 0016 there is no emptiness
-- precondition — 'refused' WIDENS the vocabulary, it does not re-define it.
INSERT INTO agent_task_new (id, caller_loop_id, caller_lane, caller_step_id, question_id,
    callee_agent, skill_id, mode, status, params_json, result_json, policies_json,
    policies_hash, created_at, completed_at)
  SELECT id, caller_loop_id, caller_lane, caller_step_id, question_id,
    callee_agent, skill_id, mode, status, params_json, result_json, policies_json,
    policies_hash, created_at, completed_at
  FROM agent_task;
DROP TABLE agent_task;
ALTER TABLE agent_task_new RENAME TO agent_task;
-- The indexes ride out under the same names (the fingerprint compares them
-- by name and shape; the 0019 lesson — recreate what the DROPPED table
-- itself owned, and nothing parked elsewhere).
CREATE INDEX idx_agent_task_status ON agent_task (status, id);
CREATE INDEX idx_agent_task_caller ON agent_task (caller_loop_id, caller_step_id);

CREATE TABLE plugin_refusal (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  agent          TEXT NOT NULL,
  task_id        INTEGER,
  revoked_at     TEXT NOT NULL,
  reason         TEXT NOT NULL CHECK (length(trim(reason)) > 0),
  cost_json      TEXT,
  cost_known     INTEGER NOT NULL CHECK (cost_known IN (0,1)),
  recorded_at    TEXT NOT NULL,
  reached_prompt INTEGER NOT NULL DEFAULT 0,
  CONSTRAINT ck_plugin_refusal_cost CHECK (
       (cost_known = 1 AND cost_json IS NOT NULL)
    OR (cost_known = 0 AND cost_json IS NULL)
  )
);
CREATE INDEX idx_plugin_refusal_agent ON plugin_refusal (agent, id);
`

// migration0023 is S15 T3's shape. plugin_agent: one LIVE row per name (the
// partial unique index — revoked rows are history, so a re-import after
// revocation creates a NEW record instead of overwriting the old life);
// the state CHECK mirrors internal/plugin's PersistedStates, and 'revoked'
// being terminal is enforced twice: no (from,to) pair the CHECK admits names
// revoked as FROM, and the verbs in plugin/state.go check the same table.
// plugin_transition: every move recorded — agent, from, to, reason, instant
// — with the reason-on-revoke as a CHECK so a revoke without a why is not
// merely refused by the CLI but UNWRITABLE, even by hand-edited SQL.
const migration0023 = `
CREATE TABLE plugin_agent (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  source      TEXT NOT NULL,
  state       TEXT NOT NULL CHECK (state IN ('disabled','enabled','revoked')),
  imported_at TEXT NOT NULL,
  CONSTRAINT ck_plugin_agent_state_not_none CHECK (state <> 'not-installed')
);
-- One live record per name; revoked rows are history and do not collide.
CREATE UNIQUE INDEX plugin_agent_live ON plugin_agent (name) WHERE state <> 'revoked';

CREATE TABLE plugin_transition (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  agent      TEXT NOT NULL,
  from_state TEXT NOT NULL CHECK (from_state IN ('','disabled','enabled')),
  to_state   TEXT NOT NULL CHECK (to_state IN ('disabled','enabled','revoked')),
  reason     TEXT NOT NULL DEFAULT '',
  at         TEXT NOT NULL,
  -- revoked is terminal AT THE DATABASE: no admitted pair has revoked on
  -- the from side, so an UPDATE out of it cannot be journalled, let alone
  -- issued by a verb.
  CONSTRAINT ck_plugin_transition_terminal CHECK (
       (from_state = ''          AND to_state = 'disabled')
    OR (from_state = 'disabled'  AND to_state IN ('enabled','revoked'))
    OR (from_state = 'enabled'   AND to_state IN ('disabled','revoked'))
  ),
  -- "never again" without a why is a deletion with extra steps, so the
  -- reason is not a convention — a revoked transition without one is a
  -- constraint violation.
  CONSTRAINT ck_plugin_transition_reason CHECK (
       to_state <> 'revoked'
    OR length(trim(reason)) > 0
  )
);
-- "what did we once run, and why did we stop" — the audit read, per agent
-- in order: the one index this table needs.
CREATE INDEX idx_plugin_transition_agent ON plugin_transition (agent, id);

-- And the terminal state is enforced on the RECORD too, not just the ledger:
-- a raw UPDATE out of revoked is aborted by the trigger, so even SQL that
-- skips every Go verb cannot resurrect a revoked row — the only resurrection
-- is a NEW record via re-import (AP4).
CREATE TRIGGER plugin_agent_no_exit_revoked BEFORE UPDATE OF state ON plugin_agent
  WHEN old.state = 'revoked'
  BEGIN
    SELECT RAISE(ABORT, 'refused: revoked is terminal (AP4) — a re-import creates a NEW record');
  END;
`

// migration0022 is S14 T3's shape. agent_task: a Task is a row, and its
// AS13 discipline is visible in the columns — policies_json carries the set
// it was CREATED under and policies_hash lets a resume detect tampering
// (S7's poisoned-manifest rule, applied to delegation). mode is immutable by
// construction: a sync Task that takes too long does not become async, so
// there is no state a step's transcript could wake up into unexplained.
// agent_chain is T5's edge table; it lands here so the number is spent once,
// and the edge row is what lets a resumed daemon (T5/S7) name a delegation it
// did not create.
const migration0022 = `
CREATE TABLE agent_task (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  caller_loop_id   TEXT NOT NULL,
  caller_lane      TEXT NOT NULL,
  caller_step_id   INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  question_id      INTEGER REFERENCES question (id),
  callee_agent     TEXT NOT NULL,
  skill_id         TEXT NOT NULL,
  mode             TEXT NOT NULL CHECK (mode IN ('sync','async')),
  status           TEXT NOT NULL CHECK (status IN ('running','waiting','completed','failed')),
  params_json      TEXT NOT NULL DEFAULT '{}',
  result_json      TEXT,
  policies_json    TEXT NOT NULL,
  policies_hash    TEXT NOT NULL,
  created_at       TEXT NOT NULL,
  completed_at     TEXT,
  CONSTRAINT ck_agent_task_completion CHECK (
       (status IN ('running','waiting') AND completed_at IS NULL)
    OR (status IN ('completed','failed') AND completed_at IS NOT NULL)
  ),
  CONSTRAINT ck_agent_task_async_question CHECK (
       (mode = 'async' AND status <> 'running' AND question_id IS NOT NULL)
    OR (mode = 'sync')
  )
);
CREATE INDEX idx_agent_task_status ON agent_task (status, id);
CREATE INDEX idx_agent_task_caller ON agent_task (caller_loop_id, caller_step_id);
CREATE TABLE agent_chain (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  caller_loop_id   TEXT NOT NULL,
  caller_step_id   INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  callee_agent     TEXT NOT NULL,
  skill_id         TEXT NOT NULL,
  depth            INTEGER NOT NULL CHECK (depth >= 1),
  created_at       TEXT NOT NULL
);
CREATE INDEX idx_agent_chain_caller ON agent_chain (caller_loop_id, caller_step_id);`

const migration0012 = `CREATE TABLE step_recovery (
	step_id INTEGER PRIMARY KEY REFERENCES step(id) ON DELETE CASCADE,
	case_name TEXT NOT NULL,
	reissued INTEGER NOT NULL DEFAULT 0,
	reason TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL
);`

const migration0013 = `ALTER TABLE step_recovery ADD COLUMN failure_json TEXT;`

const migration0014 = `CREATE TABLE loop_blueprint (
    loop_id TEXT PRIMARY KEY REFERENCES loop(id) ON DELETE CASCADE,
    blueprint_id TEXT NOT NULL,
    blueprint_version INTEGER NOT NULL,
    content TEXT NOT NULL,
    runtime_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);`

const migration0015 = `ALTER TABLE step_recovery ADD COLUMN prior_cost_json TEXT;
ALTER TABLE step_recovery ADD COLUMN reissue_cost_json TEXT;`

// ── 0016 ─────────────────────────────────────────────────────────────────────
//
// `policy` rebuilt to S10's closed vocabulary. This is the slice's ONLY
// migration, authorized by ruling R1, which amends S10's N4 ("no migration")
// with the justification below recorded in
// `docs/aeon/S10-policy/brief.md`.
//
// # Why a migration, when the slice's own brief says there is none
//
// S4 wrote this table's CHECK vocabulary before anyone had decided what a policy
// is: five types (`tool`,`path`,`network`,`spend`,`approval`) and five targets
// (`global`,`project`,`loop`,`agent`,`route`). S10's design names seven types
// (`ToolAccess` … `TriggerAccess`) and five different targets (`all-agents`,
// `agent`, `loop`, `lane-type`, `mcp`), so **not one spec-shaped policy can be
// written into the 0005 shape** — the engine arrives with nothing to stand on.
// S10's brief anticipated the mismatch and said to stop and report it rather than
// migrate, and that report was made. The rule at the top of this file is what
// turns the report into a migration: a shipped migration is immutable, so
// "correct 0005" would fix every fresh database and silently leave every real one
// on the old shape with a ledger row swearing it is current. New shape, new
// number.
//
// # Why a rebuild rather than an ALTER
//
// SQLite cannot tighten a CHECK in place and has no `DROP CONSTRAINT`. Changing a
// CHECK means building the table again: create-new / copy / drop / rename. The Go
// side of the same vocabulary lives in `internal/policy/types.go`, and T2's lock
// test pins the two together, because the drift between a CHECK and the constants
// that feed it is exactly the mistake this migration exists to correct.
//
// # The copy is guarded in SQL, not in a comment
//
// The table is provably empty: S4 created it and nothing has ever read or written
// it — `TestNothingInThisSliceReadsOrWritesThePolicyTables` keeps that true, and
// `docs/aeon/STATUS.md` says so in words. So the copy below moves zero rows.
//
// An assumption a migration depends on is an assumption the migration should
// ENFORCE, so `policy_rebuild_guard` turns "is empty" into a constraint: one row
// in `policy` makes that INSERT violate `CHECK (n = 0)`, the transaction rolls
// back, and the open fails loudly (D5). The alternative was a `DROP TABLE`
// deleting governance rows nobody knew were there — a security control vanishing
// with nothing to read, and no error to read it by.
//
// # What this does NOT touch
//
// `policy_acceptance`, `policy_event` (including `detail`) and
// `idx_policy_event_policy` are untouched. Their `REFERENCES policy (id)` clauses
// resolve by NAME at query time, so they point at the rebuilt table without being
// altered — which is also why the old table is dropped BEFORE the new one is
// renamed in: renaming first would have SQLite rewrite those clauses toward the
// departing name.
//
// The column set is 0005's, verbatim, so the copy is a straight column-for-column
// `INSERT … SELECT`. That is deliberate: this migration changes CHECKs and nothing
// else. No new free-text column enters the schema here (legal constraint 1) — the
// "subject" of a refusal stays where the design put it, in
// `policy_event.subject_kind` / `subject_id`, and `spec` stays `spec` so the
// writer T6 adds is a writer against S4's table.
const migration0016 = `
CREATE TABLE policy_new (
  id          TEXT PRIMARY KEY,
  type        TEXT NOT NULL CHECK (type IN ('tool_access','command_access','route_access','effect_class','cost_cap','skill_access','trigger_access')),
  target_kind TEXT NOT NULL CHECK (target_kind IN ('all-agents','agent','loop','lane-type','mcp')),
  target_id   TEXT,
  action      TEXT NOT NULL CHECK (action IN ('allow','deny')),
  spec        TEXT NOT NULL DEFAULT '{}',
  source      TEXT NOT NULL CHECK (source IN ('operator','agent','mcp_server','import')),
  not_after   TEXT,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
-- The precondition, asserted rather than assumed: a row here means somebody wrote a
-- policy in the eleven migrations that promised nobody had, and the open must refuse
-- the upgrade rather than drop it.
CREATE TABLE policy_rebuild_guard (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO policy_rebuild_guard SELECT COUNT(*) FROM policy;
DROP TABLE policy_rebuild_guard;

-- Guarded twice over: the copy moves nothing unless the table is empty, so a database
-- that somehow reaches here with rows keeps them where they are and fails the guard
-- above rather than half-moving them.
INSERT INTO policy_new (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
  SELECT id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at
  FROM policy WHERE (SELECT COUNT(*) FROM policy) = 0;
DROP TABLE policy;
ALTER TABLE policy_new RENAME TO policy;
-- The lookup every evaluation makes. It goes with the DROP and comes back with the
-- rename, under the same name: the fingerprint compares indexes by name and shape, and
-- T8's EXPLAIN reads through it.
CREATE INDEX idx_policy_target ON policy (target_kind, target_id);
`
const migration0017 = `ALTER TABLE project ADD COLUMN icon TEXT NOT NULL DEFAULT '';`

// ── 0018 ─────────────────────────────────────────────────────────────────────
//
// The database's net under PL1, and the index the acceptance read was always
// going to need. The deferred-findings migration: SEC-022(a) — the authorship
// CHECK the four vocabulary CHECKs cannot express — and DEF-027 — the per-target
// read in `PolicyAcceptanceListFor` (`WHERE target_kind = ? AND target_id = ?`)
// that has no index to run on because the table's PRIMARY KEY leads with
// `policy_id`.
//
// # Why a new number rather than an edit of 0016
//
// The rule at the top of this file, unchanged: a shipped migration is immutable.
// 0016 is merged and frozen; tightening its CHECK text would fix every fresh
// database and silently leave every real one on the un-netted shape with a
// ledger row swearing it is current. New shape, new number — the same ruling R1
// reasoning that made 0016 legal when S10's N4 said "no migration".
//
// # Why a rebuild rather than an ALTER
//
// SQLite cannot tighten a CHECK in place and has no `DROP CONSTRAINT`. And this
// CHECK is over a PAIR: `action='allow'` legal only with `source='operator'`.
// Each column's own set admits the bad pair (both values are in their lists),
// so no vocabulary CHECK can catch it — `allow` written by `agent` is legal SQL
// today, and PL1's refusal is convention, which is bypassable: that was exactly
// what SEC-022 reproduced. This makes the bypass inert whoever writes it.
//
// # Why THIS rebuild parks its children and 0016's did not
//
// 0016 got away with `DROP TABLE policy` because the table was provably EMPTY —
// `policy_rebuild_guard` made that an assertion, and `DROP TABLE` performs an
// implicit DELETE that deleted nothing. That precondition is wrong here: S10
// shipped a writer (`internal/policy/record.go` → `store.PolicyAdd`), real
// installs hold governance rows, and `policy` is a PARENT — `policy_acceptance`
// references it `ON DELETE CASCADE`, `policy_event` `ON DELETE SET NULL`.
// Verified against the live catalogue on this checkout, and measured: the naive
// `DROP TABLE policy` over a planted parent took the acceptance record to zero
// rows (CASCADE) and left both audit rows with `policy_id` NULL (SET NULL) —
// the acceptance history deleted, the audit's blame silently erased, and the
// migration reporting success.
//
// `PRAGMA foreign_keys=OFF` is not available as the escape: each migration runs
// in exactly one transaction (the header of Migrations() says so), and the
// pragma is a documented no-op inside one — measured here too: with the DSN's
// `_foreign_keys=1` in force, setting it OFF inside an open transaction leaves
// it reading 1. So the DROP is made safe by construction instead: both child
// tables' rows sit in the park while the parent goes, nothing references the
// parent while they do, and the DROP can fire nothing. The children come home
// by EXPLICIT column lists — `policy_event.id` included, because it is
// AUTOINCREMENT and an audit row whose id changed between two reads is an audit
// row nobody can quote. `TestTheRebuildCarriesRowsAndLeavesTheChildrenIntact`
// plants exactly that family and requires every row back, row for row.
//
// # Rows that cannot cross the net
//
// The copy is an `INSERT … SELECT` INTO the new table, and the new table carries
// the pair CHECK — so a store already holding an `allow` written by a
// non-operator source (the SEC-022 row class) fails the copy and the open
// refuses loudly (D5) rather than carrying the forged grant across or silently
// dropping it. Recovery is 0016's guard's playbook: read the row out, decide
// what it means against the closed vocabulary, delete it, re-open. Refusing is
// correct; the net that reports a forged allow is the net that prevents one.
//
// # The copy is guarded rather than trusted
//
// `policy_0018_copy_guard` turns "every row moved" into a constraint: a row
// lost between the two tables violates `CHECK (moved = total)`, the transaction
// rolls back, the open fails. An assumption a migration depends on is an
// assumption the migration enforces. And `policy_0018_fk_guard` asserts the
// end state with `pragma_foreign_key_check()` empty, so a child that came home
// dangling fails the open instead of reading as an audit trail that quietly
// lost its parent.
//
// # What this does NOT touch
//
// `policy_event.detail` (legal constraint 1: the column and its 256-rune
// write-side bound stand exactly as they are), `policy_acceptance`'s five
// columns (legal constraint 2: no free text arrives), and `idx_policy_event_policy`
// (T8's audit reads plan through it untouched). No new column enters either
// child table; the copy is column-for-column.
const migration0018 = `
CREATE TABLE policy_0018 (
  id          TEXT PRIMARY KEY,
  type        TEXT NOT NULL CHECK (type IN ('tool_access','command_access','route_access','effect_class','cost_cap','skill_access','trigger_access')),
  target_kind TEXT NOT NULL CHECK (target_kind IN ('all-agents','agent','loop','lane-type','mcp')),
  target_id   TEXT,
  action      TEXT NOT NULL CHECK (action IN ('allow','deny')),
  spec        TEXT NOT NULL DEFAULT '{}',
  source      TEXT NOT NULL CHECK (source IN ('operator','agent','mcp_server','import')),
  not_after   TEXT,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  -- SEC-022(a): the pair the four vocabulary CHECKs cannot see. action='allow' with any source but
  -- 'operator' is legal SQL today and the boundary's refusal is convention; this makes the bypass inert
  -- whoever writes it.
  CHECK (action <> 'allow' OR source = 'operator')
);

-- 1. Copy, then assert the copy was total. A row lost between the two tables aborts the transaction
--    (D5's loud open) rather than shipping a store with governance rows missing.
INSERT INTO policy_0018 (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
  SELECT id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at FROM policy;
CREATE TABLE policy_0018_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO policy_0018_copy_guard
  SELECT (SELECT COUNT(*) FROM policy_0018), (SELECT COUNT(*) FROM policy);
DROP TABLE policy_0018_copy_guard;

-- 2. Park the children BEFORE dropping the parent. Both child tables reference policy(id); dropping the
--    parent while rows reference it would fire their ON DELETE actions — CASCADE deletes the acceptance
--    record, SET NULL erases the audit's blame. Nothing references the parent while they sit in the park,
--    so the DROP can fire nothing.
CREATE TABLE zz_0018_acceptance_park AS SELECT * FROM policy_acceptance;
CREATE TABLE zz_0018_event_park       AS SELECT * FROM policy_event;
DELETE FROM policy_acceptance;
DELETE FROM policy_event;

DROP TABLE policy;
ALTER TABLE policy_0018 RENAME TO policy;
-- The lookup goes with the DROP and comes back with the rename, under the same name (0016's rule): the
-- fingerprint compares indexes by name and shape, and T8's EXPLAIN reads through it.
CREATE INDEX idx_policy_target ON policy (target_kind, target_id);

-- 3. Children home. Explicit column lists, ids included: policy_event.id is AUTOINCREMENT, and an audit
--    row whose id changed between two reads is an audit row nobody can quote.
INSERT INTO policy_acceptance (policy_id,target_kind,target_id,accepted_by,accepted_at)
  SELECT policy_id,target_kind,target_id,accepted_by,accepted_at FROM zz_0018_acceptance_park;
INSERT INTO policy_event (id,policy_id,subject_kind,subject_id,outcome,detail,at)
  SELECT id,policy_id,subject_kind,subject_id,outcome,detail,at FROM zz_0018_event_park;
DROP TABLE zz_0018_acceptance_park;
DROP TABLE zz_0018_event_park;

-- DEF-027: the per-target acceptance read had no index because the table's PK leads with policy_id.
CREATE INDEX idx_policy_acceptance_target ON policy_acceptance (target_kind, target_id);

-- 4. Integrity, asserted rather than assumed: if any FK ended up dangling, the open fails here instead of
--    reading an audit trail that quietly lost its parent.
CREATE TABLE policy_0018_fk_guard (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO policy_0018_fk_guard SELECT COUNT(*) FROM pragma_foreign_key_check();
DROP TABLE policy_0018_fk_guard;
`

// ── 0019 ─────────────────────────────────────────────────────────────────────
//
// S11's ONLY migration, carrying both of D1/D2's schema changes in one numbered
// entry so the count lock in internal/store's tests moves once rather than
// twice: the widened `policy.action` CHECK (D1 — `ask` becomes a third action)
// and `loop.attended` (D2 — the pin-time declaration T11 writes).
//
// # Why a new number rather than an edit (the rule at the top of this file)
//
// 0016 and 0018 are shipped and frozen: their `action IN ('allow','deny')`
// literals are the shape of tables that no longer exist, and the LIVE table's
// CHECK is whatever the last rebuild declared. SQLite cannot tighten — or
// widen — a CHECK in place, so the widened vocabulary means a rebuild, and a
// rebuild means a new number. Editing a shipped entry would fix every fresh
// database and leave every real one two actions short with a ledger row
// swearing it is current.
//
// # The rebuild is 0018's, statement for statement
//
// Same park-copy-restore choreography for `policy_acceptance` and
// `policy_event` (the parent is dropped only while the children sit parked, so
// no CASCADE or SET NULL can fire), same `moved = total` copy guard, same
// explicit column lists on the way back (`policy_event.id` included, because
// it is AUTOINCREMENT and an audit row whose id moved is an audit row nobody
// can quote), same `pragma_foreign_key_check()` end-state guard. The PL1 pair
// net — `CHECK (action <> 'allow' OR source = 'operator')` — travels VERBATIM:
// `ask` is deny-like at the boundary (S11's D1), so the net that made a forged
// allow inert must keep making a forged allow inert, and widening the ask
// side would be rewriting a security CHECK nobody asked to rewrite.
//
// `ask` changes only the vocabulary CHECK: the action list gains its third
// member — and this file's prose never repeats the widened literal, so
// counting lines in the DDL that admit the value is exactly one, which is the
// validation that only 0019 widens it.
// The type, target_kind and source CHECKs are carried unchanged. The index on
// the rebuilt table comes back under its original name with the rename
// (`idx_policy_target`) — the framework's fingerprint compares indexes by name
// and shape, and T8's EXPLAIN reads through it; `idx_policy_acceptance_target`
// needs nothing, because its table is parked, not dropped, and the index rides
// out the rebuild in place.
//
// # The second change needs no rebuild
//
// `ALTER TABLE loop ADD COLUMN attended INTEGER` — NULLABLE, deliberately.
// Three-state honesty (P31): NULL is "never declared" (a loop that predates
// S11), 0 is "declared unattended", 1 is "declared attended". SQLite refuses a
// NOT NULL ADD COLUMN without a default, and inventing a default would invent
// a decision nobody made — a column that arrives meaning "unattended" on every
// pre-S11 row is a third state collapsed into two. No CHECK either (SQLite
// cannot add one by ALTER); the value vocabulary is enforced in Go, by
// internal/store, when T11 writes the pin. This migration only declares the
// column; T11 owns every write to it.
const migration0019 = `
CREATE TABLE policy_0019 (
  id          TEXT PRIMARY KEY,
  type        TEXT NOT NULL CHECK (type IN ('tool_access','command_access','route_access','effect_class','cost_cap','skill_access','trigger_access')),
  target_kind TEXT NOT NULL CHECK (target_kind IN ('all-agents','agent','loop','lane-type','mcp')),
  target_id   TEXT,
  action      TEXT NOT NULL CHECK (action IN ('allow','deny','ask')),
  spec        TEXT NOT NULL DEFAULT '{}',
  source      TEXT NOT NULL CHECK (source IN ('operator','agent','mcp_server','import')),
  not_after   TEXT,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  -- SEC-022(a), carried verbatim from 0018: the PL1 net is not rewritten here, only moved. ask is
  -- deny-like at the boundary (S11's D1), so the pair that must stay illegal is exactly the pair 0018 made illegal.
  CHECK (action <> 'allow' OR source = 'operator')
);

-- 1. Copy, then assert the copy was total. A row lost between the two tables aborts the transaction
--    (D5's loud open) rather than shipping a store with governance rows missing.
INSERT INTO policy_0019 (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
  SELECT id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at FROM policy;
CREATE TABLE policy_0019_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO policy_0019_copy_guard
  SELECT (SELECT COUNT(*) FROM policy_0019), (SELECT COUNT(*) FROM policy);
DROP TABLE policy_0019_copy_guard;

-- 2. Park the children BEFORE dropping the parent. Both child tables reference policy(id); dropping the
--    parent while rows reference it would fire their ON DELETE actions — CASCADE deletes the acceptance
--    record, SET NULL erases the audit's blame. Nothing references the parent while they sit in the park,
--    so the DROP can fire nothing.
CREATE TABLE zz_0019_acceptance_park AS SELECT * FROM policy_acceptance;
CREATE TABLE zz_0019_event_park       AS SELECT * FROM policy_event;
DELETE FROM policy_acceptance;
DELETE FROM policy_event;

DROP TABLE policy;
ALTER TABLE policy_0019 RENAME TO policy;
-- The lookup goes with the DROP and comes back with the rename, under the same name (0016's rule): the
-- fingerprint compares indexes by name and shape, and T8's EXPLAIN reads through it.
CREATE INDEX idx_policy_target ON policy (target_kind, target_id);

-- 3. Children home. Explicit column lists, ids included: policy_event.id is AUTOINCREMENT, and an audit
--    row whose id changed between two reads is an audit row nobody can quote.
INSERT INTO policy_acceptance (policy_id,target_kind,target_id,accepted_by,accepted_at)
  SELECT policy_id,target_kind,target_id,accepted_by,accepted_at FROM zz_0019_acceptance_park;
INSERT INTO policy_event (id,policy_id,subject_kind,subject_id,outcome,detail,at)
  SELECT id,policy_id,subject_kind,subject_id,outcome,detail,at FROM zz_0019_event_park;
DROP TABLE zz_0019_acceptance_park;
DROP TABLE zz_0019_event_park;

-- idx_policy_acceptance_target (0018's) is NOT recreated here, and saying so is the point: only
-- policy is dropped and renamed, so the acceptance table's indexes ride out the rebuild in place —
-- recreating it would fail with "already exists", and
-- TestTheAskWideningRefusesAskBefore0019AndAdmitsItAfter counts it exactly once to pin that the
-- ride was clean.

-- 4. Integrity, asserted rather than assumed: if any FK ended up dangling, the open fails here instead of
--    reading an audit trail that quietly lost its parent.
CREATE TABLE policy_0019_fk_guard (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO policy_0019_fk_guard SELECT COUNT(*) FROM pragma_foreign_key_check();
DROP TABLE policy_0019_fk_guard;

-- 5. D2's column, and it needs no rebuild: nullable INTEGER, three states by
--    absence-and-presence, vocabulary enforced in Go at the pin (T11).
ALTER TABLE loop ADD COLUMN attended INTEGER;
`

// ── 0001 ─────────────────────────────────────────────────────────────────────
//
// v1's identity and ladder tables, ported with deliberate omissions. What is
// NOT carried, and why — a port needs its omissions stated (Q4):
//
//	loop.awaiting_user          DB6. The answer is DERIVED from `question`.
//	                            A stored flag and an unanswered question can
//	                            disagree, and nothing says which is right.
//	loop.mcp_version            v1 recorded which MCP wrote the row. Aeon's
//	                            equivalent is the step's format_version, which
//	                            is per transcript rather than per loop.
//	loop.qa_report              Four text columns holding a phase's output.
//	loop.legal                  Those are documents, and documents are rows in
//	loop.memory_harvest         `loop_document` with revisions — which is where
//	loop.docs_sync              the same content belongs once it has a history.
//	loop.deferred_gate
//	loop.blocked_op             v1's suspension model: a flag, a reason and a
//	loop.blocked_task           timestamp on the loop. Aeon suspends a STEP and
//	loop.blocked_reason         holds the reason in `question` (S6), so a
//	loop.blocked_at             blocked loop is one with an unanswered question
//	                            and there is no second place to disagree.
//	loop.f_unresolved_pr        Two boolean gates belonging to v1's own ladder.
//	loop.f_security_deferred    S10's policy tables are where a deferral is
//	                            recorded now, with its subject and its expiry.
//	project.root_folder         v1's own comment retires it: identity moved to
//	                            the repository when a worktree produced a
//	                            second project for the same repo. `project_root`
//	                            is the many-to-one map, and it is carried.
//	project.icon_*              v1's three-column blob format is omitted. A
//	                            single bounded icon value arrives in 0017.
//
// What IS carried beyond the core, and why: the branch columns (S16 needs
// them), the counters, and the `now_*` pointers. A column is free now and a
// migration on a live store later.
const migration0001 = `
CREATE TABLE project (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  slug         TEXT NOT NULL,
  remote       TEXT,
  remote_key   TEXT,
  repo_key     TEXT,
  owner        TEXT,
  tags         TEXT NOT NULL DEFAULT '[]',
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
CREATE INDEX idx_project_remote_key ON project (remote_key);
CREATE INDEX idx_project_repo_key   ON project (repo_key);

-- Every working directory that belongs to a project. A git worktree is a
-- different path for the same repository, which is what made project identity
-- a repository rather than a folder in v1.
CREATE TABLE project_root (
  project_root TEXT PRIMARY KEY,
  project_id   TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
  remote_key   TEXT,
  repo_key     TEXT,
  is_primary   INTEGER NOT NULL DEFAULT 0,
  added_at     TEXT NOT NULL
);
CREATE INDEX idx_project_root_project ON project_root (project_id);

CREATE TABLE loop (
  id                 TEXT NOT NULL,
  slug               TEXT NOT NULL,
  title              TEXT NOT NULL,
  goal               TEXT NOT NULL,
  root               TEXT NOT NULL,
  project_root       TEXT NOT NULL,
  project_key        TEXT,
  mode               TEXT NOT NULL,
  phase              TEXT NOT NULL,
  status             TEXT NOT NULL,
  default_branch     TEXT,
  base_branch        TEXT,
  branch_mode        TEXT,
  feature_branch     TEXT,
  total_failed_gates INTEGER NOT NULL DEFAULT 0,
  splits_done        INTEGER NOT NULL DEFAULT 0,
  qa_rounds          INTEGER NOT NULL DEFAULT 0,
  now_task           TEXT,
  now_agent          TEXT,
  now_step           TEXT,
  now_dispatched_at  TEXT,
  started_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL,
  PRIMARY KEY (id),
  -- The pair, together, is the identity every scoped query uses (DB4). It is
  -- also what lets two checkouts hold a loop called "refactor" without
  -- colliding, which they do constantly.
  UNIQUE (project_root, slug)
);
-- S25's history view reads "every loop by recency" first. The index lands with
-- the schema rather than as a migration on a live store (2a).
CREATE INDEX idx_loop_updated ON loop (updated_at DESC);

CREATE TABLE loop_phase (
  loop_slug    TEXT NOT NULL,
  project_root TEXT NOT NULL,
  phase        TEXT NOT NULL,
  status       TEXT NOT NULL,
  ord          INTEGER NOT NULL,
  created_at   TEXT,
  updated_at   TEXT NOT NULL,
  PRIMARY KEY (project_root, loop_slug, phase),
  FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE
);

CREATE TABLE task (
  loop_slug         TEXT NOT NULL,
  project_root      TEXT NOT NULL,
  taskid            TEXT NOT NULL,
  ord               INTEGER NOT NULL,
  title             TEXT NOT NULL,
  tech              TEXT NOT NULL,
  engineer          TEXT NOT NULL,
  depends_on        TEXT NOT NULL DEFAULT '[]',
  status            TEXT NOT NULL,
  issue             TEXT,
  pr                TEXT,
  branch            TEXT,
  retries           INTEGER NOT NULL DEFAULT 0,
  security_rounds   INTEGER NOT NULL DEFAULT 0,
  review_rounds     INTEGER NOT NULL DEFAULT 0,
  est_changed_lines INTEGER,
  success_summary   TEXT,
  last_evidence     TEXT,
  started_at        TEXT,
  created_at        TEXT,
  updated_at        TEXT NOT NULL,
  PRIMARY KEY (project_root, loop_slug, taskid),
  FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE
);
`

// ── 0002 ─────────────────────────────────────────────────────────────────────
//
// The step, its transcript, the event sequence and memory.
//
// `step.status` carries a CHECK over four values and the distinction between
// two of them is load-bearing (§3a): a SUSPENDED step has no process BY DESIGN
// (P2), an ORPHANED one has no process because the daemon died. A reconciler
// that treats "no process" as orphaned would try to recover every loop that is
// merely waiting for an answer — which is why S7 reads `status='running'`
// specifically.
//
// `cost_state` is a STORED column, never inferred (DB11). The tempting shape
// is one nullable `cost_usd` with the state read from nullity, and it collapses
// `partial` into `known` — a partial cost HAS a number, it is just a floor. The
// lower-bound semantics S3 built would be gone, and no later slice could
// recover them.
//
// `event.loop_slug` and `event.project_root` are NULLABLE, unlike v1's, and
// there is no foreign key to `loop`. The daemon's own lifecycle events
// (`daemon.started`, `client.connected`) belong to no loop and no project, and
// a NOT NULL column would make the daemon invent one.
const migration0002 = `
CREATE TABLE step (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug         TEXT NOT NULL,
  project_root      TEXT NOT NULL,
  taskid            TEXT,
  phase             TEXT NOT NULL,
  agent             TEXT NOT NULL,
  attempt           INTEGER NOT NULL DEFAULT 1,
  status            TEXT NOT NULL CHECK (status IN ('running','suspended','orphaned','done')),
  started_at        TEXT NOT NULL,
  agent_returned_at TEXT,
  ended_at          TEXT,
  agent_ms          INTEGER,
  wall_ms           INTEGER,
  outcome           TEXT,
  ended_by          TEXT NOT NULL DEFAULT 'open',
  source            TEXT NOT NULL DEFAULT 'live',
  cost_usd          REAL,
  cost_state        TEXT NOT NULL DEFAULT 'unknown' CHECK (cost_state IN ('known','partial','unknown')),
  cost_detail       TEXT,
  FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE
);
CREATE INDEX idx_step_loop ON step (project_root, loop_slug, id);
-- S7 reads this one on every startup, and S25 drills down through it (2a).
CREATE INDEX idx_step_status ON step (status, id);

-- One message per row, not a transcript blob: a transcript grows by append,
-- resume reads it whole, and compaction rewrites a middle range. A blob makes
-- every append a read-modify-write of the whole thing — and UNIQUE (step_id,
-- seq) is what makes a concurrent append DETECTABLY wrong rather than
-- silently lossy.
CREATE TABLE step_message (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id        INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  seq            INTEGER NOT NULL,
  format_version INTEGER NOT NULL,
  role           TEXT NOT NULL,
  content        TEXT NOT NULL,
  created_at     TEXT NOT NULL,
  UNIQUE (step_id, seq)
);

-- "id" IS "seq" (DB17): one global monotonic sequence, durable across a
-- restart, which is the gap S2 admitted to in a doc comment and in its demo.
--
-- A subscription filtered to one project receives ids with GAPS, and that is
-- correct: the cursor's contract is "everything after N that matches my
-- filter", and the gaps carry no information a client needs. A sequence per
-- project would need atomic allocation per project and a reconciliation story
-- for a client subscribed across several — all to remove gaps nobody reads.
--
-- No index on "id" alone: it is the rowid, so the table IS that index. What a
-- filtered cursor walk needs is the pair plus the id, which is below.
CREATE TABLE event (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug    TEXT,
  project_root TEXT,
  kind         TEXT NOT NULL,
  payload      TEXT NOT NULL,
  created_at   TEXT NOT NULL
);
CREATE INDEX idx_event_scope ON event (project_root, loop_slug, id);
CREATE INDEX idx_event_kind  ON event (kind, id);

CREATE TABLE memory (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug    TEXT NOT NULL,
  project_root TEXT NOT NULL,
  ts           TEXT NOT NULL,
  agent        TEXT,
  taskid       TEXT,
  note         TEXT NOT NULL,
  FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE
);
CREATE INDEX idx_memory_loop ON memory (project_root, loop_slug, id);
`

// ── 0003 ─────────────────────────────────────────────────────────────────────
//
// The conversation that unblocks a waiting loop. Two tables, because they fail
// differently (DB7): `question` is the suspension MECHANISM the runtime reads,
// `chat_message` is the THREAD a person reads, and a note that answers nothing
// belongs only in the second.
//
// `question.tool_use_id` is nullable on purpose: a permission prompt comes from
// a tool call and has one, a phase gate is asked by the loop driver and does
// not. A NOT NULL column would force the driver to fabricate an id, and a
// fabricated id is one some resume path will eventually try to match.
//
// `chat_message.question_id` is ON DELETE SET NULL rather than CASCADE: the
// thread is what a person read, and deleting a QUESTION should not delete the
// record of having been asked. Deleting the LOOP is different, and both tables
// cascade from it — found by a test, which deleted a loop and watched its
// question survive. An orphaned question is the DB6 failure in another form:
// `AwaitingLoops` would list a loop that no longer exists, and nothing would
// say which of the two was right.
const migration0003 = `
CREATE TABLE question (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug    TEXT NOT NULL,
  project_root TEXT NOT NULL,
  step_id      INTEGER REFERENCES step (id) ON DELETE CASCADE,
  tool_use_id  TEXT,
  kind         TEXT NOT NULL CHECK (kind IN ('clarify','gate','permission','free')),
  body         TEXT NOT NULL,
  options      TEXT,
  answer       TEXT,
  asked_at     TEXT NOT NULL,
  answered_at  TEXT,
  FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE
);
-- The derived "is this loop waiting" query (DB6), and PendingQuestion's
-- oldest-first read.
CREATE INDEX idx_question_pending ON question (project_root, loop_slug, answered_at, id);

CREATE TABLE chat_message (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug    TEXT NOT NULL,
  project_root TEXT NOT NULL,
  seq          INTEGER NOT NULL,
  author       TEXT NOT NULL CHECK (author IN ('human','agent','system')),
  kind         TEXT NOT NULL CHECK (kind IN ('note','steer','question','answer','summary')),
  body         TEXT NOT NULL,
  question_id  INTEGER REFERENCES question (id) ON DELETE SET NULL,
  created_at   TEXT NOT NULL,
  FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE,
  -- The store assigns seq (O3). A caller-supplied sequence is a race, and this
  -- is a table two goroutines append to; the constraint is what turns the race
  -- into a retry rather than a lost message.
  UNIQUE (project_root, loop_slug, seq)
);
`

// ── 0004 ─────────────────────────────────────────────────────────────────────
//
// The loop's documents and its cold storage.
//
// `loop_document` is append-only by revision: `plan.md` is rewritten on a task
// split and again for QA-fix tasks, and "what did the plan say before that" is
// usually the question worth answering.
//
// The store writes NO FILES (DB10). v1 also mirrored each document to
// `{projectRoot}/.projects/loops/{slug}/`; that mirror is disposable and is not
// built here.
const migration0004 = `
CREATE TABLE loop_document (
  id           TEXT PRIMARY KEY,
  project_root TEXT NOT NULL,
  project_key  TEXT,
  loop_slug    TEXT NOT NULL,
  file         TEXT NOT NULL,
  rev          INTEGER NOT NULL,
  content      BLOB NOT NULL,
  bytes        INTEGER NOT NULL,
  sha256       TEXT NOT NULL,
  written_by   TEXT,
  written_at   TEXT NOT NULL,
  UNIQUE (project_root, loop_slug, file, rev)
);
-- Every read is "the head revision of this file", so the index carries rev DESC.
CREATE INDEX idx_document_head ON loop_document (project_root, loop_slug, file, rev DESC);
CREATE INDEX idx_document_loop ON loop_document (project_root, loop_slug);
CREATE INDEX idx_document_project_key ON loop_document (project_key, loop_slug);

-- An archived loop is GONE from the tables above: the header plus its files are
-- the whole record. Split in two so a manifest can be listed without inflating
-- megabytes of content.
CREATE TABLE loop_archive (
  id           TEXT PRIMARY KEY,
  slug         TEXT NOT NULL,
  project_root TEXT NOT NULL,
  title        TEXT NOT NULL,
  goal         TEXT NOT NULL,
  status       TEXT NOT NULL,
  started_at   TEXT,
  ended_at     TEXT,
  archived_at  TEXT NOT NULL,
  state        BLOB NOT NULL,
  state_bytes  INTEGER NOT NULL,
  file_count   INTEGER NOT NULL,
  file_bytes   INTEGER NOT NULL,
  note         TEXT
);
CREATE INDEX idx_archive_project ON loop_archive (project_root, archived_at);

CREATE TABLE loop_archive_file (
  archive_id TEXT NOT NULL REFERENCES loop_archive (id) ON DELETE CASCADE,
  path       TEXT NOT NULL,
  bytes      INTEGER NOT NULL,
  sha256     TEXT NOT NULL,
  modified   TEXT,
  content    BLOB NOT NULL,
  PRIMARY KEY (archive_id, path)
);
`

// ── 0005 ─────────────────────────────────────────────────────────────────────
//
// S10's policy tables, created here and read by nothing in this slice.
//
// # Why a Stage 3 table lands in a Stage 1 schema
//
// The same reason as the history indexes: by S10 this store holds real loops,
// and a table is free now and a migration on a live store later. It is more
// true here, because these three carry the record of what the system was
// allowed to do.
//
// The CHECK on `action` matters more than it looks. The type set is CLOSED, so
// a typo is a write-time failure naming the caller — and without it, **a policy
// that matches nothing is indistinguishable from a policy that permits
// everything**, which is the worst failure this table can have.
//
// `not_after` is NULLABLE and null means NO EXPIRY — not a sentinel date. A
// far-future sentinel is a policy that silently lapses on a day nobody chose.
const migration0005 = `
CREATE TABLE policy (
  id          TEXT PRIMARY KEY,
  type        TEXT NOT NULL CHECK (type IN ('tool','path','network','spend','approval')),
  target_kind TEXT NOT NULL CHECK (target_kind IN ('global','project','loop','agent','route')),
  target_id   TEXT,
  action      TEXT NOT NULL CHECK (action IN ('allow','deny')),
  spec        TEXT NOT NULL DEFAULT '{}',
  source      TEXT NOT NULL,
  not_after   TEXT,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
-- The lookup every evaluation makes.
CREATE INDEX idx_policy_target ON policy (target_kind, target_id);

CREATE TABLE policy_acceptance (
  policy_id   TEXT NOT NULL REFERENCES policy (id) ON DELETE CASCADE,
  target_kind TEXT NOT NULL,
  target_id   TEXT NOT NULL,
  accepted_by TEXT NOT NULL,
  accepted_at TEXT NOT NULL,
  PRIMARY KEY (policy_id, target_kind, target_id)
);

CREATE TABLE policy_event (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  policy_id    TEXT REFERENCES policy (id) ON DELETE SET NULL,
  subject_kind TEXT NOT NULL,
  subject_id   TEXT NOT NULL,
  outcome      TEXT NOT NULL,
  detail       TEXT,
  at           TEXT NOT NULL
);
-- The audit query, and the age-based cap.
CREATE INDEX idx_policy_event_policy ON policy_event (policy_id, at);
`

// ── 0006 ─────────────────────────────────────────────────────────────────────
//
// `context_manifest` — the declared context frozen at first assembly, with a
// content hash per file, `iterationsUsed` and an ABSOLUTE `deadlineAt` (P5).
//
// Absolute, not remaining: a relative deadline cannot survive a suspension
// that lasts overnight. It would grant the resumed step its whole budget
// again, and a step could escape its bounds by suspending repeatedly.
//
// # Why this is its own migration rather than a column in 0002
//
// It is the slice's real `ALTER TABLE`, which exercises DB9's capability
// instead of asserting it — and it is what D3's upgrade test walks. A runner
// tested only on fresh databases has never run its ALTER path.
//
// SQLite cannot ADD COLUMN a NOT NULL without a default to a table that has
// rows, so it is nullable. That constraint is the reason every additive column
// in this schema is nullable or defaulted.
const migration0006 = `
ALTER TABLE step ADD COLUMN context_manifest TEXT;
`

// ── 0007 ─────────────────────────────────────────────────────────────────────
//
// `route` — a provider, a model and its prices, surviving a restart.
//
// # Why this arrives in S4 and not in S3, which built routes
//
// S3 added route management so a client could configure a provider without
// editing the environment and restarting. It had nowhere to put one, so
// `routes.list` reported `persisted: false` and the app said "persistence
// arrives with the store (S4)".
//
// **S4 shipped without keeping that promise**, and a person configuring a route
// in the app watched it vanish on restart. This closes it — the same obligation
// T8 had for S2's `seq`, which was honoured and this one was not.
//
// # THERE IS NO KEY COLUMN, AND THAT IS THE DESIGN
//
// C17 is one rule for the whole product: a credential comes from **the
// environment, or a dedicated file path — never a config file, never argv,
// never a log.** C4's reason is that a config file gets committed; a store in
// `$XDG_STATE_HOME` does not, but the rule names two sources and this is
// neither.
//
// So the credential goes where the cursor signing key already goes: a dedicated
// 0600 file, written by `internal/secrets`, in the 0700 state directory
// (P26 — access control is the filesystem). That also means **S14 can wrap
// those files in encryption without a migration**, which a column would have
// made into one.
//
// A test reads the database's raw bytes and fails if a key appears in them.
// Not a convention — a grep over the file.
//
// `prices` is JSON rather than four columns: S3 owns the shape (M25 — the unit
// is in every field name), and four nullable REALs here would be a second
// declaration of it that drifts the first time a fifth price appears.
const migration0007 = `
CREATE TABLE route (
  id                TEXT PRIMARY KEY,
  provider          TEXT NOT NULL,
  model             TEXT NOT NULL,
  base_url          TEXT,
  max_output_tokens INTEGER NOT NULL DEFAULT 0,
  prices            TEXT,
  extra_body        TEXT,
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL
);
`

// ── 0008 ─────────────────────────────────────────────────────────────────────
//
// `route.reasoning_effort` — how hard a reasoning model should think.
//
// # Why a column rather than a key in `extra_body`
//
// It was expressible in `extra_body` and that was not good enough, for a reason
// a real provider 400 made plain. OpenAI's current models refuse FUNCTION TOOLS
// and reasoning together on `/v1/chat/completions`:
//
//	Function tools with reasoning_effort are not supported for gpt-6-luna in
//	/v1/chat/completions. To use function tools, use /v1/responses or set
//	reasoning_effort to 'none'.
//
// Every Aeon step offers tools, so on such a model **no step can run** until the
// value is set. That makes it a field somebody needs on their first day — and
// the escape hatch becoming the main road is how the escape hatch stops being
// reviewable. A named column also means `aeon routes list` can show it, which an
// opaque JSON blob cannot.
//
// Nullable, like every additive column here: SQLite cannot ADD COLUMN a NOT
// NULL without a default to a table that has rows.
const migration0008 = `
ALTER TABLE route ADD COLUMN reasoning_effort TEXT;
`

// ── 0009 ─────────────────────────────────────────────────────────────────────
//
// `route.openai_endpoint` — which OpenAI API this route talks to.
//
// # Why a route needs to say
//
// `/v1/chat/completions` refuses function tools while reasoning is on;
// `/v1/responses` accepts both. Every Aeon step offers tools, so the endpoint
// decides whether a step can reason at all — and that is a property of the
// route, not of the request.
//
// Nullable, and empty means `chat`: every route written before this column
// existed keeps behaving exactly as it did.
const migration0009 = `
ALTER TABLE route ADD COLUMN openai_endpoint TEXT;
`

const migration0010 = `
ALTER TABLE route ADD COLUMN context_window INTEGER NOT NULL DEFAULT 0 CHECK (context_window >= 0);
`

const migration0011 = `
CREATE TABLE tool_receipt (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id      INTEGER NOT NULL REFERENCES step(id) ON DELETE CASCADE,
  tool_use_id  TEXT NOT NULL,
  tool         TEXT NOT NULL,
  arg_digest   TEXT NOT NULL,
  started_at   TEXT NOT NULL,
  completed_at TEXT,
  result       TEXT,
  is_error     INTEGER,
  UNIQUE (step_id, tool_use_id)
);
CREATE INDEX idx_tool_receipt_step ON tool_receipt(step_id);
`

// ── 0020 ─────────────────────────────────────────────────────────────────────
//
// `step.pause_reason` — a step the USER stopped, as opposed to one SUSPENDED
// because the model asked a real question.
//
// # Why this is a new column and not the existing `status` enum widened
//
// `step.status`'s CHECK is `('running','suspended','orphaned','done')`, and
// `step` is the most heavily cross-referenced table in the schema — widening a
// CHECK on a live SQLite table means the full rebuild `0016` and `0018` used
// for `policy` (copy into a `_new` table, drop, rename), and that technique
// leans on `PRAGMA foreign_keys` being toggled around the drop. SQLite refuses
// to change that pragma INSIDE a transaction, and every migration here runs in
// exactly one (DB9) — so the toggle has no effect where it would need one, and
// a `step` with real child rows in `step_message`, `question` and `tool_receipt`
// is a dangerous table to get that wrong on. `policy` could use the technique
// because it had either zero rows (0016) or a guarded, counted copy with no
// incoming FK of its own (0018). `step` has both real rows and real children.
//
// A plain `ADD COLUMN` has none of that risk, and `suspended` already means
// exactly what P2 says it means — "no process, by design, resumable" — for
// BOTH a step waiting on a question and one a person paused. The two cases
// differ only in *why*, which is what this column records. `NULL` is the
// existing meaning, unchanged; a non-null value is a reason, read nowhere that
// assumes it is absent, because nothing before this column could have written
// one.
//
// # Why it is not a new `question` kind either
//
// `question.kind`'s CHECK is `('clarify','gate','permission','free')`, and
// every one of them is something the MODEL asked via a `question` tool call —
// `ResumeStep`'s own non-recovery path requires a matching pending `tool_use`
// named `"question"` in the transcript before it will accept an answer
// (`hasPendingQuestion`). A user-initiated stop has no such call: the model
// was not asking anything, the person just stopped it. Modelling a stop as a
// question would mean inventing a `tool_use` that was never emitted, which is
// the kind of fiction `ValidateTranscript` exists to refuse elsewhere. The
// correct existing door is `ResumeRequest.Recovery = true`, which already
// skips `hasPendingQuestion` for exactly this reason (S7) — a continuation
// needs no question and no answer, so this migration adds none.
const migration0020 = `ALTER TABLE step ADD COLUMN pause_reason TEXT;`

// ── 0021 ─────────────────────────────────────────────────────────────────────
//
// `step_steer` — a message the person queued for a step that is still running
// (S31, D4). Rows are the queue: `pending`, then `delivered` or `cancelled`,
// with no third state and no state that means "probably".
//
// # Why the row exists at all, rather than a channel
//
// A steer is typed into a running step and consumed at the next turn boundary,
// which is exactly the window in which the daemon can die. An in-memory queue
// loses the instruction silently, and the person has no way to find that out —
// the transcript they watch simply never contains what they typed. A row is what
// makes the gap visible: after a crash the drain finds the row still `pending`
// and delivers it, or finds it `delivered` and finds its text already in the
// checkpointed transcript.
//
// # Why delivery is recorded in the checkpoint's own transaction, and what the
// crash matrix therefore looks like
//
// LW5: the runtime drains the pending rows, appends their text, and calls
// CheckpointWithSteers, which writes the transcript and flips those rows to
// `delivered` in ONE transaction (steers.go). So there is no instant at which
// the two halves disagree, and S7's replay — which reads the transcript and the
// rows after a crash, cold — can only ever meet these:
//
//	kill before the drain         → row pending,      text absent   → drain it
//	kill after the drain,         → row pending,      text absent   → drain it
//	  before the checkpoint           (the append lived in memory only)
//	kill inside the transaction   → row pending,      text absent   → drain it
//	  (SQLite rolls the whole thing back)
//	kill after the commit         → row delivered,    text present  → do not drain
//
// The two rows S7 cannot tell apart — pending-but-present, and
// delivered-but-absent — are precisely the ones a non-transactional delivery
// produces, and precisely the two this table and that one transaction make
// unrepresentable. The first is a steer injected TWICE (the model reads the same
// instruction again and does the thing twice); the second is a person's
// instruction vanished with nothing to show for it. `delivered_at` and
// `delivered_iteration` are what a replay compares against, and
// `TestASteerIsDeliveredOnceEvenAcrossAReopen` is what pins them as write-once.
//
// # Why the CHECKs are here rather than only in Go
//
// The same reason 0016's and 0019's are: a value nobody compiled is a state
// nobody reasons about. `state`'s CHECK closes the machine;
// `ck_step_steer_delivery` ties the two delivery columns to the state that
// earns them, so a row cannot say `delivered` while being unable to say when,
// nor say `pending` while carrying a timestamp — the disguise the double-delivery
// case wears. `ck_step_steer_content` refuses a row that carries neither text
// nor an override: it would be a steer that injects nothing, which is the row a
// caller with a bug in its parameter validation writes.
//
// # Why `text`, `route` and `reasoning_effort` are nullable
//
// `step.steer` takes at least one of the three (brief §5): a route-only steer is
// D5's mid-run change with no text at all. Absent is therefore NULL, not an
// empty string — the two have to be told apart when the runtime decides whether
// to append a message, and an empty column cannot say both "the person said
// nothing" and "the person did not address the text" (the same ruling 0003's
// `question.tool_use_id` and 0020's `pause_reason` follow).
//
// # Why there is no cap in the schema
//
// LW10's bounds (≤ 32 KiB of text, ≤ 16 pending per step) are refusals a person
// has to be told about in words, and the method that can name the remedy is
// `step.steer`'s handler, not the database. A CHECK here would surface them as
// a constraint failure with no advice attached (AGENTS §5). The bound is
// asserted at the door; this table's job is that whatever got through the door
// survives.
const migration0021 = `
CREATE TABLE step_steer (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id             INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  text                TEXT,
  route               TEXT,
  reasoning_effort    TEXT,
  state               TEXT NOT NULL DEFAULT 'pending',
  created_at          TEXT NOT NULL,
  delivered_at        TEXT,
  delivered_iteration INTEGER,
  CONSTRAINT ck_step_steer_state CHECK (state IN ('pending','delivered','cancelled')),
  CONSTRAINT ck_step_steer_delivery CHECK (
       (state = 'pending'   AND delivered_at IS NULL AND delivered_iteration IS NULL)
    OR (state = 'cancelled' AND delivered_at IS NULL AND delivered_iteration IS NULL)
    OR (state = 'delivered' AND delivered_at IS NOT NULL AND delivered_iteration IS NOT NULL)
  ),
  CONSTRAINT ck_step_steer_content CHECK (text IS NOT NULL OR route IS NOT NULL OR reasoning_effort IS NOT NULL)
);
-- The drain query — "what is pending for THIS step, in order" — runs before
-- every model call of every step, so it is the one index this table needs.
CREATE INDEX idx_step_steer_step_state ON step_steer (step_id, state);
`

const migration0026 = `
ALTER TABLE route ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE route ADD COLUMN display_description TEXT NOT NULL DEFAULT '';
ALTER TABLE route ADD COLUMN icon TEXT NOT NULL DEFAULT '';
ALTER TABLE route ADD COLUMN capabilities TEXT CHECK (capabilities IS NULL OR (json_valid(capabilities) AND json_type(capabilities) = 'array'));
ALTER TABLE route ADD COLUMN reasoning_efforts TEXT CHECK (reasoning_efforts IS NULL OR (json_valid(reasoning_efforts) AND json_type(reasoning_efforts) = 'array'));
`

// ── 0027 ─────────────────────────────────────────────────────────────────────
//
// `step_message.provenance` — who produced an assistant turn (S18's M26, O4):
// the JSON triple {route, provider, model}, or ” meaning none.
//
// # Why a COLUMN and not a field inside `content`
//
// `content` is the block array exactly as the providers and S3's envelope know
// it. Provenance is not content — it is a fact ABOUT the turn. Mixing the two
// would make the block array mean two things and force every historical parser
// to learn an optional shape that only appears at the edges.
//
// # Why '' is legal at the schema level and refused at the append gate
//
// Rows written before this column existed have no producer on record, and
// inventing one retroactively would be fabrication (M26 forbids provenance
// that was never earned). So the column is permissive like 0008/0009/0010 —
// every pre-M26 transcript keeps loading and recovering exactly as it did —
// and the rule "an assistant turn arrives with its provenance" is enforced
// where NEW turns enter: AppendMessages.
const migration0027 = `
ALTER TABLE step_message ADD COLUMN provenance TEXT NOT NULL DEFAULT '';
`

// ── 0028 ─────────────────────────────────────────────────────────────────────
//
// `route.wire_capabilities` — the Q8 wire-behaviour flags (S18), and
// `route.no_price` — O8's explicit "this route is unmetered" declaration.
//
// # Why two columns and not a JSON blob beside `capabilities`
//
// `capabilities` belongs to the model catalogue (migration 0026, metadata.go's
// vocabulary). Wire flags are a different axis that shares a word ("thinking")
// with it; storing them in one column would merge the vocabularies at exactly
// the layer that makes the distinction invisible. Separate columns are the
// separation.
//
// # Why the state is NOT stored here
//
// declared/verified/contradicted (M28) belongs to the probe cache (T3's
// `route_probe`), because verification is a FACT ABOUT A CLAIM made by a
// probe, not by the route. A `verified` column on `route` would let the row
// assert a fact it never earned, and I5's rule — a probe never rewrites the
// declaration — would have nowhere to live.
//
// # Why permissive defaults
//
// NULL means UNDECLARED and no_price DEFAULT 0 means "no opinion recorded" —
// every route row written before O8's rule keeps loading (a daemon must not
// die on a row it accepted under yesterday's contract), and the refusal of a
// silently unpriced NEW route lives at the write gate (UpsertRoute).
const migration0028 = `
ALTER TABLE route ADD COLUMN wire_capabilities TEXT CHECK (wire_capabilities IS NULL OR (json_valid(wire_capabilities) AND json_type(wire_capabilities) = 'array'));
ALTER TABLE route ADD COLUMN no_price INTEGER NOT NULL DEFAULT 0;
`

// ── 0029 ─────────────────────────────────────────────────────────────────────
//
// `route_probe` — what a probe FOUND, kept alongside what the route CLAIMS
// (M28, I5), never over it. Three shape decisions carry the meaning:
//
//   - **`declared` is the ABSENCE of a row.** Only verdicts a probe earned
//     (verified, contradicted) are written; the capability vocabulary's third
//     state needs no storage, which is also what makes "the probe never
//     rewrites the declaration" structural rather than promised — there is
//     no column for it to write a claim into.
//   - **The state CHECK admits exactly the two earned states.** A row saying
//     `declared` is unwritable: the schema itself refuses the shape of a
//     badge nobody earned.
//   - **`probed_at` and `probed_model` are NOT NULL** (O2). A cached verdict
//     without when and against-which-model is a stale claim wearing a
//     verified badge; the two columns are what let someone notice, and a
//     result that cannot be aged should not be cached at all.
//
// No foreign key: `route` rows live in memory-and-store with env routes
// having no row at all, and DeleteRoute clears a route's probes explicitly —
// a probe of a route someone deleted is not evidence about anything.
const migration0029 = `
CREATE TABLE route_probe (
  route_id     TEXT NOT NULL,
  capability   TEXT NOT NULL,
  state        TEXT NOT NULL CHECK (state IN ('verified','contradicted')),
  detail       TEXT NOT NULL DEFAULT '',
  probed_model TEXT NOT NULL,
  probed_at    TEXT NOT NULL,
  PRIMARY KEY (route_id, capability)
);
`

// ── 0030 ─────────────────────────────────────────────────────────────────────
//
// `step.thinking_dropped` — S18 T5's home for the drop COUNT (M4, O1).
//
// M27's rule withholds a thinking block from a wire whose provider did not
// produce it; the transcript keeps every block (M1 — the drop is wire
// direction only), so the only place the omission can live is the step's
// record. "3 thinking blocks dropped (provider 'openai' does not support
// them)" has to be READABLE where the step is read — S9's thread renders it
// — because a count that lives only in a log line is a silent drop wearing
// a number.
//
// ALTER, in 0028's style, with the honesty CHECK as a nullable column
// constraint: NULL means nobody recorded (every row predating T5, and every
// step that dropped nothing and was never written) and reads as zero —
// which is honest here in a way it is not for cost, because the number
// counts an EVENT: no event, no row-content. A negative is a bug, refused
// at the door where it costs nothing.
const migration0030 = `
ALTER TABLE step ADD COLUMN thinking_dropped INTEGER CHECK (thinking_dropped IS NULL OR thinking_dropped >= 0);
`

// migration0031 carries S19 T6's opt-in. NOT NULL DEFAULT 0 on purpose:
// unlike attendedness (whose NULL is a THIRD state nobody declared —
// P31), an absent outward permission is simply absent, and the honest
// reading of an absent permission is deny-leaning. A pre-S19 row was
// never opted in, and reading it as opted-in would be the invention the
// whole column exists to prevent.
const migration0031 = `
ALTER TABLE loop ADD COLUMN allow_outward INTEGER NOT NULL DEFAULT 0;
`

// ── 0032 ─────────────────────────────────────────────────────────────────────
//
// `trigger_schedule` and `trigger_fire` — S20's two tables, in one migration
// because a schedule without its fire-record is half a promise (TR3/TR4).
//
// The schedule row is where **TR1 lives as a schema, not a comment**:
// `blueprint NOT NULL` is the whole contract — a schedule FIRES a named
// blueprint, and there is no column that could accept a query, a filter, a
// tag, a priority or a readiness rule. Work selection is S27's; a future
// contributor cannot smuggle it in as "one convenient column" without the
// structural test in internal/trigger failing (and this schema's whitelist
// disagreeing with theirs).
//
// `zone NOT NULL` is TR6 at the door: a schedule that stores without a zone
// is a schedule that fires at some other machine's 03:00. NULLable would have
// made the host's zone the silent default — and the host's zone changes when
// somebody moves a container. `window_start`/`window_end` are a PAIR by
// CHECK: one without the other is a half-window, and half a window is the
// kind of row that "works" until a Sunday explains it.
//
// `kind` admits 'webhook' now rather than in a later migration: a webhook is
// the same row with a different cause (the brief's whole framing), and a
// second table would be the same schedule twice — two places to pause one.
// The `expr = ”` CHECK for webhooks keeps "it fires on a signature, not a
// tick" mechanical.
//
// `trigger_fire` is P31's ninth instance made physical (TR4). `outcome` is a
// closed CHECK set — fired / did_not_fire / could_not_say / skipped /
// refused — because the same question rendered from two nullable booleans
// grows a fourth answer nobody authored. `reason` is NOT NULL DEFAULT ”:
// every non-fired outcome says why (D7's "and says why" is a column
// constraint before it is a UI feature). UNIQUE (schedule_id, instant):
// the doubled autumn hour arrives twice as two different instants — the
// second row is the "already fired for this wall-clock instant" record TR6
// wants, and the uniqueness is what makes "fires once" a fact rather than a
// hope.
const migration0032 = `
CREATE TABLE IF NOT EXISTS trigger_schedule (
  id            TEXT NOT NULL PRIMARY KEY,
  kind          TEXT NOT NULL CHECK (kind IN ('schedule','webhook')),
  blueprint     TEXT NOT NULL,
  expr          TEXT NOT NULL DEFAULT '',
  zone          TEXT NOT NULL,
  window_start  TEXT,
  window_end    TEXT,
  window_days   TEXT NOT NULL DEFAULT '',
  paused        INTEGER NOT NULL DEFAULT 0 CHECK (paused IN (0,1)),
  secret_env    TEXT NOT NULL DEFAULT '',
  covered_until TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  CHECK ((window_start IS NULL AND window_end IS NULL)
      OR (window_start IS NOT NULL AND window_end IS NOT NULL)),
  CHECK (kind <> 'webhook' OR expr = '')
);

CREATE TABLE IF NOT EXISTS trigger_fire (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  schedule_id TEXT NOT NULL REFERENCES trigger_schedule(id) ON DELETE CASCADE,
  instant     TEXT NOT NULL,
  outcome     TEXT NOT NULL CHECK (outcome IN
                ('fired','did_not_fire','could_not_say','skipped','refused')),
  reason      TEXT NOT NULL DEFAULT '',
  loop_id     TEXT NOT NULL DEFAULT '',
  at          TEXT NOT NULL,
  UNIQUE (schedule_id, instant)
);
`

// ── 0033 ─────────────────────────────────────────────────────────────────────
//
// The fire's two inputs and the loop's two provenance columns, in one ALTER
// migration because they are one fact told at one moment: **a fire starts a
// loop somewhere, and the loop row must say what started it.**
//
// `trigger_schedule.workspace_root` — a loop's workspace rules are the
// loop's (validateLoopWorkspace, absolute-and-directory, S19 T7's typed
// refusals); a scheduler that guessed a directory would be inventing policy
// one layer up. DEFAULT ” for pre-S20 rows is honest: none existed, and a
// row written before this column is refused at write by the Go validator,
// not trusted with an empty guess. `goal` is the same story in smaller type:
// a fired loop prints a goal, and the sentence belongs to whoever scheduled
// the fire.
//
// `loop.started_by` / `loop.trigger_id` — R-3, and T2 scope 5's warning
// made physical: **provenance is not attendedness and must not share its
// field.** "Who started it" and "may it act outwardly" are two questions
// with two columns; one column would let somebody set the first to change
// the second, and the demo's `started-by: trigger` beside `attendedness:
// UNATTENDED` is the two-question form. Written by the pin path in the same
// transaction (the same argument 0019 made for attended); a pre-S20 row is
// a person's loop — the honest reading of absence, 0031's discipline.
const migration0033 = `
ALTER TABLE trigger_schedule ADD COLUMN workspace_root TEXT NOT NULL DEFAULT '';
ALTER TABLE trigger_schedule ADD COLUMN goal TEXT NOT NULL DEFAULT '';
ALTER TABLE loop ADD COLUMN started_by TEXT NOT NULL DEFAULT '';
ALTER TABLE loop ADD COLUMN trigger_id TEXT NOT NULL DEFAULT '';
`

// ── 0034 ─────────────────────────────────────────────────────────────────────
//
// The could-not-say record, shaped as the design's scope-6 allows it to be
// shaped: **a RANGE and a COUNT**, one row per outage-gap — not a row per
// missed instant. The choice is stated because the alternative is the trap:
// 10,079 rows for a week of a minutely schedule would make the RECORD the
// slow part of an outage (and the history listing unusable), while a bare
// count with no range could not answer "when were you silent?". One row
// answers both: from the first unanswered instant to the last, how many,
// and when the daemon woke up enough to write it.
//
// The gap lives BESIDE trigger_fire rather than in it: the fire table's
// UNIQUE (schedule_id, instant) is the one-instant-one-answer discipline
// (T1), and a gap is NOT an answer about one instant — it is the honest
// absence of answers across a span. Forcing it into the instant table would
// be the fourth answer wearing a timestamp.
const migration0034 = `
CREATE TABLE IF NOT EXISTS trigger_gap (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  schedule_id  TEXT NOT NULL REFERENCES trigger_schedule(id) ON DELETE CASCADE,
  covered_from TEXT NOT NULL,
  covered_to   TEXT NOT NULL,
  missed       INTEGER NOT NULL CHECK (missed > 0),
  at           TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_trigger_gap_schedule ON trigger_gap(schedule_id, covered_to DESC);
`

// migration0035 is S38 T1's OL1: the rename the spec's Project/Component split
// demanded, done now because nothing has shipped (brief §0.2) — no legacy, no
// dual names, no lazy backfill. Every fact here was executed before it was
// chosen: T0-Q2 measured the blast radius under the written split thresholds,
// and T0-Q7 ran this DDL and the NOT NULL rebuild below on a populated store
// copy (integrity ok, foreign_key_check empty — investigation.md).
//
// The tables carrying the scope column (T1 step-1 query against a live store,
// 2026-10-27 — the ALTER lines below must cover EXACTLY this list):
//
//	chat_message, event, loop, loop_archive, loop_document, loop_phase,
//	memory, project_root, question, step, task
//
// Renames never DROP a referenced parent (TestTheNaiveDropParentWouldHave-
// DestroyedTheChildren is why); SQLite rewrites the composite FKs
// (project_root, loop_slug) REFERENCES loop(...) and the indexes under the
// renamed names — TestMigration0035UpgradesAPopulatedStore proves no child
// was cascaded away and no FK points at a ghost.
const migration0035 = `
ALTER TABLE project RENAME TO component;
ALTER TABLE project_root RENAME TO component_root;
ALTER TABLE component_root RENAME COLUMN project_id TO component_id;
ALTER TABLE component_root RENAME COLUMN project_root TO component_root;
ALTER TABLE chat_message RENAME COLUMN project_root TO component_root;
ALTER TABLE event        RENAME COLUMN project_root TO component_root;
ALTER TABLE loop         RENAME COLUMN project_root TO component_root;
ALTER TABLE loop_archive RENAME COLUMN project_root TO component_root;
ALTER TABLE loop_document RENAME COLUMN project_root TO component_root;
ALTER TABLE loop_phase   RENAME COLUMN project_root TO component_root;
ALTER TABLE memory       RENAME COLUMN project_root TO component_root;
ALTER TABLE question     RENAME COLUMN project_root TO component_root;
ALTER TABLE step         RENAME COLUMN project_root TO component_root;
ALTER TABLE task         RENAME COLUMN project_root TO component_root;

-- The component gains what the spec's Component carries and the shipped
-- project lacked: presentation for the list, and the repo's branch contract.
ALTER TABLE component ADD COLUMN short_description TEXT NOT NULL DEFAULT '';
ALTER TABLE component ADD COLUMN description       TEXT NOT NULL DEFAULT '';
ALTER TABLE component ADD COLUMN default_branch    TEXT NOT NULL DEFAULT '';
ALTER TABLE component ADD COLUMN branch_prefix     TEXT NOT NULL DEFAULT '';

-- The NEW project: the GROUP, the spec's Project. One implicit member per
-- component (brief §0.2 forbids NULL-means-implicit — the backfill below runs
-- unconditionally and the rebuild below makes the link NOT NULL).
CREATE TABLE project (
  id                TEXT PRIMARY KEY,
  name              TEXT NOT NULL,
  short_description TEXT NOT NULL DEFAULT '',
  description       TEXT NOT NULL DEFAULT '',
  icon_emoji        TEXT,
  icon_image        TEXT,
  implicit          INTEGER NOT NULL DEFAULT 0 CHECK (implicit IN (0,1)),
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL,
  CHECK (icon_emoji IS NULL OR icon_image IS NULL)
);
CREATE INDEX idx_project_implicit ON project (implicit);

ALTER TABLE component ADD COLUMN project_id TEXT REFERENCES project (id);
INSERT INTO project (id, name, implicit, created_at, updated_at)
  SELECT 'prj-' || id, name, 1, created_at, updated_at FROM component;
UPDATE component SET project_id = 'prj-' || id;

-- T0-Q7: SQLite cannot ADD COLUMN ... NOT NULL REFERENCES, so the contract
-- (a component ALWAYS belongs to a project) is made physical by the rebuild,
-- following 0016's guarded pattern. A NULL project_id after this line is a
-- bug the schema itself refuses.
CREATE TABLE component_new (
  id                TEXT PRIMARY KEY,
  name              TEXT NOT NULL,
  slug              TEXT NOT NULL,
  remote            TEXT,
  remote_key        TEXT,
  repo_key          TEXT,
  owner             TEXT,
  tags              TEXT NOT NULL DEFAULT '[]',
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL,
  icon              TEXT NOT NULL DEFAULT '',
  short_description TEXT NOT NULL DEFAULT '',
  description       TEXT NOT NULL DEFAULT '',
  default_branch    TEXT NOT NULL DEFAULT '',
  branch_prefix     TEXT NOT NULL DEFAULT '',
  project_id        TEXT NOT NULL REFERENCES project (id)
);
INSERT INTO component_new SELECT id, name, slug, remote, remote_key, repo_key,
  owner, tags, created_at, updated_at, icon, short_description, description,
  default_branch, branch_prefix, project_id FROM component;
-- DROP TABLE on a FK parent acts as DELETE of every row FIRST (SQLite
-- semantics), and 0005's roots column carries ON DELETE CASCADE — measured in
-- S38 T2: this DROP silently emptied component_root, and an integrity check
-- on the wreckage reads clean. The rows are snapshotted and restored around
-- the swap, so "a rename never loses children" holds through the rebuild too.
CREATE TABLE component_root_backup AS SELECT * FROM component_root;
DROP TABLE component;
ALTER TABLE component_new RENAME TO component;
INSERT INTO component_root SELECT * FROM component_root_backup;
DROP TABLE component_root_backup;

-- Indexes under the new names. The parent rename kept the old ones attached
-- and the rebuild detached them; the roots table's parent index survives the
-- renames with its stale name, so it is retargeted explicitly.
DROP INDEX IF EXISTS idx_project_root_project;
CREATE INDEX idx_component_root_component ON component_root (component_id);
CREATE INDEX idx_component_remote_key ON component (remote_key);
CREATE INDEX idx_component_repo_key   ON component (repo_key);
CREATE INDEX idx_component_project    ON component (project_id);
`

// migration0036 — the records tier (S38 T5). OL7's column lists are T0-Q3's
// finals (walked through five real briefs, a real plan and real task files);
// OL14's surrogate id is T0-Q6's verdict (work_item retired — every reader of
// the loop key is one of five Scope-taking store functions, so the key can
// stay while the row gains an address). Three facts the SQL alone cannot
// say: (1) NOTHING references task — `pragma_foreign_key_list` over every
// table returned no rows naming it (measured before writing this), so the
// rebuild needs no child parking (the 0019 pattern stays unused, and
// 0035's CASCADE scar is why that check is a step, not an assumption);
// (2) the copy guard is a TABLE with a CHECK, not a test — if INSERT SELECT
// moves a different number of rows than existed, the migration itself
// refuses to complete, because a silent row loss in a rebuild is the exact
// failure 0035 proved integrity checks do not catch; (3) the loop-key index
// is PARTIAL (WHERE loop_slug IS NOT NULL) because manual tasks — OL14's
// second population — carry no loop: their (NULL, NULL, taskid) rows are
// not a collision (SQLite lets NULLs conflict), and the partial index
// states plainly which population the uniqueness governs.
const migration0036 = `
CREATE TABLE epic (
  id            TEXT PRIMARY KEY,
  project_id    TEXT NOT NULL REFERENCES project (id),
  title         TEXT NOT NULL,
  objective     TEXT NOT NULL DEFAULT '',
  owner         TEXT NOT NULL DEFAULT '',
  target_date   TEXT,
  budget_usd    REAL,
  source_step   INTEGER,
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

CREATE TABLE goal (
  id              TEXT PRIMARY KEY,
  project_id      TEXT NOT NULL REFERENCES project (id),
  epic_id         TEXT REFERENCES epic (id),
  version         INTEGER NOT NULL DEFAULT 1,
  title           TEXT NOT NULL,
  type            TEXT NOT NULL CHECK (type IN ('bug','feature','chore','refactor','investigation')),
  body_note       TEXT NOT NULL DEFAULT '',
  source_kind     TEXT NOT NULL CHECK (source_kind IN ('manual','brainstorm','webhook','cron','event')),
  source_ref      TEXT NOT NULL DEFAULT '',
  source_loop     TEXT,
  source_step     INTEGER,
  review_state    TEXT NOT NULL DEFAULT 'draft' CHECK (review_state IN ('draft','in_review','approved','rejected','parked')),
  created_by_kind TEXT NOT NULL CHECK (created_by_kind IN ('user','agent','trigger')),
  created_by_id   TEXT NOT NULL,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL
);

CREATE TABLE goal_component (
  goal_id       TEXT NOT NULL REFERENCES goal (id) ON DELETE CASCADE,
  goal_version  INTEGER NOT NULL,
  component_id  TEXT NOT NULL REFERENCES component (id),
  rationale     TEXT NOT NULL DEFAULT '',
  estimate_usd  REAL,
  confidence    INTEGER CHECK (confidence IS NULL OR confidence IN (1,2,3)),
  basis         TEXT NOT NULL DEFAULT '[]',
  PRIMARY KEY (goal_id, goal_version, component_id)
);

CREATE TABLE brief (
  id             TEXT PRIMARY KEY,
  loop_slug      TEXT NOT NULL,
  component_root TEXT NOT NULL,
  goal_id        TEXT,
  goal_version   INTEGER,
  version        INTEGER NOT NULL,
  scope          TEXT NOT NULL DEFAULT '',
  out_of_scope   TEXT NOT NULL DEFAULT '',
  acceptance     TEXT NOT NULL DEFAULT '[]',
  constraints    TEXT NOT NULL DEFAULT '[]',
  test_plan      TEXT NOT NULL DEFAULT '',
  review_state   TEXT NOT NULL DEFAULT 'in_review' CHECK (review_state IN ('draft','in_review','approved','rejected','parked')),
  source_step    INTEGER,
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  UNIQUE (component_root, loop_slug, version),
  FOREIGN KEY (component_root, loop_slug) REFERENCES loop (component_root, slug) ON DELETE CASCADE
);

CREATE TABLE review (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  subject_kind    TEXT NOT NULL CHECK (subject_kind IN ('goal','brief','task')),
  subject_id      TEXT NOT NULL,
  subject_version INTEGER NOT NULL,
  decision        TEXT NOT NULL CHECK (decision IN ('approved','rejected','parked')),
  by_kind         TEXT NOT NULL CHECK (by_kind = 'user'),
  by_id           TEXT NOT NULL,
  reason          TEXT NOT NULL DEFAULT '',
  decided_at      TEXT NOT NULL
);

CREATE TABLE attachment (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  subject_kind   TEXT NOT NULL CHECK (subject_kind IN ('goal','brief','epic','task')),
  subject_id     TEXT NOT NULL,
  loop_slug      TEXT NOT NULL,
  component_root TEXT NOT NULL,
  document       TEXT NOT NULL,
  rev            INTEGER NOT NULL,
  step_id        INTEGER,
  created_at     TEXT NOT NULL,
  UNIQUE (subject_kind, subject_id, document, rev)
);

-- loop's goal coordinates (OL7): which goal this loop works, at which
-- version, and the scope it reads — component (one workspace) or project
-- (a whole group, S40's brainstorm shape). Default component: the loops
-- that predate this column were each one workspace's.
ALTER TABLE loop ADD COLUMN goal_id TEXT;
ALTER TABLE loop ADD COLUMN goal_version INTEGER;
ALTER TABLE loop ADD COLUMN scope TEXT NOT NULL DEFAULT 'component' CHECK (scope IN ('component','project'));

-- The task rebuild (OL14). Every existing column rides; the new population
-- (manual tasks) shares the table and the board, and the two CHECKs keep
-- each honest: a loop task (manual=0) carries no approval state and MUST
-- carry its loop key; a manual task needs neither.
CREATE TABLE task_new (
  id                TEXT PRIMARY KEY,
  loop_slug         TEXT,
  component_root    TEXT,
  taskid            TEXT,
  ord               INTEGER NOT NULL DEFAULT 0,
  title             TEXT NOT NULL,
  tech              TEXT NOT NULL DEFAULT '',
  engineer          TEXT NOT NULL DEFAULT '',
  depends_on        TEXT NOT NULL DEFAULT '[]',
  status            TEXT NOT NULL,
  issue             TEXT,
  pr                TEXT,
  branch            TEXT,
  retries           INTEGER NOT NULL DEFAULT 0,
  security_rounds   INTEGER NOT NULL DEFAULT 0,
  review_rounds     INTEGER NOT NULL DEFAULT 0,
  est_changed_lines INTEGER,
  success_summary   TEXT,
  last_evidence     TEXT,
  started_at        TEXT,
  created_at        TEXT,
  updated_at        TEXT NOT NULL,
  manual            INTEGER NOT NULL DEFAULT 0 CHECK (manual IN (0,1)),
  assignee_kind     TEXT CHECK (assignee_kind IS NULL OR assignee_kind IN ('agent','user')),
  assignee          TEXT,
  -- parent_task_id carries NO foreign key on purpose: SQLite cannot
  -- self-reference a table during its own rebuild, and the store enforces
  -- the parent exists (T7). An FK here would be a promise the migration
  -- cannot keep in its own transaction.
  component_id      TEXT REFERENCES component (id),
  goal_id           TEXT,
  parent_task_id    TEXT,
  type              TEXT CHECK (type IS NULL OR type IN ('bug','feature','chore','refactor','investigation')),
  approval_state    TEXT NOT NULL DEFAULT 'none' CHECK (approval_state IN ('none','pending','approved','rejected')),
  created_by_kind   TEXT NOT NULL DEFAULT 'agent',
  created_by_id     TEXT NOT NULL DEFAULT '',
  source_step       INTEGER,
  CHECK (manual = 1 OR approval_state = 'none'),
  CHECK (manual = 1 OR loop_slug IS NOT NULL),
  FOREIGN KEY (component_root, loop_slug) REFERENCES loop (component_root, slug) ON DELETE CASCADE
);

INSERT INTO task_new (id, loop_slug, component_root, taskid, ord, title, tech, engineer, depends_on, status, issue, pr, branch,
    retries, security_rounds, review_rounds, est_changed_lines, success_summary, last_evidence, started_at, created_at, updated_at,
    assignee_kind, assignee)
  SELECT 'tsk-' || lower(hex(randomblob(8))), loop_slug, component_root, taskid, ord, title, tech, engineer, depends_on, status, issue, pr, branch,
    retries, security_rounds, review_rounds, est_changed_lines, success_summary, last_evidence, started_at, created_at, updated_at,
    CASE WHEN engineer <> '' THEN 'agent' END, NULLIF(engineer,'') FROM task;

-- The copy guard: a TABLE whose CHECK fails is a migration that does not
-- complete. Row loss in a rebuild must be LOUD (0035's scar, written
-- into the schema itself rather than left to the test suite alone).
CREATE TABLE task_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO task_copy_guard SELECT (SELECT COUNT(*) FROM task_new), (SELECT COUNT(*) FROM task);
DROP TABLE task_copy_guard;

DROP TABLE task;
ALTER TABLE task_new RENAME TO task;
CREATE UNIQUE INDEX idx_task_loop_key ON task (component_root, loop_slug, taskid) WHERE loop_slug IS NOT NULL;
CREATE INDEX idx_task_status    ON task (status);
CREATE INDEX idx_task_component ON task (component_id, manual);

CREATE TABLE task_loop (
  task_id        TEXT NOT NULL REFERENCES task (id) ON DELETE CASCADE,
  loop_slug      TEXT NOT NULL,
  component_root TEXT NOT NULL,
  role           TEXT NOT NULL CHECK (role IN ('owner','worker')),
  PRIMARY KEY (task_id, loop_slug, component_root),
  FOREIGN KEY (component_root, loop_slug) REFERENCES loop (component_root, slug) ON DELETE CASCADE
);

-- Every loop task's own loop is its 'owner' row (OL7); 'worker' rows arrive
-- with T8 when a manual task is worked by a loop.
INSERT INTO task_loop SELECT id, loop_slug, component_root, 'owner' FROM task WHERE loop_slug IS NOT NULL;
`

// migration0037 is S39's runs table (CR2). The design argument lives in
// design.md CR2 and investigation.md Q2; the load-bearing facts, restated
// where the DDL is read: loop_blueprint is NOT re-keyed — it keeps PRIMARY
// KEY (loop_id) and means *the current run's pin and live position*, because
// its seven statements (store/blueprint.go) and every ReadPinnedLoop reader
// assume one row per loop, and recovery (daemon/recover.go) walks steps, not
// pins. loop_run therefore carries what a re-key would: one immutable copy of
// each run's pinned YAML (content) plus a runtime_json snapshot taken when
// the run ends (T2's EndRun). The run state is deliberately `running|done|
// failed|cancelled`: waiting/blocked are the LOOP's condition, derived from
// loop.status (DB6) — a stored flag that can disagree with the engine's own
// status is the second truth P31 forbids. The end CHECK ties ended_at to the
// state for the same reason. The backfill gives every already-pinned loop
// run 1 — provenance rides S20's loop.started_by/trigger_id (COALESCE: they
// are nullable columns read through COALESCE in loops.go, and an explicit
// NULL here would break the NOT NULL the same way the defaults cannot),
// goal coordinates ride S38 T13's loop.goal_id/goal_version for the
// single-run case (loop has no task_id; task links arrive through
// task_loop when a manual task is worked). step.run_seq arrives by ALTER —
// per-run cost is a GROUP BY over steps (the P32 floor), never a stored
// total that a straggler step can falsify.
const migration0037 = `
CREATE TABLE loop_run (
  loop_id            TEXT    NOT NULL REFERENCES loop (id) ON DELETE CASCADE,
  run_seq            INTEGER NOT NULL CHECK (run_seq >= 1),
  blueprint          TEXT    NOT NULL,
  blueprint_version  INTEGER NOT NULL,
  content            TEXT    NOT NULL,
  runtime_json       TEXT,
  state              TEXT    NOT NULL CHECK (state IN ('running','done','failed','cancelled')),
  started_by_kind    TEXT    NOT NULL CHECK (started_by_kind IN ('user','agent','trigger')),
  started_by_id      TEXT    NOT NULL DEFAULT '',
  kickoff_message_id INTEGER,
  goal_id            TEXT,
  goal_version       INTEGER,
  task_id            TEXT,
  started_at         TEXT    NOT NULL,
  ended_at           TEXT,
  PRIMARY KEY (loop_id, run_seq),
  CONSTRAINT ck_loop_run_end CHECK (
       (state = 'running' AND ended_at IS NULL)
    OR (state <> 'running' AND ended_at IS NOT NULL))
);
CREATE INDEX idx_loop_run_state ON loop_run (loop_id, state);
ALTER TABLE step ADD COLUMN run_seq INTEGER;
INSERT INTO loop_run (loop_id, run_seq, blueprint, blueprint_version, content, runtime_json, state,
                      started_by_kind, started_by_id, goal_id, goal_version, started_at, ended_at)
SELECT b.loop_id, 1, b.blueprint_id, b.blueprint_version, b.content,
       CASE WHEN l.status IN ('done','aborted') THEN b.runtime_json END,
       CASE l.status WHEN 'done' THEN 'done' WHEN 'aborted' THEN 'cancelled' ELSE 'running' END,
       CASE WHEN l.started_by = '' OR l.started_by IS NULL THEN 'user' ELSE 'trigger' END,
       COALESCE(l.trigger_id, ''), l.goal_id, l.goal_version, l.started_at,
       CASE WHEN l.status IN ('done','aborted') THEN l.updated_at END
FROM loop_blueprint b JOIN loop l ON l.id = b.loop_id;
UPDATE step SET run_seq = 1
 WHERE EXISTS (SELECT 1 FROM loop l JOIN loop_blueprint b ON b.loop_id = l.id
                WHERE l.slug = step.loop_slug AND l.component_root = step.component_root);
`

// migration0038 is S39 T5 (CR6): provenance for threads and steers. It
// lands as two ALTERs and one honest UPDATE — no rebuilds (D10). The
// asymmetry between the two columns is deliberate, not tidiness:
// chat_message.source is NULLABLE because a row nobody typed (an agent's
// answer, a system note, a pre-S39 legacy row) has NO source — inventing
// 'user' for it would put words in a person's mouth, and the thread is
// exactly where those words are read. The single backfill is the one case
// where provenance is knowable from the row itself: an author='human' row
// was a person typing, so it is 'user'. step_steer.source is NOT NULL
// DEFAULT 'user' because before S39 every steer could only have come from
// a person — the default states history, not a placeholder. Both CHECKs
// carry the same closed four-word set (user|template|automation|blueprint);
// a fifth value is refused at the door, mirroring ValidSource in Go. Both
// ALTERs were run against a populated store during T0 (Q4) before this
// number was claimed.
const migration0038 = `
ALTER TABLE chat_message ADD COLUMN source TEXT CHECK (source IS NULL OR source IN ('user','template','automation','blueprint'));
UPDATE chat_message SET source = 'user' WHERE author = 'human';
ALTER TABLE step_steer ADD COLUMN source TEXT NOT NULL DEFAULT 'user' CHECK (source IN ('user','template','automation','blueprint'));
`

// migration0039 is S40's only migration: the automation outbox, the event
// triggers and their fire ledger beside S20's schedule tables (AU7), the
// manual-only DB trigger (AU3), loop.automation_depth (AU5),
// chat_message.source_ref, and the loop_run CHECK rebuild that admits
// started_by_kind 'event' (S40 T0-Q2 measured today's three-word CHECK
// rejecting it). Two measured shape facts are load-bearing in this text:
// the manual-only trigger keeps COALESCE OUTSIDE the subquery — S40 T0-Q7
// measured that the inside variant evaluates NULL <> 1 for a ghost row and
// lets it through — and the rebuild drops and recreates idx_loop_run_state
// by name (AGENTS §3's 0019 precedent: leaving it, the CREATE INDEX after
// the swap dies on 'already exists'; the copy of the fifteen columns is
// migration0037's verbatim with one CHECK widened, because a rebuild that
// tidies is a rebuild that drifts).
const migration0039 = `
CREATE TABLE automation_event (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event TEXT NOT NULL CHECK (event IN ('goal.approved','task.created','task.approved','task.completed')),
  subject_kind TEXT NOT NULL CHECK (subject_kind IN ('goal','task')),
  subject_id TEXT NOT NULL,
  subject_version INTEGER NOT NULL DEFAULT 0,
  project_id TEXT NOT NULL DEFAULT '',
  component_id TEXT NOT NULL DEFAULT '',
  goal_type TEXT NOT NULL DEFAULT '',
  task_type TEXT NOT NULL DEFAULT '',
  source_kind TEXT NOT NULL DEFAULT '',
  depth INTEGER NOT NULL DEFAULT 0 CHECK (depth >= 0),
  caused_by_trigger TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  CHECK ((event LIKE 'goal.%' AND subject_kind = 'goal') OR (event LIKE 'task.%' AND subject_kind = 'task'))
);
CREATE INDEX automation_event_subject ON automation_event (subject_kind, subject_id);
CREATE TABLE automation_cursor (
  name TEXT PRIMARY KEY,
  last_id INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);
CREATE TRIGGER automation_event_manual_only BEFORE INSERT ON automation_event
WHEN NEW.subject_kind = 'task'
 AND COALESCE((SELECT manual FROM task WHERE CAST(id AS TEXT) = NEW.subject_id), 0) <> 1
BEGIN SELECT RAISE(ABORT, 'automation events are for manual tasks only'); END;
CREATE TABLE event_trigger (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE CHECK (name <> ''),
  source TEXT NOT NULL DEFAULT 'event' CHECK (source = 'event'),
  project_id TEXT NOT NULL DEFAULT '',
  component_id TEXT NOT NULL DEFAULT '',
  match_json TEXT NOT NULL CHECK (json_valid(match_json)),
  blueprint TEXT NOT NULL CHECK (blueprint <> ''),
  kickoff TEXT NOT NULL DEFAULT '',
  max_runs_per_hour INTEGER NOT NULL CHECK (max_runs_per_hour BETWEEN 1 AND 1000),
  max_cost_per_day_usd REAL CHECK (max_cost_per_day_usd IS NULL OR max_cost_per_day_usd >= 0),
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  disabled_reason TEXT NOT NULL DEFAULT '',
  from_outbox_id INTEGER NOT NULL,
  created_by_kind TEXT NOT NULL CHECK (created_by_kind = 'user'),
  created_by_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE event_trigger_fire (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  trigger_id TEXT NOT NULL,
  outbox_id INTEGER NOT NULL,
  source TEXT NOT NULL DEFAULT 'event' CHECK (source = 'event'),
  subject_kind TEXT NOT NULL,
  subject_id TEXT NOT NULL,
  depth INTEGER NOT NULL,
  outcome TEXT CHECK (outcome IS NULL OR outcome IN ('started','refused','limited','skipped','could-not-say')),
  reason TEXT NOT NULL DEFAULT '',
  loop_id TEXT,
  loop_slug TEXT,
  cost_note TEXT NOT NULL DEFAULT '' CHECK (cost_note IN ('','cost-unknown','cost-floor')),
  claimed_at TEXT NOT NULL,
  decided_at TEXT,
  UNIQUE (trigger_id, outbox_id)
);
CREATE INDEX event_trigger_fire_by_trigger ON event_trigger_fire (trigger_id, id DESC);
ALTER TABLE loop ADD COLUMN automation_depth INTEGER NOT NULL DEFAULT 0;
ALTER TABLE chat_message ADD COLUMN source_ref TEXT;
DROP INDEX IF EXISTS idx_loop_run_state;
CREATE TABLE loop_run_0039 (
  loop_id            TEXT    NOT NULL REFERENCES loop (id) ON DELETE CASCADE,
  run_seq            INTEGER NOT NULL CHECK (run_seq >= 1),
  blueprint          TEXT    NOT NULL,
  blueprint_version  INTEGER NOT NULL,
  content            TEXT    NOT NULL,
  runtime_json       TEXT,
  state              TEXT    NOT NULL CHECK (state IN ('running','done','failed','cancelled')),
  started_by_kind    TEXT    NOT NULL CHECK (started_by_kind IN ('user','agent','trigger','event')),
  started_by_id      TEXT    NOT NULL DEFAULT '',
  kickoff_message_id INTEGER,
  goal_id            TEXT,
  goal_version       INTEGER,
  task_id            TEXT,
  started_at         TEXT    NOT NULL,
  ended_at           TEXT,
  PRIMARY KEY (loop_id, run_seq),
  CONSTRAINT ck_loop_run_end CHECK (
       (state = 'running' AND ended_at IS NULL)
    OR (state <> 'running' AND ended_at IS NOT NULL))
);
INSERT INTO loop_run_0039 SELECT loop_id, run_seq, blueprint, blueprint_version, content, runtime_json, state, started_by_kind, started_by_id, kickoff_message_id, goal_id, goal_version, task_id, started_at, ended_at FROM loop_run;
DROP TABLE loop_run;
ALTER TABLE loop_run_0039 RENAME TO loop_run;
CREATE INDEX idx_loop_run_state ON loop_run (loop_id, state);
`

// migration0040 is S22's index home: two plain tables and FTS5, per project
// root (V12 — a worktree is a distinct root, and the root is a COLUMN, not a
// database; every query filters by it, and TestIndexIsPerProjectRoot makes
// that a test rather than a hope). The corpus KIND is data (T1's column
// decision: Stage 5's concluded corpus must arrive as a row plus a validator
// entry, never as a migration onto live indexes), and the CHECK is the
// forward guard — provenance and observation_count are reserved NULLable for
// concluded rows and NULL-forbidden-but-present otherwise, so nobody can
// write a belief (a concluded result, which CAN be wrong and persists
// invisibly) without saying where it came from and how many times it was
// seen. Nothing in S22 writes a concluded row; internal/retrieval's validator
// rejects the kind naming Stage 5, and the raw-SQL test proves both directions
// of the CHECK so the guard is executable rather than decorative.
//
// The body lives on the chunk row because FTS5 external-content mode reads it
// back through the triggers — an INDEXED copy, not a quoted result: D8 is
// about what a QUERY returns (file:line, never text), and the retrieval
// package's types have no field a chunk body could arrive in.
const migration0040 = `
CREATE TABLE retrieval_corpus (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  component_root    TEXT NOT NULL,
  name              TEXT NOT NULL CHECK (name <> ''),
  kind              TEXT NOT NULL DEFAULT 'retrieved' CHECK (kind IN ('retrieved', 'concluded')),
  embed_model       TEXT,
  embed_dim         INTEGER,
  provenance        TEXT CHECK (provenance IS NULL OR provenance IN ('told', 'observed', 'assumed')),
  observation_count INTEGER,
  UNIQUE (component_root, name),
  CHECK (
    (kind = 'retrieved' AND provenance IS NULL AND observation_count IS NULL)
    OR
    (kind = 'concluded' AND provenance IS NOT NULL AND observation_count IS NOT NULL)
  )
);
CREATE INDEX idx_retrieval_corpus_root ON retrieval_corpus (component_root);
CREATE TABLE retrieval_chunk (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  corpus_id    INTEGER NOT NULL REFERENCES retrieval_corpus (id) ON DELETE CASCADE,
  source       TEXT NOT NULL,
  line_start   INTEGER NOT NULL,
  line_end     INTEGER NOT NULL,
  content_hash TEXT NOT NULL,
  body         TEXT NOT NULL,
  UNIQUE (corpus_id, source, line_start, line_end)
);
CREATE INDEX idx_retrieval_chunk_corpus ON retrieval_chunk (corpus_id, content_hash);
CREATE VIRTUAL TABLE retrieval_chunk_fts USING fts5(
  body,
  content='retrieval_chunk',
  content_rowid='id',
  tokenize='unicode61'
);
CREATE TRIGGER retrieval_chunk_ai AFTER INSERT ON retrieval_chunk BEGIN
  INSERT INTO retrieval_chunk_fts (rowid, body) VALUES (new.id, new.body);
END;
CREATE TRIGGER retrieval_chunk_ad AFTER DELETE ON retrieval_chunk BEGIN
  INSERT INTO retrieval_chunk_fts (retrieval_chunk_fts, rowid, body) VALUES ('delete', old.id, old.body);
END;
CREATE TRIGGER retrieval_chunk_au AFTER UPDATE ON retrieval_chunk BEGIN
  INSERT INTO retrieval_chunk_fts (retrieval_chunk_fts, rowid, body) VALUES ('delete', old.id, old.body);
  INSERT INTO retrieval_chunk_fts (rowid, body) VALUES (new.id, new.body);
END;
`

// ── 0041 ─────────────────────────────────────────────────────────────────────
//
// migration0041 is S24's loop queue (design CG7/CG8): the two per-repo
// settings and the backlog row. Why a plain ALTER and not a rebuild: 0016,
// 0019 and 0024 rebuilt tables whose ROWS depended on the old shape; the
// component here only gains columns with safe defaults, nothing is narrowed,
// and an inline CHECK on ADD COLUMN works on this driver (mattn's bundled
// SQLite — measured before writing this). Why the columns land on `component`
// and the queue keys on `component_root`: the slice's docs say `project`,
// written before 0035's OL1 rename gave that word to the group tier; the
// entity that owns roots and loops is the component, and settings on the
// group would merge two repos' advance modes into one decision. Why the size
// CHECK is three columns in one constraint: D8 says a range without its
// basis is not a size, so the database refuses to store one — the same shape
// 0040 gave the memory corpus's provenance pair, applied here.
const migration0041 = `
ALTER TABLE component ADD COLUMN queue_advance TEXT NOT NULL DEFAULT 'ask'
  CHECK (queue_advance IN ('auto','ask'));
ALTER TABLE component ADD COLUMN sizing_mode TEXT NOT NULL DEFAULT 'manual'
  CHECK (sizing_mode IN ('manual','skill'));

CREATE TABLE queue_item (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  component_root TEXT NOT NULL REFERENCES component_root (component_root),
  position       INTEGER NOT NULL,
  status         TEXT NOT NULL CHECK (status IN ('pending','sizing','ready','running','done','blocked','cancelled')),
  blueprint      TEXT NOT NULL,
  goal           TEXT NOT NULL,
  workspace_root TEXT NOT NULL,
  tech           TEXT,
  with_json      TEXT,
  attended       INTEGER CHECK (attended IS NULL OR attended IN (0,1)),
  size_min       INTEGER,
  size_max       INTEGER,
  size_basis     TEXT,
  loop_slug      TEXT,
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  CHECK (
    (size_min IS NULL AND size_max IS NULL AND size_basis IS NULL)
    OR
    (size_min IS NOT NULL AND size_max IS NOT NULL AND size_basis IS NOT NULL
     AND size_basis <> '' AND size_min <= size_max)
  ),
  FOREIGN KEY (component_root, loop_slug) REFERENCES loop (component_root, slug)
);
CREATE INDEX idx_queue_item_root_position ON queue_item (component_root, position);
`

// ── 0042 ─────────────────────────────────────────────────────────────────────
//
// migration0042 is S35 T1's proposal table (design TD1): the slice's only
// store addition — an inert row that constrains nothing until a human
// commits it (brief §4's guard-blind invariant; no code in a guard's read
// path may ever learn this table's name). Deviation from TD1's own DDL,
// recorded because the task asked to use "the same token" an existing write
// uses rather than guess: TD1 specified a single `created_by`/`decided_by`
// TEXT column, but every authored row added since (S38's 0036 goal/task,
// S40's 0039 event_trigger) carries the OL7 actor pair instead
// (`created_by_kind` CHECK'd to the three admitted authors, `created_by_id`
// — see internal/store/actor.go's own doc comment: "every record row
// carries created_by_kind/created_by_id, and a decision carries the same
// pair under by_kind/by_id"). `created_by_kind` keeps the general three-kind
// set (a proposal is a socket write like goal/task, not inherently human);
// `decided_by_kind` is CHECK'd to 'user' alone, mirroring 0036's own
// `review.by_kind` CHECK exactly, because deciding a proposal is this
// slice's entire thesis (brief §6: the human-only CLI door) — there is no
// path by which an agent or a trigger ever commits or rejects one.
// decided_by_* stay NULLable (unlike review.by_kind/by_id, which are NOT
// NULL because a review row is created only once decided): a proposal row
// exists in 'proposed' state before any decision, so its decider is
// genuinely absent, not merely empty.
const migration0042 = `
CREATE TABLE proposal (
  id              INTEGER PRIMARY KEY,
  kind            TEXT NOT NULL CHECK (kind IN
                    ('policy_add','policy_remove','seed_corpus','mcp_server_add','mcp_server_remove')),
  payload         TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'proposed'
                    CHECK (status IN ('proposed','committed','rejected','discarded')),
  created_at      TEXT NOT NULL,
  created_by_kind TEXT NOT NULL CHECK (created_by_kind IN ('user','agent','trigger')),
  created_by_id   TEXT NOT NULL,
  decided_at      TEXT,
  decided_by_kind TEXT CHECK (decided_by_kind IS NULL OR decided_by_kind = 'user'),
  decided_by_id   TEXT,
  decide_reason   TEXT,
  applied_error   TEXT
);
`

// ── 0043 ─────────────────────────────────────────────────────────────────────
//
// migration0043 is S26's roadmap (design R1/R2, brief D2–D7): the forward
// Gantt row, queue_item's (0041) sibling under the same discipline — one
// row is the plan of intent, and everything it points at stays elsewhere.
// Four facts the SQL alone cannot say: (1) the slice's docs say
// `project_root`, written before 0035's OL1 rename moved that key under the
// name `component_root`; S24's own migration recorded the same reading for
// the same pre-rename wording, and this row keys the way loops, the queue
// and history already key. (2) `provenance` has NO DEFAULT: D5's question —
// did a person write this, did a trigger produce it, did a triage hand it
// off — is answered by whoever writes the row, and a default would answer
// `authored` on their behalf, which is exactly the silent misattribution
// D5 exists to prevent. (3) The two never-bare rules are unwritable states,
// not Go conventions: an estimate is all three columns or none (min <= max,
// basis non-empty — the verbatim CHECK 0041 gave the queue's size), and a
// priority_label rides only a priority_state that is not 'unknown',
// because a label on an unknown priority is the confident wrong answer D2
// exists to prevent (a known or assumed priority MAY still carry no label
// yet — T1 makes the label meaningful-when-not-unknown, not mandatory).
// (4) status is R1's closed eight, `cancelled` D7's own honest outcome, and
// loop_slug is NULL until materialization on 0041's exact no-cascade
// composite FK — a roadmap item is a ledger of intent too, and its product
// vanishing under it is the silent record loss that rule already forbids.
// epic is a plain nullable TEXT tag (R2: a tag, not a row — an epic with a
// lifecycle of its own is a new table and a migration, never a retrofit
// onto this column).
const migration0043 = `
CREATE TABLE roadmap_item (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  component_root  TEXT NOT NULL REFERENCES component_root (component_root),
  title           TEXT NOT NULL,
  epic            TEXT,
  status          TEXT NOT NULL CHECK (status IN ('idea','ideating','brief-agreed','planned','ready','running','done','cancelled')),
  priority_state  TEXT NOT NULL DEFAULT 'unknown' CHECK (priority_state IN ('known','assumed','unknown')),
  priority_label  TEXT,
  estimate_min    INTEGER,
  estimate_max    INTEGER,
  estimate_basis  TEXT,
  provenance      TEXT NOT NULL CHECK (provenance IN ('authored','triggered','triaged')),
  brief_path      TEXT,
  plan_path       TEXT,
  loop_slug       TEXT,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  CHECK (priority_state <> 'unknown' OR priority_label IS NULL),
  CHECK (
    (estimate_min IS NULL AND estimate_max IS NULL AND estimate_basis IS NULL)
    OR
    (estimate_min IS NOT NULL AND estimate_max IS NOT NULL AND estimate_basis IS NOT NULL
     AND estimate_basis <> '' AND estimate_min <= estimate_max)
  ),
  FOREIGN KEY (component_root, loop_slug) REFERENCES loop (component_root, slug)
);
CREATE INDEX idx_roadmap_item_root_status ON roadmap_item (component_root, status);
`

// ── 0044 ─────────────────────────────────────────────────────────────────────
//
// S29 T1's triage row. The whole argument of this slice is that validation
// STATE and priority STATE are TWO fields, never one crossed enum — so the two
// CHECKs below are two separate closed sets, and priority's shape is copied
// VERBATIM from 0043's roadmap_item (three-state priority_state, default
// 'unknown', the label-on-unknown CHECK the same way), because D2 names itself
// as the constraint S26 must satisfy and the hand-off (T6) copies the two
// fields with no translation step.
//
// validation_state is the four D1 values, with 'unvalidated' as the DEFAULT —
// a self-filed claim (D7) and any human report arrive there, and it is the
// state that must never collapse into 'could-not-tell' (that would make a
// flood of never-looked-at claims read as a genuine inconclusive backlog).
// The four values are closed by the CHECK, exactly as 0043's eight statuses
// are; a fifth is unwritable at the database.
//
// priority_state mirrors 0043 exactly, and the label-on-unknown CHECK is the
// same sentence 0043 wrote: a label on an unknown priority is the D2
// contradiction, unwritable at the database, so a renderer that "just wants a
// status badge" cannot flatten the two splits into one (R4's guard, tested
// from this CHECK's shape).
//
// filed_by is an agent name or 'person' — D7's self-filing made literal. A
// row with filed_by 'person' was a human report; one carrying an agent name
// was self-filed. Both start unvalidated, never pre-labeled: the file_bug
// tool's schema (T3) cannot even name validation_state or priority_state, and
// this DEFAULT is the backstop if a row somehow reaches here another way.
//
// duplicate_of is a self-referencing FK, and it is NULL by default because the
// detector NEVER writes it (D5/N3): only a human confirmation does. The single
// write path is internal/triage's ConfirmDuplicate; this column has no DEFAULT
// and no trigger that could set it, so a raw UPDATE out of the one door is the
// only way a duplicate can become real — the same "prove it by what is not
// there" discipline the slice's tests use.
//
// roadmap_item_id is NULL until D8's hand-off (T6) creates the S26 item; one
// hand-off per bug, and the id is the record that the hand-off happened.
//
// Keyed by component_root — the post-OL1 name (0035) of the key the slice's
// docs still call project_root, the same reading 0041 and 0043 recorded.
//
// NOT in this migration: bug_vec. Like 0040's vec0, its embedding dimension is
// fixed at CREATE time and is DATA (the route's capability answers it — a
// migration cannot know a model that may not exist yet), so the store creates
// it on demand, name-derived and identifier-gated (EnsureBugVecTable), a
// per-project table beside S22's on-demand vec0 rather than a third corpus
// inside it (D6/N5). A bug with no computed embedding yet is a normal, working
// state — the on-demand table simply has no row for it.
const migration0044 = `
CREATE TABLE bug (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  component_root  TEXT NOT NULL REFERENCES component_root (component_root),
  title           TEXT NOT NULL,
  body            TEXT NOT NULL,
  validation_state TEXT NOT NULL DEFAULT 'unvalidated' CHECK (validation_state IN ('unvalidated','reproduced','not-reproduced','could-not-tell')),
  priority_state  TEXT NOT NULL DEFAULT 'unknown' CHECK (priority_state IN ('known','assumed','unknown')),
  priority_label  TEXT,
  filed_by        TEXT NOT NULL,
  duplicate_of    INTEGER REFERENCES bug(id),
  roadmap_item_id INTEGER,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  CHECK (priority_state <> 'unknown' OR priority_label IS NULL)
);
CREATE INDEX idx_bug_root_validation ON bug (component_root, validation_state);
CREATE INDEX idx_bug_root_priority ON bug (component_root, priority_state);
`

// ── 0045 ─────────────────────────────────────────────────────────────────────
// migration0045 is S37 T1's widening of S35's proposal.kind CHECK by
// 'agent_import' — a one-word widening (AD1), never an edit to 0042's shipped
// migration. It uses 0016's rebuild shape (copy, drop, rename) but WITHOUT
// 0016's "must be empty" guard: proposals may exist and must survive, so the
// copy moves every row and a copy-guard asserts the move was total. The
// column list equals 0042's exactly (T0 Q1) — the only change is ,'agent_import'
// appended to the kind CHECK. No index rides out (T0 Q1: proposal has none).
// The foreign-key-check end guard is 0019's discipline (kept even though
// nothing references proposal, so it is a no-op today — but a DROP that loses a
// referenced parent would fire CASCADE, and the guard is the thing that would
// say so).
const migration0045 = `
CREATE TABLE proposal_new (
  id              INTEGER PRIMARY KEY,
  kind            TEXT NOT NULL CHECK (kind IN
                    ('policy_add','policy_remove','seed_corpus','mcp_server_add','mcp_server_remove','agent_import')),
  payload         TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'proposed'
                    CHECK (status IN ('proposed','committed','rejected','discarded')),
  created_at      TEXT NOT NULL,
  created_by_kind TEXT NOT NULL CHECK (created_by_kind IN ('user','agent','trigger')),
  created_by_id   TEXT NOT NULL,
  decided_at      TEXT,
  decided_by_kind TEXT CHECK (decided_by_kind IS NULL OR decided_by_kind = 'user'),
  decided_by_id   TEXT,
  decide_reason   TEXT,
  applied_error   TEXT
);

-- Copy every row (unlike 0016: proposals may exist and must survive the
-- widening, not be refused over).
INSERT INTO proposal_new (id,kind,payload,status,created_at,created_by_kind,created_by_id,decided_at,decided_by_kind,decided_by_id,decide_reason,applied_error)
  SELECT id,kind,payload,status,created_at,created_by_kind,created_by_id,decided_at,decided_by_kind,decided_by_id,decide_reason,applied_error FROM proposal;

-- The copy was total: a row lost between the two tables aborts the transaction.
CREATE TABLE proposal_0045_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO proposal_0045_copy_guard
  SELECT (SELECT COUNT(*) FROM proposal_new), (SELECT COUNT(*) FROM proposal);
DROP TABLE proposal_0045_copy_guard;

DROP TABLE proposal;
ALTER TABLE proposal_new RENAME TO proposal;

-- No index to recreate (T0 Q1: proposal has none).

-- Integrity, asserted rather than assumed: if any FK ended up dangling, the open
-- fails here rather than reading a trail that quietly lost a parent.
CREATE TABLE proposal_0045_fk_guard (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO proposal_0045_fk_guard SELECT COUNT(*) FROM pragma_foreign_key_check();
DROP TABLE proposal_0045_fk_guard;
`

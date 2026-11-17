-- S63 RT1/RT2/RT6/RT16 · the outcome-fact tables: one row per terminal loop
-- event, one row per agent that worked in it, the learning link, and the
-- mutable template set the seed engine owns.
--
-- These are NEW tables, so the migration is plain: create the four tables,
-- their indexes, and the append-only triggers on the three fact tables
-- (`retro_template` is mutable — the seed engine upserts it). The trigger
-- convention rides timestamped files exactly as S55's loop_control carried it.
--
-- No FK from a fact to `loop`: an archived loop's rows CASCADE away
-- (measured, S63 T0 §3) while the fact must outlive it — history/learnings
-- outlive each other (P3). No FK to `learning` either: a learning can be
-- retracted (S52) or its host row deleted while the fact of what was filed
-- stays. The one FK is `retro_agent → retro_outcome`, inside the fact set.
--
-- terminal_key is RT2's idempotency: outcome_kind + "@" + the loop row's
-- updated_at as re-read at record time (millisecond precision, isoUTC —
-- store/open.go). The event seq is NOT reachable from a reactor (T0 measured:
-- publishLoop never sees it), so this is the key the spec's alternative names.
-- UNIQUE (component_root, loop_slug, terminal_key) makes a replay an
-- ON CONFLICT DO NOTHING, not a second fact.
--
-- NULL is never zero (P31): every measured column is nullable and a missing
-- source field is recorded as NULL with complete=0 — a 0 here would be a
-- claim that a thing was counted and found absent. A COUNT is never negative,
-- so every measured column also carries a `>= 0` CHECK: the NULL/0/third
-- state is the shape, and a negative number is not a fact — the only honest
-- readings are NULL or a count. outcome_kind is a closed bounded shape
-- (`done`, `aborted`, `cancelled`, `blocked/<kind>`,
-- `blocked/other:<kind>` ≤ 48) — free text (a reason's detail) has no column.

-- ---------------------------------------------------------------------------
-- retro_outcome — one row per terminal event (finished done/aborted/cancelled,
-- or blocked with a reason kind). Append-only.
-- ---------------------------------------------------------------------------
CREATE TABLE retro_outcome (
  id                   INTEGER PRIMARY KEY AUTOINCREMENT,
  component_root       TEXT NOT NULL,
  loop_slug            TEXT NOT NULL,
  terminal_key         TEXT NOT NULL,
  outcome_kind         TEXT NOT NULL CHECK (length(outcome_kind) <= 48),
  blueprint_id         TEXT NOT NULL DEFAULT '',
  blueprint_version    INTEGER CHECK (blueprint_version IS NULL OR blueprint_version >= 0),
                         -- NULL: pin unreadable
  started_by_kind      TEXT NOT NULL DEFAULT '',      -- the loop's started_by word ('', 'trigger', 'event', …)
  attended             INTEGER CHECK (attended IS NULL OR attended IN (0,1)),
                         -- NULL is the third state, never false
  goal_id              TEXT NOT NULL DEFAULT '',
  goal_version         INTEGER CHECK (goal_version IS NULL OR goal_version >= 0),
  wall_ms             INTEGER CHECK (wall_ms IS NULL OR wall_ms >= 0),
  cost_usd_floor       REAL,                          -- the floor, never a total (P32)
  cost_steps          INTEGER CHECK (cost_steps IS NULL OR cost_steps >= 0),
  cost_partial_steps  INTEGER CHECK (cost_partial_steps IS NULL OR cost_partial_steps >= 0),
  cost_unknown_steps  INTEGER CHECK (cost_unknown_steps IS NULL OR cost_unknown_steps >= 0),
  tasks_done          INTEGER CHECK (tasks_done IS NULL OR tasks_done >= 0),
  tasks_failed        INTEGER CHECK (tasks_failed IS NULL OR tasks_failed >= 0),
  retries             INTEGER CHECK (retries IS NULL OR retries >= 0),
  review_rounds       INTEGER CHECK (review_rounds IS NULL OR review_rounds >= 0),
  security_rounds     INTEGER CHECK (security_rounds IS NULL OR security_rounds >= 0),
  qa_rounds           INTEGER CHECK (qa_rounds IS NULL OR qa_rounds >= 0),
  total_failed_gates  INTEGER CHECK (total_failed_gates IS NULL OR total_failed_gates >= 0),
  splits_done         INTEGER CHECK (splits_done IS NULL OR splits_done >= 0),
  escalations         INTEGER CHECK (escalations IS NULL OR escalations >= 0),
  unverified_steps    INTEGER CHECK (unverified_steps IS NULL OR unverified_steps >= 0),
  verified_pass       INTEGER CHECK (verified_pass IS NULL OR verified_pass >= 0),
  verified_fail       INTEGER CHECK (verified_fail IS NULL OR verified_fail >= 0),
  complete             INTEGER NOT NULL DEFAULT 1 CHECK (complete IN (0,1)),
  source               TEXT NOT NULL DEFAULT 'recorder' CHECK (source IN ('recorder','backfill')),
  recorded_at          TEXT NOT NULL,
  UNIQUE (component_root, loop_slug, terminal_key)
);
CREATE INDEX idx_retro_outcome_kind    ON retro_outcome (component_root, outcome_kind);
CREATE INDEX idx_retro_outcome_blueprint ON retro_outcome (blueprint_id, recorded_at);
CREATE INDEX idx_retro_outcome_recorded ON retro_outcome (recorded_at);

CREATE TRIGGER retro_outcome_no_update BEFORE UPDATE ON retro_outcome
BEGIN
  SELECT RAISE(ABORT, 'retro facts are append-only');
END;
CREATE TRIGGER retro_outcome_no_delete BEFORE DELETE ON retro_outcome
BEGIN
  SELECT RAISE(ABORT, 'retro facts are append-only');
END;

-- ---------------------------------------------------------------------------
-- retro_agent — the per-agent rows of one outcome. Append-only. Grouping is
-- by slug; agent_slug_known=0 labels a legacy name that resolved to no slug
-- (S43 migrated references; old step rows may still carry names).
-- ---------------------------------------------------------------------------
CREATE TABLE retro_agent (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  outcome_id          INTEGER NOT NULL REFERENCES retro_outcome (id),
  agent               TEXT NOT NULL,
  agent_slug_known    INTEGER NOT NULL DEFAULT 1 CHECK (agent_slug_known IN (0,1)),
  steps               INTEGER CHECK (steps IS NULL OR steps >= 0),
  steps_done          INTEGER CHECK (steps_done IS NULL OR steps_done >= 0),
  steps_rejected      INTEGER CHECK (steps_rejected IS NULL OR steps_rejected >= 0),
  steps_blocked       INTEGER CHECK (steps_blocked IS NULL OR steps_blocked >= 0),
  verify_pass         INTEGER CHECK (verify_pass IS NULL OR verify_pass >= 0),
  verify_fail         INTEGER CHECK (verify_fail IS NULL OR verify_fail >= 0),
  verify_unverified   INTEGER CHECK (verify_unverified IS NULL OR verify_unverified >= 0),
  cost_usd_floor      REAL CHECK (cost_usd_floor IS NULL OR cost_usd_floor >= 0),
  cost_partial_steps  INTEGER CHECK (cost_partial_steps IS NULL OR cost_partial_steps >= 0),
  cost_unknown_steps  INTEGER CHECK (cost_unknown_steps IS NULL OR cost_unknown_steps >= 0),
  agent_ms            INTEGER CHECK (agent_ms IS NULL OR agent_ms >= 0),
  UNIQUE (outcome_id, agent)
);
CREATE INDEX idx_retro_agent ON retro_agent (agent);

CREATE TRIGGER retro_agent_no_update BEFORE UPDATE ON retro_agent
BEGIN
  SELECT RAISE(ABORT, 'retro facts are append-only');
END;
CREATE TRIGGER retro_agent_no_delete BEFORE DELETE ON retro_agent
BEGIN
  SELECT RAISE(ABORT, 'retro facts are append-only');
END;

-- ---------------------------------------------------------------------------
-- retro_learning — the link row: which outcome's milestone filed which
-- learning. It is the lock reconcile uses (RT4's idempotency), insert-only.
-- learning_id carries no FK: an S52 retraction stamps the learning, it never
-- rewrites this fact of what was filed.
-- ---------------------------------------------------------------------------
CREATE TABLE retro_learning (
  outcome_id   INTEGER PRIMARY KEY REFERENCES retro_outcome (id),
  learning_id  INTEGER NOT NULL,
  written_at   TEXT NOT NULL
);

CREATE TRIGGER retro_learning_no_update BEFORE UPDATE ON retro_learning
BEGIN
  SELECT RAISE(ABORT, 'retro facts are append-only');
END;
CREATE TRIGGER retro_learning_no_delete BEFORE DELETE ON retro_learning
BEGIN
  SELECT RAISE(ABORT, 'retro facts are append-only');
END;

-- ---------------------------------------------------------------------------
-- retro_template — the mutable half (rule 6: a body renders from this ROW at
-- render time, never from code). The seed engine upserts the system rows;
-- origin separates a person's edit from the seeded bytes.
-- ---------------------------------------------------------------------------
CREATE TABLE retro_template (
  kind       TEXT PRIMARY KEY,
  version    INTEGER NOT NULL,
  template   TEXT NOT NULL,
  origin     TEXT NOT NULL DEFAULT 'system' CHECK (origin IN ('system','user')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

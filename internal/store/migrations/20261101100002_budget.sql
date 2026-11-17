-- S55 LC7/LC8/LC11/LC13/T6 · the budget records: a dedicated table (not the
-- cost_cap policy stub) plus its append-only audit, and the adoption of
-- epic.budget_usd as one migrated budget row.
--
-- A budget is a running aggregate (spend so far across many loops and days)
-- compared against several dimensions at once, which is why it is its own
-- table and not a policy row (LC7): the policy engine decides a REQUEST at a
-- call site and carries no spend state. Scopes epic and loop are not policy
-- target kinds; a cap RAISE is one audited update, not a remove-and-add.
--
-- The migration adds two NEW tables (none is rebuilt, so no copy/FK guard is
-- needed for them) and then backfills ONE budget row per epic that has a
-- budget_usd, in the SAME transaction as one created event row per backfilled
-- budget. The guard proves the row counts match. The epic.budget_usd column
-- is NOT touched here (it is not dropped — that is a rebuild S55 does not
-- need; a comment marks it dead at T8, when the write path moves to the
-- budget row).

-- ---------------------------------------------------------------------------
-- budget — one cap per (scope, subject, window); tightening several
-- dimensions is one row. A NULL dimension is "no cap on that dimension";
-- at least one must be set (a row with no cap is a no-op, refused). Zero is a
-- real cap (nothing may be spent), so the check is >= 0, never > 0.
-- ---------------------------------------------------------------------------
CREATE TABLE budget (
  id             TEXT PRIMARY KEY,            -- 'bud-<id>'
  scope          TEXT NOT NULL CHECK (scope IN ('workspace','component','epic','loop')),
  subject_id     TEXT NOT NULL,              -- 'workspace' / a component id / an epic id / a loop id
  window         TEXT NOT NULL CHECK (window IN ('lifetime','day')),
  usd_cap        REAL    CHECK (usd_cap IS NULL OR usd_cap >= 0),
  tokens_cap     INTEGER CHECK (tokens_cap IS NULL OR tokens_cap >= 0),
  wall_ms_cap    INTEGER CHECK (wall_ms_cap IS NULL OR wall_ms_cap >= 0),
  steps_cap      INTEGER CHECK (steps_cap IS NULL OR steps_cap >= 0),
  unknown_policy TEXT NOT NULL DEFAULT 'proceed' CHECK (unknown_policy IN ('proceed','stop')),
  warn_pct       INTEGER CHECK (warn_pct IS NULL OR warn_pct BETWEEN 1 AND 99),
  enabled        INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  origin         TEXT NOT NULL DEFAULT 'user' CHECK (origin IN ('user','migrated')),
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  -- at least one dimension set: a cap with no dimension is a no-op, refused.
  CHECK (usd_cap IS NOT NULL OR tokens_cap IS NOT NULL OR wall_ms_cap IS NOT NULL OR steps_cap IS NOT NULL),
  -- a workspace budget names 'workspace' as its subject; a non-workscope names a real id.
  CHECK (scope <> 'workspace' OR subject_id = 'workspace'),
  UNIQUE (scope, subject_id, window)
);

-- ---------------------------------------------------------------------------
-- budget_event — the append-only audit of a budget's life (P3). created |
-- changed | removed | raised | hit | warned. Fixed numeric columns only; the
-- old/new values and the spend floor are numbers, never user text. A row that
-- does not change the budget (a no-op update, a read) writes nothing.
-- ---------------------------------------------------------------------------
CREATE TABLE budget_event (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  budget_id   TEXT NOT NULL,
  at          TEXT NOT NULL,
  kind        TEXT NOT NULL CHECK (kind IN ('created','changed','removed','raised','hit','warned')),
  actor_kind  TEXT NOT NULL CHECK (actor_kind IN ('user','cli','system')),
  actor_id    TEXT NOT NULL DEFAULT '',
  loop_id     TEXT NOT NULL DEFAULT '',
  dimension   TEXT NOT NULL DEFAULT '',
  old_value   REAL,
  new_value   REAL,
  spend_floor REAL,
  exact       INTEGER
);
CREATE INDEX idx_budget_event_budget ON budget_event (budget_id, id);

-- Append-only by trigger (P3): a budget's history cannot be edited or deleted.
CREATE TRIGGER budget_event_no_update BEFORE UPDATE ON budget_event
BEGIN
  SELECT RAISE(ABORT, 'budget_event is append-only');
END;
CREATE TRIGGER budget_event_no_delete BEFORE DELETE ON budget_event
BEGIN
  SELECT RAISE(ABORT, 'budget_event is append-only');
END;

-- ---------------------------------------------------------------------------
-- LC13 — adopt epic.budget_usd. One migrated budget row (scope='epic',
-- window='lifetime', origin='migrated') per epic that has a budget_usd; the
-- column is left in place (dead from T8, not dropped — dropping it is a
-- rebuild S55 does not need). Each backfilled budget earns one 'created'
-- event row so its audit is complete from the first read.
-- ---------------------------------------------------------------------------
INSERT INTO budget (id, scope, subject_id, window, usd_cap, origin,
                    unknown_policy, enabled, created_at, updated_at)
  SELECT 'bud-' || e.id, 'epic', e.id, 'lifetime', e.budget_usd, 'migrated',
         'proceed', 1, e.created_at, e.updated_at
  FROM epic e
  WHERE e.budget_usd IS NOT NULL;

INSERT INTO budget_event (budget_id, at, kind, actor_kind, actor_id,
                          dimension, new_value)
  SELECT 'bud-' || e.id, e.updated_at, 'created', 'system', 'migration',
         'usd', e.budget_usd
  FROM epic e
  WHERE e.budget_usd IS NOT NULL;

-- Guard: every epic with a budget_usd got exactly one budget row and one
-- created event, and no epic without a budget got one. A mismatch is a
-- corruption the migration must not ship.
CREATE TABLE zz_s55_budget_guard (
  moved  INTEGER,
  total  INTEGER,
  events INTEGER,
  CHECK (moved = total),
  CHECK (events = total)
);
INSERT INTO zz_s55_budget_guard (moved, total, events)
  SELECT (SELECT count(*) FROM budget WHERE origin = 'migrated'),
         (SELECT count(*) FROM epic WHERE budget_usd IS NOT NULL),
         (SELECT count(*) FROM budget_event WHERE actor_id = 'migration');
DROP TABLE zz_s55_budget_guard;

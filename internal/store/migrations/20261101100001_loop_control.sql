-- S55 LC2/LC4/LC10/LC15/T2 · the loop-control tables: the append-only audit of
-- every changing stop/resume/cancel/raise and the inert request desk.
--
-- These are NEW tables (none is rebuilt), so the migration is plain: create the
-- two tables, their indexes, and the append-only triggers. It carries no
-- copy/foreign-key guard because nothing it adds is a rebuild of an existing
-- table and neither references another S55 table — the loop_id is a plain TEXT
-- (P3: the audit outlives the loop row, so there is no FK to a loop that could
-- be archived or deleted). The reason text lives in its own bounded column,
-- never in a summary (SEC-033); detail is JSON of enums/numbers only, validated
-- against a closed key set in Go.
--
-- The loop table is NOT altered (LC1: no new status, no CHECK on loop.status).
-- A cap-hit is a blocked loop with BlockedReason{Kind:'cap-hit'}; a stop is a
-- waiting loop; a cancel ends aborted through the engine's one endStatus
-- writer — none of that is a column here.

-- ---------------------------------------------------------------------------
-- loop_control_event — one row per CHANGING control act (LC10). An act that
-- does not change state writes no row; the compare-and-set in
-- store.ParkLoopStopped is the gate. Append-only (P3): a person's stop is a
-- fact that cannot be edited or un-happened.
-- ---------------------------------------------------------------------------
CREATE TABLE loop_control_event (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_id     TEXT NOT NULL,                 -- no FK: history outlives the loop row (P3)
  at          TEXT NOT NULL,
  kind        TEXT NOT NULL CHECK (kind IN ('stop','resume','cancel','raise','cap_hit','watchdog','request','dismiss')),
  actor_kind  TEXT NOT NULL CHECK (actor_kind IN ('user','cli','system','agent')),
  actor_id    TEXT NOT NULL DEFAULT '',      -- peer uid / 'watchdog' / 'budget' / agent slug (never user text)
  step_id     INTEGER,
  rule_id     TEXT NOT NULL DEFAULT '',      -- watchdog rule or budget id, ids only
  reason      TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 512),   -- person-authored, own column, never in a summary
  detail      TEXT NOT NULL DEFAULT '{}'     -- JSON of enums/numbers only (closed keys, validated in Go)
);
CREATE INDEX idx_loop_control_event_loop ON loop_control_event (loop_id, id);

-- Append-only by trigger (P3): the audit is the record of what a person did;
-- a rewrite or a delete of it is the corruption a second truth cannot heal.
CREATE TRIGGER loop_control_event_no_update BEFORE UPDATE ON loop_control_event
BEGIN
  SELECT RAISE(ABORT, 'loop_control_event is append-only');
END;
CREATE TRIGGER loop_control_event_no_delete BEFORE DELETE ON loop_control_event
BEGIN
  SELECT RAISE(ABORT, 'loop_control_event is append-only');
END;

-- ---------------------------------------------------------------------------
-- loop_control_request — the inert request desk (LC15). An agent may file a
-- request that a person stop a loop; NOTHING halts until a person acts. The
-- row is created `open` and moves exactly once to `honored` or `dismissed`.
-- ---------------------------------------------------------------------------
CREATE TABLE loop_control_request (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_id      TEXT NOT NULL,
  requested_at TEXT NOT NULL,
  by_agent     TEXT NOT NULL,                 -- slug stamped by the daemon (S43), never from params
  by_step_id   INTEGER,
  reason       TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 512),
  status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','honored','dismissed')),
  decided_at   TEXT,
  decided_by   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_loop_control_request_open ON loop_control_request (loop_id, status);

-- A request moves open -> honored|dismissed exactly once, and is never
-- rewritten or deleted after that (it is a fact: this agent asked, this
-- person decided). A raw UPDATE that leaves status 'open' (the create) or
-- re-opens a decided row is refused; any DELETE is refused.
CREATE TRIGGER loop_control_request_no_reopen BEFORE UPDATE ON loop_control_request
WHEN NEW.status = 'open'
BEGIN
  SELECT RAISE(ABORT, 'loop_control_request cannot be re-opened');
END;
CREATE TRIGGER loop_control_request_no_update_once AFTER UPDATE OF status ON loop_control_request
WHEN OLD.status <> 'open'
BEGIN
  SELECT RAISE(ABORT, 'loop_control_request is decided; it cannot change again');
END;
CREATE TRIGGER loop_control_request_no_delete BEFORE DELETE ON loop_control_request
BEGIN
  SELECT RAISE(ABORT, 'loop_control_request is append-only once decided');
END;

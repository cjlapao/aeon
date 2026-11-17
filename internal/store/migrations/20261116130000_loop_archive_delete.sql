-- Archiving and deleting a loop, without losing what was already counted.
--
-- loop_archived hides a loop: the loop and everything under it stay exactly as they were; lists leave it out unless
-- asked. It is its own table (not a column) so the loop row and every reader of it stay untouched.
CREATE TABLE loop_archived (
  loop_id     TEXT PRIMARY KEY REFERENCES loop(id) ON DELETE CASCADE,
  archived_at TEXT NOT NULL,
  note        TEXT NOT NULL DEFAULT ''
);

-- loop_tombstone is what a deleted loop leaves behind: who it was and the totals it had earned. The component's
-- telemetry rollups are never touched by a deletion; this row is the loop's own line in that ledger.
CREATE TABLE loop_tombstone (
  loop_id          TEXT PRIMARY KEY,
  slug             TEXT NOT NULL,
  component_root   TEXT NOT NULL,
  title            TEXT NOT NULL,
  goal             TEXT NOT NULL DEFAULT '',
  status           TEXT NOT NULL DEFAULT '',
  started_at       TEXT NOT NULL DEFAULT '',
  deleted_at       TEXT NOT NULL,
  deleted_by       TEXT NOT NULL DEFAULT '',
  note             TEXT NOT NULL DEFAULT '',
  steps            INTEGER NOT NULL DEFAULT 0,
  turns            INTEGER NOT NULL DEFAULT 0,
  tool_calls       INTEGER NOT NULL DEFAULT 0,
  input_tokens     INTEGER NOT NULL DEFAULT 0,
  output_tokens    INTEGER NOT NULL DEFAULT 0,
  wall_ms          INTEGER NOT NULL DEFAULT 0,
  cost_usd         REAL,
  cost_state       TEXT NOT NULL DEFAULT 'unknown'
);
CREATE INDEX idx_loop_tombstone_component ON loop_tombstone (component_root, deleted_at);

-- detached_link remembers what a task, roadmap item or queue item was linked to when its loop was deleted. The row
-- itself stays (work is never lost with a loop); only its link goes, and this says so, with the cost the task had
-- already spent so a task's cost does not shrink with its loop.
CREATE TABLE detached_link (
  entity_kind    TEXT NOT NULL CHECK (entity_kind IN ('task','roadmap','queue')),
  entity_id      TEXT NOT NULL,
  loop_slug      TEXT NOT NULL,
  component_root TEXT NOT NULL,
  detached_at    TEXT NOT NULL,
  cost_usd       REAL,
  cost_floor     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (entity_kind, entity_id)
);

-- S55 LC17/LC18/T9 · the watchdog: rule records and their append-only audit.
--
-- A watchdog rule turns "a running loop that is not progressing" into a
-- notification, a stop (attended) or a block (unattended). The rules are
-- records an operator can list, edit and disable, shipped as a seeded default
-- set (the S43-extra seed engine). The service that evaluates them lives in
-- internal/daemon; this migration holds the rows and their audit only.
--
-- The signal and action vocabularies are CHECKed at the database and owned by
-- internal/watchdog (the closed sets watchdog.vocabulary reads) — a Go mirror
-- would be the second truth the slice forbids (the queue/roadmap pattern).
-- No table is rebuilt, so no copy/FK guard is needed; the rule's subject_id
-- is a plain TEXT (a workspace or a component id), not an FK, because the
-- audit must outlive the subject.

-- ---------------------------------------------------------------------------
-- watchdog_rule — one row per (scope, subject, signal, action, threshold).
-- A root's rule covers its tree (S44-extra: a root's telemetry is the subtree
-- total, CT7). The unit of threshold is the signal's (watchdog.vocabulary):
-- a count for same-position/repeat-block-reason, a millisecond count for
-- no-activity and step-over-deadline.
-- ---------------------------------------------------------------------------
CREATE TABLE watchdog_rule (
  id         TEXT PRIMARY KEY,
  scope      TEXT NOT NULL CHECK (scope IN ('workspace','component')),
  subject_id TEXT NOT NULL,                     -- 'workspace' or a component id (a root's rule covers its tree)
  signal     TEXT NOT NULL CHECK (signal IN ('no-activity','same-position','repeat-block-reason','step-over-deadline')),
  threshold  INTEGER NOT NULL CHECK (threshold >= 0),   -- unit is the signal's (watchdog.vocabulary)
  action     TEXT NOT NULL CHECK (action IN ('notify','stop','block')),
  enabled    INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  origin     TEXT NOT NULL DEFAULT 'user' CHECK (origin IN ('system','user')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (scope, subject_id, signal, action, threshold)
);
CREATE INDEX idx_watchdog_rule_scope ON watchdog_rule (scope, subject_id);

-- ---------------------------------------------------------------------------
-- watchdog_event — the append-only audit of a rule's life (P3). create |
-- change | remove | disable; ids only, no user text. A firing of a rule
-- (the notification / stop / block itself) is NOT a row here: it rides
-- loop_control_event{kind:'watchdog'} (T2), so a fire is never recorded
-- twice. A rule that changes nothing (a no-op) writes no row.
-- ---------------------------------------------------------------------------
CREATE TABLE watchdog_event (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  rule_id     TEXT NOT NULL,
  at          TEXT NOT NULL,
  kind        TEXT NOT NULL CHECK (kind IN ('created','changed','removed','disabled')),
  actor_kind  TEXT NOT NULL CHECK (actor_kind IN ('user','cli','system')),
  actor_id    TEXT NOT NULL DEFAULT '',
  old_value   INTEGER,
  new_value   INTEGER
);
CREATE INDEX idx_watchdog_event_rule ON watchdog_event (rule_id, id);

-- Append-only by trigger (P3): a rule's history cannot be edited or deleted.
CREATE TRIGGER watchdog_event_no_update BEFORE UPDATE ON watchdog_event
BEGIN
  SELECT RAISE(ABORT, 'watchdog_event is append-only');
END;
CREATE TRIGGER watchdog_event_no_delete BEFORE DELETE ON watchdog_event
BEGIN
  SELECT RAISE(ABORT, 'watchdog_event is append-only');
END;

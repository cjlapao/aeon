-- S44 T1: the daemon-wide settings store — key/value TEXT with typing enforced by the Go write boundary (config.Store, CS3), global scope only on purpose (CS6: a scope column nothing fills is the TL5 mistake), and an append-only audit table (CS8) whose UPDATE/DELETE are refused by trigger. No credential is ever admitted: the registry refuses credential-shaped key names (CS5), so the audit table may hold values in clear.
CREATE TABLE setting (
  key        TEXT PRIMARY KEY
             CHECK (length(key) BETWEEN 3 AND 128
                    AND key NOT GLOB '*[^A-Za-z0-9.]*'
                    AND key LIKE '%.%' AND key NOT LIKE '.%' AND key NOT LIKE '%.' AND key NOT LIKE '%..%'),
  value      TEXT NOT NULL CHECK (length(value) <= 4096),
  updated_at TEXT NOT NULL,
  updated_by TEXT NOT NULL CHECK (length(updated_by) BETWEEN 1 AND 256)
);

CREATE TABLE setting_event (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  key         TEXT NOT NULL,
  op          TEXT NOT NULL CHECK (op IN ('set','unset')),
  old_value   TEXT,                       -- NULL = there was no stored row (the default was in force)
  new_value   TEXT,                       -- NULL on 'unset'
  sensitive   INTEGER NOT NULL CHECK (sensitive IN (0,1)),
  shadowed_by TEXT CHECK (shadowed_by IS NULL OR shadowed_by = 'env'),
  actor       TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 256),
  occurred_at TEXT NOT NULL,
  CHECK ((op = 'set' AND new_value IS NOT NULL) OR (op = 'unset' AND new_value IS NULL))
);
CREATE INDEX idx_setting_event_key ON setting_event (key, id);

CREATE TRIGGER setting_event_no_update BEFORE UPDATE ON setting_event
BEGIN SELECT RAISE(ABORT, 'setting_event is append-only: an audit row is never rewritten (S44 CS8)'); END;
CREATE TRIGGER setting_event_no_delete BEFORE DELETE ON setting_event
BEGIN SELECT RAISE(ABORT, 'setting_event is append-only: an audit row is never deleted (S44 CS8)'); END;

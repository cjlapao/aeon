-- S43-extra T2: the seed ledger (what the seed process applied, per set and record) and its append-only audit. The domain rows live in their own tables with origin='system'; the ledger is the engine's memory of versions and hashes so status can say what is out of date, removed or diverged. seed_event is append-only by trigger (the setting_event precedent).
CREATE TABLE seed_ledger (
  set_id          TEXT NOT NULL CHECK (length(set_id) BETWEEN 3 AND 64),
  record_key      TEXT NOT NULL CHECK (length(record_key) BETWEEN 1 AND 128),
  applied_version INTEGER NOT NULL CHECK (applied_version >= 1),
  applied_hash    TEXT NOT NULL CHECK (length(applied_hash) = 64),
  state           TEXT NOT NULL CHECK (state IN ('applied','removed','diverged','retired','failed')),
  applied_at      TEXT NOT NULL,
  applied_by      TEXT NOT NULL CHECK (length(applied_by) BETWEEN 1 AND 256),
  PRIMARY KEY (set_id, record_key)
);
CREATE TABLE seed_event (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  set_id       TEXT NOT NULL,
  record_key   TEXT NOT NULL,
  op           TEXT NOT NULL CHECK (op IN ('apply','update','skip','restore','retire','adopt')),
  from_version INTEGER,
  to_version   INTEGER,
  reason       TEXT NOT NULL CHECK (length(reason) <= 512),
  actor        TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 256),
  occurred_at  TEXT NOT NULL
);
CREATE INDEX idx_seed_event_set ON seed_event (set_id, record_key, id);
CREATE TRIGGER seed_event_no_update BEFORE UPDATE ON seed_event
BEGIN SELECT RAISE(ABORT, 'seed_event is append-only (S43-extra SD3)'); END;
CREATE TRIGGER seed_event_no_delete BEFORE DELETE ON seed_event
BEGIN SELECT RAISE(ABORT, 'seed_event is append-only (S43-extra SD3)'); END;

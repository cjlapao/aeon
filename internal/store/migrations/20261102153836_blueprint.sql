-- S48 T1: blueprints as records (design BP1). content is the YAML text VERBATIM (comments and layout preserved, never re-serialized) and is the source of truth; blueprint_revision is the append-only history (rev numbering per name never resets); blueprint_ref is DERIVED from content on every write and is rebuildable; blueprint_import is the ledger of adopting the legacy state-dir files (BP3). Pinned loops keep their own content in loop_blueprint and are unaffected.
CREATE TABLE blueprint (
  name         TEXT PRIMARY KEY CHECK (length(name) BETWEEN 1 AND 64 AND name NOT GLOB '*[^a-z0-9_-]*' AND name GLOB '[a-z0-9]*'),
  version      INTEGER NOT NULL CHECK (version >= 1),
  content      TEXT NOT NULL CHECK (length(content) BETWEEN 1 AND 1048576),
  content_hash TEXT NOT NULL CHECK (length(content_hash) = 64),
  origin       TEXT NOT NULL CHECK (origin IN ('system','user')),
  created_at   TEXT NOT NULL, updated_at TEXT NOT NULL,
  updated_by   TEXT NOT NULL CHECK (length(updated_by) BETWEEN 1 AND 256)
);
CREATE TABLE blueprint_revision (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT NOT NULL,
  rev          INTEGER NOT NULL CHECK (rev >= 1),
  version      INTEGER NOT NULL CHECK (version >= 1),
  content      TEXT NOT NULL CHECK (length(content) BETWEEN 1 AND 1048576),
  content_hash TEXT NOT NULL CHECK (length(content_hash) = 64),
  op           TEXT NOT NULL CHECK (op IN ('create','save','duplicate','revert','seed','update','restore','adopt')),
  note         TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 256),
  actor_kind   TEXT NOT NULL CHECK (actor_kind IN ('user','cli','system')),
  actor_id     TEXT NOT NULL CHECK (length(actor_id) BETWEEN 1 AND 256),
  occurred_at  TEXT NOT NULL,
  UNIQUE (name, rev)
);
CREATE INDEX idx_blueprint_revision_name ON blueprint_revision (name, rev);
CREATE TRIGGER blueprint_revision_no_update BEFORE UPDATE ON blueprint_revision BEGIN SELECT RAISE(ABORT, 'blueprint_revision is append-only (S48 BP7)'); END;
CREATE TRIGGER blueprint_revision_no_delete BEFORE DELETE ON blueprint_revision BEGIN SELECT RAISE(ABORT, 'blueprint_revision is append-only (S48 BP7)'); END;
CREATE TABLE blueprint_ref (
  name   TEXT NOT NULL,
  kind   TEXT NOT NULL CHECK (kind IN ('agent','converse','skill','action','resolver')),
  target TEXT NOT NULL CHECK (length(target) BETWEEN 1 AND 128),
  PRIMARY KEY (name, kind, target)
);
CREATE INDEX idx_blueprint_ref_target ON blueprint_ref (kind, target);
CREATE TABLE blueprint_import (
  id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL, sha256 TEXT NOT NULL CHECK (length(sha256) = 64), name TEXT NOT NULL,
  result TEXT NOT NULL CHECK (result IN ('imported','refused')), detail TEXT NOT NULL CHECK (length(detail) <= 4096), imported_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_blueprint_import_hash ON blueprint_import (sha256) WHERE result = 'imported';

-- S62 T3: the backup_run ledger (design BK10) — one append-only row per backup, verify, prune, restore-plan or export. It is the source of "last success", "last verified" and "age" (backup.status computes them; nothing else may). History outlives files (P3): file_name is a *reference*, never a foreign key — a row whose file was pruned stays, and a file whose row was pruned is impossible because rows are never pruned. The closed sets mirror the Go door in backup_run.go (a CHECK is the backstop, the refusal is the sentence); the GLOB on file_name is the SQL twin of the package regex, held in lockstep by TestBackupRunFileNameGlobMatchesGoRegex.
CREATE TABLE backup_run (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  kind               TEXT NOT NULL CHECK (kind IN ('backup','verify','prune','restore-plan','export')),
  reason             TEXT NOT NULL CHECK (reason IN ('manual','scheduled','pre-migration','post-restore-check')),
  actor_kind         TEXT NOT NULL CHECK (actor_kind IN ('user','daemon','cli')),
  started_at         TEXT NOT NULL,
  finished_at        TEXT NOT NULL,
  outcome            TEXT NOT NULL CHECK (outcome IN ('ok','failed','refused','skipped')),
  error_kind         TEXT CHECK (error_kind IS NULL OR error_kind IN ('disk-full','dir-unwritable','busy','lock-held','integrity','fk','checksum','newer-schema','io','cancelled','unknown')),
  detail             TEXT NOT NULL DEFAULT '' CHECK (length(detail) <= 512),
  file_name          TEXT CHECK (file_name IS NULL OR file_name GLOB 'aeon-state-????????T??????Z-v*.aeonbak'),
  size_bytes         INTEGER CHECK (size_bytes IS NULL OR size_bytes >= 0),
  sha256             TEXT CHECK (sha256 IS NULL OR length(sha256) = 64),
  applied_migrations INTEGER CHECK (applied_migrations IS NULL OR applied_migrations >= 0),
  verify_level       TEXT CHECK (verify_level IS NULL OR verify_level IN ('written','verified','drill')),
  free_space_state   TEXT NOT NULL CHECK (free_space_state IN ('ok','low','unknown'))
);
CREATE INDEX idx_backup_run_kind_outcome ON backup_run (kind, outcome, finished_at);
CREATE TRIGGER backup_run_no_update BEFORE UPDATE ON backup_run BEGIN
  SELECT RAISE(ABORT, 'backup_run is append-only');
END;
CREATE TRIGGER backup_run_no_delete BEFORE DELETE ON backup_run BEGIN
  SELECT RAISE(ABORT, 'backup_run is append-only');
END;

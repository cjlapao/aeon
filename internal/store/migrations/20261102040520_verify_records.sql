-- S57 T3: the verification records (VG3/VG6/VG21). verify_command is the
-- record (a gate a component declares: name slug, command, requirement,
-- timeout, declared-not-enforced memory, position, enabled, origin);
-- verify_command_event is its append-only audit (one row per changing write,
-- same transaction, revision numbers never reset, no FK to the record —
-- history outlives the record, P3); verify_run + verify_run_result are the
-- append-only evidence (a run row is visible only finished, with its
-- results, written in one transaction — VG6, and the command text is
-- SNAPSHOTTED on the result so history survives a later edit or archive);
-- verify_template is the inert system template table T10's seed fills
-- (VG21: a template does nothing until a person applies it; empty until
-- then, and no verify_command row is ever seeded). All new tables; nothing
-- existing is touched, so no rebuild and no guard triggers.
CREATE TABLE verify_command (
  id           TEXT PRIMARY KEY,
  component_id TEXT NOT NULL REFERENCES component(id),
  -- name is a validated slug (the event summary may quote it — SEC-033);
  -- unique per component among the non-archived (the partial index below).
  name         TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64 AND name NOT GLOB '*[^a-z0-9-]*'),
  -- a gate command is authority: bounded at the table (VG3's 4096, far under
  -- the bash tool's 64 KiB boundary), NUL-free by construction (a Go-side
  -- refusal — a C-string cannot carry one).
  command      TEXT NOT NULL CHECK (length(command) BETWEEN 1 AND 4096),
  requirement  TEXT NOT NULL CHECK (requirement IN ('required','advisory')),
  timeout_s    INTEGER NOT NULL CHECK (timeout_s BETWEEN 1 AND 600),
  -- memory_mb is DECLARED, not enforced in v1 (VG3): every view says so;
  -- the CHECK only keeps the declaration sane (a zero or negative memory is
  -- not a declaration).
  memory_mb    INTEGER CHECK (memory_mb IS NULL OR memory_mb > 0),
  position     INTEGER NOT NULL DEFAULT 0,
  enabled      INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  origin       TEXT NOT NULL CHECK (origin IN ('user','template')),
  template_id  TEXT,
  archived_at  TEXT,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
-- one live gate per (component, name); an archived row frees its name.
CREATE UNIQUE INDEX uq_verify_command_component_name
  ON verify_command (component_id, name) WHERE archived_at IS NULL;
CREATE INDEX idx_verify_command_component_position ON verify_command (component_id, position);

-- The audit: append-only by trigger, no FK to the command (P3 — the record
-- may be archived and re-archived forever; the revision numbers never
-- reset, and a deleted record would orphan its history).
CREATE TABLE verify_command_event (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  command_id   TEXT NOT NULL,
  component_id TEXT NOT NULL,
  revision     INTEGER NOT NULL,
  op           TEXT NOT NULL CHECK (op IN ('add','update','archive','restore','apply_template')),
  actor_kind   TEXT NOT NULL CHECK (actor_kind IN ('user','cli','agent')),
  actor_id     TEXT NOT NULL,
  -- a fixed-template sentence from ids, enums and the validated slug —
  -- never free user text (SEC-033).
  summary      TEXT NOT NULL,
  before_json  TEXT CHECK (before_json IS NULL OR json_valid(before_json)),
  after_json   TEXT CHECK (after_json IS NULL OR json_valid(after_json)),
  at           TEXT NOT NULL
);
CREATE UNIQUE INDEX uq_verify_command_event_revision ON verify_command_event (command_id, revision);
CREATE INDEX idx_verify_command_event_component ON verify_command_event (component_id, id);
CREATE TRIGGER verify_command_event_no_update BEFORE UPDATE ON verify_command_event
BEGIN SELECT RAISE(ABORT, 'verify_command_event is append-only'); END;
CREATE TRIGGER verify_command_event_no_delete BEFORE DELETE ON verify_command_event
BEGIN SELECT RAISE(ABORT, 'verify_command_event is append-only'); END;

-- The evidence (VG6). A run is visible only finished: the row and its
-- results land in one transaction, and both tables are append-only by
-- trigger. loop_slug/component_root are keyed without FKs (the loop rows
-- move out on archive — store/archive.go — and the evidence outlives them,
-- like the autonomy pin does); component_id is the node the run happened
-- on (the gates' own source node, which may differ from the root).
CREATE TABLE verify_run (
  id             TEXT PRIMARY KEY,
  loop_slug      TEXT NOT NULL DEFAULT '',
  component_root TEXT NOT NULL,
  component_id   TEXT NOT NULL,
  task           TEXT NOT NULL DEFAULT '',
  position       INTEGER NOT NULL DEFAULT 0,
  attempt        INTEGER NOT NULL DEFAULT 0,
  head_sha       TEXT NOT NULL DEFAULT '',
  trigger        TEXT NOT NULL CHECK (trigger IN ('loop','manual')),
  actor_kind     TEXT NOT NULL CHECK (actor_kind IN ('user','cli','agent','system')),
  actor_id       TEXT NOT NULL,
  started_at     TEXT NOT NULL,
  ended_at       TEXT,
  state          TEXT NOT NULL CHECK (state IN ('pass','fail','unverified')),
  reason         TEXT NOT NULL DEFAULT '',
  summary        TEXT NOT NULL DEFAULT '',
  digest         TEXT NOT NULL DEFAULT ''
);
-- a loop re-dispatch of the same (loop, root, position, attempt) is the SAME
-- run, not a new one; manual runs (loop_slug '') are exempt — they are
-- keyed by identity, not by a slot.
CREATE UNIQUE INDEX uq_verify_run_loop
  ON verify_run (loop_slug, component_root, position, attempt) WHERE trigger = 'loop';
CREATE INDEX idx_verify_run_root_ended ON verify_run (component_root, ended_at);
CREATE INDEX idx_verify_run_loop_slug ON verify_run (loop_slug);
CREATE INDEX idx_verify_run_head_sha ON verify_run (head_sha);
CREATE TRIGGER verify_run_no_update BEFORE UPDATE ON verify_run
BEGIN SELECT RAISE(ABORT, 'verify_run is append-only'); END;
CREATE TRIGGER verify_run_no_delete BEFORE DELETE ON verify_run
BEGIN SELECT RAISE(ABORT, 'verify_run is append-only'); END;

-- One row per gate the run executed (or skipped). command is the
-- SNAPSHOT: the command text as it read when the gate ran, so a later edit
-- or archive of the gate never rewrites this row's story. tails are ≤ 2 KiB
-- each, stored only for non-pass results by the WRITER (T4), best-effort
-- redacted, never copied anywhere else (VG6).
CREATE TABLE verify_run_result (
  run_id        TEXT NOT NULL REFERENCES verify_run(id),
  seq           INTEGER NOT NULL,
  command_id    TEXT,
  name          TEXT NOT NULL,
  requirement   TEXT NOT NULL CHECK (requirement IN ('required','advisory')),
  command       TEXT NOT NULL,
  exit_code     INTEGER,
  signal        TEXT NOT NULL DEFAULT '',
  timed_out     INTEGER NOT NULL DEFAULT 0 CHECK (timed_out IN (0,1)),
  wall_ms       INTEGER NOT NULL DEFAULT 0,
  stdout_bytes  INTEGER NOT NULL DEFAULT 0,
  stderr_bytes  INTEGER NOT NULL DEFAULT 0,
  truncated     INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0,1)),
  stdout_sha256 TEXT NOT NULL DEFAULT '',
  stderr_sha256 TEXT NOT NULL DEFAULT '',
  -- the tails are the ONLY output text stored, at most 2 KiB each (VG6), and
  -- only for non-pass results (the writer's rule, T4).
  stdout_tail   TEXT NOT NULL DEFAULT '' CHECK (length(stdout_tail) <= 2048),
  stderr_tail   TEXT NOT NULL DEFAULT '' CHECK (length(stderr_tail) <= 2048),
  state         TEXT NOT NULL CHECK (state IN ('pass','fail','unverified','skipped')),
  reason        TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (run_id, seq)
);
CREATE TRIGGER verify_run_result_no_update BEFORE UPDATE ON verify_run_result
BEGIN SELECT RAISE(ABORT, 'verify_run_result is append-only'); END;
CREATE TRIGGER verify_run_result_no_delete BEFORE DELETE ON verify_run_result
BEGIN SELECT RAISE(ABORT, 'verify_run_result is append-only'); END;

-- The inert template table (VG21): system rows T10's seed fills, a person's
-- applied copy lands on verify_command as origin='template'. Empty until
-- T10; a template is a candidate, never a gate — nothing here runs.
CREATE TABLE verify_template (
  id          TEXT PRIMARY KEY,
  stack       TEXT NOT NULL CHECK (length(stack) BETWEEN 1 AND 32 AND stack NOT GLOB '*[^a-z0-9-]*'),
  name        TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64 AND name NOT GLOB '*[^a-z0-9-]*'),
  command     TEXT NOT NULL CHECK (length(command) BETWEEN 1 AND 4096),
  requirement TEXT NOT NULL CHECK (requirement IN ('required','advisory')),
  timeout_s   INTEGER NOT NULL CHECK (timeout_s BETWEEN 1 AND 600),
  origin      TEXT NOT NULL DEFAULT 'system' CHECK (origin IN ('system','user')),
  version     TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  archived_at TEXT
);
CREATE UNIQUE INDEX uq_verify_template_stack_name ON verify_template (stack, name) WHERE archived_at IS NULL;
CREATE INDEX idx_verify_template_stack ON verify_template (stack);

-- End guard (the 0019 discipline): the new FK-bearing tables left no
-- dangling reference — the CHECK makes the migration itself abort if not.
CREATE TABLE zz_s57_verify_fk_guard (
  n INTEGER CHECK (n = 0)
);
INSERT INTO zz_s57_verify_fk_guard (n)
SELECT count(*) FROM pragma_foreign_key_check();

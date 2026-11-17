-- S59 T2: the execution environment and its five companions (design EV2, EV7, EV9, EV12, EV17, EV20). `environment` is the one record a command runs in (EV2); `environment_binding` says where it applies — exactly one workspace row, at most one row per component, the two partial unique indexes being the only thing that can express that (SQLite treats NULLs as distinct, so uniqueness of the workspace row is an index, not a column constraint). `secret_ref` is a WHERE, never a WHAT (EV7): there is no column that could hold a value, and TestNoCredentialShapedColumnExists walks every table here and fails on a credential-shaped name. `step_environment` is what a step actually ran in — names only, append-only (EV9). `environment_provision` is the write-ahead lifecycle ledger, settle-once via removed_at, one live row per owner (EV12, EV14). `environment_change` is the append-only audit, one row per changing write in the same transaction, names and literals only (EV17). The legacy row is CONDITIONAL (EV20): an upgraded store keeps today's behaviour exactly, a fresh store gets nothing from this file and `host-scrubbed` from the inert seed (EV19) — shipping the permissive row to every install would defeat the point of the slice.
CREATE TABLE environment (
  id           TEXT PRIMARY KEY,
  slug         TEXT NOT NULL UNIQUE,
  name         TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  backend      TEXT NOT NULL CHECK (backend IN ('host-inherit','host-scrubbed','container','vm')),
  spec         TEXT NOT NULL CHECK (json_valid(spec)),
  origin       TEXT NOT NULL DEFAULT 'user' CHECK (origin IN ('system','user')),
  legacy       INTEGER NOT NULL DEFAULT 0 CHECK (legacy IN (0,1)),
  revision     INTEGER NOT NULL DEFAULT 1,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  archived_at  TEXT
);

CREATE TABLE environment_binding (
  scope_kind     TEXT NOT NULL CHECK (scope_kind IN ('workspace','component')),
  component_id   TEXT REFERENCES component (id),
  environment_id TEXT NOT NULL REFERENCES environment (id),
  bound_at       TEXT NOT NULL,
  CHECK ((scope_kind = 'workspace' AND component_id IS NULL) OR (scope_kind = 'component' AND component_id IS NOT NULL))
);
CREATE UNIQUE INDEX environment_binding_workspace ON environment_binding (scope_kind) WHERE scope_kind = 'workspace';
CREATE UNIQUE INDEX environment_binding_component ON environment_binding (component_id) WHERE component_id IS NOT NULL;

-- No value column, ever (EV7): `source` is an env-var NAME or an absolute file
-- PATH, `inject_as` is the child's variable NAME. A store that could hold a
-- secret value is a store whose backup, export and transcript carry one (BK, C17).
CREATE TABLE secret_ref (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  source_kind TEXT NOT NULL CHECK (source_kind IN ('daemon-env','key-file','store')),
  source      TEXT NOT NULL,
  inject_as   TEXT NOT NULL,
  origin      TEXT NOT NULL DEFAULT 'user',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  archived_at TEXT
);

-- What a step actually ran in (EV9): names only — `var_names` and `secret_refs`
-- are JSON arrays of NAMES, written by the store from the resolved set, never
-- values. Append-only by trigger: the step's own record is the only evidence of
-- what a command could see, and a widened environment must not be able to
-- rewrite it (EV2). `step_id` carries NO foreign key (P3): store/archive.go
-- moves step rows out, and the step record outlives them like history does.
-- One row per DISTINCT outcome per step — the dedupe is the store's
-- (RecordStepEnvironment), because the distinctness is over parsed JSON.
CREATE TABLE step_environment (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id          INTEGER NOT NULL,
  environment_id   TEXT,
  backend          TEXT,
  downgraded       INTEGER NOT NULL DEFAULT 0,
  downgrade_reason TEXT,
  refusal_kind     TEXT,
  refusal          TEXT,
  var_names        TEXT NOT NULL DEFAULT '[]',
  secret_refs      TEXT NOT NULL DEFAULT '[]',
  resolved_at      TEXT NOT NULL
);
CREATE INDEX step_environment_step ON step_environment (step_id);
CREATE TRIGGER step_environment_no_update BEFORE UPDATE ON step_environment BEGIN SELECT RAISE(ABORT,'step_environment is append-only'); END;
CREATE TRIGGER step_environment_no_delete BEFORE DELETE ON step_environment BEGIN SELECT RAISE(ABORT,'step_environment is append-only'); END;

-- The lifecycle ledger (EV12): the row first, then the real action; teardown
-- reverses it; settle-once via removed_at, which the partial unique index also
-- uses to hold "one live resource per owner" (EV14's services ride the same
-- ledger under owner 'service:<name>'). `environment_id` DOES carry a foreign
-- key: an environment row is never deleted (archive sets archived_at), so the
-- ledger can always name what it provisioned, and the key turns "provision an
-- environment that does not exist" into a constraint rather than a container
-- nobody asked for. `component_root` + `loop_slug` carry NONE: they are the
-- loop's identity pair, and store/archive.go:158 DELETEs loop rows — an FK here
-- would CASCADE the reconcile ledger away (or RESTRICT the archive). That is the
-- opposite of loop_worktree, which legitimately takes one: a worktree row means
-- nothing once the loop is gone, while a provision row is the record that a
-- resource EXISTS and still has to be found. Both are T0's per-table cascade
-- decisions (F9), taken with the DELETE sites read, not assumed.
CREATE TABLE environment_provision (
  id             TEXT PRIMARY KEY,
  environment_id TEXT NOT NULL REFERENCES environment (id),
  component_root TEXT NOT NULL,
  loop_slug      TEXT NOT NULL,
  backend        TEXT NOT NULL CHECK (backend IN ('host-inherit','host-scrubbed','container','vm')),
  handle         TEXT NOT NULL DEFAULT '',
  owner          TEXT NOT NULL DEFAULT 'loop',
  state          TEXT NOT NULL CHECK (state IN ('provisioning','ready','tearing_down','removed','failed')),
  created_at     TEXT NOT NULL,
  ready_at       TEXT,
  removed_at     TEXT,
  last_error     TEXT,
  -- a settled row is the only one with removed_at, and a row still
  -- provisioning has never been ready: the two facts the marks maintain, said
  -- once in SQL so a raw statement cannot leak the third state.
  CHECK (removed_at IS NULL OR state = 'removed'),
  CHECK (ready_at IS NULL OR state <> 'provisioning'),
  -- 'loop' is one value; every other owner is a service (EV14). The name shape
  -- is the store's door; this is the net.
  CHECK (owner = 'loop' OR owner GLOB 'service:*')
);
CREATE UNIQUE INDEX environment_provision_live ON environment_provision (component_root, loop_slug, owner)
  WHERE removed_at IS NULL;
CREATE INDEX idx_environment_provision_loop ON environment_provision (component_root, loop_slug, id);

-- The audit (EV17): one row in the SAME transaction as each changing write to
-- an environment, a secret reference or a binding; none for reads, refusals,
-- dry-runs or no-ops (P3). `subject_id` is the environment id, the secret
-- reference id, or — for a binding, which has no id of its own — the component
-- id, or the literal 'workspace' for the workspace binding. `revision` is the
-- environment's new revision and NULL for a subject that has no counter; `spec_after`
-- is the spec JSON (names and literals only, so no value can be in it — EV7) and
-- NULL for a binding row. Fixed-template summaries (SEC-033). No foreign key
-- anywhere: history outlives the record (P3), and an audit that disappears when
-- its subject is archived audits nothing.
CREATE TABLE environment_change (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  at           TEXT NOT NULL,
  actor_kind   TEXT NOT NULL CHECK (actor_kind IN ('user','cli','system','agent','trigger','daemon')),
  actor_id     TEXT NOT NULL,
  subject_kind TEXT NOT NULL CHECK (subject_kind IN ('environment','secret_ref','binding')),
  subject_id   TEXT NOT NULL,
  action       TEXT NOT NULL CHECK (action IN ('create','update','archive','restore','bind','unbind')),
  revision     INTEGER CHECK (revision IS NULL OR revision >= 1),
  spec_after   TEXT CHECK (spec_after IS NULL OR json_valid(spec_after)),
  summary      TEXT NOT NULL,
  -- an environment row always says which revision it landed on; a binding
  -- never has one, and a secret reference carries its own counter nowhere.
  CHECK ((subject_kind = 'environment') = (revision IS NOT NULL))
);
CREATE INDEX idx_environment_change_subject ON environment_change (subject_kind, subject_id, id);
CREATE INDEX idx_environment_change_action ON environment_change (action, id);
CREATE TRIGGER environment_change_no_update BEFORE UPDATE ON environment_change BEGIN SELECT RAISE(ABORT,'environment_change is append-only'); END;
CREATE TRIGGER environment_change_no_delete BEFORE DELETE ON environment_change BEGIN SELECT RAISE(ABORT,'environment_change is append-only'); END;

-- ---------------------------------------------------------------------------
-- The conditional legacy rows (EV20). A store that already has components or
-- loops keeps today's behaviour EXACTLY: the workspace binds `env-host-inherit`,
-- whose backend inherits everything, which is what every command saw before
-- this slice — an attended loop notices nothing. A store with neither gets
-- nothing from this file: the permissive row must never ship to a fresh install
-- (EV19 binds `host-scrubbed` there instead, through the inert seed).
--
-- The spec lists no variables because the legacy backend ignores `passthrough`:
-- it inherits EVERYTHING (EV3 — "Environment is an explicit allowlist: no").
-- `home: real`, `egress.mode: unenforced`, no limits, no services, no preview:
-- the honest shape of "nothing is enforced here" (EV1), and `legacy = 1` is what
-- `aeon doctor` and the Settings panel warn on (EV6).
--
-- The clock renders the same layout every store row uses —
-- `2006-01-02T15:04:05.000Z` (open.go's isoUTC); SQLite's %f is SS.SSS, so
-- %Y-%m-%dT%H:%M:%fZ is that layout exactly, and a row from this file is
-- indistinguishable from one the store wrote.
-- ---------------------------------------------------------------------------
INSERT INTO environment (id, slug, name, description, backend, spec, origin, legacy, revision, created_at, updated_at)
SELECT 'env-host-inherit', 'host-inherit', 'Host (inherited environment)', 'The environment every command inherited before S59: the daemon''s whole environment, nothing scrubbed, nothing enforced.', 'host-inherit',
       '{"env":{"passthrough":[],"set":{},"secrets":[]},"home":"real","limits":{},"egress":{"mode":"unenforced"},"services":[],"preview":null}',
       'system', 1, 1, strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE EXISTS (SELECT 1 FROM component) OR EXISTS (SELECT 1 FROM loop);
INSERT INTO environment_binding (scope_kind, component_id, environment_id, bound_at)
SELECT 'workspace', NULL, 'env-host-inherit', strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE EXISTS (SELECT 1 FROM environment WHERE id = 'env-host-inherit');

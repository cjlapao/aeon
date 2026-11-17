-- The variable manager: named values the app holds for processes and agents. A value is stored as ciphertext only (AES-256-GCM, the vault package's key file) and is never returned by a read of the store itself; is_secret says whether the UI may show it back, agent_visible says whether an agent's env_* tools may read it (default 0: agents never see a variable unless a person said so). project_id is a ROOT component: NULL is the global value, a root's own row overrides the global for work in that root's tree.
CREATE TABLE env_var (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128 AND name NOT GLOB '*[^A-Za-z0-9_]*' AND name NOT GLOB '[0-9]*'),
  project_id    TEXT REFERENCES component (id) ON DELETE CASCADE,
  description   TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 512),
  is_secret     INTEGER NOT NULL DEFAULT 1 CHECK (is_secret IN (0,1)),
  agent_visible INTEGER NOT NULL DEFAULT 0 CHECK (agent_visible IN (0,1)),
  value         BLOB NOT NULL CHECK (length(value) > 0),
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_env_var_scope ON env_var (name, COALESCE(project_id, ''));

-- Who changed or read what, and when. Names and flags only: a value never rides a ledger row.
CREATE TABLE env_var_change (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  project_id  TEXT,
  op          TEXT NOT NULL CHECK (op IN ('set','delete','agent_read')),
  summary     TEXT NOT NULL,
  actor_kind  TEXT NOT NULL CHECK (actor_kind IN ('user','agent','trigger')),
  actor_id    TEXT NOT NULL CHECK (length(actor_id) > 0),
  occurred_at TEXT NOT NULL
);
CREATE INDEX idx_env_var_change_name ON env_var_change (name, id);
CREATE TRIGGER env_var_change_no_update BEFORE UPDATE ON env_var_change BEGIN SELECT RAISE(ABORT, 'env_var_change is append-only'); END;
CREATE TRIGGER env_var_change_no_delete BEFORE DELETE ON env_var_change BEGIN SELECT RAISE(ABORT, 'env_var_change is append-only'); END;

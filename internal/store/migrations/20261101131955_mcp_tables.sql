-- S46 T1: MCP configuration as records (design MG1). The existing mcp.Config.Validate stays THE validator (run on the assembled candidate before every write); these constraints are the net beneath it. Agents are referenced by slug (S43). env holds variable NAMES only (C17). mcp_change is append-only by trigger; mcp_import is the adoption ledger for the legacy JSON file (MG3).
CREATE TABLE mcp_server (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  name           TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64 AND name NOT GLOB '*[^a-z0-9_-]*' AND name GLOB '[a-z]*'),
  tier           TEXT NOT NULL CHECK (tier IN ('public','private')),
  agent_slug     TEXT REFERENCES agent (slug) ON DELETE RESTRICT,
  transport_type TEXT NOT NULL CHECK (transport_type IN ('stdio','http')),
  command        TEXT NOT NULL DEFAULT '',
  args           TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(args) AND json_type(args) = 'array'),
  env            TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(env) AND json_type(env) = 'array'),
  url            TEXT NOT NULL DEFAULT '',
  enabled        INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  origin         TEXT NOT NULL DEFAULT 'user' CHECK (origin IN ('system','user','import')),
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  CHECK ((tier = 'public') = (agent_slug IS NULL))
);
CREATE UNIQUE INDEX idx_mcp_server_name ON mcp_server (name);
CREATE INDEX idx_mcp_server_agent ON mcp_server (agent_slug);
CREATE TABLE mcp_server_tool (
  server_id INTEGER NOT NULL REFERENCES mcp_server (id) ON DELETE CASCADE,
  name      TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128),
  rerun     TEXT NOT NULL DEFAULT '',
  effect    TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (server_id, name)
);
CREATE TABLE mcp_agent (
  agent_slug TEXT PRIMARY KEY REFERENCES agent (slug) ON DELETE RESTRICT,
  core       TEXT CHECK (core IS NULL OR (json_valid(core) AND json_type(core) = 'array'))   -- NULL = the whole core set; '[]' = none (distinct facts)
);
CREATE TABLE mcp_declare (
  agent_slug TEXT NOT NULL REFERENCES agent (slug) ON DELETE RESTRICT,
  qualified  TEXT NOT NULL CHECK (instr(qualified, '.') > 1),
  PRIMARY KEY (agent_slug, qualified)
);
CREATE TABLE mcp_change (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  op          TEXT NOT NULL CHECK (op IN ('server_add','server_update','server_remove','server_enable','server_disable','grant_declare','grant_revoke','core_set','import','seed')),
  server      TEXT, agent_slug TEXT, qualified TEXT,
  summary     TEXT NOT NULL CHECK (length(summary) BETWEEN 1 AND 1024),
  actor_kind  TEXT NOT NULL CHECK (actor_kind IN ('user','cli','system')),
  actor_id    TEXT NOT NULL CHECK (length(actor_id) BETWEEN 1 AND 256),
  occurred_at TEXT NOT NULL
);
CREATE INDEX idx_mcp_change_server ON mcp_change (server, id);
CREATE TRIGGER mcp_change_no_update BEFORE UPDATE ON mcp_change BEGIN SELECT RAISE(ABORT, 'mcp_change is append-only (S46 MG1)'); END;
CREATE TRIGGER mcp_change_no_delete BEFORE DELETE ON mcp_change BEGIN SELECT RAISE(ABORT, 'mcp_change is append-only (S46 MG1)'); END;
CREATE TABLE mcp_import (
  id INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL, sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
  result TEXT NOT NULL CHECK (result IN ('imported','refused')), detail TEXT NOT NULL CHECK (length(detail) <= 1024),
  servers INTEGER NOT NULL DEFAULT 0, imported_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_mcp_import_hash ON mcp_import (sha256) WHERE result = 'imported';

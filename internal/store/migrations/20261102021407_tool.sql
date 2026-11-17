-- S47 T2: the tool CATALOG ENTRY as a record (design TC2). Executors and input schemas stay code — schema is stored as a read-only display copy with its SHA-256 so drift is detectable. Written only by the tools.core seed applier (origin='system'); MCP tools are NOT rows here (S46's tables are their one truth, TC4).
CREATE TABLE tool (
  name         TEXT PRIMARY KEY CHECK (length(name) BETWEEN 1 AND 64 AND name NOT GLOB '*[^a-z0-9_]*'),
  family       TEXT NOT NULL CHECK (length(family) BETWEEN 1 AND 32),
  description  TEXT NOT NULL CHECK (length(description) BETWEEN 1 AND 4096),
  schema       TEXT NOT NULL CHECK (json_valid(schema)),
  schema_hash  TEXT NOT NULL CHECK (length(schema_hash) = 64),
  rerun_class  TEXT NOT NULL CHECK (rerun_class IN ('safe','detectable','unsafe')),
  -- capabilities is an ARRAY, and an empty array is legal: todo and report_size
  -- touch no store, file, process or socket (T0's deviation 2, pinned by
  -- internal/tools' TestEmptyCapabilitySetsAreOnlyTheDeclaredTools, which fails
  -- any OTHER tool arriving empty). A >= 1 count here would have refused the
  -- seed's own two honest rows and bought nothing the test does not already pin.
  capabilities TEXT NOT NULL CHECK (json_valid(capabilities) AND json_type(capabilities) = 'array'),
  governed     TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(governed) AND json_type(governed) = 'array'),
  twin         TEXT NOT NULL DEFAULT '',
  aliases      TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(aliases) AND json_type(aliases) = 'array'),
  conditional  INTEGER NOT NULL DEFAULT 0 CHECK (conditional IN (0,1)),
  notes        TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 200),
  version      INTEGER NOT NULL CHECK (version >= 1),
  origin       TEXT NOT NULL DEFAULT 'system' CHECK (origin IN ('system')),
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
CREATE INDEX idx_tool_family ON tool (family);

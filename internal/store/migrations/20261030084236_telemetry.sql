-- S33 T1's telemetry tables: four loop-tier detail tables and three incremental rollup tiers (design TL1/TL2). Nothing here phones home (S33 brief §0 — the name is the S31 tab's, the data never leaves the store). step_turn carries ONE column beyond design TL5's DDL, `category`: TL12's RecordTurn signature already carries it and TL6 tags transcript turns at their structurally-distinct append points, so the row that method writes needs a home for it; the CHECK is the four-value closed set, and `tool_schema` is absent on purpose (TL6: unmeasurable today — a column nothing can ever fill).
CREATE TABLE step_turn (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id          INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  iteration        INTEGER NOT NULL,
  started_at       TEXT NOT NULL,
  ended_at         TEXT NOT NULL,
  wall_ms          INTEGER NOT NULL,
  input_tokens     INTEGER,   -- nullable, mirrors model.Usage (M9) — NULL means "not reported", never 0
  output_tokens    INTEGER,
  cache_read_tokens    INTEGER,
  cache_write_tokens   INTEGER,
  reasoning_tokens     INTEGER,
  cost_usd         REAL,
  cost_state       TEXT NOT NULL DEFAULT 'unknown' CHECK (cost_state IN ('known','partial','unknown')),
  route            TEXT NOT NULL,   -- which route served this turn (routes can change mid-loop, S18)
  category         TEXT NOT NULL CHECK (category IN ('system_prompt','injected_context','assistant_output','tool_result'))
);
CREATE INDEX idx_step_turn_step ON step_turn (step_id);

-- The CompactionRecord's queryable second home (TL7): the blob inside one step's
-- checkpoint stays byte-identical; this is additive, joinable across a project's
-- whole history (D3).
CREATE TABLE step_compaction (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id          INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  start_idx        INTEGER NOT NULL,
  end_idx          INTEGER NOT NULL,
  estimate_before  INTEGER NOT NULL,
  estimate_after   INTEGER NOT NULL,
  summary_len      INTEGER NOT NULL,
  input_tokens     INTEGER, output_tokens INTEGER,   -- nullable, same M9 discipline — this is a REAL model call
  cost_usd         REAL,
  cost_state       TEXT NOT NULL DEFAULT 'unknown' CHECK (cost_state IN ('known','partial','unknown')),
  occurred_at      TEXT NOT NULL
);
CREATE INDEX idx_step_compaction_step ON step_compaction (step_id);

-- D5: the file half rides tools.Result's existing Path/LinesAdded/LinesRemoved
-- (S31 D8). Nullable line deltas: a read has neither; a write may not know (binary).
CREATE TABLE step_file_activity (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  path TEXT NOT NULL, action TEXT NOT NULL CHECK (action IN ('read','write')),
  lines_added INTEGER, lines_removed INTEGER,
  occurred_at TEXT NOT NULL
);
CREATE INDEX idx_step_file_activity_step ON step_file_activity (step_id);

-- D6: a NULL exit_code is "the process ended on a signal", not zero.
CREATE TABLE step_command_activity (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  command TEXT NOT NULL, exit_code INTEGER, wall_ms INTEGER NOT NULL,
  occurred_at TEXT NOT NULL
);
CREATE INDEX idx_step_command_activity_step ON step_command_activity (step_id);

-- The rollup tiers (TL9): counts and totals ONLY — no per-path, no per-command
-- detail survives past the loop tier, or the rollup would grow with the SAME
-- shape as the detail it exists to outlive (TL8). Every tier's row is created by
-- its first contributing write and bumped in that write's own transaction (TL2);
-- cost_state is 'known' only while every contributing model call was 'known' —
-- one 'partial' or 'unknown' contributor floors the tier (D9).
CREATE TABLE component_telemetry (
  component_id TEXT PRIMARY KEY REFERENCES component (id) ON DELETE CASCADE,
  loops_run INTEGER NOT NULL DEFAULT 0,
  wall_ms_total INTEGER NOT NULL DEFAULT 0,
  input_tokens_total INTEGER NOT NULL DEFAULT 0, output_tokens_total INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens_total INTEGER NOT NULL DEFAULT 0, cache_write_tokens_total INTEGER NOT NULL DEFAULT 0,
  reasoning_tokens_total INTEGER NOT NULL DEFAULT 0,
  cost_usd_total REAL NOT NULL DEFAULT 0, cost_state TEXT NOT NULL DEFAULT 'known' CHECK (cost_state IN ('known','partial','unknown')),
  files_touched_total INTEGER NOT NULL DEFAULT 0, files_read_total INTEGER NOT NULL DEFAULT 0, files_written_total INTEGER NOT NULL DEFAULT 0,
  lines_added_total INTEGER NOT NULL DEFAULT 0, lines_removed_total INTEGER NOT NULL DEFAULT 0,
  commands_run_total INTEGER NOT NULL DEFAULT 0, commands_failed_total INTEGER NOT NULL DEFAULT 0, command_wall_ms_total INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);

-- Identical columns, keyed by the group tier (TL1's S38 correction: what the
-- brief called "project" is `component`, and THIS row is the new group tier above it).
CREATE TABLE project_telemetry (
  project_id TEXT PRIMARY KEY REFERENCES project (id) ON DELETE CASCADE,
  loops_run INTEGER NOT NULL DEFAULT 0,
  wall_ms_total INTEGER NOT NULL DEFAULT 0,
  input_tokens_total INTEGER NOT NULL DEFAULT 0, output_tokens_total INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens_total INTEGER NOT NULL DEFAULT 0, cache_write_tokens_total INTEGER NOT NULL DEFAULT 0,
  reasoning_tokens_total INTEGER NOT NULL DEFAULT 0,
  cost_usd_total REAL NOT NULL DEFAULT 0, cost_state TEXT NOT NULL DEFAULT 'known' CHECK (cost_state IN ('known','partial','unknown')),
  files_touched_total INTEGER NOT NULL DEFAULT 0, files_read_total INTEGER NOT NULL DEFAULT 0, files_written_total INTEGER NOT NULL DEFAULT 0,
  lines_added_total INTEGER NOT NULL DEFAULT 0, lines_removed_total INTEGER NOT NULL DEFAULT 0,
  commands_run_total INTEGER NOT NULL DEFAULT 0, commands_failed_total INTEGER NOT NULL DEFAULT 0, command_wall_ms_total INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);

-- The singleton: one store, one row (TL1). The CHECK is the schema's own word
-- that a second row is structurally impossible, not a convention the Go side
-- promises to keep. No seed row: the legitimate zero-loops-ever state is the
-- ABSENCE of the row, which WorkspaceTelemetry answers with the all-zero struct
-- and an `exists=false` flag rather than a fabricated timestamp.
CREATE TABLE workspace_telemetry (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  loops_run INTEGER NOT NULL DEFAULT 0,
  wall_ms_total INTEGER NOT NULL DEFAULT 0,
  input_tokens_total INTEGER NOT NULL DEFAULT 0, output_tokens_total INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens_total INTEGER NOT NULL DEFAULT 0, cache_write_tokens_total INTEGER NOT NULL DEFAULT 0,
  reasoning_tokens_total INTEGER NOT NULL DEFAULT 0,
  cost_usd_total REAL NOT NULL DEFAULT 0, cost_state TEXT NOT NULL DEFAULT 'known' CHECK (cost_state IN ('known','partial','unknown')),
  files_touched_total INTEGER NOT NULL DEFAULT 0, files_read_total INTEGER NOT NULL DEFAULT 0, files_written_total INTEGER NOT NULL DEFAULT 0,
  lines_added_total INTEGER NOT NULL DEFAULT 0, lines_removed_total INTEGER NOT NULL DEFAULT 0,
  commands_run_total INTEGER NOT NULL DEFAULT 0, commands_failed_total INTEGER NOT NULL DEFAULT 0, command_wall_ms_total INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);

-- S28 T1's learnings store: a classified, queryable record of what an agent, a project, or a person has learned (brief §1), plus its free-form tag index. The vec0 table (learning_vec) is NOT here — its embedding dimension is data, fixed at CREATE time, so internal/store's learnings statements create it on demand (S22's retrieval precedent, 0040's own doc comment: "its dimension is data"), name-derived and identifier-sanitised; a learning with no configured route has no learning_vec row, and that is the normal, working state (D5), not a gap. scope and provenance close at the database (the CHECK is the backstop behind the Go door); scope_key carries NO foreign key — a learning's agent scope is validated in Go against S13's registry or a plugin_agent row, which SQLite cannot express (design §3).
CREATE TABLE learning (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  scope            TEXT NOT NULL CHECK (scope IN ('person','project','agent')),
  scope_key        TEXT NOT NULL,
  body             TEXT NOT NULL,
  provenance       TEXT NOT NULL CHECK (provenance IN ('told','observed','assumed','relayed')),
  observed_count   INTEGER,
  source_loop_slug TEXT,
  source_step_id   INTEGER,
  created_at       TEXT NOT NULL
);
CREATE INDEX idx_learning_scope_key ON learning (scope, scope_key);
CREATE INDEX idx_learning_provenance ON learning (provenance);

-- learning_tag is the join table (design M5, not a JSON column): an index on
-- (tag, learning_id) gives exact-match and LIKE 'prefix%' queries a real
-- index to use, and (learning_id) answers "all tags on this learning".
CREATE TABLE learning_tag (
  learning_id INTEGER NOT NULL REFERENCES learning (id) ON DELETE CASCADE,
  tag         TEXT NOT NULL CHECK (tag != '')
);
CREATE INDEX idx_learning_tag_tag ON learning_tag (tag, learning_id);
CREATE INDEX idx_learning_tag_learning ON learning_tag (learning_id);

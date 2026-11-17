-- S56 T8: widen the proposal kind CHECK to admit the two restrictive autonomy proposal kinds (autonomy_revoke, autonomy_deny); the grant itself has no proposal shape at all.
-- The DDL is migration0045's (S37 T1's agent_import widening) verbatim except the two
-- added values and the guard tables renamed to this migration's ID. SQLite cannot
-- tighten or widen a CHECK in place, so a new shape means a new entry — never an edit.
CREATE TABLE proposal_new (
  id              INTEGER PRIMARY KEY,
  kind            TEXT NOT NULL CHECK (kind IN
                    ('policy_add','policy_remove','seed_corpus','mcp_server_add','mcp_server_remove','agent_import','autonomy_revoke','autonomy_deny')),
  payload         TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'proposed'
                    CHECK (status IN ('proposed','committed','rejected','discarded')),
  created_at      TEXT NOT NULL,
  created_by_kind TEXT NOT NULL CHECK (created_by_kind IN ('user','agent','trigger')),
  created_by_id   TEXT NOT NULL,
  decided_at      TEXT,
  decided_by_kind TEXT CHECK (decided_by_kind IS NULL OR decided_by_kind = 'user'),
  decided_by_id   TEXT,
  decide_reason   TEXT,
  applied_error   TEXT
);

-- Copy every row (proposals may exist and must survive the widening, not be
-- refused over).
INSERT INTO proposal_new (id,kind,payload,status,created_at,created_by_kind,created_by_id,decided_at,decided_by_kind,decided_by_id,decide_reason,applied_error)
  SELECT id,kind,payload,status,created_at,created_by_kind,created_by_id,decided_at,decided_by_kind,decided_by_id,decide_reason,applied_error FROM proposal;

-- The copy was total: a row lost between the two tables aborts the transaction.
CREATE TABLE proposal_20261101180000_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO proposal_20261101180000_copy_guard
  SELECT (SELECT COUNT(*) FROM proposal_new), (SELECT COUNT(*) FROM proposal);
DROP TABLE proposal_20261101180000_copy_guard;

DROP TABLE proposal;
ALTER TABLE proposal_new RENAME TO proposal;

-- No index to recreate (T0 Q1: proposal has none).

-- Integrity, asserted rather than assumed: if any FK ended up dangling, the open
-- fails here rather than reading a trail that quietly lost a parent.
CREATE TABLE proposal_20261101180000_fk_guard (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO proposal_20261101180000_fk_guard SELECT COUNT(*) FROM pragma_foreign_key_check();
DROP TABLE proposal_20261101180000_fk_guard;

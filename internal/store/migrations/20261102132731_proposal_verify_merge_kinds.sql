-- S57 T8: proposal.kind gains TWO kinds — 'verify_command' (an agent may
-- propose a NEW gate, add only — a required or advisory gate can only make
-- landing harder, and it executes only under the policy gate) and
-- 'merge_rule' (an agent may propose a TIGHTENED rule; the desk computes
-- the field-by-field diff and refuses any loosening at create). Both are
-- restrictive in the AL11 sense: neither can widen what a person may do.
-- The S46 T6 rebuild pattern verbatim (copy, drop, rename — proposals may
-- exist and must survive; a copy-guard asserts the move was total; the
-- foreign-key-check end guard is kept). The column list equals the
-- 20261101193000 table's exactly — the only change is ,'verify_command',
-- 'merge_rule' in the kind CHECK. proposal carries no index (0045's T0 Q1
-- still holds: none was ever added).
-- PARALLEL-WIDENING AMENDMENT (2026-11-02, S57 T8): this rebuild sorts
-- AFTER 20261101193000, so its kind list restates every kind any earlier
-- rebuild admitted — the six base kinds, S56 T8's two restrictive autonomy
-- kinds, S46 T6's mcp_grant_revoke, and the two kinds above: eleven. A
-- later rebuild that copies the table MUST restate all eleven: the copy
-- carries the rows, but only the new CHECK decides what inserts afterwards
-- (a nine-word CHECK here would have silently locked every DB out of the
-- autonomy and mcp kinds). The Go mirror is store.ProposalKinds; the lock
-- test compares them.
CREATE TABLE proposal_new (
  id              INTEGER PRIMARY KEY,
  kind            TEXT NOT NULL CHECK (kind IN
                    ('policy_add','policy_remove','seed_corpus','mcp_server_add','mcp_server_remove','agent_import','autonomy_revoke','autonomy_deny','mcp_grant_revoke','verify_command','merge_rule')),
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

INSERT INTO proposal_new (id,kind,payload,status,created_at,created_by_kind,created_by_id,decided_at,decided_by_kind,decided_by_id,decide_reason,applied_error)
  SELECT id,kind,payload,status,created_at,created_by_kind,created_by_id,decided_at,decided_by_kind,decided_by_id,decide_reason,applied_error FROM proposal;

CREATE TABLE proposal_20261102132731_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO proposal_20261102132731_copy_guard
  SELECT (SELECT COUNT(*) FROM proposal_new), (SELECT COUNT(*) FROM proposal);
DROP TABLE proposal_20261102132731_copy_guard;

DROP TABLE proposal;
ALTER TABLE proposal_new RENAME TO proposal;

CREATE TABLE proposal_20261102132731_fk_guard (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO proposal_20261102132731_fk_guard SELECT COUNT(*) FROM pragma_foreign_key_check();
DROP TABLE proposal_20261102132731_fk_guard;

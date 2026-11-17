-- S49 T6: proposal.kind gains ONE kind — 'automation_disable' (an agent may
-- propose that ONE automation be DISABLED — paused for a schedule or webhook
-- row, switched off for an event row — as an INERT row; a person reviews and
-- commits it through S45's generic commit; the effect is T4's own disable/
-- pause path stamped with the COMMITTER, never the proposer, and the only
-- automation write an agent can ever initiate (AN9)).
-- This is the fourteenth kind.
-- The S46 T6 / S57 T8 / S48 T9 / S59 T7 rebuild pattern verbatim (copy,
-- drop, rename — proposals may exist and must survive; a copy-guard asserts
-- the move was total; the foreign-key-check end guard is kept). The column
-- list equals the 20261103181500 table's exactly — the only change is
-- ,'automation_disable' in the kind CHECK. proposal carries no index (0045's
-- T0 Q1 still holds: none was ever added).
-- PARALLEL-WIDENING AMENDMENT (restated, S57 T8's rule): this rebuild sorts
-- AFTER 20261103181500, so its kind list restates EVERY kind any earlier
-- rebuild admitted — the six base kinds, S56 T8's two restrictive autonomy
-- kinds, S46 T6's mcp_grant_revoke, S57 T8's two gate kinds, S48 T9's
-- blueprint_save, S59 T7's environment_restrict, and the kind above:
-- fourteen. A later rebuild that copies the table MUST restate all fourteen.
-- The Go mirror is store.ProposalKinds; the lock test compares them
-- (TestProposalKindListMatchesGoMirror reads the live DDL, so the mirror and
-- the CHECK cannot drift silently).
-- There is deliberately no automation_create, automation_update,
-- automation_enable or automation_archive kind: a proposal can only reduce
-- what an automation may do, never start, widen, switch on, or hide (AN9 —
-- the narrowing-only rule, S46's mcp_grant_revoke and S56's AL11 in the
-- automation plane).
CREATE TABLE proposal_new (
  id              INTEGER PRIMARY KEY,
  kind            TEXT NOT NULL CHECK (kind IN
                    ('policy_add','policy_remove','seed_corpus','mcp_server_add','mcp_server_remove','agent_import','autonomy_revoke','autonomy_deny','mcp_grant_revoke','verify_command','merge_rule','blueprint_save','environment_restrict','automation_disable')),
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

CREATE TABLE proposal_20261103193000_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO proposal_20261103193000_copy_guard
  SELECT (SELECT COUNT(*) FROM proposal_new), (SELECT COUNT(*) FROM proposal);
DROP TABLE proposal_20261103193000_copy_guard;

DROP TABLE proposal;
ALTER TABLE proposal_new RENAME TO proposal;

CREATE TABLE proposal_20261103193000_fk_guard (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO proposal_20261103193000_fk_guard SELECT COUNT(*) FROM pragma_foreign_key_check();
DROP TABLE proposal_20261103193000_fk_guard;

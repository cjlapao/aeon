-- S46 T6: the design revision S35 TD1 required — proposal.kind gains ONE
-- kind, 'mcp_grant_revoke' (the narrowing direction: an agent may propose
-- taking a declaration back, never the converse). The S37 T1 rebuild
-- pattern verbatim (copy, drop, rename — proposals may exist and must
-- survive; a copy-guard asserts the move was total; the foreign-key-check
-- end guard is kept). The column list equals migration0045's exactly — the
-- only change is ,'mcp_grant_revoke' in the kind CHECK. proposal carries
-- no index (0045's T0 Q1 still holds: none was ever added).
-- PARALLEL-WIDENING AMENDMENT (2026-11-02, at the S56 merge): this rebuild
-- sorts AFTER S56 T8's 20261101180000 (18:00 < 19:30), so its kind list is
-- the UNION of both widenings — the six base kinds, S56's two restrictive
-- autonomy kinds, and 'mcp_grant_revoke'. A later rebuild that copies the
-- table MUST restate every kind any earlier rebuild admitted: the copy
-- carries the rows, but only the new CHECK decides what inserts afterwards
-- (a seven-word CHECK here would have silently locked every DB out of the
-- autonomy kinds). The Go mirror is store.ProposalKinds; the lock test
-- compares them.
CREATE TABLE proposal_new (
  id              INTEGER PRIMARY KEY,
  kind            TEXT NOT NULL CHECK (kind IN
                    ('policy_add','policy_remove','seed_corpus','mcp_server_add','mcp_server_remove','agent_import','autonomy_revoke','autonomy_deny','mcp_grant_revoke')),
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

CREATE TABLE proposal_0070_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO proposal_0070_copy_guard
  SELECT (SELECT COUNT(*) FROM proposal_new), (SELECT COUNT(*) FROM proposal);
DROP TABLE proposal_0070_copy_guard;

DROP TABLE proposal;
ALTER TABLE proposal_new RENAME TO proposal;

CREATE TABLE proposal_0070_fk_guard (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO proposal_0070_fk_guard SELECT COUNT(*) FROM pragma_foreign_key_check();
DROP TABLE proposal_0070_fk_guard;

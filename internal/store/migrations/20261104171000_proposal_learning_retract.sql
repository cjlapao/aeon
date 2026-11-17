-- S52 T4: proposal.kind gains ONE kind — 'learning_retract' (an agent may
-- propose that one MEMORY be taken back — an INERT row; a person reviews and
-- commits it through S45's generic commit, and the RETRACTION then runs as
-- the COMMITTER: the stamp, the audit line and the vector drop all carry the
-- committing person as actor, never the proposer — LM8 stays the human door
-- and this kind is a REQUEST for a person to open it, exactly roadmap_idea's
-- shape for the roadmap and automation_disable's for automations).
-- This is the sixteenth kind.
-- The S46 T6 / S57 T8 / S48 T9 / S59 T7 / S49 T6 / S50 T5 rebuild pattern
-- verbatim (copy, drop, rename — proposals may exist and must survive; a
-- copy-guard asserts the move was total; the foreign-key-check end guard is
-- kept). The column list equals the 20261104101500 table's exactly — the
-- only change is ,'learning_retract' in the kind CHECK. proposal carries no
-- index (0045's T0 Q1 still holds: none was ever added).
-- PARALLEL-WIDENING AMENDMENT (restated, S57 T8's rule): this rebuild sorts
-- AFTER 20261104101500, so its kind list restates EVERY kind any earlier
-- rebuild admitted — the six base kinds, S56 T8's two restrictive autonomy
-- kinds, S46 T6's mcp_grant_revoke, S57 T8's two gate kinds, S48 T9's
-- blueprint_save, S59 T7's environment_restrict, S49 T6's automation_disable,
-- S50 T5's roadmap_idea, and the kind above: sixteen. A later rebuild that
-- copies the table MUST restate all sixteen.
-- The Go mirror is store.ProposalKinds; the lock test compares them
-- (TestProposalKindListMatchesGoMirror reads the live DDL, so the mirror and
-- the CHECK cannot drift silently).
-- There is deliberately no learning_restore, learning_edit or
-- learning_add_for_others kind (LM7/LM8 — the memory plane's one agent ask
-- is that a memory be RETRACTED; restoring, editing and speaking for others
-- move by a person alone through T2's doors, and the T3 guards refuse each
-- such name by word).
CREATE TABLE proposal_new (
  id              INTEGER PRIMARY KEY,
  kind            TEXT NOT NULL CHECK (kind IN
                    ('policy_add','policy_remove','seed_corpus','mcp_server_add','mcp_server_remove','agent_import','autonomy_revoke','autonomy_deny','mcp_grant_revoke','verify_command','merge_rule','blueprint_save','environment_restrict','automation_disable','roadmap_idea','learning_retract')),
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

CREATE TABLE proposal_20261104171000_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO proposal_20261104171000_copy_guard
  SELECT (SELECT COUNT(*) FROM proposal_new), (SELECT COUNT(*) FROM proposal);
DROP TABLE proposal_20261104171000_copy_guard;

DROP TABLE proposal;
ALTER TABLE proposal_new RENAME TO proposal;

CREATE TABLE proposal_20261104171000_fk_guard (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO proposal_20261104171000_fk_guard SELECT COUNT(*) FROM pragma_foreign_key_check();
DROP TABLE proposal_20261104171000_fk_guard;

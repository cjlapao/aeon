-- S61 T5: proposal.kind gains TWO kinds — 'charter_save' (a charter body
-- proposed by anyone, committed by a person: the desk's apply saves it with
-- the COMMITTING PERSON as the revision actor, source 'proposal' and the
-- proposal id as source_ref, GI4 — and never with the proposer's identity)
-- and 'intake_plan' (an S61 goal-intake plan as an inert proposal: the
-- generic commit door refuses it with a fixed sentence naming intake.commit,
-- because it needs a target path and a publish choice no generic payload can
-- carry, GI7/OD4). These are the eighteenth and nineteenth kinds.
-- The S46 T6 / S57 T8 / S48 T9 / S59 T7 / S49 T6 / S50 T5 / S52 T4 / S60 T9
-- rebuild pattern verbatim (copy, drop, rename — proposals may exist and
-- must survive; a copy-guard asserts the move was total; the foreign-key-
-- check end guard is kept). The column list equals the 20261104193000
-- table's exactly — the only change is ,'charter_save','intake_plan' in the
-- kind CHECK. proposal carries no index (0045's T0 Q1 still holds: none was
-- ever added).
-- PARALLEL-WIDENING AMENDMENT (restated, S57 T8's rule): this rebuild sorts
-- AFTER 20261104193000, so its kind list restates EVERY kind any earlier
-- rebuild admitted — the six base kinds, S56 T8's two restrictive autonomy
-- kinds, S46 T6's mcp_grant_revoke, S57 T8's two gate kinds, S48 T9's
-- blueprint_save, S59 T7's environment_restrict, S49 T6's automation_disable,
-- S50 T5's roadmap_idea, S52 T4's learning_retract, S60 T9's
-- mergequeue_enqueue, and the two above: nineteen. A later rebuild that
-- copies the table MUST restate all nineteen.
-- The Go mirror is store.ProposalKinds; the lock test compares them
-- (TestProposalKindListMatchesGoMirror reads the live DDL, so the mirror and
-- the CHECK cannot drift silently).
-- There is deliberately no charter_revert, charter_delete, intake_commit,
-- intake_publish or intake_reject kind: charter history is append-only (the
-- retract pattern S52 set for memories), publishing moves through the
-- person's intake.commit door alone (GI7), and discarding an intake plan is
-- the generic 'discarded' status, not a kind.
CREATE TABLE proposal_new (
  id              INTEGER PRIMARY KEY,
  kind            TEXT NOT NULL CHECK (kind IN
                    ('policy_add','policy_remove','seed_corpus','mcp_server_add','mcp_server_remove','agent_import','autonomy_revoke','autonomy_deny','mcp_grant_revoke','verify_command','merge_rule','blueprint_save','environment_restrict','automation_disable','roadmap_idea','learning_retract','mergequeue_enqueue','charter_save','intake_plan')),
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

CREATE TABLE proposal_20261105110000_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO proposal_20261105110000_copy_guard
  SELECT (SELECT COUNT(*) FROM proposal_new), (SELECT COUNT(*) FROM proposal);
DROP TABLE proposal_20261105110000_copy_guard;

DROP TABLE proposal;
ALTER TABLE proposal_new RENAME TO proposal;

CREATE TABLE proposal_20261105110000_fk_guard (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO proposal_20261105110000_fk_guard SELECT COUNT(*) FROM pragma_foreign_key_check();
DROP TABLE proposal_20261105110000_fk_guard;

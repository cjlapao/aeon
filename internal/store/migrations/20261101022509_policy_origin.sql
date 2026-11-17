-- S45 T12: policy.origin — 'system' for rows the seed process wrote (the shipped corpus), 'user' for everything an operator authored, 'import' reserved. Replaces the old convention of telling seeded rows apart by an id prefix; the backfill marks existing corpus.* rows system. The policy closed vocabulary is untouched (origin is provenance, not a policy field the evaluator reads).
ALTER TABLE policy ADD COLUMN origin TEXT NOT NULL DEFAULT 'user';
UPDATE policy SET origin = 'system' WHERE id LIKE 'corpus.%';
CREATE INDEX idx_policy_origin ON policy (origin);
-- origin carries no DDL CHECK on purpose: the policy table's five CHECK bodies are ITS closed vocabulary (type,
-- target_kind, action, source, and the authoring pair), pinned as exactly those five by
-- TestTheRebuiltCHECKStillClosesTheFourVocabularies — and origin is provenance, not policy vocabulary (PM11: the
-- evaluator, the gate and the in-force read consult none of it). The three-value set is instead enforced at every
-- door that writes it: PolicyAddTx refuses an origin outside system|user|import by name before the statement, the
-- ctx door never names the column (the DEFAULT decides 'user'), and every read that filters provenance parses it
-- through the boundary's ParseOrigin. (Recorded as a deviation in the task's Notes: a DDL CHECK here would make
-- the policy table declare a sixth CHECK, which the frozen vocabulary test exists to refuse.)

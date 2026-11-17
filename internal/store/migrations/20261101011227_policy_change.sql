-- S45 T3's authoring audit: one append-only row per policy authoring write (add/remove/seed/commit/reject), naming the peer-stamped actor. A separate table from policy_event on purpose — that one holds verdict refusals and is trimmed by retention (D15); an authoring audit must never be trimmed. UPDATE and DELETE are refused by trigger, the S44 setting_event precedent.
CREATE TABLE policy_change (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  op          TEXT NOT NULL CHECK (op IN ('add','remove','seed','commit','reject')),
  policy_id   TEXT,                       -- NULL for seed (many ids) and for reject
  proposal_id INTEGER,                    -- the proposal a commit/reject decided; NULL otherwise
  summary     TEXT NOT NULL CHECK (length(summary) BETWEEN 1 AND 1024),
  actor_kind  TEXT NOT NULL CHECK (actor_kind IN ('user','cli')),
  actor_id    TEXT NOT NULL CHECK (length(actor_id) BETWEEN 1 AND 256),
  occurred_at TEXT NOT NULL,
  CHECK ((op IN ('add','remove') AND policy_id IS NOT NULL) OR op IN ('seed','commit','reject'))
);
CREATE INDEX idx_policy_change_policy ON policy_change (policy_id, id);
CREATE INDEX idx_policy_change_time   ON policy_change (occurred_at);
CREATE TRIGGER policy_change_no_update BEFORE UPDATE ON policy_change
BEGIN SELECT RAISE(ABORT, 'policy_change is append-only: an authoring audit row is never rewritten (S45 PM8)'); END;
CREATE TRIGGER policy_change_no_delete BEFORE DELETE ON policy_change
BEGIN SELECT RAISE(ABORT, 'policy_change is append-only: an authoring audit row is never deleted (S45 PM8)'); END;

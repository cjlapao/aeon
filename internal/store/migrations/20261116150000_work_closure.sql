-- Work closed because its loop was archived or deleted. A task, roadmap item, queue entry or goal whose loop goes away
-- while it is still open would stay on the board for the next agents to pick up; it is closed instead, as closed NOT
-- done, and this row says why and what it was before, so restoring an archived loop can open it again.
CREATE TABLE work_closure (
  entity_kind    TEXT NOT NULL CHECK (entity_kind IN ('task','roadmap','queue','goal')),
  entity_id      TEXT NOT NULL,
  reason         TEXT NOT NULL CHECK (reason IN ('loop-archived','loop-deleted')),
  loop_slug      TEXT NOT NULL,
  component_root TEXT NOT NULL,
  prior_status   TEXT NOT NULL,
  closed_at      TEXT NOT NULL,
  PRIMARY KEY (entity_kind, entity_id)
);
CREATE INDEX idx_work_closure_loop ON work_closure (component_root, loop_slug);

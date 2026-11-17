-- Documents belong to a goal: loop_document gains owner_goal_id, set when a loop that works a goal writes, and when a loop files a goal (the loop keeps its own rows and the historic link; the goal owns what the loop produced).
ALTER TABLE loop_document ADD COLUMN owner_goal_id TEXT;
CREATE INDEX idx_document_owner_goal ON loop_document (owner_goal_id, file, rev DESC) WHERE owner_goal_id IS NOT NULL;

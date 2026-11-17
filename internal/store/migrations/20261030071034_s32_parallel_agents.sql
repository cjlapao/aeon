-- S32 T1: parallel agents — the cap on component and the daemon-made worktree ledger
ALTER TABLE component ADD COLUMN max_parallel_agents INTEGER NOT NULL DEFAULT 1 CHECK (max_parallel_agents BETWEEN 1 AND 16);
CREATE TABLE loop_worktree (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  component_root TEXT NOT NULL,
  loop_slug     TEXT NOT NULL,
  -- owner_kind/owner_id generalise over both PA4 shapes: a Shape-A lane
  -- has no task row to hang this on, a Shape-B item does.
  owner_kind    TEXT NOT NULL CHECK (owner_kind IN ('lane','task')),
  owner_id      TEXT NOT NULL,
  path          TEXT NOT NULL,
  branch        TEXT NOT NULL,
  base_sha      TEXT NOT NULL,
  created_at    TEXT NOT NULL,
  removed_at    TEXT,
  FOREIGN KEY (component_root, loop_slug) REFERENCES loop (component_root, slug) ON DELETE CASCADE,
  UNIQUE (component_root, loop_slug, owner_kind, owner_id, removed_at)
);

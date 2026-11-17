-- S75 T1: the loop's todo list. One header row per list (revision, opened, closed) and one row per item; closed lists stay as history and cascade away with the loop.
CREATE TABLE loop_todo_list (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug      TEXT NOT NULL,
  component_root TEXT NOT NULL,
  revision       INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
  opened_at      TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  last_step      INTEGER,
  closed_at      TEXT,
  closed_by      TEXT CHECK (closed_by IS NULL OR closed_by IN ('all-done','cleared','person')),
  CHECK ((closed_at IS NULL) = (closed_by IS NULL)),
  FOREIGN KEY (component_root, loop_slug) REFERENCES loop (component_root, slug) ON DELETE CASCADE
);
-- At most one ACTIVE list per loop: the partial index is the rule, not a convention.
CREATE UNIQUE INDEX idx_loop_todo_list_active ON loop_todo_list (component_root, loop_slug) WHERE closed_at IS NULL;
CREATE INDEX idx_loop_todo_list_loop ON loop_todo_list (component_root, loop_slug, id);

CREATE TABLE loop_todo (
  list_id  INTEGER NOT NULL REFERENCES loop_todo_list (id) ON DELETE CASCADE,
  position INTEGER NOT NULL CHECK (position >= 1),
  item_id  TEXT NOT NULL,
  content  TEXT NOT NULL,
  status   TEXT NOT NULL CHECK (status IN ('pending','in_progress','done')),
  PRIMARY KEY (list_id, position)
);

-- S60 T2: loop_worktree.owner_kind CHECK widens to admit 'integration' —
-- the queue's per-item merge-forward worktree takes its slot on the S32
-- ledger (design MQ6). Copy, drop, rename (S46 T6/S57 T8 pattern): rows
-- must survive, a copy-guard asserts the move was total, the
-- foreign-key-check end guard is kept.
-- THE COLUMN LIST EQUALS THE CURRENT (s32, 20261030071034) TABLE EXACTLY —
-- the only change is ,'integration' in the owner_kind CHECK (the S57 T8
-- restatement rule: a rebuild carries every value that ever admitted, and
-- lane/task stay). loop_worktree has no child tables (T0 measured: no
-- foreign_key_list entry anywhere references it), and no index beyond the
-- inline UNIQUE and AUTOINCREMENT rowid — nothing to recreate after the
-- rename. The FK to loop (component_root, slug) ON DELETE CASCADE rides
-- with the table text, unchanged.
CREATE TABLE loop_worktree_new (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  component_root TEXT NOT NULL,
  loop_slug     TEXT NOT NULL,
  -- owner_kind/owner_id generalise over both PA4 shapes: a Shape-A lane
  -- has no task row to hang this on, a Shape-B item does — and S60 adds
  -- the queue's integration branch as the third owner.
  owner_kind    TEXT NOT NULL CHECK (owner_kind IN ('lane','task','integration')),
  owner_id      TEXT NOT NULL,
  path          TEXT NOT NULL,
  branch        TEXT NOT NULL,
  base_sha      TEXT NOT NULL,
  created_at    TEXT NOT NULL,
  removed_at    TEXT,
  FOREIGN KEY (component_root, loop_slug) REFERENCES loop (component_root, slug) ON DELETE CASCADE,
  UNIQUE (component_root, loop_slug, owner_kind, owner_id, removed_at)
);

INSERT INTO loop_worktree_new (id,component_root,loop_slug,owner_kind,owner_id,path,branch,base_sha,created_at,removed_at)
  SELECT id,component_root,loop_slug,owner_kind,owner_id,path,branch,base_sha,created_at,removed_at FROM loop_worktree;

CREATE TABLE loop_worktree_20261104044822_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO loop_worktree_20261104044822_copy_guard
  SELECT (SELECT COUNT(*) FROM loop_worktree_new), (SELECT COUNT(*) FROM loop_worktree);
DROP TABLE loop_worktree_20261104044822_copy_guard;

DROP TABLE loop_worktree;
ALTER TABLE loop_worktree_new RENAME TO loop_worktree;

CREATE TABLE loop_worktree_20261104044822_fk_guard (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO loop_worktree_20261104044822_fk_guard SELECT COUNT(*) FROM pragma_foreign_key_check();
DROP TABLE loop_worktree_20261104044822_fk_guard;

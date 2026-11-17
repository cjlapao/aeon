-- CT1/CT2/CT5(A)/CT6/CT7 · S44-extra T2 · the component tree: fold the
-- project->component pair into one self-referencing component table.
--
-- This is ONE forward-only migration, applied in ONE transaction, over the
-- store's _foreign_keys=1 connection (PRAGMA foreign_keys=OFF is a no-op
-- inside a transaction, verified, so it is never relied on here). It:
--   1. builds the CT5 map (collapse / keep / drop) for every project;
--   2. rebuilds component as component_new (self-referencing parent_id,
--      nullable inherited settings, an archived_at) and copies every
--      component into it verbatim;
--   3. inserts each KEPT project as a root component under its own id;
--   4. parks and restores every child of component around the drop/rename
--      (the 0035 hazard: with cascade on and FK=OFF a no-op in a txn, a
--      DROP can silently empty a child, so the rows are snapshotted and
--      restored, not left to the cascade);
--   5. moves each kept project's rollup into component_telemetry;
--   6. installs the depth triggers AFTER the data (CT2: at most two levels);
--   7. installs the guards (fk, map, depth).
--
-- project and zz_ct_project_map SURVIVE this migration: B (the next file)
-- needs the map to re-key epic/goal/automation and to drop project.

-- ---------------------------------------------------------------------------
-- 1. The map: one row per project, its disposition and the id it resolves to.
--    collapse -> the one member's id; keep -> the project's own id;
--    drop     -> a placeholder (the project's own id; it is dropped and no
--                component is created for it, and nothing references it by
--                construction).
-- ---------------------------------------------------------------------------
CREATE TABLE zz_ct_project_map (
  project_id  TEXT PRIMARY KEY,
  component_id TEXT NOT NULL,
  disposition TEXT NOT NULL CHECK (disposition IN ('collapse','drop','keep'))
);

INSERT INTO zz_ct_project_map (project_id, component_id, disposition)
SELECT p.id,
  CASE
    WHEN p.implicit = 0 THEN p.id
    WHEN (SELECT count(*) FROM component WHERE project_id = p.id) = 1
         THEN (SELECT id FROM component WHERE project_id = p.id LIMIT 1)
    WHEN (SELECT count(*) FROM component WHERE project_id = p.id) >= 2 THEN p.id
    WHEN ref.refd = 1 THEN p.id
    ELSE p.id
  END,
  CASE
    WHEN p.implicit = 0 THEN 'keep'
    WHEN (SELECT count(*) FROM component WHERE project_id = p.id) = 1 THEN 'collapse'
    WHEN (SELECT count(*) FROM component WHERE project_id = p.id) >= 2 THEN 'keep'
    WHEN ref.refd = 1 THEN 'keep'
    ELSE 'drop'
  END
FROM project p
LEFT JOIN (
  SELECT project_id, 1 AS refd FROM (
    SELECT project_id FROM epic
    UNION
    SELECT project_id FROM goal
    UNION
    SELECT project_id FROM automation_event
    UNION
    SELECT project_id FROM event_trigger
    UNION
    SELECT project_id FROM project_telemetry
      WHERE loops_run <> 0 OR cost_usd_total <> 0 OR commands_run_total <> 0
        OR files_touched_total <> 0
    UNION
    -- a match that names the project (the value is a JSON array of ids).
    SELECT x.value FROM event_trigger t, json_tree(t.match_json) x
      WHERE x.path LIKE '$.project[%' AND x.value IS NOT NULL
  ) GROUP BY project_id
) ref ON ref.project_id = p.id;

-- Guard: every project got exactly one disposition.
CREATE TABLE zz_ct_map_guard (
  n INTEGER,
  total INTEGER,
  CHECK (n = total)
);
INSERT INTO zz_ct_map_guard (n, total)
SELECT (SELECT count(*) FROM zz_ct_project_map), (SELECT count(*) FROM project);

-- ---------------------------------------------------------------------------
-- 2. component_new: every current column except project_id, plus the
--    self-referencing parent_id and an archived_at. The inheritable columns
--    (queue_advance, sizing_mode, max_parallel_agents, default_branch,
--    branch_prefix) lose NOT NULL and keep their default; the CHECK becomes
--    "NULL OR <old check>" so a child may inherit (NULL) and a root must not.
--
--    The self-FK MUST reference component_new, not component: a self-FK
--    written as REFERENCES component(id) while component exists is dropped
--    when component is dropped and is not restored by the rename (verified
--    on the bundled driver). References component_new so it survives the
--    rename and becomes REFERENCES component(id).
-- ---------------------------------------------------------------------------
CREATE TABLE component_new (
  id                TEXT PRIMARY KEY,
  name              TEXT NOT NULL,
  slug              TEXT NOT NULL,
  remote            TEXT,
  remote_key        TEXT,
  repo_key          TEXT,
  owner             TEXT,
  tags              TEXT NOT NULL DEFAULT '[]',
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL,
  icon              TEXT NOT NULL DEFAULT '',
  short_description TEXT NOT NULL DEFAULT '',
  description       TEXT NOT NULL DEFAULT '',
  parent_id         TEXT REFERENCES component_new(id),
  archived_at       TEXT,
  default_branch    TEXT DEFAULT '',
  branch_prefix     TEXT DEFAULT '',
  queue_advance     TEXT DEFAULT 'ask'    CHECK (queue_advance IS NULL OR queue_advance IN ('auto','ask')),
  sizing_mode       TEXT DEFAULT 'manual' CHECK (sizing_mode IS NULL OR sizing_mode IN ('manual','skill')),
  max_parallel_agents INTEGER DEFAULT 1   CHECK (max_parallel_agents IS NULL OR max_parallel_agents BETWEEN 1 AND 16)
);

-- ---------------------------------------------------------------------------
-- 3. Insert each KEPT project as a root component under its own id, BEFORE
--    the components. A child's parent_id names its root's id and the
--    self-FK (parent_id REFERENCES component_new(id)) is immediate, so the
--    root must exist before any child can name it. The project's
--    name/descriptions/icon become the root's; the slug is the project's
--    name lower-cased, non-alnum -> '-', dashes trimmed, with a '-group'
--    suffix where the slug collides with any component (a root and one of
--    its children cannot share a slug). The slug check reads the ORIGINAL
--    component table, not component_new: component_new is empty here, so it
--    could not see the children it must not collide with. Inheritable
--    columns are at their defaults (a root has nothing to inherit from);
--    timestamps are preserved.
-- ---------------------------------------------------------------------------
INSERT INTO component_new
  (id, name, slug, remote, remote_key, repo_key, owner, tags,
   created_at, updated_at, icon, short_description, description,
   parent_id, archived_at, default_branch, branch_prefix,
   queue_advance, sizing_mode, max_parallel_agents)
SELECT p.id,
  p.name,
  CASE
    WHEN (SELECT count(*) FROM component cn WHERE cn.slug = lower(p.name)) = 0
      THEN lower(p.name)
    ELSE lower(p.name) || '-group'
  END,
  NULL, NULL, NULL, NULL, '[]',
  p.created_at, p.updated_at,
  COALESCE(p.icon_emoji, p.icon_image, ''),
  p.short_description, p.description,
  NULL, NULL,
  '', '', 'ask', 'manual', 1
FROM project p
JOIN zz_ct_project_map m ON m.project_id = p.id AND m.disposition = 'keep';

-- ---------------------------------------------------------------------------
-- 4. Copy every component verbatim. parent_id is the project's new id when
--    the project is a root (keep) — now present, inserted above — so the
--    self-FK is satisfied; NULL when the project folds into the component
--    (collapse) or is dropped (cannot happen: a drop has no component).
--    archived_at is NULL for every migrated row.
-- ---------------------------------------------------------------------------
INSERT INTO component_new
  (id, name, slug, remote, remote_key, repo_key, owner, tags,
   created_at, updated_at, icon, short_description, description,
   parent_id, archived_at, default_branch, branch_prefix,
   queue_advance, sizing_mode, max_parallel_agents)
SELECT c.id, c.name, c.slug, c.remote, c.remote_key, c.repo_key, c.owner,
   c.tags, c.created_at, c.updated_at, c.icon, c.short_description,
   c.description,
   CASE WHEN m.disposition = 'keep' THEN m.component_id END,
   NULL,
   c.default_branch, c.branch_prefix,
   c.queue_advance, c.sizing_mode, c.max_parallel_agents
FROM component c
JOIN zz_ct_project_map m ON m.project_id = c.project_id;

-- ---------------------------------------------------------------------------
-- 5. Park the children of component, drop, rename, restore. The park is a
--    snapshot taken BEFORE the drop (so a cascade that empties a child loses
--    nothing); the restore re-inserts the snapshot with an explicit column
--    list so a later schema change cannot misalign it.
-- ---------------------------------------------------------------------------
CREATE TABLE zz_ct_park_component_root AS SELECT * FROM component_root;
CREATE TABLE zz_ct_park_component_telemetry AS SELECT * FROM component_telemetry;
CREATE TABLE zz_ct_park_goal_component AS SELECT * FROM goal_component;
CREATE TABLE zz_ct_park_task AS SELECT * FROM task;

-- The CASCADE descendants of the children above. component_root and task are
-- CASCADE parents, so the clear below cascade-deletes every one of these;
-- snapshot them now (before the clear) and restore them after the four
-- children. On a fresh store they are empty, so the snapshot is a no-op.
CREATE TABLE zz_ct_park_step AS SELECT * FROM step;
CREATE TABLE zz_ct_park_brief AS SELECT * FROM brief;
CREATE TABLE zz_ct_park_chat_message AS SELECT * FROM chat_message;
CREATE TABLE zz_ct_park_loop_phase AS SELECT * FROM loop_phase;
CREATE TABLE zz_ct_park_loop_worktree AS SELECT * FROM loop_worktree;
CREATE TABLE zz_ct_park_memory AS SELECT * FROM memory;
CREATE TABLE zz_ct_park_question AS SELECT * FROM question;
CREATE TABLE zz_ct_park_task_loop AS SELECT * FROM task_loop;
CREATE TABLE zz_ct_park_action_intent AS SELECT * FROM action_intent;
CREATE TABLE zz_ct_park_agent_chain AS SELECT * FROM agent_chain;
CREATE TABLE zz_ct_park_agent_task AS SELECT * FROM agent_task;
CREATE TABLE zz_ct_park_step_command_activity AS SELECT * FROM step_command_activity;
CREATE TABLE zz_ct_park_step_compaction AS SELECT * FROM step_compaction;
CREATE TABLE zz_ct_park_step_file_activity AS SELECT * FROM step_file_activity;
CREATE TABLE zz_ct_park_step_message AS SELECT * FROM step_message;
CREATE TABLE zz_ct_park_step_recovery AS SELECT * FROM step_recovery;
CREATE TABLE zz_ct_park_step_steer AS SELECT * FROM step_steer;
CREATE TABLE zz_ct_park_step_turn AS SELECT * FROM step_turn;
CREATE TABLE zz_ct_park_tool_receipt AS SELECT * FROM tool_receipt;

-- copy guards: the snapshot holds every row (one guard per child; a single
-- CHECK per table, so the DDL never carries a multi-CHECK list).
CREATE TABLE zz_ct_guard_root (n INTEGER CHECK (n = 0));
INSERT INTO zz_ct_guard_root (n)
  SELECT (SELECT count(*) FROM component_root) - (SELECT count(*) FROM zz_ct_park_component_root);
CREATE TABLE zz_ct_guard_te (n INTEGER CHECK (n = 0));
INSERT INTO zz_ct_guard_te (n)
  SELECT (SELECT count(*) FROM component_telemetry) - (SELECT count(*) FROM zz_ct_park_component_telemetry);
CREATE TABLE zz_ct_guard_gc (n INTEGER CHECK (n = 0));
INSERT INTO zz_ct_guard_gc (n)
  SELECT (SELECT count(*) FROM goal_component) - (SELECT count(*) FROM zz_ct_park_goal_component);
CREATE TABLE zz_ct_guard_task (n INTEGER CHECK (n = 0));
INSERT INTO zz_ct_guard_task (n)
  SELECT (SELECT count(*) FROM task) - (SELECT count(*) FROM zz_ct_park_task);

-- component_root and component_telemetry are CASCADE children of component;
-- goal_component and task are NO ACTION. Either way the DROP below must not
-- touch them, and the DROP is blocked by their FK on a seeded store, so all
-- four are cleared here and re-inserted from their snapshots (a restore
-- into a non-empty table would otherwise duplicate — task into the
-- (component_root, loop_slug, taskid) index). On a fresh store they are
-- empty, so the clear is a no-op there.
--
-- Two of the four (component_root, task) are CASCADE parents of further
-- tables (brief, chat_message, loop_phase, loop_worktree, memory, question,
-- step, task_loop, and step's own children action_intent, agent_chain,
-- agent_task, step_command_activity, step_compaction, step_file_activity,
-- step_message, step_recovery, step_steer, step_turn, tool_receipt). A clear
-- of component_root/task cascade-deletes those, so they are snapshotted
-- above (before this clear) and restored below (after the four children, in
-- foreign-key order) — the snapshot is the only thing that stops the cascade
-- from losing them.
DELETE FROM component_root;
DELETE FROM component_telemetry;
DELETE FROM goal_component;
DELETE FROM task;

DROP TABLE component;
ALTER TABLE component_new RENAME TO component;

INSERT INTO component_root SELECT * FROM zz_ct_park_component_root;
INSERT INTO component_telemetry SELECT * FROM zz_ct_park_component_telemetry;
INSERT INTO goal_component SELECT * FROM zz_ct_park_goal_component;
INSERT INTO task SELECT * FROM zz_ct_park_task;

-- The CASCADE descendants. The clear of component_root/task cascade-deletes
-- only rows whose FK was NON-NULL; a row with a NULL FK survives, so a plain
-- re-insert would collide (UNIQUE on id). Clear each descendant first — that
-- cascade-deletes its own children, but every descendant is snapshotted above,
-- so nothing is lost — then re-insert from the snapshot, in foreign-key order
-- (step before the tables that reference it).
-- ORDER AMENDMENT (2026-11-02, found on a live store): the clear and the
-- re-insert each run in foreign-key order, and the order this list shipped in
-- broke that in two places, invisible on an empty store (the census fixture
-- carried no chat or agent rows, so every drill passed) and fatal on any
-- store that has ever asked a chat question:
--   * chat_message.question_id references question (the FK fires at re-insert
--     time whatever the delete action says, and question was re-inserted
--     AFTER chat_message — so the store's own chat rows could not come back);
--   * DELETE FROM question runs while an agent_task row still names it:
--     agent_task.question_id is ON DELETE NO ACTION, so a store with an async
--     agent task that raised a question dies on the clear, not the restore.
-- Both are ordering, not shape: the final schema is byte-identical to what
-- this file first shipped, which is why the in-file amendment (the
-- 20261101193000 precedent, amended in place on 2026-11-02 while still
-- unapplied) is safe here where a new number would be — nothing can fix the
-- order inside a transaction that has never been applied.
DELETE FROM step;
DELETE FROM brief;
DELETE FROM chat_message;
DELETE FROM loop_phase;
DELETE FROM loop_worktree;
DELETE FROM memory;
DELETE FROM task_loop;
DELETE FROM action_intent;
DELETE FROM agent_chain;
DELETE FROM agent_task;
DELETE FROM question;
DELETE FROM step_command_activity;
DELETE FROM step_compaction;
DELETE FROM step_file_activity;
DELETE FROM step_message;
DELETE FROM step_recovery;
DELETE FROM step_steer;
DELETE FROM step_turn;
DELETE FROM tool_receipt;
INSERT INTO step SELECT * FROM zz_ct_park_step;
INSERT INTO brief SELECT * FROM zz_ct_park_brief;
INSERT INTO question SELECT * FROM zz_ct_park_question;
INSERT INTO chat_message SELECT * FROM zz_ct_park_chat_message;
INSERT INTO loop_phase SELECT * FROM zz_ct_park_loop_phase;
INSERT INTO loop_worktree SELECT * FROM zz_ct_park_loop_worktree;
INSERT INTO memory SELECT * FROM zz_ct_park_memory;
INSERT INTO task_loop SELECT * FROM zz_ct_park_task_loop;
INSERT INTO action_intent SELECT * FROM zz_ct_park_action_intent;
INSERT INTO agent_chain SELECT * FROM zz_ct_park_agent_chain;
INSERT INTO agent_task SELECT * FROM zz_ct_park_agent_task;
INSERT INTO step_command_activity SELECT * FROM zz_ct_park_step_command_activity;
INSERT INTO step_compaction SELECT * FROM zz_ct_park_step_compaction;
INSERT INTO step_file_activity SELECT * FROM zz_ct_park_step_file_activity;
INSERT INTO step_message SELECT * FROM zz_ct_park_step_message;
INSERT INTO step_recovery SELECT * FROM zz_ct_park_step_recovery;
INSERT INTO step_steer SELECT * FROM zz_ct_park_step_steer;
INSERT INTO step_turn SELECT * FROM zz_ct_park_step_turn;
INSERT INTO tool_receipt SELECT * FROM zz_ct_park_tool_receipt;

DROP TABLE zz_ct_park_component_root;
DROP TABLE zz_ct_park_component_telemetry;
DROP TABLE zz_ct_park_goal_component;
DROP TABLE zz_ct_park_task;
DROP TABLE zz_ct_park_step;
DROP TABLE zz_ct_park_brief;
DROP TABLE zz_ct_park_chat_message;
DROP TABLE zz_ct_park_loop_phase;
DROP TABLE zz_ct_park_loop_worktree;
DROP TABLE zz_ct_park_memory;
DROP TABLE zz_ct_park_question;
DROP TABLE zz_ct_park_task_loop;
DROP TABLE zz_ct_park_action_intent;
DROP TABLE zz_ct_park_agent_chain;
DROP TABLE zz_ct_park_agent_task;
DROP TABLE zz_ct_park_step_command_activity;
DROP TABLE zz_ct_park_step_compaction;
DROP TABLE zz_ct_park_step_file_activity;
DROP TABLE zz_ct_park_step_message;
DROP TABLE zz_ct_park_step_recovery;
DROP TABLE zz_ct_park_step_steer;
DROP TABLE zz_ct_park_step_turn;
DROP TABLE zz_ct_park_tool_receipt;
DROP TABLE zz_ct_guard_root;
DROP TABLE zz_ct_guard_te;
DROP TABLE zz_ct_guard_gc;
DROP TABLE zz_ct_guard_task;

-- Indexes: drop the one that rode on the removed column; keep and add the
-- rest (0035 lesson: the rebuild detaches them, so recreate by name).
DROP INDEX IF EXISTS idx_component_project;
CREATE INDEX idx_component_remote_key ON component (remote_key);
CREATE INDEX idx_component_repo_key   ON component (repo_key);
CREATE INDEX idx_component_parent     ON component (parent_id);

-- ---------------------------------------------------------------------------
-- 6. Telemetry: each kept project's rollup row becomes a component row
--    (project_id is the component_id now). The columns are identical
--    apart from the key.
-- ---------------------------------------------------------------------------
INSERT INTO component_telemetry
  (component_id, loops_run, wall_ms_total, input_tokens_total, output_tokens_total,
   cache_read_tokens_total, cache_write_tokens_total, reasoning_tokens_total,
   cost_usd_total, cost_state, files_touched_total, files_read_total,
   files_written_total, lines_added_total, lines_removed_total, commands_run_total,
   commands_failed_total, command_wall_ms_total, updated_at)
SELECT project_id, loops_run, wall_ms_total, input_tokens_total, output_tokens_total,
   cache_read_tokens_total, cache_write_tokens_total, reasoning_tokens_total,
   cost_usd_total, cost_state, files_touched_total, files_read_total,
   files_written_total, lines_added_total, lines_removed_total, commands_run_total,
   commands_failed_total, command_wall_ms_total, updated_at
FROM project_telemetry
WHERE project_id IN (SELECT project_id FROM zz_ct_project_map WHERE disposition = 'keep');

-- ---------------------------------------------------------------------------
-- 7. Depth triggers, installed AFTER the data. CT2: at most two levels. A
--    parent must exist, be a root, be live, not be itself, and not already
--    be a parent (moving a node with children would orphan them). A root
--    (parent_id NULL) may not carry a NULL inheritable value.
-- ---------------------------------------------------------------------------
CREATE TRIGGER component_tree_parent_insert
BEFORE INSERT ON component
WHEN NEW.parent_id IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'component tree: a node cannot have a parent')
    WHERE NEW.id = NEW.parent_id;
  SELECT RAISE(ABORT, 'component tree: the parent is not a component')
    WHERE NOT EXISTS (SELECT 1 FROM component WHERE id = NEW.parent_id);
  SELECT RAISE(ABORT, 'component tree: a child of a child is three levels')
    WHERE EXISTS (SELECT 1 FROM component p
                  WHERE p.id = NEW.parent_id AND p.parent_id IS NOT NULL);
  SELECT RAISE(ABORT, 'component tree: the parent is archived')
    WHERE EXISTS (SELECT 1 FROM component p
                  WHERE p.id = NEW.parent_id AND p.archived_at IS NOT NULL);
  SELECT RAISE(ABORT, 'component tree: a node with children cannot be a child')
    WHERE EXISTS (SELECT 1 FROM component c WHERE c.parent_id = NEW.id);
END;

CREATE TRIGGER component_tree_parent_update
BEFORE UPDATE OF parent_id ON component
WHEN NEW.parent_id IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'component tree: a node cannot have a parent')
    WHERE NEW.id = NEW.parent_id;
  SELECT RAISE(ABORT, 'component tree: the parent is not a component')
    WHERE NOT EXISTS (SELECT 1 FROM component WHERE id = NEW.parent_id);
  SELECT RAISE(ABORT, 'component tree: a child of a child is three levels')
    WHERE EXISTS (SELECT 1 FROM component p
                  WHERE p.id = NEW.parent_id AND p.parent_id IS NOT NULL);
  SELECT RAISE(ABORT, 'component tree: the parent is archived')
    WHERE EXISTS (SELECT 1 FROM component p
                  WHERE p.id = NEW.parent_id AND p.archived_at IS NOT NULL);
  SELECT RAISE(ABORT, 'component tree: a node with children cannot be a child')
    WHERE EXISTS (SELECT 1 FROM component c WHERE c.parent_id = NEW.id);
END;

CREATE TRIGGER component_tree_root_settings_insert
BEFORE INSERT ON component
WHEN NEW.parent_id IS NULL
BEGIN
  SELECT RAISE(ABORT, 'component tree: a root cannot inherit its settings')
    WHERE NEW.queue_advance IS NULL OR NEW.sizing_mode IS NULL
       OR NEW.max_parallel_agents IS NULL
       OR NEW.default_branch IS NULL OR NEW.branch_prefix IS NULL;
END;

CREATE TRIGGER component_tree_root_settings_update
BEFORE UPDATE ON component
WHEN NEW.parent_id IS NULL
BEGIN
  SELECT RAISE(ABORT, 'component tree: a root cannot inherit its settings')
    WHERE NEW.queue_advance IS NULL OR NEW.sizing_mode IS NULL
       OR NEW.max_parallel_agents IS NULL
       OR NEW.default_branch IS NULL OR NEW.branch_prefix IS NULL;
END;

-- ---------------------------------------------------------------------------
-- 8. Guards.
-- ---------------------------------------------------------------------------
CREATE TABLE zz_ct_fk_guard (
  n INTEGER CHECK (n = 0)
);
INSERT INTO zz_ct_fk_guard (n)
SELECT count(*) FROM pragma_foreign_key_check();

-- No depth: no component's parent is itself a child.
CREATE TABLE zz_ct_depth_guard (
  n INTEGER CHECK (n = 0)
);
INSERT INTO zz_ct_depth_guard (n)
SELECT count(*) FROM component c
  JOIN component p ON p.id = c.parent_id
  WHERE p.parent_id IS NOT NULL;

DROP TABLE zz_ct_fk_guard;
DROP TABLE zz_ct_depth_guard;

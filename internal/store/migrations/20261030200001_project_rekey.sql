-- CT5(B)/CT10/CT11 · S44-extra T3 · re-key epics, goals and automation to the
-- root component id, then drop project. After this migration no table,
-- column or FK names the old group.
--
-- Applied in one transaction over the store's _foreign_keys=1 connection.
-- It reads the map T2 left (zz_ct_project_map) and rewrites every reference
-- to a project id into a reference to a root component id, then drops
-- project, project_telemetry and the map.
--
-- The epic/goal rebuilds PARK their children (a DROP can silently empty a
-- child with cascade on and FK=OFF a no-op in a txn, verified), the
-- automation columns are renamed in place (their triggers and index do not
-- name the column, verified), the stored match JSON is rewritten project ->
-- root through the map, and the guards prove nothing named project survives.

-- ---------------------------------------------------------------------------
-- 1. epic rebuild: project_id -> component_id (a root), via the map.
--    goal references epic (epic_id), so goal is parked around the swap.
-- ---------------------------------------------------------------------------
CREATE TABLE epic_new (
  id            TEXT PRIMARY KEY,
  component_id  TEXT NOT NULL REFERENCES component (id),
  title         TEXT NOT NULL,
  objective     TEXT NOT NULL DEFAULT '',
  owner         TEXT NOT NULL DEFAULT '',
  target_date   TEXT,
  budget_usd    REAL,
  source_step   INTEGER,
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

INSERT INTO epic_new
  (id, component_id, title, objective, owner, target_date, budget_usd,
   source_step, created_at, updated_at)
SELECT e.id, m.component_id, e.title, e.objective, e.owner, e.target_date,
   e.budget_usd, e.source_step, e.created_at, e.updated_at
FROM epic e
JOIN zz_ct_project_map m ON m.project_id = e.project_id;

-- Park goal (its epic_id references epic) so the epic swap loses nothing.
CREATE TABLE zz_ct_park_goal AS SELECT * FROM goal;

DROP TABLE epic;
ALTER TABLE epic_new RENAME TO epic;

INSERT INTO goal SELECT * FROM zz_ct_park_goal;
DROP TABLE zz_ct_park_goal;

DROP INDEX IF EXISTS idx_epic_project;
CREATE INDEX idx_epic_component ON epic (component_id);

-- ---------------------------------------------------------------------------
-- 2. goal rebuild: project_id -> component_id (a root); epic_id unchanged.
--    goal_component (CASCADE) is the hazard and is parked around the swap.
-- ---------------------------------------------------------------------------
CREATE TABLE goal_new (
  id              TEXT PRIMARY KEY,
  component_id    TEXT NOT NULL REFERENCES component (id),
  epic_id         TEXT REFERENCES epic (id),
  version         INTEGER NOT NULL DEFAULT 1,
  title           TEXT NOT NULL,
  type            TEXT NOT NULL CHECK (type IN ('bug','feature','chore','refactor','investigation')),
  body_note       TEXT NOT NULL DEFAULT '',
  source_kind     TEXT NOT NULL CHECK (source_kind IN ('manual','brainstorm','webhook','cron','event')),
  source_ref      TEXT NOT NULL DEFAULT '',
  source_loop     TEXT,
  source_step     INTEGER,
  review_state    TEXT NOT NULL DEFAULT 'draft' CHECK (review_state IN ('draft','in_review','approved','rejected','parked')),
  created_by_kind TEXT NOT NULL CHECK (created_by_kind IN ('user','agent','trigger')),
  created_by_id   TEXT NOT NULL,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL
);

INSERT INTO goal_new
  (id, component_id, epic_id, version, title, type, body_note, source_kind,
   source_ref, source_loop, source_step, review_state, created_by_kind,
   created_by_id, created_at, updated_at)
SELECT g.id, m.component_id, g.epic_id, g.version, g.title, g.type, g.body_note,
   g.source_kind, g.source_ref, g.source_loop, g.source_step, g.review_state,
   g.created_by_kind, g.created_by_id, g.created_at, g.updated_at
FROM goal g
JOIN zz_ct_project_map m ON m.project_id = g.project_id;

CREATE TABLE zz_ct_park_goal_component AS SELECT * FROM goal_component;

DROP TABLE goal;
ALTER TABLE goal_new RENAME TO goal;

INSERT INTO goal_component SELECT * FROM zz_ct_park_goal_component;
DROP TABLE zz_ct_park_goal_component;

DROP INDEX IF EXISTS idx_goal_project;
CREATE INDEX idx_goal_component ON goal (component_id);

-- ---------------------------------------------------------------------------
-- 3. Root-only triggers: an epic or goal may only attach to a root
--    (a component with no parent). CT10.
-- ---------------------------------------------------------------------------
CREATE TRIGGER epic_root_only
BEFORE INSERT ON epic
BEGIN
  SELECT RAISE(ABORT, 'component tree: epics and goals belong to a root component')
    WHERE EXISTS (SELECT 1 FROM component c
                  WHERE c.id = NEW.component_id AND c.parent_id IS NOT NULL);
END;

CREATE TRIGGER epic_root_only_update
BEFORE UPDATE OF component_id ON epic
BEGIN
  SELECT RAISE(ABORT, 'component tree: epics and goals belong to a root component')
    WHERE EXISTS (SELECT 1 FROM component c
                  WHERE c.id = NEW.component_id AND c.parent_id IS NOT NULL);
END;

CREATE TRIGGER goal_root_only
BEFORE INSERT ON goal
BEGIN
  SELECT RAISE(ABORT, 'component tree: epics and goals belong to a root component')
    WHERE EXISTS (SELECT 1 FROM component c
                  WHERE c.id = NEW.component_id AND c.parent_id IS NOT NULL);
END;

CREATE TRIGGER goal_root_only_update
BEFORE UPDATE OF component_id ON goal
BEGIN
  SELECT RAISE(ABORT, 'component tree: epics and goals belong to a root component')
    WHERE EXISTS (SELECT 1 FROM component c
                  WHERE c.id = NEW.component_id AND c.parent_id IS NOT NULL);
END;

-- ---------------------------------------------------------------------------
-- 4. Automation: rename project_id -> root_id in place (the trigger and the
--    subject index do not name the column, verified), then re-key the value
--    through the map.
-- ---------------------------------------------------------------------------
ALTER TABLE automation_event RENAME COLUMN project_id TO root_id;
ALTER TABLE event_trigger RENAME COLUMN project_id TO root_id;

UPDATE automation_event SET root_id = m.component_id
  FROM zz_ct_project_map m WHERE m.project_id = automation_event.root_id
  AND automation_event.root_id <> '';
UPDATE event_trigger SET root_id = m.component_id
  FROM zz_ct_project_map m WHERE m.project_id = event_trigger.root_id
  AND event_trigger.root_id <> '';

-- ---------------------------------------------------------------------------
-- 5. Stored match JSON: rewrite each event_trigger match whose object has a
--    "project" key into one whose "root" key carries the mapped values,
--    other keys preserved. (The exact canonical byte form — empty lists
--    omitted, sorted — is the one-time Go data step the task spec allows;
--    this SQL form maps the values correctly so no match silently stops
--    matching, which is the hazard the rule guards.)
-- ---------------------------------------------------------------------------
UPDATE event_trigger
SET match_json = json_object(
  'event', json_extract(match_json, '$.event'),
  'root',  (SELECT json_group_array(m.component_id)
             FROM zz_ct_project_map m
             WHERE m.project_id IN (
               SELECT j.value FROM json_each(json_extract(match_json, '$.project')) j
             )),
  'component',   json_extract(match_json, '$.component'),
  'goal_type',   json_extract(match_json, '$.goal_type'),
  'task_type',   json_extract(match_json, '$.task_type'),
  'source_kind', json_extract(match_json, '$.source_kind')
)
WHERE json_extract(match_json, '$.project') IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 6. Drops: project, project_telemetry and the map, after proving no FK
--    still targets project.
-- ---------------------------------------------------------------------------
CREATE TABLE zz_ct_drop_guard (
  n INTEGER CHECK (n = 0)
);
INSERT INTO zz_ct_drop_guard (n)
  SELECT count(*) FROM pragma_foreign_key_list('epic')
  WHERE "table" = 'project';
INSERT INTO zz_ct_drop_guard (n)
  SELECT count(*) FROM pragma_foreign_key_list('goal')
  WHERE "table" = 'project';

DROP TABLE project_telemetry;
DROP TABLE project;
DROP TABLE zz_ct_project_map;

-- ---------------------------------------------------------------------------
-- 7. Final guards.
-- ---------------------------------------------------------------------------
CREATE TABLE zz_ct_fk_guard (
  n INTEGER CHECK (n = 0)
);
INSERT INTO zz_ct_fk_guard (n)
  SELECT count(*) FROM pragma_foreign_key_check();

-- No column named project_id remains anywhere.
CREATE TABLE zz_ct_projcol_guard (
  n INTEGER CHECK (n = 0)
);
INSERT INTO zz_ct_projcol_guard (n)
  SELECT count(*) FROM pragma_table_info('component') WHERE name = 'project_id';

-- No FK targets project.
CREATE TABLE zz_ct_projfk_guard (
  n INTEGER CHECK (n = 0)
);
INSERT INTO zz_ct_projfk_guard (n)
  SELECT count(*) FROM pragma_foreign_key_list('component') WHERE "table" = 'project';
INSERT INTO zz_ct_projfk_guard (n)
  SELECT count(*) FROM pragma_foreign_key_list('component_root') WHERE "table" = 'project';

DROP TABLE zz_ct_fk_guard;
DROP TABLE zz_ct_projcol_guard;
DROP TABLE zz_ct_projfk_guard;
DROP TABLE zz_ct_drop_guard;

-- S61 T1: the goal-intake shape — the charter row-history, acceptance
-- criteria as goal data, the intake session and its resume ledger, the
-- scaffold template set, and TWO one-word CHECK widenings: `goal.source_kind`
-- gains 'intake' (GI13 files the records an intake commits with provenance
-- that names itself — T0's Finding F-2 found the CHECK would otherwise refuse
-- the commit) and `roadmap_item.provenance` gains 'intake' (GI13, OD9, P-C:
-- it is a CHECK). The widenings are the 0016/mergequeue rebuild pattern
-- verbatim — copy, assert the copy, drop, rename, foreign-key-check end guard —
-- and both tables are rebuilt from the CURRENT DDL: `goal` as
-- 20261030200001_project_rekey.sql left it (component_id, the parked-set
-- hazard, and the two CT10 root-only triggers re-created byte-identically),
-- `roadmap_item` as frozen 0043 left it. Every other column, CHECK and index
-- is restated unchanged; the only words added anywhere are the two 'intake'
-- values and the new tables' own shape.
--
-- Append-only by trigger (P3): charter history, the intake ledger and the
-- template revisions are rows that may only be written once — the UPDATE and
-- DELETE refusals are the sentences a tamper reads. `scaffold_template` and
-- `goal_criterion` are MUTABLE by design (an operator edits a template; a
-- draft goal's criteria are replaced by ReplaceGoalCriteria) — they carry no
-- append-only trigger and their mutability is the store's own contract, not
-- the schema's silence.
--
-- NULL is never zero (P31): `intake.root_id`/`target_path`/`loop_slug` and
-- `goal_criterion.check_kind`/`check_ref` are nullable with no defaults —
-- absent means absent, and the one-state trigger below treats NULL as the
-- not-yet state, never as a value to overwrite.
--
-- No secret and no host path lives in any column here: the charter body and
-- the ledger detail are fixed-template text and closed-schema JSON the daemon
-- validates BEFORE this layer (GI2, SEC-033), and the intake's scratch_path is
-- the daemon's own state-dir path it created itself (GI6), written by the
-- daemon, never by a caller.
--
-- The Go mirrors are store.charterSourceKinds / the goal-criteria / intake /
-- template types; the lock tests compare the vocabularies against the live
-- DDL the way TestProposalKindListMatchesGoMirror does.

-- ---------------------------------------------------------------------------
-- charter_revision — GI1: the charter IS its row-history; the current charter
-- of a root is max(revision). No mutable charter row, so no second truth.
-- ---------------------------------------------------------------------------
CREATE TABLE charter_revision (
  id          TEXT PRIMARY KEY,                       -- 'charter-<newID>'
  root_id     TEXT NOT NULL REFERENCES component (id),
  revision    INTEGER NOT NULL CHECK (revision >= 1),
  body        TEXT NOT NULL CHECK (json_valid(body)), -- GI2's canonical JSON, validated by the daemon before it lands
  summary     TEXT NOT NULL,                          -- fixed template only (SEC-033) — never a user sentence
  actor_kind  TEXT NOT NULL CHECK (actor_kind IN ('user','agent','trigger')),
  actor_id    TEXT NOT NULL,
  source      TEXT NOT NULL CHECK (source IN ('person','proposal','intake')),
  source_ref  TEXT NOT NULL DEFAULT '',               -- proposal id / intake id when source names one
  created_at  TEXT NOT NULL,
  UNIQUE (root_id, revision)
);

CREATE INDEX idx_charter_revision_current ON charter_revision (root_id, revision DESC);

CREATE TRIGGER charter_revision_no_update
BEFORE UPDATE ON charter_revision
BEGIN
  SELECT RAISE(ABORT, 'charter history is append-only');
END;

CREATE TRIGGER charter_revision_no_delete
BEFORE DELETE ON charter_revision
BEGIN
  SELECT RAISE(ABORT, 'charter history is append-only');
END;

-- GI3: only the root of a tree carries a charter; a child's charter question
-- is answered by its root's. Same CT10 shape as goal_root_only, the store
-- refusing with the project's name first — this trigger is the net under it.
CREATE TRIGGER charter_root_only
BEFORE INSERT ON charter_revision
BEGIN
  SELECT RAISE(ABORT, 'charter: the charter lives on the project — a child component does not carry one')
    WHERE EXISTS (SELECT 1 FROM component c
                  WHERE c.id = NEW.root_id AND c.parent_id IS NOT NULL);
END;

-- ---------------------------------------------------------------------------
-- goal_criterion — GI5: acceptance criteria become goal DATA, versioned with
-- the goal, copied forward by UpdateGoal exactly as goal_component is (the
-- test names it). Created after the goal rebuild below so the FK to `goal`
-- speaks to the post-rename table from birth.
-- ---------------------------------------------------------------------------
CREATE TABLE goal_criterion (
  goal_id      TEXT NOT NULL REFERENCES goal (id) ON DELETE CASCADE,
  goal_version INTEGER NOT NULL CHECK (goal_version >= 1),
  ordinal      INTEGER NOT NULL CHECK (ordinal >= 1),
  criterion_id TEXT NOT NULL,                          -- the charter's stable 'ac-<n>' id, carried through
  text         TEXT NOT NULL CHECK (length(text) <= 512),
  check_kind   TEXT CHECK (check_kind IS NULL OR check_kind IN ('manual','command','test')),
  check_ref    TEXT CHECK (check_ref IS NULL OR length(check_ref) <= 512),
  PRIMARY KEY (goal_id, goal_version, ordinal),
  UNIQUE (goal_id, goal_version, criterion_id)
);

-- ---------------------------------------------------------------------------
-- intake — GI6: a person-driven session. No state column — state is derived
-- from the loop, the proposal and the ledger. root_id may be set exactly once
-- (the link is history); every other column is immutable once written.
-- ---------------------------------------------------------------------------
CREATE TABLE intake (
  id              TEXT PRIMARY KEY,                    -- 'int-<newID>'
  name            TEXT NOT NULL,
  target_path     TEXT,                                -- NULL until a commit carries one
  scratch_path    TEXT NOT NULL,                       -- the daemon's own 0700 workspace (GI6)
  loop_slug       TEXT,                                -- NULL until the interview loop starts
  root_id         TEXT REFERENCES component (id),      -- NULL until the commit registers it — set ONCE
  created_by_kind TEXT NOT NULL CHECK (created_by_kind IN ('user')),  -- an intake is always a person's act (GI6)
  created_by_id   TEXT NOT NULL,
  created_at      TEXT NOT NULL
);

-- GI6 as written: `root_id` may be set **once**. The predicate generalises that
-- law to every mutable column — each may go NULL→value exactly once and may
-- never then change (a set-then-amended path or slug is the same second truth
-- a re-linked root would be). That is strictly stronger than the spec's
-- literal wording ("no other column changes"), which would refuse even the
-- commit's one legal combined root+target write; the deviation is recorded in
-- T1's Notes. Immutable columns are re-asserted equality.
CREATE TRIGGER intake_root_once
BEFORE UPDATE ON intake
BEGIN
  SELECT RAISE(ABORT, 'an intake links each of its paths exactly once — the link is history')
   WHERE NOT (
        OLD.id = NEW.id
    AND OLD.name = NEW.name
    AND OLD.scratch_path = NEW.scratch_path
    AND OLD.created_by_kind = NEW.created_by_kind
    AND OLD.created_by_id = NEW.created_by_id
    AND OLD.created_at = NEW.created_at
    AND (OLD.loop_slug    IS NULL OR NEW.loop_slug    = OLD.loop_slug)
    AND (OLD.target_path  IS NULL OR NEW.target_path  = OLD.target_path)
    AND (OLD.root_id      IS NULL OR NEW.root_id      = OLD.root_id)
  );
END;

CREATE INDEX idx_intake_root ON intake (root_id);

-- ---------------------------------------------------------------------------
-- intake_step — GI6/GI13: the resume ledger. Append-only; a re-run SKIPS a
-- done step after re-verifying its evidence, so crash-resume never duplicates.
-- detail is the fixed template with ids filled in — never a user sentence.
-- ---------------------------------------------------------------------------
CREATE TABLE intake_step (
  intake_id TEXT NOT NULL REFERENCES intake (id) ON DELETE CASCADE,
  ordinal   INTEGER NOT NULL CHECK (ordinal >= 1),
  step      TEXT NOT NULL CHECK (length(step) <= 64),
  outcome   TEXT NOT NULL CHECK (outcome IN ('done','skipped','failed')),
  detail    TEXT NOT NULL CHECK (length(detail) <= 1024),
  at        TEXT NOT NULL,
  UNIQUE (intake_id, ordinal)
);

CREATE TRIGGER intake_step_no_update
BEFORE UPDATE ON intake_step
BEGIN
  SELECT RAISE(ABORT, 'an intake ledger row is written once');
END;

CREATE TRIGGER intake_step_no_delete
BEFORE DELETE ON intake_step
BEGIN
  SELECT RAISE(ABORT, 'an intake ledger row is written once');
END;

-- ---------------------------------------------------------------------------
-- scaffold_template + revisions — GI14. The template itself is MUTABLE (the
-- operator's edit is the point); its history is append-only (the BP7 shape
-- S48's blueprint_save rides: one revision row per save, the current shape
-- readable as a window over the history). origin 'system' rows are seeded and
-- may be EDITED (the view then reads diverged) but never overwritten by a
-- re-seed and never deleted; 'user' rows are the operator's own.
-- ---------------------------------------------------------------------------
CREATE TABLE scaffold_template (
  id          TEXT PRIMARY KEY,                        -- 'tpl-<newID>'
  slug        TEXT NOT NULL UNIQUE CHECK (slug = lower(slug) AND slug NOT LIKE '% %'),
  version     INTEGER NOT NULL CHECK (version >= 1),
  origin      TEXT NOT NULL CHECK (origin IN ('system','user')),
  title       TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  stack_tags  TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(stack_tags)),
  files       TEXT NOT NULL CHECK (json_valid(files)), -- [{path,mode,content}] (GI14)
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

CREATE TABLE scaffold_template_revision (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  slug       TEXT NOT NULL,
  version    INTEGER NOT NULL CHECK (version >= 1),
  files      TEXT NOT NULL CHECK (json_valid(files)),
  actor_kind TEXT NOT NULL CHECK (actor_kind IN ('user','agent','trigger')),
  actor_id   TEXT NOT NULL,
  origin     TEXT NOT NULL CHECK (origin IN ('system','user')),
  at         TEXT NOT NULL,
  UNIQUE (slug, version)
);

CREATE TRIGGER scaffold_template_no_delete
BEFORE DELETE ON scaffold_template
BEGIN
  SELECT RAISE(ABORT, 'a scaffold template is never deleted — edit it or leave it');
END;

CREATE TRIGGER scaffold_template_revision_no_update
BEFORE UPDATE ON scaffold_template_revision
BEGIN
  SELECT RAISE(ABORT, 'template history is append-only');
END;

CREATE TRIGGER scaffold_template_revision_no_delete
BEFORE DELETE ON scaffold_template_revision
BEGIN
  SELECT RAISE(ABORT, 'template history is append-only');
END;

-- ---------------------------------------------------------------------------
-- goal — one word wider: source_kind gains 'intake' (F-2 / GI13). Rebuilt
-- from the post-rekey DDL verbatim; the parked-set hazard (the rekey's own
-- comment names it) is honoured the same way it was there, and the two CT10
-- root-only triggers are re-created BYTE-IDENTICALLY (they die with the DROP).
-- ---------------------------------------------------------------------------
CREATE TABLE goal_new (
  id              TEXT PRIMARY KEY,
  component_id    TEXT NOT NULL REFERENCES component (id),
  epic_id         TEXT REFERENCES epic (id),
  version         INTEGER NOT NULL DEFAULT 1,
  title           TEXT NOT NULL,
  type            TEXT NOT NULL CHECK (type IN ('bug','feature','chore','refactor','investigation')),
  body_note       TEXT NOT NULL DEFAULT '',
  source_kind     TEXT NOT NULL CHECK (source_kind IN ('manual','brainstorm','webhook','cron','event','intake')),
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
SELECT id, component_id, epic_id, version, title, type, body_note, source_kind,
   source_ref, source_loop, source_step, review_state, created_by_kind,
   created_by_id, created_at, updated_at
FROM goal;

CREATE TABLE zz_gi_park_goal_component AS SELECT * FROM goal_component;
DELETE FROM goal_component;

CREATE TABLE goal_20261105024130_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO goal_20261105024130_copy_guard
  SELECT (SELECT COUNT(*) FROM goal_new), (SELECT COUNT(*) FROM goal);
DROP TABLE goal_20261105024130_copy_guard;

DROP TABLE goal;
ALTER TABLE goal_new RENAME TO goal;

INSERT INTO goal_component SELECT * FROM zz_gi_park_goal_component;
DROP TABLE zz_gi_park_goal_component;

DROP INDEX IF EXISTS idx_goal_component;
CREATE INDEX idx_goal_component ON goal (component_id);

-- The CT10 triggers, byte-identical to 20261030200001's text.
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
-- roadmap_item — one word wider: provenance gains 'intake' (P-C, GI13, OD9).
-- Rebuilt from the frozen-0043 DDL verbatim (S50's roadmap_change adds no FK
-- to this table — history outlives the item), its one index re-created, the
-- two cross-column CHECKs and the composite loop FK restated unchanged.
-- ---------------------------------------------------------------------------
CREATE TABLE roadmap_item_new (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  component_root  TEXT NOT NULL REFERENCES component_root (component_root),
  title           TEXT NOT NULL,
  epic            TEXT,
  status          TEXT NOT NULL CHECK (status IN ('idea','ideating','brief-agreed','planned','ready','running','done','cancelled')),
  priority_state  TEXT NOT NULL DEFAULT 'unknown' CHECK (priority_state IN ('known','assumed','unknown')),
  priority_label  TEXT,
  estimate_min    INTEGER,
  estimate_max    INTEGER,
  estimate_basis  TEXT,
  provenance      TEXT NOT NULL CHECK (provenance IN ('authored','triggered','triaged','intake')),
  brief_path      TEXT,
  plan_path       TEXT,
  loop_slug       TEXT,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  CHECK (priority_state <> 'unknown' OR priority_label IS NULL),
  CHECK (
    (estimate_min IS NULL AND estimate_max IS NULL AND estimate_basis IS NULL)
    OR
    (estimate_min IS NOT NULL AND estimate_max IS NOT NULL AND estimate_basis IS NOT NULL
     AND estimate_basis <> '' AND estimate_min <= estimate_max)
  ),
  FOREIGN KEY (component_root, loop_slug) REFERENCES loop (component_root, slug)
);

INSERT INTO roadmap_item_new
  SELECT id, component_root, title, epic, status, priority_state, priority_label,
     estimate_min, estimate_max, estimate_basis, provenance, brief_path, plan_path,
     loop_slug, created_at, updated_at
FROM roadmap_item;

CREATE TABLE roadmap_item_20261105024130_copy_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total));
INSERT INTO roadmap_item_20261105024130_copy_guard
  SELECT (SELECT COUNT(*) FROM roadmap_item_new), (SELECT COUNT(*) FROM roadmap_item);
DROP TABLE roadmap_item_20261105024130_copy_guard;

DROP TABLE roadmap_item;
ALTER TABLE roadmap_item_new RENAME TO roadmap_item;

-- AUTOINCREMENT needs no ceremony: the INSERT above carried explicit ids, and
-- SQLite raises sqlite_sequence to the highest id it sees (documented rule;
-- TestMigrationDrill plants rows, upgrades, and proves a later insert never
-- reuses a dead id).

DROP INDEX IF EXISTS idx_roadmap_item_root_status;
CREATE INDEX idx_roadmap_item_root_status ON roadmap_item (component_root, status);

CREATE TABLE roadmap_item_20261105024130_fk_guard (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO roadmap_item_20261105024130_fk_guard SELECT COUNT(*) FROM pragma_foreign_key_check();
DROP TABLE roadmap_item_20261105024130_fk_guard;

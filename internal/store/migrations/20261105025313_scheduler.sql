-- S27 T2 CN2/CN3/CN4/CN7/CN14/CN16 · the scheduler's authority rows, the
-- claim ledger, the append-only decision log, the roadmap dependency graph,
-- and the per-item autonomy caps.
--
-- Everything here STARTS OFF (CN2): the singleton is a ROW with enabled=0,
-- not a seed — no seed set may ever claim these tables (the store is the
-- only writer; T2's TestSchedulerRowsAreNotSeeded holds the line). The
-- scheduler's `scheduler_state` row is the daemon-wide switch; `scheduler_root`
-- is the per-repository authority a person grants, with S20's window rule
-- kept verbatim (pair-or-absent, and a window without a zone is a refusal
-- the database itself enforces).
--
-- `scheduler_start` is the claim AND CN14's one-attempt rule: the PRIMARY
-- KEY is (subject_kind, subject_id) — a second attempt at the same subject
-- cannot even exist, and recovery reads the same rows. `outcome` NULL is the
-- claimed-unstarted state S20's fire ledger taught (a crash between claim
-- and start leaves a row that reads "in flight", never a silent second try).
--
-- `scheduler_decision` is append-only by trigger (the S55/S63 convention),
-- carries NO FK to its subjects (CN16: history outlives them — a deleted
-- queue item's decisions stand), and its vocabularies are closed CHECKs.
-- The dedupe that keeps it a signal lives in the store's AppendDecision,
-- not in the database.
--
-- `roadmap_dependency` answers S50's open question with two triggers only a
-- database states honestly: the two items must live in ONE repository, and
-- a 2-cycle is refused at the edge. The longer cycle walk is the store's
-- (a trigger cannot recurse portably) — AddDependency refuses from inside
-- its own transaction, before any edge lands.
--
-- The autonomy caps (CN14/CN19): `autonomy_cap TEXT NOT NULL DEFAULT 'grant'
-- CHECK IN ('grant','none','outward')` on BOTH item tables. There is no
-- widening value: 'merge' and 'all' do not exist as strings anywhere in
-- this file. S24 CG7 verified an inline-CHECK ADD COLUMN needs no rebuild;
-- re-verified here on this SQLite (the house build's bundled driver): ALTER
-- succeeds, existing rows read 'grant', and CHECK('merge') refuses.

CREATE TABLE scheduler_state (
  id              INTEGER PRIMARY KEY CHECK (id = 1),
  enabled         INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
  paused          INTEGER NOT NULL DEFAULT 0 CHECK (paused IN (0,1)),
  paused_reason   TEXT,
  updated_at      TEXT    NOT NULL,
  updated_by_kind TEXT    NOT NULL CHECK (updated_by_kind IN ('user','agent','trigger')),
  updated_by_id   TEXT    NOT NULL
);

-- The singleton ROW (CN2): off, by the migration that makes the table, not
-- by a seed an operator can forget to run.
INSERT INTO scheduler_state (id, enabled, paused, paused_reason, updated_at, updated_by_kind, updated_by_id)
  VALUES (1, 0, 0, NULL, '1970-01-01T00:00:00.000Z', 'user', 'migration');

CREATE TABLE scheduler_root (
  component_root  TEXT PRIMARY KEY REFERENCES component_root (component_root),
  enabled         INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
  blueprint       TEXT    NOT NULL,
  tech            TEXT,
  with_json       TEXT,
  window_start    TEXT,
  window_end      TEXT,
  window_days     TEXT,
  zone            TEXT,
  min_priority    TEXT    NOT NULL DEFAULT 'assumed' CHECK (min_priority IN ('known','assumed')),
  created_at      TEXT    NOT NULL,
  updated_at      TEXT    NOT NULL,
  updated_by_kind TEXT    NOT NULL CHECK (updated_by_kind IN ('user','agent','trigger')),
  updated_by_id   TEXT    NOT NULL,
  CHECK ((window_start IS NULL) = (window_end IS NULL)),
  CHECK (window_start IS NULL OR zone IS NOT NULL)
);

-- The claim and the one-attempt rule (CN14): one row per subject, ever.
CREATE TABLE scheduler_start (
  subject_kind   TEXT NOT NULL CHECK (subject_kind IN ('queue','roadmap')),
  subject_id     INTEGER NOT NULL,
  decision_id    INTEGER NOT NULL,
  component_root TEXT    NOT NULL,
  claimed_at     TEXT    NOT NULL,
  loop_slug      TEXT,
  outcome        TEXT CHECK (outcome IS NULL OR outcome IN ('started','refused','lost')),
  outcome_at     TEXT,
  PRIMARY KEY (subject_kind, subject_id)
);

CREATE TABLE scheduler_decision (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  at             TEXT    NOT NULL,
  component_root TEXT    NOT NULL DEFAULT '',
  subject_kind   TEXT    NOT NULL CHECK (subject_kind IN ('queue','roadmap','root','daemon')),
  subject_id     INTEGER NOT NULL DEFAULT 0,
  decision       TEXT    NOT NULL CHECK (decision IN
                   ('started','deferred','skipped','refused','needs-you','lost',
                    'enabled','disabled','paused','resumed','settings')),
  reason         TEXT    NOT NULL DEFAULT '',
  summary        TEXT    NOT NULL,
  grant_revision TEXT    NOT NULL DEFAULT '',
  actor_kind     TEXT    NOT NULL CHECK (actor_kind IN ('user','agent','trigger','scheduler')),
  actor_id       TEXT    NOT NULL DEFAULT '',
  loop_slug      TEXT    NOT NULL DEFAULT ''
);
-- CN16: deliberately NO foreign key from the log to its subjects. A queue
-- item or roadmap item is deleted one day; the decision stands.

CREATE INDEX idx_scheduler_decision_subject ON scheduler_decision (subject_kind, subject_id, id);
CREATE INDEX idx_scheduler_decision_root    ON scheduler_decision (component_root, id);
CREATE INDEX idx_scheduler_decision_kind    ON scheduler_decision (decision, id);

CREATE TRIGGER scheduler_decision_no_update
BEFORE UPDATE ON scheduler_decision
BEGIN
  SELECT RAISE(ABORT, 'scheduler_decision is append-only');
END;

CREATE TRIGGER scheduler_decision_no_delete
BEFORE DELETE ON scheduler_decision
BEGIN
  SELECT RAISE(ABORT, 'scheduler_decision is append-only');
END;

-- ---------------------------------------------------------------------------
-- The dependency graph (T3's readiness will read it; CN14's edges).
-- ---------------------------------------------------------------------------
CREATE TABLE roadmap_dependency (
  item_id         INTEGER NOT NULL REFERENCES roadmap_item (id),
  depends_on_id   INTEGER NOT NULL REFERENCES roadmap_item (id),
  created_at      TEXT    NOT NULL,
  created_by_kind TEXT    NOT NULL CHECK (created_by_kind IN ('user','agent','trigger')),
  created_by_id   TEXT    NOT NULL,
  PRIMARY KEY (item_id, depends_on_id),
  CHECK (item_id <> depends_on_id)
);

CREATE TRIGGER roadmap_dependency_same_root
BEFORE INSERT ON roadmap_dependency
BEGIN
  SELECT RAISE(ABORT, 'roadmap_dependency: both items must belong to the same repository')
  WHERE (SELECT component_root FROM roadmap_item WHERE id = NEW.item_id)
     IS NOT (SELECT component_root FROM roadmap_item WHERE id = NEW.depends_on_id);
END;

CREATE TRIGGER roadmap_dependency_no_2cycle
BEFORE INSERT ON roadmap_dependency
BEGIN
  SELECT RAISE(ABORT, 'roadmap_dependency: the reverse edge already exists (a 2-cycle would deadlock the graph)')
  WHERE EXISTS (SELECT 1 FROM roadmap_dependency
                WHERE item_id = NEW.depends_on_id AND depends_on_id = NEW.item_id);
END;

-- The per-item autonomy ceilings (CN14/CN19): DEFAULT 'grant' keeps every
-- existing item under its S56 grant exactly as today; there is NO value
-- that widens — the CHECK is the whole vocabulary.
ALTER TABLE roadmap_item ADD COLUMN autonomy_cap TEXT NOT NULL DEFAULT 'grant'
  CHECK (autonomy_cap IN ('grant','none','outward'));
ALTER TABLE queue_item   ADD COLUMN autonomy_cap TEXT NOT NULL DEFAULT 'grant'
  CHECK (autonomy_cap IN ('grant','none','outward'));

-- Final guard: both columns exist on both tables and every legacy row read
-- 'grant' through the default (nothing silently widened).
CREATE TABLE zz_s27_cap_guard (n INTEGER CHECK (n = 0));
INSERT INTO zz_s27_cap_guard (n)
  SELECT (SELECT COUNT(*) FROM pragma_table_info('roadmap_item') WHERE name = 'autonomy_cap')
       + (SELECT COUNT(*) FROM pragma_table_info('queue_item')   WHERE name = 'autonomy_cap') - 2;
INSERT INTO zz_s27_cap_guard (n)
  SELECT COUNT(*) FROM roadmap_item WHERE autonomy_cap <> 'grant';
INSERT INTO zz_s27_cap_guard (n)
  SELECT COUNT(*) FROM queue_item WHERE autonomy_cap <> 'grant';
DROP TABLE zz_s27_cap_guard;

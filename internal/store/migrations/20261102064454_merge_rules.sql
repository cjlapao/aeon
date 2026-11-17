-- S57 T5: the merge rule, the DoD ledger, the merge event (VG10, VG13, VG14)
--
-- merge_rule: one row per component (root or child; a child's row REPLACES
-- the root's whole row when it has one — the VG3 rule), evaluated by T5's
-- pure evaluator (VG11). The DEFAULTS ARE THE BASELINE: a row with all
-- defaults behaves exactly as no row (today's mergeGate), which a test
-- proves. revision is the row's own counter — the event ledger's MAX is
-- the source of the next number, and the number never resets (P3).
CREATE TABLE merge_rule (
  component_id             TEXT PRIMARY KEY REFERENCES component(id),
  enabled                  INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  require_local            INTEGER NOT NULL DEFAULT 0 CHECK (require_local IN (0,1)),
  require_dod              TEXT NOT NULL DEFAULT 'none' CHECK (require_dod IN ('none','all_met')),
  required_checks          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(required_checks)),
  checks_policy            TEXT NOT NULL DEFAULT 'reported_green' CHECK (checks_policy IN ('reported_green','required_only')),
  no_checks                TEXT NOT NULL DEFAULT 'allow' CHECK (no_checks IN ('refuse','allow')),
  review_mode              TEXT NOT NULL DEFAULT 'forge' CHECK (review_mode IN ('forge','aeon','either','both')),
  aeon_review_agents       TEXT NOT NULL DEFAULT '["code-reviewer"]' CHECK (json_valid(aeon_review_agents)),
  require_threads_resolved INTEGER NOT NULL DEFAULT 0 CHECK (require_threads_resolved IN (0,1)),
  method                   TEXT NOT NULL DEFAULT 'squash' CHECK (method IN ('squash','merge','rebase')),
  revision                 INTEGER NOT NULL DEFAULT 1,
  updated_at               TEXT NOT NULL
);

-- The append-only audit: one row per changing write, same transaction as
-- the write, ops set|clear, no FK to the record (the rule row can be
-- cleared; the audit outlives it, like verify_command_event does).
CREATE TABLE merge_rule_event (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  component_id TEXT NOT NULL,
  revision     INTEGER NOT NULL,
  op           TEXT NOT NULL CHECK (op IN ('set','clear')),
  actor_kind   TEXT NOT NULL CHECK (actor_kind IN ('user','cli','agent')),
  actor_id     TEXT NOT NULL,
  -- a fixed-template sentence from the field enums — never free text
  -- (SEC-033).
  summary      TEXT NOT NULL,
  before_json  TEXT CHECK (before_json IS NULL OR json_valid(before_json)),
  after_json   TEXT CHECK (after_json IS NULL OR json_valid(after_json)),
  at           TEXT NOT NULL
);
CREATE UNIQUE INDEX uq_merge_rule_event_revision ON merge_rule_event (component_id, revision);
CREATE INDEX idx_merge_rule_event_component ON merge_rule_event (component_id, id);
CREATE TRIGGER merge_rule_event_no_update BEFORE UPDATE ON merge_rule_event
BEGIN SELECT RAISE(ABORT, 'merge_rule_event is append-only'); END;
CREATE TRIGGER merge_rule_event_no_delete BEFORE DELETE ON merge_rule_event
BEGIN SELECT RAISE(ABORT, 'merge_rule_event is append-only'); END;

-- The definition-of-done ledger (VG13). The daemon materialises it from an
-- APPROVED brief (never the agent); the rows are loop-scoped (DB4) and
-- idempotent on (brief_id, idx). A brief version has its own id, so a new
-- version is a new set of rows at the same idx: materialising it carries a
-- previous version's `met` over to the new row ONLY when the text is
-- byte-equal (a changed text resets to unchecked and is told in a `reset`
-- event). The store never deletes an old version's rows — they are history.
CREATE TABLE dod_item (
  id             TEXT PRIMARY KEY,
  loop_slug      TEXT NOT NULL,
  component_root TEXT NOT NULL,
  brief_id       TEXT NOT NULL,
  brief_version  INTEGER NOT NULL,
  idx            INTEGER NOT NULL,
  text           TEXT NOT NULL,
  state          TEXT NOT NULL DEFAULT 'unchecked' CHECK (state IN ('unchecked','met','unmet','unverifiable')),
  -- '' is the honest empty: an item with no evidence yet. A `met` always
  -- names its evidence (the store enforces it before SQL).
  evidence_kind  TEXT NOT NULL DEFAULT '' CHECK (evidence_kind IN ('','verify_run','manual')),
  evidence_ref   TEXT NOT NULL DEFAULT '',
  actor_kind     TEXT,
  actor_id       TEXT,
  updated_at     TEXT NOT NULL,
  UNIQUE (brief_id, idx)
);
CREATE INDEX idx_dod_item_scope ON dod_item (component_root, loop_slug);

-- The ledger's audit: materialize (a row appears or a text changes under a
-- new brief version, resetting a stale state), mark (a state change with
-- its evidence), one row per change, append-only.
CREATE TABLE dod_event (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  item_id    TEXT NOT NULL,
  op         TEXT NOT NULL CHECK (op IN ('materialize','mark','reset')),
  from_state TEXT,
  to_state   TEXT,
  evidence   TEXT,
  actor_kind TEXT,
  actor_id   TEXT,
  at         TEXT NOT NULL
);
CREATE INDEX idx_dod_event_item ON dod_event (item_id, id);
CREATE TRIGGER dod_event_no_update BEFORE UPDATE ON dod_event
BEGIN SELECT RAISE(ABORT, 'dod_event is append-only'); END;
CREATE TRIGGER dod_event_no_delete BEFORE DELETE ON dod_event
BEGIN SELECT RAISE(ABORT, 'dod_event is append-only'); END;

-- The missing `merged` fact (VG14): written by the engine AFTER a
-- successful forge.merge dispatch, idempotent on the unique key (the
-- check-then-act re-entry "already merged" succeeds and writes at most
-- one row). loop_slug/component_root are keyed without FKs (the loop rows
-- move out on archive — store/archive.go — and the fact outlives them,
-- like the verify evidence does); pr is NOT NULL so the unique key is a
-- real key (a merge event without a PR number is not a PR merge, and
-- NULLs would be distinct from each other and break the idempotency).
CREATE TABLE merge_event (
  id             TEXT PRIMARY KEY,
  loop_slug      TEXT NOT NULL,
  component_root TEXT NOT NULL,
  component_id   TEXT,
  pr             INTEGER NOT NULL,
  merge_sha      TEXT NOT NULL,
  method         TEXT,
  base_ref       TEXT,
  head_sha       TEXT,
  rule_revision  INTEGER,
  evidence       TEXT NOT NULL,
  verify_run_id  TEXT,
  merged_at      TEXT NOT NULL,
  actor_kind     TEXT,
  actor_id       TEXT,
  UNIQUE (component_root, pr, merge_sha)
);
CREATE INDEX idx_merge_event_scope ON merge_event (component_root, loop_slug);

-- End guard (the 0019 discipline): the new FK-bearing table (merge_rule →
-- component) left no dangling reference — the CHECK makes the migration
-- itself abort if not.
CREATE TABLE zz_s57_merge_rules_fk_guard (
  n INTEGER CHECK (n = 0)
);
INSERT INTO zz_s57_merge_rules_fk_guard (n)
SELECT count(*) FROM pragma_foreign_key_check();

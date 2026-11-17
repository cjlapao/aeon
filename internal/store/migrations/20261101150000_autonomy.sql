-- S56 T3: the autonomy authority tables (AL10). autonomy_grant is the record
-- (a human-authored grant per root component, allow or deny); autonomy_change
-- is its append-only audit (one row per changing write, in the same
-- transaction); autonomy_use is the append-only use log (every unattended
-- effect taken under a grant, begun then completed/failed); loop_autonomy_pin
-- is the immutable snapshot a policy-resolved loop is pinned with (AL8). The
-- two component triggers are additive on S44-extra's table: an allow sits on
-- a root only, and a component with an active allow cannot be attached under
-- another. Nothing is seeded (AL16); the CHECK and the store are the door
-- (AU1), and the triggers are the backstop. No FK from loop_autonomy_pin to
-- loop: store/archive.go moves loop rows out, so the pin outlives them like
-- history does.
CREATE TABLE autonomy_grant (
  id              TEXT PRIMARY KEY,
  node_id         TEXT NOT NULL REFERENCES component(id),
  subject_kind    TEXT NOT NULL CHECK (subject_kind IN ('action','class')),
  subject         TEXT NOT NULL CHECK (subject_kind = 'action' OR subject = 'outward'),
  state           TEXT NOT NULL CHECK (state IN ('allow','deny')),
  conditions      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(conditions)),
  note            TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 512),
  expires_at      TEXT,
  -- an allow (a widening) is authored by a user only; a deny (a veto, the
  -- restrictive class of revoke, AL13) admits a user or the cli direct door.
  -- Amended 2026-11-01 at S56 T9's pre-flight; the slice was unmerged, so this
  -- never shipped as the user-only literal.
  created_by_kind TEXT NOT NULL CHECK (created_by_kind = 'user' OR (state = 'deny' AND created_by_kind = 'cli')),
  created_by_id   TEXT NOT NULL,
  proposed_by     TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL,
  revoked_at      TEXT,
  revoked_by_kind TEXT CHECK (revoked_by_kind IS NULL OR revoked_by_kind IN ('user','cli')),
  revoked_by_id   TEXT,
  revoked_reason  TEXT,
  -- the revoked_* trio is all-or-nothing: a grant is revoked once, or not.
  CHECK ((revoked_at IS NULL) = (revoked_by_id IS NULL)),
  CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL)),
  -- a deny carries no conditions and no expiry; a merge grant's expiry is
  -- enforced in Go (a bound the CHECK cannot express without a clock).
  CHECK (state = 'allow' OR (expires_at IS NULL AND conditions = '{}'))
);
CREATE INDEX idx_autonomy_grant_node ON autonomy_grant (node_id, state);

CREATE TABLE autonomy_change (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  at           TEXT NOT NULL,
  root_id      TEXT NOT NULL,
  grant_id     TEXT NOT NULL,
  change_kind  TEXT NOT NULL CHECK (change_kind IN ('grant','deny','revoke')),
  actor_kind   TEXT NOT NULL CHECK (actor_kind IN ('user','cli')),
  actor_id     TEXT NOT NULL,
  proposed_by  TEXT NOT NULL DEFAULT '',
  -- a fixed-template sentence from ids and enums, never user text (SEC-033).
  summary      TEXT NOT NULL
);
CREATE INDEX idx_autonomy_change_root ON autonomy_change (root_id, id);
CREATE TRIGGER autonomy_change_no_update BEFORE UPDATE ON autonomy_change
BEGIN SELECT RAISE(ABORT, 'autonomy_change is append-only'); END;
CREATE TRIGGER autonomy_change_no_delete BEFORE DELETE ON autonomy_change
BEGIN SELECT RAISE(ABORT, 'autonomy_change is append-only'); END;

CREATE TABLE autonomy_use (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  at           TEXT NOT NULL,
  loop_id      TEXT NOT NULL,
  grant_id     TEXT NOT NULL,
  grant_rev    INTEGER NOT NULL,
  action       TEXT NOT NULL,
  phase        TEXT NOT NULL CHECK (phase IN ('begun','completed','failed')),
  -- a begun row has no begun_id; a completed/failed row points at its begun.
  begun_id     INTEGER,
  evidence_ref TEXT NOT NULL DEFAULT '',
  CHECK ((phase = 'begun') = (begun_id IS NULL))
);
CREATE INDEX idx_autonomy_use_grant ON autonomy_use (grant_id, at);
CREATE INDEX idx_autonomy_use_loop ON autonomy_use (loop_id);
CREATE TRIGGER autonomy_use_no_update BEFORE UPDATE ON autonomy_use
BEGIN SELECT RAISE(ABORT, 'autonomy_use is append-only'); END;
CREATE TRIGGER autonomy_use_no_delete BEFORE DELETE ON autonomy_use
BEGIN SELECT RAISE(ABORT, 'autonomy_use is append-only'); END;

CREATE TABLE loop_autonomy_pin (
  loop_id   TEXT PRIMARY KEY,   -- no FK: store/archive.go moves loop rows out
  root_id   TEXT NOT NULL,
  rev       INTEGER NOT NULL,
  origin    TEXT NOT NULL CHECK (origin IN ('schedule','webhook','event','scheduler')),
  labels    TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(labels)),
  allowed   TEXT NOT NULL CHECK (json_valid(allowed)),
  pinned_at TEXT NOT NULL
);
-- a pin is immutable (AL6/AL8): a later grant never widens it, a revocation
-- is seen live, and the row is never rewritten or deleted.
CREATE TRIGGER loop_autonomy_pin_no_update BEFORE UPDATE ON loop_autonomy_pin
BEGIN SELECT RAISE(ABORT, 'loop_autonomy_pin is immutable (S56 AL6/AL8)'); END;
CREATE TRIGGER loop_autonomy_pin_no_delete BEFORE DELETE ON loop_autonomy_pin
BEGIN SELECT RAISE(ABORT, 'loop_autonomy_pin is immutable (S56 AL6/AL8)'); END;

-- Backstop for the store's allow-on-a-root rule: an allow may only sit on a
-- root component (not a child, not an archived node).
CREATE TRIGGER autonomy_grant_allow_root_only BEFORE INSERT ON autonomy_grant
BEGIN
  SELECT RAISE(ABORT, 'autonomy: allow grants sit on a root component')
  WHERE NEW.state = 'allow' AND EXISTS (
    SELECT 1 FROM component c WHERE c.id = NEW.node_id
      AND (c.parent_id IS NOT NULL OR c.archived_at IS NOT NULL));
END;

-- A component that carries an active allow may not be attached under another
-- root (the child would inherit a grant its new root never authorised).
CREATE TRIGGER autonomy_grant_no_attach BEFORE UPDATE OF parent_id ON component
BEGIN
  SELECT RAISE(ABORT, 'autonomy: revoke the autonomy grants of X before attaching it under another component')
  WHERE NEW.parent_id IS NOT NULL AND EXISTS (
    SELECT 1 FROM autonomy_grant g
      WHERE g.node_id = NEW.id AND g.state = 'allow' AND g.revoked_at IS NULL);
END;

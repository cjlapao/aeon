-- S59 T10 (EV12, EV14) · the ledger learns a third owner: the per-loop network.
--
-- T2 shipped `owner` with the net `owner = 'loop' OR owner GLOB 'service:*'`,
-- which was the whole vocabulary while a provision meant one container. T10
-- gives a loop a user-defined network (`aeon-<slug>-net`) that its services and
-- its app container share, and a network is a resource the daemon created, owns
-- and must remove — so it needs a ledger row like anything else, or nothing
-- finds it after a crash: reconcile reads live rows, and a resource with no row
-- is invisible to the only code that cleans up. Reconcile's `List` returns
-- containers, so an orphan network cannot be found by label alone; the row is
-- what makes it findable.
--
-- SQLite cannot widen a CHECK in place, so the table is rebuilt the house way:
-- build _new with the widened net, copy, drop, rename, recreate the two indexes
-- by name (the rebuild detaches them), and guard the copy with a count that
-- raises rather than silently losing a row. No table references this one, so no
-- parking is needed (checked: no `REFERENCES environment_provision` anywhere in
-- the migrations, schema or the store).
CREATE TABLE environment_provision_new (
  id             TEXT PRIMARY KEY,
  environment_id TEXT NOT NULL REFERENCES environment (id),
  component_root TEXT NOT NULL,
  loop_slug      TEXT NOT NULL,
  backend        TEXT NOT NULL CHECK (backend IN ('host-inherit','host-scrubbed','container','vm')),
  handle         TEXT NOT NULL DEFAULT '',
  owner          TEXT NOT NULL DEFAULT 'loop',
  state          TEXT NOT NULL CHECK (state IN ('provisioning','ready','tearing_down','removed','failed')),
  created_at     TEXT NOT NULL,
  ready_at       TEXT,
  removed_at     TEXT,
  last_error     TEXT,
  -- a settled row is the only one with removed_at, and a row still
  -- provisioning has never been ready: the two facts the marks maintain, said
  -- once in SQL so a raw statement cannot leak the third state.
  CHECK (removed_at IS NULL OR state = 'removed'),
  CHECK (ready_at IS NULL OR state <> 'provisioning'),
  -- 'loop' is the app container, 'network' the per-loop network T10 creates,
  -- every other owner is a service (EV14). The name shape is the store's door;
  -- this is the net.
  CHECK (owner = 'loop' OR owner = 'network' OR owner GLOB 'service:*')
);

INSERT INTO environment_provision_new
  (id, environment_id, component_root, loop_slug, backend, handle, owner, state,
   created_at, ready_at, removed_at, last_error)
SELECT id, environment_id, component_root, loop_slug, backend, handle, owner, state,
   created_at, ready_at, removed_at, last_error
FROM environment_provision;

-- The copy guard: one row, whose CHECK is the equality the copy must satisfy.
CREATE TABLE zz_provision_owner_guard (n INTEGER CHECK (n = 0));
INSERT INTO zz_provision_owner_guard (n)
SELECT (SELECT count(*) FROM environment_provision) - (SELECT count(*) FROM environment_provision_new);

DROP TABLE environment_provision;
ALTER TABLE environment_provision_new RENAME TO environment_provision;
DROP TABLE zz_provision_owner_guard;

CREATE UNIQUE INDEX environment_provision_live ON environment_provision (component_root, loop_slug, owner)
  WHERE removed_at IS NULL;
CREATE INDEX idx_environment_provision_loop ON environment_provision (component_root, loop_slug, id);

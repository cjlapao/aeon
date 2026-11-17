-- S60 T2: the merge queue — items with physical one-slot/one-open invariants, the append-only history, per-repository config (design MQ4)
CREATE TABLE merge_queue_item (
  id                  TEXT PRIMARY KEY,                      -- 'mq-' || newID()
  component_root      TEXT NOT NULL REFERENCES component_root (component_root),
  base_branch         TEXT NOT NULL,
  branch              TEXT NOT NULL,
  loop_slug           TEXT,                                  -- the building loop; plain TEXT, NO FK (F20)
  head_sha            TEXT NOT NULL,                         -- branch tip at enqueue / last catch-up
  base_sha            TEXT NOT NULL,                         -- base tip last integrated
  integration_branch  TEXT,
  integration_sha     TEXT,
  pr_number           INTEGER,
  verified_sha        TEXT,                                  -- the head the last green gate covered (MQ11)
  landed_sha          TEXT,
  landing_loop_slug   TEXT,
  resolve_loop_slug   TEXT,
  status              TEXT NOT NULL CHECK (status IN ('queued','catching_up','verifying','ready','merging','merged','conflicted','failed','removed')),
  reason_kind         TEXT CHECK (reason_kind IS NULL OR reason_kind IN
                        ('denied','conflict','gate-failed','head-moved','protected','forge','git','stale-exhausted','removed-by-person','loop-stopped','unknown')),
  position            INTEGER NOT NULL,
  attended            INTEGER NOT NULL DEFAULT 0 CHECK (attended IN (0,1)),   -- declared at enqueue, never inferred (S24 D5/D6 shape)
  catch_ups           INTEGER NOT NULL DEFAULT 0,
  enqueued_by_kind    TEXT NOT NULL CHECK (enqueued_by_kind IN ('user','cli','blueprint','daemon','proposal')),
  enqueued_by_id      TEXT NOT NULL,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL
);
-- One item holds the landing slot per (root, base) at a time; `conflicted`
-- releases the slot (a conflict must not stall the queue — OD2).
CREATE UNIQUE INDEX merge_queue_one_slot ON merge_queue_item (component_root, base_branch)
  WHERE status IN ('catching_up','verifying','ready','merging');
-- One live item per branch: a merged row does not block re-enqueue.
CREATE UNIQUE INDEX merge_queue_one_open ON merge_queue_item (component_root, branch)
  WHERE status IN ('queued','catching_up','verifying','ready','merging','conflicted');
CREATE INDEX merge_queue_order ON merge_queue_item (component_root, base_branch, position);

CREATE TABLE merge_queue_event (                           -- append-only by trigger (P3)
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  component_root  TEXT NOT NULL,
  item_id         TEXT,                                    -- NULL for a config change; NO FK (history outlives the record)
  kind            TEXT NOT NULL CHECK (kind IN ('transition','reorder','config')),
  from_status     TEXT, to_status TEXT,
  reason_kind     TEXT,
  actor_kind      TEXT NOT NULL CHECK (actor_kind IN ('user','cli','blueprint','daemon','agent')),
  actor_id        TEXT NOT NULL,
  detail          TEXT NOT NULL DEFAULT '{}',              -- ids, counts, enums, SHAs, PATHS — never hunk text, never user prose
  at              TEXT NOT NULL
);
CREATE TRIGGER merge_queue_event_no_update BEFORE UPDATE ON merge_queue_event BEGIN SELECT RAISE(ABORT,'merge_queue_event is append-only'); END;
CREATE TRIGGER merge_queue_event_no_delete BEFORE DELETE ON merge_queue_event BEGIN SELECT RAISE(ABORT,'merge_queue_event is append-only'); END;

CREATE TABLE merge_queue_config (                          -- one row per repository; absent row = mode 'off'
  component_root  TEXT PRIMARY KEY REFERENCES component_root (component_root),
  mode            TEXT NOT NULL DEFAULT 'off'      CHECK (mode IN ('off','manual','auto')),
  conflict_policy TEXT NOT NULL DEFAULT 'ask'      CHECK (conflict_policy IN ('ask','resolve','block')),
  land_method     TEXT NOT NULL DEFAULT 'squash'   CHECK (land_method IN ('squash','merge')),
  max_catch_ups   INTEGER NOT NULL DEFAULT 3       CHECK (max_catch_ups BETWEEN 1 AND 10),
  updated_at      TEXT NOT NULL,
  updated_by_kind TEXT NOT NULL, updated_by_id TEXT NOT NULL
);

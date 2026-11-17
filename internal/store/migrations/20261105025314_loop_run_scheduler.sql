-- S27 T2 CN15 · the loop_run rebuild admitting the `scheduler` provenance
-- word. The 0039 precedent to the letter (AGENTS §3's 0019 lesson): a fresh
-- table with the widened CHECK, an explicit-column copy, a guard that the
-- copy moved EXACTLY every row, drop, rename, and idx_loop_run_state
-- recreated BY NAME — a rebuild that forgets its index dies on
-- 'already exists', and a rebuild that loses a row dies here.
--
-- The copy shape is byte-identical to 0039's rebuild except
-- started_by_kind's CHECK, widened from ('user','agent','trigger','event')
-- to admit 'scheduler'. loop_run has no children (measured at T2: no table
-- names it in a REFERENCES clause), so nothing parks; its own FK to `loop`
-- rides the new declaration, and the final pragma_foreign_key_check guard
-- proves the whole database is consistent after the swap.

DROP INDEX IF EXISTS idx_loop_run_state;

CREATE TABLE loop_run_s27 (
  loop_id            TEXT    NOT NULL REFERENCES loop (id) ON DELETE CASCADE,
  run_seq            INTEGER NOT NULL CHECK (run_seq >= 1),
  blueprint          TEXT    NOT NULL,
  blueprint_version  INTEGER NOT NULL,
  content            TEXT    NOT NULL,
  runtime_json       TEXT,
  state              TEXT    NOT NULL CHECK (state IN ('running','done','failed','cancelled')),
  started_by_kind    TEXT    NOT NULL CHECK (started_by_kind IN ('user','agent','trigger','event','scheduler')),
  started_by_id      TEXT    NOT NULL DEFAULT '',
  kickoff_message_id INTEGER,
  goal_id            TEXT,
  goal_version       INTEGER,
  task_id            TEXT,
  started_at         TEXT    NOT NULL,
  ended_at           TEXT,
  PRIMARY KEY (loop_id, run_seq),
  CONSTRAINT ck_loop_run_end CHECK (
       (state = 'running' AND ended_at IS NULL)
    OR (state <> 'running' AND ended_at IS NOT NULL))
);

INSERT INTO loop_run_s27 (loop_id, run_seq, blueprint, blueprint_version, content, runtime_json,
                          state, started_by_kind, started_by_id, kickoff_message_id,
                          goal_id, goal_version, task_id, started_at, ended_at)
  SELECT loop_id, run_seq, blueprint, blueprint_version, content, runtime_json,
         state, started_by_kind, started_by_id, kickoff_message_id,
         goal_id, goal_version, task_id, started_at, ended_at
  FROM loop_run;

-- The copy guard: the moved count equals the old count, to the row.
CREATE TABLE zz_s27_copy_guard (n INTEGER CHECK (n = 0));
INSERT INTO zz_s27_copy_guard (n)
  SELECT (SELECT COUNT(*) FROM loop_run) - (SELECT COUNT(*) FROM loop_run_s27);

DROP TABLE loop_run;
ALTER TABLE loop_run_s27 RENAME TO loop_run;
CREATE INDEX idx_loop_run_state ON loop_run (loop_id, state);
DROP TABLE zz_s27_copy_guard;

-- The vocabulary guard: the rebuilt row-shape really admits the new word.
CREATE TABLE zz_s27_check_guard (n INTEGER CHECK (n = 1));
INSERT INTO zz_s27_check_guard (n)
  SELECT COUNT(*) FROM sqlite_master
  WHERE type = 'table' AND name = 'loop_run'
    AND sql LIKE '%started_by_kind IN (''user'',''agent'',''trigger'',''event'',''scheduler'')%';
DROP TABLE zz_s27_check_guard;

-- The index guard: named, present, exactly one.
CREATE TABLE zz_s27_index_guard (n INTEGER CHECK (n = 0));
INSERT INTO zz_s27_index_guard (n) SELECT 1
  WHERE (SELECT COUNT(*) FROM sqlite_master
         WHERE type = 'index' AND tbl_name = 'loop_run' AND name = 'idx_loop_run_state') <> 1;
DROP TABLE zz_s27_index_guard;

-- The foreign-key guard, over the WHOLE database, after the swap.
CREATE TABLE zz_s27_fk_guard (n INTEGER CHECK (n = 0));
INSERT INTO zz_s27_fk_guard (n)
  SELECT COUNT(*) FROM pragma_foreign_key_check();
DROP TABLE zz_s27_fk_guard;

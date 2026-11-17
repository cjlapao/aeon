-- S52 T1: the memory the daemon can manage — retraction columns on learning
-- (LM2: retract is a stamp, never a delete) and learning_change, the
-- append-only audit every managed write rides (LM3). Three ALTERs, one new
-- table, one index, two refusal triggers. Nothing here rewrites a row: the
-- sibling Go step (name→slug scope_keys) rides Migrate(), not the ledger.
--
-- The retraction is a stamp. SQLite cannot tighten a CHECK in place and the
-- house law is one DDL source, forward-only — so the retraction columns are
-- ADDed, the learning table is never rebuilt, and every existing row rides
-- out with the defaults: not retracted, no reason, nobody stamped it.
ALTER TABLE learning ADD COLUMN retracted_at TEXT;
ALTER TABLE learning ADD COLUMN retracted_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE learning ADD COLUMN retracted_by TEXT NOT NULL DEFAULT '';

-- learning_change is the audit (LM3): one row per managed act — the add
-- that wrote it, the retract that stamped it away, the restore that stamped
-- it back. op is the closed three; actor_kind the closed four. The CHECK
-- that makes the table honest is the last one: an AGENT may only ever
-- appear on an `add` line. Agents cannot retract and cannot restore — the
-- door that answers to them does not exist until the proposal path (S52 T4)
-- carries a person's decision through it; a retraction is a person's word
-- about memory, and the schema says so.
--
-- There is NO foreign key to learning on purpose: a learning keeps its
-- audit trail forever, and an audit table that could block the row it
-- describes would be a second owner of that row's fate. The id is a plain
-- pointer the writers keep honest.
CREATE TABLE learning_change (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    learning_id INTEGER NOT NULL,
    op          TEXT    NOT NULL CHECK (op IN ('add', 'retract', 'restore')),
    summary     TEXT    NOT NULL CHECK (length(summary) BETWEEN 1 AND 1024),
    actor_kind  TEXT    NOT NULL CHECK (actor_kind IN ('user', 'cli', 'agent', 'system')),
    actor_id    TEXT    NOT NULL CHECK (length(actor_id) BETWEEN 1 AND 256),
    occurred_at TEXT    NOT NULL,
    CHECK (actor_kind <> 'agent' OR op = 'add')
);

CREATE INDEX idx_learning_change_learning ON learning_change (learning_id, id);

-- Append-only by trigger, the house pattern: an audit line that could be
-- edited or removed is a memory of the audit, not an audit.
CREATE TRIGGER learning_change_no_update
BEFORE UPDATE ON learning_change
BEGIN
    SELECT RAISE(ABORT, 'learning_change is append-only (S52 LM3)');
END;

CREATE TRIGGER learning_change_no_delete
BEFORE DELETE ON learning_change
BEGIN
    SELECT RAISE(ABORT, 'learning_change is append-only (S52 LM3)');
END;

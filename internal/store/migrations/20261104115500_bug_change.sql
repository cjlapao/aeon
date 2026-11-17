-- S51 T1: bug_change — the append-only audit of every bug write (TM2).
--
-- One row per changing write (file, update, priority, duplicate, validate,
-- handoff, transition), written in the write's own transaction,
-- fixed-template summaries — ids and enums only, never raw user text (the
-- SEC-033 rule the policy_change, automation_change and roadmap_change
-- audits already keep).
--
-- actor_kind carries 'agent' where its siblings do not, and the final CHECK
-- is the reason: D7 lets an AGENT file a claim, and `file` is the ONLY op
-- an agent may ever carry. Every other op belongs to user, cli or system —
-- an agent setting priority, confirming a duplicate, validating, handing
-- off or editing is refused at the database itself, not by convention
-- (TM2's net under TM6/D5/N3/D2).
--
-- No FK to bug: history outlives the row (the policy_change argument,
-- carried — a cascade that deletes the audit of a deletion is the record
-- that disappears when you ask about it).
--
-- Append-only by trigger, the S44 setting_event / S45 policy_change /
-- S49 automation_change / S50 roadmap_change precedent: an append is the
-- only mutation.

CREATE TABLE bug_change (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  bug_id INTEGER NOT NULL,
  op TEXT NOT NULL CHECK (op IN ('file','update','priority','duplicate','validate','handoff','transition')),
  summary TEXT NOT NULL CHECK (length(summary) BETWEEN 1 AND 1024),
  actor_kind TEXT NOT NULL CHECK (actor_kind IN ('user','cli','agent','system')),
  actor_id TEXT NOT NULL CHECK (length(actor_id) BETWEEN 1 AND 256),
  occurred_at TEXT NOT NULL,
  CHECK (actor_kind <> 'agent' OR op = 'file')
);
CREATE INDEX idx_bug_change_bug ON bug_change (bug_id, id);
CREATE TRIGGER bug_change_no_update BEFORE UPDATE ON bug_change BEGIN SELECT RAISE(ABORT, 'bug_change is append-only (S51 TM2)'); END;
CREATE TRIGGER bug_change_no_delete BEFORE DELETE ON bug_change BEGIN SELECT RAISE(ABORT, 'bug_change is append-only (S51 TM2)'); END;

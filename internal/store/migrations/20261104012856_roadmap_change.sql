-- S50 T1: roadmap_change — the append-only audit of every roadmap_item write (RM2).
--
-- One row per changing write (add, update, cancel, estimate, priority,
-- ideate, plan, materialize, transition), written in the write's own
-- transaction, fixed-template summaries — ids, statuses and enums only,
-- never raw user text (the SEC-033 rule the policy_change and
-- automation_change audits already keep).
--
-- actor_kind widens past the S45/S49 pairs to carry 'system': the
-- reactor-driven transitions (brief-agreed, done, the materialization
-- link) are writes too, and a person must be able to see WHAT MOVED ON
-- ITS OWN — those rows carry actor 'system:reactor'.
--
-- No FK to roadmap_item: history outlives the item (the policy_change
-- precedent, PL: a cascade that deletes the audit of a deletion is the
-- record that disappears when you ask about it).
--
-- Append-only by trigger, the S44 setting_event / S45 policy_change /
-- S49 automation_change precedent: an append is the only mutation.

CREATE TABLE roadmap_change (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  item_id INTEGER NOT NULL,
  op TEXT NOT NULL CHECK (op IN ('add','update','cancel','estimate','priority','ideate','plan','materialize','transition')),
  summary TEXT NOT NULL CHECK (length(summary) BETWEEN 1 AND 1024),
  actor_kind TEXT NOT NULL CHECK (actor_kind IN ('user','cli','system')),
  actor_id TEXT NOT NULL CHECK (length(actor_id) BETWEEN 1 AND 256),
  occurred_at TEXT NOT NULL
);
CREATE INDEX idx_roadmap_change_item ON roadmap_change (item_id, id);
CREATE TRIGGER roadmap_change_no_update BEFORE UPDATE ON roadmap_change BEGIN SELECT RAISE(ABORT, 'roadmap_change is append-only (S50 RM2)'); END;
CREATE TRIGGER roadmap_change_no_delete BEFORE DELETE ON roadmap_change BEGIN SELECT RAISE(ABORT, 'roadmap_change is append-only (S50 RM2)'); END;

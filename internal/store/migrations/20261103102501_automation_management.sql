-- S49 T1: automation management storage (design AN3-AN6).
--
-- trigger_schedule gains the creator S40's event_trigger already has —
-- lock 3 of AU1 extended to the schedule family (AN3): only a person
-- authors a trigger, and the CHECK is the net under the Go door.
-- Pre-existing rows get created_by_id '' meaning "predates S49's
-- provenance" — never a fabricated id.
--
-- Both families gain archive columns (AN6): archive is a state, never a
-- delete (AU1: a trigger is never deleted — disabled, with a reason).
--
-- automation_change is the append-only audit of every authority change
-- (AN4): one row per changing write, in the write's own transaction,
-- fixed-template summaries, ids and enums only — never raw user text
-- (SEC-033). No FK: history outlives archive.
--
-- automation_fire_view unifies the two fire tables through AU12's outcome
-- mapping (AN5). A view has no write path and no new truth. Mapping, per
-- T0 item 5: fired→started, refused→refused, skipped→skipped,
-- could_not_say→could-not-say, did_not_fire→skipped (AU12's forward table
-- merges limited AND skipped(disabled) into did_not_fire; the reverse
-- arrow cannot recover which, so reason stays verbatim and carries the
-- distinction). An event claim with no decision (outcome NULL) is not an
-- answer and earns no row. at = the wall instant the fire is ABOUT:
-- trigger_fire.instant (the instant the schedule was due at),
-- event_trigger_fire.decided_at (the moment the answer was written).
-- fid is the source row's id, exposed for the unified log's cursor paging.

ALTER TABLE trigger_schedule ADD COLUMN created_by_kind TEXT NOT NULL DEFAULT 'user' CHECK (created_by_kind = 'user');
ALTER TABLE trigger_schedule ADD COLUMN created_by_id   TEXT NOT NULL DEFAULT '';
ALTER TABLE trigger_schedule ADD COLUMN archived_at     TEXT;
ALTER TABLE trigger_schedule ADD COLUMN archived_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE event_trigger    ADD COLUMN archived_at     TEXT;
ALTER TABLE event_trigger    ADD COLUMN archived_reason TEXT NOT NULL DEFAULT '';

CREATE TABLE automation_change (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source TEXT NOT NULL CHECK (source IN ('schedule','webhook','event')),
  trigger_id TEXT NOT NULL CHECK (length(trigger_id) BETWEEN 1 AND 128),
  op TEXT NOT NULL CHECK (op IN ('create','update','enable','disable','pause','resume','fire_now','archive','unarchive')),
  summary TEXT NOT NULL CHECK (length(summary) BETWEEN 1 AND 1024),
  actor_kind TEXT NOT NULL CHECK (actor_kind IN ('user','cli')),
  actor_id TEXT NOT NULL CHECK (length(actor_id) BETWEEN 1 AND 256),
  occurred_at TEXT NOT NULL
);
CREATE INDEX idx_automation_change_trigger ON automation_change (source, trigger_id, id);
CREATE TRIGGER automation_change_no_update BEFORE UPDATE ON automation_change BEGIN SELECT RAISE(ABORT, 'automation_change is append-only (S49 AN4)'); END;
CREATE TRIGGER automation_change_no_delete BEFORE DELETE ON automation_change BEGIN SELECT RAISE(ABORT, 'automation_change is append-only (S49 AN4)'); END;

CREATE VIEW automation_fire_view AS
  SELECT s.kind AS source,
         f.schedule_id AS trigger_id,
         f.instant AS at,
         CASE f.outcome
           WHEN 'fired' THEN 'started'
           WHEN 'did_not_fire' THEN 'skipped'
           WHEN 'could_not_say' THEN 'could-not-say'
           ELSE f.outcome
         END AS outcome,
         f.reason AS reason,
         NULLIF(f.loop_id, '') AS loop_id,
         CAST(NULL AS TEXT) AS loop_slug,
         CAST(NULL AS TEXT) AS subject_kind,
         CAST(NULL AS TEXT) AS subject_id,
         CAST(NULL AS INTEGER) AS depth,
         '' AS cost_note,
         f.id AS fid
    FROM trigger_fire f
    JOIN trigger_schedule s ON s.id = f.schedule_id
  UNION ALL
  SELECT 'event',
         f.trigger_id,
         f.decided_at,
         f.outcome,
         f.reason,
         f.loop_id,
         f.loop_slug,
         f.subject_kind,
         f.subject_id,
         f.depth,
         f.cost_note,
         f.id
    FROM event_trigger_fire f
   WHERE f.outcome IS NOT NULL;

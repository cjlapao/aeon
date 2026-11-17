-- The event journal becomes addressable per loop: event gets loop_id and step_id, filled for every event that names them and backfilled from stored payloads, so one loop's history is one indexed read instead of a scan of the daemon's whole history.
ALTER TABLE event ADD COLUMN loop_id TEXT;
ALTER TABLE event ADD COLUMN step_id INTEGER;
UPDATE event SET step_id = json_extract(payload, '$.stepId')
  WHERE json_valid(payload) AND json_type(payload, '$.stepId') = 'integer';
UPDATE event SET loop_id = json_extract(payload, '$.loopId')
  WHERE json_valid(payload) AND json_type(payload, '$.loopId') = 'text';
UPDATE event SET loop_id = (
    SELECT l.id FROM step s JOIN loop l ON l.slug = s.loop_slug AND l.component_root = s.component_root
    WHERE s.id = event.step_id)
  WHERE loop_id IS NULL AND step_id IS NOT NULL;
UPDATE event SET loop_slug = (SELECT l.slug FROM loop l WHERE l.id = event.loop_id)
  WHERE loop_slug IS NULL AND loop_id IS NOT NULL;
CREATE INDEX idx_event_loop ON event (loop_id, id);

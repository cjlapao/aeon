-- One row per step of a loop with what the step has done so far: model turns, tool calls and errors, tokens, cost and
-- model time. The loop page reads these instead of rebuilding them from the event journal, so a long loop opens with
-- every step's summary at once and pages older steps like it pages chat. Kept up to date as events are appended;
-- backfilled here from the events already stored.
CREATE TABLE step_summary (
  step_id          INTEGER PRIMARY KEY,
  loop_id          TEXT NOT NULL,
  lane             TEXT NOT NULL DEFAULT '',
  turns            INTEGER NOT NULL DEFAULT 0,
  tool_calls       INTEGER NOT NULL DEFAULT 0,
  tool_errors      INTEGER NOT NULL DEFAULT 0,
  input_tokens     INTEGER NOT NULL DEFAULT 0,
  output_tokens    INTEGER NOT NULL DEFAULT 0,
  reasoning_tokens INTEGER NOT NULL DEFAULT 0,
  wall_ms          INTEGER NOT NULL DEFAULT 0,
  spend_state       TEXT NOT NULL DEFAULT 'unknown',
  spend_usd         REAL,
  first_event      INTEGER NOT NULL DEFAULT 0,
  last_event       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_step_summary_loop ON step_summary (loop_id, step_id);

INSERT INTO step_summary (step_id, loop_id, turns, tool_calls, tool_errors, input_tokens, output_tokens, reasoning_tokens, wall_ms, first_event, last_event)
SELECT step_id, loop_id,
  SUM(kind = 'step.turn.completed'),
  SUM(kind = 'step.tool.end'),
  SUM(kind = 'step.tool.end' AND json_extract(payload, '$.isError') = 1),
  SUM(CASE WHEN kind = 'step.turn.completed' THEN COALESCE(json_extract(payload, '$.usage.inputTokens'), 0) ELSE 0 END),
  SUM(CASE WHEN kind = 'step.turn.completed' THEN COALESCE(json_extract(payload, '$.usage.outputTokens'), 0) ELSE 0 END),
  SUM(CASE WHEN kind = 'step.turn.completed' THEN COALESCE(json_extract(payload, '$.usage.reasoningTokens'), 0) ELSE 0 END),
  SUM(CASE WHEN kind = 'step.turn.completed' THEN COALESCE(json_extract(payload, '$.wallMs'), 0) ELSE 0 END),
  MIN(id), MAX(id)
FROM event
WHERE step_id IS NOT NULL AND loop_id IS NOT NULL AND json_valid(payload)
GROUP BY step_id, loop_id;

UPDATE step_summary SET
  spend_state = COALESCE((SELECT json_extract(payload, '$.stepCost.state') FROM event
    WHERE event.step_id = step_summary.step_id AND kind = 'step.turn.completed' AND json_valid(payload) ORDER BY id DESC LIMIT 1), 'unknown'),
  spend_usd = (SELECT COALESCE(json_extract(payload, '$.stepCost.usd'), json_extract(payload, '$.stepCost.usdAtLeast')) FROM event
    WHERE event.step_id = step_summary.step_id AND kind = 'step.turn.completed' AND json_valid(payload) ORDER BY id DESC LIMIT 1),
  lane = COALESCE((SELECT json_extract(payload, '$.lane') FROM event
    WHERE event.step_id = step_summary.step_id AND kind = 'loop.step.dispatched' AND json_valid(payload) ORDER BY id LIMIT 1), '');

-- What a step's context holds right now: the size of its latest model call's input (the whole prompt it was sent,
-- cached parts included) and the route it ran on, so the page can show how full each agent's context is.
ALTER TABLE step_summary ADD COLUMN context_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE step_summary ADD COLUMN route TEXT NOT NULL DEFAULT '';

UPDATE step_summary SET
  context_tokens = COALESCE((SELECT COALESCE(json_extract(payload, '$.usage.inputTokens'), 0)
      + COALESCE(json_extract(payload, '$.usage.cacheReadTokens'), 0) + COALESCE(json_extract(payload, '$.usage.cacheWriteTokens'), 0)
    FROM event WHERE event.step_id = step_summary.step_id AND kind = 'step.turn.completed' AND json_valid(payload) ORDER BY id DESC LIMIT 1), 0),
  route = COALESCE((SELECT json_extract(payload, '$.route') FROM event
    WHERE event.step_id = step_summary.step_id AND kind = 'step.turn.completed' AND json_valid(payload) ORDER BY id DESC LIMIT 1), '');

-- The model options a person sets on a loop AFTER it started (thinking on/off, reasoning effort). Kept apart from the
-- loop's pinned state, which the runner rewrites as it walks: a person's change here cannot be overwritten by it, and
-- the next model step reads it. No row, or an empty value, leaves the loop's own pin and the route's default in force.
CREATE TABLE loop_model_option (
  loop_id          TEXT PRIMARY KEY REFERENCES loop(id) ON DELETE CASCADE,
  reasoning_effort TEXT NOT NULL DEFAULT '',
  thinking         TEXT NOT NULL DEFAULT '' CHECK (thinking IN ('', 'on', 'off')),
  set_at           TEXT NOT NULL
);

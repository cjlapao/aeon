-- S68 B7 — the chosen-skills receipt beside a steer. `step_steer.text` stays
-- the operator's PURE words (the UI bubble reads this column and nothing
-- else, B8); `skills` is a nullable JSON array of skill NAMES the operator
-- chose for the steer, resolved to canonical instruction lines by the daemon
-- at the drain — the one place that still joins the two, tested and honest
-- because the stored row never mixes voices. NULL means nothing was chosen;
-- the third state P31 keeps distinct from an empty list.
ALTER TABLE step_steer ADD COLUMN skills TEXT;

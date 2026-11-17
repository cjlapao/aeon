-- Which events a webhook trigger listens for: `event` or `event:action,action` (for GitHub: `issues:opened`). Empty means
-- every delivery that passes the signature starts a loop, which is what every webhook did before this column existed.
ALTER TABLE trigger_schedule ADD COLUMN event_filter TEXT NOT NULL DEFAULT '';

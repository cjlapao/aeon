-- S43-extra T4: the agent columns first-party dispatch needs (tools allow-list, temperature, handoff schema, the policy-agent link, the body hash) plus origin, so a system row is distinguishable from a user/imported one. internal<=>origin='system' is asserted by a test; S43's internal flag and its refusals are untouched.
ALTER TABLE agent ADD COLUMN tools        TEXT    NOT NULL DEFAULT '[]';
ALTER TABLE agent ADD COLUMN temperature  REAL    NOT NULL DEFAULT 0;
ALTER TABLE agent ADD COLUMN handoff      TEXT    NOT NULL DEFAULT '{}';
ALTER TABLE agent ADD COLUMN policy_agent TEXT    NOT NULL DEFAULT '';
ALTER TABLE agent ADD COLUMN body_hash    TEXT    NOT NULL DEFAULT '';
ALTER TABLE agent ADD COLUMN origin       TEXT    NOT NULL DEFAULT 'user' CHECK (origin IN ('system','user','import'));
UPDATE agent SET origin = 'system' WHERE internal = 1;
UPDATE agent SET origin = 'import' WHERE internal = 0 AND signature <> '';

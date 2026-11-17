-- A loop's permission mode: what a person has pre-approved for it. No row means "ask" (the default).
CREATE TABLE loop_access (
  loop_id     TEXT PRIMARY KEY REFERENCES loop(id) ON DELETE CASCADE,
  mode        TEXT NOT NULL CHECK (mode IN ('tools','full')),
  tools_json  TEXT NOT NULL DEFAULT '[]',
  set_by_kind TEXT NOT NULL CHECK (set_by_kind IN ('user','cli','system')),
  set_by_id   TEXT NOT NULL,
  set_at      TEXT NOT NULL
);

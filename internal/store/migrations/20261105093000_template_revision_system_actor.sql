-- S61 T3: scaffold_template_revision.actor_kind gains 'system'. GI14 seeds
-- the shipped templates through the ONE save path, and the honest author of
-- that write is system:seed — the records-tier trio (user, agent, trigger)
-- describes authors AT the records doors, while the seed is a fourth writer
-- this house already admits elsewhere (roadmap's system:reactor, S50;
-- seed-origin rows everywhere, S43-extra). Rebuild in the copy-guard shape:
-- rows travel untouched, the append-only pair is re-created BYTE-IDENTICALLY
-- (both die with the DROP), and no index exists on this table to re-learn.
CREATE TABLE scaffold_template_revision_new (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  slug       TEXT NOT NULL,
  version    INTEGER NOT NULL CHECK (version >= 1),
  files      TEXT NOT NULL CHECK (json_valid(files)),
  actor_kind TEXT NOT NULL CHECK (actor_kind IN ('user','agent','trigger','system')),
  actor_id   TEXT NOT NULL,
  origin     TEXT NOT NULL CHECK (origin IN ('system','user')),
  at         TEXT NOT NULL,
  UNIQUE (slug, version)
);

INSERT INTO scaffold_template_revision_new (id, slug, version, files, actor_kind, actor_id, origin, at)
  SELECT id, slug, version, files, actor_kind, actor_id, origin, at
  FROM scaffold_template_revision;

DROP TABLE scaffold_template_revision;

ALTER TABLE scaffold_template_revision_new RENAME TO scaffold_template_revision;

CREATE TRIGGER scaffold_template_revision_no_update
BEFORE UPDATE ON scaffold_template_revision
BEGIN
  SELECT RAISE(ABORT, 'template history is append-only');
END;

CREATE TRIGGER scaffold_template_revision_no_delete
BEFORE DELETE ON scaffold_template_revision
BEGIN
  SELECT RAISE(ABORT, 'template history is append-only');
END;

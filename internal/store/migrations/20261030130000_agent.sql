-- S43 T1: the agent CONTENT table — every agent, first-party and imported,
-- becomes a row. This table joins plugin_agent by name; the slug is the new
-- universal reference key (AM2) that every by-name reference migrates to.
CREATE TABLE agent (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  -- slug is the immutable, globally-unique reference key (AM2). Generated from
  -- name at creation/duplication, never copied on duplication, never changed
  -- after creation. A collision refuses (the unique index + the app-layer check
  -- agree); it is never silently suffixed.
  slug              TEXT NOT NULL,
  -- name is what a person sees; the app shows name, code crosses-references by
  -- slug. A row may share its name with another (a duplicate) so long as the
  -- slugs differ.
  name              TEXT NOT NULL,
  description       TEXT NOT NULL DEFAULT '',
  functionality     TEXT NOT NULL DEFAULT '',
  -- metadata is an open-ended JSON blob (AM3, the operator's own word).
  metadata          TEXT NOT NULL DEFAULT '',
  -- prompt and skills are the agent's content (today in handler bodies or card
  -- files). skills is a JSON array of {id,name,description}.
  prompt            TEXT NOT NULL DEFAULT '',
  skills            TEXT NOT NULL DEFAULT '',
  -- version is the card's version; import is idempotent by (slug, version)
  -- (AM3).
  version           TEXT NOT NULL DEFAULT '1',
  -- internal=true refuses edit and delete (the write boundary, AM1) but never
  -- duplication. A first-party row is internal=true; a copy is always false.
  internal          INTEGER NOT NULL DEFAULT 0 CHECK (internal IN (0, 1)),
  -- the avatar the settings list shows at rest (AM3): a static flavour default,
  -- never a live runtime state (named non-goal in the brief). A card may carry
  -- no avatar (an empty type/mood), so the CHECK admits '' — the import maps a
  -- blank card field to a blank avatar, and the UI shows a plain label then.
  avatar_type       TEXT NOT NULL DEFAULT 'minion' CHECK (avatar_type IN ('', 'minion', 'cat')),
  avatar_color      TEXT NOT NULL DEFAULT '',
  avatar_mood       TEXT NOT NULL DEFAULT 'idle' CHECK (avatar_mood IN ('', 'idle', 'hi', 'thinking', 'working', 'stuck', 'done')),
  -- signature is the card's signature (AM3); empty when the row was imported
  -- unsigned (behind the allowUnsignedImport override).
  signature         TEXT NOT NULL DEFAULT '',
  -- signer_certificate is the card's embedded X.509 certificate (PEM); empty
  -- when unsigned. The import verifies the signature over the card bytes against
  -- this certificate's public key — tamper-evidence only, no trust root (AM3).
  signer_certificate TEXT NOT NULL DEFAULT '',
  -- signed is the import's three-state answer: 1 = the signature verified;
  -- 0 = unsigned (imported only under the override). A tampered card is refused
  -- at import and never stored, so it has no state here.
  signed            INTEGER NOT NULL DEFAULT 0 CHECK (signed IN (0, 1)),
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_agent_slug ON agent(slug);
CREATE INDEX idx_agent_name ON agent(name);

-- S69 AT2 — the attachment receipt on the person's own row. `attachments` is
-- a nullable JSON array of receipts {name, document, rev, mediaType, size,
-- sha256}: NAMES and pinned references into the frozen @attachments/
-- document revisions, never bytes. The row records what was attached; the
-- daemon resolves each pinned revision back to base64 only at send time, so
-- the transcript never carries bytes and live and replay agree because the
-- pinned revision cannot have moved. NULL means nothing was attached — the
-- third state P31 keeps distinct from an empty list nobody writes.
ALTER TABLE chat_message ADD COLUMN attachments TEXT;

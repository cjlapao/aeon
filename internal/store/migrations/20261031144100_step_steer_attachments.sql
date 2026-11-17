-- S69 AT2 — the attachment receipt on the steer row, the same shape and the
-- same rule as chat_message.attachments: references to pinned @attachments/
-- revisions, never bytes; the drain composes the wire message and the
-- expansion happens at the last moment before the adapters. NULL means the
-- steer rode with no attachments.
ALTER TABLE step_steer ADD COLUMN attachments TEXT;

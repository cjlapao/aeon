-- S70 T1 — the agent↔agent mailbox: append-only addressed messages between
-- agents of one loop (MB2/MB3). A thread is its own header — the id of the
-- first ask — so a conversation is one fact, not a join; an answer links back
-- through result_of instead of replacing anything; delivered_at is the third
-- state (NULL = queued and never drained, P31's discipline); from_agent <>
-- to_agent puts D8's addressed rule (never a broadcast, never self-talk) at
-- the column level. The codec in agent_message.go is the only writer: an ask
-- is queued, delivered exactly once, and never updated or deleted — rows are
-- facts. thread carries no FOREIGN KEY because the first row's thread IS its
-- own id, which cannot exist before the insert that creates it; the codec
-- bootstraps it inside the same transaction and names the thread's loop on
-- every later read, so a forged thread answers nothing (AT5).
CREATE TABLE agent_message (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug      TEXT NOT NULL,
  component_root TEXT NOT NULL,
  thread         INTEGER NOT NULL,
  from_agent     TEXT NOT NULL,
  to_agent       TEXT NOT NULL,
  from_step_id   INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  body           TEXT NOT NULL,
  expect_reply   INTEGER NOT NULL DEFAULT 0,
  result_of      INTEGER REFERENCES agent_message (id) ON DELETE RESTRICT,
  delivered_at   TEXT,
  -- delivered_to_step_id names WHICH step's boundary drain took the row
  -- (S70 T2d, added before the slice merged — the migration is branch-local,
  -- so this is authoring, not editing a shipped entry). Delivery is a pair:
  -- the instant and the receiving step are stamped by the SAME statement,
  -- so "delivered" never half-exists, and a completion can ask rows — not
  -- memory — which replies it owes.
  delivered_to_step_id INTEGER REFERENCES step (id) ON DELETE CASCADE,
  created_at     TEXT NOT NULL,
  CONSTRAINT ck_agent_message_not_self CHECK (from_agent <> to_agent),
  CONSTRAINT ck_agent_message_body CHECK (length (body) > 0),
  CONSTRAINT ck_agent_message_expect CHECK (expect_reply IN (0, 1)),
  CONSTRAINT ck_agent_message_answer CHECK (result_of IS NULL OR expect_reply = 0),
  CONSTRAINT ck_agent_message_delivery_pair CHECK ((delivered_at IS NULL) = (delivered_to_step_id IS NULL))
);

CREATE INDEX idx_agent_message_thread ON agent_message (thread, id);
CREATE INDEX idx_agent_message_loop ON agent_message (component_root, loop_slug, id);
CREATE INDEX idx_agent_message_step ON agent_message (from_step_id);

CREATE INDEX idx_agent_message_delivered ON agent_message (delivered_to_step_id);

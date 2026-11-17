-- S68 B6 — the chosen-skills receipt on the person's own row. `skills` is a
-- nullable JSON array of skill NAMES (canonical directory names from the live
-- catalog at send time), never rendered prose: the row records WHAT the
-- operator chose, the daemon renders the canonical instruction line at send,
-- and the two agree after a restart because the row — not a memory — holds
-- the choice. NULL means nothing was chosen (and predates S68 too): the
-- third state P31 keeps distinct from an empty list nobody writes. Chat
-- rows stay the pure source (B8) — the operator's bubble holds their words,
-- this column holds what rode beside them.
ALTER TABLE chat_message ADD COLUMN skills TEXT;

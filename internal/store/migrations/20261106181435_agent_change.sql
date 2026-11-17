-- S73 T3 (AR1): `agent_change`, the append-only ledger of every change to an
-- `agent` row. One table, seven ops (`create · duplicate · edit · delete ·
-- import · migrate · seed`), one row per write, written in the SAME
-- transaction as the row it describes (AR2), by an actor named as
-- (`actor_kind`, `actor_id`) — `user` for a person at the socket, `cli` for
-- the direct door, `system` for the seed engine and nothing else (AR3/AR4).
-- Modelled on `mcp_change` (20261101131955_mcp_tables.sql:36-48), the closest
-- sibling, and it keeps that table's two load-bearing choices: `slug` is
-- NULLABLE and there is NO foreign key to `agent`, because the ledger must
-- outlive the row — a `delete` entry says something true about a slug that is
-- gone, and a `RESTRICT` would make the honest record unstatable. The
-- `ck_agent_change_slug_names_a_row` CHECK narrows the NULL to exactly one
-- op: a `migrate` run is about the SET and has no single row to name, and no
-- other op may hide behind a missing slug. UPDATE and DELETE are refused by
-- trigger with the sentence in their own text, the same way S46's MG1, S49's
-- AN4, S51's, S52's and S54's SP3 ledgers enforce append-only at the database
-- rather than by convention — so the ledger is the fact even when the caller
-- is a bug.
CREATE TABLE agent_change (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  op          TEXT NOT NULL CHECK (op IN ('create','duplicate','edit','delete','import','migrate','seed')),
  slug        TEXT CHECK (slug IS NULL OR length(slug) BETWEEN 1 AND 256),
  summary     TEXT NOT NULL CHECK (length(summary) BETWEEN 1 AND 1024),
  actor_kind  TEXT NOT NULL CHECK (actor_kind IN ('user','cli','system')),
  actor_id    TEXT NOT NULL CHECK (length(actor_id) BETWEEN 1 AND 256),
  occurred_at TEXT NOT NULL,
  -- A table-level CHECK is written LAST, not between the columns it reads:
  -- SQLite answers a CHECK that names a column declared after it with
  -- `near "summary": syntax error`, measured with the statement run through
  -- sqlite3 on its own before this file was trusted. The design quoted this
  -- constraint in the middle of the list; it is the same constraint, in the
  -- position the parser accepts (S73 T3's Notes carry the measurement).
  CONSTRAINT ck_agent_change_slug_names_a_row CHECK (slug IS NOT NULL OR op = 'migrate')
);
CREATE INDEX idx_agent_change_slug ON agent_change (slug, id);
CREATE TRIGGER agent_change_no_update BEFORE UPDATE ON agent_change BEGIN SELECT RAISE(ABORT, 'agent_change is append-only (S73 AR1)'); END;
CREATE TRIGGER agent_change_no_delete BEFORE DELETE ON agent_change BEGIN SELECT RAISE(ABORT, 'agent_change is append-only (S73 AR1)'); END;

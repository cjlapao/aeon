-- S54 T1: platform_change — the append-only audit of the four derived-store
-- operations (retrieval.reindex, graph.build, graph.reparse, graph.delete),
-- one row per STARTED operation, written in the same transaction as the
-- operation's start record (SP3). The policy_change / automation_change /
-- roadmap_change precedent: history is append-only by trigger, offers no
-- update or delete accessor, and carries ids and fixed templates only —
-- never raw user text (SEC-033).
--
-- subject is a composed id, never prose: 'root\x00corpus' for a reindex,
-- the absolute root for a graph operation. outcome_ref names the async
-- worker ('aw_…') for the two ledger operations and '' for the two inline
-- ones (reparse and delete run synchronously and mint no row).
--
-- actor_kind is the pair the human door answers carry ('user' from the
-- kernel peer, 'cli' from an offline direct door). No 'system', no
-- 'agent': a write-hook pass is the daemon's act and is not audited, and
-- an agent never reaches any of the four doors (SP3, SP4) — the audit
-- says so at the schema, the roadmap 'system' widening does not apply
-- here because no machine edge writes an audited row.
--
-- No FK to anything: corpora and graph roots are derived and regenerable;
-- the record of an operation outlives the rows it rebuilt (the
-- policy_change argument, carried verbatim).

CREATE TABLE platform_change (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  op TEXT NOT NULL CHECK (op IN ('reindex','graph_build','graph_reparse','graph_delete')),
  subject TEXT NOT NULL CHECK (length(subject) BETWEEN 1 AND 256),
  outcome_ref TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL CHECK (length(summary) BETWEEN 1 AND 1024),
  actor_kind TEXT NOT NULL CHECK (actor_kind IN ('user','cli')),
  actor_id TEXT NOT NULL CHECK (length(actor_id) BETWEEN 1 AND 256),
  occurred_at TEXT NOT NULL
);
CREATE INDEX idx_platform_change_subject ON platform_change (subject, id);
CREATE TRIGGER platform_change_no_update BEFORE UPDATE ON platform_change BEGIN SELECT RAISE(ABORT, 'platform_change is append-only (S54 SP3)'); END;
CREATE TRIGGER platform_change_no_delete BEFORE DELETE ON platform_change BEGIN SELECT RAISE(ABORT, 'platform_change is append-only (S54 SP3)'); END;

-- S57 T7: the post-merge watch ledger (VG5) and the gate-filing dedupe ledger (VG16).
--
-- merge_watch: one row per merged PR the daemon keeps an eye on after the
-- landing — cold, from rows, bounded. The watch is keyed by the merge event
-- (its PK), so a merge can be watched at most once and the crash window
-- between RecordMergeEvent and the enqueue is repaired by the startup sweep
-- (a merge_event with no watch row simply gets one). state is a five-state
-- closed set: pending (not yet checked), unknown (checked, no answer yet —
-- pending checks, an empty report, an unreadable forge), green (every
-- reported check is success — the watch has done its job), red (a check
-- failed — the filer has fired), expired (the window elapsed; a watch that
-- never got an answer is expired, NEVER red — absence is not a failure).
-- next_check_at is the poll schedule; attempts counts backoff; detail is a
-- fixed-template one-liner (the aggregate's reason or the error kind), never
-- forge output.
CREATE TABLE merge_watch (
  merge_event_id  TEXT PRIMARY KEY REFERENCES merge_event(id),
  component_id    TEXT NOT NULL,
  base_ref        TEXT NOT NULL,
  sha             TEXT NOT NULL,
  state           TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','green','red','unknown','expired')),
  attempts        INTEGER NOT NULL DEFAULT 0,
  next_check_at   TEXT NOT NULL,
  last_checked_at TEXT,
  detail          TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL
);
CREATE INDEX idx_merge_watch_due ON merge_watch (state, next_check_at);

-- gate_filing: the VG16 dedupe ledger. A row per filing subject — a repeat
-- of the same gate failure bumps count instead of re-filing, so a red gate
-- that keeps blocking the same subject files exactly one bug. The bug row
-- itself carries no counter (T0 item 8: the bug stays a plain unvalidated
-- triage row); the ledger stands beside it. kind is the closed trigger set;
-- subject encodes the scope (a component root plus the failing subject).
CREATE TABLE gate_filing (
  subject       TEXT PRIMARY KEY,
  bug_id        INTEGER,
  kind          TEXT NOT NULL CHECK (kind IN ('verify','merge-watch','merge-refused')),
  first_seen_at TEXT NOT NULL,
  count         INTEGER NOT NULL DEFAULT 1
);

-- End guard (the 0019 discipline): the new FK-bearing table (merge_watch →
-- merge_event) left no dangling reference — the CHECK makes the migration
-- itself abort if not.
CREATE TABLE zz_s57_merge_watch_fk_guard (
  n INTEGER CHECK (n = 0)
);
INSERT INTO zz_s57_merge_watch_fk_guard (n)
SELECT count(*) FROM pragma_foreign_key_check();

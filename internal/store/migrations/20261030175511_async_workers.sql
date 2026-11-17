-- S64 T1 — the async-worker ledger: one row per background job the daemon
-- runs off the caller's clock (the code-graph build being the first). The
-- row IS the record: a client reconnecting days later reads this table to
-- answer "is anything of this type still running for this root, and what
-- was its last known state" — the in-memory tracker (graphBusy,
-- reindexTracker) could never survive a restart, which is the whole reason
-- this table exists. State closes at the database the way PL3 and the graph
-- CHECKs close their vocabularies: queued → running → one of the four
-- terminal states (finished / failed / cancelled / interrupted), and
-- `interrupted` is the third state P31 refuses to collapse into `failed` —
-- the daemon stopped, which is a different observation from the work being
-- wrong. component_root is its own column, not metadata, because the page
-- question ("anything running HERE") filters on it and metadata KVP is for
-- what is dynamic (componentId, folderPath, path, …); it is nullable
-- because not every worker is scoped to a directory. The partial unique
-- index is single-flight made durable: one queued-or-running worker per
-- (worker_type, dedupe_key), enforced by the database rather than by a map
-- a crash wipes. Timestamps are RFC3339 TEXT like every other table's
-- (telemetry's convention), NULL meaning "has not happened yet", never a
-- sentinel.

CREATE TABLE async_worker (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    worker_id         TEXT NOT NULL UNIQUE CHECK (length(worker_id) > 0),
    worker_type       TEXT NOT NULL CHECK (length(worker_type) > 0),
    state             TEXT NOT NULL DEFAULT 'queued' CHECK (state IN
                        ('queued','running','finished','failed','cancelled','interrupted')),
    cancel_requested  INTEGER NOT NULL DEFAULT 0 CHECK (cancel_requested IN (0,1)),
    component_root    TEXT,
    dedupe_key        TEXT NOT NULL,
    metadata          TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata)),
    progress          REAL CHECK (progress IS NULL OR (progress >= 0 AND progress <= 1)),
    progress_note     TEXT,
    result            TEXT,
    error             TEXT,
    queued_at         TEXT NOT NULL,
    started_at        TEXT,
    finished_at       TEXT
);

CREATE UNIQUE INDEX async_worker_flight ON async_worker (worker_type, dedupe_key)
    WHERE state IN ('queued','running');

CREATE INDEX async_worker_query ON async_worker (worker_type, state, component_root);

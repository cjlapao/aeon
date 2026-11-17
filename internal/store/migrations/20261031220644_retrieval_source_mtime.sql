-- S71 T1: the per-source mtime record — the cheap gate for the status walk
CREATE TABLE retrieval_source_mtime (
  corpus_id INTEGER NOT NULL,
  source TEXT NOT NULL,
  mtime INTEGER NOT NULL,
  PRIMARY KEY (corpus_id, source)
);

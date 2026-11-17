package daemon

// reindex.go — the daemon's side of S22: who may start a reindex, and what
// everyone else is told while one runs.
//
// V21 is the load-bearing rule: `retrieval.reindex` never fires on its own
// — not from `retrieval.status`, not from a stale `search.query`, not on
// daemon start. Only two doors start work: an explicit call (the socket
// method, the CLI verb through it) and the loop's own write trigger
// (V8/Q7). Both now start the SAME worker type through the SAME
// asyncworkers.Manager (S64's graph.build precedent, carried to retrieval
// — the "same approach it was added" the request asked for): the socket's
// retrieval.reindex and the write-hook trigger are the manager's one
// flight, so hook and button cannot double-pass.
//
// The old reindexTracker — an in-memory single-flight + running-figure
// memory — is GONE. Its job is the manager's now: the ledger row is the
// memory a restart survives, the manager's dedupe key is the single flight
// (per (root, corpus)), and the row's progress is the running figure the
// status reads. Keeping the tracker would be a second memory of the same
// pass, exactly the drift the manager exists to prevent.
//
// Q7 — how the loop signals that it wrote: the daemon's own
// tool-result conversion point (toolCaller.Call, the ONE place tool
// results become runtime values) sees successful `write`, `edit` and the
// `write_file` compatibility spelling with a path on the outcome. That is
// "the loop already knows it wrote files" made structural — no watcher, no
// polling, no second observer of the tree (N2). `git.commitAndPush` is
// deliberately NOT a trigger: it does not change the worktree — the
// writes that changed those files already triggered, and committing what
// is already indexed would embed the same bytes twice for nothing.
//
// The one named exception to Q7 (S71 T4): a bounded, on-demand `stat` of
// the component root on an explicit `retrieval.status` read is not a
// watcher, not polling, and not a *continuous* second observer — it is the
// status read's own cost, it fires only when a caller asks, it costs no
// embeddings (V10), and it starts no pass (V21). The hash-confirm, not the
// `stat`, decides staleness: a `touch` that moves mtime without changing
// content is re-hashed and found unchanged, and not flagged. The trigger
// set is unchanged (only the explicit door and the loop's own write-hook);
// the amendment only licences a `stat` on an *asked-for* read.
//
// The one thing this file keeps is the timeout: the manager bounds a pass
// at its default (30 minutes), and the manager-absent fallback (a
// storeless daemon has no manager and no pass to run, so notifyReindex
// runs the pass directly) needs the same bound.

import (
	"time"
)

// reindexTimeout bounds a background pass: it runs on the daemon's life,
// not a caller's — but "background" is not "unbounded". It is the manager's
// default timeout (asyncworkers.defaultTimeout) and the manager-absent
// fallback's own bound; the figure is one number, not a coincidence.
const reindexTimeout = 30 * time.Minute

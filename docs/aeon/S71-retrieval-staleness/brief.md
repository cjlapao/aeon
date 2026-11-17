# Brief — S71 Retrieval Staleness Signal

*Renamed from S68 to S71 at the S68-right-body merge (2026-10-31): the S68
ID was taken by the built right-body slice while this brief-only slice sat
unstarted on the other worktree; S69/S70 are reserved (attachments,
agent↔agent) per the S68 spec, so this parked brief takes S71.*

**Implements:** a design, not yet built — **promoted to brief depth by explicit choice**, to be
implemented later (the operator's rule: design now, build later). The full design and the
implementation plan already exist and are the authoritative detail:
[`../../superpowers/specs/2026-10-31-retrieval-staleness-signal-design.md`](../../superpowers/specs/2026-10-31-retrieval-staleness-signal-design.md)
and
[`../../superpowers/plans/2026-10-31-retrieval-staleness-signal-plan.md`](../../superpowers/plans/2026-10-31-retrieval-staleness-signal-plan.md).
This brief is the slice's own "what / why / decisions / non-goals"; the spec carries the measured
facts and the plan the ordered steps. A full `design.md`/`plan.md`/`tasks/` for the slice would
mirror those two files; they are referenced here rather than duplicated.

**Architecture:** [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — the load-bearing rules are V21
(reindex never fires on its own), V10 (indexing a repository costs real money), O3 (a figure
presented as exact for work not yet done is a confident lie — never `$0.00`), O4 (per-corpus
isolation), and Q7 (no watcher, no polling, no second observer of the tree). All four are
**preserved**; this slice adds exactly one named exception to Q7 (below).

**Stage:** 4 · Retrieval (evolution of S22). **Depends on:** S22-retrieval (built), S64-async-workers
(built — `retrieval.reindex` already rides the manager). **Repo:** Aeon at `aeon/` · Go · `cgo`
enabled. **Checked 2026-10-31 against the real code** (see the spec §2, all measured).

---

## 1. What this is

A person asks: *how do files that change — or new files that appear — outside the loop ever get
indexed, and should reindex be manual or automatic?*

The answer, settled by measurement: **the trigger is already decided** (V21 — only the explicit
door and the loop's write trigger start a pass; no auto-fire, no watcher). `retrieval.status`
**already walks the tree** (`Status` → `Staleness` → `reconcileCode(apply:false)`) and **already
reports `StaleFiles`** — it already sees a file that changed or a new file that appeared on disk.
So this is *not* "build a signal." It is **make the already-existing signal cheap, and price it,
so a person can decide against a number instead of a silently wrong index.**

The two small additions:

1. **A mtime-gate** on the status walk — `stat` is cheap; only the sources whose mtime moved get
   re-hashed. The walk that already runs stops re-reading a large repo every status read.
2. **The priced cost** folded into the status answer — the walk already runs, so pricing it is
   nearly free; `retrieval.status` says *"12 changed / 3 new / a pass would cost ~$0.71."*

The trigger stays the explicit door and the loop's write trigger. Nothing new fires a pass.

## 2. Why it is small (and why the easy answers were rejected)

The tempting answers were all rejected against the invariants, and the record is in the spec §9:

- **A file watcher (fsnotify / the Cursor model)** — it is Q7's *exact* "second observer of the
  tree," with per-keystroke churn, embedding money on churn (V10), and a new dependency + state.
- **A git-operation trigger (on merge/pull/checkout)** — a V21 relaxation the operator did not
  choose, and it misses untracked new files and direct edits anyway.
- **Hashing the whole tree on every status read (today's behavior)** — correct but unbounded; the
  mtime-gate keeps the same truth at a fraction of the cost.
- **The staleness signal + you decide** — keeps V21 (never auto-fire) and V10 (money only on a
  deliberate pass), and turns a silently wrong index into a *number you can decide against*.

The one correction that shaped the brief: `retrieval.status` is **not** store-only — it already
does the `reconcileCode(apply:false)` walk, so external drift is already detected. The gaps are
cost (G1) and the unbounded walk (G2/G3), not detection.

## 3. The decisions (settled in the brainstorm)

- **Direction — the staleness signal, the person decides.** Keep V21 + V10. Status reports how
  stale the index is and what a pass would cost; a person runs `retrieval.reindex` on purpose.
- **Detection — the mtime-gate.** Record mtime at index time; on a status read, `stat` every
  source (cheap); only the sources whose mtime moved get re-hashed. The `stat` is the cheap gate;
  the hash is the confirm — so a `touch` or a `git checkout` that moves mtime without changing
  content is caught by the hash (identical → not stale → record refreshed) and *not* flagged.
- **mtime home — a `retrieval_source_mtime(corpus_id, source, mtime)` side table**, not a column
  on `retrieval_chunk` (the per-file mtime would duplicate across a file's chunks). mtime is
  per-file; the record is per-source.
- **Cost home — fold the priced cost into the status answer** (`DriftFigures` with `Cost`). The
  walk already runs, so it is nearly free; `retrieval.reindexPlan` (the S54-planned door) is
  deferred — added only if the bench page needs a richer figure than status carries.
- **Trigger — unchanged.** Explicit door + loop write trigger. No automatic trigger, no watcher.

## 4. The invariants the slice must not break

- **V21** — `retrieval.reindex` never fires on its own.
- **V10** — indexing costs real money; the cost is a deliberate, asked-for act.
- **O3** — the price is three-state (known / partial / unknown); an unmetered route's cost is
  **nil, never `$0.00`** (the basis — bytes/4 — is stated).
- **O4** — per-corpus isolation: a code change never moves a history figure.
- **Q7** — *amended, not relaxed:* a bounded, on-demand `stat` of the component root on an
  explicit `retrieval.status` read is not a watcher, not polling, and not a second observer of the
  tree (it fires only when a caller asks, costs no embeddings, starts no pass; the hash-confirm,
  not the `stat`, decides staleness). One line, in `internal/daemon/reindex.go`.

## 5. Non-goals

No file watcher / fsnotify. No automatic trigger on merge/pull/checkout, on a query, or on start
(V21 stands). No change to the reconcile's correctness (the hash stays content, never mtime —
mtime only gates *whether to hash*). No change to the trigger wiring (`notifyReindex`,
`chainHooks`, the async manager, the running figure). No migration that touches the chunk record's
hash — only a per-source mtime record.

## 6. Open, to settle at implementation

- **The `stat` walk's own cost** — for a very large tree, even `stat`-ing every source is
  non-trivial. If so, a per-corpus `stat`-count cap or a "the walk is too large to gate cheaply"
  three-state degradation is the honest fallback, not a silent full walk. *Measure, then decide.*
- **The not-verified walks** (named, not assumed): a live-socket walk of a real large-repo status
  read with a file changed externally (the mtime-gate under real IO); the desktop click-through
  (no display — the bench page is the `tsc`/vitest witness, and only if the page is touched).

## 7. What a full `design.md` for the slice would carry

The spec and plan already carry it (referenced in the header). If the slice later gets its own
`design.md`/`plan.md`/`tasks/`, they would mirror the two `docs/superpowers/` files, with the
dated `go test`/`go vet` evidence and the AGENTS §4a records (SLICES / DEMO / the S22 T8 Notes)
added in the same change.

package protocol

// retrieval.go — the socket twin of the `search` tool (S22 T6, D15/I10/P34).
//
// A capability that exists only as a tool verb is half a capability: the
// person exploring the app would otherwise have to start a loop and read a
// transcript to search their own project (I10). So `search.query` answers
// over the SAME resolver function value the tool calls — this file adds
// JSON-RPC marshaling around a call, and nothing else (D15: not a second
// search implementation; the same-resolver test lives in this package
// because it is where both doors are visible).
//
// The refusal rules mirror the tool's exactly (O8's corpus-required, D2's
// named empties): two doors that refuse differently are two truths. The
// resolver refuses again — its sentence stays the last word (the house
// refusal rule survives the seam), and the availability states (index-empty,
// keyword-only) reach the caller verbatim rather than as a confident empty
// list.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
)

// SearchQueryParams is the request: {componentRoot, corpus, query, limit?}.
// componentRoot is the post-S38 name of the root the corpus rows hang off —
// the task file predates the OL1 rename and says projectRoot (recorded in
// T6's Notes); every loop and every corpus row already rides this key.
type SearchQueryParams struct {
	ComponentRoot string `json:"componentRoot"`
	Corpus        string `json:"corpus"`
	Query         string `json:"query"`
	Limit         int    `json:"limit,omitempty"`
}

// RegisterSearchQuery registers `search.query` over the wired seam. It
// rides with the wiring rather than NewTable because the seam is assigned
// after construction (the daemon's own order), and a registration that ran
// inside NewTable would only ever see nil. The nil seam is the V13
// configuration speaking — every no-retrieval build arrives here with nil
// and the method stays absent (D1's other half); that is the designed
// outcome, not a silent failure. Returns whether the method is in the
// table, so the daemon's wiring can be asserted rather than trusted.
func (t *Table) RegisterSearchQuery() bool {
	if t.Search == nil {
		return false
	}
	// The shape policy.* set: RequiresInitialize — no answers before the
	// daemon finishes booting.
	t.register(&Method{Name: MethodSearchQuery, RequiresInitialize: true, Handle: t.handleSearchQuery})
	return true
}

func (t *Table) handleSearchQuery(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	// Unreachable in any built configuration — registration is conditional
	// on this same field. The guard is the tripwire that says registration
	// and the seam disagreed, spoken rather than silent (nil-refusal
	// pattern, the GitStatus shape).
	if t.Search == nil {
		return nil, ErrStoreUnavailable.WithWhat("search.query needs the retrieval resolver, and this build did not link it — the `search` tool is absent for the same reason (V13)")
	}
	var p SearchQueryParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentRoot) == "" {
		return nil, ErrInvalidScope.WithWhat("search.query needs a componentRoot — there is no ambient root on the socket (I5), and the corpus row answers for exactly one root")
	}
	if p.Corpus != "code" && p.Corpus != "history" {
		// O8, stated here as it is stated at the tool: a correctable error
		// naming both options. The authority stays retrieval.ValidCorpus —
		// the resolver refuses again, and that sentence is the last word.
		return nil, ErrInvalidScope.WithWhat(`search.query NAMES its corpus: "code" or "history" — there is no default`)
	}
	if strings.TrimSpace(p.Query) == "" {
		return nil, ErrInvalidScope.WithWhat("search.query refused an empty query — an empty string matches nothing and answers like it found everything")
	}
	ans, err := t.Search(ctx, p.ComponentRoot, p.Corpus, p.Query, p.Limit)
	if err != nil {
		// The resolver's refusal (index-empty with its reason, D2) passes
		// through verbatim — the caller is told WHAT, not handed a shrug.
		return nil, err
	}
	return ans, nil
}

// --- T8: retrieval.status and retrieval.reindex -----------------------------
//
// The two doors for index status and the deliberate reindex (S22 T8, D15,
// I10, P34, V21). They are JSON-RPC marshaling around the daemon-injected
// function values and nothing else — the identical functions `aeon index
// status` and `aeon index rebuild` call (the call-site lock is
// TestRetrievalStatusAndReindexSocketCallSameFunctions). What this file
// adds beyond marshaling is the refusal discipline: the corpus is NAMED
// (O8, no default), the root is REQUIRED (I5, no ambient root on the
// socket), and nothing on this path ever starts work by itself —
// `retrieval.status` reads, `search.query` reads, and only an explicit
// `retrieval.reindex` call reaches the pass (V21, spy-asserted at the
// daemon).

// RetrievalStatusParams is the request: {componentRoot}.
type RetrievalStatusParams struct {
	ComponentRoot string `json:"componentRoot"`
}

// RetrievalReindexParams is the request: {componentRoot, corpus}. The
// corpus is required and named exactly as everywhere else (O8) — a reindex
// that re-embedded "everything" across both corpora would spend money on
// work whose scope the caller never stated.
type RetrievalReindexParams struct {
	ComponentRoot string  `json:"componentRoot"`
	Corpus        string  `json:"corpus"`
	EmbedRoute    *string `json:"embedRoute,omitempty"`
	Force         bool    `json:"force,omitempty"`
}

// RetrievalReindexAccepted is retrieval.reindex's S22-async answer: a
// handle, not a report. The pass belongs to the async-worker ledger now
// (the daemon runs it on the manager, the row records it, the event stream
// carries its ending), the caller reads the finished ReindexOutcome from
// the row's result column through asyncworkers.get. `started` says whether
// THIS call began the pass: false means one was already flying for this
// (root, corpus) — the same answer twice is how a double-clicked button
// becomes one pass (the same shape as graph.build's GraphBuildAccepted).
type RetrievalReindexAccepted struct {
	WorkerID string `json:"workerId"`
	State    string `json:"state"`
	Started  bool   `json:"started"`
	// StartedBy / StartedAt: S54 T1, SP3 — see GraphBuildAccepted for the
	// rule; the same additive naming of the flying pass.
	StartedBy string `json:"startedBy,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
}

// RegisterRetrievalStatus registers `retrieval.status` over the wired
// seam, absent when the seam is nil (V13 — the same conditional shape as
// RegisterSearchQuery and for the same reason).
func (t *Table) RegisterRetrievalStatus() bool {
	if t.RetrievalStatus == nil {
		return false
	}
	t.register(&Method{Name: MethodRetrievalStatus, RequiresInitialize: true, Handle: t.handleRetrievalStatus})
	return true
}

// RegisterRetrievalReindex registers `retrieval.reindex` over the wired
// seam. It NEVER registers a method that fires on its own — registration
// puts a door on the socket, and only a call walks through it (V21).
// S54 T1 adds the second condition: the reindex is an OPERATION with an
// audit, so it registers only where a stamping source exists too — no
// ActorFor, no persons, no honest door (V13, the graph writes' sibling).
func (t *Table) RegisterRetrievalReindex() bool {
	if t.RetrievalReindex == nil || t.ActorFor == nil {
		return false
	}
	t.register(&Method{Name: MethodRetrievalReindex, RequiresInitialize: true, Handle: t.handleRetrievalReindex})
	return true
}

func (t *Table) handleRetrievalStatus(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.RetrievalStatus == nil {
		return nil, ErrStoreUnavailable.WithWhat("retrieval.status needs the status seam, and this build did not link it — `aeon index status` is absent for the same reason (V13)")
	}
	var p RetrievalStatusParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentRoot) == "" {
		return nil, ErrInvalidScope.WithWhat("retrieval.status needs a componentRoot — there is no ambient root on the socket (I5), and staleness is a property of one root's index (V12)")
	}
	return t.RetrievalStatus(ctx, p.ComponentRoot)
}

func (t *Table) handleRetrievalReindex(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.RetrievalReindex == nil {
		return nil, ErrStoreUnavailable.WithWhat("retrieval.reindex needs the reindex seam, and this build did not link it — `aeon index rebuild` is absent for the same reason (V13)")
	}
	var p RetrievalReindexParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentRoot) == "" {
		return nil, ErrInvalidScope.WithWhat("retrieval.reindex needs a componentRoot — the index is per project root (V12); a nameless root is no index to rebuild")
	}
	if p.Corpus != "code" && p.Corpus != "history" {
		return nil, ErrInvalidScope.WithWhat(`retrieval.reindex NAMES its corpus: "code" or "history" — there is no default, and the money is per corpus`)
	}
	// S22 async (the graph's "same approach it was added"): the pass runs
	// as an async worker, not on this connection — the SAME manager the
	// write-hook trigger starts through (the daemon pins the identity), so
	// button and hook cannot double-pass. A deliberate call reaches the
	// pass only through THIS call (V21: registration put the door, and
	// only a call walks through it). The handle answers in the
	// milliseconds it takes to touch the ledger; the caller reads the
	// finished ReindexOutcome from the row's result column through
	// asyncworkers.get (the CLI follows it, the app polls retrieval.status).
	if t.AsyncWorkers == nil {
		return nil, ErrStoreUnavailable.WithWhat("retrieval.reindex needs the async-worker manager to run the pass off this connection, and this daemon did not wire it (no store) — the pass would otherwise be a thirty-minute hold on your socket")
	}
	abs, err := filepath.Abs(p.ComponentRoot)
	if err != nil {
		return nil, ErrInvalidScope.WithWhat("retrieval.reindex %s: cannot resolve the root: %v", p.ComponentRoot, err)
	}
	// The flight is per-(root, corpus): a code pass and a history pass
	// run concurrently, each blocking only its own corpus (the per-corpus
	// single flight the tracker had, now the manager's dedupe key). The
	// money decision (embedRoute/force) and the corpus ride Metadata the
	// runner reads — the runner names no corpus of its own (O8), and a
	// pass onto another model's corpus is the door's decision, never the
	// runner's (V17).
	metadata := map[string]string{"corpus": p.Corpus}
	if p.EmbedRoute != nil {
		metadata["embedRoute"] = *p.EmbedRoute
	}
	if p.Force {
		metadata["force"] = "true"
	}
	// S54 T1 (SP3/SP6): the reindex is an OPERATION started by the person
	// at the endpoint; the audit row commits in the SAME transaction as
	// the queued ledger row, so an "already running" answer — the flight
	// collision — writes NEITHER by construction. The subject is the
	// flight's own key; the summary names only the closed corpus word.
	actor, err := t.platformActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	view, started, err := t.AsyncWorkers.StartViewAudited("retrieval.reindex", abs, abs+"\x00"+p.Corpus, metadata, reindexAudit(abs, p.Corpus, actor))
	if err != nil {
		return nil, ErrStoreUnavailable.WithWhat("retrieval.reindex %s: %v", p.ComponentRoot, err)
	}
	acc := &RetrievalReindexAccepted{WorkerID: view.WorkerID, State: view.State, Started: started}
	if !started {
		acc.StartedBy, acc.StartedAt = t.starterWords(ctx, view.WorkerID)
	}
	return acc, nil
}

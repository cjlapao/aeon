package protocol

// platform.go — S54 T2: the platform panel and its ledger read
// (`platform.status`, `platform.changes`, `retrieval.reindexPlan`).
//
// # What this file owns
//
// The three READS SP6 lists for the platform plane, over three seams that
// are each the daemon's already-existing read path — the same function
// values `retrieval.status`, `graph.status`/`graph.roots`,
// `telemetry.workspace` and the T1 audit table answer with (SP1: one
// implementation; the panel computes nothing a subsystem also computes).
// The composer itself lives in internal/platformview, a store-free leaf;
// this file only puts it behind a door.
//
// `retrieval.reindexPlan` previews a rebuild's cost from the SAME estimate
// walk the priced refusal runs (`retrieval.Index.EstimateReindex` — T0 §3
// found the machinery already there) and NEVER calls the embedder: an
// estimate that embedded to price itself would charge for the preview.
// The cost rides three-state (known | partial | unknown, S33/O3) and an
// unknown never shows a bare number (M30).
//
// The writes of this plane were T1's (platform_door.go): human doors,
// audited. Everything here reads; none of it stamps audit; none of it
// takes an actor.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cjlapao/aeon/internal/platformview"
)

// ErrPlatformUnknown is the platform plane's not-found (-31957, second
// code of the S54 block — see errors.go's re-anchor note). Today exactly
// one door reaches it through the daemon's adapter: `retrieval.reindexPlan`
// for a corpus the plane does not have (the corpora are "code" and
// "history"; there is no third corpus to plan). Sibling shape to
// ErrLearningUnknown — the id was well-formed, the row simply is not
// there, and inventing an estimate for a corpus that does not exist is
// worse than saying so.
var ErrPlatformUnknown = &Error{
	Code:    -31957,
	Message: "no such corpus to plan a rebuild for",
	Data: Data{
		What:  "retrieval.reindexPlan named a corpus the plane does not have — the corpora are \"code\" and \"history\" and there is no third",
		Means: "nothing was estimated, nothing was started, nothing was written",
		Todo:  "run retrieval.status and read the corpora list, then name one of those",
	},
}

// --- wire shapes --------------------------------------------------------------

// PlatformChangesParams: {subject?, limit?} — subject is a prefix filter
// on the audit's subject (an absolute root or root+"\x00"+corpus key);
// limit clamps [1,500] at the store, newest first, like every ledger read.
type PlatformChangesParams struct {
	Subject string `json:"subject,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// PlatformChangeView is one platform_change row: who started a derived-
// store operation, what it started, and when. Summary is the store's own
// fixed template — never user text, never a paraphrase (SP3).
type PlatformChangeView struct {
	ID         int64  `json:"id"`
	Op         string `json:"op"`
	Subject    string `json:"subject"`
	OutcomeRef string `json:"outcomeRef,omitempty"`
	Summary    string `json:"summary"`
	ActorKind  string `json:"actorKind"`
	ActorID    string `json:"actorId"`
	OccurredAt string `json:"occurredAt"`
}

// RetrievalPlanParams: {componentRoot, corpus}. The corpus is named —
// there is no default (O8) and no ambient root on the socket (I5).
type RetrievalPlanParams struct {
	ComponentRoot string `json:"componentRoot"`
	Corpus        string `json:"corpus"`
}

// PlanCost is the estimate's price in the ONLY shape a cost may ride:
// three-state with its reason (S33/O3/M30). It mirrors search.Cost's wire
// fields but is its own type on purpose — the construction guard watches
// Cost literals, and this figure is an ESTIMATE (its honesty sentence is
// "would cost", not "did cost"); sharing the type would let a caller
// mistake the two.
type PlanCost struct {
	USD    *float64 `json:"usd,omitempty"`
	State  string   `json:"state"`
	Reason string   `json:"reason,omitempty"`
}

// RetrievalPlanAnswer is SP2's preview: what a reindex pass WOULD do and
// WOULD cost, from the same walk the priced refusal runs. willReplaceEm-
// beddings is true and says why it matters: a reindex pass rewrites the
// corpus's derived vectors; source files are never touched (derived data
// is regenerable, the deletion law).
type RetrievalPlanAnswer struct {
	ComponentRoot         string    `json:"componentRoot"`
	Corpus                string    `json:"corpus"`
	Files                 int       `json:"files"`
	StaleFiles            int       `json:"staleFiles"`
	StaleChunks           int       `json:"staleChunks"`
	EstimatedInputTokens  int       `json:"estimatedInputTokens"`
	EstimateBasis         string    `json:"estimateBasis"`
	Cost                  *PlanCost `json:"cost"`
	EmbedModel            string    `json:"embedModel,omitempty"`
	EmbedDim              int       `json:"embedDim,omitempty"`
	WillReplaceEmbeddings bool      `json:"willReplaceEmbeddings"`
}

// --- seams ---------------------------------------------------------------------

// PlatformStatusFunc composes the panel over the daemon's own read seams.
// The daemon builds the ONE adapter (platform_reads.go) that calls the
// same functions the per-subsystem doors call and hands the results to
// platformview.Compose; nil keeps the method out of the table (V13 — a
// storeless, retrievalless, graphless daemon has no panel to compose).
type PlatformStatusFunc func(ctx context.Context) (*platformview.StatusView, error)

// RetrievalPlanFunc prices one corpus's rebuild without touching the
// embedder. Nil (the no-retrieval build, retrieval_link_off.go) keeps the
// door absent (V13).
type RetrievalPlanFunc func(ctx context.Context, componentRoot, corpus string) (*RetrievalPlanAnswer, error)

// PlatformChangesFunc reads the T1 audit newest-first. Nil on a storeless
// daemon keeps the door absent (V13).
type PlatformChangesFunc func(ctx context.Context, subject string, limit int) ([]PlatformChangeView, error)

// --- registration (absent, not refusing — V13) ----------------------------------

// RegisterPlatformStatus registers `platform.status` when the daemon wired
// a composer. Every sub-block speaks its own unwired state; the door
// itself exists only where a daemon with subsystems built the adapter.
func (t *Table) RegisterPlatformStatus() bool {
	if t.PlatformStatus == nil {
		return false
	}
	t.register(&Method{Name: MethodPlatformStatus, RequiresInitialize: true, Handle: t.handlePlatformStatus})
	return true
}

// RegisterRetrievalPlan registers `retrieval.reindexPlan` over the priced
// plan seam — absent with the seam (V13), the same gate the reindex door
// keys on. It is a READ: no actor, no audit, no write (SP6).
func (t *Table) RegisterRetrievalPlan() bool {
	if t.RetrievalPlan == nil {
		return false
	}
	t.register(&Method{Name: MethodRetrievalReindexPlan, RequiresInitialize: true, Handle: t.handleRetrievalPlan})
	return true
}

// RegisterPlatformChanges registers `platform.changes` over the audit read.
// It reads ONLY T1's rows; the ledger's existence is its gate (V13).
func (t *Table) RegisterPlatformChanges() bool {
	if t.PlatformChanges == nil {
		return false
	}
	t.register(&Method{Name: MethodPlatformChanges, RequiresInitialize: true, Handle: t.handlePlatformChanges})
	return true
}

// --- handlers --------------------------------------------------------------------

func (t *Table) handlePlatformStatus(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.PlatformStatus == nil {
		return nil, ErrStoreUnavailable.WithWhat("platform.status needs the daemon's composed-read adapter, and this build did not link one — every subsystem door absent for the same reason (V13)")
	}
	var p struct{}
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return t.PlatformStatus(ctx)
}

func (t *Table) handleRetrievalPlan(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.RetrievalPlan == nil {
		return nil, ErrStoreUnavailable.WithWhat("retrieval.reindexPlan needs the retrieval plan seam, and this build did not link it — `retrieval.reindex` is absent for the same reason (V13)")
	}
	var p RetrievalPlanParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentRoot) == "" {
		return nil, ErrInvalidScope.WithWhat("retrieval.reindexPlan needs a componentRoot — there is no ambient root on the socket (I5), and a price is a property of one root's corpus (V12)")
	}
	if strings.TrimSpace(p.Corpus) == "" {
		return nil, ErrInvalidScope.WithWhat("retrieval.reindexPlan needs a corpus — there is no default corpus to price (O8); run retrieval.status and read the corpora list")
	}
	return t.RetrievalPlan(ctx, p.ComponentRoot, p.Corpus)
}

func (t *Table) handlePlatformChanges(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.PlatformChanges == nil {
		return nil, ErrStoreUnavailable.WithWhat("platform.changes needs the store's platform_change ledger, and this daemon has no store to read")
	}
	var p PlatformChangesParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return t.PlatformChanges(ctx, strings.TrimSpace(p.Subject), p.Limit)
}

package protocol

// asyncworkers.go — the app's door onto the async-worker ledger (S64 T3).
//
// This file adds JSON-RPC marshaling around internal/asyncworkers'
// read/cancel surface and NOTHING else — the same discipline graph.go
// states for internal/graph: the daemon wires ONE *asyncworkers.Manager,
// the graph.build handler starts through it, the ledger answers from it,
// and a second query implementation would be exactly what the P34
// amendment exists to prevent.
//
// # Why only reads and cancel ride this socket
//
// Starting a worker means naming a Go function the daemon registered at
// construction. A generic `asyncworkers.start` would hand a caller the
// means to fire arbitrary registered work with arguments it cannot
// correctly supply (which root? which file? the manager cannot guess, and
// a guessed argument is a wrong index, not a harmless typo). So starting
// belongs to the owning service's own method — `graph.build` starts the
// graph.build worker, exactly as `retrieval.reindex` drives its own pass.
// The ledger family stays read + cancel: cancel is safe generically
// because it names nothing but a row the caller can already see.
//
// # Absent, not refusing (V13)
//
// Nil seam keeps all three methods out of the table. A storeless daemon
// has no ledger and no graph, so it has no workers — the methods have
// nothing to be the door OF.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/cjlapao/aeon/internal/asyncworkers"
)

// AsyncWorkers is the daemon-injected manager. The shape is the
// manager's own methods restated — a drift between seam and manager is a
// compile failure at the daemon's single wiring point, not a runtime
// surprise (the Graph field's rule, carried).
//
// StartView is here because graph.build needs it THROUGH this seam value
// (one manager, every door). The asyncworkers method family deliberately
// registers no start handler; TestAsyncWorkersFamilyCarriesNoStart
// inspects the table so a future "wire the obvious method" fails loudly
// instead of quietly opening the door argued against above.
type AsyncWorkers interface {
	ListViews(workerType, componentRoot string, states []string, limit int) (*asyncworkers.ListResult, error)
	GetView(workerID string) (*asyncworkers.View, error)
	Cancel(workerID string) error
	Types() []string
	StartView(workerType, componentRoot, dedupeKey string, metadata map[string]string) (*asyncworkers.View, bool, error)
	// StartViewAudited is S54 T1's audited start: the platform_change row
	// commits in the SAME transaction as the queued ledger row (SP3), so a
	// flight collision writes neither. Only the two ledger-carrying
	// OPERATIONS walk it — retrieval.reindex and graph.build.
	StartViewAudited(workerType, componentRoot, dedupeKey string, metadata map[string]string, audit asyncworkers.Audit) (*asyncworkers.View, bool, error)
	// Starter is S54 T1's half of the "already running — started by WHO"
	// answer: the audit row behind a worker id, or (nil, nil) when none
	// names a person — a write-hook pass is the daemon's act (SP4), and
	// the door then answers without a name rather than inventing one.
	Starter(ctx context.Context, workerID string) (*asyncworkers.StarterView, error)
}

// --- params ---------------------------------------------------------------

// AsyncWorkersListParams: {workerType?, componentRoot?, states?, limit?}.
// Every narrowing is optional — the page-mount question is exactly
// "anything of THIS type for THIS root", and states lets a screen ask for
// just the live rows without a remembered list on the client.
type AsyncWorkersListParams struct {
	WorkerType    string   `json:"workerType,omitempty"`
	ComponentRoot string   `json:"componentRoot,omitempty"`
	States        []string `json:"states,omitempty"`
	Limit         int      `json:"limit,omitempty"`
}

// AsyncWorkersGetParams: {workerId}.
type AsyncWorkersGetParams struct {
	WorkerID string `json:"workerId"`
}

// AsyncWorkersCancelParams: {workerId}.
type AsyncWorkersCancelParams struct {
	WorkerID string `json:"workerId"`
}

// RegisterAsyncWorkers puts the three reads on the table — or none of
// them (nil seam, V13). Returns whether the family is in the table so the
// daemon's wiring can be asserted rather than trusted.
func (t *Table) RegisterAsyncWorkers() bool {
	if t.AsyncWorkers == nil {
		return false
	}
	for _, m := range []struct {
		name   MethodName
		handle func(context.Context, *Session, json.RawMessage) (any, error)
	}{
		{MethodAsyncWorkersList, t.handleAsyncWorkersList},
		{MethodAsyncWorkersGet, t.handleAsyncWorkersGet},
		{MethodAsyncWorkersCancel, t.handleAsyncWorkersCancel},
	} {
		t.register(&Method{Name: m.name, RequiresInitialize: true, Handle: m.handle})
	}
	return true
}

func asyncWorkersRefused(method string) error {
	return ErrStoreUnavailable.WithWhat("asyncworkers.%s needs the async-worker manager, and this daemon did not wire it (no store) — the ledger is empty because there is no ledger", method)
}

func (t *Table) handleAsyncWorkersList(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.AsyncWorkers == nil {
		return nil, asyncWorkersRefused("list")
	}
	var p AsyncWorkersListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	for _, s := range p.States {
		if !knownWorkerState(s) {
			return nil, ErrInvalidScope.WithWhat("asyncworkers.list refused state %q — the ledger's vocabulary is queued, running, finished, failed, cancelled, interrupted (the migration's CHECK is the authority)", s)
		}
	}
	return t.AsyncWorkers.ListViews(p.WorkerType, p.ComponentRoot, p.States, p.Limit)
}

func (t *Table) handleAsyncWorkersGet(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.AsyncWorkers == nil {
		return nil, asyncWorkersRefused("get")
	}
	var p AsyncWorkersGetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.WorkerID) == "" {
		return nil, ErrInvalidScope.WithWhat("asyncworkers.get needs a workerId — an empty id matches nothing and answering the question anyway is how a lookup lies")
	}
	view, err := t.AsyncWorkers.GetView(p.WorkerID)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrInvalidScope.WithWhat("asyncworkers.get found no worker %q — ids come from the owning service's start answer or from asyncworkers.list, never invented", p.WorkerID)
		}
		return nil, ErrStoreUnavailable.WithWhat("asyncworkers.get %s: %v", p.WorkerID, err)
	}
	return view, nil
}

func (t *Table) handleAsyncWorkersCancel(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.AsyncWorkers == nil {
		return nil, asyncWorkersRefused("cancel")
	}
	var p AsyncWorkersCancelParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.WorkerID) == "" {
		return nil, ErrInvalidScope.WithWhat("asyncworkers.cancel needs a workerId")
	}
	if err := t.AsyncWorkers.Cancel(p.WorkerID); err != nil {
		if isNoRows(err) {
			return nil, ErrInvalidScope.WithWhat("asyncworkers.cancel found no worker %q", p.WorkerID)
		}
		return nil, ErrInvalidScope.WithWhat("asyncworkers.cancel: %v", err)
	}
	// The answer is the row's CURRENT truth after the request: a queued
	// worker is already cancelled; a running one reads running with
	// cancelRequested, and the event stream carries its ending.
	view, err := t.AsyncWorkers.GetView(p.WorkerID)
	if err != nil {
		return nil, ErrStoreUnavailable.WithWhat("asyncworkers.cancel %s: cancel landed but the read-back failed: %v", p.WorkerID, err)
	}
	return view, nil
}

// knownWorkerState is the ledger's six-state vocabulary, checked as a
// courtesy so a typo surfaces as a named refusal rather than a silently
// empty page. The database CHECK is still the authority (this list and
// the migration's CHECK are pinned equal by asyncworkers' own tests).
func knownWorkerState(s string) bool {
	switch s {
	case "queued", "running", "finished", "failed", "cancelled", "interrupted":
		return true
	}
	return false
}

// isNoRows recognises the store's "no such row" without leaking the
// database package into any decision above this line.
func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

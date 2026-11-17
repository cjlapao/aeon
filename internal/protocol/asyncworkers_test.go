package protocol

// asyncworkers_test.go — the ledger's socket doors over a fake manager:
// registration is conditional on the seam (V13), the family carries NO
// start method (the closed door argued in asyncworkers.go), refusals name
// what they refused, and answers pass through verbatim — the handler's
// whole job is marshaling, same proof shape as graph_test.go.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/asyncworkers"
)

type fakeWorkers struct {
	listCalls  []string
	startCalls []string
	cancelCall string

	listRes *asyncworkers.ListResult
	view    *asyncworkers.View
	getErr  error
	startEr error
	cancErr error
	audit   *asyncworkers.Audit        // S54 T1: the last audit the fake was handed
	starter *asyncworkers.StarterView  // S54 T1: the already-running name to answer
	noStart bool                       // S54 T1: answer started:false (flight collision)
}

func (f *fakeWorkers) ListViews(workerType, componentRoot string, states []string, limit int) (*asyncworkers.ListResult, error) {
	f.listCalls = append(f.listCalls, workerType+"|"+componentRoot+"|"+strings.Join(states, ",")+"|"+string(rune('0'+limit)))
	if f.listRes == nil {
		return &asyncworkers.ListResult{Types: []string{"graph.build"}}, nil
	}
	return f.listRes, nil
}

func (f *fakeWorkers) GetView(workerID string) (*asyncworkers.View, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.view == nil {
		return &asyncworkers.View{WorkerID: workerID, WorkerType: "graph.build", State: "running", QueuedAt: "2026-10-30T17:41:02.000Z"}, nil
	}
	v := *f.view
	v.WorkerID = workerID
	return &v, nil
}

func (f *fakeWorkers) Cancel(workerID string) error {
	f.cancelCall = workerID
	return f.cancErr
}

func (f *fakeWorkers) Types() []string { return []string{"graph.build"} }

func (f *fakeWorkers) StartView(workerType, componentRoot, dedupeKey string, metadata map[string]string) (*asyncworkers.View, bool, error) {
	f.startCalls = append(f.startCalls, workerType+" "+componentRoot+" "+dedupeKey)
	if f.startEr != nil {
		return nil, false, f.startEr
	}
	return &asyncworkers.View{WorkerID: "aw_fake1", WorkerType: workerType, ComponentRoot: componentRoot, State: "queued", QueuedAt: "t"}, !f.noStart, nil
}

// S54 T1 enumerated adaptation: the seam grew StartViewAudited (the two
// ledger operations' audited start). The fake answers identically to the
// plain twin and records the audit so S54's own tests can assert WHAT the
// door composed.
func (f *fakeWorkers) StartViewAudited(workerType, componentRoot, dedupeKey string, metadata map[string]string, audit asyncworkers.Audit) (*asyncworkers.View, bool, error) {
	f.audit = &audit
	return f.StartView(workerType, componentRoot, dedupeKey, metadata)
}

// Starter is the fake's half of the already-running naming (S54 T1): the
// test sets f.starter to answer "user:42 started this", leaves it nil
// for the honest no-name (hook pass or absent audit).
func (f *fakeWorkers) Starter(context.Context, string) (*asyncworkers.StarterView, error) {
	return f.starter, nil
}

func asyncWorkersHas(tab *Table, name MethodName) bool {
	for _, m := range append(tab.Methods(), tab.PublicMethods()...) {
		if m == name {
			return true
		}
	}
	return false
}

func awCall(t *testing.T, tab *Table, name MethodName, params any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	switch name {
	case MethodAsyncWorkersList:
		return tab.handleAsyncWorkersList(ctx, nil, raw)
	case MethodAsyncWorkersGet:
		return tab.handleAsyncWorkersGet(ctx, nil, raw)
	case MethodAsyncWorkersCancel:
		return tab.handleAsyncWorkersCancel(ctx, nil, raw)
	}
	t.Fatalf("unknown method %s", name)
	return nil, nil
}

func TestAsyncWorkersMethodsAbsentWithoutSeam(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tab.RegisterAsyncWorkers() {
		t.Fatal("RegisterAsyncWorkers reported success with a nil seam")
	}
	for _, name := range asyncWorkersFamily {
		if asyncWorkersHas(tab, name) {
			t.Errorf("%s is registered without the seam — V13: absent, not registered-and-refusing", name)
		}
	}
}

var asyncWorkersFamily = []MethodName{MethodAsyncWorkersList, MethodAsyncWorkersGet, MethodAsyncWorkersCancel}

func TestAsyncWorkersMethodsRegisterWithSeam(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tab.AsyncWorkers = &fakeWorkers{}
	if !tab.RegisterAsyncWorkers() {
		t.Fatal("RegisterAsyncWorkers refused with a wired seam")
	}
	for _, name := range asyncWorkersFamily {
		if !asyncWorkersHas(tab, name) {
			t.Errorf("%s missing from the table", name)
		}
	}
}

// TestAsyncWorkersFamilyCarriesNoStart — the door that must never open.
// A generic start would let a caller name a Go function it cannot supply
// arguments for; starting belongs to the owning service's own method
// (graph.build starts the graph.build worker). This test reads the table
// itself, so wiring "the obvious missing method" fails here.
func TestAsyncWorkersFamilyCarriesNoStart(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tab.AsyncWorkers = &fakeWorkers{}
	tab.RegisterAsyncWorkers()
	for _, m := range append(tab.Methods(), tab.PublicMethods()...) {
		if strings.HasPrefix(string(m), "asyncworkers.") {
			for _, banned := range []string{"asyncworkers.start", "asyncworkers.register", "asyncworkers.fire"} {
				if string(m) == banned {
					t.Errorf("%s is registered — the registry holds Go functions; a generic start door is the V21 refusal, permanent", banned)
				}
			}
		}
	}
}

func TestAsyncWorkersRefusalsNameWhatTheyRefused(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	f := &fakeWorkers{}
	tab.AsyncWorkers = f
	tab.RegisterAsyncWorkers()

	cases := []struct {
		name   MethodName
		params any
		want   string
	}{
		{MethodAsyncWorkersGet, AsyncWorkersGetParams{WorkerID: "  "}, "workerId"},
		{MethodAsyncWorkersCancel, AsyncWorkersCancelParams{}, "workerId"},
		{MethodAsyncWorkersList, AsyncWorkersListParams{States: []string{"almost-done"}}, "almost-done"},
	}
	for i, c := range cases {
		_, err := awCall(t, tab, c.name, c.params)
		if err == nil {
			t.Errorf("case %d (%s): want refusal, got nil", i, c.name)
			continue
		}
		var e *Error
		if !errors.As(err, &e) || !strings.Contains(e.Data.What, c.want) {
			t.Errorf("case %d (%s): refusal %v does not name %q in its What", i, c.name, err, c.want)
		}
	}
}

func TestAsyncWorkersGetUnknownIdNamesIt(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	f := &fakeWorkers{getErr: sql.ErrNoRows}
	tab.AsyncWorkers = f
	tab.RegisterAsyncWorkers()
	_, err := awCall(t, tab, MethodAsyncWorkersGet, AsyncWorkersGetParams{WorkerID: "aw_nope"})
	var e *Error
	if !errors.As(err, &e) || !strings.Contains(e.Data.What, "aw_nope") {
		t.Fatalf("unknown-id refusal %v must name the id it refused", err)
	}
}

func TestAsyncWorkersListPassesFiltersThrough(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	f := &fakeWorkers{listRes: &asyncworkers.ListResult{
		Workers: []asyncworkers.View{{WorkerID: "aw_x", WorkerType: "graph.build", State: "running"}},
		Types:   []string{"graph.build"},
	}}
	tab.AsyncWorkers = f
	tab.RegisterAsyncWorkers()
	res, err := awCall(t, tab, MethodAsyncWorkersList, AsyncWorkersListParams{
		WorkerType: "graph.build", ComponentRoot: "/r", States: []string{"running"}, Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	lr := res.(*asyncworkers.ListResult)
	if len(lr.Workers) != 1 || lr.Workers[0].State != "running" {
		t.Errorf("the manager's answer was mangled: %+v", lr)
	}
	if f.listCalls[0] != "graph.build|/r|running|5" {
		t.Errorf("filters reached the manager as %q", f.listCalls[0])
	}
}

func TestAsyncWorkersCancelAnswersWithTheCurrentRow(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	f := &fakeWorkers{view: &asyncworkers.View{WorkerType: "graph.build", State: "running", CancelRequested: true}}
	tab.AsyncWorkers = f
	tab.RegisterAsyncWorkers()
	res, err := awCall(t, tab, MethodAsyncWorkersCancel, AsyncWorkersCancelParams{WorkerID: "aw_x"})
	if err != nil {
		t.Fatal(err)
	}
	v := res.(*asyncworkers.View)
	if !v.CancelRequested || v.State != "running" {
		t.Errorf("post-cancel answer %+v, want running WITH cancelRequested (the goroutine owns the ending)", v)
	}
	if f.cancelCall != "aw_x" {
		t.Errorf("manager cancel saw %q", f.cancelCall)
	}

	f.cancErr = errors.New("the worker is finished, there is nothing to cancel")
	_, err = awCall(t, tab, MethodAsyncWorkersCancel, AsyncWorkersCancelParams{WorkerID: "aw_x"})
	var e *Error
	if !errors.As(err, &e) || !strings.Contains(e.Data.What, "finished") {
		t.Fatalf("terminal cancel refusal %v must carry the manager's reason", err)
	}
}

// TestGraphBuildStartsAWorkerNotABuildInline — S64's contract on the
// graph door: graph.build names the worker (normalized absolute root as
// both componentRoot and flight key), never the build itself.
func TestGraphBuildStartsAWorkerNotABuildInline(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	g := &fakeGraph{}
	tab.Graph = g
	if !tab.RegisterGraph() {
		t.Fatal("graph family absent")
	}
	_, err := graphCall(t, tab, MethodGraphBuild, GraphBuildParams{ComponentRoot: "/r"})
	var e *Error
	if !errors.As(err, &e) || !strings.Contains(e.Data.What, "async-worker manager") {
		t.Fatalf("graph.build without the manager refused with %v — it must say who is missing, not run inline", err)
	}
	if len(g.calls) != 0 {
		t.Errorf("the graph service was touched by a refused build: %v", g.calls)
	}

	f := &fakeWorkers{}
	tab.AsyncWorkers = f
	// fresh table so registration sees the seam
	tab2 := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tab2.Graph = g
	tab2.AsyncWorkers = f
	// S54 T1 enumerated adaptation: the build door is audited — it
	// registers only with a stamping source, and the fake manager now
	// receives StartViewAudited (which the fake routes to the same
	// recording twin, so the assertions below stand unchanged).
	tab2.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "4242"} }
	tab2.RegisterGraph()
	res, err := graphCall(t, tab2, MethodGraphBuild, GraphBuildParams{ComponentRoot: "rel/path"})
	if err != nil {
		t.Fatal(err)
	}
	acc := res.(*GraphBuildAccepted)
	if acc.WorkerID == "" || acc.State != "queued" || !acc.Started {
		t.Errorf("handle answer %+v, want a started queued worker", acc)
	}
	abs, _ := filepath.Abs("rel/path")
	if f.startCalls[0] != "graph.build "+abs+" "+abs {
		t.Errorf("StartView saw %q, want type with the ABSOLUTE root as componentRoot AND flight key", f.startCalls[0])
	}
	for _, c := range g.calls {
		if strings.HasPrefix(c, "build ") {
			t.Errorf("the build ran INLINE on the socket call again: %v", g.calls)
		}
	}
}

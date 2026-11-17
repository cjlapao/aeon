package protocol

// S22 T8 — the status and deliberate-reindex doors. Registration rides the
// seams (V13: absent, not registered-and-refusing); the refusals name their
// missing piece; and the handlers pass through to THE injected function and
// nothing else — no handler here may start work by itself (V21; the spy
// proof spans status/query/startup in the daemon's
// TestRetrievalReindexSocketNeverAutoFires, this file holds the door's own
// shape).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/asyncworkers"
	"github.com/cjlapao/aeon/internal/search"
	"github.com/cjlapao/aeon/internal/store"
)

func newStatusTable(status search.StatusFunc, reindex search.ReindexFunc) *Table {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.RetrievalStatus = status
	tbl.RetrievalReindex = reindex
	// S54 T1 enumerated adaptation: the reindex door is an audited
	// OPERATION and registers only over its seam AND a stamping source.
	// The test table stands in for the daemon's peer-credential wiring.
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "4242"} }
	tbl.RegisterRetrievalStatus()
	tbl.RegisterRetrievalReindex()
	return tbl
}

// refusalWhat digs the three-part sentence out of a protocol error; a
// plain error contributes its own text.
func refusalWhat(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Data.What
	}
	return err.Error()
}

func TestStatusAndReindexAreAbsentWithoutTheirSeams(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tbl.RegisterRetrievalStatus() || tbl.RegisterRetrievalReindex() {
		t.Fatal("a T8 door registered without its seam — the nil answer is the V13 build speaking")
	}
	for _, m := range tbl.Methods() {
		if m == MethodRetrievalStatus || m == MethodRetrievalReindex {
			t.Errorf("%s is registered without its seam — absence means it does not answer at all", m)
		}
	}
	live := newStatusTable(
		func(context.Context, string) (search.StatusAnswer, error) { return search.StatusAnswer{}, nil },
		func(context.Context, string, string, string, bool, func(int, int)) (search.ReindexOutcome, error) {
			return search.ReindexOutcome{}, nil
		})
	seen := map[MethodName]bool{}
	for _, m := range live.Methods() {
		seen[m] = true
	}
	if !seen[MethodRetrievalStatus] || !seen[MethodRetrievalReindex] {
		t.Fatalf("wired seams did not register their doors: %v", seen)
	}
}

func TestStatusAndReindexRefuseWithoutRootAndCorpus(t *testing.T) {
	var asked int
	tbl := newStatusTable(
		func(context.Context, string) (search.StatusAnswer, error) { asked++; return search.StatusAnswer{}, nil },
		func(context.Context, string, string, string, bool, func(int, int)) (search.ReindexOutcome, error) {
			asked++
			return search.ReindexOutcome{}, nil
		})
	cases := []struct {
		method MethodName
		params string
		want   string
	}{
		{MethodRetrievalStatus, `{}`, "componentRoot"},
		{MethodRetrievalReindex, `{"corpus":"code"}`, "componentRoot"},
		{MethodRetrievalReindex, `{"componentRoot":"/r"}`, `"code" or "history"`},
		{MethodRetrievalReindex, `{"componentRoot":"/r","corpus":"wiki"}`, `"code" or "history"`},
	}
	for _, c := range cases {
		// The handlers are entered directly (the T6 shape): initialize
		// would refuse before the door's own checks and test the boot
		// guard twice, not the refusals.
		var err error
		switch c.method {
		case MethodRetrievalStatus:
			_, err = tbl.handleRetrievalStatus(context.Background(), nil, json.RawMessage(c.params))
		case MethodRetrievalReindex:
			_, err = tbl.handleRetrievalReindex(context.Background(), nil, json.RawMessage(c.params))
		}
		// protocol.Error renders Code+Message; the named refusal lives in
		// Data.What (the three-part shape), so THAT is what is inspected —
		// the err.Error()-contains-the-reason mistake T6 already learned.
		if err == nil {
			t.Fatalf("%s %s: not refused at all", c.method, c.params)
		}
		if !strings.Contains(refusalWhat(err), c.want) {
			t.Errorf("%s %s: refused %v, want the refusal to name %q", c.method, c.params, err, c.want)
		}
	}
	if asked != 0 {
		t.Errorf("a refused call reached the seam %d time(s) — refusals die at the door", asked)
	}
}

func TestStatusPassesThroughToItsSeam(t *testing.T) {
	var stRoot string
	tbl := newStatusTable(
		func(_ context.Context, root string) (search.StatusAnswer, error) {
			stRoot = root
			return search.StatusAnswer{Root: root, Corpora: []search.CorpusStatus{{Corpus: "code", Files: 3, StaleFiles: 1}}}, nil
		}, nil)
	initializedOnce(t, tbl, "c1")
	res, err := tbl.Dispatch(context.Background(), "c1", MethodRetrievalStatus, mustJSON(t, RetrievalStatusParams{ComponentRoot: "/proj"}))
	if err != nil {
		t.Fatal(err)
	}
	if a := res.(search.StatusAnswer); a.Root != "/proj" || stRoot != "/proj" || len(a.Corpora) != 1 || a.Corpora[0].StaleFiles != 1 {
		t.Fatalf("status did not pass through: %+v", a)
	}
}

// TestReindexPassesThroughToItsSeam is the reindex half of the old
// pass-through test, restated for the ASYNC contract (S64): the door no
// longer runs the injected seam on the caller's socket — it starts the
// worker type through the async-worker manager and answers with a handle.
// What reaches the seam is the runner the manager fires, and it must carry
// the door's own scoping decision: root, corpus, embedRoute and force (O8/V17
// — the runner names no corpus of its own).
//
// The manager needs a ledger, so this builds one over a TEMP store — the
// protocol package's own fixture precedent (history_test.go). Not the real
// store: a test that opened the operator's one would be a bug the test
// cannot see. The daemon package's socket-level tests cover the same path
// end to end; this one covers the HANDLER's shaping, which is protocol code.
func TestReindexPassesThroughToItsSeam(t *testing.T) {
	var gotRoot, gotCorpus, gotRoute string
	var gotForce bool
	var passes int
	runner := func(_ context.Context, job asyncworkers.Job, _ asyncworkers.Reporter) (json.RawMessage, error) {
		passes++
		gotRoot, gotCorpus, gotRoute, gotForce = job.ComponentRoot, job.Metadata["corpus"], job.Metadata["embedRoute"], job.Metadata["force"] == "true"
		out, err := json.Marshal(search.ReindexOutcome{Corpus: gotCorpus, Files: 3, Embedded: 2, CostKnown: true})
		return out, err
	}

	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// The bare manager has no event voice by default; wire one to a capture
	// so this protocol-level test can assert the pass owns its event
	// lifecycle. (The daemon test verifies the same trail through the
	// daemon's own publish bridge; here we pin that the MANAGER fires it.)
	kinds := make(chan string, 8)
	m := asyncworkers.New(st, asyncworkers.Options{Publish: func(kind, _ string, _ any) { kinds <- kind }})
	if err := m.Register("retrieval.reindex", runner, asyncworkers.WorkerOpts{}); err != nil {
		t.Fatalf("register the retrieval pass: %v", err)
	}

	rt := "other-embed"
	// The injected seam is only there to make RegisterRetrievalReindex fire.
	// If the door called it directly, the pass would bypass the manager — and
	// the counting below turns that into a failure rather than a shrug.
	var directCalls int
	tbl := newStatusTable(nil, func(_ context.Context, root, corpus, route string, force bool, _ func(int, int)) (search.ReindexOutcome, error) {
		directCalls++
		return search.ReindexOutcome{Corpus: corpus}, nil
	})
	tbl.AsyncWorkers = m
	initializedOnce(t, tbl, "c1")

	res, err := tbl.Dispatch(context.Background(), "c1", MethodRetrievalReindex, mustJSON(t, RetrievalReindexParams{ComponentRoot: "/proj", Corpus: "code", EmbedRoute: &rt, Force: true}))
	if err != nil {
		t.Fatal(err)
	}
	acc, ok := res.(*RetrievalReindexAccepted)
	if !ok {
		t.Fatalf("reindex answered %T — want *RetrievalReindexAccepted, the handle contract", res)
	}
	if !acc.Started || acc.WorkerID == "" {
		t.Fatalf("handle %+v — want a started ledger worker", acc)
	}

	// The handle returns in milliseconds; the worker takes its own moment.
	// The poll is bounded by the same budget the daemon test uses. Reading the
	// runner's inputs before the worker reaches terminal would assert against
	// a pass that has not run yet — a race, not a contract.
	var view *asyncworkers.View
	deadline := time.Now().Add(30 * time.Second)
	for {
		var err error
		view, err = m.GetView(acc.WorkerID)
		if err != nil {
			t.Fatal(err)
		}
		if view.State != "queued" && view.State != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker %s still %s after 30s", acc.WorkerID, view.State)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if passes != 1 || gotRoot != "/proj" || gotCorpus != "code" || gotRoute != "other-embed" || !gotForce {
		t.Fatalf("the runner reached the seam as root=%q corpus=%q route=%q force=%v, want the door's own scoping (O8/V17)", gotRoot, gotCorpus, gotRoute, gotForce)
	}
	if directCalls != 0 {
		t.Errorf("%d direct calls to the injected reindex seam — the door must start the worker, not run the pass itself (V21)", directCalls)
	}
	var out search.ReindexOutcome
	if err := json.Unmarshal(view.Result, &out); err != nil {
		t.Fatalf("row result does not decode: %v (%s)", view.Result, err)
	}
	if out.Embedded != 2 || !out.CostKnown {
		t.Errorf("the finished report lost the pass's figures: %+v", out)
	}
	// The event trail is the app's polling source — a started event means
	// "something to watch", and its absence would be a silent pass. The
	// manager writes the terminal row before it publishes the finished
	// event (finish), so by the time the poll saw terminal every event has
	// been sent; the drain waits only for the finished marker, not forever.
	gotKinds := []string{}
	for {
		select {
		case k := <-kinds:
			gotKinds = append(gotKinds, k)
			if k == asyncworkers.KindFinished {
				goto drained
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no finished event after 5s (got %v)", gotKinds)
		}
	}
drained:
	if len(gotKinds) == 0 || gotKinds[0] != asyncworkers.KindStarted || gotKinds[len(gotKinds)-1] != asyncworkers.KindFinished {
		t.Errorf("event kinds = %v, want started … finished — the pass owns its event lifecycle", gotKinds)
	}
}

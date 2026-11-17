package daemon

// S22 T8 / S64 — the retrieval pass is ASYNC, the way the graph's was added,
// and the tests measure it the way the task demands: STEP TIMINGS and LEDGER
// ROWS, never lock inspection. The single flight the old in-memory reindex
// tracker held is now the manager's durable flight index (the graph's
// precedent): socket door, CLI verb and write-hook trigger all START the same
// worker type through the same manager, so button and hook cannot double-pass.
//
// Every test here builds a real store-backed daemon with a SPY seam and reads
// the running truth back out of the ledger — the same walk the graph's async
// tests take.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/asyncworkers"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/search"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

func mustJSONT8(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// newReindexDaemon is the wiring under every assertion below: one store, the
// manager inside it, and the reindex seams under test. A daemon built WITHOUT
// a store has no manager and no retrieval doors (V13) — a separate test pins
// that, because it is the quiet half of the change.
func newReindexDaemon(t *testing.T, reindex search.ReindexFunc, status search.StatusFunc) *Daemon {
	return newReindexDaemonWithQuery(t, reindex, status, nil)
}

// newReindexDaemonWithQuery is the same wiring plus the search door. A daemon
// has no search.query without a Search seam (V13 — registration is decided at
// New, not patched on afterwards), and the "nothing fires automatically"
// claim should cover the query half as well as the status half.
func newReindexDaemonWithQuery(t *testing.T, reindex search.ReindexFunc, status search.StatusFunc, query search.SearchResolver) *Daemon {
	t.Helper()
	// S54 T1 enumerated adaptation: retrieval.reindex is now an audited
	// OPERATION door — it registers only over a stamping source, and a
	// dispatch is refused unless the connection's peer is a nameable
	// person. The fixture records that source the way the transport
	// does in production: one peer credential per named connection.
	d := New(Options{Store: openStoreForGraph(t), Reindex: reindex, CorpusStatus: status, Search: query, Connections: testPeerConns("c1", "c2")})
	if d.Workers() == nil {
		t.Fatal("the daemon has no async-worker manager — a pass would hold a socket for thirty minutes")
	}
	types := d.Workers().Types()
	if !containsWorkerType(types, "retrieval.reindex") {
		t.Fatalf("registered worker types %v — the retrieval pass is not riding the manager", types)
	}
	return d
}

func containsWorkerType(types []string, want string) bool {
	for _, ty := range types {
		if ty == want {
			return true
		}
	}
	return false
}

// heldReindex is a seam that PROVES it started (V8) and stays open until the
// test says so — so a live flight exists to be observed. It records every
// (corpus, route, force) handed to it, which is the scope decision the door
// made and the runner forwarded.
func heldReindex(started chan struct{}, release chan struct{}, calls chan string, once *sync.Once) search.ReindexFunc {
	return func(ctx context.Context, root, corpus, route string, force bool, _ func(int, int)) (search.ReindexOutcome, error) {
		if once != nil {
			once.Do(func() { close(started) })
		}
		calls <- corpus + "|" + route + "|" + boolStr(force)
		<-release
		return search.ReindexOutcome{Corpus: corpus, Files: 1, ChunksWritten: 1}, nil
	}
}

// flightBudget gates a poll for the runner to reach a terminal state; the
// graph test uses the same value.
const flightBudget = 30 * time.Second

// recordedRetrievalRows is the whole ledger for one root, whatever its state —
// the single-flight answer lives in the dedupe keys these rows carry.
func recordedRetrievalRows(t *testing.T, d *Daemon, root string) []*asyncworkers.View {
	t.Helper()
	v, err := d.Workers().ListViews("retrieval.reindex", root, nil, 0)
	if err != nil {
		t.Fatalf("list all workers for %s: %v", root, err)
	}
	var out []*asyncworkers.View
	for i := range v.Workers {
		w := v.Workers[i]
		out = append(out, &w)
	}
	return out
}

// TestReindexNeverDelaysAStep drives the REAL write tool through the daemon's
// own conversion point while a background pass is in flight, and asserts what
// the task names: the step's timings do not notice (V18/D10). The pass is held
// in the LEDGER — reachable by every door — and the status door sees it,
// running, per corpus (O4).
func TestReindexNeverDelaysAStep(t *testing.T) {
	root := t.TempDir()
	started := make(chan struct{})
	release := make(chan struct{})
	calls := make(chan string, 8)
	reindex := heldReindex(started, release, calls, &sync.Once{})

	d := newReindexDaemon(t, reindex, func(ctx context.Context, r string) (search.StatusAnswer, error) {
		return search.StatusAnswer{Root: r, Corpora: []search.CorpusStatus{{Corpus: "code", Files: 1}}}, nil
	})

	set, err := tools.BuildToolset([]string{"write", "read"}, tools.ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	// The REAL conversion point: the step's write signal closed over the
	// daemon's own hook chain (notifyReindex → graph notify), exactly as
	// BuildTools wires it inside a step.
	runner := d.Table.Runner.(StepRunner)
	caller := toolCaller{set: set, onWrite: func(path string) { runner.OnWorkspaceWrite(root, path) }}

	begin := time.Now()
	for i := 0; i < 5; i++ {
		in, _ := json.Marshal(map[string]any{"path": "f.go", "content": "package main\n"})
		out := caller.Call(context.Background(), "write", in)
		if out.IsError {
			t.Fatalf("write failed: %s", out.Content)
		}
	}
	// The loop's write signal reached the pass.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the write path never triggered a pass (V8: the loop already knows it wrote)")
	}
	// Five writes whose pass will NOT finish for a while: the elapsed claim.
	if elapsed := time.Since(begin); elapsed > 250*time.Millisecond {
		t.Errorf("five writes with a reindex in flight took %v — the step is paying the pass's tax (V18/D10)", elapsed)
	}
	// And the status door observes the flight through the same manager.
	a, err := d.Table.RetrievalStatus(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Corpora) != 1 || !a.Corpora[0].Reindexing || a.Corpora[0].Percent != 0 {
		t.Errorf("status does not read the running flight honestly: %+v", a.Corpora)
	}
	wid := workerOf(t, d, root)
	close(release)
	view := waitWorkerTerminal(t, d, wid, flightBudget)
	if view.State != "finished" {
		t.Errorf("the flight ended %s (%s), want finished", view.State, view.Error)
	}
}

// workerOf returns the id of the live retrieval flight for a root.
func workerOf(t *testing.T, d *Daemon, root string) string {
	t.Helper()
	v, err := d.Workers().ListViews("retrieval.reindex", root, []string{"queued", "running"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Workers) == 0 {
		t.Fatal("no live retrieval flight — the pass is not in the ledger")
	}
	return v.Workers[0].WorkerID
}

// TestLoopTriggersIncrementalReindex covers the loop's door (V8/Q7): writes
// announce, non-writes stay silent, and a burst is ONE flight — single flight
// now the manager's durable flight index, keyed per (root, corpus). The
// retrieval package's chunk-level test proves the chunk half; this proves the
// trigger half and the dedupe key the request asked for.
func TestLoopTriggersIncrementalReindex(t *testing.T) {
	root := t.TempDir()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	calls := make(chan string, 8)
	reindex := heldReindex(started, release, calls, &sync.Once{})

	d := newReindexDaemon(t, reindex, func(context.Context, string) (search.StatusAnswer, error) {
		return search.StatusAnswer{}, nil
	})
	set, err := tools.BuildToolset([]string{"write", "edit", "read", "bash"}, tools.ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	runner := d.Table.Runner.(StepRunner)
	caller := toolCaller{set: set, onWrite: func(path string) { runner.OnWorkspaceWrite(root, path) }}

	marshal := func(v any) json.RawMessage {
		b, _ := json.Marshal(v)
		return b
	}
	mustCall := func(name string, in any) {
		t.Helper()
		out := caller.Call(context.Background(), name, marshal(in))
		if out.IsError {
			t.Fatalf("%s failed: %s", name, out.Content)
		}
	}

	// Three writes in one burst: same root, same corpus, same flight window.
	mustCall("write", map[string]any{"path": "a.go", "content": "package a\n"})
	mustCall("write", map[string]any{"path": "b.go", "content": "package b\n"})
	mustCall("edit", map[string]any{"path": "a.go", "old_string": "package a\n", "new_string": "package a2\n"})

	select {
	case got := <-calls:
		if got != "code||false" {
			t.Errorf("the trigger pass named %q — it must be code, the configured route, never forced (V21/V17)", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write/edit triggered no pass")
	}
	// Let the other two hook goroutines land — they must land on the SAME
	// flight rather than start new ones.
	time.Sleep(200 * time.Millisecond)

	rows := recordedRetrievalRows(t, d, root)
	if len(rows) != 1 {
		t.Fatalf("a burst of three writes produced %d ledger rows, want ONE single flight", len(rows))
	}
	if got := ledgerRow(t, d, rows[0].WorkerID).DedupeKey; got != abs+"\x00code" {
		t.Errorf("the flight is not keyed per (root, corpus) as the request asked: %q, want %q", got, abs+"\x00code")
	}
	if rows[0].Metadata["corpus"] != "code" || rows[0].Progress != nil {
		t.Errorf("the row lost the corpus (%q) or carried a fabricated progress figure (%v)", rows[0].Metadata["corpus"], rows[0].Progress)
	}

	// Non-writes must NOT announce.
	mustCall("read", map[string]any{"path": "a.go"})
	// A write the tool itself refuses must not announce either — the refusal
	// is the boundary, not a write.
	if escaped := caller.Call(context.Background(), "write", marshal(map[string]any{"path": "x/../escape", "content": "x"})); !escaped.IsError {
		t.Fatalf("the escaping write was not refused by the tool: %s", escaped.Content)
	}
	// bash that happens to touch files is not a write signal either (Q7's
	// closed set is write/edit). In THIS toolset it cannot even run — no
	// policy source, by design — which is the same answer: nothing announces.
	if out := caller.Call(context.Background(), "bash", marshal(map[string]any{"command": "touch c.go", "description": "noop"})); out.IsError {
		t.Logf("bash refused by the toolset (no policy source): %s — it announces nothing either way", out.Content)
	}
	select {
	case extra := <-calls:
		t.Errorf("a non-write announced a second pass: %s", extra)
	case <-time.After(300 * time.Millisecond):
	}
	if rows = recordedRetrievalRows(t, d, root); len(rows) != 1 {
		t.Errorf("non-writes changed the ledger: %d rows, the flight must still be the one the write announced", len(rows))
	}

	close(release)
	view := waitWorkerTerminal(t, d, rows[0].WorkerID, flightBudget)
	if view.State != "finished" {
		t.Errorf("the flight ended %s (%s), want finished", view.State, view.Error)
	}
}

// TestRetrievalReindexSocketNeverAutoFires is V21 against a SPY and the
// LEDGER: status reads, search reads, daemon startup — each must leave the
// reindex flight count at zero; only an explicit retrieval.reindex moves it,
// and it moves exactly one, keyed per (root, corpus).
func TestRetrievalReindexSocketNeverAutoFires(t *testing.T) {
	var passes int
	stale := func(ctx context.Context, root string) (search.StatusAnswer, error) {
		return search.StatusAnswer{Root: root, Corpora: []search.CorpusStatus{
			{Corpus: "code", Files: 900, StaleFiles: 42, PendingEmbed: 7},
			{Corpus: "history", Files: 12},
		}}, nil
	}
	seam := func(ctx context.Context, root, corpus, _ string, _ bool, _ func(int, int)) (search.ReindexOutcome, error) {
		passes++
		return search.ReindexOutcome{Corpus: corpus}, nil
	}
	d := newReindexDaemonWithQuery(t, seam, stale, func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		return search.SearchAnswer{Hits: []search.SearchHit{{Source: "/r/known.go", LineStart: 4, LineEnd: 5}}}, nil
	})

	ctx := context.Background()
	initConn(t, d, ctx, "c1", "never-auto")

	// Three stale status reads: the stale figure survives, and nothing fires.
	for i := 0; i < 3; i++ {
		res, err := d.Table.Dispatch(ctx, "c1", protocol.MethodRetrievalStatus, mustJSONT8(t, protocol.RetrievalStatusParams{ComponentRoot: "/r"}))
		if err != nil {
			t.Fatal(err)
		}
		if a := res.(search.StatusAnswer); a.Corpora[0].StaleFiles != 42 {
			t.Fatalf("status lost the stale figure: %+v", a.Corpora)
		}
	}
	if passes != 0 {
		t.Fatalf("three stale status reads fired %d pass(es) (V21)", passes)
	}
	if rows := recordedRetrievalRows(t, d, "/r"); len(rows) != 0 {
		t.Fatalf("status reads left ledger rows behind: %d", len(rows))
	}
	if res, err := d.Table.Dispatch(ctx, "c1", protocol.MethodSearchQuery, mustJSONT8(t, protocol.SearchQueryParams{ComponentRoot: "/r", Corpus: "code", Query: "x"})); err != nil {
		t.Fatalf("a stale query refused: %v", err)
	} else if a := res.(search.SearchAnswer); len(a.Hits) != 1 || a.Hits[0].Source != "/r/known.go" {
		t.Errorf("the query door answered %+v — it must ride the same Search seam the CLI uses, and must not reindex", a)
	}
	if passes != 0 {
		t.Fatalf("%d passes after a stale query (V21)", passes)
	}
	// The explicit door is the only thing that moves it — and it moves ONE.
	acc := dispatched(t, d, ctx, "c1", protocol.RetrievalReindexParams{ComponentRoot: "/r", Corpus: "history"})
	if !acc.Started || acc.WorkerID == "" {
		t.Fatalf("handle %+v — want a started ledger worker", acc)
	}
	view := waitWorkerTerminal(t, d, acc.WorkerID, flightBudget)
	if view.State != "finished" {
		t.Fatalf("worker ended %s (%s), want finished", view.State, view.Error)
	}
	// The counter is read AFTER the worker reaches its terminal state — the
	// handle returns in milliseconds, and reading it before the pass runs
	// would assert nothing but a race.
	if passes != 1 {
		t.Fatalf("%d passes after one explicit reindex — exactly one is the contract (worker %s, error %q)", passes, view.State, view.Error)
	}
	rows := recordedRetrievalRows(t, d, "/r")
	if len(rows) != 1 || !equalStrings(keys(t, d, rows), []string{"/r\x00history"}) {
		t.Fatalf("the explicit door's flight is not the per-(root,corpus) one: %d rows, keys %v", len(rows), keys(t, d, rows))
	}
	if kinds := waitWorkerEventKinds(t, d, flightBudget); len(kinds) < 2 || kinds[0] != protocol.KindAsyncWorkerStarted || kinds[len(kinds)-1] != protocol.KindAsyncWorkerFinished {
		t.Errorf("event kinds = %v, want started … finished — the pass owns its event lifecycle", kinds)
	}

	// And the storeless, seamless daemon holds neither doors nor a manager.
	d2 := New(Options{})
	r2, ok := d2.Table.Runner.(StepRunner)
	if !ok || r2.Reindex != nil || r2.CorpusStatus != nil || d2.Table.RetrievalStatus != nil || d2.Table.RetrievalReindex != nil || d2.Workers() != nil {
		t.Error("a daemon built without retrieval grew T8 doors and a manager from nothing (V13)")
	}
	for _, name := range []protocol.MethodName{"retrieval.status", "retrieval.reindex", "asyncworkers.list"} {
		if slicesContains(append(d2.Table.Methods(), d2.Table.PublicMethods()...), name) {
			t.Errorf("%s is on a storeless table — absent, not registered-and-refusing (V13)", name)
		}
	}
}

// keys reads the dedupe keys the flights were pinned under. The View is the
// WIRE shape and deliberately carries no key — the single flight is keyed by
// the ledger row, so the row is where the key must be read back.
func keys(t *testing.T, d *Daemon, views []*asyncworkers.View) []string {
	t.Helper()
	var out []string
	for _, v := range views {
		out = append(out, ledgerRow(t, d, v.WorkerID).DedupeKey)
	}
	return out
}

// sameFunc says two SEAM function values are the same one. Go gives a func
// no == (it compares only to nil), so the code pointers do it — and two
// closures over the same code ARE the same value, which is exactly what
// "threaded through the one seam" claims.
func sameFunc(a, b any) bool {
	ra, rb := reflect.ValueOf(a), reflect.ValueOf(b)
	if ra.Kind() != reflect.Func || rb.Kind() != reflect.Func {
		return false
	}
	return ra.Pointer() == rb.Pointer()
}

// equalStrings compares two string slices — slices have no ==, and a test
// that compares them with one does not compile, which is the point.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ledgerRow reads one flight back out of the LEDGER. This is the object the
// single flight is keyed by — which is why the tests assert on it rather than
// on the view the socket handed back.
func ledgerRow(t *testing.T, d *Daemon, workerID string) *store.AsyncWorkerRow {
	t.Helper()
	row, err := d.Workers().Get(workerID)
	if err != nil {
		t.Fatalf("ledger row for %s: %v", workerID, err)
	}
	return row
}

func slicesContains(in []protocol.MethodName, want protocol.MethodName) bool {
	for _, m := range in {
		if m == want {
			return true
		}
	}
	return false
}

// TestSingleFlightPerRootAndCorpus pins S64's dedupe rule at the socket: the
// same (root, corpus) twice yields ONE flight — the second handle says
// started=false and names the first pass; a different corpus is its own
// flight; and the embedRoute/force money decision the door made reaches the
// seam the runner forwarded (V17).
func TestSingleFlightPerRootAndCorpus(t *testing.T) {
	root := t.TempDir()
	started := make(chan struct{})
	release := make(chan struct{})
	calls := make(chan string, 8)
	reindex := heldReindex(started, release, calls, &sync.Once{})
	d := newReindexDaemon(t, reindex, func(context.Context, string) (search.StatusAnswer, error) {
		return search.StatusAnswer{}, nil
	})
	ctx := context.Background()
	initConn(t, d, ctx, "c1", "single-flight")

	first := dispatched(t, d, ctx, "c1", protocol.RetrievalReindexParams{ComponentRoot: root, Corpus: "code"})
	if !first.Started || first.WorkerID == "" {
		t.Fatalf("handle %+v, want a started worker with an id — the pass belongs to the ledger now", first)
	}
	select {
	case got := <-calls:
		if got != "code||false" {
			t.Errorf("the first flight named %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the first flight never reached the seam")
	}
	// Double-click on the same button: the SAME flight, not a second pass —
	// invisible money. Started=false and the first worker id are the answer.
	second := dispatched(t, d, ctx, "c1", protocol.RetrievalReindexParams{ComponentRoot: root, Corpus: "code"})
	if second.Started {
		t.Fatalf("a second call for the same (root, corpus) STARTED a new pass — the manager must hand back the live one")
	}
	if second.WorkerID != first.WorkerID {
		t.Errorf("the second handle names %q — the first pass's id was %q (one flight, one row)", second.WorkerID, first.WorkerID)
	}
	// Another corpus is its own flight, and it carries the money decision.
	route := "embed-v2"
	history := dispatched(t, d, ctx, "c1", protocol.RetrievalReindexParams{ComponentRoot: root, Corpus: "history", EmbedRoute: &route, Force: true})
	if !history.Started || history.WorkerID == first.WorkerID {
		t.Fatalf("history did not get its own started flight: %+v (want started, a different worker id)", history)
	}
	close(release)
	for _, id := range []string{first.WorkerID, history.WorkerID} {
		view := waitWorkerTerminal(t, d, id, flightBudget)
		if view.State != "finished" {
			t.Fatalf("worker %s ended %s (%s), want finished", id, view.State, view.Error)
		}
	}
	got := <-calls
	if got != "history|"+route+"|true" {
		t.Errorf("the history flight reached the seam as %q — the door's embedRoute and force must ride through (V17)", got)
	}
	rows := recordedRetrievalRows(t, d, root)
	if len(rows) != 2 {
		t.Fatalf("%d ledger rows for two corpora, want 2 flight-per-corpus keys", len(rows))
	}
	want := map[string]string{"code": root + "\x00code", "history": root + "\x00history"}
	for _, r := range rows {
		if got := ledgerRow(t, d, r.WorkerID).DedupeKey; want[r.Metadata["corpus"]] != got {
			t.Errorf("corpus %q keyed the ledger as %q, want %q", r.Metadata["corpus"], got, want[r.Metadata["corpus"]])
		}
	}
}

// TestStatusMergesTheRunningFigures proves "reindex running, 61%" is the
// daemon's knowledge reaching the app through the SAME manager the pass runs
// on: the ledger row supplies the state, the seam supplies the fraction — and
// ONLY for the corpus that runs (O4). A pass that never reports progress stays
// honest: running, no figure invented.
func TestStatusMergesTheRunningFigures(t *testing.T) {
	root := t.TempDir()
	started := make(chan struct{})
	release := make(chan struct{})
	pr := make(chan func(int, int), 1)
	reindex := func(ctx context.Context, r, corpus, _ string, _ bool, progress func(int, int)) (search.ReindexOutcome, error) {
		close(started)
		pr <- progress
		<-release
		return search.ReindexOutcome{Corpus: corpus}, nil
	}
	stale := func(ctx context.Context, r string) (search.StatusAnswer, error) {
		return search.StatusAnswer{Root: r, Corpora: []search.CorpusStatus{
			{Corpus: "code", Files: 1204, StaleFiles: 38},
			{Corpus: "history", Files: 71},
		}}, nil
	}
	d := newReindexDaemon(t, reindex, stale)

	ctx := context.Background()
	initConn(t, d, ctx, "c1", "figures")
	if _, err := d.Table.Dispatch(ctx, "c1", protocol.MethodRetrievalReindex, mustJSONT8(t, protocol.RetrievalReindexParams{ComponentRoot: root, Corpus: "code"})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the flight never reached the seam")
	}
	progress := <-pr
	a, err := d.Table.RetrievalStatus(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Corpora) != 2 {
		t.Fatalf("status merged %d corpora, want 2 — the running one must not drop the others", len(a.Corpora))
	}
	// The pass reports 61 of 100 done; the door surfaces it.
	progress(61, 100)
	a, err = d.Table.RetrievalStatus(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Corpora[0].Reindexing {
		t.Errorf("code is not marked reindexing: %+v", a.Corpora[0])
	}
	// 61/100 through float64 can round a hair either way; the WINDOW is the
	// assertion, not a hope.
	if a.Corpora[0].Percent < 60 || a.Corpora[0].Percent > 61 {
		t.Errorf("the running figure did not reach status: %+v", a.Corpora[0])
	}
	if a.Corpora[1].Reindexing || a.Corpora[1].Percent != 0 {
		t.Errorf("history claims a pass it never ran (O4): %+v", a.Corpora[1])
	}
	wid := workerOf(t, d, root)
	close(release)
	view := waitWorkerTerminal(t, d, wid, flightBudget)
	if view.State != "finished" {
		t.Errorf("the flight ended %s (%s), want finished", view.State, view.Error)
	}

	// An unreported pass stays honest too: running, and no fabricated figure.
	started2 := make(chan struct{})
	release2 := make(chan struct{})
	quiet := func(ctx context.Context, r, corpus, _ string, _ bool, _ func(int, int)) (search.ReindexOutcome, error) {
		close(started2)
		<-release2
		return search.ReindexOutcome{Corpus: corpus}, nil
	}
	d2 := newReindexDaemon(t, quiet, func(ctx context.Context, r string) (search.StatusAnswer, error) {
		return search.StatusAnswer{Root: r, Corpora: []search.CorpusStatus{{Corpus: "code"}}}, nil
	})
	initConn(t, d2, ctx, "c2", "quiet")
	if _, err := d2.Table.Dispatch(ctx, "c2", protocol.MethodRetrievalReindex, mustJSONT8(t, protocol.RetrievalReindexParams{ComponentRoot: root, Corpus: "code"})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started2:
	case <-time.After(2 * time.Second):
		t.Fatal("the quiet pass never reached the seam")
	}
	b, err := d2.Table.RetrievalStatus(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Corpora[0].Reindexing || b.Corpora[0].Percent != 0 {
		t.Errorf("an unreported pass must read running with no fabricated figure, got %+v", b.Corpora[0])
	}
	wid2 := workerOf(t, d2, root)
	close(release2)
	view = waitWorkerTerminal(t, d2, wid2, flightBudget)
	if view.State != "finished" {
		t.Errorf("the quiet flight ended %s (%s), want finished", view.State, view.Error)
	}
}

// TestRetrievalDoorsShareOneWiring is D15's call-site lock, restated for the
// async contract: the socket door, the step door and the worker's runner all
// hold the SAME function values; a socket reindex really does run that value
// inside the manager and its result rides the row; and the CLI verbs stay
// clients of the socket (P34), building no index of their own.
func TestRetrievalDoorsShareOneWiring(t *testing.T) {
	var statusCalls, reindexCalls int
	d := newReindexDaemon(t, func(ctx context.Context, root, corpus, _ string, _ bool, _ func(int, int)) (search.ReindexOutcome, error) {
		reindexCalls++
		return search.ReindexOutcome{Corpus: corpus}, nil
	}, func(ctx context.Context, root string) (search.StatusAnswer, error) {
		statusCalls++
		return search.StatusAnswer{Root: root}, nil
	})
	ctx := context.Background()
	r, ok := d.Table.Runner.(StepRunner)
	if !ok || r.CorpusStatus == nil || r.Reindex == nil {
		t.Fatal("the step doors are not the T8 chain")
	}
	initConn(t, d, ctx, "c1", "same-wiring")
	if !sameFunc(r.CorpusStatus, d.Table.RetrievalStatus) {
		t.Error("the step's status door and the socket's are different function values — two chains that can drift")
	}
	if !sameFunc(r.Reindex, d.Table.RetrievalReindex) {
		t.Error("the step's reindex door and the socket's are different function values — two chains that can drift")
	}
	// Each door answers through the SAME chain — called directly and through
	// the socket — and increments the spy exactly once per call.
	if _, err := d.Table.Dispatch(ctx, "c1", protocol.MethodRetrievalStatus, mustJSONT8(t, protocol.RetrievalStatusParams{ComponentRoot: "/r"})); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CorpusStatus(ctx, "/r"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reindex(ctx, "/r", "code", "", false, nil); err != nil {
		t.Fatal(err)
	}
	if statusCalls != 2 || reindexCalls != 1 {
		t.Fatalf("the doors are not the same function chain: status %d (want 2), reindex %d (want 1)", statusCalls, reindexCalls)
	}
	// The socket's reindex door starts the manager's worker over that same
	// value, answers in milliseconds, and the row ends with the report.
	acc := dispatched(t, d, ctx, "c1", protocol.RetrievalReindexParams{ComponentRoot: "/r", Corpus: "code"})
	if !acc.Started || acc.WorkerID == "" {
		t.Fatalf("handle %+v — want a started ledger worker", acc)
	}
	view := waitWorkerTerminal(t, d, acc.WorkerID, flightBudget)
	if view.State != "finished" {
		t.Fatalf("worker ended %s (%s)", view.State, view.Error)
	}
	var out search.ReindexOutcome
	if err := json.Unmarshal(view.Result, &out); err != nil {
		t.Fatalf("row result does not decode as the ReindexOutcome the door used to return inline: %v", err)
	}
	if out.Corpus != "code" {
		t.Errorf("report %+v — the worker reported another corpus than the door asked for", out)
	}
	if reindexCalls != 2 {
		t.Errorf("the worker door ran the seam %d times, want 2 (once direct, once through the manager)", reindexCalls)
	}

	// The structural half, on the bytes: the CLI verbs are clients of the
	// socket — start, follow, print — and build no index of their own.
	verb := mustReadFileT8(t, "../../cmd/aeon/index.go")
	for _, want := range []string{"protocol.MethodRetrievalStatus", "protocol.MethodRetrievalReindex", "followWorker", "c.Call("} {
		if !strings.Contains(verb, want) {
			t.Errorf(`cmd/aeon/index.go no longer holds %q — the CLI verbs must be CLIENTS of the socket (P34)`, want)
		}
	}
	for _, banned := range []string{"retrieval.New(", "store.Open(", "internal/retrieval", "go func"} {
		if strings.Contains(verb, banned) {
			t.Errorf("cmd/aeon/index.go contains %q — a second implementation in the CLI is exactly what D15 forbids", banned)
		}
	}
}

// dispatched is the one place a test asks the socket for a reindex handle —
// and pins the answer's TYPE, not just its presence (the handle contract).
func dispatched(t *testing.T, d *Daemon, ctx context.Context, connID string, p protocol.RetrievalReindexParams) *protocol.RetrievalReindexAccepted {
	t.Helper()
	res, err := d.Table.Dispatch(ctx, connID, protocol.MethodRetrievalReindex, mustJSONT8(t, p))
	if err != nil {
		t.Fatalf("%s %+v: %v", protocol.MethodRetrievalReindex, p, err)
	}
	acc, ok := res.(*protocol.RetrievalReindexAccepted)
	if !ok {
		t.Fatalf("%s %+v answered %T — want *protocol.RetrievalReindexAccepted", protocol.MethodRetrievalReindex, p, res)
	}
	return acc
}

func mustReadFileT8(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

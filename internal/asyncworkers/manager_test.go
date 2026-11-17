package asyncworkers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// S64 T2 — the manager's own tests. The runner goroutines are gated on
// channels so every assertion runs against a state the test put there,
// never a race with a fast worker.

func openManager(t *testing.T, opts Options) (*Manager, *store.Store) {
	t.Helper()
	st, err := store.Open(store.Options{
		Dir: t.TempDir(), Env: func(string) string { return "" }, Clock: func() time.Time { return time.Now() },
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	m := New(st, opts)
	return m, st
}

type collector struct {
	mu     sync.Mutex
	events []event
}

type event struct {
	kind          string
	componentRoot string
	view          View
}

func (c *collector) publish(kind, componentRoot string, payload any) {
	v, ok := payload.(*View)
	if !ok || v == nil {
		return
	}
	c.mu.Lock()
	c.events = append(c.events, event{kind: kind, componentRoot: componentRoot, view: *v})
	c.mu.Unlock()
}

func (c *collector) kinds() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.events))
	for _, e := range c.events {
		out = append(out, e.kind)
	}
	return out
}

func (c *collector) count(kind string) int {
	n := 0
	for _, k := range c.kinds() {
		if k == kind {
			n++
		}
	}
	return n
}

func waitForState(t *testing.T, m *Manager, workerID string, states ...string) *store.AsyncWorkerRow {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		row, err := m.Get(workerID)
		if err == nil {
			for _, s := range states {
				if row.State == s {
					return row
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	row, err := m.Get(workerID)
	t.Fatalf("worker %s never reached %v (last %+v, err %v)", workerID, states, row, err)
	return nil
}

func TestLifecycleQueuedRunningFinished(t *testing.T) {
	m, _ := openManager(t, Options{})
	col := &collector{}
	m.pub = col.publish

	release := make(chan struct{})
	var calls int32
	err := m.Register("slow.build", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		rep.Progress(0.5, "half")
		return json.RawMessage(`{"files":2}`), nil
	}, WorkerOpts{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	row, started, err := m.Start("slow.build", "/root/a", "/root/a", nil)
	if err != nil || !started {
		t.Fatalf("start: %+v started=%v err=%v", row, started, err)
	}
	if row.State != "queued" && row.State != "running" {
		t.Fatalf("start returned state %q, want queued or running", row.State)
	}
	close(release)
	done := waitForState(t, m, row.WorkerID, "finished")
	if done.Result != `{"files":2}` {
		t.Errorf("result column %q, want the runner's JSON", done.Result)
	}
	if done.FinishedAt == "" || done.StartedAt == "" {
		t.Errorf("timings missing: %+v", done)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("runner called %d times, want exactly 1", calls)
	}
	kinds := col.kinds()
	if len(kinds) < 2 || kinds[0] != KindStarted || kinds[len(kinds)-1] != KindFinished {
		t.Errorf("event stream %v, want started first and finished last", kinds)
	}
	if col.count(KindProgress) != 1 {
		t.Errorf("progress events %d, want 1", col.count(KindProgress))
	}
}

func TestSingleFlightSameKeyStartsOneRunner(t *testing.T) {
	m, _ := openManager(t, Options{})
	gate := make(chan struct{})
	var calls int32
	if err := m.Register("graph.build", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		atomic.AddInt32(&calls, 1)
		<-gate
		return nil, nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}

	first, started, err := m.Start("graph.build", "/p", "/p", nil)
	if err != nil || !started {
		t.Fatalf("first start: %v %v", started, err)
	}
	second, started2, err := m.Start("graph.build", "/p", "/p", map[string]string{"note": "me too"})
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if started2 {
		t.Error("the second Start started a second worker — the flight index failed to hold")
	}
	if second.WorkerID != first.WorkerID {
		t.Errorf("second Start returned a different worker (%s vs %s)", second.WorkerID, first.WorkerID)
	}
	close(gate)
	waitForState(t, m, first.WorkerID, "finished")
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("runner ran %d times, want 1", n)
	}
}

func TestUnregisteredTypeRefusalNamesItselfAndTheSet(t *testing.T) {
	m, _ := openManager(t, Options{})
	if err := m.Register("a.type", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) { return nil, nil }, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	_, _, err := m.Start("b.type", "", "k", nil)
	if err == nil {
		t.Fatal("unregistered type was accepted")
	}
	if !strings.Contains(err.Error(), `"b.type"`) || !strings.Contains(err.Error(), "a.type") {
		t.Errorf("refusal %q must name the refused type AND the registered set", err)
	}
}

func TestStartRefusesEmptyDedupeKey(t *testing.T) {
	m, _ := openManager(t, Options{})
	if err := m.Register("t", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) { return nil, nil }, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Start("t", "", "", nil); err == nil {
		t.Fatal("Start without a dedupeKey was accepted")
	}
}

func TestCancelQueuedNeverRuns(t *testing.T) {
	m, _ := openManager(t, Options{MaxConcurrent: 1})
	blocker := make(chan struct{})
	var blockerCalls, lateCalls int32
	if err := m.Register("slow", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		atomic.AddInt32(&blockerCalls, 1)
		<-blocker
		return nil, nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register("late", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		atomic.AddInt32(&lateCalls, 1)
		return nil, nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}

	b, _, _ := m.Start("slow", "", "blocker", nil)
	waitForState(t, m, b.WorkerID, "running")

	queued, _, err := m.Start("late", "", "queued-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if queued.State != "queued" {
		t.Fatalf("second worker state %q with the slot held, want queued", queued.State)
	}
	if err := m.Cancel(queued.WorkerID); err != nil {
		t.Fatalf("cancel queued: %v", err)
	}
	row := waitForState(t, m, queued.WorkerID, "cancelled")
	if row.Error != "cancelled while queued" {
		t.Errorf("cancelled row reads error %q", row.Error)
	}

	close(blocker)
	waitForState(t, m, b.WorkerID, "finished")
	if n := atomic.LoadInt32(&lateCalls); n != 0 {
		t.Errorf("the cancelled queued worker ran %d times, want 0", n)
	}
	// The cancelled row keeps the ledger honest but must not block a retry.
	again, started, err := m.Start("late", "", "queued-key", nil)
	if err != nil || !started {
		t.Fatalf("retry after cancel: started=%v err=%v", started, err)
	}
	waitForState(t, m, again.WorkerID, "finished")
}

func TestCancelRunningCooperatively(t *testing.T) {
	m, _ := openManager(t, Options{})
	entered := make(chan struct{})
	if err := m.Register("waiter", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	w, _, _ := m.Start("waiter", "", "k", nil)
	<-entered
	waitForState(t, m, w.WorkerID, "running")
	if err := m.Cancel(w.WorkerID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	row := waitForState(t, m, w.WorkerID, "cancelled")
	if !row.CancelRequested {
		t.Error("cancelled row lost cancel_requested")
	}
}

func TestCancelTerminalRefusesNamingTheState(t *testing.T) {
	m, _ := openManager(t, Options{})
	if err := m.Register("fast", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	w, _, _ := m.Start("fast", "", "k", nil)
	waitForState(t, m, w.WorkerID, "finished")
	err := m.Cancel(w.WorkerID)
	if err == nil || !strings.Contains(err.Error(), "finished") {
		t.Fatalf("cancel of a finished worker: %v, want refusal naming finished", err)
	}
	if err := m.Cancel("aw_missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cancel of unknown id: %v, want sql.ErrNoRows", err)
	}
}

func TestPanicIsContainedAndRecorded(t *testing.T) {
	m, _ := openManager(t, Options{})
	if err := m.Register("crash", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		panic("boom")
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	w, _, _ := m.Start("crash", "", "k", nil)
	row := waitForState(t, m, w.WorkerID, "failed")
	if !strings.Contains(row.Error, "panic: boom") {
		t.Errorf("failed row reads %q, want the panic text", row.Error)
	}
	// The manager still works after a crash: another worker starts fine.
	if err := m.Register("ok", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		return json.RawMessage(`{"fine":true}`), nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	w2, _, _ := m.Start("ok", "", "k2", nil)
	waitForState(t, m, w2.WorkerID, "finished")
}

func TestTimeoutBoundsAStuckRunner(t *testing.T) {
	m, _ := openManager(t, Options{})
	if err := m.Register("stuck", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, nil
	}, WorkerOpts{Timeout: 50 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	w, _, _ := m.Start("stuck", "", "k", nil)
	row := waitForState(t, m, w.WorkerID, "failed")
	if !strings.Contains(row.Error, "timed out") {
		t.Errorf("stuck runner failed with %q, want a spoken timeout", row.Error)
	}
}

func TestProgressIsThrottledAndClamped(t *testing.T) {
	m, _ := openManager(t, Options{})
	col := &collector{}
	m.pub = col.publish
	frozen := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return frozen } // one instant: the throttle admits exactly one write
	if err := m.Register("chatty", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		for i := 1; i <= 50; i++ {
			rep.Progress(float64(i)/50.0, fmt.Sprintf("step %d", i))
		}
		return nil, nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	w, _, _ := m.Start("chatty", "", "k", nil)
	row := waitForState(t, m, w.WorkerID, "finished")
	if col.count(KindProgress) != 1 {
		t.Errorf("progress events %d with a frozen clock, want exactly 1", col.count(KindProgress))
	}
	if row.Progress == nil || *row.Progress != 1.0/50.0 {
		t.Errorf("progress %v, want the first admitted write 0.02 (the other 49 were throttled)", row.Progress)
	}

	// Clamping is the Reporter's own job: an out-of-range report lands
	// inside [0,1] rather than meeting the store's refusal.
	if err := m.Register("clamp", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		rep.Progress(1.5, "over")
		return nil, nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	w2, _, _ := m.Start("clamp", "", "k2", nil)
	row2 := waitForState(t, m, w2.WorkerID, "finished")
	if row2.Progress == nil || *row2.Progress != 1 {
		t.Errorf("clamped progress %v, want 1", row2.Progress)
	}
}

func TestMaxConcurrentHoldsAndQueueIsFIFO(t *testing.T) {
	m, _ := openManager(t, Options{MaxConcurrent: 1})
	var busy, peak int32
	order := make(chan string, 3)
	if err := m.Register("work", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		n := atomic.AddInt32(&busy, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		order <- job.Metadata["n"]
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&busy, -1)
		return nil, nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, n := range []string{"1", "2", "3"} {
		w, _, err := m.Start("work", "", "k-"+n, map[string]string{"n": n})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, w.WorkerID)
	}
	for _, id := range ids {
		waitForState(t, m, id, "finished")
	}
	close(order)
	var seq []string
	for o := range order {
		seq = append(seq, o)
	}
	if len(seq) != 3 || seq[0] != "1" {
		t.Errorf("launch order %v, want the queue FIFO from 1", seq)
	}
	if p := atomic.LoadInt32(&peak); p != 1 {
		t.Errorf("peak concurrency %d with MaxConcurrent 1, want 1", p)
	}
}

func TestRecoverInterruptsPreviousDaemonsRows(t *testing.T) {
	m, st := openManager(t, Options{})
	gate := make(chan struct{})
	if err := m.Register("forever", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		<-gate
		return nil, nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	w, _, _ := m.Start("forever", "/root/x", "k", nil)
	waitForState(t, m, w.WorkerID, "running")

	// A NEW manager over the same store is the restarted daemon: the same
	// registrations (registration is code, not state), empty memory.
	col := &collector{}
	m2, _ := newOver(t, st, col)
	if err := m2.Register("forever", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	n, err := m2.Recover(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("recover: n=%d err=%v, want 1", n, err)
	}
	row, _ := m2.Get(w.WorkerID)
	if row.State != "interrupted" || row.FinishedAt == "" || row.Error == "" {
		t.Errorf("recovered row %+v, want interrupted with finished_at and a spoken reason", row)
	}
	if col.count(KindInterrupted) != 1 {
		t.Errorf("interrupted events %d, want 1", col.count(KindInterrupted))
	}
	close(gate) // the old goroutine returns; its finish write is refused (guarded)

	// Interrupted is durable: a fresh start of the same key takes the flight.
	again, started, err := m2.Start("forever", "/root/x", "k", nil)
	if err != nil || !started {
		t.Fatalf("start after recover: %v %v", started, err)
	}
	if again.WorkerID == w.WorkerID {
		t.Error("the retry reused the interrupted row's id")
	}
	waitForState(t, m2, again.WorkerID, "finished")
}

func TestShutdownStopsLiveWorkers(t *testing.T) {
	m, _ := openManager(t, Options{})
	entered := make(chan struct{})
	if err := m.Register("waiter", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	w, _, _ := m.Start("waiter", "", "k", nil)
	<-entered
	waitForState(t, m, w.WorkerID, "running")

	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	row := waitForState(t, m, w.WorkerID, "cancelled")
	if row.State != "cancelled" {
		t.Errorf("row after shutdown %+v", row)
	}
	if _, _, err := m.Start("waiter", "", "another", nil); err == nil {
		t.Error("Start after Shutdown was accepted — the daemon is shutting down")
	}
}

func TestShutdownSweepsStubbornRunner(t *testing.T) {
	saved := shutdownGrace
	shutdownGrace = 100 * time.Millisecond
	defer func() { shutdownGrace = saved }()

	m, _ := openManager(t, Options{})
	stubborn := make(chan struct{})
	if err := m.Register("stubborn", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		<-stubborn // ignores ctx entirely — the case the sweep exists for
		return nil, nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	w, _, _ := m.Start("stubborn", "", "k", nil)
	waitForState(t, m, w.WorkerID, "running")
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	row, _ := m.Get(w.WorkerID)
	if row.State != "interrupted" {
		t.Errorf("stubborn row %q after the grace window, want interrupted", row.State)
	}
	close(stubborn)
}

func TestMetadataReachesTheRunnerAndBack(t *testing.T) {
	m, _ := openManager(t, Options{})
	if err := m.Register("echo", func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) {
		return json.Marshal(job)
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	w, _, _ := m.Start("echo", "/root/m", "k", map[string]string{"path": "a.go", "componentId": "c9"})
	row := waitForState(t, m, w.WorkerID, "finished")
	var job Job
	if err := json.Unmarshal([]byte(row.Result), &job); err != nil {
		t.Fatalf("result is not a Job: %v", err)
	}
	if job.Metadata["path"] != "a.go" || job.Metadata["componentId"] != "c9" || job.ComponentRoot != "/root/m" {
		t.Errorf("runner saw %+v", job)
	}
	v := ViewOf(row)
	if v.Metadata["path"] != "a.go" {
		t.Errorf("View lost metadata: %+v", v)
	}
}

func TestRegisterRefusesDuplicatesAndNils(t *testing.T) {
	m, _ := openManager(t, Options{})
	r := func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error) { return nil, nil }
	if err := m.Register("t", r, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register("t", r, WorkerOpts{}); err == nil {
		t.Error("duplicate registration accepted")
	}
	if err := m.Register("", r, WorkerOpts{}); err == nil {
		t.Error("empty type accepted")
	}
	if err := m.Register("u", nil, WorkerOpts{}); err == nil {
		t.Error("nil runner accepted")
	}
	if got := m.Types(); len(got) != 1 || got[0] != "t" {
		t.Errorf("Types() = %v, want [t]", got)
	}
}

// newOver builds a second manager over an already-open store — the
// restarted-daemon shape Recover is argued about.
func newOver(t *testing.T, st *store.Store, col *collector) (*Manager, *store.Store) {
	t.Helper()
	m := New(st, Options{Publish: col.publish})
	return m, st
}

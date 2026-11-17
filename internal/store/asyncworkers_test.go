package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// S64 T1 — the async-worker ledger's own tests. The invariants worth
// pinning are the two the manager will RELY on rather than re-check: the
// flight index makes single flight durable, and the terminal guard makes
// "last writer wins" impossible.

func insertWorker(t *testing.T, s *Store, typ, root, key string) *AsyncWorkerRow {
	t.Helper()
	w, err := s.InsertAsyncWorker(NewAsyncWorker{
		WorkerType: typ, ComponentRoot: root, DedupeKey: key, QueuedAt: s.Now(),
	})
	if err != nil {
		t.Fatalf("insert %s/%s: %v", typ, key, err)
	}
	return w
}

func TestInsertAndAsyncGetRoundTrip(t *testing.T) {
	s := openTemp(t)
	w := insertWorker(t, s, "graph.build", "/home/me/proj", "/home/me/proj")
	if w.State != "queued" || w.WorkerID == "" || !strings.HasPrefix(w.WorkerID, "aw_") {
		t.Fatalf("insert returned %+v, want queued with a minted aw_ id", w)
	}
	got, err := s.GetAsyncWorker(w.WorkerID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.WorkerType != "graph.build" || got.ComponentRoot != "/home/me/proj" || got.QueuedAt != w.QueuedAt {
		t.Errorf("round trip lost fields: %+v", got)
	}
	if got.Metadata != "{}" {
		t.Errorf("empty metadata stored as %q, want the honest empty object {}", got.Metadata)
	}
	if got.Progress != nil || got.StartedAt != "" || got.FinishedAt != "" {
		t.Errorf("a queued row reports started/finished/progress: %+v", got)
	}
}

func TestAsyncGetUnknownNamesNothingFound(t *testing.T) {
	s := openTemp(t)
	_, err := s.GetAsyncWorker("aw_never-existed")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("get of an unknown id returned %v, want sql.ErrNoRows", err)
	}
}

func TestAsyncInsertRefusesMissingFields(t *testing.T) {
	s := openTemp(t)
	for _, bad := range []NewAsyncWorker{
		{WorkerType: "", DedupeKey: "k", QueuedAt: s.Now()},
		{WorkerType: "t", DedupeKey: "", QueuedAt: s.Now()},
		{WorkerType: "t", DedupeKey: "k", QueuedAt: ""},
	} {
		if _, err := s.InsertAsyncWorker(bad); err == nil {
			t.Errorf("insert %+v was accepted, want refusal", bad)
		}
	}
}

func TestFlightIndexBlocksSecondLiveWorkerSameKey(t *testing.T) {
	s := openTemp(t)
	first := insertWorker(t, s, "graph.build", "/root/a", "/root/a")
	_, err := s.InsertAsyncWorker(NewAsyncWorker{WorkerType: "graph.build", DedupeKey: "/root/a", QueuedAt: s.Now()})
	if !errors.Is(err, ErrAsyncWorkerInFlight) {
		t.Fatalf("second queued insert for the in-flight key returned %v, want ErrAsyncWorkerInFlight", err)
	}

	// A different dedupe key is a different worker: same type, both live.
	other := insertWorker(t, s, "graph.build", "/root/b", "/root/b")
	if other.WorkerID == first.WorkerID {
		t.Fatal("two distinct keys minted the same worker_id")
	}

	// The key frees only when the live row goes terminal.
	if ok, err := s.StartAsyncWorker(first.WorkerID, s.Now()); err != nil || !ok {
		t.Fatalf("start: ok=%v err=%v", ok, err)
	}
	if ok, err := s.FinishAsyncWorker(first.WorkerID, "finished", "{}", "", s.Now()); err != nil || !ok {
		t.Fatalf("finish: ok=%v err=%v", ok, err)
	}
	again := insertWorker(t, s, "graph.build", "/root/a", "/root/a")
	if again.WorkerID == first.WorkerID {
		t.Fatal("the retry re-used the finished row's id")
	}
}

func TestAsyncStateCheckClosesTheVocabulary(t *testing.T) {
	s := openTemp(t)
	w := insertWorker(t, s, "t", "", "k")
	if _, err := s.w.Exec(`UPDATE async_worker SET state = 'almost-done' WHERE worker_id = ?`, w.WorkerID); err == nil {
		t.Fatal("the state CHECK accepted 'almost-done' — the vocabulary is not closed at the database")
	} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("state CHECK refusal was %v, want a CHECK refusal", err)
	}
}

func TestStartGuardMakesDoubleStartVisible(t *testing.T) {
	s := openTemp(t)
	w := insertWorker(t, s, "t", "", "k")
	ok, err := s.StartAsyncWorker(w.WorkerID, s.Now())
	if err != nil || !ok {
		t.Fatalf("first start: ok=%v err=%v", ok, err)
	}
	ok, err = s.StartAsyncWorker(w.WorkerID, s.Now())
	if err != nil || ok {
		t.Fatalf("second start reported ok=%v err=%v, want false/nil — the guard must make it visible", ok, err)
	}
	got, _ := s.GetAsyncWorker(w.WorkerID)
	if got.StartedAt != s.Now() || got.State != "running" {
		t.Errorf("running row reads started %q state %q", got.StartedAt, got.State)
	}
}

func TestProgressOnlyOnRunningAndWithinBounds(t *testing.T) {
	s := openTemp(t)
	w := insertWorker(t, s, "t", "", "k")
	if ok, _ := s.ProgressAsyncWorker(w.WorkerID, 0.5, "halfway"); ok {
		t.Error("progress wrote to a queued row — only running workers tick")
	}
	s.StartAsyncWorker(w.WorkerID, s.Now())
	if ok, err := s.ProgressAsyncWorker(w.WorkerID, 1.5, "over"); err == nil {
		t.Error("progress accepted 1.5 outside 0..1")
	} else if ok {
		t.Error("out-of-range progress reported a write")
	}
	if ok, err := s.ProgressAsyncWorker(w.WorkerID, 0.5, strings.Repeat("n", 900)); err != nil || !ok {
		t.Fatalf("progress write: ok=%v err=%v", ok, err)
	}
	got, _ := s.GetAsyncWorker(w.WorkerID)
	if got.Progress == nil || *got.Progress != 0.5 {
		t.Errorf("progress reads %v, want 0.5", got.Progress)
	}
	if len(got.ProgressNote) > 600 {
		t.Errorf("the note was not clamped: %d bytes", len(got.ProgressNote))
	}
	// After terminal, a late tick cannot land.
	s.FinishAsyncWorker(w.WorkerID, "finished", "", "", s.Now())
	if ok, _ := s.ProgressAsyncWorker(w.WorkerID, 0.9, "late"); ok {
		t.Error("a late progress tick wrote a finished row")
	}
}

func TestFinishIsGuardedTerminalIsTerminal(t *testing.T) {
	s := openTemp(t)
	w := insertWorker(t, s, "t", "", "k")
	s.StartAsyncWorker(w.WorkerID, s.Now())
	if _, err := s.FinishAsyncWorker(w.WorkerID, "half-finished", "", "", s.Now()); err == nil {
		t.Fatal("FinishAsyncWorker accepted a non-terminal state")
	}
	ok, err := s.FinishAsyncWorker(w.WorkerID, "finished", `{"files":3}`, "", s.Now())
	if err != nil || !ok {
		t.Fatalf("finish: ok=%v err=%v", ok, err)
	}
	// The late goroutine: the interrupted pass got there first below — here
	// the finished row must refuse the second write.
	ok, err = s.FinishAsyncWorker(w.WorkerID, "failed", "", "late goroutine", s.Now())
	if err != nil || ok {
		t.Fatalf("second finish: ok=%v err=%v, want false/nil", ok, err)
	}
	got, _ := s.GetAsyncWorker(w.WorkerID)
	if got.State != "finished" || got.Result != `{"files":3}` || got.Error != "" || got.FinishedAt != s.Now() {
		t.Errorf("terminal row reads %+v", got)
	}
}

func TestRequestCancelReportsTheStateItAskedIn(t *testing.T) {
	s := openTemp(t)
	q := insertWorker(t, s, "t", "", "queued-key")
	if st, err := s.RequestCancelAsyncWorker(q.WorkerID); err != nil || st != "queued" {
		t.Fatalf("cancel of queued: state=%q err=%v, want queued/nil", st, err)
	}
	if got, _ := s.GetAsyncWorker(q.WorkerID); !got.CancelRequested {
		t.Error("the queued row does not carry cancel_requested")
	}

	r := insertWorker(t, s, "t", "", "running-key")
	s.StartAsyncWorker(r.WorkerID, s.Now())
	if st, err := s.RequestCancelAsyncWorker(r.WorkerID); err != nil || st != "running" {
		t.Fatalf("cancel of running: state=%q err=%v, want running/nil", st, err)
	}

	s.FinishAsyncWorker(q.WorkerID, "cancelled", "", "", s.Now())
	if st, err := s.RequestCancelAsyncWorker(q.WorkerID); err != nil || st != "" {
		t.Fatalf("cancel of terminal: state=%q err=%v, want empty/nil — the state is the answer", st, err)
	}

	if _, err := s.RequestCancelAsyncWorker("aw_nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cancel of unknown: %v, want sql.ErrNoRows", err)
	}
}

func TestMarkInterruptedOnlyTouchesLiveRows(t *testing.T) {
	s := openTemp(t)
	queued := insertWorker(t, s, "t", "", "q")
	running := insertWorker(t, s, "t", "", "r")
	s.StartAsyncWorker(running.WorkerID, s.Now())
	done := insertWorker(t, s, "t", "", "d")
	s.StartAsyncWorker(done.WorkerID, s.Now())
	s.FinishAsyncWorker(done.WorkerID, "finished", "", "", s.Now())

	n, err := s.MarkAsyncWorkersInterrupted(s.Now())
	if err != nil || n != 2 {
		t.Fatalf("interrupt flipped %d rows err=%v, want 2/nil", n, err)
	}
	for _, id := range []string{queued.WorkerID, running.WorkerID} {
		got, _ := s.GetAsyncWorker(id)
		if got.State != "interrupted" || got.FinishedAt != s.Now() || got.Error == "" {
			t.Errorf("live row %s reads %+v, want interrupted with a spoken reason", id, got)
		}
	}
	got, _ := s.GetAsyncWorker(done.WorkerID)
	if got.State != "finished" {
		t.Errorf("the finished row was swept to %q — recovery may only speak for live rows", got.State)
	}
	// Interrupted is terminal: no second write.
	if ok, _ := s.FinishAsyncWorker(queued.WorkerID, "failed", "", "too late", s.Now()); ok {
		t.Error("a late finish overwrote interrupted")
	}
}

func TestListFiltersByTypeStateRootAndPages(t *testing.T) {
	s := openTemp(t)
	a := insertWorker(t, s, "graph.build", "/a", "a")
	insertWorker(t, s, "graph.build", "/b", "b")
	c := insertWorker(t, s, "other.type", "/a", "c")
	s.StartAsyncWorker(a.WorkerID, s.Now())
	s.StartAsyncWorker(c.WorkerID, s.Now())
	s.FinishAsyncWorker(c.WorkerID, "failed", "", "boom", s.Now())

	byType, err := s.ListAsyncWorkers(AsyncWorkerFilter{Type: "graph.build"})
	if err != nil || len(byType) != 2 {
		t.Fatalf("type filter: %d rows err=%v, want 2", len(byType), err)
	}
	byRoot, _ := s.ListAsyncWorkers(AsyncWorkerFilter{ComponentRoot: "/b"})
	if len(byRoot) != 1 || byRoot[0].DedupeKey != "b" {
		t.Fatalf("root filter: %+v, want only /b", byRoot)
	}
	live, _ := s.ListAsyncWorkers(AsyncWorkerFilter{States: []string{"running"}})
	if len(live) != 1 || live[0].WorkerID != a.WorkerID {
		t.Fatalf("state filter: %+v, want only the running graph.build", live)
	}
	// Newest first, and Limit bounds the read.
	all, _ := s.ListAsyncWorkers(AsyncWorkerFilter{Limit: 2})
	if len(all) != 2 || all[0].DedupeKey != "c" {
		t.Fatalf("limit/order: %+v, want the 2 newest with c first", all)
	}
	def, _ := s.ListAsyncWorkers(AsyncWorkerFilter{})
	if len(def) != 3 {
		t.Fatalf("no filter: %d rows, want all 3 (the default limit does not clip them)", len(def))
	}
}

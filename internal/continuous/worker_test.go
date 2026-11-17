package continuous

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// The fakes: one snapshot, one ledger. The Worker's contract is ORDER —
// record-before-start, Plan's order kept — and these tests are the order
// made executable.

type fakeReader struct {
	mu      sync.Mutex
	snap    Snapshot
	err     error
	seenAt  []time.Time
	blockOn chan struct{} // when set, Snapshot waits until it closes
}

func (f *fakeReader) seen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seenAt)
}

func (f *fakeReader) Snapshot(ctx context.Context, at time.Time) (Snapshot, error) {
	if f.blockOn != nil {
		<-f.blockOn
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenAt = append(f.seenAt, at)
	return f.snap, f.err
}

func (f *fakeReader) snapshotAt(t *testing.T) time.Time {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seenAt) == 0 {
		t.Fatal("the reader was never asked")
	}
	return f.seenAt[0]
}

type fakeStarter struct {
	mu       sync.Mutex
	events   []string // "rec <subject>" / "start <subject>"
	recErr   map[string]error
	startErr map[string]error
	lastID   int64
}

func (f *fakeStarter) Record(ctx context.Context, s Snapshot, d Decision) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := d.Subject.ID
	if err := f.recErr[key]; err != nil {
		return 0, err
	}
	f.lastID++
	f.events = append(f.events, "rec "+key)
	return f.lastID, nil
}

func (f *fakeStarter) Start(ctx context.Context, d Decision, s Snapshot, decisionID int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if decisionID <= 0 {
		panic("Start without a recorded decision id — the claim could not name its decision")
	}
	f.events = append(f.events, "start "+d.Subject.ID)
	if err := f.startErr[d.Subject.ID]; err != nil {
		return "", err
	}
	return "loop-" + d.Subject.ID, nil
}

func (s *fakeStarter) seq() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.events...)
}

type fakeClock struct{ at time.Time }

func (c fakeClock) Now() time.Time { return c.at }

func workerSnap() Snapshot {
	s := Snapshot{
		Now:      time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC),
		Master:   Master{Enabled: true},
		Settings: Settings{MaxSlots: 4, MaxStartsPerRootPerHour: 6, AgingHours: 24},
	}
	mk := func(root string, headID int64) Repo {
		return Repo{Root: root, Enabled: true, Template: "t", TemplateOK: true,
			Weight: 1, BudgetOK: true, QueueHead: &QueueUnit{ID: headID, Status: "ready"}}
	}
	s.Repos = []Repo{mk("/srv/alpha", 7), mk("/srv/beta", 8)}
	return s
}

func TestTickRecordsThenStartsInPlansOrder(t *testing.T) {
	r := &fakeReader{snap: workerSnap()}
	st := &fakeStarter{}
	w := NewWorker(r, st, fakeClock{}, nil, nil)
	if err := w.Tick(context.Background(), workerSnap().Now); err != nil {
		t.Fatal(err)
	}
	got := st.seq()
	want := []string{"rec 7", "start 7", "rec 8", "start 8"} // Plan's order: alpha, beta
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

func TestUnrecordableDecisionNeverStarts(t *testing.T) {
	r := &fakeReader{snap: workerSnap()}
	st := &fakeStarter{recErr: map[string]error{"7": errors.New("store is busy")}}
	w := NewWorker(r, st, fakeClock{}, nil, nil)
	if err := w.Tick(context.Background(), r.snap.Now); err != nil {
		t.Fatal(err)
	}
	got := st.seq()
	for _, e := range got {
		if e == "start 7" {
			t.Fatalf("a decision that could not be recorded still started: %v", got)
		}
	}
	if len(got) != 2 || got[0] != "rec 8" || got[1] != "start 8" {
		t.Fatalf("the healthy half of the tick did not proceed: %v", got)
	}
}

func TestNonStartDecisionsAreRecordedButNeverStarted(t *testing.T) {
	s := workerSnap()
	s.Master.Paused = true
	r := &fakeReader{snap: s}
	st := &fakeStarter{}
	w := NewWorker(r, st, fakeClock{}, nil, nil)
	if err := w.Tick(context.Background(), s.Now); err != nil {
		t.Fatal(err)
	}
	got := st.seq()
	want := []string{"rec /srv/alpha", "rec /srv/beta"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("events = %v, want two records and no starts", got)
	}
}

func TestTickNamesTheClocksInstant(t *testing.T) {
	at := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	r := &fakeReader{snap: workerSnap()}
	w := NewWorker(r, &fakeStarter{}, fakeClock{at: at}, nil, nil)
	if err := w.Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if got := r.snapshotAt(t); !got.Equal(at) {
		t.Fatalf("the reader was asked for %s, not the tick's instant %s", got, at)
	}
}

func TestKickIsNonBlockingAndCoalesced(t *testing.T) {
	r := &fakeReader{snap: workerSnap()}
	st := &fakeStarter{}
	w := NewWorker(r, st, fakeClock{}, SettingsReaderFunc(func(context.Context) (Settings, error) {
		return Settings{TickSeconds: 3600}, nil // the ticker will not fire in this test
	}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	for i := 0; i < 200; i++ {
		w.Kick("/srv/alpha") // 200 kicks, one slot's worth of attention
	}
	deadline := time.After(5 * time.Second)
	for {
		if len(st.seq()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no kick ever produced a tick")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run ended with %v, want context.Canceled", err)
	}
}

func TestOneTickAtATime(t *testing.T) {
	release := make(chan struct{})
	r := &fakeReader{snap: workerSnap(), blockOn: release}
	st := &fakeStarter{}
	w := NewWorker(r, st, fakeClock{}, nil, nil)
	first := make(chan struct{})
	go func() { close(first); _ = w.Tick(context.Background(), r.snap.Now) }()
	<-first
	time.Sleep(20 * time.Millisecond) // the first tick is inside Snapshot
	close(release)
	// Structural law: Run consumes the next signal only after Tick
	// returned; a second concurrent Tick is not a thing the loop does.
	time.Sleep(20 * time.Millisecond)
	if n := r.seen(); n > 1 {
		t.Fatalf("%d snapshots raced one worker", n)
	}
}

type SettingsReaderFunc func(context.Context) (Settings, error)

func (f SettingsReaderFunc) Load(ctx context.Context) (Settings, error) { return f(ctx) }

type fakeReconciler struct {
	fakeStarter
	n int
}

func (f *fakeReconciler) Reconcile(context.Context) error {
	f.n++
	return nil
}

func TestReconcileDelegatesToTheStarter(t *testing.T) {
	fr := &fakeReconciler{}
	w := NewWorker(&fakeReader{snap: workerSnap()}, fr, nil, nil, nil)
	if err := w.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fr.n != 1 {
		t.Fatalf("Reconcile ran %d times, want exactly 1 (serve.go runs it once)", fr.n)
	}
	// A starter without the recovery half is not an error — it is a
	// double; production wires one, and the absence is the test's shape.
	if err := NewWorker(&fakeReader{}, &fakeStarter{}, nil, nil, nil).Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
}

package trigger

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakes — no database, no clock, no goroutine. Every instant below is a
// literal; D14 is honoured by construction, not by short timeouts.

type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time { return f.t }

type fakeStore struct {
	scheds  []Schedule
	covered map[string]string
	fires   []Fire
	gaps    []TriggerGapView
}

func (f *fakeStore) SaveTriggerSchedule(_ context.Context, s Schedule) error {
	// Control-plane write (T8): the fake replaces the row IN PLACE so a
	// pause/resume test reads back exactly what was persisted — the
	// recording IS the row.
	for i, have := range f.scheds {
		if have.ID == s.ID {
			f.scheds[i] = s
			if v, ok := f.covered[s.ID]; ok {
				s.CoveredUntil = v // the upsert keeps the frontier (RowStore contract)
				f.scheds[i] = s
			}
			return nil
		}
	}
	f.scheds = append(f.scheds, s)
	return nil
}

func (f *fakeStore) ActiveSchedules(context.Context) ([]Schedule, error) {
	// The frontier is the STORE's state, not the caller's — the fake
	// overlays it exactly as the row does, so "answered once" is a property
	// of the frontier and not of a test forgetting to advance it.
	out := make([]Schedule, 0, len(f.scheds))
	for _, s := range f.scheds {
		if c, ok := f.covered[s.ID]; ok {
			s.CoveredUntil = c
		}
		out = append(out, s)
	}
	return out, nil
}
func (f *fakeStore) SetCovered(_ context.Context, id, until string) error {
	f.covered[id] = until
	return nil
}
func (f *fakeStore) RecordFire(_ context.Context, fire Fire) error {
	f.fires = append(f.fires, fire)
	return nil
}

// gaps is where reconcile's range-and-count rows land — ONE row per outage
// is a testable property because the fake keeps them as rows.
func (f *fakeStore) RecordGap(_ context.Context, id, coveredFrom, coveredTo string, missed int, _ time.Time) error {
	f.gaps = append(f.gaps, TriggerGapView{ScheduleID: id, CoveredFrom: coveredFrom, CoveredTo: coveredTo, Missed: missed})
	return nil
}

type fakeStarter struct {
	got  []Fire
	err  error
	id   string
	call int
}

func (f *fakeStarter) StartFromTrigger(_ context.Context, fire Fire) (string, error) {
	f.call++
	f.got = append(f.got, fire)
	if f.err != nil {
		return "", f.err
	}
	return f.id, nil
}

func sched(id, expr string) Schedule {
	return Schedule{
		ID: id, Kind: KindSchedule, Blueprint: "dev-loop", Workspace: testWS,
		Expr: expr, Zone: "UTC", CreatedAt: "2026-10-27T00:00:00Z",
	}
}

func newSched(store *fakeStore, starter *fakeStarter) *Scheduler {
	return NewScheduler(store, starter, &fakeClock{})
}

func london(t *testing.T, s string) time.Time {
	t.Helper()
	loc, err := LoadZone("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := time.ParseInLocation("2006-01-02 15:04:05", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestTickFiresTheDueInstantOnce(t *testing.T) {
	at := london(t, "2026-10-27 03:30:00")
	s := sched("nightly-survey", "30 3 * * *")
	s.CoveredUntil = at.Add(-10 * time.Minute).UTC().Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-77"}
	sc := newSched(st, fa)
	if err := sc.Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if fa.call != 1 || len(st.fires) != 1 {
		t.Fatalf("want one fire, got %d starts %d records", fa.call, len(st.fires))
	}
	f := st.fires[0]
	if f.Outcome != OutcomeFired || f.LoopID != "loop-77" {
		t.Fatalf("outcome %q loop %q — want fired/loop-77", f.Outcome, f.LoopID)
	}
	if !f.Instant.Equal(at) {
		t.Fatalf("the answer belongs to %s, not the instant %s", f.Instant, at)
	}
	if f.Provenance != "trigger" || f.TriggerID != "nightly-survey" {
		t.Fatalf("provenance %q / %q — a clock fire says who it is", f.Provenance, f.TriggerID)
	}
	if st.covered["nightly-survey"] == "" {
		t.Fatal("the frontier did not advance")
	}
	// The same tick again answers NOTHING: the instant is already answered.
	if err := sc.Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if fa.call != 1 || len(st.fires) != 1 {
		t.Fatalf("a second tick re-fired an answered instant: %d/%d", fa.call, len(st.fires))
	}
}

func TestNotYetDueAnswersNothing(t *testing.T) {
	at := london(t, "2026-10-27 03:29:59")
	s := sched("nightly-survey", "30 3 * * *")
	s.CoveredUntil = at.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-x"}
	if err := newSched(st, fa).Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if fa.call != 0 || len(st.fires) != 0 {
		t.Fatalf("a not-yet-due instant answered %d starts / %d records — silence is the answer", fa.call, len(st.fires))
	}
}

func TestPausedScheduleIsSilent(t *testing.T) {
	at := london(t, "2026-10-27 03:30:00")
	s := sched("nightly-survey", "30 3 * * *")
	s.Paused = true
	s.CoveredUntil = at.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-x"}
	if err := newSched(st, fa).Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if fa.call != 0 || len(st.fires) != 0 {
		t.Fatalf("a paused schedule spoke: %d starts / %d records — pause answers NOTHING (TR12)", fa.call, len(st.fires))
	}
}

func TestFireOutsideWindowSaysWhy(t *testing.T) {
	// 2026-10-31 is a Saturday.
	at := london(t, "2026-10-31 03:30:00")
	s := sched("nightly-survey", "30 3 * * *")
	s.WindowStart, s.WindowEnd, s.WindowDays = "09:00", "17:00", "1,2,3,4,5"
	s.CoveredUntil = at.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-x"}
	if err := newSched(st, fa).Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if fa.call != 0 {
		t.Fatal("a fire outside the window started a loop")
	}
	if len(st.fires) != 1 || st.fires[0].Outcome != OutcomeDidNotFire {
		t.Fatalf("want one recorded did_not_fire, got %+v", st.fires)
	}
	if !strings.Contains(st.fires[0].Reason, "day is outside") {
		t.Fatalf("the record must carry the reason (D7), got %q", st.fires[0].Reason)
	}
	if st.covered["nightly-survey"] == "" {
		t.Fatal("an answered question must advance the frontier")
	}
}

func TestGateOrderWindowThenOverlapThenAccessThenStart(t *testing.T) {
	at := london(t, "2026-10-27 22:00:00") // inside no working-hours window… use a 24h day: pick 10:00 Tuesday
	at = london(t, "2026-10-27 10:00:00")  // Tuesday 2026-10-27
	s := sched("nightly-survey", "0 10 * * *")
	s.WindowStart, s.WindowEnd, s.WindowDays = "09:00", "17:00", "1,2,3,4,5"
	s.CoveredUntil = at.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "loop-9"}
	var order []string
	sc := newSched(st, fa)
	sc.Gate.Overlap = func(Schedule, time.Time) (bool, string) {
		order = append(order, "overlap")
		return false, ""
	}
	sc.Gate.Access = func(string, string) (bool, string) {
		order = append(order, "access")
		return true, ""
	}
	if err := sc.Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "overlap,access" || fa.call != 1 {
		t.Fatalf("gate order wrong or start missing: %v / %d starts", order, fa.call)
	}

	// Overlap blocked: access is NOT consulted and nothing starts — the
	// previous run's state is answered first because starting a second run
	// to then be refused by governance would mis-say what happened.
	st2 := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa2 := &fakeStarter{id: "loop-9"}
	var order2 []string
	sc2 := newSched(st2, fa2)
	sc2.Gate.Overlap = func(Schedule, time.Time) (bool, string) { order2 = append(order2, "overlap"); return true, "loop-5" }
	sc2.Gate.Access = func(string, string) (bool, string) { order2 = append(order2, "access"); return true, "" }
	if err := sc2.Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order2, ",") != "overlap" || fa2.call != 0 || st2.fires[0].Outcome != OutcomeSkipped {
		t.Fatalf("overlap must short-circuit: %v / %d / %+v", order2, fa2.call, st2.fires)
	}
	if !strings.Contains(st2.fires[0].Reason, "loop-5") {
		t.Fatalf("TR5: the skip names the previous run: %q", st2.fires[0].Reason)
	}

	// Access refused: the start is NOT reached, and the record is refused.
	st3 := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa3 := &fakeStarter{id: "loop-9"}
	sc3 := newSched(st3, fa3)
	sc3.Gate.Access = func(string, string) (bool, string) { return false, "trigger_access denies schedule" }
	if err := sc3.Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if fa3.call != 0 || st3.fires[0].Outcome != OutcomeRefused {
		t.Fatalf("a governance refusal must never reach the start: %d / %+v", fa3.call, st3.fires)
	}
}

func TestStartRefusalIsRecordedAndNotRetried(t *testing.T) {
	at := london(t, "2026-10-27 03:30:00")
	s := sched("nightly-survey", "30 3 * * *")
	s.CoveredUntil = at.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{err: &PinRefusal{}}
	sc := newSched(st, fa)
	if err := sc.Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if len(st.fires) != 1 || st.fires[0].Outcome != OutcomeRefused {
		t.Fatalf("a refused start is an ANSWERED question, recorded: %+v", st.fires)
	}
	if !strings.Contains(st.fires[0].Reason, "the start was refused") {
		t.Fatalf("record must carry the start's sentence: %q", st.fires[0].Reason)
	}
	// The same instant is never re-attempted: retrying a refused fire on a
	// timer would restart a blocked loop forever (design's no-retry).
	if err := sc.Tick(context.Background(), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if fa.call != 1 {
		t.Fatalf("a refused fire was retried: %d starts", fa.call)
	}
}

// PinRefusal keeps this test honest — reuse a plain error type instead if
// the loop package's type is not importable here.
type PinRefusal struct{}

func (p *PinRefusal) Error() string { return "invalid-blueprint: the blueprint is not valid" }

func TestStarterCarriesNoHint(t *testing.T) {
	// D1, enforced against the type: a Fire may name WHERE and WHAT, never
	// WHETHER — no field of the struct the starter sees may speak
	// attendedness or outward permission, and no parameter beyond the
	// Fire exists to smuggle one through.
	ft := reflect.TypeOf(Fire{})
	forbidden := []string{"attended", "outward", "allow", "permission", "grant", "policy", "route"}
	for i := 0; i < ft.NumField(); i++ {
		lower := strings.ToLower(ft.Field(i).Name)
		for _, tok := range forbidden {
			if strings.Contains(lower, tok) {
				t.Fatalf("Fire field %q speaks %q — the fire may not hint at attendedness or permission (D1)", ft.Field(i).Name, tok)
			}
		}
	}
	mt := reflect.TypeOf((*Starter)(nil)).Elem().Method(0)
	if mt.Type.NumIn() != 2 || mt.Type.NumOut() != 2 {
		t.Fatalf("Starter must be (ctx, Fire) (string, error): %s", mt.Type)
	}
	if mt.Type.In(1) != reflect.TypeOf(Fire{}) {
		t.Fatalf("the starter's second parameter must be the Fire itself, got %s", mt.Type.In(1))
	}
}

func TestNoTimeNowOutsideTheSystemClock(t *testing.T) {
	// The package-wide pin behind every injectable-clock claim in S20:
	// the SCHEDULER holds exactly one wall-clock read — SystemClock.Now in
	// clock.go. schedule.go is allowed its one write-time probe (a
	// never-fires expression cannot be judged without a reference "now",
	// and that moment is validation's, not the tick's), and every other
	// file — every path a tick can take — must be at zero, because a
	// schedule path reading the wall directly is a path no test can move.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	counts := map[string]int{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fs, e.Name(), nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" && sel.Sel.Name == "Now" {
					counts[e.Name()]++
				}
			}
			return true
		})
	}
	if counts["clock.go"] != 1 {
		t.Fatalf("clock.go holds %d time.Now reads, want exactly 1 (SystemClock.Now)", counts["clock.go"])
	}
	if counts["schedule.go"] > 1 {
		t.Fatalf("schedule.go holds %d time.Now reads, want at most 1 (the write-time never-fires probe)", counts["schedule.go"])
	}
	if counts["schedule.go"] == 1 {
		raw, err := os.ReadFile("schedule.go")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "never fires") {
			t.Fatal("schedule.go's time.Now must be ONLY the write-time never-fires probe")
		}
	}
	for file, n := range counts {
		if file != "clock.go" && file != "schedule.go" && n > 0 {
			t.Errorf("%s reaches the wall directly %d times — no test can move this path", file, n)
		}
	}
}

func TestWiredReportNamesAbsentGates(t *testing.T) {
	sc := newSched(&fakeStore{}, &fakeStarter{})
	rep := sc.WiredReport()
	joined := strings.Join(rep, " | ")
	if !strings.Contains(joined, "overlap") || !strings.Contains(joined, "access") {
		t.Fatalf("the honest pre-T3/T7 state must name BOTH unwired gates: %q", joined)
	}
	sc.Gate.Overlap = func(Schedule, time.Time) (bool, string) { return false, "" }
	if strings.Contains(strings.Join(sc.WiredReport(), " | "), "overlap NOT wired") {
		t.Fatal("a wired gate must not be reported absent")
	}
}

func TestTickEveryIsAFieldNotAConstant(t *testing.T) {
	sc := newSched(&fakeStore{}, &fakeStarter{})
	if sc.TickEvery != 15*time.Second {
		t.Fatalf("default tick: %v", sc.TickEvery)
	}
	sc.TickEvery = time.Nanosecond // the Run tests' lever; if it were a const this file would not compile
}

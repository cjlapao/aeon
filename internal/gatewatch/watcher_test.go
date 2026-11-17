// watcher_test.go — S57 T7's poll, driven row by row over a fake ledger
// with the REAL store's rules and the shared forge.Fake (so the
// aggregate's one-line reasons are the production ones): the state
// machine (green / red / pending / unknown / unreadable), the restart
// from rows, the never-green-on-silence rule, the backoff bound and the
// window, and the startup sweep's repair of the crash window.

package gatewatch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/forge"
)

const t0 = "2026-11-02T12:00:00Z"

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// --- the fakes ---------------------------------------------------------

// fakeClock is the manual wall clock.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

// fakeSettings answers the live settings read with fixed values (or an
// error the watcher must fall back on).
type fakeSettings struct {
	window, interval time.Duration
	err              error
}

func (s *fakeSettings) Window(context.Context) (time.Duration, error)   { return s.window, s.err }
func (s *fakeSettings) Interval(context.Context) (time.Duration, error) { return s.interval, s.err }

// fileCall is one call through the filer's door.
type fileCall struct {
	kind, subject, root string
	facts               Facts
}

// fakeFiler records the calls and dedupes by subject the way the real
// door does: the first filing of a subject files (and names the bug id),
// a repeat files nothing and names the standing bug.
type fakeFiler struct {
	calls    []fileCall
	existing map[string]int64
	nextID   int64
}

func newFakeFiler() *fakeFiler { return &fakeFiler{existing: map[string]int64{}} }

func (f *fakeFiler) FileGateBug(_ context.Context, kind, subject, root string, facts Facts) (int64, bool, error) {
	f.calls = append(f.calls, fileCall{kind: kind, subject: subject, root: root, facts: facts})
	if id, ok := f.existing[subject]; ok {
		return id, false, nil
	}
	f.nextID++
	f.existing[subject] = f.nextID
	return f.nextID, true, nil
}

// fakeEvents records the watch's terminal answers.
type fakeEvents struct {
	kinds []string
	roots []string
	pays  []map[string]any
}

func (e *fakeEvents) Publish(kind, root string, payload any) {
	e.kinds = append(e.kinds, kind)
	e.roots = append(e.roots, root)
	e.pays = append(e.pays, payload.(map[string]any))
}

func (e *fakeEvents) of(kind string) []map[string]any {
	var out []map[string]any
	for i, k := range e.kinds {
		if k == kind {
			out = append(out, e.pays[i])
		}
	}
	return out
}

// fakeWatch is one ledger row plus the schedule and the window's start —
// the store owns all three; the fake keeps them together.
type fakeWatch struct {
	row    Row
	next   time.Time
	merged time.Time
}

// fakeUpdate is one UpdateMergeWatch call the poll made.
type fakeUpdate struct {
	id, state, detail string
	next              time.Time
	attempts          int
}

// fakeStore is the in-memory ledger with the real store's rules: due =
// pending/unknown scheduled at or before now, oldest first; terminal rows
// never come back; expiry turns pending/unknown rows whose window elapsed
// into expired (keeping a non-empty detail); enqueue is idempotent on the
// merge event and a window already elapsed is born expired, never pending.
type fakeStore struct {
	clock     *fakeClock
	watches   map[string]*fakeWatch
	unwatched []MergeEventRef
	updates   []fakeUpdate
	// windowsSeen records the window each expiry pass was given.
	windowsSeen []time.Duration
}

func newFakeStore(c *fakeClock) *fakeStore {
	return &fakeStore{clock: c, watches: map[string]*fakeWatch{}}
}

func (s *fakeStore) add(row Row, next, merged time.Time) {
	s.watches[row.MergeEventID] = &fakeWatch{row: row, next: next, merged: merged}
}

func (s *fakeStore) get(id string) *fakeWatch { return s.watches[id] }

func (s *fakeStore) DueMergeWatches(now time.Time, limit int) ([]Row, error) {
	if limit <= 0 {
		limit = 50
	}
	type due struct {
		next time.Time
		row  Row
	}
	var d []due
	for _, w := range s.watches {
		if (w.row.State == StatePending || w.row.State == StateUnknown) && !w.next.After(now) {
			d = append(d, due{next: w.next, row: w.row})
		}
	}
	sort.Slice(d, func(i, j int) bool {
		if !d[i].next.Equal(d[j].next) {
			return d[i].next.Before(d[j].next)
		}
		return d[i].row.MergeEventID < d[j].row.MergeEventID
	})
	out := make([]Row, 0, len(d))
	for _, v := range d {
		out = append(out, v.row)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *fakeStore) UpdateMergeWatch(id, state, detail string, next time.Time, attempts int) error {
	w, ok := s.watches[id]
	if !ok {
		return fmt.Errorf("no watch for merge event %s", id)
	}
	w.row.State = state
	w.row.Detail = detail
	w.row.Attempts = attempts
	w.next = next
	s.updates = append(s.updates, fakeUpdate{id: id, state: state, detail: detail, next: next, attempts: attempts})
	return nil
}

func (s *fakeStore) ExpireStaleMergeWatches(now time.Time, window time.Duration) (int, error) {
	s.windowsSeen = append(s.windowsSeen, window)
	n := 0
	for _, w := range s.watches {
		if (w.row.State == StatePending || w.row.State == StateUnknown) && !w.merged.Add(window).After(now) {
			w.row.State = StateExpired
			if w.row.Detail == "" {
				w.row.Detail = "window elapsed"
			}
			w.next = now.Add(365 * 24 * time.Hour)
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) MergeEventsWithoutWatch() ([]MergeEventRef, error) {
	out := make([]MergeEventRef, len(s.unwatched))
	copy(out, s.unwatched)
	return out, nil
}

func (s *fakeStore) EnqueueWatchFromEvent(ref MergeEventRef, window time.Duration) error {
	if _, ok := s.watches[ref.ID]; ok {
		// Already watched: the idempotent answer. It is no longer
		// "without a watch" either way.
		s.dropUnwatched(ref.ID)
		return nil
	}
	merged, err := time.Parse(time.RFC3339, ref.MergedAt)
	if err != nil {
		return err
	}
	row := Row{
		MergeEventID:  ref.ID,
		ComponentID:   ref.ComponentID,
		ComponentRoot: ref.ComponentRoot,
		BaseRef:       "main",
		SHA:           ref.MergeSHA,
		State:         StatePending,
		PR:            ref.PR,
	}
	next := s.clock.now
	if !merged.IsZero() && s.clock.now.After(merged.Add(window)) {
		row.State = StateExpired
		row.Detail = "window elapsed before first check"
		next = s.clock.now.Add(365 * 24 * time.Hour)
	}
	s.watches[ref.ID] = &fakeWatch{row: row, next: next, merged: merged}
	s.dropUnwatched(ref.ID)
	return nil
}

func (s *fakeStore) dropUnwatched(id string) {
	for i, r := range s.unwatched {
		if r.ID == id {
			s.unwatched = append(s.unwatched[:i], s.unwatched[i+1:]...)
			return
		}
	}
}

// forgeForRoots answers the per-root forge door: the fake for the named
// roots, nil for the others.
func forgeForRoots(f *forge.Fake, roots ...string) ForgeFor {
	set := map[string]bool{}
	for _, r := range roots {
		set[r] = true
	}
	return func(root string) forge.Forge {
		if set[root] {
			return f
		}
		return nil
	}
}

// noChecksForge implements forge.Forge WITHOUT the CheckReader method —
// the root whose forge exists but cannot read checks.
type noChecksForge struct{}

func (noChecksForge) FindOpenIssue(context.Context, string, string) (*forge.Issue, error) {
	return nil, nil
}
func (noChecksForge) OpenIssue(context.Context, forge.IssueRequest) (*forge.Issue, error) {
	return nil, nil
}
func (noChecksForge) ReadIssue(context.Context, int) (*forge.Issue, error) { return nil, nil }
func (noChecksForge) CloseIssue(context.Context, int) error                { return nil }
func (noChecksForge) FindOpenPR(context.Context, string) (*forge.PR, error) {
	return nil, nil
}
func (noChecksForge) OpenPR(context.Context, forge.PRRequest) (*forge.PR, error) {
	return nil, nil
}
func (noChecksForge) ReadPR(context.Context, int) (*forge.PR, error) { return nil, nil }
func (noChecksForge) MergePR(context.Context, int, forge.MergeMethod) (*forge.MergeResult, error) {
	return nil, nil
}
func (noChecksForge) ListReviews(context.Context, int) ([]forge.Review, error) { return nil, nil }
func (noChecksForge) PostReview(context.Context, int, string, forge.Verdict) (*forge.Review, error) {
	return nil, nil
}

// plainErrorForge reads checks but answers with a plain error — not a
// ForgeError, so the poll cannot name a kind.
type plainErrorForge struct{ noChecksForge }

func (plainErrorForge) ReadChecks(context.Context, string) (forge.ChecksReport, error) {
	return forge.ChecksReport{}, errors.New("a plain error — not a forge fault")
}

// --- Test 1: the state machine -----------------------------------------

// TestWatchGreenRedPendingUnknown — the one mapping rule: success ⇒ green
// (the job is done, no bug), failure ⇒ red (the filer fires, the red
// event names the check), pending/unknown/unreadable ⇒ unknown with a
// backoff and NO event (a check that has not answered is not an answer).
func TestWatchGreenRedPendingUnknown(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: mustTime(t, t0)}
	f := forge.NewFake()
	f.SeedChecks("sha-g", []forge.Check{{Name: "ci/a", State: forge.CheckSuccess}, {Name: "ci/b", State: forge.CheckSuccess}}, true)
	f.SeedChecks("sha-r", []forge.Check{{Name: "ci/build", State: forge.CheckFailure}, {Name: "ci/test", State: forge.CheckSuccess}}, true)
	f.SeedChecks("sha-p", []forge.Check{{Name: "ci/tests", State: forge.CheckPending}}, true)
	f.SeedChecks("sha-e", nil, true)
	f.SeedChecks("sha-u", []forge.Check{{Name: "ci/secret", State: forge.CheckUnknown}}, true)

	s := newFakeStore(clock)
	rev := 3
	revID := "run-7"
	rows := map[string]Row{
		"me-green":   {MergeEventID: "me-green", ComponentID: "c1", ComponentRoot: "/repo", BaseRef: "main", SHA: "sha-g", State: StatePending, LoopSlug: "dev", PR: 41, RuleRevision: &rev, VerifyRunID: &revID},
		"me-red":     {MergeEventID: "me-red", ComponentID: "c1", ComponentRoot: "/repo", BaseRef: "main", SHA: "sha-r", State: StatePending, LoopSlug: "dev", PR: 42},
		"me-pending": {MergeEventID: "me-pending", ComponentID: "c1", ComponentRoot: "/repo", BaseRef: "main", SHA: "sha-p", State: StatePending},
		"me-empty":   {MergeEventID: "me-empty", ComponentID: "c1", ComponentRoot: "/repo", BaseRef: "main", SHA: "sha-e", State: StatePending},
		"me-unk":     {MergeEventID: "me-unk", ComponentID: "c1", ComponentRoot: "/repo", BaseRef: "main", SHA: "sha-u", State: StatePending},
		"me-noforge": {MergeEventID: "me-noforge", ComponentID: "c2", ComponentRoot: "/other", BaseRef: "main", SHA: "sha-x", State: StatePending},
	}
	for _, row := range rows {
		s.add(row, clock.now, clock.now)
	}
	filer := newFakeFiler()
	ev := &fakeEvents{}
	w := &Watcher{
		Store:    s,
		Forge:    forgeForRoots(f, "/repo"),
		Clock:    clock,
		Filer:    filer,
		Events:   ev,
		Settings: &fakeSettings{window: DefaultWatchWindow, interval: DefaultWatchInterval},
	}

	// One pass over every row: the expiry sees nothing stale (all merged
	// at t0), then each due row is checked.
	w.Tick(ctx)

	g := s.get("me-green")
	if g.row.State != StateGreen || g.row.Detail != "all 2 reported checks are success" || g.row.Attempts != 1 {
		t.Errorf("green row: state=%q detail=%q attempts=%d, want green / the aggregate's success line / 1", g.row.State, g.row.Detail, g.row.Attempts)
	}
	if !g.next.Equal(clock.now.Add(365 * 24 * time.Hour)) {
		t.Errorf("green row next=%v, want the far-future schedule (it must never come due)", g.next)
	}
	greens := ev.of(EventGreen)
	if len(greens) != 1 {
		t.Fatalf("green events: %d, want 1", len(greens))
	}
	gp := greens[0]
	if gp["watch"] != "me-green" || gp["sha"] != "sha-g" || gp["pr"] != 41 || gp["bugId"] != int64(0) ||
		gp["reason"] != "all 2 reported checks are success" || gp["componentRoot"] != "/repo" {
		t.Errorf("green event payload = %#v — ids and enums only, no bug (the job is done)", gp)
	}

	r := s.get("me-red")
	if r.row.State != StateRed || r.row.Detail != "check ci/build is failure" || r.row.Attempts != 1 {
		t.Errorf("red row: state=%q detail=%q attempts=%d, want red / the aggregate's failure line / 1", r.row.State, r.row.Detail, r.row.Attempts)
	}
	reds := ev.of(EventRed)
	if len(reds) != 1 {
		t.Fatalf("red events: %d, want 1", len(reds))
	}
	if reds[0]["bugId"] != int64(1) || reds[0]["reason"] != "check ci/build is failure" {
		t.Errorf("red event payload = %#v — the filed bug's id and the check's state", reds[0])
	}
	if len(filer.calls) != 1 {
		t.Fatalf("filer calls: %d, want exactly one (the red) — the green never files", len(filer.calls))
	}
	c := filer.calls[0]
	if c.kind != KindMergeWatch || c.subject != "/repo:merge:sha-r" || c.root != "/repo" {
		t.Errorf("filer call kind=%q subject=%q root=%q, want merge-watch / root:merge:sha / root", c.kind, c.subject, c.root)
	}
	if c.facts.CheckName != "ci/build" || c.facts.PR != 42 || c.facts.LoopSlug != "dev" || c.facts.MergeSHA != "sha-r" || c.facts.RuleRevision != 0 {
		t.Errorf("filer facts = %#v — the failing check's name and the ids the row carried, nothing more", c.facts)
	}

	for id, wantDetail := range map[string]string{
		"me-pending": "check ci/tests is pending",
		"me-empty":   "none-reported",
		"me-unk":     "check ci/secret is unknown",
		"me-noforge": "no-forge",
	} {
		ww := s.get(id)
		if ww.row.State != StateUnknown || ww.row.Detail != wantDetail || ww.row.Attempts != 1 {
			t.Errorf("%s: state=%q detail=%q attempts=%d, want unknown / %q / 1", id, ww.row.State, ww.row.Detail, ww.row.Attempts, wantDetail)
		}
		if !ww.next.Equal(clock.now.Add(backoff(0))) {
			t.Errorf("%s next=%v, want t0+backoff(0)=%s", id, ww.next, backoff(0))
		}
	}
	if n := len(ev.kinds); n != 2 {
		t.Errorf("events published: %d, want 2 (the two terminals) — a pending, an empty report, an unknown check and a missing forge are NOT answers", n)
	}
}

// The unreadable arms: a ForgeError names its kind in the detail (so the
// operator sees a transport fault, not a red gate), a plain error cannot
// name one. Neither is green, neither is red, neither is an event.
func TestWatchUnreadableIsUnknownWithTheErrorKind(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: mustTime(t, t0)}

	cases := []struct {
		name   string
		forge  forge.Forge
		detail string
	}{
		{"transport", unreachableForge(), "unreadable (transport)"},
		{"auth", authErrForge(), "unreadable (auth)"},
		{"not-found", forge.NewFake(), "unreadable (not-found)"}, // sha never seeded: an absence
		{"plain-error", plainErrorForge{}, "unreadable (error)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeStore(clock)
			s.add(Row{MergeEventID: "me-1", ComponentID: "c1", ComponentRoot: "/repo", SHA: "sha-1", State: StatePending}, clock.now, clock.now)
			ev := &fakeEvents{}
			w := &Watcher{Store: s, Forge: func(string) forge.Forge { return tc.forge }, Clock: clock, Events: ev}
			w.Tick(ctx)
			got := s.get("me-1")
			if got.row.State != StateUnknown || got.row.Detail != tc.detail {
				t.Errorf("state=%q detail=%q, want unknown / %q — an unreadable forge is absence, never green or red", got.row.State, got.row.Detail, tc.detail)
			}
			if n := len(ev.kinds); n != 0 {
				t.Errorf("events = %v, want none — no answer, no event", ev.kinds)
			}
		})
	}
}

// --- Test 2: the restart -------------------------------------------------

// TestWatchResumesFromRowsAfterRestart — the watch is cold: the new
// process's first tick answers only from rows. The attempt count is the
// ledger's (the backoff grows from the stored count, not from memory),
// and a row that is not yet due is not touched.
func TestWatchResumesFromRowsAfterRestart(t *testing.T) {
	ctx := context.Background()
	clock := &fakeClock{now: mustTime(t, t0)}
	f := forge.NewFake()
	f.SeedChecks("sha-1", []forge.Check{{Name: "ci/tests", State: forge.CheckPending}}, true)

	s := newFakeStore(clock)
	// w1: checked twice already (attempts 2), due five minutes ago, still
	// no answer. w2: never checked, due ten minutes in the future.
	s.add(Row{MergeEventID: "w1", ComponentID: "c1", ComponentRoot: "/repo", SHA: "sha-1", State: StateUnknown, Attempts: 2, Detail: "check ci/tests is pending"},
		clock.now.Add(-5*time.Minute), clock.now.Add(-1*time.Hour))
	s.add(Row{MergeEventID: "w2", ComponentID: "c1", ComponentRoot: "/repo", SHA: "sha-1", State: StatePending},
		clock.now.Add(10*time.Minute), clock.now)

	w1 := &Watcher{Store: s, Forge: forgeForRoots(f, "/repo"), Clock: clock,
		Settings: &fakeSettings{window: DefaultWatchWindow, interval: DefaultWatchInterval}}
	w1.Tick(ctx)

	if g := s.get("w1"); g.row.Attempts != 3 {
		t.Fatalf("w1 attempts after the first tick = %d, want 3 (the stored 2, plus this poll)", g.row.Attempts)
	}
	if !s.get("w1").next.Equal(clock.now.Add(backoff(2))) {
		t.Errorf("w1 next=%v, want t0+backoff(stored attempts=2)=%s — the growth is the ledger's, not the process's", s.get("w1").next, backoff(2))
	}
	if g := s.get("w2"); g.row.Attempts != 0 || !g.next.Equal(clock.now.Add(10*time.Minute)) {
		t.Errorf("w2 touched: attempts=%d next=%v, want 0 / the original schedule — it is not yet due", g.row.Attempts, g.next)
	}

	// The restart: a NEW watcher over the SAME rows.
	clock.now = clock.now.Add(backoff(2)) // w1's schedule arrives
	w2 := &Watcher{Store: s, Forge: forgeForRoots(f, "/repo"), Clock: clock,
		Settings: &fakeSettings{window: DefaultWatchWindow, interval: DefaultWatchInterval}}
	w2.Tick(ctx)

	if g := s.get("w1"); g.row.Attempts != 4 {
		t.Errorf("w1 attempts after the restarted tick = %d, want 4 — the backoff kept growing across the boundary", g.row.Attempts)
	}
	if !s.get("w1").next.Equal(clock.now.Add(backoff(3))) {
		t.Errorf("w1 next=%v, want now+backoff(3)=%s after the restart's poll", s.get("w1").next, backoff(3))
	}
}

// --- Test 3: silence is not green -----------------------------------------

// TestWatchNeverClaimsGreenOnSilence — the rule the whole slice exists
// for: a check that never answers is NOT green. Zero checks, an unreadable
// forge and a missing forge all park the row as unknown and, when the
// window elapses, as EXPIRED — never red (absence is not a failure), and
// the green event never fires on any of them.
func TestWatchNeverClaimsGreenOnSilence(t *testing.T) {
	ctx := context.Background()
	base := mustTime(t, t0)
	window := DefaultWatchWindow

	cases := []struct {
		name       string
		seed       func(f *forge.Fake)
		forge      func(f *forge.Fake) forge.Forge
		wantDetail string
	}{
		{"zero-checks", func(f *forge.Fake) { f.SeedChecks("sha-1", nil, true) }, nil, "none-reported"},
		{"transport", nil, func(*forge.Fake) forge.Forge { return unreachableForge() }, "unreadable (transport)"},
		{"no-forge", nil, nil, "no-forge"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeClock{now: base}
			f := forge.NewFake()
			if tc.seed != nil {
				tc.seed(f)
			}
			s := newFakeStore(clock)
			s.add(Row{MergeEventID: "w1", ComponentID: "c1", ComponentRoot: "/repo", SHA: "sha-1", State: StatePending}, clock.now, clock.now)
			var ff ForgeFor
			if tc.forge != nil {
				ff = func(string) forge.Forge { return tc.forge(f) }
			} else if tc.seed != nil {
				ff = forgeForRoots(f, "/repo")
			}
			ev := &fakeEvents{}
			w := &Watcher{Store: s, Forge: ff, Clock: clock, Events: ev,
				Settings: &fakeSettings{window: window, interval: DefaultWatchInterval}}

			// First tick: checked, no answer, unknown.
			w.Tick(ctx)
			if g := s.get("w1"); g.row.State != StateUnknown || g.row.Detail != tc.wantDetail {
				t.Fatalf("after the first tick: state=%q detail=%q, want unknown / %q", g.row.State, g.row.Detail, tc.wantDetail)
			}
			// A few more ticks inside the window: still unknown, still
			// not green.
			clock.now = clock.now.Add(2 * DefaultWatchInterval)
			w.Tick(ctx)
			if g := s.get("w1"); g.row.State != StateUnknown {
				t.Fatalf("inside the window: state=%q, want unknown", g.row.State)
			}
			// The window elapses: the tick's expiry pass turns it
			// expired BEFORE the due read could check it — and the detail
			// (the last honest word) is kept, never overwritten.
			clock.now = clock.now.Add(window)
			w.Tick(ctx)
			g := s.get("w1")
			if g.row.State != StateExpired {
				t.Fatalf("after the window: state=%q, want expired — a watch that never got an answer is expired, never red", g.row.State)
			}
			if g.row.Detail != tc.wantDetail {
				t.Errorf("detail=%q, want %q — the last answer the forge gave, kept", g.row.Detail, tc.wantDetail)
			}
			for _, k := range ev.kinds {
				if k == EventGreen || k == EventRed {
					t.Errorf("event %q published on a silent watch — silence is neither green nor red", k)
				}
			}
		})
	}
}

// --- Test 4: the bounds ----------------------------------------------------

// TestWatchWindowAndBackoffAreBounded — the reschedule growth is 30 s
// doubling, capped at 5 min; the window comes from the live setting (a
// bad value falls back to the default, and the watcher keeps polling).
func TestWatchWindowAndBackoffAreBounded(t *testing.T) {
	want := []time.Duration{
		30 * time.Second, // attempts 0
		60 * time.Second, // 1
		2 * time.Minute,  // 2
		4 * time.Minute,  // 3
		5 * time.Minute,  // 4 (480 s caps)
		5 * time.Minute,  // 5 (the cap holds)
		5 * time.Minute,  // 10 (still the cap)
	}
	got := []time.Duration{backoff(0), backoff(1), backoff(2), backoff(3), backoff(4), backoff(5), backoff(10)}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("backoff(%d) = %s, want %s — 30 s doubling, capped at 5 min", []int{0, 1, 2, 3, 4, 5, 10}[i], got[i], want[i])
		}
	}

	t.Run("the live setting's window is the expiry's bound", func(t *testing.T) {
		ctx := context.Background()
		clock := &fakeClock{now: mustTime(t, t0)}
		f := forge.NewFake()
		f.SeedChecks("sha-1", nil, true)
		s := newFakeStore(clock)
		s.add(Row{MergeEventID: "w1", ComponentID: "c1", ComponentRoot: "/repo", SHA: "sha-1", State: StatePending}, clock.now, clock.now)
		w := &Watcher{Store: s, Forge: forgeForRoots(f, "/repo"), Clock: clock,
			Settings: &fakeSettings{window: 90 * time.Minute, interval: DefaultWatchInterval}}

		// At t0+91m the 90-min setting has elapsed but the 2-h default
		// has not: the setting is the bound.
		clock.now = clock.now.Add(91 * time.Minute)
		w.Tick(ctx)
		if g := s.get("w1"); g.row.State != StateExpired {
			t.Fatalf("state=%q, want expired — the 90-min setting elapsed at t0+90m", g.row.State)
		}
		last := s.windowsSeen[len(s.windowsSeen)-1]
		if last != 90*time.Minute {
			t.Errorf("the expiry pass got window=%s, want the setting's 90m (the default would not have expired this row)", last)
		}
	})

	t.Run("a bad setting falls back to the default and keeps polling", func(t *testing.T) {
		ctx := context.Background()
		clock := &fakeClock{now: mustTime(t, t0)}
		f := forge.NewFake()
		f.SeedChecks("sha-1", nil, true)
		s := newFakeStore(clock)
		s.add(Row{MergeEventID: "w1", ComponentID: "c1", ComponentRoot: "/repo", SHA: "sha-1", State: StatePending}, clock.now, clock.now)
		w := &Watcher{Store: s, Forge: forgeForRoots(f, "/repo"), Clock: clock,
			Settings: &fakeSettings{err: errors.New("the setting does not parse")}}

		clock.now = clock.now.Add(91 * time.Minute)
		w.Tick(ctx)
		if g := s.get("w1"); g.row.State != StateUnknown {
			t.Fatalf("state=%q, want unknown — the 2-h default had not elapsed at t0+91m", g.row.State)
		}
		if last := s.windowsSeen[len(s.windowsSeen)-1]; last != DefaultWatchWindow {
			t.Errorf("the expiry pass got window=%s, want the default 2h", last)
		}
		// And it kept polling (the bad setting did not stop the watch).
		if g := s.get("w1"); g.row.Attempts != 1 || g.row.Detail != "none-reported" {
			t.Errorf("attempts=%d detail=%q, want 1 / none-reported — the watch went on", g.row.Attempts, g.row.Detail)
		}
	})
}

// --- Test 5: the startup sweep ---------------------------------------------

// unreachableForge is a forge that cannot be reached (the transport arm).
func unreachableForge() forge.Forge {
	f := forge.NewFake()
	f.SetUnreachable()
	return f
}

// authErrForge is a forge that answers auth — a credential problem, not a
// network one.
func authErrForge() forge.Forge {
	f := forge.NewFake()
	f.FailMethod("ReadChecks", forge.NewMsg(forge.KindAuth, "the token was rejected (scenario)"))
	return f
}

// TestStartupSweepRepairsMissingWatch — the crash window: a merge event
// that stands without a watch gets one at startup (a root the forge
// cannot answer is skipped, a window already elapsed is born expired),
// the stale rows are expired, and a second sweep changes nothing — the
// repair is idempotent.
func TestStartupSweepRepairsMissingWatch(t *testing.T) {
	ctx := context.Background()
	base := mustTime(t, t0)
	clock := &fakeClock{now: base}
	f := forge.NewFake()
	f.SeedChecks("sha-a", []forge.Check{{Name: "ci/a", State: forge.CheckSuccess}}, true)

	s := newFakeStore(clock)
	// A stale row whose window elapsed.
	s.add(Row{MergeEventID: "w-stale", ComponentID: "c1", ComponentRoot: "/repo", SHA: "sha-a", State: StatePending, Detail: "check ci/a is pending"},
		base.Add(-2*time.Hour-30*time.Second), base.Add(-3*time.Hour))
	// The crash window: events without watches.
	s.unwatched = []MergeEventRef{
		{ID: "me-a", ComponentRoot: "/repo", ComponentID: "c1", PR: 5, MergeSHA: "sha-a", MergedAt: base.Add(-10 * time.Minute).Format(time.RFC3339)},     // watchable, in window
		{ID: "me-c", ComponentRoot: "/repo", ComponentID: "c1", PR: 6, MergeSHA: "sha-c", MergedAt: base.Add(-3 * time.Hour).Format(time.RFC3339)},        // watchable, window elapsed
		{ID: "me-b", ComponentRoot: "/noforge", ComponentID: "c2", PR: 7, MergeSHA: "sha-b", MergedAt: base.Add(-10 * time.Minute).Format(time.RFC3339)},  // no forge: not watchable
		{ID: "me-d", ComponentRoot: "/noreader", ComponentID: "c3", PR: 8, MergeSHA: "sha-d", MergedAt: base.Add(-10 * time.Minute).Format(time.RFC3339)}, // forge without checks: not watchable
	}
	forge := func(root string) forge.Forge {
		switch root {
		case "/repo":
			return f
		case "/noreader":
			return noChecksForge{}
		default:
			return nil
		}
	}
	w := &Watcher{Store: s, Forge: forge, Clock: clock,
		Settings: &fakeSettings{window: DefaultWatchWindow, interval: DefaultWatchInterval}}

	if err := w.Sweep(ctx); err != nil {
		t.Fatalf("the sweep: %v", err)
	}

	// The stale row is expired, detail kept.
	if g := s.get("w-stale"); g.row.State != StateExpired || g.row.Detail != "check ci/a is pending" {
		t.Errorf("stale row: state=%q detail=%q, want expired / the last answer kept", g.row.State, g.row.Detail)
	}
	// The watchable, in-window merge got a pending watch, due now.
	a := s.get("me-a")
	if a == nil || a.row.State != StatePending || !a.next.Equal(base) {
		t.Fatalf("me-a: %+v, want a pending watch due now (the first check is immediate)", a)
	}
	// The watchable merge whose window elapsed is born expired, never
	// pending — the sweep is repairing a crash that spanned the window.
	c := s.get("me-c")
	if c == nil || c.row.State != StateExpired || c.row.Detail != "window elapsed before first check" {
		t.Fatalf("me-c: %+v, want born expired with the honest detail", c)
	}
	// The non-watchable roots were skipped — still without a watch.
	if s.get("me-b") != nil || s.get("me-d") != nil {
		t.Error("a non-watchable root got a watch — a watch the forge cannot answer is a lie")
	}
	var still []string
	for _, r := range s.unwatched {
		still = append(still, r.ID)
	}
	if len(still) != 2 {
		t.Errorf("unwatched after the sweep = %v, want me-b and me-d (the skips)", still)
	}

	// The second sweep: nothing to repair, nothing to double.
	if err := w.Sweep(ctx); err != nil {
		t.Fatalf("the second sweep: %v", err)
	}
	if a := s.get("me-a"); a.row.State != StatePending {
		t.Errorf("me-a after the second sweep: state=%q, want still pending (the sweep does not re-enqueue)", a.row.State)
	}
}

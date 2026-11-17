package daemon

// gate_reactor_golden_test.go — S57 T1 (characterization, P2 Phase A). The
// daemon's terminal-event fan-out, pinned on the untouched tree (1ae086c0):
// `publishLoop` on `loop.finished` / `loop.blocked` resolves the loop,
// enriches the emitted fields (snapshot + row + root), and — on the two
// TERMINAL kinds only — fires the three reactors (queue, roadmap, triage)
// in fixed order, each a goroutine over the same arguments
// (kind, componentRoot, loopSlug, the enriched fields).
//
// The golden pins, per scenario × kind, the DURABLE EFFECT of each reactor
// through the real emission door (`publishLoop` → `on*Terminal` →
// `handle*Terminal`): which tracker moved, to what, and what question (if
// any) a person is asked — with the question bodies VERBATIM. The rows
// also pin the NEGATIVE surface: an untracked loop moves nothing, a
// BLOCKED loop halts the queue item (and asks) but never moves a roadmap
// item (D6) and never moves a bug out of unvalidated (a person decides),
// and NO reactor files a bug for a blocked loop (the bug list is pinned
// unchanged).
//
// T7 (VG4) adds the post-merge WATCH as a fourth reactor at this same
// emission point; it must not reorder, re-arg or silence any of the three
// pinned here — the diff is the report.
//
// The reactors are goroutines; the rows wait on the DURABLE effect (the
// same `await` discipline the package's own reactor tests use), so the
// golden is deterministic against the store, not against a clock.
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/daemon/ -run TestPublishLoopReactorsGolden -count=1 -tags sqlite_fts5

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/triage"
)

const goldGateReactor = "testdata/gate_reactor_golden.json"

type goldQuestion struct {
	Kind    string   `json:"kind"`
	Body    string   `json:"body"`
	Options []string `json:"options"`
}

type goldTracker struct {
	Identify string `json:"identify"` // the goal / title / id that names it
	Status   string `json:"status"`   // the durable state after the fan-out
}

type goldGateReactorRow struct {
	Case         string        `json:"case"`
	Kind         string        `json:"kind"`
	QueueItems   []goldTracker `json:"queueItems"`   // every item in the root, by goal
	RoadmapItems []goldTracker `json:"roadmapItems"` // every item in the root, by title
	Bugs         []goldTracker `json:"bugs"`         // every bug in the root, by id
	Learnings    []string      `json:"learnings"`    // every project learning body, verbatim
	Question     *goldQuestion `json:"question,omitempty"`
}

// gateReactorFixture drives the REAL emission door with a fresh store and
// the triage seam wired — the same harness the package's reactor tests use.
type gateReactorFixture struct {
	d    *Daemon
	s    *store.Store
	q    *queue.Queue
	root string
}

func newGateReactorFixture(t *testing.T) *gateReactorFixture {
	t.Helper()
	tc := &testClock{}
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }, Clock: tc.now})
	if err != nil {
		t.Fatalf("open the test store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	w := &wire{}
	d := New(Options{Store: s, Logger: w, Notify: w.notify})
	d.initTriage() // wire the triage seam the reactor reads (nil-safe otherwise)
	root := "/repo/gate-reactor"
	if _, err := s.CreateComponent(store.NewComponent{Name: "gr", Slug: "gr", Root: root}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	return &gateReactorFixture{d: d, s: s, q: queue.New(s), root: root}
}

// pinLoop seeds the loop row the emission resolves against and returns its
// store-minted id — the id the engine's emit carries.
func (f *gateReactorFixture) pinLoop(t *testing.T, slug string) string {
	t.Helper()
	l, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: f.root, Root: f.root, Goal: "g", Status: "running"},
		"dev-loop", 1, queueBlueprintYAML, "{}")
	if err != nil {
		t.Fatalf("pin loop %s: %v", slug, err)
	}
	return l.ID
}

// emit fires the daemon's real emission door for the terminal.
func (f *gateReactorFixture) emit(loopID, status, reason string) {
	fields := map[string]any{"loopId": loopID, "status": status}
	if reason != "" {
		fields["reason"] = reason
	}
	kind := protocol.KindLoopFinished
	if status == "blocked" {
		kind = protocol.KindLoopBlocked
	}
	f.d.publishLoop(kind, fields)
}

// readRow snapshots the durable state every reactor can touch: the queue
// items, the roadmap items, the bugs and the project learnings, plus the
// pending question (body verbatim).
// readRow snapshots the durable state every reactor can touch: the queue
// items, the roadmap items, the bugs and the project learnings, plus the
// pending question (body verbatim). The question is scoped to the loop
// that raised it (CG2), so the read names that slug — a scope without it
// would match only a question no loop raised.
func (f *gateReactorFixture) readRow(name, kind, slug string) goldGateReactorRow {
	row := goldGateReactorRow{Case: name, Kind: kind}
	if items, err := f.q.List(f.root); err == nil {
		for _, it := range items {
			row.QueueItems = append(row.QueueItems, goldTracker{Identify: it.Goal, Status: string(it.Status)})
		}
	}
	if rms, err := roadmap.New(f.s).List(f.root, roadmap.ListFilter{}); err == nil {
		for _, it := range rms {
			row.RoadmapItems = append(row.RoadmapItems, goldTracker{Identify: it.Title, Status: string(it.Status)})
		}
	}
	if bugs, err := triage.New(f.s).List(f.root, triage.ListFilter{}); err == nil {
		for _, b := range bugs {
			row.Bugs = append(row.Bugs, goldTracker{Identify: fmt.Sprintf("#%d", b.ID), Status: string(b.Validation)})
		}
	}
	if lns, err := learnings.New(f.s).List(learnings.ScopeProject, f.root); err == nil {
		for _, l := range lns {
			row.Learnings = append(row.Learnings, l.Body)
		}
	}
	sort.Slice(row.QueueItems, func(i, j int) bool { return row.QueueItems[i].Identify < row.QueueItems[j].Identify })
	sort.Slice(row.RoadmapItems, func(i, j int) bool { return row.RoadmapItems[i].Identify < row.RoadmapItems[j].Identify })
	sort.Slice(row.Bugs, func(i, j int) bool { return row.Bugs[i].Identify < row.Bugs[j].Identify })
	sort.Strings(row.Learnings)
	if q, found, err := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root, LoopSlug: slug}); err == nil && found {
		row.Question = &goldQuestion{Kind: q.Kind, Body: q.Body, Options: q.Options}
	}
	return row
}

// pinRow seeds one terminal scenario, fires the emission, waits on the
// durable effect and reads the row. The settle function is the row's own
// definition of "the reactors have done what they were going to do" —
// for the no-op rows it is a short settle window (the effect is the
// absence of rows, which a millisecond-scale reactor over an in-memory
// store has had its chance to produce by then).
func (f *gateReactorFixture) pinRow(t *testing.T, name, slug, status, reason string, settle func() bool) goldGateReactorRow {
	t.Helper()
	return f.pinRowWith(t, name, slug, f.pinLoop(t, slug), status, reason, settle)
}

// pinRowWith is the same row over an already-seeded loop — the rows whose
// setup must STAMP the loop onto a tracker (the FK needs the loop row to
// exist before the stamp) seed it through pinLoop and hand the id here.
func (f *gateReactorFixture) pinRowWith(t *testing.T, name, slug, loopID, status, reason string, settle func() bool) goldGateReactorRow {
	t.Helper()
	f.emit(loopID, status, reason)
	kind := protocol.KindLoopFinished
	if status == "blocked" {
		kind = protocol.KindLoopBlocked
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if settle() {
			return f.readRow(name, kind, slug)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the reactors did not settle for %s within 3s", name)
	return goldGateReactorRow{}
}

// settleAfter is the no-op rows' settle: a fixed window, long against the
// millisecond-scale reactor and short against the test budget.
func settleAfter(d time.Duration) func() bool {
	start := time.Now()
	return func() bool { return time.Since(start) > d }
}

func goldGateReactorRows(t *testing.T) []goldGateReactorRow {
	var rows []goldGateReactorRow

	// — Untracked loop: no queue item, no roadmap link, no triage slug.
	//   All three reactors fire and find nothing to track: the row pins
	//   that nothing moved and nothing was asked.
	for _, tc := range []struct{ name, status, reason string }{
		{"untracked-finished", "done", ""},
		{"untracked-blocked", "blocked", "the build failed at step three"},
	} {
		f := newGateReactorFixture(t)
		rows = append(rows, f.pinRow(t, tc.name, "untracked-"+tc.name, tc.status, tc.reason, settleAfter(300*time.Millisecond)))
	}

	// — Queue tracked, BLOCKED: the item halts to blocked and the halt is
	//   asked of a person (CG2), body verbatim; roadmap/triage untouched.
	{
		f := newGateReactorFixture(t)
		if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
			t.Fatal(err)
		}
		item := f.appendReady(t, "watched")
		loopID := f.pinLoop(t, "loop-one") // the FK: the loop row before the stamp
		if _, err := f.s.SetQueueItemLoop(item.ID, f.root, "loop-one"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.TransitionQueueItem(item.ID, "ready", "running"); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, f.pinRowWith(t, "queue-blocked-halts-and-asks", "loop-one", loopID, "blocked", "the build failed at step three",
			func() bool {
				row, ok, err := f.s.QueueItemByID(item.ID)
				if err != nil || !ok || row.Status != "blocked" {
					return false
				}
				_, found, qerr := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root, LoopSlug: "loop-one"})
				return qerr == nil && found
			}))
	}

	// — Queue tracked, FINISHED, advance=ask: the item reads done and the
	//   NEXT item is asked about (T4's door), body verbatim; no start.
	{
		f := newGateReactorFixture(t)
		if _, err := f.s.SetComponentQueueSettings(f.root, "ask", "manual"); err != nil {
			t.Fatal(err)
		}
		first := f.appendReady(t, "shipped")
		f.appendReady(t, "next-up")
		loopID := f.pinLoop(t, "loop-one") // the FK: the loop row before the stamp
		if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.TransitionQueueItem(first.ID, "ready", "running"); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, f.pinRowWith(t, "queue-finished-asks-about-next", "loop-one", loopID, "done", "",
			func() bool {
				row, ok, err := f.s.QueueItemByID(first.ID)
				if err != nil || !ok || row.Status != "done" {
					return false
				}
				_, found, qerr := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root, LoopSlug: "loop-one"})
				return qerr == nil && found
			}))
	}

	// — Queue tracked, FINISHED, advance=auto, NO next item: the item reads
	//   done and the advance finds nothing to start: no start, no question.
	{
		f := newGateReactorFixture(t)
		if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
			t.Fatal(err)
		}
		solo := f.appendReady(t, "solo")
		loopID := f.pinLoop(t, "loop-solo") // the FK: the loop row before the stamp
		if _, err := f.s.SetQueueItemLoop(solo.ID, f.root, "loop-solo"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.TransitionQueueItem(solo.ID, "ready", "running"); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, f.pinRowWith(t, "queue-finished-auto-no-next", "loop-solo", loopID, "done", "",
			func() bool {
				row, ok, err := f.s.QueueItemByID(solo.ID)
				return err == nil && ok && row.Status == "done"
			}))
	}

	// — Triage linked, FINISHED: the bug moves out of unvalidated to the
	//   attempt's conclusion (could-not-tell for a plain `done`) and the
	//   conclusion is filed as a project learning, body verbatim.
	{
		f := newGateReactorFixture(t)
		bug := f.fileBug(t, "crash on empty input", "the app crashes when given empty input")
		rows = append(rows, f.pinRow(t, "triage-finished-moves-bug", fmt.Sprintf("triage-validate-%d", bug.ID), "done", "",
			func() bool {
				b, ok, err := triage.New(f.s).Show(bug.ID)
				if err != nil || !ok || b.Validation != triage.ValidationCouldNotTell {
					return false
				}
				// The status commits before the reactor files its learning.
				rows, err := learnings.New(f.s).List(learnings.ScopeProject, f.root)
				return err == nil && len(rows) == 1
			}))
	}

	// — Triage linked, BLOCKED: the attempt left the bug unvalidated (a
	//   person decides), no learning filed, and NO bug filed by any
	//   reactor — the bug list is the pin for that negative.
	{
		f := newGateReactorFixture(t)
		f.fileBug(t, "crash on empty input", "the app crashes when given empty input")
		// The bug id is 1: the fixture's store is fresh and this is the
		// first bug filed in it.
		rows = append(rows, f.pinRow(t, "triage-blocked-leaves-bug", "triage-validate-1", "blocked", "the validator timed out",
			settleAfter(300*time.Millisecond)))
	}

	// — Roadmap stamped, FINISHED: the item leaves `running` for `done`.
	//   The loop row is pinned first: the stamp's composite FK refuses a
	//   slug naming no loop.
	{
		f := newGateReactorFixture(t)
		loopID := f.pinLoop(t, "loop-one")
		f.stampRunningItem(t, "the feature", "loop-one")
		rows = append(rows, f.pinRowWith(t, "roadmap-finished-lands-done", "loop-one", loopID, "done", "",
			settleAfter(300*time.Millisecond)))
	}

	// — Roadmap stamped, BLOCKED: the item KEEPS its status (D6) — the
	//   block lives in the loop's record until a person acts.
	{
		f := newGateReactorFixture(t)
		loopID := f.pinLoop(t, "loop-one")
		f.stampRunningItem(t, "the feature", "loop-one")
		rows = append(rows, f.pinRowWith(t, "roadmap-blocked-keeps-status", "loop-one", loopID, "blocked", "the build failed at step three",
			settleAfter(300*time.Millisecond)))
	}
	return rows
}

func (f *gateReactorFixture) appendReady(t *testing.T, goal string) queue.QueueItem {
	t.Helper()
	it, err := f.q.Append(f.root, "dev-loop", goal, f.root, queue.NewItem{Attended: nil})
	if err != nil {
		t.Fatalf("append %q: %v", goal, err)
	}
	it, r := f.q.Transition(it, queue.StatusReady)
	if r != nil {
		t.Fatalf("ready %q: %s", goal, r.Reason)
	}
	return it
}

func (f *gateReactorFixture) fileBug(t *testing.T, title, body string) triage.Bug {
	t.Helper()
	b, ref := triage.New(f.s).Create(f.root, triage.NewBug{Title: title, Body: body, FiledBy: "person"})
	if ref != nil {
		t.Fatalf("file bug: %s", ref.Reason)
	}
	return b
}

// stampRunningItem creates a roadmap item, walks it to `running` through
// the one door, and records the loop it materialized into.
func (f *gateReactorFixture) stampRunningItem(t *testing.T, title, slug string) {
	t.Helper()
	rm := roadmap.New(f.s)
	it, ref := rm.Create(f.root, roadmap.NewItem{Title: title, Provenance: roadmap.ProvenanceAuthored})
	if ref != nil {
		t.Fatalf("create roadmap item: %s", ref.Reason)
	}
	// D4's ready check needs a priority and both artifacts BEFORE the
	// planned → ready rung: the item is seeded the way a materialized
	// engine would have left it.
	if it, ref = rm.SetPriority(it.ID, roadmap.PriorityAssumed, "the operator said so"); ref != nil {
		t.Fatalf("set priority: %s", ref.Reason)
	}
	if it, ref = rm.SetArtifacts(it.ID, "brief.md", "plan.md"); ref != nil {
		t.Fatalf("set artifacts: %s", ref.Reason)
	}
	for _, rung := range []roadmap.Status{roadmap.StatusIdeating, roadmap.StatusBriefAgreed, roadmap.StatusPlanned, roadmap.StatusReady, roadmap.StatusRunning} {
		it, ref = rm.Transition(it.ID, rung)
		if ref != nil {
			t.Fatalf("transition to %s: %s", rung, ref.Reason)
		}
	}
	if _, ref = rm.RecordLoop(it.ID, f.root, slug); ref != nil {
		t.Fatalf("record loop: %s", ref.Reason)
	}
}

func writeGoldGateReactor(t *testing.T, rows []goldGateReactorRow) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(goldGateReactor), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goldGateReactor, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

// TestPublishLoopReactorsGolden — the terminal fan-out, pinned. The
// AEON_WRITE_GOLDEN run on the untouched tree generated the fixture;
// afterwards this test only READS it and fails on any drift, naming the
// row and the field.
func TestPublishLoopReactorsGolden(t *testing.T) {
	rows := goldGateReactorRows(t)
	got, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	if os.Getenv("AEON_WRITE_GOLDEN") == "1" {
		writeGoldGateReactor(t, rows)
		t.Logf("wrote %s (%d rows)", goldGateReactor, len(rows))
		return
	}
	want, err := os.ReadFile(goldGateReactor)
	if err != nil {
		t.Fatalf("no golden: %v — run once with AEON_WRITE_GOLDEN=1 on the untouched tree (S57 T1)", err)
	}
	if string(got) != string(want) {
		t.Fatalf("the terminal fan-out moved off its golden:\n--- golden ---\n%s--- current ---\n%sT7 (VG4) adds the watch reactor at this emission point and must not reorder, re-arg or silence the three pinned here; a move is a deliberate, listed amendment in that task's Notes.", want, got)
	}
}

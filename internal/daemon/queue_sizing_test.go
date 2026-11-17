package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/store"
)

// sizingFixture is the reactor fixture's shape with the model layer wired:
// the sizing run rides the daemon's OWN step door (R3), so the step path
// is the real one — registry, body toolset, policy gate, scripted turns.
type sizingFixture struct {
	d      *Daemon
	s      *store.Store
	q      *queue.Queue
	root   string
	models *scriptedModel
	starts chan protocol.LoopStartParams
	w      *wire
}

func newSizingFixture(t *testing.T, turns []modelTurn, advance string) *sizingFixture {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store (a failure naming fts5 means the build lacks the sqlite_fts5 tag — build through make, B6): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	root := "/repo/sizing"
	if _, err := s.CreateComponent(store.NewComponent{Name: "q", Slug: "q", Root: root}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	if _, err := s.SetComponentQueueSettings(root, advance, "skill"); err != nil {
		t.Fatalf("set settings: %v", err)
	}
	route := model.Route{ID: "r1", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024}
	sm := &scriptedModel{turns: turns}
	w := &wire{}
	d := New(Options{Store: s, Routes: protocol.NewStaticRoutes(route), Models: sm, Logger: w, Notify: w.notify})
	// The sizing-estimator's granted tools (its body names them) need
	// ToolAccess allows — the same way every gate test seeds its agent's
	// rules. No allow, no tool: the gate is real here, not stubbed.
	// The gate files tool judgements against the handler's POLICY-AGENT
	// name (measured: the disagreement audit refused my first attempt,
	// naming the mismatch) — the rules are seeded for the subject the
	// evaluator actually uses.
	for _, tool := range []string{"report_size", "read", "glob", "grep", "todo"} {
		seedRules(t, s, time.Now().UTC(), toolRulePolicy("allow-"+tool+"-for-sizing", sizingAgent+"-loop", tool, policy.Allow))
	}
	f := &sizingFixture{d: d, s: s, q: queue.New(s), root: root, models: sm, w: w,
		starts: make(chan protocol.LoopStartParams, 8)}
	d.Table.StartBlueprintLoop = func(ctx context.Context, p protocol.LoopStartParams) (any, error) {
		f.starts <- p
		return map[string]any{"id": "loop-x", "slug": "loop-x", "status": "running"}, nil
	}
	return f
}

func (f *sizingFixture) appendPending(t *testing.T, goal string) queue.QueueItem {
	t.Helper()
	it, err := f.q.Append(f.root, "dev-loop", goal, f.root, queue.NewItem{})
	if err != nil {
		t.Fatalf("append %q: %v", goal, err)
	}
	return it
}

func (f *sizingFixture) reload(t *testing.T, id int64) store.QueueItemRow {
	t.Helper()
	row, ok, err := f.s.QueueItemByID(id)
	if err != nil || !ok {
		t.Fatalf("reload item %d: ok=%v err=%v", id, ok, err)
	}
	return row
}

// TestSizingFailureRevertsToPending is CG5's edge at the dispatch: a
// sizing run that cannot report — here, no model wired, the step door
// itself failing — leaves the item PENDING, eligible to be sized again.
// The queue never sticks in `sizing` because a sizing run failed.
func TestSizingFailureRevertsToPending(t *testing.T) {
	// An empty script runs out on the FIRST turn: the scripted model
	// answers {"status":"ok","summary":"out of script"} — an ok step with
	// no size in its handoff, which is a failed sizing by CG5's own words
	// ("a failed or abandoned sizing run"). The door, the claim, and the
	// revert are all the real ones.
	f := newSizingFixture(t, nil, "auto")
	it := f.appendPending(t, "add rate limiting")
	if !f.d.startSizingRun(context.Background(), f.root) {
		t.Fatal("a skill-mode pending item must be sized")
	}
	await(t, "the failed sizing reverting to pending (CG5)", func() bool {
		return f.reload(t, it.ID).Status == string(queue.StatusPending)
	})
}

// TestSizingReportMakesItemReadyWithSize is the whole T5 path end to
// end: claim `pending → sizing`, the ORDINARY step door with the
// registered body, the model calling report_size, the handoff's three
// facts landing on the row, and the `sizing → ready` edge — through the
// daemon's own code, no hand-written rows.
func TestSizingReportMakesItemReadyWithSize(t *testing.T) {
	turns := []modelTurn{
		{toolUses: []model.Block{toolUse("t1", "report_size",
			`{"sizeMin":3,"sizeMax":8,"sizeBasis":"read the queue store, counted its doors"}`)}},
		{text: `{"status":"ok","summary":"sized at 3 to 8 points","size_min":3,"size_max":8,"size_basis":"read the queue store, counted its doors"}`},
	}
	f := newSizingFixture(t, turns, "auto")
	it := f.appendPending(t, "add rate limiting")
	if !f.d.startSizingRun(context.Background(), f.root) {
		t.Fatal("a skill-mode pending item must be sized")
	}
	if f.reload(t, it.ID).Status != string(queue.StatusSizing) {
		t.Fatalf("the claim must be visible before this returns")
	}
	// `auto` chains fast: ready is a state the queue passes through on
	// its way to running — wait for the report's FACTS (the size on the
	// row), which every post-report state carries.
	await(t, "the report landing its size on the row", func() bool {
		row := f.reload(t, it.ID)
		return row.SizeMin != nil && row.SizeMax != nil
	})
	row := f.reload(t, it.ID)
	if row.Status != string(queue.StatusReady) && row.Status != string(queue.StatusRunning) {
		t.Fatalf("after a landed report the item reads %q, want ready (ask) or running (auto chain)", row.Status)
	}
	// The tool RAN — the second model request carries the tool result
	// the runtime fed back, not just a handoff the model chose to write.
	if f.models.calls < 2 {
		t.Fatalf("the model was called %d times — the report_size tool call never reached a second turn", f.models.calls)
	}
	dump, err := json.Marshal(f.models.requests[1])
	if err != nil {
		t.Fatalf("marshal the second request: %v", err)
	}
	if !strings.Contains(string(dump), "size reported: 3-8") {
		t.Error("the second turn never carried the report_size tool's answer — the size came from prose, not from the tool (D9)")
	}
	if row.SizeMin == nil || *row.SizeMin != 3 || row.SizeMax == nil || *row.SizeMax != 8 {
		t.Errorf("size columns = %v/%v, want 3/8 — the handoff's range must be the row's range", row.SizeMin, row.SizeMax)
	}
	if row.SizeBasis == nil || !strings.Contains(*row.SizeBasis, "counted its doors") {
		t.Errorf("the basis did not survive the handoff: %v", row.SizeBasis)
	}
	// auto mode chains: the ready item's advance follows the report.
	await(t, "the auto advance starting the now-ready item", func() bool {
		select {
		case <-f.starts:
			return true
		default:
			return false
		}
	})
}

// TestSizingNeverDispatchesInManualMode pins D7's half of the mode: the
// dispatch door is skill-mode's; manual projects advance item-to-item,
// never size-to-size.
func TestSizingNeverDispatchesInManualMode(t *testing.T) {
	f := newSizingFixture(t, nil, "auto")
	if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
		t.Fatalf("flip to manual: %v", err)
	}
	it := f.appendPending(t, "unsized work")
	if f.d.startSizingRun(context.Background(), f.root) {
		t.Fatal("manual mode dispatched a sizing run — D7 says manual sizes by hand, not by daemon")
	}
	if f.reload(t, it.ID).Status != string(queue.StatusPending) {
		t.Fatalf("manual mode disturbed the item: %q", f.reload(t, it.ID).Status)
	}
	// And the advance's empty-backlog branch must not size either.
	if f.d.advanceNext(context.Background(), f.root) {
		t.Fatal("advanceNext started something it should not have")
	}
	if f.models.calls != 0 {
		t.Errorf("the model was called %d times in manual mode", f.models.calls)
	}
}

// TestAskModeStopsAfterTheReport is CG4 at sizing: a report makes the
// item ready and NOTHING advances — no question raised (that door
// belongs to terminal events), no timer, no watching. The person
// advances explicitly.
func TestAskModeStopsAfterTheReport(t *testing.T) {
	turns := []modelTurn{
		{text: `{"status":"ok","summary":"sized","size_min":2,"size_max":4,"size_basis":"counted three files"}`},
	}
	f := newSizingFixture(t, turns, "ask")
	it := f.appendPending(t, "add retries")
	if !f.d.startSizingRun(context.Background(), f.root) {
		t.Fatal("skill mode must size")
	}
	await(t, "the report landing ready", func() bool {
		return f.reload(t, it.ID).Status == string(queue.StatusReady)
	})
	time.Sleep(80 * time.Millisecond)
	select {
	case p := <-f.starts:
		t.Fatalf("ask mode started a loop after a sizing report: %+v — the advance is the person's act", p)
	default:
	}
	if n, has, err := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root}); err == nil && has {
		t.Fatalf("a sizing report raised a question (%v) — CG4 raises questions at loop terminals only", n.Body)
	}
}

// TestSizingRunCannotAppendAQueueItem is CG5's structural exemption at
// the source: neither the report tool, nor its body, nor the dispatch
// that reads the handoff reaches the Append door. Grep-pinned, per the
// house way.
func TestSizingRunCannotAppendAQueueItem(t *testing.T) {
	for _, name := range []string{"queue_sizing.go", "../tools/report_size.go", "../handler/bodies/sizing-estimator.md"} {
		src, err := readFileForTest(t, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, banned := range []string{"AppendQueueItem", ".Append("} {
			if strings.Contains(src, banned) {
				t.Errorf("%s reaches the Append door via %s — a sizing run that can queue work is a queue that sizes itself into existence (CG5)", name, banned)
			}
		}
	}
}

// TestSizingDispatchIsAnOrdinaryStepDoor pins R3 at the source: one
// step door, the daemon's own, and the agent named through the registry
// like every other step's agent — no bespoke sizing execution path.
func TestSizingDispatchIsAnOrdinaryStepDoor(t *testing.T) {
	src, err := readFileForTest(t, "queue_sizing.go")
	if err != nil {
		t.Fatalf("read the dispatch source: %v", err)
	}
	if strings.Count(src, "d.Table.RunStepForLoop(") != 1 {
		t.Error("the sizing dispatch must reach the step exactly once through d.Table.RunStepForLoop — the ordinary door")
	}
	if !strings.Contains(src, "Agent: sizingAgent") {
		t.Error("the step names its agent through the registry constant — the way any blueprint step picks one")
	}
	if strings.Contains(src, "StartBlueprintLoop(") {
		t.Error("the sizing dispatch starts loops — it dispatches a step and nothing more")
	}
}

// TestSizingReportRefusedByLawRevertsToo covers the middle refusal: an
// ok step whose handoff breaks D8's law goes back to pending like any
// failed run — the queue never holds a lawless size.
func TestSizingReportRefusedByLawRevertsToo(t *testing.T) {
	turns := []modelTurn{
		{text: `{"status":"ok","summary":"sized","size_min":9,"size_max":2,"size_basis":"inverted on purpose"}`},
	}
	f := newSizingFixture(t, turns, "ask")
	it := f.appendPending(t, "inverted report")
	if !f.d.startSizingRun(context.Background(), f.root) {
		t.Fatal("skill mode must size")
	}
	await(t, "the lawless report reverting the item", func() bool {
		return f.reload(t, it.ID).Status == string(queue.StatusPending)
	})
}

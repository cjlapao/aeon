package daemon

// automation_test.go — the seam between T4's door and T5's dispatcher, end
// to end: an approved goal becomes an unattended loop with its kickoff,
// through the REAL dispatcher and the REAL starter. The store is real, the
// blueprint is a gate (so the loop parks instead of talking to a model), and
// the runner is waited to rest before the deferred Close — the recorded
// closed-store family starts with tests that forget this bargain.

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/automation"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/store"
)

var e2eActor = store.Actor{Kind: "user", ID: "operator"}

func e2eWorld(t *testing.T) (*store.Store, *Daemon, string, store.Component) {
	t.Helper()
	s, root := t14Env(t)
	d := New(Options{Store: s})
	// The gate blueprint parks before any model step, so the runner needs
	// store + emitter + records only; no model is ever consulted.
	d.loopRunner = loop.NewRunner(loop.Deps{Store: s, Event: func(string, any) {}, Records: recordsReaderFor(s)})
	comp, err := s.ComponentForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	return s, d, root, comp
}

func e2eTrigger(t *testing.T, s *store.Store, compID, blueprint string) store.Trigger {
	t.Helper()
	tr, err := s.CreateEventTrigger(e2eActor, store.NewEventTrigger{
		Name: "on-approved", ComponentID: compID,
		Match: store.Match{Event: "goal.approved"}, Blueprint: blueprint, MaxRunsPerHour: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func lastAutomationEvent(t *testing.T, s *store.Store) store.AutomationEvent {
	t.Helper()
	evs, err := s.ReadAutomationEvents(0, 100)
	if err != nil || len(evs) == 0 {
		t.Fatalf("events: n=%d err=%v", len(evs), err)
	}
	return evs[len(evs)-1]
}

// e2eGoal files and approves a goal THAT CARRIES the component: measured,
// Trigger.Admits fails closed when a component-bound trigger meets an event
// with no component (T5's lesson again) — the same fact the dispatcher's own
// suite recorded for the plan's multi-component arm.
func e2eGoal(t *testing.T, s *store.Store, compID string) {
	t.Helper()
	comp, err := s.ReadComponent(compID)
	if err != nil {
		t.Fatal(err)
	}
	// A goal hangs under a root; a root component hangs under itself.
	rootID := comp.ParentID
	if rootID == "" {
		rootID = comp.ID
	}
	g, rf, err := s.CreateGoal(store.NewGoal{ComponentID: rootID, Title: "e2e goal", Type: "feature",
		BodyNote: "the body says go", SourceKind: "manual", By: e2eActor, Submit: true,
		Components: []store.GoalComponent{{ComponentID: compID}}})
	if err != nil || rf != nil {
		t.Fatalf("goal: refusal=%v err=%v", rf, err)
	}
	if _, rf, err := s.DecideGoal(g.ID, g.Version, "approved", e2eActor, "ok"); err != nil || rf != nil {
		t.Fatalf("approve: refusal=%v err=%v", rf, err)
	}
}

func TestAnApprovedGoalStartsAnUnattendedLoopEndToEnd(t *testing.T) {
	s, d, root, comp := e2eWorld(t)
	bp := writeGateBlueprint(t, "e2egate")
	tr := e2eTrigger(t, s, comp.ID, bp)
	e2eGoal(t, s, comp.ID)
	ev := lastAutomationEvent(t, s)

	if n, err := d.newDispatcher().Drain(context.Background()); err != nil || n != 1 {
		t.Fatalf("drain n=%d err=%v — the approved goal's event must be dispatched", n, err)
	}

	fs, err := s.ListEventFires(store.EventFireFilter{TriggerID: tr.ID})
	if err != nil || len(fs) != 1 {
		t.Fatalf("fires n=%d err=%v", len(fs), err)
	}
	want := automation.AutoSlug(tr.ID, ev.ID)
	if fs[0].Outcome != store.FireStarted || fs[0].LoopSlug != want || fs[0].LoopID == "" {
		t.Fatalf("fire %+v — started, with the deterministic slug and the loop's id", fs[0])
	}

	l, err := s.GetLoop(store.Scope{LoopSlug: want, ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if l.StartedBy != "event" || l.TriggerID != tr.ID {
		t.Errorf("provenance started_by=%q trigger_id=%q — T4's door declares BOTH at the pin", l.StartedBy, l.TriggerID)
	}
	if l.AutomationDepth != 1 {
		t.Errorf("automation_depth=%d — a human-door event (depth 0) starts the slice's first rung", l.AutomationDepth)
	}
	if att, err := s.LoopAttended(context.Background(), l.ID); err != nil || att == nil || *att {
		t.Errorf("attended=%v err=%v — an event-started loop is unattended by construction (AU2)", att, err)
	}
	if outward, err := s.LoopAllowOutward(context.Background(), l.ID); err != nil || outward {
		t.Errorf("allow_outward=%v err=%v — unattended means the outward door stays shut (AU2)", outward, err)
	}

	msgs, err := s.Thread(store.Scope{LoopSlug: want, ComponentRoot: root}, 0)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("thread n=%d err=%v", len(msgs), err)
	}
	m := msgs[0]
	if m.Seq != 1 || m.Source != "automation" || m.SourceRef != tr.ID || m.Author != loop.KickoffAuthor("automation") {
		t.Fatalf("kickoff row seq=%d source=%q ref=%q author=%q — the kickoff rides the pin and names its trigger",
			m.Seq, m.Source, m.SourceRef, m.Author)
	}
	if fs[0].LoopID != l.ID {
		t.Errorf("fire loop id %q != loop %q — the record and the row must name the same loop", fs[0].LoopID, l.ID)
	}

	waitGateParked(t, s, d, l.ID) // the runner rests before the deferred Close
}

func TestTheAdapterRefusesAndTheFireSaysSo(t *testing.T) {
	s, d, root, comp := e2eWorld(t)
	tr := e2eTrigger(t, s, comp.ID, "/no/such/blueprint.yaml")
	e2eGoal(t, s, comp.ID)

	if n, err := d.newDispatcher().Drain(context.Background()); err != nil || n != 1 {
		t.Fatalf("drain n=%d err=%v", n, err)
	}
	fs, err := s.ListEventFires(store.EventFireFilter{TriggerID: tr.ID})
	if err != nil || len(fs) != 1 {
		t.Fatalf("fires n=%d err=%v", len(fs), err)
	}
	if fs[0].Outcome != store.FireRefused || !strings.HasPrefix(fs[0].Reason, "blueprint-refused") {
		t.Fatalf("fire %+v — a missing blueprint is a spoken refusal, not a swallowed error", fs[0])
	}
	loops, err := s.ListLoops(store.ComponentScope{ComponentRoot: root})
	if err != nil || len(loops) != 0 {
		t.Fatalf("%d loop rows after a refusal (%v) — a refusal writes no loop", len(loops), err)
	}
}

func TestRunAutomationWithoutAStoreReturnsNil(t *testing.T) {
	d := New(Options{})
	if err := d.RunAutomation(context.Background()); err != nil {
		t.Fatalf("RunAutomation on an unwired daemon: %v — the honest absence, not a panic", err)
	}
}

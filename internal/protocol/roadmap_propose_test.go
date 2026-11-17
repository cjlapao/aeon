package protocol

// roadmap_propose_test.go — S50 T5 (RM8): the SHAPE of the roadmap plane's
// one agent door, beside the automation, environment, blueprint, policy and
// autonomy twins'. What lives here is the DOOR: registered iff the records
// service stands (V13), gated and non-debug like every write on this table,
// and dispatching through the SAME RecordsAPI.Do the tool door calls (one
// implementation, two doors — a handler here that answered for itself would
// be the second door RM8 forbids, and a ninth roadmap write the T4 guards
// pinned impossible). What the refusal sentence is — unknown root, existing
// twin, smuggled rank — is the seam's word (cmd/aeon/roadmap_proposal_test
// walks it at the desk); this file pins that the door carries it to the
// caller whole (ErrBadParams, named) and that the family's parity walk
// stands.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cjlapao/aeon/internal/records"
)

// TestRoadmapProposeIdeaRegistrationIsConditional is the V13 half: no
// records service, no method — ABSENT (ErrUnknownMethod), never
// registered-and-refusing. The records service is what honours what makes
// the door safe (the stamped step actor, the shared create validator of the
// seam, the per-agent open-proposal cap), so an unwired runtime does not
// advertise it. The two READ twins are not this test's subject: their
// methods are the standing roadmap.board and roadmap.show doors every
// mount registers over the roadmap seam, and registerRecords skips the
// whole roadmap family so no door is ever projected twice.
func TestRoadmapProposeIdeaRegistrationIsConditional(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tbl.RegisterRoadmapProposeIdea() {
		t.Fatal("RegisterRoadmapProposeIdea registered over a nil records seam — V13 says absent, not refusing")
	}
	if _, ok := tbl.Lookup(MethodRoadmapProposeIdea); ok {
		t.Error("roadmap.proposeIdea stands without the records service — the mutating door was registered-and-refusing")
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodRoadmapProposeIdea, json.RawMessage(`{}`)); !errors.As(err, new(ErrUnknownMethod)) {
		t.Errorf("dispatch without the seam = %v, want ErrUnknownMethod", err)
	}

	tbl.Records = &envProposeSpy{recordNameSpy: &recordNameSpy{}}
	if !tbl.RegisterRoadmapProposeIdea() {
		t.Fatal("RegisterRoadmapProposeIdea refused a wired seam")
	}
	got, ok := tbl.Lookup(MethodRoadmapProposeIdea)
	if !ok {
		t.Fatal("roadmap.proposeIdea did not register with the service wired")
	}
	if !got.RequiresInitialize {
		t.Error("roadmap.proposeIdea is not RequiresInitialize — a mutating catalog action before the handshake is unthinkable (PM7)")
	}
	if got.Debug {
		t.Error("roadmap.proposeIdea is Debug — product surface (PM10)")
	}
	// Idempotent: a second call is a no-op, not a second door (t.register
	// panics on a duplicate, so this is a crash test as much as a shape one).
	if !tbl.RegisterRoadmapProposeIdea() {
		t.Error("RegisterRoadmapProposeIdea is not idempotent over the same wired seam")
	}
}

// TestRoadmapProposeIdeaDispatchesThroughDo pins the ONE implementation: the
// socket twin dispatches into RecordsAPI.Do with the catalog tool name and
// the caller's params untouched, so the socket door and the tool door answer
// through the same function; a refusal sentence from the service reaches the
// caller as ErrBadParams with the words whole (the harness's "invalid
// params: " prefix rides; the seam's sentence does not change in transit).
func TestRoadmapProposeIdeaDispatchesThroughDo(t *testing.T) {
	spy := &envProposeSpy{recordNameSpy: &recordNameSpy{}}
	tbl := newTestTable(t)
	tbl.Records = spy
	if !tbl.RegisterRoadmapProposeIdea() {
		t.Fatal("the propose door did not register")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	params := json.RawMessage(`{"componentRoot":"/srv/aeon","title":"a title","note":"a note"}`)
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodRoadmapProposeIdea, params); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if spy.last != "roadmap_propose_idea" {
		t.Errorf("Do saw tool %q, want roadmap_propose_idea — the catalog's vocabulary, not a second method table", spy.last)
	}
	if string(spy.params) != string(params) {
		t.Errorf("Do saw params %s, want the caller's bytes %s untouched", spy.params, params)
	}

	spy.sentence = `refused: no component is rooted at "/srv/aeon" — roadmap.board shows what roots exist (S50 T5)`
	_, err := tbl.Dispatch(context.Background(), "c1", MethodRoadmapProposeIdea, params)
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("a service refusal reached the caller as %T (%v), want ErrBadParams", err, err)
	}
	if want := spy.sentence; bad.Err.Error() != want {
		t.Errorf("the seam's sentence drifted in transit:\n got  %q\n want %q", bad.Err.Error(), want)
	}
}

// TestRoadmapFamilyHasNoWriteTwin is the family's parity walk T5 names:
// every roadmap spec's dot-name twin stands, NO roadmap spec is twinned to
// any of the plane's eight writes (or to ideate/plan/materialize), and
// exactly one spec mutates. The eight writes are the person's doors — a
// catalog name pointing at one would be RM4 walking in through the schema.
func TestRoadmapFamilyHasNoWriteTwin(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Records = &envProposeSpy{recordNameSpy: &recordNameSpy{}}
	if !tbl.RegisterRoadmapProposeIdea() {
		t.Fatal("RegisterRoadmapProposeIdea refused a wired records seam")
	}
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	personDoors := []MethodName{
		MethodRoadmapAdd, MethodRoadmapUpdate, MethodRoadmapCancel, MethodRoadmapEstimate,
		MethodRoadmapPriority, MethodRoadmapIdeate, MethodRoadmapPlan, MethodRoadmapMaterialize,
	}
	human := map[MethodName]bool{}
	for _, m := range personDoors {
		human[m] = true
	}
	mutators, seen := 0, 0
	for _, sp := range records.Catalog() {
		if sp.Family != "roadmap" {
			continue
		}
		seen++
		m := MethodName(sp.Method)
		if !registered[m] {
			t.Errorf("%s twinned to %s, which is not registered — a tool the app cannot reach", sp.Name, sp.Method)
		}
		if human[m] {
			t.Errorf("%s twinned to %s — a roadmap WRITE door is the person's alone (RM8, T4's guards); no catalog name may point at one", sp.Name, sp.Method)
		}
		if sp.Mutates {
			mutators++
		}
	}
	if seen != 5 {
		// 2026-11-05 (S27 T3): 3 → 5, deliberate — the graph's two edges
		// (roadmap_depend, roadmap_undepend) joined the family. The human[]
		// check above is what keeps RM8 honest: it still refuses ANY spec
		// pointed at a humanDoorOnly write, and the eight LADDER doors keep
		// that flag whole; the edge doors do not claim it because their
		// twins are real (S27 T3's classification is the claim).
		t.Errorf("the roadmap family carries %d specs, want exactly 5 (two reads + the one ask + the graph's two edges)", seen)
	}
	if mutators != 3 {
		// 2026-11-05 (S27 T3): 1 → 3 — the ask plus the two edges; an edge
		// writes a durable row and says so, and touches no rung.
		t.Errorf("%d mutating roadmap specs, want exactly 3 (the ask and the two edges, S27 T3) — a fourth is RM8 broken", mutators)
	}
}

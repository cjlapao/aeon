package blueprint

import "testing"

// TestTheThirdStateSurvivesAnEmptyFollowUpList — S13 T16 shipped the last
// follow-up and futureAgents reached EMPTY. The empty list must not read as
// "the third state is gone": Unverifiable is the honest answer whenever a
// name is DECLARED a future body, and the mechanism is the list plus the
// agent-registry branch, not any particular name. The test mutates the
// package-private list the way the next follow-up's commit will (declare a
// name, ship nothing), asserts the exact diagnostic, and restores — a
// deleted branch would make the middle assertion unreachable, which is
// precisely what "deleting the branch deletes the state" means.
func TestTheThirdStateSurvivesAnEmptyFollowUpList(t *testing.T) {
	if got := FutureAgents(); len(got) != 0 {
		t.Fatalf("futureAgents = %v, want empty — all five S13 follow-ups shipped (measured 2026-10-26); a name reappearing here without its body shipping is the bug this test also guards", got)
	}
	saved := futureAgents
	defer func() { futureAgents = saved }()
	futureAgents = []string{"zz-phantom-follow-up"}

	b, err := Load([]byte("id: x\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: a\n    step: {kind: model-step, agent: zz-phantom-follow-up}\n"))
	if err != nil {
		t.Fatal(err)
	}
	r := Validate(b, ValidationOptions{Agents: []string{"golang-engineer"}})
	if len(r.Diagnostics) != 1 {
		t.Fatalf("a declared follow-up must produce exactly one diagnostic, got %+v", r.Diagnostics)
	}
	d := r.Diagnostics[0]
	if d.State != Unverifiable || d.Kind != "agent-registry" || d.Path != "lanes[a].step.agent" {
		t.Fatalf("the third state was not produced: %+v", d)
	}
	futureAgents = saved
	r2 := Validate(b, ValidationOptions{Agents: []string{"golang-engineer"}})
	if len(r2.Diagnostics) != 1 || r2.Diagnostics[0].State != Invalid {
		t.Fatalf("an UNDECLARED unknown name must be Invalid, never unverifiable — the third state is for named follow-ups only: %+v", r2.Diagnostics)
	}
}

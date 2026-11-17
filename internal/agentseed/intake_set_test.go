package agentseed

import (
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/seed"
)

// TestIntakeArchitectToolListIsExact is GI15's grant, pinned at the seed
// source: the interview's voice holds exactly the reads it needs to ground
// its options, the two asks it files, and the scratch write its capture
// contract names — no bash, no goal_create, no component_create, no start,
// no commit. A tool added to the body is added HERE first, in review.
func TestIntakeArchitectToolListIsExact(t *testing.T) {
	recs := IntakeSource()
	if len(recs) != 1 || recs[0].Key != "intake-architect" {
		t.Fatalf("the intake set carries %v, want the one intake-architect record", keys(recs))
	}
	var c content
	if err := json.Unmarshal(recs[0].Content, &c); err != nil {
		t.Fatal(err)
	}
	want := []string{"read", "glob", "grep", "question", "todo", "write",
		"charter_show", "template_list", "template_show", "component_list", "goal_list", "intake_status",
		"charter_propose", "intake_propose"}
	if len(c.Tools) != len(want) {
		t.Fatalf("tools = %v, want exactly %v", c.Tools, want)
	}
	for i, w := range want {
		if c.Tools[i] != w {
			t.Fatalf("tools = %v, want exactly %v", c.Tools, want)
		}
	}
	if c.PolicyAgent != "intake-architect" || c.Slug != "intake-architect" {
		t.Fatalf("policy_agent=%q slug=%q", c.PolicyAgent, c.Slug)
	}
	if recs[0].Version != intakeSetVersion {
		t.Fatalf("record version %d, want the set's %d", recs[0].Version, intakeSetVersion)
	}
}

// TestFirstPartyStillThirtyThree is the census the exclusion protects
// (2026-11-05, S61 T9): the intake-architect seeds into agents.intake, and
// agents.firstParty stays the 33 records it shipped — the intake voice is
// not a widened 34th first-party handler, and one row may not be claimed
// by two sets.
func TestFirstPartyStillThirtyThree(t *testing.T) {
	recs := Source()
	if len(recs) != 33 {
		t.Fatalf("firstParty carries %d records, want the settled 33", len(recs))
	}
	for _, r := range recs {
		if r.Key == intakeBody {
			t.Fatal("the intake-architect leaked into agents.firstParty — the exclusion filter is the law")
		}
	}
}

func keys(rs []seed.Record) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Key)
	}
	return out
}

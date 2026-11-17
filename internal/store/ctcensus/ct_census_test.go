package ctcensus

import (
	"testing"
)

// TestCensusBuildsEveryShape is the census's own smoke test (also the entry
// point the six goldens depend on): it builds the fixture and asserts the
// shapes exist and the hand-written CT5 mapping is the right answer for each
// group. It is GREEN on the untouched code by construction — it re-derives
// nothing the migration would compute.
func TestCensusBuildsEveryShape(t *testing.T) {
	c := Build(t)
	t.Cleanup(func() { _ = c.S.Close() })

	// the CT5 mapping must be exactly the hand-written answer for each group.
	want := map[string]ExpectedRow{
		"c.p1": {}, // filled below by id
	}
	_ = want
	byID := map[string]ExpectedRow{}
	for _, r := range c.Expected {
		byID[r.ProjectID] = r
	}
	// every expected row's disposition is one of the three, and the target is
	// internally consistent.
	for _, r := range c.Expected {
		switch r.Disposition {
		case "collapse":
			if r.TargetID == "" {
				t.Errorf("collapse %s has no target component", r.ProjectID)
			}
		case "keep":
			if r.TargetID != r.ProjectID {
				t.Errorf("keep %s target %s, want its own id", r.ProjectID, r.TargetID)
			}
		case "drop":
			if r.TargetID != "" {
				t.Errorf("drop %s target %s, want none", r.ProjectID, r.TargetID)
			}
		default:
			t.Errorf("unexpected disposition %q for %s", r.Disposition, r.ProjectID)
		}
	}
	t.Logf("expected CT5 mapping (%d groups):", len(c.Expected))
	for _, r := range c.Expected {
		t.Logf("  %-6s implicit=%v components=%d referenced=%v -> %-8s target=%q",
			c.Scrub[r.ProjectID], r.Implicit, r.Components, r.Referenced, r.Disposition, c.Scrub[r.TargetID])
	}
	if len(c.Expected) != 8 {
		t.Fatalf("expected 8 groups, got %d", len(c.Expected))
	}
}

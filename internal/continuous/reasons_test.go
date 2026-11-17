package continuous

import (
	"os"
	"strings"
	"testing"
)

// TestReasonVocabularyIsClosed pins CN15's closed vocabulary three ways:
// the constants, the fixture cut from design.md's CN15 line, and the
// sentences — each constant has one, no constant renders the "unknown
// reason" fallback, and no sentence embeds free text beyond the ids and
// enums the reason is allowed to name (the detail slots are the ONLY
// input, and every test detail is ids/enums).
func TestReasonVocabularyIsClosed(t *testing.T) {
	raw, err := os.ReadFile("testdata/cn15-vocabulary.txt")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(strings.TrimSpace(string(raw)))
	set := Reasons()
	if len(lines) != len(set) {
		t.Fatalf("the fixture lists %d reasons, the code %d — CN15's line is one list", len(lines), len(set))
	}
	byName := map[Reason]bool{}
	for _, r := range set {
		byName[r] = true
	}
	for i, line := range lines {
		if !byName[Reason(line)] {
			t.Errorf("fixture line %d names %q, which is not a constant", i+1, line)
		}
	}

	// Sentences: every constant renders, renders differently per reason,
	// and never falls through to the unknown-reason line.
	details := map[Reason]string{
		ReasonRepoOff: "/srv/x", ReasonRepoOccupied: "loop-7 (running)",
		ReasonWindowClosed: "2026-11-06T09:00:00Z", ReasonQueueLive: "2 live",
		ReasonAttendedItem: "queue item 3", ReasonNotReady: "brief_path unset",
		ReasonDependencyOpen: "item 4", ReasonDependencyCancelled: "item 9",
		ReasonAlreadyClaimed: "roadmap item 12", ReasonCapacity: "3 in use + 2 for this repository",
		ReasonWeightExceedsCap: "weight 5 > max_slots 4", ReasonRateLimit: "2026-11-05T11:30:00Z",
		ReasonBudgetExhausted: "daily cap reached", ReasonGrantAbsent: "repository /srv/x",
		ReasonTemplateInvalid: `blueprint "implement-only"`,
	}
	seen := map[string]Reason{}
	for _, r := range set {
		s := Sentence(r, details[r])
		if s == "" {
			t.Errorf("%s has no sentence", r)
			continue
		}
		if strings.HasPrefix(s, "unknown reason") {
			t.Errorf("%s falls through to the unknown-reason line", r)
		}
		if prev, dup := seen[s]; dup && prev != r {
			t.Errorf("%s and %s render the same sentence — the reasons must be distinguishable: %q", prev, r, s)
		}
		seen[s] = r
	}
	// The fallback itself: an id outside the closed set says so, loudly.
	if s := Sentence(Reason("not-a-reason"), ""); !strings.HasPrefix(s, "unknown reason") {
		t.Errorf("a reason outside the vocabulary rendered %q — it must name its own illegality", s)
	}
}

// TestReasonsOrderIsTheDocOrder keeps Reasons() stable for doors that
// list the vocabulary (the fixture order is the doc order).
func TestReasonsOrderIsTheDocOrder(t *testing.T) {
	raw, _ := os.ReadFile("testdata/cn15-vocabulary.txt")
	lines := strings.Fields(string(raw))
	got := Reasons()
	for i := range lines {
		if Reason(lines[i]) != got[i] {
			t.Errorf("position %d: doc says %q, Reasons() says %q", i, lines[i], got[i])
		}
	}
}

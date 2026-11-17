package history

import (
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// TestDescribeKeepsCostHonestyState pins HV2's second load-bearing rule: the
// cost arrives as Render() AND as its honesty STATE, three-valued (S33), never
// collapsed. known ⇔ a total; partial ⇔ a floor built from partial numbers
// with nothing unknown; unknown ⇔ at least one contributing step had no
// number. The three render differently and mean different things; a formatter
// that folded them would be the second truth HV2 exists to prevent.
func TestDescribeKeepsCostHonestyState(t *testing.T) {
	cases := []struct {
		name  string
		roll  Rollup
		state string
	}{
		{"known", Rollup{Total: 3.00, Floor: false, StepCount: 2}, CostKnown},
		{"partial", Rollup{Total: 3.00, Floor: true, UnknownCount: 0, StepCount: 2}, CostPartial},
		{"unknown", Rollup{Total: 0.00, Floor: true, UnknownCount: 1, StepCount: 3}, CostUnknown},
	}
	for _, tc := range cases {
		d := Describe(Entry{Kind: KindLive, ID: "x", Status: "running", Cost: tc.roll})
		if d.CostState != tc.state {
			t.Errorf("%s rollup described its cost state as %q, want %q", tc.name, d.CostState, tc.state)
		}
		if d.Cost != tc.roll.Render() {
			t.Errorf("%s: describe cost %q is not Rollup.Render() %q — one formatter, not two", tc.name, d.Cost, tc.roll.Render())
		}
	}
	// The unknown floor's sentence keeps its count beside the state: the
	// rendered string still says (1 of 3 steps unknown), so state and words
	// agree instead of the state replacing the words.
	unk := Describe(Entry{Kind: KindLive, ID: "x", Status: "running",
		Cost: Rollup{Floor: true, UnknownCount: 1, StepCount: 3}})
	if unk.Cost == "" || unk.CostState != CostUnknown {
		t.Errorf("an unknown-cost row lost either its words (%q) or its state (%q)", unk.Cost, unk.CostState)
	}
}

// TestDescribeOutcomeIsVerbatimAndLinksAreData pins HV2's first and third
// rules: the outcome word is the STORED status byte-for-byte — including a
// word outside the vocabulary, which is rendered, never collapsed into a known
// word (S25 design line 65: no surface re-phrases an outcome) — and the links
// are {kind,id} pairs a client walks, never a sentence to parse. An entry
// carrying no id for a kind carries NO link, not an empty one.
func TestDescribeOutcomeIsVerbatimAndLinksAreData(t *testing.T) {
	e := Entry{
		Kind: KindLive, ID: "loop-1", Slug: "one", Status: "suspended-by-hamilton",
		NowTask: "T3", GoalID: "goal-9", RoadmapID: "41",
		Cost: Rollup{Total: 1.00, StepCount: 1},
	}
	d := Describe(e)
	if d.Outcome != "suspended-by-hamilton" {
		t.Errorf("outcome = %q, want the stored status verbatim — collapsing an unknown word is re-phrasing an outcome", d.Outcome)
	}
	want := []Link{{LinkLoop, "loop-1"}, {LinkTask, "T3"}, {LinkGoal, "goal-9"}, {LinkRoadmap, "41"}}
	var got []Link
	for _, l := range d.Links {
		if l != (Link{}) {
			got = append(got, l)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("links = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("link %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// The honest absence: an archived header names no task, goal or item.
	a := Entry{Kind: KindArchived, ID: "arch-1", Status: "done",
		Cost: Rollup{Floor: true, UnknownCount: 1, StepCount: 1}}
	da := Describe(a)
	if da.Links[0].Kind != LinkLoop || da.Links[1] != (Link{}) || da.Links[2] != (Link{}) || da.Links[3] != (Link{}) {
		t.Errorf("an archived header carries links %v — an id nobody has must produce no link", da.Links)
	}
	if da.Lane != "archived" {
		t.Errorf("lane label = %q, want the merge's own tag %q", da.Lane, "archived")
	}
}

// TestDescribeDurationIsHonestOrAbsent: the span renders from the entry's own
// stamps, and every unreadable shape — missing end, bad stamp, an end before
// the start — is the honest "" and never a confident "0s" or a guess.
func TestDescribeDurationIsHonestOrAbsent(t *testing.T) {
	d := Describe(Entry{Kind: KindLive, ID: "x", Status: "running",
		StartedAt: "2026-11-04T08:00:00Z", Timeline: "2026-11-04T10:12:00Z",
		Cost: Rollup{Total: 0.10, StepCount: 1}})
	if d.Duration != "2h 12m" {
		t.Errorf("duration = %q, want \"2h 12m\"", d.Duration)
	}
	for _, bad := range [][2]string{
		{"", "2026-11-04T10:00:00Z"},
		{"2026-11-04T08:00:00Z", ""},
		{"not-a-time", "2026-11-04T10:00:00Z"},
		{"2026-11-04T10:00:00Z", "2026-11-04T08:00:00Z"},
	} {
		dd := Describe(Entry{Kind: KindLive, ID: "x", Status: "running",
			StartedAt: bad[0], Timeline: bad[1],
			Cost: Rollup{Floor: true, UnknownCount: 1, StepCount: 1}})
		if dd.Duration != "" {
			t.Errorf("duration for (%q,%q) = %q, want the honest absence", bad[0], bad[1], dd.Duration)
		}
	}
}

// TestQueryLinksCarryGoalTaskAndRoadmapItem: the links Describe renders come
// from the query itself — a live loop's own goal/task coordinates and the
// roadmap item materialized to it (roadmap_item.loop_slug, S26's pair) — and
// an entry with no item keeps none. This is link data beside the merge, never
// a second merge: the sequence and the facts stay Merge's exactly.
func TestQueryLinksCarryGoalTaskAndRoadmapItem(t *testing.T) {
	s := openHistoryStore(t)
	seedComponent(t, s, "/proj")
	l, _ := seedLoop(t, s, "/proj", "linked")
	if err := s.GormDB().Exec(`UPDATE loop SET goal_id = 'goal-7', now_task = 'T2' WHERE id = ?`, l.ID).Error; err != nil {
		t.Fatalf("set the loop's coordinates: %v", err)
	}
	item, err := s.CreateRoadmapItem(store.NewRoadmapItemRow{
		ComponentRoot: "/proj", Title: "the item that made the loop", Provenance: "authored",
	})
	if err != nil {
		t.Fatalf("create a roadmap item: %v", err)
	}
	for _, edge := range [][2]string{
		{"idea", "ideating"}, {"ideating", "brief-agreed"}, {"brief-agreed", "planned"},
		{"planned", "ready"}, {"ready", "running"},
	} {
		ok, err := s.TransitionRoadmapItem(item.ID, edge[0], edge[1])
		if err != nil {
			t.Fatalf("transition item %s→%s: %v", edge[0], edge[1], err)
		}
		if !ok {
			t.Fatalf("transition %s→%s claimed nothing moved", edge[0], edge[1])
		}
	}
	ok, err := s.SetRoadmapItemLoop(item.ID, "/proj", "linked", "running")
	if err != nil || !ok {
		t.Fatalf("materialize the loop: ok=%v err=%v", ok, err)
	}
	seedLoop(t, s, "/proj", "unlinked")

	entries, err := Query(s, Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}}, ByLoop)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	bySlug := map[string]Entry{}
	for _, e := range entries {
		bySlug[e.Slug] = e
	}
	linked := bySlug["linked"]
	if linked.GoalID != "goal-7" || linked.NowTask != "T2" || linked.RoadmapID == "" {
		t.Fatalf("the linked loop carries %q/%q/%q — the query must carry its coordinates and its item",
			linked.GoalID, linked.NowTask, linked.RoadmapID)
	}
	kinds := map[string]string{}
	for _, lk := range Describe(linked).Links {
		if lk != (Link{}) {
			kinds[lk.Kind] = lk.ID
		}
	}
	if kinds[LinkGoal] != "goal-7" || kinds[LinkTask] != "T2" || kinds[LinkRoadmap] == "" {
		t.Errorf("describe links = %v, want goal/task/roadmap all present", kinds)
	}
	if kinds[LinkRoadmap] != linked.RoadmapID {
		t.Errorf("the roadmap link names item %q, the query found %q", kinds[LinkRoadmap], linked.RoadmapID)
	}
	n := 0
	for _, lk := range Describe(bySlug["unlinked"]).Links {
		if lk != (Link{}) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("an unlinked loop carries %d links, want its loop alone", n)
	}
}

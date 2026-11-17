package continuous

import (
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/roadmap"
)

// TestAttendedQueueItemIsSkippedNotStarted is the CN6/attended pin, and
// the NULL clause T0 confirmed (S24 D5/D6): NULL means NEVER-DECLARED,
// and an undeclared loop is unattended — the scheduler's own start pin —
// so NULL and false start while true alone is skipped. A silent override
// of a person's "attended" would be the daemon second-guessing its owner.
func TestAttendedQueueItemIsSkippedNotStarted(t *testing.T) {
	cases := []struct {
		name     string
		attended *bool
		starts   bool
	}{
		{"attended", ptr(true), false},
		{"not attended", ptr(false), true},
		{"never declared (NULL)", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := baseSnapshot()
			h := readyQueueHead(7)
			h.Attended = c.attended
			s.Repos[0].QueueHead = h
			ds := Plan(s)
			if len(ds) != 1 {
				t.Fatalf("decisions = %v", fmtDecisions(ds))
			}
			if got := ds[0].Verdict == VerdictStart; got != c.starts {
				t.Fatalf("verdict %s, want start=%v", ds[0].Verdict, c.starts)
			}
			if !c.starts && ds[0].Reason != ReasonAttendedItem {
				t.Fatalf("reason %s, want attended-item", ds[0].Reason)
			}
		})
	}
}

// TestRoadmapOnlyIntoADrainedRepository is CN6.2 in both directions: a
// live queue row holds the repository's roadmap out of the tick
// (queue-live), and an occupying loop holds out everything (repo-occupied)
// — flooding a busy repository is how double work and orphan starts are
// born.
func TestRoadmapOnlyIntoADrainedRepository(t *testing.T) {
	t.Run("live rows", func(t *testing.T) {
		s := baseSnapshot()
		s.Repos[0].QueueLive = 1
		s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
		ds := Plan(s)
		if len(ds) != 1 || ds[0].Reason != ReasonQueueLive || ds[0].Subject.Kind != "repository" {
			t.Fatalf("decisions = %v, want one queue-live at repository level", fmtDecisions(ds))
		}
		for _, d := range ds {
			if d.Subject.Kind == "roadmap" {
				t.Fatalf("roadmap item %s entered a live repository", d.Subject)
			}
		}
	})
	t.Run("occupying loop", func(t *testing.T) {
		s := baseSnapshot()
		s.Repos[0].Occupying = []LoopRef{{Slug: "loop-4", State: "running"}}
		s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
		ds := Plan(s)
		if len(ds) != 1 || ds[0].Reason != ReasonRepoOccupied {
			t.Fatalf("decisions = %v, want one repo-occupied", fmtDecisions(ds))
		}
	})
}

// TestCapacityAdmissionOrderIsFairAndDeterministic walks the whole CN11
// order across five repositories competing for one slot: aged deferrals
// first (oldest first among them), then priority state, then oldest
// LastStart (zero = never = oldest), then root text — and identical
// snapshots decide identically, fifty runs deep, because a tick that
// flips a coin starves somebody.
func TestCapacityAdmissionOrderIsFairAndDeterministic(t *testing.T) {
	s := baseSnapshot()
	s.Settings.MaxSlots = 1
	mk := func(root string, id int64, p roadmap.PriorityState) Repo {
		return Repo{Root: root, RootID: int64(len(root)), Enabled: true, Template: "t",
			TemplateOK: true, MinPriority: roadmap.PriorityAssumed, Weight: 1, BudgetOK: true,
			Ready: []RoadmapUnit{readyItem(id, p)}}
	}
	alpha := mk("/srv/alpha", 1, roadmap.PriorityAssumed)
	alpha.FirstDeferral = map[SubjectKey]time.Time{
		Subject{Kind: "roadmap", ID: "1"}.Key(): s.Now.Add(-48 * time.Hour), // aged, oldest deferral
	}
	alpha.LastStart = s.Now // recent — aging must beat it
	beta := mk("/srv/beta", 2, roadmap.PriorityAssumed)
	beta.FirstDeferral = map[SubjectKey]time.Time{
		Subject{Kind: "roadmap", ID: "2"}.Key(): s.Now.Add(-30 * time.Hour), // aged, but younger than alpha's
	}
	known := mk("/srv/gamma", 3, roadmap.PriorityKnown) // best priority among the un-aged
	early := mk("/srv/delta", 4, roadmap.PriorityAssumed)
	early.LastStart = s.Now.Add(-100 * time.Hour) // oldest real start
	fresh := mk("/srv/epsilon", 5, roadmap.PriorityAssumed)
	fresh.LastStart = s.Now
	s.Repos = []Repo{alpha, beta, known, early, fresh} // deliberately unsorted

	ds := Plan(s)
	if ds[0].Verdict != VerdictStart || ds[0].Root != "/srv/alpha" {
		t.Fatalf("winner = %s, want /srv/alpha (oldest deferral, aged)", ds[0].Root)
	}
	for _, d := range ds[1:] {
		if d.Reason != ReasonCapacity || d.Verdict != VerdictDefer {
			t.Fatalf("the loser %s decided %s/%s, want defer capacity", d.Root, d.Verdict, d.Reason)
		}
	}
	order := make([]string, 0, 4)
	for _, d := range ds[1:] {
		order = append(order, d.Root)
	}
	want := []string{"/srv/beta", "/srv/gamma", "/srv/delta", "/srv/epsilon"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("defer order = %v, want %v (aged-younger, known, oldest-start, fresh)", order, want)
		}
	}
	first := mustJSON(t, ds)
	for i := 0; i < 50; i++ {
		if again := mustJSON(t, Plan(s)); again != first {
			t.Fatalf("run %d diverged:\n%s\n%s", i, first, again)
		}
	}
}

// TestWeightExceedsCapRefusedOnce: a repository heavier than the whole
// cap is a misconfiguration to FIX, not a queue to keep — refuse, name
// the setting, and say it ONCE (the caller de-dupes through the log;
// Plan itself emits one row per call because one candidate is per tick).
func TestWeightExceedsCapRefusedOnce(t *testing.T) {
	s := baseSnapshot()
	s.Repos[0].Weight = 9
	s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown), readyItem(4, roadmap.PriorityKnown)}
	ds := Plan(s)
	if len(ds) != 1 || ds[0].Verdict != VerdictRefuse || ds[0].Reason != ReasonWeightExceedsCap {
		t.Fatalf("decisions = %v, want exactly one refuse weight-exceeds-cap", fmtDecisions(ds))
	}
	if ds[0].Detail == "" {
		t.Fatal("the refusal must name the setting")
	}
}

// TestRateBackstop is CN12: the Nth+1 start inside an hour defers, and
// the sentence names the instant the oldest start leaves the window —
// the scheduler never sleeps on it, it just says when the road clears.
func TestRateBackstop(t *testing.T) {
	s := baseSnapshot()
	s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
	oldest := s.Now.Add(-55 * time.Minute)
	for i := 0; i < s.Settings.MaxStartsPerRootPerHour; i++ {
		s.Repos[0].StartsInHour = append(s.Repos[0].StartsInHour, oldest.Add(time.Duration(i)*time.Minute))
	}
	ds := Plan(s)
	if len(ds) != 1 || ds[0].Reason != ReasonRateLimit || ds[0].Verdict != VerdictDefer {
		t.Fatalf("decisions = %v, want one defer rate-limit", fmtDecisions(ds))
	}
	want := oldest.Add(time.Hour).UTC().Format(time.RFC3339)
	if !contains(ds[0].Detail, want) {
		t.Fatalf("Detail %q must name the leave-instant %s", ds[0].Detail, want)
	}
}

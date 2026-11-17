package continuous

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/cjlapao/aeon/internal/roadmap"
)

// TestPreviewIsPlan is CN5's promise as an executable sentence: the
// preview IS the tick. WouldStart must equal Plan's starts over the same
// snapshot — not paraphrase them, not approximate them — and every unit
// the snapshot called ready that did not start must have a failing
// reason, drawn from the same decisions, so scheduler.preview can answer
// "why not this one?" without a second code path that could drift.
func TestPreviewIsPlan(t *testing.T) {
	// Curated worlds first (the truth table's shapes), then the property
	// generator's thousand-plus.
	t.Run("curated", func(t *testing.T) {
		s := baseSnapshot()
		s.Settings.MaxSlots = 2
		s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
		s.Repos = append(s.Repos, Repo{Root: "/srv/beta", RootID: 2, Enabled: true,
			Template: "t", TemplateOK: true, MinPriority: roadmap.PriorityAssumed,
			Weight: 1, BudgetOK: true,
			Ready: []RoadmapUnit{readyItem(11, roadmap.PriorityAssumed), readyItem(12, roadmap.PriorityAssumed)}})
		r := Preview(s)
		if len(r.WouldStart) != 2 {
			t.Fatalf("WouldStart = %v, want alpha/3 and beta/11", fmtDecisions(r.WouldStart))
		}
		// beta's OTHER eligible unit never reached a line of its own (CN6
		// picked 11); its first failing reason names the winner, in CN8's
		// own reason id.
		d, ok := r.Failing("/srv/beta", Subject{Kind: "roadmap", ID: "12"})
		if !ok || d.Reason != ReasonRepoOccupied {
			t.Fatalf("beta/12 failing = %+v (%v), want repo-occupied", d, ok)
		}
		if _, bad := r.Failing("/srv/beta", Subject{Kind: "roadmap", ID: "11"}); bad {
			t.Fatal("the started unit carries a failing reason")
		}
	})

	rng := rand.New(rand.NewSource(20261109))
	for i := 0; i < propertyRuns; i++ {
		s := genSnapshot(rng)
		p := Plan(s)
		r := Preview(s)
		var planStarts []Decision
		for _, d := range p {
			if d.Verdict == VerdictStart {
				planStarts = append(planStarts, d)
			}
		}
		if mustJSON(t, r.WouldStart) != mustJSON(t, planStarts) {
			t.Fatalf("snapshot %d: the preview started %v, the tick starts %v — they are the same function, so this is a lie",
				i, fmtDecisions(r.WouldStart), fmtDecisions(planStarts))
		}
		started := map[string]bool{}
		for _, d := range r.WouldStart {
			started[d.Root+"|"+string(d.Subject.Key())] = true
		}
		for _, repo := range s.Repos {
			if repo.QueueHead != nil {
				sub := Subject{Kind: "queue", ID: fmt.Sprintf("%d", repo.QueueHead.ID)}
				if started[repo.Root+"|"+string(sub.Key())] {
					if _, bad := r.Failing(repo.Root, sub); bad {
						t.Fatalf("snapshot %d: started queue %s also carries a failing reason", i, sub)
					}
				} else if _, ok := r.Failing(repo.Root, sub); !ok {
					t.Fatalf("snapshot %d: ready queue head %s/%d has NO failing reason", i, repo.Root, repo.QueueHead.ID)
				}
			}
			for _, u := range repo.Ready {
				sub := Subject{Kind: "roadmap", ID: fmt.Sprintf("%d", u.ID)}
				if started[repo.Root+"|"+string(sub.Key())] {
					continue
				}
				d, ok := r.Failing(repo.Root, sub)
				if !ok {
					t.Fatalf("snapshot %d: ready roadmap item %s has NO failing reason — the preview would answer silence", i, sub)
				}
				if d.Reason == "" && d.Verdict == VerdictStart {
					t.Fatalf("snapshot %d: %s both started and failed", i, sub)
				}
			}
		}
	}
}

// TestSentenceCarriesTheDetail is the legibility clause CN15's table
// implies: the door renders Reason+Detail into the sentence a person (or
// the audit) reads, and the ids it must carry — the next open instant,
// the loop slug, the blocking item — survive into the string.
func TestSentenceCarriesTheDetail(t *testing.T) {
	cases := []struct {
		reason Reason
		detail string
		must   string
	}{
		{ReasonWindowClosed, "2026-11-06T09:00:00Z", "2026-11-06T09:00:00Z"},
		{ReasonRepoOccupied, "loop-9 (state running)", "loop-9"},
		{ReasonDependencyOpen, "item 4", "item 4"},
		{ReasonDependencyCancelled, "item 9", "item 9"},
		{ReasonRateLimit, "2026-11-05T13:10:00Z", "13:10:00Z"},
	}
	for _, c := range cases {
		s := Sentence(c.reason, c.detail)
		if s == "" || !contains(s, c.must) {
			t.Errorf("Sentence(%s, %q) = %q — must carry %q", c.reason, c.detail, s, c.must)
		}
	}
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

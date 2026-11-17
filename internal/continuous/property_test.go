package continuous

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/roadmap"
)

// genSnapshot builds one random-but-reproducible world. The seed lives in
// the test, never in the package (the Do-NOT law: Plan reads no random
// source); every property below drives 1,200 of these.
func genSnapshot(rng *rand.Rand) Snapshot {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s := Snapshot{
		Now:      now,
		Master:   Master{Enabled: rng.Intn(10) > 1, Paused: rng.Intn(10) > 8},
		Settings: Settings{MaxSlots: 1 + rng.Intn(6), MaxStartsPerRootPerHour: 1 + rng.Intn(6), AgingHours: 24},
		Slots:    Slots{Used: rng.Intn(4)},
	}
	n := 1 + rng.Intn(3)
	roots := []string{"/srv/alpha", "/srv/beta", "/srv/gamma"}
	for i := 0; i < n; i++ {
		r := Repo{
			Root: roots[i], RootID: int64(i + 1),
			Enabled:     rng.Intn(10) > 2,
			Template:    "implement-only",
			TemplateOK:  rng.Intn(10) > 2,
			MinPriority: []roadmap.PriorityState{roadmap.PriorityAssumed, roadmap.PriorityKnown, ""}[rng.Intn(3)],
			Weight:      1 + rng.Intn(5),
			BudgetOK:    rng.Intn(10) > 2,
			GrantAbsent: rng.Intn(10) > 8,
		}
		if r.BudgetOK == false {
			r.BudgetReason = fmt.Sprintf("cap %d reached", rng.Intn(99))
		}
		if rng.Intn(10) > 8 { // sometimes closed: 23:00–23:59 never holds at noon UTC
			r.Window = Window{Start: "23:00", End: "23:59", Zone: "UTC"}
		}
		for j := 0; j < rng.Intn(3); j++ {
			r.Occupying = append(r.Occupying, LoopRef{
				Slug:  fmt.Sprintf("loop-%d-%d", i, j),
				State: []string{"running", "waiting", "blocked", "done", "cancelled"}[rng.Intn(5)],
			})
		}
		if rng.Intn(10) > 5 {
			h := &QueueUnit{ID: int64(100 + i*10 + rng.Intn(9)), Status: "ready"}
			switch rng.Intn(3) {
			case 0:
				h.Attended = ptr(true)
			case 1:
				h.Attended = ptr(false)
			}
			r.QueueHead = h
			r.QueueLive = 1 + rng.Intn(3)
		} else if rng.Intn(4) == 0 {
			r.QueueLive = 1 + rng.Intn(3)
		}
		for j := 0; j < rng.Intn(4); j++ {
			u := RoadmapUnit{
				ID:       int64(i*10 + j + 1),
				Priority: []roadmap.PriorityState{roadmap.PriorityKnown, roadmap.PriorityAssumed, roadmap.PriorityUnknown}[rng.Intn(3)],
				BriefOK:  rng.Intn(10) > 1,
				PlanOK:   rng.Intn(10) > 1,
				Claimed:  rng.Intn(10) > 9,
			}
			if rng.Intn(10) > 9 {
				u.OpenDeps = []Dep{{ID: 500 + int64(j), Status: roadmap.StatusRunning}}
			}
			if rng.Intn(20) > 19 {
				u.OpenDeps = []Dep{{ID: 600 + int64(j), Status: roadmap.StatusCancelled}}
			}
			r.Ready = append(r.Ready, u)
			if rng.Intn(4) == 0 {
				if r.FirstDeferral == nil {
					r.FirstDeferral = map[SubjectKey]time.Time{}
				}
				r.FirstDeferral[Subject{Kind: "roadmap", ID: fmt.Sprintf("%d", u.ID)}.Key()] =
					now.Add(-time.Duration(rng.Intn(72)) * time.Hour)
			}
		}
		for j := 0; j < rng.Intn(9); j++ {
			r.StartsInHour = append(r.StartsInHour, now.Add(-time.Duration(rng.Intn(60))*time.Minute))
		}
		if rng.Intn(3) == 0 {
			r.LastStart = now.Add(-time.Duration(rng.Intn(120)) * time.Hour)
		}
		s.Repos = append(s.Repos, r)
	}
	return s
}

func repoOf(s Snapshot, root string) (Repo, bool) {
	for _, r := range s.Repos {
		if r.Root == root {
			return r, true
		}
	}
	return Repo{}, false
}

const propertyRuns = 1200

// TestUnknownPriorityNeverStarts is the silent-never-run failure S29
// exists to prevent, as a property: across 1,200 random worlds, no unit
// whose priority is unknown ever receives a start — and whenever the core
// actually reaches it (the repository was open for candidacy), it is
// REPORTED with priority-unknown, never skipped in silence.
func TestUnknownPriorityNeverStarts(t *testing.T) {
	rng := rand.New(rand.NewSource(20261105))
	for i := 0; i < propertyRuns; i++ {
		s := genSnapshot(rng)
		ds := Plan(s)
		started := map[string]bool{}
		reported := map[string]Reason{}
		for _, d := range ds {
			if d.Verdict == VerdictStart {
				started[d.Root+"/"+d.Subject.ID] = true
			}
			if d.Subject.Kind == "roadmap" {
				reported[d.Root+"/"+d.Subject.ID] = d.Reason
			}
		}
		for _, r := range s.Repos {
			for _, u := range r.Ready {
				if u.Priority != roadmap.PriorityUnknown {
					continue
				}
				key := r.Root + "/" + fmt.Sprintf("%d", u.ID)
				if started[key] {
					t.Fatalf("snapshot %d STARTED unknown-priority item %s", i, key)
				}
				if reached := repoFullyOpen(s, r); reached {
					if reason, ok := reported[key]; !ok || reason != ReasonPriorityUnknown {
						t.Fatalf("snapshot %d: unknown item %s reached candidacy but was reported %q/%v", i, key, reason, ok)
					}
				}
			}
		}
	}
}

// repoFullyOpen is the generator's mirror of Plan's own path: only here
// does an unknown-priority unit have to appear by name.
func repoFullyOpen(s Snapshot, r Repo) bool {
	return s.Master.Enabled && !s.Master.Paused &&
		r.Enabled && r.QueueHead == nil && r.QueueLive == 0 &&
		len(r.Occupying) == 0 && r.Window.Open()
}

// TestBlockedOrWaitingRepoNeverHasACandidate pins brief §8 across the
// same 1,200 worlds: any running/waiting/blocked occupant — including a
// blocked one, whose item is parked on a person — means no start of any
// kind for that repository this tick.
func TestBlockedOrWaitingRepoNeverHasACandidate(t *testing.T) {
	rng := rand.New(rand.NewSource(20261106))
	for i := 0; i < propertyRuns; i++ {
		s := genSnapshot(rng)
		ds := Plan(s)
		for _, d := range ds {
			if d.Verdict != VerdictStart {
				continue
			}
			r, ok := repoOf(s, d.Root)
			if !ok {
				t.Fatalf("snapshot %d started in unknown repository %s", i, d.Root)
			}
			for _, occ := range r.Occupying {
				switch occ.State {
				case "running", "waiting", "blocked":
					t.Fatalf("snapshot %d: repository %s started while %s (%s) is still going",
						i, d.Root, occ.Slug, occ.State)
				}
			}
		}
	}
}

// TestAtMostOneStartPerRepoPerTick — step 6 — over the same worlds: two
// concurrent starts into one repository is the race the architecture
// forbids; here it is unrepresentable, and the property proves Plan never
// sneaks a second one in through the queue AND roadmap paths at once.
func TestAtMostOneStartPerRepoPerTick(t *testing.T) {
	rng := rand.New(rand.NewSource(20261107))
	for i := 0; i < propertyRuns; i++ {
		s := genSnapshot(rng)
		counts := map[string]int{}
		for _, d := range Plan(s) {
			if d.Verdict == VerdictStart {
				counts[d.Root]++
				if counts[d.Root] > 1 {
					t.Fatalf("snapshot %d started twice into %s", i, d.Root)
				}
			}
		}
	}
}

// TestStartsNeverExceedSlots: admission arithmetic across worlds — the
// starts of one tick never spend more weight than the slots that were
// free.
func TestStartsNeverExceedSlots(t *testing.T) {
	rng := rand.New(rand.NewSource(20261108))
	for i := 0; i < propertyRuns; i++ {
		s := genSnapshot(rng)
		spent := 0
		for _, d := range Plan(s) {
			if d.Verdict == VerdictStart {
				spent += d.Weight
			}
		}
		if spent > 0 && s.Slots.Used+spent > s.Settings.MaxSlots {
			t.Fatalf("snapshot %d admitted weight %d on top of %d used against %d slots",
				i, spent, s.Slots.Used, s.Settings.MaxSlots)
		}
	}
}

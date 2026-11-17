package continuous

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/roadmap"
)

// baseSnapshot is the ordinary world every truth-table row bends exactly
// one part of: master on, one enabled repository with a valid template,
// open window, drain gate satisfied, seams clear, four slots.
func baseSnapshot() Snapshot {
	return Snapshot{
		Now:      time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC), // Thursday
		Master:   Master{Enabled: true},
		Settings: Settings{MaxSlots: 4, MaxStartsPerRootPerHour: 6, AgingHours: 24},
		Repos: []Repo{{
			Root: "/srv/alpha", RootID: 1, Enabled: true,
			Template: "implement-only", TemplateOK: true,
			MinPriority: roadmap.PriorityAssumed, Weight: 1,
			BudgetOK: true,
		}},
	}
}

func ptr(b bool) *bool { return &b }

func readyQueueHead(id int64) *QueueUnit {
	return &QueueUnit{ID: id, Status: "ready"}
}

func readyItem(id int64, p roadmap.PriorityState) RoadmapUnit {
	return RoadmapUnit{ID: id, Priority: p, BriefOK: true, PlanOK: true}
}

// want is one expected decision: the repository, the subject, the verdict,
// the reason id, and (optionally) a substring the Detail must carry — the
// ids/enums that make the line actionable.
type want struct {
	root, kind, id string
	verdict        Verdict
	reason         Reason
	detail         string
}

func w(root, kind, id string, v Verdict, r Reason) want {
	return want{root: root, kind: kind, id: id, verdict: v, reason: r}
}

func (x want) String() string {
	return fmt.Sprintf("%s %s/%s %s/%s", x.root, x.kind, x.id, x.verdict, x.reason)
}

func TestPlanTruthTable(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	thu := "4" // ISO weekday of Now
	rows := []struct {
		name   string
		mutate func(*Snapshot)
		want   []want
	}{
		// ——— step 1: the master switch answers alone ———
		{"master-off", func(s *Snapshot) { s.Master.Enabled = false },
			[]want{w("/srv/alpha", "repository", "/srv/alpha", VerdictSkip, ReasonMasterOff)}},
		{"paused", func(s *Snapshot) { s.Master.Paused = true },
			[]want{w("/srv/alpha", "repository", "/srv/alpha", VerdictSkip, ReasonPaused)}},
		{"master-off outranks everything", func(s *Snapshot) {
			s.Master.Enabled = false
			r := &s.Repos[0]
			r.Occupying = []LoopRef{{Slug: "loop-9", State: "running"}}
			r.QueueHead = readyQueueHead(7)
			r.Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
		}, []want{w("/srv/alpha", "repository", "/srv/alpha", VerdictSkip, ReasonMasterOff)}},
		{"master-off speaks per repository", func(s *Snapshot) {
			s.Master.Paused = true
			s.Repos = append(s.Repos, Repo{Root: "/srv/beta", RootID: 2})
		}, []want{
			w("/srv/alpha", "repository", "/srv/alpha", VerdictSkip, ReasonPaused),
			w("/srv/beta", "repository", "/srv/beta", VerdictSkip, ReasonPaused),
		}},

		// ——— step 2: the repository's own gates, in reading order ———
		{"repo-off before occupancy", func(s *Snapshot) {
			r := &s.Repos[0]
			r.Enabled = false
			r.Occupying = []LoopRef{{Slug: "loop-9", State: "running"}}
			r.Window = Window{Start: "18:00", End: "19:00", Zone: "UTC"}
		}, []want{w("/srv/alpha", "repository", "/srv/alpha", VerdictSkip, ReasonRepoOff)}},
		{"occupied-running names the loop and its state", func(s *Snapshot) {
			s.Repos[0].Occupying = []LoopRef{{Slug: "loop-9", State: "running"}}
			s.Repos[0].QueueHead = readyQueueHead(7)
		}, []want{want{root: "/srv/alpha", kind: "repository", id: "/srv/alpha",
			verdict: VerdictDefer, reason: ReasonRepoOccupied, detail: "loop-9 (state running)"}}},
		{"occupied-blocked is occupied (brief §8)", func(s *Snapshot) {
			s.Repos[0].Occupying = []LoopRef{{Slug: "loop-3", State: "blocked"}}
			s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
		}, []want{w("/srv/alpha", "repository", "/srv/alpha", VerdictDefer, ReasonRepoOccupied)}},
		{"occupied-waiting is occupied", func(s *Snapshot) {
			s.Repos[0].Occupying = []LoopRef{{Slug: "loop-3", State: "waiting"}}
		}, []want{w("/srv/alpha", "repository", "/srv/alpha", VerdictDefer, ReasonRepoOccupied)}},
		{"a done occupant does not occupy", func(s *Snapshot) {
			r := &s.Repos[0]
			r.Occupying = []LoopRef{{Slug: "loop-1", State: "done"}, {Slug: "loop-2", State: "cancelled"}}
			r.QueueHead = readyQueueHead(7)
		}, []want{w("/srv/alpha", "queue", "7", VerdictStart, "")}},
		{"window-closed before the candidate, and names the next open instant", func(s *Snapshot) {
			r := &s.Repos[0]
			r.Window = Window{Start: "14:00", End: "18:00", Days: thu, Zone: "UTC"}
			r.QueueHead = readyQueueHead(7)
		}, []want{want{root: "/srv/alpha", kind: "repository", id: "/srv/alpha",
			verdict: VerdictDefer, reason: ReasonWindowClosed, detail: "2026-11-05T14:00:00Z"}}},
		{"an open window says nothing", func(s *Snapshot) {
			s.Repos[0].Window = Window{Start: "09:00", End: "18:00", Days: thu, Zone: "UTC"}
		}, nil},

		// ——— step 3: the queue's claim ———
		{"unattended head starts", func(s *Snapshot) { s.Repos[0].QueueHead = readyQueueHead(7) },
			[]want{w("/srv/alpha", "queue", "7", VerdictStart, "")}},
		{"attended head is skipped, never started", func(s *Snapshot) {
			h := readyQueueHead(7)
			h.Attended = ptr(true)
			s.Repos[0].QueueHead = h
		}, []want{want{root: "/srv/alpha", kind: "queue", id: "7",
			verdict: VerdictSkip, reason: ReasonAttendedItem, detail: "queue item 7"}}},
		{"attended NULL and false start (NULL is never-declared, not attended)", func(s *Snapshot) {
			h := readyQueueHead(7) // Attended: nil
			s.Repos[0].QueueHead = h
		}, []want{w("/srv/alpha", "queue", "7", VerdictStart, "")}},
		{"attended head hides no roadmap candidate", func(s *Snapshot) {
			r := &s.Repos[0]
			h := readyQueueHead(7)
			h.Attended = ptr(true)
			r.QueueHead = h
			r.QueueLive = 1
			r.Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
		}, []want{
			w("/srv/alpha", "queue", "7", VerdictSkip, ReasonAttendedItem),
			w("/srv/alpha", "repository", "/srv/alpha", VerdictDefer, ReasonQueueLive),
		}},
		{"a lying (non-ready) head is refused, not started", func(s *Snapshot) {
			s.Repos[0].QueueHead = &QueueUnit{ID: 7, Status: "running"}
		}, []want{w("/srv/alpha", "queue", "7", VerdictSkip, ReasonNotReady)}},
		{"a spent budget defers the queue head", func(s *Snapshot) {
			r := &s.Repos[0]
			r.QueueHead = readyQueueHead(7)
			r.BudgetOK = false
			r.BudgetReason = "daily cap reached"
		}, []want{want{root: "/srv/alpha", kind: "queue", id: "7",
			verdict: VerdictDefer, reason: ReasonBudgetExhausted, detail: "daily cap reached"}}},
		{"an absent grant defers the queue head", func(s *Snapshot) {
			r := &s.Repos[0]
			r.QueueHead = readyQueueHead(7)
			r.GrantAbsent = true
		}, []want{want{root: "/srv/alpha", kind: "queue", id: "7",
			verdict: VerdictDefer, reason: ReasonGrantAbsent, detail: "/srv/alpha"}}},

		// ——— step 3: the drain gate and the roadmap's eligibility ———
		{"live rows hold roadmap out", func(s *Snapshot) {
			r := &s.Repos[0]
			r.QueueLive = 2
			r.Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
		}, []want{want{root: "/srv/alpha", kind: "repository", id: "/srv/alpha",
			verdict: VerdictDefer, reason: ReasonQueueLive, detail: "2 live"}}},
		{"claimed is said before anything else", func(s *Snapshot) {
			u := readyItem(3, roadmap.PriorityUnknown)
			u.Claimed = true
			s.Repos[0].Ready = []RoadmapUnit{u}
		}, []want{w("/srv/alpha", "roadmap", "3", VerdictSkip, ReasonAlreadyClaimed)}},
		{"unknown priority never runs silently", func(s *Snapshot) {
			s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityUnknown)}
		}, []want{w("/srv/alpha", "roadmap", "3", VerdictSkip, ReasonPriorityUnknown)}},
		{"a cancelled dependency is its own sentence", func(s *Snapshot) {
			u := readyItem(3, roadmap.PriorityKnown)
			u.OpenDeps = []Dep{{ID: 9, Status: roadmap.StatusCancelled}}
			s.Repos[0].Ready = []RoadmapUnit{u}
		}, []want{want{root: "/srv/alpha", kind: "roadmap", id: "3",
			verdict: VerdictSkip, reason: ReasonDependencyCancelled, detail: "item 9"}}},
		{"cancelled outranks open", func(s *Snapshot) {
			u := readyItem(3, roadmap.PriorityKnown)
			u.OpenDeps = []Dep{{ID: 4, Status: roadmap.StatusRunning}, {ID: 9, Status: roadmap.StatusCancelled}}
			s.Repos[0].Ready = []RoadmapUnit{u}
		}, []want{want{root: "/srv/alpha", kind: "roadmap", id: "3",
			verdict: VerdictSkip, reason: ReasonDependencyCancelled, detail: "item 9"}}},
		{"an open dependency waits by id", func(s *Snapshot) {
			u := readyItem(3, roadmap.PriorityKnown)
			u.OpenDeps = []Dep{{ID: 4, Status: roadmap.StatusRunning}}
			s.Repos[0].Ready = []RoadmapUnit{u}
		}, []want{want{root: "/srv/alpha", kind: "roadmap", id: "3",
			verdict: VerdictSkip, reason: ReasonDependencyOpen, detail: "item 4"}}},
		{"missing facts are named", func(s *Snapshot) {
			u := readyItem(3, roadmap.PriorityKnown)
			u.BriefOK = false
			s.Repos[0].Ready = []RoadmapUnit{u}
		}, []want{want{root: "/srv/alpha", kind: "roadmap", id: "3",
			verdict: VerdictSkip, reason: ReasonNotReady, detail: "brief_path unset"}}},
		{"a missing plan is the other fact", func(s *Snapshot) {
			u := readyItem(3, roadmap.PriorityKnown)
			u.PlanOK = false
			s.Repos[0].Ready = []RoadmapUnit{u}
		}, []want{want{root: "/srv/alpha", kind: "roadmap", id: "3",
			verdict: VerdictSkip, reason: ReasonNotReady, detail: "plan_path unset"}}},
		{"minPriority known refuses an assumed item (OD4)", func(s *Snapshot) {
			r := &s.Repos[0]
			r.MinPriority = roadmap.PriorityKnown
			r.Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityAssumed)}
		}, []want{want{root: "/srv/alpha", kind: "roadmap", id: "3",
			verdict: VerdictSkip, reason: ReasonNotReady, detail: "below this repository's minPriority"}}},
		{"minPriority assumed admits assumed (the default)", func(s *Snapshot) {
			s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityAssumed)}
		}, []want{w("/srv/alpha", "roadmap", "3", VerdictStart, "")}},
		{"known outranks assumed for the one candidate (CN6.3)", func(s *Snapshot) {
			s.Repos[0].Ready = []RoadmapUnit{
				readyItem(3, roadmap.PriorityAssumed),
				readyItem(9, roadmap.PriorityKnown),
			}
		}, []want{w("/srv/alpha", "roadmap", "9", VerdictStart, "")}},
		{"oldest id wins among equals (CN6.3)", func(s *Snapshot) {
			s.Repos[0].Ready = []RoadmapUnit{
				readyItem(5, roadmap.PriorityAssumed),
				readyItem(3, roadmap.PriorityAssumed),
			}
		}, []want{w("/srv/alpha", "roadmap", "3", VerdictStart, "")}},
		{"a removed template refuses at the pick, item untouched (CN5)", func(s *Snapshot) {
			r := &s.Repos[0]
			r.TemplateOK = false
			r.Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
		}, []want{want{root: "/srv/alpha", kind: "roadmap", id: "3",
			verdict: VerdictRefuse, reason: ReasonTemplateInvalid, detail: `"implement-only"`}}},
		{"the queue head ignores the template (CG3)", func(s *Snapshot) {
			r := &s.Repos[0]
			r.TemplateOK = false
			r.QueueHead = readyQueueHead(7)
		}, []want{w("/srv/alpha", "queue", "7", VerdictStart, "")}},
		{"a spent budget defers the roadmap candidate", func(s *Snapshot) {
			r := &s.Repos[0]
			r.Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
			r.BudgetOK = false
			r.BudgetReason = "daily cap reached"
		}, []want{want{root: "/srv/alpha", kind: "roadmap", id: "3",
			verdict: VerdictDefer, reason: ReasonBudgetExhausted, detail: "daily cap reached"}}},
		{"an absent grant defers the roadmap candidate", func(s *Snapshot) {
			r := &s.Repos[0]
			r.Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
			r.GrantAbsent = true
		}, []want{w("/srv/alpha", "roadmap", "3", VerdictDefer, ReasonGrantAbsent)}},

		// ——— steps 4–5: ordering and admission ———
		{"slots full defers, naming the arithmetic", func(s *Snapshot) {
			s.Slots.Used = 4
			s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
		}, []want{want{root: "/srv/alpha", kind: "roadmap", id: "3",
			verdict: VerdictDefer, reason: ReasonCapacity, detail: "4 in use + 1"}}},
		{"an exact fit starts (Used + Weight ≤ MaxSlots)", func(s *Snapshot) {
			s.Slots.Used = 3
			s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
			s.Settings.MaxSlots = 4
		}, []want{w("/srv/alpha", "roadmap", "3", VerdictStart, "")}},
		{"weight over the whole cap is refused, naming the setting", func(s *Snapshot) {
			r := &s.Repos[0]
			r.Weight = 5
			r.Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
		}, []want{want{root: "/srv/alpha", kind: "roadmap", id: "3",
			verdict: VerdictRefuse, reason: ReasonWeightExceedsCap, detail: "max_slots 4"}}},
		{"the rate backstop defers with the leave-instant", func(s *Snapshot) {
			r := &s.Repos[0]
			r.Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
			r.StartsInHour = []time.Time{now.Add(-50 * time.Minute)}
			for i := 0; i < 5; i++ {
				r.StartsInHour = append(r.StartsInHour, now.Add(-time.Duration(i)*time.Minute))
			}
		}, []want{want{root: "/srv/alpha", kind: "roadmap", id: "3",
			verdict: VerdictDefer, reason: ReasonRateLimit, detail: "2026-11-05T12:10:00Z"}}},
		{"one start under the rate limit starts", func(s *Snapshot) {
			r := &s.Repos[0]
			r.Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityKnown)}
			r.StartsInHour = r.StartsInHour[:0]
			for i := 0; i < 5; i++ { // five < six
				r.StartsInHour = append(r.StartsInHour, now.Add(-time.Duration(i)*time.Minute))
			}
		}, []want{w("/srv/alpha", "roadmap", "3", VerdictStart, "")}},
		{"capacity admits in order: aged first (CN11)", func(s *Snapshot) {
			s.Settings.MaxSlots = 1
			s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityAssumed)}
			s.Repos[0].FirstDeferral = map[SubjectKey]time.Time{
				Subject{Kind: "roadmap", ID: "3"}.Key(): now.Add(-30 * time.Hour), // aged past 24h
			}
			b := Repo{Root: "/srv/beta", RootID: 2, Enabled: true, Template: "t", TemplateOK: true,
				MinPriority: roadmap.PriorityAssumed, Weight: 1, BudgetOK: true,
				Ready: []RoadmapUnit{readyItem(11, roadmap.PriorityKnown)}, LastStart: now.Add(-time.Hour)}
			s.Repos = append(s.Repos, b)
		}, []want{
			w("/srv/alpha", "roadmap", "3", VerdictStart, ""),
			w("/srv/beta", "roadmap", "11", VerdictDefer, ReasonCapacity),
		}},
		{"a deferral younger than agingHours does not jump the queue", func(s *Snapshot) {
			s.Settings.MaxSlots = 1
			s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityAssumed)}
			s.Repos[0].FirstDeferral = map[SubjectKey]time.Time{
				Subject{Kind: "roadmap", ID: "3"}.Key(): now.Add(-10 * time.Hour),
			}
			s.Repos = append(s.Repos, Repo{Root: "/srv/beta", RootID: 2, Enabled: true, Template: "t",
				TemplateOK: true, MinPriority: roadmap.PriorityAssumed, Weight: 1, BudgetOK: true,
				Ready: []RoadmapUnit{readyItem(11, roadmap.PriorityKnown)}})
			s.Repos[0].LastStart = now // not aged, and it just started
		}, []want{
			w("/srv/beta", "roadmap", "11", VerdictStart, ""),
			w("/srv/alpha", "roadmap", "3", VerdictDefer, ReasonCapacity),
		}},
		{"round-robin: oldest LastStart first among equals", func(s *Snapshot) {
			s.Settings.MaxSlots = 1
			s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityAssumed)}
			s.Repos[0].LastStart = now.Add(-time.Hour)
			s.Repos = append(s.Repos, Repo{Root: "/srv/beta", RootID: 2, Enabled: true, Template: "t",
				TemplateOK: true, MinPriority: roadmap.PriorityAssumed, Weight: 1, BudgetOK: true,
				Ready:     []RoadmapUnit{readyItem(11, roadmap.PriorityAssumed)},
				LastStart: now.Add(-72 * time.Hour)})
		}, []want{
			w("/srv/beta", "roadmap", "11", VerdictStart, ""),
			w("/srv/alpha", "roadmap", "3", VerdictDefer, ReasonCapacity),
		}},
		{"no start ever beats an old start (zero LastStart is oldest)", func(s *Snapshot) {
			s.Settings.MaxSlots = 1
			s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityAssumed)}
			s.Repos[0].LastStart = now.Add(-time.Minute)
			s.Repos = append(s.Repos, Repo{Root: "/srv/beta", RootID: 2, Enabled: true, Template: "t",
				TemplateOK: true, MinPriority: roadmap.PriorityAssumed, Weight: 1, BudgetOK: true,
				Ready: []RoadmapUnit{readyItem(11, roadmap.PriorityAssumed)}})
		}, []want{
			w("/srv/beta", "roadmap", "11", VerdictStart, ""),
			w("/srv/alpha", "roadmap", "3", VerdictDefer, ReasonCapacity),
		}},
		{"root text breaks every tie (total order)", func(s *Snapshot) {
			s.Settings.MaxSlots = 1
			s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityAssumed)}
			s.Repos = append(s.Repos, Repo{Root: "/srv/beta", RootID: 2, Enabled: true, Template: "t",
				TemplateOK: true, MinPriority: roadmap.PriorityAssumed, Weight: 1, BudgetOK: true,
				Ready: []RoadmapUnit{readyItem(11, roadmap.PriorityAssumed)}})
		}, []want{
			w("/srv/alpha", "roadmap", "3", VerdictStart, ""),
			w("/srv/beta", "roadmap", "11", VerdictDefer, ReasonCapacity),
		}},
		{"identical snapshots decide identically", func(s *Snapshot) {
			s.Settings.MaxSlots = 2
			s.Repos[0].Ready = []RoadmapUnit{readyItem(3, roadmap.PriorityAssumed)}
			s.Repos = append(s.Repos,
				Repo{Root: "/srv/beta", RootID: 2, Enabled: true, Template: "t", TemplateOK: true,
					MinPriority: roadmap.PriorityAssumed, Weight: 1, BudgetOK: true,
					Ready: []RoadmapUnit{readyItem(11, roadmap.PriorityAssumed)}},
				Repo{Root: "/srv/gamma", RootID: 3, Enabled: false},
			)
			first := mustJSON(t, Plan(*s))
			for i := 0; i < 20; i++ {
				if got := mustJSON(t, Plan(*s)); got != first {
					t.Fatalf("run %d decided differently:\n %s\n %s", i, first, got)
				}
			}
		}, []want{
			w("/srv/gamma", "repository", "/srv/gamma", VerdictSkip, ReasonRepoOff),
			w("/srv/alpha", "roadmap", "3", VerdictStart, ""),
			w("/srv/beta", "roadmap", "11", VerdictStart, ""),
		}},

		// ——— silence, which is also a rule ———
		{"an idle repository decides nothing", func(s *Snapshot) {}, nil},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			s := baseSnapshot()
			row.mutate(&s)
			got := Plan(s)
			if len(got) != len(row.want) {
				t.Fatalf("got %d decisions, want %d:\n got %v\nwant %v", len(got), len(row.want), fmtDecisions(got), row.want)
			}
			for i, x := range row.want {
				d := got[i]
				if d.Root != x.root || d.Subject.Kind != x.kind || d.Subject.ID != x.id ||
					d.Verdict != x.verdict || d.Reason != x.reason {
					t.Fatalf("decision %d: got %s/%s %s/%s (%s), want %s",
						i, d.Root, d.Subject, d.Verdict, d.Reason, d.Detail, x)
				}
				if x.detail != "" && !strings.Contains(d.Detail, x.detail) {
					t.Errorf("decision %d: Detail %q lacks %q", i, d.Detail, x.detail)
				}
			}
		})
	}
}

func fmtDecisions(ds []Decision) string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, fmt.Sprintf("%s %s/%s %s/%s{%s}", d.Root, d.Subject.Kind, d.Subject.ID, d.Verdict, d.Reason, d.Detail))
	}
	return strings.Join(out, "\n     ")
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

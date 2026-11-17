package continuous

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/trigger"
)

// Plan is the whole scheduler as a pure function (CN1): the snapshot in,
// the decisions a tick would make out — in exactly the order the design
// enumerates (plan.md T4's steps 1–6), so a reader can hold one rule and
// one line at a time.
//
// Determinism is a contract, not a hope: identical snapshots produce
// identical decision lists, byte for byte — no map iteration reaches the
// output, every ordering is explicit. The tick and the preview call THIS
// function; nothing else may decide (CN15).
func Plan(s Snapshot) []Decision {
	var out []Decision

	// Step 1 — the master switch. Off or paused is the whole answer: the
	// rules below are not evaluated, not because they were consulted and
	// lost, but because a paused switch is a paused switch.
	if !s.Master.Enabled || s.Master.Paused {
		r := ReasonMasterOff
		if s.Master.Enabled {
			r = ReasonPaused
		}
		for _, repo := range s.Repos {
			out = append(out, Decision{
				Root:    repo.Root,
				Subject: repositorySubject(repo),
				Verdict: VerdictSkip,
				Reason:  r,
				Detail:  repo.Root,
			})
		}
		return out
	}

	var candidates []candidate

	for _, repo := range s.Repos {
		// Step 2 — the repository's own gates, in the order the person
		// standing at the board would read them.
		if !repo.Enabled {
			out = append(out, Decision{
				Root: repo.Root, Subject: repositorySubject(repo),
				Verdict: VerdictSkip, Reason: ReasonRepoOff, Detail: repo.Root,
			})
			continue
		}
		if occ, ok := occupyingLoop(repo); ok {
			out = append(out, Decision{
				Root: repo.Root, Subject: repositorySubject(repo),
				Verdict: VerdictDefer, Reason: ReasonRepoOccupied, Weight: repo.Weight,
				Detail: fmt.Sprintf("%s (state %s)", occ.Slug, occ.State),
			})
			continue
		}
		if open, why := repo.Window.InWindow(s.Now); !open {
			out = append(out, Decision{
				Root: repo.Root, Subject: repositorySubject(repo),
				Verdict: VerdictDefer, Reason: ReasonWindowClosed, Weight: repo.Weight,
				Detail: windowDetail(repo.Window, s.Now, why),
			})
			continue
		}

		// Step 3 — the candidate, at most one (CN6).
		if head := repo.QueueHead; head != nil {
			sub := Subject{Kind: "queue", ID: fmt.Sprintf("%d", head.ID)}
			if head.Status != string(queue.StatusReady) {
				// The adapter reads the head with NextReadyQueueItem, so a
				// non-ready head means the snapshot lied; say so instead of
				// starting a non-ready unit (the Do-NOT law).
				out = append(out, Decision{
					Root: repo.Root, Subject: sub, Weight: repo.Weight,
					Verdict: VerdictSkip, Reason: ReasonNotReady,
					Detail: fmt.Sprintf("queue item %d is %q — the queue head is expected ready", head.ID, head.Status),
				})
				out = append(out, queueLiveRow(repo)...)
				continue
			}
			if head.Attended != nil && *head.Attended {
				out = append(out, Decision{
					Root: repo.Root, Subject: sub, Weight: repo.Weight,
					Verdict: VerdictSkip, Reason: ReasonAttendedItem,
					Detail: fmt.Sprintf("queue item %d", head.ID),
				})
				out = append(out, queueLiveRow(repo)...)
				continue
			}
			if ref := seamsRefusal(repo, sub, head.AutonomyCap); ref != nil {
				out = append(out, *ref)
				out = append(out, queueLiveRow(repo)...)
				continue
			}
			candidates = append(candidates, newCandidate(s, repo, sub, 0))
			continue
		}

		if repo.QueueLive > 0 {
			// Roadmap work waits for a drained repository (CN6.2) — the
			// rows are a human-ordered instruction still in flight.
			out = append(out, Decision{
				Root: repo.Root, Subject: repositorySubject(repo),
				Verdict: VerdictDefer, Reason: ReasonQueueLive, Weight: repo.Weight,
				Detail: fmt.Sprintf("%d live", repo.QueueLive),
			})
			continue
		}

		if len(repo.Ready) > 0 {
			cand, rows := roadmapCandidate(repo, s)
			out = append(out, rows...)
			if cand != nil {
				candidates = append(candidates, *cand)
			}
			continue
		}
		// An enabled, empty, idle repository decides nothing — silence is
		// the honest row here, not a skip per rule that did not apply.
	}

	// Step 4 — CN11's cross-repository order, total, so two runs of one
	// snapshot decide identically.
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.aged != b.aged {
			return a.aged
		}
		if a.aged && !a.deferral.Equal(b.deferral) {
			return a.deferral.Before(b.deferral) // oldest deferral first
		}
		if a.rank != b.rank {
			return a.rank > b.rank // known before assumed
		}
		if !a.repo.LastStart.Equal(b.repo.LastStart) {
			return a.repo.LastStart.Before(b.repo.LastStart) // round-robin: oldest last-start first
		}
		return a.repo.Root < b.repo.Root
	})

	// Step 5 — admission, in that order (CN9 then CN12). A deferred
	// candidate holds no slot: deferral never reserves anything.
	used := s.Slots.Used
	for _, c := range candidates {
		if c.repo.Weight > s.Settings.MaxSlots {
			out = append(out, Decision{
				Root: c.repo.Root, Subject: c.subject, Weight: c.repo.Weight,
				Verdict: VerdictRefuse, Reason: ReasonWeightExceedsCap,
				Detail: fmt.Sprintf("weight %d > max_slots %d", c.repo.Weight, s.Settings.MaxSlots),
			})
			continue
		}
		if used+c.repo.Weight > s.Settings.MaxSlots {
			out = append(out, Decision{
				Root: c.repo.Root, Subject: c.subject, Weight: c.repo.Weight,
				Verdict: VerdictDefer, Reason: ReasonCapacity,
				Detail: fmt.Sprintf("%d in use + %d for this repository", used, c.repo.Weight),
			})
			continue
		}
		if len(c.repo.StartsInHour) >= s.Settings.MaxStartsPerRootPerHour {
			out = append(out, Decision{
				Root: c.repo.Root, Subject: c.subject, Weight: c.repo.Weight,
				Verdict: VerdictDefer, Reason: ReasonRateLimit,
				Detail: rateDetail(c.repo.StartsInHour),
			})
			continue
		}
		out = append(out, Decision{
			Root: c.repo.Root, Subject: c.subject, Weight: c.repo.Weight,
			Verdict: VerdictStart,
			Detail:  startDetail(c),
		})
		used += c.repo.Weight
		// Step 6 is structural: one candidate per repository means one
		// start per repository per tick, and there is no second candidate
		// to consider.
	}
	return out
}

// roadmapCandidate applies CN6.3's order and the eligibility rules to one
// repository's ready items: every unit that fails gets its own skip row
// (the reason a person will read in preview and the audit), and the best
// eligible one becomes the repository's candidate — or a template-invalid
// refusal, because CN5 revalidates the work template AT THE PICK.
func roadmapCandidate(repo Repo, s Snapshot) (*planCandidate, []Decision) {
	var rows []Decision
	var eligible []RoadmapUnit
	for _, u := range repo.Ready {
		sub := Subject{Kind: "roadmap", ID: fmt.Sprintf("%d", u.ID)}
		if v, reason, detail, bad := unitRefusal(repo, u); bad {
			rows = append(rows, Decision{
				Root: repo.Root, Subject: sub, Verdict: v, Reason: reason,
				Detail: detail, Weight: repo.Weight,
			})
			continue
		}
		eligible = append(eligible, u)
	}
	if len(eligible) == 0 {
		return nil, rows
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		ri, rj := priorityRank(eligible[i].Priority), priorityRank(eligible[j].Priority)
		if ri != rj {
			return ri > rj // known before assumed (unknown never got here)
		}
		return eligible[i].ID < eligible[j].ID // then oldest id
	})
	best := eligible[0]
	sub := Subject{Kind: "roadmap", ID: fmt.Sprintf("%d", best.ID)}
	if !repo.TemplateOK {
		rows = append(rows, Decision{
			Root: repo.Root, Subject: sub, Verdict: VerdictRefuse,
			Reason: ReasonTemplateInvalid, Weight: repo.Weight,
			Detail: fmt.Sprintf("blueprint %q", repo.Template),
		})
		return nil, rows
	}
	if ref := seamsRefusal(repo, sub, best.AutonomyCap); ref != nil {
		rows = append(rows, *ref)
		return nil, rows
	}
	c := newCandidate(s, repo, sub, priorityRank(best.Priority))
	return &c, rows
}

// candidate is one repository's admitted-for-ordering candidate: the
// repository, its one candidate subject, and the CN11 ordering facts.
type candidate struct {
	repo     Repo
	subject  Subject
	rank     int // priority state rank for CN11's ordering
	key      SubjectKey
	deferral time.Time
	aged     bool
}

// newCandidate stamps the CN11 aging fact: a subject whose CURRENT run of
// capacity deferrals (the adapter reads them from the decision log) began
// at least agingHours ago goes to the front. Absent, zero, or an aging
// setting of zero means not aged — the clock here is the snapshot's, never
// the wall's.
func newCandidate(s Snapshot, repo Repo, sub Subject, rank int) candidate {
	c := candidate{repo: repo, subject: sub, rank: rank, key: sub.Key()}
	if d, ok := repo.FirstDeferral[c.key]; ok && !d.IsZero() && s.Settings.AgingHours > 0 {
		c.deferral = d
		c.aged = !s.Now.Before(d.Add(time.Duration(s.Settings.AgingHours) * time.Hour))
	}
	return c
}

// planCandidate names the record for roadmapCandidate's signature.
type planCandidate = candidate

// unitRefusal is one ready item's eligibility answer, in the order the
// rules are written in the spec — claimed, priority, dependencies, the
// readiness facts, then the repository's own minimum.
func unitRefusal(repo Repo, u RoadmapUnit) (Verdict, Reason, string, bool) {
	switch {
	case u.Claimed:
		return VerdictSkip, ReasonAlreadyClaimed, fmt.Sprintf("roadmap item %d", u.ID), true
	case u.Priority == roadmap.PriorityUnknown:
		return VerdictSkip, ReasonPriorityUnknown, "", true
	}
	var cancelled, waiting []int64
	for _, d := range u.OpenDeps {
		if d.Status == roadmap.StatusCancelled {
			cancelled = append(cancelled, d.ID)
		} else {
			waiting = append(waiting, d.ID)
		}
	}
	if len(cancelled) > 0 {
		return VerdictSkip, ReasonDependencyCancelled, subjectIDs("item ", cancelled), true
	}
	if len(waiting) > 0 {
		return VerdictSkip, ReasonDependencyOpen, subjectIDs("item ", waiting), true
	}
	var missing []string
	if !u.BriefOK {
		missing = append(missing, "brief_path unset")
	}
	if !u.PlanOK {
		missing = append(missing, "plan_path unset")
	}
	if len(missing) > 0 {
		return VerdictSkip, ReasonNotReady, strings.Join(missing, "; "), true
	}
	if priorityRank(u.Priority) < priorityRank(repo.MinPriority) {
		return VerdictSkip, ReasonNotReady, fmt.Sprintf(
			"priority %s is below this repository's minPriority %s", u.Priority, repo.MinPriority), true
	}
	return "", "", "", false
}

// seamsRefusal carries the two daemon-filled facts (CN24) at the place
// the seams answer: a spent budget defers the SAME candidate a person
// would have to watch; an absent grant says so out loud. They are checked
// before a candidate is admitted — a candidate the seams refuse never
// enters the capacity order.
func seamsRefusal(repo Repo, sub Subject, cap string) *Decision {
	if !repo.BudgetOK {
		detail := repo.BudgetReason
		if detail == "" {
			detail = "the budget reported exhaustion without a reason"
		}
		return &Decision{Root: repo.Root, Subject: sub, Verdict: VerdictDefer,
			Reason: ReasonBudgetExhausted, Detail: detail, Weight: repo.Weight}
	}
	if repo.GrantAbsent {
		return &Decision{Root: repo.Root, Subject: sub, Verdict: VerdictDefer,
			Reason: ReasonGrantAbsent, Detail: fmt.Sprintf("repository %s", repo.Root), Weight: repo.Weight}
	}
	return nil
}

// queueLiveRow states what a skipped head never hides: while live queue
// rows exist, the repository's roadmap items are not candidates (CN6.2) —
// said at the repository level, so every ready unit behind the head
// inherits exactly this reason in the preview.
func queueLiveRow(repo Repo) []Decision {
	if repo.QueueLive <= 0 || len(repo.Ready) == 0 {
		return nil
	}
	return []Decision{{
		Root: repo.Root, Subject: repositorySubject(repo),
		Verdict: VerdictDefer, Reason: ReasonQueueLive, Weight: repo.Weight,
		Detail: fmt.Sprintf("%d live", repo.QueueLive),
	}}
}

// occupyingLoop is CN8 as one line: the repository is occupied while ANY
// loop on it is still going — running, waiting or blocked, which includes
// the cap-hit and budget-parked states a person has not raised. A BLOCKED
// occupant counts exactly like a running one: restarting into a blocked
// item is brief §8's forbidden failure.
func occupyingLoop(repo Repo) (LoopRef, bool) {
	for _, l := range repo.Occupying {
		if trigger.StillGoing(l.State) {
			return l, true
		}
	}
	return LoopRef{}, false
}

// priorityRank is the CLOSED ordering over the priority states: known
// before assumed, and unknown is below both because it never reaches the
// candidate stage at all (D2). No label, no text — the states only.
func priorityRank(p roadmap.PriorityState) int {
	switch p {
	case roadmap.PriorityKnown:
		return 2
	case roadmap.PriorityAssumed:
		return 1
	default:
		return 0
	}
}

func repositorySubject(repo Repo) Subject {
	return Subject{Kind: "repository", ID: repo.Root}
}

// rateDetail names the instant the oldest start leaves the sliding hour —
// the exact moment CN12 says worth another look.
func rateDetail(startsInHour []time.Time) string {
	if len(startsInHour) == 0 {
		return "now"
	}
	return startsInHour[0].Add(time.Hour).UTC().Format(time.RFC3339)
}

// startDetail keeps the audit's started-row summary free of any later
// re-derivation: the weights it admitted with, stated.
func startDetail(c planCandidate) string {
	s := fmt.Sprintf("weight %d", c.repo.Weight)
	if c.aged {
		s += ", aged (CN11)"
	}
	if c.subject.Kind == "roadmap" {
		s += ", template " + c.repo.Template
	}
	return s
}

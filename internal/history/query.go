package history

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// Kind tags an entry as a live loop or a cold-stored (archived) loop. The
// caller reads a merged sequence and never has to know which table each row
// came from (D7): the tag is on the entry, not a second screen a person has
// to remember to check.
type Kind int

const (
	// KindLive is a loop still in the live tables.
	KindLive Kind = iota
	// KindArchived is a loop moved to cold storage — GONE from the live
	// tables (archive.go), present here only because D7 says history does not
	// stop at the archive boundary.
	KindArchived
)

func (k Kind) String() string {
	if k == KindArchived {
		return "archived"
	}
	return "live"
}

// Entry is one row of the time-ordered projection: either a live loop or an
// archived loop, tagged, with the headline facts, the floored cost (H6) and
// the stored reason for a blocked loop (D4/H4).
//
// The fields are the union of a live loop's and an archive header's headline
// facts — slug, title, goal, status, started/ended — so a viewer can render
// a merged sequence without branching on Kind for the data, only for the
// tag. `Cost` is the P32 floor for this one loop; `Reason` is the stored
// reason for a blocked loop, verbatim.
type Entry struct {
	Kind          Kind
	ID            string // loop.id (live) or archive.id (archived)
	Slug          string
	ComponentRoot string // the loop's component root; the live drill-down builds its Scope from it (T2)
	Title         string
	Goal          string
	Status        string
	StartedAt     string
	EndedAt       string
	// Timeline is the sort key on the merged axis: updated_at for a live loop
	// (its last activity), ended_at for an archived one — the loop's OWN time,
	// not when it was archived, so an archived loop is placed by when it ran
	// (DEMO step 1's prediction), never pinned to the end.
	Timeline string
	// Cost is the floored rollup for this one loop (H6): a floor, never a bare
	// total, for the same reason the per-loop view's is (S9).
	Cost Rollup
	// Reason is the stored reason for a blocked loop (D4/H4), read from the
	// event stream verbatim — never a synthesized verdict. "" when the loop is
	// not blocked or the reason was never recorded (an honest empty, not a
	// fabricated "failed").
	Reason string
	// S53 T1 (HV2), additive: the link carriers Describe renders as data. A
	// live loop's GoalID/NowTask are the row's own coordinates (loops.go: GoalID
	// is S38's pinned goal, NowTask the task it is working); RoadmapID is the
	// roadmap item materialized to THIS loop (roadmap_item.loop_slug, S26's
	// pair). Every one is "" when the entry does not carry it — an archived
	// header names no task, no goal, no roadmap item, so it carries none, and
	// Describe emits no link for an id nobody has.
	GoalID    string
	NowTask   string
	RoadmapID string
}

// Options narrows the projection.
type Options struct {
	// Scope is the component scope. An empty ComponentRoot spans every
	// component (the same `--all` the loop list names), a separate, explicit
	// ask rather than the default — a daemon multiplexes every component on the
	// machine and an unscoped default would show a person every repository's
	// history.
	Scope store.ComponentScope
	// Since is an RFC3339 lower bound on the timeline (an entry whose timeline
	// is before it is omitted). "" means no lower bound. A date range is a
	// CLI-ergonomics call (design §7), not a modelling question.
	Since string
}

// Merge returns the time-ordered projection over every live loop AND every
// archived loop in the scope — D7, one sequence, not two screens a person has
// to remember to check both of (R1's guard, read literally).
//
// The sequence is ordered by the loop's OWN timeline (see Entry.Timeline):
// updated_at for a live loop, ended_at for an archived one. A stable sort
// keeps a re-run of the same fixture identical.
func Merge(s *store.Store, opts Options) ([]Entry, error) {
	live, err := s.ListLoops(opts.Scope)
	if err != nil {
		return nil, err
	}
	archives, err := s.ListArchives(opts.Scope)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(live)+len(archives))

	for _, l := range live {
		cost, err := s.LoopCost(l.ID)
		if err != nil {
			// A cost read failure is a floor, not a total — the honest answer to
			// "I cannot read this loop's cost" is unknown, and an unknown floors
			// (P32), it does not become a zero a person would believe.
			cost = store.RunCost{USDAtLeast: 0, Exact: false, Partial: 0, Unknown: 1, Steps: 1}
		}
		entries = append(entries, Entry{
			Kind:          KindLive,
			ID:            l.ID,
			Slug:          l.Slug,
			ComponentRoot: l.ComponentRoot,
			Title:         l.Title,
			Goal:          l.Goal,
			Status:        l.Status,
			StartedAt:     l.StartedAt,
			Timeline:      l.UpdatedAt, // a live loop's timeline is its last activity
			Cost:          toRollup(cost),
			Reason:        blockedReason(s, l),
			GoalID:        l.GoalID,
			NowTask:       l.NowTask,
		})
	}

	for _, a := range archives {
		// An archived loop's header carries no step costs, so its cost is
		// honestly unknown — a floor that names the unknown, never a zero
		// (P32). Recorded in the Notes: the archive preserves the header and
		// the document files, not the step costs, so reading them back is a
		// second, explicit action (Q3), not the list's job.
		timeline := a.EndedAt
		if timeline == "" {
			timeline = a.StartedAt
		}
		entries = append(entries, Entry{
			Kind:          KindArchived,
			ID:            a.ID,
			Slug:          a.Slug,
			ComponentRoot: a.ComponentRoot,
			Title:         a.Title,
			Goal:          a.Goal,
			Status:        a.Status,
			StartedAt:     a.StartedAt,
			EndedAt:       a.EndedAt,
			Timeline:      timeline,
			Cost:          Rollup{Floor: true, UnknownCount: 1, StepCount: 1},
			// An archived loop's reason lives in the cold-stored state, not a
			// column the list reads (Q3); "" is the honest "read it on
			// drill-down," not a fabricated verdict.
			Reason: "",
		})
	}

	// The time-ordered sequence: by the loop's own timeline (see Entry.
	// Timeline), descending, stable so a re-run of the same fixture is
	// identical.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Timeline > entries[j].Timeline
	})

	if opts.Since != "" {
		since, err := parseSince(opts.Since)
		if err != nil {
			return nil, err
		}
		kept := entries[:0]
		for _, e := range entries {
			if at, ok := parseTime(e.Timeline); ok && !at.Before(since) {
				kept = append(kept, e)
			}
		}
		entries = kept
	}
	return entries, nil
}

// parseTime parses an RFC3339 timeline for the --since filter; a malformed or
// empty one is not a match, which keeps a bad value from widening the filter
// silently.
func parseTime(t string) (time.Time, bool) {
	if t == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, t)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// parseSince turns the CLI's --since into a time. A malformed value is a
// refusal by name, not a silent "no filter" (DB19: a bad value is a refused
// value that names itself).
func parseSince(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// blockedReason is the stored reason for a blocked live loop (D4/H4): a
// blocked loop shows its stored reason verbatim, never a synthesized verdict.
// The loop row carries no reason column; the reason is the event the runtime
// recorded when the loop could not finish — a `loop.blocked` event carrying
// `{"reason": …}` — read from the event stream, the source design.md names.
//
// It is READ, not invented: a blocked loop with no recorded reason returns
// "", which renders as an honest "a blocked loop with no recorded reason"
// rather than a fabricated "failed." An archived loop's reason is not read
// here (it lives in the cold-stored state — Q3), so the list never pretends
// to know it.
func blockedReason(s *store.Store, l store.Loop) string {
	if l.Status != "blocked" {
		return ""
	}
	events, err := s.ReadEvents(0, store.EventFilter{
		Kinds:         []string{"loop.blocked"},
		ComponentRoot: l.ComponentRoot,
	}, 200)
	if err != nil {
		return ""
	}
	// The latest loop.blocked for THIS loop — the reason for its current
	// blocked status, not an older block it recovered from.
	var latest *store.Event
	for i := range events {
		if events[i].LoopSlug == l.Slug {
			latest = &events[i] // events are in seq order, so the last match wins
		}
	}
	if latest == nil {
		return ""
	}
	var p struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal(latest.Payload, &p) != nil {
		return ""
	}
	return p.Reason
}

// Query is the ONE read both the CLI (aeon history, S25 T3) and the socket
// (history.*, S25 T4) call (H3): the same internal/history read, parameterized
// only by the flags. The CLI and the socket render the same entries through
// the same Row, so a cost or a reason cannot come out differently under the
// socket than under the CLI — that is the second truth H3 forbids, made
// impossible by one query, not by a test that could rot. A non-default
// grouping folds the same entries into lanes (H1), flattened back to the entry
// sequence so a regroup never changes a loop's facts.
func Query(s *store.Store, opts Options, by GroupBy) ([]Entry, error) {
	if !by.Valid() {
		return nil, fmt.Errorf("history: group-by %q is not one of loop, phase, task, agent", by)
	}
	if by == "" || by == ByLoop {
		entries, err := Merge(s, opts)
		if err != nil {
			return nil, err
		}
		return attachRoadmapLinks(s, opts, entries)
	}
	lanes, err := Lanes(s, opts, by)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, l := range lanes {
		entries = append(entries, l.Loops...)
	}
	return attachRoadmapLinks(s, opts, entries)
}

// attachRoadmapLinks fills Entry.RoadmapID for the S53 T1 links (HV2): one
// scoped read of roadmap_item — the table that already names, per item, the
// loop_slug it was materialized to (S26's D6, keyed by the component_root +
// slug pair, DB4) — mapped onto the entries the merge produced. This is link
// DATA beside the merge, never a second merge: the sequence, the facts and the
// order are exactly Merge's; an entry whose slug names no item keeps its
// "" and Describe emits no roadmap link for it. When more than one item points
// at one loop (a re-materialize), the FIRST in the board's own order wins —
// deterministic, never a coin-flip.
func attachRoadmapLinks(s *store.Store, opts Options, entries []Entry) ([]Entry, error) {
	if len(entries) == 0 {
		return entries, nil
	}
	items, err := s.ListRoadmapItems(opts.Scope.ComponentRoot, store.RoadmapItemFilters{})
	if err != nil {
		return nil, err
	}
	byPair := map[string]string{}
	for _, it := range items {
		if it.LoopSlug == nil || *it.LoopSlug == "" {
			continue
		}
		key := it.ComponentRoot + "\x00" + *it.LoopSlug
		if _, seen := byPair[key]; !seen {
			byPair[key] = strconv.FormatInt(it.ID, 10)
		}
	}
	for i := range entries {
		entries[i].RoadmapID = byPair[entries[i].ComponentRoot+"\x00"+entries[i].Slug]
	}
	return entries, nil
}

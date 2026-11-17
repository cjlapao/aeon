package roadmap

// S50 T2 (RM6) — the board read and the history read, both on the door
// side so every projection (socket, CLI, app) answers from ONE assembly.
// A board row is the item, its Describe sentence, and the readiness
// answer ISID — `roadmap.health`-honest: `ready` and `blocker` are the
// bool and the exact sentence `IsReady` produced, never a re-derivation
// with drifted rules.

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// BoardFilter carries the optional narrowing. All zero: every item.
type BoardFilter struct {
	ComponentRoot string
	Status        Status
	Epic          string
}

// BoardLoop is the loop an item rode, carried when the item was
// materialized — slug and status, never a re-narrated loop story.
type BoardLoop struct {
	Slug   string `json:"slug"`
	Status string `json:"status"`
}

// BoardRow is one item as the board shows it.
type BoardRow struct {
	Item
	Describe string     `json:"describe"`
	Ready    bool       `json:"ready"`
	Blocker  string     `json:"blocker,omitempty"`
	Loop     *BoardLoop `json:"loop,omitempty"`
}

// BoardAnswer is the rows plus the group counts — how many items carry
// each epic tag, keyed by the tag (the empty key is the untagged group).
// Counts are over the FILTERED set: the counts describe what is shown.
type BoardAnswer struct {
	Rows   []BoardRow     `json:"rows"`
	Groups map[string]int `json:"groups"`
}

// Board assembles the answer. An unknown status filter is refused naming
// the valid values — a typo'd filter answering "nothing" is a plausible
// wrong answer, and the board exists to answer questions.
func (r *Roadmap) Board(f BoardFilter) (BoardAnswer, *Refusal) {
	if f.Status != "" && !ValidStatus(f.Status) {
		return BoardAnswer{}, &Refusal{Reason: fmt.Sprintf(
			"roadmap.board: status %q is not a status — the eight are %s", f.Status, joinStatuses(Statuses()))}
	}
	items, err := r.List(f.ComponentRoot, ListFilter{Status: f.Status, Epic: f.Epic})
	if err != nil {
		return BoardAnswer{}, &Refusal{Reason: fmt.Sprintf("roadmap.board: %v", err)}
	}
	ans := BoardAnswer{Rows: make([]BoardRow, 0, len(items)), Groups: map[string]int{}}
	for _, it := range items {
		// S27 T3: the board reads the SAME graph the doors do — a blocked
		// item's blocker names the dependency, and the person sees the
		// preview's sentence without opening the item (rule 3: the item is
		// never silently stuck).
		deps, err := r.Dependencies(it.ID)
		if err != nil {
			return BoardAnswer{}, &Refusal{Reason: fmt.Sprintf("roadmap.board: %v", err)}
		}
		ready, why := IsReadyWithDeps(it, deps)
		row := BoardRow{Item: it, Describe: Describe(it), Ready: ready, Blocker: why}
		if it.LoopSlug != "" {
			// The slug is only unique per root (I4), and the item knows
			// its own — take the loop under THIS item's component root.
			loops, err := r.s.LoopsBySlug(it.LoopSlug)
			if err != nil {
				return BoardAnswer{}, &Refusal{Reason: fmt.Sprintf("roadmap.board: %v", err)}
			}
			for _, l := range loops {
				if l.ComponentRoot == it.ComponentRoot {
					row.Loop = &BoardLoop{Slug: l.Slug, Status: l.Status}
					break
				}
			}
			// No loop row under this root with that slug: the pointer is
			// carried WITHOUT the loop block rather than invented — an
			// item whose loop vanished is history, and history says so.
		}
		ans.Rows = append(ans.Rows, row)
		ans.Groups[it.Epic]++
	}
	return ans, nil
}

// History reads the audit trail of one item, newest first (RM2's read of
// the append-only rows; limit <= 0 takes the store's default page).
func (r *Roadmap) History(ctx context.Context, id int64, limit int) ([]store.RoadmapChange, error) {
	return r.s.RoadmapChangeList(ctx, id, limit)
}

// MoveTimes is when the item first started running and when it finished (done, else cancelled), as
// the audit recorded them; "" for a time that has not happened. Read from the append-only
// roadmap_change rows, so it needs no column and cannot drift from the history a person reads.
func (r *Roadmap) MoveTimes(ctx context.Context, id int64) (started, finished string, err error) {
	into := func(to Status) [2]string {
		return [2]string{"moved item %" + moveArrow + string(to), "moved item %" + moveArrow + string(to) + " (%"}
	}
	if started, err = r.s.RoadmapMoveTime(ctx, id, into(StatusRunning), false); err != nil {
		return "", "", err
	}
	if finished, err = r.s.RoadmapMoveTime(ctx, id, into(StatusDone), true); err != nil || finished != "" {
		return started, finished, err
	}
	finished, err = r.s.RoadmapMoveTime(ctx, id, into(StatusCancelled), true)
	return started, finished, err
}

func joinStatuses(ss []Status) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += string(s)
	}
	return out
}

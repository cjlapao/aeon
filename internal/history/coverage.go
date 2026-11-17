package history

// ════════════════════════════════════════════════════════════════════════════
// Coverage (S53 T1, HV1): the honesty read the history view lacked. One
// question — for this scope, what does the merge SHOW, what does it NOT SHOW,
// and what can it NOT SAY — answered by walking the SAME Merge output (no
// second merge implementation, TestNoSecondMergeImplementation pins it) plus
// one decode probe per in-scope archive through the SAME decode code the
// drill-down uses (store.ReadArchive with content).
//
// The probe exists because of the decode-failure behaviour T0 recorded: the
// merge reads ARCHIVE HEADERS only, so a blob that will not decode is silently
// *unprobed* — it shows in every list as if fine, and only its drill-down dies
// (archive.go:205-214). That is P31's third state made invisible: the archive
// is neither shown (its content is unreadable) nor honestly absent. Coverage
// probes and says "cannot say", naming the ids (≤ 20 listed; the count is the
// truth even when the list is capped).
//
// The floor.statement is S25's own P32 sentence, VERBATIM — the constant below
// is what the CLI's usage block prints (cmd/aeon/history.go, the printHistory-
// Usage string), pinned to it by TestFloorStatementIsVerbatim so the two can
// never drift apart.
// ════════════════════════════════════════════════════════════════════════════

import (
	"github.com/cjlapao/aeon/internal/store"
)

// FloorStatement is S25's P32 sentence, verbatim — the statement coverage
// carries so the view states the floor in the slice's own words, never a
// re-phrasing (HV1: "S25's own P32 sentence, verbatim").
const FloorStatement = `A cost is a floor, never a bare number ("at least $X (K of N steps unknown)");`

// Counted is the live or archived side of the scope: how many loops, and the
// span the sequence actually covers (oldest/newest timeline stamps among the
// SHOWN entries — what the view shows, not what the tables merely hold).
type Counted struct {
	Loops  int    `json:"loops"`
	Oldest string `json:"oldest,omitempty"`
	Newest string `json:"newest,omitempty"`
}

// Undecodable is the "cannot say" state (P31's third state, HV1): archives
// whose content probe failed. Count is the truth; IDs lists at most
// UndecodableIDsListed of them, because a view that buried a hundred ids under
// a count would say the honest number and hide the honest name.
type Undecodable struct {
	Count int      `json:"count"`
	IDs   []string `json:"ids"`
}

// UndecodableIDsListed is the cap on listed ids (HV1: ids[≤20]).
const UndecodableIDsListed = 20

// NotShown is one honest "the query does not show this, and here is why"
// (HV1's notShown): a reason word and the count it covers. It never overlaps
// "cannot say" — notShown is known-absent from the view's presentation,
// undecodable is present-but-unprovable.
type NotShown struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// NotShownArchivedSteps is the one reason S25's own shape produces (lanes.go:
// an archived loop stands in no phase/task/agent lane — its steps live cold,
// and the list does not decode them, Q3). It appears when the lane asked for
// is not the loop lane, naming exactly how many archived loops the lane, by
// its nature, leaves out.
const NotShownArchivedSteps = "an archived loop has no live steps — the by-phase/task/agent lanes regroup live steps only (S25 Q3)"

// CoverageView is history.coverage's answer (HV1), the three states made
// explicit: shown (live + archived), cannot-say (undecodable), not-shown
// (notShown).
type CoverageView struct {
	// Scope is the component root asked of; "" is the every-component span
	// (the merge's own `--all` reading, never a silent default).
	Scope       string             `json:"scope"`
	Live        Counted            `json:"live"`
	Archived    Counted            `json:"archived"`
	Undecodable Undecodable        `json:"undecodable"`
	Floor       FloorStatementView `json:"floor"`
	NotShown    []NotShown         `json:"notShown"`
}

// FloorStatementView wraps the sentence so the wire shape leaves room for the
// statement's provenance to grow without a wire break (HV1's floor{statement}).
type FloorStatementView struct {
	Statement string `json:"statement"`
}

// Coverage answers HV1 over the SAME query code: Merge supplies the shown
// sequence and its span (counting off walked entries — no second merge, no SQL
// re-imagination), and one probe per archived entry through the drill-down's
// own decode path supplies the cannot-say state. An early-out variant (skipping
// the per-loop cost reads) is an optimisation for a slow store, not this
// pass's need — the counts must equal the real query's results, and the real
// query is Merge.
func Coverage(s *store.Store, opts Options, by GroupBy) (CoverageView, error) {
	if !by.Valid() {
		return CoverageView{}, errBadGroupBy(by)
	}
	entries, err := Merge(s, opts)
	if err != nil {
		return CoverageView{}, err
	}

	cv := CoverageView{
		Scope:       opts.Scope.ComponentRoot,
		Undecodable: Undecodable{IDs: []string{}},
		Floor:       FloorStatementView{Statement: FloorStatement},
		NotShown:    []NotShown{},
	}

	for _, e := range entries {
		side := &cv.Live
		if e.Kind == KindArchived {
			side = &cv.Archived
		}
		side.Loops++
		// entries are timeline-descending: the first seen is newest, the last
		// is oldest — both from the SHOWN sequence, never a wider table read.
		if side.Newest == "" {
			side.Newest = e.Timeline
		}
		side.Oldest = e.Timeline
	}

	// The cannot-say probe: the SAME ReadArchive the drill-down calls, content
	// inflated. A failure is not "absent" and not "shown fine" — it is named.
	for _, e := range entries {
		if e.Kind != KindArchived {
			continue
		}
		if _, _, err := s.ReadArchive(e.ID, true); err != nil {
			cv.Undecodable.Count++
			if len(cv.Undecodable.IDs) < UndecodableIDsListed {
				cv.Undecodable.IDs = append(cv.Undecodable.IDs, e.ID)
			}
		}
	}

	// The not-shown statement: on a step lane, the archived entries in scope
	// are named as left out by the lane's nature (lanes.go, Q3). On the loop
	// lane the merge shows everything it holds and notShown is honestly empty.
	if by != "" && by != ByLoop && cv.Archived.Loops > 0 {
		cv.NotShown = append(cv.NotShown, NotShown{Reason: NotShownArchivedSteps, Count: cv.Archived.Loops})
	}
	return cv, nil
}

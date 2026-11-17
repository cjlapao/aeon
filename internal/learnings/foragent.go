package learnings

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// forAgentLimitDefault and forAgentLimitMax are LM5's answer-size rules,
// named constants because learnings.vocabulary answers them and the app
// fills its control from that answer — a limit typed twice is a limit that
// drifts.
const (
	ForAgentLimitDefault = 20
	ForAgentLimitMax     = 100
)

// ForAgentWhy is the `why` LM5 demands beside every hit: what rank it came
// in at, the ordering law the ranking followed, and what the match is
// based on — the honest basis, including "no embedding was even attempted"
// (D5), which a score without a basis would hide.
type ForAgentWhy struct {
	Rank   int
	M1Note string
	Basis  string
}

// ForAgentHit is one learning the agent would be shown, beside its why.
// The learning is the learning — same struct, same provenance, same
// retraction state (always live here: a retracted body never reaches this
// door, LM2 governs the reads forAgent rides).
type ForAgentHit struct {
	Learning Learning
	Why      ForAgentWhy
}

// ForAgent answers the question memory management exists to answer
// (S52 LM5): "what would this agent be shown?" It resolves the three
// scope families a prompt would draw from — the person's learnings, the
// projects this agent has actually run in (step.agent joined to the loop's
// root: where the running lives, not a guess beside it), and the agent's
// own scope — and reads every one of them through THE SAME Query the rest
// of the world reads through. Not a parallel read with parallel rules:
// one function, so "what the prompt would see" and "what the door answers"
// cannot drift apart, and the differential test can pin them equal.
//
// Ordering (LM5's why): with a query, the merged set is ranked by the one
// rankWithinSet the tag queries use (one embedding call for the query, the
// only cost the call already incurs); without one, it lists in scope order
// with NO embedding call at all (T0's cost answer — the door that embeds
// for a browse is the door that bills for a glance). Across both, the M1
// ordering holds as a partition: relayed learnings rank below every told,
// observed and assumed one, the note beside each hit quoting the law.
//
// Read-only: it writes nothing and costs nothing but what a query costs.
func (l *Learnings) ForAgent(ctx context.Context, agentSlug, queryText string, limit int) ([]ForAgentHit, *Refusal) {
	// S52 T4: the body moved to MineQuery, and this door is its no-tags
	// call — the agent's browse and the agent's twin of the person's
	// query are ONE function, so one merged set, one ordering law, one
	// embedding budget. The doc above stands as the law; the code now
	// cannot disagree with it because there is one of it.
	return l.MineQuery(ctx, agentSlug, MineFilter{Query: queryText, Limit: limit})
}

// queryChecked is Query with the domain refusal surfaced (a store error is
// a Refusal-shaped answer too — forAgent's callers are doors that answer
// in refusal sentences).
func (l *Learnings) queryChecked(ctx context.Context, f QueryFilter) ([]Learning, *Refusal) {
	out, err := l.Query(ctx, f)
	if err != nil {
		return nil, &Refusal{Reason: fmt.Sprintf("learnings: the read through Query failed: %v", err)}
	}
	return out, nil
}

func isRelayed(l Learning) bool { return l.Provenance == ProvenanceRelayed }

// BoardFilter is LM6's daemon-side filter set for the board read: every
// field optional, every set field ANDed. The filters run IN THE DAEMON
// (the app never lists-everything-and-filters-client-side — LM6, and the
// guard pattern every board slice before this one has kept).
type BoardFilter struct {
	Scope            string // person|project|agent, empty = all
	Provenance       string // one of the four
	Tag              string // exact tag the row must carry
	Agent            string // scope_key when Scope is agent
	Project          string // scope_key when Scope is project
	Loop             string // source loop slug
	IncludeRetracted bool   // LM2's explicit exception, default false
}

// Board is the memory board's read (LM6): every learning the filters
// select, with retracted ones absent unless explicitly asked for. The
// store applies the retraction predicate and the scope filter; the rest of
// the filter set runs beside the rows in Go, because provenance, tag and
// source-loop are shape the one shared WHERE does not need to grow (and a
// board query that widens the SQL for every UI control is how a read
// becomes a query builder).
func (l *Learnings) Board(f BoardFilter) ([]Learning, error) {
	rows, err := l.s.ListLearnings(store.LearningFilters{
		Scope:            f.Scope,
		IncludeRetracted: f.IncludeRetracted,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Learning, 0, len(rows))
	for _, r := range rows {
		lr, err := FromRow(r)
		if err != nil {
			return nil, err
		}
		if f.Provenance != "" && string(lr.Provenance) != f.Provenance {
			continue
		}
		if f.Tag != "" && !carriesTag(lr, f.Tag) {
			continue
		}
		if f.Agent != "" && (string(lr.Scope) != "agent" || lr.ScopeKey != f.Agent) {
			continue
		}
		if f.Project != "" && (string(lr.Scope) != "project" || lr.ScopeKey != f.Project) {
			continue
		}
		if f.Loop != "" && (lr.SourceLoopSlug == nil || *lr.SourceLoopSlug != f.Loop) {
			continue
		}
		out = append(out, lr)
	}
	return out, nil
}

func carriesTag(l Learning, tag string) bool {
	for _, t := range l.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

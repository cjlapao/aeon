package store

import "fmt"

// attention_read_more.go — the attention plane's three later reads (S58 sources `gates`, `proposals`,
// `triage`). Same rule as attention_read.go: each is one predicate in one place, read-only, scoped by
// the same RootScopeSQL, and carries ids, enums and stored sentences — nothing a person typed beyond
// what the row already holds.

// AttentionFailedGate is a gate run that FAILED and is still the latest run of its slot: the same
// (component, loop, task) has not run again since. A failure a later run fixed is not here.
type AttentionFailedGate struct {
	RunID         string
	LoopSlug      string
	ComponentRoot string
	Task          string
	StartedAt     string
	Summary       string
	Reason        string
}

// FailedGatesForScope reads the latest run of every (component, loop, task) slot, keeping the failed ones.
func (s *Store) FailedGatesForScope(roots []string) ([]AttentionFailedGate, error) {
	where, args := RootScopeSQL("r.component_root", roots)
	rows, err := s.r.Query(`
		SELECT r.id, r.loop_slug, r.component_root, r.task, r.started_at, r.summary, r.reason
		  FROM verify_run r
		 WHERE r.state = 'fail'
		   AND r.id = (SELECT r2.id FROM verify_run r2
		                WHERE r2.component_root = r.component_root
		                  AND r2.loop_slug = r.loop_slug
		                  AND r2.task = r.task
		                ORDER BY r2.started_at DESC, r2.id DESC LIMIT 1)`+where+`
		 ORDER BY r.started_at, r.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list the failed gate runs: %w", err)
	}
	defer rows.Close()
	var out []AttentionFailedGate
	for rows.Next() {
		var g AttentionFailedGate
		if err := rows.Scan(&g.RunID, &g.LoopSlug, &g.ComponentRoot, &g.Task, &g.StartedAt, &g.Summary, &g.Reason); err != nil {
			return nil, fmt.Errorf("store: read a failed gate run: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: finish the failed gate runs: %w", err)
	}
	return out, nil
}

// AttentionOpenProposal is a proposal still waiting for a decision (status `proposed`).
type AttentionOpenProposal struct {
	ID            int64
	Kind          string
	CreatedAt     string
	CreatedByKind string
	CreatedByID   string
}

// OpenProposals reads every undecided proposal, oldest first. A proposal belongs to the daemon, not to
// a component, so it carries no root and is not narrowed by a scope.
func (s *Store) OpenProposals() ([]AttentionOpenProposal, error) {
	rows, err := s.r.Query(`
		SELECT id, kind, created_at, created_by_kind, created_by_id
		  FROM proposal WHERE status = 'proposed' ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list the proposals waiting for a decision: %w", err)
	}
	defer rows.Close()
	var out []AttentionOpenProposal
	for rows.Next() {
		var p AttentionOpenProposal
		if err := rows.Scan(&p.ID, &p.Kind, &p.CreatedAt, &p.CreatedByKind, &p.CreatedByID); err != nil {
			return nil, fmt.Errorf("store: read a waiting proposal: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: finish the waiting proposals: %w", err)
	}
	return out, nil
}

// AttentionUnvalidatedBugs is, for one component, how many bugs still wait for validation and since when.
type AttentionUnvalidatedBugs struct {
	ComponentRoot string
	Count         int
	Oldest        string
}

// UnvalidatedBugsForScope counts the `unvalidated` bugs per component root.
func (s *Store) UnvalidatedBugsForScope(roots []string) ([]AttentionUnvalidatedBugs, error) {
	where, args := RootScopeSQL("component_root", roots)
	rows, err := s.r.Query(`
		SELECT component_root, COUNT(*), MIN(created_at)
		  FROM bug WHERE validation_state = 'unvalidated'`+where+`
		 GROUP BY component_root ORDER BY MIN(created_at), component_root`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: count the bugs waiting for validation: %w", err)
	}
	defer rows.Close()
	var out []AttentionUnvalidatedBugs
	for rows.Next() {
		var b AttentionUnvalidatedBugs
		if err := rows.Scan(&b.ComponentRoot, &b.Count, &b.Oldest); err != nil {
			return nil, fmt.Errorf("store: read a bug count: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: finish the bug counts: %w", err)
	}
	return out, nil
}

// The reader seams the attention sources take (the *Store satisfies each; tests pass fakes).
type (
	FailedGatesReader interface {
		FailedGatesForScope(roots []string) ([]AttentionFailedGate, error)
	}
	OpenProposalsReader interface {
		OpenProposals() ([]AttentionOpenProposal, error)
	}
	UnvalidatedBugsReader interface {
		UnvalidatedBugsForScope(roots []string) ([]AttentionUnvalidatedBugs, error)
	}
)

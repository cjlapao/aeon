package store

// mission_read.go — S58 T6 (SV11): the two loop rollups behind
// `mission.status`, and nothing else. The counts are a GROUP BY over the
// loop rows that ARE the truth (the same `l.status` column the blocked and
// stopped attention sources read); this file derives no state, invents no
// fifth status, and reports no composite health number — SV11's overview is
// a set of facts per root, and a score would be a judgment the store is
// forbidden to hold (rule 6).
//
// The scoping is the S58 convention verbatim: the roots are the component
// PATHS the daemon resolved (`attention.ResolveScope` / `EveryScope`), and
// `RootScopeSQL` matches a path and everything under it — "a root includes
// its children" is that one helper, not a second tree walk.

import (
	"database/sql"
	"fmt"
)

// LoopStatusCountsForScope counts loop rows by their stored status over the
// scope. Every status comes back as the table holds it, INCLUDING the
// terminal ones — the caller (internal/mission) reports the four live ones
// (`running`, `waiting`, `blocked`, `idle`, the loop_control vocabulary)
// and drops the rest rather than inventing a `terminal` count.
func (s *Store) LoopStatusCountsForScope(roots []string) (map[string]int, error) {
	where, args := RootScopeSQL("l.component_root", roots)
	rows, err := s.r.Query(`SELECT l.status, COUNT(*) FROM loop l WHERE 1=1`+where+` GROUP BY l.status`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: mission loop counts: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, fmt.Errorf("store: mission loop counts scan: %w", err)
		}
		out[status] = n
	}
	return out, rows.Err()
}

// LastActivityForScope is the max loop `updated_at` over the scope — the
// most recent loop touch under these trees, "" when no loop has ever been
// written there. Not a health signal: just the newest timestamp, because an
// overview that goes silent should say since when.
func (s *Store) LastActivityForScope(roots []string) (string, error) {
	where, args := RootScopeSQL("l.component_root", roots)
	var max string
	err := s.r.QueryRow(`SELECT COALESCE(MAX(l.updated_at), '') FROM loop l WHERE 1=1`+where, args...).Scan(&max)
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("store: mission last activity: %w", err)
	}
	return max, nil
}

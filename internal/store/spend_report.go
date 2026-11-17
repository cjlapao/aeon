package store

// spend_report.go — S55 T8 (spend.report): the spend-over-time door's
// aggregation. One GROUP BY per groupBy over the step rows, plus the
// step_turn detail for the token floors. P31 throughout: unknowns are
// COUNTED, never summed as zero, and the floor is a floor — `exact` says
// when it is a total, and the counts say what it is not.

import (
	"fmt"
	"time"
)

// SpendReportBucket is one bucket of the report: the key the groupBy names,
// the USD floor and its honesty marks, the token floor (nil = no tokens
// known at all in the bucket), and the step count.
type SpendReportBucket struct {
	Key           string
	USDFloor      float64
	USDPartial    int
	USDUnknown    int
	Tokens        *int64
	TokensUnknown int
	Steps         int
}

// Exact reports whether the bucket's floor is a total: no partial and no
// unknown step in it (P31 — a floor with unknowns beside it is not an exact
// number, and the bucket says so rather than letting a lower bound read as a
// fact).
func (b SpendReportBucket) Exact() bool { return b.USDUnknown == 0 && b.USDPartial == 0 }

// SpendReport groups the step spend in [since, until] (root-scoped when
// rootID is non-empty) by groupBy:
//
//   - day:       the step's start day (the started_at date, UTC as stored)
//   - trigger:   the loop's trigger id ('none' when a person started it)
//   - component: the loop's component_root
//   - epic:      the loop's goal's epic ('none' when the loop has no goal)
//   - route:     the turn's route — the ONLY bucket computed at turn level,
//     because a step can run turns on several routes and only the turn rows
//     partition a step's cost across them (a step-level grouping would count
//     the step's whole cost once per route it touched)
//
// A step belongs wholly to one day/trigger/component/epic, so those four
// group at step level (the T6 spend's own grain); the token floors join the
// step_turn detail beside them. Buckets come back sorted by key, so a client
// never sees a shuffled report of the same numbers.
func (s *Store) SpendReport(groupBy string, since, until time.Time, rootID string) ([]SpendReportBucket, error) {
	joins := `JOIN loop l ON s.loop_slug = l.slug AND s.component_root = l.component_root`
	where := `WHERE s.started_at >= ? AND s.started_at <= ?`
	args := []any{isoUTC(since), isoUTC(until)}
	if rootID != "" {
		where += ` AND l.component_root = ?`
		args = append(args, rootID)
	}
	var keyExpr string
	switch groupBy {
	case "day":
		keyExpr = `DATE(s.started_at)`
	case "trigger":
		keyExpr = `COALESCE(NULLIF(l.trigger_id, ''), 'none')`
	case "component":
		keyExpr = `l.component_root`
	case "epic":
		joins += ` LEFT JOIN goal g ON g.id = l.goal_id`
		keyExpr = `COALESCE(g.epic_id, 'none')`
	case "route":
		joins += ` JOIN step_turn st ON st.step_id = s.id`
		keyExpr = `COALESCE(st.route, 'none')`
	default:
		return nil, fmt.Errorf("store: spend report: unknown groupBy %q (day, trigger, component, epic or route)", groupBy)
	}
	if groupBy == "route" {
		return s.routeReport(joins, where, keyExpr, args...)
	}
	return s.stepReport(joins, where, keyExpr, args...)
}

// stepReport is the step-level group (day/trigger/component/epic): the USD
// floor and its marks from the step rows, the token floor from the turn
// detail, merged on the key.
func (s *Store) stepReport(joins, where, keyExpr string, args ...any) ([]SpendReportBucket, error) {
	buckets := map[string]*SpendReportBucket{}
	// Step level: usd floor + honesty marks + step count.
	rows, err := s.r.Query(fmt.Sprintf(`SELECT %s,
		COALESCE(SUM(CASE WHEN s.cost_state IN ('known','partial') THEN s.cost_usd END), 0),
		COALESCE(SUM(CASE WHEN s.cost_state = 'partial' THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN s.cost_state IS NULL OR s.cost_state = 'unknown' THEN 1 END), 0),
		COUNT(*)
		FROM step s %s %s
		GROUP BY 1 ORDER BY 1`, keyExpr, joins, where), args...)
	if err != nil {
		return nil, fmt.Errorf("store: spend report: %w", err)
	}
	for rows.Next() {
		var b SpendReportBucket
		if err := rows.Scan(&b.Key, &b.USDFloor, &b.USDPartial, &b.USDUnknown, &b.Steps); err != nil {
			rows.Close()
			return nil, fmt.Errorf("store: spend report: %w", err)
		}
		c := b
		buckets[c.Key] = &c
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("store: spend report: %w", err)
	}
	rows.Close()
	// Turn level: the token floor and its unknowns, merged on the key.
	rows, err = s.r.Query(fmt.Sprintf(`SELECT %s,
		COALESCE(SUM(CASE WHEN st.cost_state IN ('known','partial') THEN
			COALESCE(st.input_tokens,0)+COALESCE(st.output_tokens,0)+COALESCE(st.cache_read_tokens,0)+COALESCE(st.cache_write_tokens,0)+COALESCE(st.reasoning_tokens,0) END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state IS NULL OR st.cost_state = 'unknown' THEN 1 END), 0)
		FROM step s %s JOIN step_turn st ON st.step_id = s.id %s
		GROUP BY 1`, keyExpr, joins, where), args...)
	if err != nil {
		return nil, fmt.Errorf("store: spend report: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var toks int64
		var unknown int
		if err := rows.Scan(&key, &toks, &unknown); err != nil {
			return nil, fmt.Errorf("store: spend report: %w", err)
		}
		b, ok := buckets[key]
		if !ok {
			b = &SpendReportBucket{Key: key}
			buckets[key] = b
		}
		if toks > 0 {
			t := toks
			b.Tokens = &t
		}
		b.TokensUnknown = unknown
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: spend report: %w", err)
	}
	return sortedBuckets(buckets), nil
}

// routeReport is the turn-level group: every number from the turn rows, so a
// step that crossed routes pays each route exactly its turns.
func (s *Store) routeReport(joins, where, keyExpr string, args ...any) ([]SpendReportBucket, error) {
	buckets := map[string]*SpendReportBucket{}
	rows, err := s.r.Query(fmt.Sprintf(`SELECT %s,
		COALESCE(SUM(CASE WHEN st.cost_state IN ('known','partial') THEN st.cost_usd END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state = 'partial' THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state IS NULL OR st.cost_state = 'unknown' THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state IN ('known','partial') THEN
			COALESCE(st.input_tokens,0)+COALESCE(st.output_tokens,0)+COALESCE(st.cache_read_tokens,0)+COALESCE(st.cache_write_tokens,0)+COALESCE(st.reasoning_tokens,0) END), 0),
		COUNT(DISTINCT s.id)
		FROM step s %s %s
		GROUP BY 1 ORDER BY 1`, keyExpr, joins, where), args...)
	if err != nil {
		return nil, fmt.Errorf("store: spend report: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b SpendReportBucket
		var toks int64
		if err := rows.Scan(&b.Key, &b.USDFloor, &b.USDPartial, &b.USDUnknown, &toks, &b.Steps); err != nil {
			return nil, fmt.Errorf("store: spend report: %w", err)
		}
		if toks > 0 {
			t := toks
			b.Tokens = &t
		}
		c := b
		buckets[c.Key] = &c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: spend report: %w", err)
	}
	return sortedBuckets(buckets), nil
}

func sortedBuckets(m map[string]*SpendReportBucket) []SpendReportBucket {
	out := make([]SpendReportBucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Key < out[j-1].Key; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

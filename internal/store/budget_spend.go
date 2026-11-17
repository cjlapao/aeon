package store

import (
	"errors"
	"fmt"
	"time"
)

// budget_spend.go — the one spend function (LC9/LC11) behind S55's budgets.
// It answers "how much has this budget spent so far" dimension by dimension,
// honouring the P31 rule: an unknown is a COUNT, never a zero. A confident zero
// over unpriced work is the wrong answer with decimals on it, so an unknown
// step is counted (USDUnknown, TokensUnknown) and its cost is never summed as 0.
//
// The source of a number depends on the budget's scope (LC9's source table):
// a lifetime component/workspace total comes from the telemetry rollup row
// (a root's row is the subtree total, S44-extra CT7); a loop or an epic total
// is an aggregate over the steps that the loops produce. Nothing here enforces
// a cap — that is T7's engine; this is a read, not a gate.
//
// The type is named BudgetSpend (not Spend): the package's `Spend` is S40's
// trigger cost, a different shape. This is the budget's, and it is the one the
// domain's internal/budget maps onto.

// BudgetSpend is a budget's spend so far, per dimension. USDAtLeast sums every
// known and partial number (a partial is a floor); Exact is true only when
// nothing was partial or unknown. An unknown is carried as a COUNT
// (USDUnknown, TokensUnknown), never added as zero — the count is how a reader
// sees that the number does not know (P31).
type BudgetSpend struct {
	USDAtLeast    float64
	USDPartial    int // at least one partial: USDAtLeast is a floor, not a total
	USDUnknown    int // unknown (or NULL) steps: NEVER summed as zero
	Exact         bool
	Tokens        *int64 // the sum of known+partial token counts; nil = none known
	TokensUnknown int
	WallMs        int64
	Steps         int
}

// dayWindowStart is the rolling 24-hour floor for a day-window budget (LC12):
// the window is "since 24 h ago", recomputed each read, never a calendar day.
func dayWindowStart(now time.Time) time.Time { return now.Add(-24 * time.Hour) }

// SpendFor answers a budget's spend so far, by dimension. A dimension the budget
// does not cap is left at its zero/nil — the budget does not care about it.
func (s *Store) SpendFor(b Budget, now time.Time) (BudgetSpend, error) {
	var sp BudgetSpend
	switch b.Scope {
	case "component", "workspace":
		if b.Window == "lifetime" {
			// The rollup row IS the total (CT7: a root's row is the subtree
			// total). A component with no row is a zero — no loops have run.
			telem, err := s.telemetryFor(b.Scope, b.Subject)
			if err != nil {
				return BudgetSpend{}, err
			}
			sp.USDAtLeast = telem.CostUSDTotal
			switch telem.CostState {
			case "partial":
				sp.USDPartial = 1
			case "unknown":
				sp.USDUnknown = 1
			}
			sp.WallMs = telem.WallMsTotal
			if toks := telem.TokensTotal(); toks > 0 {
				sp.Tokens = &toks
			}
			// Steps: count the step rows the subtree produced.
			if err := s.countSteps(&sp, "component", b.Subject, zeroTime(), now); err != nil {
				return BudgetSpend{}, err
			}
		} else {
			if err := s.stepAggregate(&sp, "component", b.Subject, dayWindowStart(now), now); err != nil {
				return BudgetSpend{}, err
			}
		}
	case "loop":
		since := zeroTime()
		if b.Window == "day" {
			since = dayWindowStart(now)
		}
		if err := s.stepAggregate(&sp, "loop", b.Subject, since, now); err != nil {
			return BudgetSpend{}, err
		}
	case "epic":
		since := zeroTime()
		if b.Window == "day" {
			since = dayWindowStart(now)
		}
		if err := s.epicAggregate(&sp, b.Subject, since); err != nil {
			return BudgetSpend{}, err
		}
	default:
		return BudgetSpend{}, fmt.Errorf("store: spend for budget %s: unknown scope %q", b.ID, b.Scope)
	}
	sp.Exact = sp.USDPartial == 0 && sp.USDUnknown == 0 && sp.TokensUnknown == 0
	return sp, nil
}

// telemetryFor reads the rollup row for a component or the workspace. A
// component with no row is a zero (not an error); the workspace with no row is
// the legitimate all-zero state (WorkspaceTelemetry).
func (s *Store) telemetryFor(scope, subject string) (TelemetrySums, error) {
	if scope == "workspace" {
		w, err := s.WorkspaceTelemetry()
		if err != nil {
			return TelemetrySums{}, fmt.Errorf("store: workspace telemetry: %w", err)
		}
		return w.TelemetrySums, nil
	}
	c, err := s.ComponentTelemetry(subject)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return TelemetrySums{}, nil // no row: a zero, not a fault
		}
		return TelemetrySums{}, err
	}
	return c.TelemetrySums, nil
}

// stepAggregate sums the steps under one loop since `since`, with the P31
// discipline: known+partial costs sum (a partial is a floor), unknown steps are
// counted, never added as zero. The join is the house query (step joins loop on
// slug + component_root, the live column since S38).
func (s *Store) stepAggregate(sp *BudgetSpend, scope, subject string, since, until time.Time) error {
	var partials, unknowns int
	var usd float64
	err := s.r.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN st.cost_state IN ('known','partial') THEN st.cost_usd END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state = 'partial' THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state IS NULL OR st.cost_state = 'unknown' THEN 1 END), 0),
		COUNT(*)
		FROM loop l JOIN step st ON st.loop_slug = l.slug AND st.component_root = l.component_root
		WHERE l.id = ? AND st.started_at >= ? AND st.started_at <= ?`,
		subject, isoUTC(since), isoUTC(until)).Scan(&usd, &partials, &unknowns, &sp.Steps)
	if err != nil {
		return fmt.Errorf("store: spend for %s %s: %w", scope, subject, err)
	}
	sp.USDAtLeast = usd
	sp.USDPartial = partials
	sp.USDUnknown = unknowns
	// Tokens and wall from the step_turn detail (the per-turn numbers).
	if err := s.stepTurnAggregate(sp, "loop", subject, since, until); err != nil {
		return err
	}
	return nil
}

// stepTurnAggregate sums the step_turn detail (tokens, wall) for one loop, with
// the P31 discipline: a NULL or unknown cost_state turn is counted, never
// summed as zero.
func (s *Store) stepTurnAggregate(sp *BudgetSpend, scope, subject string, since, until time.Time) error {
	var tokKnown int64
	var unknowns int
	var wall int64
	err := s.r.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN st.cost_state IN ('known','partial') THEN
			COALESCE(st.input_tokens,0)+COALESCE(st.output_tokens,0)+COALESCE(st.cache_read_tokens,0)+COALESCE(st.cache_write_tokens,0)+COALESCE(st.reasoning_tokens,0) END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state IS NULL OR st.cost_state = 'unknown' THEN 1 END), 0),
		COALESCE(SUM(st.wall_ms), 0)
		FROM loop l JOIN step s ON s.loop_slug = l.slug AND s.component_root = l.component_root
		JOIN step_turn st ON st.step_id = s.id
		WHERE l.id = ? AND s.started_at >= ? AND s.started_at <= ?`,
		subject, isoUTC(since), isoUTC(until)).Scan(&tokKnown, &unknowns, &wall)
	if err != nil {
		return fmt.Errorf("store: spend for %s %s: %w", scope, subject, err)
	}
	if tokKnown > 0 {
		sp.Tokens = &tokKnown
	}
	sp.TokensUnknown = unknowns
	sp.WallMs += wall
	return nil
}

// epicAggregate sums every loop that works toward the epic (loop → goal →
// epic) since `since`. A loop with no goal is in no epic, so it is not counted.
func (s *Store) epicAggregate(sp *BudgetSpend, epicID string, since time.Time) error {
	var partials, unknowns int
	var usd float64
	err := s.r.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN st.cost_state IN ('known','partial') THEN st.cost_usd END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state = 'partial' THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state IS NULL OR st.cost_state = 'unknown' THEN 1 END), 0),
		COUNT(*)
		FROM loop l
		JOIN goal g ON g.id = l.goal_id
		JOIN step st ON st.loop_slug = l.slug AND st.component_root = l.component_root
		WHERE g.epic_id = ? AND st.started_at >= ?`,
		epicID, isoUTC(since)).Scan(&usd, &partials, &unknowns, &sp.Steps)
	if err != nil {
		return fmt.Errorf("store: spend for epic %s: %w", epicID, err)
	}
	sp.USDAtLeast = usd
	sp.USDPartial = partials
	sp.USDUnknown = unknowns
	if err := s.epicTurnAggregate(sp, epicID, since); err != nil {
		return err
	}
	return nil
}

// epicTurnAggregate sums the step_turn detail (tokens, wall) of an epic's loops
// since `since`, with the P31 discipline.
func (s *Store) epicTurnAggregate(sp *BudgetSpend, epicID string, since time.Time) error {
	var tokKnown int64
	var unknowns int
	var wall int64
	err := s.r.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN st.cost_state IN ('known','partial') THEN
			COALESCE(st.input_tokens,0)+COALESCE(st.output_tokens,0)+COALESCE(st.cache_read_tokens,0)+COALESCE(st.cache_write_tokens,0)+COALESCE(st.reasoning_tokens,0) END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state IS NULL OR st.cost_state = 'unknown' THEN 1 END), 0),
		COALESCE(SUM(st.wall_ms), 0)
		FROM loop l
		JOIN goal g ON g.id = l.goal_id
		JOIN step s ON s.loop_slug = l.slug AND s.component_root = l.component_root
		JOIN step_turn st ON st.step_id = s.id
		WHERE g.epic_id = ? AND s.started_at >= ?`,
		epicID, isoUTC(since)).Scan(&tokKnown, &unknowns, &wall)
	if err != nil {
		return fmt.Errorf("store: spend for epic %s: %w", epicID, err)
	}
	if tokKnown > 0 {
		sp.Tokens = &tokKnown
	}
	sp.TokensUnknown = unknowns
	sp.WallMs += wall
	return nil
}

// countSteps adds the step count under one subject since `since` to the
// spend's Steps — the telemetry rollup has a loop count, not a step count, so
// the steps dimension needs its own aggregate.
func (s *Store) countSteps(sp *BudgetSpend, scope, subject string, since, until time.Time) error {
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*)
		FROM loop l JOIN step st ON st.loop_slug = l.slug AND st.component_root = l.component_root
		WHERE l.id = ? AND st.started_at >= ? AND st.started_at <= ?`,
		subject, isoUTC(since), isoUTC(until)).Scan(&n); err != nil {
		return fmt.Errorf("store: count steps for %s %s: %w", scope, subject, err)
	}
	sp.Steps = n
	return nil
}

// zeroTime is the "no lower bound" sentinel for a lifetime aggregate: every
// started_at is >= 1970, so nothing is excluded.
func zeroTime() time.Time { return time.Unix(0, 0) }

// SpendSeriesBucket is one bucket of a spend series (T8's spend.report).
type SpendSeriesBucket struct {
	Key   string
	Spend BudgetSpend
}

// SpendSeries buckets spend by groupBy (day|trigger|component|epic|route) over
// the [since, until] window, scoped to rootID's tree (or the whole workspace
// when rootID is ""). Each bucket is a BudgetSpend (P31: a bucket with an
// unknown step is not a zero). A bucket with no steps is absent.
func (s *Store) SpendSeries(groupBy string, since, until time.Time, rootID string) ([]SpendSeriesBucket, error) {
	keyExpr, join := s.spendSeriesKey(groupBy)
	if keyExpr == "" {
		return nil, fmt.Errorf("store: spend series: unknown groupBy %q", groupBy)
	}
	var q = `SELECT ` + keyExpr + `,
		COALESCE(SUM(CASE WHEN st.cost_state IN ('known','partial') THEN st.cost_usd END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state = 'partial' THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state IS NULL OR st.cost_state = 'unknown' THEN 1 END), 0),
		COUNT(*)
		FROM loop l JOIN step st ON st.loop_slug = l.slug AND st.component_root = l.component_root` + join +
		` WHERE st.started_at >= ? AND st.started_at <= ?`
	if rootID != "" {
		q += ` AND l.component_root = ?`
	}
	q += ` GROUP BY ` + keyExpr
	var args = []any{isoUTC(since), isoUTC(until)}
	if rootID != "" {
		args = append(args, rootID)
	}
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: spend series by %s: %w", groupBy, err)
	}
	defer rows.Close()
	var out []SpendSeriesBucket
	for rows.Next() {
		var key string
		var sp BudgetSpend
		var partials, unknowns int
		if err := rows.Scan(&key, &sp.USDAtLeast, &partials, &unknowns, &sp.Steps); err != nil {
			return nil, fmt.Errorf("store: scan spend series: %w", err)
		}
		sp.USDPartial = partials
		sp.USDUnknown = unknowns
		sp.Exact = partials == 0 && unknowns == 0
		out = append(out, SpendSeriesBucket{Key: key, Spend: sp})
	}
	return out, rows.Err()
}

// spendSeriesKey returns the GROUP BY expression and the extra JOINs a bucket
// needs (the epic bucket joins goal; the route bucket joins step_turn). An
// empty keyExpr is an unknown groupBy.
func (s *Store) spendSeriesKey(groupBy string) (key, join string) {
	switch groupBy {
	case "day":
		return `date(st.started_at)`, ""
	case "trigger":
		return `COALESCE(l.trigger_id, 'user')`, ""
	case "component":
		return `COALESCE(l.component_root, 'workspace')`, ""
	case "epic":
		return `COALESCE(g.epic_id, 'none')`, ` JOIN goal g ON g.id = l.goal_id`
	case "route":
		return `COALESCE(t.route, 'unknown')`, ` JOIN step_turn t ON t.step_id = st.id`
	default:
		return "", ""
	}
}

// TokensTotal is the sum of a telemetry rollup's token counts (the five token
// columns), read for a budget's tokens dimension.
func (t TelemetrySums) TokensTotal() int64 {
	return t.InputTokensTotal + t.OutputTokensTotal + t.CacheReadTokensTotal + t.CacheWriteTokensTotal + t.ReasoningTokensTotal
}

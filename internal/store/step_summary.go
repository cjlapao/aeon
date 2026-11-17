package store

import (
	"encoding/json"
	"fmt"
)

// StepSummary is what one step has done so far, kept beside the event journal so a page reads a step's totals
// without replaying its events. The step's name, status and agent are NOT here: they live on the step row and are
// joined at read time, so a summary can never disagree with the step about what state it is in.
type StepSummary struct {
	StepID          int64    `json:"stepId"`
	LoopID          string   `json:"loopId"`
	Lane            string   `json:"lane,omitempty"`
	Agent           string   `json:"agent,omitempty"`
	Status          string   `json:"status,omitempty"`
	Turns           int      `json:"turns"`
	ToolCalls       int      `json:"toolCalls"`
	ToolErrors      int      `json:"toolErrors"`
	InputTokens     int64    `json:"inputTokens"`
	OutputTokens    int64    `json:"outputTokens"`
	ReasoningTokens int64    `json:"reasoningTokens"`
	WallMs          int64    `json:"wallMs"`
	CostState       string   `json:"costState"`
	CostUSD         *float64 `json:"costUsd,omitempty"`
	FirstEvent      int64    `json:"firstEvent"`
	LastEvent       int64    `json:"lastEvent"`
	// ContextTokens is the size of the step's latest model call input: how much of its context window it holds now.
	// Route is the route that call ran on, and ContextWindow that route's known input budget (0 when unknown; filled
	// by the reader that knows the routes, never stored here).
	ContextTokens int64  `json:"contextTokens"`
	Route         string `json:"route,omitempty"`
	ContextWindow int64  `json:"contextWindow,omitempty"`
}

// summaryKinds are the events that move a step's summary.
func summaryKind(kind string) bool {
	switch kind {
	case "loop.step.dispatched", "step.turn.completed", "step.tool.end", "step.done", "step.tool.start":
		return true
	}
	return false
}

// applySummary folds one appended event into its step's summary. It is called by AppendEvent after the event row is
// written and never fails the append: a missing summary is rebuilt by the migration's query, a lost event is not.
func (s *Store) applySummary(loopID string, stepID int64, kind string, payload []byte, seq int64) {
	if loopID == "" || stepID == 0 || !summaryKind(kind) {
		return
	}
	var p struct {
		Lane    string `json:"lane"`
		IsError bool   `json:"isError"`
		WallMs  int64  `json:"wallMs"`
		Route   string `json:"route"`
		Usage   struct {
			In  int64 `json:"inputTokens"`
			Out int64 `json:"outputTokens"`
			Re  int64 `json:"reasoningTokens"`
			CR  int64 `json:"cacheReadTokens"`
			CW  int64 `json:"cacheWriteTokens"`
		} `json:"usage"`
		StepCost *struct {
			State      string   `json:"state"`
			USD        *float64 `json:"usd"`
			USDAtLeast *float64 `json:"usdAtLeast"`
		} `json:"stepCost"`
	}
	_ = json.Unmarshal(payload, &p)
	turns, calls, errs := 0, 0, 0
	switch kind {
	case "step.turn.completed":
		turns = 1
	case "step.tool.end":
		calls = 1
		if p.IsError {
			errs = 1
		}
	}
	if _, err := s.w.Exec(`
		INSERT INTO step_summary (step_id, loop_id, lane, turns, tool_calls, tool_errors, input_tokens, output_tokens, reasoning_tokens, wall_ms, first_event, last_event)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(step_id) DO UPDATE SET
		  lane = CASE WHEN excluded.lane <> '' THEN excluded.lane ELSE step_summary.lane END,
		  turns = turns + excluded.turns, tool_calls = tool_calls + excluded.tool_calls, tool_errors = tool_errors + excluded.tool_errors,
		  input_tokens = input_tokens + excluded.input_tokens, output_tokens = output_tokens + excluded.output_tokens,
		  reasoning_tokens = reasoning_tokens + excluded.reasoning_tokens, wall_ms = wall_ms + excluded.wall_ms,
		  last_event = excluded.last_event`,
		stepID, loopID, p.Lane, turns, calls, errs, p.Usage.In, p.Usage.Out, p.Usage.Re, p.WallMs, seq, seq); err != nil {
		return
	}
	if kind == "step.turn.completed" {
		_, _ = s.w.Exec(`UPDATE step_summary SET context_tokens = ?, route = CASE WHEN ? <> '' THEN ? ELSE route END WHERE step_id = ?`,
			p.Usage.In+p.Usage.CR+p.Usage.CW, p.Route, p.Route, stepID)
	}
	if kind == "step.turn.completed" && p.StepCost != nil {
		usd := p.StepCost.USD
		if usd == nil {
			usd = p.StepCost.USDAtLeast
		}
		_, _ = s.w.Exec(`UPDATE step_summary SET spend_state = ?, spend_usd = ? WHERE step_id = ?`, p.StepCost.State, usd, stepID)
	}
}

// LoopSummaryWindow is one page of a loop's step summaries, newest first, and the loop's totals.
type LoopSummaryWindow struct {
	Steps []StepSummary
	// HasOlder says whether summaries older than the last one returned exist.
	HasOlder bool
}

// LoopTotals is the loop's own totals across every step, from the summaries.
type LoopTotals struct {
	Steps           int      `json:"steps"`
	Turns           int      `json:"turns"`
	ToolCalls       int      `json:"toolCalls"`
	ToolErrors      int      `json:"toolErrors"`
	InputTokens     int64    `json:"inputTokens"`
	OutputTokens    int64    `json:"outputTokens"`
	ReasoningTokens int64    `json:"reasoningTokens"`
	WallMs          int64    `json:"wallMs"`
	CostUSD         *float64 `json:"costUsd,omitempty"`
	// CostState is known only when every step that spent has a known cost; partial when some do, unknown when none.
	CostState string `json:"costState"`
}

const (
	DefaultStepSummaryLimit = 50
	MaxStepSummaryLimit     = 500
)

// ReadStepSummaries returns a loop's step summaries newest first. `before` is a step id: only steps older than it.
func (s *Store) ReadStepSummaries(loopID string, before int64, limit int) (LoopSummaryWindow, error) {
	if loopID == "" {
		return LoopSummaryWindow{}, fmt.Errorf("store: ReadStepSummaries needs a loop id")
	}
	if limit <= 0 {
		limit = DefaultStepSummaryLimit
	}
	if limit > MaxStepSummaryLimit {
		limit = MaxStepSummaryLimit
	}
	args := []any{loopID}
	cursor := ""
	if before > 0 {
		cursor = ` AND ss.step_id < ?`
		args = append(args, before)
	}
	args = append(args, limit+1)
	rows, err := s.r.Query(`
		SELECT ss.step_id, ss.loop_id, ss.lane, COALESCE(st.agent,''), COALESCE(st.status,''), ss.turns, ss.tool_calls, ss.tool_errors,
		       ss.input_tokens, ss.output_tokens, ss.reasoning_tokens, ss.wall_ms, ss.spend_state, ss.spend_usd, ss.first_event, ss.last_event, ss.context_tokens, ss.route
		FROM step_summary ss LEFT JOIN step st ON st.id = ss.step_id
		WHERE ss.loop_id = ?`+cursor+` ORDER BY ss.step_id DESC LIMIT ?`, args...)
	if err != nil {
		return LoopSummaryWindow{}, fmt.Errorf("store: read step summaries: %w", err)
	}
	defer rows.Close()
	var out LoopSummaryWindow
	for rows.Next() {
		var x StepSummary
		if err := rows.Scan(&x.StepID, &x.LoopID, &x.Lane, &x.Agent, &x.Status, &x.Turns, &x.ToolCalls, &x.ToolErrors,
			&x.InputTokens, &x.OutputTokens, &x.ReasoningTokens, &x.WallMs, &x.CostState, &x.CostUSD, &x.FirstEvent, &x.LastEvent, &x.ContextTokens, &x.Route); err != nil {
			return LoopSummaryWindow{}, fmt.Errorf("store: scan a step summary: %w", err)
		}
		out.Steps = append(out.Steps, x)
	}
	if err := rows.Err(); err != nil {
		return LoopSummaryWindow{}, err
	}
	if len(out.Steps) > limit {
		out.Steps, out.HasOlder = out.Steps[:limit], true
	}
	return out, nil
}

// ReadLoopTotals sums a loop's step summaries.
func (s *Store) ReadLoopTotals(loopID string) (LoopTotals, error) {
	var t LoopTotals
	var spent, known int
	err := s.r.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(turns),0), COALESCE(SUM(tool_calls),0), COALESCE(SUM(tool_errors),0),
		       COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(reasoning_tokens),0), COALESCE(SUM(wall_ms),0),
		       SUM(spend_usd), COALESCE(SUM(turns > 0),0), COALESCE(SUM(turns > 0 AND spend_state = 'known'),0)
		FROM step_summary WHERE loop_id = ?`, loopID).
		Scan(&t.Steps, &t.Turns, &t.ToolCalls, &t.ToolErrors, &t.InputTokens, &t.OutputTokens, &t.ReasoningTokens, &t.WallMs, &t.CostUSD, &spent, &known)
	if err != nil {
		return LoopTotals{}, fmt.Errorf("store: sum a loop's step summaries: %w", err)
	}
	switch {
	case spent == 0 || known == 0 && t.CostUSD == nil:
		t.CostState = "unknown"
	case known == spent:
		t.CostState = "known"
	default:
		t.CostState = "partial"
	}
	return t, nil
}

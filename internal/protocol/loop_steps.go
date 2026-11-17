package protocol

import (
	"context"
	"encoding/json"
)

// `loop.steps` — one loop's step summaries, read by window.
//
// The page used to rebuild each step's totals from the event journal, which holds thousands of rows for a long loop,
// so only the newest steps were known on open. The daemon now keeps one summary row per step (turns, tool calls,
// tokens, cost, model time) and this method pages them: the newest window first, older ones with `before`. The
// loop's own totals ride the first page only.

// LoopStepsParams names the loop and the window. Before is a step id: only steps older than it come back.
type LoopStepsParams struct {
	ID     string `json:"id"`
	Before int64  `json:"before,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// StepSummaryView is one step's summary. Cost keeps its state: a figure nobody reported is "unknown", never zero.
type StepSummaryView struct {
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
	// ContextTokens is how much of its context window the step holds now (its latest call's input); ContextWindow
	// is that route's known input budget, 0 when unknown.
	ContextTokens int64  `json:"contextTokens"`
	Route         string `json:"route,omitempty"`
	ContextWindow int64  `json:"contextWindow,omitempty"`
}

// LoopTotalsView is the loop's sum across every step.
type LoopTotalsView struct {
	Steps           int      `json:"steps"`
	Turns           int      `json:"turns"`
	ToolCalls       int      `json:"toolCalls"`
	ToolErrors      int      `json:"toolErrors"`
	InputTokens     int64    `json:"inputTokens"`
	OutputTokens    int64    `json:"outputTokens"`
	ReasoningTokens int64    `json:"reasoningTokens"`
	WallMs          int64    `json:"wallMs"`
	CostUSD         *float64 `json:"costUsd,omitempty"`
	CostState       string   `json:"costState"`
}

// LoopStepsResult is one page, newest first.
type LoopStepsResult struct {
	Steps    []StepSummaryView `json:"steps"`
	HasOlder bool              `json:"hasOlder"`
	Totals   *LoopTotalsView   `json:"totals,omitempty"`
}

func (t *Table) handleLoopSteps(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	var p LoopStepsParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrInvalidScope.WithWhat("reading a loop's steps needs the loop id")
	}
	return mem.ReadLoopSteps(p)
}

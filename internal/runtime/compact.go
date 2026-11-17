package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// The estimate uses 4 UTF-8 bytes per token plus 8 tokens per message. Both
// constants are uncalibrated. We compact at 75% of ContextWindow, leaving an
// uncalibrated 25% safety margin for the next model reply and estimation error.
const estimatedBytesPerToken = 4
const estimatedMessageOverhead = 8
const compactionSafetyFraction = 0.75

func EstimateTokens(messages []model.Message) int {
	total := 0
	for _, message := range messages {
		total += estimatedMessageOverhead
		for _, block := range message.Content {
			bytes := len(block.Text) + len(block.Signature) + len(block.Name) + len(block.ID) + len(block.Input) + len(block.Content) + len(block.ToolUseID)
			total += int(math.Ceil(float64(bytes) / estimatedBytesPerToken))
		}
	}
	return total
}

type SelectResult struct {
	OK     bool
	Start  int // inclusive message index
	End    int // exclusive message index
	Reason string
}

// SelectRange finds the largest closed middle range. It never offers the
// model an inadmissible range: both sides of every tool pair stay together.
// A refusal happens before a summary call can spend money.
func SelectRange(messages []model.Message, budget int) SelectResult {
	if budget <= 0 {
		return SelectResult{Reason: "context budget is unknown"}
	}
	if len(messages) < 4 {
		return SelectResult{Reason: "no middle messages are available for compaction"}
	}
	// The first system and user messages are the task and its context.
	start := 2
	lastAssistant := -1
	for i, message := range messages {
		if message.Role == model.RoleAssistant {
			lastAssistant = i
		}
	}
	if lastAssistant <= start {
		return SelectResult{Reason: "no middle range before the last assistant turn"}
	}
	best := SelectResult{Reason: "no admissible closed tool-turn range fits the context budget"}
	bestSize := 0
	for i := start; i < lastAssistant; i++ {
		for j := i + 1; j <= lastAssistant; j++ {
			if !closedToolPairs(messages, i, j) {
				continue
			}
			size := EstimateTokens(messages[i:j])
			if size > bestSize {
				best, bestSize = SelectResult{OK: true, Start: i, End: j}, size
			}
		}
	}
	if !best.OK {
		return best
	}
	// Replacing the range with a bounded summary still needs to fit. T5
	// supplies the actual summary and checks the bound before replacement.
	if EstimateTokens(messages)-bestSize >= budget {
		return SelectResult{Reason: fmt.Sprintf("even the largest safe range leaves at least %d estimated tokens, above budget %d", EstimateTokens(messages)-bestSize, budget)}
	}
	return best
}

func closedToolPairs(messages []model.Message, start, end int) bool {
	uses := map[string]bool{}
	results := map[string]bool{}
	for i, message := range messages {
		inside := i >= start && i < end
		for _, block := range message.Content {
			if block.Kind == model.BlockToolUse && block.ID != "" {
				uses[block.ID] = inside
			}
			if block.Kind == model.BlockToolResult && block.ToolUseID != "" {
				results[block.ToolUseID] = inside
			}
		}
	}
	for id, useInside := range uses {
		if resultInside, answered := results[id]; answered && useInside != resultInside {
			return false
		}
	}
	return true
}

// A summary may use at most 1024 output tokens, or one eighth of the route's
// context window, whichever is smaller. This is an uncalibrated guess; an
// oversized result is refused rather than truncated into a misleading record.
const maxSummaryOutputTokens = 1024

type CompactionRecord struct {
	Start          int         `json:"start"`
	End            int         `json:"end"`
	EstimateBefore int         `json:"estimateBefore"`
	EstimateAfter  int         `json:"estimateAfter"`
	Summary        string      `json:"summary"`
	Usage          model.Usage `json:"usage"`
	Cost           model.Cost  `json:"cost"`
}

type CompactResult struct {
	OK     bool
	Reason string
	Record CompactionRecord
}

// Compact buys one summary through the injected model client. The selector
// runs first, so a refused range costs no model call. Spent accounting is kept
// even if the provider fails or writes an oversized summary.
func Compact(ctx context.Context, st *LoopState, deps LoopDeps, budget int) CompactResult {
	deps = deps.withDefaults()
	selected := SelectRange(st.Messages, budget)
	if !selected.OK {
		return CompactResult{Reason: selected.Reason}
	}
	before := EstimateTokens(st.Messages)
	selectedJSON, err := json.Marshal(st.Messages[selected.Start:selected.End])
	if err != nil {
		return CompactResult{Reason: fmt.Sprintf("cannot encode selected messages: %v", err)}
	}
	outputBound := st.Request.Route.ContextWindow / 8
	if outputBound <= 0 || outputBound > maxSummaryOutputTokens {
		outputBound = maxSummaryOutputTokens
	}
	request := model.ModelRequest{
		Route: st.Request.Route,
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("Summarize the following prior messages faithfully, preserving decisions, evidence, and tool outcomes. Return only the summary text.")}},
			{Role: model.RoleUser, Content: []model.Block{model.TextBlock(string(selectedJSON))}},
		},
		MaxOutputTokens: outputBound,
		ReasoningEffort: st.Request.ReasoningEffort,
	}
	deltas, results, errors := deps.Client.Stream(ctx, request)
	var summary strings.Builder
	for delta := range deltas {
		if delta.Kind == model.DeltaText {
			summary.WriteString(delta.Text)
		}
	}
	var callResult model.StreamResult
	for result := range results {
		callResult = result
	}
	var callErr error
	for err := range errors {
		if callErr == nil {
			callErr = err
		}
	}
	st.Usage = addUsage(st.Usage, callResult.Usage)
	st.Cost = st.Cost.Add(callResult.Cost)
	st.AdditionalCost = st.AdditionalCost.Add(callResult.Cost)
	if callErr != nil {
		return CompactResult{Reason: fmt.Sprintf("summary call failed: %v", callErr)}
	}
	if summary.Len() == 0 {
		return CompactResult{Reason: "summary was empty"}
	}
	if int(math.Ceil(float64(summary.Len())/estimatedBytesPerToken)) > outputBound {
		return CompactResult{Reason: fmt.Sprintf("summary exceeded its %d estimated-token bound", outputBound)}
	}
	replacement := model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock("Earlier step context, compacted: " + summary.String())}}
	next := make([]model.Message, 0, len(st.Messages)-(selected.End-selected.Start)+1)
	next = append(next, st.Messages[:selected.Start]...)
	next = append(next, replacement)
	next = append(next, st.Messages[selected.End:]...)
	after := EstimateTokens(next)
	if after >= budget {
		return CompactResult{Reason: fmt.Sprintf("summary still leaves %d estimated tokens above budget %d", after, budget)}
	}
	st.Messages = next
	record := CompactionRecord{Start: selected.Start, End: selected.End, EstimateBefore: before,
		EstimateAfter: after, Summary: summary.String(), Usage: callResult.Usage, Cost: callResult.Cost}
	st.Compactions = append(st.Compactions, record)
	// S33 T5 (TL7/D3): the compaction gains a queryable step_compaction row
	// and a durable event row — WITHOUT touching Checkpoint.Compactions,
	// which stays the byte-identical replay unit it always was. A second
	// home for the same fact, additive: the blob is what a resume replays,
	// the row is what the rollup sums and `history` queries. Nil-guarded as
	// the turn capture is (I2); a failure is an accounting hole SPOKEN, never
	// a compaction undone — the transcript has ALREADY been rewritten, so the
	// compaction stands regardless, and discarding the record would leave the
	// transcript shrunk with no account that it ever was (the worse wrong).
	if deps.Telemetry != nil {
		if err := deps.Telemetry.RecordCompaction(ctx, st.Request.StepID, record); err != nil {
			st.emit(deps, StepEvent{Kind: EventTelemetryError, Iteration: len(st.Compactions),
				Summary: fmt.Sprintf("telemetry: compaction over messages %d–%d was not recorded: %v",
					record.Start, record.End, err), IsError: true})
		}
	}
	cost := callResult.Cost
	st.emit(deps, StepEvent{Kind: EventStepCompacted, Start: selected.Start, End: selected.End,
		EstimateBefore: before, EstimateAfter: after, Cost: &cost,
		Summary: fmt.Sprintf("compacted messages %d–%d", selected.Start, selected.End-1)})
	return CompactResult{OK: true, Record: record}
}

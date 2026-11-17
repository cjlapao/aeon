package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// RecoveryReceipt is the storage-neutral view of an unsafe call's receipt.
type RecoveryReceipt struct {
	ToolUseID string
	Tool      string
	ArgDigest string
	Completed bool
	Result    *string
	IsError   bool
}

type RecoveryFailure struct {
	Kind      string `json:"kind"`
	Tool      string `json:"tool,omitempty"`
	ToolUseID string `json:"toolUseId,omitempty"`
	Detail    string `json:"detail"`
}

// RecoverStep applies a previously inspected plan and then enters ResumeStep.
// Its caller persists the final outcome; the checkpoint writer preserves each
// intermediate result if the daemon dies during recovery.
func RecoverStep(ctx context.Context, req ResumeRequest, plan RecoveryPlan, opts RunOpts) StepResult {
	req.Recovery = true
	if plan.Case == CaseUnclassifiable {
		return recoveryFailed(req, opts, plan.Reason)
	}
	for _, call := range plan.Calls {
		if call.Action == FailStep {
			return recoveryFailed(req, opts, call.Reason)
		}
	}
	if plan.Case == CaseC {
		state := &LoopState{Request: req.Request, Messages: append([]model.Message(nil), req.Messages...),
			Manifest: &req.Manifest, Usage: req.Usage, Cost: req.Cost, Compactions: req.Compactions}
		deps := LoopDeps{Client: opts.Client, Tools: opts.Tools, Sink: newGuardedSink(opts.Sink),
			Receipts: opts.Receipts, Checkpoint: opts.Checkpoint, Policy: gateForStep(opts, req.Manifest), Telemetry: opts.Telemetry, Clock: opts.Clock,
			HandoffSpec: opts.HandoffSpec, StatusField: opts.StatusField, Budget: opts.Budget}.withDefaults()
		// Recovery re-runs tools on THIS state before entering ResumeStep, so
		// this path snapshots its own policy instant once — every re-run call
		// is decided at one instant, exactly as a normal step decides its calls
		// at one (S10).
		state.policyAt = deps.Clock()
		results := make([]model.Block, 0, len(plan.Calls))
		// A previous checkpoint can already contain results for sibling calls.
		if n := len(state.Messages); n > 0 && state.Messages[n-1].Role == model.RoleUser {
			for _, block := range state.Messages[n-1].Content {
				if block.Kind == model.BlockToolResult {
					results = append(results, block)
				}
			}
		}
		// recordResults mirrors the recovered results into the transcript's
		// one tool-result message (created when the first one lands) and
		// persists the step, so a daemon death between two re-runs never
		// loses the earlier result.
		recordResults := func() error {
			if n := len(state.Messages); n > 0 && state.Messages[n-1].Role == model.RoleUser {
				state.Messages[n-1].Content = append([]model.Block(nil), results...)
			} else {
				state.Messages = append(state.Messages, model.Message{Role: model.RoleUser, Content: append([]model.Block(nil), results...)})
			}
			return state.checkpoint(ctx, deps)
		}
		for ci, call := range plan.Calls {
			var result model.Block
			if call.Action == RecoverResult {
				result = model.ToolResultBlock(call.ToolUseID, call.Result, call.IsError)
			} else {
				use, ok := findToolUse(state.Messages, call.ToolUseID)
				if !ok {
					return recoveryFailed(req, opts, fmt.Sprintf("tool_use %s disappeared during recovery", call.ToolUseID))
				}
				var err error
				result, err = state.runTool(ctx, use, deps, state.Manifest.IterationsUsed)
				if state.pendingPermission != nil {
					// The gate asked about a re-run call (S11). Recovery
					// suspends exactly as the loop does — results already
					// recorded stay recorded, the blocked call stays
					// unanswered for the answer to resume — and NO FURTHER
					// RE-RUN PROCEEDS: the rest of the plan is closed
					// honestly HERE, now (S11 review round 1; this comment
					// used to promise the later re-runs were "decided when
					// the step next runs" — that path does not exist: after
					// the answer the step enters ResumeStep's RunLoop and
					// nobody revisits this plan). Entries whose unsafe
					// receipt already captured a result keep that captured
					// result — that call DID run before the crash, and any
					// other answer would be a lie. Entries still waiting to
					// re-run get closeUnrunTool's honest "did not run":
					// nothing dispatches behind a pending permission
					// question (G12 — re-running them here would commit the
					// work behind a verdict nobody has given yet), and the
					// model re-issues what it still wants after reading the
					// answer. Checked BEFORE the error and before the block
					// is used: an ask returns no block and no error.
					question := *state.pendingPermission
					state.pendingPermission = nil
					for _, rest := range plan.Calls[ci+1:] {
						if rest.Action == RecoverResult {
							results = append(results, model.ToolResultBlock(rest.ToolUseID, rest.Result, rest.IsError))
							continue
						}
						var restInput json.RawMessage
						if u, ok := findToolUse(state.Messages, rest.ToolUseID); ok {
							restInput = u.Input
						}
						results = append(results, state.closeUnrunTool(deps, rest.Tool, rest.ToolUseID,
							question.ToolUseID, restInput, state.Manifest.IterationsUsed))
					}
					if len(results) > 0 {
						if cerr := recordResults(); cerr != nil {
							return recoveryFailed(req, opts, fmt.Sprintf("recovered results could not be checkpointed: %v", cerr))
						}
					}
					return state.suspendWith(question)
				}
				if err != nil {
					return recoveryFailed(req, opts, err.Error())
				}
			}
			results = append(results, result)
			if err := recordResults(); err != nil {
				return recoveryFailed(req, opts, fmt.Sprintf("recovered result could not be checkpointed: %v", err))
			}
		}
		req.Messages = state.Messages
	}
	return ResumeStep(ctx, req, opts)
}

func recoveryFailed(req ResumeRequest, opts RunOpts, reason string) StepResult {
	manifest := req.Manifest
	state := &LoopState{Request: req.Request, Messages: req.Messages, Manifest: &manifest, Usage: req.Usage, Cost: req.Cost, Compactions: req.Compactions}
	deps := LoopDeps{Sink: newGuardedSink(opts.Sink), Clock: opts.Clock}.withDefaults()
	return state.failed(reason, deps)
}

func findToolUse(msgs []model.Message, id string) (model.Block, bool) {
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if block.Kind == model.BlockToolUse && block.ID == id {
				return block, true
			}
		}
	}
	return model.Block{}, false
}

type Case string

const (
	CaseA              Case = "A"
	CaseB              Case = "B"
	CaseC              Case = "C"
	CaseUnclassifiable Case = "unclassifiable"
)

type CallAction string

const (
	ReRun         CallAction = "rerun"
	RecoverResult CallAction = "recover_result"
	FailStep      CallAction = "fail_step"
)

type CallPlan struct {
	ToolUseID string
	Tool      string
	Action    CallAction
	Result    string
	IsError   bool
	Reason    string
}

type RecoveryPlan struct {
	Case   Case
	Reason string
	Calls  []CallPlan
}

// DetermineCase uses only its inputs. The caller validates the manifest and
// filters status='running' before invoking it.
func DetermineCase(msgs []model.Message, receipts []RecoveryReceipt, classes map[string]string) RecoveryPlan {
	if len(msgs) == 0 {
		if len(receipts) != 0 {
			return RecoveryPlan{Case: CaseUnclassifiable, Reason: "receipts exist without a transcript"}
		}
		return RecoveryPlan{Case: CaseB, Reason: "no model turn was committed"}
	}
	if verdict := model.ValidateTranscript(msgs); !verdict.OK {
		return RecoveryPlan{Case: CaseUnclassifiable, Reason: verdict.Reason}
	}
	uses := map[string]model.Block{}
	answered := map[string]bool{}
	lastAssistant := -1
	for i, msg := range msgs {
		if msg.Role == model.RoleAssistant {
			lastAssistant = i
		}
		for _, block := range msg.Content {
			if block.Kind == model.BlockToolUse {
				uses[block.ID] = block
			}
			if block.Kind == model.BlockToolResult {
				answered[block.ToolUseID] = true
			}
		}
	}
	byID := map[string]RecoveryReceipt{}
	for _, receipt := range receipts {
		use, ok := uses[receipt.ToolUseID]
		if !ok {
			return RecoveryPlan{Case: CaseUnclassifiable, Reason: fmt.Sprintf("receipt %q references an absent tool_use", receipt.ToolUseID)}
		}
		if _, duplicate := byID[receipt.ToolUseID]; duplicate {
			return RecoveryPlan{Case: CaseUnclassifiable, Reason: fmt.Sprintf("duplicate receipt for tool_use %q", receipt.ToolUseID)}
		}
		if receipt.Tool != use.Name {
			return RecoveryPlan{Case: CaseUnclassifiable, Reason: fmt.Sprintf("receipt for %q names %q instead of %q", receipt.ToolUseID, receipt.Tool, use.Name)}
		}
		byID[receipt.ToolUseID] = receipt
	}
	if lastAssistant < 0 {
		return RecoveryPlan{Case: CaseB, Reason: "no assistant turn was committed"}
	}
	plan := RecoveryPlan{Case: CaseC}
	for _, use := range msgs[lastAssistant].Content {
		if use.Kind != model.BlockToolUse || answered[use.ID] {
			continue
		}
		class, ok := classes[use.Name]
		if !ok || (class != "safe" && class != "detectable" && class != "unsafe") {
			return RecoveryPlan{Case: CaseUnclassifiable, Reason: fmt.Sprintf("tool %q has no re-run class", use.Name)}
		}
		call := CallPlan{ToolUseID: use.ID, Tool: use.Name, Action: ReRun}
		if class == "unsafe" {
			if receipt, exists := byID[use.ID]; exists {
				switch {
				case receipt.Completed && receipt.ArgDigest != toolArgDigest(use.Name, use.Input):
					call.Action, call.Reason = FailStep, fmt.Sprintf("digest-mismatch: %s receipt for %s does not match the tool call", use.Name, use.ID)
				case receipt.Completed && receipt.Result == nil:
					call.Action, call.Reason = FailStep, fmt.Sprintf("%s receipt for %s is completed without a result", use.Name, use.ID)
				case receipt.Completed:
					call.Action, call.Result, call.IsError = RecoverResult, *receipt.Result, receipt.IsError
				default:
					call.Action, call.Reason = FailStep, fmt.Sprintf("unknown-tool-outcome: %s receipt for %s was started but not completed", use.Name, use.ID)
				}
			}
		}
		plan.Calls = append(plan.Calls, call)
	}
	if len(plan.Calls) > 0 {
		var descriptions []string
		for _, call := range plan.Calls {
			if call.Action == FailStep {
				plan.Reason = call.Reason
				return plan
			}
			if call.Action == RecoverResult {
				descriptions = append(descriptions, call.Tool+" receipt completed; using captured result")
			}
			if call.Action == ReRun {
				descriptions = append(descriptions, call.Tool+" has no captured result; re-running")
			}
		}
		plan.Reason = strings.Join(descriptions, "; ")
		return plan
	}
	for _, block := range msgs[lastAssistant].Content {
		if block.Kind == model.BlockToolUse {
			return RecoveryPlan{Case: CaseA}
		}
	}
	if msgs[len(msgs)-1].Role == model.RoleAssistant {
		return RecoveryPlan{Case: CaseA}
	}
	return RecoveryPlan{Case: CaseB, Reason: "the last tool turn completed; re-issue the model call"}
}

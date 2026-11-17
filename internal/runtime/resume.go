package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

const changedPathLimit = 10

// ResumeRequest contains only values a caller can reconstruct from storage.
// Request supplies the step's static route, handler, tool specs and workspace.
// Messages and Manifest are the persisted snapshot, never reassembled.
type ResumeRequest struct {
	Request     StepRequest
	Messages    []model.Message
	Manifest    ContextManifest
	Question    PendingQuestion
	Answer      string
	Usage       model.Usage
	Cost        model.Cost
	Compactions []CompactionRecord
	// Recovery resumes a running step from its checkpoint without adding a
	// human answer or rehashing files that were never suspended.
	Recovery bool
}

// ResumeStep adds the human answer and enters the same RunLoop as RunStep.
// Context.ReadFile is injected for comparing frozen file hashes; it never
// replaces the original content already quoted in the transcript.
func ResumeStep(ctx context.Context, req ResumeRequest, opts RunOpts) StepResult {
	sink := newGuardedSink(opts.Sink)
	manifest := req.Manifest
	state := &LoopState{
		Request: req.Request, Messages: append([]model.Message(nil), req.Messages...),
		Manifest: &manifest, Usage: req.Usage, Cost: req.Cost, AdditionalCost: model.Cost{State: model.CostKnown},
		Compactions: append([]CompactionRecord(nil), req.Compactions...),
	}
	deps := LoopDeps{Client: opts.Client, Tools: opts.Tools, Sink: sink, Receipts: opts.Receipts, Checkpoint: opts.Checkpoint, Steers: opts.Steers, MailboxDrain: opts.MailboxDrain, MailboxAsk: opts.MailboxAsk, Todo: opts.Todo, Policy: gateForStep(opts, manifest), Telemetry: opts.Telemetry, Clock: opts.Clock,
		HandoffSpec: opts.HandoffSpec, StatusField: opts.StatusField, Budget: opts.Budget}.withDefaults()
	// Resume is a step start for policy purposes: the instant is snapshotted
	// ONCE here (not per tool call), on this path exactly as RunStep does on
	// its own (S10). A resumed step may straddle hours of suspension; it is
	// decided at the instant the resume began.
	state.policyAt = deps.Clock()

	if validation := model.ValidateTranscript(state.Messages); !validation.OK {
		return state.failed("cannot resume an invalid transcript: "+validation.Reason, deps)
	}
	if !req.Recovery && !hasPendingQuestion(state.Messages, req.Question.ToolUseID, req.Question.Kind) {
		return state.failed(fmt.Sprintf("question tool_use %q is not pending in the transcript", req.Question.ToolUseID), deps)
	}
	if !req.Recovery && len(manifest.Files) > 0 && opts.Context.ReadFile == nil {
		return state.failed("cannot compare declared files on resume: no workspace reader was supplied", deps)
	}

	// A human answered: the time the step spent waiting for them is not working
	// time, so the working window starts again. (Recovery is a crash, not an
	// answer: it keeps its deadline.)
	if !req.Recovery && !manifest.DeadlineAt.IsZero() {
		window := time.Duration(manifest.DeadlineWindowMs) * time.Millisecond
		if window <= 0 {
			window = DefaultDeadline
		}
		manifest.DeadlineAt = deps.Clock().Add(window)
	}

	if !req.Recovery {
		changes := Rehash(manifest, opts.Context)
		for _, path := range changes.Changed {
			manifest.ObservedChanges = append(manifest.ObservedChanges, FileChange{Path: path, Kind: "changed"})
		}
		for _, path := range changes.Missing {
			manifest.ObservedChanges = append(manifest.ObservedChanges, FileChange{Path: path, Kind: "missing"})
		}
		content := make([]model.Block, 0, 2)
		if changes.Any() {
			content = append(content, model.TextBlock(changedFileNote(changes)))
		}
		// An empty answer is a real answer. The tool_result block keeps the empty
		// string and the model decides what it means.
		result := model.ToolResultBlock(req.Question.ToolUseID, req.Answer, false)
		if req.Question.Kind == "permission" {
			// A permission question names the BLOCKED CALL's block, and the answer decides that call:
			// approve runs it, once, now; anything else leaves it unrun. What the model reads next is
			// the call's real result (or the refusal), never the bare word "approve".
			if use, ok := blockedCall(state.Messages, req.Question.ToolUseID); ok {
				result = state.answerPermission(ctx, deps, use, req.Answer)
			}
		}
		content = append(content, result)
		state.Messages = append(state.Messages, model.Message{Role: model.RoleUser, Content: content})
		if err := state.checkpoint(ctx, deps); err != nil {
			return state.failed(fmt.Sprintf("the answer could not be checkpointed: %v", err), deps)
		}
	}
	if req.Recovery && len(state.Messages) > 0 && state.Messages[len(state.Messages)-1].Role == model.RoleAssistant {
		last := model.ModelRequest{Route: req.Request.Route, Messages: state.Messages,
			Tools: req.Request.Tools, MaxOutputTokens: req.Request.Handler.MaxOutputTokens,
			ReasoningEffort: req.Request.ReasoningEffort}
		result := state.finish(ctx, last, deps, state.Manifest.IterationsUsed)
		if err := state.checkpoint(ctx, deps); err != nil {
			return state.failed(fmt.Sprintf("the recovered state could not be checkpointed: %v", err), deps)
		}
		result.SinkFailures = sink.failures()
		return result
	}
	result := runGuarded(ctx, state, deps)
	if err := state.checkpoint(ctx, deps); err != nil {
		return state.failed(fmt.Sprintf("the resumed state could not be checkpointed: %v", err), deps)
	}
	result.SinkFailures = sink.failures()
	return result
}

// hasPendingQuestion is the answerability check on the resume path.
//
// A question tool answers its OWN block, so the pending block is the one
// named "question". A permission question (S11) names the BLOCKED CALL's
// block — the bash (or other) tool_use the gate asked about — and carries
// that tool's name, so for that kind the id alone identifies the block: the
// answer becomes its tool_result, exactly as S6's answer path answers the
// question tool. An ask_agent question (S70 MB4) is the permission shape from
// the other side: the blocked call is the ask itself, and its answer — the
// peer's reply, delivered through the mailbox — becomes its tool_result when
// the step resumes cold. What the verdict MEANS (an approval that runs the
// command) is the daemon's token path, not this check; the runtime's duty is
// that a suspended question is genuinely answerable and its block genuinely
// answered, so no transcript is left with a dangling tool_use.
func hasPendingQuestion(messages []model.Message, id string, kind string) bool {
	if id == "" {
		return false
	}
	var found, answered bool
	for _, message := range messages {
		for _, block := range message.Content {
			if block.Kind == model.BlockToolUse && block.ID == id &&
				(kind == "permission" || block.Name == "question" || block.Name == "ask_agent") {
				found = true
			}
			if block.Kind == model.BlockToolResult && block.ToolUseID == id {
				answered = true
			}
		}
	}
	return found && !answered
}

func changedFileNote(changes RehashResult) string {
	var lines []string
	if len(changes.Changed) > 0 {
		lines = append(lines, "Changed while this step waited: "+limitedPaths(changes.Changed))
	}
	if len(changes.Missing) > 0 {
		lines = append(lines, "Missing while this step waited: "+limitedPaths(changes.Missing))
	}
	return strings.Join(lines, "\n") + ". The original file content remains in the earlier transcript."
}

func limitedPaths(paths []string) string {
	named := paths
	if len(named) > changedPathLimit {
		named = named[:changedPathLimit]
	}
	out := strings.Join(named, ", ")
	if omitted := len(paths) - len(named); omitted > 0 {
		out += fmt.Sprintf(" (and %d more omitted)", omitted)
	}
	return out
}

// approvedCall is the single call a person approved: which block, which tool, and a digest of its exact
// arguments, so a block that differs in any argument is a different call and is not approved.
type approvedCall struct {
	ToolUseID string
	Tool      string
	Digest    string
}

// consumeApproval reports whether `use` is the call a person approved, and spends the approval: it
// answers true once, for exactly that block, and never again.
func (st *LoopState) consumeApproval(use model.Block) bool {
	a := st.approved
	if a == nil || a.ToolUseID != use.ID || a.Tool != use.Name || a.Digest != toolArgDigest(use.Name, use.Input) {
		return false
	}
	st.approved = nil
	return true
}

// blockedCall finds the tool_use block a permission question is about. Only a call the POLICY GATE
// asked about qualifies: the `question` and `ask_agent` blocks can carry a permission-kind question a
// model wrote itself, and approving one of those must never run anything.
func blockedCall(messages []model.Message, id string) (model.Block, bool) {
	for _, message := range messages {
		for _, block := range message.Content {
			if block.Kind == model.BlockToolUse && block.ID == id {
				if block.Name == "question" || block.Name == "ask_agent" {
					return model.Block{}, false
				}
				return block, true
			}
		}
	}
	return model.Block{}, false
}

// answerPermission turns a person's answer into the blocked call's tool_result. "approve" runs the call
// once through the ordinary dispatch (so the policy gate still judges it: an explicit deny rule is not
// softened, only the ask is answered); every other answer leaves it unrun and says a person said no.
func (st *LoopState) answerPermission(ctx context.Context, d LoopDeps, use model.Block, answer string) model.Block {
	if answer != "approve" {
		return model.ToolResultBlock(use.ID, "a person denied this call; it was not run", true)
	}
	st.approved = &approvedCall{ToolUseID: use.ID, Tool: use.Name, Digest: toolArgDigest(use.Name, use.Input)}
	block, err := st.runTool(ctx, use, d, st.Manifest.IterationsUsed)
	st.approved = nil
	if err != nil {
		return model.ToolResultBlock(use.ID, "a person approved this call, but it could not run: "+err.Error(), true)
	}
	if st.pendingPermission != nil {
		// The gate asked again instead of accepting the approval. Nothing ran; never loop on it.
		st.pendingPermission = nil
		return model.ToolResultBlock(use.ID, "a person approved this call, but the policy still asked and it was not run", true)
	}
	return block
}

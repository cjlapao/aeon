package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/stepctx"
)

// The bounded loop: **one implementation** (I4).
//
// S6 adds suspension and S7 adds recovery, and both build on this. A second
// copy would drift, and the drift is what breaks resume — a resumed step
// running a slightly different loop produces a transcript the original could
// not have produced.
//
// # Three properties that are easy to get subtly wrong
//
// **Tools execute SERIALLY, in emission order** (K9, D3). Not for safety — for
// reproducibility: a parallel loop produces transcripts nobody can reproduce
// and file writes in an order nobody chose. The test spy records ENTRY AND EXIT
// and asserts no overlap, because a spy recording only call order passes
// against an `errgroup.Wait()` implementation.
//
// **The retry is tool-free** (K4, D4), and the assertion is on the REQUEST
// OBJECT rather than on a count. A retry offered tools can start new work — a
// step that already did its job doing it again, differently, while ostensibly
// restating its report. A retry offered tools it happens not to use is still
// wrong, and a test that counts attempts cannot see it.
//
// **A bound yields `blocked`, naming the bound and its value** (O2, D5). A
// caller seeing `failed` files a bug report; one seeing `blocked` raises the
// budget. Conflating them sends every over-long step to the wrong response.

// LoopState is what the loop carries between iterations.
//
// The manifest is carried by POINTER because `IterationsUsed` is incremented on
// it rather than in a local (T8's point 3) — which is what lets S6 persist the
// count across a suspension without changing this file.
type LoopState struct {
	Request  StepRequest
	Messages []model.Message
	Manifest *ContextManifest

	// badQuestions counts badly formed question calls in a row (see maxBadQuestions); a good one resets it.
	badQuestions int

	// Usage and Cost accumulate across iterations. A step that failed on its
	// fourth call still spent the first three (M15).
	Usage model.Usage
	Cost  model.Cost
	// AdditionalCost counts calls made during this invocation. Recovery uses it
	// to show reissued work separately from the pre-crash accounting.
	AdditionalCost model.Cost

	// ThinkingDropped accumulates the request-direction thinking drops of
	// every call this step made (S18 T5, M4): the transcript kept every
	// block; this is the count of what the wires did not carry.
	ThinkingDropped int

	// PendingRoute is a route change a steer asked for MID-step (S18 T8,
	// O5/N9): refused for this step — a tool call from one provider cannot
	// be answered in another's format — and deferred to the loop, which
	// applies it before the NEXT dispatch. PendingEffort rides with its
	// route when they were steered together; alone, effort is the same
	// route's dial and applies immediately.
	PendingRoute  *model.Route
	PendingEffort string

	// S75: model calls since the agent last touched its todo list, and how many
	// times this step was sent back for finishing with items open. In memory
	// only: a resumed step starts a fresh window, which is the safe direction.
	sinceTodo   int
	todoBounces int

	// Events is what happened, appended as it happens.
	Events      []StepEvent
	Compactions []CompactionRecord

	// policyAt is the instant the step began, snapshotted ONCE from the
	// injected clock at step start by the entry point (RunStep, ResumeStep,
	// RecoverStep). Every policy decision in the step is made against THIS
	// instant — never a per-call time.Now — so two identical tool calls cannot
	// straddle an expiry and disagree (the transcript would otherwise show one
	// allowed and an identical one refused with nothing to explain it, S10).
	// Every production entry point snapshots it; a hand-built LoopState (test
	// scaffolding only) leaves it zero, and a step that skipped its entry point
	// then shows up deciding at the zero instant.
	policyAt time.Time

	// retried records that the one tool-free re-ask has been used.
	retried bool
	// cutoffs counts consecutive turns the output cap ended before the model finished (see outputCutOff).
	cutoffs int

	// pendingPermission is the ask suspension's channel out of runTool
	// (S11). runTool's signature is (model.Block, error) — it returns a
	// tool_result or an internal failure, and a question is neither — so an
	// asked decision parks the question here and the caller picks it up
	// IMMEDIATELY after runTool returns, in the one place RunLoop already
	// centralizes suspension for the question tool. A per-state field rather
	// than a sentinel error because the question path already centralizes in
	// the caller (T12's decision (b)): an error-shaped suspension would read
	// as a defect to every `if err != nil` between here and the return.
	//
	// Nil except between an asked gate decision and its consumption. Both
	// routes into runTool — RunLoop's ordinary loop and RecoverStep's CaseC
	// re-runs — check it before touching the returned block.
	pendingPermission *PendingQuestion

	// toolStarts remembers when each running tool call began, to report its duration on the end event. In memory
	// only: a step resumed after a restart reports no duration for a call that began before it.
	toolStarts map[string]time.Time

	// approved is the ONE call a person approved by answering a permission question "approve". Set
	// only by ResumeStep from the stored question, for the blocked block it names; consumed by the
	// first matching dispatch and then gone, so approving one call never approves the next.
	approved *approvedCall
}

// ToolCaller is the toolset, as the loop needs it.
//
// An interface rather than `tools.Toolset`, so `internal/runtime` does not
// import `internal/tools` — the dependency runs the other way (tools imports
// nothing from runtime either; `ToolContext` is declared in tools). It is also
// what lets the loop's tests use spies that record entry and exit times.
type ToolCaller interface {
	Call(ctx context.Context, name string, input json.RawMessage) ToolOutcome
	Granted() []string
}

// ToolOutcome is a tool's result, in the loop's terms.
type ToolOutcome struct {
	Content string
	IsError bool
	Summary string

	// Path and the line counts ride the event stream only (S31 D8) — the
	// tool's model-facing Content stays byte-identical (N5). A tool not
	// about a file leaves them empty; the sink copies them onto
	// `step.tool.end` verbatim, never re-derives them.
	Path         string
	LinesAdded   int
	LinesRemoved int

	// ExitCode/WallMs are S33 T6 (D6): the exit status and spawn-measured
	// wall a COMMAND reported — set by `bash` (nil/0 for every other tool,
	// and nil even for bash when the command never STARTED, which is not a
	// run). They cross at the one conversion point (daemon's toolCaller),
	// same rule as the write counts: the loop reads facts, never re-derives
	// them. The model never sees these — Content is untouched (N5).
	ExitCode *int
	WallMs   int64
}

// Streamer is the model client, as the loop needs it.
//
// Declared here rather than imported from `internal/protocol`, which has its
// own identical interface: `runtime` and `protocol` are both leaves over
// `model`, and having one import the other for a two-line interface would make
// a dependency out of a coincidence. Go's structural typing means
// `*model.Client` satisfies both.
type Streamer interface {
	Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error)
}

// LoopDeps are the injected dependencies (I3).
type LoopDeps struct {
	Client     Streamer
	Tools      ToolCaller
	Sink       EventSink
	Receipts   ReceiptWriter
	Checkpoint CheckpointWriter

	// Steers is the turn-boundary drain (S31 T4, D3/D5/LW1): a step asks this
	// source before a model call and again after each saved assistant turn
	// before any of its tools run. Nil means the feature is OFF — I2: every
	// pre-existing caller runs exactly what it ran before, and the injection
	// rules can never surprise a caller that never asked for them.
	Steers SteerSource

	// MailboxDrain is the peer-message drain (S70 MB4): asked at the SAME
	// pre-call boundary LW1 puts steers at — the person's words fold first,
	// the peers' after — and the rows flip delivered inside the checkpoint
	// that persists the fold (D4's law, inherited). Nil means the step has
	// no mailbox: the pre-S70 path, byte for byte (I2). The adapter closes
	// over the step's scope exactly as the steer adapter closes over its
	// queue; the runtime never learns what a scope is.
	MailboxDrain MailboxDrain

	// Todo is the nudge and completion-guard door (S75 TD9/TD10). Nil: the step
	// runs exactly as it did before the feature (I2).
	Todo TodoPolicy

	// MailboxAskDoor files `ask_agent` calls (S70 MB1/MB4): recognised by
	// name like `question` (K18 — the tool's Run panics), the RAW arguments
	// go to the door, and the ONE parser runs there before anything
	// persists (AT1). The outcome says whether the turn keeps its answer or
	// the step suspends through suspendWith, the ONE constructor. Nil means
	// the step cannot ask; the name then rides the ordinary tool path and
	// the caller's toolset answers it honestly (not granted, not offered).
	MailboxAsk MailboxAskDoor

	// Policy is the S10 policy gate, injected exactly as Receipts and
	// Checkpoint arrive (K22: the runtime knows a gate exists, not what a
	// Policy is). Nil means no enforcement — pre-S10 callers keep running;
	// production always wires one (daemon.New, and the construction sites in
	// protocol/step.go and daemon/recover.go).
	Policy PolicyGate

	// Telemetry is the S33 capture seam (TL12): the sink a step reports its
	// per-turn usage, compactions and tool activity to, exactly as Steers
	// and Policy arrive — the runtime knows a recorder exists, not what a
	// rollup row is (the concrete adapter over the store lives in
	// internal/daemon, which imports both sides). Nil means the feature is
	// OFF: I2, verbatim from Steers — every pre-existing caller runs exactly
	// what it ran before, and no capture rule can surprise a caller that
	// never asked for one. A capture failure never fails the step: what a
	// step costs is observed, not transactional with what it does.
	Telemetry TelemetrySink

	// Clock is the deadline's source. Injected so a test can exceed a deadline
	// without waiting for one.
	Clock func() time.Time

	// HandoffSpec is what a valid report must satisfy.
	HandoffSpec HandoffSpec

	// StatusField names the handoff field carrying the outcome.
	StatusField string

	// Budget is the S55 T7 in-step check (E2): the daemon resolves the
	// step's applicable budgets against current spend, evaluates them with
	// the pure engine, and hands the verdict in as plain data (the runtime
	// imports neither internal/budget nor internal/store —
	// TestRuntimeStaysPure). Asked at the same before-call boundary the
	// steers are, BEFORE them: a cap hit must not consume a steer (a steer
	// folded into a call that never happens would be lost). Nil means
	// unenforced — the pre-T7 path, byte for byte (I2). A verdict that is
	// not OK with a hit suspends the step (pause_reason='cap', no question
	// row); a verdict not OK without a hit fails the step closed — and an
	// ERROR from the seam fails it too, because a cap the engine could not
	// read must not be a cap the step spends past.
	Budget func(ctx context.Context) (BudgetVerdict, error)
}

func (d LoopDeps) withDefaults() LoopDeps {
	if d.Clock == nil {
		d.Clock = time.Now
	}
	if d.Sink == nil {
		d.Sink = DiscardSink{}
	}
	if d.StatusField == "" {
		d.StatusField = "status"
	}
	return d
}

// RunLoop runs the bounded loop and returns one of the four outcomes.
//
// It never returns `suspended` (K21, D14). S6 adds that path; a test asserts no
// result from here carries it.
func RunLoop(ctx context.Context, st *LoopState, d LoopDeps) StepResult {
	d = d.withDefaults()

	for {
		// # Bounds are checked BEFORE the call, not after
		//
		// Checking after would spend a request to discover there was no room
		// for it — and the request is the expensive part. A step at its
		// iteration limit should cost nothing more.
		// A MaxIterations of 0 is "no bound" (a loop runs until it is done); the
		// wall-clock deadline below still applies.
		bound := st.Manifest.MaxIterations
		if bound != 0 && st.Manifest.IterationsUsed >= bound {
			return st.blocked(fmt.Sprintf("exceeded MaxIterations (%d)", bound), d)
		}
		if !st.Manifest.DeadlineAt.IsZero() && d.Clock().After(st.Manifest.DeadlineAt) {
			return st.blocked(fmt.Sprintf("exceeded DeadlineAt (%s)",
				st.Manifest.DeadlineAt.UTC().Format(time.RFC3339)), d)
		}
		if err := ctx.Err(); err != nil {
			// Cancellation is `failed` rather than `blocked`: the step did not
			// run out of room, something stopped it. A caller that cancelled
			// knows why, and one that did not needs to see that it happened.
			return st.failed(fmt.Sprintf("the step was cancelled: %v", err), d)
		}
		// # S55 T7 (E2): the in-step budget check, at the same before-call
		// boundary the steers are — BEFORE them, for the steer's own reason:
		// a cap hit must not consume a steer (a steer folded into a call
		// that never happens would be lost, and the steer's queue would
		// have drained it). A hit suspends the step (pause_reason='cap',
		// no question row — a cap has no one to ask); a non-hit refusal and
		// a seam ERROR both fail the step closed: a cap the engine could
		// not read must not be a cap the step spends past (OD6: the hit is
		// a suspension, NOT OutcomeBlocked — the outcome vocabulary is the
		// model's, the cap is the engine's).
		if d.Budget != nil {
			verdict, berr := d.Budget(ctx)
			if berr != nil {
				return st.failed(fmt.Sprintf("the budget check could not run: %v", berr), d)
			}
			if !verdict.OK {
				if verdict.CapHit != nil {
					return st.suspendCapped(verdict.Reason, verdict.CapHit)
				}
				return st.failed(fmt.Sprintf("the budget check refused the step: %s", verdict.Reason), d)
			}
		}
		// # The first boundary: before the call (S31 T4, LW1)
		//
		// The in-flight model call always finishes; THIS is the first moment a
		// new instruction may speak. Before the request is built, so the steer
		// rides INSIDE the next call rather than beside it; before the window
		// is measured, so injected text is paid for like any other content
		// (a steer buys nothing — Q4 — including headroom); and the delivery
		// lands in the checkpoint below this block, the same transaction as
		// the transcript containing it (D4). The resumed step's first
		// iteration rides this path like any other boundary (S7).
		if d.Steers != nil {
			if steers := d.Steers.Pending(ctx); len(steers) > 0 {
				for _, s := range steers {
					st.Messages = AppendUserTurn(st.Messages, s.Text, s.Attachments)
				}
				st.applySteerOverrides(d, steers, st.Manifest.IterationsUsed, true)
				ids := steerIDs(steers)
				st.emitSteersDelivered(d, steers, st.Manifest.IterationsUsed, nil)
				if err := st.checkpointWithSteers(ctx, d, ids); err != nil {
					return st.failed(fmt.Sprintf("the steered turn could not be checkpointed: %v", err), d)
				}
			}
		}
		// The peers' messages arrive at the SAME boundary, after the
		// person's: a message from the operator outranks a message from a
		// colleague, and both speak before the model does (S70 MB4). One
		// read per boundary — an adapter that could not read hands back
		// nil, delivering nothing THIS boundary and losing nothing (the
		// steer source's honest reading, inherited) — and the rows flip
		// delivered in the checkpoint transaction that persists the fold,
		// which is the only shape that makes "pending with its text
		// already in the transcript" unrepresentable (D4, quoted).
		if d.MailboxDrain != nil {
			if msgs := d.MailboxDrain.Pending(ctx); len(msgs) > 0 {
				ids := make([]int64, 0, len(msgs))
				for _, m := range msgs {
					st.Messages = AppendUserTurn(st.Messages, mailboxWireText(m), nil)
					ids = append(ids, m.ID)
					st.emit(d, StepEvent{Kind: EventAgentMessageDelivered, Tool: "ask_agent",
						Summary: m.FromAgent, MessageID: m.ID, Iteration: st.Manifest.IterationsUsed})
				}
				if err := st.checkpointMailbox(ctx, d, ids); err != nil {
					return st.failed(fmt.Sprintf("the peer message could not be checkpointed: %v", err), d)
				}
			}
		}
		// S75 TD9: the nudge. An agent that has gone several model calls without
		// touching an open list is reminded once per window, by a runtime turn
		// (never the person's words), and the window restarts either way.
		if d.Todo != nil {
			if n := d.Todo.NudgeAfter(); n > 0 && st.sinceTodo >= n {
				if open, ok := d.Todo.ActiveOpen(ctx); ok && len(open) > 0 {
					st.Messages = AppendUserTurn(st.Messages, TodoNudgeText(open), nil)
					st.emit(d, StepEvent{Kind: EventTodoNudged, Reason: "stale", Summary: strings.Join(open, ","), Iteration: st.Manifest.IterationsUsed})
					if err := st.checkpoint(ctx, d); err != nil {
						return st.failed(fmt.Sprintf("the todo reminder could not be checkpointed: %v", err), d)
					}
				}
				st.sinceTodo = 0
			}
		}
		if window := st.Request.Route.ContextWindow; window > 0 {
			budget := int(float64(window) * compactionSafetyFraction)
			if EstimateTokens(st.Messages) > budget {
				compacted := Compact(ctx, st, d, budget)
				if !compacted.OK {
					return st.blocked("cannot compact: "+compacted.Reason, d)
				}
				// S75: the folded turns took the todo call with them; put the
				// list back so the agent continues it instead of re-planning.
				if d.Todo != nil {
					if text, ok := d.Todo.ActiveList(ctx); ok {
						st.Messages = AppendUserTurn(st.Messages, TodoAfterCompactionText(text), nil)
					}
				}
			}
		}

		st.Manifest.IterationsUsed++
		iteration := st.Manifest.IterationsUsed

		req := model.ModelRequest{
			Route:           st.Request.Route,
			Messages:        st.Messages,
			Tools:           st.Request.Tools,
			MaxOutputTokens: st.Request.Handler.MaxOutputTokens * (1 + st.cutoffs),
			// Carried on EVERY call, including the tool-free retry: a retry
			// that changed the model's reasoning would be a different model
			// answering the same question.
			ReasoningEffort: st.Request.ReasoningEffort,
		}

		turn, err := st.stream(ctx, req, d, iteration)
		if saveErr := st.checkpoint(ctx, d); saveErr != nil {
			return st.failed(fmt.Sprintf("the model turn could not be checkpointed: %v", saveErr), d)
		}
		if err != nil {
			return st.failed(fmt.Sprintf("the model call failed: %v", err), d)
		}

		// The assistant's turn joins the transcript whatever it contained —
		// before any tool runs, so a transcript read mid-step shows what was
		// said before what was done.
		if len(turn.blocks) > 0 {
			prov := model.NewProvenance(st.Request.Route)
			st.Messages = append(st.Messages, model.Message{
				Role: model.RoleAssistant, Content: turn.blocks, Provenance: &prov,
			})
			if saveErr := st.checkpoint(ctx, d); saveErr != nil {
				return st.failed(fmt.Sprintf("the assistant turn could not be checkpointed: %v", saveErr), d)
			}
		}

		// # The second boundary: steered means the tools do not run (LW2, LW3)
		//
		// The assistant turn is saved; nothing has been dispatched. If the
		// person steered in the meantime, every call in the turn — the
		// `question` tool alike, so the step does NOT suspend — is answered
		// with the not-run result, and their own text follows the results in
		// the SAME user message: the model reads what did not happen, then
		// what is now wanted, and decides. No permission gate is consulted —
		// a call that never runs was never asking. Then the loop continues:
		// this turn's report, handoff and tool results are NOT the step's
		// ending; the next call, carrying the steer, decides the ending.
		if d.Steers != nil {
			if steers := d.Steers.Pending(ctx); len(steers) > 0 {
				skipped := skippedToolUseIDs(turn.blocks)
				for _, b := range turn.blocks {
					if b.Kind != model.BlockToolUse {
						continue
					}
					st.emit(d, StepEvent{Kind: EventToolSkipped, Tool: b.Name, ToolUseID: b.ID,
						Reason: "steered", Iteration: iteration})
				}
				if blocks := steeredResults(turn.blocks, steers); len(blocks) > 0 {
					st.Messages = append(st.Messages, model.Message{Role: model.RoleUser, Content: blocks})
				}
				st.applySteerOverrides(d, steers, iteration, true)
				ids := steerIDs(steers)
				st.emitSteersDelivered(d, steers, iteration, skipped)
				if err := st.checkpointWithSteers(ctx, d, ids); err != nil {
					return st.failed(fmt.Sprintf("the steered turn could not be checkpointed: %v", err), d)
				}
				continue
			}
		}

		// S75: a `todo` call restarts the nudge window; any other turn ages it.
		touched := false
		for _, use := range turn.toolUses {
			if use.Name == "todo" {
				touched = true
			}
		}
		if touched {
			st.sinceTodo = 0
		} else {
			st.sinceTodo++
		}

		// A reply the output cap ended is not the model finishing: a model that thinks for the whole cap says nothing and
		// calls nothing, and treating that as the end asked for a report on work that never happened. It is told what
		// happened and given room (one more cap's worth per consecutive cut-off, at most three times); only then does
		// the ordinary ending take over.
		if len(turn.toolUses) == 0 && outputCutOff(turn.stopReason) && st.cutoffs < maxCutoffs {
			st.cutoffs++
			st.Messages = append(st.Messages, model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock(
				"Your last reply was cut off by the output limit before you finished, so nothing in it was acted on: no tool was " +
					"called and nothing was filed. Continue the work now. Keep your reasoning brief, and in THIS reply make the " +
					"tool call that does the next piece of work (or give the final report if everything is already done).")}})
			if err := st.checkpoint(ctx, d); err != nil {
				return st.failed(fmt.Sprintf("the cut-off notice could not be checkpointed: %v", err), d)
			}
			continue
		}
		if len(turn.toolUses) > 0 || !outputCutOff(turn.stopReason) {
			st.cutoffs = 0
		}

		if len(turn.toolUses) == 0 {
			// S75 TD10: a step may not claim success with items open — sent back
			// twice, then it completes and the record says so.
			if st.todoGuard(ctx, d) {
				if err := st.checkpoint(ctx, d); err != nil {
					return st.failed(fmt.Sprintf("the todo reminder could not be checkpointed: %v", err), d)
				}
				continue
			}
			return st.finish(ctx, req, d, iteration)
		}

		// # Serially, in emission order (K9)
		//
		// One result message carrying every result, because a tool_result must
		// follow its tool_use and a provider rejects a turn that interleaves
		// them differently.
		var questions []model.Block
		var ordinary []model.Block
		// asks ride their own bucket (S70): recognised by name like the
		// person's question tool (K18), filed through the door LAST — the
		// model does its own work first, then asks a peer. A door the step
		// does not carry leaves the name on the ordinary path, where the
		// step's own toolset answers it honestly (not granted, not offered).
		var asks []model.Block
		for _, use := range turn.toolUses {
			switch {
			case use.Name == "question" && offeredTool(req.Tools, "question"):
				questions = append(questions, use)
			case use.Name == "ask_agent" && d.MailboxAsk != nil && offeredTool(req.Tools, "ask_agent"):
				asks = append(asks, use)
			default:
				ordinary = append(ordinary, use)
			}
		}
		if len(questions) > 1 {
			return st.failed(fmt.Sprintf("the model asked %d questions in one turn; ask one at a time", len(questions)), d)
		}
		results := make([]model.Block, 0, len(ordinary))
		// A question the model sent badly is a tool error it can fix, not the end of the loop: it is answered as one, and the
		// model asks again. Only a model that keeps getting it wrong (three in a row) stops the step.
		if len(questions) == 1 {
			if _, qerr := pendingQuestion(questions[0]); qerr != nil {
				st.badQuestions++
				if st.badQuestions > maxBadQuestions {
					return st.failed(fmt.Sprintf("invalid question, sent %d times in a row: %v", st.badQuestions, qerr), d)
				}
				msg := fmt.Sprintf("invalid question: %v. Call question again. questions must be a JSON array of {\"text\": string, \"options\": [string]} (an array, not a string), or send just text and options for one question.", qerr)
				st.emit(d, StepEvent{Kind: EventToolEnd, Tool: "question", Summary: argumentSummary(questions[0].Input), Iteration: iteration, ToolUseID: questions[0].ID, Result: msg, IsError: true})
				results = append(results, model.ToolResultBlock(questions[0].ID, msg, true))
				if d.Checkpoint != nil {
					st.Messages = append(st.Messages, model.Message{Role: model.RoleUser})
					st.Messages[len(st.Messages)-1].Content = append([]model.Block(nil), results...)
					if saveErr := st.checkpoint(ctx, d); saveErr != nil {
						return st.failed(fmt.Sprintf("the tool result could not be checkpointed: %v", saveErr), d)
					}
				}
				questions = nil
			} else {
				st.badQuestions = 0
			}
		}
		for oi, use := range ordinary {
			if err := ctx.Err(); err != nil {
				return st.failed(fmt.Sprintf("the step was cancelled between tools: %v", err), d)
			}
			block, toolErr := st.runTool(ctx, use, d, iteration)
			if st.pendingPermission != nil {
				// The gate asked about this call and someone may answer (S11).
				// Suspend EXACTLY like the question tool: the sibling results
				// are recorded first (same as the path below), the blocked
				// tool_use stays unanswered — the answer resumes it — and the
				// step ends with no process (P2). No caching (QAP-13): this
				// runs per tool_use, so a second identical ask suspends again.
				question := *st.pendingPermission
				st.pendingPermission = nil
				// Every tool_use AFTER the blocked one closes NOW, honestly
				// (S11 review round 1): the later ordinary calls, and the
				// question blocks, which run only after all ordinary ones
				// and so never ran either. Leaving them silent is legal
				// only while this assistant turn stays the final one — the
				// answer appends a new turn, and format.go's rule then reads
				// each silent one as a lost tool result forever. See
				// closeUnrunTool for why the closure never dispatches.
				siblings := make([]model.Block, 0, len(ordinary)-oi-1+len(questions))
				siblings = append(siblings, ordinary[oi+1:]...)
				siblings = append(siblings, questions...)
				for _, sibling := range siblings {
					results = append(results, st.closeUnrunTool(d, sibling.Name, sibling.ID,
						question.ToolUseID, sibling.Input, iteration))
				}
				if len(results) > 0 {
					if d.Checkpoint == nil {
						st.Messages = append(st.Messages, model.Message{Role: model.RoleUser, Content: results})
					} else {
						// The same replace-wholesale shape the ordinary path
						// keeps under a checkpoint writer; the message itself
						// is still to be created when the ask hit the FIRST
						// block of the turn (results then holds exactly the
						// closures).
						if len(results) == len(siblings) {
							st.Messages = append(st.Messages, model.Message{Role: model.RoleUser})
						}
						st.Messages[len(st.Messages)-1].Content = append([]model.Block(nil), results...)
					}
					// Persist the closed turn the same way the per-result
					// path persists each result: a death between this return
					// and the caller's suspended-write must not leave the
					// checkpoint holding a half-closed turn (no-op writer-
					// less, like every checkpoint call here).
					if saveErr := st.checkpoint(ctx, d); saveErr != nil {
						return st.failed(fmt.Sprintf("the tool result could not be checkpointed: %v", saveErr), d)
					}
				}
				return st.suspendWith(question)
			}
			if toolErr != nil {
				return st.failed(toolErr.Error(), d)
			}
			results = append(results, block)
			if d.Checkpoint != nil {
				if len(results) == 1 {
					st.Messages = append(st.Messages, model.Message{Role: model.RoleUser})
				}
				st.Messages[len(st.Messages)-1].Content = append([]model.Block(nil), results...)
				if saveErr := st.checkpoint(ctx, d); saveErr != nil {
					return st.failed(fmt.Sprintf("the tool result could not be checkpointed: %v", saveErr), d)
				}
			}
		}
		// # The asks file last, and one of them may pause the step (S70 MB4)
		//
		// After the ordinary results and before the person's question: the
		// model does its own work, then asks a peer, and the question to the
		// person still outranks the ask below it. Raw arguments go to the
		// door (AT1: the ONE parser runs there, above the persist); a
		// refusal is an honest tool result, not a halt — an ask that comes
		// back refused is answered as refused and the step continues. A
		// suspension rides suspendWith, the ONE constructor (P2, and the
		// pinned single assignment site): sibling asks and the person's
		// question after the blocking one close unrun exactly as the
		// permission ask closes them (the closure never dispatches; the
		// blocked tool_use stays UNANSWERED — the peer's answer resumes it,
		// the permission answer's shape, quoted).
		for ai, use := range asks {
			if err := ctx.Err(); err != nil {
				return st.failed(fmt.Sprintf("the step was cancelled between asks: %v", err), d)
			}
			// suspendOn is the ask loop's ONE pause path: whatever asks the
			// step to stop — the door's wait:true or the gate's Asked — the
			// later asks and the person's question close unrun, the blocked
			// tool_use stays unanswered, and everything the pause owes is
			// recorded first (S11's sibling discipline, one copy of it).
			suspendOn := func(question PendingQuestion) StepResult {
				question.ToolUseID = use.ID
				if question.Kind == "" {
					question.Kind = "free" // the row's vocabulary; the door may name it, the loop guarantees it
				}
				siblings := append(append([]model.Block(nil), asks[ai+1:]...), questions...)
				before := len(results)
				for _, sib := range siblings {
					results = append(results, st.closeUnrunTool(d, sib.Name, sib.ID, use.ID, sib.Input, iteration))
				}
				if len(results) > 0 {
					if d.Checkpoint == nil {
						st.Messages = append(st.Messages, model.Message{Role: model.RoleUser, Content: results})
					} else {
						if before == 0 {
							st.Messages = append(st.Messages, model.Message{Role: model.RoleUser})
						}
						st.Messages[len(st.Messages)-1].Content = append([]model.Block(nil), results...)
						if saveErr := st.checkpoint(ctx, d); saveErr != nil {
							return st.failed(fmt.Sprintf("the ask suspension's tool results could not be checkpointed: %v", saveErr), d)
						}
					}
				}
				return st.suspendWith(question)
			}
			// The gate judges the ask exactly as it judges every sibling tool
			// call — the same Policy object, the same snapshotted instant,
			// the same ONE formatter (D12's parity comes structurally, never
			// from a second judgement the door could drift from). Tool names
			// the SUBJECT because the door files exactly the call decided;
			// identity is the server-side request's, never use.Input's (PL1).
			checkpointFailure := ""
			if d.Policy != nil {
				dec := d.Policy.Decide(PolicyGateRequest{
					Agent:          st.Request.Handler.Name,
					Tool:           "ask_agent",
					StepID:         st.Request.StepID,
					ModelRequested: true,
				}, st.policyAt)
				st.emit(d, StepEvent{Kind: EventPolicyDecided, Tool: "ask_agent",
					Summary: argumentSummary(use.Input), Iteration: iteration,
					ToolUseID: use.ID, Verdict: policyVerdict(dec), Reason: dec.Sentence})
				recordRefusal := func() {
					before := len(results)
					results = append(results, model.ToolResultBlock(use.ID, dec.Sentence, true))
					st.emit(d, StepEvent{Kind: EventToolEnd, Tool: "ask_agent",
						Summary: argumentSummary(use.Input), Iteration: iteration, ToolUseID: use.ID,
						Result: dec.Sentence, IsError: true})
					if d.Checkpoint != nil {
						if before == 0 {
							st.Messages = append(st.Messages, model.Message{Role: model.RoleUser})
						}
						st.Messages[len(st.Messages)-1].Content = append([]model.Block(nil), results...)
						if saveErr := st.checkpoint(ctx, d); saveErr != nil {
							// The caller checks: a refusal whose result cannot
							// be persisted is the half-state every result
							// path in this file refuses to carry.
							checkpointFailure = fmt.Sprintf("the gate refusal could not be checkpointed: %v", saveErr)
						}
					}
				}
				if dec.Asked {
					if st.Request.HasOperator {
						question := PendingQuestion{
							Kind:    "permission",
							Body:    permissionAskBody(dec, st.Request.Handler.Name, "ask_agent", ""),
							Options: PermissionOptions(),
						}
						st.emit(d, StepEvent{Kind: EventToolEnd, Tool: "ask_agent",
							Summary: argumentSummary(use.Input), Iteration: iteration, ToolUseID: use.ID,
							Result: "suspended: awaiting a permission answer"})
						return suspendOn(question)
					}
					// S11's no-answerer rule, verbatim: a question nobody
					// can answer is a deny, answered NOW, the verdict
					// sentence included, the question table empty by
					// construction (the adapter files question rows only
					// for suspended results).
					recordRefusal()
					if checkpointFailure != "" {
						return st.failed(checkpointFailure, d)
					}
					continue
				}
				if !dec.Allowed {
					recordRefusal()
					if checkpointFailure != "" {
						return st.failed(checkpointFailure, d)
					}
					continue
				}
			}
			if checkpointFailure != "" {
				return st.failed(checkpointFailure, d)
			}
			outcome, err := d.MailboxAsk.Ask(ctx, use.Input)
			if err != nil {
				before := len(results)
				results = append(results, mailboxAskResult(use, err.Error(), true))
				if d.Checkpoint != nil {
					if before == 0 {
						st.Messages = append(st.Messages, model.Message{Role: model.RoleUser})
					}
					st.Messages[len(st.Messages)-1].Content = append([]model.Block(nil), results...)
					if saveErr := st.checkpoint(ctx, d); saveErr != nil {
						return st.failed(fmt.Sprintf("the ask refusal could not be checkpointed: %v", saveErr), d)
					}
				}
				continue
			}
			if outcome.Suspends {
				if outcome.Question == nil {
					return st.failed("the ask door reported a suspension with no question to make it visible (D16)", d)
				}
				st.emit(d, StepEvent{Kind: EventAgentMessageAsked, Tool: "ask_agent",
					Summary: outcome.Question.Body, MessageID: outcome.MessageID, Iteration: iteration})
				return suspendOn(*outcome.Question)
			}
			before := len(results)
			results = append(results, mailboxAskResult(use, outcome.Result, false))
			if d.Checkpoint != nil {
				if before == 0 {
					st.Messages = append(st.Messages, model.Message{Role: model.RoleUser})
				}
				st.Messages[len(st.Messages)-1].Content = append([]model.Block(nil), results...)
				if saveErr := st.checkpoint(ctx, d); saveErr != nil {
					return st.failed(fmt.Sprintf("the ask could not be checkpointed: %v", saveErr), d)
				}
			}
			st.emit(d, StepEvent{Kind: EventAgentMessageAsked, Tool: "ask_agent",
				Summary: outcome.Result, MessageID: outcome.MessageID, Iteration: iteration})
		}
		if len(results) > 0 && d.Checkpoint == nil {
			st.Messages = append(st.Messages, model.Message{Role: model.RoleUser, Content: results})
		}
		if len(questions) == 1 {
			question, err := pendingQuestion(questions[0])
			if err != nil {
				return st.failed(fmt.Sprintf("invalid question: %v", err), d)
			}
			return st.suspendWith(question)
		}
	}
}

// suspendResult is the ONE suspended-result constructor (S6's shape; S11's
// ask suspension and S55's cap suspension ride the same path). Any step
// pausing builds its result HERE, never by hand: a second constructor is
// how a suspension starts bypassing the sibling results and the iteration
// accounting the shared loop guarantees, and
// TestOnlyTheSharedLoopProducesSuspended pins the single assignment site in
// this file. The caller has already recorded into st.Messages everything the
// pause owes (question: sibling tool results before it; permission: the
// same); this tags the result and returns — suspension is exiting (P2), no
// process, no further tool. pauseReason names a suspension WITHOUT a
// question ("cap"); the question shape passes "" and its question.
func (st *LoopState) suspendResult(pauseReason string, question *PendingQuestion) StepResult {
	res := st.result()
	res.Kind = ResultSuspended
	res.Question = question
	res.PauseReason = pauseReason
	return res
}

// suspendWith is the question-suspending door: S6's shape, unchanged for
// every pre-T7 caller.
func (st *LoopState) suspendWith(question PendingQuestion) StepResult {
	return st.suspendResult("", &question)
}

// suspendCapped is the S55 T7 cap suspension (E2): no question — a cap has
// no one to ask — the step parks with pause_reason='cap' and the raw hit
// numbers, and the only door that moves it is loop.raise (the daemon maps
// the verdict's hit into this before the result reaches the loop engine).
func (st *LoopState) suspendCapped(reason string, hit *CapHit) StepResult {
	res := st.suspendResult("cap", nil)
	res.Reason = reason
	res.CapHit = hit
	return res
}

// closeUnrunTool is the honest answer for a tool_use the ask suspension left
// unrun (S11 review round 1). A mid-turn ask stops the turn at the blocked
// block — in the loop as in a recovery re-run — and every tool_use after it
// never runs. Leaving those silent is valid only while their assistant turn
// remains the final one: format.go requires every tool_use answered unless it
// sits in the final assistant turn, and the answer to the permission question
// appends a new turn, turning each silent block into a lost tool result no
// provider accepts and nothing can resume. So they are closed AT SUSPENSION
// with a result that says exactly what happened — not run, not made, not
// approved, re-issuable after the answer — plus the tool.end that stops a
// watcher's spinner honestly. Nothing dispatches here: the closure is a block
// and an event, which is why G12's one dispatch expression is untouched, and
// the blocked block is deliberately NOT closed — the answer answers it.
func (st *LoopState) closeUnrunTool(d LoopDeps, tool, toolUseID, blockedID string, input json.RawMessage, iteration int) model.Block {
	st.emit(d, StepEvent{
		Kind: EventToolEnd, Tool: tool, Summary: argumentSummary(input), Iteration: iteration, ToolUseID: toolUseID,
		Result:  fmt.Sprintf("%s did not run: suspended on a pending permission question for %s", tool, blockedID),
		IsError: true,
	})
	return model.ToolResultBlock(toolUseID, fmt.Sprintf(
		"%s did not run: the step is suspended on a pending permission question for %s; "+
			"this call was not made and not approved — re-issue it after the answer if still wanted",
		tool, blockedID), true)
}

func offeredTool(specs []model.ToolSpec, name string) bool {
	for _, spec := range specs {
		if spec.Name == name {
			return true
		}
	}
	return false
}

// turnResult is one model turn, decomposed.
type turnResult struct {
	blocks   []model.Block
	toolUses []model.Block
	text     string
	// stopReason is how the provider ended the turn ("length"/"max_tokens" when the output cap was reached).
	stopReason string
}

// CurrentContextTokens is D2's CURRENT number: the transcript's live estimated
// size right now. It reuses the one estimator the compaction trigger already
// uses for its own threshold check (compact.go's EstimateTokens over
// st.Messages) rather than inventing a second notion of "how big is the
// context" — two estimators would be two answers to one question, and the
// compaction decision and the readout would drift the day one moved.
//
// It is the pair's twin, never a substitute: the CUMULATIVE total — every
// token this step ever spent — is the store's answer (T1's
// CumulativeTokensTotal, a SUM over the step_turn rows), computed there and
// not from a runtime-held counter because LoopState.Usage already IS that
// in-memory total, and duplicating it second way in memory is the exact thing
// TL11/D2 forbid. A compacted step proves they differ: the current number
// drops as the transcript shrinks, the cumulative never does.
func (st *LoopState) CurrentContextTokens() int {
	if st == nil {
		return 0
	}
	return EstimateTokens(st.Messages)
}

// stream makes one model call, forwarding deltas to the sink AS THEY HAPPEN
// (K19).
//
// Not after. A step that streams nothing until it finishes is a step that looks
// hung for forty seconds, and the whole reason the sink exists is that a person
// is watching.
func (st *LoopState) stream(ctx context.Context, req model.ModelRequest, d LoopDeps, iteration int) (turnResult, error) {
	// Q3's answer (T3), measured: no clock bracketed the model call here —
	// `d.Clock()` appears only at RunLoop's deadline check and emit()'s
	// event stamping, neither of which straddles this call. The bracket is
	// therefore NEW, one pair of reads, and taken only when a sink exists:
	// with Telemetry nil this function performs exactly the clock reads it
	// performed before the slice (I2).
	turnStart := d.Clock()
	deltas, results, errs := d.Client.Stream(ctx, req)

	var out turnResult
	var text, thinking strings.Builder

	for delta := range deltas {
		switch delta.Kind {
		case model.DeltaText:
			text.WriteString(delta.Text)
			st.emit(d, StepEvent{
				Kind: EventModelDelta, Text: delta.Text, Iteration: iteration,
			})
		case model.DeltaThinking:
			// Forwarded with its own kind, so a client can render reasoning
			// differently rather than inline. The text is NOT added to `text`:
			// a handoff found in a thinking block would be the model's
			// reasoning rather than its answer.
			thinking.WriteString(delta.Text)
			st.emit(d, StepEvent{
				Kind: EventModelDelta, Text: delta.Text, Iteration: iteration, Tool: "thinking",
			})
		case model.DeltaToolUse:
			if delta.Block != nil {
				out.toolUses = append(out.toolUses, *delta.Block)
			}
		case model.DeltaStop:
			// The stop reason arrives with the accounting below.
		}
	}

	var result model.StreamResult
	for r := range results {
		result = r
	}
	var streamErr error
	for e := range errs {
		if streamErr == nil {
			streamErr = e
		}
	}

	// Accounting is accumulated BEFORE the error is returned: a call that
	// failed part-way still spent what it spent (M15).
	st.Usage = addUsage(st.Usage, result.Usage)
	st.Cost = st.Cost.Add(result.Cost)
	st.AdditionalCost = st.AdditionalCost.Add(result.Cost)
	st.ThinkingDropped += result.ThinkingDropped

	// S33 T3: the per-turn capture, deliberately beside the addUsage above —
	// after the accounting, before the error return — so it INHERITS M15's
	// rule rather than re-implementing it: a call that failed part-way still
	// spent what it spent, and the step_turn row is where that claim becomes
	// auditable after the process is gone (st.Usage is memory; the rows are
	// the truth). One row per model call, category assistant_output — this
	// turn's own output; T4 tags the context side and T5 the compaction.
	// A capture failure never fails the step: what a step COSTS is observed,
	// not transactional with what the step DOES — the error is SPOKEN (an
	// event a watcher can see) and never returned.
	wallMs := d.Clock().Sub(turnStart).Milliseconds()
	// The turn, finished, on the stream (and so in the journal): its whole text and thinking, what it spent, how
	// long it took, and the step's running cost. Emitted after the accounting and before the error return, like
	// the telemetry row below, so a call that failed part-way still shows what it spent.
	turnUsage, turnCost, stepCost := result.Usage, result.Cost, st.Cost
	st.emit(d, StepEvent{
		Kind: EventTurnCompleted, Iteration: iteration, Text: text.String(), Thinking: thinking.String(),
		Usage: &turnUsage, TurnCost: &turnCost, StepCost: &stepCost, WallMs: wallMs, StopReason: result.StopReason,
		ToolCalls: len(out.toolUses), Route: st.Request.Route.ID,
	})
	if d.Telemetry != nil {
		if err := d.Telemetry.RecordTurn(ctx, st.Request.StepID, iteration, result.Usage, result.Cost,
			wallMs, st.Request.Route.ID, CategoryAssistantOutput); err != nil {
			st.emit(d, StepEvent{Kind: EventTelemetryError, Iteration: iteration,
				Summary: fmt.Sprintf("telemetry: turn %d was not recorded: %v", iteration, err), IsError: true})
		}
	}

	if streamErr != nil {
		return out, streamErr
	}

	out.text = text.String()
	out.stopReason = result.StopReason
	if out.text != "" {
		out.blocks = append(out.blocks, model.TextBlock(out.text))
	}
	out.blocks = append(out.blocks, out.toolUses...)

	// # Owed tool errors (Q6)
	//
	// S3 reports tool calls whose ARGUMENTS would not parse, rather than
	// inventing a block for them — because only this layer knows what a tool
	// result looks like in a transcript. So this is where that debt is paid: a
	// synthetic error result per owed call, which keeps every `tool_use`
	// answered and the transcript valid.
	for _, owed := range result.OwedToolErrors {
		id := owed
		out.toolUses = append(out.toolUses, model.Block{
			Kind: model.BlockToolUse,
			ID:   id,
			Name: "unparseable",
			// An empty object rather than the raw arguments: the raw text did
			// not parse, and putting it in a field typed as JSON would make the
			// transcript invalid.
			Input: json.RawMessage(`{}`),
		})
		out.blocks = append(out.blocks, out.toolUses[len(out.toolUses)-1])
	}

	return out, nil
}

// runTool executes one tool call and returns its result block.
func (st *LoopState) runTool(ctx context.Context, use model.Block, d LoopDeps, iteration int) (model.Block, error) {
	// The ARGUMENTS only. The event carries `Tool` separately, and a summary
	// that repeated the name rendered as "→ read read README.md" in the demo
	// walk — every renderer would otherwise have to strip a prefix.
	summary := argumentSummary(use.Input)
	st.emit(d, StepEvent{
		Kind: EventToolStart, Tool: use.Name, Summary: summary, Iteration: iteration,
		// The block's own id, as tool.end already carries: a watcher pairs a
		// running call with its end (and its transcript result) by it, rather
		// than guessing from order when two calls of one tool are in flight.
		ToolUseID: use.ID,
		// The whole command, for bash only: the summary cuts it at 60 runes,
		// and a person deciding whether a command was the right one needs
		// the command, not its first line. Capped — see streamCommandCap.
		Command: capRunes(commandArgument(use.Name, use.Input), streamCommandCap),
	})

	// An unparseable argument the model emitted, paid as a debt from S3's
	// stream (Q6). It is an `IsError` result and the loop continues: a model
	// emitting bad arguments is normal, and it can recover from being told.
	if use.Name == "unparseable" {
		out := model.ToolResultBlock(use.ID,
			"the arguments for that tool call were not valid JSON, so it was not run. "+
				"Send the call again with valid JSON arguments.", true)
		st.emit(d, StepEvent{
			Kind: EventToolEnd, Tool: use.Name, Summary: summary, Iteration: iteration, ToolUseID: use.ID,
			Result: "unparseable arguments", IsError: true,
		})
		return out, nil
	}
	// S10 — the policy gate (D11/PL9). Structurally before, not textually
	// before (G12 generalised): the `d.Tools.Call` below is the ONLY
	// dispatcher-call expression in this file, and it sits after this guard,
	// so it is reached if and only if the verdict allowed. A refusal returns
	// T4's sentence as an IsError tool_result — the model can act on WHY
	// (a paraphrase becomes a retry loop that fills policy_event), and every
	// tool_use stays answered so the transcript remains valid.
	if d.Policy != nil {
		// The command the dispatcher below WOULD run, parsed SERVER-SIDE from
		// this block's own input (S11). Not a claimed identity and nothing
		// model-text-shaped from elsewhere: it names exactly the call being
		// decided, which is why the gate may be asked about it (the same
		// no-substitution argument as Tool below). Only bash carries one; an
		// unparseable argument carries none and the tool question governs —
		// bash itself refuses the bad arguments (G20).
		command := commandArgument(use.Name, use.Input)
		dec := d.Policy.Decide(PolicyGateRequest{
			// Identity is the SERVER-SIDE request's, never use.Input's: a model
			// that can name its own agent can name one an allow covers. Tool is
			// use.Name because it is the SUBJECT of the question — the dispatcher
			// below executes exactly the name the gate was asked about.
			Agent: st.Request.Handler.Name,
			// Loop/LaneType/MCPServer: the compiled StepRequest carries no loop
			// slug, lane or MCP server yet (S18/S20) — see PolicyGateRequest.
			Tool: use.Name,
			// The command this very block carries (parsed above by the runtime,
			// never by the model's other text) — what the command question is
			// asked about (S11). Empty off the bash path.
			Command: command,
			// StepID from the server-side request (assignment lives with the
			// row's creator, PL1), ModelRequested because runTool runs model-
			// emitted blocks and nothing else — the definition the disagreement
			// record turns on (S10).
			StepID:         st.Request.StepID,
			ModelRequested: true,
		}, st.policyAt)
		// The verdict on the stream, for every call the gate judged — allowed
		// ones included. A chat that shows only refusals reads as "policy was
		// not consulted" for everything else, which is the two-state lie P31
		// forbids. Reason is the one formatter's sentence, verbatim (§4d).
		st.emit(d, StepEvent{
			Kind: EventPolicyDecided, Tool: use.Name, Summary: summary, Iteration: iteration,
			ToolUseID: use.ID, Verdict: policyVerdict(dec), Reason: dec.Sentence, ApprovedBy: policyApprover(dec),
		})
		approved := dec.Asked && st.consumeApproval(use)
		approver := "person"
		reason := "allowed: a person approved this one call"
		if !approved && dec.Asked && st.Request.AutoApprove.Covers(use.Name) {
			// A person turned on a standing pre-approval for this loop (full access, or this tool). It
			// answers an ASK only — a deny never reaches here — and it says so on the stream.
			approved = true
			approver = "auto"
			reason = "allowed: a person pre-approved this for the loop"
			if st.Request.AutoApprove.All {
				reason = "allowed: full access is on for this loop"
			}
			if by := st.Request.AutoApprove.By; by != "" {
				reason += " (" + by + ")"
			}
		}
		if approved {
			// Run THIS call. The verdict on the stream says why, in words, instead of leaving "asked" to
			// read as the last word.
			st.emit(d, StepEvent{
				Kind: EventPolicyDecided, Tool: use.Name, Summary: summary, Iteration: iteration,
				ToolUseID: use.ID, Verdict: "allowed", Reason: reason, ApprovedBy: approver,
			})
			ctx = stepctx.WithApprovedCall(ctx, use.Name, command)
		}
		if dec.Asked && !approved {
			// The gate asked instead of answering: neither a permission nor a
			// refusal — somebody may decide. The command named in what follows
			// travels through the explicit `command` argument below — the
			// runtime's own parse of THIS block, the exact string the
			// dispatcher would have run — never the gate's echo: what a human
			// reads before deciding must be writable by nothing but the block
			// itself (SEC-051's trust side). The decision's own Command field
			// is not re-stamped here; the ask body reads the parse, not the
			// echo (S11 review round 1 — the write was dead).
			if st.Request.HasOperator {
				// Suspend like the question tool (P2: suspension is exiting).
				// The caller returns the suspended result the moment it sees
				// the field; d.Tools.Call below is NOT reached (G12 still
				// holds — this is the one dispatch expression, and an ask can
				// never reach it, whatever a confused gate said about Allowed).
				st.pendingPermission = &PendingQuestion{
					ToolUseID: use.ID,       // the block's own id — the answer resumes it
					Kind:      "permission", // question.go's enum, S6's row pattern
					Body:      permissionAskBody(dec, st.Request.Handler.Name, use.Name, command),
					Options:   PermissionOptions(),
				}
				// The tool did not run and did not fail; the stream says so,
				// so a watcher's spinner stops honestly rather than on a
				// verdict. The verdict itself reaches the human through the
				// question's Body, verbatim — one rendering each.
				st.emit(d, StepEvent{
					Kind: EventToolEnd, Tool: use.Name, Summary: summary, Iteration: iteration, ToolUseID: use.ID,
					Result: "suspended: awaiting a permission answer",
				})
				return model.Block{}, nil
			}
			// Nobody can be vouched for answering — including a loop that
			// never made the declaration, which the adapter collapses to
			// false fail-closed. An ask answered by nobody is a deny, answered
			// NOW, with the verdict sentence verbatim (the model can act on
			// WHY) and no question: the adapter creates question rows only
			// for suspended results, so this branch leaves the question table
			// empty by construction, and a step with no operator to answer
			// never waits on an answerer that does not exist.
			out := model.ToolResultBlock(use.ID, dec.Sentence, true)
			st.emit(d, StepEvent{
				Kind: EventToolEnd, Tool: use.Name, Summary: summary, Iteration: iteration, ToolUseID: use.ID,
				Result: dec.Sentence, IsError: true,
			})
			return out, nil
		}
		if !dec.Allowed && !approved {
			out := model.ToolResultBlock(use.ID, dec.Sentence, true)
			st.emit(d, StepEvent{
				Kind: EventToolEnd, Tool: use.Name, Summary: summary, Iteration: iteration, ToolUseID: use.ID,
				Result: dec.Sentence, IsError: true,
			})
			return out, nil
		}
	}
	unsafe := false
	if classes, ok := d.Tools.(ToolClass); ok {
		unsafe = classes.Class(use.Name) == "unsafe"
	}
	if unsafe {
		if d.Receipts == nil {
			return model.Block{}, fmt.Errorf("refusing to run unsafe tool %q without a receipt writer", use.Name)
		}
		if err := d.Receipts.Begin(ctx, use.ID, use.Name, toolArgDigest(use.Name, use.Input)); err != nil {
			return model.Block{}, fmt.Errorf("refusing to run unsafe tool %q: its receipt could not be saved: %w", use.Name, err)
		}
	}

	// OL3, the one stamping site: the step's row id rides the context into
	// every tool so a tool that records provenance reads the RUNTIME's fact,
	// never an argument a model could type. Zero (an ad-hoc step before its
	// row exists) reads as absent, and the tools that need an identity then
	// fail closed — which is the point of stamping rather than asking.
	// The S33 tool wall (T4's capture; T6's command row reads this same
	// choke): bracketed only when a sink exists, the same nil-gated clock
	// discipline as stream()'s turn bracket (I2 — telemetry-off costs nothing
	// it did not cost before).
	toolStart := time.Time{}
	if d.Telemetry != nil {
		toolStart = d.Clock()
	}
	ctx = stepctx.WithStepID(ctx, st.Request.StepID)
	res := d.Tools.Call(ctx, use.Name, use.Input)
	if unsafe {
		_ = d.Receipts.Complete(ctx, use.ID, res.Content, res.IsError)
		debugCrashAfterTool()
	}

	resultSummary := res.Summary
	if resultSummary == "" {
		resultSummary = firstLine(res.Content)
	}
	// A command's output, bash only and capped: what a person reads to see
	// what the command actually did. Every other tool's content stays off the
	// stream — a `read` of a large file would put the file there twice.
	var output string
	var truncated bool
	if use.Name == "bash" {
		output, truncated = capBytes(res.Content, streamOutputCap)
	}
	// The result content of EVERY tool, capped, so a reopened loop can show what a call returned from the journal
	// alone. The whole content stays in the step's transcript; this is the replayable copy.
	detail, detailTruncated := capBytes(res.Content, streamDetailCap)
	st.emit(d, StepEvent{
		Kind: EventToolEnd, Tool: use.Name, Summary: summary, Iteration: iteration,
		Result: resultSummary, IsError: res.IsError,
		// The block's own id (brief §5 wants `toolUseId` on tool.end — a
		// watcher pairs the end with the start and with the transcript's
		// tool_result by it), and the write line counts when the tool
		// reported any (D8). The metadata rides from the Result, never
		// re-derived here.
		ToolUseID: use.ID, Path: res.Path,
		LinesAdded: res.LinesAdded, LinesRemoved: res.LinesRemoved,
		Output: output, OutputTruncated: truncated,
		Detail: detail, DetailTruncated: detailTruncated,
	})

	// S33 T6 (TL8/D5+D6): the activity rows, read from the fields the tool
	// ALREADY reported — zero re-derivation at this choke.
	if d.Telemetry != nil {
		// File half (D5): `res.Path != ""` IS the write-shape signal — the
		// one tool that sets it today (write) means an actual content
		// replacement; `read` leaves it empty, so reads never land here.
		// (The finding that `edit` also leaves it empty is recorded in the
		// task Notes — reusing existing fields means exactly capturing what
		// the tools report, per this task's zero-new-plumbing rule.)
		if res.Path != "" {
			added, removed := res.LinesAdded, res.LinesRemoved
			if err := d.Telemetry.RecordFileActivity(ctx, st.Request.StepID, res.Path, "write", &added, &removed); err != nil {
				st.emit(d, StepEvent{Kind: EventTelemetryError, Iteration: iteration, Tool: use.Name,
					Summary: fmt.Sprintf("telemetry: file activity for %s was not recorded: %v", res.Path, err), IsError: true})
			}
		}
		// Command half (D6): gated on the TOOL NAME — a legitimate `exit 0`
		// is data, the data D6 counts most ("how many commands ran", not
		// just failed), so "nothing interesting happened" must not skip a
		// row. The `ExitCode != nil` companion is not the nonzero test the
		// Do-NOT forbids — an exit 0 carries a NON-NIL pointer to zero; nil
		// means the command never started (spawn refused its token, no
		// shell), and a command that never ran is not a run (T4's refusal
		// principle again). The command string comes from the runtime's OWN
		// parse of the block (commandArgument — the same one definition the
		// permission ask uses), never from a tool echo.
		if use.Name == "bash" && res.ExitCode != nil {
			if err := d.Telemetry.RecordCommandActivity(ctx, st.Request.StepID, commandArgument(use.Name, use.Input),
				res.ExitCode, res.WallMs); err != nil {
				st.emit(d, StepEvent{Kind: EventTelemetryError, Iteration: iteration, Tool: use.Name,
					Summary: fmt.Sprintf("telemetry: command activity was not recorded: %v", err), IsError: true})
			}
		}
	}

	// S33 T4 (TL6/D4): the tool_result category recorded at the ONE point
	// where the code already knows it — this call EXECUTED. Gate refusals
	// returned above without running anything and correctly record nothing: a
	// policy decision is not tool activity, and a refused command that counted
	// as a run would inflate the very number TL9 promises. Usage and cost are
	// EMPTY by construction — a tool result is not a spend event; the store
	// keeps its cost columns NULL/'unknown' and contributes only the wall
	// (T1's rule); the file/command facts ride their own activity rows at T6.
	// Its own RecordTurn call, not an extension of stream()'s: the model-call
	// row and the transcript-append row are structurally different facts at
	// different points, and folding them together would make each read like
	// the other. Failure spoken, never fatal — the same rule as the turn.
	if d.Telemetry != nil {
		toolWall := d.Clock().Sub(toolStart).Milliseconds()
		if err := d.Telemetry.RecordTurn(ctx, st.Request.StepID, iteration, model.Usage{},
			model.Cost{State: model.CostUnknown}, toolWall, st.Request.Route.ID, CategoryToolResult); err != nil {
			st.emit(d, StepEvent{Kind: EventTelemetryError, Iteration: iteration, Tool: use.Name,
				Summary: fmt.Sprintf("telemetry: tool_result for %s was not recorded: %v", use.Name, err), IsError: true})
		}
	}

	return model.ToolResultBlock(use.ID, res.Content, res.IsError), nil
}

// finish extracts the handoff, and re-asks once — TOOL-FREE — if it cannot.
func (st *LoopState) finish(ctx context.Context, lastReq model.ModelRequest, d LoopDeps, iteration int) StepResult {
	extracted := ExtractHandoff(st.Messages)
	if extracted.OK {
		if ok, reason := ValidateHandoff(extracted.Handoff, d.HandoffSpec); !ok {
			extracted.OK = false
			extracted.Reason = reason
		}
	}

	if extracted.OK {
		handoff := extracted.Handoff
		outcome := OutcomeFromHandoff(handoff, d.StatusField)
		reason := handoffReason(handoff, outcome)
		// F-δ (S17 T9): the reject's reason must reach the
		// loop engine — the redo door refuses a reason-less retry (L32), and the
		// review lane's backlog consumes ok-reasons (O7). Absence stays empty.
		return st.complete(outcome, &handoff, reason, d)
	}

	if st.retried {
		// The second failure. `failed`, with the transcript and the partial
		// usage intact — the step spent money and the transcript is the record
		// of what it did with it.
		return st.failed(fmt.Sprintf("the model did not produce a valid handoff, twice. The second "+
			"attempt: %s", extracted.Reason), d)
	}

	st.retried = true
	st.Messages = RetryMessages(st.Messages, extracted.Reason)

	// # The retry request, and the one thing that makes it a retry
	//
	// **No tools key at all** (K4, D4). Not an empty slice — nil, so the field
	// is absent from the wire. A retry that can call tools can start new work,
	// and the assertion in the tests is on THIS OBJECT rather than on a count,
	// because a retry offered tools it happens not to use is still wrong.
	retryReq := model.ModelRequest{
		Route:           lastReq.Route,
		Messages:        st.Messages,
		Tools:           nil,
		MaxOutputTokens: lastReq.MaxOutputTokens,
		// Everything else about the call is UNCHANGED, including the reasoning
		// effort. Found by a test: the retry was built field by field and lost
		// it, so a step whose effort had been lowered to `none` reverted to the
		// route's on the retry — and the provider then refused the retry for a
		// reason that has nothing to do with the handoff.
		//
		// Copied from `lastReq` rather than re-derived, so the next field added
		// to a request is carried here by default instead of being forgotten.
		ReasoningEffort: lastReq.ReasoningEffort,
		Temperature:     lastReq.Temperature,
	}

	st.Manifest.IterationsUsed++
	turn, err := st.stream(ctx, retryReq, d, st.Manifest.IterationsUsed)
	if saveErr := st.checkpoint(ctx, d); saveErr != nil {
		return st.failed(fmt.Sprintf("the handoff retry accounting could not be checkpointed: %v", saveErr), d)
	}
	if err != nil {
		return st.failed(fmt.Sprintf("the handoff retry failed: %v", err), d)
	}
	if len(turn.blocks) > 0 {
		// Stamped like the main append (M26, T1): a handoff-retry turn is an
		// assistant turn produced by the SAME route, and S18 T5's replay rule
		// keys on provenance — an unstamped turn here would have its own
		// thinking withheld from the provider that produced it, which is
		// precisely the P21 break the rule exists to prevent.
		prov := model.NewProvenance(st.Request.Route)
		st.Messages = append(st.Messages, model.Message{
			Role: model.RoleAssistant, Content: turn.blocks, Provenance: &prov,
		})
		if saveErr := st.checkpoint(ctx, d); saveErr != nil {
			return st.failed(fmt.Sprintf("the handoff retry could not be checkpointed: %v", saveErr), d)
		}
	}

	// # A retry that calls a tool anyway
	//
	// It cannot: the request carried none. If the model emits one regardless,
	// the blocks are IGNORED rather than executed — executing them would
	// defeat the whole property, and the transcript records what it asked for.
	retryExtract := ExtractHandoff(st.Messages)
	if retryExtract.OK {
		if ok, reason := ValidateHandoff(retryExtract.Handoff, d.HandoffSpec); !ok {
			retryExtract.OK = false
			retryExtract.Reason = reason
		}
	}
	if retryExtract.OK {
		handoff := retryExtract.Handoff
		outcome := OutcomeFromHandoff(handoff, d.StatusField)
		reason := handoffReason(handoff, outcome) // F-δ (S17 T9): the reject's reason must reach the
		// loop engine — the redo door refuses a reason-less retry (L32), and the
		// review lane's backlog consumes ok-reasons (O7). Absence stays empty.
		return st.complete(outcome, &handoff, reason, d)
	}
	return st.failed(fmt.Sprintf("the model did not produce a valid handoff, twice. The second "+
		"attempt: %s", retryExtract.Reason), d)
}

// handoffReason is the sentence a finished step gives for its outcome: the
// handoff's `reason`, or — for a BLOCKED step whose schema has none — its
// `summary`, so the loop's banner never reads "no reason" over a report that
// gave one. A reject keeps an absent reason absent (the redo door needs to know).
func handoffReason(h Handoff, outcome Outcome) string {
	reason, _ := h["reason"].(string)
	if reason == "" && outcome == OutcomeBlocked {
		reason, _ = h["summary"].(string)
	}
	return reason
}

// --- result construction -----------------------------------------------------

func (st *LoopState) complete(outcome Outcome, h *Handoff, reason string, d LoopDeps) StepResult {
	res := st.result()
	res.Kind = ResultComplete
	res.Outcome = outcome
	res.Handoff = h
	res.Reason = reason
	st.emit(d, StepEvent{Kind: EventStepDone, Outcome: outcome, Reason: reason,
		Iteration: st.Manifest.IterationsUsed})
	res.Events = st.Events
	return res
}

func (st *LoopState) blocked(reason string, d LoopDeps) StepResult {
	return st.complete(OutcomeBlocked, nil, reason, d)
}

func (st *LoopState) failed(reason string, d LoopDeps) StepResult {
	return st.complete(OutcomeFailed, nil, reason, d)
}

func (st *LoopState) result() StepResult {
	return StepResult{
		Messages:        st.Messages,
		Manifest:        *st.Manifest,
		Usage:           st.Usage,
		Cost:            st.Cost,
		AdditionalCost:  st.AdditionalCost,
		ThinkingDropped: st.ThinkingDropped,
		PendingRoute:    pendingRouteID(st),
		PendingEffort:   st.PendingEffort,
		Events:          st.Events,
		Compactions:     append([]CompactionRecord(nil), st.Compactions...),
	}
}

// emit sends an event to the sink and records it.
//
// The panic boundary is in `runstep.go`, around the sink — not here, because
// the loop should not know that a sink can fail.
func (st *LoopState) emit(d LoopDeps, e StepEvent) {
	if e.At.IsZero() {
		e.At = d.Clock()
	}
	// A tool call's duration is the time between its start and its end event, measured here once so every end path
	// (ran, refused, failed, skipped) reports it the same way.
	if e.ToolUseID != "" {
		switch e.Kind {
		case EventToolStart:
			if st.toolStarts == nil {
				st.toolStarts = map[string]time.Time{}
			}
			st.toolStarts[e.ToolUseID] = e.At
		case EventToolEnd:
			if started, ok := st.toolStarts[e.ToolUseID]; ok && e.DurationMs == 0 {
				e.DurationMs = e.At.Sub(started).Milliseconds()
				delete(st.toolStarts, e.ToolUseID)
			}
		}
	}
	st.Events = append(st.Events, e)
	d.Sink.Emit(e)
}

// --- small helpers -----------------------------------------------------------

// addUsage sums two usages, keeping "absent is not zero" (M9).
//
// A nil field plus a nil field is nil, not zero: `nil` means the provider did
// not report it, and turning that into 0 would make an unreported count look
// like a measured absence of tokens.
func addUsage(a, b model.Usage) model.Usage {
	return model.Usage{
		InputTokens:      addPtr(a.InputTokens, b.InputTokens),
		OutputTokens:     addPtr(a.OutputTokens, b.OutputTokens),
		CacheReadTokens:  addPtr(a.CacheReadTokens, b.CacheReadTokens),
		CacheWriteTokens: addPtr(a.CacheWriteTokens, b.CacheWriteTokens),
		ReasoningTokens:  addPtr(a.ReasoningTokens, b.ReasoningTokens),
		ReportedCostUSD:  addFloatPtr(a.ReportedCostUSD, b.ReportedCostUSD),
	}
}

// addFloatPtr sums two optional floats with the same rule as addPtr: nothing plus nothing is nothing, not zero.
func addFloatPtr(a, b *float64) *float64 {
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		v := *b
		return &v
	case b == nil:
		v := *a
		return &v
	}
	v := *a + *b
	return &v
}

func addPtr(a, b *int) *int {
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		return model.IntPtr(*b)
	case b == nil:
		return model.IntPtr(*a)
	default:
		return model.IntPtr(*a + *b)
	}
}

// commandArgument is the command a bash block carries, as the runtime reads
// it (S11). Server-side by construction: it is parsed from the block's OWN
// input here, at the gate question's site, which is what makes "the command
// decided" and "the command run" the same string — the dispatcher executes
// exactly the call the gate was asked about (the no-substitution argument
// PolicyGateRequest carries). Only bash carries a command; anything else
// asks the tool question with an empty one. An unparseable input carries an
// empty command — the unparseable debt is paid where it always was, and the
// tool question governs whatever the gate then says.
func commandArgument(tool string, input json.RawMessage) string {
	if tool != "bash" || len(input) == 0 {
		return ""
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return ""
	}
	return args.Command
}

// permissionAskBody renders the question an asked decision suspends the step
// on (S11). The body names the three things a human needs to answer — the
// agent asking, the exact command (or bare call) being asked about, and the
// rule that asked (QAP-12: the question that does not name its rule is
// unauditable) — and closes with the verdict's own sentence, VERBATIM from
// the policy engine's one formatter: the same sentence a deny for the same
// call would have handed the model byte for byte.
//
// The command travels through strconv.Quote because it is model-authored
// text (SEC-051): a question that rendered it raw would let the command
// itself write the sentence a human reads before deciding. The renderer for
// the human is the daemon's (S6's existing question rendering); this function
// assembles the string and nothing else.
// The answers a permission question offers. The first and fourth decide this one call; the other three also
// write a rule through the human door (protocol/answer_rules.go) so the same question is not asked again:
// for this loop only, for this agent always, or a standing refusal. ResumeStep sees only approve or deny.
const (
	AnswerApprove     = "approve"
	AnswerApproveLoop = "approve for this loop"
	AnswerAlwaysAllow = "always allow"
	AnswerDeny        = "deny"
	AnswerAlwaysDeny  = "always deny"
)

// PermissionOptions is the closed answer set of a permission question, in the order a person reads it.
func PermissionOptions() []string {
	return []string{AnswerApprove, AnswerApproveLoop, AnswerAlwaysAllow, AnswerDeny, AnswerAlwaysDeny}
}

func permissionAskBody(dec PolicyGateDecision, agent, tool, command string) string {
	rule := dec.RuleSubject
	if rule == "" {
		rule = dec.RuleID
	}
	if rule == "" {
		rule = "no rule named"
	}
	subject := "the call to " + tool
	if command != "" {
		subject = tool + " command " + strconv.Quote(command)
	}
	return fmt.Sprintf("permission required: agent %s asks to run %s · deciding rule %s · %s",
		agent, subject, rule, dec.Sentence)
}

// argumentSummary describes a tool call's arguments for the event stream.
//
// Summarised rather than included: a `read` of a 2 MiB file would otherwise put
// the path in the stream and a `write` would put the content there twice.
func argumentSummary(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(input, &fields); err != nil {
		return "(unparseable arguments)"
	}
	// `path` and `command` are the two that identify a call. Anything else is
	// summarised as its key count, because a stream of full arguments is a
	// stream nobody can read.
	if p, ok := fields["path"].(string); ok {
		return p
	}
	if c, ok := fields["command"].(string); ok {
		if len(c) > 60 {
			return c[:57] + "…"
		}
		return c
	}
	return fmt.Sprintf("(%d argument(s))", len(fields))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		return s[:77] + "…"
	}
	return s
}

// What a bash call puts on the event stream, at most. The stream is a shared,
// bounded ring (S2) and every event costs a durable seq, so these are small:
// enough to read a command and the start of its output, never a log dump.
// The transcript keeps the full result; the stream says when it cut.
const (
	streamCommandCap = 2000 // runes
	streamOutputCap  = 4096 // bytes
	// streamDetailCap bounds the result content one `step.tool.end` carries for replay.
	streamDetailCap = 32 << 10 // bytes
)

// capRunes keeps at most n runes of s.
func capRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// capBytes keeps at most n bytes of s, cut on a rune boundary, and says
// whether it cut.
func capBytes(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// pendingRouteID flattens the deferred switch for the result: the LOOP owns
// route changes (its state is the single route a dispatch reads), and the
// runtime reports what it owes without touching that state itself.
func pendingRouteID(st *LoopState) string {
	if st.PendingRoute == nil {
		return ""
	}
	return st.PendingRoute.ID
}

// todoGuard is the completion guard (S75 TD10). It reports true when it appended
// a reminder and the loop should call the model again. It only ever acts on a
// step that WOULD complete `ok`: a failed or blocked stop needs no reminder.
func (st *LoopState) todoGuard(ctx context.Context, d LoopDeps) bool {
	if d.Todo == nil || !d.Todo.GuardApplies() {
		return false
	}
	extracted := ExtractHandoff(st.Messages)
	if !extracted.OK {
		return false
	}
	if ok, _ := ValidateHandoff(extracted.Handoff, d.HandoffSpec); !ok {
		return false
	}
	if OutcomeFromHandoff(extracted.Handoff, d.StatusField) != OutcomeOK {
		return false
	}
	open, ok := d.Todo.ActiveOpen(ctx)
	if !ok || len(open) == 0 {
		return false
	}
	if st.todoBounces >= maxTodoBounces {
		// The third attempt completes as the model asked, and the stream says it
		// closed with the list open: never a silent ok.
		st.emit(d, StepEvent{Kind: EventTodoNudged, Reason: "closed-with-open", Summary: strings.Join(open, ","), Iteration: st.Manifest.IterationsUsed})
		return false
	}
	st.todoBounces++
	st.Messages = AppendUserTurn(st.Messages, TodoCompletionText(open), nil)
	st.emit(d, StepEvent{Kind: EventTodoNudged, Reason: "completion", Summary: strings.Join(open, ","), Iteration: st.Manifest.IterationsUsed})
	return true
}

// policyApprover names who allowed a call the gate answered by itself: `policy` for a rule that allowed it, empty
// for a refusal or an ask (an ask is answered by a person or a standing pre-approval, reported by the event that
// follows it).
func policyApprover(dec PolicyGateDecision) string {
	if policyVerdict(dec) == "allowed" && !dec.Asked {
		return "policy"
	}
	return ""
}

// maxCutoffs bounds how many times in a row a cut-off reply is answered with "continue" before the step takes its
// ordinary ending.
const maxCutoffs = 3

// outputCutOff reports whether a provider's stop reason says the output cap ended the turn.
func outputCutOff(reason string) bool {
	switch reason {
	case "length", "max_tokens", "max_output_tokens", "incomplete":
		return true
	}
	return false
}

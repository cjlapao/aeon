package runtime

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// `RunStep`: the entry point, and the thing that **persists nothing** (K6, D15).
//
// # Why that is the design and not an omission
//
// Three later slices each need to hand this function a transcript it did not
// write:
//
//	S6  suspends by EXITING, and resumes by handing the loop rows from a
//	    previous process
//	S7  recovers a crashed step by reading `status='running'` rows and
//	    replaying them
//	S9  calls it per step from `execute`, owning the loop's bookkeeping
//
// A runtime that wrote its own rows could do none of those. So the caller owns
// the transcript, the `step` protocol handler is the only writer (K22), and a
// test asserts this package imports no store at all.
//
// A store spy at ZERO calls is the assertion (T9), rather than a grep alone: a
// grep catches the import and a spy catches a write through an injected
// interface that happened to be a store.

// RunOpts are the injected dependencies — all of them (I3).
//
// Nothing here is optional except the sink, and nothing is read from the
// environment. That is what makes "no unit test contacts a provider" (D18)
// true rather than aspirational: a test supplies a scripted client and the
// whole step runs offline.
type RunOpts struct {
	Client Streamer
	Tools  ToolCaller

	// Sink receives events as they happen. Nil means discard — a step nobody
	// is watching still runs.
	Sink EventSink

	// Clock is the deadline's source, injected so a test can exceed one
	// without waiting.
	Clock func() time.Time

	HandoffSpec HandoffSpec
	StatusField string

	// Context supplies a workspace reader for ResumeStep's hash comparison.
	// RunStep does not use it; context assembly happened before RunStep.
	Context    ContextDeps
	Receipts   ReceiptWriter
	Checkpoint CheckpointWriter

	// Steers is the turn-boundary drain source (S31 T4, D3/D5): a RESOLVED
	// queue the daemon wires to the durable steer rows, route names already
	// looked up against the caller's own registry. Nil means the step is
	// unsteerable — the pre-T4 code path, byte for byte (I2). It rides
	// RunStep, ResumeStep and RecoverStep alike: recovery drains pending
	// steers on the resumed step's first iteration like any other boundary.
	Steers SteerSource

	// MailboxDrain and MailboxAskDoor are the agent↔agent mailbox (S70
	// MB4), injected by the same rule as Steers: nil means the step has no
	// mailbox and runs the pre-S70 path byte for byte (I2). They ride
	// RunStep, ResumeStep and RecoverStep alike — a recovered step drains
	// pending peer messages on its first iteration like any other boundary,
	// and a resumed step (cold, from rows) can ask and can receive.
	MailboxDrain MailboxDrain
	MailboxAsk   MailboxAskDoor

	// Todo is S75's nudge and completion guard, injected by the same rule: nil
	// is the pre-S75 path byte for byte (I2).
	Todo TodoPolicy

	// Policy is the S10 policy gate, injected exactly as Receipts and
	// Checkpoint arrive (I3, K22): the runtime asks, the caller's adapter
	// answers. Nil means no enforcement — pre-S10 callers keep passing;
	// production always wires one and a test boots the daemon table to prove
	// it (daemon.New + TestProductionDaemonWiresThePolicyGate).
	Policy PolicyGate

	// Telemetry is the S33 capture seam (TL12) — the same injection
	// discipline as Steers and Policy, nil meaning the feature is OFF (I2).
	// It rides RunStep, ResumeStep and RecoverStep alike: a recovered step's
	// re-run model calls are spend events like any other, and the archive
	// test (T9) depends on crash-path turns having been captured too. Every
	// real construction site wires the store-backed adapter explicitly;
	// a field OMITTED from a literal is not the same decision as the nil
	// that means "off" (plan.md Q2).
	Telemetry TelemetrySink

	// PolicySet constructs the gate for a step's FROZEN policy set — the
	// replay-honesty constructor (S10, D12). ResumeStep and RecoverStep ask
	// for it whenever the manifest froze one (gateForStep), passing the
	// frozen pair — the set AND the digest recorded beside it at assembly —
	// so the constructor can recompute and compare (DEF-039) instead of
	// trusting the set's own word. A recovered or resumed step is therefore
	// decided against the set that was in force when it was decided, not
	// against today's store. Nil means the caller offers no frozen-set gate
	// — pre-S10 callers (whose manifests freeze nothing) keep the live
	// Policy; production wires it alongside Policy (daemon.New).
	PolicySet func(frozen FrozenManifest) PolicyGate

	// Budget is the S55 T7 in-step check (E2), injected by the same rule as
	// Policy: the runtime asks, the daemon's adapter answers with plain
	// data (BudgetVerdict — no store or budget-package type crosses in).
	// It rides RunStep, ResumeStep and RecoverStep alike: the re-run of a
	// raised cap step is a step like any other, and a second hit on it
	// suspends it again (the loop's `capped` arm re-blocks). Nil means
	// unenforced — the pre-T7 path, byte for byte (I2).
	Budget func(ctx context.Context) (BudgetVerdict, error)
}

// RunStep runs one step and returns exactly once.
//
// Exactly once for all four outcomes (T9's point 4): there is one return path
// through the loop, and the panic boundary below converts a panic into a
// `failed` result rather than a second return.
func RunStep(ctx context.Context, req StepRequest, opts RunOpts) StepResult {
	// THE FIRST WORD IS `Check` (S18 T4, M29): a step the route has DECLARED
	// it cannot serve is refused before the sink exists, before the first
	// event, before any checkpoint — nothing is emitted, nothing is sent,
	// nothing is charged. This return is outside the loop, so "exactly once"
	// stands: a refused step returns once, failed, with the sentence.
	if err := Check(req.Route, req); err != nil {
		return StepResult{
			Kind:     ResultComplete,
			Outcome:  OutcomeFailed,
			Reason:   err.Error(),
			Messages: req.Messages,
			Manifest: req.Manifest,
		}
	}

	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}

	// The sink is wrapped BEFORE anything is emitted, so even `step.started`
	// is protected. A sink that panics on the first event would otherwise take
	// the step down before it began.
	sink := newGuardedSink(opts.Sink)

	manifest := req.Manifest
	state := &LoopState{
		Request:  req,
		Messages: append([]model.Message(nil), req.Messages...),
		Manifest: &manifest,
	}
	// The policy instant is snapshotted ONCE here, from the injected clock —
	// not per tool call. Every decision in this step is made at this instant,
	// so a policy expiring mid-step cannot decide two identical calls
	// differently (S10; the gate itself reads no clock, D5).
	state.policyAt = clock()

	deps := LoopDeps{
		Client:       opts.Client,
		Tools:        opts.Tools,
		Sink:         sink,
		Receipts:     opts.Receipts,
		Checkpoint:   opts.Checkpoint,
		Steers:       opts.Steers,
		MailboxDrain: opts.MailboxDrain,
		MailboxAsk:   opts.MailboxAsk,
		Todo:         opts.Todo,
		Policy:       opts.Policy,
		Telemetry:    opts.Telemetry,
		Clock:        clock,
		HandoffSpec:  opts.HandoffSpec,
		StatusField:  opts.StatusField,
		Budget:       opts.Budget,
	}

	// DECLARED FILES, not sections. The manifest counts what was read at
	// assembly, and the ad-hoc handler declares none — so "0 context
	// section(s)" read as a step with no prompt, which is what the demo walk
	// found.
	toolNames := make([]string, 0, len(req.Tools))
	for _, tool := range req.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	summary := fmt.Sprintf("%d declared file(s), %d tool(s) offered (%s)",
		len(manifest.Files), len(req.Tools), strings.Join(toolNames, ", "))
	// S66: the catalog marker rides the SAME event as the opening summary
	// — the injection is part of the step's opening, and one carrier means
	// live and replay cannot disagree about when it happened.
	var stepSkills *StepSkills
	if req.SkillsInjected != nil {
		sec := req.SkillsInjected
		stepSkills = &StepSkills{
			Count: sec.Count, Names: append([]string(nil), sec.Names...),
			Truncated: sec.Truncated, Marker: sec.MarkerSentence(),
		}
	}
	for _, note := range req.Notes {
		// Appended to the opening line, which is where somebody is already
		// looking when a step starts. A decision nobody can see is a decision
		// that reads as the provider's behaviour.
		summary += " · " + note
	}

	state.emit(deps, StepEvent{
		Kind:     EventStepStarted,
		Tool:     req.Handler.Name,
		Summary:  summary,
		Withheld: req.Withheld,
		Skills:   stepSkills,
	})
	if err := state.checkpoint(ctx, deps); err != nil {
		return state.failed(fmt.Sprintf("the opening state could not be checkpointed: %v", err), deps)
	}

	result := runGuarded(ctx, state, deps)
	if err := state.checkpoint(ctx, deps); err != nil {
		return state.failed(fmt.Sprintf("the final state could not be checkpointed: %v", err), deps)
	}
	result.SinkFailures = sink.failures()
	return result
}

// runGuarded runs the loop and converts a panic into a `failed` result.
//
// # What this catches, and what it deliberately does not
//
// A panic in the LOOP — a nil map, an index, a type assertion in a code path a
// test did not reach. It becomes `failed` with the stack in the reason, because
// a daemon serving three projects should not die because one step hit a defect.
//
// It does NOT catch a panic in CONSTRUCTION: `CompileRequest` and
// `BuildToolset` run before this, and a nil dependency there panics where the
// stack names the caller. That split is T9's "a programmer error panics and a
// step failure returns" — and it is why `BuildToolset` panics on a missing
// workspace root rather than returning an error.
func runGuarded(ctx context.Context, state *LoopState, deps LoopDeps) (result StepResult) {
	defer func() {
		if r := recover(); r != nil {
			result = state.failed(fmt.Sprintf(
				"the step hit an internal error and was stopped: %v\n\n"+
					"This is a defect in Aeon rather than a problem with the request. The transcript "+
					"and the accounting below are what the step produced before it stopped.\n\n%s",
				r, string(debug.Stack())), deps)
		}
	}()
	return RunLoop(ctx, state, deps)
}

// guardedSink wraps a sink so a panicking one cannot kill the step.
//
// # Why a panicking sink is a case worth handling
//
// The sink is the CALLER's code: at S5 it forwards to S2's subscriptions, and
// at S9 it will update an app. A client that disconnected mid-step, a closed
// channel, a nil map in a handler — each is a panic in somebody else's code
// path, and none of them is a reason to lose a step that is otherwise working.
//
// Failures are COUNTED and reported on the result (T9's point 3) rather than
// swallowed: a sink that is throwing means a client is not seeing the step,
// and the step completing anyway is correct but worth knowing about.
type guardedSink struct {
	inner EventSink
	mu    sync.Mutex
	count int
}

func newGuardedSink(inner EventSink) *guardedSink {
	if inner == nil {
		inner = DiscardSink{}
	}
	return &guardedSink{inner: inner}
}

func (g *guardedSink) Emit(e StepEvent) {
	defer func() {
		if r := recover(); r != nil {
			g.mu.Lock()
			g.count++
			g.mu.Unlock()
			// Deliberately NOT logged from here. This package has no logger and
			// should not acquire one — the count reaches the caller on the
			// result, and the caller is the one with a log.
		}
	}()
	g.inner.Emit(e)
}

func (g *guardedSink) failures() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.count
}

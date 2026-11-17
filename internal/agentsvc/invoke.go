package agentsvc

import (
	"context"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// invoke.go is the thing the slice is for: an agent asking another agent to do
// something (AS10). A planner wanting a report from Reddit does not want
// reddit.search — it wants "find what people are saying about X and summarise
// it", which is a request for judgement about how to use tools. That judgement
// belongs to the callee; the caller buys the answer, never the surface (MC2).
//
// What Invoke owns and nothing more: the legality check against the callee's
// own signed card, the chain edge written BEFORE the task runs (AS6), and the
// handoff to T3's TaskStore. It owns no waiting — no goroutine, no step-row
// write, no question (locked by test); suspension lives in task.go, and a
// second waiting path here would be the slice's central failure with a
// plausible excuse.

// Invocation is everything a caller may say: caller, callee, skill, the
// request, and sync-or-async. Nothing else — no tool list, no transport
// detail, no "priority" that is a queue in disguise (AS5 has no queue).
type Invocation struct {
	CallerLoopID string
	CallerLane   string
	CallerAgent  string
	CallerStepID int64

	CalleeAgent string
	SkillID     string
	Text        string
	InputMode   string
	Params      map[string]any

	Mode   TaskMode
	Frozen []policy.Policy

	// Depth is the chain position this call occupies; 0 means the root call's
	// 1. T5 owns the bound and the cycle; writing a sane depth is T4's job.
	Depth int

	// Client answers the callee's judgement for a SYNC call. An async call
	// needs none: its answer arrives later, from the callee's own side.
	Client Client

	// EvaluatedAt is the instant the frozen policy set is judged against —
	// expiry is a fact about a moment (D12), and an invocation without one
	// cannot be judged against a set whose expiry is unknowable. Required,
	// not defaulted: a hidden clock is how an expired rule quietly revives.
	EvaluatedAt time.Time
}

// SkillRef is the canonical skill reference: the policy row, the request and
// the refusal all spell an ask the same way — `callee/skill`. One spelling
// is the difference between a rule that matches and a rule that looks right.
func SkillRef(callee, skill string) string { return callee + "/" + skill }

// Invoker is the seam: store for rows, CardStore for who may be asked of what.
type Invoker struct {
	s     *store.Store
	cards *CardStore
	tasks *TaskStore
}

// Tasks exposes the task layer the invoker owns, so the ENTRY POINT can
// wire the AS12 first-party boundary on the same TaskStore that settles
// completions (Resume labels through it — a label wired onto a different
// instance would be a label nobody applies).
func (iv *Invoker) Tasks() *TaskStore { return iv.tasks }

func NewInvoker(s *store.Store, cards *CardStore) *Invoker {
	return &Invoker{s: s, cards: cards, tasks: NewTaskStore(s)}
}

// SkillKnown is the answer loop/pin.go's preflight asks (wired via
// loop.SetSkillKnown at the cmd/aeon seam — the loop package must not import
// this one, AS11's direction): known means the agent's OWN VERIFIED card
// offers the skill. A tampered card contributes no knowledge — AS2 refuses,
// it does not warn; and "not configured" (nil CardStore semantics) is known-
// false, which the pin turns into an honest refusal naming the target.
func (iv *Invoker) SkillKnown(callee, skill string) bool {
	if iv.cards == nil {
		return false
	}
	card, err := iv.cards.Card(callee)
	if err != nil || card == nil {
		return false
	}
	_, ok := card.FindSkill(skill)
	return ok
}

// Invoke turns a request into a Task — reusing T3 with no second waiting
// path — after it clears the pre-money door: an agent is askable of a skill
// only when its own signed card offers that skill, and the request itself is
// valid against the card (T2's validator, the same one a direct Ask passes
// through; one door, not two that can drift).
func (iv *Invoker) Invoke(ctx context.Context, in Invocation) (Task, *Reject) {
	if iv.cards == nil {
		return Task{}, &Reject{Field: "cards", Detail: "invocation refused: no card store is configured — a skill is askable only of an agent whose own signed card offers it"}
	}
	if in.CallerAgent == "" {
		return Task{}, &Reject{Field: "callerAgent", Detail: `invocation refused: field "callerAgent" is empty — an agent-to-agent request must name the agent asking`}
	}
	if in.Mode == TaskSync && in.Client == nil {
		return Task{}, &Reject{Field: "client", Detail: fmt.Sprintf("invocation refused: a sync call to %s/%s waits on the callee's answer and none can arrive — pass a Client, or ask async", in.CalleeAgent, in.SkillID)}
	}

	// The pre-money door (AS2): no card, no ask. The refusal names the agent,
	// says what is missing, and states the remedy — a card at <agent>.json.
	card, err := iv.cards.Card(in.CalleeAgent)
	if err != nil {
		return Task{}, &Reject{Field: "callee", Detail: fmt.Sprintf("invocation refused: the card for %q cannot be verified: %v — AS2 refuses a card it cannot check rather than trusting the old one was fine", in.CalleeAgent, err)}
	}
	if card == nil {
		return Task{}, &Reject{Field: "callee", Detail: fmt.Sprintf("invocation refused: agent %q has no signed card, so no skill of it can be asked. The remedy is the card the callee publishes: %s.json, signed with its key — a skill is askable only of an agent whose own signed card offers it", in.CalleeAgent, in.CalleeAgent)}
	}
	req := SkillRequest{Agent: in.CalleeAgent, Skill: in.SkillID, Text: in.Text,
		InputMode: in.InputMode, Params: in.Params}
	if reject := req.validate(card); reject != nil {
		return Task{}, reject // enumerates what exists (A13's shape) — no suggestions
	}

	// AS8: per-skill security IS S10's SkillAccess policy, judged here and
	// nowhere else (no ACL in this package — a grep test holds that line).
	// S10's evaluator decides, S10's authorship rule authored the rows, and
	// S10's one formatter speaks the refusal, so the CLI, the app and a
	// transcript all say the same thing about the same refusal. Both halves
	// of the ask are judged: the CALLER'S right to ask this skill and the
	// CALLEE'S right to run it — the callee's permission is not the caller's
	// to spend, and a self-declared-open card (securitySchemes is a hint,
	// never an authority — A2A's natural reading is the opposite) opens
	// nothing that policy does not. PL5 is the resting state: no allowing
	// policy means no invocation.
	if in.EvaluatedAt.IsZero() {
		return Task{}, &Reject{Field: "evaluatedAt", Detail: "invocation refused: no instant to judge the frozen policy set at — expiry is a fact about a moment, and a hidden clock is how an expired rule quietly revives"}
	}
	ref := SkillRef(in.CalleeAgent, in.SkillID)
	for _, who := range []string{in.CallerAgent, in.CalleeAgent} {
		verdict := policy.Evaluate(in.Frozen, policy.Request{
			Type: policy.SkillAccess, Agent: who, Skill: ref,
		}, in.EvaluatedAt)
		if !verdict.Allowed {
			// S10's sentence, verbatim — one formatter (T7's whole point).
			return Task{}, &Reject{Field: "skillAccess", Detail: fmt.Sprintf("invocation refused: %s", verdict.Sentence())}
		}
	}

	// The caller's identity is the graph's spine: the edge joins this step to
	// its agent, and an ask filed under another agent's step would put an edge
	// in the graph that the cycle walk attributes to the wrong agent — every
	// refusal then diagnoses someone else's loop.
	row, err := iv.s.ReadStep(in.CallerStepID)
	if err != nil {
		return Task{}, &Reject{Field: "step", Detail: fmt.Sprintf("invocation refused: the caller step cannot be read (%v) — an ask whose caller is not on the record cannot be traced", err)}
	}
	if row.Agent != in.CallerAgent {
		return Task{}, &Reject{Field: "callerAgent", Detail: fmt.Sprintf("invocation refused: step %d is run by %q, not %q — an agent cannot delegate under another agent's step", in.CallerStepID, row.Agent, in.CallerAgent)}
	}

	// AS6/AS7, T5's two refusals BEFORE the edge and long before any Task:
	// an illegal ask leaves no trace to audit and no Task nobody completes.
	if reject := iv.chainCheck(in.CallerAgent, in.CalleeAgent); reject != nil {
		return Task{}, reject
	}

	// AS6, in this order on purpose: the edge is written AFTER the call is
	// known legal (an illegal invocation leaves no trace to audit) and BEFORE
	// any task write (a crash between leaves a recorded attempt, not an
	// untraceable cost). Depth belongs to the chain's owner (T5) to bound;
	// recording the caller's position is T4's job.
	depth := in.Depth
	if depth == 0 {
		depth = 1
	}
	if err := iv.s.CreateAgentChainEdge(store.NewChainEdge{
		CallerLoopID: in.CallerLoopID, CallerStepID: in.CallerStepID,
		CalleeAgent: in.CalleeAgent, SkillID: in.SkillID, Depth: depth,
	}); err != nil {
		return Task{}, &Reject{Field: "chain", Detail: fmt.Sprintf("invocation refused: the chain edge could not be recorded — without it the invocation would be an untraceable cost: %v", err)}
	}

	ct := CreateTask{
		CallerLoopID: in.CallerLoopID, CallerLane: in.CallerLane, CallerStepID: in.CallerStepID,
		CalleeAgent: in.CalleeAgent, SkillID: in.SkillID,
		Text: in.Text, Mode: in.Mode, Params: in.Params, Frozen: in.Frozen,
	}
	if in.Mode == TaskSync {
		task, reject := iv.tasks.Create(ctx, ct) // running — sync tasks do not suspend (AS4)
		if reject != nil {
			return Task{}, reject
		}
		text, reject := Ask(ctx, in.Client, card, req)
		if reject != nil {
			// The attempt happened and failed; the row says so (Fail), because
			// a running task with no process behind it is the exact half-state
			// this slice exists to refuse.
			if r2 := iv.tasks.Fail(ctx, task.ID, reject.Detail); r2 != nil {
				return Task{}, r2
			}
			return Task{}, reject
		}
		// The caller gets a judgement — the result text — and nothing of the
		// callee's tools (MC2: the returned Task names agents and skills,
		// never capabilities).
		return iv.tasks.CompleteSync(ctx, task.ID, text)
	}
	// Async: the step suspends inside T3's transaction and this call returns
	// while the step has already exited. Nothing here waits, polls, or sleeps.
	return iv.tasks.Create(ctx, ct)
}

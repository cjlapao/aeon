package remote

import (
	"context"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// egress.go — RA4: a remote skill invocation IS an egress, and the policy
// decides it BEFORE the request is assembled.
//
// A skill request carries this product's own material — a file path and
// whatever the skill needs to read; its transcripts, its plans, its source.
// Locally that is bookkeeping. Remotely it is SENDING it to somebody else.
// So the door is S10's: `route_access`, the same type S18 declared for model
// routes, with its SUBJECT extended to agent egress under the `remote:`
// namespace (RA4's ruling: extend the subject, never add a type — a second
// access type is a second authorship story an operator must remember to
// lock, and memory is not a security boundary).
//
// **Why "before the request is built" and not "before it is sent":** both
// placements produce the same refusal message. Only one is safe. A request
// ASSEMBLED and then discarded HAS ALREADY READ THE FILES it was going to
// carry — and in a long-running daemon that assembly may be logged, cached,
// or held in a buffer that outlives the refusal. The data has left the file
// and entered the process; the socket was never the boundary. So the check
// sits where the decision is, the assembler is a named interface the tests
// spy on, and a denial has an observable shape: the spy never fired, the
// filesystem was never touched, the dial counter never moved. The refusal's
// last line states the guarantee in words the record can quote:
//
//	"the request was not assembled. no file was read."
//
// The evaluation is PER INVOCATION (scope 5), not per peer-configuration:
// the policy set is read at call time through g.Rows and judged at call-time
// now, so a peer allowed yesterday is denied today exactly the way the
// policy engine says — an expired NotAfter, a new deny row, a changed frozen
// set. A cached verdict would be a permission that outlived its grant.

// EgressNamespace is the prefix that makes a peer a ROUTE in RouteAccess's
// vocabulary: `remote:docs-peer`, or `remote:*` for every peer at once. The
// canonical spelling lives HERE (policy never imports remote); the
// evaluator's tests on the policy side and the gate's tests on this side
// pin the two ends of the string to each other.
const EgressNamespace = "remote:"

// EgressSubject is the canonical route name for one peer's egress.
func EgressSubject(peerID string) string { return EgressNamespace + peerID }

// InvokeSpec is everything an egress INTENDS to do — what policy judges and
// what the assembler later builds. Deliberately an intention, not a request:
// on a refusal nothing downstream of this struct ever existed.
type InvokeSpec struct {
	Agent string // who is asking (policy's identity field)
	Loop  string // which loop, if any (Subject() prefers it, PL8)
	Peer  string // the peer's id — becomes the route via EgressSubject
	Skill string // the qualified skill being invoked
	Args  []byte // whatever the caller handed; never inspected before admission
}

// Assembler is the read side of an egress: the component that touches this
// product's files to BUILD the request. It is an interface for the same
// reason the mcp spy exists — the slice's sharpest test (D5) is about an
// ORDER, and an order can only be asserted against a boundary you can
// watch. A denial's proof is that this method was never called.
type Assembler interface {
	Assemble(spec InvokeSpec) ([]byte, error)
}

// EgressGate is the ordered door: policy first, assembler second, dial last.
type EgressGate struct {
	// Rows loads the policy set AT CALL TIME (scope 5). The caller decides
	// whether that is the live table or a step's frozen manifest — the same
	// choice the step gate makes (D12); this file only guarantees the read
	// happens per invocation, never once per configuration.
	Rows func(spec InvokeSpec) ([]policy.Policy, error)
	// Now is the evaluation clock; the seam keeps expiry testable to order.
	Now func() time.Time
}

func (g *EgressGate) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// EgressDecision is Admit's whole answer: the allow/deny fact, S10's
// sentence verbatim (the one formatter, never re-worded here), and the
// guarantee line when it is a refusal.
type EgressDecision struct {
	Allowed  bool
	Sentence string
}

// NoFileReadLine is the guarantee, spelled once. Tests pin it byte-exact
// because the record quotes it and operators learn to look for it.
const NoFileReadLine = "the request was not assembled. no file was read."

// Admit evaluates RouteAccess for one intended egress. It reads no files,
// touches no socket, and builds nothing — those are the steps this function
// PRECEDES, which is the entire reason it exists.
//
// A policy set that cannot be read FAILS CLOSED: "policy unreadable" is not
// an invitation to send the transcripts anyway, and the refusal says so with
// the same guarantee line, because a half-loaded permission table is exactly
// the moment the boundary must be strongest.
func (g *EgressGate) Admit(spec InvokeSpec) EgressDecision {
	req := policy.Request{
		Type:  policy.RouteAccess, // RA4: the type is S18's; only the ROUTE VALUE says "egress"
		Agent: spec.Agent,
		Loop:  spec.Loop,
		Route: EgressSubject(spec.Peer),
	}
	ps, err := g.Rows(spec)
	if err != nil {
		return EgressDecision{Allowed: false, Sentence: "egress refused: the policy set could not be read — " + err.Error() +
			" — failing closed; " + NoFileReadLine}
	}
	v := policy.Evaluate(ps, req, g.now())
	if v.Allowed {
		return EgressDecision{Allowed: true, Sentence: v.Sentence()}
	}
	return EgressDecision{Allowed: false, Sentence: v.Sentence() + "\n" + NoFileReadLine}
}

// EgressResult is the whole flow's truth: a refusal (with the sentence, and
// NOTHING happened), or an assembly plus a call (with the wire's Outcome).
type EgressResult struct {
	Refused   bool
	Sentence  string
	Outcome   Outcome // zero-value unless the egress was admitted
	Assembled []byte  // nil unless the egress was admitted
}

// Invoke runs the flow the design's diagram names:
//
//	invoke → RouteAccess evaluated ── denied ──▶ refused, nothing sent
//	                         └──── allowed ──▶ build request → dial
//
// One path, and the order is the code: Admit returns before Assembler is
// reachable, and the dial is reachable only from the assembled branch.
func (g *EgressGate) Invoke(ctx context.Context, d *Dialer, peer Peer, a Assembler, spec InvokeSpec) EgressResult {
	dec := g.Admit(spec)
	if !dec.Allowed {
		return EgressResult{Refused: true, Sentence: dec.Sentence}
	}
	body, err := a.Assemble(spec)
	if err != nil {
		return EgressResult{Refused: true, Sentence: "egress refused: the request could not be assembled — " + err.Error()}
	}
	out := d.Call(ctx, peer, body)
	return EgressResult{Sentence: dec.Sentence, Outcome: out, Assembled: body}
}

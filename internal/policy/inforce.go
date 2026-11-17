package policy

// ════════════════════════════════════════════════════════════════════════════
// "Is this policy in force?" — three answers, none collapsing into another,
// and the ONE renderer of them (PL12 — P31's eighth instance).
//
// # The three answers, and the middle one
//
// | | |
// | --- | --- |
// | **enforced and acknowledged**   | the target was told, and said so |
// | **enforced but unacknowledged** | the target was never told. **It is still enforced** |
// | **could not determine**         | no evaluator for this type yet (D10) — the binary
//                                     cannot answer, which is a different fact from
//                                     "a rule refused you" |
//
// **The middle one is the valuable one** — the seed's argument, kept here where
// the next editor reads it. A fleet of agents that never acknowledged its
// policies is working but degraded: the rules apply, and the agents burn turns
// on calls that get refused because nothing told them not to make them. **That
// is invisible unless something asks**, and this file is the asking.
//
// **And the middle state must not read as a problem with enforcement.** It is
// enforced; the gap is in the telling. Render it as though enforcement were in
// doubt and somebody goes looking for a policy bug that does not exist — which
// is why "unacknowledged still reads as enforced" is a wording criterion with a
// test behind it, not a cosmetic preference (D13's other half: PL10 says an
// unaccepted policy is enforced, and this says so too).
//
// # Why a closed type, and not two booleans
//
// `acknowledged bool` × `determinable bool` admits a fourth combination —
// acknowledged *and* undeterminable — that means nothing, and somebody will
// render it. `InForceState` therefore has exactly three values, they are
// reachable only through `AllInForceStates` (the copy discipline of
// `AllPolicyTypes`: widening the set is a code change, never a string somebody
// cast), and the enumeration test is what makes "three" a fact rather than a
// comment.
//
// # Why "could not determine" is specifically "no evaluator"
//
// Not a catch-all. The cause is the registry: either the type has no usable
// evaluator at all, or the registered one declares itself T5's placeholder
// through `StubMarker` (see spec_stubs.go — the marker, never a failure
// channel: this file declares none anywhere, S4's DB19, because a domain
// answer is not an infrastructure failure and a renderer that can fail is a
// renderer a surface drops on the floor). A bucket labelled "other" is how a
// real signal gets lost: if a second reason to be unable to answer ever
// appears, it arrives as its own value and its own test, and
// `TestCouldNotDetermineMeansNoEvaluatorSpecifically` is what refuses the
// catch-all in the meantime.
//
// # The one renderer
//
// The same discipline verdict.go (T4) enforces for the refusal sentence.
// `Render` below is the ONLY place these three sentences are assembled, and
// T12's `aeon policy in-force`, T13's socket method and T14's PolicyPage print
// the result **verbatim**: no re-casing, no re-segmenting, no status column
// that stands in for the sentence (an additive column is fine; a replaced
// sentence is a second renderer). The three forms are the contract, pinned
// byte for byte:
//
//	in force · acknowledged by <target> since <RFC3339>
//	in force · unacknowledged — still enforced (PL10)
//	could not determine · no evaluator for <type> yet (D10)
//
// The failure mode this file exists to make unrepeatable is cosmetic and
// total: an unacknowledged answer rendering like an acknowledged one deletes
// the signal while everything still appears to work. So "no state collapses
// into another in rendering" is a test, not a convention, and
// `TestRenderIsTheOnlyInForceRendererInTheModule` walks the module rather than
// trusting that nobody hand-assembles these lines downstream.
//
// # What this file does NOT do
//
//   - It is not the gate. Acceptance never conditions enforcement (PL10/D13):
//     this reads the record; the engine does not know the record exists. That
//     line is held by TestEvaluateDoesNotReadPolicyAcceptance and counted by
//     TestTheEnforcementPathReadsNoAcceptanceRows — and InForceFor is a
//     question a human asks, never a step on the verdict path.
//   - No SQL and no import of the store package (I2): the acceptance read goes
//     through the Repo seam, in store.go, exactly as List's does.
//   - No clock reads (D5): the instant arrives as `at`, and expiry is decided
//     by evaluate.go's own `expired` predicate — the same rule that decides
//     whether a deny is alive at the moment of a verdict, so the answer here
//     cannot disagree with the engine about when a rule stopped existing.
//   - No cache (N8): two calls are two reads, and `List` is the one read.
// ════════════════════════════════════════════════════════════════════════════

import (
	"cmp"
	"context"
	"slices"
	"time"
)

// InForceState is the answer to "is this policy in force (for this target)".
// Exactly three values (PL12); the count is the claim and
// TestTheThreeInForceStatesAreExactlyThese is the lock.
type InForceState string

const (
	// EnforcedAcknowledged: the rule governs this target AND this target has a
	// record of having been told about it (T7's row). The instant that record
	// carries travels in InForceAnswer.AcknowledgedAt.
	EnforcedAcknowledged InForceState = "enforced_acknowledged"

	// EnforcedUnacknowledged: the rule governs this target and nothing ever
	// told it. **Enforced regardless** (PL10/D13) — the state is about the
	// telling, never about the enforcement, and it renders that way: the
	// sentence opens "in force" precisely so nobody reads a hole in enforcement
	// where there is only a hole in communication.
	EnforcedUnacknowledged InForceState = "enforced_unacknowledged"

	// CouldNotDetermine: nothing in this binary can decide this type yet — the
	// registered evaluator is T5's placeholder, or the type has no usable
	// evaluator at all. It is not a denial and not a permission, and it shares
	// no rendering with either: "nobody can answer this yet" and "a rule
	// refused you" send an operator to two completely different places.
	CouldNotDetermine InForceState = "could_not_determine"
)

// inForceStates is the closed list, reachable only through AllInForceStates.
var inForceStates = []InForceState{EnforcedAcknowledged, EnforcedUnacknowledged, CouldNotDetermine}

// AllInForceStates returns the three states, in the design's order: told, not
// told, cannot tell. The result is a copy — sorting or appending to it tells
// you nothing about what the engine will answer tomorrow.
func AllInForceStates() []InForceState { return append([]InForceState(nil), inForceStates...) }

// The heads of the two sentences that speak for enforcement, and of the one
// that does not. Constants, so the renderer is the only place they are spelled
// and a surface cannot "tidy" one into a synonym.
const (
	inForceHead           = "in force"
	couldNotDetermineHead = "could not determine"
	unreadableHead        = "state unreadable"
)

// InForceAnswer is one policy's answer for one target.
//
// It is a value with no failure channel (DB19): the question always has one of
// the three answers, and a caller never has to guess whether the world broke
// or the answer is simply "nobody can tell yet".
type InForceAnswer struct {
	// Policy is the stored rule the answer is about — a read-only view of what
	// List handed back, exactly as the boundary re-decoded it. The renderer
	// reads its type; it never reads its target (see Target, below).
	Policy Policy

	// Target is the target the question was asked about — the party whose
	// acknowledgement is being reported, and therefore the party the sentence
	// names after "acknowledged by". It is deliberately NOT Policy.Target: a
	// rule addressed to `all-agents`, asked about `agent:boss` and recorded as
	// accepted by `agent:boss`, was acknowledged by `agent:boss`. Printing the
	// rule's scope there would credit a statement to a party that never made
	// one — the same class of wrong that makes a verdict name the policy that
	// acted rather than the request that asked. ParseTarget validated it
	// before it was ever allowed to be this field.
	Target Target

	// State is the answer. One of the three; anything else is a hand-built
	// answer and Render says so rather than guessing.
	State InForceState

	// AcknowledgedAt is the instant of the FIRST record for
	// (Policy.ID, Target) — "since when", the first time and only the first
	// time (a repeated accept moves nothing, which is what keeps the answer
	// honest across a retry or a crash). Non-nil exactly when the state was
	// set from a row that carries an instant; a nil here is reachable only on
	// an answer assembled by hand, and the renderer states it rather than
	// printing an empty slot.
	AcknowledgedAt *time.Time
}

// Render renders the answer as the sentence every surface prints — T12's CLI,
// T13's socket, T14's app. Pure string building over the fields it was handed
// (D5: no I/O, no clock): the instant printed is the instant the record
// carries, never now, so a line printed today keeps matching the row it came
// from when it is quoted tomorrow.
//
// The three forms are pinned character for character, and the order of the
// switch is not an argument — the states are the dispatch, and each gets its
// own words:
//
//	EnforcedAcknowledged   → in force · acknowledged by <target> since <RFC3339>
//	EnforcedUnacknowledged → in force · unacknowledged — still enforced (PL10)
//	CouldNotDetermine      → could not determine · no evaluator for <type> yet (D10)
//
// The first two share the head, and that sharing IS the claim: both are in
// force, and the only difference a reader may see is whether the target was
// told. The third shares nothing with either — not the head, not the rhythm —
// because a reader who confuses "nobody can answer" with "the rule is not
// binding" stops trusting the answer, and a reader who confuses it with "a
// rule refused you" goes and deletes a policy that has nothing to do with it.
//
// Anything outside the closed set renders as itself (the last line below) and
// never as one of the three. InForceState is a string type, so `InForceState("")`
// and `InForceState("sort-of")` compile: an answer that claims a state nobody
// defined is answered honestly — "state unreadable" — rather than defaulted
// into the nearest real answer, which is exactly the collapse this file exists
// to prevent. Same move verdict.go makes for a verdict that names no policy:
// it fails closed and says the true thing.
func (a InForceAnswer) Render() string {
	switch a.State {
	case EnforcedAcknowledged:
		return inForceHead + sep + "acknowledged by " + targetLabel(a.Target) + " since " + acknowledgedLabel(a.AcknowledgedAt)
	case EnforcedUnacknowledged:
		// Reads as ENFORCED. That is the whole point of the wording: the
		// target was never told, the rule still binds, and the sentence must
		// not leave room to read the second half as a doubt about the first.
		return inForceHead + sep + "unacknowledged — still enforced (PL10)"
	case CouldNotDetermine:
		return couldNotDetermineHead + sep + "no evaluator for " + typeLabel(a.Policy.Type) + " yet (D10)"
	}
	// Fail closed, and never by guessing which real answer this was meant to
	// be: a fourth string, visibly not one of the three.
	return unreadableHead + sep + quotePreview(string(a.State)) + sep + "the three answers are the only ones a surface may print"
}

// acknowledgedLabel renders <since>, and it is never empty — the same rule
// verdict.go's expiryLabel obeys: a record of "since when" that cannot show
// its instant has to SAY so, because a silent gap is indistinguishable from a
// gap nobody checked. Reachable only for an answer assembled by hand: the
// state is set from a row that carries an instant, or it is not this state.
func acknowledgedLabel(at *time.Time) string {
	if at == nil {
		return "an instant nobody recorded"
	}
	return at.Format(time.RFC3339)
}

// InForceFor answers PL12 for one target at one instant: for every stored rule
// that governs this target and is still alive at `at`, was it enforced, and
// was this target told about it.
//
// The order of operations, and each step is load-bearing:
//
//  1. the target, rebuilt through ParseTarget — the boundary trusts no
//     caller's struct, read or write (SEC-015): a hand-cast kind, an id
//     repeating its kind, a named kind naming nothing are all refused by name
//     before a single read is issued;
//  2. the instant. `at` is the question's, and a zero `at` is a missing field
//     rather than the year 1: against a zero clock every rule with an expiry
//     reads as expired, so the answer would be a confident lie about a fleet
//     of unenforced policies. D5: no boundary reads a clock it was not handed;
//  3. the stored set, through List — the one read, uncached, every row
//     re-decoded through DecodePolicy (a row that escaped the write boundary
//     refuses the whole answer loudly; a silently dropped row can silently
//     drop a Deny);
//  4. who was told and since when — the per-target acceptance read in
//     store.go, which reaches the record through the Repo seam and nowhere
//     near enforcement (PL10);
//  5. classify each rule. Another target's rule is not this target's answer
//     and is skipped by targetMatches — the engine's OWN predicate, fed a
//     Request built from the queried target, so "does this rule govern this
//     target" has one implementation and cannot disagree with what enforcement
//     would do. Expired is excluded by Evaluate's own `expired` predicate at
//     the caller's `at`: an expired rule is gone, not denied (D6/D7), so
//     reporting it as "in force · unacknowledged" would be a phantom; and the
//     exclusion is the caller's question, not the store's — List still hands
//     back every row, because an audit that hides history is an audit that
//     lies.
//
// Then the one classification rule, three branches, no default that could
// stand in for another:
//
//   - no usable evaluator for the type, or one that declares itself T5's
//     placeholder → CouldNotDetermine (see noEvaluatorYet — the marker, never
//     a match result, never a failure channel);
//   - a record exists for (Policy.ID, Target) → EnforcedAcknowledged, carrying
//     that record's first instant;
//   - otherwise → EnforcedUnacknowledged, and it reads as enforced, because it
//     is.
//
// The result is ordered by policy id (the real store's `ORDER BY id` order,
// re-asserted here so the answer does not depend on the order rows happened to
// arrive in — a list whose order moves is a list a surface cannot diff).
//
// A store failure arrives as a *Reject naming the seam that failed (Add's and
// RecordAcceptance's single-return-shape discipline), saying the answer was
// NOT computed. There is no partial answer: a half-read acceptance table would
// report "unacknowledged" for rules whose records the read simply lost, and
// that is the one wrong answer this file could give — it would send an operator
// to chase agents that had in fact been told.
func (s *Store) InForceFor(ctx context.Context, target Target, at time.Time) ([]InForceAnswer, *Reject) {
	// 1. The target, through the one parser that can make one.
	t, rej := ParseTarget(target.Kind, target.ID)
	if rej != nil {
		return nil, rej
	}

	// 2. The instant is a parameter or the answer is about no moment at all.
	if at.IsZero() {
		return nil, rejectf("refused: the in-force answer for %s has no instant; \"in force as when\" is the "+
			"question this answers, every rule with an expiry would read as expired against a zero clock, and a "+
			"clock read is not this boundary's to take (D5)", t.String())
	}

	// 3. The stored set — the one read (no cache, N8), rows already re-decoded.
	policies, rej := s.List(ctx, at)
	if rej != nil {
		return nil, rej
	}

	// 4. The records for THIS target, keyed by policy (store.go; PL10 — this
	//    read exists on the human-asking path and never on the verdict path).
	accepted, rej := s.acceptedSince(ctx, t)
	if rej != nil {
		return nil, rej
	}

	// 5. Filter, then classify.
	probe := requestForTarget(t)
	out := make([]InForceAnswer, 0, len(policies))
	for _, p := range policies {
		if !targetMatches(p.Target, probe) {
			continue // another target's rule is not an answer about this one
		}
		if expired(p, at) {
			continue // gone, not denied (D6): Evaluate's own rule, same instant
		}
		a := InForceAnswer{Policy: p, Target: t}
		if noEvaluatorYet(p.Type) {
			a.State = CouldNotDetermine
		} else if when, ok := accepted[p.ID]; ok {
			instant := when // the answer's own copy; no two answers share a pointer
			a.State, a.AcknowledgedAt = EnforcedAcknowledged, &instant
		} else {
			a.State = EnforcedUnacknowledged
		}
		out = append(out, a)
	}
	slices.SortFunc(out, func(x, y InForceAnswer) int { return cmp.Compare(x.Policy.ID, y.Policy.ID) })
	return out, nil
}

// noEvaluatorYet is the ONLY thing that produces CouldNotDetermine, and it asks
// two questions of the registry — both about whether anything can decide this
// type, neither of them a failure channel (DB19: this file declares none):
//
//   - is there a usable evaluator at all? An unregistered type, or one
//     registered with a nil half, has none. This is EvaluatorFor's own answer,
//     the same one Evaluate acts on when it refuses rather than permits, so
//     the two can never disagree about whether a type is decidable;
//   - does the registered evaluator declare itself T5's placeholder, through
//     the StubMarker method (spec_stubs.go)?
//
// The marker is the cause, and that specificity is the criterion: a real
// evaluator that simply does not match this request is NOT a stub — the engine
// refused that call, it did not fail to understand the type — so an answer
// about it says "in force", never "could not determine". Inferring stub-ness
// from a match result would put the two facts T3 kept apart back together, and
// a real refusal would start rendering as an operator's to-do list.
func noEvaluatorYet(typ PolicyType) bool {
	ev, ok := EvaluatorFor(typ)
	if !ok {
		return true // nothing is registered to answer: the plainest form of "not yet"
	}
	return evaluatorIsStub(ev)
}

// requestForTarget turns the queried target into the one Request shape
// targetMatches reads, so "does this rule govern this target" is answered by
// the SAME predicate the engine uses when it decides (evaluate.go) rather than
// by a second equality written here that could drift from it. AllAgents
// governs every agent, present and future, so the empty probe is the honest
// one; every named kind fills exactly the identity field its kind governs.
func requestForTarget(t Target) Request {
	r := Request{}
	switch t.Kind {
	case AllAgents:
		// Names everyone: matches every probe, so there is nothing to fill.
	case AgentKind:
		r.Agent = t.ID
	case LoopKind:
		r.Loop = t.ID
	case LaneTypeKind:
		r.LaneType = t.ID
	case MCPServerKind:
		r.MCPServer = t.ID
	}
	return r
}

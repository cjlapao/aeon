package runtime

import "time"

// The policy gate: the one question the loop asks before anything dispatches
// (S10, D11/PL9).
//
// # Why the types are runtime-local (K22)
//
// The runtime cannot import the store (K22, asserted by
// TestRunOptsHasNoStoreShapedDependency and TestThisPackageImportsNothingThatCouldPersist),
// and it does not import `internal/policy` either: the runtime knows a gate
// EXISTS, not what a Policy is. The gate therefore arrives as an injected
// interface on `RunOpts`/`LoopDeps` — exactly how `Receipts` and `Checkpoint`
// arrive — and the production adapter lives on the CALLER side
// (`internal/daemon/policy_gate.go`), where the store may be reached.
//
// # Structurally before, not textually before (G12 generalised)
//
// The gate is consulted inside `runTool`, immediately after the unparseable
// early-return and BEFORE the single dispatcher call expression
// (`d.Tools.Call`, the only one in loop.go): the dispatch statement is reached
// if and only if the decision allowed it. A refusal returns an IsError
// tool_result carrying the verdict sentence, so the transcript stays valid
// (every `tool_use` is answered) and the model learns WHY it was refused.
// TestGateIsStructurallyBeforeDispatch proves it with a spy at zero
// plus a textual pin that the dispatcher call sits after the `Policy != nil`
// guard and exists exactly once.
//
// # nil means no enforcement
//
// A nil gate is skipped silently — pre-S10 tests and any caller that does not
// wire one keep passing. Production always wires one (`daemon.New` assigns it
// whenever a store exists; TestProductionDaemonWiresThePolicyGate pins it).

// PolicyGateRequest is the question the loop asks the gate: may THIS agent, in
// THIS context, call THIS tool.
//
// The identity fields are assigned by trusted, SERVER-SIDE code from the
// compiled step request (PL1 rests on this, carried HIGH from the T6 gate):
// `runTool` fills Agent from `st.Request.Handler.Name` and never from
// `use.Input` — a model that could name its own agent would name one an
// allow-rule covers. Tool is `use.Name`, the block's own name, because it is
// the SUBJECT of the question, not a claimed identity: the dispatcher call
// executes exactly the name the gate was asked about, so there is no
// substitution gap between "decided" and "run".
//
// Loop, LaneType and MCPServer are empty until the compiled StepRequest (or a
// later slice) carries that identity: the loop slug lives with the protocol
// handler's bookkeeping, not the compiled request, and lanes are S18/S20.
// A policy targeting those kinds therefore cannot match a runtime decision
// yet — it denies nothing and permits nothing until its identity arrives
// (fail-closed, never fail-open), and Notes on the seed task record the gap.
type PolicyGateRequest struct {
	Agent     string
	Loop      string
	LaneType  string
	MCPServer string
	Tool      string

	// Command is the model-emitted command string of a bash tool_use, empty
	// otherwise. It is parsed SERVER-SIDE from the block's own input by the
	// runtime (never a claimed identity, never tool arguments beyond the
	// command field itself), and it is what the command question is asked
	// about. A plain string across K22: the runtime never names policy types.
	//
	// It is NOT a road from the prompt to enforcement in PL9's forbidden
	// sense: Tool is the subject of the tool question for the same reason —
	// the dispatcher below executes exactly the call the gate was asked
	// about, so there is no substitution gap between the command decided
	// and the command run. A text-bearing field that could decide something
	// OTHER than its own call is what the closed-set guard in
	// disagreement_test.go forbids; this one names the call itself (S11).
	Command string

	// StepID attributes the attempt to a step row — filled by the runtime from
	// the SERVER-SIDE request (StepRequest.StepID), never from tool arguments
	// (PL1). It is what RecordDisagreement charges the attempt to; zero means
	// the caller is not running a step row and gets a refusal, never a
	// disagreement (an unattributable attempt is not an attempt anybody can
	// be charged with).
	StepID int64

	// ModelRequested is true exactly when the gate question is about a
	// tool-use block THE MODEL ITSELF emitted — the definition separating a
	// disagreement (the agent was told, in the manifest's `## Policy` section
	// or not, and tried anyway) from a plain refusal (an internal call the
	// engine declined on someone's behalf). `runTool` — the only production
	// caller — sets it true; any future internal Decide (a call no model
	// emitted) leaves it false, and only the true branch writes the
	// disagreement row (PL9's telemetry: a rising disagreement rate means the
	// instruction stopped being read).
	//
	// It is assigned by trusted runtime code only, like Agent above; a bool
	// the model could set would be a bool that could forgive its own attempt.
	ModelRequested bool
}

// PolicyGateDecision is the gate's answer.
//
// Allowed is what the loop branches on for the permit/refuse decision.
// Sentence is the refusal (or permission) rendered by the policy engine's
// ONE formatter — `Verdict.Sentence()` from `internal/policy` — byte for
// byte: the gate implementation guarantees it, and the loop hands it to the
// model unchanged. A paraphrase here becomes a retry loop the model cannot
// steer out of, and every retry fills `policy_event` (T4's argument, enforced
// by TestRefusalReachingTheModelCarriesTheSentence).
//
// The Asked half (S11) is additive: a decision that is neither a permission
// nor a refusal — a question. The loop still must not dispatch on Asked
// (Allowed false covers it); the step's own handling of a question arrives
// with the ask suspension, which reads the plain-string rule fields below.
type PolicyGateDecision struct {
	Allowed  bool
	Sentence string

	// Asked is true exactly when the deciding verdict's action is ask: the
	// step must not run the tool and has not been refused — someone may
	// answer. RuleID and RuleSubject name the deciding rule (QAP-12: the
	// question that does not name its rule is unauditable), and Command
	// echoes what was asked about. Plain strings: K22 again.
	Asked       bool
	RuleID      string
	RuleSubject string
	Command     string
}

// PolicyGate answers the dispatch question. Implementations are the caller's:
// the runtime only ever asks. `at` is the instant the step began, snapshotted
// ONCE from the injected clock at step start — every tool call in a step is
// decided against the same instant, so two identical calls cannot straddle a
// policy expiry and produce a transcript that makes no sense (the step's
// transcript would show one allowed and one refused with nothing to explain
// it). The gate must not read a clock of its own; it decides from `at`.
type PolicyGate interface {
	Decide(req PolicyGateRequest, at time.Time) PolicyGateDecision
}

// FrozenManifest is the replay-honesty pair read straight off a step's own
// manifest: the policy set it froze, and the digest recorded beside it at
// assembly. It travels together because a caller that receives one and has to
// go find the other is a caller that will forget to — which is precisely how
// DEF-039's write-only digest survived a whole slice.
type FrozenManifest struct {
	Entries []PolicyEntry
	Hash    string
	// Frozen is the manifest's own claim that a set was consulted and frozen
	// at assembly (DEF-042). It is what makes Entries == nil mean "froze
	// nothing" rather than "predates S10".
	Frozen bool
}

// gateForStep is the replay-honesty rule, stated once for both resume paths
// (S10, D12): a step whose manifest froze a set — or froze NOTHING under the
// marker's claim that a set was consulted (PoliciesFrozen, DEF-042) — is
// decided against THAT record, reconstructed, never re-queried. The live
// gate answers only for manifests that carry neither a set nor the marker:
// genuinely pre-S10, or assembled without a policy source. "Froze nothing"
// is a claim about the world and keeps the frozen branch; "predates S10" is
// a claim about the manifest and is the only thing that reaches the live
// store. (Before DEF-042 this function's header claimed the live gate "only"
// served pre-S10 steps — true only because zero-policy manifests carried no
// way to say otherwise, which is the bug the marker closes.)
//
// The alternative — replay consulting the live store — is the silent wrong
// answer the seed names: a policy revoked after the crash would let a replay
// permit what the step was refused, and a policy added after it would refuse
// what it was allowed. Either way the transcript stops being a record of the
// step that ran, and nobody can tell from the outside.
//
// A manifest that froze entries while the caller supplied no constructor
// (PolicySet nil) falls back to the live Policy: hand-built manifests are a
// test author's own answer (they built the set, they own the gate), and
// production cannot drift into that shape — daemon.New wires both halves and
// TestProductionDaemonWiresThePolicyGate pins the pair, so a table with a
// live gate and no frozen-set constructor fails there, not in a replay.
func gateForStep(opts RunOpts, manifest ContextManifest) PolicyGate {
	if opts.PolicySet == nil {
		return opts.Policy
	}
	// The frozen branch fires on the marker, not on the length: a step that
	// froze an EMPTY set is a step governed by nothing, and "nothing" is the
	// answer it must keep after a crash. The length test stays for
	// hand-built manifests — a test author who fills Policies without the
	// marker owns the set they built (T10's rule, unchanged).
	if manifest.PoliciesFrozen || len(manifest.Policies) > 0 {
		return opts.PolicySet(FrozenManifest{
			Entries: manifest.Policies, Hash: manifest.PoliciesHash, Frozen: manifest.PoliciesFrozen,
		})
	}
	return opts.Policy // genuinely pre-S10, or assembled with no policy source
}

// policyVerdict names a decision for the stream. Asked wins over Allowed: an
// ask never dispatches (G12), whatever a confused gate set Allowed to.
func policyVerdict(dec PolicyGateDecision) string {
	switch {
	case dec.Asked:
		return "asked"
	case dec.Allowed:
		return "allowed"
	default:
		return "denied"
	}
}

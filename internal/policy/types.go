// Package policy is Aeon's policy engine, and the vocabulary it speaks.
//
// # The set is closed, and that is a security property (PL3)
//
// Seven policy types, five target kinds, four sources, three actions. Every
// value is a name in this file, and the lists are reachable only through
// AllPolicyTypes, AllTargetKinds, AllSources and AllActions — so widening the
// set is a code change (a schema, a validator, an evaluator: T5 registers
// them) and never a string somebody typed into a flag. An open set would mean
// a row in `policy` whose meaning nothing in the binary knows, which is a
// permission nobody granted wearing the costume of a policy.
//
// The same vocabulary exists a second time, in the CHECK constraints on the
// `policy` table — widened in migration 0019, which admitted the third action
// (S11's D1) — and the two have to agree value for value, with
// TestTheSchemaCHECKAndTypesGoAgree keeping them agreeing against the live
// rebuild. S4 wrote 0005's CHECKs before anyone had decided what a policy is,
// and the drift that produced — five types, none of them real, and a target
// vocabulary that named folders rather than subjects — is the mistake this
// file's twin in SQL exists to correct and this test exists to make
// unrepeatable.
//
// # Two fields carry the whole security argument
//
// Source is why a third party can be trusted with a policy at all. PL1 says an
// Allow may only be authored by an operator, so Source is not metadata: it is
// the field the write boundary checks (T6), which is why it is a closed enum
// rather than a string. A Deny from anyone is safe — the worst it can do is
// take something away — so all four sources are legal for Deny and only one is
// legal for Allow. Ask is deny-like the same way: it takes something away (the
// autonomy to act without asking), so it too is writable from all four sources
// and can never widen anyone's permissions.
//
// NotAfter is a *time.Time and nil means NO EXPIRY (PL7), never a far-future
// sentinel. A sentinel is a policy that silently lapses on a day nobody chose,
// and the lapse presents as a bug in whatever it was guarding rather than as
// the withdrawal of a rule. The zero Policy therefore has no expiry, which is
// the conservative direction for a Deny and the dangerous one for an Allow —
// which is the other reason the authorship check runs before anything reaches
// the table.
//
// # What a refusal is
//
// A bad policy is a domain refusal, not an infrastructure failure (S4's DB19),
// so every parser here returns a *Reject and not an error. *Reject non-nil is
// the whole signal, and Reason always names the value it refused:
// `unknown policy type "chat_log"` is actionable, `invalid input` is not.
//
// # Purity (D5)
//
// Nothing in this package reaches the store, the clock, the filesystem or the
// network, so every decision is a function of the values handed to it. That is
// what lets the evaluator be tested at the nanosecond boundary (T3) and what
// keeps the runtime able to decide without owning a database handle (T9). The
// import list is asserted, not assumed, by
// TestNoFileInThePolicyPackageImportsDatabaseSQL.
package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// PolicyType is the kind of thing a policy governs.
//
// Exactly seven (PL3). The wire values are the strings the `policy` table's
// CHECK constraint stores, character for character — the lock test in
// types_test.go compares this list against migration 0016, so a rename here
// fails the build rather than shipping a vocabulary only one side knows.
type PolicyType string

const (
	// ToolAccess governs whether a named tool may be dispatched for a target.
	// It is the only type S10 evaluates for real (T5); the other six exist so
	// the closed set is closed, and each arrives with its own evaluator in the
	// slice that needs it.
	ToolAccess PolicyType = "tool_access"

	// CommandAccess governs shell commands. S11 brings the matcher and its
	// rule corpus; here it is a name, a CHECK value and a refusing evaluator.
	CommandAccess PolicyType = "command_access"

	// RouteAccess governs which model route an agent may use.
	RouteAccess PolicyType = "route_access"

	// EffectClass governs a tool by the kind of effect it has — reading,
	// writing, spending — rather than by its name.
	EffectClass PolicyType = "effect_class"

	// CostCap bounds what a target may spend.
	CostCap PolicyType = "cost_cap"

	// SkillAccess governs which skills an agent may load.
	SkillAccess PolicyType = "skill_access"

	// TriggerAccess governs which triggers may start a loop for a target.
	TriggerAccess PolicyType = "trigger_access"
)

// Action is what a policy does. Three values, and the asymmetry between them
// is the whole of PL1: a Deny may come from anyone, an Allow only from an
// operator. Ask is the third — neither permit nor refusal — deny-like at the
// write boundary and middle in the fold (see its doc below).
type Action string

const (
	// Allow permits what the default denies (PL5 denies everything).
	Allow Action = "allow"

	// Deny refuses. A deny always beats an allow, whatever the relative
	// specificity of the two targets (PL4).
	Deny Action = "deny"

	// Ask is what a rule means when it declines to decide either way and
	// routes the question to a human: neither permit nor refusal. It is
	// DENY-LIKE at the write boundary (any of the four sources may author one
	// — an ask takes something away, the autonomy to act without asking, so
	// it cannot widen anyone's permissions) and it sits BELOW deny in the
	// fold: deny > ask > allow. A deny still beats an ask exactly as it beats
	// an allow (PL4 reasoning, unchanged); an ask beats an allow because a
	// rule that routes to a human outranks a rule that does not, and the
	// narrower grant may not silently close the door a broader rule opened
	// onto a person.
	Ask Action = "ask"
)

// Source is who wrote the policy — the field the write boundary checks (PL1).
//
// It is recorded at authoring and never rewritten, because "who said this" is
// the question an audit has to answer after the fact.
type Source string

const (
	// Operator is the human holding the keyboard. The only source an Allow may
	// come from.
	Operator Source = "operator"

	// Agent is a policy written by an agent about itself. It may only deny —
	// an agent that can allow itself has no ceiling.
	Agent Source = "agent"

	// MCPServer is a policy that arrived from a third-party MCP server, which
	// is as untrusted as it sounds. Deny only.
	MCPServer Source = "mcp_server"

	// Import is a policy that entered through a file or a migration rather
	// than through a person. Deny only.
	Import Source = "import"
)

// TargetKind is the kind of thing a policy points at. Five values, and each
// one answers a different question: who, which loop, which role, which server.
type TargetKind string

const (
	// AllAgents is every agent, present and future. It is the only kind that
	// carries no ID, and the only one whose Target.ID must be empty: the kind
	// already names everything, so an ID on it is a contradiction rather than
	// a detail.
	AllAgents TargetKind = "all-agents"

	// AgentKind names one agent by name.
	AgentKind TargetKind = "agent"

	// LoopKind names one loop by slug.
	LoopKind TargetKind = "loop"

	// LaneTypeKind governs by role rather than by identity — "no shell in any
	// review lane" — so it is the one target that survives an agent being
	// renamed.
	LaneTypeKind TargetKind = "lane-type"

	// MCPServerKind names one MCP server.
	MCPServerKind TargetKind = "mcp"
)

// The four closed lists. Unexported, and handed out only by copy, so the only
// way to change what the engine accepts is to edit the constants above.
var (
	policyTypes = []PolicyType{
		ToolAccess, CommandAccess, RouteAccess, EffectClass,
		CostCap, SkillAccess, TriggerAccess,
	}
	actions     = []Action{Allow, Deny, Ask}
	sources     = []Source{Operator, Agent, MCPServer, Import}
	targetKinds = []TargetKind{AllAgents, AgentKind, LoopKind, LaneTypeKind, MCPServerKind}
)

// AllPolicyTypes returns the seven policy types, in the order the design names
// them. The result is a copy: sorting or appending to it tells you nothing
// about what the engine will accept tomorrow.
func AllPolicyTypes() []PolicyType { return append([]PolicyType(nil), policyTypes...) }

// AllActions returns the three actions, deny-wins order aside — the order is
// the design's (deny > ask > allow), not an evaluation order: the fold lives
// in evaluate.go and reads the closed set, never this list's order.
func AllActions() []Action { return append([]Action(nil), actions...) }

// AllSources returns the four authoring sources. Exactly one of them may
// author an Allow; the list is what "closed" is asserted against.
func AllSources() []Source { return append([]Source(nil), sources...) }

// AllTargetKinds returns the five target kinds, `all-agents` first because it
// is the broadest rule there is.
func AllTargetKinds() []TargetKind { return append([]TargetKind(nil), targetKinds...) }

// Target is who or what a policy governs: a kind, and the name of the thing
// that kind names.
//
// ID is the BARE name — `review`, not `lane-type:review`. The canonical
// rendering is kind, then a colon, then ID ("lane-type:review", the shape PL8
// puts in a verdict), so an ID that repeats its own kind would render as
// `lane-type:lane-type:review`; ParseTarget refuses that rather than letting
// two spellings of one target disagree in the table.
//
// ID is empty if and only if Kind is AllAgents, and that rule is enforced by
// ParseTarget rather than documented and hoped for.
type Target struct {
	Kind TargetKind
	ID   string
}

// String renders the target the way every surface renders it: kind then name
// (`lane-type:review`), or the bare kind for `all-agents`, which names no
// thing in particular.
func (t Target) String() string {
	if t.Kind == AllAgents {
		return string(AllAgents)
	}
	if t.ID == "" {
		return string(t.Kind)
	}
	return string(t.Kind) + ":" + t.ID
}

// Policy is one rule, as it is stored and as it is evaluated.
//
// The field set is S4's table, and the json tags are the envelope
// DecodePolicy accepts: `id`, `type`, `target` (with `kind` and `id`),
// `action`, `spec`, `source`, `not_after`. The names match the column names
// the store writes, so the same policy reads the same in a fixture, a CLI
// flag file and a socket payload.
//
// Spec is raw, unvalidated JSON, on purpose: what a spec means is a property
// of Type, so it is validated per type on the write path (T5) and never
// earlier. A Policy decoded from bytes has been checked for shape and
// vocabulary; whether its spec means anything is a later question with a
// per-type answer.
//
// NotAfter is nil for a policy that never expires (PL7). It is never a
// sentinel and never the zero instant: `!p.NotAfter` is the only correct way
// to ask "does this expire?", and the test of that name is in types_test.go.
type Policy struct {
	ID       string          `json:"id"`
	Type     PolicyType      `json:"type"`
	Target   Target          `json:"target"`
	Action   Action          `json:"action"`
	Spec     json.RawMessage `json:"spec"`
	Source   Source          `json:"source"`
	NotAfter *time.Time      `json:"not_after"`
}

// Reject is a domain refusal: the input was well-formed enough to read and is
// not something the policy engine accepts (S4's DB19).
//
// It is deliberately NOT an error — it has no Error method and satisfies no
// error interface, because a caller receiving `error` has to guess whether the
// world broke or the input was simply wrong, and those two need different
// answers. Every parse and evaluation in this package returns *Reject, and a
// non-nil one is the whole signal: Reason is what to show a person.
//
// Reason always names the value that was refused. That is not polish: a
// refusal that says only "invalid policy" sends someone to read the source.
type Reject struct {
	Reason string
}

// rejectf builds a refusal from a format string. Every call site quotes the
// value it is refusing.
func rejectf(format string, args ...any) *Reject {
	return &Reject{Reason: fmt.Sprintf(format, args...)}
}

// ParsePolicyType turns a string into one of the seven types, or refuses.
//
// The comparison is byte-exact: the closed set is also what the database's
// CHECK stores, so a case or spacing variant that "means the same thing"
// would be a value the other side does not have.
func ParsePolicyType(s string) (PolicyType, *Reject) {
	for _, t := range policyTypes {
		if string(t) == s {
			return t, nil
		}
	}
	return "", rejectf("unknown policy type %q", s)
}

// ParseAction turns a string into allow, deny or ask, or refuses. The closed
// set is the one in this file: the wire value `ask` arrived with S11's D1
// (migration 0019 widened the database's CHECK to match), and anything else
// names the value it refused.
func ParseAction(s string) (Action, *Reject) {
	for _, a := range actions {
		if string(a) == s {
			return a, nil
		}
	}
	return "", rejectf("unknown action %q", s)
}

// ParseSource turns a string into one of the four authoring sources, or
// refuses. A source outside this set cannot be written at all, which is what
// makes the write boundary (T6) able to reason about it.
func ParseSource(s string) (Source, *Reject) {
	for _, s2 := range sources {
		if string(s2) == s {
			return s2, nil
		}
	}
	return "", rejectf("unknown source %q", s)
}

// ParseTargetKind turns a string into one of the five target kinds, or
// refuses.
func ParseTargetKind(s string) (TargetKind, *Reject) {
	for _, k := range targetKinds {
		if string(k) == s {
			return k, nil
		}
	}
	return "", rejectf("unknown target kind %q", s)
}

// ParseTarget constructs the one legal shape of Target, or refuses.
//
// The rules, and the reason each is here:
//
//   - Kind must be one of the five. `global` is refused by name — it is what
//     0005 stored, and an unconverted row must fail loudly, not evaluate as
//     nothing.
//   - AllAgents must carry no ID. The kind already governs every agent, so an
//     ID is either a no-op or a lie, and a reader should never have to work
//     out which.
//   - Every other kind must carry one. A `loop` target naming no loop would
//     match nothing and look like a policy that is not working.
//   - The ID must not repeat its kind (`"agent:boss"` under kind `agent`), so
//     that one target has exactly one spelling in the table and in a verdict.
func ParseTarget(kind TargetKind, id string) (Target, *Reject) {
	k, rej := ParseTargetKind(string(kind))
	if rej != nil {
		return Target{}, rej
	}
	if k == AllAgents {
		if id != "" {
			return Target{}, rejectf("target %q must not name an id, got %q — the kind means every agent, "+
				"so an id is a contradiction rather than a detail", string(AllAgents), id)
		}
		return Target{Kind: AllAgents}, nil
	}
	if id == "" {
		return Target{}, rejectf("target kind %q must name the thing it governs, got an empty id", string(k))
	}
	if len(id) > len(k) && id[:len(k)+1] == string(k)+":" {
		return Target{}, rejectf("target id %q repeats its kind %q; the id names the thing, "+
			"the rendering adds the kind", id, string(k))
	}
	return Target{Kind: k, ID: id}, nil
}

// DecodePolicy reads one policy from JSON and refuses anything that is not
// exactly the shape the engine accepts.
//
// # Why it is strict
//
// A typo in a field name is the dangerous kind of mistake: `{"actn":"allow"}`
// decodes, under a lenient reader, into a policy whose action is the empty
// string — and a policy nothing matches is a policy that permits everything,
// which is the opposite of what its author meant. So the decoder disallows
// unknown fields at every level, trailing content after the object is refused,
// and every closed-set value is put through its parser. The result is a Policy
// that cannot carry an unknown type, action, source or target kind, because
// there is no path from bytes to those fields that does not go through the
// closed set.
//
// A refusal here is a domain refusal (*Reject), not an error: malformed input
// is a wrong answer from the caller, not a failure of the machine.
//
// What it does NOT check is the spec. Spec meaning is per type and belongs to
// the write path (T5); shape and vocabulary belong here.
//
// What it DOES check — since the findings gate closed SEC-022/SEC-042 — is
// the action↔source pairing: an Allow may only be authored by an operator
// (PL1), and the check lands here because the load side is the ONE place that
// covers both routes to a Policy. Add guards the authoring route; bytes and
// stored columns arrive at this function — the socket, the CLI, decodeRow's
// re-read of a row that predates the check, and daemon.decodeFrozenSet's
// replay of a step's frozen manifest — so a rule enforced only at the write
// protects only the writes that went through the boundary. The rule is not
// restated here; it is called, from store.go (checkAuthorship), so it stays
// one rule in one place (D3).
func DecodePolicy(raw []byte) (Policy, *Reject) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var p Policy
	if err := dec.Decode(&p); err != nil {
		return Policy{}, rejectf("malformed policy: %v", err)
	}
	if dec.More() {
		return Policy{}, rejectf("malformed policy: trailing content after the policy object")
	}
	if p.ID == "" {
		return Policy{}, rejectf("policy has no id; a policy that cannot be named cannot be audited " +
			"or explained in a verdict")
	}

	typ, rej := ParsePolicyType(string(p.Type))
	if rej != nil {
		return Policy{}, rej
	}
	p.Type = typ

	act, rej := ParseAction(string(p.Action))
	if rej != nil {
		return Policy{}, rej
	}
	p.Action = act

	src, rej := ParseSource(string(p.Source))
	if rej != nil {
		return Policy{}, rej
	}
	p.Source = src

	target, rej := ParseTarget(p.Target.Kind, p.Target.ID)
	if rej != nil {
		return Policy{}, rej
	}
	p.Target = target

	// The pair, not just the vocabulary (SEC-022/SEC-042). Each closed set is checked field by field
	// above, and that is provably not enough: {"action":"allow","source":"agent"} is well-formed by
	// every one of those checks and is exactly the row — or the frozen manifest entry — that must not
	// evaluate. The rule is not restated here; it is called, from store.go, so it stays one rule in
	// one place (D3).
	if rej := checkAuthorship(p); rej != nil {
		return Policy{}, rej
	}

	return p, nil
}

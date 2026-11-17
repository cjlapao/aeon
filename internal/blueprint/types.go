package blueprint

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const SupportedSchemaVersion = 1

// The closed set of `after:` values (S39 CR1). The set is closed like every
// other vocabulary in this package: a value nobody compiled is a behavior
// nobody wrote.
const (
	AfterFinish   = "finish"
	AfterConverse = "converse"
)

type Blueprint struct {
	ID string `yaml:"id" json:"id"`
	// Title is the flow's friendly name — "Developer Loop" for dev-loop — what the
	// app shows; the id stays the slug every reference uses. Nothing executes on it.
	Title string `yaml:"title,omitempty" json:"title,omitempty"`
	// Description says what the flow is for, in a sentence or two: the line a
	// person reads in the library before opening it. Nothing executes on it.
	Description   string              `yaml:"description,omitempty" json:"description,omitempty"`
	SchemaVersion int                 `yaml:"schemaVersion" json:"schemaVersion"`
	Version       int                 `yaml:"version" json:"version"`
	Lanes         []Lane              `yaml:"lanes" json:"lanes"`
	Questions     map[string]Question `yaml:"questions" json:"questions"`
	OnAbort       string              `yaml:"on_abort,omitempty" json:"onAbort,omitempty"`
	// After says what a finished run leaves behind (S39 CR1). "" and
	// "finish" are today's behavior: the loop is done. "converse" leaves
	// the loop RESTING (idle) with the conversation open.
	After string `yaml:"after,omitempty" json:"after,omitempty"`
	// Converse names the agent that answers messages once a run has ended
	// (CR3); empty takes the daemon default (the `chat` agent). Legal only
	// with after: converse — an agent that talks after a run that never
	// leaves a conversation would be a lie the loader validated.
	Converse string `yaml:"converse,omitempty" json:"converse,omitempty"`
	// Kickoff is a text/template rendered into the run's first message
	// (CR6), so a typed and a templated start reach the model identically.
	Kickoff string `yaml:"kickoff,omitempty" json:"kickoff,omitempty"`
	// Output declares the DELIVERABLES the whole run exists to produce
	// (S38 T11, OL5): each entry is a catalog entity name (goal|epic|brief|
	// task — then some lane must emit it) or a file name (then some step
	// must produce or attach it). A declaration no lane can reach is
	// invalid at load, not a disappointment at the end of a long run.
	Output []string `yaml:"output,omitempty" json:"output,omitempty"`
	// Requires names the PREREQUISITES of the run (OL8): each requirement
	// is satisfied when ANY of its terms holds — an entity type with a
	// state it must already be in (goal:approved, task:approved).
	Requires []Requirement `yaml:"requires,omitempty" json:"requires,omitempty"`
}

type Lane struct {
	ID       string     `yaml:"id" json:"id"`
	Advisory bool       `yaml:"advisory,omitempty" json:"advisory,omitempty"`
	When     *Condition `yaml:"when,omitempty" json:"when,omitempty"`
	Step     *Step      `yaml:"step,omitempty" json:"step,omitempty"`
	Iterate  string     `yaml:"iterate,omitempty" json:"iterate,omitempty"`
	Ladder   []Step     `yaml:"ladder,omitempty" json:"ladder,omitempty"`
	Rework   string     `yaml:"rework,omitempty" json:"rework,omitempty"`
	OnReject *OnReject  `yaml:"on_reject,omitempty" json:"onReject,omitempty"`
	// Parallel is Shape B (S32 PA4): on an `iterate` lane it means readiness
	// per item comes from task.depends_on plus status, not from "the next in
	// ord" — a task is ready iff it is pending and every dependency reached a
	// successful terminal. Off by default: a lane with no Parallel runs exactly
	// as before (D8).
	Parallel bool `yaml:"parallel,omitempty" json:"parallel,omitempty"`
	// ParallelGroup is Shape A (S32 PA4): lanes sharing a non-empty name are a
	// declared-independent set the engine may dispatch together, bounded by
	// the component's cap. Overlapping produces across a group is an author
	// time `invalid` (PA11), not a runtime race. Empty opts out.
	ParallelGroup string `yaml:"parallel_group,omitempty" json:"parallelGroup,omitempty"`
}

type StepKind string

const (
	ModelStep  StepKind = "model-step"
	CodeAction StepKind = "code-action"
	Gate       StepKind = "gate"
)

type Step struct {
	ID    string     `yaml:"id,omitempty" json:"id,omitempty"`
	Kind  StepKind   `yaml:"kind" json:"kind"`
	When  *Condition `yaml:"when,omitempty" json:"when,omitempty"`
	Agent string     `yaml:"agent,omitempty" json:"agent,omitempty"`
	// Skill targets another agent's skill from a MODEL step whose Agent is
	// the caller — S14's shape, the amendment S8's design promised and T0
	// lands: `skill: "<callee>/<skill-id>"`. The blueprint layer checks the
	// TARGET (three states against the configured agent services — checked,
	// unverifiable when they are absent, invalid when unknown to a running
	// registry); the parameter CONTENTS belong to the skill's own typed
	// request (T2), which validates them before any model call.
	Skill    string         `yaml:"skill,omitempty" json:"skill,omitempty"`
	Params   map[string]any `yaml:"params,omitempty" json:"params,omitempty"`
	Action   string         `yaml:"action,omitempty" json:"action,omitempty"`
	Question string         `yaml:"question,omitempty" json:"question,omitempty"`
	Sets     string         `yaml:"sets,omitempty" json:"sets,omitempty"`
	Produces Paths          `yaml:"produces,omitempty" json:"produces,omitempty"`
	// Emits is the step's OUTPUT CONTRACT (S38 T11, OL9): the catalog
	// entities a model step files (the check that the step's handler is
	// GRANTED the creating tool is validation's, and an absent handler is
	// Unverifiable — P31's third state — never a guessed verdict).
	Emits []Emit `yaml:"emits,omitempty" json:"emits,omitempty"`
	// Decides names the entity a GATE step's human answer decides
	// (goal|brief only, gate kind only — OL3's boundary drawn in the
	// schema: a model step cannot decide anything).
	Decides  string    `yaml:"decides,omitempty" json:"decides,omitempty"`
	OnReject *OnReject `yaml:"on_reject,omitempty" json:"onReject,omitempty"`
	// MaxIterations bounds the model calls of THIS step (a model step only):
	// nil inherits (the agent's own bound, else the daemon setting, else no
	// bound), 0 says "no bound", N is a bound. An agent's own bound wins over
	// this one.
	MaxIterations *int `yaml:"max_iterations,omitempty" json:"maxIterations,omitempty"`
}

type ThenAction string

const (
	Escalate ThenAction = "escalate"
	FailItem ThenAction = "fail-item"
	Block    ThenAction = "block"
)

type OnReject struct {
	Goto    string            `yaml:"goto,omitempty" json:"goto,omitempty"`
	With    map[string]string `yaml:"with,omitempty" json:"with,omitempty"`
	Counter string            `yaml:"counter,omitempty" json:"counter,omitempty"`
	Max     int               `yaml:"max,omitempty" json:"max,omitempty"`
	Then    ThenAction        `yaml:"then,omitempty" json:"then,omitempty"`
}

type Question struct {
	Prompt  string   `yaml:"prompt" json:"prompt"`
	Answers []string `yaml:"answers" json:"answers"`
}

type Paths []string

// Emit is one declared deliverable of a model step. Type is a catalog
// entity (goal|epic|brief|task); Min counts how many (default 1 at the
// consuming end — validation admits 0 as "declare the shape, require
// none"); Attach names files (which the step must also produce) that get
// pinned to each emitted record — the pointer half of OL5's contract.
type Emit struct {
	Type   string `yaml:"type" json:"type"`
	Min    int    `yaml:"min,omitempty" json:"min,omitempty"`
	Attach Paths  `yaml:"attach,omitempty" json:"attach,omitempty"`
}

// Requirement is a prerequisite read as an OR; a RequirementTerm is one
// leg. State is the approval vocabulary, not the ladder's — only
// "approved" exists here, because nothing else a blueprint could name is a
// PERSON'S verdict (OL3).
type Requirement struct {
	AnyOf []RequirementTerm `yaml:"any_of" json:"anyOf"`
}

type RequirementTerm struct {
	Type  string `yaml:"type" json:"type"`
	State string `yaml:"state" json:"state"`
}

func (p *Paths) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value == "" {
			return fmt.Errorf("produces path is empty")
		}
		*p = Paths{n.Value}
		return nil
	case yaml.SequenceNode:
		var values []string
		if err := n.Decode(&values); err != nil {
			return err
		}
		*p = values
		return nil
	default:
		return fmt.Errorf("produces must be a path or sequence of paths")
	}
}

func (k *StepKind) UnmarshalYAML(n *yaml.Node) error {
	v := StepKind(n.Value)
	switch v {
	case ModelStep, CodeAction, Gate:
		*k = v
		return nil
	}
	return fmt.Errorf("unknown step kind %q; accepted: model-step, code-action, gate", n.Value)
}
func (t *ThenAction) UnmarshalYAML(n *yaml.Node) error {
	v := ThenAction(n.Value)
	switch v {
	case Escalate, FailItem, Block:
		*t = v
		return nil
	}
	return fmt.Errorf("unknown then action %q; accepted: escalate, fail-item, block", n.Value)
}

type Operator string

const (
	Eq  Operator = "eq"
	Neq Operator = "neq"
	In  Operator = "in"
	Gt  Operator = "gt"
	Gte Operator = "gte"
	Lt  Operator = "lt"
	Lte Operator = "lte"
)

type Condition struct {
	Field string      `yaml:"field,omitempty" json:"field,omitempty"`
	Op    Operator    `yaml:"op,omitempty" json:"op,omitempty"`
	Value any         `yaml:"value,omitempty" json:"value,omitempty"`
	All   []Condition `yaml:"all,omitempty" json:"all,omitempty"`
	Any   []Condition `yaml:"any,omitempty" json:"any,omitempty"`
	Not   *Condition  `yaml:"not,omitempty" json:"not,omitempty"`
}

func (b Blueprint) LaneNames() []string {
	names := make([]string, 0, len(b.Lanes))
	for _, l := range b.Lanes {
		names = append(names, l.ID)
	}
	return names
}
func (b Blueprint) String() string { return b.ID + "@" + strings.TrimSpace(fmt.Sprint(b.Version)) }

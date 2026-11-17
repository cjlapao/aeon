// Package autonomy is the pure core of S56 (AL2–AL4, AL5, AL9, AL14): the
// vocabulary derived from the action registry, the grant logic, the strict
// validation, the pure resolver Plan, the condition evaluator and its
// seams, and the sentences. It is the one implementation the start path
// (T4), the decision site (T5) and the socket (T6) all read through, so the
// legibility view can never drift from enforcement (AL14).
//
// It is pure by construction (I2): no store, no database, no exec, no net,
// and no clock read — a clock is an input, never a read. A test greps for
// the forbidden imports so the discipline is a fact, not a hope. The store
// owns the SQL half of the tables (AL10); the daemon and the CLI bridge
// this package to it. Nothing in here knows a table name.
package autonomy

import "time"

// State is the closed polarity of a grant row (AL3). A deny is a veto: it
// overrides every allow covering the same action, on the root or any of its
// children, regardless of order or age.
type State string

const (
	StateAllow State = "allow"
	StateDeny  State = "deny"
)

// Origin is the closed set of start origins that consult the policy at pin
// time (AL7). scheduler is reserved for S27 and has no caller in this slice.
type Origin string

const (
	OriginSchedule  Origin = "schedule"
	OriginWebhook   Origin = "webhook"
	OriginEvent     Origin = "event"
	OriginScheduler Origin = "scheduler"
)

// ClassShorthand is the single class a grant may name as a subject (AL2).
// A new outward action added to the registry is covered by an existing
// outward grant; an irreversible action is never covered by it.
const ClassShorthand = "outward"

// Subject is a grant's subject (AL2): either a registry action by name
// (kind "action") or the one class shorthand "outward" (kind "class").
// There is no irreversible class subject — an irreversible action is
// granted only by its own name.
type Subject struct {
	Kind string
	Name string
}

// ActionSubject names one registry action.
func ActionSubject(name string) Subject { return Subject{Kind: "action", Name: name} }

// ClassSubject names the one class shorthand.
func ClassSubject() Subject { return Subject{Kind: "class", Name: ClassShorthand} }

// Conditions is the closed, typed condition set (AL4). It is additional
// properties false at the wire: an unknown key is refused at write (the
// strict decode is in Validate). A deny carries no conditions.
type Conditions struct {
	// RequireGates is forced true on every irreversible (merge) grant — a
	// merge grant without it is unrepresentable (AL4/AL5).
	RequireGates bool `json:"requireGates"`
	// RequireBudget defaults true on every allow (AL4).
	RequireBudget bool `json:"requireBudget"`
	// Labels is the label set that must intersect the start's labels at pin
	// time; an empty list means any (AL4).
	Labels []string `json:"labels,omitempty"`
	// MaxPerDay is the count cap, ≥ 1, over the use log in the last 24 h.
	MaxPerDay int `json:"maxPerDay,omitempty"`
}

// Limits are the closed bounds a form and a validator share (AL15).
type Limits struct {
	MaxMergeGrantDays int
	MaxLabels         int
	MaxLabelLen       int
	MaxNoteBytes      int
	MaxReasonBytes    int
}

// Limits is the one definition of the closed bounds.
var limits = Limits{
	MaxMergeGrantDays: 90,
	MaxLabels:         8,
	MaxLabelLen:       64,
	MaxNoteBytes:      512,
	MaxReasonBytes:    512,
}

// LimitsValue returns the closed bounds; the daemon's vocabulary reads them.
func LimitsValue() Limits { return limits }

// Grant is the logic of a grant row (AL10); the store owns the SQL, the
// daemon bridges. It carries the fields the decision and the validation
// read; the actor and the audit live on the store row and the change log.
type Grant struct {
	ID         string
	NodeID     string // the node the row sits on: a root for allows, a root or child for denies
	RootID     string // the root of that node
	Subject    Subject
	State      State
	Conditions Conditions
	Note       string
	ExpiresAt  *time.Time // nil = no expiry
	ProposedBy string // the agent slug that proposed it, or ""
	CreatedAt  time.Time
	RevokedAt  *time.Time // nil = live
	RevokedBy  string
	RevokedReason string
}

// Active reports whether the grant is not revoked (a grant is never deleted;
// revoke sets the revoked_* fields once, AL5).
func (g Grant) Active() bool { return g.RevokedAt == nil }

// PinnedGrant is one grant's entry in a pin's allowed snapshot (AL8): the
// action it covers (expanded from the class shorthand at pin time), its
// id and the conditions carried so they can be evaluated at action time.
type PinnedGrant struct {
	GrantID    string
	Subject    string // the action name, expanded
	Conditions Conditions
}

// Pin is the immutable snapshot a policy-resolved loop is pinned with (AL8):
// the grants that applied at start, expanded to action names, with the
// revision the root held at pin time. A loop has a pin iff it was
// policy-resolved and at least one grant applied.
type Pin struct {
	Rev     int64
	Origin  Origin
	Labels  []string
	Allowed []PinnedGrant
}

// NodeInfo is the node facts the validator needs (AL2/AL3): whether the node
// is a root and whether it is archived. The store supplies it; the pure
// package decides.
type NodeInfo struct {
	IsRoot   bool
	Archived bool
}

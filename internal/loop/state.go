package loop

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// Position stores identities, not array offsets. Tasks can be added without
// silently changing which item the loop is working on.
type Position struct {
	Lane string `json:"lane"`
	Item string `json:"item,omitempty"`
	Step string `json:"step,omitempty"`
}

// CapHitState is the durable record of the cap a loop or a parked step hit
// (S55 T7). It is what a raise needs to find the budget it raises — id, scope
// and subject pick the row; the dimension names which of the four caps; the
// spend floor and the cap are what the hit said at the moment it was
// recorded, so the loop's view shows the numbers a person is being asked to
// raise against. It is set by both cap-hit arms (E1 the fresh-dispatch block,
// E2 the in-step suspension) and cleared by both raise CAS doors; nothing
// else writes it, so a loop that is not blocked on a cap carries no stale one.
type CapHitState struct {
	BudgetID   string  `json:"budgetId"`
	Scope      string  `json:"scope"`
	Subject    string  `json:"subject"`
	Dimension  string  `json:"dimension"`
	Window     string  `json:"window,omitempty"`
	SpendFloor float64 `json:"spendFloor"`
	Cap        float64 `json:"cap"`
	Reason     string  `json:"reason"`
	// Exact says the recorded floor was fully priced; the raise reads it
	// alongside the floor so its floor check never acts on an unknown floor.
	Exact bool `json:"exact,omitempty"`
}
type StepState struct {
	Status  string          `json:"status"`
	Outcome string          `json:"outcome,omitempty"`
	Reason  string          `json:"reason,omitempty"`
	StepID  int64           `json:"stepId,omitempty"`
	Cost    json.RawMessage `json:"cost,omitempty"`
}
type RuntimeState struct {
	Route string `json:"route,omitempty"`
	// ReasoningEffort and Thinking are the model options the person chose when the loop started (the Home
	// composer's advanced options). They ride every model step of the loop; empty takes the route's own.
	ReasoningEffort string   `json:"reasoningEffort,omitempty"`
	Thinking        string   `json:"thinking,omitempty"`
	Position        Position `json:"position"`
	// Positions is S32's PA13 widening, done additively rather than by rename.
	// The serial path (cap 1) reads Position, unchanged — so at cap 1 this
	// field is nil and omitempty keeps the saved JSON byte-identical to today
	// (D8: the serial path is not a special case of the parallel path, it is
	// the unchanged code the existing suite proves). A cap > 1 run populates
	// Positions with every concurrently-dispatched unit; each unit runs on its
	// own copy of LoopState whose Position is one element of this set, so the
	// widening is a second field beside the primary, not a rename that touches
	// every reader R2 named.
	Positions  []Position           `json:"positions,omitempty"`
	LaneStates map[string]string    `json:"laneStates"`
	StepStates map[string]StepState `json:"stepStates"`
	Counters   map[string]int       `json:"counters"`
	Flags      map[string]bool      `json:"flags"`
	Answers    map[string]string    `json:"answers"`
	Tasks      []string             `json:"tasks"`
	With       map[string]string    `json:"with,omitempty"`
	// Kickoff is the current run's first message (S39 CR6): typed by a
	// person or rendered from the blueprint's template — the model sees the
	// one and cannot tell the other. Empty for a run that was not given
	// one, and modelPrompt then uses Goal exactly as before (D1: every
	// loop started before S39 keeps its byte-identical prompt).
	Kickoff string `json:"kickoff,omitempty"`
	// WorkDir is S60 T5's step-workspace seam (MQ7): when set, the loop's
	// model steps and code actions run THERE rather than in Root — the
	// landing loop works inside its integration worktree. Additive by PA13's
	// argument (never a rename, never a moved field): omitempty keeps every
	// pre-S60 state's saved JSON byte-identical, and empty means today's
	// behaviour exactly — Root everywhere, goldens unmoved. The only writer
	// from evidence is applyEvidenceWorkDir (execute.go), reacting to a
	// git.integrate result's integration_dir; S32's parallel units carry it
	// per-unit in runOneUnit, never through the saved state.
	WorkDir string `json:"workDir,omitempty"`
	// RouteOverrides is the per-lane route a person chose for a LOCKED
	// (shipped) blueprint, keyed by lane id. loop.StartWith COPIES the rows
	// here at start, so a later override never touches a loop in flight (the
	// pin holds); a lane with no entry runs on Route. omitempty keeps every
	// state that never had one byte-identical.
	RouteOverrides map[string]string `json:"routeOverrides,omitempty"`
	// Redos is the pending context for rework dispatches (S17 T3): the
	// rejection the engine carries into the re-entered step, keyed by
	// stepKey(position). Rows, not memory — a suspension between the reject
	// and the rework must not lose the reason (S6's discipline: resume is
	// cold, from rows).
	Redos      map[string]Redo `json:"redos,omitempty"`
	QuestionID int64           `json:"questionId,omitempty"`
	// PendingAction is the S16 durable half of an outward ask (T10): a
	// crash between asking and answering re-consults the SAME question
	// identity instead of opening a second permission window.
	PendingAction *PendingAction `json:"pendingAction,omitempty"`
	// Evidence is the Q7 record: per-position numbers/SHAs/refs returned
	// by dispatched actions — never prose, and the transcript mirrors
	// this map, not the other way round.
	Evidence    map[string]map[string]string `json:"evidence,omitempty"`
	LastOutcome string                       `json:"lastOutcome,omitempty"`
	LastReason  string                       `json:"lastReason,omitempty"`
	// BlockKind is S60 T7: WHY this loop is blocked, as the kind of the last
	// blockTo — "action-conflict", "action-denied", "action-denied-by-person",
	// "action-refused", "action-failed", or one of the engine's other block
	// kinds. LastReason is prose for a person; the merge-queue reactor reads
	// THIS (B6's reading of a parked landing loop: a conflict and a denial
	// are different queue futures, and telling them apart from a sentence
	// would be reading tea leaves). Cleared whenever an action succeeds, so
	// a non-empty kind always describes the CURRENT block; the AnswerGate
	// arm that blocks from a gate answer never sets it — a gate rejection
	// is a routing outcome, not an action verdict.
	BlockKind    string `json:"blockKind,omitempty"`
	LastStepID   int64  `json:"lastStepId,omitempty"`
	StepInFlight bool   `json:"stepInFlight,omitempty"`
	// CapHit is the S55 T7 cap that parked this loop (or the step it parked
	// on, for an E2 suspension): nil unless the block/suspension was a cap
	// hit. A raise reads it to find which budget to raise and which
	// dimension; a resume or any other clear path leaves it alone — only the
	// two raise CAS doors clear it (the store merges capHit=nil), and the
	// cap-hit arms are the only writers, so a loop that was never capped
	// carries no stale record.
	CapHit *CapHitState `json:"capHit,omitempty"`
}
type LoopState struct {
	ID            string
	Slug          string
	Mode          string
	Status        string
	Goal          string
	Root          string
	ComponentRoot string
	StartedAt     string
	UpdatedAt     string
	Blueprint     *blueprint.Blueprint
	PinnedContent string
	// S60 MQ15: the shape loop.start PINNED beside this row (empty for any
	// loop started before S60 — those keep deriving names exactly as
	// today). Transient like TaskRows: never part of RuntimeState, never
	// marshalled, so the persisted bytes do not move (D8).
	FeatureBranch string
	BaseBranch    string
	DefaultBranch string
	RuntimeState
	// TaskRows is S32 T5's Shape-B readiness input (PA4): the iterate lane's
	// task rows, keyed by task id, that the cap>1 decision reads to decide
	// which pending item whose depends_on are all in a successful terminal is
	// ready. It is a TRANSIENT loop-package field, never part of RuntimeState,
	// so it is not marshalled and cannot move the persisted bytes (D8: the
	// serial path at cap 1 reads none of it and stays byte-identical). The
	// fan-out (AdvanceLoopParallel) fills it from the store; the decision tests
	// set it directly.
	TaskRows map[string]store.Task
}

func InitialState(b blueprint.Blueprint) RuntimeState {
	st := RuntimeState{LaneStates: map[string]string{}, StepStates: map[string]StepState{}, Counters: map[string]int{}, Flags: map[string]bool{}, Answers: map[string]string{}, Tasks: []string{}, With: map[string]string{}}
	if len(b.Lanes) > 0 {
		st.Position.Lane = b.Lanes[0].ID
	}
	for _, l := range b.Lanes {
		st.LaneStates[l.ID] = "pending"
		if l.When != nil {
			collectFields(*l.When, &st)
		}
		if l.Step != nil {
			if l.Step.When != nil {
				collectFields(*l.Step.When, &st)
			}
			collectCounter(l.Step.OnReject, &st)
		}
		for _, s := range l.Ladder {
			if s.When != nil {
				collectFields(*s.When, &st)
			}
			collectCounter(s.OnReject, &st)
		}
		collectCounter(l.OnReject, &st)
	}
	for q := range b.Questions {
		st.Answers[q] = ""
	}
	return st
}
func collectCounter(e *blueprint.OnReject, st *RuntimeState) {
	if e != nil && e.Counter != "" {
		st.Counters[e.Counter] = 0
	}
}
func collectFields(c blueprint.Condition, st *RuntimeState) {
	if strings.HasPrefix(c.Field, "flags.") {
		st.Flags[strings.TrimPrefix(c.Field, "flags.")] = false
	}
	if strings.HasPrefix(c.Field, "counters.") {
		st.Counters[strings.TrimPrefix(c.Field, "counters.")] = 0
	}
	if strings.HasPrefix(c.Field, "answers.") {
		st.Answers[strings.TrimPrefix(c.Field, "answers.")] = ""
	}
	for _, child := range c.All {
		collectFields(child, st)
	}
	for _, child := range c.Any {
		collectFields(child, st)
	}
	if c.Not != nil {
		collectFields(*c.Not, st)
	}
}

func (s LoopState) Values() blueprint.Values {
	v := blueprint.NewValues()
	v["mode"] = s.Mode
	v["tasks"] = append([]string(nil), s.Tasks...)
	v["tasks.count"] = len(s.Tasks)
	for k, value := range s.Flags {
		v["flags."+k] = value
	}
	for k, value := range s.Counters {
		v["counters."+k] = value
	}
	for k, value := range s.Answers {
		v["answers."+k] = value
	}
	return v
}

func FromPinned(pin store.PinnedLoop) (LoopState, error) {
	bp, err := blueprint.Load([]byte(pin.Content))
	if err != nil {
		return LoopState{}, fmt.Errorf("pinned blueprint for loop %s is unparseable: %w", pin.Loop.ID, err)
	}
	var st RuntimeState
	if err := json.Unmarshal([]byte(pin.RuntimeJSON), &st); err != nil {
		return LoopState{}, fmt.Errorf("loop %s has an invalid saved position: %w", pin.Loop.ID, err)
	}
	return LoopState{ID: pin.Loop.ID, Slug: pin.Loop.Slug, Mode: pin.Loop.Mode, Status: pin.Loop.Status, Goal: pin.Loop.Goal, Root: pin.Loop.Root, ComponentRoot: pin.Loop.ComponentRoot, StartedAt: pin.Loop.StartedAt, UpdatedAt: pin.Loop.UpdatedAt, Blueprint: &bp, PinnedContent: pin.Content, FeatureBranch: pin.Loop.FeatureBranch, BaseBranch: pin.Loop.BaseBranch, DefaultBranch: pin.Loop.DefaultBranch, RuntimeState: st}, nil
}

func (s LoopState) RuntimeJSON() (string, error) {
	raw, err := json.Marshal(s.RuntimeState)
	return string(raw), err
}

// missingHandlerMarker is the fragment of the daemon's recorded failure that
// identifies a missing-handler block: the step runtime refuses an agent the
// registry does not have with `invalid params: unknown handler "NAME"`, and
// the block records that text verbatim as LastReason. Historical S9-era
// loops blocked this way before S13 registered the handlers; T10's retry
// surface exists for exactly that population — the marker is the ONLY
// classification, so a block from any other cause stays refused and
// untouched.
const missingHandlerMarker = `unknown handler "`

// MissingHandlerBlock names the agent whose absence blocked the loop, when
// the block is of that kind. It reads only the loop's OWN durable state —
// status and the recorded reason — never the live registry: eligibility
// (does the handler exist NOW) is a separate question asked separately, so
// the daemon can render "eligible, and why not" from the daemon side
// without a store read per handler name.
func (st LoopState) MissingHandlerBlock() (string, bool) {
	if st.Status != "blocked" {
		return "", false
	}
	i := strings.Index(st.LastReason, missingHandlerMarker)
	if i < 0 {
		return "", false
	}
	rest := st.LastReason[i+len(missingHandlerMarker):]
	j := strings.Index(rest, `"`)
	if j <= 0 {
		return "", false
	}
	return rest[:j], true
}

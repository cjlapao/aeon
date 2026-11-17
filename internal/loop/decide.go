package loop

import (
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
)

// UnverifiedPrefix is the canonical leading clause of a gate that could
// not run (S17 T6, L33/D8). It is RENDERED by the handler layer from the
// structured verification FIELD (internal/handler/handoff.go) — the engine
// reads only this prefix and never parses prose: a reject that merely
// WRITES about 137/OOM is a finding to fix, not a check that failed to
// run. P31's fifth instance, and W5 consumed rather than re-derived.
const UnverifiedPrefix = "unverified: "

func IsUnverifiedReason(reason string) bool {
	return strings.HasPrefix(reason, UnverifiedPrefix)
}

// storedOutcome records the honest third value: a reject whose reason says
// the gate did not run stores `unverified`, never the collapse to pass —
// and routing still sees it as the reject it is (D8: asserted on the
// stored value, because the record is what a person reads afterwards).
func storedOutcome(outcome, reason string) string {
	if outcome == "reject" && IsUnverifiedReason(reason) {
		return "unverified"
	}
	return outcome
}

// Decide reads only its arguments. Every malformed position returns a
// structured blocked action, including nil blueprints and missing task ids.
func Decide(bp *blueprint.Blueprint, st LoopState) Action {
	if bp == nil {
		return block("pin-unparseable", "the pinned blueprint is missing")
	}
	st.Blueprint = bp
	// S39 CR1: an idle loop decides to nothing. The guard sits BEFORE the
	// terminal check's neighborhood for one reason — a resting loop has an
	// EMPTY position, and without this branch its next decision would be
	// ActDone (empty lane), which ends a conversation that is still open.
	if st.Status == StatusIdle {
		return Action{Kind: ActIdle}
	}
	if st.Status == "done" || st.Status == "aborted" {
		return Action{Kind: ActDone}
	}
	if st.Status == "waiting" {
		return Action{Kind: ActWaiting, Position: st.Position}
	}
	if st.Status == "blocked" {
		return block("saved-block", "the loop is blocked")
	}
	if st.Position.Lane == "" {
		return Action{Kind: ActDone}
	}
	laneIndex := -1
	for i, l := range bp.Lanes {
		if l.ID == st.Position.Lane {
			laneIndex = i
			break
		}
	}
	if laneIndex < 0 {
		return block("contradictory-state", fmt.Sprintf("lane %q is absent from the pin", st.Position.Lane))
	}
	lane := bp.Lanes[laneIndex]
	if lane.When != nil {
		ok, err := lane.When.Evaluate(st.Values())
		if err != nil {
			return block("condition-error", err.Error())
		}
		if !ok {
			return Action{Kind: ActSkip, Position: st.Position, SkipAs: skipName(lane.When)}
		}
	}
	if lane.Step != nil {
		if st.Position.Item != "" || st.Position.Step != "" {
			return block("contradictory-state", "single-step lane has an item or step position")
		}
		return decideStep(*lane.Step, st)
	}
	if len(lane.Ladder) == 0 {
		return block("contradictory-state", "lane has no step or ladder")
	}
	if len(st.Tasks) == 0 {
		return Action{Kind: ActSkip, Position: st.Position, SkipAs: "not-needed"}
	}
	item := st.Position.Item
	if item == "" {
		item = st.Tasks[0]
	}
	found := false
	for _, id := range st.Tasks {
		if id == item {
			found = true
			break
		}
	}
	if !found {
		return block("contradictory-state", fmt.Sprintf("task item %q is absent", item))
	}
	stepID := st.Position.Step
	if stepID == "" {
		stepID = lane.Ladder[0].ID
	}
	for _, step := range lane.Ladder {
		if step.ID == stepID {
			return decideStep(step, LoopState{ID: st.ID, Slug: st.Slug, Mode: st.Mode, Status: st.Status, Goal: st.Goal, Root: st.Root, ComponentRoot: st.ComponentRoot, Blueprint: st.Blueprint, PinnedContent: st.PinnedContent, RuntimeState: withPosition(st.RuntimeState, Position{Lane: lane.ID, Item: item, Step: stepID})})
		}
	}
	return block("contradictory-state", fmt.Sprintf("step %q is absent from lane %q", stepID, lane.ID))
}

func withPosition(st RuntimeState, pos Position) RuntimeState { st.Position = pos; return st }
func skipName(c *blueprint.Condition) string {
	if c != nil && c.Field == "mode" {
		return "skipped"
	}
	return "not-needed"
}

func decideStep(step blueprint.Step, st LoopState) Action {
	if step.When != nil {
		ok, err := step.When.Evaluate(st.Values())
		if err != nil {
			return block("condition-error", err.Error())
		}
		if !ok {
			return Action{Kind: ActSkip, Position: st.Position, SkipAs: skipName(step.When)}
		}
	}
	a := Action{Position: st.Position, Step: &step}
	switch step.Kind {
	case blueprint.ModelStep:
		a.Kind = ActRunModelStep
	case blueprint.CodeAction:
		a.Kind = ActRunCodeAction
	case blueprint.Gate:
		a.Kind = ActAskGate
		q, ok := st.Blueprint.Questions[step.Question]
		if !ok {
			return block("contradictory-state", fmt.Sprintf("question %q is absent", step.Question))
		}
		a.Question = &q
	default:
		return block("contradictory-state", fmt.Sprintf("unknown step kind %q", step.Kind))
	}
	return a
}

func NextPosition(bp *blueprint.Blueprint, st LoopState, pos Position) (Position, error) {
	if bp == nil {
		return Position{}, fmt.Errorf("no pinned blueprint")
	}
	laneIndex := -1
	for i, l := range bp.Lanes {
		if l.ID == pos.Lane {
			laneIndex = i
			break
		}
	}
	if laneIndex < 0 {
		return Position{}, fmt.Errorf("lane %q absent", pos.Lane)
	}
	l := bp.Lanes[laneIndex]
	if len(l.Ladder) > 0 && len(st.Tasks) > 0 {
		item := pos.Item
		if item == "" {
			item = st.Tasks[0]
		}
		itemIndex := -1
		for i, id := range st.Tasks {
			if id == item {
				itemIndex = i
				break
			}
		}
		if itemIndex < 0 {
			return Position{}, fmt.Errorf("task %q absent", item)
		}
		step := pos.Step
		if step == "" {
			step = l.Ladder[0].ID
		}
		stepIndex := -1
		for i, s := range l.Ladder {
			if s.ID == step {
				stepIndex = i
				break
			}
		}
		if stepIndex < 0 {
			return Position{}, fmt.Errorf("step %q absent", step)
		}
		if stepIndex+1 < len(l.Ladder) {
			return Position{Lane: l.ID, Item: item, Step: l.Ladder[stepIndex+1].ID}, nil
		}
		if itemIndex+1 < len(st.Tasks) {
			return Position{Lane: l.ID, Item: st.Tasks[itemIndex+1], Step: l.Ladder[0].ID}, nil
		}
	}
	if laneIndex+1 < len(bp.Lanes) {
		return Position{Lane: bp.Lanes[laneIndex+1].ID}, nil
	}
	return Position{}, nil
}

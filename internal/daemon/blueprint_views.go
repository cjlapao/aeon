// blueprint_views.go — S48 T6 (SIZE1 split of blueprint.go by cohesion):
// how a loop READS — the protocol views (lanes, items, steps, runs, control,
// the retry refusal), the counter view, and the two viewLoop renderers plus
// the loop-status reader a refusal borrows. A reader who opened blueprint.go
// "to change what loop.show answers" now opens exactly this file.

package daemon

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

type LoopLaneView struct {
	ID        string         `json:"id"`
	Structure string         `json:"structure"`
	Status    string         `json:"status"`
	Condition string         `json:"condition,omitempty"`
	Iterate   string         `json:"iterate,omitempty"`
	ItemCount int            `json:"itemCount,omitempty"`
	Produces  []string       `json:"produces,omitempty"`
	Steps     []LoopStepView `json:"steps,omitempty"`
	Items     []LoopItemView `json:"items,omitempty"`
}
type LoopItemView struct {
	ID     string         `json:"id"`
	Status string         `json:"status"`
	Steps  []LoopStepView `json:"steps"`
}
type LoopStepView struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Outcome string `json:"outcome,omitempty"`
	Reason  string `json:"reason,omitempty"`
	StepID  int64  `json:"stepId,omitempty"`
	// Agent is the RESOLVED handler the dispatch recorded on the step row —
	// for a `by-tech-tag` lane this is the proof the resolver ran
	// (golang-engineer, not the tag). Absent with no step row yet: never a
	// guess from the blueprint text, which the resolver may rewrite.
	Agent     string          `json:"agent,omitempty"`
	Cost      json.RawMessage `json:"cost,omitempty"`
	StartedAt string          `json:"startedAt,omitempty"`
	EndedAt   string          `json:"endedAt,omitempty"`
	WallMS    *int            `json:"wallMs,omitempty"`
}

// RunView is one row of loop.show's history (S39 T9, CR2): every run the
// loop has walked, with the P32 cost FLOOR beside it — usdAtLeast with its
// partial/unknown counts intact, never a stored total pretending to be
// exact. The runs array is the loop's memory; the pin is only its now.
type RunView struct {
	RunSeq           int           `json:"runSeq"`
	Blueprint        string        `json:"blueprint"`
	Version          int           `json:"version"`
	State            string        `json:"state"`
	StartedByKind    string        `json:"startedByKind"`
	StartedByID      string        `json:"startedById,omitempty"`
	GoalID           string        `json:"goalId,omitempty"`
	GoalVersion      int           `json:"goalVersion,omitempty"`
	TaskID           string        `json:"taskId,omitempty"`
	KickoffMessageID int64         `json:"kickoffMessageId,omitempty"`
	StartedAt        string        `json:"startedAt"`
	EndedAt          string        `json:"endedAt,omitempty"`
	Cost             store.RunCost `json:"cost"`
}

type LoopView struct {
	ID            string         `json:"id"`
	Slug          string         `json:"slug"`
	Blueprint     string         `json:"blueprint"`
	Version       int            `json:"version"`
	Goal          string         `json:"goal"`
	Root          string         `json:"root"`
	Status        string         `json:"status"`
	Position      loop.Position  `json:"position"`
	QuestionID    int64          `json:"questionId,omitempty"`
	GateAnswers   []string       `json:"gateAnswers,omitempty"`
	GatePrompt    string         `json:"gatePrompt,omitempty"`
	Lanes         []LoopLaneView `json:"lanes"`
	PinnedContent string         `json:"pinnedContent,omitempty"`
	LastReason    string         `json:"lastReason,omitempty"`
	// Title is the loop's short display name: its goal cut down at first, then the title made for it. Goal is the
	// full request.
	Title string `json:"title,omitempty"`
	// Route, ReasoningEffort and Thinking are the model the loop's steps run on and the options chosen for it
	// (empty means the route's own), so a reopened page shows what the loop is using instead of "default".
	Route           string `json:"route,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	Thinking        string `json:"thinking,omitempty"`
	// StartedBy is S20's provenance (R-3): "trigger"/"webhook" beside the
	// schedule id, "" for a person's loop — read from the row beside the
	// counters, and its emptiness is an answer ("a person started this"),
	// never a missing one.
	StartedBy string `json:"startedBy,omitempty"`
	// Runs is the loop's history (S39 T9): every run, in order, each with
	// its cost floor. Empty on a store read failure — the view never fails
	// for its history, and an empty array and an unread one are told apart
	// by the loop still answering its pin beside it.
	Runs []RunView `json:"runs,omitempty"`
	// RunSeq is the CURRENT run's number — the highest there is.
	RunSeq int `json:"runSeq,omitempty"`
	// Attended is the D2 declaration as THREE states (P31): true means a
	// human said "someone is here", false says nobody is, and null says
	// nobody ever said — the UI's unattended notice (T12) cannot be honest
	// without the third value, so there is no omitempty to collapse it.
	Attended  *bool  `json:"attended"`
	StartedAt string `json:"startedAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	// Counters (S17 T2): the two-level budgets exactly as the store rows
	// have them — `aeon loop show --counters` and the app's counters table
	// both render THESE and recompute nothing. A client-side tally would be
	// a second counter that can disagree with the one that stops the loop.
	LoopCounters map[string]int `json:"loopCounters,omitempty"`
	// LoopCounterCaps publishes the engine's loop-level caps (loop.Table)
	// so the counters table renders "1/10" from the daemon's data — the
	// ceiling a page invents is a second ceiling (S17 T10).
	LoopCounterCaps map[string]int    `json:"loopCounterCaps,omitempty"`
	TaskCounters    []TaskCounterView `json:"taskCounters,omitempty"`
	// Retry (T10): the daemon's eligibility verdict for a blocked loop, so
	// the app shows what the DAEMON decided — eligible, or refused and why.
	// Only the daemon reads the live registry; a UI-side guess would be a
	// second dispatcher's opinion.
	RetryEligible bool              `json:"retryEligible,omitempty"`
	RetryAgent    string            `json:"retryAgent,omitempty"`
	RetryRefusal  *RetryRefusalView `json:"retryRefusal,omitempty"`
	// CostAttribution is the ledger S18 T7 exists to produce: per-route
	// lines and the total, grouped by route (O4), computed ONCE by
	// model.AttributeCosts so the CLI table and the app's footprint panel
	// are two renderers of one computation and never two sums (D16, P34).
	// A step spanning providers bills toward the route that answered
	// (M26); steps with no provenance land on the unattributed line.
	CostAttribution *model.Attribution `json:"costAttribution,omitempty"`
	// Control is the loop.show control block (LC16): the daemon's eligibility
	// verdicts for stop/resume/cancel, computed by the SAME functions the ops
	// call (loop.CheckStop|CheckResume|CheckCancel), so the UI renders the
	// daemon's answer, never its own guess (the CheckHandlerRetry precedent).
	// T7 adds raise, openRequests, and capHit.
	Control *LoopControlView `json:"control,omitempty"`
}

// RetryRefusalView renders loop.RetryRefusal for the app, verbatim.
type RetryRefusalView struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// LoopControlView is the loop.show control block (LC16). Each action is the
// daemon's own eligibility verdict — the SAME function the op runs as a gate —
// so the button and the engine cannot disagree (the CheckHandlerRetry shape).
// T7 adds Raise (loop.CheckRaise — the same gate the raise op runs) and
// CapHit (the cap the loop is parked on, T5's wire shape).
type LoopControlView struct {
	Stop   ControlActionView `json:"stop"`
	Resume ControlActionView `json:"resume"`
	Cancel ControlActionView `json:"cancel"`
	Raise  ControlActionView `json:"raise"`
	// CapHit is nil unless the loop's pin carries a cap — blocked/cap-hit,
	// or the waiting/cap-hit window a raise committed before its kick. The
	// hit's float floors/caps come from the engine's own numbers; the wire
	// view is T5's int64 shape (the hit event keeps the exact values).
	CapHit *protocol.LoopCapHitView `json:"capHit,omitempty"`
}

// ControlActionView is one control action's eligibility: eligible true, or a
// refusal naming the reason and the thing refused (DB19 — a refusal is a
// result, not an error; Detail is the person's sentence the app prints
// verbatim).
type ControlActionView struct {
	Eligible bool                `json:"eligible"`
	Refusal  *ControlRefusalView `json:"refusal,omitempty"`
}

// ControlRefusalView renders a store.ControlRefusal for the app, verbatim.
type ControlRefusalView struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// controlAction renders one eligibility verdict: a nil refusal is eligible; a
// non-nil one names the reason and the thing refused. It is the one renderer
// of a control eligibility, so the loop.show block and the op's gate answer
// the same question with the same words.
func controlAction(refusal *store.ControlRefusal) ControlActionView {
	if refusal == nil {
		return ControlActionView{Eligible: true}
	}
	return ControlActionView{Eligible: false, Refusal: &ControlRefusalView{Kind: refusal.Kind, Detail: refusal.Detail}}
}

// Check from the daemon's filesystem before pinning a loop. A UI process can
// see a path that a daemon in another namespace or mount cannot; discovering
// that through the model's first list call wastes a provider call and leaves a
// blocked loop in the store.

func (d *Daemon) loopStatus(id string) string {
	if pin, err := d.store.ReadPinnedLoop(id); err == nil {
		return pin.Loop.Status
	}
	return "unknown"
}

// publishLoop adds a durable, post-commit view to loop events. It is additive
// wire data: older listeners can keep using the original event fields.

func viewLoop(st loop.LoopState, includeBlueprint bool) LoopView {
	v := LoopView{ID: st.ID, Slug: st.Slug, Blueprint: st.Blueprint.ID, Version: st.Blueprint.Version, Goal: st.Goal, Root: st.Root, Status: st.Status, Position: st.Position, QuestionID: st.QuestionID, Lanes: make([]LoopLaneView, 0, len(st.Blueprint.Lanes)), LastReason: st.LastReason, StartedAt: st.StartedAt, UpdatedAt: st.UpdatedAt,
		Route: st.Route, ReasoningEffort: st.ReasoningEffort, Thinking: st.Thinking}
	if includeBlueprint {
		v.PinnedContent = st.PinnedContent
	}
	if st.Status == "waiting" {
		for _, lane := range st.Blueprint.Lanes {
			if lane.ID != st.Position.Lane {
				continue
			}
			question := ""
			if lane.Step != nil && lane.Step.Kind == blueprint.Gate {
				question = lane.Step.Question
			}
			stepID := st.Position.Step
			if stepID == "" && len(lane.Ladder) > 0 {
				stepID = lane.Ladder[0].ID
			}
			for _, step := range lane.Ladder {
				if step.ID == stepID && step.Kind == blueprint.Gate {
					question = step.Question
				}
			}
			if q, ok := st.Blueprint.Questions[question]; ok {
				v.GateAnswers = append([]string(nil), q.Answers...)
				v.GatePrompt = q.Prompt
			}
			break
		}
	}
	for _, lane := range st.Blueprint.Lanes {
		status := st.LaneStates[lane.ID]
		if status == "" {
			status = "pending"
		}
		if lane.ID == st.Position.Lane {
			if st.Status == "waiting" {
				status = "waiting"
			} else if st.Status == "blocked" {
				status = "blocked"
			} else if st.Status == "running" {
				status = "running"
			}
		}
		structure := "ladder"
		if lane.Step != nil {
			structure = "direct"
		} else if lane.Iterate != "" {
			structure = "iterate"
		}
		lv := LoopLaneView{ID: lane.ID, Structure: structure, Status: status, Condition: blueprint.ConditionSummary(lane.When), Iterate: lane.Iterate}
		if lane.Step != nil {
			lv.Produces = append([]string(nil), lane.Step.Produces...)
			state := st.StepStates[lane.ID]
			stepState := stepStatus(state.Status, status)
			if lane.Step.Kind == blueprint.Gate && st.Status == "waiting" && st.Position.Lane == lane.ID {
				stepState = "waiting"
			}
			lv.Steps = []LoopStepView{{ID: lane.ID, Kind: string(lane.Step.Kind), Status: stepState, Outcome: state.Outcome, Reason: state.Reason, StepID: state.StepID, Cost: state.Cost}}
		}
		if lane.Iterate != "" {
			lv.ItemCount = len(st.Tasks)
			for _, item := range st.Tasks {
				iv := LoopItemView{ID: item, Status: "pending", Steps: make([]LoopStepView, 0, len(lane.Ladder))}
				complete := len(lane.Ladder) > 0
				for _, step := range lane.Ladder {
					state := st.StepStates[lane.ID+"/"+item+"/"+step.ID]
					stepState := stepStatus(state.Status, status)
					if state.Status == "" && status != "skipped" && status != "not-needed" {
						stepState = "pending"
					}
					if step.Kind == blueprint.Gate && st.Status == "waiting" &&
						st.Position.Lane == lane.ID && st.Position.Item == item && (st.Position.Step == "" || st.Position.Step == step.ID) {
						stepState = "waiting"
					}
					if stepState != "done" && stepState != "skipped" && stepState != "not-needed" {
						complete = false
					}
					iv.Steps = append(iv.Steps, LoopStepView{ID: step.ID, Kind: string(step.Kind), Status: stepState, Outcome: state.Outcome, Reason: state.Reason, StepID: state.StepID, Cost: state.Cost})
				}
				switch {
				case complete:
					iv.Status = "done"
				case st.Position.Lane == lane.ID && st.Position.Item == item:
					iv.Status = status
				case status == "skipped" || status == "not-needed":
					iv.Status = status
				}
				lv.Items = append(lv.Items, iv)
			}
		} else {
			for _, step := range lane.Ladder {
				state := st.StepStates[lane.ID+"/"+step.ID]
				stepState := stepStatus(state.Status, status)
				if step.Kind == blueprint.Gate && st.Status == "waiting" && st.Position.Lane == lane.ID &&
					(st.Position.Step == "" || st.Position.Step == step.ID) {
					stepState = "waiting"
				}
				lv.Steps = append(lv.Steps, LoopStepView{ID: step.ID, Kind: string(step.Kind), Status: stepState, Outcome: state.Outcome, Reason: state.Reason, StepID: state.StepID, Cost: state.Cost})
			}
		}
		v.Lanes = append(v.Lanes, lv)
	}
	if st.Status == "blocked" {
		if r := loop.CheckHandlerRetry(st); r != nil {
			v.RetryRefusal = &RetryRefusalView{Kind: r.Kind, Detail: r.Detail}
		} else if agent, ok := st.MissingHandlerBlock(); ok {
			v.RetryEligible = true
			v.RetryAgent = agent
		}
	}
	// The control block (LC16): the daemon's eligibility verdicts for
	// stop/resume/cancel, computed by the SAME functions the ops run as a
	// gate (loop.CheckStop|CheckResume|CheckCancel), so the loop.show button
	// and the engine cannot disagree (the CheckHandlerRetry precedent). Always
	// present — it is the daemon's answer, not a guess the UI makes.
	v.Control = &LoopControlView{
		Stop:   controlAction(loop.CheckStop(st)),
		Resume: controlAction(loop.CheckResume(st)),
		Cancel: controlAction(loop.CheckCancel(st)),
		Raise:  controlAction(loop.CheckRaise(st)),
	}
	if st.CapHit != nil {
		v.Control.CapHit = &protocol.LoopCapHitView{
			BudgetID:   st.CapHit.BudgetID,
			Scope:      st.CapHit.Scope,
			Dimension:  st.CapHit.Dimension,
			SpendFloor: int64(st.CapHit.SpendFloor),
			Cap:        int64(st.CapHit.Cap),
		}
	}
	return v
}

// TaskCounterView is one (task, counter) budget row for the view. Level
// rides along from the engine's table — the counter's own property (I2) —
// so no surface has to guess whether a name is task-level or loop-level.
type TaskCounterView struct {
	Task        string `json:"task"`
	Counter     string `json:"counter"`
	Level       string `json:"level"`
	Value       int    `json:"value"`
	Cap         int    `json:"cap"`
	CapExceeded bool   `json:"capExceeded"`
}

// viewLoop enriches the saved workflow with measured step timing. The runtime
// state intentionally stores only position and cost; the step row owns timing.
// resolveLinks reads the goal/task a start or run names (S38 T13's door,
// extracted S39 T9 so loop.run answers the SAME sentences loop.start does —
// two doors, one vocabulary of refusals). A nil pointer means "not named";
// an id that does not resolve is the error to return as is, so both doors
// speak ErrStartLinkUnknown's words verbatim.

func (d *Daemon) viewLoop(st loop.LoopState, includeBlueprint bool) LoopView {
	st = d.resumedAsRunning(st)
	v := viewLoop(st, includeBlueprint)
	if d.store == nil {
		return v
	}
	// S17 T2: the two-level budgets ride the same view, read from the rows
	// that decide the loop — before the timing pass, so a timing read
	// failing never hides the counters (they answer §4's question).
	sc := store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}
	// S20 T2: the provenance pair rides the row read (GetLoop below),
	// beside the counters — started_by answers "who started it"; the
	// attendedness answer rides loop.show's separate field (R-3 keeps the
	// questions apart on the wire too).
	if l, err := d.store.GetLoop(sc); err == nil {
		v.StartedBy = l.StartedBy
		v.Title = l.Title
	}
	// What the person changed since the loop started wins over the pin, as it does when the next step dispatches.
	if o, present, err := d.store.LoopModelOptionGet(context.Background(), st.ID); err == nil && present {
		if o.ReasoningEffort != "" {
			v.ReasoningEffort = o.ReasoningEffort
		}
		if o.Thinking != "" {
			v.Thinking = o.Thinking
		}
	}
	// S39 T9 (CR2/P32/P31): the run history and the declaration ride the
	// same read-fail-silent pass as the counters — a view that cannot read
	// its history shows a loop without one, never a failed loop.
	if runs, err := d.store.ListRuns(st.ID); err == nil {
		for _, r := range runs {
			rv := RunView{
				RunSeq: r.RunSeq, Blueprint: r.Blueprint, Version: r.BlueprintVersion,
				State: r.State, StartedByKind: r.StartedByKind, StartedByID: r.StartedByID,
				GoalID: r.GoalID, GoalVersion: r.GoalVersion, TaskID: r.TaskID,
				StartedAt: r.StartedAt, EndedAt: r.EndedAt,
			}
			if r.KickoffMessageID != nil {
				rv.KickoffMessageID = *r.KickoffMessageID
			}
			if cost, err := d.store.RunCost(st.ID, r.RunSeq); err == nil {
				rv.Cost = cost
			}
			v.Runs = append(v.Runs, rv)
			if r.RunSeq > v.RunSeq {
				v.RunSeq = r.RunSeq
			}
		}
	}
	if attended, err := d.store.LoopAttended(context.Background(), st.ID); err == nil {
		v.Attended = attended
	}
	counters := loop.NewCounters(d.store)
	if lc, err := counters.LoopCounters(sc); err == nil {
		v.LoopCounters = lc
		// S17 T10: the caps ride the view too — the app's counters table
		// renders the ceiling from the ENGINE's table (loop.Table), never
		// from a UI constant that could disagree with the one that stops
		// the loop.
		caps := map[string]int{}
		for _, c := range loop.Table() {
			if c.Level == loop.LevelLoop {
				caps[c.Name] = c.Cap
			}
		}
		if len(caps) > 0 {
			v.LoopCounterCaps = caps
		}
	}
	if tasks, err := d.store.ListTasks(sc); err == nil {
		for _, t := range tasks {
			tc, err := counters.TaskCounters(sc, t.TaskID)
			if err != nil {
				continue
			}
			for name, value := range tc {
				c, ok := loop.Lookup(name)
				if !ok {
					continue
				}
				v.TaskCounters = append(v.TaskCounters, TaskCounterView{
					Task: t.TaskID, Counter: name, Level: string(c.Level),
					Value: value, Cap: c.Cap, CapExceeded: c.Cap > 0 && value >= c.Cap,
				})
			}
		}
		// Deterministic order: task, then the table's counter order — the
		// map above is for reading, never for rendering order (O2 applies
		// to pixels too).
		sort.SliceStable(v.TaskCounters, func(i, j int) bool {
			if v.TaskCounters[i].Task != v.TaskCounters[j].Task {
				return v.TaskCounters[i].Task < v.TaskCounters[j].Task
			}
			return v.TaskCounters[i].Counter < v.TaskCounters[j].Counter
		})
	}
	// S18 T7: the per-route ledger rides the same view the counters ride —
	// built by the daemon from the rows, never recomputed by a client. A
	// read failure leaves it nil, which renders as "no ledger", not as an
	// empty one ($0.00's quieter cousin).
	if costRows, err := d.store.LoopCostRows(sc); err == nil {
		at := model.AttributeCosts(costRows)
		v.CostAttribution = &at
	}

	rows, err := d.store.ListSteps(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot})
	if err != nil {
		return v
	}
	byID := make(map[int64]store.Step, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	for i := range v.Lanes {
		fill := func(steps []LoopStepView) {
			for j := range steps {
				if steps[j].StepID == 0 {
					continue
				}
				row, ok := byID[steps[j].StepID]
				if !ok {
					continue
				}
				// The resolved dispatch name — from the ROW the dispatch
				// created, never from the blueprint text a resolver rewrites
				// (a `by-tech-tag` lane shows golang-engineer, not the tag).
				// "unknown" is the store's own absence marker: absent beats
				// a confident word.
				if row.Agent != "" && row.Agent != "unknown" {
					steps[j].Agent = row.Agent
				}
				steps[j].StartedAt = row.StartedAt
				steps[j].EndedAt = row.EndedAt
				steps[j].WallMS = row.WallMS
				if steps[j].WallMS == nil && row.EndedAt != "" {
					started, startErr := time.Parse(time.RFC3339Nano, row.StartedAt)
					ended, endErr := time.Parse(time.RFC3339Nano, row.EndedAt)
					if startErr == nil && endErr == nil && !ended.Before(started) {
						ms := int(ended.Sub(started).Milliseconds())
						steps[j].WallMS = &ms
					}
				}
			}
		}
		fill(v.Lanes[i].Steps)
		for j := range v.Lanes[i].Items {
			fill(v.Lanes[i].Items[j].Steps)
		}
	}
	return v
}
func stepStatus(saved, lane string) string {
	if saved != "" {
		return saved
	}
	if lane == "not-needed" || lane == "skipped" {
		return lane
	}
	return "pending"
}

// StartLoops is called only after S7's recovery scan has returned.

// resumedAsRunning is what the person sees once an answer has been given. The pinned loop stays `waiting` until
// the resumed step ENDS (the continuation hands it back through CompleteSuspendedStep), so a long step would show
// "answer requested" for its whole run after the question was answered. The saved row is left alone; only the
// view reads it as running, and only when the parked step's own row is running again (a suspended step, a gate and
// a person-stopped step keep their waiting).
func (d *Daemon) resumedAsRunning(st loop.LoopState) loop.LoopState {
	if d.store == nil || st.Status != "waiting" || st.PendingAction != nil || st.LastReason == "stopped" || st.LastReason == "cap-hit" || st.LastStepID == 0 {
		return st
	}
	if step, err := d.store.ReadStep(st.LastStepID); err != nil || step.Status != string(store.StepRunning) {
		return st
	}
	st.Status = "running"
	return st
}

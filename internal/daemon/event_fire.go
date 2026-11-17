package daemon

// event_fire.go — S40 AU9/AU10/AU2: the daemon half of an event start. It
// mirrors StartFromTrigger's STRUCTURE — validate, author attendedness
// locally, call the shared door, then interpret — while leaving S20's door
// untouched (that file contains no knowledge of this one;
// TestStartFromTriggerIsUntouched greps for it).
//
// Two departures from S20's shape, both demanded by AU9:
//
//   - Provenance is NOT stamped afterwards. SetLoopProvenance stays S20's
//     tool for S20's fires; here started_by, trigger_id, automation_depth
//     and the kickoff ride CreatePinnedLoop's ONE transaction, so no step
//     can ever run before its loop's first words exist.
//   - The answer is typed. A *automation.Refusal is a deterministic no the
//     dispatcher records as refused; any other error means "cannot say" and
//     must reach the dispatcher undressed — AU12's fifth outcome exists
//     precisely because a wedged database is not a decision.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/automation"
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// eventStarter adapts the daemon to automation.Starter. It is a value, not
// the daemon itself, so the dispatcher can hold a Starter without holding
// anything else about the process.
type eventStarter struct{ d *Daemon }

// eventStarter hands the dispatcher its start door.
func (d *Daemon) eventStarter() automation.Starter { return eventStarter{d: d} }

// Start runs one authorised event start (AU9). The flow is linear on
// purpose — workspace, subject, kickoff, door — because every branch that
// is not a listed refusal must escape plain, and a linear flow makes
// "unclassified" mean "I did not think of it", never "I dressed it up".
func (e eventStarter) Start(ctx context.Context, r automation.EventStartRequest) (automation.StartResult, error) {
	if err := validateLoopWorkspace(strings.TrimSpace(r.WorkspaceRoot)); err != nil {
		return automation.StartResult{}, &automation.Refusal{Reason: "workspace-invalid", Sentence: err.Error()}
	}
	subj, goalID, taskID, err := e.loadSubject(r)
	if err != nil {
		var ref *automation.Refusal
		if errors.As(err, &ref) {
			return automation.StartResult{}, ref
		}
		return automation.StartResult{}, err
	}
	// The blueprint is loaded ONCE here and refused HERE when it cannot be
	// resolved at all: an event aimed at a blueprint that does not exist is
	// the deterministic no AU12 names refused — the fire row keeps the
	// verbatim sentence and no retry will ever like it better. A blueprint
	// that loads but fails validation reaches the same code through the
	// door's own PinRefusal, below.
	bp, bpErr := e.d.loadStartBlueprint(r.Blueprint)
	if bpErr != nil {
		return automation.StartResult{}, &automation.Refusal{Reason: "blueprint-refused", Sentence: bpErr.Error()}
	}
	text, err := e.renderKickoff(r, subj, bp)
	if err != nil {
		return automation.StartResult{}, err // already a Refusal (kickoff-invalid) or plain — never re-dressed
	}
	attended := false // AU2/G14: authored HERE, never supplied by the request. The only Attended assignment in this file.
	p := protocol.LoopStartParams{
		Blueprint:     r.Blueprint,
		WorkspaceRoot: strings.TrimSpace(r.WorkspaceRoot),
		Slug:          r.Slug,
		Goal:          subj.Title,
		Attended:      &attended,
		GoalID:        goalID,
		TaskID:        taskID,
	}
	var first *store.NewChatMessage
	if text != "" { // S39's rule kept: nothing to say is no message, not an empty one
		first = &store.NewChatMessage{
			Author: loop.KickoffAuthor("automation"),
			Kind:   "note",
			Body:   text,
			// Source is the closed automation label (S39 CR6); SourceRef
			// answers WHICH trigger spoke (T1's column, AU9's need).
			Source:    "automation",
			SourceRef: r.TriggerID,
		}
	}
	// AL8: the start consults the root's grants once, at pin time. A store
	// read error refuses the start (fail-toward-deny, AU12's could-not-say);
	// a read that finds no applicable allow pins nothing (byte-identical to
	// today). The event supplies no labels, so a labelled grant never applies.
	autonomyPin, resolveErr := e.d.resolveAutonomy(ctx, autonomy.OriginEvent, strings.TrimSpace(r.WorkspaceRoot), nil)
	if resolveErr != nil {
		return automation.StartResult{}, resolveErr
	}
	res, err := e.d.startLoopWith(ctx, p, startExtras{StartedBy: "event", TriggerID: r.TriggerID, Depth: r.Depth, FirstMessage: first, Autonomy: autonomyPin})
	var pin loop.PinRefusal
	var perr *protocol.Error
	switch {
	case errors.As(err, &pin):
		return automation.StartResult{}, &automation.Refusal{Reason: "blueprint-refused", Sentence: pin.Error()}
	// ErrRequiresUnmet arrives via WithWhat, which COPIES the sentinel —
	// errors.Is cannot see through a copy, so the recogniser is the code,
	// the same test t14_replay_test.go makes of the socket path. The
	// sentence is the What field, which is where WithWhat wrote the
	// clause's own words; err.Error() is the code and the static message.
	case errors.As(err, &perr) && perr.Code == protocol.ErrRequiresUnmet.Code:
		sentence := err.Error()
		if perr.Data.What != "" {
			sentence = perr.Data.What
		}
		return automation.StartResult{}, &automation.Refusal{Reason: "requires-unmet", Sentence: sentence}
	case err != nil:
		return automation.StartResult{}, err // could-not-say, plain, as AU12 demands
	}
	id, _ := res["id"].(string)
	if id == "" {
		return automation.StartResult{}, fmt.Errorf("event fire %q: the start result carries no loop id", r.TriggerID)
	}
	return automation.StartResult{LoopID: id, Slug: r.Slug}, nil
}

// subject is the rendered kickoff's raw material. Title is what the loop
// says its goal is (the same text a typed start would carry — CR6's parity:
// a templated start must read the same to the model); Body is the longer
// prose the template may reach for.
type subject struct {
	Title      string
	Body       string
	SourceKind string // goal.source_kind — the untrusted rule reads it; "" for a task
}

// loadSubject reads the goal or manual task the event is about. Absent is
// the deterministic subject-missing refusal (the subject was deleted or
// never existed — asking again will not help); any other read failure is
// plain, because a wedged store is not a verdict.
func (e eventStarter) loadSubject(r automation.EventStartRequest) (subject, string, string, error) {
	switch r.SubjectKind {
	case "goal":
		g, err := e.d.store.GetGoal(r.SubjectID)
		// S38's GetGoal speaks absence as the sentence `goal %q does not
		// exist`, not wrapped in ErrNotFound (measured, T4) — both shapes
		// are recognised so a future sentinel change cannot turn a gone
		// subject into a could-not-say.
		if errors.Is(err, store.ErrNotFound) || (err != nil && strings.HasSuffix(err.Error(), "does not exist")) {
			return subject{}, "", "", &automation.Refusal{Reason: "subject-missing",
				Sentence: fmt.Sprintf("goal %q named by the %s event does not exist — the trigger fires at a subject that is gone", r.SubjectID, r.Event)}
		}
		if err != nil {
			return subject{}, "", "", err
		}
		body := g.BodyNote
		if body == "" {
			body = g.Title
		}
		return subject{Title: g.Title, Body: body, SourceKind: g.SourceKind}, g.ID, "", nil
	case "task":
		tk, err := e.d.store.GetTaskByID(r.SubjectID)
		if errors.Is(err, store.ErrNotFound) {
			return subject{}, "", "", &automation.Refusal{Reason: "subject-missing",
				Sentence: fmt.Sprintf("task %q named by the %s event does not exist — the trigger fires at a subject that is gone", r.SubjectID, r.Event)}
		}
		if err != nil {
			return subject{}, "", "", err
		}
		body, err := e.d.store.ReadManualTaskBody(r.SubjectID)
		if err != nil {
			body = "" // no body row is the honest absence (P31), not a failure of the fire
		}
		full := tk.Title
		if body != "" {
			full = tk.Title + "\n\n" + body
		}
		return subject{Title: tk.Title, Body: full}, "", tk.ID, nil
	default:
		return subject{}, "", "", &automation.Refusal{Reason: "subject-missing",
			Sentence: fmt.Sprintf("the %s event names subject %q of kind %q, which is neither goal nor task — no subject means nothing to fire at", r.Event, r.SubjectID, r.SubjectKind)}
	}
}

// renderKickoff resolves the first message's TEXT (AU10's precedence:
// trigger template > blueprint template > subject body). The four Params
// keys are the event's identity for the template author; `untrusted` rides
// S39's rule (a webhook-born goal says so and is never stripped of it —
// brief §5's "needs human review" flag is this line).
func (e eventStarter) renderKickoff(r automation.EventStartRequest, subj subject, bp *blueprint.Blueprint) (string, error) {
	kd := blueprint.KickoffData{
		Goal: subj.Title,
		Task: "",
		Params: map[string]string{
			"event":     r.Event,
			"trigger":   r.TriggerName,
			"depth":     strconv.Itoa(r.Depth),
			"untrusted": strconv.FormatBool(store.SourceUntrusted(subj.SourceKind)),
		},
	}
	if r.SubjectKind == "task" {
		kd.Task = subj.Body
		kd.Goal = "" // a task fires at its own words; the loop's goal IS the task
		if comp, err := e.componentNameForTask(r.SubjectID); err == nil {
			kd.Component = comp
		}
	} else if proj, err := e.projectNameForGoal(r.SubjectID); err == nil {
		kd.Project = proj
	}
	if r.KickoffTemplate != "" {
		text, err := blueprint.RenderKickoff(r.KickoffTemplate, kd)
		if err != nil {
			return "", &automation.Refusal{Reason: "kickoff-invalid", Sentence: err.Error()}
		}
		return text, nil
	}
	text, _, err := loop.KickoffFor(bp, kd, "")
	if err != nil {
		return "", &automation.Refusal{Reason: "kickoff-invalid", Sentence: err.Error()}
	}
	return text, nil
}

// componentNameForTask / projectNameForGoal are best-effort render colour:
// a name the template can speak. Absent is blank, never an error — the
// renderer's contract is that a missing key is a blank, not a refusal.
func (e eventStarter) componentNameForTask(taskID string) (string, error) {
	tk, err := e.d.store.GetTaskByID(taskID)
	if err != nil || tk.ComponentID == "" {
		return "", fmt.Errorf("no component")
	}
	c, err := e.d.store.ReadComponent(tk.ComponentID)
	if err != nil {
		return "", err
	}
	return c.Name, nil
}

func (e eventStarter) projectNameForGoal(goalID string) (string, error) {
	g, err := e.d.store.GetGoal(goalID)
	if err != nil || g.ComponentID == "" {
		return "", fmt.Errorf("no project")
	}
	cw, err := e.d.store.ReadComponent(g.ComponentID)
	if err != nil {
		return "", err
	}
	return cw.Name, nil
}

// StartFromEvent is the daemon's own event fire door, mirroring
// StartFromTrigger's shape for the S40 kind of fire: the dispatcher (T5)
// holds the Starter interface, and any daemon-side caller that wants the
// old shape has this. Thin by intent — one door, no second interpretation.
func (d *Daemon) StartFromEvent(ctx context.Context, r automation.EventStartRequest) (string, error) {
	res, err := d.eventStarter().Start(ctx, r)
	if err != nil {
		return "", err
	}
	return res.LoopID, nil
}

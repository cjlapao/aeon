package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// runs.go is S39 T9: the human doors for starting a run. The daemon decides,
// the table relays — and every DOMAIN refusal is data, never a JSON-RPC
// error: an unmet requirement is a sentence plus candidates the app renders,
// not a transport failure (CR5/D6). The only errors that leave these
// functions are store/plumbing failures and the two bad-params doors.
//
// The order inside startRun is the contract (CR5): preview → links →
// requires, ALL before the claim. `TestUnmetRequiresIsASentenceWithCandidatesAndNoRunRow`
// pins the observable half: a refused start leaves ListRuns untouched and
// the loop resting.

// runInput is one request to start a run, whoever knocks. The socket fills
// ByKind "user"; the agent's door (T10's run_start catalog action) fills
// "agent" with the stamped identity — the model never supplies a loop id or
// an actor.
type runInput struct {
	LoopID, Blueprint, GoalID, TaskID, Message, Route string
	With                                              map[string]string
	ByKind, ByID                                      string // user | agent | trigger
}

// runRefusalSentence renders a refused map into the words an agent must
// speak (S39 T10, D6): the detail, and — for a requires-unmet — the
// candidates as a list the person can choose between. The name is taken
// (records_socket.go renders store refusals); this one renders run doors'
// data. Pure; the sentence it builds is what the tool_result carries, so the
// agent quotes the daemon rather than re-deciding anything.
func runRefusalSentence(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		if s, isStr := v.(string); isStr {
			return s
		}
		return "refused: the run door refused without a sentence"
	}
	detail, _ := m["detail"].(string)
	if detail == "" {
		detail = "the run door refused"
	}
	if kind, _ := m["kind"].(string); kind != "requires-unmet" {
		return detail
	}
	// Two shapes reach this line: the socket's refused map has been through
	// JSON (candidates are []any of maps), and the tool boundary hands the
	// Go types straight over (the store's own []loop.Candidate). A renderer
	// that only knows one shape silently drops the candidates for the other
	// — and the D6 sentence without its list is a dead end, which is exactly
	// what the sentence exists to avoid.
	type candRow struct {
		kind, id, title, state string
	}
	var rows []candRow
	switch cands := m["candidates"].(type) {
	case []any:
		for _, c := range cands {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			kind, _ := cm["kind"].(string)
			id, _ := cm["id"].(string)
			title, _ := cm["title"].(string)
			state, _ := cm["state"].(string)
			rows = append(rows, candRow{kind, id, title, state})
		}
	case []loop.Candidate:
		for _, c := range cands {
			rows = append(rows, candRow{c.Kind, c.ID, c.Title, c.State})
		}
	}
	if len(rows) == 0 {
		return detail
	}
	var b strings.Builder
	b.WriteString(detail)
	b.WriteString(". You could use one of these:")
	for _, r := range rows {
		fmt.Fprintf(&b, "\n- %s %s \"%s\" (%s)", r.kind, r.id, r.title, r.state)
	}
	return b.String()
}

// refused is the one shape a domain refusal takes.
func refused(kind, detail string, extra map[string]any) map[string]any {
	r := map[string]any{"kind": kind, "detail": detail}
	for k, v := range extra {
		r[k] = v
	}
	return map[string]any{"refused": r}
}

// startRun answers a run request. Store failures travel as errors; every
// decision the caller must SEE (not-idle, invalid blueprint, unknown link,
// requires-unmet) is refused data with the daemon's own words.
func (d *Daemon) startRun(ctx context.Context, in runInput) (any, error) {
	if d.loopRunner != nil && d.loopRunner.Draining() {
		// T4 (LC19): the run door is the start family — a run started into a
		// draining daemon would be dispatched into a context the drain
		// cancels at the step boundary. Nothing is written.
		return nil, protocol.ErrLoopControlRefused.WithWhat(
			"run refused: the daemon is shutting down and will not accept new loop work — restart it, then run again")
	}
	pin, err := d.store.ReadPinnedLoop(in.LoopID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.ErrBadParams{Err: fmt.Errorf("no loop %q", in.LoopID)}
		}
		return nil, err
	}
	if pin.Loop.Status != loop.StatusIdle {
		return refused("not-idle", "this loop is "+pin.Loop.Status+"; a run can only start while it is idle", nil), nil
	}
	// The SAME validators a start meets (D4) — before anything writes.
	bp, err := loop.PreviewBlueprint(d.store, in.Blueprint, pin.Loop.Mode)
	if err != nil {
		var ref loop.PinRefusal
		if errors.As(err, &ref) {
			return refused(ref.Kind, ref.Detail, nil), nil
		}
		return nil, err
	}
	goal, task, err := d.resolveLinks(in.GoalID, in.TaskID)
	if err != nil {
		// The refusal must be SPOKEN, not a code: the sentence the start
		// door puts in Data.What is the words — err.Error() would render
		// the numeric identity instead.
		detail := err.Error()
		var pe *protocol.Error
		if errors.As(err, &pe) && pe.Data.What != "" {
			detail = pe.Data.What
		}
		return refused("unknown-link", detail, nil), nil
	}
	goalVersion := 0
	if goal != nil {
		goalVersion = goal.Version
	}
	// CR5: the check reads requirements against rows, never message text
	// (CR4), and NOTHING has been written yet — a refusal here leaves the
	// loop_run table exactly as it found it.
	res := loop.CheckRequires(&bp, goal, task, d.candidatesFor(pin.Loop.ComponentRoot))
	if !res.OK {
		return refused("requires-unmet", res.Sentence, map[string]any{"candidates": res.Candidates}), nil
	}
	// The kickoff data. Component/project names are best effort on purpose:
	// a template naming them renders empty when the registry has no answer,
	// and a name that cannot be read never fails a start that already
	// passed every check.
	data := blueprint.KickoffData{Goal: d.goalPromptText(goal), Params: in.With}
	if task != nil {
		data.Task = task.Title
	}
	if comp, err := d.store.ComponentForRoot(pin.Loop.ComponentRoot); err == nil {
		data.Component = comp.Name
		if comp.ParentID != "" {
			if proj, err := d.store.ReadComponent(comp.ParentID); err == nil {
				data.Project = proj.Name
			}
		}
	}
	started, err := loop.StartRun(ctx, d.store, loop.RunRequest{
		LoopID: in.LoopID, Blueprint: in.Blueprint,
		StartedByKind: in.ByKind, StartedByID: in.ByID,
		GoalID: in.GoalID, GoalVersion: goalVersion, TaskID: in.TaskID,
		With: in.With, Typed: in.Message, Data: data, Route: in.Route,
	})
	if err != nil {
		if errors.Is(err, store.ErrLoopNotIdle) {
			// The race the claim lost to another door — same refusal words.
			return refused("not-idle", "this loop is running; a run can only start while it is idle", nil), nil
		}
		var ref loop.PinRefusal
		if errors.As(err, &ref) {
			return refused(ref.Kind, ref.Detail, nil), nil
		}
		return nil, err
	}
	// S68 B6: a restart is a NEW launch — the previous launch's chosen
	// skills are its history and do not cross into it. The new run's own
	// kickoff row (which chose nothing) is what a later reload reads.
	// S77 carries the rule to attached files: run 1's receipts stay run 1's.
	d.forgetChosen(in.LoopID)
	d.forgetLaunchAttachments(in.LoopID)
	d.publishLoop(protocol.KindLoopRunStarted, map[string]any{
		"loopId":        in.LoopID,
		"runSeq":        started.Run.RunSeq,
		"blueprint":     started.Run.Blueprint,
		"version":       started.Run.BlueprintVersion,
		"startedByKind": in.ByKind,
		"startedById":   in.ByID,
		"goalId":        in.GoalID,
		"goalVersion":   started.Run.GoalVersion,
		"taskId":        in.TaskID,
	})
	if started.Kickoff.ID != 0 {
		// The thread mirror announces itself through T5's one publisher.
		d.publishMessage(in.LoopID, started.Kickoff)
	}
	// The runner is the caller's — the engine made rows, this door is the
	// human's, and a human start means the work should move.
	// T4 (LC19): started on the daemon-lifetime loopCtx, not context.Background()
	// — so a graceful shutdown's Drain can cancel it with ErrDaemonShutdown and
	// the step commit path can read the cause (a shutdown-interrupted step is
	// left running for recovery, never committed failed).
	d.loopRunner.Start(d.loopCtx, in.LoopID)
	return map[string]any{
		"id":        in.LoopID,
		"runSeq":    started.Run.RunSeq,
		"blueprint": started.Run.Blueprint,
		"version":   started.Run.BlueprintVersion,
		"status":    "running",
	}, nil
}

// runOptions answers the dialogue BEFORE the start: every blueprint the
// daemon knows, each with the verdict PreviewBlueprint + CheckRequires give
// (no goal, no task — the person has named none yet), sorted by name so the
// list is stable on screen. There is no per-component blueprint filter in
// the code today — every blueprint that previews cleanly is offered (a
// contradiction worth a name, recorded in Notes).
func (d *Daemon) runOptions(_ context.Context, loopID string) (any, error) {
	pin, err := d.store.ReadPinnedLoop(loopID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.ErrBadParams{Err: fmt.Errorf("no loop %q", loopID)}
		}
		return nil, err
	}
	names, err := d.bpSrc.Names()
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	options := make([]map[string]any, 0, len(names))
	for _, name := range names {
		bp, err := loop.PreviewBlueprint(d.store, name, pin.Loop.Mode)
		if err != nil {
			options = append(options, map[string]any{"blueprint": name, "runnable": false, "reason": err.Error()})
			continue
		}
		res := loop.CheckRequires(&bp, nil, nil, d.candidatesFor(pin.Loop.ComponentRoot))
		row := map[string]any{"blueprint": bp.ID, "version": bp.Version, "runnable": res.OK}
		if !res.OK {
			row["reason"] = res.Sentence
			row["candidates"] = res.Candidates
		}
		options = append(options, row)
	}
	return map[string]any{"options": options}, nil
}

// peerIDOf names the person behind a connection the way the records doors
// already do: the transport's uid, the kernel's claim and not the caller's.
// A connection the transport never recorded answers uid 0 — the daemon's own
// user, the only person a storeless mount can honestly name.
func (d *Daemon) peerIDOf(sess *protocol.Session) string {
	if sess == nil {
		return ""
	}
	uid, _, _ := d.peer(sess.ConnID)
	return strconv.Itoa(uid)
}

// wireRunDoors is T9's wiring, called from initBlueprints beside the other
// human loop doors.
func (d *Daemon) wireRunDoors() {
	d.Table.RunBlueprint = func(ctx context.Context, sess *protocol.Session, p protocol.LoopRunParams) (any, error) {
		return d.startRun(ctx, runInput{
			LoopID: p.ID, Blueprint: p.Blueprint, GoalID: p.GoalID, TaskID: p.TaskID,
			Message: p.Message, Route: p.Route, With: p.With,
			ByKind: "user", ByID: d.peerIDOf(sess),
		})
	}
	d.Table.RunOptions = func(ctx context.Context, p protocol.LoopRunOptionsParams) (any, error) {
		return d.runOptions(ctx, p.ID)
	}
	// The agent's door (S39 T10) is the SAME function as the human's, two
	// lines above — two doors, one code path, so no policy on one side can
	// drift from the other. initBlueprints calls this AFTER the runner
	// exists, which is what startRun needs to make the run move; a storeless
	// daemon has no service to wire and the tool says it is unavailable.
	if d.recordsSvc != nil {
		d.recordsSvc.run = d.startRun
	}
}

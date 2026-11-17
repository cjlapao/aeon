package daemon

// automation_doors.go — S49 T8: the exported OFFLINE faces of the
// automation plane, beside the socket ones. `aeon automation` is a CLIENT
// (P34): its offline road opens the store directly and reaches these
// constructors, so the second door adds NO second implementation — every
// answer it prints comes from the same adapters, the same automationWrites
// service, and control.go's same one scheduler the socket closures run
// (T8's TestDirectDoorUsesTheSameService is the go/parser proof that this
// sentence is true). The blueprint plane arrived at the identical shape
// (NewBlueprintReadDoors); AN1's store locks — requireUser, the
// created_by CHECK — sit under every call here exactly as under the socket.
//
// The offline actor is design §28's person door: {Kind:"user", ID:"cli"}
// — the same shape `aeon proposal commit` already stamps offline
// (policy_client.go). The store's requireUser and the frozen
// created_by_kind CHECK admit `user` ONLY (AU1 lock 3); the CLI's identity
// travels in actor_id='cli', and automation_change.audits it that way.
// (T8.md's "{cli,cli}" shorthand would be refused BY THE LOCKS T4/T5 built
// — the task Notes name the resolution.)
//
// Fire-now is the one verb that REFUSES offline, by name: a fire starts a
// loop, a loop needs the daemon's starter, and a CLI process that opened a
// database cannot honestly run one. The refusal is spoken here, in the
// plane's own file, so no caller can paraphrase it — the autonomy plane's
// offline grant refusal is the precedent (S56).

import (
	"context"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// NewAutomationReadDoors builds the ten unified reads over a store the
// daemon did not open. It is the constructor the daemon's own
// newAutomationReads calls — one body, two callers; the EnvSet/DirExists
// seams are the same os reads (presence only, AN1: no function here can
// return a value, and offline they answer about THIS process's
// environment, which is the only honest reading).
func NewAutomationReadDoors(s *store.Store) protocol.AutomationReads {
	return newAutomationReadsFor(s)
}

// NewAutomationWriteDoors builds trigger.update / automation.archive /
// automation.unarchive over a store the daemon did not open — the same
// automationWrites the socket table answers through.
func NewAutomationWriteDoors(s *store.Store) protocol.AutomationWrite {
	return &automationWrites{s: s, reads: newAutomationReadsFor(s)}
}

// controlAnswerOf is the ONE ControlAnswer → TriggerControlResult mapping
// (S49 T8: extracted from wireAutomationWrites' closure so the socket and
// the offline door render identically; the closure kept its short name
// because the file's rhythm depends on it).
func controlAnswerOf(a trigger.ControlAnswer) protocol.TriggerControlResult {
	return protocol.TriggerControlResult{TriggerID: a.TriggerID, Outcome: a.Outcome, Reason: a.Reason, LoopID: a.LoopID, Paused: a.Paused, Found: a.Found}
}

// addScheduleOf is the ONE TriggerAddParams → trigger.Schedule mapping
// (same extraction; an empty kind still defaults to schedule HERE, at the
// plane's door, never in a caller).
func addScheduleOf(p protocol.TriggerAddParams) trigger.Schedule {
	kind := trigger.Kind(p.Kind)
	if p.Kind == "" {
		kind = trigger.KindSchedule
	}
	return trigger.Schedule{
		ID: p.ID, Kind: kind, Blueprint: p.Blueprint, Expr: p.Expr, Zone: p.Zone,
		WindowStart: p.WindowStart, WindowEnd: p.WindowEnd, WindowDays: p.WindowDays,
		SecretEnv: p.SecretEnv, Workspace: p.Workspace, Goal: p.Goal, Listens: strings.TrimSpace(p.EventFilter),
	}
}

// AutomationControlDoors is the offline face of the S20 control plane:
// add / pause / resume over control.go's ONE Scheduler, built the way T6's
// AutomationDisable builds it — the same automationRows adapter, a nil
// STARTER (nothing offline starts a loop), SystemClock (the next-fires the
// offline List computes are the real next-fires). Validation refusals
// arrive as ANSWERS (Found/Reason), exactly as over the socket.
type AutomationControlDoors struct {
	sc *trigger.Scheduler
	s  *store.Store
}

func NewAutomationControlDoors(s *store.Store) *AutomationControlDoors {
	return &AutomationControlDoors{
		sc: trigger.NewScheduler(automationRows{triggerRows{s}}, nil, trigger.SystemClock{}),
		s:  s,
	}
}

func (c *AutomationControlDoors) Add(ctx context.Context, a protocol.Actor, p protocol.TriggerAddParams) (protocol.TriggerControlResult, error) {
	ans, err := c.sc.Add(ctx, trigger.Actor{Kind: a.Kind, ID: a.ID}, addScheduleOf(p))
	return controlAnswerOf(ans), err
}

func (c *AutomationControlDoors) Pause(ctx context.Context, a protocol.Actor, id string) (protocol.TriggerControlResult, error) {
	ans, err := c.sc.Pause(ctx, trigger.Actor{Kind: a.Kind, ID: a.ID}, id)
	return controlAnswerOf(ans), err
}

func (c *AutomationControlDoors) Resume(ctx context.Context, a protocol.Actor, id string) (protocol.TriggerControlResult, error) {
	ans, err := c.sc.Resume(ctx, trigger.Actor{Kind: a.Kind, ID: a.ID}, id)
	return controlAnswerOf(ans), err
}

// FireNow refuses offline, in this plane's voice, by name — a fire starts
// a loop and the loop has no starter here; the answer writes nothing and
// names the door that can (errType carries the ErrStoreUnavailable code
// the CLI prints verbatim).
func (c *AutomationControlDoors) FireNow(context.Context, protocol.Actor, string) (protocol.TriggerControlResult, error) {
	return protocol.TriggerControlResult{}, protocol.ErrStoreUnavailable.WithWhat(
		"fire-now starts a loop, and a loop belongs to a running daemon — start `aeon serve` and re-run it")
}

// List answers the S20 listing (next-fires computed, same library the
// clock uses) through the ONE triggerListView mapping the socket shares.
func (c *AutomationControlDoors) List(ctx context.Context) (protocol.TriggerListResult, error) {
	views, err := c.sc.List(ctx)
	if err != nil {
		return protocol.TriggerListResult{}, err
	}
	return triggerListView(views), nil
}

// History reads fire and gap rows through the ONE triggerHistoryAnswer
// mapping the socket shares.
func (c *AutomationControlDoors) History(ctx context.Context, id string, limit int) (protocol.TriggerHistoryResult, error) {
	return triggerHistoryAnswer(ctx, c.s, id, limit)
}

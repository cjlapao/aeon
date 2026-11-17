package daemon

// automation_write.go — S49 T4's ONE daemon file that writes automations.
// The T5 guard pins it: the store's trigger writers (SaveTriggerScheduleAs,
// the archive wrappers, CreateEventTrigger/UpdateEventTrigger/
// SetEventTriggerEnabled, RecordAutomationAudit) have exactly one caller
// file in the daemon, and it is this one. Everything the person asks —
// add, update, pause, resume, fire-now, create, edit, enable, disable,
// archive, unarchive — arrives here first.
//
// What moved in from triggers.go (the S40 T8 write seam): the triggerWrites
// service — validation order unchanged (grammar sentence first, blueprint's
// own sentence second, scope, template), each refusal VERBATIM, the store's
// ErrActorNotHuman backstop dressed in the same words as the door's.
//
// What is new (T4):
//
//   - The S20 control doors carry the connection's person into
//     control.go's Scheduler (P34 holds: the SAME functions the CLI's
//     socket calls — the actor is a new argument, never a new
//     implementation), and their rows adapter implements
//     trigger.HumanWriter so a person's save carries its creator stamp and
//     its audit into the write's own transaction. The base Store path stays
//     for the engine (clock.go, byte-frozen) — the fork is a type
//     assertion at the save call, one place.
//   - Fire-now audits AFTER the answer: the fire's row IS the state change
//     (written by the untouched engine path), and the audit names it — the
//     one audit-outside-the-write, its honesty argued in store's file.
//   - trigger.update validates EXACTLY like add — the merged row runs
//     through the same ValidateSchedule, so every refusal add can speak,
//     this door speaks verbatim — and STRICTER: an update may not point a
//     trigger at a blueprint that will not resolve or will not analyze
//     clean (S48's own sentences). Archive/unarchive read before/after
//     views through the READ plane's own finds — the same rows, the same
//     builder, no second view of the world — and carry the health answer
//     the flip of an engine-visible state owes.
//   - Enable refuses an archived event trigger: AN6's invisibility would
//     otherwise let a row be "enabled" while the dispatcher cannot see it
//     — a health answer that lies. Unarchive first; enabling is not an
//     unarchive. (Recorded deviation: the spec left the combination open;
//     it refused it rather than answer two truths.)

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// ── the S40 write seam (moved from triggers.go; T5's one-file pin) ─────────

// ---------------------------------------------------------------------------
// The write seam (T8). Validation ORDER is the contract: grammar sentence
// first, then the blueprint's own sentence (the very code loop start runs —
// extracted by S39 T8, not re-typed here), then scope, then the template.
// Each refusal travels VERBATIM; a re-typed sentence is a second truth.
// The store's ErrActorNotHuman is the backstop: reachable only if the
// handler's actor check regresses, mapped to the same sentence either way.

type triggerWrites struct{ s *store.Store }

// NewTriggerWrites wires the human-only trigger doors to the store.
func NewTriggerWrites(s *store.Store) protocol.EventTriggerWriter {
	return &triggerWrites{s: s}
}

// humanRefusal is the sentence the handler speaks for a non-person; the
// store's backstop is dressed the same so a regression cannot whisper a
// different sentence for the same crime.
func humanRefusal(kind string) error {
	return fmt.Errorf("triggers are written by a person; this actor is %q", kind)
}

// checkScope answers "does this id exist" through S38's own reads, and
// keeps infrastructure errors INFRASTRUCTURE: a disk failure is not a
// missing component. Both ids of the scope are checked — the root (the
// wire's RootID) and the component — naming each missing one, because a
// trigger filed under a ghost root or a ghost component fires on nothing.
// The root check the project tier used to make is not left to the DB
// trigger alone: a refusal that names no id is the one nobody reads.
func (w *triggerWrites) checkScope(rootID, componentID string) error {
	for _, id := range []string{rootID, componentID} {
		if id == "" {
			continue
		}
		if _, err := w.s.ReadComponent(id); err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("no component %q", id)
			}
			return err
		}
	}
	return nil
}

func (w *triggerWrites) CreateEventTrigger(a protocol.Actor, p protocol.EventTriggerCreateParams) (protocol.EventTriggerView, error) {
	m, err := store.ParseMatch(p.Match) // the grammar sentence, verbatim
	if err != nil {
		return protocol.EventTriggerView{}, err
	}
	if err := loop.CheckBlueprint(w.s, p.Blueprint, "standard"); err != nil { // the blueprint's own sentence
		return protocol.EventTriggerView{}, err
	}
	if err := w.checkScope(p.RootID, p.ComponentID); err != nil {
		return protocol.EventTriggerView{}, err
	}
	if p.Kickoff != "" {
		if _, err := blueprint.ParseKickoff(p.Kickoff); err != nil { // S39's parser, the same door start walks
			return protocol.EventTriggerView{}, fmt.Errorf("kickoff template does not parse: %w", err)
		}
	}
	tr, err := w.s.CreateEventTrigger(store.Actor{Kind: a.Kind, ID: a.ID}, store.NewEventTrigger{
		Name: p.Name, RootID: p.RootID, ComponentID: p.ComponentID,
		Match: m, Blueprint: p.Blueprint, Kickoff: p.Kickoff,
		MaxRunsPerHour: p.MaxRunsPerHour, MaxCostPerDayUSD: p.MaxCostPerDayUSD,
	})
	if err != nil {
		if errors.Is(err, store.ErrActorNotHuman) {
			return protocol.EventTriggerView{}, humanRefusal(a.Kind) // backstop, same sentence
		}
		return protocol.EventTriggerView{}, err
	}
	return triggerView(tr, nil), nil
}

func (w *triggerWrites) UpdateEventTrigger(a protocol.Actor, p protocol.EventTriggerUpdateParams) (protocol.EventTriggerView, error) {
	e := store.EventTriggerEdit{
		Name: p.Name, Blueprint: p.Blueprint, Kickoff: p.Kickoff,
		MaxRunsPerHour: p.MaxRunsPerHour, MaxCostPerDayUSD: p.MaxCostPerDayUSD,
		ClearMaxCost: p.ClearMaxCost,
	}
	// Re-validate ONLY what changed — and only the parts grammar/blueprint/
	// template govern; scope ids are not editable, so a change request that
	// names a scope field never arrived (EventTriggerUpdateParams has no
	// such field; the compile refuses it).
	if len(p.Match) > 0 && string(p.Match) != "null" {
		m, err := store.ParseMatch(p.Match)
		if err != nil {
			return protocol.EventTriggerView{}, err
		}
		e.Match = &m
	}
	if p.Blueprint != nil {
		if err := loop.CheckBlueprint(w.s, *p.Blueprint, "standard"); err != nil {
			return protocol.EventTriggerView{}, err
		}
	}
	if p.Kickoff != nil && *p.Kickoff != "" {
		if _, err := blueprint.ParseKickoff(*p.Kickoff); err != nil {
			return protocol.EventTriggerView{}, fmt.Errorf("kickoff template does not parse: %w", err)
		}
	}
	tr, err := w.s.UpdateEventTrigger(store.Actor{Kind: a.Kind, ID: a.ID}, p.ID, e)
	if err != nil {
		if errors.Is(err, store.ErrActorNotHuman) {
			return protocol.EventTriggerView{}, humanRefusal(a.Kind)
		}
		return protocol.EventTriggerView{}, err
	}
	last, err := lastFireOf(w.s, tr.ID)
	if err != nil {
		return protocol.EventTriggerView{}, err
	}
	return triggerView(tr, last), nil
}

func (w *triggerWrites) setEnabled(a protocol.Actor, id string, enabled bool, reason string) (protocol.EventTriggerView, error) {
	// T4: an enable that would light a row the dispatcher cannot see is
	// refused before the store is asked; disable on an archived row is
	// harmless and stays the store's own answer.
	if err := w.archivedGate(id, enabled); err != nil {
		return protocol.EventTriggerView{}, err
	}
	tr, err := w.s.SetEventTriggerEnabled(store.Actor{Kind: a.Kind, ID: a.ID}, id, enabled, reason)
	if err != nil {
		if errors.Is(err, store.ErrActorNotHuman) {
			return protocol.EventTriggerView{}, humanRefusal(a.Kind)
		}
		return protocol.EventTriggerView{}, err
	}
	last, err := lastFireOf(w.s, tr.ID)
	if err != nil {
		return protocol.EventTriggerView{}, err
	}
	return triggerView(tr, last), nil
}

func (w *triggerWrites) EnableEventTrigger(a protocol.Actor, id string) (protocol.EventTriggerView, error) {
	return w.setEnabled(a, id, true, "")
}

func (w *triggerWrites) DisableEventTrigger(a protocol.Actor, id, reason string) (protocol.EventTriggerView, error) {
	return w.setEnabled(a, id, false, reason)
}

// ── T4: the archived-enable gate on the event door ─────────────────────────

// archivedGate reads the row the door is about to flip and refuses
// enable-while-archived in its own sentence. Reads once; the store call
// below reads again — two reads of a row nobody but this daemon writes,
// accepted and named rather than papered over.
func (w *triggerWrites) archivedGate(id string, enabled bool) error {
	if !enabled {
		return nil
	}
	cur, err := w.s.GetEventTrigger(id)
	if err != nil {
		return err
	}
	if cur.ArchivedAt != "" {
		return protocol.ErrAutomationRefused.WithWhat(
			"%s is archived — an archived trigger stays engine-invisible (AN6); automation.unarchive is the door, then enabling is its own act (enabling is not an unarchive)", id)
	}
	return nil
}

// ── the human-shaped rows adapter (trigger.HumanWriter) ────────────────────

// automationRows wraps triggerRows for the Scheduler's Store, adding the
// one OPTIONAL method control.go type-asserts at the save call: a person's
// save (Add, setPaused) carries its actor and audit op into
// store.SaveTriggerScheduleAs — same upsert, plus provenance and audit in
// the write's own transaction. The clock never reaches SaveTriggerSchedule
// at all (T0 measured it), so its path never sees the fork.
type automationRows struct {
	triggerRows
}

func (r automationRows) SaveTriggerScheduleAs(ctx context.Context, s trigger.Schedule, a trigger.Actor, op, summary string) error {
	_, _, err := r.s.SaveTriggerScheduleAs(ctx, store.Actor{Kind: a.Kind, ID: a.ID}, scheduleRow(s), op, summary)
	return err
}

// scheduleRow is the field-for-field carrier triggerRows.SaveTriggerSchedule
// already used; shared by the plain save and the As path.
func scheduleRow(s trigger.Schedule) store.TriggerScheduleRow {
	return store.TriggerScheduleRow{
		ID: s.ID, Kind: string(s.Kind), Blueprint: s.Blueprint,
		Expr: s.Expr, Zone: s.Zone,
		WindowStart: s.WindowStart, WindowEnd: s.WindowEnd, WindowDays: s.WindowDays,
		Paused: s.Paused, SecretEnv: s.SecretEnv, CoveredUntil: s.CoveredUntil,
		CreatedAt: s.CreatedAt, Workspace: s.Workspace, Goal: s.Goal, EventFilter: s.Listens,
	}
}

// ── the daemon's three new doors (protocol.AutomationWrite) ────────────────

type automationWrites struct {
	s     *store.Store
	reads *automationReads
}

// person maps the connection's person onto the store actor shape. The
// protocol door already refused non-persons; the store refuses them again
// before SQL. The mapping is a type carry, never a re-decision.
func personActor(a protocol.Actor) store.Actor { return store.Actor{Kind: a.Kind, ID: a.ID} }

// blueprintGate is update's stricter blueprint door: resolvable through
// loop.CheckBlueprint (loop start's own door — the sentence a fire would
// speak is the sentence add-and-update speak), and Analyze-clean through
// the ONE S48 validator every read answers from (blueprintValidationOptions
// ("")). An invalid line's Msg rides VERBATIM; an unverifiable blueprint is
// not an invalid one — the honest third state stays third.
func (w *automationWrites) blueprintGate(name string) error {
	if err := loop.CheckBlueprint(w.s, name, "standard"); err != nil {
		return err
	}
	row, ok, err := w.s.BlueprintGet(context.Background(), name)
	if err != nil || !ok {
		if err != nil {
			return err
		}
		return fmt.Errorf("blueprint %q does not exist — a trigger may only point at a blueprint that is there to fire", name)
	}
	report := blueprint.Analyze([]byte(row.Content), blueprintValidationOptions(""))
	for _, dg := range report.Diagnostics {
		if dg.State == blueprint.Invalid {
			return errors.New(dg.Msg)
		}
	}
	return nil
}

func (w *automationWrites) TriggerUpdate(ctx context.Context, a protocol.Actor, p protocol.TriggerUpdateParams) (protocol.TriggerUpdateResult, error) {
	v, err := w.reads.findRowByID(ctx, p.ID)
	if err != nil {
		return protocol.TriggerUpdateResult{}, err
	}
	if v.Source == "event" {
		return protocol.TriggerUpdateResult{}, protocol.ErrAutomationRefused.WithWhat(
			"%q is an event trigger — automation.trigger.update is its edit door; trigger.update speaks the clock and the webhook", p.ID)
	}
	r, err := w.s.TriggerSchedule(ctx, p.ID)
	if err != nil {
		return protocol.TriggerUpdateResult{}, err
	}
	before := schedView(r)
	// The sparse merge: absent fields keep the stored value; kind, id,
	// created_at, covered_until, created_by_* and the archive state are
	// not params and no write path names them (the plain save the
	// engine shares stays as S20 wrote it; only the As path touches
	// columns the caller was given).
	merged := r
	if p.Blueprint != nil {
		merged.Blueprint = *p.Blueprint
	}
	if p.Cron != nil {
		merged.Expr = *p.Cron
	}
	if p.Timezone != nil {
		merged.Zone = *p.Timezone
	}
	if p.Workspace != nil {
		merged.Workspace = *p.Workspace
	}
	if p.Goal != nil {
		merged.Goal = *p.Goal
	}
	if p.SecretEnv != nil {
		merged.SecretEnv = *p.SecretEnv
	}
	if p.EventFilter != nil {
		merged.EventFilter = strings.TrimSpace(*p.EventFilter)
	}
	if p.ClearWindow {
		merged.WindowStart, merged.WindowEnd, merged.WindowDays = "", "", ""
	} else {
		if p.WindowStart != nil {
			merged.WindowStart = *p.WindowStart
		}
		if p.WindowEnd != nil {
			merged.WindowEnd = *p.WindowEnd
		}
		if p.WindowDays != nil {
			merged.WindowDays = *p.WindowDays
		}
	}
	sched := trigger.Schedule{
		ID: merged.ID, Kind: trigger.Kind(merged.Kind), Blueprint: merged.Blueprint,
		Workspace: merged.Workspace, Goal: merged.Goal,
		Expr: merged.Expr, Zone: merged.Zone,
		WindowStart: merged.WindowStart, WindowEnd: merged.WindowEnd, WindowDays: merged.WindowDays,
		Paused: merged.Paused, SecretEnv: merged.SecretEnv, CoveredUntil: merged.CoveredUntil,
		CreatedAt: merged.CreatedAt, Listens: merged.EventFilter,
	}
	// add's own door, on the merged row: every refusal add can speak, this
	// door speaks — same validator, same sentences, no second grammar.
	if err := trigger.ValidateSchedule(sched); err != nil {
		return protocol.TriggerUpdateResult{Before: before, After: before, Reason: err.Error()}, nil
	}
	// The stricter half, only when a blueprint is named: update may not
	// point a live trigger at a broken blueprint.
	if p.Blueprint != nil {
		if err := w.blueprintGate(*p.Blueprint); err != nil {
			return protocol.TriggerUpdateResult{Before: before, After: before, Reason: err.Error()}, nil
		}
	}
	_, changed, err := w.s.SaveTriggerScheduleAs(ctx, personActor(a), scheduleRow(sched), "update", "updated "+merged.Kind+" "+merged.ID)
	if err != nil {
		if errors.Is(err, store.ErrActorNotHuman) {
			return protocol.TriggerUpdateResult{}, humanRefusal(a.Kind) // backstop, same sentence
		}
		return protocol.TriggerUpdateResult{}, err
	}
	after := before
	if fresh, err := w.s.TriggerSchedule(ctx, p.ID); err == nil {
		after = schedView(fresh)
	}
	res := protocol.TriggerUpdateResult{Before: before, After: after, Changed: changed}
	if merged.Kind == "schedule" {
		fires, err := w.reads.PreviewAutomations(ctx, protocol.AutomationPreviewParams{
			Spec: &protocol.AutomationPreviewSpec{Expr: merged.Expr, Zone: merged.Zone},
		})
		if err != nil {
			return protocol.TriggerUpdateResult{}, err
		}
		res.NextFires = fires
	}
	return res, nil
}

func (w *automationWrites) Archive(ctx context.Context, a protocol.Actor, p protocol.AutomationArchiveParams) (protocol.AutomationArchiveResult, error) {
	return w.archiveDoor(ctx, a, p, true)
}

func (w *automationWrites) Unarchive(ctx context.Context, a protocol.Actor, p protocol.AutomationArchiveParams) (protocol.AutomationArchiveResult, error) {
	return w.archiveDoor(ctx, a, p, false)
}

// archiveDoor is both doors: the READ plane's own find names the row
// (unknown id ⇒ -31890 with the nearest ids; wrong family under the wrong
// source ⇒ the cross-family sentence), the store wrapper flips the state in
// its own transaction with its audit, and the answer carries before/after
// views plus the health the flip of an engine-visible state owes.
func (w *automationWrites) archiveDoor(ctx context.Context, a protocol.Actor, p protocol.AutomationArchiveParams, archive bool) (protocol.AutomationArchiveResult, error) {
	before, err := w.reads.findRow(ctx, p.Source, p.ID)
	if err != nil {
		return protocol.AutomationArchiveResult{}, err
	}
	var changed bool
	if archive {
		changed, err = w.s.ArchiveAutomation(ctx, personActor(a), p.Source, p.ID, strings.TrimSpace(p.Reason))
	} else {
		changed, err = w.s.UnarchiveAutomation(ctx, personActor(a), p.Source, p.ID)
	}
	if err != nil {
		if errors.Is(err, store.ErrActorNotHuman) {
			return protocol.AutomationArchiveResult{}, humanRefusal(a.Kind)
		}
		if errors.Is(err, store.ErrNotFound) {
			return protocol.AutomationArchiveResult{}, protocol.ErrAutomationUnknown.WithWhat("%s/%q no longer names a row", p.Source, p.ID)
		}
		return protocol.AutomationArchiveResult{}, err
	}
	after, err := w.reads.findRow(ctx, p.Source, p.ID)
	if err != nil {
		return protocol.AutomationArchiveResult{}, err
	}
	health, err := w.reads.healthFor(ctx, []protocol.AutomationView{after})
	if err != nil {
		return protocol.AutomationArchiveResult{}, err
	}
	res := protocol.AutomationArchiveResult{Before: before, After: after, Changed: changed}
	if len(health) > 0 {
		res.Health = health[0]
	}
	return res, nil
}

// ── wiring ──────────────────────────────────────────────────────────────────

// wireAutomationWrites wires the four S20 write seams (actor-carrying),
// the fire-now audit, and the three new doors. The control calls stay
// control.go's (P34) — this file adds WHO asked and the audit beside the
// answer, never a second control plane beside the socket.
func (d *Daemon) wireAutomationWrites() {
	if d.store == nil {
		return // V13: no store, no doors — the seam stays nil, the doors ABSENT
	}
	control := func(run func(context.Context, trigger.Actor, string) (protocol.TriggerControlResult, error)) func(context.Context, protocol.Actor, string) (any, error) {
		return func(ctx context.Context, a protocol.Actor, id string) (any, error) {
			if d.triggers == nil {
				return nil, protocol.ErrStoreUnavailable.WithWhat("the trigger scheduler is not wired in this daemon")
			}
			return run(ctx, trigger.Actor{Kind: a.Kind, ID: a.ID}, id)
		}
	}
	answer := func(a trigger.ControlAnswer) protocol.TriggerControlResult { return controlAnswerOf(a) }
	d.Table.TriggerPause = control(func(ctx context.Context, ta trigger.Actor, id string) (protocol.TriggerControlResult, error) {
		a, err := d.triggers.Pause(ctx, ta, id)
		return answer(a), err
	})
	d.Table.TriggerResume = control(func(ctx context.Context, ta trigger.Actor, id string) (protocol.TriggerControlResult, error) {
		a, err := d.triggers.Resume(ctx, ta, id)
		return answer(a), err
	})
	d.Table.TriggerFireNow = func(ctx context.Context, a protocol.Actor, id string) (any, error) {
		if d.triggers == nil {
			return nil, protocol.ErrStoreUnavailable.WithWhat("the trigger scheduler is not wired in this daemon")
		}
		ans, err := d.triggers.FireNow(ctx, trigger.Actor{Kind: a.Kind, ID: a.ID}, id)
		res := answer(ans)
		// The audit rides only when a fire answer was RECORDED (Found and
		// an outcome — a ghost id wrote nothing). The fire's row is the
		// state change; the audit names its id. An audit failure speaks in
		// the reason: the answer stands (the fire really happened), and a
		// silently missing audit row is the lie AN4 forbids.
		if err == nil && ans.Found && ans.Outcome != "" {
			kind := "schedule"
			if row, kerr := d.store.TriggerSchedule(ctx, id); kerr == nil {
				kind = row.Kind
			}
			fid, ok, ferr := d.store.LatestTriggerFireID(ctx, id)
			if ferr != nil {
				res.Reason += " — the audit row failed: " + ferr.Error()
			} else if !ok {
				res.Reason += " — the audit row failed: no fire row to name"
			} else if aerr := d.store.RecordAutomationAudit(ctx, personActor(a), store.AutomationChange{
				Source: kind, TriggerID: id, Op: "fire_now",
				Summary: fmt.Sprintf("fire_now %s %s fire %d outcome %s", kind, id, fid, ans.Outcome),
			}); aerr != nil {
				res.Reason += " — the audit row failed: " + aerr.Error()
			}
		}
		return res, err
	}
	d.Table.TriggerAdd = func(ctx context.Context, a protocol.Actor, p protocol.TriggerAddParams) (any, error) {
		if d.triggers == nil {
			return nil, protocol.ErrStoreUnavailable.WithWhat("the trigger scheduler is not wired in this daemon")
		}
		// S49 T8: the params→schedule mapping is addScheduleOf, the SAME
		// function the offline door runs — one mapping, two doors.
		a2, err := d.triggers.Add(ctx, trigger.Actor{Kind: a.Kind, ID: a.ID}, addScheduleOf(p))
		return answer(a2), err
	}
	d.Table.AutomationWrite = &automationWrites{s: d.store, reads: d.newAutomationReads()}
}

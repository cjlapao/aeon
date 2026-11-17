package main

import (
	"context"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/automationview"
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// automation_client.go — S49 T8 (AN10, AN12, P34; blueprint_client.go's
// shape, because the house rule is one shape per two-door plane). Every
// `aeon automation` verb is a CLIENT of the socket methods; the offline road
// is the SAME daemon services over a directly opened store —
// daemon.NewAutomationReadDoors / NewAutomationWriteDoors /
// NewAutomationControlDoors / NewTriggerWrites, plus the store-free
// renderers protocol.AnswerAutomationTest and daemon.PreviewDraftSpec. The
// two doors answer the SAME wire shapes, so each verb has ONE presentation
// function and the printed bytes cannot depend on which door answered.
//
// What the CLI never does: re-validate a schedule, re-parse a match, compute
// a next-fire, compose a health verdict, or decide a name exists. It refuses
// only what cannot be a value (an unknown SOURCE, a non-JSON --match, a
// non-numeric -n); every other refusal is the daemon's sentence, printed
// verbatim.
//
// THE ACTOR, and the one place T8.md's shorthand is resolved against the
// locks: the offline stamp is {Kind: "user", ID: "cli"} — design §28's
// person door, the shape `aeon proposal commit` already carries offline
// (policy_client.go). It cannot be {cli,cli}: the store's requireUser and
// the frozen created_by_kind CHECK (migration 20261103102501, AU1 lock 3 —
// immutable, §3) admit `user` ONLY, and automation_audit stamps
// actor_kind='user' unconditionally. The CLI's identity travels in
// actor_id='cli', which is what TestDirectDoorActorIsCliAudited reads back.

// automationDoor is the two doors onto the one automation implementation:
// the ten unified reads, the two S20 operational reads the trigger verbs
// keep speaking, and the eleven person writes.
type automationDoor interface {
	list(ctx context.Context, p protocol.AutomationListParams) (protocol.AutomationListResult, error)
	show(ctx context.Context, p protocol.AutomationShowParams) (protocol.AutomationShowResult, error)
	fires(ctx context.Context, p protocol.AutomationFiresParams) (protocol.AutomationFiresResult, error)
	runs(ctx context.Context, p protocol.AutomationRunsParams) (protocol.AutomationRunsResult, error)
	changes(ctx context.Context, p protocol.AutomationChangesParams) (protocol.AutomationChangesResult, error)
	health(ctx context.Context, p protocol.AutomationHealthParams) (protocol.AutomationHealthResult, error)
	vocabulary(ctx context.Context) (automationview.VocabularyView, error)
	preview(ctx context.Context, p protocol.AutomationPreviewParams) (protocol.AutomationPreviewResult, error)
	test(ctx context.Context, p protocol.AutomationTestParams) (protocol.AutomationTestResult, error)
	dryRun(ctx context.Context, p protocol.AutomationDryRunParams) (protocol.AutomationDryRunResult, error)
	triggerList(ctx context.Context) (protocol.TriggerListResult, error)
	triggerHistory(ctx context.Context, id string, limit int) (protocol.TriggerHistoryResult, error)
	triggerAdd(ctx context.Context, p protocol.TriggerAddParams) (protocol.TriggerControlResult, error)
	triggerUpdate(ctx context.Context, p protocol.TriggerUpdateParams) (protocol.TriggerUpdateResult, error)
	triggerPause(ctx context.Context, id string) (protocol.TriggerControlResult, error)
	triggerResume(ctx context.Context, id string) (protocol.TriggerControlResult, error)
	triggerFireNow(ctx context.Context, id string) (protocol.TriggerControlResult, error)
	eventCreate(ctx context.Context, p protocol.EventTriggerCreateParams) (protocol.EventTriggerView, error)
	eventUpdate(ctx context.Context, p protocol.EventTriggerUpdateParams) (protocol.EventTriggerView, error)
	eventEnable(ctx context.Context, id string) (protocol.EventTriggerView, error)
	eventDisable(ctx context.Context, id, reason string) (protocol.EventTriggerView, error)
	archive(ctx context.Context, p protocol.AutomationArchiveParams) (protocol.AutomationArchiveResult, error)
	unarchive(ctx context.Context, p protocol.AutomationArchiveParams) (protocol.AutomationArchiveResult, error)
}

// automationDial is the dial the selection uses; a var so a test can stand
// in a dial that refuses or answers "no endpoint" without touching the real
// endpoint. Production is the one dial(0) every other verb uses.
var automationDial = dial

// automationDirectNotice is the fixed stderr line the offline door says
// BEFORE any output — a person must never wonder which door answered. It is
// a const, not a template; the test pins the exact bytes (blueprint's
// sister sentence, deliberately the same words).
const automationDirectNotice = "daemon not running — applied directly to the store"

// selectAutomationDoor is the selection (S45 PM4's shape): dial; a success
// is the socket door, a "no daemon" is the direct door (with the fixed line
// on errOut before any output), and any other dial failure is a REFUSAL —
// the CLI never routes around a daemon that exists.
func selectAutomationDoor(errOut io.Writer) (automationDoor, error) {
	c, err := automationDial(0)
	if err == nil {
		return &automationSocketDoor{c: c}, nil
	}
	if isNoDaemon(err) {
		fmt.Fprintln(errOut, automationDirectNotice)
		return &automationDirectDoor{}, nil
	}
	return nil, err
}

// automationVerbatim maps a wire refusal to its sentence VERBATIM — the
// daemon's own words are the refusal; anything else (a transport fault, an
// unknown method) travels as the error itself. (The blueprint plane's
// mapping, restated for this plane's error vocabulary; both unwrap the
// same *protocol.Error.)
func automationVerbatim(err error) error {
	if err == nil {
		return nil
	}
	return blueprintVerbatim(err)
}

// --- the socket door ---------------------------------------------------------------

// automationSocketDoor is the default door: it calls the registered methods
// and relays what the wire said. It validates nothing.
type automationSocketDoor struct{ c *protocol.Client }

func (d *automationSocketDoor) call(method protocol.MethodName, params, out any) error {
	if err := d.c.Call(method, params, out); err != nil {
		return automationVerbatim(err)
	}
	return nil
}

func (d *automationSocketDoor) list(_ context.Context, p protocol.AutomationListParams) (protocol.AutomationListResult, error) {
	var res protocol.AutomationListResult
	err := d.call(protocol.MethodAutomationList, p, &res)
	return res, err
}

func (d *automationSocketDoor) show(_ context.Context, p protocol.AutomationShowParams) (protocol.AutomationShowResult, error) {
	var res protocol.AutomationShowResult
	err := d.call(protocol.MethodAutomationShow, p, &res)
	return res, err
}

func (d *automationSocketDoor) fires(_ context.Context, p protocol.AutomationFiresParams) (protocol.AutomationFiresResult, error) {
	var res protocol.AutomationFiresResult
	err := d.call(protocol.MethodAutomationFires, p, &res)
	return res, err
}

func (d *automationSocketDoor) runs(_ context.Context, p protocol.AutomationRunsParams) (protocol.AutomationRunsResult, error) {
	var res protocol.AutomationRunsResult
	err := d.call(protocol.MethodAutomationRuns, p, &res)
	return res, err
}

func (d *automationSocketDoor) changes(_ context.Context, p protocol.AutomationChangesParams) (protocol.AutomationChangesResult, error) {
	var res protocol.AutomationChangesResult
	err := d.call(protocol.MethodAutomationChanges, p, &res)
	return res, err
}

func (d *automationSocketDoor) health(_ context.Context, p protocol.AutomationHealthParams) (protocol.AutomationHealthResult, error) {
	var res protocol.AutomationHealthResult
	err := d.call(protocol.MethodAutomationHealth, p, &res)
	return res, err
}

func (d *automationSocketDoor) vocabulary(_ context.Context) (automationview.VocabularyView, error) {
	var res automationview.VocabularyView
	err := d.call(protocol.MethodAutomationVocabulary, struct{}{}, &res)
	return res, err
}

func (d *automationSocketDoor) preview(_ context.Context, p protocol.AutomationPreviewParams) (protocol.AutomationPreviewResult, error) {
	var res protocol.AutomationPreviewResult
	err := d.call(protocol.MethodAutomationPreview, p, &res)
	return res, err
}

func (d *automationSocketDoor) test(_ context.Context, p protocol.AutomationTestParams) (protocol.AutomationTestResult, error) {
	var res protocol.AutomationTestResult
	err := d.call(protocol.MethodAutomationTest, p, &res)
	return res, err
}

func (d *automationSocketDoor) dryRun(_ context.Context, p protocol.AutomationDryRunParams) (protocol.AutomationDryRunResult, error) {
	var res protocol.AutomationDryRunResult
	err := d.call(protocol.MethodAutomationDryRun, p, &res)
	return res, err
}

func (d *automationSocketDoor) triggerList(_ context.Context) (protocol.TriggerListResult, error) {
	var res protocol.TriggerListResult
	err := d.call(protocol.MethodTriggerList, protocol.TriggerListParams{}, &res)
	return res, err
}

func (d *automationSocketDoor) triggerHistory(_ context.Context, id string, limit int) (protocol.TriggerHistoryResult, error) {
	var res protocol.TriggerHistoryResult
	err := d.call(protocol.MethodTriggerHistory, protocol.TriggerHistoryParams{ID: id, Limit: limit}, &res)
	return res, err
}

func (d *automationSocketDoor) triggerAdd(_ context.Context, p protocol.TriggerAddParams) (protocol.TriggerControlResult, error) {
	var res protocol.TriggerControlResult
	err := d.call(protocol.MethodTriggerAdd, p, &res)
	return res, err
}

func (d *automationSocketDoor) triggerUpdate(_ context.Context, p protocol.TriggerUpdateParams) (protocol.TriggerUpdateResult, error) {
	var res protocol.TriggerUpdateResult
	err := d.call(protocol.MethodTriggerUpdate, p, &res)
	return res, err
}

func (d *automationSocketDoor) triggerPause(_ context.Context, id string) (protocol.TriggerControlResult, error) {
	var res protocol.TriggerControlResult
	err := d.call(protocol.MethodTriggerPause, protocol.TriggerControlParams{ID: id}, &res)
	return res, err
}

func (d *automationSocketDoor) triggerResume(_ context.Context, id string) (protocol.TriggerControlResult, error) {
	var res protocol.TriggerControlResult
	err := d.call(protocol.MethodTriggerResume, protocol.TriggerControlParams{ID: id}, &res)
	return res, err
}

func (d *automationSocketDoor) triggerFireNow(_ context.Context, id string) (protocol.TriggerControlResult, error) {
	var res protocol.TriggerControlResult
	err := d.call(protocol.MethodTriggerFireNow, protocol.TriggerControlParams{ID: id}, &res)
	return res, err
}

func (d *automationSocketDoor) eventCreate(_ context.Context, p protocol.EventTriggerCreateParams) (protocol.EventTriggerView, error) {
	var res protocol.EventTriggerView
	err := d.call(protocol.MethodAutomationTriggerCreate, p, &res)
	return res, err
}

func (d *automationSocketDoor) eventUpdate(_ context.Context, p protocol.EventTriggerUpdateParams) (protocol.EventTriggerView, error) {
	var res protocol.EventTriggerView
	err := d.call(protocol.MethodAutomationTriggerUpdate, p, &res)
	return res, err
}

func (d *automationSocketDoor) eventEnable(_ context.Context, id string) (protocol.EventTriggerView, error) {
	var res protocol.EventTriggerView
	err := d.call(protocol.MethodAutomationTriggerEnable, protocol.EventTriggerEnableParams{ID: id}, &res)
	return res, err
}

func (d *automationSocketDoor) eventDisable(_ context.Context, id, reason string) (protocol.EventTriggerView, error) {
	var res protocol.EventTriggerView
	err := d.call(protocol.MethodAutomationTriggerDisable, protocol.EventTriggerDisableParams{ID: id, Reason: reason}, &res)
	return res, err
}

func (d *automationSocketDoor) archive(_ context.Context, p protocol.AutomationArchiveParams) (protocol.AutomationArchiveResult, error) {
	var res protocol.AutomationArchiveResult
	err := d.call(protocol.MethodAutomationArchive, p, &res)
	return res, err
}

func (d *automationSocketDoor) unarchive(_ context.Context, p protocol.AutomationArchiveParams) (protocol.AutomationArchiveResult, error) {
	var res protocol.AutomationArchiveResult
	err := d.call(protocol.MethodAutomationUnarchive, p, &res)
	return res, err
}

// --- the direct door ----------------------------------------------------------------

// automationDirectDoor is the offline fallback: the SAME daemon services the
// socket answers through, over a store opened directly (the justified second
// access path — WAL serialises it behind a daemon's writer, and there is no
// daemon when this door answers). It holds no validation, no matching, no
// health composition and no copy of a write path; the guard
// TestAutomationCLINeverImportsStoreOutsideDirectDoor reads the automation
// CLI files for the single legitimate store import, and
// TestDirectDoorUsesTheSameService reads this file for the constructor calls
// rather than a local implementation.
type automationDirectDoor struct{}

// open opens the store and hands out its close. The system seed the daemon
// applies at arrival runs here first (the blueprint door's precedent): a
// store that never met a daemon then answers for a shipped blueprint name
// instead of refusing it, and trigger.add's blueprint validation sees the
// same rows either way.
func (automationDirectDoor) open() (*store.Store, func(), error) {
	s, err := openStoreDirectly(false)
	if err != nil {
		return nil, nil, err
	}
	if err := daemon.SeedStandardBlueprints(s); err != nil {
		_ = s.Close()
		return nil, nil, fmt.Errorf("seed the shipped blueprints: %w", err)
	}
	return s, func() { _ = s.Close() }, nil
}

// automationCLIActor is design §28's offline person door — see the file
// header for why `user` is the kind the locks admit and 'cli' the id that
// carries the CLI's identity. It is a const-shaped var: an actor is never a
// params field, and no verb can restamp it.
var automationCLIActor = protocol.Actor{Kind: "user", ID: "cli"}

// typedAnswer narrows an Answer* rendering (which returns `any` because the
// socket handler returns `any`) to the concrete result the printers read.
// A failure to match is a bug in THIS file, not a wire condition — it
// panics loudly rather than printing a zero row.
func typedAnswer[T any](v any, err error) (T, error) {
	if err != nil {
		var zero T
		return zero, automationVerbatim(err)
	}
	return v.(T), nil
}

func (d automationDirectDoor) list(ctx context.Context, p protocol.AutomationListParams) (protocol.AutomationListResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.AutomationListResult{}, err
	}
	defer closeFn()
	res, err := typedAnswer[protocol.AutomationListResult](protocol.AnswerAutomationList(ctx, daemon.NewAutomationReadDoors(s), p))
	return res, err
}

func (d automationDirectDoor) show(ctx context.Context, p protocol.AutomationShowParams) (protocol.AutomationShowResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.AutomationShowResult{}, err
	}
	defer closeFn()
	res, err := typedAnswer[protocol.AutomationShowResult](protocol.AnswerAutomationShow(ctx, daemon.NewAutomationReadDoors(s), p))
	return res, err
}

func (d automationDirectDoor) fires(ctx context.Context, p protocol.AutomationFiresParams) (protocol.AutomationFiresResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.AutomationFiresResult{}, err
	}
	defer closeFn()
	res, err := typedAnswer[protocol.AutomationFiresResult](protocol.AnswerAutomationFires(ctx, daemon.NewAutomationReadDoors(s), p))
	return res, err
}

func (d automationDirectDoor) runs(ctx context.Context, p protocol.AutomationRunsParams) (protocol.AutomationRunsResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.AutomationRunsResult{}, err
	}
	defer closeFn()
	res, err := typedAnswer[protocol.AutomationRunsResult](protocol.AnswerAutomationRuns(ctx, daemon.NewAutomationReadDoors(s), p))
	return res, err
}

func (d automationDirectDoor) changes(ctx context.Context, p protocol.AutomationChangesParams) (protocol.AutomationChangesResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.AutomationChangesResult{}, err
	}
	defer closeFn()
	res, err := typedAnswer[protocol.AutomationChangesResult](protocol.AnswerAutomationChanges(ctx, daemon.NewAutomationReadDoors(s), p))
	return res, err
}

func (d automationDirectDoor) health(ctx context.Context, p protocol.AutomationHealthParams) (protocol.AutomationHealthResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.AutomationHealthResult{}, err
	}
	defer closeFn()
	res, err := typedAnswer[protocol.AutomationHealthResult](protocol.AnswerAutomationHealth(ctx, daemon.NewAutomationReadDoors(s), p))
	return res, err
}

func (automationDirectDoor) vocabulary(context.Context) (automationview.VocabularyView, error) {
	// The vocabulary is code, not rows (the socket handler returns the very
	// same call): no store opens for it.
	return automationview.Vocabulary(), nil
}

func (d automationDirectDoor) preview(ctx context.Context, p protocol.AutomationPreviewParams) (protocol.AutomationPreviewResult, error) {
	if p.Spec != nil {
		// `preview --cron` walks the daemon's own store-free draft road —
		// same walk, same refusal, no store opened.
		res, err := typedAnswer[protocol.AutomationPreviewResult](protocol.AnswerAutomationDraftPreview(p, daemon.PreviewDraftSpec))
		return res, err
	}
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.AutomationPreviewResult{}, err
	}
	defer closeFn()
	res, err := typedAnswer[protocol.AutomationPreviewResult](protocol.AnswerAutomationPreview(ctx, daemon.NewAutomationReadDoors(s), p))
	return res, err
}

func (automationDirectDoor) test(_ context.Context, p protocol.AutomationTestParams) (protocol.AutomationTestResult, error) {
	// The matcher is code the daemon and this door both reach through
	// protocol.AnswerAutomationTest — no store, no daemon, same verdict.
	res, err := typedAnswer[protocol.AutomationTestResult](protocol.AnswerAutomationTest(p))
	return res, err
}

func (d automationDirectDoor) dryRun(ctx context.Context, p protocol.AutomationDryRunParams) (protocol.AutomationDryRunResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.AutomationDryRunResult{}, err
	}
	defer closeFn()
	res, err := typedAnswer[protocol.AutomationDryRunResult](protocol.AnswerAutomationDryRun(ctx, daemon.NewAutomationReadDoors(s), p))
	return res, err
}

func (d automationDirectDoor) triggerList(ctx context.Context) (protocol.TriggerListResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.TriggerListResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewAutomationControlDoors(s).List(ctx)
	return res, automationVerbatim(err)
}

func (d automationDirectDoor) triggerHistory(ctx context.Context, id string, limit int) (protocol.TriggerHistoryResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.TriggerHistoryResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewAutomationControlDoors(s).History(ctx, id, limit)
	return res, automationVerbatim(err)
}

func (d automationDirectDoor) triggerAdd(ctx context.Context, p protocol.TriggerAddParams) (protocol.TriggerControlResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.TriggerControlResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewAutomationControlDoors(s).Add(ctx, automationCLIActor, p)
	return res, automationVerbatim(err)
}

func (d automationDirectDoor) triggerUpdate(ctx context.Context, p protocol.TriggerUpdateParams) (protocol.TriggerUpdateResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.TriggerUpdateResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewAutomationWriteDoors(s).TriggerUpdate(ctx, automationCLIActor, p)
	return res, automationVerbatim(err)
}

func (d automationDirectDoor) triggerPause(ctx context.Context, id string) (protocol.TriggerControlResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.TriggerControlResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewAutomationControlDoors(s).Pause(ctx, automationCLIActor, id)
	return res, automationVerbatim(err)
}

func (d automationDirectDoor) triggerResume(ctx context.Context, id string) (protocol.TriggerControlResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.TriggerControlResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewAutomationControlDoors(s).Resume(ctx, automationCLIActor, id)
	return res, automationVerbatim(err)
}

// triggerFireNow refuses offline — in the plane's own voice, by name (see
// daemon.AutomationControlDoors.FireNow): a fire starts a loop, a loop needs
// the daemon's starter, and a CLI process that opened a database cannot
// honestly run one. The refusal travels verbatim; nothing is written.
func (d automationDirectDoor) triggerFireNow(ctx context.Context, id string) (protocol.TriggerControlResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.TriggerControlResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewAutomationControlDoors(s).FireNow(ctx, automationCLIActor, id)
	return res, automationVerbatim(err)
}

func (d automationDirectDoor) eventCreate(ctx context.Context, p protocol.EventTriggerCreateParams) (protocol.EventTriggerView, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.EventTriggerView{}, err
	}
	defer closeFn()
	res, err := daemon.NewTriggerWrites(s).CreateEventTrigger(automationCLIActor, p)
	_ = ctx
	return res, automationVerbatim(err)
}

func (d automationDirectDoor) eventUpdate(ctx context.Context, p protocol.EventTriggerUpdateParams) (protocol.EventTriggerView, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.EventTriggerView{}, err
	}
	defer closeFn()
	res, err := daemon.NewTriggerWrites(s).UpdateEventTrigger(automationCLIActor, p)
	_ = ctx
	return res, automationVerbatim(err)
}

func (d automationDirectDoor) eventEnable(ctx context.Context, id string) (protocol.EventTriggerView, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.EventTriggerView{}, err
	}
	defer closeFn()
	res, err := daemon.NewTriggerWrites(s).EnableEventTrigger(automationCLIActor, id)
	_ = ctx
	return res, automationVerbatim(err)
}

func (d automationDirectDoor) eventDisable(ctx context.Context, id, reason string) (protocol.EventTriggerView, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.EventTriggerView{}, err
	}
	defer closeFn()
	res, err := daemon.NewTriggerWrites(s).DisableEventTrigger(automationCLIActor, id, reason)
	_ = ctx
	return res, automationVerbatim(err)
}

func (d automationDirectDoor) archive(ctx context.Context, p protocol.AutomationArchiveParams) (protocol.AutomationArchiveResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.AutomationArchiveResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewAutomationWriteDoors(s).Archive(ctx, automationCLIActor, p)
	return res, automationVerbatim(err)
}

func (d automationDirectDoor) unarchive(ctx context.Context, p protocol.AutomationArchiveParams) (protocol.AutomationArchiveResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.AutomationArchiveResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewAutomationWriteDoors(s).Unarchive(ctx, automationCLIActor, p)
	return res, automationVerbatim(err)
}

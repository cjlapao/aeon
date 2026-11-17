package main

// gate_client.go — S57 T10 (VG20): the gate plane's 22 verbs behind one
// door pair.
//
// Socket-first (P34): with the daemon home every verb is a client of the
// socket method and the answer is the daemon's own bytes. "Nobody is
// home" (`nobodyIsHome`, the ONE dial fault the S45 T8 rule allows a
// fallback on — backup.go holds its argument) is the DIRECT door: the
// same three daemon services over a directly opened store, the actor
// {cli,cli}, and the fixed stderr line saying which door answered. Every
// other dial fault is refused as-is, never a fallback.
//
// The direct door is a ROUTER, not an implementation: it opens the
// store and hands each call to `daemon.NewVerifyService` /
// `NewMergeRuleService` / `NewDoDService` — the very objects the daemon's
// own doors call. A second validation, a re-formatting, a softened
// refusal is a third implementation, and `TestDirectDoorHasNoLogicCopy`
// (go/parser) plus `TestCLIRefusalsVerbatimBothDoors` (byte comparison)
// forbid it. `internal/store` is imported by this file and nowhere else
// on the gate plane (`TestCLIImportsStoreOnlyInTheDirectDoor`).
//
// `verify.try` has NO direct door: it spawns a process under the
// daemon's policy gate, and a CLI that spawns when the daemon is down is
// a gate with no gatekeeper. Offline it refuses with the fixed sentence.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// gateDirectLine is the direct door's fixed stderr line (pinned by
// test). The same sentence as the backup/autonomy/mcp doors — each
// family owns its constant; the words are the shared contract.
const gateDirectLine = "daemon not running — applied directly to the store"

// stderrW is the package's stderr seam: the fixed lines (the direct-door
// notice, the scrubbed-environment note, the tails warning) all land on
// it, and the gate tests swap it to pin those bytes.
var stderrW io.Writer = os.Stderr

// gateTryOfflineRefusal is the one refusal the direct door answers with,
// always (pinned by test): `try` is a human-door spawn under the
// daemon's policy gate — there is no direct door for it.
const gateTryOfflineRefusal = "verify try needs the daemon: it runs under the daemon's policy gate — start it with aeon serve"

// gateDoor is the 22 verbs behind the door the select picks: the
// socket's when the daemon answers, the direct one when nobody is home.
// Refusals ride back as *protocol.Error whose text the printers never
// see (renderError, three parts, both doors byte-identical). The method
// groups are the four families; the verbs file of each family takes the
// matching group.
type gateDoor interface {
	// verify family
	VerifyList(ctx context.Context, q protocol.VerifyListQuery) (protocol.VerifyListResult, error)
	VerifyShow(ctx context.Context, id string) (protocol.VerifyShowResult, error)
	VerifyRuns(ctx context.Context, q protocol.VerifyRunsQuery) (protocol.VerifyRunsResult, error)
	VerifyRunShow(ctx context.Context, id string) (protocol.VerifyRunShowResult, error)
	VerifyVocabulary(ctx context.Context) (protocol.VerifyVocabularyResult, error)
	VerifyTemplates(ctx context.Context) (protocol.VerifyTemplatesResult, error)
	VerifyAdd(ctx context.Context, in protocol.VerifyAddInput) (protocol.VerifyWriteResult, error)
	VerifyUpdate(ctx context.Context, in protocol.VerifyUpdateInput) (protocol.VerifyWriteResult, error)
	VerifyArchive(ctx context.Context, id string) (protocol.VerifyWriteResult, error)
	VerifyRestore(ctx context.Context, id string) (protocol.VerifyWriteResult, error)
	VerifyApplyTemplate(ctx context.Context, componentID string, templateIDs []string) (protocol.VerifyApplyResult, error)
	VerifyTry(ctx context.Context, componentID, gateID string) (protocol.VerifyTryResult, error)
	// merge rule family
	MergeRuleGet(ctx context.Context, componentID string, effective bool) (protocol.MergeRuleGetResult, error)
	MergeRulePreview(ctx context.Context, componentID string, pr int) (protocol.MergeRulePreviewResult, error)
	MergeRuleChanges(ctx context.Context, componentID string, limit int) (protocol.MergeRuleChangesResult, error)
	MergeRuleVocabulary(ctx context.Context) (protocol.MergeRuleVocabularyResult, error)
	MergeRuleSet(ctx context.Context, in protocol.MergeRuleSetInput) (protocol.MergeRuleSetResult, error)
	MergeRuleClear(ctx context.Context, componentID string) (protocol.MergeRuleSetResult, error)
	// merge ledger family
	MergeEvents(ctx context.Context, f protocol.MergeEventsQuery) (protocol.MergeEventsResult, error)
	MergeWatches(ctx context.Context, f protocol.MergeWatchesQuery) (protocol.MergeWatchesResult, error)
	// definition of done
	DoDList(ctx context.Context, loopSlug string) (protocol.DoDListResult, error)
	DoDMark(ctx context.Context, in protocol.DoDMarkInput) (protocol.DoDWriteResult, error)

	Close() error
}

// selectDoor picks the door per verb (the backup rule): a daemon that
// stops between verbs is seen by the next one. "Nobody is home" opens
// the direct door and speaks the fixed line; any other dial fault is
// refused as-is. The caller closes the door.
// gateDial and newGateDirectDoor are the two seams the parity tests
// stand in (the seed door's precedent, seed_test.go:204): the fall-back
// needs neither a real endpoint nor a real store to be proven.
var (
	gateDial = dial
	// The seam type is the door interface (tests stand in a fake); the
	// wrapper only returns a door when the real one was built — never
	// a typed-nil *gateDirectDoor wearing the interface.
	newGateDirectDoor func() (gateDoor, error) = func() (gateDoor, error) {
		d, err := openGateDirectDoor()
		if err != nil {
			return nil, err
		}
		return d, nil
	}
)

func selectGateDoor() (gateDoor, error) {
	c, err := gateDial(0)
	if err == nil {
		return &gateSocketDoor{c: c}, nil
	}
	if !nobodyIsHome(err) {
		return nil, err
	}
	d, derr := newGateDirectDoor()
	if derr != nil {
		return nil, derr
	}
	fmt.Fprintln(stderrW, gateDirectLine)
	return d, nil
}

// The two selection seams. Production is the functions beside them;
// tests hand out a fake door without driving dial/fallback around a
// nil client. gateSelectTry is its own seam because `verify try` has
// NO direct door — the socket or the fixed refusal, nothing between.
var (
	gateSelect    = selectGateDoor
	gateSelectTry = selectGateTryDoor
)

// selectGateTryDoor is try's honest dial: the socket door, the real
// error of a daemon that exists, or the fixed offline refusal — never
// a store-opening fallback (there is no policy gate offline to run
// under, so there is no direct door to open).
func selectGateTryDoor() (gateDoor, error) {
	c, err := gateDial(0)
	if err != nil {
		if !nobodyIsHome(err) {
			// A daemon that exists and faults is not "no daemon".
			return nil, err
		}
		return nil, errors.New(gateTryOfflineRefusal)
	}
	return &gateSocketDoor{c: c}, nil
}

// --- the socket door ----------------------------------------------------

// gateSocketDoor is the 22 verbs over the daemon's seam: the same wire
// results the app renders, the same refusal bytes.
type gateSocketDoor struct {
	c *protocol.Client
}

func (d *gateSocketDoor) Close() error { return d.c.Close() }

func (d *gateSocketDoor) VerifyList(ctx context.Context, q protocol.VerifyListQuery) (protocol.VerifyListResult, error) {
	var res protocol.VerifyListResult
	err := d.c.Call(protocol.MethodVerifyList, protocol.VerifyListParams{
		ComponentID: q.ComponentID, Effective: &q.Effective,
		IncludeArchived: q.IncludeArchived, Requirement: q.Requirement,
	}, &res)
	return res, err
}

func (d *gateSocketDoor) VerifyShow(ctx context.Context, id string) (protocol.VerifyShowResult, error) {
	var res protocol.VerifyShowResult
	err := d.c.Call(protocol.MethodVerifyShow, protocol.VerifyShowParams{ID: id}, &res)
	return res, err
}

func (d *gateSocketDoor) VerifyRuns(ctx context.Context, q protocol.VerifyRunsQuery) (protocol.VerifyRunsResult, error) {
	var res protocol.VerifyRunsResult
	err := d.c.Call(protocol.MethodVerifyRuns, protocol.VerifyRunsParams{
		ComponentID: q.ComponentID, Loop: q.LoopSlug, State: q.State, Limit: q.Limit,
	}, &res)
	return res, err
}

func (d *gateSocketDoor) VerifyRunShow(ctx context.Context, id string) (protocol.VerifyRunShowResult, error) {
	var res protocol.VerifyRunShowResult
	err := d.c.Call(protocol.MethodVerifyRunShow, protocol.VerifyRunShowParams{ID: id}, &res)
	return res, err
}

func (d *gateSocketDoor) VerifyVocabulary(ctx context.Context) (protocol.VerifyVocabularyResult, error) {
	var res protocol.VerifyVocabularyResult
	err := d.c.Call(protocol.MethodVerifyVocabulary, struct{}{}, &res)
	return res, err
}

func (d *gateSocketDoor) VerifyTemplates(ctx context.Context) (protocol.VerifyTemplatesResult, error) {
	var res protocol.VerifyTemplatesResult
	err := d.c.Call(protocol.MethodVerifyTemplates, struct{}{}, &res)
	return res, err
}

func (d *gateSocketDoor) VerifyAdd(ctx context.Context, in protocol.VerifyAddInput) (protocol.VerifyWriteResult, error) {
	var res protocol.VerifyWriteResult
	err := d.c.Call(protocol.MethodVerifyAdd, protocol.VerifyAddParams{
		ComponentID: in.ComponentID, Name: in.Name, Command: in.Command,
		Requirement: in.Requirement, TimeoutS: in.TimeoutS,
		MemoryMB: in.MemoryMB, Position: in.Position, Enabled: in.Enabled,
	}, &res)
	return res, err
}

func (d *gateSocketDoor) VerifyUpdate(ctx context.Context, in protocol.VerifyUpdateInput) (protocol.VerifyWriteResult, error) {
	var res protocol.VerifyWriteResult
	err := d.c.Call(protocol.MethodVerifyUpdate, protocol.VerifyUpdateParams{
		ID: in.ID, Command: in.Command, Requirement: in.Requirement,
		TimeoutS: in.TimeoutS, MemoryMB: in.MemoryMB, Position: in.Position, Enabled: in.Enabled,
	}, &res)
	return res, err
}

func (d *gateSocketDoor) VerifyArchive(ctx context.Context, id string) (protocol.VerifyWriteResult, error) {
	var res protocol.VerifyWriteResult
	err := d.c.Call(protocol.MethodVerifyArchive, protocol.VerifyArchiveParams{ID: id}, &res)
	return res, err
}

func (d *gateSocketDoor) VerifyRestore(ctx context.Context, id string) (protocol.VerifyWriteResult, error) {
	var res protocol.VerifyWriteResult
	err := d.c.Call(protocol.MethodVerifyRestore, protocol.VerifyRestoreParams{ID: id}, &res)
	return res, err
}

func (d *gateSocketDoor) VerifyApplyTemplate(ctx context.Context, componentID string, templateIDs []string) (protocol.VerifyApplyResult, error) {
	var res protocol.VerifyApplyResult
	err := d.c.Call(protocol.MethodVerifyApplyTemplate, protocol.VerifyApplyTemplateParams{
		ComponentID: componentID, TemplateIDs: templateIDs,
	}, &res)
	return res, err
}

func (d *gateSocketDoor) VerifyTry(ctx context.Context, componentID, gateID string) (protocol.VerifyTryResult, error) {
	var res protocol.VerifyTryResult
	err := d.c.Call(protocol.MethodVerifyTry, protocol.VerifyTryParams{ComponentID: componentID, ID: gateID}, &res)
	return res, err
}

func (d *gateSocketDoor) MergeRuleGet(ctx context.Context, componentID string, effective bool) (protocol.MergeRuleGetResult, error) {
	var res protocol.MergeRuleGetResult
	err := d.c.Call(protocol.MethodMergeRuleGet, protocol.MergeRuleGetParams{
		ComponentID: componentID, Effective: &effective,
	}, &res)
	return res, err
}

func (d *gateSocketDoor) MergeRulePreview(ctx context.Context, componentID string, pr int) (protocol.MergeRulePreviewResult, error) {
	var res protocol.MergeRulePreviewResult
	err := d.c.Call(protocol.MethodMergeRulePreview, protocol.MergeRulePreviewParams{
		ComponentID: componentID, Pr: pr,
	}, &res)
	return res, err
}

func (d *gateSocketDoor) MergeRuleChanges(ctx context.Context, componentID string, limit int) (protocol.MergeRuleChangesResult, error) {
	var res protocol.MergeRuleChangesResult
	err := d.c.Call(protocol.MethodMergeRuleChanges, protocol.MergeRuleChangesParams{
		ComponentID: componentID, Limit: limit,
	}, &res)
	return res, err
}

func (d *gateSocketDoor) MergeRuleVocabulary(ctx context.Context) (protocol.MergeRuleVocabularyResult, error) {
	var res protocol.MergeRuleVocabularyResult
	err := d.c.Call(protocol.MethodMergeRuleVocabulary, struct{}{}, &res)
	return res, err
}

func (d *gateSocketDoor) MergeRuleSet(ctx context.Context, in protocol.MergeRuleSetInput) (protocol.MergeRuleSetResult, error) {
	// The wire rule is the input's JSON (the daemon's handler decodes
	// RawMessage into this same shape — the CLI re-validates nothing, so
	// the bytes it sends are a marshal, not a judgement).
	raw, err := json.Marshal(in)
	if err != nil {
		return protocol.MergeRuleSetResult{}, err
	}
	var res protocol.MergeRuleSetResult
	err = d.c.Call(protocol.MethodMergeRuleSet, protocol.MergeRuleSetParams{
		ComponentID: in.ComponentID, Rule: raw,
	}, &res)
	return res, err
}

func (d *gateSocketDoor) MergeRuleClear(ctx context.Context, componentID string) (protocol.MergeRuleSetResult, error) {
	var res protocol.MergeRuleSetResult
	err := d.c.Call(protocol.MethodMergeRuleClear, protocol.MergeRuleClearParams{ComponentID: componentID}, &res)
	return res, err
}

func (d *gateSocketDoor) MergeEvents(ctx context.Context, f protocol.MergeEventsQuery) (protocol.MergeEventsResult, error) {
	var res protocol.MergeEventsResult
	var comp *string
	if f.ComponentID != "" {
		comp = &f.ComponentID
	}
	err := d.c.Call(protocol.MethodMergeEvents, protocol.MergeEventsParams{
		ComponentID: comp, Loop: f.LoopSlug, Limit: f.Limit,
	}, &res)
	return res, err
}

func (d *gateSocketDoor) MergeWatches(ctx context.Context, f protocol.MergeWatchesQuery) (protocol.MergeWatchesResult, error) {
	var res protocol.MergeWatchesResult
	err := d.c.Call(protocol.MethodMergeWatches, protocol.MergeWatchesParams{State: f.State}, &res)
	return res, err
}

func (d *gateSocketDoor) DoDList(ctx context.Context, loopSlug string) (protocol.DoDListResult, error) {
	var res protocol.DoDListResult
	err := d.c.Call(protocol.MethodDoDList, protocol.DoDListParams{Loop: loopSlug}, &res)
	return res, err
}

func (d *gateSocketDoor) DoDMark(ctx context.Context, in protocol.DoDMarkInput) (protocol.DoDWriteResult, error) {
	var res protocol.DoDWriteResult
	err := d.c.Call(protocol.MethodDoDMark, protocol.DoDMarkParams{
		ID: in.ID, State: in.State, Evidence: in.Evidence,
	}, &res)
	return res, err
}

// --- the direct door ------------------------------------------------------

// gateQuietLogger is the daemon.Logger the direct door hands the three
// services: a seed/CLI direct door has no log stream, and a service
// whose logger writes nothing logs nothing.
type gateQuietLogger struct{}

func (gateQuietLogger) Infof(string, ...any) {}

// gateDirectDoor is the 22 verbs over the same three services the
// daemon's own doors call (daemon.go mounts the same constructors), over
// a store opened by this process, with the actor {cli,cli}. Every method
// is a call plus the refusal-shape fold — never a decision.
type gateDirectDoor struct {
	st     *store.Store
	verify *daemon.VerifyService
	rule   *daemon.MergeRuleService
	dod    *daemon.DoDService
	actor  protocol.Actor
}

func openGateDirectDoor() (*gateDirectDoor, error) {
	st, err := openStoreSkipMigrations()
	if err != nil {
		return nil, fmt.Errorf("aeon: the store will not open for the direct gate door: %v", err)
	}
	log := gateQuietLogger{}
	return &gateDirectDoor{
		st:     st,
		verify: daemon.NewVerifyService(st, log),
		rule:   daemon.NewMergeRuleService(st, log),
		dod:    daemon.NewDoDService(st, log),
		// The actor the direct door stamps: the CLI, as a kind, with no
		// pretence at a uid — the socket door's actor is the peer's, and
		// TestCLIActorIsCliOnTheDirectDoorOnly pins that only the direct
		// door can carry cli (the socket refuses a params actor outright).
		actor: protocol.Actor{Kind: "cli", ID: "cli"},
	}, nil
}

func (d *gateDirectDoor) Close() error { return d.st.Close() }

func (d *gateDirectDoor) VerifyList(ctx context.Context, q protocol.VerifyListQuery) (protocol.VerifyListResult, error) {
	return d.verify.List(ctx, q)
}
func (d *gateDirectDoor) VerifyShow(ctx context.Context, id string) (protocol.VerifyShowResult, error) {
	return d.verify.Show(ctx, id)
}
func (d *gateDirectDoor) VerifyRuns(ctx context.Context, q protocol.VerifyRunsQuery) (protocol.VerifyRunsResult, error) {
	return d.verify.Runs(ctx, q)
}
func (d *gateDirectDoor) VerifyRunShow(ctx context.Context, id string) (protocol.VerifyRunShowResult, error) {
	return d.verify.RunShow(ctx, id)
}
func (d *gateDirectDoor) VerifyVocabulary(ctx context.Context) (protocol.VerifyVocabularyResult, error) {
	return d.verify.Vocabulary(ctx)
}
func (d *gateDirectDoor) VerifyTemplates(ctx context.Context) (protocol.VerifyTemplatesResult, error) {
	return d.verify.Templates(ctx)
}

func (d *gateDirectDoor) VerifyAdd(ctx context.Context, in protocol.VerifyAddInput) (protocol.VerifyWriteResult, error) {
	res, refusal, err := d.verify.Add(ctx, d.actor, in)
	if refusal != nil {
		return res, gateVerifyRefused("add", refusal)
	}
	return res, err
}

func (d *gateDirectDoor) VerifyUpdate(ctx context.Context, in protocol.VerifyUpdateInput) (protocol.VerifyWriteResult, error) {
	res, refusal, err := d.verify.Update(ctx, d.actor, in)
	if refusal != nil {
		return res, gateVerifyRefused("update", refusal)
	}
	return res, err
}

func (d *gateDirectDoor) VerifyArchive(ctx context.Context, id string) (protocol.VerifyWriteResult, error) {
	res, refusal, err := d.verify.Archive(ctx, d.actor, id)
	if refusal != nil {
		return res, gateVerifyRefused("archive", refusal)
	}
	return res, err
}

func (d *gateDirectDoor) VerifyRestore(ctx context.Context, id string) (protocol.VerifyWriteResult, error) {
	res, refusal, err := d.verify.Restore(ctx, d.actor, id)
	if refusal != nil {
		return res, gateVerifyRefused("restore", refusal)
	}
	return res, err
}

func (d *gateDirectDoor) VerifyApplyTemplate(ctx context.Context, componentID string, templateIDs []string) (protocol.VerifyApplyResult, error) {
	res, refusal, err := d.verify.ApplyTemplate(ctx, d.actor, componentID, templateIDs)
	if refusal != nil {
		return res, gateVerifyRefused("applyTemplate", refusal)
	}
	return res, err
}

// VerifyTry is the one method the direct door refuses without opening
// the store at all (the store was opened to answer the OTHER verbs):
// the run would execute with no policy gate in the process. Fixed
// sentence, non-zero exit, no spawn.
func (d *gateDirectDoor) VerifyTry(ctx context.Context, componentID, gateID string) (protocol.VerifyTryResult, error) {
	return protocol.VerifyTryResult{}, errors.New(gateTryOfflineRefusal)
}

func (d *gateDirectDoor) MergeRuleGet(ctx context.Context, componentID string, effective bool) (protocol.MergeRuleGetResult, error) {
	return d.rule.Get(ctx, componentID, effective)
}
func (d *gateDirectDoor) MergeRulePreview(ctx context.Context, componentID string, pr int) (protocol.MergeRulePreviewResult, error) {
	return d.rule.Preview(ctx, componentID, pr)
}
func (d *gateDirectDoor) MergeRuleChanges(ctx context.Context, componentID string, limit int) (protocol.MergeRuleChangesResult, error) {
	return d.rule.Changes(ctx, componentID, limit)
}
func (d *gateDirectDoor) MergeRuleVocabulary(ctx context.Context) (protocol.MergeRuleVocabularyResult, error) {
	return d.rule.Vocabulary(ctx)
}

func (d *gateDirectDoor) MergeRuleSet(ctx context.Context, in protocol.MergeRuleSetInput) (protocol.MergeRuleSetResult, error) {
	res, refusal, err := d.rule.Set(ctx, d.actor, in)
	if refusal != nil {
		return res, gateMergeRuleRefused("set", refusal)
	}
	return res, err
}

func (d *gateDirectDoor) MergeRuleClear(ctx context.Context, componentID string) (protocol.MergeRuleSetResult, error) {
	res, refusal, err := d.rule.Clear(ctx, d.actor, componentID)
	if refusal != nil {
		return res, gateMergeRuleRefused("clear", refusal)
	}
	return res, err
}

func (d *gateDirectDoor) MergeEvents(ctx context.Context, f protocol.MergeEventsQuery) (protocol.MergeEventsResult, error) {
	return d.rule.Events(ctx, f)
}
func (d *gateDirectDoor) MergeWatches(ctx context.Context, f protocol.MergeWatchesQuery) (protocol.MergeWatchesResult, error) {
	return d.rule.Watches(ctx, f)
}

func (d *gateDirectDoor) DoDList(ctx context.Context, loopSlug string) (protocol.DoDListResult, error) {
	return d.dod.List(ctx, loopSlug)
}

func (d *gateDirectDoor) DoDMark(ctx context.Context, in protocol.DoDMarkInput) (protocol.DoDWriteResult, error) {
	res, refusal, err := d.dod.Mark(ctx, d.actor, in)
	if refusal != nil {
		return res, gateDoDRefused("mark", refusal)
	}
	return res, err
}

// --- the refusal shapes ----------------------------------------------------
//
// The daemon's doors fold a service refusal into a *protocol.Error
// (protocol/verify.go's verifyRefusedError and siblings, unexported
// there). The direct door builds the SAME wire shape — same constant,
// same sentence pattern, same Kind — so renderError prints identical
// bytes through either door. The parity test compares those bytes; this
// is plumbing to the wire's own voice, not a second copy of any
// service's judgement.

func gateVerifyRefused(verb string, r *protocol.VerifyRefusal) error {
	e := protocol.ErrVerifyRefused.WithWhat("verify %s refused: %s", verb, r.Detail)
	e.Data.Kind = r.Kind
	return e
}

func gateMergeRuleRefused(verb string, r *protocol.MergeRuleRefusal) error {
	e := protocol.ErrMergeRuleRefused.WithWhat("merge rule %s refused: %s", verb, r.Detail)
	e.Data.Kind = r.Kind
	return e
}

func gateDoDRefused(verb string, r *protocol.DoDRefusal) error {
	e := protocol.ErrDodRefused.WithWhat("dod %s refused: %s", verb, r.Detail)
	e.Data.Kind = r.Kind
	return e
}

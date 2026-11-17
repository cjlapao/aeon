package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// blueprint_client.go — S48 T11 (BP12, BP13, P34, S45 PM4's shape). Every
// `aeon blueprint` verb is a CLIENT of the socket methods; the offline road
// is the same code over a directly opened store, never a second
// implementation. The two doors answer the SAME wire shapes (the protocol
// result types), so each verb has ONE presentation function and the printed
// bytes cannot depend on which door answered — that claim is
// TestSocketAndDirectDoorsProduceTheSameOutput, not a comment.
//
// What the CLI never does: re-validate, re-serialize YAML, diff a blueprint,
// or decide that a name exists. Flag parsing refuses only what cannot be a
// value (a missing --file, an unreadable file, a non-numeric --rev); every
// other refusal is the daemon's sentence or report, printed verbatim.

// blueprintDoor is the two doors onto the one blueprint implementation.
type blueprintDoor interface {
	fields(ctx context.Context) (json.RawMessage, error)
	list(ctx context.Context, p protocol.BlueprintListParams) (protocol.BlueprintListResult, error)
	show(ctx context.Context, name string) (protocol.BlueprintShowResult, error)
	validate(ctx context.Context, p protocol.BlueprintValidateParams) (json.RawMessage, error)
	save(ctx context.Context, p protocol.BlueprintSaveParams) (protocol.BlueprintSaveResult, error)
	duplicate(ctx context.Context, p protocol.BlueprintDuplicateParams) (protocol.BlueprintSaveResult, error)
	delete(ctx context.Context, p protocol.BlueprintDeleteParams) (protocol.BlueprintDeleteResult, error)
	revert(ctx context.Context, p protocol.BlueprintRevertParams) (protocol.BlueprintSaveResult, error)
	override(ctx context.Context, p protocol.BlueprintOverrideParams) (protocol.BlueprintOverrideResult, error)
	usage(ctx context.Context, name string) (protocol.BlueprintUsageResult, error)
	history(ctx context.Context, name string, limit int) (protocol.BlueprintHistoryResult, error)
	revision(ctx context.Context, name string, rev int) (protocol.BlueprintRevisionResult, error)
	diff(ctx context.Context, p protocol.BlueprintDiffParams) (protocol.BlueprintDiffResult, error)
	lint(ctx context.Context, p protocol.BlueprintLintParams) (protocol.BlueprintLintResult, error)
}

// blueprintDial is the dial the selection uses; a var so a test can stand in
// a dial that refuses (a handshake fault) or answers "no endpoint" without
// touching the real endpoint. Production is the one dial(0) — the same dial
// every other verb uses, so the endpoint resolution is never forked.
var blueprintDial = dial

// blueprintDirectNotice is the fixed stderr line the offline door says BEFORE
// any output (PM4's flagged honesty — a person must never wonder which door
// answered). It is a const, not a template: the test pins the exact bytes.
const blueprintDirectNotice = "daemon not running — applied directly to the store"

// selectBlueprintDoor is the selection (PM4): dial; a success is the socket
// door, a "no daemon" is the direct door (with the fixed line on errOut
// before any output), and any other dial failure is a REFUSAL. A daemon that
// exists and locks you out, or disagrees about the protocol, is a state the
// operator sees — the CLI never routes around it.
func selectBlueprintDoor(errOut io.Writer) (blueprintDoor, error) {
	c, err := blueprintDial(0)
	if err == nil {
		return &blueprintSocketDoor{c: c}, nil
	}
	if isNoDaemon(err) {
		fmt.Fprintln(errOut, blueprintDirectNotice)
		return &blueprintDirectDoor{}, nil
	}
	return nil, err
}

// blueprintVerbatim maps a wire refusal to its `what` sentence VERBATIM —
// the daemon's own words are the refusal (the blueprint plane's refusals
// carry the named name, the nearest names, the report); anything else (a
// transport fault, an unknown method) travels as the error itself.
func blueprintVerbatim(err error) error {
	var pe *protocol.Error
	if errors.As(err, &pe) {
		// The daemon's sentence, unwrapped. `What` when it filled one in (the
		// write refusals do); otherwise the Message, which for the blueprint
		// plane IS the whole sentence including its `why:`/`do:` lines —
		// blueprint.NewErrUnknownBlueprint writes its advice into the message,
		// and the offline door hands that same error up untouched. The code
		// number is what a machine reads; a person reading a CLI gets the
		// sentence, which is the PM4 ruling this door inherits.
		sentence := pe.Data.What
		if sentence == "" {
			sentence = pe.Message
		}
		if sentence != "" {
			return errors.New(sentence)
		}
	}
	return err
}

// --- the socket door ---------------------------------------------------------------

// blueprintSocketDoor is the default door: it calls the registered methods
// and relays what the wire said. It validates nothing.
type blueprintSocketDoor struct{ c *protocol.Client }

func (d *blueprintSocketDoor) call(method protocol.MethodName, params, out any) error {
	if err := d.c.Call(method, params, out); err != nil {
		return blueprintVerbatim(err)
	}
	return nil
}

func (d *blueprintSocketDoor) fields(_ context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	err := d.call(protocol.MethodBlueprintFields, struct{}{}, &raw)
	return raw, err
}

func (d *blueprintSocketDoor) list(_ context.Context, p protocol.BlueprintListParams) (protocol.BlueprintListResult, error) {
	var res protocol.BlueprintListResult
	err := d.call(protocol.MethodBlueprintList, p, &res)
	return res, err
}

func (d *blueprintSocketDoor) show(_ context.Context, name string) (protocol.BlueprintShowResult, error) {
	var res protocol.BlueprintShowResult
	err := d.call(protocol.MethodBlueprintShow, protocol.BlueprintShowParams{Name: name}, &res)
	return res, err
}

func (d *blueprintSocketDoor) validate(_ context.Context, p protocol.BlueprintValidateParams) (json.RawMessage, error) {
	var raw json.RawMessage
	err := d.call(protocol.MethodBlueprintValidate, p, &raw)
	return raw, err
}

func (d *blueprintSocketDoor) save(_ context.Context, p protocol.BlueprintSaveParams) (protocol.BlueprintSaveResult, error) {
	var res protocol.BlueprintSaveResult
	err := d.call(protocol.MethodBlueprintSave, p, &res)
	return res, err
}

func (d *blueprintSocketDoor) duplicate(_ context.Context, p protocol.BlueprintDuplicateParams) (protocol.BlueprintSaveResult, error) {
	var res protocol.BlueprintSaveResult
	err := d.call(protocol.MethodBlueprintDuplicate, p, &res)
	return res, err
}

func (d *blueprintSocketDoor) delete(_ context.Context, p protocol.BlueprintDeleteParams) (protocol.BlueprintDeleteResult, error) {
	var res protocol.BlueprintDeleteResult
	err := d.call(protocol.MethodBlueprintDelete, p, &res)
	return res, err
}

func (d *blueprintSocketDoor) revert(_ context.Context, p protocol.BlueprintRevertParams) (protocol.BlueprintSaveResult, error) {
	var res protocol.BlueprintSaveResult
	err := d.call(protocol.MethodBlueprintRevert, p, &res)
	return res, err
}

func (d *blueprintSocketDoor) override(_ context.Context, p protocol.BlueprintOverrideParams) (protocol.BlueprintOverrideResult, error) {
	var res protocol.BlueprintOverrideResult
	err := d.call(protocol.MethodBlueprintOverride, p, &res)
	return res, err
}

func (d *blueprintSocketDoor) usage(ctx context.Context, name string) (protocol.BlueprintUsageResult, error) {
	var res protocol.BlueprintUsageResult
	err := d.call(protocol.MethodBlueprintUsage, protocol.BlueprintUsageParams{Name: name}, &res)
	_ = ctx
	return res, err
}

func (d *blueprintSocketDoor) history(ctx context.Context, name string, limit int) (protocol.BlueprintHistoryResult, error) {
	var res protocol.BlueprintHistoryResult
	err := d.call(protocol.MethodBlueprintHistory, protocol.BlueprintHistoryParams{Name: name, Limit: limit}, &res)
	_ = ctx
	return res, err
}

func (d *blueprintSocketDoor) revision(ctx context.Context, name string, rev int) (protocol.BlueprintRevisionResult, error) {
	var res protocol.BlueprintRevisionResult
	err := d.call(protocol.MethodBlueprintRevision, protocol.BlueprintRevisionParams{Name: name, Rev: rev}, &res)
	_ = ctx
	return res, err
}

func (d *blueprintSocketDoor) diff(ctx context.Context, p protocol.BlueprintDiffParams) (protocol.BlueprintDiffResult, error) {
	var res protocol.BlueprintDiffResult
	err := d.call(protocol.MethodBlueprintDiff, p, &res)
	_ = ctx
	return res, err
}

func (d *blueprintSocketDoor) lint(ctx context.Context, p protocol.BlueprintLintParams) (protocol.BlueprintLintResult, error) {
	var res protocol.BlueprintLintResult
	err := d.call(protocol.MethodBlueprintLint, p, &res)
	_ = ctx
	return res, err
}

// --- the direct door ----------------------------------------------------------------

// blueprintDirectDoor is the offline fallback (PM4's flagged product call,
// BP12's one-implementation rule): the SAME daemon services the socket doors
// answer through, over a store opened directly (the justified second access
// path — WAL serialises it behind a daemon's writer, and there is no daemon
// when this door answers). It holds no validation, no diff, no YAML and no
// copy of a write path: every method is a call into internal/daemon's own
// adapters (blueprint_doors.go). The guard
// TestBlueprintCLINeverImportsStoreOutsideDirectDoor reads this file for the
// single legitimate store import in the blueprint CLI.
type blueprintDirectDoor struct{}

// open opens the store and hands out its close. The same system seed the
// daemon applies at arrival runs here first (BP10: idempotent, never
// overwrites a row a person wrote) so a store that never met a daemon answers
// for a shipped name instead of refusing an unknown one.
func (blueprintDirectDoor) open() (*store.Store, func(), error) {
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

// blueprintCLIActor is the offline door's stamp: it names itself rather than
// impersonating a socket peer (the socket's stamp comes from the connection).
// BP12 — an actor is never a params field, and this one is a const. (Named
// for its plane: `cliActor` is seed_door.go's for the seed corpus.)
var blueprintCLIActor = protocol.Actor{Kind: "cli", ID: "cli"}

func (d blueprintDirectDoor) fields(_ context.Context) (json.RawMessage, error) {
	// The field vocabulary is code, not rows: the answer is the daemon's own
	// answer function's payload, so both doors carry the same bytes.
	raw, err := json.Marshal(map[string]any{"fields": blueprintFieldsValue()})
	return raw, err
}

func (d blueprintDirectDoor) list(_ context.Context, p protocol.BlueprintListParams) (protocol.BlueprintListResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.BlueprintListResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewBlueprintReadDoors(s).List(p)
	return res, blueprintVerbatim(err)
}

func (d blueprintDirectDoor) show(_ context.Context, name string) (protocol.BlueprintShowResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.BlueprintShowResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewBlueprintReadDoors(s).Show(protocol.BlueprintShowParams{Name: name})
	return res, blueprintVerbatim(err)
}

func (d blueprintDirectDoor) validate(_ context.Context, p protocol.BlueprintValidateParams) (json.RawMessage, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return nil, err
	}
	defer closeFn()
	// The daemon's ONE validate answer over a store-backed source — the same
	// function the socket seam calls, so the report cannot differ.
	ans, err := daemon.BlueprintValidateAnswer(daemon.StoreBlueprintSource(s), p)
	if err != nil {
		return nil, blueprintVerbatim(err)
	}
	return json.Marshal(ans)
}

func (d blueprintDirectDoor) save(_ context.Context, p protocol.BlueprintSaveParams) (protocol.BlueprintSaveResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.BlueprintSaveResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewBlueprintWriteDoors(s).Save(context.Background(), blueprintCLIActor, p)
	return res, blueprintVerbatim(err)
}

func (d blueprintDirectDoor) duplicate(_ context.Context, p protocol.BlueprintDuplicateParams) (protocol.BlueprintSaveResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.BlueprintSaveResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewBlueprintWriteDoors(s).Duplicate(context.Background(), blueprintCLIActor, p)
	return res, blueprintVerbatim(err)
}

func (d blueprintDirectDoor) delete(_ context.Context, p protocol.BlueprintDeleteParams) (protocol.BlueprintDeleteResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.BlueprintDeleteResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewBlueprintWriteDoors(s).Delete(context.Background(), blueprintCLIActor, p)
	return res, blueprintVerbatim(err)
}

// override needs the running daemon: the routes a value may name are the
// daemon's registry, and the store alone cannot say which exist.
func (d blueprintDirectDoor) override(_ context.Context, _ protocol.BlueprintOverrideParams) (protocol.BlueprintOverrideResult, error) {
	return protocol.BlueprintOverrideResult{}, errors.New("blueprint override needs the daemon running: the routes it may name are the daemon's — start it with `aeon serve`")
}

func (d blueprintDirectDoor) revert(_ context.Context, p protocol.BlueprintRevertParams) (protocol.BlueprintSaveResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.BlueprintSaveResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewBlueprintWriteDoors(s).Revert(context.Background(), blueprintCLIActor, p)
	return res, blueprintVerbatim(err)
}

func (d blueprintDirectDoor) usage(ctx context.Context, name string) (protocol.BlueprintUsageResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.BlueprintUsageResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewBlueprintReadDoors(s).Usage(ctx, protocol.BlueprintUsageParams{Name: name})
	return res, blueprintVerbatim(err)
}

func (d blueprintDirectDoor) history(ctx context.Context, name string, limit int) (protocol.BlueprintHistoryResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.BlueprintHistoryResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewBlueprintReadDoors(s).History(ctx, protocol.BlueprintHistoryParams{Name: name, Limit: limit})
	return res, blueprintVerbatim(err)
}

func (d blueprintDirectDoor) revision(ctx context.Context, name string, rev int) (protocol.BlueprintRevisionResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.BlueprintRevisionResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewBlueprintReadDoors(s).Revision(ctx, protocol.BlueprintRevisionParams{Name: name, Rev: rev})
	return res, blueprintVerbatim(err)
}

func (d blueprintDirectDoor) diff(ctx context.Context, p protocol.BlueprintDiffParams) (protocol.BlueprintDiffResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.BlueprintDiffResult{}, err
	}
	defer closeFn()
	res, err := daemon.NewBlueprintReadDoors(s).Diff(ctx, p)
	return res, blueprintVerbatim(err)
}

func (d blueprintDirectDoor) lint(ctx context.Context, p protocol.BlueprintLintParams) (protocol.BlueprintLintResult, error) {
	s, closeFn, err := d.open()
	if err != nil {
		return protocol.BlueprintLintResult{}, err
	}
	defer closeFn()
	// The read service's lint with no tools catalog: the advisories that need
	// the catalog answer UNAVAILABLE, stated — the same explicit state a
	// daemon without the catalog answers, never an all-clear from absence.
	res, err := daemon.NewBlueprintReadDoors(s).Lint(ctx, p)
	return res, blueprintVerbatim(err)
}

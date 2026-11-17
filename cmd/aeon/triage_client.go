package main

// triage_client.go — S51 T6 (TM9, P5, the S50 roadmap_client.go shape).
// The five verbs S51 adds — update, history, board, health, vocabulary —
// reach the plane through ONE door abstraction with two arms. The socket
// arm is the default; the offline arm is the same answer functions over a
// directly opened store, never a second implementation: the CLI
// re-validates nothing, invents no sentence, and decides no law. The
// eight S29 verbs keep their socket-only roads untouched (TM9: goldens
// byte-identical), and the two enabling verbs — validate and handoff —
// have NO offline arm at all: offline there is no reactor to land an
// outcome and no loop-runner to hand off to, and a door that could not
// keep the promise would be a lie with a fixed stderr line on it (S50's
// dispatch-absence law, carried to the enabling verbs).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/triage"
)

// triageDoor is the two doors onto the one S51 triage plane. Update takes
// the actor as an argument so both arms share one interface — over the
// socket the stamp is the CONNECTION's person and the argument is ignored
// (TM: params carry no actor); offline the stamp is triageCLIActor.
type triageDoor interface {
	update(ctx context.Context, actor triage.Actor, p protocol.TriageUpdateParams) (protocol.TriageUpdateResult, error)
	board(ctx context.Context, p protocol.TriageBoardParams) ([]protocol.TriageBoardRow, error)
	history(ctx context.Context, p protocol.TriageHistoryParams) ([]protocol.TriageHistoryRow, error)
	vocabulary(ctx context.Context) (triage.Vocabulary, error)
	health(ctx context.Context) (protocol.TriageHealthResult, error)
}

// triageDial is the dial the selection uses; a var so a test can stand in
// a dial that refuses (a handshake fault) or answers "no endpoint" without
// touching the real endpoint. Production is the one dial(0), the same dial
// every other verb uses, so the endpoint resolution is never forked.
var triageDial = dial

// triageErrOut is where the fixed direct-door line goes; a var for the
// same reason roadmapErrOut is one — the parity test runs the verbs
// in-process and reads the notice, and production writes os.Stderr.
var triageErrOut io.Writer = os.Stderr

// triageDirectNotice is the fixed stderr line the offline door says BEFORE
// any output — a person must never wonder which door answered. It is the
// same fixed sentence the roadmap, blueprint and policy roads print.
const triageDirectNotice = "daemon not running — applied directly to the store"

// selectTriageDoor is the selection (PM4's rule, roadmap's shape): a dial
// success is the socket door; a "no daemon" is the direct door, with the
// fixed line on errOut before any output; any OTHER dial failure is a
// REFUSAL. A daemon that exists and locks you out, or disagrees about the
// protocol, is a state the operator sees — the CLI never routes around it.
func selectTriageDoor(errOut io.Writer) (triageDoor, error) {
	c, err := triageDial(0)
	if err == nil {
		return &triageSocketDoor{c: c}, nil
	}
	if isNoDaemon(err) {
		fmt.Fprintln(errOut, triageDirectNotice)
		return &triageDirectDoor{}, nil
	}
	return nil, err
}

// triageVerbatim maps a wire refusal to its `what` sentence VERBATIM —
// the daemon's own words are the refusal (the blank-title sentence, the
// unknown-bug −31934, the board's filter refusal); anything else (a
// transport fault, an unknown method) travels as the error itself.
func triageVerbatim(err error) error {
	var pe *protocol.Error
	if errors.As(err, &pe) {
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

// --- the socket door -----------------------------------------------------

// triageSocketDoor is the default door: it calls the registered methods
// and relays what the wire said. It validates nothing.
type triageSocketDoor struct{ c *protocol.Client }

func (d *triageSocketDoor) call(method protocol.MethodName, params, out any) error {
	if err := d.c.Call(method, params, out); err != nil {
		return triageVerbatim(err)
	}
	return nil
}

func (d *triageSocketDoor) update(_ context.Context, _ triage.Actor, p protocol.TriageUpdateParams) (protocol.TriageUpdateResult, error) {
	var res protocol.TriageUpdateResult
	err := d.call(protocol.MethodTriageUpdate, p, &res)
	return res, err
}

func (d *triageSocketDoor) board(_ context.Context, p protocol.TriageBoardParams) ([]protocol.TriageBoardRow, error) {
	var res []protocol.TriageBoardRow
	err := d.call(protocol.MethodTriageBoard, p, &res)
	return res, err
}

func (d *triageSocketDoor) history(_ context.Context, p protocol.TriageHistoryParams) ([]protocol.TriageHistoryRow, error) {
	var res []protocol.TriageHistoryRow
	err := d.call(protocol.MethodTriageHistory, p, &res)
	return res, err
}

func (d *triageSocketDoor) vocabulary(_ context.Context) (triage.Vocabulary, error) {
	var res triage.Vocabulary
	err := d.call(protocol.MethodTriageVocabulary, struct{}{}, &res)
	return res, err
}

func (d *triageSocketDoor) health(_ context.Context) (protocol.TriageHealthResult, error) {
	var res protocol.TriageHealthResult
	err := d.call(protocol.MethodTriageHealth, struct{}{}, &res)
	return res, err
}

// --- the direct door -------------------------------------------------------

// triageDirectDoor is the offline fallback: the SAME daemon door adapters
// the answers run through, over a store opened directly (the justified
// second access path — WAL serialises it behind a daemon's writer, and
// there is no daemon when this door answers). It holds no update loop, no
// filter, no refusal sentence: every method is one call into
// daemon.NewTriageManageDoors. The guard TestTriageDirectDoorHasNoLogic
// reads this file for the shape.
type triageDirectDoor struct{}

// triageCLIActor is the offline door's stamp: it names itself rather than
// impersonating a socket peer (the socket's stamp comes from the
// connection). An actor is never a params field, and this one is a var
// nobody can reach except through the door.
var triageCLIActor = triage.Actor{Kind: "cli", ID: "cli"}

func (triageDirectDoor) doors() (*daemon.TriageManageDoors, func(), error) {
	s, err := openStoreDirectly(false)
	if err != nil {
		return nil, nil, err
	}
	return daemon.NewTriageManageDoors(s), func() { _ = s.Close() }, nil
}

func (d triageDirectDoor) update(ctx context.Context, actor triage.Actor, p protocol.TriageUpdateParams) (protocol.TriageUpdateResult, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return protocol.TriageUpdateResult{}, err
	}
	defer closeFn()
	res, err := dr.Update(ctx, actor, p)
	return res, err
}

func (d triageDirectDoor) board(ctx context.Context, p protocol.TriageBoardParams) ([]protocol.TriageBoardRow, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return nil, err
	}
	defer closeFn()
	return dr.Board(ctx, p)
}

func (d triageDirectDoor) history(ctx context.Context, p protocol.TriageHistoryParams) ([]protocol.TriageHistoryRow, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return nil, err
	}
	defer closeFn()
	return dr.History(ctx, p)
}

func (d triageDirectDoor) vocabulary(ctx context.Context) (triage.Vocabulary, error) {
	// No store: the vocabulary is code the daemon ships, and the offline
	// door carries the very same bytes because they are the same function.
	return (&triage.Triage{}).Vocabulary(), ctx.Err()
}

func (d triageDirectDoor) health(ctx context.Context) (protocol.TriageHealthResult, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return protocol.TriageHealthResult{}, err
	}
	defer closeFn()
	return dr.Health(ctx)
}

package main

// roadmap_client.go — S50 T6 (RM9, P5, the blueprint BP12 shape). The five
// verbs S50 adds — update, history, board, health, vocabulary — reach the
// plane through ONE door abstraction with two arms. The socket arm is the
// default; the offline arm is the same answer functions over a directly
// opened store, never a second implementation: the CLI re-validates
// nothing, invents no sentence, and decides no law. The nine S26 verbs
// keep their own roads untouched (RM9: unchanged, goldens byte-identical),
// and the three dispatch verbs have NO offline arm at all — offline there
// is no loop-runner, and a door that could not keep the promise would be
// a lie with a fixed stderr line on it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
)

// roadmapDoor is the two doors onto the one S50 roadmap plane.
type roadmapDoor interface {
	update(ctx context.Context, actor roadmap.Actor, p protocol.RoadmapUpdateParams) (protocol.RoadmapUpdateResult, error)
	board(ctx context.Context, p protocol.RoadmapBoardParams) (protocol.RoadmapBoardResult, error)
	history(ctx context.Context, p protocol.RoadmapHistoryParams) (protocol.RoadmapHistoryResult, error)
	vocabulary(ctx context.Context) (roadmap.VocabularyAnswer, error)
	health(ctx context.Context) (protocol.RoadmapHealthResult, error)
}

// roadmapDial is the dial the selection uses; a var so a test can stand in
// a dial that refuses (a handshake fault) or answers "no endpoint" without
// touching the real endpoint. Production is the one dial(0), the same dial
// every other verb uses, so the endpoint resolution is never forked.
var roadmapDial = dial

// roadmapErrOut is where the fixed direct-door line goes; a var for the
// same reason blueprintErrOut is one — the parity test runs the verbs
// in-process and reads the notice, and production writes os.Stderr.
var roadmapErrOut io.Writer = os.Stderr

// roadmapDirectNotice is the fixed stderr line the offline door says BEFORE
// any output — a person must never wonder which door answered. It is the
// same fixed sentence the blueprint and policy roads print, spelled as its
// own const so a test can pin the exact bytes this file prints.
const roadmapDirectNotice = "daemon not running — applied directly to the store"

// selectRoadmapDoor is the selection (PM4's rule): a dial success is the
// socket door; a "no daemon" is the direct door, with the fixed line on
// errOut before any output; any OTHER dial failure is a REFUSAL. A daemon
// that exists and locks you out, or disagrees about the protocol, is a
// state the operator sees — the CLI never routes around it.
func selectRoadmapDoor(errOut io.Writer) (roadmapDoor, error) {
	c, err := roadmapDial(0)
	if err == nil {
		return &roadmapSocketDoor{c: c}, nil
	}
	if isNoDaemon(err) {
		fmt.Fprintln(errOut, roadmapDirectNotice)
		return &roadmapDirectDoor{}, nil
	}
	return nil, err
}

// roadmapVerbatim maps a wire refusal to its `what` sentence VERBATIM —
// the daemon's own words are the refusal (RM1's key refusal, D4's blocker,
// the unknown-row sentence); anything else (a transport fault, an unknown
// method) travels as the error itself.
func roadmapVerbatim(err error) error {
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

// --- the socket door ---------------------------------------------------------------

// roadmapSocketDoor is the default door: it calls the registered methods
// and relays what the wire said. It validates nothing.
type roadmapSocketDoor struct{ c *protocol.Client }

func (d *roadmapSocketDoor) call(method protocol.MethodName, params, out any) error {
	if err := d.c.Call(method, params, out); err != nil {
		return roadmapVerbatim(err)
	}
	return nil
}

func (d *roadmapSocketDoor) update(_ context.Context, _ roadmap.Actor, p protocol.RoadmapUpdateParams) (protocol.RoadmapUpdateResult, error) {
	// The actor is the CONNECTION's person daemon-side; the argument is
	// here only so both arms share one interface. The socket carries no
	// actor field and never could (RM2 — params are refused by key).
	var res protocol.RoadmapUpdateResult
	err := d.call(protocol.MethodRoadmapUpdate, p, &res)
	return res, err
}

func (d *roadmapSocketDoor) board(_ context.Context, p protocol.RoadmapBoardParams) (protocol.RoadmapBoardResult, error) {
	var res protocol.RoadmapBoardResult
	err := d.call(protocol.MethodRoadmapBoard, p, &res)
	return res, err
}

func (d *roadmapSocketDoor) history(_ context.Context, p protocol.RoadmapHistoryParams) (protocol.RoadmapHistoryResult, error) {
	var res protocol.RoadmapHistoryResult
	err := d.call(protocol.MethodRoadmapHistory, p, &res)
	return res, err
}

func (d *roadmapSocketDoor) vocabulary(_ context.Context) (roadmap.VocabularyAnswer, error) {
	var res roadmap.VocabularyAnswer
	err := d.call(protocol.MethodRoadmapVocabulary, struct{}{}, &res)
	return res, err
}

func (d *roadmapSocketDoor) health(_ context.Context) (protocol.RoadmapHealthResult, error) {
	var res protocol.RoadmapHealthResult
	err := d.call(protocol.MethodRoadmapHealth, struct{}{}, &res)
	return res, err
}

// --- the direct door ----------------------------------------------------------------

// roadmapDirectDoor is the offline fallback: the SAME daemon door adapters
// the answers run through, over a store opened directly (the justified
// second access path — WAL serialises it behind a daemon's writer, and
// there is no daemon when this door answers). It holds no update loop, no
// filter, no refusal sentence: every method is one call into
// daemon.NewRoadmapManageDoors. The guard TestRoadmapDirectDoorHasNoLogic
// reads this file for the shape.
type roadmapDirectDoor struct{}

// roadmapCLIActor is the offline door's stamp: it names itself rather than
// impersonating a socket peer (the socket's stamp comes from the
// connection). RM2 — an actor is never a params field, and this one is a
// var nobody can reach except through the door.
var roadmapCLIActor = roadmap.Actor{Kind: "cli", ID: "cli"}

func (roadmapDirectDoor) doors() (*daemon.RoadmapManageDoors, func(), error) {
	s, err := openStoreDirectly(false)
	if err != nil {
		return nil, nil, err
	}
	return daemon.NewRoadmapManageDoors(s), func() { _ = s.Close() }, nil
}

func (d roadmapDirectDoor) update(ctx context.Context, actor roadmap.Actor, p protocol.RoadmapUpdateParams) (protocol.RoadmapUpdateResult, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return protocol.RoadmapUpdateResult{}, err
	}
	defer closeFn()
	res, err := dr.Update(ctx, actor, p)
	return res, roadmapVerbatim(err)
}

func (d roadmapDirectDoor) board(ctx context.Context, p protocol.RoadmapBoardParams) (protocol.RoadmapBoardResult, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return protocol.RoadmapBoardResult{}, err
	}
	defer closeFn()
	res, err := dr.Board(ctx, p)
	return res, roadmapVerbatim(err)
}

func (d roadmapDirectDoor) history(ctx context.Context, p protocol.RoadmapHistoryParams) (protocol.RoadmapHistoryResult, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return protocol.RoadmapHistoryResult{}, err
	}
	defer closeFn()
	res, err := dr.History(ctx, p)
	return res, roadmapVerbatim(err)
}

func (d roadmapDirectDoor) vocabulary(ctx context.Context) (roadmap.VocabularyAnswer, error) {
	// No store: the vocabulary is code the daemon ships, and the offline
	// door carries the very same bytes because they are the same function.
	return roadmap.Vocabulary(), ctx.Err()
}

func (d roadmapDirectDoor) health(ctx context.Context) (protocol.RoadmapHealthResult, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return protocol.RoadmapHealthResult{}, err
	}
	defer closeFn()
	res, err := dr.Health(ctx)
	return res, roadmapVerbatim(err)
}

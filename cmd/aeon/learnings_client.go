package main

// learnings_client.go — S52 T5 (LM9, P5, the triage_client.go shape). The
// seven memory verbs — retract, restore, show (the gate read), board,
// history, vocabulary, for-agent — reach the plane through ONE door
// abstraction with two arms. The socket arm is the default; the offline
// arm is the same answer functions over a directly opened store, never a
// second implementation: the CLI re-validates nothing, invents no
// sentence, and decides no law — not the reason bound (LM2), not the
// actor kinds (LM4), not the readable set (LM1). The five S28 verbs
// (add/list/show/query/embed keep their own roads): `add` gained its actor
// rule underneath at T1/T2 — over the person's socket a person may speak
// `told` — and its bytes do not move here. The agent's own doors
// (learnings.mine.*) have NO arm at all: they are the agent's surface
// (LM1), and a person holding an offline store open is not an agent with a
// step.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/protocol"
)

// learningsDoor is the two doors onto the one memory plane. Retract and
// Restore take the actor as an argument so both arms share one interface —
// over the socket the stamp is the CONNECTION's person and the argument is
// ignored (params carry no actor, LM4); offline the stamp is
// learningsCLIActor.
type learningsDoor interface {
	retract(ctx context.Context, actor learnings.Actor, p protocol.LearningsRetractParams) (protocol.LearningsActResult, error)
	restore(ctx context.Context, actor learnings.Actor, p protocol.LearningsRestoreParams) (protocol.LearningsActResult, error)
	show(ctx context.Context, p protocol.LearningsShowParams) (protocol.LearningsItemResult, error)
	board(ctx context.Context, p protocol.LearningsBoardParams) ([]protocol.LearningsBoardRow, error)
	history(ctx context.Context, p protocol.LearningsHistoryParams) ([]protocol.LearningsChangeView, error)
	vocabulary(ctx context.Context) (protocol.LearningsVocabularyResult, error)
	forAgent(ctx context.Context, p protocol.LearningsForAgentParams) ([]protocol.LearningsForAgentHit, error)
}

// learningsDial is the dial the selection uses; a var so a test can stand
// in a dial that refuses (a handshake fault) or answers "no endpoint"
// without touching the real endpoint. Production is the one dial(0), the
// same dial every other verb uses, so the endpoint resolution is never
// forked.
var learningsDial = dial

// learningsErrOut is where the fixed direct-door line goes; a var for the
// same reason triageErrOut is one — the parity test runs the selection
// in-process and reads the notice, and production writes os.Stderr.
var learningsErrOut io.Writer = os.Stderr

// learningsDirectNotice is the fixed stderr line the offline door says
// BEFORE any output — a person must never wonder which door answered. The
// same fixed sentence the roadmap, blueprint, policy and triage roads
// print.
const learningsDirectNotice = "daemon not running — applied directly to the store"

// selectLearningsDoor is the selection (PM4's rule, triage's shape): a
// dial success is the socket door; a "no daemon" is the direct door, with
// the fixed line on errOut before any output; any OTHER dial failure is a
// REFUSAL. A daemon that exists and locks you out, or disagrees about the
// protocol, is a state the operator sees — the CLI never routes around it.
func selectLearningsDoor(errOut io.Writer) (learningsDoor, error) {
	c, err := learningsDial(0)
	if err == nil {
		return &learningsSocketDoor{c: c}, nil
	}
	if isNoDaemon(err) {
		fmt.Fprintln(errOut, learningsDirectNotice)
		return &learningsDirectDoor{}, nil
	}
	return nil, err
}

// learningsVerbatim maps a wire refusal to its `what` sentence VERBATIM —
// the daemon's own words are the refusal (the 257-byte reason, the
// unknown id, the unreadable agent); anything else (a transport fault, an
// unknown method) travels as the error itself.
func learningsVerbatim(err error) error {
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

// learningsSocketDoor is the default door: it calls the registered methods
// and relays what the wire said. It validates nothing — not the reason's
// length, not the scope, not the agent slug.
type learningsSocketDoor struct{ c *protocol.Client }

func (d *learningsSocketDoor) call(method protocol.MethodName, params, out any) error {
	if err := d.c.Call(method, params, out); err != nil {
		return learningsVerbatim(err)
	}
	return nil
}

func (d *learningsSocketDoor) retract(_ context.Context, _ learnings.Actor, p protocol.LearningsRetractParams) (protocol.LearningsActResult, error) {
	var res protocol.LearningsActResult
	err := d.call(protocol.MethodLearningsRetract, p, &res)
	return res, err
}

func (d *learningsSocketDoor) restore(_ context.Context, _ learnings.Actor, p protocol.LearningsRestoreParams) (protocol.LearningsActResult, error) {
	var res protocol.LearningsActResult
	err := d.call(protocol.MethodLearningsRestore, p, &res)
	return res, err
}

func (d *learningsSocketDoor) show(_ context.Context, p protocol.LearningsShowParams) (protocol.LearningsItemResult, error) {
	var res protocol.LearningsItemResult
	err := d.call(protocol.MethodLearningsShow, p, &res)
	return res, err
}

func (d *learningsSocketDoor) board(_ context.Context, p protocol.LearningsBoardParams) ([]protocol.LearningsBoardRow, error) {
	var res protocol.LearningsBoardResult
	err := d.call(protocol.MethodLearningsBoard, p, &res)
	return res.Learnings, err
}

func (d *learningsSocketDoor) history(_ context.Context, p protocol.LearningsHistoryParams) ([]protocol.LearningsChangeView, error) {
	var res protocol.LearningsHistoryResult
	err := d.call(protocol.MethodLearningsHistory, p, &res)
	return res.Changes, err
}

func (d *learningsSocketDoor) vocabulary(_ context.Context) (protocol.LearningsVocabularyResult, error) {
	var res protocol.LearningsVocabularyResult
	err := d.call(protocol.MethodLearningsVocabulary, struct{}{}, &res)
	return res, err
}

func (d *learningsSocketDoor) forAgent(_ context.Context, p protocol.LearningsForAgentParams) ([]protocol.LearningsForAgentHit, error) {
	var res protocol.LearningsForAgentResult
	err := d.call(protocol.MethodLearningsForAgent, p, &res)
	return res.Hits, err
}

// --- the direct door -------------------------------------------------------

// learningsDirectDoor is the offline fallback: the SAME daemon door
// adapters the answers run through, over a store opened directly (the
// justified second access path — WAL serialises it behind a daemon's
// writer, and there is no daemon when this door answers). It holds no
// stamping rule, no filter, no refusal sentence of its own: every method
// is one call into daemon.NewLearningsManageDoors. The guard
// TestLearningsDirectDoorHasNoLogicCopy reads this file for the shape.
type learningsDirectDoor struct{}

// learningsCLIActor is the offline door's stamp: it names itself rather
// than impersonating a socket peer (the socket's stamp comes from the
// connection). An actor is never a params field, and this one is a var
// nobody can reach except through the door. The audit's CHECK admitted
// 'cli' the day S52 T1 drew it, FOR this door (LM3 bounds the `agent`
// kind, not this one).
var learningsCLIActor = learnings.Actor{Kind: "cli", ID: "cli"}

func (learningsDirectDoor) doors() (*daemon.LearningsManageDoors, func(), error) {
	s, err := openStoreDirectly(false)
	if err != nil {
		return nil, nil, err
	}
	return daemon.NewLearningsManageDoors(s), func() { _ = s.Close() }, nil
}

func (d learningsDirectDoor) retract(ctx context.Context, actor learnings.Actor, p protocol.LearningsRetractParams) (protocol.LearningsActResult, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return protocol.LearningsActResult{}, err
	}
	defer closeFn()
	res, err := dr.Retract(ctx, actor, p)
	return res, learningsVerbatim(err)
}

func (d learningsDirectDoor) restore(ctx context.Context, actor learnings.Actor, p protocol.LearningsRestoreParams) (protocol.LearningsActResult, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return protocol.LearningsActResult{}, err
	}
	defer closeFn()
	res, err := dr.Restore(ctx, actor, p)
	return res, learningsVerbatim(err)
}

func (d learningsDirectDoor) show(ctx context.Context, p protocol.LearningsShowParams) (protocol.LearningsItemResult, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return protocol.LearningsItemResult{}, err
	}
	defer closeFn()
	res, err := dr.Show(ctx, p)
	return res, learningsVerbatim(err)
}

func (d learningsDirectDoor) board(ctx context.Context, p protocol.LearningsBoardParams) ([]protocol.LearningsBoardRow, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return nil, err
	}
	defer closeFn()
	res, err := dr.Board(ctx, p)
	return res.Learnings, learningsVerbatim(err)
}

func (d learningsDirectDoor) history(ctx context.Context, p protocol.LearningsHistoryParams) ([]protocol.LearningsChangeView, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return nil, err
	}
	defer closeFn()
	res, err := dr.History(ctx, p)
	return res.Changes, learningsVerbatim(err)
}

func (d learningsDirectDoor) vocabulary(_ context.Context) (protocol.LearningsVocabularyResult, error) {
	// No store: the vocabulary is code the daemon ships, and the offline
	// door carries the very same bytes because they are the same function
	// (the triage road's shape — not even a store handle is opened).
	return protocol.AnswerLearningsVocabulary(), nil
}

func (d learningsDirectDoor) forAgent(ctx context.Context, p protocol.LearningsForAgentParams) ([]protocol.LearningsForAgentHit, error) {
	dr, closeFn, err := d.doors()
	if err != nil {
		return nil, err
	}
	defer closeFn()
	res, err := dr.ForAgent(ctx, p)
	return res.Hits, learningsVerbatim(err)
}

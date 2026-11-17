package main

// seed_door.go — S43-extra T7 (SD5, SD10). The two doors onto the one seed
// engine, and the selection between them.
//
// # One engine, two doors (P34, the S45 PM4 pattern)
//
// The seed engine is the daemon's, and the CLI is a CLIENT of it. `selectSeedDoor`
// dials the daemon and, on a successful handshake, speaks the four `seed.*`
// methods over the socket (socketDoor). When there is NO daemon (the socket is
// not reachable — not merely a version or permission error) the CLI opens the
// SAME engine directly over the store (directDoor) and says so. Any other dial
// failure (a version mismatch, a permission error) REFUSES — it never falls
// back, because a live-but-disagreed daemon is a state the operator must see,
// not one the CLI routes around.
//
// # The direct door is the engine, not a copy
//
// directDoor builds `daemon.NewSeedEngine` over a directly opened store and
// calls it — it opens no second store, no second registry, no second
// applier, and it applies sets by the engine's OWN rules (inert freely, guard
// only through the explicit verb the CLI gates with --yes). There is no
// CLI-only bypass: the gate the CLI keeps and the gate the engine keeps are
// the same gate. `TestDirectDoorUsesTheEngineNotACopy` greps this file to
// prove it calls the engine and nothing else.
//
// # The actor is the connection or the cli — never a param
//
// The socket door's actor comes from the connection (the daemon's ActorFor
// seam, stamped server-side; the CLI never sends an actor). The direct door's
// actor is the fixed {cli, cli}. Neither door reads an actor from a flag — a
// params actor would let a caller forge the audit identity of a write (the
// same refusal the policy door keeps, §4d).
//
// This file is the ONLY place in the CLI that imports internal/store and
// internal/seed: the verbs (seed.go) import internal/protocol only, so a verb
// cannot reopen the store (TestSeedCLINeverImportsStoreOutsideDirectDoor).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/policyseed"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// seedDoor is the two doors onto the one seed engine: a read (status/plan)
// and a write (apply/restore) that the verbs speak through. Both doors
// answer the SAME engine, so the wire the CLI reads cannot drift from the
// truth (the parity test pins it, TestSeedSocketAndDirectDoorsProduceTheSameOutput).
type seedDoor interface {
	status(ctx context.Context, set string) (protocol.SeedStatusResult, error)
	plan(ctx context.Context, set string, keys []string) (protocol.SeedPlanResult, error)
	apply(ctx context.Context, set string, keys []string) (protocol.SeedApplyResult, error)
	restore(ctx context.Context, set string, key string) (protocol.SeedRestoreResult, error)
}

// cliActor is the direct door's audit identity (SD10): the cli is who a
// direct, offline apply is. It is a constant, never a flag — a params actor
// would forge the audit identity of a write.
var cliActor = seed.Actor{Kind: "cli", ID: "cli"}

// seedDial is the dial the selection uses; a var so a test can stand in a
// dial that refuses (a version fault) or answers "no daemon", without
// touching the real endpoint. Production is the one dial(0).
var seedDial = dial

// selectSeedDoor is the selection (S45 PM4): dial; a success is the socket
// door, a "no daemon" is the direct door (with the fixed stderr line), and
// any other dial failure is a refusal — it never falls back.
func selectSeedDoor(ctx context.Context, errOut io.Writer) (seedDoor, error) {
	c, err := seedDial(0)
	if err != nil {
		if isNoDaemon(err) {
			// "no daemon" is the ONLY fall back: the socket is not there, so
			// the CLI opens the same engine directly and says so. The fixed
			// line is a constant (D9), never assembled from the error.
			fmt.Fprintln(errOut, "daemon not running — applied directly to the store")
			return newDirectDoorFunc(ctx)
		}
		// A version mismatch, a permission error, any other fault: the daemon
		// is there and disagreeing, or the operator must fix a permission —
		// the CLI refuses rather than route around it (SD10).
		return nil, err
	}
	return socketDoor{c: c}, nil
}

// isNoDaemon reports whether a dial failure means the daemon is simply not
// there (fall back to the direct door) rather than present-but-disagreed
// (refuse). The dial wraps every connection failure in "no daemon at %s: %w";
// a version/protocol error comes from the handshake and is NOT wrapped, so the
// un-wrapped ones are refusals. A permission error is a wrapped connection
// failure that is still a refusal — the socket exists but the operator may not
// connect, and a CLI that opened a second door around it would hide that.
func isNoDaemon(err error) bool {
	if err == nil {
		return false
	}
	if !strings.Contains(err.Error(), "no daemon at") {
		return false // a handshake fault (version, protocol): present-but-disagreed
	}
	var op *net.OpError
	if errors.As(err, &op) && errors.Is(op.Err, os.ErrPermission) {
		return false // a permission refusal, not "no daemon"
	}
	return true // connection refused / no such socket / timeout: no daemon
}

// --- the socket door ------------------------------------------------------

// socketDoor speaks the four seed.* methods over the daemon's socket. The
// actor for a write is stamped server-side (the connection), so this door
// sends no actor.
type socketDoor struct {
	c *protocol.Client
}

func (d socketDoor) status(ctx context.Context, set string) (protocol.SeedStatusResult, error) {
	var res protocol.SeedStatusResult
	p := protocol.SeedStatusParams{}
	if set != "" {
		p.Set = &set
	}
	if err := d.c.Call(protocol.MethodSeedStatus, p, &res); err != nil {
		return protocol.SeedStatusResult{}, err
	}
	return res, nil
}

func (d socketDoor) plan(ctx context.Context, set string, keys []string) (protocol.SeedPlanResult, error) {
	var res protocol.SeedPlanResult
	if err := d.c.Call(protocol.MethodSeedPlan, protocol.SeedPlanParams{Set: set, Keys: keys}, &res); err != nil {
		return protocol.SeedPlanResult{}, err
	}
	return res, nil
}

func (d socketDoor) apply(ctx context.Context, set string, keys []string) (protocol.SeedApplyResult, error) {
	var res protocol.SeedApplyResult
	if err := d.c.Call(protocol.MethodSeedApply, protocol.SeedApplyParams{Set: set, Keys: keys}, &res); err != nil {
		return protocol.SeedApplyResult{}, err
	}
	return res, nil
}

func (d socketDoor) restore(ctx context.Context, set string, key string) (protocol.SeedRestoreResult, error) {
	var res protocol.SeedRestoreResult
	if err := d.c.Call(protocol.MethodSeedRestore, protocol.SeedRestoreParams{Set: set, Key: key}, &res); err != nil {
		return protocol.SeedRestoreResult{}, err
	}
	return res, nil
}

// --- the direct door ------------------------------------------------------

// directDoor builds the one seed engine over a directly opened store and
// speaks it. It is the engine, not a copy: no second store, registry or
// applier, and the apply/restore go through the engine's own rules (inert
// freely, guard only through the explicit verb).
type directDoor struct {
	e  *seed.Engine
	st *store.Store
}

// newDirectDoorFunc is the direct-door construction; a var so a test can stand
// in a direct door without opening a store (the fall-back test).
var newDirectDoorFunc = newDirectDoor

func newDirectDoor(ctx context.Context) (seedDoor, error) {
	// S62 T5: the safety net rides the direct door too — a seed that opens
	// an old store migrates it, and the copy precedes the first migration.
	// --no-backup is not exposed here: the flag is the db verbs' (BK18).
	st, err := store.Open(storeOpenOptions(false, "cli"))
	if err != nil {
		return nil, err
	}
	e, err := daemon.NewSeedEngine(st, seed.Options{}, mcpSeedValidator(st))
	if err != nil {
		return nil, err
	}
	return directDoor{e: e, st: st}, nil
}

func (d directDoor) status(ctx context.Context, set string) (protocol.SeedStatusResult, error) {
	states, err := d.e.Status(ctx, set)
	if err != nil {
		return protocol.SeedStatusResult{}, err
	}
	// The one mapping the socket's status also calls (SD10, the parity
	// test pins it field for field) — a second copy would be a second truth.
	return protocol.SeedStatusView(states), nil
}

func (d directDoor) plan(ctx context.Context, set string, keys []string) (protocol.SeedPlanResult, error) {
	p, err := d.e.Plan(ctx, set, keys)
	if err != nil {
		return protocol.SeedPlanResult{}, err
	}
	return protocol.SeedPlanResult{Plan: seedPlanToWire(p)}, nil
}

func (d directDoor) apply(ctx context.Context, set string, keys []string) (protocol.SeedApplyResult, error) {
	// S45 T12: the corpus set's apply runs through the corpus door — the
	// engine's apply plus the corpus bookkeeping (the actor rule, the
	// agent-table existence refusals when keys name corpus records, the
	// run's ONE policy_change seed row). Same engine, same rules, one
	// extra audit line; every other set rides the engine untouched.
	if set == policyseed.SetID {
		cr, cerr := policyseed.ApplyCorpus(ctx, d.e, d.st, nil, keys, false, cliActor, time.Now())
		if cerr != nil {
			return protocol.SeedApplyResult{}, cerr
		}
		return protocol.SeedApplyResult{Apply: seedApplyOutcomeToWire(cr.Applied)}, nil
	}
	// via=explicit: the human door, the only path a guard set may take (SD5).
	r, err := d.e.Apply(ctx, set, keys, cliActor, seed.PathExplicit)
	if err != nil {
		return protocol.SeedApplyResult{}, err
	}
	// The confirmation is the CLI's concern (the --yes gate), never the
	// engine's; a direct apply does not pretend a socket can prove a human.
	return protocol.SeedApplyResult{Apply: seedApplyOutcomeToWire(r)}, nil
}

func (d directDoor) restore(ctx context.Context, set string, key string) (protocol.SeedRestoreResult, error) {
	r, err := d.e.Restore(ctx, set, key, cliActor)
	if err != nil {
		return protocol.SeedRestoreResult{}, err
	}
	return protocol.SeedRestoreResult{Apply: seedApplyOutcomeToWire(r)}, nil
}

// seedPlanToWire is the engine→wire twin of the plan the socket's seed.plan
// maps (SD10). The engine's and the wire's plan types are field-for-field
// twins, so this is a faithful copy; the parity test pins it against the
// socket's output.
func seedPlanToWire(p seed.SetPlan) protocol.SeedSetPlan {
	out := protocol.SeedSetPlan{
		SetID:     p.SetID,
		Decisions: make([]protocol.SeedDecision, 0, len(p.Decisions)),
	}
	for _, d := range p.Decisions {
		out.Decisions = append(out.Decisions, protocol.SeedDecision{
			Key:         d.Key,
			Op:          string(d.Op),
			State:       string(d.State),
			FromVersion: d.FromVersion,
			ToVersion:   d.ToVersion,
			Reason:      d.Reason,
		})
	}
	out.Refusal = protocol.SeedRefusal{Key: p.Refusal.Key, Reason: p.Refusal.Reason}
	return out
}

// seedApplyOutcomeToWire is the engine→wire twin of an apply/restore result
// (SD10), shared by apply and restore so the two never drift.
func seedApplyOutcomeToWire(r seed.ApplyResult) protocol.SeedApplyOutcome {
	out := protocol.SeedApplyOutcome{
		SetID:   r.SetID,
		Records: make([]protocol.SeedRecordResult, 0, len(r.Records)),
	}
	for _, rec := range r.Records {
		out.Records = append(out.Records, protocol.SeedRecordResult{
			Key:         rec.Key,
			Op:          string(rec.Op),
			FromVersion: rec.FromVersion,
			ToVersion:   rec.ToVersion,
			Reason:      rec.Reason,
		})
	}
	return out
}

package main

import (
	"context"
	"errors"
	"os"
	"strconv"

	"github.com/cjlapao/aeon/internal/agentsvc"
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/remote"
)

// remote_mount.go — S21 T6's entry-point wiring: where the inbound
// completion finds the task book it binds to.
//
// The daemon must not import agentsvc (AS11's deletability, the same red
// line agent_services.go holds for the invoke half), so the adapter over
// the agentsvc TaskStore lives HERE, built by serve and injected through
// daemon.Options. An entry point that never calls wireRemoteCallbacks
// leaves the completion answering the ABSENCE by name — which is RA9's
// off-by-default reaching the socket: peers exist only when a person
// configured them at an explicit path with a pinned identity and a key,
// and the configuration path itself is an explicit environment variable
// (AEON_REMOTE_PEERS — named, never defaulted onto someone's home
// directory, so a fresh install has no path to fail against and no
// endpoint with anything to bind to).

// remoteTaskBook is the daemon's async tasks seen from the callback mount
// point: two questions and one action, answered from the SAME rows S14
// wrote. The awaiting reading is deliberate: an async Task suspended at a
// gate is TaskWaiting — that is the state P2 makes survivable, and the
// only state a completion may land into; sync Tasks never become async
// (no promotion path), so a completion for a sync id is simply not
// awaiting.
type remoteTaskBook struct{ ts *agentsvc.TaskStore }

func (b remoteTaskBook) Awaiting(task string) (string, bool, bool) {
	id, err := strconv.ParseInt(task, 10, 64)
	if err != nil {
		return "", false, false // a non-numeric id names nothing we ever created
	}
	t, err := b.ts.Get(id)
	if err != nil {
		return "", false, false // unknown is unknown; the store's silence is not a verdict we invent
	}
	return t.CalleeAgent, t.Status == agentsvc.TaskWaiting && t.Mode == agentsvc.TaskAsync, true
}

// Land runs S14's completion CAS — the first record wins — which is also
// what makes a replay land once without any cache of our own. A Reject
// naming the status or mode is the store SAYING "not there to land"
// (landed=false, no error); anything else is the store failing to answer,
// which the mount point renders as could-not-say, never as a verdict
// (P31). The completion itself resumes the suspended step through the
// store's Resume path — cold, from rows, which is the whole point of RA7.
func (b remoteTaskBook) Land(task, result string, cost *model.Cost) (bool, error) {
	id, err := strconv.ParseInt(task, 10, 64)
	if err != nil {
		return false, nil
	}
	// T8: the cost enters the SAME first-record-wins CAS as the result
	// (CompleteWithCost) — a result landed with its cost dropped is a
	// half-state the record cannot undo, and the cost decided by
	// remote.CostFor is never nil: absence is a state (unknown), not an
	// omission.
	if rj := b.ts.CompleteWithCost(context.Background(), id, result, "ok", cost); rj != nil {
		switch rj.Field {
		case "status", "mode":
			return false, nil
		default:
			// Including "cost": a cost this package's own CostFor produced
			// being refused by validateCost is OUR bug, not the peer's
			// completion — it answers could-not-say, never a verdict.
			return false, errors.New(rj.Detail)
		}
	}
	return true, nil
}

// wireRemoteCallbacks builds the callback mount point from the peer
// configuration. No AEON_REMOTE_PEERS, no wiring — and the method says so
// by name (the daemon's own words: "no remote peers are wired in this
// daemon"). A configured-but-unreadable file is a loud error at boot, not
// a quiet zero-peer world: a person who wrote the file meant to have
// peers, and starting without them silently is how a pinned identity
// quietly stops being enforced.
func wireRemoteCallbacks(opts *daemon.Options, tasks *agentsvc.TaskStore) error {
	path := os.Getenv("AEON_REMOTE_PEERS")
	if path == "" {
		return nil
	}
	cfg, err := remote.LoadConfigFile(path)
	if err != nil {
		return err
	}
	peers := make(map[string]remote.Peer, len(cfg.Peers))
	for _, p := range cfg.Peers {
		peers[p.ID] = p
	}
	opts.RemoteCallbacks = &remote.Callbacks{
		Peers: func(id string) (remote.Peer, bool) { p, ok := peers[id]; return p, ok },
		Book:  remoteTaskBook{ts: tasks},
	}
	return nil
}

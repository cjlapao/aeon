package container

import (
	"context"
	"fmt"
)

// Report is what one start sweep did. The numbers are printed by the daemon and
// asserted by the tests: a sweep that silently changed nothing when containers
// are lying around is the failure mode this file exists to close.
type Report struct {
	Adopted  int // a `ready` row whose container the engine still has — nothing to do, said out loud
	Finished int // a `provisioning` row settled (started, or removed)
	Removed  int // an orphan carrying aeon.managed, removed
	Failed   int // a `ready` row whose container vanished, marked failed
	Skipped  int // rows left exactly as they were
	// Inert is the honest third state: the engine was not reachable, so NOTHING
	// was changed and nothing was guessed. A daemon that crashes because docker
	// is down at start is a daemon that cannot boot; a daemon that guesses is
	// worse.
	Inert      bool
	Warnings   []string
	Unlabelled int // containers seen with no aeon.managed label — counted, NEVER touched
}

// Reconcile is the daemon-start sweep (EV12), beside ReconcileLoops. Its whole
// authority is the labelled list: a resource that does not carry `aeon.managed=1`
// is not ours, in any branch, and the count says how many were seen and left.
func Reconcile(ctx context.Context, led Ledger, rt Runtime, warn func(string)) (Report, error) {
	var rep Report
	live, err := led.ListLiveProvisions()
	if err != nil {
		return rep, fmt.Errorf("container: reconcile could not read the live provisions: %w", err)
	}
	handles, err := rt.List(ctx, LabelManaged+"=1")
	if err != nil {
		// The runtime is down. The rows stay exactly as they are — a
		// `provisioning` row is not evidence of a leak when the engine never
		// answered — and the daemon says so ONCE rather than crashing or, worse,
		// marking rows failed on a guess about an engine it could not ask.
		rep.Inert = true
		msg := "container: the engine did not answer at start, so the provision ledger was left exactly as it is: " + err.Error()
		rep.Warnings = append(rep.Warnings, msg)
		if warn != nil {
			warn(msg)
		}
		return rep, nil
	}

	// The networks are read BEFORE the rows are walked, because a network row has
	// to be asked about in the network's own inventory. `List` answers containers,
	// so a `ready` network row looked up there reads as a resource that "is gone"
	// and gets marked failed — and a failed network row is still live to the ledger
	// (`Live` is `removed_at IS NULL`), which refuses every later step on that loop
	// (W2-3's second half). Same authority rule as everywhere in this file: ours
	// only, by label. When this read fails the network rows are left EXACTLY as they
	// are below — the per-row form of the `Inert` rule, because a sweep that guesses
	// about a resource it could not ask about is worse than one that waits.
	nets, nerr := rt.NetworkList(ctx, LabelManaged+"=1")
	byNet := map[string]Handle{}
	if nerr == nil {
		for _, n := range nets {
			byNet[n.ID] = n // keyed by the id the read answers, the width the row stores
		}
	}

	byHandle := map[string]Handle{}
	for _, h := range handles {
		byHandle[h.ID] = h
	}
	liveIDs := map[string]bool{}
	liveNet := map[string]bool{}
	// takenDown remembers what THIS sweep already removed, so the orphan passes
	// below — which walk the lists read before the walk started — do not ask the
	// engine a second time for a resource it has already given up. A second
	// `network rm` answers not-found, and counting that as a failure would be this
	// file inventing a problem instead of reporting one.
	takenDown := map[string]bool{}
	// The claim is by the ROW, not by the row's state. `liveIDs` below has always
	// taken EVERY live row whatever its state, which is why a container was never
	// removable by the orphan pass while a row named it. The network side claimed
	// only `ready` ones, and that asymmetry made the sweep contradict itself: a
	// `provisioning` network row this very walk finishes three lines later was
	// absent from the snapshot, so the orphan pass removed the network the sweep
	// had just acquired and finished the pass holding a `ready` row pointing at
	// nothing — the exact lie EV12 exists to refuse, written by the same pass that
	// had just promised the opposite. Every live row claims its resource for the
	// length of this sweep: `provisioning` (which the walk finishes),
	// `tearing_down` (which it leaves to the teardown that owns it), `failed`
	// (which it removes, recording that in `takenDown` as it goes). One predicate,
	// no special case bolted onto the finish branch: a network is claimed the way
	// a container is.
	for _, row := range live {
		liveIDs[row.Handle] = true
		if row.Owner == "network" {
			liveNet[row.Handle] = true // the network's handle is the id the engine minted (W2-1)
		}
	}

	for _, row := range live {
		// The inventory is chosen by what the row owns: containers are asked about
		// with `List`, networks with `NetworkList`, both keyed by the engine's id —
		// the handle the row stores for either kind. Asking the wrong one is how a
		// live resource is reported gone, and a network is not a container.
		inventory := byHandle
		isNet := row.Owner == "network"
		if isNet {
			if nerr != nil {
				rep.Skipped++
				continue
			}
			inventory = byNet
		}
		switch row.State {
		case "ready":
			if _, ok := inventory[row.Handle]; ok {
				rep.Adopted++ // the row and the resource agree; said out loud, not assumed
				continue
			}
			// The resource vanished under us (engine restart without a restart
			// policy, an operator's `docker rm`, an OOM kill). The command that
			// needs it will ensure a fresh one; the row says failed because a
			// `ready` row pointing at nothing is a lie the next reader believes.
			// The sentence names the kind of thing it was, because a network row
			// being called a container is how this file's last bug hid.
			if _, _, err := led.MarkProvisionFailed(row.ID, "reconcile: the "+whoRow(row)+" "+row.Handle+" is gone"); err != nil {
				rep.Skipped++
				continue
			}
			rep.Failed++
		case "provisioning":
			// A crash between the row and the resource, or between the resource
			// and `ready`. Finish it if the engine has it, remove it if not.
			if row.Handle == "" {
				if _, _, err := led.MarkProvisionFailed(row.ID, "reconcile: the "+whoRow(row)+" was never created"); err != nil {
					rep.Skipped++
					continue
				}
				rep.Failed++
				continue
			}
			if _, ok := inventory[row.Handle]; ok {
				if isNet {
					// A network has no `start`: a row gets its handle from the ready
					// mark, so a `provisioning` network row whose resource the engine
					// has is the daemon having died between the create and the mark.
					// Finish it — asking the engine to `docker start` a network would
					// be a call nothing can answer.
					if _, _, err := led.MarkProvisionReady(row.ID, row.Handle); err != nil {
						rep.Skipped++
						continue
					}
					rep.Finished++
					continue
				}
				if err := rt.Start(ctx, row.Handle); err != nil {
					if _, _, err2 := led.MarkProvisionFailed(row.ID, "reconcile: start failed: "+err.Error()); err2 != nil {
						rep.Skipped++
						continue
					}
					rep.Failed++
					continue
				}
				if _, _, err := led.MarkProvisionReady(row.ID, row.Handle); err != nil {
					rep.Skipped++
					continue
				}
				rep.Finished++
				continue
			}
			if err := removeRowResource(ctx, rt, isNet, row.Handle); err != nil {
				rep.Skipped++
				continue
			}
			takenDown[row.Handle] = true
			if _, _, err := led.MarkProvisionRemoved(row.ID); err != nil {
				rep.Skipped++
				continue
			}
			rep.Removed++
		case "failed":
			// A row the driver settled as failed with the resource still there:
			// the health check failed, the create half-finished. A failed row is
			// not a reason to leave a container running — nobody will start it,
			// and "nothing left behind" is the promise a failed provision makes.
			if row.Handle == "" {
				rep.Skipped++
				continue
			}
			if _, ok := inventory[row.Handle]; !ok {
				rep.Skipped++
				continue
			}
			if err := removeRowResource(ctx, rt, isNet, row.Handle); err != nil {
				rep.Skipped++
				continue
			}
			takenDown[row.Handle] = true
			if _, _, err := led.MarkProvisionRemoved(row.ID); err != nil {
				rep.Skipped++
				continue
			}
			rep.Removed++
		default:
			rep.Skipped++ // `tearing_down` is settle-once in flight; the next sweep or teardown finishes it
		}
	}

	// The orphan networks. `List` answers containers only, so without this read an
	// orphan network is invisible and leaks for the life of the machine: every loop
	// that ever ran with services would leave one. Same rule as everywhere in this
	// file — ours only, by label, and only when no live row claims it (EV12), in
	// any state that row is settling through. The claim is by ID: the row stores
	// the id the engine minted, and the read answers it, which is the whole
	// content of the comparison (W2-1).
	if nerr != nil {
		// Not inert: the containers above were already reconciled against a
		// list the engine DID answer. The sweep says what it could not do, and the
		// network rows were left exactly as they were.
		rep.Warnings = append(rep.Warnings, "container: the engine would not list the networks, so none were reconciled: "+nerr.Error())
		if warn != nil {
			warn("container: the engine would not list the networks: " + nerr.Error())
		}
	}
	for _, n := range nets {
		if liveNet[n.ID] || takenDown[n.ID] {
			continue
		}
		// Removed by the ID the read answered — the same handle a row would carry.
		// `network rm` takes either, but the id is the one thing the read and the
		// ledger agree on, so it is the one this sweep speaks.
		if err := rt.NetworkRemove(ctx, n.ID); err != nil {
			rep.Failed++
			if warn != nil {
				warn("container: the loop network " + n.Name + " (" + n.ID + ") would not be removed: " + err.Error())
			}
			continue
		}
		takenDown[n.ID] = true
		rep.Removed++
	}

	// Orphans: labelled, live, and named by no row. Only ever OURS — the label
	// filter is what makes this loop safe on a shared engine, and the unlabelled
	// containers counted below are proof of what it refuses to touch.
	for _, h := range handles {
		if h.Labels[LabelManaged] != "1" {
			rep.Unlabelled++
			continue
		}
		if liveIDs[h.ID] || takenDown[h.ID] {
			continue
		}
		if err := rt.RemoveContainer(ctx, h.ID); err != nil {
			if warn != nil {
				warn("container: the orphan " + h.ID + " would not be removed: " + err.Error())
			}
			continue
		}
		takenDown[h.ID] = true
		rep.Removed++
	}
	return rep, nil
}

// removeRowResource removes one row's resource with the verb its OWNER owns:
// `network rm` for a network, `rm --force` for a container. `teardownRow` makes
// the same split for the same reason — a network is not a container, and a sweep
// that asks the engine in the wrong words removes nothing while reporting that it
// did.
func removeRowResource(ctx context.Context, rt Runtime, isNet bool, handle string) error {
	if isNet {
		return rt.NetworkRemove(ctx, handle)
	}
	return rt.RemoveContainer(ctx, handle)
}

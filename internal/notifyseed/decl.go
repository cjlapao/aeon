// Package notifyseed is the domain side of the seed engine for the notify
// class table (S58 T4, SV4): notify.events, an inert set whose records are
// the curated event classes — the kinds an emitter really produces and
// the byte-identical titles the old notifyHintFor switch carried (P2's
// characterization: T1's goldens pin what the resolver must keep).
//
// It imports internal/seed (the contract) and internal/store (the row
// shapes and the one write path); it imports no protocol, no daemon, no
// database/sql through the engine — the daemon adapts the applier (the
// blueprintseed precedent, including the function-mount: the package-
// global registry carries the refusing Adapter until the daemon mounts
// the store-backed one).
//
// The set is INERT (GuardAffecting false — a hint notifies, it permits
// nothing, so registry rule 6 is satisfied) and applies at daemon start.
// Sibling sets (notify.events.loopcontrol, .verify, .scheduler — SV4's
// parallel-worktree rule SD2) belong to their slices in their own files;
// this file seeds only what TODAY's emitters produce, all seven curated
// kinds plus the question kind and the variants the reason kinds name
// (T0 measured: the switch had grown to seven arms; a curated hint that
// went silent would be the P2 lie this whole task pins shut).
package notifyseed

import "github.com/cjlapao/aeon/internal/seed"

// SetID is the set's stable name. It is declared here, once (SD2).
const SetID = "notify.events"

// init declares the set (SD2, SD12): notify.events, class inert — it
// applies at daemon start and by aeon init and widens nothing. The
// store-backed applier is the daemon's function-mount in NewSeedEngine
// (the blueprint/policy/watchdog precedent); the global registration
// carries the refusing Adapter until then. A broken declaration panics —
// the init-time stance.
func init() {
	if err := seed.Register(
		seed.Set{
			ID:      SetID,
			Class:   seed.ClassInert,
			Version: SetVersion(),
			Records: Source,
		},
		Adapter{},
	); err != nil {
		panic("notifyseed: refusing to register the set: " + err.Error())
	}
}

package policyseed

import (
	"github.com/cjlapao/aeon/internal/seed"
)

// SetID is the set's identity — the string the ledger, the events, the
// registry and every door name the corpus by (S45 T12, PM15). The
// "policy.corpus" literal lives HERE, once, in the domain's declaration;
// the doors reach it through this name.
const SetID = "policy.corpus"

// Declaration is the set the daemon mounts (S45 T12): class GUARD, because
// applying it writes `allow` rows (SD5) — never inert, never automatic, the
// engine's class gate is D4's structural proof. Its version is the maximum
// of its records', so bumping a record's version bumps the set's with it
// (SD2), and a downgrade against the ledger fails the whole apply (SD6).
//
// Why this is a function the daemon calls rather than an init() that
// registers into the package-global registry, where agentseed's declaration
// registers itself: the global registry is read by tests that build their
// own engines with an agents-only appliers map (seed.NewEngine refuses any
// registry set it cannot apply), and a domain may not widen the global
// registry from a distance and brick those builds from across the module.
// The daemon's NewSeedEngine composes DefaultRegistry() with this
// declaration and mounts the store-backed applier beside the agents one —
// the one engine cmd/aeon and serve hold does carry the corpus set; a test
// engine carries exactly the sets its test declared.
func Declaration() seed.Set {
	return seed.Set{
		ID:      SetID,
		Class:   seed.ClassGuard,
		Version: maxRecordVersion(),
		Records: Source,
	}
}

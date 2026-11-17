package blueprintseed

import "github.com/cjlapao/aeon/internal/seed"

// init declares the set (SD2, SD12): blueprints.standard, class inert —
// it applies at daemon start (after adoption, before loops are accepted)
// and by aeon init, never widening anything (SD5: GuardAffecting false is
// what lets it be inert at all — registry rule 6). The store-backed
// applier is the daemon's function-mount in NewSeedEngine (the policy/
// watchdog/mcp precedent, recorded by policyseed/decl.go: a domain may
// not widen the package-global registry from a distance); the global
// registration carries the refusing Adapter until then. A broken
// declaration panics — the init-time stance.
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
		panic("blueprintseed: refusing to register the set: " + err.Error())
	}
}

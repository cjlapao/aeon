package scaffoldseed

import "github.com/cjlapao/aeon/internal/seed"

// init declares the set (SD2, SD12): templates.scaffold, class inert — it
// applies at daemon start and by aeon init, and never widens anything
// (GuardAffecting false is what lets it be inert at all — registry rule
// 6). A template row is a pick-by menu: nothing scaffolds until a person
// runs the person-door action T4 ships, into a target the same person
// names. The store-backed applier is the daemon's function-mount in
// NewSeedEngine (the retroseed ruling applies verbatim: a domain may not
// widen the package-global registry from a distance); a broken declaration
// panics — the init-time stance.
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
		panic("scaffoldseed: refusing to register the set: " + err.Error())
	}
}

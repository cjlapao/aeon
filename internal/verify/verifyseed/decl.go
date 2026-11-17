package verifyseed

import "github.com/cjlapao/aeon/internal/seed"

// init declares the set (SD2, SD12): verify.templates, class inert —
// it applies at daemon start and by aeon init, and never widens anything
// (SD5: GuardAffecting false is what lets it be inert at all — registry
// rule 6). A template does nothing until a person applies it (VG21), so
// the start apply writes candidate rows and changes no verdict. The
// store-backed applier is the daemon's function-mount in NewSeedEngine
// (the policy/watchdog/blueprint/tool precedent); the global registration
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
		panic("verifyseed: refusing to register the set: " + err.Error())
	}
}

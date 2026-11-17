package daemon

// settings.go — S44 T4: the ONE constructor the rest of the program uses to
// build the shared *config.Store, plus the seam the protocol door (T5) will
// hang on.
//
// NewSettings takes the store, the production defaults (config.DefaultRegistry,
// os.LookupEnv, the UTC clock — applied by config.NewStore for nil option
// fields), and returns the shared instance. There is no package-level instance
// and no sync.Once (CS7, and the "same process or not" proof): a hidden global
// would hide a second instance in a test and defeat the acceptance criterion
// T4 exists to give. A storeless daemon gets a nil instance (V13: absent, not
// registered-and-refusing), not a default.

import (
	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/store"
)

// NewSettings builds the one *config.Store the daemon and its in-process
// consumers (T10) read. It is the only constructor the program uses: the
// production defaults are applied by config.NewStore for nil option fields
// (Registry → DefaultRegistry, LookupEnv → os.LookupEnv, Now → the UTC
// clock), so a caller that passes config.Options{} gets the real environment
// and wall clock. A nil store returns nil (V13: absent, not
// registered-and-refusing) — the daemon then leaves settings.* unregistered
// rather than answering a refusal.
func NewSettings(st *store.Store, opt config.Options) *config.Store {
	if st == nil {
		return nil
	}
	return config.NewStore(newSettingsRepo(st), opt)
}

// --- the protocol door (T5) -------------------------------------------------
//
// T5 adds the protocol door: a protocol.SettingsService interface, a
// *config.Store-backed adapter (like telemetryReadStore), the five settings.*
// method handlers, the params/result types, and RegisterSettings (nil-safe,
// V13 — a nil store leaves the methods unregistered). T4 does not import that
// interface (it does not exist yet), so the adapter's method set is declared
// here as a comment, not code, and this task compiles alone.
//
// The daemon's wiring line (T5's) will be, beside the S33 telemetry block in
// daemon.go:
//
//	table.Settings = opts.Settings
//	table.RegisterSettings()
//
// — one shared *config.Store, both doors (P34's same-value rule, S30's graph
// precedent: an identity test, not a convention). The audit actor the handlers
// pass to Set/Unset is the daemon's "user:<uid>" (table.ActorFor: d.peer's
// uid; an unresolvable peer is uid 0 → "user:0" — never an empty or a
// fabricated identity, T3 refuses empty).
//
// CS12 (the setting.changed event) is T5's to emit: the emission site is the
// settings handler, where a changed=true result and the daemon's transport
// coexist. T4's files hold no transport, so the event is not built here — it
// is recorded in Notes with the staleness window it names: the gap between a
// changed Set and the next client list. A client that lists immediately after
// a change sees it (the read is live); a client that lists later may not,
// until T5's event arrives.

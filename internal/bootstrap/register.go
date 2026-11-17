// Package bootstrap is Aeon's wiring point: the one place that names which
// framework services this daemon runs.
//
// # Why this file exists at all
//
// Aeon imports github.com/cjlapao/go-backend-scaffolding/framework as a pinned
// module (B1). The scaffolding's own wiring lives in its internal/bootstrap,
// and internal/ does not cross a module boundary — so Aeon cannot reuse it and
// must write this.
//
// That constraint is a gift. It makes B8 structural rather than a discipline:
//
//	Registration is an ALLOW-LIST. Aeon names what it wants.
//	It never subtracts from somebody else's defaults.
//
// A deny-list fails open on every upstream addition — a service the framework
// adds in v0.11.0 would arrive registered, and nothing would say so. An
// allow-list makes a new framework service INERT by default: it fails safe on
// every future upgrade, not only the ones somebody remembered to audit. In a
// program that runs shell commands, every registered service is audit surface.
//
// # The rule for adding a service
//
// Uncomment or add one line here, then update services.golden in the same
// commit. register_test.go compares the two and fails when they disagree, so
// widening the daemon is always a reviewable diff and never a side effect of
// `go get -u` (B11).
package bootstrap

import (
	"context"
	"sort"

	"github.com/cjlapao/go-backend-scaffolding/framework/api"
	"github.com/cjlapao/go-backend-scaffolding/framework/core/logging"
	"github.com/cjlapao/go-backend-scaffolding/framework/core/system"
	system_interfaces "github.com/cjlapao/go-backend-scaffolding/framework/core/system/interfaces"
	"github.com/cjlapao/go-backend-scaffolding/framework/database"
	db_interfaces "github.com/cjlapao/go-backend-scaffolding/framework/database/interfaces"
	db_service "github.com/cjlapao/go-backend-scaffolding/framework/database/service"
	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/store"
)

// Pins are code-level decisions about a service's enabled state that outrank
// configuration everywhere it is read. Passed to system.Initialize.
//
// They exist because "registered" and "running" are different questions, and
// config is the wrong place to answer the second one for these two.
//
// S19 kept the `api` pin's STRENGTH and moved the exception. The default
// answer is still ForceOff, and the framework's Enabled:true-on-:8080
// default is answered by a Force in one direction or the other —
// configuration can never open this port at all. The inversion to ON exists
// only behind WithHTTP(), which serve.go passes when, and only when, an
// explicit `--http <address>` was typed AND a key resolved (a keyless
// address was refused at T2's gate long before any pin is read). Off by
// default remains a code property; a pin is a security decision, so the
// flip carries its own test as hard as the original ForceOff (register_test).
type pinConfig struct{ http bool }

// PinOption configures Pins. With no option, the plain answer: api OFF.
type PinOption func(*pinConfig)

// WithHTTP inverts the api pin to ON. Exactly one caller in the tree,
// cmd/aeon's serve path, behind both conditions named above.
func WithHTTP() PinOption { return func(c *pinConfig) { c.http = true } }

func Pins(opts ...PinOption) []system.ServicePin {
	var cfg pinConfig
	for _, o := range opts {
		o(&cfg)
	}
	apiPin := system.ForceOff("api")
	if cfg.http {
		apiPin = system.ForceOn("api")
	}
	return []system.ServicePin{
		// localrpc is the ONLY transport carrying the protocol (P25). The
		// framework defaults it off — correct for a general-purpose framework,
		// wrong for Aeon, where a daemon with no endpoint starts, reports
		// healthy, and can never be spoken to. Pinning it on deletes that
		// state rather than documenting it.
		system.ForceOn("localrpc"),

		// api — the narrow surface's listener (S19), see the block above.
		apiPin,

		// database is pinned ON. The daemon has no useful behaviour without
		// memory: a loop that cannot persist a step cannot suspend, cannot
		// resume and cannot be recovered (P2, §3a). Leaving it to
		// configuration would make "a daemon that starts, reports healthy and
		// forgets everything" a reachable state.
		system.ForceOn("database"),
	}
}

// RegisterOption configures Register.
type RegisterOption func(*registerConfig)

type registerConfig struct {
	narrow *narrowWiring
}

type narrowWiring struct {
	key    narrow.Key
	caller func() narrow.Caller
}

// narrowCaller is nil-safe: an unconfigured boot has no daemon dispatcher to
// reach, and the mounting answers those requests as a transport condition
// naming the fact (O5) rather than panicking on a nil.
func (r *registerConfig) narrowCaller() func() narrow.Caller {
	if r.narrow == nil {
		return nil
	}
	return r.narrow.caller
}

// WithNarrow hands the HTTP mounting its key (for the api plugin's
// TokenValidator — the key itself never leaves the narrow package's Key
// type, I6/§4c) and a LATE getter for the daemon dispatcher, because the
// dispatcher exists only after the daemon is constructed and the registry —
// like the routes it serves — must be in place before Start.
func WithNarrow(key narrow.Key, caller func() narrow.Caller) RegisterOption {
	return func(c *registerConfig) { c.narrow = &narrowWiring{key: key, caller: caller} }
}

// Register wires every framework service Aeon runs, in dependency order.
//
// The foundational three — app, config, logging — are already wired by
// system.Initialize before this is called, so they are deliberately absent.
//
// `st` is S4's store, already open. It is a PARAMETER rather than something
// this function opens, because the ordering matters in two directions: the
// store must exist before the database service can be handed its handle, and
// the daemon must have the same store so `seq` comes from one allocator (DB20).
// `serve` owns that ordering and passes the result here.
//
// A nil store registers no database service, which is what `--print-services`
// does: it answers "what would this daemon run" WITHOUT opening anything,
// because the reason to ask is usually that you do not want it running yet.
//
// S19's option, WithNarrow, is the OTHER half of the pin's story: the
// TokenValidator the pinned-on listener will gate every secured route with,
// and the mounting service whose routes it will mount. Without the option —
// the default, and the only path `--print-services` takes — api registers
// inert with no validator and the narrow mounting still registers (the
// registered SET must not depend on boot flags: B11's golden compares
// sets, and a flag that widened it would make the golden a per-run answer).
// The routes behind it can only ever mount when the api service is running,
// and running requires WithHTTP(), which requires a key.
func Register(svc system_interfaces.SystemServiceInterface, st *store.Store, opts ...RegisterOption) {
	var r registerConfig
	for _, o := range opts {
		o(&r)
	}

	// localrpc — the daemon's reason to exist.
	//
	// Registered first and alone because it is the ONLY transport that carries
	// the protocol (P25). It binds and starts accepting during Init rather than
	// Serve, so the endpoint is reachable while anything registered after it is
	// still initialising — which is exactly the window a daemon most needs to be
	// inspectable.
	svc.Register(localrpc.New())

	// api — the narrow HTTP surface (S19), REGISTERED always, RUNNING only by
	// explicit flag (Pins/WithHTTP).
	//
	// framework v0.10.1 added this as a real registerable server; before it,
	// the scaffolding's only HTTP server was internal/ and unreachable, so S19
	// was going to have to build one. It no longer does — Q3 answered: reuse,
	// never a second listener in one binary.
	//
	// It takes the registry because it resolves route contributors from it. It
	// declares only config and logging as dependencies — deliberately no
	// database — so registering it does not drag a store into the boot.
	//
	// The validator arrives with WithNarrow; with none installed the
	// framework's middleware refuses every route that requires auth, which
	// fails CLOSED — the safe direction for a missing piece.
	apiPlugin := api.New(svc)
	if r.narrow != nil {
		apiPlugin = apiPlugin.WithTokenValidator(narrow.NewValidator(r.narrow.key))
	}
	svc.Register(apiPlugin)

	// narrow — the HTTP mounting's route contributor (S19 T4). Registered
	// UNCONDITIONALLY so the set is flag-independent (see above); it owns no
	// listener and answers nothing until the api plugin pulls HTTPRoutes()
	// during its Serve, which requires the pin's ON branch. The caller is
	// late-bound: the daemon dispatcher exists after Start, and requests
	// before it exists answer a transport error naming the condition.
	svc.Register(narrow.NewHTTPService(narrow.Late(r.narrowCaller()), narrow.Actions()))

	// database — S4's store, and it IS the framework's service.
	//
	// # Registered in INJECTED mode, and why that is not a workaround
	//
	// `database.WithDB(handle)` is a published construction mode: injected
	// mode skips connect and config and runs the schema lock, the migration
	// runner and the store lifecycle on the handle it is given. Everything
	// else stays the framework's — the config contract (SQLiteConfig,
	// PoolConfig, fed by internal/appenv), the `_migrations` ledger, and the
	// `Store` interface Aeon's tables implement.
	//
	// Aeon opens the handle for ONE MEASURED REASON. The framework's own
	// SQLite arm opens `modernc.org/sqlite` — a pure-Go translation, which
	// cannot host a C extension:
	//
	//	sql.Open("sqlite", …)   →  select vec_version()  →  no such function
	//	its composed DSN        →  journal_mode=delete, foreign_keys=0
	//
	// Aeon needs mattn/go-sqlite3 (B3: CGO is on, sqlite-vec is a C extension,
	// and S22 reads it), WAL, and foreign keys. Through a mattn handle,
	// measured: vec_version answers v0.1.6, a vec0 table creates, the pragmas
	// are in force and a cascade actually fires.
	//
	// An earlier version of this file REFUSED the service, arguing Aeon needed
	// its own connector. That was wrong and the user corrected it: use the
	// framework's contracts and expand what is missing. The gap turned out to
	// be exactly one thing — the driver — and this is the seam for it.
	// The service REFUSES TO START with no store provider installed —
	// "database stores not configured — call ConfigureStores before init",
	// found by registering the service and nothing else. It is the right
	// refusal: a database service managing no stores is a connection nobody
	// asked for. Aeon's store is the store it manages (internal/store's
	// framework.go), and this is the compiler's only check that the contract
	// is satisfied.
	if st != nil {
		db_service.ConfigureStores(func() []db_interfaces.Store {
			return []db_interfaces.Store{st}
		})
		svc.Register(database.New(database.WithDB(st.GormDB())))
	}

	// ── Considered, and arriving with the slice that needs it ──────────────
	//
	// Each of these is one uncommented line plus a services.golden entry. They
	// are listed rather than registered because a service that is present "so
	// it is there when we need it" is audit surface nobody chose today.
	//
	//   health        S23 — doctor's probe surface
	//   osutils       S23 — path reporting (localrpc resolves paths via a pure
	//                       function, so it needs no initialised service here)
	//   encryption    S14 — at-rest credential handling
	//   idempotency   S16 — the forge actions' re-run safety
	//   validation    S8  — blueprint validation, if it fits the framework shape
	//   ratelimit     S19 — the narrow HTTP surface
	//
	// ── Refused, with the reason ───────────────────────────────────────────
	//
	//   redis, jobs, metrics, tracing, profiling, swagger, websocket,
	//   object_storage, media, email, sms, notification, session, device,
	//   i18n, cache, vaults, flipt, analytics, consent, feedback, honeytrap,
	//   otp
	//                 These defend or instrument a public multi-tenant endpoint.
	//                 Aeon has one user, one daemon, and a unix socket whose
	//                 access control is filesystem permissions (P26).
	//
	// ── Still not available from the framework ────────────────────────────
	//
	//   api_keys, role, claim
	//                 These remain in the scaffolding's internal/ and do not
	//                 cross the module boundary. S19 needs ONE key for ONE user,
	//                 so it supplies its own validator through
	//                 api.New(svc).WithTokenValidator(...) rather than waiting
	//                 for a service built for multi-tenant claims.
}

// RegisteredNames reports what Register would register, sorted, without
// registering anything.
//
// It exists so `aeon --print-services` can answer offline. The reason to ask
// "what would this daemon run" is usually that you do not want it running yet
// — so an implementation that had to boot the system to answer would be no use
// at the moment the question is asked.
//
// It collects through the same Register call the daemon uses, so the two cannot
// disagree: there is no second list.
func RegisteredNames() []string {
	c := &nameCollector{}
	Register(c, nil)
	// The database service is registered only when a store is open, and this
	// function deliberately opens nothing — so its name is added here, with
	// the reason, rather than by passing a store nobody asked for.
	//
	// The alternative (open a store to answer "what would you run") would
	// create a database as a side effect of a question about configuration,
	// which is the opposite of what the question is for.
	c.names = append(c.names, "database")
	sort.Strings(c.names)
	return c.names
}

// nameCollector satisfies the registry interface and only remembers names.
//
// The embedded nil interface means anything Register touches BEYOND Register
// panics rather than quietly succeeding — which is what keeps this honest if
// Register ever grows a second call.
type nameCollector struct {
	system_interfaces.SystemServiceInterface
	names []string
}

func (c *nameCollector) Register(s system_interfaces.Service) {
	c.names = append(c.names, s.Name())
}

// Shutdown releases what Register started, in reverse.
//
// Only services holding something the process cannot drop on exit belong here.
// localrpc holds a socket (POSIX) or a named pipe (Windows): an endpoint left
// behind makes the next start fail with "address already in use", which reads
// as a mystery rather than as the previous run not having cleaned up.
func Shutdown(ctx context.Context) {
	// api drains itself on context end (the framework bounds it at 15s) and is
	// pinned off anyway, so only localrpc needs releasing here.
	if err := localrpc.Shutdown(ctx); err != nil {
		logging.Errorf("localrpc shutdown failed: %v", err)
	}
}

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	app_models "github.com/cjlapao/go-backend-scaffolding/framework/core/app/models"
	"github.com/cjlapao/go-backend-scaffolding/framework/core/appctx"
	"github.com/cjlapao/go-backend-scaffolding/framework/core/system"
	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"

	"github.com/cjlapao/aeon/internal/appenv"
	"github.com/cjlapao/aeon/internal/bootstrap"
	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/endpoint"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/secrets"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/skills"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/vault"
)

const shutdownGrace = 30 * time.Second

// serve runs the daemon.
//
// The ordering of the first three steps is not stylistic — each must happen
// before `system.Initialize`, for a different reason:
//
//  1. appenv  — the framework's config service reads the environment as it
//     initialises, so Aeon's defaults have to be there first.
//  2. secrets — same constraint, and a key set afterwards fails as
//     "cursor.signing_key is not set", which blames configuration rather
//     than ordering.
//  3. endpoint — localrpc binds during Init, so a second instance would fail
//     inside the framework with a bind error. Aeon's own refusal has to come
//     first to say anything useful about it.
//
// S4 adds a fourth, AFTER system.Initialize and BEFORE Register: the store
// opens, and its handle is what the framework's database service is given. The
// ordering is not stylistic either — see openStore below.
func serve() error {
	return serveWithRecovery(serveSettings{})
}

// defaultNotifyBuffer gives each client connection a deeper notification queue than the transport's 256. The app is one
// connection carrying every screen's events, a queue that is full drops what does not fit, and the app re-reads when it
// sees the loss; a deeper queue makes the loss rarer, not impossible. An explicit setting is left alone.
func defaultNotifyBuffer() {
	if os.Getenv("LOCALRPC_NOTIFY_BUFFER") == "" {
		_ = os.Setenv("LOCALRPC_NOTIFY_BUFFER", "4096")
	}
}

// serveSettings carries what `aeon serve` decided BEFORE the process boots.
// S19 T2 adds the two narrow-surface fields; T4 binds them. They are parsed
// here, before system.Initialize, because a pin is a boot-time decision and
// the keyless refusal must stop the process before it can own anything —
// a port, a file handle, a listener.
type serveSettings struct {
	noRecover bool
	// httpAddr is the explicit bind address for the narrow HTTP surface.
	// Empty means NO listener — the daemon binds no port by default (N15),
	// and the address is typed as an address, not a port, so exposing past
	// the host is something someone typed.
	httpAddr string
	// key is the narrow-surface key, resolved out of band (--http-key-file
	// or AEON_HTTP_KEY — never a flag value). Its type prints <redacted>,
	// so no startup path can echo it (I6).
	key narrow.Key
	// noBackup is --no-backup (S62 T5, BK18): migrate on start without the
	// pre-migration backup. The flag's value, decided before the process
	// boots, like the rest of this struct.
	noBackup bool
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	noRecover := fs.Bool("no-recover", false, "report recovery cases and leave steps running")
	// S62 T5 (BK18): the pre-migration safety net's one switch. A flag,
	// never an environment variable — the environment is set by whatever
	// starts the process, and a migration that loses its copy to a stray
	// variable is a migration that lost it to luck. Scoped to the
	// pre-migration backup alone: it never reaches the schedule (T6).
	noBackup := fs.Bool("no-backup", false, "migrate on start without taking the pre-migration backup (records a skipped row)")
	// S19 T2/T4: the narrow HTTP surface's two deliberate knobs. The KEY is
	// never a flag value — only a PATH (O1): a value in argv is in the
	// process list and in shell history, and that failure is silent and
	// permanent. The address is a whole address, not a port (N15).
	httpAddr := fs.String("http", "", "bind the narrow HTTP surface to this address (requires a key)")
	keyFile := fs.String("http-key-file", "", "path to the narrow-surface key file (or set "+narrow.KeyEnv+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("serve takes no positional arguments")
	}
	// The refusal runs BEFORE anything is claimed — before the endpoint,
	// before the store, before the framework sees argv. An address without
	// a key is a start that must not happen (N15, D7): not a warning, and
	// never a generated default.
	key, err := narrow.ResolveKey(*keyFile)
	if err != nil {
		return err
	}
	if err := narrow.CheckKeyless(*httpAddr, key); err != nil {
		return err
	}
	// The framework parses process flags during Initialize. These flags
	// belong to Aeon, so remove them from argv after Aeon has handled them.
	originalArgs := os.Args
	if len(os.Args) > 2 {
		os.Args = os.Args[:2]
	}
	defer func() { os.Args = originalArgs }()
	return serveWithRecovery(serveSettings{noRecover: *noRecover, httpAddr: *httpAddr, key: key, noBackup: *noBackup})
}

func serveWithRecovery(settings serveSettings) error {
	defaultNotifyBuffer()
	noRecover := settings.noRecover
	// S19 T4 — the framework reads its config ONCE, at system.Initialize
	// ("anything the framework reads at init runs before system.Initialize"),
	// so the api listener's coordinates are environment values set HERE, from
	// the typed flag. The names are the framework's own, measured on
	// framework@v0.10.1 rather than derived: `bind_address` carries an
	// explicit env tag SERVER_BIND_ADDRESS (not the path-generated
	// SERVER_API_BIND_ADDRESS — a framework quirk worth knowing before
	// debugging a mis-bound port), and `port` takes the generated name
	// SERVER_API_PORT. The flag OUTRANKS the ambient environment here on
	// purpose: the whole of N15 is that someone TYPED this address.
	pins := bootstrap.Pins()
	if settings.httpAddr != "" {
		host, port, err := splitHTTPAddr(settings.httpAddr)
		if err != nil {
			return err
		}
		if err := os.Setenv("SERVER_BIND_ADDRESS", host); err != nil {
			return err
		}
		if err := os.Setenv("SERVER_API_PORT", port); err != nil {
			return err
		}
		pins = bootstrap.Pins(bootstrap.WithHTTP())
	}
	runtime.ConfigureDebugCrashAfterTool(os.Getenv("AEON_DEBUG_CRASH_AFTER_TOOL") == "1")
	if err := appenv.Apply(); err != nil {
		return err
	}
	if err := secrets.EnsureCursorSigningKey(); err != nil {
		return err
	}
	if err := endpoint.Claim(); err != nil {
		return err
	}

	ctx := appctx.NewContext(context.Background())

	svc, diag := system.Initialize(ctx,
		system.WithApp(app_models.ApplicationInfo{
			Name:      "Aeon",
			Slug:      "aeon",
			Version:   Version,
			Author:    "Carlos Lapao",
			License:   "MIT",
			BuildDate: BuildTime,
			GitCommit: GitCommit,
		}),
		system.WithForcedServices(pins...),
	)
	// Diagnostics carry warnings as well as errors, so HasErrors is the gate
	// rather than a nil check: a non-nil diag with only warnings is a normal,
	// successful start and must not be fatal.
	if diag != nil && diag.HasErrors() {
		return fmt.Errorf("initialize system: %s", diag.GetSummary())
	}

	// The store, before anything that needs memory.
	//
	// # Why it opens HERE and not inside the framework's database service
	//
	// Aeon hands the framework an already-open handle (database.WithDB), for
	// one measured reason: the framework's own SQLite arm is
	// modernc.org/sqlite, which cannot host sqlite-vec. See
	// internal/bootstrap/register.go for the measurement.
	//
	// It also has to happen before `daemon.New`, because the daemon's event log
	// takes the store as its `seq` allocator (DB20) and attaches it at
	// construction — which is what makes `seq` survive a restart without any
	// moment where a live subscription sees the numbering change.
	st, err := store.Open(storeOpenOptions(settings.noBackup, "daemon"))
	if err != nil {
		// Fatal, and loudly. A daemon that starts without memory is a daemon
		// that accepts work it cannot suspend, resume or recover — so the
		// failure belongs here, naming the path, rather than at the first step
		// somebody runs. A pre-migration backup failure names its reason,
		// its dir and --no-backup (BK6), and the store is untouched.
		return fmt.Errorf("open the store: %w", err)
	}
	defer func() { _ = st.Close() }()

	// S48 T4 (BP3): adoption is FIRST — the files an older installation
	// left in the state directory are carried into rows once, per file,
	// per transaction, idempotent by hash; invalid files get a ledger row
	// and a spoken line, and a file is NEVER written, renamed, or deleted.
	// Then the seed set (the daemon's arrival seed inside New today; the
	// `blueprints.standard` set lands at T5) — and only after this call
	// returns are loops accepted, which happens at daemon construction
	// below. An adoption failure keeps the daemon down: accepting loops on
	// a half-carried store is a state nobody can reason about.
	if err := blueprintStartup(func() error {
		rep, aerr := adoptFilesStoreDir(st)
		if aerr != nil {
			return aerr
		}
		ctx.Log().Infof("%s", rep.Line())
		for _, n := range rep.Refused {
			ctx.Log().Infof("blueprints: refused %s — %s (aeon doctor reports the ledger)", n.Name, n.Reason)
		}
		for _, n := range rep.Skipped {
			ctx.Log().Infof("blueprints: skipped %s — %s", n.Name, n.Reason)
		}
		return nil
	}, nil, nil); err != nil {
		return fmt.Errorf("blueprint adoption: %w", err)
	}

	// The narrow dispatcher is LATE: the registry and its routes must exist
	// before Start (the api plugin pulls contributors during its Serve), and
	// the daemon dispatcher exists only after it. The getter is handed down
	// now; the daemon is set below, and requests in the gap answer honestly
	// rather than falsely (narrow.Late).
	var narrowDisp lateDaemon
	var regOpts []bootstrap.RegisterOption
	if settings.httpAddr != "" {
		regOpts = append(regOpts, bootstrap.WithNarrow(settings.key, narrowDisp.get))
	}
	bootstrap.Register(svc, st, regOpts...)

	if err := svc.Start(ctx); err != nil {
		return fmt.Errorf("start services: %w", err)
	}

	// DEF-030 (T13's daemon surface): the 90-day policy_event cap fires ONCE
	// per boot here — its other trigger is the demand of `aeon policy
	// events` (T12), and its forbidden trigger is the record path (PL11).
	// Bounded by construction (one statement), and a failure is spoken by
	// the helper, never fatal: the daemon must start with or without its
	// housekeeping.
	daemon.TrimPolicyEventsAtStartup(ctx, st, ctx.Log(), time.Now())

	// Registered AFTER Start, because the service must be running to accept a
	// method — localrpc answers CodeServerNotReady for anything asked before
	// the Serve phase, which is exactly the window this sits behind.
	//
	// S1 registered one handler here. S2 replaces it with the method table:
	// five methods, negotiation, subscriptions and the event stream. The
	// transport knowledge stops at `internal/daemon` — `internal/protocol`
	// imports nothing transport-related (T11), which is what lets S19 mount the
	// same table on HTTP and MCP-stdio.
	// Routes come from the environment until S23 has configuration. A provider
	// with no key gets no route, so `ask` lists what IS configured rather than
	// failing with a 401 from a provider nobody meant to call.
	routes := daemon.RoutesFromEnvironment()

	dopts := daemon.Options{
		DaemonVersion: Version,
		BuildTime:     BuildTime,
		GitCommit:     GitCommit,
		Logger:        ctx.Log(),
		Routes:        routes,
		Models:        daemon.ModelClient(),
		// S4: the store is the `seq` allocator and the store methods' backing.
		Store: st,
	}
	// S14 T4 — agent services are wired at the ENTRY POINT and nowhere else:
	// this call is why cmd/aeon imports internal/agentsvc, and the only
	// reason it does (AS11's deletability; the same rule S12 kept for mcp).
	// An unwired daemon still serves every other verb and says skill lanes
	// cannot run; this daemon can ask signed agents for their skills.
	if err := wireAgentServices(st, &dopts); err != nil {
		return err
	}
	// S34 T3/T4 — the trust bench's MCP seam: config truth and the
	// operator-triggered probe, wired at the entry point for the same
	// deletability reason as wireAgentServices above.
	if err := wireMCPServices(st, &dopts); err != nil {
		return err
	}
	// S46 T3 (MG12) — the step offer reads the TABLES per offer: one
	// rebuild per BuildTools call, no mcp_version counter, no cache, so a
	// write through any door is visible at the next offer without a
	// restart. Production never wired StepMCP before this (the S36 T6
	// named gap; S46 T0's pre-flight found it): with empty tables the
	// bundle is empty and the step is byte-identical to today (MD8),
	// which is what makes the wire safe to land here. The offer is judged
	// against the set the STEP froze (D12) — DecodeFrozenSet's Reject
	// means "judge nothing", and every candidate then withholds
	// deny-leaning (MC10); the live table is never a fallback.
	tools.SetExternalToolAddress(mcpQualifiedAddress)
	dopts.StepMCP = func(ctx context.Context, agent string, frozen runtime.FrozenManifest, at time.Time) (*daemon.MCPBundle, error) {
		ps, rej := daemon.DecodeFrozenSet(frozen.Entries)
		if rej != nil {
			ps = nil
		}
		return buildMCPBundle(ctx, agent, ps, at)
	}
	// S35 T4 — agent.enable/disable/revoke: plain socket writes (brief §3's
	// line), calling the same cmd/aeon functions the CLI verbs call, wired
	// at the entry point for the same deletability reason as
	// wireAgentServices above.
	if err := wireAgentTransitionServices(st, &dopts); err != nil {
		return err
	}
	// S43 T7 — the agent-management family (create/import/export/duplicate/
	// delete/edit/view/migrate): the daemon's primary surface (P34), wired at
	// the entry point for the same deletability reason as wireAgentServices
	// above (cmd/aeon is the only package that may import internal/agentmgr).
	if err := wireAgentManagementServices(st, &dopts); err != nil {
		return err
	}
	// S44 T4 — the settings accessor: the ONE shared *config.Store, built here
	// at the entry point and held by the daemon (NewSettings: the only
	// constructor the program uses). A storeless daemon (st == nil) gets a nil
	// instance (V13: absent, not registered-and-refusing), and the settings.*
	// methods stay unregistered. T5 wires the door onto the table.
	dopts.Settings = daemon.NewSettings(st, config.Options{})
	// S43-extra T6 — the one seed engine the daemon holds: the bridge over
	// the store, the agentseed applier, and the inert/guard boundary (SD5).
	// Nil-safe: a storeless daemon (st == nil) gets nil, and the seed.*
	// methods stay unregistered (V13: absent, not registered-and-refusing).
	// A build failure is spoken, not fatal — the daemon starts, the sets
	// stay absent (seed.status shows them), and dispatch refuses a missing
	// agent by name (T5), so the failure is loud where it matters.
	if st != nil {
		if seedEngine, serr := daemon.NewSeedEngine(st, seed.Options{}, mcpSeedValidator(st)); serr != nil {
			ctx.Log().Infof("seed: the engine could not be built — the seed doors stay absent: %v", serr)
		} else {
			dopts.Seed = seedEngine
		}
	}
	// S65 T4 — the Agent Skills catalog, built at the ENTRY POINT: the ONE
	// *skills.Service over the two roots (the operator's ~/.agents/skills
	// and Aeon's managed folder) that the socket, the CLI client and the
	// bench page all read through (P34). Disk truth, not store truth: a
	// storeless mount still has a catalog. The startup scan IS the
	// application-start read the slice promises — the first pass fills the
	// one cache, and its summary is SPOKEN, never fatal: a missing or
	// unreadable root yields an empty catalog and a logged sentence, not a
	// daemon that refuses to start (three-state honesty: absent roots are
	// reported as absent, not as errors and not as "nothing exists").
	skillSvc, skillErr := skills.NewService(skills.Options{})
	if skillErr != nil {
		// The roots themselves could not be resolved (no HOME, no XDG base
		// to fall back on). That is not a reason to refuse to boot — the
		// catalog is absent, spoken, and every skills.* door stays out of
		// the table exactly as on a no-catalog mount (V13).
		ctx.Log().Infof("skills: no catalog — the roots could not be resolved, and the skills doors stay absent: %v", skillErr)
		skillSvc = nil
	}
	dopts.Skills = skillSvc
	if skillSvc != nil {
		if scanRes, scanErr := skillSvc.Scan(false); scanErr != nil {
			ctx.Log().Infof("skills: the startup scan failed — the catalog answers from the cache or re-reads on demand: %v", scanErr)
		} else {
			ctx.Log().Infof("skills: %d valid, %d with warnings, %d invalid, %d shadowed across %s and %s",
				scanRes.Summary.Valid, scanRes.Summary.Warn, scanRes.Summary.Invalid, scanRes.Summary.Shadowed,
				scanRes.UserRoot, scanRes.ManagedRoot)
		}
		// S66: the watcher runs for the daemon's own lifetime — its ctx
		// IS the stop. A watcher that cannot start is logged, never
		// fatal: an unwatched catalog is still a catalog, and `validate`
		// remains the manual door (S65).
		if wErr := skillSvc.StartWatch(ctx); wErr != nil {
			ctx.Log().Infof("skills: the watcher did not start (%v) — the catalog still answers, refreshed by skills.validate", wErr)
		} else if wRes, wErr2 := skillSvc.Scan(false); wErr2 == nil {
			if wRes.Watching {
				ctx.Log().Infof("skills: watching both roots — dropped-in folders are noticed without a restart%s", watchErrSuffix(wRes.WatchError))
			} else {
				ctx.Log().Infof("skills: NOT watching — %s", wRes.WatchError)
			}
		}
	}
	// S35 T2/T3 — the trust desk's validation seam: proposal.create reaches
	// T2's registry (cmd/aeon only — it calls policy.go's functions and
	// internal/mcp's Config.Validate, both off-limits to internal/protocol)
	// through this direct assignment, no wiring function needed since
	// there is nothing here that can fail at wire time.
	//
	// S45 T1 kept this line exactly as it is — the seam cannot move, since
	// internal/protocol may not import what it calls — and changed only what
	// it points AT: `validateProposalPayload` now forwards to internal/desk's
	// one effect registry. That registry (`deskEffects`, in proposal.go) also
	// carries the seams for the kinds the desk may not implement — both mcp
	// kinds in full, and `agent_import`'s effect — because they reach
	// internal/mcp and internal/plugin, which are closed to the desk by S34's
	// MC11 importer pin and to the guard by name. It is a package-level value,
	// so this daemon and `aeon proposal commit` run the SAME map: there is no
	// second registration here, and T5's socket commit door must take this
	// same registry rather than a copy of it.
	dopts.ValidateProposal = validateProposalPayload
	// S45 T5: the daemon's proposal decision door runs on THE same effect
	// registry the CLI's commit door uses — this opener builds it with the
	// exact expression `deskEffects` is built with, over the one seam map
	// desk.RegisterSeams injected at init (PM4: two doors, one table). An
	// unwired daemon (nil here) answers proposal.commit with desk's named
	// refusal while get/reject still stand.
	dopts.Effects = func() (*desk.Registry, error) { return desk.EffectsFor(openDeskBoundary) }
	// S22 T6: the retrieval search wiring — the `search` tool and its
	// `search.query` socket twin share ONE resolver function value, wired
	// here and nowhere else. The !noretrieval sibling of the link file is
	// the no-op: absence is the build configuration (V13), never a setting.
	wireRetrievalSearch(st, routes, &dopts)
	// The variable manager: its key file is created beside the store on first use
	// and is in no backup of the database. A key that cannot be opened leaves the
	// manager ABSENT (V13) — the daemon runs, the vault.* doors do not exist, and
	// the log says why; it never regenerates over a key it cannot read.
	if keyPath, kerr := paths.VaultKeyFile(); kerr != nil {
		ctx.Log().Errorf("vault: no key path: %v — the variable manager is off", kerr)
	} else if v, verr := vault.Open(st, keyPath); verr != nil {
		ctx.Log().Errorf("vault: %v — the variable manager is off", verr)
	} else {
		dopts.Vault = v
		setMCPVault(v, st)
	}
	// The daemon owns its MCP servers' processes: it starts every enabled stdio
	// server once the legacy file has been adopted (below), and stops them at
	// shutdown. Built here so the doors and the step dispatch see one supervisor.
	mcpSup = newStoreSupervisor(st)
	d := daemon.New(dopts)
	// S51 T1 (TM3): the filed_by name→slug migration runs at DAEMON START,
	// after migrations (store.Open) and after the store has its agent rows
	// (daemon.New seeds), and BEFORE the socket opens its doors (Register
	// below) — nothing can write a bug while the history is still half
	// spelled. All-or-nothing: an unresolved name refuses the start and
	// `what` names EVERY orphan (SQL cannot name what it refuses, which is
	// why this is Go, the S43 T2 precedent). A fresh store has no bug rows
	// and a migrated store resolves against rows it already carried — the
	// refusal is for the half-planted store nobody should own.
	if n, err := st.MigrateBugFiledBySlugs(); err != nil {
		return fmt.Errorf("filed_by migration: %w", err)
	} else if n > 0 {
		ctx.Log().Infof("triage: re-pointed %d bug row(s) from agent name to agent slug (S51 TM3)", n)
	}
	// S47 T6 (TC8): the narrow projection's own answer to "which action projects
	// this socket method", handed to the surface table as a func value. It is a
	// func and not an import because internal/narrow imports internal/protocol —
	// daemon→narrow would drag the HTTP middleware into the daemon package for a
	// fact it does not own. Nothing wired at all is a THIRD answer, not a false:
	// the surface then reports narrow:"unknown" for every method rather than
	// letting an unwired projection read as a closed one.
	d.Table.NarrowMethods = func() map[protocol.MethodName]string {
		out := make(map[protocol.MethodName]string, 9)
		for _, a := range narrow.Actions() {
			out[a.SocketMethod] = a.Name
		}
		return out
	}
	if err := d.Register(); err != nil {
		return fmt.Errorf("register protocol methods: %w", err)
	}
	// The dispatcher the narrow HTTP mounting dispatches THROUGH — the same
	// *protocol.Table the socket serves, one registration point, so both
	// mountings answer from one method set (N13; the set-equality test in
	// narrow's own suite drives both from one Actions()).
	if settings.httpAddr != "" {
		narrowDisp.set(&daemonCaller{table: d.Table})
	}

	reportLocation(os.Stdout)
	for _, line := range daemon.DescribeRoutes(routes) {
		fmt.Fprintln(os.Stdout, line)
	}
	if settings.httpAddr != "" {
		// O8: a listening port says so, at startup and in `aeon doctor`. The
		// address and the FACT of the key — never the key (I6); and the fact
		// is load-bearing, because a keyless bind refused to start at T2's
		// gate, so a line printed here could not be printed without one.
		fmt.Fprintln(os.Stdout, narrow.StartupLine(settings.httpAddr))
	}
	ctx.Log().Info("aeon is ready")

	// `daemon.started` is emitted after the endpoint is reported, so a client
	// that connects on seeing the endpoint line can still receive it from the
	// retained log via `subscribe {after: 0}`.
	d.Started(localrpc.EndpointPath())
	recoveryCtx, cancelRecovery := context.WithCancel(context.Background())
	recoveryDone := make(chan struct{})
	go func() {
		defer close(recoveryDone)
		// S32 T6 (PA12): reconcile every loop's live loop_worktree rows against
		// real git BEFORE the per-step recovery walk — a position whose worktree
		// is gone is rebuilt, one whose worktree is live is reattached, a settled
		// one is cleared — so a crashed loop's positions are reconciled before any
		// of them is recovered. Like the recovery scan it is gated by --no-recover
		// and its errors are logged, never fatal (the serve recovery-goroutine
		// bargain: a reconciliation outage must not take the daemon with it).
		if !noRecover {
			if _, err := d.ReconcileLoops(recoveryCtx); err != nil {
				ctx.Log().Infof("worktree reconciliation failed: %v", err)
			}
		}
		// S59 T9 (EV12): the environment sweep runs at start, BEFORE the step
		// recovery walk, so a container a crashed daemon left behind is adopted,
		// finished or removed before any step asks the ledger what is running.
		// Unlike the worktree walk it is NOT gated by --no-recover: --no-recover
		// says "do not resume my loops", not "leave my containers running".
		if _, err := d.ReconcileEnvironments(recoveryCtx); err != nil {
			ctx.Log().Infof("environment reconciliation failed: %v", err)
		}
		// The idle sweep for blocked loops runs on the daemon's own clock,
		// cancelled with the recovery context so a shutdown cannot strand a
		// sweep mid-Remove (EV15).
		d.StartEnvironmentReconcile(recoveryCtx, time.Hour)
		if summary, err := d.Recover(recoveryCtx, noRecover); err != nil {
			ctx.Log().Infof("recovery scan failed: %v", err)
		} else if summary.Total > 0 {
			ctx.Log().Infof("recovery scan: %d step(s), %d recovered, %d failed, %d skipped",
				summary.Total, summary.Recovered, summary.Failed, summary.Skipped)
		}
		// S64: the async-worker sweep — rows a dead daemon left in
		// queued/running become interrupted with their reason spoken.
		// Nothing re-fires them (the registry is code, and a crashed
		// ten-minute parse restarted invisibly is a loop nobody agreed
		// to): the row, visible through asyncworkers.list, is what the
		// app shows and what invites a deliberate retry. Gated by
		// --no-recover like the step scan, logged and never fatal.
		if !noRecover && d.Workers() != nil {
			if n, err := d.Workers().Recover(recoveryCtx); err != nil {
				ctx.Log().Infof("async-worker recovery failed: %v", err)
			} else if n > 0 {
				ctx.Log().Infof("async-worker recovery: %d worker(s) marked interrupted", n)
			}
		}
		if !noRecover && recoveryCtx.Err() == nil {
			// S43-extra T6: the inert sets apply at start, before loops are
			// accepted (SD5) — after migrations, before StartLoops. A failure
			// is logged and visible in seed.status as failed records but does
			// NOT abort start (a half-seeded agent set must not brick the
			// daemon); dispatch refuses a missing agent by name (T5), so the
			// failure is loud where it matters. One log line per set.
			daemon.ApplyInertAtStart(recoveryCtx, dopts.Seed)
			// S46 T4 (MG3): the legacy mcp.json is carried into the tables
			// ONCE, automatically, here — after migrations and agent
			// seeding, BEFORE any loop is accepted (T0's recorded point:
			// an offer must not be built from a half-adopted world). One
			// log line for the two loud outcomes; "no-file" and
			// "already-imported" say nothing (an absent file is a state,
			// and a re-start is not news). A refused file still starts at
			// level-0 — loudly: the sentence is logged here and doctor
			// repeats it from the ledger.
			if st != nil {
				defer func() {
					// After adoption, so servers carried in from the legacy file are
					// there to start. In the background: a first `npx -y` download must
					// not hold "startup recovery complete" back.
					if mcpSup != nil {
						go func() { _ = mcpSup.Sync(recoveryCtx) }()
					}
				}()
				if ar, aerr := adoptLegacy(recoveryCtx, st, ""); aerr != nil {
					ctx.Log().Infof("mcp: the legacy configuration file could not be checked: %v", aerr)
				} else if ar.Outcome == importImported {
					ctx.Log().Infof("mcp: adopted %d servers from %s", ar.Servers, ar.Path)
				} else if ar.Outcome == importRefused {
					ctx.Log().Infof("mcp: legacy file refused — %s", ar.Detail)
				}
			}
			// S58 T5 (SV-D6): "daemon was down" is not a delivery row —
			// it is unknowable — so the compensation runs ONCE at start:
			// if attention items exist, one `inbox.digest` envelope per
			// enabled channel, a COUNT and nothing else, recorded like
			// any delivery. Missed per-event messages are never replayed
			// (stale is stale); quiet hours still suppress the digest and
			// the suppression is itself a row.
			d.NotifyDigestAtStart(recoveryCtx)
			// S63 T3 (RT4 §4): the retro learning reconcile — after the
			// inert seed apply (the retro.templates rows exist now; rule 6
			// renders from rows, never code) and before StartLoops arms
			// the recorder, so a crash's missing learning is closed before
			// the next milestone is counted. Logged, never fatal: a stuck
			// learning retries at the next start, the facts stand regardless.
			if n, err := d.ReconcileRetro(recoveryCtx); err != nil {
				ctx.Log().Infof("retro reconcile failed: %v", err)
			} else if n > 0 {
				ctx.Log().Infof("retro reconcile: %d milestone learning(s) closed", n)
			}
			if err := d.StartLoops(recoveryCtx); err != nil {
				ctx.Log().Infof("loop runners could not start: %v", err)
			}
		}
		// The line a test fixture (and an operator reading the log) waits for:
		// the endpoint answers BEFORE this goroutine ends, and until it ends the
		// daemon is still writing the store (reconciliation, the inert seeds, the
		// legacy-MCP adoption), so a second writer started on "ping" alone can
		// outwait the busy timeout under load.
		ctx.Log().Infof("startup recovery complete")
	}()

	// S40 T6: the automation dispatcher starts only after StartLoops has run
	// (AU13's Reconcile must not race loop recovery) and not at all under
	// --no-recover. Errors are logged, never fatal — a dispatcher outage
	// must not take the daemon with it (the serve recovery-goroutine bargain).
	autoDone := make(chan struct{})
	go func() {
		defer close(autoDone)
		if noRecover {
			return
		}
		select {
		case <-recoveryDone:
		case <-recoveryCtx.Done():
			return
		}
		if err := d.RunAutomation(recoveryCtx); err != nil {
			ctx.Log().Infof("automation dispatcher stopped: %v", err)
		}
	}()

	// S27 T5: the continuous scheduler ticks like the dispatcher — only
	// after recovery and StartLoops have run (CN14's Reconcile must not
	// race loop recovery), and never at all under --no-recover (CN17: the
	// boot stays silent and open claims wait for a person). Reconcile the
	// open claims FIRST, then run the tick loop for the daemon's life.
	contDone := make(chan struct{})
	go func() {
		defer close(contDone)
		if noRecover {
			return
		}
		select {
		case <-recoveryDone:
		case <-recoveryCtx.Done():
			return
		}
		if err := d.ReconcileContinuous(recoveryCtx); err != nil {
			ctx.Log().Infof("continuous reconcile stopped: %v", err)
		}
		if err := d.RunContinuous(recoveryCtx); err != nil {
			ctx.Log().Infof("continuous scheduler stopped: %v", err)
		}
	}()

	// S55 T9 (LC17/LC20): the watchdog ticker starts, like the automation
	// dispatcher, only after StartLoops has run — a loop mid-recovery is not
	// a stuck one, and the tick reads running loops that must have settled
	// first — and not at all under --no-recover. Errors are logged, never
	// fatal: a stopped watchdog must not take the daemon with it (the serve
	// recovery-goroutine bargain), and an unwatched loop is a log line, not
	// a crash.
	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		if noRecover {
			return
		}
		select {
		case <-recoveryDone:
		case <-recoveryCtx.Done():
			return
		}
		if err := d.RunWatchdog(recoveryCtx); err != nil {
			ctx.Log().Infof("watchdog stopped: %v", err)
		}
	}()

	// S62 T6 (BK7): the backup schedule starts, like the watchdog, only after
	// recovery has run — a restart-safe schedule re-derives its due instant
	// from the ledger, and the ledger must have been recovered first — and
	// not at all under --no-recover. A maintenance task, not a trigger (the
	// guard proves the absence): it never starts a loop or touches a trigger
	// table. Errors are logged, never fatal — a stopped schedule must not
	// take the daemon with it (the serve recovery-goroutine bargain), and a
	// stale backup is an event, not a crash.
	backupDone := make(chan struct{})
	go func() {
		defer close(backupDone)
		if noRecover {
			return
		}
		select {
		case <-recoveryDone:
		case <-recoveryCtx.Done():
			return
		}
		if err := d.RunBackups(recoveryCtx); err != nil {
			if !errors.Is(err, context.Canceled) {
				ctx.Log().Infof("backup schedule stopped: %v", err)
			}
		}
	}()

	// Suspend by waiting, not by polling. The daemon holds the loops (P29), so
	// this is the process's whole job once the endpoint is up.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	cancelRecovery()
	select {
	case <-recoveryDone:
	case <-time.After(shutdownGrace):
	}
	select {
	case <-autoDone:
	case <-time.After(shutdownGrace):
	}
	select {
	case <-watchdogDone:
	case <-time.After(shutdownGrace):
	}
	select {
	case <-contDone:
	case <-time.After(shutdownGrace):
	}
	select {
	case <-backupDone:
	case <-time.After(shutdownGrace):
	}

	ctx.Log().Info("shutting down")
	// S55 T4 (LC19): the loop work settles BEFORE the terminals die, because
	// the drain order is the argument (LW9): no new runner spawns, every
	// in-flight runner's context carries ErrDaemonShutdown, and a step the
	// shutdown interrupts is left running at its last checkpoint — never
	// committed failed — so a restart recovers it from the store. The grace is
	// its own ctx so the drain's wait is bounded exactly like the rest of the
	// teardown (a runner that does not settle within it is LeftRunning, and
	// the process goes on to close the transport rather than waiting forever).
	drainCtx, drainCancel := context.WithTimeout(context.Background(), shutdownGrace)
	d.Drain(drainCtx)
	drainCancel()
	// S31 T7: terminals do not outlive the daemon (LW9). The reaper stops
	// first, then every shell dies as a process group — before the transport
	// starts closing, so no reader ever notifies into a dead socket.
	mcpSup.StopAll()
	d.Shutdown()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	bootstrap.Shutdown(shutdownCtx)
	ctx.Log().Info("shutdown complete")
	return nil
}

// reportLocation prints where the daemon lives (B10).
//
// Both lines go to stdout rather than the log, because they are the answer to
// "where is it" and a person reading them has usually not configured logging
// yet. Every later DEMO.md starts here.
func reportLocation(w io.Writer) {
	if ep := localrpc.EndpointPath(); ep != "" {
		fmt.Fprintf(w, "endpoint  %s\n", ep)
	} else {
		// Should be unreachable: localrpc is ForceOn, so configuration cannot
		// turn it off and a failure to bind fails the boot. Kept because an
		// empty path would otherwise print a blank line and look like the
		// daemon was fine — and this is the one line every demo starts from.
		fmt.Fprintln(w, "endpoint  (NONE — localrpc is pinned on, so this is a bug; report it)")
	}

	path, err := paths.StoreFile()
	switch {
	case err != nil:
		fmt.Fprintf(w, "store     (could not resolve: %v)\n", err)
	case !fileExists(path):
		// Unreachable since S4: the store opens before this prints, so the file
		// exists by now. Kept because "I cannot tell you where it goes" and
		// "it is not there" are different facts, and printing a blank line
		// would look like the daemon was fine.
		fmt.Fprintf(w, "store     %s  (reported missing AFTER a successful open — report this)\n", path)
	default:
		fmt.Fprintf(w, "store     %s\n", path)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// watchErrSuffix renders the partial-blindness note with its leading
// separator only when there is one; a silent "watching, no notes" line
// should not end in a dangling dash.
func watchErrSuffix(s string) string {
	if s == "" {
		return ""
	}
	return "; " + s
}

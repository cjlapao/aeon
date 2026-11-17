// Package daemon wires the protocol onto a transport.
//
// # This is the ONLY package in the slice that knows what a socket is
//
// `internal/protocol` holds the method table, the types, the registry and the
// event log, and imports nothing transport-related (T11) — which is what lets
// S19 mount the same table on HTTP and MCP-stdio. Everything transport-shaped
// lives here: registration, the error mapping, peer credentials, delivery.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/vault"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"

	"github.com/cjlapao/aeon/internal/asyncworkers"
	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/continuous"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/gatewatch"
	"github.com/cjlapao/aeon/internal/graph"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	_ "github.com/cjlapao/aeon/internal/model/adapters" // registers the provider adapters
	"github.com/cjlapao/aeon/internal/notify"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/search"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/skills"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/terminal"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/trigger"

	"github.com/cjlapao/aeon/internal/remote"
)

// Daemon is the protocol, mounted.
type Daemon struct {
	Table *protocol.Table

	// recordsSvc is the catalog's Do service, kept for the wiring the
	// runner's existence gates (S39 T10): the run door runs through it.
	// Nil on a storeless mount — the tool then answers its own
	// unavailability rather than the daemon inventing a door.
	recordsSvc *RecordsService

	// classes is S58 T4's notify-class resolver (SV4/SV5): the single
	// source of delivery-time hints, built only with a store. Nil means
	// the resolver's nil-receiver stance — no class table, no hints.
	classes *notify.Resolver

	// channels is S58 T5's channel seam (SV7/SV8): the store-backed
	// channel services and the ONE dispatcher — its two senders are the
	// only outbound path a channel delivery takes. Built only with a
	// store (V13); Shutdown drains it after the workers.
	channels *notifyChannels

	// workers is S64's async-worker manager: the ledger of background
	// work, the scheduler, and the publisher of asyncworker.* events.
	// One per daemon (P34), built only with a store; serve calls its
	// Recover after the loop recovery, and Shutdown drains it before the
	// store closes. Nil on a storeless mount — the asyncworkers family is
	// then absent from the table (V13), graph.build refuses by name, and
	// there is no ledger to sweep.
	workers *asyncworkers.Manager

	// skills is S65's Agent Skills catalog: the one service over the two
	// roots (the operator's ~/.agents/skills and Aeon's managed folder),
	// the one cached scan every door reads through. Set from
	// Options.Skills; nil on a no-catalog mount — the skills family is
	// then absent from the table (V13).
	skills *skills.Service

	// chosen is S68's per-launch CHOSEN-skills cache (B6): the canonical
	// lines a launch rides, keyed by loop id, remembered at the door and
	// reloaded from the launch's own kickoff row after a restart. The rows
	// are the truth; this is the fast path over them. chosenMu guards it.
	chosenMu sync.Mutex
	chosen   map[string]chosenLaunch

	// launchAtts is S77's per-launch ATTACHMENTS cache — the store receipts
	// a launch filed on its kickoff row, keyed by loop id, reloaded from
	// that row after a restart (the row is the truth; this is the fast path
	// over it). launchAttsMu guards it. The sibling of `chosen` above, and
	// deliberately a separate map: a launch chooses skills and attaches
	// files as two decisions, and one drifting does not move the other.
	launchAttsMu sync.Mutex
	launchAtts   map[string][]store.AttachmentReceipt

	// resolveChosen is the S68 (B6) catalog door every chosen-skills check
	// goes through: names in, canonical lines + skipped out, unknown names
	// back as an error naming every offender. Set in New from the mounted
	// catalog (nil catalog ⇒ choices are REFUSED); a test substitutes a
	// fake scanner without a filesystem.
	resolveChosen func(names []string) (*chosenSkillResolution, error)

	// notify and connections are the transport's, indirected so the whole
	// daemon is testable without binding a socket. A test substitutes a
	// recorder and asserts on what WOULD have gone on the wire — which is how
	// `client.connected carries a pid` is tested without a second process.
	notify      func(connID, method string, params any) error
	connections func() []localrpc.ConnInfo

	log   Logger
	store *store.Store

	// continuous is the S27 continuous-scheduler worker — built lazily by
	// continuousWorker() on first use (the reactors' kick and serve.go's
	// goroutine share that one instance). continuousOnce makes the build
	// exactly-once, not exactly-one-thread-guessing.
	continuous     *continuous.Worker
	continuousAdp  *continuousAdapter // set with the worker, once (T7 socket)
	continuousOnce sync.Once

	// Inbox is the S58 T2 attention service (SV1): the live source
	// registry, shared by the inbox.* doors and the sibling Register-
	// AttentionSource seam. nil exactly when there is no store.
	Inbox *InboxService

	// missionSeams is S58 T6's pair of registrars' answers (SV11): the
	// S55 cap reader and the S27 next-ready, nil until a slice registers
	// them — mission.status prints those groups `not-wired` until then.
	missionSeams missionSeams

	// missionSvc is the same service the socket door answers with (T6):
	// the records supervision adapter reads through it, so the agent's
	// overview and the person's are one evaluation, not twins.
	missionSvc *missionService
	// settings is the daemon's shared *config.Store (S44 T4), kept here so
	// every in-process consumer — the settings.* doors (T5) and the backup
	// service's read seam (S62 T10) — reads the same instance the entry
	// point built. Nil on a storeless mount (V13): the settings family is
	// absent and the backup service keeps its environment settings.
	settings *config.Store
	// roadmapSettingView is S50 T3's per-dispatch settings seam (RM3):
	// initRoadmap wires it to the shared store; tests replace it to
	// observe that every dispatch re-reads (nothing is captured at
	// construction). Nil means "use d.settings live, or the registry
	// default" — the same answers, just without the spy.
	roadmapSettingView settingView
	// triageSettingView is S51 T3's per-validate settings seam (TM4): the
	// twin of roadmapSettingView for triage.validationBlueprint — initTriage
	// wires it to the shared store, tests replace it to prove every attempt
	// re-reads. Nil means "use d.settings live, or the registry default".
	triageSettingView settingView
	// bpSrc is the daemon's OWN store-backed blueprint.Source (S48 T3):
	// every reader here — the seams, the start door, run options — reads
	// through the store it belongs to, never through a process global that
	// another daemon (or a closed test store) installed.
	bpSrc       blueprint.Source
	agentInvoke loop.ModelRunner
	// watchdog is S55 T9's watchdog service: the one place a firing becomes
	// an act (notify, or the attendedness-picked stop/block over the T2 CAS
	// functions). Built in New when the store is, so the protocol's
	// watchdog.* doors (T9) and serve's RunWatchdog share the one instance
	// (the once-per-episode memory is the service's state). Nil on a
	// storeless mount (V13): the doors are absent, the ticker never runs.
	watchdog *watchdogService
	// backup is S62 T6's backup service: the one place the five protocol
	// backup.* doors (T6) and serve's RunBackups ticker share the schedule's
	// state (the once-per-stale-transition arm, the single-flight flag).
	// Built in New when the store is (V13: a storeless mount keeps nil, the
	// five doors absent, the ticker a no-op), with a lazy fallback in
	// RunBackups for a mount built before the wiring existed.
	backup *BackupService
	// export is the S62 T8 export service: the two export doors (run/sets)
	// over the live store. Built in New when the store is (V13: a storeless
	// mount keeps nil, the two doors absent, the wiring a no-op).
	export     *ExportService
	recoveryMu sync.RWMutex
	recovering map[int64]struct{}
	loopRunner *loop.Runner
	// loopCtx is the daemon-lifetime context every loop runner is started on
	// (T4, LC19): at a graceful shutdown the drain cancels it with
	// ErrDaemonShutdown, so an in-flight runner's context carries the cause
	// the step commit path reads — a step interrupted by shutdown is left
	// running at its last checkpoint for recovery, never committed failed.
	// context.Background() would make the daemon's shutdown unable to reach
	// the goroutines it started (the abandoned-children bug T4 fixes).
	loopCtx     context.Context
	cancelLoops context.CancelCauseFunc

	// remoteCallbacks is S21 T6's seam: the mount point that verifies and
	// binds an inbound remote completion (RA7/RA8). It is nil unless the
	// ENTRY POINT wires it — only cmd/aeon's serve builds it, because it
	// speaks to agentsvc's TaskStore (AS11's deletability: the daemon must
	// build without the agent-services layer, and an unwired daemon leaves
	// remote.complete answering the absence by name rather than pretending
	// peers exist; RA9's off-by-default reaching the socket).
	remoteCallbacks *remote.Callbacks

	// triggers is S20's clock: the scheduler that answers "did this fire?"
	// from the store's rows, living beside the terminal reaper — a daemon
	// service, not a method. Nil for a storeless daemon (no rows, nothing
	// to answer; the honest absence, not a tick on nothing).
	triggers *trigger.Scheduler

	// gateWatcher is S57 T7's post-merge watch (VG5): the bounded poll that
	// answers "did the checks go red on the merged sha?" from rows. Nil for
	// a storeless daemon (no merge events, nothing to watch) — initGateWatch
	// is the only constructor, and it refuses a nil store.
	gateWatcher *gatewatch.Watcher

	// stepCancels is how `step.stop` reaches a running step on a different
	// connection. See stepcancel.go for why this is a daemon-owned map
	// rather than anything `internal/protocol` keeps for itself.
	stepCancels *stepCancels

	// turns is S39 T6's conversation guard: the loop ids with a turn's
	// model step in flight. One turn per loop — the map is the race window's
	// backstop, the sliver between the note landing and the step row
	// existing where ListSteps still says "nothing runs". Kept on the daemon
	// for the same reason stepCancels is: this is daemon liveness, not
	// protocol state.
	turnMu sync.Mutex
	turns  map[string]struct{}

	// events is the durable log. Kept on the daemon so LW7's proof can read
	// Head() before and after a terminal's whole life: terminal bytes must
	// never allocate a seq (the coalescer's chunks are notifications, not
	// events).
	events *protocol.EventLog

	// terminals is the S31 T7 PTY registry (the person's own shells), owned
	// by the daemon for the same reason stepCancels is: it is state shared
	// across calls that the method table must not hold. termCancel stops the
	// reaper on the way down — see terminal.go for the order argument.
	terminals  *terminal.Manager
	termCancel context.CancelFunc

	// S60 T7 (MQ2): the merge-queue reactor. mqNotify coalesces wakes
	// (buffered size 1 — ten event joins are one reconcile pass);
	// mqLand is the standing `mergequeue.land` requests between the door
	// and the decision, guarded by mqLandMu (the durable trail is the
	// event the decision writes, not this map).
	mqNotify chan struct{}
	mqLandMu sync.Mutex
	mqLand   map[string]bool
}

// Logger is the subset of logging this package needs. An interface rather than
// the framework's logger so a test can assert on the accept line (D12) without
// capturing global output.
type Logger interface {
	Infof(format string, args ...any)
}

// Options configures a Daemon.
type Options struct {
	DaemonVersion string
	BuildTime     string
	GitCommit     string
	Logger        Logger

	// Notify and Connections default to the live localrpc service.
	Notify      func(connID, method string, params any) error
	Connections func() []localrpc.ConnInfo

	// Routes and Models wire the model layer (S3). Nil leaves `ask` reporting
	// that no model layer is wired, which is better than a daemon that panics
	// on the first call.
	Routes protocol.Routes
	Models protocol.Streamer

	// Search is the retrieval resolver behind `search.query` (S22 T6, D15/
	// P34): the SAME function value the `search` tool was wired with at the
	// entry point — one implementation, two transports. Nil in every
	// no-retrieval build (V13) leaves the method UNREGISTERED — absent, not
	// registered-and-refusing, mirroring the tool's own absence.
	Search search.SearchResolver

	// Settings is S44 T4's shared *config.Store, built once at the entry point
	// (NewSettings) and read by the settings.* methods (T5) and the in-process
	// consumer (T10). It is the ONE instance the program holds (CS7: no
	// package-level global, no sync.Once), so "same process or not" is a fact
	// the daemon proves, not a convention. Nil (a storeless daemon, V13) leaves
	// settings.* UNREGISTERED — absent, not registered-and-refusing, mirroring
	// Search above. T4 hands the instance to the daemon; T5 wires it onto the
	// table (table.Settings + RegisterSettings).
	Settings *config.Store

	// Seed is the S43-extra T6 seam: the ONE *seed.Engine the daemon holds
	// (NewSeedEngine, built at the entry point over the store). Nil-safe (a
	// storeless daemon, V13) leaves the four seed.* methods UNREGISTERED —
	// absent, not registered-and-refusing, mirroring Search and Settings
	// above. The startup inert apply (ApplyInertAtStart) and the seed.*
	// method adapters read this one instance; the engine holds no global
	// state, no goroutine and no timer (the clock is injected), so the
	// daemon's one instance is the one the CLI client and the bench page
	// read through the socket.
	Seed *seed.Engine

	// Skills is the S65 T4 seam: the ONE *internal/skills.Service the
	// entry point built over the two roots (the operator's
	// ~/.agents/skills and Aeon's app-managed folder). The socket, the
	// CLI client and the bench page read that one catalog; nil (V13)
	// leaves the five skills.* methods UNREGISTERED — absent, not
	// registered-and-refusing, mirroring Settings above.
	Skills *skills.Service

	// CorpusStatus and Reindex are the S22 T8 seams (D15, I10, P34, V21):
	// the status figures with PER-CORPUS staleness, and the deliberate
	// embedding pass — the SAME function values the `aeon index` verbs
	// reach through the socket. Nil in every no-retrieval build (V13)
	// leaves both methods UNREGISTERED, mirroring the search doors' own
	// absence. Nothing here fires a pass on its own: the loop's write
	// trigger (V8/Q7) begins through the daemon's single-flight tracker,
	// and every other path only reads.
	CorpusStatus search.StatusFunc
	Reindex      search.ReindexFunc

	// RetrievalPlan is S54 T2's priced-preview seam (SP2): the SAME
	// estimate walk the priced refusal runs, over the same routes —
	// wired where the index is (cmd/aeon's retrieval link), nil in every
	// no-retrieval build, which leaves `retrieval.reindexPlan`
	// UNREGISTERED with its sibling (V13). It never touches an embedder:
	// a preview that embedded to price itself charged for the preview.
	RetrievalPlan protocol.RetrievalPlanFunc

	// AgentInvoke is S14 T4's seam for a MODEL step that names a skill: the
	// step is an agent asking another agent, not a model call, and the asking
	// is the agentsvc package's job. It is nil unless the entry point wires
	// it — which only cmd/aeon's serve does, because the daemon must not
	// import internal/agentsvc (AS11's deletability: `rm -r internal/agentsvc`
	// leaves this package building, the branch below reporting honestly that
	// agent services are not wired rather than pretending otherwise).
	AgentInvoke loop.ModelRunner

	// Agents is S14 T10's socket seam: the plain-func bridge to
	// internal/agentsvc's transport for the seven agent.* methods. A func
	// type for the same reason AgentInvoke is one — the daemon must not
	// import agentsvc (AS11's deletability), and only cmd/aeon's serve
	// wires it. Nil leaves all seven answering ErrNotWired by name: an
	// unwired daemon states the absence rather than pretending.
	Agents func(ctx context.Context, method string, params json.RawMessage) (any, string, error)

	// RemoteCallbacks is S21 T6's entry-point injection (see the Daemon
	// field): peers configured + task book + clock, built by cmd/aeon from
	// the agentsvc TaskStore. Nil leaves the completion refused as unwired.
	RemoteCallbacks *remote.Callbacks

	// Effects is S45 T5's effect-registry opener for the proposal decision
	// door: cmd/aeon fills it with `func() (*desk.Registry, error) {
	// return desk.EffectsFor(openPolicyBoundary) }` — THE same expression
	// the CLI's deskEffects is built with, over the one seam map
	// desk.RegisterSeams injected (PM4: no daemon-side effect table to
	// drift). Nil leaves proposal.commit answering desk's named refusal
	// while proposal.get/reject (which apply no effect) still stand.
	Effects func() (*desk.Registry, error)

	// Plugins is S15's read seam for `agent.plugins`, threaded to
	// Table.Plugins by the entry point (the registry itself is cmd's to
	// import — the daemon must build without the plugin directory too).
	Plugins func(ctx context.Context, params json.RawMessage) (any, string, error)

	// AgentImportPreview is S37's ONE seam for proposal.create{kind:
	// "agent_import"}, threaded to Table.AgentImportPreview by the entry
	// point. cmd/aeon builds a closure over internal/plugin (the only
	// package allowed to import it — TestNoUnconditionalImportOfPlugin); the
	// daemon/protocol layers hold a plain func. nil means half-wired (the
	// create answers ErrNotWired by name), never silently permissive.
	AgentImportPreview func(ctx context.Context, pkgPath string) (preview *protocol.AgentImportPreview, refusal string, err error)

	// AgentManage is S43's ONE seam for the agent-management methods
	// (agent.create/import/export/duplicate/delete/edit/view/migrate),
	// threaded to Table.AgentManage by the entry point. cmd/aeon builds a
	// closure over internal/agentmgr (the only package that may import it);
	// the daemon/protocol layers hold a plain func, dispatched by method
	// name, the one-seam-many-methods shape Agents and AgentTransition
	// already use. nil means half-wired (the methods answer ErrNotWired by
	// name), never silently permissive (P34: the daemon is the primary
	// surface).
	AgentManage func(ctx context.Context, method string, params json.RawMessage) (result any, refusal string, err error)

	// AgentRoster is S34 T1's read seam for `agent.list`, threaded to
	// Table.AgentRoster by the entry point — same deletability discipline
	// as Plugins, a separate seam because it answers a different shape
	// (TB1: one door per fact).
	AgentRoster func(ctx context.Context, params json.RawMessage) (any, string, error)

	// ToolGrants is S34 T2's read seam for `tools.grants`, threaded to
	// Table.ToolGrants by the entry point.
	ToolGrants func(ctx context.Context, params json.RawMessage) (any, string, error)

	// MCP is S34 T3's seam: internal/protocol must never import internal/mcp
	// (absent_test.go's banned list names internal/protocol explicitly — the
	// same rule S36-mcp-dispatch/design.md MD1 states for step dispatch,
	// applied here to observability). One function, dispatched by method
	// name, same shape as Agents — cmd/aeon is the only package that may
	// implement it. Answers mcp.status (T3) and mcp.check (T4).
	MCP func(ctx context.Context, method string, params json.RawMessage) (any, string, error)

	// MCPAdmin is S46 T3's seam: the five PERSON'S MCP management reads
	// (mcp.servers · mcp.server · mcp.grants · mcp.vocabulary · mcp.changes)
	// answered by cmd/aeon over the MCP tables — same seam discipline as
	// MCP above (internal/protocol and internal/daemon never import
	// internal/mcp; the func value carries it). Unlike MCP's unconditional
	// registration these five register CONDITIONALLY (RegisterMCPAdmin:
	// V13, nil seam ⇒ absent, ErrUnknownMethod — a storeless daemon does
	// not register-and-refuse).
	MCPAdmin func(ctx context.Context, method string, params json.RawMessage) (any, string, error)

	// AgentTransition is S35 T4's seam: internal/plugin is banned from
	// internal/protocol (same discipline as MCP/internal/mcp above), so
	// agent.enable/disable/revoke reach plugin.Enable/Disable/Revoke
	// through this func — cmd/aeon is the only package that may implement
	// it.
	AgentTransition func(ctx context.Context, method string, params json.RawMessage) (any, string, error)

	// ValidateProposal is S35 T2's seam: validation logic calls
	// cmd/aeon/policy.go's functions and internal/mcp's Config.Validate,
	// both off-limits to internal/protocol, so proposal.create reaches it
	// through this func — cmd/aeon is the only package that may implement
	// it, same discipline as MCP above.
	ValidateProposal func(ctx context.Context, kind, payload string) error

	// StepMCP is S36-mcp-dispatch's step seam (MD1): the function value
	// BuildTools calls to build a step's MCP bundle — the offered tools
	// (with the server's live schema), their dispatcher, and the withheld
	// candidates with grants.go's sentences. A func value, not an import:
	// internal/daemon must never import internal/mcp (MC11 — absent_test.go's
	// banned list names internal/daemon explicitly, so this stays a zero-diff
	// line). Only cmd/aeon (the sanctioned importer) implements it (T2); nil
	// leaves the step exactly as today (MD8), and the bundle's Close is
	// reaped at the four BuildTools callers (T4, MD5).
	StepMCP func(ctx context.Context, agent string, frozen runtime.FrozenManifest, at time.Time) (*MCPBundle, error)

	// Store is S4's memory. It is what makes `seq` survive a restart — the
	// gap S2 admitted — and what the store methods read.
	//
	// Nil leaves the in-memory counter in place and every store method
	// answering ErrStoreUnavailable. That is the mode the daemon's own tests
	// use; `aeon serve` always passes one, because the store opens before the
	// daemon is built.
	Store *store.Store

	// Vault is the variable manager (internal/vault): values encrypted in the
	// store, a key file beside it. Nil leaves the `vault.*` doors absent and the
	// env_* tools unavailable (V13), and process environments built as before.
	Vault *vault.Vault
}

// New builds a daemon over the protocol table.
func New(opts Options) *Daemon {
	sessions := protocol.NewSessions()
	subs := protocol.NewSubscriptions()

	// The backing is attached at CONSTRUCTION (Q8's sequencing): before the
	// method table exists, therefore before any client can connect, therefore
	// with no moment at which a live subscription could see the numbering
	// change. A storeless daemon keeps S2's in-memory counter, which is a test
	// mode and not a shipped one.
	events := protocol.NewEventLog().WithBacking(backing(opts.Store))

	table := protocol.NewTable(sessions, subs, events)

	// S30 T5 (P34's same-value rule): ONE graph service over the store —
	// the value the socket family AND the `graph` tool AND the write hook
	// below all hold. The identity is what makes the two doors one door
	// (TestGraphToolAndSocketShareOneService pins it), and the single
	// tools.SetGraphService call here is the seam's ONLY production caller
	// (TestGraphServiceHasExactlyOneProductionCaller counts it).
	// S54 T1: the stamping source is assigned HERE, before the store
	// branch below registers the doors that read it. S40 T8 stamped the
	// person behind the trigger-write doors exactly this way — the uid is
	// the kernel's, not a claim, no path constructs Kind "agent", and a
	// connection the transport never recorded is uid 0 — and S46's import
	// door read ActorFor at its registration far below, where this
	// assignment used to sit. The four derived-store OPERATIONS (S54:
	// graph.build, graph.reparse, graph.delete, retrieval.reindex)
	// register at 450/664, and their V13 gate reads ActorFor AT
	// REGISTRATION — after the old position they would all have been
	// silently absent from the production table. The closure resolves
	// `d` at the first dispatch, never at construction (the
	// publishEvents/taskChanged pattern, carried).
	var d *Daemon
	table.ActorFor = func(_ context.Context, sess *protocol.Session) protocol.Actor {
		uid, _, _ := d.peer(sess.ConnID)
		return protocol.Actor{Kind: "user", ID: strconv.Itoa(uid)}
	}
	var graphSvc *graph.Service
	// S64 T4: the async-worker manager, ONE per daemon, over the same
	// store the graph service reads. It is the ledger AND the scheduler —
	// the socket's graph.build starts through it, the write hook's
	// unbuilt-root branch starts through it, and serve's recovery and the
	// daemon's Shutdown close its loop (identity pinned by
	// TestGraphBuildStartsThroughTheSingleManager). Its publish func is
	// indirected because the event stream belongs to the Daemon, which is
	// assembled below; the closure resolves at the first runtime event,
	// never at construction (the recordsSvc.taskChanged pattern, carried).
	var workers *asyncworkers.Manager
	var publishEvents func(kind, componentRoot string, payload any)
	if opts.Store != nil {
		graphSvc = graph.NewService(opts.Store)
		table.Graph = graphSvc
		table.RegisterGraph()
		tools.SetGraphService(graphSvc)

		// S53 T3 (HV4's prerequisite, the restoration): the history seam.
		// S25 T4 wired `table.HistoryStore = history.NewStore(opts.Store)`
		// and the October "wip" commit b5620631 deleted that line along with
		// the bench tab — since then every shipped daemon answered the five
		// history doors with ErrStoreUnavailable (V13's inert absence —
		// honest, but silent, and the app screen S53 exists to rebuild had
		// nothing behind it). The line returns: one facade over the one
		// store, shared by the socket's doors and the records arms below.
		table.HistoryStore = history.NewStore(opts.Store)

		workers = asyncworkers.New(opts.Store, asyncworkers.Options{
			Publish: func(kind, componentRoot string, payload any) {
				if publishEvents != nil {
					publishEvents(kind, componentRoot, payload)
				}
			},
		})
		// The graph's ten-minute walk, registered as a worker: the runner
		// calls the SAME method value the socket held inline before S64 —
		// graphSvc.Build, the service identity above unchanged, reached
		// through the ledger rather than through the caller's connection.
		// No progress ticks: graph.Build reports at the end or not at
		// all, so the row's progress stays NULL — an honest "unknown",
		// never a fabricated crawl (three-state discipline).
		if err := workers.Register("graph.build", func(ctx context.Context, job asyncworkers.Job, rep asyncworkers.Reporter) (json.RawMessage, error) {
			report, err := graphSvc.Build(ctx, job.ComponentRoot)
			if err != nil {
				return nil, err
			}
			return json.Marshal(report)
		}, asyncworkers.WorkerOpts{}); err != nil && opts.Logger != nil {
			opts.Logger.Infof("asyncworkers: graph.build could not register: %v", err)
		}
		// S22 async: the retrieval reindex rides the SAME manager, so the
		// deliberate door (retrieval.reindex) and the write-hook trigger
		// start the SAME worker type through the SAME flight — hook and
		// button cannot double-pass (S64's graph.build precedent, carried
		// to retrieval; the "same approach it was added" the request asked
		// for). Registered only when the retrieval seam is linked: a
		// no-retrieval build has no pass to run, so the worker is absent
		// (V13), and the retrieval.reindex door goes unregistered with it.
		// The runner names no corpus of its own — the corpus, the embed
		// route and the force (the V17 money decision) ride Metadata the
		// starting door shaped; a pass onto another model's corpus is the
		// door's decision, never the runner's. No progress note: the pass
		// reports done/total, mapped to a fraction — the row's progress is
		// a real crawl, and an unreported pass stays NULL (three-state
		// honesty, not a fabricated 0%).
		if opts.Reindex != nil {
			if err := workers.Register("retrieval.reindex", func(ctx context.Context, job asyncworkers.Job, rep asyncworkers.Reporter) (json.RawMessage, error) {
				corpus := job.Metadata["corpus"]
				if corpus == "" {
					return nil, errors.New("retrieval.reindex needs a corpus in its metadata — the runner is not the door that names it (O8)")
				}
				out, err := opts.Reindex(ctx, job.ComponentRoot, corpus, job.Metadata["embedRoute"], job.Metadata["force"] == "true", func(done, total int) {
					if total > 0 {
						rep.Progress(float64(done)/float64(total), "")
					}
				})
				if err != nil {
					return nil, err
				}
				return json.Marshal(out)
			}, asyncworkers.WorkerOpts{}); err != nil && opts.Logger != nil {
				opts.Logger.Infof("asyncworkers: retrieval.reindex could not register: %v", err)
			}
		}
		table.AsyncWorkers = workers
		table.RegisterAsyncWorkers()
	}
	table.Routes = opts.Routes
	table.Models = opts.Models
	// S65 T4: the Agent Skills catalog, registered only when the entry
	// point handed one over (V13 — the no-catalog mount simply has no
	// skills doors). The typed-nil guard matters: assigning a nil
	// *skills.Service into the interface field would make it non-nil and
	// register five doors onto nothing. Nothing here scans at
	// construction: the first read walks disk, and `skills.validate`
	// re-walks on demand.
	if opts.Skills != nil {
		table.Skills = opts.Skills
		// S66: the catalog also reaches every compiled step (the ONE
		// SetSkillsProvider call in production, in skills_wire.go), and
		// the watcher announces diffs on the wire as `skills.changed` —
		// late-bound because the event door is being built around us.
		wireSkills(opts.Skills, opts.Logger)
		opts.Skills.SetEventEmit(func(ev skills.ChangeEvent) {
			table.OnEvent(protocol.KindSkillsChanged, ev)
		})
	}
	table.RegisterSkills()
	// S22 T6: the search seam, and the registration that rides with it.
	// Nil here means the retrieval link was not compiled in, and
	// RegisterSearchQuery leaves `search.query` absent (V13).
	table.Search = opts.Search
	table.RegisterSearchQuery()
	// S22 async (the graph's "same approach it was added"): the pass the
	// daemon started is the daemon's own knowledge, but it is now a ledger
	// row in the SAME manager the graph.build rides, not a separate
	// tracker. The status reads the running figure (state/progress) from
	// that row per-corpus, and the write-hook trigger starts the same
	// worker through the same manager — so hook and button cannot
	// double-pass, exactly as the graph's notifyGraph and graph.build
	// cannot double-build. The doors register only when their seams are
	// wired (V13 — absent, not registered-and-refusing).
	// S30 T4 (D5, D6): the write signal the loop already raises (S22's
	// OnWorkspaceWrite — found pre-built, subscribed to, NOT rebuilt)
	// also keeps the graph current: the written file is re-parsed alone,
	// async, its failures spoken to a logger and NEVER blocking the step
	// (V18's rule for this hook, which S22 measured). The first write to
	// an unbuilt root builds the whole root in the background — the graph
	// builds itself, and no separate command owns that first fill (the
	// DEMO's prediction, made true by the hook, not by a verb).
	graphBusy := &sync.Map{} // root → struct{}: one graph pass per root at a time
	notifyGraph := func(root, path string) {
		if graphSvc == nil || root == "" {
			return
		}
		if _, running := graphBusy.LoadOrStore(root, struct{}{}); running {
			return // a pass is already reading this tree; the file it is mid-way over re-reads on the next write
		}
		defer graphBusy.Delete(root)
		// A panic here stays inside the goroutine chainHooks gives this
		// subscriber: the graph is a convenience index and a half-built
		// one is re-buildable; the step has already moved on.
		if n, err := graphSvc.StatsForRoot(context.Background(), root); err == nil && n == 0 {
			// S64: the whole-root fill is a TEN-MINUTE job — it belongs
			// to the ledger now, not to a bare goroutine the UI cannot
			// see. The same worker type the socket's graph.build starts,
			// the same manager, the same flight key (the absolute root):
			// hook and button cannot double-build. If the manager is
			// absent (no store — there is no graph to fill either) or
			// the root will not resolve, fall back to the pre-S64 direct
			// call rather than silently skipping the fill.
			if workers != nil {
				if abs, aerr := filepath.Abs(root); aerr == nil {
					_, _, _ = workers.Start("graph.build", abs, abs, map[string]string{"trigger": "write-hook"})
					return
				}
			}
			_, _ = graphSvc.Build(context.Background(), root)
			return
		}
		_, _ = graphSvc.RebuildFile(context.Background(), root, path)
	}
	// notifyReindex is the retrieval's mirror of notifyGraph: the SAME
	// write signal (V8/Q7) starts the SAME worker the socket's
	// retrieval.reindex starts, through the SAME manager and the SAME
	// per-(root,corpus) flight — hook and button cannot double-pass. A
	// code write is a code-corpus question (never history); the trigger
	// names no route and never forces, so an automatic pass cannot
	// switch embedding models (V17's money decision stays the human
	// door's). Fire-and-forget, never blocking the step (V18): Start
	// returns in the milliseconds it takes to touch the ledger. When the
	// manager is absent (a storeless daemon has no ledger and no pass to
	// run), fall back to the pre-async bare goroutine rather than
	// silently skipping the fill.
	notifyReindex := func(root, path string) {
		if opts.Reindex == nil || path == "" || root == "" {
			return
		}
		if workers != nil {
			if abs, aerr := filepath.Abs(root); aerr == nil {
				_, _, _ = workers.Start("retrieval.reindex", abs, abs+"\x00code", map[string]string{"corpus": "code", "trigger": "write-hook"})
				return
			}
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), reindexTimeout)
			defer cancel()
			_, _ = opts.Reindex(ctx, root, "code", "", false, nil)
		}()
	}
	// wrapRetrievalStatus reads the running figure from the SAME ledger
	// row the pass wrote, per corpus (the per-corpus figure, O4, is true
	// of the runner as well as the numbers). A queued or running row
	// marks its corpus Reindexing; its progress fraction becomes Percent.
	// An unreported pass stays unreported — the row's progress is NULL,
	// not a fabricated 0 (three-state honesty). A nil seam stays nil
	// (V13), and a storeless daemon (no manager) leaves the figure as
	// the seam reported it.
	wrapRetrievalStatus := func(seam search.StatusFunc) search.StatusFunc {
		if seam == nil {
			return nil
		}
		return func(ctx context.Context, root string) (search.StatusAnswer, error) {
			a, err := seam(ctx, root)
			if err != nil {
				return a, err
			}
			if workers != nil {
				abs, aerr := filepath.Abs(root)
				if aerr == nil {
					if lr, lerr := workers.ListViews("retrieval.reindex", abs, nil, 0); lerr == nil {
						for i := range a.Corpora {
							for _, v := range lr.Workers {
								if v.Metadata["corpus"] == a.Corpora[i].Corpus {
									if v.State == "running" || v.State == "queued" {
										a.Corpora[i].Reindexing = true
									}
									if v.Progress != nil {
										a.Corpora[i].Percent = int(*v.Progress * 100)
									}
								}
							}
						}
					}
				}
			}
			return a, nil
		}
	}
	table.RetrievalStatus = wrapRetrievalStatus(opts.CorpusStatus)
	table.RegisterRetrievalStatus()
	// The seam field the door's registration keys on: the raw reindex
	// function. The retrieval.reindex door starts the manager's worker
	// (which runs this same seam) and returns a handle in milliseconds —
	// it is not called inline. The field is the "absent not refusing"
	// (V13) gate: a no-retrieval build has no seam and the door goes
	// unregistered with it.
	table.RetrievalReindex = opts.Reindex
	table.RegisterRetrievalReindex()
	// S54 T2: the priced preview rides the SAME estimate walk as its own
	// seam's refusal (SP2) and registers on the same V13 gate — the
	// no-retrieval build never sets it. It is a READ: no actor, no audit
	// (SP6 draws the line at the acts, which were T1's).
	table.RetrievalPlan = opts.RetrievalPlan
	table.RegisterRetrievalPlan()
	// S18 T3: the probe cache door. Nil store ⇒ nil cache ⇒ routes.list
	// carries no states, `ask` has no contradiction to consult and
	// routes.probe answers honestly that it cannot store — every honesty
	// path already in those handlers, no extra wire needed.
	table.Probes = NewProbeCache(opts.Store)
	table.Memory = memory(opts.Store)
	if opts.Store != nil {
		table.Components = NewComponentStore(opts.Store)
		table.TriggerReads = NewTriggerReads(opts.Store)   // S40 T7: reads (AU14)
		table.TriggerWrites = NewTriggerWrites(opts.Store) // S40 T8: human-only writes (AU1)
	}

	// S5: the runtime and the narrow store surface the `step` handler writes
	// through (K22). The runner is stateless except for the S10 policy fetch
	// (a function field, wired with the store), so it is always wired; the
	// store surface follows the store, so a storeless daemon answers
	// ErrStoreUnavailable rather than running a step it cannot record.
	var runner protocol.StepRunner = StepRunner{
		// S22 T7 (V7/R6): the retrieval resolver, the SAME function value the
		// `search` tool and `search.query` hold — resolution at declaration is
		// the third door onto one index. Nil in the no-retrieval build; the
		// step door then REFUSES a contextRetrievals declaration by name.
		Search: opts.Search,
		// S22 T8: the write trigger — the loop signalling its own writes
		// (V8/Q7), async and never delaying the step (V18/D10), and the
		// same status/reindex values the socket doors hold (D15).
		CorpusStatus:     table.RetrievalStatus,
		Reindex:          table.RetrievalReindex,
		OnWorkspaceWrite: chainHooks(notifyReindex, notifyGraph),
		StepMCP:          opts.StepMCP,
		// S59 T5: no store, no environments — nil here is what makes bash
		// inert in this build, and it is a value rather than an omission so
		// the reader sees the seam was considered and left unwired on purpose.
		ExecResolver: withVault(newStepExecResolver(opts.Store), opts.Vault),
	}
	table.Steps = stepStore(opts.Store)
	// S33 T2: the capture seam. The SAME store-backed adapter the socket's
	// runs carry (below, via Table.Telemetry) and the recovery path carries
	// (daemon/recover.go) — one value, both doors (P34's same-value rule,
	// S30's graph precedent: an identity test, not a convention). A
	// storeless daemon keeps nil: the feature is OFF, I2.
	table.Telemetry = newTelemetrySink(opts.Store)
	// S33 T8: the four telemetry READS (telemetry_reads.go) — the reading
	// twin of the writing seam above, same store, opposite direction.
	table.TelemetryReads = newTelemetryReadStore(opts.Store)
	// S54 T2: the platform plane's two composed reads (platform_reads.go).
	// Every block of the panel is the SAME seam value its own door serves
	// — the wrapped status above, the graph service from the graph block,
	// the telemetry reads on this line, the ledger the running-figures
	// merge already reads — so the panel and the per-subsystem doors can
	// not disagree, which is SP1's whole claim (platform_reads.go builds
	// it; the composer in internal/platformview owns shape only). The
	// adapters return nil (⇒ method ABSENT, V13) when there is nothing to
	// compose over: a storeless, retrievalless, graphless daemon has no
	// panel and no ledger, and says so with an absent method.
	table.PlatformChanges = NewPlatformChanges(opts.Store)
	table.RegisterPlatformChanges()
	// S54 T4: the deps are a VALUE kept beside the panel's adapter, because
	// the agent's index_status arm is built over this exact same set — one
	// set of seams, two doors, and no second list that could drift from the
	// first (records_platform.go answers through it).
	platformDeps := PlatformDeps{
		Store:     opts.Store,
		Status:    table.RetrievalStatus,
		Graph:     table.Graph,
		Telemetry: table.TelemetryReads,
		Workers:   workers,
	}
	table.PlatformStatus = NewPlatformStatus(platformDeps)
	table.RegisterPlatformStatus()
	// S44 T5: the settings surface. The ONE shared *config.Store the daemon
	// holds (opts.Settings, built by NewSettings at the entry point, T4) is
	// the seam the five settings.* methods read and write through — the same
	// value both doors share (P34, CS11). settingsService is the nil guard
	// (V13): a storeless daemon keeps nil, so RegisterSettings leaves
	// settings.* ABSENT, not registered-and-refusing. The mapping
	// (config.View → the protocol's wire twins) lives in the handler; this is
	// wiring, not translation.
	table.Settings = settingsService(opts.Settings)
	table.RegisterSettings()
	// S43-extra T6: the seed surface. The ONE *seed.Engine the daemon holds
	// (opts.Seed, built by NewSeedEngine at the entry point over the store)
	// is the seam the four seed.* methods read and write through — the same
	// instance the startup inert apply (ApplyInertAtStart) reads, so the
	// human door and the startup door share one truth (P34). *seed.Engine
	// satisfies protocol.SeedService field-for-field, so the wiring is a
	// nil-guard (a storeless daemon keeps nil, so RegisterSeed leaves
	// seed.* ABSENT, V13 — mirroring the settings wiring above).
	// S45 T12: the nil-guard now wraps — corpus applies gain their actor
	// rule and their per-run seed audit row through the wrapper's Apply;
	// every other call rides the engine exactly as before.
	table.Seed = newCorpusSeedDoor(opts.Seed, opts.Store)
	table.RegisterSeed()
	// S56 T6: the autonomy human door. The store the ten autonomy.* methods
	// read and write through (autonomy.go) — the bridge between the store's
	// rows and the pure internal/autonomy core, the same value the start
	// path and the decision site consult (AL14: one resolver). NewAutonomy-
	// Service is the nil guard (V13): a storeless daemon keeps nil, so
	// RegisterAutonomy leaves the ten ABSENT, not registered-and-refusing —
	// mirroring the settings and seed wiring above.
	table.Autonomy = NewAutonomyService(opts.Store)
	table.RegisterAutonomy()
	table.Questions = NewQuestionStore(opts.Store)
	table.Proposals = proposalStore(opts.Store)
	table.ValidateProposal = opts.ValidateProposal
	// S31 T4: the steer queue. The SAME StepStore object answers the socket's
	// three steer methods (it now satisfies protocol.SteerStore); the factory
	// binds each running step's drain to its own queue; the state read is
	// what makes LW4's refusals name a real status. A storeless daemon gets
	// nil across the board — handlers refuse by name, loops run unsteered.
	// S68 B6: the steer door's chosen-skill check and the drain's render
	// both run through the same mounted catalog every other skills door
	// reads. One resolver closure, two doors, one truth.
	chosen := chosenCatalogResolver(opts.Skills)
	if ss := NewStepStore(opts.Store); ss != nil {
		table.SteerStore = ss // the concrete type carries the steer methods; the
		// protocol.StepStore interface deliberately does not
		ss.resolveChosen = chosen
	}
	table.Steers = steerSourceFor(opts.Store, opts.Routes, chosen)
	// S70 MB4: the mailbox's two doors, same factory shape as the steers' —
	// a storeless daemon gets nil and the loop runs byte-identical to the
	// pre-mailbox path (I2). Scope, lanes and sender all come from the
	// step's row read at the moment of use, never captured here.
	table.MailboxDrain = mailboxDrainFor(opts.Store)
	table.MailboxAsk = mailboxAskFor(opts.Store, nil) // announce wired below, once d exists
	table.AgentMessages = mailboxReadFor(opts.Store)
	table.StepSteerState = stepSteerState(opts.Store)
	table.HandlerForStep = handler.Build
	// S14 T10: the agent-services socket seam, wired the same way as
	// AgentInvoke above — from the entry point, never from here.
	table.Agents = opts.Agents
	table.Plugins = opts.Plugins
	table.AgentImportPreview = opts.AgentImportPreview
	table.AgentManage = opts.AgentManage
	table.AgentRoster = opts.AgentRoster
	table.ToolGrants = opts.ToolGrants
	table.MCP = opts.MCP
	// S46 T3: the management reads register iff the seam is wired (the
	// RegisterSettings discipline beside the assignment at the Settings
	// line above; V13's absence is the configuration speaking).
	table.MCPAdmin = opts.MCPAdmin
	table.RegisterMCPAdmin()
	// S47 T5: the person's tools catalog — the four READS (`tools.list` ·
	// `tools.get` · `tools.byAgent` · `tools.status`) over the code, the
	// `tool` rows, the grant chain and the policy set, joined ONCE by
	// `internal/toolcatalog` (TC5: there is no second answer to "may this
	// agent call this tool", and a panel that assembled its own would say
	// allowed while the step refuses). Zero writes (TC1), the drift reported
	// and never repaired (TC11). The nil guard is the constructor (V13): a
	// storeless daemon hands nil, so the four doors are ABSENT rather than
	// registered-and-refusing. The MCP half arrives through the seam above —
	// the daemon never imports `internal/mcp` (MC11).
	table.ToolsCatalog = newToolsCatalogService(opts)
	table.RegisterToolsCatalog()

	// S47 T6 (TC6/TC8/P34): the SURFACE doors — `surface.list` and `surface.get`
	// answer what this daemon exposes and to whom, from the closed classification
	// table and the doors computed from the live structures. They read the table
	// itself, so there is no seam whose absence could make them refuse: what makes
	// a method absent is a conditional family that was never wired, and the answer
	// says so by omitting it. The narrow door arrives through Table.NarrowMethods,
	// a func value the entry point supplies — internal/narrow imports this package,
	// so the direction stays one-way and the daemon never learns HTTP exists.
	table.RegisterSurface()
	table.AgentTransition = opts.AgentTransition

	// S31 T6: the read-only git doors. The registry check is shared with the
	// T7 terminals (both open only on registered roots); the bounded git
	// reads delegate whole to `internal/workspace` — the timeout, the output
	// caps and the group-kill live there, never here. The three states
	// (`ok` | `not-a-repo` | `unavailable` + reason) travel as data.
	table.ComponentRootKnown = componentRootKnown(opts.Store)
	table.GitStatus = gitStatusReader()
	table.GitDiff = gitDiffReader()
	table.GitChanges = gitChangesReader()
	// Closures, not method values: `d` is assigned further down in New, and a method value bound here would capture
	// the nil it holds now (every call would dereference it).
	table.SetLoopOptions = func(ctx context.Context, p protocol.LoopOptionsParams) (protocol.LoopOptionsResult, error) {
		return d.setLoopOptions(ctx, p)
	}
	table.ListLoopDocuments = func(ctx context.Context, p protocol.LoopDocumentsParams) (protocol.LoopDocumentsResult, error) {
		return d.listLoopDocuments(ctx, p)
	}
	table.SetLoopArchived = func(ctx context.Context, p protocol.LoopSetArchivedParams) (protocol.LoopSetArchivedResult, error) {
		return d.setLoopArchived(ctx, p)
	}
	table.DeleteLoop = func(ctx context.Context, actor protocol.Actor, p protocol.LoopDeleteParams) (protocol.LoopDeleteResult, error) {
		return d.deleteLoop(ctx, actor, p)
	}
	table.ListLoopTombstones = func(ctx context.Context, p protocol.LoopTombstonesParams) (protocol.LoopTombstonesResult, error) {
		return d.listLoopTombstones(ctx, p)
	}

	// S10: the policy gate. A store exists ⇒ the gate exists: `aeon serve`
	// always passes a store (it opens before the daemon is built), so
	// production never runs ungated — TestProductionDaemonWiresThePolicyGate
	// asserts it on a booted table. The storeless daemon remains the test
	// mode it always was: nil gate, no enforcement, exactly as the nil store
	// means ErrStoreUnavailable rather than a panic.
	//
	// The gate now arrives as a PAIR (T10): the live gate for first runs, the
	// frozen-set CONSTRUCTOR for replays, and the runner's once-per-step
	// policy fetch. Any one of the three present without the others is a
	// wiring defect (live-gate-only replays against today's table — D12's
	// drift), which is exactly what TestProductionDaemonWiresThePolicyGate
	// now pins: all three, or none.
	// S38 T15 (OL10): ONE records service for the slice, wired twice below —
	// as the tools' seam (a step's door) and as the table's Records half (a
	// person's door). Two doors, one implementation.
	var recordsSvc *RecordsService
	// S75: the loop's persisted todo list — one service, three doors (the tools'
	// seam, `loop.todos`, the person's close). Nil without a store.
	var todoSvc *TodoService
	var docSvc *DocService
	var envSvc *EnvService
	if opts.Store != nil {
		docSvc = NewDocService(opts.Store)
		// The variable manager: the person's `vault.*` doors and the agents'
		// env_* tools over ONE vault. Absent without a key (V13).
		if opts.Vault != nil {
			table.Vault = vaultAdapter{v: opts.Vault, st: opts.Store}
			table.RegisterVault()
			envSvc = NewEnvService(opts.Vault, opts.Store)
		}
		todoSvc = NewTodoService(opts.Store)
		// S75 T3: the loop's todo list on the socket (absent without a store, V13).
		table.LoopTodos = loopTodosAdapter{svc: todoSvc}
		table.TodoPolicy = todoPolicyFactory(todoSvc, opts.Settings)
		table.RegisterLoopTodos()
		table.LoopQuestions = loopQuestionsAdapter{st: opts.Store}
		table.RegisterLoopQuestions()
	}
	if opts.Store != nil {
		table.PolicyGate = NewPolicyGate(opts.Store, opts.Logger, WithUnmatchedSetting(opts.Settings))
		table.PolicySubject = bindPolicySubject
		table.PolicySet = func(frozen runtime.FrozenManifest) runtime.PolicyGate {
			return NewStepPolicyGate(opts.Store, opts.Logger, frozen, WithUnmatchedSetting(opts.Settings))
		}
		// T13: the app's read window over the SAME boundary the gate reads —
		// one store, one gate, so the app is never shown a set the
		// enforcement is not using. Over the same RepoFuncs bridge; the
		// four policy.* reads answer ErrStoreUnavailable on a storeless
		// table, which is the same honesty the store methods carry.
		table.PolicyStore = policy.NewStore(newPolicyRepo(opts.Store))
		// S45 T3 (PM8): the authoring audit's read door. The audit is a
		// separate table read through its own read-only seam (the adapter
		// lives in policy_change_seam.go, the proposalStore pattern) — NOT
		// the boundary above, because the gate's family must stay blind to
		// it (PM11/TD2: the audit is downstream of every decision, never
		// upstream of one). Storeless tables keep nil: `policy.changes`
		// answers ErrStoreUnavailable, the same honesty as every policy
		// read above.
		table.PolicyChanges = policyChangeStore(opts.Store)
		// S45 T4 (PM1): the authoring doors on the human surface. The SAME
		// store, the SAME RepoFuncs bridge and the SAME boundary type the
		// gate and the reads use — the row this door writes is the row the
		// CLI's door writes, because both pass through the one Add (PM2/
		// PM4's one-implementation ruling), and every successful write
		// lands exactly one policy_change row through the T3 accessors
		// (PM8). Registration is CONDITIONAL in the wiring — the seam nil
		// (storeless daemon) leaves the three methods ABSENT, never
		// registered-and-refusing (V13); with a store the methods are in
		// iff RegisterPolicyAuthoring says so, and the actor is
		// table.ActorFor's peer stamp, refused by name when unresolvable
		// (never a "0"). The seed verb rides the engine path (opts.Seed,
		// the one engine every door holds — T12); a seedless mount refuses
		// it by name while add/remove stand.
		table.PolicyAuthoring = newAuthoringService(opts.Store, opts.Seed, opts.Logger)
		table.RegisterPolicyAuthoring()
		// S45 T5 (PM1/PM7): the proposal decision doors — the socket's
		// human door onto desk.Commit/desk.Reject, CONDITIONAL in the
		// wiring (the seam nil ⇒ the three methods ABSENT, never
		// registered-and-refusing — V13, the authoring pattern). The actor
		// is table.ActorFor's peer stamp (refused at uid 0, never a "0");
		// the effect table arrives per decision from opts.Effects —
		// cmd/aeon's one registry expression (PM4). S45 T10: the door also
		// receives S37's preview seam (already threaded to the table above
		// for create) — the agent_import commit evidence is rendered
		// through it, and the daemon still links no plugin code.
		table.ProposalDecider = newProposalDecideService(opts.Store, opts.Effects, opts.AgentImportPreview)
		table.RegisterProposalDecider()
		store := opts.Store
		recordsSvc = NewRecordsService(store)
		// S53 T3 (HV4, rule 4): the two history read arms answer through the
		// VERY SAME facade the socket's five history doors hold — one
		// implementation, two doors. A storeless mount keeps the field nil
		// and the arms refuse by name (V13), never an empty listing.
		recordsSvc.historyPlane = table.HistoryStore

		runner = StepRunner{
			Search: opts.Search,
			// S22 T8, as in the storeless literal above.
			CorpusStatus:     table.RetrievalStatus,
			Reindex:          table.RetrievalReindex,
			OnWorkspaceWrite: chainHooks(notifyReindex, notifyGraph),
			PolicyList: func(ctx context.Context, at time.Time) ([]runtime.PolicyEntry, error) {
				entries, rej := PolicyEntriesAt(ctx, store, at)
				if rej != nil {
					return nil, fmt.Errorf("%s", rej.Reason)
				}
				return entries, nil
			}, OperatorPresent: OperatorPresentFor(store, opts.Logger), AutoApprove: AutoApproveFor(store, opts.Logger),
			// T13: the spawn-key seam (G16). The same boundary, the same
			// bridge and the same gate construction as the pair above —
			// one store, one gate, so the verdict the gate decides with
			// and the verdict the token is minted from are one code path.
			CommandVerdict: CommandVerdictFor(opts.Store, opts.Logger, WithUnmatchedSetting(opts.Settings)),
			// S59 T5 (EV5, EV9): the execution-environment seam, from the same
			// store the verdict is minted from — one store, one binding read, so
			// the environment a command runs in is the one bound to the root its
			// verdict was decided for.
			ExecResolver: withVault(newStepExecResolver(store), opts.Vault),
			// S18 T3 (O3, D6): the step's use-time probe check, same
			// pattern as the pair above — one store, and the verdict
			// rules stay in model (Route.FirstContradiction); this is
			// only the wiring that lets the step consult them.
			ContradictionCheck: ContradictionCheckerFor(opts.Store, opts.Logger),
			// S18 T5 (M4/O1): the thinking-drop count files itself into
			// the step's row at completion — the record is where a
			// dropped block stays visible, not a log line.
			ThinkingCount: ThinkingRecorderFor(opts.Store, opts.Logger),
			// S38 T9: the management catalog's one implementation, injected as
			// the tools' Records seam. Identity is stamped from the step
			// (stepctx), never from tool arguments; a STORELESS daemon leaves
			// it nil and every catalog tool answers "records are unavailable
			// in this runtime" — inert, never inventing (fail-closed).
			Records:            recordsSvc.Invoker(),
			Todo:               todoSvcOrNil(todoSvc),
			Docs:               docServiceOrNil(docSvc),
			LoopContext:        NewLoopContextService(opts.Store, docSvc),
			Env:                envServiceOrNil(envSvc),
			TodoActive:         todoActiveOrNil(todoSvc),
			MaxIterations:      stepMaxIterationsFrom(opts.Settings),
			AgentMaxIterations: agentMaxIterationsFrom(opts.Store),
			Deadline:           stepDeadlineFrom(opts.Settings),
			StepMCP:            opts.StepMCP,
		}
	}
	table.Runner = runner

	// Routes survive a restart (S4, closing the promise S3 made and S4 first
	// shipped without). Attached here rather than at construction of the routes
	// themselves, because this is the only place that has both the store and
	// the route set.
	//
	// A load failure is RETURNED to the caller through Options.OnRouteLoadError
	// rather than swallowed: starting with no routes when some are stored looks
	// exactly like "my configuration vanished", which is the bug this mechanism
	// exists to fix.
	if mutable, ok := opts.Routes.(*protocol.MutableRoutes); ok && opts.Store != nil {
		if err := mutable.WithPersistence(NewRouteStore(opts.Store)); err != nil && opts.Logger != nil {
			opts.Logger.Infof("stored routes could not be loaded, so this daemon starts with only "+
				"the environment's: %v", err)
		}
	}
	table.DaemonVersion = opts.DaemonVersion
	table.BuildTime = opts.BuildTime
	table.GitCommit = opts.GitCommit

	d = &Daemon{
		Table:           table,
		remoteCallbacks: opts.RemoteCallbacks,
		notify:          opts.Notify,
		connections:     opts.Connections,
		log:             opts.Logger,
		store:           opts.Store,
		settings:        opts.Settings,
		recovering:      make(map[int64]struct{}),
		stepCancels:     newStepCancels(),
		turns:           make(map[string]struct{}),
		agentInvoke:     opts.AgentInvoke,
	}
	// S27 T7: the scheduler's twelve doors, wired over the CONSTRUCTED
	// daemon — the service reads the blueprint source, the S55 stop seam
	// and the worker through d, so it is built after the literal, not
	// beside the store-keyed seams above (measured the hard way: the
	// census TestSurfaceEveryEntryNamesARegisteredMethod names the twelve
	// when this wiring silently registers nothing). The nil guard (V13)
	// stays: a storeless daemon keeps nil and RegisterContinuous leaves
	// all twelve ABSENT — scheduler.stopAll most of all, since it has no
	// meaning without the loop plane under it.
	table.Continuous = NewContinuousService(d)
	table.RegisterContinuous()
	if d.notify == nil {
		d.notify = localrpc.Notify
	}
	if d.connections == nil {
		d.connections = localrpc.Connections
	}
	if d.log == nil {
		d.log = discardLogger{}
	}
	// S55 T5: the loop-control plane. The seven socket doors (stop, resume,
	// cancel, control, requestStop, control.requests, control.dismiss) over
	// the T3 control core and the T2 store — the daemon's own hooks, its own
	// publish, and the actor the daemon's ActorFor seam stamps from the
	// kernel's peer credential (LC5: never a param). The nil guard is the
	// constructor (V13): a storeless daemon keeps nil, so RegisterLoopControl
	// leaves the seven ABSENT, not registered-and-refusing — a daemon with
	// no loops has no control plane, and the socket says so by absence.
	table.LoopControl = d.newLoopControlService()
	table.RegisterLoopControl()
	// S55 T8: the budget plane. The eight socket doors (list, show,
	// effective, spend.report, set, remove, vocabulary, loop.raise) over
	// the T6 store and the T7 raise op — the views assembled on the daemon,
	// the confirmation bound to what was read (PM7.1), the actor stamped
	// from the kernel's peer credential (LC5: never a param). The nil guard
	// is the constructor (V13): a storeless daemon keeps nil, so
	// RegisterBudget leaves the eight ABSENT, not registered-and-refusing.
	table.BudgetStore = d.newBudgetService()
	table.RegisterBudget()
	// S58 T2: the attention plane's two reads (SV1, SV13) over the daemon's
	// inbox service — the registry of live sources, the door check wired to
	// THIS table's live method set, the T1 ack read behind the hide/show
	// seam. A storeless daemon leaves nil and RegisterInbox keeps both
	// ABSENT (V13).
	table.Inbox = nil
	table.InboxWrite = nil
	if svc := d.newInboxService(); svc != nil {
		table.Inbox = svc
		table.InboxWrite = svc // the same service: reads evaluate, writes mark (T3)
		d.Inbox = svc
	}
	table.RegisterInbox()
	table.RegisterInboxWrites()
	// S58 T6: mission.status (SV11) — the per-root overview built on THIS
	// table's own inbox service (the needs-you equality is by construction,
	// not by comparison), the store's rollups, and the two seams S55/S27
	// open through RegisterBudgetReader/RegisterNextReady. No store or no
	// inbox service leaves the method ABSENT (V13).
	table.Mission = nil
	d.missionSvc = nil
	if svc := d.newMissionService(); svc != nil {
		table.Mission = svc
		d.missionSvc = svc
	}
	table.RegisterMission()
	// S58 T6 (SV16): the records catalog's five supervision reads answer
	// through the SAME services the socket doors use — own-view is applied
	// inside the family (identity, not parameter), and a bare storeless
	// mount has no records service to attach at all.
	if recordsSvc != nil {
		recordsSvc.supervision = &daemonSupervision{d: d}
	}
	// S49 T3: the unified automation reads (AN1, AN7) over the daemon's
	// adapter — ONE view of both families' rows, their fire log, runs,
	// audit, health and previews. Built only with a store: a storeless
	// daemon leaves the field nil and RegisterAutomationReads keeps all
	// ten ABSENT (V13), never registered-and-refusing.
	if reads := d.newAutomationReads(); reads != nil {
		table.AutomationReads = reads
		// S49 T6: the automation_disable preview rides the SAME read
		// service (Describe, fires, health — the desk's own answers over
		// the proposal's payload, read-only, advisory: proposal.get omits
		// it, never fails, when this seam is nil).
		table.AutomationPreview = reads.ProposalPreview
	}
	table.RegisterAutomationReads()
	// S49 T4: the write plane — the S20 control seams re-wired to carry
	// the connection's person, the fire-now audit, and the three new
	// doors (trigger.update, automation.archive, automation.unarchive).
	// Nil store ⇒ the seam stays nil ⇒ the doors are ABSENT (V13).
	d.wireAutomationWrites()
	table.RegisterAutomationWrites()
	// S58 T4: the class resolver replaces the deleted notifyHintFor
	// switch — every publish resolves its delivery-time hint from the
	// seeded class table (SV4: no cache, one indexed read per event;
	// a nil resolver for a storeless daemon attaches nothing, the V13
	// stance). The three class doors ride the same service (V13:
	// registered iff wired).
	d.classes = nil
	table.NotifyClass = nil
	if d.store != nil {
		d.classes = notify.NewResolver(d.store)
		svc := &NotifyClassService{store: d.store, resolver: d.classes}
		table.NotifyClass = svc
	}
	table.RegisterNotifyClasses()
	// S58 T5: the channel plane and the dispatcher (SV7/SV8). The seam
	// is built only with a store — a storeless daemon has no channel
	// doors (V13) and no sender goroutines (zero channels means zero
	// notification connections, and a storeless daemon cannot even
	// answer "which channels?"). The scope resolver registration installs
	// the tree answer the subscription filter reads (fail-closed until
	// then: a subscription never matching beats a guessed tree).
	d.channels = nil
	table.NotifyChannel = nil
	if d.store != nil {
		d.channels = d.newNotifyChannels(opts.DaemonVersion)
		table.NotifyChannel = d.channels
	}
	table.RegisterNotifyChannels()
	// S55 T9: the watchdog service — the one instance serve's RunWatchdog
	// ticker and the watchdog.* doors below share (the once-per-episode
	// memory is the service's state, not a per-door local). Built only with
	// a store (V13): a storeless daemon never ticks and its doors are
	// absent, not registered-and-refusing.
	if d.store != nil {
		d.watchdog = d.newWatchdogService()
		// Assigned INSIDE the guard: a nil *watchdogService on the
		// WatchdogStore field would be a non-nil interface, and
		// RegisterWatchdog would then register six doors that panic on
		// the first call (V13 wants the six ABSENT on a storeless mount).
		table.Watchdog = d.watchdog
		table.RegisterWatchdog()
	}
	// S55 T7: the runtime's in-step budget seam (E2) — the same store-backed
	// verdict the loop engine's pre-dispatch check reads (one seam, two
	// boundaries). Nil on a storeless daemon (V13): no store, no budget —
	// the check is absent, not registered-and-refusing, the same
	// absence-is-off the control plane's doors follow.
	if d.store != nil {
		table.Budget = func(ctx context.Context, loopID string) (runtime.BudgetVerdict, error) {
			v, err := d.budgetVerdictFor(ctx, loopID)
			if err != nil {
				return runtime.BudgetVerdict{}, err
			}
			return toRuntimeVerdict(v), nil
		}
	}
	// S64: the manager's event voice is the daemon's own publish (durable
	// event-log append + subscriber fan-out), attached once the Daemon
	// exists — the manager only calls it at runtime, and Serve has not
	// opened the socket yet at this point.
	d.workers = workers
	d.skills = opts.Skills // S65: the catalog was registered off the same value above.
	// S68 B6: one resolver over the mounted catalog answers every chosen-
	// skills door (the two steer doors above and the reload below).
	d.resolveChosen = chosen
	if workers != nil {
		publishEvents = d.publish
	}

	// S40 T8 (AU1) stamped the person behind the trigger-write doors here;
	// S54 T1 MOVED the assignment above the store branch, ahead of the
	// doors whose V13 registration gate reads ActorFor (graph.build at
	// ~470, retrieval.reindex at ~684, mcp.importFile below). Same
	// closure, same meaning: the uid is the kernel's, not a claim, no
	// path constructs Kind "agent", and a connection the transport never
	// recorded is uid 0 — refused at the audited doors, never dressed in
	// a real-looking identity.
	// S46 T4: mcp.importFile is a WRITE door — it registers only once the
	// actor stamp exists (RegisterMCPImportFile checks both seams),
	// which is why it registers HERE and not beside the five reads: an
	// MCP configuration write without a nameable peer is absent, never a
	// refusing half-wired door (V13).
	table.RegisterMCPImportFile()
	// S46 T5: the seven human WRITE doors ride the same seam (registered
	// iff MCPAdmin is wired; the actor is stamped per call at the dispatch
	// edge exactly like the import door — an unresolvable peer is refused
	// with ErrMcpNoActor, nothing half-written).
	table.RegisterMCPWrites()

	// S38 T15 (OL10/OL11): the human's half of the two doors. The socket
	// speaks the catalog through the SAME service the tools call, and the
	// board's task.changed fires from here — after every committed task
	// move, never inside a store transaction. A storeless daemon leaves
	// table.Records nil: every projected method then answers the third
	// state, "records are unavailable in this runtime", inert by design
	// (fail-closed), never inventing an answer of zero.
	if todoSvc != nil {
		todoSvc.publish = d.publish
	}
	if recordsSvc != nil {
		recordsSvc.taskChanged = d.emitTaskChanged
		// S43-extra T6: the seed_status tool reads through the SAME *seed.Engine
		// the socket's seed.status reads (table.Seed = opts.Seed), so the two
		// doors answer identically. A seedless mount leaves it nil, and the
		// arm refuses rather than answering zero (V13).
		recordsSvc.seedEngine = opts.Seed
		// S45 T7 (PM9): the seven policy/proposal catalog arms read and
		// write through THE SAME handles the socket's policy/proposal
		// methods use — table.PolicyStore (the gate's own store, wired
		// over the one RepoFuncs bridge), table.Proposals (the adapter
		// proposal.create writes through) and opts.ValidateProposal (the
		// seam proposal.create validates with — table.ValidateProposal's
		// own value). One implementation, two doors; a mount missing one
		// leaves it nil and the arm refuses (V13).
		recordsSvc.policyStore = table.PolicyStore
		recordsSvc.proposals = table.Proposals
		recordsSvc.validateProposal = opts.ValidateProposal

		// S54 T4 (SP4): the platform family's one agent read — freshness of
		// the STEP'S OWN root, composed over the very platformDeps the panel
		// above composes over (the same wrapped status seam, the same graph
		// service, the same ledger question). A retrievalless, graphless
		// mount gets nil from the constructor (freshness OF NOTHING is no
		// door), and the arm refuses by name (V13).
		recordsSvc.platformIndex = NewPlatformIndexFreshness(platformDeps)

		// S47 T7 (TC9): the two own-view tool arms read through the SAME catalog
		// service the socket's four `tools.*` doors answer through — one
		// `toolcatalog.ForAgent`, one grant chain, so the agent's `tools_mine`
		// and the person's `tools.byAgent` cannot answer one question twice. The
		// assertion is a check, not a cast: a nil service must leave the field
		// NIL, because a typed nil inside the interface field would read as
		// "wired" and the arms would answer a view from a mount that has none.
		if tc, wired := table.ToolsCatalog.(*toolsCatalogService); wired && tc != nil {
			recordsSvc.toolsCatalog = tc
		}
		// S56 T8 (AL11): the three autonomy catalog reads answer through
		// THE SAME protocol.AutonomyService the socket's autonomy.*
		// methods read through (wired above, T6) — one implementation, two
		// doors. A mount without the seam leaves it nil and the arms
		// refuse (V13). The two propose arms do not use it: they create
		// proposals through the shared path beside.
		recordsSvc.autonomyService = table.Autonomy
		// S46 T8: the five mcp.mine.* arms enter the SAME admin seam the
		// table's mcp.* methods hold — one seam, two doors (the reads
		// forward with self stamped; nil seam ⇒ the arms refuse, V13).
		recordsSvc.mcpAdmin = opts.MCPAdmin
		// S55 T10: the seven loop-control / budget / watchdog arms read
		// through the SAME Table seams the socket's doors call (wired above:
		// table.LoopControl / table.BudgetStore / table.Watchdog), so the
		// tool and its twin share one implementation and cannot drift.
		recordsSvc.d = d
		d.Table.Records = newSocketRecords(d, recordsSvc)
		// S45 T7: with the records service now wired, the two propose twins
		// register — CONDITIONALLY, the RegisterSeed rule (V13): no
		// service, no method. registerRecords skips both names; this call
		// owns them (policy_propose.go says why the catalog's first
		// MUTATING specs are the conditional kind).
		table.RegisterPolicyPropose()
		// S46 T8 (MG8/MC2): the five agent-scoped mcp.mine.* twins register
		// now that BOTH services stand — the records service (their Do) and
		// the MCP admin seam (their scoped rows half). Conditional the same
		// way (V13): a daemon without the MCP seam never speaks them, and
		// TestMcpAdminAbsentWhenSeamNil's count holds on every mount.
		table.RegisterMCPMine()
		// S56 T8 (AL11): the autonomy propose twins register on the same
		// rule — conditional on the records service, owned by this call,
		// skipped by registerRecords (all five autonomy names — the three
		// reads are owned by RegisterAutonomy above, and the registration
		// order makes the explicit skip necessary; records.go records the
		// deviation).
		table.RegisterAutonomyPropose()
		// S57 T9 (VG19): the three gate twins (verify.proposeAdd ·
		// mergerule.proposeTighten · dod.markAsAgent) register on the same
		// rule — conditional on the records service, owned by this call,
		// skipped by registerRecords (the gate family's skip is by family,
		// so the six read names stay T8's to own and only the three twin
		// names are new). The twins' arms refuse when a gate seam is
		// unwired (V13); on this daemon a storeless mount has no records
		// service, so this call registers nothing there either.
		table.RegisterGatePropose()
		// S48 T10 (BP9): the blueprint propose twin registers on the same
		// rule — conditional on the records service, owned by this call,
		// and registerRecords skips the WHOLE blueprint family (the four
		// reads are owned by RegisterBlueprintReads above — the registration
		// order makes the explicit skip necessary; records.go records the
		// deviation, the autonomy shape carried).
		table.RegisterBlueprintPropose()
		// S39 T10: the daemon keeps the service it built so the wiring
		// beside the runner can hand it the run door once the runner
		// exists — the ONE loop action an agent may take (CR5), and it
		// must land on the SAME startRun the human door answers with. A
		// storeless mount has no service, no run door, and says so.
		d.recordsSvc = recordsSvc
	}

	// S31 T7: the terminal registry. It needs NO store — the door is the
	// project registry (a storeless daemon therefore refuses opens by name,
	// because no root can be confirmed as registered), and output goes
	// straight from the manager to the transport, never through the EventLog
	// (LW7). The reaper runs from birth; Shutdown stops it before CloseAll.
	d.events = events
	d.terminals = d.initTerminals()

	// S20 T2: the clock arrives beside the reaper. Its gates start UNWIRED
	// and the scheduler says so out loud at Run — T3 replaces Overlap, T7
	// replaces Access; a scheduler that silently assumed "no overlap" or
	// "governance passed" from an absent hook would be the fourth answer
	// wearing a mask (P31).
	if opts.Store != nil {
		sc := trigger.NewScheduler(automationRows{triggerRows{opts.Store}}, d, trigger.SystemClock{})
		// T3: the overlap hook is WIRED — the previous run's own row
		// answers (Q2: running OR suspended blocks), read live at the
		// instant, never cached in the fire row.
		sc.Gate.Overlap = func(sch trigger.Schedule, at time.Time) (bool, string) {
			prev, status, found, err := opts.Store.LastFiredLoopBefore(context.Background(), sch.ID, at.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
			if err != nil || !found {
				// No previous fire is the common answer ("found=false"),
				// and it does not block. A FAILED read proceeds too, with
				// eyes open: the store is then broken in a way the fire's
				// own start path will fail on immediately (honest failure
				// downstream, no loop started on a half-truth) — and
				// blocking on an unreadable row would let one bad read
				// silence a schedule forever, which is the cost asymmetry
				// that decides the direction here.
				return false, ""
			}
			return trigger.StillGoing(status), prev
		}
		// T7: the governance hook is WIRED — TriggerAccess decides beside
		// the store, the same rows the policy gate reads (one place
		// permission lives). The consequence is stated where it bites: a
		// FRESH install has no trigger_access allow, so PL5's default deny
		// fires NOTHING — every schedule answers `refused` with the
		// engine's own sentence in the fire record until an operator
		// authors an allow. That is the empty-policy state, not a bug;
		// `aeon policy add trigger_access ...` is the switch. An unreadable
		// set refuses too (fail closed — governance guesses nothing), and
		// an ASKED verdict refuses as well: there is no gate to route a
		// trigger fire's question to, and a question nobody can answer is
		// not a permission (the sentence still says it was asked).
		polStore := policy.NewStore(newPolicyRepo(opts.Store))
		sc.Gate.Access = func(provenance, triggerID string) (allow bool, reason string) {
			// Governance never crashes the clock. A closed or half-torn-down
			// store can fail this read two ways: as a REJECTION (the honest
			// shape, which is what `store.Close` now answers — a closed pool
			// says `database is closed` instead of segfaulting), or as a PANIC
			// through the repo callbacks (measured, T7's fail-closed test,
			// before the store was fixed). Both are one thing to governance: a
			// set that could not be read. Recover stays as the belt, because a
			// governance read that takes the tick goroutine down with it is
			// worse than any refusal — every schedule dies with it. Both arms
			// name the subject they were asked about, so the shape of the
			// failure never changes the words.
			defer func() {
				if rec := recover(); rec != nil {
					allow, reason = false, fmt.Sprintf("refused: agent:%s · trigger_access · the policy read panicked (%v) — refusing, not guessing", triggerID, rec)
				}
			}()
			at := sc.Clock.Now().UTC()
			// The row names the TRIGGER, not its kind (T9 walk finding #3:
			// this read Trigger: provenance, so an allow for "schedule"
			// admitted EVERY schedule — governance was a kind-wide master
			// key wearing a per-trigger subject, and the fixtures mirrored
			// it so units stayed green). Governance says "nightly-survey
			// may fire", never "anything scheduled may fire"; provenance
			// stays out of the match and in the refusal's reach.
			_ = provenance
			req := policy.Request{Type: policy.TriggerAccess, Agent: triggerID, Trigger: triggerID}
			ps, rej := polStore.List(context.Background(), at)
			if rej != nil {
				if d.log != nil {
					d.log.Infof("trigger access: the policy set could not be read (%s); refusing", rej.Reason)
				}
				v := policy.Evaluate(nil, req, at)
				return false, v.Sentence() + " · trigger_access (the policy set could not be read — refusing, not guessing)"
			}
			v := policy.Evaluate(ps, req, at)
			if !v.Allowed {
				return false, v.Sentence()
			}
			return true, ""
		}
		sc.Speak = func(format string, args ...any) {
			if d.log != nil {
				d.log.Infof(format, args...)
			}
		}
		d.triggers = sc

		// S57 T7 (VG5): the post-merge watch is a daemon service too — built
		// on the store like the triggers, started from StartLoops. A daemon
		// without a store has no merge events, and nothing to watch.
		d.initGateWatch()
	}
	reaperCtx, termCancel := context.WithCancel(context.Background())
	d.termCancel = termCancel
	go d.terminals.RunReaper(reaperCtx)
	// S53 T3 (the race fix, measured — see Notes): the merge-queue reactor
	// USED to start here, sixty lines before initBlueprints wires d.bpSrc —
	// a boot-order window in which its first pass, landing a loop through
	// loadStartBlueprint on a store that already held a merge item, read a
	// nil blueprint source and SIGSEGV-killed the process. The reactor
	// starts at the END of boot, below, where every seam it can reach is
	// wired. mergeQueueWake is nil-safe, so nothing between here and there
	// depended on the goroutine being live.
	table.Terminals = terminalDoor{m: d.terminals}

	// Bench demo runner: the handlers' socket reads. No store guard — the
	// registry is embedded (the terminals precedent: a read whose door is
	// not the store).
	d.initHandlerOps()

	// The three hooks that keep `internal/protocol` transport-free: it hands
	// back what happened, and this package decides how it reaches a wire.
	table.OnInitialized = d.onInitialized
	table.OnSubscribed = d.onSubscribed
	// Domain events — `ask.delta` and `ask.done` at S3 — reach the wire the
	// same way the daemon's own lifecycle events do (M23). One streaming path.
	// Step payloads carry their project root inside (protocol.ComponentRooted),
	// so a step stream is appendable to the event log with its REAL root and
	// a subscriber filtered by `Filter{ComponentRoot}` gets one project's steps
	// and not the machine's (S31 D1; CODEMAP (g): an empty root matches every
	// subscriber, so "empty" for a step event is a leak, not a default).
	// Everything that does not implement the interface — daemon lifecycle,
	// `ask.*` — has genuinely no project and publishes daemon-wide.
	table.ToolRegistered = tools.IsToolName
	if opts.Store != nil {
		// The person's answers are filed as the loop's `answers.md` the moment they are given.
		table.OnStepQuestionAnswered = func(stepID int64) {
			fileLoopAnswers(opts.Store, stepID, d.log)
			d.announceResumed(stepID)
		}
	}
	publishEvent := func(kind string, payload any) {
		root := ""
		if rooted, ok := payload.(protocol.ComponentRooted); ok {
			root = rooted.EventComponentRoot()
		}
		// S58 T4: one resolution point — publish consults the class
		// resolver (the old switch branch here was the second truth
		// T4 deletes; SV5's "one resolution per event").
		d.publish(kind, root, payload)
	}
	// Streamed text goes out in joined chunks, not one event per token (delta_coalesce.go).
	table.OnEvent = newDeltaCoalescer(publishEvent).Emit
	if opts.Store != nil {
		// A loop's permission mode (the chat's full-access switch, an unattended loop's pre-approved tools).
		table.LoopAccess = newLoopAccessService(opts.Store, func(kind string, payload any) { table.OnEvent(kind, payload) })
		table.RegisterLoopAccess()
	}
	// S62 T6 (BK7/BK12): the backup family — the five human-door methods
	// over the one service, and the schedule serve's RunBackups runs beside
	// the other maintenance tasks (started after recovery, F15). Built only
	// with a store (V13): a storeless daemon keeps d.backup nil, so
	// RegisterBackup leaves the five ABSENT, not registered-and-refusing,
	// and the ticker is a no-op. The service's events — the engine's
	// finished/failed plus the service's own stale/pruned — reach the wire
	// through table.OnEvent, assigned above, so this wiring lives after it.
	if d.store != nil {
		// T10: the shared settings store joins the service — the ten
		// backup keys are read environment → database → default, live per
		// tick (CS7), so a panel-written change is live on the next wake.
		d.backup = NewBackupService(d.store, opts.Settings, opts.DaemonVersion, func(kind string, payload any) {
			table.OnEvent(kind, payload)
		}, d.log)
		// Assigned INSIDE the guard: a nil *BackupService on the BackupStore
		// field would be a non-nil interface, and RegisterBackup would then
		// register five doors that panic on the first call (the watchdog's
		// lesson, V13 wants the five ABSENT on a storeless mount).
		table.Backup = d.backup
		table.RegisterBackup()
		// S62 T8: the export family, wired the same way. NewExportService
		// answers nil only for a storeless daemon, and we are inside the
		// guard, so the assignment here is always a real service; a nil
		// *ExportService on the ExportStore field would be a non-nil
		// interface and RegisterExport would register two doors that panic
		// on the first call (the same V13 lesson as backup).
		d.export = NewExportService(d.store, opts.DaemonVersion, d.log)
		table.Export = d.export
		table.RegisterExport()
	}
	// S57 T8: the gate plane — the verify family (twelve), the merge
	// plane (eight) and the DoD family (two), one service each over the
	// same store. The same V13 shape as the backup family: assigned
	// INSIDE the guard, so a storeless daemon keeps the three seams nil
	// and the three Register calls leave the twenty-two doors ABSENT,
	// not registered-and-refusing (the human doors the UI and the
	// proposals ride; absent is the honest state).
	if d.store != nil {
		table.Verify = NewVerifyService(d.store, d.log)
		table.RegisterVerify()
		table.MergeRule = NewMergeRuleService(d.store, d.log)
		table.RegisterMergeRule()
		table.DoD = NewDoDService(d.store, d.log)
		table.RegisterDoD()
		// S57 T9 (VG19): the nine gate catalog arms read and write
		// through THE SAME three gate seams just wired — one
		// implementation, two doors (the tool door and the human door
		// cannot drift). Assigned inside the guard, as the seams above:
		// the constructors return non-nil here (d.store is non-nil), so a
		// plain interface assignment carries no typed-nil; a storeless
		// mount keeps the fields nil and the arms refuse (V13).
		if recordsSvc != nil {
			recordsSvc.verify = table.Verify
			recordsSvc.mergeRule = table.MergeRule
			recordsSvc.dod = table.DoD
		}
		// S59 T6: the execution-environment plane — the thirteen environment
		// doors and the seven secret-reference doors, one service each over the
		// same store, and the SAME resolver composition a step goes through
		// (`environment.effective` is the resolution stopped before the spawn,
		// not a description of it). The V13 shape again: assigned INSIDE the
		// guard, so a storeless daemon keeps both seams nil and both Register
		// calls leave the twenty doors ABSENT rather than registered-and-
		// refusing. `Emit` is the `step.environment` echo (EV9): names and the
		// outcome, never a value — the `step_environment` row stays the record.
		envResolver := withVault(newStepExecResolver(d.store), opts.Vault)
		envResolver.Emit = stepEnvironmentEmitter(d.store, d.publish)
		// The settings reader is handed to the service as the S44 rule wants
		// (a consumer takes config.Reader, never the *Store's write half), and
		// ONLY when the daemon has one: an interface holding a nil *Store is
		// not nil, and the service's own absence check would be bypassed.
		var envSettings settingsReader
		if d.settings != nil {
			envSettings = d.settings
		}
		table.Environment = newEnvironmentService(d.store, envResolver, d.log, envSettings)
		table.RegisterEnvironment()
		table.SecretRefs = newSecretRefService(d.store, envResolver, d.log)
		table.RegisterSecretRef()
		// S60 T8 (MQ13): the ten merge-queue doors over the ONE queue door
		// service — the same store, the same mergequeue.Service and the same
		// driver the reactor runs (one implementation, two doors). The six
		// writes stamp the actor from the CONNECTION, never from params, and
		// a storeless mount keeps all ten ABSENT (V13).
		table.MergeQueue = newMergeQueueDoors(d)
		table.RegisterMergeQueue()
		// S63 T5 (RT10): the outcome-learning plane's ten doors over the
		// SAME store the recorder writes — the five scorecard reads, the
		// three retro reads plus the one human-door backfill, and the
		// digest. A storeless mount keeps all ten ABSENT (V13), and the
		// plane computes nothing here: every card is internal/scorecard's
		// and every sentence the digest quotes is rendered by its one
		// formatter.
		table.Scorecard = newScorecardReads(d.store)
		table.RegisterScorecard()
		table.Retro = newRetroReads(d)
		table.RegisterRetro()
		table.Digest = newDigestReads(d.store)
		table.RegisterDigest()
		// S61 T7 (GI8/GI14/GI9): the goal-intake plane's fourteen doors over
		// the SAME store the engine writes and the ONE intake engine the CLI
		// direct road drives — the charter doors answer what is saved (the
		// charter grants nothing through them), the template doors answer the
		// person's palette, and the intake doors start, read, preview and
		// commit. A storeless mount keeps all fourteen ABSENT (V13), and the
		// two writes that exist take the CONNECTION's person (P4).
		table.Charter = newCharterDoors(d)
		table.RegisterCharter()
		table.Templates = newTemplateDoors(d)
		table.RegisterTemplates()
		table.Intake = newIntakeDoors(d)
		table.RegisterIntake()
		// S61 T8 (GI17, rule 4): the seven agent arms read through THESE
		// VERY door structs — one implementation, two doors — and the two
		// propose twins (charter.propose · intake.propose) register on the
		// records seam now that it stands, conditional the autonomy twins'
		// way; registerRecords skips the whole "intake" family (protocol/
		// records.go says why: the five reads belong to the doors just
		// wired above, the two asks to this call).
		recordsSvc.charterPlane = table.Charter
		recordsSvc.templatePlane = table.Templates
		recordsSvc.intakePlane = table.Intake
		table.RegisterIntakePlanePropose()
		// S63 T6 (RT11, rule 4): the three catalog arms read through THESE
		// VERY services — one implementation, two doors.
		recordsSvc.scorecardPlane = table.Scorecard
		recordsSvc.digestPlane = table.Digest
		// S27 T8 (CN23): the scheduler's four read twins ride the SAME
		// ContinuousService the person's four read doors answer through —
		// assigned beside the Table seams, after table.Continuous was built
		// above (the T7 wiring line — a nil here would refuse the arms,
		// never fake them).
		recordsSvc.continuousPlane = table.Continuous
		// S59 T7: the plane's five catalog arms read through THESE VERY
		// services — one implementation, two doors (rule 4; the parity walk
		// pins the pairing, and `secretref_list` is the agent's projection
		// of the same rows the person's door answers with, `source` and all,
		// so the only difference is the type the agent may not see through).
		// Assigned inside the guard, as the gate seams above: a storeless
		// mount keeps both fields nil, the arms refuse by name (V13), and
		// the propose door never stands. The propose twin registers last and
		// is conditional on the records seam NewTable wired above — no
		// records service, no door (V13, the blueprint shape).
		if recordsSvc != nil {
			recordsSvc.environments = table.Environment
			recordsSvc.secretRefs = table.SecretRefs
			// S49 T7 (AN9, rule 4): the four automation read arms forward
			// through protocol.AnswerAutomation* over the SAME seam the
			// person's ten socket doors answer through (wired at
			// table.AutomationReads above, before RegisterAutomationReads)
			// — the tool answer IS the socket answer, statements and
			// evaluatedAt included.
			recordsSvc.automationReads = table.AutomationReads
		}
		table.RegisterEnvironmentProposeRestrict()
		// S49 T7 (AN9): the automation plane's ONE agent door, conditional
		// on the same records seam (V13, the blueprint/environment shape —
		// no records service, no propose door; the four reads need nothing
		// registered here, they ride T3's standing doors).
		table.RegisterAutomationProposeDisable()
		// S50 T7 (RM8): the roadmap plane's ONE agent door, conditional on
		// the same records seam (V13, the automation shape — no records
		// service, no propose door; the reads ride the standing doors).
		table.RegisterRoadmapProposeIdea()
		// S51 T5 (TM6): the bug_file twin — the triage plane's ONE agent
		// filing door — rides the records seam, registered iff it stands
		// (V13), exactly like the roadmap propose twin above.
		table.RegisterTriageFileAs()
		// S52 T4 (LM5/LM7/LM8): the four learnings.mine.* twins — the agent's
		// own memory browse, its own filing, and its one ask — ride the
		// records seam, registered iff it stands (V13, the mcp.mine shape;
		// surface_table_projects.go classifies all four beside the plane).
		// The arms read and write through THIS VERY plane the person's doors
		// answer through (rule 4): one *learnings.Learnings, one readable-scope
		// law, one ledger.
		recordsSvc.learningsPlane = table.Learnings
		table.RegisterLearningsMine()
		// S60 T9 (MQ14.3/MQ17, rule 4): the queue plane's five catalog arms
		// read and write through THIS VERY door service the person's ten
		// sockets answer through (wired above at table.MergeQueue, before
		// RegisterMergeQueue) — the three reads answer identical bytes, the
		// own-item dequeue runs the same Service.Dequeue with the agent as
		// the event actor, and the propose twin — the plane's ONE agent
		// door — rides the records seam, registered iff it stands (V13, the
		// learnings/environment shape; surface_table_mergequeue.go classifies
		// it beside the plane, WITHOUT a door flag — the one mergequeue name
		// an agent may reach).
		recordsSvc.mergeQueue = table.MergeQueue
		table.RegisterMergeQueueProposeEnqueue()
	}
	table.Recovering = d.isRecovering
	table.TrackStep = d.stepCancels.track
	table.CancelStep = d.stepCancels.stop
	table.StepWasStopped = d.stepCancels.wasStopped
	table.StepIsStopped = d.stepCancels.isStopped
	table.StepIsRunning = d.stepCancels.isRunning
	// T4 (LC19): the commit path's cause seam. The daemon is the one package
	// that imports both loop and protocol, so it carries the fact that a
	// cancelled run was the SHUTDOWN's: the step commit path (protocol/step.go)
	// reads the ctx cause through this function and never imports loop itself
	// (which would cycle). A step interrupted by the drain is left running at
	// its last checkpoint for recovery — never committed failed.
	table.ShutdownCause = func(ctx context.Context) bool {
		return errors.Is(context.Cause(ctx), loop.ErrDaemonShutdown)
	}
	// `step.stopped`/`step.continued` name the loop when the step belongs to
	// one (S31 D6, brief §5 `loopId?`). A slug is unique only WITH its root
	// (the store's rule), so both identify the loop; no match returns "" and
	// the event omits loopId honestly rather than guessing.
	// S70 MB6: the ask door announces its rows through the daemon's own
	// publish — mounted here, where d exists, beside the other d-bound wires.
	table.MailboxAsk = mailboxAskFor(opts.Store, d.publish)
	table.LookupLoopID = func(loopSlug, componentRoot string) string {
		if d.store == nil {
			return ""
		}
		l, err := d.store.GetLoop(store.Scope{LoopSlug: loopSlug, ComponentRoot: componentRoot})
		if err != nil {
			return ""
		}
		return l.ID
	}
	// loopCtx is the daemon-lifetime context every loop runner is started on
	// (T4, LC19). Created before initBlueprints, because the Start sites it
	// wires (and every other Start site below) must derive from it — so a
	// graceful shutdown's Drain can cancel them with ErrDaemonShutdown and the
	// step commit path can read the cause. It lives until the drain cancels it.
	d.loopCtx, d.cancelLoops = context.WithCancelCause(context.Background())
	d.initBlueprints()
	if d.recordsSvc != nil {
		// S48 T10 (BP9): the four blueprint read arms forward to the SAME
		// seams the person's socket doors answer through — assigned HERE,
		// after initBlueprints (the only place those seams are set), and
		// nowhere else: one implementation, two doors, and
		// TestToolReadsEqualSocketReads pins the answers byte-for-byte.
		// The storeless mount has no records service and nothing to wire.
		d.recordsSvc.blueprints = table.BlueprintReads
		d.recordsSvc.listBlueprints = table.ListBlueprints
		d.recordsSvc.showBlueprint = table.ShowBlueprint
		d.recordsSvc.validateBlueprint = table.ValidateBlueprint
	}
	d.wireWebhookDeliver()
	d.wireRemoteComplete() // S21 T6: the completion arrives at THIS surface, verified and bound (RA7/RA8)
	d.wireTriggerControl() // S20 T8: one control plane, reached from CLI and app alike
	if opts.Store != nil {
		table.ListLoopFiles = d.listLoopFiles
		table.ReadLoopFile = d.readLoopFile
	}
	sessions.OnClose(d.onConnectionClosed)

	// S53 T3: the merge-queue reactor, started LAST among the store-backed
	// goroutines — after initBlueprints (d.bpSrc), after the records seams,
	// after the wiring its landing path reaches through. Its first pass
	// reconciles whatever the store already holds, and every door that
	// pass can walk is now wired. (The comment at its old position says why
	// it moved.)
	d.initMergeQueueReactor(reaperCtx)

	return d
}

// ModelClient builds the model client the daemon streams through.
//
// Here rather than in `main` so the one place that knows how a model call is
// made is next to the one place that knows how a request is sent.
func ModelClient() protocol.Streamer { return &model.Client{} }

type discardLogger struct{}

func (discardLogger) Infof(string, ...any) {}

// Workers returns the S64 async-worker manager, or nil on a storeless
// mount. The only caller today is serve: it runs Recover once the loop
// recovery has had its turn, and Shutdown drains the live workers before
// the store closes. A test that wants the ledger's truth reads it through
// the same value the socket answers from.
func (d *Daemon) Workers() *asyncworkers.Manager { return d.workers }

// Skills returns the S65 Agent Skills catalog, or nil on a no-catalog
// mount. The socket answers from this same value; serve takes its startup
// figure from it. A test that wants the catalog's truth reads it through
// the same service the doors answer from.
func (d *Daemon) Skills() *skills.Service { return d.skills }

// Register binds every protocol method to the transport.
//
// **Must be called after the service has started** — `localrpc.Register`
// answers "service is not running" otherwise, which S1 found the hard way.
func (d *Daemon) Register() error {
	for _, name := range d.Table.Methods() {
		method := name // one binding per iteration
		if err := localrpc.Register(string(method), d.handler(method)); err != nil {
			return fmt.Errorf("register %q: %w", method, err)
		}
	}
	return nil
}

// handler adapts one protocol method to the transport's Handler signature.
func (d *Daemon) handler(name protocol.MethodName) localrpc.Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		connID, ok := localrpc.ConnIDFromContext(ctx)
		if !ok {
			// Nothing can be done per-connection without it: every session,
			// subscription and notification is keyed by it.
			return nil, localrpc.NewError(-32603, "connection id is missing from the request context")
		}
		result, err := d.Table.Dispatch(ctx, connID, name, params)
		if err != nil {
			return nil, d.toWireError(err)
		}
		return result, nil
	}
}

// toWireError maps a protocol error onto the transport's error object.
//
// The three kinds, and why each maps where it does:
//
//   - *protocol.Error → its Aeon code, with the three-part `data` attached
//     (T10). The transport's Data is raw JSON, so the shape survives intact.
//   - ErrUnknownMethod → JSON-RPC's -32601. A fact about JSON-RPC, not about
//     Aeon, so it does not take an Aeon code.
//   - ErrBadParams → JSON-RPC's -32602, for the same reason.
//
// Anything else becomes -32603 with its message, which is the transport's
// default and the right one: an unexpected error is an internal error.
func (d *Daemon) toWireError(err error) error {
	var aeon *protocol.Error
	if errors.As(err, &aeon) {
		wire := localrpc.NewError(aeon.Code, aeon.Message)
		if data, mErr := aeon.MarshalData(); mErr == nil {
			wire.Data = data
		}
		return wire
	}

	var unknown protocol.ErrUnknownMethod
	if errors.As(err, &unknown) {
		return localrpc.NewError(-32601, err.Error())
	}

	var bad protocol.ErrBadParams
	if errors.As(err, &bad) {
		return localrpc.NewError(-32602, err.Error())
	}
	return err
}

// backing and memory return TYPED NILS as nil interfaces.
//
// `NewEventBacking(nil)` returns `(*EventBacking)(nil)`, and assigning that to
// an interface produces a non-nil interface holding a nil pointer — which would
// pass every `!= nil` check and panic on first use. These two functions are
// where that is handled, once, rather than at each assignment.
func backing(s *store.Store) protocol.Backing {
	if b := NewEventBacking(s); b != nil {
		return b
	}
	return nil
}

func memory(s *store.Store) protocol.Memory {
	if m := NewMemory(s); m != nil {
		return m
	}
	return nil
}

func stepStore(s *store.Store) protocol.StepStore {
	if ss := NewStepStore(s); ss != nil {
		return ss
	}
	return nil
}

// proposalRow mirrors a store.Proposal onto protocol.ProposalRow — the
// payload rides as json.RawMessage on the wire (the stored column is a
// string of JSON text), and Command is left for the handler to fill
// (proposals.go's commandFor): this adapter knows the store's shape, not
// the CLI command grammar.
func proposalRow(p store.Proposal) protocol.ProposalRow {
	return protocol.ProposalRow{
		ID:            p.ID,
		Kind:          p.Kind,
		Payload:       json.RawMessage(p.Payload),
		Status:        p.Status,
		CreatedAt:     p.CreatedAt,
		CreatedByKind: p.CreatedByKind,
		CreatedByID:   p.CreatedByID,
		DecidedAt:     p.DecidedAt,
		DecidedByKind: p.DecidedByKind,
		DecidedByID:   p.DecidedByID,
		DecideReason:  p.DecideReason,
		AppliedError:  p.AppliedError,
	}
}

// proposalStoreAdapter is the protocol.ProposalStore the socket calls — the
// same StepStore/QuestionStore local-interface pattern (internal/store is
// not banned from internal/protocol). Unlike that first draft, this needs
// only *store.Store: the actor is resolved ONCE already, by the existing
// table.ActorFor seam (set in New(), same uid-from-peer stamp
// socketRecords.actor and table.ActorFor itself already use) — the handler
// passes the resolved Kind/ID straight through, so this adapter does not
// duplicate peer resolution a second time.
type proposalStoreAdapter struct {
	s *store.Store
}

func proposalStore(s *store.Store) protocol.ProposalStore {
	if s == nil {
		return nil
	}
	return &proposalStoreAdapter{s: s}
}

func (a *proposalStoreAdapter) CreateProposal(_ context.Context, kind string, payload json.RawMessage, byKind, byID string) (protocol.ProposalRow, error) {
	p, err := a.s.CreateProposal(kind, string(payload), store.Actor{Kind: byKind, ID: byID}, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return protocol.ProposalRow{}, err
	}
	return proposalRow(p), nil
}

func (a *proposalStoreAdapter) ListProposals(status string) ([]protocol.ProposalRow, error) {
	rows, err := a.s.ListProposals(status)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.ProposalRow, len(rows))
	for i, p := range rows {
		out[i] = proposalRow(p)
	}
	return out, nil
}

func (a *proposalStoreAdapter) DiscardProposal(_ context.Context, id int64, byKind, byID string) error {
	return a.s.DiscardProposal(id, store.Actor{Kind: byKind, ID: byID}, time.Now().UTC().Format(time.RFC3339Nano))
}

// chainHooks composes write-signal subscribers. The hook contract (V18,
// Q7): every subscriber is fire-and-forget and the step never waits — so
// each runs in its own goroutine and neither a slow subscriber nor a
// panicking one can delay or poison the step. Order is not a contract
// here: retrieval reindex and graph rebuild observe the same file writes
// independently; neither depends on the other having run.
func chainHooks(fns ...func(root, path string)) func(root, path string) {
	live := make([]func(root, path string), 0, len(fns))
	for _, f := range fns {
		if f != nil {
			live = append(live, f)
		}
	}
	return func(root, path string) {
		for _, f := range live {
			go func(f func(root, path string)) {
				defer func() { _ = recover() }()
				f(root, path)
			}(f)
		}
	}
}

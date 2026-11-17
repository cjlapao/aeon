// Command aeon is the whole product: one binary that is both the daemon and
// every client verb (B4).
//
//	aeon serve             the daemon
//	aeon ping              is it there, and how fast
//	aeon ask               ask a model, streaming the reply
//	aeon db                where the store is, what ran, what it holds
//	aeon loop              loops and their documents
//	aeon step "PROMPT"     run one bounded step, streaming it
//	aeon step show…        steps and their transcripts
//	aeon version           this build, and the negotiated protocol version
//	aeon watch             stream events
//	aeon debug subs…       what the daemon is holding
//	aeon --print-services  what this daemon would run
//
// One binary rather than two is what makes the editor-extension path real
// (P30): an extension ships one executable and speaks to it, instead of
// shipping a server and hoping a separate CLI is on the PATH and matches.
//
// # What this does NOT do yet
//
// No runtime, so nothing RUNS a step: `ask --save-step` persists what `ask`
// did, and the thing that assembles context and loops over tools is S5. No
// suspension and no questions asked — the `question` table exists and is
// empty, and S6 fills it. No crash recovery: `status='running'` steps are
// readable, and the thing that reads them on startup is S7.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cjlapao/aeon/internal/bootstrap"
	"github.com/cjlapao/aeon/internal/database"
)

func main() {
	verb := "help"
	args := []string{}
	if len(os.Args) > 1 {
		verb = os.Args[1]
		if len(os.Args) > 2 {
			args = os.Args[2:]
		}
	}

	// Extra arguments are REFUSED, not ignored — for the verbs that take none.
	//
	// Found by walking S1's demo: `aeon serve --print-services` silently
	// started the daemon, because the dispatch read os.Args[1] and dropped the
	// rest. A flag that is accepted and ignored is worse than one that is
	// rejected, because the person believes they asked for something.
	//
	// S2 adds verbs that DO take flags, so the check became a per-verb property
	// rather than a blanket one. It is a deny-list of verbs taking nothing
	// rather than an allow-list of flags, so a verb added later is refused
	// arguments by default instead of silently accepting them.
	if takesNoArguments(verb) && len(args) > 0 {
		fmt.Fprintf(os.Stderr, "aeon: %q takes no arguments, but %d were given: %v\n\n",
			verb, len(args), args)
		printUsage(os.Stderr)
		os.Exit(2)
	}

	var err error
	switch verb {
	case "serve":
		err = cmdServe(args)
	case "version", "--version", "-v":
		err = cmdClientVersion(os.Stdout, args)
	case "ping":
		err = cmdPing(os.Stdout, args)
	case "ask":
		err = cmdAsk(os.Stdout, args)
	case "watch":
		err = cmdWatch(os.Stdout, args)
	case "debug":
		err = cmdDebug(os.Stdout, args)
	case "db":
		err = cmdDB(os.Stdout, args)
	case "backup":
		// S62 T7: the backup family — restore is CLI-only by BK9 (the
		// swap overwrites the file the daemon owns, so a daemon cannot
		// do it); T9 extends the family with the verbs whose home is
		// the socket (P34).
		err = cmdBackup(os.Stdout, args)
	case "export":
		// S62 T8: the export family — clients of export.run / export.sets
		// (P34), with the direct door when the daemon is down (the store
		// opened read-only, never migrated, no ledger row).
		err = cmdExport(os.Stdout, args)
	case "loop":
		err = cmdLoop(os.Stdout, args)
	case "goal":
		// S38 T16 — the org doors at the terminal: clients of the socket's
		// catalog methods and the three decision methods, never the store
		// (P34; unlike `policy`, which is store-direct by PL15).
		err = cmdGoal(os.Stdout, args)
	case "vault":
		// The variable manager at the terminal — a client of vault.* (P34). The
		// value is read from standard input, never argv.
		err = cmdVault(os.Stdout, args)
	case "brief":
		err = cmdBrief(os.Stdout, args)
	case "task":
		err = cmdTask(os.Stdout, args)
	case "blueprint":
		err = cmdBlueprint(os.Stdout, args)
	case "step":
		err = cmdStep(os.Stdout, args)
	case "budget":
		// S55 T11: the budget plane's human doors — clients of the socket's
		// budget.* and spend.report (P34), never the store; the usage's
		// closed sets are the daemon's vocabulary, not a client copy.
		err = cmdBudget(os.Stdout, args)
	case "watchdog":
		// S55 T11: the watchdog plane's human doors — clients of the
		// socket's watchdog.* (P34), never the store.
		err = cmdWatchdog(os.Stdout, args)
	case "answer":
		err = cmdAnswer(os.Stdout, args)
	case "routes":
		err = cmdRoutes(os.Stdout, args)
	case "trigger":
		err = cmdTrigger(os.Stdout, args)
	case "automation":
		// S49 T8 (AN10/AN12, P34): one verb set over both families, event
		// triggers' first CLI — clients of the automation.*/trigger.*
		// socket methods, with S45 PM4's offline door (the same daemon
		// services over a directly opened store, the fixed notice line,
		// and NOTHING else on the dial road).
		err = cmdAutomation(os.Stdout, args)
	case "env":
		// S59 T8 (EV18, P34): the execution-environment plane at the
		// terminal — a PURE client of the daemon's environment.* doors.
		// No direct door: the resolver, the seed engine and the reconcile
		// state that answer these methods live in the daemon, and `aeon`
		// has no first-run path that needs an environment offline.
		err = cmdEnv(os.Stdout, args)
	case "secretref":
		// S59 T8 (EV7, §4c): the secret references, same plane, same door.
		// A reference names where a value LIVES; no flag, stdin path or
		// prompt here takes the value itself.
		err = cmdSecretRef(os.Stdout, args)
	case "policy":
		err = cmdPolicy(os.Stdout, args)
	case "queue":
		// S24 T6: the backlog's client — dialer of the queue.* methods,
		// opener of nothing (CG9's one implementation underneath).
		err = cmdQueue(os.Stdout, args)
	case "mergequeue":
		// S60 T10: the merge queue's client — dialer of the ten
		// mergequeue.* methods, opener of nothing. Unlike every two-door
		// family before it there is NO offline arm: the queue's moves run
		// in the daemon's loops (the recorded P5 deviation, EV18's
		// pure-socket shape).
		err = cmdMergeQueue(os.Stdout, args)
	case "roadmap":
		// S26 T5: the intentions-before-the-queue's client — dialer of the
		// roadmap.* methods, opener of nothing (CG9's rule, extended: the
		// CLI is a secondary client, P34; the state machine's doors all
		// ride the socket first).
		err = cmdRoadmap(os.Stdout, args)
	case "scheduler":
		// S27 T9: the continuous scheduler's client — dialer of the twelve
		// scheduler.* methods, opener of nothing. Like the mergequeue before
		// it there is NO offline arm: the scheduler lives in the daemon's
		// loops alone (the recorded P5 deviation); --yes seatbelts the
		// human-door writes (CN18).
		err = cmdScheduler(os.Stdout, args)
	case "charter":
		// S61 T10: the charter plane's client — dialer of the five
		// charter.* doors, opener of nothing. No offline arm: a charter
		// filed without the daemon is not a thing (the recorded P5
		// deviation).
		err = cmdCharter(os.Stdout, args)
	case "template":
		// S61 T10: the scaffold catalogue's client — three doors, pure
		// socket.
		err = cmdTemplate(os.Stdout, args)
	case "intake":
		// S61 T10: the interview's client — seven doors, pure socket: no
		// loop, no conversation and no file lands without the daemon, and
		// a commit must not run in a second process.
		err = cmdIntake(os.Stdout, args)
	case "triage":
		// S29 T7: the bugs-before-the-roadmap's client — dialer of the
		// triage.* methods, opener of nothing (CG9's rule extended: the
		// CLI is a secondary client, P34; the four-state machine's doors
		// all ride the socket first).
		err = cmdTriage(os.Stdout, args)
	case "learnings":
		// S28 T4: the learnings' client — dialer of the learnings.*
		// methods, opener of nothing (P34's ordering: the capability rides
		// the socket first; the CLI is a secondary client, not a second
		// door).
		err = cmdLearnings(os.Stdout, args)
	case "search":
		// S22 T9: the human's client of the same search.query door the
		// model's tool shares — locations, never text (V4).
		err = cmdSearch(os.Stdout, args)
	case "index":
		// S22 T8: the deliberate index verbs — thin clients of the socket
		// doors (retrieval.status / retrieval.reindex), never a second
		// reimplementation and never an automatic pass (V21/P34). S54 T5
		// gates rebuild: the daemon's cost plan prints first and --yes is
		// what acts (SP7).
		err = cmdIndex(os.Stdout, args)
	case "platform":
		// S54 T5: the daemon's own panel for the derived stores — status,
		// the reindex plan, the change ledger. Pure clients of the three
		// SP6 socket reads; there is NO offline arm (a composed panel or a
		// live-priced plan rebuilt client-side would be a second
		// implementation — the mergequeue family's recorded shape).
		err = cmdPlatform(os.Stdout, args)
	case "history":
		// S25 T3, restored by S53 T5: `b5620631` ("wip") deleted this case
		// along with the boot wiring T3 restored — the same silent outage,
		// second surface. S25's three verbs keep their exact direct road;
		// S53 T4's coverage/vocabulary are socket-first clients with the P5
		// offline door (history_client.go), never a second answer.
		err = cmdHistory(os.Stdout, args)
	case "scorecard", "retro":
		// S63 T7 (RT10): the outcome plane's CLI — pure clients of the
		// person's socket doors (scorecard.* and retro.*, plus
		// digest.learned). No offline arm and no store door: a scorecard
		// reads recorded facts the daemon owns, so a stopped daemon says
		// so with dial's own words (the mergequeue/platform shape, T7's
		// recorded deviation from P5). `retro backfill` is the one write,
		// dry-run unless --yes (RT13 — a seatbelt, not a control).
		if verb == "scorecard" {
			err = cmdScorecard(os.Stdout, args)
		} else {
			err = cmdRetro(os.Stdout, args)
		}
	case "telemetry":
		// S33 T8: the counting tiers, asked of the daemon (telemetry.go) —
		// clients of telemetry.* on the socket, the same four reads the app
		// calls, never a second implementation.
		err = cmdTelemetry(os.Stdout, args)
	case "component":
		// S44-extra T8: the components tree — clients of component.* on the
		// socket (the nine tree operations), never the store (CT13).
		err = cmdComponent(os.Stdout, args)
	case "project":
		// S44-extra T8: a project is a root component; the project verb is
		// gone, restated as one fixed redirect (CT13) and the usage exit (2).
		fmt.Fprintln(os.Stderr, componentRedirectLine())
		os.Exit(2)
	case "settings":
		// S44 T7: the declared settings, asked of the daemon (settings.go) —
		// clients of settings.* on the socket, the same five methods the app
		// calls (P34), never a store-opening second implementation.
		err = cmdSettings(os.Stdout, args)
	case "skills":
		// S65 T6: the Agent Skills catalog, asked of the daemon
		// (skills.go) — clients of skills.* on the socket, the same five
		// methods the bench page calls (P34), never a disk-walking or
		// git-running second implementation. Install fetches through the
		// DAEMON, which owns the one catalog.
		err = cmdSkills(os.Stdout, args)
	case "graph":
		// S30 T5: the code-graph verbs — clients of graph.* on the socket,
		// the same methods the app calls (P34), never a store-opening
		// second implementation.
		err = cmdGraph(os.Stdout, args)
	case "handlers":
		// S13's mechanical guard and registry listing. Returns its own exit
		// code — the verify contract is an exit code (0/1/2), so it does not
		// go through renderError.
		os.Exit(cmdHandlers(os.Stdout, os.Stderr, args))
	case "agent":
		// S15's five verbs. `import` is the only one of the four state
		// transitions with no socket door (R6: installing a plugin stays
		// CLI/app-only) — enable/disable/revoke answer their own socket
		// methods too, as of S35 T4 (brief §3: moving an already-consented
		// agent starts nothing new). The socket's own read is wired in
		// agent_services.go.
		err = cmdAgent(os.Stdout, args)
	case "agents":
		// S43 T8 — P34's secondary client of the agent-management methods.
		// A new command because `aeon agent import` is S15's plugin import
		// (a direct store write, no socket door); these verbs dial the
		// daemon and call the methods by name, never a second
		// implementation. A capability here is never the sole door (P34).
		err = cmdAgents(os.Stdout, args)
	case "proposal":
		// S35 T5 — the human-only door: the only place a proposal's effect
		// is ever applied (brief §7). `proposal.create/list/discard` answer
		// their own socket methods (T3); commit/reject answer nowhere but
		// here, by name, forever.
		err = cmdProposal(os.Stdout, args)
	case "action":
		// S16's governance read (T2): class and verdict for a code action,
		// so the bash route and the code route can be compared side by
		// side. The registry's `list` arrives with T10.
		err = cmdAction(os.Stdout, args)
	case "mcp":
		// The S12 seam, and the only import of internal/mcp outside the
		// package itself (T9/MC11): delete this case and cmd/aeon/mcp.go and
		// no other file in the product knows MCP exists.
		err = cmdMCP(os.Stdout, args)
	case "narrow":
		// S19 — read the narrow surface's table: the diff of
		// `narrow list --mounting=stdio` against `--mounting=http` IS N18's
		// set equality, executable by a human. Pure client: no daemon needed.
		err = cmdNarrow(os.Stdout, args)
	case "seed":
		// S43-extra T7 — a client of the daemon's four seed.* methods (P34);
		// offline it opens the same engine directly and says so.
		err = cmdSeed(os.Stdout, args)
	case "tools":
		// S47 T8 — a CLIENT of the daemon's four tools.* reads (P34); offline
		// the same catalog opens directly over the store and says so. Every
		// verb is a read: S47 ships no write tool, so there is no verb here
		// that changes anything (TC1).
		err = cmdTools(os.Stdout, args)
	case "surface":
		// S47 T6/T8 — the daemon's own classification, as words. SOCKET ONLY:
		// the listing's subject is which methods a fully wired build registers,
		// and that wiring is the entry point's, so offline it refuses rather
		// than print a partial table (tools_door.go's header is the argument).
		err = cmdSurface(os.Stdout, args)
	case "autonomy":
		// S56 T9 — the person's terminal door onto the autonomy authority:
		// a client of the daemon's ten autonomy.* methods (P34); offline the
		// reads and the restrictive verbs (deny, revoke) open the same
		// service directly and say so, and grant never runs offline (AL13).
		err = cmdAutonomy(os.Stdout, args)
	case "verify":
		// S57 T10 — the gate plane's person's door at the terminal: a
		// client of the daemon's verify.* methods (P34); offline every
		// record verb opens the same services directly and says so, and
		// try never runs offline (it needs the daemon's policy gate).
		err = cmdVerify(os.Stdout, args)
	case "merge-rule":
		// S57 T10 — the merge rule's human door; the seatbelt on a
		// loosening set is decided by the daemon's own classifier and
		// printed with its own field names (mergerule.Tightens), sending
		// nothing without --yes.
		err = cmdMergeRule(os.Stdout, args)
	case "dod":
		// S57 T10 — the loop's Definition of Done: the human's own
		// evidence-bound mark, the daemon's refusals verbatim.
		err = cmdDoD(os.Stdout, args)
	case "merge":
		// S57 T10 — the merge record's two reads. The merge itself is
		// the daemon's and the forge's; this verb never merges anything.
		err = cmdMerge(os.Stdout, args)
	case "inbox":
		// S58 T6 — the attention plane at the terminal: a pure client of
		// the daemon's five inbox.* doors (SV13). No offline door exists
		// and none is added: the inbox is live evaluation (T2's rule), so
		// a second evaluator in the CLI would be a second truth.
		err = cmdInbox(os.Stdout, args)
	case "status":
		// S58 T6 — the mission overview (SV11): a client of
		// mission.status. Unwired groups print `not wired`, never a zero;
		// no composite score is printed because none is computed.
		err = cmdStatus(os.Stdout, args)
	case "notify":
		// S58 T6 — the notification plane's person's door: a client of
		// the class, channel, subscription and delivery doors. A secret
		// enters only as --secret-file (a path the DAEMON reads); create,
		// archive and test carry the --yes seatbelt (C17).
		err = cmdNotify(os.Stdout, args)
	case "doctor":
		// S19 O8 — the running process answers what it exposes; a default
		// only helps the first time.
		err = cmdDoctor(os.Stdout, args)
	case "--print-services":
		printServices(os.Stdout)
	case "--vectors":
		err = printVectors(os.Stdout)
	case "help", "-h", "--help":
		printUsage(os.Stdout)
	default:
		// Naming the verb matters: a typo and an unimplemented verb are
		// different problems, and "unknown command" alone makes them look the
		// same. Usage goes to stderr because the exit code is non-zero.
		fmt.Fprintf(os.Stderr, "aeon: unknown command %q\n\n", verb)
		printUsage(os.Stderr)
		os.Exit(2)
	}

	if err != nil {
		// Rendered in three parts where the error has them (T10): what
		// happened, what it means, what to do. A user seeing `-31002` has to go
		// and look it up.
		renderError(os.Stderr, err)
		// A usage refusal (S38 T16: a decide without --version) is the
		// usage exit, not the failure exit — the same 2 that the argument
		// checks above use, so scripts can tell "you did not ask" from
		// "the asking failed".
		var ue *usageError
		if errors.As(err, &ue) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// takesNoArguments lists the verbs that accept nothing at all.
//
// A deny-list rather than an allow-list on purpose: a verb added later gets the
// strict behaviour by default, and has to opt out by taking flags.
func takesNoArguments(verb string) bool {
	switch verb {
	case "--print-services", "--vectors", "doctor", "help", "-h", "--help":
		return true
	}
	return false
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `aeon — a loop-native agent harness

Usage:
  aeon <command>

Commands:
  serve             Run the daemon: bind the local endpoint and wait
                    --no-recover reports cases and leaves crashed steps running
                    --http ADDR --http-key-file PATH bind the narrow HTTP
                    surface (S19) — an address is typed, a key is out of
                    band, and an address without a key refuses to start
  ping              Round-trip the daemon, and report how long it took
  ask               Ask a model, streaming the reply
  version           Print this build, and the daemon's negotiated protocol
  watch             Subscribe to events and stream them
  db                info — where the store is and what ran
                    upgrade --dry-run — what an upgrade would apply
                    query awaiting — the derived awaiting query, and its answer
  backup            run [--no-verify] | list | verify FILE [--drill] |
                    status | prune [--dry-run | --yes] | restore FILE
                    [--plan] [--yes] — the read-only tool over the
                    database (S62 T9); every verb but restore takes
                    --json; the daemon's door first, the direct door
                    (never migrating) when it is down; restore is
                    CLI-only — by BK9
  export            --dir DIR [--records a,b,c] [--include-memory]
                    [--json] — the managed records as a read-only,
                    deterministic, secret-free text snapshot (S62 T8);
                    sets [--json] lists the export sets; the daemon's
                    door first, the direct door (no ledger row) when it
                    is down
  routes            list, set, delete — providers, models and prices
  policy            list, get, vocabulary, changes, add, remove, why,
                    in-force, events — the policy
  queue             append, list, reorder, remove, set-size, settings — the
                    backlog before any of it runs (S24)
  mergequeue        list, show, status, vocabulary, enqueue, dequeue,
                    reorder, retry, land, settings — the landing queue in
                    front of main (S60): ten socket doors, no offline arm —
                    the queue's moves run in the daemon's loops
  roadmap           add, list, show, cancel, estimate, priority, ideate,
                    plan, materialize — the intentions before they are even
                    queued (S26): R1's eight statuses, one socket door each
                    engine (S45: every verb dials the daemon's methods; with
                    no daemon home it runs the same desk code on the store,
                    saying so — PL15's human door is the socket's)
  scheduler         status, preview, decisions, attention, enable, disable,
                    pause, resume, stop-all, repo set|disable, enable-tree —
                    the continuous scheduler's doors (S27): twelve verbs,
                    twelve socket doors, no offline arm — a scheduler
                    without a daemon is not a thing; --yes seatbelts the
                    writes and enable prints the daemon's own statement
  charter           list, show, history, save, vocabulary — a root's
                    charter as the daemon stores it (S61): five socket
                    doors, no offline arm — a filed charter rides the
                    daemon's record, export and notify; save reads --file
  template          list, show, save — the scaffold catalogue (S61): three
                    socket doors, pure client
  intake            start, list, status, plan, commit [--digest HEX],
                    abandon, vocabulary — the interview, the plan, the
                    landing (S61): seven socket doors, no offline arm —
                    no loop, no conversation and no file lands without the
                    daemon, and a commit must not run in a second process
  proposal          list, show, commit [--digest HEX], reject — the human
                    door that applies a staged proposal's effect: every
                    verb is a client of the socket's decision methods, with
                    the same offline fallback
  search            --root DIR --corpus code|history "query" — locations,
                    never text (S22): a client of the same search.query the
                    model's tool answers through
  index             status, build, rebuild — the retrieval index (S22): the
                    deliberate verbs, clients of retrieval.status and
                    retrieval.reindex — nothing here ever fires on its own;
                    rebuild prints the daemon's cost plan first and acts
                    only on --yes (S54 SP7)
  platform          status | plan CORPUS --component DIR | changes
                    [--subject S] [--limit N] — the daemon's own panel for
                    the derived stores (S54): states are the daemon's words
                    (available · stale · building · unwired · error), the
                    plan is its live price preview, the changes ledger
                    names who started what; pure socket clients, no offline
                    arm — a composed panel rebuilt client-side would be a
                    second implementation
  history           list, show, export, coverage, vocabulary — the
                    time-ordered projection of a project's work (S25, S53):
                    a pure read, a floored cost, the blocked reason
                    verbatim, coverage's three states, the closed vocabulary
  scorecard         agent, blueprint, component, compare, vocabulary (S63):
                    what a completed loop achieved — every metric printed
                    with its n and its state, costs as floors,
                    "too-few"/"cannot-say" as words; a measurement, never a
                    ranking (no sort-by-score flag exists)
  retro             list, show, status, backfill, digest (S63): the recorded
                    outcomes, the recorder's counts, the one-paragraph
                    readout; backfill previews and writes nothing without
                    --yes
  telemetry         loop, component, project, workspace — the counting tiers
                    (S33): pure clients of the socket's four reads, never a
                    store-opening second door (P34)
  settings          list, get, set, unset, history — the declared settings
                    (S44): pure clients of the socket's five settings methods,
                    never a store-opening second door (P34)
  skills            list, get, validate, install, remove — the Agent Skills
                    catalog (S65): pure clients of the socket's five skills
                    methods; install fetches GitHub or a folder THROUGH the
                    daemon, which owns the one catalog (P34)
  mcp               list, call — configured MCP servers (S12): the CLI seam,
                    absent configuration is a legitimate answer
                    serve — mount the narrow surface (S19) as an MCP server
                    on stdio, talking to the daemon over the socket
                    S46: the management set — show, grants, add, update,
                    remove, enable, disable, declare, revoke, core,
                    import-config, changes, status, check — CLIENTS of the
                    daemon's human-door methods; with no daemon they apply
                    directly to the store and say so on stderr
  narrow            list --mounting=stdio|http — the surface's table (S19):
                    one action per line; the diff of the two mountings is
                    empty by design (N18)
  env             list, show, effective, vocabulary, changes, provisions, create,
                    update, archive, restore, bind, unbind, teardown — the
                    execution environments (S59 T8): a PURE client of the
                    daemon's thirteen environment.* doors (P34), no direct door.
                    bind, unbind, teardown and create --backend host-inherit
                    need --yes; --help on create/update prints the closed sets AS
                    THIS DAEMON answers them. No verb takes a secret value
  secretref       list, show, check, create, update, archive, restore — the
                    secret references (S59 T8): a client of the seven secretref.*
                    doors. create names where a value LIVES (--from-env VAR |
                    --from-file PATH); there is no flag, stdin path or prompt
                    that takes the value itself (EV7, §4c)
  seed            status, plan, apply, restore — the seed sets, a client of
                    the daemon's four seed.* methods (S43-extra); offline it
                    opens the same engine directly and says so (P34)
  tools           list, show, agent, status — what this build exposes as a tool
                    (S47 T8): a client of the daemon's four tools.* reads; offline
                    the same catalog opens directly over the store and says so
                    (P34). Every verb is a read (TC1)
  surface         list, show — which methods this daemon registers and through
                    which doors (S47 T6/T8): a client of surface.list/get; it
                    REQUIRES the daemon, because the listing's subject is the
                    entry point's wiring and a partial table would lie
  autonomy        list, show, vocabulary, effective, pinned, changes, uses,
                    grant, deny, revoke — the autonomy authority (S56): a
                    client of the daemon's ten autonomy.* methods (P34);
                    offline the reads and the restrictive verbs open the same
                    service directly and say so, and grant never runs offline
  verify            list, show, add, update, archive, restore, apply-template,
                    try, runs, run-show, vocabulary, templates — the gate
                    plane (S57): a client of the daemon's verify.* methods
                    (P34); offline the record verbs open the same services
                    directly and say so, and try never runs offline (the
                    policy gate is the daemon's). "aeon verify help" prints
                    the flags; closed sets are the daemon's vocabulary
  merge-rule        get, set, clear, preview, changes, vocabulary — the
                    component's merge gate (S57); a set that LOOSENS the
                    effective rule needs --yes (the daemon's own classifier
                    decides, the person's flag authorises)
  dod               list, mark — the loop's Definition of Done: the human's
                    evidence-bound mark (S57)
  merge             events, watches — the merge record's two reads; the
                    merge itself is the daemon's and the forge's (S57)
  inbox             list, summary, ack, snooze, unack — what needs a person
                    (S58): live evaluation over the socket, the source
                    census always printed, nothing stored or cached (SV1).
                    No offline door: the inbox is live state, not a file.
  status            The mission overview (S58): per root — loop counts,
                    needs-you, the spend FLOOR against its cap, next ready;
                    unwired groups print 'not wired', no composite score
  notify            classes, channel, subscriptions, deliveries, vocabulary
                    (S58): who gets told and where it goes. A secret enters
                    only as --secret-file (a path the daemon reads, C17);
                    create, archive and test carry the --yes seatbelt
  doctor            What the RUNNING process exposes: the socket, and the
                    narrow surface's address and key state — never the key
                    (O8; a default only helps the first time)
  loop              create, list, write, read — loops and their documents
  goal              list, show, create, submit, decide — the org's intentions;
                    a goal arrives in_review and only a person's decide moves
                    it (the terminal is a client of the socket, never the
                    store)
  brief             show, decide — a loop's contract: read it, judge it;
                    authoring stays the brief-author's lane
  task              list, show, create — the board: hand-made and engineered
                    rows side by side, cost with its floor flag (P32)
  blueprint         list, path, fields, validate — inspect and check YAML
  step              "PROMPT" — run one bounded step, streaming it
                    show, verify, list — steps and their transcripts
                    stop, continue — interrupt a running step, and resume
                    one a person stopped (S55)
  budget            list, show, effective, set, remove, spend — the
                    budgets and the spend against them (S55): clients of
                    the socket's budget plane (P34), closed sets printed
                    from the daemon's vocabulary
  watchdog          list, show, vocabulary, set, enable, disable, remove
                    — the rules that fire on a signal and move a loop
                    (S55): clients of the socket's watchdog plane (P34)
  answer            STEP_ID ANSWER — resume a suspended step
  automation        list, show, fires, runs, health, changes, vocabulary,
                    preview, test, dryrun — add-schedule, add-webhook,
                    add-event, update, enable, disable, pause, resume,
                    fire-now, archive, unarchive — one verb set over both
                    automation families (S49): a client of the socket's
                    automation plane (P34), offline through the same code
  debug             subscriptions — list the daemon's subscription registry
                    roundtrip — check a transcript survives a wire round trip
                    hammer — sustained writes, for the concurrency step
                    receipts STEP_ID — inspect unsafe tool receipts
  --print-services  Print the services this daemon registers, and exit
  --vectors         Print the statically linked sqlite-vec version, and exit
  help              Print this

Flags for ask:
  --route ID        which route to call (required)
  --system TEXT     an optional system prompt
  --max-tokens N    output cap; 0 uses the route's
  --temperature F   sampling temperature; omitted when not given
  --show-transcript print the canonical transcript when the reply completes

Flags for watch:
  --kinds loop.*    only these kinds; repeatable or comma-separated
  --component PATH  only this component root
  --after N         resume after seq N (default: from now)

Every client verb accepts --protocol-version N, which asks for a specific
protocol version. It is a debugging flag: use it to see a refusal.

The daemon owns loop execution; nothing is detached (P29). Stopping the
daemon stops every loop, and a crashed loop is recovered on restart.
`)
}

// printServices reports the daemon's composition without starting it.
//
// It exists for the demo's service check (D5), and it answers the question
// offline **on purpose**: "what would this daemon run" must be answerable
// without running it, because the reason to ask is usually that you do not
// want it running yet.
//
// Registered is not running — the pins decide that — so both are printed.
func printServices(w io.Writer) {
	registered := bootstrap.RegisteredNames()
	pins := bootstrap.Pins()

	pinned := make(map[string]bool, len(pins))
	for _, p := range pins {
		pinned[p.Service] = p.Enabled
	}

	fmt.Fprintf(w, "registered services (%d)\n", len(registered))
	for _, name := range registered {
		switch enabled, hasPin := pinned[name]; {
		case hasPin && enabled:
			fmt.Fprintf(w, "  %-12s pinned ON   — configuration cannot disable it\n", name)
		case hasPin && !enabled:
			fmt.Fprintf(w, "  %-12s pinned OFF  — configuration cannot enable it\n", name)
		default:
			fmt.Fprintf(w, "  %-12s config\n", name)
		}
	}
	fmt.Fprint(w, "\nfoundational services (app, config, logging) are wired by system.Initialize\n")
	fmt.Fprint(w, "and are deliberately absent from this list.\n")
}

// printVectors reports the statically linked extension version.
//
// # Why this verb exists in the binary rather than only in a test
//
// Two reasons, and the second is the one that matters for T4.
//
// It is the human-visible proof of B3: because `sqlite-vec` is compiled in
// rather than loaded, **there is no extension-missing state** for S4 or S22 to
// handle — and S22's availability model is one reason shorter because of it.
//
// And it is what puts the cgo dependency INSIDE the shipped binary. A package
// imported only by its own tests does not affect `go build ./cmd/aeon`, so a
// six-target measurement taken without this verb measures a binary that needs
// no C compiler — and reports a number far better than the truth.
func printVectors(w io.Writer) error {
	v, err := database.VecVersion()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "sqlite-vec  %s  (statically linked)\n", v)
	return nil
}

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/cjlapao/aeon/internal/model"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// The store's verbs: `db`, `loop`, `step`, and `debug hammer`.
//
// # Every one of them exists for a step in the demo
//
// `db info` prints the PATH FIRST, because every later slice's debugging starts
// by finding the file (B10). `step verify` is the human-visible form of the
// transcript fidelity test — somebody diagnosing a rejected replay at S7 runs
// it first. `db query awaiting` prints the QUERY, because anyone arriving from
// v1 will look for an `awaiting_user` column and not find one.
//
// # Two ways to reach the store, deliberately
//
// `loop`, `step` and `db query` go through the DAEMON, like every other client
// verb: they use the same handshake a third party would, and a CLI that
// shortcut it would prove nothing about the protocol.
//
// `db info` and `debug hammer` open the store DIRECTLY, and that is not a
// shortcut either:
//
//   - "where is the file" has to be answerable with the daemon DOWN. It is the
//     first question anybody asks when the daemon will not start, and routing
//     it through the daemon would make the answer unavailable exactly when it
//     is needed.
//   - `hammer` is the demo's concurrency step, and its whole point is a SECOND
//     PROCESS writing while the daemon serves reads. Going through the daemon
//     would make it one process writing to itself, which proves nothing about
//     `busy_timeout` or the write lock.
//   - `aeon policy` (T12) is the third direct user. S45 T8 (PM4) amended
//     its standing: authoring is the PERSON's socket door now (the 0600
//     endpoint, peer credentials — PL15 as amended names the CALLER human,
//     not the transport), every verb dials first, and the direct open is
//     the OFFLINE FALLBACK only — the three daemon-less cases, saying so on
//     stderr before any output. When it does run it is justified the old
//     way still: "go through the socket" is not a shortcut to avoid when no
//     socket is answering, the next daemon read sees the write because N8
//     forbids caching, and the second write handle is safe the way
//     `hammer`'s is (WAL, writer locking at BEGIN). The offline open lives
//     in policy_client.go, over policy.go's boundary seam and the same
//     RepoFuncs bridge the daemon's gate adapter uses.
//   - `aeon autonomy` (S56 T9) is the fourth: its verbs reach the store
//     directly ONLY as the documented read/restrictive fallback when no
//     daemon is running (the same service the daemon's socket hands speak,
//     over a second WAL handle), and never for grant — an allow is widened
//     through the daemon's peer-attested identity alone (AL13).

// quietFrameworkLogging turns the framework's INFO logging down for a CLI verb.
//
// The migration runner logs every migration it considers, which is right for a
// daemon's log and wrong for `aeon db info`: a person asking where the file is
// would get twenty lines of DDL progress first. Warn rather than Fatal, so a
// migration that actually FAILS still says so.
func quietFrameworkLogging() {
	logrus.SetLevel(logrus.WarnLevel)
}

// openStoreDirectly opens the store from this process.
//
// It is a SECOND handle on the same file, which WAL supports and which this
// slice's concurrency tests cover. The writer takes its lock at BEGIN
// (`_txlock=immediate`), so a write here waits for the daemon's rather than
// failing with SQLITE_BUSY on a lock upgrade.
func openStoreDirectly(noBackup bool) (*store.Store, error) {
	quietFrameworkLogging()
	s, err := store.Open(storeOpenOptions(noBackup, "cli"))
	if err != nil {
		return nil, fmt.Errorf("open the store: %w", err)
	}
	return s, nil
}

// --- db ----------------------------------------------------------------------

func cmdDB(w io.Writer, args []string) error {
	if len(args) == 0 {
		printDBUsage(w)
		return nil
	}
	switch args[0] {
	case "info":
		return cmdDBInfo(w, args[1:])
	case "upgrade":
		return cmdDBUpgrade(w, args[1:])
	case "query":
		return cmdDBQuery(w, args[1:])
	default:
		printDBUsage(w)
		return fmt.Errorf("unknown `db` subcommand %q", args[0])
	}
}

func printDBUsage(w io.Writer) {
	fmt.Fprint(w, `aeon db — the store

  db info [--vectors] [--fingerprint]   where it is, what is in it, what ran
  db upgrade --dry-run                  what an upgrade would apply
  db query awaiting [--component PATH]  the derived awaiting query, and its answer

  --no-backup (info, upgrade) open and migrate without the pre-migration
  backup — the safety net's only switch, recorded as a skipped row
`)
}

func cmdDBInfo(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("db info", flag.ContinueOnError)
	fs.SetOutput(w)
	vectors := fs.Bool("vectors", false, "print only the statically linked sqlite-vec version")
	fingerprint := fs.Bool("fingerprint", false, "print the normalised schema shape, for comparing two databases")
	// S62 T5 (BK18): the pre-migration safety net's one switch, scoped to
	// this open. A flag, never an environment variable.
	noBackup := fs.Bool("no-backup", false, "open and migrate without taking the pre-migration backup (records a skipped row)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	s, err := openStoreDirectly(*noBackup)
	if err != nil {
		return err
	}
	defer s.Close()

	if *vectors {
		writer, reader, err := s.VecVersion()
		if err != nil {
			return err
		}
		// Both pools, because an extension registered for one is a retrieval
		// slice that works in tests and fails under load (Q2). They are printed
		// separately rather than collapsed, so a disagreement is visible.
		fmt.Fprintf(w, "sqlite-vec  %s  (statically linked)\n", writer)
		if reader != writer {
			fmt.Fprintf(w, "            %s  on the reader pool — THE TWO DISAGREE, which should be impossible\n", reader)
		}
		fmt.Fprint(w, "\nThere is no extension to install and no way to be missing it: it is compiled\n"+
			"into the binary (B3). A failure here means the binary was built without cgo.\n")
		return nil
	}

	if *fingerprint {
		fp, err := s.SchemaFingerprint()
		if err != nil {
			return err
		}
		fmt.Fprintln(w, fp.String())
		return nil
	}

	in, err := s.Info()
	if err != nil {
		return err
	}

	// The path first (B10).
	fmt.Fprintf(w, "store   %s\n", in.Path)
	size := humanBytes(in.Bytes)
	if in.WALBytes > 0 {
		// The WAL is named, not summed. In WAL mode committed data lives there
		// until a checkpoint, so a store that has just been written to reports
		// a tiny main file — which reads as broken unless the log is shown.
		size += " + " + humanBytes(in.WALBytes) + " in the -wal"
	}
	fmt.Fprintf(w, "        %s, %s, foreign_keys %s, busy_timeout %dms\n",
		in.JournalMode, size, onOff(in.ForeignKeys), in.BusyTimeoutMS)
	fmt.Fprintf(w, "        pools: 1 writer (DB15 — WAL allows one), %d readers\n", in.ReadPoolSize)

	latest := "none"
	if n := len(in.Applied); n > 0 {
		latest = in.Applied[n-1].Name
	}
	fmt.Fprintf(w, "schema  %d migration(s) applied, latest %s\n", len(in.Applied), latest)
	if len(in.Pending) > 0 {
		fmt.Fprintf(w, "        %d PENDING — run `aeon db upgrade --dry-run` to see them\n", len(in.Pending))
	}
	for _, a := range in.Applied {
		if a.Status != "applied" {
			// A `failed` ledger row means an upgrade stopped part-way. It is
			// printed because the alternative is a store that looks fine.
			fmt.Fprintf(w, "        %-32s %s\n", a.Name, strings.ToUpper(a.Status))
		}
	}

	fmt.Fprintf(w, "vectors sqlite-vec %s (statically linked)\n", in.VecWriterVersion)

	fmt.Fprint(w, "\ntables  ")
	for i, t := range in.Tables {
		if i > 0 && i%6 == 0 {
			fmt.Fprint(w, "\n        ")
		}
		fmt.Fprintf(w, "%-20s", t)
	}
	fmt.Fprintln(w)

	// S62 T9 (BK15): exactly one final line — the backup family's state
	// in the store's own voice: the age of the last success (the
	// ledger's machine, not a guess) and the configured dir. The store
	// is already open here, so the line is what THIS open sees.
	if line, err := dbInfoBackupLine(s); err == nil {
		fmt.Fprintln(w, line)
	}
	return nil
}

func cmdDBUpgrade(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("db upgrade", flag.ContinueOnError)
	fs.SetOutput(w)
	dryRun := fs.Bool("dry-run", false, "list the pending migrations without applying them")
	// S62 T5 (BK18): the pre-migration safety net's one switch, scoped to
	// this open. A flag, never an environment variable.
	noBackup := fs.Bool("no-backup", false, "open and migrate without taking the pre-migration backup (records a skipped row)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*dryRun {
		// There is no apply path, and that is the design rather than an
		// omission: migrations run ON EVERY OPEN (DB8), so an apply command
		// would be a second way to do what starting the daemon already does —
		// and the second way is the one that gets run against the wrong file.
		return fmt.Errorf("`db upgrade` only supports --dry-run.\n" +
			"  Migrations run on every open, so starting the daemon (or any verb that opens the\n" +
			"  store) applies them. There is deliberately no second way to do it.")
	}

	s, err := openStoreDirectly(*noBackup)
	if err != nil {
		return err
	}
	defer s.Close()

	// Note the ordering: opening the store APPLIED them. So this reports what
	// WAS pending a moment ago, and says so — printing "none" without that
	// would be true and misleading.
	pending, err := s.Pending()
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		fmt.Fprintf(w, "no pending migrations — %s is up to date\n", s.Path())
		fmt.Fprint(w, "  (opening the store applies them, so this is the state AFTER that)\n")
		return nil
	}
	fmt.Fprintf(w, "%d migration(s) still pending after an open, which should not happen:\n", len(pending))
	for _, m := range pending {
		fmt.Fprintf(w, "  %-32s %s\n", m.Name, m.Description)
	}
	return nil
}

func cmdDBQuery(w io.Writer, args []string) error {
	if len(args) == 0 || args[0] != "awaiting" {
		return fmt.Errorf("`db query` supports one query: `awaiting`")
	}
	fs := flag.NewFlagSet("db query awaiting", flag.ContinueOnError)
	fs.SetOutput(w)
	comp := fs.String("component", "", "only this component root (default: every component)")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.AwaitingResult
	if err := c.Call(protocol.MethodStoreAwaiting,
		protocol.AwaitingParams{ComponentRoot: *comp, All: *comp == ""}, &res); err != nil {
		return err
	}

	// THE QUERY, printed. Anyone arriving from v1 will look for an
	// `awaiting_user` column; showing the derivation is how they learn why
	// there isn't one (DB6).
	fmt.Fprintln(w, res.SQL)
	if len(res.Loops) == 0 {
		fmt.Fprintln(w, "→ no loops awaiting an answer")
	} else {
		fmt.Fprintf(w, "→ %d loop(s) awaiting an answer\n", len(res.Loops))
		for _, l := range res.Loops {
			fmt.Fprintf(w, "  %-24s %s\n", l.Slug, l.ComponentRoot)
		}
	}
	// Printed WHATEVER the answer. The explanation is the reason this verb
	// exists — somebody arriving from v1 looking for `awaiting_user` needs it
	// most when the answer is "none", which is the common case.
	fmt.Fprint(w, "\nIt is DERIVED, not stored: a flag and an unanswered question can disagree, and\n"+
		"when they do nothing says which is right (DB6).\n")
	return nil
}

// --- loop --------------------------------------------------------------------

func cmdLoop(w io.Writer, args []string) error {
	if len(args) == 0 {
		printLoopUsage(w)
		return nil
	}
	switch args[0] {
	case "start":
		return cmdLoopStart(w, args[1:])
	case "show":
		return cmdLoopShow(w, args[1:])
	case "create":
		return cmdLoopCreate(w, args[1:])
	case "list":
		return cmdLoopList(w, args[1:])
	case "write":
		return cmdLoopWrite(w, args[1:])
	case "read":
		return cmdLoopRead(w, args[1:])
	case "message":
		// S69 T5: the human's terminal path into a resting conversation,
		// with --attach files put once and carried as references.
		return cmdLoopMessage(w, args[1:])
	case "messages":
		// S70 T5: the human's window onto the agents' own exchange — a
		// read, never a write (MB7; the person's door is `loop message`).
		return cmdLoopMessages(w, args[1:])
	case "archive":
		return cmdLoopArchive(w, args[1:])
	case "stop":
		// S55 T11: the control plane — clients of loop.stop|resume|control|
		// cancel|raise and the stop-request desk, never the store.
		return cmdLoopStop(w, args[1:])
	case "resume":
		return cmdLoopResume(w, args[1:])
	case "control":
		return cmdLoopControl(w, args[1:])
	case "todos":
		return cmdLoopTodos(w, args[1:])
	case "cancel":
		return cmdLoopCancel(w, args[1:])
	case "raise":
		return cmdLoopRaise(w, args[1:])
	case "requests":
		return cmdLoopRequests(w, args[1:])
	case "dismiss":
		return cmdLoopDismiss(w, args[1:])
	default:
		printLoopUsage(w)
		return fmt.Errorf("unknown `loop` subcommand %q", args[0])
	}
}

func printLoopUsage(w io.Writer) {
	fmt.Fprint(w, `aeon loop — loops and their documents

  loop create --slug NAME [--goal TEXT] [--mode MODE]
  loop start --blueprint NAME [--route ID] [--attended=false] "GOAL"
  loop show ID [--blueprint] [--json]
  loop list [--all] [--started-by trigger|webhook]
  loop write SLUG NAME --content TEXT
  loop read SLUG [NAME]
  loop message ID "TEXT" [--attach FILE]... [--route ID] — start a conversation turn in a resting loop (S69); each --attach file is put ONCE into the loop's frozen @attachments/ revisions and the message carries the references.
  loop messages ID [--thread N] [--json] — the agents' own exchange (S70): who asked whom what, verbatim, and where each row stands (posted · delivered · answered). A thread is its first ask's id (MB3). This verb READS; the person's door to a loop is the loop message verb.
  loop archive [--note TEXT] ID — cold storage: the honest Delete (S33).
             The loop's documents ride along WITH their state; the telemetry
             rollups do not move — counted work stays counted (D8)
   loop stop ID [--reason T] [--json] — interrupt the loop's work (S55)
   loop resume ID [--message T] [--json] — resume a parked loop (S55)
   loop control ID [--json] — the loop's status, its four eligibility
              verdicts, and the recent audit (S55)
   loop todos ID [--json] — the loop's todo list, active or last closed (S75)
   loop todos close ID [--json] — close the active list (the person's door, S75)
   loop cancel ID --reason T --yes [--json] — destroy progress, audited (S55)
   loop raise ID [--budget B] [--usd N] [--tokens N] [--wall DUR] [--steps N]
              --reason T --yes [--json] — lift the cap the loop is parked
              on, and re-run it (S55)
   loop requests [ID] [--status open|honored|dismissed] [--json] —
              the stop-request desk (S55)
   loop dismiss REQ [--json] — settle an open stop request (S55)

A loop is identified by its slug AND its component root together, so the
same slug in two checkouts is two loops. --component overrides the working
directory. The component is what the loop belongs to (S38 OL1); the group
above it is the project tier, reachable with the aeon project verb.
`)
}

// cmdLoopArchive is the CLI client of loop.archive (S33 T9): cold storage,
// never destruction — and the printed line says so beside the counters,
// because an operator who believes archive deleted the loop will be
// surprised later.
func cmdLoopArchive(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop archive", flag.ContinueOnError)
	fs.SetOutput(w)
	note := fs.String("note", "", "why this loop goes to cold storage")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon loop archive [--note TEXT] ID — `aeon loop list` names the loops that exist")
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var result struct {
		ID         string `json:"id"`
		Slug       string `json:"slug"`
		Status     string `json:"status"`
		StateBytes int    `json:"stateBytes"`
		ArchivedAt string `json:"archivedAt"`
		Refused    *struct {
			Kind   string `json:"kind"`
			Detail string `json:"detail"`
		} `json:"refused"`
	}
	if err := c.Call(protocol.MethodLoopArchive, protocol.LoopArchiveParams{ID: positional[0], Note: *note}, &result); err != nil {
		return err
	}
	if result.Refused != nil {
		fmt.Fprintf(w, "refused: %s — %s\n", result.Refused.Kind, result.Refused.Detail)
		return nil
	}
	fmt.Fprintf(w, "archived loop %s (status at archive: %s) — %d bytes of state in cold storage as %s. Nothing was destroyed, and the telemetry rollups did not move: counted work stays counted.\n",
		result.Slug, result.Status, result.StateBytes, result.ID)
	return nil
}

// componentFlag adds --component, defaulting to the working directory.
// (S38 T4: this was --project while the tier it names was called project;
// OL1 renamed the tier, and the flag moved with it — the flag is a CLI
// spelling, not a wire name, so it carries no compatibility promise here
// beyond what P34 gives it: the socket is the door, this is the shortcut.)
//
// The CLIENT's working directory, not the daemon's. A loop created against
// "wherever the daemon happens to be" is a loop in a project nobody chose, and
// the daemon is usually started from somewhere else entirely.
func componentFlag(fs *flag.FlagSet) *string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	return fs.String("component", cwd, "the component root (default: the working directory)")
}

func cmdLoopCreate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop create", flag.ContinueOnError)
	fs.SetOutput(w)
	slug := fs.String("slug", "", "the loop's slug (required)")
	title := fs.String("title", "", "a title; defaults to the slug")
	goal := fs.String("goal", "", "what the loop is for")
	mode := fs.String("mode", "", "simple | standard | poc (default: standard)")
	comp := componentFlag(fs)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *slug == "" {
		return fmt.Errorf("`loop create` needs --slug")
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var l protocol.LoopView
	if err := c.Call(protocol.MethodLoopCreate, protocol.LoopCreateParams{
		Slug: *slug, ComponentRoot: *comp, Title: *title, Goal: *goal, Mode: *mode,
	}, &l); err != nil {
		return err
	}
	fmt.Fprintf(w, "%-24s %s   created · %s · %s\n", l.Slug, l.ComponentRoot, l.Mode, l.Phase)
	return nil
}

func cmdLoopList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop list", flag.ContinueOnError)
	fs.SetOutput(w)
	all := fs.Bool("all", false, "every component on this machine, not only this one")
	startedBy := fs.String("started-by", "", "only loops started by this hand: trigger or webhook (S20); empty shows every hand")
	comp := componentFlag(fs)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	params := protocol.LoopListParams{ComponentRoot: *comp, StartedBy: *startedBy}
	if *all {
		// SCOPED BY DEFAULT (T11). A daemon multiplexes every component on the
		// machine, so an unscoped default would show a person every
		// repository's loops whenever they were in one — and they would learn
		// to ignore the list.
		params = protocol.LoopListParams{All: true, StartedBy: *startedBy}
	}

	var res protocol.LoopListResult
	if err := c.Call(protocol.MethodLoopList, params, &res); err != nil {
		return err
	}
	if len(res.Loops) == 0 {
		if res.All {
			fmt.Fprintln(w, "no loops on this machine")
		} else {
			fmt.Fprintf(w, "no loops in %s  (try --all)\n", res.ComponentRoot)
		}
		return nil
	}
	for _, l := range res.Loops {
		marker := " "
		if l.Awaiting {
			// Derived, every time it is printed (DB6).
			marker = "?"
		}
		hand := ""
		if l.StartedBy != "" {
			// The provenance sentence (§1's shape): who started this loop,
			// printed beside when it moved — and never confused with
			// attendedness, which loop show answers separately (R-3).
			hand = "  started-by: " + l.StartedBy
		}
		fmt.Fprintf(w, "%s %-24s %-40s %-10s %s%s\n", marker, l.Slug, l.ComponentRoot, l.Phase, l.UpdatedAt, hand)
	}
	fmt.Fprint(w, "\n? = awaiting an answer, derived from the question table\n")
	return nil
}

func cmdLoopWrite(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop write", flag.ContinueOnError)
	fs.SetOutput(w)
	content := fs.String("content", "", "the document's content")
	by := fs.String("by", "", "who wrote it")
	comp := componentFlag(fs)
	want := addProtocolVersionFlag(fs)

	// The two positional arguments come first, so a refused NAME is still
	// reported with its reason rather than as a flag parse error.
	positional, rest := splitPositional(args, 2)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) < 2 {
		return fmt.Errorf("`loop write` needs a slug and a document name: aeon loop write SLUG NAME --content TEXT")
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.DocumentWriteResult
	err = c.Call(protocol.MethodLoopWrite, protocol.DocumentWriteParams{
		Slug: positional[0], ComponentRoot: *comp, Name: positional[1],
		Content: *content, WrittenBy: *by,
	}, &res)
	if err != nil {
		// Rendered in three parts by main's renderError. A refused document
		// path says what to do, which is the whole reason the refusal carries
		// a reason rather than a boolean.
		return err
	}
	if res.Unchanged {
		fmt.Fprintf(w, "%s  unchanged at rev %d — the content matches, so no revision was created\n",
			res.Document.File, res.Document.Rev)
		return nil
	}
	fmt.Fprintf(w, "%s  rev %d · %d bytes · %s\n",
		res.Document.File, res.Document.Rev, res.Document.Bytes, res.Document.SHA256[:12])
	return nil
}

func cmdLoopRead(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop read", flag.ContinueOnError)
	fs.SetOutput(w)
	comp := componentFlag(fs)
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 2)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) == 0 {
		return fmt.Errorf("`loop read` needs a slug: aeon loop read SLUG [NAME]")
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	name := ""
	if len(positional) > 1 {
		name = positional[1]
	}
	params := protocol.DocumentReadParams{Slug: positional[0], ComponentRoot: *comp, Name: name}

	if name == "" {
		var res protocol.DocumentListResult
		if err := c.Call(protocol.MethodLoopRead, params, &res); err != nil {
			return err
		}
		if len(res.Documents) == 0 {
			fmt.Fprintf(w, "%s has no documents\n", positional[0])
			return nil
		}
		for _, d := range res.Documents {
			fmt.Fprintf(w, "%-24s rev %-3d %8d bytes  %s\n", d.File, d.Rev, d.Bytes, d.WrittenAt)
		}
		return nil
	}

	var doc protocol.DocumentView
	if err := c.Call(protocol.MethodLoopRead, params, &doc); err != nil {
		return err
	}
	fmt.Fprint(w, doc.Content)
	if !strings.HasSuffix(doc.Content, "\n") {
		fmt.Fprintln(w)
	}
	return nil
}

// --- step --------------------------------------------------------------------

func cmdStep(w io.Writer, args []string) error {
	if len(args) == 0 {
		printStepUsage(w)
		return nil
	}
	switch args[0] {
	case "stop":
		// S55 T11: a person interrupting a step (and resuming one they
		// stopped) — explicit cases BEFORE the prompt default, which takes
		// anything else.
		return cmdStepStop(w, args[1:])
	case "continue":
		return cmdStepContinue(w, args[1:])
	case "show":
		return cmdStepShow(w, args[1:])
	case "verify":
		return cmdStepVerify(w, args[1:])
	case "list":
		return cmdStepList(w, args[1:])
	case "run":
		return cmdStepRun(w, args[1:])
	default:
		// Anything else is a PROMPT (S5). The thing a person types first is
		// what they want done, and `aeon step "count the files in docs/"`
		// running a step is the difference between a demo that is one command
		// and one that is two.
		//
		// A flag first is a prompt too — `aeon step --allow-bash "…"` — and
		// the flag set catches a misspelled one.
		return cmdStepRun(w, args)
	}
}

func printStepUsage(w io.Writer) {
	fmt.Fprint(w, `aeon step — steps and their transcripts

  step "PROMPT"             run one bounded step, streaming it as it happens
  step show ID [--json] [--input] the step, and its canonical transcript (--input: the dispatch input — a redo's reason lives there)
           [--manifest]     the frozen context manifest, with its hashes
           [--requests]     the model calls, reconstructed from the transcript
           [--compactions]  the summaries that replaced transcript ranges
  step verify ID            does the transcript round-trip byte for byte
  step list SLUG            a loop's steps, without their transcripts
  step stop ID [--json]     interrupt a running step — it parks at the next
                            turn boundary (S55)
  step continue ID [--message T] [--route R] [--reasoning-effort E]
                            resume a step a person stopped (S55); a step
                            stopped on a budget answers loop.raise instead

Flags for a run:
  --workspace DIR           the project the step reads; default: this directory
  --tools a,b               the grant list; default: read, list, write
  --allow-bash              also grant the shell. Commands are governed by
                            command_access policy (S11): an unruled command
                            denies, a ruled ask suspends for an answer
  --allow-question          let the step ask a question and suspend
  --context-files a,b       freeze and hash these files before the first turn
  --max-iterations N        override the loop bound
  --route ID                which model; optional with one configured route
  --loop SLUG               file the step under this loop
  --quiet                   print only the outcome
  --json                    print the result as JSON
`)
}

func cmdStepShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("step show", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the canonical transcript as JSON")
	showManifest := fs.Bool("manifest", false, "print the frozen context manifest instead")
	showRequests := fs.Bool("requests", false, "print the model calls reconstructed from the transcript")
	showCompactions := fs.Bool("compactions", false, "print the recorded compaction ranges and summaries")
	showInput := fs.Bool("input", false, "print the step's input — the first user message of the transcript (S17 T3: a redo's rejection reason lives HERE, and §2's check reads it)")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) == 0 {
		return fmt.Errorf("`step show` needs a step id")
	}
	id, err := strconv.ParseInt(positional[0], 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a step id: %w", positional[0], err)
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var st protocol.StepView
	if err := c.Call(protocol.MethodStepShow, protocol.StepShowParams{ID: id}, &st); err != nil {
		return err
	}

	if *showManifest {
		return renderStepManifest(w, st.ContextManifest)
	}
	if *showInput {
		// The INPUT, verbatim, as one block. The redo sentence ("redo of
		// "verify": retry 1 of 2 — <reason>") reaches the handler through
		// this message, so §2's check — "is the rejection's text in the
		// re-entered step's input?" — is answered by THIS row, not by a
		// log line the engine could have forgotten to write.
		text, found, err := firstUserText(st.Messages)
		if err != nil {
			return err
		}
		if !found {
			fmt.Fprintf(w, "step %d has no user message on the record — `step verify` says why\n", id)
			return nil
		}
		fmt.Fprintln(w, text)
		return nil
	}
	if *showRequests {
		return renderStepRequests(w, st.Messages)
	}
	if *showCompactions {
		return renderStepCompactions(w, st.ContextManifest)
	}

	if *asJSON {
		// The TRANSCRIPT, not the step. `--json` exists so a person can compare
		// it byte for byte with what went to the provider, and wrapping it in
		// the step's metadata would make that comparison harder.
		if len(st.Messages) == 0 {
			return fmt.Errorf("step %d has no readable transcript", id)
		}
		var pretty strings.Builder
		if err := indentJSON(&pretty, st.Messages); err != nil {
			return err
		}
		fmt.Fprintln(w, pretty.String())
		return nil
	}

	renderStepFacts(w, st)
	if len(st.Messages) == 0 {
		fmt.Fprint(w, "\nNo readable transcript. `step verify` says why.\n")
		return nil
	}
	fmt.Fprintln(w)
	return renderTranscript(w, st.Messages)
}

func renderStepFacts(w io.Writer, st protocol.StepView) {
	fmt.Fprintf(w, "step %d  %s/%s\n", st.ID, st.ComponentRoot, st.LoopSlug)
	fmt.Fprintf(w, "  phase   %s · agent %s · status %s\n", st.Phase, st.Agent, st.Status)
	if st.Outcome != "" {
		fmt.Fprintf(w, "  outcome %s\n", st.Outcome)
	}
	if st.Recovery != nil {
		fmt.Fprintf(w, "  recovery case %s", st.Recovery.Case)
		if st.Recovery.Reissued {
			fmt.Fprint(w, " · model call re-issued")
		}
		fmt.Fprintln(w)
		if st.Recovery.Failure != nil {
			fmt.Fprintf(w, "  failure  %s", st.Recovery.Failure.Kind)
			if st.Recovery.Failure.Tool != "" {
				fmt.Fprintf(w, " · tool %s", st.Recovery.Failure.Tool)
			}
			fmt.Fprintln(w)
		}
		if st.Recovery.Reason != "" {
			fmt.Fprintf(w, "  reason   %s\n", st.Recovery.Reason)
		}
	}
	fmt.Fprintf(w, "  started %s\n", st.StartedAt)
	if st.EndedAt != "" {
		fmt.Fprintf(w, "  ended   %s\n", st.EndedAt)
	}
	fmt.Fprintf(w, "  cost    %s\n", renderCost(st.Cost))
	if len(st.ContextManifest) > 0 {
		fmt.Fprintf(w, "  context %s\n", string(st.ContextManifest))
	}
}

// renderCost prints all three states honestly.
//
// A surface that cannot show `unknown` is a surface that will show a confident
// wrong answer — so `partial` says "at least", and `unknown` says why.
func renderCost(c protocol.CostView) string {
	switch c.State {
	case "known":
		// MoneyString, not `%f`: the literal `$0.00` is the look of free,
		// and five decimals of a real charge start "$0.00x" and get read as
		// free too (S18 T7, D9).
		return fmt.Sprintf("%s (known)", model.MoneyString(c.USD))
	case "partial":
		return fmt.Sprintf("at least %s (partial — unpriced: %s)",
			model.MoneyString(c.USDAtLeast), strings.Join(c.Missing, ", "))
	case "unknown":
		if c.USDAtLeast > 0 {
			return fmt.Sprintf("at least %s (unknown — %s)", model.MoneyString(c.USDAtLeast), c.Reason)
		}
		return fmt.Sprintf("unknown (%s)", c.Reason)
	default:
		return fmt.Sprintf("unknown (no state stored: %q)", c.State)
	}
}

// renderTranscript prints a transcript a person can read.
// firstUserText extracts the first user message's text blocks — the step's
// input as the dispatch composed it. Absent is its own honest answer
// ("no user message on the record"), never an empty string posing as one.
func firstUserText(raw json.RawMessage) (string, bool, error) {
	var msgs []struct {
		Role    string `json:"role"`
		Content []struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if len(raw) == 0 || string(raw) == "null" {
		return "", false, nil
	}
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return "", false, fmt.Errorf("the transcript could not be read: %w", err)
	}
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}
		var b strings.Builder
		for _, blk := range m.Content {
			if blk.Kind == "text" && blk.Text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(blk.Text)
			}
		}
		return b.String(), true, nil
	}
	return "", false, nil
}

func renderTranscript(w io.Writer, raw json.RawMessage) error {
	var msgs []struct {
		Role    string `json:"role"`
		Content []struct {
			Kind      string          `json:"kind"`
			Text      string          `json:"text"`
			Signature string          `json:"signature"`
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Input     json.RawMessage `json:"input"`
			ToolUseID string          `json:"toolUseId"`
			Content   string          `json:"content"`
			IsError   bool            `json:"isError"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return fmt.Errorf("the transcript could not be read: %w", err)
	}

	for i, m := range msgs {
		fmt.Fprintf(w, "  %d. %s\n", i+1, strings.ToUpper(m.Role))
		for _, b := range m.Content {
			switch b.Kind {
			case "text":
				fmt.Fprintf(w, "       %s\n", indentLines(b.Text, "       "))
			case "thinking":
				sig := "UNSIGNED"
				if b.Signature != "" {
					// The length, not the signature: it is long, opaque, and
					// the only question a person has about it is whether it is
					// intact.
					sig = fmt.Sprintf("signed, %d bytes", len(b.Signature))
				}
				fmt.Fprintf(w, "       [thinking · %s]\n       %s\n", sig, indentLines(b.Text, "       "))
			case "tool_use":
				fmt.Fprintf(w, "       [tool_use %s · %s] %s\n", b.ID, b.Name, string(b.Input))
			case "tool_result":
				marker := ""
				if b.IsError {
					marker = " · ERROR"
				}
				fmt.Fprintf(w, "       [tool_result %s%s] %s\n", b.ToolUseID, marker, b.Content)
			}
		}
	}
	return nil
}

func cmdStepVerify(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("step verify", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) == 0 {
		return fmt.Errorf("`step verify` needs a step id")
	}
	id, err := strconv.ParseInt(positional[0], 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a step id: %w", positional[0], err)
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.StepVerifyResult
	if err := c.Call(protocol.MethodStepVerify, protocol.StepShowParams{ID: id}, &res); err != nil {
		return err
	}

	if !res.OK {
		fmt.Fprintf(w, "transcript NOT verified · %s\n", res.Mismatch)
		fmt.Fprint(w, "\nS7 recovers a crashed step by replaying exactly these rows, so this is the\n"+
			"failure that would otherwise surface there as a provider 400 about signatures.\n")
		return fmt.Errorf("step %d does not verify", id)
	}

	fmt.Fprintf(w, "transcript verified · %d messages", res.Messages)
	switch {
	case res.SignatureLength > 0:
		fmt.Fprintf(w, " · thinking signature byte-identical (%d bytes)", res.SignatureLength)
	default:
		// Said explicitly. A transcript with no signature verifies, and
		// printing nothing would let a reader believe one was checked.
		fmt.Fprint(w, " · no thinking signature in this transcript")
	}
	fmt.Fprintln(w)
	if res.Digest != "" {
		fmt.Fprintf(w, "digest %s\n", res.Digest[:16])
	}
	return nil
}

func cmdStepList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("step list", flag.ContinueOnError)
	fs.SetOutput(w)
	comp := componentFlag(fs)
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) == 0 {
		return fmt.Errorf("`step list` needs a loop slug")
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.StepListResult
	if err := c.Call(protocol.MethodStepList,
		protocol.DocumentReadParams{Slug: positional[0], ComponentRoot: *comp}, &res); err != nil {
		return err
	}
	if len(res.Steps) == 0 {
		fmt.Fprintf(w, "%s has no steps\n", positional[0])
		return nil
	}
	for _, st := range res.Steps {
		fmt.Fprintf(w, "%-6d %-10s %-24s %s\n", st.ID, st.Status, st.Agent, renderCost(st.Cost))
	}
	return nil
}

// --- debug hammer ------------------------------------------------------------

// cmdDebugHammer drives sustained writes, for the demo's concurrency step.
//
// Not a benchmark — a LOAD GENERATOR a person can run in one terminal while
// reading in another. It opens the store directly, which is the point: the
// claim being demonstrated is one writer and many readers across PROCESSES,
// and a hammer that went through the daemon would be the daemon writing to
// itself.
func cmdDebugHammer(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("debug hammer", flag.ContinueOnError)
	fs.SetOutput(w)
	writes := fs.Int("writes", 1000, "how many events to append")
	every := fs.Int("report-every", 200, "print progress every N writes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *writes <= 0 {
		return fmt.Errorf("--writes must be positive")
	}

	s, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer s.Close()

	fmt.Fprintf(w, "hammering %s with %d writes from this process\n", s.Path(), *writes)
	fmt.Fprint(w, "read from another terminal while this runs: `aeon db info`, `aeon watch`\n\n")

	start := time.Now()
	var worst time.Duration
	for i := 0; i < *writes; i++ {
		at := time.Now()
		if _, err := s.AppendEvent(store.NewEvent{
			Kind:    "debug.hammer",
			Payload: map[string]any{"i": i},
		}); err != nil {
			// Reported with the index, because WHICH write failed is the fact
			// that distinguishes "it broke immediately" from "it broke under
			// sustained load".
			return fmt.Errorf("write %d of %d failed: %w", i, *writes, err)
		}
		if took := time.Since(at); took > worst {
			worst = took
		}
		if *every > 0 && (i+1)%*every == 0 {
			fmt.Fprintf(w, "  %d/%d · worst single write %v\n", i+1, *writes, worst.Round(time.Microsecond))
		}
	}

	elapsed := time.Since(start)
	head, _ := s.HeadSeq()
	fmt.Fprintf(w, "\n%d writes in %v (%.0f/s) · worst single write %v\n",
		*writes, elapsed.Round(time.Millisecond),
		float64(*writes)/elapsed.Seconds(), worst.Round(time.Microsecond))
	fmt.Fprintf(w, "seq is now %d\n", head)
	fmt.Fprint(w, "\nNothing should have reported SQLITE_BUSY: the writer takes its lock at BEGIN\n"+
		"(`_txlock=immediate`), so a second writer waits rather than failing a lock upgrade.\n")
	return nil
}

// --- small shared helpers ----------------------------------------------------

// splitPositional takes up to n leading arguments that are not flags.
//
// Needed because Go's flag package stops at the first positional argument, and
// these verbs read better with the subject first (`step show 4`) than with it
// last. Anything after the first flag is left to the FlagSet.
func splitPositional(args []string, n int) (positional, rest []string) {
	for i, a := range args {
		if len(positional) == n || strings.HasPrefix(a, "-") {
			return positional, args[i:]
		}
		positional = append(positional, a)
	}
	return positional, nil
}

func indentJSON(w io.Writer, raw json.RawMessage) error {
	var into any
	if err := json.Unmarshal(raw, &into); err != nil {
		return fmt.Errorf("the transcript is not valid JSON: %w", err)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(into)
}

func indentLines(text, prefix string) string {
	return strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\n"+prefix)
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KiB", n/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

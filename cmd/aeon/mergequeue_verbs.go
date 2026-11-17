package main

// cmd/aeon/mergequeue_verbs.go — S60 T10 (MQ17, standing rule 1, P5's
// client half). Every queue operation a PERSON can do is a verb that dials
// the daemon, prints what the wire said, and re-validates nothing: the ten
// `mergequeue.*` methods T8 registered, one verb each, zero new behaviour.
// Rule 1 holds trivially — there is no CLI-only path, no store import
// (TestMergeQueueVerbsNeverOpenTheStore is the teeth), no second renderer
// of a closed set: `--help` for every set-valued flag is GENERATED from the
// plane's own `mergequeue.vocabulary` answer, live when the daemon is up,
// and says so plainly when it is down. The recorded P5 deviation this slice
// owns: a queue is meaningless without the daemon's loops, so there is NO
// offline door here — a daemon that is not running is the answer, not a
// state to route around.
//
// The actor is never a flag (MQ13): the connection resolves the person, and
// every write carries what the CONNECTION said, not what argv whispered.
// No flag, stdin or prompt takes a secret; nothing here can print a token
// (SEC-033 — the wire shapes this file prints carry ids, enums, counts and
// paths only).
//
// The two seatbelts (behaviour 2, a `--yes` each, seatbelts not security):
//
//   - `settings` that WIDENS authority answers with the daemon's own
//     statement inside a refusal (T8 behaviour 2 — the refusal answers
//     first, so the person reads exactly what the change means BEFORE it
//     can stand); `--yes` echoes back the digest the sentence itself quoted
//     — the same read-the-statement/echo-the-digest round trip S55's
//     loosenGate runs, with the digest parsed out of the daemon's words
//     rather than re-derived (re-deriving the digest formula here would be
//     the second formatter this slice exists to avoid). Declining sends
//     nothing.
//   - `land` is bound to what the person SAW: the CLI reads
//     `mergequeue.show` first, prints the row and the driver's nextStep,
//     and only `--yes` sends the land with the digest that answer carried.
//     An item that moved under the print refuses by digest, and says so —
//     the CLI never lands on a hope.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/workspace"
)

// mergeQueueVocabularyDown is behaviour 1's other half: the set-valued
// flags carry the daemon's closed sets in their help, and when nobody is
// home the help SAYS it instead of typing a shadow copy.
const mergeQueueVocabularyDown = "start the daemon to list the allowed values"

// mqDial is the dial every verb shares; a var so the in-process tests can
// stand a daemon in without touching the real endpoint (the historyDial /
// envDial idiom). It is RAW dial: the standard "daemon not running" words
// are dial's own and this file adds none to them (EV18's rule, kept), and
// there is no offline arm to route around — the queue's moves run inside
// the daemon's loops (the recorded P5 deviation; behaviour 3).
var mqDial = dial

func cmdMergeQueue(w io.Writer, args []string) error {
	if len(args) == 0 {
		printMergeQueueUsage(w)
		return nil
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "list":
		return mqVerbList(w, rest)
	case "show":
		return mqVerbShow(w, rest)
	case "status":
		return mqVerbStatus(w, rest)
	case "vocabulary":
		return mqVerbVocabulary(w, rest)
	case "enqueue":
		return mqVerbEnqueue(w, rest)
	case "dequeue":
		return mqVerbID(w, rest, "dequeue", protocol.MethodMergeQueueDequeue)
	case "reorder":
		return mqVerbReorder(w, rest)
	case "retry":
		return mqVerbID(w, rest, "retry", protocol.MethodMergeQueueRetry)
	case "land":
		return mqVerbLand(w, rest)
	case "settings":
		return mqVerbSettings(w, rest)
	case "help":
		printMergeQueueUsage(w)
		return nil
	default:
		printMergeQueueUsage(w)
		return &usageError{msg: fmt.Sprintf("unknown `mergequeue` subcommand %q — `aeon mergequeue help` lists the ten", verb)}
	}
}

func printMergeQueueUsage(w io.Writer) {
	fmt.Fprint(w, `usage: aeon mergequeue <verb> — the person's doors onto the merge queue (S60)

  list      [--repo PATH] [--status S] [--all] [--json] — the queue, in
            queue order; --all brings the terminal rows too
  show      ID [--json] — one item, its ledger, and the driver's nextStep
  status    [--repo PATH] [--json] — one repository: settings, counts,
            lane heads, open rows
  vocabulary [--json] — every closed set the plane speaks, derived live
  enqueue   --branch B [--repo PATH] [--base BR] [--unattended] [--json]
            — the person's own enqueue (attended is declared HERE, G14)
  dequeue   ID — take an item out of the queue
  reorder   ID [--before ID2] — move a QUEUED item (omitted --before: last)
  retry     ID — answer a failure: failed back to queued
  land      ID --yes — land the row YOU SAW, bound to its digest
  settings  [--repo PATH] [--mode M] [--conflict-policy P]
            [--land-method L] [--max-catch-ups N] [--yes] [--json]

  Every verb is a client of the daemon's socket — there is no offline door:
  the queue's moves run inside the daemon's loops. --repo defaults to the
  working directory. The set-valued flags take the daemon's vocabulary:
  `+"`aeon mergequeue vocabulary`"+` lists the allowed values.
`)
}

// mqWantFromArgs pre-reads ONLY the --protocol-version debugging flag so a
// verb can dial (and thus fetch the vocabulary that decorates its own
// help) BEFORE parsing. It consumes nothing else — no value a verb acts on
// is ever read outside its FlagSet.
func mqWantFromArgs(args []string) int {
	for i, a := range args {
		if a == "--protocol-version" || a == "-protocol-version" {
			if i+1 < len(args) {
				var n int
				if _, err := fmt.Sscanf(args[i+1], "%d", &n); err == nil {
					return n
				}
			}
			return 0
		}
		if strings.HasPrefix(a, "--protocol-version=") {
			var n int
			if _, err := fmt.Sscanf(strings.TrimPrefix(a, "--protocol-version="), "%d", &n); err == nil {
				return n
			}
		}
	}
	return 0
}

// mqVocabulary fetches the plane's closed sets over an already-dialled
// connection. The set-valued flags' help and membership checks are built
// from this answer alone — behaviour 1's "no second typed list", proven by
// TestHelpSetsComeFromVocabulary (adding a constant to the daemon's tables
// changes this CLI's help with no edit to this file).
func mqVocabulary(ctx context.Context, c *protocol.Client) (protocol.VocabularyResult, error) {
	var v protocol.VocabularyResult
	err := c.Call(protocol.MethodMergeQueueVocabulary, struct{}{}, &v)
	return v, err
}

// mqStateNames / mqSetNames render a closed set for help lines and
// membership refusals.
func mqStateNames(v protocol.VocabularyResult) []string {
	out := make([]string, 0, len(v.States))
	for _, s := range v.States {
		out = append(out, s.Name)
	}
	return out
}

// mqMember is the membership check's one implementation (a help-generated
// set, not a typed one): a value the plane's vocabulary does not name is
// refused HERE as a usage error — the daemon still holds the real veto.
func mqMember(value string, set []string) bool {
	for _, s := range set {
		if s == value {
			return true
		}
	}
	return false
}

// mqRepoFlag is the component-root flag (the store_verbs.go componentFlag
// shape under this plane's own --repo word, default: the working directory;
// no store is opened to resolve it — the daemon names what is a component).
func mqRepoFlag(fs *flag.FlagSet, def string) *string {
	return fs.String("repo", def, "the repository's component root (default: the working directory)")
}

func mqCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// --- reads ---------------------------------------------------------------------

func mqVerbList(w io.Writer, args []string) error {
	want := mqWantFromArgs(args)
	c, err := mqDial(want)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	// The help line for --status carries the LIVE vocabulary — behaviour 1:
	// the daemon's answer is the only list this CLI can speak, and the
	// FlagSet is only defined once the dial has answered.
	vocab, verr := mqVocabulary(context.Background(), c)
	statusHelp := mergeQueueVocabularyDown
	if verr == nil {
		statusHelp = "only rows in this state (the plane's states: " + strings.Join(mqStateNames(vocab), ", ") + ")"
	}

	fs := flag.NewFlagSet("mergequeue list", flag.ContinueOnError)
	fs.SetOutput(w)
	repo := mqRepoFlag(fs, mqCwd())
	status := fs.String("status", "", statusHelp)
	all := fs.Bool("all", false, "include the terminal rows (landed, failed, removed) as well as the open queue")
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *status != "" {
		if verr != nil {
			return verr
		}
		if len(mqStateNames(vocab)) > 0 && !mqMember(*status, mqStateNames(vocab)) {
			return &usageError{msg: fmt.Sprintf("`mergequeue list --status %s` names no state — the plane's states are %s",
				*status, strings.Join(mqStateNames(vocab), ", "))}
		}
	}

	p := protocol.MergeQueueListParams{ComponentRoot: *repo, IncludeTerminal: *all}
	if *status == "" && *asJSON {
		return callJSON(w, c, protocol.MethodMergeQueueList, p)
	}
	var items []protocol.ItemView
	if err := c.Call(protocol.MethodMergeQueueList, p, &items); err != nil {
		return err
	}
	if *status != "" {
		// The door's answer carries every row it was asked for; --status
		// SELECTS which of those rows print. The kept rows still print as
		// the wire bytes they arrived as — no re-encode of any row's shape
		// (TestJSONIsTheWireVerbatim covers the unfiltered door-to-stdout
		// path; the filtered array below holds those same row objects).
		kept := make([]protocol.ItemView, 0, len(items))
		for _, it := range items {
			if it.Status == *status {
				kept = append(kept, it)
			}
		}
		items = kept
	}
	if *asJSON {
		raw, mErr := json.Marshal(items)
		if mErr != nil {
			return mErr
		}
		fmt.Fprintln(w, string(raw))
		return nil
	}
	renderMergeQueueItems(w, items)
	return nil
}

func mqVerbShow(w io.Writer, args []string) error {
	want := mqWantFromArgs(args)
	c, err := mqDial(want)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	positional, rest := splitPositional(args, 1)
	fs := flag.NewFlagSet("mergequeue show", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the row, the ledger and the next step")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return &usageError{msg: "`aeon mergequeue show` needs exactly one item id — `aeon mergequeue list` names them"}
	}
	p := protocol.MergeQueueShowParams{ID: positional[0]}
	if *asJSON {
		return callJSON(w, c, protocol.MethodMergeQueueShow, p)
	}
	var res protocol.ShowResult
	if err := c.Call(protocol.MethodMergeQueueShow, p, &res); err != nil {
		return err
	}
	renderMergeQueueItem(w, res.Item)
	if res.Worktree != "" {
		fmt.Fprintf(w, "  worktree  %s\n", res.Worktree)
	}
	fmt.Fprintf(w, "  next step %s\n", res.Item.NextStep)
	if len(res.Transitions) > 0 {
		fmt.Fprintln(w, "  ledger (append-only, in order):")
		for _, t := range res.Transitions {
			move := ""
			if t.FromStatus != "" || t.ToStatus != "" {
				move = fmt.Sprintf(" %s→%s", t.FromStatus, t.ToStatus)
			}
			extra := strings.TrimSpace(t.ReasonKind + " " + t.Detail)
			fmt.Fprintf(w, "    %-24s %-14s%s %s/%s%s\n", t.At, t.Kind, move, t.ActorKind, t.ActorID,
				func() string {
					if extra == "" {
						return ""
					}
					return " · " + extra
				}())
		}
	}
	return nil
}

func mqVerbStatus(w io.Writer, args []string) error {
	want := mqWantFromArgs(args)
	c, err := mqDial(want)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	fs := flag.NewFlagSet("mergequeue status", flag.ContinueOnError)
	fs.SetOutput(w)
	repo := mqRepoFlag(fs, mqCwd())
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the view")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	p := protocol.MergeQueueStatusParams{ComponentRoot: *repo}
	if *asJSON {
		return callJSON(w, c, protocol.MethodMergeQueueStatus, p)
	}
	var res protocol.StatusResult
	if err := c.Call(protocol.MethodMergeQueueStatus, p, &res); err != nil {
		return err
	}
	cfgWord := "settings"
	if res.Config.Absent {
		cfgWord = "settings (never written — the defaults stand)"
	}
	fmt.Fprintf(w, "%s\n  mode %s · conflict policy %s · land method %s · max catch-ups %d",
		cfgWord, res.Config.Mode, res.Config.ConflictPolicy, res.Config.LandMethod, res.Config.MaxCatchUps)
	if res.Config.UpdatedAt != "" {
		fmt.Fprintf(w, " · updated %s by %s", res.Config.UpdatedAt, res.Config.UpdatedBy)
	}
	fmt.Fprintln(w)
	states := make([]string, 0, len(res.Counts))
	for s := range res.Counts {
		states = append(states, s)
	}
	sort.Strings(states)
	counts := make([]string, 0, len(states))
	for _, s := range states {
		counts = append(counts, fmt.Sprintf("%s=%d", s, res.Counts[s]))
	}
	fmt.Fprintf(w, "counts  %s\n", strings.Join(counts, " "))
	for _, l := range res.Lanes {
		fmt.Fprintf(w, "lane    %s head %s\n", l.Base, l.HeadItem)
	}
	renderMergeQueueItems(w, res.Items)
	return nil
}

func mqVerbVocabulary(w io.Writer, args []string) error {
	want := mqWantFromArgs(args)
	c, err := mqDial(want)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	fs := flag.NewFlagSet("mergequeue vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the words")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *asJSON {
		return callJSON(w, c, protocol.MethodMergeQueueVocabulary, struct{}{})
	}
	v, err := mqVocabulary(context.Background(), c)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "states (each with its meaning and the moves the driver's legal table carries):")
	for _, s := range v.States {
		fmt.Fprintf(w, "  %-14s %s\n", s.Name, s.Meaning)
		if len(s.LegalNext) > 0 {
			fmt.Fprintf(w, "  %-14s → %s\n", "", strings.Join(s.LegalNext, ", "))
		}
	}
	fmt.Fprintf(w, "reasons            %s\n", strings.Join(v.Reasons, ", "))
	fmt.Fprintf(w, "modes              %s\n", strings.Join(v.Modes, ", "))
	fmt.Fprintf(w, "conflict policies  %s\n", strings.Join(v.ConflictPolicies, ", "))
	fmt.Fprintf(w, "land methods       %s\n", strings.Join(v.LandMethods, ", "))
	fmt.Fprintf(w, "merge actors       %s\n", strings.Join(v.MergeActors, ", "))
	fmt.Fprintln(w, "methods (read doors need no identity; human doors act as the person the connection resolved):")
	for _, m := range v.Methods {
		fmt.Fprintf(w, "  %-24s %s\n", m.Name, m.Door)
	}
	return nil
}

// --- the person's writes ---------------------------------------------------------

func mqVerbEnqueue(w io.Writer, args []string) error {
	want := mqWantFromArgs(args)
	c, err := mqDial(want)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	fs := flag.NewFlagSet("mergequeue enqueue", flag.ContinueOnError)
	fs.SetOutput(w)
	repo := mqRepoFlag(fs, mqCwd())
	branch := fs.String("branch", "", "the branch to queue (required; it must exist locally or on origin)")
	base := fs.String("base", "", "the base branch (default: the repository's own default branch, resolved the way the queue resolves it)")
	unattended := fs.Bool("unattended", false, "enqueue UNATTENDED: no watching lane (attendedness is declared at this door, G14 — the default is attended)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the row")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*branch) == "" {
		return &usageError{msg: "`aeon mergequeue enqueue` needs --branch — the queue queues BRANCHES"}
	}

	// The caller's git facts (the door verifies them against the repository
	// before the row lands — gathering is not validating, and the daemon
	// holds every veto). The base defaults from the repository's own origin
	// HEAD, the same source the queue resolves a proposal's base from.
	ctx := context.Background()
	gitC := git.NewClient(execenv.GitEnv(execenv.DaemonEnv{Lookup: os.Getenv}, execenv.GitEnvDaemon))
	headSHA := ""
	if out, rErr := gitC.Run(ctx, *repo, "rev-parse", *branch); rErr == nil {
		headSHA = strings.TrimSpace(out)
	}
	if headSHA == "" {
		if out, rErr := gitC.Run(ctx, *repo, "ls-remote", "--heads", "origin", *branch); rErr == nil {
			if line := strings.TrimSpace(out); line != "" {
				headSHA = strings.Fields(line)[0]
			}
		}
	}
	if headSHA == "" {
		return fmt.Errorf("mergequeue enqueue: cannot read the head of branch %q from %s — it must exist locally or on origin (the daemon would refuse it, and it refuses with better words)", *branch, *repo)
	}
	resolvedBase := strings.TrimSpace(*base)
	if resolvedBase == "" {
		resolvedBase = workspace.OriginDefaultBranch(ctx, *repo)
	}
	if resolvedBase == "" {
		return fmt.Errorf("mergequeue enqueue: no base branch is known for %s — pass --base (the daemon would refuse a base it cannot resolve, and it refuses with better words)", *repo)
	}
	baseSHA := ""
	if out, rErr := gitC.Run(ctx, *repo, "rev-parse", "refs/remotes/origin/"+resolvedBase); rErr == nil {
		baseSHA = strings.TrimSpace(out)
	}

	p := protocol.MergeQueueEnqueueParams{
		ComponentRoot: *repo, BaseBranch: resolvedBase, FeatureBranch: *branch,
		HeadSha: headSHA, BaseSha: baseSHA, Attended: !*unattended,
	}
	if *asJSON {
		return callJSON(w, c, protocol.MethodMergeQueueEnqueue, p)
	}
	var item protocol.ItemView
	if err := c.Call(protocol.MethodMergeQueueEnqueue, p, &item); err != nil {
		return err
	}
	renderMergeQueueItem(w, item)
	fmt.Fprintf(w, "  next step %s\n", item.NextStep)
	return nil
}

func mqVerbID(w io.Writer, args []string, verb string, method protocol.MethodName) error {
	want := mqWantFromArgs(args)
	c, err := mqDial(want)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	positional, rest := splitPositional(args, 1)
	fs := flag.NewFlagSet("mergequeue "+verb, flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the row")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return &usageError{msg: fmt.Sprintf("`aeon mergequeue %s` needs exactly one item id", verb)}
	}
	p := protocol.MergeQueueIDParams{ID: positional[0]}
	if *asJSON {
		return callJSON(w, c, method, p)
	}
	var item protocol.ItemView
	if err := c.Call(method, p, &item); err != nil {
		return err
	}
	renderMergeQueueItem(w, item)
	fmt.Fprintf(w, "  next step %s\n", item.NextStep)
	return nil
}

func mqVerbReorder(w io.Writer, args []string) error {
	want := mqWantFromArgs(args)
	c, err := mqDial(want)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	positional, rest := splitPositional(args, 1)
	fs := flag.NewFlagSet("mergequeue reorder", flag.ContinueOnError)
	fs.SetOutput(w)
	before := fs.String("before", "", "the item the row moves in FRONT of (omitted: last in the queue)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the row")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return &usageError{msg: "`aeon mergequeue reorder` needs exactly one item id — position is moved by --before, never typed"}
	}
	p := protocol.MergeQueueReorderParams{ID: positional[0], BeforeID: *before}
	if *asJSON {
		return callJSON(w, c, protocol.MethodMergeQueueReorder, p)
	}
	var item protocol.ItemView
	if err := c.Call(protocol.MethodMergeQueueReorder, p, &item); err != nil {
		return err
	}
	renderMergeQueueItem(w, item)
	fmt.Fprintf(w, "  next step %s\n", item.NextStep)
	return nil
}

// mqVerbLand is the digest-bound seatbelt: SHOW first (the row the person
// is approving, printed with the driver's own nextStep), then — only under
// --yes — the land carrying that answer's digest. A moved item then refuses
// by digest and the daemon says so; this file never guesses a digest and
// never lands on a hope.
func mqVerbLand(w io.Writer, args []string) error {
	want := mqWantFromArgs(args)
	c, err := mqDial(want)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	positional, rest := splitPositional(args, 1)
	fs := flag.NewFlagSet("mergequeue land", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm the land — the digest of the row just shown rides the call")
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the row")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return &usageError{msg: "`aeon mergequeue land` needs exactly one item id"}
	}
	var show protocol.ShowResult
	if err := c.Call(protocol.MethodMergeQueueShow, protocol.MergeQueueShowParams{ID: positional[0]}, &show); err != nil {
		return err
	}
	if !*yes {
		renderMergeQueueItem(w, show.Item)
		fmt.Fprintf(w, "  next step %s\n", show.Item.NextStep)
		return &usageError{msg: "re-run with --yes to land — the land is bound to the head shown, and nothing was landed"}
	}
	p := protocol.MergeQueueLandParams{ID: positional[0], Digest: show.Item.Digest}
	if *asJSON {
		return callJSON(w, c, protocol.MethodMergeQueueLand, p)
	}
	var item protocol.ItemView
	if err := c.Call(protocol.MethodMergeQueueLand, p, &item); err != nil {
		return err
	}
	renderMergeQueueItem(w, item)
	fmt.Fprintf(w, "  next step %s\n", item.NextStep)
	return nil
}

// mqAckDigestRx finds the digest the settings refusal quotes in its own
// sentence ("Resend with acknowledged \"HEX\" …") — the daemon's words are
// the transport, so no second implementation of the digest exists here.
var mqAckDigestRx = regexp.MustCompile(`acknowledged "([0-9a-f]{64})"`)

func mqVerbSettings(w io.Writer, args []string) error {
	want := mqWantFromArgs(args)
	c, err := mqDial(want)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	// The live vocabulary decorates the three set-valued flags' help —
	// behaviour 1, and TestHelpSetsComeFromVocabulary's whole point: the
	// daemon's tables are the only list this CLI can speak.
	vocab, verr := mqVocabulary(context.Background(), c)
	modeHelp, policyHelp, methodHelp := mergeQueueVocabularyDown, mergeQueueVocabularyDown, mergeQueueVocabularyDown
	if verr == nil {
		modeHelp = "the queue's mode (the plane's modes: " + strings.Join(vocab.Modes, ", ") + ")"
		policyHelp = "what the resolution loop may do at a conflict (the plane's policies: " + strings.Join(vocab.ConflictPolicies, ", ") + ")"
		methodHelp = "how a landing merges (the plane's methods: " + strings.Join(vocab.LandMethods, ", ") + ")"
	}

	fs := flag.NewFlagSet("mergequeue settings", flag.ContinueOnError)
	fs.SetOutput(w)
	repo := mqRepoFlag(fs, mqCwd())
	mode := fs.String("mode", "", modeHelp)
	policy := fs.String("conflict-policy", "", policyHelp)
	method := fs.String("land-method", "", methodHelp)
	maxCatchUps := fs.Int("max-catch-ups", -1, "how many times the driver may rebase an item under a moving base (a number, 0 included)")
	yes := fs.Bool("yes", false, "apply even an AUTHORITY-WIDENING change (mode auto, policy resolve) — the daemon's statement prints first, its digest rides the apply")
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the settings")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	p := protocol.MergeQueueSettingsParams{ComponentRoot: *repo, Mode: *mode, ConflictPolicy: *policy, LandMethod: *method}
	if fs.Lookup("max-catch-ups").Value.String() != "-1" {
		n := *maxCatchUps
		p.MaxCatchUps = &n
	}
	if verr == nil {
		// Membership against the live sets — usage words, not re-validation
		// of truth: the door holds the veto and answers with its own sentence
		// whatever this check lets through.
		if *mode != "" && len(vocab.Modes) > 0 && !mqMember(*mode, vocab.Modes) {
			return &usageError{msg: fmt.Sprintf("`mergequeue settings --mode %s` names no mode — the plane speaks %s", *mode, strings.Join(vocab.Modes, ", "))}
		}
		if *policy != "" && len(vocab.ConflictPolicies) > 0 && !mqMember(*policy, vocab.ConflictPolicies) {
			return &usageError{msg: fmt.Sprintf("`mergequeue settings --conflict-policy %s` names no policy — the plane speaks %s", *policy, strings.Join(vocab.ConflictPolicies, ", "))}
		}
		if *method != "" && len(vocab.LandMethods) > 0 && !mqMember(*method, vocab.LandMethods) {
			return &usageError{msg: fmt.Sprintf("`mergequeue settings --land-method %s` names no method — the plane speaks %s", *method, strings.Join(vocab.LandMethods, ", "))}
		}
	}

	call := func(acknowledged string) (protocol.SettingsResult, error) {
		p.Acknowledged = acknowledged
		var res protocol.SettingsResult
		err := c.Call(protocol.MethodMergeQueueSettings, p, &res)
		return res, err
	}
	res, err := call("")
	if err != nil {
		// The widening refusal answers with the daemon's statement and the
		// digest quoted in its own sentence. The statement prints either
		// way; only --yes echoes the digest back, and declining sends
		// nothing further.
		var e *protocol.Error
		if errors.As(err, &e) {
			sentence := e.Data.What
			if m := mqAckDigestRx.FindStringSubmatch(sentence); m != nil {
				fmt.Fprintln(w, sentence)
				if !*yes {
					return &usageError{msg: "re-run with --yes to apply — nothing was written"}
				}
				res, err = call(m[1])
				if err != nil {
					return err
				}
			} else {
				return err
			}
		} else {
			return err
		}
	}
	if *asJSON {
		raw, mErr := json.Marshal(res)
		if mErr != nil {
			return mErr
		}
		fmt.Fprintln(w, string(raw))
		return nil
	}
	renderMergeQueueConfig(w, res.Config)
	if res.Statement != "" {
		fmt.Fprintf(w, "  statement %s\n", res.Statement)
	}
	return nil
}

// --- the printers (one each; the bytes never depend on anything but the wire answer)

func renderMergeQueueItems(w io.Writer, items []protocol.ItemView) {
	fmt.Fprintf(w, "%-6s %-10s %-24s %-32s %-12s %-6s %-24s\n", "POSITION", "STATUS", "BRANCH", "REASON", "BEHIND/AHEAD", "PR", "LOOP")
	for _, it := range items {
		reason := it.Reason
		if len(reason) > 30 {
			reason = reason[:29] + "…"
		}
		fmt.Fprintf(w, "%-6d %-10s %-24s %-32s %-12s %-6s %-24s\n",
			it.Position, it.Status, it.Branch, reason, mqStalenessCell(it.Staleness), mqPRCell(it.PrNumber), mqDash(it.Loop))
	}
}

func mqStalenessCell(s protocol.StalenessView) string {
	if s.Behind != nil || s.Ahead != nil {
		b, a := int64(0), int64(0)
		if s.Behind != nil {
			b = *s.Behind
		}
		if s.Ahead != nil {
			a = *s.Ahead
		}
		return fmt.Sprintf("%d/%d", b, a)
	}
	if s.Reason != "" {
		return s.State + "?"
	}
	return s.State
}

func mqPRCell(pr *int) string {
	if pr == nil {
		return "-"
	}
	return fmt.Sprintf("#%d", *pr)
}

func mqDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func renderMergeQueueItem(w io.Writer, it protocol.ItemView) {
	fmt.Fprintf(w, "item    %s (%s)\n", it.ID, it.ComponentRoot)
	fmt.Fprintf(w, "        status %s · position %d · %s\n", it.Status, it.Position, func() string {
		if it.Attended {
			return "attended"
		}
		return "unattended"
	}())
	fmt.Fprintf(w, "        branch %s onto %s · head %s · base %s\n", it.Branch, it.Base, it.HeadSha, mqDash(it.BaseSha))
	fmt.Fprintf(w, "        enqueued by %s/%s · %s\n", it.EnqueuedBy.Kind, it.EnqueuedBy.ID, mqStalenessLine(it.Staleness))
	if it.Loop != "" || it.LandingLoop != "" || it.ResolveLoop != "" {
		fmt.Fprintf(w, "        loops: queue %s · landing %s · resolution %s\n", mqDash(it.Loop), mqDash(it.LandingLoop), mqDash(it.ResolveLoop))
	}
	if it.IntegrationBranch != "" {
		fmt.Fprintf(w, "        integration %s @ %s · catch-ups %d\n", it.IntegrationBranch, mqDash(it.IntegrationSha), it.CatchUps)
	}
	if it.PrNumber != nil {
		fmt.Fprintf(w, "        pull request #%d\n", *it.PrNumber)
	}
	if it.Reason != "" {
		fmt.Fprintf(w, "        reason %s\n", it.Reason)
	}
}

func mqStalenessLine(s protocol.StalenessView) string {
	if s.Behind != nil || s.Ahead != nil {
		b, a := int64(0), int64(0)
		if s.Behind != nil {
			b = *s.Behind
		}
		if s.Ahead != nil {
			a = *s.Ahead
		}
		return fmt.Sprintf("staleness %s (behind %d, ahead %d)", s.State, b, a)
	}
	if s.Reason != "" {
		return fmt.Sprintf("staleness %s (%s)", s.State, s.Reason)
	}
	return "staleness " + s.State
}

func renderMergeQueueConfig(w io.Writer, cfg protocol.ConfigView) {
	word := "settings"
	if cfg.Absent {
		word = "settings (never written — the defaults stand)"
	}
	fmt.Fprintf(w, "%s for %s\n  mode %s · conflict policy %s · land method %s · max catch-ups %d",
		word, cfg.ComponentRoot, cfg.Mode, cfg.ConflictPolicy, cfg.LandMethod, cfg.MaxCatchUps)
	if cfg.UpdatedAt != "" {
		fmt.Fprintf(w, " · updated %s by %s", cfg.UpdatedAt, cfg.UpdatedBy)
	}
	fmt.Fprintln(w)
}

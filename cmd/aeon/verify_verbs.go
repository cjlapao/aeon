package main

// verify_verbs.go — S57 T10 (VG20): `aeon verify` — the gate plane's
// person's door at the terminal. Twelve verbs, each a client of its
// socket method (socketDoor) or the same daemon service over the store
// (directDoor); the door is chosen per verb by selectGateDoor.
//
// The CLI re-validates NOTHING: closed sets are the daemon's (or the
// store's) and their refusals print verbatim through renderError; the
// only refusals spoken here are flag-shape refusals (`--timeout abc`
// cannot be a value) and usage. `--json` prints the wire result
// verbatim, stdout, nothing else.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// gateTailsSecretLine rides every --show-output print (pinned by test):
// the redaction is best-effort (redact.go says its own limit), so a tail
// that looks clean may still carry a secret with no shape.
const gateTailsSecretLine = "the tails are redacted best-effort and may contain secrets — read them as any command output"

// cmdVerify dispatches `aeon verify …` (T0 §4's twelve rows).
func cmdVerify(w io.Writer, args []string) error {
	if len(args) == 0 {
		return &usageError{msg: "aeon verify: a verb is required — " + verifyVerbLine()}
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "list":
		return runVerifyList(w, rest)
	case "show":
		return runVerifyShow(w, rest)
	case "add":
		return runVerifyAdd(w, rest)
	case "update":
		return runVerifyUpdate(w, rest)
	case "archive":
		return runVerifyArchive(w, rest)
	case "restore":
		return runVerifyRestore(w, rest)
	case "apply-template":
		return runVerifyApplyTemplate(w, rest)
	case "try":
		return runVerifyTry(w, rest)
	case "runs":
		return runVerifyRuns(w, rest)
	case "run-show":
		return runVerifyRunShow(w, rest)
	case "vocabulary":
		return runVerifyVocabulary(w, rest)
	case "templates":
		return runVerifyTemplates(w, rest)
	case "help", "-h", "--help":
		printVerifyUsage(w)
		return nil
	default:
		return &usageError{msg: fmt.Sprintf("aeon verify: unknown verb %q — "+verifyVerbLine(), verb)}
	}
}

// verifyVerbLine is the verb list from the ONE table this file's switch
// and the usage text share (no set typed twice).
func verifyVerbLine() string {
	return "list · show · add · update · archive · restore · apply-template · try · runs · run-show · vocabulary · templates"
}

func printVerifyUsage(w io.Writer) {
	fmt.Fprintf(w, `aeon verify — the component gates (S57)

Verbs: %s

  list [--component C] [--effective] [--archived] [--json]
  show ID [--json]
  add --component C --name N --command CMD --requirement %s [--timeout S] [--memory MB] [--yes] [--json]
  update ID [--command CMD] [--requirement %s] [--timeout S] [--memory MB] [--position N] [--enabled=true|false] [--json]
  archive ID --yes [--json]        restore ID [--json]
  apply-template --component C --template T[,T…] [--names a,b] [--json]
  try ID [--json]                    (daemon required — offline it refuses)
  runs [--component C] [--loop L] [--state %s] [--limit N] [--json]
  run-show ID [--show-output] [--json]
  vocabulary [--json]                templates [--json]

The closed sets above (%s / %s) are the daemon's vocabulary;
this usage names them offline from the same Go constants.
`, verifyVerbLine(),
		strings.Join(protocol.VerifyRequirementValues, " | "),
		strings.Join(protocol.VerifyRequirementValues, " | "),
		strings.Join(protocol.VerifyStateValues, " | "),
		"--requirement", "--state")
}

// printGateNote prints the daemon's scrubbed-environment note verbatim
// (VG20): the daemon decided to say it (Note non-empty on the write
// result); the CLI never re-reads the command to decide.
func printGateNote(res protocol.VerifyWriteResult) {
	if res.Note != "" {
		fmt.Fprintln(stderrOut(), res.Note)
	}
}

func runVerifyList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("verify list", flag.ContinueOnError)
	fs.SetOutput(w)
	component := fs.String("component", "", "the component's gates (empty: the whole plane)")
	effective := fs.Bool("effective", false, "include inherited gates from the component's root")
	archived := fs.Bool("archived", false, "include archived gates")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.VerifyList(context.Background(), protocol.VerifyListQuery{
		ComponentID: *component, Effective: *effective, IncludeArchived: *archived,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	if res.ComponentID != "" {
		fmt.Fprintf(w, "gates for %s", res.ComponentID)
		if res.Effective {
			fmt.Fprint(w, " (effective: own and inherited)")
		}
		fmt.Fprintln(w)
	}
	if len(res.Gates) == 0 {
		fmt.Fprintln(w, "  (none)")
		return nil
	}
	for _, g := range res.Gates {
		line := fmt.Sprintf("  %s  %s  %s", pad(g.ID, 14), pad(g.Requirement, 9), pad(g.Name, 18))
		if g.ArchivedAt != "" {
			line += "  [archived]"
		} else if !g.Enabled {
			line += "  [disabled]"
		}
		if g.Source != "" && g.Source != "own" {
			line += fmt.Sprintf("  [from %s]", g.SourceComponent)
		}
		fmt.Fprintf(w, "%s  %s\n", line, g.Command)
	}
	return nil
}

func runVerifyShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("verify show", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return &usageError{msg: "aeon verify show: one argument is required — the gate's id"}
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.VerifyShow(context.Background(), positional[0])
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	g := res.Gate
	fmt.Fprintf(w, "gate   %s\n  component   %s\n  name        %s\n  command     %s\n  requirement %s   timeout %ds",
		g.ID, g.ComponentID, g.Name, g.Command, g.Requirement, g.TimeoutS)
	if g.MemoryMB != nil {
		fmt.Fprintf(w, "   memory %dMB", *g.MemoryMB)
	}
	fmt.Fprintf(w, "\n  position    %d   enabled %v   origin %s", g.Position, g.Enabled, g.Origin)
	if g.TemplateID != "" {
		fmt.Fprintf(w, "   from template %s", g.TemplateID)
	}
	fmt.Fprintln(w)
	if g.LastRun != nil {
		fmt.Fprintf(w, "last run %s: %s", g.LastRun.ID, g.LastRun.State)
		if g.LastRun.Reason != "" {
			fmt.Fprintf(w, " (%s)", g.LastRun.Reason)
		}
		fmt.Fprintln(w)
	}
	if g.PolicyPreview != "" {
		fmt.Fprintf(w, "policy   %s\n", g.PolicyPreview)
	}
	if len(res.Events) > 0 {
		fmt.Fprintln(w, "history:")
		for _, e := range res.Events {
			fmt.Fprintf(w, "  r%d  %s  %s/%s  %s\n", e.Revision, e.Op, e.ActorKind, e.ActorID, e.Summary)
		}
	}
	return nil
}

// trackedInt is an int flag that knows whether the person typed it.
// stdlib flag does not expose "was set", and for update the difference
// between un-typed and `--timeout 0` IS the request (un-typed means
// leave the field alone; 0 goes to the daemon to refuse verbatim).
type trackedInt struct {
	v   int
	set bool
}

func (t *trackedInt) String() string {
	if t == nil {
		return "0"
	}
	return strconv.Itoa(t.v)
}

func (t *trackedInt) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		// The flag package's shape refusal (not-an-int) exits 2 —
		// "cannot be a value", the CLI's only numeric refusal.
		return err
	}
	t.v = n
	t.set = true
	return nil
}

func runVerifyAdd(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("verify add", flag.ContinueOnError)
	fs.SetOutput(w)
	component := fs.String("component", "", "the component the gate guards (required)")
	name := fs.String("name", "", "the gate's name on the component (required)")
	command := fs.String("command", "", "the command text (required)")
	requirement := fs.String("requirement", "", "required | advisory (required)")
	timeoutS := &trackedInt{}
	fs.Var(timeoutS, "timeout", "timeout in seconds (the daemon's bound applies)")
	memoryMB := &trackedInt{}
	fs.Var(memoryMB, "memory", "memory cap in MB")
	yes := fs.Bool("yes", false, "acknowledgement; accepted and inert — the daemon's refusals and notes need no client permission")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = yes // the seatbelt is the NOTE, and the daemon stamps it; see T10 Notes on --yes.
	if fs.NArg() > 0 {
		return &usageError{msg: fmt.Sprintf("aeon verify add: %q is not a verb or flag", fs.Arg(0))}
	}
	if *component == "" || *name == "" || *command == "" || *requirement == "" {
		return &usageError{msg: "aeon verify add: --component, --name, --command and --requirement are all required"}
	}
	in := protocol.VerifyAddInput{
		ComponentID: *component, Name: *name, Command: *command, Requirement: *requirement,
		TimeoutS: timeoutS.v,
	}
	if memoryMB.set {
		in.MemoryMB = &memoryMB.v
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.VerifyAdd(context.Background(), in)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "gate %s added on %s: %s (%s)\n", res.Gate.ID, res.Gate.ComponentID, res.Gate.Command, res.Gate.Requirement)
	printGateNote(res)
	return nil
}

func runVerifyUpdate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("verify update", flag.ContinueOnError)
	fs.SetOutput(w)
	command := fs.String("command", "", "replace the command text")
	requirement := fs.String("requirement", "", "required | advisory")
	timeoutS := &trackedInt{}
	fs.Var(timeoutS, "timeout", "timeout in seconds")
	memoryMB := &trackedInt{}
	fs.Var(memoryMB, "memory", "memory cap in MB")
	position := &trackedInt{}
	fs.Var(position, "position", "run order")
	enabled := fs.String("enabled", "", "true | false")
	yes := fs.Bool("yes", false, "acknowledgement; accepted and inert (see add)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	_ = yes
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return &usageError{msg: "aeon verify update: one argument is required — the gate's id"}
	}
	in := protocol.VerifyUpdateInput{ID: positional[0]}
	if *command != "" {
		in.Command = command
	}
	if *requirement != "" {
		in.Requirement = requirement
	}
	if timeoutS.set {
		in.TimeoutS = &timeoutS.v
	}
	if memoryMB.set {
		in.MemoryMB = &memoryMB.v
	}
	if position.set {
		in.Position = &position.v
	}
	if *enabled != "" {
		b, err := strconv.ParseBool(*enabled)
		if err != nil {
			return &usageError{msg: fmt.Sprintf("aeon verify update: --enabled takes true or false, not %q", *enabled)}
		}
		in.Enabled = &b
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.VerifyUpdate(context.Background(), in)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "gate %s updated: %s (%s)\n", res.Gate.ID, res.Gate.Command, res.Gate.Requirement)
	printGateNote(res)
	return nil
}

func runVerifyArchive(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("verify archive", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm: archiving stops the gate running for every loop")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return &usageError{msg: "aeon verify archive: one argument is required — the gate's id"}
	}
	if !*yes {
		return &usageError{msg: "aeon verify archive: --yes is required — archiving stops the gate running for every loop (restore turns it back on)"}
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.VerifyArchive(context.Background(), positional[0])
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "gate %s archived: %s\n", res.Gate.ID, res.Gate.Command)
	printGateNote(res)
	return nil
}

func runVerifyRestore(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("verify restore", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return &usageError{msg: "aeon verify restore: one argument is required — the gate's id"}
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.VerifyRestore(context.Background(), positional[0])
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "gate %s restored: %s\n", res.Gate.ID, res.Gate.Command)
	return nil
}

func runVerifyApplyTemplate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("verify apply-template", flag.ContinueOnError)
	fs.SetOutput(w)
	component := fs.String("component", "", "the component to apply to (required)")
	tmpl := fs.String("template", "", "template id(s) or stack(s), comma-separated (required)")
	names := fs.String("names", "", "copy only these gate names (pruning at pick time)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *component == "" || *tmpl == "" {
		return &usageError{msg: "aeon verify apply-template: --component and --template are both required"}
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	// The picks come from the daemon's own library read: an exact id
	// passes through, a stack name ("go") expands to that stack's live
	// template ids, and --names filters by the row's name. Nothing is
	// invented here — a name the library does not carry is refused below
	// by name, from the library's own list.
	libs, err := door.VerifyTemplates(context.Background())
	if err != nil {
		return err
	}
	ids, err := expandTemplatePicks(libs, splitCSV(*tmpl), splitCSV(*names))
	if err != nil {
		return err
	}
	res, err := door.VerifyApplyTemplate(context.Background(), *component, ids)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	for _, g := range res.Gates {
		fmt.Fprintf(w, "gate %s on %s: %s (%s)\n", g.ID, g.ComponentID, g.Command, g.Requirement)
	}
	fmt.Fprintf(w, "%d gate(s) copied — a template is data: prune with verify archive, edit is by copy\n", len(res.Gates))
	return nil
}

// expandTemplatePicks resolves --template tokens (ids or stack names)
// and the --names filter against the daemon's library listing.
func expandTemplatePicks(libs protocol.VerifyTemplatesResult, tokens, want []string) ([]string, error) {
	if len(libs.Templates) == 0 {
		return nil, &usageError{msg: "aeon verify apply-template: the template library is empty offline — aeon seed apply verify.templates fills the system rows"}
	}
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, tok := range tokens {
		var matched []string
		for _, t := range libs.Templates {
			exact := t.ID == tok
			stack := t.Stack == tok
			if !exact && !stack {
				continue
			}
			if len(wantSet) > 0 && !wantSet[t.Name] {
				continue
			}
			matched = append(matched, t.ID)
		}
		if len(matched) == 0 {
			return nil, &usageError{msg: fmt.Sprintf("aeon verify apply-template: %q picks no template — aeon verify templates lists the library", tok)}
		}
		for _, id := range matched {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	for _, w := range want {
		found := false
		for _, t := range libs.Templates {
			if t.Name == w {
				found = true
			}
		}
		if !found {
			return nil, &usageError{msg: fmt.Sprintf("aeon verify apply-template: --names %q matches no template name in the library", w)}
		}
	}
	if len(out) == 0 {
		return nil, &usageError{msg: "aeon verify apply-template: the picks resolved to nothing"}
	}
	return out, nil
}

func runVerifyTry(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("verify try", flag.ContinueOnError)
	fs.SetOutput(w)
	showOutput := fs.Bool("show-output", false, "print the redacted tails (they may contain secrets)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return &usageError{msg: "aeon verify try: one argument is required — the gate's id"}
	}
	// `try` is the one verb with NO direct door (it spawns a process
	// under the daemon's policy gate — offline there is no gate to run
	// under, so there is no store-opening fallback and no direct-door
	// line). selectGateTryDoor answers the fixed refusal before any
	// store is opened.
	door, err := gateSelectTry()
	if err != nil {
		return err
	}
	defer door.Close()
	gate, err := door.VerifyShow(context.Background(), positional[0])
	if err != nil {
		return err
	}
	res, err := door.VerifyTry(context.Background(), gate.Gate.ComponentID, positional[0])
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "run %s: %s (%s)\n  ran in: %s\n", res.Run.ID, res.Run.State, first(res.Run.Reason, "no reason given"), res.RanIn)
	if len(res.ActiveLoops) > 0 {
		fmt.Fprintf(w, "  active loops in that root: %s — this try may race their tree (OD8)\n", strings.Join(res.ActiveLoops, ", "))
	}
	printRunResults(w, res.Results, *showOutput)
	return nil
}

func runVerifyRuns(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("verify runs", flag.ContinueOnError)
	fs.SetOutput(w)
	component := fs.String("component", "", "filter to one component")
	loop := fs.String("loop", "", "filter to one loop")
	state := fs.String("state", "", "filter: pass | fail | unverified (the daemon's vocabulary)")
	limit := fs.Int("limit", 0, "most recent N (daemon default applies)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.VerifyRuns(context.Background(), protocol.VerifyRunsQuery{
		ComponentID: *component, LoopSlug: *loop, State: *state, Limit: *limit,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	if len(res.Runs) == 0 {
		fmt.Fprintln(w, "(no runs match)")
		return nil
	}
	for _, r := range res.Runs {
		fmt.Fprintf(w, "  %s  %s  %s  %s  %s\n", pad(r.ID, 14), pad(r.State, 11), pad(first(r.ComponentID, "-"), 20), first(r.Reason, ""), r.StartedAt)
	}
	return nil
}

func runVerifyRunShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("verify run-show", flag.ContinueOnError)
	fs.SetOutput(w)
	showOutput := fs.Bool("show-output", false, "print the redacted tails (they may contain secrets)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return &usageError{msg: "aeon verify run-show: one argument is required — the run's id"}
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.VerifyRunShow(context.Background(), positional[0])
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	r := res.Run
	fmt.Fprintf(w, "run %s: %s\n  component %s   loop %s   trigger %s (%s/%s)\n  head %s   started %s\n",
		r.ID, r.State, first(r.ComponentID, "-"), first(r.LoopSlug, "-"), r.Trigger, r.ActorKind, r.ActorID, r.HeadSHA, r.StartedAt)
	if r.Reason != "" {
		fmt.Fprintf(w, "  reason: %s\n", r.Reason)
	}
	printRunResults(w, res.Results, *showOutput)
	return nil
}

// printRunResults prints each gate result one line, and the tails only
// past --show-output (never by default — a tail is command output, and
// command output may carry a secret the redactor has no shape for).
func printRunResults(w io.Writer, results []protocol.VerifyRunResultView, showOutput bool) {
	for _, r := range results {
		state := r.State
		exit := ""
		if r.ExitCode != nil {
			exit = fmt.Sprintf(" exit %d", *r.ExitCode)
		}
		if r.TimedOut {
			exit += " [timed out]"
		}
		fmt.Fprintf(w, "  %d  %s  %s%s  %dms\n", r.Seq, pad(r.Name, 18), pad(state, 11), exit, r.WallMS)
		if r.Reason != "" {
			fmt.Fprintf(w, "     %s\n", r.Reason)
		}
		if !showOutput {
			continue
		}
		fmt.Fprintln(w, "     "+gateTailsSecretLine)
		if r.StdoutTail != "" {
			fmt.Fprintf(w, "     stdout:\n")
			for _, l := range strings.Split(r.StdoutTail, "\n") {
				fmt.Fprintf(w, "       %s\n", l)
			}
		}
		if r.StderrTail != "" {
			fmt.Fprintf(w, "     stderr:\n")
			for _, l := range strings.Split(r.StderrTail, "\n") {
				fmt.Fprintf(w, "       %s\n", l)
			}
		}
	}
}

func runVerifyVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("verify vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.VerifyVocabulary(context.Background())
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "requirements  %s\n", strings.Join(res.Requirements, " | "))
	fmt.Fprintf(w, "run states    %s\n", strings.Join(res.States, " | "))
	fmt.Fprintf(w, "run reasons   %s\n", strings.Join(res.Reasons, " | "))
	fmt.Fprintf(w, "gate env      %s\n", strings.Join(res.EnvAllowlist, ", "))
	fmt.Fprintf(w, "try           %s\n", res.TryStatement)
	return nil
}

func runVerifyTemplates(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("verify templates", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.VerifyTemplates(context.Background())
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	for _, t := range res.Templates {
		fmt.Fprintf(w, "  %s  %s  %s  %ds  %s\n", pad(t.ID, 20), pad(t.Requirement, 9), pad(t.Origin, 7), t.TimeoutS, t.Command)
	}
	if len(res.Templates) == 0 {
		fmt.Fprintln(w, "(the library is empty — aeon seed apply verify.templates fills the system rows)")
	}
	return nil
}

// --- small helpers shared by the four gate verb files -------------------

// stderrOut is the stderr writer seam (a test swaps it to pin the fixed
// lines; the direct-door line and the note both land here).
func stderrOut() io.Writer { return stderrW }

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

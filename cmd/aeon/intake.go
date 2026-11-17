package main

// cmd/aeon/intake.go — S61 T10: the intake plane as a pure client, the
// same law as charter.go (dial, call, print; no offline door — a loop, a
// conversation and a filesystem effect do not exist without the daemon,
// and `intake.commit` must not run in a second process; the recorded P5
// deviation). Refusals pass through untouched: the daemon's sentence is
// the output, the CLI adds no sentence of its own (rule 4).
//
// The `commit` flow is the spec's own ordering: the verb calls `intake.plan`
// FIRST and prints the daemon's statement and file list, because the
// person is about to watch files land; `--digest` is required — its
// absence is a refusal pointing at `plan`, not a silent plan-of-the-moment
// (GI12: the digest pins what was shown to what is filed). `--yes` is the
// seatbelt where S45 uses one: required for `--publish private|public`.
// `--publish` values come from the daemon's OWN vocabulary for the human
// usage line — generated, never typed twice.

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// intakeVerbs names each verb and the socket method it drives. The
// `propose` twins are the agent's tools, not the person's verbs — the
// plane table (T0's census, fifteen doors) is what parity walks.
var intakeVerbs = []cliVerb{
	{"start", protocol.MethodIntakeStart, false},
	{"list", protocol.MethodIntakeList, false},
	{"status", protocol.MethodIntakeStatus, false},
	{"plan", protocol.MethodIntakePlan, false},
	{"commit", protocol.MethodIntakeCommit, false},
	{"abandon", protocol.MethodIntakeAbandon, false},
	{"vocabulary", protocol.MethodIntakeVocabulary, false},
}

// intakeDial is the dial every verb shares; a var for the tests.
var intakeDial = dial

func cmdIntake(w io.Writer, args []string) error {
	if len(args) == 0 {
		printIntakeUsage(w)
		return nil
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "start":
		return intakeVerbStart(w, rest)
	case "list":
		return intakeVerbList(w, rest)
	case "status":
		return intakeVerbStatus(w, rest)
	case "plan":
		return intakeVerbPlan(w, rest)
	case "commit":
		return intakeVerbCommit(w, rest)
	case "abandon":
		return intakeVerbAbandon(w, rest)
	case "vocabulary":
		return intakeVerbVocabulary(w, rest)
	case "help":
		printIntakeUsage(w)
		return nil
	default:
		printIntakeUsage(w)
		return &usageError{msg: fmt.Sprintf("unknown `intake` subcommand %q — `aeon intake help` lists the seven", verb)}
	}
}

// intakeVocabularyProbe is the usage probe: one dial, one
// intake.vocabulary. A cold daemon is an answer, not a bug — the line
// then says the values are the daemon's to publish.
func intakeVocabularyProbe() (protocol.IntakeVocabularyResult, error) {
	c, err := intakeDial(0)
	if err != nil {
		return protocol.IntakeVocabularyResult{}, err
	}
	defer c.Close()
	var res protocol.IntakeVocabularyResult
	if err := c.Call(protocol.MethodIntakeVocabulary, struct{}{}, &res); err != nil {
		return protocol.IntakeVocabularyResult{}, err
	}
	return res, nil
}

func printIntakeUsage(w io.Writer) {
	publishLine := "              (the closed publish set is the daemon's to publish — start it, or run `aeon intake vocabulary`)"
	if res, err := intakeVocabularyProbe(); err == nil && len(res.Publish) > 0 {
		values := make([]string, 0, len(res.Publish))
		for _, pc := range res.Publish {
			values = append(values, pc.Value)
		}
		publishLine = fmt.Sprintf("              --publish one of [%s] (the daemon's closed set)", strings.Join(values, " "))
	}
	fmt.Fprint(w, `usage: aeon intake <verb> — the interview, the plan, the landing (S61)

  start      --name NAME [--scratch PATH] — open the interview: a row, a
             private scratch, the attended goal-intake loop
  list       [--json] — every intake with its honest state
  status     ID [--json] — one intake whole: state, proposal, the ledger
  plan       ID --proposal P [--target PATH] [--publish v] [--owner O]
             [--confirm-public] [--json] — the preview: files, rows, digest
  commit     ID --proposal P --digest D [--target PATH] [--publish v]
             [--owner O] [--confirm-public] [--yes] [--json] — the landing:
`+publishLine+`
             plans first (prints the preview), refuses without --digest,
             and --yes seats the publish for private or public
  abandon    ID --reason TEXT — close the session: stops the loop, rejects
             the open plans; the scratch stays for the person
  vocabulary [--json] — the closed sets and caps the doors ENFORCE

  Every verb is a client of the daemon's socket — there is no offline door:
  no loop, no conversation and no file lands without it, and a commit must
  not run in a second process (the recorded P5 deviation). The daemon's own
  sentences answer every refusal; the CLI re-validates nothing except
  inputs that cannot be a value at all.
`)
}

// intakePublishFlags mounts the publish trio the plan and commit verbs
// share; the values are the daemon's closed set (the vocabulary verb
// publishes them) and the CLI checks none — an illegal value comes back
// as the daemon's sentence.
type intakePublishFlags struct {
	target  *string
	publish *string
	owner   *string
	confirm *bool
}

func addIntakePublishFlags(fs *flag.FlagSet) intakePublishFlags {
	return intakePublishFlags{
		target:  fs.String("target", "", "where the root lands (default: the plan's own target or the configured workspace root)"),
		publish: fs.String("publish", "", "the vocabulary's publish choice (none is the default; the trio covers the plan's pin)"),
		owner:   fs.String("owner", "", "the account a publish targets (only with a publish choice)"),
		confirm: fs.Bool("confirm-public", false, "a second, deliberate yes for `public` — the word the door demands"),
	}
}

func (f intakePublishFlags) params(p *protocol.IntakePlanParams) {
	p.TargetPath = *f.target
	p.Publish = *f.publish
	p.Owner = *f.owner
	p.ConfirmPublic = *f.confirm
}

func intakeVerbStart(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("intake start", flag.ContinueOnError)
	fs.SetOutput(w)
	name := fs.String("name", "", "one line about the want (1–120 characters; the interview's first word)")
	scratch := fs.String("scratch", "", "where the interview notes live (default: the daemon lays a private directory under the state dir)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"intake start takes no positional arguments — the name is --name"}
	}
	if *name == "" {
		return &usageError{"intake start needs --name — the row, the scratch and the loop all open on that one line"}
	}
	p := protocol.IntakeStartParams{Name: *name, ScratchPath: *scratch}
	c, err := intakeDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodIntakeStart, p)
	}
	var view protocol.IntakeView
	if err := c.Call(protocol.MethodIntakeStart, p, &view); err != nil {
		return err
	}
	fmt.Fprintf(w, "intake %s opened · loop %s\n", view.ID, view.LoopSlug)
	fmt.Fprintf(w, "notes: %s — the interview answers through the loop's questions\n", view.ScratchPath)
	return nil
}

func intakeVerbList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("intake list", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"intake list takes no positional arguments — `aeon intake list [--json]`"}
	}
	c, err := intakeDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodIntakeList, struct{}{})
	}
	var res protocol.IntakeListResult
	if err := c.Call(protocol.MethodIntakeList, struct{}{}, &res); err != nil {
		return err
	}
	if len(res.Intakes) == 0 {
		fmt.Fprint(w, "no intakes — `aeon intake start --name …` opens the first interview\n")
		return nil
	}
	for _, r := range res.Intakes {
		line := fmt.Sprintf("%s · %s · %s", r.ID, r.State, r.Name)
		if r.ProposalID != "" {
			line += " · proposal " + r.ProposalID
		}
		if r.LoopSlug != "" {
			line += " · loop " + r.LoopSlug
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

func intakeVerbStatus(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("intake status", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || fs.NArg() != 0 {
		return &usageError{"intake status names exactly one intake — `aeon intake status ID [--json]`"}
	}
	p := protocol.IntakeStatusParams{IntakeID: positional[0]}
	c, err := intakeDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodIntakeStatus, p)
	}
	var view protocol.IntakeStatusView
	if err := c.Call(protocol.MethodIntakeStatus, p, &view); err != nil {
		return err
	}
	fmt.Fprintf(w, "%s · state: %s\n", view.IntakeID, view.State)
	if view.ProposalID != "" {
		fmt.Fprintf(w, "proposal %s (%s)\n", view.ProposalID, view.ProposalStatus)
	}
	if view.RootID != "" || view.TargetPath != "" {
		fmt.Fprintf(w, "root %s at %s\n", view.RootID, view.TargetPath)
	}
	if view.FailedStep != "" {
		fmt.Fprintf(w, "failed at: %s\n", view.FailedStep)
	}
	if view.Warning != "" {
		fmt.Fprintf(w, "warning: %s\n", view.Warning)
	}
	for _, s := range view.Steps {
		line := fmt.Sprintf("%s: %s", s.Step, s.Outcome)
		if s.Detail != "" {
			line += " — " + s.Detail
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

// intakeVerbPlan answers the preview — reading files nothing (GI10).
func intakeVerbPlan(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("intake plan", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	pf := addIntakePublishFlags(fs)
	proposal := fs.String("proposal", "", "the intake_plan proposal to preview (an id `intake status` names)")
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || fs.NArg() != 0 {
		return &usageError{"intake plan names exactly one intake — `aeon intake plan ID --proposal P [--target PATH] [--json]`"}
	}
	if *proposal == "" {
		return &usageError{"intake plan needs --proposal — a preview of what? `intake status ID` names the open proposal"}
	}
	p := protocol.IntakePlanParams{IntakeID: positional[0], ProposalID: *proposal}
	pf.params(&p)
	c, err := intakeDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodIntakePlan, p)
	}
	var res protocol.IntakePreview
	if err := c.Call(protocol.MethodIntakePlan, p, &res); err != nil {
		return err
	}
	renderIntakePreview(w, res)
	return nil
}

// renderIntakePreview is the fixed human template over the preview: the
// digest LAST (it is what `commit --digest` must carry), the file list
// under each component, the record census in the daemon's own lines.
func renderIntakePreview(w io.Writer, res protocol.IntakePreview) {
	fmt.Fprintf(w, "plan %s · intake %s · publish %s\n", res.ProposalID, res.IntakeID, res.Publish)
	fmt.Fprintf(w, "target: %s\n", res.TargetPath)
	for _, comp := range res.Components {
		fmt.Fprintf(w, "  %s (%s, %s) → %s · blueprint %s\n", comp.Slug, comp.Role, comp.Name, comp.TargetPath, comp.Blueprint)
		for _, f := range comp.Files {
			fmt.Fprintf(w, "    %s\n", f)
		}
		// The git commands the run will execute, printed so the person
		// sees WHAT runs, not just what lands (GI7's preview).
		for _, a := range comp.Actions {
			fmt.Fprintf(w, "    $ %s\n", a)
		}
		if comp.RemoteCreated {
			fmt.Fprint(w, "    the remote repository will be CREATED\n")
		}
	}
	fmt.Fprintf(w, "records: goals %d · epics %d · items %d\n", res.Goals, res.Epics, res.Items)
	for _, line := range res.Records {
		fmt.Fprintf(w, "  %s\n", line)
	}
	fmt.Fprintf(w, "digest: %s\n", res.Digest)
	fmt.Fprint(w, "nothing was filed — carry the digest to `intake commit --digest` to land this plan\n")
}

func intakeVerbCommit(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("intake commit", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	yes := fs.Bool("yes", false, "the seatbelt for a publish that reaches outside the machine")
	digest := fs.String("digest", "", "the digest `intake plan` printed — required")
	proposal := fs.String("proposal", "", "the intake_plan proposal to land (an id `intake status` names)")
	pf := addIntakePublishFlags(fs)
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || fs.NArg() != 0 {
		return &usageError{"intake commit names exactly one intake — `aeon intake commit ID --proposal P --digest D [flags]`"}
	}
	if *proposal == "" {
		return &usageError{"intake commit needs --proposal — `intake status ID` names the open proposal"}
	}
	if *digest == "" {
		return &usageError{msg: "intake commit needs --digest — run `aeon intake plan ID --proposal P …` first and carry the digest it prints; the digest is what pins the shown plan to the filed one (GI12)"}
	}
	// --json prints the WIRE result verbatim — interleaving the preview's
	// own bytes would corrupt that promise, so the human road alone gets
	// the preview print; the digest pin guards both.
	if !*asJSON {
		// The spec's flow: print the daemon's statement and file list
		// before the landing, from the daemon's OWN preview — the same
		// door, the same digest the flag must carry.
		pp := protocol.IntakePlanParams{IntakeID: positional[0], ProposalID: *proposal}
		pp.TargetPath, pp.Publish, pp.Owner, pp.ConfirmPublic = *pf.target, *pf.publish, *pf.owner, *pf.confirm
		c, err := intakeDial(*want)
		if err != nil {
			return err
		}
		var prev protocol.IntakePreview
		if err := c.Call(protocol.MethodIntakePlan, pp, &prev); err != nil {
			c.Close()
			return err
		}
		renderIntakePreview(w, prev)
		c.Close()
	}
	if *pf.publish == "private" || *pf.publish == "public" {
		if !*yes {
			return &usageError{msg: fmt.Sprintf("`--publish %s` reaches outside this machine and needs --yes — a publish publishes (the seatbelt is a habit, not a security control)", *pf.publish)}
		}
	}
	p := protocol.IntakeCommitParams{
		IntakeID: positional[0], ProposalID: *proposal, Digest: *digest,
		TargetPath: *pf.target, Publish: *pf.publish, Owner: *pf.owner, ConfirmPublic: *pf.confirm,
	}
	c, err := intakeDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodIntakeCommit, p)
	}
	var res protocol.IntakeCommitResult
	if err := c.Call(protocol.MethodIntakeCommit, p, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "committed: intake %s · root %s at %s\n", res.IntakeID, res.RootID, res.TargetPath)
	if res.Resumed {
		fmt.Fprint(w, "resumed an interrupted run — steps already done stand\n")
	}
	for _, comp := range res.Components {
		line := fmt.Sprintf("  %s → %s", comp.Slug, comp.TargetPath)
		if comp.RepoUrl != "" {
			line += " · " + comp.RepoUrl
		}
		if comp.CommitSha != "" {
			line += " · commit " + comp.CommitSha
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintf(w, "records: goals %d · epics %d · items %d\n", res.Goals, res.Epics, res.Items)
	if res.Warning != "" {
		fmt.Fprintf(w, "warning: %s\n", res.Warning)
	}
	return nil
}

func intakeVerbAbandon(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("intake abandon", flag.ContinueOnError)
	fs.SetOutput(w)
	reason := fs.String("reason", "", "why the session closes (required — a decision without its sentence is an audit gap)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || fs.NArg() != 0 {
		return &usageError{"intake abandon names exactly one intake — `aeon intake abandon ID --reason TEXT`"}
	}
	if *reason == "" {
		return &usageError{"intake abandon needs --reason — the rejects this drives are decisions, and decisions carry their sentence"}
	}
	p := protocol.IntakeAbandonParams{IntakeID: positional[0], Reason: *reason}
	c, err := intakeDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodIntakeAbandon, p)
	}
	var res protocol.IntakeAbandonResult
	if err := c.Call(protocol.MethodIntakeAbandon, p, &res); err != nil {
		return err
	}
	loop := "still stands — `loop stop` answers for it"
	if res.LoopClosed {
		loop = "closed"
	}
	fmt.Fprintf(w, "abandoned: intake %s — the interview loop %s\n", res.IntakeID, loop)
	if len(res.RejectedProposals) > 0 {
		fmt.Fprintf(w, "rejected proposals: %v\n", res.RejectedProposals)
	}
	fmt.Fprintf(w, "the scratch at %s stays for the person — nothing here deletes it\n", res.Intake.ScratchPath)
	return nil
}

func intakeVerbVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("intake vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"intake vocabulary takes no positional arguments — `aeon intake vocabulary [--json]`"}
	}
	c, err := intakeDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodIntakeVocabulary, struct{}{})
	}
	var res protocol.IntakeVocabularyResult
	if err := c.Call(protocol.MethodIntakeVocabulary, struct{}{}, &res); err != nil {
		return err
	}
	publish := make([]string, 0, len(res.Publish))
	for _, pc := range res.Publish {
		publish = append(publish, pc.Value)
	}
	fmt.Fprintf(w, "publish: [%s]\nroles: %v\n", strings.Join(publish, " "), res.Roles)
	if res.RemoteCreateEnabled {
		fmt.Fprint(w, "remote create: enabled\n")
	} else {
		fmt.Fprintf(w, "remote create: off — %s\n", res.RemoteCreateReason)
	}
	if !res.RouteAvailable {
		fmt.Fprint(w, "no model route configured — `aeon routes set` before opening an interview\n")
	}
	fmt.Fprintf(w, "caps: payload %d B · components %d · epics %d · goals %d · items %d · criteria per goal %d\n",
		res.Caps.PayloadBytes, res.Caps.Components, res.Caps.Epics, res.Caps.Goals, res.Caps.Items, res.Caps.CriteriaPerGoal)
	if res.WorkspaceRoot != "" {
		fmt.Fprintf(w, "workspace root: %s (%s)\n", res.WorkspaceRoot, res.WorkspaceRootState)
	} else {
		fmt.Fprintf(w, "workspace root: unset — %s\n", res.WorkspaceRootState)
	}
	for _, t := range res.Templates {
		fmt.Fprintf(w, "template %s v%d (%s)\n", t.Slug, t.Version, t.Origin)
	}
	return nil
}

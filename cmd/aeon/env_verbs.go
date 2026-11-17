package main

// env_verbs.go — S59 T8 (EV18, EV7, P34): `aeon env`, the execution-environment
// plane at the terminal.
//
// A **pure client**, and that is the whole design. Every verb names a socket
// method in `envVerbs` and calls it; nothing here re-validates, re-formats or
// softens a refusal. `renderError` prints the daemon's three-part sentence
// (what / means / todo) verbatim and the verb exits non-zero; `--json` prints
// the wire result verbatim (the same bytes the panel gets), so a script and the
// app read one answer, not two.
//
// **No direct door — the recorded deviation from P5.** Every operation on this
// plane needs the daemon's resolver, the seed engine and the reconcile state
// that lives with it, and `aeon` has no first-run path that needs an offline
// environment (design EV18). So `selectEnvDoor` dials and, when nobody is home,
// returns the dial fault — there is no `nobodyIsHome` fallback here, no
// `internal/store` import in this file or its sibling
// (`TestEnvVerbsNeverImportTheStore`), and no second copy of any validation.
//
// **No secret value can enter** (EV7, C17). `create`/`update` take a spec
// document — which holds no value by construction (EV4) — and secret REFERENCES
// by name; the sibling file's `secretref create` takes a LOCATION (`--from-env`,
// `--from-file`), never a value. There is no `--value`, no `--secret-value`, no
// `--from-stdin`, no prompt, in either file; the flag-set walk and the source
// grep in `TestSecretRefCreateTakesALocationNeverAValue` are what keep that
// true after someone adds the next flag.
//
// `--help` on `create`/`update` is **generated from `environment.vocabulary`**
// (`envSpecFlagHelp`): every closed set a person could type is printed as the
// live daemon answers it, so `internal/execenv` stays the only place those
// words are written down.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/protocol"
)

// envVerb is one row of the verb table: the CLI word, the socket method it is a
// client of, and what gates it. `agentOnly` marks the plane's one agent door —
// the CLI has NO verb for it and says why, so the parity test can walk the
// methods in both directions and still account for all twenty-one.
type envVerb struct {
	name      string
	method    protocol.MethodName
	seatbelt  bool // --yes required (an authority change; a dry run is exempt)
	agentOnly bool // no verb; the agent's door, listed with its reason
	why       string
}

// envVerbs is the plane's door list at the terminal: the thirteen
// `environment.*` methods, plus the plane's twenty-first method marked
// agent-only. `environment.proposeRestrict` is the ONLY environment door an
// agent may reach (S59 T7), so it has no verb: a person does not need a CLI
// verb for the door their agent knocks on, and a verb here would be a second
// author of a tightening that only the desk's commit applies.
var envVerbs = []envVerb{
	{name: "list", method: protocol.MethodEnvironmentList},
	{name: "show", method: protocol.MethodEnvironmentShow},
	{name: "effective", method: protocol.MethodEnvironmentEffective},
	{name: "vocabulary", method: protocol.MethodEnvironmentVocabulary},
	{name: "changes", method: protocol.MethodEnvironmentChanges},
	{name: "provisions", method: protocol.MethodEnvironmentProvisions},
	{name: "create", method: protocol.MethodEnvironmentCreate},
	{name: "update", method: protocol.MethodEnvironmentUpdate},
	{name: "archive", method: protocol.MethodEnvironmentArchive},
	{name: "restore", method: protocol.MethodEnvironmentRestore},
	{name: "bind", method: protocol.MethodEnvironmentBind, seatbelt: true},
	{name: "unbind", method: protocol.MethodEnvironmentUnbind, seatbelt: true},
	{name: "teardown", method: protocol.MethodEnvironmentTeardown, seatbelt: true},
	{name: "propose-restrict", method: protocol.MethodEnvironmentProposeRestrict, agentOnly: true,
		why: "the agent's own door (S59 T7): an agent FILES the inert tightening and a person commits it — there is no CLI author"},
}

// envDoor is the one call this file makes. It is an interface so the tests can
// stand in an answer without a socket; production has exactly one
// implementation, the socket. There is no second door — see the header.
type envDoor interface {
	relay(method protocol.MethodName, params, out any) error
	close() error
}

type envSocketDoor struct{ c *protocol.Client }

func (d *envSocketDoor) relay(method protocol.MethodName, params, out any) error {
	return d.c.Call(method, params, out)
}

func (d *envSocketDoor) close() error { return d.c.Close() }

// envDial is the dial itself (production), and envDialDoor the seam the tests
// stand a fake door behind — the gate door's `newGateDirectDoor` precedent, so
// a seatbelt refusal can be proven without an endpoint, a store or a socket.
// The seam returns a door or an error, never a typed-nil wearing the interface.
var (
	envDial     = dial
	envDialDoor = func() (envDoor, error) {
		c, err := envDial(0)
		if err != nil {
			return nil, err
		}
		return &envSocketDoor{c: c}, nil
	}
)

func selectEnvDoor() (envDoor, error) {
	d, err := envDialDoor()
	if err != nil {
		// EV18: no fallback. The standard "daemon not running" words are
		// dial's own and this file adds none to them.
		return nil, err
	}
	return d, nil
}

// envParse parses a verb's arguments, then re-parses with any positional moved
// to the end. The flag package stops at the FIRST positional, so the order the
// task's usage spells — `aeon env update <id|slug> --expect-revision 3` — would
// otherwise leave the trailing flags unparsed and the verb refusing for the
// wrong reason. A person gets to type it either way; the refusal stays the
// daemon's.
func envParse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) > 1 {
		tail := append([]string{}, rest[1:]...)
		return fs.Parse(append(tail, rest[0]))
	}
	return nil
}

// envJSONFlag declares --json once, with the same contract every other family's
// client gives it: the wire result verbatim, the panel's bytes.
func envJSONFlag(fs *flag.FlagSet) *bool {
	return fs.Bool("json", false, "print the wire result verbatim (the panel's bytes), not the human lines")
}

// cmdEnv is the dispatcher: `aeon env <verb> …`.
func cmdEnv(w io.Writer, args []string) error {
	if len(args) == 0 {
		return envUsage(w)
	}
	verb, rest := args[0], args[1:]
	for _, v := range envVerbs {
		if v.name != verb {
			continue
		}
		if v.agentOnly {
			return fmt.Errorf("aeon env %s is not a person's verb — %s", v.name, v.why)
		}
		return v.run(w, rest)
	}
	fmt.Fprintf(w, "aeon env: unknown verb %q\n\n", verb)
	return envUsage(w)
}

func envUsage(w io.Writer) error {
	fmt.Fprintln(w, "usage: aeon env <verb> [flags]")
	fmt.Fprintln(w, "")
	for _, v := range envVerbs {
		if v.agentOnly {
			fmt.Fprintf(w, "  %-12s (agent's door only — %s)\n", v.name, v.why)
			continue
		}
		gate := ""
		if v.seatbelt {
			gate = " [--yes]"
		}
		fmt.Fprintf(w, "  %-12s → %s%s [--json]\n", v.name, v.method, gate)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  create and update take (--spec-file PATH | the spec flags); --help on either")
	fmt.Fprintln(w, "  prints the closed sets AS THE DAEMON ANSWERS THEM (environment.vocabulary).")
	fmt.Fprintln(w, "  No verb, flag, stdin path or prompt on this plane accepts a secret value (EV7).")
	return nil
}

// run wires one verb: its FlagSet, its door, its call, its printer. A refusal
// from the daemon is returned UNCHANGED, so `renderError` — the ONE renderer —
// speaks it.
func (v envVerb) run(w io.Writer, args []string) error {
	door, err := selectEnvDoor()
	if err != nil {
		return err
	}
	defer func() { _ = door.close() }()
	ctx := context.Background()

	switch v.name {
	case "list":
		return envList(w, ctx, door, args)
	case "show":
		return envShow(w, ctx, door, args)
	case "effective":
		return envEffective(w, ctx, door, args)
	case "vocabulary":
		return envVocabulary(w, ctx, door, args)
	case "changes":
		return envChanges(w, ctx, door, args)
	case "provisions":
		return envProvisions(w, ctx, door, args)
	case "create":
		return envCreate(w, ctx, door, args, v.name)
	case "update":
		return envUpdate(w, ctx, door, args, v.name)
	case "archive", "restore":
		return envArchiveRestore(w, ctx, door, v, args)
	case "bind":
		return envBind(w, ctx, door, args)
	case "unbind":
		return envUnbind(w, ctx, door, args)
	case "teardown":
		return envTeardown(w, ctx, door, args)
	}
	return fmt.Errorf("aeon env %s: no implementation (the verb table and the runner have drifted)", v.name)
}

// envPrint is the ONE presentation helper: `--json` prints the wire bytes and
// nothing else; the human path prints the verb's lines — and every line a
// daemon authored is printed verbatim, because a re-worded refusal is a second
// truth.
func envPrint(w io.Writer, jsonOut bool, res any, human func()) error {
	if jsonOut {
		return printJSON(w, res)
	}
	if human != nil {
		human()
	}
	return nil
}

// --- the reads -------------------------------------------------------------------

func envList(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("env list", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	backend := fs.String("backend", "", "narrow to one backend (the closed set; the daemon refuses an unknown word)")
	origin := fs.String("origin", "", "narrow to one origin: system or user")
	archived := fs.Bool("archived", false, "include archived environments too")
	limit := fs.Int("limit", 0, "at most this many rows (0 = the daemon's own default)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p := protocol.EnvironmentListParams{Backend: *backend, Origin: *origin, IncludeArchived: *archived, Limit: *limit}
	var rows []protocol.EnvironmentView
	if err := door.relay(protocol.MethodEnvironmentList, p, &rows); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, rows, func() {
		if len(rows) == 0 {
			fmt.Fprintln(w, "no environments")
			return
		}
		for _, e := range rows {
			fmt.Fprintf(w, "%-28s %-14s rev %d  %s%s\n", e.Slug, e.Backend, e.Revision, e.Name, envArchivedMark(e))
		}
	})
}

func envShow(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("env show", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	if err := envParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("aeon env show needs exactly one environment: <id|slug>")
	}
	p := protocol.EnvironmentShowParams{Slug: fs.Arg(0)}
	var out protocol.EnvironmentView
	if err := door.relay(protocol.MethodEnvironmentShow, p, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() { envPrintView(w, out) })
}

func envEffective(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("env effective", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	component := fs.String("component", "", "the component to ask about (id or path; empty asks the workspace default)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p := protocol.EnvironmentEffectiveParams{ComponentID: *component}
	// No --attended flag (S59 T12): attendedness decides EV6's inheritance
	// note, and G14 reserves declaring it to the three start doors — a person
	// at a preview door must not be able to pick the answer that shows them
	// more than the spawn will give. The daemon answers it; this verb asks.
	var out protocol.EnvironmentEffectiveView
	if err := door.relay(protocol.MethodEnvironmentEffective, p, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() { envPrintEffective(w, out) })
}

func envVocabulary(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("env vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	var out protocol.EnvironmentVocabularyView
	if err := door.relay(protocol.MethodEnvironmentVocabulary, struct{}{}, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() { envPrintVocabulary(w, out) })
}

func envChanges(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("env changes", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	subjectKind := fs.String("subject-kind", "", "environment · secret_ref · binding (empty = the whole log)")
	subject := fs.String("subject", "", "the subject's own id")
	action := fs.String("action", "", "one action word")
	limit := fs.Int("limit", 0, "at most this many rows")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p := protocol.EnvironmentChangesParams{SubjectKind: *subjectKind, SubjectID: *subject, Action: *action, Limit: *limit}
	var rows []protocol.EnvironmentChangeView
	if err := door.relay(protocol.MethodEnvironmentChanges, p, &rows); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, rows, func() {
		if len(rows) == 0 {
			fmt.Fprintln(w, "no changes")
			return
		}
		for _, c := range rows {
			fmt.Fprintf(w, "%s  %s %s · %s\n", c.At, c.ActorKind, c.Action, c.Summary)
		}
	})
}

func envProvisions(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("env provisions", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	state := fs.String("state", "", "one lifecycle state (provisioning · ready · tearing_down · removed · failed)")
	limit := fs.Int("limit", 0, "at most this many rows")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p := protocol.EnvironmentProvisionsParams{State: *state, Limit: *limit}
	var rows []protocol.ProvisionView
	if err := door.relay(protocol.MethodEnvironmentProvisions, p, &rows); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, rows, func() {
		if len(rows) == 0 {
			fmt.Fprintln(w, "no provisions")
			return
		}
		for _, r := range rows {
			fmt.Fprintf(w, "%-24s %-14s %-12s %s\n", r.ID, r.State, r.Backend, r.EnvironmentID)
		}
	})
}

// --- the writes -------------------------------------------------------------------

func envCreate(w io.Writer, ctx context.Context, door envDoor, args []string, verb string) error {
	fs := flag.NewFlagSet("env create", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	name := fs.String("name", "", "the environment's name (required)")
	slug := fs.String("slug", "", "the slug the person reads; empty derives from --name")
	backend := fs.String("backend", "", "the backend (required — one of the closed set the vocabulary answers)")
	desc := fs.String("description", "", "what it is for")
	specFile := fs.String("spec-file", "", "the spec document (JSON or YAML, EV4) — it holds no secret value by construction")
	yes := fs.Bool("yes", false, "the seatbelt: required for a host-inherit environment")
	spec := envBindSpecFlags(fs)
	// BEFORE the flag package sees -h (which would print its own usage and
	// return ErrHelp): these two verbs answer with the vocabulary instead.
	if wantsHelp(args) {
		return envSpecFlagHelp(w, door, verb)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *backend == "" {
		return fmt.Errorf("aeon env create needs --name and --backend (`aeon env vocabulary` lists the closed sets as this daemon answers them)")
	}
	if *backend == execenv.BackendHostInherit && !*yes {
		return fmt.Errorf("aeon env create --backend %s needs --yes — inheriting the daemon's own environment is the widest thing an environment can be, and EV6 says so out loud", execenv.BackendHostInherit)
	}
	doc, err := envSpecDocument(fs, spec, *specFile)
	if err != nil {
		return err
	}
	p := protocol.EnvironmentCreateParams{Slug: *slug, Name: *name, Description: *desc, Backend: *backend, Spec: doc}
	var out protocol.EnvironmentView
	if err := door.relay(protocol.MethodEnvironmentCreate, p, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() { envPrintView(w, out) })
}

func envUpdate(w io.Writer, ctx context.Context, door envDoor, args []string, verb string) error {
	fs := flag.NewFlagSet("env update", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	expect := fs.Int64("expect-revision", -1, "the revision your edit is written against (required — a stale one refuses, it never clobbers)")
	specFile := fs.String("spec-file", "", "the new spec document (JSON or YAML, EV4)")
	spec := envBindSpecFlags(fs)
	// BEFORE the flag package sees -h (its own usage and ErrHelp would answer
	// instead of the daemon's vocabulary).
	if wantsHelp(args) {
		return envSpecFlagHelp(w, door, verb)
	}
	if err := envParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("aeon env update needs the environment: aeon env update <id|slug> (--spec-file PATH | the spec flags) --expect-revision N")
	}
	if *expect < 0 {
		return fmt.Errorf("aeon env update needs --expect-revision N — the plane writes with compare-and-set, and a client that guesses is the clobber (EV18)")
	}
	doc, err := envSpecDocument(fs, spec, *specFile)
	if err != nil {
		return err
	}
	p := protocol.EnvironmentUpdateParams{ID: fs.Arg(0), Spec: doc, ExpectedRevision: *expect}
	var out protocol.EnvironmentView
	if err := door.relay(protocol.MethodEnvironmentUpdate, p, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() { envPrintView(w, out) })
}

func envArchiveRestore(w io.Writer, ctx context.Context, door envDoor, v envVerb, args []string) error {
	fs := flag.NewFlagSet("env "+v.name, flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	if err := envParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("aeon env %s needs exactly one environment: <id|slug>", v.name)
	}
	p := protocol.EnvironmentIDParams{Slug: fs.Arg(0)}
	var out protocol.EnvironmentView
	if err := door.relay(v.method, p, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() { envPrintView(w, out) })
}

func envBind(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("env bind", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	workspace := fs.Bool("workspace", false, "bind the workspace floor")
	component := fs.String("component", "", "bind one component (id or path)")
	dryRun := fs.Bool("dry-run", false, "ask what would change and write nothing (no --yes: the answer is the daemon's own statement)")
	yes := fs.Bool("yes", false, "the seatbelt for the applied bind")
	if err := envParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("aeon env bind needs the environment: aeon env bind (--workspace | --component ID|PATH) <id|slug>")
	}
	p := protocol.EnvironmentBindParams{EnvironmentID: fs.Arg(0), DryRun: *dryRun}
	switch {
	case *workspace && *component != "":
		return fmt.Errorf("aeon env bind takes --workspace OR --component, not both")
	case *workspace:
		p.Scope = "workspace"
	case *component != "":
		p.Scope = "component"
		p.ComponentID = *component
	default:
		return fmt.Errorf("aeon env bind needs --workspace or --component ID|PATH — a bind that names no scope is a guess about what a loop runs in")
	}
	if !p.DryRun && !*yes {
		return fmt.Errorf("aeon env bind needs --yes (or --dry-run to read the statement first) — a binding decides what every command on that scope runs with (EV18)")
	}
	var out protocol.BindResult
	if err := door.relay(protocol.MethodEnvironmentBind, p, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() {
		// One formatter: the daemon's `execenv.DescribeBind` sentence, printed
		// verbatim. The dry run and the applied bind differ only by that
		// sentence's own clause, so there is nothing here to add — and a
		// second wording of it is EV18's forbidden second truth.
		fmt.Fprintln(w, out.Statement)
		if out.DryRun {
			fmt.Fprintln(w, "(dry run — nothing was written)")
		}
	})
}

func envUnbind(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("env unbind", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	component := fs.String("component", "", "the component to release (id or path; required)")
	yes := fs.Bool("yes", false, "the seatbelt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *component == "" {
		return fmt.Errorf("aeon env unbind needs --component ID|PATH — the workspace binding is the floor the seed laid and no door removes it (EV19)")
	}
	if !*yes {
		return fmt.Errorf("aeon env unbind needs --yes — releasing a binding sends that component's commands back to the workspace floor")
	}
	p := protocol.EnvironmentUnbindParams{ComponentID: *component}
	var out protocol.UnbindResult
	if err := door.relay(protocol.MethodEnvironmentUnbind, p, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() {
		if out.Unbound {
			fmt.Fprintln(w, "unbound — that component now takes the workspace floor")
			return
		}
		fmt.Fprintln(w, "nothing was unbound")
	})
}

func envTeardown(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("env teardown", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	yes := fs.Bool("yes", false, "the seatbelt: teardown removes a real backend")
	if err := envParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("aeon env teardown needs exactly one provision id: aeon env teardown <provision-id> --yes")
	}
	if !*yes {
		return fmt.Errorf("aeon env teardown needs --yes — it removes a real container or VM, and the ledger walk that answers is the daemon's (EV15)")
	}
	p := protocol.ProvisionIDParams{ProvisionID: fs.Arg(0)}
	var out protocol.ProvisionView
	if err := door.relay(protocol.MethodEnvironmentTeardown, p, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() {
		fmt.Fprintf(w, "%s  %s  %s\n", out.ID, out.State, out.Backend)
	})
}

// --- the spec flags, and the help the daemon writes --------------------------------

// envSpecFlags carries the spec's own flags. `--spec-file` is the other half of
// the pair; the daemon's `execenv.Validate` is the ONLY validator, so nothing
// here checks a name, a bound or a mode — every one of those answers comes back
// as the daemon's own three-part refusal (EV4).
type envSpecFlags struct {
	passthrough string
	set         stringList
	secrets     stringList
	home        string
	wallSeconds int
	memoryMiB   int
	cpus        float64
	egress      string
	allow       stringList
	image       string
	pull        string
}

func envBindSpecFlags(fs *flag.FlagSet) *envSpecFlags {
	s := &envSpecFlags{}
	fs.StringVar(&s.passthrough, "passthrough", "", "comma list of variable NAMES to pass through (names only — A,B,LC_*)")
	fs.Var(&s.set, "set", "K=V to set (repeatable); a credential-shaped name is refused by the daemon")
	fs.Var(&s.secrets, "secret", "secret-reference NAME to inject (repeatable) — a reference NAME, never a value (EV7)")
	fs.StringVar(&s.home, "home", "", "home mode (the closed set the vocabulary answers)")
	fs.IntVar(&s.wallSeconds, "wall-seconds", 0, "wall-clock cap, seconds (0 = no cap)")
	fs.IntVar(&s.memoryMiB, "memory-mib", 0, "memory cap, MiB (0 = no cap)")
	fs.Float64Var(&s.cpus, "cpus", 0, "CPU cap (0 = no cap)")
	fs.StringVar(&s.egress, "egress", "", "egress mode (the closed set the vocabulary answers)")
	fs.Var(&s.allow, "allow", "egress allow entry (repeatable), with --egress")
	fs.StringVar(&s.image, "image", "", "image reference (container/vm)")
	fs.StringVar(&s.pull, "pull", "", "pull policy (with --image; the closed set the vocabulary answers)")
	return s
}

// wantsHelp is the `--help` of create and update. The flag package would print
// its own usage for -h and return ErrHelp; these two verbs answer with the
// vocabulary-generated help instead, so the closed sets are read BEFORE the
// refusal — and read from the daemon, never typed here.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// envSpecDocument assembles the spec STRING the wire carries. It emits only the
// axes the flags named and validates nothing: `execenv.Validate` behind the
// daemon is the one validator, and what comes back is what it approved (EV4).
func envSpecDocument(fs *flag.FlagSet, s *envSpecFlags, specFile string) (string, error) {
	flagsGiven := s.passthrough != "" || len(s.set) > 0 || len(s.secrets) > 0 || s.home != "" ||
		s.wallSeconds != 0 || s.memoryMiB != 0 || s.cpus != 0 || s.egress != "" || len(s.allow) > 0 ||
		s.image != "" || s.pull != ""
	if specFile != "" && flagsGiven {
		return "", fmt.Errorf("aeon env takes --spec-file OR the spec flags, never both — one of them would be silently ignored")
	}
	if specFile != "" {
		raw, err := os.ReadFile(specFile)
		if err != nil {
			return "", fmt.Errorf("aeon env cannot read --spec-file %s: %w", specFile, err)
		}
		return string(raw), nil
	}
	if !flagsGiven {
		return "", fmt.Errorf("aeon env needs a spec: --spec-file PATH or the spec flags (`aeon env create --help` prints them with the daemon's closed sets)")
	}
	sp := execenv.Spec{}
	sp.Home = s.home
	if s.passthrough != "" {
		for _, n := range strings.Split(s.passthrough, ",") {
			if n = strings.TrimSpace(n); n != "" {
				sp.Env.Passthrough = append(sp.Env.Passthrough, n)
			}
		}
	}
	if len(s.set) > 0 {
		sp.Env.Set = map[string]string{}
		for _, kv := range s.set {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				return "", fmt.Errorf("aeon env --set wants K=V, got %q", kv)
			}
			sp.Env.Set[strings.TrimSpace(k)] = v
		}
	}
	sp.Env.Secrets = append(sp.Env.Secrets, s.secrets...)
	if s.wallSeconds != 0 {
		n := s.wallSeconds
		sp.Limits.WallSeconds = &n
	}
	if s.memoryMiB != 0 {
		n := s.memoryMiB
		sp.Limits.MemoryMiB = &n
	}
	if s.cpus != 0 {
		n := s.cpus
		sp.Limits.CPUs = &n
	}
	if s.egress != "" || len(s.allow) > 0 {
		sp.Egress.Mode = s.egress
		sp.Egress.Allow = append(sp.Egress.Allow, s.allow...)
	}
	if s.image != "" {
		sp.Image = &execenv.Image{Ref: s.image, Pull: s.pull}
	}
	raw, err := json.Marshal(sp)
	if err != nil {
		return "", fmt.Errorf("aeon env could not encode the spec: %w", err)
	}
	return string(raw), nil
}

// envSpecFlagHelp writes create/update's help from the DAEMON's vocabulary, so
// `internal/execenv` stays the only place the closed sets are written down
// (T8's `TestCreateHelpIsGeneratedFromVocabulary`).
func envSpecFlagHelp(w io.Writer, door envDoor, verb string) error {
	var v protocol.EnvironmentVocabularyView
	if err := door.relay(protocol.MethodEnvironmentVocabulary, struct{}{}, &v); err != nil {
		return err
	}
	fmt.Fprintf(w, "usage: aeon env %s [flags] (--spec-file PATH | the spec flags)\n\n", verb)
	fmt.Fprintln(w, "  --name N --backend B [--slug S] [--description D] [--yes] [--json]    (create)")
	fmt.Fprintln(w, "  <id|slug> --expect-revision N [--json]                                 (update)")
	fmt.Fprintln(w, "  spec flags:")
	fmt.Fprintln(w, "    --passthrough A,B,LC_*   --set K=V (repeat)   --secret NAME (repeat)")
	fmt.Fprintln(w, "    --home MODE              --egress MODE [--allow E (repeat)]")
	fmt.Fprintln(w, "    --wall-seconds N         --memory-mib N       --cpus F")
	fmt.Fprintln(w, "    --image REF              --pull POLICY")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "the closed sets, as this daemon answers them (environment.vocabulary):")
	var names []string
	for _, b := range v.Backends {
		names = append(names, fmt.Sprintf("%s(%d)", b.Name, b.Strength))
	}
	fmt.Fprintf(w, "  backends        %s\n", strings.Join(names, " · "))
	fmt.Fprintf(w, "  home modes      %s\n", strings.Join(v.HomeModes, " · "))
	fmt.Fprintf(w, "  egress modes    %s\n", strings.Join(v.EgressModes, " · "))
	fmt.Fprintf(w, "  pull policies   %s\n", strings.Join(v.PullPolicies, " · "))
	fmt.Fprintf(w, "  source kinds    %s\n", strings.Join(v.SourceKinds, " · "))
	fmt.Fprintf(w, "  default allowed %s\n", strings.Join(v.DefaultAllowlist, " · "))
	fmt.Fprintf(w, "  denied words    %s\n", strings.Join(v.DenyWords, " · "))
	fmt.Fprintf(w, "  denied prefixes %s\n", strings.Join(v.DenyPrefixes, " · "))
	fmt.Fprintf(w, "  denied exact    %s\n", strings.Join(v.DenyExact, " · "))
	fmt.Fprintf(w, "  denied loader   %s\n", strings.Join(v.DenyLoader, " · "))
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  A spec carries NAMES, not values: a secret enters by reference and never comes out (EV7).")
	fmt.Fprintln(w, "  The daemon is the only validator; its refusal is printed verbatim (EV4).")
	return nil
}

// --- the human lines ---------------------------------------------------------------

func envArchivedMark(e protocol.EnvironmentView) string {
	if e.ArchivedAt != nil {
		return "  (archived)"
	}
	return ""
}

func envPrintView(w io.Writer, e protocol.EnvironmentView) {
	fmt.Fprintf(w, "%s  rev %d  %s\n", e.Slug, e.Revision, e.Name)
	fmt.Fprintf(w, "  backend   %s · origin %s%s\n", e.Backend, e.Origin, envArchivedMark(e))
	if e.Description != "" {
		fmt.Fprintf(w, "  note      %s\n", e.Description)
	}
	if len(e.Capabilities) > 0 {
		fmt.Fprintf(w, "  caps      %s\n", strings.Join(e.Capabilities, " · "))
	}
	if len(e.Warnings) > 0 {
		fmt.Fprintf(w, "  warnings  %s\n", strings.Join(e.Warnings, " · "))
	}
	if len(e.SecretRefNames) > 0 {
		fmt.Fprintf(w, "  secrets   %s\n", strings.Join(e.SecretRefNames, " · "))
	}
	for _, b := range e.Bindings {
		fmt.Fprintf(w, "  bound     %s %s → %s\n", b.ScopeKind, b.ComponentID, b.EnvironmentID)
	}
}

func envPrintEffective(w io.Writer, v protocol.EnvironmentEffectiveView) {
	fmt.Fprintf(w, "%s  backend %s  %s\n", v.ScopeKind, v.Backend, v.Honesty)
	if v.EnvironmentSlug != "" {
		fmt.Fprintf(w, "  environment %s\n", v.EnvironmentSlug)
	}
	if v.Legacy {
		fmt.Fprintln(w, "  legacy      yes — this is the unbound inherit the seed laid down (EV19)")
	}
	if len(v.Capabilities) > 0 {
		fmt.Fprintf(w, "  caps        %s\n", strings.Join(v.Capabilities, " · "))
	}
	for _, vr := range v.Variables {
		fmt.Fprintf(w, "  %-28s %s\n", vr.Name, vr.Source)
	}
	if len(v.SecretRefNames) > 0 {
		fmt.Fprintf(w, "  secrets     %s\n", strings.Join(v.SecretRefNames, " · "))
	}
	if v.DowngradeNote != "" {
		fmt.Fprintf(w, "  note        %s\n", v.DowngradeNote)
	}
}

func envPrintVocabulary(w io.Writer, v protocol.EnvironmentVocabularyView) {
	var names []string
	for _, b := range v.Backends {
		names = append(names, fmt.Sprintf("%s(%d)", b.Name, b.Strength))
	}
	fmt.Fprintf(w, "backends        %s\n", strings.Join(names, " · "))
	fmt.Fprintf(w, "home modes      %s\n", strings.Join(v.HomeModes, " · "))
	fmt.Fprintf(w, "egress modes    %s\n", strings.Join(v.EgressModes, " · "))
	fmt.Fprintf(w, "pull policies   %s\n", strings.Join(v.PullPolicies, " · "))
	fmt.Fprintf(w, "source kinds    %s\n", strings.Join(v.SourceKinds, " · "))
	fmt.Fprintf(w, "default allowed %s\n", strings.Join(v.DefaultAllowlist, " · "))
	fmt.Fprintf(w, "denied          %s · %s · %s · %s\n",
		strings.Join(v.DenyWords, " · "), strings.Join(v.DenyPrefixes, " · "),
		strings.Join(v.DenyExact, " · "), strings.Join(v.DenyLoader, " · "))
	fmt.Fprintf(w, "sweep           %s (from %s)\n", v.ProvisionIdleTTL, v.ProvisionIdleTTLFrom)
	if v.ProvisionIdleTTLNote != "" {
		fmt.Fprintf(w, "                %s\n", v.ProvisionIdleTTLNote)
	}
	fmt.Fprintf(w, "honesty         %s\n", v.Honesty)
	fmt.Fprintf(w, "redaction       %s\n", v.RedactionLimits)
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/protocol"
)

// blueprint.go — `aeon blueprint`'s dispatch, the two verbs whose answer is
// not one row-backed call (fields, validate), and the retired one (path).
// S48 T11 (BP12, BP13, P34) put every verb on the socket door
// (blueprint_client.go); the management verbs are in blueprint_verbs.go and
// the editor round-trip in blueprint_edit.go.
//
// The CLI re-validates NOTHING. Flag parsing refuses only what cannot be a
// value; every other refusal is the daemon's sentence or report, verbatim.

func cmdBlueprint(w io.Writer, args []string) error {
	if len(args) == 0 {
		printBlueprintUsage(w)
		return nil
	}
	verb := args[0]
	// One selection per invocation, whichever arm answers: the offline door
	// prints its fixed line once, to stderr, before any output.
	needDoor := func() (blueprintDoor, error) { return selectBlueprintDoor(blueprintErrOut) }
	if isBlueprintDoorVerb(verb) {
		if verb == "lint" && blueprintWantsFile(args[1:]) {
			// `lint --file` needs no door at all — the spec's line, and the
			// reason this arm is before the selection: a door chosen here
			// would print "applied directly to the store" about a store the
			// file path never opens. nil is not a placeholder; the file branch
			// of runBlueprintLint returns before it touches the door.
			return runBlueprintLint(w, nil, args[1:])
		}
		door, err := needDoor()
		if err != nil {
			return err
		}
		return runBlueprintDoorVerb(w, door, verb, args[1:])
	}
	switch verb {
	case "fields":
		door, err := needDoor()
		if err != nil {
			return err
		}
		return runBlueprintFields(w, door, args[1:])
	case "validate":
		return cmdBlueprintValidate(w, args[1:])
	case "path":
		return retiredBlueprintPath(args)
	case "edit":
		door, err := needDoor()
		if err != nil {
			return err
		}
		return runBlueprintEdit(w, door, args[1:])
	case "export":
		door, err := needDoor()
		if err != nil {
			return err
		}
		return runBlueprintExport(w, door, args[1:])
	default:
		printBlueprintUsage(w)
		return fmt.Errorf("unknown blueprint subcommand %q", args[0])
	}
}

// blueprintDoorVerbs are the verbs that speak exactly one socket method each
// — the parity table's left column, pinned against the live method table in
// both directions by blueprint_parity_test.go. `fields`, `validate`, `edit`,
// `export` and the retired `path` are handled in cmdBlueprint itself because
// their shape is not one row-backed call: fields answers from code, validate
// may need no store at all, edit and export are COMPOSITIONS, and `path`
// speaks nothing at all.
func blueprintDoorVerbs() []string {
	return []string{"list", "show", "save", "duplicate", "delete", "revert", "override", "history", "diff", "usage", "lint"}
}

func isBlueprintDoorVerb(verb string) bool {
	for _, v := range blueprintDoorVerbs() {
		if v == verb {
			return true
		}
	}
	return false
}

// blueprintVerbMethods is the parity fixture — T0's CLI parity table restated
// as code, so the guard compares the CLI to the live method table instead of
// to prose. `edit` and `export` are compositions
// (blueprintCompositionMethods); `revision` is a method with no verb of its
// own (`history` indexes it, `diff` reads through it, the UI uses it
// directly), which the verb→method direction must not mistake for a gap.
func blueprintVerbMethods() map[string]protocol.MethodName {
	return map[string]protocol.MethodName{
		"list":      protocol.MethodBlueprintList,
		"show":      protocol.MethodBlueprintShow,
		"fields":    protocol.MethodBlueprintFields,
		"validate":  protocol.MethodBlueprintValidate,
		"save":      protocol.MethodBlueprintSave,
		"duplicate": protocol.MethodBlueprintDuplicate,
		"delete":    protocol.MethodBlueprintDelete,
		"revert":    protocol.MethodBlueprintRevert,
		"override":  protocol.MethodBlueprintOverride,
		"usage":     protocol.MethodBlueprintUsage,
		"history":   protocol.MethodBlueprintHistory,
		"diff":      protocol.MethodBlueprintDiff,
		"lint":      protocol.MethodBlueprintLint,
	}
}

// blueprintCompositionMethods names the two verbs that are NOT one call, and
// the methods they are built from. A verb is legal only if it is one method
// or one of these.
func blueprintCompositionMethods() map[string][]protocol.MethodName {
	return map[string][]protocol.MethodName{
		// show + save — through the one write path, never a file write.
		"edit": {protocol.MethodBlueprintShow, protocol.MethodBlueprintSave},
		// list and/or show per name, printing bytes somewhere nobody reads
		// back.
		"export": {protocol.MethodBlueprintList, protocol.MethodBlueprintShow},
	}
}

// blueprintUnmethodedList is the guard's explicit list of blueprint methods
// that NO verb speaks, so the reverse direction reports a gap instead of
// inventing a verb: `blueprint.propose` is the agent's door (T10) and a
// person never proposes to themselves. The three form doors (model, render,
// vocabulary) are the app's translation between a flow's text and the structure
// its builder edits; the CLI edits the text itself, so it has nothing to say with them.
func blueprintUnmethodedList() []protocol.MethodName {
	return []protocol.MethodName{
		protocol.MethodBlueprintRevision,
		protocol.MethodBlueprintPropose,
		protocol.MethodBlueprintModel,
		protocol.MethodBlueprintRender,
		protocol.MethodBlueprintVocabulary,
	}
}

func printBlueprintUsage(w io.Writer) {
	fmt.Fprint(w, `aeon blueprint — the database is the source; a file is a transport

  blueprint list [--origin system|user] [--q TEXT] [--json]
  blueprint show NAME [--json]
  blueprint fields [--json]
  blueprint validate [NAME | --file F] [--mode simple|standard|poc] [--strict] [--json]
  blueprint save NAME --file F [--create-only] [--mode M] [--note N]
  blueprint edit NAME [--note N]                     ($EDITOR → save)
  blueprint duplicate FROM NAME [--note N]
  blueprint delete NAME --yes
  blueprint revert NAME --rev N --yes [--note N]
  blueprint override NAME LANE FIELD [VALUE]         (a shipped flow's one open choice; no VALUE clears it)
  blueprint history NAME [--limit N]
  blueprint diff NAME [--a REV|current|shipped] [--b REV|current|shipped]
  blueprint usage NAME [--json]
  blueprint lint [NAME | --file F] [--json]
  blueprint export [NAME ...] [--out DIR]            (a disposable mirror)
  blueprint path NAME                                (retired: blueprints live in the database)

With no daemon running the same code applies directly to the store, and says so.
`)
}

// retiredBlueprintPath is the retirement of the `path` verb (BP12; T0's
// parity table's documented intentional change): a filesystem path no longer
// means anything, because the row is the source and the file was always a
// mirror. The sentence names the two doors that DO mean something, and a
// person who typed a verb as the NAME gets told it is a verb.
func retiredBlueprintPath(args []string) error {
	if len(args) == 2 && (args[1] == "export" || args[1] == "edit") {
		return fmt.Errorf("blueprints live in the database — use `aeon blueprint export` for a file copy or `aeon blueprint edit NAME` to change one (there is no path to %q: it is a verb, not a blueprint)", args[1])
	}
	return fmt.Errorf("blueprints live in the database — use `aeon blueprint export` for a file copy or `aeon blueprint edit NAME` to change one")
}

// blueprintBoolFlags are the blueprint verbs' flags that carry no value. The
// splitter below needs to know which names take one so a value flag followed
// by a position still takes its value (`--note one more`), while a boolean
// does not eat the next word (`--yes two-step` is a flag and a name).
var blueprintBoolFlags = map[string]bool{"json": true, "strict": true, "yes": true, "create-only": true}

// blueprintParseFlags parses a verb's flags from WHEREVER they appear and
// hands back the positional arguments. Go's flag package stops at the first
// positional, and every one of these verbs puts a NAME first —
// `aeon blueprint save NAME --file F` is the documented spelling, and a
// person types the thing they mean before its modifiers. Flags are gathered
// first; an unknown or malformed flag still goes through flag.Parse, so the
// refusal is the flag package's own words and `-h` still prints the verb's
// usage. The CLI adds no judgment of its own here.
func blueprintParseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var flagArgs, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			positional = append(positional, a)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !hasValue && !blueprintBoolFlags[name] && i+1 < len(args) {
			value, hasValue = args[i+1], true
			i++
		}
		if hasValue {
			flagArgs = append(flagArgs, "-"+name+"="+value)
		} else {
			flagArgs = append(flagArgs, "-"+name)
		}
	}
	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}
	return positional, nil
}

// joinOps renders a field's operator vocabulary for `fields` (unchanged from
// T3 — TestValidateAndFieldsOutputUnchanged pins these bytes).
func joinOps(values []blueprint.Operator) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return strings.Join(out, ",")
}

// blueprintErrOut is where the blueprint verbs' SIDE-CHANNEL lines go: the
// offline door's fixed notice, the offline file tools' note, a kept temp
// file's path. A var so a test can read exactly what a person sees; the
// binary writes to os.Stderr. The two doors' parity test still runs the real
// binary and strips this channel's one permitted line.
var blueprintErrOut io.Writer = os.Stderr

// blueprintFieldsValue is the payload BOTH doors answer `fields` with: the
// field vocabulary is code, not rows (the daemon's seam says the same).
func blueprintFieldsValue() any { return blueprint.Fields() }

// runBlueprintFields prints the vocabulary through the door — the daemon's
// bytes, decoded only to lay the table out, never re-validated.
func runBlueprintFields(w io.Writer, door blueprintDoor, args []string) error {
	fs := flag.NewFlagSet("blueprint fields", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the vocabulary as JSON")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("blueprint fields takes no arguments")
	}
	raw, err := door.fields(context.Background())
	if err != nil {
		return err
	}
	if *asJSON {
		return printBlueprintRawJSON(w, raw)
	}
	var ans struct {
		Fields []blueprint.Field `json:"fields"`
	}
	if err := json.Unmarshal(raw, &ans); err != nil {
		return fmt.Errorf("the fields answer could not be read: %w", err)
	}
	for _, f := range ans.Fields {
		fmt.Fprintf(w, "%-18s %-10s %-24s %s\n", f.Path, f.Type, joinOps(f.Operators), f.Doc)
	}
	return nil
}

// printBlueprintRawJSON pretty-prints a door's raw answer. It re-formats the
// DAEMON's bytes and adds nothing to them.
func printBlueprintRawJSON(w io.Writer, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return fmt.Errorf("the answer could not be read: %w", err)
	}
	_, err := fmt.Fprintln(w, buf.String())
	return err
}

// installBlueprintSourceForCLI points blueprint.Get/Names at the store when
// one is reachable (the justified direct-open, WAL-serialised behind the
// daemon's writer — the `aeon policy` shape); with none, the tool is offline
// and installs the embedded source by name. Returns the close (or a no-op).
//
// S48 T11 leaves it ONE caller — the offline file tools (validate --file,
// lint --file): every row-backed verb goes through the door now, so this is
// the only place the CLI swaps the source, which is how T8's guard reads it.
func installBlueprintSourceForCLI() func() {
	if s, err := openStoreDirectly(false); err == nil {
		// The same system seed the daemon applies at arrival, run here too:
		// a store that never met a daemon is EMPTY (idempotent, never
		// overwrites a row a person wrote — BP10). A failed seed means the
		// store is not trustworthy to read: fall back to nothing.
		if err := daemon.SeedStandardBlueprints(s); err == nil {
			daemon.InstallBlueprintSource(s)
			return func() { _ = s.Close() }
		}
		_ = s.Close()
	}
	blueprint.UseEmbeddedForOfflineTools()
	return func() {}
}

// --- validate ----------------------------------------------------------------------

// readBlueprintFileArg reads a person's own file — an ARGUMENT, never the
// state mirror. T11 drops the legacy resolver from this file entirely: a path
// is a path, and a mirror has nothing to resolve against.
func readBlueprintFileArg(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return raw, nil
}

// blueprintOfflineOptions is what the no-store file tools analyze with: the
// SHIPPED agents (embeddedSource is the offline tool's only honest authority
// about which agents exist), resolvers and actions.
func blueprintOfflineOptions(mode string) blueprint.ValidationOptions {
	return blueprint.ValidationOptions{
		Mode:       mode,
		Agents:     handler.Names(),
		Resolvers:  handler.Resolvers(),
		Actions:    action.Names(),
		AgentTools: handler.ToolsFor,
	}
}

// blueprintOfflineFileNotice is said on stderr before the report, so a person
// never mistakes a shipped-agent verdict for the daemon's (T3's offline-tools
// honesty, kept through the door rewrite).
const blueprintOfflineFileNotice = "no daemon and no store read — validated against the shipped agent set"

func cmdBlueprintValidate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("blueprint validate", flag.ContinueOnError)
	fs.SetOutput(w)
	strict := fs.Bool("strict", false, "fail on unverifiable references as well as invalid ones")
	mode := fs.String("mode", "standard", "validation mode: simple, standard or poc")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	file := fs.String("file", "", "validate this file instead of a stored blueprint")
	rest, err := blueprintParseFlags(fs, args)
	if err != nil {
		return err
	}
	if *mode != "simple" && *mode != "standard" && *mode != "poc" {
		return fmt.Errorf("unknown mode %q; accepted: simple, standard, poc", *mode)
	}
	if *file == "" && len(rest) == 0 {
		return fmt.Errorf("usage: aeon blueprint validate [flags] NAME | --file PATH")
	}
	if *file != "" && len(rest) != 0 {
		return fmt.Errorf("validate takes a NAME or --file PATH, not both")
	}
	if *file == "" && len(rest) == 1 && looksLikePath(rest[0]) {
		// A path argument IS --file (T3's accepted spelling, kept): a person
		// typing a path means a file.
		*file = rest[0]
	}
	if *file != "" {
		return validateBlueprintFile(w, *file, *mode, *strict, *asJSON)
	}
	if len(rest) != 1 {
		return fmt.Errorf("validate takes exactly one blueprint name")
	}
	door, err := selectBlueprintDoor(blueprintErrOut)
	if err != nil {
		return err
	}
	raw, err := door.validate(context.Background(), protocol.BlueprintValidateParams{Name: rest[0], Mode: *mode})
	if err != nil {
		return err
	}
	return printBlueprintReport(w, raw, *strict, *asJSON)
}

// looksLikePath is the one client-side judgment the CLI keeps: whether an
// argument can even BE a blueprint name. A name never contains a separator,
// and a .yaml/.yml suffix is a file by spelling.
// blueprintWantsFile reports whether a `lint` command line names a FILE
// rather than a stored blueprint, without deciding anything about it: the
// same two shapes the verb itself accepts (`--file PATH`, or a bare argument
// that looks like a path).
func blueprintWantsFile(args []string) bool {
	for i, a := range args {
		switch {
		case a == "--file" || a == "-file":
			return i+1 < len(args) && args[i+1] != ""
		case strings.HasPrefix(a, "--file="), strings.HasPrefix(a, "-file="):
			return strings.TrimLeft(strings.TrimPrefix(strings.TrimPrefix(a, "--"), "-"), "file=") != ""
		case !strings.HasPrefix(a, "-") && looksLikePath(a):
			return true
		}
	}
	return false
}

func looksLikePath(arg string) bool {
	return strings.ContainsAny(arg, "/\\") || strings.HasSuffix(arg, ".yaml") || strings.HasSuffix(arg, ".yml")
}

// validateBlueprintFile is the offline file tool: no daemon, no store, no
// rows — Analyze over the shipped agent set, printed through the ONE report
// renderer the door's answer uses too (BP4).
func validateBlueprintFile(w io.Writer, path, mode string, strict, asJSON bool) error {
	raw, err := readBlueprintFileArg(path)
	if err != nil {
		return err
	}
	fmt.Fprintln(blueprintErrOut, blueprintOfflineFileNotice)
	cleanup := installBlueprintSourceForCLI()
	defer cleanup()
	report := blueprint.Analyze(raw, blueprintOfflineOptions(mode))
	encoded, err := json.Marshal(report)
	if err != nil {
		return err
	}
	return printBlueprintReport(w, encoded, strict, asJSON)
}

// printBlueprintReport is the ONE report renderer (T3's bytes): the door's
// report or the offline Analyze's — the same lines either way.
func printBlueprintReport(w io.Writer, raw json.RawMessage, strict, asJSON bool) error {
	var report blueprint.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		return fmt.Errorf("the report could not be read: %w", err)
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		renderBlueprintReport(w, report) // the ONE renderer, shared with show
	}
	if !report.OK(strict) {
		return fmt.Errorf("blueprint validation failed")
	}
	return nil
}

package main

// secretref_verbs.go — S59 T8 (EV7, §4c, P34): `aeon secretref`, the seven
// secret-reference doors at the terminal.
//
// The whole point of this file is what it CANNOT do: **a secret value has no
// path in**. `create` takes a LOCATION — `--from-env VAR` names a variable the
// daemon already holds, `--from-file PATH` names a file it can read — and
// `update` moves the pointer or edits the note. There is no `--value`, no
// `--secret-value`, no `--from-stdin`, no interactive prompt in this file, and
// the wire type has no field for one either (EV7, §4c: a credential goes IN and
// never comes OUT, enforced by having two types rather than one redaction).
// `TestSecretRefCreateTakesALocationNeverAValue` walks the flag sets AND greps
// these two files for a value-shaped flag, a stdin read or a prompt, so the
// property survives the next person adding a flag here.
//
// Like `env_verbs.go` this is a **pure client**: one table naming the socket
// methods, one dial, no direct door, no `internal/store` import, no re-validation,
// one renderer (`renderError`) and one printer for `--json` (`printJSON`). A
// refusal from the daemon — a credential-shaped name, a `store` kind that is
// staged but not open, a source that is not readable — is printed verbatim.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// secretrefSourceFlags is the closed pair of locations a person may point a
// reference at from the terminal, and the only place the CLI maps a flag to a
// `sourceKind`. `store` is deliberately absent: that kind's door refuses with a
// sentence until the vault lands (D3), so a flag for it would promise a door
// that answers nothing.
var secretrefSourceFlags = []struct {
	flag  string
	kind  string
	usage string
}{
	{flag: "from-env", kind: "daemon-env", usage: "the NAME of a variable the daemon holds (a daemon credential is refused by name, EV11)"},
	{flag: "from-file", kind: "key-file", usage: "a path the daemon can read"},
}

// secretrefVerbs is the seven doors, in the order the usage prints them. The
// plane has no agent-only method on this half: `secretref_list` is an AGENT tool
// (S59 T7) but its DOOR is the person's read the CLI is a client of, so all
// seven have verbs and the parity test accounts for the tool side separately.
var secretrefVerbs = []envVerb{
	{name: "list", method: protocol.MethodSecretRefList},
	{name: "show", method: protocol.MethodSecretRefShow},
	{name: "check", method: protocol.MethodSecretRefCheck},
	{name: "create", method: protocol.MethodSecretRefCreate},
	{name: "update", method: protocol.MethodSecretRefUpdate},
	{name: "archive", method: protocol.MethodSecretRefArchive},
	{name: "restore", method: protocol.MethodSecretRefRestore},
}

// cmdSecretRef is the dispatcher: `aeon secretref <verb> …`. One door for the
// whole plane (`selectEnvDoor`) — the socket does not care which half of the
// plane a method is on, and a second dial would be a second truth about who is
// calling.
func cmdSecretRef(w io.Writer, args []string) error {
	if len(args) == 0 {
		return secretrefUsage(w)
	}
	verb, rest := args[0], args[1:]
	for _, v := range secretrefVerbs {
		if v.name != verb {
			continue
		}
		return v.runSecretRef(w, rest)
	}
	fmt.Fprintf(w, "aeon secretref: unknown verb %q\n\n", verb)
	return secretrefUsage(w)
}

func secretrefUsage(w io.Writer) error {
	fmt.Fprintln(w, "usage: aeon secretref <verb> [flags]")
	fmt.Fprintln(w, "")
	for _, v := range secretrefVerbs {
		fmt.Fprintf(w, "  %-9s → %s [--json]\n", v.name, v.method)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  create names where a value LIVES, never the value:")
	for _, s := range secretrefSourceFlags {
		fmt.Fprintf(w, "    --%s (%s) — %s\n", s.flag, s.kind, s.usage)
	}
	fmt.Fprintln(w, "  There is no flag, stdin path or prompt that takes a secret value (EV7, §4c).")
	return nil
}

func (v envVerb) runSecretRef(w io.Writer, args []string) error {
	door, err := selectEnvDoor()
	if err != nil {
		return err
	}
	defer func() { _ = door.close() }()
	ctx := context.Background()

	switch v.name {
	case "list":
		return secretrefList(w, ctx, door, args)
	case "show":
		return secretrefShow(w, ctx, door, args)
	case "check":
		return secretrefCheck(w, ctx, door, args)
	case "create":
		return secretrefCreate(w, ctx, door, args)
	case "update":
		return secretrefUpdate(w, ctx, door, args)
	case "archive", "restore":
		return secretrefArchiveRestore(w, ctx, door, v, args)
	}
	return fmt.Errorf("aeon secretref %s: no implementation (the verb table and the runner have drifted)", v.name)
}

// --- the reads -------------------------------------------------------------------

func secretrefList(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("secretref list", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	archived := fs.Bool("archived", false, "include archived references too")
	limit := fs.Int("limit", 0, "at most this many rows")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p := protocol.SecretRefListParams{IncludeArchived: *archived, Limit: *limit}
	var rows []protocol.SecretRefView
	if err := door.relay(protocol.MethodSecretRefList, p, &rows); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, rows, func() {
		if len(rows) == 0 {
			fmt.Fprintln(w, "no secret references")
			return
		}
		for _, r := range rows {
			fmt.Fprintf(w, "%-26s → %-22s %s%s\n", r.Name, r.InjectAs, r.SourceKind, secretrefArchivedMark(r))
		}
	})
}

func secretrefShow(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("secretref show", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	if err := envParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("aeon secretref show needs exactly one reference: <id|name>")
	}
	p := protocol.SecretRefShowParams{Name: fs.Arg(0)}
	var out protocol.SecretRefView
	if err := door.relay(protocol.MethodSecretRefShow, p, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() { secretrefPrintView(w, out) })
}

func secretrefCheck(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("secretref check", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	if err := envParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("aeon secretref check needs exactly one reference: <id|name>")
	}
	p := protocol.SecretRefCheckParams{Name: fs.Arg(0)}
	var out protocol.SecretRefCheckResult
	if err := door.relay(protocol.MethodSecretRefCheck, p, &out); err != nil {
		return err
	}
	// The answer is two booleans and the resolver's own sentence. `sourcePresent`
	// is the three-state part — absent is not empty — and it is printed as the
	// daemon spoke it, because "the token was not injected" is diagnosed there.
	return envPrint(w, *jsonOut, out, func() {
		fmt.Fprintf(w, "%s  %s  resolvable=%t  sourcePresent=%t\n", out.Name, out.SourceKind, out.Resolvable, out.SourcePresent)
		if out.RefusalSentence != "" {
			fmt.Fprintf(w, "  %s\n", out.RefusalSentence)
		}
	})
}

// --- the writes -------------------------------------------------------------------

func secretrefCreate(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("secretref create", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	name := fs.String("name", "", "the reference's name (required)")
	injectAs := fs.String("inject-as", "", "the variable NAME it injects as (required)")
	desc := fs.String("description", "", "what it is for")
	fromEnv := fs.String("from-env", "", secretrefSourceFlags[0].usage)
	fromFile := fs.String("from-file", "", secretrefSourceFlags[1].usage)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *injectAs == "" {
		return fmt.Errorf("aeon secretref create needs --name and --inject-as (and one of --from-env / --from-file — a location, never a value)")
	}
	kind, source, err := secretrefLocation(*fromEnv, *fromFile)
	if err != nil {
		return err
	}
	p := protocol.SecretRefCreateParams{Name: *name, Description: *desc, SourceKind: kind, Source: source, InjectAs: *injectAs}
	var out protocol.SecretRefView
	if err := door.relay(protocol.MethodSecretRefCreate, p, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() { secretrefPrintView(w, out) })
}

func secretrefUpdate(w io.Writer, ctx context.Context, door envDoor, args []string) error {
	fs := flag.NewFlagSet("secretref update", flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	injectAs := fs.String("inject-as", "", "inject as this variable NAME instead")
	desc := fs.String("description", "", "replace the note")
	if err := envParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("aeon secretref update needs the reference: aeon secretref update <id|name> [--inject-as VAR] [--description D]")
	}
	// The wire's nil pointers ARE the three state: a flag not given is "leave
	// that field alone", which is what lets a person edit a note without ever
	// re-presenting a source they were never shown (§4c).
	p := protocol.SecretRefUpdateParams{Name: fs.Arg(0)}
	if *injectAs != "" {
		p.InjectAs = injectAs
	}
	if *desc != "" {
		p.Description = desc
	}
	var out protocol.SecretRefView
	if err := door.relay(protocol.MethodSecretRefUpdate, p, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() { secretrefPrintView(w, out) })
}

func secretrefArchiveRestore(w io.Writer, ctx context.Context, door envDoor, v envVerb, args []string) error {
	fs := flag.NewFlagSet("secretref "+v.name, flag.ContinueOnError)
	fs.SetOutput(w)
	jsonOut := envJSONFlag(fs)
	if err := envParse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("aeon secretref %s needs exactly one reference: <id|name>", v.name)
	}
	p := protocol.SecretRefNameParams{Name: fs.Arg(0)}
	var out protocol.SecretRefView
	if err := door.relay(v.method, p, &out); err != nil {
		return err
	}
	return envPrint(w, *jsonOut, out, func() { secretrefPrintView(w, out) })
}

// secretrefLocation is the ONLY mapping from a CLI flag to a `sourceKind`, and
// it takes no value: each branch carries a NAME or a PATH. Exactly one of the
// two may be given — a reference points at one place.
func secretrefLocation(fromEnv, fromFile string) (kind, source string, err error) {
	if fromEnv != "" && fromFile != "" {
		return "", "", fmt.Errorf("aeon secretref takes --from-env OR --from-file, never both — a reference points at one place")
	}
	for _, s := range secretrefSourceFlags {
		switch s.flag {
		case "from-env":
			if fromEnv != "" {
				return s.kind, fromEnv, nil
			}
		case "from-file":
			if fromFile != "" {
				return s.kind, fromFile, nil
			}
		}
	}
	return "", "", fmt.Errorf("aeon secretref create needs where the value lives: --from-env VAR or --from-file PATH (a LOCATION — there is no flag, stdin path or prompt that takes a secret value, EV7)")
}

// --- the human lines ---------------------------------------------------------------

func secretrefArchivedMark(r protocol.SecretRefView) string {
	if r.ArchivedAt != nil {
		return "  (archived)"
	}
	return ""
}

// secretrefPrintView prints what a reference IS: a pointer, and the booleans
// about it. `source` is printed here and nowhere else on the CLI, because the
// person's door answers the person — and even then it is a NAME or a PATH, the
// two shapes this command can put in.
func secretrefPrintView(w io.Writer, r protocol.SecretRefView) {
	fmt.Fprintf(w, "%s  → %s\n", r.Name, r.InjectAs)
	fmt.Fprintf(w, "  source    %s: %s%s\n", r.SourceKind, r.Source, secretrefArchivedMark(r))
	if r.Description != "" {
		fmt.Fprintf(w, "  note      %s\n", r.Description)
	}
	fmt.Fprintf(w, "  origin    %s  rev %s\n", r.Origin, r.UpdatedAt)
	if len(r.ReferencedBy) > 0 {
		fmt.Fprintf(w, "  used by   %s\n", strings.Join(r.ReferencedBy, " · "))
	}
}

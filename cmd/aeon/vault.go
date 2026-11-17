package main

// vault.go — `aeon vault`: the variable manager at the terminal. A client of the
// daemon's `vault.*` doors (P34), never the store.
//
// A value never travels on a command line: argv is the process list and the
// shell history (C17). `set` reads it from standard input — hidden, when that is
// a terminal; the whole stream, when it is a pipe. A secret is never printed
// back: `list` shows its name, scope and flags, not what it says.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/cjlapao/aeon/internal/protocol"
)

const vaultUsage = `usage:
  aeon vault list
  aeon vault set NAME [--project ROOT_ID] [--description TEXT] [--plain] [--agent-access yes|no]
       the value is read from standard input (hidden at a terminal, the whole stream from a pipe);
       a NEW variable is a secret hidden from agents unless you say otherwise; omit input to keep
       the stored value and change only the flags
  aeon vault unset NAME [--project ROOT_ID]
  aeon vault changes [NAME]
`

func cmdVault(w io.Writer, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(w, vaultUsage)
		return nil
	}
	switch args[0] {
	case "list":
		return cmdVaultList(w, args[1:])
	case "set":
		return cmdVaultSet(w, args[1:], os.Stdin)
	case "unset":
		return cmdVaultUnset(w, args[1:])
	case "changes":
		return cmdVaultChanges(w, args[1:])
	default:
		fmt.Fprint(w, vaultUsage)
		return fmt.Errorf("unknown `vault` subcommand %q", args[0])
	}
}

func cmdVaultList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("vault list", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res protocol.VaultListResult
	if err := c.Call(protocol.MethodVaultList, map[string]any{}, &res); err != nil {
		return err
	}
	if len(res.Variables) == 0 {
		fmt.Fprintln(w, "no variables — `aeon vault set NAME` adds one")
		return nil
	}
	for _, v := range res.Variables {
		scope := "global"
		if v.Project != "" {
			scope = "project " + firstNonEmpty(v.ProjectName, v.Project)
		}
		kind, agents := "plain ", "agents: no"
		if v.Secret {
			kind = "secret"
		}
		if v.AgentVisible {
			agents = "agents: YES"
		}
		shown := "(hidden)"
		if !v.Secret {
			shown = v.Value
		}
		fmt.Fprintf(w, "%-28s %s  %-18s %-12s %s", v.Name, kind, scope, agents, shown)
		if v.Description != "" {
			fmt.Fprintf(w, "  — %s", v.Description)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func cmdVaultSet(w io.Writer, args []string, in *os.File) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("vault set takes the variable name first, then flags")
	}
	name := args[0]
	fs := flag.NewFlagSet("vault set", flag.ContinueOnError)
	fs.SetOutput(w)
	var (
		project = fs.String("project", "", "a ROOT component id: this value applies to that project only, overriding the global one")
		desc    = fs.String("description", "", "what the variable is for")
		plain   = fs.Bool("plain", false, "NOT a secret: its value is shown back and a new one is agent-visible by default")
		access  = fs.String("agent-access", "", "yes: agents may read it (env_get); no: they may not; omit to keep the stored setting (a new secret starts as no)")
	)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *access != "" && *access != "yes" && *access != "no" {
		return fmt.Errorf("--agent-access is yes or no, not %q", *access)
	}
	p := protocol.VaultSetParams{Name: name, Project: *project}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if given["description"] {
		p.Description = desc
	}
	if given["plain"] {
		secret := !*plain
		p.Secret = &secret
	}
	if *access != "" {
		visible := *access == "yes"
		p.AgentVisible = &visible
	}
	value, err := readVaultValue(in, w)
	if err != nil {
		return err
	}
	if value != "" {
		p.Value = &value
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res protocol.VaultSetResult
	if err := c.Call(protocol.MethodVaultSet, p, &res); err != nil {
		return err
	}
	v := res.Variable
	fmt.Fprintf(w, "%s saved (%s, agents: %s)\n", v.Name, map[bool]string{true: "secret", false: "plain"}[v.Secret], map[bool]string{true: "can read it", false: "cannot read it"}[v.AgentVisible])
	return nil
}

// readVaultValue takes the value from standard input: hidden at a terminal, the
// whole stream (one trailing newline trimmed) from a pipe, nothing from /dev/null.
func readVaultValue(in *os.File, w io.Writer) (string, error) {
	if term.IsTerminal(int(in.Fd())) {
		fmt.Fprint(os.Stderr, "Value (hidden; empty keeps the stored one): ")
		raw, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read the value: %w", err)
		}
		return string(raw), nil
	}
	raw, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read the value from standard input: %w", err)
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r"), nil
}

func cmdVaultUnset(w io.Writer, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("vault unset takes the variable name first")
	}
	fs := flag.NewFlagSet("vault unset", flag.ContinueOnError)
	fs.SetOutput(w)
	project := fs.String("project", "", "the project override to remove; omit for the global value")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res protocol.VaultDeleteResult
	if err := c.Call(protocol.MethodVaultDelete, protocol.VaultDeleteParams{Name: args[0], Project: *project}, &res); err != nil {
		return err
	}
	if !res.Deleted {
		fmt.Fprintf(w, "%s: nothing to remove in that scope\n", args[0])
		return nil
	}
	fmt.Fprintf(w, "%s removed\n", args[0])
	return nil
}

func cmdVaultChanges(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("vault changes", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	p := protocol.VaultChangesParams{}
	if fs.NArg() > 0 {
		p.Name = fs.Arg(0)
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res protocol.VaultChangesResult
	if err := c.Call(protocol.MethodVaultChanges, p, &res); err != nil {
		return err
	}
	if len(res.Changes) == 0 {
		fmt.Fprintln(w, "no changes yet")
		return nil
	}
	for _, ch := range res.Changes {
		fmt.Fprintf(w, "%s  %-10s %s:%s  %s\n", ch.OccurredAt, ch.Op, ch.ActorKind, ch.ActorID, ch.Summary)
	}
	return nil
}

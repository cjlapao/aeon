package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/cjlapao/aeon/internal/action"
)

// cmdAction is the S16 read of the code-action governance: `aeon action
// check <name>` prints an action's class and the verdict it produces, so
// the two governance routes can be compared side by side (§3's demo):
//
//	aeon policy why --type command_access --agent github-steward "gh pr merge 15"   # → ask
//	aeon action check forge.merge                                                    # → ask (irreversible)
//
// The line is rendered by action.CheckAnswer — the ONE renderer — and
// printed verbatim, because a second phrasing of a verdict is a second
// truth. `action list` arrives with the registry (T10).
func cmdAction(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("aeon action list | aeon action check <action>")
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("list takes no arguments — it prints the closed set")
		}
		fmt.Fprintln(w, ActionTable())
		return nil
	case "check":
		if len(args) != 2 {
			return fmt.Errorf("check takes exactly one action name: aeon action check forge.merge")
		}
		line, err := action.CheckAnswer(args[1])
		if err != nil {
			return err
		}
		fmt.Fprintln(w, line)
		return nil
	default:
		return fmt.Errorf("unknown verb %q — S16 carries `action list` and `action check`", args[0])
	}
}

// ActionTable renders the closed registry: name, effect class, strategy,
// required args. One renderer (the one-formatter rule), so the CLI and
// any surface that needs the table print the SAME bytes.
func ActionTable() string {
	out := "ACTION                 CLASS          STRATEGY              ARGS (required: *)\n"
	for _, name := range action.Names() {
		e, _ := action.Lookup(name)
		entry := fmt.Sprintf("%-22s %-14s %-21s", name, classOf(name), action.Strategy(name))
		first := true
		for _, field := range sortedFields(e) {
			spec := e.Args[field]
			marker := " "
			if spec.Required {
				marker = "*"
			}
			if first {
				entry += fmt.Sprintf("%s%s(%s)", marker, field, spec.Source)
				first = false
			} else {
				entry += fmt.Sprintf(" %s%s(%s)", marker, field, spec.Source)
			}
		}
		if first {
			entry += "—"
		}
		out += entry + "\n"
	}
	return out
}

// classOf reads the class from the same closed table Check answers
// from — never a second declaration (one truth per fact).
func classOf(name string) string {
	r, err := action.Check(name)
	if err != nil {
		return "unknown"
	}
	return string(r.Class)
}

func sortedFields(e action.Entry) []string {
	fs := make([]string, 0, len(e.Args))
	for f := range e.Args {
		fs = append(fs, f)
	}
	sort.Strings(fs)
	return fs
}

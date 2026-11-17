package main

// component_verbs.go — S44-extra T8: the terminal's component door (P34: the
// socket is primary, the CLI is its client). Nine verbs, one c.Call each, no
// store import (CT13's recorded deviation — a verb here dials the socket, it
// never opens the store). What the wire says is what the terminal prints; a
// refusal the daemon makes is a refusal the terminal restates verbatim.

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdComponent(w io.Writer, args []string) error {
	if len(args) == 0 {
		printComponentUsage(w)
		return nil
	}
	switch args[0] {
	case "list":
		return cmdComponentList(w, args[1:])
	case "show":
		return cmdComponentShow(w, args[1:])
	case "create":
		return cmdComponentCreate(w, args[1:])
	case "update":
		return cmdComponentUpdate(w, args[1:])
	case "promote":
		return cmdComponentPromote(w, args[1:])
	case "attach":
		return cmdComponentAttach(w, args[1:])
	case "detach":
		return cmdComponentDetach(w, args[1:])
	case "archive":
		return cmdComponentArchive(w, args[1:])
	case "restore":
		return cmdComponentRestore(w, args[1:])
	default:
		printComponentUsage(w)
		return fmt.Errorf("unknown `component` subcommand %q", args[0])
	}
}

func printComponentUsage(w io.Writer) {
	fmt.Fprint(w, `aeon component — the components tree (S44-extra); the socket is the door, this is its client

  component list [--json]            the tree: roots, children indented
  component show ID [--json]         one component
  component create --name N --slug S --root R [--parent ID] [--short-description D]
                                     [--description D] [--default-branch B] [--branch-prefix P]
                                     [--max-parallel-agents N]
  component update ID [--name N] [--slug S] [--short-description D] [--description D]
                 [--default-branch B] [--branch-prefix P] [--max-parallel-agents N]
  component promote ID [--name N] [--short-description D] [--description D] [--icon I]
  component attach ID --parent ID
  component detach ID
  component archive ID
  component restore ID

A component is a node; a root with children is a project (S44-extra). There is
no `+"`project`"+` verb — a project is a root component, and the tree moves by
promote / attach / detach. Every verb dials the daemon; none opens the store.
`+"`--json`"+` prints the wire result verbatim.
`)
}

// printJSON is in settings.go — the one --json printer (verbatim wire result).

func cmdComponentList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("component list", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result as JSON")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.ComponentListResult
	if err := c.Call(protocol.MethodName("component.list"), map[string]any{}, &res); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	if len(res.Components) == 0 {
		fmt.Fprintln(w, "no components — `aeon component create --name N --slug S --root R` files one")
		return nil
	}
	roots := make([]protocol.ComponentView, 0)
	children := map[string][]protocol.ComponentView{}
	for _, cv := range res.Components {
		if cv.ParentID == "" {
			roots = append(roots, cv)
		} else {
			children[cv.ParentID] = append(children[cv.ParentID], cv)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Slug < roots[j].Slug })
	for _, r := range roots {
		kids := children[r.ID]
		marker := " "
		if len(kids) > 0 {
			marker = "project"
		}
		fmt.Fprintf(w, "%-10s %-22s %s\n", marker, r.Name, treeMeta(r))
		for _, k := range sortedChildren(kids) {
			fmt.Fprintf(w, "    └ %-22s %s\n", k.Name, treeMeta(k))
		}
	}
	return nil
}

// treeMeta is the human line's tail: the slug and the effective settings a root
// shows (a child's settings inherit, so the daemon is the source of truth; the
// CLI prints the resolved values it was handed, never a second rules pass).
func treeMeta(cv protocol.ComponentView) string {
	parts := []string{cv.Slug}
	if cv.Root != "" {
		parts = append(parts, "root="+cv.Root)
	}
	if cv.DefaultBranch != "" {
		parts = append(parts, "branch="+cv.DefaultBranch)
	}
	if cv.MaxParallelAgents > 0 {
		parts = append(parts, fmt.Sprintf("parallel=%d", cv.MaxParallelAgents))
	}
	return strings.Join(parts, "  ")
}

func sortedChildren(kids []protocol.ComponentView) []protocol.ComponentView {
	out := append([]protocol.ComponentView(nil), kids...)
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

func cmdComponentShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("component show", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result as JSON")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id, err := oneID(fs)
	if err != nil {
		return err
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res protocol.ComponentView
	if err := c.Call(protocol.MethodName("component.show"), protocol.ComponentIDParams{ID: id}, &res); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "%s  (%s)\n", res.Name, res.Slug)
	if res.ShortDescription != "" {
		fmt.Fprintf(w, "  %s\n", res.ShortDescription)
	}
	if res.Description != "" {
		fmt.Fprintf(w, "  %s\n", res.Description)
	}
	if res.ParentID != "" {
		fmt.Fprintf(w, "  child of %s\n", res.ParentID)
	}
	fmt.Fprintf(w, "  %s\n", treeMeta(res))
	return nil
}

func cmdComponentCreate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("component create", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result as JSON")
	name := fs.String("name", "", "the component's name")
	slug := fs.String("slug", "", "the component's slug")
	root := fs.String("root", "", "the workspace root")
	parent := fs.String("parent", "", "file as a child of this component (empty makes a root)")
	short := fs.String("short-description", "", "a one-line description")
	body := fs.String("description", "", "the prose description")
	db := fs.String("default-branch", "", "the default branch")
	bp := fs.String("branch-prefix", "", "the branch prefix")
	par := fs.Int("max-parallel-agents", 0, "the parallel-agent cap (1..16; 0 keeps the default)")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *slug == "" || *root == "" {
		return fmt.Errorf("component create needs --name, --slug and --root")
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	in := protocol.ComponentCreateParams{Name: *name, Slug: *slug, Root: *root,
		ParentID: *parent, ShortDescription: *short, Description: *body,
		DefaultBranch: *db, BranchPrefix: *bp, MaxParallelAgents: *par}
	var res protocol.ComponentView
	if err := c.Call(protocol.MethodName("component.create"), in, &res); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "created %s  (%s)\n", res.Name, res.Slug)
	return nil
}

func cmdComponentUpdate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("component update", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result as JSON")
	short := fs.String("short-description", "", "the one-line description")
	body := fs.String("description", "", "the prose description")
	db := fs.String("default-branch", "", "the default branch")
	bp := fs.String("branch-prefix", "", "the branch prefix")
	par := fs.Int("max-parallel-agents", 0, "the parallel-agent cap (1..16; 0 keeps the existing)")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id, err := oneID(fs)
	if err != nil {
		return err
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	in := protocol.ComponentUpdateParams{ID: id,
		ShortDescription: *short, Description: *body,
		DefaultBranch: *db, BranchPrefix: *bp, MaxParallelAgents: *par}
	var res protocol.ComponentView
	if err := c.Call(protocol.MethodName("component.update"), in, &res); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "updated %s  (%s)\n", res.Name, res.Slug)
	return nil
}

func cmdComponentPromote(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("component promote", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result as JSON")
	name := fs.String("name", "", "the new root's name (empty keeps current)")
	short := fs.String("short-description", "", "the new root's one-liner (empty keeps current)")
	body := fs.String("description", "", "the new root's prose (empty keeps current)")
	icon := fs.String("icon", "", "the new root's icon (empty keeps current)")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id, err := oneID(fs)
	if err != nil {
		return err
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	in := protocol.ComponentPromoteParams{ID: id, Name: *name,
		ShortDescription: *short, Description: *body, Icon: *icon}
	var res protocol.ComponentView
	if err := c.Call(protocol.MethodName("component.promote"), in, &res); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "promoted %s to a project  (%s)\n", res.Name, res.Slug)
	return nil
}

func cmdComponentAttach(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("component attach", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result as JSON")
	parent := fs.String("parent", "", "the root to make it a child of")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id, err := oneID(fs)
	if err != nil {
		return err
	}
	if *parent == "" {
		return fmt.Errorf("component attach needs --parent")
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	in := protocol.ComponentAttachParams{ID: id, ParentID: *parent}
	var res protocol.ComponentView
	if err := c.Call(protocol.MethodName("component.attach"), in, &res); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "attached %s to %s\n", res.Name, *parent)
	return nil
}

func cmdComponentDetach(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("component detach", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result as JSON")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id, err := oneID(fs)
	if err != nil {
		return err
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res protocol.ComponentView
	if err := c.Call(protocol.MethodName("component.detach"), protocol.ComponentIDParams{ID: id}, &res); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "detached %s (now a root)\n", res.Name)
	return nil
}

func cmdComponentArchive(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("component archive", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result as JSON")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id, err := oneID(fs)
	if err != nil {
		return err
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res protocol.ComponentView
	if err := c.Call(protocol.MethodName("component.archive"), protocol.ComponentIDParams{ID: id}, &res); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "archived %s (hidden, not deleted)\n", res.Name)
	return nil
}

func cmdComponentRestore(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("component restore", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result as JSON")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id, err := oneID(fs)
	if err != nil {
		return err
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res protocol.ComponentView
	if err := c.Call(protocol.MethodName("component.restore"), protocol.ComponentIDParams{ID: id}, &res); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "restored %s\n", res.Name)
	return nil
}

// oneID is the shared positional-id door: exactly one component id, trimmed.
func oneID(fs *flag.FlagSet) (string, error) {
	pos := fs.Args()
	if len(pos) != 1 {
		return "", fmt.Errorf("exactly one component id is required")
	}
	return strings.TrimSpace(pos[0]), nil
}

// componentRedirectLine is the one CT13 sentence the gone `project` doors
// restate — `aeon project` (main.go) and `aeon telemetry project` (telemetry.go)
// print it verbatim and exit 2, so the operator reads the same redirect on
// every door that used to mean "project".
func componentRedirectLine() string {
	return "aeon project is gone — a project is a root component; use `aeon component` (S44-extra)"
}

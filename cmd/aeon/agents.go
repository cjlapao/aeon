package main

// agents.go — S43 T8 (P34's secondary client): the agent-management CLI verbs.
//
// The daemon is the primary surface (P34): every agent-management method first
// exists as a method on the daemon's own socket, and the app renders it. This
// file is a SECONDARY client of that same surface — it dials the daemon and
// calls the methods by name through the generic c.Call bridge, so it is a
// thin re-render, never a second implementation of the import/export logic.
//
// It is a NEW command, `aeon agents`, because `aeon agent import` is S15's
// plugin import (a direct store write, no socket door) and `import` is taken.
// A capability that exists ONLY as a CLI verb is not accepted (P34); these
// verbs are the secondary door over the daemon's methods, not the only door.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// cmdAgents is the `aeon agents` command: the secondary CLI client of the
// agent-management methods. It dispatches import/export/list/view over the
// daemon's socket (c.Call), and prints the daemon's answer verbatim — a
// refusal is the daemon's refusal, never a re-spelling here.
func cmdAgents(w io.Writer, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(w, "usage: aeon agents <import|export|list|view> …")
		return nil
	}
	switch args[0] {
	case "import":
		return cmdAgentsImport(w, args[1:])
	case "export":
		return cmdAgentsExport(w, args[1:])
	case "list":
		return cmdAgentsList(w)
	case "view":
		return cmdAgentsView(w, args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(w, "usage: aeon agents <import|export|list|view> …")
		return nil
	default:
		return fmt.Errorf("refused: unknown agents subcommand %q — import/export/list/view", args[0])
	}
}

// cmdAgentsImport imports a card file through the daemon's agent.import
// method (AM3's (slug, version) idempotency). The path is the only argument;
// nothing is fetched (a URL is refused by the daemon, AP10).
func cmdAgentsImport(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("agents import", flag.ContinueOnError)
	fs.SetOutput(w)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	path := strings.TrimSpace(fs.Arg(0))
	if path == "" {
		return fmt.Errorf("refused: import takes exactly ONE argument — the local path of a card file")
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	var res importResult
	if err := c.Call(protocol.MethodAgentImport, struct {
		Path string `json:"path"`
	}{Path: path}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "imported %s (%s)\n", res.Agent.Slug, res.Result)
	return nil
}

// cmdAgentsExport renders an agent's card through the daemon's agent.export
// method. With --out PATH the daemon writes the card to the file; without it
// the card JSON is returned and printed.
func cmdAgentsExport(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("agents export", flag.ContinueOnError)
	fs.SetOutput(w)
	out := fs.String("out", "", "write the card to this path (default: print it)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	slug := strings.TrimSpace(fs.Arg(0))
	if slug == "" {
		return fmt.Errorf("refused: export takes a slug as its first argument")
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	// The daemon returns a single string: the path written when --out is set,
	// otherwise the card JSON itself (the secondary client renders either).
	var res string
	if err := c.Call(protocol.MethodAgentExport, struct {
		Slug string `json:"slug"`
		Path string `json:"path"`
	}{Slug: slug, Path: *out}, &res); err != nil {
		return err
	}
	if *out != "" {
		fmt.Fprintf(w, "exported %s → %s\n", slug, res)
		return nil
	}
	fmt.Fprintln(w, res)
	return nil
}

// cmdAgentsList lists the agents the daemon's agent.list method returns — the
// roster (identity: name, provenance, state), read-only.
func cmdAgentsList(w io.Writer) error {
	c, err := dial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	var res struct {
		Agents []struct {
			Name       string `json:"name"`
			Provenance string `json:"provenance"`
			State      string `json:"state"`
			Source     string `json:"source"`
		} `json:"agents"`
	}
	if err := c.Call(protocol.MethodAgentList, nil, &res); err != nil {
		return err
	}
	if len(res.Agents) == 0 {
		fmt.Fprintln(w, "no agents — zero imports is the normal state")
		return nil
	}
	for _, a := range res.Agents {
		fmt.Fprintf(w, "%-24s %-12s %s\n", a.Name, a.State, a.Provenance)
	}
	return nil
}

// cmdAgentsView renders one agent's full row through the daemon's agent.view
// method (by slug). It is the read the settings list uses for the detail the
// roster does not carry (slug, version, signed, internal, avatar).
func cmdAgentsView(w io.Writer, args []string) error {
	slug := strings.TrimSpace(args[0])
	if slug == "" {
		return fmt.Errorf("refused: view takes a slug as its first argument")
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	var res storeAgent
	if err := c.Call(protocol.MethodAgentView, struct {
		Slug string `json:"slug"`
	}{Slug: slug}, &res); err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

// storeAgent mirrors the store.Agent the daemon's agent.view returns, for the
// secondary client's own rendering — the daemon is the source of truth, this
// is only the shape the CLI prints.
type storeAgent struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Functionality string `json:"functionality"`
	Version       string `json:"version"`
	Internal      bool   `json:"internal"`
	AvatarType    string `json:"avatar_type"`
	AvatarColor   string `json:"avatar_color"`
	AvatarMood    string `json:"avatar_mood"`
	Signed        bool   `json:"signed"`
}

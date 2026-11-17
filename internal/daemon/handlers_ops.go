package daemon

// The handlers' socket reads for the bench demo runner (spec
// 2026-10-25-bench-demo-runner). No store guard: the registry is embedded,
// so a storeless daemon still answers — this is the terminals precedent (a
// read whose door is not the store). Verify reports; it writes nothing, and
// originals are reachable by NAME only, with the answer carrying no path:
// the daemon knows where opencode/ is, the socket answer never says so.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/protocol"
)

// originalName is a bare basename, nothing else: no separators, no dots, no
// uppercase. A caller cannot point this method at a file, only ask about a
// named original.
var originalName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func (d *Daemon) initHandlerOps() {
	d.Table.HandlersList = func(ctx context.Context, p protocol.HandlersListParams) (any, error) {
		var out []protocol.HandlerView
		for _, name := range handler.Names() {
			kind := "body"
			if handler.Preset(name) {
				kind = "preset"
			} else if !isShippedBody(name) {
				kind = "custom" // an agent a person made or imported
			}
			var tools, tech []string
			if h, ok := handler.Lookup(name); ok {
				tools = h.Tools
			}
			if d, rej := handler.EmbeddedDefinition(name); rej == nil {
				tech = d.Tech
			}
			out = append(out, protocol.HandlerView{Name: name, Kind: kind, Tools: tools, Tech: tech})
		}
		var techTags []protocol.TechTagView
		for _, t := range handler.TechVocabulary() {
			techTags = append(techTags, protocol.TechTagView{Tag: t.Tag, Label: t.Label, Group: t.Group})
		}
		var toolKits []protocol.ToolKitView
		for _, k := range handler.ToolKits() {
			toolKits = append(toolKits, protocol.ToolKitView{ID: k.ID, Label: k.Label, Description: k.Description, Effect: k.Effect, Tools: k.Tools})
		}
		return protocol.HandlersListView{Handlers: out, Resolvers: handler.Resolvers(), TechTags: techTags, ToolKits: toolKits}, nil
	}

	d.Table.HandlersVerify = func(ctx context.Context, p protocol.HandlersVerifyParams) (any, error) {
		switch p.Scope {
		case "corpus":
			view := protocol.HandlersVerifyView{Scope: "corpus"}
			for _, name := range handler.RegisteredBodies() {
				def, rej := handler.LoadDefinition(name)
				if rej != nil {
					view.Findings = append(view.Findings, protocol.HandlersFindingView{
						Handler: name, Kind: "front-matter", Ref: "front matter", Msg: rej.Error(),
					})
					continue
				}
				findings := handler.Verify(def, nil)
				if len(findings) == 0 {
					view.Passed++
					continue
				}
				for _, f := range findings {
					view.Findings = append(view.Findings, protocol.HandlersFindingView{
						Handler: f.Handler, Kind: f.Kind, Ref: f.Ref, Msg: f.Msg,
					})
				}
			}
			view.Refused = len(view.Findings) > 0
			return view, nil

		case "original":
			if !originalName.MatchString(p.Name) {
				return nil, protocol.ErrBadParams{Err: fmt.Errorf(
					"original name %q is not a bare basename — lowercase letters, digits and dashes, never a path; this method verifies named originals, not files you point it at", p.Name)}
			}
			dir, ok := originalsDir()
			if !ok {
				return protocol.HandlersVerifyView{Scope: "original", Name: p.Name, Reason: &protocol.HandlersRefusalView{
					Kind:   "originals-unavailable",
					Detail: "the originals ship with the source tree, not the daemon — set AEON_OPENCODE_DIR to the directory of original bodies (or run where the tree is above you) to verify a negative control from here",
				}}, nil
			}
			candidate := filepath.Join(dir, p.Name+".md")
			if _, err := os.Stat(candidate); err != nil {
				var names []string
				if ents, rerr := os.ReadDir(dir); rerr == nil {
					for _, e := range ents {
						if n := e.Name(); strings.HasSuffix(n, ".md") {
							names = append(names, strings.TrimSuffix(n, ".md"))
						}
					}
				}
				return protocol.HandlersVerifyView{Scope: "original", Name: p.Name, Reason: &protocol.HandlersRefusalView{
					Kind:   "unknown-name",
					Detail: fmt.Sprintf("no original is named %q — available: %s", p.Name, strings.Join(names, ", ")),
				}}, nil
			}
			findings, _ := handler.VerifyFile(candidate, true, nil)
			view := protocol.HandlersVerifyView{Scope: "original", Name: p.Name, Refused: len(findings) > 0}
			for _, f := range findings {
				// The basename the CALLER asked for, always: the answer never
				// carries a path, and never a name the caller did not request.
				view.Findings = append(view.Findings, protocol.HandlersFindingView{
					Handler: p.Name, Kind: f.Kind, Ref: f.Ref, Msg: f.Msg,
				})
			}
			return view, nil
		}
		return nil, protocol.ErrBadParams{Err: fmt.Errorf("scope %q names no sweep", p.Scope)}
	}
}

// originalsDir resolves the negative-control directory: AEON_OPENCODE_DIR
// when set (a directory or the answer is an honest unavailable), else the
// first `opencode/agents` walking up from the working directory — the same
// convention the CLI's bodies discovery uses.
func originalsDir() (string, bool) {
	if env := strings.TrimSpace(os.Getenv("AEON_OPENCODE_DIR")); env != "" {
		if st, err := os.Stat(env); err == nil && st.IsDir() {
			return env, true
		}
		return "", false
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(dir, "opencode", "agents")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// isShippedBody reports whether a name is one of the code's own bodies.
func isShippedBody(name string) bool {
	for _, b := range handler.EmbeddedBodies() {
		if b == name {
			return true
		}
	}
	return false
}

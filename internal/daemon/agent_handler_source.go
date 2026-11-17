package daemon

import (
	"sort"

	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/store"
)

// agent_handler_source.go — custom agents join dispatch.
//
// The shipped agents are served as they always were, from the code's bodies (the embedded source),
// and nothing here changes how they are looked up. An agent a person made or imported lives in the
// `agent` table, and this source adds it: a name the shipped set does not have is looked up in the
// table, built from its row through the same guards (handler.FromRow), and dispatched like any
// other. The shipped name always wins, so a custom agent can never shadow a first-party one.
//
// What stands between a custom agent and a tool call is policy — the rules written when its tools
// were chosen — exactly as for the shipped agents. There is no cache: every read goes to the store,
// so an edit is the next step's agent.

// agentRowSource is handler.Source: the embedded (shipped) source, plus the custom rows.
type agentRowSource struct {
	shipped handler.Source
	s       *store.Store
}

// InstallAgentHandlerSource makes the store's custom agents dispatchable. Last installer wins (as
// the blueprint source does), so a process that builds many daemons reads through the last one
// built; production installs once.
func InstallAgentHandlerSource(s *store.Store) {
	handler.ReplaceSource(agentRowSource{shipped: handler.EmbeddedSource(), s: s})
}

func agentViewOf(a store.Agent) handler.AgentView {
	return handler.AgentView{
		Slug: a.Slug, Name: a.Name, Description: a.Description, Prompt: a.Prompt, Tools: a.Tools,
		Temperature: a.Temperature, Handoff: a.Handoff, PolicyAgent: a.PolicyAgent, Origin: a.Origin,
	}
}

// custom reads one custom row; a shipped row, or none, is not one.
func (x agentRowSource) custom(name string) (store.Agent, bool) {
	a, found, err := x.s.AgentBySlug(name)
	if err != nil || !found || a.Internal || a.Origin == "system" {
		return store.Agent{}, false
	}
	return a, true
}

func (x agentRowSource) Lookup(name string) (handler.Handler, bool) {
	if h, ok := x.shipped.Lookup(name); ok {
		return h, true
	}
	a, ok := x.custom(name)
	if !ok {
		return handler.Handler{}, false
	}
	h, rej := handler.FromRow(agentViewOf(a))
	if rej != nil {
		return handler.Handler{}, false
	}
	return h, true
}

func (x agentRowSource) Definition(name string) (handler.Definition, *handler.Reject) {
	if d, rej := x.shipped.Definition(name); rej == nil {
		return d, nil
	}
	a, ok := x.custom(name)
	if !ok {
		return x.shipped.Definition(name)
	}
	return handler.DefinitionFromRow(agentViewOf(a)), nil
}

// Names are the shipped names, then the custom ones, each group in its own order.
func (x agentRowSource) Names() []string {
	out := x.shipped.Names()
	slugs, err := x.s.CustomAgentSlugs()
	if err != nil {
		return out
	}
	have := make(map[string]bool, len(out))
	for _, n := range out {
		have[n] = true
	}
	var custom []string
	for _, slug := range slugs {
		if !have[slug] {
			custom = append(custom, slug)
		}
	}
	sort.Strings(custom)
	return append(out, custom...)
}

// Bodies and Preset are about the shipped set alone: the seeder and the corpus sweeps read them.
func (x agentRowSource) Bodies() []string        { return x.shipped.Bodies() }
func (x agentRowSource) Preset(name string) bool { return x.shipped.Preset(name) }

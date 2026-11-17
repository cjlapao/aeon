package protocol

// The door computation (S47 T6, TC8): every door flag is READ from the structure
// that owns it, never declared beside it. The socket door is the method table;
// the public door is `!Debug`; the agent-service door is `AgentServiceMethods()`;
// the tool twin is the records catalog's own `Method` field; and the narrow door
// arrives through `Table.NarrowMethods`, a func seam, because `internal/narrow`
// imports this package and this package must never import it back.

import (
	"sort"

	"github.com/cjlapao/aeon/internal/records"
)

// The narrow door's three answers. `unknown` is a value and not an absence:
// a daemon that did not wire the narrow seam has not said the method is absent
// from it, and a screen that renders the difference is a screen that cannot
// mislead. `SurfaceNarrowNone` is the answer a wired daemon gives for a method
// the nine actions do not project.
const (
	SurfaceNarrowNone    = "none"
	SurfaceNarrowUnknown = "unknown"
)

// SurfaceDoors is who can reach a method, as the live structures say.
type SurfaceDoors struct {
	// Socket says the method is registered on this daemon's table right now —
	// the same fact a `-32601` is built from, read from the same map.
	Socket bool `json:"socket"`
	// Public says S19's HTTP and MCP-stdio projections may mount it: `!Debug`,
	// computed from the entry and not from a remembered list.
	Public bool `json:"public"`
	// Narrow is "none", "unknown", or the narrow action that projects it —
	// "unknown" only when the daemon never wired `NarrowMethods`.
	Narrow string `json:"narrow"`
	// AgentService says the name is in the agent-to-agent enumeration.
	AgentService bool `json:"agentService"`
	// ToolTwin names the catalog action whose `Method` is this method, or is
	// empty when the method has no agent-facing twin.
	ToolTwin string `json:"toolTwin,omitempty"`
}

// SurfaceEntry is one method as a person sees it: the declared classification
// beside the doors the structures actually compute.
type SurfaceEntry struct {
	Method             MethodName   `json:"method"`
	Kind               SurfaceKind  `json:"kind"`
	Summary            string       `json:"summary"`
	HumanDoorOnly      bool         `json:"humanDoorOnly"`
	RequiresInitialize bool         `json:"requiresInitialize"`
	Debug              bool         `json:"debug"`
	Doors              SurfaceDoors `json:"doors"`
}

// Surface answers "what does this daemon expose, and to whom" for every method
// the table holds right now, in name order.
//
// It is a method on the Table and not a package function because the doors are
// facts about THIS daemon: a conditional family its seam never wired is not in
// the answer (its table entry simply has no socket door to report, and the
// completeness tests allow an entry for a conditional method precisely so a
// fully wired daemon can list it).
func (t *Table) Surface() []SurfaceEntry {
	table := surfaceTable()
	services := map[MethodName]bool{}
	for _, m := range AgentServiceMethods() {
		services[m.Name] = true
	}
	twins := map[MethodName]string{}
	for _, spec := range records.Catalog() {
		if spec.Method != "" {
			twins[MethodName(spec.Method)] = spec.Name
		}
	}
	var narrow map[MethodName]string
	narrowKnown := t.NarrowMethods != nil
	if narrowKnown {
		narrow = t.NarrowMethods()
	}

	registered := map[MethodName]bool{}
	for _, n := range t.Methods() {
		registered[n] = true
	}

	names := make([]MethodName, 0, len(registered))
	for n := range registered {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })

	out := make([]SurfaceEntry, 0, len(names))
	for _, n := range names {
		m, ok := t.Lookup(n)
		if !ok {
			continue
		}
		s, classified := table[n]
		door := SurfaceDoors{
			Socket:       true,
			Public:       !m.Debug,
			Narrow:       SurfaceNarrowNone,
			AgentService: services[n],
			ToolTwin:     twins[n],
		}
		if !narrowKnown {
			door.Narrow = SurfaceNarrowUnknown
		} else if act, projected := narrow[n]; projected {
			door.Narrow = act
		}
		_ = classified // an unclassified method is listed with the zero classification and the completeness test says so by name
		out = append(out, SurfaceEntry{
			Method:             n,
			Kind:               s.Kind,
			Summary:            s.Summary,
			HumanDoorOnly:      s.HumanDoorOnly,
			RequiresInitialize: m.RequiresInitialize,
			Debug:              m.Debug,
			Doors:              door,
		})
	}
	return out
}

// surfaceClassify answers the classification for one name, for the tests and for
// `surface.get`'s refusal path; the second value says whether the table holds it.
func surfaceClassify(name MethodName) (Surface, bool) {
	s, ok := surfaceTable()[name]
	return s, ok
}

// surfaceTwinOf reports the catalog action whose socket twin is this method, and
// whether this method is one of the named propose-only actions.
func surfaceTwinOf(name MethodName) (string, bool) {
	for _, spec := range records.Catalog() {
		if spec.Method == string(name) {
			return spec.Name, true
		}
	}
	return "", false
}

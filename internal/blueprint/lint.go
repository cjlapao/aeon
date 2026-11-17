package blueprint

// lint.go — S48 T2 (BP12/BP14): lint advisories. A blueprint's model steps
// file entities through catalog tools; when the step's agent is NOT granted
// one of those tools today, lint says so — as an ADVISORY, never a verdict.
// `Analyze` keeps its monopoly on validity (a lint advisory never fails a
// save, never enters a Report, never gates a start): grants change without
// YAML edits, so a hard gate would make yesterday's valid blueprint refuse
// to open today. The grant answer comes through a small interface so this
// package stays policy-free and tools-free (the daemon wires it to
// toolcatalog.Effective, T7); the clock is injected — `at` rendered RFC3339
// UTC so the advisory says WHEN it spoke.

import (
	"time"
)

// ToolState answers one grant question: what is `tool`'s state for `agent`?
// ok false is the honest third state — unknown agent or unknown tool — and
// lint then says NOTHING (unknown-agent is Analyze's line to speak, and an
// unknown tool never came from this table). The daemon answers from
// toolcatalog.Effective (S47's one function), rendering state/reason in its
// own words; lint repeats the reason VERBATIM.
type ToolState interface {
	State(agent, tool string) (state, reason string, ok bool)
}

// Advisory is one lint finding: which lane/step, which agent's which tool,
// the daemon's state word and reason verbatim, when it was evaluated, and
// its kind (today one kind, "grant"; the field is the seam for more).
type Advisory struct {
	Lane        string `json:"lane"`
	Step        string `json:"step"`
	Agent       string `json:"agent"`
	Tool        string `json:"tool"`
	State       string `json:"state"`
	Reason      string `json:"reason"`
	EvaluatedAt string `json:"evaluatedAt"`
	Kind        string `json:"kind"`
}

// The state words that produce an advisory. "allowed" and "ask" (and
// anything unrecognized) do not: lint warns about tools the agent CANNOT
// use today, and an ask is a human door, not an absence.
var lintAdvisoryStates = map[string]bool{
	"granted_denied":  true,
	"granted_no_rule": true,
	"not_granted":     true,
}

// Lint walks every model step with an agent and asks the ToolState for each
// tool its `emits` implies (entity → the catalog tool through the SAME
// emitTool table Analyze's OL9 check uses — one table, two readers — plus
// "attach" when it pins files). It never returns an error and never touches
// a Report; order is lanes, steps, emits, attach.
func Lint(bp *Blueprint, ts ToolState, at time.Time) []Advisory {
	if bp == nil || ts == nil {
		return nil
	}
	evaluatedAt := at.UTC().Format(time.RFC3339)
	var out []Advisory
	for _, l := range bp.Lanes {
		for _, s := range laneStepSlice(l) {
			if s.Kind != ModelStep || s.Agent == "" || len(s.Emits) == 0 {
				continue
			}
			step := s.ID
			if step == "" {
				step = l.ID
			}
			var tools []string
			for _, em := range s.Emits {
				if tool, ok := emitTool[em.Type]; ok {
					tools = append(tools, tool)
				}
			}
			for _, em := range s.Emits {
				if len(em.Attach) > 0 {
					tools = append(tools, "attach")
					break
				}
			}
			for _, tool := range tools {
				state, reason, ok := ts.State(s.Agent, tool)
				if !ok || !lintAdvisoryStates[state] {
					continue
				}
				out = append(out, Advisory{
					Lane: l.ID, Step: step, Agent: s.Agent, Tool: tool,
					State: state, Reason: reason, EvaluatedAt: evaluatedAt, Kind: "grant",
				})
			}
		}
	}
	return out
}

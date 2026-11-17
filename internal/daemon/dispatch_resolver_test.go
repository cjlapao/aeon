package daemon

// T10's dispatch seam, tested where it lives: a blueprint step may name the
// closed resolver `by-tech-tag`, and ONLY the daemon maps that name to a
// registered handler — or refuses. The refusal is the loop's block reason
// verbatim; nothing generic dispatches in its place (A13).

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/loop"
)

// shipped is the agents as the seed declares them — the same tags a live store
// carries after the seed applies.
func shipped(t *testing.T) func() ([]handler.TechAgent, error) {
	return func() ([]handler.TechAgent, error) {
		var out []handler.TechAgent
		for _, n := range handler.EmbeddedBodies() {
			d, rej := handler.EmbeddedDefinition(n)
			if rej != nil {
				t.Fatalf("%s: %s", n, rej.Reason)
			}
			if len(d.Tech) > 0 {
				out = append(out, handler.TechAgent{Slug: n, Tech: d.Tech})
			}
		}
		return out, nil
	}
}

func TestResolveDispatchAgent(t *testing.T) {
	resolverStep := blueprint.Step{Kind: blueprint.ModelStep, Agent: "by-tech-tag"}
	plainStep := blueprint.Step{Kind: blueprint.ModelStep, Agent: "golang-engineer"}
	if got, err := resolveDispatchAgent(loop.LoopState{}, plainStep, shipped(t)); err != nil || got != "golang-engineer" {
		t.Errorf("a registered name was disturbed: %q, %v — the resolver is consulted only for its own registered name", got, err)
	}
	for tag, want := range map[string]string{
		"go": "golang-engineer", "python": "python-engineer", "rust": "rust-engineer",
		"react-ts": "react-typescript-engineer", "websocket": "websocket-engineer",
	} {
		got, err := resolveDispatchAgent(loop.LoopState{RuntimeState: loop.RuntimeState{With: map[string]string{"tech": tag}}}, resolverStep, shipped(t))
		if err != nil || got != want {
			t.Errorf("tech %q resolved to %q, %v — want %q", tag, got, err, want)
		}
	}
	st := loop.LoopState{RuntimeState: loop.RuntimeState{With: map[string]string{"tech": "elixir"}}}
	_, err := resolveDispatchAgent(st, resolverStep, shipped(t))
	if err == nil {
		t.Fatal("elixir dispatched a handler — A13's no-fallback rule is what §2 of the demo walks")
	}
	if !strings.Contains(err.Error(), `tech "elixir"`) || !strings.Contains(err.Error(), "available:") || !strings.Contains(err.Error(), "go") {
		t.Errorf("refusal does not name the tag and list available tags: %v", err)
	}
	// And NOTHING resolves for a plain registered agent, even with a tech set:
	// the tag belongs to the resolver alone.
	if got, err := resolveDispatchAgent(loop.LoopState{RuntimeState: loop.RuntimeState{With: map[string]string{"tech": "elixir"}}}, plainStep, shipped(t)); err != nil || got != "golang-engineer" {
		t.Errorf("an unrelated step was rerouted by a tech tag: %q, %v", got, err)
	}
	// The resolver name itself is never a dispatchable handler (it is not in
	// the body registry) — retry/classifier paths must not treat it as one.
	if _, ok := handler.Lookup("by-tech-tag"); ok {
		t.Error("by-tech-tag registered as a handler — a resolver is not a handler")
	}
}

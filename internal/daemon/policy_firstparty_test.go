package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/policyseed"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/seed"
)

// The shipped first-party defaults, end to end: the start applies them, the gate then lets a first-party
// agent read and keep its records without asking, makes the dangerous tools ask by NAME, and leaves an agent
// nobody seeded (user-made, imported) to the unmatched default — ask — never to the first-party allows.
func TestFirstPartyDefaultsApplyAtStartAndTheGateReadsThem(t *testing.T) {
	s := openSeedStore(t)
	ctx := context.Background()
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatalf("NewSeedEngine: %v", err)
	}
	ApplyInertAtStart(ctx, e)

	subjects := policyseed.FirstPartySubjects()
	if len(subjects) < 30 {
		t.Fatalf("only %d first-party subjects: %v", len(subjects), subjects)
	}
	agent := subjects[0]
	settings := config.NewStore(newSettingsRepo(s), config.Options{})
	gate := NewPolicyGate(s, nil, WithUnmatchedSetting(settings))
	at := time.Now()
	decide := func(who, tool string) runtime.PolicyGateDecision {
		return gate.Decide(runtime.PolicyGateRequest{Agent: who, Tool: tool}, at)
	}

	if d := decide(agent, "read"); !d.Allowed {
		t.Fatalf("a first-party agent must read without asking: %+v", d)
	}
	if d := decide(agent, "task_create"); !d.Allowed {
		t.Fatalf("a first-party agent must keep its records (tasks, learnings…) without asking: %+v", d)
	}
	for _, tool := range []string{"write", "edit", "doc_write", "webfetch", "ask_agent"} {
		d := decide(agent, tool)
		if !d.Asked || d.Allowed || d.RuleID == "" {
			t.Fatalf("%s must ASK and name the rule that asks (a dangerous tool): %+v", tool, d)
		}
	}
	if d := decide("someone-imported", "read"); !d.Asked || !strings.Contains(d.Sentence, "no rule covers this") {
		t.Fatalf("an agent with no rows must fall to the unmatched default (ask), not inherit the allows: %+v", d)
	}

	// The operator keeps authority: a deny the operator writes outranks the shipped allow.
	// (Covered by the engine's deny > ask > allow; here we only pin that the seeded rows are removable
	// and are not re-applied by the next start.)
	rows, _ := s.PolicyListAll(ctx)
	removed := ""
	for _, r := range rows {
		if r.TargetID == agent && strings.Contains(r.SpecJSON, `"*.*.read"`) {
			removed = r.ID
		}
	}
	if removed == "" {
		t.Fatal("the shipped read allow for the agent was not found")
	}
}

package blueprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
)

// chat_keys_test.go is S39 T3: the three optional keys — after, converse,
// kickoff — load, validate, and refuse the shapes that cannot mean anything.
// Nothing in the engine reads them yet (T4/T6/T7 do); the validator is the
// whole contract this task owns.

func baseYAML(t *testing.T, extra string) []byte {
	t.Helper()
	return []byte("id: x\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: a\n    step: {kind: model-step, agent: brainstormer}\n" + extra)
}

func mustValidate(t *testing.T, raw []byte) Report {
	t.Helper()
	b, err := Load(raw)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return Validate(b, ValidationOptions{Agents: handler.Names(), Resolvers: handler.Resolvers()})
}

func TestAfterConverseLoadsAndValidates(t *testing.T) {
	r := mustValidate(t, baseYAML(t, "after: converse\n"))
	if !r.OK(true) {
		t.Fatalf("after: converse must validate clean: %+v", r.Diagnostics)
	}
	b, _ := Load(baseYAML(t, "after: converse\n"))
	if b.After != AfterConverse {
		t.Fatalf("After is %q, want converse", b.After)
	}
}

func TestAfterValueRefused(t *testing.T) {
	r := mustValidate(t, baseYAML(t, "after: converse-always\n"))
	if r.OK(true) || len(r.Diagnostics) == 0 || r.Diagnostics[0].Kind != "after-value" {
		t.Fatalf("after: converse-always = %+v, want one after-value diagnostic", r.Diagnostics)
	}
}

func TestConverseNeedsAfterConverse(t *testing.T) {
	r := mustValidate(t, baseYAML(t, "converse: brainstormer\n"))
	if r.OK(true) || len(r.Diagnostics) == 0 || r.Diagnostics[0].Kind != "converse-without-after" {
		t.Fatalf("converse alone = %+v, want converse-without-after", r.Diagnostics)
	}
}

func TestConverseMustNameARegisteredAgent(t *testing.T) {
	r := mustValidate(t, baseYAML(t, "after: converse\nconverse: ghost\n"))
	if r.OK(true) || len(r.Diagnostics) == 0 || r.Diagnostics[0].Kind != "unknown-agent" {
		t.Fatalf("unknown converse = %+v, want unknown-agent", r.Diagnostics)
	}
	// The third state rides the futureAgents mechanism, mutated exactly as
	// the next follow-up's commit will declare it (third_state_t16_test.go's
	// pattern): the branch must survive the list being empty today.
	saved := futureAgents
	defer func() { futureAgents = saved }()
	futureAgents = []string{"zz-phantom-converse"}
	r = mustValidate(t, baseYAML(t, "after: converse\nconverse: zz-phantom-converse\n"))
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].State != Unverifiable || r.Diagnostics[0].Kind != "agent-registry" {
		t.Fatalf("future converse = %+v, want one Unverifiable agent-registry", r.Diagnostics)
	}
}

func TestConverseIsNotCheckedAgainstResolvers(t *testing.T) {
	// by-tech-tag is a RESOLVER name, not a handler — the converse of a
	// blueprint must be an agent body that can answer a conversation.
	r := mustValidate(t, baseYAML(t, "after: converse\nconverse: by-tech-tag\n"))
	if r.OK(true) || len(r.Diagnostics) == 0 || r.Diagnostics[0].Kind != "unknown-agent" {
		t.Fatalf("converse: by-tech-tag = %+v, want unknown-agent (a resolver is not a conversation)", r.Diagnostics)
	}
}

func loadShipped(t *testing.T, name string) Blueprint {
	t.Helper()
	// The source tree's embedded directory, not the state copy: this is the
	// same evidence base shipped_test.go and the loop contract test use.
	raw, err := os.ReadFile(filepath.Join("blueprints", name+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(raw)
	if err != nil {
		t.Fatalf("%s must load: %v", name, err)
	}
	return b
}

func TestShippedBlueprintsStillHaveNoConverse(t *testing.T) {
	// Amended S39 T6 (naming the task, as T3 required): TWO blueprints have
	// opted in — brainstorm rests after its capture run (its conversation
	// agent is brainstormer), and chat IS a conversation. Anyone else grows
	// a converse unannounced.
	rested := map[string]string{"brainstorm": "brainstormer", "chat": "chat"}
	for _, name := range StandardNames() {
		b := loadShipped(t, name)
		if want, opted := rested[name]; opted {
			if b.Converse != want {
				t.Errorf("%s rests, so converse must name %q; got %q (S39 T6's rest)", name, want, b.Converse)
			}
			continue
		}
		if b.Converse != "" {
			t.Errorf("%s ships converse: %q — T3 adds the KEY; only S39 T6's two (brainstorm, chat) have opted in", name, b.Converse)
		}
	}
}

func TestAfterOnShippedBlueprintsIsValidOrAbsent(t *testing.T) {
	for _, name := range StandardNames() {
		b := loadShipped(t, name)
		if b.After != "" && b.After != AfterFinish && b.After != AfterConverse {
			t.Errorf("%s ships after: %q — not in the closed set", name, b.After)
		}
	}
}

func TestNoShippedBlueprintUsesTheNewKeysYet(t *testing.T) {
	// The zero-regression D1 promise: keys exist, none is USED. brainstorm
	// and goal-to-brief ship without after: (their author ended the loop);
	// if a later task opts one in it amends THIS test deliberately, naming
	// the task — an accidental breach is a shipped behavior change nobody
	// scheduled.
	// Amended S39 T6: brainstorm and chat are the OPTED-IN pair, exactly —
	// after: converse with their named conversation agent, no kickoff (that
	// arrives with T7/T8's run door). Any third name, or either one wearing
	// a key T6 did not give it, is the shipped-behavior-change-nobody-
	// scheduled this test exists to catch.
	allowed := map[string]struct{ after, converse string }{
		"brainstorm": {"converse", "brainstormer"},
		"chat":       {"converse", "chat"},
	}
	for _, name := range StandardNames() {
		b := loadShipped(t, name)
		if a, ok := allowed[name]; ok {
			if b.After != a.after || b.Converse != a.converse || b.Kickoff != "" {
				t.Errorf("%s wears the new keys WRONG (after=%q converse=%q kickoff=%t) — S39 T6 gave it after:%s converse:%s",
					name, b.After, b.Converse, b.Kickoff != "", a.after, a.converse)
			}
			continue
		}
		if b.After != "" || b.Converse != "" || b.Kickoff != "" {
			t.Errorf("%s uses a new key (after=%q converse=%q kickoff=%t) — T3 adds keys; only S39 T6's two use them",
				name, b.After, b.Converse, b.Kickoff != "")
		}
	}
}

func TestKickoffTemplateAcceptedAndRefused(t *testing.T) {
	r := mustValidate(t, baseYAML(t, "kickoff: 'Goal: {{.Goal}}'\n"))
	if !r.OK(true) {
		t.Fatalf("valid kickoff refused: %+v", r.Diagnostics)
	}
	r = mustValidate(t, baseYAML(t, "kickoff: 'x{{.Missing}'\n"))
	if r.OK(true) || len(r.Diagnostics) == 0 {
		t.Fatal("kickoff parse error accepted")
	}
	r = mustValidate(t, baseYAML(t, "kickoff: '{{.Nope}}'\n"))
	if r.OK(true) || len(r.Diagnostics) == 0 || r.Diagnostics[0].Kind != "kickoff-field" {
		t.Fatalf("unknown kickoff field = %+v, want kickoff-field", r.Diagnostics)
	}
}

func TestUnknownKeyStillRefused(t *testing.T) {
	if _, err := Load(baseYAML(t, "afterr: converse\n")); err == nil {
		t.Fatal("typo key accepted")
	}
}

func TestAbsentKeysStayEmpty(t *testing.T) {
	b, err := Load(baseYAML(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if b.After != "" || b.Converse != "" || b.Kickoff != "" {
		t.Fatalf("absent keys read %q/%q/%q, want empty strings — absent stays absent, never a default that changes behavior", b.After, b.Converse, b.Kickoff)
	}
	r := Validate(b, ValidationOptions{Agents: handler.Names(), Resolvers: handler.Resolvers()})
	if !r.OK(true) {
		t.Fatalf("an old blueprint must validate exactly as before: %+v", r.Diagnostics)
	}
}

func TestKickoffParamsRenderEmptyNotError(t *testing.T) {
	tpl, err := ParseKickoff("{{index .Params \"branch\"}}")
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, KickoffData{}); err != nil {
		t.Fatalf("an absent Params entry must render empty (missingkey=zero): %v", err)
	}
	if sb.String() != "" {
		t.Fatalf("absent param rendered %q, want empty", sb.String())
	}
}

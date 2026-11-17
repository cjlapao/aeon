package protocol

// learnings_mine_test.go — S52 T4 (LM5, LM7, V13, rule 4): the memory
// plane's four agent-scoped twins. The mcp.mine family's shapes, carried:
// registration is conditional on the records seam (absent, never
// registered-and-refusing); dispatch is a pass-through to the SAME
// RecordsAPI.Do the tool door calls, bytes untouched and the refusal
// sentence verbatim; and the catalog's memory family cannot grow a door
// onto the person's plane — retract and restore have no twin at all, and
// TestMemoryFamilyCarriesNoRetractionDoor says so of the catalog itself.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/cjlapao/aeon/internal/records"
)

var mineTwinNames = []MethodName{
	MethodLearningsMineQuery,
	MethodLearningsMineShow,
	MethodLearningsMineAdd,
	MethodLearningsMineProposeRetract,
}

func TestLearningsMineRegistrationIsConditional(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tbl.RegisterLearningsMine() {
		t.Fatal("RegisterLearningsMine registered over a nil records seam — V13 says absent, not refusing")
	}
	for _, name := range mineTwinNames {
		if _, ok := tbl.Lookup(name); ok {
			t.Errorf("%s stands without the records service — the memory door was registered-and-refusing", name)
		}
		if _, err := tbl.Dispatch(context.Background(), "c1", name, json.RawMessage(`{}`)); !errors.As(err, new(ErrUnknownMethod)) {
			t.Errorf("dispatch of %s without the seam = %v, want ErrUnknownMethod", name, err)
		}
	}
	// NewTable itself must not project them either: the memory family is
	// skipped by the catalog loop, so the conditional register is the ONLY
	// door — an auto-projection would answer over an unwired plane with a
	// generic sentence where the plane's own refusal belongs.
	for _, name := range mineTwinNames {
		if _, ok := tbl.Lookup(name); ok {
			t.Errorf("%s was auto-projected by NewTable despite the memory-family skip", name)
		}
	}

	tbl.Records = &envProposeSpy{recordNameSpy: &recordNameSpy{}}
	if !tbl.RegisterLearningsMine() {
		t.Fatal("RegisterLearningsMine refused a wired seam")
	}
	for _, name := range mineTwinNames {
		got, ok := tbl.Lookup(name)
		if !ok {
			t.Fatalf("%s did not register with the service wired", name)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s is not RequiresInitialize — a mutating catalog action before the handshake is unthinkable (PM7)", name)
		}
		if got.Debug {
			t.Errorf("%s is Debug — product surface (PM10)", name)
		}
	}
	// Idempotent: a second call is a no-op, not a second door.
	if !tbl.RegisterLearningsMine() {
		t.Error("RegisterLearningsMine is not idempotent over the same wired seam")
	}
}

func TestLearningsMineDispatchesThroughDo(t *testing.T) {
	cases := []struct {
		method MethodName
		tool   string
		params string
	}{
		{MethodLearningsMineQuery, "learning_query", `{"queryText":"retry policy","tags":[{"pattern":"ci","prefix":true}],"limit":5}`},
		{MethodLearningsMineShow, "learning_show", `{"id":7}`},
		{MethodLearningsMineAdd, "learning_add", `{"scope":"agent","body":"flaky until the cache warms","tags":["ci"],"provenance":"observed"}`},
		{MethodLearningsMineProposeRetract, "learning_propose_retract", `{"id":7,"reason":"superseded by the fix"}`},
	}
	for _, c := range cases {
		spy := &envProposeSpy{recordNameSpy: &recordNameSpy{}}
		tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
		tbl.Records = spy
		if !tbl.RegisterLearningsMine() {
			t.Fatalf("RegisterLearningsMine refused a wired records seam (%s)", c.tool)
		}
		if _, err := tbl.Dispatch(context.Background(), "c1", MethodInitialize, json.RawMessage(fmt.Sprintf(`{"protocolVersion":%d,"clientInfo":{"name":"test","version":"0"}}`, PROTOCOL_VERSION))); err != nil {
			t.Fatal(err)
		}
		params := json.RawMessage(c.params)
		if _, err := tbl.Dispatch(context.Background(), "c1", c.method, params); err != nil {
			t.Fatalf("dispatch %s: %v", c.tool, err)
		}
		if spy.last != c.tool {
			t.Errorf("Do saw tool %q, want %s — the catalog name is the vocabulary", spy.last, c.tool)
		}
		if string(spy.params) != string(params) {
			t.Errorf("%s params were rewritten in transit: %s, want %s (rule 4 — Do is one implementation, two doors)", c.tool, spy.params, params)
		}
		// The refusal sentence surfaces VERBATIM as ErrBadParams, not
		// laundered — the no-oracle sentence and the counted cap both
		// depend on travelling untouched.
		spy.sentence = "refused: no such learning with id 7 — the memory doors answer rows your own scopes can reach (S52 LM5)"
		_, err := tbl.Dispatch(context.Background(), "c1", c.method, params)
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Fatalf("%s: a refusal sentence came back as %v (%T), want ErrBadParams", c.tool, err, err)
		}
		if !errors.Is(err, bad) || !contains(err.Error(), spy.sentence) {
			t.Errorf("%s: the sentence was laundered: %v", c.tool, err)
		}
	}
}

// TestMemoryFamilyCarriesNoRetractionDoor is the catalog-side half of
// LM8: the four specs' names, methods and schemas must never twin a
// person's door. The Go-side guards (T3's) refuse the words; this refuses
// the STRUCTURE — a fifth spec aimed at learnings.retract/.restore, or a
// schema that smuggles an actor field past additionalProperties:false.
func TestMemoryFamilyCarriesNoRetractionDoor(t *testing.T) {
	var memory []records.Spec
	for _, sp := range records.Catalog() {
		if sp.Family == "memory" {
			memory = append(memory, sp)
		}
	}
	if len(memory) != 4 {
		t.Fatalf("the memory family holds %d specs, want exactly the four — a fifth is a new door onto a person's plane", len(memory))
	}
	allowed := map[string]bool{"learning_query": true, "learning_show": true, "learning_add": true, "learning_propose_retract": true}
	for _, sp := range memory {
		if !allowed[sp.Name] {
			t.Errorf("unexpected memory spec %q — the family is closed at four", sp.Name)
		}
		if sp.Method == "learnings.retract" || sp.Method == "learnings.restore" {
			t.Errorf("spec %q twins the retraction door %s (LM8)", sp.Name, sp.Method)
		}
		// Key-form only: "word": as a PROPERTY name. The scope enum's
		// VALUE list says "agent","project" — the enum naming the scope
		// kind is exactly what LM1 wants nameable; a field NAMED agent is
		// not.
		for _, forbidden := range []string{`"actor":`, `"agent":`, `"scope":`, `"told":`, `"relayed":`} {
			if (sp.Name == "learning_query" || sp.Name == "learning_show" || sp.Name == "learning_propose_retract") && contains(string(sp.Schema), forbidden) {
				t.Errorf("%s schema carries %s — identity and person-word provenance are nameable at no memory read or ask (LM1, LM4)", sp.Name, forbidden)
			}
		}
		if sp.Name == "learning_add" {
			for _, forbidden := range []string{`"actor":`, `"agent":`, `"told":`, `"relayed":`} {
				if contains(string(sp.Schema), forbidden) {
					t.Errorf("learning_add schema carries %s — the actor is the step's, never a field, and told/relayed are nameable by no agent door (LM4)", forbidden)
				}
			}
		}
	}
}

package mcp

// candidate_test.go — the candidate operations (S46 T2, law 6). Each test
// asks one behaviour of `With`: the refusals that stop a silent wrong
// (duplicate names, removal under declaration) and the idempotence that
// makes a no-op reportable (changed:false in T5, not an error). None of
// them validates content — that is Assemble's one validator; these refuse
// only what is structural to the change.

import (
	"reflect"
	"strings"
	"testing"
)

func candRows() Rows {
	return Rows{
		Servers: []ServerRow{
			{Name: "alpha", Tier: TierPublic, Enabled: true,
				Transport: TransportConfig{Type: TransportStdio, Command: "run"},
				Tools:     []ToolConfig{{Name: "t1", Rerun: "safe", Effect: "local"}}},
			{Name: "beta", Tier: TierPublic, Enabled: true,
				Transport: TransportConfig{Type: TransportStdio, Command: "run"},
				Tools:     []ToolConfig{{Name: "b1", Rerun: "safe", Effect: "local"}}},
			{Name: "pv", Tier: TierPrivate, AgentKey: "moe", Enabled: true,
				Transport: TransportConfig{Type: TransportStdio, Command: "run"},
				Tools:     []ToolConfig{{Name: "p1", Rerun: "safe", Effect: "local"}}},
		},
		Agents: []AgentRow{
			{AgentKey: "moe", Declares: []string{"alpha.t1"}},
			{AgentKey: "curly", Declares: []string{"beta.b1"}},
		},
	}
}

func TestWithAddRefusesDuplicateNameAnywhere(t *testing.T) {
	r := candRows()
	add := func(name string, tier Tier, agent string) error {
		_, err := r.With(AddServer{Server: ServerRow{Name: name, Tier: tier, AgentKey: agent, Enabled: true,
			Transport: TransportConfig{Type: TransportStdio, Command: "run"}}})
		return err
	}
	// Duplicate of a public server — refused, naming it and where.
	if err := add("alpha", TierPublic, ""); err == nil || !strings.Contains(err.Error(), `"alpha"`) ||
		!strings.Contains(err.Error(), "public") {
		t.Errorf("public duplicate: %v", err)
	}
	// Same name as a PRIVATE server, offered as public — refused (one
	// namespace, MC8 — the T1 UNIQUE index agrees).
	if err := add("pv", TierPublic, ""); err == nil || !strings.Contains(err.Error(), `agent "moe"`) {
		t.Errorf("private-name collision through public: %v", err)
	}
	// Same name as a public server, hidden under an agent — refused.
	if err := add("beta", TierPrivate, "curly"); err == nil || !strings.Contains(err.Error(), "public") {
		t.Errorf("public name hidden as private: %v", err)
	}
	// A free name goes through.
	out, err := add0(t, r, AddServer{Server: ServerRow{Name: "newone", Tier: TierPublic, Enabled: true,
		Transport: TransportConfig{Type: TransportStdio, Command: "run"}}})
	if err != nil {
		t.Fatalf("free add refused: %v", err)
	}
	if len(out.Servers) != 4 {
		t.Errorf("free add did not land: %+v", out.Servers)
	}
	// And the candidate then passes the one validator (add + assemble is
	// the write path's shape).
	if _, err := Assemble(out, AssembleOptions{IncludeDisabled: true}); err != nil {
		t.Errorf("valid add candidate refused by the validator: %v", err)
	}
}

func add0(t *testing.T, r Rows, c Change) (Rows, error) {
	t.Helper()
	return r.With(c)
}

func TestWithRemoveRefusesWhileDeclared(t *testing.T) {
	r := candRows()
	// alpha is declared by moe — default refuses, naming moe.
	_, err := r.With(RemoveServer{Name: "alpha"})
	if err == nil || !strings.Contains(err.Error(), "moe") || !strings.Contains(err.Error(), "never silent") {
		t.Fatalf("declared remove: %v", err)
	}
	// The cascade is explicit and takes exactly the naming declarations.
	out, err := r.With(RemoveServer{Name: "alpha", CascadeDeclares: true})
	if err != nil {
		t.Fatalf("cascade: %v", err)
	}
	for _, s := range out.Servers {
		if s.Name == "alpha" {
			t.Fatal("cascade left the server")
		}
	}
	for _, a := range out.Agents {
		switch a.AgentKey {
		case "moe":
			if len(a.Declares) != 0 {
				t.Errorf("cascade did not remove moe's alpha declare: %v", a.Declares)
			}
		case "curly":
			if len(a.Declares) != 1 || a.Declares[0] != "beta.b1" {
				t.Errorf("cascade touched an unrelated declare: %v", a.Declares)
			}
		}
	}
	// An undeclared server removes cleanly with the default (pv is nobody's
	// declaration — beta is curly's, so beta is not the example).
	if _, err := r.With(RemoveServer{Name: "pv"}); err != nil {
		t.Errorf("undeclared remove refused: %v", err)
	}
	// Unknown server — named refusal.
	if _, err := r.With(RemoveServer{Name: "ghost"}); err == nil || !strings.Contains(err.Error(), `"ghost"`) {
		t.Errorf("remove of unknown server: %v", err)
	}
}

func TestWithRevokeUndeclaredIsNoOp(t *testing.T) {
	r := candRows()
	before := candRows()
	out, err := r.With(Revoke{AgentKey: "moe", Qualified: "beta.b1"}) // moe never declared it
	if err != nil {
		t.Fatalf("revoking what was never declared must be a no-op, not an error: %v", err)
	}
	if !reflect.DeepEqual(out, before) {
		t.Errorf("the no-op changed rows: %+v", out.Agents)
	}
	// Unknown agent is also a no-op (nothing was declared, by definition).
	out, err = r.With(Revoke{AgentKey: "nobody", Qualified: "alpha.t1"})
	if err != nil || !reflect.DeepEqual(out, before) {
		t.Errorf("revoke by unknown agent: %+v err=%v", out.Agents, err)
	}
	// The real revoke does revoke.
	out, err = r.With(Revoke{AgentKey: "moe", Qualified: "alpha.t1"})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	for _, a := range out.Agents {
		if a.AgentKey == "moe" && len(a.Declares) != 0 {
			t.Errorf("revoke did not remove: %v", a.Declares)
		}
	}
}

func TestWithDeclareIsIdempotentAndLands(t *testing.T) {
	r := candRows()
	out, err := r.With(Declare{AgentKey: "curly", Qualified: "alpha.t1"})
	if err != nil {
		t.Fatalf("declare: %v", err)
	}
	out2, err := out.With(Declare{AgentKey: "curly", Qualified: "alpha.t1"})
	if err != nil {
		t.Fatalf("re-declare errored (idempotence): %v", err)
	}
	if !reflect.DeepEqual(out, out2) {
		t.Errorf("re-declare changed rows: %v vs %v", out.Agents, out2.Agents)
	}
	// Declaring to an agent with no row yet creates it.
	out3, err := r.With(Declare{AgentKey: "shemp", Qualified: "beta.b1"})
	if err != nil {
		t.Fatalf("declare to fresh agent: %v", err)
	}
	found := false
	for _, a := range out3.Agents {
		if a.AgentKey == "shemp" && reflect.DeepEqual(a.Declares, []string{"beta.b1"}) {
			found = true
		}
	}
	if !found {
		t.Errorf("fresh-agent declare did not create the row: %+v", out3.Agents)
	}
	// The candidate validates — declaring a nameless tool is refused by
	// the ONE validator, verbatim (proving With did not grow its own).
	bad, err := r.With(Declare{AgentKey: "curly", Qualified: "alpha.nothere"})
	if err != nil {
		t.Fatalf("With refused a structurally-fine declare (validator's job): %v", err)
	}
	if _, err := Assemble(bad, AssembleOptions{IncludeDisabled: true}); err == nil {
		t.Error("the one validator did not see through the candidate path")
	}
}

func TestWithUpdateReplacesWholeFieldsOnly(t *testing.T) {
	r := candRows()
	out, err := r.With(UpdateServer{Server: ServerRow{
		Name: "alpha",
		// Tier/AgentKey/Enabled deliberately zero-valued — they must ride out.
		Transport: TransportConfig{Type: TransportHTTP, URL: "http://h/p"},
		Tools:     []ToolConfig{{Name: "z9", Rerun: "unsafe", Effect: "irreversible"}},
	}})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	var got ServerRow
	for _, s := range out.Servers {
		if s.Name == "alpha" {
			got = s
		}
	}
	if got.Tier != TierPublic || got.AgentKey != "" || !got.Enabled {
		t.Errorf("update patched identity/enabled: %+v", got)
	}
	if got.Transport.Type != TransportHTTP || got.Transport.URL != "http://h/p" || got.Transport.Command != "" {
		t.Errorf("transport not replaced whole-field: %+v", got.Transport)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "z9" {
		t.Errorf("tools not replaced whole-field: %+v", got.Tools)
	}
	if _, err := r.With(UpdateServer{Server: ServerRow{Name: "ghost", Transport: TransportConfig{Type: TransportStdio, Command: "c"}}}); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Errorf("update of unknown server: %v", err)
	}
}

func TestWithSetEnabledNamesMissingServer(t *testing.T) {
	r := candRows()
	out, err := r.With(SetEnabled{Name: "alpha", Enabled: false})
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	for _, s := range out.Servers {
		if s.Name == "alpha" && s.Enabled {
			t.Error("enabled did not flip")
		}
	}
	// MG4 once more at the change layer: tools and declares ride out a
	// disable untouched.
	if _, err := Assemble(out, AssembleOptions{IncludeDisabled: true}); err != nil {
		t.Errorf("disabled candidate must still validate: %v", err)
	}
	if _, err := r.With(SetEnabled{Name: "ghost", Enabled: true}); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Errorf("enable of unknown server: %v", err)
	}
}

func TestWithSetCoreKeepsTheTwoEmptiesApart(t *testing.T) {
	r := candRows()
	none := []string{}
	out, err := r.With(SetCore{AgentKey: "lucy", Core: &none})
	if err != nil {
		t.Fatalf("set none: %v", err)
	}
	var lucy *AgentRow
	for i := range out.Agents {
		if out.Agents[i].AgentKey == "lucy" {
			lucy = &out.Agents[i]
		}
	}
	if lucy == nil || lucy.Core == nil || len(*lucy.Core) != 0 {
		t.Fatalf("none-core lost: %+v", lucy)
	}
	out2, err := out.With(SetCore{AgentKey: "lucy", Core: nil})
	if err != nil {
		t.Fatalf("restore whole: %v", err)
	}
	for _, a := range out2.Agents {
		if a.AgentKey == "lucy" && a.Core != nil {
			t.Error("whole-set restore did not go back to nil")
		}
	}
	c, err := Assemble(out2, AssembleOptions{IncludeDisabled: true})
	if err != nil {
		t.Fatalf("assemble after core set: %v", err)
	}
	if c.Agents["lucy"].Core != nil {
		t.Error("lucy's core did not read as the whole set")
	}
}

func TestWithIsPure(t *testing.T) {
	// The current rows survive every call — T5 audits before/after from
	// the same originals without defensive copies of its own.
	before := candRows()
	frozen := candRows()
	_, _ = before.With(AddServer{Server: ServerRow{Name: "extra", Tier: TierPublic, Enabled: true,
		Transport: TransportConfig{Type: TransportStdio, Command: "run"}, Tools: []ToolConfig{{Name: "e1"}}}})
	_, _ = before.With(Revoke{AgentKey: "moe", Qualified: "alpha.t1"})
	out, _ := before.With(Declare{AgentKey: "moe", Qualified: "beta.b1"})
	if !reflect.DeepEqual(before, frozen) {
		t.Errorf("With mutated its receiver: %+v", before)
	}
	// Mutating the result's slices must not reach back either.
	for i := range out.Agents {
		if out.Agents[i].AgentKey == "curly" && len(out.Agents[i].Declares) > 0 {
			out.Agents[i].Declares[0] = "poisoned"
		}
	}
	if !reflect.DeepEqual(before, frozen) {
		t.Errorf("result slices alias the receiver")
	}
	// The refused candidate returns zero Rows — a half-shape can never be
	// assembled by accident.
	if out, err := before.With(RemoveServer{Name: "alpha"}); err == nil || len(out.Servers) != 0 || len(out.Agents) != 0 {
		t.Errorf("refusal returned a partial candidate: %+v", out)
	}
}

func TestChangeSumIsClosed(t *testing.T) {
	// Change's method is unexported — an outside package cannot implement
	// the sum. Here: every member satisfies the interface and each is a
	// distinct refusal-able operation (compile-time coverage).
	var changes = []Change{
		AddServer{}, UpdateServer{}, RemoveServer{}, SetEnabled{}, Declare{}, Revoke{}, SetCore{},
	}
	if len(changes) != 7 {
		t.Fatalf("the closed set drifted: %d members", len(changes))
	}
}

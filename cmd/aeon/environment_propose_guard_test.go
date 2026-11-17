package main

// environment_propose_guard_test.go — S59 T7's own guards, the sibling that
// makes T6's plane-and-its-four-caller-classes guard true for the agent
// surface this slice was given. T6 proved the twenty human doors never reach
// an agent. T7 adds FIVE names to the agent's world — four reads and the ONE
// ask (that the environment be TIGHTENED) — and this file is what keeps that
// addition inside what the spec granted:
//
//   - EXACTLY five catalog specs in the family, and exactly five tools named
//     for them. A sixth is an agent door nobody argued for;
//   - none of them twins to any of the twenty human doors' WRITE half. The
//     thirteen environment writes and the four secretref writes have no tool,
//     and no tool is twinned to them;
//   - the agent's reference list has no field to carry a source or a value —
//     not redacted, ABSENT, which is §4c's two types rather than one
//     redaction, aimed at a model this time (EV7);
//   - the one propose twin is a write by classification and NOT a human door,
//     exempted by its single entry in the surface's closed propose-only set,
//     and absent from the narrow projection and the agent-services
//     enumeration like every other environment name.
//
// The exemptions T6's two predicates consult live HERE, not in T6's file: the
// exemption is T7's claim to own, and the canaries below fire it at the
// violations the real data does not contain.

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/tools"
)

// environmentAgentSurface is the closed list of names this slice put on the
// agent's surface: the catalog tool name, the method it twins to, and the
// class + mutability the pair must carry for the exemption to hold AT ALL.
// Both halves must match, so a spec named environment_list that points at
// environment.create is not exempt — it is the offender the predicate exists
// to catch.
type environmentAgentSurfaceShape struct {
	tool    string
	method  string
	class   string
	mutates bool
}

var environmentAgentSurface = []environmentAgentSurfaceShape{
	{tool: "environment_list", method: "environment.list", class: "safe", mutates: false},
	{tool: "environment_show", method: "environment.show", class: "safe", mutates: false},
	{tool: "environment_effective", method: "environment.effective", class: "safe", mutates: false},
	{tool: "secretref_list", method: "secretref.list", class: "safe", mutates: false},
	{tool: "environment_propose_restrict", method: "environment.proposeRestrict", class: "detectable", mutates: true},
}

func environmentAgentPair(tool string) (environmentAgentSurfaceShape, bool) {
	for _, e := range environmentAgentSurface {
		if e.tool == tool {
			return e, true
		}
	}
	return environmentAgentSurfaceShape{}, false
}

// environmentAgentSurfaceTool is the registry half of the exemption: a name on
// the closed list only, and nothing matched by pattern.
func environmentAgentSurfaceTool(name string) bool {
	_, ok := environmentAgentPair(name)
	return ok
}

// environmentAgentSurfaceSpec is the catalog half: the pair (tool name AND
// method twin) is on the list, and the pair still carries the class and the
// mutability it was granted with. A "safe" read that starts mutating, or a
// read twinned to a write door, is not exempt — it is a violation.
func environmentAgentSurfaceSpec(sp records.Spec) bool {
	e, ok := environmentAgentPair(sp.Name)
	if !ok {
		return false
	}
	return sp.Method == e.method && sp.Class == e.class && sp.Mutates == e.mutates && sp.Family == "environment"
}

// environmentLiteralFileExemptions is the walk's ONE named exemption: the file,
// the literals exempt in it, and why. It is closed by test
// (TestEnvironmentProposeGuardHasTeeth asserts its exact shape), the METHOD
// constants are exempt nowhere, and no other file of the four walked
// directories appears here.
var environmentLiteralFileExemptions = map[string]map[string]string{
	"internal/records/catalog.go": {
		`"environment.`: "S59 T7 (EV16): the catalog is DATA, and the five agent specs this slice's spec names live here with their method twins. The twenty human doors stay forbidden everywhere.",
		`"secretref.`:   "S59 T7 (EV7): secretref_list's twin, same reason — the agent's names-only read of the references.",
	},
}

// environmentLiteralExempt reports the walk's per-file, per-literal exemption.
// Matching is on the path's tail so the walk's relative roots and this list
// agree, and on the WHOLE literal — never a prefix of it.
func environmentLiteralExempt(path, literal string) bool {
	slash := filepath.ToSlash(path)
	for suffix, lits := range environmentLiteralFileExemptions {
		if strings.HasSuffix(slash, suffix) {
			_, ok := lits[literal]
			return ok
		}
	}
	return false
}

// TestEnvironmentAgentSurfaceIsExactlyTheFive is the closed-set claim, in both
// directions: five specs in the family and five tools named for them, with the
// class and mutability each was granted with — and none of the twenty human
// doors' WRITE half has a tool, or a spec pointing at it.
func TestEnvironmentAgentSurfaceIsExactlyTheFive(t *testing.T) {
	specs := records.Catalog()
	var family []records.Spec
	for _, sp := range specs {
		if sp.Family == "environment" {
			family = append(family, sp)
		}
	}
	if len(family) != len(environmentAgentSurface) {
		t.Fatalf("the catalog carries %d environment-family specs, want exactly %d — a sixth is an agent door nobody argued for (EV16)", len(family), len(environmentAgentSurface))
	}
	byName := map[string]records.Spec{}
	for _, sp := range family {
		byName[sp.Name] = sp
	}
	for _, want := range environmentAgentSurface {
		sp, ok := byName[want.tool]
		if !ok {
			t.Errorf("the catalog has no %s spec — the agent surface the spec names is missing", want.tool)
			continue
		}
		if sp.Method != want.method {
			t.Errorf("%s twins to %q, want %q — the pair is the exemption, and a pair that moved is not exempt", want.tool, sp.Method, want.method)
		}
		if sp.Class != want.class || sp.Mutates != want.mutates {
			t.Errorf("%s is class %q mutating=%v, want %q/%v — a read that mutates, or an ask that claims to be safe, is the surface widened by a field", want.tool, sp.Class, sp.Mutates, want.class, want.mutates)
		}
		if len(sp.Description) < 40 {
			t.Errorf("%s's description is %d characters — a tool the model cannot tell apart from another gets called wrong", want.tool, len(sp.Description))
		}
	}

	// The registry is the catalog projected, so the count here is the count
	// that reaches a model's tool list.
	reg := tools.Registry()
	var named []string
	for name := range reg {
		if environmentToolName.MatchString(name) {
			named = append(named, name)
		}
	}
	if len(named) != len(environmentAgentSurface) {
		t.Errorf("the tool registry carries %d environment-named tools (%s), want exactly %d", len(named), strings.Join(named, ", "), len(environmentAgentSurface))
	}
	for _, name := range named {
		if !environmentAgentSurfaceTool(name) {
			t.Errorf("the registry carries %s, which is not on the closed agent list — an agent tool onto the plane nobody granted", name)
		}
	}

	// The twenty human doors, write half: no tool, and no spec twinned to
	// them. This is T6's claim re-run over T7's data, because the catalog is
	// where a write door would sneak in.
	for _, m := range environmentWriteMethods {
		for _, sp := range specs {
			if sp.Method == string(m) {
				t.Errorf("catalog action %s twins to the human door %s — an agent door onto %s is EV16's exact refusal", sp.Name, m, m)
			}
		}
		for name := range reg {
			if strings.EqualFold(strings.ReplaceAll(name, "_", "."), string(m)) {
				t.Errorf("the tool registry carries %s, the human door %s in tool spelling", name, m)
			}
		}
	}
}

// TestEnvironmentProposeTwinIsClassifiedNotAHumanDoor is the surface half of
// EV16: the one agent door is a WRITE (it files a row), is NOT a human door
// (the closed propose-only set names it, and only it), and is absent from the
// narrow projection and the agent-services enumeration like every other name
// in the plane.
func TestEnvironmentProposeTwinIsClassifiedNotAHumanDoor(t *testing.T) {
	s, ok := protocol.SurfaceClassified(protocol.MethodEnvironmentProposeRestrict)
	if !ok {
		t.Fatal("environment.proposeRestrict is not classified — a person's surface list would show it with no kind and no sentence (TC6)")
	}
	if s.Kind != protocol.SurfaceWrite {
		t.Errorf("classified %q, want write — it files a row", s.Kind)
	}
	if s.HumanDoorOnly {
		t.Error("environment.proposeRestrict is marked HumanDoorOnly — it is the agent's ask, and the mark would demand its absence from the catalog that carries it")
	}
	if strings.TrimSpace(s.Summary) == "" {
		t.Error("classified with no summary")
	}

	for _, a := range narrow.Actions() {
		if string(a.SocketMethod) == string(protocol.MethodEnvironmentProposeRestrict) {
			t.Error("the narrow projection carries environment.proposeRestrict — a key-holder's projection must not file environment proposals (EV17)")
		}
	}
	for _, m := range protocol.AgentServiceMethods() {
		if string(m.Name) == string(protocol.MethodEnvironmentProposeRestrict) {
			t.Error("the agent-services enumeration carries environment.proposeRestrict — an agent reaches it through the tool catalog, not as a service door")
		}
	}
}

// TestSecretRefListCarriesNoSource is EV7 and §4c at the type: the agent's
// reference view has NO field that can carry where a credential lives (or its
// length, or its contents), while the person's view keeps `source` — the one
// caller entitled to it. Two types, not one redaction: leaking is not
// something a forgotten call site can do.
func TestSecretRefListCarriesNoSource(t *testing.T) {
	agent := reflect.TypeOf(protocol.SecretRefListView{})
	for _, banned := range []string{"source", "value", "secret", "token", "apiKey", "key", "password", "credential", "contents", "data", "raw", "bytes", "valueLength", "sourceValue"} {
		if _, ok := agent.FieldByNameFunc(func(n string) bool { return strings.EqualFold(n, banned) }); ok {
			t.Errorf("protocol.SecretRefListView has a %s field — the agent's reference view must not be able to carry it at all (EV7)", banned)
		}
	}
	human := reflect.TypeOf(protocol.SecretRefView{})
	if _, ok := human.FieldByName("Source"); !ok {
		t.Error("protocol.SecretRefView lost `source` — the person is the one caller it is written for, and the guard above would pass on a plane that hid it from everyone")
	}
	// The projection is the only bridge, and it is field-by-field by
	// construction: every agent field's name must exist on the person's view,
	// so a new agent field can only arrive by naming a person-side field.
	a := reflect.TypeOf(protocol.SecretRefListView{})
	for i := 0; i < a.NumField(); i++ {
		if _, ok := human.FieldByName(a.Field(i).Name); !ok {
			t.Errorf("SecretRefListView.%s has no counterpart on SecretRefView — the agent view grew a fact the plane does not track", a.Field(i).Name)
		}
	}
}

// TestEnvironmentProposeGuardHasTeeth feeds each predicate the violation the
// real data does not contain. Without these, the guards above are assertions
// that something never arrived — which is what a broken guard always looked
// like, and the exemptions make a silently-vacuous predicate easy to write.
func TestEnvironmentProposeGuardHasTeeth(t *testing.T) {
	t.Run("RegistryNames", func(t *testing.T) {
		bad := map[string]tools.Tool{
			"environment_bind":   {},
			"secretref_create":   {},
			"environment_list":   {},
			"environment_update": {},
		}
		got := strings.Join(registryNamingEnvironment(bad), ",")
		for _, want := range []string{"environment_bind", "secretref_create", "environment_update"} {
			if !strings.Contains(got, want) {
				t.Errorf("registryNamingEnvironment did not flag %s (it flagged %q) — the exemption is wider than the five", want, got)
			}
		}
		if strings.Contains(got, "environment_list") {
			t.Errorf("registryNamingEnvironment flagged the granted environment_list — the exemption does not reach the list it names")
		}
	})

	t.Run("CatalogPairs", func(t *testing.T) {
		poisoned := []records.Spec{
			{Name: "environment_bind", Method: "environment.bind", Family: "environment", Class: "detectable", Mutates: true},
			{Name: "secretref_create", Method: "secretref.create", Family: "environment", Class: "detectable", Mutates: true},
			// The subtle one: an EXEMPT tool name aimed at a write door.
			{Name: "environment_list", Method: "environment.create", Family: "environment", Class: "safe"},
			// And the exempt pair gone slack: a "read" that mutates.
			{Name: "environment_show", Method: "environment.show", Family: "environment", Class: "safe", Mutates: true},
			{Name: "environment_whatever", Method: "environment.show", Family: "environment", Class: "safe"},
		}
		got := strings.Join(catalogNamingEnvironment(poisoned), ",")
		for _, want := range []string{"environment_bind", "secretref_create", "environment_list", "environment_show", "environment_whatever"} {
			if !strings.Contains(got, want) {
				t.Errorf("catalogNamingEnvironment did not flag %s (it flagged %q) — the pair check or the class check is not running", want, got)
			}
		}
		var real []records.Spec
		for _, sp := range records.Catalog() {
			if sp.Family == "environment" {
				real = append(real, sp)
			}
		}
		if bad := catalogNamingEnvironment(real); len(bad) != 0 {
			t.Errorf("the five granted specs now trip the guard: %s", strings.Join(bad, ", "))
		}
	})

	t.Run("LiteralWalk", func(t *testing.T) {
		poison := "package x\n\nconst a = \"environment.create\"\nconst b = \"secretref.update\"\nvar c = MethodEnvironmentBind\nvar d = MethodSecretRefCreate\n"
		hits := environmentLiteralsIn(poison)
		for _, want := range []string{`"environment.`, `"secretref.`, "MethodEnvironment", "MethodSecretRef"} {
			found := false
			for _, h := range hits {
				if h == want {
					found = true
				}
			}
			if !found {
				t.Errorf("environmentLiteralsIn did not flag %q in a poisoned source — the predicate reads nothing", want)
			}
		}
		// The exemption is the closed list it claims to be.
		if len(environmentLiteralFileExemptions) != 1 {
			t.Errorf("the literal exemption covers %d files, want exactly 1", len(environmentLiteralFileExemptions))
		}
		for file, lits := range environmentLiteralFileExemptions {
			if len(lits) != 2 {
				t.Errorf("%s exempts %d literals, want exactly the two wire prefixes", file, len(lits))
			}
			if environmentLiteralExempt(filepath.Join("..", "..", file), "MethodEnvironment") {
				t.Errorf("%s exempts the METHOD constant — only the two wire prefixes are exempt, never the constants", file)
			}
			if !environmentLiteralExempt(filepath.Join("..", "..", file), `"environment.`) {
				t.Errorf("%s does not exempt the one prefix it lists — the walk would fail on the catalog the spec mandates", file)
			}
		}
		// And it reaches NO other file of the four walked directories.
		for _, other := range []string{
			"internal/records/other.go", "internal/tools/records_tools.go",
			"internal/narrow/actions.go", "internal/agentsvc/services.go",
		} {
			if environmentLiteralExempt(filepath.Join("..", "..", other), `"environment.`) {
				t.Errorf("%s is exempt from the literal walk — the exemption spread beyond the one file", other)
			}
		}
	})

	t.Run("AgentSurfaceClosed", func(t *testing.T) {
		if len(environmentAgentSurface) != 5 {
			t.Errorf("the agent surface list holds %d entries, want 5 — the list is the exemption and it is closed", len(environmentAgentSurface))
		}
		if environmentAgentSurfaceTool("environment_bind") || environmentAgentSurfaceSpec(records.Spec{Name: "environment_bind", Method: "environment.bind", Family: "environment"}) {
			t.Error("a WRITE tool is exempt by name — the closed list is not closed")
		}
	})
}

// TestEnvironmentAgentToolsAreNotDefaultGranted is S11's rule carried onto
// this slice's five: registered is not granted. The registry holds them, the
// catalog projects them, and `DefaultGrant()` still names `read, list, write`
// — a step reaches any of the five only through a `tool_access` row an
// operator added. The five are named here rather than asserted in the
// abstract, because "none of the new ones is granted" is the claim that
// fails the day someone grants one to make a demo work.
func TestEnvironmentAgentToolsAreNotDefaultGranted(t *testing.T) {
	granted := map[string]bool{}
	for _, n := range tools.DefaultGrant() {
		granted[n] = true
	}
	if len(tools.DefaultGrant()) != 3 {
		t.Errorf("DefaultGrant() is %v — the three (read, list, write) are the floor; a fourth name is a widening of every step's world", tools.DefaultGrant())
	}
	for _, e := range environmentAgentSurface {
		if granted[e.tool] {
			t.Errorf("%s is in DefaultGrant() — registered is not granted (S11); an operator's tool_access row is the door", e.tool)
		}
		if _, ok := tools.Registry()[e.tool]; !ok {
			t.Errorf("%s is not in the registry — the catalog's five arrive through recordTools' projection, and a missing one means the projection stopped covering this family", e.tool)
		}
	}
}

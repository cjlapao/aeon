package tools

// meta_test.go is S47 T1's half of design TC3: the tests that make "every tool
// says what it can do" a property of the build rather than a habit. A tool
// arriving with no Meta, a capability nobody reviewed, a policy type that does
// not exist, or a Notes sentence with a control character in it each fails here,
// naming the tool.

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/records"
)

// honestRegistryNames is Registry()'s key set with the hidden alias filtered,
// exactly as ToolNames() does — a Meta keyed `write_file` would be a second
// truth for one tool, and a completeness test that iterated the raw keys would
// demand one.
func honestRegistryNames() []string {
	reg := Registry()
	out := make([]string, 0, len(reg))
	for name := range reg {
		if name == aliasWriteFile {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestEveryRegistryNameHasMeta is rule 1's first direction: a tool in the
// registry with no declaration fails the suite and names itself. It runs with
// whatever seams are wired in this build; TestConditionalToolsDeclareMetaWhenWired
// covers the other wiring.
func TestEveryRegistryNameHasMeta(t *testing.T) {
	for _, name := range honestRegistryNames() {
		meta, ok := MetaFor(name)
		if !ok {
			t.Errorf("tool %q has no Meta — a tool cannot ship without declaring what it can do (design TC3)", name)
			continue
		}
		if strings.TrimSpace(meta.Family) == "" {
			t.Errorf("tool %q has an empty Family", name)
		}
		if strings.TrimSpace(meta.Notes) == "" {
			t.Errorf("tool %q has no Notes sentence — a grant decision with no qualifier is a guess", name)
		}
	}
}

// TestEveryMetaNamesARealTool is rule 1's second direction, the one that catches
// a tool that was deleted while its declaration stayed behind.
func TestEveryMetaNamesARealTool(t *testing.T) {
	reg := Registry()
	for name, meta := range AllMeta() {
		if _, ok := reg[name]; ok {
			continue
		}
		// The two conditional tools are declared whether or not their seam is
		// wired (TC7 seeds them always); a name that is neither registered nor
		// one of those is stale.
		if name == "search" || name == "graph" {
			continue
		}
		t.Errorf("Meta names %q (%+v), which is not in the registry — a declaration for a tool that no longer exists", name, meta)
	}
}

// TestConditionalToolsDeclareMetaWhenWired closes the wiring gap: in a build
// where the seams are up, the conditional tools are registry names and must
// carry Meta like any other.
func TestConditionalToolsDeclareMetaWhenWired(t *testing.T) {
	for _, pair := range []struct {
		name  string
		wired bool
	}{
		{"search", SearchRegistered()},
		{"graph", GraphRegistered()},
	} {
		if !pair.wired {
			continue
		}
		if _, ok := Registry()[pair.name]; !ok {
			t.Errorf("%s reports itself wired but is absent from the registry", pair.name)
		}
		if _, ok := MetaFor(pair.name); !ok {
			t.Errorf("%s is wired and has no Meta", pair.name)
		}
	}
}

// TestCapabilityVocabularyIsClosed is rule 2: the closed set itself is sorted
// and duplicate-free, and no tool declares a string outside it.
func TestCapabilityVocabularyIsClosed(t *testing.T) {
	caps := Capabilities()
	if !sort.SliceIsSorted(caps, func(i, j int) bool { return caps[i] < caps[j] }) {
		t.Errorf("Capabilities() is not sorted: %v", caps)
	}
	seen := map[Capability]bool{}
	for _, c := range caps {
		if seen[c] {
			t.Errorf("Capabilities() lists %q twice", c)
		}
		seen[c] = true
	}
	for name, meta := range AllMeta() {
		for _, c := range meta.Capabilities {
			if !c.Valid() {
				t.Errorf("tool %q declares the capability %q, which is outside the closed vocabulary", name, c)
			}
		}
		if !sort.SliceIsSorted(meta.Capabilities, func(i, j int) bool {
			return meta.Capabilities[i] < meta.Capabilities[j]
		}) {
			t.Errorf("tool %q has unsorted capabilities %v", name, meta.Capabilities)
		}
		dupe := map[Capability]bool{}
		for _, c := range meta.Capabilities {
			if dupe[c] {
				t.Errorf("tool %q lists the capability %q twice", name, c)
			}
			dupe[c] = true
		}
	}
}

// TestGovernedNamesAreRealPolicyTypes is rule 2's second half, checked against
// the policy vocabulary itself rather than a restated list: a policy type that
// does not exist gates nothing, so naming one would promise a gate that is not
// there.
func TestGovernedNamesAreRealPolicyTypes(t *testing.T) {
	real := map[string]bool{}
	for _, p := range policy.AllPolicyTypes() {
		real[string(p)] = true
	}
	for name, meta := range AllMeta() {
		seen := map[string]bool{}
		for _, g := range meta.Governed {
			if !real[g] {
				t.Errorf("tool %q names the policy type %q, which the policy vocabulary does not contain (it gates nothing)", name, g)
			}
			if seen[g] {
				t.Errorf("tool %q names the policy type %q twice", name, g)
			}
			seen[g] = true
		}
	}
}

// TestCapabilityPins is rule 3: the facts a person relies on when deciding a
// grant. Each row is a claim about the world, not about this file, and each
// would be a lie if the code changed under it.
func TestCapabilityPins(t *testing.T) {
	capOf := func(t *testing.T, name string) map[Capability]bool {
		t.Helper()
		meta, ok := MetaFor(name)
		if !ok {
			t.Fatalf("tool %q has no Meta", name)
		}
		set := map[Capability]bool{}
		for _, c := range meta.Capabilities {
			set[c] = true
		}
		return set
	}
	has := func(t *testing.T, name string, want ...Capability) {
		t.Helper()
		set := capOf(t, name)
		for _, c := range want {
			if !set[c] {
				t.Errorf("%q does not declare %q (it has %v)", name, c, set)
			}
		}
	}
	lacks := func(t *testing.T, name string, unwanted ...Capability) {
		t.Helper()
		set := capOf(t, name)
		for _, c := range unwanted {
			if set[c] {
				t.Errorf("%q declares %q, which it must not", name, c)
			}
		}
	}

	// The shell spawns and is governed by the command gate as well as the tool gate.
	has(t, "bash", CapSpawnProcess)
	gov := map[string]bool{}
	for _, g := range mustMeta(t, "bash").Governed {
		gov[g] = true
	}
	if !gov[string(policy.CommandAccess)] {
		t.Errorf("bash is not governed by command_access: %v", mustMeta(t, "bash").Governed)
	}
	// bash is deliberately NOT widened to the transitive effects a shell can
	// reach; the control for those is command_access and the allow token.
	lacks(t, "bash", CapWorkspaceRead, CapWorkspaceWrite, CapNetworkEgress)

	// The egress is exactly one tool.
	has(t, "webfetch", CapNetworkEgress)
	for name, meta := range AllMeta() {
		if name == "webfetch" {
			continue
		}
		for _, c := range meta.Capabilities {
			if c == CapNetworkEgress {
				t.Errorf("%q declares %q — webfetch is the loop's one egress", name, c)
			}
		}
	}

	// The two workspace writers, and the four readers that must not write.
	has(t, "write", CapWorkspaceWrite)
	has(t, "edit", CapWorkspaceWrite, CapWorkspaceRead)
	for _, name := range []string{"read", "list", "glob", "grep"} {
		has(t, name, CapWorkspaceRead)
		lacks(t, name, CapWorkspaceWrite, CapSpawnProcess, CapNetworkEgress)
	}

	// The human door and the loop starter.
	has(t, "question", CapAsksHuman)
	has(t, "run_start", CapStartsLoop)
	// The peer ask carries T0's single reviewed vocabulary addition.
	has(t, "ask_agent", CapMessagesAgent)
	lacks(t, "ask_agent", CapAsksHuman, CapOrgRecordsWrite)

	// No hand-written core tool writes an org record: that capability belongs to
	// the projected catalog tools, and a core tool acquiring it means the core
	// reached the record layer.
	for _, name := range []string{"read", "list", "glob", "grep", "write", "edit", "webfetch", "bash", "question", "ask_agent", "todo", "report_size"} {
		lacks(t, name, CapOrgRecordsWrite)
	}

	// The two conditional tools read their own layers.
	has(t, "search", CapIndexRead)
	has(t, "graph", CapGraphRead)

	// The peer-ask and the checklist are not the human door.
	lacks(t, "todo", CapAsksHuman, CapOrgRecordsWrite, CapWorkspaceWrite)
	lacks(t, "report_size", CapAsksHuman, CapOrgRecordsWrite, CapWorkspaceWrite)
}

func mustMeta(t *testing.T, name string) Meta {
	t.Helper()
	meta, ok := MetaFor(name)
	if !ok {
		t.Fatalf("tool %q has no Meta", name)
	}
	return meta
}

// TestDefaultGrantHasNoSpawnOrEgress states the registration audit's claim
// ("no shell, no egress by default") in capability terms, so widening the
// default grant to a tool that spawns or egresses fails here even if the name
// itself is new.
func TestDefaultGrantHasNoSpawnOrEgress(t *testing.T) {
	for _, name := range DefaultGrant() {
		meta := mustMeta(t, name)
		for _, c := range meta.Capabilities {
			if c == CapSpawnProcess || c == CapNetworkEgress {
				t.Errorf("the default grant names %q, which declares %q — the default grant reaches no shell and no network", name, c)
			}
		}
	}
}

// TestRecordsToolsDeriveFromTheCatalog is rule 4: the projected half is a
// function of the catalog, so the two cannot disagree about what writes.
func TestRecordsToolsDeriveFromTheCatalog(t *testing.T) {
	for _, sp := range records.Catalog() {
		meta, ok := MetaFor(sp.Name)
		if !ok {
			t.Errorf("catalog action %q has no Meta", sp.Name)
			continue
		}
		if meta.Family != sp.Family {
			t.Errorf("%q: Meta.Family %q, catalog Family %q", sp.Name, meta.Family, sp.Family)
		}
		has := map[Capability]bool{}
		for _, c := range meta.Capabilities {
			has[c] = true
		}
		if has[CapOrgRecordsWrite] != sp.Mutates {
			t.Errorf("%q: org_records_write=%v but Spec.Mutates=%v — the derivation and the catalog disagree", sp.Name, has[CapOrgRecordsWrite], sp.Mutates)
		}
		if !sp.Mutates && !has[CapOrgRecordsRead] {
			t.Errorf("%q: a non-mutating catalog action must declare org_records_read, got %v", sp.Name, meta.Capabilities)
		}
		if len(meta.Governed) != 1 || meta.Governed[0] != string(policy.ToolAccess) {
			t.Errorf("%q: every catalog action is judged per call by tool_access, got %v", sp.Name, meta.Governed)
		}
	}
	// The declared deviations exist, and are exactly the tools the evidence named.
	if !hasCap(t, "run_start", CapStartsLoop) {
		t.Error("run_start must declare starts_loop — the mechanical Mutates rule alone would hide what it does")
	}
	for _, name := range []string{"attach", "goal_create", "brief_create"} {
		if !hasCap(t, name, CapWorkspaceRead, CapOrgRecordsWrite) {
			t.Errorf("%q must declare workspace_read beside org_records_write (it pins workspace bytes into the row)", name)
		}
	}
}

func hasCap(t *testing.T, name string, want ...Capability) bool {
	t.Helper()
	set := map[Capability]bool{}
	for _, c := range mustMeta(t, name).Capabilities {
		set[c] = true
	}
	for _, c := range want {
		if !set[c] {
			return false
		}
	}
	return true
}

// TestEmptyCapabilitySetsAreOnlyTheDeclaredTools is the guard an empty set needs
// to be an honest declaration rather than an omission: exactly these tools say
// they reach nothing outside the step, and a new tool arriving empty fails.
func TestEmptyCapabilitySetsAreOnlyTheDeclaredTools(t *testing.T) {
	allowed := map[string]bool{"todo": true, "report_size": true}
	for _, name := range honestRegistryNames() {
		meta := mustMeta(t, name)
		if len(meta.Capabilities) == 0 && !allowed[name] {
			t.Errorf("tool %q declares an empty capability set, which only %v may", name, metaKeyList(allowed))
		}
	}
	for name := range allowed {
		if _, ok := MetaFor(name); !ok {
			t.Errorf("%q is an allowed-empty tool and has no Meta", name)
		}
	}
}

func metaKeyList(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestMetaNotesAreSafeText is rule 5: the sentence is rendered in the app, so it
// is bounded and free of control characters (SEC-033).
func TestMetaNotesAreSafeText(t *testing.T) {
	for name, meta := range AllMeta() {
		if len([]rune(meta.Notes)) > 200 {
			t.Errorf("tool %q has a %d-rune Notes sentence; the cap is 200", name, len([]rune(meta.Notes)))
		}
		for _, r := range meta.Notes {
			if r < 0x20 || r == 0x7f {
				t.Errorf("tool %q's Notes contains the control character %q", name, r)
			}
		}
	}
}

// TestMetaIsAFunctionBuiltMapNotAPackageVariable pins the Registry() stance:
// a package-level map anyone could append to is a second registry.
func TestMetaIsAFunctionBuiltMapNotAPackageVariable(t *testing.T) {
	src, err := os.ReadFile("meta.go")
	if err != nil {
		t.Fatalf("read meta.go: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "meta.go", src, 0)
	if err != nil {
		t.Fatalf("parse meta.go: %v", err)
	}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if declaresMetaMap(vs.Type) {
				t.Errorf("meta.go declares a package-level map[string]Meta — the table is function-built so nothing can append to it")
			}
		}
	}
}

func declaresMetaMap(expr ast.Expr) bool {
	ml, ok := expr.(*ast.MapType)
	if !ok {
		return false
	}
	key, ok := ml.Key.(*ast.Ident)
	if !ok || key.Name != "string" {
		return false
	}
	val, ok := ml.Value.(*ast.Ident)
	return ok && val.Name == "Meta"
}

// TestSchemaHashIsStableAndSensitive pins the drift detector: two spellings of
// one schema hash alike, a real change hashes differently, and the hash is the
// 64 hex characters the `tool` table's CHECK demands.
func TestSchemaHashIsStableAndSensitive(t *testing.T) {
	a := model.ToolSpec{Name: "x", Parameters: json.RawMessage(`{"type":"object","properties":{"b":{"type":"string"},"a":{"type":"integer"}},"required":["a"]}`)}
	b := model.ToolSpec{Name: "x", Parameters: json.RawMessage(`{ "required" : [ "a" ],"properties":{"a":{"type":"integer"},"b":{"type":"string"}}, "type":"object"}`)}
	if SchemaHash(a) != SchemaHash(b) {
		t.Errorf("the same schema in two key orders hashes differently: %s vs %s", SchemaHash(a), SchemaHash(b))
	}
	c := model.ToolSpec{Name: "x", Parameters: json.RawMessage(`{"type":"object","properties":{"b":{"type":"string"}},"required":["a"]}`)}
	if SchemaHash(a) == SchemaHash(c) {
		t.Error("a changed property type hashes identically — drift would be invisible")
	}
	if h := SchemaHash(a); len(h) != 64 || strings.ToLower(h) != h {
		t.Errorf("SchemaHash returned %q, want 64 lowercase hex characters", h)
	}
	if SchemaHash(model.ToolSpec{Name: "none"}) != SchemaHash(model.ToolSpec{Name: "none2"}) {
		t.Error("two tools with no schema hash differently — the empty schema must have one value")
	}
	if SchemaHash(a) == SchemaHash(model.ToolSpec{Name: "none"}) {
		t.Error("a real schema hashes like no schema at all")
	}
	if got := string(CanonicalSchema(a)); !strings.HasPrefix(got, `{"properties"`) {
		t.Errorf("CanonicalSchema did not sort keys: %s", got)
	}
	if !json.Valid(CanonicalSchema(a)) {
		t.Error("CanonicalSchema returned invalid JSON")
	}
}

// TestAllMetaIsACopy keeps the caller from editing the table by returning it.
func TestAllMetaIsACopy(t *testing.T) {
	first := AllMeta()
	first["read"] = Meta{Family: "tampered"}
	if got := AllMeta()["read"].Family; got == "tampered" {
		t.Error("AllMeta handed out the table itself — a caller can rewrite the catalog")
	}
	caps := AllMeta()["read"].Capabilities
	caps[0] = CapSpawnProcess
	if got := AllMeta()["read"].Capabilities[0]; got != CapWorkspaceRead {
		t.Errorf("AllMeta shared a capability slice — a caller rewrote it (got %q)", got)
	}
}

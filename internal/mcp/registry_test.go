package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/cjlapao/aeon/internal/tools"
)

// fixtureConfig is the configuration the tier tests walk. It is deliberately
// shaped like the file an operator writes — private servers nested under
// their agent, public servers in their own section — because Q3's answer
// (T1 notes) is that the shape itself is what makes a mis-tiered server hard
// to write, so the tests have to exercise that shape, not a flat list.
func fixtureConfig() Config {
	return Config{
		Public: []ServerConfig{
			{
				Name:      "docsproxy",
				Transport: TransportConfig{Type: "http", URL: "http://127.0.0.1:9/mcp"},
				Tools: []ToolConfig{
					{Name: "docs", Rerun: "safe", Effect: "local"},
					{Name: "publish", Rerun: "unsafe", Effect: "outward"},
				},
			},
			{
				Name:      "alpha",
				Transport: TransportConfig{Type: "http", URL: "http://127.0.0.1:9/mcp"},
				Tools:     []ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}},
			},
			{
				Name:      "beta",
				Transport: TransportConfig{Type: "http", URL: "http://127.0.0.1:9/mcp"},
				Tools:     []ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}},
			},
		},
		Agents: map[string]AgentConfig{
			"reddit-agent": {
				Private: []ServerConfig{
					{
						Name:      "fakeserver",
						Transport: TransportConfig{Type: "stdio", Command: "/bin/true"},
						Tools: []ToolConfig{
							{Name: "search", Rerun: "safe", Effect: "local"},
							{Name: "post", Rerun: "unsafe", Effect: "irreversible"},
							{Name: "mystery"}, // unclassified: T3's subject, T1 must keep it OUT
						},
					},
				},
				Declares: []string{"docsproxy.docs"},
			},
			"golang-engineer-loop": {
				Declares: []string{"docsproxy.docs", "alpha.search", "beta.search"},
			},
		},
	}
}

func qualifiedNames(tools []Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Qualified)
	}
	sort.Strings(out)
	return out
}

// TestPeerCannotSeePrivateTool is D1: **asserted on the list, not on a
// refusal.** A refusal-only test would pass against the filter-over-a-shared-
// list implementation that MC2 forbids.
func TestPeerCannotSeePrivateTool(t *testing.T) {
	r, err := New(fixtureConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	peer := r.ToolsFor("golang-engineer-loop")
	for _, tool := range peer {
		if tool.Server == "fakeserver" {
			t.Errorf("peer list contains %s — a private tool is visible to a peer (MC2)", tool.Qualified)
		}
		if tool.Tier == TierPrivate {
			t.Errorf("peer list contains %s with Tier private — peers see private tools at all (MC2)", tool.Qualified)
		}
	}

	owner := r.ToolsFor("reddit-agent")
	var sawSearch, sawPost bool
	for _, tool := range owner {
		switch tool.Qualified {
		case "fakeserver.search":
			sawSearch = true
			if tool.Tier != TierPrivate {
				t.Errorf("fakeserver.search tier = %q, want private", tool.Tier)
			}
		case "fakeserver.post":
			sawPost = true
		}
	}
	if !sawSearch || !sawPost {
		t.Errorf("owner list missing its own private tools (search=%v post=%v): %v",
			sawSearch, sawPost, qualifiedNames(owner))
	}
}

// TestUnclassifiedToolNotInCapabilityList pins the T1 half of MC6: an
// unclassified tool is not in ToolsFor — the refusing view (Refused) arrives
// with T3, and a tool that was listable first is a tool somebody will default.
func TestUnclassifiedToolNotInCapabilityList(t *testing.T) {
	r, err := New(fixtureConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, tool := range r.ToolsFor("reddit-agent") {
		if tool.Qualified == "fakeserver.mystery" {
			t.Errorf("unclassified fakeserver.mystery appears in the capability list (MC6)")
		}
	}
}

// TestTierComesFromConfigNotCaller is MC12: the tier is a property of where
// the server is CONFIGURED, never of who is asking. fakeserver is configured
// under reddit-agent, so it is private — and no caller argument can render
// the same server public.
func TestTierComesFromConfigNotCaller(t *testing.T) {
	r, err := New(fixtureConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Asking as nobody in particular returns nothing, not a public view.
	if got := r.ToolsFor(""); len(got) != 0 {
		t.Errorf("ToolsFor(\"\") = %v, want empty — the caller does not choose what exists", qualifiedNames(got))
	}
	// Asking as another agent does not launder a private server public.
	if got := r.ToolsFor("some-other-agent"); containsQualified(got, "fakeserver.search") {
		t.Errorf("fakeserver.search visible to an unrelated agent — tier answered by the caller (MC12)")
	}
	// The public section stays public for any declaring agent.
	if !containsQualified(r.ToolsFor("golang-engineer-loop"), "docsproxy.docs") {
		t.Error("declared public tool missing from the declaring agent")
	}
	if containsQualified(r.ToolsFor("reddit-agent"), "docsproxy.publish") {
		t.Error("a public tool the agent never declared is present — public means available-to-declare, not granted-to-all")
	}
}

func containsQualified(tools []Tool, qualified string) bool {
	for _, tool := range tools {
		if tool.Qualified == qualified {
			return true
		}
	}
	return false
}

// TestTwoServersOfferingSearchBothReachable is MC8/D11: namespacing by
// server, with neither shadowed.
func TestTwoServersOfferingSearchBothReachable(t *testing.T) {
	r, err := New(fixtureConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := r.ToolsFor("golang-engineer-loop")
	if !containsQualified(got, "alpha.search") || !containsQualified(got, "beta.search") {
		t.Errorf("namespaced collision: want alpha.search and beta.search both present, got %v", qualifiedNames(got))
	}
	seen := map[string]bool{}
	for _, tool := range got {
		if seen[tool.Qualified] {
			t.Errorf("%s listed twice", tool.Qualified)
		}
		seen[tool.Qualified] = true
	}
}

// TestCoreNineVisibleToAnyAgent pins the core tier and W21's rescope: the
// nine are visible to any agent (the grant list refines per step elsewhere),
// their classes are the ones internal/tools declares, and an agent whose
// config names a core subset sees only that subset.
func TestCoreNineVisibleToAnyAgent(t *testing.T) {
	cfg := fixtureConfig()
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	any := r.ToolsFor("golang-engineer-loop")
	coreNames := map[string]bool{}
	for _, tool := range any {
		if tool.Tier == TierCore {
			if tool.Server != "" {
				t.Errorf("core tool %s carries a server %q — core is built in (MC1)", tool.Qualified, tool.Server)
			}
			coreNames[tool.Qualified] = true
		}
	}
	want := map[string]bool{}
	for name := range tools.Registry() {
		if name == "write_file" {
			continue // the hidden compatibility alias is filtered from EVERY listing (S11)
		}
		want[name] = true
	}
	for name := range want {
		if !coreNames[name] {
			t.Errorf("core tool %s missing from an agent's default view", name)
		}
	}
	if coreNames["write_file"] {
		t.Error("the hidden write_file alias reached a listing — it is filtered from every listing (S11)")
	}
	// The classes reported for core are the ones internal/tools declares —
	// the registry re-labels nothing.
	for _, tool := range any {
		if tool.Tier != TierCore {
			continue
		}
		if reg, ok := tools.Registry()[tool.Name]; ok && tool.Rerun != reg.RerunClass() {
			t.Errorf("core tool %s rerun class %q, want %q — the registry invented a class",
				tool.Name, tool.Rerun, reg.RerunClass())
		}
	}

	// A configured core subset narrows the view.
	cfg2 := fixtureConfig()
	cfg2.Agents["golang-engineer-loop"] = AgentConfig{Core: []string{"read", "list"}}
	r2, err := New(cfg2)
	if err != nil {
		t.Fatalf("New(cfg2): %v", err)
	}
	for _, tool := range r2.ToolsFor("golang-engineer-loop") {
		if tool.Tier == TierCore && tool.Name != "read" && tool.Name != "list" {
			t.Errorf("core subset config leaked %s — the grant source is not respected", tool.Qualified)
		}
	}
}

// TestUnknownAgentGetsNoMcpTools: an agent absent from the configuration
// still gets the core view and no MCP tools — declaring is the gate for
// public (T6's "not automatically declared"), and absence is not an error.
func TestUnknownAgentGetsNoMcpTools(t *testing.T) {
	r, err := New(fixtureConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, tool := range r.ToolsFor("stranger") {
		if tool.Tier != TierCore {
			t.Errorf("agent absent from config sees %s (%s) — MCP tools arrived without a config edit (MC3)",
				tool.Qualified, tool.Tier)
		}
	}
}

// TestNoAllToolsFunctionExists is D3, **asserted by grep** over the package's
// exported surface: the only exported function that returns tools — singly or
// in bulk — is ToolsFor. A `map` return of tools is the sneaky third shape,
// so it is pinned too. This is the guard that keeps D1 and D2 true next year;
// a filter is a thing somebody forgets to apply, and a list that never
// contained the tool is not.
func TestNoAllToolsFunctionExists(t *testing.T) {
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	// `func (recv Type) Name(args) []Tool` / `) Tool` / `) map[...]…Tool…`
	// (?m): ^ must anchor per LINE, not per file — without it the grep
	// "passes" by finding nothing, which is exactly the failure the
	// seenFuncs==0 check below exists to catch.
	returnsTools := regexp.MustCompile(`(?m)^func \(\w+ \*?\w+\) ([A-Z]\w*)\([^)]*\) (\[\]Tool|Tool|map\[[^\]]*\]Tool) \{`)
	seenFuncs := 0
	const allowed = "ToolsFor"
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		if len(e.Name()) > 8 && e.Name()[len(e.Name())-9:] == "_test.go" {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		for _, m := range returnsTools.FindAllStringSubmatch(string(src), -1) {
			seenFuncs++
			if m[1] != allowed {
				t.Errorf("%s: exported method %s returns tools; %s is the only accessor (D3/MC2)",
					e.Name(), m[1], allowed)
			}
		}
	}
	if seenFuncs == 0 {
		t.Fatal("the grep matched nothing at all — it cannot prove a restriction it never found")
	}
}

// TestToolsForIsTheOnlyAccessorReturningTools is the stricter pair: the
// guard test's own regex is pinned against a list, so widening the accessor
// surface is a deliberate edit to this test, not an oversight in a new file.
func TestToolsForIsTheOnlyAccessorReturningTools(t *testing.T) {
	r, err := New(fixtureConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Compile-time shape checks: the accessor exists and nothing beside it
	// does. (The AST-level proof is the grep test above; this one fails at
	// COMPILE time if the methods disappear, which the grep cannot do.)
	var _ func(string) []Tool = r.ToolsFor

	// And the availability view answers in verdicts, not tools: it cannot
	// smuggle a Tool out the side door.
	_, _, _ = r.Availability("reddit-agent", "fakeserver.search")
	_, _ = r.Refused("reddit-agent")
}

// TestAvailabilityDistinguishesTheThreeRefusals pins the enum shape T5's
// error rendering needs: unknown (not in this agent's registry at all),
// unavailable (exists, unclassified), available (this agent, classified).
func TestAvailabilityDistinguishesTheThreeRefusals(t *testing.T) {
	r, err := New(fixtureConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if st, _, _ := r.Availability("golang-engineer-loop", "fakeserver.search"); st != AvailabilityUnknown {
		t.Errorf("peer availability of a private tool = %v, want unknown (T5 renders unknown tool)", st)
	}
	if st, _, _ := r.Availability("reddit-agent", "fakeserver.mystery"); st != AvailabilityUnavailable {
		t.Errorf("unclassified availability = %v, want unavailable (T3 renders unavailable)", st)
	}
	if st, _, _ := r.Availability("reddit-agent", "fakeserver.search"); st != AvailabilityAvailable {
		t.Errorf("owner availability = %v, want available", st)
	}
	if _, missing, _ := r.Availability("reddit-agent", "fakeserver.mystery"); len(missing) == 0 {
		t.Error("unclassified availability names nothing missing — \"unavailable\" alone is not actionable (T3)")
	}
	if st, _, _ := r.Availability("golang-engineer-loop", "nope.nope"); st != AvailabilityUnknown {
		t.Errorf("nonexistent tool = %v, want unknown", st)
	}
}

// TestRefusedViewNamesTheReason is the --include-refused data source: the
// refused tools come back as verdicts (name + reason), never as Tools —
// T3 fills the reasons; T1 pins the shape and the exclusion.
func TestRefusedViewNamesTheReason(t *testing.T) {
	r, err := New(fixtureConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	refused, err := r.Refused("reddit-agent")
	if err != nil {
		t.Fatalf("Refused: %v", err)
	}
	var found bool
	for _, f := range refused {
		if f.Qualified == "fakeserver.mystery" {
			found = true
			if f.Reason == "" {
				t.Error("refused row carries no reason — a refusal nobody can act on")
			}
			if f.MissingClasses == nil {
				t.Error("unclassified refusal names no missing class (T3's actionable sentence)")
			} else {
				if !contains(*f.MissingClasses, "rerun") || !contains(*f.MissingClasses, "effect") {
					t.Errorf("mystery missing classes = %v, want rerun+effect", *f.MissingClasses)
				}
			}
		}
	}
	if !found {
		t.Error("fakeserver.mystery absent from the refused view — invisible-and-unexplained is not refusal (MC6)")
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// TestRegistryIsImmutableAfterConstruction: ToolsFor hands out copies, and
// mutating what a caller holds does not change what the next caller sees.
// Tool carries no maps/slices today; this test is the tripwire if one is
// added without deep-copying.
func TestRegistryIsImmutableAfterConstruction(t *testing.T) {
	r, err := New(fixtureConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	first := r.ToolsFor("reddit-agent")
	for i := range first {
		first[i].Qualified = "tampered"
	}
	for _, tool := range r.ToolsFor("reddit-agent") {
		if tool.Qualified == "tampered" {
			t.Fatal("ToolsFor handed out the registry's own storage")
		}
	}
}

// TestConfigJSONRoundTrip pins the on-disk shape the operator edits:
// nesting private servers under their agent is not a code-only idea — it is
// the FILE, and this is what a review sees.
func TestConfigJSONRoundTrip(t *testing.T) {
	raw := `{
	  "public": [
	    {"name": "docsproxy",
	     "transport": {"type": "http", "url": "http://127.0.0.1:9/mcp"},
	     "tools": [{"name": "docs", "rerun": "safe", "effect": "local"}]}
	  ],
	  "agents": {
	    "reddit-agent": {
	      "private": [
	        {"name": "fakeserver",
	         "transport": {"type": "stdio", "command": "/bin/true"},
	         "tools": [{"name": "post", "rerun": "unsafe", "effect": "irreversible"}]}
	      ],
	      "declares": ["docsproxy.docs"]
	    }
	  }
	}`
	cfg, err := parseConfig([]byte(raw))
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !containsQualified(r.ToolsFor("reddit-agent"), "fakeserver.post") {
		t.Error("nested private server did not load its tools")
	}
	if !containsQualified(r.ToolsFor("reddit-agent"), "docsproxy.docs") {
		t.Error("declared public tool did not load")
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("fixture JSON invalid: %v", err)
	}
}

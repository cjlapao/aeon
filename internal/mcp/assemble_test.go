package mcp

// assemble_test.go — the round-trip law and the behaviours (S46 T2,
// T2.md's list). The law: Assemble(Disassemble(c), {IncludeDisabled:true})
// == c — for every fixture and for 200 generated configs — is what proves
// the rows lose nothing; the verbatim-validator table is what proves rows
// and file refuse the same mistake with the same words (no second
// validator); the disabled-server test is what pins MG4's semantics.

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// stdioServer / httpServer build the two transport shapes canonically (nil
// slices when empty — the canonical form the round-trip law pins).
func stdioServer(name, cmd string, env []string, tools ...ToolConfig) ServerConfig {
	s := ServerConfig{Name: name, Transport: TransportConfig{Type: TransportStdio, Command: cmd}, Tools: tools}
	if len(env) > 0 {
		s.Transport.Env = env
	}
	if s.Tools == nil {
		s.Tools = nil
	}
	return s
}

func httpServer(name, url string, tools ...ToolConfig) ServerConfig {
	return ServerConfig{Name: name, Transport: TransportConfig{Type: TransportHTTP, URL: url}, Tools: tools}
}

func roundTripFixtures(t *testing.T) []Config {
	t.Helper()
	return []Config{
		{}, // level 0 (MC11)
		{Public: []ServerConfig{
			stdioServer("alpha", "run", []string{"TOKEN"}, ToolConfig{Name: "t1", Rerun: "safe", Effect: "local"}),
			httpServer("beta", "http://127.0.0.1:9/mcp", ToolConfig{Name: "x.y", Rerun: "detectable", Effect: "outward"}),
		}},
		{Agents: map[string]AgentConfig{
			"reddit": {Private: []ServerConfig{stdioServer("secret", "priv", nil, ToolConfig{Name: "s1", Rerun: "unsafe", Effect: "irreversible"})}},
		}},
		{
			Public: []ServerConfig{stdioServer("alpha", "run", nil,
				ToolConfig{Name: "t1", Rerun: "safe", Effect: "local"},
				ToolConfig{Name: "t2"})}, // one UNCLASSIFIED tool (MC6)
			Agents: map[string]AgentConfig{
				"a": {Core: []string{"read", "list"}, Declares: []string{"alpha.t1"}},
				"b": {Core: []string{}}, // "none" — the other empty
			},
		},
		{
			Public: []ServerConfig{
				stdioServer("alpha", "run", []string{"A", "B"}, ToolConfig{Name: "t1", Rerun: "detectable", Effect: "local"}),
				httpServer("beta", "https://h/p", ToolConfig{Name: "b1", Rerun: "safe", Effect: "outward"}),
			},
			Agents: map[string]AgentConfig{
				"manny": {Core: nil, Declares: []string{"alpha.t1", "beta.b1"}},
				"moe": {
					Core: []string{"read"},
					Private: []ServerConfig{
						httpServer("gamma", "http://g/p"),
						stdioServer("zeta", "z", nil, ToolConfig{Name: "zt", Rerun: "safe", Effect: "local"}),
					},
				},
			},
		},
		{Agents: map[string]AgentConfig{"declarer-only": {}}},
	}
}

func TestRoundTripAllFixtures(t *testing.T) {
	for i, c := range roundTripFixtures(t) {
		if err := c.Validate(); err != nil {
			t.Fatalf("fixture %d is not even valid (a fixture is the law's witness): %v", i, err)
		}
		back, err := Assemble(Disassemble(c), AssembleOptions{IncludeDisabled: true})
		if err != nil {
			t.Fatalf("fixture %d round trip refused: %v", i, err)
		}
		if !reflect.DeepEqual(back, c) {
			b, _ := json.Marshal(c)
			g, _ := json.Marshal(back)
			t.Errorf("fixture %d broke the round-trip law:\n in:  %s\n out: %s", i, b, g)
		}
	}
}

// TestRoundTripProperty runs the law over 200 seeded configs (T2.md law 1).
// Generation is canonical on purpose (sorted names/declares, nil-not-empty
// slices): the law is deep-equal, and an unsorted Config has no single
// canonical image — the SAME rule (Assemble and Disassemble both sort)
// makes rows written today and read back tomorrow byte-stable; what a
// NON-canonical input Config loses through the trip is slice order and
// nil-vs-empty, never a fact (Core's two empties included — those are
// values, and they ride exactly).
func TestRoundTripProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(46022)) // seeded: a red reproduces
	corePool := []string{"read", "list", "write"}
	reruns := []string{"", "safe", "detectable", "unsafe"}
	effects := []string{"", "local", "outward", "irreversible"}

	for n := 0; n < 200; n++ {
		var c Config
		names := map[string]bool{}
		var pubNames []string
		for k := rng.Intn(4); k > 0; k-- {
			name := fmt.Sprintf("pub%d", rng.Intn(6))
			if names[name] {
				continue
			}
			names[name] = true
			pubNames = append(pubNames, name)
			c.Public = append(c.Public, genServer(rng, name, reruns, effects))
		}
		sort.Slice(c.Public, func(i, j int) bool { return c.Public[i].Name < c.Public[j].Name })

		agentKeys := map[string]bool{}
		for k := rng.Intn(3); k > 0; k-- {
			agentKeys[fmt.Sprintf("ag%d", rng.Intn(4))] = true
		}
		if len(agentKeys) > 0 {
			c.Agents = map[string]AgentConfig{}
			for key := range agentKeys {
				ac := AgentConfig{}
				for k := rng.Intn(3); k > 0; k-- {
					name := fmt.Sprintf("priv%d", rng.Intn(5))
					if names[name] {
						continue
					}
					names[name] = true
					ac.Private = append(ac.Private, genServer(rng, name, reruns, effects))
				}
				sort.Slice(ac.Private, func(i, j int) bool { return ac.Private[i].Name < ac.Private[j].Name })
				switch rng.Intn(3) {
				case 0: // whole core set — nil
				case 1: // none — the other empty
					ac.Core = []string{}
				default:
					var made []string
					for k := rng.Intn(3) + 1; k > 0; k-- {
						made = append(made, corePool[rng.Intn(len(corePool))])
					}
					sort.Strings(made)
					ac.Core = dedupSorted(made)
				}
				// Declares name REAL public tools only (validity), sorted.
				set := map[string]bool{}
				for k := rng.Intn(3); k > 0 && len(pubNames) > 0; k-- {
					sname := pubNames[rng.Intn(len(pubNames))]
					for _, s := range c.Public {
						if s.Name != sname || len(s.Tools) == 0 {
							continue
						}
						set[sname+"."+s.Tools[rng.Intn(len(s.Tools))].Name] = true
					}
				}
				if len(set) > 0 {
					for q := range set {
						ac.Declares = append(ac.Declares, q)
					}
					sort.Strings(ac.Declares)
				}
				c.Agents[key] = ac
			}
		}

		if err := c.Validate(); err != nil {
			b, _ := json.Marshal(c)
			t.Fatalf("seed %d generated an invalid config (the generator is the bug): %v\n%s", n, err, b)
		}
		back, err := Assemble(Disassemble(c), AssembleOptions{IncludeDisabled: true})
		if err != nil {
			b, _ := json.Marshal(c)
			t.Fatalf("seed %d round trip refused a valid config: %v\n%s", n, err, b)
		}
		if !reflect.DeepEqual(back, c) {
			b, _ := json.Marshal(c)
			g, _ := json.Marshal(back)
			t.Fatalf("seed %d broke the round-trip law:\n in:  %s\n out: %s", n, b, g)
		}
	}
}

func genServer(rng *rand.Rand, name string, reruns, effects []string) ServerConfig {
	seen := map[string]bool{}
	var tools []ToolConfig
	for k := rng.Intn(4); k > 0; k-- {
		tc := ToolConfig{
			Name:   fmt.Sprintf("t%d", rng.Intn(4)),
			Rerun:  reruns[rng.Intn(len(reruns))],
			Effect: effects[rng.Intn(len(effects))],
		}
		if seen[tc.Name] {
			continue
		}
		seen[tc.Name] = true
		tools = append(tools, tc)
	}
	if rng.Intn(2) == 0 {
		s := stdioServer(name, "run-"+name, nil, tools...)
		if rng.Intn(2) == 0 {
			s.Transport.Env = []string{"A", "B"}
		}
		if rng.Intn(3) == 0 {
			s.Transport.Args = []string{"serve"}
		}
		return s
	}
	return httpServer(name, "http://127.0.0.1:8/p", tools...)
}

func dedupSorted(xs []string) []string {
	out := make([]string, 0, len(xs))
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}

// --- law 3: one validator, verbatim

type rowsBadCase struct {
	name string
	c    Config
	// loose marks the one case whose refusal sentence depends on Go's map
	// iteration ORDER (which of two agents is named first) — compared by
	// shape instead of exact bytes; every other case must match exactly.
	loose bool
}

func TestAssembleReturnsValidatorErrorsVerbatim(t *testing.T) {
	good := stdioServer("alpha", "run", nil, ToolConfig{Name: "t1", Rerun: "safe", Effect: "local"})
	httpGood := httpServer("beta", "http://h/p", ToolConfig{Name: "b1", Rerun: "safe", Effect: "local"})

	cases := []rowsBadCase{
		{"upper name", Config{Public: []ServerConfig{stdioServer("Alpha", "run", nil)}}, false},
		{"dotted name", Config{Public: []ServerConfig{stdioServer("a.b", "run", nil)}}, false},
		{"leading digit", Config{Public: []ServerConfig{stdioServer("1x", "run", nil)}}, false},
		{"empty name", Config{Public: []ServerConfig{stdioServer("", "run", nil)}}, false},
		{"twice public+private", Config{
			Public: []ServerConfig{good},
			Agents: map[string]AgentConfig{"a": {Private: []ServerConfig{good}}},
		}, false},
		{"twice two agents", Config{
			Agents: map[string]AgentConfig{
				"a": {Private: []ServerConfig{good}},
				"b": {Private: []ServerConfig{good}},
			},
		}, true},
		{"bad tool name", Config{Public: []ServerConfig{stdioServer("alpha", "run", nil, ToolConfig{Name: "-bad"})}}, false},
		{"tool twice", Config{Public: []ServerConfig{stdioServer("alpha", "run", nil,
			ToolConfig{Name: "t1"}, ToolConfig{Name: "t1"})}}, false},
		{"bad rerun", Config{Public: []ServerConfig{stdioServer("alpha", "run", nil, ToolConfig{Name: "t1", Rerun: "sometimes"})}}, false},
		{"bad effect", Config{Public: []ServerConfig{stdioServer("alpha", "run", nil, ToolConfig{Name: "t1", Effect: "maybe"})}}, false},
		{"stdio no command", Config{Public: []ServerConfig{{Name: "alpha", Transport: TransportConfig{Type: TransportStdio}}}}, false},
		{"stdio with url", Config{Public: []ServerConfig{{Name: "alpha", Transport: TransportConfig{Type: TransportStdio, Command: "c", URL: "http://h"}}}}, false},
		{"env value-shaped", Config{Public: []ServerConfig{stdioServer("alpha", "run", []string{"TOKEN=hunter2"})}}, false},
		{"http no url", Config{Public: []ServerConfig{{Name: "alpha", Transport: TransportConfig{Type: TransportHTTP}}}}, false},
		{"http bad url", Config{Public: []ServerConfig{{Name: "alpha", Transport: TransportConfig{Type: TransportHTTP, URL: "ftp://h"}}}}, false},
		{"http with stdio fields", Config{Public: []ServerConfig{{Name: "alpha", Transport: TransportConfig{Type: TransportHTTP, URL: "http://h", Command: "c"}}}}, false},
		{"unknown transport", Config{Public: []ServerConfig{{Name: "alpha", Transport: TransportConfig{Type: "grpc", Command: "c"}}}}, false},
		{"core grants alias", Config{Agents: map[string]AgentConfig{"a": {Core: []string{"write_file"}}}}, false},
		{"core grants non-tool", Config{Agents: map[string]AgentConfig{"a": {Core: []string{"nosuch"}}}}, false},
		{"declares unqualified", Config{Public: []ServerConfig{good}, Agents: map[string]AgentConfig{"a": {Declares: []string{"alpha"}}}}, false},
		{"declares twice", Config{Public: []ServerConfig{good}, Agents: map[string]AgentConfig{"a": {Declares: []string{"alpha.t1", "alpha.t1"}}}}, false},
		{"declares private server", Config{
			Agents: map[string]AgentConfig{"a": {Private: []ServerConfig{good}, Declares: []string{"alpha.t1"}}},
		}, false},
		{"declares tool server lacks", Config{Public: []ServerConfig{good}, Agents: map[string]AgentConfig{"a": {Declares: []string{"alpha.nope"}}}}, false},
		{"declares on missing server", Config{Agents: map[string]AgentConfig{"a": {Declares: []string{"ghost.t1"}}}}, false},
		{"empty agent name", Config{Agents: map[string]AgentConfig{"": {}}}, false},
		{"declares tool the http server lacks", Config{Public: []ServerConfig{httpGood}, Agents: map[string]AgentConfig{"a": {Declares: []string{"beta.b1", "beta.zzz"}}}}, false},
	}

	for _, tc := range cases {
		direct := tc.c.Validate()
		if direct == nil {
			t.Errorf("%s: the table says invalid; Validate accepted it — the table drifted", tc.name)
			continue
		}
		viaRows, err := Assemble(Disassemble(tc.c), AssembleOptions{IncludeDisabled: true})
		if err == nil {
			t.Errorf("%s: rows path ACCEPTED a config the file path refuses — a second, weaker validator", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), "mcp:") {
			t.Errorf("%s: rows path invented its own sentence: %v", tc.name, err)
		}
		if tc.loose {
			if !strings.Contains(err.Error(), "is configured twice") ||
				!strings.Contains(err.Error(), "agents.a.private") || !strings.Contains(err.Error(), "agents.b.private") {
				t.Errorf("%s: rows refusal lost shape: %v", tc.name, err)
			}
			continue
		}
		if err.Error() != direct.Error() {
			t.Errorf("%s: refusal reworded through rows:\n file: %v\n rows: %v", tc.name, direct, err)
		}
		if len(viaRows.Public) != 0 || len(viaRows.Agents) != 0 {
			t.Errorf("%s: a refused config still came back shaped: %+v", tc.name, viaRows)
		}
	}
}

// --- MG4 / law 2: disabled servers

func TestAssembleOmitsDisabledForRegistryOnly(t *testing.T) {
	rows := Rows{
		Servers: []ServerRow{
			{Name: "alpha", Tier: TierPublic, Enabled: false, // NOT offered
				Transport: TransportConfig{Type: TransportStdio, Command: "run"},
				Tools:     []ToolConfig{{Name: "t1", Rerun: "safe", Effect: "local"}}},
			{Name: "beta", Tier: TierPublic, Enabled: true,
				Transport: TransportConfig{Type: TransportStdio, Command: "run"},
				Tools:     []ToolConfig{{Name: "b1", Rerun: "safe", Effect: "local"}}},
			{Name: "hidden", Tier: TierPrivate, AgentKey: "moe", Enabled: false,
				Transport: TransportConfig{Type: TransportStdio, Command: "run"},
				Tools:     []ToolConfig{{Name: "h1", Rerun: "safe", Effect: "local"}}},
		},
		Agents: []AgentRow{
			{AgentKey: "moe", Declares: []string{"alpha.t1", "beta.b1"}},
		},
	}

	// The candidate (validation truth): everything exists, validation ran
	// against THIS (law 2) — so a declare on a disabled server can never
	// dangle past the validator.
	cand, err := Assemble(rows, AssembleOptions{IncludeDisabled: true})
	if err != nil {
		t.Fatalf("candidate refused: %v", err)
	}
	if len(cand.Public) != 2 {
		t.Fatalf("candidate lost a server: %+v", cand.Public)
	}
	// The registry's view: disabled servers are NOT in the offer…
	view, err := Assemble(rows, AssembleOptions{})
	if err != nil {
		t.Fatalf("view refused (validation is not re-run on the filtered view): %v", err)
	}
	if len(view.Public) != 1 || view.Public[0].Name != "beta" {
		t.Errorf("a disabled server is in the offer: %+v", view.Public)
	}
	if _, ok := view.Agents["moe"]; !ok {
		t.Fatalf("the disabled private server took its agent out of the view entirely")
	}
	if len(view.Agents["moe"].Private) != 0 {
		t.Errorf("disabled private server is in the offer: %+v", view.Agents["moe"].Private)
	}
	// …but its declarations STAND (the grant is the agent's, the outage is
	// the server's — MG4): unchanged in the view, and they were never
	// allowed to dangle because the validator saw the disabled server. The
	// registry's own not-configured paths (ToolsFor's public lookup,
	// Call's configured() check) are what answer about them.
	if got := view.Agents["moe"].Declares; len(got) != 2 || got[0] != "alpha.t1" || got[1] != "beta.b1" {
		t.Errorf("declarations did not stand through the disabled filter: %v", got)
	}

	// The landmine T3 must respect (recorded in T2 Notes): the UNCHANGED
	// Registry.New re-validates, and this law-2 view — which keeps the
	// standing-but-currently-unofferable declare — is deliberately NOT
	// New-able. The offer is built from the validated candidate with the
	// row's enabled-ness applied at the seam; here we pin both halves:
	// New(candidate) answers ToolsFor fully, and New(view) is refused by
	// the very validation this file promises never to weaken.
	reg, err := New(cand)
	if err != nil {
		t.Fatalf("New(candidate): %v", err)
	}
	names := map[string]bool{}
	for _, tl := range reg.ToolsFor("moe") {
		names[tl.Server+"."+tl.Name] = true
	}
	if !names["alpha.t1"] || !names["beta.b1"] {
		t.Errorf("candidate registry offers changed: %v", names)
	}
	if _, err := New(view); err == nil {
		t.Error("New(view) accepted a dangling declare — Validate() got weaker, which this slice must never allow")
	}
}

// --- MC6 and C17 through the rows path

func TestUnclassifiedToolStaysUnclassified(t *testing.T) {
	// Rows path and file path must answer IDENTICALLY for an unclassified
	// tool (MC6): the config accepts it, the registry refuses it naming
	// both missing classes — never defaulted.
	rows := Rows{
		Agents: []AgentRow{{AgentKey: "a"}},
		Servers: []ServerRow{{Name: "alpha", Tier: TierPrivate, AgentKey: "a", Enabled: true,
			Transport: TransportConfig{Type: TransportStdio, Command: "run"},
			Tools:     []ToolConfig{{Name: "mystery"}}}},
	}
	viaRows, err := Assemble(rows, AssembleOptions{IncludeDisabled: true})
	if err != nil {
		t.Fatalf("an empty-class tool is UNCLASSIFIED, not invalid — it must stay accepted: %v", err)
	}
	if got := viaRows.Agents["a"].Private[0].Tools[0]; got.Rerun != "" || got.Effect != "" {
		t.Errorf("classes were defaulted through rows: %+v", got)
	}
	direct := Config{Agents: map[string]AgentConfig{"a": {Private: []ServerConfig{
		{Name: "alpha", Transport: TransportConfig{Type: TransportStdio, Command: "run"},
			Tools: []ToolConfig{{Name: "mystery"}}},
	}}}}

	r1, err := New(viaRows)
	if err != nil {
		t.Fatalf("registry from rows: %v", err)
	}
	r2, err := New(direct)
	if err != nil {
		t.Fatalf("registry from file-shape: %v", err)
	}
	f1, err := r1.Refused("a")
	if err != nil {
		t.Fatalf("refused rows: %v", err)
	}
	f2, err := r2.Refused("a")
	if err != nil {
		t.Fatalf("refused direct: %v", err)
	}
	if !reflect.DeepEqual(f1, f2) {
		t.Errorf("rows and file disagree about the refusal:\n rows: %+v\n file: %+v", f1, f2)
	}
	if len(f1) != 1 || f1[0].Qualified != "alpha.mystery" || !strings.Contains(f1[0].Reason, "unclassified") {
		t.Fatalf("MC6 refusal missing through rows: %+v", f1)
	}
	if f1[0].MissingClasses == nil || len(*f1[0].MissingClasses) != 2 {
		t.Errorf("both classes must be named missing: %+v", f1[0])
	}
}

func TestEnvValueShapedRefused(t *testing.T) {
	// C17 through the rows path: a value-shaped env entry never reaches a
	// Config — and with the SAME words as the file path.
	rows := Rows{Servers: []ServerRow{{
		Name: "alpha", Tier: TierPublic, Enabled: true,
		Transport: TransportConfig{Type: TransportStdio, Command: "run", Env: []string{"API_KEY=sk-live-42"}},
	}}}
	_, err := Assemble(rows, AssembleOptions{IncludeDisabled: true})
	if err == nil {
		t.Fatal("a value-shaped env entry assembled into a Config (C17)")
	}
	direct := Config{Public: []ServerConfig{{Name: "alpha",
		Transport: TransportConfig{Type: TransportStdio, Command: "run", Env: []string{"API_KEY=sk-live-42"}}}}}
	want := direct.Validate()
	if want == nil || err.Error() != want.Error() {
		t.Errorf("refusal differs from the file path's:\n file: %v\n rows: %v", want, err)
	}
	if !strings.Contains(err.Error(), "values never live in the configuration file") {
		t.Errorf("C17 sentence not carried verbatim: %v", err)
	}
}

// --- determinism and the structural refusals

func TestAssembleIsDeterministic(t *testing.T) {
	rows := Rows{
		Servers: []ServerRow{
			{Name: "zeta", Tier: TierPublic, Enabled: true, Transport: TransportConfig{Type: TransportStdio, Command: "z"},
				Tools: []ToolConfig{{Name: "t1", Rerun: "safe", Effect: "local"}, {Name: "t2", Rerun: "safe", Effect: "local"}}},
			{Name: "alpha", Tier: TierPublic, Enabled: true, Transport: TransportConfig{Type: TransportStdio, Command: "a"}},
			{Name: "mid", Tier: TierPublic, Enabled: true, Transport: TransportConfig{Type: TransportStdio, Command: "m"},
				Tools: []ToolConfig{{Name: "t1", Rerun: "safe", Effect: "local"}}},
			{Name: "pv1", Tier: TierPrivate, AgentKey: "b", Enabled: true, Transport: TransportConfig{Type: TransportStdio, Command: "p1"}},
			{Name: "pv0", Tier: TierPrivate, AgentKey: "b", Enabled: true, Transport: TransportConfig{Type: TransportStdio, Command: "p0"}},
		},
		Agents: []AgentRow{
			{AgentKey: "b", Declares: []string{"zeta.t1", "zeta.t2"}},
			{AgentKey: "a", Declares: []string{"mid.t1"}}, // a owns no server, still appears
		},
	}

	want := ""
	rng := rand.New(rand.NewSource(46))
	for i := 0; i < 50; i++ {
		shuffled := Rows{Servers: append([]ServerRow(nil), rows.Servers...), Agents: append([]AgentRow(nil), rows.Agents...)}
		rng.Shuffle(len(shuffled.Servers), func(a, b int) {
			shuffled.Servers[a], shuffled.Servers[b] = shuffled.Servers[b], shuffled.Servers[a]
		})
		rng.Shuffle(len(shuffled.Agents), func(a, b int) {
			shuffled.Agents[a], shuffled.Agents[b] = shuffled.Agents[b], shuffled.Agents[a]
		})
		c, err := Assemble(shuffled, AssembleOptions{})
		if err != nil {
			t.Fatalf("shuffle %d refused: %v", i, err)
		}
		blob, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if want == "" {
			want = string(blob)
			continue
		}
		if string(blob) != want {
			t.Fatalf("shuffle %d produced different bytes:\n%s\nvs\n%s", i, blob, want)
		}
	}
}

func TestAssembleRefusesImpossibleRowShapes(t *testing.T) {
	// The store's CHECK net makes these caller bugs, not config mistakes
	// (T1); Assemble refuses them with its own words — no VALIDATE case
	// reaches here, so these sentences are not a second validator.
	stdio := TransportConfig{Type: TransportStdio, Command: "c"}
	if _, err := Assemble(Rows{Servers: []ServerRow{{Name: "a", Tier: TierPublic, AgentKey: "x", Enabled: true, Transport: stdio}}}, AssembleOptions{}); err == nil ||
		!strings.Contains(err.Error(), "belongs to nobody") {
		t.Errorf("public-with-agent row assembled: %v", err)
	}
	if _, err := Assemble(Rows{Servers: []ServerRow{{Name: "a", Tier: TierPrivate, Enabled: true, Transport: stdio}}}, AssembleOptions{}); err == nil ||
		!strings.Contains(err.Error(), "names no agent") {
		t.Errorf("private-without-agent row assembled: %v", err)
	}
	if _, err := Assemble(Rows{Servers: []ServerRow{{Name: "a", Tier: "somewhat", Enabled: true, Transport: stdio}}}, AssembleOptions{}); err == nil ||
		!strings.Contains(err.Error(), "tier") {
		t.Errorf("unknown tier assembled: %v", err)
	}
	dup := Rows{Agents: []AgentRow{{AgentKey: "a"}, {AgentKey: "a"}}}
	if _, err := Assemble(dup, AssembleOptions{}); err == nil ||
		!strings.Contains(err.Error(), "appears twice") {
		t.Errorf("duplicate agent rows assembled silently: %v", err)
	}
}

// --- MC11: the deletability clause (law 7)

// TestMcpImportsUnchanged pins the package's import baseline (T0 measured
// it; T2.md law 7 asserts it): this package — non-test files — imports
// exactly stdlib and these four internal packages, and NOTHING else. No
// internal/store, no database/sql, no os-level reach beyond what S12
// already had. Adding an import here is a reviewed diff about MC11, never
// a convenience.
func TestMcpImportsUnchanged(t *testing.T) {
	baseline := map[string]bool{
		"bufio": true, "bytes": true, "context": true, "encoding/json": true,
		"errors": true, "fmt": true, "io": true, "maps": true, "net/http": true,
		"net/url": true, "os": true, "os/exec": true, "path/filepath": true,
		"regexp": true, "sort": true, "strings": true, "sync": true,
		"sync/atomic": true, "time": true,
		// 2026-11-08 (process control): `syscall`, in proc_unix.go ONLY — a server
		// runs in its own process group and a terminate kills the group, so the
		// daemon's stop reaches an npx wrapper's node child and not just the
		// wrapper. Reviewed as an MC11 diff: it is stdlib, it reaches no store and
		// no database, and the windows build has none (proc_other.go).
		"syscall":                                 true,
		"github.com/cjlapao/aeon/internal/label":  true,
		"github.com/cjlapao/aeon/internal/model":  true,
		"github.com/cjlapao/aeon/internal/policy": true,
		"github.com/cjlapao/aeon/internal/tools":  true,
	}

	fset := token.NewFileSet()
	dir, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, de := range dir {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".go") || strings.HasSuffix(de.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", de.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", de.Name(), err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if !baseline[path] {
				t.Errorf("%s imports %q — outside the T0 baseline; MC11's deletability is the import list, and the list is the assertion", de.Name(), path)
			}
		}
	}
}

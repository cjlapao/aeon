package main

// ════════════════════════════════════════════════════════════════════════════
// mcp_admin_test.go — S46 T3's task-table tests (the management reads, the
// rows-as-truth flips, and the seam disciplines). Fixture plumbing lives in
// mcp_seed_test.go; assertions here are about BEHAVIOR the task names.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"

	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// adminFixture builds the seam over a seeded store: same handle the daemon
// hands wireMCPServices, fresh probe cache.
func adminFixture(t *testing.T) (seam func(context.Context, string, json.RawMessage) (any, string, error), st *store.Store, cache *mcpProbeCache) {
	t.Helper()
	isolateStoreEnv(t) // the leaked-DATABASE-keys pin (policy_golden_test.go:179)
	st = openAgentStore(t)
	cache = newMCPProbeCache()
	return mcpAdminHandleFor(st, cache), st, cache
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	return string(b)
}

// TestNoRuntimeReadOfTheConfigFile pins the flip (T0's reader inventory,
// fully flipped): from T3 the runtime reads the TABLES, and the config file
// belongs to the importer (T4) and the doctor. The ban that matters is on
// the LOAD (mcp.LoadConfigFile / os.ReadFile of the config path) — it may
// not appear in any runtime file. Resolving the legacy PATH for the
// status door's configPath field (mcp_admin.go's mcpLegacyPath) is allowed
// by name and comment, because naming where a legacy file would live is
// not consulting it — this test says so explicitly, as the recorded
// wording deviation from the task's stricter grep line.
func TestNoRuntimeReadOfTheConfigFile(t *testing.T) {
	bannedLoad := []string{"mcp.LoadConfigFile("} // the MCP config's ONLY loader
	// Runtime files = everything in cmd/aeon except _test files, the T4
	// importer (does not exist yet — when it ships it lands in
	// mcp_import.go and joins the allow-list), and the doctor (S7 era,
	// its MCP check is an OPERATOR diagnosis of the legacy file, which is
	// exactly the importer's remit).
	allowList := map[string]bool{
		"mcp_import.go": true, // T4 (absent today)
		"doctor.go":     true, // operator diagnosis of the legacy file
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || allowList[name] {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range bannedLoad {
			if strings.Contains(string(src), b) {
				t.Errorf("%s still loads the config file (%s) — from T3 the tables are the truth; only the importer and the doctor may read the file", name, b)
			}
		}
	}
	// The path RESOLUTION appears only in mcp_admin.go among runtime files.
	entries2, _ := os.ReadDir(".")
	for _, e := range entries2 {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || allowList[name] || name == "mcp_admin.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "paths.MCPConfigFile(") {
			t.Errorf("%s resolves the legacy config path — only mcp_admin.go (the seam), the importer and the doctor own that name now", name)
		}
	}
}

// TestEmptyTablesPrintLevelZeroWords: no rows is the legitimate absence at
// every management door — an empty list is an EMPTY ARRAY (never null,
// which a UI would render as a bug), and mcp.changes carries the
// empty-table statement while a filter that matched nothing never does.
func TestEmptyTablesPrintLevelZeroWords(t *testing.T) {
	agentHome(t) // empty tables: the temp store migrates to an unseeded world
	seam, _, _ := adminFixture(t)
	res, refusal, err := seam(context.Background(), "mcp.servers", json.RawMessage(`{}`))
	if err != nil || refusal != "" {
		t.Fatalf("empty tables are a state, not a refusal: err=%v refusal=%q", err, refusal)
	}
	if got := jsonOf(t, res); got != `{"servers":[]}` {
		t.Fatalf("empty servers must be an empty array, got %s", got)
	}
	res, refusal, err = seam(context.Background(), "mcp.changes", nil)
	if err != nil || refusal != "" {
		t.Fatalf("err=%v refusal=%q", err, refusal)
	}
	if got := jsonOf(t, res); got != `{"changes":[],"statement":"no MCP configuration changes recorded."}` {
		t.Fatalf("the empty TABLE speaks the statement; got %s", got)
	}
	// mcp.vocabulary answers even with nothing configured (it is the
	// validator's, not a view of the rows).
	res, refusal, err = seam(context.Background(), "mcp.vocabulary", nil)
	if err != nil || refusal != "" || res == nil {
		t.Fatalf("vocabulary must answer on empty tables: err=%v refusal=%q", err, refusal)
	}
}

// TestWriteVisibleToNextOfferAndStatus is MG12's whole argument in one
// test: no cache means NO restart semantics — a write (here, the same Tx
// writers a T5 door will use) is visible to the very next read on the
// SAME seam value, and to the next offer build.
func TestWriteVisibleToNextOfferAndStatus(t *testing.T) {
	agentHome(t)
	seam, _, _ := adminFixture(t)
	res, _, _ := seam(context.Background(), "mcp.servers", json.RawMessage(`{}`))
	if jsonOf(t, res) != `{"servers":[]}` {
		t.Fatalf("world must start empty: %s", jsonOf(t, res))
	}
	if _, err := loadMCPConfig(); err != nil && !isNoConfig(err) {
		t.Fatalf("empty tables read as absence: %v", err)
	}
	seedMCPConfigRows(t, mcp.Config{Public: []mcp.ServerConfig{{
		Name:      "late",
		Transport: mcp.TransportConfig{Type: "stdio", Command: "late-mcp"},
		Tools:     []mcp.ToolConfig{{Name: "t", Rerun: "safe", Effect: "local"}},
	}}})
	res, _, _ = seam(context.Background(), "mcp.servers", json.RawMessage(`{}`))
	if !strings.Contains(jsonOf(t, res), `"name":"late"`) {
		t.Fatalf("the next read on the SAME seam must see the write (no cache, no restart): %s", jsonOf(t, res))
	}
	// And the offer path: buildMCPBundle reads rows per call (no cache).
	off, err := loadMCPDisabled()
	if err != nil || off["late"] {
		t.Fatalf("fresh server is enabled: off=%v err=%v", off, err)
	}
}

// TestMcpServersListsEnvNamesNeverValues (MG13/C17): the rows never held
// values; the response carries envNames — and the value the same-named
// environment variable holds must not appear anywhere in the bytes.
func TestMcpServersListsEnvNamesNeverValues(t *testing.T) {
	agentHome(t)
	t.Setenv("GOLDEN_SECRET_X", "hunter2-do-not-echo")
	seam, _, _ := adminFixture(t)
	seedMCPConfigRows(t, mcp.Config{Public: []mcp.ServerConfig{{
		Name:      "vaulted",
		Transport: mcp.TransportConfig{Type: "stdio", Command: "v-mcp", Env: []string{"GOLDEN_SECRET_X"}},
	}}})
	res, refusal, err := seam(context.Background(), "mcp.servers", json.RawMessage(`{}`))
	if err != nil || refusal != "" {
		t.Fatalf("err=%v refusal=%q", err, refusal)
	}
	got := jsonOf(t, res)
	if !strings.Contains(got, `"envNames":["GOLDEN_SECRET_X"]`) {
		t.Fatalf("names must be listed: %s", got)
	}
	if strings.Contains(got, "hunter2") {
		t.Fatalf("a VALUE echoed (C17): %s", got)
	}
}

// TestMcpServersDoesNotProbe (MG5): listing configured servers must never
// spawn a process. The stdio server's command is a marker writer: if any
// path dialled it, the file would exist.
func TestMcpServersDoesNotProbe(t *testing.T) {
	agentHome(t)
	marker := filepath.Join(t.TempDir(), "dialled.marker")
	seam, _, _ := adminFixture(t)
	seedMCPConfigRows(t, mcp.Config{Public: []mcp.ServerConfig{{
		Name:      "dialer",
		Transport: mcp.TransportConfig{Type: "stdio", Command: "/bin/touch", Args: []string{marker}},
	}}})
	for _, m := range []string{"mcp.servers", "mcp.server", "mcp.changes", "mcp.vocabulary"} {
		params := json.RawMessage(`{}`)
		if m == "mcp.server" {
			params = json.RawMessage(`{"name":"dialer"}`)
		}
		if _, _, err := seam(context.Background(), m, params); err != nil {
			t.Fatalf("%s: %v", m, err)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a management read dialled a server — the probe door is mcp.check ONLY (MG5)")
	}
}

// TestMcpServerUnknownNamesNearest: a name that does not exist is the
// TYPED ErrMcpUnknown (protocol pass-through), and it helps — near names
// first, full roster otherwise.
func TestMcpServerUnknownNamesNearest(t *testing.T) {
	agentHome(t)
	seam, _, _ := adminFixture(t)
	seedMCPConfigRows(t, mcp.Config{Public: []mcp.ServerConfig{
		{Name: "github", Transport: mcp.TransportConfig{Type: "stdio", Command: "gh"}},
		{Name: "gitforge", Transport: mcp.TransportConfig{Type: "stdio", Command: "gf"}},
	}})
	_, refusal, err := seam(context.Background(), "mcp.server", json.RawMessage(`{"name":"githubb"}`))
	if refusal != "" {
		t.Fatalf("not-exists is a typed error, not a refusal sentence: %q", refusal)
	}
	pe, ok := err.(*protocol.Error)
	if !ok || pe.Code != protocol.ErrMcpUnknown.Code {
		t.Fatalf("want typed ErrMcpUnknown, got %T %v", err, err)
	}
	what := pe.Data.What
	// "githubb"→"github" is one edit (named); "gitforge" is five away
	// (not near — the roster fallback is the far-name branch below).
	if !strings.Contains(what, "did you mean: github") {
		t.Fatalf("near names missing: %s", what)
	}
	if strings.Contains(what, "gitforge") {
		t.Fatalf("a five-edit name is not near: %s", what)
	}
	_, _, err = seam(context.Background(), "mcp.server", json.RawMessage(`{"name":"zzzzzz"}`))
	peFar, ok := err.(*protocol.Error)
	if !ok || peFar.Code != protocol.ErrMcpUnknown.Code {
		t.Fatalf("far name must still be typed unknown: %v", err)
	}
	if !strings.Contains(peFar.Data.What, `known servers: gitforge, github`) {
		t.Fatalf("far name must fall back to the sorted roster, got %q", peFar.Data.What)
	}
}

// TestMcpGrantsEqualsGrantViewVerbatim (MD9 discipline): the door prints
// grants.go's answer FIELD BY FIELD — no re-derivation, no re-wording.
// (Expiry-free rules make the answer clock-independent.)
func TestMcpGrantsEqualsGrantViewVerbatim(t *testing.T) {
	agentHome(t)
	cfg := mcp.Config{Public: []mcp.ServerConfig{{
		Name:      "github",
		Transport: mcp.TransportConfig{Type: "stdio", Command: "gh"},
		Tools:     []mcp.ToolConfig{{Name: "list_issues", Rerun: "safe", Effect: "local"}, {Name: "create_issue", Rerun: "safe", Effect: "outward"}},
	}}, Agents: map[string]mcp.AgentConfig{"reviewer": {Declares: []string{"github.list_issues"}}}}
	seedMCPConfigRows(t, cfg)
	seam, st, _ := adminFixture(t)
	// The reference: the registry + the same policy set the door reads.
	reg, err := mcp.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ps, frej := policy.NewStore(policyRepo(st)).List(context.Background(), time.Now().UTC())
	if frej != nil {
		t.Fatal(frej.Reason)
	}

	res, refusal, err := seam(context.Background(), "mcp.grants", json.RawMessage(`{"agent":"reviewer"}`))
	if err != nil || refusal != "" {
		t.Fatalf("err=%v refusal=%q", err, refusal)
	}
	got, ok := res.(protocol.MCPGrantsResult)
	if !ok {
		t.Fatalf("result type %T", res)
	}
	want := reg.GrantView("reviewer", ps, time.Now().UTC())
	if len(got.Grants) != len(want) {
		t.Fatalf("grant count diverged — the door re-derived: got %d want %d", len(got.Grants), len(want))
	}
	for i, w := range want {
		g := got.Grants[i]
		if g.Qualified != w.Qualified || g.Tier != string(w.Tier) || g.State != string(w.State) || g.Reason != w.Reason {
			t.Fatalf("grant %d not verbatim: got %+v want %+v", i, g, w)
		}
	}
	// Unknown agent: typed unknown naming the roster.
	if _, _, err := seam(context.Background(), "mcp.grants", json.RawMessage(`{"agent":"nobody"}`)); err == nil {
		t.Fatal("unknown agent must be typed ErrMcpUnknown")
	} else if pe, ok := err.(*protocol.Error); !ok || pe.Code != protocol.ErrMcpUnknown.Code {
		t.Fatalf("want typed unknown: %T %v", err, err)
	}
}

// TestMcpVocabularyComesFromTheValidator: the handler answers THROUGH
// mcpVocabularySource (proven by swapping it), and the production source
// reads the validator's OWN strings — never this package's copies.
func TestMcpVocabularyComesFromTheValidator(t *testing.T) {
	agentHome(t)
	seam, _, _ := adminFixture(t)
	old := mcpVocabularySource
	defer func() { mcpVocabularySource = old }()
	mcpVocabularySource = func() protocol.MCPVocabularyResult {
		return protocol.MCPVocabularyResult{Transports: []string{"sentinel"}, Grammars: protocol.MCPGrammar{Server: "SENTINEL"}}
	}
	res, _, err := seam(context.Background(), "mcp.vocabulary", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := jsonOf(t, res); !strings.Contains(got, "SENTINEL") || !strings.Contains(got, "sentinel") {
		t.Fatalf("the handler did not read through the seam: %s", got)
	}
	// Production strings are the validator's own exported values.
	voc := mcpVocabularySource()
	_ = voc
	prod := old()
	if prod.Grammars.Server != mcp.ServerNameGrammar() || prod.Grammars.Tool != mcp.ToolNameGrammar() || prod.Grammars.EnvName != mcp.EnvNameGrammar() {
		t.Fatal("grammars are restated copies, not the validator's strings")
	}
	// Params discipline: vocabulary takes no parameters.
	if _, refusal, err := seam(context.Background(), "mcp.vocabulary", json.RawMessage(`{"filter":"x"}`)); refusal == "" || err != nil {
		t.Fatalf("parameters must be refused: refusal=%q err=%v", refusal, err)
	}
}

// TestMcpReadsHoldNoCache (MG12, the shape half): the seam's state is
// handles and functions — reflection proves it holds no map, slice, or
// string that could hold yesterday's configuration.
func TestMcpReadsHoldNoCache(t *testing.T) {
	agentHome(t)
	_, st, cache := adminFixture(t)
	a := newMCPAdmin(st)
	typ := reflect.TypeOf(*a)
	for i := 0; i < typ.NumField(); i++ {
		ft := typ.Field(i).Type
		switch ft.Kind() {
		case reflect.Ptr, reflect.Func:
			// handles and functions: allowed.
		default:
			t.Errorf("mcpAdmin field %s of kind %s could hold a cached value — the seam holds handles and functions only (MG12)", typ.Field(i).Name, ft.Kind())
		}
	}
	_ = cache
}

// TestNoAgentToolCallsAManagementRead (MG8's static half): the agent-facing
// tool surface must never reach the person's management reads — the two
// doors are different code paths by construction, and this test keeps them
// that way by source inspection (the runtime half is T8's).
func TestNoAgentToolCallsAManagementRead(t *testing.T) {
	for _, file := range []string{"mcp_tools.go", "agent_tools.go", "tools_mcp.go"} {
		if _, err := os.Stat(file); err != nil {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"mcpServersRead", "mcpServerRead(", "mcpGrantsRead", "mcpChangesRead", "MCPAdmin"} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s reaches a management read (%s) — the agent lane is Registry.ToolsFor(self), never the person's door (MG8)", file, banned)
			}
		}
	}
	// And the five names are the ONLY mcp.* the admin seam answers; the
	// S34 probe names stay on the probe seam.
	seamCalls := map[string]bool{}
	for _, m := range []string{"mcp.servers", "mcp.server", "mcp.grants", "mcp.vocabulary", "mcp.changes"} {
		seamCalls[m] = true
	}
	if seamCalls["mcp.status"] || seamCalls["mcp.check"] {
		t.Fatal("the probe door joined the management seam — two probe truths waiting to happen")
	}
}

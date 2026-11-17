package daemon

// Tests for S47 T5 — the daemon's tools-catalog adapter, the thing behind the
// four `tools.*` doors.
//
// The protocol suite pins the DOOR; this file pins the ANSWER. What it holds the
// adapter to:
//
//   - TC2 — the served schema is the CODE's, not the row's. The row is read for
//     `version`/`origin`/drift and never for the contract the model is handed.
//   - TC10/TC11 — the drift report is produced, said, and NOT acted on: after a
//     row is tampered with, the row is still tampered with when the test ends.
//   - TC5 — `tools.byAgent` agrees with the gate, checked against
//     `tools.GrantedSet` and `policy.Evaluate` reached by an independent path and
//     not against the catalog's own answer, which would be the tautology TC5
//     exists to prevent.
//   - TC1 — the four reads write nothing: not a tool row, not an agent row, not a
//     policy row, not an MCP audit row, and the applier is never called.
//   - N8 — no cache: change a row between two calls and the second call answers
//     the new world.
//   - V11 — an unreadable world is an ERROR. A closed store must not produce an
//     empty catalog, which is the plausible wrong answer this slice exists to make
//     impossible.
//   - MC11/MC2 — the MCP half arrives only through the entry point's seam, and
//     `tools.byAgent` lists the MCP names that agent's own view shows.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// --- the fixture -----------------------------------------------------------------

// toolsCatalogEnv is one daemon-shaped world: a real store (migrations and all),
// the inert sets applied so `tools.core` has its rows and its ledger, the agent
// rows the grant chain reads, the policy rows the gate decides with, and an MCP
// seam this test supplies — counted, so "the reads are reads" is measured and not
// assumed.
type toolsCatalogEnv struct {
	st       *store.Store
	svc      protocol.ToolsCatalog
	admin    *fakeMCPAdmin
	policies *policy.Store
	now      time.Time
}

// fakeMCPAdmin is the entry point's MCP seam as the adapter sees it: it answers
// `mcp.servers` and `mcp.grants` from the fixture and counts every call, so a test
// can say which reads a door performed. MC11's rule is structural (the daemon must
// never import `internal/mcp`) and this fake is the only path the adapter has to
// the MCP half.
type fakeMCPAdmin struct {
	servers  protocol.MCPServersResult
	grants   map[string]protocol.MCPGrantsResult // by agent slug
	calls    []string                            // "mcp.servers", "mcp.grants atlas", …
	refuse   string
	failWith error
}

func (f *fakeMCPAdmin) fn() func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
	return func(_ context.Context, method string, params json.RawMessage) (any, string, error) {
		if f.failWith != nil {
			return nil, "", f.failWith
		}
		if f.refuse != "" {
			return nil, f.refuse, nil
		}
		switch method {
		case "mcp.servers":
			f.calls = append(f.calls, method)
			return f.servers, "", nil
		case "mcp.grants":
			var p struct {
				Agent string `json:"agent"`
			}
			_ = json.Unmarshal(params, &p)
			f.calls = append(f.calls, method+" "+p.Agent)
			if g, ok := f.grants[p.Agent]; ok {
				return g, "", nil
			}
			return protocol.MCPGrantsResult{Agent: p.Agent}, "", nil
		}
		return nil, "refused: " + method + " is not a method this seam answers", nil
	}
}

// mcpFixture is the MCP half of the world: one enabled private server with two
// declared tools (one classified, one NOT — MC6's unclassified tool is shown as
// unclassified, never hidden) and one DISABLED server whose tools must not appear
// anywhere, because a tool of a disabled server cannot be called by anyone.
func mcpFixture() (protocol.MCPServersResult, map[string]protocol.MCPGrantsResult) {
	servers := protocol.MCPServersResult{Servers: []protocol.MCPServerView{
		{
			Name: "acme", Tier: "private", Agent: "atlas", Enabled: true, Origin: "user",
			Tools: []protocol.MCPToolView{
				{Name: "search", Rerun: "safe", Effect: "local", Classified: true},
				{Name: "post", Rerun: "unsafe", Effect: "outward", Classified: false},
			},
		},
		{
			Name: "off", Tier: "public", Enabled: false, Origin: "system",
			Tools: []protocol.MCPToolView{{Name: "sleep", Rerun: "safe", Effect: "local", Classified: true}},
		},
	}}
	grants := map[string]protocol.MCPGrantsResult{
		"atlas": {
			Agent: "atlas",
			// Both of the server's tools are declared: the unclassified one is a
			// tool the agent asked for and the classifier has not spoken about,
			// which is the state MC6 is about — shown, refused, and still honest
			// about where it came from.
			Declares: []string{"acme.search", "acme.post"},
			Grants: []protocol.MCPGrantView{
				{Qualified: "acme.search", Tier: "private", State: "available", Reason: "allow · effect_class · acme.search · agent:atlas"},
				{Qualified: "acme.post", Tier: "private", State: "unavailable", Reason: "refused: an unclassified tool cannot be called (MC6)"},
				// A CORE-shaped grant, which is what `internal/mcp/registry.go:272`
				// really hands back for an agent's granted core tools: `Qualified`
				// carries the PLAIN name there, so this seam's answer overlaps the
				// code's own names. A fixture of qualified names only never
				// overlapped, and the catalog answered 146 rows for 73 tools on a
				// real daemon while every fixture stayed green (the S47 T11 walk).
				{Qualified: "read", Tier: "core", State: "available", Reason: "allow · tool_access · read · agent:atlas"},
			},
		},
	}
	return servers, grants
}

// newToolsCatalogEnv builds the world. `withMCP` decides whether the entry point
// wired the MCP seam at all — the V13 half of the MCP story: no seam is an ABSENT
// MCP half, never a refusal.
func newToolsCatalogEnv(t *testing.T, withMCP bool) *toolsCatalogEnv {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// The inert sets through the ONE engine the daemon holds: `tools.core` gets its
	// rows and its ledger, which is what makes `tools.status`' seed half a real
	// answer rather than a fixture's claim about one.
	engine, err := NewSeedEngine(st, seed.Options{})
	if err != nil {
		t.Fatalf("build the seed engine: %v", err)
	}
	ApplyInertAtStart(context.Background(), engine)

	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for _, a := range []store.Agent{
		{Slug: "atlas", Name: "Atlas", Version: "1", Origin: "system", Tools: `["read","list","write","bash","webfetch"]`},
		{Slug: "sentry", Name: "Sentry", Version: "1", Origin: "system", Tools: `["read","list"]`},
		// A CUSTOM agent: its row names tools and it cannot be dispatched at all
		// (S43-extra SD7). The view still shows its grant sources — the row does
		// name them — and states once that the agent is not dispatchable.
		{Slug: "cursitor", Name: "Cursitor", Version: "1", Origin: "user", Tools: `["read","grep"]`},
	} {
		if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, a, "2026-11-01T00:00:00Z"); err != nil {
			t.Fatalf("create agent %s: %v", a.Slug, err)
		}
		// `CreateAgent` writes the CONTENT columns; `tools` and `origin` belong to
		// the seed applier and the agent-mutation paths, so the fixture stamps them
		// the way those paths do. Without this the row is a name with no grants and
		// the grant chain has nothing to be tested against.
		db, err := st.GormDB().DB()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE agent SET tools = ?, origin = ? WHERE slug = ?`, a.Tools, a.Origin, a.Slug); err != nil {
			t.Fatalf("stamp agent %s: %v", a.Slug, err)
		}
	}

	pol := policy.NewStore(newPolicyRepo(st))
	addPolicy(t, pol, "tc-deny-bash-atlas", "bash", policy.Deny, policy.AgentKind, "atlas", nil)
	addPolicy(t, pol, "tc-allow-read-all", "read", policy.Allow, policy.AllAgents, "", nil)

	admin := &fakeMCPAdmin{}
	var adminFn func(ctx context.Context, method string, params json.RawMessage) (any, string, error)
	if withMCP {
		admin.servers, admin.grants = mcpFixture()
		adminFn = admin.fn()
	}

	svc := newToolsCatalogService(Options{Store: st, Seed: engine, MCPAdmin: adminFn})
	if svc == nil {
		t.Fatal("newToolsCatalogService handed back nil with a store wired")
	}
	sc, ok := svc.(*toolsCatalogService)
	if !ok {
		t.Fatalf("the seam is %T, want *toolsCatalogService", svc)
	}
	sc.now = func() time.Time { return now }
	return &toolsCatalogEnv{st: st, svc: svc, admin: admin, policies: pol, now: now}
}

func addPolicy(t *testing.T, pol *policy.Store, id, tool string, action policy.Action, kind policy.TargetKind, target string, notAfter *time.Time) {
	t.Helper()
	spec, err := json.Marshal(map[string]string{"toolName": tool, "state": string(action)})
	if err != nil {
		t.Fatal(err)
	}
	p := policy.Policy{ID: id, Type: policy.ToolAccess, Target: policy.Target{Kind: kind, ID: target},
		Action: action, Spec: spec, Source: policy.Operator, NotAfter: notAfter}
	if rej := pol.Add(context.Background(), p, time.Now().UTC()); rej != nil {
		t.Fatalf("add policy %s: %s", id, rej.Reason)
	}
}

func strptr(s string) *string { return &s }
func boolptr(b bool) *bool    { return &b }

func mustList(t *testing.T, env *toolsCatalogEnv, f protocol.ToolsListFilter) []protocol.ToolEntry {
	t.Helper()
	out, err := env.svc.ToolsList(context.Background(), f)
	if err != nil {
		t.Fatalf("tools.list: %v", err)
	}
	return out
}

func entryByName(entries []protocol.ToolEntry, name string) (protocol.ToolEntry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return protocol.ToolEntry{}, false
}

func toolEntryNames(entries []protocol.ToolEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

func hasString(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

func countUnwired(entries []protocol.ToolEntry) int {
	n := 0
	for _, e := range entries {
		if e.Conditional && !e.Available {
			n++
		}
	}
	return n
}

func statusOrFatal(t *testing.T, env *toolsCatalogEnv) protocol.ToolsStatusResult {
	t.Helper()
	st, err := env.svc.ToolsStatus(context.Background())
	if err != nil {
		t.Fatalf("tools.status: %v", err)
	}
	return st
}

func driftFor(st protocol.ToolsStatusResult, name string) (protocol.ToolDriftEntry, bool) {
	for _, d := range st.Drift {
		if d.Name == name {
			return d, true
		}
	}
	return protocol.ToolDriftEntry{}, false
}

// --- tools.list --------------------------------------------------------------------

// TestToolsListIsCompleteAndAliasIsNotAnEntry is TC10's completeness and TC11's
// single-truth rule in one pass: every honest registry name appears; the two
// conditionals appear WHETHER OR NOT this build wires them (P31 — "exists and
// cannot run" is a different fact from "does not exist"); the MCP half appears for
// the enabled servers only; and the hidden `write_file` alias is nowhere a row of
// its own — it rides on `write`, because one executor rendered twice is the drift
// the whole slice exists to make visible.
func TestToolsListIsCompleteAndAliasIsNotAnEntry(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	all := mustList(t, env, protocol.ToolsListFilter{})

	seen := map[string]protocol.ToolEntry{}
	for _, e := range all {
		if _, dup := seen[e.Name]; dup {
			t.Fatalf("%s appears twice — one tool, one entry (TC11)", e.Name)
		}
		seen[e.Name] = e
	}

	for _, n := range tools.ToolNames() {
		e, ok := seen[n]
		if !ok {
			t.Errorf("%s is a registry name and is not in the catalog", n)
			continue
		}
		if e.Source != protocol.ToolsSourceSystem {
			t.Errorf("%s has source %q", n, e.Source)
		}
	}
	for _, n := range []string{"search", "graph"} {
		e, ok := seen[n]
		if !ok {
			t.Errorf("the conditional %s is missing — an unwired tool must still be listed, with available:false (P31)", n)
			continue
		}
		if !e.Conditional {
			t.Errorf("%s is not flagged conditional", n)
		}
	}
	if _, ok := seen["write_file"]; ok {
		t.Error("`write_file` is an entry of its own — one executor, two rows, which is the drift TC11 refuses")
	}
	w, ok := seen["write"]
	if !ok {
		t.Fatal("`write` is missing")
	}
	if !hasString(w.Aliases, "write_file") {
		t.Errorf("write's aliases = %v, want write_file to ride on it", w.Aliases)
	}

	acmeSearch, ok := seen["acme.search"]
	if !ok {
		t.Fatal("the enabled server's declared tool is missing from the catalog")
	}
	if acmeSearch.Source != protocol.ToolsSourceMCP || acmeSearch.Server != "acme" || acmeSearch.Tier != "private" {
		t.Errorf("the MCP entry lost its server half: %+v", acmeSearch)
	}
	if !acmeSearch.Classified {
		t.Error("a classified tool is reported unclassified")
	}
	if post, ok := seen["acme.post"]; !ok || post.Classified {
		t.Errorf("MC6's unclassified tool must be listed AS unclassified, not hidden: %+v ok=%v", post, ok)
	}
	if _, ok := seen["off.sleep"]; ok {
		t.Error("a DISABLED server's tool is listed as callable — it cannot be called by anyone")
	}
	if acmeSearch.Version != 0 {
		t.Errorf("the MCP entry carries version %d — MCP tools are not rows in `tool` (TC4)", acmeSearch.Version)
	}
	if acmeSearch.Description != "" {
		t.Errorf("the MCP entry invented a description (%q) — no authority holds one, so it says nothing", acmeSearch.Description)
	}

	st := statusOrFatal(t, env)
	system, mcp := 0, 0
	for _, e := range all {
		if e.Source == protocol.ToolsSourceSystem {
			system++
		} else {
			mcp++
		}
	}
	if st.Counts.System != system || st.Counts.MCP != mcp {
		t.Errorf("counts = %+v, want system=%d mcp=%d — the counts and the listing are one pass over one world", st.Counts, system, mcp)
	}
	if st.Counts.ConditionalUnwired != countUnwired(all) {
		t.Errorf("conditionalUnwired = %d, want %d", st.Counts.ConditionalUnwired, countUnwired(all))
	}
}

// TestToolsListFiltersAreDaemonSide is TC10's "filters answered
// daemon-side": the five optional filters, and the empties that mean something.
// `available:false` is the honest "what cannot run today" list; an empty `q`
// matches nothing rather than everything, because a client whose search box is
// empty means "nothing matched", not "show me the world".
func TestToolsListFiltersAreDaemonSide(t *testing.T) {
	env := newToolsCatalogEnv(t, true)

	bySource := mustList(t, env, protocol.ToolsListFilter{Source: strptr(protocol.ToolsSourceMCP)})
	if len(bySource) == 0 {
		t.Fatal("source=mcp answered nothing")
	}
	for _, e := range bySource {
		if e.Source != protocol.ToolsSourceMCP {
			t.Fatalf("the source filter leaked %s (%q)", e.Name, e.Source)
		}
	}
	byFamily := mustList(t, env, protocol.ToolsListFilter{Family: strptr("process")})
	if len(byFamily) == 0 {
		t.Fatal("family=process answered nothing — bash lives there")
	}
	for _, e := range byFamily {
		if !strings.EqualFold(e.Family, "process") {
			t.Fatalf("the family filter leaked %s (%q)", e.Name, e.Family)
		}
	}
	byCap := mustList(t, env, protocol.ToolsListFilter{Capability: strptr("workspace_read")})
	if _, ok := entryByName(byCap, "read"); !ok {
		t.Errorf("capability=workspace_read missed `read`: %v", toolEntryNames(byCap))
	}
	for _, e := range byCap {
		if !hasString(e.Capabilities, "workspace_read") {
			t.Fatalf("the capability filter leaked %s (%v)", e.Name, e.Capabilities)
		}
	}
	unwired := mustList(t, env, protocol.ToolsListFilter{Available: boolptr(false)})
	for _, e := range unwired {
		if e.Available {
			t.Fatalf("available=false leaked the available %s", e.Name)
		}
	}
	if len(unwired) == 0 {
		t.Error("available=false answered nothing — this build has no search/graph resolver wired, so it owes a list")
	}
	if got := mustList(t, env, protocol.ToolsListFilter{Q: strptr("")}); len(got) != 0 {
		t.Errorf("an empty q matched %d tools — an empty search is the filter's answer, not the table's", len(got))
	}
	if got := mustList(t, env, protocol.ToolsListFilter{Q: strptr("BASH")}); len(got) == 0 {
		t.Error("q is case-insensitive over the name and the description")
	}
	if got := mustList(t, env, protocol.ToolsListFilter{Q: strptr("zzzzz")}); len(got) != 0 {
		t.Errorf("a filter that can match nothing answered %d tools", len(got))
	}
}

// TestToolsReadsAreLive is N8 and MC11 together: a policy
// row added between two calls changes the second answer without a restart, and
// every MCP fact in the answer came through the seam — the fake counted exactly one
// `mcp.servers` read for the list, which is the whole MCP path this build has.

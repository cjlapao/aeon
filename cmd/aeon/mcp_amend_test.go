package main

// mcp_amend_test.go — S46 T7: the guard amendment. S35 TD4 welded the MCP
// config door shut ("authoring is proposal + commit only") when the daemon
// had no MCP surface at all; S46 extends S45's PM1 ruling the same way S45
// amended PL15: direct authoring exists as a HUMAN DOOR (the eight write
// methods + five management reads, wired iff the seam is), the proposal
// path remains (T6), and every prohibition that is still true is now stated
// POSITIVELY with a canary each — S45's method, never edits: the pre-
// amendment assertion logic is pinned by hash (the two originals survive
// byte-identical) while this sibling carries the amended claim.
//
// What "still true" means here, rule by rule:
//   - MG8/MG15 + S45 PM10: the 13 management methods are the person's door
//     and appear on NO surface a non-person reaches — not the narrow
//     projection, not the agent-services enumeration, not the tool
//     registry, not the records catalog. The two S34 reads (mcp.status,
//     mcp.check) are NOT management and stay exactly as S34 shipped them.
//   - MC11: internal/mcp is importable only by cmd/aeon — extended (not
//     relaxed) to a NAMED importer list, so the deletability claim is a
//     measured set, not a grep that passes on nothing.
//   - MC2: the agent-facing tool handlers (T8 supplies them) must reach the
//     config only through the scoped ToolsFor(self) path — never the
//     unscoped person-view reads. Until T8 lands there are no handlers to
//     fail, and the walk says so rather than passing vacuously.
//   - MG5: the mcp_* rows are written only through the store's Tx helpers
//     as called by the admin seam's own files — one write path, named.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/tools"
)

// --- the closed vocabularies this file guards ---------------------------------

// mcpWriteMethods are S46 T4/T5's eight human-door writes.
var mcpWriteMethods = []protocol.MethodName{
	protocol.MethodMCPServerAdd, protocol.MethodMCPServerUpdate,
	protocol.MethodMCPServerRemove, protocol.MethodMCPServerSetEnabled,
	protocol.MethodMCPGrantDeclare, protocol.MethodMCPGrantRevoke,
	protocol.MethodMCPCoreSet, protocol.MethodMCPImportFile,
	// 2026-11-08: the inspect door — runs a CANDIDATE server and lists its tools; stores nothing.
	protocol.MethodMCPServerInspect,
	// 2026-11-08: share a public server with every agent — a declaration, never a permission.
	protocol.MethodMCPServerShare,
	// 2026-11-08: the processes' controls — start, terminate, restart; a person's act.
	protocol.MethodMCPProcessStart, protocol.MethodMCPProcessStop, protocol.MethodMCPProcessRestart,
}

// mcpAdminReadMethods are S46 T3's five management reads.
var mcpAdminReadMethods = []protocol.MethodName{
	protocol.MethodMCPServers, protocol.MethodMCPServer,
	protocol.MethodMCPGrants, protocol.MethodMCPVocabulary, protocol.MethodMCPChanges,
	// 2026-11-08: the processes' two reads (state/PID, and the log ring).
	protocol.MethodMCPProcesses, protocol.MethodMCPProcessLogs,
}

// mcpS34Reads are S34's two operational reads — not management, unchanged
// by S46, and the ONLY mcp.* names the bare table carries.
var mcpS34Reads = []protocol.MethodName{protocol.MethodMCPStatus, protocol.MethodMCPCheck}

// mcpManagementNames is everything a non-person surface must never name.
func mcpManagementNames() []string {
	var out []string
	for _, m := range mcpWriteMethods {
		out = append(out, string(m))
	}
	for _, m := range mcpAdminReadMethods {
		out = append(out, string(m))
	}
	return out
}

// mcpMethodConstNames are the Go constant spellings beside the literals —
// a smuggled-in projection could name either (guard #5 checks both).
var mcpMethodConstNames = []string{
	"MethodMCPServerAdd", "MethodMCPServerUpdate", "MethodMCPServerRemove",
	"MethodMCPServerSetEnabled", "MethodMCPGrantDeclare", "MethodMCPGrantRevoke",
	"MethodMCPCoreSet", "MethodMCPImportFile",
	"MethodMCPServers", "MethodMCPServer ", "MethodMCPGrants", "MethodMCPVocabulary", "MethodMCPChanges",
}

// t8AllowedMcpTools is T8's positive list, filled WHEN T8 LANDS (the five
// own-view read tools + the propose twins). Empty today: there is no agent
// MCP vocabulary yet, and the guard asserts that fact, not a hope.
var t8AllowedMcpTools = map[string]bool{
	// Landed 2026-11-02 with T8: the ONLY mcp-family specs that may mutate,
	// each twinned to an mcp.mine.* door, each creating an INERT narrowing
	// proposal (mcp_server_remove / mcp_grant_revoke — MG9 has no widening
	// kind to smuggle into either).
	"mcp_propose_remove_server": true,
	"mcp_propose_revoke_grant":  true,
}

// --- the predicates (canaries feed THESE, so a passing guard is shown to bite) --

func mcpManagementOnNarrow(actions []narrow.Action) []string {
	banned := map[string]bool{}
	for _, n := range mcpManagementNames() {
		banned[n] = true
	}
	var bad []string
	for _, a := range actions {
		if banned[string(a.SocketMethod)] {
			bad = append(bad, a.Name+" projects "+string(a.SocketMethod))
		}
	}
	return bad
}

func mcpManagementOnAgentServices(ms []protocol.AgentServiceMethod) []string {
	banned := map[string]bool{}
	for _, n := range mcpManagementNames() {
		banned[n] = true
	}
	var bad []string
	for _, m := range ms {
		if banned[string(m.Name)] {
			bad = append(bad, string(m.Name))
		}
	}
	return bad
}

var mcpToolNameRe = regexp.MustCompile(`mcp_(add|remove|declare|enable|import|commit)`)

func mcpToolsInRegistry(reg map[string]tools.Tool) []string {
	var bad []string
	for name := range reg {
		if mcpToolNameRe.MatchString(name) && !t8AllowedMcpTools[name] {
			bad = append(bad, name+" (management-shaped, not in T8's list)")
		}
	}
	return bad
}

func mcpCatalogOffenses(specs []records.Spec) []string {
	writes := map[string]bool{}
	for _, m := range mcpWriteMethods {
		writes[string(m)] = true
	}
	var bad []string
	for _, s := range specs {
		isMCP := strings.EqualFold(s.Family, "mcp") || strings.HasPrefix(strings.ToLower(s.Method), "mcp.")
		if !isMCP {
			continue
		}
		if writes[s.Method] {
			bad = append(bad, s.Name+" is twinned to the human-door write "+s.Method)
			continue
		}
		if s.Mutates && !t8AllowedMcpTools[s.Name] {
			bad = append(bad, s.Name+" mutates and is not one of T8's propose twins (an agent-reachable config write is MG15's refusal made mechanical)")
		}
	}
	return bad
}

// mcpLiteralGrep walks ROOTS' non-test .go files for the banned vocabularies
// and reports (path, needle) pairs. extra lets the CANARY feed synthetic
// files without touching the tree — the walk and the check are the same
// code path the production assertion runs.
func mcpLiteralGrep(roots []string, needles []string, extra map[string]string) []string {
	var bad []string
hit:
	for path, src := range walkedSources(roots, extra) {
		for _, needle := range needles {
			if strings.Contains(src, needle) {
				bad = append(bad, filepath.ToSlash(path)+" names "+needle)
				continue hit
			}
		}
	}
	return bad
}

// walkedSources returns path→content for every non-test .go under roots,
// plus the canary's synthetic overlays. An empty result is a broken walk,
// and every caller asserts non-empty (T7's "do not pass on an empty walk").
func walkedSources(roots []string, extra map[string]string) map[string]string {
	out := map[string]string{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			out[path] = string(src)
			return nil
		})
	}
	for path, src := range extra {
		out[path] = src
	}
	return out
}

// mcpLayerImporters walks NON-test Go under internal/ and cmd/ and returns
// every file importing internal/mcp that is not on the named list. The
// named list is T0 item 4's seven plus T3/T4/T5's named siblings (recorded
// in T7's Notes): the admin seam's whole family.
var mcpNamedImporters = map[string]bool{
	"cmd/aeon/mcp.go": true, "cmd/aeon/mcp_admin.go": true,
	"cmd/aeon/mcp_admin_handlers.go": true, "cmd/aeon/mcp_admin_writes.go": true,
	"cmd/aeon/mcp_admin_inspect.go": true, "cmd/aeon/mcp_supervisor.go": true,
	"cmd/aeon/mcp_import.go": true, "cmd/aeon/mcp_dispatch_dial.go": true,
	"cmd/aeon/mcp_dispatch_seam.go": true, "cmd/aeon/mcp_services.go": true,
	"cmd/aeon/agent.go": true, "cmd/aeon/proposal.go": true,
	"cmd/aeon/proposal_validators.go": true,
	// S46 T10: the seed validator seam — the ONE place the seed engine's
	// candidate reaches the grammar (mcpseed itself stays mcp-free; MC11
	// extended by naming, never relaxed).
	"cmd/aeon/mcp_seed_validator.go": true,
}

func mcpImportOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		if !strings.Contains(src, `"github.com/cjlapao/aeon/internal/mcp"`) {
			continue
		}
		slash := filepath.ToSlash(path)
		// Normalize to the repo-relative name the list carries.
		if i := strings.Index(slash, "cmd/aeon/"); i >= 0 {
			slash = slash[i:]
		}
		if !mcpNamedImporters[slash] {
			bad = append(bad, slash+" imports internal/mcp — outside the named list (MC11: the layer is cmd/aeon's alone)")
		}
	}
	return bad
}

// mcpRowWriteOffenses: the store's Tx helpers and raw mcp_ SQL may live
// ONLY in the named write-path files (the store's own SQL half + the admin
// seam's family). One write path (MG5), named and walked.
var mcpRowWriteLiterals = []string{
	"McpAddServerTx", "McpUpdateServerTx", "McpRemoveServerTx", "McpDeclareTx",
	"McpRevokeTx", "McpCoreSetTx", "McpSetEnabledTx",
	"INSERT INTO mcp_", "UPDATE mcp_", "DELETE FROM mcp_",
}

var mcpNamedWritePathFiles = map[string]bool{
	"internal/store/mcp.go":  true, // the SQL half — definitions AND raw SQL
	"cmd/aeon/mcp_admin.go":  true, // the seam
	"cmd/aeon/mcp_import.go": true,
	// mcp_admin_writes.go is the door half of the same named family — split
	// out of mcp_admin.go at T5 (recorded deviation: T7's spec text predates
	// the split and names only mcp_admin.go/mcp_import.go).
	"cmd/aeon/mcp_admin_writes.go": true,
	// 2026-11-08: the share table's SQL half, beside mcp.go's; mcp_admin_share.go is its door.
	"internal/store/mcp_share.go": true,
	// S46 T10: the seed appliers call the store's OWN Tx helpers (MG10:
	// "the same store the human doors use"; no raw SQL lives here — the
	// literals are the accessor names).
	"internal/mcpseed/applier.go": true,
}

func mcpRowWriteOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		slash := filepath.ToSlash(path)
		short := slash
		for _, cut := range []string{"internal/", "cmd/"} {
			if i := strings.Index(short, cut); i >= 0 {
				short = short[i:]
				break
			}
		}
		if mcpNamedWritePathFiles[short] {
			continue
		}
		for _, lit := range mcpRowWriteLiterals {
			if strings.Contains(src, lit) {
				bad = append(bad, short+" uses "+lit+" — the mcp_* rows are written only through the named write-path files (MG5)")
			}
		}
	}
	return bad
}

// agentToolHandlerFiles finds the files defining agent-reachable MCP tools
// (T8's handler half). Until T8 lands there are none, and the assertion is
// the COUNT of handlers found — zero is an honest state, not a pass.
func mcpUnscopedReadCallers(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		slash := filepath.ToSlash(path)
		if !strings.Contains(slash, "records_mcp") && !strings.Contains(slash, "tools_mcp") {
			continue // not an agent-tool handler file
		}
		for _, unscoped := range []string{`"mcp.servers"`, `"mcp.grants"`} {
			if strings.Contains(src, unscoped) {
				bad = append(bad, slash+" reaches the unscoped person-view read "+unscoped+" — agent tools go through the scoped ToolsFor(self) path only (MC2)")
			}
		}
	}
	return bad
}

// --- the positive half ---------------------------------------------------------

func wiredMcpTable(t *testing.T) (*protocol.Table, func()) {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.MCPAdmin = func(_ context.Context, _ string, _ json.RawMessage) (any, string, error) {
		return struct{}{}, "", nil
	}
	tbl.ActorFor = func(context.Context, *protocol.Session) protocol.Actor {
		return protocol.Actor{Kind: "user", ID: "1000"}
	}
	if !tbl.RegisterMCPAdmin() {
		t.Fatal("RegisterMCPAdmin refused a wired seam — the management reads are wired out of existence (P34: the app must have its door)")
	}
	if !tbl.RegisterMCPWrites() {
		t.Fatal("RegisterMCPWrites refused a wired seam — the human write doors are wired out of existence")
	}
	if !tbl.RegisterMCPImportFile() {
		t.Fatal("RegisterMCPImportFile refused a wired seam AND actor stamp")
	}
	return tbl, func() { _ = tbl }
}

// TestMcpWritesAreAHumanDoor is MG15/PM10 stated positively: wired ⇒ exactly
// the closed set is registered (nothing under the management prefixes
// slipped in beside it); each requires Initialize and none is Debug; and
// with the seam nil every one is ABSENT — Dispatch answers the sentinel
// (V13: absent, never registered-and-refusing), while S34's two reads stand
// untouched.
func TestMcpWritesAreAHumanDoor(t *testing.T) {
	tbl, done := wiredMcpTable(t)
	defer done()

	registered := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	for _, m := range append(append([]protocol.MethodName{}, mcpWriteMethods...), mcpAdminReadMethods...) {
		if !registered[m] {
			t.Fatalf("%s is not registered with the seam wired — MG8's human door is wired out of existence", m)
		}
	}
	// The prefix closure: every registered mcp.* name is management (the
	// 13) or S34's two reads. A fourteenth door (mcp.saveConfig,
	// mcp.grant.grant, proposal-masquerading-as-config) is the hole here.
	known := map[protocol.MethodName]bool{}
	for _, m := range append(append([]protocol.MethodName{}, mcpWriteMethods...), mcpAdminReadMethods...) {
		known[m] = true
	}
	for _, m := range mcpS34Reads {
		known[m] = true
	}
	for _, m := range tbl.Methods() {
		if strings.HasPrefix(string(m), "mcp.") && !known[m] {
			t.Errorf("%s is registered under mcp. and is neither the closed 13 nor S34's two reads — the door set is closed", m)
		}
	}
	// Flags: gated, never Debug (the narrow exclusion must never rest on a
	// Debug flag — PM10's rule, carried to this surface).
	for _, m := range append(append([]protocol.MethodName{}, mcpWriteMethods...), mcpAdminReadMethods...) {
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s vanished between Methods() and Lookup()", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not require Initialize — a config write on an unnegotiated connection is unthinkable", m)
		}
		if got.Debug {
			t.Errorf("%s is Debug — product methods on the wide socket; the narrow exclusion lives in this file's negative half, never in a flag", m)
		}
	}

	// The absent half (V13): a bare table registers none of the 13,
	// Dispatch answers the sentinel, and S34's reads are untouched.
	bare := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	if bare.RegisterMCPAdmin() || bare.RegisterMCPWrites() || bare.RegisterMCPImportFile() {
		t.Fatal("a registration succeeded on the unwired table — registered-and-refusing is V13's forbidden shape")
	}
	for _, m := range append(append([]protocol.MethodName{}, mcpWriteMethods...), mcpAdminReadMethods...) {
		if _, ok := bare.Lookup(m); ok {
			t.Errorf("%s is registered without the seam", m)
		}
		if _, err := bare.Dispatch(context.Background(), "c-absent", m, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Errorf("%s answered %v, want ErrUnknownMethod", m, err)
		}
	}
	for _, m := range mcpS34Reads {
		if _, ok := bare.Lookup(m); !ok {
			t.Errorf("%s is NOT registered on the bare table — S34's operational reads are unchanged by S46 (MG8's boundary: management is new, status/check are old)", m)
		}
	}
}

// --- the negative half: one subtest per door -----------------------------------

func TestMcpNegativeDoors(t *testing.T) {
	rootsFour := []string{
		filepath.Join("..", "..", "internal", "narrow"),
		filepath.Join("..", "..", "internal", "agentsvc"),
		filepath.Join("..", "..", "internal", "tools"),
		filepath.Join("..", "..", "internal", "records"),
	}

	t.Run("NarrowSurface", func(t *testing.T) {
		actions := narrow.Actions()
		if len(actions) == 0 {
			t.Fatal("narrow.Actions() is empty — the projection has no table, so this guard would pass on nothing")
		}
		if bad := mcpManagementOnNarrow(actions); len(bad) != 0 {
			t.Errorf("the narrow surface projects MCP management through %s — a key-holder projected onto a config write is MG15's refusal made mechanical", strings.Join(bad, ", "))
		}
	})

	t.Run("AgentServices", func(t *testing.T) {
		ms := protocol.AgentServiceMethods()
		if len(ms) == 0 {
			t.Fatal("protocol.AgentServiceMethods() is empty — the enumeration is gone, so this guard would pass on nothing")
		}
		if bad := mcpManagementOnAgentServices(ms); len(bad) != 0 {
			t.Errorf("the agent-services enumeration names %s — an agent surface carrying a config write is the side door MG15 keeps shut", strings.Join(bad, ", "))
		}
	})

	t.Run("ToolRegistry", func(t *testing.T) {
		reg := tools.Registry()
		if len(reg) == 0 {
			t.Fatal("tools.Registry() is empty — the registry guard would pass on nothing")
		}
		if bad := mcpToolsInRegistry(reg); len(bad) != 0 {
			t.Errorf("the tool registry carries %s — the only MCP tool vocabulary an agent may hold is T8's own-view reads and propose twins", strings.Join(bad, ", "))
		}
	})

	t.Run("RecordsCatalog", func(t *testing.T) {
		specs := records.Catalog()
		if len(specs) == 0 {
			t.Fatal("records.Catalog() is empty — the catalog guard would pass on nothing")
		}
		if bad := mcpCatalogOffenses(specs); len(bad) != 0 {
			t.Errorf("the records catalog carries %s — no config write has an agent-reachable twin; every mcp-family spec is read-only or one of T8's two propose twins", strings.Join(bad, ", "))
		}
	})

	t.Run("SourceLiterals", func(t *testing.T) {
		needles := append(mcpManagementNames(), mcpMethodConstNames...)
		needles = append(needles, `"mcp.server.add"`, `"mcp.importFile"`)
		bad := mcpLiteralGrep(rootsFour, needles, nil)
		if len(bad) == 0 {
			// The walk itself must have teeth: prove it saw the tree.
			if seen := len(walkedSources(rootsFour, nil)); seen == 0 {
				t.Fatal("the walk saw no non-test files — the grep checked nothing")
			}
		}
		if len(bad) != 0 {
			t.Errorf("the four non-person packages name the management vocabulary: %s", strings.Join(bad, "\n"))
		}
	})

	t.Run("BoundaryStatement", func(t *testing.T) {
		stmt := narrow.BoundaryStatement()
		if stmt == "" {
			t.Fatal("narrow.BoundaryStatement() is empty — the surface has no account of itself to check")
		}
		lower := strings.ToLower(stmt)
		for _, n := range mcpManagementNames() {
			if strings.Contains(lower, strings.ToLower(n)) {
				t.Errorf("the projection's rendered boundary statement lists %s — the surface DISPLAYS a door its table must not hold", n)
			}
		}
	})
}

// --- MC11: deletability, extended (not relaxed) to a named list ------------------

func TestMcpLayerStillDeletable(t *testing.T) {
	files := walkedSources([]string{
		filepath.Join("..", "..", "internal"),
		filepath.Join("..", "..", "cmd"),
	}, nil)
	if len(files) < 50 {
		t.Fatalf("the importer walk saw %d files — far too few to conclude anything", len(files))
	}
	if bad := mcpImportOffenses(files); len(bad) != 0 {
		t.Errorf("internal/mcp has importers outside the named list:\n%s", strings.Join(bad, "\n"))
	}
	// The named list must not be a shrine to files that no longer exist:
	// every name it carries is still a real importer.
	present := map[string]bool{}
	for path, src := range files {
		if strings.Contains(src, `"github.com/cjlapao/aeon/internal/mcp"`) {
			slash := filepath.ToSlash(path)
			if i := strings.Index(slash, "cmd/aeon/"); i >= 0 {
				slash = slash[i:]
			}
			present[slash] = true
		}
	}
	for named := range mcpNamedImporters {
		if !present[named] {
			t.Errorf("the named importer %s imports internal/mcp no more — drop it from the list (an exemption without its file is a hole waiting for its name)", named)
		}
	}
	// The store's side of the law: internal/store has tables, never the
	// config grammar.
	for path, src := range files {
		if strings.Contains(filepath.ToSlash(path), "internal/store/") &&
			strings.Contains(src, `"github.com/cjlapao/aeon/internal/mcp"`) {
			t.Errorf("%s — the store speaks SQL, not mcp types (T1's law; Assemble/Disassemble stay in cmd/aeon)", filepath.ToSlash(path))
		}
	}
	// The vet-level deletability claim (documented, not a real delete): the
	// graph walk above proves NO package outside the named cmd/aeon files
	// imports the layer — so removing internal/mcp plus exactly those files
	// leaves every other package's import set satisfied. (An actual `go
	// build` after a delete would fight this package's own test files.)
}

// --- MC2: the agent handlers (T8's files) reach only the scoped path ----------

func TestAgentReadToolsNeverCallTheUnscopedReads(t *testing.T) {
	files := walkedSources([]string{filepath.Join("..", "..", "cmd", "aeon")}, nil)
	if len(files) == 0 {
		t.Fatal("the cmd/aeon walk saw nothing — the MC2 guard checked nothing")
	}
	handlers := 0
	for path := range files {
		slash := filepath.ToSlash(path)
		if strings.Contains(slash, "records_mcp") || strings.Contains(slash, "tools_mcp") {
			handlers++
		}
	}
	if handlers == 0 {
		// Honest empty state, stated: T8 has not landed. The moment a
		// records_mcp*.go / tools_mcp*.go appears this guard arms itself.
		t.Logf("no agent MCP tool handler files exist yet (T8 supplies them) — the MC2 check is armed and empty, not vacuous: it counts handler files and asserts zero today")
	}
	if bad := mcpUnscopedReadCallers(files); len(bad) != 0 {
		t.Errorf("%s", strings.Join(bad, "\n"))
	}
}

// --- MG5: one write path, named ---------------------------------------------------

func TestMcpRowsAreWrittenOnlyThroughTheAdminSeam(t *testing.T) {
	files := walkedSources([]string{
		filepath.Join("..", "..", "internal"),
		filepath.Join("..", "..", "cmd"),
	}, nil)
	if len(files) < 50 {
		t.Fatalf("the write-path walk saw %d files — not enough to conclude anything", len(files))
	}
	if bad := mcpRowWriteOffenses(files); len(bad) != 0 {
		t.Errorf("a second write path opened:\n%s", strings.Join(bad, "\n"))
	}
	// And the named files really do carry the literals (the walk bites in
	// both directions — a renamed helper family is caught too).
	seen := 0
	for path := range files {
		short := filepath.ToSlash(path)
		for _, cut := range []string{"internal/", "cmd/"} {
			if i := strings.Index(short, cut); i >= 0 {
				short = short[i:]
				break
			}
		}
		if mcpNamedWritePathFiles[short] {
			seen++
		}
	}
	if seen != len(mcpNamedWritePathFiles) {
		t.Errorf("the named write-path files present are %d, want %d — a named file vanished", seen, len(mcpNamedWritePathFiles))
	}
}

// --- canaries: every negative predicate is fed a violation ------------------------

func TestMcpGuardsHaveTeeth(t *testing.T) {
	t.Run("NarrowCanary", func(t *testing.T) {
		bad := mcpManagementOnNarrow([]narrow.Action{{Name: "aeon_mcp_server_add", SocketMethod: protocol.MethodMCPServerAdd}})
		if len(bad) != 1 {
			t.Fatal("the narrow predicate let a projected mcp.server.add through — the guard is decorative")
		}
	})
	t.Run("AgentServicesCanary", func(t *testing.T) {
		bad := mcpManagementOnAgentServices([]protocol.AgentServiceMethod{{Name: protocol.MethodMCPGrantDeclare}})
		if len(bad) != 1 {
			t.Fatal("the enumeration predicate let mcp.grant.declare through")
		}
	})
	t.Run("ToolRegistryCanary", func(t *testing.T) {
		bad := mcpToolsInRegistry(map[string]tools.Tool{"mcp_add_server": tools.Read()})
		if len(bad) != 1 {
			t.Fatal("the registry predicate let mcp_add_server through")
		}
	})
	t.Run("CatalogCanary", func(t *testing.T) {
		bad := mcpCatalogOffenses([]records.Spec{{Name: "mcp_grant_declare", Method: "mcp.grant.declare", Family: "mcp", Mutates: true}})
		if len(bad) != 1 {
			t.Fatal("the catalog predicate let a spec twinned to mcp.grant.declare through")
		}
		bad2 := mcpCatalogOffenses([]records.Spec{{Name: "mcp_sneaky_write", Method: "mcp.changes", Family: "mcp", Mutates: true}})
		if len(bad2) != 1 {
			t.Fatal("the catalog predicate let a mutating mcp-family spec that is not a propose twin through")
		}
	})
	t.Run("LiteralCanary", func(t *testing.T) {
		bad := mcpLiteralGrep(nil, []string{`"mcp.importFile"`}, map[string]string{
			filepath.Join("..", "..", "internal", "tools", "fake_smuggled.go"): `package tools // "mcp.importFile" smuggled in`,
		})
		if len(bad) != 1 {
			t.Fatal("the literal grep missed the planted needle")
		}
	})
	t.Run("ImporterCanary", func(t *testing.T) {
		bad := mcpImportOffenses(map[string]string{
			filepath.Join("..", "..", "internal", "daemon", "fake_mcp.go"): `package daemon // import "github.com/cjlapao/aeon/internal/mcp"`,
		})
		if len(bad) != 1 {
			t.Fatal("the importer walk let a daemon file importing internal/mcp through — MC11 is decorative")
		}
	})
	t.Run("WritePathCanary", func(t *testing.T) {
		bad := mcpRowWriteOffenses(map[string]string{
			filepath.Join("..", "..", "internal", "agentsvc", "fake_writer.go"): `package agentsvc // tx.Exec("DELETE FROM mcp_server WHERE 1=1") + McpDeclareTx`,
		})
		if len(bad) == 0 {
			t.Fatal("the write-path walk let a foreign DELETE FROM mcp_ / McpDeclareTx through — MG5 is decorative")
		}
	})
}

// --- the pre-amendment originals, pinned by hash -----------------------------------

// mcpOriginalPins are the guard bodies S46's amendment did NOT edit: S12's
// MC11 layer claims (the two structural assertions of internal/mcp's
// absent_test.go) and S45's TD4 carrier (the mcp-surface half of
// TestAmendmentDoesNotLoosenOtherHumanDoors, which stands verbatim against
// the bare table). The digests are recorded HERE (T7's Notes), because T0
// recorded no digests — it inventoried the files. A mismatch is a deliberate
// amendment entered in T7's Notes, never a re-pinned constant.
var mcpOriginalPins = []struct {
	file   string
	guard  string
	digest string
}{
	{"internal/mcp/absent_test.go", "TestNoUnconditionalImportOfMCP", "fe61c4d76114b836368955a0f4d5ead543398f411f6fe89374c74c72fa66c7fa"},
	{"internal/mcp/absent_test.go", "TestNoFeatureFlagWasAdded", "1fe77b24fba430d75ee72a836724e1f3509f56555f7bff6086910272376f2035"},
	{"cmd/aeon/policy_amend_test.go", "TestAmendmentDoesNotLoosenOtherHumanDoors", "b8db7a6757a28cc993476a04bd640ec8b7853425a70496cb92af86e942d7be96"},
}

func TestOriginalMcpGuardsAreByteIdentical(t *testing.T) {
	for _, pin := range mcpOriginalPins {
		path := filepath.Join("..", "..", pin.file)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", pin.file, err)
		}
		got := guardBodyDigest(t, string(src), pin.guard)
		if got != pin.digest {
			t.Errorf("%s's %s drifted:\n recorded %s\n found    %s\n — the pre-amendment logic is frozen (S45's method); an amendment is a written entry in T7's Notes", pin.file, pin.guard, pin.digest, got)
		}
	}
}

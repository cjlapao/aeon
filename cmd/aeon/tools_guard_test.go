package main

// tools_guard_test.go — S47 T9: the slice's claims made mechanical, each shown
// to fail.
//
// Eight guards, each a claim about what this slice does NOT expose and does NOT
// read, and each backed by a predicate the canary feeds a synthetic violation —
// a guard nobody has seen fail is a rumour. The shape is S46's (mcp_amend_test.go):
// predicates are pure functions, and the real assertion and the canary run the
// SAME code path, so "the canary passed" means the guard bites.
//
// Nothing here edits an existing guard. S10's, S35's, S45's and S46's guards,
// `TestRegistryExactNames`, T1's completeness tests and T6's classification tests
// stay exactly as they are; this file re-asserts the S47-named set from
// `cmd/aeon`, where the sibling slices' guards live, and adds the two claims only
// a sibling can make — that no S47 read reaches a door it was never mounted on,
// and that every registered tool name is attributed to the slice that brought it.
//
// The claims, in the order the guards make them:
//
//  1. TC1 — this slice ships reads: the S47 methods are exactly the nine named
//     below (T5's four, T6's two, T7's two own-view twins, and T10's vocabulary
//     door, added under the operator ruling of 2026-11-02 recorded at
//     `protocol.MethodToolsVocabulary`), each `RequiresInitialize`, none `Debug`,
//     every one classified `read`; no
//     write-shaped name anywhere in the `tools.`/`surface.` space; no `tools`
//     family catalog spec mutates; no CLI verb mutates; the `tool` table has a
//     closed set of readers.
//  2. TC6 — none of it reaches the narrow projection, the agent-services
//     enumeration, the tool registry beyond the two named own-view tools, or the
//     four packages that must not even name the methods.
//  3. TC2 — the executors and the input schemas are code: the runtime's packages
//     reference no `tool` row and no `toolseed`, and tampering a row cannot move
//     the spec the model is handed.
//  4. TC3 — every tool declares its capabilities, in every seam combination, so a
//     conditional tool cannot dodge the vocabulary.
//  5. TC6 — every method a real daemon registers is classified, against the table
//     `daemon.New` actually built here.
//  6. MC2 — the agent's own view reaches only the scoped path, and a peer's
//     private world is absent from `tools_mine`, `tool_show` and the S46 own-view
//     doors TOGETHER, in one world.
//  7. —   — every registered tool name is attributed to the slice that brought it.
//  8. —   — each predicate above is fed a violation and must report it.

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/graph"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/search"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// s47Methods is the closed set this slice adds. Nine doors, no more: a tenth
// `tools.*`/`surface.*` method is either a write (TC1's refusal) or a read nobody
// classified (TC6's). The set was amended once, deliberately: TC10 froze the
// surface at eight, and T10's panel needed capability meanings the design said
// were "sent by the daemon" but no door carried — the operator ruled in favour of
// sending them from the code that owns them (2026-11-02), which is this line. An
// amendment that adds a name is allowed only with a sentence saying who ruled it
// and why; a name arriving without one is the guard doing its job.
var s47Methods = []protocol.MethodName{
	protocol.MethodToolsList, protocol.MethodToolsGet, protocol.MethodToolsByAgent, protocol.MethodToolsStatus,
	protocol.MethodToolsVocabulary,
	protocol.MethodToolsMineList, protocol.MethodToolsMineShow,
	protocol.MethodSurfaceList, protocol.MethodSurfaceGet,
}

// s47NeighbourMethods are registered `tools.*` names this slice neither owns nor
// may change — named so the positive assertion is exact rather than a prefix
// match that silently admits a newcomer.
var s47NeighbourMethods = map[string]string{
	string(protocol.MethodToolsGrants): "S34's agent-facing grants read (S46 T8 changed what it reads; the method is not this slice's)",
	// S74 T2: a read over the catalog that previews which tools a ToolAccess pattern names.
	string(protocol.MethodToolsMatchPattern): "S74's pattern preview: a read, matched by policy.MatchToolAddress",
}

// toolsWriteShapeRe is the write-shaped name pattern: any `tools.`/`surface.`
// method that reads like a change is an offense, because TC1 says this slice
// ships no write method, tool or verb.
var toolsWriteShapeRe = regexp.MustCompile(`^(tools|surface)\.(add|create|update|delete|remove|enable|disable|set|grant|revoke|import|apply|commit|write|seed|repair|sync|clear|prune|install|uninstall)`)

// toolRowReaderFiles is the closed set of non-test files allowed to reach the
// `tool` table's accessors: the store's own definition, and the catalog adapter
// (with the entry builder it shares its world with). The seed package writes the
// row through the applier and is allowed by directory. Anything else is a second
// reader of a table the runtime must never consult (TC2).
var toolRowReaderFiles = map[string]bool{
	"internal/store/tool.go":                   true,
	"internal/daemon/tools_catalog.go":         true,
	"internal/daemon/tools_catalog_entries.go": true,
}

// toolRowAccessorRe names the accessors and row types whose call sites are
// restricted above.
var toolRowAccessorRe = regexp.MustCompile(`\b(ToolList|ToolGet|ToolCount|ToolUpsertTx|ToolRow|SeedToolRow|SeedToolRetire)\b`)

// runtimeReadRoots are the packages TC2 keeps away from the `tool` table: the
// toolset and its specs, the step runtime, the model layer, and the step file
// that assembles what the model is handed.
var runtimeReadRoots = []string{
	filepath.Join("..", "..", "internal", "tools"),
	filepath.Join("..", "..", "internal", "runtime"),
	filepath.Join("..", "..", "internal", "model"),
	filepath.Join("..", "..", "internal", "daemon", "step.go"),
}

// runtimeReadNeedles are the names those packages must never spell.
var runtimeReadNeedles = []string{"store.Tool", "ToolRow", "toolseed."}

// s47LiteralNeedles are the six person-facing reads whose names must appear in
// none of the four packages that decide what an agent or a narrow caller reaches.
// The two own-view methods are deliberately absent from the needles: they are
// agent-scoped by design, and their names belong in the records catalog.
var s47LiteralNeedles = []string{
	`"tools.list"`, `"tools.get"`, `"tools.byAgent"`, `"tools.status"`, `"surface.list"`, `"surface.get"`,
	`protocol.MethodToolsList`, `protocol.MethodToolsGet`, `protocol.MethodToolsByAgent`, `protocol.MethodToolsStatus`,
	`protocol.MethodSurfaceList`, `protocol.MethodSurfaceGet`,
}

// s47LiteralRoots are those four packages.
var s47LiteralRoots = []string{
	filepath.Join("..", "..", "internal", "narrow"),
	filepath.Join("..", "..", "internal", "agentsvc"),
	filepath.Join("..", "..", "internal", "tools"),
	filepath.Join("..", "..", "internal", "records"),
}

// ownViewHandlerFiles are the agent-facing handler files whose scoping MC2
// governs: T7's own-view implementation and the arms that stamp the step's agent.
var ownViewHandlerFiles = []string{"tools_catalog_mine", "records_tools"}

// ownViewUnscopedNeedles are the unscoped person doors an agent tool must never
// reach: the four `tools.*` reads answer about every agent, which is exactly what
// the own view exists not to do.
var ownViewUnscopedNeedles = []string{
	`"tools.list"`, `"tools.get"`, `"tools.byAgent"`, `"tools.status"`,
	".ToolsList(", ".ToolsGet(", ".ToolsByAgent(", ".ToolsStatus(",
}

// --- guard 1: TC1, positive and negative -------------------------------------

func TestToolsCatalogIsReadOnly(t *testing.T) {
	tbl := toolsRoadTable(t)

	// Positive: what is registered in the tools./surface. space is exactly the
	// nine plus the one named neighbour, each initialised and none Debug.
	var facts []methodFact
	for _, name := range tbl.Methods() {
		m, ok := tbl.Lookup(name)
		if !ok {
			continue
		}
		facts = append(facts, methodFact{Name: string(name), RequiresInitialize: m.RequiresInitialize, Debug: m.Debug})
	}
	if bad := s47MethodSetOffenses(facts); len(bad) > 0 {
		t.Errorf("the read-only claim broke:\n%s", strings.Join(bad, "\n"))
	}

	// Negative: no write-shaped method registered anywhere in the space.
	for _, name := range tbl.Methods() {
		if s47NeighbourMethods[string(name)] != "" {
			continue // named above; the exemption is visible, not silent
		}
		if toolsWriteShapeRe.MatchString(string(name)) {
			t.Errorf("%s is registered and write-shaped — TC1 adds no write method, tool or verb", name)
		}
	}

	// Every S47 method's classification is `read`.
	if bad := s47SurfaceReadOffenses(tbl.Surface()); len(bad) > 0 {
		t.Errorf("the surface classification is not what TC1 promises:\n%s", strings.Join(bad, "\n"))
	}

	// The `tools` family holds the two reads and nothing that mutates.
	if bad := toolsCatalogSpecOffenses(records.Catalog()); len(bad) > 0 {
		t.Errorf("the records catalog carries something it must not:\n%s", strings.Join(bad, "\n"))
	}

	// The CLI verb table carries no mutating verb.
	var verbs []string
	for _, c := range toolsVerbTable {
		verbs = append(verbs, c.verb)
	}
	if bad := toolsVerbOffenses(verbs); len(bad) > 0 {
		t.Errorf("a CLI verb reads like a change:\n%s", strings.Join(bad, "\n"))
	}

	// The `tool` table has a closed set of readers — and the walk has teeth in
	// both directions: enough files, and every named reader file really present
	// (a renamed reader would otherwise make the guard true by absence).
	files := walkedSources([]string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")}, nil)
	if len(files) < 50 {
		t.Fatalf("the reader walk saw %d files — not enough to conclude anything", len(files))
	}
	if bad := toolRowReaderOffenses(files); len(bad) > 0 {
		t.Errorf("a second reader of the tool table opened:\n%s", strings.Join(bad, "\n"))
	}
	seen := 0
	for path := range files {
		if toolRowReaderFiles[repoRelGuard(path)] {
			seen++
		}
	}
	if seen != len(toolRowReaderFiles) {
		t.Errorf("only %d of the %d named reader files are present — a named reader vanished, and the guard would now pass on its absence", seen, len(toolRowReaderFiles))
	}
}

// --- guard 2: TC6, the doors it was never mounted on --------------------------

func TestToolsCatalogNeverReachesNarrowOrAgentSurfaces(t *testing.T) {
	tbl := toolsRoadTable(t)
	// The doors really are on the socket, so everything below is a statement
	// about the OTHER projections and not an accident of an unwired table.
	for _, m := range s47Methods {
		if _, ok := tbl.Lookup(m); !ok {
			t.Fatalf("%s is not registered — the negative assertions would pass on nothing", m)
		}
	}

	t.Run("NarrowProjection", func(t *testing.T) {
		if bad := toolsOnNarrow(narrow.Actions()); len(bad) > 0 {
			t.Errorf("%s", strings.Join(bad, "\n"))
		}
	})
	t.Run("AgentServices", func(t *testing.T) {
		if bad := toolsOnAgentServices(protocol.AgentServiceMethods()); len(bad) > 0 {
			t.Errorf("%s", strings.Join(bad, "\n"))
		}
	})
	t.Run("Registry", func(t *testing.T) {
		// The registry gained exactly the two named own-view tools, arrived the
		// way every catalog tool arrives (through the projection, not hand), and
		// nothing write-shaped came beside them.
		reg := tools.Registry()
		for name := range reg {
			if toolsWriteShapeRe.MatchString(name) {
				t.Errorf("%s is a write-shaped tool name in the registry — TC1", name)
			}
		}
		held := 0
		for _, s := range records.Catalog() {
			if !strings.EqualFold(s.Family, "tools") {
				continue
			}
			held++
			if _, ok := reg[s.Name]; !ok {
				t.Errorf("%s is a catalog spec with no registry entry — the projection did not run", s.Name)
			}
		}
		if held != 2 {
			t.Errorf("the tools family holds %d specs, want exactly the two own-view reads", held)
		}
	})
	t.Run("LiteralsInForeignPackages", func(t *testing.T) {
		if bad := toolsLiteralOffenses(walkedSources(s47LiteralRoots, nil), s47LiteralNeedles); len(bad) > 0 {
			t.Errorf("a package that decides what an agent or a narrow caller reaches names a person's read:\n%s", strings.Join(bad, "\n"))
		}
		if len(walkedSources(s47LiteralRoots, nil)) < 10 {
			t.Fatal("the literal walk saw almost nothing — the guard would pass on an empty tree")
		}
	})
	t.Run("BoundaryStatement", func(t *testing.T) {
		stmt := narrow.BoundaryStatement()
		for _, m := range s47Methods {
			if strings.Contains(stmt, string(m)) {
				t.Errorf("the narrow boundary statement names %s — it must not, because nothing in that projection reaches it", m)
			}
		}
	})
}

// --- guard 3: TC2, the executors and schemas stay code ------------------------

func TestExecutorAndSchemaAreNeverReadFromTheDatabase(t *testing.T) {
	files := walkedSources(runtimeReadRoots, nil)
	if len(files) < 20 {
		t.Fatalf("the TC2 walk saw %d files — not enough to conclude anything", len(files))
	}
	if bad := runtimeRowOffenses(files); len(bad) > 0 {
		t.Errorf("the runtime reached for the row:\n%s", strings.Join(bad, "\n"))
	}

	// The behavioural half: write a lie into every stored content column of one
	// row and the spec the model is handed does not move — the assertion T5 made
	// from inside the daemon, re-made from the sibling's side where the runtime
	// lives. The catalog, meanwhile, reports the disagreement instead of
	// believing what it was handed (TC11).
	st := toolsRoadStore(t)
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	before := string(tools.CanonicalSchema(mustRegistrySpec(t, "read")))
	beforeHash := tools.SchemaHash(mustRegistrySpec(t, "read"))
	if err := tamperToolRow(ctx, st, "read"); err != nil {
		t.Fatalf("tamper the row: %v", err)
	}
	if got := string(tools.CanonicalSchema(mustRegistrySpec(t, "read"))); got != before {
		t.Errorf("the model-facing schema moved when the ROW was tampered with — TC2 is broken end to end\nbefore: %s\nafter:  %s", before, got)
	}
	if tools.SchemaHash(mustRegistrySpec(t, "read")) != beforeHash {
		t.Error("the schema hash moved with it")
	}
	svc := newCatalogForGuard(t, st)
	res, gerr := svc.ToolsGet(ctx, "read")
	if gerr != nil {
		t.Fatalf("tools.get after the tampering: %v", gerr)
	}
	if string(res.Tool.Schema) != before {
		t.Errorf("the catalog served the tampered row's schema — TC2's whole argument is that the row never serves anything")
	}
	if res.Tool.RowCurrent {
		t.Error("the catalog called a tampered row current — TC11 reports drift, it does not wave it through")
	}
}

func mustRegistrySpec(t *testing.T, name string) model.ToolSpec {
	t.Helper()
	tool, ok := tools.Registry()[name]
	if !ok {
		t.Fatalf("%s is not registered — the guard's own fixture moved", name)
	}
	return tool.Spec
}

// tamperToolRow writes a lie into the row's stored content columns through the
// store's own write door: the row this guard tampers with is a row the product
// writes, not one the test invented a shape for.
func tamperToolRow(ctx context.Context, st *store.Store, name string) error {
	tampered, err := json.Marshal(map[string]any{
		"type":       "object",
		"properties": map[string]any{"evil": map[string]string{"type": "string"}},
	})
	if err != nil {
		return err
	}
	row := store.ToolRow{
		Name:         name,
		Family:       "workspace",
		Description:  "a lie the row tells about what the tool does",
		Schema:       string(tampered),
		SchemaHash:   strings.Repeat("0", 64),
		RerunClass:   "safe",
		Capabilities: `["workspace_read"]`,
		Governed:     `["tool_access"]`,
		Aliases:      `[]`,
		Origin:       "system",
		Version:      99,
		CreatedAt:    "2026-11-02T12:00:00Z",
		UpdatedAt:    "2026-11-02T12:00:00Z",
	}
	return st.SeedWithTx(ctx, func(tx *sql.Tx) error { return st.ToolUpsertTx(tx, row) })
}

// newCatalogForGuard builds the adapter the daemon's table speaks and the CLI's
// direct door builds, over this store.
func newCatalogForGuard(t *testing.T, st *store.Store) protocol.ToolsCatalog {
	t.Helper()
	engine, err := daemon.NewSeedEngine(st, seed.Options{}, mcpSeedValidator(st))
	if err != nil {
		t.Fatalf("seed engine: %v", err)
	}
	svc := daemon.NewToolsCatalogService(daemon.Options{
		Store:    st,
		Seed:     engine,
		MCPAdmin: mcpAdminHandleFor(st, newMCPProbeCache()),
	})
	if svc == nil {
		t.Fatal("the catalog service is nil over a live store — the nil guard fired where it should not have")
	}
	return svc
}

// --- guard 4: TC3, the vocabulary, every seam combination ---------------------

// graphSeamForGuard satisfies graph.QueryService by promotion. None of its
// methods is ever called: the guard asks only whether the seam is WIRED, which is
// what changes which tools the registry holds.
type graphSeamForGuard struct{ *graph.Service }

func restoreSeams(t *testing.T, withSearch, withGraph bool) {
	t.Helper()
	prevSearch, prevGraph := tools.SearchRegistered(), tools.GraphRegistered()
	if withSearch {
		tools.SetSearchResolver(func(context.Context, string, string, string, int) (search.SearchAnswer, error) {
			return search.SearchAnswer{}, nil
		})
	} else {
		tools.SetSearchResolver(nil)
	}
	if withGraph {
		tools.SetGraphService(graphSeamForGuard{})
	} else {
		tools.SetGraphService(nil)
	}
	t.Cleanup(func() {
		if prevSearch {
			tools.SetSearchResolver(func(context.Context, string, string, string, int) (search.SearchAnswer, error) {
				return search.SearchAnswer{}, nil
			})
		} else {
			tools.SetSearchResolver(nil)
		}
		if prevGraph {
			tools.SetGraphService(graphSeamForGuard{})
		} else {
			tools.SetGraphService(nil)
		}
	})
}

func TestEveryToolDeclaresCapabilities(t *testing.T) {
	vocabulary := map[string]bool{}
	for _, c := range tools.Capabilities() {
		vocabulary[string(c)] = true
	}
	if len(vocabulary) == 0 {
		t.Fatal("the capability vocabulary is empty — the completeness check would pass on anything")
	}
	// T1's two named, reasoned exceptions: tools that touch no store and reach no
	// authority, whose empty set is a declaration and not an omission.
	allowEmpty := map[string]bool{"todo": true, "report_size": true}

	for _, combo := range []struct {
		name           string
		search, graphB bool
	}{
		{name: "no-retrieval", search: false, graphB: false},
		{name: "retrieval", search: true, graphB: false},
		{name: "retrieval-and-graph", search: true, graphB: true},
	} {
		t.Run(combo.name, func(t *testing.T) {
			restoreSeams(t, combo.search, combo.graphB)
			// The combination tested must be the combination named, or the run
			// proves nothing about a conditional tool.
			if tools.SearchRegistered() != combo.search {
				t.Fatalf("the search seam did not take: registered=%v want %v", tools.SearchRegistered(), combo.search)
			}
			if tools.GraphRegistered() != combo.graphB {
				t.Fatalf("the graph seam did not take: registered=%v want %v", tools.GraphRegistered(), combo.graphB)
			}
			meta := tools.AllMeta()
			var pairs []capabilityPair
			for _, name := range tools.ToolNames() {
				m, ok := meta[name]
				var caps []string
				for _, c := range m.Capabilities {
					caps = append(caps, string(c))
				}
				pairs = append(pairs, capabilityPair{Name: name, Capabilities: caps, Declared: ok})
			}
			if len(pairs) < 60 {
				t.Fatalf("the registry holds %d tools — too few for the completeness claim to mean anything", len(pairs))
			}
			if bad := capabilityOffenses(pairs, vocabulary, allowEmpty); len(bad) > 0 {
				t.Errorf("with the seams %s, the capability claim broke:\n%s", combo.name, strings.Join(bad, "\n"))
			}
			// The conditional tools must actually be present in the combinations
			// that wire them: a guard that silently checked the same registry
			// three times would be decoration.
			names := strings.Join(tools.ToolNames(), " ")
			if combo.search && !strings.Contains(names, "search") {
				t.Error("the search seam is wired and `search` is not registered")
			}
			if combo.graphB && !strings.Contains(names, "graph") {
				t.Error("the graph seam is wired and `graph` is not registered")
			}
		})
	}
}

// --- guard 5: TC6, every registered method is classified ----------------------

// guardWiredDaemon builds the daemon as the entry point wires it — store, the one
// seed engine, the MCP services, the desk effects, the proposal validator, the
// narrow actions — so "registered" means what it means in production rather than
// what a hand-built table happens to hold.
func guardWiredDaemon(t *testing.T) *daemon.Daemon {
	t.Helper()
	st := toolsRoadStore(t)
	engine, err := daemon.NewSeedEngine(st, seed.Options{}, mcpSeedValidator(st))
	if err != nil {
		t.Fatalf("seed engine: %v", err)
	}
	opts := daemon.Options{
		Store:            st,
		Seed:             engine,
		ValidateProposal: validateProposalPayload,
		Effects:          func() (*desk.Registry, error) { return desk.EffectsFor(openDeskBoundary) },
	}
	if err := wireMCPServices(st, &opts); err != nil {
		t.Fatalf("wire the MCP services: %v", err)
	}
	d := daemon.New(opts)
	d.Table.NarrowMethods = func() map[protocol.MethodName]string {
		out := map[protocol.MethodName]string{}
		for _, a := range narrow.Actions() {
			out[a.SocketMethod] = a.Name
		}
		return out
	}
	return d
}

func TestEveryMethodIsClassified(t *testing.T) {
	d := guardWiredDaemon(t)
	var registered []string
	for _, m := range d.Table.Methods() {
		registered = append(registered, string(m))
	}
	sort.Strings(registered)
	if len(registered) < 200 {
		t.Fatalf("the wired table registers %d methods — the daemon is not wired the way serve wires it, and the completeness claim would be hollow", len(registered))
	}
	if bad := unclassifiedMethodOffenses(registered, d.Table.Surface()); len(bad) > 0 {
		t.Errorf("a method a real daemon registers carries no classification:\n%s", strings.Join(bad, "\n"))
	}
	// And the nine S47 doors are in there, classified, so the check above is not
	// passing on a table that never had them.
	for _, want := range s47Methods {
		found := false
		for _, e := range d.Table.Surface() {
			if e.Method == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s is missing from the fully wired classification", want)
		}
	}
}

// --- guard 6: MC2, the own view, both families in one world -------------------

// ownViewReader is the shape T7's adapter answers through. Asserting it inline
// keeps the guard honest: if the methods are renamed or the scoping moves, this
// fails rather than skipping.
type ownViewReader interface {
	ToolsMine(ctx context.Context, agent string) (protocol.ToolsMineResult, error)
	ToolShow(ctx context.Context, agent, name string) (protocol.ToolShowResult, error)
}

func TestAgentToolsAreOwnViewOnly(t *testing.T) {
	// The static half first: T7's handler files reach only the scoped path.
	files := walkedSources([]string{filepath.Join("..", "..", "internal", "daemon")}, nil)
	if len(files) < 20 {
		t.Fatalf("the MC2 walk saw %d files — nothing was checked", len(files))
	}
	sawHandler := false
	for path := range files {
		for _, name := range ownViewHandlerFiles {
			if strings.Contains(repoRelGuard(path), name) {
				sawHandler = true
			}
		}
	}
	if !sawHandler {
		t.Fatal("neither own-view handler file was walked — the MC2 static check is armed and empty, which is not the same as passing")
	}
	if bad := ownViewScopingOffenses(files); len(bad) > 0 {
		t.Errorf("%s", strings.Join(bad, "\n"))
	}

	// The behavioural half, and the part only this slice can add: the planted
	// private world, checked across the two families TOGETHER — S46's own-view
	// doors and S47's two own-view reads, in one store and one run, because a
	// leak that needs both doors to matter is exactly what per-family tests miss.
	// SD7 first, because it decides everything below: a name on a CUSTOM agent's
	// row is not a capability the app can dispatch, so a non-system agent's own view
	// is honestly empty (and empty is the right answer, not a missing one). The
	// agent whose view this guard reads is therefore seated the way a dispatchable
	// one is — origin `system`, an authored allow-list — BEFORE `mineWorld` runs,
	// which tolerates an existing slug. Registered is not granted: the allow-list
	// names the three default tools only; the two MCP tools below are reachable
	// solely through the operator's `tool_access` rows.
	st, cache := mineWorld(t, false)
	// SD7 first, because it decides everything below: a name on a CUSTOM agent's
	// row is not a capability the app can dispatch, so a non-system agent's own view
	// is honestly empty — and empty is the right answer, not a missing one. The
	// agent whose view this guard reads is therefore seated the way a dispatchable
	// one is: origin `system` and an authored allow-list. Registered is not granted:
	// the allow-list names the three default tools only, and the two MCP tools below
	// are reachable solely through the operator's `tool_access` rows.
	seatDispatchableAgent(t, st, "alpha")
	// Registered is not granted: an agent's own view answers what the operator's
	// `tool_access` rows judge, so the world this guard needs includes the two
	// rules an operator would have authored — authored through the boundary
	// (`Add`, allow only from operator), never a hand-inserted row that skips it.
	ps := policy.NewStore(policyRepo(st))
	rules := []policy.Policy{
		allowPolicy("alpha", "alpha-own.do_work"),
		allowPolicy("alpha", "pub-tools.search"),
	}
	for i := range rules {
		rules[i].Source = policy.Operator // PL1: an allow comes only from the operator
	}
	for _, rule := range rules {
		if rj := ps.Add(context.Background(), rule, time.Now()); rj != nil {
			t.Fatalf("seed the operator's rule for %s: %s", rule.ID, rj.Reason)
		}
	}
	svc := daemon.NewToolsCatalogService(daemon.Options{
		Store:    st,
		Seed:     mustGuardSeedEngine(t, st),
		MCPAdmin: mineSeam(st, cache),
	})
	reader, ok := svc.(ownViewReader)
	if !ok {
		t.Fatal("the catalog adapter does not answer the own view — T7's doors moved somewhere the CLI and the arms cannot reach")
	}
	// The MCP half of the own view is keyed by the CALLER's stamp, which the
	// records arm supplies from the step (OL3) — so the guard stamps it the way the
	// arm does. Without a stamp the scoped `ToolsFor(self)` has no self to scope to.
	ctx := mineCtx("alpha")

	mine, merr := reader.ToolsMine(ctx, "alpha")
	if merr != nil {
		t.Fatalf("tools.mine.list for alpha: %v", merr)
	}
	rawMine, jerr := json.Marshal(mine)
	if jerr != nil {
		t.Fatal(jerr)
	}
	assertNoBetaBytes(t, "tools.mine.list", string(rawMine))
	// The walk is not simply returning nothing: alpha's own private tool and the
	// public declared tool are both in the answer.
	if !strings.Contains(string(rawMine), "alpha-own.do_work") {
		t.Errorf("alpha's own private tool is missing from its own view — MC2 hides a PEER's private, not the caller's own:\n%s", rawMine)
	}
	if !strings.Contains(string(rawMine), "pub-tools.search") {
		t.Errorf("the public declared tool is missing from alpha's own view:\n%s", rawMine)
	}

	// A peer's private tool: refused with the SAME sentence an unknown name gets,
	// and with none of the peer's bytes in it.
	_, serr := reader.ToolShow(ctx, "alpha", "beta-own.secret-b")
	if serr == nil {
		t.Fatal("alpha was shown beta's private tool — MC2's whole refusal made nothing of")
	}
	assertNoBetaBytes(t, "tool_show of a peer's private tool", serr.Error())

	// And the S46 own-view doors in the same world, so the two families are
	// checked against the same planted rows.
	for _, method := range []string{"mcp.mine.servers", "mcp.mine.grants", "mcp.mine.status"} {
		res, refusal, rerr := mineSeam(st, cache)(mineCtx("alpha"), method, json.RawMessage(`{}`))
		if rerr != nil {
			t.Fatalf("%s: %v", method, rerr)
		}
		if refusal != "" {
			t.Fatalf("%s refused alpha's own view: %s", method, refusal)
		}
		raw, mje := json.Marshal(res)
		if mje != nil {
			t.Fatal(mje)
		}
		assertNoBetaBytes(t, method, string(raw))
	}
}

// seatDispatchableAgent stamps the two columns that decide whether an own view can
// hold anything at all: `origin` and `tools`.
//
// The measured finding this encodes (T9's, recorded in Notes): `CreateAgent` does
// not write either column and `UpdateAgent`'s UPDATE does not name them — the only
// writer in this build is S43-extra's agent seed set. So every agent that did not
// arrive through the seed is `origin:'user'` with `tools:"[]"`, and SD7
// ("a name on a custom agent's row is not a capability the app can dispatch")
// makes its own view EMPTY. A guard that asserted "the own view shows the held
// set" without seating a dispatchable agent would be asserting that nothing is
// shown — which is exactly the kind of passing guard this file exists to refuse.
// The write below is the seed applier's own statement, through the seed family's
// tx helper, not a shape the test invented.
func seatDispatchableAgent(t *testing.T, st *store.Store, slug string) {
	t.Helper()
	if _, found, err := st.AgentBySlug(slug); err != nil || !found {
		t.Fatalf("the planted world carries no agent %q: found=%v err=%v", slug, found, err)
	}
	err := st.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(),
			`UPDATE agent SET origin='system', tools='["read","list","write"]' WHERE slug=?`, slug)
		return err
	})
	if err != nil {
		t.Fatalf("seat %s as a dispatchable agent: %v", slug, err)
	}
}

func mustGuardSeedEngine(t *testing.T, st *store.Store) *seed.Engine {
	t.Helper()
	engine, err := daemon.NewSeedEngine(st, seed.Options{}, mcpSeedValidator(st))
	if err != nil {
		t.Fatalf("seed engine: %v", err)
	}
	return engine
}

// --- guard 7: every registered name is attributed -----------------------------

// registryContribution is one row of the attribution table: the slice that
// brought a set of tool names, the names, and why they are there. T0's table of
// contributions (T0.md, the registry row) is the source; `TestRegistryExactNames`
// stays untouched as the count guard, and this is the sibling that makes the
// count MEAN something — a name arriving that nobody counted is attributed here,
// not just detected there.
type registryContribution struct {
	Slice string
	Names []string
	Why   string
	// Conditional marks a row whose names are registered only while a service seam
	// is wired (V13): the no-retrieval build does not register `search`, and that
	// absence is the correct answer, not a missing tool. The stale-name check
	// skips these; the seam-correspondence assertion in the guard covers them.
	Conditional bool
}

func registryAttributionOffenses(registered []string, table []registryContribution) []string {
	claimed := map[string]string{}
	conditional := map[string]bool{}
	var bad []string
	for _, row := range table {
		if strings.TrimSpace(row.Slice) == "" {
			bad = append(bad, "a contribution row carries no slice: "+strings.Join(row.Names, ", "))
		}
		if strings.TrimSpace(row.Why) == "" {
			bad = append(bad, row.Slice+" carries no reason — a table row without its why is a list, not an attribution")
		}
		if len(row.Names) == 0 {
			bad = append(bad, row.Slice+" claims no name — a row that attributes nothing is decoration")
		}
		for _, n := range row.Names {
			conditional[n] = row.Conditional
			if prior, dup := claimed[n]; dup {
				bad = append(bad, n+" is attributed to two slices ("+prior+" and "+row.Slice+") — one name has one author")
			}
			claimed[n] = row.Slice
		}
	}
	for _, n := range registered {
		if _, ok := claimed[n]; !ok {
			bad = append(bad, n+" is registered and attributed to NO slice — add its row with the slice, the task and the reason")
		}
	}
	for n := range claimed {
		if conditional[n] {
			continue // its absence is V13's correct answer, checked against the seam instead
		}
		found := false
		for _, r := range registered {
			if r == n {
				found = true
				break
			}
		}
		if !found {
			bad = append(bad, "the table attributes "+n+" to "+claimed[n]+" but the registry no longer registers it — a stale table stops catching the next arrival")
		}
	}
	return bad
}

// guardRegistryAttribution is the measured registry (86 honest names on the
// merged tree — main's 70 plus S62 T6's backup_status plus S62 T8's
// export_sets plus S57 T9's nine gate names plus S48 T10's five blueprint
// names, measured 2026-11-03; the
// hidden `write_file` alias is a compatibility key and not a listing entry,
// so it is not here and `TestRegistryExactNames` owns that claim).
var guardRegistryAttribution = []registryContribution{
	{Slice: "S11 (the registered ten)", Names: []string{"bash", "edit", "glob", "grep", "list", "question", "read", "todo", "webfetch", "write"},
		Why: "the hand-written toolset the registry was built from; registered is not granted, and DefaultGrant names only read, list, write"},
	{Slice: "S24 T5", Names: []string{"report_size"},
		Why: "the sizing run's one native tool; touches no store, so its capability set is a declared empty one"},
	{Slice: "S70 MB1", Names: []string{"ask_agent"},
		Why: "the agent↔agent ask, registered and NEVER granted until an operator adds a tool_access row"},
	{Slice: "S38 T9 (OL2's management catalog)", Names: []string{"brief_create", "brief_show", "epic_create", "epic_list", "epic_show", "epic_update", "goal_create", "goal_list", "goal_show", "goal_submit_review", "goal_update", "loop_get", "loop_list", "attach", "attachments_list"},
		Why: "one vocabulary for the loop's records, projected from internal/records — never hand-written twice"},
	{Slice: "S38 T10 (OL14's task surface)", Names: []string{"task_bump_counter", "task_create", "task_list", "task_set_status", "task_show", "task_update"},
		Why: "the ladder reached THROUGH the machine, scoped to the step's own task"},
	{Slice: "S39 T10 (CR5)", Names: []string{"run_start"},
		Why: "the one loop action an agent may take, behind an ask row the operator adds"},
	{Slice: "S44-extra CT9", Names: []string{"component_archive", "component_attach", "component_create", "component_detach", "component_list", "component_promote", "component_restore", "component_show", "component_update"},
		Why: "the nine component tree operations; the four project tools went and these joined, Count 41→42 DELIBERATE"},
	{Slice: "S43-extra T6", Names: []string{"seed_status"},
		Why: "the one seed tool, a read projected from the records catalog; registered, never default-granted"},
	{Slice: "S46 T8", Names: []string{"mcp_grants", "mcp_propose_remove_server", "mcp_propose_revoke_grant", "mcp_servers", "mcp_status"},
		Why: "the five MCP own-view doors — three reads and the two narrowing proposes (MG9 can only narrow)"},
	{Slice: "S45 T7 (PM9)", Names: []string{"policy_in_force", "policy_list", "policy_propose_add", "policy_propose_remove", "policy_show", "policy_why", "proposal_list"},
		Why: "five policy/proposal reads and the two propose-only twins that create INERT rows a human decides"},
	{Slice: "S56 T8 (AL12)", Names: []string{"autonomy_effective", "autonomy_list", "autonomy_propose_deny", "autonomy_propose_revoke", "autonomy_show"},
		Why: "three autonomy reads and the two RESTRICTIVE propose twins; there is no grant tool, by AL11"},
	{Slice: "S55 T10", Names: []string{"budget_effective", "budget_list", "budget_show", "budget_spend", "loop_control_state", "loop_request_stop", "watchdog_list"},
		Why: "six safe loop-control/budget/watchdog reads plus loop_request_stop, which records a request and stays inert until a person acts (LC5)"},
	{Slice: "S22 T6 (conditional)", Names: []string{"search"}, Conditional: true,
		Why: "the retrieval answer tool — registered only while the search resolver seam is wired (V13); the no-retrieval build's absence is the correct answer, not a missing tool"},
	{Slice: "S30 T5 (conditional)", Names: []string{"graph"}, Conditional: true,
		Why: "the code-graph tool — registered only while the graph service seam is wired; same rule as `search`"},
	{Slice: "S47 T7 (TC9)", Names: []string{"tools_mine", "tool_show"},
		Why: "the agent's own view of its tools, answering SELF from the stamped step; zero write tools in the family (TC1)"},
	{Slice: "S62 T6 (BK9)", Names: []string{"backup_status"},
		Why: "the backup family's one read twin over backup.status — a re-run re-reads, nothing to reconcile; run/verify/prune stay human doors and restore is never a tool (BK9)"},
	{Slice: "S62 T8 (BK14)", Names: []string{"export_sets"},
		Why: "the export family's one read twin over export.sets — a re-run re-lists the declared sets, nothing to reconcile; export.run stays a human door and there is no importer (BK14)"},
	// 2026-11-03 (S48 T10 merge reconciliation): the nine gate names landed
	// on main with S57 T9 but never got their rows here — the guard that
	// demands an author for every registered name was red on main. The rows
	// are S57 T9's own claims (VG19/VG17), carried by the merge that has to
	// pass the guard, not invented here.
	{Slice: "S57 T9 (VG19)", Names: []string{"verify_list", "verify_show", "verify_runs", "mergerule_show", "mergerule_preview", "dod_list", "verify_propose_add", "mergerule_propose_tighten", "dod_mark"},
		Why: "the gate plane's nine agent doors — six reads scoped to the caller's own loop and component (scope stamped from the step, never a param) and three permitted mutating specs: the two tightening-only INERT proposals a person commits (TD2/TD3) and the evidence-bound dod_mark on the loop's own ledger; the nine human gate writes stay out of the catalog (VG17) and none of the nine is granted by default"},
	{Slice: "S48 T10 (BP9)", Names: []string{"blueprint_list", "blueprint_show", "blueprint_validate", "blueprint_lint", "blueprint_propose"},
		Why: "four blueprint reads twinned to the person's standing doors and the ONE propose twin that files the INERT blueprint_save row a person commits; there is deliberately no save/delete/duplicate/revert tool (BP9, T8's guard)"},
	{Slice: "S58 T6 (SV16)", Names: []string{"inbox_list", "inbox_summary", "mission_status", "notify_channel_list", "notify_class_list"},
		Why: "the five supervision reads, own-view from the stamped identity (another root's tree is refused verbatim); the channel view is a different struct, incapable of carrying a secret, a path or subscription detail (SV7); NO write twins — acking is a person's act (SV12) and a channel is authority (SV7); registered, granted to nobody"},
	// 2026-11-03 (S59 T7): the execution-environment plane's five. Four reads
	// answered by the SAME services the person's twenty doors answer through,
	// and the one ask an agent may make of its own environment.
	{Slice: "S59 T7 (EV16)", Names: []string{"environment_list", "environment_show", "environment_effective", "secretref_list", "environment_propose_restrict"},
		Why: "the four reads carry names, where each name came from, and reference NAMES — no value and no reference source crosses this door (EV7, and the agent's reference view is a type with no field for it); the fifth is the ONE ask: the INERT environment_restrict proposal that can only TIGHTEN what the loop runs with, refused at create for every loosening and committed by a person alone. There is deliberately no create/update/archive/restore/bind/unbind/teardown or secretref-write tool and no backend field on the ask (EV16 — T6's and T7's guards refuse each by name), and none of the five is granted by default"},
	// 2026-11-04 (S49 T7): the automation plane's five. Four reads answered by
	// the SAME services the person's ten standing doors answer through (rule 4
	// — the tool answer IS the socket answer, statements and evaluatedAt
	// included), and the one ask an agent may make about what starts agents.
	// 2026-11-04 (S50 T5, RM8): the roadmap plane's three. Two reads answered
	// by the SAME shared answers the person's board/show/history doors run
	// (rule 4 — roadmap_list twins roadmap.board; roadmap_show carries the
	// trail the person splits across two doors), and the one ask an agent may
	// make of the roadmap.
	{Slice: "S50 T5 (RM8)", Names: []string{"roadmap_list", "roadmap_show", "roadmap_propose_idea"},
		Why: "the two reads carry the board and the item-with-trail through the ONE shared rendering the person's doors run; the third is the ONE ask: the INERT roadmap_idea proposal that can only ADD an idea to a board, refused at create when the component root names no component or the identical open item already stands, committed by a person alone as authored with the COMMITTER as actor. There is deliberately no roadmap_update/-cancel/-estimate/-priority/-ideate/-plan/-materialize tool and no such proposal kind (RM8; T4's guards refuse each such name), and none of the three is granted by default"},
	{Slice: "S49 T7 (AN9)", Names: []string{"automation_list", "automation_show", "automation_fires", "automation_health", "automation_propose_disable"},
		Why: "the four reads carry no secret value anywhere — health answers the secret env's NAME and a set-bool, never the value (§4c) — and the fifth is the ONE ask: the INERT automation_disable proposal that can only take an automation away (AN9 narrowing-only), refused at create when the automation is unknown, archived or already off, committed by a person alone. There is deliberately NO create/update/enable/disable/archive/unarchive/pause/resume/fire-now tool and no automation_propose_enable or -update or -archive name (AU1 lock 1 stands UNEDITED, T5's guard refuses each such name by word); none of the five is granted by default"},
	// 2026-11-04 (S51 T5, TM6): the bug family's three. Row added at T6's
	// sweep — T5's landing carried the tools and the class bump but not
	// this table, which is exactly the silence the guard exists to keep.
	{Slice: "S51 T5 (TM6)", Names: []string{"bug_list", "bug_show", "bug_file"},
		Why: "the two reads carry the person's own bug list and bug show through the SAME renderings the person's doors run (rule 4), and the third is the ONE filing an agent may make: bug_file files an UNVALIDATED, priority-unknown claim through the same CreateAudited write triage.file runs, filed_by the caller's own stamped slug and capped per agent at triage.MaxOpenClaimsPerAgent (eight) — the agent never names itself (TM6), and validation, priority, duplicate, handoff and state are not nameable here at all (TM2; T4's guards refuse each such name). There is deliberately no bug_update/-validate/-handoff/-confirm/-triage tool, and none of the three is granted by default — DefaultGrant() untouched"},
	{Slice: "S52 T4 (LM5, LM7)", Names: []string{"learning_add", "learning_propose_retract", "learning_query", "learning_show"},
		Why: "the two reads answer the agent's OWN readable set — the person's word, the projects the agent actually ran in, its own agent scope — merged through the ONE MineQuery the learnings.forAgent door runs and rendered by the ONE protocol renderer, so prompt-view and tool-view are one function's bytes, not two that agree by luck; a row outside the set answers the same sentence a nonexistent id does (LM5's no-oracle law, in success or error). learning_add is the ONE memory write an agent may make: observed or assumed only (LM4 — told and relayed are refused by schema AND arm), scope STAMPED from the step (a key naming another agent or root is refused, not honoured), and capped per agent at learnings.MaxSelfLogAddsPerAgent counted off the append-only learning_change ledger before the write. learning_propose_retract files the INERT learning_retract ask through the shared inert-proposal path; only a person's commit retracts, and the COMMITTER is the actor on stamp, audit line and vector drop (LM8). There is deliberately no learning_retract/-restore/-edit/-relay tool (T3's guards refuse each such name by word), and none of the four is granted by default — DefaultGrant() untouched"},
	// 2026-11-04 (S53 T3, HV4): the history family's two — reads only.
	{Slice: "S53 T3 (HV4)", Names: []string{"history_list", "history_show"},
		Why: "the two reads carry the merged live-and-archived history and the drill-down through protocol.AnswerHistory*, the ONE rendering the person's history.list/history.show doors run over the same read-only facade the socket seam holds (rule 4), costs rendered with three-state honesty (a floor reads as a floor, P32). There is deliberately NO history_export twin — the NDJSON dump stays the person's door (HV5; S53's guard refuses such a tool by name) — and no coverage or vocabulary twin either (those two S53 doors are the app's census, HV4). The schemas carry filter words only (projectRoot/groupBy/since, an id), no identity anywhere, and neither tool is granted by default — DefaultGrant() untouched"},
	{Slice: "S54 T4 (SP4)", Names: []string{"index_status"},
		Why: "the platform family's ONE read twin over platform.status, arriving through recordTools' projection of records.Catalog(): freshness of the STEP'S OWN root — per corpus the file and pending-embed counts and whether a reindex is flying, beside the same root's graph figures — composed over the very PlatformDeps the person's panel composes over (platform_reads.go keeps NewPlatformStatus and NewPlatformIndexFreshness reading through the same seams, the same flying question and the same composer, so the two doors cannot answer one question twice), minus the operator-view fields T0's visibility ruling keeps out of model-facing answers (root, embedRoute, embedModel, embedDim, other roots) — an unwired subsystem reads `unwired`, never an omitted key. The schema is EMPTY: scope is the step's stamp, not a param (OL3), so no call can ask about another component's index. There is deliberately NO control twin — no reindex, rebuild, graph build/reparse/delete tool and no proposal kind twinned to any operation (SP3: an operation spends a person's money and may discard derived data; S54 T3's guards refuse each such name, dotted and underscored, from this registry, the catalog, the narrow projection and the agent services) — and the tool is registered and never default-granted: DefaultGrant() untouched, the tool_access rows judge every call"},
	// 2026-11-05 (S63 T6, RT11): the scorecard family's three — reads only,
	// and the family carries NO write to attribute.
	{Slice: "S63 T6 (RT11)", Names: []string{"scorecard_mine", "scorecard_component", "digest_learned"},
		Why: "the three reads answer through the SAME services the person's scorecard.agent/scorecard.component/digest.learned doors answer through (rule 4 — the parity test pins the answers byte-identically over the same store), scoped by the STAMP, never the call: scorecard_mine is the step's own agent's card (no subject can be smuggled — the schema is a window and nothing else, and the component/digest twins read the step's own root), scorecard_component carries the card WITHOUT the person's per-child breakdown (no other agent's rows by the back door, T6.2), and digest_learned lists only project-scope learnings — agent-scope rows are filtered structurally by the store read. A score is a measurement, not an instruction (RT3), so the family has deliberately NO write, NO propose twin, and NO ranking: scorecard_compare, scorecard.blueprint, scorecard.vocabulary and every retro.* door stay the person's socket (RT10). None of the three is granted by default — DefaultGrant() untouched"},
	// 2026-11-05 (S27 T3 merge reconciliation, the S57/S48 mechanism): the
	// merge-queue family's five landed from S60 T9 without their row here —
	// TestRegistryCountIsExactlyAccountedFor was red on main for the five
	// names, exactly as the gate family's nine were before their row was
	// carried by the merge that had to pass the guard. The row is S60 T9's
	// own claim (MQ13/MQ17), carried here, with its date, by the census that
	// must pass it.
	{Slice: "S60 T9 (MQ17)", Names: []string{"mergequeue_list", "mergequeue_show", "mergequeue_status", "mergequeue_dequeue", "mergequeue_propose_enqueue"},
		Why: "three safe own-view reads (the person's own door answers through recordTools over the SAME queue service, rule 4, filtered to the repositories the agent's steps ran in), the ONE detectable narrowing write mergequeue_dequeue (this loop's own queued/conflicted item only — a removal can only narrow what the queue may do), and the ONE detectable ask mergequeue_propose_enqueue, which files an INERT mergequeue_enqueue row a person commits unattended as {proposal,<id>}; there is no enqueue/reorder/retry/land/settings name an agent can reach (MQ13/MQ17, T8's guards); none is granted by default"},
	// 2026-11-05 (S27 T3, CN7/P6): the roadmap plane's first two WRITE
	// tools — the graph's edges. RM8's amendment is precise, not a widening:
	// an edge is INTENT, never a rung.
	{Slice: "S27 T3 (CN7, P6)", Names: []string{"roadmap_depend", "roadmap_undepend"},
		Why: "the graph's two edges, twinned to the person's standing doors roadmap.depend/roadmap.undepend and answered by the SAME services (rule 4). An edge says one item WAITS FOR another and nothing else: it moves no status, no priority, no estimate, and nothing dispatches because an edge appeared or vanished — readiness is re-read at every attempt (D4), so filing an edge starts no loop and removing one stops none. That is why these two and not the LADDER: add · update · cancel · estimate · priority and the three dispatches stay RM8's sealed eight (S27 T3's P6 note reads them whole — the tools are direct twins and widen no plane). Both class detectable (a re-run touches durable roadmap_dependency rows; the duplicate and no-such-edge refusals make a double call visible, never silent). Registered and GRANTED TO NO ONE — DefaultGrant() untouched; tool_access rows judge every call"},
	// 2026-11-05 (S27 T8, CN23): the scheduler family's four reads — the
	// agent's eyes on the continuous scheduler. All four are pure reads
	// answered through the SAME ContinuousService the person's
	// scheduler.status/.preview/.decisions/.attention doors answer through
	// (rule 4), scoped to the step's own component root — the root is
	// stamped from the store, never typed (OL3), and a foreign repository
	// is absent from the answer, not named (MC2). The eight CONTROLS are
	// the family's absence: enable, disable, pause, resume, root.set,
	// root.disable, enableTree, stopAll are human doors on the 0600
	// surface (S27 CN2; cmd/aeon/continuous_guard_test.go) — this family
	// carries NO write to attribute, and TestNoSchedulerWriteToolExists +
	// TestSchedulerToolsNotInDefaultGrant pins that no control verb enters registry or
	// catalog. None of the four is in DefaultGrant() — registered is not
	// granted (S11). —
	{Slice: "S27 T8 (CN2, CN23)", Names: []string{"scheduler_status", "scheduler_preview", "scheduler_decisions", "scheduler_attention"},
		Why: "the four reads answer through the SAME ContinuousService the person's scheduler.status/.preview/.decisions/.attention doors answer through (rule 4), scoped to the step's stamped component root (OL3, MC2); the family's eight controls stay human doors with no tool, no spec, no arm (S27 CN2)"},
	// The goal-intake plane's seven (S61 T8, GI17): five safe reads and two
	// inert asks; the plane's four writes (charter.save · template.save ·
	// intake.start · intake.commit) carry NO tool, NO spec and NO arm —
	// cmd/aeon/intake_amend_test.go re-runs the T7 negatives with these
	// seven present, and TestNoDecisionOrWriteToolExists pins the absence.
	// None of the seven is in DefaultGrant() — registered is not granted.
	{Slice: "S61 T8 (GI17)", Names: []string{"charter_list", "charter_propose", "charter_show", "intake_propose", "intake_status", "template_list", "template_show"},
		Why: "the five reads answer through the SAME socket doors the person rings (one implementation, two doors); the two proposes file INERT charter_save/intake_plan rows through the SAME desk.Validate proposal.create runs — inert until a person commits (GI17)"},
	// The deliverables door's four (2026-11-08): a loop's plan.md, legal.md,
	// brief.md and tasks/ are rows in loop_document, filed by these tools —
	// not files in the workspace. Every call is scoped to the step's own loop
	// by the daemon (the step on the context), and the schema names a
	// document and nothing that addresses a loop, a project or a goal
	// (additionalProperties false, strict decode). A document the step's loop
	// did not write is readable only when the goal the loop works owns it.
	// None of the four is in DefaultGrant() — registered is not granted.
	{Slice: "documents (2026-11-08)", Names: []string{"doc_edit", "doc_list", "doc_read", "doc_write"},
		Why: "the four tools file, read, edit and list the step's own loop's documents through tools.DocStore, implemented by daemon.DocService over store.WriteDocument/ReadDocument (the one writer of a loop_document revision); identity is the step's, never an argument, so no call can address another loop; a goal's document is read through ReadGoalDocument, never edited in place; the engine's produces check reads the same rows and files a workspace file into them as the net under a weaker model"},
	// The variable manager's agent door (2026-11-08): env_list names the variables
	// a person made agent-visible, env_get reads one by name. The rule is the
	// vault's: an agent sees a variable only if a person marked it agent-visible,
	// nothing is visible by default, a hidden variable answers the sentence an
	// absent one does, and every read is a ledger row naming the agent. The model
	// supplies a name and nothing else (strict decode); the project comes from the
	// step. Neither tool is in DefaultGrant() — registered is not granted.
	{Slice: "variables (2026-11-08)", Names: []string{"env_get", "env_list"},
		Why: "the two tools answer through daemon.EnvService over vault.Vault: AgentList/AgentGet judge the variable that APPLIES to the step's project (an override that hides a variable hides it), refuse hidden and absent names with one sentence, and write the append-only env_var_change row that names the agent; values never appear in a listing, in an event summary or on a ledger row; the person's doors (vault.*, aeon vault) can set the flags and cannot read a secret back"},
}

func TestRegistryCountIsExactlyAccountedFor(t *testing.T) {
	registered := tools.ToolNames()
	if len(registered) < 60 {
		t.Fatalf("the registry holds %d names — too few for the attribution to mean anything", len(registered))
	}
	if bad := registryAttributionOffenses(registered, guardRegistryAttribution); len(bad) > 0 {
		t.Errorf("the registry is not exactly accounted for:\n%s", strings.Join(bad, "\n"))
	}
	// The hidden alias stays hidden and unaccounted-for on purpose — asserted
	// here so a change to that rule is noticed in the file that claims it.
	if _, ok := tools.Registry()["write_file"]; !ok {
		t.Error("the hidden write_file alias is gone from Registry(); the attribution table's exclusion is now wrong")
	}
	for _, n := range registered {
		if n == "write_file" {
			t.Error("ToolNames() lists the hidden alias")
		}
	}
	// The conditional rows are not waved through: a conditional name is registered
	// exactly when its seam is wired, which is the half of the claim the attribution
	// table cannot make on its own.
	held := map[string]bool{}
	for _, n := range registered {
		held[n] = true
	}
	if got, want := tools.SearchRegistered(), held["search"]; got != want {
		t.Errorf("the search seam says registered=%v and the registry says %v — a conditional tool that ignores its seam is available when its service is not", got, want)
	}
	if got, want := tools.GraphRegistered(), held["graph"]; got != want {
		t.Errorf("the graph seam says registered=%v and the registry says %v", got, want)
	}
}

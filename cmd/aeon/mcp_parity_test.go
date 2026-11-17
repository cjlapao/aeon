package main

// mcp_parity_test.go — S46 T9. The parity guard, both directions, with
// T0's parity table (docs/aeon/S46-mcp-management/tasks/T0.md §4) as the
// fixture: no verb without a socket twin, no human socket method without
// a verb. The walk is of the REAL dispatch (go/parser over mcp.go's
// cmdMCP cases) so a verb added without the table — or a table entry
// without a verb — fails HERE, at the switch, not in a comment.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// t9VerbMethods is the parity table: every management verb of cmdMCP and
// the socket method(s) it speaks. This map IS the fixture; a drift between
// it and the dispatch switch fails TestEveryMcpVerbHasASocketMethod.
var t9VerbMethods = map[string][]protocol.MethodName{
	"show":          {protocol.MethodMCPServer},
	"grants":        {protocol.MethodMCPGrants},
	"add":           {protocol.MethodMCPServerAdd},
	"update":        {protocol.MethodMCPServerUpdate},
	"remove":        {protocol.MethodMCPServerRemove, protocol.MethodMCPServer}, // the pre-read for the seatbelt's plan
	"enable":        {protocol.MethodMCPServerSetEnabled},
	"disable":       {protocol.MethodMCPServerSetEnabled},
	"share":         {protocol.MethodMCPServerShare},
	"declare":       {protocol.MethodMCPGrantDeclare},
	"revoke":        {protocol.MethodMCPGrantRevoke},
	"core":          {protocol.MethodMCPCoreSet},
	"import-config": {protocol.MethodMCPImportFile},
	"changes":       {protocol.MethodMCPChanges},
	"status":        {protocol.MethodMCPStatus},
	"check":         {protocol.MethodMCPCheck},
	"inspect":       {protocol.MethodMCPServerInspect},
	"processes":     {protocol.MethodMCPProcesses},
	"logs":          {protocol.MethodMCPProcessLogs},
	"start":         {protocol.MethodMCPProcessStart},
	"stop":          {protocol.MethodMCPProcessStop},
	"restart":       {protocol.MethodMCPProcessRestart},
}

// t9CliOnlyByNature closes the loop for verbs whose nature is the CLI's
// own, each with the decision's provenance (T0's parity table named these;
// help is the usage screen, not a capability).
var t9CliOnlyByNature = map[string]string{
	"list":  "cli-only-by-nature — T0 §4: the byte-golden presentation (T3 Phase A pins its bytes; its rows already come from the SAME functions the admin reads answer from). Recorded deviation: the socket twin exists (mcp.servers/mcp.grants/mcp.status) and the settings UI (T11) is that client; the CLI list keeps the CLI-env live-dial --agent view call's spawn rights share",
	"call":  "cli-only-by-nature (T0 §4): a one-shot operator dial from the CLI's own env; a socket twin would hand any socket caller a dial-and-spawn door outside the step gate",
	"serve": "cli-only-by-nature (T0 §4): aeon AS an MCP server (S19 narrow surface), not management",
	"help":  "the usage screen — a capability that prints its own name needs no method",
}

// t9MethodsWithoutVerb is the backward direction's named exemption list —
// a human method whose consumer is not a CLI verb. Every entry names its
// closer; an unnamed gap fails the test.
var t9MethodsWithoutVerb = map[protocol.MethodName]string{
	protocol.MethodMCPVocabulary: "the classes dictionary for the T11 settings' pickers (MG5: the dropdown is the validator's mirror) — a UI consumer, not a human verb; T10/T11 is its closer",
	protocol.MethodMCPServers:    "the consumer is the exempt list verb (T0 §4 — its byte-golden presentation; the rows come from this same read's functions) and the T11 settings' server list; the unscoped full-table view is a UI fact, not a second CLI table",
}

// t9DispatchVerbs walks mcp.go's cmdMCP switch — the REAL dispatch.
func t9DispatchVerbs(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "mcp.go", nil, 0)
	if err != nil {
		t.Fatalf("parse mcp.go: %v", err)
	}
	var verbs []string
	ast.Inspect(f, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "cmdMCP" {
			return true
		}
		// cmdMCP's ONE switch (on args[0]) is the dispatch; its string
		// cases are the verb set. A second switch inside cmdMCP would be
		// a hidden dispatch — the walk takes the first and the count
		// assertion above makes an empty walk fatal.
		ast.Inspect(fd, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			for _, st := range sw.Body.List {
				cs, ok := st.(*ast.CaseClause)
				if !ok || len(cs.List) != 1 {
					continue
				}
				if lit, ok := cs.List[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					verbs = append(verbs, strings.Trim(lit.Value, `"`))
				}
			}
			return false
		})
		return false
	})
	if len(verbs) == 0 {
		t.Fatal("walked cmdMCP and found no verb cases — the walk is broken, not the dispatch")
	}
	return verbs
}

func TestEveryMcpVerbHasASocketMethod(t *testing.T) {
	tbl, cleanup := wiredMcpTable(t)
	defer cleanup()
	registered := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	for _, verb := range t9DispatchVerbs(t) {
		if why, exempt := t9CliOnlyByNature[verb]; exempt {
			if !strings.Contains(why, "nature") && !strings.Contains(why, "usage") {
				t.Errorf("the %q exemption does not name its nature", verb)
			}
			continue
		}
		methods, ok := t9VerbMethods[verb]
		if !ok {
			t.Errorf("verb %q is in the dispatch but not in the parity table — a verb without a socket twin is P34's forbidden shape (a capability that exists only as a CLI verb). Add it to t9VerbMethods or name it in t9CliOnlyByNature WITH its reason", verb)
			continue
		}
		for _, m := range methods {
			if !registered[m] {
				t.Errorf("verb %q speaks %q, which the wired table does not register — the door was wired out of existence (V13 drift)", verb, m)
			}
		}
	}
}

func TestEveryMcpSocketMethodHasAVerb(t *testing.T) {
	tbl, cleanup := wiredMcpTable(t)
	defer cleanup()
	spoken := map[protocol.MethodName]string{}
	for verb, methods := range t9VerbMethods {
		for _, m := range methods {
			spoken[m] = verb
		}
	}
	seen := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		if !strings.HasPrefix(string(m), "mcp.") {
			continue
		}
		seen[m] = true
		if _, ok := spoken[m]; ok {
			continue
		}
		if why, exempt := t9MethodsWithoutVerb[m]; exempt {
			if len(why) < 20 {
				t.Errorf("%q is exempted without a reason that names its closer", m)
			}
			continue
		}
		t.Errorf("human method %q stands on the table but no verb speaks it — the reverse drift (a method nobody calls is surface without a consumer; name it in t9MethodsWithoutVerb with its closer, or give it a verb)", m)
	}
	// The fifteen must ALL be on the table or this test walks nothing.
	for _, m := range []protocol.MethodName{
		protocol.MethodMCPServers, protocol.MethodMCPServer, protocol.MethodMCPGrants,
		protocol.MethodMCPChanges, protocol.MethodMCPVocabulary,
		protocol.MethodMCPServerAdd, protocol.MethodMCPServerUpdate, protocol.MethodMCPServerRemove,
		protocol.MethodMCPServerSetEnabled, protocol.MethodMCPGrantDeclare, protocol.MethodMCPGrantRevoke,
		protocol.MethodMCPCoreSet, protocol.MethodMCPImportFile,
		protocol.MethodMCPStatus, protocol.MethodMCPCheck,
	} {
		if !seen[m] {
			t.Errorf("mcp method %q is not registered on the wired table — the walk above would silently pass without it", m)
		}
	}
}

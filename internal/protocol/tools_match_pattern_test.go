package protocol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// S74 T2: tools.matchPattern answers with the one matcher the gate uses.

func matchPatternStub() *toolsCatalogStub {
	e := func(name, family, access string) ToolEntry {
		t := toolEntryForTest(name, family, ToolsSourceSystem)
		t.Category, t.Access, t.Address = family, access, family+"."+name+"."+access
		return t
	}
	return &toolsCatalogStub{tools: []ToolEntry{
		e("glob", "workspace", "read"), e("read", "workspace", "read"),
		e("write", "workspace", "write"), e("webfetch", "egress", "write"),
	}}
}

func TestMatchPatternNamesTheReadTools(t *testing.T) {
	tbl, conn := wiredToolsCatalog(t, matchPatternStub())
	res := dispatchToolsResult[ToolsMatchPatternResult](t, tbl, conn, MethodToolsMatchPattern, json.RawMessage(`{"pattern":"workspace.*.read"}`))
	if res.Count != 2 || res.Matches[0] != "workspace.glob.read" || res.Matches[1] != "workspace.read.read" {
		t.Fatalf("workspace.*.read matched %v", res.Matches)
	}
	all := dispatchToolsResult[ToolsMatchPatternResult](t, tbl, conn, MethodToolsMatchPattern, json.RawMessage(`{"pattern":"*.*.*"}`))
	if all.Count != 4 {
		t.Fatalf("*.*.* matched %d, want all 4", all.Count)
	}
	none := dispatchToolsResult[ToolsMatchPatternResult](t, tbl, conn, MethodToolsMatchPattern, json.RawMessage(`{"pattern":"process.*.read"}`))
	if none.Count != 0 || none.Matches == nil {
		t.Fatalf("zero matches must be [] and an answer: %+v", none)
	}
}

func TestMatchPatternRefusesAPartialWildcardNamingTheSegment(t *testing.T) {
	tbl, conn := wiredToolsCatalog(t, matchPatternStub())
	_, err := tbl.Dispatch(context.Background(), conn, MethodToolsMatchPattern, json.RawMessage(`{"pattern":"wo*.*.read"}`))
	pe, ok := err.(*Error)
	if !ok || !strings.Contains(pe.Data.What, "segment 1") || !strings.Contains(pe.Data.What, "partial wildcard") {
		t.Fatalf("a partial star must be refused naming segment 1: %v", err)
	}
}

package protocol

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// S34 T3 — this package must never import internal/mcp: the whole point of
// Table.MCP is that mcp.status/mcp.check reach their domain logic through a
// plain func the entry point supplies, not an import (internal/mcp's own
// absent_test.go bans internal/protocol from its dependency graph the same
// way). An AST-free string scan, same technique absent_test.go uses on its
// own side of the boundary.
func TestProtocolNeverImportsInternalMCP(t *testing.T) {
	// Built by concatenation, not a literal: a literal copy of the banned
	// import string would make this very file a false positive under its
	// own scan — the quoted import line never appears anywhere here.
	bannedImport := `"github.com/cjlapao/aeon/internal/` + `mcp"`
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), bannedImport) {
			t.Errorf("%s imports internal/mcp — route it through Table.MCP instead (the seam exists exactly to avoid this)", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMCPCallUnwiredRefusesNamingTheTask(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodMCPStatus, nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrNotWired.Code {
		t.Fatalf("unwired MCP seam must answer ErrNotWired, got %v", err)
	}
	if !strings.Contains(e.Data.What, "not wired") || !strings.Contains(e.Data.What, "MCP") {
		t.Fatalf("the refusal names the missing seam: %+v", e.Data)
	}
}

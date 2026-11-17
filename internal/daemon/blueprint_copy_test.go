package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// TestBlueprintCopyCreatesANewNamedSourceWithoutOverwriting — S48 T6: the
// copy door is now a ROWS door (the file writer is retired). The behavior
// the S17 walk proved stands: a new named source is created without
// touching the original, createOnly never overwrites, and a path-like name
// never gets in (grammar at the service now, path traversal by the file's
// grace before).
func TestBlueprintCopyCreatesANewNamedSourceWithoutOverwriting(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	shipped, err := os.ReadFile(filepath.Join("..", "blueprint", "blueprints", "two-step.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Replace(string(shipped), "id: two-step", "id: my-copy", 1)
	ctx := context.Background()
	actor := protocol.Actor{Kind: "user", ID: "operator"}
	res, err := d.Table.SaveBlueprint(ctx, actor, protocol.BlueprintSaveParams{Name: "my-copy", Content: content, CreateOnly: true})
	if err != nil {
		t.Fatalf("create copy: %v", err)
	}
	if !res.Changed || res.Revision != 1 || res.Origin != "user" {
		t.Fatalf("copy save answered %+v — want a changed create at revision 1, origin user", res)
	}
	row, ok, err := s.BlueprintGet(ctx, "my-copy")
	if err != nil || !ok {
		t.Fatalf("the copy is not a row: %v", err)
	}
	if row.Content != content {
		t.Fatal("the stored copy differs from the source bytes — verbatim is the contract")
	}
	if row.UpdatedBy != "user:operator" || row.Origin != "user" {
		t.Fatalf("provenance: %+v — the peer stamps the row (BP12)", row)
	}
	listed, err := d.Table.ListBlueprints(protocol.BlueprintListParams{})
	if err != nil {
		t.Fatal(err)
	}
	names := listed.Names
	if !strings.Contains(strings.Join(names, ","), "my-copy") {
		t.Fatalf("copy is absent from blueprint.list: %v", names)
	}
	_, err = d.Table.SaveBlueprint(ctx, actor, protocol.BlueprintSaveParams{Name: "my-copy", Content: content, CreateOnly: true})
	if err == nil {
		t.Fatal("createOnly overwrote an existing blueprint")
	}
	if e, k := refusalKind(err); e == nil || k != "collision" || !strings.Contains(e.Data.What, "my-copy") {
		t.Fatalf("collision refusal %v (%s) — must name the existing name", err, k)
	}
	// Identical re-save is NOT an act: no new revision (BP7's audit truth).
	res2, err := d.Table.SaveBlueprint(ctx, actor, protocol.BlueprintSaveParams{Name: "my-copy", Content: content})
	if err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if res2.Changed || res2.Revision != 1 {
		t.Fatalf("identical content wrote a revision: %+v", res2)
	}
	revs, _ := s.RevisionList(ctx, "my-copy", 10)
	if len(revs) != 1 {
		t.Fatalf("revision history grew on a no-op: %d rows", len(revs))
	}
	if _, err := d.Table.SaveBlueprint(ctx, actor, protocol.BlueprintSaveParams{Name: "../escape", Content: content}); err == nil {
		t.Fatal("path-like blueprint name was accepted")
	}
}

func refusalKind(err error) (*protocol.Error, string) {
	if e, ok := err.(*protocol.Error); ok {
		return e, e.Data.Kind
	}
	return nil, ""
}

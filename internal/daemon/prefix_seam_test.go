package daemon

// prefix_seam_test.go — S60 T4 (behaviour 6): the prefix rule is refused
// WHERE IT IS SET — the component.create and component.update seams — with
// the construction rule's own sentence, before any row is written.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

func pfxDispatch(t *testing.T, w *walkEnv, method protocol.MethodName, params map[string]any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return w.d.Table.Dispatch(context.Background(), w.conn, method, raw)
}

func TestComponentPrefixSeamRefuses(t *testing.T) {
	w := newWalkEnv(t)
	ctx := context.Background()
	_ = ctx
	for _, bad := range []string{"Bad/", "aeon", "a b/", "/x/"} {
		_, err := pfxDispatch(t, w, protocol.MethodComponentUpdate, map[string]any{
			"id": w.root.ID, "name": w.root.Name, "slug": w.root.Slug, "branchPrefix": bad,
		})
		if err == nil {
			t.Fatalf("component.update accepted prefix %q", bad)
		}
		if !strings.Contains(err.Error(), "optional lowercase identity") {
			t.Fatalf("refusal for %q must name the rule, got: %v", bad, err)
		}
	}
	// The refusal wrote nothing: the row still carries what it had.
	eff, err := w.store.EffectiveComponent(w.root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if eff.BranchPrefix.Value != "" {
		t.Fatalf("a refused update moved the prefix to %q", eff.BranchPrefix.Value)
	}
	// A legal prefix lands through the same seam and reads back effective.
	if _, err := pfxDispatch(t, w, protocol.MethodComponentUpdate, map[string]any{
		"id": w.root.ID, "name": w.root.Name, "slug": w.root.Slug, "branchPrefix": "aeon/",
	}); err != nil {
		t.Fatalf("legal prefix refused: %v", err)
	}
	eff, err = w.store.EffectiveComponent(w.root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if eff.BranchPrefix.Value != "aeon/" {
		t.Fatalf("prefix stored as %q", eff.BranchPrefix.Value)
	}
	// The create seam carries the same rule (recorded extension of the
	// spec, which named only the update door): refusing at one and not the
	// other would leave a way to SET the unsafe value.
	dir := t.TempDir()
	_, err = pfxDispatch(t, w, protocol.MethodComponentCreate, map[string]any{
		"name": "bad-pfx", "slug": "bad-pfx", "root": dir, "branchPrefix": "Bad/",
	})
	if err == nil {
		t.Fatal("component.create accepted an unsafe prefix")
	}
	if !strings.Contains(err.Error(), "optional lowercase identity") {
		t.Fatalf("create refusal must name the rule, got: %v", err)
	}
}

package daemon

// # Records goldens — the records tools over the census (S44-extra T1)
//
// The records tier is the model's own door into the store: the same rows the
// store tier and the wire tier read, reached through the records catalog (the
// tools an agent calls). Each golden is GREEN on the UNTOUCHED code; a later
// task (T2/T3) that folds the implicit groups changes what the catalog returns
// and the golden fails — the proof the migration changed the records surface.
//
// A user actor is used: the person's door reads the catalog's reads (the only
// arms personDoorRefusal lets through) without a step, so the census' rows are
// read the way a person would ask for them.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/store/ctcensus"
)

func TestRecordsGoldensBeforeTree(t *testing.T) {
	c := ctcensus.Build(t)
	t.Cleanup(func() { _ = c.S.Close() })
	s, scrub := c.S, c.Scrub
	svc := NewRecordsService(s)
	ctx := context.Background()
	actor := Actor{Kind: "user", ID: "u1"}
	g := func(name string, input []byte) {
		t.Helper()
		res, f, err := svc.Do(ctx, actor, name, input)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if f != nil {
			t.Fatalf("%s: refused: %s", name, f.Sentence)
		}
		ctRecordsGolden(t, scrub, res, name+".json")
	}

	// The whole component tree through the records catalog.
	g("component_list", nil)
	g("goal_list", nil)
	g("task_list", nil)
	g("epic_list", nil)

	// per-component shows, ordered by the scrub label.
	comps, err := c.S.ListComponents(store.ComponentFilter{})
	if err != nil {
		t.Fatalf("list components: %v", err)
	}
	var cShows []any
	for _, cp := range comps {
		res, f, err := svc.Do(ctx, actor, "component_show", json.RawMessage(ctMustJSON(t, map[string]string{"component_id": cp.ID})))
		if err != nil || f != nil {
			t.Fatalf("component_show %s: %v %v", cp.ID, f, err)
		}
		cShows = append(cShows, res)
	}
	ctRecordsGolden(t, scrub, cShows, "component_show.json")
}

// ctRecordsGolden scrubs a records result and compares it with the pinned bytes
// (write-if-absent), re-sorting any id-bearing array so the catalog's
// non-deterministic order cannot drift the golden run to run.
func ctRecordsGolden(t *testing.T, scrub map[string]string, res any, name string) {
	t.Helper()
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatalf("%s: marshal result: %v", name, err)
	}
	b = ctcensus.ScrubJSON(b, scrub)
	b = ctSortByID(t, b)
	path := filepath.Join("testdata/ct_golden/records", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("%s: mkdir: %v", name, err)
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: read: %v", name, err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatalf("%s: write: %v", name, err)
		}
		return
	}
	if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(b)) {
		t.Errorf("%s: golden drifted from the untouched code\n--- got\n%s\n--- want\n%s",
			name, b, existing)
	}
}

func ctMustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("ctMustJSON: %v", err)
	}
	return b
}

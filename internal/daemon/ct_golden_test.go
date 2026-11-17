package daemon

// # Wire goldens — the daemon's RPC surface over the census (S44-extra T1)
//
// The wire tier proves the daemon's socket methods read the census the same way
// the store tier does: same rows, same ids, just reached over the RPC surface.
// Each golden is GREEN on the UNTOUCHED code; a later task (T2/T3) that folds
// the implicit groups makes the same dispatch return a different tree and the
// golden fails, which is the proof the migration changed the surface.
//
// The daemon is built with the census store (New(Options{Store: c.S})); the
// census owns the store's lifetime, the daemon only reads through it.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store/ctcensus"
)

// ctWireGolden dispatches one daemon method over the census, scrubs the raw ids
// from the result and compares with the pinned bytes (write-if-absent).
func ctWireGolden(t *testing.T, d *Daemon, scrub map[string]string, method protocol.MethodName, params any, name string) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("%s: marshal params: %v", name, err)
	}
	res, err := d.Table.Dispatch(context.Background(), "c1", method, raw)
	if err != nil {
		t.Fatalf("%s: dispatch %s: %v", name, method, err)
	}
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatalf("%s: marshal result: %v", name, err)
	}
	b = ctcensus.ScrubJSON(b, scrub)
	b = ctSortByID(t, b)
	path := filepath.Join("testdata/ct_golden/wire", name)
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

// ctSortByID re-sorts a scrubbed JSON document by the "id" field of any array
// whose elements are objects. The daemon's list methods return rows in a
// non-deterministic order (updated_at DESC, id; the fixed clock makes id the
// random tiebreak), so without this the same golden drifts run to run. After
// ScrubJSON the "id" field is the stable label, so sorting on it is stable.
func ctSortByID(t *testing.T, b []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("ctSortByID: unmarshal: %v", err)
	}
	ctSortWalk(v)
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("ctSortByID: marshal: %v", err)
	}
	return out
}

// ctSortWalk sorts every array whose elements are objects, by their "id"
// field, recursing first so nested arrays are stable before their container.
func ctSortWalk(v any) {
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			ctSortWalk(e)
		}
		if len(t) > 1 && isIDArray(t) {
			sort.Slice(t, func(i, j int) bool { return idOf(t[i]) < idOf(t[j]) })
		}
	case map[string]any:
		for _, e := range t {
			ctSortWalk(e)
		}
	}
}

func isIDArray(a []any) bool {
	for _, e := range a {
		if idOf(e) != "" {
			return true
		}
	}
	return false
}

// idOf finds the first "id" value in an element, descending into nested
// objects. The records catalog wraps a show in a {Project, Reason, Refusal}
// envelope, so the id sits one level down; a bare list has it at the top.
func idOf(v any) string {
	switch t := v.(type) {
	case map[string]any:
		if s, ok := t["id"].(string); ok {
			return s
		}
		for _, e := range t {
			if s := idOf(e); s != "" {
				return s
			}
		}
	}
	return ""
}

// ctWireCompare scrubs a pre-dispatched result and compares it with the pinned
// bytes (write-if-absent). Used where the result is built outside a single
// dispatch (e.g. project.show per group, collected into an ordered array).
func ctWireCompare(t *testing.T, scrub map[string]string, res any, name string) {
	t.Helper()
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatalf("%s: marshal result: %v", name, err)
	}
	b = ctcensus.ScrubJSON(b, scrub)
	b = ctSortByID(t, b)
	path := filepath.Join("testdata/ct_golden/wire", name)
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

func TestWireGoldensBeforeTree(t *testing.T) {
	c := ctcensus.Build(t)
	t.Cleanup(func() { _ = c.S.Close() })
	s, scrub := c.S, c.Scrub
	d := New(Options{Store: s})
	// The component/project methods have RequiresInitialize, so the "c1"
	// connection must be initialized before they will dispatch.
	initConn(t, d, context.Background(), "c1", "ct-golden")
	g := func(method protocol.MethodName, params any, name string) {
		ctWireGolden(t, d, scrub, method, params, name)
	}

	// component.list — every component, incl. the inherited settings.
	g(protocol.MethodComponentList, struct{}{}, "component_list.json")
}

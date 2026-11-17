package store_test

// # Store goldens — the component-tree characterisation (S44-extra T1)
//
// Every golden here is GREEN on the UNTOUCHED code. It serialises a surface of
// the census fixture through today's store API, scrubs the random ids (the
// scrub map the census captured), and compares with the pinned bytes in
// testdata/ct_golden/store/. Write-if-absent, compare-if-present: the first run
// pins the pre-migration state, and a LATER task (T2/T3, the migration) that
// changes the source makes the same golden FAIL, proving the migration did
// what it said. The goldens do not re-derive the CT5 mapping; the census'
// hand-written expected.json is that.
//
// # Why the census builder is importable, not in testdata/
//
// The task named the shared census `internal/store/testdata/ct_census`. A
// `testdata/` directory is excluded from the Go build, so the store/daemon/cmd
// goldens could not import code from it. The builder therefore lives in
// `internal/store/ctcensus` (importable); the DATA files — expected.json and
// the goldens — stay under `testdata/`. This is the one deviation from the
// task's file layout, recorded in T1's Notes.
//
// # Why this file is an EXTERNAL test (package store_test)
//
// `ctcensus` imports `store` to use its API. An INTERNAL store test that also
// imported `ctcensus` would be `store -> ctcensus -> store`, a cycle. An
// external `store_test` package imports both as a third party, breaking the
// cycle. The goldens read only exported store surfaces, so external is fine.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/store/ctcensus"
)

// ctStoreGolden pins one surface: scrub the raw ids, compare with the pinned
// bytes, write them if absent. A mismatch on a re-run is a PASS-worthy change
// only when a named task (T2/T3) is the one that made it.
func ctStoreGolden(t *testing.T, scrub map[string]string, name string, data any) {
	t.Helper()
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("%s: marshal: %v", name, err)
	}
	b = ctcensus.ScrubJSON(b, scrub)
	path := filepath.Join("testdata/ct_golden/store", name)
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

// TestStoreGoldensBeforeTree is the store tier of the characterisation. Each
// sub-golden names the LATER task expected to change it in its header (below),
// because the failure of a re-run after T2/T3 is the proof the migration ran.
func TestStoreGoldensBeforeTree(t *testing.T) {
	c := ctcensus.Build(t)
	t.Cleanup(func() { _ = c.S.Close() })
	s, scrub := c.S, c.Scrub
	g := func(name string, d any) { ctStoreGolden(t, scrub, name, d) }

	// components.json — the component rows, incl. the inherited settings
	// (queue_advance, sizing_mode, max_parallel_agents, branch_prefix,
	// default_branch) T2 must preserve through the migration. T3 re-reads
	// them through the tree.
	comps, err := s.ListComponents(store.ComponentFilter{})
	if err != nil {
		t.Fatalf("list components: %v", err)
	}
	sort.Slice(comps, func(i, j int) bool { return scrub[comps[i].ID] < scrub[comps[j].ID] })
	g("components.json", comps)

	// projects.json — every root (a "project" is a root, CT1). The collapse/
	// keep/drop answer lives in the census' expected.json; this golden pins
	// the roots the census derived the projects from.
	// A "project" is a root (CT1); the census's roots are the rows the old
	// projects.json iterated, read back through the tree.
	roots, err := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil {
		t.Fatalf("list roots: %v", err)
	}
	sort.Slice(roots, func(i, j int) bool { return scrub[roots[i].ID] < scrub[roots[j].ID] })
	g("projects.json", roots)

	// goals.json — goals on every kind that can hold a record (explicit,
	// implicit-keep, implicit-collapse), incl. the g4 that references an
	// empty implicit group. T2 keeps them where their group is kept.
	goals, err := s.ListGoals(store.GoalFilter{})
	if err != nil {
		t.Fatalf("list goals: %v", err)
	}
	sort.Slice(goals, func(i, j int) bool { return scrub[goals[i].ID] < scrub[goals[j].ID] })
	g("goals.json", goals)

	// epics.json — epics on the explicit groups.
	var epics []store.Epic
	for _, r := range roots {
		ls, err := s.ListEpics(r.ID)
		if err != nil {
			t.Fatalf("list epics %s: %v", r.ID, err)
		}
		epics = append(epics, ls...)
	}
	sort.Slice(epics, func(i, j int) bool { return scrub[epics[i].ID] < scrub[epics[j].ID] })
	g("epics.json", epics)

	// tasks.json — the loop task (carries a component id) and the manual task
	// (emits task.created), read through the board's TaskRow. T2 moves these
	// with their component's group. Sorted by the task's label: the board's
	// ORDER BY is updated_at DESC, id, and the fixed clock makes id the
	// (random) tiebreak, so without a re-sort the order would not be stable.
	rows, err := s.ListTaskRows(store.TaskFilter{})
	if err != nil {
		t.Fatalf("list task rows: %v", err)
	}
	sort.Slice(rows, func(i, j int) bool { return scrub[rows[i].Task.ID] < scrub[rows[j].Task.ID] })
	g("tasks.json", rows)

	// automation_events.json — task.created and goal.approved, each carrying
	// the project id that T2 folds. An empty outbox is its own answer (P31),
	// so the golden pins the non-empty one.
	autos, err := s.ReadAutomationEvents(0, 1000)
	if err != nil {
		t.Fatalf("read automation events: %v", err)
	}
	g("automation_events.json", autos)

	// triggers.json — the two event triggers matching on project and
	// component keys; their match_json carries the pre-migration ids T2 must
	// re-point.
	triggers, err := s.ListEventTriggers(store.EventTriggerFilter{})
	if err != nil {
		t.Fatalf("list triggers: %v", err)
	}
	g("triggers.json", triggers)
}

// TestTelemetryGoldensBeforeTree is the telemetry tier: component -> project ->
// workspace rollups, read through the store's ComponentTelemetry /
// ProjectTelemetry / WorkspaceTelemetry. T2 must carry the sums across the
// migration; this golden pins the pre-migration numbers.
func TestTelemetryGoldensBeforeTree(t *testing.T) {
	c := ctcensus.Build(t)
	t.Cleanup(func() { _ = c.S.Close() })
	s, scrub := c.S, c.Scrub
	g := func(name string, d any) { ctStoreGolden(t, scrub, name, d) }

	// component_telemetry.json — every component's rollup. Only the component
	// that owns the loop/step the census recorded telemetry on (c2) has a row;
	// the rest report no row, pinned as Exists:false (P31: an absent rollup is
	// a third state, not a zero sum).
	comps, err := s.ListComponents(store.ComponentFilter{})
	if err != nil {
		t.Fatalf("list components: %v", err)
	}
	sort.Slice(comps, func(i, j int) bool { return scrub[comps[i].ID] < scrub[comps[j].ID] })
	var componentTel []store.ComponentTelemetry
	for _, comp := range comps {
		ct, err := s.ComponentTelemetry(comp.ID)
		if errors.Is(err, store.ErrNotFound) {
			ct = store.ComponentTelemetry{ComponentID: comp.ID}
		} else if err != nil {
			t.Fatalf("component telemetry %s: %v", comp.ID, err)
		}
		componentTel = append(componentTel, ct)
	}
	g("component_telemetry.json", componentTel)

	// project_telemetry.json — every root's rollup (a "project" is a root,
	// CT1), ordered by label. Only the root that owns c2 has a row; the rest
	// are Exists:false.
	type pair struct {
		id  string
		tel any
	}
	roots, err := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil {
		t.Fatalf("list roots: %v", err)
	}
	var pairs []pair
	for _, r := range roots {
		pt, err := s.ComponentTelemetry(r.ID)
		if errors.Is(err, store.ErrNotFound) {
			pt = store.ComponentTelemetry{ComponentID: r.ID}
		} else if err != nil {
			t.Fatalf("root telemetry %s: %v", r.ID, err)
		}
		pairs = append(pairs, pair{r.ID, pt})
	}
	sort.Slice(pairs, func(i, j int) bool { return scrub[pairs[i].id] < scrub[pairs[j].id] })
	var projectTel []any
	for _, p := range pairs {
		projectTel = append(projectTel, p.tel)
	}
	g("project_telemetry.json", projectTel)

	// workspace_telemetry.json — the whole workspace's rollup.
	wt, err := s.WorkspaceTelemetry()
	if err != nil {
		t.Fatalf("workspace telemetry: %v", err)
	}
	g("workspace_telemetry.json", wt)
}

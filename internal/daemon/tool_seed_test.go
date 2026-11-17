package daemon

// tool_seed_test.go — S47 T3's proof on the **engine side**: the one engine
// every door holds (`NewSeedEngine`) carries `tools.core`, declares it inert, and
// applies it at start; every system tool ends up with one row whose `origin` is
// `system`; a second apply writes nothing; and the drift report, run against a
// row a hand edited, reports the disagreement without touching the row.
//
// These live here rather than in `internal/toolseed` for the same reason S46 T10
// recorded for `mcpseed`: the set is mounted by this package, and the adapter
// that turns the engine's `seed.Tx` into the `*sql.Tx` the store needs is
// `toolApplier` below — a start path is proven where the start is built.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/toolseed"
)

func toolSeedEngine(t *testing.T, s *store.Store) *seed.Engine {
	t.Helper()
	e, err := NewSeedEngine(s, seed.Options{}, seedValidatorPass)
	if err != nil {
		t.Fatalf("NewSeedEngine: %v", err)
	}
	return e
}

// TestToolsCoreIsDeclaredInEveryDoorsEngine is the "sets present" assertion: the
// default registry is empty (internal/seed's own test pins that, and a domain
// must not widen it from a distance), so the sets that exist are the ones this
// function mounts — and `tools.core` is one of them, inert.
func TestToolsCoreIsDeclaredInEveryDoorsEngine(t *testing.T) {
	s := openSeedStore(t)
	e := toolSeedEngine(t, s)
	// `seed.status` is the person-facing enumeration of what the engine carries,
	// so visibility there is the assertion — and it is the same read the Settings
	// panel and `aeon seed status` use (there is no registry accessor to peek at,
	// which is what makes the read the honest one).
	st, err := e.Status(context.Background(), toolseed.SetID)
	if err != nil {
		t.Fatalf("seed.status: %v", err)
	}
	if len(st) != 1 {
		t.Fatalf("the engine every door holds shows %d entries for %q — an invisible set is never applied and a lying tracker", len(st), toolseed.SetID)
	}
	if st[0].Class != seed.ClassInert {
		t.Errorf("%s is class %q — applying the catalog permits nothing, so it applies at start (D4)", toolseed.SetID, st[0].Class)
	}
	if st[0].AvailableVersion != toolseed.SetVersion() {
		t.Errorf("the engine carries version %d, the records say %d", st[0].AvailableVersion, toolseed.SetVersion())
	}
	if len(st[0].Records) != len(toolseed.Source()) {
		t.Errorf("seed.status lists %d records, the source has %d", len(st[0].Records), len(toolseed.Source()))
	}
}

// TestApplyCreatesRowsWithSystemOrigin is TC2's provenance half: rows exist for
// every system tool, each written as `origin='system'`, with the alias on `write`
// and the two conditionals flagged.
func TestApplyCreatesRowsWithSystemOrigin(t *testing.T) {
	s := openSeedStore(t)
	ctx := context.Background()
	e := toolSeedEngine(t, s)
	res, err := e.ApplyInert(ctx, seed.Actor{Kind: "system", ID: "startup"})
	if err != nil {
		t.Fatalf("startup apply: %v", err)
	}
	var applied int
	sawSet := false
	for _, r := range res {
		if r.SetID != toolseed.SetID {
			continue
		}
		sawSet = true
		applied = len(r.Records)
	}
	if !sawSet {
		t.Fatal("the startup apply never reached tools.core")
	}
	want := len(toolseed.Source())
	if applied != want {
		t.Fatalf("the apply wrote %d records, the source has %d", applied, want)
	}
	rows, err := s.ToolList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != want {
		t.Fatalf("%d rows after the apply, want %d", len(rows), want)
	}
	byName := map[string]store.ToolRow{}
	for _, r := range rows {
		byName[r.Name] = r
		if r.Origin != "system" {
			t.Errorf("%s has origin %q — only the seed writes here, and it writes system", r.Name, r.Origin)
		}
	}
	for _, name := range []string{"bash", "read", "write", "webfetch", "goal_create", "mcp_servers", "policy_list"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("%s has no row after the apply", name)
		}
	}
	// The class came from the code, not from a default.
	if byName["bash"].RerunClass != "unsafe" {
		t.Errorf("bash is class %q — the only unsafe tool in the registry", byName["bash"].RerunClass)
	}
	if byName["read"].RerunClass != "safe" {
		t.Errorf("read is class %q", byName["read"].RerunClass)
	}
	// The alias rode along rather than becoming its own row.
	var aliases []string
	if err := json.Unmarshal([]byte(byName["write"].Aliases), &aliases); err != nil {
		t.Fatalf("write's aliases are not JSON: %q", byName["write"].Aliases)
	}
	if len(aliases) != 1 || aliases[0] != "write_file" {
		t.Errorf("write carries %v, want [write_file]", aliases)
	}
	if _, leaked := byName["write_file"]; leaked {
		t.Error("write_file has its own row — the alias is a key, not a tool")
	}
	// The conditionals are seeded and flagged even though neither seam is wired
	// in a test engine.
	for _, name := range []string{"search", "graph"} {
		r, ok := byName[name]
		if !ok {
			t.Fatalf("%s has no row in a build that does not wire its seam", name)
		}
		if !r.Conditional {
			t.Errorf("%s is not flagged conditional", name)
		}
	}
	// And the report a person would read says everything agrees.
	d := toolseed.Drift(ctx, rows, toolseed.Source())
	for _, entry := range d {
		if entry.State != toolseed.DriftCurrent {
			t.Errorf("%s reports %s (%v) straight after an apply", entry.Name, entry.State, entry.Fields)
		}
	}
}

// TestApplyIsIdempotent: applying the same set twice writes nothing the second
// time — no churn, no new ledger rows, no moved `updated_at`. A set that rewrote
// its rows at every start would make `updated_at` meaningless and fill the ledger.
func TestApplyIsIdempotent(t *testing.T) {
	s := openSeedStore(t)
	ctx := context.Background()
	e := toolSeedEngine(t, s)
	if _, err := e.ApplyInert(ctx, seed.Actor{Kind: "system", ID: "startup"}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	before, err := s.ToolList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.ApplyInert(ctx, seed.Actor{Kind: "system", ID: "startup"})
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	for _, r := range res {
		if r.SetID != toolseed.SetID {
			continue
		}
		for _, rec := range r.Records {
			if rec.Op != seed.OpSkip {
				t.Errorf("%s re-%s at the second apply — the set churns", rec.Key, rec.Op)
			}
		}
	}
	after, err := s.ToolList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("row count moved %d → %d", len(before), len(after))
	}
	for i := range before {
		if before[i].UpdatedAt != after[i].UpdatedAt || before[i].Version != after[i].Version {
			t.Errorf("%s moved at the second apply (updated_at %q → %q)", before[i].Name, before[i].UpdatedAt, after[i].UpdatedAt)
		}
	}
}

// TestStartupAppliesToolsCore is the whole point of the set existing: a daemon
// start leaves a row per system tool, and the drift report is all `current`
// without anyone asking for an apply.
func TestStartupAppliesToolsCore(t *testing.T) {
	s := openSeedStore(t)
	ctx := context.Background()
	e := toolSeedEngine(t, s)
	ApplyInertAtStart(ctx, e) // the daemon-start hook itself, not a hand-rolled apply
	rows, err := s.ToolList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	honest := len(tools.ToolNames())
	if len(rows) < honest {
		t.Fatalf("%d rows after a start, the honest registry has %d — the catalog is not seeded at start", len(rows), honest)
	}
	n, err := s.ToolCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(toolseed.Source()) {
		t.Errorf("ToolCount %d, the source has %d", n, len(toolseed.Source()))
	}
	st, err := e.Status(ctx, toolseed.SetID)
	if err != nil {
		t.Fatalf("seed.status: %v", err)
	}
	if len(st) != 1 {
		t.Fatalf("seed.status shows %d entries for the set", len(st))
	}
	if st[0].AppliedVersion == 0 {
		t.Errorf("seed.status says the set was never applied after a start: %+v", st[0])
	}
	for _, r := range st[0].Records {
		if r.State != seed.StateApplied {
			t.Errorf("%s is %q after a start (reason %q)", r.Key, r.State, r.Reason)
		}
	}
}

// TestDriftReportNeverWritesAtTheStore is TC11's promise where a write could
// actually happen: report a hand-edited row a hundred times and the row is
// byte-identical afterwards. The only thing that rewrites a row is an apply, and
// an apply of unchanged content is a skip (the test above).
func TestDriftReportNeverWritesAtTheStore(t *testing.T) {
	s := openSeedStore(t)
	ctx := context.Background()
	e := toolSeedEngine(t, s)
	if _, err := e.ApplyInert(ctx, seed.Actor{Kind: "system", ID: "startup"}); err != nil {
		t.Fatal(err)
	}
	if err := s.GormDB().Exec(`UPDATE tool SET notes = 'a hand edit' WHERE name = 'read'`).Error; err != nil {
		t.Fatalf("stage the hand edit: %v", err)
	}
	rows, err := s.ToolList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d := toolseed.Drift(ctx, rows, toolseed.Source())
	entry := driftEntry(d, "read")
	if entry.State != toolseed.DriftDiffers || strings.Join(entry.Fields, ",") != "notes" {
		t.Fatalf("a hand-edited note reports %q/%v, want differs/[notes]", entry.State, entry.Fields)
	}
	for i := 0; i < 100; i++ {
		if got := driftEntry(toolseed.Drift(ctx, rows, toolseed.Source()), "read"); got.State != toolseed.DriftDiffers {
			t.Fatalf("report %d repaired the row", i)
		}
	}
	after, err := s.ToolList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range after {
		if r.Name == "read" && r.Notes != "a hand edit" {
			t.Errorf("the report rewrote the hand edit to %q", r.Notes)
		}
	}
}

func driftEntry(entries []toolseed.DriftEntry, name string) toolseed.DriftEntry {
	for _, e := range entries {
		if e.Name == name {
			return e
		}
	}
	return toolseed.DriftEntry{Name: name, State: "absent_from_report"}
}

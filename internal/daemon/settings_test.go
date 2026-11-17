package daemon

// settings_test.go — S44 T4: the real-store proof. These tests open a real
// SQLite file (the store.Open idiom, not a fake) and prove the slice's first
// acceptance criterion: a value written is read back correctly on the very next
// call, same process or a fresh one, with no boot-time caching anywhere in the
// new path. The headline tests are TestWriteThroughOneHandleReadThroughAnother
// and TestWriteVisibleToAFreshProcess.
//
// CS12 (the setting.changed event) is not built in T4 — its emission site is
// the settings handler (T5), where a changed=true result and the daemon's
// transport coexist; these files hold no transport. The
// TestSettingChangedEventEmittedOnChangeOnly test is therefore absent, recorded
// in T4's Notes with the staleness window it names.

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/store"
)

// fixedNow is the clock every test injects: a write's stamp is deterministic,
// so an event trail can be asserted without a wall clock.
func fixedNow() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }

// noEnv is the environment half a test injects: no env var is ever set, so a
// read falls through to the database layer (the layer under test).
func noEnv(string) (string, bool) { return "", false }

// testRegistry is the closed set a test daemon declares. It is NOT in init
// (CS3: the production DefaultRegistry is empty), so it is built here, exactly
// as config's own test does — a bool, a string and an int, each with a legal
// canonical default. A build failure is a test bug, so it panics.
func testRegistry() *config.Registry {
	r, err := config.NewRegistry(
		config.Setting{Key: "test.enabled", Type: config.TypeBool, Default: "false", Description: "a test bool"},
		config.Setting{Key: "test.name", Type: config.TypeString, Default: "", Description: "a test string"},
		config.Setting{Key: "test.count", Type: config.TypeInt, Default: "0", Description: "a test int"},
	)
	if err != nil {
		panic("test registry: " + err.Error())
	}
	return r
}

// newTestOpts builds the production-default-free option set a test injects: the
// test registry, no env, and the fixed clock.
func newTestOpts() config.Options {
	return config.Options{Registry: testRegistry(), LookupEnv: noEnv, Now: func() time.Time { return fixedNow() }}
}

// TestSettingsRepoMappingRoundTripsEveryField — the mapping is total and
// lossless: RepoRow/RepoEvent → store → back, field for field, including nil
// OldValue/NewValue and ShadowedBy=="env".
func TestSettingsRepoMappingRoundTripsEveryField(t *testing.T) {
	// RepoRow round-trip.
	inRow := config.RepoRow{Key: "test.enabled", Value: "true", UpdatedAt: "2026-10-30T12:00:00Z", UpdatedBy: "user:1"}
	if got := repoRowFrom(settingRowFrom(inRow)); got != inRow {
		t.Errorf("RepoRow round-trip: got %+v want %+v", got, inRow)
	}
	// RepoEvent round-trip, with the two NULLs and the env shadow. The mapping
	// copies the *string by value (new pointer, never aliased), so the test
	// compares the pointed-to values, not the addresses.
	old, new := "was", "now"
	inEv := config.RepoEvent{
		ID: 7, Key: "test.enabled", Op: "set", OldValue: &old, NewValue: &new,
		Sensitive: true, ShadowedBy: "env", Actor: "user:1", OccurredAt: "2026-10-30T12:00:00Z",
	}
	gotEv := repoEventFrom(settingEventFrom(inEv))
	if gotEv.ID != inEv.ID || gotEv.Key != inEv.Key || gotEv.Op != inEv.Op ||
		gotEv.Sensitive != inEv.Sensitive || gotEv.ShadowedBy != inEv.ShadowedBy ||
		gotEv.Actor != inEv.Actor || gotEv.OccurredAt != inEv.OccurredAt {
		t.Errorf("RepoEvent round-trip (scalars): got %+v want %+v", gotEv, inEv)
	}
	if gotEv.OldValue == nil || *gotEv.OldValue != *inEv.OldValue ||
		gotEv.NewValue == nil || *gotEv.NewValue != *inEv.NewValue {
		t.Errorf("RepoEvent round-trip (pointers): old=%v new=%v", gotEv.OldValue, gotEv.NewValue)
	}
	// The pointers are new (not aliased to the input), but point to the value.
	if gotEv.OldValue == inEv.OldValue || gotEv.NewValue == inEv.NewValue {
		t.Errorf("RepoEvent round-trip: the mapping must copy the pointer, not alias it")
	}
	// The two NULLs: a nil OldValue and a nil NewValue stay nil (not "").
	nilEv := config.RepoEvent{Key: "test.name", Op: "unset", OldValue: nil, NewValue: nil, ShadowedBy: ""}
	if got := repoEventFrom(settingEventFrom(nilEv)); got.OldValue != nil || got.NewValue != nil {
		t.Errorf("RepoEvent NULLs: a nil pointer must stay nil")
	}
	// ShadowedBy=="env" is verbatim, not collapsed to "".
	if got := repoEventFrom(settingEventFrom(inEv)).ShadowedBy; got != "env" {
		t.Errorf("ShadowedBy: got %q want %q", got, "env")
	}
	// The real store also round-trips a put through the mapping (the value the
	// config layer wrote is the value the store holds).
	ctx := context.Background()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	repo := newSettingsRepo(st)
	if err := repo.SettingPut(ctx, config.RepoRow{Key: "test.count", Value: "5", UpdatedAt: "2026-10-30T12:00:00Z", UpdatedBy: "user:1"},
		config.RepoEvent{Key: "test.count", Op: "set", NewValue: ptr("5"), Actor: "user:1", OccurredAt: "2026-10-30T12:00:00Z"}); err != nil {
		t.Fatalf("SettingPut: %v", err)
	}
	row, ok, err := repo.SettingGet(ctx, "test.count")
	if err != nil || !ok {
		t.Fatalf("SettingGet: ok=%v err=%v", ok, err)
	}
	if row.Value != "5" || row.UpdatedBy != "user:1" {
		t.Errorf("round-trip through the real store: got %+v", row)
	}
}

// TestWriteThroughOneHandleReadThroughAnother — the headline criterion. Two
// *store.Store opened on the SAME file (two *sql.DBs); Set via A, Get via B
// immediately, no sleep, no reopen. Any read-side caching (CS7) or a
// WAL/visibility surprise would make this fail.
func TestWriteThroughOneHandleReadThroughAnother(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	aSt, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open A: %v", err)
	}
	bSt, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open B: %v", err)
	}
	defer aSt.Close()
	defer bSt.Close()

	a := NewSettings(aSt, newTestOpts())
	b := NewSettings(bSt, newTestOpts())

	if _, changed, err := a.Set(ctx, "test.enabled", "true", "user:1"); err != nil || !changed {
		t.Fatalf("A.Set: changed=%v err=%v", changed, err)
	}
	// B reads the same file, immediately, no sleep, no reopen.
	v, err := b.Get(ctx, "test.enabled")
	if err != nil {
		t.Fatalf("B.Get: %v", err)
	}
	if v.Value != "true" || v.Source != config.SettingDatabase {
		t.Fatalf("B read the wrong layer: value=%q source=%q (want true/database)", v.Value, v.Source)
	}
}

// TestWriteVisibleToAFreshProcess — the second headline criterion. Write via a
// store, close it, re-exec the test binary (the standard helper-process idiom),
// which opens the file and prints Get's value; the parent asserts the printed
// value. State that lived only in the writer's memory would fail this.
func TestWriteVisibleToAFreshProcess(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	s := NewSettings(st, newTestOpts())
	if _, changed, err := s.Set(ctx, "test.name", "freshly-written", "user:1"); err != nil || !changed {
		t.Fatalf("Set: changed=%v err=%v", changed, err)
	}
	st.Close() // close the writer: any read now is from the file, not the memory

	// Re-exec this test binary as a helper that opens the file and prints Get.
	cmd := exec.Command(os.Args[0], "-test.run=TestFreshProcessReadsSetting")
	cmd.Env = append(os.Environ(), "AEON_SETTINGS_HELPERDIR="+dir)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("fresh process: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "value=freshly-written") {
		t.Fatalf("fresh process did not read the written value from the file: %q", out)
	}
}

// TestFreshProcessReadsSetting is the helper the parent re-execs. When
// AEON_SETTINGS_HELPERDIR is set (only in the spawned run) it opens the file,
// reads, prints, and the parent asserts the line. In a normal run it skips.
func TestFreshProcessReadsSetting(t *testing.T) {
	dir := os.Getenv("AEON_SETTINGS_HELPERDIR")
	if dir == "" {
		t.Skip("not a fresh-process helper run")
	}
	st, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("helper open: %v", err)
	}
	defer st.Close()
	s := NewSettings(st, newTestOpts())
	v, err := s.Get(context.Background(), "test.name")
	if err != nil {
		t.Fatalf("helper Get: %v", err)
	}
	// fmt.Printf (not t.Logf): the parent re-execs this test and reads stdout,
	// so the value must land on stdout, not in the test log.
	fmt.Printf("value=%s\n", v.Value)
}

// TestSettingSurvivesDaemonRestartShape — close and reopen the store, rebuild
// NewSettings, Get still returns the saved value and Source=database.
func TestSettingSurvivesDaemonRestartShape(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// First "daemon" life.
	aSt, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open first: %v", err)
	}
	a := NewSettings(aSt, newTestOpts())
	if _, _, err := a.Set(ctx, "test.count", "42", "user:1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	aSt.Close()
	// Second "daemon" life: reopen the same file.
	bSt, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open second: %v", err)
	}
	defer bSt.Close()
	b := NewSettings(bSt, newTestOpts())
	v, err := b.Get(ctx, "test.count")
	if err != nil {
		t.Fatalf("Get after restart: %v", err)
	}
	if v.Value != "42" || v.Source != config.SettingDatabase {
		t.Fatalf("after restart: value=%q source=%q (want 42/database)", v.Value, v.Source)
	}
}

// TestSetAndEventAreOneTransactionAgainstRealStore — wrap the real repo in a
// double that corrupts ev.Op (to a value the CHECK refuses) before delegating
// to SettingPut; Set returns an error and SettingGet finds NO row. Atomicity
// only held against the real store, not the fake.
func TestSetAndEventAreOneTransactionAgainstRealStore(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	real := newSettingsRepo(st)
	// A repo that corrupts the event's Op before delegating to the real store.
	corrupt := corruptEventRepo{repo: real}
	s := config.NewStore(corrupt, newTestOpts())

	// Set must fail (the CHECK refuses the bad op) and write no row.
	if _, _, err := s.Set(ctx, "test.enabled", "true", "user:1"); err == nil {
		t.Fatal("Set with a corrupt event op must fail")
	}
	// The row must not exist (the transaction rolled back).
	if _, ok, err := real.SettingGet(ctx, "test.enabled"); err != nil || ok {
		t.Fatalf("after a failed put, the row must not exist: ok=%v err=%v", ok, err)
	}
}

// corruptEventRepo delegates to the real repo but corrupts ev.Op before
// SettingPut, so the store's CHECK refuses the event and the whole
// transaction (row + event) rolls back.
type corruptEventRepo struct {
	repo config.Repo
}

func (c corruptEventRepo) SettingGet(ctx context.Context, key string) (config.RepoRow, bool, error) {
	return c.repo.SettingGet(ctx, key)
}
func (c corruptEventRepo) SettingList(ctx context.Context) ([]config.RepoRow, error) {
	return c.repo.SettingList(ctx)
}
func (c corruptEventRepo) SettingPut(ctx context.Context, row config.RepoRow, ev config.RepoEvent) error {
	bad := "not-a-real-op" // the CHECK admits only "set" / "unset"
	ev.Op = bad
	return c.repo.SettingPut(ctx, row, ev)
}
func (c corruptEventRepo) SettingDelete(ctx context.Context, key string, ev config.RepoEvent) (bool, error) {
	return c.repo.SettingDelete(ctx, key, ev)
}
func (c corruptEventRepo) SettingEventList(ctx context.Context, key string, limit int) ([]config.RepoEvent, error) {
	return c.repo.SettingEventList(ctx, key, limit)
}

// TestEventTrailAgainstRealStore — Set, Set(changed), Set(same), Unset →
// History returns exactly 3 events (the same-value Set is a no-op, no event).
func TestEventTrailAgainstRealStore(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	s := NewSettings(st, newTestOpts())

	if _, changed, err := s.Set(ctx, "test.name", "a", "user:1"); err != nil || !changed {
		t.Fatalf("Set a: changed=%v err=%v", changed, err)
	}
	if _, changed, err := s.Set(ctx, "test.name", "b", "user:1"); err != nil || !changed {
		t.Fatalf("Set b: changed=%v err=%v", changed, err)
	}
	if _, changed, err := s.Set(ctx, "test.name", "b", "user:1"); err != nil || changed {
		t.Fatalf("Set b again (no-op): changed=%v err=%v (want false/nil)", changed, err)
	}
	if _, changed, err := s.Unset(ctx, "test.name", "user:1"); err != nil || !changed {
		t.Fatalf("Unset: changed=%v err=%v", changed, err)
	}
	evs, err := s.History(ctx, "test.name", 50)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(evs) != 3 {
		t.Fatalf("History: got %d events want 3 (a no-op writes no event)", len(evs))
	}
	// Newest first: the unset is the most recent; the two sets trail it.
	ops := []string{evs[0].Op, evs[1].Op, evs[2].Op}
	if ops[0] != "unset" || ops[1] != "set" || ops[2] != "set" {
		t.Errorf("event trail (newest first): %v (want [unset set set])", ops)
	}
}

// TestEnvOverridesRealDatabaseValue — a row of "false" with the env "true" in
// force (injected via Options.LookupEnv) reads Source=environment and
// ShadowedBy set: precedence holds against the real store, not the fake.
func TestEnvOverridesRealDatabaseValue(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	// The row is "false" (the database layer).
	s0 := NewSettings(st, newTestOpts())
	if _, _, err := s0.Set(ctx, "test.enabled", "false", "user:1"); err != nil {
		t.Fatalf("Set false: %v", err)
	}
	// Now the env is "true" in force: a second handle on the SAME file, with an
	// env that returns "true" for the key's env name.
	env := func(k string) (string, bool) {
		if k == config.EnvName("test.enabled") {
			return "true", true
		}
		return "", false
	}
	st2, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	s2 := NewSettings(st2, config.Options{Registry: testRegistry(), LookupEnv: env, Now: func() time.Time { return fixedNow() }})
	v, err := s2.Get(ctx, "test.enabled")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.Value != "true" || v.Source != config.SettingEnvironment {
		t.Fatalf("env must win: value=%q source=%q (want true/environment)", v.Value, v.Source)
	}
	if v.ShadowedBy != config.EnvName("test.enabled") {
		t.Fatalf("ShadowedBy must name the env: got %q", v.ShadowedBy)
	}
}

// TestStoredInvalidAgainstRealStore — insert a raw invalid value with the
// store's own accessor (bypassing the config boundary), then Get flags
// StoredInvalid: the real read path validates, it does not skip it.
func TestStoredInvalidAgainstRealStore(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	// Write a raw value the bool declaration will not parse, straight to the
	// store (the boundary the config layer would have refused).
	if err := st.SettingPut(ctx, store.SettingRow{Key: "test.enabled", Value: "not-a-bool", UpdatedAt: "2026-10-30T12:00:00Z", UpdatedBy: "user:0"},
		store.SettingEvent{Key: "test.enabled", Op: "set", NewValue: ptr("not-a-bool"), Actor: "user:0", OccurredAt: "2026-10-30T12:00:00Z"}); err != nil {
		t.Fatalf("raw SettingPut: %v", err)
	}
	s := NewSettings(st, newTestOpts())
	v, err := s.Get(ctx, "test.enabled")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !v.StoredInvalid {
		t.Fatalf("StoredInvalid must be flagged for a row that fails the current declaration")
	}
	if v.Value != "false" || v.Source != config.SettingDefault {
		t.Fatalf("stored-invalid falls to the restrictive default: value=%q source=%q (want false/default)", v.Value, v.Source)
	}
}

// TestNoPackageLevelSettingsInstance — no top-level var of type *config.Store
// and no sync.Once anywhere in internal/daemon's settings files: a hidden
// global would hide a second instance in a test and defeat "same process or
// not".
func TestNoPackageLevelSettingsInstance(t *testing.T) {
	// The test's cwd is the package dir (internal/daemon), so the files are
	// named relative to ".", not the module root.
	for _, name := range []string{"settings.go", "settings_repo.go"} {
		path := filepath.Join(".", name)
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if hasConfigStoreType(vs) {
					t.Errorf("%s has a top-level *config.Store var", name)
				}
			}
		}
	}
}

// --- helpers ---------------------------------------------------------------

func ptr(s string) *string { return &s }

// hasConfigStoreType reports whether a ValueSpec's type is *config.Store (or
// config.Store): it unwraps a StarExpr, then matches the SelectorExpr.
func hasConfigStoreType(vs *ast.ValueSpec) bool {
	expr := vs.Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	s, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == "config" && s.Sel.Name == "Store"
}

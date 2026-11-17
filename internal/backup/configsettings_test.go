package backup

// configsettings_test.go — S62 T10: the seam over the config store, each
// precedence layer and each of its voices a case. The repo is an in-memory
// fake and the env is a map, so no case touches the process environment or
// a real setting table.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
)

// fakeSettingsRepo is an in-memory config.Repo. rows is the table; err makes
// every call fail (the "unreadable" layer); gets counts SettingGet calls.
type fakeSettingsRepo struct {
	rows map[string]config.RepoRow
	err  error
	gets int
}

func (f *fakeSettingsRepo) SettingGet(ctx context.Context, key string) (config.RepoRow, bool, error) {
	f.gets++
	if f.err != nil {
		return config.RepoRow{}, false, f.err
	}
	r, ok := f.rows[key]
	return r, ok, nil
}

func (f *fakeSettingsRepo) SettingList(ctx context.Context) ([]config.RepoRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]config.RepoRow, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeSettingsRepo) SettingPut(ctx context.Context, row config.RepoRow, ev config.RepoEvent) error {
	if f.err != nil {
		return f.err
	}
	f.rows[row.Key] = row
	return nil
}

func (f *fakeSettingsRepo) SettingDelete(ctx context.Context, key string, ev config.RepoEvent) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	_, ok := f.rows[key]
	delete(f.rows, key)
	return ok, nil
}

func (f *fakeSettingsRepo) SettingEventList(ctx context.Context, key string, limit int) ([]config.RepoEvent, error) {
	return nil, nil
}

// envFunc adapts a map to the established-AEON_BACKUP_* lookup seam.
func envFunc(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// newCfgStore builds the S44 accessor over the fake repo with a map env.
func newCfgStore(t *testing.T, repo *fakeSettingsRepo, env map[string]string) *config.Store {
	t.Helper()
	return config.NewStore(repo, config.Options{
		LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok },
	})
}

// TestBackupSettingsPrecedence (T10) walks the whole precedence the seam
// answers — established env, S44 env, the row, the default — and each
// layer's source: env beats db beats default, and the status view's
// settings:[{key,value,source}] line reports which one won.
func TestBackupSettingsPrecedence(t *testing.T) {
	// No env, no rows: every key answers its declared default, source
	// "default" — exactly what EnvSettings answered before the adoption.
	cs := NewConfigSettingsWith(newCfgStore(t, &fakeSettingsRepo{rows: map[string]config.RepoRow{}}, nil), envFunc(nil))
	if v, src := cs.Duration(KeyInterval); v != DefaultInterval || src != "default" {
		t.Errorf("no layer: interval = %s %q, want %s default", v, src, DefaultInterval)
	}
	if v, src := cs.Int(KeyKeep); v != DefaultKeep || src != "default" {
		t.Errorf("no layer: keep = %d %q, want %d default", v, src, DefaultKeep)
	}
	if v, src := cs.Bool(KeyEnabled); !v || src != "default" {
		t.Errorf("no layer: enabled = %v %q, want true default", v, src)
	}
	if v, src := cs.String(KeyDir); v != "" || src != "default" {
		t.Errorf("no layer: dir = %q %q, want \"\" default", v, src)
	}

	rows := map[string]config.RepoRow{KeyInterval: {Key: KeyInterval, Value: "2h"}}

	// The database layer: a row beats the default, source "db".
	cs = NewConfigSettingsWith(newCfgStore(t, &fakeSettingsRepo{rows: rows}, nil), envFunc(nil))
	if v, src := cs.Duration(KeyInterval); v != 2*time.Hour || src != "db" {
		t.Errorf("row: interval = %s %q, want 2h db", v, src)
	}

	// The established env beats the row, source "env".
	cs = NewConfigSettingsWith(newCfgStore(t, &fakeSettingsRepo{rows: rows}, nil), envFunc(map[string]string{envKey[KeyInterval]: "3h"}))
	if v, src := cs.Duration(KeyInterval); v != 3*time.Hour || src != "env" {
		t.Errorf("env over row: interval = %s %q, want 3h env", v, src)
	}

	// The S44 env (AEON_SETTING_*) beats the row too, source "env".
	cs = NewConfigSettingsWith(newCfgStore(t, &fakeSettingsRepo{rows: rows}, map[string]string{config.EnvName(KeyInterval): "4h"}), envFunc(nil))
	if v, src := cs.Duration(KeyInterval); v != 4*time.Hour || src != "env" {
		t.Errorf("s44 env over row: interval = %s %q, want 4h env", v, src)
	}

	// ...but the established env outranks the S44 env — it existed first
	// and its users must not be shadowed by the adoption.
	cs = NewConfigSettingsWith(newCfgStore(t, &fakeSettingsRepo{rows: rows}, map[string]string{config.EnvName(KeyInterval): "4h"}), envFunc(map[string]string{envKey[KeyInterval]: "3h"}))
	if v, src := cs.Duration(KeyInterval); v != 3*time.Hour || src != "env" {
		t.Errorf("established over s44 env: interval = %s %q, want 3h env", v, src)
	}

	// A set-but-unparseable established value is "invalid" — the P31 third
	// state — and the resolver refuses it rather than falling back.
	cs = NewConfigSettingsWith(newCfgStore(t, &fakeSettingsRepo{rows: rows}, nil), envFunc(map[string]string{envKey[KeyInterval]: "banana"}))
	if v, src := cs.Duration(KeyInterval); v != 0 || src != "invalid" {
		t.Errorf("garbage env: interval = %s %q, want 0 invalid", v, src)
	}
	if _, err := ResolveSettings(cs); err == nil {
		t.Error("a set-but-unparseable value must be refused by the resolver, not defaulted")
	}

	// A set-but-unparseable S44 env is S44's fall-through, not this seam's
	// refusal voice: the row's value stands, source "db".
	s44Rows := map[string]config.RepoRow{KeyKeep: {Key: KeyKeep, Value: "7"}}
	cs = NewConfigSettingsWith(newCfgStore(t, &fakeSettingsRepo{rows: s44Rows}, map[string]string{config.EnvName(KeyKeep): "banana"}), envFunc(nil))
	if v, src := cs.Int(KeyKeep); v != 7 || src != "db" {
		t.Errorf("garbage s44 env: keep = %d %q, want 7 db (S44's fall-through)", v, src)
	}

	// A table that cannot be read is "unreadable": the accessor answers its
	// declared default, named, and the resolver still answers (a run on a
	// known-good default beats a run refused over a layer fault).
	fault := &fakeSettingsRepo{rows: rows, err: errors.New("database is locked")}
	cs = NewConfigSettingsWith(newCfgStore(t, fault, nil), envFunc(nil))
	if v, src := cs.Duration(KeyInterval); v != DefaultInterval || src != "unreadable" {
		t.Errorf("faulty table: interval = %s %q, want %s unreadable", v, src, DefaultInterval)
	}
	if v, src := cs.Int(KeyKeep); v != DefaultKeep || src != "unreadable" {
		t.Errorf("faulty table: keep = %d %q, want %d unreadable", v, src, DefaultKeep)
	}
	if _, err := ResolveSettings(cs); err != nil {
		t.Errorf("an unreadable table must not refuse the run: %v", err)
	}
}

// TestInvalidSettingRefusedAtSetTime (T10): the declaration carries the
// range for what it can (the int keys' Min 1), and a value outside it is
// refused at the write — candidate first, the row never reached on a
// refusal — while a value inside it lands in the table and is answered
// live from the database layer.
func TestInvalidSettingRefusedAtSetTime(t *testing.T) {
	repo := &fakeSettingsRepo{rows: map[string]config.RepoRow{}}
	s := newCfgStore(t, repo, nil)
	ctx := context.Background()

	// A keep below the declared Min: refused, and nothing written.
	if _, _, err := s.Set(ctx, "backup.keep", "0", "user:1000"); err == nil {
		t.Error("a keep of zero must be refused at set time (Min 1): a keep of zero deletes everything it keeps")
	}
	// Not an int: refused.
	if _, _, err := s.Set(ctx, "backup.keep", "abc", "user:1000"); err == nil {
		t.Error("a non-int keep must be refused at set time")
	}
	// Not a bool: refused (S44's bool is "true"/"false", no looser forms).
	if _, _, err := s.Set(ctx, "backup.enabled", "yes", "user:1000"); err == nil {
		t.Error("a non-bool enabled must be refused at set time")
	}
	// Refusals are candidate-first: the table holds nothing after them.
	if len(repo.rows) != 0 {
		t.Errorf("a refused write must not reach the table; rows = %v", repo.rows)
	}

	// A duration key's text form is its canonical value: the declaration
	// admits it, and the write is answered live from the database layer.
	if _, changed, err := s.Set(ctx, "backup.interval", "2h", "user:1000"); err != nil || !changed {
		t.Fatalf("a valid interval write: changed=%v err=%v", changed, err)
	}
	cs := NewConfigSettingsWith(s, envFunc(nil))
	if v, src := cs.Duration(KeyInterval); v != 2*time.Hour || src != "db" {
		t.Errorf("after the write: interval = %s %q, want 2h db", v, src)
	}
}

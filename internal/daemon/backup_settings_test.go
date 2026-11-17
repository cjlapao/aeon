package daemon

// backup_settings_test.go — S62 T10: the daemon's read seam over the config
// store. The behaviour that matters to an operator: a change written by the
// panel or the CLI is live on the schedule's NEXT read — no restart, no
// cache (CS7: the read is one row read plus one env lookup, every time).

import (
	"context"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/store"
)

// TestChangingIntervalAffectsTheNextTick (T10) drives the ticker's own read
// site — the resolved() a tick performs — before and after one write on the
// daemon's shared settings store: the first read answers the declared
// default, the write lands in the table, and the next read answers the row
// live, source "db".
func TestChangingIntervalAffectsTheNextTick(t *testing.T) {
	st, err := store.Open(store.Options{
		Dir: t.TempDir(),
		Env: func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("open the throwaway store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// The daemon's one settings constructor (S44 T4) over the same store —
	// the shared *config.Store the service reads through (T10's swap) — with
	// a hermetic env so no AEON_* on this box can shadow the case.
	cs := NewSettings(st, config.Options{
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	svc := NewBackupService(st, cs, "9.9.9-test", func(string, any) {}, discardLogger{})
	if svc == nil {
		t.Fatal("the service over a live store answered nil")
	}

	// Tick 1: no row, no env — the declared default, source "default".
	res, err := svc.resolved()
	if err != nil {
		t.Fatalf("resolve the defaults: %v", err)
	}
	if res.Interval != backup.DefaultInterval {
		t.Fatalf("first read Interval = %s, want the default %s", res.Interval, backup.DefaultInterval)
	}
	if got := res.Sources[backup.KeyInterval]; got != "default" {
		t.Fatalf("first read source = %q, want %q", got, "default")
	}

	// The panel's write: one Set on the shared settings store.
	if _, changed, err := cs.Set(context.Background(), "backup.interval", "2h", "user:1000"); err != nil || !changed {
		t.Fatalf("write the new interval: changed=%v err=%v", changed, err)
	}

	// The next tick's read sees it live: nothing between the write and the
	// read was rebuilt, re-registered or restarted.
	res, err = svc.resolved()
	if err != nil {
		t.Fatalf("resolve after the change: %v", err)
	}
	if res.Interval != 2*time.Hour {
		t.Fatalf("next read Interval = %s, want 2h — the change is not live", res.Interval)
	}
	if got := res.Sources[backup.KeyInterval]; got != "db" {
		t.Fatalf("next read source = %q, want %q", got, "db")
	}
}

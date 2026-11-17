package store

// A closed store answers. It does not crash the process.
//
// # Why this file exists
//
// `Close()` used to set both `*sql.DB` pools to nil, which turned every read
// that arrived afterwards into a nil dereference inside `database/sql` — a
// segfault with no sentence, in a goroutine whose caller had no idea it was
// talking to the store. The case that found it is the daemon's own:
// `daemon/queue_reactor.go` dispatches every loop-terminal event to a goroutine
// (V18: the queue must never delay a step), so a loop finishing as a test tears
// down calls `QueueItemByLoopSlug` on a store that has just been closed, and the
// whole test binary dies instead of one report being lost.
//
// `sql.DB.Close` is idempotent and a statement against a closed pool returns
// `sql: database is closed`, so the pools now stay in place and `closed` says
// what happened. The behaviour this file pins is the difference between an error
// a caller logs and a dead daemon: P3's three-state honesty, applied to the
// store's own lifetime.

import (
	"context"
	"strings"
	"testing"
)

func closedStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestACloseStoreAnswersRatherThanCrashing(t *testing.T) {
	s := closedStore(t)
	if s.Closed() {
		t.Fatal("an open store says it is closed")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !s.Closed() {
		t.Fatal("Close did not say so")
	}

	// The read that used to segfault: a nil *sql.DB reaches database/sql before
	// any of our code can answer, so a panic HERE is the regression.
	if _, _, err := s.QueueItemByLoopSlug("/some/root", "the-loop"); err == nil {
		t.Fatal("a read on a closed store answered no error")
	} else if !strings.Contains(err.Error(), "closed") {
		t.Errorf("the read error does not say the store is closed: %v", err)
	}

	// The two places that asked "is this open?" by testing the pools for nil
	// still say the true thing now that the pools stay in place.
	if err := s.Health(context.Background()); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("Health on a closed store = %v, want the closed sentence", err)
	}
	if _, err := s.SnapshotTo(context.Background(), t.TempDir()+"/backup.db"); err == nil ||
		!strings.Contains(err.Error(), "closed") {
		t.Errorf("SnapshotTo on a closed store = %v, want the closed sentence", err)
	}

	// Idempotent: the second Close is a no-op, not an error and not a panic.
	if err := s.Close(); err != nil {
		t.Errorf("the second Close returned %v", err)
	}
}

func TestACloseStoreStillReportsItsPath(t *testing.T) {
	// The panic took the process with it, so nothing was ever printed. The
	// minimum a daemon operator needs after the fact is WHICH database — the
	// fields Close does not disturb stay readable.
	s := closedStore(t)
	path := s.Path()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.Path() != path || path == "" {
		t.Errorf("Path() after Close = %q, want %q", s.Path(), path)
	}
}

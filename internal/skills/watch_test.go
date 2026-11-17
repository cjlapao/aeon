package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// S66 T1. These tests use the REAL fsnotify watcher over temp dirs —
// a fake watcher would test the debounce only and swear it tested the
// watching. Timing assertions carry a 5 s budget so a loaded box is
// slow, never wrongly red; the debounce itself is injected at 20 ms so
// the burst test measures COLLAPSE, not patience.

type watchHarness struct {
	svc    *Service
	events chan ChangeEvent
	roots  [2]string // user, managed
}

func newWatchHarness(t *testing.T, missingRoots bool) *watchHarness {
	t.Helper()
	user, managed := t.TempDir(), t.TempDir()
	if missingRoots {
		user = filepath.Join(t.TempDir(), "not-there-yet")
		managed = filepath.Join(t.TempDir(), "also-not-there")
	}
	h := &watchHarness{roots: [2]string{user, managed}, events: make(chan ChangeEvent, 16)}
	svc, err := NewService(Options{
		Getenv: func(k string) string {
			switch k {
			case "AEON_USER_SKILLS_DIR":
				return user
			case "AEON_SKILLS_DIR":
				return managed
			}
			return ""
		},
		Clock:         time.Now,
		WatchDebounce: 20 * time.Millisecond,
		OnWatchChange: func(ev ChangeEvent) { h.events <- ev },
	})
	if err != nil {
		t.Fatal(err)
	}
	h.svc = svc
	return h
}

func (h *watchHarness) start(t *testing.T) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	if err := h.svc.StartWatch(ctx); err != nil {
		t.Fatalf("StartWatch: %v", err)
	}
	t.Cleanup(func() { cancel() })
	return cancel
}

// waitForEvent blocks for one announced diff, failing red past the
// budget. An unannounced change is exactly the silence S66 exists to
// delete, so tests that EXPECT nothing use quietFor, never this.
func (h *watchHarness) waitForEvent(t *testing.T) ChangeEvent {
	t.Helper()
	select {
	case ev := <-h.events:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher stayed silent through a change")
		return ChangeEvent{}
	}
}

func (h *watchHarness) quietFor(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case ev := <-h.events:
		t.Fatalf("an event nobody asked for: %+v", ev)
	case <-time.After(d):
	}
}

func writeSkillFile(t *testing.T, dir, name, extra string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: Watched by the daemon since S66.\n---\n\nbody " + extra + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestWatchNoticesAnAddedFolder(t *testing.T) {
	h := newWatchHarness(t, false)
	h.start(t)
	writeSkillFile(t, filepath.Join(h.roots[1], "docx"), "docx", "one")

	ev := h.waitForEvent(t)
	if !contains(ev.Added, "docx") {
		t.Fatalf("the new folder must arrive as an Added name, got %+v", ev)
	}
	if !ev.Watching || ev.WatchError != "" {
		t.Fatalf("an event from a watched daemon must say watching, got %+v", ev)
	}
	// The cache moved too: a later unforced Scan answers the new world.
	res, err := h.svc.Scan(false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Watching || len(res.Skills) != 1 || res.Skills[0].Name != "docx" {
		t.Fatalf("the refresh the watcher triggered must reach the cache: %+v", res.Skills)
	}
}

func TestWatchNoticesRemovalAndChange(t *testing.T) {
	h := newWatchHarness(t, false)
	d := filepath.Join(h.roots[0], "caveman")
	writeSkillFile(t, d, "caveman", "short")
	h.svc.Invalidate()
	if _, err := h.svc.Scan(true); err != nil {
		t.Fatal(err)
	}
	h.start(t)

	writeSkillFile(t, d, "caveman", "a much longer body than before, so the judged shape moves")
	if ev := h.waitForEvent(t); !contains(ev.Changed, "caveman") {
		t.Fatalf("an edited skill must arrive as Changed, got %+v", ev)
	}
	if err := os.RemoveAll(d); err != nil {
		t.Fatal(err)
	}
	if ev := h.waitForEvent(t); !contains(ev.Removed, "caveman") {
		t.Fatalf("a deleted folder must arrive as Removed, got %+v", ev)
	}
}

func TestWatchDebounceCollapsesABurst(t *testing.T) {
	h := newWatchHarness(t, false)
	h.start(t)
	d := filepath.Join(h.roots[1], "burst")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		body := strings.Repeat("x", 100+i*10) // every write really changes the bytes
		if err := os.WriteFile(filepath.Join(d, "SKILL.md"),
			[]byte("---\nname: burst\ndescription: A burst of writes.\n---\n\n"+body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The burst must land as ONE arrival, and the only follow-up the
	// watcher may ever announce about it is the same folder's own shape
	// settling — never a second Added, never other names.
	first := true
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-h.events:
			if first {
				if !contains(ev.Added, "burst") || len(ev.Added) != 1 || len(ev.Removed) != 0 {
					t.Fatalf("the burst must land as one Added, got %+v", ev)
				}
				first = false
			} else {
				for _, n := range append(append(append([]string{}, ev.Added...), ev.Removed...), ev.Changed...) {
					if n != "burst" {
						t.Fatalf("a follow-up event may only concern the bursting folder, got %+v", ev)
					}
				}
				if len(ev.Added) != 0 || len(ev.Removed) != 0 {
					t.Fatalf("after the one arrival there are no more arrivals, got %+v", ev)
				}
			}
		case <-deadline:
			if first {
				t.Fatal("the burst never announced anything")
			}
			return
		}
	}
}

func TestWatchMissingRootsIsSpokenNotSilent(t *testing.T) {
	h := newWatchHarness(t, true)
	h.start(t)
	res, err := h.svc.Scan(false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Watching {
		t.Fatal("a daemon with no root to watch must not claim it watches")
	}
	if !strings.Contains(res.WatchError, "no root existed to watch") {
		t.Fatalf("the blind state must NAME its blindness, got %q", res.WatchError)
	}
	// A root created later is still discovered — the honest fallback for
	// the unwatched path is the next scan, and it must answer.
	if err := os.MkdirAll(h.roots[0], 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillFile(t, filepath.Join(h.roots[0], "late"), "late", "")
	res, err = h.svc.Scan(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 1 || res.Skills[0].Name != "late" {
		t.Fatalf("a late root must still be discovered by a forced scan, got %+v", res.Skills)
	}
}

func TestWatchIgnoresOurOwnDotTrees(t *testing.T) {
	h := newWatchHarness(t, false)
	h.start(t)
	// The installer's staging and trash folders live INSIDE the managed
	// root; watching them would let the daemon chase its own writes.
	for _, name := range []string{".staging-docx", ".trash-docx-1730000000"} {
		d := filepath.Join(h.roots[1], name)
		writeSkillFile(t, d, "hidden", "not a skill")
	}
	h.quietFor(t, 300*time.Millisecond)
	res, err := h.svc.Scan(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 0 {
		t.Fatalf("dot entries are not skills: %+v", res.Skills)
	}
}

func TestWatchStateTravelsWithEveryScan(t *testing.T) {
	h := newWatchHarness(t, false)
	res, err := h.svc.Scan(false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Watching || !strings.Contains(res.WatchError, "never started") {
		t.Fatalf("before StartWatch the honest answer is unwatched-because-never-started, got %+v", res)
	}
	h.start(t)
	res, err = h.svc.Scan(false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Watching || res.WatchError != "" {
		t.Fatalf("after StartWatch every scan carries the watch, got %+v", res)
	}
}

func TestStartWatchTwiceRefuses(t *testing.T) {
	h := newWatchHarness(t, false)
	h.start(t)
	if err := h.svc.StartWatch(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "already running") {
		t.Fatalf("a second watch on one service must refuse with the reason, got %v", err)
	}
}

func TestWatchDiffAgainstNilIsQuiet(t *testing.T) {
	// The first refresh (no cache yet) must not announce the entire
	// catalog as "added" — boot lists the world, the diff reports NEWS.
	ev := diffScans(nil, &ScanResult{Skills: []*Skill{{Name: "a"}, {Name: "b"}}})
	if len(ev.Added) != 0 || len(ev.Removed) != 0 || len(ev.Changed) != 0 {
		t.Fatalf("no baseline means no news, got %+v", ev)
	}
}

package skills

// watch.go is S66 T1: the catalog notices a folder someone drops in
// without being asked. The S65 record said the cache never goes stale on
// a timer because "a timer is a second clock nobody declared" — this
// replaces the timer with an honest listener: fsnotify says a root
// changed, the service re-derives, and `skills.changed` carries the
// diff. Everything here degrades spoken, never silently: a root that did
// not exist at boot is not watched and says so (it is still discovered
// by any later scan), a watcher that could not be made sets Watching
// false with the reason, and a daemon whose watch failed boots exactly
// as it did before S66.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DefaultWatchDebounce is how long the watcher waits for a quiescent
// disk before re-deriving. One `git clone` of a skill fires dozens of
// events; scanning per event would let a partial folder answer the
// socket. A named constant, not a magic sleep: it is the same argument
// the install staging makes — never serve a half-written tree.
const DefaultWatchDebounce = 500 * time.Millisecond

// ChangeEvent is the diff one refresh produced, in the shape the daemon
// puts on the wire as `skills.changed`. Names, not contents: a listener
// that wants the truth reads it back through skills.list, so the event
// stays small even when an operator syncs a hundred folders.
type ChangeEvent struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Changed []string `json:"changed"`
	// Watching/WatchError ride every event so a listener that joined
	// mid-life still learns the watch is off — a silent event stream
	// from a blind watcher would be the confident wrong answer.
	Watching   bool   `json:"watching"`
	WatchError string `json:"watchError,omitempty"`
}

// watchOptions are Options fields read at StartWatch.
type watchState struct {
	enabled bool
	reason  string   // why it is NOT watching, when enabled is false
	errs    []string // spoken per-path Add failures, joined into WatchError
}

func (w *watchState) watchError() string {
	if !w.enabled {
		if w.reason != "" {
			return w.reason
		}
		return "the watch was never started — skills list answers from the last scan"
	}
	if len(w.errs) == 0 {
		return ""
	}
	return strings.Join(w.errs, "; ")
}

// Settle every event through one debounce window and refresh.
type debounce struct {
	armed chan struct{}
}

func (s *Service) StartWatch(ctx context.Context) error {
	s.mu.Lock()
	if s.watching {
		s.mu.Unlock()
		return errTwiceWatch
	}
	s.mu.Unlock()

	w, err := fsnotify.NewWatcher()
	if err != nil {
		s.setWatch(false, "the watcher could not be created: "+err.Error(), nil)
		return err
	}
	state := &watchState{}
	watched := map[string]bool{}
	for _, root := range s.Roots() {
		if err := s.watchPath(w, root.Dir, watched, state); err != nil {
			// The root itself missing is the empty first-run case, not a
			// fault — spoken in the reason list, never an error back to
			// serve. A present-but-unwatchable root (permissions) says so
			// through the same list.
			continue
		}
		// Subfolders that already exist are watched now; the loop adds
		// ones that appear. inotify does not recurse, so this is where
		// recursion is paid for.
		if entries, derr := os.ReadDir(root.Dir); derr == nil {
			for _, e := range entries {
				if !e.IsDir() || skipWatchName(e.Name()) {
					continue
				}
				_ = s.watchPath(w, filepath.Join(root.Dir, e.Name()), watched, state)
			}
		}
	}
	if len(watched) == 0 {
		_ = w.Close()
		s.setWatch(false, "no root existed to watch ("+strings.Join(rootPaths(s.Roots()), ", ")+") — a root created later is discovered by the next scan", nil)
		return nil
	}

	// The baseline is established HERE, synchronously: the first diff
	// needs something to diff against. Left to the event path, the
	// FIRST real addition would be silently consumed as the baseline —
	// the exact silence this slice exists to delete.
	if _, err := s.Scan(true); err != nil {
		state.errs = append(state.errs, "the baseline scan failed: "+err.Error())
	}
	s.mu.Lock()
	s.watching = true
	s.mu.Unlock()
	s.setWatch(true, "", state)

	db := &debounce{armed: make(chan struct{}, 1)}
	go s.watchLoop(ctx, w, watched, state, db)
	return nil
}

var errTwiceWatch = &ErrRefused{Field: "watch", Detail: "the watch is already running"}

func (s *Service) watchPath(w *fsnotify.Watcher, dir string, watched map[string]bool, state *watchState) error {
	if _, err := os.Stat(dir); err != nil {
		state.errs = append(state.errs, dir+" is not there to watch")
		return err
	}
	if err := w.Add(dir); err != nil {
		state.errs = append(state.errs, dir+" could not be watched: "+err.Error())
		return err
	}
	watched[dir] = true
	return nil
}

func skipWatchName(name string) bool {
	// Dot entries are ours (staging, trash) or the OS's (DS_Store); the
	// installer's own staging renames must not chase the daemon in a
	// refresh loop while it is mid-write.
	return strings.HasPrefix(name, ".")
}

func rootPaths(roots []Root) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		out = append(out, r.Dir)
	}
	return out
}

func (s *Service) setWatch(enabled bool, reason string, state *watchState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.watchEnabled, s.watchReason = enabled, reason
	if state != nil {
		s.watchErrs = append([]string(nil), state.errs...)
	} else {
		s.watchErrs = nil
	}
}

func (s *Service) watchLoop(ctx context.Context, w *fsnotify.Watcher, watched map[string]bool, state *watchState, db *debounce) {
	defer w.Close()
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	armed := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(s.debounce)
	}
	for {
		select {
		case <-ctx.Done():
			s.setWatch(false, "the watch was stopped", state)
			return
		case <-timer.C:
			s.refreshOnWatch(state)
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			name := filepath.Base(ev.Name)
			if skipWatchName(name) {
				continue
			}
			// A folder that appeared under a root gets watched too
			// (the skill folder itself is one level under a root).
			if ev.Op.Has(fsnotify.Create) {
				if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
					_ = s.watchPath(w, ev.Name, watched, state)
				}
			}
			if ev.Op.Has(fsnotify.Remove) || ev.Op.Has(fsnotify.Rename) {
				delete(watched, ev.Name)
			}
			if ev.Op.Has(fsnotify.Create) || ev.Op.Has(fsnotify.Write) || ev.Op.Has(fsnotify.Remove) || ev.Op.Has(fsnotify.Rename) {
				armed()
			}
		case <-w.Errors:
			// A lost watch is spoken, not swallowed: record it and keep
			// serving from the last scan.
			state.errs = append(state.errs, "the watcher reported an error; the catalog answers from the last scan until the next change event")
			s.setWatch(true, "", state)
		}
	}
}

// refreshOnWatch re-derives and reports the diff. No diff ⇒ no event:
// fsnotify fires for touches that change nothing (atime-ish writes, our
// own reads' side effects on some systems), and an event stream of
// nothings trains everyone to ignore it.
func (s *Service) refreshOnWatch(state *watchState) {
	s.mu.Lock()
	old := s.cache
	s.mu.Unlock()
	fresh, err := s.Scan(true)
	if err != nil {
		state.errs = append(state.errs, "the change-triggered scan failed: "+err.Error())
		s.setWatch(true, "", state)
		return
	}
	diff := diffScans(old, fresh)
	if len(diff.Added) == 0 && len(diff.Removed) == 0 && len(diff.Changed) == 0 {
		return
	}
	diff.Watching, diff.WatchError = s.watchSnapshot()
	if s.onWatchChange != nil {
		s.onWatchChange(diff)
	}
}

// watchSnapshot reads the watch fields under the same mutex the cache
// carries them on (they travel together).
func (s *Service) watchSnapshot() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := watchState{enabled: s.watchEnabled, reason: s.watchReason, errs: s.watchErrs}
	return st.enabled, st.watchError()
}

// diffScans answers Added/Removed/Changed between two scans. "Changed"
// means a listed skill's judged shape moved — status, size or file
// counts — not a mere re-scan. A nil old scan (first refresh) is the
// quiet case: boot already lists the world, so announcing every skill as
// "added" would be noise dressed as news.
func diffScans(old, fresh *ScanResult) ChangeEvent {
	if old == nil {
		return ChangeEvent{}
	}
	oldByName := map[string]*Skill{}
	for _, sk := range old.Skills {
		oldByName[sk.Name+":"+string(sk.Source)] = sk
	}
	newByName := map[string]*Skill{}
	ev := ChangeEvent{}
	for _, sk := range fresh.Skills {
		key := sk.Name + ":" + string(sk.Source)
		newByName[key] = sk
		prev, had := oldByName[key]
		if !had {
			ev.Added = append(ev.Added, sk.Name)
			continue
		}
		if prev.Status != sk.Status || prev.TotalBytes != sk.TotalBytes ||
			prev.Files != sk.Files || prev.Shadowed != sk.Shadowed {
			ev.Changed = append(ev.Changed, sk.Name)
		}
	}
	for key, sk := range oldByName {
		if _, still := newByName[key]; !still {
			ev.Removed = append(ev.Removed, sk.Name)
		}
	}
	sort.Strings(ev.Added)
	sort.Strings(ev.Removed)
	sort.Strings(ev.Changed)
	return ev
}

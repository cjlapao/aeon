package daemon

// reindex_q7_test.go — S71 T4: the Q7 exception is a NAMED amendment, not a
// silent relaxation. The mtime-gate is a `stat` on an explicit status read;
// Q7 (N2) forbids a watcher/poll/second observer of the tree, so the
// amendment must say so in words — the exception is the stat on an
// *asked-for* read, not a continuous observer. The trigger set is unchanged
// (only the explicit door and the loop's own write-hook; the auto-fire is
// asserted by TestRetrievalReindexSocketNeverAutoFires).

import (
	"os"
	"strings"
	"testing"
)

func TestQ7ExceptionIsNamed(t *testing.T) {
	raw, err := os.ReadFile("reindex.go")
	if err != nil {
		t.Fatalf("read reindex.go: %v", err)
	}
	s := string(raw)
	// The amendment names the exception in words — not a silent relaxation.
	for _, want := range []string{
		"named exception to Q7",
		"watcher, not polling",
		"on-demand",
		"it costs no", // embeddings (V10)
		"starts no pass", // V21
		"hash-confirm", // the hash, not the stat, decides staleness
	} {
		if !strings.Contains(s, want) {
			t.Errorf("reindex.go's Q7 amendment must name %q — the exception is stated, not silently relaxed", want)
		}
	}
	// The exception is a `stat` on an asked-for read, not a watcher: the N2
	// ban that forbids watching the tree still holds in this file.
	banned := []string{"fsnotify", "inotify", "time.NewTicker", "time.Tick(", "NotifyOn", "StartWatcher"}
	for _, tok := range banned {
		if strings.Contains(s, tok) {
			t.Errorf("reindex.go contains %q — the exception is a stat, not a watcher", tok)
		}
	}
}

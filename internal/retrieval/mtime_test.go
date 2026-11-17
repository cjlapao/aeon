//go:build !noretrieval

package retrieval

// mtime_test.go — S71 T2: the cheap gate. The gate (internal/retrieval/
// code.go's reconcileCode) reads a source's recorded mtime before it reads
// the file; a matching mtime is ASSUMED fresh and not re-read, a moved or
// absent record is a candidate and is hash-confirmed as before. These are
// the behavioural proof: a held record makes a content change invisible
// (the read was skipped — the read count is zero), a moved mtime forces a
// re-read, and a touch that moves mtime without changing content is caught
// by the hash and not flagged, with its record refreshed (the confirm, not
// the gate, decides staleness).

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// setMtime pins a file's atime and mtime — the touch lever (move mtime,
// hold content). syscall.Utimes is microsecond-grained; the gate compares
// at the mtime the filesystem actually returns, so the touch reads as a
// move regardless of the sub-microsecond remainder.
func setMtime(t *testing.T, path string, when int64) {
	t.Helper()
	tv := []syscall.Timeval{{Sec: when, Usec: 0}, {Sec: when, Usec: 0}}
	if err := syscall.Utimes(path, tv); err != nil {
		t.Fatalf("set mtime %s: %v", path, err)
	}
}

// setRecordMtime writes the gate's memory for one source — the lever the
// held-mtime case uses: pin the record to the file's CURRENT mtime so the
// gate sees a match and skips, whatever the content on disk says.
func setRecordMtime(t *testing.T, x *Index, root, corpus, rel string, when int64) {
	t.Helper()
	row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if !ok || err != nil {
		t.Fatalf("corpus row %q/%s: ok=%v err=%v", root, corpus, ok, err)
	}
	if err := x.s.UpsertSourceMtime(row.ID, rel, when); err != nil {
		t.Fatalf("set record %q: %v", rel, err)
	}
}

func recordFor(t *testing.T, x *Index, root, corpus, rel string) int64 {
	t.Helper()
	row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
	if !ok || err != nil {
		t.Fatalf("corpus row %q/%s: ok=%v err=%v", root, corpus, ok, err)
	}
	m, found, err := x.s.SourceMtime(row.ID, rel)
	if !found || err != nil {
		t.Fatalf("source mtime %q: found=%v err=%v", rel, found, err)
	}
	return m
}

func TestMtimeGateSkipsTheUnchanged(t *testing.T) {
	// retry.go: content is changed but its RECORD is pinned to the file's
	// current mtime — the gate sees a match and does not re-read, so the
	// change is invisible (the read count for it is zero). escalate.go:
	// content is changed and its mtime moves past the record — the gate
	// re-reads and detects it (stale).
	x, root, _ := seedBuilt(t)

	must(t, os.WriteFile(filepath.Join(root, "retry.go"), []byte("package main\n\nfunc retry() int {\n\treturn 7\n}\n"), 0o644), "edit retry")
	retryInfo, err := os.Stat(filepath.Join(root, "retry.go"))
	must(t, err, "stat retry")
	setRecordMtime(t, x, root, CorpusCode, "retry.go", retryInfo.ModTime().UnixNano())

	must(t, os.WriteFile(filepath.Join(root, "escalate.go"), []byte("package main\n\nfunc escalate() int {\n\treturn 11\n}\n"), 0o644), "edit escalate")
	escalateNow, err := os.Stat(filepath.Join(root, "escalate.go"))
	must(t, err, "stat escalate")
	// escalate's record is left at the index-time value; its on-disk mtime
	// has moved past it, so the gate must re-read.
	if escalateNow.ModTime().UnixNano() <= recordFor(t, x, root, CorpusCode, "escalate.go") {
		t.Fatalf("escalate's on-disk mtime must move past its record, got %d vs record %d", escalateNow.ModTime().UnixNano(), recordFor(t, x, root, CorpusCode, "escalate.go"))
	}

	f, err := x.Staleness(context.Background(), root, CorpusCode)
	must(t, err, "staleness")
	// Both files are walked; exactly one (escalate, mtime moved) is stale.
	// If the gate had NOT skipped retry.go, its content change would read as
	// stale too and the count would be 2 — so the count of 1 IS the read
	// count of zero for the held-record file.
	if f.Files != 2 {
		t.Fatalf("both files must be walked, got Files=%d", f.Files)
	}
	if f.StaleFiles != 1 {
		t.Fatalf("only the moved-mtime file may read stale, got StaleFiles=%d (the held-record file was re-read)", f.StaleFiles)
	}
	// The gate did not re-read retry.go, so it wrote nothing: its record is
	// still the value the test pinned, not a refreshed one.
	if got := recordFor(t, x, root, CorpusCode, "retry.go"); got != retryInfo.ModTime().UnixNano() {
		t.Errorf("a skipped (fresh-by-mtime) source must not write through, got %d want %d", got, retryInfo.ModTime().UnixNano())
	}
}

func TestMtimeLieDoesNotOverReport(t *testing.T) {
	// A touch moves mtime without changing content. The gate re-reads (the
	// mtime moved), the hash CONFIRMS the content is unchanged, the file is
	// not flagged, and its record is refreshed to the touch's mtime — so the
	// next read gates it and the file is not re-read again. This is the
	// confirm-not-the-gate rule: a moved mtime is never itself staleness.
	x, root, _ := seedBuilt(t)
	before := recordFor(t, x, root, CorpusCode, "retry.go")
	touch := before + 100_000_000_000 // 100s later, in nanoseconds
	setMtime(t, filepath.Join(root, "retry.go"), touch/1_000_000_000)

	f, err := x.Staleness(context.Background(), root, CorpusCode)
	must(t, err, "staleness")
	if f.StaleFiles != 0 {
		t.Fatalf("a touch that moves mtime but not content must not read stale, got StaleFiles=%d", f.StaleFiles)
	}
	// The confirm refreshed the record to the touch's mtime — the next read
	// gates it as fresh rather than re-reading the unchanged content. The
	// filesystem may keep the value microsecond-grained; assert it moved.
	after := recordFor(t, x, root, CorpusCode, "retry.go")
	if after <= before {
		t.Fatalf("a confirmed-fresh file must refresh its mtime record, got %d (was %d)", after, before)
	}
}

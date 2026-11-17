//go:build !noretrieval

package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// code_test.go — the walk's discipline (V8): incremental, .git-skipping,
// symlink-refusing, and above all LOUD about absences — the same rule
// S11's glob and grep follow: "an unexplained absence reads as no
// matches".

func TestSkipsAreReported(t *testing.T) {
	x, ensure := embedIndex(t)
	_ = ensure
	root := ensure2Root(t, x)

	must(t, os.WriteFile(filepath.Join(root, "keep.go"), []byte("package main\n\nfunc keep() int {\n\treturn 1\n}\n"), 0o644), "seed keep")
	// .git: the one skipped tree.
	must(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755), "mkdir .git")
	must(t, os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[core]\n"), 0o644), "seed .git")
	// A too-large file, named by the walk's OWN rule.
	big := make([]byte, maxIndexFileBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	must(t, os.WriteFile(filepath.Join(root, "bundle.js"), big, 0o644), "seed bundle")
	// A binary file (NUL in the first 8 KiB) — skipped by the chunker, and
	// the skip must ride through the walk's report.
	must(t, os.WriteFile(filepath.Join(root, "blob.bin"), append([]byte{0x00, 0x01}, []byte("binary")...), 0o644), "seed blob")
	// A symlink: never followed, never indexed, named.
	must(t, os.Symlink(filepath.Join(root, "keep.go"), filepath.Join(root, "link.go")), "seed symlink")
	// An unreadable directory: skipped, counted, NAMED — never a crash.
	must(t, os.MkdirAll(filepath.Join(root, "locked"), 0o755), "mkdir locked")
	must(t, os.WriteFile(filepath.Join(root, "locked", "secret.go"), []byte("package main\n"), 0o644), "seed locked")
	must(t, os.Chmod(filepath.Join(root, "locked"), 0o000), "chmod locked")
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "locked"), 0o755) })

	res, err := x.IndexTree(context.Background(), root, CorpusCode)
	must(t, err, "index tree with skips")

	joined := ""
	for _, s := range res.Skips {
		if s.Path == "" || s.Reason == "" {
			t.Fatalf("an anonymous skip %+v — every absence gets a name and a reason", s)
		}
		joined += s.Path + "|" + s.Reason + "\n"
	}
	for _, want := range []string{"bundle.js", "blob.bin", "link.go", "locked", ".git"} {
		if want == ".git" {
			// .git is skipped SILENTLY-BY-DESIGN like glob's walker (it is
			// not a candidate answer anywhere); the S11 rule that binds is
			// that FILES that looked indexable and were refused are named.
			if strings.Contains(joined, ".git/config") {
				t.Error(".git contents were indexed — the tree is skipped")
			}
			continue
		}
		if !strings.Contains(joined, want) {
			t.Errorf("%s is absent from the skips without being named:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "keep.go") {
		t.Error("keep.go was skipped — it is the one file that should have indexed")
	}
	if res.Written == 0 {
		t.Fatal("nothing was written — the walk skipped even the clean file")
	}
}

func TestIndexTreeIsIncrementalAndPrunesVanishedFiles(t *testing.T) {
	x, ensure := embedIndex(t)
	_ = ensure
	root := ensure2Root(t, x)
	path := filepath.Join(root, "a.go")
	body := "package main\n\nfunc one() int {\n\treturn 1\n}\n"
	must(t, os.WriteFile(path, []byte(body), 0o644), "seed a")

	first, err := x.IndexTree(context.Background(), root, CorpusCode)
	must(t, err, "first walk")
	if first.Written == 0 || first.Deleted != 0 {
		t.Fatalf("first walk wrote %d deleted %d", first.Written, first.Deleted)
	}

	// Quiet tree: the same walk re-plans with NO writes.
	second, err := x.IndexTree(context.Background(), root, CorpusCode)
	must(t, err, "second walk")
	if second.Written != 0 {
		t.Fatalf("an unchanged tree wrote %d chunks — V8's incrementality is the whole point", second.Written)
	}
	if second.Unchanged == 0 {
		t.Fatal("the unchanged count is zero — the walk did not even look")
	}
	if second.Indexed != 1 {
		t.Fatalf("fully-unchanged files: %d, want 1", second.Indexed)
	}

	// A vanished file is PRUNED and its absence is NAMED (not silently
	// absent).
	must(t, os.Remove(path), "remove a")
	third, err := x.IndexTree(context.Background(), root, CorpusCode)
	must(t, err, "third walk")
	if third.Deleted == 0 {
		t.Fatal("the vanished file still holds chunks — the walk owes the prune")
	}
	found := false
	for _, s := range third.Skips {
		if strings.Contains(s.Reason, "vanished") && strings.Contains(s.Path, "a.go") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the prune was silent: %+v", third.Skips)
	}
}

func TestIndexTreeRefusesWhatItMustNotTouch(t *testing.T) {
	x, ensure := embedIndex(t)
	_ = ensure
	_, err := x.IndexTree(context.Background(), "", CorpusCode)
	if err == nil || !strings.Contains(err.Error(), "per project root") {
		t.Fatalf("empty root: %v", err)
	}
	_, err = x.IndexTree(context.Background(), "/no/such/root-at-all", CorpusCode)
	if err == nil {
		t.Fatal("a nonexistent root was accepted")
	}
	root := ensure2Root(t, x)
	_, err = x.IndexTree(context.Background(), root, "memories")
	if err == nil || !strings.Contains(err.Error(), `"memories"`) {
		t.Fatalf("invented corpus: %v — the refusal names the value", err)
	}
	// An unensured corpus refuses the walk: the walk indexes, it does not
	// decide.
	fresh := t.TempDir()
	must(t, os.WriteFile(filepath.Join(fresh, "x.go"), []byte("package main\n"), 0o644), "seed")
	if _, err := x.IndexTree(context.Background(), fresh, CorpusCode); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unensured corpus walk: %v", err)
	}
}

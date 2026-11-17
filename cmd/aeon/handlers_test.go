package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerifyExitCodeCLI is CI's contract on the real verb: 1 on refusals,
// 0 on a clean sweep, 2 on usage — no output parsing required to gate.
func TestVerifyExitCodeCLI(t *testing.T) {
	orig := originalsDirForTest(t)
	var out, errb bytes.Buffer
	if code := cmdHandlers(&out, &errb, []string{"verify", "--file", orig, "--original"}); code != 1 {
		t.Fatalf("verify on an original must exit 1 (it must FAIL — D1); got %d, output:\n%s", code, out.String())
	}
	s := out.String()
	for _, want := range []string{"loop_get", "memory_append", "event_append", "FINAL REPORT", "dev-loop-protocol"} {
		if !strings.Contains(s, want) {
			t.Errorf("the CLI output must name %q; got:\n%s", want, s)
		}
	}
	// Clean body → exit 0.
	dir := t.TempDir()
	clean := filepath.Join(dir, "quiet.md")
	os.WriteFile(clean, []byte("---\nname: quiet\ndescription: a reviewed body with no residue at all.\ntemperature: 0.3\ntools: [read, write]\n---\nRead the failing test, implement the smallest change, run the package suite.\n"), 0o644)
	out.Reset()
	if code := cmdHandlers(&out, &errb, []string{"verify", "--file", clean}); code != 0 {
		t.Fatalf("a clean rewrite must exit 0; got %d:\n%s", code, out.String())
	}
	// Usage → exit 2, diagnostic on stderr.
	out.Reset()
	errb.Reset()
	if code := cmdHandlers(&out, &errb, []string{"nonsense"}); code != 2 || errb.Len() == 0 {
		t.Fatalf("usage must exit 2 with a stderr diagnostic; got %d, stderr %q", code, errb.String())
	}
	// handlers list prints the registered names, one per line (DEMO §6 counts them).
	out.Reset()
	if code := cmdHandlers(&out, &errb, []string{"list"}); code != 0 || !strings.Contains(out.String(), "default\n") {
		t.Fatalf("list broken: %d %q", code, out.String())
	}
}

func originalsDirForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, "opencode", "agents", "golang-engineer-loop.md")
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("opencode/agents/golang-engineer-loop.md not found walking up")
	return ""
}

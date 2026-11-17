package daemon

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func testBuildTags() string {
	if tags := os.Getenv("AEON_GO_TAGS"); tags != "" {
		return tags
	}
	return "sqlite_fts5"
}

// — row 10's CLI —

// buildCLI builds the one binary once, in the test's temp dir, from the
// module's own sources. The cgo preamble (CGO_ENABLED, the shim's include
// path, GOPRIVATE) rides the environment the walk's own test run was
// started with; the build is incremental over the test's own compile.
func (w *walkEnv) buildCLI(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	aeonDir := filepath.Join(wd, "..", "..")
	bin := filepath.Join(t.TempDir(), "aeon")
	cmd := exec.Command("go", "build", "-tags", testBuildTags(), "-o", bin, "./cmd/aeon")
	cmd.Dir = aeonDir
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build ./cmd/aeon: %v\n%s", err, errb.String())
	}
	return bin
}

// runCLI runs one CLI verb and returns stdout, stderr and the exit error
// (nil only on exit 0 — the refusal rows assert the non-nil).
func runCLI(t *testing.T, bin string, env []string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

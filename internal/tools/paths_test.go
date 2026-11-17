package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// T5's tests: one path resolver, and every refusal with a case.
//
// # The standing constraint for this whole package
//
// No test runs a command outside a temp directory, and none targets a public
// URL. Every workspace here is `t.TempDir()`, and every escape attempt points
// at a sibling temp directory rather than at anything real.

// workspace builds a temp workspace with some files, and returns the root.
func workspace(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Resolved, because `t.TempDir()` can sit under a symlink (`/tmp` is one
	// on macOS) and a test comparing an unresolved root against a resolved
	// result fails for a reason that has nothing to do with the code.
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func TestResolveAcceptsAnOrdinaryRelativePath(t *testing.T) {
	root := workspace(t, map[string]string{"docs/a.md": "hello"})

	res := Resolve(root, "docs/a.md")
	if !res.OK {
		t.Fatalf("refused: %s — %s", res.Refusal, res.Reason)
	}
	if res.Abs != filepath.Join(root, "docs/a.md") {
		t.Errorf("abs is %q", res.Abs)
	}
	if res.Rel != "docs/a.md" {
		t.Errorf("rel is %q", res.Rel)
	}
}

func TestResolveAcceptsAPathThatDoesNotExistYet(t *testing.T) {
	// There is no write tool at S5, but this resolver is the one a write tool
	// will use — and "resolve the nearest existing ancestor" is what catches a
	// write to a NEW file through a symlinked directory. A resolver that
	// refused missing paths would push that check into S12.
	root := workspace(t, nil)
	res := Resolve(root, "not/created/yet.md")
	if !res.OK {
		t.Fatalf("a missing path was refused: %s — %s", res.Refusal, res.Reason)
	}
	if res.Rel != "not/created/yet.md" {
		t.Errorf("rel is %q", res.Rel)
	}
}

func TestEveryRefusalHasACaseAndNamesTheOffendingPath(t *testing.T) {
	root := workspace(t, map[string]string{"a.md": "x"})

	cases := []struct {
		name string
		rel  string
		want Refusal
	}{
		{"empty", "", RefusalEmpty},
		{"whitespace only", "   ", RefusalEmpty},
		{"an absolute unix path", "/etc/passwd", RefusalAbsolute},
		{"a bare traversal", "../outside", RefusalTraversal},
		{"a traversal in the middle", "docs/../../outside", RefusalTraversal},
		{"a traversal that cleans away", "a/../a.md", RefusalTraversal},
		{"a NUL byte", "a\x00b", RefusalTraversal},
		{"a backslash traversal", `docs\..\..\outside`, RefusalTraversal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Resolve(root, tc.rel)
			if res.OK {
				t.Fatalf("%q was accepted as %s", tc.rel, res.Abs)
			}
			if res.Refusal != tc.want {
				t.Errorf("refusal %q, want %q (%s)", res.Refusal, tc.want, res.Reason)
			}
			if res.Reason == "" {
				t.Error("no reason")
			}
		})
	}

	// Every declared refusal is reachable. A constant nothing produces is a
	// refusal that was renamed and left behind.
	produced := map[Refusal]bool{}
	for _, tc := range cases {
		produced[Resolve(root, tc.rel).Refusal] = true
	}
	for _, r := range []Refusal{RefusalEmpty, RefusalAbsolute, RefusalTraversal} {
		if !produced[r] {
			t.Errorf("no case produces %q", r)
		}
	}
}

func TestATraversalIsRefusedInTheInputRatherThanResolvedAway(t *testing.T) {
	// `a/../a.md` is a path INSIDE the workspace once cleaned. It is still
	// refused, because the check runs on what the model sent: a cleaned path
	// has had its escape resolved away, and `a/../../b` becomes `../b` — so
	// the check would be about a different string than the one that arrived.
	root := workspace(t, map[string]string{"a.md": "x"})

	res := Resolve(root, "a/../a.md")
	if res.OK {
		t.Fatal("a `..` segment that cleans back inside was accepted")
	}
	if !strings.Contains(res.Reason, "refused rather than resolved") {
		t.Errorf("the reason does not explain why: %q", res.Reason)
	}
}

func TestASymlinkPointingOutOfTheWorkspaceIsRefusedAfterResolution(t *testing.T) {
	// The case the whole resolver exists for. Before resolution the path looks
	// like an ordinary one inside the workspace: `secrets.txt`, no `..`, not
	// absolute. Only after `EvalSymlinks` is it somewhere else.
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("not yours"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := workspace(t, nil)
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "secrets.txt")); err != nil {
		t.Fatal(err)
	}

	res := Resolve(root, "secrets.txt")
	if res.OK {
		t.Fatalf("a symlink out of the workspace resolved to %s", res.Abs)
	}
	if res.Refusal != RefusalEscape {
		t.Errorf("refusal %q, want %q", res.Refusal, RefusalEscape)
	}
	if !strings.Contains(res.Reason, "after resolution") {
		t.Errorf("the reason does not say when the check runs: %q", res.Reason)
	}
}

func TestASymlinkedDirectoryPointingOutIsRefusedForAFileThatDoesNotExist(t *testing.T) {
	// The reason `resolveExistingPrefix` walks UP. `EvalSymlinks` is
	// all-or-nothing: given a missing leaf it reports IsNotExist and resolves
	// nothing, so a resolver that gave up there would accept
	// `escape/new-file.txt` — a write through a symlinked directory.
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}

	outside := t.TempDir()
	root := workspace(t, nil)
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	res := Resolve(root, "escape/new-file.txt")
	if res.OK {
		t.Fatalf("a new file through a symlinked directory resolved to %s", res.Abs)
	}
	if res.Refusal != RefusalEscape {
		t.Errorf("refusal %q, want %q (%s)", res.Refusal, RefusalEscape, res.Reason)
	}
}

func TestASymlinkInsideTheWorkspaceIsAccepted(t *testing.T) {
	// The negative control on the escape check. A symlink is not suspicious;
	// a symlink pointing OUT is. A resolver that refused all of them would
	// break every repository with a linked config.
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}

	root := workspace(t, map[string]string{"real/a.md": "x"})
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	res := Resolve(root, "link/a.md")
	if !res.OK {
		t.Fatalf("a symlink inside the workspace was refused: %s — %s", res.Refusal, res.Reason)
	}
}

func TestASiblingDirectoryWithThePrefixIsNotInsideTheWorkspace(t *testing.T) {
	// "the oldest bug in this family and it is one character of difference":
	// a string-prefix comparison says `/workspace-evil` is inside
	// `/workspace`. `filepath.Rel` says it is not.
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}

	parent := t.TempDir()
	real, err := filepath.EvalSymlinks(parent)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(real, "workspace")
	evil := filepath.Join(real, "workspace-evil")
	for _, dir := range []string{root, evil} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(evil, "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A link inside the workspace to the LOOK-ALIKE sibling.
	if err := os.Symlink(evil, filepath.Join(root, "sibling")); err != nil {
		t.Fatal(err)
	}

	res := Resolve(root, "sibling/a.md")
	if res.OK {
		t.Fatalf("the look-alike sibling %s was treated as inside %s", res.Abs, root)
	}

	// And `within` itself, directly, because this is the function that is
	// easy to "simplify" into a prefix check.
	if within(root, evil) {
		t.Errorf("within(%q, %q) is true", root, evil)
	}
	if !within(root, filepath.Join(root, "a", "b")) {
		t.Error("a real child is reported outside")
	}
	if !within(root, root) {
		t.Error("the root is reported outside itself")
	}
}

func TestTheRootIsResolvedToo(t *testing.T) {
	// Found by a failing test on macOS, where `/tmp` IS a symlink: resolving
	// only the candidate makes every path look like an escape, because the
	// resolved candidate sits under `/private/var` and the unresolved root
	// under `/tmp`.
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}

	real := workspace(t, map[string]string{"a.md": "x"})
	linkParent := t.TempDir()
	linked := filepath.Join(linkParent, "via-link")
	if err := os.Symlink(real, linked); err != nil {
		t.Fatal(err)
	}

	res := Resolve(linked, "a.md")
	if !res.OK {
		t.Fatalf("a symlinked ROOT made an inside path look like an escape: %s — %s",
			res.Refusal, res.Reason)
	}
}

func TestARelativeRootPanicsRatherThanBeingRefused(t *testing.T) {
	// A programmer error. The root comes from `StepRequest.WorkspaceRoot`,
	// which the protocol handler validated — so reaching here with a relative
	// one means a caller skipped construction, and a refusal would report that
	// as the model's bad path.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("a relative root did not panic")
		}
		if !strings.Contains(toString(r), "not absolute") {
			t.Errorf("the panic does not say what is wrong: %v", r)
		}
	}()
	Resolve("relative/root", "a.md")
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}

func TestThereIsExactlyOnePathResolver(t *testing.T) {
	// W2/I1. Two resolvers means two sets of refusals, and the second one is
	// always the weaker — it gets written by somebody who needed one case to
	// work.
	//
	// So: no file in this package calls `filepath.Join` on the workspace root
	// except the resolver, and no tool calls `os.Open`/`os.ReadFile` on
	// anything but an already-resolved absolute path.
	sources := packageSources(t)

	// S22 T6, recorded exception: the search tool's root is a KEY — the
	// identifier of the corpus rows the resolver is asked about — never a
	// directory this package opens. The resolver is a function value living
	// beyond the module boundary; the tools side touches no filesystem at
	// all, which the explicit ban below keeps true. The guard keeps its
	// teeth by naming the absence, not by granting a general pardon.
	if src, ok := sources["search.go"]; ok {
		for _, banned := range []string{"os.Open", "os.ReadFile", "os.Getenv", "os.Getwd", "filepath.", "exec."} {
			if strings.Contains(src, banned) {
				t.Errorf("search.go uses %q — the search tool's root is a row key, not a path it opens (S22 T6)", banned)
			}
		}
	}

	// S30 T5, the same recorded exception search.go holds: the graph
	// tool's root is a ROW KEY — the identifier the graph rows hang off
	// (T5 §5's required project_root) — never a directory this package
	// opens. Parsing happens beyond the module boundary, in
	// internal/graph; the tools side touches no filesystem at all, which
	// the explicit ban below keeps true.
	if src, ok := sources["graph.go"]; ok {
		for _, banned := range []string{"os.Open", "os.ReadFile", "os.Getenv", "os.Getwd", "filepath.", "exec."} {
			if strings.Contains(src, banned) {
				t.Errorf("graph.go uses %q — the graph tool's root is a row key, not a path it opens (S30 T5)", banned)
			}
		}
	}

	for name, src := range sources {
		if name == "paths.go" || name == "search.go" || name == "graph.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if strings.Contains(src, "WorkspaceRoot,") && !strings.Contains(src, "Resolve(tc.WorkspaceRoot") {
			t.Errorf("%s uses the workspace root without going through Resolve", name)
		}
		if strings.Contains(src, "filepath.Join(tc.WorkspaceRoot") {
			t.Errorf("%s joins onto the workspace root itself rather than calling Resolve", name)
		}
	}

	// And the resolver is called by name from the tool that needs it, so the
	// check above is checking something.
	if !strings.Contains(sources["read.go"], "Resolve(") {
		t.Error("read.go does not call the resolver")
	}
}

func TestWindowsDriveLettersAreRefusedEverywhere(t *testing.T) {
	// Refused on every platform, not only on Windows. A model trained on
	// Windows paths sends `C:\Users\...`, and on Linux that is a RELATIVE path
	// whose first segment happens to contain a colon — so it would resolve to
	// `<root>/C:/Users/...` and silently read nothing, or worse, read
	// something somebody created at that name.
	root := workspace(t, nil)
	for _, rel := range []string{`C:\Windows\System32`, "C:/Windows", `D:\data`} {
		res := Resolve(root, rel)
		if res.OK {
			t.Errorf("%q was accepted as %s", rel, res.Abs)
		}
		if res.Refusal != RefusalAbsolute {
			t.Errorf("%q gave refusal %q, want %q", rel, res.Refusal, RefusalAbsolute)
		}
	}
}

func TestALeadingDotSlashIsOrdinary(t *testing.T) {
	root := workspace(t, map[string]string{"a.md": "x"})
	res := Resolve(root, "./a.md")
	if !res.OK {
		t.Fatalf("refused: %s — %s", res.Refusal, res.Reason)
	}
	if res.Rel != "a.md" {
		t.Errorf("rel is %q, want the cleaned form", res.Rel)
	}
}

package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// # The test ground rule (AGENTS §5, brief §6 T6)
//
// Everything here builds its repositories in t.TempDir() and talks to no real
// repository, no remote forge, no real store. The remote for ahead/behind is a
// LOCAL bare repo. Tests set user.email/user.name locally on the repo they
// created — never globally.

// testGitEnv isolates git from anything on the machine: no system config, no
// user config, no prompts. The test asserts facts about git's machine-readable
// output, not about this box's gitconfig.
func testGitEnv(t *testing.T) []string {
	t.Helper()
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"SYSTEMROOT=" + os.Getenv("SYSTEMROOT"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + filepath.Join(t.TempDir(), "no-such-config"),
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"HOME=" + t.TempDir(),
	}
}

// git runs a plain setup command — the TEST's hands, not the code under test.
// The product path goes through the bounded layer in git.go; this is exec with
// an isolated environment and a plain context.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testGitEnv(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// repo builds a fresh repository with a deterministic identity and branch.
// `init -b main` needs git ≥ 2.28; the measured git here is 2.53.0 (the
// version parsed against, recorded in tasks/T6.md Notes).
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main", ".")
	git(t, dir, "config", "user.email", "aeon-test@localhost")
	git(t, dir, "config", "user.name", "Aeon Test")
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func strptr(s string) *string { return &s }
func intptr(n int) *int       { return &n }

// The first contract: a clean repository answers `ok` with its branch, no
// upstream (ABSENT, not zero — P31: no upstream is not "0 ahead of nothing"),
// and no changed paths.
func TestGitStatusCleanRepo(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "readme.md", "# hello\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")

	got := GitStatus(context.Background(), dir)

	if got.State != StateOK {
		t.Fatalf("state = %q (reason %q), want %q", got.State, got.Reason, StateOK)
	}
	if got.Branch == nil || *got.Branch != "main" {
		t.Errorf("branch = %v, want main", got.Branch)
	}
	if got.Detached {
		t.Error("detached = true on a branch")
	}
	if got.Upstream != nil {
		t.Errorf("upstream = %v on a branch without one; absent is not a claim of zero", got.Upstream)
	}
	if got.Ahead != nil || got.Behind != nil {
		t.Errorf("ahead/behind = %v/%v without an upstream; they must be absent, not 0",
			got.Ahead, got.Behind)
	}
	if len(got.Changed) != 0 {
		t.Errorf("changed = %+v on a clean repo", got.Changed)
	}
}

// findChanged asserts one changed entry exists with EXACTLY these fields.
func findChanged(t *testing.T, got []ChangedFile, want ChangedFile) {
	t.Helper()
	for _, c := range got {
		if c == want {
			return
		}
	}
	t.Errorf("no changed entry %+v in %+v", want, got)
}

// gitAllowErr runs a setup command whose non-zero exit is the expected case
// (the conflicting `git merge`, for instance) and returns its output.
func gitAllowErr(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testGitEnv(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The five working-set states in one listing: modified (worktree), deleted
// (staged), added (staged), renamed (staged, origPath carried), untracked —
// plus a path with spaces, which `-z` makes ordinary rather than quoted.
func TestGitStatusWorkingSet(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "tracked.txt", "one\n")
	writeFile(t, dir, "removed.txt", "gone\n")
	writeFile(t, dir, "orig.txt", "stable\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")

	writeFile(t, dir, "tracked.txt", "one modified\n") // unstaged modify → .M
	git(t, dir, "rm", "-q", "removed.txt")             // staged delete → D.
	writeFile(t, dir, "a b.txt", "brand new\n")        //
	git(t, dir, "add", "a b.txt")                      // staged add, spaces → A.
	git(t, dir, "mv", "orig.txt", "renamed.txt")       // staged rename → R. + orig
	writeFile(t, dir, "untracked.txt", "not yet\n")

	got := GitStatus(context.Background(), dir)
	if got.State != StateOK {
		t.Fatalf("state = %q (reason %q), want ok", got.State, got.Reason)
	}
	findChanged(t, got.Changed, ChangedFile{Path: "tracked.txt", Index: ".", Worktree: "M"})
	findChanged(t, got.Changed, ChangedFile{Path: "removed.txt", Index: "D", Worktree: "."})
	findChanged(t, got.Changed, ChangedFile{Path: "a b.txt", Index: "A", Worktree: "."})
	findChanged(t, got.Changed, ChangedFile{Path: "renamed.txt", Index: "R", Worktree: ".", OrigPath: "orig.txt"})
	findChanged(t, got.Changed, ChangedFile{Path: "untracked.txt", Index: "?", Worktree: "U"})
	if len(got.Changed) != 5 {
		t.Errorf("changed has %d entries, want 5: %+v", len(got.Changed), got.Changed)
	}
}

// A merge conflict is listed, not dropped: the `u` record's XY codes pass
// through (UU for both sides).
func TestGitStatusUnmergedConflict(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "shared.txt", "base\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "base")
	git(t, dir, "checkout", "-qb", "feature")
	writeFile(t, dir, "shared.txt", "feature side\n")
	git(t, dir, "commit", "-qam", "feature")
	git(t, dir, "checkout", "-q", "main")
	writeFile(t, dir, "shared.txt", "main side\n")
	git(t, dir, "commit", "-qam", "main")

	if _, err := gitAllowErr(t, dir, "merge", "feature"); err == nil {
		t.Fatal("the merge was expected to conflict")
	}

	got := GitStatus(context.Background(), dir)
	if got.State != StateOK {
		t.Fatalf("state = %q (reason %q), want ok", got.State, got.Reason)
	}
	findChanged(t, got.Changed, ChangedFile{Path: "shared.txt", Index: "U", Worktree: "U"})
}

// Detached HEAD answers ok with Detached set and NO branch claim — the
// porcelain marker "(detached)" is git's state note, not a branch name
// (measured against git 2.53.0).
func TestGitStatusDetachedHead(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "a.txt", "1\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "first")
	writeFile(t, dir, "a.txt", "2\n")
	git(t, dir, "commit", "-qam", "second")
	sha := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD~1"))
	git(t, dir, "checkout", "-q", sha)

	got := GitStatus(context.Background(), dir)
	if got.State != StateOK {
		t.Fatalf("state = %q (reason %q), want ok", got.State, got.Reason)
	}
	if !got.Detached {
		t.Error("detached = false on a detached HEAD")
	}
	if got.Branch != nil {
		t.Errorf("branch = %v; the detached marker is not a branch name", *got.Branch)
	}
}

// Ahead/behind against a LOCAL bare remote — never a real forge. One commit
// pushed that the remote lacks (ahead) and one the remote gained elsewhere
// (behind), then `git fetch` so the comparison is against the stored upstream.
func TestGitStatusAheadBehind(t *testing.T) {
	dir := repo(t)
	bare := t.TempDir()
	git(t, bare, "init", "-q", "--bare", "-b", "main", ".")

	writeFile(t, dir, "a.txt", "1\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "initial")
	git(t, dir, "remote", "add", "origin", bare)
	git(t, dir, "push", "-q", "-u", "origin", "main")

	// A second checkout advances the remote by one commit.
	other := t.TempDir()
	git(t, other, "clone", "-q", bare, "clone")
	clone := filepath.Join(other, "clone")
	// A clone gets no identity from the origin's LOCAL config; set it locally,
	// never globally (AGENTS §5 — tests touch only their own repo).
	git(t, clone, "config", "user.email", "aeon-test@localhost")
	git(t, clone, "config", "user.name", "Aeon Test")
	writeFile(t, clone, "b.txt", "2\n")
	git(t, clone, "add", "-A")
	git(t, clone, "commit", "-qm", "remote ahead")
	git(t, clone, "push", "-q", "origin", "main")

	// And the work checkout gains one local commit the remote lacks.
	writeFile(t, dir, "c.txt", "3\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "local ahead")
	git(t, dir, "fetch", "-q", "origin")

	got := GitStatus(context.Background(), dir)
	if got.State != StateOK {
		t.Fatalf("state = %q (reason %q), want ok", got.State, got.Reason)
	}
	if got.Upstream == nil || *got.Upstream != "origin/main" {
		t.Fatalf("upstream = %v, want origin/main", got.Upstream)
	}
	if got.Ahead == nil || *got.Ahead != 1 {
		t.Errorf("ahead = %v, want 1", got.Ahead)
	}
	if got.Behind == nil || *got.Behind != 1 {
		t.Errorf("behind = %v, want 1", got.Behind)
	}
}

// A directory that is not a repository answers `not-a-repo`, with no fields
// filled — never an ok with an empty changed list (D7).
func TestGitStatusNotARepo(t *testing.T) {
	got := GitStatus(context.Background(), t.TempDir())
	if got.State != StateNotARepo {
		t.Fatalf("state = %q (reason %q), want not-a-repo", got.State, got.Reason)
	}
	if got.Reason != "" || got.Branch != nil || got.Upstream != nil || got.Ahead != nil ||
		got.Behind != nil || got.Changed != nil {
		t.Errorf("not-a-repo carries fields: %+v", got)
	}
}

// git absent from PATH answers `unavailable` naming the thing refused, on
// BOTH methods (the brief's wording: `unavailable: "git is not installed"`).
// The repositories are built BEFORE PATH is narrowed.
func TestGitMissingFromPATHNamesIt(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "a.txt", "1\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")

	t.Setenv("PATH", t.TempDir())

	st := GitStatus(context.Background(), dir)
	if st.State != StateUnavailable || !strings.Contains(st.Reason, "git is not installed") {
		t.Errorf("status = %+v, want unavailable naming \"git is not installed\"", st)
	}
	df := GitDiff(context.Background(), dir, "a.txt")
	if df.State != StateUnavailable || !strings.Contains(df.Reason, "git is not installed") {
		t.Errorf("diff = %+v, want unavailable naming \"git is not installed\"", df)
	}
}

// A git that never finishes is killed AS A GROUP, and the timeout names
// itself in the reason. The group claim gets spawn.go's kind of evidence —
// not "the function returned", but a fact about the CHILD: a fake `git` whose
// shell traps TERM and touches a marker. A parent-only kill (what
// exec.CommandContext does) never reaches the child, so the marker would
// never appear; the group TERM makes it appear. The test shortens the
// package's own timeout/grace variables (single-goroutine: no t.Parallel
// here) so it costs ~half a second, not five.
func TestGitStatusTimeoutKillsTheGroup(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "a.txt", "1\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")

	fakeDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "child-was-signalled")
	script := "#!/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; export PATH\n" +
		"trap 'touch " + marker + "; exit 143' TERM\n" +
		"sleep 30 &\nwait\n"
	if err := os.WriteFile(filepath.Join(fakeDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir)

	oldTimeout, oldGrace := commandTimeout, killGrace
	commandTimeout, killGrace = 300*time.Millisecond, 50*time.Millisecond
	defer func() { commandTimeout, killGrace = oldTimeout, oldGrace }()

	started := time.Now()
	got := GitStatus(context.Background(), dir)
	elapsed := time.Since(started)

	if got.State != StateUnavailable {
		t.Fatalf("state = %q, want unavailable", got.State)
	}
	if !strings.Contains(got.Reason, "300ms") || !strings.Contains(got.Reason, "killed") {
		t.Errorf("reason %q does not name the timeout and the kill", got.Reason)
	}
	if elapsed > 3*time.Second {
		t.Errorf("the call took %v; the timeout did not bound it", elapsed)
	}

	// Positive proof the CHILD felt the TERM: poll briefly for the marker.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("the fake git's child was never signalled: the kill reached the process only, not the group")
}

// # Parser units — the states no temp-dir repo can cheaply produce
//
// These call parseStatus directly with recorded bytes (AGENTS §5: recorded
// fixtures). A truncated listing, a malformed record and a rename without its
// source must all answer unavailable — never a short listing, never ok.

func TestParseStatusUnknownRecordIsUnavailable(t *testing.T) {
	got := parseStatus("# branch.oid abc\x00# branch.head main\x00Z weird record\x00")
	if got.State != StateUnavailable || !strings.Contains(got.Reason, "porcelain v2 record") {
		t.Errorf("got %+v, want unavailable naming the record", got)
	}
	if got.Changed != nil {
		t.Error("an unavailable answer must not carry a listing")
	}
}

func TestParseStatusTruncatedRenameIsUnavailable(t *testing.T) {
	// A `2` record whose origPath field (the NEXT NUL-separated record) is gone.
	got := parseStatus("# branch.head main\x002 R. N... 100644 100644 100644 aaa bbb R100 new.txt\x00")
	if got.State != StateUnavailable || !strings.Contains(got.Reason, "rename") {
		t.Errorf("got %+v, want unavailable naming the rename", got)
	}
}

func TestParseStatusBadAheadBehindIsUnavailable(t *testing.T) {
	got := parseStatus("# branch.head main\x00# branch.ab +x -y\x00")
	if got.State != StateUnavailable {
		t.Errorf("got %+v, want unavailable", got)
	}
}

func TestParseStatusXYWrongLengthIsUnavailable(t *testing.T) {
	got := parseStatus("# branch.head main\x001 X N... 100644 100644 100644 aaa bbb f.txt\x00")
	if got.State != StateUnavailable {
		t.Errorf("got %+v, want unavailable", got)
	}
}

// A status stream past the cap is refused at the GitStatus level (before
// parsing), so the listing cannot render complete while missing entries. The
// fake `git` is dd: five megabytes of NUL — content is irrelevant because
// truncation is checked first, which is the point.
func TestGitStatusTruncatedIsUnavailableNotShort(t *testing.T) {
	dir := repo(t)
	fakeDir := t.TempDir()
	script := "#!/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; export PATH\n" +
		"dd if=/dev/zero bs=1048576 count=5 2>/dev/null\nexit 0\n"
	if err := os.WriteFile(filepath.Join(fakeDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir)

	got := GitStatus(context.Background(), dir)
	if got.State != StateUnavailable {
		t.Fatalf("state = %q (reason %q), want unavailable — a truncated listing must not render short", got.State, got.Reason)
	}
	if !strings.Contains(got.Reason, "5242880") {
		t.Errorf("reason %q does not carry the TRUE byte count (spawn.go's D9)", got.Reason)
	}
}

// # GitDiff — working tree vs HEAD for one path (D8)

func TestGitDiffTrackedModified(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "code.go", "package a\n\nvar x = 1\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	writeFile(t, dir, "code.go", "package a\n\nvar x = 2\n")

	got := GitDiff(context.Background(), dir, "code.go")
	if got.State != StateOK {
		t.Fatalf("state = %q (reason %q), want ok", got.State, got.Reason)
	}
	if got.Binary || got.Truncated {
		t.Errorf("binary/truncated = %v/%v on a small text diff", got.Binary, got.Truncated)
	}
	for _, want := range []string{"@@ -1,3 +1,3 @@", "-var x = 1", "+var x = 2"} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("body does not contain %q:\n%s", want, got.Body)
		}
	}
}

// An untracked file gets the all-added diff git will show the moment it is
// staged — the shape the conformance fixture pins.
func TestGitDiffUntrackedAllAdded(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "tracked.txt", "t\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	writeFile(t, dir, "SURVEY.md", "# Survey\n\nFindings here.\n")

	got := GitDiff(context.Background(), dir, "SURVEY.md")
	if got.State != StateOK {
		t.Fatalf("state = %q (reason %q), want ok", got.State, got.Reason)
	}
	want := "--- /dev/null\n+++ b/SURVEY.md\n@@ -0,0 +1,3 @@\n+# Survey\n+\n+Findings here.\n"
	if got.Body != want {
		t.Errorf("synthesised all-added body:\n%q\nwant:\n%q", got.Body, want)
	}
	if got.Binary || got.Truncated {
		t.Errorf("binary/truncated = %v/%v on a small all-added diff", got.Binary, got.Truncated)
	}
	// BodyBytes is the FILE's true size (content, not diff text):
	// "# Survey\n" + "\n" + "Findings here.\n" = 9+1+15.
	if got.BodyBytes != 25 {
		t.Errorf("bodyBytes = %d, want 25 (the file's true size)", got.BodyBytes)
	}
}

// Content that does not end in a newline gets git's own marker. Measured
// against `git diff --no-index /dev/null` on git 2.53.0 — the synthesis is
// byte-identical to what staging the file would produce.
func TestGitDiffUntrackedNoNewlineMarker(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "tracked.txt", "t\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	writeFile(t, dir, "nonl.txt", "a\nb")

	got := GitDiff(context.Background(), dir, "nonl.txt")
	if got.State != StateOK {
		t.Fatalf("state = %q (reason %q)", got.State, got.Reason)
	}
	if !strings.HasSuffix(got.Body, "\\ No newline at end of file\n") {
		t.Errorf("body %q lacks the no-newline marker", got.Body)
	}
	if !strings.Contains(got.Body, "@@ -0,0 +1,2 @@") {
		t.Errorf("body %q has the wrong hunk header", got.Body)
	}
}

// A binary file reports `binary: true` with NO body — a NUL stream rendered
// into a web view is not a diff. (Measured: git itself emits only
// `Binary files a/… and b/… differ` for the tracked case.)
func TestGitDiffBinaryHasNoBody(t *testing.T) {
	dir := repo(t)
	if err := os.MkdirAll(filepath.Join(dir, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "img", "logo.png"), []byte("PNG\x00\x01\x02"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	if err := os.WriteFile(filepath.Join(dir, "img", "logo.png"), []byte("PNG\x00\x09\x09\x09"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := GitDiff(context.Background(), dir, "img/logo.png")
	if got.State != StateOK {
		t.Fatalf("state = %q (reason %q)", got.State, got.Reason)
	}
	if !got.Binary {
		t.Errorf("binary = false, want true; body:\n%q", got.Body)
	}
	if got.Body != "" {
		t.Errorf("binary answer carries a body: %q", got.Body)
	}
}

// A deleted file shows the all-deleted diff; a clean file honestly answers
// ok with an empty body.
func TestGitDiffDeletedAndClean(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "doomed.txt", "was here\n")
	writeFile(t, dir, "kept.txt", "stay\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	if err := os.Remove(filepath.Join(dir, "doomed.txt")); err != nil {
		t.Fatal(err)
	}

	doomed := GitDiff(context.Background(), dir, "doomed.txt")
	if doomed.State != StateOK || !strings.Contains(doomed.Body, "-was here") {
		t.Errorf("deleted file diff = %+v", doomed)
	}
	kept := GitDiff(context.Background(), dir, "kept.txt")
	if kept.State != StateOK || kept.Body != "" || kept.Binary {
		t.Errorf("clean file diff = %+v, want ok with empty body", kept)
	}
}

// A diff past the 512 KiB cap reports truncated WITH the true byte count
// (spawn.go's D9) rather than being silently short.
func TestGitDiffHugeIsTruncatedWithTrueSize(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "base.txt", "seed\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")

	var big strings.Builder
	for i := 0; i < 12000; i++ {
		fmt.Fprintf(&big, "line %06d · append me to exceed half a megabyte of diff text\n", i)
	}
	writeFile(t, dir, "base.txt", "seed\n"+big.String())

	got := GitDiff(context.Background(), dir, "base.txt")
	if got.State != StateOK {
		t.Fatalf("state = %q (reason %q)", got.State, got.Reason)
	}
	if !got.Truncated {
		t.Fatalf("truncated = false on a %d-byte diff", got.BodyBytes)
	}
	if len(got.Body) != DiffCapBytes {
		t.Errorf("body kept %d bytes, want exactly the %d-byte cap", len(got.Body), DiffCapBytes)
	}
	if got.BodyBytes <= DiffCapBytes {
		t.Errorf("bodyBytes = %d does not exceed the cap; truncation must carry the TRUE size", got.BodyBytes)
	}
}

// A path with spaces survives resolve → pathspec → header, unquoted.
func TestGitDiffPathWithSpaces(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "a b.txt", "one\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	writeFile(t, dir, "a b.txt", "two\n")

	got := GitDiff(context.Background(), dir, "a b.txt")
	if got.State != StateOK {
		t.Fatalf("state = %q (reason %q)", got.State, got.Reason)
	}
	if !strings.Contains(got.Body, "-one") || !strings.Contains(got.Body, "+two") {
		t.Errorf("spaced-path diff is wrong:\n%s", got.Body)
	}
}

// Paths are bounded by the SAME resolver as `loop.file.read` (tools.Resolve):
// traversal and absolute paths are refused, and the refusal NAMES the path.
func TestGitDiffRefusesEscapingAndAbsolutePaths(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "in.txt", "inside\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")

	traversal := GitDiff(context.Background(), dir, "../outside.txt")
	if traversal.State != StateUnavailable ||
		!strings.Contains(traversal.Reason, "../outside.txt") ||
		!strings.Contains(traversal.Reason, "refused") {
		t.Errorf("traversal refusal = %+v; must be unavailable naming the path", traversal)
	}
	absolute := GitDiff(context.Background(), dir, "/etc/passwd")
	if absolute.State != StateUnavailable ||
		!strings.Contains(absolute.Reason, "/etc/passwd") {
		t.Errorf("absolute refusal = %+v; must name the path", absolute)
	}
}

func TestGitDiffNotARepo(t *testing.T) {
	got := GitDiff(context.Background(), t.TempDir(), "anything.txt")
	if got.State != StateNotARepo {
		t.Fatalf("state = %q (reason %q), want not-a-repo", got.State, got.Reason)
	}
}

// No commits yet: "working tree vs HEAD" has no answer, and it is SPOKEN,
// not rendered as an empty ok.
func TestGitDiffNoCommitsYet(t *testing.T) {
	dir := repo(t)
	writeFile(t, dir, "a.txt", "1\n")
	got := GitDiff(context.Background(), dir, "a.txt")
	if got.State != StateUnavailable || !strings.Contains(got.Reason, "no commits yet") {
		t.Fatalf("state = %q (reason %q), want unavailable naming the missing HEAD", got.State, got.Reason)
	}
}

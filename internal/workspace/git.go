// Package workspace answers the loop workspace's read-only git questions
// (S31 T6, D7/D8): what state is this project in, and what changed in this
// file. It is the only place git is spoken to on the daemon's own behalf.
//
// # Read-only, daemon-issued, and why there is no policy token here (LW11)
//
// `tools.Spawn`'s FIRST statement demands a policy-issued `AllowToken` minted
// for one exact agent command — that is S11's structural order (verdict →
// receipt → spawn), and it is right for what AGENTS run. These reads are not
// agent commands: they are the DAEMON's own reads, issued on the P26 socket to
// answer the person's own screen (branch, ahead/behind, one file's diff). A
// token would have to be minted for the daemon impersonating an agent, which
// is theatre: the reads are bounded, not governed. Git *actions* (commit,
// branch, push) arrive at S16 under effect classes; this slice never writes
// (N3), and `--no-optional-locks` keeps even the index untouched.
//
// What IS reproduced from `tools/spawn.go`, exactly, is the bounded process
// layer it measured (CODEMAP §8): own `context.WithTimeout` rather than
// `exec.CommandContext` (which kills only the process — the GROUP must die),
// `Setpgid: true` with `Kill(-pgid, TERM)` → grace → unconditional `Kill(-pgid,
// KILL)`, stdin the null device, and output caps that keep the TRUE byte
// counts so truncation is reported rather than silent.
//
// # Three states, never two (D7, P31)
//
// `ok` | `not-a-repo` | `unavailable` (naming the reason). An `unavailable`
// must never render as an empty `ok`: `StateOK` is assigned at the END of a
// fully parsed success path, and every early return is `unavailable` or
// `not-a-repo`. A truncated status listing is `unavailable`, not a confident
// short list; `ahead`/`behind` are pointers because "no upstream" is not the
// claim "0 ahead of nothing".
package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/tools"
)

const (
	// DiffCapBytes is the brief's 512 KiB cap on a rendered diff (§5), with
	// truncation reported rather than silent.
	DiffCapBytes = 512 * 1024

	// statusOutputCapBytes bounds `git status` stdout. Truncation THERE makes
	// the listing incomplete, and an incomplete list rendered as `ok` is
	// exactly the confident wrong answer P31 forbids — so hitting this cap
	// answers `unavailable` naming the true size instead of rendering.
	statusOutputCapBytes = 4 << 20

	// stderrCapBytes bounds the fatal text carried into `reason`.
	stderrCapBytes = 64 * 1024

	// binarySniffBytes is how much of a synthesised (untracked) file is
	// scanned for a NUL before calling it binary — git's own heuristic is a
	// NUL anywhere in the buffer; a HUMAN READS the diff body, and a NUL in
	// the first 8 KiB of a multi-megabyte log is git's binary signal too.
	binarySniffBytes = 8192
)

// The timeout and grace are package variables rather than constants so the
// timeout test can shorten them without waiting out five real seconds. The
// production values are the brief's (`~5 s`) and the value spawn.go measured
// (tools.BashKillGrace, 2 s). Only tests reassign them, one goroutine at a
// time (no t.Parallel in this package's tests).
var (
	commandTimeout = 5 * time.Second
	killGrace      = 2 * time.Second
)

// State is the discriminator of every result in this package (D7/P31).
type State string

const (
	StateOK          State = "ok"
	StateNotARepo    State = "not-a-repo"
	StateUnavailable State = "unavailable"
)

// ChangedFile is one entry of the status listing. Index and Worktree carry
// the porcelain v2 XY codes verbatim (X to index, Y to worktree); an
// untracked path maps to Index "?" and Worktree "U" — the wire convention the
// conformance fixture pins (conformance/workspace/git-status.json).
// OrigPath is set for rename/copy records only.
type ChangedFile struct {
	Path     string
	Index    string
	Worktree string
	OrigPath string
}

// StatusResult is the three-state answer to `GitStatus`. The ok-state fields
// are pointers so a non-ok answer cannot carry a plausible-looking zero:
// Branch nil ≠ branch named "". Ahead/Behind nil means no upstream — absence,
// not zero (P31).
type StatusResult struct {
	State  State
	Reason string // non-empty only when State == StateUnavailable

	Branch   *string
	Detached bool
	Upstream *string
	Ahead    *int
	Behind   *int
	Changed  []ChangedFile
}

// DiffResult is the three-state answer to `GitDiff`. Body is the unified diff
// (working tree vs HEAD), empty for a binary file or a file with nothing to
// show. BodyBytes is the TRUE size before the cap, so `Truncated` says how
// much is missing.
type DiffResult struct {
	State  State
	Reason string

	Body      string
	Binary    bool
	Truncated bool
	BodyBytes int
}

func unavailableStatus(reason string) StatusResult {
	return StatusResult{State: StateUnavailable, Reason: reason}
}

func unavailableDiff(reason string) DiffResult {
	return DiffResult{State: StateUnavailable, Reason: reason}
}

// # GitStatus — branch, upstream, ahead/behind, changed paths (D7)
//
// The command is `git status --porcelain=v2 --branch -z` under
// `--no-optional-locks` (a view must not write the index; a step running
// concurrently owns it). `-z` is what makes paths with spaces and newlines
// parseable: records are NUL-separated and paths appear raw, never quoted.
// Measured against git 2.53.0 (the version this parser was written and
// tested against; the record grammar is stable since porcelain v2 arrived in
// 2.11):
//
//	# branch.oid <sha>\0 # branch.head <name>\0        (detached: "(detached)")
//	# branch.upstream <name>\0 # branch.ab +A -B\0     (both absent without upstream)
//	1 <XY> N... <mH> <mI> <mW> <hH> <hI> <path>\0
//	2 <XY> N... <mH> <mI> <mW> <hH> <hI> <score> <newPath>\0<origPath>\0
//	u <XY> N... <m1> <m2> <m3> <mW> <h1> <h2> <hI> <path>\0
//	? <path>\0
//
// An unrecognised record type is `unavailable`, not a skipped line: silently
// dropping an entry drops a change, and a listing with changes missing is the
// reassuring empty list P31 names.

func GitStatus(ctx context.Context, root string) StatusResult {
	out, err := run(ctx, root, statusOutputCapBytes,
		"--no-optional-locks", "status", "--porcelain=v2", "--branch", "-z")
	if err != nil {
		return unavailableStatus(err.Error()) // git missing or could not start
	}
	if out.timedOut {
		return unavailableStatus(timeoutReason("git status"))
	}
	if out.exitCode == 128 && isNotARepo(out.stderr) {
		return StatusResult{State: StateNotARepo}
	}
	if out.exitCode != 0 {
		return unavailableStatus("git status failed: " + tail(out.stderr))
	}
	if out.stdoutTruncated {
		return unavailableStatus(fmt.Sprintf(
			"git status output exceeded its %d MiB cap (true size %d bytes); an incomplete "+
				"listing would render as a complete one, which is the wrong answer P31 forbids",
			statusOutputCapBytes>>20, out.stdoutBytes))
	}
	return parseStatus(out.stdout)
}

func parseStatus(out string) StatusResult {
	res := StatusResult{State: StateOK}
	recs := strings.Split(out, "\x00")
	if len(recs) > 0 && recs[len(recs)-1] == "" {
		recs = recs[:len(recs)-1] // the stream ends with its final NUL
	}

	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		switch {
		case strings.HasPrefix(rec, "# "):
			key, val, _ := strings.Cut(rec[2:], " ")
			switch key {
			case "branch.oid":
				// not part of the answer (the UI asks branch, not sha)
			case "branch.head":
				if val == "(detached)" { // measured: git 2.53.0's detached marker
					res.Detached = true
				} else {
					v := val
					res.Branch = &v
				}
			case "branch.upstream":
				v := val
				res.Upstream = &v
			case "branch.ab":
				a, b, ok := strings.Cut(val, " ")
				ahead, err1 := strconv.Atoi(strings.TrimPrefix(a, "+"))
				behind, err2 := strconv.Atoi(strings.TrimPrefix(b, "-"))
				if !ok || err1 != nil || err2 != nil {
					return unavailableStatus(fmt.Sprintf(
						"the ahead/behind header %q could not be parsed; the state is unknown, "+
							"and unknown is answered, not guessed", rec))
				}
				res.Ahead, res.Behind = &ahead, &behind
			default:
				// An unknown `# branch.*` header is tolerated: additive fields
				// from a future git ride past, the way forward-compat fixtures
				// tolerate unknown result fields. Unknown RECORD TYPES do not.
			}

		case strings.HasPrefix(rec, "1 "):
			f := strings.SplitN(rec, " ", 9) // type XY sub mH mI mW hH hI path
			if len(f) != 9 {
				return malformed(rec)
			}
			idx, wt, bad := xyCodes(f[1])
			if bad != "" {
				return unavailableStatus(bad + " in " + quoteShort(rec))
			}
			res.Changed = append(res.Changed,
				ChangedFile{Path: f[8], Index: idx, Worktree: wt})

		case strings.HasPrefix(rec, "2 "):
			f := strings.SplitN(rec, " ", 10) // … + score newPath; origPath is the NEXT record
			if len(f) != 10 {
				return malformed(rec)
			}
			idx, wt, bad := xyCodes(f[1])
			if bad != "" {
				return unavailableStatus(bad + " in " + quoteShort(rec))
			}
			i++
			if i >= len(recs) {
				return unavailableStatus(
					"a rename record arrived without its source path; the listing is incomplete, " +
						"so it is unavailable rather than rendered short")
			}
			res.Changed = append(res.Changed,
				ChangedFile{Path: f[9], Index: idx, Worktree: wt, OrigPath: recs[i]})

		case strings.HasPrefix(rec, "u "):
			f := strings.SplitN(rec, " ", 11) // type XY sub m1 m2 m3 mW h1 h2 hI path
			if len(f) != 11 {
				return malformed(rec)
			}
			idx, wt, bad := xyCodes(f[1])
			if bad != "" {
				return unavailableStatus(bad + " in " + quoteShort(rec))
			}
			res.Changed = append(res.Changed,
				ChangedFile{Path: f[10], Index: idx, Worktree: wt})

		case strings.HasPrefix(rec, "? "):
			f := strings.SplitN(rec, " ", 2)
			if len(f) != 2 {
				return malformed(rec)
			}
			// The wire convention for untracked (pinned by the conformance
			// fixture): Index "?", Worktree "U".
			res.Changed = append(res.Changed,
				ChangedFile{Path: f[1], Index: "?", Worktree: "U"})

		default:
			// Includes the empty interior record — a path is never empty.
			return malformed(rec)
		}
	}
	return res
}

// # GitDiff — one path, working tree vs HEAD (D8)
//
// Tracked files diff through git itself (`git diff --no-color --no-ext-diff
// HEAD -- <path>`: --no-color and --no-ext-diff because the output is
// machine-rendered, not for a terminal). An empty result means either "tracked
// and identical to HEAD" or "not in HEAD at all", and `git ls-files
// --error-unmatch` tells the two apart; an untracked regular file gets a
// SYNTHESISED all-added diff, which is what git would show the moment it is
// staged. A binary file reports Binary with no body (a NUL stream rendered
// into a web view is not a diff). Everything is capped at DiffCapBytes with
// Truncated and the true BodyBytes reported, per spawn.go's rule that
// truncation is spoken, never silent.
//
// The path goes through the SAME resolver as `loop.file.read` (brief §5:
// `tools.Resolve`) — an escape is refused before any process starts. The
// protocol handler turns the refusal into a named error; this layer still
// refuses defensively, because an internal package may not exec a path its
// caller did not bound.

// OriginDefaultBranch reads the repository's default branch as the REMOTE
// named it: symbolic-ref refs/remotes/origin/HEAD, with the origin/ prefix
// trimmed. S60 MQ15's last fallback when loop.start pins a loop's base and
// nobody configured one. Everything it cannot answer — not a repo, no
// remote, HEAD not set, timeout, truncation — comes back as the empty
// string: EMPTY MEANS UNKNOWN here (P31), never a confident "main".
func OriginDefaultBranch(ctx context.Context, root string) string {
	out, err := run(ctx, root, statusOutputCapBytes, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil || out.timedOut || out.exitCode != 0 || out.stdoutTruncated {
		return ""
	}
	branch := strings.TrimSpace(out.stdout)
	if branch == "" || branch == "HEAD" {
		return ""
	}
	return strings.TrimPrefix(branch, "origin/")
}

func GitDiff(ctx context.Context, root, path string) DiffResult {
	r := tools.Resolve(root, path)
	if !r.OK {
		return unavailableDiff(fmt.Sprintf("the path %q was refused: %s", path, r.Reason))
	}

	// `rev-parse --verify --quiet HEAD` is one cheap probe that separates
	// three preconditions: not a repo (128 + the fatal text), no commits yet
	// (exit 1 — HEAD names nothing, so "working tree vs HEAD" has no answer,
	// and it is answered as unavailable rather than as an empty diff), and
	// git working at all. Measured against git 2.53.0.
	head, err := run(ctx, root, stderrCapBytes, "--no-optional-locks", "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return unavailableDiff(err.Error())
	}
	if head.timedOut {
		return unavailableDiff(timeoutReason("git rev-parse"))
	}
	switch {
	case head.exitCode == 0:
	case head.exitCode == 1:
		return unavailableDiff("the repository has no commits yet — there is no HEAD to diff against")
	case head.exitCode == 128 && isNotARepo(head.stderr):
		return DiffResult{State: StateNotARepo}
	default:
		return unavailableDiff("git rev-parse failed: " + tail(head.stderr))
	}

	out, err := run(ctx, root, DiffCapBytes,
		"--no-optional-locks", "diff", "--no-color", "--no-ext-diff", "HEAD", "--", r.Rel)
	if err != nil {
		return unavailableDiff(err.Error())
	}
	if out.timedOut {
		return unavailableDiff(timeoutReason("git diff"))
	}
	if out.exitCode != 0 {
		return unavailableDiff("git diff failed: " + tail(out.stderr))
	}

	body := out.stdout
	if isBinaryDiff(body) {
		// Measured: git renders a binary change as the one line
		// `Binary files a/<p> and b/<p> differ` — no hunk to carry.
		return DiffResult{State: StateOK, Binary: true, BodyBytes: out.stdoutBytes}
	}
	if body == "" {
		ls, err := run(ctx, root, stderrCapBytes,
			"--no-optional-locks", "ls-files", "--error-unmatch", "--", r.Rel)
		if err != nil {
			return unavailableDiff(err.Error())
		}
		if ls.timedOut {
			return unavailableDiff(timeoutReason("git ls-files"))
		}
		if ls.exitCode == 0 {
			// Tracked and byte-identical to HEAD: an honest empty diff.
			return DiffResult{State: StateOK}
		}
		info, statErr := os.Stat(r.Abs)
		if statErr != nil || !info.Mode().IsRegular() {
			// Not in HEAD, not in the index, not a readable file: there is
			// nothing to show, and `ok` with an empty body says that.
			return DiffResult{State: StateOK}
		}
		return synthAllAdded(r.Rel, r.Abs, info.Size())
	}
	return DiffResult{
		State:     StateOK,
		Body:      body,
		Truncated: out.stdoutTruncated,
		BodyBytes: out.stdoutBytes,
	}
}

// synthAllAdded renders an untracked file as the all-added diff git will show
// the moment it is staged: `--- /dev/null` / `+++ b/<path>` / one hunk, every
// line `+`-prefixed, and the `\ No newline at end of file` marker git itself
// emits when the content does not end in a newline. Binary by the NUL
// heuristic (stated as one), capped by DiffCapBytes with the true size from
// the stat.
func synthAllAdded(rel, abs string, size int64) DiffResult {
	f, err := os.Open(abs)
	if err != nil {
		return unavailableDiff(fmt.Sprintf("the untracked file %q could not be read: %v", rel, err))
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, int64(DiffCapBytes)+1))
	if err != nil {
		return unavailableDiff(fmt.Sprintf("the untracked file %q could not be read: %v", rel, err))
	}
	truncated := len(data) > DiffCapBytes
	if truncated {
		data = data[:DiffCapBytes]
	}
	sniff := data
	if len(sniff) > binarySniffBytes {
		sniff = sniff[:binarySniffBytes]
	}
	if bytes.IndexByte(sniff, 0) >= 0 {
		return DiffResult{State: StateOK, Binary: true, BodyBytes: int(size)}
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1] // trailing newline terminates the last line
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", rel, len(lines))
	for _, line := range lines {
		sb.WriteString("+")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		sb.WriteString("\\ No newline at end of file\n")
	}
	return DiffResult{
		State:     StateOK,
		Body:      sb.String(),
		Truncated: truncated,
		BodyBytes: int(size),
	}
}

// # The bounded process layer (LW11 — spawn.go's discipline, no policy token)
//
// See the package comment for why there is no AllowToken here and what is
// kept from spawn.go. The ONE deliberate addition over spawn.go's inherited
// environment: every `GIT_*` variable is stripped and git's own determinism
// switches are set. spawn.go inherits the environment because a bash tool run
// needs the caller's PATH and HOME, and says so; a git READ that an inherited
// GIT_DIR could silently retarget at another repository is a wrong answer
// with a caller-shaped cause — the read must answer for `root`, the working
// directory, and nothing else. LC_ALL=C keeps fatal texts and numbers stable.

type runOutcome struct {
	exitCode        int
	stdout          string
	stderr          string
	stdoutBytes     int // TRUE size, before the cap (spawn.go's D9)
	stderrBytes     int
	stdoutTruncated bool
	timedOut        bool
}

// errGitMissing names "git is not installed" — the brief's wording, so an
// operator reading the UI's `unavailable` reason knows what to do.
type errGitMissing struct{ lookupErr error }

func (e errGitMissing) Error() string {
	return "git is not installed: " + e.lookupErr.Error()
}
func (e errGitMissing) Unwrap() error { return e.lookupErr }

// run executes one git command with every bound applied. A non-zero exit is
// NOT an error — git ran, and what it said is a fact for the caller to
// classify (three states). Only "git could not be started at all" is an error.
func run(ctx context.Context, dir string, stdoutCap int, args ...string) (runOutcome, error) {
	// LookPath per call, never cached: the daemon's PATH is the operator's,
	// and "install git, retry" must work without a daemon restart.
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return runOutcome{}, errGitMissing{lookupErr: err}
	}

	// The bounded timings are captured ONCE, synchronously, here — the kill
	// goroutine below is handed `grace` as a parameter rather than reading
	// the package global, because it can outlive the call: a test that
	// shortens these knobs and restores them on return would otherwise race
	// the goroutine still sleeping toward its SIGKILL (measured under -race).
	timeout, grace := commandTimeout, killGrace

	// spawn.go's measured reason: `exec.CommandContext` kills only the PROCESS.
	// This kills the GROUP, so the timeout is a context of our own.
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command(gitPath, args...)
	cmd.Dir = dir
	// spawn.go's null device: a git waiting on input reads EOF instead of
	// blocking to the timeout. `git` does not read stdin here — no pager (GIT_PAGER),
	// no prompts (GIT_TERMINAL_PROMPT) — but the bound is kept, not assumed.
	cmd.Stdin = nil
	cmd.Env = gitEnv()

	// Setpgid is what makes a group kill possible (without it, Kill(-pid)
	// signals the DAEMON's group — a daemon that kills itself when git hangs).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout := &cappedBuffer{limit: stdoutCap}
	stderr := &cappedBuffer{limit: stderrCapBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return runOutcome{}, fmt.Errorf("git could not be started: %w", err)
	}
	pgid := cmd.Process.Pid

	done := make(chan struct{})
	var terminated bool
	var termMu sync.Mutex
	go func() {
		select {
		case <-runCtx.Done():
			termMu.Lock()
			terminated = true
			termMu.Unlock()
			terminateGroup(pgid, grace)
		case <-done:
		}
	}()

	waitErr := cmd.Wait()
	close(done)

	termMu.Lock()
	wasTerminated := terminated
	termMu.Unlock()

	res := runOutcome{
		stdout:          stdout.String(),
		stderr:          stderr.String(),
		stdoutBytes:     stdout.total,
		stderrBytes:     stderr.total,
		stdoutTruncated: stdout.truncated,
		// The TIMEOUT specifically, not any cancellation: a cancelled handler
		// and a git that ran too long are different facts (spawn.go's rule).
		timedOut: wasTerminated && errors.Is(runCtx.Err(), context.DeadlineExceeded),
	}
	if state := cmd.ProcessState; state != nil {
		res.exitCode = state.ExitCode()
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			return res, fmt.Errorf("git did not complete: %w", waitErr)
		}
	}
	return res, nil
}

// terminateGroup: SIGTERM, the grace, then unconditional SIGKILL — spawn.go
// verbatim. The negative pid is the mechanism; SIGKILL to a dead group is a
// harmless ESRCH, and checking aliveness first is a race.
func terminateGroup(pgid int, grace time.Duration) {
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	time.Sleep(grace)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// gitEnv builds the environment for one read of a repository for the app.
//
// # A3 (S59 T5): the base is execenv.GitEnv's, not the process's
//
// This function used to copy the daemon's whole environment minus `GIT_*`, and
// the hardening block below it was written out here. It was the one
// environment builder in the codebase that already did the strip, which is why
// EV10 names this path (F16) and why the block now lives in `execenv` — one
// list, one order, two callers. What changed is the base: the child gets the
// allowlist (`PATH`, `HOME`, `USER`, `LOGNAME`, `SHELL`, `LANG`, `LC_*`, `TZ`,
// `TMPDIR`) rather than everything the daemon happens to hold, so a UI read of
// `git log` can no longer see a provider key that was in the daemon's
// environment for entirely unrelated reasons. `SSH_AUTH_SOCK` is deliberately
// NOT here — it goes to the daemon's own git and nowhere else (D9).
//
// `os.Getenv` appears only in this file's injected reader: the app's git reads
// are the daemon answering a question about a directory, and the ambient state
// has to enter at exactly one place for a test to be able to say what entered.
func gitEnv() []string {
	return execenv.GitEnv(
		execenv.DaemonEnv{Lookup: os.Getenv, Names: func() []string {
			raw := os.Environ()
			out := make([]string, 0, len(raw))
			for _, kv := range raw {
				if i := strings.IndexByte(kv, '='); i > 0 {
					out = append(out, kv[:i])
				}
			}
			return out
		}},
		execenv.GitEnvUIRead)
}

// cappedBuffer keeps the first `limit` bytes and counts ALL of them —
// tools/spawn.go's D9: truncation is reported with the TRUE size. It never
// errors from Write, so a chatty git is capped rather than killed.
type cappedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	total     int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total += len(p)
	room := c.limit - c.buf.Len()
	switch {
	case room <= 0:
		c.truncated = true
	case len(p) > room:
		c.buf.Write(p[:room])
		c.truncated = true
	default:
		c.buf.Write(p)
	}
	// The full length is reported written: returning less makes exec treat it
	// as a short write and abandon the stream (spawn.go's reason).
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// # Small helpers, each naming what it refuses

func isNotARepo(stderr string) bool {
	// Measured against git 2.53.0 outside any repository:
	//   fatal: not a git repository (or any parent up to mount point /)
	// The stable substring across git's discovery variants is this one.
	return strings.Contains(stderr, "not a git repository")
}

func timeoutReason(what string) string {
	return fmt.Sprintf("%s did not finish within %s; its process group was killed (TERM, then KILL after the grace)",
		what, commandTimeout)
}

func isBinaryDiff(body string) bool {
	// Line-anchored so a context line that merely quotes the sentence cannot
	// claim the diff binary; the NUL check is the belt.
	return strings.Contains(body, "\nBinary files ") || strings.ContainsRune(body, 0)
}

func xyCodes(xy string) (string, string, string) {
	if len(xy) != 2 {
		return "", "", fmt.Sprintf("the XY status %q is not two characters", xy)
	}
	return xy[0:1], xy[1:2], ""
}

func malformed(rec string) StatusResult {
	return unavailableStatus(fmt.Sprintf(
		"the porcelain v2 record %q is not one this parser knows; the state is unknown, "+
			"and unknown is answered — never rendered as an empty ok", quoteShort(rec)))
}

// tail renders git's fatal text for a `reason`: trimmed and shortened, since
// the reason is read in a UI, not grepped.
func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

func quoteShort(s string) string {
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return strconv.Quote(s)
}

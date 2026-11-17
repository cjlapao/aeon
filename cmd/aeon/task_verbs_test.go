package main

// task_verbs_test.go — S38 T16: the org doors at the terminal, against a
// REAL daemon on a temp socket (the same fixture discipline as every client
// verb: the CLI is a client of the socket, so the test speaks as a client
// too). The socket-side proofs (who the decider is, the filters, the cost
// floor) live in internal/daemon's t15_socket_test.go against the fake
// transport's injected peer; these tests own what only a subprocess can
// own: exit codes, help text, and the flags a person actually types.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

func writeFile(t *testing.T, f *daemonFixture, text string) string {
	t.Helper()
	path := filepath.Join(f.stateDir, "..", "body.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runExit(t *testing.T, f *daemonFixture, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(aeonBinary, args...)
	cmd.Env = f.env()
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), ee.ExitCode()
	}
	t.Fatalf("aeon %s: %v\n%s", strings.Join(args, " "), err, out)
	return "", -1
}

func TestGoalCreateThenListThenDecide(t *testing.T) {
	d := startDaemon(t)
	seedRoot(t, d, "Board group")

	out := d.mustRun("goal", "create", "--root", "Board group", "--title", "Ship the board",
		"--type", "feature", "--body-file", writeFile(t, d, "goal was a wish until a person judged it"))
	fields := strings.Fields(out)
	if len(fields) < 2 || !strings.Contains(out, "in_review") {
		t.Fatalf("goal create did not file into review: %q", out)
	}
	goalID := fields[0]

	listed := d.mustRun("goal", "list")
	if !strings.Contains(listed, goalID) || !strings.Contains(listed, "in_review") {
		t.Fatalf("the listing lost the goal or its state: %q", listed)
	}
	if filtered := d.mustRun("goal", "list", "--state", "approved"); strings.Contains(filtered, goalID) {
		t.Fatalf("the state filter let an unjudged goal through: %q", filtered)
	}

	decided := d.mustRun("goal", "decide", goalID, "--version", "1", "--decision", "approved", "--reason", "the frame is right")
	if !strings.Contains(decided, "decided: approved") {
		t.Fatalf("decide did not decide: %q", decided)
	}
	shown := d.mustRun("goal", "show", goalID)
	if !strings.Contains(shown, "[approved]") || !strings.Contains(shown, "the frame is right") {
		t.Fatalf("show does not carry the verdict and its reason: %q", shown)
	}
	// The decider is the peer the KERNEL reported — for a real subprocess
	// that is this very user; the assertion is that the ledger line names a
	// USER by id, not the params (which carry no actor field at all).
	if !strings.Contains(shown, "v1  approved  user/") {
		t.Fatalf("the ledger does not stamp the deciding user: %q", shown)
	}

	// A second decision changes nothing and says who did.
	again := d.mustRun("goal", "decide", goalID, "--version", "1", "--decision", "rejected")
	if !strings.Contains(again, "already decided") || strings.Contains(again, "decided: rejected") {
		t.Fatalf("a second decide decided again: %q", again)
	}

	// Unknown ids answer, they do not crash.
	if out, code := runExit(t, d, "goal", "show", "tsg-nope"); code == 0 {
		t.Fatalf("showing a goal that does not exist answered happily: %q", out)
	}
}

// A decision that does not name its version judges the wrong page, so the
// CLI refuses it as USAGE (exit 2), not as failure (exit 1).
func TestDecideNeedsAVersion(t *testing.T) {
	d := startDaemon(t)
	seedRoot(t, d, "Versioned")
	out := d.mustRun("goal", "create", "--root", "Versioned", "--title", "v-gated", "--type", "feature")
	goalID := strings.Fields(out)[0]

	if out, code := runExit(t, d, "goal", "decide", goalID, "--decision", "approved"); code != 2 {
		t.Fatalf("decide without --version exited %d, want the usage exit 2: %q", code, out)
	}
	if out, code := runExit(t, d, "brief", "decide", "--loop", "any", "--decision", "approved"); code != 2 {
		t.Fatalf("brief decide without --version exited %d, want 2: %q", code, out)
	}
	if out, code := runExit(t, d, "goal", "decide", goalID, "--version", "1", "--decision", "maybe"); code != 2 {
		t.Fatalf("an off-vocabulary decision exited %d, want 2: %q", code, out)
	}
}

func TestTaskCreateManualAssignedToUser(t *testing.T) {
	d := startDaemon(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	// The walk's finding, recorded: `loop create` runs a loop in a directory
	// — it does NOT register a component (loops carry no FK, and the records
	// doors refuse an unregistered root by name, exactly as here). Components
	// arrive through the socket's component.create, which has no CLI verb
	// yet; the test seeds one through the SAME store file the daemon holds
	// (WAL serialises the second connection — the access path PL15 already
	// sanctions for `aeon policy`).
	seedComponent(t, d.stateDir, "boardcomp", repo)

	out := d.mustRun("task", "create", "--component", repo, "--title", "hand rung",
		"--type", "feature", "--assignee", "user:carlos")
	if !strings.Contains(out, "user:carlos") {
		t.Fatalf("the assignee's kind was lost at the flag: %q", out)
	}
	taskID := strings.Fields(out)[0]

	manual := d.mustRun("task", "list", "--manual", "true")
	if !strings.Contains(manual, taskID) || !strings.Contains(manual, "manual") {
		t.Fatalf("the board does not show the hand-made row as manual: %q", manual)
	}
	engineered := d.mustRun("task", "list", "--manual", "false")
	if strings.Contains(engineered, taskID) {
		t.Fatalf("the engine filter let a manual row through: %q", engineered)
	}
	shown := d.mustRun("task", "show", taskID)
	if !strings.Contains(shown, "hand-made") || !strings.Contains(shown, "assignee: user:carlos") {
		t.Fatalf("show does not carry the hand-made truth: %q", shown)
	}

	// A bare name is a usage refusal: kind and name are separate ledger
	// columns, so the flag must say both.
	if out, code := runExit(t, d, "task", "create", "--component", repo, "--title", "x", "--type", "chore", "--assignee", "carlos"); code != 2 {
		t.Fatalf("--assignee without a kind exited %d, want 2: %q", code, out)
	}
	// --manual with a third answer is refused, not defaulted.
	if out, code := runExit(t, d, "task", "list", "--manual", "sometimes"); code != 2 {
		t.Fatalf("a third --manual answer exited %d, want 2: %q", code, out)
	}
}

func TestBriefShowRefusesWhatAPersonMustName(t *testing.T) {
	d := startDaemon(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	d.mustRun("loop", "create", "--slug", "contracted", "--component", repo, "--goal", "contract")

	// The loop exists; the brief does not — the sentence says WHICH loop
	// resolved, proving the slug (not an id) crossed the resolver.
	out, err := d.run("brief", "show", "--loop", "contracted")
	if err == nil || !strings.Contains(out, "no brief yet") {
		t.Fatalf("brief show on a briefless loop answered %q (%v)", out, err)
	}
	// An unknown slug is refused by name (DB19), not answered with nothing.
	if out, err := d.run("brief", "show", "--loop", "no-such-loop"); err == nil || !strings.Contains(out, "no-such-loop") {
		t.Fatalf("the unknown loop was not named in the refusal: %q", out)
	}
	if _, code := runExit(t, d, "brief", "show"); code != 2 {
		t.Fatalf("brief show without --loop should take the usage exit, got %d", code)
	}
}

func TestHelpMentionsEveryVerb(t *testing.T) {
	out := mustRunFree(t, "help")
	for _, want := range []string{
		"goal              list, show, create, submit, decide",
		"brief             show, decide",
		"task              list, show, create",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help lost a verb line: %q", want)
		}
	}
	// And the per-verb usage answers rather than the bare "unknown".
	for verb, want := range map[string]string{
		"goal":  "aeon goal — the org's intentions",
		"brief": "aeon brief — a loop's contract",
		"task":  "aeon task — the board",
	} {
		if out := mustRunFree(t, verb); !strings.Contains(out, want) {
			t.Errorf("`aeon %s` usage did not print its header: %q", verb, out)
		}
	}
}

// seedThroughBootWindow runs one direct store write against the DAEMON's
// live database, retrying the transient "database is locked" the fixture
// class is exposed to: localrpc binds in Init, so the socket answers while
// migrations and the boot seed sets still own the single writer — the write
// window S58 T5's Notes name (T6, 2026-11-03: five tools.core rows more
// widened it by five seed inserts, and TestQueueSettingsRoundTrip began
// losing to it — A/B on this machine 2026-11-03: base 0/10 green, T6 tree
// 4 fails in 11 runs, before this helper existed).
// The bounded client retry is the honest half: it waits out the writer
// WITHOUT touching boot order — unlike the reverted second-handle
// quiescence wait, which only hid this race by entering the seed-engine vs
// arrival-net race T5 records. Production's answer to that window stays
// daemon-side and rides T9/T10's named finding; a test helper tolerating a
// BUSY for seconds is not that fix and claims it is not.
//
// An attempt that times out on the writer is AMBIGUOUS — the row can have
// committed while the client saw the busy error (observed under the
// hammer's load), so a bare retry of CreateComponent collides with its own
// first attempt ("already registered"). The loop therefore treats that
// refusal as the committed outcome ONLY after at least one locked attempt;
// on attempt zero it is the real refusal it always was, and the test fails.
func seedThroughBootWindow(t *testing.T, what string, write func() error) {
	t.Helper()
	lockedOnce := false
	for attempt := 0; attempt < 60; attempt++ {
		err := write()
		if err == nil {
			return
		}
		if strings.Contains(err.Error(), "database is locked") {
			lockedOnce = true
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if lockedOnce && strings.Contains(err.Error(), "is already registered to a component") {
			return // the ambiguous attempt committed; the row is there
		}
		t.Fatalf("%s: %v", what, err) // a real refusal — retrying would hide it
	}
	t.Fatalf("%s: still locked after 30s of the boot write window", what)
}

// mustRunFree runs a verb that needs no daemon (help/usage answer offline).
func seedComponent(t *testing.T, stateDir, name, root string) {
	t.Helper()
	// The daemon's store lives under the state home's aeon/ subdirectory
	// (internal/paths) — opening the state home itself would seed a second,
	// private database the daemon never sees.
	s, err := store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon"), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	groupRoot := "/repo/board-group-" + name
	var p store.ComponentWrite
	seedThroughBootWindow(t, "seed root", func() error {
		var err error
		p, err = s.CreateComponent(store.NewComponent{Name: "board group " + name, Slug: "board-group-" + name, Root: groupRoot})
		if err == nil && !p.OK() {
			err = fmt.Errorf("write refused: %s", p.Reason)
		}
		return err
	})
	// The id a RETRY never returns (the adopting branch has no ComponentWrite):
	// read the row back by its root, which is exact either way.
	group, err := s.ComponentForRoot(groupRoot)
	if err != nil {
		t.Fatalf("seed root read-back: %v", err)
	}
	seedThroughBootWindow(t, "seed component", func() error {
		_, err := s.CreateComponent(store.NewComponent{Name: name, Slug: name, Root: root, ParentID: group.ID})
		return err
	})
}

// seedRoot files a root component by exact name — the "root" a goal hangs
// under in the component tree. The CLI has no project verb, so the test seeds
// the root through the store the daemon shares (the daemon's goal-create
// resolver reads roots by id or exact name).
func seedRoot(t *testing.T, d *daemonFixture, name string) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: filepath.Join(d.stateDir, "aeon"), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	seedThroughBootWindow(t, "seed root", func() error {
		_, err := s.CreateComponent(store.NewComponent{Name: name, Slug: slug, Root: "/repo/" + slug})
		return err
	})
}

func mustRunFree(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(aeonBinary, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("aeon %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// The P34 line the task's DoD draws in grep: these three files are socket
// clients, and the store does not appear in them.
func TestOrgVerbsNeverOpenTheStore(t *testing.T) {
	for _, name := range []string{"goal.go", "brief.go", "task.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "store.") || strings.Contains(string(raw), "internal/store") {
			t.Errorf("%s reaches for the store — these verbs speak the socket only (P34, unlike policy's PL15 exception)", name)
		}
	}
}

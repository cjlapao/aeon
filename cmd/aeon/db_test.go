package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// T11's tests, against a REAL daemon on a REAL store in a temp directory — not
// a mock. A CLI tested against a mock of its own store tests nothing about the
// store.

// replayRouteID is the route the fixture server answers on, and
// startDaemonWithReplayRoute is ask_test.go's harness under a name that says
// what it is for here: a recorded capture replayed by a local server, so these
// tests need no credential and touch no provider (S3's I7).
const replayRouteID = "anthropic"

func startDaemonWithReplayRoute(t *testing.T) *daemonFixture {
	t.Helper()
	d, _ := startDaemonWithRoute(t, "anthropic/text-only.sse")
	return d
}

func TestDBInfoPrintsThePathWALTheMigrationsAndTheTables(t *testing.T) {
	d := startDaemon(t)
	out := d.mustRun("db", "info")

	// The PATH FIRST (B10): every later slice's debugging starts by finding the
	// file, and the daemon prints the same path on startup so the two can be
	// compared without reading source.
	if !strings.Contains(out, d.stateDir) {
		t.Errorf("db info does not name the store path (%s):\n%s", d.stateDir, out)
	}
	if !strings.Contains(out, "state.db") {
		t.Errorf("db info does not name the file:\n%s", out)
	}
	if !strings.Contains(out, "wal") {
		t.Errorf("db info does not report the journal mode:\n%s", out)
	}
	if !strings.Contains(out, "foreign_keys ON") {
		t.Errorf("db info does not report foreign_keys, which is the pragma whose silent absence "+
			"stops every cascade:\n%s", out)
	}
	if !strings.Contains(out, "1 writer") {
		t.Errorf("db info does not report the pool shape (DB15):\n%s", out)
	}
	// The COUNT is asserted against store.Migrations() rather than a literal,
	// because a literal is a second place to update and the first one somebody
	// forgets. (It was `6` and this test failed the moment 0007 landed, which
	// is how this comment came to exist.)
	if !strings.Contains(out, fmt.Sprintf("%d migration(s) applied", len(store.Migrations()))) {
		t.Errorf("db info does not report %d migrations:\n%s", len(store.Migrations()), out)
	}
	latest := store.Migrations()[len(store.Migrations())-1].Name
	if !strings.Contains(out, latest) {
		t.Errorf("db info does not name the latest migration (%s):\n%s", latest, out)
	}
	for _, table := range []string{"step_message", "question", "chat_message", "event", "policy"} {
		if !strings.Contains(out, table) {
			t.Errorf("db info does not list the %s table:\n%s", table, out)
		}
	}
}

func TestDBInfoVectorsReportsTheStaticallyLinkedVersion(t *testing.T) {
	// The human-visible proof of DB18: because the extension is compiled in
	// rather than loaded, there is no extension-missing state for S4 or S22 to
	// handle — and this is what tells a reader there is nothing to install.
	d := startDaemon(t)
	out := d.mustRun("db", "info", "--vectors")
	if !strings.Contains(out, "sqlite-vec") || !strings.Contains(out, "statically linked") {
		t.Errorf("db info --vectors does not report the linked extension:\n%s", out)
	}
	if !strings.Contains(out, "no extension to install") {
		t.Errorf("db info --vectors does not say there is nothing to install, which is the point:\n%s", out)
	}
}

func TestDBInfoWorksWithTheDaemonDown(t *testing.T) {
	// "Where is the file" has to be answerable with the daemon DOWN. It is the
	// first question anybody asks when the daemon will not start, and routing
	// it through the daemon would make the answer unavailable exactly when it
	// is needed.
	base := t.TempDir()
	cmd := aeonCommand(t, base, "db", "info")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("db info failed with no daemon: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), filepath.Join(base, "aeon", "state.db")) {
		t.Errorf("db info did not resolve the store path from XDG_STATE_HOME:\n%s", out)
	}
	// And it CREATED the store, migrations and all — which is what makes the
	// answer trustworthy rather than a guess about where a file would go.
	if _, err := os.Stat(filepath.Join(base, "aeon", "state.db")); err != nil {
		t.Errorf("db info did not create the store it reported: %v", err)
	}
}

func TestDBUpgradeDryRunReportsNothingPendingAndSaysWhy(t *testing.T) {
	d := startDaemon(t)
	out := d.mustRun("db", "upgrade", "--dry-run")
	if !strings.Contains(out, "no pending migrations") {
		t.Errorf("db upgrade --dry-run does not report the state:\n%s", out)
	}
	// Saying that opening applied them is the difference between a true answer
	// and a misleading one.
	if !strings.Contains(out, "opening the store applies them") {
		t.Errorf("db upgrade --dry-run does not explain why nothing is pending:\n%s", out)
	}
}

func TestDBUpgradeWithoutDryRunRefusesAndSaysWhyThereIsNoApplyPath(t *testing.T) {
	d := startDaemon(t)
	out, err := d.run("db", "upgrade")
	if err == nil {
		t.Fatalf("db upgrade without --dry-run succeeded:\n%s", out)
	}
	if !strings.Contains(out, "run on every open") {
		t.Errorf("the refusal does not explain that there is deliberately no second way:\n%s", out)
	}
}

func TestDBQueryAwaitingPrintsTheSQLAndTheResult(t *testing.T) {
	// Anyone arriving from v1 will look for an `awaiting_user` column and not
	// find one. Showing the query is how they learn why it is derived (DB6).
	d := startDaemon(t)
	out := d.mustRun("db", "query", "awaiting")

	if !strings.Contains(out, "FROM question") {
		t.Errorf("db query awaiting does not print the SQL:\n%s", out)
	}
	if !strings.Contains(out, "answered_at IS NULL") {
		t.Errorf("the printed SQL does not show the derivation:\n%s", out)
	}
	if !strings.Contains(out, "no loops awaiting") {
		t.Errorf("db query awaiting does not print its answer:\n%s", out)
	}
	if !strings.Contains(out, "DERIVED") {
		t.Errorf("db query awaiting does not say the answer is derived rather than stored:\n%s", out)
	}
}

func TestLoopCreateAndListAndListIsScopedByDefault(t *testing.T) {
	// A daemon multiplexes every project on the machine, so an unscoped
	// default would show a person every repository's loops whenever they were
	// in one — and they would learn to ignore the list.
	d := startDaemon(t)
	repoA := t.TempDir()
	repoB := t.TempDir()

	if out := d.mustRun("loop", "create", "--slug", "refactor", "--component", repoA,
		"--goal", "split the store"); !strings.Contains(out, "created") {
		t.Fatalf("loop create did not report success:\n%s", out)
	}
	// THE SAME SLUG in a second project root. Two loops, not one (DB4).
	if out := d.mustRun("loop", "create", "--slug", "refactor", "--component", repoB); !strings.Contains(out, "created") {
		t.Fatalf("the same slug in a second project was refused:\n%s", out)
	}

	inA := d.mustRun("loop", "list", "--component", repoA)
	if !strings.Contains(inA, repoA) {
		t.Errorf("loop list does not show the loop in %s:\n%s", repoA, inA)
	}
	if strings.Contains(inA, repoB) {
		t.Errorf("loop list in %s showed %s's loops; the default must be scoped:\n%s", repoA, repoB, inA)
	}

	all := d.mustRun("loop", "list", "--all")
	if !strings.Contains(all, repoA) || !strings.Contains(all, repoB) {
		t.Errorf("loop list --all does not show both projects:\n%s", all)
	}
}

func TestLoopWriteSurfacesEachRefusalWithItsReason(t *testing.T) {
	// The demo's five refusals, each legible. A silent clamp would turn an
	// escape attempt into a successful write somewhere unexpected — and these
	// names come from a MODEL.
	d := startDaemon(t)
	repo := t.TempDir()
	d.mustRun("loop", "create", "--slug", "refusals", "--component", repo)

	cases := []struct {
		name  string
		needs string
	}{
		{"/etc/passwd", "absolute path"},
		{"../../../etc/passwd", ".. segment"},
		{"scratch/notes.exe", "unsupported file type"},
		{"secrets/key.md", "addressable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := d.run("loop", "write", "refusals", tc.name, "--content", "x", "--component", repo)
			if err == nil {
				t.Fatalf("%q was accepted:\n%s", tc.name, out)
			}
			// All three parts (S2's error shape), because a refused document
			// path has to say what to do.
			for _, part := range []string{"what", "means", "todo"} {
				if !strings.Contains(out, part) {
					t.Errorf("the refusal of %q is missing its %q part:\n%s", tc.name, part, out)
				}
			}
			if !strings.Contains(out, tc.needs) {
				t.Errorf("the refusal of %q does not name the specific reason (%q):\n%s", tc.name, tc.needs, out)
			}
		})
	}

	// An empty name goes through a different path — the flag parser sees no
	// positional argument — so it is checked separately rather than assumed.
	if out, err := d.run("loop", "write", "refusals", "", "--content", "x", "--component", repo); err == nil {
		t.Errorf("an empty document name was accepted:\n%s", out)
	}

	// Then the two successes, so this test cannot pass against a store that
	// refuses everything.
	if out := d.mustRun("loop", "write", "refusals", "plan", "--content", "# Plan\n",
		"--component", repo); !strings.Contains(out, "plan.md") {
		t.Errorf("a legitimate write did not report the file:\n%s", out)
	}
	if out := d.mustRun("loop", "write", "refusals", "tasks/T1.md", "--content", "# T1\n",
		"--component", repo); !strings.Contains(out, "rev 1") {
		t.Errorf("a legitimate path write did not report its revision:\n%s", out)
	}
}

func TestLoopWriteReportsAnUnchangedWriteRatherThanANewRevision(t *testing.T) {
	d := startDaemon(t)
	repo := t.TempDir()
	d.mustRun("loop", "create", "--slug", "same", "--component", repo)
	d.mustRun("loop", "write", "same", "plan", "--content", "identical", "--component", repo)
	out := d.mustRun("loop", "write", "same", "plan", "--content", "identical", "--component", repo)
	if !strings.Contains(out, "unchanged") {
		t.Errorf("writing the same content twice did not report `unchanged`:\n%s", out)
	}
}

func TestLoopReadReturnsTheContentAndTheManifest(t *testing.T) {
	d := startDaemon(t)
	repo := t.TempDir()
	d.mustRun("loop", "create", "--slug", "reading", "--component", repo)
	d.mustRun("loop", "write", "reading", "plan", "--content", "# The Plan\n", "--component", repo)
	d.mustRun("loop", "write", "reading", "tasks/T1.md", "--content", "# T1\n", "--component", repo)

	content := d.mustRun("loop", "read", "reading", "plan", "--component", repo)
	if !strings.Contains(content, "# The Plan") {
		t.Errorf("loop read did not return the content:\n%s", content)
	}

	manifest := d.mustRun("loop", "read", "reading", "--component", repo)
	if !strings.Contains(manifest, "plan.md") || !strings.Contains(manifest, "tasks/T1.md") {
		t.Errorf("loop read with no name did not list the documents:\n%s", manifest)
	}
	if strings.Contains(manifest, "# The Plan") {
		t.Errorf("the manifest inflated content:\n%s", manifest)
	}
}

func TestDebugHammerDrivesSustainedWritesAndReportsNoBusy(t *testing.T) {
	// The demo's concurrency step, in miniature. It opens the store directly —
	// a SECOND PROCESS writing while the daemon serves reads — which is the
	// whole claim being demonstrated.
	d := startDaemon(t)
	d.waitStartupQuiet() // the hammer is a second writer; see waitStartupQuiet
	out := d.mustRun("debug", "hammer", "--writes", "200", "--report-every", "100")

	if !strings.Contains(out, "200 writes in") {
		t.Errorf("hammer did not report its throughput:\n%s", out)
	}
	if !strings.Contains(out, "seq is now") {
		t.Errorf("hammer did not report the sequence it reached:\n%s", out)
	}
	if strings.Contains(strings.ToUpper(out), "BUSY") && !strings.Contains(out, "should have reported") {
		t.Errorf("hammer reported SQLITE_BUSY:\n%s", out)
	}

	// And the daemon can still read, WHILE the store has been written to by
	// another process. This is the two-process case `busy_timeout` and
	// `_txlock=immediate` exist for.
	if out := d.mustRun("db", "query", "awaiting"); !strings.Contains(out, "FROM question") {
		t.Errorf("a read after another process wrote failed:\n%s", out)
	}
}

func TestStepShowAndVerifyReportAMissingStepHonestly(t *testing.T) {
	// Step ids are never reused (AUTOINCREMENT), so an id that is not there
	// was never created or belongs to a different store — which is why the
	// remedy names the store path.
	d := startDaemon(t)
	out, err := d.run("step", "show", "9999")
	if err == nil {
		t.Fatalf("step show succeeded for a step that does not exist:\n%s", out)
	}
	if !strings.Contains(out, "no such step") {
		t.Errorf("the failure does not say what is wrong:\n%s", out)
	}
	if !strings.Contains(out, "db info") {
		t.Errorf("the failure does not say how to find the store:\n%s", out)
	}
}

func TestStepShowVerifyAndListReadBackASavedStep(t *testing.T) {
	// `ask --save-step` is the bridge between S3 and S4, so this drives the
	// whole chain: a recorded provider replays a reply, the daemon saves it,
	// and the three step verbs read it back.
	d := startDaemonWithReplayRoute(t)
	repo := t.TempDir()

	out := d.mustRun("ask", "--route", replayRouteID, "--save-step",
		"--loop", "saved", "--component", repo, "what is 17 times 23?")
	stepID := extractStepID(t, out)

	show := d.mustRun("step", "show", strconv.FormatInt(stepID, 10))
	if !strings.Contains(show, "saved") {
		t.Errorf("step show does not name the loop:\n%s", show)
	}
	if !strings.Contains(show, "USER") || !strings.Contains(show, "ASSISTANT") {
		t.Errorf("step show does not render the transcript:\n%s", show)
	}
	if !strings.Contains(show, "cost") {
		t.Errorf("step show does not report the cost:\n%s", show)
	}

	asJSON := d.mustRun("step", "show", strconv.FormatInt(stepID, 10), "--json")
	if !strings.Contains(asJSON, `"kind"`) || !strings.Contains(asJSON, `"role"`) {
		t.Errorf("step show --json did not print the canonical transcript:\n%s", asJSON)
	}
	// The canonical shape, not a provider's. A `choices` key here would mean
	// something in S3 leaked past the adapter.
	for _, provider := range []string{"choices", "tool_calls", "content_block"} {
		if strings.Contains(asJSON, provider) {
			t.Errorf("step show --json printed a provider shape (%s):\n%s", provider, asJSON)
		}
	}

	verify := d.mustRun("step", "verify", strconv.FormatInt(stepID, 10))
	if !strings.Contains(verify, "transcript verified") {
		t.Errorf("step verify did not verify a transcript this build just wrote:\n%s", verify)
	}
	// It says so explicitly when there is no signature, rather than printing
	// nothing and letting a reader believe one was checked.
	if !strings.Contains(verify, "signature") {
		t.Errorf("step verify says nothing about the signature:\n%s", verify)
	}

	list := d.mustRun("step", "list", "saved", "--component", repo)
	if !strings.Contains(list, strconv.FormatInt(stepID, 10)) {
		t.Errorf("step list does not include the saved step:\n%s", list)
	}
}

func TestASavedStepSurvivesADaemonRestart(t *testing.T) {
	// The demo's step 2, as a test. The daemon is stopped and started on the
	// SAME state directory, and the step is still there — which is the whole
	// of "a suspended step exits its process and resumes cold" (P2), minus the
	// runtime.
	d := startDaemonWithReplayRoute(t)
	repo := t.TempDir()

	out := d.mustRun("ask", "--route", replayRouteID, "--save-step",
		"--loop", "survives", "--component", repo, "remember this")
	stepID := extractStepID(t, out)

	digestBefore := d.mustRun("step", "verify", strconv.FormatInt(stepID, 10))

	// Stop it, start a new one on the same state directory.
	d.restart(t)

	digestAfter := d.mustRun("step", "verify", strconv.FormatInt(stepID, 10))
	if !strings.Contains(digestAfter, "transcript verified") {
		t.Fatalf("the transcript does not verify after a restart:\n%s", digestAfter)
	}
	if digestBefore != digestAfter {
		t.Errorf("the transcript changed across a restart:\n  before %s\n  after  %s",
			digestBefore, digestAfter)
	}
}

func TestSeqContinuesAcrossADaemonRestart(t *testing.T) {
	// **The gap S2 admitted**, closed. S2's demo told the reader `seq` would
	// reset; this is the test that says it does not.
	d := startDaemon(t)

	// Drive some events, then read the head from the store.
	d.mustRun("ping")
	d.mustRun("ping")
	before := headSeq(t, d)
	if before == 0 {
		t.Fatal("no events were recorded, so this test cannot say anything about seq")
	}

	d.restart(t)

	// A ping on the NEW daemon allocates the next seq.
	d.mustRun("ping")
	after := headSeq(t, d)

	if after <= before {
		t.Errorf("seq did not advance across the restart: %d then %d", before, after)
	}
	if after < before+1 {
		t.Errorf("seq went backwards across a restart: %d then %d. In S2 this restarted at 1, and a "+
			"cursor that silently redelivers is worse than one that admits it resets", before, after)
	}
}

// headSeq reads the store's event head with a second handle, which is also a
// small two-process check: the daemon holds the file and this reads it.
func headSeq(t *testing.T, d *daemonFixture) int64 {
	t.Helper()
	out := d.mustRun("debug", "hammer", "--writes", "1", "--report-every", "0")
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "seq is now "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
			if err != nil {
				t.Fatalf("could not read the seq from %q: %v", line, err)
			}
			return n
		}
	}
	t.Fatalf("no seq line in:\n%s", out)
	return 0
}

func extractStepID(t *testing.T, out string) int64 {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "step ") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		if n, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			return n
		}
	}
	t.Fatalf("no step id in:\n%s", out)
	return 0
}

// --- route persistence (S4, closing S3's promise) -----------------------------

func TestARouteSetThroughTheDaemonSurvivesARestart(t *testing.T) {
	// The user's report, as a test: "it still says nothing is saved, and it
	// indeed does not get persisted".
	//
	// S3 added route management with `persisted: false` and the app said
	// "persistence arrives with the store (S4)". S4 shipped without it. This
	// drives the whole path — set, restart, read back — through the real
	// daemon and the real store.
	d := startDaemon(t)

	set := d.mustRun("routes", "set", "--id", "survivor", "--provider", "anthropic",
		"--model", "claude-sonnet-5", "--max-tokens", "9001", "--no-price")
	if !strings.Contains(set, "survivor") {
		t.Fatalf("routes set did not report the route:\n%s", set)
	}

	before := d.mustRun("routes", "list")
	if !strings.Contains(before, "survivor") {
		t.Fatalf("the route is not listed before the restart:\n%s", before)
	}

	d.restart(t)

	after := d.mustRun("routes", "list")
	if !strings.Contains(after, "survivor") {
		t.Errorf("THE ROUTE VANISHED ON RESTART, which is the bug this test exists for:\n%s", after)
	}
	if !strings.Contains(after, "claude-sonnet-5") {
		t.Errorf("the route came back without its model:\n%s", after)
	}
	if !strings.Contains(after, "9001") {
		t.Errorf("the route came back without its output cap:\n%s", after)
	}
}

func TestAStoredRouteReportsItselfAsPersistedAndStored(t *testing.T) {
	// `persisted` is what the app prints, and `source` is what it shows beside
	// each route. Both were wrong before: `persisted` was hard-coded false.
	d := startDaemon(t)
	d.mustRun("routes", "set", "--id", "stored-one", "--provider", "openai", "--model", "gpt-5", "--no-price")

	out := d.mustRun("routes", "list")
	if !strings.Contains(out, "stored") {
		t.Errorf("a route written through to the store does not report its source as `stored`:\n%s", out)
	}
	// And the environment's own routes still say `environment`, because the
	// precedence is "what you typed wins" and a reader needs to see which is
	// which.
	if strings.Contains(out, "runtime") {
		t.Errorf("a route reports `runtime` on a daemon that has a store; that source means "+
			"in-memory-only and should now be unreachable:\n%s", out)
	}
}

func TestACredentialIsNeverWrittenIntoTheDatabase(t *testing.T) {
	// C17, end to end: the environment or a dedicated file path, never a
	// config file. Asserted on the database's RAW BYTES after a real
	// `routes set --api-key-file`, because a key could reach a column that is
	// not `route`'s — a prices blob, an event payload, a document.
	d := startDaemon(t)

	const secret = "sk-ant-api03-END-TO-END-MUST-NOT-BE-IN-THE-DB"
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	d.mustRun("routes", "set", "--id", "keyed", "--provider", "anthropic",
		"--model", "claude-sonnet-5", "--api-key-file", keyFile, "--no-price")

	// The key must be USABLE — `hasKey` true — or this test would pass against
	// a daemon that simply threw it away.
	list := d.mustRun("routes", "list")
	if !strings.Contains(list, "keyed") {
		t.Fatalf("the route was not stored:\n%s", list)
	}
	if strings.Contains(list, secret) {
		t.Errorf("`routes list` PRINTED the credential:\n%s", list)
	}

	// Checkpoint, so the bytes are in the main file rather than only the WAL,
	// then scan every file in the state directory except the credential itself.
	d.restart(t)

	var scanned int
	err := filepath.WalkDir(d.stateDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), "/credentials/") {
			// The one place it is SUPPOSED to be. Checked rather than skipped:
			// if it is not here, the key was lost and `hasKey` is lying.
			if !strings.Contains(string(raw), secret) {
				t.Errorf("%s does not hold the credential it is for", path)
			}
			if info, statErr := entry.Info(); statErr == nil && info.Mode().Perm() != 0o600 {
				t.Errorf("%s is %04o, want 0600", path, info.Mode().Perm())
			}
			return nil
		}
		scanned++
		if strings.Contains(string(raw), secret) {
			t.Errorf("the credential is in %s — C17 allows the environment or a dedicated file path, "+
				"and a key in the database is one that gets copied with a backup and migrated by "+
				"every future schema change", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned == 0 {
		t.Fatal("no files were scanned, so this test checked nothing")
	}

	// And after the restart the route is still usable, key included.
	after := d.mustRun("routes", "list")
	if !strings.Contains(after, "keyed") {
		t.Errorf("the keyed route vanished on restart:\n%s", after)
	}
	if strings.Contains(after, secret) {
		t.Errorf("`routes list` printed the credential after a restart:\n%s", after)
	}
}

func TestDeletingARouteRemovesItsCredentialToo(t *testing.T) {
	// A key left behind for a route nobody can call is a secret on disk with no
	// owner — and on the next `routes set` of the same id it would come back as
	// "the existing key", which is a credential resurrected from a deleted
	// route.
	d := startDaemon(t)

	const secret = "sk-ant-api03-DELETE-WITH-THE-ROUTE"
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	d.mustRun("routes", "set", "--id", "doomed", "--provider", "openai",
		"--model", "gpt-5", "--api-key-file", keyFile, "--no-price")

	keyPath := filepath.Join(d.stateDir, "aeon", "credentials", "doomed.key")
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("the credential was not written where it should be: %v", err)
	}

	d.mustRun("routes", "delete", "--id", "doomed")

	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Errorf("%s still exists after the route was deleted", keyPath)
	}
	d.restart(t)
	if out := d.mustRun("routes", "list"); strings.Contains(out, "doomed") {
		t.Errorf("a deleted route came back after a restart:\n%s", out)
	}
}

// TestStepShowInputPrintsTheDispatchInput — S17 T3's CLI half. §2 of the
// S17 demo asks "is the rejection's text in the re-entered step's input?"
// and `--input` answers it from the ROW: the first user message, verbatim,
// and nothing of the assistant's. A check that needed the whole transcript
// rendered would invite reading the reply as if it were the ask.
func TestStepShowInputPrintsTheDispatchInput(t *testing.T) {
	d := startDaemonWithReplayRoute(t)
	repo := t.TempDir()
	const prompt = "redo of \"verify\": retry 1 of 2 — json output omits 'generated_at'"
	out := d.mustRun("ask", "--route", replayRouteID, "--save-step",
		"--loop", "redo-input", "--component", repo, prompt)
	stepID := extractStepID(t, out)

	in := d.mustRun("step", "show", strconv.FormatInt(stepID, 10), "--input")
	// The sharpest honest assertion: the printed input IS the dispatch
	// input, verbatim — not a rendering of the whole transcript.
	if got := strings.TrimSpace(in); got != prompt {
		t.Errorf("--input printed the wrong thing:\ngot:  %q\nwant: %q", got, prompt)
	}
	// The role headers of the full render must not appear: this verb
	// answers "what was asked", and printing the reply beside it would let
	// a checker mistake the answer for the request.
	if strings.Contains(in, "USER") || strings.Contains(in, "ASSISTANT") {
		t.Errorf("--input printed the transcript render instead of the input:\n%s", in)
	}
}

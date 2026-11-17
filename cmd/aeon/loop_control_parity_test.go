package main

// loop_control_parity_test.go — S55 T11: the P5 parity, both directions, and
// the proofs that keep the CLI a THIN client. A capability the daemon can do
// and the CLI cannot reach is not finished (P34); a verb that names no method
// is a door with no back. The fixture is T0's table: every control-plane
// method and the verb that reaches it. loop.requestStop is deliberately
// ABSENT — it is the agent's act (T10), not a person's CLI door.

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// controlMethodNames is the control family's methods, read from the
// protocol's own constants, so a method that lands on the socket is seen
// here at once.
func controlMethodNames() []protocol.MethodName {
	return []protocol.MethodName{
		protocol.MethodLoopStop,
		protocol.MethodLoopResume,
		protocol.MethodLoopCancel,
		protocol.MethodLoopControl,
		protocol.MethodLoopControlRequests,
		protocol.MethodLoopControlDismiss,
		protocol.MethodLoopRaise,
		protocol.MethodBudgetList,
		protocol.MethodBudgetShow,
		protocol.MethodBudgetEffective,
		protocol.MethodBudgetSet,
		protocol.MethodBudgetRemove,
		protocol.MethodBudgetVocabulary,
		protocol.MethodSpendReport,
		protocol.MethodWatchdogList,
		protocol.MethodWatchdogShow,
		protocol.MethodWatchdogVocabulary,
		protocol.MethodWatchdogSet,
		protocol.MethodWatchdogSetEnabled,
		protocol.MethodWatchdogRemove,
		protocol.MethodStepStop,
		protocol.MethodStepContinue,
	}
}

// controlParity is T0's table: each control-plane method, and the CLI verb
// (or verbs) that reaches it. watchdog enable and disable share one method;
// budget's bare usage reads budget.vocabulary, so "budget" is its verb.
var controlParity = map[protocol.MethodName][]string{
	protocol.MethodLoopStop:            {"loop stop"},
	protocol.MethodLoopResume:          {"loop resume"},
	protocol.MethodLoopCancel:          {"loop cancel"},
	protocol.MethodLoopControl:         {"loop control"},
	protocol.MethodLoopControlRequests: {"loop requests"},
	protocol.MethodLoopControlDismiss:  {"loop dismiss"},
	protocol.MethodLoopRaise:           {"loop raise"},
	protocol.MethodBudgetList:          {"budget list"},
	protocol.MethodBudgetShow:          {"budget show"},
	protocol.MethodBudgetEffective:     {"budget effective"},
	protocol.MethodBudgetSet:           {"budget set"},
	protocol.MethodBudgetRemove:        {"budget remove"},
	protocol.MethodBudgetVocabulary:    {"budget"},
	protocol.MethodSpendReport:         {"budget spend"},
	protocol.MethodWatchdogList:        {"watchdog list"},
	protocol.MethodWatchdogShow:        {"watchdog show"},
	protocol.MethodWatchdogVocabulary:  {"watchdog vocabulary"},
	protocol.MethodWatchdogSet:         {"watchdog set"},
	protocol.MethodWatchdogSetEnabled:  {"watchdog enable", "watchdog disable"},
	protocol.MethodWatchdogRemove:      {"watchdog remove"},
	protocol.MethodStepStop:            {"step stop"},
	protocol.MethodStepContinue:        {"step continue"},
}

// TestEveryControlMethodHasAVerb is one direction of P5: a socket method
// nobody can reach from the terminal is a capability that exists only on the
// wire.
func TestEveryControlMethodHasAVerb(t *testing.T) {
	if len(controlParity) != len(controlMethodNames()) {
		t.Fatalf("the parity table holds %d methods, the family has %d — one of them has no verb",
			len(controlParity), len(controlMethodNames()))
	}
	for _, m := range controlMethodNames() {
		verbs, ok := controlParity[m]
		if !ok || len(verbs) == 0 {
			t.Errorf("control method %s has no CLI verb", m)
		}
	}
}

// TestEveryControlVerbHasAMethod is the other direction of P5: a verb filed
// under no method of the family is a door with no back.
func TestEveryControlVerbHasAMethod(t *testing.T) {
	family := make(map[protocol.MethodName]bool, len(controlMethodNames()))
	for _, m := range controlMethodNames() {
		family[m] = true
	}
	for m, verbs := range controlParity {
		if !family[m] {
			t.Errorf("the verbs %v are filed under %s, which is not in the control family", verbs, m)
			continue
		}
		for _, v := range verbs {
			if strings.TrimSpace(v) == "" {
				t.Errorf("method %s has an empty verb", m)
			}
		}
	}
}

// controlVerbDispatch names each parity verb's CLI arguments and the fixed
// sentence a dead endpoint answers. Running every one of them against a dead
// endpoint proves the DISPATCH exists — a missing case reads as "unknown
// subcommand" or a prompt, never as the fixed sentence.
var controlVerbDispatch = []struct {
	verb     string
	args     []string
	sentence string
}{
	{"loop stop", []string{"loop", "stop", "1"}, noDaemonStopSentence},
	{"loop resume", []string{"loop", "resume", "1"}, noDaemonStopSentence},
	{"loop control", []string{"loop", "control", "1"}, noDaemonStopSentence},
	{"loop cancel", []string{"loop", "cancel", "1", "--reason", "r"}, noDaemonStopSentence},
	{"loop raise", []string{"loop", "raise", "1", "--usd", "1", "--reason", "r"}, noDaemonStopSentence},
	{"loop requests", []string{"loop", "requests"}, noDaemonStopSentence},
	{"loop dismiss", []string{"loop", "dismiss", "1"}, noDaemonStopSentence},
	{"budget list", []string{"budget", "list"}, noDaemonGuardSentence},
	{"budget show", []string{"budget", "show", "b1"}, noDaemonGuardSentence},
	{"budget effective", []string{"budget", "effective", "--loop", "1"}, noDaemonGuardSentence},
	{"budget set", []string{"budget", "set", "--scope", "s", "--subject", "x", "--window", "w", "--usd", "1"}, noDaemonGuardSentence},
	{"budget remove", []string{"budget", "remove", "b1"}, noDaemonGuardSentence},
	{"budget", []string{"budget"}, noDaemonGuardSentence},
	{"budget spend", []string{"budget", "spend"}, noDaemonGuardSentence},
	{"watchdog list", []string{"watchdog", "list"}, noDaemonGuardSentence},
	{"watchdog show", []string{"watchdog", "show", "r1"}, noDaemonGuardSentence},
	{"watchdog vocabulary", []string{"watchdog", "vocabulary"}, noDaemonGuardSentence},
	{"watchdog set", []string{"watchdog", "set", "--scope", "s", "--subject", "x", "--signal", "g", "--threshold", "1", "--action", "a"}, noDaemonGuardSentence},
	{"watchdog enable", []string{"watchdog", "enable", "r1"}, noDaemonGuardSentence},
	{"watchdog disable", []string{"watchdog", "disable", "r1"}, noDaemonGuardSentence},
	{"watchdog remove", []string{"watchdog", "remove", "r1"}, noDaemonGuardSentence},
	{"step stop", []string{"step", "stop", "1"}, noDaemonStopSentence},
	{"step continue", []string{"step", "continue", "1"}, noDaemonStopSentence},
}

func TestControlVerbDispatch(t *testing.T) {
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(t.TempDir(), "nothing.sock"))
	for _, tc := range controlVerbDispatch {
		var buf strings.Builder
		var err error
		switch tc.args[0] {
		case "loop":
			err = cmdLoop(&buf, tc.args[1:])
		case "step":
			err = cmdStep(&buf, tc.args[1:])
		case "budget":
			err = cmdBudget(&buf, tc.args[1:])
		case "watchdog":
			err = cmdWatchdog(&buf, tc.args[1:])
		default:
			t.Fatalf("unrouted verb family %q", tc.args[0])
		}
		if err == nil {
			t.Errorf("verb %q succeeded with no daemon — it should answer the fixed sentence", tc.verb)
			continue
		}
		if !strings.Contains(err.Error(), tc.sentence) {
			t.Errorf("verb %q answers %q, want the fixed sentence %q", tc.verb, err, tc.sentence)
		}
		if strings.Contains(err.Error(), "unknown") {
			t.Errorf("verb %q reads as an unknown subcommand — the dispatch case is missing: %v", tc.verb, err)
		}
	}
}

// --- the test daemon's own wire client ----------------------------------------
//
// socketCall dials the test daemon IN-PROCESS and makes one call. It is the
// test's copy of the wire, so the CLI's bytes can be compared against the
// wire's (TestJSONIsTheWire) and its state asserted (the --yes tests) without
// a second formatter or a second truth.

func socketCall(t *testing.T, endpoint string, method protocol.MethodName, params, res any) {
	t.Helper()
	t.Setenv("LOCALRPC_ENDPOINT_PATH", endpoint)
	c, err := dial(0)
	if err != nil {
		t.Fatalf("dialing the test daemon: %v", err)
	}
	defer c.Close()
	if err := c.Call(method, params, res); err != nil {
		t.Fatalf("socket %s: %v", method, err)
	}
}

// controlLoopFixture is a running daemon plus a control-eligible loop in its
// store, and the id of a real step row belonging to that loop.
//
// `loop create` writes only the loop row, and every control door reads the
// PIN — so a loop-create loop answers "not in this store" to all of them (the
// T0 finding). The fixture pins the loop the way a started loop is pinned:
// one store transaction (loop row, pin row, run 1), no runner, no execution.
// The loop row's own status is "active" — the state the tests' verdicts and
// refusal sentences are written against (CheckStop/Resume/Cancel/Raise all
// refuse it with their own one-sentence-per-state).
//
// The seeding happens BEFORE the daemon starts: the store is opened in-process
// on a fresh state directory, the pin and the step are committed, the store
// closes, and only then does the daemon open that same directory. Seeding
// after the daemon is up was racy — the daemon's connection snapshot did not
// always see a pin written by a second connection, which read as -32602
// "no loop" on the second test of a run. Seeding before start makes the pin
// part of the database the daemon opens, so there is no race to lose.
func controlLoopFixture(t *testing.T, slug string) (*daemonFixture, string, int64) {
	t.Helper()
	// 2026-11-02 (2026-11-02 merge reconciliation, S55 T11): this fixture's
	// tests end on in-process socket dials, and the in-process dial path
	// (dial.go, appenv.Apply) leaves the three store-location variables
	// process-wide, pointing at this test's state directory. The store
	// resolver honours those variables before XDG_STATE_HOME, so the next
	// test in the package — whatever its own state directory is — would
	// otherwise open this test's (already deleted) database. S45's
	// in-process door helper restores only the keys present in its fixture
	// env, and the store keys are stripped from that env, so a leaked key
	// outranks it (measured: the door store opened the deleted path and the
	// proposal row landed in the wrong store, a -31844 decide refusal).
	// Save the pre-test state of the three keys here and restore it when
	// the test is done; t.Cleanup runs LIFO, so registered first this
	// restoration runs last — after every in-process dial the test makes.
	for _, key := range []string{
		"DATABASE_SQLITE_STORAGE_PATH",
		"DATABASE_SQLITE_FILE_NAME",
		"DATABASE_SQLITE_DSN",
	} {
		if v, ok := os.LookupEnv(key); ok {
			t.Cleanup(func() { os.Setenv(key, v) })
		} else {
			t.Cleanup(func() { os.Unsetenv(key) })
		}
	}
	stateDir := t.TempDir()
	s, err := store.Open(store.Options{Dir: filepath.Join(stateDir, "aeon"), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("opening the seed store: %v", err)
	}
	// The pin's content must be a parseable blueprint (FromPinned loads it on
	// every control op): schemaVersion 1, one lane, one model step on the
	// ad-hoc preset — the daemon's own lcBlueprint shape, unpinned.
	bp := "id: s55-pin\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: only\n    step: {kind: model-step, agent: default}\n"
	l, err := s.CreatePinnedLoop(
		store.NewLoop{Slug: slug, ComponentRoot: "/repo/s55", Title: slug, Goal: "a loop for the S55 control verbs"},
		"s55-pin", 1, bp, "{}")
	if err != nil {
		t.Fatalf("seeding the pin: %v", err)
	}
	// A REAL step row for the step-continue assertion: step 999999 (no row)
	// answers "state could not be read" (-31302), not the refusal sentence.
	// "done" is not "suspended", so step.continue refuses it with the pinned
	// one-liner; and recovery scans only status='running', so a done row is
	// invisible to the daemon's startup walk.
	st, err := s.CreateStep(
		store.Scope{LoopSlug: slug, ComponentRoot: "/repo/s55"},
		store.NewStep{Agent: "s55-fixture", Status: store.StepDone})
	if err != nil {
		t.Fatalf("seeding the step: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("closing the seed store: %v", err)
	}
	d := startDaemonInStateDir(t, stateDir)
	// The daemon seeds its inert sets (the system watchdog rules) in its
	// startup pass, and that pass runs AFTER the socket is live — the
	// endpoint binds at Init, before recovery finishes — so a read made
	// right after the ping can race the seed and see an empty rule table.
	// Measured on this box: the fixture daemon's first list at ~1.5s saw 0
	// rules, the next at ~2s saw all 5. The fixture therefore waits for the
	// seed to be visible before returning: a fixture that returns before
	// the daemon's own startup completes hands its tests a race to lose.
	deadline := time.Now().Add(20 * time.Second)
	for {
		var res protocol.WatchdogListResult
		socketCall(t, d.endpoint, protocol.MethodWatchdogList, protocol.WatchdogListParams{}, &res)
		if len(res.Rules) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon's startup seed never became visible\n--- daemon log ---\n%s", d.daemonLog())
		}
		time.Sleep(100 * time.Millisecond)
	}
	return d, l.ID, st.ID
}

func socketBudgetIDForSubject(t *testing.T, endpoint, subjectID string) string {
	t.Helper()
	var res protocol.BudgetListResult
	socketCall(t, endpoint, protocol.MethodBudgetList, protocol.BudgetListParams{SubjectID: subjectID}, &res)
	if len(res.Budgets) != 1 {
		t.Fatalf("expected exactly one budget for subject %s, got %d", subjectID, len(res.Budgets))
	}
	return res.Budgets[0].ID
}

func socketSystemWatchdogRule(t *testing.T, endpoint string) string {
	t.Helper()
	var res protocol.WatchdogListResult
	socketCall(t, endpoint, protocol.MethodWatchdogList, protocol.WatchdogListParams{}, &res)
	for _, r := range res.Rules {
		if r.Origin == "system" && r.Enabled {
			return r.ID
		}
	}
	t.Fatalf("no enabled system watchdog rule among %d rules", len(res.Rules))
	return ""
}

func assertLoopStatus(t *testing.T, endpoint, id, want string) {
	t.Helper()
	var res protocol.LoopControlResult
	socketCall(t, endpoint, protocol.MethodLoopControl, protocol.LoopControlParams{ID: id}, &res)
	if res.Status != want {
		t.Fatalf("loop %s has status %q, want %q", id, res.Status, want)
	}
}

func assertBudgetUSDCap(t *testing.T, endpoint, budgetID string, want float64) {
	t.Helper()
	var res protocol.BudgetListResult
	socketCall(t, endpoint, protocol.MethodBudgetList, protocol.BudgetListParams{}, &res)
	for _, b := range res.Budgets {
		if b.ID != budgetID {
			continue
		}
		for _, d := range b.Dimensions {
			if d.Dimension == "usd" && d.Cap != nil && *d.Cap == want {
				return
			}
		}
		t.Fatalf("budget %s has no usd cap of %v", budgetID, want)
	}
	t.Fatalf("budget %s is not in the list", budgetID)
}

func assertBudgetPresent(t *testing.T, endpoint, budgetID string, want bool) {
	t.Helper()
	var res protocol.BudgetListResult
	socketCall(t, endpoint, protocol.MethodBudgetList, protocol.BudgetListParams{}, &res)
	found := false
	for _, b := range res.Budgets {
		if b.ID == budgetID {
			found = true
		}
	}
	if found != want {
		t.Fatalf("budget %s present=%v, want %v", budgetID, found, want)
	}
}

func assertWatchdogEnabled(t *testing.T, endpoint, id string, want bool) {
	t.Helper()
	var res protocol.WatchdogShowResult
	socketCall(t, endpoint, protocol.MethodWatchdogShow, protocol.WatchdogShowParams{ID: id}, &res)
	if res.Rule.Enabled != want {
		t.Fatalf("watchdog rule %s enabled=%v, want %v", id, res.Rule.Enabled, want)
	}
}

// --- the tests ------------------------------------------------------------------

// TestLoopStopResumeCancelOverASocketDaemon is the end-to-end proof that each
// verb is wired to its own method: a real daemon, a real loop, and the
// daemon's own answers — a verb wired to the wrong method cannot produce the
// sentence the right one gets.
func TestLoopStopResumeCancelOverASocketDaemon(t *testing.T) {
	d, id, stepID := controlLoopFixture(t, "s55-t11-e2e")
	step := strconv.FormatInt(stepID, 10)

	// The read: status, and the four eligibility verdicts, all present.
	out := d.mustRun("loop", "control", id)
	if !strings.Contains(out, "loop "+id+" — active") {
		t.Fatalf("loop control did not print the loop's status:\n%s", out)
	}
	for _, act := range []string{"stop", "resume", "cancel", "raise"} {
		if !strings.Contains(out, act) {
			t.Errorf("loop control is missing the %s verdict:\n%s", act, out)
		}
	}

	// A stop of work that is not running is the daemon's refusal, non-zero,
	// with the daemon's sentence.
	out, err := d.run("loop", "stop", id)
	if err == nil {
		t.Fatalf("loop stop on an active loop succeeded:\n%s", out)
	}
	if !strings.Contains(out, "the loop is active — it is finished; a stop has nothing to do") {
		t.Errorf("loop stop did not answer the daemon's sentence:\n%s", out)
	}

	// A resume that does not move carries the refusal IN THE RESULT — exit 0,
	// the daemon's sentence as the daemon wrote it.
	out = d.mustRun("loop", "resume", id)
	if !strings.Contains(out, "not resumed") ||
		!strings.Contains(out, "the loop is active — it has no control to apply") {
		t.Errorf("loop resume did not carry the daemon's refusal in the result:\n%s", out)
	}

	// A cancel without --yes prints the state and stops: non-zero, no act.
	out, err = d.run("loop", "cancel", id, "--reason", "the test is over")
	if err == nil {
		t.Fatalf("loop cancel without --yes succeeded:\n%s", out)
	}
	if !strings.Contains(out, "re-run with --yes to apply") {
		t.Errorf("loop cancel without --yes did not name the seatbelt:\n%s", out)
	}

	// With --yes the act goes out — and the daemon refuses an active loop
	// with its own sentence (the seatbelt and the engine are different doors).
	out, err = d.run("loop", "cancel", id, "--reason", "the test is over", "--yes")
	if err == nil {
		t.Fatalf("loop cancel --yes on an active loop succeeded:\n%s", out)
	}
	if !strings.Contains(out, "the loop is active — it has no control to apply") {
		t.Errorf("loop cancel --yes did not answer the daemon's sentence:\n%s", out)
	}
	assertLoopStatus(t, d.endpoint, id, "active")

	// The desk is empty, and says so as the desk's answer.
	out = d.mustRun("loop", "requests")
	if !strings.Contains(out, "no stop requests match") {
		t.Errorf("loop requests did not say the desk is empty:\n%s", out)
	}

	// step stop of a step that is not running: exit 0, the honest answer.
	out = d.mustRun("step", "stop", step)
	if !strings.Contains(out, "not stopped") {
		t.Errorf("step stop did not say the step was not running:\n%s", out)
	}

	// step continue of a step that is not paused: refused, non-zero, with the
	// daemon's sentence. A real row is required — a step id with no row
	// answers "state could not be read" (-31302), not this refusal.
	out, err = d.run("step", "continue", step)
	if err == nil {
		t.Fatalf("step continue on a non-paused step succeeded:\n%s", out)
	}
	if !strings.Contains(out, "step.continue only resumes a step a person stopped") {
		t.Errorf("step continue did not answer the daemon's sentence:\n%s", out)
	}
}

// TestRefusalsPrintedVerbatim is the anti-re-wording guard: the daemon's
// sentence, from its own refusal, must appear on the person's screen UNCHANGED
// — a CLI that re-validates or re-words is a second truth.
func TestRefusalsPrintedVerbatim(t *testing.T) {
	d, id, _ := controlLoopFixture(t, "s55-t11-verbatim")

	out, err := d.run("loop", "stop", id)
	if err == nil {
		t.Fatalf("loop stop succeeded; wanted the refusal")
	}
	if !strings.Contains(out, "loop stop refused: the loop is active — it is finished; a stop has nothing to do") {
		t.Errorf("the daemon's stop refusal was re-worded by the CLI:\n%s", out)
	}
	if !strings.Contains(out, "-31860") {
		t.Errorf("the refusal did not carry its code:\n%s", out)
	}

	out, err = d.run("loop", "cancel", id, "--reason", "verbatim", "--yes")
	if err == nil {
		t.Fatalf("loop cancel succeeded; wanted the refusal")
	}
	if !strings.Contains(out, "loop cancel refused: the loop is active — it has no control to apply") {
		t.Errorf("the daemon's cancel refusal was re-worded by the CLI:\n%s", out)
	}
}

// TestJSONIsTheWire is the anti-second-formatter guard: --json must print the
// wire result's BYTES — compared against the same call made over the socket
// in-process, so a re-encode or a pretty-print is a diff the test sees.
func TestJSONIsTheWire(t *testing.T) {
	d, id, _ := controlLoopFixture(t, "s55-t11-json")

	out := d.mustRun("loop", "control", id, "--json")
	var raw json.RawMessage
	socketCall(t, d.endpoint, protocol.MethodLoopControl, protocol.LoopControlParams{ID: id}, &raw)
	got := strings.TrimRight(out, "\n")
	if got != string(raw) {
		t.Fatalf("--json is not the wire bytes:\nCLI:   %s\nwire:  %s", got, raw)
	}
}

// TestYesPrintsTheStatementFirst is the seatbelt's proof, for every loosening
// door: the daemon's statement is printed BEFORE the act, and without --yes
// nothing that changes state is written — asserted against the wire's own
// state, not the CLI's claim.
func TestYesPrintsTheStatementFirst(t *testing.T) {
	d, id, _ := controlLoopFixture(t, "s55-t11-yes")

	// A TIGHTENING set commits directly: no statement, no --yes needed.
	out := d.mustRun("budget", "set", "--scope", "loop", "--subject", id, "--window", "lifetime", "--usd", "10")
	if strings.Contains(out, "re-run with --yes") {
		t.Errorf("a tightening set demanded confirmation:\n%s", out)
	}
	budgetID := socketBudgetIDForSubject(t, d.endpoint, id)

	// A RAISING set without --yes: the statement is printed, nothing written.
	// The gate prints the daemon's STATEMENT (Data.Statement — the conformance
	// shape "budget <id>: set the usd cap from 10 to 20"); the refusal's What
	// ("this act loosens authority …") is the error text and the gate does not
	// render it (it intercepts confirm-required before renderError).
	out, err := d.run("budget", "set", "--scope", "loop", "--subject", id, "--window", "lifetime", "--usd", "20")
	if err == nil {
		t.Fatalf("a raising set without --yes committed:\n%s", out)
	}
	if !strings.Contains(out, "set the usd cap from 10 to 20") {
		t.Errorf("the raising set did not print the daemon's statement:\n%s", out)
	}
	if !strings.Contains(out, "re-run with --yes to apply — nothing was written") {
		t.Errorf("the raising set did not name the seatbelt:\n%s", out)
	}
	assertBudgetUSDCap(t, d.endpoint, budgetID, 10)

	// With --yes the digest commits and the cap moves.
	out = d.mustRun("budget", "set", "--scope", "loop", "--subject", id, "--window", "lifetime", "--usd", "20", "--yes")
	assertBudgetUSDCap(t, d.endpoint, budgetID, 20)
	if !strings.Contains(out, "confirmed") {
		t.Errorf("the confirmed commit did not say it was confirmed:\n%s", out)
	}

	// loop cancel without --yes: the state is the statement; nothing moves.
	out, err = d.run("loop", "cancel", id, "--reason", "the test is over")
	if err == nil {
		t.Fatalf("loop cancel without --yes acted:\n%s", out)
	}
	if !strings.Contains(out, "loop "+id+" — active") {
		t.Errorf("loop cancel did not print the loop's state as the statement:\n%s", out)
	}
	assertLoopStatus(t, d.endpoint, id, "active")

	// loop raise without --yes: same seatbelt, no act.
	out, err = d.run("loop", "raise", id, "--usd", "50", "--reason", "the test is over")
	if err == nil {
		t.Fatalf("loop raise without --yes acted:\n%s", out)
	}
	if !strings.Contains(out, "loop raise "+id+": re-run with --yes to apply") {
		t.Errorf("loop raise did not name the seatbelt:\n%s", out)
	}

	// A WATCHDOG disable without --yes: the statement, nothing written.
	ruleID := socketSystemWatchdogRule(t, d.endpoint)
	out, err = d.run("watchdog", "disable", ruleID)
	if err == nil {
		t.Fatalf("watchdog disable without --yes committed:\n%s", out)
	}
	if !strings.Contains(out, "after this it will no longer fire") {
		t.Errorf("watchdog disable did not print the daemon's statement:\n%s", out)
	}
	if !strings.Contains(out, "re-run with --yes to apply — nothing was written") {
		t.Errorf("watchdog disable did not name the seatbelt:\n%s", out)
	}
	assertWatchdogEnabled(t, d.endpoint, ruleID, true)

	// With --yes the disable commits.
	out = d.mustRun("watchdog", "disable", ruleID, "--yes")
	assertWatchdogEnabled(t, d.endpoint, ruleID, false)

	// A BUDGET remove without --yes: the statement, the cap still in force.
	out, err = d.run("budget", "remove", budgetID)
	if err == nil {
		t.Fatalf("budget remove without --yes committed:\n%s", out)
	}
	if !strings.Contains(out, "in-force caps") {
		t.Errorf("budget remove did not print the daemon's statement:\n%s", out)
	}
	assertBudgetPresent(t, d.endpoint, budgetID, true)

	// With --yes the budget is gone.
	out = d.mustRun("budget", "remove", budgetID, "--yes")
	assertBudgetPresent(t, d.endpoint, budgetID, false)
}

// TestNoDaemonSentenceAndExit is the no-offline-fallback proof: with nobody
// home the verbs answer their FIXED sentence and a non-zero exit, and open no
// store (the state directory stays empty of databases).
func TestNoDaemonSentenceAndExit(t *testing.T) {
	state := t.TempDir()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"loop", "stop", "1"}, noDaemonStopSentence},
		{[]string{"loop", "control", "1"}, noDaemonStopSentence},
		{[]string{"loop", "requests"}, noDaemonStopSentence},
		{[]string{"step", "stop", "1"}, noDaemonStopSentence},
		{[]string{"step", "continue", "1"}, noDaemonStopSentence},
		{[]string{"budget", "list"}, noDaemonGuardSentence},
		{[]string{"budget"}, noDaemonGuardSentence},
		{[]string{"watchdog", "list"}, noDaemonGuardSentence},
	} {
		out, err := aeonCommand(t, state, tc.args...).CombinedOutput()
		if err == nil {
			t.Errorf("aeon %v succeeded with no daemon — it should exit non-zero", tc.args)
			continue
		}
		if !strings.Contains(string(out), tc.want) {
			t.Errorf("aeon %v did not answer the fixed sentence:\n%s (want %q)", tc.args, out, tc.want)
		}
	}

	// No store opened: a database file anywhere under the state dir is a
	// fallback the design does not have.
	var dbs []string
	_ = filepath.WalkDir(state, func(p string, de fs.DirEntry, err error) error {
		if err == nil && !de.IsDir() && strings.HasSuffix(p, ".db") {
			dbs = append(dbs, p)
		}
		return nil
	})
	if len(dbs) > 0 {
		t.Fatalf("the control verbs opened a store with no daemon: %v", dbs)
	}
}

// TestHelpIsGeneratedFromVocabulary is the "typed twice" guard, both ways:
// the closed sets in the help are the daemon's vocabulary's sets (compared
// value for value), and none of the set's literals is typed in the verb
// sources — a literal there is a second truth that drifts when the set widens.
// Named TestBudgetAndWatchdogHelpIsGeneratedFromVocabulary (2026-11-02
// merge): S56's autonomy CLI carries its own TestHelpIsGeneratedFromVocabulary
// (autonomy_parity_test.go) — the two pins measure different verbs and
// stand side by side.
func TestBudgetAndWatchdogHelpIsGeneratedFromVocabulary(t *testing.T) {
	d := startDaemon(t)

	out := d.mustRun("budget")
	var bv protocol.BudgetVocabularyResult
	socketCall(t, d.endpoint, protocol.MethodBudgetVocabulary, struct{}{}, &bv)
	for _, set := range [][]string{bv.Scopes, bv.Windows, bv.Dimensions, bv.UnknownPolicies, bv.Statuses} {
		joined := strings.Join(set, " | ")
		if !strings.Contains(out, joined) {
			t.Errorf("the budget help is missing the vocabulary's set %q:\n%s", joined, out)
		}
	}

	out = d.mustRun("watchdog")
	var wv protocol.WatchdogVocabularyResult
	socketCall(t, d.endpoint, protocol.MethodWatchdogVocabulary, struct{}{}, &wv)
	if !strings.Contains(out, strings.Join(wv.Scopes, " | ")) {
		t.Errorf("the watchdog help is missing the vocabulary's scopes:\n%s", out)
	}
	if !strings.Contains(out, strings.Join(wv.Actions, " | ")) {
		t.Errorf("the watchdog help is missing the vocabulary's actions:\n%s", out)
	}
	for _, s := range wv.Signals {
		if !strings.Contains(out, s.Signal+" ("+s.Unit+", ") {
			t.Errorf("the watchdog help is missing the signal %q with its unit:\n%s", s.Signal, out)
		}
	}

	// The other way: no closed-set literal is typed in the verb sources.
	for _, f := range []string{"loop_control_verbs.go", "step_verbs_control.go", "budget_verbs.go", "watchdog_verbs.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		for _, lit := range []string{
			`"lifetime"`, `"wall-time"`, `"no-activity"`, `"same-position"`,
			`"repeat-block-reason"`, `"step-over-deadline"`, `"notify"`, `"epic"`, `"trigger"`,
		} {
			if strings.Contains(string(b), lit) {
				t.Errorf("%s types %s — the closed sets come from the daemon's vocabulary, never a client copy", f, lit)
			}
		}
	}
}

// TestCLIControlVerbsNeverImportStore is the store-discipline guard (a
// go/parser test, not a convention): the control verbs are socket clients, and
// a direct import of internal/store in any of them is a second door.
func TestCLIControlVerbsNeverImportStore(t *testing.T) {
	fset := token.NewFileSet()
	for _, name := range []string{"loop_control_verbs.go", "step_verbs_control.go", "budget_verbs.go", "watchdog_verbs.go"} {
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == "github.com/cjlapao/aeon/internal/store" || strings.HasSuffix(p, "/internal/store") {
				t.Errorf("%s imports the store — the control verbs are socket clients, never a direct door", name)
			}
		}
	}
}

// The two fixed sentences, asserted to differ: a test that made them equal
// would pass both no-daemon halves while proving neither.
func TestTheTwoNoDaemonSentencesDiffer(t *testing.T) {
	if noDaemonStopSentence == noDaemonGuardSentence {
		t.Fatal("the no-daemon sentences must name different answers for different verbs")
	}
}

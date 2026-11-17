package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

// DEF-041/SEC-033 at the display boundary: this verb's own assembled prose
// quotes caller-supplied values, so a crafted flag cannot inject escape bytes
// through the CLI's lines (the formatters' own output stays raw, per D9).
func TestCliProseQuotesCallerSuppliedValues(t *testing.T) {
	out, err := runPolicy(t, "remove", "--id", "x\u001b[31mRED\u001b[0m")
	if err != nil {
		t.Fatalf("remove of an unknown id must not fail: %v\n%s", err, out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("the CLI printed raw escape bytes from a flag value in its own prose:\n%q", out)
	}

	// F-14's line is the events verb's own prose carrying a caller-supplied
	// --subject, so it rides %q for the same SEC-033 reason as eventsUnauditedAdHoc.
	// Two halves are pinned: the formatter escapes whatever reaches it, and the
	// CLI never lets such bytes reach the terminal at all (the boundary's subject
	// parse refuses them before any line prints — defense in depth, not either/or).
	t.Run("the filter-honesty line escapes a hostile subject", func(t *testing.T) {
		hostile := "agent:x\"\n\x1b[31mRED\x1b[0m"
		line := eventsFilteredEmptyHonesty(hostile)
		if strings.ContainsAny(line, "\n\x1b") {
			t.Errorf("the filtered honesty line carried raw control bytes from the subject (SEC-033 class):\n%q", line)
		}
		if !strings.Contains(line, `\"`) || !strings.Contains(line, `\x1b[31m`) || !strings.Contains(line, `\n`) {
			t.Errorf("the filtered honesty line does not show the subject in escaped (%q) form:\n%q", hostile, line)
		}
		badOut, badErr := runPolicy(t, "events", "--subject", hostile)
		if badErr == nil {
			t.Errorf("the boundary ACCEPTED a subject carrying quote/newline/ESC bytes — the identifier parse regressed:\n%s", badOut)
		}
		if strings.Contains(badOut, "\x1b[") || strings.Contains(badOut, "\x1b") {
			t.Errorf("the events verb printed raw escape bytes from the --subject flag:\n%q", badOut)
		}
	})
}

// --- verb-shape hygiene -----------------------------------------------------------

func TestPolicyUsageAndUnknownSubcommand(t *testing.T) {
	out, err := runPolicy(t)
	if err != nil || !strings.Contains(out, "aeon policy") {
		t.Errorf("bare `policy` should print usage and succeed (got err %v):\n%s", err, out)
	}
	if _, err := runPolicy(t, "nope"); err == nil || !strings.Contains(err.Error(), "unknown `policy` subcommand") {
		t.Errorf("an unknown subcommand was not refused by name: %v", err)
	}
}

// --- the subprocess walk: the registration, the real store, the daemon down ----

// TestPolicyVerbsRunThroughTheRealBinaryWithTheDaemonDown is T12's DEMO walk
// in one binary launch each: main.go's `case "policy":` registered, the store
// opened DIRECTLY (daemon down, XDG_STATE_HOME in a temp dir — never the real
// user store), authoring over the socket nowhere, and the daemon-visibility
// claim (N8) in one direction we can see: our own write re-read on the next
// invocation.
func TestPolicyVerbsRunThroughTheRealBinaryWithTheDaemonDown(t *testing.T) {
	base := t.TempDir()
	run := func(args ...string) (string, error) {
		cmd := aeonCommand(t, base, append([]string{"policy"}, args...)...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := run("list")
	if err != nil {
		t.Fatalf("policy list with the daemon down: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no policies. default is DENY (PL5)") {
		t.Fatalf("the empty list did not state the default:\n%s", out)
	}

	// The demo's grant + broad deny + PL4 loss, through the real table.
	if out, err := run("add", "--type", "tool_access", "--id", "grant-bash-to-go",
		"--target", "agent:golang-engineer-loop", "--action", "allow",
		"--spec", `{"toolName":"bash","state":"allow"}`); err != nil {
		t.Fatalf("add grant: %v\n%s", err, out)
	}
	if out, err := run("add", "--type", "tool_access", "--id", "no-shell-anywhere",
		"--target", "all-agents", "--action", "deny",
		"--spec", `{"toolName":"bash","state":"deny"}`); err != nil {
		t.Fatalf("add deny: %v\n%s", err, out)
	}

	out, err = run("list")
	if err != nil || !strings.Contains(out, "grant-bash-to-go") || !strings.Contains(out, "no expiry") {
		t.Fatalf("list lost the stored rows:\n%s (%v)", out, err)
	}

	out, err = run("why", "--agent", "golang-engineer-loop", "--tool", "bash")
	if err != nil {
		t.Fatalf("why: %v\n%s", err, out)
	}
	if !strings.Contains(out, `refused: agent:golang-engineer-loop · tool_access "no-shell-anywhere"`) ||
		!strings.Contains(out, `(an allow exists: "grant-bash-to-go"`) {
		t.Errorf("why lost the deny-wins-with-override sentence:\n%s", out)
	}

	// PL1 across a REAL process: the non-operator Allow is refused at write,
	// with the source named, and exits non-zero.
	out, err = run("add", "--type", "tool_access", "--id", "self-grant", "--target", "agent:golang-engineer-loop",
		"--action", "allow", "--source", "agent", "--spec", `{"toolName":"bash","state":"allow"}`)
	if err == nil {
		t.Fatalf("a non-operator Allow was stored through the binary:\n%s", out)
	}
	if !strings.Contains(out, `source "agent"`) {
		t.Errorf("the refusal did not name the source:\n%s", out)
	}

	out, err = run("in-force", "--agent", "golang-engineer-loop")
	if err != nil || !strings.Contains(out, (policy.InForceAnswer{State: policy.EnforcedUnacknowledged}).Render()) {
		t.Fatalf("in-force lost the middle state:\n%s (%v)", out, err)
	}

	out, err = run("events")
	if err != nil {
		t.Fatalf("events: %v\n%s", err, out)
	}
	if !strings.Contains(out, "cap 90 days") || !strings.Contains(out, "oldest (none)") {
		t.Errorf("events lost the D15 summary (cap rendered from the const):\n%s", out)
	}
	if !strings.Contains(out, "NOT \"nothing was refused\"") {
		t.Errorf("the empty log read as clean without the honesty line:\n%s", out)
	}
	if strings.Contains(out, "unaudited") {
		t.Errorf("the minted actor was reported unaudited through the real binary:\n%s", out)
	}

	out, err = run("remove", "--id", "grant-bash-to-go")
	if err != nil || !strings.Contains(out, "removed") {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	out, _ = run("list")
	if strings.Contains(out, "grant-bash-to-go") {
		t.Errorf("the removed policy survived its removal:\n%s", out)
	}
}

// --- flag hygiene, store-failure honesty, and the bridge -------------------------

// TestEveryVerbAnswersHelpAndRefusesUnknownFlags: --help is a courtesy exit
// for every verb, and an unknown flag is an input error that fails the verb
// (flag.ContinueOnError, usage printed to the verb's writer — never a
// fallback that runs the verb anyway).
func TestEveryVerbAnswersHelpAndRefusesUnknownFlags(t *testing.T) {
	spy := &spyRepo{}
	withSpy(t, spy)
	verbs := []struct {
		name string
		more []string // minimum sensible args so the unknown flag is the failure
	}{
		{"list", nil},
		{"add", []string{"--type", "tool_access", "--id", "h", "--target", "agent:b", "--action", "deny", "--spec", `{"toolName":"bash","state":"deny"}`}},
		{"remove", []string{"--id", "h"}},
		{"seed-corpus", []string{"--agent", "b"}},
		{"why", []string{"--agent", "bob"}},
		{"in-force", []string{"--agent", "bob"}},
		{"events", nil},
	}
	for _, v := range verbs {
		if out, err := runPolicy(t, append([]string{v.name, "--help"}, v.more...)...); err != nil {
			t.Errorf("%s --help exited %v (help is a courtesy):\n%s", v.name, err, out)
		}
		if _, err := runPolicy(t, append([]string{v.name, "--definitely-not-a-flag"}, v.more...)...); err == nil {
			t.Errorf("%s accepted an unknown flag and continued", v.name)
		}
	}
}

// TestBoundaryOpenFailureFailsEveryVerbLoudest: when the store cannot be
// opened at all, every verb says so and fails — none of them prints a
// confident zero (which for list/events would be exactly the blind read
// DEF-041 forbids).
func TestBoundaryOpenFailureFailsEveryVerbLoudest(t *testing.T) {
	prev := policyBoundary
	policyBoundary = func() (*policy.Store, func(), error) {
		return nil, nil, errors.New("the store is struck: open /nope/state.db: permission denied")
	}
	t.Cleanup(func() { policyBoundary = prev })

	verbs := [][]string{
		{"list"},
		{"add", "--type", "tool_access", "--id", "h", "--target", "agent:b", "--action", "deny", "--spec", `{"toolName":"bash","state":"deny"}`},
		{"remove", "--id", "h"},
		{"seed-corpus", "--agent", "bob"},
		{"why", "--agent", "bob", "--tool", "bash"},
		{"in-force", "--agent", "bob"},
		{"in-force", "--loop", "my-loop"}, // the loop branch takes the same door
		{"events"},
	}
	for _, args := range verbs {
		out, err := runPolicy(t, args...)
		if err == nil || !strings.Contains(err.Error(), "the store is struck") {
			t.Errorf("policy %v on a struck store: err=%v\n%s", args, err, out)
		}
	}
}

// --- T8: `aeon policy add` with ask (issue 136) ----------------------------------
//
// The CLI half of S11: `--action ask` authored, `--command` sugar that writes
// the CommandAccess spec's command AND its state (the action restated — the
// only spelling the boundary's stateAgreesWithAction accepts), help that
// names all three actions, ask open to every source while allow stays the
// operator's alone (PL1 untouched), and SEC-014's carry-forward: a
// corpus-broad allow is WARNED about on stderr and then stored, never
// refused. Nothing here re-validates — action, source, spec shape all stay
// the write boundary's judgement (TestAddDoesNotRevalidate still holds).

// The verb's account of itself must name every action the closed set has —
// pinned against policy.AllActions(), never a second literal list, so the
// help fails LOUDLY the day the set widens instead of quietly lying about
// two. Three surfaces print the SAME string: the bare verb, unknown verbs,
// and `add --help` (fs.Usage).
func TestAddHelpAndUsageNameEveryAction(t *testing.T) {
	usage := policyUsage()
	for _, a := range policy.AllActions() {
		if !strings.Contains(usage, string(a)) {
			t.Errorf("the usage text omits action %q — the closed set has %d actions and the help must name all of them",
				a, len(policy.AllActions()))
		}
	}
	if !strings.Contains(usage, "--command") {
		t.Error("the usage text never mentions the --command sugar")
	}

	// The bare verb prints it.
	out := mustPolicy(t)
	if !strings.Contains(out, "Actions are allow, deny or ask.") {
		t.Errorf("bare `policy` did not print the action line:\n%s", out)
	}

	// And so does `add --help`, through the real flag parser (fs.Usage).
	out, err := runPolicy(t, "add", "--help")
	if err != nil {
		t.Fatalf("`add --help` failed: %v\n%s", err, out)
	}
	for _, a := range policy.AllActions() {
		if !strings.Contains(out, string(a)) {
			t.Errorf("`add --help` omits action %q:\n%s", a, out)
		}
	}
	if !strings.Contains(out, "--command") || !strings.Contains(out, "--action") {
		t.Errorf("`add --help` does not print its flags:\n%s", out)
	}
}

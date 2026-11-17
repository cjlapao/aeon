package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// The --command sugar's contract, one table: it writes "command" and (when an
// action was given) "state" as that action restated, leaves every other spec
// field alone, and refuses to say "command" twice. The exact JSON matters:
// the boundary's validator reads these bytes back, and state must be exactly
// ONE value agreeing with the row's action.
func TestSpecSugarSetsCommandNameAndRestatedState(t *testing.T) {
	cases := []struct {
		name    string
		spec    string
		command string
		action  string
		want    string // exact marshalled bytes (map order: sorted keys)
		wantErr string // substring; "" = must succeed
	}{
		{name: "ask restates its state", command: "git status", action: "ask",
			want: `{"command":"git status","state":"ask"}`},
		{name: "allow restates its state", command: "git push", action: "allow",
			want: `{"command":"git push","state":"allow"}`},
		{name: "deny restates its state", command: "rm -rf /", action: "deny",
			want: `{"command":"rm -rf /","state":"deny"}`},
		{name: "blank action writes no state", command: "git status", action: "",
			want: `{"command":"git status"}`},
		{name: "any spec beside the sugar is say-it-once", spec: `{"state":"ask"}`, command: "git status", action: "ask",
			wantErr: "say it once"},
		{name: "a non-object spec is refused", spec: `5`, command: "",
			wantErr: "not a JSON object"},
		{name: "a null spec is refused", spec: `null`, command: "",
			wantErr: "not an object"},
		{name: "no sugar no spec writes nothing", spec: "", command: "", action: "ask", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The SAME two merges cmdPolicyAdd runs, in the same order: the
			// tool-side merge parses --spec on every path, the command-side
			// merge adds the sugar and stays silent when it is unused.
			build := func(spec, command, action string) ([]byte, error) {
				base, err := mergeSpecToolName(spec, "")
				if err != nil {
					return nil, err
				}
				sugared, err := mergeSpecCommandName(spec, command, action)
				if err != nil {
					return nil, err
				}
				if sugared != nil {
					return sugared, nil
				}
				return base, nil
			}
			got, err := build(tc.spec, tc.command, tc.action)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want an error naming %q, got %v (%q)", tc.wantErr, err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("sugar refused legal input: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("spec bytes drifted:\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// The pairings that cannot mean a row are refused by the flag parser that
// owns the flags — and refused BEFORE the store is opened (the spy sees zero
// round-trips). What a pattern MEANS is never checked here; that stays the
// boundary's single judgement.
func TestAddCommandSugarFlagPairingRefusals(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"tool and command together",
			[]string{"add", "--type", "command_access", "--id", "pair-x", "--target", "agent:bob",
				"--action", "ask", "--tool", "bash", "--command", "git status"},
			"--tool or --command, not both"},
		{"command on a non-command type",
			[]string{"add", "--type", "tool_access", "--id", "pair-y", "--target", "agent:bob",
				"--action", "allow", "--command", "git status"},
			"command_access"},
		{"spec beside command says it twice",
			[]string{"add", "--type", "command_access", "--id", "pair-z", "--target", "agent:bob",
				"--action", "ask", "--spec", `{}`, "--command", "git status"},
			"say it once"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := &spyRepo{}
			withSpy(t, spy)
			out, err := runPolicy(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want refusal naming %q, got err=%v\n%s", tc.wantErr, err, out)
			}
			if spy.calls.add != 0 || spy.calls.listAll != 0 {
				t.Errorf("a flag-level refusal still crossed the boundary (add=%d list=%d)", spy.calls.add, spy.calls.listAll)
			}
		})
	}
}

// The acceptance table, and the point of T8: `ask` is open to EVERY source
// (both of its siblings take autonomy away from the agent, T6's argument now
// covering the third), while `allow` from anyone but the operator is still
// refused — verbatim by the boundary, not re-implemented here.
func TestAskIsSourceOpenAndAllowStaysOperatorOnly(t *testing.T) {
	cases := []struct {
		action string
		source string
		wantOK bool
	}{
		{"ask", "operator", true},
		{"ask", "agent", true},
		{"ask", "mcp_server", true},
		{"ask", "import", true},
		{"deny", "agent", true},
		{"allow", "operator", true},
		{"allow", "agent", false},
		{"allow", "mcp_server", false},
		{"allow", "import", false},
	}
	for _, tc := range cases {
		t.Run(tc.action+"/"+tc.source, func(t *testing.T) {
			spy := &spyRepo{}
			withSpy(t, spy)
			out, err := runPolicy(t, "add",
				"--type", "command_access", "--id", "cap-"+tc.action+"-"+tc.source,
				"--target", "agent:bob", "--action", tc.action, "--source", tc.source,
				"--command", "git status")
			if tc.wantOK {
				if err != nil {
					t.Fatalf("a legal %s from %s was refused:\n%s\n%v", tc.action, tc.source, out, err)
				}
				if spy.calls.add != 1 {
					t.Errorf("%s/%s stored %d rows, want 1", tc.action, tc.source, spy.calls.add)
				}
				return
			}
			if err == nil {
				t.Fatalf("an %s authored by %s was STORED — PL1 must refuse it:\n%s", tc.action, tc.source, out)
			}
			if !strings.Contains(err.Error(), "PL1") || !strings.Contains(err.Error(), `source "`+tc.source+`"`) {
				t.Errorf("the refusal does not name PL1 and the source:\n%v", err)
			}
			if spy.calls.add != 0 {
				t.Errorf("the refused row reached the repo (%d adds)", spy.calls.add)
			}
		})
	}
}

// The plan's fixture criterion, made executable: an ask command_access row
// authored through --command lands in the store EXACTLY as the contract's
// spec JSON — {"command":...,"state":"ask"} — written by an agent source,
// through a throwaway XDG_STATE_HOME (AGENTS.md §6: never the real store).
func TestPolicyAddAskRowStoredWithExactSpecJSON(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	add := exec.Command(aeonBinary, "policy", "add",
		"--type", "command_access", "--id", "ask-git-status",
		"--target", "agent:bob", "--action", "ask", "--source", "agent",
		"--command", "git status")
	add.Env = append(os.Environ(), "XDG_STATE_HOME="+stateHome)
	out, err := add.CombinedOutput()
	if err != nil {
		t.Fatalf("the shipped binary refused an ask from agent: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "policy ask-git-status stored") {
		t.Fatalf("the add did not report storing:\n%s", out)
	}

	// Read the row back through the SAME opener the verbs use (empty Options
	// resolves the very XDG_STATE_HOME the subprocess wrote under).
	s, err := store.Open(store.Options{})
	if err != nil {
		t.Fatalf("open the fixture store: %v", err)
	}
	defer func() { _ = s.Close() }()
	rows, err := s.PolicyListAll(context.Background())
	if err != nil {
		t.Fatalf("read back the fixture row: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the store holds %d rows, want the one fixture row", len(rows))
	}
	row := rows[0]
	if row.ID != "ask-git-status" || row.Type != "command_access" ||
		row.TargetKind != "agent" || row.TargetID != "bob" {
		t.Errorf("the fixture row's identity drifted: %+v", row)
	}
	if row.Action != "ask" {
		t.Errorf("stored action is %q, want \"ask\"", row.Action)
	}
	if row.Source != "agent" {
		t.Errorf("stored source is %q, want \"agent\" (ask is source-open)", row.Source)
	}
	if row.SpecJSON != `{"command":"git status","state":"ask"}` {
		t.Errorf("the stored spec is not the contract's exact JSON:\n got %s\nwant %s",
			row.SpecJSON, `{"command":"git status","state":"ask"}`)
	}
	if row.NotAfter != "" {
		t.Errorf("the fixture row carries an expiry %q — an omitted --not-after means none (PL7)", row.NotAfter)
	}
}

// SEC-014's measurement, pinned against the corpus itself rather than a
// curated scary-string list: the numbers the warning prints are the numbers
// policy.MatchPattern produces over Corpus.DistinctPatterns, recomputed here
// independently. Broad → warns. Narrow → silent. Anything but allow → silent.
func TestBroadAllowWarningMeasuresAgainstTheCorpus(t *testing.T) {
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		t.Fatalf("the embedded corpus does not parse: %s", rej.Reason)
	}
	distinct := corpus.DistinctPatterns()
	if len(distinct) == 0 {
		t.Fatal("the corpus has no distinct patterns — the measurement has no denominator")
	}
	coverage := func(pattern string) int {
		n := 0
		for _, p := range distinct {
			if policy.MatchPattern(pattern, p) {
				n++
			}
		}
		return n
	}

	cases := []struct {
		name   string
		action policy.Action
		spec   string
		want   bool
	}{
		{"a wildcard allow over the whole corpus", policy.Allow, `{"command":"*","state":"allow"}`, true},
		{"a wildcard allow over a quarter of it", policy.Allow, `{"command":"git*","state":"allow"}`, true},
		{"a narrow allow is quiet", policy.Allow, `{"command":"git status","state":"allow"}`, false},
		{"an allow matching nothing in the corpus is quiet", policy.Allow, `{"command":"totally-unheard-of","state":"allow"}`, false},
		{"the same wildcard as a deny is never broad", policy.Deny, `{"command":"*","state":"deny"}`, false},
		{"the same wildcard as an ask is never broad", policy.Ask, `{"command":"*","state":"ask"}`, false},
		{"an unvalidated action string never warns (the boundary judges it)", policy.Action("permissive"), `{"command":"*","state":"allow"}`, false},
		{"a tool spec has no command to measure", policy.Allow, `{"toolName":"bash","state":"allow"}`, false},
		{"a non-string command is not measurable here", policy.Allow, `{"command":7}`, false},
		{"no spec at all is quiet", policy.Allow, ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line, ok := broadAllowWarning(tc.action, []byte(tc.spec))
			if ok != tc.want {
				t.Fatalf("warned=%v, want %v (line=%q)", ok, tc.want, line)
			}
			if ok && !strings.Contains(line, "SEC-014") {
				t.Errorf("a SEC-014 warning that does not name SEC-014: %q", line)
			}
		})
	}

	// The numbers ARE the corpus's own — recomputed, not remembered.
	starLine, ok := broadAllowWarning(policy.Allow, []byte(`{"command":"*"}`))
	if !ok {
		t.Fatal("the wildcard allow did not warn")
	}
	if want := fmt.Sprintf("%d of %d", coverage("*"), len(distinct)); !strings.Contains(starLine, want) {
		t.Errorf("the warning's coverage is not the corpus's own:\nwant %q inside\n%s", want, starLine)
	}
	gitLine, ok := broadAllowWarning(policy.Allow, []byte(`{"command":"git*"}`))
	if !ok {
		t.Fatalf("git* covers %d of %d distinct patterns and should warn", coverage("git*"), len(distinct))
	}
	if want := fmt.Sprintf("%d of %d", coverage("git*"), len(distinct)); !strings.Contains(gitLine, want) {
		t.Errorf("the warning's coverage is not the corpus's own:\nwant %q inside\n%s", want, gitLine)
	}

	// commandPatternFromSpec's own edges (the warn probe, not a validator).
	if got := commandPatternFromSpec([]byte(`not json`)); got != "" {
		t.Errorf("non-JSON yielded %q, want empty", got)
	}
	if got := commandPatternFromSpec([]byte(`{"command":null}`)); got != "" {
		t.Errorf("JSON null command yielded %q, want empty", got)
	}

	// The unmeasurable variant: a corpus that cannot be parsed sizes nothing,
	// so the line says so instead of inventing a percentage (the only pattern
	// broad on its face still speaks — see broadAllowWarning's fallback).
	unmeasurable := broadAllowWarn("*", 0, 0)
	if !strings.Contains(unmeasurable, "SEC-014") || strings.Contains(unmeasurable, " of ") {
		t.Errorf("the unmeasurable line invented a number: %q", unmeasurable)
	}
}

// The SEC-014 ruling end to end, through the shipped binary and a throwaway
// XDG_STATE_HOME: warned on stderr, stored on stdout, exit 0. The warning is
// a warning — none of these adds is a refusal, and the narrow one is silent
// (a warning that always fires is a warning nobody reads).
func TestAddBroadAllowWarnsOnStderrAndIsStillStored(t *testing.T) {
	cases := []struct {
		pattern  string
		wantWarn bool
	}{
		{"*", true},
		{"git*", true},
		{"git status", false},
	}
	for _, tc := range cases {
		t.Run("pattern="+tc.pattern, func(t *testing.T) {
			stateHome := t.TempDir()
			t.Setenv("XDG_STATE_HOME", stateHome)

			add := exec.Command(aeonBinary, "policy", "add",
				"--type", "command_access", "--id", "broad-allow",
				"--target", "all-agents", "--action", "allow", "--source", "operator",
				"--command", tc.pattern)
			add.Env = append(os.Environ(), "XDG_STATE_HOME="+stateHome)
			var stdout, stderr strings.Builder
			add.Stdout = &stdout
			add.Stderr = &stderr
			if err := add.Run(); err != nil {
				t.Fatalf("a broad allow was REFUSED (warn, never refuse): %v\nstdout:\n%s\nstderr:\n%s",
					err, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), "policy broad-allow stored") {
				t.Errorf("the row was not reported stored on stdout:\n%s", stdout.String())
			}
			hasWarn := strings.Contains(stderr.String(), "SEC-014")
			if hasWarn != tc.wantWarn {
				t.Errorf("stderr warn present = %v, want %v\nstderr:\n%s", hasWarn, tc.wantWarn, stderr.String())
			}
			if tc.wantWarn && !strings.Contains(stderr.String(), "warning") {
				t.Errorf("the SEC-014 line is not shaped like the verb's other warnings:\n%s", stderr.String())
			}
			// Warned AND stored — the row is really there.
			s, err := store.Open(store.Options{})
			if err != nil {
				t.Fatalf("open the fixture store: %v", err)
			}
			defer func() { _ = s.Close() }()
			rows, err := s.PolicyListAll(context.Background())
			if err != nil {
				t.Fatalf("read back the row: %v", err)
			}
			if len(rows) != 1 || rows[0].Action != "allow" {
				t.Fatalf("the warned row did not reach the store: %+v", rows)
			}
		})
	}
}

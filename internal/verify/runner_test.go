package verify

// runner_test.go — the VG4 behaviours. The policy cases run the REAL
// evaluator (a hand-arranged row, as the tools' own policy seam tests do)
// so the sentence recorded is the one formatter's, verbatim; the spawn is a
// spy where the property is "it must not happen" and real where the
// property is the process itself.

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
)

// gateRowT is one stored CommandAccess row over a pattern — the same shape
// the tools' seam tests author: the spec JSON, the target, the action.
func gateRowT(tb testing.TB, id, pattern, action string) policy.Policy {
	tb.Helper()
	spec, err := json.Marshal(map[string]string{"command": pattern, "state": action})
	if err != nil {
		tb.Fatalf("helper built an unreadable spec: %v", err)
	}
	target, rej := policy.ParseTarget(policy.AllAgents, "")
	if rej != nil {
		tb.Fatalf("helper built an illegal target: %s", rej.Reason)
	}
	act, rej := policy.ParseAction(action)
	if rej != nil {
		tb.Fatalf("helper built an illegal action: %s", rej.Reason)
	}
	return policy.Policy{
		ID:     id,
		Type:   policy.CommandAccess,
		Target: target,
		Action: act,
		Spec:   spec,
		Source: policy.Operator,
	}
}

// seamOver returns the Verdict seam over a fixed rule set: the same call
// the daemon's CommandVerdictFor makes, from the runner's side of the seam.
func seamOver(tb testing.TB, ps []policy.Policy, agent string) func(agent, command string) (policy.Verdict, bool) {
	tb.Helper()
	return func(a, command string) (policy.Verdict, bool) {
		v := policy.Evaluate(ps, policy.Request{
			Type: policy.CommandAccess, Agent: a, Tool: "bash", Command: command,
		}, time.Now())
		return v, true
	}
}

// allowAllSeam is the permissive source: one operator allow over `*`.
func allowAllSeam(tb testing.TB, agent string) func(agent, command string) (policy.Verdict, bool) {
	tb.Helper()
	return seamOver(tb, []policy.Policy{gateRowT(tb, "gate-allow", "*", "allow")}, agent)
}

// spawnSpy counts the calls a gate must never make.
func spawnSpy(tb testing.TB) (*int, func(context.Context, string, tools.SpawnOptions, policy.AllowToken,
	execenv.ChildEnv) (tools.RunResult, error)) {
	tb.Helper()
	calls := 0
	fn := func(ctx context.Context, command string, opts tools.SpawnOptions, tok policy.AllowToken, _ execenv.ChildEnv) (tools.RunResult, error) {
		calls++
		tb.Errorf("the gate spawned — the policy question had a no: %q", command)
		return tools.RunResult{}, nil
	}
	return &calls, fn
}

// gate is a required gate with a 30 s timeout.
func gate(name, command string) GateSpec {
	return GateSpec{Name: name, Command: command, Requirement: "required", TimeoutS: 30}
}

// TestPolicyDeniedGateNeverSpawns — a deny is a no that happens before the
// spawn: unverified with reason policy, the one formatter's sentence
// VERBATIM, and the spawner never called.
func TestPolicyDeniedGateNeverSpawns(t *testing.T) {
	const agent = "gate-policy-test"
	ps := []policy.Policy{gateRowT(t, "gate-deny", "curl*", "deny")}
	calls, spy := spawnSpy(t)
	runner := Runner{Verdict: seamOver(t, ps, agent), Spawn: spy}
	g := gate("denied", "curl https://example.com")

	got := runner.RunGate(context.Background(), t.TempDir(), agent, g)
	if *calls != 0 {
		t.Fatalf("the denied gate spawned %d time(s); a deny happens before the spawn", *calls)
	}

	if got.State != "unverified" || ReasonWord(got.Reason) != "policy" {
		t.Fatalf("the denied gate is %s/%s; it is unverified with reason policy", got.State, got.Reason)
	}
	// The sentence, verbatim: the same evaluation the seam made.
	want := policy.Evaluate(ps, policy.Request{
		Type: policy.CommandAccess, Agent: agent, Tool: "bash", Command: g.Command,
	}, time.Now()).Sentence()
	if !strings.HasPrefix(got.Reason, "policy: ") || !strings.Contains(got.Reason, want) {
		t.Errorf("the recorded reason %q does not carry the verdict's sentence %q verbatim", got.Reason, want)
	}
	if got.ExitCode != nil {
		t.Errorf("a gate that never ran has no exit code; got %d", *got.ExitCode)
	}
}

// TestPolicyAskIsDenyUnattended — an ask is a question, and a gate has no
// human to ask: no token can be minted for one, and nothing spawns.
func TestPolicyAskIsDenyUnattended(t *testing.T) {
	const agent = "gate-policy-test"
	ps := []policy.Policy{gateRowT(t, "gate-ask", "rm*", "ask")}
	calls, spy := spawnSpy(t)
	runner := Runner{Verdict: seamOver(t, ps, agent), Spawn: spy}
	g := gate("asked", "rm -rf ./build")

	got := runner.RunGate(context.Background(), t.TempDir(), agent, g)
	if *calls != 0 {
		t.Fatalf("the asked gate spawned %d time(s); an unattended ask denies at once", *calls)
	}

	if got.State != "unverified" || ReasonWord(got.Reason) != "policy" {
		t.Fatalf("the asked gate is %s/%s; it is unverified with reason policy", got.State, got.Reason)
	}
	if got.ExitCode != nil {
		t.Errorf("a gate that never ran has no exit code; got %d", *got.ExitCode)
	}
}

// TestPolicyNoVerdictNeverSpawns — a seam that cannot answer is a refusal,
// not a default: the gate is unverified with the fixed sentence, and no
// command is ever handed to a spawner.
func TestPolicyNoVerdictNeverSpawns(t *testing.T) {
	calls, spy := spawnSpy(t)
	runner := Runner{
		Verdict: func(agent, command string) (policy.Verdict, bool) { return policy.Verdict{}, false },
		Spawn:   spy,
	}

	got := runner.RunGate(context.Background(), t.TempDir(), "gate-policy-test", gate("silent", "true"))
	if *calls != 0 {
		t.Fatalf("the no-verdict gate spawned %d time(s); no command reaches a spawner", *calls)
	}

	if got.State != "unverified" || ReasonWord(got.Reason) != "policy" {
		t.Fatalf("the no-verdict gate is %s/%s; it is unverified with reason policy", got.State, got.Reason)
	}
	if !strings.Contains(got.Reason, noVerdictSentence) {
		t.Errorf("the recorded reason %q does not carry the no-verdict sentence", got.Reason)
	}
}

// TestExitCodeMapping — the completed-command half of VG4 item 4: exit 0
// is pass (and stores NO tail, VG6), a non-zero exit is fail (and stores
// the redacted tail). The third case is behaviour 1: a gate with an allow
// token runs in dir with EXACTLY the scrubbed environment — the printenv
// output may contain only allowlisted names.
func TestExitCodeMapping(t *testing.T) {
	agent := "gate-map-test"
	runner := Runner{Verdict: allowAllSeam(t, agent)}
	dir := t.TempDir()

	// exit 0 ⇒ pass, and a pass stores no output text at all.
	pass := runner.RunGate(context.Background(), dir, agent, gate("ok", "echo pass-out"))
	if pass.State != "pass" || pass.Reason != "" {
		t.Fatalf("exit 0 is pass with an empty reason; got %s/%s", pass.State, pass.Reason)
	}
	if pass.ExitCode == nil || *pass.ExitCode != 0 {
		t.Fatalf("the pass gate's exit code is %v; it is 0", pass.ExitCode)
	}
	if pass.StdoutTail != "" || pass.StderrTail != "" {
		t.Errorf("a pass stores no output text; tails are %q / %q", pass.StdoutTail, pass.StderrTail)
	}
	if pass.StdoutBytes != len("pass-out\n") {
		t.Errorf("the pass gate's true stdout count is %d; the command printed %d bytes", pass.StdoutBytes, len("pass-out\n"))
	}

	// exit 3 ⇒ fail, and a fail stores the redacted tail.
	fail := runner.RunGate(context.Background(), dir, agent, gate("bad", "echo out3; exit 3"))
	if fail.State != "fail" {
		t.Fatalf("exit 3 is fail; got %s/%s", fail.State, fail.Reason)
	}
	if fail.ExitCode == nil || *fail.ExitCode != 3 {
		t.Fatalf("the fail gate's exit code is %v; it is 3", fail.ExitCode)
	}
	if fail.StdoutTail != "out3\n" {
		t.Errorf("the fail gate's stdout tail is %q; it is the redacted captured output", fail.StdoutTail)
	}

	// Behaviour 1: the child's environment is EXACTLY the allowlist —
	// printenv says so, and nothing the daemon carries that is not on the
	// list is in the output. The gate fails on purpose: a pass stores no
	// tail (VG6), and the output must be READ to check it.
	envGate := runner.RunGate(context.Background(), dir, agent, gate("env", "printenv; exit 1"))
	if envGate.State != "fail" {
		t.Fatalf("the printenv gate is %s/%s; exit 1 is fail", envGate.State, envGate.Reason)
	}
	allowed := make(map[string]bool)
	for _, n := range tools.GateEnvNames() {
		allowed[n] = true
	}
	allowed["CI"] = true
	// The child's own shell is bash (the tool's choice, T1 — unchanged by
	// this task), and bash regenerates three of its own bookkeeping
	// variables in the child's environment whatever it inherited: _ (the
	// previous command's last argument), PWD (the working directory) and
	// SHLVL (the nesting counter). Those are the shell's, not the
	// daemon's: the assertion is that EVERYTHING else in the output is
	// allowlisted, i.e. nothing the daemon carries crossed in.
	shellOwns := map[string]bool{"_": true, "PWD": true, "SHLVL": true}
	for _, line := range strings.Split(strings.TrimRight(envGate.StdoutTail, "\n"), "\n") {
		if line == "" {
			continue
		}
		name, _, _ := strings.Cut(line, "=")
		if !allowed[name] && !strings.HasPrefix(name, "LC_") && !shellOwns[name] {
			t.Errorf("the name %s reached the gate's environment and is not on the allowlist", name)
		}
	}
}

// TestTimeoutKillsTheGroup — a gate that overruns its own timeout is
// unverified with reason timeout (the check did not complete, so it
// proves nothing), the process GROUP is gone (no orphan child survives the
// kill), and the timed-out flag says which.
func TestTimeoutKillsTheGroup(t *testing.T) {
	agent := "gate-timeout-test"
	runner := Runner{Verdict: allowAllSeam(t, agent)}
	dir := t.TempDir()

	g := gate("slow", "sleep 300 & echo $! > child.pid; sleep 300")
	g.TimeoutS = 1
	got := runner.RunGate(context.Background(), dir, agent, g)

	if got.State != "unverified" || got.Reason != "timeout" {
		t.Fatalf("the overrunning gate is %s/%s; it is unverified with reason timeout", got.State, got.Reason)
	}
	if !got.TimedOut {
		t.Errorf("the timed-out flag is false; the timeout is the specific cause")
	}
	if got.ExitCode != nil {
		t.Errorf("a gate killed by its own timeout has no exit code; got %d", *got.ExitCode)
	}
	// The backgrounded child is in the same group: it must be dead.
	pidBytes, err := os.ReadFile(filepath.Join(dir, "child.pid"))
	if err != nil {
		t.Fatalf("the child's pid file is missing: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		t.Fatalf("the child's pid is unreadable: %v", err)
	}
	if processRunningAfterGrace(pid, 2*time.Second) {
		t.Errorf("child %d survived the group kill", pid)
	}
}

// TestKilledIsUnverified — a gate that dies to SIGKILL (or exits 137) did
// not complete: unverified with reason killed, the heuristic reused from
// tools (exit 137 or SIGKILL), not re-derived here.
func TestKilledIsUnverified(t *testing.T) {
	agent := "gate-kill-test"
	runner := Runner{Verdict: allowAllSeam(t, agent)}

	got := runner.RunGate(context.Background(), t.TempDir(), agent, gate("suicide", "kill -9 $$"))

	if got.State != "unverified" || got.Reason != "killed" {
		t.Fatalf("the killed gate is %s/%s; it is unverified with reason killed", got.State, got.Reason)
	}
	if got.TimedOut {
		t.Errorf("the killed flag and the timeout flag are different causes")
	}
}

// TestCancelledIsUnverified — the run's ctx is the loop's: S55's stop
// cancels it, the running gate's group is terminated (unverified:
// cancelled), and the gates after it are not started — recorded as
// cancelled with no exit code, so the evidence shows why the run stopped.
func TestCancelledIsUnverified(t *testing.T) {
	agent := "gate-cancel-test"
	runner := Runner{Verdict: allowAllSeam(t, agent)}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	in := RunInput{
		Gates: []GateSpec{
			gate("running", "sleep 30"),
			gate("never-started", "echo should-not-run > ran"),
		},
		Agent: agent,
		Dir:   t.TempDir(),
	}
	got := runner.Run(ctx, in)

	if got.State != "unverified" || got.Reason != "unverified: cancelled" {
		t.Fatalf("the cancelled run is %s/%s; it is unverified: cancelled", got.State, got.Reason)
	}
	if len(got.Results) != 2 {
		t.Fatalf("the run recorded %d results; both gates must be accounted for", len(got.Results))
	}
	first, second := got.Results[0], got.Results[1]
	if first.State != "unverified" || first.Reason != "cancelled" {
		t.Errorf("the running gate is %s/%s; it is unverified with reason cancelled", first.State, first.Reason)
	}
	if second.State != "unverified" || second.Reason != "cancelled" || second.ExitCode != nil {
		t.Errorf("the unstarted gate is %s/%s exit=%v; it is unverified, cancelled, and never ran", second.State, second.Reason, second.ExitCode)
	}
	if _, err := os.Stat(filepath.Join(in.Dir, "ran")); !os.IsNotExist(err) {
		t.Errorf("the second gate ran after the cancellation")
	}
}

// TestEmptyAndAdvisoryOnlyAreNoGatesConfigured — a run with no required
// gate proves nothing required: unverified, reason no-gates-configured,
// never pass on emptiness. An advisory-only set is the same shape.
func TestEmptyAndAdvisoryOnlyAreNoGatesConfigured(t *testing.T) {
	agent := "gate-empty-test"
	runner := Runner{Verdict: allowAllSeam(t, agent)}
	calls := 0
	runner.Spawn = func(ctx context.Context, command string, opts tools.SpawnOptions, tok policy.AllowToken, _ execenv.ChildEnv) (tools.RunResult, error) {
		calls++
		return tools.RunResult{ExitCode: 0}, nil
	}

	empty := runner.Run(context.Background(), RunInput{Agent: agent, Dir: t.TempDir()})
	if empty.State != "unverified" || empty.Reason != "unverified: no-gates-configured" {
		t.Fatalf("the empty run is %s/%s; it is unverified: no-gates-configured", empty.State, empty.Reason)
	}

	adv := gate("advisory-only", "true")
	adv.Requirement = "advisory"
	only := runner.Run(context.Background(), RunInput{Gates: []GateSpec{adv}, Agent: agent, Dir: t.TempDir()})
	if only.State != "unverified" || only.Reason != "unverified: no-gates-configured" {
		t.Fatalf("the advisory-only run is %s/%s; it is unverified: no-gates-configured", only.State, only.Reason)
	}
	if calls != 0 {
		t.Errorf("the run that proves nothing required ran %d gate(s)", calls)
	}
}

// TestAggregatePrecedence — the VG4 item-5 truth table, scripted so each
// row is a pure function of the results: any required fail ⇒ fail; else any
// required unverified ⇒ unverified; else pass. Advisory results are
// recorded and counted, and never change the state.
func TestAggregatePrecedence(t *testing.T) {
	agent := "gate-agg-test"
	type out struct {
		exit   int
		killed bool
	}
	cases := []struct {
		name  string
		gates []struct {
			requirement string
			out         out
		}
		wantState  string
		wantReason string
	}{
		{
			name: "required fail beats advisory pass",
			gates: []struct {
				requirement string
				out         out
			}{{"required", out{exit: 3}}, {"advisory", out{exit: 0}}},
			wantState: "fail",
		},
		{
			name: "required unverified over required pass",
			gates: []struct {
				requirement string
				out         out
			}{{"required", out{killed: true}}, {"required", out{exit: 0}}},
			wantState:  "unverified",
			wantReason: "unverified: killed",
		},
		{
			name: "fail beats unverified",
			gates: []struct {
				requirement string
				out         out
			}{{"required", out{exit: 7}}, {"required", out{killed: true}}},
			wantState: "fail",
		},
		{
			name: "advisory fail never sinks the run",
			gates: []struct {
				requirement string
				out         out
			}{{"required", out{exit: 0}}, {"advisory", out{exit: 1}}},
			wantState: "pass",
		},
		{
			name: "all required pass with an advisory gap is pass",
			gates: []struct {
				requirement string
				out         out
			}{{"required", out{exit: 0}}, {"advisory", out{killed: true}}},
			wantState:  "pass",
			wantReason: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			i := 0
			runner := Runner{
				Verdict: allowAllSeam(t, agent),
				Spawn: func(ctx context.Context, command string, opts tools.SpawnOptions, tok policy.AllowToken, _ execenv.ChildEnv) (tools.RunResult, error) {
					g := c.gates[i]
					i++
					if g.out.killed {
						return tools.RunResult{ExitCode: -1, Signal: "killed", OOMKilled: true}, nil
					}
					return tools.RunResult{ExitCode: g.out.exit}, nil
				},
			}
			var specs []GateSpec
			for n, g := range c.gates {
				specs = append(specs, GateSpec{Name: "g" + string(rune('1'+n)), Command: "true", Requirement: g.requirement, TimeoutS: 30})
			}
			got := runner.Run(context.Background(), RunInput{Gates: specs, Agent: agent, Dir: t.TempDir()})
			if got.State != c.wantState {
				t.Fatalf("the run is %s (%s); the truth table says %s", got.State, got.Summary, c.wantState)
			}
			if c.wantReason != "" && got.Reason != c.wantReason {
				t.Errorf("the run's reason is %q; the truth table says %q", got.Reason, c.wantReason)
			}
			// Advisory results are RECORDED even when they do not sink.
			if len(got.Results) != len(c.gates) {
				t.Errorf("the run recorded %d results; every gate's result is kept", len(got.Results))
			}
		})
	}
}

// TestNoShortCircuit — a required fail does not stop the round: the second
// gate still runs (one round of rework sees every red), and the run is
// fail with both results.
func TestNoShortCircuit(t *testing.T) {
	agent := "gate-noshort-test"
	runner := Runner{Verdict: allowAllSeam(t, agent)}
	dir := t.TempDir()

	in := RunInput{
		Gates: []GateSpec{
			gate("first-fails", "touch ran1; exit 1"),
			gate("second-runs", "touch ran2"),
		},
		Agent: agent,
		Dir:   dir,
	}
	got := runner.Run(context.Background(), in)

	if got.State != "fail" {
		t.Fatalf("the run is %s; a required fail makes the run fail", got.State)
	}
	if len(got.Results) != 2 {
		t.Fatalf("the run recorded %d results; the round must not stop at the first red", len(got.Results))
	}
	for _, f := range []string{"ran1", "ran2"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s is missing — the gate after the fail did not run", f)
		}
	}
}

// TestTailsOnlyOnNonPassAndRedacted — behaviour 7, the tail half: a pass
// stores no tail even when the stream carries a token; a fail stores the
// tail ≤ 2 KiB, with the token-shaped string masked and the shape kept.
func TestTailsOnlyOnNonPassAndRedacted(t *testing.T) {
	const secret = "ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ012345" // 36 chars of body
	agent := "gate-tail-test"
	runner := Runner{Verdict: allowAllSeam(t, agent)}
	dir := t.TempDir()

	pass := runner.RunGate(context.Background(), dir, agent,
		gate("pass-with-token", "echo "+secret))
	if pass.State != "pass" {
		t.Fatalf("the gate is %s; the command is allowed", pass.State)
	}
	if pass.StdoutTail != "" {
		t.Errorf("a pass stores no output text; the tail is %q", pass.StdoutTail)
	}
	if len(pass.StdoutSHA256) != 64 {
		t.Errorf("the pass gate's stdout digest is %q; a sha256 hex digest is ALWAYS present and 64 long", pass.StdoutSHA256)
	}

	fail := runner.RunGate(context.Background(), dir, agent,
		gate("fail-with-token", "echo "+secret+"; exit 2"))
	if fail.State != "fail" {
		t.Fatalf("the gate is %s; exit 2 is fail", fail.State)
	}
	if strings.Contains(fail.StdoutTail, secret) {
		t.Errorf("the full token reached the fail tail: %q", fail.StdoutTail)
	}
	if !strings.Contains(fail.StdoutTail, "ghp_***") {
		t.Errorf("the tail %q does not carry the masked shape ghp_***", fail.StdoutTail)
	}
	if len(fail.StdoutTail) > 2048 {
		t.Errorf("the tail is %d bytes; the table bound is 2048", len(fail.StdoutTail))
	}
}

// TestDigestIsDeterministic — the same inputs produce the same digest; a
// changed exit code changes it. Wall time is excluded by construction, so
// two runs of the same gate a minute apart digest alike.
func TestDigestIsDeterministic(t *testing.T) {
	agent := "gate-digest-test"
	runner := Runner{Verdict: allowAllSeam(t, agent)}

	in := RunInput{Gates: []GateSpec{gate("d", "echo digest-fixed")}, Agent: agent, Dir: t.TempDir()}
	first := runner.Run(context.Background(), in)
	time.Sleep(50 * time.Millisecond)
	second := runner.Run(context.Background(), in)
	if first.Digest == "" {
		t.Fatalf("the digest is empty")
	}
	if first.Digest != second.Digest {
		t.Errorf("the same inputs digest differently: %s vs %s", first.Digest, second.Digest)
	}

	other := RunInput{Gates: []GateSpec{gate("d", "echo digest-fixed; exit 3")}, Agent: agent, Dir: t.TempDir()}
	third := runner.Run(context.Background(), other)
	if third.Digest == first.Digest {
		t.Errorf("a changed exit code left the digest unchanged: %s", third.Digest)
	}
}

// TestRunnerImportsNoStore — the onion rule, pinned at the import: nothing
// in internal/verify (tests included) reaches a store. The records are
// T3's; the runner only passes them through its seams.
func TestRunnerImportsNoStore(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("the package directory is unknown: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the package directory will not read: %v", err)
	}
	checked := 0
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("%s will not parse: %v", name, err)
		}
		for _, imp := range f.Imports {
			if strings.Contains(imp.Path.Value, "internal/store") {
				t.Errorf("%s imports a store: %s", name, imp.Path.Value)
			}
		}
		checked++
	}
	if checked == 0 {
		t.Errorf("no Go files were checked; the package is empty")
	}
}

// TestRunnerSpawnsOnlyThroughToolsSpawn — no exec in the package: a gate is
// spawned only through tools.Spawn, which is where the token check and the
// group kill live.
func TestRunnerSpawnsOnlyThroughToolsSpawn(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("the package directory is unknown: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the package directory will not read: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s will not read: %v", name, err)
		}
		// The needle is built by concatenation so this test's own source —
		// which must name the thing it bans — does not match it.
		needle := "exec." + "Command"
		if strings.Contains(string(data), needle) {
			t.Errorf("%s spawns directly; a gate goes through tools.Spawn only", name)
		}
	}
}

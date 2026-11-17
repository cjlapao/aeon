package tools

// The execution-seam tests (S59 T5).
//
// These are the tools half of "a model-run command executes with a BUILT
// environment, and cannot execute without one". The resolver half — bindings,
// the unattended rule, the backend door — is in `internal/daemon`, where the
// store is; what is proved here is the contract between `bash`, the executor
// and `Spawn`, because that seam is what makes the rest of the slice reachable
// from a command a model wrote.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
)

const (
	seamSentinel  = "s59-t5-daemon-side-sentinel-9f3x"
	seamSecretKey = "s59-t5-injected-secret-7Qk2Lm"
)

// TestSpawnRefusesAnInertEnv is EV5's one sentence, tested at the door: the
// zero `ChildEnv` does not spawn. The marker file is the proof — an error
// return alone could come from anything after the fork.
func TestSpawnRefusesAnInertEnv(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "RAN")
	token := mintSpawnToken(t, "touch "+marker)

	_, err := Spawn(context.Background(), "touch "+marker, SpawnOptions{Dir: dir}, token, execenv.ChildEnv{})
	if err == nil {
		t.Fatal("Spawn accepted an inert ChildEnv")
	}
	if !strings.Contains(err.Error(), "no execution environment was resolved") {
		t.Errorf("the refusal does not name the missing thing, so the caller cannot act on it: %v", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("the command ran: the refusal was a return value and not a boundary")
	}
}

// TestSpawnRefusesTwoEnvironments: the gate's wholesale slice and a resolved
// ChildEnv together are not resolved by field order.
func TestSpawnRefusesTwoEnvironments(t *testing.T) {
	dir := t.TempDir()
	token := mintSpawnToken(t, "true")
	_, err := Spawn(context.Background(), "true", SpawnOptions{Dir: dir, Env: []string{"PATH=" + os.Getenv("PATH")}},
		token, testScrubbedEnv(t))
	if err == nil || !strings.Contains(err.Error(), "two environments") {
		t.Fatalf("the two sources were reconciled by precedence rather than refused: %v", err)
	}
}

// TestGatePathStillScrubsWithoutAChildEnv pins the ONE pairing Spawn accepts
// without a resolved environment: the gate's scrubbed allowlist. It is the
// S57 VG5 contract, kept alive across EV5 rather than quietly folded away.
func TestGatePathStillScrubsWithoutAChildEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AEON_DAEMON_ONLY", seamSentinel)
	cmd := `printf 'PATH_SET=%s DROP=%s\n' "$([ -n "$PATH" ] && echo 1 || echo 0)" "$AEON_DAEMON_ONLY"`
	token := mintSpawnToken(t, cmd)

	res, err := Spawn(context.Background(), cmd, SpawnOptions{Dir: dir, Env: GateEnv(os.Environ())}, token,
		execenv.ChildEnv{})
	if err != nil {
		t.Fatalf("the gate path was refused: %v", err)
	}
	if !strings.Contains(res.Stdout, "PATH_SET=1") {
		t.Errorf("the scrubbed gate environment has no PATH, so no gate command could run: %q", res.Stdout)
	}
	if strings.Contains(res.Stdout, seamSentinel) {
		t.Errorf("an AEON_* daemon variable crossed the gate scrub: %q", res.Stdout)
	}
}

// TestBashStepSeesOnlyTheBuiltEnvironment is behaviour #1: the step's child is
// the environment record, not the daemon.
func TestBashStepSeesOnlyTheBuiltEnvironment(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AEON_DAEMON_ONLY", seamSentinel)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-fake-s59-t5-not-a-real-key")

	cmd := `printf 'SENTINEL=%s KEY=%s PATH_SET=%s\n' "$AEON_DAEMON_ONLY" "$ANTHROPIC_API_KEY" ` +
		`"$([ -n "$PATH" ] && echo 1 || echo 0)"`
	set, err := BuildToolset([]string{"bash"}, ToolContext{
		WorkspaceRoot:  root,
		CommandVerdict: allowAllSeam(t),
		Exec:           testExecSeam(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	res := set.Call(context.Background(), "bash", json.RawMessage(`{"command":`+quoteJSON(cmd)+`}`))
	if res.IsError {
		t.Fatalf("the scrubbed bash was refused: %s", res.Content)
	}
	if strings.Contains(res.Content, seamSentinel) {
		t.Errorf("the daemon's sentinel reached a model-driven child: %s", res.Content)
	}
	if strings.Contains(res.Content, "sk-ant-fake") {
		t.Errorf("a provider key reached a model-driven child: %s", res.Content)
	}
	if !strings.Contains(res.Content, "PATH_SET=1") {
		t.Errorf("the scrubbed child cannot find its own binaries, which is a broken environment, not a safe one: %s",
			res.Content)
	}
}

// TestBashRefusalTravelsToTheModelVerbatim: the environment's own sentence,
// unedited, as an IsError result.
func TestBashRefusalTravelsToTheModelVerbatim(t *testing.T) {
	refusal := execenv.NewRefusal(execenv.RefusalEnvironmentUnbound,
		"no execution environment is bound for this workspace",
		"there is no binding anywhere and there is no built-in default",
		"bind one")
	set, err := BuildToolset([]string{"bash"}, ToolContext{
		WorkspaceRoot:  t.TempDir(),
		CommandVerdict: allowAllSeam(t),
		Exec:           refusedExecSeam(refusal),
	})
	if err != nil {
		t.Fatal(err)
	}
	res := set.Call(context.Background(), "bash", json.RawMessage(`{"command":"echo hi"}`))
	if !res.IsError {
		t.Fatal("a refused environment produced a successful result")
	}
	if res.Content != "refused: "+refusal.What+" · "+refusal.Why+" · "+refusal.Do &&
		!strings.Contains(res.Content, refusal.What) {
		t.Errorf("the sentence was paraphrased; the model would act on a different problem than the one "+
			"the resolver named: %q", res.Content)
	}
	if strings.Contains(res.Content, "no such file") || strings.Contains(res.Content, "exit") {
		t.Errorf("a refusal to run is dressed as a command that ran: %s", res.Content)
	}
}

// TestPolicyRefusalNeverReachesAnExecutor is behaviour #4: the gate is not a
// suggestion the executor can walk past.
func TestPolicyRefusalNeverReachesAnExecutor(t *testing.T) {
	spy := &spyExecutor{}
	set, err := BuildToolset([]string{"bash"}, ToolContext{
		WorkspaceRoot:  t.TempDir(),
		CommandVerdict: denySeam(t, "rm*"),
		Exec:           func(context.Context) (CommandExecutor, error) { return spy, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	res := set.Call(context.Background(), "bash", json.RawMessage(`{"command":"rm -rf /tmp/never"}`))
	if !res.IsError {
		t.Error("a denied command did not read as an error to the model")
	}
	if spy.calls != 0 {
		t.Fatalf("the executor was called %d time(s) for a denied command; the policy gate is a note, not a "+
			"boundary", spy.calls)
	}
}

// TestSecretInjectedAndRedactedEverywhere is behaviour #5 in one run: the
// value REACHES the child (the injection works) and does not REACH the
// transcript in any of its three shapes — stdout, stderr, and the command the
// model itself wrote.
func TestSecretInjectedAndRedactedEverywhere(t *testing.T) {
	root := t.TempDir()
	env := envWithSecrets(t, "db-url", "DATABASE_URL", seamSecretKey)

	cmd := `echo "stdout:$DATABASE_URL"; echo "stderr:$DATABASE_URL" >&2; printf 'argv:%s\n' "$DATABASE_URL"`
	set, err := BuildToolset([]string{"bash"}, ToolContext{
		WorkspaceRoot:  root,
		CommandVerdict: allowAllSeam(t),
		Exec: func(context.Context) (CommandExecutor, error) {
			return HostExecutor{Env: env, Redactor: execenv.NewRedactor(mustInjections(t, "db-url", "DATABASE_URL",
				seamSecretKey))}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res := set.Call(context.Background(), "bash", json.RawMessage(`{"command":`+quoteJSON(cmd)+`}`))
	if res.IsError {
		t.Fatalf("the secret step was refused: %s", res.Content)
	}
	if strings.Contains(res.Content, seamSecretKey) {
		t.Errorf("the injected secret reached the transcript: %s", res.Content)
	}
	// The child really had it — otherwise "redacted" is just "never there".
	if !strings.Contains(res.Content, "stdout:[secret:db-url]") ||
		!strings.Contains(res.Content, "stderr:[secret:db-url]") ||
		!strings.Contains(res.Content, "argv:[secret:db-url]") {
		t.Errorf("the three sinks did not each pass through the filter: %s", res.Content)
	}
}

// TestExecDirIsTheStepRoot is behaviour #9 and S32's follow-up: the directory
// comes from the request, once, and an empty one is refused rather than the
// daemon's own.
func TestExecDirIsTheStepRoot(t *testing.T) {
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolved = root // a platform without symlinks; the value is already canonical
	}
	token := mintSpawnToken(t, "pwd")
	res, err := HostExecutor{Env: testScrubbedEnv(t)}.Run(context.Background(),
		ExecRequest{Command: "pwd", Dir: root}, token)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(res.Stdout)
	if want := filepath.Base(resolved); !strings.HasSuffix(got, want) {
		t.Errorf("the command ran in %q, not under the step root %q", got, root)
	}

	if _, err := (HostExecutor{Env: testScrubbedEnv(t)}).Run(context.Background(),
		ExecRequest{Command: "pwd"}, mintSpawnToken(t, "pwd")); err == nil ||
		!strings.Contains(err.Error(), "working directory") {
		t.Errorf("an empty Dir did not read as the bound it is: %v", err)
	}
}

// TestExecRequestCarriesNoEnvironment is EV9's shape, checked structurally:
// a field that could carry an environment is a door that one eventually will.
func TestExecRequestCarriesNoEnvironment(t *testing.T) {
	typ := reflect.TypeOf(ExecRequest{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if strings.Contains(name, "Env") || strings.Contains(name, "Secret") ||
			strings.Contains(name, "Credential") || strings.Contains(name, "Token") {
			t.Errorf("ExecRequest has a field %q: the request carries the command and its bounds, and "+
				"anything else is a second source of the environment", name)
		}
	}
	if _, ok := reflect.TypeOf(SpawnOptions{}).FieldByName("ChildEnv"); ok {
		t.Error("SpawnOptions grew a ChildEnv field: the environment is a positional parameter, not an option")
	}
}

// TestBashWithoutExecSeamIsInert is the storeless daemon's bash: no seam, no
// spawn — the same fail-closed posture as a missing policy source.
func TestBashWithoutExecSeamIsInert(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "RAN")
	set, err := BuildToolset([]string{"bash"}, ToolContext{
		WorkspaceRoot:  root,
		CommandVerdict: allowAllSeam(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	res := set.Call(context.Background(), "bash", json.RawMessage(`{"command":"touch `+marker+`"}`))
	if !res.IsError {
		t.Fatal("a toolset with no execution seam spawned")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the command ran anyway")
	}
	if !strings.Contains(res.Content, "Execution") && !strings.Contains(res.Content, "execution-environment") {
		t.Errorf("the refusal does not name what is missing: %s", res.Content)
	}
}

// --- helpers ---------------------------------------------------------------

type spyExecutor struct {
	calls   int
	lastReq ExecRequest
}

func (s *spyExecutor) Run(_ context.Context, req ExecRequest, _ policy.AllowToken) (RunResult, error) {
	s.calls++
	s.lastReq = req
	return RunResult{ExitCode: 0, Stdout: "spy"}, nil
}
func (s *spyExecutor) Names() []execenv.Var {
	return []execenv.Var{{Name: "PATH", Source: "passthrough"}}
}
func (s *spyExecutor) Redact(text string) string       { return text }
func (s *spyExecutor) Redactions() []execenv.NameCount { return nil }

func quoteJSON(s string) string { return fmt.Sprintf("%q", s) }

// mustInjections resolves the same reference the environment was built with, so
// a test that builds an environment with a secret and an executor that filters
// it cannot accidentally filter a different value than the one injected.
func mustInjections(t *testing.T, name, injectAs, value string) []execenv.Injection {
	t.Helper()
	inj, r := execenv.ResolveRef(
		execenv.RefSpec{Name: name, SourceKind: "daemon-env", Source: injectAs, InjectAs: injectAs},
		execenv.ResolveEnv{
			Lookup:      func(string) string { return value },
			DaemonUID:   os.Getuid(),
			Credentials: execenv.DaemonCredentials{Provider: []string{"ANTHROPIC_API_KEY"}, Forge: []string{"GH_TOKEN"}},
		})
	if r != nil {
		t.Fatalf("the reference was refused: %s", r.Sentence())
	}
	return []execenv.Injection{inj}
}

package container

// The behaviour table of S59 T9 (#1–#10), read off the fake runtime's recorded
// command log. The log is the oracle: a flag that is not there is not there, and
// a value that reached argv is a value in the process list.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

const (
	sentinelSet   = "SET-VALUE-sentinel-8f21c4"
	sentinelSec   = "SECRET-VALUE-sentinel-1b7d99"
	secretRefName = "gh-token"
)

// allowToken mints a REAL token through the real engine. A hand-built
// `policy.AllowToken{}` refuses, and a test whose token refuses everything is a
// test that asserts nothing.
func allowToken(tb testing.TB, command string) policy.AllowToken {
	tb.Helper()
	spec, err := json.Marshal(map[string]string{"command": command, "state": "allow"})
	if err != nil {
		tb.Fatalf("helper built an unreadable spec: %v", err)
	}
	target, rej := policy.ParseTarget(policy.AllAgents, "")
	if rej != nil {
		tb.Fatalf("helper built an illegal target: %s", rej.Reason)
	}
	act, rej := policy.ParseAction("allow")
	if rej != nil {
		tb.Fatalf("helper built an illegal action: %s", rej.Reason)
	}
	v := policy.Evaluate([]policy.Policy{{
		ID: "container-test-allow", Type: policy.CommandAccess, Target: target, Action: act,
		Spec: spec, Source: policy.Operator,
	}}, policy.Request{Type: policy.CommandAccess, Agent: "container-test", Tool: "bash", Command: command}, time.Now())
	tok, ok := v.Token(command)
	if !ok {
		tb.Fatalf("the harness's allow refused to mint a token for %q", command)
	}
	return tok
}

// specFor is the environment under test: one image, the caps, and the two
// sentinels — one through `set`, one as an injected reference, because #5 names
// both sources and a test that only walks one proves half the claim.
func specFor(egressMode, pull string) execenv.Spec {
	wall := 0
	mem := 512
	cpus := 1.5
	return execenv.Spec{
		Env:    execenv.EnvSection{Set: map[string]string{"APP_MODE": sentinelSet}},
		Limits: execenv.Limits{WallSeconds: ptr(wall), MemoryMiB: &mem, CPUs: &cpus},
		Egress: execenv.Egress{Mode: egressMode},
		Image:  &execenv.Image{Ref: "python:3.12-slim", Pull: pull},
	}
}

func ptr(i int) *int { return &i }

// tbTempDir is testing.TB's own TempDir, which has no method set to call
// through an interface.
func tbTempDir(tb testing.TB) string {
	if t, ok := tb.(*testing.T); ok {
		return t.TempDir()
	}
	tb.Fatalf("the harness expects a *testing.T")
	return ""
}

func testEnv(egressMode, pull string) execenv.BoundEnv {
	return execenv.BoundEnv{ID: "env-1", Slug: "python-worker", Backend: execenv.BackendContainer, Spec: specFor(egressMode, pull)}
}

// testRefs resolves the secret reference through the SAME door the daemon uses
// (`execenv.ResolveAll` over a real 0600 key file), so #5 and #6 walk a value
// that came from the reference plane and not from a test convenience: an
// injection minted here is the injection the daemon would have minted, and the
// redactor is built from that same list — the one the child was built from.
func testRefs(tb testing.TB) []execenv.Injection {
	tb.Helper()
	dir := tbTempDir(tb)
	path := filepath.Join(dir, "gh-token")
	if err := os.WriteFile(path, []byte(sentinelSec), 0o600); err != nil {
		tb.Fatalf("the key file could not be staged: %v", err)
	}
	injections, refusals := execenv.ResolveAll([]execenv.RefSpec{{
		Name: secretRefName, SourceKind: "key-file", Source: path, InjectAs: "APP_TOKEN",
	}}, execenv.ResolveEnv{
		Lookup: func(string) string { return "" },
		Files:  execenv.OSFiles{}, DaemonUID: os.Getuid(),
	})
	if len(refusals) > 0 {
		tb.Fatalf("the reference refused to resolve: %s", refusals[0].Sentence())
	}
	if len(injections) != 1 {
		tb.Fatalf("the resolver produced %d injections, expected 1", len(injections))
	}
	return injections
}

func testChild(tb testing.TB, spec execenv.Spec) execenv.ChildEnv {
	tb.Helper()
	child, ref := execenv.Build(spec, execenv.DaemonEnv{
		Lookup: func(string) string { return "" },
		Names:  func() []string { return nil },
	}, testRefs(tb))
	if ref != nil {
		tb.Fatalf("the test environment refused to build: %s", ref.Sentence())
	}
	return child
}

// rootDir is the loop's working root. It is a real directory because the
// prepared command is a real child and `tools.RunPrepared` refuses to start one
// whose directory does not exist — a fake path would make every run fail for the
// wrong reason, and a guard that fires everywhere proves nothing.
func rootDir(tb testing.TB) string {
	tb.Helper()
	return tbTempDir(tb)
}

// harness is one loop, one environment, one provisioner over the two fakes.
func harness(tb testing.TB, egressMode, pull string) (*Provisioner, *fakeRuntime, *fakeLedger, *Executor, string) {
	tb.Helper()
	dir := rootDir(tb)
	lg := newFakeLedger()
	rt := &fakeRuntime{log: lg.log, imageExists: true, execOutput: "hello"}
	p := &Provisioner{Ledger: lg, Runtime: rt, User: "1000:1000"}
	spec := specFor(egressMode, pull)
	ex := &Executor{
		P: p, Loop: LoopRef{ComponentRoot: dir, LoopSlug: "loop-a", Owner: "step-1"},
		Env: testEnv(egressMode, pull), Child: testChild(tb, spec), WorkDir: dir,
	}
	return p, rt, lg, ex, dir
}

// #1–#4: the run's argv, every hardening flag, exactly one mount.
func TestContainerRunArgvGolden(t *testing.T) {
	_, rt, _, ex, dir := harness(t, "none", execenv.PullNever)
	ctx := context.Background()
	for _, cmd := range []string{"echo one", "echo two"} {
		if _, err := ex.Run(ctx, tools.ExecRequest{Command: cmd, Dir: dir}, allowToken(t, cmd)); err != nil {
			t.Fatalf("the %s command failed: %v", cmd, err)
		}
	}
	// One container per loop: two commands, ONE `docker run`.
	if n := rt.log.count("runtime", "create"); n != 1 {
		t.Fatalf("two commands produced %d containers; a per-loop backend runs one per loop (#1)", n)
	}
	runs := rt.log.argvOf("create")
	if len(runs) != 1 {
		t.Fatalf("the log recorded %d run argvs, expected 1", len(runs))
	}
	joined := strings.Join(runs[0], " ")
	wants := []string{"run", "-d", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--user", "1000:1000", "--pids-limit", "--memory", "512m", "--cpus", "1.5",
		"--network", "none", "-w", "/workspace",
		"--label", "aeon.managed=1", "--label", "aeon.loop=loop-a",
		"--entrypoint", "/bin/sh", "python:3.12-slim", "-c", "sleep infinity",
		"-v", dir + ":/workspace:rw"}
	for _, w := range wants {
		if !strings.Contains(joined, w) {
			t.Errorf("the run argv lost %q:\n%s", w, joined)
		}
	}
	if !strings.Contains(joined, "aeon-loop-a-prov-1") {
		t.Errorf("the container is not named after its ledger row (%s):\n%s", "aeon-<slug>-<provisionId>", joined)
	}
	// The names in argv, exactly the two declared variables — and no `-v`
	// besides the one mount.
	if n := countFlag(runs[0], "-v"); n != 1 {
		t.Errorf("the container mounted %d paths; the working directory is the only mount (EV3):\n%s", n, joined)
	}
	if n := countFlag(runs[0], "--env"); n != 2 {
		t.Errorf("the run declared %d variables, expected 2 (APP_MODE, APP_TOKEN):\n%s", n, joined)
	}
	// The forbidden set, walked over EVERY argv the driver ever built.
	for _, argv := range allArgv(rt.log) {
		j := strings.Join(argv, " ")
		for _, bad := range []string{"--privileged", "docker.sock", "--net=host", "--network=host", "--pid=host", "--uts=host", "--cap-add"} {
			if strings.Contains(j, bad) {
				t.Errorf("an argv carries %q — this driver cannot produce it, and here it did:\n%s", bad, j)
			}
		}
	}
}

// The `--network` mapping, isolated: `none` constrains, the rest is the engine's
// default bridge and is not dressed up as more than that.
func TestNetworkModeFollowsEgress(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want bool
	}{{"none", true}, {"unenforced", false}, {"unrestricted", false}} {
		_, rt, _, ex, dir := harness(t, tc.mode, execenv.PullNever)
		if _, err := ex.Run(context.Background(), tools.ExecRequest{Command: "echo n", Dir: dir}, allowToken(t, "echo n")); err != nil {
			t.Fatalf("%s: %v", tc.mode, err)
		}
		j := strings.Join(rt.log.argvOf("create")[0], " ")
		if got := strings.Contains(j, "--network none"); got != tc.want {
			t.Errorf("egress %s: --network none present=%v, want %v:\n%s", tc.mode, got, tc.want, j)
		}
	}
}

// #5: no sentinel in any argv, from either source — and the sentinels really
// were in play (the process environment of the same call carries them), or the
// walk would be green on an empty stage.
func TestNoSecretInAnyArgv(t *testing.T) {
	_, rt, _, ex, dir := harness(t, "unenforced", execenv.PullNever)
	if _, err := ex.Run(context.Background(), tools.ExecRequest{Command: "echo go", Dir: dir}, allowToken(t, "echo go")); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	argvs := allArgv(rt.log)
	if len(argvs) < 2 {
		t.Fatalf("the walk had %d argvs to read; nothing was staged", len(argvs))
	}
	for _, argv := range argvs {
		for _, a := range argv {
			if strings.Contains(a, sentinelSet) || strings.Contains(a, sentinelSec) {
				t.Errorf("an argv element carries a value — it is in the process list now:\n%v", argv)
			}
		}
	}
	var sawSet, sawSecret bool
	for _, o := range rt.log.all() {
		for _, e := range o.Env {
			if strings.Contains(e, sentinelSet) {
				sawSet = true
			}
			if strings.Contains(e, sentinelSec) {
				sawSecret = true
			}
		}
	}
	if !sawSet || !sawSecret {
		t.Errorf("the values never reached the engine client's environment either (set=%v secret=%v) — the test staged nothing", sawSet, sawSecret)
	}
}

// #6: the bytes a container command prints go through the SAME capture, and the
// SAME filter. The control (no redactor) proves the pipe really carries them;
// without it the redaction assertion could pass on an empty stdout.
func TestContainerOutputGoesThroughTheSharedBoundary(t *testing.T) {
	lg := newFakeLedger()
	rt := &fakeRuntime{log: lg.log, imageExists: true, execOutput: "echo printed-" + sentinelSec + "-end"}
	p := &Provisioner{Ledger: lg, Runtime: rt, User: "1000:1000"}
	spec := specFor("unenforced", execenv.PullNever)
	child := testChild(t, spec)

	dir := rootDir(t)
	plain := &Executor{P: p, Env: testEnv("unenforced", execenv.PullNever), Child: child, WorkDir: dir}
	res, err := plain.Run(context.Background(), tools.ExecRequest{Command: "echo", Dir: dir}, allowToken(t, "echo"))
	if err != nil {
		t.Fatalf("the control run failed: %v", err)
	}
	if !strings.Contains(res.Stdout, sentinelSec) {
		t.Fatalf("the control run did not print the secret (%q) — the test can no longer prove redaction", res.Stdout)
	}

	filtered := &Executor{P: p, Env: testEnv("unenforced", execenv.PullNever), Child: child, WorkDir: dir,
		Red: execenv.NewRedactor(testRefs(t))}
	res2, err := filtered.Run(context.Background(), tools.ExecRequest{Command: "echo again", Dir: dir}, allowToken(t, "echo again"))
	if err != nil {
		t.Fatalf("the filtered run failed: %v", err)
	}
	if strings.Contains(res2.Stdout, sentinelSec) {
		t.Errorf("the container's output escaped the shared filter (EV8):\n%s", res2.Stdout)
	}
	if len(filtered.Redactions()) == 0 {
		t.Errorf("the filter matched nothing, so the pass above proved nothing about it")
	}
}

// #7: an unavailable engine refuses. The command does NOT run on the host.
func TestUnavailableRuntimeRefusesNeverRunsOnHost(t *testing.T) {
	lg := newFakeLedger()
	rt := &fakeRuntime{log: lg.log, availableErr: errors.New("Cannot connect to the Docker daemon")}
	p := &Provisioner{Ledger: lg, Runtime: rt, User: "1000:1000"}
	_, ref := p.Ensure(context.Background(), LoopRef{ComponentRoot: "/work", LoopSlug: "loop-a", Owner: "s"}, testEnv("unenforced", execenv.PullNever), "/work", nil)
	if ref == nil {
		t.Fatal("an unavailable engine was accepted")
	}
	if ref.Kind != execenv.RefusalEnvironmentUnavailable {
		t.Errorf("the refusal kind is %q, want %q", ref.Kind, execenv.RefusalEnvironmentUnavailable)
	}
	for _, want := range []string{"Cannot connect to the Docker daemon", "not at all"} {
		if !strings.Contains(ref.Sentence(), want) {
			t.Errorf("the refusal does not carry the engine's own reason or the no-host-fallback clause (%q):\n%s", want, ref.Sentence())
		}
	}
	for _, op := range []string{"create", "exec", "start"} {
		if n := rt.log.count("runtime", op); n != 0 {
			t.Errorf("the refusal still %sd %d %s calls — a container that could not start means nothing ran (EV13)", op, n, op)
		}
	}
}

// #8: `pull: never` names the image and stops; `missing` pulls once.
func TestImagePolicy(t *testing.T) {
	lg := newFakeLedger()
	rt := &fakeRuntime{log: lg.log, imageExists: false}
	p := &Provisioner{Ledger: lg, Runtime: rt, User: "1000:1000"}
	_, ref := p.Ensure(context.Background(), LoopRef{ComponentRoot: "/w", LoopSlug: "l", Owner: "s"}, testEnv("unenforced", execenv.PullNever), "/w", nil)
	if ref == nil || ref.Kind != execenv.RefusalProvisionFailed {
		t.Fatalf("an absent image with pull=never was accepted: %+v", ref)
	}
	if !strings.Contains(ref.Sentence(), "python:3.12-slim") {
		t.Errorf("the refusal does not name the image: %s", ref.Sentence())
	}
	if n := rt.log.count("runtime", "pull"); n != 0 {
		t.Errorf("pull=never pulled %d times — a registry fetch a person forbade", n)
	}

	lg2 := newFakeLedger()
	rt2 := &fakeRuntime{log: lg2.log, imageExists: false}
	p2 := &Provisioner{Ledger: lg2, Runtime: rt2, User: "1000:1000"}
	if _, ref := p2.Ensure(context.Background(), LoopRef{ComponentRoot: "/w", LoopSlug: "l", Owner: "s"}, testEnv("unenforced", execenv.PullMissing), "/w", nil); ref != nil {
		t.Fatalf("pull=missing failed: %s", ref.Sentence())
	}
	if n := rt2.log.count("runtime", "pull"); n != 1 {
		t.Errorf("pull=missing pulled %d times, expected exactly 1", n)
	}
}

// #9 (the ledger half): write-ahead, and settle-once.
func TestProvisionWriteAheadAndSettleOnce(t *testing.T) {
	lg := newFakeLedger()
	rt := &fakeRuntime{log: lg.log, imageExists: true}
	p := &Provisioner{Ledger: lg, Runtime: rt, User: "1000:1000"}
	if _, ref := p.Ensure(context.Background(), LoopRef{ComponentRoot: "/w", LoopSlug: "l", Owner: "s"}, testEnv("unenforced", execenv.PullNever), "/w", nil); ref != nil {
		t.Fatalf("ensure failed: %s", ref.Sentence())
	}
	whats := lg.log.all()
	firstRow, firstCreate := -1, -1
	for i, o := range whats {
		if o.What == "CreateProvision" && firstRow < 0 {
			firstRow = i
		}
		if o.What == "create" && firstCreate < 0 {
			firstCreate = i
		}
	}
	if firstRow < 0 || firstCreate < 0 {
		t.Fatalf("the log is missing the row (%d) or the container (%d) — the claim cannot be checked", firstRow, firstCreate)
	}
	if firstRow > firstCreate {
		t.Errorf("the container was created BEFORE the ledger row (ops %d and %d) — EV12 says the row is written first", firstRow, firstCreate)
	}

	// A crash after the row, before the container: the row is not `ready` and it
	// says why.
	lg2 := newFakeLedger()
	rt2 := &fakeRuntime{log: lg2.log, imageExists: true}
	lg2.crash("ready", nil)
	p2 := &Provisioner{Ledger: lg2, Runtime: rt2, User: "1000:1000"}
	if _, ref := p2.Ensure(context.Background(), LoopRef{ComponentRoot: "/w", LoopSlug: "l", Owner: "s"}, testEnv("unenforced", execenv.PullNever), "/w", nil); ref == nil {
		t.Fatal("a ledger that refused the ready mark still reported success")
	}
	row := lg2.rows["prov-1"]
	if row == nil {
		t.Fatal("the crash left no row at all — a container with no row is the leak EV12 refuses")
	}
	if row.State == store.ProvisionStateReady {
		t.Errorf("the row claims ready after its mark was refused")
	}
	if row.LastError == nil || !strings.Contains(*row.LastError, "staged crash") {
		t.Errorf("the failed row carries no driver sentence: %v", row.LastError)
	}

	// Teardown: tearing_down → remove → removed, in that order, and a repeat is
	// a no-op rather than a second removal.
	lg3 := newFakeLedger()
	rt3 := &fakeRuntime{log: lg3.log, imageExists: true}
	p3 := &Provisioner{Ledger: lg3, Runtime: rt3, User: "1000:1000"}
	if _, ref := p3.Ensure(context.Background(), LoopRef{ComponentRoot: "/w", LoopSlug: "l", Owner: "s"}, testEnv("unenforced", execenv.PullNever), "/w", nil); ref != nil {
		t.Fatalf("ensure failed: %s", ref.Sentence())
	}
	if _, ref := p3.Teardown(context.Background(), "prov-1"); ref != nil {
		t.Fatalf("teardown failed: %s", ref.Sentence())
	}
	if _, ref := p3.Teardown(context.Background(), "prov-1"); ref != nil {
		t.Errorf("the second teardown was not a no-op: %s", ref.Sentence())
	}
	if n := rt3.log.count("runtime", "remove"); n != 1 {
		t.Errorf("the container was removed %d times, expected 1 (settle-once)", n)
	}
	order := []string{}
	for _, o := range lg3.log.all() {
		if o.Actor == "ledger" || o.What == "remove" {
			order = append(order, o.What)
		}
	}
	want := []string{"CreateProvision", "MarkProvisionReady", "MarkProvisionTearingDown", "remove", "MarkProvisionRemoved"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("the lifecycle went %v, want %v (reverse order, EV15)", order, want)
	}
}

// The mount rule: a loop whose root moved gets a fresh provision, not the old
// container.
func TestWorkDirMustMatchTheProvision(t *testing.T) {
	_, _, _, ex, dir := harness(t, "unenforced", execenv.PullNever)
	ctx := context.Background()
	if _, err := ex.Run(ctx, tools.ExecRequest{Command: "echo here", Dir: dir}, allowToken(t, "echo here")); err != nil {
		t.Fatalf("the first command failed: %v", err)
	}
	_, err := ex.Run(ctx, tools.ExecRequest{Command: "echo there", Dir: "/somewhere/else"}, allowToken(t, "echo there"))
	if err == nil {
		t.Fatal("a command ran in a directory the provision never mounted")
	}
	if !strings.Contains(err.Error(), "mounted root") || !strings.Contains(err.Error(), "new provision") {
		t.Errorf("the refusal does not say what to do about it: %v", err)
	}
}

// `wallSeconds` is a CEILING on the tool's own timeout, enforced by the shared
// path that kills the process group.
func TestWallSecondsShortensTheTimeout(t *testing.T) {
	lg := newFakeLedger()
	rt := &fakeRuntime{log: lg.log, imageExists: true, execOutput: "sleep 30"}
	p := &Provisioner{Ledger: lg, Runtime: rt, User: "1000:1000"}
	env := testEnv("unenforced", execenv.PullNever)
	env.Spec.Limits.WallSeconds = ptr(1) // the ceiling under test
	dir := rootDir(t)
	ex := &Executor{P: p, Env: env, Child: testChild(t, env.Spec), WorkDir: dir}
	start := time.Now()
	res, err := ex.Run(context.Background(), tools.ExecRequest{Command: "sleep 30", Dir: dir, Timeout: time.Hour}, allowToken(t, "sleep 30"))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("the run returned an error instead of a timed-out result: %v", err)
	}
	if !res.TimedOut {
		t.Errorf("a 30-second sleep under wallSeconds=1 came back without TimedOut: %+v", res)
	}
	if elapsed > 8*time.Second {
		t.Errorf("the ceiling did not bite: it took %s", elapsed)
	}
}

// The engine client's own environment is a LIST, not an inheritance: PATH and
// HOME because the CLI cannot work without them, the declared pairs because that
// is how the values travel — and nothing else, whatever the daemon holds.
func TestExecArgvCarriesNamesNotValues(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "DO-NOT-LEAK-9931")
	argv := execArgv("docker", ExecSpec{Handle: "ctr-1", Command: "echo hi", WorkDir: "/workspace", User: "1000:1000",
		Env: []Arg{{Name: "APP_MODE", Value: sentinelSet}, {Name: "APP_TOKEN", Value: sentinelSec}}})
	j := strings.Join(argv, " ")
	if strings.Contains(j, sentinelSet) || strings.Contains(j, sentinelSec) {
		t.Errorf("exec argv carries a value:\n%s", j)
	}
	if c := countFlag(argv, "--env"); c != 2 {
		t.Errorf("exec declared %d variables by name, expected 2:\n%s", c, j)
	}
	env := cliProcessEnv([]Arg{{Name: "APP_MODE", Value: sentinelSet}})
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "APP_MODE="+sentinelSet) {
		t.Errorf("the declared value did not travel in the client's environment:\n%s", joined)
	}
	if strings.Contains(joined, "DO-NOT-LEAK-9931") || strings.Contains(joined, "AWS_SECRET_ACCESS_KEY") {
		t.Errorf("the engine client inherited a daemon credential (EV10):\n%s", joined)
	}
	for _, e := range env {
		name, _, ok := strings.Cut(e, "=")
		if !ok {
			t.Errorf("the client environment carries a malformed entry")
			continue
		}
		if name != "PATH" && name != "HOME" && name != "APP_MODE" {
			t.Errorf("the client environment carries the unexplained name %q — two names plus the declared ones is the whole rule", name)
		}
	}
}

// EV3, drawn in code: one mount, at /workspace, or the container is not created.
func TestValidateMountsRefusesEverythingElse(t *testing.T) {
	ok := []Mount{{Host: "/work/comp", Container: "/workspace"}}
	if r := ValidateMounts(ok); r != nil {
		t.Errorf("the one legal mount was refused: %s", r.Sentence())
	}
	bad := [][]Mount{
		nil,
		{{Host: "/work/comp", Container: "/workspace"}, {Host: "/etc", Container: "/etc", ReadOnly: true}},
		{{Host: "", Container: "/workspace"}},
		{{Host: "/work/comp", Container: "/"}},
		{{Host: "/var/run/docker.sock", Container: "/workspace"}},
		{{Host: "/home/carlos", Container: "/workspace"}},
	}
	for i, m := range bad {
		if r := ValidateMounts(m); r == nil {
			t.Errorf("mount set %d was accepted: %+v (EV3 allows exactly one, the working directory, at /workspace)", i, m)
		}
	}
}

// The builders are pure, so the forbidden flags are not merely absent from a run:
// they cannot be produced from a CreateSpec at all. This walks the source for a
// second capture path, which is the failure EV13 is about.
func TestNoSecondCapturePathInThePackage(t *testing.T) {
	for _, name := range []string{"cli_runtime.go", "executor.go", "provision.go", "reconcile.go", "runtime.go"} {
		src := readSource(t, name)
		if name == "cli_runtime.go" {
			continue // the engine CLIENT's own calls are its business, recorded and walked above
		}
		for _, bad := range []string{".Output()", ".CombinedOutput()", "cmd.Run()", "cmd.Start()", ".Output("} {
			if strings.Contains(src, bad) {
				t.Errorf("%s reaches a process directly (%s) — every command goes through tools.RunPrepared (EV13)", name, bad)
			}
		}
	}
}

// --- helpers -------------------------------------------------------------------

func countFlag(argv []string, flag string) int {
	n := 0
	for _, a := range argv {
		if a == flag {
			n++
		}
	}
	return n
}

func allArgv(l *log) [][]string {
	var out [][]string
	for _, o := range l.all() {
		if len(o.Argv) > 0 {
			out = append(out, o.Argv)
		}
	}
	return out
}

// readSource reads a production file of this package, so the "no second capture
// path" walk reads the real thing rather than a remembered copy of it.
func readSource(tb testing.TB, name string) string {
	tb.Helper()
	b, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		tb.Fatalf("the walk could not read %s: %v", name, err)
	}
	if len(b) < 200 {
		tb.Fatalf("%s read as %d bytes — a walk over an empty file proves nothing", name, len(b))
	}
	return string(b)
}

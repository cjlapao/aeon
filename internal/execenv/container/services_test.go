package container

// T10 · the per-loop services (EV14). The oracle is the argv log the fake
// runtime keeps: it holds what `docker` would have been handed, so a published
// port, a mount, a secret value or a missing `--network-alias` is visible
// without an engine.

import (
	"context"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

const (
	svcImage      = "postgres:16"
	svcSecretName = "DB_PASSWORD"
	svcSecretVal  = "PG-SECRET-sentinel-6a3e10"
)

// serviceHarness is one loop bound to an environment that declares a database
// and a queue. The Probe door is wired the way the daemon wires it — a minted
// token and the shared capture path — because a health check that captured
// output on its own would be the second boundary this slice forbids.
func serviceHarness(t *testing.T, health *execenv.Health) (*Provisioner, *fakeRuntime, *fakeLedger, *Executor, string) {
	t.Helper()
	p, rt, lg, ex, dir := harness(t, "none", execenv.PullNever)
	ex.Env.Spec.Services = []execenv.Service{
		{Name: "db", Image: svcImage, Port: 5432, Health: health, Secrets: []string{"gh-token"}},
		{Name: "queue", Image: "redis:7", Port: 6379},
	}
	p.ServiceArgs = func(env execenv.BoundEnv, svc execenv.Service) ([]Arg, *execenv.Refusal) {
		if svc.Name != "db" {
			return nil, nil
		}
		return []Arg{{Name: svcSecretName, Value: svcSecretVal}}, nil
	}
	p.Probe = func(ctx context.Context, cmd *exec.Cmd, command string) (tools.RunResult, *execenv.Refusal) {
		res, err := tools.RunPrepared(ctx, cmd, tools.PreparedOptions{Command: command, Timeout: 5 * time.Second},
			allowToken(t, command), execenv.NewRedactor(nil))
		if err != nil {
			return tools.RunResult{}, execenv.NewRefusal(execenv.RefusalProvisionFailed,
				"the health probe could not be prepared: "+err.Error(),
				"a probe that never ran has not proven anything about the service", "fix the probe and run again")
		}
		return res, nil
	}
	return p, rt, lg, ex, dir
}

func runOnce(t *testing.T, ex *Executor, dir string) {
	t.Helper()
	cmd := "make it so"
	if _, err := ex.Run(context.Background(), tools.ExecRequest{Command: cmd, Dir: dir}, allowToken(t, cmd)); err != nil {
		t.Fatalf("the loop's command failed: %v", err)
	}
}

func svcRows(t *testing.T, lg *fakeLedger) map[string]store.Provision {
	t.Helper()
	rows, err := lg.ListLiveProvisions()
	if err != nil {
		t.Fatalf("the ledger would not answer: %v", err)
	}
	by := map[string]store.Provision{}
	for _, r := range rows {
		by[r.Owner] = r
	}
	return by
}

// #1 — network, then each healthy service, then the app container; the reverse
// on the way down; one ledger row per resource.
func TestServiceProvisionOrderAndTeardownReverse(t *testing.T) {
	p, rt, lg, ex, dir := serviceHarness(t, &execenv.Health{TCP: 5432, TimeoutSeconds: 2})
	runOnce(t, ex, dir)

	order := rt.log.whats("runtime")
	at := func(what string) int {
		for i, w := range order {
			if w == what {
				return i
			}
		}
		t.Fatalf("no %q in the engine log (%v)", what, order)
		return -1
	}
	if !(at("network-create") < at("create-service") && at("create-service") < at("create")) {
		t.Errorf("the order was %v; the network exists before a service joins it and a service is up before the app runs (#1)", order)
	}
	rows := svcRows(t, lg)
	for _, owner := range []string{store.ProvisionOwnerNetwork, "service:db", "service:queue", store.ProvisionOwnerLoop} {
		if _, ok := rows[owner]; !ok {
			t.Errorf("no ledger row owns %s; a resource with no row is a resource reconcile cannot find (#1)", owner)
		}
	}

	// Down in the reverse order, and the network last: it cannot be removed
	// while anything is attached to it.
	live := mustRows(t, lg)
	owners := []string{}
	for _, r := range live {
		owners = append(owners, r.Owner)
	}
	t.Logf("the live rows and their owners: %v", owners)
	// The order the daemon's sweep uses: the app first, then the services, the
	// network last.
	sort.SliceStable(live, func(i, j int) bool {
		if TeardownRank(live[i].Owner) != TeardownRank(live[j].Owner) {
			return TeardownRank(live[i].Owner) < TeardownRank(live[j].Owner)
		}
		return live[i].ID < live[j].ID
	})
	ids := []string{}
	for _, r := range live {
		ids = append(ids, r.ID)
	}
	for _, id := range ids {
		if _, ref := p.Teardown(context.Background(), id); ref != nil {
			t.Fatalf("teardown of %s failed: %s", id, ref.Sentence())
		}
	}
	downs := rt.log.whats("runtime")
	last := func(what string) int {
		n := -1
		for i, w := range downs {
			if w == what {
				n = i
			}
		}
		return n
	}
	if !(last("remove") < last("network-rm")) {
		t.Errorf("the network came down before the containers (%v); a network with endpoints attached cannot be removed", downs)
	}
	if live := svcRows(t, lg); len(live) != 0 {
		t.Errorf("%d rows are still live after a full teardown", len(live))
	}
	if n := len(rt.netList); n != 0 {
		t.Errorf("%d networks are still there after a full teardown (#1)", n)
	}
}

// #2 — an unhealthy service stops the loop and leaves nothing behind.
func TestUnhealthyServiceFailsProvisioningCleanly(t *testing.T) {
	p, rt, lg, ex, dir := serviceHarness(t, &execenv.Health{TCP: 5432, TimeoutSeconds: 1})
	rt.probeOutput = "exit 1" // the probe container's `sh -c` fails, as a refused connection does
	cmd := "make it so"
	_, err := ex.Run(context.Background(), tools.ExecRequest{Command: cmd, Dir: dir}, allowToken(t, cmd))
	if err == nil {
		t.Fatal("a loop started against a database that never answered")
	}
	if !strings.Contains(err.Error(), execenv.RefusalServiceUnhealthy) || !strings.Contains(err.Error(), "db") {
		t.Errorf("the refusal is %q; it must be service-unhealthy and name the service (#2)", err)
	}
	if rt.log.count("runtime", "create-service") == 0 {
		t.Error("no service container was created, so the probe proved nothing")
	}
	if rt.log.count("runtime", "probe") == 0 {
		t.Error("the health probe never ran, so the verdict was invented")
	}
	if rt.log.count("runtime", "remove") < rt.log.count("runtime", "create-service") {
		t.Errorf("the rollback removed %d of the %d resources the run created (#2)",
			rt.log.count("runtime", "remove"), rt.log.count("runtime", "create-service"))
	}
	if n := len(rt.netList); n != 0 {
		t.Errorf("%d loop networks leaked from a failed provision (#2)", n)
	}
	for _, r := range mustRows(t, lg) {
		if r.State == store.ProvisionStateReady {
			t.Errorf("the row %s (%s) still reads ready after the failure", r.ID, r.Owner)
		}
	}
	_ = p
}

func mustRows(t *testing.T, lg *fakeLedger) []store.Provision {
	t.Helper()
	rows, err := lg.ListLiveProvisions()
	if err != nil {
		t.Fatalf("the ledger would not answer: %v", err)
	}
	return rows
}

// #3 — the app receives the DERIVED names: the daemon spells them from name and
// port, and nothing a person typed is a variable name.
func TestServiceEnvNamesAreDerivedNotTyped(t *testing.T) {
	_, rt, _, ex, dir := serviceHarness(t, nil)
	runOnce(t, ex, dir)
	app := rt.log.argvOf("create")
	if len(app) == 0 {
		t.Fatal("no app container was created")
	}
	joined := strings.Join(app[0], " ")
	for _, want := range []string{"--env", "AEON_SERVICE_DB_HOST", "--env", "AEON_SERVICE_DB_PORT",
		"--env", "AEON_SERVICE_QUEUE_HOST", "--env", "AEON_SERVICE_QUEUE_PORT"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the app container's argv lost %q:\n%s", want, joined)
		}
	}
	// Names in argv, values in the engine client's own environment (EV11).
	var env []string
	for _, op := range rt.log.all() {
		if op.What == "create" {
			env = op.Env
		}
	}
	if !hasEnv(env, "AEON_SERVICE_DB_HOST=db") || !hasEnv(env, "AEON_SERVICE_DB_PORT=5432") {
		t.Errorf("the derived values are not in the client environment (%v) (#3)", env)
	}
}

func hasEnv(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

// #3 again, the half that matters most: a database password stays in the
// database's container unless the environment's own env.secrets asks for it.
func TestServiceSecretsStayInTheServiceUnlessListed(t *testing.T) {
	_, rt, _, ex, dir := serviceHarness(t, nil)
	runOnce(t, ex, dir)
	var svcArgv, appArgv []string
	var svcEnv, appEnv []string
	for _, op := range rt.log.all() {
		switch op.What {
		case "create-service":
			if op.Ref == "db" { // the queue's argv would overwrite the one under test
				svcArgv, svcEnv = op.Argv, op.Env
			}
		case "create":
			appArgv, appEnv = op.Argv, op.Env
		}
	}
	if !hasEnv(svcEnv, svcSecretName+"="+svcSecretVal) {
		t.Errorf("the service never received its own secret (%v)", svcEnv)
	}
	if strings.Contains(strings.Join(svcArgv, " "), svcSecretVal) {
		t.Errorf("the secret's value is in the service's argv: %v", svcArgv)
	}
	if strings.Contains(strings.Join(appArgv, " "), svcSecretName) {
		t.Errorf("the app container was handed the service's secret name: %v (#3)", appArgv)
	}
	for _, e := range appEnv {
		if strings.HasPrefix(e, svcSecretName+"=") {
			t.Errorf("the service's secret reached the model's environment: %s (#3)", e)
		}
	}
}

// #4 — no service is reachable from outside the loop's network: the argv log is
// the proof, over every create the driver made.
func TestNoServicePublishesAPort(t *testing.T) {
	_, rt, _, ex, dir := serviceHarness(t, nil)
	runOnce(t, ex, dir)
	for _, op := range rt.log.all() {
		if op.What != "create-service" && op.What != "create" {
			continue
		}
		for i, a := range op.Argv {
			if a == "-p" || a == "--publish" || a == "-P" || a == "--privileged" {
				t.Errorf("the %s argv carries %q at %d: %v (#4)", op.What, a, i, op.Argv)
			}
			if (a == "-v" || a == "--volume") && op.What == "create-service" {
				t.Errorf("the %s argv carries %q at %d: %v (#4)", op.What, a, i, op.Argv)
			}
			if strings.HasPrefix(a, "--publish") || strings.HasPrefix(a, "-p=") {
				t.Errorf("the %s argv publishes a port: %v (#4)", op.Argv, op)
			}
		}
		if op.What == "create-service" && !hasFlag(op.Argv, "--network-alias", "db") && !hasFlag(op.Argv, "--network-alias", "queue") {
			t.Errorf("the service joined the network without an alias; the derived HOST name would not resolve: %v", op.Argv)
		}
	}
}

func hasFlag(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}

// #5 — two loops on one environment own two networks and two sets of service
// containers; sharing them is cross-loop access with a label on it.
func TestLoopsDoNotShareServiceNetworks(t *testing.T) {
	p, rt, _, ex, dir := serviceHarness(t, nil)
	runOnce(t, ex, dir)
	second := &Executor{P: p, Loop: LoopRef{ComponentRoot: dir, LoopSlug: "loop-b", Owner: "step-1"},
		Env: ex.Env, Child: ex.Child, WorkDir: dir}
	cmd := "second loop"
	if _, err := second.Run(context.Background(), tools.ExecRequest{Command: cmd, Dir: dir}, allowToken(t, cmd)); err != nil {
		t.Fatalf("the second loop's command failed: %v", err)
	}
	names := map[string]bool{}
	for _, h := range rt.netList {
		names[h.Name] = true
	}
	if !names[NetworkName("loop-a")] || !names[NetworkName("loop-b")] {
		t.Errorf("the loops share a network (%v); each loop owns its own (#5)", names)
	}
	svcs := map[string]bool{}
	for _, op := range rt.log.all() {
		if op.What == "create-service" {
			svcs[strings.Join(op.Argv, " ")] = true
		}
	}
	if len(svcs) < 4 {
		t.Errorf("%d service containers for two loops × two services; a service reused across loops is cross-loop access (#5)", len(svcs))
	}
}

// The name rules: a name that could not survive upper-casing, and two names
// that would write the same derived variable.
func TestServiceNameRules(t *testing.T) {
	for _, bad := range []string{"", "DB", "db_1", "-db", "a..b", strings.Repeat("a", 33)} {
		if ref := derivedNameRefusal([]execenv.Service{{Name: bad, Image: svcImage, Port: 5432}}); ref == nil {
			t.Errorf("the service name %q was accepted; it cannot become a variable name and a network alias", bad)
		}
	}
	if ref := derivedNameRefusal([]execenv.Service{
		{Name: "db", Image: svcImage, Port: 5432},
		{Name: "db", Image: "redis:7", Port: 6379},
	}); ref == nil {
		t.Error("two services named db were accepted; which one the loop reaches would be undefined")
	}
	if ref := derivedNameRefusal([]execenv.Service{
		{Name: "db", Image: svcImage, Port: 5432},
		{Name: "queue", Image: "redis:7", Port: 6379},
	}); ref != nil {
		t.Errorf("two ordinary services were refused: %s", ref.Sentence())
	}
}

// Preview is specified, not built: the member exists so the shape is declared,
// and any non-null value is refused (EV14). The follow-up's brief lives in the
// task's Notes, not in code.
func TestPreviewIsRefused(t *testing.T) {
	spec := specFor("none", execenv.PullNever)
	spec.Preview = &execenv.Preview{Build: "npm run build", URL: "https://example", TTL: 300}
	refs := execenv.Validate(spec, execenv.BackendContainer, execenv.ValidateOpts{Drivers: []string{execenv.BackendContainer}})
	if len(refs) == 0 {
		t.Fatal("a preview deployment was accepted; nothing builds, hosts or expires one (#preview)")
	}
	found := false
	for _, r := range refs {
		if strings.Contains(r.Sentence(), "preview") {
			found = true
		}
	}
	if !found {
		t.Errorf("no refusal names preview: %v", refs)
	}
}

// The crash matrix: a daemon that dies after each step leaves rows the start
// sweep settles, and every labelled resource comes down.
func TestServiceReconcileCrashMatrix(t *testing.T) {
	for _, crash := range []string{"network-ready", "service-ready", "app-ready"} {
		t.Run(crash, func(t *testing.T) {
			_, rt, lg, ex, dir := serviceHarness(t, nil)
			switch crash {
			case "network-ready":
				lg.failOnCall["ready"] = 1 // the network was made, the daemon died marking it ready
			case "service-ready":
				lg.failOnCall["ready"] = 2 // the network is ready, the first service died half-built
			case "app-ready":
				lg.failOnCall["ready"] = 4 // network + two services ready, the app died at the last mark
			}
			cmd := "make it so"
			if _, err := ex.Run(context.Background(), tools.ExecRequest{Command: cmd, Dir: dir}, allowToken(t, cmd)); err == nil {
				t.Fatalf("the crash at %s did not happen", crash)
			}
			rep, err := Reconcile(context.Background(), lg, rt, func(string) {})
			if err != nil {
				t.Fatalf("reconcile failed: %v", err)
			}
			if rep.Inert {
				t.Fatal("reconcile was inert with an engine that answers")
			}
			if crash == "app-ready" {
				// The last mark was the app's own: everything below it exists and
				// is consistent, so the sweep FINISHES it (adopted, ready) rather
				// than destroying a working provision. That is the other half of
				// the promise, and it is asserted here so nobody "fixes" it into
				// a sweep that deletes a loop's whole environment on a hiccup.
				// The app's ready mark never landed, so its row is `provisioning`
				// with no handle while the container is really there: the sweep
				// settles the row and removes the container as an orphan. It must
				// NOT take the healthy services and the network down — that is the
				// other failure this branch exists to catch.
				if rep.Removed == 0 {
					t.Errorf("the half-built app's container was left running: %+v", rep)
				}
				if rep.Adopted < 2 {
					t.Errorf("the sweep took down what was healthy and ready: %+v", rep)
				}
				if n := len(rt.netList); n != 1 {
					t.Errorf("%d networks for one loop whose services were adopted", n)
				}
				return
			}
			if n := len(rt.netList); n != 0 {
				t.Errorf("%d labelled networks survived the sweep after a crash at %s", n, crash)
			}
			for _, r := range mustRows(t, lg) {
				if r.State == store.ProvisionStateReady && r.Owner != store.ProvisionOwnerLoop {
					t.Errorf("the row %s (%s) still reads ready after the sweep at %s", r.ID, r.Owner, crash)
				}
			}
		})
	}
}

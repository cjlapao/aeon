package daemon

// The resolver's own tests (S59 T5).
//
// These are the four properties the slice is named for, at the one place that
// joins the store to the execution plane: an unattended loop never inherits
// (EV6), a missing backend never falls back to the host (EV11), the step record
// carries names and never a value (EV9), and a storeless daemon runs nothing.
//
// Fixtures go through the store's exported doors, and the binding is at
// WORKSPACE scope: a component would exercise the same resolver arm with a
// longer fixture, and the arm that matters here is "which environment did the
// step get, and was it the one that was bound".

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

const (
	wireRoot       = "/repo/s59-wire"
	wireSecretName = "wire-token"
	wireSecretVal  = "s59-t5-wire-injected-4Kd9Xp"
	wireSpec       = `{"env":{"passthrough":["PATH","HOME"],"set":{"TERM":"dumb"},"secrets":[]},` +
		`"home":"real","limits":{},"egress":{"mode":"unenforced"},"image":null,"services":[],"preview":null}`
	wireSpecWithSecret = `{"env":{"passthrough":["PATH","HOME"],"set":{},"secrets":["` + wireSecretName + `"]},` +
		`"home":"real","limits":{},"egress":{"mode":"unenforced"},"image":null,"services":[],"preview":null}`
)

// wireFixture lays down the rows a step needs: an environment (or two), a
// workspace binding, a loop with an attended state, and a step whose context
// carries its id.
type wireFixture struct {
	store  *store.Store
	stepID int64
	root   string
}

func wireStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("throwaway store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func wireActor() store.Actor { return store.Actor{Kind: "user", ID: "carlos"} }

// bind puts an environment at workspace scope and seeds the system scrubbed row
// unless asked not to (the cell where the safe row has been removed).
func (f *wireFixture) bind(t *testing.T, slug, backend string, legacy bool, spec string) {
	t.Helper()
	actor := wireActor()
	if _, _, err := f.store.CreateEnvironment(actor, store.NewEnvironment{
		Slug: slug, Name: slug, Backend: backend, Spec: spec,
	}); err != nil {
		t.Fatalf("create environment %s: %v", slug, err)
	}
	if _, r, err := f.store.BindEnvironment(actor, store.NewBinding{
		ScopeKind: "workspace", EnvironmentID: slug,
	}); r != nil || err != nil {
		t.Fatalf("bind %s: r=%v err=%v", slug, r, err)
	}
}

func (f *wireFixture) seedScrubbed(t *testing.T) {
	t.Helper()
	if _, _, err := f.store.CreateEnvironment(wireActor(), store.NewEnvironment{
		Slug: systemScrubbedSlug, Name: "Host (scrubbed)", Backend: "host-scrubbed", Spec: wireSpec,
	}); err != nil {
		t.Fatalf("seed the system scrubbed row: %v", err)
	}
}

func (f *wireFixture) loop(t *testing.T, slug string, attended *bool) {
	t.Helper()
	if _, err := f.store.CreateLoop(store.NewLoop{
		Slug: slug, Title: slug, Goal: "the wire fixture", Root: f.root, ComponentRoot: f.root,
		Mode: "autonomous", Phase: "build", Status: "active", Attended: attended,
	}); err != nil {
		t.Fatalf("create loop: %v", err)
	}
}

func (f *wireFixture) step(t *testing.T, loopSlug string) int64 {
	t.Helper()
	st, err := f.store.CreateStep(store.Scope{LoopSlug: loopSlug, ComponentRoot: f.root},
		store.NewStep{Phase: "build", Agent: "builder", Attempt: 1, Source: "test"})
	if err != nil {
		t.Fatalf("create step: %v", err)
	}
	return st.ID
}

func wireResolver(t *testing.T, s *store.Store, getenv func(string) string) *StepExecResolver {
	return &StepExecResolver{
		Store:       s,
		Getenv:      getenv,
		Names:       func() []string { return []string{"PATH", "HOME", "TERM"} },
		DaemonUID:   execenv.CurrentUID(),
		Log:         func(format string, args ...any) { t.Logf(format, args...) },
		Credentials: execenv.DaemonCredentials{Provider: []string{"ANTHROPIC_API_KEY"}, Forge: []string{"GH_TOKEN"}},
	}
}

func testGetenv(name string) string {
	switch name {
	case "PATH":
		return "/usr/bin:/bin"
	case "HOME":
		return "/var/lib/aeon"
	case "TERM":
		return "dumb"
	}
	return ""
}

func newWireFixture(t *testing.T) *wireFixture {
	t.Helper()
	return &wireFixture{store: wireStore(t), root: wireRoot}
}

// TestUnattendedLoopNeverInherits is behaviour #2, every cell.
func TestUnattendedLoopNeverInherits(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name          string
		attended      *bool
		wantBackend   string
		wantDowngrade bool
		wantRefusal   string
	}{
		{"never declared, so nobody said anyone was watching", nil, "host-scrubbed", true, ""},
		{"declared unattended", &no, "host-scrubbed", true, ""},
		{"declared attended, the upgraded store's continuity", &yes, "host-inherit", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newWireFixture(t)
			f.seedScrubbed(t)
			f.bind(t, "host-inherit", "host-inherit", true,
				`{"env":{"passthrough":[],"set":{},"secrets":[]},"home":"real","limits":{},`+
					`"egress":{"mode":"unenforced"},"image":null,"services":[],"preview":null}`)
			f.loop(t, "l1", tc.attended)
			stepID := f.step(t, "l1")

			ex, err := wireResolver(t, f.store, testGetenv).For(stepctx.WithStepID(context.Background(), stepID), f.root)
			if tc.wantRefusal != "" {
				if err == nil {
					t.Fatal("the cell that must refuse returned an executor")
				}
				return
			}
			if err != nil {
				t.Fatalf("the step was refused: %v", err)
			}
			host, ok := ex.(tools.HostExecutor)
			if !ok {
				t.Fatalf("the executor is %T, not the host executor", ex)
			}
			if tc.wantBackend == "host-inherit" {
				if !host.Env.Legacy() {
					t.Error("an attended loop on a legacy binding did NOT inherit — the continuity EV6 promises " +
						"an upgraded store is gone")
				}
				return
			}
			if host.Env.Legacy() {
				t.Fatal("an unattended loop inherited the daemon's environment")
			}
			names := host.Env.Names()
			if len(names) == 0 {
				t.Fatal("the scrubbed child has no variables at all")
			}
			for _, v := range names {
				if v.Name == "*" {
					t.Error("the scrubbed child is the legacy inherit value")
				}
			}
			rows, err := f.store.StepEnvironments(stepID)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("the step record is %d rows, want 1: %+v", len(rows), rows)
			}
			if rows[0].Downgraded != tc.wantDowngrade {
				t.Errorf("downgraded = %v, want %v — a tightening nobody recorded is a silent downgrade",
					rows[0].Downgraded, tc.wantDowngrade)
			}
			if tc.wantDowngrade && (rows[0].DowngradeReason == nil || *rows[0].DowngradeReason == "") {
				t.Error("the downgrade has no reason, so the record says only that something changed")
			}
		})
	}

	t.Run("the safe row removed is a refusal, never an inherit", func(t *testing.T) {
		f := newWireFixture(t)
		f.bind(t, "host-inherit", "host-inherit", true,
			`{"env":{"passthrough":[],"set":{},"secrets":[]},"home":"real","limits":{},`+
				`"egress":{"mode":"unenforced"},"image":null,"services":[],"preview":null}`)
		f.loop(t, "l1", &no)
		stepID := f.step(t, "l1")

		_, err := wireResolver(t, f.store, testGetenv).For(stepctx.WithStepID(context.Background(), stepID), f.root)
		if err == nil {
			t.Fatal("with no safe environment to downgrade TO, the legacy environment ran")
		}
		ref, ok := err.(*execenv.Refusal)
		if !ok || ref.Kind != execenv.RefusalEnvironmentUnavailable {
			t.Fatalf("the answer was %v, want an %s refusal", err, execenv.RefusalEnvironmentUnavailable)
		}
		rows, _ := f.store.StepEnvironments(stepID)
		if len(rows) != 1 || rows[0].RefusalKind == nil {
			t.Errorf("the refusal was not recorded; the step would read as never having needed an environment: %+v",
				rows)
		}
	})

	t.Run("the attended read failing is unattended, never a guess the other way", func(t *testing.T) {
		// The cell the store cannot produce on command: the loop row is there
		// and says "attended", and READING it fails. The seam exists for this
		// one arm — the alternative is a test that passes because a database was
		// inconvenient, or no test at all, and this is the arm that decides
		// whether a legacy environment inherits the daemon's provider keys.
		f := newWireFixture(t)
		f.seedScrubbed(t)
		f.bind(t, "host-inherit", "host-inherit", true,
			`{"env":{"passthrough":[],"set":{},"secrets":[]},"home":"real","limits":{},`+
				`"egress":{"mode":"unenforced"},"image":null,"services":[],"preview":null}`)
		f.loop(t, "l1", &yes)
		stepID := f.step(t, "l1")

		resolver := wireResolver(t, f.store, testGetenv)
		resolver.Attended = func(context.Context, int64) (*bool, error) {
			attended := true
			return &attended, errors.New("the loop row is locked")
		}
		ex, err := resolver.For(stepctx.WithStepID(context.Background(), stepID), f.root)
		if err != nil {
			t.Fatalf("the step was refused: %v", err)
		}
		if host := ex.(tools.HostExecutor); host.Env.Legacy() {
			t.Error("a step whose attended state could not be read INHERITED the daemon's environment — an " +
				"unreadable row is not an assurance that a human is watching")
		}
	})
}

// TestUnavailableBackendNeverFallsBackToHost is behaviour #3 and EV11: a bound
// backend the daemon does not have is a refusal with no executor, and the step
// record says so. There is no path from here to a host spawn.
func TestUnavailableBackendNeverFallsBackToHost(t *testing.T) {
	f := newWireFixture(t)
	f.seedScrubbed(t)
	f.bind(t, "in-container", "container", false,
		`{"env":{"passthrough":["PATH"],"set":{},"secrets":[]},"home":"real","limits":{},`+
			`"egress":{"mode":"unenforced"},"image":{"ref":"alpine@sha256:00000000000000000000000000000000`+
			`00000000000000000000000000000000","pull":"never"},"services":[],"preview":null}`)
	f.loop(t, "l1", &([]bool{false}[0]))
	stepID := f.step(t, "l1")

	resolver := wireResolver(t, f.store, testGetenv)
	// No entry in Backends: this is the state of a daemon built without the
	// container backend, which is most daemons today.
	ex, err := resolver.For(stepctx.WithStepID(context.Background(), stepID), f.root)
	if err == nil {
		t.Fatalf("a container-bound step got a host executor: %T", ex)
	}
	ref, ok := err.(*execenv.Refusal)
	if !ok || ref.Kind != execenv.RefusalEnvironmentUnavailable {
		t.Fatalf("the answer was %v, want an %s refusal", err, execenv.RefusalEnvironmentUnavailable)
	}
	if !strings.Contains(ref.Sentence(), "container") {
		t.Errorf("the refusal does not name the backend that is missing, so it reads as a general failure: %s",
			ref.Sentence())
	}
	rows, _ := f.store.StepEnvironments(stepID)
	if len(rows) != 1 || rows[0].RefusalKind == nil {
		t.Errorf("the refused spawn left no record: %+v", rows)
	}

	// And with a backend registered, the SAME step gets that backend's executor
	// — the refusal above is about the missing driver, not about the record.
	resolver.Backends = map[string]BackendExecutor{"container": stubBackend{}}
	if _, err := resolver.For(stepctx.WithStepID(context.Background(), stepID), f.root); err != nil {
		t.Fatalf("with the backend registered the step was still refused: %v", err)
	}
}

type stubBackend struct{ got int }

// The seam carries the provision identity (S59 T12, F-1): it once carried none, so a
// container-bound step could never be created and no test here noticed.
func (s stubBackend) Executor(_ context.Context, env execenv.BoundEnv, _ execenv.ChildEnv, _ execenv.ProvisionIdent) (tools.CommandExecutor, *execenv.Refusal) {
	if env.Backend != "container" {
		return nil, execenv.NewRefusal(execenv.RefusalEnvironmentUnavailable,
			"the stub backend was asked to run "+env.Backend, "stub", "stub")
	}
	return stubExecutor{}, nil
}

type stubExecutor struct{}

func (stubExecutor) Run(context.Context, tools.ExecRequest, policy.AllowToken) (tools.RunResult, error) {
	return tools.RunResult{ExitCode: 0}, nil
}
func (stubExecutor) Names() []execenv.Var {
	return []execenv.Var{{Name: "PATH", Source: "passthrough"}}
}
func (stubExecutor) Redact(text string) string       { return text }
func (stubExecutor) Redactions() []execenv.NameCount { return nil }

// TestStepEnvironmentRecordedNamesOnly is EV9's record and the scanner's job
// from the resolver's side: the row that describes a step with an injected
// secret must not contain the secret.
func TestStepEnvironmentRecordedNamesOnly(t *testing.T) {
	f := newWireFixture(t)
	f.seedScrubbed(t)
	actor := wireActor()
	if _, _, err := f.store.CreateSecretRef(actor, store.NewSecretRef{
		Name: wireSecretName, Description: "the wire token", SourceKind: "daemon-env",
		Source: "WIRE_DSN", InjectAs: "WIRE_DSN",
	}, []string{"ANTHROPIC_API_KEY"}); err != nil {
		t.Fatalf("create the reference: %v", err)
	}
	if _, _, err := f.store.CreateEnvironment(actor, store.NewEnvironment{
		Slug: "with-secret", Name: "With secret", Backend: "host-scrubbed", Spec: wireSpecWithSecret,
	}); err != nil {
		t.Fatalf("create the environment: %v", err)
	}
	if _, r, err := f.store.BindEnvironment(actor, store.NewBinding{
		ScopeKind: "workspace", EnvironmentID: "with-secret",
	}); r != nil || err != nil {
		t.Fatalf("bind: r=%v err=%v", r, err)
	}
	f.loop(t, "l1", &([]bool{false}[0]))
	stepID := f.step(t, "l1")

	getenv := func(name string) string {
		if name == "WIRE_DSN" {
			return wireSecretVal
		}
		return testGetenv(name)
	}
	ex, err := wireResolver(t, f.store, getenv).For(stepctx.WithStepID(context.Background(), stepID), f.root)
	if err != nil {
		t.Fatalf("the step was refused: %v", err)
	}
	host := ex.(tools.HostExecutor)
	if host.Redactor == nil {
		t.Fatal("an environment that injected a secret got no filter")
	}

	rows, err := f.store.StepEnvironments(stepID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("the step record is %d rows: %+v", len(rows), rows)
	}
	row := rows[0]
	if len(row.SecretRefs) != 1 || row.SecretRefs[0] != wireSecretName {
		t.Errorf("the record does not name the reference it injected: %+v", row.SecretRefs)
	}
	found := false
	for _, v := range row.VarNames {
		if strings.HasPrefix(v, "WIRE_DSN=") {
			found = true
		}
	}
	if !found {
		t.Errorf("the injected name is missing from the record: %+v", row.VarNames)
	}
	// The value, in any form the scanner knows, must be nowhere in the row —
	// including in the JSON of the two name columns.
	if blob, err := json.Marshal(row); err != nil {
		t.Fatal(err)
	} else if strings.Contains(string(blob), wireSecretVal) {
		t.Errorf("the step record carries the secret it injected: %s", redactForFailure(string(blob)))
	}
}

func redactForFailure(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// TestStorelessDaemonSpawnsNothing is the fail-closed shape: no store means no
// resolver, and no resolver means bash never spawns.
func TestStorelessDaemonSpawnsNothing(t *testing.T) {
	if r := newStepExecResolver(nil); r != nil {
		t.Fatal("a storeless daemon was handed a resolver; it would have no bindings to resolve")
	}
	r := &StepExecResolver{}
	if _, err := r.For(context.Background(), wireRoot); err == nil {
		t.Fatal("the zero resolver answered with an executor")
	} else if ref, ok := err.(*execenv.Refusal); !ok || ref.Do == "" {
		t.Errorf("the zero resolver's answer is not an actionable refusal: %v", err)
	}

	runner := StepRunner{} // the storeless literal's shape
	if runner.ExecResolver != nil {
		t.Fatal("a storeless StepRunner carries a resolver")
	}
}

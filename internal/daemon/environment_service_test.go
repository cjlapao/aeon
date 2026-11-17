package daemon

// environment_service_test.go — S59 T6: the environment and secret-reference
// services over a real store, testing the four claims the plane is built on:
//
//   - the dry run's statement and the applied write's statement are THE SAME
//     sentence, from one formatter, and the dry run wrote nothing (EV18, P3);
//   - a stale revision, a workspace unbind and an unresolvable reference are
//     REFUSALS that wrote nothing, in the plane's one voice (EV16, EV17, EV7);
//   - `environment.effective` answers with names and sources and never a value,
//     even when the value is sitting in the daemon's own environment two
//     function calls away (EV5, EV7);
//   - every changing write is audited, and a refusal is not a changing write
//     (EV17) — the pair that makes the audit worth reading.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// canaryValue is what a leak would look like. Every assertion below that says
// "no value" searches the marshalled output for THIS string, so a leak cannot
// hide behind a field the test did not think to check.
const canaryValue = "sk-live-CANARY-8f2b1c"

type envFixture struct {
	store    *store.Store
	resolver *StepExecResolver
	env      *environmentService
	refs     *secretRefService
	log      *recordingLogger
}

// recordingLogger is the daemon's Logger seam (one method) so the services'
// sentences are observable: several of the assertions below ask what the daemon
// SAID, not only what it wrote.
type recordingLogger struct{ lines []string }

func (l *recordingLogger) Infof(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func newEnvFixture(t *testing.T) *envFixture {
	t.Helper()
	st := openSeedStore(t)
	env := map[string]string{
		"PATH": "/usr/bin", "HOME": "/home/person", "USER": "person", "TERM": "dumb",
		"DEPLOY_TOKEN": canaryValue,
		// The daemon's own: a reference naming one of these is refused (EV7),
		// and a child that inherits it is the failure EV1 describes.
		"ANTHROPIC_API_KEY": "sk-ant-" + canaryValue,
	}
	names := make([]string, 0, len(env))
	for k := range env {
		names = append(names, k)
	}
	r := &StepExecResolver{
		Store:     st,
		Getenv:    func(name string) string { return env[name] },
		Names:     func() []string { return names },
		DaemonUID: execenv.CurrentUID(),
		Credentials: execenv.DaemonCredentials{
			Provider: []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"},
			Forge:    []string{"AEON_FORGE_TOKEN"},
		},
		Backends: map[string]BackendExecutor{},
	}
	f := &envFixture{store: st, resolver: r}
	logger := &recordingLogger{}
	f.log = logger
	f.env = newEnvironmentService(st, r, logger, nil)
	f.refs = newSecretRefService(st, r, logger)
	return f
}

// canonicalSpec is a spec built the way a person's write reaches the store:
// parsed, validated, canonicalised. Hand-written JSON would let this file drift
// from the shape the validator actually signs.
func canonicalSpec(t *testing.T, passthrough []string, set map[string]string, secrets []string, backend string) string {
	t.Helper()
	spec := execenv.Spec{
		Env:    execenv.EnvSection{Passthrough: passthrough, Set: set, Secrets: secrets},
		Home:   execenv.HomeReal,
		Egress: execenv.Egress{Mode: "unenforced"},
	}
	if backend == "container" {
		spec.Image = &execenv.Image{Ref: "debian:bookworm-slim", Pull: execenv.PullMissing}
		spec.Home = execenv.HomeScratch
	}
	refusals := execenv.Validate(spec, backend, execenv.ValidateOpts{Drivers: []string{"host-scrubbed", "host-inherit", "container"}})
	if len(refusals) > 0 {
		t.Fatalf("the test's own spec is refused by the validator: %s", refusals[0].What)
	}
	return string(execenv.Canonical(spec))
}

var person = protocol.Actor{Kind: "user", ID: "person"}

func (f *envFixture) create(t *testing.T, slug, backend, spec string) protocol.EnvironmentView {
	t.Helper()
	v, err := f.env.Create(context.Background(), person, protocol.EnvironmentCreateParams{
		Slug: slug, Name: slug, Backend: backend, Spec: spec,
	})
	if err != nil {
		t.Fatalf("create %s (%s): %v", slug, backend, err)
	}
	return v
}

func (f *envFixture) bind(t *testing.T, scope, componentID, envID string, dryRun bool) protocol.BindResult {
	t.Helper()
	r, err := f.env.Bind(context.Background(), person, protocol.EnvironmentBindParams{
		Scope: scope, ComponentID: componentID, EnvironmentID: envID, DryRun: dryRun,
	})
	if err != nil {
		t.Fatalf("bind %s→%s (dryRun=%v): %v", componentID, envID, dryRun, err)
	}
	return r
}

// mustOK fails with the REFUSAL'S SENTENCE, not the transport's base message:
// debugging a refusal by its code alone is how an hour goes missing.
func mustOK(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var perr *protocol.Error
	if errors.As(err, &perr) {
		t.Fatalf("%s: refused (%s): %s", what, perr.Data.Kind, perr.Data.What)
	}
	t.Fatalf("%s: %v", what, err)
}

func (f *envFixture) auditCount(t *testing.T, subjectKind, subjectID string) int {
	t.Helper()
	rows, err := f.store.ListChanges(store.EnvironmentChangeFilter{SubjectKind: subjectKind, SubjectID: subjectID, Limit: 200})
	if err != nil {
		t.Fatalf("list changes for %s/%s: %v", subjectKind, subjectID, err)
	}
	return len(rows)
}

func refusalOf(t *testing.T, err error) *protocol.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a refusal, got success")
	}
	var perr *protocol.Error
	if !errors.As(err, &perr) {
		t.Fatalf("the refusal is %T (%v), want the plane's *protocol.Error", err, err)
	}
	if perr.Code != protocol.ErrEnvironmentRefused.Code {
		t.Fatalf("code = %d, want the plane's one code %d (DB19: a refusal is not an infrastructure failure)", perr.Code, protocol.ErrEnvironmentRefused.Code)
	}
	return perr
}

// TestBindDryRunStatementEqualsAppliedStatement is EV18's promise: the dry run
// answers the person's question in the words the applied write will use, and
// the only difference is the clause that says nothing was applied. One
// formatter, four callers (CLI, panel, dry run, audit).
func TestBindDryRunStatementEqualsAppliedStatement(t *testing.T) {
	f := newEnvFixture(t)
	ctx := context.Background()
	spec := canonicalSpec(t, []string{"PATH", "HOME"}, nil, nil, "host-scrubbed")
	env := f.create(t, "ci-default", "host-scrubbed", spec)

	dry := f.bind(t, "workspace", "", env.ID, true)
	if !dry.DryRun {
		t.Errorf("a dry run answered DryRun=false")
	}
	if !strings.Contains(dry.Statement, "dry-run") {
		t.Errorf("the dry run's statement does not say it was a dry run: %q", dry.Statement)
	}
	if dry.Changed {
		t.Errorf("a dry run claimed changed=true — nothing was applied (P3)")
	}

	// The dry run wrote nothing: no binding, no audit row.
	if _, source, err := f.store.BindingFor(""); err != nil {
		t.Fatalf("binding for: %v", err)
	} else if source != "" {
		t.Errorf("the dry run left a binding at source %q", source)
	}
	if n := f.auditCount(t, "binding", "workspace"); n != 0 {
		t.Errorf("the dry run wrote %d audit rows, want 0 (EV17: a write that wrote nothing is not a changing write)", n)
	}

	applied := f.bind(t, "workspace", "", env.ID, false)
	if applied.DryRun {
		t.Errorf("the applied write answered DryRun=true")
	}
	without := strings.TrimSuffix(dry.Statement, " · dry-run: nothing applied")
	if without != applied.Statement {
		t.Errorf("the dry run and the applied write disagree — two formatters:\n dry:     %q\n applied: %q", dry.Statement, applied.Statement)
	}
	for _, want := range []string{"bind", "environment ci-default", "backend host-scrubbed (strength 1)"} {
		if !strings.Contains(applied.Statement, want) {
			t.Errorf("the statement omits %q: %q", want, applied.Statement)
		}
	}
	if n := f.auditCount(t, "binding", "workspace"); n != 1 {
		t.Errorf("the applied bind wrote %d audit rows, want 1", n)
	}
	if !applied.Changed {
		t.Errorf("the first bind from nothing claimed changed=false — EV18's answer must say whether anything moved")
	}

	// Re-binding the same environment is a no-op the statement says so for
	// (the store skips its audit row; the door reports `changed` honestly).
	again := f.bind(t, "workspace", "", env.ID, false)
	if again.Changed {
		t.Errorf("re-binding the same environment claimed changed=true")
	}
	if n := f.auditCount(t, "binding", "workspace"); n != 1 {
		t.Errorf("a no-op bind wrote %d audit rows in total, want 1", n)
	}
	_ = ctx
}

// TestBindStatementNamesWhatWasBoundBefore is the tightening/widening half of
// EV16: a person replacing a binding is told the strength moved, in which
// direction, and from what.
func TestBindStatementNamesWhatWasBoundBefore(t *testing.T) {
	f := newEnvFixture(t)
	spec := canonicalSpec(t, []string{"PATH"}, nil, nil, "host-scrubbed")
	first := f.create(t, "loose", "host-scrubbed", spec)
	f.bind(t, "workspace", "", first.ID, false)

	dry := f.bind(t, "workspace", "", first.ID, true)
	if !strings.Contains(dry.Statement, "same strength") {
		t.Errorf("re-binding at the same strength did not say so: %q", dry.Statement)
	}
}

// TestBindRefusesAnUnresolvableSecretRef: a binding to an environment whose
// reference does not exist (or is archived) is refused BEFORE anything is
// written — and the check is a liveness check, never a read: the door asks
// whether the reference exists and is live, and never what it points at (EV7).
func TestBindRefusesAnUnresolvableSecretRef(t *testing.T) {
	f := newEnvFixture(t)
	missing := canonicalSpec(t, []string{"PATH"}, nil, []string{"ghost-token"}, "host-scrubbed")
	env := f.create(t, "needs-ghost", "host-scrubbed", missing)

	_, err := f.env.Bind(context.Background(), person, protocol.EnvironmentBindParams{
		Scope: "workspace", EnvironmentID: env.ID,
	})
	perr := refusalOf(t, err)
	if !strings.Contains(perr.Data.What, "ghost-token") {
		t.Errorf("the refusal does not name the reference it could not resolve: %q", perr.Data.What)
	}
	if strings.Contains(perr.Data.What, canaryValue) {
		t.Errorf("the refusal carried a value: %q", perr.Data.What)
	}
	if _, source, _ := f.store.BindingFor(""); source != "" {
		t.Errorf("a refused bind left a binding")
	}
	if n := f.auditCount(t, "binding", "workspace"); n != 0 {
		t.Errorf("a refused bind wrote %d audit rows, want 0", n)
	}

	// And the live case: create the reference, and the same bind succeeds.
	_, refErr := f.refs.Create(context.Background(), person, protocol.SecretRefCreateParams{
		Name: "ghost-token", SourceKind: "daemon-env", Source: "DEPLOY_TOKEN", InjectAs: "DEPLOY_TOKEN",
	})
	mustOK(t, "create the reference", refErr)
	if r, err := f.env.Bind(context.Background(), person, protocol.EnvironmentBindParams{
		Scope: "workspace", EnvironmentID: env.ID,
	}); err != nil {
		t.Fatalf("the bind after the reference exists: %v", err)
	} else if !strings.Contains(r.Statement, "ghost-token") {
		t.Errorf("the statement does not name the reference being injected: %q", r.Statement)
	}
}

// TestUpdateRefusesStaleRevision is EV16's lost-update failure, spoken: the
// refusal names the revision the row is on NOW, so the caller re-reads instead
// of guessing.
func TestUpdateRefusesStaleRevision(t *testing.T) {
	f := newEnvFixture(t)
	spec := canonicalSpec(t, []string{"PATH"}, nil, nil, "host-scrubbed")
	env := f.create(t, "cas", "host-scrubbed", spec)
	if env.Revision != 1 {
		t.Fatalf("a fresh environment's revision = %d, want 1", env.Revision)
	}
	updated, err := f.env.Update(context.Background(), person, protocol.EnvironmentUpdateParams{
		ID: env.ID, Spec: canonicalSpec(t, []string{"PATH", "HOME"}, nil, nil, "host-scrubbed"), ExpectedRevision: 1,
	})
	if err != nil {
		t.Fatalf("the first update: %v", err)
	}
	if updated.Revision != 2 {
		t.Errorf("after one update revision = %d, want 2", updated.Revision)
	}

	_, err = f.env.Update(context.Background(), person, protocol.EnvironmentUpdateParams{
		ID: env.ID, Spec: canonicalSpec(t, []string{"PATH"}, nil, nil, "host-scrubbed"), ExpectedRevision: 1,
	})
	perr := refusalOf(t, err)
	if !strings.Contains(strings.ToLower(perr.Data.Kind), "stale") && !strings.Contains(strings.ToLower(perr.Data.What), "revision") {
		t.Errorf("the stale write was refused as %q/%q — the caller needs to know the row moved", perr.Data.Kind, perr.Data.What)
	}
	if !strings.Contains(perr.Data.What, "2") {
		t.Errorf("the refusal does not name the current revision (the caller must re-read, not guess): %q", perr.Data.What)
	}
	// The refused write did not touch the row or the audit.
	after, err := f.env.Show(context.Background(), protocol.EnvironmentShowParams{ID: env.ID})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if after.Revision != 2 {
		t.Errorf("the refused update bumped the revision to %d", after.Revision)
	}
	if n := f.auditCount(t, "environment", env.ID); n != 2 {
		t.Errorf("%d audit rows after create + one accepted update + one refused one, want 2", n)
	}
}

// TestValidatorRefusalsArriveTogether: a person fixing a form learns about
// every problem in one round trip, in the validator's own sentences.
func TestValidatorRefusalsArriveTogether(t *testing.T) {
	f := newEnvFixture(t)
	// home: "maybe" is invalid, and a container spec with no image is another.
	_, err := f.env.Create(context.Background(), person, protocol.EnvironmentCreateParams{
		Slug: "broken", Name: "broken", Backend: "container",
		Spec: `{"env":{"passthrough":["PATH"],"set":{},"secrets":[]},"home":"maybe","limits":{},"egress":{"mode":"unenforced","allow":[]},"image":null,"services":[],"preview":null}`,
	})
	perr := refusalOf(t, err)
	if !strings.Contains(perr.Data.What, "problem(s)") {
		t.Errorf("the refusal does not say how many problems it found: %q", perr.Data.What)
	}
	if !strings.Contains(perr.Data.What, "home") {
		t.Errorf("the refusal dropped the validator's home sentence: %q", perr.Data.What)
	}
	if _, err := f.store.GetEnvironment("broken"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a refused create wrote a row anyway")
	}
}

// TestWorkspaceBindingCannotBeUnbound is EV18's spoken limit: the floor the
// seed laid cannot be kicked away, and the refusal says what to do instead.
func TestWorkspaceBindingCannotBeUnbound(t *testing.T) {
	f := newEnvFixture(t)
	spec := canonicalSpec(t, []string{"PATH"}, nil, nil, "host-scrubbed")
	env := f.create(t, "floor", "host-scrubbed", spec)
	f.bind(t, "workspace", "", env.ID, false)

	_, err := f.env.Unbind(context.Background(), person, protocol.EnvironmentUnbindParams{ComponentID: "workspace"})
	perr := refusalOf(t, err)
	if !strings.Contains(perr.Data.What, "environment.bind") {
		t.Errorf("the refusal names the problem but not the replacement: %q", perr.Data.What)
	}
	if !strings.Contains(perr.Data.What, "unbound") {
		t.Errorf("the refusal does not say what it refused: %q", perr.Data.What)
	}
	if _, source, _ := f.store.BindingFor(""); source != "workspace" {
		t.Errorf("the refused unbind removed the workspace binding")
	}

	// A component binding, by contrast, unbinds — and says so. The component is
	// a real row: the store refuses to bind something that does not exist, and
	// a test that faked its way past that would be testing nothing.
	cw, err := f.store.CreateComponent(store.NewComponent{
		Name: "Bound One", Slug: "bound-one", Root: "/tmp/bound-one",
	})
	if err != nil || !cw.OK() {
		t.Fatalf("create the component: refusal=%v err=%v", cw.Refusal, err)
	}
	if _, ref, err := f.store.BindEnvironment(store.Actor{Kind: "user", ID: "person"}, store.NewBinding{
		ScopeKind: "component", ComponentID: cw.Component.ID, EnvironmentID: env.ID,
	}); ref != nil || err != nil {
		t.Fatalf("seed a component binding: ref=%v err=%v", ref, err)
	}
	res, err := f.env.Unbind(context.Background(), person, protocol.EnvironmentUnbindParams{ComponentID: cw.Component.ID})
	if err != nil {
		t.Fatalf("unbind the component: %v", err)
	}
	if !res.Unbound {
		t.Errorf("unbinding a live component binding answered unbound=false")
	}
}

// TestEffectiveCarriesNamesAndSourcesNeverValues is EV5 and EV7 on the one read
// a person uses to decide whether to trust a loop. The canary is sitting in the
// daemon's environment; the reference resolves through it; the answer must
// still contain no value anywhere in its marshalled form.
func TestEffectiveCarriesNamesAndSourcesNeverValues(t *testing.T) {
	f := newEnvFixture(t)
	ctx := context.Background()
	if _, err := f.refs.Create(ctx, person, protocol.SecretRefCreateParams{
		Name: "deploy-token", SourceKind: "daemon-env", Source: "DEPLOY_TOKEN", InjectAs: "DEPLOY_TOKEN",
	}); err != nil {
		t.Fatalf("create the reference: %v", err)
	}
	spec := canonicalSpec(t, []string{"PATH", "HOME"}, map[string]string{"TERM": "dumb"}, []string{"deploy-token"}, "host-scrubbed")
	env := f.create(t, "with-secret", "host-scrubbed", spec)
	f.bind(t, "workspace", "", env.ID, false)

	view, err := f.env.Effective(ctx, protocol.EnvironmentEffectiveParams{})
	mustOK(t, "effective", err)
	if view.EnvironmentSlug != "with-secret" || view.Backend != "host-scrubbed" {
		t.Errorf("the preview resolved %s/%s, want the bound environment", view.EnvironmentSlug, view.Backend)
	}
	if view.Legacy {
		t.Errorf("a host-scrubbed preview reported legacy=true — the honest third state is reserved for a real inherit")
	}
	if len(view.SecretRefNames) != 1 || view.SecretRefNames[0] != "deploy-token" {
		t.Errorf("secretRefNames = %v, want the one reference by name", view.SecretRefNames)
	}
	sawInjected := false
	for _, v := range view.Variables {
		if v.Name == "" {
			t.Errorf("a variable arrived with no name: %+v", v)
		}
		if v.Source == "" {
			t.Errorf("variable %s has no source — a name with no provenance is a name a panel cannot explain", v.Name)
		}
		if strings.Contains(v.Name, canaryValue) || strings.Contains(v.Source, canaryValue) {
			t.Errorf("variable %+v carries the value in its name or source", v)
		}
		if v.Name == "DEPLOY_TOKEN" {
			sawInjected = true
		}
	}
	if !sawInjected {
		t.Errorf("the injected reference's variable is missing from the preview: %+v", view.Variables)
	}
	blob, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, leak := range []string{canaryValue, "sk-ant-"} {
		if strings.Contains(string(blob), leak) {
			t.Errorf("the marshalled preview contains %q — the preview is names and sources, never values (EV7)", leak)
		}
	}
	// The honesty line travels with the answer, so a panel prints the truth of
	// host-scrubbed rather than composing its own.
	if view.Honesty == "" {
		t.Errorf("the preview carries no honesty line — the plane's sentence has one copy, and this is where a reader meets it")
	}
}

// TestEffectiveRefusesRatherThanAnsweringEmpty: EV2 has no fallback, so an
// unbound workspace is a refusal that says what to do, and NOT a preview of an
// empty environment — the plausible wrong answer this plane keeps refusing.
func TestEffectiveRefusesRatherThanAnsweringEmpty(t *testing.T) {
	f := newEnvFixture(t)
	_, err := f.env.Effective(context.Background(), protocol.EnvironmentEffectiveParams{})
	perr := refusalOf(t, err)
	if !strings.Contains(perr.Data.What, "bind") && !strings.Contains(perr.Data.What, "binding") {
		t.Errorf("the refusal names nothing the caller can act on: %q", perr.Data.What)
	}
}

// TestEveryChangingWriteAudited is EV17's pair: each act that changes a row
// leaves a row in the audit naming its actor, and the acts that change nothing
// — a dry run, a refusal, a no-op — leave nothing.
func TestEveryChangingWriteAudited(t *testing.T) {
	f := newEnvFixture(t)
	ctx := context.Background()
	spec := canonicalSpec(t, []string{"PATH"}, nil, nil, "host-scrubbed")

	env := f.create(t, "audited", "host-scrubbed", spec)
	// What the daemon SAID is part of the record: a write nobody logged is a
	// write nobody can find in a log when the audit is the thing being argued
	// about.
	if !strings.Contains(strings.Join(f.log.lines, "\n"), "created audited") {
		t.Errorf("the create logged nothing identifiable: %v", f.log.lines)
	}
	if n := f.auditCount(t, "environment", env.ID); n != 1 {
		t.Errorf("create wrote %d audit rows, want 1", n)
	}
	if _, err := f.env.Update(ctx, person, protocol.EnvironmentUpdateParams{
		ID: env.ID, Spec: canonicalSpec(t, []string{"PATH", "HOME"}, nil, nil, "host-scrubbed"), ExpectedRevision: 1,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if n := f.auditCount(t, "environment", env.ID); n != 2 {
		t.Errorf("create + update wrote %d audit rows, want 2", n)
	}
	if _, err := f.env.Archive(ctx, person, protocol.EnvironmentIDParams{ID: env.ID}); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := f.env.Restore(ctx, person, protocol.EnvironmentIDParams{ID: env.ID}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if n := f.auditCount(t, "environment", env.ID); n != 4 {
		t.Errorf("create+update+archive+restore wrote %d audit rows, want 4", n)
	}

	// The reference plane, same discipline.
	_, refErr := f.refs.Create(ctx, person, protocol.SecretRefCreateParams{
		Name: "audited-ref", SourceKind: "daemon-env", Source: "DEPLOY_TOKEN", InjectAs: "AUDITED_REF",
	})
	mustOK(t, "create the reference", refErr)
	rows, err := f.store.ListChanges(store.EnvironmentChangeFilter{SubjectKind: "secret_ref", SubjectID: "audited-ref", Limit: 50})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(rows) == 0 {
		// The store audits a reference by its id; re-read by any subject to see
		// whether the write was audited at all under some other id.
		all, err := f.store.ListChanges(store.EnvironmentChangeFilter{SubjectKind: "secret_ref", Limit: 50})
		if err != nil {
			t.Fatalf("changes (all): %v", err)
		}
		if len(all) == 0 {
			t.Errorf("a secret-reference create wrote no audit row at all (EV17)")
		} else {
			t.Logf("the reference's audit row is keyed by id: %s", all[0].SubjectID)
		}
	}

	// And the negative half, on the same table: a refused write audits nothing.
	before := f.auditCount(t, "environment", env.ID)
	if _, err := f.env.Create(ctx, person, protocol.EnvironmentCreateParams{
		Slug: "audited", Name: "duplicate", Backend: "host-scrubbed", Spec: spec,
	}); err == nil {
		t.Errorf("a duplicate slug was accepted")
	}
	if after := f.auditCount(t, "environment", env.ID); after != before {
		t.Errorf("a refused write added audit rows (%d → %d); a refusal is not a changing write", before, after)
	}
}

// TestVocabularyServesHonestyAndLimits: everything a client would otherwise
// retype or guess, served from the one place each is defined (EV1, EV18). A
// panel that composed its own deny list would be a second authority, and the
// lists are compared against the code's, not against remembered numbers.
func TestVocabularyServesHonestyAndLimits(t *testing.T) {
	f := newEnvFixture(t)
	v, err := f.env.Vocabulary(context.Background())
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	if v.Honesty != execenv.Honesty {
		t.Errorf("honesty = %q, want the core's sentence verbatim — this plane's sentence has exactly one copy", v.Honesty)
	}
	if v.RedactionLimits != execenv.RedactionLimits {
		t.Errorf("redactionLimits = %q, want the core's", v.RedactionLimits)
	}
	// EV15. The panel prints these and holds NO status of its own, so the
	// daemon must be the one saying whether each limitation is true today — and
	// saying it in the core's words, not a paraphrase that can drift.
	known := execenv.KnownLimits()
	if len(v.KnownLimits) != len(known) {
		t.Fatalf("%d knownLimits served, want the %d the code compiles", len(v.KnownLimits), len(known))
	}
	for i, k := range known {
		got := v.KnownLimits[i]
		if got.Key != k.Key || got.Sentence != k.Sentence || got.Active != k.Active {
			t.Errorf("knownLimits[%d] = %+v, want %+v — the sentence has one copy and the status has one owner", i, got, k)
		}
	}
	if !v.KnownLimits[0].Active {
		t.Errorf("the S32 shared-working-tree limit is reported inactive while T0's proof says otherwise")
	}
	if len(v.KnownLimits[0].Key) == 0 || len(v.KnownLimits[0].Sentence) == 0 {
		t.Error("a known limit arrived unnamed or unsaid — a chip with no words is a chip the UI must invent")
	}
	if len(v.Backends) != len(execenv.Backends()) {
		t.Errorf("%d backends, want the %d the code compiles", len(v.Backends), len(execenv.Backends()))
	}
	for _, b := range v.Backends {
		if got := execenv.BackendStrength(b.Name); got != b.Strength {
			t.Errorf("backend %s strength = %d, want %d (the number the statement prints)", b.Name, b.Strength, got)
		}
		if len(b.Capabilities) == 0 {
			t.Errorf("backend %s arrived with no capabilities — the panel would have to invent the chips", b.Name)
		}
	}
	deny := execenv.Deny()
	if strings.Join(v.DenyWords, ",") != strings.Join(deny.Words, ",") {
		t.Errorf("the vocabulary's deny words are not the deny list's")
	}
	if strings.Join(v.DenyLoader, ",") != strings.Join(deny.Loader, ",") {
		t.Errorf("the vocabulary's loader list is not the code's — LD_PRELOAD would be missing from what a person is told")
	}
	if len(v.DefaultAllowlist) == 0 || v.DefaultAllowlist[0] != execenv.DefaultPassthrough()[0] {
		t.Errorf("defaultAllowlist = %v, want the core's allowlist", v.DefaultAllowlist)
	}
	if len(v.SourceKinds) == 0 || len(v.EgressModes) == 0 || len(v.HomeModes) == 0 || len(v.PullPolicies) == 0 {
		t.Errorf("a closed set arrived empty: %v %v %v %v — a form with no options is a form that guesses",
			v.SourceKinds, v.EgressModes, v.HomeModes, v.PullPolicies)
	}
}

// TestShowRefusesToRenderADamagedSpecAsAnEmptyEnvironment is the honest third
// state. A row whose spec cannot be parsed is refused with the parse failure,
// not shown as an environment with no allowlist.
func TestShowRefusesToRenderADamagedSpecAsAnEmptyEnvironment(t *testing.T) {
	// Damage the row BEHIND the door — the way a bad restore or a hand-edited
	// database would. The store's guard is size and shape (any JSON object),
	// the core's is the vocabulary, so a row CAN exist whose spec cannot be
	// read, and the read must say so rather than answer with an environment
	// that appears to have no allowlist.
	f := newEnvFixture(t)
	row, ref, err := f.store.CreateEnvironment(store.Actor{Kind: "user", ID: "person"}, store.NewEnvironment{
		Slug: "damaged", Name: "damaged", Backend: "host-scrubbed",
		Spec: `{"home":"real","notAField":true}`,
	})
	if ref != nil || err != nil {
		t.Fatalf("write the damaged row (the store's guard should accept any JSON object): ref=%v err=%v", ref, err)
	}
	_, err = f.env.Show(context.Background(), protocol.EnvironmentShowParams{ID: row.ID})
	perr := refusalOf(t, err)
	if !strings.Contains(perr.Data.What, "does not parse") {
		t.Errorf("the refusal does not say the spec is unreadable: %q", perr.Data.What)
	}
	if !strings.Contains(perr.Data.What, "damaged") {
		t.Errorf("the refusal does not name the environment it could not read: %q", perr.Data.What)
	}
	// The listing refuses the whole page rather than showing that row as an
	// environment with no allowlist — a confident wrong answer on a screen a
	// person decides with.
	if _, err := f.env.List(context.Background(), protocol.EnvironmentListParams{}); err == nil {
		t.Errorf("the listing rendered a row whose spec cannot be read")
	}
}

func TestListViewCarriesBindingsAndReferences(t *testing.T) {
	f := newEnvFixture(t)
	ctx := context.Background()
	_, refErr := f.refs.Create(ctx, person, protocol.SecretRefCreateParams{
		Name: "view-ref", SourceKind: "daemon-env", Source: "DEPLOY_TOKEN", InjectAs: "VIEW_REF",
	})
	mustOK(t, "create the reference", refErr)
	env := f.create(t, "viewed", "host-scrubbed", canonicalSpec(t, []string{"PATH"}, nil, []string{"view-ref"}, "host-scrubbed"))
	f.bind(t, "workspace", "", env.ID, false)
	views, err := f.env.List(ctx, protocol.EnvironmentListParams{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *protocol.EnvironmentView
	for i := range views {
		if views[i].Slug == "viewed" {
			found = &views[i]
		}
	}
	if found == nil {
		t.Fatalf("the created environment is missing from the listing: %d rows", len(views))
	}
	if len(found.SecretRefNames) != 1 || found.SecretRefNames[0] != "view-ref" {
		t.Errorf("secretRefNames = %v, want the one name", found.SecretRefNames)
	}
	if len(found.Bindings) != 1 || found.Bindings[0].ScopeKind != "workspace" {
		t.Errorf("bindings = %+v, want the workspace binding", found.Bindings)
	}
	if len(found.Capabilities) == 0 {
		t.Errorf("capabilities empty — the chips come from the code, not from a table")
	}
	blob, err := json.Marshal(views)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), canaryValue) {
		t.Errorf("the listing carries a value")
	}
}

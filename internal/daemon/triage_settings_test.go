package daemon

// ════════════════════════════════════════════════════════════════════════════
// S51 T3 — the validation blueprint is a SETTING (TM4): read per attempt,
// refused-not-fell-back when broken, mirrored by triage.health which never
// gates. The shape is S50 T3's twin on purpose; the tests are too.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/triage"
)

type triageSettingsFixture struct {
	d      *Daemon
	s      *store.Store
	cfg    *config.Store
	root   string
	starts chan protocol.LoopStartParams
}

// newTriageSettingsFixture builds the daemon the board tests build, plus the
// two things T3 needs: the seeded standard blueprints (so the declared
// default resolves as a real, validating row) and a spy on the loop.start
// door (so what an attempt STARTS is observable). With an env map the
// daemon mounts a real *config.Store over its own store; with nil, no
// settings mount at all — the registry default answers, exactly as before
// T3 (P2's point).
func newTriageSettingsFixture(t *testing.T, env map[string]string) *triageSettingsFixture {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	if _, err := st.CreateComponent(store.NewComponent{Name: "r", Slug: "r", Root: root}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	if err := SeedStandardBlueprints(st); err != nil {
		t.Fatalf("seed blueprints: %v", err)
	}
	d, _ := newTestDaemonWithStore(t, st)
	f := &triageSettingsFixture{d: d, s: st, root: root, starts: make(chan protocol.LoopStartParams, 16)}
	if env != nil {
		f.cfg = NewSettings(st, config.Options{
			Registry: config.DefaultRegistry(),
			LookupEnv: func(k string) (string, bool) {
				v, ok := env[k]
				return v, ok
			},
			Now: func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) },
		})
		d.settings = f.cfg
	}
	d.initTriage()
	d.Table.StartBlueprintLoop = func(_ context.Context, p protocol.LoopStartParams) (any, error) {
		f.starts <- p
		return map[string]any{"started": p.Slug}, nil
	}
	return f
}

func (f *triageSettingsFixture) fileBug(t *testing.T, title string) triage.Bug {
	t.Helper()
	bug, ref := f.d.Table.Triage.CreateAudited(f.root, triage.NewBug{Title: title, Body: "the body the attempt reads", FiledBy: "person"}, boardPerson())
	if ref != nil {
		t.Fatalf("file bug: %s", ref.Reason)
	}
	return bug
}

func (f *triageSettingsFixture) validate(id int64) error {
	_, err := f.d.Table.TriageValidate(context.Background(), protocol.TriageValidateParams{ID: id})
	return err
}

func (f *triageSettingsFixture) nextStart(t *testing.T) protocol.LoopStartParams {
	t.Helper()
	select {
	case p := <-f.starts:
		return p
	case <-time.After(2 * time.Second):
		t.Fatal("no loop.start reached the spy")
		return protocol.LoopStartParams{}
	}
}

func (f *triageSettingsFixture) drainStarts() {
	for {
		select {
		case <-f.starts:
		default:
			return
		}
	}
}

// putBlueprintRow is the roadmap fixture's trick for the triage store: a
// row laid straight into the table, past the write-time validation — the
// ONLY way a test gets an invalid row in front of health and dispatch.
func (f *triageSettingsFixture) putBlueprintRow(t *testing.T, name, content string) {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])
	err := f.s.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error {
		return store.BlueprintPutTx(tx, store.BlueprintRow{
			Name: name, Version: 1, Content: content, ContentHash: hash, Origin: "user",
			CreatedAt: "2026-11-04T12:00:00Z", UpdatedAt: "2026-11-04T12:00:00Z", UpdatedBy: "user:1",
		}, store.RevisionRow{Name: name, Rev: 1, Version: 1, Content: content, ContentHash: hash, Op: "save",
			ActorKind: "user", ActorID: "user:1", OccurredAt: "2026-11-04T12:00:00Z"}, nil)
	})
	if err != nil {
		t.Fatalf("put blueprint row %q: %v", name, err)
	}
}

func (f *triageSettingsFixture) bugVecRows(t *testing.T, id int64) int {
	t.Helper()
	bug, ok, err := f.s.BugByID(id)
	if err != nil || !ok {
		t.Fatalf("show bug %d: ok=%v err=%v", id, ok, err)
	}
	hits, err := f.s.QueryBugVec(f.root, triageEmbedDim, triageEmbedding(bug.Title+" "+bug.Body), 50)
	if err != nil {
		t.Fatalf("vec probe: %v", err)
	}
	n := 0
	for _, h := range hits {
		if h.BugID == id {
			n++
		}
	}
	return n
}

func TestTriageDefaultsReproduceTodaysValidate(t *testing.T) {
	// P2: with NO settings mounted, the attempt starts what the constant
	// started — `brainstorm` — because the registry default carries the
	// old value and nothing else spells it. The whole dispatch shape is
	// golden-pinned: goal, workspace, deterministic slug, the With keys
	// the reactor keys on.
	f := newTriageSettingsFixture(t, nil)
	bug := f.fileBug(t, "counter counts one too many")
	if err := f.validate(bug.ID); err != nil {
		t.Fatalf("default validate: %v", err)
	}
	start := f.nextStart(t)
	if start.Blueprint != "brainstorm" {
		t.Fatalf("the attempt started %q, want the declared default brainstorm", start.Blueprint)
	}
	if start.Slug != fmt.Sprintf("triage-validate-%d", bug.ID) || start.WorkspaceRoot != f.root {
		t.Fatalf("dispatch shape drifted: %+v", start)
	}
	if start.With["bug_id"] != fmt.Sprint(bug.ID) || start.With["role"] != "validate" {
		t.Fatalf("the reactor's With keys drifted: %+v", start.With)
	}
}

func TestTriageSettingChangeVisibleAtTheNextValidate(t *testing.T) {
	// No restart, no cache: `settings.set` and the VERY NEXT attempt
	// starts the new blueprint. `goal-to-brief` is a seeded standard, so
	// it is a legal choice. And BACK: unset returns the default.
	f := newTriageSettingsFixture(t, nil)
	f.d.settings = NewSettings(f.s, config.Options{
		Registry:  config.DefaultRegistry(),
		LookupEnv: func(string) (string, bool) { return "", false },
		Now:       func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) },
	})
	f.d.triageSettingView = nil // force the live d.settings path
	if _, _, err := f.d.settings.Set(context.Background(), triageValidationSetting, "goal-to-brief", "user:1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	bug := f.fileBug(t, "export drops the year")
	if err := f.validate(bug.ID); err != nil {
		t.Fatalf("validate with the changed setting: %v", err)
	}
	if start := f.nextStart(t); start.Blueprint != "goal-to-brief" {
		t.Fatalf("the attempt started %q, want goal-to-brief — the setting moved without a restart", start.Blueprint)
	}
	if _, _, err := f.d.settings.Unset(context.Background(), triageValidationSetting, "user:1"); err != nil {
		t.Fatalf("unset: %v", err)
	}
	bug2 := f.fileBug(t, "session header outlives the session")
	if err := f.validate(bug2.ID); err != nil {
		t.Fatalf("validate after unset: %v", err)
	}
	if start := f.nextStart(t); start.Blueprint != "brainstorm" {
		t.Fatalf("after unset the attempt started %q, want the default brainstorm", start.Blueprint)
	}
}

func TestTriageMissingOrInvalidChoiceRefusesNamedAndNeverFallsBack(t *testing.T) {
	// Missing: the refusal names the SETTING and the VALUE and points at
	// the panel — and NOTHING ran: no loop, no embedding, no quiet use
	// of the default.
	f := newTriageSettingsFixture(t, nil)
	f.d.settings = NewSettings(f.s, config.Options{
		Registry:  config.DefaultRegistry(),
		LookupEnv: func(string) (string, bool) { return "", false },
		Now:       func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) },
	})
	f.d.triageSettingView = nil
	if _, _, err := f.d.settings.Set(context.Background(), triageValidationSetting, "no-such-blueprint", "user:1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	bug := f.fileBug(t, "counter counts one too many")
	err := f.validate(bug.ID)
	if err == nil || !strings.Contains(err.Error(), `triage.validationBlueprint is "no-such-blueprint", which does not exist`) ||
		!strings.Contains(err.Error(), "Settings → Triage") {
		t.Fatalf("missing-choice refusal: %v", err)
	}
	f.drainStarts()
	if n := f.bugVecRows(t, bug.ID); n != 0 {
		t.Fatalf("the refused attempt stored %d embedding rows — the choice resolves BEFORE any side effect", n)
	}
	// Invalid: a stored row that does not validate refuses — "does not
	// validate", never the default brainstorm.
	f.putBlueprintRow(t, "broken-validate-host", "id: broken\nlanes: not-a-list [[\n")
	if _, _, err := f.d.settings.Set(context.Background(), triageValidationSetting, "broken-validate-host", "user:1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	bug2 := f.fileBug(t, "export drops the year")
	err = f.validate(bug2.ID)
	if err == nil || !strings.Contains(err.Error(), `triage.validationBlueprint is "broken-validate-host", which does not validate`) {
		t.Fatalf("invalid-choice refusal: %v", err)
	}
	f.drainStarts()
}

func TestTriageEnvOverrideBeatsStoredValue(t *testing.T) {
	// S44 precedence (D3): the environment shadows the stored row, and
	// the attempt reads the SHADOWED value — brainstorm is stored,
	// goal-to-brief is enviromed, and goal-to-brief runs.
	env := map[string]string{config.EnvName(triageValidationSetting): "goal-to-brief"}
	f := newTriageSettingsFixture(t, env)
	if _, _, err := f.cfg.Set(context.Background(), triageValidationSetting, "brainstorm", "user:1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	bug := f.fileBug(t, "counter counts one too many")
	if err := f.validate(bug.ID); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if start := f.nextStart(t); start.Blueprint != "goal-to-brief" {
		t.Fatalf("the attempt started %q, want the enviromed goal-to-brief (S44 precedence)", start.Blueprint)
	}
}

func triageHealthOf(t *testing.T, f *triageSettingsFixture) protocol.TriageHealthResult {
	t.Helper()
	res, err := f.d.triageHealth(context.Background())
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	out, ok := res.(protocol.TriageHealthResult)
	if !ok || len(out.Settings) != 1 {
		t.Fatalf("health shape: %#v", res)
	}
	return out
}

func TestTriageHealthStatesAndUnverifiable(t *testing.T) {
	f := newTriageSettingsFixture(t, nil)
	f.d.settings = NewSettings(f.s, config.Options{
		Registry:  config.DefaultRegistry(),
		LookupEnv: func(string) (string, bool) { return "", false },
		Now:       func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) },
	})
	f.d.triageSettingView = nil
	got := triageHealthOf(t, f)
	if got.EvaluatedAt == "" {
		t.Fatal("evaluatedAt empty")
	}
	e := got.Settings[0]
	if e.Key != triageValidationSetting || e.State != "ok" || e.Name != "brainstorm" {
		t.Fatalf("default health: %+v — want ok/brainstorm over the seeded row", e)
	}
	if e.ReportCounts == nil || e.ReportCounts.Valid == 0 {
		t.Fatalf("reportCounts = %+v, want the analyzer's valid checks beside an ok", e.ReportCounts)
	}
	if _, _, err := f.d.settings.Set(context.Background(), triageValidationSetting, "no-such-blueprint", "user:1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if e := triageHealthOf(t, f).Settings[0]; e.State != "missing" || e.Name != "no-such-blueprint" {
		t.Fatalf("missing state: %+v", e)
	}
	f.putBlueprintRow(t, "broken-choice", "id: broken\nlanes: not-a-list [[\n")
	if _, _, err := f.d.settings.Set(context.Background(), triageValidationSetting, "broken-choice", "user:1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if e := triageHealthOf(t, f).Settings[0]; e.State != "invalid" || e.ReportCounts == nil {
		t.Fatalf("invalid state: %+v", e)
	}
	// Unverifiable: the source itself cannot answer — S48 unwired (or a
	// source down) never reads ok. TM4's explicit promise.
	keep := f.d.bpSrc
	f.d.bpSrc = failingSource{}
	defer func() { f.d.bpSrc = keep }()
	if e := triageHealthOf(t, f).Settings[0]; e.State != "unverifiable" {
		t.Fatalf("source down answered %s, want unverifiable — never ok", e.State)
	}
}

func TestTriageHealthNeverGatesValidate(t *testing.T) {
	// The read and the act are decidable separately: with a legal
	// setting the attempt runs no matter how many times health was asked
	// first — a green or red health line changes nothing.
	f := newTriageSettingsFixture(t, nil)
	f.d.settings = NewSettings(f.s, config.Options{
		Registry:  config.DefaultRegistry(),
		LookupEnv: func(string) (string, bool) { return "", false },
		Now:       func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) },
	})
	f.d.triageSettingView = nil
	if _, _, err := f.d.settings.Set(context.Background(), triageValidationSetting, "goal-to-brief", "user:1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	for i := 0; i < 3; i++ {
		triageHealthOf(t, f)
	}
	bug := f.fileBug(t, "counter counts one too many")
	if err := f.validate(bug.ID); err != nil {
		t.Fatalf("health reads gated the attempt: %v", err)
	}
	if start := f.nextStart(t); start.Blueprint != "goal-to-brief" {
		t.Fatalf("started %q", start.Blueprint)
	}
}

func TestTriageReaderIsCalledPerAttemptNotAtConstruction(t *testing.T) {
	// The seam itself is the promise: every attempt re-reads. Two
	// attempts, two (at least) reads — a construction-time capture would
	// answer zero or one.
	f := newTriageSettingsFixture(t, nil)
	calls := 0
	f.d.triageSettingView = func(ctx context.Context, key string) (config.View, error) {
		calls++
		decl, ok := config.DefaultRegistry().Lookup(key)
		if !ok {
			return config.View{}, fmt.Errorf("no %s", key)
		}
		return config.View{Setting: decl, Value: decl.Default, Source: "default"}, nil
	}
	for _, title := range []string{"counter counts one too many", "export drops the year"} {
		bug := f.fileBug(t, title)
		if err := f.validate(bug.ID); err != nil {
			t.Fatalf("validate %q: %v", title, err)
		}
		f.nextStart(t)
	}
	if calls < 2 {
		t.Fatalf("the settings seam was read %d time(s) across two attempts — the reader must run AT DISPATCH, not at construction (TM4)", calls)
	}
}

package daemon

// roadmap_settings_test.go — S50 T3 (RM3/RM6): the two blueprint choices are
// settings read AT DISPATCH; a broken choice refuses with its own name and
// never falls back; health mirrors the same facts and never gates.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

// roadmapFixtureSettingsHook is read by newRoadmapFixtureRooted at New time;
// set it through withSettings and the fixture's daemon mounts a real
// *config.Store over its own store. Nil (every older fixture) leaves the
// daemon settings-less: the registry defaults answer, exactly as before T3.
var roadmapFixtureSettingsHook func(*store.Store) *config.Store

func withSettings(t *testing.T, env map[string]string, build func(*testing.T, *roadmapFixture, *config.Store)) {
	t.Helper()
	var captured *config.Store
	roadmapFixtureSettingsHook = func(s *store.Store) *config.Store {
		captured = NewSettings(s, config.Options{
			Registry: config.DefaultRegistry(),
			LookupEnv: func(k string) (string, bool) {
				v, ok := env[k]
				return v, ok
			},
			Now: func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) },
		})
		return captured
	}
	defer func() { roadmapFixtureSettingsHook = nil }()
	f := newRoadmapFixture(t)
	build(t, f, captured)
}

func drainStarts(f *roadmapFixture) {
	for {
		select {
		case <-f.starts:
		default:
			return
		}
	}
}

// putBlueprintRow lays a row straight into the blueprint table, past the
// daemon's write-time validation — the ONLY way a test gets an invalid row
// in front of health and dispatch, which is exactly what those tests need.
func putBlueprintRow(t *testing.T, f *roadmapFixture, name, content string) {
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

func TestDefaultsReproduceTodaysDispatch(t *testing.T) {
	// P2: with NO settings mounted, both dispatches start what the
	// constants started — ideation `brainstorm`, plan host `roadmap-plan`
	// — because the registry defaults carry the old values and nothing
	// else spells them.
	f := newRoadmapFixture(t)
	it := f.create(t, "defaults")
	if _, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"},
		protocol.RoadmapIdeateParams{ID: it.ID, WorkspaceRoot: f.root}); err != nil {
		t.Fatalf("default ideation dispatch: %v", err)
	}
	start := <-f.starts
	if start.Blueprint != "brainstorm" {
		t.Fatalf("ideation started %q, want the declared default brainstorm", start.Blueprint)
	}
	it = f.walkToBriefAgreed(t, it.ID)
	if _, err := f.d.Table.RoadmapPlan(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"},
		protocol.RoadmapPlanParams{ID: it.ID, WorkspaceRoot: f.root}); err != nil {
		t.Fatalf("default plan dispatch: %v", err)
	}
	pinned, err := f.s.LoopBlueprintID(store.Scope{ComponentRoot: it.ComponentRoot, LoopSlug: fmt.Sprintf("plan-item-%d", it.ID)})
	if err != nil {
		t.Fatalf("read the host pin: %v", err)
	}
	if pinned != "roadmap-plan" {
		t.Fatalf("plan host pinned %q, want the declared default roadmap-plan", pinned)
	}
}

func TestSettingChangeIsVisibleAtTheNextDispatch(t *testing.T) {
	// No restart, no cache: `settings.set` and the VERY NEXT dispatch
	// starts the new blueprint. `goal-to-brief` is the second seeded
	// standard that declares brief.md, so it is a legal choice.
	withSettings(t, nil, func(t *testing.T, f *roadmapFixture, st *config.Store) {
		if _, _, err := st.Set(context.Background(), roadmapIdeationSetting, "goal-to-brief", "user:1"); err != nil {
			t.Fatalf("set: %v", err)
		}
		it := f.create(t, "changed mind")
		if _, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"},
			protocol.RoadmapIdeateParams{ID: it.ID, WorkspaceRoot: f.root}); err != nil {
			t.Fatalf("ideate with the changed setting: %v", err)
		}
		start := <-f.starts
		if start.Blueprint != "goal-to-brief" {
			t.Fatalf("ideation started %q, want goal-to-brief — the setting moved without a restart", start.Blueprint)
		}
		// And BACK: unset returns the default on the next dispatch, too.
		if _, _, err := st.Unset(context.Background(), roadmapIdeationSetting, "user:1"); err != nil {
			t.Fatalf("unset: %v", err)
		}
		it2 := f.create(t, "default again")
		if _, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"},
			protocol.RoadmapIdeateParams{ID: it2.ID, WorkspaceRoot: f.root}); err != nil {
			t.Fatalf("ideate after unset: %v", err)
		}
		start = <-f.starts
		if start.Blueprint != "brainstorm" {
			t.Fatalf("after unset ideation started %q, want the default brainstorm", start.Blueprint)
		}
	})
}

func TestMissingOrInvalidChoiceRefusesNamedAndNeverFallsBack(t *testing.T) {
	// Missing (ideation): the refusal names the SETTING and the VALUE and
	// points at the panel. Nothing claimed, no loop started, and the
	// default NOT quietly used.
	withSettings(t, nil, func(t *testing.T, f *roadmapFixture, st *config.Store) {
		if _, _, err := st.Set(context.Background(), roadmapIdeationSetting, "no-such-blueprint", "user:1"); err != nil {
			t.Fatalf("set: %v", err)
		}
		it := f.create(t, "refused")
		_, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"},
			protocol.RoadmapIdeateParams{ID: it.ID, WorkspaceRoot: f.root})
		if err == nil || !strings.Contains(err.Error(), `roadmap.ideationBlueprint is "no-such-blueprint", which does not exist`) ||
			!strings.Contains(err.Error(), "Settings → Roadmap") {
			t.Fatalf("missing-choice refusal: %v", err)
		}
		if got := f.show(t, it.ID); got.Status != roadmap.StatusIdea {
			t.Fatalf("the refused dispatch moved the item to %s — the choice resolves BEFORE anything claims", got.Status)
		}
		drainStarts(f)
	})
	// Invalid (plan): a stored row that does not validate refuses over the
	// PLAN key — "does not validate", never the default roadmap-plan.
	withSettings(t, nil, func(t *testing.T, f *roadmapFixture, st *config.Store) {
		putBlueprintRow(t, f, "broken-plan-host", "id: broken\nlanes: not-a-list [[\n")
		if _, _, err := st.Set(context.Background(), roadmapPlanSetting, "broken-plan-host", "user:1"); err != nil {
			t.Fatalf("set: %v", err)
		}
		it := f.create(t, "plan refused")
		f.walkToBriefAgreed(t, it.ID)
		_, err := f.d.Table.RoadmapPlan(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"},
			protocol.RoadmapPlanParams{ID: it.ID, WorkspaceRoot: f.root})
		if err == nil || !strings.Contains(err.Error(), `roadmap.planBlueprint is "broken-plan-host", which does not validate`) {
			t.Fatalf("invalid-choice plan refusal: %v", err)
		}
	})
}

func TestEnvOverrideBeatsStoredValue(t *testing.T) {
	// S44 precedence (D3): the environment shadows the stored row, and
	// the dispatch reads the SHADOWED value — here brainstorm is stored,
	// goal-to-brief is enviromed, and goal-to-brief runs.
	env := map[string]string{config.EnvName(roadmapIdeationSetting): "goal-to-brief"}
	withSettings(t, env, func(t *testing.T, f *roadmapFixture, st *config.Store) {
		if _, _, err := st.Set(context.Background(), roadmapIdeationSetting, "brainstorm", "user:1"); err != nil {
			t.Fatalf("set: %v", err)
		}
		it := f.create(t, "env wins")
		if _, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"},
			protocol.RoadmapIdeateParams{ID: it.ID, WorkspaceRoot: f.root}); err != nil {
			t.Fatalf("ideate: %v", err)
		}
		start := <-f.starts
		if start.Blueprint != "goal-to-brief" {
			t.Fatalf("ideation started %q, want the enviromed goal-to-brief (S44 precedence)", start.Blueprint)
		}
	})
}

// failingSource is a blueprint.Source whose store said nothing provable —
// health's honest `unverifiable`, and a dispatch refusal, never a borrowed ok.
type failingSource struct{}

func (failingSource) Get(string) ([]byte, bool, error) { return nil, false, fmt.Errorf("the source is down") }
func (failingSource) Names() ([]string, error)         { return nil, fmt.Errorf("the source is down") }
func (failingSource) Standard() []string               { return nil }

func TestHealthStatesAndUnverifiable(t *testing.T) {
	withSettings(t, nil, func(t *testing.T, f *roadmapFixture, st *config.Store) {
		health := func() protocol.RoadmapHealthResult {
			t.Helper()
			res, err := f.d.roadmapHealth(context.Background())
			if err != nil {
				t.Fatalf("health: %v", err)
			}
			out, ok := res.(protocol.RoadmapHealthResult)
			if !ok || len(out.Settings) != 2 {
				t.Fatalf("health shape: %#v", res)
			}
			return out
		}
		got := health()
		for _, e := range got.Settings {
			if e.State != "ok" || e.Name == "" {
				t.Fatalf("default health %s = %s/%q, want ok with a name (seeded standards validate)", e.Key, e.State, e.Name)
			}
			if e.ReportCounts == nil || e.ReportCounts.Valid == 0 {
				t.Fatalf("%s reportCounts = %+v, want the analyzer's valid checks beside an ok", e.Key, e.ReportCounts)
			}
		}
		if got.EvaluatedAt == "" {
			t.Fatal("evaluatedAt empty")
		}
		if _, _, err := st.Set(context.Background(), roadmapIdeationSetting, "no-such-blueprint", "user:1"); err != nil {
			t.Fatalf("set: %v", err)
		}
		if e := health().Settings[0]; e.State != "missing" || e.Name != "no-such-blueprint" {
			t.Fatalf("missing state: %s/%q", e.State, e.Name)
		}
		putBlueprintRow(t, f, "broken-choice", "id: broken\nlanes: not-a-list [[\n")
		if _, _, err := st.Set(context.Background(), roadmapPlanSetting, "broken-choice", "user:1"); err != nil {
			t.Fatalf("set: %v", err)
		}
		e := health().Settings[1]
		if e.State != "invalid" || e.ReportCounts == nil {
			t.Fatalf("invalid state: %s counts %+v", e.State, e.ReportCounts)
		}
		// Unverifiable: the source itself cannot answer. An UNWIRED S48
		// never reads ok — that is RM6's explicit promise.
		keep := f.d.bpSrc
		f.d.bpSrc = failingSource{}
		defer func() { f.d.bpSrc = keep }()
		for _, e := range health().Settings {
			if e.State != "unverifiable" {
				t.Fatalf("source down: %s answered %s, want unverifiable — never ok", e.Key, e.State)
			}
		}
	})
}

func TestHealthNeverGatesDispatch(t *testing.T) {
	// The read and the act are decidable separately: health answers, and
	// the dispatch reaches its OWN same facts at its OWN moment. With a
	// legal setting the dispatch runs no matter how many times health was
	// asked first — a green or red health line changes nothing.
	withSettings(t, nil, func(t *testing.T, f *roadmapFixture, st *config.Store) {
		if _, _, err := st.Set(context.Background(), roadmapIdeationSetting, "goal-to-brief", "user:1"); err != nil {
			t.Fatalf("set: %v", err)
		}
		for i := 0; i < 3; i++ {
			if _, err := f.d.roadmapHealth(context.Background()); err != nil {
				t.Fatalf("health read %d: %v", i, err)
			}
		}
		it := f.create(t, "ungated")
		if _, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"},
			protocol.RoadmapIdeateParams{ID: it.ID, WorkspaceRoot: f.root}); err != nil {
			t.Fatalf("health reads gated the dispatch: %v", err)
		}
		start := <-f.starts
		if start.Blueprint != "goal-to-brief" {
			t.Fatalf("started %q", start.Blueprint)
		}
	})
}

func TestReaderIsCalledPerDispatchNotAtConstruction(t *testing.T) {
	// The seam itself is the promise: every dispatch re-reads. Two
	// dispatches, two (at least) reads — a construction-time capture
	// would answer exactly one.
	f := newRoadmapFixture(t)
	calls := 0
	f.d.roadmapSettingView = func(ctx context.Context, key string) (config.View, error) {
		calls++
		decl, ok := config.DefaultRegistry().Lookup(key)
		if !ok {
			return config.View{}, fmt.Errorf("no %s", key)
		}
		return config.View{Setting: decl, Value: decl.Default, Source: "default"}, nil
	}
	it := f.create(t, "spy")
	f.walkToBriefAgreed(t, it.ID)
	for i := 0; i < 2; i++ {
		_, _ = f.d.Table.RoadmapPlan(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"},
			protocol.RoadmapPlanParams{ID: it.ID, WorkspaceRoot: f.root}) // second refuses on the host key — after reading
	}
	if calls < 2 {
		t.Fatalf("the settings seam was read %d time(s) across two dispatches — the reader must run AT DISPATCH, not at construction (RM3)", calls)
	}
}

func TestRoadmapDeclarationsAreValidAndEnvNamesDoNotCollide(t *testing.T) {
	reg := config.DefaultRegistry()
	seen := map[string]string{}
	for _, s := range reg.All() {
		env := config.EnvName(s.Key)
		if other, dup := seen[env]; dup {
			t.Fatalf("env collision: %s and %s both map to %s (CS4)", other, s.Key, env)
		}
		seen[env] = s.Key
	}
	for _, tc := range []struct{ key, def string }{
		{roadmapIdeationSetting, "brainstorm"},
		{roadmapPlanSetting, "roadmap-plan"},
	} {
		decl, ok := reg.Lookup(tc.key)
		if !ok {
			t.Fatalf("%s is not declared — the dispatch reads its default from the registry or nowhere", tc.key)
		}
		if decl.Default != tc.def {
			t.Fatalf("%s default %q, want %q — P2: defaults reproduce today's dispatch", tc.key, decl.Default, tc.def)
		}
		if decl.Sensitive {
			t.Fatalf("%s is Sensitive — a blueprint name is not a credential (CS8)", tc.key)
		}
	}
	_ = blueprint.Blueprint{}
}

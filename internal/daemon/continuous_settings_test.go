package daemon

// continuous_settings_test.go — S27 T8 (CN21/CN22): the scheduler's tuning
// knobs are declared settings and NOTHING else. What each test pins:
//
//   - the four declarations are the single truth — the daemon's compiled
//     defaults ARE config.Registry's declared strings, not a second set of
//     literals (TestSchedulerDeclaredDefaultsMatchTheDeclarations);
//   - scheduler.status answers each setting with the layer it ACTUALLY came
//     from — declared-default, setting (database), environment — never a
//     word that flatters a missing row (TestStatusSettingsCarryTheHonestSourceWord);
//   - CN21's live law: one write to scheduler.maxAgentSlots between two
//     ticks changes what the SECOND tick does, with no daemon restart
//     (TestSettingChangeIsLive);
//   - and the negative that makes the whole slice honest: no authority key
//     — enabled, paused, root rows — is anywhere in the settings plane
//     (checked at the registry itself in internal/config; here the read
//     side proves the answer carries authority through the socket view,
//     not through a setting).

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/continuous"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/store"
)

// newSchedSettingsEnv is newSchedEnv's shape WITH the settings plane wired
// (the production daemon always wires it — serve.go; this is the test's
// way of being the production mount).
type schedSettingsEnv struct {
	d        *Daemon
	s        *store.Store
	a        *continuousAdapter
	w        *continuous.Worker
	settings *config.Store
	root     string
	bpPath   string
}

func newSchedSettingsEnv(t *testing.T) *schedSettingsEnv {
	t.Helper()
	s := recoveryStore(t)
	set := NewSettings(s, config.Options{})
	d := New(Options{Store: s, Settings: set})
	bp := writeGateBlueprint(t, "sched-settings")
	root := t.TempDir()
	if _, err := s.CreateComponent(store.NewComponent{Name: "sched-set", Slug: "sched-set", Root: root}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	user := store.SchedulerActor{Kind: "user", ID: "test"}
	if _, err := s.SetSchedulerState(user, true, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertSchedulerRoot(user, store.SchedulerRootRow{
		ComponentRoot: root, Enabled: true, Blueprint: bp, Tech: "go", WithJSON: `{"route":"aeon"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetComponentQueueSettings(root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	a := &continuousAdapter{d: d, locks: map[string]*sync.Mutex{}, weights: map[string]int{}}
	return &schedSettingsEnv{d: d, s: s, a: a, settings: set,
		w: continuous.NewWorker(a, a, nil, a, nil), root: root, bpPath: bp}
}

func (e *schedSettingsEnv) tickNow() {
	e.w.Tick(context.Background(), time.Now().UTC())
}

// readyItem appends a queue item on any root and walks it to ready.
func (e *schedSettingsEnv) readyItem(t *testing.T, root, goal string) store.QueueItemRow {
	t.Helper()
	q := queue.New(e.s)
	it, err := q.Append(root, e.bpPath, goal, root, queue.NewItem{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ref := q.Transition(it, queue.StatusReady); ref != nil {
		t.Fatalf("ready: %s", ref.Reason)
	}
	row, ok, err := e.s.QueueItemByID(it.ID)
	if err != nil || !ok {
		t.Fatalf("the row behind item %d is gone", it.ID)
	}
	return row
}

// fileSecondRoot files another component + governed scheduler root and
// walks its queue settings the same way the first root's were walked.
func (e *schedSettingsEnv) fileSecondRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := e.s.CreateComponent(store.NewComponent{Name: "sched-set-b", Slug: "sched-set-b", Root: root}); err != nil {
		t.Fatalf("seed component b: %v", err)
	}
	user := store.SchedulerActor{Kind: "user", ID: "test"}
	if _, err := e.s.UpsertSchedulerRoot(user, store.SchedulerRootRow{
		ComponentRoot: root, Enabled: true, Blueprint: e.bpPath, Tech: "go", WithJSON: `{"route":"aeon"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.SetComponentQueueSettings(root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	return root
}

func (e *schedSettingsEnv) setSetting(t *testing.T, key, raw string) {
	t.Helper()
	if _, _, err := e.settings.Set(context.Background(), key, raw, "user:operator"); err != nil {
		t.Fatalf("set %s=%s: %v", key, raw, err)
	}
}

// TestSchedulerDeclaredDefaultsMatchTheDeclarations is the single-source
// pin: the adapter's compiled fallbacks ARE config.Registry's declared
// strings. Change a default in settings_decl_scheduler.go and this test —
// not a silent behavior drift — is what speaks.
func TestSchedulerDeclaredDefaultsMatchTheDeclarations(t *testing.T) {
	reg := config.DefaultRegistry()
	for key, want := range map[string]int{
		schedulerMaxSlotsSetting:  declaredMaxSlotsDefault,
		schedulerTickSetting:      declaredTickDefault,
		schedulerMaxStartsSetting: declaredMaxStartsDefault,
		schedulerAgingSetting:     declaredAgingDefault,
	} {
		d, ok := reg.Lookup(key)
		if !ok {
			t.Fatalf("%s is not declared", key)
		}
		n, err := strconv.Atoi(d.Default)
		if err != nil {
			t.Fatalf("%s default %q is not a number", key, d.Default)
		}
		if n != want {
			t.Errorf("%s: daemon fallback is %d, the declaration says %d — one truth, please", key, want, n)
		}
	}
}

// TestStatusSettingsCarryTheHonestSourceWord pins CN22's honesty on the
// read side: with no rows the answer is the DECLARED default wearing the
// word declared-default; a database row reads as setting with its value;
// and an environment value reads as environment — the layer that actually
// answered, never the flattest word.
func TestStatusSettingsCarryTheHonestSourceWord(t *testing.T) {
	s := recoveryStore(t)
	user := store.SchedulerActor{Kind: "user", ID: "test"}
	if _, err := s.SetSchedulerState(user, true, false, ""); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{config.EnvName(schedulerAgingSetting): "48"}
	schedulerMaxSlotsEnv := config.EnvName(schedulerMaxSlotsSetting)
	set := NewSettings(s, config.Options{LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }})
	d := New(Options{Store: s, Settings: set})
	door := NewContinuousService(d)
	ctx := context.Background()

	view, err := door.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]string{}
	val := map[string]string{}
	for _, row := range view.Settings {
		byKey[row.Key] = row.Source
		val[row.Key] = row.Value
	}
	if len(view.Settings) != 4 {
		t.Fatalf("the settings answer carries %d rows: %v", len(view.Settings), view.Settings)
	}
	if byKey[schedulerMaxSlotsSetting] != "declared-default" || val[schedulerMaxSlotsSetting] != strconv.Itoa(declaredMaxSlotsDefault) {
		t.Errorf("%s answered %s/%s — with no row it is the declared default and SAYS so", schedulerMaxSlotsSetting, val[schedulerMaxSlotsSetting], byKey[schedulerMaxSlotsSetting])
	}
	if byKey[schedulerAgingSetting] != "environment" || val[schedulerAgingSetting] != "48" {
		t.Errorf("%s answered %s/%s — the environment layer answered and must be NAMED", schedulerAgingSetting, val[schedulerAgingSetting], byKey[schedulerAgingSetting])
	}
	if _, _, err := set.Set(ctx, schedulerMaxSlotsSetting, "5", "user:operator"); err != nil {
		t.Fatal(err)
	}
	view, err = door.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range view.Settings {
		if row.Key != schedulerMaxSlotsSetting {
			continue
		}
		if row.Source != "setting" || row.Value != "5" {
			t.Errorf("%s answered %s/%s after the row — the database layer answered and must be NAMED", row.Key, row.Value, row.Source)
		}
	}
	// A value the registry cannot honour reads as the honest fourth word,
	// never as a silently trusted one. The SET door refuses an
	// out-of-bounds write at the declaration (Min 1) — so the invalid value
	// is planted where it happens in real life: the environment layer, a
	// row-shadowing word the operator exported by hand.
	env[schedulerMaxSlotsEnv] = "0"
	if v, err := set.Int(ctx, schedulerMaxSlotsSetting); err == nil && v == 0 {
		t.Error("an unusable env value answered zero through Int — the declared default governs")
	}
	view, err = door.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range view.Settings {
		if row.Key == schedulerMaxSlotsSetting && row.Source != "unreadable-default" {
			t.Errorf("a stored-but-invalid row read as %s — the honest word is unreadable-default with the declared value: %s", row.Source, row.Value)
		}
	}
}

// TestSettingChangeIsLive is T8's named test for CN21: no restart, no
// relaunch — one write between two ticks and the SECOND tick behaves
// differently. Slots cap 2 starts the second repository; the cap dropped
// to 1 defers the third with the capacity word; the cap restored starts it.
func TestSettingChangeIsLive(t *testing.T) {
	e := newSchedSettingsEnv(t)
	e.readyItem(t, e.root, "starts under the default cap")
	e.tickNow()
	rowsA, err := e.s.Decisions(store.DecisionQuery{Decision: "started", ComponentRoot: e.root})
	if err != nil || len(rowsA) == 0 {
		t.Fatalf("tick 1 started nothing under the declared cap: %v rows err %v", len(rowsA), err)
	}
	// The gate blueprint parks its loop WAITING; CN9 counts RUNNING loops
	// against the slots. Walk the started loop to running — the same status
	// a real loop reaches when its first step spawns — so the capacity the
	// second tick must honour is a slot that is really spent.
	if err := e.s.SetLoopStatus(store.Scope{LoopSlug: expectedSlug(rowsA[0].SubjectKind, rowsA[0].SubjectID), ComponentRoot: e.root}, "running"); err != nil {
		t.Fatal(err)
	}

	// One write, no restart — the cap drops to 1.
	e.setSetting(t, schedulerMaxSlotsSetting, "1")

	rootB := e.fileSecondRoot(t)
	itemB := e.readyItem(t, rootB, "arrives after the cap dropped")
	e.tickNow()
	deferred, err := e.s.Decisions(store.DecisionQuery{Decision: "deferred", Reason: "capacity", ComponentRoot: rootB})
	if err != nil || len(deferred) == 0 {
		t.Fatalf("tick 2 started B under a cap the setting had already lowered to 1 — the change was not live (err %v)", err)
	}
	if deferred[0].SubjectID != itemB.ID {
		t.Errorf("the capacity line names subject %d, not the new item %d", deferred[0].SubjectID, itemB.ID)
	}

	// And live the other way: restore the cap and the SAME pending item
	// starts on the next tick — no restart, no relaunch.
	e.setSetting(t, schedulerMaxSlotsSetting, "2")
	e.tickNow()
	startedB, err := e.s.Decisions(store.DecisionQuery{Decision: "started", ComponentRoot: rootB})
	if err != nil || len(startedB) == 0 {
		t.Fatalf("tick 3 still defers B after the cap was restored — liveness must run both directions (err %v)", err)
	}
}

// TestSettingsAnswerCarriesNoAuthorityWord closes the read side of the
// wall: the settings list is four tuning numbers and nothing else — the
// master's state travels in the socket view's own fields, never here.
func TestSettingsAnswerCarriesNoAuthorityWord(t *testing.T) {
	e := newSchedSettingsEnv(t)
	view, err := NewContinuousService(e.d).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range view.Settings {
		k := strings.ToLower(row.Key)
		for _, w := range []string{"enabled", "disable", "paused", "pause", "master", "authority", "stop"} {
			if strings.Contains(k, w) {
				t.Errorf("%s is an authority key — the wall between tuning and power has a hole", row.Key)
			}
		}
	}
	if len(view.Settings) != 4 {
		t.Errorf("the settings answer carries %d rows — exactly four tuning knobs exist", len(view.Settings))
	}
}

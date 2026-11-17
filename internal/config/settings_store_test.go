package config

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// settings_store_test.go — T3's accessor, tested entirely against an in-memory
// fakeRepo (CS7/CS10): fast, hermetic, no SQLite. T4 proves the same contract
// against the real store. The fake has a mutex (so the concurrent test is clean
// under -race), a call log (so a test can assert WHICH repo calls ran — the
// no-cache and no-partial-write pins) and an injectable failure per method.

// --- the fake repo ---

// fakeRepo is an in-memory Repo. Its mutex is there so TestConcurrentSets
// LastWriterWinsNoRace is clean under -race; it is NOT a cache — every method
// re-reads the map, exactly as the real repo would.
type fakeRepo struct {
	mu           sync.Mutex
	rows         map[string]RepoRow
	events       []RepoEvent
	callLog      []string
	getErr       error
	listErr      error
	putErr       error
	deleteErr    error
	eventListErr error
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[string]RepoRow{}} }

// seedRow plants a row directly (bypassing Set) so a test can start from a
// stored state — e.g. a row an env must beat, or a stored value that fails the
// current declaration. It records no event.
func (r *fakeRepo) seedRow(key, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[key] = RepoRow{Key: key, Value: value}
}

// seedEvent plants an audit entry directly (bypassing Set) so a test can
// assert History over a key not in the registry.
func (r *fakeRepo) seedEvent(ev RepoEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *fakeRepo) countCall(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.callLog {
		if c == name {
			n++
		}
	}
	return n
}

func (r *fakeRepo) totalCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.callLog)
}

func (r *fakeRepo) eventCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func (r *fakeRepo) lastEvent() RepoEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return RepoEvent{}
	}
	return r.events[len(r.events)-1]
}

// SettingGet records the call and returns the row. A repo read error is
// returned (P31), never a default.
func (r *fakeRepo) SettingGet(ctx context.Context, key string) (RepoRow, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callLog = append(r.callLog, "SettingGet")
	if r.getErr != nil {
		return RepoRow{}, false, r.getErr
	}
	row, ok := r.rows[key]
	return row, ok, nil
}

// SettingList returns every row (the Store never calls it, but the seam needs
// it). Records the call.
func (r *fakeRepo) SettingList(ctx context.Context) ([]RepoRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callLog = append(r.callLog, "SettingList")
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]RepoRow, 0, len(r.rows))
	for _, row := range r.rows {
		out = append(out, row)
	}
	return out, nil
}

// SettingPut is the one write. It fills ev.OldValue from the row it replaced
// (a first write leaves OldValue nil), appends the event, and updates the row
// — in one transaction. A putErr leaves NO partial view: the row is unchanged
// and no event is appended.
func (r *fakeRepo) SettingPut(ctx context.Context, row RepoRow, ev RepoEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callLog = append(r.callLog, "SettingPut")
	if r.putErr != nil {
		return r.putErr
	}
	if old, ok := r.rows[row.Key]; ok {
		ov := old.Value
		ev.OldValue = &ov
	} else {
		ev.OldValue = nil
	}
	r.rows[row.Key] = row
	r.events = append(r.events, ev)
	return nil
}

// SettingDelete fills ev.OldValue from the row it deletes and appends the
// event. Absent → existed=false, no event (idempotent-delete).
func (r *fakeRepo) SettingDelete(ctx context.Context, key string, ev RepoEvent) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callLog = append(r.callLog, "SettingDelete")
	if r.deleteErr != nil {
		return false, r.deleteErr
	}
	old, ok := r.rows[key]
	if !ok {
		return false, nil
	}
	ov := old.Value
	ev.OldValue = &ov
	delete(r.rows, key)
	r.events = append(r.events, ev)
	return true, nil
}

// SettingEventList returns the events (newest first), filtered by key ("").
func (r *fakeRepo) SettingEventList(ctx context.Context, key string, limit int) ([]RepoEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callLog = append(r.callLog, "SettingEventList")
	if r.eventListErr != nil {
		return nil, r.eventListErr
	}
	var out []RepoEvent
	for _, e := range r.events {
		if key == "" || e.Key == key {
			out = append(out, e)
		}
	}
	// newest first: reverse the append (insertion) order
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// --- the test wiring ---

type testConfig struct {
	env map[string]string
	now time.Time
	reg *Registry
}

type testOpt func(*testConfig)

// withEnv injects an env map (a key present but "" is "set to empty").
func withEnv(env map[string]string) testOpt {
	return func(c *testConfig) {
		if env != nil {
			c.env = env
		}
	}
}

// withClock injects a fixed clock.
func withClock(now time.Time) testOpt { return func(c *testConfig) { c.now = now } }

// withRegistry overrides the (default) test registry.
func withRegistry(r *Registry) testOpt { return func(c *testConfig) { c.reg = r } }

// newTestStore wires a fake repo, the T2 test registry, an injected env map and
// a fixed clock. It returns both so a test can inspect the fake's call log,
// events and rows.
func newTestStore(t *testing.T, opts ...testOpt) (*Store, *fakeRepo) {
	t.Helper()
	cfg := testConfig{
		env: map[string]string{},
		now: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC),
		reg: testRegistry(t),
	}
	for _, o := range opts {
		o(&cfg)
	}
	fake := newFakeRepo()
	s := NewStore(fake, Options{
		Registry:  cfg.reg,
		LookupEnv: func(name string) (string, bool) { v, ok := cfg.env[name]; return v, ok },
		Now:       func() time.Time { return cfg.now },
	})
	return s, fake
}

// fixedNow is the clock every newTestStore shares.
var fixedNow = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// --- the tests ---

// TestGetUnsetKeyAnswersDefault: a declared, never-written key answers its
// default with Source=default, Stored=nil, no error (absence is a normal
// answer, not a fault — DB19).
func TestGetUnsetKeyAnswersDefault(t *testing.T) {
	s, _ := newTestStore(t)
	v, err := s.Get(context.Background(), "test.flag")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.Source != SettingDefault {
		t.Fatalf("Source = %s, want default", v.Source)
	}
	if v.Value != v.Default {
		t.Fatalf("Value = %q, want default %q", v.Value, v.Default)
	}
	if v.Stored != nil {
		t.Fatalf("Stored = %v, want nil", *v.Stored)
	}
}

// TestGetUndeclaredKeyNamesNearest: an undeclared key is ErrSettingUnknown whose
// Do lists the nearest declared keys (a typo must not create a silent no-op).
func TestGetUndeclaredKeyNamesNearest(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.Get(context.Background(), "test.notThere")
	if err == nil {
		t.Fatal("expected ErrSettingUnknown")
	}
	e, ok := err.(*ErrSettingUnknown)
	if !ok {
		t.Fatalf("expected *ErrSettingUnknown, got %T", err)
	}
	if len(e.Nearest) == 0 {
		t.Fatal("Nearest is empty")
	}
	if !strings.Contains(e.Do(), e.Nearest[0]) {
		t.Fatalf("Do must list the nearest declared key %q: %q", e.Nearest[0], e.Do())
	}
}

// TestSetThenGetSameStoreSameCall: a write is visible on the very next read,
// same Store, same process (CS7: the next read sees the write).
func TestSetThenGetSameStoreSameCall(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	if _, changed, err := s.Set(ctx, "test.flag", "true", "actor"); err != nil || !changed {
		t.Fatalf("Set: changed=%v err=%v", changed, err)
	}
	v, err := s.Get(ctx, "test.flag")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.Value != "true" || v.Source != SettingDatabase {
		t.Fatalf("Get = %q/%s, want true/database", v.Value, v.Source)
	}
}

// TestSetVisibleThroughASecondStoreOverTheSameRepo: two Stores over one fake
// repo — a write via A is read via B with no sleep (any per-instance state
// would break this; CS7).
func TestSetVisibleThroughASecondStoreOverTheSameRepo(t *testing.T) {
	fake := newFakeRepo()
	mk := func() *Store {
		return NewStore(fake, Options{
			Registry:  testRegistry(t),
			LookupEnv: func(string) (string, bool) { return "", false },
			Now:       func() time.Time { return fixedNow },
		})
	}
	ctx := context.Background()
	a, b := mk(), mk()
	if _, changed, err := a.Set(ctx, "test.flag", "true", "actor"); err != nil || !changed {
		t.Fatalf("a.Set: changed=%v err=%v", changed, err)
	}
	v, err := b.Get(ctx, "test.flag")
	if err != nil {
		t.Fatalf("b.Get: %v", err)
	}
	if v.Value != "true" || v.Source != SettingDatabase {
		t.Fatalf("b.Get = %q/%s, want true/database (write via A visible via B)", v.Value, v.Source)
	}
}

// TestStoreHoldsNoCache: reflection asserts Store's fields are exactly
// repo, reg, lookupEnv, now (no map, no mutex, no timestamp), and the repo's
// call log shows one SettingGet per Get across 5 consecutive reads (no cache).
func TestStoreHoldsNoCache(t *testing.T) {
	tt := reflect.TypeOf((*Store)(nil)).Elem()
	got := make([]string, tt.NumField())
	for i := range got {
		got[i] = tt.Field(i).Name
	}
	want := []string{"repo", "reg", "lookupEnv", "now"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Store fields = %v, want %v", got, want)
	}
	s, fake := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := s.Get(ctx, "test.flag"); err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}
	}
	if n := fake.countCall("SettingGet"); n != 5 {
		t.Fatalf("SettingGet calls = %d, want 5 (one per Get, no cache)", n)
	}
}

// TestSetRefusesInvalidValueAndWritesNothing: a value that fails the key's
// type/bounds is refused before any repo write (validation before write).
func TestSetRefusesInvalidValueAndWritesNothing(t *testing.T) {
	s, fake := newTestStore(t)
	_, _, err := s.Set(context.Background(), "test.count", "11", "actor") // Max 10
	if err == nil {
		t.Fatal("expected ErrSettingInvalidValue")
	}
	if _, ok := err.(*ErrSettingInvalidValue); !ok {
		t.Fatalf("expected *ErrSettingInvalidValue, got %T", err)
	}
	if n := fake.countCall("SettingPut"); n != 0 {
		t.Fatalf("SettingPut calls = %d, want 0 (validation before write)", n)
	}
}

// TestSetRefusesEmptyActor: an unattributed write is refused, before any repo
// call (the audit actor column is NOT NULL).
func TestSetRefusesEmptyActor(t *testing.T) {
	s, fake := newTestStore(t)
	_, _, err := s.Set(context.Background(), "test.flag", "true", "")
	if err == nil {
		t.Fatal("expected a refusal for an empty actor")
	}
	if n := fake.totalCalls(); n != 0 {
		t.Fatalf("repo calls = %d, want 0 (actor checked before any repo call)", n)
	}
}

// TestSetUnchangedWritesNoEvent: a second identical write is a no-op —
// changed=false, no write, no event (an audit trail of no-ops is noise).
func TestSetUnchangedWritesNoEvent(t *testing.T) {
	s, fake := newTestStore(t)
	ctx := context.Background()
	if _, changed, err := s.Set(ctx, "test.flag", "true", "actor"); err != nil || !changed {
		t.Fatalf("first Set: changed=%v err=%v", changed, err)
	}
	if _, changed, err := s.Set(ctx, "test.flag", "true", "actor"); err != nil || changed {
		t.Fatalf("second Set: changed=%v err=%v (want changed=false, no error)", changed, err)
	}
	if n := fake.countCall("SettingPut"); n != 1 {
		t.Fatalf("SettingPut calls = %d, want 1", n)
	}
	if n := fake.eventCount(); n != 1 {
		t.Fatalf("event count = %d, want 1", n)
	}
}

// TestSetChangedWritesOneEventWithOldAndNew: a change 3→4 records exactly one
// event carrying old=3, new=4, the actor and the clock time (the trail must
// have before/after).
func TestSetChangedWritesOneEventWithOldAndNew(t *testing.T) {
	s, fake := newTestStore(t)
	fake.seedRow("test.count", "3") // start from a stored value, no event
	if _, changed, err := s.Set(context.Background(), "test.count", "4", "actor"); err != nil || !changed {
		t.Fatalf("Set 4: changed=%v err=%v", changed, err)
	}
	evt := fake.lastEvent()
	if evt.Op != "set" {
		t.Fatalf("Op = %q, want set", evt.Op)
	}
	if evt.OldValue == nil || *evt.OldValue != "3" {
		t.Fatalf("OldValue = %v, want 3", evt.OldValue)
	}
	if evt.NewValue == nil || *evt.NewValue != "4" {
		t.Fatalf("NewValue = %v, want 4", evt.NewValue)
	}
	if evt.Actor != "actor" {
		t.Fatalf("Actor = %q, want actor", evt.Actor)
	}
	if evt.OccurredAt != fixedNow.Format(time.RFC3339) {
		t.Fatalf("OccurredAt = %q, want %q", evt.OccurredAt, fixedNow.Format(time.RFC3339))
	}
}

// TestSetFirstWriteEventHasNilOld: the first write's event has OldValue == nil
// (NULL, not "", for "there was none").
func TestSetFirstWriteEventHasNilOld(t *testing.T) {
	s, fake := newTestStore(t)
	if _, _, err := s.Set(context.Background(), "test.flag", "true", "actor"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	evt := fake.lastEvent()
	if evt.OldValue != nil {
		t.Fatalf("first write's OldValue = %v, want nil", *evt.OldValue)
	}
}

// TestSetSensitiveKeyMarksTheEvent: a Sensitive key's event carries
// Sensitive=true; a non-sensitive one, false (CS8).
func TestSetSensitiveKeyMarksTheEvent(t *testing.T) {
	s1, f1 := newTestStore(t)
	if _, _, err := s1.Set(context.Background(), "test.guard", "true", "actor"); err != nil {
		t.Fatalf("Set test.guard: %v", err)
	}
	if !f1.lastEvent().Sensitive {
		t.Fatal("test.guard event Sensitive = false, want true (CS8)")
	}
	s2, f2 := newTestStore(t)
	if _, _, err := s2.Set(context.Background(), "test.flag", "true", "actor"); err != nil {
		t.Fatalf("Set test.flag: %v", err)
	}
	if f2.lastEvent().Sensitive {
		t.Fatal("test.flag event Sensitive = true, want false")
	}
}

// TestUnsetRemovesAndRecords: Set then Unset → Source=default, Stored=nil,
// changed=true, and an event with op=unset, new=nil (an unset leaves no row and
// leaves a trail).
func TestUnsetRemovesAndRecords(t *testing.T) {
	s, fake := newTestStore(t)
	ctx := context.Background()
	if _, _, err := s.Set(ctx, "test.flag", "true", "actor"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, changed, err := s.Unset(ctx, "test.flag", "actor"); err != nil || !changed {
		t.Fatalf("Unset: changed=%v err=%v", changed, err)
	}
	v, err := s.Get(ctx, "test.flag")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.Source != SettingDefault || v.Stored != nil {
		t.Fatalf("after Unset: Source=%s Stored=%v, want default/nil", v.Source, v.Stored)
	}
	evt := fake.lastEvent()
	if evt.Op != "unset" {
		t.Fatalf("Op = %q, want unset", evt.Op)
	}
	if evt.NewValue != nil {
		t.Fatalf("NewValue = %v, want nil", *evt.NewValue)
	}
}

// TestUnsetOfAbsentKeyIsChangedFalse: unsetting a key with no row is
// changed=false, no event, no error (idempotent-delete).
func TestUnsetOfAbsentKeyIsChangedFalse(t *testing.T) {
	s, fake := newTestStore(t)
	if _, changed, err := s.Unset(context.Background(), "test.flag", "actor"); err != nil || changed {
		t.Fatalf("Unset of absent key: changed=%v err=%v (want changed=false, no error)", changed, err)
	}
	if n := fake.eventCount(); n != 0 {
		t.Fatalf("event count = %d, want 0", n)
	}
}

// TestEnvBeatsDatabase: an env that is in force beats the stored row —
// Value=env, Source=environment, Stored=the row, ShadowedBy=the env var name
// (CS4 precedence, and the stored value is not hidden).
func TestEnvBeatsDatabase(t *testing.T) {
	s, fake := newTestStore(t, withEnv(map[string]string{"AEON_SETTING_TEST_FLAG": "true"}))
	fake.seedRow("test.flag", "false")
	v, err := s.Get(context.Background(), "test.flag")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.Value != "true" || v.Source != SettingEnvironment {
		t.Fatalf("Value=%q Source=%s, want true/environment", v.Value, v.Source)
	}
	if v.Stored == nil || *v.Stored != "false" {
		t.Fatalf("Stored = %v, want &false", v.Stored)
	}
	if v.ShadowedBy != "AEON_SETTING_TEST_FLAG" {
		t.Fatalf("ShadowedBy = %q, want AEON_SETTING_TEST_FLAG", v.ShadowedBy)
	}
}

// TestEnvAbsentFallsBackToDatabase: with no env, the row wins (an env lookup
// must not leak across keys).
func TestEnvAbsentFallsBackToDatabase(t *testing.T) {
	s, fake := newTestStore(t)
	fake.seedRow("test.flag", "true")
	v, err := s.Get(context.Background(), "test.flag")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.Value != "true" || v.Source != SettingDatabase {
		t.Fatalf("Value=%q Source=%s, want true/database (no env → database)", v.Value, v.Source)
	}
}

// TestEnvInvalidIsNotUsedAndIsNamed: an env that fails validation is not used
// and is named — the read falls to the row (source=database) but carries
// envInvalid.
func TestEnvInvalidIsNotUsedAndIsNamed(t *testing.T) {
	s, fake := newTestStore(t, withEnv(map[string]string{"AEON_SETTING_TEST_FLAG": "maybe"}))
	fake.seedRow("test.flag", "true")
	v, err := s.Get(context.Background(), "test.flag")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.Value != "true" || v.Source != SettingDatabase {
		t.Fatalf("Value=%q Source=%s, want true/database (invalid env not used)", v.Value, v.Source)
	}
	if v.EnvInvalid != "AEON_SETTING_TEST_FLAG" {
		t.Fatalf("EnvInvalid = %q, want AEON_SETTING_TEST_FLAG", v.EnvInvalid)
	}
}

// TestEmptyEnvVarIsReportedInvalidForNonString: an env set to "" for a bool is
// reported (EnvInvalid), not silently ignored — the common "exported but empty"
// mistake must be loud.
func TestEmptyEnvVarIsReportedInvalidForNonString(t *testing.T) {
	s, _ := newTestStore(t, withEnv(map[string]string{"AEON_SETTING_TEST_FLAG": ""}))
	v, err := s.Get(context.Background(), "test.flag")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.EnvInvalid != "AEON_SETTING_TEST_FLAG" {
		t.Fatalf("EnvInvalid = %q, want AEON_SETTING_TEST_FLAG (exported-but-empty bool must be loud)", v.EnvInvalid)
	}
	if v.Source != SettingDefault {
		t.Fatalf("Source = %s, want default (no row, invalid env)", v.Source)
	}
}

// TestWriteWhileEnvShadowsStillStoresAndSaysSo: a write while the environment
// shadows the key still succeeds and is stored — but the result says so
// (Source=environment, ShadowedBy set) and the audit row records
// shadowed_by="env".
func TestWriteWhileEnvShadowsStillStoresAndSaysSo(t *testing.T) {
	s, fake := newTestStore(t, withEnv(map[string]string{"AEON_SETTING_TEST_FLAG": "true"}))
	v, changed, err := s.Set(context.Background(), "test.flag", "false", "actor")
	if err != nil || !changed {
		t.Fatalf("Set: changed=%v err=%v", changed, err)
	}
	if v.Source != SettingEnvironment {
		t.Fatalf("View.Source = %s, want environment", v.Source)
	}
	if v.ShadowedBy == "" {
		t.Fatal("View.ShadowedBy is empty (the env shadows the stored row)")
	}
	if _, ok := fake.rows["test.flag"]; !ok {
		t.Fatal("the row was not stored")
	}
	if evt := fake.lastEvent(); evt.ShadowedBy != "env" {
		t.Fatalf("event ShadowedBy = %q, want env", evt.ShadowedBy)
	}
}

// TestStoredInvalidReadsDefaultAndFlags: a stored value that fails the current
// declaration reads as the default with StoredInvalid=true and the raw text —
// loud, never fatal to a consumer (CS10).
func TestStoredInvalidReadsDefaultAndFlags(t *testing.T) {
	s, fake := newTestStore(t)
	fake.seedRow("test.flag", "banana") // invalid for a bool
	v, err := s.Get(context.Background(), "test.flag")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !v.StoredInvalid {
		t.Fatal("StoredInvalid = false, want true")
	}
	if v.Value != "false" || v.Value != v.Default {
		t.Fatalf("Value = %q, want default %q", v.Value, v.Default)
	}
	if v.Source != SettingDefault {
		t.Fatalf("Source = %s, want default", v.Source)
	}
	if v.Stored == nil || *v.Stored != "banana" {
		t.Fatalf("Stored = %v, want &banana", v.Stored)
	}
}

// TestRepoErrorOnReadIsReturnedNotDefaulted: a repo read error is returned, not
// swallowed into a default (P31) — Get and every Reader call return it.
func TestRepoErrorOnReadIsReturnedNotDefaulted(t *testing.T) {
	s, fake := newTestStore(t)
	fake.getErr = errors.New("repo read failed")
	ctx := context.Background()
	if _, err := s.Get(ctx, "test.flag"); err == nil {
		t.Fatal("Get expected an error")
	}
	if _, err := s.Bool(ctx, "test.flag"); err == nil {
		t.Fatal("Bool expected an error")
	}
	if _, err := s.Int(ctx, "test.count"); err == nil {
		t.Fatal("Int expected an error")
	}
	if _, err := s.String(ctx, "test.label"); err == nil {
		t.Fatal("String expected an error")
	}
	if _, err := s.Enum(ctx, "test.mode"); err == nil {
		t.Fatal("Enum expected an error")
	}
}

// TestRepoErrorOnPutLeavesNoPartialView: a repo write error leaves no partial
// view — Set returns the error, changed=false, and a following Get shows the
// old value.
func TestRepoErrorOnPutLeavesNoPartialView(t *testing.T) {
	s, fake := newTestStore(t)
	fake.putErr = errors.New("repo write failed")
	fake.seedRow("test.flag", "false") // the old value
	_, changed, err := s.Set(context.Background(), "test.flag", "true", "actor")
	if err == nil {
		t.Fatal("Set expected an error")
	}
	if changed {
		t.Fatal("changed = true, want false (a failed write is not a change)")
	}
	v, err := s.Get(context.Background(), "test.flag")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.Value != "false" {
		t.Fatalf("Get = %q, want the old value false (no partial view)", v.Value)
	}
}

// TestListReturnsDeclaredKeysOnlyAndSorted: only declared keys are listed
// (orphan repo rows are not), sorted by area then key, with the area filter
// working.
func TestListReturnsDeclaredKeysOnlyAndSorted(t *testing.T) {
	s, fake := newTestStore(t)
	fake.seedRow("other.orphan", "x") // an undeclared row in the repo
	views, err := s.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, v := range views {
		if v.Key == "other.orphan" {
			t.Fatal("an orphan repo row was listed")
		}
	}
	for i := 1; i < len(views); i++ {
		prev, cur := views[i-1], views[i]
		if prev.Area > cur.Area || (prev.Area == cur.Area && prev.Key > cur.Key) {
			t.Fatalf("not sorted by area then key: %q/%q before %q/%q", prev.Area, prev.Key, cur.Area, cur.Key)
		}
	}
	views, err = s.List(context.Background(), "test")
	if err != nil {
		t.Fatalf("List test: %v", err)
	}
	for _, v := range views {
		if v.Area != "test" {
			t.Fatalf("area filter leaked %q (area %q)", v.Key, v.Area)
		}
	}
}

// TestListAreaFilterUnknownAreaIsEmptyNotError: an unknown area is an empty
// slice, not a refusal.
func TestListAreaFilterUnknownAreaIsEmptyNotError(t *testing.T) {
	s, _ := newTestStore(t)
	views, err := s.List(context.Background(), "nope")
	if err != nil {
		t.Fatalf("List nope: %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("List nope = %d, want 0", len(views))
	}
}

// TestListOnEmptyRegistryIsEmptyNotError: an empty registry lists nothing,
// with no error (the empty state is honest).
func TestListOnEmptyRegistryIsEmptyNotError(t *testing.T) {
	empty, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	s, _ := newTestStore(t, withRegistry(empty))
	views, err := s.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("List on empty registry = %d, want 0", len(views))
	}
}

// TestHistoryNewestFirstAndLimits: events are newest first; the default limit
// is 50; a limit over 500 is clamped (does not error); key "" is all events.
func TestHistoryNewestFirstAndLimits(t *testing.T) {
	s, fake := newTestStore(t)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		if _, _, err := s.Set(ctx, "test.count", strconv.Itoa(i), "actor"); err != nil {
			t.Fatalf("Set %d: %v", i, err)
		}
	}
	// default (limit 0 → 50): all five, newest first (5 is last written)
	ev, err := s.History(ctx, "test.count", 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(ev) != 5 {
		t.Fatalf("History default = %d, want 5", len(ev))
	}
	if ev[0].NewValue == nil || *ev[0].NewValue != "5" {
		t.Fatalf("newest = %v, want 5", ev[0].NewValue)
	}
	// clamp (limit 1000 → 500): still the five available, no error
	ev, err = s.History(ctx, "test.count", 1000)
	if err != nil {
		t.Fatalf("History 1000: %v", err)
	}
	if len(ev) != 5 {
		t.Fatalf("History 1000 = %d, want 5", len(ev))
	}
	// key "" → all events
	ev, err = s.History(ctx, "", 0)
	if err != nil {
		t.Fatalf("History empty key: %v", err)
	}
	if len(ev) != 5 {
		t.Fatalf("History empty key = %d, want 5", len(ev))
	}
	_ = fake
}

// TestHistoryUndeclaredKeyStillReadable: history of a key not in the registry
// (removed or never declared) still returns its events — the audit is not
// hidden when a declaration is deleted.
func TestHistoryUndeclaredKeyStillReadable(t *testing.T) {
	r, err := NewRegistry(Setting{Key: "test.count", Type: TypeInt, Min: intPtr(1), Max: intPtr(10), Default: "3", Description: "d"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	s, fake := newTestStore(t, withRegistry(r))
	fake.seedEvent(RepoEvent{Key: "test.flag", Op: "set", Actor: "actor"})
	ev, err := s.History(context.Background(), "test.flag", 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(ev) != 1 {
		t.Fatalf("History = %d, want 1 (a key removed from the registry keeps its trail)", len(ev))
	}
}

// TestReaderTypedHelpers: Bool/Int/String/Enum return the effective value for
// each declared type.
func TestReaderTypedHelpers(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	for _, c := range []struct{ key, val string }{{"test.flag", "true"}, {"test.count", "7"}, {"test.label", "hello"}, {"test.mode", "manual"}} {
		if _, _, err := s.Set(ctx, c.key, c.val, "actor"); err != nil {
			t.Fatalf("Set %s: %v", c.key, err)
		}
	}
	if b, err := s.Bool(ctx, "test.flag"); err != nil || b != true {
		t.Fatalf("Bool = %v err=%v, want true", b, err)
	}
	if n, err := s.Int(ctx, "test.count"); err != nil || n != 7 {
		t.Fatalf("Int = %d err=%v, want 7", n, err)
	}
	if v, err := s.String(ctx, "test.label"); err != nil || v != "hello" {
		t.Fatalf("String = %q err=%v, want hello", v, err)
	}
	if v, err := s.Enum(ctx, "test.mode"); err != nil || v != "manual" {
		t.Fatalf("Enum = %q err=%v, want manual", v, err)
	}
}

// TestReaderWrongTypeIsAProgrammerError: reading a key as a type it is not
// declared as is ErrSettingWrongType (naming both types), not a coerced guess.
func TestReaderWrongTypeIsAProgrammerError(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.Bool(context.Background(), "test.count") // an int key
	if err == nil {
		t.Fatal("expected ErrSettingWrongType")
	}
	e, ok := err.(*ErrSettingWrongType)
	if !ok {
		t.Fatalf("expected *ErrSettingWrongType, got %T", err)
	}
	if e.Key != "test.count" || e.Declared != TypeInt || e.Asked != TypeBool {
		t.Fatalf("ErrSettingWrongType = {Key:%q Declared:%s Asked:%s}, want {test.count int bool}", e.Key, e.Declared, e.Asked)
	}
}

// TestReaderOnStoredInvalidReturnsDefaultNilError: a stored value that fails
// the current declaration returns the effective (default) value through the
// Reader with a nil error — the loud signal is in Get's View, not the hot path.
func TestReaderOnStoredInvalidReturnsDefaultNilError(t *testing.T) {
	s, fake := newTestStore(t)
	fake.seedRow("test.flag", "banana") // invalid for a bool
	b, err := s.Bool(context.Background(), "test.flag")
	if err != nil {
		t.Fatalf("Bool: %v (a stored-invalid read returns the default, not an error)", err)
	}
	if b {
		t.Fatal("Bool = true, want false (the default)")
	}
}

// TestCredentialNamedKeyWriteRefusedDefensively: a registry mutated to contain a
// credential-named key (bypassing NewRegistry, which would refuse it at init)
// is still refused by the write path's defensive re-check (CS5) — no repo call.
func TestCredentialNamedKeyWriteRefusedDefensively(t *testing.T) {
	r := &Registry{
		byKey: map[string]Setting{
			"test.flag":   {Key: "test.flag", Area: "test", Type: TypeBool, Default: "false", Description: "d"},
			"test.apiKey": {Key: "test.apiKey", Area: "test", Type: TypeString, Default: "x", Description: "a credential-shaped name smuggled into the registry"},
		},
		byEnv: map[string]string{},
	}
	s, fake := newTestStore(t, withRegistry(r))
	_, _, err := s.Set(context.Background(), "test.apiKey", "x", "actor")
	if err == nil {
		t.Fatal("expected ErrSettingNotWritable for a credential-named key")
	}
	if _, ok := err.(*ErrSettingNotWritable); !ok {
		t.Fatalf("expected *ErrSettingNotWritable, got %T", err)
	}
	if n := fake.totalCalls(); n != 0 {
		t.Fatalf("repo calls = %d, want 0 (credential check before any repo call)", n)
	}
}

// TestSetClockAndActorRecorded: the event's OccurredAt is the injected clock's
// RFC3339 (no wall-clock leak) and the row's UpdatedBy is the actor.
func TestSetClockAndActorRecorded(t *testing.T) {
	s, fake := newTestStore(t, withClock(fixedNow))
	ctx := context.Background()
	if _, _, err := s.Set(ctx, "test.flag", "true", "alice"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	evt := fake.lastEvent()
	if evt.OccurredAt != fixedNow.Format(time.RFC3339) {
		t.Fatalf("OccurredAt = %q, want %q", evt.OccurredAt, fixedNow.Format(time.RFC3339))
	}
	if evt.Actor != "alice" {
		t.Fatalf("Actor = %q, want alice", evt.Actor)
	}
	v, err := s.Get(ctx, "test.flag")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.UpdatedBy != "alice" {
		t.Fatalf("UpdatedBy = %q, want alice", v.UpdatedBy)
	}
}

// TestValueAtLengthCapAccepted: a string at MaxLen is accepted, MaxLen+1 is
// refused (the boundary).
func TestValueAtLengthCapAccepted(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	if _, _, err := s.Set(ctx, "test.label", strings.Repeat("a", 16), "actor"); err != nil { // MaxLen 16
		t.Fatalf("Set at MaxLen: %v", err)
	}
	if _, _, err := s.Set(ctx, "test.label", strings.Repeat("a", 17), "actor"); err == nil {
		t.Fatal("Set at MaxLen+1 was accepted")
	}
}

// TestConcurrentSetsLastWriterWinsNoRace: 20 goroutines write distinct values
// under -race; the final read is one of them and every changing write left
// exactly one event (no lost audit, no data race).
func TestConcurrentSetsLastWriterWinsNoRace(t *testing.T) {
	s, fake := newTestStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, _ = s.Set(ctx, "test.label", "v"+strconv.Itoa(i), "actor")
		}(i)
	}
	wg.Wait()
	v, err := s.Get(ctx, "test.label")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		seen["v"+strconv.Itoa(i)] = true
	}
	if !seen[v.Value] {
		t.Fatalf("final value %q is not one of the 20 set values", v.Value)
	}
	// all 20 values are distinct, so no write is a no-op: 20 changing writes,
	// 20 events.
	if n := fake.eventCount(); n != 20 {
		t.Fatalf("event count = %d, want 20 (every write changed the value)", n)
	}
}

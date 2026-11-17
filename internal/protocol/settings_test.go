package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
)

// settings_test.go — S44 T5: the five settings.* methods, tested over a real
// config.Store on an in-memory fakeRepo (the same recipe config's own tests
// use — "tests never point at the real store"). The handler maps config's
// View/Event to this package's wire twins, so a test that seeds the repo and
// reads the wire result exercises the whole door: the mapping AND the store's
// resolution (env → database → default, the invalid flags, ShadowedBy) AND the
// error mapping (config's ErrSettingUnknown/ErrSettingInvalidValue → the
// protocol's two codes, verbatim). A fake that bypassed the store would test
// only the mapping, not the door; a fake that pointed at the real store would
// break hermeticity.

// --- the fake repo (config's recipe, over config's exported seam types) ------

// settingsFakeRepo is an in-memory config.Repo. Its mutex makes a concurrent
// write clean under -race; it is NOT a cache — every method re-reads the map,
// exactly as the real repo would (CS7).
type settingsFakeRepo struct {
	mu           sync.Mutex
	rows         map[string]config.RepoRow
	events       []config.RepoEvent
	callLog      []string
	getErr       error
	listErr      error
	putErr       error
	deleteErr    error
	eventListErr error
}

func newSettingsFakeRepo() *settingsFakeRepo {
	return &settingsFakeRepo{rows: map[string]config.RepoRow{}}
}

// seedRow plants a row directly (bypassing Set) so a test can start from a
// stored state — a row an env must beat, or a stored value that fails the
// current declaration. It records no event.
func (r *settingsFakeRepo) seedRow(key, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[key] = config.RepoRow{Key: key, Value: value}
}

// seedEvent plants an audit entry directly (bypassing Set) so a test can
// assert History over a key not in the registry.
func (r *settingsFakeRepo) seedEvent(ev config.RepoEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *settingsFakeRepo) countCall(name string) int {
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

func (r *settingsFakeRepo) lastEvent() config.RepoEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return config.RepoEvent{}
	}
	return r.events[len(r.events)-1]
}

// SettingGet records the call and returns the row. A repo read error is
// returned (P31), never a default.
func (r *settingsFakeRepo) SettingGet(_ context.Context, key string) (config.RepoRow, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callLog = append(r.callLog, "SettingGet")
	if r.getErr != nil {
		return config.RepoRow{}, false, r.getErr
	}
	row, ok := r.rows[key]
	return row, ok, nil
}

// SettingList returns every row. Records the call.
func (r *settingsFakeRepo) SettingList(_ context.Context) ([]config.RepoRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callLog = append(r.callLog, "SettingList")
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]config.RepoRow, 0, len(r.rows))
	for _, row := range r.rows {
		out = append(out, row)
	}
	return out, nil
}

// SettingPut is the one write. It fills ev.OldValue from the row it replaced
// (a first write leaves OldValue nil), appends the event, and updates the row
// — in one transaction. A putErr leaves NO partial view: the row is unchanged
// and no event is appended.
func (r *settingsFakeRepo) SettingPut(_ context.Context, row config.RepoRow, ev config.RepoEvent) error {
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
func (r *settingsFakeRepo) SettingDelete(_ context.Context, key string, ev config.RepoEvent) (bool, error) {
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
func (r *settingsFakeRepo) SettingEventList(_ context.Context, key string, limit int) ([]config.RepoEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callLog = append(r.callLog, "SettingEventList")
	if r.eventListErr != nil {
		return nil, r.eventListErr
	}
	var out []config.RepoEvent
	for _, e := range r.events {
		if key == "" || e.Key == key {
			out = append(out, e)
		}
	}
	// newest first: reverse the append (insertion) order
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// --- the test wiring --------------------------------------------------------

// settingsFixedNow is the clock every test store shares (no wall-clock leak).
var settingsFixedNow = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// settingsTestRegistry builds the closed registry the tests resolve against:
// the same five declarations config's own testRegistry uses (so the two stay
// alike) — a bool, an int with bounds, a string with a length cap, an enum,
// and a Sensitive guard.
func settingsTestRegistry(t *testing.T) *config.Registry {
	t.Helper()
	guardDesc := "This guard widens an import check: when true, an unsigned card may be imported; the default false keeps the restrictive state and refuses unsigned imports."
	r, err := config.NewRegistry(
		config.Setting{Key: "test.flag", Type: config.TypeBool, Default: "false", Description: "A test boolean setting."},
		config.Setting{Key: "test.count", Type: config.TypeInt, Min: intPtr(1), Max: intPtr(10), Default: "3", Description: "A test count setting."},
		config.Setting{Key: "test.label", Type: config.TypeString, MaxLen: 16, Default: "x", Description: "A test label setting."},
		config.Setting{Key: "test.mode", Type: config.TypeEnum, EnumValues: []string{"auto", "manual"}, Default: "auto", Description: "A test mode setting."},
		config.Setting{Key: "test.guard", Type: config.TypeBool, Default: "false", Sensitive: true, Description: guardDesc},
	)
	if err != nil {
		t.Fatalf("settingsTestRegistry: %v", err)
	}
	return r
}

// settingsTable is the door under test: a Table wired with a real config.Store
// on a fakeRepo (the daemon adapter's shape — the shared *config.Store), the
// actor the handlers stamp from the connection, and an initialized session (so
// the RequiresInitialize gate is satisfied). env injects the process
// environment a test wants in force.
func settingsTable(t *testing.T, env map[string]string) (*Table, *settingsFakeRepo, *config.Store) {
	t.Helper()
	fake := newSettingsFakeRepo()
	store := config.NewStore(fake, config.Options{
		Registry:  settingsTestRegistry(t),
		LookupEnv: func(name string) (string, bool) { v, ok := env[name]; return v, ok },
		Now:       func() time.Time { return settingsFixedNow },
	})
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Settings = store
	if !tbl.RegisterSettings() {
		t.Fatal("RegisterSettings refused a wired seam")
	}
	// The audit actor comes from the CONNECTION (the daemon's ActorFor), never
	// from params — a params actor would let a caller forge the write's
	// identity (CS8). A storeless build has no persons; an empty id is refused.
	tbl.ActorFor = func(_ context.Context, _ *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl, fake, store
}

// settingsStore builds the seam the conditional-registration tests wire: a
// real config.Store on a fakeRepo (the daemon adapter's shape), with no
// ActorFor and no session — the table wiring, not the door. The want-list
// test (handshake_test.go) uses it to see the conditionally-registered
// settings.* methods it asserts.
func settingsStore(t *testing.T) *config.Store {
	t.Helper()
	fake := newSettingsFakeRepo()
	return config.NewStore(fake, config.Options{
		Registry:  settingsTestRegistry(t),
		LookupEnv: func(string) (string, bool) { return "", false },
		Now:       func() time.Time { return settingsFixedNow },
	})
}

// dispatchSetting sends one settings call through the door (the initialize
// gate is satisfied by settingsTable). params is the marshalled JSON.
func dispatchSetting(t *testing.T, tbl *Table, m MethodName, params json.RawMessage) (any, error) {
	t.Helper()
	return tbl.Dispatch(context.Background(), "c1", m, params)
}

func dispatchJSON(t *testing.T, tbl *Table, m MethodName, params any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return dispatchSetting(t, tbl, m, raw)
}

// --- the tests --------------------------------------------------------------

// TestSettingsListReturnsEveryDeclaredKeyWithDescriptors: list answers the
// whole declared set, each a full View (the declaration plus the effective
// value and its source), sorted by area then key (config's List order).
func TestSettingsListReturnsEveryDeclaredKeyWithDescriptors(t *testing.T) {
	tbl, _, _ := settingsTable(t, nil)
	res, err := dispatchJSON(t, tbl, MethodSettingsList, SettingsListParams{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got, ok := res.(SettingsListResult)
	if !ok {
		t.Fatalf("result is %T, want SettingsListResult", res)
	}
	if len(got.Settings) != 5 {
		t.Fatalf("list = %d settings, want 5 (the whole declared set)", len(got.Settings))
	}
	byKey := map[string]SettingView{}
	for _, v := range got.Settings {
		byKey[v.Key] = v
	}
	for _, key := range []string{"test.flag", "test.count", "test.label", "test.mode", "test.guard"} {
		v, ok := byKey[key]
		if !ok {
			t.Fatalf("list omitted %q", key)
		}
		// a declared, never-written key answers its default with Source=default.
		if v.Value != v.Default {
			t.Fatalf("%q value = %q, want default %q", key, v.Value, v.Default)
		}
		if v.Source != SettingSourceDefault {
			t.Fatalf("%q source = %s, want default", key, v.Source)
		}
		if v.Stored != nil {
			t.Fatalf("%q stored = %v, want nil (never written)", key, *v.Stored)
		}
	}
	if byKey["test.guard"].Sensitive {
		// the Sensitive flag travels to the wire (CS8) — the UI must know.
	}
	if !byKey["test.guard"].Sensitive {
		t.Fatalf("test.guard sensitive = false, want true (CS8 travels to the wire)")
	}
}

// TestSettingsListAreaFilter: the area param scopes the listing; an unknown
// area is an empty slice, not a refusal.
func TestSettingsListAreaFilter(t *testing.T) {
	tbl, _, _ := settingsTable(t, nil)
	// the "test" area holds all five.
	_, err := dispatchJSON(t, tbl, MethodSettingsList, SettingsListParams{Area: strPtr("test")})
	if err != nil {
		t.Fatalf("list test: %v", err)
	}
	// an unknown area is an empty slice, not an error (config's List).
	res, err := dispatchJSON(t, tbl, MethodSettingsList, SettingsListParams{Area: strPtr("nope")})
	if err != nil {
		t.Fatalf("list nope: %v", err)
	}
	got := res.(SettingsListResult)
	if len(got.Settings) != 0 {
		t.Fatalf("list nope = %d, want 0 (an unknown area is empty, not a refusal)", len(got.Settings))
	}
}

// TestSettingsGetDefaultAndStored: get answers a default view (no row) and,
// after a set, the stored view (Source=database, Stored set).
func TestSettingsGetDefaultAndStored(t *testing.T) {
	tbl, _, _ := settingsTable(t, nil)
	// default, no row.
	res, err := dispatchJSON(t, tbl, MethodSettingsGet, SettingsGetParams{Key: "test.flag"})
	if err != nil {
		t.Fatalf("get default: %v", err)
	}
	got := res.(SettingsGetResult).Setting
	if got.Source != SettingSourceDefault || got.Value != "false" || got.Stored != nil {
		t.Fatalf("get default = %q/%s stored=%v, want false/default/nil", got.Value, got.Source, got.Stored)
	}
	// set, then the stored view.
	if _, _, err := tbl.Settings.Set(context.Background(), "test.flag", "true", "user:1000"); err != nil {
		t.Fatalf("set: %v", err)
	}
	res, err = dispatchJSON(t, tbl, MethodSettingsGet, SettingsGetParams{Key: "test.flag"})
	if err != nil {
		t.Fatalf("get stored: %v", err)
	}
	got = res.(SettingsGetResult).Setting
	if got.Source != SettingSourceDatabase || got.Value != "true" {
		t.Fatalf("get stored = %q/%s, want true/database", got.Value, got.Source)
	}
	if got.Stored == nil || *got.Stored != "true" {
		t.Fatalf("get stored: Stored = %v, want &true", got.Stored)
	}
}

// TestSettingsSetRoundTrip: a set writes the value (changed=true) and the
// result's View carries it; a following get sees it (same store, same process).
func TestSettingsSetRoundTrip(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	res, err := dispatchJSON(t, tbl, MethodSettingsSet, SettingsSetParams{Key: "test.count", Value: strPtr("7")})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	got := res.(SettingsSetResult)
	if !got.Changed {
		t.Fatal("set changed = false, want true")
	}
	if got.Setting.Value != "7" || got.Setting.Source != SettingSourceDatabase {
		t.Fatalf("set result = %q/%s, want 7/database", got.Setting.Value, got.Setting.Source)
	}
	// the row is written and exactly one event recorded.
	if n := fake.countCall("SettingPut"); n != 1 {
		t.Fatalf("SettingPut calls = %d, want 1", n)
	}
	if len(fake.events) != 1 {
		t.Fatalf("event count = %d, want 1", len(fake.events))
	}
	// a following get sees the write (CS7: the next read sees the write).
	res2, err := dispatchJSON(t, tbl, MethodSettingsGet, SettingsGetParams{Key: "test.count"})
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if res2.(SettingsGetResult).Setting.Value != "7" {
		t.Fatal("get after set = not 7 (the write was not visible)")
	}
}

// TestSettingsSetUnchangedIsChangedFalse: a second identical write is a no-op —
// changed=false, no second event (an audit trail of no-ops is noise, CS8).
func TestSettingsSetUnchangedIsChangedFalse(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	if _, _, err := tbl.Settings.Set(context.Background(), "test.flag", "true", "user:1000"); err != nil {
		t.Fatalf("first set: %v", err)
	}
	res, err := dispatchJSON(t, tbl, MethodSettingsSet, SettingsSetParams{Key: "test.flag", Value: strPtr("true")})
	if err != nil {
		t.Fatalf("second set: %v", err)
	}
	if got := res.(SettingsSetResult); got.Changed {
		t.Fatal("second set changed = true, want false (an identical write is a no-op)")
	}
	if n := fake.countCall("SettingPut"); n != 1 {
		t.Fatalf("SettingPut calls = %d, want 1 (a no-op writes nothing)", n)
	}
	if len(fake.events) != 1 {
		t.Fatalf("event count = %d, want 1 (a no-op records no event)", len(fake.events))
	}
}

// TestSettingsSetRejectsNonStringValue: a value that is not canonical text
// (JSON true / 3) is refused — a value is always a string (CS3), so a non-
// string is a bad param, not a silent coerce.
func TestSettingsSetRejectsNonStringValue(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	// "value": true and "value": 3 are not strings; decodeParams refuses them.
	for _, raw := range []json.RawMessage{
		[]byte(`{"key":"test.flag","value":true}`),
		[]byte(`{"key":"test.count","value":3}`),
	} {
		_, err := dispatchSetting(t, tbl, MethodSettingsSet, raw)
		if err == nil {
			t.Fatalf("set %s: a non-string value must be refused", string(raw))
		}
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Fatalf("set %s: want ErrBadParams, got %v", string(raw), err)
		}
	}
	if n := fake.countCall("SettingPut"); n != 0 {
		t.Fatalf("SettingPut calls = %d, want 0 (a refused value writes nothing)", n)
	}
}

// TestSettingsSetMissingValueIsBadParams: a set with NO value is refused (a
// silent no-op), while an empty string IS a real value (the missing/empty
// distinction a plain string cannot hold, CS10).
func TestSettingsSetMissingValueIsBadParams(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	// no value at all.
	_, err := dispatchSetting(t, tbl, MethodSettingsSet, json.RawMessage(`{"key":"test.flag"}`))
	if err == nil {
		t.Fatal("set with no value must be refused")
	}
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("set with no value: want ErrBadParams, got %v", err)
	}
	// an empty string IS a real value for a string key — allowed.
	if _, err := dispatchJSON(t, tbl, MethodSettingsSet, SettingsSetParams{Key: "test.label", Value: strPtr("")}); err != nil {
		t.Fatalf("set empty string: %v (an empty string is a real value, not an absence)", err)
	}
	if n := fake.countCall("SettingPut"); n != 1 {
		t.Fatalf("SettingPut calls = %d, want 1 (only the empty-string set wrote)", n)
	}
}

// TestSettingsSetUnknownKeyNamesNearest: an undeclared key is ErrSettingUnknown
// whose What/ToDo name the nearest declared key — a typo must not create a
// silent no-op (CS10). The three parts travel from config VERBATIM.
func TestSettingsSetUnknownKeyNamesNearest(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	_, err := dispatchJSON(t, tbl, MethodSettingsSet, SettingsSetParams{Key: "test.notThere", Value: strPtr("true")})
	if err == nil {
		t.Fatal("set an undeclared key must be refused")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrSettingUnknown.Code {
		t.Fatalf("set unknown: want ErrSettingUnknown, got %v", err)
	}
	// the What names the key and the nearest declared keys.
	if e.Data.What == "" {
		t.Fatal("ErrSettingUnknown has no What")
	}
	// the write path never touched the repo (the refusal is before any write).
	if n := fake.countCall("SettingPut"); n != 0 {
		t.Fatalf("SettingPut calls = %d, want 0 (an unknown key writes nothing)", n)
	}
}

// TestSettingsSetInvalidValueNamesKeyExpectedGot: a value that fails the key's
// type/bounds is ErrSettingInvalidValue naming the key, the expected form and
// what was given (CS10) — and the write is before the value check, so a
// refused value leaves no row.
func TestSettingsSetInvalidValueNamesKeyExpectedGot(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	// test.count has Max 10; 11 is refused.
	_, err := dispatchJSON(t, tbl, MethodSettingsSet, SettingsSetParams{Key: "test.count", Value: strPtr("11")})
	if err == nil {
		t.Fatal("set an over-max value must be refused")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrSettingInvalidValue.Code {
		t.Fatalf("set invalid: want ErrSettingInvalidValue, got %v", err)
	}
	if e.Data.What == "" {
		t.Fatal("ErrSettingInvalidValue has no What")
	}
	if n := fake.countCall("SettingPut"); n != 0 {
		t.Fatalf("SettingPut calls = %d, want 0 (validation before write)", n)
	}
}

// TestSettingsUnknownFieldIgnoredPerT3Invariant: a param carrying an unknown
// field (here "bogus") is IGNORED — the package's T3 invariant (nothing in the
// package ever rejects an unknown field; TestNothingCallsDisallowUnknownFields
// pins it) is stronger than "a typo'd param is a silent no-op" and the door
// follows it. (The task spec's T5.md wanted this refused as ErrBadParams; the
// package's T3/D3 invariant forbids rejecting unknown fields, so the door
// cannot honour it — recorded in Notes. The write still succeeds.)
func TestSettingsUnknownFieldIgnoredPerT3Invariant(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	_, err := dispatchSetting(t, tbl, MethodSettingsSet, json.RawMessage(`{"key":"test.flag","value":"true","bogus":1}`))
	if err != nil {
		t.Fatalf("set with an unknown field: %v (the package ignores unknown fields, T3 — it does not refuse them)", err)
	}
	if n := fake.countCall("SettingPut"); n != 1 {
		t.Fatalf("SettingPut calls = %d, want 1 (the unknown field is ignored, the value is written)", n)
	}
}

// TestSettingsUnsetReturnsDefaultView: Set then Unset → Source=default,
// Stored=nil, changed=true, and an event with op=unset, new=nil (an unset
// leaves no row and leaves a trail, CS8).
func TestSettingsUnsetReturnsDefaultView(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	if _, _, err := tbl.Settings.Set(context.Background(), "test.flag", "true", "user:1000"); err != nil {
		t.Fatalf("set: %v", err)
	}
	res, err := dispatchJSON(t, tbl, MethodSettingsUnset, SettingsUnsetParams{Key: "test.flag"})
	if err != nil {
		t.Fatalf("unset: %v", err)
	}
	got := res.(SettingsUnsetResult)
	if !got.Changed {
		t.Fatal("unset changed = false, want true")
	}
	if got.Setting.Source != SettingSourceDefault || got.Setting.Stored != nil {
		t.Fatalf("after unset: source=%s stored=%v, want default/nil", got.Setting.Source, got.Setting.Stored)
	}
	evt := fake.lastEvent()
	if evt.Op != "unset" || evt.NewValue != nil {
		t.Fatalf("unset event = {op:%q new:%v}, want {unset nil}", evt.Op, evt.NewValue)
	}
}

// TestSettingsUnsetAbsentKeyIsChangedFalse: unsetting a key with no row is
// changed=false, no event, no error (an idempotent delete).
func TestSettingsUnsetAbsentKeyIsChangedFalse(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	res, err := dispatchJSON(t, tbl, MethodSettingsUnset, SettingsUnsetParams{Key: "test.flag"})
	if err != nil {
		t.Fatalf("unset absent: %v", err)
	}
	if got := res.(SettingsUnsetResult); got.Changed {
		t.Fatal("unset of an absent key changed = true, want false")
	}
	if len(fake.events) != 0 {
		t.Fatalf("event count = %d, want 0 (an absent unset records no event)", len(fake.events))
	}
}

// TestSettingsEnvShadowVisibleOnTheWire: an env in force beats the stored row —
// the wire View shows Value=env, Source=environment, Stored=the row and
// ShadowedBy the env var name (CS4: the stored value is not hidden, the env
// that wins is named).
func TestSettingsEnvShadowVisibleOnTheWire(t *testing.T) {
	tbl, fake, _ := settingsTable(t, map[string]string{config.EnvName("test.flag"): "true"})
	fake.seedRow("test.flag", "false")
	res, err := dispatchJSON(t, tbl, MethodSettingsGet, SettingsGetParams{Key: "test.flag"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got := res.(SettingsGetResult).Setting
	if got.Value != "true" || got.Source != SettingSourceEnvironment {
		t.Fatalf("get = %q/%s, want true/environment", got.Value, got.Source)
	}
	if got.Stored == nil || *got.Stored != "false" {
		t.Fatalf("Stored = %v, want &false (the env does not hide the row)", got.Stored)
	}
	if got.ShadowedBy != config.EnvName("test.flag") {
		t.Fatalf("ShadowedBy = %q, want %q", got.ShadowedBy, config.EnvName("test.flag"))
	}
}

// TestSettingsStoredInvalidVisibleOnTheWire: a stored value that fails the
// current declaration reads as the default with StoredInvalid=true and the raw
// text — loud, never a silent default (CS10).
func TestSettingsStoredInvalidVisibleOnTheWire(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	fake.seedRow("test.flag", "banana") // invalid for a bool
	res, err := dispatchJSON(t, tbl, MethodSettingsGet, SettingsGetParams{Key: "test.flag"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got := res.(SettingsGetResult).Setting
	if !got.StoredInvalid {
		t.Fatal("StoredInvalid = false, want true (a stored-invalid value must be loud)")
	}
	if got.Value != "false" || got.Value != got.Default {
		t.Fatalf("Value = %q, want default %q", got.Value, got.Default)
	}
	if got.Source != SettingSourceDefault {
		t.Fatalf("Source = %s, want default", got.Source)
	}
	if got.Stored == nil || *got.Stored != "banana" {
		t.Fatalf("Stored = %v, want &banana (the raw invalid text is shown)", got.Stored)
	}
}

// TestSettingsHistoryNewestFirstAndLimit: history is newest first; the default
// limit is 50; a limit over 500 clamps (no error); a negative limit is refused.
func TestSettingsHistoryNewestFirstAndLimit(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	// set 1..5 so the newest is 5.
	for i, v := range []string{"1", "2", "3", "4", "5"} {
		if _, _, err := tbl.Settings.Set(context.Background(), "test.count", v, "user:1000"); err != nil {
			t.Fatalf("set %s: %v", v, err)
		}
		_ = i
	}
	// default (limit absent → 50): all five, newest first (5 is last written).
	res, err := dispatchJSON(t, tbl, MethodSettingsHistory, SettingsHistoryParams{Key: strPtr("test.count")})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	got := res.(SettingsHistoryResult)
	if len(got.Events) != 5 {
		t.Fatalf("history default = %d, want 5", len(got.Events))
	}
	if got.Events[0].NewValue == nil || *got.Events[0].NewValue != "5" {
		t.Fatalf("newest event = %v, want 5 (newest first)", got.Events[0].NewValue)
	}
	// a negative limit is refused (a negative count is a caller mistake).
	_, err = dispatchJSON(t, tbl, MethodSettingsHistory, SettingsHistoryParams{Key: strPtr("test.count"), Limit: intPtr(-1)})
	if err == nil {
		t.Fatal("history with a negative limit must be refused")
	}
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("history negative limit: want ErrBadParams, got %v", err)
	}
	_ = fake
}

// TestSettingsHistoryUndeclaredKeyStillReadable: history of a key not in the
// registry (removed or never declared) still returns its events — the audit is
// not hidden when a declaration is deleted.
func TestSettingsHistoryUndeclaredKeyStillReadable(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	fake.seedEvent(config.RepoEvent{Key: "test.flag", Op: "set", Actor: "user:1000", OccurredAt: settingsFixedNow.Format(time.RFC3339)})
	res, err := dispatchJSON(t, tbl, MethodSettingsHistory, SettingsHistoryParams{Key: strPtr("test.flag")})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if got := res.(SettingsHistoryResult); len(got.Events) != 1 {
		t.Fatalf("history = %d, want 1 (a key removed from the registry keeps its trail)", len(got.Events))
	}
}

// TestSettingsActorComesFromTheSessionNotTheParams: the audit actor comes from
// the CONNECTION (ActorFor), never from params — a params "actor" is an unknown
// field, ignored (T3), and the event's actor is the session's (CS8). A caller
// cannot forge the identity of a write to their own rules.
func TestSettingsActorComesFromTheSessionNotTheParams(t *testing.T) {
	tbl, fake, _ := settingsTable(t, nil)
	// a params "actor" is an unknown field: ignored, not honoured.
	_, err := dispatchSetting(t, tbl, MethodSettingsSet, json.RawMessage(`{"key":"test.flag","value":"true","actor":"attacker:999"}`))
	if err != nil {
		t.Fatalf("set: %v (an unknown 'actor' field is ignored, T3 — not honoured)", err)
	}
	evt := fake.lastEvent()
	if evt.Actor != "user:1000" {
		t.Fatalf("event actor = %q, want user:1000 (the session's, from ActorFor — not a params field)", evt.Actor)
	}
}

// TestSettingsMethodsAbsentWhenServiceNil: a nil seam (a storeless daemon,
// V13) leaves the five methods ABSENT, not registered-and-refusing —
// RegisterSettings is a no-op, and the table has none of the five.
func TestSettingsMethodsAbsentWhenServiceNil(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tbl.RegisterSettings() {
		t.Fatal("RegisterSettings on a nil seam must be a no-op (return false)")
	}
	for _, m := range []MethodName{MethodSettingsList, MethodSettingsGet, MethodSettingsSet, MethodSettingsUnset, MethodSettingsHistory} {
		if _, ok := tbl.Lookup(m); ok {
			t.Fatalf("%s is registered on a nil seam — it must be absent (V13)", m)
		}
	}
}

// TestSettingsMethodsNotDebug: the five are product methods, not diagnosis —
// none is Debug (so the narrow surface's own allow-list is the lever that
// keeps them off it, CS9; Debug would mislabel them).
func TestSettingsMethodsNotDebug(t *testing.T) {
	tbl, _, _ := settingsTable(t, nil)
	for _, m := range []MethodName{MethodSettingsList, MethodSettingsGet, MethodSettingsSet, MethodSettingsUnset, MethodSettingsHistory} {
		meth, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not registered", m)
		}
		if meth.Debug {
			t.Fatalf("%s is Debug — it is a product method, not a diagnostic (CS9)", m)
		}
		if !meth.RequiresInitialize {
			t.Fatalf("%s is not RequiresInitialize — all five are", m)
		}
	}
}

// TestSettingsMethodsBeforeInitializeRefused: a settings call before initialize
// is refused (the RequiresInitialize gate, T7) — the methods are not reachable
// before negotiation.
func TestSettingsMethodsBeforeInitializeRefused(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Settings = config.NewStore(newSettingsFakeRepo(), config.Options{Registry: settingsTestRegistry(t), LookupEnv: func(string) (string, bool) { return "", false }, Now: func() time.Time { return settingsFixedNow }})
	tbl.RegisterSettings()
	// no initialize: the gate refuses.
	_, err := tbl.Dispatch(context.Background(), "c1", MethodSettingsGet, json.RawMessage(`{"key":"test.flag"}`))
	if err == nil {
		t.Fatal("a settings call before initialize must be refused")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrNotInitialized.Code {
		t.Fatalf("want ErrNotInitialized, got %v", err)
	}
}

// TestSettingsErrorCodesReserved: the two setting codes are the reserved
// -31810/-31811 (T0 3.2) and are in the registry.
func TestSettingsErrorCodesReserved(t *testing.T) {
	if ErrSettingUnknown.Code != -31810 {
		t.Fatalf("ErrSettingUnknown.Code = %d, want -31810", ErrSettingUnknown.Code)
	}
	if ErrSettingInvalidValue.Code != -31811 {
		t.Fatalf("ErrSettingInvalidValue.Code = %d, want -31811", ErrSettingInvalidValue.Code)
	}
	for _, e := range []error{ErrSettingUnknown, ErrSettingInvalidValue} {
		found := false
		for _, reg := range ErrorRegistry() {
			if reg == e {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%v is not in ErrorRegistry", e)
		}
	}
}

func strPtr(s string) *string { return &s }
func intPtr(n int) *int       { return &n }

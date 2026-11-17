package daemon

// triggers_write_test.go — the writer over a REAL store: sentences carried
// verbatim from their owners (a re-typed sentence is a second truth), the
// store's actor backstop dressed in the handler's clothes, and the pin that
// the store's trigger writes have exactly one caller in the whole tree.

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

var userActor = protocol.Actor{Kind: "user", ID: "1000"}

// loopCheckBlueprintForTest asks the OWNER what its sentence is, so the
// writer's refusal is compared byte-for-byte instead of re-typed from
// memory (a re-typed sentence is a second truth waiting to drift).
func loopCheckBlueprintForTest(s *store.Store, name string) string {
	return loop.CheckBlueprint(s, name, "standard").Error()
}

func writerStore(t *testing.T) (*store.Store, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// S48 T3 (mechanical): the trigger writer validates the blueprint name
	// through loop start's door, and that door reads rows.
	seedEmbeddedRows(t, s, "two-step")
	return s, &now
}

func goodCreate() protocol.EventTriggerCreateParams {
	return protocol.EventTriggerCreateParams{
		Name: "intake", ComponentID: "",
		Match:            []byte(`{"event":"goal.approved","goal_type":["feature"]}`),
		Blueprint:        "two-step", // S39's standard pin — the name loop start itself accepts
		MaxRunsPerHour:   10,
		MaxCostPerDayUSD: nil,
	}
}

func TestCreateTriggerRefusesWithTheGrammarsOwnSentence(t *testing.T) {
	s, _ := writerStore(t)
	w := NewTriggerWrites(s)
	p := goodCreate()
	p.Match = []byte(`{"event":"goal.approved","labels":["urgent"]}`)
	_, err := w.CreateEventTrigger(userActor, p)
	// Byte-for-byte identity with the grammar's OWN sentence — computed by
	// asking the owner, not by re-typing what I think it says.
	_, wantErr := store.ParseMatch(p.Match)
	if wantErr == nil {
		t.Fatal("ParseMatch accepted what the writer refused — the doors disagree")
	}
	if err == nil || err.Error() != wantErr.Error() {
		t.Fatalf("grammar refusal:\n got %v\nwant %v", err, wantErr)
	}
	trs, _ := s.ListEventTriggers(store.EventTriggerFilter{})
	if len(trs) != 0 {
		t.Fatalf("%d triggers stored behind a refused create", len(trs))
	}
}

func TestCreateTriggerRefusesAnUnknownOrInvalidBlueprintWithItsOwnSentence(t *testing.T) {
	s, _ := writerStore(t)
	w := NewTriggerWrites(s)
	for _, name := range []string{"no-such-blueprint", "/no/such/blueprint.yaml"} {
		p := goodCreate()
		p.Blueprint = name
		_, err := w.CreateEventTrigger(userActor, p)
		want := loopCheckBlueprintForTest(s, name)
		if err == nil || err.Error() != want {
			t.Fatalf("blueprint %q refusal:\n got %v\nwant %v", name, err, want)
		}
	}
	// A blueprint that parses as YAML but is not a blueprint at all.
	path := filepath.Join(t.TempDir(), "not-a-blueprint.yaml")
	if err := os.WriteFile(path, []byte("schemaVersion: 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := goodCreate()
	p.Blueprint = path
	if _, err := w.CreateEventTrigger(userActor, p); err == nil || !strings.Contains(err.Error(), "invalid-blueprint") {
		t.Fatalf("schemaVersion:9 accepted or mis-spoken: %v", err)
	}
	trs, _ := s.ListEventTriggers(store.EventTriggerFilter{})
	if len(trs) != 0 {
		t.Fatalf("%d triggers stored behind refused blueprints", len(trs))
	}
}

func TestCreateTriggerRefusesAnUnknownScopeAndABadKickoffTemplate(t *testing.T) {
	s, _ := writerStore(t)
	w := NewTriggerWrites(s)
	p := goodCreate()
	p.ComponentID = "cmp-nope"
	if _, err := w.CreateEventTrigger(userActor, p); err == nil || err.Error() != `no component "cmp-nope"` {
		t.Fatalf("ghost component: %v — want the naming sentence", err)
	}
	p = goodCreate()
	p.RootID = "prj-nope"
	if _, err := w.CreateEventTrigger(userActor, p); err == nil || err.Error() != `no component "prj-nope"` {
		t.Fatalf("ghost root: %v", err)
	}
	// Recorded plan correction: the task file's `{{.Nope}}` does NOT refuse
	// — S39's ParseKickoff parses only (missingkey=zero makes an absent
	// field render blank). What refuses is malformed SYNTAX, which is the
	// honest "a kickoff that can never render" this door is for.
	p = goodCreate()
	p.Kickoff = "Approved: {{.Goal"
	if _, err := w.CreateEventTrigger(userActor, p); err == nil || !strings.Contains(err.Error(), "kickoff template does not parse") {
		t.Fatalf("broken template accepted or mis-spoken: %v", err)
	}
	trs, _ := s.ListEventTriggers(store.EventTriggerFilter{})
	if len(trs) != 0 {
		t.Fatalf("%d triggers behind refused scopes/templates", len(trs))
	}
}

func TestCreateThenShowThenDisableThenEnableRoundTrips(t *testing.T) {
	s, now := writerStore(t)
	w := NewTriggerWrites(s)
	r := NewTriggerReads(s)
	tr, err := w.CreateEventTrigger(userActor, goodCreate())
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Enabled || tr.CreatedByKind != "user" || tr.CreatedByID != "1000" || tr.LastFire != nil {
		t.Fatalf("fresh view %+v — born enabled, authored by the person, no fires yet", tr)
	}
	shown, found, err := r.ShowTrigger(tr.ID)
	if err != nil || !found || shown.Name != "intake" || shown.MaxRunsPerHour != 10 {
		t.Fatalf("show after create: found=%v err=%v view=%+v", found, err, shown)
	}
	created := shown.UpdatedAt

	*now = now.Add(30 * time.Minute)
	off, err := w.DisableEventTrigger(userActor, tr.ID, "operator paused it")
	if err != nil || off.Enabled || off.DisabledReason != "operator paused it" {
		t.Fatalf("disable: %+v err=%v", off, err)
	}
	if off.UpdatedAt == created {
		t.Errorf("updatedAt did not move on disable — the desk sorts on it")
	}

	*now = now.Add(30 * time.Minute)
	on, err := w.EnableEventTrigger(userActor, tr.ID)
	if err != nil || !on.Enabled || on.DisabledReason != "" {
		t.Fatalf("enable clears the sentence: %+v err=%v", on, err)
	}
	if on.UpdatedAt == off.UpdatedAt {
		t.Errorf("updatedAt did not move on enable")
	}
}

func TestUpdateKeepsAuthorshipAndCannotMoveScope(t *testing.T) {
	s, now := writerStore(t)
	w := NewTriggerWrites(s)
	rootID, compID := seedTriggerWorld(t, s)
	p := goodCreate()
	p.RootID, p.ComponentID = rootID, compID
	tr, err := w.CreateEventTrigger(userActor, p)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Hour)
	nm := "renamed"
	lim := 25
	updated, err := w.UpdateEventTrigger(userActor, protocol.EventTriggerUpdateParams{ID: tr.ID, Name: &nm, MaxRunsPerHour: &lim})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "renamed" || updated.MaxRunsPerHour != 25 {
		t.Fatalf("edit did not land: %+v", updated)
	}
	if updated.CreatedByKind != "user" || updated.CreatedByID != "1000" {
		t.Errorf("authorship moved: %s/%s — the row remembers who stood behind its birth", updated.CreatedByKind, updated.CreatedByID)
	}
	if updated.RootID != rootID || updated.ComponentID != compID {
		t.Errorf("scope moved: %s/%s", updated.RootID, updated.ComponentID)
	}
	// Compile-time in Go, asserted here for the record: the params type has
	// no scope fields, so a scope-carrying edit request cannot be expressed.
	tp := reflect.TypeOf(protocol.EventTriggerUpdateParams{})
	for i := range tp.NumField() {
		tag := strings.Split(tp.Field(i).Tag.Get("json"), ",")[0]
		if tag == "rootId" || tag == "componentId" {
			t.Errorf("EventTriggerUpdateParams gained field %q — scope is not editable; to move a trigger, create another", tag)
		}
	}
	// ClearMaxCost is its own intention; absent kept the cap.
	cap2 := 5.0
	withCap, err := w.UpdateEventTrigger(userActor, protocol.EventTriggerUpdateParams{ID: tr.ID, MaxCostPerDayUSD: &cap2})
	if err != nil || withCap.MaxCostPerDayUSD == nil || *withCap.MaxCostPerDayUSD != 5.0 {
		t.Fatalf("setting the cap: %+v err=%v", withCap, err)
	}
	kept, err := w.UpdateEventTrigger(userActor, protocol.EventTriggerUpdateParams{ID: tr.ID})
	if err != nil || kept.MaxCostPerDayUSD == nil {
		t.Fatalf("an edit without the cap field must KEEP it: %+v err=%v", kept, err)
	}
	cleared, err := w.UpdateEventTrigger(userActor, protocol.EventTriggerUpdateParams{ID: tr.ID, ClearMaxCost: true})
	if err != nil || cleared.MaxCostPerDayUSD != nil {
		t.Fatalf("clearMaxCost must CLEAR it: %+v err=%v", cleared, err)
	}
}

func TestTheStoreStillRefusesAnAgentActorIfTheHandlerRegresses(t *testing.T) {
	s, _ := writerStore(t)
	w := NewTriggerWrites(s)
	// Straight to the seam, bypassing every handler check: the store's
	// requireUser is the last door, and the writer dresses its refusal in
	// the SAME sentence the handler speaks — one crime, one sentence.
	_, err := w.CreateEventTrigger(protocol.Actor{Kind: "agent", ID: "brainstormer"}, goodCreate())
	if err == nil || !strings.Contains(err.Error(), "written by a person") || !strings.Contains(err.Error(), `"agent"`) {
		t.Fatalf("backstop refusal: %v — want the person sentence naming the kind", err)
	}
	_, err = w.CreateEventTrigger(protocol.Actor{Kind: "user", ID: ""}, goodCreate())
	if err == nil || !strings.Contains(err.Error(), "written by a person") {
		t.Fatalf("empty-id actor through the backstop: %v", err)
	}
	trs, _ := s.ListEventTriggers(store.EventTriggerFilter{})
	if len(trs) != 0 {
		t.Fatalf("%d triggers behind refused actors", len(trs))
	}
}

func TestOnlyTheDaemonWriterCallsTheStoreTriggerWrites(t *testing.T) {
	// The store's trigger writes have ONE caller in all of internal/: the
	// daemon's seam. A second caller is a second door nobody audited. The
	// walk starts at ".." because a go test runs inside its own package
	// directory — from internal/daemon, ".." IS internal/.
	calls := regexp.MustCompile(`\bs\.(CreateEventTrigger|UpdateEventTrigger|SetEventTriggerEnabled)\(`)
	seen := 0
	var offenders []string
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		// The CT census is a characterization builder — it pins the tree's
		// golden rows, not a production door, so it may call the store's
		// trigger writes. The one-PRODUCTION-caller guarantee it proves is
		// daemon/triggers.go; the census is excluded by name.
		if strings.Contains(filepath.ToSlash(path), "/ctcensus/") {
			return nil
		}
		seen++
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// S49 T4: the writer moved whole into daemon/automation_write.go
		// (the file's 143-line block rides with it); the rule — one
		// caller file in the daemon — survives the rename, and T5 re-pins
		// the set on the new plane.
		switch filepath.ToSlash(path) {
		case "../daemon/triggers.go", "../daemon/automation_write.go":
		default:
			if calls.Find(b) != nil {
				offenders = append(offenders, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 20 {
		t.Fatalf("the scan read %d files — a grep that reads nothing passes forever", seen)
	}
	if len(offenders) != 0 {
		t.Fatalf("second callers of the store's trigger writes: %v — only daemon/automation_write.go (T4) and the retired triggers.go shim may call them", offenders)
	}
}

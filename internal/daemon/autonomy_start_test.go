package daemon

// autonomy_start_test.go — S56 T4: the start path's one consultation of the
// root's grants, at pin time. Each behaviour in T4's table is a test. The
// store owns the SQL (T3); these prove the daemon bridges it correctly:
// schedule/webhook/event consult resolveAutonomy once, a no-grant start writes
// no pin row and is byte-identical to today, a readable failure refuses, and
// the legacy attended/outward flags never flip because a pin exists.

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// autonomyRootEnv opens a store and registers a workspace as the root of its
// own component tree, returning the store, a daemon, the workspace and the root
// component. The workspace is a real directory (validateLoopWorkspace's gate)
// and a registered component root (ComponentForRoot's gate), so resolveAutonomy
// finds a root to read grants from.
func autonomyRootEnv(t *testing.T) (*store.Store, *Daemon, string, store.Component) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ws := t.TempDir()
	cw, err := s.CreateComponent(store.NewComponent{Name: "autonomy root", Slug: "autonomy-root", Root: ws})
	if err != nil || !cw.OK() {
		t.Fatalf("create root component: %v %s", err, cw.Reason)
	}
	d := New(Options{Store: s})
	return s, d, ws, cw.Component
}

// seedAutonomyGrant writes one grant through the store's door (AL11 lock 3). A
// refusal is a test failure, never a silent no-op.
func seedAutonomyGrant(t *testing.T, s *store.Store, nodeID string, in store.NewAutonomyGrant) store.AutonomyGrant {
	t.Helper()
	in.NodeID = nodeID
	g, ref, err := s.CreateAutonomyGrant(store.Actor{Kind: "user", ID: "u1"}, in)
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	if ref != nil {
		t.Fatalf("grant refused: %s", ref.Sentence)
	}
	return g
}

// fireSchedule starts a loop through the schedule origin (the clock's fire) and
// waits for it to settle before the store cleanup can close the handle — the
// runner must not outlive the store (the panic the fire family has always been).
func fireSchedule(t *testing.T, s *store.Store, d *Daemon, id, ws string) string {
	t.Helper()
	instant := time.Date(2026, 11, 1, 3, 30, 0, 0, time.UTC)
	loopID, err := d.StartFromTrigger(context.Background(), trigger.Fire{
		TriggerID: id, Blueprint: "two-step", Workspace: ws,
		Provenance: trigger.ProvenanceTrigger, Instant: instant, At: instant,
	})
	if err != nil {
		t.Fatalf("schedule fire %q: %v", id, err)
	}
	awaitSettled(t, s, loopID)
	return loopID
}

// TestPolicyPinnedLoopsKeepAllowOutwardZero is AL8's structural guard: a policy
// pin is an ADDITION to the loop, never a widening of the legacy flags. A
// schedule/webhook/event loop with a grant keeps attended=false and
// allow_outward=false — the two columns the pin must never move.
func TestPolicyPinnedLoopsKeepAllowOutwardZero(t *testing.T) {
	s, d, ws, root := autonomyRootEnv(t)
	seedAutonomyGrant(t, s, root.ID, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow", Conditions: `{}`,
	})
	for _, tc := range []struct {
		what    string
		provenance string
	}{
		{"schedule", trigger.ProvenanceTrigger},
		{"webhook", trigger.ProvenanceWebhook},
	} {
		loopID, err := d.StartFromTrigger(context.Background(), trigger.Fire{
			TriggerID: "pin-" + tc.what, Blueprint: "two-step", Workspace: ws,
			Provenance: tc.provenance, Instant: time.Now(), At: time.Now(),
		})
		if err != nil {
			t.Fatalf("%s fire: %v", tc.what, err)
		}
		awaitSettled(t, s, loopID)
		att, err := s.LoopAttended(context.Background(), loopID)
		if err != nil {
			t.Fatalf("%s: attended read: %v", tc.what, err)
		}
		if att == nil || *att {
			t.Errorf("%s: a pinned loop flipped attended to %v — the legacy flag must stay a declared false (AL8)", tc.what, att)
		}
		out, err := s.LoopAllowOutward(context.Background(), loopID)
		if err != nil {
			t.Fatalf("%s: allow_outward read: %v", tc.what, err)
		}
		if out {
			t.Errorf("%s: a pinned loop flipped allow_outward — a pin is not the outward lever (AL8)", tc.what)
		}
	}
}

// TestNoGrantsMeansNoPinRowAndIdenticalLoop is the byte-identity promise: a root
// with no grant writes NO pin row, and the loop is exactly today's shape —
// started_by/trigger_id as authored, attended a declared false, allow_outward
// false. The autonomy consultation is invisible when nothing applies.
func TestNoGrantsMeansNoPinRowAndIdenticalLoop(t *testing.T) {
	s, d, ws, _ := autonomyRootEnv(t)
	loopID := fireSchedule(t, s, d, "no-grant", ws)
	_, ok, err := s.ReadAutonomyPin(loopID)
	if err != nil {
		t.Fatalf("read pin: %v", err)
	}
	if ok {
		t.Fatalf("a no-grant start wrote a pin row — byte-identity broken")
	}
	startedBy, triggerID, err := s.LoopStartedBy(context.Background(), loopID)
	if err != nil {
		t.Fatalf("provenance read: %v", err)
	}
	if startedBy != "trigger" || triggerID != "no-grant" {
		t.Errorf("a no-grant start moved provenance to %q/%q — want trigger/no-grant (byte-identical)", startedBy, triggerID)
	}
	att, err := s.LoopAttended(context.Background(), loopID)
	if err != nil || att == nil || *att {
		t.Errorf("a no-grant start attended = %v (err %v) — want a declared false", att, err)
	}
	out, err := s.LoopAllowOutward(context.Background(), loopID)
	if err != nil || out {
		t.Errorf("a no-grant start allow_outward = %v (err %v) — want false", out, err)
	}
}

// TestPinSnapshotIsSelfDescribing is behaviour 3: a grant on the outward CLASS
// is expanded at pin time to the current outward action names, so the snapshot
// names what it covers without a registry at read time; and the rev is the
// root's AutonomyRev.
func TestPinSnapshotIsSelfDescribing(t *testing.T) {
	s, d, ws, root := autonomyRootEnv(t)
	seedAutonomyGrant(t, s, root.ID, store.NewAutonomyGrant{
		SubjectKind: "class", Subject: "outward", State: "allow", Conditions: `{}`,
	})
	loopID := fireSchedule(t, s, d, "self-desc", ws)
	pin, ok, err := s.ReadAutonomyPin(loopID)
	if err != nil {
		t.Fatalf("read pin: %v", err)
	}
	if !ok {
		t.Fatalf("an outward-class grant pinned nothing — the snapshot is absent")
	}
	if pin.Origin != "schedule" {
		t.Errorf("pin origin = %q, want schedule", pin.Origin)
	}
	rev, err := s.AutonomyRev(root.ID)
	if err != nil {
		t.Fatalf("rev read: %v", err)
	}
	if pin.Rev != rev {
		t.Errorf("pin rev = %d, want the root's AutonomyRev %d", pin.Rev, rev)
	}
	// The snapshot must name the expanded outward actions, not the class word.
	if strings.Contains(pin.Allowed, `"outward"`) {
		t.Errorf("the snapshot carries the bare class word, not the expanded names: %s", pin.Allowed)
	}
	if len(pin.Allowed) == 0 || pin.Allowed == "[]" {
		t.Errorf("the outward grant pinned an empty snapshot: %s", pin.Allowed)
	}
}

// TestLabelledGrantNeedsMatchingLabels is behaviour 4: a grant whose labels do
// not intersect the start's labels is absent from allowed. All three origins
// today supply NO labels, so a labelled grant never applies — and pins nothing.
func TestLabelledGrantNeedsMatchingLabels(t *testing.T) {
	s, d, ws, root := autonomyRootEnv(t)
	seedAutonomyGrant(t, s, root.ID, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow",
		Conditions: `{"labels":["nightly"]}`,
	})
	loopID := fireSchedule(t, s, d, "labelled", ws)
	_, ok, err := s.ReadAutonomyPin(loopID)
	if err != nil {
		t.Fatalf("read pin: %v", err)
	}
	if ok {
		t.Fatalf("a labelled grant applied to a label-less start — the label gate bypassed")
	}
}

// TestUnreadablePolicyRefusesTheStart is behaviour 5's hard half: a start that
// cannot READ the policy refuses (fail-toward-deny, the next tick retries),
// never falling through to an unpinned loop. The failure is induced the way the
// code itself frames it (grantsToAutonomy's own comment — "an unparseable value
// is the store could not be read, never a no-value"): a grant whose conditions
// are valid JSON (the store's CHECK is only json_valid, so the door writes it)
// but cannot be unmarshalled into the typed set (labels is a string, not a
// []string). The bridge cannot read the policy it was handed, so the start
// answers with the typed sentence and refuses BEFORE the start door — no loop
// row, no half-write. The store stays open (closing it nils the reader and
// panics, not a typed refusal), so a later read can confirm the loop never
// existed.
func TestUnreadablePolicyRefusesTheStart(t *testing.T) {
	s, d, ws, root := autonomyRootEnv(t)
	// A grant whose conditions pass the json_valid CHECK but cannot unmarshal
	// into the typed Conditions (labels is a string, not a []string): the store
	// holds the row, the bridge cannot read it.
	seedAutonomyGrant(t, s, root.ID, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow",
		Conditions: `{"labels":"not-an-array"}`,
	})
	_, err := d.StartFromTrigger(context.Background(), trigger.Fire{
		TriggerID: "unreadable", Blueprint: "two-step", Workspace: ws,
		Provenance: trigger.ProvenanceTrigger, Instant: time.Now(), At: time.Now(),
	})
	if err == nil {
		t.Fatal("an unreadable policy started a loop — the start must refuse, not fall through")
	}
	if !strings.Contains(err.Error(), "the autonomy policy") || !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("the refusal is not the typed sentence: %v", err)
	}
	// The refusal is before the start door, so the loop never existed: a read
	// over the still-open store finds no loop for this workspace (no half-write).
	loops, err := s.ListLoops(store.ComponentScope{ComponentRoot: ws})
	if err != nil {
		t.Fatalf("list loops: %v", err)
	}
	if len(loops) != 0 {
		t.Errorf("the unreadable policy started %d loop(s) — it must refuse before the start door, not fall through", len(loops))
	}
}

// TestOnlyOriginSitesCallResolveAutonomy is behaviour 9: the one resolution
// function appears only at the origin sites (the schedule/webhook fire, the
// event fire, and this test's canary), never in a person or narrow path. A
// person door resolving a grant would be a second truth; the grep is the lock.
func TestOnlyOriginSitesCallResolveAutonomy(t *testing.T) {
	for _, f := range []string{"autonomy_start.go", "trigger_fire.go", "event_fire.go", "autonomy_start_test.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if !strings.Contains(string(src), "resolveAutonomy") {
			t.Errorf("%s is expected to reference resolveAutonomy and does not", f)
		}
	}
	// The canary: the person door (StartBlueprintLoop's closure) and the
	// narrow surfaces must NOT consult the policy.
	for _, f := range []string{"blueprint.go", "queue_reactor.go", "roadmap_dispatch.go", "triage_dispatch.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Contains(string(src), "resolveAutonomy") {
			t.Errorf("%s consults resolveAutonomy — a person/narrow path must not (AL8)", f)
		}
	}
}

// TestStartParamsCarryNoAuthority is T4's Do-NT made a test: no wire type may
// carry merge authority. reflect over protocol.LoopStartParams: no field whose
// name contains grant, merge, allowMerge or autonomy — the only authority
// fields the wire carries are Attended and AllowOutward (the legacy pair).
func TestStartParamsCarryNoAuthority(t *testing.T) {
	tt := reflect.TypeOf(protocol.LoopStartParams{})
	if tt.Kind() != reflect.Struct {
		t.Fatalf("protocol.LoopStartParams is %s, want a struct", tt.Kind())
	}
	forbidden := []string{"grant", "merge", "allowmerge", "autonomy"}
	for i := 0; i < tt.NumField(); i++ {
		f := tt.Field(i)
		name := strings.ToLower(f.Name)
		for _, f2 := range forbidden {
			if strings.Contains(name, f2) {
				t.Errorf("protocol.LoopStartParams.%s is a wire route to merge authority — refuse it (T4 Do-NT)", f.Name)
			}
		}
	}
	// The only two authority fields the wire carries are the legacy pair.
	var got []string
	for i := 0; i < tt.NumField(); i++ {
		n := tt.Field(i).Name
		if n == "Attended" || n == "AllowOutward" {
			got = append(got, n)
		}
	}
	if strings.Join(got, ",") != "Attended,AllowOutward" {
		// The order is the struct's; assert the set is exactly the legacy pair.
		if len(got) != 2 || (got[0] != "Attended" && got[0] != "AllowOutward") {
			t.Errorf("the wire's authority fields = %v, want exactly Attended and AllowOutward", got)
		}
	}
}

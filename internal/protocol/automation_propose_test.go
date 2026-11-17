package protocol

// automation_propose_test.go — S49 T7 (AN9): the SHAPE of the automation
// plane's one agent door, beside environment_propose_test.go's and the
// blueprint/policy/autonomy twins'. What lives here is the DOOR: registered
// iff the records service stands (V13), gated and non-debug like every write
// on this table, and dispatching through the SAME RecordsAPI.Do the tool door
// calls (one implementation, two doors — a handler here that answered for
// itself would be the second door AN9 forbids). What the refusal sentence is
// — already-off, archived, unknown — is the service's word: this file only
// pins that the door carries it to the caller whole (ErrBadParams, named).
//
// TestRecordsParityAutomationFamily is the two-direction walk T7 names:
// every automation spec's dot-name twin stands, no spec is twinned to any of
// the plane's eleven writes, and exactly one spec mutates. It sits here
// because the method table is what a parity walk needs.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/records"
)

// TestAutomationProposeDisableRegistrationIsConditional is the V13 half: no
// records service, no method — ABSENT (ErrUnknownMethod), never
// registered-and-refusing. The records service is what honours what makes the
// door safe (the stamped agent actor, the shared create validator, the
// per-agent open-proposal cap), so an unwired runtime does not advertise it.
// The four READS are not this test's subject: RegisterAutomationReads owns
// them over the plane's own seam, and registerRecords skips the whole family
// so no door is ever projected twice.
func TestAutomationProposeDisableRegistrationIsConditional(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tbl.RegisterAutomationProposeDisable() {
		t.Fatal("RegisterAutomationProposeDisable registered over a nil records seam — V13 says absent, not refusing")
	}
	if _, ok := tbl.Lookup(MethodAutomationProposeDisable); ok {
		t.Error("automation.proposeDisable stands without the records service — the mutating door was registered-and-refusing")
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodAutomationProposeDisable, json.RawMessage(`{}`)); !errors.As(err, new(ErrUnknownMethod)) {
		t.Errorf("dispatch without the seam = %v, want ErrUnknownMethod", err)
	}

	tbl.Records = &envProposeSpy{recordNameSpy: &recordNameSpy{}}
	if !tbl.RegisterAutomationProposeDisable() {
		t.Fatal("RegisterAutomationProposeDisable refused a wired seam")
	}
	got, ok := tbl.Lookup(MethodAutomationProposeDisable)
	if !ok {
		t.Fatal("automation.proposeDisable did not register with the service wired")
	}
	if !got.RequiresInitialize {
		t.Error("automation.proposeDisable is not RequiresInitialize — a mutating catalog action before the handshake is unthinkable (PM7)")
	}
	if got.Debug {
		t.Error("automation.proposeDisable is Debug — product surface (PM10)")
	}
	// Idempotent: a second call is a no-op, not a second door (t.register
	// panics on a duplicate, so this is a crash test as much as a shape one).
	if !tbl.RegisterAutomationProposeDisable() {
		t.Error("RegisterAutomationProposeDisable is not idempotent over the same wired seam")
	}
}

// TestAutomationProposeDisableDispatchesThroughDo pins the ONE implementation:
// the socket twin dispatches into RecordsAPI.Do with the catalog tool name
// and the caller's params untouched, so the socket door and the tool door
// answer through the same function.
func TestAutomationProposeDisableDispatchesThroughDo(t *testing.T) {
	spy := &envProposeSpy{recordNameSpy: &recordNameSpy{}}
	tbl := newTestTable(t)
	tbl.Records = spy
	if !tbl.RegisterAutomationProposeDisable() {
		t.Fatal("RegisterAutomationProposeDisable refused")
	}
	init := mustJSON(t, InitializeParams{ClientName: "t", ClientVersion: "0", ProtocolVersion: PROTOCOL_VERSION})
	if _, err := tbl.Dispatch(context.Background(), "spy", MethodInitialize, init); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	body := `{"source":"schedule","id":"nightly-index","reason":"the embedder is out of quota"}`
	if _, err := tbl.Dispatch(context.Background(), "spy", MethodAutomationProposeDisable, json.RawMessage(body)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if spy.last != "automation_propose_disable" {
		t.Errorf("Do saw tool %q, want the catalog name automation_propose_disable — the socket door went around the catalog", spy.last)
	}
	if string(spy.params) != body {
		t.Errorf("Do received %s, want the caller's params verbatim (%s) — a door that re-writes the ask before the service sees it is a second authority", spy.params, body)
	}
}

// TestAutomationProposeDisableCarriesTheRefusalSentenceWhole is the arm→door
// contract: the service's sentence (in the real mount, the shared validator's
// "already off" or "is archived") reaches the caller as a refusal that quotes
// it, not a generic error that drops the reason.
func TestAutomationProposeDisableCarriesTheRefusalSentenceWhole(t *testing.T) {
	spy := &envProposeSpy{recordNameSpy: &recordNameSpy{}, sentence: "refused: schedule/sch-x is already off — nothing to disable (S49 T6, AN9)"}
	tbl := newTestTable(t)
	tbl.Records = spy
	if !tbl.RegisterAutomationProposeDisable() {
		t.Fatal("RegisterAutomationProposeDisable refused")
	}
	init := mustJSON(t, InitializeParams{ClientName: "t", ClientVersion: "0", ProtocolVersion: PROTOCOL_VERSION})
	if _, err := tbl.Dispatch(context.Background(), "spy", MethodInitialize, init); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	_, err := tbl.Dispatch(context.Background(), "spy", MethodAutomationProposeDisable, json.RawMessage(`{"source":"schedule","id":"sch-x","reason":"r"}`))
	if err == nil {
		t.Fatal("the door swallowed the service's refusal sentence")
	}
	if !strings.Contains(err.Error(), "already off") {
		t.Errorf("the refusal %q does not carry the sentence the service spoke", err)
	}
}

// TestRecordsParityAutomationFamily is T7's two-direction walk over the
// automation family. Forward — every automation Spec's dot-name twin stands
// (the four reads are T3's standing methods, the ask stands through
// RegisterAutomationProposeDisable). Backward — no spec is twinned to ANY of
// the plane's eleven writes (the clock family's add/update/pause/resume/
// fire-now, the event family's create/update/enable/disable, the cross-
// family archive pair): an agent may PROPOSE a disable, never switch, edit,
// archive or fire (AN9); and exactly one spec mutates.
func TestRecordsParityAutomationFamily(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Records = recordsListingStub{}
	tbl.AutomationReads = automationReadsStub{}
	if !tbl.RegisterAutomationReads() {
		t.Fatal("RegisterAutomationReads refused a wired seam")
	}
	if !tbl.RegisterAutomationProposeDisable() {
		t.Fatal("RegisterAutomationProposeDisable refused a wired records seam")
	}
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	forbiddenTwins := map[string]bool{
		// The constants, never dotted literals: the eleven writes named as
		// one sanctioned list (a spec twinned to any of them is an agent
		// door onto a widening — AN9's lock at the catalog level).
		string(MethodTriggerAdd):                 true,
		string(MethodTriggerUpdate):              true,
		string(MethodTriggerPause):               true,
		string(MethodTriggerResume):              true,
		string(MethodTriggerFireNow):             true,
		string(MethodAutomationTriggerCreate):    true,
		string(MethodAutomationTriggerUpdate):    true,
		string(MethodAutomationTriggerEnable):    true,
		string(MethodAutomationTriggerDisable):   true,
		string(MethodAutomationArchive):          true,
		string(MethodAutomationUnarchive):        true,
		string(MethodAutomationRuns):             true, // reads the tool plane did not take (rule 2's exact five)
		string(MethodAutomationVocabulary):       true,
		string(MethodAutomationPreview):          true,
		string(MethodAutomationTest):             true,
		string(MethodAutomationDryRun):           true,
		string(MethodAutomationChanges):          true,
	}
	mutators := 0
	seen := map[string]bool{}
	for _, sp := range records.Catalog() {
		if sp.Family != "automation" {
			continue
		}
		seen[sp.Name] = true
		if !registered[MethodName(sp.Method)] {
			t.Errorf("%s names twin %q, which the table does not register — the two doors are drifting", sp.Name, sp.Method)
		}
		if forbiddenTwins[sp.Method] {
			t.Errorf("%s is twinned to %q — no automation write, and no read outside the five the plane took, reaches an agent through the catalog (AN9)", sp.Name, sp.Method)
		}
		if sp.Mutates {
			mutators++
			if sp.Method != string(MethodAutomationProposeDisable) {
				t.Errorf("%s mutates and is not the propose twin — an agent may file a disable ask, never switch, edit, archive or fire", sp.Name)
			}
		}
	}
	if mutators != 1 {
		t.Errorf("%d mutating automation specs, want exactly 1 (the ask)", mutators)
	}
	if len(seen) != 5 {
		t.Errorf("the automation family holds %d specs (%v), want exactly 5 — four reads and one ask", len(seen), seen)
	}
	// The reverse half: the plane's one agent door stands, and it stands
	// because a spec points at it. A registered method with no spec is a door
	// the model cannot see; a spec with no method is a promise nobody answers.
	if !registered[MethodAutomationProposeDisable] {
		t.Error("automation.proposeDisable is not registered over a wired records seam — the ask's own tool would have nothing to call")
	}
	for _, m := range []MethodName{MethodAutomationList, MethodAutomationShow, MethodAutomationFires, MethodAutomationHealth} {
		if !registered[m] {
			t.Errorf("%s is not registered — a read twin of the person's standing door is missing, so the tool and the door are no longer one implementation", m)
		}
	}
}

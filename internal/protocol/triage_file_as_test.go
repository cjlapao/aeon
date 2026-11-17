package protocol

// triage_file_as_test.go — S51 T5 (TM6, rule 4, V13): the triage plane's
// ONE agent filing door. The roadmap-propose family's three shapes,
// carried: registration is conditional on the records seam (absent, never
// registered-and-refusing); dispatch is a pass-through to the SAME
// RecordsAPI.Do the tool door calls, bytes untouched and the refusal
// sentence verbatim; and the catalog's bug family cannot grow a second
// write, let alone one pointed at a person's door.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/cjlapao/aeon/internal/records"
)

func TestTriageFileAsRegistrationIsConditional(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tbl.RegisterTriageFileAs() {
		t.Fatal("RegisterTriageFileAs registered over a nil records seam — V13 says absent, not refusing")
	}
	if _, ok := tbl.Lookup(MethodTriageFileAs); ok {
		t.Error("triage.fileAs stands without the records service — the filing door was registered-and-refusing")
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodTriageFileAs, json.RawMessage(`{}`)); !errors.As(err, new(ErrUnknownMethod)) {
		t.Errorf("dispatch without the seam = %v, want ErrUnknownMethod", err)
	}

	tbl.Records = &envProposeSpy{recordNameSpy: &recordNameSpy{}}
	if !tbl.RegisterTriageFileAs() {
		t.Fatal("RegisterTriageFileAs refused a wired seam")
	}
	got, ok := tbl.Lookup(MethodTriageFileAs)
	if !ok {
		t.Fatal("triage.fileAs did not register with the service wired")
	}
	if !got.RequiresInitialize {
		t.Error("triage.fileAs is not RequiresInitialize — a mutating catalog action before the handshake is unthinkable (PM7)")
	}
	if got.Debug {
		t.Error("triage.fileAs is Debug — product surface (PM10)")
	}
	// Idempotent: a second call is a no-op, not a second door.
	if !tbl.RegisterTriageFileAs() {
		t.Error("RegisterTriageFileAs is not idempotent over the same wired seam")
	}
}

func TestTriageFileAsDispatchesThroughDo(t *testing.T) {
	spy := &envProposeSpy{recordNameSpy: &recordNameSpy{}}
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.Records = spy
	if !tbl.RegisterTriageFileAs() {
		t.Fatal("RegisterTriageFileAs refused a wired records seam")
	}
	params := json.RawMessage(`{"componentRoot":"/srv/x","title":"t","body":"b"}`)
	// The handshake is RequiresInitialize's gate; run it so dispatch is honest.
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodInitialize, json.RawMessage(fmt.Sprintf(`{"protocolVersion":%d,"clientInfo":{"name":"test","version":"0"}}`, PROTOCOL_VERSION))); err != nil {
		t.Fatal(err)
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodTriageFileAs, params); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if spy.last != "bug_file" {
		t.Errorf("Do saw tool %q, want bug_file — the catalog name is the vocabulary", spy.last)
	}
	if string(spy.params) != string(params) {
		t.Errorf("params were rewritten in transit: %s, want %s (rule 4 — Do is one implementation, two doors)", spy.params, params)
	}
	// The refusal sentence surfaces VERBATIM as ErrBadParams, not laundered.
	spy.sentence = "refused: filing a bug claim is a person's own act — triage.file is the person's door; use it, and the claim is filed under you, not under a step (TM6)"
	_, err := tbl.Dispatch(context.Background(), "c1", MethodTriageFileAs, params)
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("a refusal sentence came back as %v (%T), want ErrBadParams", err, err)
	}
	if !errors.Is(err, bad) || !contains(err.Error(), spy.sentence) {
		t.Errorf("the sentence was laundered: %v", err)
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (hay == needle || indexOf(hay, needle) >= 0)
}
func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// TestBugFamilyHasNoPersonDoorTwin is TM6 in Go: the bug family carries
// EXACTLY three specs — two reads and the one filing — no spec points at
// a person's state door (the five writes) or at triage.file itself (the
// person's filing; its agent twin is the SEPARATE triage.fileAs, which is
// what keeps triage.file's humanDoorOnly classification true), and at most
// one spec mutates.
func TestBugFamilyHasNoPersonDoorTwin(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Records = &envProposeSpy{recordNameSpy: &recordNameSpy{}}
	if !tbl.RegisterTriageFileAs() {
		t.Fatal("RegisterTriageFileAs refused a wired records seam")
	}
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	personDoors := map[MethodName]bool{
		MethodTriageFile: true, MethodTriageUpdate: true, MethodTriageSetPriority: true,
		MethodTriageConfirmDuplicate: true, MethodTriageValidate: true, MethodTriageHandoff: true,
	}
	mutators, seen := 0, 0
	for _, sp := range records.Catalog() {
		if sp.Family != "bug" {
			continue
		}
		seen++
		m := MethodName(sp.Method)
		if !registered[m] {
			t.Errorf("%s twinned to %s, which is not registered — a tool the app cannot reach", sp.Name, sp.Method)
		}
		if personDoors[m] {
			t.Errorf("%s twinned to %s — a person's door; the agent filing is the SEPARATE triage.fileAs so triage.file's humanDoorOnly claim stays true (TM6)", sp.Name, sp.Method)
		}
		if sp.Mutates {
			mutators++
		}
	}
	if seen != 3 {
		t.Errorf("the bug family carries %d specs, want exactly 3 (two reads + the one filing)", seen)
	}
	if mutators != 1 {
		t.Errorf("%d mutating bug specs, want exactly 1 (the filing) — a second write is TM6 broken", mutators)
	}
}

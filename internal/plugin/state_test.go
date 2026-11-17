// The four states (AP4), the transitions, and the terminal-ness of revoked.
// `not-installed` is the ABSENCE of a record — the other three are rows —
// and the distinction this file guards hardest is between the two states a
// careless person collapses: disabled ("not now") and revoked ("never
// again", record kept). T5 makes them behave differently for in-flight
// work; if both were one state, that distinction could not exist.
package plugin

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

var stateAt = time.Date(2026, time.October, 25, 13, 0, 0, 0, time.UTC)

// fakeStateStore mirrors what migration 0023's constraints enforce at the
// database level — one live record per name (the partial unique index), no
// transition out of revoked (no from_state='revoked' pair the CHECK admits),
// and revoke-without-reason impossible (the length(trim(reason)) CHECK). The
// fake mirrors the rules; the store tests prove the DATABASE enforces them
// even if every line of this package were deleted.
type fakeStateStore struct {
	mu       sync.Mutex
	live     map[string]State
	history  map[string][]TransitionRecord
	creates  int
	records  map[string]string // name -> source
	auditLog []TransitionRecord
	refusals []RefusalRecord
}

func newFakeStore() *fakeStateStore {
	return &fakeStateStore{live: map[string]State{}, history: map[string][]TransitionRecord{}, records: map[string]string{}}
}

func (f *fakeStateStore) CreateRecord(name, source string, at time.Time) *Reject {
	f.mu.Lock()
	defer f.mu.Unlock()
	if st, ok := f.live[name]; ok && st != StateRevoked {
		return &Reject{Field: "name", Detail: fmt.Sprintf("refused: %s already has a live record (state %q) · remedy: revoke it first — history is not overwritten", name, st)}
	}
	f.live[name] = StateDisabled
	f.records[name] = source
	f.creates++
	return nil
}

func (f *fakeStateStore) SetTransition(t TransitionRecord) *Reject {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.To == StateRevoked && t.Reason == "" {
		return refuse("reason", "refused: revoke without a reason")
	}
	cur, ok := f.live[t.Agent]
	if !ok {
		return &Reject{Field: "name", Detail: fmt.Sprintf("refused: %s has no record", t.Agent)}
	}
	if t.From == StateNotInstalled && cur == StateDisabled && t.To == StateDisabled {
		// the import landing: (none) → disabled, recorded right after the
		// record is created — the row already reads disabled.
		f.history[t.Agent] = append(f.history[t.Agent], t)
		f.auditLog = append(f.auditLog, t)
		return nil
	}
	if !cur.CanTransition(t.To) {
		return &Reject{Field: "state", Detail: fmt.Sprintf("refused: %s %s → %s is not a transition", t.Agent, cur, t.To)}
	}
	if t.From != cur {
		return &Reject{Field: "state", Detail: fmt.Sprintf("refused: %s moved under us (record is %q, caller said %q)", t.Agent, cur, t.From)}
	}
	f.live[t.Agent] = t.To
	f.history[t.Agent] = append(f.history[t.Agent], t)
	f.auditLog = append(f.auditLog, t)
	return nil
}

func (f *fakeStateStore) StateOf(name string) (State, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.live[name]
	return st, ok
}

func (f *fakeStateStore) RecordRefusal(r RefusalRecord) *Reject {
	if r.Agent == "" || r.Reason == "" || r.RevokedAt.IsZero() {
		return &Reject{Field: "refusal", Detail: "an arrival refusal needs agent, revoked_at and reason — the three future readers ask about"}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refusals = append(f.refusals, r)
	return nil
}

func (f *fakeStateStore) RefusalsOf(agent string) ([]RefusalRecord, *Reject) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []RefusalRecord
	for _, r := range f.refusals {
		if r.Agent == agent {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStateStore) Transitions(name string) ([]TransitionRecord, *Reject) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TransitionRecord(nil), f.history[name]...), nil
}

// withStore binds a fresh fake, imports one agent, and returns both.
func withStore(t *testing.T, name string) (*fakeStateStore, *Agent) {
	t.Helper()
	f := newFakeStore()
	BindStateStore(f)
	t.Cleanup(func() { BindStateStore(nil); Reset() })
	dir := writePackage(t, name, "", nil, nil)
	a, rj := ImportChecked(dir, nil, stateAt)
	if rj != nil {
		t.Fatalf("import: %s", rj.Error())
	}
	return f, a
}

func TestStatesAreClosedAndEnumerated(t *testing.T) {
	// Four values, and NOTHING reachably produces a fifth combination —
	// which is why this is not two booleans (a boolean pair admits the
	// fourth combination that means nothing, and somebody will render it).
	want := []State{StateNotInstalled, StateDisabled, StateEnabled, StateRevoked}
	got := AllStates()
	if len(got) != len(want) {
		t.Fatalf("the vocabulary is closed at %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("AllStates()[%d] = %q want %q", i, got[i], want[i])
		}
	}
	// The persisted three (not-installed is the ABSENCE of a row): the same
	// vocabulary the migration's CHECK mirrors, by the store's own list.
	if n := len(PersistedStates()); n != 3 {
		t.Fatalf("three states are rows, got %v", PersistedStates())
	}
	for _, s := range PersistedStates() {
		if s == StateNotInstalled {
			t.Fatal("not-installed must never be a row — it is the absence of one")
		}
	}
}

func TestRevokedIsTerminal(t *testing.T) {
	f, a := withStore(t, "revoked-agent")
	if _, rj := Revoke(a.Name, "the vendor shipped a data-exfil update"); rj != nil {
		t.Fatal(rj.Error())
	}
	for _, to := range AllStates() {
		if to == StateRevoked {
			continue
		}
		if StateRevoked.CanTransition(to) {
			t.Fatalf("revoked is terminal: %s → %s is open", StateRevoked, to)
		}
	}
	// The verbs agree with the table (no back door around it):
	if _, rj := Enable(a.Name); rj == nil {
		t.Fatal("enable on a revoked agent refuses")
	} else if rj.Field != "state" {
		t.Fatalf("the refusal is about the state: %q", rj.Detail)
	}
	if st, _ := f.StateOf(a.Name); st != StateRevoked {
		t.Fatalf("still revoked: %q", st)
	}
}

func TestRevokedIsNotNotInstalled(t *testing.T) {
	// The record SURVIVES — "what did we once run, and why did we stop" is
	// asked precisely when something has gone wrong, and it must have an
	// answer that is not "nothing ever happened here".
	f, a := withStore(t, "once-ran")
	if _, rj := Enable(a.Name); rj != nil {
		t.Fatal(rj.Error())
	}
	if _, rj := Revoke(a.Name, "prompt-injection in its upstream docs"); rj != nil {
		t.Fatal(rj.Error())
	}
	st, found := f.StateOf(a.Name)
	if !found {
		t.Fatal("a revoked agent is NOT not-installed: the record must survive")
	}
	if st != StateRevoked {
		t.Fatalf("the record says revoked, got %q", st)
	}
	hist, rjH := f.Transitions(a.Name)
	if rjH != nil {
		t.Fatal(rjH.Error())
	}
	var revoke *TransitionRecord
	for i := range hist {
		if hist[i].To == StateRevoked {
			revoke = &hist[i]
		}
	}
	if revoke == nil || revoke.Reason != "prompt-injection in its upstream docs" {
		t.Fatalf("the WHY survives with the record: %+v", hist)
	}
}

func TestReimportAfterRevokeIsANewRecord(t *testing.T) {
	// Somebody put this back must be VISIBLE, not inferred: re-import after
	// revocation creates a NEW record — the revoked row stays, and its
	// transitions remain readable underneath the new life.
	f, a := withStore(t, "reimported")
	if _, rj := Revoke(a.Name, "first life ended badly"); rj != nil {
		t.Fatal(rj.Error())
	}
	createsBefore := f.creates
	dir := writePackage(t, "reimported", "", nil, nil)
	a2, rj := ImportChecked(dir, nil, stateAt)
	if rj != nil {
		t.Fatalf("re-import after revoke is allowed (it is a NEW record): %s", rj.Error())
	}
	if f.creates != createsBefore+1 {
		t.Fatal("a new record was not created")
	}
	if a2.State != StateDisabled {
		t.Fatalf("and it arrives disabled like any import: %q", a2.State)
	}
	// Both lives readable: the revoked history still holds its revoke row.
	var sawOldRevoke bool
	histRI, rjRI := f.Transitions("reimported")
	if rjRI != nil {
		t.Fatal(rjRI.Error())
	}
	for _, tr := range histRI {
		if tr.To == StateRevoked && tr.Reason == "first life ended badly" {
			sawOldRevoke = true
		}
	}
	if !sawOldRevoke {
		t.Fatal("the old life's record survived into the new one's history")
	}
}

func TestDisableEnableReversible(t *testing.T) {
	f, a := withStore(t, "toggle-agent")
	if _, rj := Enable(a.Name); rj != nil {
		t.Fatal(rj.Error())
	}
	if st, _ := f.StateOf(a.Name); st != StateEnabled {
		t.Fatalf("enabled: %q", st)
	}
	if _, rj := Disable(a.Name); rj != nil {
		t.Fatal(rj.Error())
	}
	if st, _ := f.StateOf(a.Name); st != StateDisabled {
		t.Fatalf("disabled: %q", st)
	}
	// Back again — the cheap direction is cheap both ways (no reason asked).
	if _, rj := Enable(a.Name); rj != nil {
		t.Fatalf("disable/enable are reversible: %s", rj.Error())
	}
	// And the transition table says so:
	for _, pair := range [][2]State{{StateDisabled, StateEnabled}, {StateEnabled, StateDisabled}} {
		if !pair[0].CanTransition(pair[1]) {
			t.Fatalf("%s → %s must be open", pair[0], pair[1])
		}
	}
	if StateDisabled.CanTransition(StateRevoked) == false || StateEnabled.CanTransition(StateRevoked) == false {
		t.Fatal("both live states can be revoked")
	}
	if StateNotInstalled.CanTransition(StateEnabled) {
		t.Fatal("(none) → enabled must be impossible — import lands disabled, always (AP2)")
	}
	if StateNotInstalled.CanTransition(StateRevoked) {
		t.Fatal("you cannot revoke something that was never here")
	}
}

func TestRevokeRequiresAReason(t *testing.T) {
	_, a := withStore(t, "reasonless")
	if _, rj := Revoke(a.Name, ""); rj == nil {
		t.Fatal("revoke without a reason refuses — \"never again\" without a why is a deletion with extra steps")
	} else if rj.Field != "reason" {
		t.Fatalf("the refusal names the thing refused: %q", rj.Field)
	}
	if _, rj := Revoke(a.Name, "   "); rj == nil {
		t.Fatal("whitespace is not a reason")
	}
	if _, rj := Revoke(a.Name, "vendor revokes its own card"); rj != nil {
		t.Fatal(rj.Error())
	}
}

func TestEveryTransitionIsRecorded(t *testing.T) {
	f, a := withStore(t, "audited")
	if _, rj := Enable(a.Name); rj != nil {
		t.Fatal(rj.Error())
	}
	if _, rj := Disable(a.Name); rj != nil {
		t.Fatal(rj.Error())
	}
	if _, rj := Revoke(a.Name, "the audit trail earns its keep here"); rj != nil {
		t.Fatal(rj.Error())
	}
	hist, rjH := f.Transitions(a.Name)
	if rjH != nil {
		t.Fatal(rjH.Error())
	}
	// import landed, up, down, revoked — four rows, each with who/what/when.
	if len(hist) != 4 {
		t.Fatalf("every transition is recorded: want 4 rows, got %d (%+v)", len(hist), hist)
	}
	want := []State{StateEnabled, StateDisabled, StateRevoked}
	for i, w := range want {
		if hist[i+1].To != w {
			t.Fatalf("transition %d: to %q want %q", i+1, hist[i+1].To, w)
		}
		if hist[i+1].At.IsZero() || hist[i+1].Agent != "audited" {
			t.Fatalf("a row names agent, instant and move: %+v", hist[i+1])
		}
	}
	if hist[3].Reason != "the audit trail earns its keep here" {
		t.Fatal("the revoke row carries its reason")
	}
	// Enable/disable may have NO reason — demanding one for a routine
	// toggle gets you "because", and then the reason column lies.
	if hist[1].Reason != "" {
		t.Fatalf("enable needs no reason: %q", hist[1].Reason)
	}
}

func TestVerbsRefuseUnknownAgents(t *testing.T) {
	BindStateStore(newFakeStore())
	t.Cleanup(func() { BindStateStore(nil); Reset() })
	if _, rj := Enable("never-imported"); rj == nil {
		t.Fatal("enabling something that was never imported refuses plainly")
	}
	if _, rj := Revoke("never-imported", "x"); rj == nil {
		t.Fatal("and so does revoking it — there is nothing here to revoke")
	}
}

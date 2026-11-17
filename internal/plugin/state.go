// The four states (AP4). Two booleans would admit a fourth combination that
// means nothing — and somebody would eventually render it — so the state is
// a closed type with exactly four values, and the transition table is the
// only door between them.
//
//	not-installed   never a row: the ABSENCE of a record
//	disabled        here, not running — "not now"
//	enabled         running
//	revoked         "never again" — terminal, and the record is KEPT
//
// disabled vs revoked is not cosmetic: T5 gives them different behaviour
// for in-flight work (disable lets a Task finish, revoke refuses its
// result). If both were one state, that distinction could not exist, and
// somebody would reach for `disable` expecting it to stop something.
package plugin

import (
	"strings"
	"time"
)

// State is the closed vocabulary. The values are the ROW text migration
// 0023's CHECK mirrors; the enumeration test pins the two together.
type State string

const (
	// StateNotInstalled is the answer when no record exists — it is never
	// stored, only returned (AP4: revoked is NOT this, and the difference
	// is the whole record's survival).
	StateNotInstalled State = "not-installed"
	// StateDisabled is the arrival state (T1) and the other side of the
	// cheap, reversible pair.
	StateDisabled State = "disabled"
	// StateEnabled — declared here, in the state machine, and nowhere the
	// import path can reach (T1's grep pins import.go silent about it).
	StateEnabled State = "enabled"
	// StateRevoked is terminal. The only "exit" is a NEW record via
	// re-import; the revoked row and its transitions stay.
	StateRevoked State = "revoked"
)

// AllStates enumerates the closed set — the test asserts against THIS list,
// so widening the type without widening the table is caught as a test
// failure rather than shipped as a fifth state nobody reasoned about.
func AllStates() []State {
	return []State{StateNotInstalled, StateDisabled, StateEnabled, StateRevoked}
}

// PersistedStates is the three that are rows; not-installed is the absence
// of one. Migration 0023's CHECK mirrors exactly these, and the store's own
// vocabulary list (store.PluginAgentStates) is compared against it by test.
func PersistedStates() []State {
	return []State{StateDisabled, StateEnabled, StateRevoked}
}

// transitions is the whole state machine:
//
//	(none)     → disabled          import — the only way in
//	disabled   ⇄ enabled           enable / disable — reversible, cheap
//	disabled   → revoked           revoke — terminal
//	enabled    → revoked           revoke — terminal
var transitions = map[State][]State{
	StateNotInstalled: {StateDisabled},
	StateDisabled:     {StateEnabled, StateRevoked},
	StateEnabled:      {StateDisabled, StateRevoked},
	StateRevoked:      {}, // terminal: no value on any right side names it as FROM
}

// CanTransition is the table's only read.
func (s State) CanTransition(to State) bool {
	for _, t := range transitions[s] {
		if t == to {
			return true
		}
	}
	return false
}

// TransitionRecord is one recorded move — who, from what, to what, why,
// when. Every transition is recorded; revoke REQUIRES the reason (a "never
// again" without a why is a deletion with extra steps), while enable and
// disable may carry none — they are cheap and reversible, and demanding a
// reason for a routine toggle gets you "because".
type TransitionRecord struct {
	Agent  string
	From   State
	To     State
	Reason string
	At     time.Time
}

// StateStore is the durable side, implemented by internal/store (migration
// 0023) and wired by the daemon. plugin does not open stores — it defines
// the contract and asks. The fake in state_test.go mirrors the database's
// constraints (live-uniqueness, terminal revoked, reason-on-revoke); the
// store's own tests prove the DATABASE enforces them even with this package
// deleted — the double enforcement is the point.
type StateStore interface {
	CreateRecord(name, source string, at time.Time) *Reject
	SetTransition(t TransitionRecord) *Reject
	StateOf(name string) (State, bool) // false = not-installed
	// Transitions reads an agent's recorded life. A read that FAILS is the
	// third state (S10's audit rule): it must never render as "nothing
	// happened here".
	Transitions(name string) ([]TransitionRecord, *Reject)

	// RecordRefusal keeps what AP7 demands when a revoked agent's completed
	// work is stopped at the door; RefusalsOf reads it back. The cost in a
	// refusal is RECORDED, never recovered.
	RecordRefusal(r RefusalRecord) *Reject
	RefusalsOf(agent string) ([]RefusalRecord, *Reject)
}

var stateStore StateStore

// BindStateStore wires the durable side (nil unbinds; tests and the
// level-0 absent state run without one).
func BindStateStore(s StateStore) { stateStore = s }

// Enable moves disabled → enabled. The only call paths here; there is no
// exported setter, because a setter is a back door around the table.
func Enable(name string) (*Agent, *Reject) { return move(name, StateEnabled, "") }

// Disable moves enabled → disabled — the cheap direction, no reason asked.
func Disable(name string) (*Agent, *Reject) { return move(name, StateDisabled, "") }

// Revoke moves a live agent → revoked, TERMINALLY, and requires a reason.
// What revoke does to in-flight work is T5's; what it must not do is delete
// policies (T5's test) or erase the record (AP4, tested here).
func Revoke(name, reason string) (*Agent, *Reject) {
	if strings.TrimSpace(reason) == "" {
		return nil, refuse("reason", "refused: revoke %s without a reason · \"never again\" without a why is a deletion with extra steps · remedy: --reason with the actual why (the vendor, the audit, the incident) — it is the column future readers ask about", name)
	}
	return move(name, StateRevoked, reason)
}

func move(name string, to State, reason string) (*Agent, *Reject) {
	mu.Lock()
	a, ok := imported[name]
	mu.Unlock()
	if !ok && (to == StateDisabled || to == StateRevoked) {
		// Stopping needs NOTHING from the package — and requiring it would
		// make revoke depend on the very thing that may have motivated it
		// (the vendor's directory, present on this disk). The durable row
		// alone carries the whole truth a stop needs.
		if stateStore == nil {
			return nil, refuse("name", "refused: %s is unknown to this process AND no ledger is wired — there is nothing to stop and no record of what was here; UNAVAILABLE is not none", name)
		}
		st, found := stateStore.StateOf(name)
		if !found {
			return nil, refuse("name", "refused: %s is not imported in this process and has no record · first-party agents have no plugin state (they are the 25) — remedy: aeon agent import <path> first, or name one of the 25", name)
		}
		if !st.CanTransition(to) {
			return nil, refuse("state", "refused: %s is %q · %s → %s is not a transition (the table is the only door; revoked is terminal)", name, st, st, to)
		}
		tr := TransitionRecord{Agent: name, From: st, To: to, Reason: reason, At: time.Now().UTC()}
		if rj := stateStore.SetTransition(tr); rj != nil {
			return nil, rj
		}
		return nil, nil // stopped by rows alone — no in-memory copy existed to move
	}
	if !ok {
		// In-process honesty: ENABLE speaks about agents THIS process holds
		// the package for (prompt, card, source). A durable record without
		// the package cannot run — its prompt is on a disk this process has
		// not read, and running unread prose is AS12's nightmare with our
		// own signature on it.
		return nil, refuse("name", "refused: %s is not imported in this process · first-party agents have no plugin state (they are the 25); and a durable record without its package cannot run — remedy: aeon agent import <path> first (or rehydrate it), then enable", name)
	}
	from := a.State
	if stateStore != nil {
		if st, found := stateStore.StateOf(name); found {
			from = st // the durable record outranks the in-memory copy
		}
	}
	if !from.CanTransition(to) {
		return nil, refuse("state", "refused: %s is %q · %s → %s is not a transition · the state machine has exactly four states and the table is the only door (revoked is terminal — a re-import creates a NEW record and makes \"somebody put this back\" visible)", name, from, from, to)
	}
	tr := TransitionRecord{Agent: name, From: from, To: to, Reason: reason, At: time.Now().UTC()}
	if stateStore != nil {
		if rj := stateStore.SetTransition(tr); rj != nil {
			return nil, rj
		}
	}
	a.State = to
	mu.Lock()
	imported[name] = a
	mu.Unlock()
	return &a, nil
}

// recordImport wires step one's durable half: the new record (rejected if a
// LIVE one already exists — history is not overwritten) and its
// (none) → disabled transition. Called by ImportChecked after all three
// steps pass; on refusal the import itself is unwound.
func recordImport(a *Agent) *Reject {
	if stateStore == nil {
		return nil
	}
	if rj := stateStore.CreateRecord(a.Name, a.Source, a.ImportedAt); rj != nil {
		return rj
	}
	return stateStore.SetTransition(TransitionRecord{Agent: a.Name, From: StateNotInstalled, To: StateDisabled, At: a.ImportedAt})
}

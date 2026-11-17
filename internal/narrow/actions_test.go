package narrow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// tableUnderTest is the assertion target for the existence gate: S2's OWN
// method table, constructed the way the daemon's tests construct it
// (protocol/handlers_test.go pattern). PublicMethods is what S2 documented
// for this slice — "the methods a narrowed surface may mount" (method.go:603)
// — so the narrow surface asserts against that table, never a copy (C4).
func tableUnderTest(t *testing.T) []protocol.MethodName {
	t.Helper()
	table := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	return table.PublicMethods()
}

func containsMethod(ms []protocol.MethodName, want protocol.MethodName) bool {
	for _, m := range ms {
		if m == want {
			return true
		}
	}
	return false
}

// TestNoActionMatchesOperationalControl is the enumeration (D1, C2, I1):
// over the WHOLE table, not against named actions — and the enumeration is a
// function over tables so the test can feed it poisoned ones. The risk is
// not `kill`, which anyone would think to forbid; it is the synonym added in
// a year that nobody thought about. Both poisonings must be caught: the
// trigger-kind one by the one-trigger pin, the read-kind one by the
// forbidden-token scan over name AND socket method (an operational action
// must project some socket method, every existing operational method carries
// a forbidden word, and a NEW method fails TestEverySocketMethodExists —
// N8 pins that door shut).
func TestNoActionMatchesOperationalControl(t *testing.T) {
	if got := violations(Actions()); len(got) != 0 {
		t.Fatalf("the real table carries operational control:\n  %s", strings.Join(got, "\n  "))
	}

	poisoned := []struct {
		why    string
		table  []Action
		wantIn string
	}{
		{
			why: "a trigger named with a synonym nobody forbade",
			table: []Action{
				{Name: "aeon_loop_terminate", SocketMethod: protocol.MethodLoopStart, Kind: KindTrigger, Cannot: []string{"x"}},
			},
			wantIn: "only trigger",
		},
		{
			why: "an operational method smuggled in as a read",
			table: []Action{
				{Name: "aeon_step_halt", SocketMethod: protocol.MethodStepStop, Kind: KindRead, Cannot: []string{"x"}},
			},
			wantIn: "token stop",
		},
		{
			// S20 T8's own poisoning: the clock's fire-now verb smuggled
			// in as an action. The method EXISTS (for CLI and app) and is
			// still a violation HERE — existence on the socket table is
			// not eligibility for the narrow surface (R6).
			why: "the clock's fire-now smuggled in as a trigger action",
			table: []Action{
				{Name: "aeon_trigger_fire_now", SocketMethod: protocol.MethodTriggerFireNow, Kind: KindTrigger, Cannot: []string{"x"}},
			},
			wantIn: "token fire",
		},
		{
			why: "an answer action wearing a benign name",
			table: []Action{
				{Name: "aeon_gate_close", SocketMethod: protocol.MethodLoopAnswer, Kind: KindRead, Cannot: []string{"x"}},
			},
			wantIn: "token answer",
		},
	}
	for _, p := range poisoned {
		got := violations(p.table)
		if len(got) == 0 {
			t.Errorf("%s: the enumeration passed a table it must refuse (%v)", p.why, p.table)
			continue
		}
		joined := strings.Join(got, "\n")
		if !strings.Contains(joined, p.wantIn) {
			t.Errorf("%s: refusal must mention %q, got:\n%s", p.why, p.wantIn, joined)
		}
	}
}

// TestEverySocketMethodExists asserts every projection against S2's real
// table — the derived-not-parallel requirement (N13, D3, I3). A method
// renamed or removed upstream fails HERE, not on a live call.
func TestEverySocketMethodExists(t *testing.T) {
	public := tableUnderTest(t)
	if len(public) == 0 {
		t.Fatal("S2's table answered no public methods — the assertion target is broken, not the table")
	}
	for _, a := range Actions() {
		if !containsMethod(public, a.SocketMethod) {
			t.Errorf("%s projects %q, which is not in S2's PublicMethods()", a.Name, a.SocketMethod)
		}
	}
}

// TestEveryEntryStatesWhatItCannotDo enforces N12: an empty Cannot fails.
// Every narrow action is narrower than the method it projects, and the
// difference is the point — an entry whose author cannot name its own
// narrowing has not been thought about.
func TestEveryEntryStatesWhatItCannotDo(t *testing.T) {
	for _, a := range Actions() {
		if len(a.Cannot) == 0 {
			t.Errorf("%s: an empty Cannot means nobody decided what this entry may not do", a.Name)
		}
		for i, c := range a.Cannot {
			if strings.TrimSpace(c) == "" {
				t.Errorf("%s: Cannot[%d] is blank filler", a.Name, i)
			}
		}
	}
}

// TestActionsIsTheOnlyRegistrationPoint (O7, I2, E1) scans every non-test Go
// file in the module: nothing outside this package constructs an Action, and
// nothing appends to the table at runtime. Actions() is the single door.
func TestActionsIsTheOnlyRegistrationPoint(t *testing.T) {
	root := filepath.Join("..", "..") // module root, seen from internal/narrow
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		clean := filepath.ToSlash(path)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s := string(b)
		if strings.Contains(clean, "/internal/narrow/") {
			// Inside the package: only actions.go may append to the table.
			if !strings.HasSuffix(clean, "/actions.go") && strings.Contains(s, "append(actions") {
				t.Errorf("narrow: %s grows the table at runtime", clean)
			}
			return nil
		}
		if strings.Contains(s, "narrow.Action{") {
			t.Errorf("%s constructs narrow.Action outside the table — Actions() is the only registration point", clean)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
}

// TestBoundaryNamesWhatToUseInstead (O3, D2): the statement is a
// redirection, not a wall. It must enumerate the ABSENT words (rendered
// from the same data the enumeration checks, never hand-copied), carry the
// reason, and name alternatives that EXIST — the app and the socket. It must
// not name `aeon loop stop`: no such CLI verb exists (plan.md F-1), and
// promising a phantom verb is how S18's walk found the old demo lying.
func TestBoundaryNamesWhatToUseInstead(t *testing.T) {
	s := BoundaryStatement()
	for _, want := range []string{
		"aeon_loop_start (trigger)",        // available half, rendered from the table
		"aeon_questions_pending",           // reads too
		"not restricted, absent",           // the absence half
		"around every bound placed on one", // the reason
		"the app",                          // a real alternative
		"`step.stop`",                      // the real socket verb
		"0600",                             // why the socket is the human door (P26)
	} {
		if !strings.Contains(s, want) {
			t.Errorf("boundary statement must contain %q, got:\n%s", want, s)
		}
	}
	for _, absentWord := range ForbiddenOperationalControl {
		if !strings.Contains(s, absentWord) {
			t.Errorf("boundary statement must enumerate absent capability %q", absentWord)
		}
	}
	if strings.Contains(s, "aeon loop stop") {
		t.Error("the boundary promises `aeon loop stop`, which does not exist (plan.md F-1) — name only real doors")
	}
}

// TestTableMatchesCommittedFixture is the O7 reviewability gate in both
// directions: adding, renaming, or re-wording an entry fails until
// testdata/actions.json is edited in the same commit — so widening the
// surface is always a visible diff, never a side effect.
func TestTableMatchesCommittedFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "actions.json"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	got, err := json.MarshalIndent(Actions(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(raw)) {
		t.Errorf("the table and testdata/actions.json disagree — edit the fixture in the same commit as the entry:\n--- table ---\n%s\n--- fixture ---\n%s", got, raw)
	}
	// The other direction: the fixture parses back to the same table, so the
	// fixture cannot carry an entry the code refuses to load.
	var fromFixture []Action
	if err := json.Unmarshal(raw, &fromFixture); err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	fa, ga := fromFixture, Actions()
	if len(fa) != len(ga) {
		t.Fatalf("fixture carries %d entries, the table %d", len(fa), len(ga))
	}
	for i := range fa {
		if fa[i].Name != ga[i].Name || fa[i].SocketMethod != ga[i].SocketMethod || fa[i].Kind != ga[i].Kind ||
			strings.Join(fa[i].Cannot, "\x00") != strings.Join(ga[i].Cannot, "\x00") {
			t.Errorf("fixture entry %d differs from the table: %+v vs %+v", i, fa[i], ga[i])
		}
	}
}

// TestKindsAreClosed: the enumerated categories of caller permission. A new
// kind is a new category, and categories must not grow quietly — the set
// widens only by a commit that updates this test beside the table.
func TestKindsAreClosed(t *testing.T) {
	seen := map[Kind]bool{}
	for _, a := range Actions() {
		if !a.Kind.Valid() {
			t.Errorf("%s: kind %q is not one of the three closed kinds", a.Name, a.Kind)
		}
		seen[a.Kind] = true
	}
	// Four since S21 T6 — and CLOSED is still the word: the webhook kind
	// arrived at S20 T6 and the callback kind at S21 T6, each by a
	// table-changing commit that updated THIS assertion in the same diff,
	// which is the whole point of a closed set (a kind added by accident
	// still dies here).
	if len(seen) != 4 || !seen[KindRead] || !seen[KindTrigger] || !seen[KindWebhook] || !seen[KindCallback] {
		t.Errorf("the table must demonstrate all four closed kinds, saw %v", seen)
	}
	if Kind("watch").Valid() || Kind("").Valid() {
		t.Error("Valid() accepted a kind outside the closed set")
	}
}

// TestTriggerIsOneWay: the table holds exactly one trigger — the one-way
// start — and its own Cannot says so: it cannot stop, resume, retry or steer
// the loop it started. Start is what CI needs; everything AFTER the start is
// human authority (R6).
func TestTriggerIsOneWay(t *testing.T) {
	var triggers []Action
	for _, a := range Actions() {
		if a.Kind == KindTrigger {
			triggers = append(triggers, a)
		}
	}
	if len(triggers) != 1 {
		t.Fatalf("exactly one trigger action is allowed, found %d", len(triggers))
	}
	a := triggers[0]
	if a.Name != "aeon_loop_start" || a.SocketMethod != protocol.MethodLoopStart {
		t.Fatalf("the one trigger must be aeon_loop_start → loop.start, got %s → %s", a.Name, a.SocketMethod)
	}
	joined := strings.Join(a.Cannot, " ")
	for _, must := range []string{"stop", "resume", "retry", "steer"} {
		if !strings.Contains(joined, must) {
			t.Errorf("the trigger's Cannot must disclaim %q — the one-way property is written in the table, not just tested", must)
		}
	}
}

// TestQ1Recorded answers Q1 for the record (N8): every read action this
// slice projects — loop.list, loop.show, step.show, blueprint.validate,
// store.awaiting — already existed in S2's table. **No socket method was
// added for S19, and none was wanted.** The narrow surface did not grow the
// wide one; had a read needed something the socket lacked, the finding would
// be recorded HERE as a finding about S2 rather than met with a new method.
// (This table also asserts the debug methods stay unprojectable:
// PublicMethods excludes them at the source.)
func TestQ1Recorded(t *testing.T) {
	public := tableUnderTest(t)
	for _, a := range Actions() {
		if !containsMethod(public, a.SocketMethod) {
			t.Errorf("%s wants %q, which S2 does not have — record the finding, do NOT add the method (N8)", a.Name, a.SocketMethod)
		}
		if strings.HasPrefix(string(a.SocketMethod), "debug.") {
			t.Errorf("%s projects a debug method — PublicMethods must keep these out of every narrowed surface", a.Name)
		}
	}
}

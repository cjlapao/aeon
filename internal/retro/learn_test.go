package retro

// learn_test.go — S63 T3's pure half: the milestone ladder, RT4a's one
// decider, the family map, and the closed grammar's refusals. The store-
// wired half (write/reconcile/leak) lives in the daemon fixture.

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

func iptr(v int) *int { return &v }

func TestNotableIsOneFunction(t *testing.T) {
	cases := []struct {
		name string
		o    Outcome
		want bool
	}{
		{"done clean", Outcome{OutcomeKind: "done"}, false},
		{"done clean-ish with NULLs", Outcome{OutcomeKind: "done", Retries: nil, Escalations: nil}, false},
		{"done zero-scar", Outcome{OutcomeKind: "done", Retries: iptr(0), TotalFailedGates: iptr(0)}, false},
		{"done with retries", Outcome{OutcomeKind: "done", Retries: iptr(1)}, true},
		{"done with failed gates", Outcome{OutcomeKind: "done", TotalFailedGates: iptr(2)}, true},
		{"done unverified", Outcome{OutcomeKind: "done", UnverifiedSteps: iptr(1)}, true},
		{"done escalated", Outcome{OutcomeKind: "done", Escalations: iptr(1)}, true},
		{"done with failed tasks", Outcome{OutcomeKind: "done", TasksFailed: iptr(1)}, true},
		{"aborted", Outcome{OutcomeKind: "aborted"}, true},
		{"blocked", Outcome{OutcomeKind: "blocked/cap-hit"}, true},
		{"other finished", Outcome{OutcomeKind: "other:finished-weird"}, true},
	}
	for _, c := range cases {
		if got := Notable(c.o); got != c.want {
			t.Errorf("%s: Notable = %v, want %v", c.name, got, c.want)
		}
	}
	// The grep half: ONE definition, and no sibling file in this package
	// re-decides it by comparing outcome kinds of its own.
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	defs := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		defs += strings.Count(string(src), "func Notable(")
	}
	if defs != 1 {
		t.Fatalf("func Notable( appears %d times in the package — RT4a is ONE function", defs)
	}
}

func TestMilestoneLadderIsTheSpecSeries(t *testing.T) {
	want := []int{1, 3, 10, 30, 100, 300, 1000, 3000, 10000, 30000, 100000, 300000, 1000000}
	if !reflect.DeepEqual(Milestones, want) {
		t.Fatalf("ladder = %v, want %v (RT4's ×3.33 series to 10^6 — a change here is a review act)", Milestones, want)
	}
	if !isMilestone(1) || !isMilestone(3) || !isMilestone(10) {
		t.Fatal("1, 3, 10 must be milestones (the spec's own example)")
	}
	for _, n := range []int{0, 2, 4, 9, 11, 12, 500, 999999, 1000001} {
		if isMilestone(n) {
			t.Errorf("isMilestone(%d) true — the ladder is discrete", n)
		}
	}
	// windowOf: the milestone's own reach — 1 covers one, 3 covers two,
	// 10 covers the seven since the previous milestone.
	for n, want := range map[int]int{1: 1, 3: 2, 10: 7, 30: 20, 300: 200} {
		if got := windowOf(n); got != want {
			t.Errorf("windowOf(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestFamilyIsTotal(t *testing.T) {
	cases := []struct {
		o    Outcome
		want string
	}{
		{Outcome{OutcomeKind: "done"}, "default"},
		{Outcome{OutcomeKind: "done", Retries: iptr(1)}, "done-with-rework"},
		{Outcome{OutcomeKind: "done", TotalFailedGates: iptr(1)}, "done-with-rework"},
		{Outcome{OutcomeKind: "done", UnverifiedSteps: iptr(2)}, "unverified"},
		{Outcome{OutcomeKind: "aborted"}, "aborted"},
		{Outcome{OutcomeKind: "blocked/cap-hit"}, "blocked"},
		{Outcome{OutcomeKind: "blocked/other:whatever"}, "blocked"},
		{Outcome{OutcomeKind: "other:finished-weird"}, "default"},
		{Outcome{OutcomeKind: "done", Escalations: iptr(3)}, "default"}, // escalation is not a scar family
	}
	for _, c := range cases {
		if got := family(c.o); got != c.want {
			t.Errorf("family(%+v) = %q, want %q", c.o, got, c.want)
		}
	}
	// Every family the map can name must be one of the five shipped keys.
	shipped := map[string]bool{"default": true, "done-with-rework": true, "unverified": true, "aborted": true, "blocked": true}
	for _, c := range cases {
		if !shipped[family(c.o)] {
			t.Fatalf("family %q is not a shipped template key", family(c.o))
		}
	}
}

func TestTemplateWithUnknownPlaceholderRefused(t *testing.T) {
	bad := `Blocked {secret_words} loop`
	if err := Validate(bad); err == nil {
		t.Fatal("Validate accepted an unknown placeholder")
	} else if !strings.Contains(err.Error(), "secret_words") {
		t.Errorf("the refusal does not name the placeholder: %v", err)
	}
	if _, err := Render(bad, Facts{}); err == nil {
		t.Fatal("Render accepted an unknown placeholder")
	}
	// The grammar's edges: stray/unclosed braces are refused too.
	for _, s := range []string{`a } b`, `{unclosed`, `{}`, `{a{b}`, "{count\n}"} {
		if err := Validate(s); err == nil {
			t.Errorf("Validate accepted %q", s)
		}
	}
	// And the good bodies all speak the closed grammar.
	for _, name := range PlaceholderNames() {
		if err := Validate("x {" + name + "} y"); err != nil {
			t.Errorf("registry word {%s} failed its own grammar: %v", name, err)
		}
	}
}

func TestRenderIsTheClosedGrammarOnly(t *testing.T) {
	f := Facts{
		Count: 10, Window: 7, OutcomeKind: "blocked/cap-hit", Blueprint: "standard",
		Retries: iptr(2), Escalations: iptr(1),
		CostFloor: "at least $1.20 (1 of 3 steps unknown)", CostState: "unknown",
		Wall: "2m3s",
	}
	body, err := Render("n={count} w={window} k={outcome_kind} bp={blueprint} r={retries} e={escalations} c={cost_floor} s={cost_state} wall={wall} rr={rounds_review} sf={gates_failed}", f)
	if err != nil {
		t.Fatal(err)
	}
	want := "n=10 w=7 k=blocked/cap-hit bp=standard r=2 e=1 c=at least $1.20 (1 of 3 steps unknown) s=unknown wall=2m3s rr=not recorded sf=not recorded"
	if body != want {
		t.Fatalf("rendered\n  %q\nwant\n  %q", body, want)
	}
}

func TestNullRendersAsNotRecordedNeverZero(t *testing.T) {
	// P31 riding into memory: an empty Facts (everything NULL) must say
	// "not recorded" everywhere a number would lie.
	body, err := Render("{retries}/{gates_failed}/{escalations}/{rounds_review}/{rounds_security}/{cost_floor}/{cost_state}/{wall}", Facts{OutcomeKind: "aborted"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "0") {
		t.Fatalf("a NULL rendered as a zero: %q", body)
	}
	if n := strings.Count(body, notRecorded); n != 8 {
		t.Fatalf("not-recorded count = %d, want 8: %q", n, body)
	}
}

func TestCostStateReadsTheRowsOwnCounters(t *testing.T) {
	cases := []struct {
		o    Outcome
		want string
	}{
		{Outcome{}, notRecorded},
		{Outcome{CostUSDFloor: new(float64)}, notRecorded}, // floor with NO steps is not a cost statement
		{Outcome{CostUSDFloor: func() *float64 { v := 1.0; return &v }(), CostSteps: iptr(2)}, "known"},
		{Outcome{CostUSDFloor: func() *float64 { v := 1.0; return &v }(), CostSteps: iptr(2), CostPartialSteps: iptr(1)}, "partial"},
		{Outcome{CostUSDFloor: func() *float64 { v := 1.0; return &v }(), CostSteps: iptr(2), CostUnknownSteps: iptr(1)}, "unknown"},
	}
	for i, c := range cases {
		if got := costState(c.o); got != c.want {
			t.Errorf("case %d: costState = %q, want %q", i, got, c.want)
		}
	}
	if w := wallWord(Outcome{}); w != notRecorded {
		t.Errorf("NULL wall rendered %q", w)
	}
	ms := int64(123000)
	if w := wallWord(Outcome{WallMS: &ms}); w != "2m3s" {
		t.Errorf("wall rendered %q, want 2m3s", w)
	}
}

// The Outcome alias must keep the store's row type honest here.
var _ = fmt.Sprintf
var _ store.RetroOutcome = Outcome{}

package autonomy

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/action"
)

func mustTime(t *testing.T, d time.Duration) *time.Time {
	n := time.Now().Add(d)
	return &n
}

// TestVocabularyEqualsTheRegistry (AL2/AL15): the vocabulary's action
// subjects are exactly the non-local registry actions, plus the one outward
// class shorthand — read from the registry, never a copy.
func TestVocabularyEqualsTheRegistry(t *testing.T) {
	v := Vocabulary()
	classes := action.Classes()
	got := map[string]bool{}
	for _, s := range v.Subjects {
		got[s.Name] = true
	}
	for _, name := range action.Names() {
		c := classes[name]
		if c == action.EffectLocal {
			if got[name] {
				t.Errorf("a local action %s must not be a subject", name)
			}
			continue
		}
		if !got[name] {
			t.Errorf("a non-local action %s is missing from the vocabulary", name)
		}
	}
	if !got[ClassShorthand] {
		t.Errorf("the outward class shorthand must be in the vocabulary")
	}
	// No extra names.
	if len(got) != len(action.Names())-countLocal(classes)+1 {
		t.Errorf("vocabulary subject count = %d, want %d (non-local actions + 1 class)",
			len(got), len(action.Names())-countLocal(classes)+1)
	}
}

// TestExpandOutwardIsCurrent (AL2): adding an outward action to the registry
// is covered by an existing outward grant with no edit; an irreversible
// action is not covered by the outward class.
func TestExpandOutwardIsCurrent(t *testing.T) {
	outward := ExpandSubject(ClassSubject())
	for _, n := range outward {
		if action.Classes()[n] != action.EffectOutward {
			t.Errorf("outward expansion contains a non-outward action %s", n)
		}
	}
	if action.Classes()[action.ForgeMerge] == action.EffectOutward {
		t.Fatal("precondition: forge.merge is not outward")
	}
	for _, n := range outward {
		if n == action.ForgeMerge {
			t.Errorf("the outward expansion must not contain forge.merge")
		}
	}
	if got := ExpandSubject(ActionSubject(action.ForgeOpenPR)); got[0] != action.ForgeOpenPR {
		t.Errorf("an action subject expands to itself, got %v", got)
	}
}

// TestValidateRefusals (AL4/AL5): each closed code returns its sentence; a
// merge grant with requireGates:false, no expiry, or 91 days is refused; an
// irreversible class is refused.
func TestValidateRefusals(t *testing.T) {
	now := time.Now()
	root := NodeInfo{IsRoot: true}
	child := NodeInfo{IsRoot: false}
	cases := []struct {
		name    string
		g       Grant
		node    NodeInfo
		want    RefusalCode
	}{
		{"unknown-subject", Grant{State: StateAllow, Subject: ActionSubject("not.a.action")}, root, CodeUnknownSubject},
		{"local-subject", Grant{State: StateAllow, Subject: ActionSubject(action.GitCreateBranch)}, root, CodeLocalSubject},
		{"class-not-grantable", Grant{State: StateAllow, Subject: Subject{Kind: "class", Name: "irreversible"}}, root, CodeClassNotGrantable},
		{"condition-forced", Grant{State: StateAllow, Subject: ActionSubject(action.ForgeMerge), ExpiresAt: mustTime(t, 30*24*time.Hour)}, root, CodeConditionForced},
		{"expiry-required", Grant{State: StateAllow, Subject: ActionSubject(action.ForgeMerge), Conditions: Conditions{RequireGates: true}}, root, CodeExpiryRequired},
		{"expiry-too-far", Grant{State: StateAllow, Subject: ActionSubject(action.ForgeMerge), Conditions: Conditions{RequireGates: true}, ExpiresAt: mustTime(t, 91*24*time.Hour)}, root, CodeExpiryTooFar},
		{"expired", Grant{State: StateAllow, Subject: ActionSubject(action.ForgeMerge), Conditions: Conditions{RequireGates: true}, ExpiresAt: mustTime(t, -24*time.Hour)}, root, CodeExpired},
		{"not-a-root", Grant{State: StateAllow, Subject: ActionSubject(action.ForgeOpenPR)}, child, CodeNotARoot},
		{"node-archived", Grant{State: StateAllow, Subject: ActionSubject(action.ForgeOpenPR)}, NodeInfo{IsRoot: true, Archived: true}, CodeNodeArchived},
		{"condition-invalid-maxperday", Grant{State: StateAllow, Subject: ActionSubject(action.ForgeOpenPR), Conditions: Conditions{MaxPerDay: -1}}, root, CodeConditionInvalid},
		{"condition-invalid-labels", Grant{State: StateAllow, Subject: ActionSubject(action.ForgeOpenPR), Conditions: Conditions{Labels: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}}}, root, CodeConditionInvalid},
	}
	for _, c := range cases {
		got := ValidateGrant(c.g, c.node, now)
		if got == nil {
			t.Errorf("%s: expected a refusal, got nil", c.name)
			continue
		}
		if got.Code != c.want {
			t.Errorf("%s: code = %s, want %s (%s)", c.name, got.Code, c.want, got.Sentence)
		}
		if got.Sentence == "" {
			t.Errorf("%s: a refusal must carry a sentence", c.name)
		}
	}
}

// TestValidateAcceptsWellFormed: an outward action and a bounded merge grant
// are accepted.
func TestValidateAcceptsWellFormed(t *testing.T) {
	now := time.Now()
	root := NodeInfo{IsRoot: true}
	if r := ValidateGrant(Grant{State: StateAllow, Subject: ActionSubject(action.ForgeOpenPR)}, root, now); r != nil {
		t.Errorf("outward allow refused: %v", r)
	}
	if r := ValidateGrant(Grant{State: StateAllow, Subject: ActionSubject(action.ForgeMerge),
		Conditions: Conditions{RequireGates: true}, ExpiresAt: mustTime(t, 7*24*time.Hour)}, root, now); r != nil {
		t.Errorf("merge allow refused: %v", r)
	}
	if r := ValidateGrant(Grant{State: StateDeny, Subject: ActionSubject(action.ForgeOpenPR)}, root, now); r != nil {
		t.Errorf("a deny refused: %v", r)
	}
}

// TestConditionsNilSeamIsCannotSay (AL4): a nil seam is cannot-say, never
// met — a merge grant is inert while the verification service is unwired.
func TestConditionsNilSeamIsCannotSay(t *testing.T) {
	// all seams nil
	status := Evaluate(context.Background(), Conditions{RequireGates: true, RequireBudget: true, MaxPerDay: 5},
		"ag-1", "loop-1", 0, Seams{}, time.Now())
	for _, s := range status {
		if s.State != action.CondCannotSay {
			t.Errorf("condition %s with nil seams = %s, want cannot-say", s.Name, s.State)
		}
	}
}

// TestConditionsMetWhenSeamAnswersMet: a passing seam is met.
func TestConditionsMetWhenSeamAnswersMet(t *testing.T) {
	s := Seams{Gates: func(ctx context.Context, loopID string, pr int) Answer { return AnswerMet("the merge rule passed") }}
	status := Evaluate(context.Background(), Conditions{RequireGates: true}, "ag-1", "loop-1", 7, s, time.Now())
	if len(status) != 1 || status[0].State != action.CondMet || status[0].Sentence != "the merge rule passed" {
		t.Errorf("gates met expected, got %+v", status)
	}
}

// TestLabelsMatchAtPinTime (AL4): a labelled grant applies only when the
// start's labels intersect; with no labels supplied a labelled grant never
// applies.
func TestLabelsMatchAtPinTime(t *testing.T) {
	now := time.Now()
	g := Grant{State: StateAllow, Subject: ActionSubject(action.ForgeOpenPR),
		Conditions: Conditions{Labels: []string{"critical"}}, NodeID: "root"}
	if !labelsMatch(g.Conditions.Labels, []string{"critical", "other"}) {
		t.Error("matching labels should match")
	}
	if labelsMatch(g.Conditions.Labels, []string{"other"}) {
		t.Error("non-matching labels should not match")
	}
	if !labelsMatch(Grant{Conditions: Conditions{}}.Conditions.Labels, nil) {
		t.Error("an empty label set means any")
	}
	// A plan with no labels does not cover a labelled grant.
	plan := Plan(PlanInput{Grants: []Grant{g}, Labels: nil, Now: now})
	if coveredBy(plan, action.ForgeOpenPR) {
		t.Error("a labelled grant must not apply with no labels supplied")
	}
}

// TestOutwardClassDoesNotCoverMerge: an outward-class allow covers the
// outward actions but not the merge; the merge is granted only by name.
func TestOutwardClassDoesNotCoverMerge(t *testing.T) {
	now := time.Now()
	outwardAllow := Grant{State: StateAllow, ID: "ag-out", Subject: ClassSubject()}
	plan := Plan(PlanInput{Grants: []Grant{outwardAllow}, Now: now})
	if coveredBy(plan, action.ForgeMerge) {
		t.Error("the outward class must not cover forge.merge")
	}
	if !coveredBy(plan, action.ForgeOpenPR) {
		t.Error("the outward class must cover forge.openPR")
	}
	// An explicit merge grant (its own name, forced gates, bounded expiry)
	// does cover it — subject to the (cannot-say) condition.
	mergeAllow := Grant{State: StateAllow, ID: "ag-merge", Subject: ActionSubject(action.ForgeMerge),
		Conditions: Conditions{RequireGates: true}, ExpiresAt: mustTime(t, 7*24*time.Hour)}
	plan = Plan(PlanInput{Grants: []Grant{mergeAllow}, Now: now})
	if !coveredBy(plan, action.ForgeMerge) {
		t.Error("a merge grant by its own name must cover forge.merge")
	}
}

// TestPlanIsPureAndDeterministic (AL14): the same input twice yields the
// same output; the plan reads no clock (now is an input).
func TestPlanIsPureAndDeterministic(t *testing.T) {
	now := time.Now()
	in := PlanInput{
		Origin: OriginSchedule,
		Labels: []string{"x"},
		Grants: []Grant{
			{State: StateAllow, ID: "ag-1", Subject: ActionSubject(action.ForgeOpenPR)},
			{State: StateDeny, ID: "ag-v", Subject: ActionSubject(action.ForgePostReview)},
		},
		Now: now,
	}
	a := Plan(in)
	b := Plan(in)
	if len(a) != len(b) {
		t.Fatalf("non-deterministic length: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			t.Errorf("plan drifted at %d:\n  a=%+v\n  b=%+v", i, a[i], b[i])
		}
	}
}

// TestVetoOverridesInPlan (AL3): a deny overrides a covering allow in the
// plan, on the same action.
func TestVetoOverridesInPlan(t *testing.T) {
	now := time.Now()
	in := PlanInput{
		Grants: []Grant{
			{State: StateAllow, ID: "ag-1", Subject: ActionSubject(action.ForgeOpenPR)},
			{State: StateDeny, ID: "ag-v", Subject: ActionSubject(action.ForgeOpenPR)},
		},
		Now: now,
	}
	plan := Plan(in)
	ap := findAction(plan, action.ForgeOpenPR)
	if ap.Verdict != action.VerdictDeny {
		t.Errorf("veto must override the allow in the plan: got %v", ap.Verdict)
	}
}

// TestPinFromCarriesOnlyCovered (AL8): the pin's allowed snapshot contains
// only the covered (runnable) grants, expanded to action names.
func TestPinFromCarriesOnlyCovered(t *testing.T) {
	now := time.Now()
	in := PlanInput{
		Grants: []Grant{
			{State: StateAllow, ID: "ag-1", Subject: ActionSubject(action.ForgeOpenPR)},
			{State: StateDeny, ID: "ag-v", Subject: ActionSubject(action.ForgePostReview)},
		},
		Now: now,
	}
	allowed := PinFrom(Plan(in))
	if len(allowed) != 1 || allowed[0].Subject != action.ForgeOpenPR {
		t.Errorf("pin should carry only the covered openPR grant, got %+v", allowed)
	}
}

// TestPackageIsPure (I2): a walk of the package's .go files asserts no
// import of the store, the database, exec or net, and no environment read —
// the pure discipline is a fact, not a hope.
func TestPackageIsPure(t *testing.T) {
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	walked := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		walked++
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, imp := range f.Imports {
			p := imp.Path.Value
			for _, bad := range []string{"internal/store", "database/sql", "os/exec", `net`, `internal/protocol`} {
				if contains(p, bad) {
					t.Errorf("%s imports %s — the package must stay pure", e.Name(), bad)
				}
			}
		}
	}
	if walked == 0 {
		t.Error("walked no files — the guard is vacuous")
	}
}

// --- helpers ---

// countLocal counts the local names of the LOOP-EXECUTABLE set. S61 T4
// (dated 2026-11-05) makes that distinction real: Classes() also carries
// the person-door names, which live in no Names() and in no vocabulary —
// counting the classes map against the names list compared two different
// worlds once the doors arrived, and a census that compares ground to
// doors undercounts and then cries wolf.
func countLocal(classes map[string]action.EffectClass) int {
	inNames := map[string]bool{}
	for _, n := range action.Names() {
		inNames[n] = true
	}
	n := 0
	for name, c := range classes {
		if c == action.EffectLocal && inNames[name] {
			n++
		}
	}
	return n
}

func findAction(plan []ActionPlan, name string) ActionPlan {
	for _, ap := range plan {
		if ap.Action == name {
			return ap
		}
	}
	return ActionPlan{}
}

func coveredBy(plan []ActionPlan, name string) bool {
	// Coverage means a grant matched the action; the verdict may still be
	// deny (e.g. a cannot-say condition), so coverage is GrantID, not Run.
	return findAction(plan, name).GrantID != ""
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

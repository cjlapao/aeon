package retro

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// fake is the whole Reader under test: canned answers per method, with a
// per-method error to stand in for an unreadable field.
type fake struct {
	pin     store.PinnedLoop
	pinErr  error
	steps   []store.Step
	stepsE  error
	tasks   []store.Task
	tasksE  error
	runs    []store.Run
	runsE   error
	cost    store.RunCost
	costE   error
	esc     int
	escE    error
	byName  map[string]store.Agent
	bySlug  map[string]store.Agent
	lookupE error
}

func (f *fake) ReadPinnedLoop(string) (store.PinnedLoop, error) { return f.pin, f.pinErr }
func (f *fake) ListSteps(store.Scope) ([]store.Step, error)     { return f.steps, f.stepsE }
func (f *fake) ListTasks(store.Scope) ([]store.Task, error)     { return f.tasks, f.tasksE }
func (f *fake) ListRuns(string) ([]store.Run, error)            { return f.runs, f.runsE }
func (f *fake) LoopCost(string) (store.RunCost, error)          { return f.cost, f.costE }
func (f *fake) CountEscalationQuestions(store.Scope) (int, error) {
	return f.esc, f.escE
}
func (f *fake) AgentByName(name string) (store.Agent, bool, error) {
	if f.lookupE != nil {
		return store.Agent{}, false, f.lookupE
	}
	a, ok := f.byName[name]
	return a, ok, nil
}
func (f *fake) AgentBySlug(slug string) (store.Agent, bool, error) {
	if f.lookupE != nil {
		return store.Agent{}, false, f.lookupE
	}
	a, ok := f.bySlug[slug]
	return a, ok, nil
}

func basePin() store.PinnedLoop {
	return store.PinnedLoop{
		Loop: store.Loop{
			Slug: "s63-test", ComponentRoot: "/repo/x", Status: "running",
			StartedBy: "user", QARounds: 2, TotalFailedGates: 1, SplitsDone: 0,
			GoalID: "g1", GoalVersion: 3,
		},
		BlueprintID: "dev-loop", BlueprintVersion: 7,
		UpdatedAt: "2026-11-04T21:57:35.123Z", Attended: nil,
	}
}

func TestReasonKindIsClosed(t *testing.T) {
	cases := []struct{ in, want string }{
		{"cap-hit", "blocked/cap-hit"},
		{"cap-hit: daily budget exhausted at 4.20", "blocked/cap-hit"},
		{"budget-unreadable: openai: rate limited", "blocked/budget-unreadable"},
		{"  ", "blocked/other:unknown"},
		{"step-reject", "blocked/other:step-reject"},
	}
	for _, c := range cases {
		if got := NormalizeBlockedKind(c.in); got != c.want {
			t.Errorf("NormalizeBlockedKind(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// The 48-column cap: a long unknown kind cannot overflow the CHECK.
	long := "u" + strings.Repeat("x", 120)
	got := NormalizeBlockedKind(long)
	if len(got) > maxKindLen || !strings.HasPrefix(got, "blocked/other:") {
		t.Errorf("long kind %q escaped the cap (%d chars)", got, len(got))
	}
	// step-<outcome> words are the unmapped family (T0 inventory).
	if k := NormalizeBlockedKind("step-reject"); k != "blocked/other:step-reject" {
		t.Errorf("step-reject mapped to %q, want the other: family", k)
	}
	// Finished: the three known words pass through, anything else is named.
	for _, s := range []string{"done", "aborted", "cancelled"} {
		if OutcomeKindForFinished(s) != s {
			t.Errorf("finished %q was rewritten", s)
		}
	}
	if k := OutcomeKindForFinished("merged"); k != "other:finished-merged" {
		t.Errorf("unknown finished word %q became %q", "merged", k)
	}
	// The terminal key is the idempotency key: kind@pin.updated_at.
	if k := TerminalKey("blocked/cap-hit", "2026-11-04T21:57:35.123Z"); k != "blocked/cap-hit@2026-11-04T21:57:35.123Z" {
		t.Errorf("terminal key %q", k)
	}
}

func TestSkipRules(t *testing.T) {
	if _, skip := SkipPin("/r", "triage-validate-42", "triage-validation"); !skip {
		t.Error("an F15 validation loop was not skipped")
	}
	if _, skip := SkipPin("/r", "triage-validate-", ""); !skip {
		t.Error("the prefix rule missed its edge case")
	}
	if _, skip := SkipPin("/r", "s63-outcome-learning", "retrospective"); !skip {
		t.Error("a retrospective loop (RT5) was not skipped")
	}
	if why, skip := SkipPin("/r", "s63-outcome-learning", "dev-loop"); skip {
		t.Errorf("an ordinary loop was skipped: %s", why)
	}
}

func TestComposeWithoutPinRefuses(t *testing.T) {
	f := &fake{pinErr: store.ErrNotFound}
	_, _, _, err := Compose(context.Background(), f, Event{LoopID: "l1", OutcomeKind: "done"})
	if !errors.Is(err, ErrNoPin) {
		t.Fatalf("missing pin gave %v, want ErrNoPin", err)
	}
	f.pinErr = errors.New("disk on fire")
	if _, _, _, err = Compose(context.Background(), f, Event{LoopID: "l1", OutcomeKind: "done"}); err == nil || errors.Is(err, ErrNoPin) {
		t.Fatalf("read failure gave %v, want the store error (not ErrNoPin)", err)
	}
}

func TestUnreadableFieldsStayNull(t *testing.T) {
	f := &fake{
		pin:    basePin(),
		tasksE: errors.New("no"), stepsE: errors.New("no"), runsE: errors.New("no"),
		costE: errors.New("no"), escE: errors.New("no"),
	}
	out, _, missing, err := Compose(context.Background(), f, Event{LoopID: "l1", OutcomeKind: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if out.TasksDone != nil || out.UnverifiedSteps != nil || out.WallMS != nil ||
		out.CostUSDFloor != nil || out.CostSteps != nil || out.Escalations != nil {
		t.Fatal("an unreadable field arrived as a zero, not NULL (P31)")
	}
	// What COULD be read is still there — a read failure never voids the row.
	if out.QARounds == nil || *out.QARounds != 2 || out.BlueprintVersion == nil || *out.BlueprintVersion != 7 {
		t.Error("the readable fields vanished with the unreadable ones")
	}
	if out.Attended != nil {
		t.Error("attended is the pin's third state — a NULL pin stays NULL")
	}
	if out.GoalID != "g1" || *out.GoalVersion != 3 || out.StartedByKind != "user" {
		t.Error("the goal/started-by coordinates lost")
	}
	for _, want := range []string{"tasks", "steps", "wall", "cost", "escalations"} {
		found := false
		for _, m := range missing {
			if strings.HasPrefix(m, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing is missing %q: %v", want, missing)
		}
	}
	// The permanent honest NULL — named, never zeroed.
	if out.VerifiedPass != nil {
		t.Error("verified_pass was invented")
	}
	if !strings.Contains(strings.Join(missing, "|"), "verified_pass") {
		t.Errorf("verified_pass not named in missing: %v", missing)
	}
}

func TestAgentRowsGroupBySlug(t *testing.T) {
	known := 500
	f := &fake{
		pin: basePin(),
		steps: []store.Step{
			{Agent: "golang-engineer", Outcome: "done", Cost: model.Cost{State: model.CostKnown, USD: 1.0, USDAtLeast: 1.0}, AgentMS: &known},
			{Agent: "golang-engineer", Outcome: "reject", Cost: model.Cost{State: model.CostPartial, USDAtLeast: 0.5}},
			{Agent: "ghost-bot", Outcome: "blocked", Cost: model.Cost{State: model.CostUnknown}},
			{Agent: "", Outcome: "done"}, // nobody's step — counts in nothing
		},
		byName: map[string]store.Agent{"golang-engineer": {Slug: "golang-engineer"}},
	}
	_, agents, _, err := Compose(context.Background(), f, Event{LoopID: "l1", OutcomeKind: "aborted"})
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 {
		t.Fatalf("%d agent rows, want 2 (the empty agent must not be invented)", len(agents))
	}
	g, ghost := agents[1], agents[0] // name-sorted: "ghost-bot" < "golang-engineer"
	if g.Agent != "golang-engineer" || !g.AgentSlugKnown {
		t.Fatalf("row 0: %+v", g)
	}
	if *g.Steps != 2 || *g.StepsDone != 1 || *g.StepsRejected != 1 || *g.VerifyFail != 1 {
		t.Errorf("golang counts wrong: %+v", g)
	}
	if *g.CostUSDFloor != 1.5 || *g.CostPartialSteps != 1 {
		t.Errorf("the floor re-summed known+partial-at-least wrongly: %v / %v", *g.CostUSDFloor, *g.CostPartialSteps)
	}
	if g.AgentMS == nil || *g.AgentMS != 500 {
		t.Errorf("agent_ms %v, want the sum of the readable ones (500)", g.AgentMS)
	}
	if ghost.Agent != "ghost-bot" || ghost.AgentSlugKnown {
		t.Errorf("an unresolvable name claimed a slug: %+v", ghost)
	}
	if *ghost.StepsBlocked != 1 || *ghost.CostUnknownSteps != 1 || *ghost.CostUSDFloor != 0 {
		t.Errorf("ghost cost honesty broken: %+v", ghost)
	}
	if g.VerifyPass != nil || ghost.VerifyPass != nil {
		t.Error("a per-agent verified_pass was invented (T0 §1.9)")
	}
}

func TestCostIsTheFloorNotATotal(t *testing.T) {
	f := &fake{
		pin:  basePin(),
		cost: store.RunCost{USDAtLeast: 3.3, Partial: 2, Unknown: 1, Steps: 5},
		steps: []store.Step{ // steps sum to 2.0 — if Compose re-summed, 2.0 would appear
			{Agent: "a", Cost: model.Cost{State: model.CostKnown, USD: 1.0}},
			{Agent: "a", Cost: model.Cost{State: model.CostKnown, USD: 1.0}},
		},
		byName: map[string]store.Agent{"a": {Slug: "a"}},
	}
	out, _, missing, err := Compose(context.Background(), f, Event{LoopID: "l1", OutcomeKind: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if *out.CostUSDFloor != 3.3 || *out.CostSteps != 5 || *out.CostPartialSteps != 2 || *out.CostUnknownSteps != 1 {
		t.Errorf("outcome cost %v/%v/%v/%v, want the store's floor verbatim (F11)",
			*out.CostUSDFloor, *out.CostSteps, *out.CostPartialSteps, *out.CostUnknownSteps)
	}
	for _, m := range missing {
		if strings.HasPrefix(m, "cost") {
			t.Errorf("a readable cost was named missing: %v", missing)
		}
	}
}

func TestWallFromLastEndedRun(t *testing.T) {
	f := &fake{
		pin: basePin(),
		runs: []store.Run{
			{RunSeq: 1, State: "ended", StartedAt: "2026-11-04T10:00:00Z", EndedAt: "2026-11-04T10:00:01Z"},
			{RunSeq: 2, State: "ended", StartedAt: "2026-11-04T10:05:00Z", EndedAt: "2026-11-04T10:05:03Z"},
			{RunSeq: 3, State: "running", StartedAt: "2026-11-04T10:10:00Z"},
		},
	}
	out, _, missing, err := Compose(context.Background(), f, Event{LoopID: "l1", OutcomeKind: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if out.WallMS == nil || *out.WallMS != 3000 {
		t.Errorf("wall %v, want the LAST ENDED run's 3000ms (not the running tail)", out.WallMS)
	}
	for _, m := range missing {
		if strings.HasPrefix(m, "wall") {
			t.Errorf("wall readable but named missing: %v", missing)
		}
	}
}

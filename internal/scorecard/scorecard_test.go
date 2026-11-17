package scorecard

// scorecard_test.go — T4's rule table made executable: every "each is a
// test" rule has its named test. Whitebox because the rule list talks
// about internals (the compute seam, the nil gate plane, the package's
// own text for the two source-scan guards).

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/retro"
	"github.com/cjlapao/aeon/internal/store"
)

func openFixture(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func depsOf(s *store.Store) Deps { return Deps{S: s} }

func pi(v int) *int         { return &v }
func pf(v float64) *float64 { return &v }
func p64(v int64) *int64    { return &v }

// loop records one terminal row, COMPLETE with every field present by
// default (a full row, so a test's denominators are loops, not NULLs);
// mutators carve the exceptions each rule needs.
func loop(t *testing.T, s *store.Store, root, slug, kind string, agents []store.RetroAgent, mut func(*store.RetroOutcome)) {
	t.Helper()
	attended := true
	o := store.RetroOutcome{
		ComponentRoot:    root,
		LoopSlug:         slug,
		OutcomeKind:      kind,
		TerminalKey:      kind + "@" + slug,
		StartedByKind:    "human",
		Attended:         &attended,
		WallMS:           p64(1000),
		CostUSDFloor:     pf(1.0),
		CostSteps:        pi(10),
		CostPartialSteps: pi(0),
		CostUnknownSteps: pi(0),
		TasksDone:        pi(2),
		Retries:          pi(0),
		ReviewRounds:     pi(0),
		SecurityRounds:   pi(0),
		QARounds:         pi(0),
		TotalFailedGates: pi(0),
		Escalations:      pi(0),
		UnverifiedSteps:  pi(0),
		VerifiedPass:     pi(5),
		VerifiedFail:     pi(0),
		Complete:         true,
		Source:           "recorder",
	}
	if mut != nil {
		mut(&o)
	}
	if _, _, err := s.RecordRetroOutcome(o, agents); err != nil {
		t.Fatalf("record %s/%s: %v", root, slug, err)
	}
}

func doneLoops(t *testing.T, s *store.Store, root string, n int, mut func(*store.RetroOutcome)) {
	t.Helper()
	for i := range n {
		loop(t, s, root, slugN(i, "l"), "done", nil, mut)
	}
}

func slugN(i int, p string) string { return p + string(rune('0'+i)) }

func cardOf(t *testing.T, s *store.Store, subj Subject, w Window) Card {
	t.Helper()
	c, err := Compute(context.Background(), depsOf(s), subj, w)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	return c
}

func mustCreate(t *testing.T, s *store.Store, in store.NewComponent) store.ComponentWrite {
	t.Helper()
	cw, err := s.CreateComponent(in)
	if err != nil {
		t.Fatalf("create %s: %v", in.Name, err)
	}
	if cw.Refusal != "" || cw.Component.ID == "" {
		t.Fatalf("create %s refused: %s / %s", in.Name, cw.Refusal, cw.Reason)
	}
	return cw
}

func componentCard(t *testing.T, s *store.Store, root string) Card {
	return cardOf(t, s, Subject{Kind: SubjectComponent, ID: root}, Window{})
}

// ---------- rule 3: state vocabulary ----------

func TestSmallSampleReadsTooFew(t *testing.T) {
	s := openFixture(t)
	doneLoops(t, s, "/r", 4, nil)
	c := componentCard(t, s, "/r")
	m := c.Metrics["doneRate"]
	if m.N != 4 || m.State != StateTooFew || m.Value != nil {
		t.Fatalf("n=4 doneRate = {N:%d State:%s Value:%v}, want 4/too-few/null — a rate from four runs is not a rate", m.N, m.State, m.Value)
	}
	doneLoops(t, s, "/r", 1, func(o *store.RetroOutcome) { o.TerminalKey = "done@extra"; o.LoopSlug = "extra" })
	c = componentCard(t, s, "/r")
	m = c.Metrics["doneRate"]
	if m.N != 5 || m.State != StateOk || m.Value == nil || *m.Value != 1.0 {
		t.Fatalf("n=5 doneRate = {N:%d State:%s}, want 5/ok/1", m.N, m.State)
	}
}

// ---------- rules 4 + P31/P32: the floor, never a zero ----------

func TestUnknownIsNeverZero(t *testing.T) {
	s := openFixture(t)
	doneLoops(t, s, "/r", 5, func(o *store.RetroOutcome) {
		o.CostUSDFloor = nil
		o.CostSteps = nil
		o.CostPartialSteps = nil
		o.CostUnknownSteps = nil
	})
	m := componentCard(t, s, "/r").Metrics["costPerCompletedItem"]
	if m.State != StateCannotSay || m.Value != nil {
		t.Fatalf("all-unknown cost = {%s %v}, want cannot-say/null — 0 would be a fabricated budget", m.State, m.Value)
	}
	// Five loops with a known floor among the unknowns: the floor
	// appears (partial, named) rather than the whole metric going dark.
	for i := range 5 {
		loop(t, s, "/r", "known"+string(rune('0'+i)), "done", nil, func(o *store.RetroOutcome) {
			*o.CostUSDFloor = 3.0
			*o.CostSteps = 10
			o.CostPartialSteps = pi(1)
		})
	}
	m = componentCard(t, s, "/r").Metrics["costPerCompletedItem"]
	if m.Value == nil || *m.Value != 3.0 || m.State != StatePartial {
		t.Fatalf("five-known cost = {%s %v}, want partial/3.0 — a stated floor with named unknowns", m.State, m.Value)
	}
}

func TestCostPerItemIsAFloorWithItsUnknowns(t *testing.T) {
	s := openFixture(t)
	doneLoops(t, s, "/r", 5, func(o *store.RetroOutcome) {
		*o.CostUSDFloor = 2.0
		*o.CostSteps = 10
		o.CostUnknownSteps = pi(2)
	})
	m := componentCard(t, s, "/r").Metrics["costPerCompletedItem"]
	if m.State != StatePartial {
		t.Fatalf("state %s, want partial (unknowns ride the value)", m.State)
	}
	want := history.Rollup{Total: 10.0, StepCount: 50, UnknownCount: 10, Floor: true}.Render()
	if got := m.Detail["rendered"]; got != want {
		t.Fatalf("rendered %q != history's renderer %q", got, want)
	}
	if m.Detail["unknownSteps"] != 10 || m.Detail["steps"] != 50 || m.Detail["partialSteps"] != 0 {
		t.Fatalf("detail counts %v", m.Detail)
	}
	if !strings.Contains(want, "at least") {
		t.Fatalf("history's own render lost its floor words: %q", want)
	}
}

// ---------- rule 1: latest per loop; ever-blocked ----------

func TestLatestOutcomePerLoopRule(t *testing.T) {
	s := openFixture(t)
	loop(t, s, "/r", "one", "blocked/stuck", nil, nil)
	loop(t, s, "/r", "one", "done", nil, nil) // the SAME loop later finished
	doneLoops(t, s, "/r", 4, nil)             // four clean loops so the counts can be STATED
	c := componentCard(t, s, "/r")
	if m := c.Metrics["loops"]; m.N != 5 {
		t.Fatalf("loops %d, want 5 — loop one's two rows are ONE unit", m.N)
	}
	if m := c.Metrics["finishedDone"]; m.Value == nil || *m.Value != 5 {
		t.Fatalf("finishedDone %v, want 5 — a double-count would say 6", m.Value)
	}
	if m := c.Metrics["blocked"]; m.Value == nil || *m.Value != 1 {
		t.Fatalf("blocked %v, want 1 — ever-blocked survives the later finish", m.Value)
	}
}

// ---------- rule 6: backfill rows cannot raise firstPassRate ----------

func TestFirstPassIgnoresBackfill(t *testing.T) {
	s := openFixture(t)
	loop(t, s, "/r", "j1", "done", nil, nil)
	loop(t, s, "/r", "j2", "done", nil, nil)
	for _, sl := range []string{"b3", "b4"} {
		loop(t, s, "/r", sl, "done", nil, func(o *store.RetroOutcome) {
			o.Source = "backfill"
			o.Complete = false
			o.Retries = nil
			o.ReviewRounds = nil
			o.SecurityRounds = nil
			o.QARounds = nil
			o.UnverifiedSteps = nil
			o.Escalations = nil
			o.WallMS = nil
		})
	}
	m := componentCard(t, s, "/r").Metrics["firstPassRate"]
	if m.N != 2 {
		t.Fatalf("firstPassRate n = %d, want 2 — the JUDGED loops; backfill NULLs cannot vote", m.N)
	}
	if m.State != StateTooFew {
		t.Fatalf("state %s — four loops, two judgable, the claim dies honestly", m.State)
	}
	if !strings.Contains(m.Note, "unreadable") {
		t.Fatalf("note %q should name the skipped rows", m.Note)
	}
}

// ---------- rule 5: median and p90, nearest rank ----------

func TestMedianNotMean(t *testing.T) {
	s := openFixture(t)
	walls := []int64{1, 1, 1, 1, 10000}
	for i, w := range walls {
		w := w
		loop(t, s, "/r", slugN(i, "w"), "done", nil, func(o *store.RetroOutcome) { o.WallMS = p64(w) })
	}
	m := componentCard(t, s, "/r").Metrics["wallPerItem"]
	if m.Value == nil || *m.Value != 1 {
		t.Fatalf("median %v, want 1 — the mean of this skew (2000.6) is nobody's loop", m.Value)
	}
	if got := m.Detail["p90"]; got != 10000.0 {
		t.Fatalf("p90 %v, want 10000 (nearest rank ceil(0.9·5) = 5th)", got)
	}
	if got := m.Detail["excludedNull"]; got != 0 {
		t.Fatalf("excludedNull %v, want 0 here", got)
	}
}

// ---------- rule 8: per-agent plane ----------

func TestAgentCardIsPerSlug(t *testing.T) {
	s := openFixture(t)
	for i := range 5 {
		ag := []store.RetroAgent{{Agent: "scout", AgentSlugKnown: true,
			Steps: pi(10), StepsRejected: pi(2), VerifyPass: pi(8), VerifyFail: pi(1),
			VerifyUnverified: pi(1), CostUSDFloor: pf(0.5)}}
		loop(t, s, "/r", slugN(i, "a"), "done", ag, nil)
	}
	c := cardOf(t, s, Subject{Kind: SubjectAgent, ID: "scout"}, Window{})
	if m := c.Metrics["stepsRun"]; m.N != 5 || m.State != StateOk || m.Value == nil || *m.Value != 50 {
		t.Fatalf("stepsRun {N:%d %s %v}, want 5/ok/50", m.N, m.State, m.Value)
	}
	if m := c.Metrics["stepRejectRate"]; m.Value == nil || *m.Value != 0.2 {
		t.Fatalf("stepRejectRate %v, want 10/50", m.Value)
	}
	if m := c.Metrics["verifyFailRate"]; m.Value == nil || *m.Value != 1.0/9.0 {
		t.Fatalf("verifyFailRate %v, want 5/45", m.Value)
	}
	if m := c.Metrics["costPerStep"]; m.Value == nil || *m.Value != 0.05 || m.Detail["rendered"] == nil {
		t.Fatalf("costPerStep %v %v, want floor 2.50/50 with rendering", m.Value, m.Detail)
	}
	// A foreign agent gets an empty card, not another agent's numbers.
	other := cardOf(t, s, Subject{Kind: SubjectAgent, ID: "nobody"}, Window{})
	if m := other.Metrics["stepsRun"]; m.State != StateCannotSay {
		t.Fatalf("unknown agent stepsRun %s, want cannot-say", m.State)
	}
}

func TestUnresolvedNameIsLabelled(t *testing.T) {
	s := openFixture(t)
	for i := range 6 {
		ag := []store.RetroAgent{{Agent: "Carlos's laptop", AgentSlugKnown: false,
			Steps: pi(4), VerifyPass: pi(2), VerifyFail: pi(0), VerifyUnverified: pi(0)}}
		loop(t, s, "/r", slugN(i, "u"), "done", ag, nil)
	}
	c := cardOf(t, s, Subject{Kind: SubjectAgent, ID: "Carlos's laptop"}, Window{})
	m := c.Metrics["loops"]
	if m.N != 6 {
		t.Fatalf("loops %d, want the six rows under the name", m.N)
	}
	if m.Note != "name, not slug" {
		t.Fatalf("note %q, want the exact label \"name, not slug\" — misattribution is the failure this note exists to prevent", m.Note)
	}
}

// ---------- rule 9: the tree ----------

func TestComponentRollupIncludesChildren(t *testing.T) {
	s := openFixture(t)
	root := mustCreate(t, s, store.NewComponent{Name: "Root", Slug: "root", Root: "/root"})
	c1 := mustCreate(t, s, store.NewComponent{Name: "Kid1", Slug: "kid1", Root: "/kid1", ParentID: root.Component.ID})
	c2 := mustCreate(t, s, store.NewComponent{Name: "Kid2", Slug: "kid2", Root: "/kid2", ParentID: root.Component.ID})
	doneLoops(t, s, "/root", 2, nil)
	doneLoops(t, s, "/kid1", 2, nil)
	loop(t, s, "/kid1", "b0", "blocked/stuck", nil, nil)
	doneLoops(t, s, "/kid2", 1, nil)

	roll := cardOf(t, s, Subject{Kind: SubjectComponent, ID: root.Component.ID, IncludeChildren: true}, Window{})
	if m := roll.Metrics["loops"]; m.Value == nil || *m.Value != 6 {
		t.Fatalf("rollup loops %v breakdown %+v, want 6 = 2+3+1, the sum of the three nodes", *m.Value, roll.Breakdown)
	}
	if m := roll.Metrics["doneRate"]; m.Value == nil || *m.Value != 5.0/6.0 {
		t.Fatalf("rollup doneRate %v, want 5/6 across the tree", m.Value)
	}
	if len(roll.Breakdown) != 2 {
		t.Fatalf("breakdown %d children, want 2", len(roll.Breakdown))
	}
	byRoot := map[string]BreakdownEntry{}
	for _, b := range roll.Breakdown {
		byRoot[b.Root] = b
	}
	if k := byRoot["/kid1"]; k.Loops != 3 || k.Done != 2 || k.Blocked != 1 {
		t.Fatalf("kid1 breakdown %+v, want {3,2,1}", k)
	}
	_ = c2
	if k := byRoot["/kid2"]; k.Loops != 1 {
		t.Fatalf("kid2 breakdown %+v", k)
	}
	// Counts only — a breakdown entry has no float field to rank with
	// (RT11): integers or nothing, structurally.
	rt := reflect.TypeOf(roll.Breakdown[0])
	for i := range rt.NumField() {
		if rt.Field(i).Type.Kind() == reflect.Float64 {
			t.Fatalf("breakdown field %s is a float — counts only, no scores", rt.Field(i).Name)
		}
	}
	_ = c1

	solo := cardOf(t, s, Subject{Kind: SubjectComponent, ID: root.Component.ID, IncludeChildren: false}, Window{})
	if m := solo.Metrics["loops"]; m.N != 2 {
		t.Fatalf("node-only loops n %d, want 2 (the node alone)", m.N)
	}
	if len(solo.Breakdown) != 0 {
		t.Fatalf("node-only card should carry no breakdown, got %d", len(solo.Breakdown))
	}
}

// ---------- rule 10: coverage on every card ----------

func TestCoverageOnEveryCard(t *testing.T) {
	s := openFixture(t)
	// Two loops in HISTORY (S25's projection), facts for one.
	for _, sl := range []string{"hist1", "hist2"} {
		if _, err := s.CreateLoop(store.NewLoop{Slug: sl, ComponentRoot: "/r", Status: "idle"}); err != nil {
			t.Fatal(err)
		}
	}
	loop(t, s, "/r", "hist1", "done", nil, nil)
	c := componentCard(t, s, "/r")
	if c.Coverage.LoopsInHistory < 2 {
		t.Fatalf("loopsInHistory %d, want both loops the history projection carries", c.Coverage.LoopsInHistory)
	}
	if c.Coverage.LoopsRecorded != 1 || c.Coverage.LoopsNotRecorded != c.Coverage.LoopsInHistory-1 {
		t.Fatalf("coverage %+v, want recorded 1 and the difference NAMED", c.Coverage)
	}
	if c.Coverage.RecordedSince == "" {
		t.Fatal("recordedSince empty — the card must say when recording started")
	}
	// An empty subject still gets a card: coverage and a sentence.
	empty := componentCard(t, s, "/nothing-here")
	if empty.Metrics["loops"].State != StateCannotSay || empty.Statement == "" || empty.Coverage.LoopsRecorded != 0 {
		t.Fatalf("empty subject card %+v, want cannot-say + statement, not error and not zeros", empty)
	}
	// `last` caps the metrics, never the honesty strip.
	doneLoops(t, s, "/cap", 6, nil)
	capped := cardOf(t, s, Subject{Kind: SubjectComponent, ID: "/cap"}, Window{LastN: 2})
	if capped.Coverage.LoopsRecorded != 6 {
		t.Fatalf("coverage loopsRecorded %d under last=2, want 6 — the cap trims the metrics, not the truth", capped.Coverage.LoopsRecorded)
	}
	if capped.Metrics["loops"].N != 2 {
		t.Fatalf("capped loops n %d, want the cap in the metrics", capped.Metrics["loops"].N)
	}
}

// ---------- rule 7: prerequisites cannot-say ----------

func TestPrerequisiteMetricsSayCannotSay(t *testing.T) {
	s := openFixture(t)
	doneLoops(t, s, "/r", 5, nil)
	c := componentCard(t, s, "/r")
	gate := c.Metrics["gatePassRate"]
	if gate.State != StateCannotSay || gate.Value != nil || !strings.Contains(gate.Note, "S57") {
		t.Fatalf("gatePassRate %+v, want cannot-say naming S57", gate)
	}
	rev := c.Metrics["mergedThenRevertedRate"]
	if rev.State != StateCannotSay || rev.Value != nil || !strings.Contains(rev.Note, "S57") {
		t.Fatalf("mergedThenRevertedRate %+v, want cannot-say naming S57", rev)
	}
}

// ---------- rule 11: compare is two reads ----------

func TestCompareIsTwoReads(t *testing.T) {
	s := openFixture(t)
	doneLoops(t, s, "/r", 5, nil)
	var windows []string
	orig := compute
	compute = func(ctx context.Context, d Deps, subj Subject, w Window) (Card, error) {
		windows = append(windows, w.Since+"/"+w.Until)
		return orig(ctx, d, subj, w)
	}
	t.Cleanup(func() { compute = orig })
	comp, err := Compare(context.Background(), depsOf(s), Subject{Kind: SubjectComponent, ID: "/r"},
		Window{Since: "2026-01-01T00:00:00Z", Until: "2026-02-01T00:00:00Z"},
		Window{Since: "2026-02-01T00:00:00Z", Until: "2026-03-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 2 || windows[0] == windows[1] {
		t.Fatalf("compute calls %v, want the SAME rules over the TWO windows", windows)
	}
	if comp.CardA.Window.Since != "2026-01-01T00:00:00Z" || comp.CardB.Window.Until != "2026-03-01T00:00:00Z" {
		t.Fatalf("cards must echo their own windows: %+v / %+v", comp.CardA.Window, comp.CardB.Window)
	}
	// The Comparison is exactly two cards and two windows: field by
	// field, no delta exists to fill (RT8: a person reads two numbers).
	rt := reflect.TypeOf(comp)
	for i := range rt.NumField() {
		if strings.Contains(strings.ToLower(rt.Field(i).Name), "delta") {
			t.Fatalf("Comparison field %s — a stored delta is exactly what RT8 forbids", rt.Field(i).Name)
		}
	}
}

// ---------- the two source-scan guards ----------

func TestNoWritesNoCache(t *testing.T) {
	files := pkgFiles(t)
	banned := []string{".w.", "Exec(", "Begin(", "INSERT INTO", "UPDATE ", "DELETE FROM", "func init(", "sync.Once", "map[string]Card"}
	for _, f := range files {
		src := mustRead(t, f)
		for _, b := range banned {
			if strings.Contains(src, b) {
				t.Errorf("%s contains %q — N8: the scorecard writes nothing and caches nothing", filepath.Base(f), b)
			}
		}
		// Package-level mutables: only the two named seams and the error
		// exist; a fresh `var` behind a caller is a cache with a new name.
		for _, m := range regexp.MustCompile(`(?m)^var\s+([A-Za-z_][\w]*)`).FindAllStringSubmatch(src, -1) {
			switch m[1] {
			case "compute", "gateResults", "ErrNotRecorded", "_":
			default:
				t.Errorf("package-level var %q in %s — the package holds NO state across calls", m[1], filepath.Base(f))
			}
		}
	}
}

func TestNoRankingExported(t *testing.T) {
	fnName := regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`)
	badWords := []string{"rank", "sort", "best", "worst", "leader", "score"}
	for _, f := range pkgFiles(t) {
		for _, m := range fnName.FindAllStringSubmatch(mustRead(t, f), -1) {
			name := m[1]
			if !unicode.IsUpper(rune(name[0])) {
				continue // unexported helpers cannot rank anything from outside
			}
			low := strings.ToLower(name)
			for _, w := range badWords {
				if strings.Contains(low, w) {
					t.Errorf("%s exports %q — ranking word %q: RT11, nothing here can be ranked", filepath.Base(f), name, w)
				}
			}
		}
	}
}

func pkgFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			out = append(out, e.Name())
		}
	}
	if len(out) < 5 {
		t.Fatalf("only %d source files — the census is too small to guard anything", len(out))
	}
	return out
}

func mustRead(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---------- the ladder identity + the blueprints ----------

func TestMilestonesMatchTheLearner(t *testing.T) {
	got, want := Milestones(), retro.Milestones
	if len(got) != len(want) {
		t.Fatalf("ladder lengths %d vs %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ladder[%d] = %d vs %d — the read side and the write side laddered apart", i, got[i], want[i])
		}
	}
}

func TestBlueprintCompletionSplitsVersions(t *testing.T) {
	s := openFixture(t)
	for i := range 5 {
		v := 1
		if i >= 3 {
			v = 2
		}
		kind := "done"
		if i == 2 {
			kind = "aborted"
		}
		vv := v
		loop(t, s, "/r", "bp"+string(rune('0'+i)), kind, nil, func(o *store.RetroOutcome) {
			o.BlueprintID = "feature"
			o.BlueprintVersion = &vv
			o.TerminalKey = kind + "@bp" + string(rune('0'+i))
		})
	}
	c := cardOf(t, s, Subject{Kind: SubjectBlueprint, ID: "feature"}, Window{})
	m := c.Metrics["completionByVersion"]
	if m.N != 5 || m.Value == nil || *m.Value != 0.8 {
		t.Fatalf("completion %v n=%d, want 4/5", m.Value, m.N)
	}
	tbl := m.Detail["byVersion"].(map[string]any)
	if v1 := tbl["1"].(map[string]any); v1["loops"] != 3 || v1["done"] != 2 {
		t.Fatalf("version 1 %v, want {3,2}", v1)
	}
	if v2 := tbl["2"].(map[string]any); v2["loops"] != 2 || v2["done"] != 2 {
		t.Fatalf("version 2 %v, want {2,2} — before/after never blend into one number pretending to be one", v2)
	}
}

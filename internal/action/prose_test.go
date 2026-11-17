package action

// T9's tests, written first. pr_summary is the ONE authored paragraph per
// PR (plus the review body); everything else is a template. The checks
// below are existence and shape — never quality — and the test that
// records that limit is as load-bearing as the checks themselves.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/runtime"
)

const goodSummary = "Adds GET /version, returning the daemon's build stamp from the " +
	"compile-time variable. The handler reads the runtime cache, so repeated calls " +
	"cost one map lookup; no database round-trip is involved."

// TestStubSummaryIsAReject (L21, D12, O4) — each refused case yields a
// reject naming the reason, routable by the blueprint. A hard failure
// would kill the loop over a paragraph the model can write again; the
// handler can be asked again, a failed loop cannot.
func TestStubSummaryIsAReject(t *testing.T) {
	title := "Add version endpoint"
	cases := []struct {
		name    string
		summary string
		want    string // what the reason must name
	}{
		{"empty", "", "empty"},
		{"whitespace", "   \n\t ", "empty"},
		{"placeholder echo", "{{pr_summary}}", "placeholder"},
		{"placeholder echo spaced", "{{ pr_summary }}", "placeholder"},
		{"below the floor", "Adds the endpoint.", "floor"},
		{"title restatement", title, "restat"},
		{"title plus filler", title + " — done", "restat"},
	}
	for _, c := range cases {
		r := CheckPRSummary(c.summary, title)
		if r == nil {
			t.Fatalf("%s: the stub passed — %q", c.name, c.summary)
		}
		if r.Outcome != runtime.OutcomeReject {
			t.Fatalf("%s: must be a ROUTABLE reject, not a failure (O4): %+v", c.name, r)
		}
		if !strings.Contains(strings.ToLower(r.Reason), c.want) {
			t.Fatalf("%s: the reason must name the case (%s): %q", c.name, c.want, r.Reason)
		}
		if r.Field != "pr_summary" {
			t.Fatalf("%s: names the field for S17's redo edges: %+v", c.name, r)
		}
	}
	// And the good paragraph sails through.
	if r := CheckPRSummary(goodSummary, title); r != nil {
		t.Fatalf("a real paragraph rejected: %+v", r)
	}
}

// TestQualityIsNotChecked — AN UNUSUAL TEST: it asserts a limitation (D12,
// C9, L21). A well-formed paragraph describing the WRONG change passes
// every check, and that is the honest limit. It is recorded here so
// nobody later mistakes the stub check for a quality gate — or adds a
// "quality score" and puts a number nobody should trust in front of
// something outward-facing. The control that exists for quality is the
// same one that exists for a handler's code: a human reads the PR, at
// the `ask` on forge.openPR.
func TestQualityIsNotChecked(t *testing.T) {
	wrong := "Introduces a pizza delivery module with geofenced courier assignment " +
		"and a loyalty discount engine. The pricing table is cache-backed and the " +
		"route optimizer is bounded per request. Tests cover the discount edge cases."
	if r := CheckPRSummary(wrong, "Add version endpoint"); r != nil {
		t.Fatalf("this check CANNOT know the paragraph describes a different change — asserting otherwise would be a lie about a quality gate (D12): %+v", r)
	}
	// A fluent lie passes too. Shape is all code can see.
	lie := "This change fixes the parsing order bug completely and all tests pass, " +
		"verified end to end with real traffic in production. The implementation " +
		"is minimal and surgical, touching no public surface whatsoever today."
	if r := CheckPRSummary(lie, "Add version endpoint"); r != nil {
		t.Fatalf("self-claims are prose, not evidence — the stub check does not and must not believe them: %+v", r)
	}
}

// TestThresholdsAreCommitted (Q9's answer) — the numbers live in code
// with a comment, and this test pins them, because a floor nobody wrote
// down gets adjusted until it passes.
//
// Q9 ANSWERED: minimum 120 RUNES (not bytes — non-English summaries are
// real), restatement slack 15 runes beyond a title-prefix match; the
// placeholder check compares against the committed templates' own token
// spellings. All three are exported consts asserted here:
func TestThresholdsAreCommitted(t *testing.T) {
	if PrSummaryMinRunes != 120 {
		t.Fatalf("Q9's floor is 120 runes, stated in the task record; changing it here without amending the record is the drift this test stops: %d", PrSummaryMinRunes)
	}
	if PrSummaryRestatementSlack != 15 {
		t.Fatalf("Q9's restatement slack is 15 runes: %d", PrSummaryRestatementSlack)
	}
	// The floor comment exists where the constant is declared (the source
	// is the record — grep-level evidence, not a doc promise).
	src, err := os.ReadFile("prose.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "120") || !strings.Contains(string(src), "runes") {
		t.Fatal("the floor must be STATED at its declaration, not just set")
	}
}

// TestOnlyPRReachingHandlersDeclareIt — a handler that never runs in a
// PR-producing lane declaring pr_summary is noise, and a
// declared-but-unused field is a field nobody maintains. The declaring
// bodies must match the committed set EXACTLY, both directions.
func TestOnlyPRReachingHandlersDeclareIt(t *testing.T) {
	dir := filepath.Join("..", "handler", "bodies")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bodyDeclaresPrSummary(string(raw)) {
			declared[strings.TrimSuffix(e.Name(), ".md")] = true
		}
	}
	want := PRProducingHandlers()
	if len(declared) != len(want) {
		t.Fatalf("declaring bodies %v vs the committed PR-producing set %v — both directions are findings", declared, want)
	}
	for name := range want {
		if !declared[name] {
			t.Fatalf("%s is a PR-producing handler that does NOT declare the paragraph its PR carries", name)
		}
	}
	for name := range declared {
		if !want[name] {
			t.Fatalf("%s declares pr_summary but never reaches a PR body — declared-but-unreachable is a finding", name)
		}
	}
}

func bodyDeclaresPrSummary(raw string) bool {
	fm := strings.SplitN(raw, "---", 3)
	if len(fm) < 3 {
		return false
	}
	inHandoff := false
	for _, line := range strings.Split(fm[1], "\n") {
		if strings.HasPrefix(line, "handoff:") {
			inHandoff = true
			continue
		}
		if inHandoff {
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				if strings.HasPrefix(strings.TrimSpace(line), "pr_summary:") {
					return true
				}
				continue
			}
			inHandoff = false
		}
	}
	return false
}

// TestRejectRoutesToRework (S8's L4) — the committed blueprint really
// routes a reject from the implementing step back through rework, and
// the redo round is CONSUMED (a bounded counter, not an infinite loop).
func TestRejectRoutesToRework(t *testing.T) {
	b, err := blueprint.LoadFile(filepath.Join("..", "blueprint", "blueprints", "dev-loop.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var build *blueprint.Lane
	for i := range b.Lanes {
		if b.Lanes[i].ID == "build" {
			build = &b.Lanes[i]
		}
	}
	if build == nil {
		t.Fatal("dev-loop's build lane is the one that carries the implement step")
	}
	if build.Rework != "implement" {
		t.Fatalf("a reject from the implementing work must route to rework:implement (O4: the handler can be asked again): %q", build.Rework)
	}
	if build.OnReject == nil || build.OnReject.Max <= 0 {
		t.Fatalf("the reject must be bounded — the redo round is consumed (S8's L4), not looped forever: %+v", build.OnReject)
	}
}

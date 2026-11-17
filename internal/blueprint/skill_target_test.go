package blueprint_test

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
)

// T0 — the S8 amendment: a step carries `skill:` and `params:`.
//
// The shape is the one the S14 design and DEMO imply: `skill: "<callee>/<skill-id>"`
// on a MODEL step whose `agent:` is the caller, with `params:` a free-form mapping
// the SKILL's own typed request (T2) validates — the blueprint layer checks the
// target, never the parameter contents. Three states, same discipline as the agent
// registry: checked when agent services are configured (SkillKnown answers),
// unverifiable with a named remedy when they are not, invalid when the target is
// unknown to a configured registry.

func skillLane(skill, agent, params string) string {
	head := `id: ask-reddit
schemaVersion: 1
version: 1
lanes:
  - id: survey
    step: {kind: model-step, agent: ` + agent
	if skill != "" {
		head += `, skill: "` + skill + `"`
	}
	if params != "" {
		head += `, params: {"query": "aeon", "max": 5}`
	}
	head += ", produces: OUT.md}\n"
	return head
}

func TestSkillTargetRoundTrips(t *testing.T) {
	bp, err := blueprint.Load([]byte(skillLane("reddit-agent/summarise-sentiment", "planner", "yes")))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	st := bp.Lanes[0].Step
	if st.Skill != "reddit-agent/summarise-sentiment" {
		t.Fatalf("skill lost: %q", st.Skill)
	}
	if st.Params["query"] != "aeon" {
		t.Fatalf("params lost: %#v", st.Params)
	}
	if got, _ := st.Params["max"].(int); got != 5 {
		t.Fatalf("params max: %#v", st.Params["max"])
	}
	// A lane without the fields behaves as before: nothing invented.
	bp2, err := blueprint.Load([]byte(skillLane("", "planner", "")))
	if err != nil {
		t.Fatalf("load plain: %v", err)
	}
	if bp2.Lanes[0].Step.Skill != "" || bp2.Lanes[0].Step.Params != nil {
		t.Fatalf("plain step grew fields: %#v", bp2.Lanes[0].Step)
	}
}

func TestSkillTargetRequiresAModelStepAgent(t *testing.T) {
	bp, err := blueprint.Load([]byte(skillLane("reddit-agent/summarise-sentiment", "planner", "")))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// gate + skill is a step-shape refusal.
	gateYaml := `id: g
schemaVersion: 1
version: 1
lanes:
  - id: g
    step: {kind: gate, question: q, skill: "reddit-agent/summarise-sentiment", on_reject: {goto: g}}
`
	_, err = blueprint.Load([]byte(gateYaml))
	if err != nil {
		// strict loader may legitimately reject a gate carrying skill at load;
		// either refusal is fine as long as it is REFUSED.
		return
	}
	_ = bp
	r := blueprint.Validate(mustLoad(t, gateYaml), blueprint.ValidationOptions{})
	if r.OK(false) {
		t.Fatal("gate + skill validated clean")
	}
	if !hasDiag(r, blueprint.Invalid, "step-shape") {
		t.Fatalf("want step-shape refusal, got %v", r.Diagnostics)
	}
}

func TestParamsWithoutSkillIsInvalid(t *testing.T) {
	y := `id: p
schemaVersion: 1
version: 1
lanes:
  - id: a
    step: {kind: model-step, agent: planner, produces: OUT.md, params: {"x": 1}}
`
	r := blueprint.Validate(mustLoad(t, y), blueprint.ValidationOptions{})
	if r.OK(false) || !hasDiag(r, blueprint.Invalid, "params-without-skill") {
		t.Fatalf("params without a skill target must be refused: %v", r.Diagnostics)
	}
}

func TestSkillShapeRefused(t *testing.T) {
	for _, bad := range []string{"reddit-agent", "/summarise", "reddit-agent/", "a/b/c"} {
		y := skillLane(bad, "planner", "")
		r := blueprint.Validate(mustLoad(t, y), blueprint.ValidationOptions{})
		if r.OK(false) || !hasDiag(r, blueprint.Invalid, "skill-shape") {
			t.Fatalf("skill %q must be refused as a shape error: %v", bad, r.Diagnostics)
		}
	}
}

func TestSkillUnverifiableWhenServicesAbsent(t *testing.T) {
	// SkillKnown nil: agent services are NOT configured on this process.
	// The honest answer is unverifiable naming the target AND the remedy —
	// never silently valid, never a fabricated invalid.
	r := validateWith(t, skillLane("reddit-agent/summarise-sentiment", "planner", ""), blueprint.ValidationOptions{
		Agents: []string{"planner"},
	})
	if !r.OK(false) {
		t.Fatalf("unconfigured services must not INVALIDATE the blueprint: %v", r.Diagnostics)
	}
	d := findDiag(r, blueprint.Unverifiable, "agentsvc")
	if d == nil {
		t.Fatalf("want an unverifiable agentsvc diagnostic: %v", r.Diagnostics)
	}
	for _, want := range []string{"reddit-agent/summarise-sentiment", "agent services"} {
		if !strings.Contains(d.Msg, want) {
			t.Fatalf("diagnostic must name %q, got %q", want, d.Msg)
		}
	}
}

func TestSkillKnownPassesAndUnknownIsInvalid(t *testing.T) {
	known := func(callee, skill string) bool { return callee == "reddit-agent" && skill == "summarise-sentiment" }
	y := skillLane("reddit-agent/summarise-sentiment", "planner", "yes")
	r := validateWith(t, y, blueprint.ValidationOptions{Agents: []string{"planner"}, SkillKnown: known})
	if !r.OK(false) {
		t.Fatalf("a registered skill target must validate clean: %v", r.Diagnostics)
	}
	if findDiag(r, blueprint.Unverifiable, "agentsvc") != nil {
		t.Fatal("a checked target must not report unverifiable")
	}
	// The params rode along and were NOT content-checked here (T2 owns that).
	r2 := validateWith(t, skillLane("reddit-agent/other", "planner", ""), blueprint.ValidationOptions{
		Agents: []string{"planner"}, SkillKnown: known,
	})
	if r2.OK(false) {
		t.Fatal("an unknown target on a CONFIGURED registry must be invalid")
	}
	d := findDiag(r2, blueprint.Invalid, "agentsvc")
	if d == nil || !strings.Contains(d.Msg, "reddit-agent/other") {
		t.Fatalf("unknown target must be named: %v", r2.Diagnostics)
	}
}

// --- helpers local to this file (kept out of the shared test helpers on purpose).

func mustLoad(t *testing.T, y string) blueprint.Blueprint {
	t.Helper()
	bp, err := blueprint.Load([]byte(y))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return bp
}

func validateWith(t *testing.T, y string, opts blueprint.ValidationOptions) blueprint.Report {
	t.Helper()
	return blueprint.Validate(mustLoad(t, y), opts)
}

func hasDiag(r blueprint.Report, state blueprint.DiagState, kind string) bool {
	for _, d := range r.Diagnostics {
		if d.State == state && d.Kind == kind {
			return true
		}
	}
	return false
}

func findDiag(r blueprint.Report, state blueprint.DiagState, kind string) *blueprint.Diagnostic {
	for i, d := range r.Diagnostics {
		if d.State == state && d.Kind == kind {
			return &r.Diagnostics[i]
		}
	}
	return nil
}

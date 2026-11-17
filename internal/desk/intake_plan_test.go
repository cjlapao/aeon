package desk

// intake_plan_test.go — S61 T5: the nineteenth kind's validator matrix, the
// closed-schema walk, and the generic-door refusal that IS the kind's apply
// half. The matrix is one arm per rule the GI7 schema states — each cap,
// each duplicate, each missing role, each dangling reference — because a
// malformed plan that survives create is a malformed plan a person commits
// blind, and the whole point of the desk reading it first is that nobody
// has to. The walk test is the schema's lock: the payload type carries no
// field that names a path, a credential or an identity, so a smuggled one
// is a PARSE failure at every door, not a discipline nobody can check.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

func validPlan() IntakePlanProposal {
	return IntakePlanProposal{
		IntakeID: "intake-1",
		Charter:  json.RawMessage(`{"goal":"ship the intake plane"}`),
		Components: []IntakePlanComponent{
			{Name: "Server", Slug: "server", Template: "go-service", Role: "root", Tags: []string{"go"}},
			{Name: "App", Slug: "app", Template: "react-app", Role: "child"},
		},
		ProjectName: "Intake Demo",
		Epics: []IntakePlanEpic{
			{Key: "core", Title: "Core", Objective: "the working core"},
		},
		Goals: []IntakePlanGoal{
			{EpicKey: "core", Title: "Stand it up", Type: "goal", ComponentSlugs: []string{"server", "app"},
				Criteria: []IntakePlanCriterion{{Text: "the server boots", Check: "command", Ref: "make run"}}},
		},
		Items: []IntakePlanItem{
			{ComponentSlug: "server", EpicKey: "core", Title: "lay the scaffold", Priority: "unknown"},
		},
		Reason: "a new project starts here",
	}
}

func planBytes(t *testing.T, p IntakePlanProposal) []byte {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestIntakePlanValidatorMatrix — one arm per rule; each names the sentence
// half it must come from.
func TestIntakePlanValidatorMatrix(t *testing.T) {
	if err := validateIntakePlan(validPlan()); err != nil {
		t.Fatalf("the base plan must pass: %v", err)
	}
	cases := []struct {
		name    string
		mutate  func(*IntakePlanProposal)
		mustHas string
	}{
		{"no-components", func(p *IntakePlanProposal) { p.Components = nil }, "between 1 and"},
		{"too-many-components", func(p *IntakePlanProposal) {
			for i := 2; i <= 8; i++ {
				p.Components = append(p.Components, IntakePlanComponent{Name: fmt.Sprintf("c%d", i), Slug: fmt.Sprintf("c-%d", i), Template: "t", Role: "child"})
			}
			p.Components = append(p.Components, IntakePlanComponent{Name: "c9", Slug: "c-9", Template: "t", Role: "child"})
		}, "between 1 and 8"},
		{"duplicate-slug", func(p *IntakePlanProposal) { p.Components[1].Slug = "server" }, "appears twice"},
		{"bad-slug", func(p *IntakePlanProposal) { p.Components[0].Slug = "Server_1" }, "kebab-case"},
		{"empty-template", func(p *IntakePlanProposal) { p.Components[0].Template = "" }, "names no template"},
		{"unknown-role", func(p *IntakePlanProposal) { p.Components[0].Role = "primary" }, "the closed set is"},
		{"root-without-child", func(p *IntakePlanProposal) {
			p.Components = []IntakePlanComponent{{Name: "A", Slug: "a", Template: "t", Role: "root"}}
			p.ProjectName = ""
		}, "come together"},
		{"child-without-root", func(p *IntakePlanProposal) { p.Components[0].Role = "standalone" }, "come together"},
		{"two-roots", func(p *IntakePlanProposal) {
			p.Components[1].Role = "root"
			p.Components = append(p.Components, IntakePlanComponent{Name: "C", Slug: "c", Template: "t", Role: "child"})
		}, "exactly one"},
		{"no-project-name", func(p *IntakePlanProposal) { p.ProjectName = "  " }, "needs a projectName"},
		{"dangling-epic", func(p *IntakePlanProposal) { p.Goals[0].EpicKey = "ghost" }, "no such epic"},
		{"duplicate-epic-key", func(p *IntakePlanProposal) {
			p.Epics = append(p.Epics, IntakePlanEpic{Key: "core", Title: "Second", Objective: "x"})
		}, "appears twice"},
		{"goal-without-components", func(p *IntakePlanProposal) { p.Goals[0].ComponentSlugs = nil }, "names no component"},
		{"goal-unknown-component", func(p *IntakePlanProposal) { p.Goals[0].ComponentSlugs = []string{"ghost"} }, "no such component"},
		{"bad-check-kind", func(p *IntakePlanProposal) { p.Goals[0].Criteria[0].Check = "pray" }, "is not one of manual, command, test"},
		{"item-unknown-component", func(p *IntakePlanProposal) { p.Items[0].ComponentSlug = "ghost" }, "no such component"},
		{"item-dangling-epic", func(p *IntakePlanProposal) { p.Items[0].EpicKey = "ghost" }, "no such epic"},
		{"no-reason", func(p *IntakePlanProposal) { p.Reason = "" }, "needs a reason"},
		{"long-reason", func(p *IntakePlanProposal) { p.Reason = strings.Repeat("w", 513) }, "the cap is 512"},
		{"no-intake-id", func(p *IntakePlanProposal) { p.IntakeID = " " }, "needs an intakeId"},
		{"no-goal-type", func(p *IntakePlanProposal) { p.Goals[0].Type = "" }, "names no type"},
	}
	for _, tc := range cases {
		p := validPlan()
		tc.mutate(&p)
		err := validateIntakePlan(p)
		if err == nil {
			t.Errorf("%s: the plan passed — the rule is missing", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.mustHas) {
			t.Errorf("%s: refusal %q does not name %q", tc.name, err, tc.mustHas)
		}
	}
}

func TestIntakePlanCapsAndEnvelope(t *testing.T) {
	p := validPlan()
	p.Epics = nil
	for i := 0; i <= 50; i++ {
		p.Epics = append(p.Epics, IntakePlanEpic{Key: fmt.Sprintf("k%d", i), Title: "t", Objective: "o"})
	}
	if err := validateIntakePlan(p); err == nil || !strings.Contains(err.Error(), "cap is 50") {
		t.Errorf("51 epics passed: %v", err)
	}
	p = validPlan()
	p.Epics = p.Epics[:1]
	p.Goals = nil
	for i := 0; i <= 200; i++ {
		p.Goals = append(p.Goals, IntakePlanGoal{Title: "t", Type: "goal", ComponentSlugs: []string{"server"}})
	}
	if err := validateIntakePlan(p); err == nil || !strings.Contains(err.Error(), "cap is 200") {
		t.Errorf("201 goals passed: %v", err)
	}
	p = validPlan()
	p.Goals = p.Goals[:1]
	p.Goals[0].Criteria = nil
	for i := 0; i <= 100; i++ {
		p.Goals[0].Criteria = append(p.Goals[0].Criteria, IntakePlanCriterion{Text: "c"})
	}
	if err := validateIntakePlan(p); err == nil || !strings.Contains(err.Error(), "cap is 100") {
		t.Errorf("101 criteria passed: %v", err)
	}
	p = validPlan()
	p.Goals[0].Criteria = p.Goals[0].Criteria[:1]
	p.Items = nil
	for i := 0; i <= 500; i++ {
		p.Items = append(p.Items, IntakePlanItem{ComponentSlug: "server", Title: "t"})
	}
	if err := validateIntakePlan(p); err == nil || !strings.Contains(err.Error(), "cap is 500") {
		t.Errorf("501 items passed: %v", err)
	}
	// The 128 KiB envelope is a byte rule, tasted at the payload door.
	huge := validPlan()
	huge.Components[0].Description = strings.Repeat("d", 140*1024)
	if err := validateIntakePlanPayload(context.Background(), planBytes(t, huge)); err == nil ||
		!strings.Contains(err.Error(), "cap is 131072") {
		t.Errorf("an over-envelope payload passed: %v", err)
	}
}

// TestIntakePlanPayloadRefusesSmuggledFields tastes the strict decode:
// every name GI7 keeps OUT of the plan is a parse refusal, not a field
// someone remembered to ignore.
func TestIntakePlanPayloadRefusesSmuggledFields(t *testing.T) {
	for _, extra := range []string{
		`"path":"/srv/where"`, `"targetPath":"/srv/where"`, `"token":"ghp_x"`,
		`"secret":"s"`, `"actor":{"kind":"user","id":"me"}`, `"publish":"public"`,
		`"owner":"someone"`, `"targetDir":"/x"`,
	} {
		raw := string(planBytes(t, validPlan()))
		bad := raw[:len(raw)-1] + "," + extra + "}"
		if err := validateIntakePlanPayload(context.Background(), []byte(bad)); err == nil ||
			!strings.Contains(err.Error(), "refused") {
			t.Errorf("smuggled %s passed the strict decode: %v", extra, err)
		}
	}
}

// TestIntakePlanCarriesNoPathSecretOrActor walks the payload TYPES, not the
// bytes: the reflection says the schema HAS no field whose name speaks of a
// path, a credential, an owner, a publisher or an actor, at any depth —
// the compile-time face of the strict-decode arms above.
func TestIntakePlanCarriesNoPathSecretOrActor(t *testing.T) {
	forbidden := []string{"path", "dir", "token", "secret", "credential", "password", "actor",
		"owner", "publish", "author", "committer", "identity", "remote", "url"}
	names := map[string]bool{}
	var walk func(reflect.Type)
	walk = func(tt reflect.Type) {
		for tt.Kind() == reflect.Slice || tt.Kind() == reflect.Ptr {
			tt = tt.Elem()
		}
		if tt.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < tt.NumField(); i++ {
			f := tt.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "" || tag == "-" {
				tag = f.Name
			}
			if names[tag] {
				continue
			}
			names[tag] = true
			lower := strings.ToLower(tag) + strings.ToLower(f.Name)
			for _, bad := range forbidden {
				if strings.Contains(lower, bad) {
					t.Errorf("field %q speaks of %q — the plan may not name it (GI7)", tag, bad)
				}
			}
			walk(f.Type)
		}
	}
	walk(reflect.TypeOf(IntakePlanProposal{}))
	for _, want := range []string{"intakeId", "charter", "components", "epics", "goals", "items", "reason", "componentSlug"} {
		if !names[want] {
			t.Errorf("the closed schema lost %q", want)
		}
	}
}

// TestGenericCommitRefusesIntakePlan is the refusal at the effect boundary,
// in both of its halves: AlreadyApplied answers NOT applied (there is no
// generic footprint to complete), and Apply answers the fixed sentence,
// byte for byte, whoever calls and through whatever seam map that registers
// ONLY Validate — because registering an Apply here would REPLACE the
// refusal the kind exists to give.
func TestGenericCommitRefusesIntakePlan(t *testing.T) {
	reg := New(Deps{}, Seams{KindIntakePlan: {Validate: func(context.Context, []byte) error { return nil }}})
	eff, ok := reg.Effect(string(KindIntakePlan))
	if !ok {
		t.Fatalf("intake_plan is not a complete effect: %s", reg.Missing(string(KindIntakePlan)))
	}
	applied, err := eff.AlreadyApplied(context.Background(), []byte("{}"))
	if err != nil || applied {
		t.Fatalf("AlreadyApplied = (%v,%v), want (false,nil) — an intake plan never applied generically", applied, err)
	}
	if err := eff.Apply(context.Background(), io.Discard, []byte("{}")); err == nil || err.Error() != IntakeCommitRefusal {
		t.Fatalf("Apply = %v, want the fixed sentence verbatim", err)
	}
	// The seam that forgets to register Apply still refuses in the same
	// words — the built-in half rides through the merge (desk.New merges,
	// never clears).
	bare := New(Deps{}, Seams{})
	if _, ok := bare.Effect(string(KindIntakePlan)); !ok {
		t.Fatalf("the un-seamed kind lost its built-in halves: %s", bare.Missing(string(KindIntakePlan)))
	}
}

// TestCharterSaveShapeHalf is the desk half of the eighteenth kind: the
// shape, the envelope, the reason rule — the charter's own body law and the
// root facts belong to the seam (charter_proposal_seams.go), which runs
// THIS function first, so these sentences are exactly what both doors hear
// first.
func TestCharterSaveShapeHalf(t *testing.T) {
	ctx := context.Background()
	ok := `{"rootId":"/srv/x","body":{"goal":"a charter"},"reason":"because"}`
	if err := validateCharterSavePayload(ctx, []byte(ok)); err != nil {
		t.Fatalf("the base payload must pass: %v", err)
	}
	for _, tc := range []struct{ name, payload, mustHas string }{
		{"trailing-json", ok + `{"rootId":"/x"}`, "trailing JSON"},
		{"unknown-actor", `{"rootId":"/srv/x","body":{"goal":"g"},"reason":"r","actor":{"kind":"user","id":"me"}}`, "carries no field besides them"},
		{"unknown-note", `{"rootId":"/srv/x","body":{"goal":"g"},"reason":"r","note":"mine"}`, "carries no field besides them"},
		{"unknown-source", `{"rootId":"/srv/x","body":{"goal":"g"},"reason":"r","source":"person"}`, "carries no field besides them"},
		{"no-root", `{"rootId":"  ","body":{"goal":"g"},"reason":"r"}`, "needs a rootId"},
		{"body-array", `{"rootId":"/x","body":[1,2],"reason":"r"}`, "must be a JSON object"},
		{"no-reason", `{"rootId":"/x","body":{"goal":"g"},"reason":""}`, "needs a reason"},
		{"long-reason", fmt.Sprintf(`{"rootId":"/x","body":{"goal":"g"},"reason":"%s"}`, strings.Repeat("w", 513)), "the cap is 512"},
	} {
		err := validateCharterSavePayload(ctx, []byte(tc.payload))
		if err == nil || !strings.Contains(err.Error(), tc.mustHas) {
			t.Errorf("%s: refusal %q does not name %q", tc.name, err, tc.mustHas)
		}
	}
	huge := `{"rootId":"/x","body":{"goal":"` + strings.Repeat("g", 140*1024) + `"},"reason":"r"}`
	if err := validateCharterSavePayload(ctx, []byte(huge)); err == nil ||
		!strings.Contains(err.Error(), "cap is 131072") {
		t.Errorf("the 128 KiB envelope leaked: %v", err)
	}
}

// TestSharedSlugRule: the ONE function — kebab-case in, anything else out.
func TestSharedSlugRule(t *testing.T) {
	for _, good := range []string{"a", "go-service", "a1-b2-c3", "server"} {
		if !ValidComponentSlug(good) {
			t.Errorf("%q must be a slug", good)
		}
	}
	for _, bad := range []string{"", "-", "a--b", "A-b", "a_b", "a-", "-a", "a b", "a.b"} {
		if ValidComponentSlug(bad) {
			t.Errorf("%q must not be a slug", bad)
		}
	}
}

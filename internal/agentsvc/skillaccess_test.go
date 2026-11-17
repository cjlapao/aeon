package agentsvc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// T7 — SkillAccess wiring (AS8). A2A ships `securitySchemes`; Aeon does NOT
// build a second authorization system to hold them. Per-skill security is
// expressed through S10's policy type — S10's authorship rule, S10's record,
// S10's refusal sentence — because a second mechanism is a second place for
// the authorship rule to drift, and the drift direction is the dangerous
// one: an ACL that grants is an allow nobody authored as a policy (MC4).

// skillAllow is how an operator's rule arrives in a test: the row shape
// policy.Store.Add would have written after the authorship check, authored
// `operator`, matching under every agent's request.
func skillAllow(ref string, action policy.Action) policy.Policy {
	return policy.Policy{
		ID: "allow-" + ref, Type: policy.SkillAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: action,
		Spec:   json.RawMessage(fmt.Sprintf(`{"skill":%q,"state":%q}`, ref, string(action))),
		Source: policy.Operator,
	}
}

func frozenAllow(refs ...string) []policy.Policy {
	var out []policy.Policy
	for _, r := range refs {
		out = append(out, skillAllow(r, policy.Allow))
	}
	return out
}

// invokeWith runs the standard chain ask under a caller-chosen frozen set —
// the policy door's test surface.
func (e *chainEnv) invokeWith(t *testing.T, caller, callee string, frozen []policy.Policy) (Task, *Reject) {
	t.Helper()
	return e.iv.Invoke(context.Background(), Invocation{
		CallerLoopID: e.loopSt.ID, CallerLane: "work", CallerAgent: caller,
		CallerStepID: runningStepAs(t, e.s, e.loopSt, caller),
		CalleeAgent:  callee, SkillID: "do-thing", Text: "asked", Mode: TaskAsync,
		Frozen: frozen, EvaluatedAt: time.Now(),
	})
}

func TestDenyingSkillAccessMakesSkillUninvokable(t *testing.T) {
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	ref := "reddit-agent/do-thing"

	// The allow alone is enough to walk through the door (proved elsewhere);
	// here a deny for the same ref rides alongside it.
	frozen := append(frozenAllow(ref), skillAllow(ref, policy.Deny))
	_, r := e.invokeWith(t, "planner", "reddit-agent", frozen)
	if r == nil {
		t.Fatal("deny outranks allow (PL4) — an ask that a rule refuses must not walk in on a second rule's permission")
	}
	if r.Field != "skillAccess" {
		t.Fatalf("the field must name the mechanism that refused: %q", r.Field)
	}
	if !strings.Contains(r.Detail, "skill_access") {
		t.Fatalf("the refusal must read as S10's answer about skill_access: %q", r.Detail)
	}
	// The refusal left nothing behind (the policy door runs before the edge).
	if edges, err := e.s.ChainEdgesByCallerAgent("planner"); err != nil || len(edges) != 0 {
		t.Fatalf("a policy refusal must leave no chain trace: %v", edges)
	}
}

func TestRefusalCarriesPolicySentence(t *testing.T) {
	// One formatter (S10): the CLI, the app and a transcript all say the
	// same thing, so the invoke refusal must carry Verdict.Sentence()
	// VERBATIM — not a paraphrase, not a prefix.
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	ref := "reddit-agent/do-thing"
	frozen := append(frozenAllow(ref), skillAllow(ref, policy.Deny))
	_, r := e.invokeWith(t, "planner", "reddit-agent", frozen)
	if r == nil {
		t.Fatal("the deny must refuse")
	}
	want := policy.Evaluate(frozen, policy.Request{
		Type: policy.SkillAccess, Agent: "planner", Skill: ref,
	}, time.Now()).Sentence()
	if !strings.Contains(r.Detail, want) {
		t.Fatalf("the refusal must carry S10's sentence verbatim.\n sentence: %q\n refusal:  %q", want, r.Detail)
	}
}

func TestCallerAndCalleeAreBothJudged(t *testing.T) {
	// Scope 1: caller, callee, skill. A deny that only matches the caller's
	// request still refuses the invocation — the callee's permission is not
	// the caller's to spend.
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	ref := "reddit-agent/do-thing"
	frozen := append(frozenAllow(ref), policy.Policy{
		ID: "deny-planner-asks", Type: policy.SkillAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "planner"},
		Action: policy.Deny,
		Spec:   json.RawMessage(fmt.Sprintf(`{"skill":%q,"state":"deny"}`, ref)),
		Source: policy.Operator,
	})
	_, r := e.invokeWith(t, "planner", "reddit-agent", frozen)
	if r == nil || r.Field != "skillAccess" {
		t.Fatalf("the caller is judged too: %v", r)
	}
	if !strings.Contains(r.Detail, "planner") {
		t.Fatalf("the sentence must name who was refused (PL8): %q", r.Detail)
	}
}

func TestSkillWithNoAllowingPolicyIsUninvokable(t *testing.T) {
	// PL5: the empty-registry state is refusal, not permission. A fresh
	// daemon with no skill_access rows lets nothing through — and says the
	// default-deny sentence, which is a different sentence from a rule deny.
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	_, r := e.invokeWith(t, "planner", "reddit-agent", nil)
	if r == nil || r.Field != "skillAccess" {
		t.Fatalf("no allowing policy means no invocation: %v", r)
	}
	if !strings.Contains(r.Detail, "no policy") && !strings.Contains(strings.ToLower(r.Detail), "default") {
		t.Fatalf("the empty set must read as the default deny, not as a rule that refused: %q", r.Detail)
	}
}

func TestCardsSecuritySchemesCannotGrant(t *testing.T) {
	// A third-party card declaring `securitySchemes` (and per-skill
	// `security`) says it is OPEN. It does not open anything. The A2A
	// spec's natural reading is the opposite, so this is said in three
	// parts: the declaration is READ (the card still loads), it is IGNORED
	// (there is no field to land on), and the POLICY still refuses.
	dir := t.TempDir()
	cardsDir := filepath.Join(dir, "cards")
	if err := os.MkdirAll(cardsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"reddit-agent","description":"reads reddit","version":"1.0.0",
	  "defaultInputModes":["text/plain"],"defaultOutputModes":["text/plain"],
	  "securitySchemes":{"google":{"type":"oauth2","flows":{"implicit":{"authorizationUrl":"https://example"}}}},
	  "skills":[{"id":"do-thing","name":"Do the thing","description":"the thing","security":[{"google":[]}]}]}`
	sc, err := Sign([]byte(body), testKey())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cardsDir, "reddit-agent.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// (1) The card loads — a hint is read, not rejected; a daemon that
	// could not parse the world's cards could not enumerate them either.
	card, err := OpenCardStore(cardsDir).Card("reddit-agent")
	if err != nil || card == nil {
		t.Fatalf("a card declaring securitySchemes must still load: %v %v", card, err)
	}
	// (2) Wherever the declaration lands, it lands as OPAQUE BYTES (T1's
	// recorded decision, and the only shape compatible with hint-not-
	// authority): not a bool somebody can branch on, not a scheme struct
	// somebody can honour, and read by NOTHING in the package. A field that
	// can only hold bytes nobody parses is a hint; the moment somebody
	// parses it, this test's second half catches the drift — the refusal
	// below must survive it.
	for _, decl := range []struct {
		what string
		typ  reflect.Type
	}{
		{"AgentCard", reflect.TypeOf(*card)},
		{"AgentSkill", reflect.TypeOf(AgentSkill{})},
	} {
		for i := 0; i < decl.typ.NumField(); i++ {
			f := decl.typ.Field(i)
			if !strings.Contains(strings.ToLower(f.Name), "security") {
				continue
			}
			if f.Type != reflect.TypeOf(json.RawMessage(nil)) {
				t.Fatalf("%s carries %q as %s — a parseable security shape is an authority waiting to be read", decl.what, f.Name, f.Type)
			}
		}
	}
	// Nothing outside card.go's declaration touches the field: an opaque
	// thing that is never read cannot grant anything.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".go") || strings.HasSuffix(en.Name(), "_test.go") || en.Name() == "card.go" {
			continue
		}
		raw, err := os.ReadFile(en.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "SecuritySchemes") {
			t.Fatalf("%s reads SecuritySchemes — the hint stays a hint only while nobody parses it", en.Name())
		}
	}
	// (3) The policy still refuses: a self-declared-open skill with no
	// allowing policy is uninvokable.
	s, sdir := taskTestStore(t)
	stLoop, _ := startTaskLoop(t, s, "hint-loop")
	_ = sdir
	iv := NewInvoker(s, OpenCardStore(cardsDir))
	_, r := iv.Invoke(context.Background(), Invocation{
		CallerLoopID: stLoop.ID, CallerLane: "work", CallerAgent: "planner",
		CallerStepID: runningStepAs(t, s, stLoop, "planner"),
		CalleeAgent:  "reddit-agent", SkillID: "do-thing", Text: "x",
		Mode: TaskAsync, Frozen: nil, EvaluatedAt: time.Now(),
	})
	if r == nil || r.Field != "skillAccess" {
		t.Fatalf("a card that declares itself open is open only in the card: %v", r)
	}
}

func TestExactlyOneAuthorizationMechanism(t *testing.T) {
	// Scope 2, asserted by grep: internal/agentsvc holds NO authorization
	// state. No ACL, no allowlist, no grant table, no package-level anything
	// — the only path to a permission decision is policy.Evaluate over the
	// frozen set.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{"acl", "whitelist", "allowlist", "allow-list", "deny-list", "grant", "authoriz"}
	evals := 0
	for _, en := range entries {
		name := en.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			// Comments may SAY the forbidden word to forbid it — the grep
			// guards state, not vocabulary. Code is what must stay clean.
			code := line
			if j := strings.Index(code, "//"); j >= 0 {
				code = code[:j]
			}
			lower := strings.ToLower(code)
			for _, b := range banned {
				if strings.Contains(lower, b) {
					t.Fatalf("%s:%d carries %q in code — authorization state in this package is a second authorship rule waiting to drift (MC4)", name, i+1, b)
				}
			}
		}
		if regexp.MustCompile(`(?m)^var\s`).MatchString(string(raw)) {
			t.Fatalf("%s declares package-level state — agentsvc decides nothing it can remember", name)
		}
		if strings.Contains(string(raw), "policy.Evaluate(") {
			evals++
		}
	}
	if evals != 1 {
		t.Fatalf("exactly one file may ask the policy engine (invoke.go); found %d — a second caller is a second judgement point", evals)
	}
}

func TestNoClockNoJudgement(t *testing.T) {
	// The policy set is judged `at` an instant (expiry, D12). An invocation
	// that supplies no clock cannot be judged against a set whose expiry is
	// unknowable — refusal names the thing refused, including when the
	// missing thing is a timestamp.
	e := newChainEnv(t)
	e.offer(t, "planner")
	e.offer(t, "reddit-agent")
	_, r := e.iv.Invoke(context.Background(), Invocation{
		CallerLoopID: e.loopSt.ID, CallerLane: "work", CallerAgent: "planner",
		CallerStepID: runningStepAs(t, e.s, e.loopSt, "planner"),
		CalleeAgent:  "reddit-agent", SkillID: "do-thing", Text: "x",
		Mode: TaskAsync, Frozen: frozenAllow("reddit-agent/do-thing"),
	})
	if r == nil || r.Field != "evaluatedAt" {
		t.Fatalf("no clock, no judgement, and a refusal that says so: %v", r)
	}
}

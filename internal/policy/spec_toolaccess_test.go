package policy_test

// ToolAccess — the first real spec, tested as the shape every later type copies.
//
// The claims here, in order: the spec is the pinned object and nothing else
// (an unknown field is refused with its name, an empty or missing toolName is
// refused, a state that contradicts the row's action is refused naming both);
// the demo's §3 holds through the real type (a tool granted to one agent is
// refused by an all-agents deny, D1); matching is exact (a prefix is not a
// match, a different case is not a match); the evaluator stays total on rows
// the validator would have refused; and the default blocklist is empty — the
// only denies are the ones somebody authored.
//
// Every policy row is built with the raw spec bytes the test names, because the
// spec is what these tests are about; targets still come through ParseTarget so
// a helper cannot invent an illegal one (T2's convention, reused not reinvented).

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

// ── fixtures ─────────────────────────────────────────────────────────────────

// specPolicy builds one stored-looking row of a production type with the exact
// spec bytes a test names. Source is operator: who authored a row is the write
// boundary's question (T6/PL1), not this file's.
func specPolicy(tb testing.TB, typ policy.PolicyType, id string, kind policy.TargetKind,
	targetID string, action policy.Action, spec string) policy.Policy {
	tb.Helper()
	target, rej := policy.ParseTarget(kind, targetID)
	if rej != nil {
		tb.Fatalf("helper built an illegal target %q/%q: %s", kind, targetID, rej.Reason)
	}
	return policy.Policy{
		ID: id, Type: typ, Target: target, Action: action,
		Spec: json.RawMessage(spec), Source: policy.Operator,
	}
}

// toolSpec renders the ToolAccess spec through the exported struct, so the
// accepted shapes in these tables are produced by the schema itself rather than
// by a string that could drift from it. (The REFUSED shapes are literals —
// that is the whole point of them.)
func toolSpec(tb testing.TB, toolName, state string) string {
	tb.Helper()
	raw, err := json.Marshal(policy.ToolAccessSpec{ToolName: toolName, State: state})
	if err != nil {
		tb.Fatalf("marshal the spec: %v", err)
	}
	return string(raw)
}

// toolReq is the question the ToolAccess evaluator is asked.
func toolReq(agent, tool string) policy.Request {
	return policy.Request{Type: policy.ToolAccess, Agent: agent, Tool: tool}
}

// validateWith asks the registered validator — the one T6 will call, never a
// locally constructed one, so the test proves the registration as it goes.
func validateWith(tb testing.TB, typ policy.PolicyType, p policy.Policy) *policy.Reject {
	tb.Helper()
	v, ok := policy.ValidatorFor(typ)
	if !ok {
		tb.Fatalf("%s has no validator registered; the write boundary has nothing to check", typ)
	}
	return v.Validate(p)
}

// toolPolicy builds a ToolAccess row; spec is the raw JSON to store.
func toolPolicy(tb testing.TB, id string, kind policy.TargetKind, targetID string,
	action policy.Action, spec string) policy.Policy {
	tb.Helper()
	return specPolicy(tb, policy.ToolAccess, id, kind, targetID, action, spec)
}

// toolAccessShapeText is the shape every ToolAccess refusal quotes, spelled here
// rather than imported so a failing run shows the operator's sentence. The
// schema lock in spec_stubs_test.go pins it against the struct.
const toolAccessShapeText = `{"toolName": string, "state": "allow"|"deny"|"ask"}`

// ── the spec is the pinned object, and nothing else ─────────────────────────

func TestAToolAccessSpecIsExactlyThePinnedShape(t *testing.T) {
	// Every row is the same policy with one field wrong, and the refusal has to
	// say which. The lenient alternative — ignore the unexpected, default the
	// missing — turns a typo into a policy that matches nothing, and a policy
	// that matches nothing is read by the engine exactly as a policy that
	// permits everything is read: as nothing happening. That is the failure
	// this table exists to make impossible.
	cases := []struct {
		name    string
		spec    string
		action  policy.Action
		wantErr string // "" means the spec must be accepted
	}{
		{"the shape itself, allowing", `{"toolName":"bash","state":"allow"}`, policy.Allow, ""},
		{"the shape itself, denying", `{"toolName":"bash","state":"deny"}`, policy.Deny, ""},
		{"whitespace around the object is not content", "  \n" + `{"toolName":"bash","state":"deny"}` + "\t ", policy.Deny, ""},
		{"a tool name with spaces is a name, not padding", `{"toolName":"git push","state":"allow"}`, policy.Allow, ""},

		{"an unknown field", `{"tool":"bash","state":"allow"}`, policy.Allow, `"tool"`},
		{"an unknown field spelled near a known one", `{"toolName":"bash","stat":"allow"}`, policy.Allow, `"stat"`},
		{"a field added by hope", `{"toolName":"bash","state":"allow","glob":"ba*"}`, policy.Allow, `"glob"`},
		{"an empty object", `{}`, policy.Allow, `"toolName"`},
		{"an empty state", `{"toolName":"bash","state":""}`, policy.Allow, `"state"`},
		{"a state that is only a space", `{"toolName":"bash","state":" "}`, policy.Allow, `"state"`},
		{"a tool name that is only a space", `{"toolName":"  ","state":"allow"}`, policy.Allow, `"toolName"`},
		{"a wrong enum value", `{"toolName":"bash","state":"permit"}`, policy.Allow, `"state"`},
		{"a state of the wrong JSON type", `{"toolName":"bash","state":true}`, policy.Allow, "state"},
		{"a tool name of the wrong JSON type", `{"toolName":5,"state":"allow"}`, policy.Allow, "toolName"},
		{"a spec that is not an object", `"bash"`, policy.Allow, "JSON object"},
		{"a spec that is an array", `["bash"]`, policy.Allow, "JSON object"},
		{"a spec that is null", `null`, policy.Allow, "null"},
		{"a spec that is empty bytes", ``, policy.Allow, "empty"},
		{"trailing content after the object", `{"toolName":"bash","state":"allow"} {"toolName":"rm","state":"deny"}`,
			policy.Allow, "trailing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := toolPolicy(t, "row", policy.AgentKind, "main", c.action, c.spec)
			rej := validateWith(t, policy.ToolAccess, p)
			if c.wantErr == "" {
				if rej != nil {
					t.Fatalf("the validator refused a spec that is exactly the pinned shape: %s", rej.Reason)
				}
				return
			}
			if rej == nil {
				t.Fatalf("the validator accepted %s (%s); it must refuse", c.name, c.spec)
			}
			if !strings.Contains(rej.Reason, c.wantErr) {
				t.Errorf("the refusal %q does not name %q; a refusal has to say what it refused", rej.Reason, c.wantErr)
			}
			if !strings.Contains(rej.Reason, string(policy.ToolAccess)) {
				t.Errorf("the refusal %q does not say which type refused it", rej.Reason)
			}
		})
	}
}

func TestAnUnknownFieldInTheSpecIsRefused(t *testing.T) {
	// Named by the task because it is the case a lenient decoder gets wrong
	// silently: {"tool": "bash"} is what an operator types when they mean
	// {"toolName": "bash"}, and under a permissive reader it decodes into a
	// row whose ToolName is "" — a row that matches nothing while LOOKING like
	// a grant. The refusal must carry the offending field name, or the operator
	// is left reading the schema to find their own typo.
	p := toolPolicy(t, "typo", policy.AgentKind, "main", policy.Allow, `{"tool":"bash","state":"allow"}`)
	rej := validateWith(t, policy.ToolAccess, p)
	if rej == nil {
		t.Fatal(`{"tool":"bash"} was accepted; an unknown field must be refused, not ignored`)
	}
	if !strings.Contains(rej.Reason, strconv.Quote("tool")) {
		t.Errorf("the refusal %q does not name the field it refused", rej.Reason)
	}
	if !strings.Contains(rej.Reason, "did you mean") {
		t.Errorf("the refusal %q does not suggest the key the typo clearly meant", rej.Reason)
	}
	// The suggestion is a kindness, not a second spelling that decodes: the
	// near-miss key is still refused on its own merits.
	near := toolPolicy(t, "near", policy.AgentKind, "main", policy.Allow, `{"toolName":"bash","states":"allow"}`)
	if rej := validateWith(t, policy.ToolAccess, near); rej == nil ||
		!strings.Contains(rej.Reason, strconv.Quote("states")) {
		t.Errorf(`"states" must be refused by name; got %v`, rej)
	}
	// A wild card gets no guess: a wrong suggestion costs more than none.
	wild := toolPolicy(t, "wild", policy.AgentKind, "main", policy.Allow, `{"zzz":"bash","state":"allow"}`)
	if rej := validateWith(t, policy.ToolAccess, wild); rej == nil || strings.Contains(rej.Reason, "did you mean") {
		t.Errorf(`an unrecognisable key must not be guessed at; got %v`, rej)
	}
	// Both fields unknown: both named, and in a stable order — a refusal that
	// changes between runs is a refusal nobody can pin in a test or a UI.
	both := toolPolicy(t, "both", policy.AgentKind, "main", policy.Allow, `{"zebra":"z","aaa":"a"}`)
	rej2 := validateWith(t, policy.ToolAccess, both)
	if rej2 == nil {
		t.Fatal("a spec of nothing but unknown fields was accepted")
	}
	if i, j := strings.Index(rej2.Reason, "aaa"), strings.Index(rej2.Reason, "zebra"); i < 0 || j < 0 || i > j {
		t.Errorf("both unknown fields must be named, in a stable order: %s", rej2.Reason)
	}
}

func TestAnEmptyOrMissingToolNameIsRefused(t *testing.T) {
	// The other half of the same failure: an absent or empty toolName is the
	// zero value a lenient decode lands on, and an empty name compared against
	// an unset Request.Tool is a grant to every request that names no tool.
	// Refusing it at the write boundary is what lets the evaluator's own
	// empty-name guard be belt-and-braces rather than the only defence.
	for _, c := range []struct {
		spec    string
		wantErr string
	}{
		// Absent and blank are refused for two different reasons, and the
		// messages say which: one is a field the author forgot, the other is a
		// value that names nothing.
		{`{"state":"allow"}`, "left out"},
		{`{"toolName":"","state":"allow"}`, "permits everything"},
		{`{"toolName":"   ","state":"allow"}`, "permits everything"},
	} {
		p := toolPolicy(t, "empty-name", policy.AgentKind, "main", policy.Allow, c.spec)
		rej := validateWith(t, policy.ToolAccess, p)
		if rej == nil {
			t.Errorf("%s was accepted; an empty or missing toolName names no tool and must be refused", c.spec)
			continue
		}
		if !strings.Contains(rej.Reason, `"toolName"`) {
			t.Errorf("the refusal %q does not name toolName", rej.Reason)
		}
		if !strings.Contains(rej.Reason, c.wantErr) {
			t.Errorf("the refusal %q does not say %q; a refusal has to name the failure it is answering",
				rej.Reason, c.wantErr)
		}
		if !strings.Contains(rej.Reason, toolAccessShapeText) {
			t.Errorf("the refusal %q does not quote the shape that was wanted", rej.Reason)
		}
	}
	// And the belt: a row that reached the table anyway (hand-written SQL, or
	// stored before this validator existed) matches nothing — including a
	// request whose tool is also empty.
	empty := toolPolicy(t, "stored-empty", policy.AgentKind, "main", policy.Allow, `{"toolName":"","state":"allow"}`)
	v := policy.Evaluate([]policy.Policy{empty}, policy.Request{Type: policy.ToolAccess, Agent: "main"}, evalAt)
	assertVerdict(t, v, false, true, false, "", "")
}

func TestTheSpecStateMustAgreeWithTheAction(t *testing.T) {
	// state is the spec's copy of the row's action, and two copies of one fact
	// are one fact too many if they may disagree: a row saying "allow" inside
	// and deny outside is a policy whose author meant one thing and stored the
	// other. The refusal names both sides, because the fix is to change one of
	// them and "inconsistent" does not say which.
	contradictions := []struct {
		name   string
		spec   string
		action policy.Action
	}{
		{"the spec allows, the row denies", `{"toolName":"bash","state":"allow"}`, policy.Deny},
		{"the spec denies, the row allows", `{"toolName":"bash","state":"deny"}`, policy.Allow},
	}
	for _, c := range contradictions {
		t.Run(c.name, func(t *testing.T) {
			p := toolPolicy(t, "contradiction", policy.AgentKind, "main", c.action, c.spec)
			rej := validateWith(t, policy.ToolAccess, p)
			if rej == nil {
				t.Fatalf("a contradictory row was accepted: spec %s action %q", c.spec, c.action)
			}
			for _, want := range []string{`"state"`, strconv.Quote(string(c.action))} {
				if !strings.Contains(rej.Reason, want) {
					t.Errorf("the refusal %q does not name %s", rej.Reason, want)
				}
			}
		})
	}

	t.Run("a row whose action escaped the closed set decides nothing", func(t *testing.T) {
		// The validator is handed stored rows, and a row that skipped every
		// guard has no action for a state to agree with. Refuse, don't guess.
		p := toolPolicy(t, "no-action", policy.AgentKind, "main", policy.Action("permit"),
			`{"toolName":"bash","state":"allow"}`)
		rej := validateWith(t, policy.ToolAccess, p)
		if rej == nil || !strings.Contains(rej.Reason, "permit") {
			t.Fatalf("action %q must be refused by name; got %v", "permit", rej)
		}
	})

	t.Run("a row the validator refuses cannot decide anything", func(t *testing.T) {
		// The evaluator's share of the same rule: a contradictory row is inert,
		// not read as whichever half happens to be checked first.
		contrary := toolPolicy(t, "contrary", policy.AgentKind, "main", policy.Deny,
			`{"toolName":"bash","state":"allow"}`)
		v := policy.Evaluate([]policy.Policy{contrary}, toolReq("main", "bash"), evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	})
}

// ── the demo's §3, through the real type (D1) ────────────────────────────────

func TestAGrantedToolIsRefusedByAnAllAgentsDeny(t *testing.T) {
	// The slice's thesis, run through ToolAccess rather than a test-only type:
	// the most specific allow loses to the broadest deny, and the verdict says
	// so by naming the grant it overrode. "Most specific wins" is the intuitive
	// rule and the wrong one — it is the union-of-permissions escalation this
	// design closes, so this test is the one that has to fail for the whole
	// slice to be worth building.
	grant := toolPolicy(t, "grant-bash-to-go", policy.AgentKind, "golang-engineer-loop", policy.Allow,
		toolSpec(t, "bash", "allow"))
	ban := toolPolicy(t, "no-shell-anywhere", policy.AllAgents, "", policy.Deny,
		toolSpec(t, "bash", "deny"))
	req := toolReq("golang-engineer-loop", "bash")

	for name, set := range map[string][]policy.Policy{
		"grant written first": {grant, ban},
		"deny written first":  {ban, grant},
	} {
		t.Run(name, func(t *testing.T) {
			v := policy.Evaluate(set, req, evalAt)
			assertVerdict(t, v, false, false, false, ban.ID, grant.ID)
			if v.By.Target.Kind != policy.AllAgents {
				t.Errorf("the deny the verdict names targets %q; the broad rule is the one that decided",
					v.By.Target.Kind)
			}
			s := v.Sentence()
			for _, want := range []string{"grant-bash-to-go", "no-shell-anywhere", "golang-engineer-loop"} {
				if !strings.Contains(s, want) {
					t.Errorf("the sentence %q does not carry %q; the override has to be readable from the "+
						"one sentence every surface prints", s, want)
				}
			}
		})
	}

	t.Run("the same grant stands when the ban names a different tool", func(t *testing.T) {
		// The control: the denial above is about the tool, not about the agent.
		// Change the tool the ban names and the grant is simply an allow.
		other := toolPolicy(t, "no-rm-anywhere", policy.AllAgents, "", policy.Deny, toolSpec(t, "rm", "deny"))
		v := policy.Evaluate([]policy.Policy{grant, other}, req, evalAt)
		assertVerdict(t, v, true, false, false, grant.ID, "")
	})

	t.Run("an all-agents allow still permits", func(t *testing.T) {
		// Deny is absolute; allow is not specific. A broad allow is still an
		// allow — nothing in D1 makes an allow need to be narrow to work.
		broad := toolPolicy(t, "bash-for-everyone", policy.AllAgents, "", policy.Allow, toolSpec(t, "bash", "allow"))
		v := policy.Evaluate([]policy.Policy{broad}, req, evalAt)
		assertVerdict(t, v, true, false, false, broad.ID, "")
	})
}

func TestTheToolNameMatchesExactlyNotByPrefixOrCase(t *testing.T) {
	// No globs and no prefixes: scope is what the target dimension is for, and
	// a second scoping language inside the spec is one nobody audits. So "ba"
	// governs a tool called `ba` — and nothing at all, since there is no such
	// tool — rather than becoming a wildcard over `bash`.
	cases := []struct {
		name     string
		specTool string
		reqTool  string
	}{
		{"an exact name matches", "bash", "bash"},
		{"a prefix of it does not", "ba", "bash"},
		{"a longer name does not", "bash-git", "bash"},
		{"case is part of the name", "Bash", "bash"},
		{"the request's case is part of the name too", "bash", "Bash"},
		{"a trailing space is not the same name", "bash ", "bash"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := toolPolicy(t, "row", policy.AgentKind, "main", policy.Allow, toolSpec(t, c.specTool, "allow"))
			matches := c.specTool == c.reqTool
			byID := ""
			if matches {
				byID = row.ID
			}
			v := policy.Evaluate([]policy.Policy{row}, toolReq("main", c.reqTool), evalAt)
			assertVerdict(t, v, matches, !matches, false, byID, "")
		})
	}

	t.Run("a name that is a glob is a literal name", func(t *testing.T) {
		// The operator who writes "*" expects everything; the engine reads a
		// tool called "*". The refusal to interpret is the point — a wildcard
		// would be a grant nobody authored, and S10 has no glob vocabulary to
		// audit. It matches a request that literally asks for "*".
		star := toolPolicy(t, "star", policy.AgentKind, "main", policy.Allow, toolSpec(t, "*", "allow"))
		v := policy.Evaluate([]policy.Policy{star}, toolReq("main", "bash"), evalAt)
		assertVerdict(t, v, false, true, false, "", "")
		v = policy.Evaluate([]policy.Policy{star}, toolReq("main", "*"), evalAt)
		assertVerdict(t, v, true, false, false, star.ID, "")
	})
}

// ── totality: rows the validator would have refused (D10/PL6) ────────────────

func TestAToolAccessEvaluatorIsTotalOnRowsTheValidatorWouldRefuse(t *testing.T) {
	// The write boundary and the evaluator are two different guarantees, and the
	// evaluator's is the one that protects the runtime: rows exist that were
	// stored before a validator existed, and rows can be written by hand
	// straight into the table. None of them may become a panic in the daemon or,
	// worse, a permission. Every case below is refused by the validator and
	// answered by the default deny.
	bad := []struct {
		name string
		spec string
	}{
		{"malformed JSON", `{"toolName":"bash",`},
		{"not an object", `42`},
		{"an array", `["bash","rm"]`},
		{"null", `null`},
		{"empty", ``},
		{"an unknown field", `{"tool":"bash"}`},
		{"a spec of nothing but a secret", `{"secret":"hunter2-in-a-spec"}`},
		{"the wrong field type", `{"toolName":{"nested":"deep"},"state":"allow"}`},
		{"trailing content", `{"toolName":"bash","state":"allow"}{"toolName":"rm","state":"deny"}`},
		{"a duplicated key", `{"toolName":"bash","state":"allow","state":"deny"}`},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			row := toolPolicy(t, "bad", policy.AgentKind, "main", policy.Allow, c.spec)
			if rej := validateWith(t, policy.ToolAccess, row); rej == nil {
				t.Fatalf("the validator accepted %s (%s); the evaluator's totality is a backstop, "+
					"not a reason to loosen the write path", c.name, c.spec)
			}
			v := policy.Evaluate([]policy.Policy{row}, toolReq("main", "bash"), evalAt)
			assertVerdict(t, v, false, true, false, "", "")
		})
	}

	t.Run("the evaluator is asked only its own type, and answers no other", func(t *testing.T) {
		// The engine filters by type before it calls Match, so this is the belt
		// rather than the braces — and a belt nobody has tested is a belt
		// nobody can rely on. Asked about another type, ToolAccess says "not
		// me": not a match, and not a refusal to decide either.
		ev, ok := policy.EvaluatorFor(policy.ToolAccess)
		if !ok {
			t.Fatal("ToolAccess has no evaluator")
		}
		row := toolPolicy(t, "fine-but-not-my-type", policy.AgentKind, "main", policy.Allow,
			toolSpec(t, "bash", "allow"))
		res := ev.Match(row, policy.Request{Type: policy.CommandAccess, Agent: "main", Tool: "bash"})
		if res.Matched || res.Unimplemented || strings.TrimSpace(res.Note) == "" {
			t.Errorf("Match on a foreign type = %+v; the answer is \"not me\", which is neither a "+
				"match nor a refusal to decide", res)
		}
	})

	t.Run("the evaluator notes never carry spec content", func(t *testing.T) {
		// MatchRes.Note is a log line. The slice promises its logs and events
		// carry no policy content (legal constraint 1), and a Note that quotes
		// the bytes that failed is how a spec — or a secret somebody typed into
		// one — reaches a log.
		ev, ok := policy.EvaluatorFor(policy.ToolAccess)
		if !ok {
			t.Fatal("ToolAccess has no evaluator")
		}
		for _, spec := range []string{`{"tool":"bash"}`, `{"secret":"hunter2-in-a-spec"}`, `{"toolName":"rm -rf /"}`, `[1,2,3]`} {
			row := toolPolicy(t, "note-check", policy.AgentKind, "main", policy.Allow, spec)
			res := ev.Match(row, toolReq("main", "bash"))
			if res.Matched || res.Unimplemented {
				t.Errorf("Match(%s) = %+v; an unreadable spec matches nothing and decides nothing", spec, res)
			}
			for _, leak := range []string{"bash", "hunter2", "rm", "secret"} {
				if strings.Contains(res.Note, leak) {
					t.Errorf("the Note %q leaks %q from the spec %s; a log line names the shape, never the content",
						res.Note, leak, spec)
				}
			}
			if strings.TrimSpace(res.Note) == "" {
				t.Errorf("Match(%s) refused with no Note; a refusal names what it refused", spec)
			}
		}
	})
}

// ── the default blocklist is empty (Q1) ──────────────────────────────────────

func TestTheDefaultBlocklistIsEmpty(t *testing.T) {
	// Q1's answer, recorded in the task's Notes as well as here: **a default
	// blocklist is a policy decision disguised as a safe default.** A deployment
	// that wants `bash` refused authors that refusal, and `doctor` reports what
	// is in force; shipping an opinion would hand every deployment a choice
	// nobody made for their situation. And "empty" is not "permissive" — PL5
	// denies everything already, so a built-in blocklist would add a second,
	// unauthored denial mechanism next to the one rules are written with.
	//
	// So the assertion has two halves: absence in the code, and behaviour that
	// only an authored row can change.

	t.Run("the package declares no blocklist", func(t *testing.T) {
		for _, file := range productionSources(t) {
			for _, hit := range findBlocklistDeclarations(readSource(t, file)) {
				t.Errorf("%s declares %s; the default blocklist is empty and stays empty by having "+
					"nothing to hold it (Q1) — a deployment refuses what it wants refused by writing a Deny", file, hit)
			}
		}
	})

	t.Run("nothing is refused before anything is written", func(t *testing.T) {
		// The tools an opinionated default would have refused. Each answers the
		// default deny — PL5, not a named rule — and none of them is denied by
		// a row that exists.
		for _, tool := range []string{"bash", "rm -rf /", "git push --force", "curl", "write"} {
			v := policy.Evaluate(nil, toolReq("main", tool), evalAt)
			assertVerdict(t, v, false, true, false, "", "")
			if v.By != nil {
				t.Errorf("%q was refused by %q with no policy written; only an authored Deny may name itself",
					tool, v.By.ID)
			}
		}
	})

	t.Run("a refusal needs a row, and names it", func(t *testing.T) {
		// The other direction: the only way to a named denial is a Deny somebody
		// authored, which is what makes the empty default safe rather than
		// merely quiet.
		ban := toolPolicy(t, "operator-wrote-this", policy.AgentKind, "main", policy.Deny, toolSpec(t, "bash", "deny"))
		v := policy.Evaluate([]policy.Policy{ban}, toolReq("main", "bash"), evalAt)
		assertVerdict(t, v, false, false, false, ban.ID, "")
	})
}

// findBlocklistDeclarations reports package-level declarations that could carry
// a built-in blocklist: a slice of policy rows at file scope, or a name that
// says blocklist/blacklist/denylist. It takes source text so the guard itself
// can be proven to fire (TestTheBlocklistGuardCanFail).
func findBlocklistDeclarations(src string) []string {
	var hits []string
	for _, d := range packageLevelNames(src) {
		lower := strings.ToLower(d)
		for _, banned := range []string{"blocklist", "blacklist", "denylist", "defaultdeny", "default_deny"} {
			if strings.Contains(lower, banned) {
				hits = append(hits, fmt.Sprintf("%s (a name saying %q)", d, banned))
			}
		}
	}
	hits = append(hits, packageLevelRowSlices(src)...)
	return hits
}

func TestTheBlocklistGuardCanFail(t *testing.T) {
	// The empty default is a decision somebody could quietly reverse in one
	// line, so the guard that keeps it empty is proven on the two shapes that
	// reversal would take — a named list, and an anonymous slice of rows — plus
	// the code it must leave alone.
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"a named list of refusals",
			"package policy\n\nvar defaultBlocklist = []string{\"bash\"}\n", true},
		{"an unlisted slice of rows",
			"package policy\n\nvar hardened = []Policy{{ID: \"ban-bash\", Action: Deny}}\n", true},
		{"a slice of policy types is not a blocklist",
			"package policy\n\nvar policyTypes = []PolicyType{ToolAccess}\n", false},
		{"the schema table this package does have",
			"package policy\n\nvar toolAccessSchema = newSpecSchema(ToolAccess, \"shape\", nil)\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hits := findBlocklistDeclarations(c.src)
			if c.want && len(hits) == 0 {
				t.Errorf("the guard missed a built-in blocklist: %s", c.src)
			}
			if !c.want && len(hits) > 0 {
				t.Errorf("the guard cried wolf on legitimate code: %v", hits)
			}
		})
	}
}

// ── SEC-059 — the alias row decides: Match speaks both spellings ────────────

// wireAliasCanonicalizer installs the production-shaped stub: the one hidden
// alias answers its honest name, every other name answers itself — exactly
// what tools.CanonicalToolName does. These unit tests deliberately do NOT
// import internal/tools to get the real function (tools imports policy; a
// unit file testing the seam against the wired binary would pin nothing):
// the daemon and protocol suites link tools for their own reasons and assert
// the real-function wiring there. Nothing that moves the canonicalizer runs
// in parallel, and t.Cleanup restores identity so the unwired default below
// is always the package default.
func wireAliasCanonicalizer(t *testing.T) {
	t.Helper()
	policy.SetToolNameCanonicalizer(func(s string) string {
		if s == "write_file" {
			return "write"
		}
		return s
	})
	t.Cleanup(func() { policy.SetToolNameCanonicalizer(nil) })
}

// toolAccessMatcher is the registered evaluator, asked directly — the SAME
// Match the engine runs at evaluate.go, so the matrix below tests the judge
// itself, not a re-formulation of it.
func toolAccessMatcher(tb testing.TB) policy.Evaluator {
	tb.Helper()
	ev, ok := policy.EvaluatorFor(policy.ToolAccess)
	if !ok {
		tb.Fatal("ToolAccess has no evaluator registered; the matcher cannot be asked directly")
	}
	return ev
}

func TestToolAccessMatchSpellsWriteBothWays(t *testing.T) {
	wireAliasCanonicalizer(t)
	ev := toolAccessMatcher(t)

	// The 2×2: every row spelling × every request spelling matches. One row
	// decides for both spellings of the same capability — and because the
	// gate, the CLI why and the socket why all run through THIS Match, what
	// the row says here is what all three surfaces say.
	for _, rowName := range []string{"write", "write_file"} {
		for _, callName := range []string{"write", "write_file"} {
			row := toolPolicy(t, "row", policy.AgentKind, "main", policy.Allow, toolSpec(t, rowName, "allow"))
			res := ev.Match(row, policy.Request{Type: policy.ToolAccess, Agent: "main", Tool: callName})
			if !res.Matched || res.Unimplemented {
				t.Errorf("row %q vs request %q: Matched=false (note %q); SEC-059 — the alias is a spelling, not a different tool",
					rowName, callName, res.Note)
			}
		}
	}

	// Negative controls: exactness survives on every name the canonicalizer
	// does not map. A prefix, an empty request, unknown names — none of them
	// canonicalize onto each other, and the refusal keeps the fixed Note
	// (never the bytes that failed it, legal constraint 1).
	neg := []struct{ rowName, callName string }{
		{"writex", "write"},
		{"write", "writex"},
		{"write_file", "writex"},
		{"write", ""},
		{"write_file", ""},
		{"unauthored-a", "unauthored-b"},
	}
	for _, n := range neg {
		row := toolPolicy(t, "row", policy.AgentKind, "main", policy.Allow, toolSpec(t, n.rowName, "allow"))
		res := ev.Match(row, policy.Request{Type: policy.ToolAccess, Agent: "main", Tool: n.callName})
		if res.Matched || res.Note != "the toolName does not name the request's tool" {
			t.Errorf("row %q vs request %q: matched %v (note %q); want Matched false and the fixed non-match Note",
				n.rowName, n.callName, res.Matched, res.Note)
		}
	}

	t.Run("a canonicalizer that answers nothing falls back to the spelling", func(t *testing.T) {
		// The seam's other safe path: the hook exists but says nothing about
		// this name ("" is not an answer), so the comparison is identity —
		// bash still matches bash. Never a guess.
		policy.SetToolNameCanonicalizer(func(string) string { return "" })
		row := toolPolicy(t, "row", policy.AgentKind, "main", policy.Allow, toolSpec(t, "bash", "allow"))
		if res := ev.Match(row, toolReq("main", "bash")); !res.Matched {
			t.Errorf("an empty canonical answer lost the literal match: %q", res.Note)
		}
	})
}

func TestToolAccessMatchWithoutCanonicalizerStaysExact(t *testing.T) {
	// The safe unwired default, pinned: a nil hook is identity — the
	// pre-SEC-059 behaviour kept as the DEFAULT, so it is the wiring (not
	// this unit file) that carries the alias, and an engine that somehow
	// runs unwired stays inert — a row under write_file matching nothing
	// honest — rather than inventing a match.
	policy.SetToolNameCanonicalizer(nil)
	ev := toolAccessMatcher(t)
	aliasRow := toolPolicy(t, "legacy", policy.AgentKind, "main", policy.Allow, toolSpec(t, "write_file", "allow"))
	if res := ev.Match(aliasRow, toolReq("main", "write")); res.Matched {
		t.Error("with no canonicalizer wired the alias row matched an honest-name request — the identity default must stay inert; the wiring is what carries the alias")
	}
	if res := ev.Match(aliasRow, toolReq("main", "write_file")); !res.Matched {
		t.Errorf("the literal must still match its own spelling: note %q", res.Note)
	}
	if res := ev.Match(aliasRow, toolReq("main", "write_file")); res.Note != "" {
		t.Errorf("the matched arm must carry no Note, got %q", res.Note)
	}
}

func TestTheCanonicalizerWiringAppearsInExactlyOnePlace(t *testing.T) {
	// The grep pin, mirroring TestTheAuthorshipRuleAppearsInExactlyOnePlace:
	// the seam has ONE definition (internal/policy/spec_toolaccess.go owns
	// what a ToolAccess row matches) and exactly ONE production call
	// (internal/tools/toolset.go, the file that already owns the alias —
	// policy asks, tools answers). A second definition is a second mapping
	// free to drift from the Registry; a second caller is a runtime
	// re-shaping of enforcement wearing an init-time seam's name. Test files
	// are exempt — this file sets the hook on purpose, and prose about the
	// rule is not a second enforcement of it.
	const seamExpr = "SetToolNameCanonicalizer("
	var defs int
	for _, file := range moduleSources(t) {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		text := readSource(t, file)
		d := strings.Count(text, "func "+seamExpr)
		c := strings.Count(text, seamExpr) - d
		defs += d
		slash := filepathToSlash(file)
		switch {
		case strings.HasSuffix(slash, "internal/policy/spec_toolaccess.go"):
			if d != 1 || c != 0 {
				t.Errorf("spec_toolaccess.go holds %d definitions and %d calls of SetToolNameCanonicalizer; want 1 and 0 — the seam is defined here and called nowhere here", d, c)
			}
		case strings.HasSuffix(slash, "internal/tools/toolset.go"):
			if d != 0 || c != 1 {
				t.Errorf("toolset.go holds %d definitions and %d calls of SetToolNameCanonicalizer; want 0 and 1 — the registry's honest names are wired exactly once, from the file that owns the alias", d, c)
			}
		case d != 0 || c != 0:
			t.Errorf("%s mentions SetToolNameCanonicalizer (%d defs, %d calls); the seam is set by internal/tools/toolset.go alone — anything else is a second wiring point", slash, d, c)
		}
	}
	if defs != 1 {
		t.Errorf("SetToolNameCanonicalizer is defined %d times; the seam has exactly one home", defs)
	}
}

func TestTheBoundaryStaysPermissiveOnAliasAuthoredRows(t *testing.T) {
	// SEC-059's other half, and the reason the refusal design was rejected:
	// the fix sits at MATCH time, never at Add. Rows authored under the
	// alias predate T19 and live in real stores; a boundary refusing them
	// would silently revoke every operator-authored grant those stores carry
	// — and "the legacy pinned blueprint grants again" could never be shown
	// true. Add answers nil; the canonicalizer in Match is what makes such a
	// row decide. This is the documented compatibility decision, not an
	// oversight: policing a compatibility spelling was never the boundary's
	// job (its validator still pins the SHAPE — state, non-emptiness,
	// authorship — everything but the name's provenance).
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	aliasDeny := boundaryPolicy(t, "legacy-alias-deny", policy.AgentKind, "main", policy.Deny, policy.Operator,
		json.RawMessage(`{"toolName":"write_file","state":"deny"}`))
	if rej := s.Add(context.Background(), aliasDeny, addAt); rej != nil {
		t.Errorf("Store.Add refused an alias-authored deny row: %s — the boundary stays permissive so legacy pinned stores keep working", rej.Reason)
	}
	aliasAllow := boundaryPolicy(t, "legacy-alias-allow", policy.AgentKind, "main", policy.Allow, policy.Operator,
		json.RawMessage(`{"toolName":"write_file","state":"allow"}`))
	if rej := s.Add(context.Background(), aliasAllow, addAt); rej != nil {
		t.Errorf("Store.Add refused an alias-authored allow row: %s — a silent revocation of operator-authored allows is the bug this design refuses to ship", rej.Reason)
	}
}

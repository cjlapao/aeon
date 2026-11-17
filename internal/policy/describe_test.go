package policy_test

// The ONE Describe formatter (S45 T2, PM5/D9): every policy type × every
// action renders its design-pinned sentence — no more, no less; every value
// %q-quoted; the two suffixes where the row carries them; the fallback
// sentence for anything the engine cannot read, so no surface can be handed
// a panic or an empty string beside a real rule.
//
// The expected strings below are INDEPENDENT literals — typed here from the
// design's templates, not produced by calling Describe. A formatter that
// drifts from the grammar fails here; a surface that assembles its own
// sentence fails the protocol-side pin (and the CLI's goldens pin the CLI).

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

var describeExpiry = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

func describeRow(typ policy.PolicyType, target policy.Target, action policy.Action, spec string, notAfter *time.Time) policy.Policy {
	return policy.Policy{
		ID: "row", Type: typ, Target: target, Action: action,
		Spec: json.RawMessage(spec), Source: policy.Operator, NotAfter: notAfter,
	}
}

func TestDescribeEveryType(t *testing.T) {
	agent := policy.Target{Kind: policy.AgentKind, ID: "boss"}
	all := policy.Target{Kind: policy.AllAgents}
	lane := policy.Target{Kind: policy.LaneTypeKind, ID: "review"}
	mcp := policy.Target{Kind: policy.MCPServerKind, ID: "github"}
	loop := policy.Target{Kind: policy.LoopKind, ID: "nightly"}

	st := func(act policy.Action) string { return fmt.Sprintf("%q", string(act)) }
	cases := []struct {
		name string
		p    policy.Policy
		want string
	}{
		// tool_access × 3 — the design's exact template.
		{"tool/allow", describeRow(policy.ToolAccess, agent, policy.Allow, `{"toolName":"bash","state":`+st(policy.Allow)+`}`, nil),
			`allows tool "bash" for agent "boss" — authored by operator`},
		{"tool/deny", describeRow(policy.ToolAccess, agent, policy.Deny, `{"toolName":"bash","state":`+st(policy.Deny)+`}`, nil),
			`denies tool "bash" for agent "boss" — authored by operator`},
		{"tool/ask", describeRow(policy.ToolAccess, agent, policy.Ask, `{"toolName":"bash","state":`+st(policy.Ask)+`}`, nil),
			`asks before tool "bash" for agent "boss" — authored by operator`},
		// command_access × 3 — "matching" is the pattern language's word; one
		// row carries the expiry suffix (the — expires clause, RFC3339 UTC).
		{"command/allow", describeRow(policy.CommandAccess, all, policy.Allow, `{"command":"git push*","state":`+st(policy.Allow)+`}`, nil),
			`allows commands matching "git push*" for all agents — authored by operator`},
		{"command/deny-expiring", describeRow(policy.CommandAccess, all, policy.Deny, `{"command":"git push*","state":`+st(policy.Deny)+`}`, &describeExpiry),
			`denies commands matching "git push*" for all agents — expires 2026-11-01T00:00:00Z — authored by operator`},
		{"command/ask", describeRow(policy.CommandAccess, all, policy.Ask, `{"command":"git push*","state":`+st(policy.Ask)+`}`, nil),
			`asks before commands matching "git push*" for all agents — authored by operator`},
		// route_access × 3 — matching (the single trailing star is the one
		// pattern form), naming the field `route`.
		{"route/allow", describeRow(policy.RouteAccess, lane, policy.Allow, `{"route":"remote:*","state":`+st(policy.Allow)+`}`, nil),
			`allows routes matching "remote:*" for lane type "review" — authored by operator`},
		{"route/deny", describeRow(policy.RouteAccess, lane, policy.Deny, `{"route":"remote:*","state":`+st(policy.Deny)+`}`, nil),
			`denies routes matching "remote:*" for lane type "review" — authored by operator`},
		{"route/ask", describeRow(policy.RouteAccess, lane, policy.Ask, `{"route":"remote:*","state":`+st(policy.Ask)+`}`, nil),
			`asks before routes matching "remote:*" for lane type "review" — authored by operator`},
		// effect_class × 3 — naming the field `effect`.
		{"effect/allow", describeRow(policy.EffectClass, mcp, policy.Allow, `{"effect":"write","state":`+st(policy.Allow)+`}`, nil),
			`allows effect "write" for MCP server "github" — authored by operator`},
		{"effect/deny", describeRow(policy.EffectClass, mcp, policy.Deny, `{"effect":"write","state":`+st(policy.Deny)+`}`, nil),
			`denies effect "write" for MCP server "github" — authored by operator`},
		{"effect/ask", describeRow(policy.EffectClass, mcp, policy.Ask, `{"effect":"write","state":`+st(policy.Ask)+`}`, nil),
			`asks before effect "write" for MCP server "github" — authored by operator`},
		// cost_cap × 3 — "caps %q spend at %s": the token is quoted, the cap
		// is the number token (unquoted, as the design's grammar says).
		{"cost/allow", describeRow(policy.CostCap, loop, policy.Allow, `{"token":"gpt-tokens","max":1000}`, nil),
			`caps "gpt-tokens" spend at 1000 for loop "nightly" — authored by operator`},
		{"cost/deny", describeRow(policy.CostCap, loop, policy.Deny, `{"token":"gpt-tokens","max":1000}`, nil),
			`caps "gpt-tokens" spend at 1000 for loop "nightly" — authored by operator`},
		{"cost/ask", describeRow(policy.CostCap, loop, policy.Ask, `{"token":"gpt-tokens","max":1000}`, nil),
			`caps "gpt-tokens" spend at 1000 for loop "nightly" — authored by operator`},
		// skill_access × 3 — naming the field `skill`, exact (no "matching").
		{"skill/allow", describeRow(policy.SkillAccess, policy.Target{Kind: policy.AgentKind, ID: "bob"}, policy.Allow, `{"skill":"acme/deploy","state":`+st(policy.Allow)+`}`, nil),
			`allows skill "acme/deploy" for agent "bob" — authored by operator`},
		{"skill/deny", describeRow(policy.SkillAccess, policy.Target{Kind: policy.AgentKind, ID: "bob"}, policy.Deny, `{"skill":"acme/deploy","state":`+st(policy.Deny)+`}`, nil),
			`denies skill "acme/deploy" for agent "bob" — authored by operator`},
		{"skill/ask", describeRow(policy.SkillAccess, policy.Target{Kind: policy.AgentKind, ID: "bob"}, policy.Ask, `{"skill":"acme/deploy","state":`+st(policy.Ask)+`}`, nil),
			`asks before skill "acme/deploy" for agent "bob" — authored by operator`},
		// trigger_access × 3 — naming the field `trigger`.
		{"trigger/allow", describeRow(policy.TriggerAccess, all, policy.Allow, `{"trigger":"webhook","state":`+st(policy.Allow)+`}`, nil),
			`allows trigger "webhook" for all agents — authored by operator`},
		{"trigger/deny", describeRow(policy.TriggerAccess, all, policy.Deny, `{"trigger":"webhook","state":`+st(policy.Deny)+`}`, nil),
			`denies trigger "webhook" for all agents — authored by operator`},
		{"trigger/ask", describeRow(policy.TriggerAccess, all, policy.Ask, `{"trigger":"webhook","state":`+st(policy.Ask)+`}`, nil),
			`asks before trigger "webhook" for all agents — authored by operator`},
	}
	if len(cases) < 21 {
		t.Fatalf("the table covers %d lines; the spec demands at least 21 (7 types × 3 actions)", len(cases))
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := policy.Describe(c.p); got != c.want {
				t.Errorf("Describe =\n  %q\nwant\n  %q", got, c.want)
			}
		})
	}
	// The remaining target phrases ride separate rows: every kind of Target
	// renders its pinned phrase (all agents / agent %q / loop %q / lane type
	// %q / MCP server %q — all five appear above; assert the set is present).
	kinds := map[policy.TargetKind]string{
		policy.AllAgents: "all agents", policy.AgentKind: `agent "`, policy.LoopKind: `loop "`,
		policy.LaneTypeKind: `lane type "`, policy.MCPServerKind: `MCP server "`,
	}
	for _, k := range policy.AllTargetKinds() {
		phrase, ok := kinds[k]
		if !ok {
			t.Fatalf("target kind %q has no expected phrase pinned here", k)
		}
		p := describeRow(policy.ToolAccess, policy.Target{Kind: k, ID: "x"}, policy.Deny, `{"toolName":"bash","state":"deny"}`, nil)
		if s := policy.Describe(p); !strings.Contains(s, phrase) {
			t.Errorf("target kind %q did not render its phrase %q in %q", k, phrase, s)
		}
	}
}

func TestDescribeQuotesValues(t *testing.T) {
	// A tool name carrying quotes, a newline, ANSI, and a BiDi override: the
	// sentence must carry the %q rendering and no raw control byte (the
	// sentence is printed to terminals — SEC-033/SEC-046). The spec is
	// built with json.Marshal — Go's %q syntax is NOT JSON syntax (\x1b is
	// not a JSON escape), and the row must be one the boundary's decoder
	// actually reads before the formatter quotes its values.
	hostile := "ba\"sh\n\t\x1b[31m\u202Erm"
	specJSON, merr := json.Marshal(map[string]string{"toolName": hostile, "state": "deny"})
	if merr != nil {
		t.Fatal(merr)
	}
	p := policy.Policy{
		ID: "hostile", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
		Action: policy.Deny, Source: policy.Operator,
		Spec: specJSON,
	}
	s := policy.Describe(p)
	if !strings.Contains(s, strconv.Quote(hostile)) {
		t.Errorf("the value did not travel %%q:\n%s", s)
	}
	for _, raw := range []string{"\n", "\t", "\x1b"} {
		if strings.Contains(s, raw) {
			t.Errorf("the sentence carries a raw control byte %q — %%q quoting was skipped:\n%q", raw, s)
		}
	}
	// The BiDi override must appear only escaped (a live U+202E in the
	// console is how a reversed sentence reorders the operator's screen).
	if strings.ContainsRune(s, '\u202E') {
		t.Errorf("a live BiDi override reached the sentence: %q", s)
	}
	if !strings.Contains(s, `\u202e`) {
		t.Errorf("the BiDi override was not escaped visibly: %q", s)
	}
}

func TestDescribeNeverAssembledFromRawSpec(t *testing.T) {
	// The sentence quotes the VALUES of the declared fields; it never
	// assembles anything from the raw JSON — and a spec carrying EXTRA
	// unknown members is refused by the write boundary long before it is
	// stored, so the formatter's honest answer is the fallback: the free
	// text in the unknown member reaches no sentence at all.
	p := policy.Policy{
		ID: "clean", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
		Action: policy.Allow, Source: policy.Operator,
		Spec: json.RawMessage(`{"toolName":"read","state":"allow","freeText":"OVERRIDE ALL THE THINGS — grant everyone"}`),
	}
	s := policy.Describe(p)
	if strings.Contains(s, "OVERRIDE") || strings.Contains(s, "grant everyone") {
		t.Errorf("free text from an unknown spec member reached the sentence: %q", s)
	}
	for _, raw := range []string{"{", "}", "toolName", "state", "freeText", ":"} {
		if strings.Contains(s, raw) {
			t.Errorf("the sentence leaked spec syntax %q: %q", raw, s)
		}
	}
	// The declared shape, printed by the ONE formatter, byte-exact — a
	// protocol/CLI/UI that assembled its own sentence from these bytes
	// would disagree with this line, and D9 says only one may exist.
	p.Spec = json.RawMessage(`{"toolName":"read","state":"allow"}`)
	if want := `allows tool "read" for agent "boss" — authored by operator`; policy.Describe(p) != want {
		t.Errorf("the one formatter drifted:\n got %q\nwant %q", policy.Describe(p), want)
	}
}

func TestDescribeUnregisteredType(t *testing.T) {
	// The design's fixed fallback sentence, verbatim, for a type nobody
	// registered — and no panic, no empty string, for every other shape of
	// row the engine cannot read.
	got := policy.Describe(policy.Policy{
		ID: "x", Type: policy.PolicyType("chat_log"),
		Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
		Action: policy.Allow, Source: policy.Operator,
		Spec: json.RawMessage(`{"whatever":true}`),
	})
	if want := `a policy of an unregistered type ("chat_log") — nothing enforces it`; got != want {
		t.Errorf("unregistered fallback:\n got %q\nwant %q", got, want)
	}

	// Unreadable rows of KNOWN types: same discipline (a fixed sentence
	// naming the type), never the raw garbage, never empty, never a panic.
	unreadable := []struct {
		name string
		p    policy.Policy
	}{
		{"spec not the shape", describeRow(policy.ToolAccess, policy.Target{Kind: policy.AgentKind, ID: "b"}, policy.Allow, `{"toolName":{"nested":"evil"},"state":"allow"}`, nil)},
		{"spec contradicts action", describeRow(policy.ToolAccess, policy.Target{Kind: policy.AgentKind, ID: "b"}, policy.Deny, `{"toolName":"bash","state":"allow"}`, nil)},
		{"action outside the set", describeRow(policy.ToolAccess, policy.Target{Kind: policy.AgentKind, ID: "b"}, policy.Action("permit"), `{"toolName":"bash","state":"allow"}`, nil)},
		{"source outside the set", func() policy.Policy {
			p := describeRow(policy.ToolAccess, policy.Target{Kind: policy.AgentKind, ID: "b"}, policy.Deny, `{"toolName":"bash","state":"deny"}`, nil)
			p.Source = policy.Source("govern\u202Emp")
			return p
		}()},
		{"target kind outside the set", describeRow(policy.ToolAccess, policy.Target{Kind: policy.TargetKind("folder"), ID: "x"}, policy.Deny, `{"toolName":"bash","state":"deny"}`, nil)},
		{"empty spec", describeRow(policy.ToolAccess, policy.Target{Kind: policy.AgentKind, ID: "b"}, policy.Deny, `{}`, nil)},
	}
	for _, c := range unreadable {
		t.Run(c.name, func(t *testing.T) {
			s := policy.Describe(c.p)
			if s == "" {
				t.Fatal("an empty sentence beside a real row — the UI would render a blank")
			}
			if !strings.Contains(s, "nothing enforces it") {
				t.Errorf("the fallback did not state that nothing enforces it: %q", s)
			}
			if strings.Contains(s, "evil") || strings.Contains(s, "{") {
				t.Errorf("the fallback echoed raw bytes: %q", s)
			}
		})
	}
}

package policy_test

// Every test here is named as a sentence, because the sentence is the claim the
// slice rests on and a failing run should read as an English complaint. The
// tests fall into four groups: the closed set is exactly what the design says
// it is; nothing outside the set can be constructed; a refusal is a
// discriminated result; and the schema's CHECK vocabulary and these constants
// agree — the last of which is the reason this task exists at all.

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// ── The closed set is exactly this ───────────────────────────────────────────

func TestTheSevenTypesAreExactlyThese(t *testing.T) {
	// PL3, asserted by enumeration against the literal list rather than by
	// reading the file: seven names, in the design's order, carrying the wire
	// values the `policy` table stores. A reordering here is not a defect, but
	// it should be a deliberate one, and this test makes it visible.
	want := []policy.PolicyType{
		policy.ToolAccess, policy.CommandAccess, policy.RouteAccess, policy.EffectClass,
		policy.CostCap, policy.SkillAccess, policy.TriggerAccess,
	}
	wantWire := []string{
		"tool_access", "command_access", "route_access", "effect_class",
		"cost_cap", "skill_access", "trigger_access",
	}

	got := policy.AllPolicyTypes()
	if len(got) != len(want) {
		t.Fatalf("AllPolicyTypes() returns %d types (%v), want exactly %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AllPolicyTypes()[%d] = %q, want %q", i, got[i], want[i])
		}
		if string(got[i]) != wantWire[i] {
			t.Errorf("the wire value of %q is %q, want %q — migration 0016's CHECK stores this "+
				"string character for character", want[i], string(got[i]), wantWire[i])
		}
		// Everything the set publishes must parse back to itself, or the set
		// and the parser disagree about what is legal.
		if back, rej := policy.ParsePolicyType(string(got[i])); rej != nil || back != want[i] {
			t.Errorf("ParsePolicyType(%q) = (%q, %v); the set's own value must parse", wantWire[i], back, rej)
		}
	}

	// The set cannot be widened from the outside: AllPolicyTypes hands out a
	// copy and the list behind it is unexported, so sorting or appending to a
	// result tells you nothing about what the engine accepts tomorrow.
	got[0] = policy.PolicyType("tampered")
	if again := policy.AllPolicyTypes(); again[0] != policy.ToolAccess {
		t.Errorf("mutating the result of AllPolicyTypes() changed the engine's own set: %q", again[0])
	}

	// The plausible eighths — values someone could reach for, including the
	// five 0005 shipped. Each is refused, and refused by name.
	for _, eighth := range []string{
		"chat_log", "tool", "path", "network", "spend", "approval",
		"budget", "mcp_server", "model_availability", "guardrail", "", "ToolAccess", "tool access",
	} {
		v, rej := policy.ParsePolicyType(eighth)
		if rej == nil {
			t.Errorf("an eighth policy type %q was accepted as %q; the set is closed at seven (PL3)", eighth, v)
			continue
		}
		if !strings.Contains(rej.Reason, strconv.Quote(eighth)) {
			t.Errorf("the refusal of policy type %q does not quote the value it refused: %s", eighth, rej.Reason)
		}
	}
}

func TestTheFiveTargetKindsAreExactlyThese(t *testing.T) {
	// The design's five: `all-agents · agent:<name> · loop:<slug> ·
	// lane-type:<type> · mcp:<server>`. `lane-type` is the one worth reading
	// twice — it governs by role rather than by identity, so it is the target
	// that survives an agent being renamed.
	want := []policy.TargetKind{
		policy.AllAgents, policy.AgentKind, policy.LoopKind, policy.LaneTypeKind, policy.MCPServerKind,
	}
	wantWire := []string{"all-agents", "agent", "loop", "lane-type", "mcp"}

	got := policy.AllTargetKinds()
	if len(got) != len(want) {
		t.Fatalf("AllTargetKinds() returns %d kinds (%v), want exactly %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] || string(got[i]) != wantWire[i] {
			t.Errorf("AllTargetKinds()[%d] = (%q, %q), want (%q, %q)", i, got[i], string(got[i]), want[i], wantWire[i])
		}
		if back, rej := policy.ParseTargetKind(string(got[i])); rej != nil || back != want[i] {
			t.Errorf("ParseTargetKind(%q) = (%q, %v); the set's own value must parse", wantWire[i], back, rej)
		}
	}
}

func TestTheFourSourcesAreExactlyThese(t *testing.T) {
	// PL1's field. The asymmetry this list makes possible — an Allow from
	// operator only, a Deny from anyone — is T6's write boundary, and it can
	// only reason about sources if this list is the whole of them.
	want := []policy.Source{policy.Operator, policy.Agent, policy.MCPServer, policy.Import}
	wantWire := []string{"operator", "agent", "mcp_server", "import"}

	got := policy.AllSources()
	if len(got) != len(want) {
		t.Fatalf("AllSources() returns %d sources (%v), want exactly %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] || string(got[i]) != wantWire[i] {
			t.Errorf("AllSources()[%d] = (%q, %q), want (%q, %q)", i, got[i], string(got[i]), want[i], wantWire[i])
		}
		if back, rej := policy.ParseSource(string(got[i])); rej != nil || back != want[i] {
			t.Errorf("ParseSource(%q) = (%q, %v); the set's own value must parse", wantWire[i], back, rej)
		}
	}

	// `system` and `admin` are the two names most likely to be invented by
	// someone who wants a fifth. Neither is in the design, and a source the
	// engine does not know is a source whose trust level nobody decided.
	for _, bad := range []string{"system", "admin", "root", "operator ", "MCP_SERVER", "daemon", "mcp", ""} {
		v, rej := policy.ParseSource(bad)
		if rej == nil {
			t.Errorf("the source %q was accepted as %q; the set is closed at four (PL1)", bad, v)
			continue
		}
		if !strings.Contains(rej.Reason, strconv.Quote(bad)) {
			t.Errorf("the refusal of source %q does not quote it: %s", bad, rej.Reason)
		}
	}
}

func TestTheThreeActionsAreExactlyThese(t *testing.T) {
	want := []policy.Action{policy.Allow, policy.Deny, policy.Ask}
	got := policy.AllActions()
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("AllActions() = %v, want %v", got, want)
	}
	if again := policy.AllActions(); len(again) == 3 {
		again[0] = policy.Action("tampered")
		if policy.AllActions()[0] != policy.Allow {
			t.Error("mutating the result of AllActions() changed the engine's own set")
		}
	}
	// The third action parses to the third constant — the wire value the
	// corpus carries and migration 0019's CHECK admits.
	if a, rej := policy.ParseAction("ask"); rej != nil || a != policy.Ask {
		t.Errorf(`ParseAction("ask") = (%q, %v); ask is in the closed set since S11's D1`, a, rej)
	}
	for _, bad := range []string{"maybe", "ALLOW", "permit", "allow ", "", "ASK", "Ask", "ask "} {
		v, rej := policy.ParseAction(bad)
		if rej == nil {
			t.Errorf("the action %q was accepted as %q", bad, v)
			continue
		}
		if !strings.Contains(rej.Reason, strconv.Quote(bad)) {
			t.Errorf("the refusal of action %q does not quote it: %s", bad, rej.Reason)
		}
	}
}

// ── Nothing outside the set can be constructed ───────────────────────────────

func TestAnUnknownPolicyTypeIsRefused(t *testing.T) {
	// The seed task's own words, and the case that started this slice's
	// argument: `chat_log` is plausible, absent from the design, and exactly
	// what a CHECK has to refuse. The refusal names the value it refused —
	// `permission denied` sends someone to read the source, this does not.
	cases := []struct {
		in   string
		want string // the substring the refusal must carry
	}{
		{"chat_log", `"chat_log"`},
		{"", `""`},
		{"tool", `"tool"`},                 // 0005's vocabulary, still refused
		{"approval", `"approval"`},         // 0005's vocabulary, still refused
		{"Tool_Access", `"Tool_Access"`},   // case is not a formatting detail here
		{"tool_access ", `"tool_access "`}, // nor is a stray space
		{"tool-access", `"tool-access"`},
		{"toolaccess", `"toolaccess"`},
	}
	for _, c := range cases {
		got, rej := policy.ParsePolicyType(c.in)
		if rej == nil {
			t.Errorf("ParsePolicyType(%q) accepted %q; the set is closed at seven", c.in, got)
			continue
		}
		if got != policy.PolicyType("") {
			t.Errorf("ParsePolicyType(%q) refused but still returned %q; a refusal returns the zero value", c.in, got)
		}
		if !strings.Contains(rej.Reason, c.want) {
			t.Errorf("ParsePolicyType(%q) refused with %q, which does not name the value refused", c.in, rej.Reason)
		}
		if !strings.Contains(rej.Reason, "unknown policy type") {
			t.Errorf("ParsePolicyType(%q) refused with %q; a refusal says which field it refused", c.in, rej.Reason)
		}
	}
}

func TestAnUnknownActionSourceOrTargetKindIsRefused(t *testing.T) {
	// The three remaining closed sets, each checked the same way: refused, and
	// refused by name. `global` in the target list is 0005's word for
	// everything, and it is refused rather than quietly read as `all-agents`.
	for _, in := range []string{"maybe", "ALLOW", "ASK", "", "allow|deny"} {
		_, rej := policy.ParseAction(in)
		if rej == nil {
			t.Errorf("ParseAction(%q) accepted a value outside allow/deny/ask", in)
			continue
		}
		if !strings.Contains(rej.Reason, "unknown action") || !strings.Contains(rej.Reason, strconv.Quote(in)) {
			t.Errorf("ParseAction(%q) refused with %q; it must name the action it refused", in, rej.Reason)
		}
	}

	for _, in := range []string{"system", "mcp", "self", "agent ", "", "mcp-server"} {
		_, rej := policy.ParseSource(in)
		if rej == nil {
			t.Errorf("ParseSource(%q) accepted a value outside the four (PL1)", in)
			continue
		}
		if !strings.Contains(rej.Reason, "unknown source") || !strings.Contains(rej.Reason, strconv.Quote(in)) {
			t.Errorf("ParseSource(%q) refused with %q; it must name the source it refused", in, rej.Reason)
		}
	}

	for _, in := range []string{"global", "project", "route", "repository", "all_agents", "", "ALL-AGENTS"} {
		_, rej := policy.ParseTargetKind(in)
		if rej == nil {
			t.Errorf("ParseTargetKind(%q) accepted a value outside the five", in)
			continue
		}
		if !strings.Contains(rej.Reason, "unknown target kind") || !strings.Contains(rej.Reason, strconv.Quote(in)) {
			t.Errorf("ParseTargetKind(%q) refused with %q; it must name the kind it refused", in, rej.Reason)
		}
	}

	// ParseTarget runs the same gate, because it is the constructor: a
	// hand-cast kind reaching it must not slip through into a row.
	if _, rej := policy.ParseTarget(policy.TargetKind("global"), "everything"); rej == nil {
		t.Error(`ParseTarget accepted the kind "global"; 0005's word for everything is not 0016's`)
	} else if !strings.Contains(rej.Reason, `"global"`) {
		t.Errorf(`the refusal of the kind "global" does not name it: %s`, rej.Reason)
	}
}

func TestATargetNamesItsSubjectExceptForAllAgents(t *testing.T) {
	// The invariant behind the five: `all-agents` governs everyone and so names
	// nothing, and every other kind has to name the thing it governs or it
	// matches nothing and reads as a policy that is broken.
	for _, good := range []policy.Target{
		{Kind: policy.AllAgents},
		{Kind: policy.AgentKind, ID: "golang-engineer"},
		{Kind: policy.LoopKind, ID: "s10-policy"},
		{Kind: policy.LaneTypeKind, ID: "review"},
		{Kind: policy.MCPServerKind, ID: "github"},
	} {
		got, rej := policy.ParseTarget(good.Kind, good.ID)
		if rej != nil {
			t.Errorf("ParseTarget(%q, %q) refused: %s", good.Kind, good.ID, rej.Reason)
			continue
		}
		if got != good {
			t.Errorf("ParseTarget(%q, %q) = %+v, want %+v", good.Kind, good.ID, got, good)
		}
	}

	// The canonical rendering: kind, colon, name — `lane-type:review`, the
	// shape PL8 puts in a verdict. `all-agents` renders no ID, because there is
	// no subject to name.
	for _, c := range []struct {
		target policy.Target
		want   string
	}{
		{policy.Target{Kind: policy.LaneTypeKind, ID: "review"}, "lane-type:review"},
		{policy.Target{Kind: policy.AgentKind, ID: "golang-engineer"}, "agent:golang-engineer"},
		{policy.Target{Kind: policy.MCPServerKind, ID: "github"}, "mcp:github"},
		{policy.Target{Kind: policy.AllAgents}, "all-agents"},
		// A hand-built target that skipped ParseTarget renders its kind with no
		// dangling colon. It is not a legal target — ParseTarget is what makes
		// one — but a renderer that emitted `agent:` would be rendering a
		// subject that does not exist.
		{policy.Target{Kind: policy.AgentKind}, "agent"},
		{policy.Target{}, ""},
	} {
		if got := c.target.String(); got != c.want {
			t.Errorf("Target %+v renders %q, want %q", c.target, got, c.want)
		}
	}

	// all-agents with an id: the row SEC-001 named — a rule that says
	// "everyone" and then names someone. Refused, and the refusal names both
	// the kind and the id it would have stored.
	if got, rej := policy.ParseTarget(policy.AllAgents, "agent:only-me"); rej == nil {
		t.Errorf("ParseTarget(all-agents, %q) accepted %+v; the kind means every agent, so an id "+
			"is a contradiction, not a detail", "agent:only-me", got)
	} else {
		for _, want := range []string{`"agent:only-me"`, "all-agents"} {
			if !strings.Contains(rej.Reason, want) {
				t.Errorf("the refusal of an all-agents id does not name %s: %s", want, rej.Reason)
			}
		}
	}

	// Every other kind with no id.
	for _, kind := range []policy.TargetKind{policy.AgentKind, policy.LoopKind, policy.LaneTypeKind, policy.MCPServerKind} {
		if got, rej := policy.ParseTarget(kind, ""); rej == nil {
			t.Errorf("ParseTarget(%q, %q) accepted %+v; a kind other than all-agents must name its subject", kind, "", got)
		} else if !strings.Contains(rej.Reason, strconv.Quote(string(kind))) {
			t.Errorf("the refusal of an empty id for kind %q does not name the kind: %s", kind, rej.Reason)
		}
	}

	// The ID must not repeat its kind, so one target has exactly one spelling in
	// the table and in a verdict. Unguarded, `agent:boss` under kind `agent`
	// renders as `agent:agent:boss` and becomes a second, unreachable target
	// sitting next to the first — the kind of disagreement two spellings always
	// produce.
	if got, rej := policy.ParseTarget(policy.AgentKind, "agent:boss"); rej == nil {
		t.Errorf("ParseTarget(agent, %q) accepted %+v; the id names the thing, the rendering adds the kind",
			"agent:boss", got)
	} else if !strings.Contains(rej.Reason, `"agent:boss"`) {
		t.Errorf("the refusal of a kind-prefixed id does not name it: %s", rej.Reason)
	}

	// The hyphenated kind is the case that makes the prefix rule worth having:
	// `lane-type` is the only kind whose own name contains the separator.
	if _, rej := policy.ParseTarget(policy.LaneTypeKind, "lane-type:review"); rej == nil {
		t.Error(`ParseTarget(lane-type, "lane-type:review") was accepted; the stored id is "review"`)
	}
}

func TestAPolicyWithAnUnknownFieldIsRefused(t *testing.T) {
	// The strict envelope, and the reason for it: `{"actn":"allow"}` under a
	// lenient reader becomes a policy whose action is the empty string, and a
	// policy that matches nothing is a policy that permits everything — the
	// opposite of what its author meant, in the dangerous direction. A typo has
	// to fail at the boundary.
	//
	// `oneShot` and `metadata` are the reference engine's own field names: a
	// policy copied from a plai-backend fixture must be refused here rather than
	// decoded into a policy whose extra meaning was silently dropped.
	for _, c := range []struct {
		name string
		json string
		want string // the field name the refusal must carry, "" where any refusal will do
	}{
		{"a typo in the action", `{"id":"p1","type":"tool_access","target":{"kind":"all-agents"},"actn":"deny","source":"operator"}`, `"actn"`},
		{"a typo in the type", `{"id":"p1","typ":"tool_access","target":{"kind":"all-agents"},"action":"deny","source":"operator"}`, `"typ"`},
		{"an invented field", `{"id":"p1","type":"tool_access","target":{"kind":"all-agents"},"action":"deny","source":"operator","oneShot":true}`, `"oneShot"`},
		{"a typo inside the target", `{"id":"p1","type":"tool_access","target":{"kindd":"agent","id":"x"},"action":"deny","source":"operator"}`, `"kindd"`},
		{"an invented field inside the target", `{"id":"p1","type":"tool_access","target":{"kind":"agent","id":"x","metadata":{}},"action":"deny","source":"operator"}`, `"metadata"`},
		{"a top-level array", `[{"id":"p1"}]`, ``},
		{"nothing at all", ``, ``},
		{"two policies in one body", `{"id":"p1","type":"tool_access","target":{"kind":"all-agents"},"action":"deny","source":"operator"} {"id":"p2","type":"tool_access","target":{"kind":"all-agents"},"action":"deny","source":"operator"}`, ``},
	} {
		got, rej := policy.DecodePolicy([]byte(c.json))
		if rej == nil {
			t.Errorf("%s: DecodePolicy(%s) accepted %+v", c.name, c.json, got)
			continue
		}
		if !reflect.DeepEqual(got, policy.Policy{}) {
			t.Errorf("%s: DecodePolicy refused but still returned a policy: %+v", c.name, got)
		}
		if c.want != "" && !strings.Contains(rej.Reason, c.want) {
			t.Errorf("%s: the refusal %q does not name the field %s", c.name, rej.Reason, c.want)
		}
		if !strings.Contains(rej.Reason, "policy") {
			t.Errorf("%s: the refusal %q does not say what it was refusing", c.name, rej.Reason)
		}
	}
}

func TestADecodedPolicyCannotCarryAnUnknownValue(t *testing.T) {
	// The other half of strictness: unknown FIELD names are refused, and so is
	// every known field holding a value outside its closed set. There is no path
	// from bytes to a Policy that does not cross one of the four parsers, which
	// is what lets T3 trust a Policy it is handed.
	for _, c := range []struct {
		name string
		json string
		want string
	}{
		{"an unknown type", `{"id":"p1","type":"chat_log","target":{"kind":"all-agents"},"action":"deny","source":"operator"}`, `"chat_log"`},
		{"an unknown action", `{"id":"p1","type":"tool_access","target":{"kind":"all-agents"},"action":"maybe","source":"operator"}`, `"maybe"`},
		{"an unknown source", `{"id":"p1","type":"tool_access","target":{"kind":"all-agents"},"action":"deny","source":"root"}`, `"root"`},
		{"an unknown target kind", `{"id":"p1","type":"tool_access","target":{"kind":"global"},"action":"deny","source":"operator"}`, `"global"`},
		{"an all-agents target with an id", `{"id":"p1","type":"tool_access","target":{"kind":"all-agents","id":"agent:only-me"},"action":"deny","source":"operator"}`, `"agent:only-me"`},
		{"an agent target with no id", `{"id":"p1","type":"tool_access","target":{"kind":"agent"},"action":"deny","source":"operator"}`, `"agent"`},
		{"no id", `{"type":"tool_access","target":{"kind":"all-agents"},"action":"deny","source":"operator"}`, `no id`},
		{"no target at all", `{"id":"p1","type":"tool_access","action":"deny","source":"operator"}`, `unknown target kind`},
	} {
		got, rej := policy.DecodePolicy([]byte(c.json))
		if rej == nil {
			t.Errorf("%s: DecodePolicy accepted %+v", c.name, got)
			continue
		}
		if !strings.Contains(rej.Reason, c.want) {
			t.Errorf("%s: the refusal %q does not name %q", c.name, rej.Reason, c.want)
		}
	}

	// The positive control, so the strictness above cannot pass merely because
	// the decoder refuses everything: this policy is legal and must come back
	// intact, spec included and unvalidated.
	raw := `{"id":"no-shell-in-review","type":"tool_access","target":{"kind":"lane-type","id":"review"},` +
		`"action":"deny","spec":{"toolName":"bash"},"source":"operator","not_after":null}`
	got, rej := policy.DecodePolicy([]byte(raw))
	if rej != nil {
		t.Fatalf("a legal policy was refused: %s", rej.Reason)
	}
	want := policy.Policy{
		ID:       "no-shell-in-review",
		Type:     policy.ToolAccess,
		Target:   policy.Target{Kind: policy.LaneTypeKind, ID: "review"},
		Action:   policy.Deny,
		Spec:     json.RawMessage(`{"toolName":"bash"}`),
		Source:   policy.Operator,
		NotAfter: nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DecodePolicy =\n    %+v\nwant\n    %+v", got, want)
	}
	// The spec arrived as raw bytes, unchanged: its meaning is per type and
	// belongs to the write path (T5), not to this decoder.
	if string(got.Spec) != `{"toolName":"bash"}` {
		t.Errorf("the spec was altered on the way through: %s", got.Spec)
	}
}

func TestDecodePolicyRefusesAnAllowThatIsNotAuthoredByAnOperator(t *testing.T) {
	// SEC-022/SEC-042, at the load-side choke (T19). The per-field closed sets
	// above are provably not enough for PL1: every field below is a legal
	// member of its own set, and the PAIR is the forgery — an allow that
	// authorises its own author. Add refused this pair at the write (T6) and
	// the QA probe walked straight past it three times (the on-disk row, the
	// forged manifest entries, the frozen-manifest path), because every route
	// from bytes or columns to a Policy passes HERE: the socket, the CLI,
	// decodeRow's re-read of a pre-check row, daemon.decodeFrozenSet's replay.
	// The refusal is the boundary's own sentence — called via checkAuthorship,
	// never restated (D3).
	for _, src := range []string{"agent", "mcp_server", "import"} {
		body := `{"id":"forged-self-grant","type":"tool_access","target":{"kind":"agent","id":"boss"},` +
			`"action":"allow","source":"` + src + `","spec":{"toolName":"bash","state":"allow"}}`
		got, rej := policy.DecodePolicy([]byte(body))
		if rej == nil {
			t.Errorf("DecodePolicy accepted an allow authored by %q: %+v — that is SEC-042's plant, honoured", src, got)
			continue
		}
		if !reflect.DeepEqual(got, policy.Policy{}) {
			t.Errorf("%s: refused but still returned a policy: %+v", src, got)
		}
		// The refusal names the source it refused (T6's shape, byte-shared
		// with Add's — one voice), the rule, and the safe direction.
		for _, want := range []string{`"` + src + `"`, "operator", "PL1", "Deny"} {
			if !strings.Contains(rej.Reason, want) {
				t.Errorf("the refusal of an allow authored by %q does not name %q: %s", src, want, rej.Reason)
			}
		}
	}

	// The positive controls, so the pair-check cannot pass by refusing
	// everything: the operator's allow decodes, and a deny — the direction
	// PL1 never restricts, the whole reason an import or an MCP bundle can
	// bring rules at all — decodes from all four sources.
	accepts := []string{
		`{"id":"grant","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"allow","source":"operator","spec":{"toolName":"read","state":"allow"}}`,
		`{"id":"d-op","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","source":"operator","spec":{"toolName":"bash","state":"deny"}}`,
		`{"id":"d-ag","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","source":"agent","spec":{"toolName":"bash","state":"deny"}}`,
		`{"id":"d-mcp","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","source":"mcp_server","spec":{"toolName":"bash","state":"deny"}}`,
		`{"id":"d-im","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","source":"import","spec":{"toolName":"bash","state":"deny"}}`,
	}
	for _, body := range accepts {
		got, rej := policy.DecodePolicy([]byte(body))
		if rej != nil {
			t.Errorf("a legal pairing was refused (%s): %s", body, rej.Reason)
			continue
		}
		if string(got.Action) != "allow" && string(got.Action) != "deny" {
			t.Errorf("the action did not survive the pair check: %+v", got)
		}
	}
}

func TestANilNotAfterIsNotAnExpiry(t *testing.T) {
	// PL7, and the two ways it gets broken: a far-future sentinel (the policy
	// lapses on a day nobody chose, and the lapse presents as a bug in whatever
	// it was guarding) and the zero instant behind a non-nil pointer (the policy
	// lapsed before it was written, and reads as if it had not). Both are
	// refusals of the same argument, so both are checked here, and `!p.NotAfter`
	// is the only correct question.
	//
	// The type is part of the claim, so it is asserted against the type rather
	// than read out of the file: `*time.Time`, because `time.Time` cannot say
	// "no expiry" at all — its zero value is an instant in year 1.
	field, ok := reflect.TypeOf(policy.Policy{}).FieldByName("NotAfter")
	if !ok {
		t.Fatal("Policy has no NotAfter field")
	}
	if field.Type != reflect.TypeOf((*time.Time)(nil)) {
		t.Errorf("Policy.NotAfter is %s; nil must be expressible, so it is *time.Time (PL7)", field.Type)
	}

	// A policy with the field absent, one that says null, and the zero Policy:
	// all three are no-expiry, and all three are NIL — not a pointer to an
	// instant somebody chose. (A non-nil pointer to the zero time would be
	// caught by the same check, because it is not nil.)
	bodies := map[string]string{
		"the field absent": `{"id":"p1","type":"tool_access","target":{"kind":"all-agents"},"action":"deny","source":"operator"}`,
		"the field null":   `{"id":"p1","type":"tool_access","target":{"kind":"all-agents"},"action":"deny","source":"operator","not_after":null}`,
	}
	for name, body := range bodies {
		p, rej := policy.DecodePolicy([]byte(body))
		if rej != nil {
			t.Fatalf("%s: a legal policy was refused: %s", name, rej.Reason)
		}
		if p.NotAfter != nil {
			t.Errorf("%s: NotAfter is %p (%v); no expiry is nil, never an instant somebody chose",
				name, p.NotAfter, *p.NotAfter)
		}
		// No instant in the calendar turns it into an expiry, because there is
		// nothing there to compare against. The three below are the ones a bug
		// would reach for: the zero time, a plausible date, and the sentinel.
		for _, at := range []time.Time{{}, time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
			time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)} {
			if p.NotAfter != nil {
				t.Errorf("%s: at %v the policy had an expiry (%v)", name, at, *p.NotAfter)
			}
		}
	}
	if (&policy.Policy{}).NotAfter != nil {
		t.Error("the zero Policy has an expiry; no-expiry is the zero value (PL7)")
	}

	// Rendered back out, a no-expiry policy says so — literally null, with no
	// far-future date standing in for "never". This is what lets `aeon policy
	// list` and T4's sentence state "no expiry" instead of a date.
	p, rej := policy.DecodePolicy([]byte(bodies["the field absent"]))
	if rej != nil {
		t.Fatalf("decode: %s", rej.Reason)
	}
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"not_after":null`) {
		t.Errorf("a no-expiry policy renders as %s; it must state null, not omit the field or invent a date", out)
	}
	for _, sentinel := range []string{"9999", "0001-01-01"} {
		if strings.Contains(string(out), sentinel) {
			t.Errorf("a no-expiry policy renders %q inside %s; a sentinel is a lapse on a day nobody chose",
				sentinel, out)
		}
	}

	// A real expiry, on the other hand, is the instant that was written and
	// nothing near it — exact to the nanosecond, because T3 tests the boundary
	// at NotAfter-1ns, NotAfter and NotAfter+1ns without sleeps.
	const stamp = "2027-01-01T00:00:00.000000001Z"
	expiring, rej := policy.DecodePolicy([]byte(
		`{"id":"p2","type":"tool_access","target":{"kind":"agent","id":"golang-engineer"},` +
			`"action":"allow","source":"operator","not_after":"` + stamp + `"}`))
	if rej != nil {
		t.Fatalf("a legal expiring policy was refused: %s", rej.Reason)
	}
	if expiring.NotAfter == nil {
		t.Fatal("an expiry written as " + stamp + " decoded as no expiry")
	}
	if got := expiring.NotAfter.UTC().Format(time.RFC3339Nano); got != stamp {
		t.Errorf("the expiry round-trips as %q, want %q — the instant IS the decision, so it is exact", got, stamp)
	}

	// And a malformed instant is a refusal, not a silent no-expiry: an expiry
	// that failed to parse and left nil would be a policy that never expires,
	// which is the wider permission, which is the wrong direction to fail in.
	if got, rej := policy.DecodePolicy([]byte(
		`{"id":"p3","type":"tool_access","target":{"kind":"all-agents"},` +
			`"action":"deny","source":"operator","not_after":"tomorrow"}`)); rej == nil {
		t.Errorf("a policy with an unparseable expiry decoded as %+v; that reads as never expiring", got)
	} else if !strings.Contains(rej.Reason, "tomorrow") {
		t.Errorf("the refusal of a malformed expiry does not name it: %s", rej.Reason)
	}
}

func TestARefusalIsADiscriminatedResultNotAnError(t *testing.T) {
	// S4's DB19, made structural. A domain refusal — "that is not one of the
	// seven" — is not the same kind of event as "the disk is full", and a single
	// `error` return makes every caller guess which it got. So every parser here
	// returns *Reject, and Reject is not an error: no Error method, nothing
	// satisfying the interface, and no `error` anywhere in a signature. The
	// evaluator (T3), the write boundary (T6) and the three-state answer (T11)
	// all return this same carrier, so the convention is asserted once, here.

	// Compile time: these assignments stop the build if a signature ever grows
	// or loses a return, which is the earliest any of this can be caught.
	var _ func(string) (policy.PolicyType, *policy.Reject) = policy.ParsePolicyType
	var _ func(string) (policy.Action, *policy.Reject) = policy.ParseAction
	var _ func(string) (policy.Source, *policy.Reject) = policy.ParseSource
	var _ func(string) (policy.TargetKind, *policy.Reject) = policy.ParseTargetKind
	var _ func(policy.TargetKind, string) (policy.Target, *policy.Reject) = policy.ParseTarget
	var _ func([]byte) (policy.Policy, *policy.Reject) = policy.DecodePolicy

	// Run time: the same claim as a test result rather than a reading of the
	// source, so the evidence survives someone editing a signature and a
	// matching test in the same commit.
	errType := reflect.TypeOf((*error)(nil)).Elem()
	parsers := map[string]any{
		"ParsePolicyType": policy.ParsePolicyType,
		"ParseAction":     policy.ParseAction,
		"ParseSource":     policy.ParseSource,
		"ParseTargetKind": policy.ParseTargetKind,
		"ParseTarget":     policy.ParseTarget,
		"DecodePolicy":    policy.DecodePolicy,
	}
	for name, fn := range parsers {
		ft := reflect.TypeOf(fn)
		if ft == nil {
			t.Fatalf("%s is nil", name)
		}
		if ft.NumOut() < 2 {
			t.Errorf("%s returns %d values; a parse returns its value and a refusal", name, ft.NumOut())
			continue
		}
		for i := 0; i < ft.NumOut(); i++ {
			if out := ft.Out(i); out == errType || out.Implements(errType) {
				t.Errorf("%s returns %s; a domain refusal is a discriminated result, not an error (DB19)", name, out)
			}
		}
		if last := ft.Out(ft.NumOut() - 1); last != reflect.TypeOf((*policy.Reject)(nil)) {
			t.Errorf("%s's final result is %s, want *policy.Reject", name, last)
		}
	}
	if reflect.TypeOf(policy.Reject{}).Implements(errType) {
		t.Error("policy.Reject implements error; a refusal must not be passable as one (DB19)")
	}
	if reflect.PointerTo(reflect.TypeOf(policy.Reject{})).Implements(errType) {
		t.Error("*policy.Reject implements error; a refusal must not be passable as one (DB19)")
	}

	// And a refusal carries what to show a person: a non-empty Reason naming the
	// value. A type with the right shape and an empty reason would satisfy every
	// signature above and still be useless.
	for _, c := range []struct {
		name string
		rej  *policy.Reject
	}{
		{"ParsePolicyType", second(policy.ParsePolicyType("chat_log"))},
		{"ParseAction", second(policy.ParseAction("maybe"))},
		{"ParseSource", second(policy.ParseSource("root"))},
		{"ParseTargetKind", second(policy.ParseTargetKind("global"))},
		{"ParseTarget", second(policy.ParseTarget(policy.AgentKind, ""))},
		{"DecodePolicy", second(policy.DecodePolicy([]byte(`{"id":"p1","type":"chat_log"}`)))},
	} {
		if c.rej == nil {
			t.Errorf("%s accepted something it should have refused", c.name)
			continue
		}
		if strings.TrimSpace(c.rej.Reason) == "" {
			t.Errorf("%s refused with an empty reason; a refusal names the reason and the thing refused", c.name)
		}
	}
}

// ── Purity: the boundary the evaluator will be held to, asserted now ─────────

func TestNoFileInThePolicyPackageImportsDatabaseSQL(t *testing.T) {
	// D5. The engine decides from the values it is handed and nothing else,
	// which is why the runtime can be given a decision without being given a
	// database handle (T9) and why the evaluator can be tested at the nanosecond
	// boundary (T3). The rule is asserted here, before the evaluator exists, so
	// that the first thing to arrive in this package is a pure one.
	//
	// The import list is parsed rather than grepped, on the house precedent
	// (internal/protocol's import_test.go): grep over source text matches the
	// words in comments and in this very file, so it either cries wolf or gets
	// weakened with exclusions until it matches nothing.
	//
	// The `database/sql` ban covers every file in the package, tests included —
	// if a later task needs a null-handling type in a test, it changes this test
	// in the open, with the reason, rather than quietly widening the boundary.
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob the package: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("found %d source files in the package; this test would pass almost vacuously", len(files))
	}

	fset := token.NewFileSet()
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		isTest := strings.HasSuffix(file, "_test.go")
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if path == "database/sql" || strings.HasPrefix(path, "database/sql/") {
				t.Errorf("%s imports %q; nothing under internal/policy touches a database (D5) — "+
					"the statements live in internal/store and the adapter holds the boundary", file, path)
			}
			if isTest {
				continue
			}
			// The environment, the network and the clock are the other three
			// ways a "pure" decision becomes a function of the world.
			for _, bad := range []string{"os", "net", "math/rand"} {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports %q; the engine is a function of its inputs (D5), "+
						"and every clock read arrives as a parameter", file, path)
				}
			}
		}
	}
}

// ── The lock test: the schema's CHECK and these constants agree ──────────────

// checkIN matches one `CHECK (column IN ('a','b'))` constraint. It reads the
// DDL text rather than a live database on purpose: the question is whether the
// two declarations of one vocabulary in the repository agree, and both of them
// are text.
var checkIN = regexp.MustCompile(`CHECK\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)\s+IN\s*\(([^)]*)\)\s*\)`)

func TestTheSchemaCHECKAndTypesGoAgree(t *testing.T) {
	// THE lock. The vocabulary exists twice in this repository: once as the
	// CHECK constraints on the live `policy` table, once as the constants in
	// types.go. Nothing in a compiler or a database links them, so a change to
	// one side produces a policy row the other side refuses — and the version
	// of that bug this slice already lived through is S4's: 0005's CHECK named
	// a vocabulary the design never used, the Go side and the DDL agreed with
	// each other inside the same commit, and the mismatch surfaced only when
	// an engine arrived with nothing to stand on.
	//
	// The DDL read is the LAST rebuild, not the first: 0016 and 0018 are
	// shipped and frozen, and their consts are now the shape of tables that no
	// longer exist. Migration 0019 (S11's D1) carries all four vocabulary
	// CHECKs — with the action list widened to admit ask — in the table the
	// database actually holds, so this lock compares types.go against what a
	// store opened today ENFORCES. A lock on a dead DDL would agree with
	// itself and miss a CHECK edited into the live one.
	//
	// So this test reads the DDL out of store.Migrations() — the same source
	// the runner applies, never a copy kept beside the constants, because a
	// copy edited in the same commit agrees with them and still breaks the
	// database — and compares each list value for value, byte for byte. A
	// missing entry or a missing CHECK FAILS: a lock nobody can see locking is
	// a decoration. TestTheLockCanFail is what proves this one bites.
	ddl := migrationSQL(t, "0019_policy_action_ask_and_loop_attended")
	if !strings.Contains(ddl, "policy_0019") {
		t.Fatal("the entry named 0019_policy_action_ask_and_loop_attended does not look like the table rebuild, " +
			"so the lock would be comparing the wrong DDL")
	}

	for _, c := range []struct {
		column string
		what   string
		want   []string
	}{
		{"type", "the seven policy types", valuesOf(policy.AllPolicyTypes())},
		{"target_kind", "the five target kinds", valuesOf(policy.AllTargetKinds())},
		{"action", "the three actions", valuesOf(policy.AllActions())},
		{"source", "the four sources", valuesOf(policy.AllSources())},
	} {
		missing, extra, problem := disagreement(ddl, c.column, c.want)
		if problem != "" {
			t.Errorf("migration 0019 does not close %q the way types.go does: %s", c.column, problem)
			continue
		}
		if len(missing) > 0 || len(extra) > 0 {
			t.Errorf("migration 0019's %q CHECK and types.go disagree on %s:\n"+
				"    in types.go, absent from the CHECK: %v\n"+
				"    in the CHECK, absent from types.go: %v\n"+
				"  one side will accept a row the other refuses. These two lists are one decision, "+
				"made once (PL3).", c.column, c.what, missing, extra)
		}
		t.Logf("%s CHECK agrees with types.go: %v", c.column, c.want)
	}

	// And the shipped consts stay frozen at two actions, so the widened list
	// is provably NEW shape at a new number and not an edit of a shipped one:
	// the live table admits ask, the frozen 0016 rebuild does not.
	frozen := migrationSQL(t, "0016_policy_governance")
	if strings.Contains(frozen, "'ask'") {
		t.Error("migration 0016's shipped DDL names ask; a shipped migration is immutable — widening " +
			"the vocabulary means a new number, never an edit (the rule at the top of schema.go)")
	}
}

func TestTheLockCanFail(t *testing.T) {
	// The guard on the guard. A comparison that cannot report a difference is not
	// a lock, and this one is the only thing standing between the next person's
	// rename and a policy row the database accepts and the engine refuses — so it
	// is run against a DDL deliberately broken seven ways, and every one of them
	// has to be reported. Same discipline as the schema's own
	// TestTheEquivalenceTestCanFail, for the same reason: an unfailable test is
	// not evidence of anything. The DDL is the live rebuild (0019) — the same
	// source the lock itself reads, so the guard guards the real thing.
	ddl := migrationSQL(t, "0019_policy_action_ask_and_loop_attended")

	for _, m := range []struct {
		name   string
		column string
		mutate func(string) string
	}{
		{"a type dropped from the CHECK", "type",
			func(s string) string { return strings.Replace(s, ",'trigger_access'", "", 1) }},
		{"an eighth type added to the CHECK", "type",
			func(s string) string {
				return strings.Replace(s, "'trigger_access')", "'trigger_access','chat_log')", 1)
			}},
		{"the target kinds reverted to 0005's", "target_kind",
			func(s string) string {
				return strings.Replace(s, "'all-agents','agent','loop','lane-type','mcp'",
					"'global','project','loop','agent','route'", 1)
			}},
		{"a source missing from the CHECK", "source",
			func(s string) string {
				return strings.Replace(s, "'operator','agent','mcp_server','import'", "'operator','agent'", 1)
			}},
		{"a value spelled differently", "action",
			func(s string) string { return strings.Replace(s, "'deny'", "'DENY'", 1) }},
		{"the CHECK removed entirely", "source",
			func(s string) string {
				return strings.Replace(s, "CHECK (source IN ('operator','agent','mcp_server','import'))", "", 1)
			}},
		{"a value left unquoted, which is a column reference and not a value", "type",
			func(s string) string { return strings.Replace(s, "'cost_cap'", "cost_cap", 1) }},
	} {
		mutated := m.mutate(ddl)
		if mutated == ddl {
			t.Errorf("%s: the mutation changed nothing, so this run would prove nothing", m.name)
			continue
		}
		missing, extra, problem := disagreement(mutated, m.column, mustWant(t, m.column))
		if problem == "" && len(missing) == 0 && len(extra) == 0 {
			t.Errorf("%s: the lock reported nothing against a DDL it was built to catch", m.name)
			continue
		}
		t.Logf("%s: reported missing=%v extra=%v problem=%q", m.name, missing, extra, problem)
	}

	// And the direction that matters most here: 0005 still holds the vocabulary
	// this migration replaced, so pointing the lock at the stale DDL must fail on
	// the columns that changed. This is the stale-copy failure mode — a lock
	// reading the wrong migration would otherwise pass against the old words.
	// S11's D1 changed the reading of "action" too: 0005's list was the one the
	// lock used to AGREE with, two-for-two, and the widened set made it stale
	// like the rest — which is exactly the case for reading the LAST rebuild
	// rather than any migration. An old DDL that no longer matches must fail
	// the lock, and now all four columns of 0005 do.
	stale := migrationSQL(t, "0005_policy")
	for _, column := range []string{"type", "target_kind", "source", "action"} {
		missing, extra, problem := disagreement(stale, column, mustWant(t, column))
		if problem == "" && len(missing) == 0 && len(extra) == 0 {
			t.Errorf("the lock reports nothing against 0005's %q CHECK; the stale vocabulary would pass it", column)
		}
	}
}

func TestMigration0005sVocabularyIsTheOne0016Replaces(t *testing.T) {
	// Q3, answered executably rather than in prose: this is what S4's CHECK
	// actually said and what it still says — a shipped migration is immutable,
	// so 0005 keeps its text and 0016 is the correction. Two things are
	// asserted. First, that every one of the old type values is still refused by
	// types.go today: the drift is corrected forward, not papered over, and the
	// two vocabularies share no type name. Second, that 0005 had no CHECK on
	// source at all, which is why 0016 added one — PL1's field had no
	// database-level backstop until the rebuild, and without that CHECK half of
	// the lock below would be comparing nothing.
	old := migrationSQL(t, "0005_policy")

	seven := valuesOf(policy.AllPolicyTypes())
	oldTypes, problem := checkListErr(old, "type")
	if problem != "" {
		t.Fatalf("0005's type CHECK: %s", problem)
	}
	if want := []string{"tool", "path", "network", "spend", "approval"}; !equalStrings(oldTypes, want) {
		t.Errorf("0005's type CHECK is %v, not the five the record says (%v); reconcile this test "+
			"with the migration notes — the immutability of 0005 is being asserted against something", oldTypes, want)
	}
	for _, v := range oldTypes {
		if _, rej := policy.ParsePolicyType(v); rej == nil {
			t.Errorf("%q is both 0005's vocabulary and one of the seven; the two sets were supposed to "+
				"share no type name, which is what 0016 was for", v)
		}
		if containsString(seven, v) {
			t.Errorf("%q appears in 0005's CHECK and in types.go; say which migration owns it", v)
		}
	}

	five := valuesOf(policy.AllTargetKinds())
	oldKinds, problem := checkListErr(old, "target_kind")
	if problem != "" {
		t.Fatalf("0005's target_kind CHECK: %s", problem)
	}
	for _, v := range oldKinds {
		_, rej := policy.ParseTargetKind(v)
		if containsString(five, v) {
			// `agent` and `loop` are the two words both vocabularies use, and
			// they mean the same thing in both, so they must parse.
			if rej != nil {
				t.Errorf("%q is in 0005's CHECK and in the five, yet types.go refuses it: %s", v, rej.Reason)
			}
			continue
		}
		if rej == nil {
			t.Errorf("%q is 0005's word for a target and types.go accepts it; 0016's word should be the only one", v)
		}
	}

	if _, problem := checkListErr(old, "source"); problem == "" {
		t.Error("0005 already CHECKs source, so 0016's addition is not the change the record says; " +
			"reconcile this test with the migration notes")
	}
	if _, problem := checkListErr(migrationSQL(t, "0016_policy_governance"), "source"); problem != "" {
		t.Errorf("0016 has no source CHECK (%s), so half of the lock compares nothing and PL1 has no "+
			"database-level backstop", problem)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// migrationSQL returns a migration's DDL from the same source the runner
// applies. Reading it here rather than pasting a copy beside the constants is
// the whole point: a copy edited in the same commit as the constants agrees
// with them and still breaks an upgraded database.
func migrationSQL(t *testing.T, name string) string {
	t.Helper()
	migrations := store.Migrations()
	for _, m := range migrations {
		if m.Name != name {
			continue
		}
		if strings.TrimSpace(m.SQL) == "" {
			t.Fatalf("migration %s carries no SQL to compare", name)
		}
		return m.SQL
	}
	names := make([]string, 0, len(migrations))
	for _, m := range migrations {
		names = append(names, m.Name)
	}
	t.Fatalf("store.Migrations() has no entry named %q; it holds %d entries: %v",
		name, len(migrations), names)
	return ""
}

// checkListErr pulls the literal list out of `CHECK (column IN (…))`. The
// second result is what is wrong with the constraint: absent, empty, holding
// something that is not a quoted literal, or duplicated — two constraints
// closing the same column with different lists means the column is closed
// twice, and which one the database enforces is a fact nobody reading this
// file can tell. Every one of those is a failure of the lock, never a skip: a
// vocabulary being closed somewhere the lock cannot see is exactly the drift
// this test exists to catch.
func checkListErr(ddl, column string) ([]string, string) {
	var found [][]string
	for _, m := range checkIN.FindAllStringSubmatch(ddl, -1) {
		if m[1] != column {
			continue
		}
		var out []string
		for _, item := range strings.Split(m[2], ",") {
			item = strings.TrimSpace(item)
			if len(item) < 2 || item[0] != '\'' || item[len(item)-1] != '\'' {
				return nil, fmt.Sprintf("%s's CHECK holds %q, which is not a quoted string literal", column, item)
			}
			v := item[1 : len(item)-1]
			if v == "" {
				return nil, fmt.Sprintf("%s's CHECK holds an empty string literal", column)
			}
			if containsString(out, v) {
				return nil, fmt.Sprintf("%s's CHECK holds %q twice", column, v)
			}
			out = append(out, v)
		}
		if len(out) == 0 {
			return nil, fmt.Sprintf("%s's CHECK list is empty", column)
		}
		found = append(found, out)
	}
	switch len(found) {
	case 0:
		return nil, fmt.Sprintf("no `CHECK (%s IN (…))` constraint was found; the vocabulary that closes %s "+
			"is being enforced somewhere the lock cannot see", column, column)
	case 1:
		return found[0], ""
	default:
		for i := 1; i < len(found); i++ {
			if !equalStrings(found[0], found[i]) {
				return nil, fmt.Sprintf("%s is closed by two CHECK constraints with different lists (%v and %v); "+
					"the column has one vocabulary or the lock has no single answer", column, found[0], found[i])
			}
		}
		return found[0], fmt.Sprintf("%s is closed by %d identical CHECK constraints, which is one too many "+
			"places to keep in step", column, len(found))
	}
}

// disagreement is the comparison, kept free of *testing.T so that
// TestTheLockCanFail can run it against a deliberately broken DDL.
func disagreement(ddl, column string, want []string) (missing, extra []string, problem string) {
	have, problem := checkListErr(ddl, column)
	if problem != "" {
		return nil, nil, problem
	}
	for _, w := range want {
		if !containsString(have, w) {
			missing = append(missing, w)
		}
	}
	for _, h := range have {
		if !containsString(want, h) {
			extra = append(extra, h)
		}
	}
	return missing, extra, ""
}

// mustWant is the Go side of one column's vocabulary, as plain strings. A
// column with no closed set pinned against it is a mistake in this file, and it
// says so rather than comparing against nothing.
func mustWant(t *testing.T, column string) []string {
	t.Helper()
	switch column {
	case "type":
		return valuesOf(policy.AllPolicyTypes())
	case "target_kind":
		return valuesOf(policy.AllTargetKinds())
	case "action":
		return valuesOf(policy.AllActions())
	case "source":
		return valuesOf(policy.AllSources())
	}
	t.Fatalf("no closed set is pinned against the column %q", column)
	return nil
}

// valuesOf turns one of the closed sets into the strings the DDL is compared
// against. The comparison is byte-exact: a wire value and a CHECK literal that
// differ by case or spacing are two names for one thing, which is the drift.
func valuesOf[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

// second takes the refusal out of a two-result parse.
func second[T any](_ T, rej *policy.Reject) *policy.Reject { return rej }

func containsString(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

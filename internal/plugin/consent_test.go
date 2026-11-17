// Consent is what the operator READS before authoring anything (AP5): it
// renders REQUESTS — "this agent asked for" — and never capabilities,
// because under MC4 an agent has no capability until a policy says so. The
// distinction these tests guard is the one that makes consent honest rather
// than decorative: requested and granted are DIFFERENT LISTS, the words for
// what granting means come from the tools registry (the same objects the
// model sees — one description, not a consent-package copy), and consent
// itself writes nothing — it cannot mint the very permission it is showing.
package plugin

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
)

func consentFixture(t *testing.T, name string, toolsList []string) *Agent {
	t.Helper()
	manifest := "---\nname: " + name + "\ndescription: consent fixture\nrequests:\n  tools: [" + strings.Join(toolsList, ", ") + "]\n  skills: [" + name + "/do-thing]\n  routes: [POST /report]\n---\nYou are " + name + ". Be brief and harmless.\n"
	dir := writePackage(t, name, manifest, nil, nil)
	a, rj := ImportChecked(dir, nil, at)
	if rj != nil {
		t.Fatalf("import: %s", rj.Error())
	}
	return a
}

func TestConsentRendersRequestsNotCapabilities(t *testing.T) {
	a := consentFixture(t, "consent-agent", []string{"webfetch"})
	c := RenderConsent(a, nil)
	text := c.String()
	if !strings.Contains(text, "REQUESTS") {
		t.Fatal("consent must say REQUESTS — the word the operator reads (AP5)")
	}
	// The BANNED rendering: consent that reads as a capability inventory is
	// consent that has already granted. Words composed so this file cannot
	// self-prosecute its own assertions.
	for _, banned := range []string{"capabil" + "ity", "grants", "has access"} {
		if strings.Contains(strings.ToLower(text), banned) {
			t.Fatalf("consent speaks of %q — that is the granted voice, and consent has granted nothing", banned)
		}
	}
	if !strings.Contains(text, "webfetch") || !strings.Contains(text, "not granted") {
		t.Fatalf("every request is shown AS a request, ungranted: %q", text)
	}
	if !strings.Contains(text, "disabled") || !strings.Contains(text, "aeon policy") {
		t.Fatalf("consent names the state (disabled) and the ONLY way anything becomes granted: %q", text)
	}
}

func TestRequestedAndGrantedDiffer(t *testing.T) {
	// The two lists differ even when the operator has ALREADY authored an
	// allow for this tool: consent shows the request, shows "not granted BY
	// CONSENT", and shows the policy that decides — three facts, not one
	// merged blur.
	a := consentFixture(t, "granted-differ", []string{"webfetch"})
	rows := []policy.Policy{{
		ID: "open-web", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Allow, Source: policy.Operator,
		Spec: json.RawMessage(`{"toolName":"webfetch","state":"allow"}`),
	}}
	c := RenderConsent(a, rows)
	text := c.String()
	if !strings.Contains(text, "webfetch") || !strings.Contains(text, "not granted") {
		t.Fatal("the request line is ungranted-by-consent no matter what policy says")
	}
	if !strings.Contains(text, "open-web") {
		t.Fatal("the deciding policy is shown — the operator sees what already applies, not a fiction of virgin state")
	}
	var toolLines []RequestLine
	for _, r := range c.Requests {
		if r.Kind == "tool" {
			toolLines = append(toolLines, r)
		}
		if r.Granted {
			t.Fatalf("a request line that reports itself granted is consent lying: %+v", r)
		}
	}
	if len(toolLines) != 1 || toolLines[0].Name != "webfetch" {
		t.Fatalf("the STRUCTURED answer agrees with the text: %+v", toolLines)
	}
}

func TestConsentShowsWhatGrantingMeans(t *testing.T) {
	// The description comes from the tools registry — the SAME objects the
	// model is shown. A consent-package copy would be a second description
	// that eventually lies.
	a := consentFixture(t, "means-agent", []string{"webfetch", "no-such-tool"})
	c := RenderConsent(a, nil)
	text := c.String()
	want := tools.Registry()["webfetch"].Spec.Description
	if want == "" || !strings.Contains(text, want) {
		t.Fatalf("what granting webfetch means is the registry's own sentence.\n want⊇ %q\ngot: %q", want, text)
	}
	if !strings.Contains(text, "no such tool is registered") {
		t.Fatalf("a request for an unregistered tool is the third state, not a silent line: %q", text)
	}
	if !strings.Contains(text, "You are means-agent") {
		t.Fatal("consent shows what the PROMPT says the agent to be — the body is the biggest request of all")
	}
}

func TestReimportShowsPreExistingGrants(t *testing.T) {
	// Q2's answer made visible: re-import inherits whatever the operator
	// already authored about this name, so consent must SHOW it — granting
	// again is not re-granting what exists, and revoking is not undoing
	// what consent merely lists.
	a := consentFixture(t, "reimported-consent", []string{"webfetch"})
	rows := []policy.Policy{
		{ID: "narrow-me", Type: policy.ToolAccess,
			Target: policy.Target{Kind: policy.AgentKind, ID: "reimported-consent"},
			Action: policy.Deny, Source: policy.Operator,
			Spec: json.RawMessage(`{"toolName":"webfetch","state":"deny"}`)},
		{ID: "not-mine", Type: policy.ToolAccess,
			Target: policy.Target{Kind: policy.AgentKind, ID: "somebody-else"},
			Action: policy.Deny, Source: policy.Operator,
			Spec: json.RawMessage(`{"toolName":"webfetch","state":"deny"}`)},
	}
	c := RenderConsent(a, rows)
	text := c.String()
	if !strings.Contains(text, "narrow-me") {
		t.Fatalf("policies targeting THIS agent are shown (re-import inherits them): %q", text)
	}
	if strings.Contains(text, "not-mine") {
		t.Fatal("another agent's policies are not this agent's consent — no noise, no implication")
	}
}

func TestConsentWritesNothing(t *testing.T) {
	// The whole point rendered as a shape: the consent file cannot open a
	// store, cannot call the write boundary, cannot mint a policy. If this
	// test ever fails, consent has become the grant button it must not be.
	src, err := os.ReadFile("consent.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"St" + "ore", ".Ad" + "d(", "Bind" + "StateStore", "da" + "tabase"} {
		if strings.Contains(string(src), banned) {
			t.Fatalf("consent.go mentions %q — consent is a rendering, and renderings do not write", banned)
		}
	}
	// The spy half: bind the fake ledger, count everything it holds, render
	// consent fully — and the counters must not move. Zero writes, observed,
	// not just un-mentionable by construction.
	f := newFakeStore()
	BindStateStore(f)
	t.Cleanup(func() { BindStateStore(nil); Reset() })
	a := consentFixtureNoBind(t, "spy-agent", []string{"webfetch"})
	creates, audit := f.creates, len(f.auditLog)
	_ = RenderConsent(a, nil).String()
	if f.creates != creates || len(f.auditLog) != audit {
		t.Fatal("consent MOVED THE LEDGER — a preview that writes is a grant button wearing preview clothes")
	}
}

// consentFixtureNoBind builds an agent while the fake is bound WITHOUT
// ledger expectations beyond what ImportChecked itself does — the spy
// counters are read AFTER import, around the render only.
func consentFixtureNoBind(t *testing.T, name string, toolsList []string) *Agent {
	t.Helper()
	manifest := "---\nname: " + name + "\ndescription: spy fixture\nrequests:\n  tools: [" + strings.Join(toolsList, ", ") + "]\n---\nYou are " + name + ".\n"
	dir := writePackage(t, name, manifest, nil, nil)
	a, rj := ImportChecked(dir, nil, at)
	if rj != nil {
		t.Fatalf("import: %s", rj.Error())
	}
	return a
}

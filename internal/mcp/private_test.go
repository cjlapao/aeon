package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// T5: the call path agrees with the registry, and the error is right.
//
//	unknown tool, never denied.
//
// `denied` confirms the tool exists — an information leak dressed as a
// refusal: a peer learns the reddit agent has a reddit server, which is
// exactly the fact a private namespace exists to not tell it. And `unknown`
// is not a lie: from this agent's point of view the tool does not exist —
// not in its list, not in its prompt, not reachable. The honest answer and
// the safe answer are the same one.

// privateFixture is the brief's shape: one agent owns fakeserver privately;
// a peer exists beside it with its own legitimate config.
func privateFixture() Config {
	return Config{
		Public: []ServerConfig{{
			Name:      "alpha",
			Transport: TransportConfig{Type: TransportStdio, Command: "unused"},
			Tools:     []ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}},
		}},
		Agents: map[string]AgentConfig{
			"reddit-agent": {Private: []ServerConfig{{
				Name:      "fakeserver",
				Transport: TransportConfig{Type: TransportStdio, Command: "unused"},
				Tools: []ToolConfig{
					{Name: "search", Rerun: "safe", Effect: "local"},
					{Name: "post", Rerun: "unsafe", Effect: "irreversible"},
					{Name: "mystery"},
				},
			}}},
			"peer-agent": {Declares: []string{"alpha.search"}},
		},
	}
}

func mustCallUnknown(t *testing.T, r *Registry, agent, qualified string) CallOutcome {
	t.Helper()
	res, err := r.Call(context.Background(), agent, qualified, nil, nil, time.Now())
	if err != nil {
		t.Fatalf("Call(%s, %s): unexpected transport error %v", agent, qualified, err)
	}
	return res
}

// TestPrivateCallAnswersUnknownNotDenied is D2. Asserted on the refusal's
// WORDS, not on its code: `denied` anywhere in the answer would confirm the
// tool exists.
func TestPrivateCallAnswersUnknownNotDenied(t *testing.T) {
	r := mustRegistry(t, privateFixture())

	res := mustCallUnknown(t, r, "peer-agent", "fakeserver.search")
	if !res.IsError {
		t.Fatal("the peer's call was not a refusal at all")
	}
	if !strings.HasPrefix(res.Content, "unknown tool:") {
		t.Errorf("refusal = %q, want it to lead with `unknown tool:` (D2)", res.Content)
	}
	for _, leaky := range []string{"denied", "refused", "denied by policy", "not allowed", "unauthorized", "private"} {
		if strings.Contains(strings.ToLower(res.Content), leaky) {
			t.Errorf("refusal contains %q — that word confirms the tool exists (R2): %q", leaky, res.Content)
		}
	}
}

// TestRefusalDoesNotRevealExistence is the sharper half: the peer's answer
// for a REAL private tool is byte-identical to the answer for a tool nobody
// ever configured. Any difference — "no such server", a suggestion, a
// shorter sentence — is a disclosure channel. A suggestion engine over tool
// names is exactly that, so none exists, and none may be added.
func TestRefusalDoesNotRevealExistence(t *testing.T) {
	r := mustRegistry(t, privateFixture())

	private := mustCallUnknown(t, r, "peer-agent", "fakeserver.search")
	neverExisted := mustCallUnknown(t, r, "peer-agent", "nosuchserver.search")

	// The refusal may contain exactly two things: a fixed framing and the
	// name the CALLER ITSELF supplied (quoting the caller's own words back
	// is not a disclosure channel — they already had them). Pinned byte
	// for byte, because "byte-identical" is only meaningful against a fixed
	// expectation, and the framing must be the SAME framing:
	if want := "unknown tool: fakeserver.search"; private.Content != want {
		t.Errorf("refusal for a REAL private tool = %q, want exactly %q — anything more is a disclosure channel (R2)", private.Content, want)
	}
	if want := "unknown tool: nosuchserver.search"; neverExisted.Content != want {
		t.Errorf("refusal for a non-existent tool = %q, want exactly %q", neverExisted.Content, want)
	}
	if a, b := strings.ReplaceAll(private.Content, "fakeserver.search", "<name>"),
		strings.ReplaceAll(neverExisted.Content, "nosuchserver.search", "<name>"); a != b {
		t.Errorf("the two framings differ — a caller can tell a real private tool from a typo: %q vs %q", a, b)
	}
	if private.IsError != neverExisted.IsError {
		t.Errorf("IsError differs (%v vs %v) — the flag is a disclosure channel too", private.IsError, neverExisted.IsError)
	}
	// And nothing about the rest of the configuration rides along.
	for _, leaky := range []string{"reddit-agent", "did you mean", "available tools", "post", "mystery", "alpha"} {
		if strings.Contains(private.Content, leaky) {
			t.Errorf("refusal mentions %q — the refusal itself disclosed configuration: %q", leaky, private.Content)
		}
	}
	// A stranger (agent absent from config entirely) gets the identical
	// answer — the honest answer and the safe answer are the same one.
	stranger := mustCallUnknown(t, r, "nobody-at-all", "fakeserver.search")
	if stranger.Content != private.Content {
		t.Errorf("stranger and peer answers differ: %q vs %q", stranger.Content, private.Content)
	}
}

// TestPrivateToolNeverInPeerPrompt asserts on the ASSEMBLED PROMPT (the
// specs the provider is shown), not the registry — the second surface that
// could quietly grow its own list.
func TestPrivateToolNeverInPeerPrompt(t *testing.T) {
	bin := fakeServerPath(t)
	// Attach real listings so descriptions exist — an absent description
	// would hide whether the tool itself leaked.
	srvCfg := privateFixture()
	srvCfg.Agents["reddit-agent"].Private[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r2, err := New(srvCfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := dialFakeAt(t, srvCfg.Agents["reddit-agent"].Private[0])
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	remote, err := c.ListTools(ctx)
	cancel()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if err := r2.AttachClient("fakeserver", c); err != nil {
		t.Fatalf("AttachClient: %v", err)
	}
	if err := r2.ApplyListings("fakeserver", remote); err != nil {
		t.Fatalf("ApplyListings: %v", err)
	}

	peerPrompt := []string{}
	for _, s := range r2.ToolSpecsFor("peer-agent") {
		peerPrompt = append(peerPrompt, s.Name+"|"+s.Description)
	}
	joined := strings.Join(peerPrompt, "\n")
	for _, forbidden := range []string{"fakeserver", "Search Reddit"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("peer's assembled prompt contains %q — invisible failed at the prompt surface even if the registry held:\n%s", forbidden, joined)
		}
	}

	// The owner's prompt DOES carry it (labelled) — the obvious one, and
	// the assertion that catches a registry which hides everything.
	ownerPrompt := []string{}
	for _, s := range r2.ToolSpecsFor("reddit-agent") {
		ownerPrompt = append(ownerPrompt, s.Name+"|"+s.Description)
	}
	ownerJoined := strings.Join(ownerPrompt, "\n")
	if !strings.Contains(ownerJoined, "fakeserver.search") {
		t.Error("the owning agent cannot see its own private tool in its prompt")
	}
	if !strings.Contains(ownerJoined, "untrusted") || !strings.Contains(ownerJoined, "Search for things.") {
		t.Error("the owner's copy of its private tool's description is unlabelled (T4 must apply to private tiers too)")
	}
}

// TestTheThreeRefusalKindsAreDistinguishable pins the transcript table:
// three facts, three lead-ins. (The third — S10's `refused: … · ToolAccess`
// — arrives with T7 and must not collide with these two.)
func TestTheThreeRefusalKindsAreDistinguishable(t *testing.T) {
	r := mustRegistry(t, privateFixture())

	unknown := mustCallUnknown(t, r, "peer-agent", "fakeserver.search")
	unavailable := mustCallUnknown(t, r, "reddit-agent", "fakeserver.mystery")

	if unknown.Content == unavailable.Content {
		t.Fatal("unknown and unavailable render identically")
	}
	if !strings.HasPrefix(unknown.Content, "unknown tool:") {
		t.Errorf("unknown refusal lead: %q", unknown.Content)
	}
	if !strings.HasPrefix(unavailable.Content, "unavailable:") {
		t.Errorf("unavailable refusal lead: %q", unavailable.Content)
	}
	// And the third kind's lead-in is reserved: no current refusal may
	// impersonate the policy sentence's shape.
	if strings.Contains(unknown.Content, "ToolAccess") || strings.Contains(unavailable.Content, "ToolAccess") {
		t.Error("a non-policy refusal names ToolAccess — S10's sentence is its own (T7)")
	}
}

// TestOwningAgentCanStillSeeAndCallIt: the obvious one, and the one that
// catches an implementation that "achieves" invisibility by hiding
// everything from everyone.
func TestOwningAgentCanStillSeeAndCallIt(t *testing.T) {
	cfg := privateFixture()
	bin := fakeServerPath(t)
	cfg.Agents["reddit-agent"].Private[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Agents["reddit-agent"].Private[0])
	if err := r.AttachClient("fakeserver", c); err != nil {
		t.Fatalf("AttachClient: %v", err)
	}
	res, err := r.Call(context.Background(), "reddit-agent", "fakeserver.search", json.RawMessage(`{"q":"aeon"}`), allowPolicy("reddit-agent", "fakeserver.search"), time.Now())
	if err != nil || res.IsError {
		t.Fatalf("owner's own call failed: %+v, %v", res, err)
	}
	if !strings.Contains(res.Content, `{"results":["alpha","beta"]}`) || !strings.Contains(res.Content, "untrusted") {
		t.Errorf("owner's result wrong or unlabelled: %q", res.Content)
	}
}

// TestNoFilterOverASharedList is T5's guard (the task's own validation):
// if this task needed a filter over a shared list, T1's shape was wrong —
// the grep proves none was added. These identifiers are the shapes a filter
// takes; their absence is the property that peers were never handed a list
// to filter.
func TestNoFilterOverASharedList(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	banned := []string{"allTools", "AllTools", "sharedList", "if visible(", "visibleTo(", "filterTools(", "everyTool"}
	positive := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		// Comments DOCUMENT the ban (registry.go's header literally says
		// "no AllTools function exists") — they must not trip it. T3's
		// lesson, applied before it had to be learned twice.
		var codeLines []string
		for _, line := range strings.Split(string(src), "\n") {
			if t := strings.TrimSpace(line); strings.HasPrefix(t, "//") {
				continue
			}
			codeLines = append(codeLines, line)
		}
		code := strings.Join(codeLines, "\n")
		for _, pat := range banned {
			if strings.Contains(code, pat) {
				t.Errorf("%s contains %q — a shared list plus a filter is the shape MC2 forbids (R1)", e.Name(), pat)
			}
		}
		if strings.Contains(string(src), "ToolsFor") {
			positive++
		}
	}
	if positive == 0 {
		t.Error("no file even mentions ToolsFor — the accessor is not being used, which is worse than a filter")
	}
}

// TestPeerCannotProbePrivateWithDeclaredPublicNames is MC12's call-side
// shadow: a peer declaring (config-valid, public) names cannot use the
// call path to probe the private tier — the qualified name's SERVER decides
// the lookup, and `fakeserver` lives only under reddit-agent.
func TestPeerCannotProbePrivateWithDeclaredPublicNames(t *testing.T) {
	r := mustRegistry(t, privateFixture())
	res := mustCallUnknown(t, r, "peer-agent", "fakeserver.post")
	if !strings.HasPrefix(res.Content, "unknown tool:") {
		t.Errorf("peer probe of a private tool answered %q", res.Content)
	}
	// The same NAME on the public tier is unaffected: namespacing is by
	// server, and `alpha.search` remains the peer's.
	if st, _, _ := r.Availability("peer-agent", "alpha.search"); st != AvailabilityAvailable {
		t.Error("the private tier's existence changed the public tier's answers")
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// T7: MC4 — the reason this slice has NO security model of its own.
//
// A grant is an operator-authored ToolAccess policy. There is no second
// grant mechanism: a registry ACL here would be a second place for the
// authorship rule to drift, and the drift direction is the dangerous one —
// an ACL that grants is an allow nobody authored as a policy.

func declaredConfig() Config {
	return Config{
		Public: []ServerConfig{{
			Name:      "alpha",
			Transport: TransportConfig{Type: TransportStdio, Command: "unused"},
			Tools:     []ToolConfig{{Name: "search", Rerun: "safe", Effect: "local"}},
		}},
		Agents: map[string]AgentConfig{
			"agent-a": {Declares: []string{"alpha.search"}},
		},
	}
}

func wireAlpha(t *testing.T, cfg *Config) (*Registry, *Client) {
	t.Helper()
	bin := fakeServerPath(t)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r := mustRegistry(t, *cfg)
	c := dialFakeAt(t, cfg.Public[0])
	if err := r.AttachClient("alpha", c); err != nil {
		t.Fatalf("AttachClient: %v", err)
	}
	return r, c
}

// TestRevokingThePolicyRemovesTheTool is MC4's promise made executable:
// the ONLY removal is the policy's. Same Registry object, same config file
// on disk, nothing reloaded — the tool lives while the allow row is in the
// set and is gone the moment it is not.
func TestRevokingThePolicyRemovesTheTool(t *testing.T) {
	cfg := declaredConfig()
	r, c := wireAlpha(t, &cfg)
	ctx := context.Background()

	res, err := r.Call(ctx, "agent-a", "alpha.search", nil, allowPolicy("agent-a", "alpha.search"), time.Now())
	if err != nil || res.IsError || res.AskRequired {
		t.Fatalf("granted call failed: %+v %v", res, err)
	}
	views := r.GrantView("agent-a", allowPolicy("agent-a", "alpha.search"), time.Now())
	if v := grantViewOf(views, "alpha.search"); v == nil || v.State != GrantAvailable {
		t.Fatalf("granted tool not available in the grant view: %+v", views)
	}

	// Revoke: pass the set WITHOUT the allow. That's it. That's the only
	// way there is (TestExactlyOneWayToRevoke pins the exclusivity).
	res, err = r.Call(ctx, "agent-a", "alpha.search", nil, nil, time.Now())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "MC10") {
		t.Errorf("revoked tool still callable: %+v", res)
	}
	v := grantViewOf(r.GrantView("agent-a", nil, time.Now()), "alpha.search")
	if v == nil || v.State != GrantUnavailable || !strings.Contains(v.Reason, "MC10") {
		t.Errorf("revoked tool in grant view = %+v, want unavailable/MC10", v)
	}

	// And the server confirms: nothing arrived after the revocation.
	n, _ := c.CallTool(ctx, "calls", json.RawMessage(`{"name":"search"}`))
	if n.Text != "1" {
		t.Errorf("server heard %s invocations, want 1 (the pre-revocation one)", n.Text)
	}
}

// TestCapabilityOutlivesGrant is the deliberate two-view split, stated so
// nobody "fixes" it: ToolsFor is the CAPABILITY view (config facts — what
// this agent declared and we classified); GrantView is the AUTHORITY view
// (what the frozen policy set at `at` permits). The prompt shows capability
// (frozen with the step — a policy event mid-step must not flicker the
// model's tool list); the call and the CLI consult authority. `aeon mcp
// list` without --include-refused hides only what capability never had;
// with it, revoked tools render as unavailable with their reason.
func TestCapabilityOutlivesGrant(t *testing.T) {
	cfg := declaredConfig()
	r, _ := wireAlpha(t, &cfg)

	inCapability := false
	for _, tl := range r.ToolsFor("agent-a") {
		if tl.Qualified == "alpha.search" {
			inCapability = true
		}
	}
	if !inCapability {
		t.Fatal("the capability view lost a declared, classified tool — that is the prompt flickering on policy churn")
	}
	v := grantViewOf(r.GrantView("agent-a", nil, time.Now()), "alpha.search")
	if v == nil || v.State == GrantAvailable {
		t.Fatalf("ungranted tool reads available in the authority view: %+v", v)
	}
}

// TestExactlyOneWayToRevoke is the task's point, asserted by shape: the
// Registry type exposes NO second door. Two ways to revoke means one of
// them gets forgotten and a revoked tool stays live. Positive-controlled
// like every grep in this package.
func TestExactlyOneWayToRevoke(t *testing.T) {
	// Every file, not just registry.go — the methods live across three
	// files and a one-file grep would certify silence it never listened to.
	var code strings.Builder
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		code.WriteString(stripComments(string(src)))
		code.WriteString("\n")
	}
	codeStr := code.String()
	// Methods on *Registry that could revoke outside policy:
	revokish := regexp.MustCompile(`(?m)^func \(\w+ \*Registry\) (Revoke\w*|Remove\w*|Ban\w*|Block\w*|Disable\w*|Drop\w*|Forget\w*|Clear\w*)\(`)
	hits := revokish.FindAllStringSubmatch(codeStr, -1)
	for _, h := range hits {
		t.Errorf("(*Registry).%s is a second revocation door — revocation is the policy's (MC4)", h[1])
	}
	// Positive control: the accessor grep must see REAL methods or it is
	// matching nothing (T1's (?m) lesson).
	seen := regexp.MustCompile(`(?m)^func \(\w+ \*Registry\) ([A-Z]\w*)\(`).FindAllStringSubmatch(codeStr, -1)
	if len(seen) == 0 {
		t.Fatal("no exported *Registry methods matched the pattern at all — the revokish grep above was matching nothing")
	}
	names := map[string]bool{}
	for _, h := range seen {
		names[h[1]] = true
	}
	for _, must := range []string{"ToolsFor", "GrantView", "Availability", "AttachClient", "ApplyListings"} {
		if !names[must] {
			t.Errorf("expected method %s absent — did the accessor move? (this grep's positive control)", must)
		}
	}
}

// TestRegistryHoldsNoGrantState is scope item 2's grep: no field of any
// type in the package holds policies, allow-lists or ACLs. `policy.Policy`
// may appear as a PARAMETER (the frozen set arrives per call) and must
// NEVER appear as a struct field — state lives in S10's store, the caller
// brings the set, and this package forgets it the moment the call returns.
func TestRegistryHoldsNoGrantState(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	inStruct, paramsSeen := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		inStructBody := false
		for _, line := range strings.Split(stripComments(string(src)), "\n") {
			t0 := strings.TrimSpace(line)
			switch {
			case strings.Contains(t0, "struct {"):
				inStructBody = true
			case inStructBody && strings.HasPrefix(t0, "}"):
				inStructBody = false
			case inStructBody && strings.Contains(t0, "policy.Policy"):
				inStruct++
				t.Errorf("%s: struct field holds policy state (%q) — grants live in S10's store and arrive per call (MC4)", e.Name(), t0)
			}
			if strings.Contains(t0, "ps []policy.Policy") {
				paramsSeen++
			}
		}
	}
	if inStruct != 0 {
		t.Errorf("%d policy-state fields found", inStruct)
	}
	if paramsSeen == 0 {
		t.Fatal("no policy.Policy PARAMETER anywhere either — then the package is not consulting policy at all, which is worse")
	}
	// No second source vocabulary for permission, under any name.
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		code := stripComments(string(src))
		for _, banned := range []string{"acl", "ACL", "allowlist", "allowList", "AllowList", "whitelist", "grantMap", "grants map"} {
			if strings.Contains(code, banned) {
				t.Errorf("%s contains %q — a second grant mechanism in the package MC4 says has none", e.Name(), banned)
			}
		}
	}
}

// TestConfiguredServersToolWithNoAllowIsNotAvailable is MC10 named in full:
// a server offering a tool does not make it available; THE POLICY does.
// Everything else being perfect — configured server, attached client,
// reported tool, classified, declared — is still not an allow.
func TestConfiguredServersToolWithNoAllowIsNotAvailable(t *testing.T) {
	cfg := declaredConfig()
	r, c := wireAlpha(t, &cfg)
	res, err := r.Call(context.Background(), "agent-a", "alpha.search", nil, nil, time.Now())
	if err != nil || !res.IsError {
		t.Fatalf("offered = granted? %+v %v", res, err)
	}
	if !strings.HasPrefix(res.Content, "unavailable:") || !strings.Contains(res.Content, "MC10") {
		t.Errorf("MC10 refusal shape: %q", res.Content)
	}
	n, _ := c.CallTool(context.Background(), "calls", json.RawMessage(`{"name":"search"}`))
	if n.Text != "0" {
		t.Errorf("the server heard %s calls under no allow", n.Text)
	}
}

// TestPrivateToolStillEvaluatesPolicy is scope item 1's last clause:
// invisibility is structural (T5) AND the call still evaluates — invisible
// is not a substitute for allowed. The owner's private tool is refused
// without an allow rule exactly like a public one.
func TestPrivateToolStillEvaluatesPolicy(t *testing.T) {
	cfg := privateFixture()
	bin := fakeServerPath(t)
	cfg.Agents["reddit-agent"].Private[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r := mustRegistry(t, cfg)
	c := dialFakeAt(t, cfg.Agents["reddit-agent"].Private[0])
	if err := r.AttachClient("fakeserver", c); err != nil {
		t.Fatalf("AttachClient: %v", err)
	}
	res, err := r.Call(context.Background(), "reddit-agent", "fakeserver.search", nil, nil, time.Now())
	if err != nil || !res.IsError || !strings.Contains(res.Content, "MC10") {
		t.Fatalf("private tool callable without an allow: %+v %v — invisible is not a substitute for allowed", res, err)
	}
	// Same call, with the allow: runs. The only difference is the policy.
	res, err = r.Call(context.Background(), "reddit-agent", "fakeserver.search", nil,
		allowPolicy("reddit-agent", "fakeserver.search"), time.Now())
	if err != nil || res.IsError {
		t.Fatalf("private tool with allow failed: %+v %v", res, err)
	}
}

// TestAtIsTakenOncePerStep is S10's T8 discipline re-earned here: the clock
// is a parameter, and within one step (one `at`) two calls decide
// identically — they cannot straddle an expiry, because there is no second
// clock reading to straddle it with.
func TestAtIsTakenOncePerStep(t *testing.T) {
	cfg := declaredConfig()
	r, _ := wireAlpha(t, &cfg)

	expiry := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	allow := allowPolicy("agent-a", "alpha.search")
	allow[0].NotAfter = &expiry

	stepInside := expiry.Add(-time.Nanosecond)
	stepAfter := expiry.Add(time.Nanosecond)

	// Two calls in the step inside the validity window: both allowed.
	for i := 0; i < 2; i++ {
		res, err := r.Call(context.Background(), "agent-a", "alpha.search", nil, allow, stepInside)
		if err != nil || res.IsError {
			t.Fatalf("call %d in the in-force step refused: %+v %v", i+1, res, err)
		}
	}
	// Two calls in the step past it: both refused, identically — the
	// expired allow is dropped and the default takes over (PL5), never a
	// phantom denial and never a linger.
	var first string
	for i := 0; i < 2; i++ {
		res, _ := r.Call(context.Background(), "agent-a", "alpha.search", nil, allow, stepAfter)
		if !res.IsError {
			t.Fatalf("call %d in the expired step still ran", i+1)
		}
		if i == 0 {
			first = res.Content
		} else if res.Content != first {
			t.Errorf("two calls in ONE step decided differently: %q vs %q", first, res.Content)
		}
	}

	// And the clock is never read inside the package for a decision: every
	// policy.Evaluate call site takes `at` as given.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	seenEval := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		code := stripComments(string(src))
		for _, line := range strings.Split(code, "\n") {
			if strings.Contains(line, "policy.Evaluate(") {
				seenEval = true
				if strings.Contains(line, "time.Now") {
					t.Errorf("%s: policy decision reads the wall clock (%q) — `at` arrives from the caller (S10 purity)", e.Name(), strings.TrimSpace(line))
				}
			}
		}
	}
	if !seenEval {
		t.Fatal("no policy.Evaluate call site exists to check — is anyone consulting S10 at all?")
	}
}

// TestCoreIsExemptAndSaysSo records the deviation from the task line
// "every core and public tool" — core evaluation is REAL, it lives in
// S11's step gate (which judges the frozen set per step), and a second
// evaluation here would be a second place for the verdict to disagree with
// the gate that spawned the step. The registry's call path answers `unknown
// tool` for core names because core calls belong to the step toolset;
// GrantView marks them with that truth rather than inventing an MCP grant
// row for a tool the compile-time design already owns.
func TestCoreIsExemptAndSaysSo(t *testing.T) {
	r := mustRegistry(t, declaredConfig())
	res, err := r.Call(context.Background(), "agent-a", "read", nil, nil, time.Now())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if want := "unknown tool: read"; res.Content != want {
		t.Errorf("core name through the MCP path = %q, want exactly %q", res.Content, want)
	}
	coreMarked := false
	for _, v := range r.GrantView("agent-a", nil, time.Now()) {
		if strings.HasPrefix(v.Qualified, "core:") {
			t.Errorf("core tools must render by name (%q), not with a namespace nobody uses", v.Qualified)
		}
		if v.Qualified == "read" && v.State == GrantAvailable {
			coreMarked = true
		}
	}
	if !coreMarked {
		t.Error("read is not marked available in the grant view — S11's gate owns its verdict, and the view must say so")
	}
}

// TestGrantViewHidesPrivateFromPeers: the authority view is as invisible as
// the capability view — a second listing surface that leaks is still a
// leak.
func TestGrantViewHidesPrivateFromPeers(t *testing.T) {
	r := mustRegistry(t, privateFixture())
	for _, v := range r.GrantView("peer-agent", nil, time.Now()) {
		if strings.Contains(v.Qualified, "fakeserver") {
			t.Errorf("authority view leaked private tool %q to a peer", v.Qualified)
		}
	}
	ownerSaw := false
	for _, v := range r.GrantView("reddit-agent", nil, time.Now()) {
		if v.Qualified == "fakeserver.search" {
			ownerSaw = true
		}
	}
	if !ownerSaw {
		t.Error("the owner's authority view lost its own private tool")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────

func grantViewOf(views []GrantView, qualified string) *GrantView {
	for i := range views {
		if views[i].Qualified == qualified {
			return &views[i]
		}
	}
	return nil
}

func stripComments(src string) string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "//") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

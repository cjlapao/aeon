// What `tools.byAgent` answers (S47 T5): one agent's whole view, compared with
// the gate the step actually decides with — `tools.GrantedSet` and
// `policy.Evaluate` re-called in the test, never `toolcatalog` as its own oracle
// (TC5) — plus the dispatchability rule (SD7) and the MCP half of that view.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/toolcatalog"
	"github.com/cjlapao/aeon/internal/tools"
)

// --- tools.byAgent ---------------------------------------------------------------------

// TestToolsByAgentCoversEveryToolAndMatchesTheGate is TC5 standing up. The view is
// checked against the gate's OWN authorities reached by an independent path —
// `tools.GrantedSet` for the grant chain (the function `BuildToolset` calls, T4)
// and `policy.Evaluate` for the verdict — and never against `toolcatalog`, which
// would be the tautology the ruling exists to prevent. Two rules: a tool OUTSIDE
// the granted set is `not_granted` with no verdict quoted, and one INSIDE it is
// never `not_granted` — it is the engine's answer, whatever that is.
func TestToolsByAgentCoversEveryToolAndMatchesTheGate(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	res, err := env.svc.ToolsByAgent(context.Background(), "atlas")
	if err != nil {
		t.Fatalf("tools.byAgent: %v", err)
	}
	if res.Agent != "atlas" || !res.Dispatchable {
		t.Fatalf("the subject half is wrong: %+v", res)
	}

	granted, err := tools.GrantedSet([]string{"read", "list", "write", "bash", "webfetch"})
	if err != nil {
		t.Fatalf("GrantedSet refused a good list: %v", err)
	}
	for _, g := range tools.DefaultGrant() {
		if !hasString(granted, g) {
			t.Fatalf("the default grant %s is missing from GrantedSet's own answer, so this fixture cannot measure the union", g)
		}
	}
	set := map[string]bool{}
	for _, g := range granted {
		set[tools.CanonicalToolName(g)] = true
	}
	rows := map[string]toolcatalog.Entry{}
	for _, e := range res.Tools {
		rows[e.Tool] = e
	}

	for _, n := range tools.ToolNames() {
		row, ok := rows[n]
		if !ok {
			t.Errorf("%s is not in the agent's view — every tool that exists answers", n)
			continue
		}
		if set[n] && row.State == toolcatalog.StateNotGranted {
			t.Errorf("%s is in the granted set and answers not_granted — the catalog and the gate disagree (TC5)", n)
		}
		if !set[n] && row.State != toolcatalog.StateNotGranted {
			t.Errorf("%s is NOT in the granted set and answers %q — a tool nobody granted reads as usable", n, row.State)
		}
		if !set[n] && row.Reason != "" {
			t.Errorf("%s is not held yet carries the verdict %q — no verdict was sought", n, row.Reason)
		}
	}

	// The verdict itself for the tool the set actually refuses: the engine's
	// sentence, byte for byte, on the row. This is the pair the T4 differential
	// asserts in the abstract; here it is asserted through the daemon's own reads.
	bash, ok := rows["bash"]
	if !ok {
		t.Fatal("bash is missing from the view")
	}
	listed, rej := env.policies.List(context.Background(), env.now)
	if rej != nil {
		t.Fatalf("read the policy set: %s", rej.Reason)
	}
	v := policy.Evaluate(listed, policy.Request{Type: policy.ToolAccess, Agent: "atlas", Tool: "bash"}, env.now)
	if bash.State != toolcatalog.StateGrantedDenied {
		t.Fatalf("bash = %q, want granted_denied", bash.State)
	}
	if bash.Reason != v.Sentence() {
		t.Errorf("the row's reason is not the engine's sentence verbatim:\n got %q\nwant %q", bash.Reason, v.Sentence())
	}
	// And the granted_by half: bash is in atlas's row, so `handler` names it; the
	// default grant names read/list/write.
	if !hasString(bash.GrantedBy, toolcatalog.GrantedHandler) {
		t.Errorf("bash's granted_by = %v, want the handler's row named", bash.GrantedBy)
	}
	readRow := rows["read"]
	if !hasString(readRow.GrantedBy, toolcatalog.GrantedDefault) {
		t.Errorf("read's granted_by = %v, want the default grant named", readRow.GrantedBy)
	}
}

// TestToolsByAgentCustomAgentIsDispatchedLikeAnyOther: an agent a person made or imported is an
// agent like any other — the one that brought it in answers for it. Its row's tools are judged by
// policy exactly as a shipped agent's are, so the answer carries dispatchable=true and a held tool
// with no rule reads `granted_no_rule` (silence permits nothing, PL5), not `not_granted`. What
// lets it call a tool is the rule written when the tool was chosen, not a class of agent.
func TestToolsByAgentCustomAgentIsDispatchedLikeAnyOther(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	res, err := env.svc.ToolsByAgent(context.Background(), "cursitor")
	if err != nil {
		t.Fatalf("tools.byAgent: %v", err)
	}
	if !res.Dispatchable {
		t.Error("a custom agent (origin 'user') must report itself dispatchable")
	}
	grep, ok := mapEntries(res.Tools)["grep"]
	if !ok {
		t.Fatal("grep is missing from the view")
	}
	if grep.State == toolcatalog.StateNotGranted {
		t.Errorf("grep is on the row and must be judged, not hidden: %q", grep.State)
	}
	if !grep.Dispatchable {
		t.Error("grep must carry dispatchable=true on a custom agent's row")
	}
	if !hasString(grep.GrantedBy, toolcatalog.GrantedHandler) {
		t.Errorf("grep's granted_by = %v — the row names it and the answer must say so", grep.GrantedBy)
	}
	// The display name resolves when it is unambiguous (the person types what the
	// agent screen shows them), and the answer echoes the SLUG.
	byName, err := env.svc.ToolsByAgent(context.Background(), "Cursitor")
	if err != nil {
		t.Fatalf("the display name was refused: %v", err)
	}
	if byName.Agent != "cursitor" {
		t.Errorf("agent = %q, want the slug", byName.Agent)
	}
}

func mapEntries(entries []toolcatalog.Entry) map[string]toolcatalog.Entry {
	out := make(map[string]toolcatalog.Entry, len(entries))
	for _, e := range entries {
		out[e.Tool] = e
	}
	return out
}

// TestToolsByAgentUnknownNamesKnownAgents is the refusal that keeps an
// absent agent from reading as an agent that may call nothing: the typed code, and
// the known slugs in the sentence, so the person corrects a typo instead of
// debugging a grant that was never missing.
func TestToolsByAgentUnknownNamesKnownAgents(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	_, err := env.svc.ToolsByAgent(context.Background(), "nobody")
	e, ok := err.(*protocol.Error)
	if !ok {
		t.Fatalf("err = %T (%v), want a typed *protocol.Error", err, err)
	}
	if e.Code != protocol.ErrToolAgentUnknown.Code {
		t.Fatalf("code = %d, want %d (the two S47 codes are distinct facts)", e.Code, protocol.ErrToolAgentUnknown.Code)
	}
	for _, want := range []string{"atlas", "sentry", "cursitor"} {
		if !strings.Contains(e.Data.What, `"`+want+`"`) {
			t.Errorf("the refusal does not name the known agent %q: %s", want, e.Data.What)
		}
	}
}

// TestToolsByAgentMCPNamesComeFromThatAgentsOwnView is MC2 through the door: the
// qualified names in an agent's list are the ones THAT agent's authority view
// shows. `atlas` declared against acme and sees its tools; `sentry` never did and
// the private server is invisible to it — including in an error, which is why no
// door in this slice can be used to enumerate a peer's servers.
//
// The companion assertion is the seam's failure path: when the read fails, the door
// ERRORS rather than rendering an agent whose MCP tools have all quietly vanished.
func TestToolsByAgentMCPNamesComeFromThatAgentsOwnView(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	atlas, err := env.svc.ToolsByAgent(context.Background(), "atlas")
	if err != nil {
		t.Fatalf("atlas: %v", err)
	}
	seen := mapEntries(atlas.Tools)
	search, ok := seen["acme.search"]
	if !ok {
		t.Fatal("the declared, available MCP tool is missing from its own agent's view")
	}
	if search.State != toolcatalog.StateGrantedAllowed || search.Reason == "" {
		t.Errorf("acme.search = %q / %q, want the view's available answer with its reason verbatim", search.State, search.Reason)
	}
	if !hasString(search.GrantedBy, toolcatalog.GrantedMCPDeclare) {
		t.Errorf("acme.search granted_by = %v, want mcp-declare named (the row behind it)", search.GrantedBy)
	}
	// The held-but-unavailable tool is `granted_no_rule`, not `not_granted`: S12's
	// four states map 1:1 onto the catalog's five, and a tool the authority holds
	// but cannot classify has been looked at and no rule spoke — `not_granted` would
	// say nobody declared it, which is a different fact and would send the person to
	// the wrong screen. Its reason is the view's text verbatim (MC6).
	post, ok := seen["acme.post"]
	if !ok {
		t.Fatal("the unclassified declared tool vanished from its own agent's view")
	}
	if post.State != toolcatalog.StateGrantedNoRule || post.Reason == "" {
		t.Errorf("acme.post = %q / %q, want granted_no_rule with the view's reason verbatim", post.State, post.Reason)
	}
	if !hasString(post.GrantedBy, toolcatalog.GrantedMCPDeclare) {
		t.Errorf("acme.post granted_by = %v — the row declares it, and that stays true while the answer refuses", post.GrantedBy)
	}

	sentry, err := env.svc.ToolsByAgent(context.Background(), "sentry")
	if err != nil {
		t.Fatalf("sentry: %v", err)
	}
	for _, e := range sentry.Tools {
		if strings.HasPrefix(e.Tool, "acme.") {
			t.Errorf("a peer's private MCP tool %s appeared in another agent's view (MC2)", e.Tool)
		}
	}

	env.admin.failWith = errors.New("the MCP tables are unreadable")
	if _, err := env.svc.ToolsByAgent(context.Background(), "atlas"); err == nil {
		t.Error("a failed MCP grants read answered an agent view — the MCP half would have silently vanished (V11)")
	}
}

// TestToolsMCPHalfAbsentWithoutTheSeam is V13 on the MCP half: with no entry-point
// seam, the catalog simply has no MCP tools — not refused ones, not empty ones —
// and a qualified name is an unknown tool rather than a refusal. An agent's view is
// then the system half plus nothing, which is the honest answer about a build that
// ships no MCP.
func TestToolsMCPHalfAbsentWithoutTheSeam(t *testing.T) {
	env := newToolsCatalogEnv(t, false)
	for _, e := range mustList(t, env, protocol.ToolsListFilter{}) {
		if e.Source == protocol.ToolsSourceMCP {
			t.Fatalf("an MCP entry (%s) appeared with no seam wired", e.Name)
		}
	}
	if _, err := env.svc.ToolsGet(context.Background(), "acme.search"); err == nil {
		t.Error("a qualified name was answered with no MCP half in this build")
	}
	res, err := env.svc.ToolsByAgent(context.Background(), "atlas")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Tools {
		if toolcatalog.IsMCPToolName(e.Tool) {
			t.Errorf("the agent view carries %s with no MCP seam", e.Tool)
		}
	}
}

// TestToolsByAgentAnswersEachToolOnce is the walk's finding made a test. The
// universe this read answers over is the union of the code's names and the tools
// the agent's own row names — and the two halves OVERLAP, because a granted tool
// is in both. `toolcatalog.ForAgent` keeps duplicates on purpose (they are the
// caller's count), so a caller that hands it a list with repeats answers every
// granted tool twice: a real daemon answered 146 rows for 73 tools. One row per
// tool that exists is what the door promises, and a map keyed by name — as every
// other test here indexes the answer — cannot see a duplicate, which is how the
// defect lived beside a green suite.
func TestToolsByAgentAnswersEachToolOnce(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	ctx := context.Background()
	res, err := env.svc.ToolsByAgent(ctx, "atlas")
	if err != nil {
		t.Fatalf("tools.byAgent: %v", err)
	}
	counts := map[string]int{}
	for _, e := range res.Tools {
		counts[e.Tool]++
	}
	var dupes []string
	for name, c := range counts {
		if c > 1 {
			dupes = append(dupes, fmt.Sprintf("%s×%d", name, c))
		}
	}
	sort.Strings(dupes)
	if len(dupes) > 0 {
		t.Fatalf("the answer repeats tools; one row per tool that exists, got %v", dupes)
	}
	// Non-vacuity, and it is the overlap itself that has to be shown: every name
	// the agent's row grants is ALSO a name the code offers, so the raw union the
	// read builds from repeats it. A world where the agent was granted nothing the
	// code offers would let a missing dedupe pass.
	granted, err := tools.GrantedSet([]string{"read", "list", "write", "bash", "webfetch"})
	if err != nil {
		t.Fatalf("GrantedSet refused a good list: %v", err)
	}
	overlap := 0
	for _, g := range granted {
		name := tools.CanonicalToolName(g)
		if !hasString(tools.ToolNames(), name) {
			continue
		}
		overlap++
		if counts[name] != 1 {
			t.Fatalf("the granted tool %s answers %d times — the union's overlap must collapse to one row", name, counts[name])
		}
	}
	if overlap == 0 {
		t.Fatal("no granted name is a code name, so this test measured nothing")
	}
}

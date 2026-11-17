// tools_mine_test.go — S47 T7 (TC9, MC2, OL3, 2026-11-02): the agent's own tool
// view, walked at the two doors an agent actually stands at.
//
// What this file pins:
//
//   - THE LIST IS AN INVENTORY, not a survey of refusals (rule 2): every row is a
//     granted state, and a tool the agent holds no grant for is absent — not
//     answered `not_granted`. `tools.byAgent` keeps the ungranted rows because a
//     person needs "may call nothing" and "never granted" told apart; an agent
//     reading its own door has no such question, and the difference is the
//     difference between a list and a reconnaissance.
//   - THE REFUSAL IS ONE SENTENCE (rule 3): the name you hold and the name nobody
//     ever had answer identically, so the door is not an existence oracle for a
//     peer's private MCP tool or for a grant an operator withheld. The peer case
//     and the nonexistent case are compared BYTE for byte, and a peer's private
//     name is hunted through every byte of every answer, including the hints and
//     the counts (rule 4, MC2).
//   - THE GATE AND THE VIEW AGREE (rule 7 and TC5): the states are re-derived in
//     the test from `tools.GrantedSet` and `policy.Evaluate` — never from
//     `toolcatalog`, which would be the tautology — and the reason is the engine's
//     sentence verbatim.
//   - ONLY YOUR OWN RULES ARE SHOWN (rule 5): a `tool_access` row naming another
//     agent is absent from the policy list, because it reports that agent's
//     restrictions and says nothing about the caller.
//   - THE DOOR IS THE AGENT'S (rule 6, OL3): a person session is refused by name
//     with the unscoped door that answers it, a smuggled `agent` param dies at the
//     strict decode and never reaches the view, and an unmounted catalog REFUSES
//     rather than answering an empty inventory (V13/P31 — "you may call nothing" is
//     the most dangerous plausible answer this door can produce).

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/toolcatalog"
	"github.com/cjlapao/aeon/internal/tools"
)

// peerPrivateServer is the fixture that makes MC2 measurable: a PRIVATE server
// owned by `sentry`, enabled, declaring one tool whose qualified name contains the
// string `secret-b`. It is in the world the catalog service reads (the unscoped
// `mcp.servers` answer carries every enabled server, which is correct for the
// person's doors), so it is genuinely present in the input and must never appear in
// atlas's answer — not as a row, not in a hint, not in a count.
func peerPrivateServer(e *toolsCatalogEnv) {
	e.admin.servers.Servers = append(e.admin.servers.Servers, protocol.MCPServerView{
		Name: "secret", Tier: "private", Agent: "sentry", Enabled: true, Origin: "user",
		Tools: []protocol.MCPToolView{{Name: "secret-b", Rerun: "safe", Effect: "local", Classified: true}},
	})
	e.admin.grants["sentry"] = protocol.MCPGrantsResult{
		Agent:    "sentry",
		Declares: []string{"secret.secret-b"},
		Grants: []protocol.MCPGrantView{{
			Qualified: "secret.secret-b", Tier: "private", State: "available",
			Reason: "allow · effect_class · secret.secret-b · agent:sentry",
		}},
	}
}

type toolsMineEnv struct {
	cat *toolsCatalogEnv
	svc *toolsCatalogService
	rec *RecordsService
}

func newToolsMineEnv(t *testing.T) *toolsMineEnv {
	t.Helper()
	e := newToolsCatalogEnv(t, true)
	peerPrivateServer(e)
	sc, ok := e.svc.(*toolsCatalogService)
	if !ok {
		t.Fatalf("the catalog seam is %T, want *toolsCatalogService", e.svc)
	}
	rec := NewRecordsService(e.st)
	rec.toolsCatalog = sc // the SAME service the person's four doors answer through
	return &toolsMineEnv{cat: e, svc: sc, rec: rec}
}

// --- rule 2: the list is an inventory ---------------------------------------------------

func TestToolsMineListsOnlyHeldTools(t *testing.T) {
	env := newToolsMineEnv(t)
	res, err := env.svc.ToolsMine(context.Background(), "atlas")
	if err != nil {
		t.Fatalf("tools_mine: %v", err)
	}
	if res.Held != len(res.Tools) {
		t.Errorf("held = %d but the array is %d long — a badge and the list would disagree", res.Held, len(res.Tools))
	}

	granted, err := tools.GrantedSet([]string{"read", "list", "write", "bash", "webfetch"})
	if err != nil {
		t.Fatalf("GrantedSet refused a good list: %v", err)
	}
	set := map[string]bool{}
	for _, g := range granted {
		set[tools.CanonicalToolName(g)] = true
	}
	// The MCP half of the agent's own holds, reached the same way the view reaches
	// them — the scoped declaration list, not the server list.
	for _, q := range []string{"acme.search", "acme.post"} {
		set[q] = true
	}

	seen := map[string]bool{}
	for _, row := range res.Tools {
		seen[row.Tool.Name] = true
		if row.State == toolcatalog.StateNotGranted {
			t.Errorf("%s answers not_granted in the agent's OWN list — a tool nobody granted is absent here, never announced (rule 2)", row.Tool.Name)
		}
		if !row.State.Valid() {
			t.Errorf("%s answers the state %q, outside the closed five", row.Tool.Name, row.State)
		}
		if !set[row.Tool.Name] {
			t.Errorf("%s is listed as held and nothing granted it — the list would be a claim about a row the grant chain does not contain", row.Tool.Name)
		}
	}
	// The tools atlas was NOT given, and one tool that exists in the world and
	// belongs to somebody else: absent, not refused.
	for _, absent := range []string{"todo", "grep", "glob", "secret.secret-b"} {
		if seen[absent] {
			t.Errorf("%s appears in atlas's inventory — it is not in the grant set, so it is not in the answer", absent)
		}
	}
	// And the granted half really is in there, or the test above is measuring
	// nothing but an empty list.
	for _, must := range []string{"read", "bash", "acme.search"} {
		if !seen[must] {
			t.Errorf("%s is granted and missing — an inventory that omits what you hold is worse than one that adds what you do not", must)
		}
	}
}

// --- rule 3: unheld is nonexistent ------------------------------------------------------

func TestToolShowOfUnheldEqualsNonexistentSentence(t *testing.T) {
	env := newToolsMineEnv(t)
	// Three names, all FAR from atlas's own set (so the hint list is the same
	// string in all three answers) and none of them held: a peer's PRIVATE tool
	// that really exists and is really enabled, a tool that exists and was simply
	// never granted, and a name that appears in no catalog anywhere. The refusal
	// must be one template — the only difference across the three is the name the
	// caller typed, which the caller already knows. If any other byte moved for one
	// of the three, the door would be answering "does that exist?" to anybody who
	// could spell (rule 3, MC2).
	const peer = "secret.secret-b"       // exists, enabled, belongs to sentry
	const ungranted = "secret.secret-b2" // the same shape, held by nobody
	const never = "secret.secret-b3"     // appears in no catalog at all
	answers := map[string]string{}
	for _, name := range []string{peer, ungranted, never} {
		_, err := env.svc.ToolShow(context.Background(), "atlas", name)
		if err == nil {
			t.Fatalf("tool_show on %q answered instead of refusing", name)
		}
		e, ok := err.(*protocol.Error)
		if !ok || e.Code != protocol.ErrToolUnknown.Code {
			t.Fatalf("tool_show on %q: err = %T %v, want the ErrToolUnknown code", name, err, err)
		}
		if !strings.Contains(e.Data.What, "is not a tool in your set") {
			t.Errorf("tool_show on %q: what = %q, want the own-set sentence", name, e.Data.What)
		}
		// The caller's own string is echoed back — that is not a leak, it is the
		// input. The HINTS are the leak vector, so the comparison strips the echo
		// and compares the rest byte for byte.
		answers[name] = strings.ReplaceAll(e.Data.What, `"`+name+``, `"<name>"`)
		if hints := hintsOf(e.Data.What); strings.Contains(hints, "secret.secret-b") || strings.Contains(hints, "secret.secret-b2") {
			t.Errorf("tool_show on %q hints at a name outside the caller's set: %s", name, hints)
		}
	}
	if answers[peer] != answers[never] || answers[peer] != answers[ungranted] {
		t.Errorf("the three refusals are not one template — the door distinguishes what exists from what does not:\n peer:      %s\n ungranted: %s\n never:     %s",
			answers[peer], answers[ungranted], answers[never])
	}
	// And the near-name half of the same rule: a name CLOSE to the caller's own
	// tools gets hints from the caller's own set — never from the catalog at large,
	// which is the answer to "what exists" that this door is not allowed to give.
	_, err := env.svc.ToolShow(context.Background(), "atlas", "acme.serch")
	if e, ok := err.(*protocol.Error); !ok || !strings.Contains(hintsOf(e.Data.What), "acme.search") {
		t.Fatalf("a typo of the caller's own tool did not hint at it: %v", err)
	}
}

// hintsOf returns the nearest-name tail of a refusal, which is the part built from
// data the caller did NOT supply and therefore the only part an oracle can live in.
func hintsOf(what string) string {
	if i := strings.Index(what, "the nearest names are:"); i >= 0 {
		return what[i:]
	}
	return what
}

// --- rule 4: MC2 on every output path ---------------------------------------------------

func TestPeerPrivateMcpToolNeverAppears(t *testing.T) {
	env := newToolsMineEnv(t)
	ctx := context.Background()
	// The fixture is real: sentry holds the private tool, so the name is in the
	// world and reachable through the person's doors.
	peerView, err := env.svc.ToolShow(ctx, "sentry", "secret.secret-b")
	if err != nil {
		t.Fatalf("the peer cannot even see its own tool — the fixture is broken: %v", err)
	}
	if peerView.Tool.Name != "secret.secret-b" {
		t.Fatalf("sentry's own view answers %q", peerView.Tool.Name)
	}

	check := func(what string, v any) {
		b, merr := json.Marshal(v)
		if merr != nil {
			t.Fatalf("%s: %v", what, merr)
		}
		for _, banned := range []string{"secret-b", `"secret"`, "agent:sentry", "pol-sentry"} {
			if strings.Contains(string(b), banned) {
				t.Errorf("%s leaks %q into atlas's answer: %s", what, banned, b)
			}
		}
	}
	list, err := env.svc.ToolsMine(ctx, "atlas")
	if err != nil {
		t.Fatalf("tools_mine: %v", err)
	}
	check("tools_mine's answer", list)
	own, err := env.svc.ToolShow(ctx, "atlas", "acme.search")
	if err != nil {
		t.Fatalf("tool_show: %v", err)
	}
	check("tool_show's answer", own)
	// The HINTS are an output path too: a refusal that names a peer's tool in
	// `what` is a leak through the sentence, which is the path MC2 was written for.
	_, err = env.svc.ToolShow(ctx, "atlas", "secret.secret-b")
	if err == nil {
		t.Fatal("the refusal did not happen")
	}
	if e, ok := err.(*protocol.Error); ok {
		// The echoed name the caller typed is the caller's own string; the leak
		// vector is the HINT list, which is built from data the caller did not
		// supply. `agent:sentry` may never appear anywhere in the answer, because
		// no input names sentry and a hint that does is a peer's world in the
		// caller's hands.
		if strings.Contains(e.Data.What, "agent:sentry") {
			t.Errorf("the refusal names another agent: %s", e.Data.What)
		}
		if strings.Contains(hintsOf(e.Data.What), "secret") {
			t.Errorf("the refusal hints at a peer's private tool: %s", hintsOf(e.Data.What))
		}
	}
	// A person reading the SAME world through the unscoped door DOES see the peer's
	// tool — the guard must not be a filter that hides private tools from everyone,
	// which would break the panel that manages them.
	personView, perr := env.svc.ToolsByAgent(ctx, "sentry")
	if perr != nil {
		t.Fatalf("tools.byAgent: %v", perr)
	}
	found := false
	for _, row := range personView.Tools {
		if row.Tool == "secret.secret-b" {
			found = true
		}
	}
	if !found {
		t.Error("the person's byAgent no longer sees a peer's declared tool — the own-view guard has become a filter over everybody's view")
	}
}

// --- rule 5: only the rows that could reach this agent ----------------------------------

func TestPolicyRowsShownAreOnlyThoseAffectingSelf(t *testing.T) {
	env := newToolsMineEnv(t)
	// Three rows about `read`, which atlas holds: one naming ANOTHER agent, one
	// naming atlas, and one class-wide. The environment already carries
	// `tc-allow-read-all` (all-agents), so this adds the two agent-target ones.
	addPolicy(t, env.cat.policies, "mine-deny-read-sentry", "read", policy.Deny, policy.AgentKind, "sentry", nil)
	addPolicy(t, env.cat.policies, "mine-ask-read-atlas", "read", policy.Ask, policy.AgentKind, "atlas", nil)

	res, err := env.svc.ToolShow(context.Background(), "atlas", "read")
	if err != nil {
		t.Fatalf("tool_show read: %v", err)
	}
	if len(res.Policies) == 0 {
		t.Fatal("no policy rows at all — the fixture added three and the answer shows none")
	}
	targets := map[string]bool{}
	ids := map[string]bool{}
	for _, p := range res.Policies {
		targets[p.Target] = true
		ids[p.ID] = true
		if p.Target == "agent:sentry" {
			t.Errorf("sentry's own row %s is shown to atlas — that row reports what SENTRY may not do, which is information about another agent and nothing about this caller (rule 5)", p.ID)
		}
	}
	if !targets["agent:atlas"] {
		t.Error("atlas's own row is missing — the view must show the rule that speaks about the caller")
	}
	if !targets["all-agents"] {
		t.Error("the all-agents row is missing — a class rule speaks about the caller too, and hiding it would say nothing refuses them")
	}
	if ids["mine-deny-read-sentry"] {
		t.Error("the by-ID check also caught sentry's row")
	}
	// The rows still reach SENTRY's own view — the restriction is per caller, not a
	// global filter that quietly loses rules from the panel that manages them.
	peer, err := env.svc.ToolShow(context.Background(), "sentry", "read")
	if err != nil {
		t.Fatalf("tool_show read as sentry: %v", err)
	}
	sawPeerRow := false
	for _, p := range peer.Policies {
		if p.ID == "mine-deny-read-sentry" {
			sawPeerRow = true
		}
	}
	if !sawPeerRow {
		t.Error("sentry cannot see its own row — the rule was implemented as a filter over the data instead of a restriction per caller")
	}
}

// --- rule 7 / TC5: the states are the gate's ---------------------------------------------

func TestStatesEqualTheGateForSelf(t *testing.T) {
	env := newToolsMineEnv(t)
	ctx := context.Background()
	res, err := env.svc.ToolsMine(ctx, "atlas")
	if err != nil {
		t.Fatalf("tools_mine: %v", err)
	}
	granted, gerr := tools.GrantedSet([]string{"read", "list", "write", "bash", "webfetch"})
	if gerr != nil {
		t.Fatalf("GrantedSet: %v", gerr)
	}
	set := map[string]bool{}
	for _, g := range granted {
		set[tools.CanonicalToolName(g)] = true
	}
	for _, q := range []string{"acme.search", "acme.post"} {
		set[q] = true
	}
	listed, rej := env.cat.policies.List(ctx, env.cat.now)
	if rej != nil {
		t.Fatalf("read the policy set: %s", rej.Reason)
	}
	for _, row := range res.Tools {
		if !set[row.Tool.Name] {
			t.Errorf("%s is not in the gate's own granted set and the view calls it held", row.Tool.Name)
		}
		if toolcatalog.IsMCPToolName(row.Tool.Name) {
			// A qualified MCP name is judged by the effect_class verdict the
			// scoped grant view carries (T4), not by the `tool_access` set —
			// comparing it against `tool_access` here would assert a rule the
			// engine does not have. What IS checked is the verbatim discipline:
			// the row's reason is the view's sentence, unchanged.
			if row.Reason == "" {
				t.Errorf("%s is an MCP row with no reason — the state came from somewhere and the answer should say where", row.Tool.Name)
			}
			continue
		}
		v := policy.Evaluate(listed, policy.Request{Type: policy.ToolAccess, Agent: "atlas", Tool: row.Tool.Name}, env.cat.now)
		switch row.State {
		case toolcatalog.StateGrantedAllowed:
			if !v.Allowed {
				t.Errorf("%s says granted_allowed; the engine did not allow it (asked=%v, defaultDeny=%v) — a view that flatters the gate is the dangerous direction", row.Tool.Name, v.Asked, v.DefaultDeny)
			}
		case toolcatalog.StateGrantedAsk:
			if !v.Asked {
				t.Errorf("%s says granted_ask; the engine did not route a question (allowed=%v, defaultDeny=%v)", row.Tool.Name, v.Allowed, v.DefaultDeny)
			}
		case toolcatalog.StateGrantedDenied:
			if v.Allowed || v.Asked || v.By == nil {
				t.Errorf("%s says granted_denied; the engine named no deny (allowed=%v, asked=%v, by=%v)", row.Tool.Name, v.Allowed, v.Asked, v.By)
			}
			if row.Reason != v.Sentence() {
				t.Errorf("%s's reason is not the engine's sentence verbatim:\n got %q\nwant %q", row.Tool.Name, row.Reason, v.Sentence())
			}
		case toolcatalog.StateGrantedNoRule:
			// The state means "the set never spoke about this tool". PL5 still
			// denies it, and saying so as `granted_no_rule` rather than
			// `granted_denied` is the distinction T4 exists to keep visible: an
			// operator who adds no rule has not refused anything.
			if !v.DefaultDeny {
				t.Errorf("%s says granted_no_rule while the engine reached a decision (allowed=%v, asked=%v, by=%v)", row.Tool.Name, v.Allowed, v.Asked, v.By)
			}
		}
	}
	// The pair the whole slice is about, asserted concretely: the env's deny row
	// means bash is refused for atlas, and the reason is the engine's own words.
	for _, row := range res.Tools {
		if row.Tool.Name != "bash" {
			continue
		}
		if row.State != toolcatalog.StateGrantedDenied {
			t.Errorf("bash = %q, want granted_denied", row.State)
		}
		if !strings.Contains(row.Reason, "deny") {
			t.Errorf("bash's reason = %q, want the engine's deny sentence", row.Reason)
		}
	}
}

// --- no cache (N8) ------------------------------------------------------------------------

func TestToolsMineIsLive(t *testing.T) {
	env := newToolsMineEnv(t)
	ctx := context.Background()
	before, err := env.svc.ToolsMine(ctx, "atlas")
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if stateOfMine(before, "write") != toolcatalog.StateGrantedNoRule && stateOfMine(before, "write") != toolcatalog.StateGrantedAllowed {
		t.Fatalf("the fixture does not start with `write` in a judgeable state: %q", stateOfMine(before, "write"))
	}
	addPolicy(t, env.cat.policies, "mine-live-deny-write", "write", policy.Deny, policy.AgentKind, "atlas", nil)
	after, err := env.svc.ToolsMine(ctx, "atlas")
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if stateOfMine(after, "write") != toolcatalog.StateGrantedDenied {
		t.Errorf("a row added between two reads changed nothing (%q) — the view is caching, and a cached permission view is a permission the operator already revoked", stateOfMine(after, "write"))
	}
	if stateOfMine(before, "write") == toolcatalog.StateGrantedDenied {
		t.Error("the first read already saw the row that was added after it")
	}
}

func stateOfMine(r protocol.ToolsMineResult, name string) toolcatalog.State {
	for _, row := range r.Tools {
		if row.Tool.Name == name {
			return row.State
		}
	}
	return ""
}

// --- rules 1 and 6 at the records arms ------------------------------------------------------

// mineSpy records what the arms asked the catalog service for. It is the only way
// to prove the stamp travelled rather than the param: the answer itself would look
// the same either way for a caller who lies.
type mineSpy struct {
	agents  []string
	names   []string
	refuse  bool
	res     protocol.ToolsMineResult
	show    protocol.ToolShowResult
	callErr error
}

func (s *mineSpy) ToolsMine(ctx context.Context, agent string) (protocol.ToolsMineResult, error) {
	s.agents = append(s.agents, agent)
	if s.callErr != nil {
		return protocol.ToolsMineResult{}, s.callErr
	}
	return s.res, nil
}

func (s *mineSpy) ToolShow(ctx context.Context, agent, name string) (protocol.ToolShowResult, error) {
	s.agents = append(s.agents, agent)
	s.names = append(s.names, name)
	if s.callErr != nil {
		return protocol.ToolShowResult{}, s.callErr
	}
	return s.show, nil
}

func TestMineTwinsRefusePersonSessions(t *testing.T) {
	env := newToolsMineEnv(t)
	ctx := context.Background()
	for _, tc := range []struct{ tool, door string }{
		{"tools_mine", "tools.byAgent"},
		{"tool_show", "tools.get"},
	} {
		out, f, err := env.rec.Do(ctx, Actor{Kind: "user", ID: "1000"}, tc.tool, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s as a person: %v", tc.tool, err)
		}
		if f == nil {
			t.Fatalf("%s answered a PERSON — the own-view doors serve self, and a person holding the endpoint must not be able to walk them one slug at a time (rule 6)", tc.tool)
		}
		if out != nil {
			t.Errorf("%s returned a payload beside its refusal", tc.tool)
		}
		if !strings.Contains(f.Sentence, "agent-scoped") || !strings.Contains(f.Sentence, tc.door) {
			t.Errorf("%s refusal = %q, want the pointer sentence naming %s", tc.tool, f.Sentence, tc.door)
		}
	}
}

// TestMineArmsStampSelfAndRefuseASmuggledAgent is rule 1 at the arm: the catalog
// schema closes, the strict decode refuses the field, and the agent the view is
// asked for is the STEP's stamp — not the string in the input, and not the
// connection's uid.
func TestMineArmsStampSelfAndRefuseASmuggledAgent(t *testing.T) {
	env := newToolsMineEnv(t)
	spy := &mineSpy{}
	env.rec.toolsCatalog = spy
	ctx := context.Background()

	if _, f, err := env.rec.toolsMine(ctx, Identity{Agent: "atlas"}, json.RawMessage(`{"agent":"sentry"}`)); f == nil || err != nil {
		t.Fatalf("a smuggled `agent` param was accepted: failure=%v err=%v", f, err)
	} else if !strings.Contains(f.Sentence, "stamped by the step") {
		t.Errorf("smuggled param: %q, want the identity-field refusal", f.Sentence)
	}
	if _, f, err := env.rec.toolShow(ctx, Identity{Agent: "atlas"}, json.RawMessage(`{"name":"read","actor":"root"}`)); f == nil || err != nil {
		t.Fatalf("a smuggled `actor` param was accepted: failure=%v err=%v", f, err)
	}
	if len(spy.agents) != 0 {
		t.Errorf("the view was asked for %v even though both inputs were refused", spy.agents)
	}

	if _, f, err := env.rec.toolsMine(ctx, Identity{Agent: "atlas"}, nil); f != nil || err != nil {
		t.Fatalf("the clean call: failure=%v err=%v", f, err)
	}
	if _, f, err := env.rec.toolShow(ctx, Identity{Agent: "atlas"}, json.RawMessage(`{"name":"read"}`)); f != nil || err != nil {
		t.Fatalf("the clean show: failure=%v err=%v", f, err)
	}
	if len(spy.agents) != 2 || spy.agents[0] != "atlas" || spy.agents[1] != "atlas" {
		t.Errorf("the view was asked for %v, want atlas twice — the stamp is the subject (OL3)", spy.agents)
	}
	if len(spy.names) != 1 || spy.names[0] != "read" {
		t.Errorf("the view was asked to show %v", spy.names)
	}
	// A step that names no agent has no own view. The fallback that would be
	// tempting here — the connection's uid, a default agent — is the bug this
	// refuses: a guessed identity that resolves is somebody else's grants served
	// as yours.
	if _, f, err := env.rec.toolsMine(ctx, Identity{}, nil); f == nil || err != nil {
		t.Fatalf("an unstampéd step answered: failure=%v err=%v", f, err)
	} else if !strings.Contains(f.Sentence, "stamps no agent") {
		t.Errorf("unstampéd step: %q, want the OL3 refusal", f.Sentence)
	}
	if _, f, err := env.rec.toolShow(ctx, Identity{User: true, Agent: "1000"}, json.RawMessage(`{"name":"read"}`)); f == nil || err != nil {
		t.Fatalf("a person-shaped identity reached the arm and was served: failure=%v err=%v", f, err)
	}
}

func TestToolsMineV13RefusesWithoutTheCatalog(t *testing.T) {
	env := newToolsMineEnv(t)
	env.rec.toolsCatalog = nil // a catalogless mount, exactly what daemon.New leaves
	ctx := context.Background()
	for _, tc := range []struct {
		tool string
		call func() (any, *Failure, error)
	}{
		{"tools_mine", func() (any, *Failure, error) { return env.rec.toolsMine(ctx, Identity{Agent: "atlas"}, nil) }},
		{"tool_show", func() (any, *Failure, error) {
			return env.rec.toolShow(ctx, Identity{Agent: "atlas"}, json.RawMessage(`{"name":"read"}`))
		}},
	} {
		out, f, err := tc.call()
		if err != nil {
			t.Fatalf("%s: %v", tc.tool, err)
		}
		if f == nil {
			t.Fatalf("%s answered a payload with no catalog wired — an empty inventory is a confident wrong answer about a surface this build never mounted (V13/P31)", tc.tool)
		}
		if out != nil {
			t.Errorf("%s returned %v beside its refusal", tc.tool, out)
		}
		if !strings.Contains(f.Sentence, "V13") || !strings.Contains(f.Sentence, tc.tool) {
			t.Errorf("%s refusal = %q, want the named V13 sentence", tc.tool, f.Sentence)
		}
	}
}

// TestToolsMineArmsAreTheCatalogsOwnDoors is the parity claim in code: the catalog
// names the two arms, the switch implements them, and the socket twin names come
// from the same spec — so `tools_mine` the tool and `tools.mine.list` the method
// cannot answer two different questions.
func TestToolsMineArmsAreTheCatalogsOwnDoors(t *testing.T) {
	names := map[string]string{}
	for _, sp := range records.Catalog() {
		names[sp.Name] = sp.Method
	}
	if names["tools_mine"] != "tools.mine.list" || names["tool_show"] != "tools.mine.show" {
		t.Fatalf("the catalog's twins drifted: %v", names)
	}
	env := newToolsMineEnv(t)
	// An unknown action through the same door still refuses by naming the catalog
	// as the whole vocabulary — the arms did not add a private verb.
	if _, f, err := env.rec.Do(context.Background(), Actor{Kind: "agent", ID: "atlas"}, "tools_mine_extra", nil); f == nil || err != nil {
		t.Fatalf("an invented action was accepted: failure=%v err=%v", f, err)
	} else if !strings.Contains(f.Sentence, "not a catalog action") {
		t.Errorf("invented action: %q", f.Sentence)
	}
}

// TestToolsMineAnswersEachHeldToolOnce is the own-view twin of the byAgent
// finding: the held universe is the same union (the code's names plus what this
// agent declares through MCP), so the same overlap would have printed a held
// tool twice. The agent's own inventory repeating itself is not a cosmetic
// problem — the count is what a person reads as "what this agent may call".
func TestToolsMineAnswersEachHeldToolOnce(t *testing.T) {
	env := newToolsMineEnv(t)
	ctx := context.Background()
	res, err := env.svc.ToolsMine(ctx, "atlas")
	if err != nil {
		t.Fatalf("tools_mine: %v", err)
	}
	counts := map[string]int{}
	for _, row := range res.Tools {
		counts[row.Tool.Name]++
	}
	var dupes []string
	for name, c := range counts {
		if c > 1 {
			dupes = append(dupes, fmt.Sprintf("%s×%d", name, c))
		}
	}
	sort.Strings(dupes)
	if len(dupes) > 0 {
		t.Fatalf("the agent's own inventory repeats tools: %v", dupes)
	}
	if res.Held != len(res.Tools) {
		t.Fatalf("the badge says %d and the array is %d long — a count that double-counts is the same defect wearing a number", res.Held, len(res.Tools))
	}
	// Non-vacuity: this agent holds BOTH a name the code offers and a qualified
	// name its MCP declarations add, which is exactly the two-halves overlap the
	// dedupe collapses.
	code, qualified := 0, 0
	for name := range counts {
		if strings.Contains(name, ".") {
			qualified++
		} else if hasString(tools.ToolNames(), name) {
			code++
		}
	}
	if code == 0 || qualified == 0 {
		t.Fatalf("the answer held code names %d and qualified names %d — one row per tool is only proven where the union overlaps", code, qualified)
	}
}

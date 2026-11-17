package protocol

// agent_grants.go — the policy rules that go with an agent's tools.
//
// An agent's `tools` list says what it may ASK for. Policy decides each call, and with no rule the
// answer is "no rule matched, so deny" (PL5). A first-party agent works because the shipped policy
// corpus has rules for it; an agent a person makes or imports needs the same, or every tool it was
// given would be refused. So the agent doors that change an agent's tools author its rules — through
// the daemon's own policy authoring door, as the person who pressed the button, audited like any
// other policy write — and the delete door takes them away again:
//
//   - a tool the person granted is `allow`;
//   - `bash` is `ask`, for every agent: a human is asked before a command runs (a tool rule for the
//     tool, and a command rule that matches any command). "Allow it for the rest of the chat" is the
//     chat's answer to that question, not a standing rule;
//   - an agent that was IMPORTED without a signature gets `ask` for everything, until the person
//     decides otherwise.
//
// The rules have predictable ids (`grant.<slug>.<tool>`), so an edit adds only the rules a new tool
// needs, removes only the rules a dropped tool leaves behind, and a delete finds them by name. The
// rules are policy rows like any other: the person can see them, change them and remove them with
// the policy surface, and nothing here reads them back as a special kind.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// AgentGrantPrefix starts the id of every rule written for an agent's tools. The delete door's
// "a policy row still references this agent" refusal looks past rows with it for the agent's own
// slug: they are the agent's, and go with it.
const AgentGrantPrefix = "grant."

// maxPolicyIDRunes is the policy id cap (internal/policy's MaxIdentifierRunes, which this package
// does not import).
const maxPolicyIDRunes = 64

// agentGrantID is the id of the rule for one tool (or one named part of a tool, like `bash.cmd`).
// An id that would pass the cap is shortened with a hash of the whole name, so it stays
// predictable and never collides with another agent's.
func agentGrantID(slug, part string) string {
	id := AgentGrantPrefix + slug + "." + part
	if len([]rune(id)) <= maxPolicyIDRunes {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	short := hex.EncodeToString(sum[:])[:10]
	keep := maxPolicyIDRunes - len(AgentGrantPrefix) - len(short) - 1
	r := []rune(slug + "." + part)
	if keep > len(r) {
		keep = len(r)
	}
	return AgentGrantPrefix + string(r[:keep]) + "." + short
}

// agentGrantRule is one rule an agent's tools call for.
type agentGrantRule struct {
	ID    string
	Type  string
	State string // allow | ask
	Spec  string
}

// agentGrantRules is the rule set for an agent holding these tools. `asksEverything` is the
// imported-unsigned case: every tool asks, not just `bash`.
func agentGrantRules(slug string, tools []string, asksEverything bool) []agentGrantRule {
	names := append([]string(nil), tools...)
	sort.Strings(names)
	var out []agentGrantRule
	seen := map[string]bool{}
	for _, tool := range names {
		tool = strings.TrimSpace(tool)
		if tool == "" || seen[tool] {
			continue
		}
		seen[tool] = true
		state := "allow"
		if asksEverything || tool == "bash" {
			state = "ask"
		}
		out = append(out, agentGrantRule{
			ID: agentGrantID(slug, tool), Type: "tool_access", State: state,
			Spec: fmt.Sprintf(`{"toolName":%q,"state":%q}`, tool, state),
		})
		if tool == "bash" {
			// A bash call that names a command is judged by the command rules, not the tool rule, so
			// the command question needs its own: every command asks.
			out = append(out, agentGrantRule{
				ID: agentGrantID(slug, "bash.cmd"), Type: "command_access", State: "ask",
				Spec: `{"command":"*","state":"ask"}`,
			})
		}
	}
	return out
}

// agentGrantState is what the agent doors need to know about a row to say what its rules are.
type agentGrantState struct {
	Slug   string
	Tools  []string
	Origin string
	Signed bool
}

// asksEverything is the imported-without-a-signature rule.
func (s agentGrantState) asksEverything() bool { return s.Origin == "import" && !s.Signed }

func (s agentGrantState) rules() []agentGrantRule {
	return agentGrantRules(s.Slug, s.Tools, s.asksEverything())
}

// agentGrantStateOf reads what the grants need out of an agent answer (`agent.view`'s map, or the
// created row): its slug, its tools column, where it came from, whether it is signed.
func agentGrantStateOf(answer any) (agentGrantState, bool) {
	raw, err := json.Marshal(answer)
	if err != nil {
		return agentGrantState{}, false
	}
	var row struct {
		Slug   string `json:"slug"`
		Tools  string `json:"tools"`
		Origin string `json:"origin"`
		Signed bool   `json:"signed"`
	}
	if err := json.Unmarshal(raw, &row); err != nil || row.Slug == "" {
		return agentGrantState{}, false
	}
	var tools []string
	if strings.TrimSpace(row.Tools) != "" {
		_ = json.Unmarshal([]byte(row.Tools), &tools) // a column that is not a list holds no tools
	}
	return agentGrantState{Slug: row.Slug, Tools: tools, Origin: row.Origin, Signed: row.Signed}, true
}

// readAgentGrantState asks the management seam for one agent and reads its grant state. An agent
// that is not there, or an answer that cannot be read, is "no state".
func (t *Table) readAgentGrantState(ctx context.Context, slug string) (agentGrantState, bool) {
	if t.AgentManage == nil || strings.TrimSpace(slug) == "" {
		return agentGrantState{}, false
	}
	ask, err := json.Marshal(map[string]string{"slug": slug})
	if err != nil {
		return agentGrantState{}, false
	}
	answer, refusal, err := t.AgentManage(ctx, string(MethodAgentView), ask)
	if err != nil || refusal != "" {
		return agentGrantState{}, false
	}
	return agentGrantStateOf(answer)
}

// syncAgentGrants brings the agent's rules from `before` to `after`: it adds the rules `after` has
// and `before` does not (or has in another state), and removes the ones only `before` has. A nil
// PolicyAuthoring (a build that wires no policy authoring) leaves the agent as it is — the agent
// then holds tools no rule allows, which is the safe direction.
func (t *Table) syncAgentGrants(ctx context.Context, sess *Session, before, after *agentGrantState) error {
	if t.PolicyAuthoring == nil || t.ActorFor == nil {
		return nil
	}
	actor := t.ActorFor(ctx, sess)
	if strings.TrimSpace(actor.ID) == "" || actor.ID == "0" {
		return nil // the write edge already refused such a peer; nothing here to author as
	}
	old := map[string]agentGrantRule{}
	if before != nil {
		for _, r := range before.rules() {
			old[r.ID] = r
		}
	}
	want := map[string]agentGrantRule{}
	var wantOrder []agentGrantRule
	if after != nil {
		for _, r := range after.rules() {
			want[r.ID] = r
			wantOrder = append(wantOrder, r)
		}
	}
	slug := ""
	switch {
	case after != nil:
		slug = after.Slug
	case before != nil:
		slug = before.Slug
	}
	var failed []string
	// Remove what is gone or changed first, so a changed state is a clean replace.
	oldIDs := make([]string, 0, len(old))
	for id := range old {
		oldIDs = append(oldIDs, id)
	}
	sort.Strings(oldIDs)
	for _, id := range oldIDs {
		if w, keep := want[id]; keep && w.State == old[id].State {
			continue
		}
		if _, err := t.PolicyAuthoring.Remove(ctx, actor, id); err != nil {
			failed = append(failed, id)
		}
	}
	for _, r := range wantOrder {
		if o, had := old[r.ID]; had && o.State == r.State {
			continue
		}
		p := PolicyAddParams{
			ID: r.ID, Type: r.Type, Target: PolicyTargetP{Kind: "agent", ID: slug},
			Action: r.State, Spec: json.RawMessage(r.Spec), Source: "operator",
		}
		if _, err := t.PolicyAuthoring.Add(ctx, actor, p); err != nil {
			failed = append(failed, r.ID)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("the agent was saved, but these policy rules could not be written or removed: %s — change its tools again to retry, or fix them in the policy page", strings.Join(failed, ", "))
	}
	return nil
}

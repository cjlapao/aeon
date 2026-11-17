package policyseed

// firstparty.go — the permissions.firstParty set: what the shipped first-party agents may do, written at daemon
// start so the software is useful with no setup (operator ruling, 2026-11-15).
//
// Every first-party agent holds every tool EXCEPT the dangerous ones (tools.DangerousTools: a shell, a file
// write, the network, another agent, and every MCP tool), which ASK a person first. Concretely, per agent:
//
//   - allow `*.*.read`  — every read tool
//   - allow `*.*.write` — the write-access tools that stay inside the product's records (tasks, learnings,
//     goals, the roadmap, bugs…)
//   - ask   <each dangerous tool> and `mcp.*.*.*` — ask outranks allow (deny > ask > allow), so these carve
//     the dangerous tools back out of the `*.*.write` allow and name the rule that is asking.
//
// A shell COMMAND is a different question (command_access) and is not seeded here: the shipped command
// corpus decides the commands it names, and any other command is a question under permissions.unmatched=ask.
//
// Class DEFAULT: applied automatically at start, written as system rows through the one write boundary. An
// operator keeps authority: removing a row is recorded in the ledger and not re-applied, a hand-edited row is
// diverged and left alone, and any rule the operator writes (a deny above all) outranks these.
//
// Only the SUBJECTS are named here — the policy name each handler is judged under (bindPolicySubject) — so a
// user-made or imported agent, which has no rows, falls to the unmatched default (ask) and never inherits
// these allows.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"

	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/tools"
)

// FirstPartySetID is the set's identity.
const FirstPartySetID = "permissions.firstParty"

// firstPartyVersion is every record's version, and so the set's. A change to WHAT a first-party agent may do
// (a tool added to or dropped from the dangerous set, a pattern) bumps it so the ledger notices.
const firstPartyVersion = 1

// FirstPartyDeclaration is the set the daemon mounts: class default (applies at start), version above.
func FirstPartyDeclaration() seed.Set {
	return seed.Set{ID: FirstPartySetID, Class: seed.ClassDefault, Version: firstPartyVersion, Records: FirstPartySource}
}

// FirstPartySubjects are the policy names the shipped handlers are judged under: the handler's own policy
// agent when it declares one, else its name. Sorted, deduplicated.
func FirstPartySubjects() []string {
	seen := map[string]bool{}
	for _, name := range handler.EmbeddedBodies() {
		d, rej := handler.EmbeddedDefinition(name)
		if rej != nil {
			panic("policyseed: a shipped handler will not load — " + name + ": " + rej.Reason)
		}
		subject := name
		if d.PolicyAgent != "" {
			subject = d.PolicyAgent
		}
		seen[subject] = true
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// firstPartyRules are the (tool, action) pairs seeded for each subject, in a fixed order.
func firstPartyRules() []struct{ Tool, Action string } {
	rules := []struct{ Tool, Action string }{{"*.*.read", "allow"}, {"*.*.write", "allow"}}
	for _, t := range tools.DangerousTools() {
		rules = append(rules, struct{ Tool, Action string }{t, "ask"})
	}
	return append(rules, struct{ Tool, Action string }{tools.MCPDangerousPattern, "ask"})
}

// FirstPartyRuleID is the stable id of one seeded rule.
func FirstPartyRuleID(subject, tool string) string {
	sum := sha256.Sum256([]byte(tool))
	return "firstparty." + subject + "." + hex.EncodeToString(sum[:])[:10]
}

// FirstPartyPolicy builds one seeded rule.
func FirstPartyPolicy(subject, tool, action string) (policy.Policy, error) {
	spec, err := json.Marshal(map[string]string{"toolName": tool, "state": action})
	if err != nil {
		return policy.Policy{}, err
	}
	return policy.Policy{
		ID:     FirstPartyRuleID(subject, tool),
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: subject},
		Action: policy.Action(action),
		Spec:   spec,
		Source: policy.Operator, // the operator's ruling shipped as a default; stored as an origin-system row
	}, nil
}

var firstPartyRecords = sync.OnceValue(buildFirstParty)

// FirstPartySource is the set's declared records; callers get private copies.
func FirstPartySource() []seed.Record {
	base := firstPartyRecords()
	out := make([]seed.Record, len(base))
	for i, r := range base {
		out[i] = seed.Record{Key: r.Key, Version: r.Version, Content: append([]byte(nil), r.Content...)}
	}
	return out
}

func buildFirstParty() []seed.Record {
	var out []seed.Record
	for _, subject := range FirstPartySubjects() {
		for _, r := range firstPartyRules() {
			p, err := FirstPartyPolicy(subject, r.Tool, r.Action)
			if err != nil {
				panic("policyseed: a first-party rule cannot be built — " + subject + "/" + r.Tool + ": " + err.Error())
			}
			raw, err := seed.Canonical(contentFromPolicy(p))
			if err != nil {
				panic("policyseed: a first-party record will not canonicalise — " + subject + "/" + r.Tool + ": " + err.Error())
			}
			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
				panic("policyseed: a first-party record will not re-parse: " + err.Error())
			}
			b, err := seed.Canonical(v)
			if err != nil {
				panic("policyseed: a first-party record will not canonicalise: " + err.Error())
			}
			if _, rej := policyFromContent(b); rej != nil {
				panic("policyseed: a first-party record will not decode at the boundary — " + subject + "/" + r.Tool + ": " + rej.Reason)
			}
			out = append(out, seed.Record{Key: p.ID, Version: firstPartyVersion, Content: b})
		}
	}
	if len(out) == 0 {
		panic("policyseed: the first-party set is empty")
	}
	return out
}

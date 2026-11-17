// The security check — step two of import → check → enable, and the step
// that decides what an agent IS when it arrives. It is a policy EVALUATION
// (AP3), not a list: the import gate and the use gate are the same call to
// the same evaluator, so they cannot drift — a second list eventually
// disagrees with the real one, and a gate that can disagree is advisory.
package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// CheckRequests asks the S10 evaluator the question the use gate will ask at
// first use — "does the blocklist deny this?" — for every declared request.
// Refuse ONLY when a Deny rule matched (By names a policy): PL5's default
// deny on an empty table is the USE answer "not granted yet", and requests
// are ungranted by design (MC4) — refusing those would make the blocklist
// into a grant list and invert the whole model. An Ask that matched is a
// question for first use, not an import refusal. Stubs (RouteAccess,
// SkillAccess) answer could-not-determine and pass here exactly as they pass
// at use — parity, not a special case.
func CheckRequests(a *Agent, ps []policy.Policy, at time.Time) *Reject {
	type asked struct {
		kind  policy.PolicyType
		field string
		req   policy.Request
	}
	var asks []asked
	for _, tool := range a.Requests.Tools {
		r := policy.Request{Type: policy.ToolAccess, Agent: a.Name, Tool: tool}
		asks = append(asks, asked{policy.ToolAccess, "requests.tools", r})
	}
	for _, skill := range a.Requests.Skills {
		r := policy.Request{Type: policy.SkillAccess, Agent: a.Name, Skill: skill}
		asks = append(asks, asked{policy.SkillAccess, "requests.skills", r})
	}
	for _, route := range a.Requests.Routes {
		r := policy.Request{Type: policy.RouteAccess, Agent: a.Name, Route: route}
		asks = append(asks, asked{policy.RouteAccess, "requests.routes", r})
	}
	for _, q := range asks {
		v := policy.Evaluate(ps, q.req, at)
		if v.By != nil && v.By.Action == policy.Deny {
			// The deciding sentence verbatim (one formatter): this is S10's
			// answer, and the app, the CLI and this refusal all print the
			// same words for the same decision.
			return refuse(q.field, "refused: %s · %s · the agent was not imported (a blocklist that only fires at first use would let the same agent be importable forever; import and use ask the same gate)", a.Name, v.Sentence())
		}
	}
	return nil
}

// ReadCarriedPolicies reads a package's policies/*.json through the policy
// package's ONE decoder — closed vocabulary, spec shape and the PL1
// authorship boundary included.
//
// THE FORGERY THIS CLOSES: a package that sets its own "source" to operator
// does not become an operator. The source is OVERWRITTEN to policy.Import
// BEFORE policy's own check reads it — so an Allow in a package is refused
// by policy's real authorship sentence (which already names the source and
// says a Deny or an Ask would be accepted: the author was usually aiming
// legitimately, and the remedy is to aim at narrowing), and accepted denials
// are recorded as what they actually are. Persistence of the accepted ones
// is the CLI's (T7), through policy.Store.Add — the write boundary — so
// self-restriction reaches the set the use gate decides with.
func ReadCarriedPolicies(pkgPath string) ([]policy.Policy, *Reject) {
	dir := filepath.Join(pkgPath, "policies")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // carrying nothing is the normal state
		}
		return nil, refuse("policies", "refused: reading policies/: %v", err)
	}
	var out []policy.Policy
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue // the admission walk already refused anything else; this is belt-and-braces
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, refuse("policies", "refused: reading policies/%s: %v", e.Name(), err)
		}
		var p policy.Policy
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, refuse("policies", "refused: policies/%s is not a policy: %v", e.Name(), err)
		}
		p.Source = policy.Import // overwrite the CLAIM before policy reads it
		// Re-encode and run the ONE decoder: vocabulary, spec shape, and
		// the authorship boundary on the FORCED source — the only source a
		// package can ever have.
		rebuilt, err := json.Marshal(p)
		if err != nil {
			return nil, refuse("policies", "refused: policies/%s: %v", e.Name(), err)
		}
		decoded, rj := policy.DecodePolicy(rebuilt)
		if rj != nil {
			return nil, refuse("policies", "refused: policies/%s · policy id %q · %s", e.Name(), p.ID, rj.Reason)
		}
		out = append(out, decoded)
	}
	return out, nil
}

package learnings

import (
	"fmt"
)

// validateScope is the one place a learning's agent scope is checked
// (design §3, re-ruled by S52 LM1). SQLite cannot express "a valid agent,"
// so the CHECK closes the scope vocabulary (person/project/agent) and this
// Go door closes the agent's scope_key — and since S52 the key it checks
// is the S43 SLUG, not a name: the agent table keyed on the slug is the
// authority (AgentBySlug answers for the agent), and the pre-S52 rows that
// carry names moved (MigrateLearningAgentScopeKeys, at the open) before
// this check ever reads them.
//
// person and project carry no Go check: a person is cross-project (S4
// made it per-user, the store's own decision) and a project is a root;
// the CHECK is their backstop. Only the agent scope needs the Go door,
// because only its scope_key names something the database cannot see.
//
// The handler.Lookup path S28 accepted (an in-process S13 body is a real
// agent by registry, no row needed) is GONE: S43 gave every first-party
// agent an agent row, so the registry no longer stands in for the table —
// and a scope_key the table cannot see is a scope_key nothing else can
// manage. The refusal sentence changed with it (listed in the T1 Notes as
// a wire-text change): it names the slug it could not resolve.
func (l *Learnings) validateScope(scope Scope, key string) *Refusal {
	switch scope {
	case ScopeAgent:
		if !l.validAgentScope(key) {
			return &Refusal{Reason: fmt.Sprintf(
				"agent scope %q is not a real agent — no agent row answers to that slug (S43's slug is the identity S52 LM1 makes every learning key on), so a learning cannot be scoped to an agent that does not exist (design §3: the CHECK cannot see the agent, so the check lives here, not in the schema)",
				key)}
		}
	}
	return nil
}

// validAgentScope answers the §3 question, in S52's words: is `slug` a
// real agent? Two facts make one, in this order —
//
//   - an agent row keyed on that slug (first-party or imported: AgentBySlug
//     is the whole first-party list and the plugin list in one, since S43);
//   - and, if that agent is ALSO a plugin_agent, not revoked. The revoked
//     check joins plugin_agent by NAME, because plugin rows key on names
//     (S43's import keeps the plugin's own name), not slugs — a revoked
//     plugin agent does not exist (§3: revoked is not merely disabled), so
//     its slug is refused. An agent with no plugin_agent row at all is NOT
//     "missing a plugin record" — it is first-party, or an imported agent
//     that never carried one, and it is valid.
func (l *Learnings) validAgentScope(slug string) bool {
	a, ok, err := l.s.AgentBySlug(slug)
	if err != nil || !ok {
		return false
	}
	// The revoked join rides the NAME, the only key plugin rows carry.
	pa, found, err := l.s.PluginAgentState(a.Name)
	if err != nil || !found {
		// No plugin row: not a plugin agent — valid. An error reading the
		// plugin table is not a "no": it fails closed (the door refuses,
		// naming it through Append's error path one layer up only if the
		// store itself broke; here false routes to the same refusal, which
		// is honest enough — a broken plugin read cannot confirm life).
		return err == nil
	}
	return pa.State != "revoked"
}

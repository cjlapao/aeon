package policyseed

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// AgentRun is one named agent's share of an apply run — the counts the
// seed door's printers (S45 T4/T5) render. Seeded counts the records whose
// rows the run wrote (apply, adopt or update); Unchanged counts the skips
// (already applied, diverged, removed or conflict-skipped), so a run's two
// numbers always add up to the agent's corpus rules and a reader can never
// be shown a seeded count that silently omits what was left alone.
type AgentRun struct {
	Agent     string
	Seeded    int
	Unchanged int
}

// Run is one apply run of the policy.corpus set: the per-agent counts, the
// totals, and whether anything was actually written (Wrote) — the fact the
// per-run audit row keys on. Dry runs report the plan's counts and write
// nothing, policy_change row included. Applied is the engine's own answer —
// the per-record ops the generic seed doors render; a dry run carries the
// engine's empty result, as it carries no writes.
type Run struct {
	Agents    []AgentRun
	Seeded    int
	Unchanged int
	Wrote     bool
	DryRun    bool
	Applied   seed.ApplyResult
}

// ApplyCorpus is the corpus door over the engine's apply (S45 T12, PM15):
// the seed surface's generic seed.apply for this set, and any caller that
// wants the corpus applied WITH its provenance bookkeeping, comes through
// here. What it adds over the bare engine.Apply:
//
//  1. agent naming — agents named by a caller must be IN the corpus (the
//     whole-run refusal desk.ResolveSeedTargets owns, reused not copied),
//     and must exist in the agent table (T12 behaviour 2: the seed refuses
//     to author rules for an agent nobody has; the check is the agent
//     table's own AgentByName/AgentBySlug reads);
//  2. the actor rule — only a user or the cli may run the door
//     (policy_change's CHECK admits those two actor kinds; a system actor
//     asking to write allow-rows is D4 in a costume and is refused by
//     name before anything writes);
//  3. the per-run audit row — ONE policy_change op='seed' row per run that
//     wrote anything, appended in its own short transaction once the
//     engine's apply has committed (the same order the authored event
//     keeps beside every row: enforcement rows land first, the audit
//     follows, a dry run writes neither).
//
// The engine keeps every decision: idempotence (applied records skip), the
// divergence refusals, the removed-that-stay-removed, the downgrade
// refusal. keys, when given, is the record-key filter (the socket's
// seed.apply keys); agents, when given, resolve to their corpus records.
// Both empty applies the whole declared set — the generic door's meaning.
func ApplyCorpus(ctx context.Context, e *seed.Engine, s *store.Store, agents, keys []string, dryRun bool, actor seed.Actor, at time.Time) (*Run, error) {
	if e == nil {
		return nil, fmt.Errorf("policyseed: the corpus door was handed no engine")
	}
	if s == nil {
		return nil, fmt.Errorf("policyseed: the corpus door was handed no store")
	}
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		return nil, fmt.Errorf("policyseed: %s", rej.Reason)
	}

	// (1) Agent naming, both halves, before any write. An agent the corpus
	// does not carry is ResolveSeedTargets' refusal (the same sentence the
	// CLI verb prints — the one resolver, TD3's reason it was extracted);
	// an agent the corpus carries but the agent table does not is refused
	// here, by name: seeding rules for a nonexistent agent is authoring a
	// permission nobody holds.
	agentOf := map[string]string{}
	if len(agents) > 0 {
		targets, err := desk.ResolveSeedTargets(corpus, agents)
		if err != nil {
			return nil, err
		}
		for _, t := range targets {
			if _, found, err := s.AgentByName(t.Agent); err != nil {
				return nil, fmt.Errorf("policyseed: the agent table could not be read for %q: %w", t.Agent, err)
			} else if !found {
				if _, found, err := s.AgentBySlug(t.Agent); err != nil {
					return nil, fmt.Errorf("policyseed: the agent table could not be read for %q: %w", t.Agent, err)
				} else if !found {
					return nil, fmt.Errorf("refused: seed · agent %q · no such agent in the agent table — the corpus carries its rules, but authoring permissions for an agent nobody has is not seeding (T12)", t.Agent)
				}
			}
			for _, r := range t.Rules {
				key := desk.SeedPolicyID(t.Agent, r.Pattern)
				agentOf[key] = t.Agent
			}
		}
	}

	// The key filter: agents resolve to their corpus records; explicit keys
	// ride straight through; both empty is the whole set.
	setKeys := make(map[string]bool)
	for _, r := range Source() {
		setKeys[r.Key] = true
	}
	filter := append([]string(nil), keys...)
	if len(agents) > 0 {
		filter = filter[:0]
		for k := range agentOf {
			filter = append(filter, k)
		}
		sort.Strings(filter)
	}
	for _, k := range filter {
		if !setKeys[k] {
			return nil, fmt.Errorf("policyseed: key %q is not a record of the %s set — the door refuses to pretend an unknown record was seeded", k, SetID)
		}
	}

	// (2) The actor rule, before any write: policy_change admits actor
	// kinds 'user' and 'cli' only, and the seed that authors policy is
	// exactly the audit those kinds exist for. A system actor at this door
	// is the automatic seeding D4 forbids, arriving in costume.
	if !dryRun {
		if actor.Kind != "user" && actor.Kind != "cli" {
			return nil, fmt.Errorf("refused: seed · %s set · actor %q · only a user or the cli may apply the corpus — a system actor seeding allow-rows is D4 in a costume (S45 T12)", SetID, actor.String())
		}
		if actor.ID == "" {
			return nil, fmt.Errorf("refused: seed · %s set · actor %q names no identity; the seed audit row must say who ran the door (S45 T12)", SetID, actor.Kind)
		}
	}

	if dryRun {
		plan, err := e.Plan(ctx, SetID, filter)
		if err != nil {
			return nil, err
		}
		if plan.Refusal.Key != "" {
			return nil, fmt.Errorf("policyseed: the plan for %s was refused: %s", SetID, plan.Refusal.Reason)
		}
		return countRun(planDecisions(plan.Decisions), agentOf, agents, true), nil
	}

	res, err := e.Apply(ctx, SetID, filter, actor, seed.PathExplicit)
	if err != nil {
		return nil, err
	}

	// (3) The per-run audit row, after the engine's commits (the authored
	// event's own order, carried: a stored rule stands whether or not its
	// audit lands, and an audit failure names which half happened instead
	// of claiming a clean run). Only a run that WROTE anything owes a
	// row — PM8's no-op-and-no-row.
	ran := countRun(applyDecisions(res.Records), agentOf, agents, false)
	ran.Applied = res
	if ran.Wrote {
		summary := fmt.Sprintf("applied the %s seed set: %d rule(s) written, %d left unchanged", SetID, ran.Seeded, ran.Unchanged)
		c := store.PolicyChange{
			Op:         "seed",
			Summary:    summary,
			ActorKind:  actor.Kind,
			ActorID:    actor.ID,
			OccurredAt: at.UTC().Format(time.RFC3339Nano),
		}
		if aerr := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
			return s.PolicySeedAuditTx(ctx, tx, c)
		}); aerr != nil {
			return nil, fmt.Errorf("policyseed: the %s rules were written, but the run's seed audit row failed: %w — which half happened is stated here so the audit is never silently missing", SetID, aerr)
		}
	}
	return ran, nil
}

// countRun folds the per-key decisions into the per-agent counts. A key
// with no owner (the socket door's raw keys, or the whole-set run) counts
// into the totals with no agent line — the totals are the audit's, the
// agent lines are the printers'.
func countRun(perKey map[string]seed.Op, agentOf map[string]string, agents []string, dry bool) *Run {
	byAgent := map[string]*AgentRun{}
	for _, a := range agents {
		byAgent[a] = &AgentRun{Agent: a}
	}
	r := &Run{DryRun: dry}
	for key, op := range perKey {
		wrote := op == seed.OpApply || op == seed.OpAdopt || op == seed.OpUpdate
		if wrote {
			r.Seeded++
			r.Wrote = true
		} else {
			r.Unchanged++
		}
		if a, ok := agentOf[key]; ok {
			line, have := byAgent[a]
			if !have {
				line = &AgentRun{Agent: a}
				byAgent[a] = line
			}
			if wrote {
				line.Seeded++
			} else {
				line.Unchanged++
			}
		}
	}
	for _, a := range agents {
		r.Agents = append(r.Agents, *byAgent[a])
	}
	return r
}

func planDecisions(ds []seed.Decision) map[string]seed.Op {
	out := make(map[string]seed.Op, len(ds))
	for _, d := range ds {
		out[d.Key] = d.Op
	}
	return out
}

func applyDecisions(rs []seed.RecordResult) map[string]seed.Op {
	out := make(map[string]seed.Op, len(rs))
	for _, rr := range rs {
		out[rr.Key] = rr.Op
	}
	return out
}

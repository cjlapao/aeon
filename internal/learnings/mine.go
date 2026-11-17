package learnings

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// mine.go — S52 T4 (LM5/LM7): the agent's own view of memory, as ONE code
// path. learning_query, learning_show and the readable-scope half of
// learning_propose_retract all answer through what lives here, and
// forAgent answers through MineQuery too — the agent's browse, the agent's
// twin of the person's query, and "what a prompt would show" are ONE
// merged readable set read through THE SAME Query (LM5's law that the
// doors cannot drift), with tags as the only extra filter and the same
// one-embedding rank over the merged set.
//
// What readable means (LM1, LM5): the person's learnings (everyone acts
// under them), the projects THIS agent has actually run in (the step rows
// say which), and its OWN agent scope. Never another agent's memory — not
// its body, not its count, not a sentence that proves it exists. The
// out-of-scope answer is the SAME sentence the nonexistent answer gets
// (learning_show, learning_propose_retract's Validate): a difference would
// be an oracle for a slug-guesser.

// MaxSelfLogAddsPerAgent is T4's rate law at learning_add: at most this
// many agent-authored `add` audit lines in SelfLogWindow per slug. Named
// because the refusal must quote its own limit (a counted refusal, never
// silence), and the tests read it rather than recopy it. The ledger is the
// counter: LearningAgentAddCountSince counts the audit rows, so a restart
// cannot reset a limit that is not a row.
//
// The number is 32: an order of magnitude above what one day of honest
// work logs (the loop's own feed files its observed learnings through the
// board hook, which is system-kind and NOT counted here — this cap bounds
// an agent's own typing, not the daemon's seeing), and an order of
// magnitude below what a runaway loop produces. A runaway worth stopping
// is stopped by the refusal naming this constant and the way to spend the
// budget wisely (fewer, observed, deduped).
const MaxSelfLogAddsPerAgent = 32

// SelfLogWindow is the window MaxSelfLogAddsPerAgent counts over.
const SelfLogWindow = 24 * time.Hour

// SelfLogAddsInWindow answers how many of the agent's own adds sit inside
// the window — the count the learning_add door checks BEFORE the write.
// The window rides the store's clock (s.Now()), so a test that sets the
// store's clock tests the real arithmetic.
func (l *Learnings) SelfLogAddsInWindow(agentSlug string) (int, *Refusal) {
	now, err := time.Parse(isoLayout, l.s.Now())
	if err != nil {
		return 0, &Refusal{Reason: fmt.Sprintf("learnings: the store's clock answered %q, which is not a time — the rate window cannot be measured against it", l.s.Now())}
	}
	since := now.UTC().Add(-SelfLogWindow).Format(isoLayout)
	n, err := l.s.LearningAgentAddCountSince(agentSlug, since)
	if err != nil {
		return 0, &Refusal{Reason: fmt.Sprintf("learnings: the add ledger could not be counted (%v) — the cap is enforced by counting, so a store that cannot answer stops the write", err)}
	}
	return n, nil
}

// isoLayout is the store's timestamp shape (open.go's isoUTC). Kept here
// as a private copy for the WINDOW arithmetic only — the store remains
// the only writer of timestamps; this parses one it already made.
const isoLayout = "2006-01-02T15:04:05.000Z"

// MineFilter is the agent's read filter: query text (ranks, never widens —
// D6), tag patterns (the same AND-of-patterns the person's query uses),
// and a limit. There is deliberately NO Scope/ScopeKey here: the readable
// set is resolved from the identity the door stamped, never chosen by the
// agent — the filter narrows within what is already readable and can name
// nothing else (T4's schema says the same shape from the other side).
type MineFilter struct {
	Query string
	Tags  []TagPattern
	Limit int
}

// ResolveReadable is the ONE readable-scope resolution: the agent must
// exist by slug (LM1), and the projects it may read are the roots its step
// rows actually carry. Shared by MineQuery, the readable check beside
// learning_show, and the create-time readable check beside
// learning_propose_retract — three doors, one law.
func (l *Learnings) ResolveReadable(agentSlug string) (roots []string, refusal *Refusal) {
	if agentSlug == "" {
		return nil, &Refusal{Reason: "memory answers from the agent's own view, and this caller stamps no agent — identity is read from the step, never typed (LM1, OL3)"}
	}
	agent, ok, err := l.s.AgentBySlug(agentSlug)
	if err != nil {
		return nil, &Refusal{Reason: fmt.Sprintf("learnings: reading agent %q: %v", agentSlug, err)}
	}
	if !ok {
		return nil, &Refusal{Reason: fmt.Sprintf("learnings refuses agent %q — no agent row answers to that slug (S52 LM1's authority: the slug is the identity; a name is not a key here)", agentSlug)}
	}
	roots, err = l.s.AgentProjectRoots(agent.Name)
	if err != nil {
		return nil, &Refusal{Reason: fmt.Sprintf("learnings: reading the projects agent %q has run in: %v", agentSlug, err)}
	}
	return roots, nil
}

// MayRead answers whether this learning is in the agent's readable set:
// the person's word, a project this agent has run in, or its OWN agent
// scope. Another agent's scope never answers true — and every caller that
// refuses on false says the SAME sentence its nonexistent case says, so
// the answer never distinguishes "someone else's" from "nothing's".
func MayRead(lr Learning, agentSlug string, roots []string) bool {
	switch lr.Scope {
	case ScopePerson:
		return true
	case ScopeAgent:
		return lr.ScopeKey == agentSlug
	case ScopeProject:
		for _, r := range roots {
			if r == lr.ScopeKey {
				return true
			}
		}
		return false
	}
	return false
}

// MineQuery is the merged readable set: own scope first, the person's
// word, then the projects this agent has run in (roots sorted) — read
// scope by scope through Query, tag-filtered by the same pattern law, then
// ranked ONCE over the merged set with at most one embedding call, then
// M1's stable partition. ForAgent is this function with no tags; the two
// doors therefore cannot hold two orderings, because there is one
// function. Read-only, and a browse with no query embeds nothing (T0's
// cost law).
func (l *Learnings) MineQuery(ctx context.Context, agentSlug string, f MineFilter) ([]ForAgentHit, *Refusal) {
	roots, r := l.ResolveReadable(agentSlug)
	if r != nil {
		return nil, r
	}
	limit := f.Limit
	if limit <= 0 {
		limit = ForAgentLimitDefault
	}
	if limit > ForAgentLimitMax {
		limit = ForAgentLimitMax
	}

	var merged []Learning
	own, r := l.queryChecked(ctx, QueryFilter{Scope: ScopeAgent, ScopeKey: agentSlug, Tags: f.Tags})
	if r != nil {
		return nil, r
	}
	merged = append(merged, own...)
	person, r := l.queryChecked(ctx, QueryFilter{Scope: ScopePerson, Tags: f.Tags})
	if r != nil {
		return nil, r
	}
	merged = append(merged, person...)
	for _, root := range roots {
		proj, r := l.queryChecked(ctx, QueryFilter{Scope: ScopeProject, ScopeKey: root, Tags: f.Tags})
		if r != nil {
			return nil, r
		}
		merged = append(merged, proj...)
	}

	if f.Query != "" {
		merged = l.rankWithinSet(ctx, f.Query, merged)
	}

	sort.SliceStable(merged, func(i, j int) bool {
		return !isRelayed(merged[i]) && isRelayed(merged[j])
	})

	basis := "no query — scope order (own scope, person, projects this agent has run in); no embedding call was made"
	if f.Query != "" {
		if l.route == nil || l.embedder == nil {
			basis = "query given but no embedding route is configured — scope order stands, similarity not attempted (D5)"
		} else {
			basis = "embedding similarity within the merged set (D6's within-set ranking), then M1's ordering"
		}
	}

	out := make([]ForAgentHit, 0, limit)
	for i, lr := range merged {
		if len(out) >= limit {
			break
		}
		out = append(out, ForAgentHit{
			Learning: lr,
			Why:      ForAgentWhy{Rank: i + 1, M1Note: M1OrderingNote, Basis: basis},
		})
	}
	return out, nil
}

// SelfLogAdd is the agent's OWN filing (T4's learning_add door): scope
// stamped by the caller (the door refuses a key it did not stamp),
// provenance limited to observed/assumed by the door's schema, and the
// LM4 actor rule below the whole thing — an agent can never say told, and
// never file into another agent's scope, in this call or any other.
func (l *Learnings) SelfLogAdd(ctx context.Context, in AppendInput, actor Actor) (Learning, *Refusal) {
	if actor.Kind != "agent" {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("self-log writes with an agent actor — given %s/%q; the person's door is learnings.add (S52 LM4/LM7)", actor.Kind, actor.ID)}
	}
	if in.Provenance != ProvenanceObserved && in.Provenance != ProvenanceAssumed {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("an agent's own log is observed or assumed — %s is not something an agent witnesses or is told by a person at THIS door (told is the human door's word, relayed the board hook's; S52 LM4)", in.Provenance)}
	}
	n, r := l.SelfLogAddsInWindow(actor.ID)
	if r != nil {
		return Learning{}, r
	}
	if n >= MaxSelfLogAddsPerAgent {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("refused: agent %q has filed %d self-logged learnings in the last %v — that is the cap (MaxSelfLogAddsPerAgent = %d); merge what repeats (observed_count rides an observed row) or file nothing until the window moves", actor.ID, n, SelfLogWindow, MaxSelfLogAddsPerAgent)}
	}
	return l.AppendAs(in, actor)
}

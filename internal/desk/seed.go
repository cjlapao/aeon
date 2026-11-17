package desk

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// SeedPolicyID is the deterministic identity that makes idempotency checkable:
// corpus.<agent>.<sha256(pattern)[0:12 hex]>. A second run computes the same
// ids, and an existing row with one of them is LEFT ALONE — the run reports
// `unchanged` and touches nothing. Overwriting silently is the failure this
// rule exists to prevent: an operator who later edits a seeded rule (widened
// a pattern's action, moved it to ask) must never find the corpus's original
// verdict back in place because someone re-ran a command.
//
// The separators are '.', never ':': the boundary's identifier policy
// (SEC-013) admits letters, digits, '-', '_' and '.' and refuses anything
// else, because a policy id reaches an operator's console through every
// verdict. The hash is six bytes rendered as twelve hex characters — 48 bits
// over a corpus of hundreds of rules: collision risk measured ~1e-8, and a
// collision would mean two rules of one agent share a pattern-prefix, which
// the deterministic recompute makes a refusal-by-clash at the boundary rather
// than a silent overwrite.
//
// Exported, and pinned by TestSeedPolicyIDStable against the literals computed
// before this function moved: an id is a row's primary key, so a change here
// orphans every seeded row an earlier run wrote.
func SeedPolicyID(agent, pattern string) string {
	sum := sha256.Sum256([]byte(pattern))
	return fmt.Sprintf("corpus.%s.%x", agent, sum[:6])
}

// seedSpecJSON builds the CommandAccess spec for one corpus rule:
// {"command":<pattern>,"state":<verdict>} — the state is the row's action
// restated byte for byte, which is the ONLY spelling the boundary's
// stateAgreesWithAction guard admits (the same reason --command's sugar
// writes it; the seeder is that sugar without the shell). Sorted-key marshal
// gives the one byte rendering; the boundary validates the bytes, this
// function guarantees nothing about them beyond the construction.
func seedSpecJSON(rule policy.CorpusRule) ([]byte, error) {
	fields := map[string]json.RawMessage{}
	pattern, err := json.Marshal(rule.Pattern)
	if err != nil {
		return nil, fmt.Errorf("command pattern %q is not encodable: %v", rule.Pattern, err)
	}
	state, err := json.Marshal(string(rule.Verdict))
	if err != nil {
		return nil, fmt.Errorf("verdict %q is not encodable: %v", string(rule.Verdict), err)
	}
	fields["command"] = pattern
	fields["state"] = state
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("the spec could not be built: %v", err)
	}
	return out, nil
}

// SeedSpecJSON is the exported face of the one spec builder (S45 T12): the
// seed engine's policy.corpus source builds through it, so the CLI verb and
// the declared set mint the SAME bytes from the same corpus rule — one
// definition below, two doors above, which is exactly what a copy would not
// guarantee.
func SeedSpecJSON(rule policy.CorpusRule) ([]byte, error) { return seedSpecJSON(rule) }

// SeedTarget is one resolved agent's rules, ready to seed.
type SeedTarget struct {
	Agent string
	Rules []policy.CorpusRule
}

// ResolveSeedTargets is D4's step 2 — every requested agent must be IN the
// corpus before any write — extracted so the `seed_corpus` proposal validator
// runs this exact refusal at create time, never a second, divergent copy of it
// (TD3). Pure: it reads nothing but its two arguments.
func ResolveSeedTargets(corpus policy.Corpus, agents []string) ([]SeedTarget, error) {
	targets := make([]SeedTarget, 0, len(agents))
	seen := make(map[string]bool, len(agents))
	for _, name := range agents {
		if seen[name] {
			continue // one agent named twice is one agent; the counts stay honest
		}
		seen[name] = true
		rules, found := corpus.RulesFor(name)
		if !found {
			return nil, fmt.Errorf("`policy seed-corpus` agent %q is not in the corpus — the corpus carries %d "+
				"agents and names nobody else's rules; asking to seed one the corpus does not carry is the "+
				"filter's refusal, not a seeding of zero rules, and nothing was written",
				name, len(corpus.Agents))
		}
		targets = append(targets, SeedTarget{Agent: name, Rules: rules})
	}
	return targets, nil
}

// RunSeedCorpus writes the rules, or says exactly why it did not. The output is
// an audit surface, not a log line: per-agent counts (seeded / unchanged, or
// would-seed under dry-run), boundary refusals verbatim, and a partial seed
// that names the rule it stopped at instead of pretending the agent is fully
// seeded.
//
// Order of operations, each half load-bearing:
//
//  1. parse the embedded corpus — if the committed bytes do not parse, the
//     refusal prints verbatim; there is no partial corpus to seed;
//  2. resolve EVERY requested agent against the corpus before any write: an
//     agent the corpus does not carry is the FILTER's answer (P31) and
//     refuses the whole run — it is never "seeded zero rules", which reads
//     as success;
//  3. read the existing rule ids through the boundary's List (a read, not
//     the repo) — that snapshot is what "unchanged" is measured against;
//  4. per agent, per rule in AUTHORED order: mint the id, skip if it exists,
//     otherwise add through the boundary (closed sets, PL1, validator) then
//     record the authored audit event (the boundary's record door). A rule
//     the boundary refuses stays REFUSED — the corpus is the specification
//     and a rule of it that cannot clear the boundary is a defect in the
//     matcher or the validator that must surface, not be worked around.
//
// The audit row lands AFTER the row: a stored rule stands whether or not its
// event writes (enforcement never waits on the audit — DEF-041's argument),
// and an event-write failure says which half happened instead of claiming a
// clean seed.
//
// This is the interim home of the runner: T12 re-bases it onto the seed engine
// (`policy.corpus`, a guard set) and reduces this to a printer. Until then it
// is the ONE implementation — the CLI verb and the committed effect both call
// it, the verb with a writer and the effect with `io.Discard`.
func RunSeedCorpus(ctx context.Context, w io.Writer, boundary Boundary, agents []string, dryRun bool, at time.Time) error {
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		return fmt.Errorf("%s", rej.Reason)
	}

	targets, err := ResolveSeedTargets(corpus, agents)
	if err != nil {
		return err
	}

	ps, rej := boundary.ListPolicies(ctx, at)
	if rej != nil {
		// The existing set could not be read; there is nothing to seed
		// idempotently against, and a blind write would be the second writer
		// of a rule that may already stand — with an operator's edit on it.
		return fmt.Errorf("%s", rej.Reason)
	}
	existing := make(map[string]bool, len(ps))
	for _, p := range ps {
		existing[p.ID] = true
	}

	seededTotal, unchangedTotal := 0, 0
	for _, t := range targets {
		seeded, unchanged := 0, 0
		for i, rule := range t.Rules {
			id := SeedPolicyID(t.Agent, rule.Pattern)
			if existing[id] {
				unchanged++
				continue
			}
			if dryRun {
				seeded++
				continue
			}
			specJSON, err := seedSpecJSON(rule)
			if err != nil {
				return err
			}
			p := policy.Policy{
				ID:     id,
				Type:   policy.CommandAccess,
				Target: policy.Target{Kind: policy.AgentKind, ID: t.Agent},
				Action: rule.Verdict,
				Spec:   specJSON,
				Source: policy.Operator,
			}
			if rej := boundary.AddPolicy(ctx, p, at); rej != nil {
				fmt.Fprintf(w, "agent %q: seeded %d · unchanged %d · stopped before rule %d of %d\n",
					t.Agent, seeded, unchanged, i+1, len(t.Rules))
				return fmt.Errorf("policy seed-corpus · agent %q rule %d (%q) was REFUSED by the write "+
					"boundary — a corpus rule that does not clear it is a defect to surface, not work "+
					"around, and the agent is NOT fully seeded: %s",
					t.Agent, i+1, rule.Pattern, rej.Reason)
			}
			if rej := boundary.RecordAuthoredPolicy(ctx, "agent:"+t.Agent, id, at); rej != nil {
				fmt.Fprintf(w, "agent %q: seeded %d · unchanged %d · stopped before rule %d of %d\n",
					t.Agent, seeded, unchanged, i+1, len(t.Rules))
				return fmt.Errorf("policy seed-corpus · agent %q rule %d (%q) IS stored as %q but its "+
					"authored audit row was NOT written — the rule stands (enforcement never waits on the "+
					"audit), the audit is missing it, and the run stopped rather than reporting a clean "+
					"seed: %s", t.Agent, i+1, rule.Pattern, id, rej.Reason)
			}
			existing[id] = true
			seeded++
		}
		if dryRun {
			fmt.Fprintln(w, SeedAgentLine(t.Agent, seeded, unchanged, true))
		} else {
			fmt.Fprintln(w, SeedAgentLine(t.Agent, seeded, unchanged, false))
		}
		seededTotal += seeded
		unchangedTotal += unchanged
	}
	fmt.Fprintln(w, SeedRunTotalsLine(seededTotal, len(targets), unchangedTotal, dryRun))
	return nil
}

// SeedAgentLine is the ONE rendering of a seed run's per-agent count line
// (S45 T8, PM4): RunSeedCorpus prints it as it goes, and the CLI's socket
// door prints the identical bytes from `seedCorpus`'s wire counts — two
// doors, one definition, exactly as warn.go keeps the authoring warnings.
// The dry-run wording ("would seed … the store saw zero writes") is the
// D4 honesty line for a run that counted without writing; it is part of the
// rendering, never assembled by a caller.
func SeedAgentLine(agent string, seeded, unchanged int, dryRun bool) string {
	if dryRun {
		return fmt.Sprintf("agent %q: would seed %d · unchanged %d · dry-run wrote nothing", agent, seeded, unchanged)
	}
	return fmt.Sprintf("agent %q: seeded %d · unchanged %d", agent, seeded, unchanged)
}

// SeedRunTotalsLine is the totals half of the same rendering (S45 T8): the
// run's own audit promise ("every seeded row carries one authored audit
// event") or its dry-run counterpart, defined once beside SeedAgentLine.
func SeedRunTotalsLine(seededTotal, agentCount, unchangedTotal int, dryRun bool) string {
	if dryRun {
		return fmt.Sprintf("dry-run %d rule(s) across %d agent(s) · unchanged %d · the store saw zero writes",
			seededTotal, agentCount, unchangedTotal)
	}
	return fmt.Sprintf("seeded %d rule(s) across %d agent(s) · unchanged %d · every seeded row carries one authored audit event",
		seededTotal, agentCount, unchangedTotal)
}

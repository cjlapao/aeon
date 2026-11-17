package policy_test

// ════════════════════════════════════════════════════════════════════════════
// THE CORPUS AS A SPECIFICATION, RUN THROUGH THE WHOLE ENGINE (brief D4).
//
// pattern_test.go holds the matcher against the corpus, pattern by pattern.
// This file holds the ENGINE against it, rule by rule: every rule the corpus
// carries is rendered into a stored row (target, source, spec, expiry and all)
// and judged by policy.Evaluate with the real registered evaluator — not by a
// builder that could quietly agree with the code under test.
//
// "Every rule evaluates" is the claim that makes corpus.json a specification
// rather than a curiosity, so it is a test here rather than a paragraph in a
// design doc. Concretely, for every rule:
//
//   - the command its probe says it MUST match decides the way the verdict
//     says, and the verdict names that row (and its agent, D9/G11);
//   - the command it MUST NOT match produces no such verdict (PL5 answers);
//   - a row whose target names a different agent decides nothing (the target
//     filter), and a row that has expired decides nothing (the expiry filter)
//     — in both directions, so those two arms cannot pass by always dropping;
//   - the row is one the write boundary itself would admit (except the `ask`
//     rules, which T7 admits — see the ask test, which is honest about today).
//
// A rule with no probe entry FAILS (mustProbe), so the sweep cannot fall
// behind the corpus. Nothing in this file asserts a count of rules, agents or
// patterns: the corpus measures live agent files, and a count assertion fails
// the day an agent is added, which is the day it is supposed to be absorbed
// silently (T2's rule).
//
// # The deviation this file is on the record for
//
// docs/aeon/S11-permissions/tasks/T4.md specifies an ORDERED, first-match
// evaluator. Measured over the committed corpus, that design disagrees with
// the shipped deny-wins engine on a large minority of probe commands and the
// disagreements run overwhelmingly in the permissive direction, because the
// great majority of these agent files author `"*": allow` as their FIRST rule.
// So first-match was NOT built: `Evaluate` (evaluate.go) is reused unchanged.
// TestDenyBeatsAllowRegardlessOfSpecificityCommandAccess asserts the shipped
// behaviour with the rejected scenario named in its own name, and
// TestRuleOrderIsPreservedFromCorpus carries an in-test first-match MUTANT so
// "authored order is data the engine carries and never decides with" is
// re-measured on every run instead of remembered. Full record: the Notes block
// at the top of that task file. If the deviation test below ever fails, the
// engine has been changed to order-decide — do not "fix" it back.
//
// # Two files, one claim
//
// The probes every rule here is judged against are the shared table in
// corpus_probe_test.go — PROMOTED out of pattern_test.go rather than copied
// (T3's handoff: a copied table is a second truth that will disagree), so the
// matcher's view and the engine's view cannot drift apart. And
// TestNoCommandPolicyPanics, the totality half of the same claim, lives in
// evaluate_commandaccess_totality_test.go: the two files were past the
// thousand-line limit as one, which is a layout fact, not a change of subject.
// ════════════════════════════════════════════════════════════════════════════

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// ── the corpus-to-policies bridge (test-local: nothing here is shipped) ──────

// corpusOutsider is the agent name no corpus agent may answer to: the row's
// target filter is only proven by asking the same command as somebody the row
// does not govern. Each test that uses it checks the corpus really has no such
// agent, so the name cannot silently become a governed one tomorrow.
const corpusOutsider = "corpus-outsider-agent"

// corpusRowID is the deterministic ID of one corpus rule rendered as a row:
// the agent and its zero-padded authored index. Zero-padding matters — ties in
// mostSpecific break by ID, and unpadded "…/10" < "…/9" would make the verdict
// name a different row than the authored-first one for no reason at all.
//
// The ID carries no pattern text: a Verdict's Sentence() names By.ID, the
// sentence lands in policy_event.detail, and the slice's rule is that logs and
// events carry no policy content (legal constraint 1). Test IDs are not
// exempted from a rule by being test-only; they simply do not reach the audit.
func corpusRowID(agent string, index int) string {
	return fmt.Sprintf("corpus/%s/%03d", agent, index)
}

// corpusRow renders one corpus rule as a stored CommandAccess row: the agent's
// own name as an agent-kind target, operator as the source (the corpus is the
// operator's statement about its agents — and PL1 says only an operator may
// author an allow, so any other source would make the corpus unseedable), and a
// spec whose `state` repeats the rule's verdict, as stateAgreesWithAction
// requires.
func corpusRow(tb testing.TB, agent string, index int, rule policy.CorpusRule) policy.Policy {
	tb.Helper()
	if agent == "" {
		tb.Fatalf("corpus rule %q carries an empty agent name", rule.Pattern)
	}
	target, rej := policy.ParseTarget(policy.AgentKind, agent)
	if rej != nil {
		tb.Fatalf("helper built an illegal target for agent %q: %s", agent, rej.Reason)
	}
	return policy.Policy{
		ID:     corpusRowID(agent, index),
		Type:   policy.CommandAccess,
		Target: target,
		Action: rule.Verdict,
		Spec:   json.RawMessage(commandSpec(tb, rule.Pattern, string(rule.Verdict))),
		Source: policy.Operator,
	}
}

// corpusPolicies renders one agent's corpus rules as stored rows, IN AUTHORED
// ORDER. Order is preserved HERE so TestRuleOrderIsPreservedFromCorpus can
// prove that evaluate.go ignores it — the claim under test is "authored order
// is data the engine carries and never decides with".
func corpusPolicies(tb testing.TB, agent string) []policy.Policy {
	tb.Helper()
	rules, ok := mustCorpus(tb).RulesFor(agent)
	if !ok {
		tb.Fatalf("the committed corpus has no agent %q", agent)
	}
	if len(rules) == 0 {
		tb.Fatalf("agent %q carries no rules; ParseCorpus would have refused it", agent)
	}
	ps := make([]policy.Policy, 0, len(rules))
	for i, r := range rules {
		ps = append(ps, corpusRow(tb, agent, i, r))
	}
	return ps
}

// corpusRuleCase is one corpus rule and the row it renders to. The probe is
// looked up inside each subtest (mustProbe) so the failure names the rule that
// is missing a probe rather than aborting the whole sweep on the first one.
type corpusRuleCase struct {
	agent string
	index int
	rule  policy.CorpusRule
	row   policy.Policy
}

// everyCorpusRule flattens the corpus to one case per rule, in corpus order.
// It fails rather than returning nothing: a sweep over an empty slice is the
// most convincing fake green this file could produce.
func everyCorpusRule(tb testing.TB) []corpusRuleCase {
	tb.Helper()
	c := mustCorpus(tb)
	var out []corpusRuleCase
	for _, a := range c.Agents {
		if _, ok := c.RulesFor(a.Agent); !ok {
			tb.Fatalf("agent %q lists rules but RulesFor reports it absent", a.Agent)
		}
		for i, r := range a.Rules {
			out = append(out, corpusRuleCase{agent: a.Agent, index: i, rule: r, row: corpusRow(tb, a.Agent, i, r)})
		}
	}
	if len(out) == 0 {
		tb.Fatal("the corpus yielded no rules; this sweep would pass vacuously")
	}
	return out
}

// ruleName is the subtest name for one rule: agent, authored position, verdict,
// pattern. All four, because a red run has to say which rule and how.
func (c corpusRuleCase) ruleName() string {
	return fmt.Sprintf("%s/%03d %s %q", c.agent, c.index, c.rule.Verdict, c.rule.Pattern)
}

// probeFor looks the rule's probe up, failing the subtest when the shared table
// has no entry for the pattern.
func (c corpusRuleCase) probeFor(tb testing.TB) matchProbe {
	tb.Helper()
	probe := mustProbe(tb, c.rule.Pattern)
	// The table was written for the matcher; the engine adds one demand. A
	// must-match command with no segment can never match anything (T5: zero
	// segments is inert toward deny), so such a probe would make every "it
	// decides" arm impossible to satisfy — a table bug, said as one.
	if policy.Split(probe.mustMatch).Count == 0 {
		tb.Fatalf("the must-match probe %q for %q has no segment; no row can ever match it",
			probe.mustMatch, c.rule.Pattern)
	}
	return probe
}

// requireOutsider checks the corpus really has no agent named corpusOutsider,
// so the wrong-agent arms below ask as somebody no row governs.
func requireOutsider(tb testing.TB) {
	tb.Helper()
	if _, ok := mustCorpus(tb).RulesFor(corpusOutsider); ok {
		tb.Fatalf("the corpus now carries an agent named %q; the outsider name must change or these "+
			"target-filter arms would test the wrong thing", corpusOutsider)
	}
}

// ── 1. every rule evaluates (brief D4) ───────────────────────────────────────

func TestEveryCorpusRuleEvaluates(t *testing.T) {
	// The headline claim, executed: every rule in the corpus, through
	// Evaluate, with a command it MUST match and one it MUST NOT. Every rule
	// is covered — allow and deny by their probes deciding as the verdict
	// says, ask by asking as it says (its own test below pins the asked
	// shape's specifics; S11's D1 put it in the closed set, so it decides
	// like the other two and never, ever permits).
	for _, cs := range everyCorpusRule(t) {
		t.Run(cs.ruleName(), func(t *testing.T) {
			probe := cs.probeFor(t)
			switch cs.rule.Verdict {
			case policy.Allow, policy.Deny, policy.Ask:
				// The row must be one the write boundary itself admits: a
				// corpus the `aeon policy add` path refuses is a corpus that
				// cannot be seeded (T9), and a guard that vetoes the
				// specification it is written against is a broken guard.
				// This now covers ask too — migration 0019 made it a closed-set
				// action, so a corpus ask row passes the same boundary as every
				// other row and the sweep below decides it.
				if rej := validateWith(t, policy.CommandAccess, cs.row); rej != nil {
					t.Fatalf("the write boundary refuses a corpus rule (%s %q): %s",
						cs.rule.Verdict, cs.rule.Pattern, rej.Reason)
				}

				t.Run("the must-match command decides as the verdict says", func(t *testing.T) {
					v := policy.Evaluate([]policy.Policy{cs.row}, commandReq(cs.agent, probe.mustMatch), evalAt)
					assertCorpusVerdict(t, cs.rule.Verdict, v, cs.row.ID)
					// The verdict carries the row and who it is about
					// (D9/G11): "denied" is unactionable, "denied by
					// <pattern> against agent:<name>" is not.
					if v.By == nil {
						t.Fatalf("no deciding rule named (%+v); the verdict must carry the row that fired", shapeOf(v))
					}
					if v.By.Target.Kind != policy.AgentKind || v.By.Target.ID != cs.agent {
						t.Errorf("the deciding rule targets %s; the corpus rule belongs to agent %q",
							v.By.Target.String(), cs.agent)
					}
					if v.By.Source != policy.Operator {
						t.Errorf("the deciding rule's source is %q; the corpus is the operator's statement (PL1)",
							v.By.Source)
					}
					if want := "agent:" + cs.agent; v.Subject != want {
						t.Errorf("Subject = %q, want %q", v.Subject, want)
					}
					if v.Type != policy.CommandAccess {
						t.Errorf("Type = %q, want %q", v.Type, policy.CommandAccess)
					}
				})

				t.Run("the must-not-match command produces no such verdict", func(t *testing.T) {
					if probe.noNegative != "" {
						// Only the bare "*" can say this, and it is held to the
						// universality arm instead — never silently skipped.
						if cs.rule.Pattern != "*" {
							t.Errorf("%q claims no negative probe exists (%s); name a command it must not match",
								cs.rule.Pattern, probe.noNegative)
						}
						for _, cmd := range universalProbes {
							v := policy.Evaluate([]policy.Policy{cs.row}, commandReq(cs.agent, cmd), evalAt)
							if policy.Split(cmd).Count == 0 {
								// Nothing was asked for: inert, and PL5 denies.
								assertVerdict(t, v, false, true, false, "", "")
								continue
							}
							assertCorpusVerdict(t, cs.rule.Verdict, v, cs.row.ID)
						}
						return
					}
					v := policy.Evaluate([]policy.Policy{cs.row}, commandReq(cs.agent, probe.mustNotMatch), evalAt)
					assertVerdict(t, v, false, true, false, "", "")
				})

				t.Run("a different agent is not governed by this rule", func(t *testing.T) {
					// The target filter, in the direction that matters: the
					// command is the one the row DOES match, and only the
					// asking agent changed.
					requireOutsider(t)
					v := policy.Evaluate([]policy.Policy{cs.row}, commandReq(corpusOutsider, probe.mustMatch), evalAt)
					assertVerdict(t, v, false, true, false, "", "")
				})

				t.Run("an expired rule decides nothing", func(t *testing.T) {
					// D6/D7: gone, not denied — and not fallen through to an
					// allow either. Both rows of the boundary, so this arm
					// cannot pass by always dropping the row.
					dead := expiring(cs.row, evalAt.Add(-time.Nanosecond))
					v := policy.Evaluate([]policy.Policy{dead}, commandReq(cs.agent, probe.mustMatch), evalAt)
					assertVerdict(t, v, false, true, false, "", "")

					alive := expiring(cs.row, evalAt.Add(time.Nanosecond))
					v = policy.Evaluate([]policy.Policy{alive}, commandReq(cs.agent, probe.mustMatch), evalAt)
					assertCorpusVerdict(t, cs.rule.Verdict, v, alive.ID)
				})

			default:
				// A verdict outside the three. ParseCorpus refuses such a
				// value at the reader, so arriving here means the corpus and
				// this sweep disagree about the vocabulary — and the only
				// safe reading of a fourth value is said, not assumed: inert,
				// never a permission.
				t.Errorf("the corpus carries verdict %q for %q, which is outside allow|deny|ask — "+
					"the reader would have refused it, so a fourth verdict means the corpus and this "+
					"sweep disagree about the vocabulary", cs.rule.Verdict, cs.rule.Pattern)
				for _, cmd := range []string{probe.mustMatch, probe.mustNotMatch} {
					if cmd == "" {
						continue
					}
					v := policy.Evaluate([]policy.Policy{cs.row}, commandReq(cs.agent, cmd), evalAt)
					if v.Allowed {
						t.Errorf("a row with verdict %q PERMITTED %q (%+v); a value the closed set does not "+
							"have may never be a permission", cs.rule.Verdict, cmd, shapeOf(v))
					}
					assertVerdict(t, v, false, true, false, "", "")
				}
			}
		})
	}
}

// assertCorpusVerdict is the sweep's three-way comparison: which assertion a
// corpus row's verdict earns, now that the closed set has three members.
// S11's D1 flipped the ask row from "inert, tested around" to "decides,
// tested straight": an asked verdict names its rule, is never Allowed, and
// never claims the type cannot decide. Everything else about the verdict —
// subject, type, the deciding row's target and source — is asserted by the
// caller's own block, identically for all three verdicts.
func assertCorpusVerdict(tb testing.TB, verdict policy.Action, v policy.Verdict, byID string) {
	tb.Helper()
	switch verdict {
	case policy.Allow:
		assertVerdict(tb, v, true, false, false, byID, "")
	case policy.Ask:
		assertAskVerdict(tb, v, byID, "")
	case policy.Deny:
		assertVerdict(tb, v, false, false, false, byID, "")
	}
}

// ── 2. the corpus's ask rules decide — the flip T6 left named ────────────────

func TestCorpusAskRulesProduceAskVerdicts(t *testing.T) {
	// T6's deferred flip, executed in the open (its scratch note demanded T7
	// name the target here, not hide it): the seven corpus ask rules now
	// yield exactly the assertion the task spec asked for — v.Asked,
	// !v.Allowed, v.By != nil — across every real ask rule, not a fixture.
	// Migration 0019 admitted the value to the CHECK, types.go to the closed
	// set, and the fold to deny > ask > allow, so the inertness assertions
	// this test replaced are now WRONG assertions: an ask row decides, routes
	// to a human, and still never permits. The deny-beats-ask and
	// ask-beats-allow halves pin the ranking over the corpus's own rows.
	seen := 0
	for _, cs := range everyCorpusRule(t) {
		if cs.rule.Verdict != policy.Ask {
			continue
		}
		seen++
		t.Run(cs.ruleName(), func(t *testing.T) {
			probe := cs.probeFor(t)

			// The reader maps the wire value onto the CLOSED-SET constant —
			// not a private forward reference, not a string that happens to
			// spell ask.
			if cs.row.Action != policy.Ask {
				t.Fatalf("row action %q does not carry the corpus verdict as the closed-set constant", cs.row.Action)
			}

			// The write boundary ADMITS the row now: state "ask" agrees with
			// action ask, and an ask from the operator source is the same
			// deny-like authoring the boundary accepts from anyone. A refusal
			// here is the boundary losing the vocabulary migration 0019 gave
			// it.
			if rej := validateWith(t, policy.CommandAccess, cs.row); rej != nil {
				t.Fatalf("the write boundary refuses a corpus ask rule (%q): %s", cs.rule.Pattern, rej.Reason)
			}

			// Reachable: the pattern is one the matcher matches, and the very
			// same row with a deny action decides — only the action was ever
			// the difference; nothing about the pattern needed T7.
			if !policy.MatchPattern(cs.rule.Pattern, probe.mustMatch) {
				t.Fatalf("the ask rule %q does not match its own must-match probe %q; the rule is unreachable",
					cs.rule.Pattern, probe.mustMatch)
			}
			closed := corpusRow(t, cs.agent, cs.index, policy.CorpusRule{Pattern: cs.rule.Pattern, Verdict: policy.Deny})
			v := policy.Evaluate([]policy.Policy{closed}, commandReq(cs.agent, probe.mustMatch), evalAt)
			assertVerdict(t, v, false, false, false, closed.ID, "")

			// The spec's own claim, on the rule's own probe: asked, named,
			// never permitted.
			v = policy.Evaluate([]policy.Policy{cs.row}, commandReq(cs.agent, probe.mustMatch), evalAt)
			assertAskVerdict(t, v, cs.row.ID, "")

			// The must-not-match probe asks nothing: PL5 answers, and the
			// rule is credited with no decision — an ask that fires on
			// commands it does not name is a question nobody authored.
			if cmd := probe.mustNotMatch; cmd != "" {
				v := policy.Evaluate([]policy.Policy{cs.row}, commandReq(cs.agent, cmd), evalAt)
				if v.Allowed || v.Asked || v.By != nil {
					t.Errorf("an ask row for %q decided %q as %+v; it matches nothing there",
						cs.rule.Pattern, cmd, shapeOf(v))
				}
				assertVerdict(t, v, false, true, false, "", "")
			}

			// The ranking, over the corpus's real rows: a deny still outranks
			// the ask (the human is not consulted to be told no), and the ask
			// outranks an allow (the narrow grant may not close the door the
			// rule opened onto a person — and the verdict SHOWS the allow it
			// outranked).
			ban := corpusRow(t, cs.agent, cs.index, policy.CorpusRule{Pattern: cs.rule.Pattern, Verdict: policy.Deny})
			v = policy.Evaluate([]policy.Policy{cs.row, ban}, commandReq(cs.agent, probe.mustMatch), evalAt)
			assertVerdict(t, v, false, false, false, ban.ID, "")

			grant := corpusRow(t, cs.agent, 900, policy.CorpusRule{Pattern: "*", Verdict: policy.Allow})
			v = policy.Evaluate([]policy.Policy{cs.row, grant}, commandReq(cs.agent, probe.mustMatch), evalAt)
			assertAskVerdict(t, v, cs.row.ID, grant.ID)
		})
	}
	if seen == 0 {
		t.Error("the corpus carries no ask rule, so T7 added a third action no data uses — " +
			"the seven (six github-steward, one git-loop) are the reason D1 exists")
	}
}

// ── 3. authored order is data, never a decision ──────────────────────────────

// mirrorExpired mirrors evaluate.go's expired, and mirrorTargetMatches mirrors
// targetMatches. They are duplicated here because the engine's own filters are
// unexported and this file is the external test package; the duplication is
// pinned rather than trusted — TestEveryCorpusRuleEvaluates' "different agent"
// and "expired rule" arms hold BOTH mirrors against the engine's own answers in
// each direction, so a drift shows up as a red test instead of a mutant that
// quietly agrees with itself.
func mirrorExpired(p policy.Policy, at time.Time) bool {
	return p.NotAfter != nil && !at.Before(*p.NotAfter)
}

func mirrorTargetMatches(t policy.Target, r policy.Request) bool {
	switch t.Kind {
	case policy.AllAgents:
		return true
	case policy.AgentKind:
		return t.ID != "" && t.ID == r.Agent
	}
	return false
}

// firstMatchDecision is the REJECTED pre-S10 design (docs/aeon/S11-permissions/
// tasks/T4.md: "Rules is ORDERED. First match wins"), kept here as an in-test
// mutant and shipped by nobody. Its only difference from Evaluate is what
// happens after a row matches: it stops and lets that row decide, in the order
// the caller's slice arrived.
//
// It exists so that two claims are measured on every run instead of remembered:
// (a) the engine is insensitive to that order (the mutant is sensitive to it —
// see TestRuleOrderIsPreservedFromCorpus), and (b) the order-deciding design is
// the more permissive one (T3's precedent: a table whose mutant disagrees on no
// case proves nothing).
//
// It consults the SAME registered evaluator, the same expiry rule and the same
// target rule, so the disagreement is a statement about order and nothing else.
// Ask rows never match on either side today (the evaluator has no arm for them),
// so they are inert here exactly as they are in the engine.
func firstMatchDecision(ps []policy.Policy, req policy.Request, at time.Time) policy.Verdict {
	v := policy.Verdict{Subject: req.Subject(), Type: req.Type}
	ev, ok := policy.EvaluatorFor(req.Type)
	if !ok {
		v.Unimplemented = true
		return v
	}
	for i := range ps {
		p := &ps[i]
		if p.Type != req.Type || mirrorExpired(*p, at) || !mirrorTargetMatches(p.Target, req) {
			continue
		}
		if !ev.Match(*p, req).Matched {
			continue
		}
		switch p.Action {
		case policy.Allow:
			v.Allowed = true
			v.By = p
		case policy.Deny:
			v.By = p
		}
		return v // FIRST match wins — that is the whole of the rejected design
	}
	v.DefaultDeny = true
	return v
}

func TestRuleOrderIsPreservedFromCorpus(t *testing.T) {
	// Two claims, said apart (discovery risk 3) so nobody later "fixes" one by
	// breaking the other:
	//
	//  1. the slice Evaluate is handed keeps the corpus's AUTHORED order — the
	//     fidelity property, re-checked here on the rows themselves (the raw
	//     bytes against the loader is corpus_test.go's TestRuleOrderIsPreserved);
	//  2. reversing that slice changes no verdict — the decision rule is PL4,
	//     and order is data the engine carries and never decides with.
	//
	// And the non-vacuity half: for the same rows, the first-match MUTANT is
	// moved by the reversal and disagrees with the engine mostly in the
	// permissive direction. If the engine ever became order-decide, claim 2 dies
	// first.
	requireOutsider(t)
	c := mustCorpus(t)
	var (
		probes             int
		permissive         int
		restrictive        int
		mutantMovesByOrder int
	)
	for _, a := range c.Agents {
		ps := corpusPolicies(t, a.Agent)

		t.Run(a.Agent, func(t *testing.T) {
			// Claim 1: authored order, position for position.
			var want, got []string
			for _, r := range a.Rules {
				want = append(want, r.Pattern)
			}
			for _, p := range ps {
				got = append(got, specCommandOf(t, p))
			}
			if !slices.Equal(want, got) {
				t.Fatalf("the rows handed to Evaluate are %v, the corpus authors %v — order is the operator's statement",
					got, want)
			}
			if slices.IsSorted(got) {
				t.Errorf("%q's rules arrived sorted; if that ever became true, claim 1 below would prove nothing", a.Agent)
			}
			if ps[0].ID != corpusRowID(a.Agent, 0) {
				t.Errorf("the first row is %q; the bridge lost the authored head of the slice", ps[0].ID)
			}

			reversed := make([]policy.Policy, len(ps))
			for i, p := range ps {
				reversed[len(ps)-1-i] = p
			}
			if reversed[0].ID != ps[len(ps)-1].ID {
				t.Fatal("the reversal is not a reversal; the assertion below would prove nothing")
			}

			for i, r := range a.Rules {
				probe := mustProbe(t, r.Pattern)
				req := commandReq(a.Agent, probe.mustMatch)
				probes++

				// Claim 2: order decides nothing.
				forward, backward := policy.Evaluate(ps, req, evalAt), policy.Evaluate(reversed, req, evalAt)
				if shapeOf(forward) != shapeOf(backward) || forward.Reason != backward.Reason {
					t.Errorf("rule %q/%03d verdict moved when the slice was reversed: %+v then %+v",
						a.Agent, i, shapeOf(forward), shapeOf(backward))
				}

				// The mutant, on the same two slices.
				mFwd, mBack := firstMatchDecision(ps, req, evalAt), firstMatchDecision(reversed, req, evalAt)
				if shapeOf(mFwd) != shapeOf(mBack) {
					mutantMovesByOrder++ // the rejected design IS order-sensitive
				}
				engine := shapeOf(forward)
				mutant := shapeOf(mFwd)
				if engine != mutant {
					if mutant.Allowed && !engine.Allowed {
						permissive++
					} else if engine.Allowed && !mutant.Allowed {
						restrictive++
						t.Errorf("the first-match design REFUSES a command the shipped engine permits (%q/%03d, "+
							"%q): %+v vs %+v. deny-wins permits are a subset of first-match permits — a shipped "+
							"engine allow that first-match loses means one of the two has changed shape",
							a.Agent, i, r.Pattern, engine, mutant)
					}
				}
			}
		})
	}
	// The measurements, printed every run with -v and recorded in the task
	// Notes: the deviation is not a claim copied from discovery.md, it is what
	// this checkout's corpus produces.
	t.Logf("corpus probes %d: first-match disagrees with deny-wins on %d (permissive %d, restrictive %d); "+
		"the mutant's answer moves with slice order on %d", probes, permissive+restrictive, permissive, restrictive, mutantMovesByOrder)
	if permissive+restrictive == 0 {
		t.Error("the first-match mutant agrees with deny-wins on every probe of every agent; either the corpus " +
			"has no `" + "*\": allow`-first agents any more or the mutant is not testing order — this file's " +
			"reason to exist would be gone")
	}
	if permissive == 0 {
		t.Error("no disagreement runs in the permissive direction; the measured basis for rejecting first-match " +
			"is not reproducible on this corpus")
	}
	if mutantMovesByOrder == 0 {
		t.Error("reversing the slice moves the first-match mutant nowhere; the reversal proves nothing about order")
	}
}

// specCommandOf reads the command out of a row's spec through the exported
// struct, so the order assertion above compares decoded values and cannot be
// satisfied by a spec that does not parse.
func specCommandOf(tb testing.TB, p policy.Policy) string {
	tb.Helper()
	var s policy.CommandAccessSpec
	if err := json.Unmarshal(p.Spec, &s); err != nil {
		tb.Fatalf("row %q carries an unreadable spec: %v", p.ID, err)
	}
	return s.Command
}

// ── 4. THE deviation test ────────────────────────────────────────────────────

func TestDenyBeatsAllowRegardlessOfSpecificityCommandAccess(t *testing.T) {
	// THE deviation test, named so it cannot be mistaken for a first-match
	// test: an agent whose FIRST rule is {"*":"allow"} — which the great
	// majority of this repository's own agent files do — and whose LATER rule
	// denies a narrower command, must be DENIED for that command.
	//
	// A first-match evaluator would ALLOW here, because it stops at the wide
	// grant and never reads the denial after it. That is not hypothetical: the
	// in-test mutant below answers allow for a large minority of the corpus's
	// probes, nearly all of them in this direction, which is why the seed
	// design's ordering was measured and rejected. The shipped deny-wins
	// engine (PL4) is reused unchanged. If this test ever fails, the engine has
	// been changed to order-decide; do not "fix" it back.
	//
	// The rows are the corpus's own: golang-engineer-loop is the nine-rule
	// block the slice's design names — "*" allow authored first, then eight
	// denials, `git push*` among them.
	agent := "golang-engineer-loop"
	ps := corpusPolicies(t, agent)
	if ps[0].Action != policy.Allow || specCommandOf(t, ps[0]) != "*" {
		t.Fatalf("agent %q's first authored rule is no longer the wide allow this test is named for; "+
			"pick the deviation on a real agent that still writes one", agent)
	}
	var banID string
	for _, p := range ps {
		if p.Action == policy.Deny && specCommandOf(t, p) == "git push*" {
			banID = p.ID
		}
	}
	if banID == "" {
		t.Fatalf("agent %q no longer denies \"git push*\"; this test's scenario is that rule", agent)
	}

	req := commandReq(agent, "git push origin main") // the probe "git push*" must match
	if !policy.MatchPattern("git push*", "git push origin main") {
		t.Fatal("the fixture command no longer matches the pattern the corpus denies; the probe table lies")
	}

	for name, set := range map[string][]policy.Policy{
		"authored order (the wide allow first)": ps,
		"reversed order (the denial first)":     reversedCopy(ps),
	} {
		t.Run(name, func(t *testing.T) {
			v := policy.Evaluate(set, req, evalAt)
			assertVerdict(t, v, false, false, false, banID, ps[0].ID)
			// The sentence names BOTH: the rule that refused and the grant it
			// overrode — "I allowed that!" answered rather than surprised
			// (D1's visible half, T4 renders it verbatim).
			s := v.Sentence()
			for _, want := range []string{`"` + banID + `"`, `"` + ps[0].ID + `"`, "agent:" + agent, string(policy.CommandAccess)} {
				if !strings.Contains(s, want) {
					t.Errorf("the sentence %q does not name %q", s, want)
				}
			}
		})
	}

	t.Run("the first-match design this replaces would have permitted it", func(t *testing.T) {
		// Executable proof that the test above discriminates the two designs:
		// same rows, same request, same evaluator, one different rule of
		// decision. If someone restores ordering, THIS subtest is the one that
		// starts agreeing with the engine — and it fails for saying so.
		m := firstMatchDecision(ps, req, evalAt)
		if !m.Allowed {
			t.Errorf("the first-match mutant denies what the engine denies (%+v); the corpus's wide-allow-first "+
				"shape is gone or the mutant stopped consulting order — either way this test has stopped "+
				"pinning the deviation", shapeOf(m))
		}
	})

	t.Run("every wide-allow-first agent loses the same way to its own later denials", func(t *testing.T) {
		// The property, over the whole corpus rather than one agent: for every
		// agent that authors the wide allow FIRST and denies something later,
		// the whole authored set denies that command, and names the wide allow
		// as the grant it overrode. No count — just every agent that has the
		// shape, and a failure if none does.
		//
		// The denial named is NOT asserted to be the rule whose probe is being
		// asked about: two of one agent's own denials can cover the same
		// command ("rm -rf*" and "rm -fr*" both name `rm -fr node_modules`),
		// and mostSpecific then names the smaller ID. What is asserted is the
		// shape of a correct denial — a deny row of this agent, whose pattern
		// really does name the command — plus the override. The exact-ID claim
		// lives where it is unambiguous: the single-row arms of
		// TestEveryCorpusRuleEvaluates.
		c := mustCorpus(t)
		shapes := 0
		for _, a := range c.Agents {
			if len(a.Rules) < 2 || a.Rules[0].Pattern != "*" || a.Rules[0].Verdict != policy.Allow {
				continue
			}
			wide := corpusRow(t, a.Agent, 0, a.Rules[0])
			ps := corpusPolicies(t, a.Agent)
			for _, r := range a.Rules[1:] {
				if r.Verdict != policy.Deny {
					continue
				}
				probe := mustProbe(t, r.Pattern)
				if policy.Split(probe.mustMatch).Count == 0 {
					t.Fatalf("the must-match probe %q for %q has no segment; no row can ever match it",
						probe.mustMatch, r.Pattern)
				}
				shapes++
				v := policy.Evaluate(ps, commandReq(a.Agent, probe.mustMatch), evalAt)
				if v.Allowed || v.DefaultDeny || v.Unimplemented {
					t.Errorf("%q denied by nobody: %q on %q for agent %q — the wide allow authored first did not "+
						"lose to this agent's own later denial", a.Agent, r.Pattern, probe.mustMatch, a.Agent)
					continue
				}
				if v.By == nil || v.By.Action != policy.Deny {
					t.Errorf("%q: the verdict names %v, not a denial", a.Agent, idOf(v.By))
					continue
				}
				if !strings.HasPrefix(v.By.ID, "corpus/"+a.Agent+"/") {
					t.Errorf("%q: the denial named (%q) is not a row of this agent", a.Agent, v.By.ID)
					continue
				}
				if !denyPatternNames(specCommandOf(t, *v.By), probe.mustMatch) {
					t.Errorf("%q: the verdict names %q, whose pattern %q does not name the command %q",
						a.Agent, v.By.ID, specCommandOf(t, *v.By), probe.mustMatch)
				}
				if v.OverriddenAllow == nil || v.OverriddenAllow.ID != wide.ID {
					t.Errorf("%q: the denial did not name the wide allow %q as the grant it overrode (got %v); "+
						"PL4 made visible is what makes this auditable instead of surprising",
						a.Agent, wide.ID, idOf(v.OverriddenAllow))
				}
			}
		}
		if shapes == 0 {
			t.Error("no corpus agent authors a wide allow followed by a denial; the deviation this file pins " +
				"has no real case left, which is the day the design conversation reopens")
		}
	})
}

// denyPatternNames asks the matcher whether one deny row's pattern names the
// command, on ANY segment — the same reading the evaluator's deny arm uses
// (spec_commandaccess.go), asked here from the outside so a verdict that names
// an unrelated rule cannot pass it.
func denyPatternNames(pattern, command string) bool {
	for _, s := range policy.Split(command).Items {
		if policy.MatchPattern(pattern, s) {
			return true
		}
	}
	return false
}

// reversedCopy returns a reversed copy of a slice, leaving the caller's order
// intact (the authored slice has to stay authored for the test after this one).
func reversedCopy(ps []policy.Policy) []policy.Policy {
	out := make([]policy.Policy, len(ps))
	for i, p := range ps {
		out[len(ps)-1-i] = p
	}
	return out
}

// ── 5. the verdict carries its rule and its agent (D9/G11) ───────────────────

func TestEveryCorpusDecisionNamesItsRuleAndAgent(t *testing.T) {
	// Sentence() is the one renderer every surface prints (verdict.go), so the
	// corpus's decisions must be actionable IN THAT SENTENCE: the deciding
	// rule's ID, the agent it governs, the type asked about, who wrote the row,
	// and whether it expires (PL7 — silence would read as a lifetime nobody
	// checked). Asserted over every corpus decision, not eyeballed on one.
	for _, cs := range everyCorpusRule(t) {
		probe := cs.probeFor(t)
		v := policy.Evaluate([]policy.Policy{cs.row}, commandReq(cs.agent, probe.mustMatch), evalAt)
		// All three verdicts decide (since migration 0019 admitted ask), so all
		// three owe the same actionable sentence: the rule, the agent, the
		// type, the source, the expiry. Prefix honesty is checked in both
		// directions for every shape — "asked:" must appear exactly when the
		// verdict is asked, "allowed:" exactly when permitted.
		if v.By == nil {
			t.Errorf("%s decided nothing on its must-match probe; no sentence to check", cs.ruleName())
			continue
		}
		s := v.Sentence()
		for _, want := range []string{
			`"` + cs.row.ID + `"`,        // the rule that decided
			"agent:" + cs.agent,          // whose policy it is (O1)
			string(policy.CommandAccess), // what was asked about
			"source operator",            // who wrote it (PL1's answer)
			"no expiry",                  // PL7: never blank, never a sentinel
			cs.agent,                     // the target rendering, agent:<name>
		} {
			if !strings.Contains(s, want) {
				t.Errorf("%s's sentence %q does not name %q; a decision nobody can act on is not a decision, "+
					"it is a bug report", cs.ruleName(), s, want)
			}
		}
		if v.Allowed != strings.HasPrefix(s, "allowed: ") {
			t.Errorf("%s: Allowed = %v but the sentence %q disagrees", cs.ruleName(), v.Allowed, s)
		}
		if v.Asked != strings.HasPrefix(s, "asked: ") {
			t.Errorf("%s: Asked = %v but the sentence %q disagrees; the third decision must be legible "+
				"from the first word (S11's D1)", cs.ruleName(), v.Asked, s)
		}
	}
}

// ── 6. PL5 for this type: the empty set denies, and it is not a stub ─────────

func TestCommandAccessEmptySetDenies(t *testing.T) {
	// PL5 for CommandAccess specifically, which is the assertion that the stub
	// era is over: no rows means the engine DECIDED to permit nothing
	// (DefaultDeny), and did not answer "nobody can evaluate this yet"
	// (Unimplemented). The distinction is what T11 renders as three states, and
	// the placeholder this type replaced could only ever answer the second one.
	commands := []string{"", "ls", "git push origin main", "rm -rf /", "cd sub && git push", "echo a & b"}
	agents := []string{"main", "", corpusOutsider}
	for _, agent := range agents {
		for _, command := range commands {
			for name, ps := range map[string][]policy.Policy{"nil": nil, "empty slice": {}} {
				v := policy.Evaluate(ps, commandReq(agent, command), evalAt)
				if v.Allowed {
					t.Fatalf("an empty CommandAccess set PERMITTED %q as %q (%+v)", command, agent, shapeOf(v))
				}
				if !v.DefaultDeny {
					t.Errorf("empty set (%s) for command %q is not a default deny: %+v", name, command, shapeOf(v))
				}
				if v.Unimplemented {
					t.Errorf("empty set for command %q refused to decide: %+v — CommandAccess has an evaluator; "+
						"this is the answer the stub gave", command, shapeOf(v))
				}
				if v.By != nil || v.OverriddenAllow != nil {
					t.Errorf("an empty set named deciding rules for %q: %+v", command, shapeOf(v))
				}
				if want := "agent:" + agent; v.Subject != want {
					t.Errorf("Subject = %q, want %q", v.Subject, want)
				}
				if s := v.Sentence(); !strings.HasPrefix(s, "refused: "+v.Subject) {
					t.Errorf("the sentence %q does not open with the refusal of %q", s, v.Subject)
				}
			}
		}
	}

	t.Run("another type's rows do not decide a command question", func(t *testing.T) {
		// Rows exist, none of them are this question's answers: still PL5, and
		// still not could-not-determine. A ToolAccess allow for bash must not
		// read as permission to run any command.
		tool := toolPolicy(t, "bash-for-main", policy.AgentKind, "main", policy.Allow,
			`{"toolName":"bash","state":"allow"}`)
		v := policy.Evaluate([]policy.Policy{tool}, commandReq("main", "git push origin main"), evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	})
}

package main

// verify_amend_propose_test.go — S57 T8, the second half of the gate
// amendment's guards: the two PROPOSE-kind laws, beside the surface guards
// in verify_amend_test.go. Split from that file by cohesion (SIZE1): the
// surface half proves the nine writes are a human door no agent reaches;
// this file proves what an agent CAN do with the two kinds — create an
// inert proposal that only tightens, and nothing more.
//
// What is under test (T8.md, verbatim):
//
//  5. TestProposalKindsAreRestrictiveOnly — a merge_rule proposal that
//     loosens any single field is refused at create with the field named
//     (property style over the Tightens classification table); a
//     verify_command proposal with an op other than "add" is refused.
//
//  6. TestProposeKindsAreInertUntilCommitted — a sitting verify_command /
//     merge_rule proposal changes NO byte of what an agent can read
//     (verify.list, mergerule.get, the merge preview), and the commit
//     stamps the COMMITTER as the actor on the written row.

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/mergerule"
	"github.com/cjlapao/aeon/internal/store"
)

// gateEnv is the commit test's world, the autonomy_commit_test.go idiom:
// a throwaway home, the test's own store handle, one component — and the
// seams' per-call openStoreDirectly() opens the SAME file, WAL serialising
// the two handles.
type gateEnv struct {
	t    *testing.T
	st   *store.Store
	comp string
}

func newGateEnv(t *testing.T) gateEnv {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := openAgentStore(t)
	c, err := st.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha", Root: "/repo/alpha"})
	if err != nil || !c.OK() {
		t.Fatalf("seed component: %v %v", err, c.Reason)
	}
	return gateEnv{t: t, st: st, comp: c.Component.ID}
}

// seedRule plants the node's rule through the store's own door (a person's
// act) so the proposals under test stand against a CURRENT rule, not the
// baseline.
func (e gateEnv) seedRule(r mergerule.Rule) {
	e.t.Helper()
	w, err := e.st.SetMergeRule(store.Actor{Kind: "user", ID: "u-seed"}, r)
	if err != nil {
		e.t.Fatalf("seed rule: %v", err)
	}
	if !w.OK() {
		e.t.Fatalf("seed rule refused: %s", w.Reason)
	}
}

// gateTightRule is the all-tight reference the property flips from: every
// behavioural field at its tightest value, per the store's own Tightens
// table (internal/mergerule/tighten.go) — RequiredChecks superset =
// tighter, AeonReviewAgents subset = tighter, and the counter-intuitive
// one: ChecksPolicy reported_green = TIGHTER (it demands EVERY reported
// check be green; required_only, which lets an unrelated red pass, is the
// loose value — even though the baseline carries reported_green, because
// "today's behaviour" is not the same order as "tightest").
func gateTightRule(comp string) mergerule.Rule {
	return mergerule.Rule{
		ComponentID:            comp,
		Enabled:                true,
		RequireLocal:           true,
		RequireDoD:             mergerule.RequireDoDAllMet,
		RequiredChecks:         []string{"x", "y"},
		ChecksPolicy:           mergerule.ChecksReported,
		NoChecks:               mergerule.NoChecksRefuse,
		ReviewMode:             mergerule.ReviewModeBoth,
		AeonReviewAgents:       nil,
		RequireThreadsResolved: true,
		Method:                 mergerule.MethodSquash,
	}
}

// gateRuleIdentityFields: the fields that never classify (the store's own
// Tightens table says identity).
var gateRuleIdentityFields = map[string]bool{
	"ComponentID": true,
	"Revision":    true,
	"Source":      true,
	"UpdatedAt":   true,
}

// gateRuleLooseners is the property's flip table, one entry per BEHAVIOURAL
// field: applied to the all-tight reference it moves that one field toward
// loose. Method's entry flips to the other merge kind — the NEUTRAL field,
// asserted separately (a flip of it is a tightening neither way).
var gateRuleLooseners = map[string]func(mergerule.Rule) mergerule.Rule{
	"Enabled":                func(r mergerule.Rule) mergerule.Rule { r.Enabled = false; return r },
	"RequireLocal":           func(r mergerule.Rule) mergerule.Rule { r.RequireLocal = false; return r },
	"RequireThreadsResolved": func(r mergerule.Rule) mergerule.Rule { r.RequireThreadsResolved = false; return r },
	"RequireDoD":             func(r mergerule.Rule) mergerule.Rule { r.RequireDoD = mergerule.RequireDoDNone; return r },
	"ChecksPolicy":           func(r mergerule.Rule) mergerule.Rule { r.ChecksPolicy = mergerule.ChecksRequired; return r },
	"NoChecks":               func(r mergerule.Rule) mergerule.Rule { r.NoChecks = mergerule.NoChecksAllow; return r },
	"ReviewMode":             func(r mergerule.Rule) mergerule.Rule { r.ReviewMode = mergerule.ReviewModeEither; return r },
	"Method":                 func(r mergerule.Rule) mergerule.Rule { r.Method = mergerule.MethodMerge; return r },
	"RequiredChecks":         func(r mergerule.Rule) mergerule.Rule { r.RequiredChecks = []string{"x"}; return r },
	"AeonReviewAgents":       func(r mergerule.Rule) mergerule.Rule { r.AeonReviewAgents = []string{"code-reviewer"}; return r },
}

// gateMergeRulePayload builds the merge_rule proposal the way the create
// door receives it: the full proposed rule (a patch shape is refused) plus
// the reason.
func gateMergeRulePayload(rule mergerule.Rule, reason string) string {
	raw, err := json.Marshal(rule)
	if err != nil {
		panic(err)
	}
	b, err := json.Marshal(desk.MergeRuleProposal{Rule: raw, Reason: reason})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func gateVerifyPayload(comp, name, command string) string {
	b, err := json.Marshal(desk.VerifyCommandProposal{
		Op:          "add",
		Name:        name,
		Command:     command,
		Requirement: "required",
		TimeoutS:    600,
		ComponentID: comp,
		Reason:      "the branch has burned us twice",
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestProposalKindsAreRestrictiveOnly(t *testing.T) {
	t.Run("LooseningRefusedAtCreateNamesTheField", func(t *testing.T) {
		e := newGateEnv(t)
		e.seedRule(gateTightRule(e.comp))

		cases := []struct {
			field string
			flip  func(mergerule.Rule) mergerule.Rule
		}{
			{"RequireLocal", gateRuleLooseners["RequireLocal"]},
			{"NoChecks", gateRuleLooseners["NoChecks"]},
			{"RequiredChecks", gateRuleLooseners["RequiredChecks"]},
			{"ChecksPolicy", gateRuleLooseners["ChecksPolicy"]},
			{"RequireDoD", gateRuleLooseners["RequireDoD"]},
			{"Enabled", gateRuleLooseners["Enabled"]},
		}
		for _, c := range cases {
			loose := c.flip(gateTightRule(e.comp))
			err := validateMergeRuleProposal([]byte(gateMergeRulePayload(loose, "tighten it before release")))
			if err == nil {
				t.Errorf("%s loosened: the create door ACCEPTED a loosening — a proposal may only tighten, and a loosening is a person's act (S57 T8)", c.field)
				continue
			}
			if !strings.Contains(err.Error(), c.field) {
				t.Errorf("%s loosened: the refusal does not name the field (got %q) — a refusal that does not say WHAT it refused is not a refusal", c.field, err.Error())
			}
		}

		// the incomparable pair, both directions: the refusal names the
		// pair, not a field (the loosened list is empty by contract).
		for _, dir := range [][2]string{
			{mergerule.ReviewModeForge, mergerule.ReviewModeAeon},
			{mergerule.ReviewModeAeon, mergerule.ReviewModeForge},
		} {
			cur := gateTightRule(e.comp)
			cur.ReviewMode = dir[0]
			e.seedRule(cur)
			prop := gateTightRule(e.comp)
			prop.ReviewMode = dir[1]
			err := validateMergeRuleProposal([]byte(gateMergeRulePayload(prop, "tighten it before release")))
			if err == nil {
				t.Errorf("ReviewMode %s→%s: the create door accepted the incomparable switch", dir[0], dir[1])
			} else if !strings.Contains(err.Error(), "incomparable") {
				t.Errorf("ReviewMode %s→%s: the refusal does not name the incomparable pair (got %q)", dir[0], dir[1], err.Error())
			}
		}

		// and the honest positive: a true TIGHTENING passes the create
		// door (RequiredChecks gains a check — the superset is tighter).
		tight := gateTightRule(e.comp)
		tight.RequiredChecks = []string{"x", "y", "z"}
		if err := validateMergeRuleProposal([]byte(gateMergeRulePayload(tight, "tighten it before release"))); err != nil {
			t.Errorf("a genuine tightening was refused at the create door: %v", err)
		}
	})

	t.Run("OpOtherThanAddRefused", func(t *testing.T) {
		// A real component: the validator is the FULL seam (desk half →
		// decode → store half), and the positive case must clear both.
		e := newGateEnv(t)
		base := desk.VerifyCommandProposal{
			Op: "add", Name: "build-check", Command: "make ci", Requirement: "required",
			TimeoutS: 600, ComponentID: e.comp, Reason: "the branch has burned us twice",
		}
		for _, op := range []string{"update", "archive", "restore", ""} {
			p := base
			p.Op = op
			b, _ := json.Marshal(p)
			err := validateVerifyCommandProposal(b)
			if err == nil {
				t.Errorf("op %q: the create door accepted an op other than \"add\" — removing, archiving or re-pointing a gate is a person's act", op)
			} else if !strings.Contains(err.Error(), "op") {
				t.Errorf("op %q: the refusal does not name the op (got %q)", op, err.Error())
			}
		}
		// and op "add" with the full shape passes the whole create door.
		if err := validateVerifyCommandProposal(mustJSON(t, base)); err != nil {
			t.Errorf("a well-formed op:\"add\" was refused at the create door: %v", err)
		}
	})

	t.Run("EveryLooseFlipIsClassified", func(t *testing.T) {
		// Reflection over the store's own classification table (Tightens):
		// the loosener map must cover EXACTLY Rule's non-identity fields,
		// and every single loose flip must come back a loosening that
		// names the field — or, for the neutral Method, a tightening
		// neither way.
		tight := gateTightRule("c")
		typ := reflect.TypeOf(mergerule.Rule{})
		var behavioural []string
		for i := 0; i < typ.NumField(); i++ {
			name := typ.Field(i).Name
			if gateRuleIdentityFields[name] {
				continue
			}
			behavioural = append(behavioural, name)
			if _, ok := gateRuleLooseners[name]; !ok {
				t.Fatalf("the property table has no loosener for field %s — the table has drifted from the Rule (add it, or the classifier is lying about a field)", name)
			}
		}
		if len(gateRuleLooseners) != len(behavioural) {
			t.Fatalf("the property table covers %d fields and the Rule has %d behavioural — a field was renamed or added (S57 T8 property drift)", len(gateRuleLooseners), len(behavioural))
		}

		for name, flip := range gateRuleLooseners {
			loose := flip(tight)
			ok, loosened := mergerule.Tightens(tight, loose)
			if name == "Method" {
				if !ok {
					t.Errorf("Method %s→%s classified incomparable — Method is the neutral field (the store's own table says so)", tight.Method, loose.Method)
				}
				continue
			}
			if ok {
				t.Errorf("%s flipped toward loose classified a TIGHTENING — the classifier table drifted from the order it pins", name)
			}
			if len(loosened) != 1 || loosened[0] != name {
				t.Errorf("%s: the loosened list is %v, want exactly [%s] — the classifier misnames the field it refused", name, loosened, name)
			}
		}

		// the one incomparable pair, both directions: not ok, and an EMPTY
		// loosened list (the refusal sentence has no field to name).
		for _, pair := range [][2]string{
			{mergerule.ReviewModeForge, mergerule.ReviewModeAeon},
			{mergerule.ReviewModeAeon, mergerule.ReviewModeForge},
		} {
			a, b := tight, tight
			a.ReviewMode = pair[0]
			b.ReviewMode = pair[1]
			ok, loosened := mergerule.Tightens(a, b)
			if ok || len(loosened) != 0 {
				t.Errorf("ReviewMode %s→%s: want the incomparable pair (not ok, empty list), got ok=%v %v", pair[0], pair[1], ok, loosened)
			}
		}
	})
}

// ─── 6: inert until committed ─────────────────────────────────────────────

func TestProposeKindsAreInertUntilCommitted(t *testing.T) {
	e := newGateEnv(t)
	e.seedRule(gateTightRule(e.comp))

	// The snapshot: everything an agent could read about this node's
	// gates — the gate list, the effective rule with its source, the own
	// row's absence, and what the merge gate would say right now (the
	// preview's evaluator half, on fixed facts).
	snap := func() string {
		gates, err1 := e.st.ListVerifyCommands(store.VerifyCommandFilter{ComponentID: e.comp})
		rule, source, err2 := e.st.EffectiveMergeRule(e.comp)
		own, err3 := e.st.GetMergeRule(e.comp)
		out := struct {
			Gates   []store.VerifyCommand `json:"gates"`
			Rule    mergerule.Rule        `json:"rule"`
			Source  string                `json:"source"`
			Own     mergerule.Rule        `json:"own"`
			Errs    [3]string             `json:"errs"`
			Verdict mergerule.Verdict     `json:"verdict"`
		}{gates, rule, source, own, [3]string{gateErrString(err1), gateErrString(err2), gateErrString(err3)},
			mergerule.Evaluate(rule, mergerule.Facts{PR: 7})}
		b, _ := json.Marshal(out)
		return string(b)
	}

	before := snap()

	// The create door: the registry's validator (the SAME function the
	// create door runs — TD3) and then the inert row, agent-authored.
	vp := gateVerifyPayload(e.comp, "build-check", "make ci")
	if err := validateVerifyCommandProposal([]byte(vp)); err != nil {
		t.Fatalf("the create door refused a well-formed add: %v", err)
	}
	vrow, err := e.st.CreateProposal(string(desk.KindVerifyCommand), vp,
		store.Actor{Kind: "agent", ID: "implementer"}, "2026-11-02T12:00:00Z")
	if err != nil {
		t.Fatalf("stage the verify_command row: %v", err)
	}

	// A tightening against the seeded all-tight rule: RequiredChecks
	// gains a check (the superset is the tighter order).
	mp := gateMergeRulePayload(func() mergerule.Rule {
		r := gateTightRule(e.comp)
		r.RequiredChecks = []string{"x", "y", "z"}
		return r
	}(), "tighten it before release")
	if err := validateMergeRuleProposal([]byte(mp)); err != nil {
		t.Fatalf("the create door refused the tightening: %v", err)
	}
	mrow, err := e.st.CreateProposal(string(desk.KindMergeRule), mp,
		store.Actor{Kind: "agent", ID: "implementer"}, "2026-11-02T12:00:01Z")
	if err != nil {
		t.Fatalf("stage the merge_rule row: %v", err)
	}

	// Inert: two proposals sitting in the table change NO byte of what an
	// agent can read.
	if after := snap(); after != before {
		t.Fatalf("a sitting proposal changed what an agent can read — the row must be inert until a person commits:\nbefore: %s\nafter:  %s", before, after)
	}

	// The rows are PROPOSED and agent-authored — not yet decided.
	for _, row := range []store.Proposal{vrow, mrow} {
		if row.Status != "proposed" {
			t.Fatalf("row %d status is %q, want proposed", row.ID, row.Status)
		}
		if row.CreatedByKind != "agent" || row.CreatedByID != "implementer" {
			t.Fatalf("row %d author is %s/%s, want the agent who proposed it", row.ID, row.CreatedByKind, row.CreatedByID)
		}
	}

	// The person's commit: the EFFECT applies, and the COMMITTER — the
	// person at the door, not the agent who filed the row — is the actor
	// on what the store wrote.
	res, err := desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, vrow.ID,
		desk.ProposalPayloadDigest(vp), desk.Actor{Kind: "user", ID: "u-decide"})
	if err != nil {
		t.Fatalf("commit the verify_command: %v", err)
	}
	if !res.EffectApplied {
		t.Fatalf("the gate effect did not apply: %+v", res)
	}

	gates, err := e.st.ListVerifyCommands(store.VerifyCommandFilter{ComponentID: e.comp})
	if err != nil || len(gates) != 1 {
		t.Fatalf("after commit: %d gates (want 1): %v", len(gates), err)
	}
	if gates[0].Name != "build-check" || gates[0].Origin != "user" {
		t.Fatalf("the committed gate is %+v, want name build-check origin user", gates[0])
	}
	evs, err := e.st.ListVerifyCommandEvents(store.VerifyCommandEventFilter{ComponentID: e.comp})
	if err != nil || len(evs) == 0 {
		t.Fatalf("no gate audit event after the commit: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Op != "add" || last.ActorKind != "user" || last.ActorID != "u-decide" {
		t.Fatalf("the gate audit is %+v, want op add as the COMMITTER user/u-decide — never the agent who proposed", last)
	}

	res, err = desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, mrow.ID,
		desk.ProposalPayloadDigest(mp), desk.Actor{Kind: "user", ID: "u-decide"})
	if err != nil {
		t.Fatalf("commit the merge_rule: %v", err)
	}
	if !res.EffectApplied {
		t.Fatalf("the rule effect did not apply: %+v", res)
	}

	rule, source, err := e.st.EffectiveMergeRule(e.comp)
	if err != nil {
		t.Fatalf("effective rule after commit: %v", err)
	}
	if source != "own" {
		t.Fatalf("the committed rule's source is %q, want own", source)
	}
	if len(rule.RequiredChecks) != 3 {
		t.Fatalf("the committed rule's RequiredChecks is %v, want the tightened trio", rule.RequiredChecks)
	}
	revs, err := e.st.ListMergeRuleEvents(store.MergeRuleEventFilter{ComponentID: e.comp})
	if err != nil || len(revs) == 0 {
		t.Fatalf("no rule audit after the commit: %v", err)
	}
	rLast := revs[len(revs)-1]
	if rLast.Op != "set" || rLast.ActorKind != "user" || rLast.ActorID != "u-decide" {
		t.Fatalf("the rule audit is %+v, want op set as the COMMITTER user/u-decide", rLast)
	}

	// The rows are DECIDED by the person — the decision's own pair, kept
	// separate from the row's author.
	for _, id := range []int64{vrow.ID, mrow.ID} {
		row, found, err := e.st.GetProposal(id)
		if err != nil || !found {
			t.Fatalf("row %d gone after commit: %v (found %v)", id, err, found)
		}
		if row.DecidedByKind != "user" || row.DecidedByID != "u-decide" {
			t.Fatalf("row %d decided by %s/%s, want user/u-decide", id, row.DecidedByKind, row.DecidedByID)
		}
		if row.CreatedByKind != "agent" {
			t.Fatalf("row %d lost its agent author on commit (%s) — the proposer and the decider are two people", id, row.CreatedByKind)
		}
	}
}

// ─── small helpers ────────────────────────────────────────────────────────

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func gateErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

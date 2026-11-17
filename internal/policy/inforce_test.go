package policy_test

// T11 — "is this policy in force?" has exactly three answers, and none of them
// may collapse into another when it is rendered (PL12, P31's eighth instance).
//
// The tests are named as sentences because each one IS the claim, and a failing
// run should read as an English complaint. They fall into four groups:
//
//   - the closed set: three states, by enumeration, with the copy discipline
//     the other closed sets obey (TestTheThreeInForceStatesAreExactlyThese);
//   - reachability and separation: all three produced by real rows, and
//     unacknowledged byte-distinct from could-not-determine in BOTH the state
//     and the render;
//   - the wording, which is not cosmetic: the unacknowledged answer reads as
//     ENFORCED (D13's other half), and no two answers render alike — the
//     failure mode is a line that looks fine while the signal is gone;
//   - the cause: could-not-determine comes from the registry's Stub() marker
//     and from nothing else, and the file that answers has no failure channel
//     to check (DB19).
//
// Everything is table-driven over the three states where a table fits, every
// instant is a parameter built from the calendar (D5 — nothing here sleeps or
// reads a clock, and TestTheEvaluatorIsPure fails the file that does), and the
// store is always the counting fake: never the real one.
//
// The renderer comparison this file makes is against Verdict.Sentence() as the
// model of a single-renderer file (verdict.go, T4): one function assembles the
// string, every surface prints it. This file pins the in-force forms the way
// verdict_test.go pins the refusal forms, and the guard at the bottom walks the
// module looking for a second assembly of them.

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// The instants this file asks its questions at. Fixed, built from the calendar
// (D5): `at` is the caller's, and the boundary that would read a clock is the
// one being tested.
var (
	inforceAsked = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	inforceTold  = time.Date(2026, 10, 19, 11, 0, 0, 123456789, time.UTC)
)

// The three pinned forms, spelled once so every test compares against the same
// bytes and a paraphrase in one test cannot quietly bless a paraphrase in the
// renderer.
const (
	pinnedAcknowledged   = "in force · acknowledged by agent:boss since 2026-10-19T11:00:00Z"
	pinnedUnacknowledged = "in force · unacknowledged — still enforced (PL10)"
	// The could-not-determine form is pinned over a type that still has no
	// evaluator. It named command_access until S11 gave that type one, then
	// route_access until S21 T4 gave that one its evaluator (RA4's subject
	// extension); pinning it over a filled type would pin a sentence the code
	// can no longer produce. effect_access is one of the two that remain, and
	// the table below renders exactly this row's answer.
	// (effect_class is its wire string; the constant is EffectClass.)
	pinnedUndeterminable = "could not determine · no evaluator for effect_class yet (D10)"
)

// inforceRow builds a stored-shaped row (valid closed vocabulary, empty spec)
// for a type whose spec shape this file has no business validating: the answer
// under test reads the type, the target and the records, never the spec.
func inforceRow(id string, typ policy.PolicyType, kind policy.TargetKind, targetID, action string) policy.Row {
	return policy.Row{
		ID: id, Type: string(typ), TargetKind: string(kind), TargetID: targetID,
		Action: action, Source: "operator", SpecJSON: "{}",
		CreatedAt: "2026-10-19T10:00:00Z",
	}
}

// inforceAnswers is InForceFor with the two shapes every test has to survive
// decided once, here: a refusal is a fatality in a test that asked a legal
// question.
func inforceAnswers(t *testing.T, s *policy.Store, target policy.Target, at time.Time) []policy.InForceAnswer {
	t.Helper()
	got, rej := s.InForceFor(context.Background(), target, at)
	if rej != nil {
		t.Fatalf("InForceFor(%s, %s) refused: %s", target.String(), at.Format(time.RFC3339Nano), rej.Reason)
	}
	return got
}

// answerFor picks one answer by policy id, so a test never asserts against a
// slice index it would have to keep in its head.
func answerFor(t *testing.T, answers []policy.InForceAnswer, id string) policy.InForceAnswer {
	t.Helper()
	for _, a := range answers {
		if a.Policy.ID == id {
			return a
		}
	}
	t.Fatalf("no answer for policy %q (got %v)", id, idsOf(answers))
	return policy.InForceAnswer{}
}

func stateOf(t *testing.T, answers []policy.InForceAnswer, id string) policy.InForceState {
	t.Helper()
	return answerFor(t, answers, id).State
}

func idsOf(answers []policy.InForceAnswer) []string {
	out := make([]string, 0, len(answers))
	for _, a := range answers {
		out = append(out, a.Policy.ID)
	}
	return out
}

func inforceTarget(t *testing.T, kind policy.TargetKind, id string) policy.Target {
	t.Helper()
	target, rej := policy.ParseTarget(kind, id)
	if rej != nil {
		t.Fatalf("helper built an illegal target %q/%q: %s", kind, id, rej.Reason)
	}
	return target
}

// storeWith returns the boundary over a fresh counting fake holding `rows`.
func storeWith(rows ...policy.Row) (*policy.Store, *fakeRepo) {
	repo := &fakeRepo{rows: rows}
	return policy.NewStore(repo), repo
}

// ── the closed set: exactly three, by enumeration (PL12) ─────────────────────

func TestTheThreeInForceStatesAreExactlyThese(t *testing.T) {
	// P31's eighth instance, asserted as a COUNT and as bytes: a fourth value
	// would be a bucket somebody labels "other", and a bucket labelled "other"
	// is how a real signal gets lost.
	got := policy.AllInForceStates()
	want := []policy.InForceState{
		policy.EnforcedAcknowledged, policy.EnforcedUnacknowledged, policy.CouldNotDetermine,
	}
	if len(got) != 3 {
		t.Fatalf("AllInForceStates returns %d states %v; PL12 has exactly three", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("state %d is %q, want %q", i, got[i], want[i])
		}
	}
	// The wire forms, byte for byte: they reach logs, the socket payload and
	// the app's status column, and a rename is a vocabulary change.
	for _, c := range []struct {
		got  policy.InForceState
		want string
	}{
		{policy.EnforcedAcknowledged, "enforced_acknowledged"},
		{policy.EnforcedUnacknowledged, "enforced_unacknowledged"},
		{policy.CouldNotDetermine, "could_not_determine"},
	} {
		if string(c.got) != c.want {
			t.Errorf("a state renders as %q on the wire, want %q", string(c.got), c.want)
		}
	}
	// Three distinct values — the two-booleans disease is exactly this: four
	// combinations, three of them real, one of them meaningless, and somebody
	// renders the meaningless one.
	seen := map[policy.InForceState]bool{}
	for _, s := range got {
		if seen[s] {
			t.Errorf("%q appears twice in the closed set", s)
		}
		seen[s] = true
	}
	// And the list is a copy, like every other closed set: editing what you
	// were handed tells you nothing about what the engine will answer.
	got[0] = policy.InForceState("handed")
	if again := policy.AllInForceStates(); again[0] != policy.EnforcedAcknowledged {
		t.Errorf("the closed set is shared mutable state: %v", again)
	}
}

// ── all three states are reachable ───────────────────────────────────────────

func TestAllThreeStatesAreReachable(t *testing.T) {
	// Each state constructed from the rows the boundary actually reads — a
	// stub-type rule, a live rule with a record, a live rule without one — and
	// nothing else about them (no spec shape, no source) may decide the answer.
	cases := []struct {
		name     string
		id       string
		row      policy.Row
		told     bool // whether an acceptance record exists for (id, agent:boss)
		want     policy.InForceState
		wantRend string
	}{
		{
			name:     "a rule the binary cannot evaluate",
			id:       "effects-unclassified",
			row:      inforceRow("effects-unclassified", policy.EffectClass, policy.AgentKind, "boss", "deny"),
			want:     policy.CouldNotDetermine,
			wantRend: pinnedUndeterminable,
		},
		{
			name:     "a live rule the target was told about",
			id:       "no-bash",
			row:      row("no-bash", "deny", "operator", "agent", "boss", "", deny()),
			told:     true,
			want:     policy.EnforcedAcknowledged,
			wantRend: pinnedAcknowledged,
		},
		{
			name:     "a live rule nobody ever mentioned",
			id:       "no-write",
			row:      row("no-write", "deny", "operator", "agent", "boss", "", `{"toolName":"write","state":"deny"}`),
			want:     policy.EnforcedUnacknowledged,
			wantRend: pinnedUnacknowledged,
		},
	}
	repo := &fakeRepo{}
	s := policy.NewStore(repo)
	for _, c := range cases {
		repo.rows = append(repo.rows, c.row)
		if c.told {
			mustRecord(t, s, c.id, inforceTarget(t, policy.AgentKind, "boss"), "operator", inforceTold)
		}
	}

	answers := inforceAnswers(t, s, inforceTarget(t, policy.AgentKind, "boss"), inforceAsked)
	if len(answers) != len(cases) {
		t.Fatalf("the answer covers %d rules %v, want %d", len(answers), idsOf(answers), len(cases))
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := answerFor(t, answers, c.id)
			if a.State != c.want {
				t.Errorf("%q is answered %q, want %q", c.id, a.State, c.want)
			}
			if a.Render() != c.wantRend {
				t.Errorf("%q renders\n %q\nwant\n %q", c.id, a.Render(), c.wantRend)
			}
			if a.Target.String() != "agent:boss" {
				t.Errorf("%q names %q as the target the question was asked about", c.id, a.Target.String())
			}
		})
	}

	// The acknowledged half carries its "since when", and the other two carry
	// no instant to misread.
	if got := answerFor(t, answers, "no-bash").AcknowledgedAt; got == nil || !got.Equal(inforceTold) {
		t.Errorf("the acknowledged answer carries %v, want the first record instant %s", got, inforceTold)
	}
	for _, id := range []string{"effects-unclassified", "no-write"} {
		if got := answerFor(t, answers, id).AcknowledgedAt; got != nil {
			t.Errorf("%q carries an instant %v it never earned; only a record produces one", id, got)
		}
	}
}

// ── unacknowledged is distinguishable from could-not-determine ───────────────

func TestUnacknowledgedIsDistinguishableFromCouldNotDetermine(t *testing.T) {
	// The pair the design is actually about: "never told (and still bound)" and
	// "nobody can tell yet" are the two an operator acts on differently, so
	// they differ in the state bytes AND in the sentence bytes — a difference
	// only one of the two carries is a difference a surface can lose.
	_, repo := storeWith(
		inforceRow("cap", policy.CostCap, policy.AgentKind, "boss", "deny"),
		row("no-bash", "deny", "operator", "agent", "boss", "", deny()),
	)
	s := policy.NewStore(repo)
	answers := inforceAnswers(t, s, inforceTarget(t, policy.AgentKind, "boss"), inforceAsked)

	un := answerFor(t, answers, "no-bash")
	cnd := answerFor(t, answers, "cap")

	if un.State == cnd.State {
		t.Fatalf("both answers claim %q; the two states are one state", un.State)
	}
	if string(un.State) == string(cnd.State) {
		t.Errorf("the state bytes are identical (%q)", string(un.State))
	}
	if !strings.HasPrefix(string(un.State), "enforced_") || strings.HasPrefix(string(cnd.State), "enforced_") {
		t.Errorf("the states do not say what they are: %q / %q", un.State, cnd.State)
	}
	if un.Render() == cnd.Render() {
		t.Errorf("both answers render %q; the distinction stops at the type", un.Render())
	}
	if len(un.State) == 0 || len(cnd.State) == 0 {
		t.Error("one state is the empty string; an empty answer is an answer nobody checked")
	}
}

// ── unacknowledged still reads as enforced (D13's other half) ────────────────

func TestUnacknowledgedStillReadsAsEnforced(t *testing.T) {
	// Wording as a gate, not as taste: the target was never told and the rule
	// still binds. A sentence that reads as a problem with enforcement sends an
	// operator to hunt a policy bug that does not exist (PL10).
	_, repo := storeWith(row("no-bash", "deny", "operator", "agent", "boss", "", deny()))
	s := policy.NewStore(repo)
	got := answerFor(t, inforceAnswers(t, s, inforceTarget(t, policy.AgentKind, "boss"), inforceAsked), "no-bash")

	render := got.Render()
	lower := strings.ToLower(render)
	if !strings.Contains(lower, "in force") {
		t.Errorf("the unacknowledged answer does not say it is in force: %q", render)
	}
	for _, banned := range []string{"not in force", "unknown"} {
		if strings.Contains(lower, banned) {
			t.Errorf("the unacknowledged answer contains %q; it reads as doubt about enforcement, which is the "+
				"lie this criterion exists to refuse: %q", banned, render)
		}
	}
	if !strings.Contains(render, "unacknowledged") {
		t.Errorf("the answer no longer names the gap that IS the signal: %q", render)
	}
	if !strings.Contains(render, "still enforced (PL10)") {
		t.Errorf("the answer no longer says the enforcement is intact: %q", render)
	}
	if render != pinnedUnacknowledged {
		t.Errorf("the pinned form moved:\n got %q\nwant %q", render, pinnedUnacknowledged)
	}
	// And it shares its head with the acknowledged answer: told or not told,
	// BOTH are in force — that sharing is the claim, not an accident.
	ack := policy.InForceAnswer{
		Policy: policy.Policy{ID: "x", Type: policy.ToolAccess},
		Target: inforceTarget(t, policy.AgentKind, "boss"),
		State:  policy.EnforcedAcknowledged,
	}
	if head := pinnedAcknowledged[:len(inForceHeadText)]; ack.Render()[:len(inForceHeadText)] != head {
		t.Errorf("the acknowledged answer no longer opens with %q: %q", inForceHeadText, ack.Render())
	}
}

const inForceHeadText = "in force"

// ── no state collapses into another in rendering ─────────────────────────────

func TestNoStateCollapsesInRendering(t *testing.T) {
	// The criterion the seed calls cosmetic-and-total: an unacknowledged answer
	// rendering like an acknowledged one deletes the signal while everything
	// still appears to work. So all three renders are pairwise distinct AND
	// distinct from the empty string, and a hand-built answer claiming a state
	// outside the closed set renders as ITSELF, never as the nearest real
	// answer.
	acks := time.Date(2026, 10, 19, 9, 30, 0, 0, time.UTC)
	boss := inforceTarget(t, policy.AgentKind, "boss")

	// The table is HOMOGENEOUS on purpose: one policy value, one target, and
	// the only variance across the three rows is State (plus the instant the
	// acknowledged answer exists to report). Heterogeneous inputs would weaken
	// this test to a test of its own inputs — a renderer that collapsed two
	// states while interpolating policy data (an id, a status, a type) would
	// still produce three different strings, and the collapse would pass. With
	// the inputs pinned, distinctness can only come from the state, which is
	// the one thing this criterion is about.
	shared := policy.Policy{
		ID: "pol-in-force", Type: policy.ToolAccess,
		Target: inforceTarget(t, policy.AllAgents, ""),
		Action: policy.Deny, Source: policy.Operator, Spec: json.RawMessage(`{"tool":"bash"}`),
	}
	three := map[policy.InForceState]policy.InForceAnswer{
		policy.EnforcedAcknowledged: {
			Policy: shared, Target: boss,
			State: policy.EnforcedAcknowledged, AcknowledgedAt: &acks,
		},
		policy.EnforcedUnacknowledged: {
			Policy: shared, Target: boss,
			State: policy.EnforcedUnacknowledged,
		},
		policy.CouldNotDetermine: {
			Policy: shared, Target: boss,
			State: policy.CouldNotDetermine,
		},
	}
	states := policy.AllInForceStates()
	rendered := make(map[string]policy.InForceState, 3)
	for _, st := range states {
		a, ok := three[st]
		if !ok {
			t.Fatalf("the table has no answer for %q; the collapse could be hiding there", st)
		}
		text := a.Render()
		if strings.TrimSpace(text) == "" {
			t.Errorf("%q renders as nothing; an empty line is the collapse a surface cannot see", st)
		}
		if other, dup := rendered[text]; dup {
			t.Errorf("%q and %q render identically as %q — the signal of one of them is gone", st, other, text)
		}
		rendered[text] = st
	}
	if len(rendered) != 3 {
		t.Fatalf("%d distinct renderings for three states: %v", len(rendered), rendered)
	}

	// An acknowledged answer whose instant is missing still cannot be mistaken
	// for the unacknowledged one: the gap is stated, not swallowed.
	noInstant := three[policy.EnforcedAcknowledged]
	noInstant.AcknowledgedAt = nil
	if noInstant.Render() == three[policy.EnforcedUnacknowledged].Render() {
		t.Errorf("an acknowledged answer with no instant collapses into the unacknowledged one: %q", noInstant.Render())
	}
	if strings.TrimSpace(noInstant.Render()) == "" {
		t.Error("an acknowledged answer with no instant renders as nothing")
	}

	// The closed set is a STRING type, so anything is constructible; a fourth
	// claim is answered honestly and is never defaulted into a real answer.
	for _, bogus := range []policy.InForceState{"", "acknowledged", "enforced", "not_in_force", "EnforcedAcknowledged"} {
		b := policy.InForceAnswer{Policy: shared, Target: boss, State: bogus}
		text := b.Render()
		if strings.TrimSpace(text) == "" {
			t.Errorf("state %q renders as nothing", bogus)
		}
		if _, collides := rendered[text]; collides {
			t.Errorf("state %q renders as one of the three real answers (%q) — an undefined state must never "+
				"be defaulted into a defined one", bogus, text)
		}
	}

	// The other half of "the renderer, not the inputs": holding State fixed, the
	// sentence must NOT move when the policy's other fields move. Read together
	// with the homogeneous table above, that pins what Render may depend on —
	// State, the answer's Target, the instant, and (for could-not-determine) the
	// policy's Type — and forbids the rest. It is the assertion that catches the
	// collapse the pairwise test alone cannot: a renderer that printed the rule's
	// own scope instead of the target that spoke would still hand back three
	// different strings here, so distinctness by itself would not catch it.
	later := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)
	other := inforceTarget(t, policy.AgentKind, "peon")
	for _, st := range states {
		base := three[st]
		moved := base
		moved.Policy = policy.Policy{
			ID: "a-completely-different-id", Type: base.Policy.Type,
			Target: other, Action: policy.Allow, Source: policy.Agent,
			Spec: json.RawMessage(`{"tool":"rm"}`), NotAfter: &later,
		}
		if moved.Render() != base.Render() {
			t.Errorf("%q renders differently for policy data the answer does not report:\n with %q\n got  %q",
				st, base.Render(), moved.Render())
		}
		// And the acknowledged sentence names the TARGET IT WAS ASKED ABOUT,
		// never the rule's scope: an all-agents rule accepted by agent:boss was
		// acknowledged by agent:boss, and crediting `all-agents` puts words in
		// the mouth of a party that never spoke.
		if st == policy.EnforcedAcknowledged {
			scoped := base
			scoped.Policy.Target = other
			if !strings.Contains(scoped.Render(), "acknowledged by agent:boss ") {
				t.Errorf("the acknowledged sentence no longer names the target it was asked about: %q", scoped.Render())
			}
		}
	}

	// Type is the one policy field could-not-determine legitimately reports (the
	// sentence names which kind has no evaluator yet), so it alone may move that
	// rendering — and it may not move the two enforcement answers.
	typed := three[policy.CouldNotDetermine]
	typed.Policy.Type = policy.CostCap
	if typed.Render() == three[policy.CouldNotDetermine].Render() {
		t.Error("the could-not-determine sentence does not name the type that has no evaluator; the operator " +
			"cannot tell which kind is still unimplemented")
	}
	if !strings.Contains(typed.Render(), string(policy.CostCap)) {
		t.Errorf("the could-not-determine sentence omits the type it is about: %q", typed.Render())
	}
	for _, st := range []policy.InForceState{policy.EnforcedAcknowledged, policy.EnforcedUnacknowledged} {
		moved := three[st]
		moved.Policy.Type = policy.CostCap
		if moved.Render() != three[st].Render() {
			t.Errorf("%q renders differently when the policy type changes; enforcement sentences do not name "+
				"the kind: %q", st, moved.Render())
		}
	}
}

// ── could not determine means "no evaluator", specifically ───────────────────

func TestCouldNotDetermineMeansNoEvaluatorSpecifically(t *testing.T) {
	// Three halves of "specifically": the sweep over the seven real types says
	// the placeholders answer could-not-determine and the types with a real
	// evaluator never does; the CAUSE is the Stub() marker, not the match
	// result and not any failure channel; and the answer type carries no
	// failure channel at all (DB19).

	// The types this binary decides for real, pinned by hand rather than read
	// off the registry — that is what makes the sweep below a check ON the
	// registry rather than a restatement of it. A type arriving with its own
	// evaluator must be added here in the same diff that registers it, which is
	// the same discipline spec_stubs_test.go's table enforces from the other
	// side. CommandAccess joined in S11; ToolAccess was S10's.
	// TriggerAccess joined the decided set at S20 T7 (spec_triggeraccess.go) and
	// RouteAccess at S21 T4 (spec_routeaccess.go) — the same moves the sweep
	// exists to notice (as CommandAccess at S11, SkillAccess at S14).
	decided := map[policy.PolicyType]bool{policy.ToolAccess: true, policy.CommandAccess: true, policy.SkillAccess: true, policy.TriggerAccess: true, policy.RouteAccess: true}

	t.Run("the sweep: two placeholders, five real evaluators", func(t *testing.T) {
		boss := policy.AgentKind
		rows := make([]policy.Row, 0, len(policy.AllPolicyTypes()))
		for _, typ := range policy.AllPolicyTypes() {
			rows = append(rows, inforceRow("r-"+string(typ), typ, boss, "boss", "deny"))
		}
		s, repo := storeWith(rows...)
		answers := inforceAnswers(t, s, inforceTarget(t, policy.AgentKind, "boss"), inforceAsked)
		if len(answers) != len(rows) {
			t.Fatalf("%d answers for %d rules %v", len(answers), len(rows), idsOf(answers))
		}
		for _, typ := range policy.AllPolicyTypes() {
			want := policy.CouldNotDetermine
			if decided[typ] {
				want = policy.EnforcedUnacknowledged
			}
			if got := stateOf(t, answers, "r-"+string(typ)); got != want {
				t.Errorf("type %q is answered %q, want %q", typ, got, want)
			}
		}
		// No acceptance rows exist, so "unacknowledged" is the only answer a
		// decidable type can have — and the sweep read the record exactly once
		// for the target, through the one read.
		if repo.listForCalls != 1 {
			t.Errorf("the acceptance seam was read %d times for one target; one read per question (N8)", repo.listForCalls)
		}
	})

	t.Run("the cause is the marker, and the marker agrees with the answer", func(t *testing.T) {
		// The answer must follow the registry, not this file's opinion about
		// it: for every production type, the state is could-not-determine
		// exactly when the registered evaluator implements the Stub() marker
		// and answers true. (The four registry shapes that make "the marker,
		// not the match result" provable — a real evaluator, a marked
		// placeholder, an unmarked one that still refuses to decide, and an
		// unfinished registration — are pinned over noEvaluatorYet itself in
		// inforce_internal_test.go: a type outside the closed seven cannot be
		// stored at all, List refuses its row, so the distinction cannot be
		// staged through the store.)
		for _, typ := range policy.AllPolicyTypes() {
			ev, ok := policy.EvaluatorFor(typ)
			if !ok {
				t.Fatalf("production type %q has no usable evaluator; every answer would be could-not-determine", typ)
			}
			marked, isMarker := ev.(policy.StubMarker)
			stub := isMarker && marked.Stub()
			want := policy.EnforcedUnacknowledged
			if stub {
				want = policy.CouldNotDetermine
			}
			if decided[typ] && stub {
				t.Errorf("%q declares itself the placeholder; a type this slice implements must not", typ)
			}
			if decided[typ] != !stub {
				t.Errorf("%q is in the decided set but the registry disagrees (stub=%v); the set above and "+
					"the registration are one fact, stated twice, and they must move together", typ, stub)
			}
			_, repo := storeWith(inforceRow("sweep", typ, policy.AgentKind, "boss", "deny"))
			got := stateOf(t, inforceAnswers(t, policy.NewStore(repo), inforceTarget(t, policy.AgentKind, "boss"), inforceAsked), "sweep")
			if got != want {
				t.Errorf("type %q (stub=%v) is answered %q, want %q", typ, stub, got, want)
			}
		}
	})

	t.Run("a decidable type is never answered could-not-determine", func(t *testing.T) {
		// The one sentence the criterion turns on: a non-stub type must never
		// produce the state, whatever else is true about the rule — no record,
		// no matching spec, an allow rather than a deny, every target kind.
		boss := inforceTarget(t, policy.AgentKind, "boss")
		s, repo := storeWith()
		for _, p := range []policy.Policy{
			boundaryPolicy(t, "deny-live", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec()),
			boundaryPolicy(t, "allow-live", policy.AgentKind, "boss", policy.Allow, policy.Operator, allowSpec()),
			boundaryPolicy(t, "all-live", policy.AllAgents, "", policy.Deny, policy.Operator, denySpec()),
		} {
			mustAdd(t, s, p)
		}
		answers := inforceAnswers(t, s, boss, inforceAsked)
		if len(answers) != 3 {
			t.Fatalf("%d answers %v, want 3", len(answers), idsOf(answers))
		}
		for _, a := range answers {
			if a.State == policy.CouldNotDetermine {
				t.Errorf("tool_access rule %q was answered could-not-determine; that state means \"nobody can "+
					"evaluate this type\", and this type has an evaluator (%T) that answers", a.Policy.ID, mustEvaluator(t, a.Policy.Type))
			}
			if a.State != policy.EnforcedUnacknowledged {
				t.Errorf("%q is answered %q with no record in existence", a.Policy.ID, a.State)
			}
		}
		// The record changes the answer; the evaluator's silence about a spec
		// never does.
		mustRecord(t, s, "deny-live", boss, "operator", inforceTold)
		if got := stateOf(t, inforceAnswers(t, s, boss, inforceAsked), "deny-live"); got != policy.EnforcedAcknowledged {
			t.Errorf("the record did not move the answer: %q", got)
		}
		if repo.listCalls != 2 || repo.listForCalls != 2 {
			t.Errorf("two questions cost %d policy reads and %d record reads; two questions cost two of each (N8)",
				repo.listCalls, repo.listForCalls)
		}
	})

	t.Run("the answer has no failure channel (DB19)", func(t *testing.T) {
		// A domain answer is not an infrastructure failure: InForceFor returns
		// an answer or a *Reject, Render returns a string, and inforce.go names
		// the predeclared failure type nowhere at all.
		ft := reflect.TypeOf((*policy.Store).InForceFor)
		if ft.NumIn() != 4 || ft.NumOut() != 2 {
			t.Fatalf("InForceFor is %s; want (store, ctx, target, instant) → ([]InForceAnswer, *Reject)", ft)
		}
		if ft.In(2) != reflect.TypeOf(policy.Target{}) || ft.In(3) != reflect.TypeOf(time.Time{}) {
			t.Errorf("InForceFor takes (%s, %s); the target and the instant are both the caller's (D5)", ft.In(2), ft.In(3))
		}
		errType := reflect.TypeOf((*error)(nil)).Elem()
		for i := 0; i < ft.NumOut(); i++ {
			if out := ft.Out(i); out == errType || out.Implements(errType) {
				t.Errorf("InForceFor returns %s; the boundary answers in *Reject, not in that (DB19)", out)
			}
		}
		if want := reflect.TypeOf((*policy.Reject)(nil)); ft.Out(1) != want {
			t.Errorf("InForceFor's second result is %s, want *policy.Reject", ft.Out(1))
		}
		if rt := reflect.TypeOf(policy.InForceAnswer.Render); rt.NumIn() != 1 || rt.NumOut() != 1 || rt.Out(0).Kind() != reflect.String {
			t.Errorf("Render is %s; a total renderer takes nothing and returns one string", rt)
		}
		if at := reflect.TypeOf(policy.AllInForceStates); at.NumIn() != 0 || at.NumOut() != 1 {
			t.Errorf("AllInForceStates is %s; the enumeration takes nothing", at)
		}
		src := readSource(t, "inforce.go")
		if hits := failureMentions(src); len(hits) > 0 {
			t.Errorf("inforce.go names the failure type %d times (%v); a domain answer has no failure channel, and "+
				"a renderer that can fail is one a surface drops (DB19)", len(hits), hits)
		}
	})
}

// failureMentions reports the identifiers naming the predeclared failure type
// in Go source. Pure over text so its own self-test can hand it the code the
// rule forbids (a guard that cannot fire guards nothing).
func failureMentions(src string) []string {
	f, _, err := parseSource(src)
	if err != nil {
		return []string{"unparseable source"}
	}
	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "error" {
			hits = append(hits, id.Name)
		}
		return true
	})
	return hits
}

func TestTheNoFailureChannelGuardCanFail(t *testing.T) {
	if len(failureMentions("package policy\n\nfunc f() error { return nil }\n")) == 0 {
		t.Error("the guard missed a function returning the failure type")
	}
	if len(failureMentions("package policy\n\nfunc f(p Policy) (int, error) { return 0, nil }\n\ntype t struct{ err error }\n")) < 2 {
		t.Error("the guard missed the return half and the field")
	}
	if len(failureMentions("package policy\n\nfunc f(p Policy) string { return p.ID }\n")) != 0 {
		t.Error("the guard cried wolf on a file with no failure channel")
	}
}

func mustEvaluator(t *testing.T, typ policy.PolicyType) policy.Evaluator {
	t.Helper()
	ev, ok := policy.EvaluatorFor(typ)
	if !ok {
		t.Fatalf("type %q has no usable evaluator", typ)
	}
	return ev
}

// ── an expired policy is excluded at the given at ────────────────────────────

func TestAnExpiredPolicyIsExcludedAtTheGivenAt(t *testing.T) {
	// The caller's `at` decides, with Evaluate's OWN predicate (evaluate.go's
	// expired): strictly-after is alive, equal is gone. The same instant must
	// not mean "alive" to one answer and "enforced" to the other, so the
	// boundary is asked of both, three times, with no sleeping anywhere.
	expiry := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	boss := inforceTarget(t, policy.AgentKind, "boss")

	live := expiring(boundaryPolicy(t, "until-november", policy.AgentKind, "boss", policy.Deny, policy.Operator, denySpec()), expiry)
	s, repo := storeWith()
	mustAdd(t, s, live)
	mustRecord(t, s, "until-november", boss, "operator", inforceTold)

	cases := []struct {
		name string
		at   time.Time
		want int
	}{
		{"one nanosecond before the boundary, still in force", expiry.Add(-time.Nanosecond), 1},
		{"exactly at it, gone", expiry, 0},
		{"one nanosecond after, gone", expiry.Add(time.Nanosecond), 0},
		{"asked before it was even written, still in force", inforceAsked, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := inforceAnswers(t, s, boss, c.at)
			if len(got) != c.want {
				t.Fatalf("at %s the answer lists %v, want %d rule(s)", c.at.Format(time.RFC3339Nano), idsOf(got), c.want)
			}
		})
	}

	// The record cannot resurrect a rule that stopped existing: "it was told"
	// says nothing about a rule that is no longer there.
	if got := idsOf(inforceAnswers(t, s, boss, expiry)); len(got) != 0 {
		t.Errorf("the acceptance record brought an expired rule back as %v", got)
	}

	// The other half of the claim: the two answers agree BECAUSE they share the
	// predicate. At the instant the engine drops the deny, the in-force answer
	// drops the rule; one nanosecond earlier, both keep it.
	req := policy.Request{Type: policy.ToolAccess, Agent: "boss", Tool: "bash"}
	ps, rej := s.List(context.Background(), expiry)
	if rej != nil {
		t.Fatalf("List refused its own row: %s", rej.Reason)
	}
	if v := policy.Evaluate(ps, req, expiry); !v.DefaultDeny {
		t.Errorf("at the boundary the engine still applies the expired deny (%+v); the two answers would disagree "+
			"about a rule that has stopped existing", shapeOf(v))
	}
	if len(inforceAnswers(t, s, boss, expiry)) != 0 {
		t.Error("the engine dropped the expired deny while the in-force answer still calls it enforced")
	}
	before, rej := s.List(context.Background(), expiry.Add(-time.Nanosecond))
	if rej != nil {
		t.Fatalf("List refused its own row: %s", rej.Reason)
	}
	if v := policy.Evaluate(before, req, expiry.Add(-time.Nanosecond)); v.By == nil || v.By.ID != "until-november" {
		t.Fatalf("one nanosecond early the deny should have fired: %+v", shapeOf(v))
	}
	if len(inforceAnswers(t, s, boss, expiry.Add(-time.Nanosecond))) != 1 {
		t.Error("one nanosecond early the engine applies the rule and the in-force answer does not see it")
	}

	// And the exclusion is the CALLER's question: the store still hands back
	// every row (an audit that hides history is an audit that lies), so the
	// filter above is not the read quietly agreeing to forget.
	if repo.listCalls == 0 {
		t.Fatal("no policy was read at all; the exclusions above would be vacuous")
	}
	if all, rej := s.List(context.Background(), expiry); rej != nil || len(all) != 1 {
		t.Errorf("List hid the expired row (%d rows, %v); it is the answer that filters, not the read", len(all), rej)
	}
}

// ── the target, the records, and the boundary's refusals ─────────────────────

func TestInForceForAnswersOnlyAboutTheTargetItWasGiven(t *testing.T) {
	// Another target's rule is not an answer about this one — and a rule
	// addressed to everyone IS an answer about this one, with THIS target as
	// the party that acknowledged.
	boss := inforceTarget(t, policy.AgentKind, "boss")
	s, _ := storeWith(
		inforceRow("about-boss", policy.ToolAccess, policy.AgentKind, "boss", "deny"),
		inforceRow("about-sue", policy.ToolAccess, policy.AgentKind, "sue", "deny"),
		inforceRow("about-everyone", policy.ToolAccess, policy.AllAgents, "", "deny"),
		inforceRow("about-loop", policy.ToolAccess, policy.LoopKind, "l1", "deny"),
	)
	answers := inforceAnswers(t, s, boss, inforceAsked)
	got := strings.Join(idsOf(answers), ",")
	if got != "about-boss,about-everyone" {
		t.Fatalf("asking about agent:boss listed [%s]; another agent's rule and another loop's rule are not "+
			"answers about this target", got)
	}

	// The all-agents rule, acknowledged by this target: the sentence names the
	// party that SPOKE, not the scope of the rule. "Acknowledged by all-agents"
	// would credit a statement to everyone.
	mustRecord(t, s, "about-everyone", boss, "operator", inforceTold)
	answers = inforceAnswers(t, s, boss, inforceAsked)
	a := answerFor(t, answers, "about-everyone")
	if a.State != policy.EnforcedAcknowledged {
		t.Fatalf("an all-agents rule accepted by this target is answered %q", a.State)
	}
	if a.Render() != "in force · acknowledged by agent:boss since 2026-10-19T11:00:00Z" {
		t.Errorf("the sentence names the wrong acknowledger: %q", a.Render())
	}
	if a.Policy.Target.Kind != policy.AllAgents {
		t.Errorf("the answer stopped carrying the rule as stored: %+v", a.Policy.Target)
	}
	// Asking about the loop finds the same all-agents rule and none of agent
	// boss's, and the records for boss are not the loop's records.
	loop := inforceTarget(t, policy.LoopKind, "l1")
	if got := strings.Join(idsOf(inforceAnswers(t, s, loop, inforceAsked)), ","); got != "about-everyone,about-loop" {
		t.Errorf("asking about loop:l1 listed [%s]", got)
	}
	for _, x := range inforceAnswers(t, s, loop, inforceAsked) {
		if x.State == policy.EnforcedAcknowledged {
			t.Errorf("loop:l1 is reported as having acknowledged %q; only boss was told", x.Policy.ID)
		}
	}
}

func TestEveryTargetKindIsAnsweredByItsOwnIdentity(t *testing.T) {
	// "Governs this target" is the engine's own predicate over the five kinds
	// (targetMatches), fed a probe built from the queried target — so every
	// kind is asked, with a rule naming it, a rule naming someone else of the
	// SAME kind (the confusion a sloppy substring match would make), and the
	// rule naming everyone.
	cases := []struct {
		kind      policy.TargetKind
		id        string
		decoyKind policy.TargetKind
		decoyID   string
		wantList  string
	}{
		{policy.AllAgents, "", policy.AgentKind, "sue", "everyone,mine"},
		{policy.AgentKind, "boss", policy.AgentKind, "sue", "everyone,mine"},
		{policy.LoopKind, "l1", policy.LoopKind, "l2", "everyone,mine"},
		{policy.LaneTypeKind, "review", policy.LaneTypeKind, "build", "everyone,mine"},
		{policy.MCPServerKind, "github", policy.MCPServerKind, "gitlab", "everyone,mine"},
	}
	for _, c := range cases {
		name := string(c.kind)
		if c.id != "" {
			name = policy.Target{Kind: c.kind, ID: c.id}.String()
		}
		t.Run(name, func(t *testing.T) {
			s, _ := storeWith(
				inforceRow("mine", policy.ToolAccess, c.kind, c.id, "deny"),
				inforceRow("not-mine", policy.ToolAccess, c.decoyKind, c.decoyID, "deny"),
				inforceRow("everyone", policy.ToolAccess, policy.AllAgents, "", "deny"),
			)
			got := strings.Join(idsOf(inforceAnswers(t, s, policy.Target{Kind: c.kind, ID: c.id}, inforceAsked)), ",")
			if got != c.wantList {
				t.Fatalf("asking about %s listed [%s], want [%s] (the decoy must not answer for %s)", name, got, c.wantList, name)
			}
		})
	}
}

func TestTheEarliestRecordIsTheOneThatAnswersSinceWhen(t *testing.T) {
	// "Since when" only means the FIRST time. The table's own key keeps one row
	// per (policy, target); a hand-edited or out-of-order duplicate keeps its
	// earliest instant rather than the last one written.
	boss := inforceTarget(t, policy.AgentKind, "boss")
	s, repo := storeWith(inforceRow("dup", policy.ToolAccess, policy.AgentKind, "boss", "deny"))
	repo.acceptances = []policy.AcceptanceRow{
		{PolicyID: "dup", TargetKind: "agent", TargetID: "boss", AcceptedBy: "operator", AcceptedAt: "2026-10-19T10:00:00Z"},
		{PolicyID: "dup", TargetKind: "agent", TargetID: "boss", AcceptedBy: "operator", AcceptedAt: "2026-10-18T09:00:00Z"},
	}
	a := answerFor(t, inforceAnswers(t, s, boss, inforceAsked), "dup")
	if a.State != policy.EnforcedAcknowledged {
		t.Fatalf("the duplicate rows answered %q", a.State)
	}
	if a.AcknowledgedAt == nil || !a.AcknowledgedAt.Equal(time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("the answer says since %v; the earliest record is the 18th at 09:00", a.AcknowledgedAt)
	}
	if !strings.HasSuffix(a.Render(), "since 2026-10-18T09:00:00Z") {
		t.Errorf("the sentence says %q", a.Render())
	}
	// Another target's records are not these records.
	repo.acceptances = append(repo.acceptances, policy.AcceptanceRow{
		PolicyID: "dup", TargetKind: "agent", TargetID: "sue", AcceptedBy: "operator", AcceptedAt: "2026-10-17T08:00:00Z",
	})
	if got := stateOf(t, inforceAnswers(t, s, boss, inforceAsked), "dup"); got != policy.EnforcedAcknowledged {
		t.Errorf("sue's record changed boss's answer to %q", got)
	}
}

func TestInForceForRefusesBeforeItReadsAnything(t *testing.T) {
	// The boundary trusts no caller, read or write (SEC-015): a target that
	// cannot name anything and an instant that is not an instant are refused by
	// name, with zero reads issued — a half-asked question must not reach the
	// table at all.
	cases := []struct {
		name   string
		target policy.Target
		at     time.Time
		want   string
	}{
		{
			name:   "a kind that is not in the closed set",
			target: policy.Target{Kind: policy.TargetKind("global")},
			at:     inforceAsked,
			want:   "unknown target kind",
		},
		{
			name:   "an id that repeats its kind",
			target: policy.Target{Kind: policy.LoopKind, ID: "loop:l1"},
			at:     inforceAsked,
			want:   "repeats its kind",
		},
		{
			name:   "a named kind naming nothing",
			target: policy.Target{Kind: policy.AgentKind},
			at:     inforceAsked,
			want:   "must name the thing it governs",
		},
		{
			name:   "no instant at all",
			target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
			at:     time.Time{},
			want:   "no instant",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, repo := storeWith(inforceRow("any", policy.ToolAccess, policy.AgentKind, "boss", "deny"))
			_, rej := s.InForceFor(context.Background(), c.target, c.at)
			if rej == nil {
				t.Fatalf("the request was accepted; it must be refused (%q expected in the reason)", c.want)
			}
			if !strings.Contains(rej.Reason, c.want) {
				t.Errorf("the refusal does not name the defect %q: %s", c.want, rej.Reason)
			}
			if repo.listCalls != 0 || repo.listForCalls != 0 {
				t.Errorf("the refusal still read the store (%d policy reads, %d record reads)", repo.listCalls, repo.listForCalls)
			}
		})
	}
}

func TestInForceForFoldsAFailedReadIntoARefusalNamingTheSeam(t *testing.T) {
	// The machine breaking is said once, in the shape every caller already
	// handles (Add's discipline): the answer was NOT computed, and there is no
	// partial answer on offer. A half-read record table would report
	// "unacknowledged" for rules whose records the read simply lost — the one
	// wrong answer this file could give.
	s, repo := storeWith(inforceRow("any", policy.ToolAccess, policy.AgentKind, "boss", "deny"))
	repo.listErr = errors.New("read pool gone")
	_, rej := s.InForceFor(context.Background(), inforceTarget(t, policy.AgentKind, "boss"), inforceAsked)
	if rej == nil {
		t.Fatal("the policy-read failure vanished")
	}
	for _, want := range []string{"NOT read", "read pool gone"} {
		if !strings.Contains(rej.Reason, want) {
			t.Errorf("the fold lost %q: %s", want, rej.Reason)
		}
	}

	s, repo = storeWith(inforceRow("any", policy.ToolAccess, policy.AgentKind, "boss", "deny"))
	repo.listForErr = errors.New("record reader gone")
	_, rej = s.InForceFor(context.Background(), inforceTarget(t, policy.AgentKind, "boss"), inforceAsked)
	if rej == nil {
		t.Fatal("the record-read failure vanished")
	}
	for _, want := range []string{"NOT computed", "record reader gone"} {
		if !strings.Contains(rej.Reason, want) {
			t.Errorf("the fold lost %q: %s", want, rej.Reason)
		}
	}
	if repo.listForCalls != 1 {
		t.Errorf("the record seam was reached %d times", repo.listForCalls)
	}

	// A tampered record refuses the whole answer rather than dropping the row
	// the answer would have liked to ignore (T7's decode is the one gate, and
	// this answer inherits it).
	s, repo = storeWith(inforceRow("any", policy.ToolAccess, policy.AgentKind, "boss", "deny"))
	repo.acceptances = []policy.AcceptanceRow{{PolicyID: "any", TargetKind: "agent", TargetID: "boss", AcceptedBy: "operator", AcceptedAt: "yesterday"}}
	_, rej = s.InForceFor(context.Background(), inforceTarget(t, policy.AgentKind, "boss"), inforceAsked)
	if rej == nil || !strings.Contains(rej.Reason, "yesterday") {
		t.Errorf("a record whose instant cannot be read was not refused by name: %+v", rej)
	}
}

// ── the one-renderer rule, guarded over the module ────────────────────────────

// inForceRenderMarkers are the pieces of the three sentences that only the
// renderer has any business assembling. A file outside inforce.go that writes
// one is a second renderer being born — and T12's CLI, T13's socket and T14's
// app will then print two different answers to one question.
//
// The engine's own D10 fixed string ("no evaluator for %q yet", evaluate.go
// and spec_stubs.go, and quoted in record.go's header) is deliberately NOT a
// marker: it is the reason a Verdict carries, not one of these three sentences,
// and a hand-built answer is caught by the head it has to reproduce anyway.
var inForceRenderMarkers = []string{
	"in force · ",
	"could not determine · ",
	"still enforced (PL10)",
}

// inForceOffender reports the first pinned fragment a source assembles itself
// without ever calling the renderer. Pure over text, so the self-test below can
// hand it exactly the code the rule forbids.
func inForceOffender(text string) (string, bool) {
	if strings.Contains(text, "Render()") || strings.Contains(text, "InForceFor(") {
		return "", false // forwarding the renderer's output is what surfaces must do
	}
	for _, marker := range inForceRenderMarkers {
		if strings.Contains(text, marker) {
			return marker, true
		}
	}
	return "", false
}

func TestRenderIsTheOnlyInForceRendererInTheModule(t *testing.T) {
	// The same rule verdict.go holds for the refusal sentence, held for this
	// one: one file assembles, every surface prints. The pin file is exempt for
	// the same reason verdict_test.go is — it has to spell the forms to pin
	// them.
	root := moduleRoot(t)
	seen := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		switch filepath.Base(path) {
		case "inforce.go", "inforce_test.go":
			return nil
		}
		seen++
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		marker, bad := inForceOffender(string(src))
		if !bad {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		t.Errorf("%s assembles %q itself and never calls Render(); the three answers are built in inforce.go "+
			"alone — print what InForceAnswer.Render() returns (T12, T13 and T14 print it verbatim)", rel, marker)
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module: %v", err)
	}
	if seen < 20 {
		t.Fatalf("only %d .go files were scanned under %s; this guard would pass almost vacuously", seen, root)
	}
}

func TestTheInForceRendererGuardCanFail(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // the fragment it must be caught on ("" = must not be caught)
	}{
		{
			"a CLI that spells the unacknowledged line itself",
			"package cli\n\nfunc line(a policy.InForceAnswer) string {\n\treturn \"in force · unacknowledged — still enforced (PL10)\"\n}\n",
			"in force · ",
		},
		{
			"a socket handler that assembles the third answer",
			"package protocol\n\nvar answer = \"could not determine · no evaluator for \" + string(t) + \" yet (D10)\"\n",
			"could not determine · ",
		},
		{
			"a surface that forwards the renderer",
			"package cli\n\n// Prints the answer verbatim.\nfunc line(a policy.InForceAnswer) string { return a.Render() }\n",
			"",
		},
		{
			"a wiring file that reaches the boundary",
			"package main\n\nfunc run(s *policy.Store) { _, _ = s.InForceFor(ctx, target, at) }\n",
			"",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			marker, bad := inForceOffender(c.src)
			if c.want == "" {
				if bad {
					t.Errorf("the guard cried wolf on source that forwards the renderer: flagged %q", marker)
				}
				return
			}
			if !bad {
				t.Fatalf("the guard missed a hand-assembled answer: %s", c.src)
			}
			if marker != c.want {
				t.Errorf("the guard found %q, want %q", marker, c.want)
			}
		})
	}

	// And the marker list still names what the real renderer writes, or the
	// guard is checking sentences nobody renders. The two heads are constants
	// glued to the shared separator, so they are pinned as the constants and
	// as the tails they carry.
	real, err := os.ReadFile("inforce.go")
	if err != nil {
		t.Fatalf("read inforce.go: %v", err)
	}
	text := string(real)
	for _, must := range []string{
		`"in force"`,
		`"could not determine"`,
		`"unacknowledged — still enforced (PL10)"`,
		`"no evaluator for "`,
		`" yet (D10)"`,
	} {
		if !strings.Contains(text, must) {
			t.Errorf("inforce.go no longer contains %s; the guard is checking sentences nobody renders", must)
		}
	}
}

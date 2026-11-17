package policy

// Two things can only be proven from inside the package, and both are about
// the answer's cause rather than its wording:
//
//   - the stub marker's truth table. A type outside the closed seven cannot be
//     STORED at all — List refuses such a row before any classification
//     happens, and that refusal is T6's and correct — so the four registry
//     shapes that make "the marker, not the match result" provable are pinned
//     here, over noEvaluatorYet itself. The production sweep in
//     inforce_test.go pins that InForceFor uses exactly this rule;
//   - the refusal branch of the acceptance read, which is defence in depth:
//     decodeAcceptance (record.go, T7) has already proven the instant parses,
//     so the answer would only see an unreadable one if that check were
//     relaxed later. A branch that cannot be reached through the exported API
//     still has to be proven to ANSWER — refuse by name, hand back no instant
//     — rather than to panic or to return the zero time and let the sentence
//     say "since 0001-01-01T00:00:00Z".
//
// The reachable half of both is tested from outside, in inforce_test.go.

import (
	"strings"
	"testing"
	"time"
)

// ── the four registry shapes the state has to tell apart ─────────────────────

const (
	inforceLiveType   PolicyType = "inforce_live"   // decides for real; declares nothing
	inforceMarkedType PolicyType = "inforce_marked" // declares itself the placeholder
	inforceSilentType PolicyType = "inforce_silent" // refuses to decide, declares NOTHING
	inforceNoneType   PolicyType = "inforce_none"   // registered with a nil evaluator
)

// inforceValidator accepts every spec: these types answer registry questions,
// not spec-shape questions (T5 owns those, and they are already pinned).
type inforceValidator struct{}

func (inforceValidator) Validate(Policy) *Reject { return nil }

// inforceLiveEvaluator decides — it simply matches nothing here. It declares
// NO Stub method, and that absence IS the false.
type inforceLiveEvaluator struct{}

func (inforceLiveEvaluator) Match(Policy, Request) MatchRes {
	return MatchRes{Note: "the live evaluator matches nothing in these tests"}
}

// inforceMarkedEvaluator is the placeholder shape: registered, consulted,
// refusing, and SAYING so.
type inforceMarkedEvaluator struct{}

func (inforceMarkedEvaluator) Match(Policy, Request) MatchRes {
	return MatchRes{Unimplemented: true, Note: "the marked placeholder never decides"}
}

func (inforceMarkedEvaluator) Stub() bool { return true }

// inforceSilentEvaluator refuses to decide — Match answers exactly as a
// placeholder does — while declaring nothing. If the state ever followed THIS
// rather than the marker, could-not-determine would start tracking behaviour a
// real evaluator also exhibits (declining to match a request), and the two
// facts T3 kept apart would be back together.
type inforceSilentEvaluator struct{}

func (inforceSilentEvaluator) Match(Policy, Request) MatchRes {
	return MatchRes{Unimplemented: true, Note: "the silent evaluator never decides"}
}

// Registration in init, like every registration: the registry is read
// unsynchronised by every decision (SEC-010).
func init() {
	RegisterType(inforceLiveType, inforceValidator{}, inforceLiveEvaluator{})
	RegisterType(inforceMarkedType, inforceValidator{}, inforceMarkedEvaluator{})
	RegisterType(inforceSilentType, inforceValidator{}, inforceSilentEvaluator{})
	RegisterType(inforceNoneType, inforceValidator{}, nil) // the unfinished registration
}

func TestTheStubMarkerAndNothingElseCausesCouldNotDetermine(t *testing.T) {
	// The whole of the third state's cause, as a truth table over the four
	// shapes a registry can actually hold. `true` here is what InForceFor
	// answers CouldNotDetermine for, and nothing else ever does.
	cases := []struct {
		name string
		typ  PolicyType
		want bool
		why  string
	}{
		{
			name: "a real evaluator that matches nothing",
			typ:  inforceLiveType,
			want: false,
			why:  "it decides; it simply did not match this request — that is a refusal, not an inability",
		},
		{
			name: "a placeholder that declares itself",
			typ:  inforceMarkedType,
			want: true,
			why:  "the marker is the cause",
		},
		{
			name: "an evaluator that refuses to decide and declares nothing",
			typ:  inforceSilentType,
			want: false,
			why:  "behaviour is not the marker; a real evaluator can decline to match too",
		},
		{
			name: "a type registered with no evaluator at all",
			typ:  inforceNoneType,
			want: true,
			why:  "nobody is there to answer, which is the plainest form of \"not yet\"",
		},
		{
			name: "a type nobody registered",
			typ:  PolicyType("inforce_never_registered"),
			want: true,
			why:  "the same fact, stated by the registry's absence",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := noEvaluatorYet(c.typ); got != c.want {
				t.Errorf("noEvaluatorYet(%q) = %v, want %v — %s", c.typ, got, c.want, c.why)
			}
		})
	}

	// And the question is asked of the registry, not answered from a list: the
	// production placeholders say true and the types with a real evaluator say
	// false, so the state's rarity is a property of what is registered.
	// CommandAccess moved out of the placeholder set in S11, SkillAccess at
	// S14, TriggerAccess at S20 and RouteAccess at S21 T4 (RA4's subject
	// extension) — which is exactly the move this sweep is here to notice.
	for _, typ := range AllPolicyTypes() {
		decided := typ == ToolAccess || typ == CommandAccess || typ == SkillAccess || typ == TriggerAccess || typ == RouteAccess
		if got := noEvaluatorYet(typ); got == decided {
			t.Errorf("noEvaluatorYet(%q) = %v; %s", typ, got, map[bool]string{
				true:  "this type has an evaluator that decides, so the in-force answer is about enforcement",
				false: "nothing is registered to decide this type, so the in-force answer is the third state",
			}[decided])
		}
	}
	if _, ok := EvaluatorFor(ToolAccess); !ok {
		t.Fatal("tool_access has no usable evaluator; the sweep above would be vacuous")
	}
	if _, ok := EvaluatorFor(inforceNoneType); ok {
		t.Error("the unfinished registration reports an evaluator")
	}
	// A marker that answers false is the same as no marker at all — the default
	// is the false, and declaring it does not make a real evaluator a stub.
	if evaluatorIsStub(inforceLiveEvaluator{}) || !evaluatorIsStub(inforceMarkedEvaluator{}) {
		t.Error("evaluatorIsStub does not read the marker")
	}
	if evaluatorIsStub(inforceSilentEvaluator{}) {
		t.Error("an unmarked evaluator that refuses to decide was read as a stub")
	}
}

// ── the acceptance read's defence-in-depth refusal ────────────────────────────

func TestTheInForceAcceptanceReadRefusesAnInstantItCannotRead(t *testing.T) {
	boss := Target{Kind: AgentKind, ID: "boss"}
	for _, c := range []struct {
		name   string
		stored string
	}{
		{"the blank", ""},
		{"a word", "yesterday"},
		{"a datetime with no offset", "2026-10-19T11:00:00"},
		{"a month that does not exist", "2026-13-01T00:00:00Z"},
		{"a date with no time", "2026-10-19"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := Acceptance{PolicyID: "no-bash", Target: boss, AcceptedBy: "operator", AcceptedAt: c.stored}
			got, rej := acceptanceInstant(a)
			if rej == nil {
				t.Fatalf("the read accepted %q and handed the answer %s; an unreadable \"since when\" must refuse",
					c.stored, got.Format(time.RFC3339Nano))
			}
			if !got.IsZero() {
				t.Errorf("the refusal still carried an instant (%s); a refusal that returns a value invites a "+
					"caller to use it", got.Format(time.RFC3339Nano))
			}
			for _, want := range []string{"no-bash", "accepted_at", `"` + c.stored + `"`} {
				if !strings.Contains(rej.Reason, want) {
					t.Errorf("the refusal does not name %q: %s", want, rej.Reason)
				}
			}
			if !strings.Contains(rej.Reason, "in-force") {
				t.Errorf("the refusal does not say which answer it is refusing to compute: %s", rej.Reason)
			}
		})
	}

	// The happy path, so the loop above cannot be a function that simply
	// refuses: the instant comes back as an instant, in UTC, whole.
	got, rej := acceptanceInstant(Acceptance{
		PolicyID: "no-bash", Target: boss, AcceptedBy: "operator", AcceptedAt: "2026-10-19T13:00:00.5+02:00",
	})
	if rej != nil {
		t.Fatalf("the read refused a legal instant: %s", rej.Reason)
	}
	want := time.Date(2026, 10, 19, 11, 0, 0, 500000000, time.UTC)
	if !got.Equal(want) {
		t.Errorf("the instant came back as %s, want %s", got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
	if got.Location() != time.UTC {
		t.Errorf("the instant carries %v; the answer renders RFC3339 and the offset is not the record's to keep", got.Location())
	}
}

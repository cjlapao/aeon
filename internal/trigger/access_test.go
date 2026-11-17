package trigger

import (
	"context"
	"strings"
	"testing"
	"time"
)

// access_test.go — T7: authentication is not authorization, tested at the
// seam where the two meet. The policy ENGINE's own decisions are tested in
// internal/policy (spec_triggeraccess_test.go); these tests hold the
// trigger's ORDER and WORDS: the signature first, the policy second, the
// policy's sentence verbatim, and the parenthesis that teaches the
// distinction riding the record — not just the response.

func TestDeniedTriggerKindCannotFire(t *testing.T) {
	// The schedule side of the switch: a denied kind answers `refused`
	// (the THIRD answer — not did_not_fire, which would blame the window,
	// and not skipped, which would blame the previous run), records it,
	// and starts nothing.
	covered := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	s := sched("nightly", "30 3 * * *")
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	fa := &fakeStarter{id: "should-not-exist"}
	sc := newSched(st, fa)
	sc.Gate.Access = func(prov, id string) (bool, string) {
		if prov != string(KindSchedule) || id != "nightly" {
			t.Errorf("the hook saw (%q, %q) — the kind AND the asking trigger must ride the question", prov, id)
		}
		return false, "refused: agent:nightly · trigger_access deny by policy rule"
	}
	at := time.Date(2026, 10, 27, 3, 30, 0, 0, time.UTC)
	if err := sc.Tick(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if fa.call != 0 {
		t.Fatal("a denied kind started a loop")
	}
	if len(st.fires) != 1 || st.fires[0].Outcome != OutcomeRefused {
		t.Fatalf("a denied fire answers %v, want the third answer refused", st.fires)
	}
}

func TestVerifiedWebhookStillEvaluatesTriggerAccess(t *testing.T) {
	// TR9's second half, THE sharpest test in the slice: the delivery is
	// PROVABLY from the sender — and the provable stranger still needs an
	// authored allow.
	body := `{"ref":"verified-by-everybody-trusted-by-none"}`
	_, fa, sc, at := newWebhookSched(t, body)
	var sawProvenance, sawID string
	sc.Gate.Access = func(prov, id string) (bool, string) {
		sawProvenance, sawID = prov, id
		return false, "refused: agent:ship · no allow matched · default deny (PL5)"
	}
	a := sc.DeliverWebhook(context.Background(), "ship", []byte(body), sign(body), "", "d1", "", at)
	if fa.call != 0 {
		t.Fatal("a verified signature bought a start")
	}
	if a.Outcome != OutcomeRefused || a.HTTPStatus != 403 {
		t.Fatalf("the policy must answer 403-refused to a verified delivery: %+v", a)
	}
	if sawProvenance != ProvenanceWebhook || sawID != "ship" {
		t.Fatalf("the hook saw (%q, %q)", sawProvenance, sawID)
	}
	for _, must := range []string{"default deny (PL5)", "(signature verified — authentication is not authorization)"} {
		if !strings.Contains(a.Reason, must) {
			t.Fatalf("the refusal must carry %q — the sentence IS the lesson: %q", must, a.Reason)
		}
	}
}

func TestSignatureIsCheckedBeforePolicy(t *testing.T) {
	// Scope 3's ORDER, measured by calls: a bad signature never reaches
	// governance (a stranger gets the door, not the committee); a good one
	// always does.
	body := `{"ref":"x"}`
	_, fa, sc, at := newWebhookSched(t, body)
	hookCalls := 0
	sc.Gate.Access = func(string, string) (bool, string) {
		hookCalls++
		return true, ""
	}
	if a := sc.DeliverWebhook(context.Background(), "ship", []byte(body), "sha256=deadbeef", "", "d1", "", at); a.HTTPStatus != 401 {
		t.Fatalf("bad signature: %+v", a)
	}
	if hookCalls != 0 {
		t.Fatal("governance ran for an unauthenticated delivery — the order protects the committee from the queue")
	}
	if a := sc.DeliverWebhook(context.Background(), "ship", []byte(body), sign(body), "", "d2", "", at); a.Outcome != OutcomeFired {
		t.Fatalf("good signature: %+v", a)
	}
	if hookCalls != 1 {
		t.Fatalf("a verified delivery skipped governance (%d calls) — authentication is not authorization", hookCalls)
	}
	if fa.call != 1 {
		t.Fatalf("starts: %d", fa.call)
	}
}

func TestRefusalCarriesPolicySentence(t *testing.T) {
	// S10's sentence, one formatter, VERBATIM — the fire record must be
	// searchable by the same string `aeon policy` prints. The schedule
	// path carries it bare (no signature to parenthesise); the webhook
	// path appends the parenthesis and NOTHING else.
	sentence := "refused: agent:nightly · trigger_access · rule `no-schedules-in-prod` (deny) matched · PL4"
	covered := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	s := sched("nightly", "30 3 * * *")
	s.CoveredUntil = covered.Format(time.RFC3339Nano)
	st := &fakeStore{scheds: []Schedule{s}, covered: map[string]string{}}
	sc := newSched(st, &fakeStarter{})
	sc.Gate.Access = func(string, string) (bool, string) { return false, sentence }
	if err := sc.Tick(context.Background(), time.Date(2026, 10, 27, 3, 30, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if len(st.fires) != 1 || st.fires[0].Reason != sentence {
		t.Fatalf("the record must carry the sentence verbatim, got %v", st.fires)
	}
	body := `{"ref":"x"}`
	st2, _, sc2, at2 := newWebhookSched(t, body)
	sc2.Gate.Access = func(string, string) (bool, string) { return false, sentence }
	a := sc2.DeliverWebhook(context.Background(), "ship", []byte(body), sign(body), "", "d1", "", at2)
	if a.Reason != sentence+" (signature verified — authentication is not authorization)" {
		t.Fatalf("byte-exact: sentence + parenthesis, got %q", a.Reason)
	}
	for _, f := range st2.fires {
		if f.Outcome == OutcomeRefused && f.Reason != sentence+" (signature verified — authentication is not authorization)" {
			t.Fatalf("the RECORD must carry the same bytes the response did: %q", f.Reason)
		}
	}
}

func TestPolicyRefusalIsNotSkippedNorDidNotFire(t *testing.T) {
	// The five answers stay five (TR4): with the window passing and no
	// previous run, a governance refusal is REFUSED — the answer that
	// blames neither the schedule's contract nor its last run. The
	// record's word is what an operator greps for.
	body := `{"ref":"x"}`
	_, _, sc, at := newWebhookSched(t, body)
	sc.Gate.Overlap = func(Schedule, time.Time) (bool, string) { return false, "" }
	sc.Gate.Access = func(string, string) (bool, string) { return false, "refused: no" }
	a := sc.DeliverWebhook(context.Background(), "ship", []byte(body), sign(body), "", "d1", "", at)
	if a.Outcome != OutcomeRefused {
		t.Fatalf("governance refusal answered %q — it must not collapse into skipped or did_not_fire", a.Outcome)
	}
}

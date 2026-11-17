package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// trigger_access_test.go — T7 where the policy rows and the firing body
// meet: the daemon's OWN wired hook, the store's OWN rows, and the claim
// the notes must shout — a fresh install fires NOTHING.

func TestFreshInstallFiresNothing(t *testing.T) {
	// The empty-policy state, proved against the daemon a user actually
	// starts: no trigger_access rows exist, so PL5's default deny answers
	// EVERY tick — refused, recorded, unstarted. Not a bug; the switch is
	// `aeon policy add trigger_access ...`, and the fire record says so.
	s := recoveryStore(t)
	d := New(Options{Store: s})
	ws := t.TempDir()
	ctx := context.Background()
	if err := s.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{
		ID: "nightly-survey", Kind: "schedule", Blueprint: "two-step",
		Workspace: ws, Expr: "30 3 * * *", Zone: "UTC", CreatedAt: "2026-10-27T00:00:00Z",
		CoveredUntil: time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 27, 3, 30, 0, 0, time.UTC)
	sc := trigger.NewScheduler(triggerRows{s}, d, &fixedClock{t: at})
	// THE daemon's hook — copied, not a friendlier one (T3's discipline).
	sc.Gate = d.triggers.Gate
	if err := sc.Tick(ctx, at); err != nil {
		t.Fatal(err)
	}
	hist, err := s.TriggerFireHistory(ctx, "nightly-survey", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Outcome != "refused" {
		t.Fatalf("a fresh install answered %+v — PL5 must refuse everything", hist)
	}
	// BYTE-EXACT against the one formatter (S10's guard: tests print what
	// Verdict.Sentence() returns, never re-assemble it): the record's
	// reason is exactly the default-deny verdict for THIS subject.
	if want := (policy.Verdict{Subject: "agent:nightly-survey", Type: policy.TriggerAccess, DefaultDeny: true}).Sentence(); hist[0].Reason != want {
		t.Fatalf("the record must carry the engine's own sentence verbatim\n got %q\nwant %q", hist[0].Reason, want)
	}
	loops, err := s.ListLoops(store.ComponentScope{ComponentRoot: ws})
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 0 {
		t.Fatalf("a fresh install started loops: %v", loops)
	}
}

func TestAuthoredAllowUnlocksTheFire(t *testing.T) {
	// The other half of the switch: ONE operator row, and the same fixture
	// fires. Governance is a policy table, not a hardcode — and a deny
	// row beats the allow (PL4) exactly as spec_triggeraccess_test.go
	// proved at the engine layer.
	s := recoveryStore(t)
	d := New(Options{Store: s})
	seedTriggerAllow(t, s, "nightly-survey")
	ws := t.TempDir()
	ctx := context.Background()
	if err := s.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{
		ID: "nightly-survey", Kind: "schedule", Blueprint: "two-step",
		Workspace: ws, Expr: "30 3 * * *", Zone: "UTC", CreatedAt: "2026-10-27T00:00:00Z",
		CoveredUntil: time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 27, 3, 30, 0, 0, time.UTC)
	sc := trigger.NewScheduler(triggerRows{s}, d, &fixedClock{t: at})
	sc.Gate = d.triggers.Gate
	if err := sc.Tick(ctx, at); err != nil {
		t.Fatal(err)
	}
	hist, err := s.TriggerFireHistory(ctx, "nightly-survey", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Outcome != "fired" || hist[0].LoopID == "" {
		t.Fatalf("the allow row did not unlock the fire: %+v", hist)
	}
	awaitSettled(t, s, hist[0].LoopID)
}

func TestVerifiedWebhookWithoutAllowIsRefusedWithTheParenthesis(t *testing.T) {
	// THE end-to-end TR9 proof: real signature, real store, no allow row.
	// Authenticated AND refused, both facts in one recorded sentence — the
	// parenthesis can only be honest because the signature ran first and
	// passed.
	trigger.ResetWebhookReplays()
	s := recoveryStore(t)
	d := New(Options{Store: s})
	ws := seedWebhook(t, s) // deliberately NO seedTriggerAllow — empty-policy state
	body := `{"ref":"provable-stranger"}`
	r := deliverVia(t, d, protocol.WebhookDeliverParams{ID: "ship", Payload: body, Signature: daemonSign(body), DeliveryID: "del-p1"})
	if r.Outcome != "refused" || r.HTTPStatus != 403 {
		t.Fatalf("an allowed-less verified delivery: %+v", r)
	}
	denied := (policy.Verdict{Subject: "agent:ship", Type: policy.TriggerAccess, DefaultDeny: true}).Sentence()
	if want := denied + " (signature verified — authentication is not authorization)"; r.Reason != want {
		t.Fatalf("the refusal is the engine's sentence plus the earned parenthesis\n got %q\nwant %q", r.Reason, want)
	}
	hist, err := s.TriggerFireHistory(context.Background(), "ship", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Outcome != "refused" || !strings.Contains(hist[0].Reason, "signature verified") {
		t.Fatalf("the RECORD carries both facts: %+v", hist)
	}
	loops, err := s.ListLoops(store.ComponentScope{ComponentRoot: ws})
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 0 {
		t.Fatalf("a provable stranger started loops: %v", loops)
	}
}

func TestGovernanceFailsClosedWhenThePolicySetIsUnreadable(t *testing.T) {
	// The hook's worst day: the store cannot answer. Governance guesses
	// nothing — it refuses, carrying the engine's default-deny rendering
	// and the stated reason. (A live daemon on a closed store would fail
	// its start path anyway; the hook alone is exercised here.)
	s := recoveryStore(t)
	d := New(Options{Store: s})
	if allow, _ := d.triggers.Gate.Access("schedule", "anything"); allow {
		t.Fatal("the empty policy set must not allow — the hook itself is the claim")
	}
	// Now unreadable: close the store the hook reads.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	allow, reason := d.triggers.Gate.Access("schedule", "anything")
	if allow {
		t.Fatal("an unreadable policy set allowed a fire — governance does not guess")
	}
	// Measured failure mode: the closed store PANICS through the repo
	// callbacks (nil deref), and the governance hook recovers — a hook
	// that crashed would take every schedule down with it. Whatever the
	// shape of the read failure (error or panic), the honest words are:
	// refused, and it says it refused rather than guessed.
	if !strings.Contains(reason, "refusing, not guessing") {
		t.Fatalf("fail-closed must SAY it refused rather than guessed: %q", reason)
	}
	if !strings.Contains(reason, "trigger_access") {
		t.Fatalf("the refusal names what was asked about: %q", reason)
	}
}

func TestKindNamedRowsUnlockNothing(t *testing.T) {
	// Finding #3, pinned (T9 walk, 2026-10-27): the hook once matched the
	// request's Trigger against the trigger's KIND, so an operator's
	// `{"trigger":"schedule"}` row admitted EVERY schedule — a kind-wide
	// master key wearing a per-trigger subject, and the fixtures mirrored
	// the bug so the units stayed green. The row names the TRIGGER.
	trigger.ResetWebhookReplays()
	s := recoveryStore(t)
	d := New(Options{Store: s})
	seedTriggerAllow(t, s, "schedule") // the OLD shape: a kind word, not a name
	ws := t.TempDir()
	if err := s.SaveTriggerSchedule(context.Background(), store.TriggerScheduleRow{
		ID: "nightly-survey", Kind: "schedule", Blueprint: "two-step", Workspace: ws,
		Expr: "0 4 * * *", Zone: "UTC", CreatedAt: "2026-10-27T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := d.Table.TriggerFireNow(context.Background(), protocol.Actor{Kind: "user", ID: "operator"}, "nightly-survey")
	if err != nil {
		t.Fatal(err)
	}
	r := raw.(protocol.TriggerControlResult)
	if r.Outcome != "refused" || r.LoopID != "" {
		t.Fatalf("a row naming a KIND unlocked a fire — governance is per trigger, ever: %+v", r)
	}
	if want := (policy.Verdict{Subject: "agent:nightly-survey", Type: policy.TriggerAccess, DefaultDeny: true}).Sentence(); r.Reason != want {
		t.Fatalf("the refusal must be the engine's sentence for the NAMED trigger\n got %q\nwant %q", r.Reason, want)
	}
}

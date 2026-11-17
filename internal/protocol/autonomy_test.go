package protocol

// autonomy_test.go — S56 T6 (AL11): the socket human door over the fake seam.
// The handler is the test subject here: the peer stamp, the refusal-as-typed-
// error, the event after the commit, and the identity-key door. The seam's
// own behaviour lives in internal/daemon/autonomy_test.go (store-backed) —
// this file proves the door, that file proves the bridge.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// autonomyMethodNames is the ten S56 autonomy.* methods, registration order.
func autonomyMethodNames() []MethodName {
	return []MethodName{
		MethodAutonomyList, MethodAutonomyShow, MethodAutonomyVocabulary,
		MethodAutonomyEffective, MethodAutonomyPinned, MethodAutonomyChanges,
		MethodAutonomyUses, MethodAutonomyGrant, MethodAutonomyDeny,
		MethodAutonomyRevoke,
	}
}

// --- the fake seam ----------------------------------------------------------

// autonomyFake is the AutonomyService test double: scripted results and
// refusals, recorded calls. A pointer to the zero value is a valid seam (the
// handshake's exact-set test wires one before the listing): a method on a
// zero fake answers empty, never panics.
type autonomyFake struct {
	// the write side: what the seam saw
	lastActor   Actor
	grantCalls  int
	denyCalls   int
	revokeCalls int
	grantIn     AutonomyGrantRequest
	denyIn      AutonomyDenyRequest
	revokeIn    AutonomyRevokeRequest
	// scripted refusals — the pure validator's / daemon's own sentences,
	// set per test case
	grantRef  *AutonomyRefusal
	denyRef   *AutonomyRefusal
	revokeRef *AutonomyRefusal
	// scripted success outcomes
	grantOut  AutonomyWriteOutcome
	denyOut   AutonomyWriteOutcome
	revokeOut AutonomyWriteOutcome

	// the read side: scripted results
	grants    []GrantView
	showView  GrantView
	showRef   *AutonomyRefusal
	vocab     VocabularyView
	effective EffectiveView
	effRef    *AutonomyRefusal
	pinned    PinnedView
	pinRef    *AutonomyRefusal
	changes   []ChangeView
	uses      []UseView
}

func (f *autonomyFake) List(context.Context, string, string, bool) ([]GrantView, error) {
	return f.grants, nil
}

func (f *autonomyFake) Show(context.Context, string) (GrantView, *ChangeView, int, []string, *AutonomyRefusal, error) {
	return f.showView, nil, 0, nil, f.showRef, nil
}

func (f *autonomyFake) Vocabulary() (VocabularyView, error) { return f.vocab, nil }

func (f *autonomyFake) Effective(context.Context, string, string, []string) (EffectiveView, *AutonomyRefusal, error) {
	return f.effective, f.effRef, nil
}

func (f *autonomyFake) Pinned(context.Context, string) (PinnedView, *AutonomyRefusal, error) {
	return f.pinned, f.pinRef, nil
}

func (f *autonomyFake) Changes(context.Context, string, string, int) ([]ChangeView, error) {
	return f.changes, nil
}

func (f *autonomyFake) Uses(context.Context, string, string, string, int) ([]UseView, error) {
	return f.uses, nil
}

func (f *autonomyFake) Grant(_ context.Context, actor Actor, in AutonomyGrantRequest) (AutonomyWriteOutcome, *AutonomyRefusal, error) {
	f.grantCalls++
	f.lastActor = actor
	f.grantIn = in
	if f.grantRef != nil {
		return AutonomyWriteOutcome{}, f.grantRef, nil
	}
	return f.grantOut, nil, nil
}

func (f *autonomyFake) Deny(_ context.Context, actor Actor, in AutonomyDenyRequest) (AutonomyWriteOutcome, *AutonomyRefusal, error) {
	f.denyCalls++
	f.lastActor = actor
	f.denyIn = in
	if f.denyRef != nil {
		return AutonomyWriteOutcome{}, f.denyRef, nil
	}
	return f.denyOut, nil, nil
}

func (f *autonomyFake) Revoke(_ context.Context, actor Actor, in AutonomyRevokeRequest) (AutonomyWriteOutcome, *AutonomyRefusal, error) {
	f.revokeCalls++
	f.lastActor = actor
	f.revokeIn = in
	if f.revokeRef != nil {
		return AutonomyWriteOutcome{}, f.revokeRef, nil
	}
	return f.revokeOut, nil, nil
}

// userActorFor stamps the connection as the named user — what the daemon's
// ActorFor does in production (the kernel's attestation, never a claim).
func userActorFor(id string) func(context.Context, *Session) Actor {
	return func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: id} }
}

// autonomyTable is a complete, initialised table wired with the fake seam —
// the test's door onto all ten methods.
func autonomyTable(t *testing.T, f *autonomyFake) *Table {
	t.Helper()
	tbl := newTestTable(t)
	tbl.ActorFor = userActorFor("peer-1")
	tbl.Autonomy = f
	if !tbl.RegisterAutonomy() {
		t.Fatal("RegisterAutonomy refused a wired seam")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl
}

// --- the tests ---------------------------------------------------------------

// TestAutonomyMethodsRegistered — the T6 table's first row: the exact set of
// ten, all RequiresInitialize, none Debug, and ABSENT (not refusing) without
// the seam (V13, the settings precedent).
func TestAutonomyMethodsRegistered(t *testing.T) {
	names := autonomyMethodNames()
	if len(names) != 10 {
		t.Fatalf("the set is %d names, want exactly ten — count it, don't guess it", len(names))
	}

	// nil seam: registration is a no-op, the ten are absent.
	plain := newTestTable(t)
	if plain.RegisterAutonomy() {
		t.Fatal("RegisterAutonomy over a nil seam claimed the methods — V13 wants them absent, not refusing")
	}
	initialized(t, plain, "c1", PROTOCOL_VERSION)
	for _, m := range names {
		if _, ok := plain.Lookup(m); ok {
			t.Errorf("%s is registered without its seam — a nil AutonomyService must leave it ABSENT (V13)", m)
		}
		_, err := plain.Dispatch(context.Background(), "c1", m, nil)
		var unknown ErrUnknownMethod
		if !errors.As(err, &unknown) {
			t.Errorf("%s without its seam answered %v — absent is ErrUnknownMethod, not a refusal", m, err)
		}
	}

	// wired seam: the ten appear, RequiresInitialize, not Debug.
	tbl := newTestTable(t)
	tbl.Autonomy = &autonomyFake{}
	if !tbl.RegisterAutonomy() {
		t.Fatal("RegisterAutonomy refused a wired seam")
	}
	for _, m := range names {
		method, ok := tbl.Lookup(m)
		if !ok {
			t.Errorf("%s is missing from the table RegisterAutonomy claimed to fill", m)
			continue
		}
		if !method.RequiresInitialize {
			t.Errorf("%s is reachable before initialize — the autonomy door opens on a negotiated connection (T7)", m)
		}
		if method.Debug {
			t.Errorf("%s is Debug-marked — an autonomy method is the app's window, never an operator probe (AL17)", m)
		}
	}
}

// TestGrantStampsThePeerNotParams — T6 behaviour 9's write half and the Do-NT
// "no identity in params": the actor is the connection's kernel attestation
// (AL11 lock 2), and the five identity keys are refused on every method.
func TestGrantStampsThePeerNotParams(t *testing.T) {
	f := &autonomyFake{}
	tbl := autonomyTable(t, f)

	if _, err := tbl.Dispatch(context.Background(), "c1", MethodAutonomyGrant,
		mustJSON(t, AutonomyGrantParams{RootID: "root-1", Subject: "forge.openPR"})); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if f.lastActor != (Actor{Kind: "user", ID: "peer-1"}) {
		t.Fatalf("the write was stamped %v — the actor is ActorFor's answer (the peer), never a claim", f.lastActor)
	}
	if f.grantIn.Conditions.RequireBudget != true || f.grantIn.Conditions.RequireGates ||
		len(f.grantIn.Conditions.Labels) != 0 || f.grantIn.Conditions.MaxPerDay != 0 {
		t.Errorf("absent conditions became %v, want the whole-object default {RequireBudget:true} (AL4)", f.grantIn.Conditions)
	}

	// The identity door: each of the five keys is refused on EVERY method,
	// read or write — a smuggled actor on a read would still forge the
	// surface's audit identity.
	for _, m := range autonomyMethodNames() {
		for _, key := range autonomyIdentityKeys {
			raw := json.RawMessage(fmt.Sprintf(`{"%s":"u1"}`, key))
			_, err := tbl.Dispatch(context.Background(), "c1", m, raw)
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Errorf("%s with params key %q was accepted (%v) — the actor is the kernel's, not a claim (AL11)", m, key, err)
				continue
			}
			if !strings.Contains(bad.Error(), key) {
				t.Errorf("%s's identity refusal does not name the key it refused: %q", m, bad.Error())
			}
		}
	}

	// And the complement (T3): an unknown field that is NOT an identity claim
	// is ignored — the door is exactly five keys, not a strict decode.
	f.grantCalls = 0
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodAutonomyGrant,
		json.RawMessage(`{"rootId":"root-1","subject":"forge.openPR","someFutureField":{"x":1}}`)); err != nil {
		t.Errorf("an unknown non-identity param was rejected: %v", err)
	}
	if f.grantCalls != 1 {
		t.Errorf("a future field stopped the write before the seam (grantCalls=%d) — a field is not an actor", f.grantCalls)
	}
}

// TestGrantRefusalsAreTypedWithTheirSentences — T6 behaviours 2–3 plus the
// actor family: every refusal is a typed -31833 with the validator's sentence
// verbatim in `what` — a re-worded refusal is a second truth. The scripted
// sentences are the pure validator's and the daemon bridge's own words.
func TestGrantRefusalsAreTypedWithTheirSentences(t *testing.T) {
	far := "2027-03-01T00:00:00Z"
	cases := []struct {
		name   string
		method MethodName
		params any
		// the refusal the answer must carry, verbatim.
		refusal *AutonomyRefusal
		// the connection's identity (default: a user); a nil actorFor means
		// the table carries no ActorFor at all (the actor-unavailable case).
		actorFor  func(context.Context, *Session) Actor
		noActor   bool
		// beforeSeam marks the actor family: the identity door refuses
		// BEFORE the seam, so the write must never reach it.
		beforeSeam bool
	}{
		{
			name:   "unknown-subject",
			method: MethodAutonomyGrant,
			params: AutonomyGrantParams{RootID: "root-1", Subject: "forge.warp"},
			refusal: &AutonomyRefusal{Code: "unknown-subject",
				Sentence: "forge.warp is not a registry action (the registry is closed)"},
		},
		{
			name:   "local-subject",
			method: MethodAutonomyGrant,
			params: AutonomyGrantParams{RootID: "root-1", Subject: "git.createBranch"},
			refusal: &AutonomyRefusal{Code: "local-subject",
				Sentence: "git.createBranch is local — it runs without a grant"},
		},
		{
			name:   "class-not-grantable",
			method: MethodAutonomyGrant,
			params: AutonomyGrantParams{RootID: "root-1", Subject: "irreversible"},
			refusal: &AutonomyRefusal{Code: "class-not-grantable",
				Sentence: `the only class a grant may name is "outward"; "irreversible" is not grantable — an irreversible action is granted only by its own name`},
		},
		{
			name:   "condition-forced",
			method: MethodAutonomyGrant,
			params: AutonomyGrantParams{RootID: "root-1", Subject: "forge.merge", Conditions: &AutonomyConditions{}},
			refusal: &AutonomyRefusal{Code: "condition-forced",
				Sentence: "forge.merge must carry the merge rule (requireGates); it is forced on and cannot be turned off"},
		},
		{
			name:   "expiry-required",
			method: MethodAutonomyGrant,
			params: AutonomyGrantParams{RootID: "root-1", Subject: "forge.merge", Conditions: &AutonomyConditions{RequireGates: true}},
			refusal: &AutonomyRefusal{Code: "expiry-required",
				Sentence: "forge.merge must carry an expiry — irreversible authority is bounded and renewable"},
		},
		{
			name:   "expiry-too-far",
			method: MethodAutonomyGrant,
			params: AutonomyGrantParams{RootID: "root-1", Subject: "forge.merge", Conditions: &AutonomyConditions{RequireGates: true}, ExpiresAt: &far},
			refusal: &AutonomyRefusal{Code: "expiry-too-far",
				Sentence: "a merge grant may not outlive 90 days from now"},
		},
		{
			name:   "not-a-root",
			method: MethodAutonomyGrant,
			params: AutonomyGrantParams{RootID: "child-1", Subject: "forge.openPR"},
			refusal: &AutonomyRefusal{Code: "not-a-root",
				Sentence: "refused: allow grants sit on a root component; child-1 is not a root — its root is root-1 (AL3)"},
		},
		{
			name:   "deny-unknown-subject",
			method: MethodAutonomyDeny,
			params: AutonomyDenyParams{NodeID: "root-1", Subject: "forge.warp"},
			refusal: &AutonomyRefusal{Code: "unknown-subject",
				Sentence: "forge.warp is not a registry action (the registry is closed)"},
		},
		{
			name:   "revoke-unknown-grant",
			method: MethodAutonomyRevoke,
			params: AutonomyRevokeParams{ID: "g-nope", Reason: "test"},
			refusal: &AutonomyRefusal{Code: "unknown-grant",
				Sentence: "refused: g-nope is not an autonomy grant — it cannot be revoked"},
		},
		{
			name:      "actor-unavailable",
			method:    MethodAutonomyGrant,
			params:    AutonomyGrantParams{RootID: "root-1", Subject: "forge.openPR"},
			noActor:   true,
			beforeSeam: true,
			refusal: &AutonomyRefusal{Code: "actor-unavailable",
				Sentence: "refused: an autonomy write needs a person on the connection, and this one carries no identity"},
		},
		{
			name:      "actor-anonymous",
			method:    MethodAutonomyGrant,
			params:    AutonomyGrantParams{RootID: "root-1", Subject: "forge.openPR"},
			actorFor:  func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: ""} },
			beforeSeam: true,
			refusal: &AutonomyRefusal{Code: "actor-anonymous",
				Sentence: "refused: an autonomy write needs a named actor — an anonymous author cannot be audited (AU1)"},
		},
		{
			name:      "not-a-person",
			method:    MethodAutonomyGrant,
			params:    AutonomyGrantParams{RootID: "root-1", Subject: "forge.openPR"},
			actorFor:  func(context.Context, *Session) Actor { return Actor{Kind: "agent", ID: "agent-1"} },
			beforeSeam: true,
			refusal: &AutonomyRefusal{Code: "not-a-person",
				Sentence: `refused: only a user may write autonomy — caller kind "agent" is not a person (AL11 lock 2)`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &autonomyFake{}
			switch c.method {
			case MethodAutonomyGrant:
				f.grantRef = c.refusal
			case MethodAutonomyDeny:
				f.denyRef = c.refusal
			case MethodAutonomyRevoke:
				f.revokeRef = c.refusal
			}
			tbl := newTestTable(t)
			if !c.noActor {
				if c.actorFor != nil {
					tbl.ActorFor = c.actorFor
				} else {
					tbl.ActorFor = userActorFor("peer-1")
				}
			}
			tbl.Autonomy = f
			tbl.RegisterAutonomy()
			initialized(t, tbl, "c1", PROTOCOL_VERSION)

			_, err := tbl.Dispatch(context.Background(), "c1", c.method, mustJSON(t, c.params))
			if err == nil {
				t.Fatal("a refusal arrived as a success")
			}
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("the refusal is not a typed Aeon error: %T: %v", err, err)
			}
			if e.Code != ErrAutonomyRefused.Code {
				t.Errorf("code = %d, want %d (the autonomy refusal family)", e.Code, ErrAutonomyRefused.Code)
			}
			if e.Data.What != c.refusal.Sentence {
				t.Errorf("the sentence was re-worded on the way out:\n got  %q\n want %q", e.Data.What, c.refusal.Sentence)
			}
			if e.Data.Means != ErrAutonomyRefused.Data.Means || e.Data.Todo != ErrAutonomyRefused.Data.Todo {
				t.Error("the refusal clobbered the typed error's generic means/todo — the sentence rides `what`, the halves stay")
			}
			if c.beforeSeam && f.grantCalls != 0 {
				t.Errorf("an actor-refused write reached the seam (grantCalls=%d) — the identity door is before the judgement", f.grantCalls)
			}
		})
	}
}

// TestAutonomyWritesPublishTheirEventsAfterCommit — R5: the event is the
// signal of a committed write. A changed write publishes exactly one event,
// filed under the grant's component root; the payload is ids and enums only.
func TestAutonomyWritesPublishTheirEventsAfterCommit(t *testing.T) {
	cases := []struct {
		method MethodName
		params any
		kind   string
		root   string
	}{
		{MethodAutonomyGrant, AutonomyGrantParams{RootID: "root-1", Subject: "forge.openPR"}, KindAutonomyGranted, "/ws/one"},
		{MethodAutonomyDeny, AutonomyDenyParams{NodeID: "root-1", Subject: "outward"}, KindAutonomyDenied, "/ws/one"},
		{MethodAutonomyRevoke, AutonomyRevokeParams{ID: "g-1", Reason: "the test is over"}, KindAutonomyRevoked, "/ws/one"},
	}
	for _, c := range cases {
		t.Run(string(c.method), func(t *testing.T) {
			f := &autonomyFake{}
			outcome := AutonomyWriteOutcome{
				View:          &GrantView{ID: "g-1", RootID: "root-1"},
				Confirmation:  "written",
				Changed:       true,
				ComponentRoot: c.root,
			}
			switch c.method {
			case MethodAutonomyGrant:
				f.grantOut = outcome
			case MethodAutonomyDeny:
				f.denyOut = outcome
			case MethodAutonomyRevoke:
				f.revokeOut = outcome
			}
			var events []struct {
				kind    string
				payload any
			}
			tbl := newTestTable(t)
			tbl.ActorFor = userActorFor("peer-1")
			tbl.Autonomy = f
			tbl.RegisterAutonomy()
			tbl.OnEvent = func(kind string, payload any) {
				events = append(events, struct {
					kind    string
					payload any
				}{kind, payload})
			}
			initialized(t, tbl, "c1", PROTOCOL_VERSION)

			if _, err := tbl.Dispatch(context.Background(), "c1", c.method, mustJSON(t, c.params)); err != nil {
				t.Fatalf("%s: %v", c.method, err)
			}
			if len(events) != 1 {
				t.Fatalf("a committed write published %d events, want exactly one (at-most-once, as task.changed)", len(events))
			}
			if events[0].kind != c.kind {
				t.Errorf("the event kind is %q, want %s", events[0].kind, c.kind)
			}
			ev, ok := events[0].payload.(AutonomyChangeEvent)
			if !ok {
				t.Fatalf("the event payload is %T, want AutonomyChangeEvent (ids and enums only — the rows are the record)", events[0].payload)
			}
			if ev.GrantID != "g-1" || ev.RootID != "root-1" {
				t.Errorf("the event payload names %s/%s, want the committed grant's ids", ev.GrantID, ev.RootID)
			}
			if ev.EventComponentRoot() != c.root {
				t.Errorf("the event was filed under root %q, want the grant's component root %q — that is the subscriber's Filter", ev.EventComponentRoot(), c.root)
			}
		})
	}
}

// TestADryRunWritesNothingAndPublishesNothing — the complement: a dry run
// validates and answers the confirmation, but nothing was written, so there
// is no row, and an event for a write that is not there is a second truth.
func TestADryRunWritesNothingAndPublishesNothing(t *testing.T) {
	f := &autonomyFake{}
	f.grantOut = AutonomyWriteOutcome{View: nil, Confirmation: "dry run: nothing was written", Changed: false, ComponentRoot: "/ws/one"}
	var events []string
	tbl := newTestTable(t)
	tbl.ActorFor = userActorFor("peer-1")
	tbl.Autonomy = f
	tbl.RegisterAutonomy()
	tbl.OnEvent = func(kind string, payload any) { events = append(events, kind) }
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	res, err := tbl.Dispatch(context.Background(), "c1", MethodAutonomyGrant,
		mustJSON(t, AutonomyGrantParams{RootID: "root-1", Subject: "forge.openPR", DryRun: true}))
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	out := res.(AutonomyGrantResult)
	if out.Grant != nil {
		t.Error("a dry run returned a grant row — nothing was written, so there is no row")
	}
	if out.Confirmation == "" {
		t.Error("a dry run must answer the daemon's confirmation — that is its only product")
	}
	if !f.grantIn.DryRun {
		t.Error("the dryRun flag did not reach the seam — it would have written")
	}
	if len(events) != 0 {
		t.Errorf("a dry run published %v — an event for a write that is not there is a second truth", events)
	}
}

// TestAutonomyMethodsRefuseBadParams — the closed sets are checked at the
// door, because a typo would otherwise answer a plausible wrong answer: a
// misspelled state reads as an empty list, a negative limit as a promise the
// store will not keep.
func TestAutonomyMethodsRefuseBadParams(t *testing.T) {
	cases := []struct {
		name   string
		method MethodName
		raw    string
		want   string
	}{
		{"list-state-typo", MethodAutonomyList, `{"state":"sometimes"}`, `state must be "allow" or "deny"`},
		{"uses-negative-limit", MethodAutonomyUses, `{"limit":-1}`, "limit must be non-negative"},
		{"changes-negative-limit", MethodAutonomyChanges, `{"limit":-1}`, "limit must be non-negative"},
		{"effective-origin-typo", MethodAutonomyEffective, `{"rootId":"root-1","origin":"cron"}`, "origin must be one of"},
		{"effective-no-root", MethodAutonomyEffective, `{}`, "rootId is required"},
		{"grant-no-root", MethodAutonomyGrant, `{"subject":"forge.openPR"}`, "rootId is required"},
		{"grant-no-subject", MethodAutonomyGrant, `{"rootId":"root-1"}`, "subject is required"},
		{"grant-bad-expires", MethodAutonomyGrant, `{"rootId":"root-1","subject":"forge.openPR","expiresAt":"next tuesday"}`, "expiresAt must be RFC3339"},
		{"deny-no-node", MethodAutonomyDeny, `{"subject":"outward"}`, "nodeId is required"},
		{"revoke-no-id", MethodAutonomyRevoke, `{"reason":"test"}`, "id is required"},
		{"revoke-no-reason", MethodAutonomyRevoke, `{"id":"g-1"}`, "reason is required"},
		{"show-no-id", MethodAutonomyShow, `{}`, "id is required"},
		{"pinned-no-loop", MethodAutonomyPinned, `{}`, "loopId is required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tbl := autonomyTable(t, &autonomyFake{})
			_, err := tbl.Dispatch(context.Background(), "c1", c.method, json.RawMessage(c.raw))
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Fatalf("%s: the bad params were accepted (%v) — a plausible wrong answer", c.name, err)
			}
			if !strings.Contains(bad.Error(), c.want) {
				t.Errorf("the refusal does not name the rule it broke: %q", bad.Error())
			}
		})
	}
}

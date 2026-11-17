package daemon

// autonomy_test.go — S56 T6: the daemon's autonomy service over a real store.
// The protocol's fake-seam tests (internal/protocol/autonomy_test.go) cover
// the handler — the peer stamp, the typed refusal, the event after the
// commit; this file covers the bridge: the daemon is the ONE layer that may
// know both the store's rows and the pure internal/autonomy core, so the
// behaviours that need rows are proven here, each a test:
//
//   - effective ≡ the pin a start writes (AL14, one resolver)
//   - pinned shows the grant's LIVE status, the same sentences the decision
//     site speaks (AL6/AL17)
//   - uses pairs a begun with its outcome, and an unpaired begun says
//     "unknown" rather than hiding it (AL10)
//   - revoke is a no-op when already revoked, and names the affected loops
//     (AL5/AL6, OD8)
//   - the describe sentence is one formatter, and no client re-words it
//     (AL17)
//
// The T4 test environment (autonomyRootEnv, seedAutonomyGrant, fireSchedule)
// is reused: a real workspace root, a real start, a real runner settling the
// loop before the store cleanup can close the handle.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// autonomyServiceEnv is autonomyRootEnv plus the daemon's service over the
// same store — the tests speak the seam the socket speaks.
func autonomyServiceEnv(t *testing.T) (*store.Store, *Daemon, protocol.AutonomyService, string, store.Component) {
	t.Helper()
	s, d, ws, root := autonomyRootEnv(t)
	svc := NewAutonomyService(s)
	if svc == nil {
		t.Fatal("a wired store returned a nil autonomy service — V13 is for storeless daemons only")
	}
	return s, d, svc, ws, root
}

// --- AL14: the effective view is the pin --------------------------------------

// TestEffectiveEqualsWhatAStartPins — T6 behaviour 6: the SAME pure Plan, the
// SAME inputs, and the legibility view and the enforcement must agree
// action-for-action, grant-for-grant. A drift between the two is a promise
// the panel makes that the loop will not keep.
func TestEffectiveEqualsWhatAStartPins(t *testing.T) {
	s, d, svc, ws, root := autonomyServiceEnv(t)
	g := seedAutonomyGrant(t, s, root.ID, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow", Conditions: `{}`,
	})
	view, ref, err := svc.Effective(context.Background(), root.ID, "schedule", nil)
	if err != nil || ref != nil {
		t.Fatalf("effective: %v %v", ref, err)
	}
	if view.RootID != root.ID || view.Origin != "schedule" {
		t.Errorf("the view names %s/%s, want the queried root and origin", view.RootID, view.Origin)
	}
	if !strings.Contains(view.Statement, "forge.openPR") || !strings.Contains(view.Statement, g.ID) {
		t.Errorf("the statement does not name the covered action and its grant: %q", view.Statement)
	}
	// The enforcement half: actually start, and compare the pin the loop was
	// born with.
	loopID := fireSchedule(t, s, d, "effective-eq", ws)
	pin, ok, err := s.ReadAutonomyPin(loopID)
	if err != nil || !ok {
		t.Fatalf("the start wrote no pin row (ok=%v, err=%v) — a no-condition grant must pin", ok, err)
	}
	var entries []pinEntry
	if err := json.Unmarshal([]byte(pin.Allowed), &entries); err != nil {
		t.Fatalf("pin allowed JSON: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("the pin carries no covered actions — nothing could run")
	}
	pinned := map[string]string{} // action → the grant behind it
	for _, e := range entries {
		pinned[e.Subject] = e.Grant
	}
	// The non-local actions the effective view says would run, started now.
	effective := map[string]string{}
	for _, ea := range view.Actions {
		if ea.Verdict == string(action.VerdictRun) && ea.Class != string(action.EffectLocal) {
			effective[ea.Action] = ea.GrantID
		}
	}
	for actionName, grantID := range pinned {
		got, ok := effective[actionName]
		if !ok || got != grantID {
			t.Errorf("the pin covers %s under grant %s, but the effective view says otherwise (%q) — AL14 drifted", actionName, grantID, effective[actionName])
		}
	}
	for actionName, grantID := range effective {
		got, ok := pinned[actionName]
		if !ok || got != grantID {
			t.Errorf("the effective view says %s would run under grant %s, but the start did not pin it — AL14 drifted", actionName, grantID)
		}
	}
}

// --- AL6/AL8: the pinned view shows live status --------------------------------

// TestPinnedShowsLiveStatus — T6 behaviour 7: a loop shows what it was pinned
// with and whether that authority is still live. The pin is immutable; the
// grant's live status is not. A revoked grant reads "revoked" with the same
// sentence the decision site speaks; a no-pin loop reads {pinned:false,
// statement}; an unknown loop is a refusal, not a "no pin".
func TestPinnedShowsLiveStatus(t *testing.T) {
	s, d, svc, ws, root := autonomyServiceEnv(t)
	g := seedAutonomyGrant(t, s, root.ID, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow", Conditions: `{}`,
	})
	loopID := fireSchedule(t, s, d, "pinned-live", ws)

	// While the grant is in force: pinned, live, covered.
	view, ref, err := svc.Pinned(context.Background(), loopID)
	if err != nil || ref != nil {
		t.Fatalf("pinned: %v %v", ref, err)
	}
	if !view.Pinned {
		t.Fatal("a grant-covered loop shows pinned=false")
	}
	if view.Rev == 0 {
		t.Error("the pin's rev is 0 — the root's AutonomyRev rides the pin (AL8)")
	}
	if len(view.Grants) != 1 || view.Grants[0].GrantID != g.ID {
		t.Fatalf("the pin's grants = %+v, want the one seeded grant", view.Grants)
	}
	if view.Grants[0].Live.State != "active" {
		t.Errorf("a live grant reads %q, want active", view.Grants[0].Live.State)
	}
	if run := findPinnedAction(t, view.Actions, "forge.openPR"); run.Verdict != string(action.VerdictRun) {
		t.Errorf("the covered action reads %q, want run (the grant is live, the conditions are empty)", run.Verdict)
	}

	// Revoke through the same door a person uses.
	out, ref, err := svc.Revoke(context.Background(), protocol.Actor{Kind: "user", ID: "u1"},
		protocol.AutonomyRevokeRequest{ID: g.ID, Reason: "the test is over"})
	if err != nil || ref != nil {
		t.Fatalf("revoke: %v %v", ref, err)
	}
	if !out.Changed {
		t.Fatal("a first revoke reports Changed=false")
	}

	// The pin is still the same snapshot; the authority is gone. The covered
	// action now denies with the revoked clause — the loop is told what the
	// panel shows (AL14/AL17: one truth, panel and loop).
	view2, ref, err := svc.Pinned(context.Background(), loopID)
	if err != nil || ref != nil {
		t.Fatalf("pinned after revoke: %v %v", ref, err)
	}
	if len(view2.Grants) != 1 {
		t.Fatalf("the pin lost its grant on revoke — the pin is immutable, the status is not")
	}
	if view2.Grants[0].Live.State != "revoked" {
		t.Fatalf("a revoked grant reads %q, want revoked", view2.Grants[0].Live.State)
	}
	denied := findPinnedAction(t, view2.Actions, "forge.openPR")
	if denied.Verdict != string(action.VerdictDeny) {
		t.Errorf("the revoked covered action reads %q, want deny", denied.Verdict)
	}
	if !strings.Contains(denied.Because, g.ID) || !strings.Contains(denied.Because, "was revoked at") {
		t.Errorf("the denial does not name the grant and the revocation: %q", denied.Because)
	}

	// A loop started with no grant applies to nothing new: the grant is
	// revoked, so a new start pins nothing and shows the honest no-pin answer.
	loopNoPin := fireSchedule(t, s, d, "no-pin", ws)
	view3, ref, err := svc.Pinned(context.Background(), loopNoPin)
	if err != nil || ref != nil {
		t.Fatalf("pinned no-pin: %v %v", ref, err)
	}
	if view3.Pinned {
		t.Error("a no-grant loop shows pinned=true")
	}
	if view3.Statement == "" {
		t.Error("the no-pin view has no statement — the honest 'started with no autonomy' is the answer")
	}

	// An unknown loop is a refusal, not a "no pin": a fabricated id reading as
	// 'started with no autonomy' would be a confident wrong answer.
	_, ref, err = svc.Pinned(context.Background(), "loop-nope")
	if err != nil {
		t.Fatalf("pinned unknown loop: %v", err)
	}
	if ref == nil || ref.Code != "unknown-loop" {
		t.Errorf("an unknown loop answers %v, want the unknown-loop refusal", ref)
	}
}

func findPinnedAction(t *testing.T, actions []protocol.PinnedAction, name string) protocol.PinnedAction {
	t.Helper()
	for _, a := range actions {
		if a.Action == name {
			return a
		}
	}
	t.Fatalf("the pinned view has no entry for %s — a registry action is absent from the plan", name)
	var zero protocol.PinnedAction
	return zero
}

// --- AL10: the use log pairs begun with its outcome ----------------------------

// TestUnpairedBegunIsUnknown — T6 behaviour 8: a begun row carries the
// outcome of the completed/failed row that closed it; a crash leaves a begun
// with no outcome, and the view says "unknown — the process stopped before
// the result was recorded" rather than hiding it. A completed/failed row is
// an outcome, not a use — its outcome is null.
func TestUnpairedBegunIsUnknown(t *testing.T) {
	s, d, svc, ws, root := autonomyServiceEnv(t)
	g := seedAutonomyGrant(t, s, root.ID, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow", Conditions: `{}`,
	})
	loopID := fireSchedule(t, s, d, "uses-pair", ws)
	rev, err := s.AutonomyRev(root.ID)
	if err != nil {
		t.Fatalf("rev read: %v", err)
	}
	begun, err := s.RecordAutonomyUse(loopID, g.ID, rev, "forge.openPR", "begun", 0, "")
	if err != nil {
		t.Fatalf("record begun: %v", err)
	}
	completed, err := s.RecordAutonomyUse(loopID, g.ID, rev, "forge.openPR", "completed", begun, "evidence-1")
	if err != nil {
		t.Fatalf("record completed: %v", err)
	}
	unpaired, err := s.RecordAutonomyUse(loopID, g.ID, rev, "forge.openPR", "begun", 0, "")
	if err != nil {
		t.Fatalf("record unpaired begun: %v", err)
	}

	uses, err := svc.Uses(context.Background(), "", "", loopID, 0)
	if err != nil {
		t.Fatalf("uses: %v", err)
	}
	byID := map[int64]protocol.UseView{}
	for _, u := range uses {
		byID[u.ID] = u
	}
	if len(uses) != 3 {
		t.Fatalf("uses returned %d rows, want the three written", len(uses))
	}
	paired := byID[begun]
	if paired.Outcome == nil || *paired.Outcome != "completed" {
		t.Errorf("the paired begun's outcome = %v, want completed", paired.Outcome)
	}
	if paired.BegunID != nil {
		t.Errorf("a begun row carries begun_id %v — a begun has no begun", paired.BegunID)
	}
	done := byID[completed]
	if done.Phase != "completed" {
		t.Fatalf("the completed row is phase %q", done.Phase)
	}
	if done.Outcome != nil {
		t.Errorf("a completed/failed row is an outcome, not a use — its outcome must be null, got %q", *done.Outcome)
	}
	if done.BegunID == nil || *done.BegunID != begun {
		t.Errorf("the completed row does not point at its begun (got %v, want %d)", done.BegunID, begun)
	}
	lost := byID[unpaired]
	if lost.Outcome == nil || *lost.Outcome != protocol.UseOutcomeUnknown {
		t.Errorf("an unpaired begun's outcome = %v, want the fixed unknown sentence — the effect may have happened", lost.Outcome)
	}

	// The caller's limit is the NEWEST N, ascending: the unpaired begun is
	// the newest row, so it is the limited read's last entry.
	limited, err := svc.Uses(context.Background(), "", "", loopID, 2)
	if err != nil {
		t.Fatalf("uses limited: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("limit 2 returned %d rows, want the newest 2", len(limited))
	}
	if limited[len(limited)-1].ID != unpaired {
		t.Errorf("the limited read's newest row is %d, want the newest written (%d)", limited[len(limited)-1].ID, unpaired)
	}
}

// --- AL5/AL6: revoke is idempotent ---------------------------------------------

// TestRevokeIsIdempotent — T6 behaviour 4: the second revoke is a no-op
// success — the same view, no new change row, an empty affected list. A
// no-op writes nothing.
func TestRevokeIsIdempotent(t *testing.T) {
	s, _, svc, _, root := autonomyServiceEnv(t)
	g := seedAutonomyGrant(t, s, root.ID, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow", Conditions: `{}`,
	})
	changes, err := s.AutonomyChanges(store.AutonomyChangeFilter{GrantID: g.ID})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(changes) != 1 || changes[0].ChangeKind != "grant" {
		t.Fatalf("before any revoke the audit holds %d rows — want exactly the grant row", len(changes))
	}
	first, ref, err := svc.Revoke(context.Background(), protocol.Actor{Kind: "user", ID: "u1"},
		protocol.AutonomyRevokeRequest{ID: g.ID, Reason: "the test is over"})
	if err != nil || ref != nil {
		t.Fatalf("first revoke: %v %v", ref, err)
	}
	if !first.Changed || first.View == nil {
		t.Fatal("a first revoke reports Changed=false or no view")
	}
	if first.Affected == nil {
		t.Error("the revoke's affected loops are nil — empty is the answer, nil is not")
	}
	if first.View.Revoked == nil || first.View.Revoked.Reason != "the test is over" {
		t.Fatalf("the revoked trio is %v, want the reason on the view", first.View.Revoked)
	}
	if first.View.Active {
		t.Error("a revoked grant reads active=true")
	}
	second, ref, err := svc.Revoke(context.Background(), protocol.Actor{Kind: "user", ID: "u1"},
		protocol.AutonomyRevokeRequest{ID: g.ID, Reason: "again"})
	if err != nil || ref != nil {
		t.Fatalf("second revoke: %v %v", ref, err)
	}
	if second.Changed {
		t.Error("a second revoke reports Changed=true — already revoked is a no-op success")
	}
	if second.Affected == nil || len(second.Affected) != 0 {
		t.Errorf("the no-op revoke's affected loops = %v, want non-nil empty (nothing runs under a dead grant)", second.Affected)
	}
	again, err := s.AutonomyChanges(store.AutonomyChangeFilter{GrantID: g.ID})
	if err != nil {
		t.Fatalf("changes after: %v", err)
	}
	if len(again) != 2 {
		t.Fatalf("the no-op revoke wrote a change row — the audit holds %d rows, want the grant and the one revoke", len(again))
	}
	if again[1].ChangeKind != "revoke" || again[1].ActorKind != "user" || again[1].ActorID != "u1" {
		t.Errorf("the revoke row is %s/%s/%s, want the user's one revoke", again[1].ChangeKind, again[1].ActorKind, again[1].ActorID)
	}
	// Same view, same trio: a no-op reads the row it already set.
	if *second.View.Revoked != *first.View.Revoked {
		t.Errorf("the second revoke's view carries a different revoked trio: %+v vs %+v", *second.View.Revoked, *first.View.Revoked)
	}
}

// TestRevokeListsAffectedLoops — T6 behaviour 4 (the list half): the revoke
// returns the RUNNING loops pinned to the grant, so the caller can stop them
// with S55's loop.stop (OD8: AL pulls the authority, it never stops loops).
// A terminal loop is not affected — its work is done.
func TestRevokeListsAffectedLoops(t *testing.T) {
	s, _, svc, ws, root := autonomyServiceEnv(t)
	g := seedAutonomyGrant(t, s, root.ID, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow", Conditions: `{}`,
	})
	// The join keys on the pin's allowed JSON ({"grant": id, "subject": …}),
	// so the pins are written through the store's one pin writer — the same
	// call the start path makes — with the grant named in the snapshot.
	allowed := `[{"grant":"` + g.ID + `","subject":"forge.openPR"}]`
	live, err := s.CreatePinnedLoop(store.NewLoop{
		Slug: "affected-live", ComponentRoot: ws, Status: "active",
		AutonomyPin: &store.AutonomyPin{RootID: root.ID, Rev: 1, Origin: "schedule", Labels: `[]`, Allowed: allowed},
	}, "two-step", 1, `{"steps":[]}`, `{"state":"rest"}`)
	if err != nil {
		t.Fatalf("create the running pinned loop: %v", err)
	}
	// A terminal loop pinned to the same grant: the revocation is not an
	// interruption of finished work.
	_, err = s.CreatePinnedLoop(store.NewLoop{
		Slug: "affected-done", ComponentRoot: ws, Status: "done",
		AutonomyPin: &store.AutonomyPin{RootID: root.ID, Rev: 1, Origin: "schedule", Labels: `[]`, Allowed: allowed},
	}, "two-step", 1, `{"steps":[]}`, `{"state":"rest"}`)
	if err != nil {
		t.Fatalf("create the done pinned loop: %v", err)
	}

	out, ref, err := svc.Revoke(context.Background(), protocol.Actor{Kind: "user", ID: "u1"},
		protocol.AutonomyRevokeRequest{ID: g.ID, Reason: "stopping the pins"})
	if err != nil || ref != nil {
		t.Fatalf("revoke: %v %v", ref, err)
	}
	if len(out.Affected) != 1 || out.Affected[0] != live.ID {
		t.Fatalf("the revoke's affected loops = %v, want exactly the running pinned loop %s (the done loop is not affected)", out.Affected, live.ID)
	}
	again, ref, err := svc.Revoke(context.Background(), protocol.Actor{Kind: "user", ID: "u1"},
		protocol.AutonomyRevokeRequest{ID: g.ID, Reason: "again"})
	if err != nil || ref != nil {
		t.Fatalf("second revoke: %v %v", ref, err)
	}
	if again.Affected == nil || len(again.Affected) != 0 {
		t.Errorf("the no-op revoke lists affected loops %v — already revoked affects nothing", again.Affected)
	}
}

// --- AL17: the describe sentence is one formatter --------------------------------

// TestDescribeIsOneFormatter — T6's table row: the describe sentence is the
// daemon's ONE fixed template, byte-identical on every read path, and no
// client file formats a grant sentence — a second formatter is a second
// truth. The grep is the lock: the template's distinctive phrase may appear
// in the daemon's autonomy.go and nowhere under ui/ or cmd/.
func TestDescribeIsOneFormatter(t *testing.T) {
	s, _, svc, _, root := autonomyServiceEnv(t)
	// A child needs its OWN workspace (a component root is registered once);
	// the veto it carries still rides the root's list.
	child, err := s.CreateComponent(store.NewComponent{Name: "the child", Slug: "the-child", Root: t.TempDir(), ParentID: root.ID})
	if err != nil || !child.OK() {
		t.Fatalf("create the child component: %v %s", err, child.Reason)
	}
	expires := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	gAllow := seedAutonomyGrant(t, s, root.ID, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow",
		Conditions: `{"requireBudget":true}`, ExpiresAt: &expires,
	})
	gDeny := seedAutonomyGrant(t, s, child.Component.ID, store.NewAutonomyGrant{
		SubjectKind: "class", Subject: "outward", State: "deny",
	})
	// The list read and the show read must render the SAME sentence for the
	// same row — two readers of one formatter. The child's veto rides the
	// root's list (a veto sits on a root or a child, OD4).
	listed, err := svc.List(context.Background(), root.ID, "", true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byID := map[string]protocol.GrantView{}
	for _, v := range listed {
		byID[v.ID] = v
	}
	for _, id := range []string{gAllow.ID, gDeny.ID} {
		listedView, ok := byID[id]
		if !ok {
			t.Fatalf("list(root) does not hold grant %s — the child's veto rides the root's list", id)
		}
		shown, _, _, _, ref, err := svc.Show(context.Background(), id)
		if err != nil || ref != nil {
			t.Fatalf("show %s: %v %v", id, ref, err)
		}
		if shown.Describe != listedView.Describe {
			t.Errorf("show and list disagree on the describe sentence for %s:\n list  %q\n show  %q", id, listedView.Describe, shown.Describe)
		}
	}
	// The template, rendered: the distinctive phrase is the daemon's.
	phrase := "for unattended loops on"
	if !strings.Contains(byID[gAllow.ID].Describe, phrase) {
		t.Fatalf("the describe sentence lost the template phrase: %q", byID[gAllow.ID].Describe)
	}
	src, err := os.ReadFile("autonomy.go")
	if err != nil {
		t.Fatalf("read daemon/autonomy.go: %v", err)
	}
	if !strings.Contains(string(src), phrase) {
		t.Fatal("the daemon's describe formatter no longer carries the template phrase")
	}
	// …and NOWHERE on the client surface. A client that formats a grant
	// sentence re-words it.
	for _, dir := range []string{"../../ui", "../cmd"} {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if info.Name() == "node_modules" || info.Name() == "dist" {
				return nil
			}
			if !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") && !strings.HasSuffix(path, ".go") {
				return nil
			}
			b, rerr := os.ReadFile(path)
			if rerr == nil && strings.Contains(string(b), phrase) {
				t.Errorf("client file %s formats a grant sentence — a second formatter is a second truth (AL17)", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

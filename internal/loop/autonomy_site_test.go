package loop

// autonomy_site_test.go — S56 T5's decision site (AL4, AL6, AL9, AL10). The
// tests drive the loop until it reaches an action and assert what the LIVE
// read plus the pure verdict decide: the forge fake is the witness (a call
// seen is a run; zero calls is a denial that actually held), and the use log
// is the receipt (begun before the effect, completed/failed after). The
// store setup (component + grant + pin) is the same shape the daemon's start
// path builds through resolveAutonomy; these tests seed it directly so the
// decision site is exercised in isolation.
//
// A grant with no conditions (condJSON "{}") carries no action-time
// conditions, so no seam is consulted and a nil-seam daemon wiring still runs
// it — the cross-slice conditions are the grant's own, not the loop's.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/store"
)

// --- store setup ---------------------------------------------------------
//
// The decision site reads AutonomyLive, which resolves the loop's
// component_root to a component, reads the pin (loop_autonomy_pin) and the
// covered grant's live status, and checks the veto. So a test seeds: a
// component (so ComponentForRoot resolves the root), a grant (the ceiling),
// and a pin on the loop (the covered snapshot).

// seedComponentAndGrant creates a component root and one allow grant on it
// covering actionName, returning the root path (the loop's component_root),
// the root id and the grant id. condJSON is the grant's canonical conditions
// ("{}" for none); expiresAt, when set, is the grant's expiry (a past time
// drives the expiry test).
func seedComponentAndGrant(t *testing.T, s *store.Store, actionName, condJSON string, expiresAt *time.Time) (rootPath, rootID, grantID string) {
	t.Helper()
	return seedComponentAndGrantAt(t, s, t.TempDir(), actionName, condJSON, expiresAt)
}

// seedComponentAndGrantAt is seedComponentAndGrant on a GIVEN root path — the
// tests whose action must actually RUN (a granted openPR) need a real git
// repository with origin as the root, not a bare TempDir.
func seedComponentAndGrantAt(t *testing.T, s *store.Store, path, actionName, condJSON string, expiresAt *time.Time) (rootPath, rootID, grantID string) {
	t.Helper()
	rootPath = path
	c, err := s.CreateComponent(store.NewComponent{Name: "repo", Slug: "repo", Root: rootPath})
	if err != nil {
		t.Fatal(err)
	}
	rootID = c.Component.ID
	g, refusal, err := s.CreateAutonomyGrant(store.Actor{Kind: "user", ID: "u-op"}, store.NewAutonomyGrant{
		NodeID: rootID, SubjectKind: "action", Subject: actionName, State: "allow",
		Conditions: condJSON, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if refusal != nil {
		t.Fatalf("grant refused: %v", refusal)
	}
	return rootPath, rootID, g.ID
}

// pushTaskBranch creates the task branch in a gitTestRepo and pushes it to
// origin — the state commitAndPush leaves behind, so forge.openPR's
// head-exists probe passes and the PR actually opens (the grant test needs
// the fake to SEE OpenPR, and OpenPR refuses a head nobody has pushed).
func pushTaskBranch(t *testing.T, dir, branch string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=loop", "GIT_AUTHOR_EMAIL=l@l", "GIT_COMMITTER_NAME=loop", "GIT_COMMITTER_EMAIL=l@l")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-b", branch)
	run("push", "-u", "origin", branch)
}

// addVeto writes an active deny (veto) on the root covering actionName,
// returning the veto grant id (AL3/AL7: the floor, checked with or without a
// pin and even when the flag was typed).
func addVeto(t *testing.T, s *store.Store, rootID, actionName string) string {
	t.Helper()
	g, refusal, err := s.CreateAutonomyGrant(store.Actor{Kind: "user", ID: "u-op"}, store.NewAutonomyGrant{
		NodeID: rootID, SubjectKind: "action", Subject: actionName, State: "deny",
	})
	if err != nil {
		t.Fatal(err)
	}
	if refusal != nil {
		t.Fatalf("veto refused: %v", refusal)
	}
	return g.ID
}

// startPinned creates a pinned loop on the component root with an autonomy
// pin whose allowed snapshot covers `allowed` (each with the grant), mirroring
// the daemon's start path (resolveAutonomy → CreatePinnedLoop with
// in.AutonomyPin). attended/allowOutward are the pin's declarations and
// bpAction is the single code-action the blueprint drives.
func startPinned(t *testing.T, s *store.Store, slug, rootPath, rootID string, allowed []string, grantID string, attended, allowOutward bool, bpAction string) LoopState {
	t.Helper()
	entries := make([]map[string]string, 0, len(allowed))
	for _, subj := range allowed {
		entries = append(entries, map[string]string{"grant": grantID, "subject": subj})
	}
	allowedJSON, _ := json.Marshal(entries)
	rev, err := s.AutonomyRev(rootID)
	if err != nil {
		t.Fatal(err)
	}
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: slug, ComponentRoot: rootPath, Root: rootPath, Mode: "standard",
		AllowOutward: allowOutward,
		AutonomyPin:  &store.AutonomyPin{RootID: rootID, Rev: rev, Origin: "event", Labels: "[]", Allowed: string(allowedJSON)},
	}, outwardBlueprint(t, bpAction), boolPtr(attended), taskIdentity())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// grantContext is the decision site's grantContextFor, returned as the verdict
// it feeds — a helper so a test reads the LiveState facts and the
// ResolveWithGrant result in one expression.
func grantVerdict(t *testing.T, s *store.Store, st *LoopState, name string, attended, allowOutward bool, seams autonomy.Seams) action.CheckResult {
	t.Helper()
	dep := Deps{Store: s, Autonomy: AutonomySeams{Seams: seams}}
	gctx, _, err := grantContextFor(context.Background(), dep, st, name, attended)
	if err != nil {
		t.Fatalf("grantContextFor: %v", err)
	}
	v, err := action.ResolveWithGrant(name, attended, allowOutward, gctx)
	if err != nil {
		t.Fatalf("ResolveWithGrant(%s): %v", name, err)
	}
	return v
}

// --- end to end, the forge fake as witness (the allowoutward_test.go shape) -

// TestGrantedActionRunsAndIsLogged (behaviour 1): a pinned loop with an allow
// grant reaches the outward action ⇒ the fake SAW the call, the use log has
// begun then completed, the event carries grant+rev, and allow_outward stayed 0
// (the grant lifted the action; it did not flip the legacy flag).
func TestGrantedActionRunsAndIsLogged(t *testing.T) {
	s := openLoopStore(t)
	f := forge.NewFake()
	// A real repository with origin, and the task branch pushed: openPR
	// refuses a head nobody has pushed (the phantom-ref guard, O8), so the
	// granted run needs the commitAndPush end-state to exist.
	rootPath := gitTestRepo(t)
	pushTaskBranch(t, rootPath, "task/granted/4a17")
	_, rootID, grantID := seedComponentAndGrantAt(t, s, rootPath, action.ForgeOpenPR, "{}", nil)
	st := startPinned(t, s, "granted", rootPath, rootID, []string{action.ForgeOpenPR}, grantID, false, false, action.ForgeOpenPR)

	var executed []map[string]any
	dep := Deps{
		Store: s,
		// openPR reuses the check-only branch probe (the commitAndPush end-
		// state); without it the action refuses "no branch probe wired", so
		// the granted run wires the real client on the test repository.
		Actions: func(string) *action.Services {
			return &action.Services{
				Git:   &action.GitActions{Git: realGitClient()},
				Forge: &action.ForgeActions{Forge: f, Git: realGitClient()},
			}
		},
		Event: func(kind string, payload any) {
			if p, ok := payload.(map[string]any); ok && kind == "loop.action.executed" {
				executed = append(executed, p)
			}
		},
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	// 1. The fake saw the outward action run.
	sawOpen := false
	for _, c := range f.Calls() {
		if c.Method == "OpenPR" {
			sawOpen = true
		}
	}
	if !sawOpen {
		saved, _ := Pinned(context.Background(), s, st.ID)
		t.Fatalf("the granted loop did not run its outward action: fake calls %v, status %q reason %q", f.Calls(), saved.Status, saved.LastReason)
	}
	// 2. The use log has begun then completed.
	uses, err := s.AutonomyUses(store.AutonomyUseFilter{GrantID: grantID})
	if err != nil {
		t.Fatal(err)
	}
	if len(uses) != 2 || uses[0].Phase != "begun" || uses[1].Phase != "completed" {
		t.Errorf("want begun then completed, got %d rows: %+v", len(uses), uses)
	}
	// 3. allow_outward is still 0 — the grant lifted the action, not the flag.
	ao, err := s.LoopAllowOutward(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ao {
		t.Error("the grant lifted the action but flipped allow_outward — the legacy flag must stay 0")
	}
	// 4. The event carries grant and rev (additive).
	found := false
	for _, e := range executed {
		if e["action"] == action.ForgeOpenPR && e["grant"] == grantID && e["rev"] != nil {
			found = true
		}
	}
	if !found {
		t.Errorf("the loop.action.executed event did not carry grant and rev: %v", executed)
	}
}

// TestNotCoveredDeniesVerbatim (behaviour 2): the loop is pinned for forge.openPR
// but reaches forge.closeIssue, which is NOT in the snapshot ⇒ blocked with the
// legacy (T1) sentence, and the fake saw zero calls. A pin for one action does
// not widen to its sibling. (S57 T6 moved the sibling off forge.postReview:
// VG9 made it unrunnable from a blueprint, so the door refuses the shape
// before the pin is ever consulted — closeIssue is the grant's sibling the
// pin does not cover, and the legacy sentence is the one the door speaks.)
func TestNotCoveredDeniesVerbatim(t *testing.T) {
	s := openLoopStore(t)
	f := forge.NewFake()
	rootPath, rootID, grantID := seedComponentAndGrant(t, s, action.ForgeCloseIssue, "{}", nil)
	// The pin covers openPR, NOT closeIssue (the action the loop reaches).
	st := startPinned(t, s, "notcovered", rootPath, rootID, []string{action.ForgeOpenPR}, grantID, false, false, action.ForgeCloseIssue)
	dep := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(f) }}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" {
		t.Fatalf("status %q; want blocked — an action not in the pin's snapshot must not run", saved.Status)
	}
	if !strings.Contains(saved.LastReason, "outward") || !strings.Contains(saved.LastReason, "unattended") {
		t.Errorf("the block must carry the legacy unattended-outward sentence, got %q", saved.LastReason)
	}
	for _, c := range f.Calls() {
		if strings.Contains(c.Method, "CloseIssue") {
			t.Fatalf("the fake saw the ungranted action run (%v) — the pin did not widen", c)
		}
	}
}

// TestAttendedLoopsAreUntouched (behaviour 11): with a pin row present
// (contrived — the pin covers the action and the grant is live), an ATTENDED
// loop still asks exactly as before (OD2): the grant never touches an attended
// ask, and the fake saw nothing.
func TestAttendedLoopsAreUntouched(t *testing.T) {
	s := openLoopStore(t)
	f := forge.NewFake()
	rootPath, rootID, grantID := seedComponentAndGrant(t, s, action.ForgeOpenPR, "{}", nil)
	st := startPinned(t, s, "attended", rootPath, rootID, []string{action.ForgeOpenPR}, grantID, true, false, action.ForgeOpenPR)
	dep := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(f) }}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "waiting" {
		t.Fatalf("attended loop status %q; want waiting (an ask) — the grant must not run an attended action", saved.Status)
	}
	for _, c := range f.Calls() {
		if c.Method == "OpenPR" {
			t.Fatalf("the attended loop ran its action (%v) — an attended ask is not a grant", c)
		}
	}
	uses, err := s.AutonomyUses(store.AutonomyUseFilter{GrantID: grantID})
	if err != nil {
		t.Fatal(err)
	}
	if len(uses) != 0 {
		t.Errorf("an attended ask wrote use rows (%d) — it must write nothing", len(uses))
	}
}

// --- live checks, read through the decision site (AL6/AL9) ----------------

// TestRevocationMidRunDenies (behaviour 3): pin under the grant, revoke it,
// advance ⇒ the live read sees the revocation and denies, naming the grant and
// who/when. The pin is immutable (it still names the grant); the LIVE status
// is what pulls the run back.
func TestRevocationMidRunDenies(t *testing.T) {
	s := openLoopStore(t)
	rootPath, rootID, grantID := seedComponentAndGrant(t, s, action.ForgeOpenPR, "{}", nil)
	st := startPinned(t, s, "revoke", rootPath, rootID, []string{action.ForgeOpenPR}, grantID, false, false, action.ForgeOpenPR)
	// Revoke after the pin: the pin still names the grant, the grant is now dead.
	if _, _, refusal, err := s.RevokeAutonomyGrant(store.Actor{Kind: "user", ID: "u-op"}, grantID, "no longer safe"); err != nil {
		t.Fatal(err)
	} else if refusal != nil {
		t.Fatalf("revoke refused: %v", refusal)
	}
	v := grantVerdict(t, s, &st, action.ForgeOpenPR, false, false, autonomy.Seams{})
	if v.Verdict != action.VerdictDeny {
		t.Fatalf("a revoked covering grant must deny, got %v", v.Verdict)
	}
	if !strings.Contains(v.Sentence, grantID) || !strings.Contains(v.Sentence, "revoked") {
		t.Errorf("the denial must name the grant and that it was revoked, got %q", v.Sentence)
	}
}

// TestVetoBeatsPinAndFlag (behaviour 5): a veto on the root denies a
// PINNED-AND-GRANTED action, and denies an action in a person-flagged
// (allowOutward=1, no pin) unattended loop — the floor overrides both the
// ceiling and the legacy flag (AL3/AL7).
func TestVetoBeatsPinAndFlag(t *testing.T) {
	s := openLoopStore(t)
	rootPath, rootID, grantID := seedComponentAndGrant(t, s, action.ForgeOpenPR, "{}", nil)
	st := startPinned(t, s, "veto", rootPath, rootID, []string{action.ForgeOpenPR}, grantID, false, false, action.ForgeOpenPR)
	addVeto(t, s, rootID, action.ForgeOpenPR)
	// Pinned + granted, but vetoed: the veto wins.
	v := grantVerdict(t, s, &st, action.ForgeOpenPR, false, false, autonomy.Seams{})
	if v.Verdict != action.VerdictDeny {
		t.Fatalf("a veto must deny a pinned-and-granted action, got %v", v.Verdict)
	}
	if !strings.Contains(v.Sentence, "veto") {
		t.Errorf("the denial must name the veto, got %q", v.Sentence)
	}
	// And it beats the legacy flag: an allowOutward=1 loop with NO pin, unattended.
	flagged := startPinned(t, s, "veto-flag", rootPath, rootID, nil, "", true, true, action.ForgeOpenPR)
	vf := grantVerdict(t, s, &flagged, action.ForgeOpenPR, false, true, autonomy.Seams{})
	if vf.Verdict != action.VerdictDeny {
		t.Fatalf("a veto must deny even when the person typed allowOutward, got %v", vf.Verdict)
	}
}

// TestExpiryDenies (behaviour 6): a grant whose expiry has passed the store
// clock ⇒ denied naming expiry. Time passing writes no row — only the live
// read of expires_at decides.
func TestExpiryDenies(t *testing.T) {
	s := openLoopStore(t)
	past := time.Now().Add(-time.Hour)
	rootPath, rootID, grantID := seedComponentAndGrant(t, s, action.ForgeOpenPR, "{}", &past)
	st := startPinned(t, s, "expired", rootPath, rootID, []string{action.ForgeOpenPR}, grantID, false, false, action.ForgeOpenPR)
	v := grantVerdict(t, s, &st, action.ForgeOpenPR, false, false, autonomy.Seams{})
	if v.Verdict != action.VerdictDeny {
		t.Fatalf("an expired covering grant must deny, got %v", v.Verdict)
	}
	if !strings.Contains(v.Sentence, "expired") {
		t.Errorf("the denial must name the expiry, got %q", v.Sentence)
	}
	// Time passing wrote no use row.
	uses, err := s.AutonomyUses(store.AutonomyUseFilter{GrantID: grantID})
	if err != nil {
		t.Fatal(err)
	}
	if len(uses) != 0 {
		t.Errorf("an expired grant wrote use rows (%d) — nothing ran", len(uses))
	}
}

// --- the pin is the ceiling: what happened after the pin never reaches it ----

// TestWideningNeverReachesRunningLoop (behaviour 4): the loop is pinned when
// the root carries NO grant (an empty snapshot), then an allow for the action
// appears on the root. Advancing the loop still denies — the pin is an
// immutable snapshot (AL8), and a widening that happens after the pin never
// reaches the running loop. The fake is the witness that it stayed shut.
func TestWideningNeverReachesRunningLoop(t *testing.T) {
	s := openLoopStore(t)
	f := forge.NewFake()
	// A registered root with no grants at pin time.
	rootPath := t.TempDir()
	c, err := s.CreateComponent(store.NewComponent{Name: "repo", Slug: "widen", Root: rootPath})
	if err != nil {
		t.Fatal(err)
	}
	rootID := c.Component.ID
	st := startPinned(t, s, "widen", rootPath, rootID, nil, "", false, false, action.ForgeOpenPR)
	dep := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(f) }}

	// 1. No grant at all: the legacy deny, zero calls.
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "forge.openPR denied — effect class 'outward' resolves to deny (loop is unattended)"
	if saved.Status != "blocked" || saved.LastReason != want {
		t.Fatalf("status %q reason %q; want blocked with the legacy sentence %q", saved.Status, saved.LastReason, want)
	}
	for _, c := range f.Calls() {
		if c.Method == "OpenPR" {
			t.Fatalf("the ungranted action ran (%v)", c)
		}
	}
	// 2. The widening: an allow appears on the root AFTER the pin.
	if _, refusal, err := s.CreateAutonomyGrant(store.Actor{Kind: "user", ID: "u-op"}, store.NewAutonomyGrant{
		NodeID: rootID, SubjectKind: "action", Subject: action.ForgeOpenPR, State: "allow",
	}); err != nil {
		t.Fatal(err)
	} else if refusal != nil {
		t.Fatalf("widening refused: %v", refusal)
	}
	// The decision site still reads the pin's empty snapshot, not the widened root.
	v := grantVerdict(t, s, &saved, action.ForgeOpenPR, false, false, autonomy.Seams{})
	if v.Verdict != action.VerdictDeny {
		t.Fatalf("the widening reached the running loop: verdict %v", v.Verdict)
	}
	if v.Sentence != want {
		t.Errorf("the denial must stay the legacy sentence, got %q", v.Sentence)
	}
	// 3. And advancing the loop again still denies — the widening never reaches it.
	st2, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := st2.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(st.ID, "running", raw); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	again, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != "blocked" || again.LastReason != want {
		t.Errorf("after the widening: status %q reason %q; want blocked with %q", again.Status, again.LastReason, want)
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("the widened grant ran the action: %v", f.Calls())
	}
}

// --- the merge: the Gates seam is the merge rule's one voice -------------------

// mergeOKProbe is the post-merge guard's instrument for the merge tests: the
// tree answers yes (the merge SHA is an ancestor of the feature branch). The
// fake is the witness of the merge; the probe is the guard it cannot reach
// without (D5).
type mergeOKProbe struct{}

func (mergeOKProbe) IsAncestor(context.Context, string, string, string) (bool, error) {
	return true, nil
}

// seedPREvidence records a PR number in the loop's evidence rows — the
// evidence a prior openPR would have recorded (Q7) — so the merge test reaches
// the forge with the number the actions recorded, and the Gates seam is asked
// about THAT pr, never a remembered guess.
func seedPREvidence(t *testing.T, s *store.Store, st LoopState, pr int) {
	t.Helper()
	if st.Evidence == nil {
		st.Evidence = map[string]map[string]string{}
	}
	st.Evidence["act"] = map[string]string{"pr_number": fmt.Sprintf("%d", pr)}
	raw, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(st.ID, st.Status, raw); err != nil {
		t.Fatal(err)
	}
}

// TestMergeGateSeams (behaviour 7): an allow on forge.merge carries the forced
// requireGates condition, and the Gates seam is the merge rule's one voice.
// met ⇒ the fake SAW MergePR (the only path by which an unattended irreversible
// action runs); unmet ⇒ denied with S57's sentence VERBATIM (the seam's words,
// not a paraphrase); seam nil ⇒ denied naming that the merge rule could not be
// asked; a grant on `outward` alone ⇒ merge denied, byte-identical to T1
// (the class expands to outward actions only — the merge is irreversible, and
// an irreversible action is granted only by its own name, AL2).
func TestMergeGateSeams(t *testing.T) {
	future := time.Now().Add(30 * 24 * time.Hour) // within the 90-day bound

	t.Run("met", func(t *testing.T) {
		s := openLoopStore(t)
		f := forge.NewFake()
		rootPath, rootID, grantID := seedComponentAndGrant(t, s, action.ForgeMerge, `{"requireGates":true}`, &future)
		st := startPinned(t, s, "merge", rootPath, rootID, []string{action.ForgeMerge}, grantID, false, false, action.ForgeMerge)
		// The forge's copy of the PR (base = the feature branch, so the
		// phantom-merge guard passes) and the review the gate stands on.
		names, err := action.NewBranchNames("merge", "4a17")
		if err != nil {
			t.Fatal(err)
		}
		pr := f.SeedPR(forge.PR{Head: names.TaskBranch(), BaseRef: names.FeatureBranch()})
		f.SeedReview(pr.Number, "the gate's yes stands on the recorded review", forge.VerdictApprove)
		st2, err := Pinned(context.Background(), s, st.ID)
		if err != nil {
			t.Fatal(err)
		}
		seedPREvidence(t, s, st2, pr.Number)
		// The seam's yes, and the pr it was asked about.
		var gatePR int
		dep := Deps{
			Store: s,
			Actions: func(string) *action.Services {
				return &action.Services{
					Git:   &action.GitActions{Git: realGitClient()},
					Forge: &action.ForgeActions{Forge: f, Merge: mergeOKProbe{}},
				}
			},
			Autonomy: AutonomySeams{Seams: autonomy.Seams{Gates: func(ctx context.Context, loopID string, asked int) autonomy.Answer {
				gatePR = asked
				return autonomy.AnswerMet("the merge rule is met — CI green, approved in (S57 stand-in)")
			}}},
		}
		if err := Run(context.Background(), dep, st.ID); err != nil {
			t.Fatal(err)
		}
		// 1. The fake saw the irreversible effect run.
		saw := false
		for _, c := range f.Calls() {
			if c.Method == "MergePR" {
				saw = true
			}
		}
		if !saw {
			t.Fatalf("the granted merge did not run: fake calls %v", f.Calls())
		}
		// 2. The gate was asked about the evidence's PR, not a remembered number.
		if gatePR != pr.Number {
			t.Errorf("the Gates seam was asked about PR %d; want the evidence's %d", gatePR, pr.Number)
		}
		// 3. The use log receipts.
		uses, err := s.AutonomyUses(store.AutonomyUseFilter{GrantID: grantID})
		if err != nil {
			t.Fatal(err)
		}
		if len(uses) != 2 || uses[0].Phase != "begun" || uses[1].Phase != "completed" {
			t.Errorf("want begun then completed, got %d rows: %+v", len(uses), uses)
		}
		saved, err := Pinned(context.Background(), s, st.ID)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Status != "done" {
			t.Errorf("status %q; want done after the merge ran", saved.Status)
		}
	})

	t.Run("unmet", func(t *testing.T) {
		s := openLoopStore(t)
		f := forge.NewFake()
		// The pin names the REAL grant: an empty grant id would read back as
		// a covered entry with no grant row, and the live read refuses it.
		rootPath, rootID, grantID := seedComponentAndGrant(t, s, action.ForgeMerge, `{"requireGates":true}`, &future)
		st := startPinned(t, s, "merge-unmet", rootPath, rootID, []string{action.ForgeMerge}, grantID, false, false, action.ForgeMerge)
		// S57's stand-in sentence: whatever the seam says, the deny carries it.
		s57 := "the merge rule says no — CI is red on the PR's head (S57 stand-in)"
		dep := Deps{
			Store:   s,
			Actions: func(string) *action.Services { return svcWith(f) },
			Autonomy: AutonomySeams{Seams: autonomy.Seams{Gates: func(context.Context, string, int) autonomy.Answer {
				return autonomy.AnswerUnmet(s57)
			}}},
		}
		if err := Run(context.Background(), dep, st.ID); err != nil {
			t.Fatal(err)
		}
		saved, err := Pinned(context.Background(), s, st.ID)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Status != "blocked" {
			t.Fatalf("status %q; want blocked — an unmet merge rule must not merge", saved.Status)
		}
		if !strings.Contains(saved.LastReason, s57) {
			t.Errorf("the S57 sentence must ride VERBATIM in the deny, got %q", saved.LastReason)
		}
		if !strings.Contains(saved.LastReason, "requireGates is unmet") {
			t.Errorf("the deny must name the failing condition, got %q", saved.LastReason)
		}
		for _, c := range f.Calls() {
			if c.Method == "MergePR" {
				t.Fatalf("the unmet gate ran the merge (%v)", c)
			}
		}
	})

	t.Run("seam-nil", func(t *testing.T) {
		s := openLoopStore(t)
		rootPath, rootID, grantID := seedComponentAndGrant(t, s, action.ForgeMerge, `{"requireGates":true}`, &future)
		st := startPinned(t, s, "merge-nil", rootPath, rootID, []string{action.ForgeMerge}, grantID, false, false, action.ForgeMerge)
		// No seam wired (the daemon's wiring until S57 lands): cannot-say, never met.
		v := grantVerdict(t, s, &st, action.ForgeMerge, false, false, autonomy.Seams{})
		if v.Verdict != action.VerdictDeny {
			t.Fatalf("a nil Gates seam must deny, got %v", v.Verdict)
		}
		const want = "the merge rule (S57) could not be asked — the verification service is not wired"
		if !strings.Contains(v.Sentence, want) {
			t.Errorf("the deny must name that the merge rule could not be asked, got %q", v.Sentence)
		}
	})

	t.Run("outward-only", func(t *testing.T) {
		s := openLoopStore(t)
		rootPath := t.TempDir()
		c, err := s.CreateComponent(store.NewComponent{Name: "repo", Slug: "outward", Root: rootPath})
		if err != nil {
			t.Fatal(err)
		}
		rootID := c.Component.ID
		g, refusal, err := s.CreateAutonomyGrant(store.Actor{Kind: "user", ID: "u-op"}, store.NewAutonomyGrant{
			NodeID: rootID, SubjectKind: "class", Subject: "outward", State: "allow",
		})
		if err != nil {
			t.Fatal(err)
		}
		if refusal != nil {
			t.Fatalf("class grant refused: %v", refusal)
		}
		// The pin's snapshot is the outward expansion — the merge is not among it.
		st := startPinned(t, s, "outward-only", rootPath, rootID, outwardActions(), g.ID, false, false, action.ForgeMerge)
		v := grantVerdict(t, s, &st, action.ForgeMerge, false, false, autonomy.Seams{})
		base, err := action.Resolve(action.ForgeMerge, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if v.Verdict != action.VerdictDeny {
			t.Fatalf("an outward grant must not widen to the merge, got %v", v.Verdict)
		}
		if v.Sentence != base.Sentence {
			t.Errorf("the deny must be T1's, byte-identical to the no-grant loop:\ngot:  %q\nwant: %q", v.Sentence, base.Sentence)
		}
	})
}

// --- the use ceiling: maxPerDay is the count of begun effects -----------------

// TestMaxPerDayDenies (behaviour 8): the grant carries maxPerDay=1, backed by
// the use log through the Uses seam (the daemon's CountAutonomyUsesSince).
// With no begun use the verdict runs; with one begun in the last 24 h the
// (N+1)th is denied, naming the limit. The count is begun rows — an outcome
// row is the receipt of an effect, not a new effect.
func TestMaxPerDayDenies(t *testing.T) {
	s := openLoopStore(t)
	rootPath, rootID, grantID := seedComponentAndGrant(t, s, action.ForgeOpenPR, `{"maxPerDay":1}`, nil)
	st := startPinned(t, s, "maxday", rootPath, rootID, []string{action.ForgeOpenPR}, grantID, false, false, action.ForgeOpenPR)
	uses := func(ctx context.Context, grantID string, since time.Time) (int, error) {
		return s.CountAutonomyUsesSince(grantID, since)
	}
	seams := autonomy.Seams{Uses: uses}

	// No begun use yet: within the limit, the verdict runs.
	v0 := grantVerdict(t, s, &st, action.ForgeOpenPR, false, false, seams)
	if v0.Verdict != action.VerdictRun {
		t.Fatalf("with no begun use the limit must not deny, got %v: %s", v0.Verdict, v0.Sentence)
	}
	// One begun use in the last 24 h: the (N+1)th is denied, naming the limit.
	if _, err := s.RecordAutonomyUse(st.ID, grantID, 0, action.ForgeOpenPR, "begun", 0, ""); err != nil {
		t.Fatal(err)
	}
	v1 := grantVerdict(t, s, &st, action.ForgeOpenPR, false, false, seams)
	if v1.Verdict != action.VerdictDeny {
		t.Fatalf("the second begun use within 24 h must deny, got %v", v1.Verdict)
	}
	if !strings.Contains(v1.Sentence, "the limit of 1 per day is reached (1 begun in the last 24 h)") {
		t.Errorf("the denial must name the limit and the count, got %q", v1.Sentence)
	}
}

// --- unauditable ⇒ no effect ---------------------------------------------------

// TestUnauditableEffectDoesNotRun (behaviour 9): the store refuses the use row
// (a second handle on the same file, writer read-only — every write on it
// fails at once), and the effect does NOT run: the fake saw zero calls and no
// use row exists. The receipt-first order is the property: an effect whose
// begun cannot be recorded is an unauditable effect, and an unauditable effect
// does not run. (The block's own status write rides the same refused writer, so
// the run surfaces the refusal as an error — the daemon's runner-error path,
// AL9: a write failure is a blocked kind of stop, never a run.)
func TestUnauditableEffectDoesNotRun(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	f := forge.NewFake()
	rootPath, rootID, grantID := seedComponentAndGrant(t, s, action.ForgeOpenPR, "{}", nil)
	st := startPinned(t, s, "unaudited", rootPath, rootID, []string{action.ForgeOpenPR}, grantID, false, false, action.ForgeOpenPR)
	// A second handle on the same file, read-only: its writer refuses every
	// write, so the use row cannot be recorded. SkipMigrations: the first
	// handle already migrated the file, and this one needs no DDL.
	dbPath := filepath.Join(dir, "state.db")
	ro, err := store.Open(store.Options{
		SkipMigrations: true,
		Env: func(k string) string {
			if k == "DATABASE_SQLITE_DSN" {
				return "file:" + dbPath + "?mode=ro"
			}
			return ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ro.Close() })
	dep := Deps{Store: ro, Actions: func(string) *action.Services { return svcWith(f) }}
	if err := Run(context.Background(), dep, st.ID); err == nil {
		t.Fatal("the run completed on a store that cannot record the use — the unauditable effect ran")
	}
	// The effect did not run: the fake saw nothing.
	for _, c := range f.Calls() {
		if c.Method == "OpenPR" {
			t.Fatalf("the unauditable effect ran: %v", c)
		}
	}
	// And no use row exists, read through the writable handle.
	uses, err := s.AutonomyUses(store.AutonomyUseFilter{GrantID: grantID})
	if err != nil {
		t.Fatal(err)
	}
	if len(uses) != 0 {
		t.Errorf("use rows on a writer that refused them: %+v", uses)
	}
}

// --- guards ---------------------------------------------------------------------

// TestEveryConditionSeamNilDenies: a nil seam is cannot-say, never met (AL4).
// Each condition, with its seam unwired, denies naming the condition and the
// absent service — the slice fails closed while a neighbour is unwired, so a
// merge grant is inert until S57 exists and a budget grant until S55.
func TestEveryConditionSeamNilDenies(t *testing.T) {
	cases := []struct {
		name string
		cond string
		want string
	}{
		{"gates", `{"requireGates":true}`, "the merge rule (S57) could not be asked — the verification service is not wired"},
		{"budget", `{"requireBudget":true}`, "the budget could not be asked — the budget service is not wired"},
		{"uses", `{"maxPerDay":1}`, "the use count could not be asked — the use log is not wired"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openLoopStore(t)
			rootPath, rootID, grantID := seedComponentAndGrant(t, s, action.ForgeOpenPR, tc.cond, nil)
			st := startPinned(t, s, "nilseam-"+tc.name, rootPath, rootID, []string{action.ForgeOpenPR}, grantID, false, false, action.ForgeOpenPR)
			v := grantVerdict(t, s, &st, action.ForgeOpenPR, false, false, autonomy.Seams{})
			if v.Verdict != action.VerdictDeny {
				t.Fatalf("a nil seam must deny, got %v", v.Verdict)
			}
			if !strings.Contains(v.Sentence, tc.want) {
				t.Errorf("the denial must name the condition and the absent service, got %q", v.Sentence)
			}
		})
	}
}

// TestOnlyTheDecisionSiteCallsResolveWithGrant (AL9): the one decision door.
// A second door would be a second truth — a surface that can decide a grant
// outside the loop's action moment is a veto the live checks never see. The
// walk pins that ResolveWithGrant is CALLED from non-test sources only in
// loop/execute.go (its definition lives in action/grant.go; tests may call it).
//
// Amended 2026-11-01 by S56 T7: daemon/autonomy.go is admitted as a second,
// read-only caller — the autonomy.pinned view renders each action's verdict
// with zero seams (AL14/AL17), and the result is only displayed, never stored
// as the loop's decision. T7's sibling guard re-asserts this amended allow
// set (TestResolveWithGrantIsCalledOnlyFromTheDecisionSite, S56 T7).
func TestOnlyTheDecisionSiteCallsResolveWithGrant(t *testing.T) {
	intRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	err = filepath.WalkDir(intRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(intRoot, p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || !strings.Contains(line, "ResolveWithGrant(") {
				continue
			}
			if strings.Contains(line, "func ResolveWithGrant(") {
				continue // the definition, not a call
			}
			calls = append(calls, filepath.ToSlash(rel)+": line "+strconv.Itoa(i+1)+": "+trimmed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) == 0 {
		t.Error("ResolveWithGrant is never called — the decision site lost its door")
	}
	for _, c := range calls {
		// loop/execute.go is the decision site. daemon/autonomy.go renders the
		// pinned view (zero seams, display only) — see the amendment note above.
		if !strings.HasPrefix(c, "loop/execute.go:") && !strings.HasPrefix(c, "daemon/autonomy.go:") {
			t.Errorf("a second decision door: %s", c)
		}
	}
}

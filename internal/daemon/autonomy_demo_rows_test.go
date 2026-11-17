package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
)

// TestAutonomyDemoWalk is the T11 step-3 table, row by row.
func TestAutonomyDemoWalk(t *testing.T) {
	if testing.Short() {
		t.Skip("full autonomy demo builds and runs the CLI; run make test-go")
	}
	w := newWalkEnv(t)
	ctx := context.Background()
	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Format(time.RFC3339)

	// --- ROW 1: effective before any grant --------------------------------
	walkRow(t, 1, "aeon autonomy effective --root R before any grant: the no-grant statement; every non-local action deny")
	w.installRunner(t, walkPhase{budget: walkBudgetMet})
	eff := w.method(t, protocol.MethodAutonomyEffective, protocol.AutonomyEffectiveParams{RootID: w.root.ID, Origin: strP("schedule")}).(protocol.EffectiveView)
	if !strings.Contains(eff.Statement, "No autonomy is granted") {
		t.Errorf("the no-grant statement is not the fixed sentence: %q", eff.Statement)
	}
	if len(eff.Actions) == 0 {
		t.Fatalf("the effective view carries no per-action rows")
	}
	for _, row := range eff.Actions {
		if row.Class != "local" && row.Verdict != "deny" {
			t.Errorf("action %s (class %s) is %s before any grant — want deny", row.Action, row.Class, row.Verdict)
		}
	}
	t.Logf("row 1: statement %q; %d action rows, every non-local deny", eff.Statement, len(eff.Actions))

	// --- ROW 2: the first fire, no grant ----------------------------------
	walkRow(t, 2, "trigger fires: the local createBranch runs, git.commitAndPush blocks action-denied with T2's sentence verbatim; the fake saw nothing; allow_outward = 0; no pin row")
	loopID := w.fire(t, "walk-2", time.Date(2026, 11, 1, 3, 30, 0, 0, time.UTC))
	st := w.loopState(t, loopID)
	if st.Status != "blocked" {
		t.Fatalf("the no-grant loop is %s, want blocked: reason %q", st.Status, st.Runtime.LastReason)
	}
	want := "git.commitAndPush denied — effect class 'outward' resolves to deny (loop is unattended)"
	if !strings.Contains(st.Runtime.LastReason, want) {
		t.Errorf("the block reason is not T2's sentence verbatim:\n got %q\nwant %q", st.Runtime.LastReason, want)
	}
	if calls := w.fake.Calls(); len(calls) != 0 {
		t.Errorf("the forge fake saw %d calls before any grant: %v", len(calls), calls)
	}
	outward, err := w.store.LoopAllowOutward(ctx, loopID)
	if err != nil {
		t.Fatalf("allow_outward read: %v", err)
	}
	if outward {
		t.Errorf("a no-grant loop moved allow_outward — the pin must never touch the legacy flag (AL8)")
	}
	if _, pinOK, err := w.store.ReadAutonomyPin(loopID); err != nil || pinOK {
		t.Errorf("a no-grant start wrote a pin row (ok=%v) — want none (byte-identical)", pinOK)
	}
	t.Logf("row 2: %q", st.Runtime.LastReason)

	// --- ROW 3: grant the outward class ------------------------------------
	walkRow(t, 3, "aeon autonomy grant --root R --action outward --for 30d --yes: the confirmation; grant active; changes has one fixed-template row naming the peer (the template carries no grant id — the id is the row's own field)")
	gres := w.method(t, protocol.MethodAutonomyGrant, protocol.AutonomyGrantParams{
		RootID: w.root.ID, Subject: "outward", ExpiresAt: &expires,
	}).(protocol.AutonomyGrantResult)
	if gres.Grant == nil || gres.Grant.ID == "" {
		t.Fatalf("the grant did not write: %+v", gres)
	}
	if !strings.Contains(gres.Confirmation, "is now allowed for unattended loops on "+w.root.ID) {
		t.Errorf("the grant confirmation is not the fixed template: %q", gres.Confirmation)
	}
	cres := w.method(t, protocol.MethodAutonomyChanges, protocol.AutonomyChangesParams{RootID: strP(w.root.ID)}).(protocol.AutonomyChangesResult)
	if len(cres.Changes) != 1 {
		t.Fatalf("changes has %d rows, want one fixed-template row", len(cres.Changes))
	}
	c := cres.Changes[0]
	if c.GrantID != gres.Grant.ID {
		t.Errorf("the change row names grant %s, want %s", c.GrantID, gres.Grant.ID)
	}
	if c.ChangeKind != "grant" {
		t.Errorf("the change row's kind is %s, want grant", c.ChangeKind)
	}
	if c.ActorKind != "user" {
		t.Errorf("the change row's actor kind is %s, want user (the socket's actor)", c.ActorKind)
	}
	wantedSummary := "autonomy grant of outward on root " + c.RootID + " by " + c.ActorID
	if c.Summary != wantedSummary {
		t.Errorf("the change row's summary is not the fixed template:\n got %q\nwant %q", c.Summary, wantedSummary)
	}
	t.Logf("row 3: grant %s; %q; change row %q", gres.Grant.ID, gres.Confirmation, c.Summary)

	// --- ROW 4: the fire under the grant -----------------------------------
	walkRow(t, 4, "the next fire under the grant: pinned; push and forge.openPR run (the fake saw them); the use log shows begun/completed with the grant id and rev; allow_outward still 0")
	w.installRunner(t, walkPhase{budget: walkBudgetMet})
	loopID2 := w.fire(t, "walk-4", time.Date(2026, 11, 1, 4, 30, 0, 0, time.UTC))
	pin, pinOK, err := w.store.ReadAutonomyPin(loopID2)
	if err != nil || !pinOK {
		t.Fatalf("the granted start wrote no pin row (ok=%v, err=%v)", pinOK, err)
	}
	var allowed []pinEntry
	if err := json.Unmarshal([]byte(pin.Allowed), &allowed); err != nil {
		t.Fatalf("pin allowed JSON: %v", err)
	}
	// The pin records the outward class EXPANDED to its action names — one
	// entry per action the class covers, all under the one grant.
	wantSubjects := outwardActionSubjects()
	if len(allowed) != len(wantSubjects) {
		t.Errorf("the pin carries %d entries, want one per outward action (%d): %s", len(allowed), len(wantSubjects), pin.Allowed)
	}
	for _, e := range allowed {
		if e.Grant != gres.Grant.ID {
			t.Errorf("a pin entry names grant %s, want %s", e.Grant, gres.Grant.ID)
		}
		if !wantSubjects[e.Subject] {
			t.Errorf("the pin entry's subject %s is not an outward action", e.Subject)
		}
		wantSubjects[e.Subject] = false
	}
	pr4 := w.bindPR(t)
	if pr4 != 1 {
		t.Errorf("the second PR opened is #%d, want #1 (the walk is the fake's only client)", pr4)
	}
	t.Logf("row 4: pinned at rev %d, origin %s, allowed %s", pin.Rev, pin.Origin, pin.Allowed)
	st2 := w.loopState(t, loopID2)
	if st2.Status != "waiting" {
		t.Fatalf("the granted loop is %s (reason %q), want waiting at the ship gate", st2.Status, st2.Runtime.LastReason)
	}
	if st2.Runtime.QuestionID == 0 {
		t.Errorf("the loop is waiting but carries no question id")
	}
	sawOpenPR, sawMerge := false, false
	for _, fc := range w.fake.Calls() {
		if fc.Method == "OpenPR" {
			sawOpenPR = true
		}
		if fc.Method == "MergePR" {
			sawMerge = true
		}
	}
	if !sawOpenPR {
		t.Errorf("the forge fake never saw OpenPR under the outward grant")
	}
	if sawMerge {
		t.Errorf("the forge fake saw MergePR without a merge grant")
	}
	ures := w.method(t, protocol.MethodAutonomyUses, protocol.AutonomyUsesParams{LoopID: strP(loopID2)}).(protocol.AutonomyUsesResult)
	if len(ures.Uses) == 0 {
		t.Fatalf("the use log is empty for the loop that ran two outward actions")
	}
	seen := map[string]bool{}
	for _, u := range ures.Uses {
		if u.GrantID != gres.Grant.ID {
			t.Errorf("a use row names grant %s, want %s", u.GrantID, gres.Grant.ID)
		}
		if u.GrantRev != pin.Rev {
			t.Errorf("a use row names rev %d, want the pin's rev %d", u.GrantRev, pin.Rev)
		}
		seen[u.Action+" "+u.Phase] = true
	}
	for _, k := range []string{"git.commitAndPush begun", "git.commitAndPush completed", "forge.openPR begun", "forge.openPR completed"} {
		if !seen[k] {
			t.Errorf("the use log lacks %q: %+v", k, ures.Uses)
		}
	}
	outward2, err := w.store.LoopAllowOutward(ctx, loopID2)
	if err != nil {
		t.Fatalf("allow_outward read: %v", err)
	}
	if outward2 {
		t.Errorf("the pinned loop moved allow_outward — the pin is not the legacy lever (AL8)")
	}

	// --- ROW 5: the same loop reaches forge.merge ---------------------------
	walkRow(t, 5, "the same loop reaches forge.merge: denied — irreversible is not implied by outward (the action is outside the pin, so the base verdict stands)")
	answerShip(t, w, loopID2)
	st2 = w.loopState(t, loopID2)
	if st2.Status != "blocked" {
		t.Fatalf("after the gate the loop is %s, want blocked at forge.merge", st2.Status)
	}
	want5 := "forge.merge denied — effect class 'irreversible' resolves to deny (loop is unattended)"
	if !strings.Contains(st2.Runtime.LastReason, want5) {
		t.Errorf("the merge denial is not the base verdict:\n got %q\nwant %q", st2.Runtime.LastReason, want5)
	}
	t.Logf("row 5: %q", st2.Runtime.LastReason)

	// --- ROW 6: the merge grant's doors --------------------------------------
	walkRow(t, 6, "aeon autonomy grant --root R --action forge.merge: refused without an expiry (the expiry rule speaks first once the merge rule is carried), refused with the merge rule turned off, accepted only with both — the forced gate condition")
	// (a) no expiry — carrying RequireGates, or the rule's refusal would
	// speak first (the check order is the point).
	noExpiry := w.methodErr(t, protocol.MethodAutonomyGrant, protocol.AutonomyGrantParams{
		RootID: w.root.ID, Subject: "forge.merge",
		Conditions: &protocol.AutonomyConditions{RequireGates: true},
	})
	if perr := autonomyRefused(t, noExpiry); !strings.Contains(perr.Data.What, "forge.merge must carry an expiry") {
		t.Errorf("the no-expiry refusal is not the fixed sentence: %+v", perr)
	}
	// (b) the merge rule turned off — it cannot be.
	noGates := w.methodErr(t, protocol.MethodAutonomyGrant, protocol.AutonomyGrantParams{
		RootID: w.root.ID, Subject: "forge.merge", ExpiresAt: &expires,
	})
	if perr := autonomyRefused(t, noGates); !strings.Contains(perr.Data.What, "forge.merge must carry the merge rule") {
		t.Errorf("the no-merge-rule refusal is not the fixed sentence: %+v", perr)
	}
	// (c) both — accepted.
	mres := w.method(t, protocol.MethodAutonomyGrant, protocol.AutonomyGrantParams{
		RootID: w.root.ID, Subject: "forge.merge",
		Conditions: &protocol.AutonomyConditions{RequireGates: true},
		ExpiresAt:  &expires,
	}).(protocol.AutonomyGrantResult)
	if mres.Grant == nil || mres.Grant.ID == "" {
		t.Fatalf("the merge grant did not write: %+v", mres)
	}
	t.Logf("row 6: merge grant %s accepted: %q", mres.Grant.ID, mres.Confirmation)

	// --- ROW 7: the Gates seam's triad, one fire each ------------------------
	walkRow(t, 7, "three next fires under the merge grant, one per seam state: absent → cannot-say names the unwired service; unmet → denied with the seam's sentence; met → the merge runs and the loop is done")
	// (a) the seam absent — a pin whose merge rule could not be asked.
	w.installRunner(t, walkPhase{budget: walkBudgetMet})
	loop7a := w.fire(t, "walk-7a", time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC))
	w.bindPR(t)
	answerShip(t, w, loop7a)
	st7a := w.loopState(t, loop7a)
	if st7a.Status != "blocked" {
		t.Fatalf("the cannot-say loop is %s, want blocked at forge.merge", st7a.Status)
	}
	want7a := "grant " + mres.Grant.ID + "'s condition requireGates is cannot-say — the merge rule (S57) could not be asked — the verification service is not wired"
	if !strings.Contains(st7a.Runtime.LastReason, want7a) {
		t.Errorf("the cannot-say denial does not carry the seam's sentence verbatim:\n got %q\nwant %q", st7a.Runtime.LastReason, want7a)
	}
	if !strings.Contains(st7a.Runtime.LastReason, "the loop was pinned at rev") {
		t.Errorf("the cannot-say denial does not name the pinned revision: %q", st7a.Runtime.LastReason)
	}
	t.Logf("row 7a: %q", st7a.Runtime.LastReason)

	// (b) the seam answers unmet — its own sentence, carried verbatim.
	unmetSent := "the walk's gates stub says the merge rule failed (row 7b)"
	w.installRunner(t, walkPhase{
		gates: func(ctx context.Context, loopID string, pr int) autonomy.Answer {
			return autonomy.AnswerUnmet(unmetSent)
		},
		budget: walkBudgetMet,
	})
	loop7b := w.fire(t, "walk-7b", time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC))
	w.bindPR(t)
	answerShip(t, w, loop7b)
	st7b := w.loopState(t, loop7b)
	if st7b.Status != "blocked" {
		t.Fatalf("the unmet loop is %s, want blocked at forge.merge", st7b.Status)
	}
	want7b := "grant " + mres.Grant.ID + "'s condition requireGates is unmet — " + unmetSent
	if !strings.Contains(st7b.Runtime.LastReason, want7b) {
		t.Errorf("the unmet denial does not carry the seam's sentence verbatim:\n got %q\nwant %q", st7b.Runtime.LastReason, want7b)
	}
	t.Logf("row 7b: %q", st7b.Runtime.LastReason)

	// (c) the seam answers met — the merge runs; the loop is done.
	w.installRunner(t, walkPhase{gates: gatesMet, budget: walkBudgetMet})
	loop7c := w.fire(t, "walk-7c", time.Date(2026, 11, 1, 7, 30, 0, 0, time.UTC))
	pr7c := w.bindPR(t)
	w.fake.SeedReview(pr7c, "walk approval", forge.VerdictApprove)
	answerShip(t, w, loop7c)
	st7c := w.loopState(t, loop7c)
	if st7c.Status != "done" {
		t.Fatalf("the met-gates loop is %s (reason %q), want done — the merge ran", st7c.Status, st7c.Runtime.LastReason)
	}
	sawMergePR := false
	for _, fc := range w.fake.Calls() {
		if fc.Method == "MergePR" {
			sawMergePR = true
		}
	}
	if !sawMergePR {
		t.Errorf("the fake never saw MergePR on the met-gates fire")
	}
	mergedPR, err := w.fake.ReadPR(ctx, pr7c)
	if err != nil || mergedPR == nil || !mergedPR.Merged || mergedPR.MergeSHA == "" {
		t.Errorf("the fake's PR #%d is not merged (merged=%v sha=%q err=%v)", pr7c, mergedPR != nil && mergedPR.Merged, mergedPR.MergeSHA, err)
	}
	if st7c.Runtime.LastReason != "" {
		t.Logf("row 7c (run sentence): %q", st7c.Runtime.LastReason)
	}

	// --- ROW 8: the revoke mid-run -------------------------------------------
	walkRow(t, 8, "revoke mid-run: the loop waits at the gate; the merge grant is revoked (the revoke names the affected loops); the answer runs — and the merge is blocked with the revocation clause")
	loop8 := w.fire(t, "walk-8", time.Date(2026, 11, 1, 8, 30, 0, 0, time.UTC))
	w.bindPR(t)
	st8 := w.loopState(t, loop8)
	if st8.Status != "waiting" {
		t.Fatalf("the loop is %s (reason %q), want waiting at the ship gate", st8.Status, st8.Runtime.LastReason)
	}
	rev := w.method(t, protocol.MethodAutonomyRevoke, protocol.AutonomyRevokeParams{
		ID: mres.Grant.ID, Reason: "walk: mid-run revocation",
	}).(protocol.AutonomyRevokeResult)
	affected := false
	for _, l := range rev.AffectedLoops {
		if l == loop8 {
			affected = true
		}
	}
	if !affected {
		t.Errorf("the revoke's affectedLoops %v does not include the waiting loop %s", rev.AffectedLoops, loop8)
	}
	want8 := "revoked: " + mres.Grant.ID + " (forge.merge) no longer applies — "
	if !strings.HasPrefix(rev.Confirmation, want8) || !strings.HasSuffix(rev.Confirmation, "running loop(s) were pinned to it") {
		t.Errorf("the revoke confirmation is not the fixed template: %q", rev.Confirmation)
	}
	t.Logf("row 8: %q; affectedLoops %v", rev.Confirmation, rev.AffectedLoops)
	answerShip(t, w, loop8)
	st8 = w.loopState(t, loop8)
	if st8.Status != "blocked" {
		t.Fatalf("after the revoke the loop is %s, want blocked at forge.merge", st8.Status)
	}
	if !strings.Contains(st8.Runtime.LastReason, "grant "+mres.Grant.ID+" was revoked at") {
		t.Errorf("the post-revoke denial is not the revocation clause:\n %q", st8.Runtime.LastReason)
	}
	// The revoker is the walk's default uid-0 actor, which the revocation
	// clause renders bare as `user` (not `user:0`).
	if !strings.Contains(st8.Runtime.LastReason, " by user — ") {
		t.Errorf("the post-revoke denial does not name the revoker (the walk's default actor, rendered bare as `user`):\n %q", st8.Runtime.LastReason)
	}
	if !strings.Contains(st8.Runtime.LastReason, "the loop was pinned at rev") {
		t.Errorf("the post-revoke denial does not name the pinned revision:\n %q", st8.Runtime.LastReason)
	}
	t.Logf("row 8: %q", st8.Runtime.LastReason)

	// --- ROW 9: a grant added after the pin does not widen it -----------------
	walkRow(t, 9, "grant added after the pin: the loop pins the outward grant only (the merge grant is revoked); the merge grant is re-granted while the loop waits; the merge is still denied — the base verdict, the live grant is not the pin")
	loop9 := w.fire(t, "walk-9", time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC))
	pin9, pinOK9, err := w.store.ReadAutonomyPin(loop9)
	if err != nil || !pinOK9 {
		t.Fatalf("the granted start wrote no pin row (ok=%v, err=%v)", pinOK9, err)
	}
	var allowed9 []pinEntry
	if err := json.Unmarshal([]byte(pin9.Allowed), &allowed9); err != nil {
		t.Fatalf("pin allowed JSON: %v", err)
	}
	// A FRESH set: row 4's loop marked its own map's entries as it consumed
	// them, and this row must not see those marks.
	wantSubjects9 := outwardActionSubjects()
	if len(allowed9) != len(wantSubjects9) {
		t.Fatalf("the pin carries %d entries, want the outward expansion (%d): %s", len(allowed9), len(wantSubjects9), pin9.Allowed)
	}
	for _, e := range allowed9 {
		if e.Grant != gres.Grant.ID {
			t.Fatalf("the pin carries a grant other than the outward one: %s (the merge grant was revoked at row 8)", pin9.Allowed)
		}
		if !wantSubjects9[e.Subject] {
			t.Fatalf("the pin entry's subject %s is not an outward action: %s", e.Subject, pin9.Allowed)
		}
	}
	w.bindPR(t)
	mg2 := w.method(t, protocol.MethodAutonomyGrant, protocol.AutonomyGrantParams{
		RootID: w.root.ID, Subject: "forge.merge",
		Conditions: &protocol.AutonomyConditions{RequireGates: true},
		ExpiresAt:  &expires,
	}).(protocol.AutonomyGrantResult)
	if mg2.Grant == nil || mg2.Grant.ID == "" {
		t.Fatalf("the re-grant did not write: %+v", mg2)
	}
	answerShip(t, w, loop9)
	st9 := w.loopState(t, loop9)
	if st9.Status != "blocked" {
		t.Fatalf("after the gate the loop is %s, want blocked at forge.merge", st9.Status)
	}
	want9 := "forge.merge denied — effect class 'irreversible' resolves to deny (loop is unattended)"
	if !strings.Contains(st9.Runtime.LastReason, want9) {
		t.Errorf("the post-pin grant did not widen the loop — the denial is not the base verdict:\n got %q\nwant %q", st9.Runtime.LastReason, want9)
	}
	if strings.Contains(pin9.Allowed, mg2.Grant.ID) {
		t.Errorf("the pin's allowed list carries the post-pin grant %s: %s", mg2.Grant.ID, pin9.Allowed)
	}
	t.Logf("row 9: live merge grant %s exists; the pin (rev %d) is %s; the denial: %q", mg2.Grant.ID, pin9.Rev, pin9.Allowed, st9.Runtime.LastReason)

	// --- ROW 10: the CLI with the daemon stopped ------------------------------
	walkRow(t, 10, "the CLI with the daemon stopped: deny falls back with the fixed stderr line (the veto's actor is cli:cli); the veto holds when the daemon reads again; grant is refused with the fixed sentence and writes nothing")
	bin := w.buildCLI(t)
	childDir := t.TempDir()
	walkGit(t, childDir, "init", "-b", "main")
	walkGit(t, childDir, "config", "user.email", "walk@example.com")
	walkGit(t, childDir, "config", "user.name", "Walk")
	if err := os.WriteFile(filepath.Join(childDir, "README.md"), []byte("# child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	walkGit(t, childDir, "add", ".")
	walkGit(t, childDir, "commit", "-m", "initial")
	ccw, err := w.store.CreateComponent(store.NewComponent{ParentID: w.root.ID, Name: "walk child", Slug: "walk-child", Root: childDir})
	if err != nil || !ccw.OK() {
		t.Fatalf("create the child component: %v %s", err, ccw.Reason)
	}
	child := ccw.Component

	// "daemon stopped" made deterministic: the CLI is pointed at a dead
	// endpoint path — a dial to nobody — and at the SAME state file the
	// in-process daemon reads (AEON_STATE_DIR), so the offline write is the
	// one the daemon's next read folds in. The in-process daemon never bound
	// a socket to restart; the fact being proven is the read, not the
	// process, and there is no process-side cache (the decision site re-reads
	// the store per action) — said so in the record.
	deadEndpoint := filepath.Join(t.TempDir(), "no-such-socket")
	cliEnv := append(os.Environ(),
		"LOCALRPC_ENDPOINT_PATH="+deadEndpoint,
		"AEON_STATE_DIR="+w.storeDir,
	)

	allowBefore, err := w.store.ListAutonomyGrants(store.AutonomyGrantFilter{RootID: w.root.ID, State: "allow"})
	if err != nil {
		t.Fatalf("list allows before: %v", err)
	}
	_, stderr, denyErr := runCLI(t, bin, cliEnv, "autonomy", "deny",
		"--node", child.ID, "--action", "outward", "--note", "walk veto", "--json")
	if denyErr != nil {
		t.Fatalf("the offline deny exited non-zero: %v\nstderr: %s", denyErr, stderr)
	}
	if !strings.Contains(stderr, "daemon not running — applied directly to the store") {
		t.Errorf("the offline deny did not print the fixed stderr line:\n%s", stderr)
	}
	vetoes, err := w.store.ListAutonomyGrants(store.AutonomyGrantFilter{RootID: w.root.ID, State: "deny"})
	if err != nil {
		t.Fatalf("list vetoes: %v", err)
	}
	var veto *store.AutonomyGrant
	for i := range vetoes {
		if vetoes[i].NodeID == child.ID && vetoes[i].Subject == "outward" && vetoes[i].State == "deny" {
			veto = &vetoes[i]
		}
	}
	if veto == nil {
		t.Fatalf("the offline deny wrote no outward veto on the child")
	}
	if veto.CreatedByKind != "cli" || veto.CreatedByID != "cli" {
		t.Errorf("the veto's actor is %s:%s, want cli:cli — the direct door's audit identity", veto.CreatedByKind, veto.CreatedByID)
	}
	t.Logf("row 10: offline deny wrote veto %s on child %s, actor %s:%s", veto.ID, child.ID, veto.CreatedByKind, veto.CreatedByID)

	eff2 := w.method(t, protocol.MethodAutonomyEffective, protocol.AutonomyEffectiveParams{RootID: w.root.ID, Origin: strP("schedule")}).(protocol.EffectiveView)
	foundOutwardDeny := false
	for _, row := range eff2.Actions {
		if row.Class == "outward" && row.Verdict == "deny" {
			foundOutwardDeny = true
		}
		if row.Class != "local" && row.Verdict != "deny" {
			t.Errorf("after the child veto, action %s (class %s) is %s — the veto overrides every grant on the component and its root", row.Action, row.Class, row.Verdict)
		}
	}
	if !foundOutwardDeny {
		t.Errorf("no outward action shows the veto's deny — the veto did not hold when the daemon read again: %+v", eff2.Actions)
	}

	_, grantStderr, grantErr := runCLI(t, bin, cliEnv, "autonomy", "grant",
		"--root", w.root.ID, "--action", "forge.merge", "--for", "7d", "--yes", "--json")
	if grantErr == nil {
		t.Errorf("the offline grant succeeded — it must be refused")
	}
	if !strings.Contains(grantStderr, "daemon not running — grants are written through the daemon so the audit carries who asked; start the daemon, or use `aeon autonomy deny`/`revoke` to restrict") {
		t.Errorf("the offline grant did not print the fixed refusal sentence:\n%s", grantStderr)
	}
	allowAfter, err := w.store.ListAutonomyGrants(store.AutonomyGrantFilter{RootID: w.root.ID, State: "allow"})
	if err != nil {
		t.Fatalf("list allows after: %v", err)
	}
	if len(allowAfter) != len(allowBefore) {
		t.Errorf("the refused grant wrote a row: %d -> %d allows", len(allowBefore), len(allowAfter))
	}

	// --- ROW 11: the agent arm, the human commit ------------------------------
	walkRow(t, 11, "the agent arm: the agent proposes a revoke in the chat loop (inert — nothing is enforced); the person commits at the human door; the grant is revoked with the person as actor, the proposal carrying the agent's authorship")
	// The commit door takes its actor from the peer (ActorFor). The walk's
	// default stamps the test connection's uid 0, and the policy door
	// refuses id "0" (decisionActorRefusal) — the person who commits is a
	// person, so the walk names one, before the commit dispatch.
	w.d.Table.ActorFor = func(_ context.Context, _ *protocol.Session) protocol.Actor {
		return protocol.Actor{Kind: "user", ID: "walk-user"}
	}
	prop, f, err := w.d.recordsSvc.autonomyProposeRevoke(ctx, Actor{Kind: "agent", ID: "walk-agent"},
		json.RawMessage(`{"id":"`+mg2.Grant.ID+`","reason":"walk: the agent asks to revoke the merge grant"}`))
	if err != nil {
		t.Fatalf("autonomy_propose_revoke: %v", err)
	}
	if f != nil {
		t.Fatalf("the agent's revoke proposal was refused: %s", f.Sentence)
	}
	res11 := prop.(protocol.PolicyProposeResult)
	if res11.Proposal.Status != "proposed" {
		t.Errorf("the proposal is %s, want proposed", res11.Proposal.Status)
	}
	if res11.Proposal.CreatedByKind != "agent" || res11.Proposal.CreatedByID != "walk-agent" {
		t.Errorf("the proposal's authorship is %s:%s, want agent:walk-agent", res11.Proposal.CreatedByKind, res11.Proposal.CreatedByID)
	}
	if res11.Note != protocol.PolicyProposeInertNote {
		t.Errorf("the inertness note is not the fixed sentence: %q", res11.Note)
	}
	t.Logf("row 11: proposal %d proposed by agent:walk-agent; note %q", res11.Proposal.ID, res11.Note)

	got11 := w.method(t, protocol.MethodProposalGet, protocol.ProposalGetParams{ID: res11.Proposal.ID}).(protocol.ProposalGetResult)
	cres11 := w.method(t, protocol.MethodProposalCommit, protocol.ProposalCommitParams{ID: res11.Proposal.ID, Digest: got11.Digest}).(protocol.ProposalCommitResult)
	if !cres11.EffectApplied {
		t.Errorf("the commit did not apply the effect: %+v", cres11)
	}
	if cres11.Proposal.DecidedByKind != "user" || cres11.Proposal.DecidedByID != "walk-user" {
		t.Errorf("the decision stamps %s:%s, want user:walk-user", cres11.Proposal.DecidedByKind, cres11.Proposal.DecidedByID)
	}
	g, err := w.store.ReadAutonomyGrant(mg2.Grant.ID)
	if err != nil {
		t.Fatalf("read the revoked grant: %v", err)
	}
	if g.RevokedAt == nil {
		t.Fatalf("the committed proposal left the grant unrevoked")
	}
	if g.RevokedByID == nil || *g.RevokedByID != "walk-user" {
		t.Errorf("the revocation stamps %v, want walk-user (the committer, not the proposer)", g.RevokedByID)
	}
	if g.ProposedBy != "" {
		t.Errorf("the grant's proposed_by is %q — the grant was created directly, so it carries no proposal authorship", g.ProposedBy)
	}
	changes, err := w.store.AutonomyChanges(store.AutonomyChangeFilter{GrantID: mg2.Grant.ID})
	if err != nil {
		t.Fatalf("list the grant's changes: %v", err)
	}
	revRows := 0
	for _, c := range changes {
		if c.ChangeKind != "revoke" {
			continue
		}
		revRows++
		if c.ActorKind != "user" || c.ActorID != "walk-user" {
			t.Errorf("the revoke change row's actor is %s:%s, want user:walk-user", c.ActorKind, c.ActorID)
		}
	}
	if revRows != 1 {
		t.Errorf("there are %d revoke change rows for %s, want exactly one", revRows, mg2.Grant.ID)
	}

	// --- ROW 12: no grant proposal, and the store's CHECK ---------------------
	walkRow(t, 12, "the agent cannot propose a grant: no such tool in the catalog (the refusal logged), while a catalog read runs clean; the store's CHECK half — a bypassed allow payload refused before SQL — is the sixth migration drill (s56_migration_test.go)")
	inv := w.d.recordsSvc.Invoker()
	content, isError := inv(ctx, "autonomy_propose_grant", json.RawMessage(`{"rootId":"`+w.root.ID+`","subject":"outward","reason":"walk: the agent tries to propose a grant"}`))
	if !isError {
		t.Fatalf("autonomy_propose_grant answered success — the catalog must not carry a grant proposal: %s", content)
	}
	t.Logf("row 12 refusal: %s", content)
	// The read runs with a real step identity: the invoker's agent half
	// resolves the actor from the step row (OL3). The walk's lanes are pure
	// code-action and write no step rows of their own (the model-step
	// runtime does), so the walk files one under loop9's scope. The row is
	// identity context only — the autonomy surface it reads is untouched.
	lp9, err12 := w.store.ReadLoopByID(loop9)
	if err12 != nil {
		t.Fatalf("read loop9 for its scope: %v", err12)
	}
	row12, errStep := w.store.CreateStep(store.Scope{LoopSlug: lp9.Slug, ComponentRoot: lp9.ComponentRoot},
		store.NewStep{Agent: "walk-agent", Status: store.StepRunning})
	if errStep != nil {
		t.Fatalf("file the row-12 step row: %v", errStep)
	}
	listContent, listErr := inv(stepctx.WithStepID(ctx, row12.ID), "autonomy_list", json.RawMessage(`{}`))
	if listErr {
		t.Errorf("autonomy_list failed where a catalog read was expected: %s", listContent)
	}
	if !strings.Contains(listContent, gres.Grant.ID) {
		t.Errorf("the catalog read does not carry the outward grant %s — it is not the surface the socket shows", gres.Grant.ID)
	}

	// --- ROW 13: the narrow surface, allowOutward true -------------------------
	walkRow(t, 13, "the narrow surface with allowOutward:true: the forced-attended transform holds (no policy consulted, no pin row), the outward actions run on the legacy flag, and merge is denied with the allowOutward clause")
	if err := os.WriteFile(filepath.Join(w.ws, "walk-13.txt"), []byte("work walk-13\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	narrowed := narrow.TriggerParamsFor(map[string]any{
		"blueprint":     "autonomy-walk",
		"workspaceRoot": w.ws,
		"goal":          "the narrow walk",
		"attended":      true,
		"allowOutward":  true,
		"with": map[string]any{
			"task":           "walk",
			"task_title":     "walk 13",
			"commit_message": "walk 13: the work file",
			"pr_summary":     "the walk 13 work file",
		},
	})
	if narrowed["attended"] != false {
		t.Fatalf("TriggerParamsFor left attended=%v — the narrow surface forces attended=false", narrowed["attended"])
	}
	if narrowed["allowOutward"] != true {
		t.Fatalf("TriggerParamsFor dropped allowOutward — the transform passes it through verbatim")
	}
	raw13, err := json.Marshal(narrowed)
	if err != nil {
		t.Fatalf("marshal the narrow start: %v", err)
	}
	res13, err := w.d.Table.Dispatch(ctx, w.conn, protocol.MethodLoopStart, raw13)
	if err != nil {
		t.Fatalf("loop.start: %v", err)
	}
	m13, ok := res13.(map[string]any)
	if !ok {
		t.Fatalf("loop.start returned %T, want the start map", res13)
	}
	loop13, _ := m13["id"].(string)
	if loop13 == "" {
		t.Fatalf("loop.start returned no loop id: %v", m13)
	}
	awaitSettled(t, w.store, loop13)
	st13 := w.loopState(t, loop13)
	att, err := w.store.LoopAttended(ctx, loop13)
	if err != nil || att == nil || *att != false {
		t.Errorf("the narrow-surface loop's attended is %v (err %v), want declared false", att, err)
	}
	outward13, err := w.store.LoopAllowOutward(ctx, loop13)
	if err != nil || !outward13 {
		t.Errorf("the narrow-surface loop's allow_outward is %v (err %v), want true", outward13, err)
	}
	if _, pinOK13, err := w.store.ReadAutonomyPin(loop13); err != nil || pinOK13 {
		t.Errorf("the socket start wrote a pin row (ok=%v) — only StartFromTrigger resolves autonomy (no policy consulted)", pinOK13)
	}
	if st13.Status != "waiting" {
		t.Fatalf("the narrow-surface loop is %s (reason %q), want waiting at the ship gate — the outward actions ran on the legacy flag", st13.Status, st13.Runtime.LastReason)
	}
	answerShip(t, w, loop13)
	st13 = w.loopState(t, loop13)
	if st13.Status != "blocked" {
		t.Fatalf("after the gate the narrow-surface loop is %s, want blocked at forge.merge", st13.Status)
	}
	want13 := "forge.merge denied — effect class 'irreversible' resolves to deny (loop is unattended; allowOutward does not grant it)"
	if !strings.Contains(st13.Runtime.LastReason, want13) {
		t.Errorf("the narrow-surface merge denial is not the legacy allowOutward clause:\n got %q\nwant %q", st13.Runtime.LastReason, want13)
	}
	t.Logf("row 13: %q", st13.Runtime.LastReason)
}

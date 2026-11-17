package daemon

// mergequeue_conflict_test.go — the conflict lane: the ask (attended and
// unattended), the resolution loop the resolve answer starts, the abort
// that stops the landing instead, and the re-verification of the head the
// resolution produced (a resolved merge was never verified — MQ11 again).

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// clash stands the conflict fixture: the feature writes shared.md one
// way, the base writes it the other, and the queue's catch-up is the
// merge that discovers it.
func (e *mqEnv) clash(t *testing.T, name string) string {
	t.Helper()
	f := e.conflictFeature(t, name)
	e.conflictBaseMove(t)
	return f
}

// conflictQuestion reads the step-less gate question the queue raised.
func (e *mqEnv) conflictQuestion(t *testing.T, slug string) store.Question {
	t.Helper()
	q, ok, err := e.s.LatestLoopGateQuestion(e.ws, slug)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("no gate question stands on %s\nloops: %s\nlog:\n%s", slug, e.dumpLoops(), e.w.logLines())
	}
	if q.StepID != nil {
		t.Fatalf("the conflict question at %s is a step question (engine's, not the queue's): %+v", slug, q)
	}
	return q
}

// behaviour 4 + 5 (the resolve leg): the attended conflict ASKS, the
// resolve answer starts the resolution loop, the resolved head rides
// re-verification back to ready, and the ending SHA is the RESOLVED one
// — never the pre-conflict feature head.
// mqEventDetailHasPaths proves the conflicted transition recorded the
// paths (SEC-033: paths only — the ledger half the UI reads).
func mqEventDetailHasPaths(t *testing.T, e *mqEnv, id string) bool {
	t.Helper()
	evs, err := e.s.ListMergeQueueEvents(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.ToStatus == "conflicted" && strings.Contains(ev.Detail, "conflict_paths") {
			return true
		}
	}
	return false
}

func TestConflictAttendedAsksThenResolves(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "manual", "ask", 3)
	f := e.clash(t, "feat-clash")
	it := e.enqueue(t, f)

	got := e.waitFor(t, it.ID, "conflicted")
	if !mqEventDetailHasPaths(t, e, it.ID) {
		t.Fatalf("the conflicted transition carried no conflict paths in its event detail\nlog:\n%s", e.w.logLines())
	}
	q := e.conflictQuestion(t, got.LandingLoopSlug)
	if q.Answered() {
		t.Fatalf("the queue's conflict question stands already answered: %+v", q)
	}
	if !strings.Contains(q.Body, it.ID) || !strings.Contains(q.Body, `Answer "resolve"`) {
		t.Fatalf("the question does not name the item or the words: %q", q.Body)
	}

	// The person says resolve. The resolution loop's model step fixes
	// the worktree through the scripted seam; conclude, verify, and the
	// queue walk back through verifying to ready.
	e.resolveScript = func(t *testing.T, dir string) { mqResolveBothSides(t, dir) }
	e.answerGate(t, e.loopIDFor(t, got.LandingLoopSlug), "resolve")

	got = e.waitFor(t, it.ID, "ready")
	if got.ResolveLoopSlug == "" {
		t.Fatal("resolved without a resolve-loop stamp")
	}
	want := strings.TrimSpace(mustGitOut(t, e.ws, "rev-parse", got.IntegrationBranch))
	if want == "" || got.VerifiedSHA != want {
		t.Fatalf("verified sha %q is not the resolved integration tip %q", got.VerifiedSHA, want)
	}
	if got.VerifiedSHA == got.HeadSHA {
		t.Fatal("the resolved head equals the pre-conflict feature head — nothing was integrated")
	}
	// Both sides survived the resolution in the tree the merge ends on.
	shared := mustGitOut(t, e.ws, "show", got.IntegrationBranch+":shared.md")
	if !strings.Contains(shared, "base side") || !strings.Contains(shared, "feature side") {
		t.Fatalf("the resolved shared.md lost a side:\n%s", shared)
	}
	if e.mergeCalls() != 0 {
		t.Fatalf("manual mode merged after the conflict (%d)", e.mergeCalls())
	}
}

// behaviour 5 (the abort leg): the abort word stops the landing loops
// NOW and the item keeps its conflict visible — stopped, not silently
// resolved, and nothing merged.
func TestConflictAttendedAbortStopsLanding(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "manual", "ask", 3)
	f := e.clash(t, "feat-abort")
	it := e.enqueue(t, f)

	got := e.waitFor(t, it.ID, "conflicted")
	e.answerGate(t, e.loopIDFor(t, got.LandingLoopSlug), "abort")
	e.settle(t)

	got = e.waitFor(t, it.ID, "failed")
	if got.ReasonKind != "conflict" {
		t.Fatalf("abort ended failed(%s), want the abort's own word (MQ5: conflict)", got.ReasonKind)
	}
	if got.ResolveLoopSlug != "" {
		t.Fatalf("abort started a resolution loop: %s", got.ResolveLoopSlug)
	}
	st, ok := e.loopStateFor(t, got.LandingLoopSlug)
	// A blocked loop parks with no process — stopped already; aborted or
	// removed is the engine's own stop. Anything still RUNNING fails.
	if ok && st.Status != "blocked" && st.Status != "aborted" && st.Status != "cancelled" && st.Status != "removed" && st.Status != "failed" {
		t.Fatalf("abort left the landing loop %s — the stop was not obeyed", st.Status)
	}
	if e.mergeCalls() != 0 {
		t.Fatalf("an aborted landing merged %d times", e.mergeCalls())
	}
}

// behaviour 6 + the ask policy's honesty: UNATTENDED with policy ask
// still raises the question (there is no other door) and holds — the
// queue never resolves by itself, never block-forgets, and the question
// carries ids, enums and a count, never hunks (SEC-033).
func TestConflictUnattendedAskHoldsAndNotifies(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "manual", "ask", 3)
	f := e.clash(t, "feat-quiet")
	it := e.enqueueAttended(t, f, false)

	got := e.waitFor(t, it.ID, "conflicted")
	q := e.conflictQuestion(t, got.LandingLoopSlug) // raised unattended too
	for _, leak := range []string{"base side", "feature side", "<<<<<<<", "=======", ">>>>>>>"} {
		if strings.Contains(q.Body, leak) || strings.Contains(strings.Join(q.Options, " "), leak) {
			t.Fatalf("the conflict question leaks conflict text %q (SEC-033): %q", leak, q.Body)
		}
	}
	e.resolveScript = nil // any model call would say: the queue resolved unasked
	e.settle(t)
	if again := e.item(t, it.ID); again.Status != "conflicted" {
		t.Fatalf("policy ask moved the unattended item to %s by itself", again.Status)
	}
	if e.mergeCalls() != 0 {
		t.Fatalf("an unresolved conflict merged %d times", e.mergeCalls())
	}
}

// behaviour 7: policy resolve needs nobody — the resolution loop starts
// on the reactor's own initiative and the item rides back to ready.
func TestConflictPolicyResolveNeedsNoPerson(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "manual", "resolve", 3)
	f := e.clash(t, "feat-autoresolve")
	it := e.enqueueAttended(t, f, false)

	e.resolveScript = func(t *testing.T, dir string) { mqResolveBothSides(t, dir) }
	got := e.waitFor(t, it.ID, "ready")
	if got.ResolveLoopSlug == "" {
		t.Fatal("policy resolve never started a resolution loop")
	}
	// No conflict question was ever asked (the step-less kind is the
	// queue's; a step gate is the engine's own, answered by nobody).
	if q, ok, err := e.s.LatestLoopGateQuestion(e.ws, got.LandingLoopSlug); err != nil {
		t.Fatal(err)
	} else if ok && q.StepID == nil && strings.Contains(strings.Join(q.Options, ","), "resolve") {
		t.Fatalf("policy resolve raised a conflict question anyway — the policy said the person is not needed: %+v", q)
	}
	// Runs record the root the REQUEST came in on (the integration
	// worktree for a queue lane), so the re-verification claim is read
	// by SHA — the binding the ready state stands on.
	runs, err := e.s.ListVerifyRuns(store.VerifyRunFilter{HeadSHA: got.VerifiedSHA, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) == 0 {
		t.Fatalf("the resolved head %s reached ready with ZERO verify runs recorded — re-verification is the point", got.VerifiedSHA)
	}
	if runs[0].State != "pass" {
		t.Fatalf("the run bound to the resolved head reads %s, not pass", runs[0].State)
	}
}

// behaviour 6's binding: after a resolution the item goes through
// verifying AGAIN, and the ready state stands on the RESOLVED head — not
// on the verification the item earned before the conflict. Landing on
// the pre-conflict verification would land code the gates never saw.
func TestResolveReverifiesTheNewHead(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "manual", "ask", 3)
	f := e.clash(t, "feat-reverify")
	it := e.enqueue(t, f)
	got := e.waitFor(t, it.ID, "conflicted")
	e.resolveScript = func(t *testing.T, dir string) { mqResolveBothSides(t, dir) }
	e.answerGate(t, e.loopIDFor(t, got.LandingLoopSlug), "resolve")
	ready := e.waitFor(t, it.ID, "ready")
	if ready.HeadSHA == "" || ready.VerifiedSHA == "" || ready.IntegrationSHA == "" {
		t.Fatalf("ready without the binding facts: %+v", ready)
	}
	if ready.VerifiedSHA == ready.HeadSHA {
		t.Fatalf("verified_sha still names the enqueue head %s — the resolution never moved the verification", ready.VerifiedSHA)
	}
	if ready.VerifiedSHA != ready.IntegrationSHA {
		t.Fatalf("verified_sha %s is not the integration sha %s the re-verification rode", ready.VerifiedSHA, ready.IntegrationSHA)
	}
	runs, err := e.s.ListVerifyRuns(store.VerifyRunFilter{HeadSHA: ready.VerifiedSHA, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) == 0 {
		t.Fatalf("no verify run is bound to the resolved head %s", ready.VerifiedSHA)
	}
	for _, r := range runs {
		if r.State != "pass" {
			t.Fatalf("the run bound to the resolved head reads %s, not pass", r.State)
		}
	}
}

package daemon

// mergequeue_harden_test.go — the properties the state machine cannot
// fake: recovery at every kill point (behaviour 9), the stop contract
// (behaviours 10 and 11), one-merge-per-item under concurrent reconciles,
// the SEC-033 hunk ban read as a scan, and the shipped blueprints'
// contracts.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/landingseed"
	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/store"
)

// TestRecoveryAtEveryState is behaviour 9: the daemon dies at every
// interesting state and the reopened reactor re-derives the RIGHT next
// action from rows alone — no double merge, no lost loop, no forgotten
// resolution.
func TestRecoveryAtEveryState(t *testing.T) {
	t.Run("queued-no-loop", func(t *testing.T) {
		e := newMQEnv(t)
		e.grant(t)
		e.configure(t, "manual", "ask", 3)
		it := e.enqueue(t, e.feature(t, "feat-rec-queued", ""))
		e.killAndReopen(t)
		e.settle(t)
		if _, ok := e.loopStateFor(t, e.item(t, it.ID).LandingLoopSlug); !ok {
			t.Fatalf("the reopened reactor never started the landing loop\n%s\nlog:\n%s", e.dump(), e.w.logLines())
		}
	})

	t.Run("catching-up-no-loop", func(t *testing.T) {
		e := newMQEnv(t)
		e.grant(t)
		e.configure(t, "manual", "ask", 3)
		it := e.enqueue(t, e.feature(t, "feat-rec-catch", ""))
		svc := e.svc()
		if _, err := svc.Transition(it.ID, mergequeue.StatusCatchingUp, store.TransitionFacts{}, mqMergeActor(), it); err != nil {
			t.Fatal(err)
		}
		e.killAndReopen(t)
		e.settle(t)
		if _, ok := e.loopStateFor(t, e.item(t, it.ID).LandingLoopSlug); !ok {
			t.Fatalf("catching_up with no loop survived the reopen unfixed\n%s\nlog:\n%s", e.dump(), e.w.logLines())
		}
	})

	t.Run("merging-already-merged", func(t *testing.T) {
		e := newMQEnv(t)
		e.grant(t)
		e.configure(t, "auto", "ask", 3)
		f := e.feature(t, "feat-rec-merged", "")
		it := e.enqueueAttended(t, f, false)
		svc := e.svc()
		// The row is walked to merging the LEGAL way, facts and all —
		// then the PR is merged on the forge by a person and the daemon
		// dies mid-landing.
		sha := it.HeadSHA
		tf := store.TransitionFacts{IntegrationBranch: "integrate/" + it.ID, IntegrationSHA: sha, VerifiedSHA: sha}
		if _, err := svc.Transition(it.ID, mergequeue.StatusCatchingUp, tf, mqMergeActor(), it); err != nil {
			t.Fatal(err)
		}
		got := e.item(t, it.ID)
		if _, err := svc.Transition(it.ID, mergequeue.StatusVerifying, store.TransitionFacts{}, mqMergeActor(), got); err != nil {
			t.Fatal(err)
		}
		got = e.item(t, it.ID)
		n := 41
		if _, err := svc.Transition(it.ID, mergequeue.StatusReady, store.TransitionFacts{PRNumber: &n}, mqMergeActor(), got); err != nil {
			t.Fatal(err)
		}
		got = e.item(t, it.ID)
		if _, err := svc.Transition(it.ID, mergequeue.StatusMerging, store.TransitionFacts{}, mqMergeActor(), got); err != nil {
			t.Fatal(err)
		}
		// The person lands PR #41 on the forge (through the fixture's
		// git-visible forge, as a real merge moves a real base).
		e.fake.SeedPR(forge.PR{Number: n, Head: f, BaseRef: "main"})
		if _, err := e.forger.MergePR(context.Background(), n, "squash"); err != nil {
			t.Fatal(err)
		}
		before := e.mergeCalls()
		e.killAndReopen(t)
		e.settle(t)
		got = e.waitFor(t, it.ID, "merged")
		pr, err := e.forger.ReadPR(context.Background(), n)
		if err != nil {
			t.Fatal(err)
		}
		if got.LandedSHA != pr.MergeSHA {
			t.Fatalf("merged with landed_sha %q, the forge's %q", got.LandedSHA, pr.MergeSHA)
		}
		if after := e.mergeCalls(); after != before {
			t.Fatalf("the reopened queue merged again: %d calls, was %d (behaviour 9: the same PR is never merged twice)", after, before)
		}
	})

	t.Run("conflicted-no-resolve-loop", func(t *testing.T) {
		e := newMQEnv(t)
		e.grant(t)
		e.configure(t, "manual", "resolve", 3)
		e.resolveScript = func(t *testing.T, dir string) { mqResolveBothSides(t, dir) }
		it := e.enqueueAttended(t, e.feature(t, "feat-rec-conflict", ""), false)
		svc := e.svc()
		if _, err := svc.Transition(it.ID, mergequeue.StatusCatchingUp, store.TransitionFacts{}, mqMergeActor(), it); err != nil {
			t.Fatal(err)
		}
		got := e.item(t, it.ID)
		if _, err := svc.Transition(it.ID, mergequeue.StatusConflicted,
			store.TransitionFacts{ConflictPaths: []string{"shared.md"}}, mqMergeActor(), got); err != nil {
			t.Fatal(err)
		}
		e.killAndReopen(t)
		e.settle(t)
		got = e.item(t, it.ID)
		if _, ok := e.loopStateFor(t, "resolve-"+got.ID); !ok {
			t.Fatalf("policy resolve forgot its resolution loop across the restart\n%s\nlog:\n%s", e.dump(), e.w.logLines())
		}
	})

	// The three live kill points: the daemon dies mid-flight (after the
	// worktree add, at verification, and waiting at ready) and the
	// restarted reactor + engine converge the item to the SAME end a
	// run without the crash reaches — the walk home is the person's
	// ordinary walk, unchanged by the restart.
	for _, kill := range []struct{ name, at string }{
		{"catching-up-after-worktree", "catching_up"},
		{"verifying", "verifying"},
		{"ready-waiting", "ready"},
	} {
		kill := kill
		t.Run(kill.name, func(t *testing.T) {
			e := newMQEnv(t)
			e.grant(t)
			e.configure(t, "manual", "ask", 3)
			it := e.enqueue(t, e.feature(t, "feat-rec-"+kill.name, ""))
			if kill.at == "verifying" {
				// verifying is a PASSING state, not a resting one — a
				// warm runner can cross it inside one poll. Accept
				// ready: then the kill lands one rung later and this
				// subtest re-proves the ready rung's convergence. The
				// table's claim is CONVERGENCE, not the row's instant.
				e.waitFor(t, it.ID, "verifying", "ready")
			} else {
				e.waitFor(t, it.ID, kill.at)
			}
			if kill.at == "catching_up" {
				// The kill lands AFTER the ledger's worktree add.
				deadline := time.Now().Add(30 * time.Second)
				for mqWorktreeGone(t, e.ws, it.ID) && time.Now().Before(deadline) {
					e.d.reconcileMergeQueue(context.Background())
					e.waitQuiet(t)
					time.Sleep(50 * time.Millisecond)
				}
				if mqWorktreeGone(t, e.ws, it.ID) {
					t.Fatal("no integration worktree ever appeared to kill over")
				}
			}
			e.killAndReopen(t)
			if _, err := e.d.ReconcileLoops(context.Background()); err != nil {
				t.Fatal(err)
			}
			e.settle(t)
			mqWalkHome(t, e, it.ID)
			if e.mergeCalls() != 1 {
				t.Fatalf("%d merge calls across the restart, want exactly one", e.mergeCalls())
			}
		})
	}

	t.Run("merging-after-the-PR-before-the-forge", func(t *testing.T) {
		// The crash rung the spec cares about: the landing loop is gone,
		// the PR is open, the merge has NOT happened. The restarted
		// reactor self-heals a fresh loop; the loop replays cold (the
		// published integration attaches, the gates re-run, the PR is
		// REUSED not reopened) and the grants carry the landing home —
		// exactly one merge, exactly one PR.
		e := newMQEnv(t)
		e.grant(t)
		e.configure(t, "auto", "ask", 3)
		f := e.feature(t, "feat-rec-merging", "")
		it := e.enqueueAttended(t, f, false)
		mqForceToMerging(t, e, it, f, 42)
		e.killAndReopen(t)
		if _, err := e.d.ReconcileLoops(context.Background()); err != nil {
			t.Fatal(err)
		}
		e.settle(t)
		got := e.waitFor(t, it.ID, "merged")
		if e.mergeCalls() != 1 {
			t.Fatalf("%d merge calls, want the one the replay landed", e.mergeCalls())
		}
		if e.openPRCalls() != 0 {
			t.Fatalf("%d PR opens across the restart, want none: the open PR is REUSED, never reopened", e.openPRCalls())
		}
		pr, err := e.forger.ReadPR(context.Background(), 42)
		if err != nil || !pr.Merged || pr.MergeSHA != got.LandedSHA {
			t.Fatalf("landed_sha %s is not PR #42's merge: %+v %v", got.LandedSHA, pr, err)
		}
	})
}

// mqWalkHome is the attended person's full walk to a landing: approve
// the publish, approve the PR, demand the land, say merge-now at the
// land-check, approve the landing. (T8 fuses the gate answer and the
// demand into one command; T7 drives both halves by hand.)
func mqWalkHome(t *testing.T, e *mqEnv, id string) {
	t.Helper()
	got := e.waitFor(t, id, "ready")
	slug := got.LandingLoopSlug
	e.answerGate(t, e.waitWaiting(t, slug), "approve") // git.publishIntegration
	e.answerGate(t, e.waitWaiting(t, slug), "approve") // forge.openLandingPR
	if n := e.waitForPR(t, id); n == nil || *n <= 0 {
		t.Fatal("no landing PR opened on the forge")
	}
	e.landRequest(id)
	e.answerGate(t, e.waitWaiting(t, slug), "merge-now") // the land-check gate
	e.answerGate(t, e.waitWaiting(t, slug), "approve")   // forge.land
	e.settle(t)
	got = e.waitFor(t, id, "merged")
	if got.LandedSHA == "" {
		t.Fatal("merged without landed_sha")
	}
}

// mqForceToMerging walks a row to `merging` the LEGAL way over a REAL
// integration branch (main + the feature merged, pushed to origin),
// stamps a seeded-but-unlanded PR, and returns the branch's sha. The
// row's facts are all true — recovery tests then kill and reopen and
// the git world answers exactly as it would for a live daemon.
func mqForceToMerging(t *testing.T, e *mqEnv, it store.MergeItem, feature string, prNumber int) string {
	t.Helper()
	clone := t.TempDir() + "/integrate"
	mqGit(t, filepath.Dir(clone), "clone", e.origin, clone)
	mqGit(t, clone, "config", "user.email", "queue@aeon.test")
	mqGit(t, clone, "config", "user.name", "Queue")
	branch := "integrate/" + it.ID
	mqGit(t, clone, "checkout", "-b", branch, "origin/main")
	mqGit(t, clone, "merge", "--no-ff", "-m", "integrate "+feature, "origin/"+feature)
	sha := mqGit(t, clone, "rev-parse", "HEAD")
	mqGit(t, clone, "push", "origin", branch)

	svc := e.svc()
	tf := store.TransitionFacts{IntegrationBranch: branch, IntegrationSHA: sha, VerifiedSHA: sha}
	if _, err := svc.Transition(it.ID, mergequeue.StatusCatchingUp, tf, mqMergeActor(), it); err != nil {
		t.Fatal(err)
	}
	got := e.item(t, it.ID)
	if _, err := svc.Transition(it.ID, mergequeue.StatusVerifying, store.TransitionFacts{}, mqMergeActor(), got); err != nil {
		t.Fatal(err)
	}
	got = e.item(t, it.ID)
	n := prNumber
	if _, err := svc.Transition(it.ID, mergequeue.StatusReady, store.TransitionFacts{PRNumber: &n}, mqMergeActor(), got); err != nil {
		t.Fatal(err)
	}
	got = e.item(t, it.ID)
	if _, err := svc.Transition(it.ID, mergequeue.StatusMerging, store.TransitionFacts{}, mqMergeActor(), got); err != nil {
		t.Fatal(err)
	}
	e.fake.SeedPR(forge.PR{Number: prNumber, Head: branch, BaseRef: "main", HeadSHA: sha})
	// L34/D10: the land leg reads the PR's reviews — the pre-seeded PR
	// arrives as reviewed as any PR this fixture has ever carried.
	e.fake.SeedReview(prNumber, "the queue walk-through", forge.VerdictApprove)
	return sha
}

// TestStoppedLandingFailsTheItem is behaviours 10 and 11: a stopped (or
// removed) landing loop ENDS the item with its own reason — the queue
// never silently restarts a landing a person stopped, and it never
// leaves the item parked forever on a loop that will not run.
func TestStoppedLandingFailsTheItem(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "manual", "ask", 3)
	it := e.enqueue(t, e.feature(t, "feat-stop", ""))
	got := e.waitFor(t, it.ID, "catching_up", "verifying", "ready")
	slug := got.LandingLoopSlug
	id := e.loopIDFor(t, slug)
	// The engine's own door: a loop parked on a question cannot be
	// stopped (answer it first) — a person's full stop is the cancel,
	// and LC4 says it is certain and free.
	if _, refusal, err := e.d.CancelLoop(context.Background(), id, store.Actor{Kind: "user", ID: "op"}, "the walk ends the landing"); err != nil {
		t.Fatal(err)
	} else if refusal != nil {
		t.Fatalf("the person's cancel was refused: %s", refusal.Detail)
	}
	e.settle(t)
	// failed(loop-stopped) where MQ5 allows it; removed(loop-stopped)
	// from the states whose only exit is removed (the event reason is
	// the word in both cases).
	got = e.waitFor(t, it.ID, "failed", "removed")
	if got.ReasonKind != "loop-stopped" {
		t.Fatalf("stopped landing ended %s(%s), want reason loop-stopped", got.Status, got.ReasonKind)
	}
	if e.mergeCalls() != 0 {
		t.Fatalf("a stopped landing merged %d times", e.mergeCalls())
	}
	// The stop is honoured, not fought: settle after the end starts
	// nothing new at the same slug.
	e.settle(t)
	if st, ok := e.loopStateFor(t, slug); ok && (st.Status == "running" || st.Status == "pending") {
		t.Fatalf("the queue restarted the stopped loop %s", slug)
	}
}

// TestDriverDecisionsAreSerialPerBase is the driver's serial-per-base
// promise read as an end-to-end property: reconciles may overlap freely
// (wake + tick + restart passes), yet each item's landing happens exactly
// once, in base order. (No -race: the standing P10 deviation.)
func TestDriverDecisionsAreSerialPerBase(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "auto", "ask", 3)
	f1 := e.feature(t, "feat-race-1", "")
	f2 := e.feature(t, "feat-race-2", "")
	i1 := e.enqueueAttended(t, f1, false)
	i2 := e.enqueueAttended(t, f2, false)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				e.d.reconcileMergeQueue(context.Background())
			}
		}()
	}
	wg.Wait()
	e.settle(t)

	for _, id := range []string{i1.ID, i2.ID} {
		if got := e.item(t, id); got.Status != "merged" {
			t.Fatalf("%s ended %s after concurrent reconciles: %s", id, got.Status, e.dump())
		}
	}
	if n := e.mergeCalls(); n != 2 {
		t.Fatalf("%d merges for two items — a PR was landed twice", n)
	}
	if n := e.openPRCalls(); n != 2 {
		t.Fatalf("%d PR opens for two items — a loop was started twice", n)
	}
}

// TestNoHunkTextAnywhere is SEC-033 as a scan, not a promise: run the
// conflict walk end to end, then read every byte the system kept — the
// database file, the ledger details, the loop states, the questions, the
// forge's call log — and refuse any of it to carry conflict CONTENT.
func TestNoHunkTextAnywhere(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "manual", "ask", 3)
	// behaviour 7's planted secrets: an AWS-looking key on one side, a
	// private-key block on the other. The hunks carry them into the
	// worktree (honest — the worktree is where a conflict LIVES); the
	// system — rows, events, logs, questions, the forge — must never.
	e.sideFeature = "feature claim: rotate token AKIAIOSFODNN7EXAMPLE in prod"
	e.sideBase = "base claim:\n-----BEGIN RSA PRIVATE KEY-----\nTUlHSU5BU0VOVElORUxOT1RTVE9SQUdFREFOSVdIRVJF\n-----END RSA PRIVATE KEY-----"
	f := e.clash(t, "feat-hunk")
	it := e.enqueue(t, f)
	got := e.waitFor(t, it.ID, "conflicted")
	e.resolveScript = func(t *testing.T, dir string) { mqResolveBothSides(t, dir) }
	e.answerGate(t, e.loopIDFor(t, got.LandingLoopSlug), "resolve")
	e.waitFor(t, it.ID, "ready")

	needles := []string{
		"base side", "feature side", "<<<<<<<", ">>>>>>>",
		"AKIAIOSFODNN7EXAMPLE", "BEGIN RSA PRIVATE KEY", "TUlHSU5BU0VOVElORUxOT1RTVE9SQUdFREFOSVdIRVJF",
	}
	scan := func(where, blob string) {
		for _, n := range needles {
			if strings.Contains(blob, n) {
				t.Fatalf("SEC-033: %s carries conflict text %q", where, n)
			}
		}
	}

	err := filepath.WalkDir(e.storeDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scan(fmt.Sprintf("database file %s", filepath.Base(path)), string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	evs, err := e.s.ListMergeQueueEvents(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		scan("queue event detail", ev.Detail+" "+ev.ReasonKind)
	}
	item := e.item(t, it.ID)
	raw, _ := json.Marshal(item)
	scan("the item row", string(raw))
	for _, slug := range []string{item.LandingLoopSlug, item.ResolveLoopSlug} {
		if slug == "" {
			continue
		}
		if st, ok := e.d.mqLoopState(context.Background(), e.ws, slug); ok {
			raw, _ := json.Marshal(st)
			scan("loop state "+slug, string(raw))
		}
		if q, ok, err := e.s.LatestLoopGateQuestion(e.ws, slug); err != nil {
			t.Fatal(err)
		} else if ok {
			scan("question body", q.Body+" "+strings.Join(q.Options, " "))
		}
	}
	for _, c := range e.fake.Calls() {
		scan("forge call "+c.Method, fmt.Sprintf("%v", c.Args))
	}
}

// TestLandingBlueprintsAreShippedAndHonest reads the SHIPPED seed set:
// exactly the two blueprints, parseable, the landing lane complete, and
// the resolution loop carrying zero outward actions (its policy-off is
// belt to these suspenders, not the whole argument).
func TestLandingBlueprintsAreShippedAndHonest(t *testing.T) {
	recs := landingseed.Source()
	if len(recs) != 2 {
		var names []string
		for _, r := range recs {
			names = append(names, r.Key)
		}
		t.Fatalf("the landing seed set carries %d records (%v), want the two blueprints", len(recs), names)
	}
	byID := map[string]blueprint.Blueprint{}
	for _, r := range recs {
		var env struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(r.Content, &env); err != nil {
			t.Fatalf("seed record %s: %v", r.Key, err)
		}
		bp, err := blueprint.Load([]byte(env.Content))
		if err != nil {
			t.Fatalf("blueprint %s does not parse: %v", r.Key, err)
		}
		byID[bp.ID] = bp
	}
	land, ok := byID["land-feature"]
	if !ok {
		t.Fatalf("no land-feature in the shipped set: %v", byID)
	}
	lanes := map[string]bool{}
	for _, l := range land.Lanes {
		lanes[l.ID] = true
	}
	for _, want := range []string{"catch-up", "verify", "publish", "pr", "land-check", "land"} {
		if !lanes[want] {
			t.Fatalf("the landing blueprint lost lane %s", want)
		}
	}
	res, ok := byID["resolve-conflict"]
	if !ok {
		t.Fatalf("no resolve-conflict in the shipped set")
	}
	for _, l := range res.Lanes {
		for _, name := range []string{l.Step.Action} {
			if strings.HasPrefix(name, "forge.") || strings.Contains(name, "publish") || strings.Contains(name, "push") {
				t.Fatalf("the resolution loop carries an outward action: %s/%s", l.ID, name)
			}
		}
	}
}

// behaviour 11, the loop's half: dequeue is a REMOVAL of an open item —
// the store's transaction releases the slot, and the queue-owned landing
// loop that was mid-flight must die through the loop's own door (S55's
// cancel), not keep catching up, publishing, or landing an item that no
// longer exists in the queue. The reactor's orphan sweep is what carries
// the cancel — the same path a crashed-mid-dequeue restart takes.
func TestDequeueStopsTheLandingLoop(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	// manual holds the loop on its land-check question — the live loop a
	// real dequeue overtakes mid-landing. In auto the unattended loop
	// would simply finish the landing before the test could dequeue.
	e.configure(t, "manual", "ask", 3)
	f := e.feature(t, "feat-dequeue-live", "")
	it := e.enqueueAttended(t, f, false)
	got := e.waitFor(t, it.ID, "catching_up", "verifying", "ready")
	slug := got.LandingLoopSlug
	if slug == "" {
		t.Fatalf("no landing loop was ever started to stop: %+v", got)
	}
	if _, err := e.svc().Dequeue(it.ID, store.MergeActor{Kind: "cli", ID: "test"}); err != nil {
		t.Fatal(err)
	}
	e.settle(t)
	term, err := e.s.MergeItemTerminal(it.ID)
	if err != nil || !term {
		t.Fatalf("dequeued item is not terminal: %v %v", term, err)
	}
	if st, ok := e.loopStateFor(t, slug); ok && st.Status != "aborted" && st.Status != "cancelled" {
		t.Fatalf("the dequeued item's loop %s lives on at %s — the sweep must cancel it", slug, st.Status)
	}
	if e.mergeCalls() != 0 {
		t.Fatalf("a dequeued item still reached the forge: %d merge calls", e.mergeCalls())
	}
}

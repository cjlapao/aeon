// engine_test.go — S61 T6's named proofs: the happy paths in both tree
// shapes, the digest's claim, the publish keys, the records' landing
// states, the proposal's marking, the secret law, and the derived status.
package intake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/store"
)

func TestCommitHappyPathSingleComponent(t *testing.T) {
	h := newHarness(t)
	id := h.openIntake("single")
	pid := h.propose(singlePlan(id))
	in := h.ask(id, pid, h.target(), "none", "", false)
	pv, _, err := h.eng.Plan(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.eng.Commit(context.Background(), in, testUser)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if res.RootID == "" || res.CharterRev != 1 || res.Records.Goals != 2 || res.Records.Items != 2 {
		t.Fatalf("result: %+v", res)
	}
	dir := filepath.Join(h.target(), "alpha")

	// The preview equals what was written — file for file, byte for byte
	// (the digest's promise, kept by the same render on both roads).
	if len(pv.Components) != 1 || len(pv.Components[0].Files) != 3 {
		t.Fatalf("preview: %+v", pv.Components)
	}
	for _, f := range pv.Components[0].Files {
		b, err := os.ReadFile(filepath.Join(dir, f.Path))
		if err != nil {
			t.Fatalf("preview promised %s: %v", f.Path, err)
		}
		if hexOf(b) != f.SHA256 {
			t.Fatalf("%s: written sha %s, preview sha %s", f.Path, hexOf(b), f.SHA256)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "Alpha\n\nthe one component\n" {
		t.Fatalf("README: %q", b)
	}

	// A real repository with a real commit carrying the intake id.
	head, err := h.eng.Git.LocalHEAD(context.Background(), dir)
	if err != nil || head == "" {
		t.Fatalf("head: %v %q", err, head)
	}
	subject, err := h.eng.Git.Run(context.Background(), dir, "log", "-1", "--format=%s")
	if err != nil || !strings.Contains(subject, id) {
		t.Fatalf("subject %q (err %v) must carry %s", subject, err, id)
	}

	// Registered, chartered by the intake, linked.
	comp, err := h.st.ComponentForRoot(dir)
	if err != nil || comp.Slug != "alpha" {
		t.Fatalf("component: %+v %v", comp, err)
	}
	revs, err := h.eng.Charter.Revisions(context.Background(), res.RootID)
	if err != nil || len(revs) != 1 || revs[0].Source != "intake" || revs[0].SourceRef != id ||
		revs[0].ActorKind != "user" || revs[0].ActorID != "carlos" {
		t.Fatalf("charter revisions: %+v err %v", revs, err)
	}
	row, err := h.st.GetIntake(id)
	if err != nil || row.RootID == "" || row.TargetPath != h.target() {
		t.Fatalf("intake row: %+v %v", row, err)
	}

	// The ledger: every step said once, publish and tree honestly skipped.
	want := []string{"verify-digest=done", "scaffold:alpha=done", "verify-git:alpha=done",
		"publish:alpha=skipped", "register:alpha=done", "tree=skipped",
		"charter=done", "records=done", "link=done"}
	c := h.census(id)
	if strings.Join(c.Ledger, ",") != strings.Join(want, ",") {
		t.Fatalf("ledger:\n %v\nwant\n %v", c.Ledger, want)
	}

	// The proposal is applied and the interview loop closed.
	if c.Proposal.Status != "committed" || c.Proposal.DecidedByID != "carlos" || c.Proposal.DecidedByKind != "user" {
		t.Fatalf("proposal: %+v", c.Proposal)
	}
	if h.closed != 1 {
		t.Fatalf("close-loop seam calls: %d", h.closed)
	}
	st, err := h.eng.Status(context.Background(), id)
	if err != nil || st.State != "committed" {
		t.Fatalf("status: %+v %v", st, err)
	}
}

func TestCommitHappyPathTreeOfThree(t *testing.T) {
	h := newHarness(t)
	id := h.openIntake("tree")
	pid := h.propose(treePlan(id))
	in := h.ask(id, pid, h.target(), "none", "", false)
	res, err := h.eng.Commit(context.Background(), in, testUser)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Three nodes: the promoted project root over head and kid.
	roots, err := h.st.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil || len(roots) != 1 || roots[0].ID != res.RootID {
		t.Fatalf("roots: %+v err %v", roots, err)
	}
	if roots[0].Name != "Proj Two" || roots[0].Root != "" {
		t.Fatalf("the project root: %+v — promote makes a repo-less root", roots[0])
	}
	kids, err := h.st.ListChildren(res.RootID)
	if err != nil || len(kids) != 2 {
		t.Fatalf("children: %+v %v", kids, err)
	}
	head, err := h.st.ComponentForRoot(filepath.Join(h.target(), "head"))
	if err != nil || head.ParentID != res.RootID {
		t.Fatalf("head: %+v %v", head, err)
	}

	// Charter only on the root — the components got none.
	if revs, err := h.eng.Charter.Revisions(context.Background(), res.RootID); err != nil || len(revs) != 1 {
		t.Fatalf("root charter: %+v %v", revs, err)
	}
	for _, kid := range kids {
		if revs, err := h.eng.Charter.Revisions(context.Background(), kid.ID); err != nil || len(revs) != 0 {
			t.Fatalf("%s carries %d charter revisions — the charter lives on the root", kid.Slug, len(revs))
		}
	}

	// Goals hang on the root; items sit on their own component.
	c := h.census(id)
	for _, g := range c.Goals {
		if g.ComponentID != res.RootID {
			t.Fatalf("goal %s hangs on %s, not the project root", g.ID, g.ComponentID)
		}
	}
	kidDir := filepath.Join(h.target(), "kid")
	items, err := h.st.ListRoadmapItems(kidDir, store.RoadmapItemFilters{})
	if err != nil || len(items) != 1 || items[0].Title != "Walk the demo" {
		t.Fatalf("kid items: %+v %v", items, err)
	}
	if c.Revisions != 1 || c.RootID != res.RootID {
		t.Fatalf("census: %+v", c)
	}
}

func TestCommitRefusesStaleDigest(t *testing.T) {
	h := newHarness(t)
	id := h.openIntake("stale")
	pid := h.propose(singlePlan(id))
	in := h.ask(id, pid, h.target(), "none", "", false)

	h.book.bump("base") // the world moved: the template shipped a new version

	_, err := h.eng.Commit(context.Background(), in, testUser)
	if err == nil || !strings.Contains(err.Error(), "changed since you saw it") {
		t.Fatalf("stale digest: %v", err)
	}
	// Nothing happened: no directory, no rows, no ledger, the ask is open.
	if _, err := os.Stat(filepath.Join(h.target(), "alpha")); !os.IsNotExist(err) {
		t.Fatal("the refused run wrote the target")
	}
	c := h.census(id)
	if len(c.Components) != 0 || c.Rows != 0 || len(c.Goals) != 0 {
		t.Fatalf("the refused run left rows behind: %+v", c)
	}
	if c.Proposal.Status != "proposed" {
		t.Fatalf("proposal: %+v", c.Proposal)
	}
}

func TestPublishNeedsKeysAndNeverUnattended(t *testing.T) {
	ctx := context.Background()
	t.Run("private without owner", func(t *testing.T) {
		h := newHarness(t)
		id := h.openIntake("pk1")
		pid := h.propose(singlePlan(id))
		in := CommitInput{IntakeID: id, ProposalID: pid, TargetPath: h.target(), Publish: "private"}
		if _, _, err := h.eng.Plan(ctx, in); err == nil || !strings.Contains(err.Error(), "owner") {
			t.Fatalf("plan: %v", err)
		}
	})
	t.Run("public without confirmation", func(t *testing.T) {
		h := newHarness(t)
		id := h.openIntake("pk2")
		pid := h.propose(singlePlan(id))
		in := CommitInput{IntakeID: id, ProposalID: pid, TargetPath: h.target(), Publish: "public", Owner: "acme"}
		if _, _, err := h.eng.Plan(ctx, in); err == nil || !strings.Contains(err.Error(), "confirm_public") {
			t.Fatalf("plan: %v", err)
		}
	})
	t.Run("setting off stops the run and the resume finishes it", func(t *testing.T) {
		h := newHarness(t)
		h.settingOn = false
		id := h.openIntake("pk3")
		pid := h.propose(singlePlan(id))
		in := h.ask(id, pid, h.target(), "private", "acme", false)
		_, err := h.eng.Commit(ctx, in, testUser)
		if err == nil || !strings.Contains(err.Error(), "publish:alpha") || !strings.Contains(err.Error(), "intake.allowRemoteCreate") {
			t.Fatalf("setting off: %v", err)
		}
		if len(h.fg.Calls()) != 0 {
			t.Fatalf("the forge saw calls with the setting off: %v", h.fg.Calls())
		}
		st, err := h.eng.Status(ctx, id)
		if err != nil || st.State != "committing" || st.FailedStep != "publish:alpha" {
			t.Fatalf("status: %+v %v", st, err)
		}
		// The person turns the setting on; the next run resumes exactly there.
		h.settingOn = true
		res, err := h.eng.Commit(ctx, in, testUser)
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
		if res.RootID == "" {
			t.Fatalf("resumed result: %+v", res)
		}
		var sawCreate bool
		for _, c := range h.fg.Calls() {
			sawCreate = sawCreate || c.Method == "CreateRepo"
		}
		if !sawCreate {
			t.Fatalf("no CreateRepo call after the keys opened: %v", h.fg.Calls())
		}
		if len(res.Components) == 0 || res.Components[0].RepoURL == "" {
			t.Fatalf("evidence: %+v", res.Components)
		}
	})
	t.Run("grant denied stops before any request", func(t *testing.T) {
		h := newHarness(t)
		h.grantErr = errors.New("autonomy: forge.createRepo is not granted for this root")
		id := h.openIntake("pk4")
		pid := h.propose(singlePlan(id))
		in := h.ask(id, pid, h.target(), "private", "acme", false)
		if _, err := h.eng.Commit(ctx, in, testUser); err == nil || !strings.Contains(err.Error(), "autonomy") {
			t.Fatalf("grant: %v", err)
		}
		if len(h.fg.Calls()) != 0 {
			t.Fatal("a denied grant still reached the forge")
		}
	})
}

func TestGoalsLandInReviewAndItemsAsIdea(t *testing.T) {
	h := newHarness(t)
	id := h.openIntake("land")
	pid := h.propose(singlePlan(id))
	in := h.ask(id, pid, h.target(), "none", "", false)
	if _, err := h.eng.Commit(context.Background(), in, testUser); err != nil {
		t.Fatal(err)
	}
	c := h.census(id)
	if len(c.Goals) != 2 || len(c.Items) != 2 {
		t.Fatalf("census: %d goals %d items", len(c.Goals), len(c.Items))
	}
	for _, g := range c.Goals {
		if g.ReviewState != "in_review" {
			t.Fatalf("goal %s is %s — GI14: nothing here approves", g.ID, g.ReviewState)
		}
		if g.SourceKind != "intake" || g.SourceRef != id {
			t.Fatalf("goal %s provenance: %s/%s", g.ID, g.SourceKind, g.SourceRef)
		}
	}
	// The goal that named an epic rides the epic THIS run created.
	epics, err := h.st.ListEpics(c.RootID)
	if err != nil || len(epics) != 1 {
		t.Fatalf("epics: %+v %v", epics, err)
	}
	byTitle := map[string]store.Goal{}
	for _, g := range c.Goals {
		byTitle[g.Title] = g
	}
	if g := byTitle["Commit lands"]; g.EpicID != epics[0].ID {
		t.Fatalf("goal epic: %q, created %q", g.EpicID, epics[0].ID)
	}
	if g := byTitle["Resume holds"]; g.EpicID != "" {
		t.Fatalf("epicless goal got an epic: %s", g.EpicID)
	}
	if c.Criteria != 2 {
		t.Fatalf("criteria: %d (goal one has two, goal two none)", c.Criteria)
	}
	for _, it := range c.Items {
		if it.Status != "idea" || it.Provenance != "intake" {
			t.Fatalf("item %s: %s/%s — ideas enter at idea, provenance intake", it.Title, it.Status, it.Provenance)
		}
		switch it.Title {
		case "Write the engine":
			if it.PriorityState != "known" || it.PriorityLabel == nil || *it.PriorityLabel != "should" {
				t.Fatalf("labeled priority: %+v", it)
			}
			if it.Epic == nil || *it.Epic != "e1" {
				t.Fatalf("the plan's epic key rides the epic column: %+v", it.Epic)
			}
			if it.EstimateMin == nil || it.EstimateMax == nil || it.EstimateBasis == nil ||
				*it.EstimateMin != 3 || *it.EstimateMax != 5 || *it.EstimateBasis != "intake plan" {
				t.Fatalf("estimate trio (D3): %+v", it)
			}
		case "Walk the demo":
			if it.PriorityState != "unknown" || it.PriorityLabel != nil {
				t.Fatalf("unranked item: %+v", it)
			}
			if it.EstimateMin != nil || it.EstimateMax != nil || it.EstimateBasis != nil {
				t.Fatalf("no estimate, no invented numbers: %+v", it)
			}
		}
	}
}

func TestProposalMarkedAppliedByCommitter(t *testing.T) {
	h := newHarness(t)
	id := h.openIntake("mark")
	pid := h.propose(singlePlan(id))
	in := h.ask(id, pid, h.target(), "none", "", false)
	if _, err := h.eng.Commit(context.Background(), in, testUser); err != nil {
		t.Fatal(err)
	}
	p, found, err := h.st.GetProposal(pid)
	if err != nil || !found {
		t.Fatal(err)
	}
	if p.Status != "committed" || p.AppliedError != "" {
		t.Fatalf("proposal: %+v", p)
	}
	if p.DecidedByKind != "user" || p.DecidedByID != "carlos" {
		t.Fatalf("the decider must be the COMMITTER, not the proposing agent: %+v", p)
	}
	if p.CreatedByID == p.DecidedByID {
		t.Fatal("proposer and decider are the same being — the test means they differ")
	}

	// A loop that will not close says so as a WARNING — the commit stands.
	h2 := newHarness(t)
	id2 := h2.openIntake("mark2")
	pid2 := h2.propose(singlePlan(id2))
	in2 := h2.ask(id2, pid2, h2.target(), "none", "", false)
	h2.eng.CloseLoop = func(context.Context) error { return errors.New("loop is not idle") }
	res, err := h2.eng.Commit(context.Background(), in2, testUser)
	if err != nil {
		t.Fatalf("a close-loop failure must not fail the commit: %v", err)
	}
	if res.Warning == "" || !strings.Contains(res.Warning, "not idle") {
		t.Fatalf("warning: %+v", res)
	}
}

func TestNoSecretInLedgerOrErrors(t *testing.T) {
	const sentinel = "gl0ria-99-outward-token"
	t.Setenv("AEON_TEST_SENTINEL", sentinel)
	h := newHarness(t)
	id := h.openIntake("secret")
	pid := h.propose(singlePlan(id))
	in := h.ask(id, pid, h.target(), "private", "acme", false)
	h.fg.FailMethod("CreateRepo", forge.NewMsg(forge.KindAuth, "the forge closed its door"))
	res, err := h.eng.Commit(context.Background(), in, testUser)
	if err == nil {
		t.Fatal("a failing publish must stop the run")
	}
	c := h.census(id)
	var blob strings.Builder
	blob.WriteString(err.Error())
	bj, _ := json.Marshal(res)
	blob.Write(bj)
	for _, s := range c.Ledger {
		blob.WriteString(s)
	}
	steps, err := h.st.IntakeSteps(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		blob.WriteString(s.Detail)
	}
	blob.WriteString(fmt.Sprint(c))
	if strings.Contains(blob.String(), sentinel) {
		t.Fatal("the engine leaked the environment's secret into its answers")
	}
	// And nothing under the target carries it either.
	if strings.Contains(fmt.Sprint(filesUnder(h.target())), sentinel) {
		t.Fatal("a scaffolded file carries the sentinel")
	}
}

func TestStatusIsDerived(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.openIntake("derive")

	st, err := h.eng.Status(ctx, id)
	if err != nil || st.State != "interviewing" {
		t.Fatalf("fresh: %+v %v", st, err)
	}
	pid := h.propose(singlePlan(id))
	st, _ = h.eng.Status(ctx, id)
	if st.State != "planned" || st.ProposalID != pid || st.ProposalStat != "proposed" {
		t.Fatalf("planned: %+v", st)
	}

	in := h.ask(id, pid, h.target(), "none", "", false)
	// A run interrupted by a crash (the hook) says committing, naming no
	// failure — the ledger's silence is the crash's shape.
	h.eng.Hook = func(step, phase string) error {
		if step == "charter" && phase == "before" {
			return errors.New("simulated crash")
		}
		return nil
	}
	if _, err := h.eng.Commit(ctx, in, testUser); err == nil {
		t.Fatal("the crash did not stop the run")
	}
	st, _ = h.eng.Status(ctx, id)
	if st.State != "committing" || st.FailedStep != "" {
		t.Fatalf("crashed: %+v", st)
	}

	// A FAILED step is named.
	h.eng.Hook = nil
	h.eng.RecordsFault = func(n int) error {
		if n == 1 {
			return errors.New("simulated mid-transaction death")
		}
		return nil
	}
	if _, err := h.eng.Commit(ctx, in, testUser); err == nil {
		t.Fatal("the fault did not stop the run")
	}
	st, _ = h.eng.Status(ctx, id)
	if st.State != "committing" || st.FailedStep != "records" {
		t.Fatalf("failed: %+v", st)
	}

	// Finish it: committed, and the failed word is history.
	h.eng.RecordsFault = nil
	if _, err := h.eng.Commit(ctx, in, testUser); err != nil {
		t.Fatalf("finish: %v", err)
	}
	st, _ = h.eng.Status(ctx, id)
	if st.State != "committed" || st.FailedStep != "" || st.RootID == "" {
		t.Fatalf("committed: %+v", st)
	}

	// A rejected plan with nothing else is an abandoned session.
	h2 := newHarness(t)
	id2 := h2.openIntake("derive2")
	pid2 := h2.propose(singlePlan(id2))
	if err := h2.st.DecideProposal(pid2, "rejected", testUser, "not now", h2.st.Now()); err != nil {
		t.Fatal(err)
	}
	st, _ = h2.eng.Status(ctx, id2)
	if st.State != "abandoned" {
		t.Fatalf("rejected: %+v", st)
	}
}

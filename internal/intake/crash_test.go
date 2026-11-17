// crash_test.go — GI13's crash matrix: for every position between two
// steps (and mid-`records` transaction), kill the run there, resume it,
// and demand the finished state equal the uninterrupted run's — same files,
// same rows, no duplicates. Plus the two laws the matrix leans on: a step
// recorded done whose evidence no longer holds REFUSES, and a partial
// failure keeps the repositories it really made.
package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

var errCrash = errors.New("simulated process death")

func TestCrashMatrix(t *testing.T) {
	ctx := context.Background()

	// The uninterrupted baseline.
	base := newHarness(t)
	id := base.openIntake("matrix")
	pid := base.propose(singlePlan(id))
	in := base.ask(id, pid, base.target(), "none", "", false)
	pv, _, err := base.eng.Plan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	stepList := pv.Steps
	baseRes, err := base.eng.Commit(ctx, in, testUser)
	if err != nil {
		t.Fatalf("baseline commit: %v", err)
	}
	baseCensus := base.census(id)
	if baseCensus.Rows == 0 || len(baseCensus.Goals) != 2 || baseCensus.Revisions != 1 {
		t.Fatalf("baseline census: %+v", baseCensus)
	}

	// Every between-step position, both sides of the step's work…
	type pos struct{ step, phase string }
	var positions []pos
	for _, s := range stepList {
		positions = append(positions, pos{s, "before"}, pos{s, "after"})
	}
	// …and mid-`records`, killed after the first goal of two is inserted.
	positions = append(positions, pos{"records:mid-tx", ""})

	for _, p := range positions {
		p := p
		t.Run(p.step+"/"+p.phase, func(t *testing.T) {
			h := newHarness(t)
			hid := h.openIntake("matrix")
			hpid := h.propose(singlePlan(hid))
			hin := h.ask(hid, hpid, h.target(), "none", "", false)
			if p.step == "records:mid-tx" {
				h.eng.RecordsFault = func(n int) error {
					if n == 1 {
						return errCrash
					}
					return nil
				}
			} else {
				h.eng.Hook = func(step, phase string) error {
					if step == p.step && phase == p.phase {
						return errCrash
					}
					return nil
				}
			}
			if _, err := h.eng.Commit(ctx, hin, testUser); err == nil {
				t.Fatal("the crash did not stop the run")
			}
			// The crash is not a failure: either the ledger says nothing
			// of a crash-killed step (between-step) or records says failed
			// (the transaction died and said so honestly).
			if p.step != "records:mid-tx" {
				steps, err := h.st.IntakeSteps(hid)
				if err != nil {
					t.Fatal(err)
				}
				for _, s := range steps {
					if s.Step == p.step && (p.phase == "before" || s.Outcome == "failed") {
						t.Fatalf("a crash at %s/%s wrote ledger row %s=%s — a crash says nothing",
							p.step, p.phase, s.Step, s.Outcome)
					}
				}
			}

			// Resume with nothing armed: the run completes.
			h.eng.Hook = nil
			h.eng.RecordsFault = nil
			res, err := h.eng.Commit(ctx, hin, testUser)
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if res.RootID == "" {
				t.Fatalf("resumed result: %+v", res)
			}
			c := h.census(hid)

			// Census equality with the uninterrupted run.
			if strings.Join(c.Files, "\n") != strings.Join(baseCensus.Files, "\n") {
				t.Fatalf("files differ:\n %v\n %v", c.Files, baseCensus.Files)
			}
			if strings.Join(collapsed(c.Ledger), ",") != strings.Join(collapsed(baseCensus.Ledger), ",") {
				t.Fatalf("ledger last-words differ:\n %v\n %v", collapsed(c.Ledger), collapsed(baseCensus.Ledger))
			}
			if len(c.Components) != len(baseCensus.Components) || len(c.Goals) != len(baseCensus.Goals) ||
				len(c.Items) != len(baseCensus.Items) || c.Criteria != baseCensus.Criteria ||
				c.Revisions != baseCensus.Revisions || c.Proposal.Status != baseCensus.Proposal.Status {
				t.Fatalf("row census differs:\n got %+v\n want %+v", c, baseCensus)
			}
			// Zero duplicates: every step's LAST word done/skipped, and no
			// step ever says done twice.
			seen := map[string]int{}
			for _, row := range c.Ledger {
				step, outcome, _ := strings.Cut(row, "=")
				if outcome == "done" {
					seen[step]++
				}
				_ = step
			}
			for step, n := range seen {
				if n > 1 {
					t.Fatalf("step %s ran to done %d times — the run duplicated a real effect", step, n)
				}
			}
			for _, row := range collapsed(c.Ledger) {
				_, outcome, _ := strings.Cut(row, "=")
				if outcome == "failed" {
					t.Fatalf("the finished ledger still carries a failure: %s", row)
				}
			}
			// Exactly one charter revision, exactly two goals, no repeated
			// item — the duplicate checks in the rows themselves.
			if baseRes.RootID == "" || c.RootID == "" {
				t.Fatal("no root on a completed run")
			}
		})
	}
}

// TestResumeRefusesOnUnexpectedState — a step recorded done whose evidence
// no longer holds is a refusal, not a repair: the run stops naming the
// step and the mismatch, and nothing is overwritten.
func TestResumeRefusesOnUnexpectedState(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.openIntake("tamper")
	pid := h.propose(singlePlan(id))
	in := h.ask(id, pid, h.target(), "none", "", false)

	// Stop the run just after the scaffold step is recorded done.
	h.eng.Hook = func(step, phase string) error {
		if step == "register:alpha" && phase == "before" {
			return errCrash
		}
		return nil
	}
	if _, err := h.eng.Commit(ctx, in, testUser); err == nil || !errors.Is(err, errCrash) {
		t.Fatalf("crash run: %v", err)
	}

	// A stray hand edits the scaffolded README.
	readme := filepath.Join(h.target(), "alpha", "README.md")
	if err := os.WriteFile(readme, []byte("tampered by a stray hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.eng.Hook = nil
	_, err := h.eng.Commit(ctx, in, testUser)
	if err == nil || !strings.Contains(err.Error(), "recorded done") ||
		!strings.Contains(err.Error(), "scaffold:alpha") || !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("refusal must name the step and the mismatch: %v", err)
	}
	// Nothing was repaired over the stranger's bytes.
	b, _ := os.ReadFile(readme)
	if string(b) != "tampered by a stray hand\n" {
		t.Fatalf("the resume overwrote unexpected state: %q", b)
	}

	// Put the bytes back; the same run now finishes (the file's sha is the
	// preview's, and the plan says so).
	os.WriteFile(readme, []byte("Alpha\n\nthe one component\n"), 0o644)
	res, err := h.eng.Commit(ctx, in, testUser)
	if err != nil {
		t.Fatalf("clean resume: %v", err)
	}
	if res.RootID == "" {
		t.Fatalf("result: %+v", res)
	}
}

// TestPartialFailureKeepsRealRepos — a step's honest failure stops the run
// and leaves what it really made: the first component stands as a real
// repository, registered, and the ledger says exactly where it stopped.
func TestPartialFailureKeepsRealRepos(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.openIntake("partial")
	p := treePlan(id)
	pid := h.propose(p)
	in := h.ask(id, pid, h.target(), "none", "", false)

	// The second component's target dir pre-exists, non-empty: CheckTarget
	// refuses there — the first component's steps have all run by then.
	stranger := filepath.Join(h.target(), "kid")
	if err := os.MkdirAll(stranger, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stranger, "note.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := h.eng.Commit(ctx, in, testUser)
	if err == nil || !strings.Contains(err.Error(), "scaffold:kid") {
		t.Fatalf("expected stop at scaffold:kid: %v", err)
	}

	// head is real: a git repository with its commit, registered.
	headDir := filepath.Join(h.target(), "head")
	head, err := h.eng.Git.LocalHEAD(ctx, headDir)
	if err != nil || head == "" {
		t.Fatalf("the first component's repository must stand: %v", err)
	}
	if comp, err := h.st.ComponentForRoot(headDir); err != nil || comp.Slug != "head" {
		t.Fatalf("registered head: %+v %v", comp, err)
	}
	// kid untouched, unregistered; nothing past the stop happened.
	if b, _ := os.ReadFile(filepath.Join(stranger, "note.txt")); string(b) != "mine\n" {
		t.Fatal("the stranger's file was disturbed")
	}
	if _, err := h.st.ComponentForRoot(stranger); err == nil {
		t.Fatal("kid was registered although it was never scaffolded")
	}
	st, _ := h.eng.Status(ctx, id)
	if st.State != "committing" || st.FailedStep != "scaffold:kid" {
		t.Fatalf("status must name where it stopped: %+v", st)
	}
	if c := h.census(id); len(c.Goals) != 0 || c.Revisions != 0 {
		t.Fatalf("nothing past the failure may exist: %+v", c)
	}

	// Remove the stranger dir; the SAME run resumes past the stop.
	os.RemoveAll(stranger)
	res, err := h.eng.Commit(ctx, in, testUser)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if res.RootID == "" {
		t.Fatalf("resumed result: %+v", res)
	}
}

// TestRecordsAreOneTransaction — the mid-transaction death: goals 1 is
// inserted, the transaction dies at goal 2, and NOTHING of the step
// survives — not the epic written first, not the criteria, not the item.
// The resume then files the whole set exactly once.
func TestRecordsAreOneTransaction(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.openIntake("tx")
	pid := h.propose(singlePlan(id))
	in := h.ask(id, pid, h.target(), "none", "", false)

	h.eng.RecordsFault = func(n int) error {
		if n == 1 { // die after the first goal, mid-transaction
			return errCrash
		}
		return nil
	}
	if _, err := h.eng.Commit(ctx, in, testUser); err == nil || !strings.Contains(err.Error(), "records") {
		t.Fatalf("expected the records step to fail: %v", err)
	}
	c := h.census(id)
	if len(c.Goals) != 0 || c.Criteria != 0 || len(c.Items) != 0 {
		t.Fatalf("the aborted transaction left rows: %d goals, %d criteria, %d items",
			len(c.Goals), c.Criteria, len(c.Items))
	}
	epics, err := h.st.ListEpics(c.RootID)
	if err == nil && len(epics) != 0 {
		t.Fatalf("the epic written before the death survived: %+v", epics)
	}
	st, _ := h.eng.Status(ctx, id)
	if st.FailedStep != "records" {
		t.Fatalf("status: %+v", st)
	}

	h.eng.RecordsFault = nil
	if _, err := h.eng.Commit(ctx, in, testUser); err != nil {
		t.Fatalf("resume: %v", err)
	}
	c = h.census(id)
	if len(c.Goals) != 2 || c.Criteria != 2 || len(c.Items) != 2 {
		t.Fatalf("the resumed step must file the set exactly once: %+v", c)
	}
}

// TestKillMidScaffoldResumesOwnHalf walks the T12 live-walk finding: a
// kill -9 DURING scaffold:X (the crash matrix's between-step law has no
// answer for a death mid-step) leaves our own torn half with no ledger row,
// and CheckTarget lawfully refuses any non-empty target. The resume takes
// the half back — the preview's own bytes are the proof — and completes.
func TestKillMidScaffoldResumesOwnHalf(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.openIntake("torn")
	pid := h.propose(singlePlan(id))
	in := h.ask(id, pid, h.target(), "none", "", false)
	pv, _, err := h.eng.Plan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// The crash's scene: the directory made, the first previewed file's bytes
	// written, nothing else — exactly where Write could have been cut off.
	target := pv.Components[0].Target
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "README.md"), []byte("Alpha\n\nthe one component\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := h.eng.Commit(ctx, in, testUser)
	if err != nil {
		t.Fatalf("the run's own half stalled the resume: %v", err)
	}
	if res.RootID == "" {
		t.Fatalf("resumed result: %+v", res)
	}
	c := h.census(id)
	if len(c.Goals) != 2 || c.Criteria != 2 || len(c.Items) != 2 || c.Revisions != 1 {
		t.Fatalf("census after the take-back: %d goals, %d criteria, %d items, %d revisions",
			len(c.Goals), c.Criteria, len(c.Items), c.Revisions)
	}
	for _, f := range pv.Components[0].Files {
		b, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Fatalf("%s missing after the take-back: %v", f.Path, err)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			t.Fatalf("%s does not hold the preview's bytes after the take-back", f.Path)
		}
	}
	steps, err := h.st.IntakeSteps(id)
	if err != nil {
		t.Fatal(err)
	}
	taken := false
	for _, s := range steps {
		if s.Step == "scaffold:alpha" && s.Outcome == "done" && strings.Contains(s.Detail, "taken back") {
			taken = true
		}
	}
	if !taken {
		t.Fatal("the ledger does not say the half was taken back")
	}
}

// TestKillMidScaffoldStrayBytesStillRefuse is its twin: a half that is NOT
// ours — an extra path the plan never previewed — must keep CheckTarget's
// refusal standing, byte for byte, and the stranger's file must survive
// (GI9's forbidden repair stays forbidden).
func TestKillMidScaffoldStrayBytesStillRefuse(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.openIntake("stray")
	pid := h.propose(singlePlan(id))
	in := h.ask(id, pid, h.target(), "none", "", false)
	pv, _, err := h.eng.Plan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	target := pv.Components[0].Target
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "MINE.txt"), []byte("a stray hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.eng.Commit(ctx, in, testUser); err == nil || !strings.Contains(err.Error(), "is not empty") {
		t.Fatalf("the stray half was not refused with CheckTarget's sentence: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(target, "MINE.txt")); err != nil {
		t.Fatalf("the refusal touched the stranger's bytes: %v", err)
	}
	st, err := h.eng.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.FailedStep != "scaffold:alpha" {
		t.Fatalf("failed step: %+v", st)
	}
}

// TestReEntryAdoptsTheCreateThatLandedWithoutItsRow is the register twin of
// the take-back tests: the T12 walk's kill -9 at register:shop-jobs left the
// component registered with no ledger row, and every re-run died on
// "already registered". The create is adoptable when verifyRegister's own
// read passes — same root, same planned slug.
func TestReEntryAdoptsTheCreateThatLandedWithoutItsRow(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.openIntake("half-reg")
	pid := h.propose(singlePlan(id))
	in := h.ask(id, pid, h.target(), "none", "", false)
	pv, _, err := h.eng.Plan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	pc := pv.Components[0]
	if err := os.MkdirAll(pc.Target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc.Target, "README.md"), []byte("Alpha\n\nthe one component\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w, err := h.st.CreateComponent(store.NewComponent{Name: "Alpha", Slug: "alpha", Root: pc.Target}); err != nil || !w.OK() {
		t.Fatalf("planting the landed create: %v %+v", err, w)
	}
	if _, err := h.eng.Commit(ctx, in, testUser); err != nil {
		t.Fatalf("re-entry refused an adoption it could prove: %v", err)
	}
	comps, err := h.st.ListComponents(store.ComponentFilter{IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range comps {
		if c.Slug == "alpha" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the adoption made a second component row: %d", n)
	}
	steps, err := h.st.IntakeSteps(id)
	if err != nil {
		t.Fatal(err)
	}
	adopted := false
	for _, st := range steps {
		if st.Step == "register:alpha" && st.Outcome == "done" && strings.Contains(st.Detail, "adopted") {
			adopted = true
		}
	}
	if !adopted {
		t.Fatal("the ledger does not record the adoption")
	}
}

// TestForeignRegistrationStillRefusesAdoption keeps the refusal honest: a
// DIFFERENT slug on the planned root is not this run's create, and the
// create's refusal must stand with its own sentence.
func TestForeignRegistrationStillRefusesAdoption(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.openIntake("foreign")
	pid := h.propose(singlePlan(id))
	in := h.ask(id, pid, h.target(), "none", "", false)
	pv, _, err := h.eng.Plan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	pc := pv.Components[0]
	if w, err := h.st.CreateComponent(store.NewComponent{Name: "Intruder", Slug: "intruder", Root: pc.Target}); err != nil || !w.OK() {
		t.Fatalf("planting the foreign component: %v %+v", err, w)
	}
	if _, err := h.eng.Commit(ctx, in, testUser); err == nil || !strings.Contains(err.Error(), "component.create refused") {
		t.Fatalf("a foreign registration was not refused with the create's sentence: %v", err)
	}
}

// TestTreeAdoptsThePromoteThatLandedWithoutItsRow is the tree step's twin:
// the project row exists beside a first component already its child — the
// promote landed, its word did not — and re-entry must adopt that project,
// never promote a second one.
func TestTreeAdoptsThePromoteThatLandedWithoutItsRow(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.openIntake("half-tree")
	pid := h.propose(treePlan(id))
	in := h.ask(id, pid, h.target(), "none", "", false)
	pv, _, err := h.eng.Plan(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	half := map[string]string{ // the crash's own halves: previewed bytes only
		pv.Components[0].Target: "Head\n\nthe head repo\n",
		pv.Components[1].Target: "Kid\n\na child repo\n",
	}
	planted := map[string]string{}
	for i, pc := range pv.Components {
		if err := os.MkdirAll(pc.Target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pc.Target, "README.md"), []byte(half[pc.Target]), 0o644); err != nil {
			t.Fatal(err)
		}
		w, err := h.st.CreateComponent(store.NewComponent{Name: []string{"Head", "Kid"}[i], Slug: pc.Slug, Root: pc.Target})
		if err != nil || !w.OK() {
			t.Fatalf("planting %s: %v %+v", pc.Slug, err, w)
		}
		planted[pc.Slug] = w.Component.ID
	}
	// The promote landed too: head is already a child of a project row.
	if w, err := h.st.PromoteComponent(planted["head"], store.PromoteInput{Name: "Proj Two"}); err != nil || !w.OK() {
		t.Fatalf("landing the promote: %v %+v", err, w)
	}
	res, err := h.eng.Commit(ctx, in, testUser)
	if err != nil {
		t.Fatalf("the tree stalled on a promote it could prove: %v", err)
	}
	if res.RootID == "" {
		t.Fatalf("result: %+v", res)
	}
	comps, err := h.st.ListComponents(store.ComponentFilter{IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	projects := 0
	for _, c := range comps {
		if c.Name == "Proj Two" {
			projects++
		}
	}
	if projects != 1 {
		t.Fatalf("the adoption made a second project row: %d", projects)
	}
	steps, err := h.st.IntakeSteps(id)
	if err != nil {
		t.Fatal(err)
	}
	adopted := false
	for _, st := range steps {
		if st.Step == "tree" && st.Outcome == "done" && strings.Contains(st.Detail, "adopted") {
			adopted = true
		}
	}
	if !adopted {
		t.Fatal("the ledger does not record the tree adoption")
	}
}

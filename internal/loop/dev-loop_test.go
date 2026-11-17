package loop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/store"
)

// S17 T7 — the executing half of the ladder's authorship: THE shipped
// dev-loop.yaml, run by the real engine against the fake forge. The
// structural rulings (one gate, legal sets in every mode, no new
// mechanism, --strict) live in internal/blueprint/dev-loop_test.go; this
// file is D13: every lane runs, in order, and reaches done.
//
// Two findings this walk records rather than repairs (N1/D15 — the
// missing capability is information about S8–S16, not work for S17):
//
//	F4  `flags.*` has NO production writer: collectFields initialises each
//	    named flag to false and nothing in engine, daemon or agentsvc ever
//	    sets one true. In production every conditional lane is a permanent
//	    not-needed. The harness sets them the way the orchestrator must
//	    one day, and the not-needed path gets its own test.
//	F5  `sets:` is a declaration with no engine consumer — task rows move
//	    through SetTaskStatus via the orchestrator (the T4 finding again),
//	    so the ladder's sets values are validator-enforced and runtime-
//	    inert at this layer. The walk therefore asserts the ROWS the
//	    engine does own (phases, step states, counters), not task statuses.

const devLoopPath = "../blueprint/blueprints/dev-loop.yaml"

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// devRoot builds what the git actions demand: a real repo on `main`, one
// commit, and a bare origin the branch can push to. The forge stays fake.
func devRoot(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	origin := filepath.Join(base, "origin.git")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "--bare", origin).CombinedOutput(); err != nil {
		t.Fatalf("bare init: %v\n%s", err, out)
	}
	gitRun(t, root, "-c", "init.defaultBranch=main", "init")
	gitRun(t, root, "config", "user.email", "dev-loop@aeon.test")
	gitRun(t, root, "config", "user.name", "dev-loop test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("dev-loop walk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "seed")
	gitRun(t, root, "remote", "add", "origin", origin)
	gitRun(t, root, "push", "-u", "origin", "main")
	return root, origin
}

func startDevLoop(t *testing.T, s *store.Store, root, slug string, flags map[string]bool) string {
	t.Helper()
	// task_title is what the forge asks need (O1); its only production
	// writer is the caller's `with:` map (F6 — the same writerless family
	// as flags, F4). The single-task walk supplies it as the orchestrator
	// one day will, per task.
	st, err := Start(context.Background(), s, store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "the dev-loop walk", Mode: "standard"}, devLoopPath, yes(), map[string]string{"task_title": "the dev-loop walk", "pr_summary": "t1 lands the dev-loop walk behind the flag, verified on the fake forge.", "commit_message": "t1: land the dev-loop walk step"})
	if err != nil {
		t.Fatal(err)
	}
	st.Tasks = []string{"t1"}
	if flags != nil {
		for k, v := range flags {
			st.Flags[k] = v
		}
	}
	sc := store.Scope{LoopSlug: slug, ComponentRoot: root}
	if _, err := s.CreateTask(sc, store.NewTask{TaskID: "t1", Title: "the dev-loop walk"}); err != nil {
		t.Fatal(err)
	}
	raw, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(st.ID, "running", raw); err != nil {
		t.Fatal(err)
	}
	return st.ID
}

func yes() *bool { b := true; return &b }

// devArtifacts writes what each producing lane's `produces:` demands —
// S8 enforces it, so an ok without the artefact becomes a reject naming
// the file (TestProducesIsEnforcedOnEveryArtefactLane proves the refusal
// half).
func devArtifacts(root string, step blueprint.Step) {
	switch step.Agent {
	case "intent-clarifier":
		_ = os.WriteFile(filepath.Join(root, "prompt.md"), []byte("# intent\n\nship the flag\n"), 0o644)
	case "investigator":
		_ = os.WriteFile(filepath.Join(root, "discovery.md"), []byte("# discovery\n\nit is a loop\n"), 0o644)
	case "legal-advisor":
		_ = os.WriteFile(filepath.Join(root, "legal.md"), []byte("# legal\n\nno constraints found\n"), 0o644)
	case "project-planner":
		_ = os.WriteFile(filepath.Join(root, "plan.md"), []byte("# plan\n\n## Tasks\n- t1 do the thing\n"), 0o644)
		_ = os.MkdirAll(filepath.Join(root, "tasks"), 0o755)
	case "security-engineer":
		_ = os.WriteFile(filepath.Join(root, "security.md"), []byte("# security\n\nnothing found\n"), 0o644)
	case "qa-test-spec":
		_ = os.MkdirAll(filepath.Join(root, "docs", "test_suites"), 0o755)
	}
}

// devDrive runs the loop to a terminal state, answering every question
// the machinery opens. onQuestion names the answer for GATE questions
// (returning stop==true returns the loop as-is — the escalation walks use
// this to inspect the question instead of answering it); ACTION asks
// always answer with the single word that executes.
func devDrive(t *testing.T, dep Deps, id string, onQuestion func(body string, options []string) (string, bool)) (*LoopState, error) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 120; i++ {
		if err := Run(ctx, dep, id); err != nil {
			return nil, err
		}
		st, err := Pinned(ctx, dep.Store, id)
		if err != nil {
			return nil, err
		}
		if st.Status != "waiting" {
			return &st, nil
		}
		if st.PendingAction != nil && st.PendingAction.QuestionID != 0 {
			if _, err := AnswerActionAsk(ctx, dep, id, "approve"); err != nil {
				return nil, err
			}
			continue
		}
		q, err := dep.Store.ReadQuestion(st.QuestionID)
		if err != nil {
			return nil, err
		}
		answer, stop := "approve", false
		if onQuestion != nil {
			answer, stop = onQuestion(q.Body, q.Options)
		}
		if stop {
			return &st, nil
		}
		// The escalation question has its own door (AnswerGate routes
		// through the blueprint step at the position, and an escalation
		// is not a step — F9).
		if len(q.Options) == 2 && q.Options[0] == "continue" && q.Options[1] == "abort" {
			if _, err := AnswerEscalation(ctx, dep, id, answer); err != nil {
				return nil, err
			}
			continue
		}
		if _, err := AnswerGate(ctx, dep, id, answer); err != nil {
			return nil, err
		}
	}
	t.Fatal("devDrive: 120 question rounds without a terminal — the loop is looping on questions")
	return nil, nil
}

// devApproveReview is the review lane's production behaviour walked
// through the forge's OWN door: the code-reviewer posts its approval via
// PostReview, and T8's merge gate reads it from there (L34/D10) — a
// merge without an approve is unreachable, so every walk that reaches
// the merge must have walked a review.
func devApproveReview(ctx context.Context, f *forge.Fake, step blueprint.Step) {
	if f == nil || step.Agent != "code-reviewer" {
		return
	}
	if _, err := f.PostReview(ctx, 1, "the feature reads clean", forge.VerdictApprove); err != nil {
		panic("dev review post: " + err.Error())
	}
}

// devServices mirrors the daemon's OWN wiring (internal/daemon/actions.go)
// with the fake standing where the live forge stands: the probes are real
// git, because openPR's head check and merge's ancestor guard are facts
// about a repository, not about a forge.
// devMerge stands where S16's own merge_test stands: the fake forge
// INVENTS its merge SHA (its TestSquashSHAIsAncestorAndOriginalsAreNot
// pins that), so real git would rightly call the guard's commit name
// invalid. The D5 guard itself is pinned where the commit is real —
// internal/action/merge_test.go; the LOOP walk pins the wiring: that
// merge runs, asks, and records.
type devMerge struct{}

func (devMerge) IsAncestor(_ context.Context, _, _, _ string) (bool, error) { return true, nil }

func devServices(f *forge.Fake) *action.Services {
	gitC := realGitClient()
	return &action.Services{
		Git: &action.GitActions{Git: gitC},
		Forge: &action.ForgeActions{
			Forge: f,
			Git:   gitC, // BranchProbe (O8) stays REAL — it caught the unpushed head
			Merge: devMerge{},
		},
	}
}

func devDeps(s *store.Store, model ModelRunner) (Deps, *forge.Fake) {
	f := forge.NewFake()
	return Deps{Store: s, Model: model, Actions: func(string) *action.Services { return devServices(f) }}, f
}

// TestEveryLaneRuns — D13 over THE file: every lane is visited by the
// engine; every unconditional lane executes; the conditional lanes were
// GIVEN their flags (F4) and run too. The forge saw the outward calls
// the ladder makes; the loop reaches done.
func TestEveryLaneRuns(t *testing.T) {
	s := openLoopStore(t)
	root, origin := devRoot(t)
	_ = origin
	id := startDevLoop(t, s, root, "walk", map[string]bool{
		"legal_review": true, "security_scan": true, "integration_surface": true,
	})
	var fake *forge.Fake
	model := func(ctx context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		devArtifacts(root, step)
		devApproveReview(ctx, fake, step)
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep, f := devDeps(s, model)
	fake = f
	// One gate must be the plan gate — count the COMMITTED gate questions
	// through the walk; escalation never fires on this path.
	st, err := devDrive(t, dep, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "done" {
		t.Fatalf("status %q (reason %q) — the shipped ladder does not run to done", st.Status, st.LastReason)
	}
	// Every lane visited, and its recorded verdict is a real one.
	for _, lane := range []string{"clarify", "discovery", "legal", "plan", "plan-gate", "git-feature-start", "build", "qa", "memory"} {
		state := st.LaneStates[lane]
		if state == "" || state == "pending" {
			t.Fatalf("lane %s verdict %q — a lane the walk never ran", lane, state)
		}
	}
	// The iterating lane ran its ladder, step for step, on the item.
	for _, step := range []string{"issue", "implement", "verify", "security", "pr", "review", "merge", "close-issue"} {
		if got := st.StepStates["build/t1/"+step].Status; got == "" || got == "pending" {
			t.Fatalf("build step %s status %q — the ladder did not walk it", step, got)
		}
	}
	// The fake forge is the receipt drawer: the ladder opened the issue,
	// opened the PR, merged, and closed — for the one task.
	seen := map[string]int{}
	for _, c := range fake.Calls() {
		seen[c.Method]++
	}
	for _, m := range []string{"OpenIssue", "OpenPR", "MergePR", "CloseIssue"} {
		if seen[m] < 1 {
			t.Fatalf("forge never saw %s — calls %v", m, seen)
		}
	}
	// Exactly ONE gate question came from the ladder itself this walk
	// (plan-gate). Engine escalation questions would add more; none fired.
	// The forge ask-questions came from the execution layer's irreversibility
	// (S16), which is the exception shape, not a lane.
}

// TestAbortIsAbortedNotFailed — a plan someone read and declined is a
// working system. v1's loop_finish took done OR aborted for exactly this
// reason; the terminal must say ABORTED, and the on_abort handler lane
// must be what ends the loop.
func TestAbortIsAbortedNotFailed(t *testing.T) {
	s := openLoopStore(t)
	root, _ := devRoot(t)
	id := startDevLoop(t, s, root, "abort", nil)
	model := func(_ context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		devArtifacts(root, step)
		if step.ID == "verify" {
			return ModelResult{Kind: "completed", Outcome: "reject", Reason: "field 'generated_at' missing"}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep, _ := devDeps(s, model)
	// ABORT mid-work, not at the plan gate: an abort before build leaves
	// the cleanup lane with no evidence to clean (F7 — the issue number
	// only exists after the issue step ran, and forge.closeIssue refuses
	// without it), so the walk drives to the exhaustion question and
	// answers THAT with abort.
	st, err := devDrive(t, dep, id, func(body string, options []string) (string, bool) {
		for _, o := range options {
			if o == "abort" && strings.Contains(body, "exhausted") {
				return "abort", false
			}
		}
		return "approve", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "aborted" {
		t.Fatalf("status %q after answering abort at the one gate — declined plans are not failures, and done would be the lie (T7)", st.Status)
	}
	if st.Flags["aborted"] != true {
		t.Fatal("Flags[aborted] not set — the on_abort routing is what got the loop to git-abort")
	}
	if st.StepStates["git-abort"].Status != "done" {
		t.Fatalf("the abort handler lane recorded %q — cleanup cannot be omitted (GAP-4's resolution must still hold)", st.StepStates["git-abort"].Status)
	}
}

// TestConditionalLanesRecordNotNeeded — with their flags ABSENT (the
// production default, F4), the conditional lanes record not-needed, and
// not-needed never reads as pending (T5/O8).
func TestConditionalLanesRecordNotNeeded(t *testing.T) {
	s := openLoopStore(t)
	root, _ := devRoot(t)
	id := startDevLoop(t, s, root, "cond", nil)
	var fake *forge.Fake
	model := func(ctx context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		devArtifacts(root, step)
		devApproveReview(ctx, fake, step)
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep, f := devDeps(s, model)
	fake = f
	st, err := devDrive(t, dep, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "done" {
		t.Fatalf("status %q (reason %q)", st.Status, st.LastReason)
	}
	sc := store.Scope{LoopSlug: "cond", ComponentRoot: root}
	rows, err := s.Phases(sc)
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, p := range rows {
		status[p.Phase] = p.Status
	}
	if status["legal"] != "not-needed" {
		t.Fatalf("legal recorded %q — when-false must persist not-needed, its condition is the blueprint's declaration (T5)", status["legal"])
	}
	if status["qa"] != "not-needed" {
		t.Fatalf("qa recorded %q", status["qa"])
	}
	// qa-specs is not a tracked phase (the tracker's ten names are the
	// protocol's phases, not every lane); its verdict lives on the lane.
	// And a MODE condition skips — the lane belongs to a different loop
	// kind — where a FLAG condition records not-needed (decide's
	// skipName): a POC loop would run this lane, no loop runs it without
	// its flag.
	if st.LaneStates["qa-specs"] != "skipped" {
		t.Fatalf("qa-specs (POC-only) lane recorded %q in a standard loop — mode mismatch is skipped", st.LaneStates["qa-specs"])
	}
	if got := st.StepStates["build/t1/security"].Status; got != "not-needed" {
		t.Fatalf("security step %q — the scan flag was false", got)
	}
	if got := st.StepStates["build/t1/commit-local"].Status; got != "skipped" {
		t.Fatalf("commit-local step %q — a MODE condition skips; only FLAG conditions record not-needed", got)
	}
	if got := st.StepStates["build/t1/branch"].Status; got == "" || got == "pending" {
		t.Fatalf("branch step %q — the task branch rung ran in neither direction", got)
	}
	for phase, got := range status {
		if got == "pending" {
			t.Fatalf("phase %s persisted pending — a bypassed lane must not read as still-to-come", phase)
		}
	}
}

// TestProducesIsEnforcedOnEveryArtefactLane — S8's rule walking the
// shipped file: a lane reporting ok WITHOUT its artefact becomes a
// reject NAMING the file. clarify's artefact is prompt.md.
func TestProducesIsEnforcedOnEveryArtefactLane(t *testing.T) {
	s := openLoopStore(t)
	root, _ := devRoot(t)
	id := startDevLoop(t, s, root, "produces", nil)
	model := func(_ context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		if step.Agent != "intent-clarifier" {
			devArtifacts(root, step)
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep, _ := devDeps(s, model)
	st, err := devDrive(t, dep, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "blocked" {
		t.Fatalf("status %q — an ok without prompt.md must stop the loop, not proceed on the claim", st.Status)
	}
	if !strings.Contains(st.LastReason, "prompt.md") {
		t.Fatalf("reason %q does not NAME the file the lane promised", st.LastReason)
	}
	if got := st.StepStates["clarify"].Outcome; got != "reject" {
		t.Fatalf("clarify outcome %q — the claim was ok, the record says reject (the model's self-claim is an input, never the verdict)", got)
	}
}

// TestBuildLaneRoutesAllThreeVerifyOutcomes — the shipped build lane
// routes T6's three: pass proceeds, fail reworks with the finding and
// escalates at exhaustion (attended), unverified reworks with the
// distinct reason and charges NO task budget.
func TestBuildLaneRoutesAllThreeVerifyOutcomes(t *testing.T) {
	t.Run("pass proceeds to done", func(t *testing.T) {
		s := openLoopStore(t)
		root, _ := devRoot(t)
		id := startDevLoop(t, s, root, "v-pass", nil)
		var fake *forge.Fake
		model := func(ctx context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
			devArtifacts(root, step)
			devApproveReview(ctx, fake, step)
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		}
		dep, f := devDeps(s, model)
		fake = f
		st, err := devDrive(t, dep, id, nil)
		if err != nil {
			t.Fatal(err)
		}
		if st.Status != "done" {
			t.Fatalf("status %q (reason %q)", st.Status, st.LastReason)
		}
		if st.StepStates["build/t1/verify"].Outcome != "ok" {
			t.Fatalf("verify stored %q", st.StepStates["build/t1/verify"].Outcome)
		}
		merged := false
		for _, c := range fake.Calls() {
			if c.Method == "MergePR" {
				merged = true
			}
		}
		if !merged {
			t.Fatal("a passing verify did not reach the merge — the ladder stopped short")
		}
	})

	t.Run("fail reworks with the finding and escalates attended", func(t *testing.T) {
		s := openLoopStore(t)
		root, _ := devRoot(t)
		id := startDevLoop(t, s, root, "v-fail", nil)
		var reworkInputs []string
		model := func(_ context.Context, st LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
			devArtifacts(root, step)
			if step.ID == "implement" && st.Position.Item == "t1" {
				reworkInputs = append(reworkInputs, prompt)
			}
			if step.ID == "verify" {
				return ModelResult{Kind: "completed", Outcome: "reject", Reason: "field 'generated_at' missing"}, nil
			}
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		}
		dep, _ := devDeps(s, model)
		st, err := devDrive(t, dep, id, func(body string, options []string) (string, bool) {
			if strings.Contains(body, "exhausted") {
				return "", true // inspect, do not answer
			}
			return "approve", false
		})
		if err != nil {
			t.Fatal(err)
		}
		if st.Status != "waiting" {
			t.Fatalf("status %q — attended exhaustion must WAIT on the question", st.Status)
		}
		if len(reworkInputs) < 2 || !strings.Contains(reworkInputs[1], "retry 1 of 2") || !strings.Contains(reworkInputs[1], "generated_at") {
			t.Fatalf("the fail rework lost its finding or its attempt: %v", reworkInputs)
		}
	})

	t.Run("unverified reworks without charging the task budget", func(t *testing.T) {
		s := openLoopStore(t)
		root, _ := devRoot(t)
		id := startDevLoop(t, s, root, "v-unverified", nil)
		verifies := 0
		var reworkInputs []string
		var fake *forge.Fake
		model := func(ctx context.Context, st LoopState, step blueprint.Step, prompt string, _ string) (ModelResult, error) {
			devArtifacts(root, step)
			devApproveReview(ctx, fake, step)
			if step.ID == "implement" && st.Position.Item == "t1" {
				reworkInputs = append(reworkInputs, prompt)
			}
			if step.ID == "verify" {
				verifies++
				if verifies <= 2 {
					return ModelResult{Kind: "completed", Outcome: "reject", Reason: unverifiedOOM}, nil
				}
			}
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		}
		dep, f := devDeps(s, model)
		fake = f
		_ = fake
		st, err := devDrive(t, dep, id, nil)
		if err != nil {
			t.Fatal(err)
		}
		if st.Status != "done" {
			t.Fatalf("status %q (reason %q)", st.Status, st.LastReason)
		}
		rows, err := NewCounters(s).TaskCounters(store.Scope{LoopSlug: "v-unverified", ComponentRoot: root}, "t1")
		if err != nil {
			t.Fatal(err)
		}
		if rows["retries"] != 0 {
			t.Fatalf("retries %d — the shipped lane charged the task for a tool that would not run (O4)", rows["retries"])
		}
		if len(reworkInputs) < 2 || !strings.Contains(reworkInputs[1], "unverified") || strings.Contains(reworkInputs[1], "retry 1 of 2") {
			t.Fatalf("the unverified rework is not the distinct send-back: %v", reworkInputs)
		}
	})
}

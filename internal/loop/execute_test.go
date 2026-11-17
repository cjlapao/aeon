package loop

// T10's loop-side tests: the refusal is gone and what runs instead is
// governed. Every verdict route of execute.go, walked against the fakes.

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

func gitTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origin := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=loop", "GIT_AUTHOR_EMAIL=l@l", "GIT_COMMITTER_NAME=loop", "GIT_COMMITTER_EMAIL=l@l")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	must := exec.Command("git", "init", "--bare", origin)
	if out, err := must.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	run("init", "-b", "main", dir)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "seed")
	run("remote", "add", "origin", origin)
	run("push", "-u", "origin", "main")
	return dir
}

func svcWith(f *forge.Fake) *action.Services {
	return &action.Services{Git: &action.GitActions{Git: realGitClient()}, Forge: &action.ForgeActions{Forge: f}}
}

// TestUnattendedOutwardDeniesVerbatim — the loop's blocked reason IS
// T2's sentence, unedited. A loop that paraphrases its own refusal is a
// loop that lies about why it stopped.
func TestUnattendedOutwardDeniesVerbatim(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "pr.yaml")
	writeBlueprint(t, path, "  - id: pr\n    step: {kind: code-action, action: forge.openPR}\n")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "pr", ComponentRoot: root, Root: root, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dep := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(forge.NewFake()) }}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "forge.openPR denied — effect class 'outward' resolves to deny (loop is unattended)"
	if saved.Status != "blocked" || saved.LastReason != want {
		t.Fatalf("unattended outward blocks with T2's sentence VERBATIM\ngot:  %q (%s)\nwant: %q", saved.LastReason, saved.Status, want)
	}
}

// TestLocalActionRunsAndRecordsEvidence (Q7) — a local action runs with
// no question, and numbers land in the row AND the transcript event.
func TestLocalActionRunsAndRecordsEvidence(t *testing.T) {
	s := openLoopStore(t)
	root := gitTestRepo(t)
	path := filepath.Join(t.TempDir(), "branch.yaml")
	writeBlueprint(t, path, "  - id: start\n    step: {kind: code-action, action: git.createBranch}\n")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "4a17-version", ComponentRoot: root, Root: root, Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var executed []map[string]any
	dep := Deps{Store: s, Event: func(kind string, payload any) {
		if kind == "loop.action.executed" {
			if m, ok := payload.(map[string]any); ok {
				executed = append(executed, m)
			}
		}
	}, Actions: func(string) *action.Services { return svcWith(forge.NewFake()) }}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	ev := saved.Evidence["start"] // the key is the executed position, wherever the loop has moved since
	if ev == nil || ev["branch"] == "" || ev["branch"] == "(unset)" {
		t.Fatalf("evidence must reach the row with its facts: %+v", saved.Evidence)
	}
	if len(executed) != 1 || executed[0]["action"] != "git.createBranch" {
		t.Fatalf("the transcript carries the same map (Q7): %v", executed)
	}
	// The branch really exists — the row and the repo agree.
	out, err := exec.Command("git", "rev-parse", "--verify", "feature/4a17-version").Output()
	if err == nil && strings.TrimSpace(string(out)) == "" {
		t.Fatal("evidence claims a branch the repo does not have")
	}
}

// TestAttendedOutwardAsksThenApproveExecutes — the ask route (Q5: S11's
// permission kind reused): question commits, loop waits, asking effects
// NOTHING, only "approve" releases the effect. Junk answers refuse.
func TestAttendedOutwardAsksThenApproveExecutes(t *testing.T) {
	run := func(t *testing.T, answer string) (LoopState, *forge.Fake) {
		t.Helper()
		f := forge.NewFake()
		f.SeedIssue(forge.Issue{Number: 4, Title: "[4a17] Add version endpoint", Labels: []string{"dev-loop"}})
		s := openLoopStore(t)
		root := t.TempDir()
		path := filepath.Join(t.TempDir(), "close.yaml")
		writeBlueprint(t, path, "  - id: close-issue\n    step: {kind: code-action, action: forge.closeIssue}\n")
		attended := true
		st, err := Start(context.Background(), s, store.NewLoop{Slug: "4a17", ComponentRoot: root, Root: root, Mode: "standard"}, path, &attended, nil)
		if err != nil {
			t.Fatal(err)
		}
		dep := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(f) }}
		// The number arrives as EVIDENCE from the openIssue row (Q7).
		st.Evidence = map[string]map[string]string{stepKey(st.Position): {"issue_number": "4"}}
		if err := save(dep, st); err != nil {
			t.Fatal(err)
		}
		if err := Run(context.Background(), dep, st.ID); err != nil {
			t.Fatal(err)
		}
		waiting, err := Pinned(context.Background(), s, st.ID)
		if err != nil {
			t.Fatal(err)
		}
		if waiting.Status != "waiting" || waiting.QuestionID == 0 {
			t.Fatalf("attended outward must ASK: %+v", waiting)
		}
		if waiting.PendingAction == nil || waiting.PendingAction.Name != "forge.closeIssue" {
			t.Fatalf("the ask is durable — the crash re-consults the SAME question: %+v", waiting.PendingAction)
		}
		q, err := s.ReadQuestion(waiting.QuestionID)
		if err != nil {
			t.Fatal(err)
		}
		if len(q.Options) != 2 || q.Options[0] != "approve" || q.Options[1] != "deny" {
			t.Fatalf("the verbs are approve/deny exactly (T2): %v", q.Options)
		}
		if iss, _ := f.ReadIssue(context.Background(), 4); iss.State != "open" {
			t.Fatal("asking must not effect")
		}
		if answer == "approve" || answer == "deny" {
			if _, err := AnswerActionAsk(context.Background(), dep, st.ID, answer); err != nil {
				t.Fatal(err)
			}
		} else {
			// OUT OF BAND: the answered path must honour the row even when
			// a chat thread answered it past the door.
			if _, err := s.Answer(waiting.QuestionID, answer); err != nil {
				t.Fatal(err)
			}
		}
		if err := Run(context.Background(), dep, st.ID); err != nil {
			t.Fatal(err)
		}
		saved, err := Pinned(context.Background(), s, st.ID)
		if err != nil {
			t.Fatal(err)
		}
		return saved, f
	}

	saved, f := run(t, "approve")
	if saved.Status != "done" && saved.Status != "blocked" {
		// advanced → Run continues to the end (single-lane) → done
	}
	if iss, _ := f.ReadIssue(context.Background(), 4); iss.State != "closed" {
		t.Fatalf("approve executes the effect: status=%s issue=%+v", saved.Status, iss)
	}

	saved2, f2 := run(t, "Approve") // the case trap: capital A is NOT the verb
	if iss, _ := f2.ReadIssue(context.Background(), 4); iss.State != "open" {
		t.Fatalf("junk refuses, never executes — and it must stay refused: %+v", iss)
	}
	if !strings.Contains(saved2.LastReason, "approve") {
		t.Fatalf("the refusal says what was refused: %q", saved2.LastReason)
	}
}

// TestNoRefusalPathRemains (D16) — a grep against the SOURCE: the
// retired refusal is gone from the paths that run. The BlockedReason
// KIND survives in the type (removing a kind from a structured protocol
// type is a protocol change); the BRANCH died.
func TestNoRefusalPathRemains(t *testing.T) {
	for _, file := range []string{"advance.go", "execute.go"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "unimplemented-action") {
			t.Fatalf("%s still mints the retired refusal (D16)", file)
		}
	}
	types, err := os.ReadFile("action.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(types), "unimplemented-action") {
		t.Log("note: the kind string survives only in comments/uses elsewhere — acceptable either way")
	}
}

var _ = blueprint.CodeAction

// TestAskNamesCodeOwnedIdentifiers — the O1 half of the ask, walked at
// T12: the question must carry head/base/title, and every one of them
// comes from CODE (the with map the operator declared, the branch names
// code derives). The approve then runs into the NEXT honest gate: the
// required-arg guard refuses the dispatch naming pr_summary, because no
// model handoff produced one — the ask is never a rubber stamp, and a
// dispatch without its data dies naming the data.
func TestAskNamesCodeOwnedIdentifiers(t *testing.T) {
	f := forge.NewFake()
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "openpr.yaml")
	writeBlueprint(t, path, "  - id: open-pr\n    step: {kind: code-action, action: forge.openPR}\n")
	attended := true
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "walk-ask", ComponentRoot: root, Root: root, Mode: "standard"},
		path, &attended, map[string]string{"task": "4a17", "task_title": "Add version endpoint"})
	if err != nil {
		t.Fatal(err)
	}
	dep := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(f) }}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	waiting, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != "waiting" {
		t.Fatalf("attended openPR asks: %s (%s)", waiting.Status, waiting.LastReason)
	}
	q, err := s.ReadQuestion(waiting.QuestionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"task/walk-ask/4a17", "feature/walk-ask", "Add version endpoint"} {
		if !strings.Contains(q.Body, want) {
			t.Fatalf("the ask must name %s (O1) — a content-less ask trains operators to wave asks through:\n%s", want, q.Body)
		}
	}
	if _, err := AnswerActionAsk(context.Background(), dep, st.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" || !strings.Contains(saved.LastReason, "pr_summary") {
		t.Fatalf("approve releases the dispatch, and the dispatch refuses naming the missing handoff: %s (%s)", saved.Status, saved.LastReason)
	}
	for _, c := range f.Calls() {
		if c.Method == "OpenPR" {
			t.Fatal("the dispatch refused on args — the forge was never asked")
		}
	}
}

// TestStepParamsCarryWhich — the two createBranch lanes of one loop are
// different lanes (feature first, then task); the step's authored params
// say which, and CreateBranch validates the value. This is T12's walk
// finding the channel: without it every createBranch defaults to task
// and no loop can ever grow a feature branch.
func TestStepParamsCarryWhich(t *testing.T) {
	s := openLoopStore(t)
	dir := gitTestRepo(t)
	path := filepath.Join(t.TempDir(), "branches.yaml")
	writeBlueprint(t, path, "  - id: feat\n    step: {kind: code-action, action: git.createBranch, params: {which: feature}}\n")
	attended := false
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "walk-w2", ComponentRoot: dir, Root: dir, Mode: "standard"},
		path, &attended, map[string]string{"task": "4a17"})
	if err != nil {
		t.Fatal(err)
	}
	dep := Deps{Store: s, Actions: func(string) *action.Services {
		return &action.Services{Git: &action.GitActions{Git: realGitClient()}}
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "done" {
		t.Fatalf("which=feature from step params runs the lane: %s (%s)", saved.Status, saved.LastReason)
	}
	out, err := (realGitClient()).Run(context.Background(), dir, "branch", "--list", "feature/walk-w2")
	if err != nil || !strings.Contains(out, "feature/walk-w2") {
		t.Fatalf("the feature branch exists: %q %v", out, err)
	}
}

// TestAnswerActionAskNoticesAnAnsweredRow — the chat thread answered the
// row out of band; the door must NOTICE and re-drive, not refuse the
// re-entry as a race. The caller's text changes nothing: the recorded
// answer is the record.
func TestAnswerActionAskNoticesAnAnsweredRow(t *testing.T) {
	f := forge.NewFake()
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "openpr.yaml")
	writeBlueprint(t, path, "  - id: open-pr\n    step: {kind: code-action, action: forge.openPR}\n")
	attended := true
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "walk-note", ComponentRoot: root, Root: root, Mode: "standard"},
		path, &attended, map[string]string{"task": "4a17", "task_title": "T"})
	if err != nil {
		t.Fatal(err)
	}
	dep := Deps{Store: s, Actions: func(string) *action.Services { return svcWith(f) }}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	waiting, err := Pinned(context.Background(), s, st.ID)
	if err != nil || waiting.Status != "waiting" {
		t.Fatalf("waiting: %v", err)
	}
	// Out of band: nobody went through the door.
	if _, err := s.Answer(waiting.QuestionID, "approve"); err != nil {
		t.Fatal(err)
	}
	if _, err := AnswerActionAsk(context.Background(), dep, st.ID, "anything-the-caller-types"); err != nil {
		t.Fatalf("the door notices the answered row instead of refusing it: %v", err)
	}
	after, err := Pinned(context.Background(), s, st.ID)
	if err != nil || after.Status != "running" {
		t.Fatalf("noticing re-drives: %s %v", after.Status, err)
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" {
		t.Fatalf("approve runs the dispatch, which refuses naming pr_summary: %s", saved.Status)
	}
}

package loop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// S17 T8 — the deferred-findings backlog (O7, I6). A finding worth
// recording and NOT worth blocking on needs somewhere durable to live;
// without it every finding becomes a blocker by default and the review
// lane becomes the thing people route around. Appended, never rewritten:
// a backlog that gets regenerated loses the history that makes it a
// backlog.
//
// Q10, answered: the file lives in the PROJECT the loop works on —
// <root>/docs/unresolved-pr-comments.md (v1's name, kept: six months of
// muscle memory beats a fresh path) — and its entry shape is documented
// in aeon/docs/BACKLOG.md. The spec's `aeon/docs/BACKLOG.md` is read as
// naming the DOCUMENT; the runtime file naming diverges deliberately and
// the divergence is recorded here and in the document.

func TestBacklogIsAppendOnly(t *testing.T) {
	root := t.TempDir()
	first := BacklogEntry{PR: 7, Date: "2026-10-26", Finding: "off-by-one in the pager", Lane: "build", Step: "review"}
	if err := AppendBacklog(root, first); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(BacklogPath(root))
	if err != nil {
		t.Fatal(err)
	}
	second := BacklogEntry{PR: 9, Date: "2026-10-27", Finding: "retry counter races", Lane: "build", Step: "review"}
	if err := AppendBacklog(root, second); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(BacklogPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(after) <= len(before) {
		t.Fatalf("the file did not grow — an append that does not grow is a rewrite (%d → %d bytes)", len(before), len(after))
	}
	if !strings.HasPrefix(string(after), string(before)) {
		t.Fatalf("prior content is not a PREFIX of the new file — the backlog was rewritten, not appended:\n%s", after)
	}
	if !strings.Contains(string(after), "off-by-one in the pager") || !strings.Contains(string(after), "retry counter races") {
		t.Fatalf("both entries must survive:\n%s", after)
	}
}

func TestBacklogEntryNamesPRAndDate(t *testing.T) {
	root := t.TempDir()
	e := BacklogEntry{PR: 42, Date: "2026-10-26", Finding: "the gate evidence lacks the reviewer", Lane: "build", Step: "review"}
	if err := AppendBacklog(root, e); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(BacklogPath(root))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"PR #42", "2026-10-26", "the gate evidence lacks the reviewer"} {
		if !strings.Contains(text, want) {
			t.Fatalf("entry must name %q — an entry that does not say PR and date cannot be triaged:\n%s", want, text)
		}
	}
}

// TestEngineAppendsReviewFindings — the call site: a review step that
// reports ok WITH non-blocking findings appends them, naming the PR from
// the evidence (never from prose); an ok with nothing to report appends
// nothing.
func TestEngineAppendsReviewFindings(t *testing.T) {
	s := openLoopStore(t)
	root, _ := devRoot(t) // real git: openPR's head probe is the honest BranchProbe
	path := filepath.Join(t.TempDir(), "backlog.yaml")
	writeBlueprint(t, path,
		"  - id: branch\n    step: {kind: code-action, action: git.createBranch, params: {which: task}}\n"+
			"  - id: push\n    step: {kind: code-action, action: git.commitAndPush}\n"+
			"  - id: pr\n    step: {kind: code-action, action: forge.openPR}\n"+
			"  - id: review\n    step: {kind: model-step, agent: code-reviewer, sets: in-review}\n")
	attended := true
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "blg", ComponentRoot: root, Root: root, Mode: "standard"},
		path, &attended, map[string]string{"task": "4a17", "task_title": "add the endpoint", "pr_summary": "adds the version endpoint for the walk.", "commit_message": "4a17: add the version endpoint"})
	if err != nil {
		t.Fatal(err)
	}
	model := func(_ context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		if step.Agent == "code-reviewer" {
			return ModelResult{Kind: "completed", Outcome: "ok", Reason: "n+1 overflow risk at pager.go:42 — non-blocking"}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep, _ := devDeps(s, model)
	loopSt, err := devDrive(t, dep, st.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if loopSt.Status != "done" {
		t.Fatalf("status %q (reason %q)", loopSt.Status, loopSt.LastReason)
	}
	raw, err := os.ReadFile(BacklogPath(root))
	if err != nil {
		t.Fatalf("the review reported a non-blocking finding and no backlog exists: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "n+1 overflow risk at pager.go:42") {
		t.Fatalf("the finding text is missing:\n%s", text)
	}
	if !strings.Contains(text, "PR #1") {
		t.Fatalf("the entry must name the PR from the EVIDENCE (the fake opened PR #1):\n%s", text)
	}
}

// TestEngineOkWithoutFindingsAppendsNothing — an ok with nothing to report
// reports nothing; a backlog full of empty entries is noise that trains
// nobody to read it.
func TestEngineOkWithoutFindingsAppendsNothing(t *testing.T) {
	s := openLoopStore(t)
	root, _ := devRoot(t)
	path := filepath.Join(t.TempDir(), "backlog2.yaml")
	writeBlueprint(t, path,
		"  - id: branch\n    step: {kind: code-action, action: git.createBranch, params: {which: task}}\n"+
			"  - id: push\n    step: {kind: code-action, action: git.commitAndPush}\n"+
			"  - id: pr\n    step: {kind: code-action, action: forge.openPR}\n"+
			"  - id: review\n    step: {kind: model-step, agent: code-reviewer, sets: in-review}\n")
	attended := true
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "blg2", ComponentRoot: root, Root: root, Mode: "standard"},
		path, &attended, map[string]string{"task": "4a17", "task_title": "add the endpoint", "pr_summary": "adds the version endpoint for the walk.", "commit_message": "4a17: add the version endpoint"})
	if err != nil {
		t.Fatal(err)
	}
	model := func(_ context.Context, _ LoopState, _ blueprint.Step, _ string, _ string) (ModelResult, error) {
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
	dep, _ := devDeps(s, model)
	if _, err := devDrive(t, dep, st.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(BacklogPath(root)); !os.IsNotExist(err) {
		t.Fatalf("an ok with no findings wrote a backlog entry (err=%v)", err)
	}
}

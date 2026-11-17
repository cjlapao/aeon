package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

func docToolset(t *testing.T, svc *DocService) tools.Toolset {
	t.Helper()
	set, err := tools.BuildToolset([]string{"doc_write", "doc_read", "doc_edit", "doc_list"}, tools.ToolContext{WorkspaceRoot: t.TempDir(), Docs: svc})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// The whole chain, the way agents walk it: a brainstorm loop files its
// deliverables and a goal, and the loop that works the goal reads and extends
// them by name.
func TestTheGoalLoopReadsWhatTheBrainstormFiled(t *testing.T) {
	e := newRecordsEnv(t, "brainstorm")
	svc := NewDocService(e.s)
	set := docToolset(t, svc)

	for _, doc := range []string{`{"name":"proposal.md","content":"the proposal"}`, `{"name":"brief.md","content":"the brief"}`} {
		if r := set.Call(e.ctx, "doc_write", json.RawMessage(doc)); r.IsError {
			t.Fatalf("write: %s", r.Content)
		}
	}
	out, f, err := e.do(t, "goal_create", `{"title":"ship it","type":"feature","components":[{"component":"alpha"}]}`)
	if err != nil || f != nil {
		t.Fatalf("goal_create: %v %+v", err, f)
	}
	goal := out.(map[string]any)["goal"].(store.Goal)

	owned, err := e.s.GoalDocuments(goal.ID)
	if err != nil || len(owned) != 2 {
		t.Fatalf("the goal owns what the brainstorm produced: %+v %v", owned, err)
	}
	// The brainstorm keeps its own rows and the link: nothing moved away.
	if _, err := e.s.ReadDocument(store.Scope{LoopSlug: "walk", ComponentRoot: "/repo/alpha"}, "brief.md"); err != nil {
		t.Fatalf("the brainstorm loop lost its own document: %v", err)
	}

	// The loop that works the goal: a new loop, a new step, the goal's id.
	gv := goal.Version
	if _, err := e.s.CreatePinnedLoop(store.NewLoop{Slug: "work", ComponentRoot: "/repo/alpha", Title: "W", Goal: "g", GoalID: goal.ID, GoalVersion: &gv}, "feature-loop", 1, "raw", "{}"); err != nil {
		t.Fatal(err)
	}
	step, err := e.s.CreateStep(store.Scope{LoopSlug: "work", ComponentRoot: "/repo/alpha"}, store.NewStep{Phase: "plan", Agent: "project-planner"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := stepctx.WithStepID(context.Background(), step.ID)

	list := set.Call(ctx, "doc_list", json.RawMessage(`{}`))
	if !strings.Contains(list.Content, "Owned by the goal this loop works:") || !strings.Contains(list.Content, "brief.md") || !strings.Contains(list.Content, "proposal.md") {
		t.Fatalf("the goal loop must see the goal's documents: %q", list.Content)
	}
	read := set.Call(ctx, "doc_read", json.RawMessage(`{"name":"brief.md"}`))
	if read.IsError || !strings.Contains(read.Content, "the brief") || !strings.Contains(read.Content, "written by loop walk for goal "+goal.ID) {
		t.Fatalf("read the inherited brief: %+v", read)
	}
	if r := set.Call(ctx, "doc_edit", json.RawMessage(`{"name":"brief.md","old":"the brief","new":"changed"}`)); !r.IsError {
		t.Fatalf("an inherited document is read, then re-filed, not edited in place: %+v", r)
	}
	// Filing its own plan makes it the goal's too, without touching the brainstorm's rows.
	if r := set.Call(ctx, "doc_write", json.RawMessage(`{"name":"plan.md","content":"the plan"}`)); r.IsError {
		t.Fatalf("write plan: %s", r.Content)
	}
	if r := set.Call(ctx, "doc_write", json.RawMessage(`{"name":"brief.md","content":"the refined brief"}`)); r.IsError {
		t.Fatalf("refile brief: %s", r.Content)
	}
	head, err := e.s.ReadGoalDocument(goal.ID, "brief.md")
	if err != nil || head.Content != "the refined brief" || head.LoopSlug != "work" {
		t.Fatalf("the goal's head is the newer write: %+v %v", head, err)
	}
	old, _ := e.s.ReadDocument(store.Scope{LoopSlug: "walk", ComponentRoot: "/repo/alpha"}, "brief.md")
	if old.Content != "the brief" {
		t.Fatalf("the brainstorm's own history stays: %q", old.Content)
	}
}

func TestADocumentToolRefusesWhatTheDocumentDoorRefuses(t *testing.T) {
	e := newRecordsEnv(t, "")
	set := docToolset(t, NewDocService(e.s))
	for name, why := range map[string]string{"/etc/passwd": "absolute", "../x.md": ".. segment", "main.go": "unsupported file type", "a/b/c.md": "nested one level"} {
		in, _ := json.Marshal(map[string]string{"name": name, "content": "x"})
		r := set.Call(e.ctx, "doc_write", in)
		if !r.IsError || !strings.Contains(r.Content, "refused:") || !strings.Contains(r.Content, strings.Split(why, " ")[0]) {
			t.Fatalf("%s must be refused (%s): %+v", name, why, r)
		}
	}
	if r := set.Call(context.Background(), "doc_list", json.RawMessage(`{}`)); !r.IsError || !strings.Contains(r.Content, "need a step") {
		t.Fatalf("a call with no step on the context is refused: %+v", r)
	}
}

const claimBlueprint = `id: legal-loop
schemaVersion: 1
version: 1
lanes:
  - id: review
    step: {kind: model-step, agent: default, produces: [legal.md, plan.md]}
  - id: split
    step: {kind: model-step, agent: default, produces: [tasks/]}
`

// The routing net, end to end: a loop pinned on a blueprint that declares
// legal.md, plan.md and tasks/ — a model that uses `write` anyway leaves its
// deliverables in the database and nothing in the project root.
func TestWriteOnADeclaredDeliverableFilesItAndLeavesNoFile(t *testing.T) {
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	if _, err := st.CreatePinnedLoop(store.NewLoop{Slug: "legal-1", ComponentRoot: root, Root: root, Title: "L", Goal: "g"}, "legal-loop", 1, claimBlueprint, "{}"); err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: "legal-1", ComponentRoot: root}
	step, err := st.CreateStep(sc, store.NewStep{Phase: "review", Agent: "legal-advisor"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := stepctx.WithStepID(context.Background(), step.ID)
	svc := NewDocService(st)
	set, err := tools.BuildToolset([]string{"write", "edit", "read"}, tools.ToolContext{WorkspaceRoot: root, Docs: svc})
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ path, content string }{
		{"legal.md", "the legal review"},
		{"tasks/T1.md", "task one"},
		{".projects/legal-1/plan.md", "the plan"},
		{".projects/loops/legal-1/tasks/T2.md", "task two"},
	} {
		in, _ := json.Marshal(map[string]string{"path": c.path, "content": c.content})
		if r := set.Call(ctx, "write", in); r.IsError || !strings.Contains(r.Content, "in the database") {
			t.Fatalf("write %s: %+v", c.path, r)
		}
	}
	for name, want := range map[string]string{"legal.md": "the legal review", "tasks/T1.md": "task one", "plan.md": "the plan", "tasks/T2.md": "task two"} {
		doc, err := st.ReadDocument(sc, name)
		if err != nil || doc.Content != want || doc.WrittenBy != "legal-advisor" {
			t.Fatalf("%s in the store: %+v %v", name, doc, err)
		}
	}
	if r := set.Call(ctx, "edit", json.RawMessage(`{"path":"legal.md","old_string":"legal","new_string":"LEGAL"}`)); r.IsError {
		t.Fatalf("edit: %+v", r)
	}
	if r := set.Call(ctx, "read", json.RawMessage(`{"path":"legal.md"}`)); !strings.Contains(r.Content, "the LEGAL review") {
		t.Fatalf("read: %+v", r)
	}
	// A path the blueprint does not name is the workspace's.
	if r := set.Call(ctx, "write", json.RawMessage(`{"path":"main.go","content":"package main"}`)); r.IsError || !strings.Contains(r.Content, "wrote main.go") {
		t.Fatalf("source file: %+v", r)
	}
	if _, err := st.ReadDocument(sc, "scratch/x.md"); err == nil {
		t.Fatal("nothing else was filed")
	}
	for _, gone := range []string{"legal.md", "plan.md", "tasks", ".projects"} {
		if _, err := os.Stat(filepath.Join(root, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s must not exist in the workspace: %v", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "main.go")); err != nil {
		t.Fatalf("the source file is on disk: %v", err)
	}
}

// goal.show lists the documents the goal owns, so a person sees what the
// brainstorm produced without opening the loop it came from.
func TestGoalShowListsTheDocumentsTheGoalOwns(t *testing.T) {
	e := newRecordsEnv(t, "brainstorm")
	set := docToolset(t, NewDocService(e.s))
	for _, doc := range []string{`{"name":"proposal.md","content":"p"}`, `{"name":"brief.md","content":"bb"}`} {
		if r := set.Call(e.ctx, "doc_write", json.RawMessage(doc)); r.IsError {
			t.Fatal(r.Content)
		}
	}
	out, f, err := e.do(t, "goal_create", `{"title":"ship","type":"feature","components":[{"component":"alpha"}]}`)
	if err != nil || f != nil {
		t.Fatalf("goal_create: %v %+v", err, f)
	}
	goal := out.(map[string]any)["goal"].(store.Goal)

	res, sentence, err := (&socketRecords{d: &Daemon{store: e.s}}).GoalShow(context.Background(), nil, protocol.GoalShowParams{ID: goal.ID})
	if err != nil || sentence != "" {
		t.Fatalf("goal.show: %q %v", sentence, err)
	}
	if len(res.Documents) != 2 || res.Documents[0].Name != "brief.md" || res.Documents[0].LoopSlug != "walk" ||
		res.Documents[0].ComponentRoot != "/repo/alpha" || res.Documents[1].Name != "proposal.md" || res.Documents[1].Bytes != 1 {
		t.Fatalf("documents: %+v", res.Documents)
	}
}

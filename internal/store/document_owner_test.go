package store

import (
	"errors"
	"testing"
)

func ownerStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func ownerLoop(t *testing.T, st *Store, slug, root string) Scope {
	t.Helper()
	if _, err := st.CreatePinnedLoop(NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "g", Status: "running"}, "brainstorm", 1, "raw", `{}`); err != nil {
		t.Fatal(err)
	}
	return Scope{LoopSlug: slug, ComponentRoot: root}
}

func mustWrite(t *testing.T, st *Store, sc Scope, name, content string) Document {
	t.Helper()
	res, err := st.WriteDocument(sc, name, content, "tester")
	if err != nil || !res.OK {
		t.Fatalf("write %s: %+v %v", name, res, err)
	}
	return res.Document
}

func TestAGoalOwnsWhatTheLoopThatFiledItHadWritten(t *testing.T) {
	st := ownerStore(t)
	root := t.TempDir()
	brain := ownerLoop(t, st, "brain", root)
	mustWrite(t, st, brain, "proposal.md", "the proposal")
	mustWrite(t, st, brain, "brief.md", "the brief")

	if docs, _ := st.GoalDocuments("goal-1"); len(docs) != 0 {
		t.Fatalf("nobody owns anything yet: %+v", docs)
	}
	n, err := st.AdoptDocuments(brain, "goal-1")
	if err != nil || n != 2 {
		t.Fatalf("adopt: %d %v", n, err)
	}
	docs, err := st.GoalDocuments("goal-1")
	if err != nil || len(docs) != 2 || docs[0].File != "brief.md" || docs[1].File != "proposal.md" {
		t.Fatalf("goal documents: %+v %v", docs, err)
	}
	got, err := st.ReadGoalDocument("goal-1", "brief.md")
	if err != nil || got.Content != "the brief" || got.LoopSlug != "brain" {
		t.Fatalf("read: %+v %v", got, err)
	}
	// The loop keeps its own rows: the link is historic, not a move.
	if own, err := st.ReadDocument(brain, "brief.md"); err != nil || own.OwnerGoal != "goal-1" {
		t.Fatalf("the loop's own row: %+v %v", own, err)
	}
}

func TestADocumentKeepsItsFirstOwner(t *testing.T) {
	st := ownerStore(t)
	brain := ownerLoop(t, st, "brain", t.TempDir())
	mustWrite(t, st, brain, "plan.md", "v1")
	if _, err := st.AdoptDocuments(brain, "goal-1"); err != nil {
		t.Fatal(err)
	}
	if n, err := st.AdoptDocuments(brain, "goal-2"); err != nil || n != 0 {
		t.Fatalf("a second goal adopts nothing: %d %v", n, err)
	}
	// A later revision by the same loop stays the first goal's.
	if d := mustWrite(t, st, brain, "plan.md", "v2"); d.OwnerGoal != "goal-1" {
		t.Fatalf("rev 2 owner: %q", d.OwnerGoal)
	}
	if _, err := st.ReadGoalDocument("goal-2", "plan.md"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("goal-2 owns nothing: %v", err)
	}
}

func TestALoopThatWorksAGoalWritesIntoIt(t *testing.T) {
	st := ownerStore(t)
	root := t.TempDir()
	brain := ownerLoop(t, st, "brain", root)
	mustWrite(t, st, brain, "brief.md", "from brainstorm")
	if _, err := st.AdoptDocuments(brain, "goal-1"); err != nil {
		t.Fatal(err)
	}

	work := ownerLoop(t, st, "work", root)
	if _, err := st.w.Exec(`UPDATE loop SET goal_id = ? WHERE component_root = ? AND slug = ?`, "goal-1", root, "work"); err != nil {
		t.Fatal(err)
	}
	if g, err := st.LoopGoal(work); err != nil || g != "goal-1" {
		t.Fatalf("loop goal: %q %v", g, err)
	}
	d := mustWrite(t, st, work, "plan.md", "the plan")
	if d.OwnerGoal != "goal-1" {
		t.Fatalf("the goal loop's document is the goal's: %+v", d)
	}
	mustWrite(t, st, work, "brief.md", "refined")
	got, err := st.ReadGoalDocument("goal-1", "brief.md")
	if err != nil || got.Content != "refined" || got.LoopSlug != "work" {
		t.Fatalf("the newer write is the goal's head: %+v %v", got, err)
	}
	docs, _ := st.GoalDocuments("goal-1")
	if len(docs) != 2 {
		t.Fatalf("two names, one head each: %+v", docs)
	}
}

func TestATaskPlannedInAGoalsLoopBelongsToTheGoal(t *testing.T) {
	st := ownerStore(t)
	root := t.TempDir()
	work := ownerLoop(t, st, "work", root)
	if _, err := st.w.Exec(`UPDATE loop SET goal_id = ? WHERE component_root = ? AND slug = ?`, "goal-1", root, "work"); err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(work, NewTask{TaskID: "T1", Ord: 1, Title: "do it"})
	if err != nil || task.GoalID != "goal-1" {
		t.Fatalf("task goal: %+v %v", task, err)
	}
	other := ownerLoop(t, st, "free", root)
	if free, err := st.CreateTask(other, NewTask{TaskID: "T1", Ord: 1, Title: "loose"}); err != nil || free.GoalID != "" {
		t.Fatalf("a loop with no goal makes goal-less tasks: %+v %v", free, err)
	}
	rows, err := st.ListTaskRows(TaskFilter{GoalID: "goal-1"})
	if err != nil || len(rows) != 1 || rows[0].Title != "do it" {
		t.Fatalf("the goal's tasks: %+v %v", rows, err)
	}
}

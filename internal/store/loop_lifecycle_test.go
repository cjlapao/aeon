package store

import (
	"context"
	"testing"
)

// Archiving hides a loop and changes nothing else; deleting removes the loop and what hangs from it, keeps its
// totals in a tombstone, and leaves its tasks standing with the link cut and the cost they had spent.
func TestArchiveHidesAndDeleteKeepsTheStatsAndTheWork(t *testing.T) {
	s, err := Open(Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	l, err := s.CreateLoop(NewLoop{Slug: "doomed", ComponentRoot: "/r", Title: "t", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	sc := Scope{LoopSlug: "doomed", ComponentRoot: "/r"}
	task, err := s.CreateTask(sc, NewTask{TaskID: "T1", Title: "keep me", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`INSERT INTO step (loop_slug, component_root, phase, agent, attempt, status, started_at, ended_by, source, taskid, cost_usd, cost_state)
		VALUES ('doomed','/r','p','a',1,'done','2026-01-01T00:00:00Z','agent','live','T1',1.5,'known')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteDocument(sc, "plan.md", "# plan", "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`UPDATE component_telemetry SET loops_run = loops_run`); err != nil {
		t.Logf("no telemetry row to check: %v", err)
	}

	if err := s.SetLoopArchived(ctx, l.ID, true, "old"); err != nil {
		t.Fatal(err)
	}
	if hidden, _ := s.ArchivedLoops(ctx); hidden[l.ID] == "" {
		t.Fatal("the loop must read as archived")
	}
	if _, err := s.ReadLoopByID(l.ID); err != nil {
		t.Fatalf("archiving must leave the loop in place: %v", err)
	}
	if err := s.SetLoopArchived(ctx, l.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	if hidden, _ := s.ArchivedLoops(ctx); len(hidden) != 0 {
		t.Fatal("showing the loop again must clear the archive")
	}

	tomb, err := s.DeleteLoopKeepingStats(ctx, l.ID, "user:me", "broken")
	if err != nil {
		t.Fatal(err)
	}
	if tomb.Slug != "doomed" || tomb.Steps != 1 || tomb.CostUSD == nil || *tomb.CostUSD != 1.5 || tomb.CostState != "known" {
		t.Fatalf("the tombstone lost the loop's totals: %+v", tomb)
	}
	if _, err := s.ReadLoopByID(l.ID); err == nil {
		t.Fatal("the loop must be gone")
	}
	if docs, _ := s.ListDocuments(sc); len(docs) != 0 {
		t.Fatalf("the loop's documents must go with it: %d left", len(docs))
	}
	var steps int
	_ = s.r.QueryRow(`SELECT COUNT(*) FROM step WHERE loop_slug = 'doomed'`).Scan(&steps)
	if steps != 0 {
		t.Fatalf("the steps must go with the loop: %d left", steps)
	}
	var kept Task
	rows, err := s.ListTaskRows(TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == task.ID {
			kept = r.Task
			if r.CostUSD != 1.5 {
				t.Errorf("the task's cost must survive its loop: %v", r.CostUSD)
			}
		}
	}
	if kept.ID == "" || kept.LoopSlug != "" || !kept.Manual {
		t.Fatalf("the task must stay, detached: %+v", kept)
	}
	link, ok, _ := s.DetachedLinkFor(ctx, "task", task.ID)
	if !ok || link.LoopSlug != "doomed" {
		t.Fatalf("the task must say which loop it lost: %+v", link)
	}
	if tombs, _ := s.ListLoopTombstones(ctx, "/r"); len(tombs) != 1 {
		t.Fatalf("one tombstone expected, got %d", len(tombs))
	}
}

// Archiving a loop closes its open work as not done and restoring it opens that work again; deleting closes it for
// good and says so. Finished work is never touched.
func TestArchiveAndDeleteCloseTheLoopsOpenWork(t *testing.T) {
	s, err := Open(Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.CreateComponent(NewComponent{Name: "work", Slug: "work", Root: "/r"}); err != nil {
		t.Fatal(err)
	}
	l, err := s.CreateLoop(NewLoop{Slug: "work", ComponentRoot: "/r", Title: "t", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	sc := Scope{LoopSlug: "work", ComponentRoot: "/r"}
	open, _ := s.CreateTask(sc, NewTask{TaskID: "T1", Title: "open", Status: "in-progress"})
	done, _ := s.CreateTask(sc, NewTask{TaskID: "T2", Title: "done", Status: "complete"})
	status := func(id string) string {
		var st string
		_ = s.r.QueryRow(`SELECT status FROM task WHERE id = ?`, id).Scan(&st)
		return st
	}
	if _, err := s.w.Exec(`INSERT INTO roadmap_item (component_root, title, status, provenance, loop_slug, created_at, updated_at)
		VALUES ('/r','idea','running','authored','work','2026-01-01','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	roadmap := func() string {
		var st string
		_ = s.r.QueryRow(`SELECT status FROM roadmap_item WHERE loop_slug = 'work' OR title = 'idea'`).Scan(&st)
		return st
	}

	if err := s.SetLoopArchived(ctx, l.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if status(open.ID) != "skipped" || roadmap() != "cancelled" {
		t.Fatalf("an archive closes open work as not done: task %q roadmap %q", status(open.ID), roadmap())
	}
	if status(done.ID) != "complete" {
		t.Fatalf("finished work is never touched: %q", status(done.ID))
	}
	closed, _ := s.ListWorkClosures(ctx, "/r")
	if len(closed) != 2 || closed[0].Reason != ClosedLoopArchived {
		t.Fatalf("each closure says why: %+v", closed)
	}

	if err := s.SetLoopArchived(ctx, l.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	if status(open.ID) != "in-progress" || roadmap() != "running" {
		t.Fatalf("restoring opens the work again as it was: task %q roadmap %q", status(open.ID), roadmap())
	}
	if closed, _ := s.ListWorkClosures(ctx, "/r"); len(closed) != 0 {
		t.Fatalf("nothing stays closed after a restore: %+v", closed)
	}

	if _, err := s.DeleteLoopKeepingStats(ctx, l.ID, "user:me", ""); err != nil {
		t.Fatal(err)
	}
	if status(open.ID) != "skipped" || roadmap() != "cancelled" {
		t.Fatalf("a delete closes open work: task %q roadmap %q", status(open.ID), roadmap())
	}
	closed, _ = s.ListWorkClosures(ctx, "/r")
	if len(closed) != 2 || closed[0].Reason != ClosedLoopDeleted {
		t.Fatalf("a deleted loop's closures say so: %+v", closed)
	}
}

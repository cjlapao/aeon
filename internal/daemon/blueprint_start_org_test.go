package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// S38 T13 — starting a loop ON a goal or a task (OL9).

// The lanes PARK at a gate: what this task tests is the START — the
// requires check, the pin, the link. A model-step lane would keep the
// daemon runner emitting events after the test's store is closed (the
// runner outlives the test; a parked loop does not).
const requiresYAML = "id: org-needs\nschemaVersion: 1\nversion: 1\nrequires:\n  - any_of:\n      - {type: goal, state: approved}\n      - {type: task, state: approved}\nlanes:\n  - id: approve\n    step: {kind: gate, question: go}\nquestions:\n  go:\n    prompt: Continue?\n    answers: [yes, no]\n"

const plainYAML = "id: org-plain\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: approve\n    step: {kind: gate, question: go}\nquestions:\n  go:\n    prompt: Continue?\n    answers: [yes, no]\n"

func orgStartEnv(t *testing.T, raw string) (*Daemon, *store.Store, string, string) {
	t.Helper()
	s := recoveryStore(t)
	d := New(Options{Store: s})
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "org.yaml")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	// A "project" is a root component (S44-extra); alpha is a child of it.
	p, err := s.CreateComponent(store.NewComponent{Name: "org group", Slug: "org-group", Root: "/repo/org-group"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha", Root: root, ParentID: p.Component.ID}); err != nil {
		t.Fatal(err)
	}
	return d, s, path, root
}

// orgStart starts a loop and WAITS for it to park at its gate before the
// test may finish: the daemon runner is a goroutine that outlives the
// request, and a test that closes its store while a runner is mid-emit
// panics the binary (measured — the panic names nobody's test).
func orgStart(t *testing.T, d *Daemon, s *store.Store, p protocol.LoopStartParams) {
	t.Helper()
	if _, err := d.Table.StartBlueprintLoop(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		l, err := s.GetLoop(store.Scope{LoopSlug: p.Slug, ComponentRoot: p.WorkspaceRoot})
		if err == nil && (l.Status == "waiting" || l.Status == "blocked" || l.Status == "done") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("loop %s never parked: %+v", p.Slug, l)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func orgGoal(t *testing.T, s *store.Store, projectID, title, review string) store.Goal {
	t.Helper()
	g, rf, err := s.CreateGoal(store.NewGoal{ComponentID: projectID, Title: title, Type: "feature", BodyNote: "the body", SourceKind: "manual", SourceRef: "test", By: store.Actor{Kind: "user", ID: "operator"}, Submit: true})
	if err != nil || rf != nil {
		t.Fatalf("create goal: %v %v", rf, err)
	}
	if review == "approved" {
		// Goals are born in_review; the PERSON approves (OL8).
		if _, rf, err := s.DecideGoal(g.ID, g.Version, "approved", store.Actor{Kind: "user", ID: "operator"}, "ok"); err != nil || rf != nil {
			t.Fatalf("approve goal: %v %v", rf, err)
		}
		g.ReviewState = "approved"
	}
	return g
}

func TestUnmetRequiresCreatesNoLoopRow(t *testing.T) {
	d, s, path, root := orgStartEnv(t, requiresYAML)
	_, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: path, Goal: "do it", WorkspaceRoot: root, Slug: "never-born",
	})
	var pe *protocol.Error
	if err == nil || !errors.As(err, &pe) || pe.Code != protocol.ErrRequiresUnmet.Code {
		t.Fatalf("an unmet requires did not refuse with ErrRequiresUnmet: %v", err)
	}
	// The refusal carries the sentence AND the candidates (S39's dialogue
	// reads this instead of offering a dead end).
	if !strings.Contains(pe.Data.What, "goal") || !strings.Contains(pe.Data.What, "task") {
		t.Fatalf("the refusal does not name the alternatives: %q", pe.Data.What)
	}
	// No row was created — the slug the start would have used is absent.
	if _, err := s.GetLoop(store.Scope{LoopSlug: "never-born", ComponentRoot: root}); err == nil {
		t.Fatal("the loop row was created before the requires check")
	}
	// With an approved candidate on offer, the todo names it.
	p, _ := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	if len(p) == 0 {
		t.Fatal("the env lost its project")
	}
	orgGoal(t, s, p[0].ID, "the approved one", "approved")
	_, err = d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: path, Goal: "do it", WorkspaceRoot: root, Slug: "still-never",
	})
	if err == nil || !strings.Contains(err.Error()+func() string { var pe *protocol.Error; errors.As(err, &pe); return pe.Data.Todo }(), "the approved one") {
		t.Fatalf("the refusal did not offer the approved goal: %v", err)
	}
	if _, err := s.GetLoop(store.Scope{LoopSlug: "still-never", ComponentRoot: root}); err == nil {
		t.Fatal("the offered refusal still wrote a row")
	}
}

func TestStartWithGoalPinsVersion(t *testing.T) {
	d, s, path, root := orgStartEnv(t, requiresYAML)
	ps, _ := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	g := orgGoal(t, s, ps[0].ID, "frame the work", "approved")
	orgStart(t, d, s, protocol.LoopStartParams{Blueprint: path, Goal: "typed goal wins", WorkspaceRoot: root, Slug: "on-goal", GoalID: g.ID})
	l, err := s.GetLoop(store.Scope{LoopSlug: "on-goal", ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if l.GoalID != g.ID || l.GoalVersion != g.Version {
		t.Fatalf("the goal coordinates are not pinned: %+v", l)
	}
	// The person's typed goal beats the seeded one (P-goal: words typed
	// are words meant).
	if l.Goal != "typed goal wins" {
		t.Fatalf("the seeded prompt overwrote the typed one: %q", l.Goal)
	}
}

func TestStartSeedsPromptFromGoal(t *testing.T) {
	d, s, path, root := orgStartEnv(t, plainYAML)
	ps, _ := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	g := orgGoal(t, s, ps[0].ID, "seed me from the goal", "approved")
	orgStart(t, d, s, protocol.LoopStartParams{Blueprint: path, WorkspaceRoot: root, Slug: "seeded", GoalID: g.ID})
	l, err := s.GetLoop(store.Scope{LoopSlug: "seeded", ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(l.Goal, "seed me from the goal") || !strings.Contains(l.Goal, "the body") {
		t.Fatalf("the prompt was not seeded from the goal: %q", l.Goal)
	}
}

func TestStartWithTaskLinksWorker(t *testing.T) {
	d, s, path, root := orgStartEnv(t, requiresYAML)
	ps, _ := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	cs, err := s.ListComponents(store.ComponentFilter{ParentID: ps[0].ID})
	if err != nil || len(cs) == 0 {
		t.Fatalf("components: %v", err)
	}
	yes := true
	_ = yes
	task, rf, err := s.CreateManualTask(store.NewManualTask{ComponentID: cs[0].ID, Title: "the approved task", Type: "chore", RequireApproval: true, By: store.Actor{Kind: "user", ID: "operator"}})
	if err != nil || rf != nil {
		t.Fatalf("manual task: %v %v", rf, err)
	}
	if _, rf, err := s.DecideTask(task.ID, "approved", store.Actor{Kind: "user", ID: "operator"}, "ok"); err != nil || rf != nil {
		t.Fatalf("approve task: %v %v", rf, err)
	}
	orgStart(t, d, s, protocol.LoopStartParams{Blueprint: path, WorkspaceRoot: root, Slug: "on-task", TaskID: task.ID})
	// The worker row exists (read back through the board's own listing).
	rows, err := s.ListTaskRows(store.TaskFilter{ComponentID: cs[0].ID, Manual: &yes})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == task.ID {
			if len(r.LoopSlugs) != 1 || r.LoopSlugs[0] != "on-task" {
				t.Fatalf("the worker link is missing: %+v", r.LoopSlugs)
			}
			return
		}
	}
	t.Fatal("the task never showed on the board")
}

func TestNoRequiresNoIdsIsToday(t *testing.T) {
	d, s, path, root := orgStartEnv(t, plainYAML)
	orgStart(t, d, s, protocol.LoopStartParams{Blueprint: path, Goal: "plain", WorkspaceRoot: root, Slug: "plain-loop"})
	l, err := s.GetLoop(store.Scope{LoopSlug: "plain-loop", ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if l.GoalID != "" || l.GoalVersion != 0 {
		t.Fatalf("an unlinked start grew coordinates: %+v", l)
	}
}

func TestUnknownStartCoordinatesRefuse(t *testing.T) {
	d, _, path, root := orgStartEnv(t, plainYAML)
	_, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: path, Goal: "g", WorkspaceRoot: root, Slug: "bad-goal", GoalID: "goal-ghost",
	})
	var pe *protocol.Error
	if err == nil || !errors.As(err, &pe) || pe.Code != protocol.ErrStartLinkUnknown.Code || !strings.Contains(pe.Data.What, "goal-ghost") {
		t.Fatalf("an unknown goalId did not refuse by name: %v", err)
	}
	_, err = d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: path, Goal: "g", WorkspaceRoot: root, Slug: "bad-task", TaskID: "task-ghost",
	})
	if err == nil || !errors.As(err, &pe) || pe.Code != protocol.ErrStartLinkUnknown.Code || !strings.Contains(pe.Data.What, "task-ghost") {
		t.Fatalf("an unknown taskId did not refuse by name: %v", err)
	}
}

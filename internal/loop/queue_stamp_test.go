package loop

// S60 T6 — the engine's half of queue.enqueue: attendedness and the actor
// are stamped from the loop's PIN at dispatch (MQ13/MQ14.1), not from a
// handoff a model could compose. Three pins, three answers: attended true,
// attended false, attended never-declared (NULL) — the last must read as
// unattended, because "nobody said" is not a claim that somebody is here.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/store"
)

type recEnq struct {
	in     action.QueueEnqueueInput
	called int
}

func (r *recEnq) Enqueue(_ context.Context, in action.QueueEnqueueInput) (action.QueueEnqueueResult, error) {
	r.called++
	r.in = in
	return action.QueueEnqueueResult{ItemID: "mq-stamp", Status: "queued", Position: 3}, nil
}

func driveEnqueue(t *testing.T, slug string, attended *bool) *recEnq {
	t.Helper()
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "enqueue.yaml")
	writeBlueprint(t, path, "  - id: queue\n    step: {kind: code-action, action: queue.enqueue}\n")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Mode: "standard"},
		path, attended, map[string]string{"base_branch": "main", "position": "3"})
	if err != nil {
		t.Fatal(err)
	}
	e := &recEnq{}
	dep := Deps{
		Store:   s,
		Actions: func(string) *action.Services { return &action.Services{Queue: &action.QueueActions{Enq: e}} },
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	if e.called != 1 {
		t.Fatalf("the local action ran once: %d", e.called)
	}
	return e
}

func TestEnqueueAttendednessIsThePin(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		att  *bool
		want bool
	}{
		{"pin says attended", &yes, true},
		{"pin says unattended", &no, false},
		{"pin never said", nil, false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := driveEnqueue(t, "walk-enq-"+string(rune('a'+i)), tc.att)
			if e.in.Attended != tc.want {
				t.Fatalf("attendedness is the PIN read at dispatch, want %v got %v", tc.want, e.in.Attended)
			}
			if e.in.ActorKind != "blueprint" {
				t.Fatalf("the actor is the pinned blueprint (MQ14.1), never a model: %q", e.in.ActorKind)
			}
			// The identities are the loop's own: the branch pin and the
			// base pin, composed by code — nothing here came from prose.
			if e.in.ComponentRoot == "" || e.in.Branch == "" || e.in.BaseBranch != "main" {
				t.Fatalf("pin-fed identities: %+v", e.in)
			}
		})
	}
}

// TestLandWithoutTheBindingNeverAsks — an attended landing loop with no
// recorded verification does NOT get a question row: BuildAsk refuses to
// render an ask that could not say what it affects (MQ11 is exactly what
// the person would be vouching for), and the step blocks naming the
// missing binding. The ask a human cannot fact-check is never asked.
func TestLandWithoutTheBindingNeverAsks(t *testing.T) {
	f := forge.NewFake()
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "land.yaml")
	writeBlueprint(t, path, "  - id: land\n    step: {kind: code-action, action: forge.land}\n")
	attended := true
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "walk-land", ComponentRoot: root, Root: root, Mode: "standard"},
		path, &attended, map[string]string{
			"item": "mq-abc123", "integration_branch": "integrate/mq-abc123",
			"base_branch": "main",
		})
	if err != nil {
		t.Fatal(err)
	}
	dep := Deps{Store: s, Actions: func(string) *action.Services {
		return &action.Services{Forge: &action.ForgeActions{Forge: f}}
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" {
		t.Fatalf("an unbuildable land ask blocks, it does not ask: %s (%s)", saved.Status, saved.LastReason)
	}
	if saved.QuestionID != 0 {
		t.Fatalf("no question row was committed for an ask that cannot name its facts")
	}
	for _, want := range []string{"pr number", "verified sha"} {
		if !strings.Contains(saved.LastReason, want) {
			t.Fatalf("the block names the missing binding (%q):\n%s", want, saved.LastReason)
		}
	}
	for _, c := range f.Calls() {
		if c.Method == "MergePR" {
			t.Fatal("the forge was never asked")
		}
	}
}

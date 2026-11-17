package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// t6_walk_test.go is the DoD's walk, made runnable and then removed. It
// starts `brainstorm` with the t14 replay model, watches the capture walk
// END IN REST (idle) rather than die, sends `loop.message` "hello" through
// the socket door, and reads the agent's reply back out of the thread.

func TestT6WalkBrainstormRestsAndTalks(t *testing.T) {
	f, m := gatedFixture(t, []gatedTurn{{deltas: []string{`{"status":"ok","summary":"hello — the goal is captured. Anything else?"}`}}})
	s, root := f.s, f.root
	// Records need a registered component claiming the root (t14Env's shape).
	pr, err := s.CreateComponent(store.NewComponent{Name: "t6 walk", Slug: "t6-walk", Root: "/repo/t6-walk"})
	if err != nil || !pr.OK() {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha", Root: root, ParentID: pr.Component.ID}); err != nil {
		t.Fatal(err)
	}
	st, err := loop.Start(context.Background(), s, store.NewLoop{
		Slug: "capture", Title: "capture", Goal: "shape the org layers", Root: root, ComponentRoot: root, Mode: "standard",
	}, "brainstorm", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var goalID string
	var sid int64
	dep := loop.Deps{Store: s, Event: func(string, any) {}, Records: recordsReaderFor(s),
		Model: func(_ context.Context, state loop.LoopState, _ blueprint.Step, _ string, _ string) (loop.ModelResult, error) {
			sid = startLaneStep(t, s, "capture", root, "brainstormer")
			call, _ := callerFor(t, s, "brainstormer", root, sid)
			if c, bad := call("write", `{"path":"proposal.md","content":"# Proposal\n"} `); bad {
				t.Fatalf("write proposal: %s", c)
			}
			if c, bad := call("write", `{"path":"brief.md","content":"# Brief\n"}`); bad {
				t.Fatalf("write brief: %s", c)
			}
			c, bad := call("goal_create", `{"title":"ship the org layers","type":"feature"}`)
			if bad {
				t.Fatalf("goal_create: %s", c)
			}
			var out struct {
				Goal struct {
					ID string `json:"id"`
				} `json:"goal"`
			}
			if err := json.Unmarshal([]byte(c), &out); err != nil || out.Goal.ID == "" {
				t.Fatalf("goal_create content: %q (%v)", c, err)
			}
			goalID = out.Goal.ID
			for _, doc := range []string{"proposal.md", "brief.md"} {
				if c, bad := call("attach", `{"subject":{"kind":"goal","id":"`+goalID+`"},"document":"`+doc+`"}`); bad {
					t.Fatalf("attach %s: %s", doc, c)
				}
			}
			return loop.ModelResult{StepID: sid, Kind: "completed", Outcome: "ok"}, nil
		}}
	if err := loop.Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	// The runner's own close of the lane step row (step.go finishes what it
	// created); t14's harness never closed its row because it never asked
	// the loop a second question — the walk does, and the gate reads rows.
	if err := s.SetStepStatus(sid, store.StepDone, "ok"); err != nil {
		t.Fatal(err)
	}
	pin, err := loop.Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Status != "idle" {
		t.Fatalf("step 2 of the walk failed: brainstorm ended %s (%s), not idle", pin.Status, pin.LastReason)
	}
	t.Logf("WALK step 1-2: brainstorm capture run ended %s (run row: %+v)", pin.Status, func() string {
		r, _ := s.CurrentRun(st.ID)
		return r.State
	}())

	// Step 3: the person talks to the resting loop through the socket door.
	ctx := context.Background()
	initConn(t, f.d, ctx, "c-walk", "ui")
	sub := subscribe(t, f.d, "c-walk", protocol.SubscribeParams{})
	res := dispatchJSON(t, f, "c-walk", protocol.MethodLoopMessage, map[string]any{"id": st.ID, "text": "hello"})
	if res["turn"] != "started" {
		t.Fatalf("step 3 failed: loop.message answered %v", res)
	}
	waitTurnIdle(t, f.d, st.ID)

	// Step 4: the reply is IN the thread.
	rows, err := s.Thread(store.Scope{LoopSlug: "capture", ComponentRoot: root}, 0)
	if err != nil || len(rows) != 2 {
		t.Fatalf("step 4 failed: thread has %d rows (err %v)", len(rows), err)
	}
	t.Logf("WALK step 3-4: thread rows: [%s/%s %q] [%s/%s %q]; model calls %d",
		rows[0].Author, rows[0].Source, rows[0].Body, rows[1].Author, rows[1].Source, rows[1].Body, m.calls)
	if rows[0].Author != "human" || rows[0].Source != "user" || rows[0].Body != "hello" {
		t.Fatalf("the typed message lost its provenance: %+v", rows[0])
	}
	if rows[1].Author != "agent" || rows[1].Source != "" || rows[1].Body == "" {
		t.Fatalf("the agent's reply is not in the thread: %+v", rows[1])
	}
	_ = sub
}

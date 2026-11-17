package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
)

// S38 T14 — the two org blueprints, replayed. The scripted model walks every
// door a real turn crosses (grant list → toolset gate → Records invoker →
// identity stamping → rows), and the contract (T12) is what turns "the model
// said ok" into "the records exist".

func t14Env(t *testing.T) (*store.Store, string) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	root := t.TempDir()
	p, err := s.CreateComponent(store.NewComponent{Name: "t14 group", Slug: "t14-group", Root: "/repo/t14-group"})
	if err != nil || !p.OK() {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha", Root: root, ParentID: p.Component.ID}); err != nil {
		t.Fatal(err)
	}
	// S48 T3 (mechanical): names resolve from rows; these tests start
	// shipped blueprints through loop.Start on a bare store.
	seedEmbeddedRows(t, s, "brainstorm", "goal-to-brief")
	return s, root
}

// startLaneStep is the test's share of the runner's job: the step row the
// identity (OL3) is stamped from, written before the work starts.
func startLaneStep(t *testing.T, s *store.Store, slug, root, agent string) int64 {
	t.Helper()
	st, err := s.CreateStep(store.Scope{LoopSlug: slug, ComponentRoot: root}, store.NewStep{
		Phase: "build", Agent: agent, Status: store.StepRunning,
	})
	if err != nil {
		t.Fatalf("lane step row: %v", err)
	}
	return st.ID
}

func callerFor(t *testing.T, s *store.Store, agent, root string, stepID int64) (func(name, input string) (string, bool), []string) {
	t.Helper()
	granted, ok := handler.ToolsFor(agent)
	if !ok {
		t.Fatalf("no handler %q", agent)
	}
	r := StepRunner{Records: NewRecordsService(s).Invoker()}
	caller, _, err := r.BuildTools(granted, root, agent, runtime.FrozenManifest{})
	if err != nil {
		t.Fatalf("build tools for %s: %v", agent, err)
	}
	ctx := stepctx.WithStepID(context.Background(), stepID)
	return func(name, input string) (string, bool) {
		out := caller.Call(ctx, name, json.RawMessage(input))
		return out.Content, out.IsError
	}, granted
}

func TestBrainstormReplayCreatesGoalAndAttachments(t *testing.T) {
	s, root := t14Env(t)
	st, err := loop.Start(context.Background(), s, store.NewLoop{
		Slug: "capture", Title: "capture", Goal: "shape the org layers", Root: root, ComponentRoot: root, Mode: "standard",
	}, "brainstorm", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var goalID string
	dep := loop.Deps{Store: s, Event: func(string, any) {}, Records: recordsReaderFor(s),
		Model: func(_ context.Context, state loop.LoopState, _ blueprint.Step, _ string, _ string) (loop.ModelResult, error) {
			// The step row is the S5 bookkeeping the production runner does
			// before dispatch (step.go CreateRunningStep); this test owns the
			// runner, so it creates the row the identity is stamped from.
			sid := startLaneStep(t, s, "capture", root, "brainstormer")
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
	done, err := loop.Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	// S39 T6 (CR1) amended this line: brainstorm now carries `after:
	// converse`, so the capture walk ENDS IN REST — the goal and its files
	// are unchanged; what changed is that the conversation which produced
	// them may continue (loop.message) until a person closes the rest.
	if done.Status != "idle" {
		t.Fatalf("the capture lane did not rest: %s %s", done.Status, done.LastReason)
	}
	goal, err := s.GetGoal(goalID)
	if err != nil {
		t.Fatal(err)
	}
	if goal.ReviewState != "in_review" {
		t.Fatalf("the goal did not arrive in the review queue: %s", goal.ReviewState)
	}
	if goal.SourceLoop != "capture" {
		t.Fatalf("the goal is not the loop's own: %+v", goal)
	}
	docs, err := s.StepAttachments(done.LastStepID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(docs, ",") != "brief.md,proposal.md" {
		t.Fatalf("the attachments are not the written pair: %v", docs)
	}
}

func TestBrainstormWithoutGoalCreateRejects(t *testing.T) {
	s, root := t14Env(t)
	st, err := loop.Start(context.Background(), s, store.NewLoop{
		Slug: "files-only", Title: "files", Goal: "g", Root: root, ComponentRoot: root, Mode: "standard",
	}, "brainstorm", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dep := loop.Deps{Store: s, Event: func(string, any) {}, Records: recordsReaderFor(s),
		Model: func(_ context.Context, state loop.LoopState, _ blueprint.Step, _ string, _ string) (loop.ModelResult, error) {
			sid := startLaneStep(t, s, "files-only", root, "brainstormer")
			call, _ := callerFor(t, s, "brainstormer", root, sid)
			if c, bad := call("write", `{"path":"proposal.md","content":"# Proposal\n"}`); bad {
				t.Fatalf("write: %s", c)
			}
			if c, bad := call("write", `{"path":"brief.md","content":"# Brief\n"}`); bad {
				t.Fatalf("write: %s", c)
			}
			return loop.ModelResult{StepID: sid, Kind: "completed", Outcome: "ok"}, nil
		}}
	if err := loop.Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := loop.Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" || !strings.Contains(saved.LastReason, "declared record was not created") ||
		!strings.Contains(saved.LastReason, "goal_create") {
		t.Fatalf("files alone passed the capture contract: %+v", saved)
	}
	if saved.StepStates["capture"].Outcome != "reject" {
		t.Fatalf("the step outcome is %q, not reject", saved.StepStates["capture"].Outcome)
	}
	// And the database really is empty of the goal the lane never filed.
	goals, err := s.ListGoals(store.GoalFilter{})
	if err != nil || len(goals) != 0 {
		t.Fatalf("a goal appeared without goal_create: %v %v", goals, err)
	}
}

func TestGoalToBriefRefusedWithoutAnApprovedGoal(t *testing.T) {
	s, root := t14Env(t)
	d := New(Options{Store: s})
	p, _ := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	g, rf, err := s.CreateGoal(store.NewGoal{ComponentID: p[0].ID, Title: "unjudged", Type: "feature",
		SourceKind: "manual", SourceRef: "test", By: store.Actor{Kind: "user", ID: "operator"}, Submit: true})
	if err != nil || rf != nil {
		t.Fatalf("goal: %v %v", rf, err)
	}
	_, err = d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: "goal-to-brief", WorkspaceRoot: root, Slug: "early", GoalID: g.ID,
	})
	var pe *protocol.Error
	if err == nil || !errors.As(err, &pe) || pe.Code != protocol.ErrRequiresUnmet.Code {
		t.Fatalf("an in_review goal satisfied the requires clause: %v", err)
	}
	// The clause names what would satisfy it.
	if !strings.Contains(pe.Data.What, "goal") || !strings.Contains(pe.Data.What, "approved") {
		t.Fatalf("the sentence does not name the missing state: %q", pe.Data.What)
	}
}

// runGoalToBrief starts the loop through the socket door and STEERS the
// daemon's own runner (T13's lesson: a runner the test does not own outlives
// the store and panics on a closed pool in somebody else's test). The test
// replaces d.loopRunner with a runner over its scripted Deps before the
// start wakes it, so the lane runs on the scripted model and the daemon
// machinery — start, requires, links, answer, wake — stays the real thing.
func runGoalToBrief(t *testing.T, s *store.Store, root string, model func(loop.Deps) func(context.Context, loop.LoopState, blueprint.Step, string, string) (loop.ModelResult, error)) (string, *Daemon) {
	t.Helper()
	d := New(Options{Store: s})
	p, _ := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	g, rf, err := s.CreateGoal(store.NewGoal{ComponentID: p[0].ID, Title: "approved frame", Type: "feature",
		SourceKind: "manual", SourceRef: "test", By: store.Actor{Kind: "user", ID: "operator"}, Submit: true})
	if err != nil || rf != nil {
		t.Fatalf("goal: %v %v", rf, err)
	}
	if _, rf, err := s.DecideGoal(g.ID, g.Version, "approved", store.Actor{Kind: "user", ID: "operator"}, "ok"); err != nil || rf != nil {
		t.Fatalf("approve: %v %v", rf, err)
	}
	dep := loop.Deps{Store: s, Event: func(string, any) {}, Records: recordsReaderFor(s),
		Model: model(dep0(s))}
	d.loopRunner = loop.NewRunner(dep)
	yes := true
	started, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: "goal-to-brief", WorkspaceRoot: root, Slug: "briefing", GoalID: g.ID, Attended: &yes,
	})
	if err != nil {
		t.Fatal(err)
	}
	return started.(map[string]any)["id"].(string), d
}

// dep0 exists so the Model closure can capture the Deps it is dispatched
// under without the literal referencing itself.
func dep0(s *store.Store) loop.Deps {
	return loop.Deps{Store: s, Event: func(string, any) {}, Records: recordsReaderFor(s)}
}

func waitState(t *testing.T, s *store.Store, id string, want ...string) loop.LoopState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := loop.Pinned(context.Background(), s, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range want {
			if st.Status == w {
				return st
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("loop %s never reached %v (last: %s — %s)", id, want, st.Status, st.LastReason)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestApproveDecidesTheBrief(t *testing.T) {
	s, root := t14Env(t)
	id, d := runGoalToBrief(t, s, root, func(base loop.Deps) func(context.Context, loop.LoopState, blueprint.Step, string, string) (loop.ModelResult, error) {
		return func(_ context.Context, _ loop.LoopState, _ blueprint.Step, _ string, _ string) (loop.ModelResult, error) {
			sid := startLaneStep(t, s, "briefing", root, "brief-author")
			call, _ := callerFor(t, s, "brief-author", root, sid)
			if c, bad := call("write", `{"path":"brief.md","content":"# Brief\n"}`); bad {
				t.Fatalf("write: %s", c)
			}
			c, bad := call("brief_create", `{"scope":"the org layers","acceptance_criteria":["the pair is filed"],"document":"brief.md"}`)
			if bad {
				t.Fatalf("brief_create: %s", c)
			}
			return loop.ModelResult{StepID: sid, Kind: "completed", Outcome: "ok"}, nil
		}
	})
	st := waitState(t, s, id, "waiting", "blocked", "done")
	if st.Status != "waiting" || st.QuestionID == 0 {
		t.Fatalf("the lane did not park at the brief gate: %+v", st)
	}
	if _, err := d.Table.AnswerBlueprintLoop(context.Background(), protocol.LoopAnswerParams{ID: id, Answer: "approve"}); err != nil {
		t.Fatal(err)
	}
	b, err := s.LatestBrief(store.Scope{LoopSlug: "briefing", ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if b.ReviewState != "approved" {
		t.Fatalf("the person answered approve and the brief stayed %s", b.ReviewState)
	}
	rows, err := s.ListReviews("brief", "briefing")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ByKind != "user" || rows[0].ByID == "" {
		t.Fatalf("no single user review row: %+v", rows)
	}
	// The daemon's own runner finishes the loop after the decided gate.
	done := waitState(t, s, id, "done", "blocked")
	if done.Status != "done" {
		t.Fatalf("after the decided gate the loop should finish: %s %s", done.Status, done.LastReason)
	}
}

func TestReviseThenReissueMakesVersionTwo(t *testing.T) {
	s, root := t14Env(t)
	var pass int32
	id, d := runGoalToBrief(t, s, root, func(base loop.Deps) func(context.Context, loop.LoopState, blueprint.Step, string, string) (loop.ModelResult, error) {
		return func(_ context.Context, _ loop.LoopState, _ blueprint.Step, prompt, frame string) (loop.ModelResult, error) {
			n := atomic.AddInt32(&pass, 1)
			sid := startLaneStep(t, s, "briefing", root, "brief-author")
			call, _ := callerFor(t, s, "brief-author", root, sid)
			body := "# Brief\\n"
			scope := "the org layers"
			if n > 1 {
				if !strings.Contains(prompt+frame, "revise") {
					t.Fatalf("the rework context never arrived: %q", prompt+frame)
				}
				body = "# Brief v2\\n"
				scope = "the org layers v2"
			}
			if c, bad := call("write", `{"path":"brief.md","content":"`+body+`"}`); bad {
				t.Fatalf("write: %s", c)
			}
			c, bad := call("brief_create", `{"scope":"`+scope+`","acceptance_criteria":["the pair is filed"],"document":"brief.md"}`)
			if bad {
				t.Fatalf("brief_create: %s", c)
			}
			return loop.ModelResult{StepID: sid, Kind: "completed", Outcome: "ok"}, nil
		}
	})
	st := waitState(t, s, id, "waiting", "blocked", "done")
	if st.Status != "waiting" {
		t.Fatalf("not parked: %+v", st)
	}
	if _, err := d.Table.AnswerBlueprintLoop(context.Background(), protocol.LoopAnswerParams{ID: id, Answer: "revise"}); err != nil {
		t.Fatal(err)
	}
	// The reject route sends the brief lane around again (on_reject: goto
	// brief, with mode: revise) — the runner drives it; the second pass
	// files v2 and parks at the gate once more.
	st2 := waitState(t, s, id, "waiting", "blocked", "done")
	// waitState returns at the FIRST waiting it sees, which the answer
	// also briefly produces before the runner re-dispatches; require the
	// second version to have actually landed.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if b2, err := s.GetBrief(store.Scope{LoopSlug: "briefing", ComponentRoot: root}, 2); err == nil && b2.ReviewState == "in_review" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("v2 was never filed: %+v", st2)
		}
		time.Sleep(10 * time.Millisecond)
	}
	b1, err := s.GetBrief(store.Scope{LoopSlug: "briefing", ComponentRoot: root}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if b1.ReviewState != "in_review" {
		t.Fatalf("revise judged v1 — it must not decide anything: %s", b1.ReviewState)
	}
	final := waitState(t, s, id, "waiting", "blocked", "done")
	if final.Status != "waiting" || final.QuestionID == 0 {
		t.Fatalf("the re-issued brief never reached its gate again: %+v", final)
	}
}

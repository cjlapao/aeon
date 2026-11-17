package loop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// rest_test.go is S39 T4: the resting state. A converse blueprint's run
// ends and the loop RESTS — no loop.finished, one loop.run.finished, the
// run row closed with its snapshot; every other blueprint ends done with
// byte-identical events (D1). Recovery and startup leave idle alone (the
// daemon package pins that half; here: Decide and Run).

const restYAML = "id: rest\nschemaVersion: 1\nversion: 1\nafter: converse\nlanes:\n  - id: only\n    step: {kind: model-step, agent: default, produces: OUT.md}\n"

const plainYAML = "id: rest\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: only\n    step: {kind: model-step, agent: default, produces: OUT.md}\n"

func restWalk(t *testing.T, yaml string) (LoopState, []string, []map[string]any, *store.Store, int) {
	t.Helper()
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "rest.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "rest", ComponentRoot: root, Root: root, Goal: "g", Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	var payloads []map[string]any
	calls := 0
	dep := Deps{Store: s, Event: func(kind string, payload any) {
		events = append(events, kind)
		if fields, ok := payload.(map[string]any); ok {
			cp := map[string]any{"__kind": kind}
			for k, v := range fields {
				cp[k] = v
			}
			payloads = append(payloads, cp)
		}
	}, Model: func(_ context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		calls++
		if err := os.WriteFile(filepath.Join(root, step.Produces[0]), []byte("fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return ModelResult{StepID: 1, Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return st, events, payloads, s, calls
}

func TestAfterConverseEndsTheRunIdle(t *testing.T) {
	st, events, payloads, s, calls := restWalk(t, restYAML)
	if calls != 1 {
		t.Fatalf("model called %d times, want 1", calls)
	}
	pin, err := s.ReadPinnedLoop(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Loop.Status != StatusIdle {
		t.Fatalf("loop status is %q, want idle", pin.Loop.Status)
	}
	// Position: the run ended, so the cursor is empty — and yet the loop is
	// not done. That pairing IS CR1.
	if pin.Loop.Status == "done" {
		t.Fatal("a converse blueprint ended the LOOP; CR1 rests it")
	}
	run, err := s.CurrentRun(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "done" || run.EndedAt == "" {
		t.Fatalf("run row is %s/ended=%q, want done with ended_at set (EndRun settled it)", run.State, run.EndedAt)
	}
	if len(events) != 2 || events[0] != "loop.advanced" || events[1] != "loop.run.finished" {
		t.Fatalf("events are %v, want exactly [loop.advanced loop.run.finished] — no loop.finished for a resting loop", events)
	}
	if len(payloads) != 2 || payloads[1]["status"] != StatusIdle || payloads[1]["runSeq"] != 1 || payloads[1]["outcome"] != "done" {
		t.Fatalf("loop.run.finished payload is %v, want status=idle runSeq=1 outcome=done", payloads[1])
	}
	if payloads[1]["cost"] == nil {
		t.Fatal("loop.run.finished must carry the run's P32 cost floor")
	}
}

func TestWithoutAfterTheLoopStillEndsDone(t *testing.T) {
	st, events, payloads, s, _ := restWalk(t, plainYAML)
	pin, err := s.ReadPinnedLoop(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Loop.Status != "done" {
		t.Fatalf("loop status is %q, want done — D1: a blueprint that never opted in must be untouched", pin.Loop.Status)
	}
	if len(events) != 2 || events[0] != "loop.advanced" || events[1] != "loop.finished" {
		t.Fatalf("events are %v, want [loop.advanced loop.finished]", events)
	}
	// The payload is byte-for-byte today's: exactly {loopId, status}.
	p := payloads[1]
	if len(p) != 3 || p["__kind"] != "loop.finished" || p["loopId"] != st.ID || p["status"] != "done" {
		t.Fatalf("loop.finished payload drifted: %v, want exactly {loopId, status:done}", p)
	}
	if run, _ := s.CurrentRun(st.ID); run.State != "done" {
		t.Fatalf("run row %s, want done", run.State)
	}
}

func TestEveryShippedBlueprintWithoutAfterEndsDone(t *testing.T) {
	// endStatus is the ONLY terminal writer (DoD grep); this proves it
	// honours the key for the real corpus. Amended S39 T6 (naming the task):
	// brainstorm and chat now ship the key, so they rest — the pair ends
	// idle, every other walk ends done, and every walk, resting or not,
	// ends aborted after an abort (abort stays terminal, CR1).
	rested := map[string]bool{"brainstorm": true, "chat": true}
	for _, name := range blueprint.StandardNames() {
		raw, err := os.ReadFile(filepath.Join("..", "blueprint", "blueprints", name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		bp, err := blueprint.Load(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		wantEnd := "done"
		if rested[name] {
			wantEnd = "idle"
		}
		if got := endStatus(&bp, false); got != wantEnd {
			t.Errorf("%s ships after=%q yet a finished walk must end %s", name, bp.After, wantEnd)
		}
		if got := endStatus(&bp, true); got != "aborted" {
			t.Errorf("%s: an aborted walk must stay terminal, got %q", name, got)
		}
	}
}

func TestEndStatusAbortWinsOverRest(t *testing.T) {
	bp := &blueprint.Blueprint{ID: "x", After: blueprint.AfterConverse}
	if got := endStatus(bp, true); got != "aborted" {
		t.Fatalf("abort over a converse blueprint is %q, want aborted — CR1: a declined plan is a fact, not a rest", got)
	}
	if got := endStatus(bp, false); got != StatusIdle {
		t.Fatalf("finished converse walk is %q, want idle", got)
	}
	if got := endStatus(nil, false); got != "done" {
		t.Fatalf("nil blueprint is %q, want done (today's answer, unchanged)", got)
	}
}

func TestDecideIdleIsActIdle(t *testing.T) {
	bp := &blueprint.Blueprint{ID: "x", After: blueprint.AfterConverse, Lanes: []blueprint.Lane{{ID: "only"}}}
	if got := Decide(bp, LoopState{Status: StatusIdle}).Kind; got != ActIdle {
		t.Fatalf("Decide(idle) = %q, want idle — the guard sits before the empty-position ActDone", got)
	}
}

func TestRunStopsOnIdle(t *testing.T) {
	st, _, _, s, first := restWalk(t, restYAML)
	if first != 1 {
		t.Fatalf("setup: model ran %d times, want 1", first)
	}
	calls := 0
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, _ blueprint.Step, _ string, _ string) (ModelResult, error) {
		calls++
		return ModelResult{StepID: 1, Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatalf("Run on a resting loop: %v", err)
	}
	if calls != 0 {
		t.Fatalf("a resting loop dispatched the model %d times, want 0 — Run must stop on idle", calls)
	}
}

func TestRunStateForFailedFlag(t *testing.T) {
	rested := LoopState{Status: StatusIdle}
	rested.Flags = map[string]bool{"run-failed": true}
	if got := runStateFor(rested); got != "failed" {
		t.Fatalf("resting + run-failed maps to %q, want failed", got)
	}
	if got := runStateFor(LoopState{Status: "aborted"}); got != "cancelled" {
		t.Fatalf("aborted maps to %q, want cancelled (the run was called off)", got)
	}
	if got := runStateFor(LoopState{Status: "done"}); got != "done" {
		t.Fatalf("done maps to %q, want done", got)
	}
	// CR1's sentence in one case: a FAILED run on a converse blueprint rests
	// the LOOP (idle) while the run row says failed — the two states live
	// on different tables by design (DB6).
}

func TestFailItemRestsAnAfterConverseLoopAndMarksTheRunFailed(t *testing.T) {
	s := openLoopStore(t)
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "rest-fail.yaml")
	raw := "id: restfail\nschemaVersion: 1\nversion: 1\nafter: converse\nlanes:\n  - id: build\n    iterate: tasks\n    ladder:\n      - {id: write, kind: model-step, agent: default}\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "restfail", ComponentRoot: root, Root: root, Goal: "g", Mode: "standard"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: "restfail", ComponentRoot: root}
	if _, err := s.CreateTask(sc, store.NewTask{TaskID: "T1", Ord: 0, Title: "the only task"}); err != nil {
		t.Fatal(err)
	}
	bp, err := blueprint.Load([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	st.Tasks = []string{"T1"}
	st.Status = "running"
	pos := Position{Lane: "build", Item: "T1", Step: "write"}
	// The lane's items are ALL settled (T1 is the only one): failItem's
	// terminal branch — with after: converse the loop rests and the run
	// row says failed (escalate.go's one terminal write).
	if err := failItem(context.Background(), Deps{Store: s}, sc, &st, pos, &bp.Lanes[0]); err != nil {
		t.Fatalf("failItem: %v", err)
	}
	if st.Status != StatusIdle {
		t.Fatalf("fail-item on a converse blueprint wrote %q, want idle (CR1: a failed run rests the loop)", st.Status)
	}
	if !st.Flags["run-failed"] {
		t.Fatal("the resting loop's run must be marked run-failed — the run row says failed, the loop rests")
	}
	// And the non-resting twin is byte-identical to today: done, NO flag.
	st2 := st
	st2.Blueprint = &blueprint.Blueprint{ID: "restfail", Lanes: bp.Lanes}
	st2.Status = "running"
	delete(st2.Flags, "run-failed")
	if err := failItem(context.Background(), Deps{Store: s}, sc, &st2, pos, &bp.Lanes[0]); err != nil {
		t.Fatal(err)
	}
	if st2.Status != "done" || st2.Flags["run-failed"] {
		t.Fatalf("fail-item WITHOUT after: wrote %q flag=%v, want done and no flag — D1: saved JSON unchanged", st2.Status, st2.Flags["run-failed"])
	}
}

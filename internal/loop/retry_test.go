package loop

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/database"
	"github.com/cjlapao/aeon/internal/store"
)

// T10 — the historical missing-handler block, retried once. The population
// is S9-era loops that blocked on `unknown handler "…"` when the registry
// held two entries; the retries commit ONE transition and dispatch ONE step.

func blockLoop(t *testing.T, s *store.Store, id, agent string, mutate func(*LoopState)) LoopState {
	t.Helper()
	st, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	st.Status = "blocked"
	st.LastReason = `invalid params: unknown handler "` + agent + `"`
	if mutate != nil {
		mutate(&st)
	}
	raw, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(st.ID, st.Status, raw); err != nil {
		t.Fatal(err)
	}
	return st
}

func startRetryLoop(t *testing.T, agent string) (*store.Store, string) {
	t.Helper()
	s := openLoopStore(t)
	path := t.TempDir() + "/retry.yaml"
	writeBlueprint(t, path, "  - id: implement\n    step: {kind: model-step, agent: "+agent+"}\n")
	st, err := Start(context.Background(), s, store.NewLoop{Slug: "hist", ComponentRoot: "/repo", Goal: "ship it"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, st.ID
}

func TestHistoricalHandlerBlockRetry(t *testing.T) {
	s, id := startRetryLoop(t, "golang-engineer")
	blockLoop(t, s, id, "golang-engineer", nil)

	var events []string
	dispatches := 0
	dep := Deps{Store: s, Event: func(kind string, _ any) { events = append(events, kind) },
		Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
			dispatches++
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		}}
	res, err := RetryMissingHandlerBlock(context.Background(), dep, id)
	if err != nil {
		t.Fatalf("the historical block was refused: %v — the handler registered in T4; this is the exact population the retry exists for", err)
	}
	if dispatches != 1 {
		t.Errorf("the retry dispatched %d model steps, want exactly 1 — one transition, one dispatch", dispatches)
	}
	saved, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "done" {
		t.Errorf("after a successful retry the loop is %q (reason %q), want done — the ladder ran the step to its end", saved.Status, saved.LastReason)
	}
	if saved.LastReason != "" {
		t.Errorf("the durable block reason survived the retry: %q", saved.LastReason)
	}
	if !hasStr(events, "loop.retried") {
		t.Errorf("the retry emitted %v, with no loop.retried — the audit must be able to tell a retry from a first run", events)
	}
	if res.Status != "done" {
		t.Errorf("Result.Status = %q", res.Status)
	}
	// The completed receipt is never replayed: the same op again refuses.
	if _, err := RetryMissingHandlerBlock(context.Background(), dep, id); err == nil || !strings.Contains(err.Error(), "not-blocked") {
		t.Errorf("retrying a finished loop dispatched again: %v", err)
	}
	if dispatches != 1 {
		t.Errorf("the second call dispatched (total %d) — the receipt is not a suggestion", dispatches)
	}
}

func TestHandlerBlockRetryIsSingleDispatch(t *testing.T) {
	// Two requests — the app's two windows pressing the same button — must
	// commit one transition and dispatch one step. The second window meets
	// a state that already moved, and the state (not a lock) serialises it.
	s, id := startRetryLoop(t, "golang-engineer")
	blockLoop(t, s, id, "golang-engineer", nil)
	var events []string
	dispatches := 0
	dep := Deps{Store: s, Event: func(kind string, _ any) { events = append(events, kind) },
		Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
			dispatches++
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		}}
	first, err := RetryMissingHandlerBlock(context.Background(), dep, id)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RetryMissingHandlerBlock(context.Background(), dep, id)
	if err == nil || !strings.Contains(err.Error(), "not-blocked") {
		t.Fatalf("the second window got result %+v err %v — after the first transition the loop is no longer a blocked missing-handler case", second, err)
	}
	if dispatches != 1 {
		t.Errorf("%d dispatches for two requests — one step, once", dispatches)
	}
	if first.Status != "done" {
		t.Errorf("first result %q", first.Status)
	}
	n := 0
	for _, e := range events {
		if e == "loop.retried" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d loop.retried events for two requests — the audit says a retry happened once", n)
	}
}

func TestHandlerBlockRetryRefusals(t *testing.T) {
	type seed struct {
		agent   string // blueprint step agent
		block   string // agent named in the block text
		mutate  func(*LoopState)
		want    string // refusal Kind
		history bool   // rewrite the PIN to name the agent: the historical
		// population pinned before preflight knew these names (pin-time
		// validation would refuse task-coordinator today — Pinned reads the
		// pin as recorded, and retry must judge the pin, not re-litigate it)
	}
	cases := []seed{
		// The S9-era refusal keeps being tested with a name that will
		// NEVER ship (not registered, not in futureAgents, not one of the
		// follow-up five): loop-doctor. The historical task-coordinator
		// case moved to eligibility below, 2026-10-26 (T12).
		{agent: "loop-doctor", block: "loop-doctor", want: "handler-still-missing", history: true},
		// T12 flipped S9: the body shipped, so the historical block is
		// ELIGIBLE — the one transition commits and the one step dispatches
		// (want "" asserts no refusal at all; the runner below checks the
		// dispatch happened exactly once).
		{agent: "task-coordinator", block: "task-coordinator", want: "", history: true},
		// An unrelated block stays untouched: a provider failure is not a
		// missing handler and has its own remedy.
		{agent: "golang-engineer", block: "", want: "not-missing-handler"},
		// The block names one agent, the position holds another: someone
		// edited history, or the ladder moved — do not dispatch on a
		// mismatched story.
		{agent: "python-engineer", block: "golang-engineer", want: "agent-mismatch"},
		{agent: "golang-engineer", block: "golang-engineer", mutate: func(st *LoopState) { st.StepInFlight = true }, want: "step-in-flight"},
		{agent: "golang-engineer", block: "golang-engineer", mutate: func(st *LoopState) {
			st.StepStates["implement"] = StepState{Status: "done", Outcome: "ok", StepID: 7}
		}, want: "receipt-exists"},
	}
	for _, tc := range cases {
		name := tc.want
		if name == "" {
			name = "eligible-dispatches-once"
		}
		if tc.block == "" {
			name = "not-a-missing-handler-block"
		}
		t.Run(name, func(t *testing.T) {
			seedAgent := tc.agent
			if tc.history {
				seedAgent = "golang-engineer" // pin-time validation would refuse some history names; rewrite the pin below
			}
			s, id := startRetryLoop(t, seedAgent)
			if tc.history && tc.agent != "golang-engineer" {
				db, err := sql.Open(database.DriverName, s.Path())
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				var content string
				if err := db.QueryRow(`SELECT content FROM loop_blueprint WHERE loop_id=?`, id).Scan(&content); err != nil {
					t.Fatal(err)
				}
				content = strings.ReplaceAll(content, "agent: golang-engineer", "agent: "+tc.agent)
				if _, err := db.Exec(`UPDATE loop_blueprint SET content=? WHERE loop_id=?`, content, id); err != nil {
					t.Fatal(err)
				}
			}
			st, err := Pinned(context.Background(), s, id)
			if err != nil {
				t.Fatal(err)
			}
			st.Status = "blocked"
			if tc.block == "" {
				st.LastReason = "provider failed: upstream 503"
			} else {
				st.LastReason = `invalid params: unknown handler "` + tc.block + `"`
			}
			if tc.mutate != nil {
				tc.mutate(&st)
			}
			raw, err := st.RuntimeJSON()
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SavePinnedRuntime(st.ID, st.Status, raw); err != nil {
				t.Fatal(err)
			}
			before := string(raw)
			dispatches := 0
			dep := Deps{Store: s, Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
				dispatches++
				return ModelResult{Kind: "completed", Outcome: "ok"}, nil
			}}
			_, err = RetryMissingHandlerBlock(context.Background(), dep, id)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("T12 shipped the body — this block is eligible, the refusal came anyway: %v", err)
				}
				if dispatches != 1 {
					t.Fatalf("dispatches = %d, want exactly 1 — the retry dispatches the one step, no replay", dispatches)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal was %v, want Kind %q", err, tc.want)
			}
			if dispatches != 0 {
				t.Errorf("a refused retry dispatched %d steps — refusals must leave the loop UNTOUCHED", dispatches)
			}
			saved, err := Pinned(context.Background(), s, id)
			if err != nil {
				t.Fatal(err)
			}
			after, err := saved.RuntimeJSON()
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != before {
				t.Errorf("the refusal changed the loop state\nbefore: %s\nafter:  %s", before, after)
			}
		})
	}
	// The unreadable pinned source leaves the loop unchanged with a clear
	// error (the store's own words, not a retry-specific fiction).
	t.Run("unreadable-pin", func(t *testing.T) {
		s, id := startRetryLoop(t, "golang-engineer")
		blockLoop(t, s, id, "golang-engineer", nil)
		db, err := sql.Open(database.DriverName, s.Path())
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := db.Exec(`UPDATE loop_blueprint SET content=? WHERE loop_id=?`, `broken: [`, id); err != nil {
			t.Fatal(err)
		}
		dep := Deps{Store: s, Model: func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error) {
			t.Error("dispatched with an unreadable pin")
			return ModelResult{Kind: "completed", Outcome: "ok"}, nil
		}}
		if _, err := RetryMissingHandlerBlock(context.Background(), dep, id); err == nil {
			t.Fatal("retry proceeded with an unreadable pinned blueprint")
		}
	})
}

func TestMissingHandlerBlockClassifier(t *testing.T) {
	st := LoopState{Status: "blocked"}
	st.LastReason = `invalid params: unknown handler "task-coordinator"`
	agent, ok := st.MissingHandlerBlock()
	if !ok || agent != "task-coordinator" {
		t.Errorf("classifier = %q, %v", agent, ok)
	}
	st.Status = "running"
	if _, ok := st.MissingHandlerBlock(); ok {
		t.Error("a running loop classified as a missing-handler block")
	}
	st = LoopState{Status: "blocked"}
	st.LastReason = `unknown handler "`
	if _, ok := st.MissingHandlerBlock(); ok {
		t.Error("a truncated marker classified as a block — a name must be there to name it")
	}
	st = LoopState{Status: "blocked"}
	st.LastReason = "gate answer revise: reject has no rework target"
	if _, ok := st.MissingHandlerBlock(); ok {
		t.Error("an unrelated block classified")
	}
}

func hasStr(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

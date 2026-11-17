package loop

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// S17 T4 — exhaustion: what happens when a task's budget runs out. S8 made
// `then:` REQUIRED because its absence was "an infinite loop waiting to
// happen"; this task makes all three answers real. The failure mode of a
// half-implemented `then:` is silent: the loop either spins, or dies with a
// message that conflates "the cap is too low" with "nobody was there to
// ask" — and those two facts call for opposite responses (O1).

func exhaustionBlueprint(t *testing.T, then string) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "exhaust.yaml")
	writeBlueprint(t, path, "  - id: build\n    iterate: tasks\n    rework: implement\n    on_reject: { counter: retries, max: 2, then: "+then+" }\n    ladder:\n      - {id: write, kind: model-step, agent: default}\n      - {id: implement, kind: model-step, agent: default}\n      - {id: verify, kind: model-step, agent: default}\n")
	return path, root
}

func startExhaustLoop(t *testing.T, s *store.Store, path, root, slug string, attended *bool, deps map[string][]string, tasks ...string) string {
	t.Helper()
	if len(tasks) == 0 {
		tasks = []string{"T1"}
	}
	st, err := Start(context.Background(), s, store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "the --format flag", Mode: "standard"}, path, attended, nil)
	if err != nil {
		t.Fatal(err)
	}
	st.Tasks = tasks
	// The rows the per-task budgets live on. In production these are
	// authored by the orchestrator (internal/agentsvc), not by loop.Start
	// — FINDING T4: an engine loop whose task rows are absent gets a
	// typed not-found and blocks rather than counting a budget in memory.
	// The harness seeds them the way agentsvc would; rows seeded earlier
	// (with DependsOn edges) are left exactly as they are.
	sc := store.Scope{LoopSlug: slug, ComponentRoot: root}
	existing, err := s.ListTasks(sc)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, tk := range existing {
		have[tk.TaskID] = true
	}
	for i, id := range tasks {
		if have[id] {
			continue
		}
		if _, err := s.CreateTask(sc, store.NewTask{TaskID: id, Ord: i, Title: "task " + id, DependsOn: deps[id]}); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePinnedRuntime(st.ID, "running", raw); err != nil {
		t.Fatal(err)
	}
	return st.ID
}

// alwaysReject verifies forever; everything else passes.
func alwaysReject() ModelRunner {
	return func(_ context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		if step.ID == "verify" {
			return ModelResult{Kind: "completed", Outcome: "reject", Reason: "field 'generated_at' missing"}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}
}

// TestEscalateCollapsesToBlockUnattended — L29/D4: escalating to a human
// nobody can reach is a loop that stops without saying why. Same collapse
// as G7's ask → deny, for the same reason, consistent so nobody has to
// remember two rules.
func TestEscalateCollapsesToBlockUnattended(t *testing.T) {
	s := openLoopStore(t)
	path, root := exhaustionBlueprint(t, "escalate")
	id := startExhaustLoop(t, s, path, root, "esc-unattended", nil, nil)
	dep := Deps{Store: s, Model: alwaysReject()}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" {
		t.Fatalf("escalate unattended settled as %q, want blocked (L29: the collapse)", saved.Status)
	}
	for _, clause := range []string{"T1 retries 2/2 exhausted at 'verify'", "then: escalate", "collapsed to block", "unattended"} {
		if !strings.Contains(saved.LastReason, clause) {
			t.Fatalf("the blocked reason %q lacks %q — D4's both-clauses sentence", saved.LastReason, clause)
		}
	}
}

// TestEscalateAttendedWaits — the same edge, attended: a question is
// created and the loop WAITS. The collapse exists precisely because nobody
// is listening when unattended; when somebody is, escalation is a question,
// not a block.
func TestEscalateAttendedWaits(t *testing.T) {
	s := openLoopStore(t)
	path, root := exhaustionBlueprint(t, "escalate")
	yes := true
	id := startExhaustLoop(t, s, path, root, "esc-attended", &yes, nil)
	dep := Deps{Store: s, Model: alwaysReject()}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "waiting" {
		t.Fatalf("attended escalation settled as %q, want waiting", saved.Status)
	}
	q, found, err := s.PendingQuestion(store.Scope{LoopSlug: "esc-attended", ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if !found || !strings.Contains(q.Body, "exhausted") {
		t.Fatalf("no escalation question is pending — a human cannot answer what was never asked: %+v found=%v", q, found)
	}
}

// TestBlockDistinguishesCapFromNobodyToAsk — O1: two facts call for
// opposite responses (raise the cap / run attended). Both clauses when
// both apply (pinned above); only the relevant one when only one does:
// `then: block` never went near a human, so it must NOT claim a collapse.
func TestBlockDistinguishesCapFromNobodyToAsk(t *testing.T) {
	s := openLoopStore(t)
	path, root := exhaustionBlueprint(t, "block")
	// Unattended AND then: block — the case where the collapse clause must
	// NOT appear: nothing escalated, so nothing collapsed (O1).
	id := startExhaustLoop(t, s, path, root, "blk-reason", nil, nil)
	dep := Deps{Store: s, Model: alwaysReject()}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" {
		t.Fatalf("then: block settled as %q, want blocked", saved.Status)
	}
	if !strings.Contains(saved.LastReason, "T1 retries 2/2 exhausted at 'verify'") || !strings.Contains(saved.LastReason, "then: block") {
		t.Fatalf("the cap clause is missing from %q", saved.LastReason)
	}
	for _, wrong := range []string{"collapsed", "unattended", "escalate"} {
		if strings.Contains(saved.LastReason, wrong) {
			t.Fatalf("the reason claims %q — nothing collapsed and nothing escalated here; conflating the facts sends someone to change the wrong thing (O1)", wrong)
		}
	}
}

// TestFailItemContinuesToTheNext — the one worth naming: in an iterating
// lane, one task that cannot converge must not stop the other four.
func TestFailItemContinuesToTheNext(t *testing.T) {
	s := openLoopStore(t)
	path, root := exhaustionBlueprint(t, "fail-item")
	id := startExhaustLoop(t, s, path, root, "failitem", nil, nil, "T1", "T2")
	var seen []string
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, prompt, frame string) (ModelResult, error) {
		seen = append(seen, step.ID+"|"+prompt+frame)
		if step.ID == "verify" && strings.Contains(prompt+frame, "T1") {
			return ModelResult{Kind: "completed", Outcome: "reject", Reason: "field missing"}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	sawT2 := false
	for _, s := range seen {
		if strings.HasPrefix(s, "write|") && strings.Contains(s, "T2") {
			sawT2 = true
		}
	}
	if !sawT2 {
		t.Fatalf("T2 never ran — fail-item stopped the loop instead of the item: %v", seen)
	}
	saved, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "done" {
		t.Fatalf("loop settled as %q (%s), want done — the healthy sibling finishes", saved.Status, saved.LastReason)
	}
}

// TestSiblingsKeepTheirBudgets — T2's split, observed through exhaustion:
// after T1 spends its retries, T2's budget starts FULL. This is D2
// decided at the engine's routing — not merely stored per-task.
func TestSiblingsKeepTheirBudgets(t *testing.T) {
	s := openLoopStore(t)
	path, root := exhaustionBlueprint(t, "fail-item")
	id := startExhaustLoop(t, s, path, root, "budgets", nil, nil, "T1", "T2")
	var t2Prompts []string
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, prompt, frame string) (ModelResult, error) {
		if step.ID == "implement" && strings.Contains(prompt+frame, "T2") {
			t2Prompts = append(t2Prompts, prompt+"\n"+frame)
		}
		if step.ID == "verify" {
			return ModelResult{Kind: "completed", Outcome: "reject", Reason: "field missing"}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	// T1 exhausted its budget (fail-item), so T2 must have run and
	// received its own rejections. Its FIRST REDO — the second implement
	// dispatch, the first carrying no redo sentence — says "retry 1 of 2":
	// proof the counters are per-task where it counts: routing. (Asserting
	// on t2Prompts[0] would have failed for the right answer — the first
	// run is not a redo.)
	if len(t2Prompts) < 2 {
		t.Fatalf("T2 never got a rework dispatch — its budget was charged by T1: %d", len(t2Prompts))
	}
	if !strings.Contains(t2Prompts[1], "retry 1 of 2") {
		t.Fatalf("T2's first redo says %q, not 'retry 1 of 2' — D2 fails at the engine, not the store", t2Prompts[1])
	}
}

// TestDependentsOfAFailedItemAreBlocked — the one that would be missed.
// The obvious implementation marks everything downstream failed; a report
// saying five tasks failed when one did is a report nobody can act on.
// They never ran: blocked, not failed (O5, C9).
func TestDependentsOfAFailedItemAreBlocked(t *testing.T) {
	s := openLoopStore(t)
	path, root := exhaustionBlueprint(t, "fail-item")
	slug := "deps"
	// Edges ride the seed itself, after Start created the loop row —
	// task rows carry a foreign key to the loop, so authoring them before
	// it is the constraint's refusal, not a merge.
	id := startExhaustLoop(t, s, path, root, slug, nil, map[string][]string{"T2": {"T1"}, "T3": {"T2"}}, "T1", "T2", "T3")
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, prompt, frame string) (ModelResult, error) {
		if step.ID == "verify" && strings.Contains(prompt+frame, "T1") {
			return ModelResult{Kind: "completed", Outcome: "reject", Reason: "field missing"}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: slug, ComponentRoot: root}
	tasks, err := s.ListTasks(sc)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for _, tk := range tasks {
		byID[tk.TaskID] = tk.Status
	}
	if byID["T1"] != "failed" {
		t.Fatalf("T1 = %q, want failed", byID["T1"])
	}
	for _, who := range []string{"T2", "T3"} {
		if byID[who] == "failed" {
			t.Fatalf("%s marked FAILED — it never ran; that is a report nobody can act on (O5)", who)
		}
		if byID[who] != "blocked" {
			t.Fatalf("%s = %q, want blocked (transitive dependent of a failed item)", who, byID[who])
		}
	}
}

// TestBlockStopsTheLoop — `then: block` stops the LOOP (contrast with
// fail-item): no advance past the exhausted item, no further dispatches.
func TestBlockStopsTheLoop(t *testing.T) {
	s := openLoopStore(t)
	path, root := exhaustionBlueprint(t, "block")
	id := startExhaustLoop(t, s, path, root, "blk-stop", nil, nil, "T1", "T2")
	models := 0
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, _ string, _ string) (ModelResult, error) {
		models++
		if step.ID == "verify" {
			return ModelResult{Kind: "completed", Outcome: "reject", Reason: "field missing"}, nil
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" {
		t.Fatalf("settled as %q, want blocked", saved.Status)
	}
	// write,implement,verify,implement,verify,implement,verify — then stop.
	if models != 7 {
		t.Fatalf("%d dispatches after a block — the loop kept advancing (T2 would have run): %d", models, models)
	}
}

// TestLoopCeilingBlocks — D3/Q11: reaching total_failed_gates is a BLOCK,
// not an escalation. The loop as a whole is not converging, which is a
// different problem from one task struggling, and a human answering a
// question would not fix it. Five tasks failing two gates each pass the
// ceiling of ten while every per-task budget still has room.
func TestLoopCeilingBlocks(t *testing.T) {
	s := openLoopStore(t)
	path, root := exhaustionBlueprint(t, "fail-item")
	id := startExhaustLoop(t, s, path, root, "ceiling", nil, nil, "T1", "T2", "T3", "T4", "T5")
	// each task: verify rejects once (charge), then ok — 5 charges is not
	// enough; reject the first TWO verifies per task (10 charges).
	verifies := map[string]int{}
	dep := Deps{Store: s, Model: func(_ context.Context, _ LoopState, step blueprint.Step, prompt, frame string) (ModelResult, error) {
		if step.ID == "verify" {
			item := "T1"
			for _, cand := range []string{"T1", "T2", "T3", "T4", "T5"} {
				if strings.Contains(prompt+frame, cand) {
					item = cand
					break
				}
			}
			verifies[item]++
			if verifies[item] <= 2 {
				return ModelResult{Kind: "completed", Outcome: "reject", Reason: "field missing"}, nil
			}
		}
		return ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if err := Run(context.Background(), dep, id); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "blocked" {
		t.Fatalf("settled as %q, want blocked by the loop ceiling (D3)", saved.Status)
	}
	if !strings.Contains(saved.LastReason, "total_failed_gates 10/10") || !strings.Contains(saved.LastReason, "not converging") {
		t.Fatalf("the ceiling sentence is missing or wrong: %q", saved.LastReason)
	}
	if strings.Contains(saved.LastReason, "escalate") {
		t.Fatalf("the ceiling escalated (%q) — Q11 decided it BLOCKS: a human answering a question would not fix a loop that is not converging", saved.LastReason)
	}
}

// TestAllThreeThenValuesAreReachable — S8 made `then:` REQUIRED on any
// capped edge; a required value with an unreachable option is a validator
// that accepts blueprints whose loops then do something nobody specified.
// Each of the three gets its own named scenario above; this pins that each
// TERMINATES in its specified shape, by name.
func TestAllThreeThenValuesAreReachable(t *testing.T) {
	cases := []struct {
		then     string
		wantName string
		attended *bool
		check    func(t *testing.T, s *store.Store, root, slug string, dep Deps)
	}{
		{"block", "TestBlockStopsTheLoop", nil, func(t *testing.T, s *store.Store, root, slug string, dep Deps) {}},
		{"fail-item", "TestFailItemContinuesToTheNext", nil, func(t *testing.T, s *store.Store, root, slug string, dep Deps) {}},
		{"escalate", "TestEscalateAttendedWaits", boolPtr(true), func(t *testing.T, s *store.Store, root, slug string, dep Deps) {}},
	}
	for _, tc := range cases {
		t.Run(tc.then, func(t *testing.T) {
			s := openLoopStore(t)
			path, root := exhaustionBlueprint(t, tc.then)
			id := startExhaustLoop(t, s, path, root, "reach-"+tc.then, tc.attended, nil)
			dep := Deps{Store: s, Model: alwaysReject()}
			if err := Run(context.Background(), dep, id); err != nil {
				t.Fatal(err)
			}
			saved, err := Pinned(context.Background(), s, id)
			if err != nil {
				t.Fatal(err)
			}
			switch tc.then {
			case "block", "escalate":
				if tc.attended != nil && *tc.attended {
					if saved.Status != "waiting" {
						t.Fatalf("%s attended settled as %q, want waiting (named by %s)", tc.then, saved.Status, tc.wantName)
					}
				} else if saved.Status != "blocked" {
					t.Fatalf("%s settled as %q, want blocked (named by %s)", tc.then, saved.Status, tc.wantName)
				}
			case "fail-item":
				if saved.Status == "blocked" {
					t.Fatalf("fail-item blocked the loop (%s), want continuation (named by %s)", saved.LastReason, tc.wantName)
				}
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

package store

import (
	"testing"
)

// agent_tasks_test.go is the ledger side of migration 0022: the transaction
// that makes an async creation atomic, the race nets, and the tamper read.
// The domain side (suspension semantics, no promotion, policy freezing) lives
// in internal/agentsvc's task_test.go; this file owns the raw handle.

// openAgentTaskStore rides the package's own openTemp (D19: no test names a
// path outside t.TempDir()).
func openAgentTaskStore(t *testing.T) *Store { return openTemp(t) }

func agentStep(t *testing.T, s *Store) (Scope, int64) {
	t.Helper()
	sc := seedLoop(t, s, "delegation", "/repo")
	step, err := s.CreateStep(sc, NewStep{Phase: "model", Agent: "golang-engineer", Attempt: 1, Status: StepRunning, Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return sc, step.ID
}

func newAsyncTask(stepID int64) NewAgentTask {
	return NewAgentTask{
		CallerLoopID: "L1", CallerLane: "work", CallerStepID: stepID,
		CalleeAgent: "reddit-agent", SkillID: "summarise-sentiment",
		Mode: "async", Status: "waiting",
		ParamsJSON: `{"query":"aeon"}`, PoliciesJSON: `[]`, PoliciesHash: "abc",
	}
}

func TestMigration0022Shape(t *testing.T) {
	s := openAgentTaskStore(t)
	for _, table := range []string{"agent_task", "agent_chain"} {
		var n int
		if err := s.r.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("0022 must create %s: %d %v", table, n, err)
		}
	}
	var indexed int
	if err := s.r.QueryRow(`SELECT count(DISTINCT tbl_name) FROM sqlite_master WHERE type='index' AND (tbl_name='agent_task' OR tbl_name='agent_chain')`).Scan(&indexed); err != nil || indexed != 2 {
		t.Fatalf("both tables must carry their indexes: %d %v", indexed, err)
	}
	// The vocabulary CHECKs reject invention at the door.
	s2 := openAgentTaskStore(t)
	_, stepID := agentStep(t, s2)
	in := newAsyncTask(stepID)
	in.Mode = "sideways"
	if _, err := s2.SuspendStepForTask(Scope{LoopSlug: "delegation", ComponentRoot: "/repo"}, in, "x"); err == nil {
		t.Fatal("the mode CHECK must refuse a value outside sync/async")
	}
}

func TestSuspendStepForTaskIsAtomic(t *testing.T) {
	s := openAgentTaskStore(t)
	sc, stepID := agentStep(t, s)
	task, err := s.SuspendStepForTask(sc, newAsyncTask(stepID), "awaiting task · reddit-agent/summarise-sentiment · step 1")
	if err != nil {
		t.Fatal(err)
	}
	step, err := s.ReadStep(stepID)
	if err != nil || step.Status != "suspended" {
		t.Fatalf("the step row must be suspended in the same transaction: %#v %v", step, err)
	}
	q, ok, err := s.PendingStepQuestion(stepID)
	if err != nil || !ok || q.ID != *task.QuestionID {
		t.Fatalf("the question must exist and belong to the task row: %v %v", ok, err)
	}
	// The thread entry arrived with the question (D16 as a pair).
	entries, err := s.Thread(sc, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Kind == "question" && e.QuestionID != nil && *e.QuestionID == q.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the wait must be visible in the thread — a person can see what the step is waiting on")
	}
	// The race net: a step that is not running cannot suspend, and nothing of
	// the refused attempt remains.
	if _, err := s.SuspendStepForTask(sc, newAsyncTask(stepID), "again"); err == nil {
		t.Fatal("suspending a suspended step must fail on the guard")
	}
	var tasks int
	if err := s.r.QueryRow(`SELECT count(*) FROM agent_task WHERE caller_step_id=?`, stepID).Scan(&tasks); err != nil || tasks != 1 {
		t.Fatalf("the refused suspension must have rolled back completely: %d %v", tasks, err)
	}
	var qs int
	if err := s.r.QueryRow(`SELECT count(*) FROM question WHERE step_id=?`, stepID).Scan(&qs); err != nil || qs != 1 {
		t.Fatalf("the refused suspension left a question behind: %d %v", qs, err)
	}
}

func TestMarkAgentTaskTerminalIsOnce(t *testing.T) {
	s := openAgentTaskStore(t)
	sc, stepID := agentStep(t, s)
	if _, err := s.SuspendStepForTask(sc, newAsyncTask(stepID), "awaiting"); err != nil {
		t.Fatal(err)
	}
	moved, err := s.MarkAgentTaskTerminal(1, "waiting", "completed", `{"outcome":"ok","result":"done"}`)
	if err != nil || !moved {
		t.Fatalf("the first completion must land: %v %v", moved, err)
	}
	moved, err = s.MarkAgentTaskTerminal(1, "waiting", "completed", `{"outcome":"ok","result":"again"}`)
	if err != nil || moved {
		t.Fatalf("the second completion must be visible as a non-move: %v %v", moved, err)
	}
	// The completion CHECK: completed_at arrives with the terminal status and
	// only then.
	row, err := s.GetAgentTask(1)
	if err != nil || row.Status != "completed" || row.CompletedAt == "" || row.ResultJSON == "" {
		t.Fatalf("terminal shape wrong: %#v %v", row, err)
	}
}

func TestTaskPolicyRowIsReadVerbatim(t *testing.T) {
	// The store is a faithful mirror: what the frozen bytes were at write time
	// is what a resumed evaluation reads — tampered by hand or not (detection
	// is the agentsvc hash's job; verbatim-ness is the store's).
	s := openAgentTaskStore(t)
	sc, stepID := agentStep(t, s)
	in := newAsyncTask(stepID)
	in.PoliciesJSON = `[{"id":"p-1","type":"skill_access","target":{"kind":"agent","id":"golang-engineer"},"action":"deny","spec":{},"source":"operator","not_after":null}]`
	if _, err := s.SuspendStepForTask(sc, in, "awaiting"); err != nil {
		t.Fatal(err)
	}
	row, err := s.GetAgentTask(1)
	if err != nil || row.PoliciesJSON != in.PoliciesJSON {
		t.Fatalf("the frozen set must survive the round trip verbatim: %q", row.PoliciesJSON)
	}
	if _, err := s.w.Exec(`UPDATE agent_task SET policies_json='[]' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	tampered, err := s.GetAgentTask(1)
	if err != nil || tampered.PoliciesJSON != "[]" || tampered.PoliciesHash != "abc" {
		t.Fatalf("the tampered row must read back as tampered, hash intact: %#v %v", tampered, err)
	}
}

func TestChainTableIsReadyForT5(t *testing.T) {
	// The edge table arrives with 0022 (the number is spent once). One row
	// proves the shape; T5 puts it to work.
	s := openAgentTaskStore(t)
	_, stepID := agentStep(t, s)
	if _, err := s.w.Exec(`INSERT INTO agent_chain (caller_loop_id, caller_step_id, callee_agent, skill_id, depth, created_at)
		VALUES ('L1', ?, 'reddit-agent', 'summarise-sentiment', 1, '2026-10-25T00:00:00Z')`, stepID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`INSERT INTO agent_chain (caller_loop_id, caller_step_id, callee_agent, skill_id, depth, created_at)
		VALUES ('L1', ?, 'reddit-agent', 'summarise-sentiment', 0, '2026-10-25T00:00:00Z')`, stepID); err == nil {
		t.Fatal("depth >= 1 is the whole meaning of an edge — zero must be refused")
	}
}

// ── T4: the chain edge the invoker writes before the callee runs ────────────

func TestChainEdgeIsRecordedOrRefusedNotHalfWritten(t *testing.T) {
	s := openAgentTaskStore(t)
	sc := seedLoop(t, s, "chained", "/repo")
	step := seedStep(t, s, sc)

	// The legal edge writes, and the counts answer the pairing question the
	// invoker's ordering proof asks: edge WITHOUT a task is a recorded
	// attempt; task without an edge is the untraceable cost AS6 forbids.
	if err := s.CreateAgentChainEdge(NewChainEdge{CallerLoopID: "chained", CallerStepID: step.ID, CalleeAgent: "reddit-agent", SkillID: "summarise-sentiment", Depth: 1}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.CountAgentChainEdges("chained", step.ID); err != nil || n != 1 {
		t.Fatalf("the edge must be countable where the invoker will look: %d %v", n, err)
	}
	if n, err := s.CountAgentTasksForCaller("chained", step.ID); err != nil || n != 0 {
		t.Fatalf("an edge alone is the crash-record shape: %d %v", n, err)
	}

	// Every empty identity is refused with words, before the CHECK gets the
	// chance to answer with a constraint code nobody reads.
	for _, bad := range []NewChainEdge{
		{CallerLoopID: "", CallerStepID: step.ID, CalleeAgent: "a", SkillID: "b", Depth: 1},
		{CallerLoopID: "chained", CallerStepID: step.ID, CalleeAgent: "", SkillID: "b", Depth: 1},
		{CallerLoopID: "chained", CallerStepID: step.ID, CalleeAgent: "a", SkillID: "", Depth: 1},
		{CallerLoopID: "chained", CallerStepID: step.ID, CalleeAgent: "a", SkillID: "b", Depth: 0},
	} {
		if err := s.CreateAgentChainEdge(bad); err == nil {
			t.Errorf("an edge %#v must be refused before it becomes an audit gap", bad)
		}
	}
}

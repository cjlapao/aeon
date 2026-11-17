package agentsvc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
	"regexp"
	"strconv"
)

// T3 — task.go: a Task is a row; async is a suspension, not a concurrency
// model (AS4). The step fires the Task and EXITS (P2, S6's machinery
// verbatim). The test that matters is the ABSENCE of a goroutine — a blocked
// step cannot survive a restart, and the failure surfaces as a loop that
// never resumes with no record of why. And nothing spawns (AS5).

func taskTestStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}

func oneDeny() []policy.Policy {
	return []policy.Policy{{
		ID: "p-1", Type: policy.SkillAccess, Target: policy.Target{Kind: policy.AgentKind, ID: "golang-engineer"},
		Action: policy.Deny, Spec: json.RawMessage(`{"skill":"reddit-agent/summarise-sentiment"}`), Source: policy.Operator,
	}}
}

func startTaskLoop(t *testing.T, s *store.Store, slug string) (loop.LoopState, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "tasklane.yaml")
	raw := "id: tasklane\nschemaVersion: 1\nversion: 1\nlanes:\n" +
		// A PLAIN lane: the pin preflight (T0) refuses skill lanes until T4 wires
		// SkillKnown from this package's registry — the task layer does not depend
		// on the lane carrying the annotation; invocation (T4) reads it from there.
		"  - id: work\n    step: {kind: model-step, agent: golang-engineer, produces: OUT.md}\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := loop.Start(context.Background(), s, store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "delegate"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return st, root
}

func runningStep(t *testing.T, s *store.Store, st loop.LoopState) int64 {
	t.Helper()
	step, err := s.CreateStep(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.Root},
		store.NewStep{Phase: "model", Agent: "golang-engineer", Attempt: 1, Status: store.StepRunning, Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return step.ID
}

func createArgs(stepID int64, st loop.LoopState, mode TaskMode) CreateTask {
	return CreateTask{
		CallerLoopID: st.ID, CallerLane: "work", CallerStepID: stepID,
		CalleeAgent: "reddit-agent", SkillID: "summarise-sentiment",
		Mode: mode, Params: map[string]any{"query": "aeon"},
		Frozen: oneDeny(),
	}
}

func TestAsyncTaskSuspendsTheStep(t *testing.T) {
	s, _ := taskTestStore(t)
	st, _ := startTaskLoop(t, s, "async-suspend")
	stepID := runningStep(t, s, st)
	ts := NewTaskStore(s)
	task, reject := ts.Create(context.Background(), createArgs(stepID, st, TaskAsync))
	if reject != nil {
		t.Fatalf("create: %s", reject.Detail)
	}
	if task.Mode != TaskAsync || task.Status != TaskWaiting {
		t.Fatalf("task row wrong: %#v", task)
	}
	// THE assertion: the step row is suspended — the step exited, it did not
	// wait. And a question row makes the wait visible to a person (Q4).
	row, err := s.ReadStep(stepID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != string(store.StepSuspended) {
		t.Fatalf("async task left the step as %s — waiting steps do not survive restarts", row.Status)
	}
	q, ok, err := s.PendingStepQuestion(stepID)
	if err != nil || !ok {
		t.Fatalf("the wait must be visible as a question: %v %v", ok, err)
	}
	if !strings.Contains(q.Body, "reddit-agent/summarise-sentiment") || !strings.Contains(q.Body, "task") {
		t.Fatalf("the question must name the task and its target: %q", q.Body)
	}
	// The task row is sufficient to find everything: loop, lane, step, question.
	got, err := ts.Get(task.ID)
	if err != nil || got.CallerLoopID != st.ID || got.CallerStepID != stepID || got.QuestionID != q.ID {
		t.Fatalf("task row insufficient for recovery: %#v %v", got, err)
	}
}

func TestNoGoroutineForAnAsyncTask(t *testing.T) {
	s, _ := taskTestStore(t)
	st, _ := startTaskLoop(t, s, "no-goroutine")
	stepID := runningStep(t, s, st)
	ts := NewTaskStore(s)
	if _, reject := ts.Create(context.Background(), createArgs(stepID, st, TaskAsync)); reject != nil {
		t.Fatal(reject.Detail)
	}

	// The claim under test (AS4/P2): an async Task leaves NOTHING parked.
	// The honest shape of that check is per-goroutine over two dumps 150 ms
	// apart, and the threat model decides what counts: a task-owned goroutine
	// is one PARKED (chan/select/sync/semaphore/syscall — a waiter has to be
	// waiting) whose own frames include this package's PRODUCT code. The
	// test's own execution lives here too and is excluded by file; transient
	// infrastructure caught mid-flight in a single dump (finalizers, timers,
	// anything that had already finished in the quiet run) is not a waiter,
	// and two samples is what separates the two honestly — a parked waiter
	// is in both.
	first := goroutineDumps(t)
	time.Sleep(150 * time.Millisecond)
	second := goroutineDumps(t)
	for _, blocks := range [2][]string{first, second} {
		for _, block := range blocks {
			if !isParked(block) || !hasProductFrame(block) {
				continue
			}
			t.Fatalf("an async Task owns a parked goroutine in product code — suspension is EXITING (P2):\n%s", block)
		}
	}

	// And nothing SPAWNS: read /proc directly — the alternative of shelling
	// out to `ps` counted the `ps` itself (measured), which is the exact
	// class of casual test this slice refuses to leave behind (AS5 — no
	// worker, no helper process).
	kids, err := childPIDs(os.Getpid())
	if err != nil {
		t.Fatalf("count children: %v", err)
	}
	if len(kids) != 0 {
		t.Fatalf("the delegation spawned %d child process(es): %v", len(kids), kids)
	}
}

// goroutineDumps splits the all-goroutine stack dump into blocks.
func goroutineDumps(t *testing.T) []string {
	t.Helper()
	dump := make([]byte, 1<<20)
	n := runtime.Stack(dump, true)
	return strings.Split(string(dump[:n]), "\n\ngoroutine ")
}

var parkedStates = []string{"[chan ", "[select", "[sync.", "[semacquire", "[syscall", "[chan receive", "[chan send", "[IO wait"}

func isParked(block string) bool {
	header := block
	if i := strings.Index(header, "\n"); i >= 0 {
		header = header[:i]
	}
	for _, st := range parkedStates {
		if strings.Contains(header, st) {
			return true
		}
	}
	return false
}

// hasProductFrame answers the threat-model question: does this goroutine's
// own stack carry this package's NON-TEST code? A goroutine started from
// test code is the test's business; a task-owned waiter would be started and
// parked inside task.go — and product-file frames are exactly that shape.
var productFrame = regexp.MustCompile(`internal/agentsvc/[a-zA-Z0-9_]+\.go:`)

func hasProductFrame(block string) bool {
	for _, m := range productFrame.FindAllString(block, -1) {
		if !strings.HasSuffix(strings.TrimSuffix(m, ":"), "_test.go") {
			return true
		}
	}
	return false
}

// childPIDs lists direct children by reading every /proc/<pid>/stat and
// comparing field 4 (ppid) — no subprocess involved, so the counter cannot
// count itself.
func childPIDs(pid int) ([]string, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var kids []string
	for _, e := range entries {
		if !isPID(e.Name()) {
			continue
		}
		raw, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue // the process is already gone; it cannot be a child we spawned
		}
		// field 2 is comm in parens and may itself contain spaces — split
		// after the LAST ')': the fields after it are stable.
		stat := string(raw)
		i := strings.LastIndex(stat, ")")
		if i < 0 {
			continue
		}
		fields := strings.Fields(stat[i+1:])
		if len(fields) >= 2 && fields[1] == strconv.Itoa(pid) { // field 4 = ppid
			kids = append(kids, e.Name())
		}
	}
	return kids, nil
}

func children(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/task") // same-process threads; children below
	if err != nil {
		return -1
	}
	threads := len(entries)
	entries, err = os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range entries {
		if isPID(e.Name()) {
			count++
		}
	}
	return threads*100000 + count // both dimensions in one comparable number
}

func isPID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func firstFrameBlock(dump string) string {
	i := strings.Index(dump, "internal/agentsvc")
	if i < 0 || i > 4000 {
		return dump[:min(len(dump), 2000)]
	}
	start := strings.LastIndex(dump[:i], "goroutine ")
	return dump[start:min(len(dump), i+400)]
}

func TestCompletionResumesCold(t *testing.T) {
	s, dir := taskTestStore(t)
	st, root := startTaskLoop(t, s, "cold")
	stepID := runningStep(t, s, st)
	ts := NewTaskStore(s)

	// Daemon one: the step fires the Task and exits — S6's machinery exactly.
	dep1 := loop.Deps{Store: s, Model: func(_ context.Context, state loop.LoopState, _ blueprint.Step, _ string, _ string)(loop.ModelResult, error) {
		task, reject := ts.Create(context.Background(), createArgs(stepID, state, TaskAsync))
		if reject != nil {
			return loop.ModelResult{}, reject
		}
		return loop.ModelResult{StepID: stepID, Kind: "suspended", QuestionID: task.QuestionID}, nil
	}}
	if _, err := loop.AdvanceLoop(context.Background(), dep1, st.ID); err != nil {
		t.Fatal(err)
	}
	waiting, err := loop.Pinned(context.Background(), s, st.ID)
	if err != nil || waiting.Status != "waiting" {
		t.Fatalf("the loop must park as waiting on the task: %#v %v", waiting, err)
	}

	// DAEMON RESTART between creation and completion: the first handle is
	// closed and a fresh process-equivalent opens the same file.
	_ = s.Close()
	s2, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	ts2 := NewTaskStore(s2)
	// AS12's boundary, told honestly: this callee is a reviewed first-party
	// body, so the answer the model is handed stays verbatim (the labelled
	// half of the boundary is untrusted_test.go's).
	ts2.SetFirstParty(func(string) bool { return true })
	// S7 must resume a Task it did not created — the row is sufficient.
	taskRow, err := ts2.Get(1)
	if err != nil || taskRow.CallerStepID != stepID {
		t.Fatalf("the restarted daemon cannot find the task it did not create: %v", err)
	}
	// The callee's work product lands where the step declared it.
	if err := os.WriteFile(filepath.Join(root, "OUT.md"), []byte("sentiment: positive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if reject := ts2.Complete(context.Background(), 1, "sentiment: positive", "ok"); reject != nil {
		t.Fatalf("complete: %s", reject.Detail)
	}

	// Cold resume: the loop runs again and the ladder MOVES — the step is done
	// with an outcome, the loop left waiting.
	ranAgain := false
	dep2 := loop.Deps{Store: s2, Model: func(context.Context, loop.LoopState, blueprint.Step, string, string)(loop.ModelResult, error) {
		ranAgain = true
		return loop.ModelResult{Kind: "completed", Outcome: "ok"}, nil
	}}
	if _, err := loop.AdvanceLoop(context.Background(), dep2, st.ID); err != nil {
		t.Fatal(err)
	}
	if ranAgain {
		t.Fatal("completion re-ran the model — the ladder itself must move (CompleteSuspendedStep), not a second dispatch")
	}
	done, err := loop.Pinned(context.Background(), s2, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status == "waiting" {
		t.Fatalf("a completed task left the loop parked: %#v", done)
	}
	row, err := s2.ReadStep(stepID)
	if err != nil || row.Status != string(store.StepDone) || row.Outcome != "ok" {
		t.Fatalf("the step row must record the completed delegation: %#v %v", row, err)
	}
	// The answered question is visible in the thread: the wait has a record.
	q, err := s2.ReadQuestion(taskRow.QuestionID)
	if err != nil || q.Answer != "sentiment: positive" {
		t.Fatalf("the question must carry the answer: %#v %v", q, err)
	}
	if reject := ts2.Complete(context.Background(), 1, "again", "ok"); reject == nil {
		t.Fatal("completing twice must be refused (DB19 — first answer wins)")
	}
}

func TestSyncNeverBecomesAsync(t *testing.T) {
	s, _ := taskTestStore(t)
	st, _ := startTaskLoop(t, s, "sync")
	stepID := runningStep(t, s, st)
	ts := NewTaskStore(s)
	task, reject := ts.Create(context.Background(), createArgs(stepID, st, TaskSync))
	if reject != nil {
		t.Fatal(reject.Detail)
	}
	if task.Status != TaskRunning {
		t.Fatalf("sync runs within the step: %#v", task)
	}
	// The step is NOT suspended and there is NO question: sync waits inside the
	// step's own life; it never grew the async machinery.
	row, err := s.ReadStep(stepID)
	if err != nil || row.Status != string(store.StepRunning) {
		t.Fatalf("a sync task touched the step row: %#v %v", row, err)
	}
	if _, ok, _ := s.PendingStepQuestion(stepID); ok {
		t.Fatal("a sync task created a question — it has no wait to be visible in")
	}
	// Time passes. A sync Task that takes too long does NOT become async:
	// nothing rewrites mode, and no timeout path exists to call — the store
	// package's own ledger test walks the row.
	got, err := ts.Get(task.ID)
	if err != nil || got.Mode != TaskSync || got.Status != TaskRunning {
		t.Fatalf("a sync task drifted toward async: %#v %v", got, err)
	}
	if reject := ts.Complete(context.Background(), task.ID, "done", "ok"); reject == nil || !strings.Contains(reject.Detail, "sync") {
		t.Fatalf("sync tasks complete within the step — Complete must refuse saying so: %v", reject)
	}
	if reject := ts.MarkDone(context.Background(), task.ID); reject != nil {
		t.Fatal(reject.Detail)
	}
	if reject := ts.MarkDone(context.Background(), task.ID); reject == nil {
		t.Fatal("a completed task must refuse a second completion")
	}
}

func TestTaskCarriesCreationTimePolicySet(t *testing.T) {
	s, _ := taskTestStore(t)
	st, _ := startTaskLoop(t, s, "as13")
	stepID := runningStep(t, s, st)
	ts := NewTaskStore(s)
	task, reject := ts.Create(context.Background(), createArgs(stepID, st, TaskAsync))
	if reject != nil {
		t.Fatal(reject.Detail)
	}
	// A different world today — the task must answer with the set it was
	// CREATED under (S7's replay discipline applied to delegation).
	frozenA, hashA, err := ts.FrozenSet(task.ID)
	if err != nil || len(frozenA) != 1 || frozenA[0].Action != policy.Deny || frozenA[0].ID != "p-1" {
		t.Fatalf("the row must carry the creation-time set verbatim: %v %v", frozenA, err)
	}
	if hashA == "" {
		t.Fatal("no hash recorded")
	}
	// The resumed evaluation reads the ORIGINAL set: a helper recomputes the
	// hash over the parsed entries and the row's hash must match — a tampered
	// row must fail the check (S7's poisoned-manifest discipline).
	if ok, err := ts.FrozenSetIntact(task.ID); err != nil || !ok {
		t.Fatalf("hash over parsed entries must match the row: %v %v", ok, err)
	}
	// A tampered set hashes differently — the poisoned-manifest discipline of
	// S7, at the function level (the row-level tamper rides the store
	// package's own ledger test, which owns the raw handle).
	hA, err := HashPolicies(oneDeny())
	if err != nil {
		t.Fatal(err)
	}
	wider := []policy.Policy{{ID: "p-2", Type: policy.SkillAccess, Target: policy.Target{Kind: policy.AgentKind, ID: "golang-engineer"},
		Action: policy.Allow, Spec: json.RawMessage(`{}`), Source: policy.Operator}}
	hB, werr := HashPolicies(wider)
	if werr != nil {
		t.Fatal(werr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if hA == hB || hA != hashA {
		t.Fatalf("the hash must identify the set: hA=%s hB=%s row=%s", hA, hB, hashA)
	}
}

func TestTaskCreateRefusals(t *testing.T) {
	s, _ := taskTestStore(t)
	st, _ := startTaskLoop(t, s, "refusals")
	stepID := runningStep(t, s, st)
	ts := NewTaskStore(s)
	base := createArgs(stepID, st, TaskAsync)
	for _, test := range []struct {
		name string
		mut  func(*CreateTask)
		want string
	}{
		{"mode", func(c *CreateTask) { c.Mode = "maybe" }, "maybe"},
		{"callee", func(c *CreateTask) { c.CalleeAgent = "" }, "callee"},
		{"skill", func(c *CreateTask) { c.SkillID = "" }, "skill"},
		{"frozen-nil", func(c *CreateTask) { c.Frozen = nil }, "policy set"},
		{"bad-params", func(c *CreateTask) { c.Params = map[string]any{"fn": func() {}} }, "params"},
	} {
		arg := base
		test.mut(&arg)
		_, reject := ts.Create(context.Background(), arg)
		if reject == nil || !strings.Contains(reject.Detail, test.want) {
			t.Fatalf("%s: want refusal naming %q, got %v", test.name, test.want, reject)
		}
	}
	// A step that is not running cannot be suspended.
	doneStep := runningStep(t, s, st)
	if err := s.SetStepStatus(doneStep, store.StepDone, "ok"); err != nil {
		t.Fatal(err)
	}
	arg := createArgs(doneStep, st, TaskAsync)
	if _, reject := ts.Create(context.Background(), arg); reject == nil || !strings.Contains(reject.Detail, "step") {
		t.Fatalf("suspending a finished step must be refused: %v", reject)
	}
}

func TestTaskMigrationLedger(t *testing.T) {
	// Fresh-equals-migrated rides the store's own ledger walk; here the number
	// and the vocabulary are pinned from the consumer side.
	// The consumer-side proof: the row methods the task layer needs exist on a
	// fresh store, which only works if 0022 arrived. The table/index shape and
	// the raw tamper case live in the store package's own ledger test.
	s, _ := taskTestStore(t)
	ts := NewTaskStore(s)
	if _, err := ts.Get(999); err == nil {
		t.Fatal("Get on a missing task must fail — the table exists only if 0022 did")
	}
	if n := len(store.AgentTaskStatuses); n < 3 {
		t.Fatalf("the status vocabulary is missing: %v", store.AgentTaskStatuses)
	}
}

// The absence test is only as good as its detector: this feeds the detector
// a synthetic dump of exactly the shape AS4 forbids — a goroutine parked in
// task.go behind a channel — and requires it to FIRE. An assertion that can
// never fail is not evidence.
func TestTheLeakDetectorDetects(t *testing.T) {
	leaked := "goroutine 42 [chan receive]:\ngithub.com/cjlapao/aeon/internal/agentsvc.(*TaskStore).wait(\n\t/repo/aeon/internal/agentsvc/task.go:300 +0x40\ncreated by github.com/cjlapao/aeon/internal/agentsvc.(*TaskStore).Create\n\t/repo/aeon/internal/agentsvc/task.go:120 +0x88"
	if !isParked(leaked) || !hasProductFrame(leaked) {
		t.Fatal("the detector missed the exact goroutine AS4 forbids")
	}
	innocent := "goroutine 7 [runnable]:\ngithub.com/cjlapao/aeon/internal/agentsvc.TestAsyncTaskSuspendsTheStep(\n\t/repo/aeon/internal/agentsvc/task_test.go:90 +0x40\ntesting.tRunner(\n\t/usr/local/go/src/testing/testing.go:1690 +0xe4"
	if isParked(innocent) || hasProductFrame(innocent) {
		t.Fatal("the detector flags the test's own execution — the false positive that trains people to ignore flakes")
	}
	waitingInfra := "goroutine 9 [syscall]:\nosyscall.Syscall(\n\t/usr/local/go/src/runtime/asm_amd64.s:1071 +0x48"
	if !isParked(waitingInfra) || hasProductFrame(waitingInfra) {
		t.Fatal("runtime infrastructure must be parked-but-innocent: filtered by frame, not by state")
	}
}

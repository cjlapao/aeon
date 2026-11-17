package agentsvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// task.go is T3: a Task is a row, and an async Task is a SUSPENSION, not a
// concurrency model (AS4). The step fires the Task and exits — S6's
// machinery verbatim: the step row goes `suspended`, a question row makes the
// wait visible to a person (D16's pair), and a completion resumes the loop
// cold through loop.CompleteSuspendedStep, the same primitive `step.continue`
// uses. Nothing here starts a goroutine and nothing spawns (AS5): the
// package-level test dumps the goroutine stacks and demands no frame names
// this package. A blocked step cannot survive a restart — that is not a style
// preference, it is why the row is the only state.
//
// Q4, answered here because this is where it becomes a row (and restated in
// the task notes): YES — a skill whose agent needs an `ask` is a Task that
// suspends, and the CALLER's step suspends with it. Suspension is already a
// row, so the two compose: the loop parks `waiting` (S6's shape), and the
// person sees the question naming the task and its target. The alternative —
// the caller spinning while the callee waits on a human — is a caller blocked
// on a person it cannot see, which is the failure AS4 exists to prevent with
// a different excuse.

// TaskMode is chosen at invocation and immutable thereafter (T3 scope 3: no
// promotion — a step whose transcript changes shape on a timeout has a gap
// nothing explains).
type TaskMode string

const (
	TaskSync  TaskMode = "sync"
	TaskAsync TaskMode = "async"
)

// TaskStatus is the migration 0022 vocabulary.
type TaskStatus string

const (
	TaskRunning   TaskStatus = "running"
	TaskWaiting   TaskStatus = "waiting"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	// TaskRefused is S15 T5's terminal status (migration 0024): the callee's
	// work COMPLETED — it happened, it was paid for — and its result was
	// refused AT ARRIVAL because the agent is revoked (AP7). It is not
	// "failed": the stranger answered correctly; we declined the answer.
	// completed_at is set like any terminal move; the cost stands recorded.
	TaskRefused TaskStatus = "refused"
)

// Task is the domain view of the row. The row is sufficient for recovery
// (scope 6): loop, lane, step and question are all carried, so S7 resumes a
// Task this process did not create.
type Task struct {
	ID           int64
	CallerLoopID string
	CallerLane   string
	CallerStepID int64
	QuestionID   int64
	CalleeAgent  string
	SkillID      string
	Mode         TaskMode
	Status       TaskStatus
	// Cost is what the callee reported, in S3's three states — never zero
	// by absence (an unreported cost is unknown, and says so).
	Cost         model.Cost
	Text         string
	Params       map[string]any
	ResultJSON   string
	PoliciesHash string
	CreatedAt    string
	CompletedAt  string
}

// Outcome reads the completion verdict out of the result envelope; a running
// or waiting task has none.
func (t Task) Outcome() string {
	var env resultEnvelope
	if t.ResultJSON == "" || json.Unmarshal([]byte(t.ResultJSON), &env) != nil {
		return ""
	}
	return env.Outcome
}

// ResultText is what the thread shows as the answer.
func (t Task) ResultText() string {
	var env resultEnvelope
	if t.ResultJSON == "" || json.Unmarshal([]byte(t.ResultJSON), &env) != nil {
		return t.ResultJSON
	}
	return env.Result
}

// requestEnvelope is what params_json stores: the request's words and its
// parameters, one canonical write so a resumed callee reads exactly what was
// asked (T4 — the invocation's text must survive the crash it was made for).
type requestEnvelope struct {
	Text   string         `json:"text"`
	Params map[string]any `json:"params,omitempty"`
}

type resultEnvelope struct {
	Outcome string `json:"outcome"`
	Result  string `json:"result"`
	// Cost is what the callee REPORTED it cost, in S3's three states, stored
	// with the terminal record — the same write, never a second one that
	// could disagree with the first (DB19's shape applied to numbers).
	Cost *model.Cost `json:"cost,omitempty"`
}

// CreateTask is what an invocation supplies (T4 is the production caller).
// Frozen is REQUIRED, not optional (AS13): a Task created under no policy set
// completes under whoever is in charge when it wakes, and the record cannot
// say which governed it.
type CreateTask struct {
	CallerLoopID string
	CallerLane   string
	CallerStepID int64
	CalleeAgent  string
	SkillID      string
	// Text is the request in the caller's own words — "find what people are
	// saying about X and summarise it". It rides the task row because the
	// callee answers from rows, not memory (P2's discipline applied to
	// delegation: a restarted daemon resumes the ask with the ask intact).
	Text   string
	Mode   TaskMode
	Params map[string]any
	Frozen []policy.Policy
}

// TaskStore orchestrates the domain over store rows; every statement stays in
// internal/store (I2), including the transaction that makes an async creation
// atomic — step flip, question + thread, task row — because the half-states
// are exactly what a cold restart could not reason about.
type TaskStore struct {
	s *store.Store
	// firstParty is AS12's injected compile-time boundary (untrusted.go):
	// nil means NOBODY is first-party, so an unwired daemon labels every
	// skill result — the safe reading, argued in that file.
	firstParty func(agent string) bool

	// invocationGate is S15 T5's injected seam: nil (every daemon that has
	// not wired it, and every S14 test) changes NOTHING. When wired from
	// the entry point it asks, at CREATION, whether the callee may be
	// invoked at all — a revoked or disabled plugin agent is refused before
	// a row exists, so "revoke stops new invocations" costs no goroutine
	// and races nothing. agentsvc never imports the plugin package: the
	// entry point injects the answer (AS11's direction, the SetFirstParty
	// precedent).
	invocationGate func(calleeAgent string) (refusal string, refused bool)

	// arrivalGate is the other half of AP7 and the HARDER half: consulted
	// not at creation (the agent was not revoked then) but at the point a
	// COMPLETED result arrives — a Task already in flight will finish and
	// its words would reach a prompt unless something stops them at the
	// door. The gate records the refusal (agent, revocation instant,
	// reason, cost RECORDED-not-recovered) and returns the sentence that
	// replaces the stranger's words.
	arrivalGate func(calleeAgent string, taskID int64, cost *model.Cost) (refusal string, refused bool)
}

// SetInvocationGate injects the creation-time answer (S15 T5; wired once by
// the entry point, exactly like SetFirstParty — nil restores inertness).
func (ts *TaskStore) SetInvocationGate(fn func(calleeAgent string) (string, bool)) {
	ts.invocationGate = fn
}

// SetArrivalGate injects the arrival-time answer (S15 T5, AP7).
func (ts *TaskStore) SetArrivalGate(fn func(calleeAgent string, taskID int64, cost *model.Cost) (string, bool)) {
	ts.arrivalGate = fn
}

func NewTaskStore(s *store.Store) *TaskStore { return &TaskStore{s: s} }

// HashPolicies is the canonical digest of a policy set — the same discipline
// S7 applies to a step's frozen manifest (recompute and compare, a mismatch
// poisons rather than trusts).
func HashPolicies(set []policy.Policy) (string, error) {
	raw, err := json.Marshal(set)
	if err != nil {
		return "", fmt.Errorf("the policy set cannot be canonicalised: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Create refuses every shape that could not be honoured later, and only then
// writes. Refusals are values (DB19), each naming the thing refused.
func (ts *TaskStore) Create(_ context.Context, in CreateTask) (Task, *Reject) {
	if in.Mode != TaskSync && in.Mode != TaskAsync {
		return Task{}, &Reject{Field: "mode", Detail: fmt.Sprintf("task refused: mode %q is neither sync nor async", string(in.Mode))}
	}
	if strings.TrimSpace(in.CalleeAgent) == "" {
		return Task{}, &Reject{Field: "callee", Detail: `task refused: field "callee" is empty — a delegation must name the agent it asks`}
	}
	if strings.TrimSpace(in.SkillID) == "" {
		return Task{}, &Reject{Field: "skill", Detail: "task refused: a delegation must name the skill it asks for"}
	}
	if in.Frozen == nil {
		return Task{}, &Reject{Field: "frozen", Detail: "task refused: a Task must be created under an explicit policy set (AS13) — nil is not a set, and a Task that woke up under whoever happens to be in charge has no record of what governed it"}
	}
	if ts.invocationGate != nil {
		if refusal, refused := ts.invocationGate(in.CalleeAgent); refused {
			return Task{}, &Reject{Field: "state", Detail: fmt.Sprintf("task refused: %s", refusal)}
		}
	}
	raw, err := json.Marshal(requestEnvelope{Text: in.Text, Params: in.Params})
	if err != nil {
		return Task{}, &Reject{Field: "params", Detail: fmt.Sprintf("task refused: params are not encodable (%v) — a parameter that cannot be written down cannot be honoured on resume", err)}
	}
	paramsJSON := string(raw)
	hash, err := HashPolicies(in.Frozen)
	if err != nil {
		return Task{}, &Reject{Field: "frozen", Detail: fmt.Sprintf("task refused: %v", err)}
	}
	row, err := ts.s.ReadStep(in.CallerStepID)
	if err != nil {
		return Task{}, &Reject{Field: "step", Detail: fmt.Sprintf("task refused: the caller step cannot be read (%v)", err)}
	}
	if row.Status != string(store.StepRunning) {
		return Task{}, &Reject{Field: "step", Detail: fmt.Sprintf("task refused: step %d is %s — only a running step can delegate", in.CallerStepID, row.Status)}
	}
	pin, err := loop.Pinned(context.Background(), ts.s, in.CallerLoopID)
	if err != nil {
		return Task{}, &Reject{Field: "loop", Detail: fmt.Sprintf("task refused: the caller loop is not pinned (%v)", err)}
	}
	in1 := store.NewAgentTask{
		CallerLoopID: in.CallerLoopID, CallerLane: in.CallerLane, CallerStepID: in.CallerStepID,
		CalleeAgent: in.CalleeAgent, SkillID: in.SkillID, Mode: string(in.Mode),
		ParamsJSON: paramsJSON, PoliciesJSON: mustJSON(in.Frozen), PoliciesHash: hash,
	}
	if in.Mode == TaskAsync {
		in1.Status = string(TaskWaiting)
		body := fmt.Sprintf("awaiting task · %s/%s · step %d — the step exited; a completion resumes it cold",
			in.CalleeAgent, in.SkillID, in.CallerStepID)
		t, err := ts.s.SuspendStepForTask(store.Scope{LoopSlug: pin.Slug, ComponentRoot: pin.ComponentRoot}, in1, body)
		if err != nil {
			return Task{}, &Reject{Field: "store", Detail: fmt.Sprintf("task refused: %v", err)}
		}
		return toTask(t), nil
	}
	in1.Status = string(TaskRunning)
	t, err := ts.s.CreateAgentTask(in1)
	if err != nil {
		return Task{}, &Reject{Field: "store", Detail: fmt.Sprintf("task refused: %v", err)}
	}
	return toTask(t), nil
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "null" // HashPolicies already refused unencodable sets before any write
	}
	return string(raw)
}

func toTask(t store.AgentTask) Task {
	out := Task{
		ID: t.ID, CallerLoopID: t.CallerLoopID, CallerLane: t.CallerLane, CallerStepID: t.CallerStepID,
		CalleeAgent: t.CalleeAgent, SkillID: t.SkillID,
		Mode: TaskMode(t.Mode), Status: TaskStatus(t.Status),
		ResultJSON: t.ResultJSON, PoliciesHash: t.PoliciesHash,
		Cost:      model.Cost{State: model.CostUnknown, Reason: "cost-not-reported"},
		CreatedAt: t.CreatedAt, CompletedAt: t.CompletedAt,
	}
	if t.QuestionID != nil {
		out.QuestionID = *t.QuestionID
	}
	var req requestEnvelope
	if json.Unmarshal([]byte(t.ParamsJSON), &req) == nil {
		out.Text, out.Params = req.Text, req.Params
	}
	var res resultEnvelope
	if json.Unmarshal([]byte(t.ResultJSON), &res) == nil && res.Cost != nil {
		out.Cost = *res.Cost
	}
	return out
}

// validateCost admits only honest cost records (M10): one of the three
// states, an unknown that says WHY (the reason set is closed), and no
// negative numbers. A cost cannot be laundered into certainty here.
func validateCost(c *model.Cost) *Reject {
	if c == nil {
		return nil
	}
	switch c.State {
	case model.CostKnown, model.CostPartial:
		if c.USD < 0 || c.USDAtLeast < 0 {
			return &Reject{Field: "cost", Detail: "task refused: a cost below zero is not a measurement"}
		}
	case model.CostUnknown:
		if c.Reason == "" {
			return &Reject{Field: "cost", Detail: "task refused: an unknown cost must carry a reason — the reason set is closed, and an unexplained absence is what M10 exists to stop"}
		}
	default:
		return &Reject{Field: "cost", Detail: fmt.Sprintf("task refused: cost state %q is not one of known, partial or unknown", string(c.State))}
	}
	return nil
}

// Get reads one task by id; the row alone must suffice for recovery.
func (ts *TaskStore) Get(id int64) (Task, error) {
	t, err := ts.s.GetAgentTask(id)
	if err != nil {
		return Task{}, err
	}
	return toTask(t), nil
}

// Complete is the async finish: the status flip FIRST (the idempotency gate —
// the first record wins, DB19), then the cold resume, which is a separate
// idempotent step so a crash between the two leaves a task a restarted daemon
// can finish rather than one it can only refuse.
func (ts *TaskStore) Complete(ctx context.Context, id int64, result, outcome string) *Reject {
	return ts.CompleteWithCost(ctx, id, result, outcome, nil)
}

// CompleteWithCost is Complete with the callee's REPORTED cost recorded in
// the same terminal write (T6's rollup reads it; an unreported cost stays
// honestly unknown, never zero).
func (ts *TaskStore) CompleteWithCost(ctx context.Context, id int64, result, outcome string, cost *model.Cost) *Reject {
	if rj := validateCost(cost); rj != nil {
		return rj
	}
	if outcome == "" {
		outcome = "ok"
	}
	if outcome != "ok" && outcome != "reject" {
		return &Reject{Field: "outcome", Detail: fmt.Sprintf("task %d: outcome %q is neither ok nor reject", id, outcome)}
	}
	task, err := ts.Get(id)
	if err != nil {
		return &Reject{Field: "task", Detail: fmt.Sprintf("task %d cannot be read: %v", id, err)}
	}
	if task.Mode == TaskSync {
		return &Reject{Field: "mode", Detail: fmt.Sprintf("task %d is sync — it completes within its step (MarkDone); a sync Task never becomes async, there is no promotion path to call", id)}
	}
	if task.Status != TaskWaiting {
		return &Reject{Field: "status", Detail: fmt.Sprintf("task %d is %s (completed at %s) — completion is once, the first record wins", id, task.Status, task.CompletedAt)}
	}
	// AP7's arrival point — THE point, not creation: at creation the agent
	// was not revoked, and "revocation that revokes eventually" is not a
	// guarantee at all. The gate sees the result only to DECIDE; its words
	// are what the refusal (not the stranger) will say.
	if ts.arrivalGate != nil {
		if refusal, refused := ts.arrivalGate(task.CalleeAgent, id, cost); refused {
			renv, _ := json.Marshal(resultEnvelope{Outcome: "refused", Result: refusal, Cost: cost})
			moved, err := ts.s.MarkAgentTaskTerminal(id, string(TaskWaiting), string(TaskRefused), string(renv))
			if err != nil {
				return &Reject{Field: "store", Detail: fmt.Sprintf("task %d: refusing the arrival failed: %v", id, err)}
			}
			if !moved {
				return &Reject{Field: "status", Detail: fmt.Sprintf("task %d is no longer waiting — another hand completed it first (the first record wins)", id)}
			}
			fresh, err := ts.Get(id)
			if err != nil {
				return &Reject{Field: "store", Detail: fmt.Sprintf("task %d vanished after refusal: %v", id, err)}
			}
			// The wait must still END — the caller's step resumes carrying
			// the refusal, never the stranger's words (ModelFacingResult
			// is what makes that structural, not a promise).
			return ts.Resume(ctx, fresh)
		}
	}
	env, _ := json.Marshal(resultEnvelope{Outcome: outcome, Result: result, Cost: cost})
	moved, err := ts.s.MarkAgentTaskTerminal(id, string(TaskWaiting), string(TaskCompleted), string(env))
	if err != nil {
		return &Reject{Field: "store", Detail: fmt.Sprintf("task %d: %v", id, err)}
	}
	if !moved {
		return &Reject{Field: "status", Detail: fmt.Sprintf("task %d is no longer waiting — another hand completed it", id)}
	}
	fresh, err := ts.Get(id)
	if err != nil {
		return &Reject{Field: "store", Detail: fmt.Sprintf("task %d vanished after completion: %v", id, err)}
	}
	return ts.Resume(ctx, fresh)
}

// Resume re-drives the completion of a COMPLETED task: answer the question if
// it is unanswered, move the suspended step row, and move the ladder. Every
// step is guarded on its current state, so this is safe to call again after a
// crash anywhere in the completion — and it is how S7 finishes a Task it did
// not create.
func (ts *TaskStore) Resume(ctx context.Context, task Task) *Reject {
	if task.Status != TaskCompleted && task.Status != TaskRefused {
		return &Reject{Field: "status", Detail: fmt.Sprintf("task %d is %s — only a completed (or refused) task can be resumed", task.ID, task.Status)}
	}
	// AS12: the ANSWER is where the result reaches a model, so the label
	// belongs here — never in the stored row (scope 3: the record stays
	// byte-for-byte what the callee said; the transcript can still read it).
	outcome := task.Outcome()
	answer := ts.ModelFacingResult(task)
	if outcome == "" {
		outcome = "ok"
	}
	if task.QuestionID != 0 {
		q, err := ts.s.ReadQuestion(task.QuestionID)
		if err != nil {
			return &Reject{Field: "question", Detail: fmt.Sprintf("task %d: its question cannot be read (%v)", task.ID, err)}
		}
		if q.Answer == "" {
			if _, err := ts.s.Answer(task.QuestionID, answer); err != nil {
				return &Reject{Field: "store", Detail: fmt.Sprintf("task %d: answering the wait failed (%v)", task.ID, err)}
			}
			// A refused double answer (OK=false) still proceeds: the wait HAS
			// ended — someone answered first — and the ladder must move.
		}
	}
	step, err := ts.s.ReadStep(task.CallerStepID)
	if err != nil {
		return &Reject{Field: "step", Detail: fmt.Sprintf("task %d: its caller step cannot be read (%v)", task.ID, err)}
	}
	if step.Status == string(store.StepSuspended) {
		if err := ts.s.SetStepStatus(task.CallerStepID, store.StepDone, outcome); err != nil {
			return &Reject{Field: "store", Detail: fmt.Sprintf("task %d: settling the step failed (%v)", task.ID, err)}
		}
	}
	pin, err := loop.Pinned(ctx, ts.s, task.CallerLoopID)
	if err != nil {
		return &Reject{Field: "loop", Detail: fmt.Sprintf("task %d: the caller loop cannot be read (%v)", task.ID, err)}
	}
	if pin.Status == "waiting" && pin.LastStepID == task.CallerStepID {
		dep := loop.Deps{Store: ts.s}
		if err := loop.CompleteSuspendedStep(ctx, dep, task.CallerLoopID,
			loop.ModelResult{StepID: task.CallerStepID, Kind: "completed", Outcome: outcome}); err != nil {
			return &Reject{Field: "ladder", Detail: fmt.Sprintf("task %d: the ladder refused the completion (%v)", task.ID, err)}
		}
		emitTask(ts, task)
	}
	return nil
}

func emitTask(ts *TaskStore, task Task) {
	// T10 puts the event on the socket; T3 keeps the seam named and unused-
	// safe rather than inventing an emit path the transport does not have yet.
	_ = task
}

// MarkDone is the sync finish — within the step, no question, no ladder move
// (the step completes normally right after).
func (ts *TaskStore) MarkDone(_ context.Context, id int64) *Reject {
	task, err := ts.Get(id)
	if err != nil {
		return &Reject{Field: "task", Detail: fmt.Sprintf("task %d cannot be read: %v", id, err)}
	}
	if task.Mode != TaskSync {
		return &Reject{Field: "mode", Detail: fmt.Sprintf("task %d is async — it completes through Complete, which also resumes its caller", id)}
	}
	moved, err := ts.s.MarkAgentTaskTerminal(id, string(TaskRunning), string(TaskCompleted), "")
	if err != nil {
		return &Reject{Field: "store", Detail: fmt.Sprintf("task %d: %v", id, err)}
	}
	if !moved {
		return &Reject{Field: "status", Detail: fmt.Sprintf("task %d is %s (completed at %s) — completion is once", id, task.Status, task.CompletedAt)}
	}
	return nil
}

// FrozenSet returns the set the task was CREATED under (AS13 — the resumed
// evaluation reads this, never today's table) and the recorded hash.
// CompleteSync settles a SYNC task from its running state — the mode guard is
// the other half of "no promotion": Complete refuses sync tasks and this
// refuses async ones, so neither path can borrow the other's waiting
// machinery. The result is stored, not merely returned: the task row is the
// record, the return value is convenience.
func (ts *TaskStore) CompleteSync(_ context.Context, id int64, result string) (Task, *Reject) {
	return ts.CompleteSyncWithCost(context.Background(), id, result, nil)
}

// CompleteSyncWithCost settles a sync task with the callee's reported cost
// stored in the same write.
func (ts *TaskStore) CompleteSyncWithCost(_ context.Context, id int64, result string, cost *model.Cost) (Task, *Reject) {
	if rj := validateCost(cost); rj != nil {
		return Task{}, rj
	}
	task, err := ts.Get(id)
	if err != nil {
		return Task{}, &Reject{Field: "id", Detail: fmt.Sprintf("task refused: task %d: %v", id, err)}
	}
	if task.Mode != TaskSync {
		return Task{}, &Reject{Field: "mode", Detail: fmt.Sprintf("task refused: task %d is async — an async completion is Complete, which resumes the suspended step; a sync task never suspended anything to resume", id)}
	}
	if task.Status != TaskRunning {
		return Task{}, &Reject{Field: "status", Detail: fmt.Sprintf("task refused: task %d is %s — only a running sync task can be settled, and the first record wins", id, task.Status)}
	}
	env, err := json.Marshal(resultEnvelope{Outcome: "ok", Result: result, Cost: cost})
	if err != nil {
		return Task{}, &Reject{Field: "result", Detail: fmt.Sprintf("task refused: the result cannot be stored: %v", err)}
	}
	moved, err := ts.s.MarkAgentTaskTerminal(id, string(TaskRunning), string(TaskCompleted), string(env))
	if err != nil {
		return Task{}, &Reject{Field: "id", Detail: fmt.Sprintf("task refused: settling task %d: %v", id, err)}
	}
	if !moved {
		return Task{}, &Reject{Field: "status", Detail: fmt.Sprintf("task refused: task %d was settled by another record first — the first record wins", id)}
	}
	settled, err := ts.Get(id)
	if err != nil {
		return Task{}, &Reject{Field: "id", Detail: fmt.Sprintf("task refused: the settled task cannot be read back: %v", err)}
	}
	return settled, nil
}

// Fail moves a running or waiting task to failed — the terminal the CHECK
// admits and T3 deferred the verb for. A failed attempt is still a row: the
// chain edge says it was tried, the task row says how it ended, and neither
// depends on a process that is no longer there.
func (ts *TaskStore) Fail(_ context.Context, id int64, reason string) *Reject {
	return ts.FailWithCost(context.Background(), id, reason, nil)
}

// FailWithCost fails a task while keeping whatever cost the callee managed
// to measure — an expensive failure whose number vanishes with the failure
// is exactly what A1's amendment refuses (unattributed reads as zero).
func (ts *TaskStore) FailWithCost(_ context.Context, id int64, reason string, cost *model.Cost) *Reject {
	if rj := validateCost(cost); rj != nil {
		return rj
	}
	if strings.TrimSpace(reason) == "" {
		return &Reject{Field: "reason", Detail: "a failure must say why — a failed row that cannot be read is decoration"}
	}
	env, err := json.Marshal(resultEnvelope{Outcome: "reject", Result: reason, Cost: cost})
	if err != nil {
		return &Reject{Field: "reason", Detail: fmt.Sprintf("task refused: the reason cannot be stored: %v", err)}
	}
	for _, from := range []string{string(TaskRunning), string(TaskWaiting)} {
		moved, err := ts.s.MarkAgentTaskTerminal(id, from, string(TaskFailed), string(env))
		if err != nil {
			return &Reject{Field: "id", Detail: fmt.Sprintf("task refused: failing task %d: %v", id, err)}
		}
		if moved {
			return nil
		}
	}
	return &Reject{Field: "status", Detail: fmt.Sprintf("task refused: task %d is already terminal — a completed task does not become failed after the fact", id)}
}

func (ts *TaskStore) FrozenSet(id int64) ([]policy.Policy, string, error) {
	t, err := ts.s.GetAgentTask(id)
	if err != nil {
		return nil, "", err
	}
	var set []policy.Policy
	if err := json.Unmarshal([]byte(t.PoliciesJSON), &set); err != nil {
		return nil, t.PoliciesHash, fmt.Errorf("task %d carries a policy set that does not parse: %w", id, err)
	}
	return set, t.PoliciesHash, nil
}

// FrozenSetIntact recomputes the hash over the parsed entries and compares —
// S7's poisoned-manifest rule at the task layer: a mismatch is detected, and
// detection is the whole value of the column.
func (ts *TaskStore) FrozenSetIntact(id int64) (bool, error) {
	t, err := ts.s.GetAgentTask(id)
	if err != nil {
		return false, err
	}
	var set []policy.Policy
	if err := json.Unmarshal([]byte(t.PoliciesJSON), &set); err != nil {
		return false, nil // unparseable is not intact
	}
	hash, err := HashPolicies(set)
	if err != nil {
		return false, err
	}
	return hash == t.PoliciesHash, nil
}

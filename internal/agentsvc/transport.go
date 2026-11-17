package agentsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// transport.go — the agent.* methods of S14 T10, the agentsvc half.
//
// # No new transport (P25/N7)
//
// Aeon already has a socket speaking JSON-RPC 2.0 — which is what A2A
// specifies — so this file is a method IMPLEMENTATION, not a listener: no
// Listen, no bind, no port, no host, no URL accepted as configuration, no
// TLS (the grep test holds all of it; if a remote address could be
// configured here, S21's trust-store work would have been pre-empted
// without its egress policy). The protocol side (internal/protocol/agent.go)
// owns framing, the enumeration of what each method cannot do, and the
// event fan-out; this file owns what the methods MEAN — and every judgement
// still runs through the same doors T4–T8 built: Invoke's card door, the
// skill enumeration, the SkillAccess verdict for both halves, the
// caller-identity read, the chain, the task layer. The socket is a second
// HAND to one mechanism (MC4), never a second mechanism.
//
// # The frozen set is read SERVER-side or the method does not run (D12)
//
// agent.invoke accepts NO policy set, no verdict, no permission from
// params — a caller-supplied frozen set is a caller authoring its own
// permissions. The seam reads the real store through an injected reader at
// ONE clock reading: the instant the set is listed is the instant the door
// judges (the same single-reading argument as the lane adapter's). Unwired
// reader: a loud infrastructure failure, not an empty set — an empty set
// answers "default deny", and "nobody wired the reader" is not that answer.
//
// # Third-party text has ceilings and identifier gates
//
// Agent and skill names pass policy.CheckIdentifier — the boundary's own
// alphabet, exported for this delegation (SEC-046): these strings reach
// renderings everywhere. Free text (the ask, a result, a reason) is content,
// not identity, so it carries length ceilings instead of alphabet gates:
// socket text that reaches a transcript unbounded is an availability bug
// waiting for a generous callee.

// ceilings, stated rather than discovered.
const (
	maxAskText   = 64 << 10 // the ask, in the caller's words
	maxResultLen = 64 << 10 // what the callee reports back
	maxReasonLen = 4 << 10  // why a task failed
)

// ListPoliciesFunc reads the live policy set as of an instant — cmd/aeon
// wires it over the same policyRepo bridge the gate reads, so the socket
// freezes exactly the set the enforcement is using (the policy.* reads' own
// promise, carried to the invoke door).
type ListPoliciesFunc func(ctx context.Context, at time.Time) ([]policy.Policy, error)

// Transport is the seven methods' meaning, over one store, one invoker and
// one policy reader.
type Transport struct {
	s            *store.Store
	iv           *Invoker
	listPolicies ListPoliciesFunc
}

func NewTransport(s *store.Store, iv *Invoker, listPolicies ListPoliciesFunc) *Transport {
	return &Transport{s: s, iv: iv, listPolicies: listPolicies}
}

// Handles is the agentsvc half of the one-enumeration claim: exactly these
// names, matching protocol's registrations (the test pins the two lists
// against each other and against protocol.AgentServiceMethods).
func Handles() []string {
	return []string{"agent.discover", "agent.card", "agent.invoke", "agent.task.list", "agent.task.show", "agent.task.complete", "agent.task.fail"}
}

// Handle is the seam protocol.Table.Agents forwards to. Refusals — the
// doors' own sentences — come back in `refusal` and travel verbatim;
// `err` is reserved for infrastructure.
func (tr *Transport) Handle(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
	switch method {
	case "agent.discover":
		return tr.discover(params)
	case "agent.card":
		return tr.card(params)
	case "agent.invoke":
		return tr.invoke(ctx, params)
	case "agent.task.list":
		return tr.taskList(params)
	case "agent.task.show":
		return tr.taskShow(ctx, params)
	case "agent.task.complete":
		return tr.taskComplete(ctx, params)
	case "agent.task.fail":
		return tr.taskFail(ctx, params)
	default:
		// Unreachable through the table; loud anyway, because reaching here
		// means the two enumerations have drifted — the drift the tests
		// exist to prevent.
		return nil, "", fmt.Errorf("agentsvc: transport asked for unknown method %q (enumeration drift)", method)
	}
}

// decodeStrict is the params gate: unknown fields refuse by name. A caller
// that typed `callerLoopId` for `callerLoopSlug` must hear about the field,
// not watch the ask go out with a missing identity — DB19 through the
// socket, refusing by field.
func decodeStrict(params json.RawMessage, v any) *Reject {
	if len(params) == 0 {
		params = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(params))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &Reject{Field: "params", Detail: "the params do not match the method: " + err.Error()}
	}
	return nil
}

// ── the reads ───────────────────────────────────────────────────────────────

func (tr *Transport) discover(params json.RawMessage) (any, string, error) {
	var p struct{}
	if rj := decodeStrict(params, &p); rj != nil {
		return nil, rj.Error(), nil
	}
	d := tr.iv.cards.Discover()
	return map[string]any{
		"candidates": d.Candidates,
		"refuses":    d.Refuses,
		// The honest count of what the listing is: broken cards are NOT
		// silently missing from a total (AS9 — one truth, read once).
		"count": len(d.Candidates),
	}, "", nil
}

func (tr *Transport) card(params json.RawMessage) (any, string, error) {
	var p struct {
		Agent string `json:"agent"`
	}
	if rj := decodeStrict(params, &p); rj != nil {
		return nil, rj.Error(), nil
	}
	if rj := policy.CheckIdentifier("agent", p.Agent); rj != nil {
		return nil, rj.Reason, nil
	}
	card, err := tr.iv.cards.Card(p.Agent)
	if err != nil {
		// A card that EXISTS and FAILS the load path is not "no card": the
		// three states stay three. The load error is the why.
		return map[string]any{"agent": p.Agent, "loaded": false, "state": "refused", "why": err.Error()}, "", nil
	}
	if card == nil {
		return map[string]any{"agent": p.Agent, "loaded": false, "state": "absent"}, "", nil
	}
	return map[string]any{"agent": p.Agent, "loaded": true, "state": "loaded", "card": card}, "", nil
}

// ── the ask ─────────────────────────────────────────────────────────────────

type invokeParams struct {
	CallerLoopSlug string         `json:"callerLoopSlug"`
	ComponentRoot  string         `json:"componentRoot"`
	CallerStepID   int64          `json:"callerStepId"`
	CalleeAgent    string         `json:"calleeAgent"`
	SkillID        string         `json:"skillId"`
	Text           string         `json:"text"`
	Params         map[string]any `json:"params"`
	Mode           string         `json:"mode"`
	// InputMode travels (T12 walk): the typed contract checks the media
	// type against the skill's declared modes, and the socket that carries
	// the ask must be able to SAY the media type or the check is a museum
	// piece — visible from the lane, invisible from the app.
	InputMode string `json:"inputMode"`
}

func (tr *Transport) invoke(ctx context.Context, params json.RawMessage) (any, string, error) {
	var p invokeParams
	if rj := decodeStrict(params, &p); rj != nil {
		return nil, rj.Error(), nil
	}
	if p.Mode != "" && p.Mode != "async" {
		// Async-only, stated at the door that enforces it: an ask is
		// answered when the callee reports, and a socket call that BLOCKS on
		// a stranger is S21's problem, not this socket's.
		return nil, `refused: agent.invoke · mode "sync" is not invokable over the socket · the ask is a WAITING task answered by agent.task.complete when the callee reports`, nil
	}
	for _, id := range []struct{ field, value string }{
		{"calleeAgent", p.CalleeAgent}, {"skillId", p.SkillID}, {"callerLoopSlug", p.CallerLoopSlug},
	} {
		if rj := policy.CheckIdentifier(id.field, id.value); rj != nil {
			return nil, rj.Reason, nil
		}
	}
	if p.Text == "" {
		return nil, `refused: text · an ask with no words is not an ask · remedy: say what you want the callee to do`, nil
	}
	if len(p.Text) > maxAskText {
		return nil, fmt.Sprintf("refused: text · %d bytes over the %d-byte ceiling · remedy: shorten the ask — the task row records these words verbatim and every rendering reads them", len(p.Text), maxAskText), nil
	}
	if p.CallerStepID <= 0 {
		return nil, `refused: callerStepId · every ask belongs to a step (P33: nothing here assumes one running step per loop) · remedy: name the step that is asking`, nil
	}
	// The loop identity resolves through the store, and the Invoker's
	// caller-identity gate then demands the step is RUNNING and OWNED by the
	// caller it is claimed from — the socket does not trust the caller's
	// self-report any more than the lane adapter does; both are refused by
	// the same read.
	l, err := tr.s.GetLoop(store.Scope{LoopSlug: p.CallerLoopSlug, ComponentRoot: p.ComponentRoot})
	if err != nil {
		return nil, "", fmt.Errorf("read the caller loop: %w", err)
	}
	// The step identity is READ, not trusted: the ask must name a step that
	// belongs to the loop it names, and the CALLER AGENT comes from the row
	// — the app cannot invoke as an agent the step does not belong to. The
	// Invoker's own caller-identity gate re-checks the same row; two readers
	// of one row is paranoia about a door, which is the correct amount.
	stepRow, err := tr.s.ReadStep(p.CallerStepID)
	if err != nil {
		return nil, fmt.Sprintf(`refused: callerStepId · step %d could not be read (%v) · remedy: name a step that exists`, p.CallerStepID, err), nil
	}
	if stepRow.LoopSlug != p.CallerLoopSlug || stepRow.ComponentRoot != p.ComponentRoot {
		return nil, fmt.Sprintf("refused: callerStepId · step %d belongs to loop %s/%s, not the loop named in the ask — an ask asked from the wrong loop is a misfired ask, not a cross-loop delegation", p.CallerStepID, stepRow.LoopSlug, stepRow.ComponentRoot), nil
	}
	// One clock reading for freeze and verdict (D12, as the adapter).
	if tr.listPolicies == nil {
		return nil, "", fmt.Errorf("the policy reader is not wired — an invoke frozen against nothing would answer its SkillAccess question with the default deny, which is not the truth about this daemon")
	}
	now := time.Now()
	frozen, err := tr.listPolicies(ctx, now)
	if err != nil {
		return nil, "", fmt.Errorf("the policy set could not be read to freeze the task: %w", err)
	}
	task, rj := tr.iv.Invoke(ctx, Invocation{
		CallerLoopID: l.ID, CallerLane: "work", CallerAgent: stepRow.Agent, CallerStepID: p.CallerStepID,
		CalleeAgent: p.CalleeAgent, SkillID: p.SkillID, Text: p.Text, Params: p.Params,
		InputMode: p.InputMode, Mode: TaskAsync, Frozen: frozen, EvaluatedAt: now,
	})
	if rj != nil {
		// The door's own sentence, verbatim — card, enumeration, SkillAccess,
		// identity, chain — this file adds no interpretation (the refusals
		// are values rule, carried to the socket).
		return nil, rj.Error(), nil
	}
	return map[string]any{
		"taskId":       task.ID,
		"status":       string(task.Status),
		"skillRef":     SkillRef(p.CalleeAgent, p.SkillID),
		"callerStepId": p.CallerStepID,
		"policiesHash": task.PoliciesHash,
	}, "", nil
}

// ── the reports and the task reads ──────────────────────────────────────────

func (tr *Transport) taskList(params json.RawMessage) (any, string, error) {
	var p struct {
		CallerLoopSlug string `json:"callerLoopSlug"`
		ComponentRoot  string `json:"componentRoot"`
		CallerStepID   int64  `json:"callerStepId"`
	}
	if rj := decodeStrict(params, &p); rj != nil {
		return nil, rj.Error(), nil
	}
	if p.CallerStepID <= 0 {
		return nil, `refused: callerStepId · tasks are keyed by the step that asked (D12/P33), never by loop`, nil
	}
	if rj := policy.CheckIdentifier("callerLoopSlug", p.CallerLoopSlug); rj != nil {
		return nil, rj.Reason, nil
	}
	l, err := tr.s.GetLoop(store.Scope{LoopSlug: p.CallerLoopSlug, ComponentRoot: p.ComponentRoot})
	if err != nil {
		return nil, "", fmt.Errorf("read the caller loop: %w", err)
	}
	tasks, err := tr.s.ListAgentTasksForCaller(l.ID, p.CallerStepID)
	if err != nil {
		return nil, "", fmt.Errorf("read the tasks: %w", err)
	}
	edges, err := tr.s.ListAgentChainEdges(l.ID, p.CallerStepID)
	if err != nil {
		return nil, "", fmt.Errorf("read the chain edges: %w", err)
	}
	out := make([]map[string]any, 0, len(tasks))
	for _, row := range tasks {
		t := toTask(row)
		out = append(out, taskView(t))
	}
	// MC2 holds in the projection: tasks name callees and skills; neither
	// the callee's tools nor its card internals appear here.
	return map[string]any{"tasks": out, "edges": edges, "count": len(out)}, "", nil
}

func (tr *Transport) taskShow(ctx context.Context, params json.RawMessage) (any, string, error) {
	var p struct {
		TaskID int64 `json:"taskId"`
	}
	if rj := decodeStrict(params, &p); rj != nil {
		return nil, rj.Error(), nil
	}
	if p.TaskID <= 0 {
		return nil, `refused: taskId · name the task to show`, nil
	}
	row, err := tr.s.GetAgentTask(p.TaskID)
	if err != nil {
		return nil, "", fmt.Errorf("read the task: %w", err)
	}
	t := toTask(row)
	view := taskView(t)
	rollup, rj := tr.iv.RollupCost(ctx, t.CallerLoopID, t.CallerStepID)
	if rj != nil {
		return nil, rj.Error(), nil
	}
	// Three-state honesty on the wire: the rollup arrives with its state
	// and its missing reasons — `known` appears only when every cost in the
	// chain reported; partial keeps floors, never rounds them up to a fact.
	view["rollup"] = map[string]any{
		"cost":     rollup.Cost,
		"children": rollup.Children,
		"nodes":    rollup.Nodes,
	}
	return view, "", nil
}

func (tr *Transport) taskComplete(ctx context.Context, params json.RawMessage) (any, string, error) {
	var p struct {
		TaskID  int64       `json:"taskId"`
		Result  string      `json:"result"`
		Outcome string      `json:"outcome"`
		Cost    *model.Cost `json:"cost"`
	}
	if rj := decodeStrict(params, &p); rj != nil {
		return nil, rj.Error(), nil
	}
	if p.TaskID <= 0 {
		return nil, `refused: taskId · name the task being settled`, nil
	}
	if len(p.Result) > maxResultLen {
		return nil, fmt.Sprintf("refused: result · %d bytes over the %d-byte ceiling · remedy: report what the callee said; the ceiling bounds the socket, not the work", len(p.Result), maxResultLen), nil
	}
	outcome := p.Outcome
	if outcome == "" {
		outcome = "ok"
	}
	// A report with no cost records unknown-with-a-reason, NEVER zero —
	// cost by absence is the confident wrong answer (three states).
	cost := p.Cost
	if cost == nil {
		cost = &model.Cost{State: model.CostUnknown, Reason: "cost-not-reported"}
	}
	if rj := tr.iv.Tasks().CompleteWithCost(ctx, p.TaskID, p.Result, outcome, cost); rj != nil {
		return nil, rj.Error(), nil
	}
	return map[string]any{"taskId": p.TaskID, "status": "done"}, "", nil
}

func (tr *Transport) taskFail(ctx context.Context, params json.RawMessage) (any, string, error) {
	var p struct {
		TaskID int64       `json:"taskId"`
		Reason string      `json:"reason"`
		Cost   *model.Cost `json:"cost"`
	}
	if rj := decodeStrict(params, &p); rj != nil {
		return nil, rj.Error(), nil
	}
	if p.TaskID <= 0 {
		return nil, `refused: taskId · name the task being failed`, nil
	}
	if p.Reason == "" {
		return nil, `refused: reason · a failure without a reason is a failure nobody can fix · remedy: say why, in the words you would want in a transcript`, nil
	}
	if len(p.Reason) > maxReasonLen {
		return nil, fmt.Sprintf("refused: reason · %d bytes over the %d-byte ceiling", len(p.Reason), maxReasonLen), nil
	}
	if rj := tr.iv.Tasks().FailWithCost(ctx, p.TaskID, p.Reason, p.Cost); rj != nil {
		return nil, rj.Error(), nil
	}
	return map[string]any{"taskId": p.TaskID, "status": "failed"}, "", nil
}

// taskView is the socket's face of a Task: identity, state, cost as the
// three states, the hash of the set the ask was judged under (AS13 made
// visible). NOT the raw envelope: the result text is the callee's words and
// they belong in the record; what a reader gets is the report plus its
// shape. (The untrusted region is applied where the text REACHES A MODEL —
// untrusted.go — not in a listing.)
func taskView(t Task) map[string]any {
	return map[string]any{
		"taskId":       t.ID,
		"callerLoopId": t.CallerLoopID,
		"callerLane":   t.CallerLane,
		"callerStepId": t.CallerStepID,
		"calleeAgent":  t.CalleeAgent,
		"skillId":      t.SkillID,
		"mode":         string(t.Mode),
		"status":       string(t.Status),
		"text":         t.Text,
		"cost":         t.Cost,
		"policiesHash": t.PoliciesHash,
		"createdAt":    t.CreatedAt,
		"completedAt":  t.CompletedAt,
	}
}

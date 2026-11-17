package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

type RecoverySummary struct {
	Total     int `json:"total"`
	Recovered int `json:"recovered"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

type recoveryStepEvent struct {
	StepID int64        `json:"stepId"`
	Case   runtime.Case `json:"case"`
	Index  int          `json:"index"`
	Total  int          `json:"total"`
	Reason string       `json:"reason,omitempty"`
	DryRun bool         `json:"dryRun,omitempty"`
}

type daemonRecoveryWriter struct {
	st       *store.Store
	stepID   int64
	loopSlug string
}

func (w daemonRecoveryWriter) Begin(ctx context.Context, id, name, digest string) error {
	return w.st.BeginReceipt(ctx, w.stepID, id, name, digest)
}
func (w daemonRecoveryWriter) Complete(ctx context.Context, id, result string, isError bool) error {
	return w.st.CompleteReceipt(ctx, w.stepID, id, result, isError)
}
func (w daemonRecoveryWriter) Save(ctx context.Context, cp runtime.Checkpoint) error {
	raw, err := json.Marshal(protocol.StepSnapshot{ContextManifest: cp.Manifest, Request: cp.Request, LoopSlug: w.loopSlug, Usage: cp.Usage, Compactions: cp.Compactions})
	if err != nil {
		return err
	}
	return w.st.SaveRunningCheckpoint(ctx, w.stepID, cp.Messages, raw, cp.Cost)
}

func recoveryClasses() map[string]string {
	out := map[string]string{"unparseable": "safe"}
	for name, tool := range tools.Registry() {
		out[name] = string(tool.RerunClass())
	}
	return out
}

// Recover scans only running rows. It reports every classification before
// acting, and commits a final outcome per step so a second pass skips it.
func (d *Daemon) Recover(ctx context.Context, dryRun bool) (RecoverySummary, error) {
	if d.store == nil {
		return RecoverySummary{}, fmt.Errorf("recovery requires a store")
	}
	ids, err := d.store.RunningStepIDs()
	if err != nil {
		return RecoverySummary{}, err
	}
	summary := RecoverySummary{Total: len(ids)}
	d.recoveryMu.Lock()
	for _, id := range ids {
		d.recovering[id] = struct{}{}
	}
	d.recoveryMu.Unlock()
	defer func() {
		d.recoveryMu.Lock()
		for _, id := range ids {
			delete(d.recovering, id)
		}
		d.recoveryMu.Unlock()
	}()
	d.publish(protocol.KindRecoveryStarted, "", map[string]any{"total": len(ids)})
	for i, id := range ids {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		plan, st, snapshot, msgs := d.inspectRecoveryStep(id)
		ev := recoveryStepEvent{StepID: id, Case: plan.Case, Index: i + 1, Total: len(ids), Reason: plan.Reason, DryRun: dryRun}
		d.log.Infof("recovering %d of %d: step %d case %s %s", i+1, len(ids), id, plan.Case, plan.Reason)
		d.publish(protocol.KindRecoveryStep, st.ComponentRoot, ev)
		if dryRun {
			summary.Skipped++
			d.clearRecovering(id)
			continue
		}
		if plan.Case == runtime.CaseUnclassifiable {
			if err := d.commitRecoveryFailure(ctx, id, st, snapshot, msgs, plan, plan.Reason); err != nil {
				d.log.Infof("recovery of step %d could not be committed: %v", id, err)
			}
			d.publish(protocol.KindRecoveryResolved, st.ComponentRoot, map[string]any{"stepId": id, "outcome": "failed", "failure": recoveryFailure(plan, plan.Reason)})
			summary.Failed++
			d.clearRecovering(id)
			continue
		}
		if err := d.guardedRecoverOne(ctx, id, st, snapshot, msgs, plan); err != nil {
			d.log.Infof("recovery of step %d failed: %v", id, err)
			_ = d.commitRecoveryFailure(ctx, id, st, snapshot, msgs, plan, err.Error())
			d.publish(protocol.KindRecoveryResolved, st.ComponentRoot, map[string]any{"stepId": id, "outcome": "failed", "failure": recoveryFailure(plan, err.Error())})
			summary.Failed++
		} else {
			resolved, _ := d.store.ReadStep(id)
			if resolved.Outcome == "failed" {
				summary.Failed++
			} else {
				summary.Recovered++
			}
		}
		d.clearRecovering(id)
		debugCrashDuringRecovery(i + 1)
	}
	if !dryRun {
		// S70 T2d: asks whose peers' turns died with the previous process
		// are still rows — dispatched again from what the table says.
		d.mailboxScanWaiting()
	}
	d.publish(protocol.KindRecoveryFinished, "", summary)
	return summary, nil
}

func (d *Daemon) guardedRecoverOne(ctx context.Context, id int64, st store.Step, snapshot protocol.StepSnapshot, msgs []model.Message, plan runtime.RecoveryPlan) (err error) {
	defer func() {
		if panicValue := recover(); panicValue != nil {
			err = fmt.Errorf("recovery panicked for step %d: %v", id, panicValue)
		}
	}()
	return d.recoverOne(ctx, id, st, snapshot, msgs, plan)
}

func (d *Daemon) isRecovering(id int64) bool {
	d.recoveryMu.RLock()
	defer d.recoveryMu.RUnlock()
	_, ok := d.recovering[id]
	return ok
}
func (d *Daemon) clearRecovering(id int64) {
	d.recoveryMu.Lock()
	delete(d.recovering, id)
	d.recoveryMu.Unlock()
}

func (d *Daemon) inspectRecoveryStep(id int64) (runtime.RecoveryPlan, store.Step, protocol.StepSnapshot, []model.Message) {
	st, err := d.store.ReadStep(id)
	if err != nil {
		return runtime.RecoveryPlan{Case: runtime.CaseUnclassifiable, Reason: fmt.Sprintf("step row cannot be read: %v", err)}, store.Step{ID: id}, protocol.StepSnapshot{}, nil
	}
	var snapshot protocol.StepSnapshot
	if len(st.ContextManifestRaw) == 0 {
		return runtime.RecoveryPlan{Case: runtime.CaseUnclassifiable, Reason: "the frozen context manifest is absent"}, st, snapshot, nil
	}
	if err := json.Unmarshal(st.ContextManifestRaw, &snapshot); err != nil || snapshot.Request.WorkspaceRoot == "" {
		return runtime.RecoveryPlan{Case: runtime.CaseUnclassifiable, Reason: "the frozen step request is absent or invalid"}, st, snapshot, nil
	}
	transcript := d.store.ReadTranscript(id)
	var msgs []model.Message
	if !transcript.OK {
		if transcript.Count != 0 {
			return runtime.RecoveryPlan{Case: runtime.CaseUnclassifiable, Reason: transcript.Reason()}, st, snapshot, nil
		}
	} else {
		msgs = transcript.Messages
	}
	rows, err := d.store.Receipts(context.Background(), id)
	if err != nil {
		return runtime.RecoveryPlan{Case: runtime.CaseUnclassifiable, Reason: fmt.Sprintf("receipts cannot be read: %v", err)}, st, snapshot, msgs
	}
	receipts := make([]runtime.RecoveryReceipt, 0, len(rows))
	for _, row := range rows {
		receipts = append(receipts, runtime.RecoveryReceipt{ToolUseID: row.ToolUseID, Tool: row.Tool, ArgDigest: row.ArgDigest,
			Completed: row.CompletedAt != nil, Result: row.Result, IsError: row.IsError != nil && *row.IsError})
	}
	return runtime.DetermineCase(msgs, receipts, recoveryClasses()), st, snapshot, msgs
}

func (d *Daemon) recoverOne(ctx context.Context, id int64, st store.Step, snapshot protocol.StepSnapshot, msgs []model.Message, plan runtime.RecoveryPlan) error {
	if len(msgs) == 0 {
		msgs = snapshot.Request.Messages
	}
	granted := make([]string, 0, len(snapshot.Request.Tools))
	for _, spec := range snapshot.Request.Tools {
		granted = append(granted, spec.Name)
	}
	toolset, _, err := d.Table.Runner.BuildTools(granted, snapshot.Request.WorkspaceRoot, snapshot.Request.Handler.Name,
		frozenManifest(snapshot.ContextManifest))
	if err != nil {
		return fmt.Errorf("rebuild tools: %w", err)
	}
	// MD5: the rebuilt toolset may carry cached MCP server children (a composite
	// caller); close them when recovery finishes — not immediately after
	// BuildTools. A no-op when no MCP bundle is wired (MD8).
	defer func() {
		if c, ok := toolset.(io.Closer); ok {
			_ = c.Close()
		}
	}()
	writer := daemonRecoveryWriter{st: d.store, stepID: id, loopSlug: st.LoopSlug}
	priorCost := st.Cost
	if plan.Case == runtime.CaseB {
		if err := d.store.SaveRecoveryObservation(ctx, id, string(plan.Case), "model call re-issued after crash; earlier in-flight usage may be unknown", true); err != nil {
			return err
		}
		// The interrupted call may have been billed without leaving usage. Its
		// cost is unknown even when earlier completed calls were measured.
		priorCost = model.InterruptedCost(st.Cost)
	}
	req := runtime.ResumeRequest{Request: snapshot.Request, Messages: msgs, Manifest: snapshot.ContextManifest,
		Usage: snapshot.Usage, Cost: priorCost, Compactions: snapshot.Compactions, Recovery: true}
	// Q6 answered, recovery half (S18 T8): the loop's CURRENT route governs
	// the re-issued call, not the bytes the snapshot froze. S7 reconstructed
	// the route from the step's saved request — so a crash between a steer
	// and the next dispatch silently undid a deliberate switch, the exact
	// failure the task names. A step no loop owns keeps its snapshot route.
	if st.LoopSlug != "" && st.ComponentRoot != "" {
		qs := &QuestionStore{store: d.store}
		if name, ok := qs.StepRoute(id); ok && name != "" && snapshot.Request.Route.ID != name {
			route, ok := d.Table.Routes.Route(name)
			if !ok {
				return fmt.Errorf("recovery of step %d: loop %s is now on route %q, which is not "+
					"configured; the switch is honoured rather than silently reverted to the route "+
					"the step started on", id, st.LoopSlug, name)
			}
			req.Request.Route = route
		}
	}
	// The row's id is asserted by whoever recovered the row (S10): a
	// disagreement re-run during recovery is charged to THIS step, and the
	// stored request may predate the field.
	req.Request.StepID = id
	// The operator decision data arrives through the adapter on this entry
	// too (S11), never from the stored request's bytes: CaseC re-runs ask the
	// gate, and a re-run the gate asks about must suspend honestly — with a
	// question only when somebody may answer, and as a deny when nobody may.
	if d.store != nil {
		req.Request.HasOperator = OperatorPresentFor(d.store, d.log)(ctx, id)
		req.Request.AutoApprove = AutoApproveFor(d.store, d.log)(ctx, id)
	}
	result := runtime.RecoverStep(ctx, req, plan, runtime.RunOpts{Client: d.Table.Models, Tools: toolset, Receipts: writer, Checkpoint: writer,
		Policy:    d.Table.PolicyGate,
		PolicySet: d.Table.PolicySet,
		// A steer typed before the crash is still pending, and the recovered
		// run is that queue's only remaining boundary: the replay phase reads
		// no queue (the tool results it replays are history), and the loop it
		// hands over to drains at its first turn (D3 — recovery included).
		Steers: steerSourceFor(d.store, d.Table.Routes, d.resolveChosenSkills)(id),
		// S70 MB4: the recovered run is the pending queue's only remaining
		// boundary, mailbox included — a peer's words that arrived before
		// the crash are still pending and arrive at this run's first turn;
		// the ask door rides the recovered continuation so an ask the
		// re-run files goes through the same doors as any other (D3 — the
		// steers' argument, quoted). Storeless daemons never reach
		// recovery: the rows it reads live in the store.
		MailboxDrain: mailboxDrainFor(d.store)(id),
		Todo:         recoveredTodoPolicy(d.Table, id),
		MailboxAsk:   mailboxAskFor(d.store, d.publish)(id),
		// S55 T7 (E2 on the recovery path): the recovered re-run prices
		// itself against the same budgets the live path does, so a crash
		// cannot spend past a cap the live path would have refused. Nil on
		// a storeless daemon — the check absent, the same as the live path.
		Budget: d.recoverBudgetCheck(st.LoopSlug, st.ComponentRoot),
		// S33 T2: a recovered step's re-run model calls are spend events
		// like any other — the crash path captures too (T9's archive test
		// depends on crash-path turns having rows). The same adapter object
		// the socket path runs through; a storeless daemon gets nil (I2).
		Telemetry:   newTelemetrySink(d.store),
		HandoffSpec: runtime.DefaultHandoffSpec(), Sink: runtime.SinkFunc(func(e runtime.StepEvent) {
			d.publish(e.Kind, st.ComponentRoot, protocol.StepEventPayload{StepID: id, Tool: e.Tool, Text: e.Text, Summary: e.Summary,
				Result: e.Result, IsError: e.IsError, Iteration: e.Iteration, Outcome: string(e.Outcome), Reason: e.Reason,
				Start: e.Start, End: e.End, EstimateBefore: e.EstimateBefore, EstimateAfter: e.EstimateAfter, Cost: e.Cost})
		})})
	// S55 T7 (E2): a re-run that hits the budget cap suspends with a PAUSE
	// REASON, not a question — the checkpoint is intact and loop.raise
	// continues it. The no-question shape is the cap's, so it is admitted
	// here exactly as the live path admits it; every other no-question
	// suspension is still a defect the error names.
	var pauseReason string
	var question *store.RecoveryQuestion
	if result.Kind == runtime.ResultSuspended {
		if result.Question == nil {
			if result.PauseReason == "cap" {
				pauseReason = "cap"
			} else {
				return fmt.Errorf("recovered step suspended without a question")
			}
		} else {
			question = &store.RecoveryQuestion{ToolUseID: result.Question.ToolUseID, Kind: store.QuestionKind(result.Question.Kind), Body: result.Question.Body, Options: result.Question.Options}
		}
	}
	raw, err := json.Marshal(protocol.StepSnapshot{ContextManifest: result.Manifest, Request: snapshot.Request, LoopSlug: st.LoopSlug, Usage: result.Usage, Compactions: result.Compactions})
	if err != nil {
		return err
	}
	reason := result.Reason
	if plan.Case == runtime.CaseB {
		reason = strings.TrimSpace("model call re-issued after crash; " + reason)
	}
	var storedFailure *store.RecoveryFailure
	if result.Outcome == runtime.OutcomeFailed {
		storedFailure = storeFailure(plan, reason)
	}
	var costs []model.Cost
	if plan.Case == runtime.CaseB {
		costs = []model.Cost{priorCost, result.AdditionalCost}
	}
	if err := d.store.CommitRecoveredStep(ctx, id, result.Messages, raw, result.Cost, string(result.Outcome), string(plan.Case), reason, pauseReason, plan.Case == runtime.CaseB, question, storedFailure, costs...); err != nil {
		return err
	}
	if question != nil {
		if row, ok, err := d.store.PendingStepQuestion(id); err == nil && ok {
			d.publish(protocol.KindQuestionAsked, st.ComponentRoot, runtime.PendingQuestion{ID: row.ID, StepID: id, ToolUseID: row.ToolUseID, Body: row.Body, Kind: row.Kind, Options: row.Options})
		}
	}
	payload := map[string]any{"stepId": id, "case": plan.Case, "outcome": result.Outcome}
	if plan.Case == runtime.CaseB {
		payload["priorCost"], payload["reissueCost"] = priorCost, result.AdditionalCost
	}
	if result.Outcome == runtime.OutcomeFailed {
		payload["failure"] = recoveryFailure(plan, reason)
	}
	// S70 T2d: the replay's endings need the hooks too — a recovered step
	// that suspends dispatches the peers its replayed asks addressed, and
	// one that completes files the replies its boundary drained. After the
	// COMMIT, so the hooks always read committed rows (D4's ordering,
	// quoted at the door).
	switch result.Kind {
	case runtime.ResultSuspended:
		d.mailboxDispatchTurns(st.ComponentRoot, st.LoopSlug, id)
	case runtime.ResultComplete:
		summary := ""
		if result.Handoff != nil {
			if txt, ok := (*result.Handoff)["summary"].(string); ok {
				summary = strings.TrimSpace(txt)
			}
		}
		d.mailboxReplyTurns(id, summary)
	}
	d.publish(protocol.KindRecoveryResolved, st.ComponentRoot, payload)
	return nil
}

func recoveryFailure(plan runtime.RecoveryPlan, reason string) runtime.RecoveryFailure {
	f := runtime.RecoveryFailure{Kind: "unclassifiable", Detail: reason}
	for _, call := range plan.Calls {
		if call.Action == runtime.FailStep {
			f.Tool = call.Tool
			f.ToolUseID = call.ToolUseID
			f.Kind = "unknown-tool-outcome"
			if strings.Contains(call.Reason, "digest-mismatch") {
				f.Kind = "digest-mismatch"
			}
			if strings.Contains(call.Reason, "without a result") {
				f.Kind = "invalid-receipt"
			}
			break
		}
	}
	if plan.Case != runtime.CaseUnclassifiable && f.Tool == "" {
		f.Kind = "recovery-failed"
	}
	return f
}

func storeFailure(plan runtime.RecoveryPlan, reason string) *store.RecoveryFailure {
	f := recoveryFailure(plan, reason)
	return &store.RecoveryFailure{Kind: f.Kind, Tool: f.Tool, ToolUseID: f.ToolUseID, Detail: f.Detail}
}

func (d *Daemon) commitRecoveryFailure(ctx context.Context, id int64, st store.Step, snapshot protocol.StepSnapshot, msgs []model.Message, plan runtime.RecoveryPlan, reason string) error {
	raw := st.ContextManifestRaw
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	return d.store.CommitRecoveredStep(ctx, id, msgs, raw, st.Cost, "failed", string(plan.Case), reason, "", false, nil, storeFailure(plan, reason))
}

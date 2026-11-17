package daemon

import (
	"errors"

	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/budget"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func (d *Daemon) initBlueprints() {
	if d.store == nil {
		return
	}
	// S48 T3 (BP2): the store is THE source. The daemon reads through its
	// OWN instance; the process install serves the CLI/offline readers and
	// any package-level caller that holds no store.
	if err := SeedStandardBlueprints(d.store); err != nil {
		d.log.Infof("blueprint seed refused: %v", err)
	}
	d.bpSrc = StoreBlueprintSource(d.store)
	InstallBlueprintSource(d.store)
	InstallAgentHandlerSource(d.store)
	dep := loop.Deps{Store: d.store, Records: recordsReaderFor(d.store), Event: func(kind string, payload any) { d.publishLoop(kind, payload) }, Actions: d.actionServices, RecoveredStep: func(_ context.Context, st loop.LoopState) (loop.ModelResult, error) {
		step, err := d.store.ReadStep(st.LastStepID)
		if err != nil {
			return loop.ModelResult{}, err
		}
		if step.LoopSlug != st.Slug || step.ComponentRoot != st.ComponentRoot {
			return loop.ModelResult{}, fmt.Errorf("step %d does not belong to loop %s", step.ID, st.ID)
		}
		cost, _ := json.Marshal(step.Cost)
		out := loop.ModelResult{StepID: step.ID, Kind: "complete", Outcome: step.Outcome, Cost: cost}
		switch step.Status {
		case "done":
			return out, nil
		case "suspended":
			if step.PauseReason == "user" {
				// S31 D6 — a PERSON stopped this step (before a daemon
				// restart, say). The loop re-parks `waiting/stopped`; the
				// "suspended with no question" error below is for the
				// genuinely contradictory case, and a stopped step is not it.
				out.Kind, out.Outcome = "stopped", ""
				return out, nil
			}
			if step.PauseReason == "cap" {
				// S55 T7 (E2) — a cap parked this step (before a daemon
				// restart, say). Re-block the loop on cap-hit; the hit's
				// numbers are not in the step row (they are the engine's,
				// not the step's), so the loop re-reads them from its own
				// pin's CapHit — the raise's CAS keeps it until the re-run.
				out.Kind, out.Outcome = "capped", ""
				return out, nil
			}
			q, ok, err := d.store.PendingStepQuestion(step.ID)
			if err != nil {
				return loop.ModelResult{}, err
			}
			if !ok {
				return loop.ModelResult{}, fmt.Errorf("suspended step %d has no question", step.ID)
			}
			out.Kind, out.QuestionID = "suspended", q.ID
			return out, nil
		default:
			return loop.ModelResult{}, fmt.Errorf("step %d is still %s after recovery", step.ID, step.Status)
		}
	}, Model: func(ctx context.Context, st loop.LoopState, step blueprint.Step, prompt, frame string) (loop.ModelResult, error) {
		agent, err := d.resolveDispatchAgent(st, step)
		if err != nil {
			// T10: an unknown tech tag REFUSES here — the Refusal text (the
			// tag named, the available tags listed) becomes the loop's
			// durable block reason through the ordinary blocked path. There
			// is no fallback handler (A13): a generic handler writing an
			// unfamiliar language reports `ok` and the wrongness surfaces
			// three steps later, attributed to the model.
			return loop.ModelResult{}, err
		}
		// S68 B6: the launch's chosen skills ride this launch's own model
		// steps — the system message the launch speaks. A `skill` step is
		// a DIFFERENT body being asked (S14), and what rides ITS system
		// message is the agent-to-agent question, not this launch's
		// operator choice (S70's territory); it keeps dispatchSkill as-is.
		if step.Skill != "" {
			// S14 T4: a model step that names a skill is an agent asking
			// another agent — not a model call. The asking belongs to
			// agentsvc; the branch belongs here because the LANE semantics
			// (this step, this loop, this exit) belong to the loop runtime.
			return d.dispatchSkill(ctx, st, step, prompt, frame)
		}
		params := stepParamsFor(st, step, prompt, frame)
		d.applyLoopModelOptions(ctx, st.ID, &params)
		if chosen := d.chosenForLoop(st); chosen != nil {
			params.Skills = chosen.Lines
			params.ChosenSkillsNote = chosen.Note
		}
		// S77: the launch's ATTACHED files ride this launch's own model steps
		// beside its chosen skills — the kickoff row's receipts reach the ONE
		// expansion point as references (launchAttachmentParams is where they
		// are checked against the step's route — see launch_attachments.go).
		// A `skill` step
		// dispatched above rides its asking agent's body, not this launch's
		// attachments — same carve-out S68 drew for chosen skills.
		d.launchAttachmentParams(st, &params)
		params.Agent = agent // the RESOLVED name (T10 seam), never the raw lane name
		result, err := d.Table.RunStepForLoop(ctx, params,
			protocol.LoopStepIdentity{LoopID: st.ID, Lane: st.Position.Lane, Step: step.ID},
			func(stepID int64) error {
				if err := loop.MarkStepStarted(ctx, loop.Deps{Store: d.store}, st.ID, st.Position, stepID); err != nil {
					return err
				}
				// S31 D2 — the orchestrator names the step it dispatched BEFORE
				// the step's first event can exist. This callback is the only
				// seam where the id is known and the runtime has not started,
				// so ordering the link here is structural, not a race a reader
				// has to trust. `reasoningEffort` is absent until loop steps
				// carry one — an absent field reads as "none named", never as
				// the route's value (P31 honesty about what the daemon knows).
				d.publishLoop(protocol.KindLoopStepDispatched, map[string]any{
					"loopId": st.ID, "lane": st.Position.Lane, "step": step.ID,
					"stepId": stepID, "agent": agent, "route": params.Route,
				})
				return nil
			})
		if err != nil {
			return loop.ModelResult{}, err
		}
		cost, _ := json.Marshal(result.Cost)
		out := loop.ModelResult{StepID: result.StepID, Kind: result.Kind, Outcome: result.Outcome, Reason: result.Reason, Cost: cost,
			PendingRoute: result.PendingRoute}
		if result.PauseReason == "user" {
			// S31 D6 — the person stopped this step. Map to the ladder's
			// `stopped` kind, which PARKS the loop (waiting, reason
			// `stopped`). This must be decided from the pause REASON, not
			// from the runtime's kind: a stopped step reports `suspended`
			// with no question, and a loop waiting questionably-without-a-
			// question is the plausible wrong answer D6 exists to prevent.
			out.Kind, out.Outcome, out.QuestionID = "stopped", "", 0
			return out, nil
		}
		if result.PauseReason == "cap" {
			// S55 T7 (E2) — the step's own in-step check hit a cap. Map to
			// the ladder's `capped` kind, which BLOCKS the loop on cap-hit
			// with the hit carried (the raise's door reads it to find the
			// budget to raise). The same one rule as `stopped`: decided
			// from the pause REASON, never the runtime's kind — a capped
			// step reports `suspended` with no question, and the loop must
			// not wait on a question nobody filed. out.Reason already
			// carries the hit's sentence (the seam's verdict.Reason, mapped
			// above), and the mailbox owes the capped step nothing: it
			// waits on a raise, not on a peer.
			out.Kind, out.Outcome, out.QuestionID = "capped", "", 0
			if result.CapHit != nil {
				out.CapHit = &loop.CapHitState{BudgetID: result.CapHit.BudgetID, Scope: result.CapHit.Scope,
					Subject: result.CapHit.Subject, Dimension: result.CapHit.Dimension, Window: result.CapHit.Window,
					SpendFloor: result.CapHit.SpendFloor, Cap: result.CapHit.Cap, Reason: result.CapHit.Reason,
					Exact: result.CapHit.Exact}
			}
			return out, nil
		}
		if result.Question != nil {
			out.QuestionID = result.Question.ID
		}
		// S70 T2d: a step that FINISHED speaks its owed replies (the rows
		// name them — the delivery pair, not memory); a step that
		// SUSPENDED dispatches the peers its wait:true asks addressed and
		// run nowhere. The stopped-by-person path owes neither: its loop
		// waits on the person, not on a peer.
		if result.Question == nil && result.PauseReason != "user" {
			d.mailboxReplyTurns(result.StepID, handoffSummary(result.Handoff))
		} else if result.Question != nil {
			d.mailboxDispatchTurns(st.ComponentRoot, st.Slug, result.StepID)
		}
		return out, nil
	},
		Budget: func(ctx context.Context, st loop.LoopState) (budget.Verdict, error) {
			// S55 T7 (E1): the pre-dispatch check's seam. The same store-backed
			// verdict the runtime's in-step check reads (one seam, two
			// boundaries) — nil dep in a test is "unpriced", the check absent.
			return d.budgetVerdictFor(ctx, st.ID)
		},
		BudgetEvent: func(hit budget.Hit, loopID string) error {
			// The hit audit row (the engine's voice is system/engine — a hit is
			// the spend reaching the cap, never somebody acting).
			if err := d.store.RecordBudgetHit(loopID, hit.BudgetID, hit.Dimension, hit.SpendFloor, hit.Cap, hit.Exact); err != nil {
				return err
			}
			// S55 T8: the hit is a screen event too — the loop is about to
			// park (or was just refused), and the person the cap is over
			// sees the exhaustion in the stream, not only in the ladder.
			d.publishBudgetEvent(protocol.KindBudgetHit, d.budgetEventRoot(hit.Scope, hit.SubjectID, loopID), map[string]any{
				"budgetId": hit.BudgetID, "scope": hit.Scope, "subjectId": hit.SubjectID,
				"dimension": hit.Dimension, "spendFloor": hit.SpendFloor, "cap": hit.Cap,
				"exact": hit.Exact, "loopId": loopID,
			})
			return nil
		},
		Autonomy: d.autonomySeams(),
		// The merged fact (VG14, S57 T6): the engine's Merged hook,
		// written by the daemon right after a successful forge.merge
		// dispatch (verify_wiring.go). A failure is the engine's
		// loop.warning — the merge stands.
		Merged: d.mergeEventRecorder,
	}
	d.loopRunner = loop.NewRunner(dep)
	// S55 T3: the park hook is a no-op safety net. A stop's park is a
	// compare-and-set (the store's ParkLoopStopped) and the control core's Stop
	// is its single writer — it runs the park AFTER the runner has settled
	// (Stop → Wait → ParkLoopStopped), so the person's actor and reason ride
	// the one control event, and a lost race is a no-op that names the status
	// the loop already moved to (LC2, LC10). A stop that reaches the runner by
	// some other path just lets it exit; the loop is parked by the operation,
	// never twice.
	d.loopRunner.SetPark(func(_ string) {})
	d.Table.OnStepContinued = func(stepID int64, kind, outcome, reason string, questionID int64, cost json.RawMessage, pendingRoute string) {
		// S70 T2d: the continuation's own endings need the same hooks — a
		// step that completes may have drained peer words mid-continuation
		// (it owes replies), and one that suspends again dispatches the
		// peers its NEW asks addressed. The continuation carries no handoff
		// text to answer with; the reply then REPORTS that honestly rather
		// than inventing words (recorded deviation in T2 Notes).
		if kind == "suspended" {
			if cst, err := d.store.ReadStep(stepID); err == nil && cst.LoopSlug != "" {
				d.mailboxDispatchTurns(cst.ComponentRoot, cst.LoopSlug, stepID)
			}
		} else if kind != "stopped" && kind != "capped" {
			// `capped` (S55 T7) owes the mailbox nothing, for the same
			// reason `stopped` does: the step waits on a raise, not on a
			// peer — and a reply written into a re-blocked step's
			// transcript would spend words the cap just refused.
			d.mailboxReplyTurns(stepID, "")
		}
		pins, err := d.store.PinnedLoopsByStatus("waiting")
		if err != nil {
			d.publish("loop.blocked", "", map[string]any{"reason": err.Error()})
			return
		}
		for _, pin := range pins {
			var saved loop.RuntimeState
			if json.Unmarshal([]byte(pin.RuntimeJSON), &saved) != nil || saved.LastStepID != stepID {
				continue
			}
			result := loop.ModelResult{StepID: stepID, Kind: kind, Outcome: outcome, Reason: reason, QuestionID: questionID, Cost: cost,
				PendingRoute: pendingRoute}
			if err := loop.CompleteSuspendedStep(context.Background(), dep, pin.Loop.ID, result); err != nil {
				d.publish("loop.blocked", "", map[string]any{"loopId": pin.Loop.ID, "reason": err.Error()})
				return
			}
			if kind != "suspended" && kind != "stopped" {
				// Both waiting handbacks have just been PARKED by
				// CompleteSuspendedStep; kicking the runner for either would
				// move a ladder that is not allowed to move. `stopped` is
				// S31 D6's second person-stop of a continued step.
				//
				// And for the completing handbacks: start the runner ONLY if
				// the row says there is work. The final step's completion
				// already ran through `done` INSIDE CompleteSuspendedStep
				// (which saves it and emits loop.finished itself); starting
				// the runner there would advance a settled loop for a second,
				// redundant pass — writing and publishing after the person
				// watching had already seen the loop finish, which S31 T2's
				// stop/continue hammering exposed as goroutines outliving
				// every observer. `running` is the only status with a next
				// step to run; `done` and `blocked` have none.
				if fresh, err := d.store.ReadPinnedLoop(pin.Loop.ID); err == nil && fresh.Loop.Status == "running" {
					// T4 (LC19): started on the daemon-lifetime loopCtx, so the
					// shutdown Drain reaches this goroutine too (a recovery
					// re-start must not be an un-drainable context.Background()).
					d.loopRunner.Start(d.loopCtx, pin.Loop.ID)
				}
			}
			return
		}
	}
	d.Table.StartBlueprintLoop = func(ctx context.Context, p protocol.LoopStartParams) (any, error) {
		// S40 T4: the body lifted to start_loop.go verbatim — one door for the
		// socket and the event fire alike. NOT `return d.startLoopWith(...)`:
		// a nil map returned beside a nil error would wrap into a NON-NIL
		// `any`, and every caller type-asserts the map.
		res, err := d.startLoopWith(ctx, p, startExtras{})
		if err != nil {
			return nil, err
		}
		return res, nil
	}
	d.Table.ShowBlueprintLoop = func(ctx context.Context, p protocol.LoopShowParams) (any, error) {
		st, err := loop.Pinned(ctx, d.store, p.ID)
		if err != nil {
			return nil, err
		}
		return d.viewLoop(st, p.IncludeBlueprint), nil
	}
	d.Table.AnswerBlueprintLoop = func(ctx context.Context, p protocol.LoopAnswerParams) (any, error) {
		if d.loopRunner != nil && d.loopRunner.Draining() {
			// T4 (LC19): the answer door is the start family for the drain —
			// answering re-dispatches the runner on a running loop, so an
			// answer given into a dying daemon would re-arm a ladder the
			// drain is stopping. Nothing is written.
			return nil, protocol.ErrLoopControlRefused.WithWhat(
				"answer refused: the daemon is shutting down and will not accept new loop work — restart it, then answer again")
		}
		// One question identity reaches the daemon through one verb; the
		// waiting STATE decides which door answers it (T12's walk wired
		// this routing — the action-ask door existed in the loop package
		// with no socket route, and an ask nobody can answer is a stranding
		// bug wearing a governance costume). A blueprint gate keeps
		// AnswerGate; a S16 permission ask keeps AnswerActionAsk — each
		// validates its own closed set, and neither can be answered by the
		// other's door.
		st, err := loop.Pinned(ctx, d.store, p.ID)
		if err != nil {
			return nil, err
		}
		// S24 T4: the queue's arm, tried BEFORE the ladder's routing. Its
		// discriminator is the question itself (gate kind, no step, on a
		// queue-tracked loop) — never the loop's status, because a queue
		// question rides a FINISHED loop while the ladder's doors answer
		// waiting ones. Not-the-queue's returns untouched.
		if handled, qres, qerr := d.answerQueueGate(ctx, p, st.ComponentRoot, st.Slug); handled {
			return qres, qerr
		}
		// S60 T7 (MQ9): the merge queue's conflict-answer arm, tried right
		// after the S24 queue's own — same discipline, same discriminator
		// (a step-less gate question standing ON an open item's landing
		// loop WHILE the item reads conflicted). The landing loop's own
		// gate questions carry a step id and stay the ladder's door.
		if handled, mres, merr := d.answerMergeQueueGate(ctx, p, st.ComponentRoot, st.Slug); handled {
			return mres, merr
		}
		var q store.Question
		// S55 LC6: the escalation arm, tried after the queue arm and before
		// the ladder's routing. Its discriminator is the question itself —
		// kind gate, options exactly [continue abort], no PendingAction, on a
		// waiting loop (AnswerEscalation re-checks all of it; this only routes,
		// it does not re-decide). AnswerGate routes through the blueprint step
		// at the position, which an escalation is NOT; AnswerActionAsk serves
		// only PendingAction asks; neither can answer an escalation, so it must
		// be tried before them. Not-an-escalation leaves the ladder's doors
		// untouched.
		var escalationHandled bool
		if st.Status == "waiting" && st.PendingAction == nil && st.QuestionID != 0 {
			if q0, qerr := d.store.ReadQuestion(st.QuestionID); qerr == nil &&
				string(q0.Kind) == "gate" && !q0.Answered() && len(q0.Options) == 2 &&
				q0.Options[0] == "continue" && q0.Options[1] == "abort" {
				q, err = loop.AnswerEscalation(ctx, dep, p.ID, p.Answer)
				escalationHandled = true
			}
		}
		if !escalationHandled {
			if st.Status == "waiting" && st.PendingAction != nil {
				q, err = loop.AnswerActionAsk(ctx, dep, p.ID, p.Answer)
			} else {
				// The gate step is captured BEFORE the answer advances the
				// position (S38 T14): the join needs the step that DECIDES, and
				// after a durable answer the position has moved on.
				action := loop.DecideGateForAnswer(st)
				q, err = loop.AnswerGate(ctx, dep, p.ID, p.Answer)
				if err == nil && action.Step != nil && action.Step.Decides != "" {
					// The verdict lands AFTER the answer is durable (claimed-FROM
					// makes a crash between the two harmless — the replay finds
					// the row already judged and changes nothing). The decider is
					// the answering person: the daemon serves exactly one user's
					// socket (P26's peer-credential trust), so the OS user behind
					// the endpoint IS the person whose verdict a review records
					// (OL8: a review is a person's act, and the store refuses
					// any other kind BEFORE SQL).
					if derr := loop.DecideFromGate(dep, st, *action.Step, p.Answer, socketActor()); derr != nil {
						return nil, derr
					}
				}
			}
		}
		if err != nil {
			return nil, err
		}
		saved, err := loop.Pinned(ctx, d.store, p.ID)
		if err != nil {
			return nil, err
		}
		if saved.Status == "running" {
			// T4 (LC19): started on the daemon-lifetime loopCtx, so the
			// shutdown Drain reaches this goroutine too (not a context.Background()
			// the drain could never cancel).
			d.loopRunner.Start(d.loopCtx, p.ID)
		}
		// The decision is filed in the database with the rest of the loop's answers.
		fileGateAnswers(d.store, p.ID, p.Answer, p.Feedback, d.log)
		return map[string]any{"id": p.ID, "questionId": q.ID, "answer": q.Answer, "status": saved.Status}, nil
	}
	d.Table.RetryHandlerBlock = func(ctx context.Context, p protocol.LoopRetryHandlerBlockParams) (any, error) {
		if d.loopRunner != nil && d.loopRunner.Active(p.ID) {
			// The runner already holds this loop: a retry beside a live
			// ladder would be a second dispatcher, which is exactly what
			// the receipt guard exists to prevent. Refuse, name, change
			// nothing.
			return map[string]any{"refused": map[string]string{"kind": "loop-running", "detail": "this loop is already running — nothing was dispatched twice"}}, nil
		}
		res, err := loop.RetryMissingHandlerBlock(ctx, dep, p.ID)
		var r *loop.RetryRefusal
		if errors.As(err, &r) {
			// The refusal travels to the app as DATA — the daemon's own
			// words, rendered verbatim, never a toast the UI invents.
			return map[string]any{"refused": map[string]string{"kind": r.Kind, "detail": r.Detail}}, nil
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"retried": true, "status": res.Status, "position": res.Position}, nil
	}
	// loop.recover: any blocked loop goes back to the step that blocked and runs it again. The same guards as the retry above:
	// a loop the runner already holds is refused, and a refusal travels as data.
	d.Table.RecoverBlockedStep = func(ctx context.Context, p protocol.LoopRecoverParams) (any, error) {
		if d.loopRunner != nil && d.loopRunner.Active(p.ID) {
			return map[string]any{"refused": map[string]string{"kind": "loop-running", "detail": "this loop is already running — nothing was dispatched twice"}}, nil
		}
		return d.recoverBlocked(ctx, dep, p.ID)
	}
	// loop.close (S39 CR1): the store's claim is the arbiter — idle→done or
	// a named refusal. The refusal travels as DATA (the app renders the
	// daemon's sentence, never its own guess), and loop.finished rides only
	// the success: closing a resting loop ENDS it, which is exactly what
	// that event means; resting (loop.run.finished) said it earlier.
	d.Table.CloseLoop = func(ctx context.Context, p protocol.LoopCloseParams) (any, error) {
		if err := d.store.CloseIdleLoop(p.ID); err != nil {
			if errors.Is(err, store.ErrLoopNotIdle) {
				return map[string]any{"refused": map[string]string{"kind": "not-idle", "detail": "only an idle loop can be closed; this one is " + d.loopStatus(p.ID)}}, nil
			}
			return nil, err
		}
		d.publishLoop("loop.finished", map[string]any{"loopId": p.ID, "status": "done"})
		return map[string]any{"id": p.ID, "status": "done"}, nil
	}
	// loop.archive (S33 T9): the bench's Delete door — the honest one. The
	// store assembles the archived state (its rule, not the caller's), the
	// loop's documents ride into cold storage with it, and NOTHING touches
	// the telemetry tiers: the counted work was counted (D8). The daemon
	// resolves the loop from its id exactly the way loop.close does —
	// ReadPinnedLoop, the same door loopStatus reads.
	d.Table.ArchiveLoop = func(ctx context.Context, p protocol.LoopArchiveParams) (any, error) {
		l, err := d.store.ReadLoopByID(p.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return map[string]any{"refused": map[string]string{"kind": "no-such-loop", "detail": "no loop " + p.ID + " is in this store — `aeon loop list` names the loops that exist"}}, nil
			}
			return nil, err
		}
		a, err := d.store.ArchiveLoop(store.Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot}, p.Note)
		if err != nil {
			return nil, err
		}
		// A durable view, not a protocol-notified kind (the step.compacted
		// precedent): the fixture guard over Kinds() stays untouched and the
		// archive still speaks where loop history is read.
		d.publishLoop("loop.archived", map[string]any{"loopId": p.ID, "archivedId": a.ID})
		return a, nil
	}
	// S39 T6 (CR3): a message to a resting loop starts one conversation
	// turn — an ordinary step by the loop's conversation agent, through the
	// very same step door a blueprint step takes. See conversation.go.
	d.Table.MessageLoop = d.messageLoop
	// S69 AT5: the ONE send-time expansion, daemon-wired — pinned
	// references become base64 content blocks at the compile boundary and
	// nowhere else; a revision the store lost speaks through the step's
	// notes (ruling 3).
	d.Table.AttachmentExpand = d.ExpandAttachments
	d.wireRunDoors() // S39 T9: the human run doors
	// S48 T7: the read service (blueprint_read.go) took list and show too —
	// names and path:"" stay byte-compatibly, the summaries join them.
	d.wireBlueprintReads()
	d.Table.BlueprintFields = func() any { return map[string]any{"fields": blueprint.Fields()} }
	d.wireBlueprintForm()
	// S48 T11 (BP12): a THIN call to BlueprintValidateAnswer
	// (blueprint_doors.go) — the socket door and the CLI's offline door
	// answer from one implementation over their own source (the daemon's
	// instance here, a store-backed source offline). Same report, same
	// options, no second validator.
	d.Table.ValidateBlueprint = func(p protocol.BlueprintValidateParams) (any, error) {
		return BlueprintValidateAnswer(d.bpSrc, p)
	}
	// S48 T6: the save door is REWRITTEN onto rows — the file writer is
	// gone. The seam keeps its field name (the registration and the bench
	// keep working); its implementation is the daemon's ONE write service
	// (blueprint_write.go), the only daemon file that calls
	// store.BlueprintPutTx/BlueprintDeleteTx. The actor arrives from the
	// protocol door, stamped from the connection — never a param (BP12).
	// The three new write doors (duplicate/delete/revert) register over
	// the same service, conditionally (V13).
	blueprintW := newBlueprintWrites(d.store)
	d.Table.SaveBlueprint = blueprintW.Save
	d.Table.BlueprintWrites = blueprintW
	d.Table.BlueprintOverrides = &blueprintOverrides{store: d.store, routes: func() []string {
		if d.Table.Routes == nil {
			return nil
		}
		return d.Table.Routes.RouteIDs()
	}}
	d.Table.RegisterBlueprintManage()
	// S24 T7: the queue's seams join the same wiring point — the socket
	// handlers and the CLI share one internal/queue underneath (CG9).
	d.initQueue()
	// S26 T5: the roadmap's seams join beside it — same rule, one
	// internal/roadmap under the socket and the CLI (CG9's extension,
	// brief §5's D5), and the dispatch doors (T3/T4) the seams ride.
	d.initRoadmap()
	// S29 T7: the triage's seams join beside it — same rule, one
	// internal/triage under the socket and the CLI (CG9's extension,
	// P34), and the dispatch doors (T3/T6/T4) the seams ride.
	d.initTriage()
	// S28 T4: the learnings' seam joins the same wiring point — one
	// internal/learnings under the socket and the CLI (P34's ordering: the
	// capability rides the socket first, the CLI and the bench are
	// secondary clients), and the scope validation (§3) and the relayed
	// write-protection (M1) live in that one place.
	d.initLearnings()
}

// loopStatus is the refusal's reader: what status IS this loop, said from
// the pin (the same read the ladder renders). Unknown when even that fails
// — a refusal sentence must never be withheld because naming failed.
func (d *Daemon) publishLoop(kind string, payload any) {
	// S59 T9 (EV15): the loop's terminal moment is a container's terminal
	// moment. It rides the reactor point rather than a call inside the loop
	// package because the loop package must not learn the word `container`: the
	// event is the fact, and this is the half that acts on it. `blocked` is
	// deliberately NOT torn down here — that container is being kept for a
	// person who may come back, and the idle TTL decides when "may" expires.
	d.onEnvironmentTerminal(kind, payload)

	// A payload that carries its own project root (protocol.ComponentRooted)
	// is a self-describing struct: it is appended under that root as-is, with
	// no map enrichment — there is no loopId to re-read the pin for, and a
	// snapshot/row next to it would be a second story beside the payload's
	// own ids (S31 D1; the step-event precedent in table.OnEvent). The map
	// path below is untouched: every existing loop event still rides it
	// byte-identically.
	if rooted, ok := payload.(protocol.ComponentRooted); ok {
		d.publish(kind, rooted.EventComponentRoot(), payload)
		return
	}
	fields, ok := payload.(map[string]any)
	if !ok || d.store == nil {
		d.publish(kind, "", payload)
		return
	}
	copyFields := make(map[string]any, len(fields)+2)
	for key, value := range fields {
		copyFields[key] = value
	}
	root := ""
	if id, ok := fields["loopId"].(string); ok && id != "" {
		if pin, err := d.store.ReadPinnedLoop(id); err == nil {
			if state, err := loop.FromPinned(pin); err == nil {
				copyFields["snapshot"] = d.viewLoop(state, false)
			}
			if row, err := NewMemory(d.store).loopView(pin.Loop); err == nil {
				copyFields["row"] = row
			}
			// S31 D1: a loop's events belong to the loop's project, so they
			// are appended with the loop row's REAL root and a subscriber
			// filtered by `Filter{ComponentRoot}` can select (and replay) one
			// project's ladder. When the row cannot be read there is nothing
			// to assert about the root — "" (daemon-wide) is the honest
			// absence, not a guess at a root.
			root = pin.Loop.ComponentRoot
			// S24 T3: the queue's reactor subscribes HERE — at the
			// daemon's own emission point, which already resolved the
			// loop identity — rather than through a general pub/sub
			// layer this slice does not need (the existing consumers
			// all read inline at emission; the reactor joins them).
			// Terminal kinds only, and the handler never blocks the
			// emit path: the work is a goroutine (V18 — advancing a
			// queue must never delay the step that just finished).
			if kind == protocol.KindLoopFinished || kind == protocol.KindLoopBlocked {
				d.onQueueTerminal(kind, pin.Loop.ComponentRoot, pin.Loop.Slug, copyFields)
				// S26 T3/T4: the roadmap's reactor joins at the same
				// emission point, on the same terminal kinds — the
				// ideation engine's produces check has already run by
				// the time `done` fires, and a materialized loop's
				// finish is the evidence `done` means (R3's call-site
				// assertion: through THIS event, never a new path).
				d.onRoadmapTerminal(kind, pin.Loop.ComponentRoot, pin.Loop.Slug, copyFields)
				// S29 T3: the triage's reactor joins at the same emission
				// point, on the same terminal kinds — a validation
				// attempt's loop finishing is the evidence the bug moves
				// out of unvalidated, and an untracked loop is not this
				// reactor's business (the S24 T3 precedent carried).
				d.onTriageTerminal(kind, pin.Loop.ComponentRoot, pin.Loop.Slug, copyFields)
				// S57 T7 (VG3): the gate reactor joins the SAME emission
				// point, after the three pinned reactors (the T1 golden's
				// byte-green requires this order and the untouched three):
				// a terminal loop whose reason is a gate failure files its
				// deduped bug. The reactor answers the BlockedReason struct
				// only — a string reason is not a gate failure.
				d.onGateTerminal(kind, pin.Loop.ComponentRoot, pin.Loop.Slug, copyFields)
				// S60 T7 (MQ2): the merge queue joins the same emission
				// point, last, on the same terminal kinds — a landing or
				// resolution loop finishing (or blocking: the conflict
				// signal rides loop.blocked) is only a WAKE here; the
				// reactor re-reads every state from the pin, so the event
				// earns immediacy and nothing else. An untracked loop is
				// not this reactor's business (the S24 T3 precedent).
				d.onMergeQueueTerminal(kind, pin.Loop.ComponentRoot, pin.Loop.Slug, copyFields)
				// S63 T2 (RT2): the outcome recorder joins the same emission
				// point, last, on the same terminal kinds — the fact row is
				// written from a re-read of the pin and the loop's own rows,
				// so the event earns immediacy and nothing else (the merge
				// queue's shape, verbatim). One goroutine, no delay to the
				// emit path (V18), and never an input to any decision: facts
				// are read by people and decide nothing themselves (RT3).
				d.onRetroTerminal(kind, pin.Loop.ComponentRoot, pin.Loop.Slug, copyFields)
			}
		}
	}
	d.publish(kind, root, copyFields)
}

// resolveDispatchAgent is T10's dispatch seam. A blueprint may name the
// CLOSED resolver `by-tech-tag`; the resolver maps the loop's own tech (the
// operator's --tech at start, extended by blueprint `with:` edges) to a
// registered handler. Non-resolver names pass through untouched — the
// resolver is consulted ONLY for its own registered name, so a typo'd
// resolver is not this function's business (the registry's unknown-handler
// refusal is, and stays honest about what is missing).
// dispatchSkill is the skill branch of the Model seam, split out so the
// daemon's own tests can walk it without a store, a socket, or an agentsvc —
// the nil case is AS11's deletability made checkable: a daemon built without
// the hook says WHICH step, WHICH skill, and WHAT to do about it.

package loop

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// The exhaustion paths — S17 T4. S8 made `then:` REQUIRED on any capped
// edge because its absence was "an infinite loop waiting to happen"; this
// file makes all three answers real:
//
//	escalate   raises to a human — the loop WAITS; unattended it COLLAPSES
//	           to block (L29, same rule as G7's ask → deny: escalating to a
//	           human nobody can reach is a loop that stops without saying why)
//	fail-item  fails THIS item and continues to the next — one task that
//	           cannot converge must not stop the other four; a failed
//	           item's dependents are blocked, NOT failed (O5: they never
//	           ran, and marking them failed reports work as attempted that
//	           was not)
//	block      stops the loop
//
// The block's reason must DISTINGUISH two facts that call for opposite
// responses (O1): the cap was reached (raise it, or accept the task
// cannot converge) and nobody could be asked (run it attended). Both
// clauses, always, when both apply; only the relevant one when only one
// does — a conflated sentence sends someone to change the wrong thing.

// exhaustionError signals the AdvanceLoop callers that the loop blocked on
// exhaustion. The reason is FINAL — callers keep it verbatim rather than
// prefixing the rejection text, because O1's sentence is an exact one and
// a prefix would bury the clauses a reader is looking for.
type exhaustionError struct{ reason string }

func (e *exhaustionError) Error() string { return e.reason }

// escalationWait signals a committed escalation question (attended): the
// loop is waiting, and an answered question resumes it like any gate.
type escalationWait struct{}

func (e *escalationWait) Error() string { return "escalated: awaiting a human" }

// budgetClause renders "T2 retries 2/2 exhausted at 'verify'" — the cap
// fact with its four identifiers: task, counter, value/cap, step.
func budgetClause(taskID, counter string, value, max int, stepID string) string {
	who := taskID
	if who == "" {
		who = "lane"
	}
	if max <= 0 {
		return fmt.Sprintf("%s %s exhausted at '%s'", who, counter, stepID)
	}
	return fmt.Sprintf("%s %s %d/%d exhausted at '%s'", who, counter, value, max, stepID)
}

// ceilingSentence is D3/Q11's answer: the loop as a whole is not
// converging, which is a different problem from one task struggling, and
// a human answering a question would not fix it — a BLOCK, never an
// escalation.
func ceilingSentence(value int) string {
	return fmt.Sprintf("loop ceiling: total_failed_gates %d/%d — the loop as a whole is not converging; this is a block, not an escalation (a human answering a question would not fix it)", value, LoopCeiling)
}

// handleExhaustion runs when DecideExhaustion says the budget is spent.
// It owns the store writes (task statuses, the escalation question) and
// the st mutations (status, position, last reason) so the callers only
// translate the returned signal into out.Status.
func handleExhaustion(ctx context.Context, dep Deps, id string, st *LoopState, pos Position, lane *blueprint.Lane, edge *blueprint.OnReject, attempt int) error {
	sc := store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}
	// The ceiling checked first: whichever task-side answer the then:
	// would give, the loop-wide verdict outranks it (Q11).
	if reached, value, err := NewCounters(dep.Store).LoopCeilingReached(sc); err != nil {
		return err
	} else if reached {
		return blockExhaustion(ctx, dep, st, lane.ID, ceilingSentence(value))
	}
	stepID := pos.Step
	var then blueprint.ThenAction
	if edge != nil {
		then = edge.Then
	}
	value := attempt
	max := 0
	counter := ""
	if edge != nil {
		max = edge.Max
		counter = edge.Counter
	}
	if max > 0 && value > max {
		// The row is clamped at the cap (T2), and so is the sentence:
		// "3/2" would claim a budget that was never granted.
		value = max
	}
	clause := budgetClause(pos.Item, counter, value, max, stepID)
	switch then {
	case blueprint.FailItem:
		// Fail THIS item; the lane's other items are unaffected.
		if err := failItem(ctx, dep, sc, st, pos, lane); err != nil {
			return err
		}
		return nil
	case blueprint.Escalate:
		attended := false
		if att, err := dep.Store.LoopAttended(ctx, id); err != nil {
			return err
		} else if att != nil {
			attended = *att
		}
		if attended {
			// The escalation is a QUESTION — the same suspend-a-gate
			// machinery, so resume is the same cold path as any answer.
			st.Status = "waiting"
			prompt := clause + " — then: escalate: raise the cap, or accept the task cannot converge"
			raw, err := st.RuntimeJSON()
			if err != nil {
				return err
			}
			q, err := dep.Store.CommitGate(id, sc, prompt, []string{"continue", "abort"}, raw)
			if err != nil {
				return err
			}
			// T7 walk finding, T4 defect: CommitGate injects the question
			// id into the runtime JSON IT saved, but the caller saves the
			// in-memory state afterwards — without this assignment the
			// save overwrites the pointer and an escalation question,
			// once asked, is unanswerable (AnswerGate demands a non-zero
			// QuestionID). The walk found it; the pointer is held here.
			st.QuestionID = q.ID
			st.LastReason = prompt
			emit(dep, "loop.waiting", map[string]any{"loopId": id, "questionId": q.ID, "prompt": q.Body, "answers": q.Options, "lane": lane.ID})
			return &escalationWait{}
		}
		// L29: unattended, escalate COLLAPSES to block — with BOTH
		// clauses, always, when both apply.
		return blockExhaustion(ctx, dep, st, lane.ID, clause+" / then: escalate — collapsed to block (loop is unattended)")
	case blueprint.Block:
		// No collapse clause: nothing escalated, so nothing collapsed.
		// The cap clause alone is the whole truth here (O1).
		return blockExhaustion(ctx, dep, st, lane.ID, clause+" / then: block")
	default:
		// S8 required `then:` at validation, so this is the unreachable
		// shape — and the honest block names that, rather than silently
		// picking a behaviour nobody specified.
		return blockExhaustion(ctx, dep, st, lane.ID, clause+" / then: '"+string(then)+"' has no action — the blueprint validator should have refused it")
	}
}

// settleTask moves a task to a terminal verdict THROUGH the machine. The
// ladder having run does not put the row at `in-progress` — the engine
// starts ladders without touching the row (FINDING T4: the orchestrator
// marks it; nothing in the loop engine does). The machine therefore
// refuses pending→failed outright, and it is right to: a task that never
// started was not failed. A fail-item DID run its ladder — so the walk
// visits the rungs the machine itself lists as legal from where the row
// stands, each step a legal transition, the whole walk saying what
// actually happened. A refusal with no legal walk is an error naming the
// states, never a silent skipped write.
func settleTask(ctx context.Context, dep Deps, sc store.Scope, taskID string, to store.TaskStatus) error {
	res, err := dep.Store.SetTaskStatus(sc, taskID, to)
	if err != nil {
		return err
	}
	if res.OK {
		return nil
	}
	loop, err := dep.Store.GetLoop(sc)
	if err != nil {
		return err
	}
	mode := store.LoopMode(loop.Mode)
	for _, mid := range res.Transition.Allowed {
		if mid == to || !store.CanTransition(mid, to, mode) {
			continue
		}
		probe, err := dep.Store.SetTaskStatus(sc, taskID, mid)
		if err != nil {
			return err
		}
		if !probe.OK {
			continue
		}
		next, err := dep.Store.SetTaskStatus(sc, taskID, to)
		if err != nil {
			return err
		}
		if next.OK {
			return nil
		}
	}
	return fmt.Errorf("loop: task %s cannot honestly reach %s from %s — the machine refuses the step and every rung that could lead there", taskID, to, res.Transition.From)
}

func blockExhaustion(ctx context.Context, dep Deps, st *LoopState, lane, reason string) error {
	st.Status = "blocked"
	// T5: a lane that blocks carries an open bracket, and an open bracket
	// on a dead lane is the lie bracketing exists to prevent. failed is
	// the terminal answer; closePhase keeps anything its own path already
	// closed (and iterating lanes, which record no single row, are a
	// no-op here — their verdict lives on the task rows).
	if err := closePhase(ctx, dep, st, lane, "failed"); err != nil {
		return err
	}
	st.LastReason = reason
	return &exhaustionError{reason: reason}
}

// failItem marks the item failed, marks its (transitive, never-ran)
// dependents BLOCKED, and moves the iteration to the next item — the
// ladder starts at its top, exactly as a fresh item would.
func failItem(ctx context.Context, dep Deps, sc store.Scope, st *LoopState, pos Position, lane *blueprint.Lane) error {
	if pos.Item == "" {
		// A non-iterating lane has no item to fail: the whole lane is the
		// unit, so stopping the loop honestly is the only left answer.
		return blockExhaustion(ctx, dep, st, lane.ID, "fail-item refused: the reject is not on an iterating lane — there is no item to fail, only the lane")
	}
	if err := settleTask(ctx, dep, sc, pos.Item, store.StatusFailed); err != nil {
		return err
	}
	// Dependents that never ran are blocked, not failed (O5, C9). The
	// walk is transitive: T3 behind T2 behind a failed T1 is blocked too.
	tasks, err := dep.Store.ListTasks(sc)
	if err != nil {
		return err
	}
	frontier := []string{pos.Item}
	seen := map[string]bool{pos.Item: true}
	for len(frontier) > 0 {
		current := frontier[0]
		frontier = frontier[1:]
		for _, t := range tasks {
			if seen[t.TaskID] {
				continue
			}
			depended := false
			for _, d := range t.DependsOn {
				if d == current {
					depended = true
					break
				}
			}
			if !depended {
				continue
			}
			seen[t.TaskID] = true
			// Only a task that never ran gets blocked; anything already
			// settled (complete, failed, skipped) is left exactly as it is.
			if t.Status == string(store.StatusPending) {
				if err := settleTask(ctx, dep, sc, t.TaskID, store.StatusBlocked); err != nil {
					return err
				}
			}
			frontier = append(frontier, t.TaskID)
		}
	}
	// Continue to the next item — the lane's other tasks are unaffected.
	next := ""
	for i, id := range st.Tasks {
		if id == pos.Item && i+1 < len(st.Tasks) {
			next = st.Tasks[i+1]
			break
		}
	}
	if next != "" {
		first := ""
		if len(lane.Ladder) > 0 {
			first = lane.Ladder[0].ID
		}
		st.Position = Position{Lane: pos.Lane, Item: next, Step: first}
		return nil
	}
	// The failed item was the lane's last: the lane is done with, and the
	// engine exits it exactly as a finished ladder does — the next lane,
	// or the loop's end.
	laneIndex := -1
	for i := range st.Blueprint.Lanes {
		if st.Blueprint.Lanes[i].ID == pos.Lane {
			laneIndex = i
			break
		}
	}
	if laneIndex+1 < len(st.Blueprint.Lanes) {
		st.Position = Position{Lane: st.Blueprint.Lanes[laneIndex+1].ID}
		return nil
	}
	// S39 CR1: fail-item settles the LAST lane, which is where a walk ends —
	// so this is a terminal write too, and endStatus is the one writer. On a
	// resting blueprint the loop rests and the run row says failed; the flag
	// is set ONLY when resting so a non-resting loop's saved JSON is
	// unchanged (D1 — today's fail-item bytes must not move).
	st.Status = endStatus(st.Blueprint, false)
	if st.Status == StatusIdle {
		st.Flags["run-failed"] = true // CR1: a run that fails rests the loop; the run row says failed
	}
	st.LastReason = budgetClause(pos.Item, "", 0, 0, pos.Step) + " / then: fail-item — the lane's items are all settled"
	return nil
}

// AnswerEscalation is the door for an exhaustion escalation question —
// the twin of AnswerActionAsk for the question the CommitGate inside
// handleExhaustion commits. AnswerGate cannot serve it: that door routes
// through the BLUEPRINT step at the current position (an escalation is
// not a step), and AnswerActionAsk serves only PendingAction asks. The
// T7 walk found the orphan — an escalation question that could be asked
// and never answered was the escalation-shaped door missing its handle
// (F9: the daemon verb that fronts this is the socket's next wiring,
// recorded for the slice that owns it).
//
//	continue — re-dispatch the SAME position. The cap still says
//	         exhausted, so the question re-fires: a person who cannot
//	         decide is asked forever, which is correct — the decision
//	         is theirs, not the counter's.
//	abort    — the person accepts the task cannot converge: the loop
//	         takes the abort path (on_abort handler lane, then the
//	         ABORTED terminal — not failed, not done).
func AnswerEscalation(ctx context.Context, dep Deps, id, answer string) (store.Question, error) {
	if answer != "continue" && answer != "abort" {
		return store.Question{}, fmt.Errorf("escalation answer %q is not one of [continue abort] — junk refuses, never executes", answer)
	}
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return store.Question{}, err
	}
	if st.Status != "waiting" || st.QuestionID == 0 || st.PendingAction != nil {
		return store.Question{}, fmt.Errorf("loop %s is not waiting at an escalation question", id)
	}
	q, err := dep.Store.ReadQuestion(st.QuestionID)
	if err != nil {
		return store.Question{}, err
	}
	if string(q.Kind) != "gate" || q.Answered() || len(q.Options) != 2 || q.Options[0] != "continue" || q.Options[1] != "abort" {
		return store.Question{}, fmt.Errorf("question %d is not an escalation question (options %v) — this door serves only the exhaustion ask", q.ID, q.Options)
	}
	st.QuestionID = 0
	if answer == "continue" {
		st.Status = "running"
	} else {
		st.Flags["aborted"] = true
		if st.Blueprint == nil || st.Blueprint.OnAbort == "" {
			st.Status = "blocked"
			st.LastReason = "aborted by user; blueprint has no terminal handler"
		} else {
			st.Position = Position{Lane: st.Blueprint.OnAbort}
			st.Status = "running"
		}
	}
	raw, err := st.RuntimeJSON()
	if err != nil {
		return store.Question{}, err
	}
	question, err := dep.Store.AnswerGate(id, q.ID, answer, st.Status, raw)
	if err != nil {
		return store.Question{}, err
	}
	emit(dep, "loop.advanced", map[string]any{"loopId": id, "lane": st.Position.Lane, "questionId": q.ID, "answer": answer, "outcome": answer})
	if st.Status == "blocked" {
		emit(dep, "loop.blocked", map[string]any{"loopId": id, "reason": BlockedReason{Kind: "escalation-answer", Detail: st.LastReason}})
	}
	return question, nil
}

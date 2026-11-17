package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cjlapao/aeon/internal/action"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/budget"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// ModelResult is what the model runner reports for one ladder step.
//
// `Kind` is the discriminator (DB19 — the absence of a question is a fact,
// not a nil to guess at): `completed` (carry `Outcome`), `suspended` (a real
// question — carry `QuestionID`; the loop waits answerably), and S31's
// `stopped` (a PERSON interrupted the step — no question to answer, the loop
// parks `waiting` with reason `stopped`; only `step.continue` on that step
// moves it again — D6). Anything else is treated as completed; the vocabulary
// grows only here, honestly, never by overloading `Outcome`.
type ModelResult struct {
	StepID     int64
	Kind       string
	Outcome    string
	Reason     string
	QuestionID int64
	Cost       json.RawMessage
	// PendingRoute is a mid-step route switch the runtime owed to the next
	// step (S18 T8, O5/N9): the loop applies it HERE — before the next
	// dispatch reads st.Route — and says so as an event.
	PendingRoute string
	// CapHit (S55 T7) rides with a `capped` result: the step's in-step
	// budget hit (E2). The loop reconstructs its own CapHitState from it —
	// the runtime carries the raw numbers, the loop owns the durable
	// record — and parks blocked/cap-hit with the step still suspended.
	CapHit *CapHitState
}

// ModelRunner runs one ladder step. (S68, B2/B3:) the fourth argument is the
// operator's own WORDS — the kickoff/goal verbatim, the user message ALONE
// (never decorated with lane facts or goal prefixes); the fifth is the FRAME
// the loop speaks around them (lane, blueprint facts, artifact contract),
// which rides the system message and is never shown as the person's words.
// Pre-S68 both arrived as one decorated string.
type ModelRunner func(context.Context, LoopState, blueprint.Step, string, string) (ModelResult, error)
type RecoveredStep func(context.Context, LoopState) (ModelResult, error)
type Deps struct {
	Store         *store.Store
	Model         ModelRunner
	RecoveredStep RecoveredStep
	Event         func(string, any)
	// Actions hands the S16 execution instruments (T10). A path that
	// does not wire them blocks HONESTLY — action-runtime-missing —
	// rather than panicking or pretending; the registry exists, the
	// instruments are what this field carries. It is given the loop's
	// ROOT: a forge identity (owner/repo) belongs to the repository the
	// loop runs in, not to the daemon process — two loops in two
	// repositories must not share one resolved identity (T12's walk
	// found the daemon-global version was the wrong shape).
	Actions func(root string) *action.Services
	// Records is the S38 T12 contract seam (contract.go): nil means the
	// emits: contract CANNOT be checked, and a step that declares emits
	// then blocks with contract-unverifiable — an unchecked contract is
	// never a passed one (P31). A loop that declares no emits runs
	// byte-identically without it.
	Records RecordsReader
	// Autonomy is the S56 T5 condition seam set (AL4, R1, R2): the decision
	// site's cross-slice conditions (requireGates = S57's merge rule, require-
	// budget = S55's budget, maxPerDay = the use log). A nil seam is cannot-say,
	// never met — a merge grant is inert until S57 wires Gates. The daemon passes
	// it at Deps construction; a zero value (no seams) makes every grant inert,
	// which is fail-toward-deny, not a silent allow.
	Autonomy AutonomySeams
	// Budget is the S55 T7 pre-dispatch check (E1): the daemon resolves
	// the loop's applicable budgets against current spend and evaluates
	// them, returning the verdict BEFORE a fresh step is dispatched.
	// nil means unenforced — the loop runs exactly as it did before T7,
	// the way Records nil means contract-unchecked. An ERROR from the
	// seam is NOT an unenforced run: E1 fails toward stop
	// (budget-unreadable), because a cap the engine could not read must
	// not be a cap the loop spends past.
	Budget func(context.Context, LoopState) (budget.Verdict, error)
	// BudgetEvent records the hit that caused a cap-hit block: one
	// kind-'hit' budget_event per hit (LC11). nil is a no-op, so a test
	// or an unwired daemon can still park the loop on the cap without an
	// audit door.
	BudgetEvent func(hit budget.Hit, loopID string) error
	// Merged records a LANDING merge as a merge_event (S57 T6, VG7) — the
	// ledger's fact that a merge happened under the gate's verdict. It is
	// called only AFTER the forge's irreversible call succeeded, so a
	// seam error is a WARNING the loop survives, never a failed action:
	// the merge already happened, and re-driving must not un-merge. nil
	// means unwired — a tree without the seam merges exactly as before.
	Merged func(ctx context.Context, rec MergeRecord) error
}

// MergeRecord is what the Merged seam receives: the identities that locate
// the merge (loop + root + PR + the SHA the forge reported) and the gate's
// evidence map as-is, so the daemon can pull the rule revision, the run id,
// the head SHA and the method out of it without a second read.
type MergeRecord struct {
	LoopSlug      string
	ComponentRoot string
	PR            int
	MergeSHA      string
	Ev            map[string]string
}
type Result struct {
	Kind       ActionKind      `json:"kind"`
	LoopID     string          `json:"loopId"`
	Status     string          `json:"status"`
	Position   Position        `json:"position"`
	QuestionID int64           `json:"questionId,omitempty"`
	Reason     *BlockedReason  `json:"reason,omitempty"`
	Step       *blueprint.Step `json:"-"`
}

func emit(dep Deps, kind string, payload any) {
	if dep.Event != nil {
		dep.Event(kind, payload)
	}
}
func stepKey(pos Position) string {
	key := pos.Lane
	if pos.Item != "" {
		key += "/" + pos.Item
	}
	if pos.Step != "" {
		key += "/" + pos.Step
	}
	return key
}

// AdvanceLoop reads one durable snapshot, decides once, executes at most one
// action, commits its next position, and returns. It holds no in-memory state.
func AdvanceLoop(ctx context.Context, dep Deps, id string) (Result, error) {
	if dep.Store == nil {
		return Result{}, fmt.Errorf("loop store is unavailable")
	}
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return Result{}, err
	}
	seedTasksFromStore(ctx, dep, &st)
	action := Decide(st.Blueprint, st)
	// Decide resolves implicit first item/step identities. Persist that resolved
	// position with the action, so a ladder gate or suspension resumes exactly
	// where it stopped rather than at an ambiguous lane-only cursor.
	if action.Position.Lane != "" {
		st.Position = action.Position
	}
	out := Result{Kind: action.Kind, LoopID: id, Status: st.Status, Position: st.Position}
	switch action.Kind {
	case ActIdle:
		// Nothing to do; the loop is resting (S39 CR1). No status write,
		// no event, no save — an idle loop advances by NOT advancing.
	case ActDone:
		// S17 T7: v1's loop_finish took done OR aborted for a reason —
		// a plan someone read and declined is a working system, and
		// recording it as done (or failed) makes the metrics lie about
		// the method. The on_abort handler lane having run, the terminal
		// says ABORTED. S39 CR1 widens the sentence, not the history:
		// endStatus is now the ONE writer of a walk's terminal (aborted
		// stays terminal; after: converse rests as idle).
		st.Status = endStatus(st.Blueprint, st.Flags["aborted"])
		st.Position = Position{}
		if err := save(dep, st); err != nil {
			return Result{}, err
		}
		if err := emitIfOver(dep, st, id); err != nil {
			return Result{}, err
		}
	case ActWaiting:
		// An action ask answered OUT OF BAND (the chat thread answered
		// the row past AnswerActionAsk's door) must not strand the loop:
		// the answered path at the same position is the arbiter, so the
		// wait flips to running and the decision runs again. Bounded:
		// one flip per answer — the answered path clears the pending.
		if st.PendingAction != nil && st.PendingAction.QuestionID != 0 {
			if q, err := dep.Store.ReadQuestion(st.PendingAction.QuestionID); err == nil && q.Answered() {
				st.Status = "running"
				if err := save(dep, st); err != nil {
					return Result{}, err
				}
				return AdvanceLoop(ctx, dep, id)
			}
		}
		out.QuestionID = st.QuestionID
	case ActBlocked:
		st.Status = "blocked"
		if action.Blocked != nil {
			st.LastReason = action.Blocked.Detail
			out.Reason = action.Blocked
		}
		// A lane blocked at DECISION time (bad condition, contradictory
		// state) may already hold an open bracket — failed is terminal,
		// and an open bracket left standing is the lie this file exists
		// to prevent.
		if err := closePhase(ctx, dep, &st, action.Position.Lane, "failed"); err != nil {
			return Result{}, err
		}
		if err := save(dep, st); err != nil {
			return Result{}, err
		}
		emit(dep, "loop.blocked", map[string]any{"loopId": id, "reason": action.Blocked})
	case ActRunCodeAction:
		// S8's refusal is RETIRED (D16): registered actions execute under
		// T2's governance — class decides, evidence records.
		if err := openPhase(ctx, dep, &st, action.Position.Lane); err != nil {
			return Result{}, err
		}
		if err := executeCodeAction(ctx, dep, &st, action, &out); err != nil {
			return Result{}, err
		}
	case ActAskGate:
		if action.Question == nil {
			return Result{}, fmt.Errorf("gate has no question")
		}
		// A gate is a step of its lane: the lane's bracket opens before
		// the question is committed, not after it is answered — the
		// tracker says what the loop is DOING, and waiting on a person
		// is doing.
		if err := openPhase(ctx, dep, &st, action.Position.Lane); err != nil {
			return Result{}, err
		}
		st.Status = "waiting"
		runtimeJSON, err := st.RuntimeJSON()
		if err != nil {
			return Result{}, err
		}
		q, err := dep.Store.CommitGate(id, store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}, action.Question.Prompt, action.Question.Answers, runtimeJSON)
		if err != nil {
			return Result{}, err
		}
		out.Status = "waiting"
		out.QuestionID = q.ID
		// lane/step name WHICH gate is waiting — a chat showing "Approve?" with
		// no position cannot say what is being approved.
		emit(dep, "loop.waiting", map[string]any{"loopId": id, "questionId": q.ID, "prompt": q.Body, "answers": q.Options,
			"lane": action.Position.Lane, "step": action.Position.Step})
	case ActSkip:
		st.StepStates[stepKey(action.Position)] = StepState{Status: action.SkipAs}
		if action.Position.Step == "" {
			st.LaneStates[action.Position.Lane] = action.SkipAs
			// S17 T5 (O8): the skip decide already distinguished —
			// not-needed (when false) versus skipped (mode) — reaches the
			// tracker here, as decide produced it, never re-derived.
			if err := recordSkip(ctx, dep, &st, action.Position.Lane, action.SkipAs); err != nil {
				return Result{}, err
			}
		}
		if err := moveNext(&st, action.Position); err != nil {
			return Result{}, err
		}
		if err := closeTrackedExit(ctx, dep, &st, action.Position); err != nil {
			return Result{}, err
		}
		if err := save(dep, st); err != nil {
			return Result{}, err
		}
		emit(dep, "loop.skipped", map[string]any{"loopId": id, "lane": action.Position.Lane, "as": action.SkipAs})
		if err := emitIfOver(dep, st, id); err != nil {
			return Result{}, err
		}
	case ActRunModelStep:
		if dep.Model == nil && !st.StepInFlight {
			return Result{}, fmt.Errorf("loop model runner is unavailable")
		}
		// S17 T5: v1 bracketed every phase BEFORE dispatch — the ritual
		// S13 deleted from the 26 bodies, performed here by the engine
		// (openPhase is a no-op for untracked and iterating lanes).
		if err := openPhase(ctx, dep, &st, action.Position.Lane); err != nil {
			return Result{}, err
		}
		words := ModelWords(st)
		// S68 B3: the person's words, verbatim, ride the user message alone.
		// S17 T3: the pending redo rides THIS dispatch's input, once — and
		// it is a PERSON'S sentence, so it rides with the words, not with
		// the frame (consumed here so a later dispatch of the same step
		// carries only what sent IT back; the step row keeps the full text,
		// step show --input reads it, so consuming is safe against a crash).
		if redo, ok := takeRedo(&st, action.Position); ok {
			words += "\n\n" + redo.InputSentence()
		}
		frame := TaskFrame(st, *action.Step)
		var result ModelResult
		var err error
		if st.StepInFlight {
			if dep.RecoveredStep == nil {
				return Result{}, fmt.Errorf("loop %s has an in-flight step but no recovery reader", id)
			}
			result, err = dep.RecoveredStep(ctx, st)
		} else {
			// S55 T7 (E1): the pre-dispatch budget check. It sits ONLY here,
			// the fresh-dispatch path, and never before a StepInFlight
			// re-run: the in-flight step already owns a running row, and a
			// second cap hit on its way back is E2's business, decided at
			// the step's own boundary inside the runtime (OD6). A seam
			// ERROR blocks too — a cap the engine could not read must not
			// be a cap the loop spends past (fail toward stop).
			if dep.Budget != nil {
				verdict, berr := dep.Budget(ctx, st)
				if berr != nil {
					st.Status = "blocked"
					st.LastReason = "budget-unreadable: " + berr.Error()
					out.Reason = &BlockedReason{Kind: "budget-unreadable", Detail: berr.Error()}
					if cerr := closePhase(ctx, dep, &st, action.Position.Lane, "failed"); cerr != nil {
						return Result{}, cerr
					}
					if saveErr := save(dep, st); saveErr != nil {
						return Result{}, saveErr
					}
					emit(dep, "loop.blocked", map[string]any{"loopId": id, "reason": out.Reason})
					break
				}
				if !verdict.OK {
					hit := verdict.Hits[0]
					st.Status = "blocked"
					st.LastReason = "cap-hit"
					st.CapHit = &CapHitState{BudgetID: hit.BudgetID, Scope: hit.Scope, Subject: hit.SubjectID,
						Dimension: hit.Dimension, Window: hit.Window, SpendFloor: hit.SpendFloor, Cap: hit.Cap, Reason: hit.Reason,
						Exact: hit.Exact}
					out.Reason = &BlockedReason{Kind: "cap-hit", Detail: hit.Sentence()}
					if cerr := closePhase(ctx, dep, &st, action.Position.Lane, "failed"); cerr != nil {
						return Result{}, cerr
					}
					if saveErr := save(dep, st); saveErr != nil {
						return Result{}, saveErr
					}
					if dep.BudgetEvent != nil {
						if eerr := dep.BudgetEvent(hit, id); eerr != nil {
							return Result{}, eerr
						}
					}
					emit(dep, "loop.blocked", map[string]any{"loopId": id, "reason": out.Reason})
					break
				}
			}
			result, err = dep.Model(ctx, st, *action.Step, words, frame)
		}
		if err != nil {
			st.Status = "blocked"
			st.LastReason = err.Error()
			out.Reason = &BlockedReason{Kind: "model-error", Detail: err.Error()}
			if cerr := closePhase(ctx, dep, &st, action.Position.Lane, "failed"); cerr != nil {
				return Result{}, cerr
			}
			if saveErr := save(dep, st); saveErr != nil {
				return Result{}, saveErr
			}
			emit(dep, "loop.blocked", map[string]any{"loopId": id, "reason": out.Reason})
			break
		}
		// S55 T4 (LC19): the daemon's drain cancelled this run while the step
		// was in flight. The step's commit path saw the same cause and left
		// the row RUNNING at its last checkpoint, so the pin must stay where
		// MarkStepStarted left it — running, StepInFlight, LastStepID — and
		// the recovery walk resumes the step cold on the next start, exactly
		// as it does after a crash. Applying the result here (any branch of
		// the switch below) would move the pin past the in-flight step and
		// block the loop on a failure that is really an exit, and the
		// recovery walk would find nothing to resume. The check is on the
		// CAUSE, never on ctx.Err() alone: a person's stop (ErrStopRequested)
		// still hands the "stopped" result down to its branch below, which
		// parks waiting/stopped and clears StepInFlight — T3's order stands.
		if context.Cause(ctx) == ErrDaemonShutdown && result.Kind != "stopped" {
			return Result{}, ctx.Err()
		}
		// S18 T8 (completed at T9's walk): every hand-back of a step is the
		// boundary the deferral's sentence promised — "it will take effect at
		// the next step". Suspension IS exit (P2), so a switch deferred during
		// the run that suspended must land NOW, or it is lost: the steer is
		// already delivered and never drains again, and an unapplied switch
		// makes the refusal's promise a lie. The mid-step protection lives in
		// the runtime's drain (the refusal itself); the loop engine applies
		// whatever the step reports when the step reports it.
		applyPendingRoute(dep, id, &st, result)
		outcome := result.Outcome
		reason := result.Reason
		st.StepInFlight = false
		if result.Kind == "suspended" {
			st.Status = "waiting"
			st.QuestionID = result.QuestionID
			st.LastStepID = result.StepID
			st.StepStates[stepKey(action.Position)] = StepState{Status: "waiting", StepID: result.StepID}
			if err := save(dep, st); err != nil {
				return Result{}, err
			}
			out.Status = "waiting"
			out.QuestionID = result.QuestionID
			emit(dep, "loop.waiting", map[string]any{"loopId": id, "questionId": result.QuestionID})
			break
		}
		if result.Kind == "stopped" {
			// S31 D6 — a PERSON stopped this step. The loop PARKS: waiting,
			// reason `stopped`, carrying the step id so `step.show`/`step.steers`
			// and the UI can name what is stopped. `failed` would say the model
			// broke; `blocked` would say the ladder cannot move; both are the
			// wrong accusation for "the person pressed stop". Nothing here
			// moves the position — only the step's own completion (through
			// CompleteSuspendedStep, from `step.continue`) does.
			st.Status = "waiting"
			st.LastStepID = result.StepID
			st.LastReason = "stopped"
			st.StepStates[stepKey(action.Position)] = StepState{Status: "waiting", StepID: result.StepID}
			if err := save(dep, st); err != nil {
				return Result{}, err
			}
			out.Status = "waiting"
			emit(dep, "loop.waiting", map[string]any{"loopId": id, "reason": "stopped", "stepId": result.StepID})
			break
		}
		if result.Kind == "capped" {
			// S55 T7 (E2) — the STEP's own in-step check hit a cap while it
			// was running. Unlike `stopped` (a person's interruption, park
			// and wait for step.continue), a cap hit is a refusal the loop
			// CANNOT step around: it PARKS BLOCKED on cap-hit with the step
			// still suspended (its row stays suspended with
			// pause_reason='cap', no question row — an unattended cap hit
			// has no one to ask), carrying the hit so `loop.raise` knows
			// which budget to raise. OD6: this is NOT OutcomeBlocked — the
			// outcome vocabulary is the model's, the cap is the engine's.
			if result.CapHit == nil {
				return Result{}, fmt.Errorf("loop %s reported a capped step with no hit", id)
			}
			st.Status = "blocked"
			st.LastReason = "cap-hit"
			st.LastStepID = result.StepID
			st.CapHit = result.CapHit
			st.StepStates[stepKey(action.Position)] = StepState{Status: "waiting", StepID: result.StepID}
			out.Reason = &BlockedReason{Kind: "cap-hit", Detail: result.CapHit.Reason}
			if cerr := closePhase(ctx, dep, &st, action.Position.Lane, "failed"); cerr != nil {
				return Result{}, cerr
			}
			if saveErr := save(dep, st); saveErr != nil {
				return Result{}, saveErr
			}
			if dep.BudgetEvent != nil {
				if eerr := dep.BudgetEvent(budget.Hit{BudgetID: result.CapHit.BudgetID, Scope: result.CapHit.Scope, SubjectID: result.CapHit.Subject,
					Dimension: result.CapHit.Dimension, Window: result.CapHit.Window, SpendFloor: result.CapHit.SpendFloor, Cap: result.CapHit.Cap, Reason: result.CapHit.Reason,
					Exact: result.CapHit.Exact}, id); eerr != nil {
					return Result{}, eerr
				}
			}
			emit(dep, "loop.blocked", map[string]any{"loopId": id, "reason": out.Reason})
			break
		}
		// The self-check: whatever the step said, did it file what its lane declares? A step that was cut off, or that
		// reported blocked/failed because it never got to its last call, has done the work and lost the deliverable;
		// failing the loop for that throws the work away. It gets ONE more go, told what is missing and what it
		// reported, before the ordinary routing judges it.
		if action.Step != nil && len(action.Step.Produces) > 0 && dep.Model != nil && (outcome == "ok" || outcome == "blocked" || outcome == "failed") {
			if missing := missingProduced(dep, st.Slug, st.ComponentRoot, st.Root, action.Step.Produces); missing != "" {
				emit(dep, "loop.step.recovering", map[string]any{"loopId": id, "lane": action.Position.Lane, "step": action.Position.Step, "missing": missing, "afterOutcome": outcome, "stepId": result.StepID})
				if again, rerr := dep.Model(ctx, st, *action.Step, words+recoveryNote(missing, outcome, reason), frame); rerr == nil &&
					again.Kind != "suspended" && again.Kind != "stopped" && again.Kind != "capped" {
					result, outcome, reason = again, again.Outcome, again.Reason
				}
			}
		}
		if outcome == "ok" {
			if missing := missingProduced(dep, st.Slug, st.ComponentRoot, st.Root, action.Step.Produces); missing != "" {
				outcome = "reject"
				reason = "declared artifact was not written: " + missing
			}
		}
		// S38 T12 (OL5/OL6): the record contract, beside the artifact one.
		// Unverifiable BLOCKS the loop (P31 — never silently ok); a missing
		// record rejects the step, and rejection routes exactly like a
		// missing artifact: rework via on_reject, or block.
		if outcome == "ok" && len(action.Step.Emits) > 0 && !contractVerifiable(dep, *action.Step) {
			st.Status = "blocked"
			st.LastReason = contractUnverifiableKind + ": " + contractUnverifiableDetail
			out.Reason = &BlockedReason{Kind: contractUnverifiableKind, Detail: contractUnverifiableDetail}
			if cerr := closePhase(ctx, dep, &st, action.Position.Lane, "failed"); cerr != nil {
				return Result{}, cerr
			}
			if saveErr := save(dep, st); saveErr != nil {
				return Result{}, saveErr
			}
			emit(dep, "loop.blocked", map[string]any{"loopId": id, "reason": out.Reason})
			break
		}
		if outcome == "ok" {
			if why := missingRecord(dep, *action.Step, result.StepID); why != "" {
				outcome = "reject"
				reason = contractRejection(why)
			}
		}
		// T6: the STORED outcome is the honest third value (`unverified`
		// when the gate's reason says the check did not run) while the
		// routing switch below still runs on `outcome` — work cannot
		// proceed on an unknown, so it routes as the reject it is.
		st.StepStates[stepKey(action.Position)] = StepState{Status: "done", Outcome: storedOutcome(outcome, reason), Reason: reason, StepID: result.StepID, Cost: result.Cost}
		st.LastOutcome = outcome
		st.LastReason = reasonOrNone(outcome, reason)
		st.LastStepID = result.StepID
		switch outcome {
		case "ok":
			// S17 T8 (O7, I6): a review that passes WITH non-blocking
			// findings sends them to the durable backlog — ok-with-nothing
			// appends nothing. A failed append BLOCKS: a deferred finding
			// that cannot be recorded is loud, not lost, and the whole
			// point of the backlog is that nothing silently evaporates.
			if action.Step != nil && action.Step.Sets == "in-review" && strings.TrimSpace(reason) != "" {
				be := BacklogEntry{PR: evidencePR(&st), Finding: reason,
					Lane: action.Position.Lane, Step: action.Position.Step}
				if err := AppendBacklog(st.Root, be); err != nil {
					st.Status = "blocked"
					st.LastReason = "backlog append failed — a finding that cannot be recorded is not deferred, it is lost (O7): " + err.Error()
					out.Reason = &BlockedReason{Kind: "backlog-append", Detail: st.LastReason}
					if cerr := closePhase(ctx, dep, &st, action.Position.Lane, "failed"); cerr != nil {
						return Result{}, cerr
					}
					break
				}
			}
			// S57 T6 (VG9): a review that completes `ok` is the ENGINE's
			// review token — "ok@<head sha>", the head the review actually
			// saw. It lands in THIS step's evidence, the ONLY source
			// forge.merge's aeon_review arg reads from (actionArgs), so a
			// model's handoff of the same name can never certify a review.
			// A head-read failure writes nothing: a review that cannot say
			// its head is not one that certified it, and the merge rule
			// simply refuses the absence.
			if action.Step != nil && action.Step.Sets == "in-review" {
				writeAeonReviewToken(ctx, dep, &st, action.Position)
			}
			if err := moveNext(&st, action.Position); err != nil {
				return Result{}, err
			}
			if err := closeTrackedExit(ctx, dep, &st, action.Position); err != nil {
				return Result{}, err
			}
		case "reject":
			if advisory(st.Blueprint, action.Position.Lane) {
				err = moveNext(&st, action.Position)
				st.LaneStates[action.Position.Lane] = "advisory"
				if err == nil {
					if cerr := closeTrackedExit(ctx, dep, &st, action.Position); cerr != nil {
						return Result{}, cerr
					}
				}
			} else {
				err = routeReject(ctx, dep, id, &st, *action.Step, action.Position)
			}
			if err != nil {
				var ex *exhaustionError
				var ew *escalationWait
				switch {
				case errors.As(err, &ex):
					// handleExhaustion already blocked st with the exact
					// O1 sentence; the rejection text must not be prefixed
					// over it — the clauses are what a reader searches for.
					out.Reason = &BlockedReason{Kind: "exhaustion", Detail: ex.reason}
				case errors.As(err, &ew):
					// Attended escalation committed its question; the loop
					// waits exactly as a gate would.
					out.Status = "waiting"
				default:
					st.Status = "blocked"
					st.LastReason = reason + "; " + err.Error()
					out.Reason = &BlockedReason{Kind: "unresolvable-reject", Detail: st.LastReason}
					if cerr := closePhase(ctx, dep, &st, action.Position.Lane, "failed"); cerr != nil {
						return Result{}, cerr
					}
				}
			}
		default:
			st.Status = "blocked"
			out.Reason = &BlockedReason{Kind: "step-" + outcome, Detail: reason}
			if cerr := closePhase(ctx, dep, &st, action.Position.Lane, "failed"); cerr != nil {
				return Result{}, cerr
			}
		}
		if err := save(dep, st); err != nil {
			return Result{}, err
		}
		emit(dep, "loop.advanced", map[string]any{"loopId": id, "lane": action.Position.Lane, "item": action.Position.Item, "step": action.Position.Step, "outcome": outcome, "cost": result.Cost})
		if err := emitIfOver(dep, st, id); err != nil {
			return Result{}, err
		}
		if st.Status == "blocked" {
			emit(dep, "loop.blocked", map[string]any{"loopId": id, "reason": out.Reason})
		}
	default:
		return Result{}, fmt.Errorf("unknown action kind %q", action.Kind)
	}
	out.Status = st.Status
	out.Position = st.Position
	return out, nil
}

func advisory(bp *blueprint.Blueprint, laneID string) bool {
	if bp == nil {
		return false
	}
	for _, lane := range bp.Lanes {
		if lane.ID == laneID {
			return lane.Advisory
		}
	}
	return false
}

func save(dep Deps, st LoopState) error {
	raw, err := st.RuntimeJSON()
	if err != nil {
		return err
	}
	return dep.Store.SavePinnedRuntime(st.ID, st.Status, raw)
}
func moveNext(st *LoopState, pos Position) error {
	next, err := NextPosition(st.Blueprint, *st, pos)
	if err != nil {
		return err
	}
	if next.Lane != pos.Lane && (st.LaneStates[pos.Lane] == "" || st.LaneStates[pos.Lane] == "pending") {
		st.LaneStates[pos.Lane] = "ok"
	}
	st.Position = next
	if next.Lane == "" {
		// The done|aborted pair (T7): the ladder ending past its last
		// lane says done ONLY when no person declined; after an abort
		// answered through on_abort, the terminal is aborted. S39 CR1:
		// endStatus is the one writer — after: converse rests as idle.
		st.Status = endStatus(st.Blueprint, st.Flags["aborted"])
	}
	return nil
}

// ContinueFallbackText is B9's one honest sentence: a step dispatched with
// nothing human to answer says so, and the chat window renders it as the
// system speaking — never as something the operator typed. The UI pins this
// exact string (shared fixture, S68 T4); changing it here must change it
// there.
const ContinueFallbackText = "Continue the loop per the task instructions in the system prompt."

// ModelWords is the USER message for a ladder step: the run's kickoff if it
// has one, else the goal (S39 T7's seam, the ONLY read — D1), verbatim.
// S68 B3: no `Goal:` prefix, no lane, no decoration. S68 B9: a step with
// genuinely nothing human to answer speaks the fallback rather than shipping
// an empty turn the provider would refuse (or worse, accept).
func ModelWords(st LoopState) string {
	if strings.TrimSpace(goalText(st)) == "" {
		return ContinueFallbackText
	}
	return goalText(st)
}

// TaskFrame is the SYSTEM-side frame around those words: what the loop knows
// about where this step sits, spoken as instructions. Pre-S68 all of this
// rode the user message and the chat window replayed blueprint facts as
// something the operator had said. Deterministic — same state, same bytes
// (P29's replay promise).
func TaskFrame(st LoopState, step blueprint.Step) string {
	var b strings.Builder
	b.WriteString("The person's own words in your message are the goal. Work the blueprint position named here.\n")
	b.WriteString("Current blueprint lane: ")
	b.WriteString(st.Position.Lane)
	if st.Position.Item != "" {
		b.WriteString("\nTask item: ")
		b.WriteString(st.Position.Item)
	}
	if len(st.Answers) > 0 {
		keys := make([]string, 0, len(st.Answers))
		for key, answer := range st.Answers {
			if answer != "" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			b.WriteString("\nGate answer ")
			b.WriteString(key)
			b.WriteString(": ")
			b.WriteString(st.Answers[key])
		}
	}
	if len(st.With) > 0 {
		keys := make([]string, 0, len(st.With))
		for key := range st.With {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range st.With {
			b.WriteString("\nRework context ")
			b.WriteString(key)
			b.WriteString(": ")
			b.WriteString(st.With[key])
		}
	}
	if len(step.Produces) > 0 {
		b.WriteString("\nFile the declared artifact(s) with `doc_write`, named exactly as declared: ")
		b.WriteString(strings.Join(step.Produces, ", "))
		b.WriteString(". They are stored in the database, not on disk. If you do not hold `doc_write`, write each with `write` in the workspace and it is filed for you. Verify each exists before reporting ok.")
	}
	// S38 T12: the step is told its record contract before it runs (OL5) —
	// the refusal then names a call the prompt already made.
	b.WriteString(contractPrompt(step.Emits))
	return b.String()
}

func missingArtifact(root string, paths blueprint.Paths) string {
	for _, rel := range paths {
		trimmed := strings.TrimSuffix(rel, "/")
		resolved := tools.Resolve(root, trimmed)
		if !resolved.OK {
			return rel
		}
		info, err := os.Stat(resolved.Abs)
		if err != nil {
			return rel
		}
		if strings.HasSuffix(rel, "/") && !info.IsDir() {
			return rel
		}
		if !strings.HasSuffix(rel, "/") && info.IsDir() {
			return rel
		}
		if !filepath.IsAbs(resolved.Abs) {
			return rel
		}
	}
	return ""
}

func routeReject(ctx context.Context, dep Deps, id string, st *LoopState, step blueprint.Step, pos Position) error {
	var lane *blueprint.Lane
	for i := range st.Blueprint.Lanes {
		if st.Blueprint.Lanes[i].ID == pos.Lane {
			lane = &st.Blueprint.Lanes[i]
			break
		}
	}
	if lane == nil {
		return fmt.Errorf("lane absent")
	}
	edge := step.OnReject
	if edge == nil {
		edge = lane.OnReject
	}
	target := ""
	if edge != nil {
		target = edge.Goto
	}
	if pos.Step != "" && lane.Rework != "" {
		target = lane.Rework
	}
	if target == "" {
		return fmt.Errorf("reject has no rework target; stopped at %s", pos.Lane)
	}
	if edge != nil && len(edge.With) > 0 {
		if st.With == nil {
			st.With = map[string]string{}
		}
		for key, value := range edge.With {
			for question, answer := range st.Answers {
				value = strings.ReplaceAll(value, "${question."+question+"}", answer)
				value = strings.ReplaceAll(value, "${answers."+question+"}", answer)
			}
			st.With[key] = value
		}
	}
	// S17 T6 (L33, O4, Q8): a gate that could not RUN routes back like a
	// fail — work cannot proceed on an unknown — but charges NO task
	// budget. A tool that will not run is not the handler's failure;
	// draining its retries would exhaust the budget on an environment
	// problem before any real attempt is made. The ONLY counter it
	// touches is the loop ceiling's own, which is also what bounds it:
	// repeated unverified ends blocked, never forever. The redo rides
	// with the distinct reason and NO attempt claim (nothing was granted
	// to count) — "sent back", not "retry 1 of 2".
	unverified := false
	if pos.Step != "" {
		unverified = IsUnverifiedReason(st.StepStates[stepKey(pos)].Reason)
	}
	if unverified {
		sc := store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}
		value, atCeiling, derr := NewCounters(dep.Store).DecideUnverified(sc)
		if derr != nil {
			return derr
		}
		if atCeiling {
			return blockExhaustion(ctx, dep, st, lane.ID, ceilingSentence(value))
		}
	}
	attempt, attemptMax := 0, 0
	if !unverified && edge != nil && edge.Counter != "" {
		// S17 T4 (D2): the budget that decides routing is the PERSISTED
		// per-task row — not the S8 per-loop map, which charged every task
		// from one ledger (the D2 failure at the engine). DecideExhaustion
		// bumps, checks the loop ceiling (D3), and returns the attempt the
		// redo carries; one decision point, one truth.
		var exhausted bool
		var derr error
		sc := store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}
		attempt, exhausted, derr = NewCounters(dep.Store).DecideExhaustion(ctx, st, sc, pos.Item, edge.Counter, edge.Max)
		if derr != nil {
			return derr
		}
		if exhausted {
			return handleExhaustion(ctx, dep, id, st, pos, lane, edge, attempt)
		}
		attemptMax = edge.Max
	}
	if pos.Step != "" && lane.Rework != "" {
		// S17 T3 (L32): the re-entered step arrives KNOWING why it was sent
		// back and which try this is. The reason is the rejected step's
		// recorded reason — the same value the view and step.show render,
		// never prose the engine invented. A reject with no reason cannot
		// rework: the bare retry is the forbidden shape, refused here,
		// blocking the loop with the absence named rather than dispatching
		// a call that would reproduce the same output. (The gate-answer
		// revise path is different in kind — a human instruction, whose
		// context already rides st.Answers into every prompt — and keeps
		// its existing behaviour.)
		redo, redoErr := NewRedo(pos.Step, st.StepStates[stepKey(pos)].Reason, attempt, attemptMax)
		if redoErr != nil {
			return fmt.Errorf("bare retry refused: %w", redoErr)
		}
		if st.Redos == nil {
			st.Redos = map[string]Redo{}
		}
		st.Redos[stepKey(Position{Lane: pos.Lane, Item: pos.Item, Step: target})] = redo
		st.Position = Position{Lane: pos.Lane, Item: pos.Item, Step: target}
	} else {
		st.Position = Position{Lane: target}
	}
	return nil
}

// applyPendingRoute honours a route switch the runtime deferred from a
// mid-step steer (S18 T8, O5/N9). The LOOP owns the route a dispatch reads;
// this is the one place that state moves after the pin, and it says so: an
// applied switch is an event, not a silent field write, because "which
// provider is this loop on now" is exactly the fact a person must be able to
// see change when they steered it.
func applyPendingRoute(dep Deps, id string, st *LoopState, result ModelResult) {
	if result.PendingRoute == "" || st.Route == result.PendingRoute {
		return
	}
	from := st.Route
	st.Route = result.PendingRoute
	emit(dep, "loop.route.applied", map[string]any{"loopId": id, "from": from, "to": result.PendingRoute,
		"stepId": result.StepID, "detail": "a steer asked for this switch mid-step; it takes effect at the next step"})
}

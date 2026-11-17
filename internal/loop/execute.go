package loop

// execute.go — where S8's refusal retired to (D16). The advance-time
// branch that answered blocked with the old not-yet-implemented kind is
// GONE; this is what runs in its place. That BlockedReason KIND stays in
// the structured type (S17 may need it; removing a kind from a protocol
// type is a protocol change) — but no code path returns it for a
// registered action, and TestNoRefusalPathRemains greps the sources to
// prove it (naming the retired kind's spelling here would re-litigate
// the grep against a comment — S16 T1/T3's lesson, learned twice).
//
// Governance comes before the effect (T2): the effect class decides.
// local runs; unattended outward/irreversible denies AT ONCE with T2's
// exact sentence as the blocked reason; attended outward/irreversible
// commits a `permission` question (Q5: S11's kind, reused) and waits —
// and ONLY protocol.ResolveActionAsk's "approve" releases the effect.
// Evidence — numbers, SHAs, refs, never prose — lands in state and the
// transcript (Q7).

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// PendingAction is the durable half of the ask: a crash between asking
// and answering re-asks the SAME question identity rather than opening a
// second permission window nobody answered the first one.
type PendingAction struct {
	Name       string            `json:"name"`
	Position   string            `json:"position"`
	QuestionID int64             `json:"questionId"`
	Args       map[string]string `json:"args,omitempty"`
}

func blockTo(st *LoopState, out *Result, dep Deps, kind, detail, name string) error {
	reason := &BlockedReason{Kind: kind, Detail: detail, Action: name}
	st.Status = "blocked"
	st.LastReason = detail
	// S60 T7: the kind survives in a machine-readable field beside the
	// prose (state.go's BlockKind comment says why: the queue reads kinds,
	// screens read sentences).
	st.BlockKind = kind
	out.Reason = reason
	out.Status = "blocked"
	if err := save(dep, *st); err != nil {
		return err
	}
	emit(dep, "loop.blocked", map[string]any{"loopId": st.ID, "reason": reason})
	return nil
}

func executeCodeAction(ctx context.Context, dep Deps, st *LoopState, act Action, out *Result) error {
	name := act.Step.Action
	if _, ok := action.Lookup(name); !ok {
		// Load-time validation refuses unknown actions now (D15);
		// reaching here means state older than the registry. Blocked,
		// naming it — never executed, never the retired refusal's kind.
		return blockTo(st, out, dep, "unknown-action",
			fmt.Sprintf("action %q is not in the closed registry (D14)", name), name)
	}
	if dep.Actions == nil {
		// Honest three-state: the registry exists, its instruments do not
		// on THIS path. A block that names what is missing is a finding
		// someone can close; a panic is not.
		return blockTo(st, out, dep, "action-runtime-missing",
			"action services (git/forge/intents) are not wired on this path — the registry exists; its instruments were not handed over", name)
	}
	attended := false
	if att, err := dep.Store.LoopAttended(ctx, st.ID); err == nil && att != nil {
		attended = *att
	}
	// S19 T6: the loop's ONE opt-in, read from its pinned row exactly the
	// way attendedness is — both were declared at start and neither is
	// re-derived afterwards. A read error fails toward deny (the zero
	// value), never toward run: the opt-in must be PRESENT to lift
	// anything.
	allowOutward, err := dep.Store.LoopAllowOutward(ctx, st.ID)
	if err != nil {
		return err
	}
	// S56 T5: the decision site consults the root's grants once, at the action
	// moment, through the live read (grantContextFor). The result enters the
	// verdict only through ResolveWithGrant, exactly as the pin entered the start
	// through startExtras (T4). An attended loop gets an empty context (OD2); a
	// read error is returned (runner-error), never a silent run (AL9).
	gctx, grantUse, err := grantContextFor(ctx, dep, st, name, attended)
	if err != nil {
		return err
	}
	verdict, err := action.ResolveWithGrant(name, attended, allowOutward, gctx)
	if err != nil {
		return blockTo(st, out, dep, "action-unclassified", err.Error(), name)
	}

	asked := st.PendingAction != nil && st.PendingAction.Name == name && st.PendingAction.Position == stepKey(act.Position)

	switch verdict.Verdict {
	case action.VerdictDeny:
		// Unattended outward/irreversible: blocked AT ONCE, and the
		// reason is T2's sentence VERBATIM — the loop does not paraphrase
		// its own refusal.
		return blockTo(st, out, dep, "action-denied", verdict.Sentence, name)

	case action.VerdictAsk:
		if !asked {
			return askPermission(ctx, dep, st, act, name, out)
		}
		q, err := dep.Store.ReadQuestion(st.PendingAction.QuestionID)
		if err != nil {
			return err
		}
		if !q.Answered() {
			// Still waiting — the position stays put, no second question.
			st.Status = "waiting"
			out.Status = "waiting"
			out.QuestionID = q.ID
			return save(dep, *st)
		}
		execute, err := protocol.ResolveActionAsk(q.Answer)
		if err != nil || !execute {
			// A junk answer IS a refusal — the reason it could not be
			// understood is the reason it will not be executed.
			reason := fmt.Sprintf("%s denied — the answer %q is not the word approve (junk refuses, never executes)", name, q.Answer)
			if err == nil {
				reason = fmt.Sprintf("%s denied by the person (answered %q)", name, q.Answer)
			}
			st.PendingAction = nil
			st.QuestionID = 0 // the ask is answered — its identity lives on in the question row
			return blockTo(st, out, dep, "action-denied-by-person", reason, name)
		}
		st.PendingAction = nil // released by the ONE verb that can
		st.QuestionID = 0      // …and the waiting display retires with it

	case action.VerdictRun:
		// local: no question, no receipt — the class already decided.
	}

	// The use log (AL10): a begun row before the effect, a completed/failed row
	// after — a crash leaves a begun with no outcome ("effect may have happened"),
	// never silently absent (the receipt-first shape). Only an effect taken UNDER
	// a grant is logged; a no-grant run (a local, or the legacy allowOutward) and
	// an attended ask write nothing. A failed begun BLOCKS the action — an
	// unauditable effect does not run.
	var begunID int64
	if grantUse != nil {
		var uerr error
		begunID, uerr = dep.Store.RecordAutonomyUse(st.ID, grantUse.GrantID, grantUse.Rev, grantUse.Action, "begun", 0, "")
		if uerr != nil {
			return blockTo(st, out, dep, "action-denied",
				fmt.Sprintf("%s — the use could not be recorded; nothing was done", name), name)
		}
	}
	args := actionArgs(st, act)
	// S60 T6 (MQ13): queue.enqueue declares attendedness from the loop's
	// PIN, read beside the row at dispatch, and the actor is the pinned
	// blueprint (MQ14.1) — both stamped by CODE at composition, so there
	// is nothing for a model or a step's params to choose. A failed read
	// degrades to unattended: "nobody said" is not attended.
	if name == action.QueueEnqueue {
		if att, err := dep.Store.LoopAttended(ctx, st.ID); err == nil && att != nil {
			args.Attended = *att
		}
		args.ActorKind = "blueprint"
		if st.Blueprint != nil {
			args.ActorID = st.Blueprint.ID
		}
	}
	// S60 MQ7 (write-ahead, PA5's order copied): the integration ledger ROW
	// exists before the action can build its directory. A crash between row
	// and `worktree add` is ReconcileWorktrees' rebuild case — the shape PA12
	// is built for — where a crash the other way (directory, no row) is the
	// orphan recovery cannot see.
	if name == action.GitIntegrate {
		if err := ensureIntegrationRow(ctx, dep, st, args); err != nil {
			return blockTo(st, out, dep, "integration-ledger", err.Error(), name)
		}
	}
	ev, err := action.Dispatch(ctx, dep.Actions(st.Root), name, args)
	if err != nil {
		if grantUse != nil {
			recordAutonomyUse(dep, st, grantUse, "failed", begunID)
		}
		// S60 MQ8: a conflict is not an action-failure. The typed
		// ConflictError blocks the position with its OWN kind — the queue's
		// driver turns `action-conflict` into the item's conflicted state
		// (and the merge stays in progress in the worktree for the
		// resolution loop); every other error keeps today's action-failed.
		// S60 T7 adds the two authority kinds: a DENIAL (grant gone, person
		// said no at the ask) and a forge REFUSAL (protected branch said
		// no) are queue endings — `denied` and `protected` — not retries,
		// and the reactor tells them apart from BlockKind, not from prose.
		var conflict *action.ConflictError
		if errors.As(err, &conflict) {
			return blockTo(st, out, dep, "action-conflict", err.Error(), name)
		}
		var denied *action.LandDeniedError
		if errors.As(err, &denied) {
			return blockTo(st, out, dep, "action-denied", err.Error(), name)
		}
		var fe *forge.ForgeError
		if errors.As(err, &fe) && fe.Kind == forge.KindRefused {
			return blockTo(st, out, dep, "action-refused", err.Error(), name)
		}
		return blockTo(st, out, dep, "action-failed", err.Error(), name)
	}
	// The effect succeeded: whatever block this position carried before is
	// no longer the loop's story (a resume that re-ran the action clears
	// the kind; BlockKind describes the CURRENT block or nothing).
	st.BlockKind = ""
	// Evidence (Q7): numbers/SHAs/refs into state, and the transcript
	// event carries the same map — the mirror stays a projection of the
	// row, never its own story.
	if st.Evidence == nil {
		st.Evidence = map[string]map[string]string{}
	}
	st.Evidence[stepKey(act.Position)] = ev
	// S60 T5: the integrate evidence MOVES the workspace (one writer,
	// applyEvidenceWorkDir — the grep-pinned single-assignment rule).
	applyEvidenceWorkDir(st, ev)
	st.StepStates[stepKey(act.Position)] = StepState{Status: "done"}
	if grantUse != nil {
		recordAutonomyUse(dep, st, grantUse, "completed", begunID)
	}
	// The payload gains grant/rev ONLY when a grant allowed it (additive) — absent
	// otherwise, so existing consumers and goldens read exactly today's shape.
	payload := map[string]any{"loopId": st.ID, "action": name,
		"lane": act.Position.Lane, "step": act.Position.Step, "evidence": ev}
	if grantUse != nil {
		payload["grant"] = grantUse.GrantID
		payload["rev"] = grantUse.Rev
	}
	emit(dep, "loop.action.executed", payload)

	// S57 T6 (VG7): verify.run's outcome is the ROUTE, not just the step's
	// state — pass moves, fail/unverified rework through on_reject, and a
	// red gate with no edge to rework to blocks. It returns the save, so
	// the generic moveNext below is skipped.
	if name == action.VerifyRun {
		return routeVerify(ctx, dep, st, act, out, ev)
	}

	// S57 T6: the merge event — the ledger's record that a merge happened
	// under the gate's verdict. A seam error NEVER fails a landed merge:
	// the forge already did the irreversible work, so the failure is a
	// warning the loop survives (T7's watch and recovery's reconcile
	// tolerate the missing record; T0 item 4 checks re-entry).
	if name == action.ForgeMerge && dep.Merged != nil {
		rec := MergeRecord{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot,
			PR: args.PRNumber, MergeSHA: ev["merge_sha"], Ev: ev}
		if merr := dep.Merged(ctx, rec); merr != nil {
			emit(dep, "loop.warning", map[string]any{"loopId": st.ID, "action": action.ForgeMerge,
				"warning": "the merge landed but its merge_event could not be recorded: " + merr.Error()})
		}
	}

	// The completed step MOVES — an executed action whose position never
	// advances is a spin, and Run re-invokes the effect forever. moveNext
	// is how every completing arm leaves a position; the executed arm
	// completes like the rest.
	if err := moveNext(st, act.Position); err != nil {
		return err
	}
	return save(dep, *st)
}

// routeVerify is VG7's decision for a finished verify.run:
//
//   - pass       ⇒ the step completes and moves;
//   - fail       ⇒ on_reject routes with the fixed reason "verify: <summary>";
//   - unverified ⇒ on_reject routes with the UnverifiedPrefix reason —
//     routeReject's guard sends it to DecideUnverified (the loop ceiling
//     only, never the step's counter);
//   - no edge     ⇒ blocked "verify-failed" with the summary.
//
// The reason is the fixed template over the run's own summary — the loop
// never paraphrases gate output into a routing decision.
func routeVerify(ctx context.Context, dep Deps, st *LoopState, act Action, out *Result, ev map[string]string) error {
	key := stepKey(act.Position)
	state := ev["verify_state"]
	switch state {
	case "pass":
		if err := moveNext(st, act.Position); err != nil {
			return err
		}
		return save(dep, *st)
	case "fail", "unverified":
		prefix := "verify: "
		if state == "unverified" {
			prefix = UnverifiedPrefix
		}
		ss := st.StepStates[key]
		ss.Reason = prefix + ev["verify_summary"]
		st.StepStates[key] = ss
		if act.Step == nil || verifyReworkTarget(st, act) == "" {
			return blockTo(st, out, dep, "verify-failed", ev["verify_summary"], action.VerifyRun)
		}
		// The outcome is handled exactly as the other two routeReject
		// call sites handle it (advance.go's reject switch, runner.go's
		// gate-answer path): exhaustion means the loop is ALREADY blocked
		// with the exact clause, escalation is a wait, and anything else
		// is an unresolvable reject. This path returns to Run, so the
		// state is saved HERE — an unsaved block would surface as an
		// error, not as a parked loop.
		rerr := routeReject(ctx, dep, st.ID, st, *act.Step, act.Position)
		var ex *exhaustionError
		var ew *escalationWait
		switch {
		case rerr == nil:
			// Rework routed (or fail-item consumed the item): the
			// position and the redo already stand.
		case errors.As(rerr, &ex):
			// S57 T7 (VG3): the exhaustion's counter is the verify rework's —
			// Action names it so the gate reactor can tell a verify
			// exhaustion from the other routeReject sites' (which carry no
			// Action, and are not verify gate failures).
			out.Reason = &BlockedReason{Kind: "exhaustion", Detail: ex.reason, Action: action.VerifyRun}
		case errors.As(rerr, &ew):
			out.Status = "waiting"
		default:
			st.Status = "blocked"
			st.LastReason = ev["verify_summary"] + "; " + rerr.Error()
			out.Reason = &BlockedReason{Kind: "unresolvable-reject", Detail: st.LastReason}
			if cerr := closePhase(ctx, dep, st, act.Position.Lane, "failed"); cerr != nil {
				return cerr
			}
		}
		if err := save(dep, *st); err != nil {
			return err
		}
		if st.Status == "blocked" {
			emit(dep, "loop.blocked", map[string]any{"loopId": st.ID, "reason": out.Reason})
		}
		return nil
	}
	// The runner's closed set is pass|fail|unverified; a fourth value is
	// a runner bug, and it blocks named rather than defaulting to pass.
	return blockTo(st, out, dep, "verify-failed", "verify.run returned state "+state+" (the runner's closed set is pass|fail|unverified)", action.VerifyRun)
}

// writeAeonReviewToken is VG9's source half: the moment the review-rung
// step (sets: in-review) completes ok, the engine writes "ok@<head sha>"
// into that step's own evidence — the head the review actually saw. The
// consumer is actionArgs' aeon_review scan (evidence only). A failed head
// read writes NOTHING: a review that cannot say its head is not one that
// certified it, and the merge rule refuses the absence (the aeon-review-
// missing sentence names exactly this).
func writeAeonReviewToken(ctx context.Context, dep Deps, st *LoopState, pos Position) {
	if dep.Actions == nil {
		return
	}
	svc := dep.Actions(st.Root)
	if svc == nil || svc.Git == nil {
		return
	}
	head, err := svc.Git.LocalHEAD(ctx, st.Root)
	if err != nil || head == "" {
		return
	}
	if st.Evidence == nil {
		st.Evidence = map[string]map[string]string{}
	}
	key := stepKey(pos)
	ev := st.Evidence[key]
	if ev == nil {
		ev = map[string]string{}
		st.Evidence[key] = ev
	}
	ev["aeon_review"] = "ok@" + head
}

// verifyReworkTarget answers "does this position have anywhere a rejected
// verify run can go" — the exact target derivation routeReject applies, so
// the block check and the route can never disagree about edge existence.
func verifyReworkTarget(st *LoopState, act Action) string {
	var lane *blueprint.Lane
	for i := range st.Blueprint.Lanes {
		if st.Blueprint.Lanes[i].ID == act.Position.Lane {
			lane = &st.Blueprint.Lanes[i]
			break
		}
	}
	if lane == nil {
		return ""
	}
	var edge *blueprint.OnReject
	if act.Step != nil {
		edge = act.Step.OnReject
	}
	if edge == nil {
		edge = lane.OnReject
	}
	target := ""
	if edge != nil {
		target = edge.Goto
	}
	if act.Position.Step != "" && lane.Rework != "" {
		target = lane.Rework
	}
	return target
}

func askPermission(ctx context.Context, dep Deps, st *LoopState, act Action, name string, out *Result) error {
	// The ask names what it asks ABOUT — the numbers come from the
	// evidence rows the actions recorded (Q7), never from prose or
	// remembered guesses.
	tgt := action.Target{Repository: st.Root, Branch: st.Slug}
	// The ask names what it affects (O1), and every identifier comes from
	// CODE — the same code-owned args the dispatch would use, with
	// evidence rows consulted first where they exist (Q7). Nothing here
	// is remembered or re-derived: a model never contributes to an ask.
	a := actionArgs(st, act)
	if ev := st.Evidence[stepKey(act.Position)]; ev != nil {
		tgt.IssueNumber = atoiSafe(ev["issue_number"])
		tgt.PRNumber = atoiSafe(ev["pr_number"])
		tgt.BaseRef = ev["base_ref"]
		tgt.Head = ev["head_ref"]
	}
	if a.Task != "" {
		if names, err := loopNames(st, a.Task); err == nil {
			if tgt.Head == "" {
				tgt.Head = names.TaskBranch()
			}
			if tgt.Branch == "" {
				tgt.Branch = names.TaskBranch()
			}
			if tgt.BaseRef == "" {
				tgt.BaseRef = names.FeatureBranch()
			}
		}
	}
	if tgt.BaseRef == "" {
		tgt.BaseRef = a.Base
	}
	if tgt.Title == "" {
		tgt.Title = a.TaskTitle
	}
	if tgt.IssueNumber == 0 {
		tgt.IssueNumber = a.IssueNumber
	}
	if tgt.PRNumber == 0 {
		tgt.PRNumber = a.PRNumber
	}
	// S60 T6 (MQ12.6): a landing ask names the queue's facts — the item,
	// its position, the SHA the merge binds (evidence, never prose), the
	// gap the queue last reported, and the gate's own summary. A person
	// approving a land approves THESE numbers.
	tgt.Item = a.Item
	tgt.Position = a.Position
	tgt.VerifiedSHA = a.VerifiedSHA
	tgt.Behind = atoiSafe(st.With["behind"])
	tgt.Ahead = atoiSafe(st.With["ahead"])
	if tgt.Head == "" {
		tgt.Head = a.IntegrationBranch
	}
	if tgt.BaseRef == "" {
		tgt.BaseRef = a.BaseBranch
	}
	for _, ev := range st.Evidence {
		if tgt.GateSummary == "" {
			tgt.GateSummary = ev["verify_summary"]
		}
	}
	ask, err := action.BuildAsk(name, tgt)
	if err != nil {
		return blockTo(st, out, dep, "action-ask-unbuildable", err.Error(), name)
	}
	runtimeJSON, err := st.RuntimeJSON()
	if err != nil {
		return err
	}
	// The row carries the WHOLE rendering: the short prompt and the lines
	// naming what is affected (O1). A row holding only the prompt would
	// ask "Open a pull request?" without saying which — and a person
	// approving that is approving nothing.
	q, err := dep.Store.CommitGate(st.ID, store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}, ask.Prompt+"\n\n"+ask.Body, ask.Options, runtimeJSON)
	if err != nil {
		return err
	}
	st.PendingAction = &PendingAction{Name: name, Position: stepKey(act.Position), QuestionID: q.ID}
	st.QuestionID = q.ID // the waiting state carries its question identity, as the gate arm does
	st.Status = "waiting"
	out.Status = "waiting"
	out.QuestionID = q.ID
	emit(dep, "loop.waiting", map[string]any{"loopId": st.ID, "questionId": q.ID, "prompt": q.Body,
		"answers": q.Options, "lane": act.Position.Lane, "step": act.Position.Step, "action": name})
	return save(dep, *st)
}

// actionArgs builds the registry Args from state and the step's own
// declarations. Numbers come from evidence rows, never from prose
// (L3/Q7): the openPR body's summary is the ONE handoff-sourced value,
// and it is prose by declaration.
func actionArgs(st *LoopState, act Action) action.Args {
	a := action.Args{Dir: st.Root, Loop: st.Slug, Task: act.Position.Item}
	// S60 T5 (MQ7): the step-workspace seam — with WorkDir set (a landing
	// loop after its integrate), the action runs in the WORKTREE, not the
	// person's checkout. Empty is byte-identically today.
	if st.WorkDir != "" {
		a.Dir = st.WorkDir
	}
	// The queue's identifiers (MQ8): code-supplied from the loop's pinned
	// `with` (the driver composes them from the item row — ids, branches,
	// never prose). Root is the COMPONENT root even when Dir has moved into
	// a worktree — the integration worktrees hang off the root (MQ7).
	a.Root = st.ComponentRoot
	a.Item = st.With["item"]
	a.IntegrationBranch = st.With["integration_branch"]
	a.HeadBranch = st.With["head_branch"]
	if a.Task == "" {
		// The ladder materialises items at S17; a single-task loop
		// declares with["task"] at start (T12's walk bridge). Absent is
		// absent — the registry's required-arg guard refuses naming the
		// argument, which is the honest answer, never a guessed task.
		a.Task = st.With["task"]
	}
	if act.Step != nil && act.Step.Action != "" {
		// The step's `with:`/task carry titles; numbers come only from
		// recorded evidence — see the keys the registry guarantees.
		a.TaskTitle = st.With["task_title"]
		a.Message = st.With["commit_message"]
		a.PRSummary = st.With["pr_summary"]
		a.ReviewSumm = st.With["review_summary"]
	}
	// `which` is an authored constant: the blueprint's step params carry
	// it (T12's walk — a lane that creates the FEATURE branch and a lane
	// that creates the TASK branch are different lanes of the same loop,
	// so a loop-level with entry cannot serve both). The BranchWhich
	// enum validates at the action; junk refuses naming the value.
	if act.Step != nil {
		if v, ok := act.Step.Params["which"].(string); ok && v != "" {
			a.Which = action.BranchWhich(v)
		}
		// S57 T6: verify.run's narrowing params — the gate's id-or-name
		// and the gate's agent identity. Code-supplied (the validator
		// type-checked them as strings at load), never model prose.
		if v, ok := act.Step.Params["gate"].(string); ok && v != "" {
			a.Gate = v
		}
		if v, ok := act.Step.Params["agent"].(string); ok && v != "" {
			a.Agent = v
		}
	}
	if a.Which == "" {
		a.Which = action.BranchWhich(st.With["branch_which"])
	}
	// The base the task branch merges INTO is code's fact to know: the
	// feature branch of this loop (T3's names). Not a default — a
	// derivation, exactly the one the ask shows.
	if a.Task != "" && a.Base == "" {
		if names, err := loopNames(st, a.Task); err == nil {
			a.Base = names.FeatureBranch()
		}
	}
	// S60 MQ15: the pinned shape rides the Args so the registry builds the
	// SAME prefixed names (makeNames takes Prefix), the create action cuts
	// from the pinned base, and nothing re-derives a prefixed name with a
	// prefix-free door. Un-pinned loops: all three empty, every name
	// byte-identical to before S60 (the T1 goldens pin that).
	a.Prefix = branchPrefix(st)
	a.FeatureBranch = st.FeatureBranch
	a.BaseBranch = st.BaseBranch
	// Evidence-fed identifiers (Q7): the open issue/PR numbers recorded
	// by the actions that created them.
	if ev := st.Evidence[stepKey(act.Position)]; ev != nil {
		a.IssueNumber = atoiSafe(ev["issue_number"])
		a.PRNumber = atoiSafe(ev["pr_number"])
	}
	for _, ev := range st.Evidence {
		if a.IssueNumber == 0 {
			a.IssueNumber = atoiSafe(ev["issue_number"])
		}
		if a.PRNumber == 0 {
			a.PRNumber = atoiSafe(ev["pr_number"])
		}
		// S57 T6 (VG9): the engine's review token — the "ok@<head sha>"
		// the review step's ok wrote into ITS OWN evidence — is the only
		// aeon_review forge.merge ever sees. It comes from evidence only:
		// a handoff field of the same name (st.With) is a different value
		// and is never read here, so a model cannot self-certify its own
		// review.
		if a.AeonReview == "" {
			a.AeonReview = ev["aeon_review"]
		}
		// S60 T6 (MQ11): the two SHAs the landing binds are EVIDENCE keys
		// — the verification's head (verify_head_sha, written by
		// verify.run) and the integration's tip (integration_sha, written
		// by git.integrate). First non-empty wins, and no With/handoff
		// field can name either: the binding a model cannot move.
		if a.VerifiedSHA == "" {
			a.VerifiedSHA = ev["verify_head_sha"]
		}
		if a.IntegrationSHA == "" {
			a.IntegrationSHA = ev["integration_sha"]
		}
	}
	// S60 T6 (MQ8): the queue identifiers ride the loop's pinned `with` —
	// the driver composes them from the item row (code, ids, never prose).
	// The land method is the repository's setting the driver read (MQ3),
	// not the step's. (The enqueue's attendedness/actor are stamped at
	// dispatch — executeCodeAction, where the loop's PIN is read.)
	if p := atoiSafe(st.With["position"]); p > 0 {
		a.Position = p
	}
	if m := st.With["land_method"]; m != "" && a.Method == "" {
		a.Method = forge.MergeMethod(m)
	}
	return a
}

// loopNames builds this loop's branch names from its PINNED shape. The
// prefix is recovered from the pinned feature-branch name — MQ15's no-new-
// column choice: "aeon/feature/<slug>" says its prefix is "aeon/". A loop
// with no pin (everything started before S60) derives through the original
// no-prefix door, byte-identical to before this task.
func loopNames(st *LoopState, task string) (action.BranchNames, error) {
	if p := branchPrefix(st); p != "" {
		return action.NewBranchNamesWith(p, st.Slug, task)
	}
	return action.NewBranchNames(st.Slug, task)
}

// branchPrefix recovers the pinned prefix from the pinned name; empty when
// there is no pin or the name does not end in the canonical feature shape
// (a hand-edited row names nothing — the derivation falls back to today).
func branchPrefix(st *LoopState) string {
	suffix := "feature/" + st.Slug
	if st.FeatureBranch != "" && strings.HasSuffix(st.FeatureBranch, suffix) && st.FeatureBranch != suffix {
		return strings.TrimSuffix(st.FeatureBranch, suffix)
	}
	return ""
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// AnswerActionAsk is the door for a permission ask — the PendingAction
// twin of AnswerGate (which serves blueprint gates and refuses step
// questions). The closed answer set is enforced AT THE DOOR (approve or
// deny, T2's verbs verbatim); the answer is recorded and the loop flips
// back to running AT THE SAME POSITION, where executeCodeAction's
// answered path — the single arbiter — resolves it through
// protocol.ResolveActionAsk. An answer recorded out of band (a chat
// thread answered it) is still honoured by that path: it reads the row,
// not the door.
func AnswerActionAsk(ctx context.Context, dep Deps, id, answer string) (store.Question, error) {
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return store.Question{}, err
	}
	if st.Status != "waiting" || st.PendingAction == nil || st.PendingAction.QuestionID == 0 {
		return store.Question{}, fmt.Errorf("loop %s is not waiting at an action ask", id)
	}
	q, err := dep.Store.ReadQuestion(st.PendingAction.QuestionID)
	if err != nil {
		return store.Question{}, err
	}
	if q.Answered() {
		// The row was answered OUT OF BAND (the chat thread answered it —
		// the S9 design: the thread is a door too). The caller's text is
		// irrelevant then: the recorded answer is the record (P2), and
		// the door's job is to NOTICE and re-drive. The answered path at
		// the same position remains the only arbiter — junk stays junk.
		st.Status = "running"
		if err := save(dep, st); err != nil {
			return store.Question{}, err
		}
		return q, nil
	}
	if answer != "approve" && answer != "deny" {
		return store.Question{}, fmt.Errorf("answer %q is not one of [approve deny] — the closed set refuses at the door (T2)", answer)
	}
	updated, err := dep.Store.Answer(q.ID, answer)
	if err != nil {
		return store.Question{}, err
	}
	// Running again at the SAME position: the answered path resolves the
	// answer; the door itself never executes.
	st.Status = "running"
	if err := save(dep, st); err != nil {
		return store.Question{}, err
	}
	return updated.Question, nil
}

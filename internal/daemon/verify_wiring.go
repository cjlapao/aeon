// verify_wiring.go — the daemon's S57 T6 adapters: where the verify.run
// action, the merge rule's fact gathering, the autonomy Gates seam and
// the merge-event write all meet the store.
//
// The action package is store-free by construction (action/verify_actions.go
// says why), so this file adapts *store.Store to its seams — one thin
// adapter type per seam — and gives the merge rule's two consumers (the
// merge gate and the autonomy seam) the ONE shared fact gatherer, so the
// facts the grant's live check sees cannot drift from the facts the
// merge's gate sees (D-T6-1).
//
// The recorded author is the engine itself — kind "system" (verify_run's
// closed set admits user|cli|agent|system), id "loop-engine": a loop run
// is no person's act, no named agent's act (verifier-engineer is the
// identity the gate's COMMANDS are policy-checked under, not the run's
// author), and no trigger's act.

package daemon

import (
	"errors"
	"path/filepath"

	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/mergerule"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/verify"
)

// engineActor is the author the engine records under: the loop's verify
// runs and the merge it allowed.
var engineActor = store.Actor{Kind: "system", ID: "loop-engine"}

// --- verify.run: the engine's gate runner (the action's four seams) -----------

// storeVerifyComponent answers "which node does this root belong to"
// (VG6: a run names the node it happened on).
type storeVerifyComponent struct{ s *store.Store }

func (v storeVerifyComponent) ComponentForRoot(ctx context.Context, root string) (string, error) {
	c, err := v.s.ComponentForRoot(root)
	if errors.Is(err, store.ErrNotFound) {
		// S60 T7: the merge queue verifies INSIDE the integration
		// worktree — <component-root>/.worktrees/integrate-<item> — and
		// that path registers no component of its own. It is the
		// component's own worktree (the layout composes it from the
		// registered root), so the read answers with the enclosing
		// component. Anything else under .worktrees still refuses: this
		// walks UP exactly the composed queue layout, never the
		// filesystem.
		if base, sub, ok := strings.Cut(root, string(filepath.Separator)+".worktrees"+string(filepath.Separator)+"integrate-"); ok && base != "" && !strings.Contains(sub, string(filepath.Separator)) {
			if c, err := v.s.ComponentForRoot(base); err == nil {
				return c.ID, nil
			}
		}
	}
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

// storeVerifyGates answers "what runs here" (VG3's effective set).
type storeVerifyGates struct{ s *store.Store }

func (v storeVerifyGates) EffectiveGates(ctx context.Context, componentID string) ([]verify.GateSpec, error) {
	eg, err := v.s.EffectiveGates(componentID)
	if err != nil {
		return nil, err
	}
	out := make([]verify.GateSpec, 0, len(eg.Gates))
	for _, g := range eg.Gates {
		out = append(out, verify.GateSpec{
			ID: g.Command.ID, Name: g.Command.Name, Command: g.Command.Command,
			Requirement: g.Command.Requirement, TimeoutS: g.Command.TimeoutS,
			MemoryMB: g.Command.MemoryMB,
		})
	}
	return out, nil
}

// storeVerifyRuns is the ledger half: the attempt count and the
// one-transaction finish.
type storeVerifyRuns struct{ s *store.Store }

// CountLoopRuns counts the loop's finished runs at its root (D-T6-8: the
// attempt is count+1). The store's run filter has no trigger dimension, so
// a manual run of the same loop advances the counter too — the attempt is
// the run's slot in the loop's evidence, not a loop-trigger-only count.
func (v storeVerifyRuns) CountLoopRuns(ctx context.Context, loopSlug, componentRoot string) (int, error) {
	runs, err := v.s.ListVerifyRuns(store.VerifyRunFilter{ComponentRoot: componentRoot, LoopSlug: loopSlug, Limit: 1000000})
	if err != nil {
		return 0, err
	}
	return len(runs), nil
}

func (v storeVerifyRuns) Record(ctx context.Context, in action.RecordRun) (string, error) {
	d, err := v.s.CreateVerifyRun(store.Actor{Kind: in.Actor.Kind, ID: in.Actor.ID}, store.NewVerifyRun{
		LoopSlug: in.LoopSlug, ComponentRoot: in.ComponentRoot, ComponentID: in.ComponentID,
		Task: in.Task, Position: in.Position, Attempt: in.Attempt,
		HeadSHA: in.HeadSHA, Trigger: in.Trigger, StartedAt: in.StartedAt,
	})
	if err != nil {
		return "", err
	}
	for _, r := range in.Results {
		if err := d.AppendVerifyResult(store.VerifyRunResult{
			Name: r.Name, Requirement: r.Requirement, Command: r.Command,
			ExitCode: r.ExitCode, Signal: r.Signal, TimedOut: r.TimedOut,
			WallMS: r.WallMS, StdoutBytes: r.StdoutBytes, StderrBytes: r.StderrBytes,
			Truncated: r.Truncated, StdoutSHA256: r.StdoutSHA256, StderrSHA256: r.StderrSHA256,
			StdoutTail: r.StdoutTail, StderrTail: r.StderrTail, State: r.State, Reason: r.Reason,
		}); err != nil {
			return "", err
		}
	}
	run, err := v.s.FinishVerifyRun(d, in.State, in.Reason, in.Summary, in.Digest, in.EndedAt)
	if err != nil {
		return "", err
	}
	return run.ID, nil
}

// storeVerifyDoD materializes the loop's DoD from its latest brief.
type storeVerifyDoD struct{ s *store.Store }

func (v storeVerifyDoD) MaterializeDoD(ctx context.Context, loopSlug, componentRoot string) (int, error) {
	sc := store.Scope{LoopSlug: loopSlug, ComponentRoot: componentRoot}
	b, err := v.s.LatestBrief(sc)
	if err != nil {
		// No brief yet is not a failure: the loop has no DoD to
		// materialize, and the rule's dod evidence reads
		// "none-recorded" — the honest state.
		if strings.Contains(err.Error(), "has no brief yet") {
			return 0, nil
		}
		return 0, err
	}
	// MaterializeDoD is idempotent (D-T6-2): the rework rounds write
	// nothing.
	return v.s.MaterializeDoD(sc, store.DoDBrief{BriefID: b.ID, BriefVersion: b.Version, Items: b.Acceptance})
}

// verifyService is the loop's dispatch door to the shared constructor
// (buildVerifyActionService below).
func (d *Daemon) verifyService(gitC *git.Client) *action.VerifyService {
	return buildVerifyActionService(d.store, d.log, gitC)
}

// buildVerifyActionService stands up the verify.run action over the
// store's seams, with the gate runner under the step-side policy verdict
// — the same CommandVerdictFor the step's spawns read (one policy source,
// two call sites). The runner's deadline rides the caller's ctx: the loop
// engine dispatches each action under the step's deadline, and a cancelled
// step cancels the run (T0 item 5). The loop's dispatch and the manual
// verify.try (S57 T8) build it this way — ONE construction, so the two
// paths cannot drift apart on a seam.
//
// T0 item 6: the zero frozen manifest answers from the live store — a
// gate's command is vouched for by the policy set AS STORED, the same set
// the step's spawns are judged against.
func buildVerifyActionService(st *store.Store, log Logger, gitC *git.Client) *action.VerifyService {
	runner := verify.New()
	cv := CommandVerdictFor(st, log)
	runner.Verdict = func(agent, command string) (policy.Verdict, bool) {
		return cv(agent, runtime.FrozenManifest{}, command)
	}
	return &action.VerifyService{
		HeadSHA:   gitC.LocalHEAD,
		Component: storeVerifyComponent{s: st},
		Gates:     storeVerifyGates{s: st},
		Recorder:  storeVerifyRuns{s: st},
		DoD:       storeVerifyDoD{s: st},
		Runner:    &runner,
		Now:       time.Now,
		Actor:     action.RunActor{Kind: "system", ID: "loop-engine"},
	}
}

// --- the merge rule's facts (one gatherer, two consumers) ---------------------

// fillGateFacts fills the extended half of the gate's facts from the
// store: the latest run the rule measures (require_local's subject — the
// newest finished run of this (root, loop, head); honest absence when
// none) and the loop's DoD ledger (all briefs, all states).
func (d *Daemon) fillGateFacts(ctx context.Context, q action.GateFactsQuery, facts *mergerule.Facts) error {
	runs, err := d.store.ListVerifyRuns(store.VerifyRunFilter{
		ComponentRoot: q.ComponentRoot, LoopSlug: q.LoopSlug, HeadSHA: q.HeadSHA, Limit: 1,
	})
	if err != nil {
		return fmt.Errorf("gate facts: listing the run at %s: %w", q.HeadSHA, err)
	}
	if len(runs) > 0 {
		r := runs[0]
		facts.LatestRun = &mergerule.Run{ID: r.ID, HeadSHA: r.HeadSHA, State: r.State, Reason: r.Reason}
	}
	items, err := d.store.ListDoD(store.Scope{LoopSlug: q.LoopSlug, ComponentRoot: q.ComponentRoot})
	if err != nil {
		return fmt.Errorf("gate facts: listing the DoD: %w", err)
	}
	facts.DoD = make([]mergerule.Item, 0, len(items))
	for _, it := range items {
		facts.DoD = append(facts.DoD, mergerule.Item{
			ID: it.ID, State: it.State, EvidenceKind: it.EvidenceKind,
			EvidenceRef: it.EvidenceRef, Text: it.Text,
		})
	}
	return nil
}

// mergeGateAnswer is the autonomy seam's R2 answer (S57 T6, D-T6-1): the
// loop's effective merge rule, evaluated over the PR's LIVE facts at the
// decision site. Three states, never a silent met: allowed is met (with
// the rule's revision named), refused is unmet (the first refusal's
// sentence, verbatim), and any read that could not be made is cannot-say
// (AL4) — the grant's condition then blocks, not passes.
func (d *Daemon) mergeGateAnswer(ctx context.Context, loopID string, pr int) autonomy.Answer {
	cannotSay := func(detail string) autonomy.Answer {
		return autonomy.AnswerCannotSay("the merge rule (S57) could not be asked: " + detail)
	}
	lp, err := d.store.ReadLoopByID(loopID)
	if err != nil {
		return cannotSay("the loop " + loopID + " cannot be read (" + err.Error() + ")")
	}
	comp, err := d.store.ComponentForRoot(lp.ComponentRoot)
	if err != nil {
		return cannotSay(err.Error())
	}
	rule, _, err := d.store.EffectiveMergeRule(comp.ID)
	if err != nil {
		return cannotSay("reading the merge rule: " + err.Error())
	}
	f := d.forgeClient(lp.ComponentRoot)
	if f == nil {
		return cannotSay("the repository has no forge identity to read the PR's facts from")
	}
	prf, err := f.ReadPR(ctx, pr)
	if err != nil {
		return cannotSay("the PR " + strconv.Itoa(pr) + " cannot be read (" + err.Error() + ")")
	}
	facts, err := action.GatherGateFacts(ctx, f, pr, prf.HeadSHA)
	if err != nil {
		return cannotSay(err.Error())
	}
	if rule.Enabled && !rule.IsBaseline() {
		q := action.GateFactsQuery{PR: pr, LoopSlug: lp.Slug, ComponentRoot: lp.ComponentRoot, HeadSHA: prf.HeadSHA}
		if err := d.fillGateFacts(ctx, q, &facts); err != nil {
			return cannotSay(err.Error())
		}
	}
	verdict := mergerule.Evaluate(rule, facts)
	if verdict.Allowed {
		return autonomy.AnswerMet(fmt.Sprintf("the merge rule allows PR #%d (rule revision %d)", pr, rule.Revision))
	}
	if len(verdict.Refusals) > 0 {
		return autonomy.AnswerUnmet(verdict.Refusals[0].Sentence)
	}
	return cannotSay("the evaluation refused without a sentence")
}

// --- the merged fact (VG14) -----------------------------------------------------

// mergeEventRecorder is the loop's Merged hook: the fact that the merge
// landed, written AFTER the successful dispatch, idempotent in the store.
// The engine's contract makes a failure here a loop.warning, never a
// failure of the merge itself (the seam error is surfaced, the merge
// stands).
//
// The record's identity comes from the merge's dispatch event: the head
// the rule measured (verify_head_sha — present for a non-baseline rule,
// absent for the baseline, which carries no extended evidence), the rule's
// revision, and the run it measured. The method is the RULE's (the
// baseline's squash); BaseRef stays NULL — the dispatch event does not
// carry it and the post-merge path makes no forge call.
func (d *Daemon) mergeEventRecorder(ctx context.Context, rec loop.MergeRecord) error {
	ev := rec.Ev
	method := "squash"
	componentID := ""
	if comp, err := d.store.ComponentForRoot(rec.ComponentRoot); err == nil {
		componentID = comp.ID
		if rule, _, err := d.store.EffectiveMergeRule(comp.ID); err == nil && rule.Method != "" {
			method = rule.Method
		}
	}
	ruleRevision := 0
	if s := ev["rule_revision"]; s != "" {
		ruleRevision, _ = strconv.Atoi(s)
	}
	me, err := d.store.RecordMergeEvent(store.MergeEventRecord{
		Scope:        store.Scope{LoopSlug: rec.LoopSlug, ComponentRoot: rec.ComponentRoot},
		ComponentID:  componentID,
		PR:           rec.PR,
		MergeSHA:     rec.MergeSHA,
		Method:       method,
		HeadSHA:      ev["verify_head_sha"],
		RuleRevision: ruleRevision,
		Evidence:     gateEvidenceString(ev),
		VerifyRunID:  ev["verify_run_id"],
		Actor:        engineActor,
	})
	if err != nil {
		return err
	}
	// S57 T7 (VG5): the post-merge watch — a SEPARATE write after the
	// record succeeded. The merge fact stands regardless of the watch: a
	// failed enqueue is logged and repaired by the startup sweep, never a
	// failed merge. The recorder's contract (a seam error is a loop.warning,
	// the merge stands) is untouched.
	d.enqueueMergeWatchForEvent(me)
	return nil
}

// gateEvidenceString is the verdict's account in sorted k=v form — the
// one evidence column the merge_event row keeps (D-T6-7).
func gateEvidenceString(ev map[string]string) string {
	keys := make([]string, 0, len(ev))
	for k := range ev {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+ev[k])
	}
	return strings.Join(parts, " | ")
}

// forgeClient is the loop root's forge identity (the daemon owns no forge
// identity of its own); the construction is the shared forgeClientForRoot
// the merge-plane reads (S57 T8) call on the same terms.
func (d *Daemon) forgeClient(root string) forge.Forge {
	return forgeClientForRoot(root)
}

// forgeClientForRoot is the root's forge identity: the origin remote's
// owner/repo. A nil answer is a non-GitHub origin: the outward half is
// simply absent, and the local half (verify, git, audit) stands on its own.
func forgeClientForRoot(root string) forge.Forge {
	owner, repo, ok := githubOrigin(root)
	if !ok {
		return nil
	}
	return forge.New(forge.Config{
		Owner:       owner,
		Repo:        repo,
		BaseURL:     forge.DefaultBaseURL, // named once, here, from the constant in the one file the tripwire exempts
		TokenSource: forge.DefaultTokenSource(),
		MaxRetries:  3,
	})
}

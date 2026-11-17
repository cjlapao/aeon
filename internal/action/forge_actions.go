package action

// The forge-facing actions that CREATE outward records (T5): openIssue and
// openPR. Both find by SEARCHING the forge — neither trusts a stored
// number (D1). The crash between creating and recording is exactly the
// case S7 was built for, and a stored number may be stale: an issue
// closed and reopened, a PR closed by hand. The forge is the truth about
// the forge, and no signature even accepts a number to remember (pinned
// by reflection).
//
// `open` is part of both checks: a closed issue for the same task means
// the task was DONE, and reusing a record someone deliberately closed
// resurrects it.
//
// The search keys are business keys — an issue's exact title plus label,
// a PR's head ref — which is why these two need no receipt (contrast
// T7's postReview, which has none).

import (
	"context"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/mergerule"
)

// ForgeActions is the door the registry (T10) wires.
type ForgeActions struct {
	Forge forge.Forge

	// Git is the CHECK-ONLY branch probe: branch creation is not
	// expressible through it, so openPR reusing a branch is a type
	// guarantee, not a behavioural one. The branch must have been pushed
	// by commitAndPush — opening a PR against a ref nobody can fetch is
	// O8's lesson re-learned at the forge boundary.
	Git BranchProbe

	// Merge is the post-merge guard's instrument (D5) — REQUIRED for the
	// irreversible action: forge.merge refuses to run without a probe,
	// because an unguarded irreversible action would not run.
	Merge MergeProbe

	// Intents is the ONE receipt path (D7) — REQUIRED for the one
	// receipt-first action: postReview refuses to run without it,
	// because a receipt-first action that cannot write receipts is just
	// an action that hopes.
	Intents IntentStore

	// Record is the evidence sink (Q7): numbers land in loop state and
	// the transcript. T10 wires it to the store; tests wire a recorder.
	Record func(Evidence)

	// RuleSource answers the merge rule governing this merge (VG10).
	// A nil seam takes the baseline — today's gate, byte for byte
	// (VG11): the baseline is the contract, and a tree that never
	// authored a rule still has one. The daemon wires the store's
	// EffectiveMergeRule; tests wire a fixed rule or leave it nil.
	RuleSource func(ctx context.Context) (mergerule.Rule, error)

	// GateFacts fills the extended half of the gate's facts — the run
	// the rule measures (LatestRun) and the DoD it demands (DoD) — for
	// a non-baseline rule. The query is the merge's identity: a run's
	// scope is (component root, loop, head) and the DoD ledger is
	// scoped the same way, so the seam cannot ask the store without all
	// three. A nil seam leaves the facts honest-absent, and the
	// baseline path never reads them, so an unwired tree evaluates
	// exactly as today.
	GateFacts func(ctx context.Context, q GateFactsQuery, facts *mergerule.Facts) error

	// Rule is S60 T6's merge-rule seam for forge.land (MQ12.2): consulted
	// AFTER mergeGate and BEFORE the forge call, never instead of it. A
	// nil seam is recorded as `merge_rule: "none-configured"` — the
	// absence is evidence, not a pass.
	Rule MergeRule

	// Grants is S60 T6's S56 seam, consulted only by forge.land — the
	// queue's one irreversible action. nil is deny-leaning (landing
	// then needs the attended ask; nothing else reaches it).
	Grants Grants

	// Fetch is forge.land's other post-guard instrument: the base ref
	// must be CURRENT before IsAncestor answers. Required for land
	// exactly as Merge is (an irreversible action whose guard cannot
	// run does not run); forge.merge never reads it.
	Fetch BaseFetcher
}

// GateFactsQuery is the identity of the merge the gate is asking the
// forge about: which PR, of which loop, in which component root, at
// which head. The daemon's closure scopes its store reads (the run the
// rule measures, the DoD it demands) by exactly these halves — and the
// root comes from the merge's dir, which the query carries so the seam
// never has to look the loop up.
type GateFactsQuery struct {
	PR            int
	LoopSlug      string
	ComponentRoot string
	HeadSHA       string
}

// BranchProbe is deliberately one method wide.
type BranchProbe interface {
	BranchExistsRemote(ctx context.Context, dir, branch string) (bool, error)
}

// Evidence is what a completed outward action hands to loop state.
type Evidence struct {
	Action string
	Key    string
	Value  string
}

// ForgeResult is the discriminated outcome: the number (evidence, Q7) and
// whether the forge already had the record.
type ForgeResult struct {
	Action string
	Number int
	Reused bool
}

// ForgeArgs carries identities, not numbers. Dir is where the git
// questions get asked; Names build the keys; TaskTitle feeds the exact
// title (the key format itself is code, below).
type ForgeArgs struct {
	Dir       string
	Names     BranchNames
	TaskTitle string
	Base      string
	PRBody    string
}

// IssueTitle is the business key, constructed by code from the task
// identity — the format v1 used and the search matches.
func IssueTitle(n BranchNames, taskTitle string) string {
	return fmt.Sprintf("[%s] %s", n.Task, taskTitle)
}

// DevLoopLabel is the label that scopes the search to loop-opened issues.
const DevLoopLabel = "dev-loop"

func (a *ForgeActions) record(e Evidence) {
	if a.Record != nil {
		a.Record(e)
	}
}

// OpenIssue searches OPEN issues for the exact title with label dev-loop.
// Found → its number, no create. Not found → open, and the number comes
// back as evidence. `blocked` transport errors pass through with their
// kind — "could not reach the forge" is not "the forge said no" (O5), and
// the loop retries one and reports the other.
func (a *ForgeActions) OpenIssue(ctx context.Context, args ForgeArgs) (ForgeResult, error) {
	if a.Forge == nil {
		return ForgeResult{}, fmt.Errorf("forge.openIssue: no forge wired")
	}
	title := IssueTitle(args.Names, args.TaskTitle)
	found, err := a.Forge.FindOpenIssue(ctx, title, DevLoopLabel)
	if err != nil {
		return ForgeResult{}, classify(err)
	}
	if found != nil {
		a.record(Evidence{Action: ForgeOpenIssue, Key: "issue_number", Value: fmt.Sprintf("%d", found.Number)})
		return ForgeResult{Action: ForgeOpenIssue, Number: found.Number, Reused: true}, nil
	}
	opened, err := a.Forge.OpenIssue(ctx, forge.IssueRequest{
		Title:  title,
		Body:   fmt.Sprintf("Opened by loop %s for task %s.", args.Names.Loop, args.Names.Task),
		Labels: []string{DevLoopLabel},
	})
	if err != nil {
		// A conflict means someone raced us to it — ONE re-search (never
		// a retry loop) resolves it rather than double-opening.
		if isConflict(err) {
			if again, serr := a.Forge.FindOpenIssue(ctx, title, DevLoopLabel); serr == nil && again != nil {
				a.record(Evidence{Action: ForgeOpenIssue, Key: "issue_number", Value: fmt.Sprintf("%d", again.Number)})
				return ForgeResult{Action: ForgeOpenIssue, Number: again.Number, Reused: true}, nil
			}
		}
		return ForgeResult{}, classify(err)
	}
	a.record(Evidence{Action: ForgeOpenIssue, Key: "issue_number", Value: fmt.Sprintf("%d", opened.Number)})
	return ForgeResult{Action: ForgeOpenIssue, Number: opened.Number}, nil
}

// OpenPR keys on the HEAD REF — not the title, which two PRs can share.
// An open PR with head task/{loop}/{task} satisfies the check. The branch
// must already exist on the remote (pushed by commitAndPush): the action
// refuses rather than opening a PR against a ref nobody can find, and the
// refusal names commitAndPush as the way forward. Base is REQUIRED (L26):
// a PR that cannot say what it merges into cannot be trusted at merge
// time.
func (a *ForgeActions) OpenPR(ctx context.Context, args ForgeArgs) (ForgeResult, error) {
	if a.Forge == nil {
		return ForgeResult{}, fmt.Errorf("forge.openPR: no forge wired")
	}
	if args.Base == "" {
		return ForgeResult{}, fmt.Errorf("forge.openPR refuses: the base ref is required (L26) — a PR that cannot say what it merges into cannot be trusted at merge time")
	}
	head := args.Names.TaskBranch()
	found, err := a.Forge.FindOpenPR(ctx, head)
	if err != nil {
		return ForgeResult{}, classify(err)
	}
	if found != nil {
		a.record(Evidence{Action: ForgeOpenPR, Key: "pr_number", Value: fmt.Sprintf("%d", found.Number)})
		return ForgeResult{Action: ForgeOpenPR, Number: found.Number, Reused: true}, nil
	}
	if a.Git == nil {
		return ForgeResult{}, fmt.Errorf("forge.openPR: no branch probe wired")
	}
	present, err := a.Git.BranchExistsRemote(ctx, args.Dir, head)
	if err != nil {
		return ForgeResult{}, classify(err)
	}
	if !present {
		return ForgeResult{}, &BlockedError{Reason: "head-missing", Detail: fmt.Sprintf(
			"the head %q has never been pushed — run commitAndPush first; a PR against a ref nobody can fetch is the phantom-ref failure (O8) at the forge boundary", head)}
	}
	opened, err := a.Forge.OpenPR(ctx, forge.PRRequest{
		Title: IssueTitle(args.Names, args.TaskTitle),
		Body:  args.PRBody,
		Head:  head,
		Base:  args.Base,
	})
	if err != nil {
		if isConflict(err) {
			// Raced to open — ONE re-search resolves it; the branch is
			// never recreated (it cannot be, through this probe).
			if again, serr := a.Forge.FindOpenPR(ctx, head); serr == nil && again != nil {
				a.record(Evidence{Action: ForgeOpenPR, Key: "pr_number", Value: fmt.Sprintf("%d", again.Number)})
				return ForgeResult{Action: ForgeOpenPR, Number: again.Number, Reused: true}, nil
			}
		}
		return ForgeResult{}, classify(err)
	}
	a.record(Evidence{Action: ForgeOpenPR, Key: "pr_number", Value: fmt.Sprintf("%d", opened.Number)})
	return ForgeResult{Action: ForgeOpenPR, Number: opened.Number}, nil
}

// BlockedError marks a step-blocked outcome that is NOT a refusal: the
// world is not shaped right (head missing, transport down), and the
// loop's honest answer is blocked-with-reason, never "the forge said no"
// (O5).
type BlockedError struct {
	Reason string
	Detail string
}

func (e *BlockedError) Error() string { return "blocked (" + e.Reason + "): " + e.Detail }

// classify maps a forge error to the action layer's vocabulary without
// flattening kinds: transport stays transport, refused stays refused.
func classify(err error) error { return err }

func isConflict(err error) bool {
	fe, ok := err.(*forge.ForgeError)
	return ok && fe.Kind == forge.KindConflict
}

// --- forge.merge and the phantom-merge guard (T6, one task — R6) ------------
//
// The guard is not a check bolted beside the merge; it is what makes the
// merge correct, and this is the only `irreversible` action in the
// registry. Splitting merge from guard creates a window where the merge
// works and the guard does not — R6 keeps them one task.

// MergeProbe asks the tree the one question the post-merge guard needs.
// Kept separate from BranchProbe so "openPR cannot create" stays a type
// guarantee while merge gets its ancestry instrument.
type MergeProbe interface {
	IsAncestor(ctx context.Context, dir, sha, ref string) (bool, error)
}

// MergeArgs: which PR, into which feature branch (derived), in which dir.
type MergeArgs struct {
	Dir      string
	Names    BranchNames
	PRNumber int
	Method   forge.MergeMethod
	// Loop is the loop slug the merge belongs to. The gate's extended
	// facts (the measured run, the DoD ledger) are scoped (component
	// root, loop, head), and the root comes from Dir — the slug is the
	// other half of that scope, so it rides with the merge's identity.
	Loop string
	// AeonReview is the engine's review token (VG9) — "ok@<head sha>",
	// carried from the loop's EVIDENCE (the review step's ok at that
	// head), never from a handoff. The gate reads it for a rule whose
	// review mode admits Aeon's own review; the baseline ignores it.
	AeonReview string
}

// MergeResult reports the merge and its SHA — the recorded SHA when the
// PR was merged before, the fresh SHA otherwise. The SHA is the squash
// SHA (the merge result's own), never a task branch original.
type MergeResult struct {
	Action        string
	PR            int
	SHA           string
	AlreadyMerged bool
	// Fields is the gate's audit trail (T8/O6): the review state read,
	// the reviewer, the timestamp, the check results and the thread
	// answer, arriving in loop state as the step's evidence.
	Fields map[string]string
}

// PhantomMergeError is the guard failing AFTER the forge reported a
// merge: the forge said merged, the tree says otherwise, and the tree
// wins. Reporting success here would be the phantom itself.
type PhantomMergeError struct {
	PR   int
	SHA  string
	Base string
}

func (e *PhantomMergeError) Error() string {
	return "phantom merge (D5): PR #" + itoaInt(e.PR) + " reported merged at " + e.SHA +
		" but that commit is not an ancestor of " + e.Base + " — the work is NOT where the loop believes it is; " +
		"verify the squash SHA (the task branch's originals are never ancestors after a squash — checking them would call every correct merge a failure)"
}

func itoaInt(n int) string {
	if n == 0 {
		return "0"
	}
	digits := "0123456789"
	var b []byte
	for n > 0 {
		b = append([]byte{digits[n%10]}, b...)
		n /= 10
	}
	return string(b)
}

// MergePR is check-then-act with BOTH guards (D5). Before: the PR's
// base.ref must BE the feature branch — a PR merged into the wrong base
// looks merged, reports merged, and its work is not where the loop
// believes it is (the phantom-merge root cause; T1's PR.BaseRef exists
// for this one assertion, L26). After: the merge SHA must be an ancestor
// of the feature branch — and it is the MERGE RESULT's sha that gets
// asked about, never the task branch's originals.
//
// An already-merged PR returns its recorded SHA and SUCCEEDS: never
// merge twice, never fail — the desired end state reported as failure
// would fail a loop whose work is done.
//
// A refusal (protected branch) is an honest failure naming it (N4): no
// merge-queue workaround, no retry with another method — working around
// branch protection is working around a decision someone made
// deliberately.
func (a *ForgeActions) MergePR(ctx context.Context, args MergeArgs) (MergeResult, error) {
	if a.Forge == nil {
		return MergeResult{}, fmt.Errorf("forge.merge: no forge wired")
	}
	if a.Merge == nil {
		return MergeResult{}, fmt.Errorf("forge.merge: no merge probe wired — an unguarded irreversible action would not run")
	}
	if args.PRNumber <= 0 {
		return MergeResult{}, fmt.Errorf("forge.merge: PR number must be positive — the evidence comes from openPR's result (Q7)")
	}
	feature := args.Names.FeatureBranch()

	pr, err := a.Forge.ReadPR(ctx, args.PRNumber)
	if err != nil {
		return MergeResult{}, classify(err)
	}
	if pr.Merged {
		// Desired end state: success, the recorded SHA, nothing else.
		a.record(Evidence{Action: ForgeMerge, Key: "merge_sha", Value: pr.MergeSHA})
		return MergeResult{Action: ForgeMerge, PR: args.PRNumber, SHA: pr.MergeSHA, AlreadyMerged: true}, nil
	}

	// The guard BEFORE merging (D5) — the phantom-merge root cause. A
	// missing base is the same refusal: a PR that cannot say its base
	// cannot be trusted at merge time (L26).
	if pr.BaseRef == "" {
		return MergeResult{}, forge.NewMsg(forge.KindRefused, fmt.Sprintf(
			"PR #%d has no base ref — refusing to merge something that cannot say what it merges into (L26)", args.PRNumber))
	}
	if pr.BaseRef != feature {
		return MergeResult{}, forge.NewMsg(forge.KindRefused, fmt.Sprintf(
			"phantom-merge guard: PR #%d is based on %q, not the feature branch %q — merging it would land work where nobody is looking (D5)",
			args.PRNumber, pr.BaseRef, feature))
	}

	// The review-STATE gate (L34, D10) — between the base guard and the
	// irreversible call, so a blocked gate makes MergePR unreachable:
	// "a check that happens to run before an action is a convention,
	// and a check the action cannot be reached without is a property"
	// (the same shape as S11's AllowToken). S57 T6: the gate is now
	// mergerule.Evaluate under the effective rule — the baseline is the
	// byte-for-byte contract, a rule row adds its own requirements.
	rule, err := a.effectiveRule(ctx)
	if err != nil {
		return MergeResult{}, err
	}
	gateFields, err := a.mergeGate(ctx, GateFactsQuery{
		PR: args.PRNumber, LoopSlug: args.Loop, ComponentRoot: args.Dir, HeadSHA: pr.HeadSHA,
	}, args.AeonReview, rule)
	if err != nil {
		return MergeResult{}, err
	}

	// The method: the caller's wins; otherwise the rule's (the baseline
	// is squash — today's default, so an unwired tree merges exactly as
	// it always has, and a rule row may say merge or rebase).
	method := args.Method
	if method == "" {
		method = forge.MergeMethod(rule.Method)
	}
	merged, err := a.Forge.MergePR(ctx, args.PRNumber, method)
	if err != nil {
		// Honest failure — N4: no workaround attempted, no second shape.
		return MergeResult{}, classify(err)
	}
	if !merged.Merged || merged.SHA == "" {
		return MergeResult{}, &PhantomMergeError{PR: args.PRNumber, SHA: "(none reported)", Base: feature}
	}

	// The guard AFTER merging (D5): the MERGE RESULT's sha, never the
	// originals. The tree's answer outranks the forge's report.
	ancestor, err := a.Merge.IsAncestor(ctx, args.Dir, merged.SHA, feature)
	if err != nil {
		return MergeResult{}, fmt.Errorf("forge.merge cannot verify the merge landed (D5): %w", err)
	}
	if !ancestor {
		return MergeResult{}, &PhantomMergeError{PR: args.PRNumber, SHA: merged.SHA, Base: feature}
	}
	a.record(Evidence{Action: ForgeMerge, Key: "merge_sha", Value: merged.SHA})
	return MergeResult{Action: ForgeMerge, PR: args.PRNumber, SHA: merged.SHA, Fields: gateFields}, nil
}

// effectiveRule answers "which merge rule governs this merge" (VG10):
// the seam's row when wired, the baseline when not. The baseline is the
// contract (VG11) — a nil seam is the tree's answer, not a gap.
func (a *ForgeActions) effectiveRule(ctx context.Context) (mergerule.Rule, error) {
	if a.RuleSource == nil {
		return mergerule.Baseline(), nil
	}
	return a.RuleSource(ctx)
}

// mergeGate gathers the forge facts (the gatherer), evaluates them under
// the effective rule (mergerule.Evaluate, T5 — the pure half), and
// returns the gate's evidence or its refusal. The gatherer stays here,
// in the action package, because its refusals carry a TRANSPORT error
// the Facts type cannot hold (the golden's two gather-error rows); the
// evaluation's refusals are the rule's own sentences, verbatim.
//
// The baseline path (no seam, or a baseline-shaped row) is byte for byte
// today's gate: the same five evidence keys, the same first-refusal-wins
// order, the same sentences — the T1 goldens pin it. A non-baseline rule
// ADDS its facts (the run, the DoD) through GateFacts and its
// requirements through the evaluator; its refusals are all collected,
// in order, joined.
//
// q carries the merge's identity (PR, loop, component root, head) — the
// head is the PR head at merge time (the rule's require_local measures a
// run against it), and the root+slug are the extended facts' store scope;
// aeonReview is the engine's token (VG9).
func (a *ForgeActions) mergeGate(ctx context.Context, q GateFactsQuery, aeonReview string, rule mergerule.Rule) (map[string]string, error) {
	facts, err := a.gatherGateFacts(ctx, q.PR, q.HeadSHA)
	if err != nil {
		return nil, err
	}
	facts.AeonReview = aeonReview
	if rule.Enabled && !rule.IsBaseline() && a.GateFacts != nil {
		if err := a.GateFacts(ctx, q, &facts); err != nil {
			return nil, fmt.Errorf("forge.merge gate: reading the rule's facts: %w", err)
		}
	}
	verdict := mergerule.Evaluate(rule, facts)
	if !verdict.Allowed {
		sentences := make([]string, 0, len(verdict.Refusals))
		for _, r := range verdict.Refusals {
			sentences = append(sentences, r.Sentence)
		}
		return nil, forge.NewMsg(forge.KindRefused, strings.Join(sentences, "; "))
	}
	return verdict.Evidence, nil
}

// GatherGateFacts reads the three signals every merge asks (L34/D10):
//
//  1. the reviews (latest STATE approve, no outstanding REQUEST_CHANGES);
//  2. the checks via the status half — a server (or repository) that
//     reports NO checks answers N/A, and N/A is not a failure;
//  3. the unresolved threads where the server reports them — honoured,
//     never resolved.
//
// A server that cannot answer (2) or (3) at all is the threadless
// reference server the protocol names; absence passes with the absence
// RECORDED (the Facts carry it as ChecksReadable=false / a zero
// ThreadFacts, and the evaluator's evidence names it). A server that
// claims the capability and then fails transport/auth REFUSES at the
// gatherer — unverifiable is not green right before something
// irreversible, and the sentence carries the transport error no Facts
// can hold.
//
// It is exported because the merge rule has TWO consumers with one
// fact-set (S57 T6, D-T6-1): the gate at the merge dispatch (here) and
// the autonomy seam at the grant's decision site (the daemon). One
// gatherer, two call sites, so the facts the grant's live check sees
// cannot drift from the facts the merge's gate will see.
func GatherGateFacts(ctx context.Context, f forge.Forge, pr int, headSHA string) (mergerule.Facts, error) {
	refuse := func(detail string) error {
		return forge.NewMsg(forge.KindRefused, "forge.merge gate: "+detail+" — the merge is refused BEFORE the forge is asked (L34/D10)")
	}
	facts := mergerule.Facts{PR: pr, HeadSHA: headSHA}

	reviews, err := f.ListReviews(ctx, pr)
	if err != nil {
		return facts, refuse(fmt.Sprintf("PR #%d reviews cannot be read: %v", pr, err))
	}
	facts.Reviews = reviews

	if sl, ok := f.(forge.StatusLister); ok {
		checks, err := sl.ListStatuses(ctx, pr)
		if err != nil && !isNotApplicable(err) {
			return facts, refuse(fmt.Sprintf("PR #%d checks cannot be read: %v", pr, err))
		}
		// A not-applicable answer is an EMPTY readable report, not an
		// unreadable one: T1's own split, its goldens pin it byte for
		// byte (VG11) — the no-API evidence is reserved for a forge that
		// claims no status API at all, and a claimed API that cannot
		// answer the commit reports "no checks", never "no API".
		state, _ := forge.Aggregate(checks, true)
		facts.Checks = forge.ChecksReport{SHA: headSHA, Checks: checks, State: state, Complete: true}
		facts.ChecksReadable = true
	}

	if tl, ok := f.(forge.ThreadLister); ok {
		threads, err := tl.ListUnresolvedThreads(ctx, pr)
		if err != nil && !isNotApplicable(err) {
			return facts, refuse(fmt.Sprintf("PR #%d threads cannot be read: %v", pr, err))
		}
		facts.Threads = mergerule.ThreadFacts{Available: true, Readable: err == nil, Unresolved: threads}
	}
	return facts, nil
}

// gatherGateFacts is the action-side half of GatherGateFacts — the
// daemon's autonomy seam shares the gatherer (D-T6-1).
func (a *ForgeActions) gatherGateFacts(ctx context.Context, pr int, headSHA string) (mergerule.Facts, error) {
	return GatherGateFacts(ctx, a.Forge, pr, headSHA)
}

// isNotApplicable: the server's own "nothing here" answers. not-found
// and refused are the two a conforming forge uses for capability
// absence; transport/auth are the two that must NEVER read as N/A —
// conflating them sends someone to check their permissions when their
// network is out (O5).
func isNotApplicable(err error) bool {
	fe, ok := err.(*forge.ForgeError)
	return ok && (fe.Kind == forge.KindNotFound || fe.Kind == forge.KindRefused)
}

// --- strategies: the exception is the point (D7) ------------------------------

// Strategy names how an action achieves idempotency. A plan applying one
// strategy uniformly gets it wrong in exactly one place, so the
// classification is declared, and TestPostReviewIsTheOnlyReceiptFirstAction
// enumerates it.
const (
	StrategyCheckThenAct = "check-then-act" // a business key exists: search first
	StrategyReceiptFirst = "receipt-first"  // no key exists: intent, post, record
	// StrategyNaturallyIdempotent: a read — running it twice changes
	// nothing, so it needs no key AND no receipt. It is its own strategy
	// so the receipt-first count stays exactly one by CONSTRUCTION, not
	// by audit being mislabelled as an act.
	StrategyNaturallyIdempotent = "naturally-idempotent"
)

var strategies = map[string]string{
	GitCreateBranch:        StrategyCheckThenAct,
	GitCommitAndPush:       StrategyCheckThenAct,
	GitMergeBranch:         StrategyCheckThenAct,
	GitIntegrate:           StrategyCheckThenAct, // the ancestor check IS the re-run guard (MQ6)
	GitConcludeIntegration: StrategyCheckThenAct, // clean-tree assert answers the re-run
	GitPublishIntegration:  StrategyCheckThenAct, // the name gate + a new ref: plain-push idempotent
	QueueEnqueue:           StrategyNaturallyIdempotent, // one OPEN row per branch is the queue's own key (MQ8)
	ForgeOpenIssue:         StrategyCheckThenAct,
	ForgeOpenPR:            StrategyCheckThenAct,
	ForgeOpenLandingPR:     StrategyCheckThenAct, // FindOpenPR(head) first — the head ref is the business key
	ForgePostReview:        StrategyReceiptFirst, // the one — a review has no natural business key
	ForgeMerge:             StrategyCheckThenAct,
	ForgeLand:              StrategyCheckThenAct, // the already-merged answer IS the re-run guard (MQ12)
	ForgeCloseIssue:        StrategyCheckThenAct,
	VerifyRun:              StrategyNaturallyIdempotent, // a run is evidence; each round is its own attempt row
	Audit:                  StrategyNaturallyIdempotent,
	// S61 T4: both bootstrap actions are check-then-act — scaffold checks
	// the target is absent-or-empty BEFORE writing (a matching existing
	// tree is "already done", never an overwrite), and createRepo GETs the
	// repository before POSTing it (GI10's strategy, half-done resumes).
	RepoScaffold:    StrategyCheckThenAct,
	ForgeCreateRepo: StrategyCheckThenAct,
}

// Strategy reports the declared strategy for an action; the closed set is
// code, not configuration (same rule as classes).
func Strategy(name string) string { return strategies[name] }

// --- the intent seam (Q6: ONE table, so ONE recovery path) --------------------

// Intent is the receipt of an about-to-happen outward post, keyed
// (pr, step, attempt). ReviewID == 0 means "posted? unknown — go and
// look"; reconciliation matches BY ID (D7), never by body text: two
// reviews with identical bodies are two reviews, and text matching would
// either miss one or merge two, wrong in a way nobody can see afterwards.
type Intent struct {
	Loop        string
	StepID      int64
	Attempt     int
	Action      string
	PR          int
	PreReviewID int64 // the highest review id seen before this attempt began
	ReviewID    int64 // 0 = unrecorded
}

// IntentStore is the action side of the single intent table (internal/
// store's action_intent — migration 0025). Q6's answer: S7's
// tool_receipt records SPAWNS (command, token, outcome); action intents
// are a different shape read by a different recovery path, and the
// invariant TestOneIntentTable pins is that exactly ONE table holds
// action intents — two would be two recovery paths, and recovery must
// have one.
type IntentStore interface {
	BeginIntent(ctx context.Context, in Intent) error
	RecordIntent(ctx context.Context, in Intent, reviewID int64) error
	OpenIntent(ctx context.Context, pr int, stepID int64, attempt int) (*Intent, error)
}

// ReviewArgs carries the review and the (step, attempt) identity that
// makes its receipt unique.
type ReviewArgs struct {
	Names   BranchNames
	Loop    string
	PR      int
	StepID  int64
	Attempt int
	Body    string
	Verdict forge.Verdict
}

// ReviewResult: the review id (recorded), and whether this run reconciled
// a crashed predecessor rather than posting.
type ReviewResult struct {
	Action     string
	PR         int
	ReviewID   int64
	Reconciled bool
	Reused     bool
}

// PostReview is the one receipt-first action (D7): intent, post, record.
//
// A resume finding an intent with no id means go and LOOK — one bounded
// ListReviews (O3), candidates filtered to ids above the intent's
// baseline, exactly the half that is easy to get wrong: reconciliation
// matches BY ID, never by body. Zero candidates or more than one is the
// honest unknown: fail naming the intent rather than posting into a
// possible duplicate (S7's rule: failing beats guessing when an effect
// may or may not have happened).
func (a *ForgeActions) PostReview(ctx context.Context, args ReviewArgs) (ReviewResult, error) {
	if a.Forge == nil {
		return ReviewResult{}, fmt.Errorf("forge.postReview: no forge wired")
	}
	if a.Intents == nil {
		return ReviewResult{}, fmt.Errorf("forge.postReview refuses: no intent store wired — a receipt-first action without its receipt path would be an action that hopes (D7)")
	}
	if args.PR <= 0 || args.StepID <= 0 || args.Attempt < 1 {
		return ReviewResult{}, fmt.Errorf("forge.postReview needs pr, step and attempt — the intent key is (pr, step, attempt)")
	}
	in := Intent{Loop: args.Loop, StepID: args.StepID, Attempt: args.Attempt, Action: ForgePostReview, PR: args.PR}

	// Resume path: an intent with no id means go and look.
	open, err := a.Intents.OpenIntent(ctx, args.PR, args.StepID, args.Attempt)
	if err != nil {
		return ReviewResult{}, err
	}
	if open != nil {
		if open.ReviewID != 0 {
			// Posted and recorded before the crash — the reuse answer.
			return ReviewResult{Action: ForgePostReview, PR: args.PR, ReviewID: open.ReviewID, Reused: true}, nil
		}
		reviews, err := a.Forge.ListReviews(ctx, args.PR) // ONE listing: bounded (O3)
		if err != nil {
			return ReviewResult{}, classify(err)
		}
		var candidates []forge.Review
		for _, r := range reviews {
			if r.ID > open.PreReviewID {
				candidates = append(candidates, r)
			}
		}
		if len(candidates) != 1 {
			return ReviewResult{}, fmt.Errorf(
				"forge.postReview cannot resolve the intent (step %d, attempt %d, pr %d): %d reviews appeared after the baseline — the effect may or may not have happened, and reposting would gamble on a duplicate (D7/O3: fail naming the intent)",
				args.StepID, args.Attempt, args.PR, len(candidates))
		}
		if err := a.Intents.RecordIntent(ctx, *open, candidates[0].ID); err != nil {
			return ReviewResult{}, err
		}
		a.record(Evidence{Action: ForgePostReview, Key: "review_id", Value: fmt.Sprintf("%d", candidates[0].ID)})
		return ReviewResult{Action: ForgePostReview, PR: args.PR, ReviewID: candidates[0].ID, Reconciled: true}, nil
	}

	// Fresh attempt: baseline, intent BEFORE the effect, post, record.
	baseline := int64(0)
	if reviews, err := a.Forge.ListReviews(ctx, args.PR); err == nil {
		for _, r := range reviews {
			if r.ID > baseline {
				baseline = r.ID
			}
		}
	} else {
		return ReviewResult{}, classify(err)
	}
	in.PreReviewID = baseline
	if err := a.Intents.BeginIntent(ctx, in); err != nil {
		return ReviewResult{}, err
	}
	posted, err := a.Forge.PostReview(ctx, args.PR, args.Body, args.Verdict)
	if err != nil {
		return ReviewResult{}, classify(err)
	}
	if err := a.Intents.RecordIntent(ctx, in, posted.ID); err != nil {
		return ReviewResult{}, err
	}
	a.record(Evidence{Action: ForgePostReview, Key: "review_id", Value: fmt.Sprintf("%d", posted.ID)})
	return ReviewResult{Action: ForgePostReview, PR: args.PR, ReviewID: posted.ID}, nil
}

// --- forge.closeIssue (D4) -----------------------------------------------------

// CloseArgs: which issue. Numbers are evidence from openIssue (Q7), not
// remembered guesses.
type CloseArgs struct {
	IssueNumber int
}

// CloseResult says whether this call closed it or found it closed.
type CloseResult struct {
	Action        string
	Issue         int
	Closed        bool
	AlreadyClosed bool
}

// CloseIssue is the only thing that closes a task issue when its work
// lands, because `Closes #n` does NOT fire for task PRs: GitHub's
// keyword auto-closes only on a merge into the DEFAULT branch, and task
// PRs target the feature branch — the keyword is a comforting no-op
// (D4). Check-then-act: read first, closed ⇒ done without a call; and
// after a close it MUST READ THE ISSUE BACK — a silently-failed close
// leaves the issue open with nothing downstream noticing, the loop
// believing the task done while the tracker disagrees, indefinitely.
func (a *ForgeActions) CloseIssue(ctx context.Context, args CloseArgs) (CloseResult, error) {
	if a.Forge == nil {
		return CloseResult{}, fmt.Errorf("forge.closeIssue: no forge wired")
	}
	if args.IssueNumber <= 0 {
		return CloseResult{}, fmt.Errorf("forge.closeIssue needs a positive issue number — evidence from openIssue, never a remembered guess")
	}
	iss, err := a.Forge.ReadIssue(ctx, args.IssueNumber)
	if err != nil {
		return CloseResult{}, classify(err)
	}
	if iss.State == "closed" {
		return CloseResult{Action: ForgeCloseIssue, Issue: args.IssueNumber, AlreadyClosed: true}, nil
	}
	if err := a.Forge.CloseIssue(ctx, args.IssueNumber); err != nil {
		return CloseResult{}, classify(err)
	}
	// THE assertion: read it back. A close that reported success and did
	// nothing is invisible to every return-value check (C4).
	back, err := a.Forge.ReadIssue(ctx, args.IssueNumber)
	if err != nil {
		return CloseResult{}, classify(err)
	}
	if back.State != "closed" {
		return CloseResult{}, forge.NewMsg(forge.KindRefused, fmt.Sprintf(
			"forge.closeIssue: the forge accepted the close of #%d and the read-back says it is still %q — a silently-failed close leaves the tracker disagreeing with the loop indefinitely, so this is a failure, not a success (D4)",
			args.IssueNumber, back.State))
	}
	a.record(Evidence{Action: ForgeCloseIssue, Key: "issue_closed", Value: fmt.Sprintf("%d", args.IssueNumber)})
	return CloseResult{Action: ForgeCloseIssue, Issue: args.IssueNumber, Closed: true}, nil
}

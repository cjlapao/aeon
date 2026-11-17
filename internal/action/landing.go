package action

// S60 T6 · the landing half of the merge queue (MQ8/MQ11/MQ12/MQ13).
//
// Two actions live here that forge_actions.go cannot hold — it is already
// 762 lines and SIZE1 is the line every reader pays for. This file is the
// landing question as one concern: open the landing PR, and land it.
//
// `forge.merge` is NOT here and NOT touched: MQ12 says it out loud —
// `forge.land` is a NEW action; merge's guard, tests and evidence stay
// exactly as they are. What land reuses is the GATE (mergeGate, verbatim)
// and the phantom-merge reasoning, applied to the right base: the queue's
// PR goes into the ITEM'S base branch, not a feature branch.
//
// The irreversible call stays unreachable except through all of: the PR's
// own base/head agreement, the gate, the merge-rule seam, the verified-SHA
// binding (MQ11), and the post-merge ancestor guard. A refusal at any of
// those leaves the Fake's (or GitHub's) call log without a MergePR entry —
// the tests read that log, not a mock expectation.

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/mergerule"
)

// --- the seams the queue needs, each fail-closed (MQ12.2) -------------------

// MergeFacts is what the merge-rule seam is asked about: the PR as the
// forge just said it, the gate's own facts (reviews, checks, threads —
// the same gatherer the gate uses, so the two consumers cannot answer
// differently), and the queue's binding facts. It is a struct of reads,
// never a verdict — the seam decides, this file only carries.
type MergeFacts struct {
	PR             forge.PR
	Gate           mergerule.Facts
	VerifiedSHA    string
	IntegrationSHA string
	Repository     string
	Base           string
	Item           string
	Position       int
}

// RuleVerdict is the merge-rule seam's answer. Deny names the rule in
// Reason — a refusal nobody can trace to the rule that made it is a
// mystery, not a gate.
type RuleVerdict struct {
	Allow  bool
	Reason string
}

// MergeRule is S57's merge-rule seam (MQ12.2's cross-slice request): the
// rule consulted AFTER mergeGate and BEFORE the forge call, never instead
// of it. A nil seam ⇒ only mergeGate applies, and the evidence says
// `merge_rule: "none-configured"` — the absence is recorded, not assumed
// green. An ERROR refuses: unverifiable is not green before something
// irreversible (the same shape as the gatherer's transport refusals).
type MergeRule interface {
	Allow(ctx context.Context, facts MergeFacts) (RuleVerdict, error)
}

// Grants is S56's grant seam for the one irreversible action in the
// queue. MergeAllowed answers "does a person's standing merge grant cover
// this repository", naming the covering grant's revision in rev so the
// sentence can cite it. The seam is consulted ONLY for forge.land, and a
// nil seam is deny-leaning: with no grants wired, land is reachable only
// through the attended ask — G14 unchanged, and `allowOutward` still never
// grants irreversible (class.go's Resolve owns that sentence, untouched).
type Grants interface {
	MergeAllowed(componentRoot string) (allowed bool, rev string)
}

// BaseFetcher is the post-guard's other instrument: the fetch that makes
// `origin/<base>` mean NOW instead of "whenever somebody last fetched".
// Required for land exactly as MergeProbe is: an irreversible action whose
// guard cannot see the current base does not run.
type BaseFetcher interface {
	FetchBase(ctx context.Context, dir, base string) error
}

// QueueEnqueuer is the queue service as the enqueue action sees it — one
// method, no store types (I2 keeps SQL in the store; this package imports
// neither the store nor the merge-queue core, only their answer).
type QueueEnqueuer interface {
	Enqueue(ctx context.Context, in QueueEnqueueInput) (QueueEnqueueResult, error)
}

// QueueEnqueueInput carries the identities the queue rows are keyed on.
// Every field arrives from CODE — the loop's pinned branch shape, the
// loop's own slug, the loop's pinned attendedness — never a model, never
// an ask (MQ13: "the loop pin is the only input").
type QueueEnqueueInput struct {
	ComponentRoot string
	Branch        string
	BaseBranch    string
	LoopSlug      string
	Attended      bool
	ActorKind     string // "blueprint" for the action path (MQ14.1)
	ActorID       string // the loop slug — who asked, from the pin
}

// QueueEnqueueResult is the queue row's own answer.
type QueueEnqueueResult struct {
	ItemID   string
	Status   string
	Position int
	Reused   bool
}

// --- forge.openLandingPR (outward) ------------------------------------------

// LandingArgs is the landing PR's identity: the item, its branches, the
// base, the queue position, and the loop's summary as marked prose.
type LandingArgs struct {
	Dir               string // the component root — where the remote is asked
	Item              string
	IntegrationBranch string
	HeadBranch        string
	BaseBranch        string
	Position          int
	Summary           string // the loop's own paragraph — never a fact slot
}

// LandingTitle is the fixed title. Code builds it; it says what the PR IS.
func LandingTitle(a LandingArgs) string {
	return fmt.Sprintf("[merge-queue] %s → %s", a.Item, a.BaseBranch)
}

// LandingBody is the fixed template plus the marked summary paragraph —
// the L3 split again in one body: identifiers from code above, the loop's
// own prose introduced AS PROSE below, so a reader knows which words came
// from a model and which are the queue's facts.
func LandingBody(a LandingArgs) string {
	body := fmt.Sprintf("Merge-queue item %s — landing `%s` onto `%s`.\n\n"+
		"- item: %s\n- integration ref: %s\n- carried branch: %s\n- base: %s\n- queue position: %d\n",
		a.Item, a.IntegrationBranch, a.BaseBranch, a.Item, a.IntegrationBranch, a.HeadBranch, a.BaseBranch, a.Position)
	if a.Summary != "" {
		body += "\nThe rest of this body is the loop's own summary — prose, not a queue fact:\n\n" + a.Summary + "\n"
	}
	return body
}

// OpenLandingPR opens — or REUSES, keyed on the head ref, exactly as
// OpenPR does — the PR that lands one queue item: head is the item's
// integration branch, base is the item's base branch. The head must exist
// ON THE REMOTE: an integration ref nobody has published is the phantom-ref
// failure (O8) at the forge boundary, and the refusal names
// git.publishIntegration as the way forward — not commitAndPush, which is
// not a step of a landing loop.
func (a *ForgeActions) OpenLandingPR(ctx context.Context, args LandingArgs) (ForgeResult, error) {
	if a.Forge == nil {
		return ForgeResult{}, fmt.Errorf("forge.openLandingPR: no forge wired")
	}
	if a.Git == nil {
		return ForgeResult{}, fmt.Errorf("forge.openLandingPR: no branch probe wired")
	}
	if args.BaseBranch == "" {
		return ForgeResult{}, fmt.Errorf("forge.openLandingPR refuses: the base ref is required (L26) — a PR that cannot say what it merges into cannot be trusted at merge time")
	}
	head := args.IntegrationBranch
	found, err := a.Forge.FindOpenPR(ctx, head)
	if err != nil {
		return ForgeResult{}, classify(err)
	}
	if found != nil {
		a.record(Evidence{Action: ForgeOpenLandingPR, Key: "pr_number", Value: strconv.Itoa(found.Number)})
		return ForgeResult{Action: ForgeOpenLandingPR, Number: found.Number, Reused: true}, nil
	}
	present, err := a.Git.BranchExistsRemote(ctx, args.Dir, head)
	if err != nil {
		return ForgeResult{}, classify(err)
	}
	if !present {
		return ForgeResult{}, &BlockedError{Reason: "head-missing", Detail: fmt.Sprintf(
			"the integration ref %q is not on the remote — run git.publishIntegration first; the queue opens a landing PR only for a ref people can fetch (O8)", head)}
	}
	opened, err := a.Forge.OpenPR(ctx, forge.PRRequest{
		Title: LandingTitle(args),
		Body:  LandingBody(args),
		Head:  head,
		Base:  args.BaseBranch,
	})
	if err != nil {
		if isConflict(err) {
			// Raced to open — ONE re-search, never a retry loop (OpenPR's
			// own pattern, copied because sharing it would mean editing
			// OpenPR, and T6's rule is that forge code stays untouched.
			if again, serr := a.Forge.FindOpenPR(ctx, head); serr == nil && again != nil {
				a.record(Evidence{Action: ForgeOpenLandingPR, Key: "pr_number", Value: strconv.Itoa(again.Number)})
				return ForgeResult{Action: ForgeOpenLandingPR, Number: again.Number, Reused: true}, nil
			}
		}
		return ForgeResult{}, classify(err)
	}
	a.record(Evidence{Action: ForgeOpenLandingPR, Key: "pr_number", Value: strconv.Itoa(opened.Number)})
	return ForgeResult{Action: ForgeOpenLandingPR, Number: opened.Number}, nil
}

// --- forge.land (irreversible) ----------------------------------------------

// LandDeniedError is the DENIAL typed (S60 T7): the live S56 re-check at
// effect time said no. It is not a failure — nothing went wrong, the
// person's standing grant simply does not cover this root any more (or
// never did). The engine maps a typed denial to the block kind
// `action-denied`, which is what lets the merge-queue reactor end the item
// `denied` with the sentence preserved, instead of re-asking the person a
// land the authority has already refused. A transport or protection
// failure stays its own error — the reactor must not read "try later" as
// "refused forever".
type LandDeniedError struct{ Detail string }

func (e *LandDeniedError) Error() string { return e.Detail }

// LandArgs is the landing decision's input: which PR, into which base,
// bound to which verified SHA, in which tree the ancestry question gets
// asked. VerifiedSHA/IntegrationSHA arrive from EVIDENCE (MQ11/MQ12), the
// branches from the loop's pinned `with`, the method from the repository's
// land_method config read by CODE — never from an ask, never from a model.
type LandArgs struct {
	Dir               string // where fetch + IsAncestor are asked (the integration worktree)
	PRNumber          int
	Item              string
	IntegrationBranch string
	BaseBranch        string
	VerifiedSHA       string
	IntegrationSHA    string
	ComponentRoot     string
	Position          int
	Method            forge.MergeMethod // the configured land_method; empty ⇒ squash (OD4)
	Loop              string            // gate scope (the loop slug)
	AeonReview        string            // VG9's token, evidence-only, same as merge
}

// LandResult is the merge's report. Fields carries the gate's and the
// seam's audit trail exactly as merge's does.
type LandResult struct {
	PR            int
	SHA           string
	AlreadyMerged bool
	Fields        map[string]string
}

// Land is MQ12's five steps, run after the class gate has already done its
// work upstream (attended ask, or S56's grant — never allowOutward):
//
//  1. the PR's base must be the ITEM's base branch and its head must BE
//     the item's integration branch — D5's reasoning with the queue's
//     base (F7 generalized, not merge's feature-branch assertion);
//  2. mergeGate under the effective rule — reused VERBATIM (F8);
//  3. the merge-rule seam, after the gate and before the call;
//  4. the verified-SHA binding: PR head, verified SHA and integration SHA
//     are one SHA or land refuses `head-moved` (MQ11) — green for A can
//     never land B;
//  5. Forge.MergePR with the configured method (default squash, OD4),
//     then the post-guard: fetch, and IsAncestor(mergeSHA, origin/base)
//     or PhantomMergeError (F7).
//
// An already-merged PR answers success with the recorded SHA — never merge
// twice. A protection refusal is honest failure: no workaround, no second
// method, no second anything (N4).
func (a *ForgeActions) Land(ctx context.Context, args LandArgs) (LandResult, error) {
	if a.Forge == nil {
		return LandResult{}, fmt.Errorf("forge.land: no forge wired")
	}
	if a.Merge == nil || a.Fetch == nil {
		// Checked BEFORE anything: an irreversible action whose guard
		// cannot run does not run (the same rule forge.merge applies to
		// its probe — "Merge is REQUIRED for the irreversible action").
		return LandResult{}, fmt.Errorf("forge.land refuses: the post-merge guard needs its instruments (MergeProbe + BaseFetcher) — an irreversible action that cannot verify the merge does not run")
	}
	if args.PRNumber <= 0 {
		return LandResult{}, fmt.Errorf("forge.land: PR number must be positive — it comes from openLandingPR's evidence (Q7)")
	}
	if args.BaseBranch == "" || args.IntegrationBranch == "" {
		return LandResult{}, fmt.Errorf("forge.land: the item's base and integration branch are required — the queue lands an item, not a guess")
	}
	// The S56 door, live. nil seam ⇒ nothing to consult (an unattended
	// land never gets here — Resolve refuses it; an attended one passed
	// the person). A wired seam re-answers at effect time, which is the
	// whole point: a grant revoked since the ask DENIES here.
	if a.Grants != nil {
		allowed, rev := a.Grants.MergeAllowed(args.ComponentRoot)
		if !allowed {
			return LandResult{}, &LandDeniedError{Detail: fmt.Sprintf("forge.land denied — no standing merge grant covers %s (grants are a person's act on the socket; S56)", args.ComponentRoot)}
		}
		if rev != "" {
			// Recorded, not decoration: the sentence and the evidence both
			// say WHICH grant let this through.
			defer func(rev string) {
				a.record(Evidence{Action: ForgeLand, Key: "merge_grant_rev", Value: rev})
			}(rev)
		}
	}

	pr, err := a.Forge.ReadPR(ctx, args.PRNumber)
	if err != nil {
		return LandResult{}, classify(err)
	}
	if pr.Merged {
		a.record(Evidence{Action: ForgeLand, Key: "merge_sha", Value: pr.MergeSHA})
		return LandResult{PR: args.PRNumber, SHA: pr.MergeSHA, AlreadyMerged: true,
			Fields: map[string]string{"already_merged": "true", "merge_rule": "none-consulted-already-merged"}}, nil
	}

	// Step 1 — the right base, and the head that IS the item.
	if pr.BaseRef == "" {
		return LandResult{}, forge.NewMsg(forge.KindRefused, fmt.Sprintf(
			"PR #%d has no base ref — refusing to land something that cannot say what it merges into (L26)", args.PRNumber))
	}
	if pr.BaseRef != args.BaseBranch {
		return LandResult{}, forge.NewMsg(forge.KindRefused, fmt.Sprintf(
			"phantom-merge guard: PR #%d is based on %q, not the item's base %q — landing it would put work on a branch nobody is looking at (D5/MQ12.1)",
			args.PRNumber, pr.BaseRef, args.BaseBranch))
	}
	if pr.Head != args.IntegrationBranch {
		return LandResult{}, forge.NewMsg(forge.KindRefused, fmt.Sprintf(
			"PR #%d head %q is not the item's integration branch %q — the queue lands the ref it built, not whatever is on the PR (MQ12.1)",
			args.PRNumber, pr.Head, args.IntegrationBranch))
	}

	// Step 2 — the gate, VERBATIM (F8's reuse is the whole point; the
	// identical refusal text is what TestLandReusesMergeGateVerbatim pins).
	rule, err := a.effectiveRule(ctx)
	if err != nil {
		return LandResult{}, fmt.Errorf("forge.land: the merge rule could not be read — unverifiable is not green before something irreversible: %w", err)
	}
	gateFields, err := a.mergeGate(ctx, GateFactsQuery{
		PR: args.PRNumber, LoopSlug: args.Loop, ComponentRoot: args.ComponentRoot, HeadSHA: pr.HeadSHA,
	}, args.AeonReview, rule)
	if err != nil {
		return LandResult{}, err
	}

	// Step 3 — the merge-rule seam, after the gate, before the call.
	ruleField := "none-configured"
	if a.Rule != nil {
		gf, gerr := GatherGateFacts(ctx, a.Forge, args.PRNumber, pr.HeadSHA)
		if gerr != nil {
			return LandResult{}, gerr // the gatherer's own refusal — verbatim
		}
		v, serr := a.Rule.Allow(ctx, MergeFacts{
			PR: *pr, Gate: gf, VerifiedSHA: args.VerifiedSHA, IntegrationSHA: args.IntegrationSHA,
			Repository: args.ComponentRoot, Base: args.BaseBranch, Item: args.Item, Position: args.Position,
		})
		if serr != nil {
			return LandResult{}, forge.NewMsg(forge.KindRefused, fmt.Sprintf(
				"forge.land refuses: the merge rule could not be consulted — %v (unverifiable is not green before an irreversible call)", serr))
		}
		if !v.Allow {
			return LandResult{}, forge.NewMsg(forge.KindRefused, fmt.Sprintf(
				"forge.land refuses: the merge rule denied — %s", v.Reason))
		}
		ruleField = "allowed"
	}

	// Step 4 — the verified-SHA binding (MQ11). Three names for one SHA:
	// what the forge says the head is, what the last green gate verified,
	// and what the queue integrated. Any disagreement — including a
	// missing link — refuses.
	if pr.HeadSHA == "" {
		return LandResult{}, forge.NewMsg(forge.KindRefused, fmt.Sprintf(
			"forge.land refuses: PR #%d reports no head SHA — the verified-SHA binding (MQ11) cannot be checked, and an uncheckable binding is not a pass", args.PRNumber))
	}
	if args.VerifiedSHA == "" {
		return LandResult{}, forge.NewMsg(forge.KindRefused, fmt.Sprintf(
			"forge.land refuses: %s has no verified_sha in evidence — nothing has verified this item green, and only a verified item is landable (MQ11)", args.Item))
	}
	if pr.HeadSHA != args.VerifiedSHA || args.VerifiedSHA != args.IntegrationSHA {
		return LandResult{}, forge.NewMsg(forge.KindRefused, fmt.Sprintf(
			"head-moved (MQ11): PR head %s, verified %s, integration %s do not agree — a green result for one head can never land another",
			pr.HeadSHA, args.VerifiedSHA, args.IntegrationSHA))
	}

	// Step 5 — the merge, then the post-guard.
	method := args.Method
	if method == "" {
		method = forge.MergeSquash // OD4: the default is the configured one; nothing else exists here
	}
	merged, err := a.Forge.MergePR(ctx, args.PRNumber, method)
	if err != nil {
		// A protection refusal says no. There is no second method to try
		// (N4) and no retry that would make one.
		return LandResult{}, classify(err)
	}
	// The instruments were checked at the door; the guard runs here,
	// after the forge said merged.
	if err := a.Fetch.FetchBase(ctx, args.Dir, args.BaseBranch); err != nil {
		return LandResult{}, fmt.Errorf("forge.land cannot verify the merge landed: fetching %s: %w", args.BaseBranch, err)
	}
	baseRef := "origin/" + args.BaseBranch
	ancestor, err := a.Merge.IsAncestor(ctx, args.Dir, merged.SHA, baseRef)
	if err != nil {
		return LandResult{}, fmt.Errorf("forge.land cannot verify the merge landed (D5): %w", err)
	}
	if !ancestor {
		return LandResult{}, &PhantomMergeError{PR: args.PRNumber, SHA: merged.SHA, Base: baseRef}
	}

	a.record(Evidence{Action: ForgeLand, Key: "merge_sha", Value: merged.SHA})
	fields := make(map[string]string, len(gateFields)+3)
	for k, v := range gateFields {
		fields[k] = v
	}
	fields["merge_rule"] = ruleField
	fields["verified_sha"] = args.VerifiedSHA
	return LandResult{PR: args.PRNumber, SHA: merged.SHA, Fields: fields}, nil
}

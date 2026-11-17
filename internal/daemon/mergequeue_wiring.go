package daemon

// mergequeue_wiring.go — S60 T7's seams between the action layer and the
// queue: the three Forge instruments forge.land consults (the merge-rule
// seam MQ12.2 asked S57 for, the S56 grants floor, and the base fetch the
// post-guard requires), and the queue.enqueue door's enqueuer. All of it
// is adaptation — the decisions live in mergequeue, the writes in store,
// the rules in mergerule; this file only answers "which live object
// serves this seam for THIS loop root".

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/mergerule"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/workspace"
)

// mqForgeOverride is the test seam for the reactor's forge READS (ReadPR,
// gate facts). Production answers forgeClient — the root's origin-built
// identity; a test with a fake forge standing where GitHub would be needs
// the reads to come from that witness, and the fake cannot ride a real
// binary (autonomy_demo_walk_test.go's record says so).
var mqForgeOverride func(root string) forge.Forge

func (d *Daemon) mqForge(root string) forge.Forge {
	if mqForgeOverride != nil {
		return mqForgeOverride(root)
	}
	return d.forgeClient(root)
}

// mqRuleAdapter serves the MQ12.2 merge-rule seam for one loop root: the
// same effective rule merge's consults (S57), evaluated over the gate
// facts Land already gathered. An unreadable rule ERRORS — unverifiable
// refuses an irreversible call; that is the seam's contract (never a
// silent allow, never a silent deny).
type mqRuleAdapter struct {
	d    *Daemon
	root string
}

func (r mqRuleAdapter) Allow(ctx context.Context, facts action.MergeFacts) (action.RuleVerdict, error) {
	if r.d.store == nil {
		return action.RuleVerdict{}, fmt.Errorf("no store wired — the merge rule cannot be read")
	}
	comp, err := r.d.store.ComponentForRoot(r.root)
	if err != nil {
		return action.RuleVerdict{}, fmt.Errorf("component for %s unreadable: %w", r.root, err)
	}
	rule, source, err := r.d.store.EffectiveMergeRule(comp.ID)
	if err != nil {
		return action.RuleVerdict{}, fmt.Errorf("effective merge rule unreadable: %w", err)
	}
	v := mergerule.Evaluate(rule, facts.Gate)
	if !v.Allowed {
		reasons := make([]string, 0, len(v.Refusals))
		for _, rf := range v.Refusals {
			reasons = append(reasons, fmt.Sprintf("%s: %s", rf.Code, rf.Sentence))
		}
		return action.RuleVerdict{Allow: false,
			Reason: fmt.Sprintf("rule %q — %s", source, strings.Join(reasons, "; "))}, nil
	}
	return action.RuleVerdict{Allow: true}, nil
}

// mqGrantsAdapter serves the S56 grants floor for forge.land: the root's
// LIVE grant read (the floor re-checked at effect time — a grant revoked
// since the pin denies here). The revision rides so the evidence cites
// which grant let the merge through.
type mqGrantsAdapter struct{ d *Daemon }

func (g mqGrantsAdapter) MergeAllowed(componentRoot string) (bool, string) {
	pin, err := g.d.resolveAutonomy(context.Background(), autonomy.OriginEvent, componentRoot, nil)
	if err != nil || pin == nil {
		return false, ""
	}
	return mqPinAllowsLand(pin)
}

// mqPinAllowsLand reads the one subject the queue lands on out of a pin's
// allowed set, keeping the pin's revision for the citation.
func mqPinAllowsLand(pin *store.AutonomyPin) (bool, string) {
	var entries []pinEntry
	if err := json.Unmarshal([]byte(pin.Allowed), &entries); err != nil {
		return false, ""
	}
	for _, e := range entries {
		if e.Subject == "forge.land" {
			return true, strconv.FormatInt(pin.Rev, 10)
		}
	}
	return false, ""
}

// mqQueueEnqueuer is queue.enqueue's door into the queue (MQ14.1: the
// loop pin is the only input — every identity arrives from CODE through
// the action, the actor is the loop's blueprint kind and its slug, and no
// model string reaches a queue row). Repository validation happens here
// because the validator deliberately reads no git and no database
// (T3's design): mqRepoFacts is the gathered answer to its sheet.
type mqQueueEnqueuer struct{ d *Daemon }

func (e mqQueueEnqueuer) Enqueue(ctx context.Context, in action.QueueEnqueueInput) (action.QueueEnqueueResult, error) {
	if e.d.store == nil {
		return action.QueueEnqueueResult{}, fmt.Errorf("merge queue: no store wired")
	}
	root := in.ComponentRoot
	facts, headSHA, baseSHA, err := e.d.mqRepoFacts(ctx, root, in.Branch, in.BaseBranch)
	if err != nil {
		return action.QueueEnqueueResult{}, err
	}
	base := in.BaseBranch
	if base == "" && len(facts.KnownBases) > 0 {
		// MQ15's resolution order is the caller's job and the first
		// known base IS the effective one (the validator's own words).
		base = facts.KnownBases[0]
	}
	// The reuse probe the RESULT answers (the service itself is
	// idempotent — this only names the reuse for the evidence).
	reused := false
	if open, lerr := e.d.store.ListMergeItems(store.MergeFilter{ComponentRoot: root, Statuses: store.MergeOpenStatusSet}); lerr == nil {
		for _, g := range open {
			if g.Branch == in.Branch {
				reused = true
				break
			}
		}
	}
	svc := mergequeue.NewService(e.d.store)
	m, err := svc.Enqueue(
		mergequeue.EnqueueRequest{ComponentRoot: root, Branch: in.Branch, BaseBranch: base, Attended: in.Attended},
		facts,
		store.NewMergeItem{
			ComponentRoot: root, BaseBranch: base, Branch: in.Branch, LoopSlug: in.LoopSlug,
			HeadSHA: headSHA, BaseSHA: baseSHA, Attended: in.Attended,
			EnqueuedByKind: in.ActorKind, EnqueuedByID: in.ActorID,
		},
		store.MergeActor{Kind: in.ActorKind, ID: in.ActorID},
	)
	if err != nil {
		return action.QueueEnqueueResult{}, err
	}
	return action.QueueEnqueueResult{ItemID: m.ID, Status: m.Status, Position: int(m.Position), Reused: reused}, nil
}

// mqRepoFacts gathers the validator's whole question sheet plus the two
// SHAs the row records, from reads only. A root git cannot run in returns
// the error — an enqueue that cannot be checked does not happen; a root
// git CAN run in but which is not a primary component answers with the
// facts that make the validator refuse by name. (T9: the body moved into
// the exported MergeQueueRepoFacts so the desk seam — which runs in EITHER
// process, daemon or CLI direct door — gathers facts through the SAME
// reads; two gatherers would be two truths. The daemon wrapper keeps this
// method's exact behaviour: its own store, its own action-built client.)
func (d *Daemon) mqRepoFacts(ctx context.Context, root, branch, base string) (mergequeue.RepoFacts, string, string, error) {
	return MergeQueueRepoFacts(ctx, d.store, d.mqGitClient(root), root, branch, base)
}

// MergeQueueRepoFacts is mqRepoFacts's body, exported for S60 T9's proposal
// seam, which must answer the validator's sheet from BOTH doors — the
// daemon's commit and the CLI's direct-door commit — without a second
// gatherer to drift (MQ14: one validator, and one sheet for it). The git
// client is the CALLER's (the daemon passes its action-built one; the CLI
// seam builds a daemon-git client over its own process environment — EV10's
// allowlist is the point of the constructor, not a private convenience).
// A nil store or client is the named error, never a half-sheet.
func MergeQueueRepoFacts(ctx context.Context, st *store.Store, gitC *git.Client, root, branch, base string) (mergequeue.RepoFacts, string, string, error) {
	f := mergequeue.RepoFacts{}
	if st == nil {
		return f, "", "", fmt.Errorf("merge queue: no store wired")
	}
	if _, err := st.ComponentForRoot(root); err == nil {
		f.HasPrimaryRoot = true
	}
	if gitC == nil {
		return f, "", "", fmt.Errorf("merge queue: no git client for %s", root)
	}
	headSHA, baseSHA := "", ""
	if eff, err := st.EffectiveForRoot(root); err == nil {
		if b := strings.TrimSpace(eff.DefaultBranch.Value); b != "" {
			f.KnownBases = append(f.KnownBases, b)
		}
	}
	if b := workspace.OriginDefaultBranch(ctx, root); b != "" && !mqContains(f.KnownBases, b) {
		f.KnownBases = append(f.KnownBases, b)
	}
	if _, err := gitC.Run(ctx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		f.BranchLocal = true
	}
	if out, err := gitC.Run(ctx, root, "ls-remote", "--heads", "origin", branch); err == nil {
		if line := strings.TrimSpace(out); line != "" {
			f.BranchRemote = true
			headSHA = strings.Fields(line)[0]
		}
	}
	if out, err := gitC.Run(ctx, root, "rev-parse", branch); err == nil {
		if sha := strings.TrimSpace(out); sha != "" && !strings.HasPrefix(sha, "fatal") {
			headSHA = sha
		}
	}
	if base != "" {
		if out, err := gitC.Run(ctx, root, "rev-parse", "refs/remotes/origin/"+base); err == nil {
			if sha := strings.TrimSpace(out); sha != "" {
				baseSHA = sha
			}
		}
	}
	if open, err := st.ListMergeItems(store.MergeFilter{ComponentRoot: root, Statuses: store.MergeOpenStatusSet}); err == nil {
		for _, g := range open {
			f.OpenBranches = append(f.OpenBranches, g.Branch)
		}
	}
	return f, headSHA, baseSHA, nil
}

// mqContains is the membership check KnownBases dedup needs (a component
// default that IS origin's default is listed once).
func mqContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

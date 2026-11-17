package mergequeue

import (
	"fmt"
	"regexp"
)

// validate.go — the ONE `ValidateEnqueue` behind all three enqueue paths
// (MQ14): the `queue.enqueue` blueprint action, the person's socket method,
// and the proposal's commit. Create/commit parity is not a convention —
// T3's test calls this function from all three adapters, and T9's commit
// path reuses the same call.
//
// Refusals are `*Refusal` values (DB19's shape: discriminated results; the
// error interface is implemented for convenience so a caller may treat one
// as an error). Sentences are verbatim where the design quotes them.

// EnqueueRequest is what a caller ASKS to enqueue, before any facts.
type EnqueueRequest struct {
	ComponentRoot string
	Branch        string
	BaseBranch    string // "" = resolve it from the repo facts (MQ15's order is the caller's job)
	Attended      bool
}

// RepoFacts is the validator's view of the repository, gathered by the
// caller: the validator reads no git and no database.
type RepoFacts struct {
	// HasPrimaryRoot is the "repository without a primary root" check —
	// the component exists and answers as the queue's key.
	HasPrimaryRoot bool
	// KnownBases are the base branches known for the repository (the loop
	// pin, the component's default branch, refs/remotes/origin/HEAD — the
	// MQ15 resolution order, first = effective). Empty means NONE is known.
	KnownBases []string
	// BranchLocal / BranchRemote: the branch exists as a local ref / on the
	// remote (MQ14's "does not exist locally or on the remote").
	BranchLocal  bool
	BranchRemote bool
	// OpenBranches carries the branch when another open queue item already
	// holds it. The SERVICE answers idempotent-reuse before reaching the
	// validator; the validator's refusal is then the last line behind any
	// caller that skipped that step.
	OpenBranches []string
}

// Refusal is a discriminated refusal: Kind is machine-readable, Reason the
// person's sentence. It implements error.
type Refusal struct {
	Kind   string
	Reason string
}

func (r *Refusal) Error() string { return "refused: " + r.Kind + " · " + r.Reason }

// branchRef is the identity rule (MQ15): the store-side identity regex
// per slash-separated component — lowercase, dot/dash/underscore inside,
// no leading or trailing decoration, no empty component.
var branchRef = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*$`)

// ValidateEnqueue runs the MQ14/MQ15 checks in this order: the repository,
// the branch's shape, its existence, the branch-vs-base equality, the base's
// knownness, and a duplicate open item last (idempotent callers never see
// it). Every refusal is a *Refusal.
func ValidateEnqueue(req EnqueueRequest, facts RepoFacts) error {
	if req.ComponentRoot == "" {
		return &Refusal{Kind: "no-root", Reason: "an enqueue names the repository it queues into — componentRoot is empty"}
	}
	if !facts.HasPrimaryRoot {
		return &Refusal{Kind: "no-primary-root",
			Reason: fmt.Sprintf("no primary component_root answers for %s — the queue is per-repository and this one is not known to Aeon", req.ComponentRoot)}
	}
	if req.Branch == "" {
		return &Refusal{Kind: "no-branch", Reason: "an enqueue names the branch to land — branch is empty"}
	}
	if !branchRef.MatchString(req.Branch) {
		return &Refusal{Kind: "bad-branch-name",
			Reason: fmt.Sprintf("branch %q is not a valid name: lowercase identity components separated by '/' (^[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*$)", req.Branch)}
	}
	if !facts.BranchLocal && !facts.BranchRemote {
		return &Refusal{Kind: "branch-missing",
			Reason: fmt.Sprintf("branch %q exists neither locally nor on the remote — nothing can be landed from a branch that is not there", req.Branch)}
	}
	base := req.BaseBranch
	if base == "" && len(facts.KnownBases) > 0 {
		base = facts.KnownBases[0] // the MQ15 resolution order was the caller's; first = effective
	}
	if base == "" {
		return &Refusal{Kind: "base-unknown",
			Reason: "no base branch is known for this repository — set its default branch (`aeon component update <id> --default-branch main`)"}
	}
	if base == req.Branch {
		return &Refusal{Kind: "branch-is-base",
			Reason: fmt.Sprintf("branch %q IS the base branch %q — a branch cannot land into itself", req.Branch, base)}
	}
	if !containsString(facts.KnownBases, base) {
		return &Refusal{Kind: "base-unknown",
			Reason: fmt.Sprintf("base branch %q is not known for this repository (known: %v) — the loop pin, the component's default branch, or origin/HEAD decide a base", base, facts.KnownBases)}
	}
	if containsString(facts.OpenBranches, req.Branch) {
		return &Refusal{Kind: "duplicate-open",
			Reason: fmt.Sprintf("branch %q already has an open item in the queue for %s — one open item per branch is the queue's shape", req.Branch, req.ComponentRoot)}
	}
	return nil
}

func containsString(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// ResolveBase answers the effective base for a request the way the
// validator uses it (request first, then the caller-resolved order). The
// socket method and the blueprint action share it so "which base" is never
// decided twice.
func ResolveBase(req EnqueueRequest, facts RepoFacts) string {
	if req.BaseBranch != "" {
		return req.BaseBranch
	}
	if len(facts.KnownBases) > 0 {
		return facts.KnownBases[0]
	}
	return ""
}

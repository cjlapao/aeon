package daemon

// actions.go — the daemon's half of S16 T10. The registry is closed and
// the loop package executes; this is where the execution instruments are
// stood up per loop.
//
// NO SLICE TASK OWNED THIS FILE. The T12 walk found the gap: T10 wired
// the loop's execute seam and T11 wired the forge client, and nobody
// wired the seam to the client — a daemon that blocks every code action
// with `action-runtime-missing` is honest but not built. The walk made
// the call; this file is the record.

import (
	"context"
	"strings"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/mergerule"
	"github.com/cjlapao/aeon/internal/store"
)

// actionServices resolves the instruments from the LOOP'S OWN repository
// (root): the forge identity is the origin remote's owner/repo, so two
// loops in two repositories act as two identities on two repos — the
// daemon process owns no forge identity of its own.
//
// A repository whose origin is not GitHub gets Git and Audit but a nil
// Forge: outward forge actions then refuse naming the missing service
// (registry.Dispatch's svc-nil arm), because pretending is the failure
// mode this whole slice was built against.
func (d *Daemon) actionServices(root string) *action.Services {
	// S59 T5 (EV10): the daemon's own git gets the hardened, allowlisted
	// environment — ambient GIT_* stripped, the prompt guard on, and none of
	// the daemon's provider keys or forge token, which a code action has no
	// use for and a hook that dumps its environment would have shown.
	gitC := git.NewClient(daemonGitEnv())
	svc := &action.Services{
		Git: &action.GitActions{Git: gitC},
		Audit: func(context.Context) (int, error) {
			return d.store.CountActionIntents()
		},
	}
	if d.store != nil {
		// verify.run (S57 T6): the engine's gate runner, over the
		// store's seams (verify_wiring.go). Storeless mount: the
		// registry's Dispatch answers "no verify service wired" —
		// the honest state, same as the forge's.
		svc.Verify = d.verifyService(gitC)
	}
	if f := d.forgeClient(root); f != nil {
		svc.Forge = &action.ForgeActions{
			Forge:   f,
			Git:     gitC, // BranchProbe — the remote branch check is origin's truth, read through git
			Merge:   gitC, // MergeProbe — the ancestor assertion is a local git fact (T6)
			Intents: storeIntents{s: d.store},
		}
		if d.store != nil {
			// The merge rule (VG10): the gate's facts and the
			// governing rule come from the store, per the loop's
			// component (verify_wiring.go). Unwired, the gate
			// takes the baseline — today's gate, byte for byte.
			svc.Forge.RuleSource = func(ctx context.Context) (mergerule.Rule, error) {
				comp, err := d.store.ComponentForRoot(root)
				if err != nil {
					return mergerule.Rule{}, err
				}
				rule, _, err := d.store.EffectiveMergeRule(comp.ID)
				return rule, err
			}
			svc.Forge.GateFacts = d.fillGateFacts
			// S60 T6/T7's forge.land instruments (MQ12): the merge-rule
			// seam (consulted AFTER mergeGate, never instead of it), the
			// S56 grants floor (a revoked grant denies at effect time),
			// and the base fetch the post-guard requires — Fetch rides
			// the same git client (GitActions.FetchBase).
			svc.Forge.Rule = mqRuleAdapter{d: d, root: root}
			svc.Forge.Grants = mqGrantsAdapter{d: d}
			svc.Forge.Fetch = svc.Git
		}
	}
	if d.store != nil {
		// queue.enqueue (MQ14.1): the seam, not the service — the door's
		// identities arrive from the loop's pin through the action layer
		// and reach the queue through the ONE service (T3's).
		svc.Queue = &action.QueueActions{Enq: mqQueueEnqueuer{d: d}}
	}
	return svc
}

// githubOrigin reads remote.origin.url and extracts owner/repo for
// GitHub forms (https and ssh), rejecting everything else — a GitLab
// origin is not a GitHub identity, and "it looked like one" is exactly
// the guessing this slice refuses.
func githubOrigin(dir string) (owner, repo string, ok bool) {
	if dir == "" {
		return "", "", false
	}
	out, err := git.NewClient(daemonGitEnv()).Run(context.Background(), dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", "", false
	}
	u := strings.TrimSpace(out)
	const host = "github.com"
	if strings.HasPrefix(u, "git@"+host+":") {
		u = strings.TrimPrefix(u, "git@"+host+":")
	} else if strings.HasPrefix(u, "https://"+host+"/") {
		u = strings.TrimPrefix(u, "https://"+host+"/")
	} else if strings.HasPrefix(u, "ssh://git@"+host+"/") {
		u = strings.TrimPrefix(u, "ssh://git@"+host+"/")
	} else {
		return "", "", false
	}
	u = strings.TrimSuffix(u, ".git")
	parts := strings.Split(u, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// storeIntents adapts the ONE intent table (D7) to the action port. The
// port speaks the action language; the store keeps the single schema —
// recovery has one path because there is one table and one adapter.
type storeIntents struct {
	s *store.Store
}

func (i storeIntents) BeginIntent(ctx context.Context, in action.Intent) error {
	return i.s.BeginActionIntent(ctx, store.ActionIntent{
		LoopSlug: in.Loop, StepID: in.StepID, Attempt: in.Attempt,
		Action: in.Action, PR: in.PR, PreReviewID: in.PreReviewID,
	})
}

func (i storeIntents) RecordIntent(ctx context.Context, in action.Intent, reviewID int64) error {
	return i.s.RecordActionReview(ctx, in.StepID, in.Attempt, in.Action, in.PR, reviewID)
}

func (i storeIntents) OpenIntent(ctx context.Context, pr int, stepID int64, attempt int) (*action.Intent, error) {
	// The port's OpenIntent is the RECEIPT lookup, and only one action
	// keeps receipts (the receipt-first exception, asserted by count at
	// T10) — so the action name is not a parameter and cannot drift.
	ai, err := i.s.OpenActionIntent(ctx, stepID, attempt, action.ForgePostReview, pr)
	if err != nil {
		return nil, err
	}
	if ai == nil {
		return nil, nil
	}
	return &action.Intent{Loop: ai.LoopSlug, StepID: ai.StepID, Attempt: ai.Attempt,
		Action: ai.Action, PR: ai.PR, PreReviewID: ai.PreReviewID, ReviewID: ai.ReviewID}, nil
}

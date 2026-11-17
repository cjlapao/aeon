// mergequeue_proposal_seams.go — S60 T9: the mergequeue_enqueue proposal
// kind, as ONE seam map that BOTH doors run.
//
// The law the whole file serves is MQ14's: ONE validator. The shape is the
// desk's (desk.ValidateMergeQueueEnqueue — which repository, which branch,
// why, nothing else); the facts are daemon.MergeQueueRepoFacts gathered
// through the SAME reads the daemon's own enqueue path uses, answered to
// mergequeue.ValidateEnqueue. The arm (daemon), this seam's Validate (the
// create door and the commit door alike, TD3) and the socket's
// mergequeue.enqueue all run that one function over that one sheet — a
// payload the socket refuses is refused by the tool with the same words,
// and a queue that moved between the ask and the commit refuses at the
// second run rather than landing what nobody approved.
//
// Apply is the queue's OWN Service.Enqueue — the same idempotent door the
// socket path enters — with three stamps the ASK fixes and only the ask
// fixes (MQ14.3): attended = FALSE (an agent's committed ask never buys a
// human watching the landing), enqueued_by = {proposal, <id>} (T2's CHECK
// admitted that kind for exactly this row — the queue's history names the
// proposal that produced it), and the enqueue EVENT carried by the
// COMMITTING PERSON (the event ledger's closed set has no 'proposal' actor
// — a person committed, and the event says who; the deviation from MQ14.3's
// one-word "actor {proposal, id}" is recorded in T9's Notes and it moves
// nothing: the row is proposal-stamped, the audit is person-stamped).
//
// There is deliberately no seam for a mergequeue_land, _retry, _reorder or
// _settings kind — the names do not exist as kinds, T8's guards refuse
// them by word, and this file is where they would have arrived if a later
// change forgot which side of the desk an agent may sit on.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/store"
)

// mergeQueueProposalFacts decodes a VALID payload (both seam halves run the
// desk validator first, so by the time either reaches this the shape is
// proven) and answers the validator's sheet over the live store and git.
func mergeQueueProposalFacts(ctx context.Context, st *store.Store, p desk.MergeQueueEnqueueProposal) (mergequeue.RepoFacts, string, string, string, error) {
	base := "" // MQ15: the ask proposes NO base; the resolution order below picks one
	gitC := git.NewClient(execenv.GitEnv(execenv.DaemonEnv{Lookup: os.Getenv}, execenv.GitEnvDaemon))
	facts, headSHA, baseSHA, err := daemon.MergeQueueRepoFacts(ctx, st, gitC, p.ComponentRoot, p.Branch, base)
	return facts, base, headSHA, baseSHA, err
}

// resolveMergeQueueBase is MQ15's order applied to the gathered sheet: the
// first known base IS the effective one (the validator's own words, the
// same resolution mqQueueEnqueuer applies — one order, spelled twice only
// because neither site can reach the other's receiver; the resolver here is
// exercised beside the validator in TestEnqueueValidatorParityAcrossDoors).
func resolveMergeQueueBase(facts mergequeue.RepoFacts) string {
	if len(facts.KnownBases) > 0 {
		return facts.KnownBases[0]
	}
	return ""
}

// seamValidateMergeQueueEnqueue is BOTH doors' Validate (TD3): the desk's
// shape, then mergequeue.ValidateEnqueue over the gathered sheet — the same
// function the socket door runs. A root git cannot run in errors (an ask
// that cannot be checked does not get filed); a sheet the validator refuses
// travels verbatim.
func seamValidateMergeQueueEnqueue(ctx context.Context, payload []byte) error {
	if err := desk.ValidateMergeQueueEnqueue(ctx, payload); err != nil {
		return err
	}
	var p desk.MergeQueueEnqueueProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return err // the validator above just refused this shape; unreachable, named anyway
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	facts, base, _, _, err := mergeQueueProposalFacts(ctx, st, p)
	if err != nil {
		return err
	}
	if base == "" {
		base = resolveMergeQueueBase(facts)
	}
	if r := mergequeue.ValidateEnqueue(mergequeue.EnqueueRequest{
		ComponentRoot: p.ComponentRoot, Branch: p.Branch, BaseBranch: base, Attended: false,
	}, facts); r != nil {
		return r
	}
	return nil
}

// seamAlreadyAppliedMergeQueueEnqueue is the crash-retry answer: the branch
// is ALREADY an open item for that root — Service.Enqueue itself reuses the
// open row idempotently, and naming it here lets a half-finished commit
// complete its own effect instead of re-validating into a duplicate-open
// refusal it caused itself. Any unreadable anything answers false, letting
// Validate refuse loudly.
func seamAlreadyAppliedMergeQueueEnqueue(ctx context.Context, payload []byte) (bool, error) {
	if err := desk.ValidateMergeQueueEnqueue(ctx, payload); err != nil {
		return false, nil // a broken payload is not "already applied"; validate refuses it
	}
	var p desk.MergeQueueEnqueueProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return false, nil
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return false, nil
	}
	defer st.Close()
	open, err := st.ListMergeItems(store.MergeFilter{ComponentRoot: p.ComponentRoot, Statuses: store.MergeOpenStatusSet})
	if err != nil {
		return false, nil
	}
	for _, g := range open {
		if g.Branch == p.Branch {
			return true, nil
		}
	}
	return false, nil
}

// seamApplyMergeQueueEnqueue lands the committed ask through the queue's
// own door: ValidateEnqueue re-run over FRESH facts (TD3 — the sheet the
// person saw may have moved; duplicate-open, branch-missing and
// base-unknown all refuse here with their own words), then
// Service.Enqueue with attended = false, the row stamped {proposal, <id>}
// and the enqueue event carried by the committing person (MQ14.3, header).
func seamApplyMergeQueueEnqueue(ctx context.Context, w io.Writer, payload []byte) error {
	if err := desk.ValidateMergeQueueEnqueue(ctx, payload); err != nil {
		return err
	}
	dec, ok := desk.DeciderFrom(ctx)
	if !ok || dec.Kind != "user" || dec.ID == "" {
		return errors.New("refused: mergequeue_enqueue's apply needs the committer's identity from the commit door — the enqueue event names a person, and an actor guessed at is the audit gap (S60 T9, MQ14.3)")
	}
	id, ok := desk.ProposalIDFrom(ctx)
	if !ok {
		return errors.New("refused: mergequeue_enqueue's apply needs the proposal's id from the commit door — the queue row is STAMPED with it, and an enqueue that cannot say which ask produced it is broken wiring, not a shortcut (S60 T9)")
	}
	var p desk.MergeQueueEnqueueProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	facts, base, headSHA, baseSHA, err := mergeQueueProposalFacts(ctx, st, p)
	if err != nil {
		return err
	}
	if base == "" {
		base = resolveMergeQueueBase(facts)
	}
	if r := mergequeue.ValidateEnqueue(mergequeue.EnqueueRequest{
		ComponentRoot: p.ComponentRoot, Branch: p.Branch, BaseBranch: base, Attended: false,
	}, facts); r != nil {
		return r
	}
	fmt.Fprintf(w, "Committing mergequeue_enqueue: %s into the queue for %s (ask %d, committed by user %s)\n", p.Branch, p.ComponentRoot, id, dec.ID)
	m, err := mergequeue.NewService(st).Enqueue(
		mergequeue.EnqueueRequest{ComponentRoot: p.ComponentRoot, Branch: p.Branch, BaseBranch: base, Attended: false},
		facts,
		store.NewMergeItem{
			ComponentRoot: p.ComponentRoot, BaseBranch: base, Branch: p.Branch,
			HeadSHA: headSHA, BaseSHA: baseSHA, Attended: false,
			EnqueuedByKind: "proposal", EnqueuedByID: strconv.FormatInt(id, 10),
		},
		store.MergeActor{Kind: "user", ID: dec.ID},
	)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Committed: %s joined the queue as %s (position %d, status %s) — unattended, proposal #%d on the row, the enqueue event carried by user %s\n",
		p.Branch, m.ID, m.Position, m.Status, id, dec.ID)
	return nil
}

// validateMergeQueueEnqueueProposal is the create door's entry (TD3): the
// same registry entry the commit door re-runs, reached from the one call
// site this package keeps for it.
func validateMergeQueueEnqueueProposal(payload []byte) error {
	return validateKindPayload(string(desk.KindMergeQueueEnqueue), payload)
}

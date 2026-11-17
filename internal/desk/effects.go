package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/policy"
)

// PolicyRemovePayload is the `policy_remove` payload: the one id a rule is
// named by. Exported because `cmd/aeon`'s tests and the CLI's own staging
// helpers name it — the alias there keeps that spelling while this stays the
// single definition.
type PolicyRemovePayload struct {
	ID string `json:"id"`
}

// SeedCorpusPayload is the `seed_corpus` payload: the agents to seed, named.
// Never empty — the whole corpus by omission is the thing D4 welded shut.
type SeedCorpusPayload struct {
	Agents []string `json:"agents"`
}

// --- the dispatch the two doors call ------------------------------------------

// ValidateProposal is the create door's and the commit door's shared validator
// (TD3): `proposal.create` on the socket and `aeon proposal commit` call THIS
// function on the same payload, so one fix lands on both doors at once. Its
// signature is the daemon's `ValidateProposal` seam exactly, which is how
// `cmd/aeon serve` wires it without the protocol package importing anything
// the validator needs.
func (r *Registry) ValidateProposal(ctx context.Context, kind, payload string) error {
	e, ok := r.effects[Kind(kind)]
	if !ok {
		return fmt.Errorf("refused: proposal kind %q has no validator registered", kind)
	}
	if e.Validate == nil {
		return fmt.Errorf("refused: proposal kind %q has no validator registered — %s", kind, r.Missing(kind))
	}
	return e.Validate(ctx, []byte(payload))
}

// AlreadyApplied is TD4's gate, run BEFORE re-validation: an effect that
// already stands skips straight to the flip, so a crash between "applied" and
// "flipped" never reapplies (a second `policy_add` would refuse on the primary
// key, a second `mcp_server_add` on MC12's "configured twice").
func (r *Registry) AlreadyApplied(ctx context.Context, kind, payload string) (bool, error) {
	e, ok := r.effects[Kind(kind)]
	if !ok {
		return false, fmt.Errorf("refused: proposal kind %q has no idempotency check", kind)
	}
	if e.AlreadyApplied == nil {
		return false, fmt.Errorf("refused: proposal kind %q has no idempotency check — %s", kind, r.Missing(kind))
	}
	return e.AlreadyApplied(ctx, []byte(payload))
}

// Apply performs the effect, through the same functions the direct CLI verbs
// use — never a sixth implementation. The writer reaches the effects that print
// for the human at the door; the ones that print nothing today stay silent.
func (r *Registry) Apply(ctx context.Context, w io.Writer, kind, payload string) error {
	e, ok := r.effects[Kind(kind)]
	if !ok {
		return fmt.Errorf("refused: proposal kind %q has no apply path", kind)
	}
	if e.Apply == nil {
		return fmt.Errorf("refused: proposal kind %q has no apply path — %s", kind, r.Missing(kind))
	}
	if w == nil {
		w = io.Discard
	}
	return e.Apply(ctx, w, []byte(payload))
}

// --- policy_add -----------------------------------------------------------------

func (d Deps) alreadyAppliedPolicyAdd(ctx context.Context, payload []byte) (bool, error) {
	var p policy.Policy
	if err := json.Unmarshal(payload, &p); err != nil {
		return false, err
	}
	b, closeFn, err := OpenBoundary(d.Boundary)
	if err != nil {
		return false, err
	}
	defer closeFn()
	rows, rej := b.ListPolicies(ctx, d.now().UTC())
	if rej != nil {
		return false, fmt.Errorf("%s", rej.Reason)
	}
	for _, existing := range rows {
		if existing.ID == p.ID {
			return true, nil
		}
	}
	return false, nil
}

func (d Deps) applyPolicyAdd(ctx context.Context, _ io.Writer, payload []byte) error {
	var p policy.Policy
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	b, closeFn, err := OpenBoundary(d.Boundary)
	if err != nil {
		return err
	}
	defer closeFn()
	if rej := b.AddPolicy(ctx, p, d.now().UTC()); rej != nil {
		return fmt.Errorf("%s", rej.Reason)
	}
	return nil
}

// --- policy_remove ----------------------------------------------------------------

func (d Deps) alreadyAppliedPolicyRemove(ctx context.Context, payload []byte) (bool, error) {
	var in PolicyRemovePayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return false, err
	}
	b, closeFn, err := OpenBoundary(d.Boundary)
	if err != nil {
		return false, err
	}
	defer closeFn()
	rows, rej := b.ListPolicies(ctx, d.now().UTC())
	if rej != nil {
		return false, fmt.Errorf("%s", rej.Reason)
	}
	for _, existing := range rows {
		if existing.ID == in.ID {
			return false, nil // still there — not yet removed
		}
	}
	return true, nil // already gone
}

func (d Deps) applyPolicyRemove(ctx context.Context, _ io.Writer, payload []byte) error {
	var in PolicyRemovePayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return err
	}
	b, closeFn, err := OpenBoundary(d.Boundary)
	if err != nil {
		return err
	}
	defer closeFn()
	if _, err := b.RemovePolicy(ctx, in.ID); err != nil {
		return err
	}
	return nil
}

// --- seed_corpus -------------------------------------------------------------------

// noDetection is `seed_corpus`'s answer to the idempotency gate: it needs no
// detection, because its own per-rule skip-if-exists discipline makes re-running
// it on a partially-seeded set naturally convergent (D4). Always re-run.
func noDetection(_ context.Context, _ []byte) (bool, error) { return false, nil }

func (d Deps) applySeedCorpus(ctx context.Context, _ io.Writer, payload []byte) error {
	var in SeedCorpusPayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return err
	}
	b, closeFn, err := OpenBoundary(d.Boundary)
	if err != nil {
		return err
	}
	defer closeFn()
	// io.Discard, deliberately: the commit door has never printed the seeder's
	// per-agent progress, and the CLI verb — the same function, the one
	// implementation — is where those lines belong.
	return RunSeedCorpus(ctx, io.Discard, b, in.Agents, false, d.now().UTC())
}

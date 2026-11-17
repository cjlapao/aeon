// proposal_ctx.go — S59 T7: the third thing Commit injects, beside the
// Decider (autonomy_kinds.go) and the CommitActor (decide.go).
//
// An effect that writes an audit row should be able to say WHICH proposal it
// was applying, without the caller threading an id through every Effect
// signature. Before this, only the policy kinds could point back — their
// preview carries the row from the store — and the S59 environment audit row
// would have had to re-derive it from the payload (a guess about which row is
// being committed, which is exactly what an effect must never do).
//
// Fail-closed by convention, like the other two channels: an effect that
// needs the proposal id asks with ProposalIDFrom and refuses when it is
// absent (a commit door that did not install it is a wiring bug, and a silent
// zero would put "proposal 0" in an append-only audit row).
//
// The S46 T6 record added WithCommitActor for the same reason and named this
// shape as the pattern; nothing here changes what the other two carry.
package desk

import "context"

type proposalIDKey struct{}

// WithProposalID installs the id of the proposal being committed, for effects
// that record it (today: environment_restrict's audit summary).
func WithProposalID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, proposalIDKey{}, id)
}

// ProposalIDFrom returns the id of the proposal being committed and whether
// the door installed it. ok is false outside a commit — an effect called from
// anywhere else has no proposal, and must say so rather than record a zero.
func ProposalIDFrom(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(proposalIDKey{}).(int64)
	return id, ok
}

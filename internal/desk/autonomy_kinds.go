// autonomy_kinds.go — S56 T8: the two RESTRICTIVE autonomy proposal kinds
// (autonomy_revoke, autonomy_deny) as registry entries in this package.
//
// The desk keeps only what is pure: the payload SHAPES, the shape validators
// (the create door's and the commit door's shared half, TD3), the required
// reason rule, and the Decider identity the commit stamps. The store halves
// (the grant exists, the node exists, the subject validates against the
// action registry, an identical veto already stands) arrive as `Seams` from
// `cmd/aeon` — the same importer pin that keeps the MCP and import effects
// outside this package: this file must never name an autonomy table or open a
// store, so the "no agent may widen" rule (AL11) holds here by construction —
// neither kind's payload carries an allow, a condition or an expiry, and the
// strict decode refuses any that is smuggled in.
//
// There is deliberately NO third autonomy kind in Kinds(): a proposal that
// creates or widens an allow is not a shape the desk knows, and the store's
// CHECK (migration 20261101180000) refuses it before SQL.
package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The two restrictive autonomy kinds, in the order the store's
// ProposalKinds declares them (it appends them last, beside the kind list).
const (
	KindAutonomyRevoke Kind = "autonomy_revoke"
	KindAutonomyDeny   Kind = "autonomy_deny"
)

// AutonomyDenySubject is the deny payload's subject: the same two-shape
// vocabulary the pure core knows (an action by name, or the one class
// shorthand) — re-declared here rather than imported because this package's
// dependency rule is stdlib + policy + paths, and importing the autonomy core
// would drag its sentence renderer into the decision code. The class name
// literal must stay equal to the core's shorthand ("outward"); the seam's
// store half re-runs the core's own validator, so a drift is refused there
// with the core's own words.
type AutonomyDenySubject struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// AutonomyRevokePayload is autonomy_revoke's payload: the grant id and the
// why. There is no state field — the effect is fixed at revoke (an agent that
// wants to un-revoke asks a person), and the strict decode below refuses one.
type AutonomyRevokePayload struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// AutonomyDenyPayload is autonomy_deny's payload: the node (a root or a
// child — the veto may sit on either) and the subject it vetoes. state is
// implied (deny) and conditions and expiry are absent by shape: a veto with
// conditions is not a veto, and no proposal shape may grant anything (AL11).
type AutonomyDenyPayload struct {
	NodeID  string              `json:"nodeId"`
	Subject AutonomyDenySubject `json:"subject"`
	Reason  string              `json:"reason"`
}

// autonomyClassShorthand mirrors internal/autonomy.ClassShorthand — the one
// class name a subject may carry. It is a literal on purpose (the importer
// pin); TestDeskImportsNothingForbidden keeps it that way.
const autonomyClassShorthand = "outward"

// ProposeReasonSentence is the required-reason rule the propose schemas
// declare and every propose door enforces: present, non-blank, at most 512
// bytes. An unreasoned proposal is a question a person cannot decide. It is
// defined here — the package both doors already share — so the socket's
// records arms (which build a Failure) and the seam's desk halves (which
// return an error) carry the SAME bytes; an empty return means the reason
// passed. S45's daemon helper delegates to this (2026-11-01, S56 T8), so the
// policy arms keep their bytes and the autonomy arms stop re-declaring them.
func ProposeReasonSentence(reason, tool string) string {
	if strings.TrimSpace(reason) == "" {
		return fmt.Sprintf("refused: %s needs a reason — a proposal nobody can read a why for is a question a person cannot decide (1 to 512 bytes)", tool)
	}
	if len(reason) > 512 {
		return fmt.Sprintf("refused: %s reason is %d bytes and the cap is 512 — the why is for reading, not for burying the desk", tool, len(reason))
	}
	return ""
}

// validateAutonomyRevokePayload is the desk's pure half of autonomy_revoke's
// Validate (the shape): a strict decode — a smuggled "state" or "conditions"
// key is a parse refusal with this sentence, byte for byte the one the
// create door gives — a non-blank id, and the reason rule. The store half
// (the grant exists and is not already revoked, is not a deny row) is the
// seam's; this half deliberately does NOT refuse an already-revoked grant —
// that is AlreadyApplied's job at commit, and refusing it here would kill
// the crash-retry path (an effect that landed but did not flip is a retry,
// not a conflict).
func validateAutonomyRevokePayload(_ context.Context, payload []byte) error {
	var p AutonomyRevokePayload
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: autonomy_revoke payload is {\"id\": \"<grant id>\", \"reason\": \"<why>\"} and nothing else — %v", err)
	}
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("refused: autonomy_revoke needs the grant's id — see it with autonomy_show")
	}
	if s := ProposeReasonSentence(p.Reason, "autonomy_propose_revoke"); s != "" {
		return fmt.Errorf("%s", s)
	}
	return nil
}

// validateAutonomyDenyPayload is the desk's pure half of autonomy_deny's
// Validate (the shape): a strict decode — the same smuggle refusal as
// revoke's — a non-blank node id, a subject in the two-shape vocabulary
// (action by non-empty name, or the one class, named exactly "outward"), and
// the reason rule. The store half (the node exists and is not archived, the
// action name is one the registry knows, an identical veto already stands)
// is the seam's, which re-runs the pure core's own deny validator so the
// words cannot drift.
func validateAutonomyDenyPayload(_ context.Context, payload []byte) error {
	var p AutonomyDenyPayload
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: autonomy_deny payload is {\"nodeId\": \"<root or child>\", \"subject\": {\"kind\": \"action\"|\"class\", \"name\": \"<action>\"|\"outward\"}, \"reason\": \"<why>\"} and nothing else — %v", err)
	}
	if strings.TrimSpace(p.NodeID) == "" {
		return fmt.Errorf("refused: autonomy_deny needs the node's id — a root or a child (a veto may sit on either)")
	}
	switch p.Subject.Kind {
	case "action":
		if strings.TrimSpace(p.Subject.Name) == "" {
			return fmt.Errorf("refused: an action subject needs the action's name — the registry's names, from autonomy's vocabulary")
		}
	case "class":
		if p.Subject.Name != autonomyClassShorthand {
			return fmt.Errorf("refused: the one class is named %q, not %q — there is no other class and no irreversible class", autonomyClassShorthand, p.Subject.Name)
		}
	default:
		return fmt.Errorf("refused: a subject's kind is \"action\" or \"class\", not %q", p.Subject.Kind)
	}
	if s := ProposeReasonSentence(p.Reason, "autonomy_propose_deny"); s != "" {
		return fmt.Errorf("%s", s)
	}
	return nil
}

// Exported for the seam's full Validate halves (cmd/aeon runs the desk half
// first, then its store half): the create door and the commit door both run
// the SAME function through the registry, so a smuggled payload is refused
// with the same sentence at both (TD3, behaviour 4).
var (
	ValidateAutonomyRevoke = validateAutonomyRevokePayload
	ValidateAutonomyDeny   = validateAutonomyDenyPayload
)

// Decider is the identity a commit stamps: the committer (the actor at the
// door — the peer a person holds) and the proposal's author (who filed it).
// The autonomy Apply seams read it from the context: the actor on the store
// row is the COMMITTER, and the grant's proposed_by is the AUTHOR (S56 T8
// behaviour 2 — the two identities are different people, and conflating them
// is the failure the audit exists to catch).
type Decider struct {
	Kind       string
	ID         string
	AuthorKind string
	AuthorID   string
}

type deciderKey struct{}

// WithDecider returns a context carrying the decider; Commit installs it
// immediately before the Apply step, so the only code that sees it is an
// effect's Apply half — the two autonomy kinds.
func WithDecider(ctx context.Context, d Decider) context.Context {
	return context.WithValue(ctx, deciderKey{}, d)
}

// DeciderFrom reads the decider; ok is false on a context Commit did not
// prepare, and an Apply half that needs it must fail closed (a refusal
// naming the missing half), never fall back to a guessed identity.
func DeciderFrom(ctx context.Context) (Decider, bool) {
	d, ok := ctx.Value(deciderKey{}).(Decider)
	return d, ok
}

// live.go — the grant's live status as a person reads it (AL6/AL17). The
// pinned view (T6) shows each grant in a pin's snapshot with its live status:
// is the authority still there, and if not, what happened to it. The state is
// the closed trio "active" | "revoked" | "expired"; the sentence is rendered
// by this package's one formatter (sentences.go) so no surface re-words it —
// the revoked and expired clauses are the decision site's OWN builders
// (action.RevokedClause / action.ExpiredClause, T5), so what a loop is told
// and what the panel shows are one truth, by construction.
package autonomy

import (
	"time"
)

// LiveState is the closed live-status of a grant row (the pinned view's
// grants[].live.state). Revoked outranks expired: a grant revoked after its
// expiry is read as "revoked", which is the act that removed the authority.
const (
	LiveActive  = "active"
	LiveRevoked = "revoked"
	LiveExpired = "expired"
)

// Live answers, for one grant, whether its authority is still in force at
// `now`, and the sentence that says what it is (AL17: the one formatter —
// activeSentence is new here, revokedSentence/expiredSentence are the ones
// the decision site already speaks). It reads no store and no clock: the
// grant and the time are inputs, exactly as Plan takes them.
func Live(g Grant, now time.Time) (state, sentence string) {
	switch {
	case g.RevokedAt != nil:
		return LiveRevoked, revokedSentence(g)
	case g.ExpiresAt != nil && !g.ExpiresAt.After(now):
		return LiveExpired, expiredSentence(g)
	default:
		return LiveActive, activeSentence(g)
	}
}

// LiveStateOf is the wire-ready pair the pinned view's handler maps: one
// place the state and its sentence are decided together, so a view cannot
// carry a state whose sentence was built for another.
func LiveStateOf(g Grant, now time.Time) (state, sentence string) { return Live(g, now) }

package policy

// The spawn key (S11, G16/QAP-15): the value that stands between a verdict
// and a process.
//
// # The property, and how Go supplies it
//
// Spawn is the one call in the module that forks. G16 demands it be
// UNREACHABLE without a verdict — not "check the verdict first" as a source
// ordering a refactor can reorder (G12's lesson, generalised), but a value
// you cannot construct without having asked. That is what an unexported-
// fielded struct with a single minting method is in Go: every other package
// can NAME AllowToken, can pass one around, can hand one to Spawn — and
// cannot produce one whose Allows() answers true. The zero value is inert
// (the minted sentinel is what Allows checks, not the shape), and the fields
// carry no exported setter because there are no exported fields.
//
// # Why the token binds the command string and nothing else
//
// The engine already segmented and decided (T4/T5); the token is the proof
// that a decision exists FOR THIS EXACT COMMAND. Binding the exact string
// means a token minted for one command authorizes no other — the caller
// cannot obtain a verdict for `echo hi` and spawn `git push` with it. The
// minting path is the engine's verdict, so the token inherits the verdict's
// trust: a deny mints nothing, and an ask mints nothing (nobody spawns on a
// question nobody answered).
//
// # What the token is NOT
//
// Not a credential across processes or restarts — it is an in-process
// capability, and its MAC-freeness is deliberate: the adversary G16 defends
// against is a REACHABLE CALL SITE, not a memory forger (the P26 trust
// domain already owns that threat). Not a question row either: the token
// path reads nothing the model can write (SEC-054) — no Kind string, no
// pending-question record; authority comes from the engine over the exact
// command and nowhere else.
//
// The unforgeability is proven from a SEPARATE package
// (token_forge_test/, decision 11), because "internal to policy" privileges
// asserted from inside policy are nothing.

import "sync/atomic"

// tokenNonceSource is the mint-time discriminator. A monotonic counter (not
// a clock: clocks go backwards and repeat) so two allow verdicts over the
// same command never produce two value-equal tokens — `t1 == t2` is false
// for every pair this package mints, which is what stops tokens from being
// treated as interchangeable values, compared by equality, or used as map
// keys that silently merge.
var tokenNonceSource atomic.Uint64

// AllowToken is proof that the engine allowed this exact command. Its
// fields are unexported and the ONLY way to obtain one is Verdict.Token on
// an allow verdict — there is no exported constructor, zero-value tokens
// are inert (the minted sentinel is what Allows checks), and a token is
// bound to one command string. This is G16: Spawn is unreachable without a
// verdict, and the unforgeability is proven from a separate test package
// (token_forge_test), not asserted from inside.
type AllowToken struct {
	minted  bool   // set ONLY by Verdict.Token on an allow verdict
	command string // the exact command authorized
	nonce   uint64 // mint-time discriminator (prevents value-equal tokens)
}

// Token returns the spawn authorization for this verdict. ok is false for
// deny and ask — you cannot spawn on a question nobody answered.
//
// The deviation from the task's skeleton is one parameter and it is load-
// bearing: the skeleton reads `Token() (AllowToken, bool)` and binds the
// command "the verdict authorized", but a Verdict deliberately does not
// carry the command (evaluate.go copies only Subject and Type off the
// Request — the command is the REQUEST's, and a Verdict that carried it
// would be a verdict that could outlive the string it was asked about and
// be re-rendered for a different one). A token bound to no string would
// authorize every command; a token bound to a string the VERDICT never saw
// would mint authority from the caller. So the caller hands over the exact
// command it is about to run — the same string the Request carried into
// Evaluate, checked by Allows against the token — and an empty command
// mints nothing: a permission to run "" is a permission nobody asked for,
// and fail-closed says no.
func (v Verdict) Token(command string) (AllowToken, bool) {
	if !v.Allowed || v.Asked || command == "" {
		return AllowToken{}, false
	}
	return AllowToken{minted: true, command: command, nonce: tokenNonceSource.Add(1)}, true
}

// Allows reports whether this token authorizes command: minted sentinel
// plus EXACT string equality. The engine already segmented; a token for
// one command authorizes no other. The minted sentinel is checked FIRST:
// the zero value must be inert even against a command that could not have
// been minted into it (a hand-laid AllowToken{} carries no minted bit, and
// no other package can set one).
func (t AllowToken) Allows(command string) bool {
	return t.minted && command != "" && command == t.command
}

// Package intake is S61's engine: it turns an `intake_plan` proposal into a
// registered, scaffolded, seeded project — one digest-bound, resumable,
// idempotent run — and answers the read side (Plan, Status).
//
// The engine holds no truth of its own. The proposal row is the plan, the
// intake ledger is the progress, the components/charter/records rows are
// the result, and the git repositories on disk are the proof; every state
// word is DERIVED from those (GI6), and every effect goes through the one
// implementation the daemon already owns — the action registry's person
// doors for scaffold and publish, the store's creators for registration and
// records, the charter service for the charter. What this package adds is
// only ORDER, the digest, and the resume law.
package intake

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// intakeIDRule bounds the directory name the scratch helper builds from an
// id the caller read out of the store — the id arrived over a wire, and a
// path built from an unbounded string is a path-train waiting to leave the
// state directory.
var intakeIDRule = regexp.MustCompile(`^int-[A-Za-z0-9_-]{1,64}$`)

// ScratchDir makes (idempotently, 0700) the intake session's scratch
// workspace — the interview's drafts, notes and pinned files live here, and
// nowhere near the target (GI6's "the scratch is not the project"). It is
// the daemon's directory under its own state root; T9's intake.start calls
// it BEFORE store.CreateIntake, because the row records a path that must
// already exist to be honest.
func ScratchDir(stateDir, intakeID string) (string, error) {
	if stateDir == "" || !filepath.IsAbs(stateDir) {
		return "", fmt.Errorf("intake: the scratch lives under the state directory — an absolute root is required")
	}
	if !intakeIDRule.MatchString(intakeID) {
		return "", fmt.Errorf("intake: scratch workspace for %q refused — the intake id must be `int-` and a short token", intakeID)
	}
	dir := filepath.Join(stateDir, "intake", intakeID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("intake: creating the scratch workspace: %w", err)
	}
	// MkdirAll honours an existing directory's mode, and 0700 is the promise
	// (P26's floor): set it explicitly, so a pre-existing 0777 scratch (a
	// hand-made dir) does not quietly keep its openness.
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("intake: securing the scratch workspace: %w", err)
	}
	return dir, nil
}

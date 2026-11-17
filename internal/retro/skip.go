package retro

import "strings"

// SkipRule answers, before any reading, whether a terminal event is the
// recorder's business. RT2 names the skips; T0's measurements supply the
// shapes. Recording nothing is the whole behaviour — the log line that
// names the reason is the daemon's job, so this returns the reason word.
//
// F15's triage-validation loops (`triage-validate-<bugid>`, S51) are
// ordinary loops by engine shape, so without this skip every validation
// attempt would double-count the bug's own loops (T0 §inventory: the
// slug is the only marker it carries).
const (
	skipTriageValidation = "triage-validate- slug (F15)"
	skipRetrospective    = "retrospective blueprint (RT5 — the loop is the retrospective itself)"
)

// SkipPin checks the two pin-visible rules against an already-read pin.
// A loop with NO readable pin is also skipped, but that decision needs no
// rule table — the caller has the read failure in hand (Behaviour §1's
// last item, handled in the daemon where the read happens).
func SkipPin(componentRoot, slug, blueprintID string) (reason string, skip bool) {
	if strings.HasPrefix(slug, "triage-validate-") {
		return skipTriageValidation, true
	}
	if blueprintID == "retrospective" {
		return skipRetrospective, true
	}
	return "", false
}

package automation

// outcome.go — AU12's mapping table, the one place a fire outcome is
// spoken in S20's vocabulary. The two vocabularies are deliberately
// different (an event fire and a schedule fire answer different questions),
// and this is the bridge: total over the closed sets, so T8's UI never
// re-implements the translation and cannot drift from it.

import (
	"strings"

	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// S20Outcome maps a fire row's (outcome, reason code) onto S20's Outcome.
// ok=false means the pair is not in either closed set — the caller has a
// broken row, not a decision, and must say so rather than pick the
// nearest word. The reason code is the text before the first colon.
func S20Outcome(outcome, reasonCode string) (trigger.Outcome, bool) {
	code := reasonCode
	if i := strings.Index(code, ":"); i >= 0 {
		code = code[:i]
	}
	switch outcome {
	case store.FireStarted:
		return trigger.OutcomeFired, true
	case store.FireLimited:
		return trigger.OutcomeDidNotFire, true
	case store.FireSkipped:
		switch code {
		case "disabled":
			return trigger.OutcomeDidNotFire, true
		case "overlap":
			return trigger.OutcomeSkipped, true
		}
	case store.FireRefused:
		return trigger.OutcomeRefused, true
	case store.FireCouldNotSay:
		return trigger.OutcomeCouldNotSay, true
	}
	return "", false
}

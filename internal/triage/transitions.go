package triage

import "fmt"

// AllowedTransitions is D1's machine, edge for edge. From `unvalidated`
// (no attempt has run) an attempt lands one of the three validated states;
// nothing moves back to `unvalidated`, ever — a re-attempt is a NEW attempt
// record (D3), not a reset of the bug's own state, so the three validated
// states are terminal for the bug. A self-transition is idempotent (a
// retried reactor write is safe), not wrong.
func AllowedTransitions() map[ValidationState][]ValidationState {
	return map[ValidationState][]ValidationState{
		ValidationUnvalidated:   {ValidationReproduced, ValidationNotReproduced, ValidationCouldNotTell},
		ValidationReproduced:    {},
		ValidationNotReproduced: {},
		ValidationCouldNotTell:  {},
	}
}

// TransitionResult is the DB19 shape: a discriminated result, not a bare
// error — the caller branches on OK and quotes Reason.
type TransitionResult struct {
	OK     bool
	Reason string
	// Legal carries the targets the table allows from `from`, so a refusal
	// can name what CAN be done (the house refusal rule).
	Legal []ValidationState
}

// ValidateTransition answers "may this bug move" without touching a row.
func ValidateTransition(from, to ValidationState) TransitionResult {
	if !ValidValidationState(from) {
		return TransitionResult{Reason: fmt.Sprintf("validation_state %q is not one of D1's four", from)}
	}
	if !ValidValidationState(to) {
		return TransitionResult{Reason: fmt.Sprintf("validation_state %q is not one of D1's four", to)}
	}
	legal := AllowedTransitions()[from]
	if from == to {
		return TransitionResult{OK: true, Legal: legal}
	}
	for _, s := range legal {
		if s == to {
			return TransitionResult{OK: true, Legal: legal}
		}
	}
	reason := fmt.Sprintf("bug cannot go from %q to %q", from, to)
	if from != ValidationUnvalidated {
		reason += " — the bug has already been validated; a re-attempt is a NEW attempt record, not a reset of its own state (D3)"
	}
	if to == ValidationUnvalidated {
		reason += " — nothing transitions back to unvalidated (D1): `unvalidated` means no attempt ran, and an attempt that ran cannot unrun"
	}
	return TransitionResult{Reason: reason, Legal: legal}
}

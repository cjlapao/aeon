package roadmap

// S50 T2 (RM5) — ONE formatter for the honesty states. The three priority
// states are never collapsed to a badge and an estimate is never a bare
// number (D8): the basis rides the rendering. list, show, board, the CLI
// and the UI all print THIS string — a formatter per surface is exactly
// how one slice's honest `unknown` becomes another slice's blank.

import (
	"fmt"
	"strings"
)

// Describe renders the item's honest words: status, the three-state
// priority with whose word it carries, the estimate range WITH its basis,
// and the loop link when the item materialized. Readiness is deliberately
// NOT guessed here — IsReady is consulted at its own moments (the board
// passes its own verdict beside this string; RM6's blocker rule says the
// SAME sentence IsReady produced, never a re-derivation).
func Describe(item Item) string {
	parts := []string{string(item.Status)}
	switch item.Priority {
	case PriorityKnown:
		parts = append(parts, fmt.Sprintf("priority known (%s)", item.PriorityLabel))
	case PriorityAssumed:
		parts = append(parts, fmt.Sprintf("priority assumed (%s)", item.PriorityLabel))
	default:
		parts = append(parts, "priority unknown")
	}
	if item.Estimate != nil {
		parts = append(parts, fmt.Sprintf("estimate %d–%d (%s)", item.Estimate.Min, item.Estimate.Max, item.Estimate.Basis))
	} else {
		parts = append(parts, "no estimate")
	}
	if item.LoopSlug != "" {
		parts = append(parts, "loop "+item.LoopSlug)
	}
	return strings.Join(parts, " · ")
}

// PathEditableStatuses is RM1's window: artifact paths are history from
// `running` onward; before that a person may correct a pointer. `done`
// and `cancelled` sit outside the window (title/epic only — the typo fix
// stays legal, the history rewrite does not).
func PathEditableStatuses() []Status {
	return []Status{StatusIdea, StatusIdeating, StatusBriefAgreed, StatusPlanned, StatusReady}
}

// PathEditable reports whether item's status sits inside RM1's window.
func PathEditable(item Item) bool {
	for _, s := range PathEditableStatuses() {
		if item.Status == s {
			return true
		}
	}
	return false
}

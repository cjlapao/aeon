package learnings

import "fmt"

// Describe renders one learning's provenance as its own words (S52 LM6).
// It exists because the four provenances are four different claims about
// the world, and a surface that shows only "confidence: 0.8" collapses
// them into a number nobody can check. Every surface that shows a
// learning shows this sentence verbatim — the board, the CLI, the app's
// pills — one formatter, one truth (the pattern policy's verdict sentence
// set).
//
// The four templates are fixed and deliberately distinct words:
//
//   - told — a person said it;
//   - observed — the loop saw it, with its count (M3's strength rides the
//     word, "×N", never a merged score); an observed learning with no
//     recorded count says exactly that rather than implying ×0;
//   - assumed — reasoned, not seen;
//   - relayed — a FACT ABOUT THE CHANNEL: it arrived by the board. It is
//     never labelled verified, checked, or confirmed — relayed says where
//     it came from, not whether it is true (M1's honesty, which is the
//     whole reason the label exists).
func Describe(l Learning) string {
	switch l.Provenance {
	case ProvenanceTold:
		return "told — a person said it"
	case ProvenanceObserved:
		if l.ObservedCount == nil {
			return "observed — a loop saw it; no count was recorded (M3: the count is set once at write time or not at all)"
		}
		n := *l.ObservedCount
		switch n {
		case 0:
			return "observed — seen zero times as recorded (a count set before any sighting, stated as it is)"
		case 1:
			return "observed (×1) — a loop saw it once"
		default:
			return fmt.Sprintf("observed (×%d) — a loop saw it %d times", n, n)
		}
	case ProvenanceAssumed:
		return "assumed — reasoned, not seen"
	case ProvenanceRelayed:
		return "relayed — it came by the board channel; that is a fact about the channel, never a claim that it was verified (M1)"
	default:
		// An unknown provenance word should not reach here (the CHECK
		// closed the four), but a Describe that panics or silently says
		// "told" about a fifth value is worse than one that says it does
		// not know.
		return fmt.Sprintf("provenance %q is not one of the four — say so rather than render a label that lies", string(l.Provenance))
	}
}

// M1OrderingNote is the one sentence every surface quotes when ordering
// learnings says WHY relayed sits below the rest (S52 LM5's `why`, LM6's
// vocabulary). One string, quoted verbatim, so the board, forAgent and the
// CLI can never disagree about the law they are all following.
const M1OrderingNote = "relayed ranks below told, observed and assumed — a channel fact never outscores a person's word or a loop's seeing (M1's honesty ordering, unchanged by S52)"

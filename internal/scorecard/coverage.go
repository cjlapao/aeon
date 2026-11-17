package scorecard

// coverage.go — RT9 on every card: the honesty strip. loopsInHistory
// comes from the merged history projection (S25 — the same view a person
// reads, live loops and archived ones side by side on their own time),
// loopsRecorded from the card's own scope, and the difference is stated,
// never hidden. The card is returned even when empty (T4 rule 10).

import (
	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/store"
)

func (c *Card) onCoverage(d Deps, p plan, w Window, recorded map[string]bool) error {
	since, _, err := d.S.RetroCoverage()
	if err != nil {
		return err
	}
	c.Coverage = Coverage{
		RecordedSince: since,
		LoopsRecorded: len(recorded),
	}
	if p.noScope {
		c.Coverage.LoopsInHistory = 0
		return nil
	}
	// One merge per scoped root (history's own scope semantics: an empty
	// ComponentRoot spans everything — the workspace-wide subjects take
	// exactly one merge with an empty scope).
	scopes := []store.ComponentScope{{}}
	if !p.spanAll {
		scopes = scopes[:0]
		for _, r := range p.roots {
			scopes = append(scopes, store.ComponentScope{ComponentRoot: r})
		}
	}
	inHistory := map[string]bool{}
	for _, sc := range scopes {
		entries, err := history.Merge(d.S, history.Options{Scope: sc, Since: w.Since})
		if err != nil {
			return err
		}
		for _, e := range entries {
			if w.Until != "" && e.Timeline > w.Until {
				continue
			}
			inHistory[keyOf(e.ComponentRoot, e.Slug)] = true
		}
	}
	c.Coverage.LoopsInHistory = len(inHistory)
	if c.Coverage.LoopsInHistory > c.Coverage.LoopsRecorded {
		c.Coverage.LoopsNotRecorded = c.Coverage.LoopsInHistory - c.Coverage.LoopsRecorded
	}
	return nil
}

// statement is the ONE sentence RT7 asks for on a card — built once
// here, never re-rendered by a caller (N8: one place knows this truth).
func statement(cov Coverage) string {
	switch {
	case cov.LoopsInHistory == 0 && cov.LoopsRecorded == 0:
		return "no loops in the window's history and no recorded facts — the card is honest, not empty"
	case cov.LoopsRecorded == 0:
		return "no recorded facts for this window's loops"
	default:
		return "facts recorded for the card's unit"
	}
}

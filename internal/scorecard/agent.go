package scorecard

// agent.go — the per-agent plane (rule 8, RT7's `per agent only` row):
// every metric is computed from `retro_agent` rows joined to outcomes in
// the window. Attribution is the S43/F7 rule kept: a row whose agent
// column held a NAME that resolved to no slug carries
// `agent_slug_known = 0`, and the card says so in its note — name, not
// slug, is a label, never a merge into the slug-keyed card next door.

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

func (c *Card) onAgent(d Deps, p plan, w Window, agent string, keys map[string]bool) error {
	roots := p.roots // nil ⇒ every component (span-all)
	rows, err := d.S.RetroAgentRowsInWindow(roots, w.Since, w.Until, agent)
	if err != nil {
		return err
	}
	c.agentLoops(rows, keys)
	c.agentMetrics(rows)
	return nil
}

// agentLoops: the loops the agent appears in — n for the card, and the
// rule-8 label note when EVERY row of this card is name-only.
func (c *Card) agentLoops(rows []store.RetroAgentWindowRow, keys map[string]bool) {
	loops := map[string]bool{}
	known, unknown := 0, 0
	for _, r := range rows {
		k := keyOf(r.ComponentRoot, r.LoopSlug)
		loops[k] = true
		keys[k] = true
		if r.AgentSlugKnown {
			known++
		} else {
			unknown++
		}
	}
	m := metric(len(loops), 0, float64(len(loops)))
	switch {
	case unknown > 0 && known == 0:
		m.Note = "name, not slug"
	case unknown > 0:
		m.Note = fmt.Sprintf("%d row(s) carry a name, not a slug", unknown)
	}
	c.Metrics["loops"] = m
}

func appendNote(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + "; " + add
}

func (c *Card) agentMetrics(rows []store.RetroAgentWindowRow) {
	// stepsRun: Σ steps over rows that carry it (NULL rows excluded).
	steps, nSteps, skipped := 0.0, 0, 0
	rej, nRej := 0.0, 0
	for _, r := range rows {
		if v, ok := iv(r.Steps); ok {
			steps += float64(v)
			nSteps++
			if v > 0 {
				if rv, ok := iv(r.StepsRejected); ok {
					rej += float64(rv)
					nRej++
				}
			}
		} else {
			skipped++
		}
	}
	c.Metrics["stepsRun"] = metric(nSteps, skipped, steps)
	c.Metrics["stepRejectRate"] = metric(nRej, 0, ratio(rej, steps))

	// verify rates: over rows whose verdict columns are present.
	var vPass, vFail, vUnver float64
	nV, nU := 0, 0
	for _, r := range rows {
		p, okP := iv(r.VerifyPass)
		f, okF := iv(r.VerifyFail)
		u, okU := iv(r.VerifyUnverified)
		if okP && okF && (p+f > 0) {
			vPass += float64(p)
			vFail += float64(f)
			nV++
		}
		if okU && okP && okF {
			vUnver += float64(u)
			nU++
		}
	}
	c.Metrics["verifyFailRate"] = metric(nV, 0, ratio(vFail, vPass+vFail))
	c.Metrics["verifyUnverifiedRate"] = metric(nU, 0, ratio(vUnver, vPass+vFail+vUnver))

	// costPerStep: a floor, same discipline as costPerCompletedItem.
	sum, n, stepsN, partial, unknown := 0.0, 0, 0, 0, 0
	for _, r := range rows {
		if r.CostUSDFloor == nil {
			continue
		}
		sum += *r.CostUSDFloor
		n++
		stepsN += derefZ(r.Steps)
		partial += derefZ(r.CostPartialSteps)
		unknown += derefZ(r.CostUnknownSteps)
	}
	m := metric(n, 0, ratio(sum, float64(stepsN)))
	if n > 0 {
		if partial > 0 || unknown > 0 {
			m.State = StatePartial
			m.Note = appendNote(m.Note, "a floor")
		}
		m.Detail = map[string]any{
			"total":        sum,
			"steps":        stepsN,
			"unknownSteps": unknown,
			"partialSteps": partial,
			"rendered":     costSentence(sum, stepsN, unknown, partial > 0 || unknown > 0),
		}
	}
	c.Metrics["costPerStep"] = m
}

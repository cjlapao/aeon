package scorecard

// compute.go — the card's shared engine: resolve the window, resolve the
// subject's roots, read the unit (RT7: latest outcome per loop in the
// window), and compute the common metrics with rule-checked states.
// Every rule in T4's list has a name here as it does in the tests.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/store"
)

// ErrNotRecorded is the prerequisite answer: a metric whose slice has
// not landed says this, and the caller renders `cannot-say` (rule 7).
var ErrNotRecorded = errors.New("scorecard: not recorded")

// GateResults is the prerequisite interface for the gate-plane metrics
// (gatePassRate, mergedThenRevertedRate). S57's tables will grow it;
// until then nothing implements it and both metrics cannot-say. The
// interface — not a string check — is what makes the future
// implementation a compile-time event.
type GateResults interface {
	GatePassFail(componentRoots []string, since, until string) (pass, fail int, err error)
	MergedThenReverted(componentRoots []string, since, until string) (merged, reverted int, err error)
}

// gateResults is the wiring point; nil is the shipped truth (T0 measured
// the revert fact does not exist).
var gateResults GateResults //nolint:revive // the honest nil: no S57 twin yet

// prereqGate answers rule 7: with no gate plane the metric is
// ErrNotRecorded, never a zero.
func prereqGate(g GateResults) error {
	if g == nil {
		return ErrNotRecorded
	}
	return nil
}

// defaultLookback is RT8's default window.
const defaultLookback = 30 * 24 * time.Hour

// resolveWindow fills the default 30-day since (RFC3339), echoing what
// the card will claim.
func resolveWindow(w Window, now time.Time) Window {
	out := w
	if out.Since == "" {
		out.Since = now.Add(-defaultLookback).UTC().Format(time.RFC3339)
	}
	return out
}

// plan is the resolved scope: which component_roots the card spans.
// spanAll is the workspace-wide subject (agent, blueprint); a component
// with no resolvable root has NEITHER — it spans nothing and every
// metric cannot-says honestly, rather than answering for the machine.
type plan struct {
	roots   []string
	spanAll bool
	noScope bool
	// children lists the DIRECT children for a component card's
	// breakdown (RT8: counts only, nothing to rank).
	children []store.Component
	self     string
}

func planSubject(s *store.Store, subj Subject) (plan, error) {
	if subj.Kind != SubjectComponent {
		if subj.RootID == "" {
			return plan{spanAll: true}, nil
		}
		// A narrowed agent or blueprint card (RT10's rootId): the subtree
		// under RootID, resolved exactly as a component subject is — same
		// tree reads, same honest-empty rules.
		return planSubject(s, Subject{Kind: SubjectComponent, ID: subj.RootID, IncludeChildren: true})
	}
	comp, err := s.ReadComponent(subj.ID)
	if err != nil {
		// An unregistered id is read AS a root path (types.go's promise).
		return plan{roots: []string{subj.ID}, self: subj.ID}, nil
	}
	p := plan{self: comp.ID}
	if comp.Root != "" {
		p.roots = append(p.roots, comp.Root)
	} else {
		p.noScope = true
	}
	if !subj.IncludeChildren {
		p.noScope = false // the node alone still has its own root
		return p, nil
	}
	children, err := s.ListChildren(comp.ID)
	if err != nil {
		return plan{}, err
	}
	p.children = children
	// Depth two (RT8): the children and THEIR children.
	p.roots = append(p.roots, descendantRoots(s, children)...)
	if len(p.roots) == 0 {
		p.noScope = true
	}
	return p, nil
}

func descendantRoots(s *store.Store, comps []store.Component) []string {
	var roots []string
	for _, c := range comps {
		if c.Root != "" {
			roots = append(roots, c.Root)
		}
		kids, err := s.ListChildren(c.ID)
		if err != nil {
			continue // an unreadable child is a smaller tree, not a failed card
		}
		roots = append(roots, descendantRoots(s, kids)...)
	}
	return roots
}

// unit is the card's raw material: the latest outcome per loop, plus
// the window-free ever-blocked set ("blocked" counts loops that EVER
// blocked — rule 1).
type unit struct {
	rows        []store.RetroOutcome
	everBlocked map[string]bool // root + "\x00" + slug
}

func keyOf(root, slug string) string { return root + "\x00" + slug }

func (u unit) everBlockedFor(o store.RetroOutcome) bool {
	return u.everBlocked[keyOf(o.ComponentRoot, o.LoopSlug)]
}

func readUnit(ctx context.Context, d Deps, p plan, w Window, lastN int) (unit, error) {
	u := unit{everBlocked: map[string]bool{}}
	if p.noScope {
		return u, nil
	}
	roots := p.roots
	if p.spanAll {
		roots = nil
	}
	rows, err := d.S.RetroLatestOutcomes(roots, w.Since, w.Until, lastN)
	if err != nil {
		return u, err
	}
	u.rows = rows
	blocked, err := d.S.RetroEverBlockedLoops(roots)
	if err != nil {
		return u, err
	}
	for _, b := range blocked {
		u.everBlocked[keyOf(b.Root, b.Slug)] = true
	}
	return u, nil
}

// Compute builds one card. Errors are store/history failures only — a
// subject with no facts is a full card of cannot-say metrics (RT7), not
// an error.
func Compute(ctx context.Context, d Deps, subj Subject, w Window) (Card, error) {
	eff := resolveWindow(w, d.nowOrDefault())
	p, err := planSubject(d.S, subj)
	if err != nil {
		return Card{}, err
	}
	u, err := readUnit(ctx, d, p, eff, eff.LastN)
	if err != nil {
		return Card{}, err
	}
	card := Card{Subject: subj, Window: eff, Metrics: map[string]Metric{}}
	keys := map[string]bool{} // the card's distinct loops — the coverage numerator
	switch subj.Kind {
	case SubjectAgent:
		if err := card.onAgent(d, p, eff, subj.ID, keys); err != nil {
			return Card{}, err
		}
	case SubjectBlueprint:
		uu := u.filteredBlueprint(subj.ID)
		card.onCommon(uu, keys)
		card.onBlueprint(uu.rows)
	case SubjectComponent:
		card.onCommon(u, keys)
		card.onComponent(p, u)
	}
	// The coverage numerator is never capped by `last` (RT9 counts loops,
	// the cap trims the card's metrics, not the honesty strip): refetch
	// the uncapped distinct set only when a cap was in force.
	if eff.LastN > 0 && subj.Kind != SubjectAgent && !p.noScope {
		roots := p.roots
		if p.spanAll {
			roots = nil
		}
		all, err := d.S.RetroLatestOutcomes(roots, eff.Since, eff.Until, 0)
		if err != nil {
			return Card{}, err
		}
		keys = map[string]bool{}
		for _, o := range all {
			if subj.Kind == SubjectBlueprint && o.BlueprintID != subj.ID {
				continue
			}
			keys[keyOf(o.ComponentRoot, o.LoopSlug)] = true
		}
	}
	if err := card.onCoverage(d, p, eff, keys); err != nil {
		return Card{}, err
	}
	card.Statement = statement(card.Coverage)
	return card, nil
}

func (u unit) filteredBlueprint(id string) unit {
	out := unit{everBlocked: u.everBlocked}
	for _, r := range u.rows {
		if r.BlueprintID == id {
			out.rows = append(out.rows, r)
		}
	}
	return out
}

// ---------- the common plane (RT7's shared metrics) ----------

// num lifts a computed float to the Metric's value pointer.
func num(v float64) *float64 { return &v }

// stateFor is rule 3 in one function: n==0 ⇒ cannot-say, n < MinSample ⇒
// too-few (value stays null — a rate from two runs is a rate nobody can
// act on), incomplete contributions ⇒ partial, else ok.
func stateFor(n, incomplete int) (State, string) {
	switch {
	case n == 0:
		return StateCannotSay, "no recorded facts"
	case n < MinSample:
		return StateTooFew, fmt.Sprintf("%d sample(s), fewer than the minimum %d", n, MinSample)
	case incomplete > 0:
		return StatePartial, fmt.Sprintf("%d of %d row(s) incomplete", incomplete, n)
	default:
		return StateOk, ""
	}
}

func metric(n, incomplete int, value float64) Metric {
	st, note := stateFor(n, incomplete)
	m := Metric{N: n, State: st, Note: note}
	if st == StateOk || st == StatePartial {
		m.Value = num(value)
	}
	return m
}

// finishedDone / aborted / blocked-kind predicates on the CLOSED kinds
// vocabulary (the design's words, not this package's).
func isDone(o store.RetroOutcome) bool     { return o.OutcomeKind == "done" }
func isAborted(o store.RetroOutcome) bool  { return o.OutcomeKind == "aborted" }
func isBlocked(o store.RetroOutcome) bool  { return strings.HasPrefix(o.OutcomeKind, "blocked") }
func isBackfill(o store.RetroOutcome) bool { return o.Source == "backfill" }

func iv(v *int) (int, bool) {
	if v == nil {
		return 0, false
	}
	return *v, true
}

// onCommon computes the metrics every component and blueprint card
// carries. Rule 1's backfill clause falls out of the NULL discipline:
// backfill rows hold NULLs exactly where first-pass, rework, wall and
// unverified need numbers — every NULL is excluded AND counted, and the
// note says "incomplete", so a backfilled loop can never inflate a rate.
func (c *Card) onCommon(u unit, keys map[string]bool) {
	for _, o := range u.rows {
		keys[keyOf(o.ComponentRoot, o.LoopSlug)] = true
	}
	n := len(u.rows)
	c.Metrics["loops"] = metric(n, 0, float64(n))
	done, aborted, everBlocked := 0, 0, 0
	incomplete := 0
	for _, o := range u.rows {
		if isDone(o) {
			done++
		}
		if isAborted(o) {
			aborted++
		}
		if isBlocked(o) || u.everBlockedFor(o) {
			everBlocked++
		}
		if !o.Complete {
			incomplete++
		}
	}
	c.Metrics["loops"] = metric(n, incomplete, float64(n))
	c.Metrics["finishedDone"] = metric(n, incomplete, float64(done))
	c.Metrics["aborted"] = metric(n, incomplete, float64(aborted))
	c.Metrics["blocked"] = metric(n, incomplete, float64(everBlocked)) // EVER, rule 1
	c.Metrics["doneRate"] = metric(n, incomplete, ratioI(done, n))
	c.Metrics["abortedRate"] = metric(n, incomplete, ratioI(aborted, n))
	c.Metrics["blockedRate"] = metric(n, incomplete, ratioI(everBlocked, n))

	// firstPassRate (rule 6): denominator = finished-done loops the row
	// can JUDGE; a NULL where the rule needs a number drops the row.
	denom, numer, skipped := 0, 0, 0
	for _, o := range u.rows {
		if !isDone(o) {
			continue
		}
		retries, okR := iv(o.Retries)
		unver, okU := iv(o.UnverifiedSteps)
		if !okR || !okU {
			skipped++
			continue
		}
		denom++
		if retries == 0 && unver == 0 && !u.everBlockedFor(o) {
			numer++
		}
	}
	fp := metric(denom, skipped, ratioI(numer, denom))
	if skipped > 0 {
		fp.Note = fmt.Sprintf("%s; %d finished-done loop(s) unreadable (backfilled or partial)", fp.Note, skipped)
	}
	c.Metrics["firstPassRate"] = fp

	c.Metrics["reworkPerItem"] = meanMetric(u.rows, func(o store.RetroOutcome) (float64, bool) {
		r, ok1 := iv(o.Retries)
		rr, ok2 := iv(o.ReviewRounds)
		sr, ok3 := iv(o.SecurityRounds)
		qr, ok4 := iv(o.QARounds)
		if !(ok1 && ok2 && ok3 && ok4) {
			return 0, false
		}
		return float64(r + rr + sr + qr), true
	})
	c.Metrics["reviewRounds"] = sumMeanMetric(u.rows, func(o store.RetroOutcome) (float64, bool) { return fiv(o.ReviewRounds) })
	c.Metrics["securityRounds"] = sumMeanMetric(u.rows, func(o store.RetroOutcome) (float64, bool) { return fiv(o.SecurityRounds) })

	// unverifiedRate = Σ unverified / Σ verdict-carrying steps. The fact
	// table (T1, frozen) holds no total-steps column — the steps a row
	// KNOWS are the ones a verification verdict reached: pass + fail +
	// unverified. That is the denominator, and a row missing any of the
	// three is skipped, not zero-filled (P31).
	sumU, sumS, nU := 0.0, 0.0, 0
	for _, o := range u.rows {
		uv, ok1 := fiv(o.UnverifiedSteps)
		vp, ok2 := fiv(o.VerifiedPass)
		vf, ok3 := fiv(o.VerifiedFail)
		if !(ok1 && ok2 && ok3) {
			continue
		}
		sumU += uv
		sumS += vp + vf + uv
		nU++
	}
	c.Metrics["unverifiedRate"] = metric(nU, 0, ratio(sumU, sumS))

	escRows, escLoops, escSkipped := 0, 0, 0
	for _, o := range u.rows {
		e, ok := iv(o.Escalations)
		if !ok {
			escSkipped++
			continue
		}
		escRows++
		if e > 0 {
			escLoops += e
		}
	}
	c.Metrics["escalations"] = metric(escRows, escSkipped, float64(escLoops))
	c.Metrics["escalationRate"] = metric(escRows, escSkipped, ratioI(escLoops, escRows))

	c.Metrics["costPerCompletedItem"] = costMetric(u.rows)
	c.Metrics["wallPerItem"] = wallMetric(u.rows)

	// Rule 7: the prerequisite metrics name their missing slice.
	c.Metrics["gatePassRate"] = prereqMetric(prereqGate(gateResults), "needs S57 (gate results)")
	c.Metrics["mergedThenRevertedRate"] = prereqMetric(prereqGate(gateResults), "needs S57 (revert facts)")
}

func ratio(num, den float64) float64 {
	if den == 0 {
		return 0
	}
	return num / den
}

// ratioI is the integer caller's convenience.
func ratioI(num, den int) float64 { return ratio(float64(num), float64(den)) }

func fiv(v *int) (float64, bool) {
	if v == nil {
		return 0, false
	}
	return float64(*v), true
}

// meanMetric: mean over rows the metric can judge (rule 2 — n is the
// judged rows, the skipped ones ride the partial note).
func meanMetric(rows []store.RetroOutcome, pick func(store.RetroOutcome) (float64, bool)) Metric {
	sum, n, skipped := 0.0, 0, 0
	for _, o := range rows {
		if v, ok := pick(o); ok {
			sum += v
			n++
		} else {
			skipped++
		}
	}
	return metric(n, skipped, ratio(sum, float64(n)))
}

func sumMeanMetric(rows []store.RetroOutcome, pick func(store.RetroOutcome) (float64, bool)) Metric {
	return meanMetric(rows, pick)
}

// costMetric is rule 4: a floor over finished-done loops that carry any
// cost, with the unknown/partial counts and history's own rendering
// shipped in Detail; no loop with any known cost ⇒ cannot-say, never 0.
func costMetric(rows []store.RetroOutcome) Metric {
	sum, n, steps, partial, unknown := 0.0, 0, 0, 0, 0
	for _, o := range rows {
		if !isDone(o) || o.CostUSDFloor == nil {
			continue
		}
		sum += *o.CostUSDFloor
		n++
		steps += derefZ(o.CostSteps)
		partial += derefZ(o.CostPartialSteps)
		unknown += derefZ(o.CostUnknownSteps)
	}
	m := metric(n, 0, sum/maxf(float64(n), 1))
	if n > 0 && (partial > 0 || unknown > 0) {
		m.State = StatePartial
		m.Note = fmt.Sprintf("a floor: %d partial and %d unknown step(s)", partial, unknown)
	}
	if n > 0 {
		m.Detail = map[string]any{
			"unknownSteps": unknown,
			"partialSteps": partial,
			"steps":        steps,
			"total":        sum,
			"rendered":     costSentence(sum, steps, unknown, partial > 0 || unknown > 0),
		}
	}
	return m
}

func derefZ(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// costSentence renders EXACTLY history's Rollup words (rule 4's
// "rendered string equals history's renderer"): the same struct, the
// same Render call — never a copy of its shape.
func costSentence(total float64, steps, unknown int, floor bool) string {
	return history.Rollup{Total: total, Floor: floor, UnknownCount: unknown, StepCount: steps}.Render()
}

// wallMetric is rule 5: median and p90 of wall_ms, NULLs excluded and
// the exclusion counted.
func wallMetric(rows []store.RetroOutcome) Metric {
	var samples []float64
	skipped := 0
	for _, o := range rows {
		if o.WallMS == nil {
			skipped++
			continue
		}
		samples = append(samples, float64(*o.WallMS))
	}
	n := len(samples)
	sort.Float64s(samples)
	m := metric(n, 0, percentile(samples, 0.5))
	if n >= MinSample {
		m.Detail = map[string]any{"p90": percentile(samples, 0.90), "excludedNull": skipped}
	}
	return m
}

func prereqMetric(err error, why string) Metric {
	m := Metric{State: StateCannotSay}
	if errors.Is(err, ErrNotRecorded) {
		m.Note = why
	}
	return m
}

// RootsPlan resolves a subject to the component_roots its card spans,
// for the ONE caller that must aggregate over the same rows the package
// does: the digest (daemon/digest_reads.go). all=true means the card
// spans every component; an empty roots with all=false spans nothing.
func RootsPlan(d Deps, subj Subject) (roots []string, all bool, err error) {
	p, err := planSubject(d.S, subj)
	switch {
	case err != nil:
		return nil, false, err
	case p.spanAll:
		return nil, true, nil
	case p.noScope:
		return []string{}, false, nil
	default:
		return p.roots, false, nil
	}
}

// StateFor answers the state a count of n deserves (RT7 rule 3) with no
// incomplete rows: the digest's by-kind counts ride the SAME rule the
// cards use, so one MinSample governs every number on the plane.
func StateFor(n int) State {
	st, _ := stateFor(n, 0)
	return st
}

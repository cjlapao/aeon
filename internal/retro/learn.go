package retro

// learn.go — S63 T3 (RT4/RT4a): the retrospective learning. From a
// recorded fact row, at a milestone, through S28's one write path —
// template-rendered, provenance `observed`, never a flood, never free
// text. RT4a is why this is ONE function (Notable below): if "worth
// remembering" were decided in two places, the milestone gate and the
// write would drift into arguing about the same row.
//
// The milestone rule is derived-count only (RT4: no stored tallies):
// n = the (root, kind) tally at the row's write order. The learning is
// written when n lands ON a milestone — 1, 3, 10, 30, … (×3.33 steps to
// 10^6, generated below by the one definition).
//
// RT3 again, because this file writes into the memory plane: the learning
// says what was counted. It cannot route anything, and nothing here reads
// a scorecard, a dispatch, a grant or a setting.

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/store"
)

// Outcome is the fact row's name here — the render input's source, and
// the argument Notable reads (RT4a's single signature).
type Outcome = store.RetroOutcome

// Notable is RT4a — the ONE decider of "worth a learning". A clean done
// is not (the loop did the thing, no fact to remember yet — F6's flood
// guard runs through this gate); anything that ended otherwise, or ended
// done with a scar (retries, failed gates, unverified steps, escalations,
// failed tasks), is. A NULL scar field is unreadable, not a zero — it
// does not make a clean done notable on its own (P31 riding the decision
// too); complete=0 rows still render, and the body says not recorded.
func Notable(o Outcome) bool {
	if o.OutcomeKind != "done" {
		return true
	}
	return pos(o.Retries) || pos(o.TotalFailedGates) || pos(o.UnverifiedSteps) ||
		pos(o.Escalations) || pos(o.TasksFailed)
}

func pos(v *int) bool { return v != nil && *v > 0 }

// milestonesUpTo caps the generated ladder at 10^6 — beyond that a
// component's memory is not the ladder's question (RT4's "…to 10^6").
const milestoneCap = 1_000_000

// Milestones is the ONE definition: 1, then ×3.33 steps rounded to one
// significant digit — 1, 3, 10, 30, 100, 300, 1000, … — generated rather
// than remembered, capped at 10^6. A test pins the literal prefix so a
// silent change to the generator is a review act.
var Milestones = buildMilestones()

func buildMilestones() []int {
	out := []int{1}
	for {
		next := roundOneSig(float64(out[len(out)-1]) * 3.33)
		if next > milestoneCap || next <= out[len(out)-1] {
			return out
		}
		out = append(out, next)
	}
}

// roundOneSig collapses 3.33's running product to a single significant
// digit, which is what makes the ladder read 30/100/300 (the spec's
// words) instead of 33.3/110.88/369.
func roundOneSig(v float64) int {
	mag := math.Pow10(int(math.Floor(math.Log10(v))))
	return int(math.Round(v/mag) * mag)
}

// isMilestone is the membership test over the generated ladder.
func isMilestone(n int) bool {
	for _, m := range Milestones {
		if n == m {
			return true
		}
		if n < m {
			return false
		}
	}
	return false
}

// windowOf is how many facts a milestone learning summarises: itself plus
// everything since the previous milestone (at n=1 one fact; at n=3 two;
// at n=10 seven). Rendered as {window}, so the sentence says its own
// reach instead of implying the whole history.
func windowOf(n int) int {
	prev := 0
	for _, m := range Milestones {
		if m >= n {
			return n - prev
		}
		prev = m
	}
	return n
}

// family picks which seeded template renders an outcome — a TOTAL
// function over the kind word plus the two done-scar fields, so a body
// per family stays reviewable (the five shipped families: default,
// done-with-rework, unverified, aborted, blocked). An unknown kind
// renders from default: honest, since "other:…" is its own word already.
func family(o Outcome) string {
	switch {
	case strings.HasPrefix(o.OutcomeKind, "blocked"):
		return "blocked"
	case o.OutcomeKind == "aborted":
		return "aborted"
	case o.OutcomeKind == "done" && (pos(o.Retries) || pos(o.TotalFailedGates)):
		return "done-with-rework"
	case o.OutcomeKind == "done" && pos(o.UnverifiedSteps):
		return "unverified"
	default:
		return "default"
	}
}

// Appender is the one write path's shape (F4/F5): *learnings.Learnings
// satisfies it, a spy wraps it, and the guard test greps that nothing
// here reaches a store learning-write directly. The production call is
// AppendAs — S52 made the audited actor the production path; T3's spec
// names `Append`, and Append is that path's post-S52 audited name.
type Appender interface {
	AppendAs(in learnings.AppendInput, actor learnings.Actor) (learnings.Learning, *learnings.Refusal)
}

// Deps is Write's world: the store (outcome, tally, template, link), the
// one learning write path, a log line door, and the P32 cost sentence
// RENDERER. RenderCost is injected, not imported — history's renderer is
// CALLED by the daemon's wiring (T3 §3's "call it, do not copy it"), and
// retro's own imports stay store + learnings + stdlib (RT3.2). A nil
// RenderCost renders cost as notRecorded, never as a copy of the shape.
type Deps struct {
	S          *store.Store
	L          Appender
	Log        func(string)
	RenderCost func(Outcome) string
}

func (d Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// Actor is the system identity on the audit line (S52 LM4/LM3): the
// retro reactor, not a person, not pretending to be.
var Actor = learnings.Actor{Kind: "system", ID: "retro"}

// Write is the recorder's follow-through: read the outcome by id, take
// the (root, kind) tally NOW (the row was just inserted, so the tally is
// its milestone number — the same rank reconcile's query recomputes), and
// write if the three gates open: inserted (caller's job to call only on
// insert), Notable (RT4a), and n ON a milestone.
func Write(ctx context.Context, deps Deps, outcomeID int64) error {
	out, found, err := deps.S.RetroOutcomeByID(outcomeID)
	if err != nil || !found {
		return fmt.Errorf("retro learning: outcome %d: %w", outcomeID, err)
	}
	n, err := deps.S.RetroTally(out.ComponentRoot, out.OutcomeKind)
	if err != nil {
		return fmt.Errorf("retro learning: tally %s/%s: %w", out.ComponentRoot, out.OutcomeKind, err)
	}
	return WriteAt(ctx, deps, out, n)
}

// WriteAt is Write with the count carried in — reconcile holds the row's
// RANK (store.MilestoneOutcome), which is the exact n the learning must
// name (M3: the count is set once, at write, and never touched again).
// Idempotent by the link: an outcome that already carries its learning is
// skipped, and the UNIQUE on retro_learning is the lock under the check.
func WriteAt(ctx context.Context, deps Deps, out Outcome, n int) error {
	linked, err := deps.S.HasRetroLearning(out.ID)
	if err != nil {
		return fmt.Errorf("retro learning: link probe %d: %w", out.ID, err)
	}
	if linked {
		return nil // the lock already answered — never a second learning
	}
	if !Notable(out) || !isMilestone(n) {
		return nil // not a flood, not yet (RT4's two gates)
	}
	fam := family(out)
	tmpl, found, err := deps.S.GetRetroTemplate(fam)
	if err != nil {
		return fmt.Errorf("retro learning: template %q: %w", fam, err)
	}
	if !found {
		// Rule 6 is why this REFUSES rather than falls back to code: the
		// body renders from the ROW or not at all. The seed set lays the
		// rows at start; reconcile runs after it (T3 §4's ordering).
		deps.logf("retro: no %q template row — no learning written for outcome %d (rule 6: the row is the body)", fam, out.ID)
		return nil
	}
	f := Facts{
		Count:          n,
		Window:         windowOf(n),
		OutcomeKind:    out.OutcomeKind,
		Blueprint:      out.BlueprintID,
		ReviewRounds:   out.ReviewRounds,
		SecurityRounds: out.SecurityRounds,
		Retries:        out.Retries,
		GatesFailed:    out.TotalFailedGates,
		Escalations:    out.Escalations,
		CostState:      costState(out),
		Wall:           wallWord(out),
	}
	if deps.RenderCost != nil {
		f.CostFloor = deps.RenderCost(out)
	}
	body, err := Render(tmpl.Template, f)
	if err != nil {
		// A row that no longer validates (an edit outside the seed's eye)
		// refuses at render too — the named template, never a silent body.
		deps.logf("retro: template %q refused to render: %v", fam, err)
		return nil
	}
	tags := []string{"retro", "retro:outcome:" + out.OutcomeKind}
	if out.BlueprintID != "" {
		tags = append(tags, "retro:blueprint:"+out.BlueprintID)
	}
	count := n
	slug := out.LoopSlug
	got, refusal := deps.L.AppendAs(learnings.AppendInput{
		Scope:          learnings.ScopeProject,
		ScopeKey:       out.ComponentRoot,
		Body:           body,
		Provenance:     learnings.ProvenanceObserved,
		ObservedCount:  &count,
		Tags:           tags,
		SourceLoopSlug: &slug,
	}, Actor)
	if refusal != nil {
		// RT4 §1: a refusal is logged, no link is laid — reconcile retries
		// it at the next start. The fact row stands either way.
		deps.logf("retro: the learning for outcome %d was refused: %s", out.ID, refusal.Reason)
		return nil
	}
	linkedNow, err := deps.S.LinkRetroLearning(out.ID, got.ID)
	if err != nil {
		return fmt.Errorf("retro learning: link %d/%d: %w", out.ID, got.ID, err)
	}
	if !linkedNow {
		// The UNIQUE answered "already linked" under us — a racing writer
		// won, and this Append is an orphan. Named, never smoothed: the
		// window is a concurrent reconcile+reactor race, which the boot
		// order (reconcile before the reactor can fire) keeps astronomically small.
		deps.logf("retro: outcome %d was already linked by another writer — learning %d is an orphan from a race", out.ID, got.ID)
	}
	return nil
}

// costState reads the honesty word out of the fact row's own counters —
// unknown if any step had no number, partial if any floor was partial,
// else known. The recorder's Rollup floor-flag is not a column (RT4: no
// stored tallies); a plain total needs no marker to be read as a total.
func costState(o Outcome) string {
	switch {
	case o.CostUSDFloor == nil || o.CostSteps == nil:
		// A floor with no step count is not a cost statement — the two
		// travel together or the state cannot be read.
		return notRecorded
	case o.CostUnknownSteps != nil && *o.CostUnknownSteps > 0:
		return "unknown"
	case o.CostPartialSteps != nil && *o.CostPartialSteps > 0:
		return "partial"
	default:
		return "known"
	}
}

// wallWord renders the stored span as a Go duration string, or the one
// NULL word. No prose formats here: "2h3m4s" is the machine-readable
// honest shape (history's human sentences are history's renderer).
func wallWord(o Outcome) string {
	if o.WallMS == nil {
		return notRecorded
	}
	return (time.Duration(*o.WallMS) * time.Millisecond).String()
}

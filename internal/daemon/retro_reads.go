package daemon

// retro_reads.go — S63 T5 (RT10, RT13): the daemon's door onto the
// outcome facts. Three reads and the one human door.
//
// `describe` is RT10's sentence, and it renders ONLY from ids and enums
// — the row's own loop slug, root, kind, its known counters and its cost
// floor — because the fact tables hold no free text and the sentence is
// not allowed to smuggle any in. What the row does not know is SAID in
// the sentence ("(1 of 6 steps unknown)"), never smoothed to zero (P31)
// and never left for the client to re-derive (the one formatter's law).

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

type retroReads struct {
	d *Daemon
}

func newRetroReads(d *Daemon) *retroReads { return &retroReads{d: d} }

var _ protocol.RetroService = (*retroReads)(nil)

// --- describe (RT10's one sentence-shape) -------------------------------------

// describeRetroOutcome renders the row's sentence. Grammar branches are
// by outcome KIND (enum), never by remembered prose; unknown counters
// drop their clause, and a row that knows no cost says so.
func describeRetroOutcome(o store.RetroOutcome) string {
	verb := "ended with outcome " + o.OutcomeKind
	switch {
	case o.OutcomeKind == "done":
		verb = "finished done"
	case o.OutcomeKind == "aborted":
		verb = "ended aborted"
	case strings.HasPrefix(o.OutcomeKind, "blocked"):
		verb = "ended " + o.OutcomeKind
	}
	var b strings.Builder
	fmt.Fprintf(&b, "loop %q on %s %s", o.LoopSlug, o.ComponentRoot, verb)
	if o.ReviewRounds != nil {
		n := *o.ReviewRounds
		if n > 0 {
			fmt.Fprintf(&b, " after %d review round%s", n, plural(n))
		}
	}
	switch {
	case o.CostUSDFloor != nil:
		fmt.Fprintf(&b, ", cost at least $%.2f", *o.CostUSDFloor)
		if o.CostUnknownSteps != nil && o.CostSteps != nil && *o.CostUnknownSteps > 0 {
			fmt.Fprintf(&b, " (%d of %d steps unknown)", *o.CostUnknownSteps, *o.CostSteps)
		} else if o.CostUnknownSteps != nil && *o.CostUnknownSteps > 0 {
			fmt.Fprintf(&b, " (%d steps unknown)", *o.CostUnknownSteps)
		}
	default:
		b.WriteString(", cost not recorded")
	}
	if !o.Complete {
		b.WriteString(", counters not-recorded (partial)")
	}
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// --- the reads ----------------------------------------------------------------

func (r *retroReads) List(ctx context.Context, p protocol.RetroListParams) (protocol.RetroListResult, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.d.store.ListRetroOutcomes(store.RetroFilter{
		Root: p.ID, Agent: p.Agent, Blueprint: p.Blueprint, Outcome: p.Outcome,
		Since: p.Since, Until: p.Until, Limit: limit, Cursor: p.Cursor,
	})
	if err != nil {
		return protocol.RetroListResult{}, protocol.ErrRetroUnavailable.WithWhat("%v", err)
	}
	out := protocol.RetroListResult{Statement: protocol.ScorecardStatement}
	for _, o := range rows {
		out.Rows = append(out.Rows, retroOutcomeWire(o))
	}
	if len(rows) > 0 {
		out.NextCursor = rows[len(rows)-1].ID
	}
	return out, nil
}

func (r *retroReads) Show(ctx context.Context, p protocol.RetroShowParams) (protocol.RetroShowResult, error) {
	row, found, err := r.d.store.RetroOutcomeByID(p.OutcomeID)
	if err != nil {
		return protocol.RetroShowResult{}, protocol.ErrRetroUnavailable.WithWhat("%v", err)
	}
	if !found {
		return protocol.RetroShowResult{}, protocol.ErrScorecardRefused.WithWhat(
			"no retro outcome has id %d — retro.list hands out the ids", p.OutcomeID)
	}
	agents, err := r.d.store.RetroAgentsFor(p.OutcomeID)
	if err != nil {
		return protocol.RetroShowResult{}, protocol.ErrRetroUnavailable.WithWhat("%v", err)
	}
	out := protocol.RetroShowResult{
		Row:       retroOutcomeWire(row),
		Statement: protocol.ScorecardStatement,
	}
	for _, a := range agents {
		out.Agents = append(out.Agents, retroAgentWire(a))
	}
	if linkID, ok, err := r.d.store.RetroLearningLink(p.OutcomeID); err != nil {
		return protocol.RetroShowResult{}, protocol.ErrRetroUnavailable.WithWhat("%v", err)
	} else if ok {
		lr, foundL, err := r.d.store.GetLearning(linkID)
		if err != nil {
			return protocol.RetroShowResult{}, protocol.ErrRetroUnavailable.WithWhat("%v", err)
		}
		if foundL {
			// A learning a person RETRACTED (S52 LM2) is stated as
			// retracted here too — the link survives (LM2's stamp law)
			// and the door says what is true of the row it points at.
			out.Learning = &protocol.LearningLinkWire{
				ID: lr.ID, Provenance: lr.Provenance, Body: lr.Body, RetractedAt: lr.RetractedAt,
			}
		}
	}
	return out, nil
}

func (r *retroReads) Status(ctx context.Context) (protocol.RetroStatusResult, error) {
	since, loops, err := r.d.store.RetroCoverage()
	if err != nil {
		return protocol.RetroStatusResult{}, protocol.ErrRetroUnavailable.WithWhat("%v", err)
	}
	outcomes, learnings, err := r.d.store.RetroStatusCounts()
	if err != nil {
		return protocol.RetroStatusResult{}, protocol.ErrRetroUnavailable.WithWhat("%v", err)
	}
	out := protocol.RetroStatusResult{
		RecordedSince:    since,
		LoopsRecorded:    loops,
		Dropped:          retroDroppedCount(),
		Outcomes:         outcomes,
		LearningsWritten: learnings,
		Statement:        protocol.ScorecardStatement,
	}
	if last := retroLastFacts(); !last.IsZero() {
		out.LastWriteAt = last.UTC().Format(time.RFC3339Nano)
	}
	at, written, ran := retroReconcileState()
	out.Reconcile = protocol.RetroReconcileWire{NeverRun: !ran, LastWritten: written}
	if ran {
		out.Reconcile.LastRunAt = at.Format(time.RFC3339Nano)
	}
	// RT15's optional model retrospective: the setting is not declared
	// and the retrospective blueprint is not built (that is T8's door),
	// so the honest state is enabled:false with the reason NAMED — an
	// absent option is stated, never pretended away.
	out.ModelRetro = protocol.RetroModelRetroWire{
		Enabled:   false,
		BlockedBy: "no retro.modelEnabled setting declared and no retrospective blueprint built yet (S63 T8)",
	}
	return out, nil
}

// --- the one human door (RT13) --------------------------------------------------

func (r *retroReads) Backfill(ctx context.Context, _ protocol.Actor, p protocol.RetroBackfillParams) (protocol.RetroBackfillWire, error) {
	dryRun := true
	if p.DryRun != nil {
		dryRun = *p.DryRun
	}
	// The door already refused a write without yes:true; this is the
	// same law seen from the service, kept because the direct CLI arm
	// (aeon retro backfill, T7) arrives through HERE too.
	rep, err := r.d.RetroBackfill(ctx, p.Root, dryRun)
	if err != nil {
		return protocol.RetroBackfillWire{}, protocol.ErrRetroUnavailable.WithWhat("%v", err)
	}
	return protocol.RetroBackfillWire{
		Scanned:         rep.Scanned,
		DryRun:          rep.DryRun,
		ByKind:          rep.ByKind,
		SkippedRecorded: rep.SkippedRecorded,
		SkippedRule:     rep.SkippedRule,
		SkippedOpen:     rep.SkippedOpen,
		SkippedNoScope:  rep.SkippedNoScope,
		Written:         rep.Written,
		Statement:       protocol.ScorecardStatement,
	}, nil
}

// --- the mirror -----------------------------------------------------------------

func retroOutcomeWire(o store.RetroOutcome) protocol.RetroOutcomeWire {
	return protocol.RetroOutcomeWire{
		ID: o.ID, ComponentRoot: o.ComponentRoot, LoopSlug: o.LoopSlug,
		TerminalKey: o.TerminalKey, OutcomeKind: o.OutcomeKind,
		BlueprintID: o.BlueprintID, BlueprintVersion: o.BlueprintVersion,
		StartedByKind: o.StartedByKind, Attends: o.Attended,
		GoalID: o.GoalID, GoalVersion: o.GoalVersion,
		WallMS: o.WallMS, CostFloor: o.CostUSDFloor,
		CostSteps: o.CostSteps, CostPartialSteps: o.CostPartialSteps, CostUnknownSteps: o.CostUnknownSteps,
		TasksDone: o.TasksDone, TasksFailed: o.TasksFailed, Retries: o.Retries,
		ReviewRounds: o.ReviewRounds, SecurityRounds: o.SecurityRounds, QARounds: o.QARounds,
		TotalFailedGates: o.TotalFailedGates, SplitsDone: o.SplitsDone, Escalations: o.Escalations,
		UnverifiedSteps: o.UnverifiedSteps, VerifiedPass: o.VerifiedPass, VerifiedFail: o.VerifiedFail,
		Complete: o.Complete, Source: o.Source, RecordedAt: o.RecordedAt,
		Describe: describeRetroOutcome(o),
	}
}

func retroAgentWire(a store.RetroAgent) protocol.RetroAgentWire {
	return protocol.RetroAgentWire{
		Agent: a.Agent, AgentSlugKnown: a.AgentSlugKnown,
		Steps: a.Steps, StepsDone: a.StepsDone, StepsRejected: a.StepsRejected, StepsBlocked: a.StepsBlocked,
		VerifyPass: a.VerifyPass, VerifyFail: a.VerifyFail, VerifyUnverified: a.VerifyUnverified,
		CostFloor: a.CostUSDFloor, CostPartialSteps: a.CostPartialSteps, CostUnknownSteps: a.CostUnknownSteps,
		AgentMS: a.AgentMS,
	}
}

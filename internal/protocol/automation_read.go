package protocol

// automation_read.go — S49 T3 (AN1, AN5, AN7): the unified automation READS
// on the socket — ten methods over ONE view of both families (S20's clock/
// webhook `trigger_schedule` rows and S40's event `event_trigger` rows), so
// the desk answers "what automations exist, what each does in plain words,
// what happened, what loops it started, whether it could fire right now,
// when it fires next, whether a sample event would match, and who changed
// what" without a client ever union-ing two tables itself.
//
// The existing `trigger.*` and `automation.trigger.*` reads are untouched —
// these ten are a NEW surface over the same rows, and no old wire shape
// changed (T3's compat rule; the S20/S40 fixtures are byte-identical).
//
// The seam rule is V13: all ten register ONLY over a non-nil Table.AutomationReads
// (RegisterAutomationReads is a no-op otherwise — absent, never
// registered-and-refusing), each RequiresInitialize, none Debug, and none
// enters narrow.Actions() (AN7: reads are nothing an operator cannot do;
// the narrow guard stays pinned and this slice adds no entry).
//
// The views carry what the rows carry: a schedule's expression and zone, an
// event trigger's canonical match object and kickoff TEMPLATE — never a
// rendered body (S40's rule, kept: a read cannot leak a goal's text through
// a preview). The plain-words description is the daemon's, composed by
// automationview.Describe from the row's own fields (AN5 — the one sentence
// every surface prints verbatim); the adapter builds it, this file only
// carries it.
//
// Nothing here writes, claims, or gates (AN8, behaviour notes 1–2): these
// handlers call the seam and shape the answer. The engines' run-time checks
// are not consulted and cannot be — a read that could change what fires is
// the second authority the design forbids.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// automationSourceNames is AN2's closed three — the family a row belongs to,
// spelled once for every filter and refusal in this file.
var automationSourceNames = []string{"schedule", "webhook", "event"}

func isAutomationSource(s string) bool {
	for _, n := range automationSourceNames {
		if n == s {
			return true
		}
	}
	return false
}

// automationArchivedNames is automation.list's archived filter: exclude
// (default — AN6's lists hide the archived), include (live and archived
// side by side), only (the archive itself).
var automationArchivedNames = []string{"exclude", "include", "only"}

// AutomationWindowView is a schedule window's three parts as one object.
// Days is the stored form (the grammar's own comma list — the canonical
// bytes, which the vocabulary read explains); automationview renders it.
type AutomationWindowView struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Days  string `json:"days,omitempty"`
}

// AutomationFireView is one row of the UNIFIED fire log (T1's
// automation_fire_view over both fire tables). Outcome is AN12's closed
// five; reason rides VERBATIM from the engine row, because the reason is the
// answer and a paraphrase is a second authority. loopId/loopSlug are present
// exactly when the outcome was "started".
type AutomationFireView struct {
	Source      string `json:"source"`
	TriggerID   string `json:"triggerId"`
	FireID      int64  `json:"fireId"` // the source row's id — the cursor's second half
	At          string `json:"at"`
	Outcome     string `json:"outcome"`
	Reason      string `json:"reason"`
	LoopID      string `json:"loopId,omitempty"`
	LoopSlug    string `json:"loopSlug,omitempty"`
	SubjectKind string `json:"subjectKind,omitempty"`
	SubjectID   string `json:"subjectId,omitempty"`
	Depth       int    `json:"depth,omitempty"`
	CostNote    string `json:"costNote,omitempty"`
}

// AutomationView is one automation of either family as the desk sees it.
// Family-specific fields are omitted when they belong to the other family
// (expr/zone/window for schedules and webhooks — the latter carries none;
// match/kickoff for events), so a page reads one shape, not a union.
type AutomationView struct {
	Source      string                `json:"source"`
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Blueprint   string                `json:"blueprint"`
	Workspace   string                `json:"workspace,omitempty"`
	Goal        string                `json:"goal,omitempty"`
	Expr        string                `json:"expr,omitempty"`
	Zone        string                `json:"zone,omitempty"`
	Window      *AutomationWindowView `json:"window,omitempty"`
	SecretEnv   string                `json:"secretEnv,omitempty"`
	// EventFilter is a webhook's `event` or `event:action,action`; empty listens to every signed delivery.
	EventFilter string                `json:"eventFilter,omitempty"`
	Match       json.RawMessage       `json:"match,omitempty"`
	Kickoff     string                `json:"kickoff,omitempty"`
	RootID      string                `json:"rootId,omitempty"`
	ComponentID string                `json:"componentId,omitempty"`

	MaxRunsPerHour   int      `json:"maxRunsPerHour,omitempty"`
	MaxCostPerDayUSD *float64 `json:"maxCostPerDayUsd,omitempty"`

	Enabled        bool   `json:"enabled"`
	Paused         bool   `json:"paused,omitempty"`
	Archived       bool   `json:"archived,omitempty"`
	ArchivedReason string `json:"archivedReason,omitempty"`
	DisabledReason string `json:"disabledReason,omitempty"`

	CreatedByKind string `json:"createdByKind,omitempty"`
	CreatedByID   string `json:"createdById,omitempty"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt,omitempty"`

	// LastFire is the automation's most recent recorded answer — null when
	// it never fired, which the page renders as "never", never as broken
	// (S40's EventTriggerView rule, kept for the unified view).
	LastFire *AutomationFireView `json:"lastFire"`
}

// ── automation.list ───────────────────────────────────────────────────────

type AutomationListParams struct {
	Source      string `json:"source,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Archived    string `json:"archived,omitempty"`
	Blueprint   string `json:"blueprint,omitempty"`
	RootID      string `json:"rootId,omitempty"`
	ComponentID string `json:"componentId,omitempty"`
	// Q is a substring over name, id and the plain-words description — the
	// desk's one search box.
	Q string `json:"q,omitempty"`
}

type AutomationListResult struct {
	Automations []AutomationView `json:"automations"`
	// Statement names WHICH empty this is when the list is empty, and is
	// absent otherwise: "no automations match this filter." is the filter's
	// answer, "no automations. nothing starts agents on its own." is the
	// store's — conflating them is how a typo reads as peace (AN5).
	Statement string `json:"statement,omitempty"`
}

func (p AutomationListParams) validate() error {
	if p.Source != "" && !isAutomationSource(p.Source) {
		return ErrBadParams{Err: fmt.Errorf("source %q is not one of schedule, webhook, event", p.Source)}
	}
	if p.Archived != "" {
		for _, a := range automationArchivedNames {
			if a == p.Archived {
				return nil
			}
		}
		return ErrBadParams{Err: fmt.Errorf("archived %q is not one of exclude, include, only", p.Archived)}
	}
	return nil
}

// filtered says whether ANY narrowing was asked — the two empties are
// distinguished by this, so "archived:only" with zero archives answers the
// filter's sentence, not the store's.
func (p AutomationListParams) filtered() bool {
	return p.Source != "" || p.Enabled != nil || p.Archived != "" || p.Blueprint != "" ||
		p.RootID != "" || p.ComponentID != "" || p.Q != ""
}

func (t *Table) handleAutomationList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p AutomationListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return AnswerAutomationList(ctx, t.AutomationReads, p)
}

// AnswerAutomationList is the ONE rendering of the list door's answer —
// validate, seam, the two-empties wrap — shared by the person's socket door
// and S49 T7's automation_list tool arm (rule 4: the tool answer IS the
// socket answer, one implementation, never a second wrapping of the same
// rows). An arm that re-spelled the wrap would be a second truth about
// which empty is which.
func AnswerAutomationList(ctx context.Context, reads AutomationReads, p AutomationListParams) (any, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	views, liveTotal, err := reads.ListAutomations(ctx, p)
	if err != nil {
		return nil, err
	}
	res := AutomationListResult{Automations: views}
	if len(views) == 0 {
		if p.filtered() || liveTotal > 0 {
			res.Statement = "no automations match this filter."
		} else {
			res.Statement = "no automations. nothing starts agents on its own."
		}
	}
	return res, nil
}

// ── automation.show ───────────────────────────────────────────────────────

type AutomationShowParams struct {
	Source string `json:"source"`
	ID     string `json:"id"`
}

// AutomationCanonical is the family's canonical object — the exact fields a
// write of that family takes. The event family's carries the stored (already
// canonical) match object and the kickoff TEMPLATE, never a rendered body.
type AutomationCanonical struct {
	Expr      string                `json:"expr,omitempty"`
	Zone      string                `json:"zone,omitempty"`
	Window    *AutomationWindowView `json:"window,omitempty"`
	SecretEnv string                `json:"secretEnv,omitempty"`
	Match     json.RawMessage       `json:"match,omitempty"`
	Kickoff   string                `json:"kickoff,omitempty"`
}

type AutomationShowResult struct {
	Automation  AutomationView         `json:"automation"`
	Schedule    *AutomationCanonical   `json:"schedule,omitempty"`
	Webhook     *AutomationCanonical   `json:"webhook,omitempty"`
	Event       *AutomationCanonical   `json:"event,omitempty"`
	Description string                 `json:"description"`
	Health      AutomationHealthView   `json:"health"`
	RecentFires []AutomationFireView   `json:"recentFires"`
	Changes     []AutomationChangeView `json:"changes"`
}

func (p AutomationShowParams) validate() error {
	if p.ID == "" {
		return ErrBadParams{Err: fmt.Errorf("`id` is required — the automation is named by its id")}
	}
	if !isAutomationSource(p.Source) {
		return ErrBadParams{Err: fmt.Errorf("source %q is not one of schedule, webhook, event", p.Source)}
	}
	return nil
}

func (t *Table) handleAutomationShow(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p AutomationShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return AnswerAutomationShow(ctx, t.AutomationReads, p)
}

// AnswerAutomationShow is the one rendering of the show door — same reason,
// same shared pair: the person's socket door and S49 T7's automation_show
// tool arm run THIS (rule 4).
func AnswerAutomationShow(ctx context.Context, reads AutomationReads, p AutomationShowParams) (any, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	res, err := reads.ShowAutomation(ctx, p)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ── automation.fires ──────────────────────────────────────────────────────

type AutomationFiresParams struct {
	Source string `json:"source,omitempty"`
	ID     string `json:"id,omitempty"`
	// Outcome is AN12's closed five; the words are the seam's own list
	// (eventFireOutcomeNames above — pinned against the store's in the
	// daemon's reader test, so this handler refuses with no second list).
	Outcome string `json:"outcome,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	// BeforeId is the cursor of the PREVIOUS page's last row, handed out as
	// nextBeforeId ("<at>|<source>:<fireId>") and read back the same way —
	// the log is newest first and paging never re-reads a page.
	BeforeId string `json:"beforeId,omitempty"`
}

// AutomationFireCounts is the per-outcome tally for the same
// source/trigger filters (the counts ignore the outcome filter and the
// cursor — one counting rule serves both logs, TR4's convention).
type AutomationFireCounts struct {
	Started     int `json:"started"`
	Refused     int `json:"refused"`
	Limited     int `json:"limited"`
	Skipped     int `json:"skipped"`
	CouldNotSay int `json:"couldNotSay"`
}

type AutomationFiresResult struct {
	Fires        []AutomationFireView `json:"fires"`
	Counts       AutomationFireCounts `json:"counts"`
	NextBeforeId string               `json:"nextBeforeId,omitempty"`
}

func (p AutomationFiresParams) validate() error {
	if p.Source != "" && !isAutomationSource(p.Source) {
		return ErrBadParams{Err: fmt.Errorf("source %q is not one of schedule, webhook, event", p.Source)}
	}
	if p.Outcome != "" && !isEventFireOutcome(p.Outcome) {
		return ErrBadParams{Err: fmt.Errorf("outcome %q is not one of %s", p.Outcome, strings.Join(eventFireOutcomeNames, ", "))}
	}
	if p.Limit < 0 {
		return ErrBadParams{Err: fmt.Errorf("limit must not be negative, got %d", p.Limit)}
	}
	return nil
}

func (t *Table) handleAutomationFires(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p AutomationFiresParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return AnswerAutomationFires(ctx, t.AutomationReads, p)
}

// AnswerAutomationFires is the one rendering of the fire log — same shared
// pair as the list and the show (rule 4): the person's socket door and
// S49 T7's automation_fires tool arm run THIS.
func AnswerAutomationFires(ctx context.Context, reads AutomationReads, p AutomationFiresParams) (any, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	fires, counts, next, err := reads.ListAutomationFires(ctx, p)
	if err != nil {
		return nil, err
	}
	return AutomationFiresResult{Fires: fires, Counts: counts, NextBeforeId: next}, nil
}

// ── automation.runs ───────────────────────────────────────────────────────

// AutomationRunView is one loop an automation started, attributed by
// provenance (loop.started_by + loop.trigger_id — 0033/0039, the same fact
// the fire log's loop columns carry, phrased for the loop table).
type AutomationRunView struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Status    string `json:"status"`
	StartedAt string `json:"startedAt"`
}

type AutomationRunsParams struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Limit  int    `json:"limit,omitempty"`
}

type AutomationRunsResult struct {
	Loops []AutomationRunView `json:"loops"`
	Total int                 `json:"total"`
}

func (p AutomationRunsParams) validate() error {
	if p.ID == "" {
		return ErrBadParams{Err: fmt.Errorf("`id` is required — the runs of one named automation are listed")}
	}
	if !isAutomationSource(p.Source) {
		return ErrBadParams{Err: fmt.Errorf("source %q is not one of schedule, webhook, event", p.Source)}
	}
	if p.Limit < 0 {
		return ErrBadParams{Err: fmt.Errorf("limit must not be negative, got %d", p.Limit)}
	}
	return nil
}

func (t *Table) handleAutomationRuns(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p AutomationRunsParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return AnswerAutomationRuns(ctx, t.AutomationReads, p)
}

// AnswerAutomationRuns is the runs door's ONE rendering (S49 T8: extracted
// from the handler — the socket and the CLI's offline door now answer
// through this function, the stamping rule-4 shape T7 gave the other three).
func AnswerAutomationRuns(ctx context.Context, reads AutomationReads, p AutomationRunsParams) (any, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	return reads.AutomationRuns(ctx, p)
}

// ── automation.changes ────────────────────────────────────────────────────

// AutomationChangeView is one row of automation_change (T1): who changed
// what, newest first. The actor is the person the write path stamped — the
// audit, not a claim (AN4).
type AutomationChangeView struct {
	ID         int64  `json:"id"`
	Source     string `json:"source"`
	TriggerID  string `json:"triggerId"`
	Op         string `json:"op"`
	Summary    string `json:"summary"`
	ActorKind  string `json:"actorKind"`
	ActorID    string `json:"actorId"`
	OccurredAt string `json:"occurredAt"`
}

type AutomationChangesParams struct {
	Source string `json:"source,omitempty"`
	ID     string `json:"id,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type AutomationChangesResult struct {
	Changes []AutomationChangeView `json:"changes"`
	// Statement is "no automation changes recorded." — ONLY when the
	// listing is empty (the filter's answer over a non-empty audit is an
	// empty list, never that sentence, which would misdescribe the audit).
	Statement string `json:"statement,omitempty"`
}

func (p AutomationChangesParams) validate() error {
	if p.Source != "" && !isAutomationSource(p.Source) {
		return ErrBadParams{Err: fmt.Errorf("source %q is not one of schedule, webhook, event", p.Source)}
	}
	if p.Limit < 0 {
		return ErrBadParams{Err: fmt.Errorf("limit must not be negative, got %d", p.Limit)}
	}
	return nil
}

func (t *Table) handleAutomationChanges(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p AutomationChangesParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return AnswerAutomationChanges(ctx, t.AutomationReads, p)
}

// AnswerAutomationChanges is the changes door's ONE rendering (S49 T8, same
// extraction; the empty-audit Statement stays INSIDE the rendering so a
// second door cannot drop the sentence AN5 requires).
func AnswerAutomationChanges(ctx context.Context, reads AutomationReads, p AutomationChangesParams) (any, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	changes, err := reads.ListAutomationChanges(ctx, p)
	if err != nil {
		return nil, err
	}
	res := AutomationChangesResult{Changes: changes}
	if len(changes) == 0 {
		res.Statement = "no automation changes recorded."
	}
	return res, nil
}

// AutomationReads is the daemon's share (S33/S35 pattern — the store stays
// behind it): it reads the rows and composes the views, the descriptions,
// the health and the previews. Failures arrive as plain errors except the
// concrete *protocol.ErrAutomationUnknown for an id no family carries, and
// ErrBadParams/ErrAutomationPreview for a preview the clock cannot answer —
// the handler returns those untouched, because a paraphrase of a validator
// sentence would be a second authority over a fact the validator owns.
// ListAutomations also answers liveTotal: the count of NON-archived
// automations ignoring every filter, which is what lets the handler tell the
// two empties apart (an empty filtered list over a populated store is NOT
// the "nothing exists" sentence).
type AutomationReads interface {
	ListAutomations(ctx context.Context, p AutomationListParams) ([]AutomationView, int, error)
	ShowAutomation(ctx context.Context, p AutomationShowParams) (AutomationShowResult, error)
	ListAutomationFires(ctx context.Context, p AutomationFiresParams) ([]AutomationFireView, AutomationFireCounts, string, error)
	AutomationRuns(ctx context.Context, p AutomationRunsParams) (AutomationRunsResult, error)
	ListAutomationChanges(ctx context.Context, p AutomationChangesParams) ([]AutomationChangeView, error)
	// The advisory three (their contract is at the top of
	// automation_advice.go): health inputs, the clock preview, the dry run.
	AutomationHealth(ctx context.Context, p AutomationHealthParams) ([]AutomationHealthView, error)
	PreviewAutomations(ctx context.Context, p AutomationPreviewParams) ([]AutomationInstant, error)
	DryRunAutomation(ctx context.Context, p AutomationDryRunParams) (AutomationDryRunResult, error)
}

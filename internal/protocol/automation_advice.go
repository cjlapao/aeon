package protocol

// automation_advice.go — S49 T3's second half of the unified reads: the
// ADVISORY five (health, vocabulary, preview, test, dryrun), split from
// automation_read.go by cohesion — those five read rows as they are; these
// five RUN production code over an input (the S48 validator, the engine's
// clock walk, the grammar's matcher) and report what it would answer. The
// rule the whole file serves (AN8): an advisory read runs the SAME code the
// engine runs and quotes its sentences verbatim — a second opinion, never a
// second authority. None of these can change what fires: T3's tests prove
// the engines behave identically with every advisory read open, and a dry
// run inserts nothing anywhere.
//
// The two refusals this plane speaks:
//
//   - ErrAutomationUnknown — an id no family carries, naming the nearest
//     ids (a typo reads as a missing thing, never as an empty answer).
//   - ErrAutomationPreview — the clock or the grammar REFUSED the thing
//     asked: the validator's sentence rides VERBATIM in `what` (TR6's zone
//     refusals, the cron grammar's own words), because a paraphrase here is
//     the second authority AN8 exists to forbid. A params-shape problem is
//     JSON-RPC's -32602 (ErrBadParams); this code is for a well-shaped
//     request the engine itself declines.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/automationview"
)

// ── automation.health ─────────────────────────────────────────────────────

// AutomationBlueprintHealth is the blueprint check's line: the STATE from
// S48's rows read through the same validation `loop.start` runs
// (ok|missing|invalid|unverifiable — unverifiable means the read is not
// wired, said out loud rather than guessed as ok), and the validator's
// counts for the detail row.
type AutomationBlueprintHealth struct {
	State    string `json:"state"`
	Errors   int    `json:"errors,omitempty"`
	Warnings int    `json:"warnings,omitempty"`
}

// AutomationSecretHealth reports PRESENCE only: a bool plus the env NAME.
// The value is never read into anything that outlives the call and never
// travels on the wire, in a log, or in the audit (C17, T3 behaviour note 3).
type AutomationSecretHealth struct {
	EnvName string `json:"envName"`
	Set     bool   `json:"set"`
}

// AutomationExists is one exists-check.
type AutomationExists struct {
	Exists bool `json:"exists"`
}

// AutomationHealthView is one automation's advisory verdict. It NEVER gates
// (AN8): the fire log's outcome is the engine's answer; this is advice with
// its reasons, and canFire is "every input says ready", computed by
// automationview.ComposeHealth — the one composer, called by the adapter.
type AutomationHealthView struct {
	Source    string                    `json:"source"`
	ID        string                    `json:"id"`
	Blueprint AutomationBlueprintHealth `json:"blueprint"`
	Webhook   *AutomationSecretHealth   `json:"webhook,omitempty"`
	Workspace AutomationExists          `json:"workspace"`
	Scope     AutomationExists          `json:"scope"`
	CanFire   bool                      `json:"canFire"`
	Reasons   []string                  `json:"reasons"`
}

type AutomationHealthParams struct {
	Source string `json:"source,omitempty"`
	ID     string `json:"id,omitempty"`
}

type AutomationHealthResult struct {
	Health []AutomationHealthView `json:"health"`
	// EvaluatedAt is the instant the inputs were read — health is a snapshot
	// of live state (N8: no cache across calls), so the stamp is honest
	// about which moment it describes.
	EvaluatedAt string `json:"evaluatedAt"`
}

func (p AutomationHealthParams) validate() error {
	if p.Source != "" && !isAutomationSource(p.Source) {
		return ErrBadParams{Err: fmt.Errorf("source %q is not one of schedule, webhook, event", p.Source)}
	}
	return nil
}

func (t *Table) handleAutomationHealth(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p AutomationHealthParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return AnswerAutomationHealth(ctx, t.AutomationReads, p)
}

// AnswerAutomationHealth is the one rendering of the health door — the
// shared pair again (rule 4): the person's socket door and S49 T7's
// automation_health tool arm run THIS, stamp including the honest
// evaluatedAt of the moment the inputs were read (N8: no cache, and the
// stamp says which moment the snapshot describes).
func AnswerAutomationHealth(ctx context.Context, reads AutomationReads, p AutomationHealthParams) (any, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	rows, err := reads.AutomationHealth(ctx, p)
	if err != nil {
		return nil, err
	}
	return AutomationHealthResult{Health: rows, EvaluatedAt: now().UTC().Format("2006-01-02T15:04:05Z07:00")}, nil
}

// ── automation.vocabulary ─────────────────────────────────────────────────

// handleAutomationVocabulary answers with automationview.Vocabulary() —
// the vocabulary the VALIDATOR derives (applicability probed through the
// grammar's own Validate), so no form hardcodes a word list and the read
// cannot drift from the code that enforces it (T3's test pins equality).
func (t *Table) handleAutomationVocabulary(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p struct{}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return automationview.Vocabulary(), nil
}

// ── automation.preview ────────────────────────────────────────────────────

// AutomationPreviewSpec is a bare schedule spec — the editor's unsaved form,
// previewed before it is written. Zone is the expression's wall-clock
// contract (TR6), so a spec without one is refused by the engine loader,
// verbatim, through ErrAutomationPreview.
type AutomationPreviewSpec struct {
	Expr   string                `json:"expr"`
	Zone   string                `json:"zone"`
	Window *AutomationWindowView `json:"window,omitempty"`
}

type AutomationPreviewParams struct {
	// Exactly one of ID (preview a stored schedule/webhook — webhooks have
	// no clock and the refusal says so) or Spec (preview the editor's
	// draft). An id naming an event trigger is refused the same shape: an
	// event fires on an event, never on a clock.
	ID   string                 `json:"id,omitempty"`
	Spec *AutomationPreviewSpec `json:"spec,omitempty"`
	N    int                    `json:"n,omitempty"`
	// From is RFC3339; the default is now.
	From string `json:"from,omitempty"`
}

// AutomationInstant is one previewed instant. Kind is the engine's own
// ("due" | "window" | "dst-collapsed" | "dst-missing") and Note is the
// engine's reason VERBATIM — "due" promises the clock will ASK, not that a
// loop will start (overlap, governance and the starter belong to the
// instant, not the preview).
type AutomationInstant struct {
	At   string `json:"at"`
	Kind string `json:"kind"`
	Note string `json:"note,omitempty"`
	Wall string `json:"wall"`
}

type AutomationPreviewResult struct {
	Instants []AutomationInstant `json:"instants"`
}

// previewDefault / previewMax mirror the engine's cap THROUGH the seam:
// automationview owns the number (trigger.PreviewLimit); this handler only
// refuses a request past it before the walk starts. previewMaxLocal is read
// from the vocabulary so the number is imported, not restated.
const previewDefault = 5

func (p AutomationPreviewParams) validate() error {
	// Reject BOTH-present and BOTH-absent; the XOR that survives is the
	// equality of the two absences — `(id absent) == (spec absent)`.
	if (p.ID == "") == (p.Spec == nil) {
		return ErrBadParams{Err: fmt.Errorf("exactly one of `id` and `spec` is required — a preview is of a stored automation or of a draft spec, never of nothing")}
	}
	if p.N < 0 {
		return ErrBadParams{Err: fmt.Errorf("n must not be negative, got %d", p.N)}
	}
	if p.N > automationview.Vocabulary().Limits.PreviewMax {
		return ErrBadParams{Err: fmt.Errorf("n %d is past the preview horizon (%d)", p.N, automationview.Vocabulary().Limits.PreviewMax)}
	}
	if p.From != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.From); err != nil {
			return ErrBadParams{Err: fmt.Errorf("`from` is not an RFC3339 instant: %v", err)}
		}
	}
	if p.Spec != nil && p.Spec.Expr == "" {
		return ErrBadParams{Err: fmt.Errorf("a draft spec needs its `expr` — the expression is what the preview walks")}
	}
	return nil
}

func (t *Table) handleAutomationPreview(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p AutomationPreviewParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return AnswerAutomationPreview(ctx, t.AutomationReads, p)
}

// AnswerAutomationPreview is the preview door's ONE rendering (S49 T8
// extraction; the XOR refusal and the horizon cap live in p.validate, the
// walk lives in the seam — the CLI's offline `preview --cron` road runs
// THIS function over a draft spec with no row read at all).
func AnswerAutomationPreview(ctx context.Context, reads AutomationReads, p AutomationPreviewParams) (any, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	insts, err := reads.PreviewAutomations(ctx, p)
	if err != nil {
		return nil, err
	}
	return AutomationPreviewResult{Instants: insts}, nil
}

// AnswerAutomationDraftPreview is the store-free preview rendering (S49
// T8): the SAME socket validation (the XOR, the horizon cap) runs verbatim,
// and the walk is INJECTED — the CLI's offline door passes
// daemon.PreviewDraftSpec, because protocol cannot import daemon and the
// walk's shape is this package's own. The result is the identical struct
// the socket door fills, which is what makes the two doors' `--json`
// byte-comparable.
func AnswerAutomationDraftPreview(p AutomationPreviewParams, walk func(AutomationPreviewParams) ([]AutomationInstant, error)) (any, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	insts, err := walk(p)
	if err != nil {
		return nil, err
	}
	return AutomationPreviewResult{Instants: insts}, nil
}

// ── automation.test ───────────────────────────────────────────────────────

// AutomationTestEvent is the flat sample a person pastes (the same six
// fields the grammar admits; absent fields say so by absence).
type AutomationTestEvent struct {
	Event       string `json:"event"`
	RootID      string `json:"rootId,omitempty"`
	ComponentID string `json:"componentId,omitempty"`
	GoalType    string `json:"goalType,omitempty"`
	TaskType    string `json:"taskType,omitempty"`
	SourceKind  string `json:"sourceKind,omitempty"`
}

type AutomationTestParams struct {
	Match  json.RawMessage     `json:"match"`
	Sample AutomationTestEvent `json:"sample"`
}

// AutomationTestKey is one key's row of the explanation panel: what the
// rule asks, what the event carries, whether it matched (an unconstrained
// key matches anything — want empty prints as such).
type AutomationTestKey struct {
	Key  string   `json:"key"`
	Want []string `json:"want"`
	Have string   `json:"have,omitempty"`
	OK   bool     `json:"ok"`
}

type AutomationTestResult struct {
	Matches   bool                `json:"matches"`
	PerKey    []AutomationTestKey `json:"perKey"`
	Canonical string              `json:"canonical"`
}

func (t *Table) handleAutomationTest(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p AutomationTestParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return AnswerAutomationTest(p)
}

// AnswerAutomationTest is the test door's ONE rendering (S49 T8: extracted
// from the handler; the store-free CLI door runs this exact function, which
// is WHY it can work with no daemon and no store — the grammar it shapes
// lives in automationview, not behind the seam).
func AnswerAutomationTest(p AutomationTestParams) (any, error) {
	if len(p.Match) == 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("`match` is required — the rule to test, in the stored form")}
	}
	// The verdict is the REAL grammar's (internal/match, the one the
	// dispatcher claims through): this renderer only shapes it. T3's
	// differential pins equality with the production matcher.
	res, err := automationview.TestMatch(p.Match, automationview.Event{
		Event: p.Sample.Event, RootID: p.Sample.RootID, ComponentID: p.Sample.ComponentID,
		GoalType: p.Sample.GoalType, TaskType: p.Sample.TaskType, SourceKind: p.Sample.SourceKind,
	})
	if err != nil {
		// The validator's sentence, verbatim — the same words a save would
		// answer, which is the point of testing before saving.
		return nil, ErrAutomationPreview.WithWhat("%s", err.Error())
	}
	out := AutomationTestResult{Matches: res.Matches, Canonical: res.Canonical}
	for _, k := range res.PerKey {
		out.PerKey = append(out.PerKey, AutomationTestKey{Key: k.Key, Want: k.Want, Have: k.Have, OK: k.OK})
	}
	return out, nil
}

// ── automation.dryrun ─────────────────────────────────────────────────────

type AutomationDryRunParams struct {
	ID string `json:"id"`
	// SinceOutboxId is where to start reading automation_event; the default
	// is the trigger's OWN cursor (FromOutboxID) — where the dispatcher left
	// off, so "would have fired" means exactly that.
	SinceOutboxId *int64 `json:"sinceOutboxId,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

// AutomationWouldMatch is one outbox row the trigger admits — a judgement
// about an event that is already here. It CLAIMS nothing (behaviour note 1:
// a pure scan; event_trigger_fire stays empty, the cursor stays where it
// was — T3's test counts rows before and after).
type AutomationWouldMatch struct {
	OutboxID    int64  `json:"outboxId"`
	Event       string `json:"event"`
	SubjectKind string `json:"subjectKind,omitempty"`
	SubjectID   string `json:"subjectId,omitempty"`
	At          string `json:"at"`
}

type AutomationDryRunResult struct {
	WouldMatch []AutomationWouldMatch `json:"wouldMatch"`
	Scanned    int                    `json:"scanned"`
	From       int64                  `json:"from"`
}

func (p AutomationDryRunParams) validate() error {
	if p.ID == "" {
		return ErrBadParams{Err: fmt.Errorf("`id` is required — a dry run is one named trigger's verdict on the waiting events")}
	}
	if p.Limit < 0 {
		return ErrBadParams{Err: fmt.Errorf("limit must not be negative, got %d", p.Limit)}
	}
	return nil
}

func (t *Table) handleAutomationDryRun(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p AutomationDryRunParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return AnswerAutomationDryRun(ctx, t.AutomationReads, p)
}

// AnswerAutomationDryRun is the dry-run door's ONE rendering (S49 T8
// extraction — the offline door answers through it over a directly opened
// store; the scan stays the seam's, cursor and all).
func AnswerAutomationDryRun(ctx context.Context, reads AutomationReads, p AutomationDryRunParams) (any, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	return reads.DryRunAutomation(ctx, p)
}

// The three advisory seam methods (AutomationHealth, PreviewAutomations,
// DryRunAutomation) live in the ONE AutomationReads interface beside the
// row-read methods (automation_read.go). Their contract: PreviewAutomations
// resolves an id (through the rows) or takes the draft spec, walks it with
// the engine's own preview, and answers ErrAutomationPreview with the
// validator's sentence verbatim when the walk refuses; an id whose row is a
// webhook (or an event) is ErrBadParams naming why. An id no family carries
// is ErrAutomationUnknown. AutomationHealth reads the live inputs (S48's
// rows through the same validation loop.start runs, an env NAME's presence
// via LookupEnv — never the value, the workspace, the scope) and composes
// every verdict with automationview.ComposeHealth. DryRunAutomation scans
// automation_event from the given (or the trigger's own) cursor and applies
// the trigger's parsed match — the production matcher — to each row,
// inserting NOTHING.

// RegisterAutomationReads registers the ten unified reads over a non-nil
// seam (V13, the RegisterInbox/RegisterBudget shape). It returns whether the
// methods are in the table, so the daemon's wiring can be asserted rather
// than trusted.
func (t *Table) RegisterAutomationReads() bool {
	if t.AutomationReads == nil {
		return false
	}
	for _, m := range []*Method{
		{Name: MethodAutomationList, RequiresInitialize: true, Handle: t.handleAutomationList},
		{Name: MethodAutomationShow, RequiresInitialize: true, Handle: t.handleAutomationShow},
		{Name: MethodAutomationFires, RequiresInitialize: true, Handle: t.handleAutomationFires},
		{Name: MethodAutomationRuns, RequiresInitialize: true, Handle: t.handleAutomationRuns},
		{Name: MethodAutomationHealth, RequiresInitialize: true, Handle: t.handleAutomationHealth},
		{Name: MethodAutomationVocabulary, RequiresInitialize: true, Handle: t.handleAutomationVocabulary},
		{Name: MethodAutomationPreview, RequiresInitialize: true, Handle: t.handleAutomationPreview},
		{Name: MethodAutomationTest, RequiresInitialize: true, Handle: t.handleAutomationTest},
		{Name: MethodAutomationDryRun, RequiresInitialize: true, Handle: t.handleAutomationDryRun},
		{Name: MethodAutomationChanges, RequiresInitialize: true, Handle: t.handleAutomationChanges},
	} {
		t.register(m)
	}
	return true
}

package protocol

// retro.go — S63 T5 (RT10, RT13, P4): the outcome-fact doors.
//
// Four methods over one seam. Three are reads (list, show, status); the
// fourth, retro.backfill, is the plane's ONE write and a person's door
// (RT13): dry-run by default, writing only with `yes: true` resent, the
// actor the CONNECTION's person — never a param (P4: an absent name on
// the narrow projection, the agent services, the tool registry and the
// records catalog; pinned by cmd/aeon/retro_guard_test.go).
//
// Every listed row arrives with a `describe` sentence rendered from IDS
// AND ENUMS only ("loop `x` on `y` finished done after 1 review round,
// cost at least $0.42 (1 of 6 steps unknown)") — RT10's own shape. Free
// text does not exist on these rows: the fact tables store ids, and the
// sentence renders what a row KNOWS, saying "(N of M steps unknown)"
// when it does not know everything (P31: unknown is said, never zeroed).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// --- the method names -------------------------------------------------------

const (
	MethodRetroList     MethodName = "retro.list"
	MethodRetroShow     MethodName = "retro.show"
	MethodRetroStatus   MethodName = "retro.status"
	MethodRetroBackfill MethodName = "retro.backfill"
)

// retroMethodNames is the plane's exact four.
func retroMethodNames() []MethodName {
	return []MethodName{MethodRetroList, MethodRetroShow, MethodRetroStatus, MethodRetroBackfill}
}

// --- the params ---------------------------------------------------------------

// RetroListParams filters the fact rows. ID resolves like a card subject
// (component id, or a component_root verbatim); empty spans every
// component. Cursor is a row id: pages move id DESC, and cursor 0 is
// the first page.
type RetroListParams struct {
	ID        string `json:"id,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Blueprint string `json:"blueprint,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	Since     string `json:"since,omitempty"`
	Until     string `json:"until,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Cursor    int64  `json:"cursor,omitempty"`
}

type RetroShowParams struct {
	OutcomeID int64 `json:"outcomeId"`
}

// RetroBackfillParams is RT13's door: dry-run by default; a write needs
// yes:true (and only writes backfill rows for loops the recorder never
// saw — a loop already recorded is skipped, never rewritten).
type RetroBackfillParams struct {
	Root   string `json:"root,omitempty"`
	DryRun *bool  `json:"dryRun,omitempty"`
	Yes    bool   `json:"yes,omitempty"`
}

// --- the results --------------------------------------------------------------

// RetroOutcomeWire is one fact row whole, in ids and enums, plus the
// rendered sentence. NULLs ride as nulls (P31): a field the row does not
// know is absent from the sentence and null on the wire. The recorded
// attendedness is ANSWERED here, so the field is spelled Attends, not
// Attended: internal/loop's G14 guard (TestQ4Recorded) counts Attended
// fields in this package as caller-settable doors, and this is not one —
// protocol/environment.go carries the same convention. The wire tag is
// unchanged.
type RetroOutcomeWire struct {
	ID               int64    `json:"id"`
	ComponentRoot    string   `json:"componentRoot"`
	LoopSlug         string   `json:"loopSlug"`
	TerminalKey      string   `json:"terminalKey"`
	OutcomeKind      string   `json:"outcomeKind"`
	BlueprintID      string   `json:"blueprintId,omitempty"`
	BlueprintVersion *int     `json:"blueprintVersion,omitempty"`
	StartedByKind    string   `json:"startedByKind,omitempty"`
	Attends          *bool    `json:"attended,omitempty"`
	GoalID           string   `json:"goalId,omitempty"`
	GoalVersion      *int     `json:"goalVersion,omitempty"`
	WallMS           *int64   `json:"wallMs,omitempty"`
	CostFloor        *float64 `json:"costFloor,omitempty"`
	CostSteps        *int     `json:"costSteps,omitempty"`
	CostPartialSteps *int     `json:"costPartialSteps,omitempty"`
	CostUnknownSteps *int     `json:"costUnknownSteps,omitempty"`
	TasksDone        *int     `json:"tasksDone,omitempty"`
	TasksFailed      *int     `json:"tasksFailed,omitempty"`
	Retries          *int     `json:"retries,omitempty"`
	ReviewRounds     *int     `json:"reviewRounds,omitempty"`
	SecurityRounds   *int     `json:"securityRounds,omitempty"`
	QARounds         *int     `json:"qaRounds,omitempty"`
	TotalFailedGates *int     `json:"totalFailedGates,omitempty"`
	SplitsDone       *int     `json:"splitsDone,omitempty"`
	Escalations      *int     `json:"escalations,omitempty"`
	UnverifiedSteps  *int     `json:"unverifiedSteps,omitempty"`
	VerifiedPass     *int     `json:"verifiedPass,omitempty"`
	VerifiedFail     *int     `json:"verifiedFail,omitempty"`
	Complete         bool     `json:"complete"`
	Source           string   `json:"source"`
	RecordedAt       string   `json:"recordedAt"`
	Describe         string   `json:"describe"`
}

type RetroAgentWire struct {
	Agent            string   `json:"agent"`
	AgentSlugKnown   bool     `json:"agentSlugKnown"`
	Steps            *int     `json:"steps,omitempty"`
	StepsDone        *int     `json:"stepsDone,omitempty"`
	StepsRejected    *int     `json:"stepsRejected,omitempty"`
	StepsBlocked     *int     `json:"stepsBlocked,omitempty"`
	VerifyPass       *int     `json:"verifyPass,omitempty"`
	VerifyFail       *int     `json:"verifyFail,omitempty"`
	VerifyUnverified *int     `json:"verifyUnverified,omitempty"`
	CostFloor        *float64 `json:"costFloor,omitempty"`
	CostPartialSteps *int     `json:"costPartialSteps,omitempty"`
	CostUnknownSteps *int     `json:"costUnknownSteps,omitempty"`
	AgentMS          *int64   `json:"agentMs,omitempty"`
}

// LearningLinkWire is the one learning a milestone wrote for this row —
// its body arrives VERBATIM (a stored learning is already-validated
// words; re-rendering it would be a second formatter, RT12's rule).
type LearningLinkWire struct {
	ID          int64   `json:"id"`
	Provenance  string  `json:"provenance"`
	Body        string  `json:"body"`
	RetractedAt *string `json:"retractedAt,omitempty"`
}

type RetroListResult struct {
	Rows       []RetroOutcomeWire `json:"rows"`
	NextCursor int64              `json:"nextCursor,omitempty"`
	Statement  string             `json:"statement"`
}

type RetroShowResult struct {
	Row       RetroOutcomeWire  `json:"row"`
	Agents    []RetroAgentWire  `json:"agents"`
	Learning  *LearningLinkWire `json:"learning,omitempty"`
	Statement string            `json:"statement"`
}

// RetroReconcileWire is the reconcile door's last answer, in the words
// the daemon remembers from the last boot pass (in-memory: reconcile
// runs at boot, and a daemon that never ran one says neverRun — that
// truth is the state, not a zero pretending to be a run).
type RetroReconcileWire struct {
	NeverRun    bool   `json:"neverRun"`
	LastRunAt   string `json:"lastRunAt,omitempty"`
	LastWritten int    `json:"lastWritten"`
}

// RetroModelRetroWire is the optional model retrospective's setting
// state (RT15). Until the retrospective blueprint and its declared
// setting exist, enabled:false with the named blockedBy is the honest
// answer — absent is not a state a status door may pretend at.
type RetroModelRetroWire struct {
	Enabled   bool   `json:"enabled"`
	BlockedBy string `json:"blockedBy,omitempty"`
}

type RetroStatusResult struct {
	RecordedSince    string              `json:"recordedSince"`
	LoopsRecorded    int                 `json:"loopsRecorded"`
	LastWriteAt      string              `json:"lastWriteAt"`
	Dropped          uint64              `json:"dropped"`
	Outcomes         int                 `json:"outcomes"`
	LearningsWritten int                 `json:"learningsWritten"`
	Reconcile        RetroReconcileWire  `json:"reconcile"`
	ModelRetro       RetroModelRetroWire `json:"modelRetrospective"`
	Statement        string              `json:"statement"`
}

// RetroBackfillWire mirrors daemon.RetroBackfillReport: the counts a
// dry run PREVIEWED, or the same counts plus Written>0 after the real
// pass. Skips are counted by WHY, so a person sees what the pass
// declined to touch.
type RetroBackfillWire struct {
	Scanned         int            `json:"scanned"`
	DryRun          bool           `json:"dryRun"`
	ByKind          map[string]int `json:"byKind"`
	SkippedRecorded int            `json:"skippedRecorded"`
	SkippedRule     int            `json:"skippedRule"`
	SkippedOpen     int            `json:"skippedOpen"`
	SkippedNoScope  int            `json:"skippedNoScope"`
	Written         int            `json:"written"`
	Statement       string         `json:"statement"`
}

// --- the seam -----------------------------------------------------------------

// RetroService is the daemon's read side plus the one human door.
// Backfill takes the actor the CONNECTION resolved, never a params
// field (P4); the reads need no actor — facts are facts.
type RetroService interface {
	List(ctx context.Context, p RetroListParams) (RetroListResult, error)
	Show(ctx context.Context, p RetroShowParams) (RetroShowResult, error)
	Status(ctx context.Context) (RetroStatusResult, error)
	Backfill(ctx context.Context, actor Actor, p RetroBackfillParams) (RetroBackfillWire, error)
}

// --- registration ---------------------------------------------------------------

// RegisterRetro registers the four doors over a non-nil seam (V13: nil
// seam ⇒ all four ABSENT).
func (t *Table) RegisterRetro() bool {
	if t.Retro == nil {
		return false
	}
	for _, m := range []*Method{
		{Name: MethodRetroList, RequiresInitialize: true, Handle: t.handleRetroList},
		{Name: MethodRetroShow, RequiresInitialize: true, Handle: t.handleRetroShow},
		{Name: MethodRetroStatus, RequiresInitialize: true, Handle: t.handleRetroStatus},
		{Name: MethodRetroBackfill, RequiresInitialize: true, Handle: t.handleRetroBackfill},
	} {
		t.register(m)
	}
	return true
}

// --- the handlers -------------------------------------------------------------

var retroIdentityKeys = []string{"actor", "actorKind", "identity", "peer", "uid", "source"}

func rejectRetroIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams will say it better
	}
	for _, k := range retroIdentityKeys {
		if _, ok := bag[k]; ok {
			return ErrBadParams{Err: fmt.Errorf(
				"%s takes no %q — fact rows carry their source, and a backfill write carries the connection's person, never a typed actor", method, k)}
		}
	}
	return nil
}

func (t *Table) retroSeam() error {
	if t.Retro == nil {
		return ErrRetroUnavailable.WithWhat("no store is wired — no outcome facts exist to read, and none can be written")
	}
	return nil
}

func (t *Table) handleRetroList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectRetroIdentityKeys(string(MethodRetroList), raw); err != nil {
		return nil, err
	}
	if err := t.retroSeam(); err != nil {
		return nil, err
	}
	var p RetroListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return t.Retro.List(ctx, p)
}

func (t *Table) handleRetroShow(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectRetroIdentityKeys(string(MethodRetroShow), raw); err != nil {
		return nil, err
	}
	if err := t.retroSeam(); err != nil {
		return nil, err
	}
	var p RetroShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.OutcomeID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("retro.show needs an outcomeId — retro.list hands out the ids")}
	}
	return t.Retro.Show(ctx, p)
}

func (t *Table) handleRetroStatus(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectRetroIdentityKeys(string(MethodRetroStatus), raw); err != nil {
		return nil, err
	}
	if err := t.retroSeam(); err != nil {
		return nil, err
	}
	return t.Retro.Status(ctx)
}

func (t *Table) handleRetroBackfill(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectRetroIdentityKeys(string(MethodRetroBackfill), raw); err != nil {
		return nil, err
	}
	if err := t.retroSeam(); err != nil {
		return nil, err
	}
	// The person check FIRST (P4): a backfill write is a person's door,
	// and the actor is the connection's — uid 0 and a blank id refuse
	// before any parameter is even read (the merge-queue actor shape).
	if t.ActorFor == nil {
		return nil, ErrScorecardRefused.WithWhat("retro.backfill is a person's door and no identity is available on this connection — the daemon stamped none")
	}
	a := t.ActorFor(ctx, sess)
	if id := strings.TrimSpace(a.ID); id == "" || id == "0" {
		return nil, ErrScorecardRefused.WithWhat("retro.backfill is a person's door and the peer is unresolvable (uid %q) — the actor is the connection's person, never a guess", a.ID)
	}
	var p RetroBackfillParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	dryRun := true
	if p.DryRun != nil {
		dryRun = *p.DryRun
	}
	if !dryRun && !p.Yes {
		return nil, ErrBadParams{Err: fmt.Errorf("retro.backfill would write rows: resend with \"yes\": true to confirm — the preview (dryRun default) shows the counts by outcome first")}
	}
	return t.Retro.Backfill(ctx, Actor{Kind: "user", ID: strings.TrimSpace(a.ID)}, p)
}

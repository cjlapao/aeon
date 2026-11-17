package protocol

// S27-continuous T7: the scheduler's socket surface.
//
// The scheduler is an authority record (CN2): what it may start is written
// by a person on this surface and nowhere else, and every control here
// carries the peer who gave the order (the actor is the connection's, never
// a param — AL11's lock, re-walked for these rows). The four reads answer
// "what will run, what ran, why not, who is needed"; the eight controls are
// the human door: the master, the pause, the per-repository template, the
// one-time tree expansion, and stopAll — the off switch that is not an
// uninstall (CN18).
//
// The seam rule is V13, the shape every human door in this package shares:
// registration is conditional on Table.Continuous, so a daemon without the
// service leaves all twelve methods ABSENT — `scheduler.enable` is
// ErrUnknownMethod, not a registered door that refuses. In particular
// `scheduler.stopAll` is never registered without S55: the service exists
// exactly over a store, and a storeless daemon has no S55 control ledger to
// stop a loop through (T6's schedulerStopAllWired is the same fact at the
// daemon's end).

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// the seam — what the daemon must answer for the twelve doors to open
// ---------------------------------------------------------------------------

// ContinuousActor is the write's audit identity, mapped from the
// connection's peer (never from params).
type ContinuousActor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// ContinuousRefusal is the seam's "the door declined": Sentence is the
// validator's or the store's own words (rendered verbatim into the typed
// error's what — CN20's no-paraphrase rule), and NeedsMaster marks the one
// refusal family that has its own voice (ErrSchedulerDisabled): the control
// asked for repository authority under a master that is off, and the door
// refuses the composition rather than writing half of it.
type ContinuousRefusal struct {
	Sentence    string
	NeedsMaster bool
}

// ContinuousSetting is one effective setting with its source word — the
// value the tick will actually spend, and where it came from.
type ContinuousSetting struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

// ContinuousWindow is a repository's active window as stored; nil means the
// repository is always-on.
type ContinuousWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Days  string `json:"days,omitempty"`
	Zone  string `json:"zone,omitempty"`
}

// ContinuousTemplate is the CN5 work template exactly as the row holds it.
// With is the fixed with-pairs JSON, mirrored raw — this plane renders the
// row, it does not reinterpret it.
type ContinuousTemplate struct {
	Blueprint string          `json:"blueprint"`
	Tech      string          `json:"tech,omitempty"`
	With      json.RawMessage `json:"with,omitempty"`
}

// ContinuousRepoState is the repository's live state in the two words the
// panel reads: Kind is the derived word (the repository's honest condition
// right now — disabled, paused, out-of-window, occupied, ready, idle,
// template-invalid, budget-exhausted, grant-absent, started), Reason is the
// closed token behind it when there is one, Detail the sentence the daemon
// would print (budget hits ride verbatim).
type ContinuousRepoState struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// ContinuousRepoView is one scheduler_root row with its live reading.
type ContinuousRepoView struct {
	ComponentRoot string              `json:"componentRoot"`
	RootID        string              `json:"rootId,omitempty"`
	Enabled       bool                `json:"enabled"`
	Template      ContinuousTemplate  `json:"template"`
	Window        *ContinuousWindow   `json:"window,omitempty"`
	MinPriority   string              `json:"minPriority"`
	LastStartAt   string              `json:"lastStartAt,omitempty"`
	State         ContinuousRepoState `json:"state"`
}

// ContinuousDescriptor is where forms get their vocabulary (rule 4): the
// closed sets the door accepts, in the daemon's own words, so no client
// types a state list.
type ContinuousDescriptor struct {
	PriorityStates []string `json:"priorityStates"`
	Reasons        []string `json:"reasons"`
	WindowGrammar  string   `json:"windowGrammar"`
	BlueprintsFrom string   `json:"blueprintsFrom"`
}

// ContinuousStatusView is scheduler.status's whole answer.
type ContinuousStatusView struct {
	Enabled      bool                   `json:"enabled"`
	Paused       bool                   `json:"paused"`
	PausedReason string                 `json:"pausedReason,omitempty"`
	Settings     []ContinuousSetting    `json:"settings"`
	Slots        ContinuousSlots        `json:"slots"`
	NextTickAt   string                 `json:"nextTickAt"`
	Repos        []ContinuousRepoView   `json:"repos"`
	Attention    ContinuousAttentionSum `json:"attention"`
	Statement    string                 `json:"statement"`
	Descriptor   ContinuousDescriptor   `json:"descriptor"`
	S20Paused    bool                   `json:"s20Paused"`
}

// ContinuousSlots is CN9's capacity answer: what the tick would spend
// against what it may spend.
type ContinuousSlots struct {
	Used int `json:"used"`
	Cap  int `json:"cap"`
}

// ContinuousAttentionSum is the count half of the attention plane the
// status view carries (the items themselves ride scheduler.attention).
type ContinuousAttentionSum struct {
	Open int `json:"open"`
}

// ContinuousPreviewLine is one line the tick would decide — the CN15
// preview's row shape, the same words Plan used. SubjectID is the core's
// string (a queue id, a roadmap id, a root path — the ledger's int64
// subject is a different, narrower thing).
type ContinuousPreviewLine struct {
	ComponentRoot string `json:"componentRoot"`
	SubjectKind   string `json:"subjectKind"`
	SubjectID     string `json:"subjectId"`
	Verdict       string `json:"verdict"`
	Reason        string `json:"reason"`
	Detail        string `json:"detail,omitempty"`
}

// ContinuousPreview is scheduler.preview: the tick, run, not described.
type ContinuousPreview struct {
	Now        string                  `json:"now"`
	Decisions  []ContinuousPreviewLine `json:"decisions"`
	WouldStart []ContinuousPreviewLine `json:"wouldStart"`
}

// ContinuousDecisionRow mirrors the audit row verbatim — the ledger speaks
// for itself; this plane adds no interpretation.
type ContinuousDecisionRow struct {
	ID            int64  `json:"id"`
	At            string `json:"at"`
	ComponentRoot string `json:"componentRoot"`
	SubjectKind   string `json:"subjectKind"`
	SubjectID     int64  `json:"subjectId"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	Summary       string `json:"summary"`
	GrantRevision string `json:"grantRevision,omitempty"`
	ActorKind     string `json:"actorKind"`
	ActorID       string `json:"actorId"`
	LoopSlug      string `json:"loopSlug,omitempty"`
}

// ContinuousDecisionQuery is the decisions read after the door has parsed
// the wire subject ("queue/7") into its two parts.
type ContinuousDecisionQuery struct {
	ComponentRoot string
	SubjectKind   string
	SubjectID     int64
	HasSubject    bool
	Decision      string
	Reason        string
	Since         string
	Limit         int
}

// ContinuousAttentionItem is one open needs-you: a kind shown verbatim
// (CN21's no-interpretation rule) with the daemon's own note when the kind
// has a remedy that is a different act (cap-hit's raise).
type ContinuousAttentionItem struct {
	DecisionID    int64  `json:"decisionId"`
	LoopSlug      string `json:"loopSlug,omitempty"`
	ComponentRoot string `json:"componentRoot"`
	SubjectKind   string `json:"subjectKind"`
	SubjectID     int64  `json:"subjectId"`
	Kind          string `json:"kind"`
	Note          string `json:"note,omitempty"`
	Since         string `json:"since"`
}

// ContinuousAttention is scheduler.attention's answer.
type ContinuousAttention struct {
	Items []ContinuousAttentionItem `json:"items"`
	Open  int                       `json:"open"`
}

// ContinuousRootInput is the CN5 template as authored.
type ContinuousRootInput struct {
	ComponentRoot string
	RootID        string
	Enabled       bool
	Blueprint     string
	Tech          string
	WithJSON      string
	Window        *ContinuousWindow
	MinPriority   string
}

// ContinuousStopAllResult is CN18's off-switch ledger: the pause that
// happened FIRST, every loop stopped, every failure listed (the pause
// stands even on partial failure — a half-off switch is still off).
type ContinuousStopAllResult struct {
	Paused   bool     `json:"paused"`
	Stopped  []string `json:"stopped"`
	Failures []string `json:"failures"`
}

// ContinuousAPI is the scheduler service the twelve doors speak through.
// Every control takes the actor the CONNECTION resolved (a params actor
// would be a forged audit identity on the authority record itself). The
// controls answer in two voices: a REFUSAL is the door declining (typed,
// sentence verbatim), an ERROR is the store failing — the second is never
// dressed as the first.
type ContinuousAPI interface {
	Status(ctx context.Context) (ContinuousStatusView, error)
	Preview(ctx context.Context, componentRoot string) (ContinuousPreview, error)
	Decisions(ctx context.Context, q ContinuousDecisionQuery) ([]ContinuousDecisionRow, error)
	Attention(ctx context.Context, componentRoot string) (ContinuousAttention, error)
	SetMaster(ctx context.Context, actor ContinuousActor, enabled bool) (*ContinuousRefusal, error)
	SetPaused(ctx context.Context, actor ContinuousActor, paused bool, reason string) (*ContinuousRefusal, error)
	SetRoot(ctx context.Context, actor ContinuousActor, in ContinuousRootInput) (ContinuousRepoView, *ContinuousRefusal, error)
	DisableRoot(ctx context.Context, actor ContinuousActor, componentRoot string) (*ContinuousRefusal, error)
	EnableTree(ctx context.Context, actor ContinuousActor, rootID string, in ContinuousRootInput) ([]string, *ContinuousRefusal, error)
	StopAll(ctx context.Context, actor ContinuousActor, reason string) (ContinuousStopAllResult, *ContinuousRefusal, error)
}

// ---------------------------------------------------------------------------
// the identity door
// ---------------------------------------------------------------------------

// continuousIdentityKeys are the keys NO scheduler method accepts in
// params: the actor, the state words the authority rows carry, and the
// audit stamps — all of them the KERNEL's answers, never a caller's
// claims. An unknown field is tolerated (a field from the future); an
// identity CLAIM is refused by name.
var continuousIdentityKeys = []string{
	"actor", "actorKind", "actorId", "by", "createdBy", "updatedBy", "status", "source",
}

// checkContinuousParams reads raw params as a key bag and refuses any
// identity key by name (the autonomy/backup precedent).
func checkContinuousParams(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return ErrBadParams{Err: err}
	}
	for _, k := range continuousIdentityKeys {
		if _, ok := bag[k]; ok {
			return ErrBadParams{Err: fmt.Errorf(
				"%q is not a parameter — the scheduler's actor comes from the connection's peer credential, never from params (CN2)", k)}
		}
	}
	return nil
}

// schedulerActor resolves the control's audit identity from the
// CONNECTION. A connection with no actor seam, a blank id, or a kind that
// is not `user` is refused with a typed sentence — the authority record
// only hears people (CN2).
func (t *Table) schedulerActor(ctx context.Context, sess *Session) (ContinuousActor, error) {
	if t.ActorFor == nil {
		return ContinuousActor{}, ErrSchedulerRefused.WithWhat("%s",
			"refused: a scheduler control needs a person on the connection, and this one carries no identity")
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return ContinuousActor{}, ErrSchedulerRefused.WithWhat("%s",
			"refused: a scheduler control needs a named person — an anonymous author cannot be audited (CN2)")
	}
	if a.Kind != "user" {
		return ContinuousActor{}, ErrSchedulerRefused.WithWhat("%s",
			fmt.Sprintf("refused: only a user may move the scheduler — caller kind %q is not a person (CN2)", a.Kind))
	}
	return ContinuousActor{Kind: a.Kind, ID: a.ID}, nil
}

// schedulerRefused renders the seam's refusal typed: the validator's or the
// store's sentence verbatim in `what`, under ErrSchedulerDisabled when the
// refusal is the master's and ErrSchedulerRefused otherwise.
func schedulerRefused(ref *ContinuousRefusal) error {
	if ref.NeedsMaster {
		return ErrSchedulerDisabled.WithWhat("%s", ref.Sentence)
	}
	return ErrSchedulerRefused.WithWhat("%s", ref.Sentence)
}

// continuousTripwire is the unreachable guard: registration is conditional
// on the same field, so a handler seeing nil means registration and the
// seam disagreed — spoken, not silent.
func (t *Table) continuousTripwire() error {
	return ErrStoreUnavailable.WithWhat(
		"scheduler.* needs the continuous scheduler service, and this build did not link it — the scheduler surface is absent for the same reason (V13)")
}

// ---------------------------------------------------------------------------
// registration (called from the daemon's wiring, V13-gated)
// ---------------------------------------------------------------------------

// RegisterContinuous registers the twelve scheduler doors — four reads and
// the eight human-door controls — iff the seam is wired (V13: a nil seam
// leaves them ALL absent, `scheduler.stopAll` included, so the method never
// stands without S55 behind it). All twelve are RequiresInitialize and none
// is Debug. It reports whether it registered.
func (t *Table) RegisterContinuous() bool {
	if t.Continuous == nil {
		return false
	}
	t.register(&Method{Name: MethodSchedulerStatus, RequiresInitialize: true, Handle: t.handleSchedulerStatus})
	t.register(&Method{Name: MethodSchedulerPreview, RequiresInitialize: true, Handle: t.handleSchedulerPreview})
	t.register(&Method{Name: MethodSchedulerDecisions, RequiresInitialize: true, Handle: t.handleSchedulerDecisions})
	t.register(&Method{Name: MethodSchedulerAttention, RequiresInitialize: true, Handle: t.handleSchedulerAttention})
	t.register(&Method{Name: MethodSchedulerEnable, RequiresInitialize: true, Handle: t.handleSchedulerEnable})
	t.register(&Method{Name: MethodSchedulerDisable, RequiresInitialize: true, Handle: t.handleSchedulerDisable})
	t.register(&Method{Name: MethodSchedulerPause, RequiresInitialize: true, Handle: t.handleSchedulerPause})
	t.register(&Method{Name: MethodSchedulerResume, RequiresInitialize: true, Handle: t.handleSchedulerResume})
	t.register(&Method{Name: MethodSchedulerRootSet, RequiresInitialize: true, Handle: t.handleSchedulerRootSet})
	t.register(&Method{Name: MethodSchedulerRootDisable, RequiresInitialize: true, Handle: t.handleSchedulerRootDisable})
	t.register(&Method{Name: MethodSchedulerEnableTree, RequiresInitialize: true, Handle: t.handleSchedulerEnableTree})
	t.register(&Method{Name: MethodSchedulerStopAll, RequiresInitialize: true, Handle: t.handleSchedulerStopAll})
	return true
}

// ---------------------------------------------------------------------------
// the reads
// ---------------------------------------------------------------------------

func (t *Table) handleSchedulerStatus(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.Continuous == nil {
		return nil, t.continuousTripwire()
	}
	if err := checkContinuousParams(raw); err != nil {
		return nil, err
	}
	return t.Continuous.Status(ctx)
}

// continuousRepoFilterParams is the {componentRoot?, rootId?} pair the
// preview and attention reads share: at most ONE of the two (they name the
// same repository from different ends; naming both is a request the door
// cannot honestly answer).
type continuousRepoFilterParams struct {
	ComponentRoot *string `json:"componentRoot"`
	RootID        *string `json:"rootId"`
}

func (p continuousRepoFilterParams) one() (string, error) {
	a, b := p.ComponentRoot != nil, p.RootID != nil
	if a && b {
		return "", ErrBadParams{Err: fmt.Errorf("name one of componentRoot or rootId, not both — they are the same repository from two ends")}
	}
	if a {
		return *p.ComponentRoot, nil
	}
	if b {
		return "root:" + *p.RootID, nil
	}
	return "", nil
}

func (t *Table) handleSchedulerPreview(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.Continuous == nil {
		return nil, t.continuousTripwire()
	}
	if err := checkContinuousParams(raw); err != nil {
		return nil, err
	}
	var p continuousRepoFilterParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	root, err := p.one()
	if err != nil {
		return nil, err
	}
	return t.Continuous.Preview(ctx, root)
}

func (t *Table) handleSchedulerAttention(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.Continuous == nil {
		return nil, t.continuousTripwire()
	}
	if err := checkContinuousParams(raw); err != nil {
		return nil, err
	}
	var p continuousRepoFilterParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	root, err := p.one()
	if err != nil {
		return nil, err
	}
	return t.Continuous.Attention(ctx, root)
}

// schedulerDecisionsParams is the audit read's filter set. limit is bounded
// here: an unbounded audit read is a page that grows with the ledger, and
// the newest-first read with a ceiling is the honest one.
type schedulerDecisionsParams struct {
	ComponentRoot string `json:"componentRoot"`
	Subject       string `json:"subject"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	Since         string `json:"since"`
	Limit         *int   `json:"limit"`
}

// continuousSubjectKinds is the closed subject vocabulary of the decision
// ledger (T2's CHECK).
var continuousSubjectKinds = map[string]bool{"queue": true, "roadmap": true, "root": true, "daemon": true}

// parseSchedulerSubject reads "kind/id" — the wire spelling of the ledger's
// two subject columns.
func parseSchedulerSubject(s string) (string, int64, error) {
	kind, idText, ok := strings.Cut(s, "/")
	if !ok || !continuousSubjectKinds[kind] {
		return "", 0, ErrBadParams{Err: fmt.Errorf(
			"subject %q is not \"kind/id\" with kind one of queue, roadmap, root, daemon", s)}
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		return "", 0, ErrBadParams{Err: fmt.Errorf("subject id %q is not a number: %w", idText, err)}
	}
	return kind, id, nil
}

func (t *Table) handleSchedulerDecisions(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.Continuous == nil {
		return nil, t.continuousTripwire()
	}
	if err := checkContinuousParams(raw); err != nil {
		return nil, err
	}
	var p schedulerDecisionsParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	q, err := ParseContinuousDecisionFilters(ContinuousDecisionFilters{
		Subject: p.Subject, Decision: p.Decision, Reason: p.Reason,
		Since: p.Since, Limit: p.Limit,
	})
	if err != nil {
		return nil, err
	}
	q.ComponentRoot = p.ComponentRoot
	return t.Continuous.Decisions(ctx, q)
}

// ContinuousDecisionFilters is the ledger read's filter set BEFORE the
// repository is named — the shared grammar S27 T8 extracted so the
// person's door and the agent's scheduler_decisions tool parse one way
// (rule 4: two doors, one implementation, no drift).
type ContinuousDecisionFilters struct {
	Subject  string
	Decision string
	Reason   string
	Since    string
	Limit    *int
}

// ParseContinuousDecisionFilters validates the ledger's filter words:
// subject "kind/id" from the closed vocabulary, since strictly RFC3339,
// limit inside 1..500. It is the ONLY validation these filters get
// anywhere — the socket handler and the records arm both come through
// here, and the ComponentRoot is stamped by the CALLER (the person's
// params, or the step's identity) after it returns.
func ParseContinuousDecisionFilters(f ContinuousDecisionFilters) (ContinuousDecisionQuery, error) {
	q := ContinuousDecisionQuery{Decision: f.Decision, Reason: f.Reason}
	if f.Subject != "" {
		kind, id, err := parseSchedulerSubject(f.Subject)
		if err != nil {
			return q, err
		}
		q.SubjectKind, q.SubjectID, q.HasSubject = kind, id, true
	}
	if f.Since != "" {
		if _, err := time.Parse(time.RFC3339, f.Since); err != nil {
			return q, ErrBadParams{Err: fmt.Errorf("since %q is not RFC3339: %w", f.Since, err)}
		}
		q.Since = f.Since
	}
	if f.Limit != nil {
		if *f.Limit < 1 || *f.Limit > 500 {
			return q, ErrBadParams{Err: fmt.Errorf("limit %d is outside 1..500 — the decisions read is bounded, newest first", *f.Limit)}
		}
		q.Limit = *f.Limit
	}
	return q, nil
}

// ---------------------------------------------------------------------------
// the master and the pause
// ---------------------------------------------------------------------------

func (t *Table) handleSchedulerEnable(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.Continuous == nil {
		return nil, t.continuousTripwire()
	}
	if err := checkContinuousParams(raw); err != nil {
		return nil, err
	}
	actor, err := t.schedulerActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	ref, err := t.Continuous.SetMaster(ctx, actor, true)
	if err != nil {
		return nil, err
	}
	if ref != nil {
		return nil, schedulerRefused(ref)
	}
	return map[string]any{"enabled": true}, nil
}

func (t *Table) handleSchedulerDisable(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.Continuous == nil {
		return nil, t.continuousTripwire()
	}
	if err := checkContinuousParams(raw); err != nil {
		return nil, err
	}
	actor, err := t.schedulerActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	ref, err := t.Continuous.SetMaster(ctx, actor, false)
	if err != nil {
		return nil, err
	}
	if ref != nil {
		return nil, schedulerRefused(ref)
	}
	return map[string]any{"enabled": false}, nil
}

// schedulerReasonParams is the pause/stopAll shape: the reason is REQUIRED
// (CN18 — the operational off carries its word into the audit; a blank
// reason is refused at the door, not stored).
type schedulerReasonParams struct {
	Reason string `json:"reason"`
}

func requireSchedulerReason(p schedulerReasonParams, method string) (string, error) {
	if strings.TrimSpace(p.Reason) == "" {
		return "", ErrBadParams{Err: fmt.Errorf("%s requires a reason — the audit asks who moved the switch and why (CN18)", method)}
	}
	return p.Reason, nil
}

func (t *Table) handleSchedulerPause(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.Continuous == nil {
		return nil, t.continuousTripwire()
	}
	if err := checkContinuousParams(raw); err != nil {
		return nil, err
	}
	var p schedulerReasonParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	reason, err := requireSchedulerReason(p, "scheduler.pause")
	if err != nil {
		return nil, err
	}
	actor, err := t.schedulerActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	ref, err := t.Continuous.SetPaused(ctx, actor, true, reason)
	if err != nil {
		return nil, err
	}
	if ref != nil {
		return nil, schedulerRefused(ref)
	}
	// CN18's shape, said here so no client infers it: pause STOPS PICKING.
	// Running loops keep running — they are the loops a start decision
	// already justified.
	return map[string]any{"paused": true, "runningLoops": "unchanged — pause stops picking, not running work"}, nil
}

func (t *Table) handleSchedulerResume(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.Continuous == nil {
		return nil, t.continuousTripwire()
	}
	if err := checkContinuousParams(raw); err != nil {
		return nil, err
	}
	actor, err := t.schedulerActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	ref, err := t.Continuous.SetPaused(ctx, actor, false, "")
	if err != nil {
		return nil, err
	}
	if ref != nil {
		return nil, schedulerRefused(ref)
	}
	return map[string]any{"paused": false}, nil
}

// ---------------------------------------------------------------------------
// the repository door
// ---------------------------------------------------------------------------

// schedulerRootSetParams is the CN5 template as authored. `enabled` is
// REQUIRED (pointer, not zero-value): a root.set that omitted it would
// silently enable a repository nobody typed yes to.
type schedulerRootSetParams struct {
	ComponentRoot string            `json:"componentRoot"`
	Enabled       *bool             `json:"enabled"`
	Blueprint     string            `json:"blueprint"`
	Tech          string            `json:"tech"`
	With          json.RawMessage   `json:"with"`
	Window        *ContinuousWindow `json:"window"`
	MinPriority   string            `json:"minPriority"`
}

func (p schedulerRootSetParams) toInput(componentRoot string) (ContinuousRootInput, error) {
	if strings.TrimSpace(componentRoot) == "" {
		return ContinuousRootInput{}, ErrBadParams{Err: fmt.Errorf("componentRoot is required — scheduler authority is per repository (CN2)")}
	}
	in := ContinuousRootInput{
		ComponentRoot: componentRoot,
		Blueprint:     p.Blueprint,
		Tech:          p.Tech,
		MinPriority:   p.MinPriority,
	}
	if p.Enabled == nil {
		return ContinuousRootInput{}, ErrBadParams{Err: fmt.Errorf("enabled is required — an omitted field must not silently enable a repository (CN2)")}
	}
	in.Enabled = *p.Enabled
	if len(p.With) > 0 && string(p.With) != "null" {
		// The with pairs are authored as an object; the row stores them as
		// JSON text. A non-object here would become a row the tick cannot
		// read, so the door checks the SHAPE only — the seam validates the
		// CONTENT with the template validator's own sentences.
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(p.With, &probe); err != nil {
			return ContinuousRootInput{}, ErrBadParams{Err: fmt.Errorf("with must be an object of fixed pairs: %w", err)}
		}
		in.WithJSON = string(p.With)
	}
	if p.Window != nil {
		w := *p.Window
		if strings.TrimSpace(w.Start) == "" || strings.TrimSpace(w.End) == "" {
			return ContinuousRootInput{}, ErrBadParams{Err: fmt.Errorf("a window needs its start AND end (CN5's pair rule) — an open-ended window is not a window")}
		}
		in.Window = &w
	}
	return in, nil
}

func (t *Table) handleSchedulerRootSet(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.Continuous == nil {
		return nil, t.continuousTripwire()
	}
	if err := checkContinuousParams(raw); err != nil {
		return nil, err
	}
	var p schedulerRootSetParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	in, err := p.toInput(p.ComponentRoot)
	if err != nil {
		return nil, err
	}
	actor, err := t.schedulerActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	view, ref, rerr := t.Continuous.SetRoot(ctx, actor, in)
	if rerr != nil {
		return nil, rerr
	}
	if ref != nil {
		return nil, schedulerRefused(ref)
	}
	return view, nil
}

func (t *Table) handleSchedulerRootDisable(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.Continuous == nil {
		return nil, t.continuousTripwire()
	}
	if err := checkContinuousParams(raw); err != nil {
		return nil, err
	}
	var p struct {
		ComponentRoot string `json:"componentRoot"`
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentRoot) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("componentRoot is required")}
	}
	actor, err := t.schedulerActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	ref, err := t.Continuous.DisableRoot(ctx, actor, p.ComponentRoot)
	if err != nil {
		return nil, err
	}
	if ref != nil {
		return nil, schedulerRefused(ref)
	}
	return map[string]any{"componentRoot": p.ComponentRoot, "enabled": false}, nil
}

// schedulerEnableTreeParams is CN2's bulk expansion: the template is
// authored ONCE and lands as one row per repository in the tree AS IT
// STANDS — a repository attached later is not enabled by a call it never
// saw, which is the failure mode CN6 names.
type schedulerEnableTreeParams struct {
	RootID      string            `json:"rootId"`
	Blueprint   string            `json:"blueprint"`
	Tech        string            `json:"tech"`
	With        json.RawMessage   `json:"with"`
	Window      *ContinuousWindow `json:"window"`
	MinPriority string            `json:"minPriority"`
}

func (t *Table) handleSchedulerEnableTree(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.Continuous == nil {
		return nil, t.continuousTripwire()
	}
	if err := checkContinuousParams(raw); err != nil {
		return nil, err
	}
	var p schedulerEnableTreeParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.RootID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("rootId is required — the tree expansion starts at one tree")}
	}
	if strings.TrimSpace(p.Blueprint) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("blueprint is required — CN5: no template, no enable")}
	}
	// The one-time expansion enables (there is no other reading of
	// enableTree); the row-by-row authority lands under the peer.
	set := schedulerRootSetParams{
		Blueprint: p.Blueprint, Tech: p.Tech, With: p.With,
		Window: p.Window, MinPriority: p.MinPriority,
	}
	yes := true
	set.Enabled = &yes
	in, err := set.toInput("tree:" + p.RootID)
	if err != nil {
		return nil, err
	}
	actor, err := t.schedulerActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	roots, ref, rerr := t.Continuous.EnableTree(ctx, actor, p.RootID, in)
	if rerr != nil {
		return nil, rerr
	}
	if ref != nil {
		return nil, schedulerRefused(ref)
	}
	return map[string]any{"roots": roots, "count": len(roots),
		"note": "enabled for the repositories that exist NOW — a repository attached later keeps its own authority (CN6)"}, nil
}

// ---------------------------------------------------------------------------
// the off switch
// ---------------------------------------------------------------------------

func (t *Table) handleSchedulerStopAll(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.Continuous == nil {
		return nil, t.continuousTripwire()
	}
	if err := checkContinuousParams(raw); err != nil {
		return nil, err
	}
	var p schedulerReasonParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	reason, err := requireSchedulerReason(p, "scheduler.stopAll")
	if err != nil {
		return nil, err
	}
	actor, err := t.schedulerActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, ref, rerr := t.Continuous.StopAll(ctx, actor, reason)
	if rerr != nil {
		return nil, rerr
	}
	if ref != nil {
		return nil, schedulerRefused(ref)
	}
	return res, nil
}

// intake.go — S61 T7 (GI8..GI13, P4): the intake doors, the wizard's side
// of the socket.
//
// Six doors over ONE seam, registered together or absent (V13). They
// answer what the intake engine already computes; this plane runs none of
// it (the engine is the one implementation, P34's other side).
//
// The shape the wire states:
//
//   - Two writes only — `intake.start` (a row + a scratch dir, the loop
//     belongs to T9) and `intake.commit` (the commit is exactly what
//     `internal/intake`'s Engine.Commit does — same function the CLI and
//     the tests drive, P34's one implementation). Everything else reads.
//   - The commit's actor is the connection's person (P4) and its shape is
//     `intake.commit`'s own (GI9): a stale digest, a refused target, a
//     publish with a missing key, a non-person writer and a resume whose
//     evidence no longer holds all answer `ErrIntakeRefused` (-31965) with
//     the ENGINE's own sentence in `what` — the five refusal families the
//   - `intake.plan` is READ-ONLY by contract (GI10): it opens nothing,
//     files nothing, and the digest it hands out is the commit's only
//     key. Conformance asserts the census is untouched.
//   - `intake.vocabulary` publishes the closed sets from the packages
//     that enforce them (publish modes, roles, the plan caps, the
//     template slugs, the workspace root as configured) — the wizard
//     never re-spells a bound it can read.
//
// No actor params, anywhere (P4): the identity keys are refused by name
// before decode, on reads and writes alike.

package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// --- the method names -------------------------------------------------------

const (
	// MethodIntakeStart files an intake row and its scratch dir — a
	// person's door; the interview loop that fills the scratch starts in
	// T9, and start says so in its answer.
	MethodIntakeStart MethodName = "intake.start"
	// MethodIntakeList lists intakes with state (idea → committed) read
	// from the proposal row and the ledger, never a column lie.
	MethodIntakeList MethodName = "intake.list"
	// MethodIntakeStatus answers one intake's ledger: steps, outcomes,
	// where a crash stopped the run.
	MethodIntakeStatus MethodName = "intake.status"
	// MethodIntakePlan previews the commit — scaffold preview plus the
	// rows-would-file census — and hands out the digest commit needs
	// (GI10: read-only).
	MethodIntakePlan MethodName = "intake.plan"
	// MethodIntakeCommit commits the plan: scaffold, rows, one commit —
	// the wizard's shape (GI9), resumable (GI11), the actor the
	// connection's person (P4).
	MethodIntakeCommit MethodName = "intake.commit"
	// MethodIntakeVocabulary publishes the closed sets from the packages
	// that enforce them.
	MethodIntakeVocabulary MethodName = "intake.vocabulary"
	// MethodIntakeAbandon closes an interview that will not commit: the
	// loop stops, the intake's OPEN plans are rejected through the desk's
	// own reject path, and the derived state says abandoned (GI13). The
	// scratch workspace stays on disk — a person's door may close a
	// session, not erase its notes (T9, 2026-11-05).
	MethodIntakeAbandon MethodName = "intake.abandon"
	// MethodIntakePropose is the AGENT's ask (S61 T8, GI17): from inside an
	// intake session it files an inert intake_plan proposal; outside one it
	// refuses. The start and commit doors stay the person's (GI9);
	// conditional on the records seam (charter_propose.go).
	MethodIntakePropose MethodName = "intake.propose"
)

// --- the wire shapes ---------------------------------------------------------

// IntakeView is one intake row.
type IntakeView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ScratchPath string `json:"scratchPath"`
	LoopSlug    string `json:"loopSlug,omitempty"`
	RootID      string `json:"rootId,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

// IntakeStepView is one ledger row of a commit run.
type IntakeStepView struct {
	Step    string `json:"step"`
	Outcome string `json:"outcome"` // done | failed | skipped
	Detail  string `json:"detail,omitempty"`
	At      string `json:"at"`
}

// IntakeStatusView is `intake.status`' answer. State is the honest read:
// open | running (a loop attached) | committed | failed-at-<step> —
// derived from the proposal row and the ledger, exactly what Engine.Status
// computes.
type IntakeStatusView struct {
	IntakeID       string           `json:"intakeId"`
	State          string           `json:"state"`
	ProposalID     string           `json:"proposalId,omitempty"`
	ProposalStatus string           `json:"proposalStatus,omitempty"`
	RootID         string           `json:"rootId,omitempty"`
	TargetPath     string           `json:"targetPath,omitempty"`
	FailedStep     string           `json:"failedStep,omitempty"`
	Warning        string           `json:"warning,omitempty"`
	Steps          []IntakeStepView `json:"steps"`
}

// IntakePreviewComponent is one component of the preview: the plan's slug
// with the scaffold's answer for it.
type IntakePreviewComponent struct {
	Slug          string   `json:"slug"`
	Role          string   `json:"role"`
	Name          string   `json:"name"`
	TargetPath    string   `json:"targetPath"`
	Blueprint     string   `json:"blueprint"`
	Files         []string `json:"files"`
	Actions       []string `json:"actions"`
	RemoteCreated bool     `json:"remoteCreated"`
	// RepoUrl and CommitSha arrive only on a commit's answer — the
	// evidence the run produced (preview leaves them empty: nothing has
	// been created or committed at preview time).
	RepoUrl   string `json:"repoUrl,omitempty"`
	CommitSha string `json:"commitSha,omitempty"`
}

// IntakePreview is `intake.plan`' answer: the scaffold preview, the rows
// the commit would file, and the digest that keys the commit. Reading it
// files nothing (GI10).
type IntakePreview struct {
	IntakeID   string                   `json:"intakeId"`
	ProposalID string                   `json:"proposalId"`
	Digest     string                   `json:"digest"`
	TargetPath string                   `json:"targetPath"`
	Publish    string                   `json:"publish"`
	Components []IntakePreviewComponent `json:"components"`
	Goals      int                      `json:"goals"`
	Epics      int                      `json:"epics"`
	Items      int                      `json:"items"`
	Records    []string                 `json:"records"` // "goals: 3" style census lines
}

// IntakeCommitResult is `intake.commit`' answer — the same result the
// engine returns the CLI (one implementation, two doors).
type IntakeCommitResult struct {
	IntakeID   string                   `json:"intakeId"`
	ProposalID string                   `json:"proposalId"`
	RootID     string                   `json:"rootId"`
	TargetPath string                   `json:"targetPath"`
	Resumed    bool                     `json:"resumed"`
	Warning    string                   `json:"warning,omitempty"`
	Components []IntakePreviewComponent `json:"components"`
	Goals      int                      `json:"goals"`
	Epics      int                      `json:"epics"`
	Items      int                      `json:"items"`
}

// IntakeStartParams — `{name, scratchPath?}`. A scratch path is the
// person's own choice of interview notes location; absent lays the
// daemon default under the workspace root's intake dir.
type IntakeStartParams struct {
	Name        string `json:"name"`
	ScratchPath string `json:"scratchPath,omitempty"`
}

// IntakeAbandonParams — `{intakeId, reason}`. The reason is REQUIRED: the
// rejects it drives are decisions, and a decision without its sentence is
// the audit gap S35 PM7.5 closed everywhere else.
type IntakeAbandonParams struct {
	IntakeID string `json:"intakeId"`
	Reason   string `json:"reason"`
}

// IntakeAbandonResult is intake.abandon's answer: what closed, what was
// rejected (proposal ids as strings, the protocol's voice for them), and
// the row after the close. No scratch path is deleted, so none is promised.
type IntakeAbandonResult struct {
	IntakeID          string     `json:"intakeId"`
	LoopClosed        bool       `json:"loopClosed"`
	RejectedProposals []string   `json:"rejectedProposals,omitempty"`
	Intake            IntakeView `json:"intake"`
}

// IntakeListResult is intake.list's whole answer.
type IntakeListResult struct {
	Intakes []IntakeStatusRow `json:"intakes"`
}

// IntakeStatusRow is one list row — the row shape plus the state words,
// a details list (no bare ids).
type IntakeStatusRow struct {
	IntakeView
	State      string `json:"state"`
	ProposalID string `json:"proposalId,omitempty"`
}

// IntakeStatusParams — `{intakeId}`.
type IntakeStatusParams struct {
	IntakeID string `json:"intakeId"`
}

// IntakePlanParams — `{intakeId, proposalId, targetPath?, publish?, owner?,
// confirmPublic?}`. `targetPath` may be absent when the plan names its own
// target (the wizard reads the default from the vocabulary's
// workspaceRoot); the publish trio is exactly the commit's choice — the
// preview commits to nothing but the digest covers it (GI12's pin).
type IntakePlanParams struct {
	IntakeID      string `json:"intakeId"`
	ProposalID    string `json:"proposalId"`
	TargetPath    string `json:"targetPath,omitempty"`
	Publish       string `json:"publish,omitempty"`
	Owner         string `json:"owner,omitempty"`
	ConfirmPublic bool   `json:"confirmPublic,omitempty"`
}

// IntakeCommitParams — the wizard's commit shape (GI9): the plan choice
// as one typed object, the actor NOT among them.
type IntakeCommitParams struct {
	IntakeID      string `json:"intakeId"`
	ProposalID    string `json:"proposalId"`
	Digest        string `json:"digest"`
	TargetPath    string `json:"targetPath,omitempty"`
	Publish       string `json:"publish,omitempty"`
	Owner         string `json:"owner,omitempty"`
	ConfirmPublic bool   `json:"confirmPublic,omitempty"`
}

// IntakeVocabularyResult publishes the closed sets — T11's finalised
// descriptor. Caps are the plan validator's OWN numbers; publish is the
// closed trio as CHOICES, each carrying its i18n label KEY (the app owns
// the words it prints) and, where the act reaches outward, the daemon's
// OWN confirmation statement (P7: the client never writes an authority
// sentence, it renders this one); the templates are the palette as saved;
// remoteCreateEnabled is the S44 gate AS READ, and when it is false the
// REASON is the sentence the action itself would speak — the descriptor
// cannot claim a door the action would refuse; routeAvailable is the fact
// intake.start's gate enforces, so the wizard's front door reads the same
// truth the back door keeps; workspaceRoot is as configured, empty when
// unset — and the state says which.
type IntakeVocabularyResult struct {
	Publish             []IntakePublishChoice `json:"publish"`
	Roles               []string              `json:"roles"`
	Caps                IntakePlanCapsWire    `json:"caps"`
	Templates           []TemplateSummary     `json:"templates"`
	RemoteCreateEnabled bool                  `json:"remoteCreateEnabled"`
	RemoteCreateReason  string                `json:"remoteCreateReason,omitempty"`
	RouteAvailable      bool                  `json:"routeAvailable"`
	WorkspaceRoot       string                `json:"workspaceRoot"`
	WorkspaceRootState  string                `json:"workspaceRootState"`
}

// IntakePublishChoice is one publish mode whole: the value the wire
// carries, the i18n key for the label the app prints, and the daemon's
// confirmation statement when the mode touches anything outward.
type IntakePublishChoice struct {
	Value     string `json:"value"`
	Label     string `json:"label"`
	Statement string `json:"statement,omitempty"`
}

// IntakePlanCapsWire mirrors the desk validator's caps, one field per
// bound — the numbers ENFORCED, read through the daemon seam.
type IntakePlanCapsWire struct {
	PayloadBytes    int `json:"payloadBytes"`
	Components      int `json:"components"`
	Epics           int `json:"epics"`
	Goals           int `json:"goals"`
	Items           int `json:"items"`
	CriteriaPerGoal int `json:"criteriaPerGoal"`
}

// --- the seam -----------------------------------------------------------------

// IntakeService is the daemon's intake side. The writes take the actor
// the CONNECTION resolved, never a params field (P4). Refusals arrive
// voiced as ErrIntakeRefused-shaped errors carrying the engine's or the
// door's own sentence.
type IntakeService interface {
	Start(ctx context.Context, actor Actor, p IntakeStartParams) (IntakeView, error)
	List(ctx context.Context) (IntakeListResult, error)
	Status(ctx context.Context, p IntakeStatusParams) (IntakeStatusView, error)
	Plan(ctx context.Context, p IntakePlanParams) (IntakePreview, error)
	Commit(ctx context.Context, actor Actor, p IntakeCommitParams) (IntakeCommitResult, error)
	Abandon(ctx context.Context, actor Actor, p IntakeAbandonParams) (IntakeAbandonResult, error)
	Vocabulary(ctx context.Context) (IntakeVocabularyResult, error)
}

// --- registration ---------------------------------------------------------------

// RegisterIntake registers the seven intake doors over a non-nil seam
// (V13: nil seam ⇒ all seven ABSENT — the CLI direct doors answer without a
// daemon, and nothing about the socket pretends otherwise).
func (t *Table) RegisterIntake() bool {
	if t.Intake == nil {
		return false
	}
	for _, m := range []*Method{
		{Name: MethodIntakeStart, RequiresInitialize: true, Handle: t.handleIntakeStart},
		{Name: MethodIntakeList, RequiresInitialize: true, Handle: t.handleIntakeList},
		{Name: MethodIntakeStatus, RequiresInitialize: true, Handle: t.handleIntakeStatus},
		{Name: MethodIntakePlan, RequiresInitialize: true, Handle: t.handleIntakePlan},
		{Name: MethodIntakeCommit, RequiresInitialize: true, Handle: t.handleIntakeCommit},
		{Name: MethodIntakeAbandon, RequiresInitialize: true, Handle: t.handleIntakeAbandon},
		{Name: MethodIntakeVocabulary, RequiresInitialize: true, Handle: t.handleIntakeVocabulary},
	} {
		t.register(m)
	}
	return true
}

// --- the handlers -------------------------------------------------------------

func rejectIntakeIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams will say it better
	}
	for _, k := range charterIdentityKeys {
		if _, ok := bag[k]; ok {
			return ErrBadParams{Err: fmt.Errorf(
				"%s takes no %q — an intake row and every ledger row carry the connection's person, never a typed actor", method, k)}
		}
	}
	return nil
}

// intakePerson resolves the connection's person or answers the refusal
// voice with the sentence the P4 guards check for by name.
func (t *Table) intakePerson(method string, ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrIntakeRefused.WithWhat("%s is a person's door and no identity is available on this connection — the daemon stamped none", method)
	}
	a := t.ActorFor(ctx, sess)
	if id := strings.TrimSpace(a.ID); id == "" || id == "0" {
		return Actor{}, ErrIntakeRefused.WithWhat("%s is a person's door and the peer is unresolvable (uid %q) — the actor is the connection's person, never a guess", method, a.ID)
	}
	return Actor{Kind: "user", ID: strings.TrimSpace(a.ID)}, nil
}

func (t *Table) handleIntakeStart(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectIntakeIdentityKeys(string(MethodIntakeStart), raw); err != nil {
		return nil, err
	}
	actor, err := t.intakePerson(string(MethodIntakeStart), ctx, sess)
	if err != nil {
		return nil, err
	}
	var p IntakeStartParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("intake.start needs a name — it names the row and the scratch dir")}
	}
	return t.Intake.Start(ctx, actor, p)
}

func (t *Table) handleIntakeList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectIntakeIdentityKeys(string(MethodIntakeList), raw); err != nil {
		return nil, err
	}
	return t.Intake.List(ctx)
}

func (t *Table) handleIntakeStatus(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectIntakeIdentityKeys(string(MethodIntakeStatus), raw); err != nil {
		return nil, err
	}
	var p IntakeStatusParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.IntakeID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("intake.status needs an intakeId — intake.list hands them out")}
	}
	return t.Intake.Status(ctx, p)
}

func (t *Table) handleIntakePlan(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectIntakeIdentityKeys(string(MethodIntakePlan), raw); err != nil {
		return nil, err
	}
	var p IntakePlanParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.IntakeID) == "" || strings.TrimSpace(p.ProposalID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("intake.plan needs an intakeId and a proposalId — the proposal carries the plan (desk.propose kind intake_plan)")}
	}
	return t.Intake.Plan(ctx, p)
}

func (t *Table) handleIntakeCommit(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectIntakeIdentityKeys(string(MethodIntakeCommit), raw); err != nil {
		return nil, err
	}
	actor, err := t.intakePerson(string(MethodIntakeCommit), ctx, sess)
	if err != nil {
		return nil, err
	}
	var p IntakeCommitParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.IntakeID) == "" || strings.TrimSpace(p.ProposalID) == "" || strings.TrimSpace(p.Digest) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("intake.commit needs intakeId, proposalId and digest — intake.plan hands out all three, and the digest is what proves the preview is the thing being committed")}
	}
	return t.Intake.Commit(ctx, actor, p)
}

func (t *Table) handleIntakeAbandon(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectIntakeIdentityKeys(string(MethodIntakeAbandon), raw); err != nil {
		return nil, err
	}
	actor, err := t.intakePerson(string(MethodIntakeAbandon), ctx, sess)
	if err != nil {
		return nil, err
	}
	var p IntakeAbandonParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.IntakeID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("intake.abandon needs an intakeId — intake.list hands them out")}
	}
	if strings.TrimSpace(p.Reason) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("intake.abandon needs a reason — the rejects it drives are decisions, and a decision without its sentence is an audit gap")}
	}
	return t.Intake.Abandon(ctx, actor, p)
}

func (t *Table) handleIntakeVocabulary(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectIntakeIdentityKeys(string(MethodIntakeVocabulary), raw); err != nil {
		return nil, err
	}
	return t.Intake.Vocabulary(ctx)
}

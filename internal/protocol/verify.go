// verify.go — S57 T8 (VG18): the twelve verify methods — the daemon's
// surface for the verification gates: the six reads (list, show, runs,
// runShow, vocabulary, templates) and the six human-door writes
// (add, update, archive, restore, applyTemplate, try).
//
// The seam is the daemon's VerifyService (internal/daemon/verify_service.go),
// which implements VerifyStore. The wire types below are THIS package's;
// the store's closed vocabularies travel as plain lists, so no client
// types a set twice (the vocabulary method is the single source). A
// *VerifyRefusal is the engine's refusal rendered as data (DB19): the
// closed-set kind in Data.Kind, the person's sentence in Data.What, one
// code for the family (ErrVerifyRefused, T0's -31870).
//
// Registered CONDITIONALLY by RegisterVerify (V13: a nil seam leaves all
// twelve ABSENT, not registered-and-refusing — the backup shape). All
// twelve are RequiresInitialize and none is Debug. The six writes stamp
// the actor from the CONNECTION (Table.ActorFor, the kernel peer
// credential), never from params — rejectGateIdentityKeys refuses an
// identity key in the wire, and TestNoMethodAcceptsAnActorParam pins the
// family. verify.try is the one write that SPAWNS: it runs one gate
// through verify.Runner in the component's primary root, under the policy
// gate and the scrubbed env, and the response names the path it ran in
// and the loops still active there (OD8) — a person sees the race they
// just joined.
//
// The method name constants live HERE, beside their family, and not in
// method.go: that file is already past the SIZE1 line, and a change that
// lands in a standing violation splits it rather than grows it (the T8
// Notes record the ruling). Same for the two families' own files.

package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The twelve `verify.*` method names live in method.go's S57 block — protogen
// enumerates that file alone, and the app's contract is what T11's panel
// reads. The rest of this file owns the plane's types, closed sets and seams.

// --- the seam ----------------------------------------------------------------

// VerifyStore is the daemon's one verify seam: twelve doors, one service.
// The wire views are this package's types; a *VerifyRefusal is the engine's
// refusal rendered as data (DB19), never an error; a non-nil error is an
// infrastructure failure the door cannot name any better than the store's
// own sentence.
type VerifyStore interface {
	// List answers verify.list: the component's own gates, or the
	// EFFECTIVE set (own plus the root's, each marked with its source
	// and the supplying component) when f.Effective. includeArchived
	// widens the set, requirement narrows it. Every gate carries its
	// last finished run (state + times) and the policy preview —
	// "would it run as the verifier now" — which is a warning, never
	// a refusal (the run-time gate is authoritative).
	List(ctx context.Context, f VerifyListQuery) (VerifyListResult, error)
	// Show answers verify.show: the gate with its full detail plus its
	// event trail (the audit the person may read the changes through).
	Show(ctx context.Context, id string) (VerifyShowResult, error)
	// Runs answers verify.runs: the finished runs, newest first, scoped
	// by component (its primary root), loop and state, capped.
	Runs(ctx context.Context, f VerifyRunsQuery) (VerifyRunsResult, error)
	// RunShow answers verify.runShow: one run with its per-gate results
	// — the ONLY door that crosses the wire with tails (a human reads
	// them; the list never does, and the daemon's event discipline
	// keeps them out of events, bugs and audit).
	RunShow(ctx context.Context, id string) (VerifyRunShowResult, error)
	// Vocabulary answers verify.vocabulary: the closed sets (requirement,
	// run states, run reasons), the gate env allowlist NAMES (the client
	// renders the set, it never types it), and the tryStatement — the
	// daemon's own sentence of what verify.try will do.
	Vocabulary(ctx context.Context) (VerifyVocabularyResult, error)
	// Templates answers verify.templates: the inert template picker.
	Templates(ctx context.Context) (VerifyTemplatesResult, error)
	// Add answers verify.add: a new gate on the component, the actor
	// stamped from the connection.
	Add(ctx context.Context, actor Actor, in VerifyAddInput) (VerifyWriteResult, *VerifyRefusal, error)
	// Update answers verify.update: the named fields on one gate
	// (absent fields keep their value — the store's pointer shape).
	Update(ctx context.Context, actor Actor, in VerifyUpdateInput) (VerifyWriteResult, *VerifyRefusal, error)
	// Archive answers verify.archive: the gate leaves the live set (the
	// run evidence is untouched — a gate's history outlives its archive).
	Archive(ctx context.Context, actor Actor, id string) (VerifyWriteResult, *VerifyRefusal, error)
	// Restore answers verify.restore: the archived gate re-enters the
	// live set, position and all (its position was never reissued).
	Restore(ctx context.Context, actor Actor, id string) (VerifyWriteResult, *VerifyRefusal, error)
	// ApplyTemplate answers verify.applyTemplate: the named templates
	// copied onto the component as origin='template' gates, all or
	// nothing (the store checks every name collision before any write).
	ApplyTemplate(ctx context.Context, actor Actor, componentID string, templateIDs []string) (VerifyApplyResult, *VerifyRefusal, error)
	// Try answers verify.try: ONE gate, run now, in the component's
	// primary root, under the policy gate and the scrubbed env,
	// recorded as a trigger='manual' run with the peer actor. The
	// result says ranIn (the path) and activeLoops (the slugs still
	// running there) — OD8: the answer names the race.
	Try(ctx context.Context, actor Actor, componentID, gateID string) (VerifyTryResult, *VerifyRefusal, error)
}

// VerifyRefusal is a refused verify act as data (DB19): Kind is the
// store's closed refusal vocabulary (the machine can branch on it),
// Detail the person's sentence, printed verbatim.
type VerifyRefusal struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// --- the seam's queries and inputs -------------------------------------------

type VerifyListQuery struct {
	ComponentID     string
	Effective       bool
	IncludeArchived bool
	Requirement     string
}

type VerifyRunsQuery struct {
	ComponentID string
	LoopSlug    string
	State       string
	Limit       int
}

type VerifyAddInput struct {
	ComponentID string
	Name        string
	Command     string
	Requirement string
	TimeoutS    int
	MemoryMB    *int
	Position    *int
	Enabled     *bool
}

type VerifyUpdateInput struct {
	ID          string
	Command     *string
	Requirement *string
	TimeoutS    *int
	MemoryMB    *int
	Position    *int
	Enabled     *bool
}

// --- the wire views ------------------------------------------------------------

// VerifyGateView is one gate as a list-with-details carries it: the
// row's own fields (memory "declared, not enforced" — the view says the
// word in its JSON name, memoryMb, and the daemon's vocabulary carries
// the sentence), plus the four the list-with-details owes (source and
// the supplying component in the effective set, the last finished run,
// the policy preview). The empty-omitted fields are the conditional
// ones: a row read off-component has no source, a never-run gate has no
// last run, and the preview is absent only where the daemon could not
// ask the policy (that refusal is an infrastructure error, not a view).
type VerifyGateView struct {
	ID          string `json:"id"`
	ComponentID string `json:"componentId"`
	Name        string `json:"name"`
	Command     string `json:"command"`
	Requirement string `json:"requirement"`
	TimeoutS    int    `json:"timeoutS"`
	MemoryMB    *int   `json:"memoryMb,omitempty"`
	Position    int    `json:"position"`
	Enabled     bool   `json:"enabled"`
	ArchivedAt  string `json:"archivedAt,omitempty"`
	Origin      string `json:"origin"`
	TemplateID  string `json:"templateId,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`

	Source          string         `json:"source,omitempty"` // "own" | "root", the effective set
	SourceComponent string         `json:"sourceComponent,omitempty"`
	LastRun         *VerifyRunLite `json:"lastRun,omitempty"`
	PolicyPreview   string         `json:"policyPreview,omitempty"`
}

// VerifyRunLite is the list's last-run line: the state and the times,
// nothing the run's own door answers better.
type VerifyRunLite struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	Trigger   string `json:"trigger"`
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt,omitempty"`
}

type VerifyRunView struct {
	ID            string `json:"id"`
	LoopSlug      string `json:"loopSlug"`
	ComponentRoot string `json:"componentRoot"`
	ComponentID   string `json:"componentId"`
	Task          string `json:"task"`
	Position      int    `json:"position"`
	Attempt       int    `json:"attempt"`
	HeadSHA       string `json:"headSha"`
	Trigger       string `json:"trigger"` // "loop" | "manual"
	ActorKind     string `json:"actorKind"`
	ActorID       string `json:"actorId"`
	StartedAt     string `json:"startedAt"`
	EndedAt       string `json:"endedAt,omitempty"`
	State         string `json:"state"`
	Reason        string `json:"reason,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Digest        string `json:"digest,omitempty"`
}

// VerifyRunResultView is one gate of a run. The TAILS (stdoutTail /
// stderrTail) are the human door's: they cross the wire only through
// verify.runShow, never through a list, and never into events, bugs or
// the audit (VG23's output discipline — the daemon keeps the tails out
// of those records; this view is runShow's answer only).
type VerifyRunResultView struct {
	Seq          int    `json:"seq"`
	Name         string `json:"name"`
	Requirement  string `json:"requirement"`
	Command      string `json:"command"`
	ExitCode     *int   `json:"exitCode"`
	Signal       string `json:"signal,omitempty"`
	TimedOut     bool   `json:"timedOut"`
	WallMS       int    `json:"wallMs"`
	StdoutBytes  int    `json:"stdoutBytes"`
	StderrBytes  int    `json:"stderrBytes"`
	Truncated    bool   `json:"truncated"`
	StdoutSHA256 string `json:"stdoutSha256"`
	StderrSHA256 string `json:"stderrSha256"`
	StdoutTail   string `json:"stdoutTail,omitempty"`
	StderrTail   string `json:"stderrTail,omitempty"`
	State        string `json:"state"`
	Reason       string `json:"reason,omitempty"`
}

type VerifyListResult struct {
	ComponentID string           `json:"componentId"`
	Effective   bool             `json:"effective"`
	Gates       []VerifyGateView `json:"gates"`
}

// VerifyEventView is one row of the gate's trail (the audit, the same
// shape the merge rule's changes carry — id, op, who, when, summary;
// the before/after JSON is deliberately NOT crossed: a list door, and
// the gate's full after-image is its own row, read with show).
type VerifyEventView struct {
	Op        string `json:"op"`
	Revision  int    `json:"revision"`
	ActorKind string `json:"actorKind"`
	ActorID   string `json:"actorId"`
	At        string `json:"at"`
	Summary   string `json:"summary"`
}

type VerifyShowResult struct {
	Gate   VerifyGateView    `json:"gate"`
	Events []VerifyEventView `json:"events"`
}

type VerifyRunsResult struct {
	Runs []VerifyRunView `json:"runs"`
}

type VerifyRunShowResult struct {
	Run     VerifyRunView         `json:"run"`
	Results []VerifyRunResultView `json:"results"`
}

// The verify plane's closed sets, defined ONCE here (S57 T10, VG20): the
// daemon's verify.vocabulary answer is built from them, and the CLI's
// offline usage — the same words with no daemon to ask — reads the very
// same Go constants, so no set is typed twice. The DoD states join them
// in dod.go.
var (
	VerifyRequirementValues = []string{"required", "advisory"}
	VerifyStateValues       = []string{"pass", "fail", "unverified"}
	// VerifyReasonValues is the runner's closed REASON WORDS as the wire
	// names them: the run's reason is a pass (empty), a fail (the fixed
	// template, which IS the counts), or "unverified: <word>" where the
	// word is one of these (the policy marker the runner maps to
	// "policy"). The store keeps the reason as free text on purpose —
	// this list is what the runner OWES, not a CHECK.
	VerifyReasonValues = []string{"cancelled", "policy", "timeout"}
)

// VerifyVocabularyResult is the set the client renders from — the closed
// requirement/run-state/run-reason vocabularies, the gate env allowlist
// NAMES (the daemon reads tools.GateEnvNames; no client types the list),
// and the tryStatement: the daemon's own sentence of what verify.try
// will do, rendered with the daemon's own names (the verifier's agent
// name and the policy's standing), so the confirmation the UI shows is
// the daemon's words, not a translation.
type VerifyVocabularyResult struct {
	Requirements []string `json:"requirements"`
	States       []string `json:"states"`
	Reasons      []string `json:"reasons"`
	EnvAllowlist []string `json:"envAllowlist"`
	TryStatement string   `json:"tryStatement"`
}

type VerifyTemplateView struct {
	ID          string `json:"id"`
	Stack       string `json:"stack"`
	Name        string `json:"name"`
	Command     string `json:"command"`
	Requirement string `json:"requirement"`
	TimeoutS    int    `json:"timeoutS"`
	Origin      string `json:"origin"`
	Version     string `json:"version"`
}

type VerifyTemplatesResult struct {
	Templates []VerifyTemplateView `json:"templates"`
}

// VerifyEnvNoteSentence is the daemon's scrubbed-environment note, owned
// here as the one sentence (S57 T10, VG20): a gate command is command
// text, never a credential holder, and when a write's command carries a
// token shape the daemon says this once, verbatim, on the write result —
// the CLI prints it and never re-decides it.
const VerifyEnvNoteSentence = "gate commands run with a scrubbed environment; do not embed a credential"

type VerifyWriteResult struct {
	Gate VerifyGateView `json:"gate"`
	// Note is the scrubbed-environment note (VerifyEnvNoteSentence) when
	// the written command carries a token shape, and empty otherwise —
	// present only when the daemon chose to say it (S57 T10).
	Note string `json:"note,omitempty"`
}

type VerifyApplyResult struct {
	Gates []VerifyGateView `json:"gates"`
}

// VerifyTryResult is the answer to a try: the run with its per-gate
// result, plus the two facts OD8 owes the person — RanIn, the path the
// command ran in (the component's primary root, which the response says
// out loud), and ActiveLoops, the slugs still running in that root when
// the try started (an empty list says the root was quiet; a non-empty
// one says the race is on, and names it).
type VerifyTryResult struct {
	Run         VerifyRunView         `json:"run"`
	Results     []VerifyRunResultView `json:"results"`
	RanIn       string                `json:"ranIn"`
	ActiveLoops []string              `json:"activeLoops"`
}

// --- the params ----------------------------------------------------------------

type VerifyListParams struct {
	ComponentID     string `json:"componentId"`
	Effective       *bool  `json:"effective"`
	IncludeArchived bool   `json:"includeArchived"`
	Requirement     string `json:"requirement"`
}

type VerifyShowParams struct {
	ID string `json:"id"`
}

type VerifyRunsParams struct {
	ComponentID string `json:"componentId"`
	Loop        string `json:"loop"`
	State       string `json:"state"`
	Limit       int    `json:"limit"`
}

type VerifyRunShowParams struct {
	ID string `json:"id"`
}

type VerifyAddParams struct {
	ComponentID string `json:"componentId"`
	Name        string `json:"name"`
	Command     string `json:"command"`
	Requirement string `json:"requirement"`
	TimeoutS    int    `json:"timeoutS"`
	MemoryMB    *int   `json:"memoryMb"`
	Position    *int   `json:"position"`
	Enabled     *bool  `json:"enabled"`
}

type VerifyUpdateParams struct {
	ID          string  `json:"id"`
	Command     *string `json:"command"`
	Requirement *string `json:"requirement"`
	TimeoutS    *int    `json:"timeoutS"`
	MemoryMB    *int    `json:"memoryMb"`
	Position    *int    `json:"position"`
	Enabled     *bool   `json:"enabled"`
}

type VerifyArchiveParams struct {
	ID string `json:"id"`
}

type VerifyRestoreParams struct {
	ID string `json:"id"`
}

// VerifyApplyTemplateParams: the templates to copy, by id. The spec's
// one-line shape ({componentId, templateId, names?}) is realised as the
// store's own (componentID, templateIDs) — T3's apply is a list over
// template ids, one row per template, all or nothing in ONE store call;
// a names-based subset would be a second resolver the store does not
// have (the T8 Notes record the ruling).
type VerifyApplyTemplateParams struct {
	ComponentID string   `json:"componentId"`
	TemplateIDs []string `json:"templateIds"`
}

type VerifyTryParams struct {
	ComponentID string `json:"componentId"`
	ID          string `json:"id"`
}

// --- the shared actor and refusal shape (the nine writes, three files) --------

// gateActor stamps who-wrote from the CONNECTION (the daemon's ActorFor
// seam, the kernel's peer credential), never from params — the shared
// half of the nine write doors (the verify six, the merge rule's two,
// the DoD's one). The honest absence refuses the write, and an empty id
// is refused as well (the seedActor shape).
func (t *Table) gateActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("the gate write is a person's door and no identity is available on this connection — the daemon stamped none")}
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("the gate write is a person's door and no identity is available on this connection — the daemon stamped none")}
	}
	return a, nil
}

// gateIdentityKeys names the identity keys a gate call may never carry:
// the actor is stamped by the door that holds the connection, and a
// param that names it would let a caller forge the audit identity of a
// write (the backup's BK13, carried to the gate plane).
var gateIdentityKeys = []string{"actor", "actorKind", "who", "identity", "origin"}

func rejectGateIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range gateIdentityKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — the actor is stamped by the daemon from the connection, never named by the caller",
				method, key)}
		}
	}
	return nil
}

// verifyRefusedError renders the engine's refusal as the socket's
// (DB19, the backupRefusedError shape): one code (ErrVerifyRefused),
// the closed-set kind in Data.Kind, the person's sentence in What.
func verifyRefusedError(verb string, r *VerifyRefusal) *Error {
	e := ErrVerifyRefused.WithWhat("verify %s refused: %s", verb, r.Detail)
	e.Data.Kind = r.Kind
	return e
}

// verifySeam is the one nil-guard shape the twelve handlers share:
// registration is conditional on the seam, so a nil here is a
// registration/seam disagreement the guard tests catch — spoken, not
// silent (the backup shape).
func (t *Table) verifySeam() error {
	if t.Verify == nil {
		return ErrStoreUnavailable.WithWhat("verify.* needs the verify seam, and this build did not link it — the verify surface is absent for the same reason (V13)")
	}
	return nil
}

// --- the handlers --------------------------------------------------------------

func (t *Table) handleVerifyList(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.verifySeam(); err != nil {
		return nil, err
	}
	var p VerifyListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.list needs the component's id")}
	}
	if p.Requirement != "" && p.Requirement != "required" && p.Requirement != "advisory" {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.list's requirement is \"required\" or \"advisory\", not %q", p.Requirement)}
	}
	effective := false
	if p.Effective != nil {
		effective = *p.Effective
	}
	res, err := t.Verify.List(ctx, VerifyListQuery{
		ComponentID:     p.ComponentID,
		Effective:       effective,
		IncludeArchived: p.IncludeArchived,
		Requirement:     p.Requirement,
	})
	if err != nil {
		return nil, err
	}
	if res.Gates == nil {
		res.Gates = []VerifyGateView{}
	}
	return res, nil
}

func (t *Table) handleVerifyShow(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.verifySeam(); err != nil {
		return nil, err
	}
	var p VerifyShowParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.show needs the gate's id")}
	}
	res, err := t.Verify.Show(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if res.Events == nil {
		res.Events = []VerifyEventView{}
	}
	return res, nil
}

func (t *Table) handleVerifyRuns(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.verifySeam(); err != nil {
		return nil, err
	}
	var p VerifyRunsParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Limit < 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.runs's limit is a count, not %d", p.Limit)}
	}
	res, err := t.Verify.Runs(ctx, VerifyRunsQuery{
		ComponentID: p.ComponentID,
		LoopSlug:    p.Loop,
		State:       p.State,
		Limit:       p.Limit,
	})
	if err != nil {
		return nil, err
	}
	if res.Runs == nil {
		res.Runs = []VerifyRunView{}
	}
	return res, nil
}

func (t *Table) handleVerifyRunShow(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.verifySeam(); err != nil {
		return nil, err
	}
	var p VerifyRunShowParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.runShow needs the run's id")}
	}
	res, err := t.Verify.RunShow(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if res.Results == nil {
		res.Results = []VerifyRunResultView{}
	}
	return res, nil
}

func (t *Table) handleVerifyVocabulary(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.verifySeam(); err != nil {
		return nil, err
	}
	_ = params // the set has no params: the vocabulary is the daemon's own
	res, err := t.Verify.Vocabulary(ctx)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (t *Table) handleVerifyTemplates(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.verifySeam(); err != nil {
		return nil, err
	}
	_ = params
	res, err := t.Verify.Templates(ctx)
	if err != nil {
		return nil, err
	}
	if res.Templates == nil {
		res.Templates = []VerifyTemplateView{}
	}
	return res, nil
}

func (t *Table) handleVerifyAdd(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.verifySeam(); err != nil {
		return nil, err
	}
	if err := rejectGateIdentityKeys(string(MethodVerifyAdd), params); err != nil {
		return nil, err
	}
	var p VerifyAddParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	actor, err := t.gateActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, refusal, err := t.Verify.Add(ctx, actor, VerifyAddInput{
		ComponentID: p.ComponentID, Name: p.Name, Command: p.Command,
		Requirement: p.Requirement, TimeoutS: p.TimeoutS,
		MemoryMB: p.MemoryMB, Position: p.Position, Enabled: p.Enabled,
	})
	if refusal != nil {
		return nil, verifyRefusedError("add", refusal)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (t *Table) handleVerifyUpdate(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.verifySeam(); err != nil {
		return nil, err
	}
	if err := rejectGateIdentityKeys(string(MethodVerifyUpdate), params); err != nil {
		return nil, err
	}
	var p VerifyUpdateParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.update needs the gate's id")}
	}
	if p.Command == nil && p.Requirement == nil && p.TimeoutS == nil && p.MemoryMB == nil && p.Position == nil && p.Enabled == nil {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.update names no field — nothing to update")}
	}
	actor, err := t.gateActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, refusal, err := t.Verify.Update(ctx, actor, VerifyUpdateInput{
		ID: p.ID, Command: p.Command, Requirement: p.Requirement,
		TimeoutS: p.TimeoutS, MemoryMB: p.MemoryMB, Position: p.Position, Enabled: p.Enabled,
	})
	if refusal != nil {
		return nil, verifyRefusedError("update", refusal)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (t *Table) handleVerifyArchive(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.verifySeam(); err != nil {
		return nil, err
	}
	if err := rejectGateIdentityKeys(string(MethodVerifyArchive), params); err != nil {
		return nil, err
	}
	var p VerifyArchiveParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.archive needs the gate's id")}
	}
	actor, err := t.gateActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, refusal, err := t.Verify.Archive(ctx, actor, p.ID)
	if refusal != nil {
		return nil, verifyRefusedError("archive", refusal)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (t *Table) handleVerifyRestore(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.verifySeam(); err != nil {
		return nil, err
	}
	if err := rejectGateIdentityKeys(string(MethodVerifyRestore), params); err != nil {
		return nil, err
	}
	var p VerifyRestoreParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.restore needs the gate's id")}
	}
	actor, err := t.gateActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, refusal, err := t.Verify.Restore(ctx, actor, p.ID)
	if refusal != nil {
		return nil, verifyRefusedError("restore", refusal)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (t *Table) handleVerifyApplyTemplate(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.verifySeam(); err != nil {
		return nil, err
	}
	if err := rejectGateIdentityKeys(string(MethodVerifyApplyTemplate), params); err != nil {
		return nil, err
	}
	var p VerifyApplyTemplateParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.applyTemplate needs the component's id")}
	}
	if len(p.TemplateIDs) == 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.applyTemplate names no template")}
	}
	actor, err := t.gateActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, refusal, err := t.Verify.ApplyTemplate(ctx, actor, p.ComponentID, p.TemplateIDs)
	if refusal != nil {
		return nil, verifyRefusedError("applyTemplate", refusal)
	}
	if err != nil {
		return nil, err
	}
	if res.Gates == nil {
		res.Gates = []VerifyGateView{}
	}
	return res, nil
}

func (t *Table) handleVerifyTry(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.verifySeam(); err != nil {
		return nil, err
	}
	if err := rejectGateIdentityKeys(string(MethodVerifyTry), params); err != nil {
		return nil, err
	}
	var p VerifyTryParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.try needs the component's id")}
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("verify.try needs the gate's id")}
	}
	actor, err := t.gateActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, refusal, err := t.Verify.Try(ctx, actor, p.ComponentID, p.ID)
	if refusal != nil {
		return nil, verifyRefusedError("try", refusal)
	}
	if err != nil {
		return nil, err
	}
	if res.ActiveLoops == nil {
		res.ActiveLoops = []string{}
	}
	return res, nil
}

// RegisterVerify conditionally registers the twelve verify methods (V13:
// a nil seam leaves them ABSENT — the daemon reports the family as
// unavailable, not as a set of registered doors that all refuse).
func (t *Table) RegisterVerify() bool {
	if t.Verify == nil {
		return false
	}
	t.register(&Method{Name: MethodVerifyList, RequiresInitialize: true, Handle: t.handleVerifyList})
	t.register(&Method{Name: MethodVerifyShow, RequiresInitialize: true, Handle: t.handleVerifyShow})
	t.register(&Method{Name: MethodVerifyRuns, RequiresInitialize: true, Handle: t.handleVerifyRuns})
	t.register(&Method{Name: MethodVerifyRunShow, RequiresInitialize: true, Handle: t.handleVerifyRunShow})
	t.register(&Method{Name: MethodVerifyVocabulary, RequiresInitialize: true, Handle: t.handleVerifyVocabulary})
	t.register(&Method{Name: MethodVerifyTemplates, RequiresInitialize: true, Handle: t.handleVerifyTemplates})
	t.register(&Method{Name: MethodVerifyAdd, RequiresInitialize: true, Handle: t.handleVerifyAdd})
	t.register(&Method{Name: MethodVerifyUpdate, RequiresInitialize: true, Handle: t.handleVerifyUpdate})
	t.register(&Method{Name: MethodVerifyArchive, RequiresInitialize: true, Handle: t.handleVerifyArchive})
	t.register(&Method{Name: MethodVerifyRestore, RequiresInitialize: true, Handle: t.handleVerifyRestore})
	t.register(&Method{Name: MethodVerifyApplyTemplate, RequiresInitialize: true, Handle: t.handleVerifyApplyTemplate})
	t.register(&Method{Name: MethodVerifyTry, RequiresInitialize: true, Handle: t.handleVerifyTry})
	return true
}

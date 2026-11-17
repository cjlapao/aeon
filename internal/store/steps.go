package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/model"
)

// The step row, its cost, its frozen context, and its transcript.
//
// # This is where cost honesty either survives or quietly dies
//
// `cost_state` is a STORED column, never derived (DB11). The tempting shape is
// one nullable `cost_usd` with the state inferred from nullity — and it
// COLLAPSES `partial` INTO `known`, because a partial cost has a number; it is
// just a floor rather than a total. The lower-bound semantics S3's whole cost
// design preserved would be gone at that point, and no later slice could
// recover them: cost rollups and the app's "at least $X" both read this column.
//
// So cost is written through ONE function and read through ONE function, and a
// test greps for any other statement touching the three columns. A caller that
// writes `cost_state` directly will eventually write one inconsistent with
// `cost_usd`, and nothing would notice.
//
// # `status` distinguishes suspended from orphaned, and that matters
//
// A suspended step has NO PROCESS, BY DESIGN (P2). An orphaned one has no
// process because the daemon died. A reconciler treating "no process" as
// orphaned would mark every loop that is merely waiting for an answer as
// crashed and try to recover it — which is why S7 reads `status='running'`
// specifically: it is the only state that means the daemon died mid-step.

// StepStatus is the four-valued lifecycle, with a CHECK behind it.
type StepStatus string

const (
	// StepRunning — a process is executing it. S7's recovery reads exactly
	// this on startup: the daemon died while a step was in it.
	StepRunning StepStatus = "running"
	// StepSuspended — waiting for an answer, with no process by design (P2).
	StepSuspended StepStatus = "suspended"
	// StepOrphaned — a running step the reconciler could not account for.
	StepOrphaned StepStatus = "orphaned"
	// StepDone — finished, whatever the outcome.
	StepDone StepStatus = "done"
)

// StepStatuses is every value, for the CHECK's test and for the CLI.
func StepStatuses() []StepStatus {
	return []StepStatus{StepRunning, StepSuspended, StepOrphaned, StepDone}
}

// ContextManifest is the declared context, frozen at first assembly (P5).
type ContextManifest struct {
	Files          []ContextFile `json:"files"`
	IterationsUsed int           `json:"iterationsUsed"`

	// DeadlineAt is ABSOLUTE, not remaining.
	//
	// A relative deadline cannot survive a suspension that lasts overnight: it
	// would grant the resumed step its whole budget again, and a step could
	// escape its bounds by suspending repeatedly. An absolute instant is the
	// same instant on both sides of the suspension.
	DeadlineAt string `json:"deadlineAt,omitempty"`
}

// ContextFile is one declared file and the hash of what was read.
//
// The store holds the hash; it does not verify it. Verification needs the
// filesystem, and this package touches no files (DB10).
type ContextFile struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// Step is a step's row.
type Step struct {
	ID            int64  `json:"id"`
	LoopSlug      string `json:"loopSlug"`
	ComponentRoot string `json:"componentRoot"`
	TaskID        string `json:"taskid,omitempty"`
	Phase         string `json:"phase"`
	Agent         string `json:"agent"`
	Attempt       int    `json:"attempt"`
	Status        string `json:"status"`

	StartedAt       string `json:"startedAt"`
	AgentReturnedAt string `json:"agentReturnedAt,omitempty"`
	EndedAt         string `json:"endedAt,omitempty"`
	AgentMS         *int   `json:"agentMs,omitempty"`
	WallMS          *int   `json:"wallMs,omitempty"`
	Outcome         string `json:"outcome,omitempty"`
	EndedBy         string `json:"endedBy"`
	Source          string `json:"source"`

	// Cost is reassembled from three columns by ReadStep. The state comes from
	// the column, never from whether USD is null (D12).
	Cost model.Cost `json:"cost"`

	// ContextManifest is nil when none was frozen — which is every step until
	// S5 assembles one.
	//
	// It is this package's VIEW of the manifest: the three fields a reader here
	// needs. A field the runtime added and this view does not declare is absent
	// from it, which is why the raw bytes are carried alongside.
	ContextManifest *ContextManifest `json:"contextManifest,omitempty"`

	// ContextManifestRaw is the column, byte for byte.
	//
	// # Why both
	//
	// The manifest's shape belongs to the runtime (K13, frozen at assembly).
	// This package stores it and must not narrow it — and the view above does
	// narrow it, which cost `maxIterations` and every file's `bytes` until the
	// demo walk printed `iterations 2 of 0`.
	//
	// So: the view for readers in this package, the bytes for everyone who owns
	// the shape. Anything crossing the protocol uses these bytes.
	ContextManifestRaw json.RawMessage `json:"contextManifestRaw,omitempty"`

	// PauseReason is empty for every step except one a PERSON stopped.
	// `Status` is `suspended` either way (P2); this is the only place the two
	// reasons — a model's real question, or a person's stop — are told apart.
	// See migration `0019`.
	PauseReason string `json:"pauseReason,omitempty"`

	// ThinkingDropped is the step's count of thinking blocks withheld from a
	// model call because their producer is not that provider (S18 T5, M4):
	// the transcript kept every block (M1), and this is the number that says
	// the WIRE omitted some. NULL (never recorded — every row before T5)
	// reads as 0; the count is an event, and no event is honestly zero here.
	// `SetStepThinkingDropped` is the only writer.
	ThinkingDropped int `json:"thinkingDropped,omitempty"`
	// RunSeq is the S39 CR2 tag: which run of the loop this step belonged to,
	// 0 when nothing tagged it (rows from before S39, or fixtures that never
	// went through MarkStepStarted). Cost per run is the group-by over this
	// (P32 floor) — Go readers were none until the tag needed one to prove
	// itself; this field is honest exposure, not a stored total.
	RunSeq int `json:"runSeq,omitempty"`
}

// NewStep is what a caller supplies to create one.
type NewStep struct {
	TaskID  string
	Phase   string
	Agent   string
	Attempt int
	Status  StepStatus
	Source  string
}

// CreateStep inserts a step and returns it with its allocated id.
func (s *Store) CreateStep(sc Scope, in NewStep) (Step, error) {
	if !sc.Valid() {
		return Step{}, fmt.Errorf("store: CreateStep needs both a slug and a component root, got %s", sc)
	}

	status := in.Status
	if status == "" {
		status = StepRunning
	}
	attempt := in.Attempt
	if attempt <= 0 {
		attempt = 1
	}

	now := s.Now()
	res, err := s.w.Exec(`
		INSERT INTO step (
			loop_slug, component_root, taskid, phase, agent, attempt, status,
			started_at, ended_by, source, cost_state
		) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		sc.LoopSlug, sc.ComponentRoot, nullable(in.TaskID), orDefault(in.Phase, string(PhaseBuild)),
		orDefault(in.Agent, "unknown"), attempt, string(status), now, "open",
		orDefault(in.Source, "live"), string(model.CostUnknown))
	if err != nil {
		return Step{}, fmt.Errorf("store: create a step in %s: %w", sc, err)
	}

	// last_insert_rowid() through LastInsertId, which is safe precisely because
	// the writer pool is at one connection (Q7): the id returned belongs to the
	// statement just executed on that connection, and there is no other.
	id, err := res.LastInsertId()
	if err != nil {
		return Step{}, fmt.Errorf("store: read the id of the new step in %s: %w", sc, err)
	}
	return s.ReadStep(id)
}

const stepColumns = `id, loop_slug, component_root, COALESCE(taskid,''), phase, agent, attempt, status,
	started_at, COALESCE(agent_returned_at,''), COALESCE(ended_at,''), agent_ms, wall_ms,
	COALESCE(outcome,''), ended_by, source,
	cost_usd, cost_state, COALESCE(cost_detail,''), COALESCE(context_manifest,''),
	COALESCE(pause_reason,''), COALESCE(thinking_dropped,0),
	COALESCE(run_seq,0)` // S39 CR2: 0 reads honestly as UNtagged (legacy rows and pre-run fixtures)

func scanStep(row interface{ Scan(...any) error }) (Step, error) {
	var st Step
	var costUSD sql.NullFloat64
	var costState, costDetail, manifest string

	if err := row.Scan(
		&st.ID, &st.LoopSlug, &st.ComponentRoot, &st.TaskID, &st.Phase, &st.Agent, &st.Attempt, &st.Status,
		&st.StartedAt, &st.AgentReturnedAt, &st.EndedAt, &st.AgentMS, &st.WallMS,
		&st.Outcome, &st.EndedBy, &st.Source,
		&costUSD, &costState, &costDetail, &manifest, &st.PauseReason, &st.ThinkingDropped,
		&st.RunSeq); err != nil {
		return Step{}, err
	}

	cost, err := decodeCost(costUSD, costState, costDetail)
	if err != nil {
		return Step{}, err
	}
	st.Cost = cost

	if manifest != "" {
		var cm ContextManifest
		if err := json.Unmarshal([]byte(manifest), &cm); err != nil {
			return Step{}, fmt.Errorf("the context manifest of step %d is not valid JSON: %w", st.ID, err)
		}
		st.ContextManifest = &cm
		// The bytes too, so a consumer that owns the shape is not limited to
		// the fields this package happens to declare.
		st.ContextManifestRaw = json.RawMessage(manifest)
	}
	return st, nil
}

// ReadStep reads one step by id.
func (s *Store) ReadStep(id int64) (Step, error) {
	row := s.r.QueryRow(`SELECT `+stepColumns+` FROM step WHERE id = ?`, id)
	st, err := scanStep(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Step{}, fmt.Errorf("store: step %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Step{}, fmt.Errorf("store: read step %d: %w", id, err)
	}
	return st, nil
}

// ListSteps reads a loop's steps, oldest first.
func (s *Store) ListSteps(sc Scope) ([]Step, error) {
	where, args := sc.Where("")
	rows, err := s.r.Query(`SELECT `+stepColumns+` FROM step WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list the steps of %s: %w", sc, err)
	}
	defer rows.Close()
	return collectSteps(rows)
}

// StepsByStatus reads every step in a status, across every component.
//
// This is S7's recovery query, written here so recovery needs nothing from the
// store that resume does not (§3a). Unscoped on purpose: a daemon recovering
// after a crash does not know which projects it was serving.
func (s *Store) StepsByStatus(status StepStatus) ([]Step, error) {
	rows, err := s.r.Query(`SELECT `+stepColumns+` FROM step WHERE status = ? ORDER BY id`, string(status))
	if err != nil {
		return nil, fmt.Errorf("store: list %s steps: %w", status, err)
	}
	defer rows.Close()
	return collectSteps(rows)
}

func collectSteps(rows *sql.Rows) ([]Step, error) {
	var out []Step
	for rows.Next() {
		st, err := scanStep(rows)
		if err != nil {
			return nil, fmt.Errorf("store: read a step: %w", err)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// SetStepStatus moves a step's status.
//
// `done` also stamps `ended_at` when it is not already set, because a step
// that is done and has no end time is a step nothing can measure.
func (s *Store) SetStepStatus(id int64, status StepStatus, outcome string) error {
	var res sql.Result
	var err error
	if status == StepDone {
		res, err = s.w.Exec(`
			UPDATE step SET status = ?, outcome = ?, ended_at = COALESCE(ended_at, ?), ended_by = ?
			WHERE id = ?`, string(status), nullable(outcome), s.Now(), "closed", id)
	} else {
		res, err = s.w.Exec(`UPDATE step SET status = ?, outcome = COALESCE(?, outcome) WHERE id = ?`,
			string(status), nullable(outcome), id)
	}
	if err != nil {
		return fmt.Errorf("store: set the status of step %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set the status of step %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: step %d: %w", id, ErrNotFound)
	}
	return nil
}

// SetStepPauseReason records why a step is suspended with no pending question.
//
// Empty CLEARS it — the same convention `COALESCE(?, outcome)` uses elsewhere
// in this file: writing nothing is a real instruction here, not "leave it".
// Called twice in a stop/continue round trip: once with a reason when the
// daemon stops a step, once empty when it resumes — so a step that is running
// again never carries a stale reason from the time it was not.
func (s *Store) SetStepPauseReason(id int64, reason string) error {
	res, err := s.w.Exec(`UPDATE step SET pause_reason = ? WHERE id = ?`, nullable(reason), id)
	if err != nil {
		return fmt.Errorf("store: set the pause reason of step %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set the pause reason of step %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: step %d: %w", id, ErrNotFound)
	}
	return nil
}

// --- cost: one function in, one function out --------------------------------

const setCostStatement = `UPDATE step SET cost_usd = ?, cost_state = ?, cost_detail = ? WHERE id = ?`

// SetStepCost writes a step's cost, decomposing S3's Cost into three columns.
//
// THE ONLY writer of those three columns. Everything about cost honesty depends
// on that: `cost_state` and `cost_usd` must agree, and two call sites cannot be
// made to agree by review.
func (s *Store) SetStepCost(id int64, cost model.Cost) error {
	usd, state, detail, err := encodeCost(cost)
	if err != nil {
		return err
	}
	res, err := s.w.Exec(setCostStatement,
		usd, state, detail, id)
	if err != nil {
		return fmt.Errorf("store: set the cost of step %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set the cost of step %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: step %d: %w", id, ErrNotFound)
	}
	return nil
}

// SetStepThinkingDropped records what the wires withheld (S18 T5, M4/O1):
// the step's count of thinking blocks dropped at request build because
// their producer is not the called provider. THE ONLY writer of the column.
//
// It is written once, at step completion, and only when a block actually
// fell: a zero left NULL is every step that dropped nothing, and it reads
// back as 0 — honest here, because the number counts an event that either
// happened or did not (unlike cost, whose third state is a real thing,
// DB11; this has no unmeasured state — the adapter that builds the request
// always knows what it omitted).
func (s *Store) SetStepThinkingDropped(id int64, dropped int) error {
	if dropped < 0 {
		return fmt.Errorf("store: step %d: a negative drop count is not a number, it is a bug", id)
	}
	res, err := s.w.Exec(`UPDATE step SET thinking_dropped = ? WHERE id = ?`, dropped, id)
	if err != nil {
		return fmt.Errorf("store: set the thinking-drop count of step %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set the thinking-drop count of step %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: step %d: %w", id, ErrNotFound)
	}
	return nil
}

// costDetail is the JSON in `cost_detail`: the half of a Cost that is not a
// number.
//
// Two fields rather than one generic one, because `missing` is a list of what
// could not be priced and `reason` is a sentence about why nothing could. A
// single "detail" string would make `partial` and `unknown` carry the same
// shape and lose the distinction on the way back in.
type costDetail struct {
	Missing []string `json:"missing,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

func encodeCost(cost model.Cost) (usd any, state string, detail any, err error) {
	switch cost.State {
	case model.CostKnown:
		return cost.USD, string(model.CostKnown), nil, nil

	case model.CostPartial:
		d, mErr := json.Marshal(costDetail{Missing: cost.Missing})
		if mErr != nil {
			return nil, "", nil, fmt.Errorf("store: encode a partial cost: %w", mErr)
		}
		// The FLOOR goes in cost_usd. A partial cost has a number and it is a
		// lower bound; storing NULL here would discard the part that was
		// measured, which is the part somebody is about to be billed for.
		return cost.USDAtLeast, string(model.CostPartial), string(d), nil

	case model.CostUnknown:
		d, mErr := json.Marshal(costDetail{Reason: cost.Reason})
		if mErr != nil {
			return nil, "", nil, fmt.Errorf("store: encode an unknown cost: %w", mErr)
		}
		// An unknown cost may still carry a measured floor (P32: an aggregate
		// over uncertain values keeps the floor). NULL only when there is not
		// even that.
		if cost.USDAtLeast > 0 {
			return cost.USDAtLeast, string(model.CostUnknown), string(d), nil
		}
		return nil, string(model.CostUnknown), string(d), nil

	case "":
		// An unset state is `unknown`, not `known` with a zero. "Nobody has
		// said" and "it cost nothing" are different facts, and only one of them
		// is ever true of a model call.
		return nil, string(model.CostUnknown), nil, nil

	default:
		return nil, "", nil, fmt.Errorf("store: %q is not a cost state; the three are known, partial and unknown", cost.State)
	}
}

// decodeCost reassembles a Cost, reading the STATE FROM THE COLUMN.
//
// Nothing here looks at whether `cost_usd` is null to decide the state. That is
// the whole of D12, and the test for it stores a `partial` and a `known` that
// BOTH have a non-null `cost_usd` and asserts they come back distinguishable —
// which an implementation inferring state from nullity cannot do, while every
// positive test would still pass against it.
func decodeCost(usd sql.NullFloat64, state, detail string) (model.Cost, error) {
	var d costDetail
	if detail != "" {
		if err := json.Unmarshal([]byte(detail), &d); err != nil {
			return model.Cost{}, fmt.Errorf("a stored cost_detail is not valid JSON: %w", err)
		}
	}

	switch model.CostState(state) {
	case model.CostKnown:
		return model.Cost{State: model.CostKnown, USD: usd.Float64}, nil
	case model.CostPartial:
		return model.Cost{State: model.CostPartial, USDAtLeast: usd.Float64, Missing: d.Missing}, nil
	case model.CostUnknown:
		return model.Cost{State: model.CostUnknown, USDAtLeast: usd.Float64, Reason: d.Reason}, nil
	default:
		// The CHECK constraint makes this unreachable through Aeon's own
		// writes. It is reported rather than defaulted because the only way to
		// get here is a row written by something else, and guessing which state
		// it meant is how a `partial` becomes a `known`.
		return model.Cost{}, fmt.Errorf("a stored cost_state is %q, which is not one of known, partial or unknown", state)
	}
}

// SetContextManifest freezes the declared context on a step.
//
// It takes this package's view and RE-ENCODES it, which loses any field the
// view does not declare. `SetContextManifestJSON` is the one the owner of the
// shape should call — see the note there.
func (s *Store) SetContextManifest(id int64, cm ContextManifest) error {
	encoded, err := json.Marshal(cm)
	if err != nil {
		return fmt.Errorf("store: encode the context manifest of step %d: %w", id, err)
	}
	return s.SetContextManifestJSON(id, encoded)
}

// SetContextManifestJSON stores the manifest BYTE FOR BYTE.
//
// # Found by walking S5's demo
//
// `aeon step show 1 --manifest` printed `iterations 2 of 0`. The manifest is
// the RUNTIME's shape (K13, frozen at assembly) and this package declared a
// narrower view of it — so the step handler's manifest went through
// `json.Unmarshal` into that view and back out, and every field the view did
// not name was silently dropped. Two of them were: `maxIterations`, which is
// the whole point of carrying the bound on the manifest rather than reading
// today's default, and each file's `bytes`.
//
// The column is TEXT holding JSON, so there was never a reason to re-encode.
// `ContextManifest` stays as the READ view — a reader that wants three fields
// should not have to know about five — but the write preserves what it was
// given.
func (s *Store) SetContextManifestJSON(id int64, raw json.RawMessage) error {
	if !json.Valid(raw) {
		// Refused rather than stored: a column holding text that is not JSON
		// is a column every later reader has to guard, and the guard is always
		// written by somebody debugging something else.
		return fmt.Errorf("store: the context manifest of step %d is not valid JSON", id)
	}
	res, err := s.w.Exec(`UPDATE step SET context_manifest = ? WHERE id = ?`, string(raw), id)
	if err != nil {
		return fmt.Errorf("store: set the context manifest of step %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set the context manifest of step %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: step %d: %w", id, ErrNotFound)
	}
	return nil
}

// LoopCostRows builds the rows the cost ledger groups by (S18 T7, M26): one
// per step, the cost from the step's own columns (D12: the state comes from
// the column) and the provenance from its LAST assistant turn — the turn that
// answered, whose usage is the one that was billed.
//
// The last turn, not the first: a step that retried onto another provider
// answered from somewhere, and billing follows the answer. A step with no
// assistant turn — never reached a model, or predates provenance — keeps the
// empty triple and lands on the ledger's unattributed line, which is the
// absence spoken aloud rather than a row that guesses (P31, I5).
//
// This package READS; it computes nothing about money (K22 keeps that even
// further out): grouping and totals belong to model.AttributeCosts, and one
// function is what the CLI table and the app's panel both render (D16).
func (s *Store) LoopCostRows(sc Scope) ([]model.CostRow, error) {
	steps, err := s.ListSteps(sc)
	if err != nil {
		return nil, err
	}
	rows := make([]model.CostRow, 0, len(steps))
	for _, st := range steps {
		row := model.CostRow{StepID: st.ID, Cost: st.Cost}
		tr := s.ReadTranscript(st.ID)
		// Later assistant turns overwrite earlier ones — last-wins is the
		// reading above, and a read failure simply leaves the empty triple
		// (the unattributed line), because a transcript this package cannot
		// parse has already said so through its own migration verdict.
		for _, m := range tr.Messages {
			if m.Role == model.RoleAssistant && m.Provenance != nil {
				row.RouteID = m.Provenance.Route
				row.Provider = m.Provenance.Provider
				row.Model = m.Provenance.Model
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

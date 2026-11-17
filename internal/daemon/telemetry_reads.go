package daemon

// telemetry_reads.go — S33 T8: the adapter behind the socket's four
// telemetry reads (protocol.TelemetryStore). Store reads and nothing else
// except the ONE derived number the protocol layer cannot compute itself:
// currentContextTokens is runtime.EstimateTokens over the step's
// transcript rows — the same estimator the compaction budget runs on
// (compact.go), so the wire's "what the next call would carry" and the
// runtime's own sizing can never disagree. That is why the derivation
// lives HERE, in the only layer that may know both runtime and store, and
// not in a handler that would have to import one of them to fake the other.
//
// Paging: step_turn is the one paged read (T1 capped it at 500/page,
// default 100); the adapter walks pages to the end rather than answering a
// truncated detail list — a caller reading a loop's whole history gets its
// whole history. The other three detail tables are unbounded reads.
//
// Transcript parse failure is said, not zeroed: an unparseable transcript
// (a from-the-future store) cannot yield an estimate, and a 0 there would
// be the confident wrong answer P31 exists to prevent — the read refuses
// with that sentence instead. (CumulativeTokensTotal, read from billed
// rows, would still have been fine; it is the CURRENT number that cannot be
// fabricated. Both numbers, or neither — D2's pair is atomic.)

import (
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

type telemetryReadStore struct {
	s *store.Store
}

// newTelemetryReadStore follows the newTelemetrySink convention: a nil
// store yields a nil seam (typed-nil trap avoided by returning the
// interface only through this function), and the protocol handlers answer
// ErrStoreUnavailable.
func newTelemetryReadStore(s *store.Store) protocol.TelemetryStore {
	if s == nil {
		return nil
	}
	return &telemetryReadStore{s: s}
}

// absent translates the store's not-found into the protocol's own sentinel
// — the seam is where the two packages meet, so it is the seam that
// translates, never a handler reaching across.
func absent(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return protocol.ErrNoTelemetryRow
	}
	return err
}

// sums converts the store's totals to the wire twin. The conversion is
// legal because the field sets are identical — and that legality is the
// drift detector: renaming a column on one side turns the compile itself
// into the failure, which is what the guard demanded here.
func sums(d store.TelemetrySums) protocol.TelemetrySums {
	return protocol.TelemetrySums(d)
}

func (t *telemetryReadStore) ComponentTelemetry(componentID string) (protocol.ComponentTelemetry, error) {
	row, err := t.s.ComponentTelemetry(componentID)
	if err != nil {
		return protocol.ComponentTelemetry{}, absent(err)
	}
	return protocol.ComponentTelemetry{
		ComponentID:   row.ComponentID,
		TelemetrySums: sums(row.TelemetrySums),
		UpdatedAt:     row.UpdatedAt,
		Exists:        row.Exists,
	}, nil
}

func (t *telemetryReadStore) ProjectTelemetry(projectID string) (protocol.ProjectTelemetry, error) {
	row, err := t.s.ComponentTelemetry(projectID)
	if err != nil {
		return protocol.ProjectTelemetry{}, absent(err)
	}
	return protocol.ProjectTelemetry{
		ProjectID:     row.ComponentID,
		TelemetrySums: sums(row.TelemetrySums),
		UpdatedAt:     row.UpdatedAt,
		Exists:        row.Exists,
	}, nil
}

func (t *telemetryReadStore) WorkspaceTelemetry() (protocol.WorkspaceTelemetry, error) {
	row, err := t.s.WorkspaceTelemetry()
	if err != nil {
		return protocol.WorkspaceTelemetry{}, absent(err)
	}
	return protocol.WorkspaceTelemetry{
		TelemetrySums: sums(row.TelemetrySums),
		UpdatedAt:     row.UpdatedAt,
		Exists:        row.Exists,
	}, nil
}

func (t *telemetryReadStore) LoopTelemetry(stepID int64) (protocol.TelemetryLoopData, error) {
	var zero protocol.TelemetryLoopData
	step, err := t.s.ReadStep(stepID)
	if err != nil {
		return zero, absent(err) // names the step for the handler's refusal
	}
	var turns []protocol.TurnRow
	for offset := 0; ; {
		page, err := t.s.StepTurns(stepID, 0, offset)
		if err != nil {
			return zero, err
		}
		for _, row := range page {
			turns = append(turns, protocol.TurnRow(row))
		}
		if len(page) < 100 {
			break
		}
		offset += len(page)
	}
	comps, err := t.s.StepCompactions(stepID)
	if err != nil {
		return zero, err
	}
	var compRows []protocol.CompactionRow
	for _, row := range comps {
		compRows = append(compRows, protocol.CompactionRow(row))
	}
	files, err := t.s.StepFileActivities(stepID)
	if err != nil {
		return zero, err
	}
	var fileRows []protocol.FileActivityRow
	for _, row := range files {
		fileRows = append(fileRows, protocol.FileActivityRow(row))
	}
	cmds, err := t.s.StepCommandActivities(stepID)
	if err != nil {
		return zero, err
	}
	var cmdRows []protocol.CommandActivityRow
	for _, row := range cmds {
		cmdRows = append(cmdRows, protocol.CommandActivityRow(row))
	}
	tr := t.s.ReadTranscript(stepID)
	if !tr.OK {
		return zero, errUnparseableTranscript(stepID)
	}
	cum, err := t.s.CumulativeTokensTotal(stepID)
	if err != nil {
		return zero, err
	}
	var composition []byte
	if raw, ok, err := t.s.StepContextManifest(stepID); err != nil {
		return zero, err
	} else if ok && raw != "" {
		composition = []byte(raw)
	}
	return protocol.TelemetryLoopData{
		Step: protocol.TelemetryStep{
			ID:                 step.ID,
			LoopSlug:           step.LoopSlug,
			ComponentRoot:      step.ComponentRoot,
			TaskID:             step.TaskID,
			Phase:              step.Phase,
			Agent:              step.Agent,
			Attempt:            step.Attempt,
			Status:             step.Status,
			StartedAt:          step.StartedAt,
			AgentReturnedAt:    step.AgentReturnedAt,
			EndedAt:            step.EndedAt,
			AgentMS:            step.AgentMS,
			WallMS:             step.WallMS,
			Outcome:            step.Outcome,
			EndedBy:            step.EndedBy,
			Source:             step.Source,
			Cost:               step.Cost,
			ContextManifestRaw: step.ContextManifestRaw,
		},
		Turns:                 turns,
		Compactions:           compRows,
		FileActivities:        fileRows,
		CommandActivities:     cmdRows,
		CurrentContextTokens:  int64(runtime.EstimateTokens(tr.Messages)),
		CumulativeTokensTotal: cum,
		Composition:           composition,
	}, nil
}

// errUnparseableTranscript is the refusal, worded to name what failed:
// the CURRENT token number is an estimate over parsed rows, and a
// transcript that will not parse (from-the-future or corrupt) has no
// parseable rows to estimate — fabricating 0 there would be TL11's exact
// forbid, a number where the honest answer is "cannot tell".
func errUnparseableTranscript(stepID int64) error {
	return fmt.Errorf("daemon: step %d's transcript does not parse, so its current context cannot be honestly estimated — cumulative-only readers should not fabricate a zero", stepID)
}

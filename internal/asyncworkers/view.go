package asyncworkers

import (
	"encoding/json"

	"github.com/cjlapao/aeon/internal/store"
)

// View is a worker's answer to anyone — the socket read's payload and the
// event's payload, the same shape (one row, one rendering; a second View
// type would be a second truth). The row is the source of truth: events
// carry the View so a subscriber can paint without a read-back for the
// common case, and a subscriber that races an event reads the same truth
// through asyncworkers.get (S14's discipline, adopted).
//
// Field names are the WIRE names (camelCase, the protocol's convention);
// metadata is decoded to an object, result travels as raw JSON — a runner
// that stored `{"files":3}` reaches the app as an object, and a runner
// that stored non-JSON (impossible through the manager's own guard) would
// travel as a string rather than corrupt the envelope.
type View struct {
	WorkerID        string            `json:"workerId"`
	WorkerType      string            `json:"workerType"`
	ComponentRoot   string            `json:"componentRoot,omitempty"`
	State           string            `json:"state"`
	CancelRequested bool              `json:"cancelRequested,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	Progress        *float64          `json:"progress,omitempty"`
	ProgressNote    string            `json:"progressNote,omitempty"`
	Result          json.RawMessage   `json:"result,omitempty"`
	Error           string            `json:"error,omitempty"`
	QueuedAt        string            `json:"queuedAt"`
	StartedAt       string            `json:"startedAt,omitempty"`
	FinishedAt      string            `json:"finishedAt,omitempty"`
}

// ViewOf renders a row. It does not fail: an unreadable metadata column
// (written outside the manager — there is no such door) degrades to no
// metadata rather than swallowing the worker, because "a worker exists and
// I cannot show its tags" beats "no worker".
func ViewOf(row *store.AsyncWorkerRow) *View {
	if row == nil {
		return nil
	}
	v := &View{
		WorkerID:        row.WorkerID,
		WorkerType:      row.WorkerType,
		ComponentRoot:   row.ComponentRoot,
		State:           row.State,
		CancelRequested: row.CancelRequested,
		Progress:        row.Progress,
		ProgressNote:    row.ProgressNote,
		Error:           row.Error,
		QueuedAt:        row.QueuedAt,
		StartedAt:       row.StartedAt,
		FinishedAt:      row.FinishedAt,
	}
	if md, err := unmarshalMetadata(row.Metadata); err == nil && len(md) > 0 {
		v.Metadata = md
	}
	if row.Result != "" {
		if json.Valid([]byte(row.Result)) {
			v.Result = json.RawMessage(row.Result)
		} else {
			b, _ := json.Marshal(row.Result)
			v.Result = b
		}
	}
	return v
}

// ViewsOf renders a page of rows.
func ViewsOf(rows []store.AsyncWorkerRow) []View {
	out := make([]View, 0, len(rows))
	for i := range rows {
		if v := ViewOf(&rows[i]); v != nil {
			out = append(out, *v)
		}
	}
	return out
}

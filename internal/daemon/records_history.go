// records_history.go — S53 T3 (HV4): the two history arms of the records
// service. Both are reads that forward to the SAME shared answers the
// person's socket doors run — protocol.AnswerHistory* is the one rendering
// each door shares (rule 4), so the tool door and the socket door cannot
// drift field for field: history_list twins history.list and history_show
// twins history.show, one implementation behind two doors.
//
// What this file does NOT do, and each is a rule rather than an omission:
//
//   - it holds no export arm. history.export stays the person's door
//     (HV5); a step asking for a dump is answered by history_list with the
//     same rows, and the NDJSON emission lives on the CLI, not here —
//     T2's guard refuses a history_export tool by name;
//   - it holds no coverage or vocabulary arm either (HV4): the two new
//     S53 T1 doors stay the app's — an agent that wants to know what a
//     lane leaves out can read the same rows the person reads, and the
//     census door is the human surface's own question;
//   - it never writes: the facade it holds (the same *history.Store the
//     Table seam holds) is the read-only one, whose exported method set is
//     closed to List/Show/Coverage by T2's guard;
//   - it takes no identity from the input — the reads are the STAMPED
//     step's project scope only through the filters the schema names
//     (projectRoot is a filter word, not an identity claim; the history
//     query itself is the unscoped span the socket's own door runs).

package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
)

// historyList answers history_list through protocol.AnswerHistoryList —
// the ONE list answer the person's history.list door runs, rows and the
// honest-empty statement included (P31: "no history" is a fact, never a
// gap). The filter refusals ride through verbatim (P3).
func (r *RecordsService) historyList(_ context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.historyPlane == nil {
		return nil, historyPlanelessRefusal("history_list"), nil
	}
	var in protocol.HistoryListParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := protocol.AnswerHistoryList(r.historyPlane, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// historyShow answers history_show through protocol.AnswerHistoryShow —
// the ONE drill-down answer the person's history.show door runs: the empty
// id and the unknown id are refused BY NAME in the door's own words (DB19),
// a wrong-door entry travels as the verbatim refusal, never an empty read.
func (r *RecordsService) historyShow(_ context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.historyPlane == nil {
		return nil, historyPlanelessRefusal("history_show"), nil
	}
	var in protocol.HistoryShowParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := protocol.AnswerHistoryShow(r.historyPlane, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// historyPlanelessRefusal is the V13 shape for the family: the store is
// what honours these doors (the merge, the rollups, the cold archive), and
// a storeless mount says so by name, never answering an empty history.
func historyPlanelessRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the history store, and this mount did not wire it — absent is stated, never faked (V13)", name)}
}

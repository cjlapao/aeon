package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/model"
)

type RecoveryRecord struct {
	StepID      int64            `json:"stepId"`
	Case        string           `json:"case"`
	Reissued    bool             `json:"reissued"`
	Reason      string           `json:"reason"`
	UpdatedAt   string           `json:"updatedAt"`
	Failure     *RecoveryFailure `json:"failure,omitempty"`
	PriorCost   *model.Cost      `json:"priorCost,omitempty"`
	ReissueCost *model.Cost      `json:"reissueCost,omitempty"`
}

type RecoveryFailure struct {
	Kind      string `json:"kind"`
	Tool      string `json:"tool,omitempty"`
	ToolUseID string `json:"toolUseId,omitempty"`
	Detail    string `json:"detail"`
}

type RecoveryQuestion struct {
	ToolUseID string
	Kind      QuestionKind
	Body      string
	Options   []string
}

// RunningStepIDs deliberately avoids decoding a corrupt row: one damaged
// manifest must not prevent every other running step from being recovered.
func (s *Store) RunningStepIDs() ([]int64, error) {
	rows, err := s.r.Query(`SELECT id FROM step WHERE status='running' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) ReadRecovery(stepID int64) (RecoveryRecord, bool, error) {
	var r RecoveryRecord
	var reissued int
	var failure, priorCost, reissueCost sql.NullString
	err := s.r.QueryRow(`SELECT step_id,case_name,reissued,reason,updated_at,failure_json,prior_cost_json,reissue_cost_json FROM step_recovery WHERE step_id=?`, stepID).
		Scan(&r.StepID, &r.Case, &reissued, &r.Reason, &r.UpdatedAt, &failure, &priorCost, &reissueCost)
	if err == sql.ErrNoRows {
		return RecoveryRecord{}, false, nil
	}
	if err != nil {
		return RecoveryRecord{}, false, err
	}
	r.Reissued = reissued != 0
	if failure.Valid {
		var parsed RecoveryFailure
		if err := json.Unmarshal([]byte(failure.String), &parsed); err != nil {
			return RecoveryRecord{}, false, err
		}
		r.Failure = &parsed
	}
	if priorCost.Valid {
		r.PriorCost = new(model.Cost)
		if err := json.Unmarshal([]byte(priorCost.String), r.PriorCost); err != nil {
			return RecoveryRecord{}, false, err
		}
	}
	if reissueCost.Valid {
		r.ReissueCost = new(model.Cost)
		if err := json.Unmarshal([]byte(reissueCost.String), r.ReissueCost); err != nil {
			return RecoveryRecord{}, false, err
		}
	}
	return r, true, nil
}

func (s *Store) SaveRecoveryObservation(ctx context.Context, stepID int64, caseName, reason string, reissued bool) error {
	_, err := s.w.ExecContext(ctx, `INSERT INTO step_recovery(step_id,case_name,reissued,reason,updated_at)
		VALUES(?,?,?,?,?) ON CONFLICT(step_id) DO UPDATE SET case_name=excluded.case_name,
		reissued=step_recovery.reissued OR excluded.reissued,reason=excluded.reason,updated_at=excluded.updated_at`,
		stepID, caseName, reissued, reason, s.Now())
	return err
}

// CommitRecoveredStep writes the final transcript, snapshot, cost, status and
// recovery explanation in one transaction. It refuses a step no longer running.
//
// pauseReason is the S55 T7 half of a suspension that has NO question: a step
// the budget engine capped (E2) parks suspended with pause_reason='cap', and
// there is no question row to write. A step with question != nil always takes
// the question's path; pauseReason != "" (with no question) suspends the step
// and stamps its pause_reason in the same transaction — a suspended step with
// a pause reason and no question is the cap's shape, and a recovery that
// committed it cannot lose the reason.
func (s *Store) CommitRecoveredStep(ctx context.Context, stepID int64, msgs []model.Message,
	manifest json.RawMessage, cost model.Cost, outcome, caseName, reason, pauseReason string, reissued bool, question *RecoveryQuestion, failure *RecoveryFailure, recoveryCosts ...model.Cost) error {
	if len(msgs) > 0 {
		if valid := model.ValidateTranscript(msgs); !valid.OK {
			return fmt.Errorf("recovered transcript: %s", valid.Reason)
		}
	}
	usd, state, detail, err := encodeCost(cost)
	if err != nil {
		return err
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM step_message WHERE step_id=?`, stepID); err != nil {
		return err
	}
	for i, msg := range msgs {
		content, err := marshalBlocks(msg)
		if err != nil {
			return err
		}
		prov, err := marshalProvenance(msg)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO step_message(step_id,seq,format_version,role,content,provenance,created_at) VALUES(?,?,?,?,?,?,?)`, stepID, i+1, model.FormatVersion, string(msg.Role), content, prov, s.Now()); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, setCostStatement, usd, state, detail, stepID); err != nil {
		return err
	}
	status := "done"
	if question != nil || pauseReason != "" {
		status = "suspended"
	}
	res, err := tx.ExecContext(ctx, `UPDATE step SET context_manifest=?,status=?,outcome=?,ended_at=CASE WHEN ?='done' THEN COALESCE(ended_at,?) ELSE ended_at END,ended_by=CASE WHEN ?='done' THEN 'closed' ELSE ended_by END
		WHERE id=? AND status='running'`, string(manifest), status, nullable(outcome), status, s.Now(), status, stepID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("step %d is no longer running", stepID)
	}
	// The cap suspension's pause reason rides its own column (the question
	// path writes none: a question IS the reason to wait, and a cap has no
	// question row). Set beside the status flip in the same transaction, so
	// a reader that sees suspended/cap never sees suspended with an empty
	// reason (the two are one state, P31).
	if question == nil && pauseReason != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE step SET pause_reason=? WHERE id=?`, pauseReason, stepID); err != nil {
			return err
		}
	}
	if question != nil {
		if question.Body == "" {
			return fmt.Errorf("recovered question has no body")
		}
		kind := question.Kind
		if kind == "" {
			kind = QuestionFree
		}
		var options any
		if len(question.Options) > 0 {
			encoded, err := json.Marshal(question.Options)
			if err != nil {
				return err
			}
			options = string(encoded)
		}
		var slug, root string
		if err := tx.QueryRowContext(ctx, `SELECT loop_slug,component_root FROM step WHERE id=?`, stepID).Scan(&slug, &root); err != nil {
			return err
		}
		q, err := tx.ExecContext(ctx, `INSERT INTO question(loop_slug,component_root,step_id,tool_use_id,kind,body,options,asked_at) VALUES(?,?,?,?,?,?,?,?)`,
			slug, root, stepID, question.ToolUseID, string(kind), question.Body, options, s.Now())
		if err != nil {
			return err
		}
		qid, err := q.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := appendThread(tx, s.Now(), Scope{LoopSlug: slug, ComponentRoot: root}, NewChatMessage{Author: "agent", Kind: "question", Body: question.Body}, &qid); err != nil {
			return err
		}
	}
	var failureJSON any
	if failure != nil {
		encoded, err := json.Marshal(failure)
		if err != nil {
			return err
		}
		failureJSON = string(encoded)
	}
	var priorCostJSON, reissueCostJSON any
	if len(recoveryCosts) == 2 {
		prior, err := json.Marshal(recoveryCosts[0])
		if err != nil {
			return err
		}
		reissue, err := json.Marshal(recoveryCosts[1])
		if err != nil {
			return err
		}
		priorCostJSON, reissueCostJSON = string(prior), string(reissue)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO step_recovery(step_id,case_name,reissued,reason,updated_at,failure_json,prior_cost_json,reissue_cost_json) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(step_id) DO UPDATE SET case_name=excluded.case_name,reissued=step_recovery.reissued OR excluded.reissued,
		reason=excluded.reason,updated_at=excluded.updated_at,failure_json=excluded.failure_json,
		prior_cost_json=excluded.prior_cost_json,reissue_cost_json=excluded.reissue_cost_json`, stepID, caseName, reissued, reason, s.Now(), failureJSON, priorCostJSON, reissueCostJSON)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// plugin_state.go is S15 T3's row layer (migration 0023). The domain
// decisions — the four states, the transition table, revoke's reason
// demand — live in internal/plugin; every statement over plugin_agent and
// plugin_transition lives HERE, as the slice's I2 discipline demands, and
// the two rules that must survive even a deleted domain package are CHECK
// constraints: revoked is terminal (no admitted (from,to) pair names revoked
// as FROM), and a revoke without a reason is UNWRITABLE, not merely refused.

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// PluginAgentStates is the CHECK vocabulary mirrored from migration 0023 —
// the three states that are ROWS. internal/plugin's PersistedStates is
// pinned equal to this list by its own test (the S14 status-vocabulary
// pattern): one vocabulary, two homes, a test that notices drift.
var PluginAgentStates = []string{"disabled", "enabled", "revoked"}

// PluginRefusal is one arrival refusal (AP7): the Task completed — it was
// paid for — and its words were stopped at the door. reached_prompt is
// written 0 and the test asserts the read: a refusal that reached a prompt
// is not a row, it is a bug.
type PluginRefusal struct {
	ID            int64  `json:"id"`
	Agent         string `json:"agent"`
	TaskID        *int64 `json:"taskId,omitempty"`
	RevokedAt     string `json:"revokedAt"`
	Reason        string `json:"reason"`
	CostJSON      string `json:"costJson,omitempty"`
	CostKnown     bool   `json:"costKnown"`
	RecordedAt    string `json:"recordedAt"`
	ReachedPrompt bool   `json:"reachedPrompt"`
}

// CreatePluginRefusal records the refusal the database cannot lose. The
// cost is RECORDED, not recovered — it happened; pretending otherwise makes
// the ledger lie (T5's scope point 4). cost_known=false is the honest
// unknown (an unreported cost), never a zero.
func (s *Store) CreatePluginRefusal(agent, revokedAt, reason, costJSON string, costKnown bool, taskID *int64) error {
	if agent == "" || revokedAt == "" || reason == "" {
		return fmt.Errorf("refused: an arrival refusal needs agent, revoked_at and reason — the three future readers ask about")
	}
	if costKnown && costJSON == "" || !costKnown && costJSON != "" {
		return fmt.Errorf("refused: cost_known and cost_json must agree (unknown is unknown, not zero)")
	}
	_, err := s.w.Exec(`INSERT INTO plugin_refusal (agent, task_id, revoked_at, reason, cost_json, cost_known, recorded_at, reached_prompt) VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
		agent, taskID, revokedAt, reason, nullable(costJSON), boolInt(costKnown), s.Now())
	return err
}

// PluginRefusals reads an agent's stopped arrivals, oldest first. The read
// is three-state at the CALLER (a failed read is UNAVAILABLE, never an
// empty list).
func (s *Store) PluginRefusals(agent string) ([]PluginRefusal, error) {
	rows, err := s.r.Query(`SELECT id, agent, task_id, revoked_at, reason, cost_json, cost_known, recorded_at, reached_prompt FROM plugin_refusal WHERE agent = ? ORDER BY id`, agent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PluginRefusal
	for rows.Next() {
		var r PluginRefusal
		var costJSON *string
		var ck int
		var rp int
		if err := rows.Scan(&r.ID, &r.Agent, &r.TaskID, &r.RevokedAt, &r.Reason, &costJSON, &ck, &r.RecordedAt, &rp); err != nil {
			return nil, err
		}
		if costJSON != nil {
			r.CostJSON = *costJSON
		}
		r.CostKnown = ck == 1
		r.ReachedPrompt = rp == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ErrPluginRecordExists distinguishes the one domain refusal this layer
// makes itself: a LIVE record already carries that name. (The database's
// partial unique index agrees; this error exists so the caller need not
// parse a constraint string to offer its remedy.)
var ErrPluginRecordExists = errors.New("a live plugin record already carries this name")

// PluginAgent is one durable agent record — the "here, once here, or never
// again" answer that survives restarts (AP4: revoked rows persist; the
// partial unique index keeps at most one live row per name).
type PluginAgent struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Source     string `json:"source"`
	State      string `json:"state"`
	ImportedAt string `json:"importedAt"`
}

// PluginTransition is one recorded move — who, from what, to what, why,
// when.
type PluginTransition struct {
	ID        int64  `json:"id"`
	Agent     string `json:"agent"`
	FromState string `json:"fromState"`
	ToState   string `json:"toState"`
	Reason    string `json:"reason"`
	At        string `json:"at"`
}

// CreatePluginAgent writes the new record (import's durable half). A live
// record with the name already — revoked rows never collide, which is what
// makes "a re-import after revoke is a NEW record" the database's own
// behaviour rather than a convention.
func (s *Store) CreatePluginAgent(name, source, importedAt string) error {
	name = strings.TrimSpace(name)
	if name == "" || source == "" || importedAt == "" {
		return fmt.Errorf("refused: plugin record needs name, source and imported_at")
	}
	var live int
	if err := s.r.QueryRow(`SELECT count(*) FROM plugin_agent WHERE name = ? AND state <> 'revoked'`, name).Scan(&live); err != nil {
		return err
	}
	if live > 0 {
		return ErrPluginRecordExists
	}
	_, err := s.w.Exec(`INSERT INTO plugin_agent (name, source, state, imported_at) VALUES (?, ?, 'disabled', ?)`, name, source, importedAt)
	return err
}

// TransitionPluginAgent moves a record and records the move in ONE
// transaction: a state changed without its transition row (or a transition
// row for a state that never moved) is exactly the half-state a restart
// cannot reason about. The UPDATE is guarded on the caller's claimed FROM —
// zero rows affected means somebody moved the agent under us, which the
// caller reports rather than papers over. The database still gets the last
// word: revoked-as-FROM and reasonless-revoke are CHECK violations even for
// this function.
func (s *Store) TransitionPluginAgent(name, fromState, toState, reason, at string) error {
	if fromState != "" && !inList(PluginAgentStates, fromState) {
		return fmt.Errorf("refused: plugin state %q is not in %v", fromState, PluginAgentStates)
	}
	if !inList(PluginAgentStates, toState) {
		return fmt.Errorf("refused: plugin state %q is not in %v", toState, PluginAgentStates)
	}
	if toState == "revoked" && strings.TrimSpace(reason) == "" {
		return fmt.Errorf("refused: revoke without a reason — the reason column is the one future readers ask about")
	}
	tx, err := s.w.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if fromState == "" {
		// The import landing — (none) → disabled — has no UPDATE to guard:
		// the record was just created in that state by CreatePluginAgent,
		// and the row is written to PROVE it. Anything else arriving here
		// (an import landing onto a moved record) is refused on the read.
		var st string
		if err := tx.QueryRow(`SELECT state FROM plugin_agent WHERE name = ? AND state <> 'revoked' ORDER BY id DESC LIMIT 1`, name).Scan(&st); err != nil {
			return fmt.Errorf("refused: %s has no live record to land a transition on: %w", name, err)
		}
		if st != "disabled" {
			return fmt.Errorf("refused: %s already reads %q — (none) → disabled lands once, at import", name, st)
		}
		if _, err := tx.Exec(`INSERT INTO plugin_transition (agent, from_state, to_state, reason, at) VALUES (?, '', 'disabled', '', ?)`, name, at); err != nil {
			return err
		}
		return tx.Commit()
	}
	res, err := tx.Exec(`UPDATE plugin_agent SET state = ? WHERE name = ? AND state = ?`, toState, name, fromState)
	if err != nil {
		// A CHECK violation surfaces here: terminal revoked, bad vocabulary.
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("refused: %s is not in state %q anymore — somebody moved it under us (re-read and decide again)", name, fromState)
	}
	if _, err := tx.Exec(`INSERT INTO plugin_transition (agent, from_state, to_state, reason, at) VALUES (?, ?, ?, ?, ?)`,
		name, fromState, toState, reason, at); err != nil {
		return err
	}
	return tx.Commit()
}

// PluginAgentState answers who is here: the live record's state, or the most
// recent revoked record's, or nothing (not-installed is the ABSENCE of a
// row — the two empties this returns are "never here", and revoked is a
// third, loud answer).
func (s *Store) PluginAgentState(name string) (PluginAgent, bool, error) {
	var a PluginAgent
	err := s.r.QueryRow(`SELECT id, name, source, state, imported_at FROM plugin_agent WHERE name = ? ORDER BY CASE WHEN state <> 'revoked' THEN 0 ELSE 1 END, id DESC LIMIT 1`, name).
		Scan(&a.ID, &a.Name, &a.Source, &a.State, &a.ImportedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PluginAgent{}, false, nil
	}
	if err != nil {
		return PluginAgent{}, false, err
	}
	return a, true, nil
}

// PluginAgentTransitions reads an agent's whole life, oldest first — "what
// did we once run, and why did we stop" is answered FROM THE ROWS, which is
// the whole reason revoked is not not-installed.
func (s *Store) PluginAgentTransitions(name string) ([]PluginTransition, error) {
	rows, err := s.r.Query(`SELECT id, agent, from_state, to_state, reason, at FROM plugin_transition WHERE agent = ? ORDER BY id`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PluginTransition
	for rows.Next() {
		var t PluginTransition
		if err := rows.Scan(&t.ID, &t.Agent, &t.FromState, &t.ToState, &t.Reason, &t.At); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PluginAgentsAll lists the registry's OWN records for `aeon agent list`
// and the `agent.plugins` read. Revoked rows are EXCLUDED unless asked —
// not hidden out of embarrassment but because the listing's default is
// "what is here now", and `--all` is the loud door to what was here
// (revoked is a life that ended, AP4's record that survives).
func (s *Store) PluginAgentsAll(includeRevoked bool) ([]PluginAgent, error) {
	q := `SELECT id, name, source, state, imported_at FROM plugin_agent`
	if !includeRevoked {
		q += ` WHERE state <> 'revoked'`
	}
	q += ` ORDER BY name, id DESC`
	rows, err := s.r.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PluginAgent
	seen := map[string]bool{}
	for rows.Next() {
		var a PluginAgent
		if err := rows.Scan(&a.ID, &a.Name, &a.Source, &a.State, &a.ImportedAt); err != nil {
			return nil, err
		}
		// A revoked-then-re-imported name has two records; the listing shows
		// the LIVE one unless revoked itself was asked for, where both lives
		// are the point — keep every row only in that mode.
		if !includeRevoked && seen[a.Name] {
			continue
		}
		seen[a.Name] = true
		out = append(out, a)
	}
	return out, rows.Err()
}

func inList(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

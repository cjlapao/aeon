package store

// attention.go — the store half of S58's attention inbox (T1). The inbox
// itself is a READ over other tables (T2 computes items from live state);
// what persists here is only what a person did about them: the
// append-only `attention_ack` row. The latest row per subject_key is the
// truth — ack marks seen, snooze marks seen-until, unack is an ack that
// happened by retracting an earlier one (SV3). Nothing rewrites or
// deletes a marker: the triggers refuse, and this file has no accessor
// that would even try (the grep test locks that).

import (
	"errors"
	"fmt"
	"strings"
)

// AttentionMark is one subject's current visibility, read from its
// latest attention_ack row. Op is ack | snooze | unack; Until is set only
// for a snooze (the table's CHECK couples them). A subject with no rows
// is simply absent from LatestAcks — unseen is the absence, not a row.
type AttentionMark struct {
	SubjectKey string
	Op         string
	Until      string
	OccurredAt string
	ActorKind  string
	ActorID    string
}

// AppendAttentionAck writes one visibility marker. The op vocabulary and
// the until/op coupling are the CHECK's; they are pre-checked here only
// so the door hears a sentence and not SQLite's trigger text. Actor is
// the peer identity stamped by the daemon (never a request field), the
// same discipline every human-door audit carries.
func (s *Store) AppendAttentionAck(subjectKey, op, until, actorKind, actorID string) (int64, error) {
	if subjectKey == "" {
		return 0, fmt.Errorf("store: attention ack: %w", errors.New("empty subject key"))
	}
	switch op {
	case "ack", "snooze", "unack":
	default:
		return 0, fmt.Errorf("store: attention ack: unknown op %q (want ack, snooze or unack)", op)
	}
	if (op == "snooze") != (until != "") {
		return 0, fmt.Errorf("store: attention ack: until belongs to snooze alone (op %q, until %q)", op, until)
	}
	if actorKind == "" || actorID == "" {
		return 0, fmt.Errorf("store: attention ack: %w", errors.New("actor is required"))
	}
	tx, err := s.w.Begin()
	if err != nil {
		return 0, fmt.Errorf("store: attention ack: %w", err)
	}
	defer tx.Rollback()
	now := s.Now()
	var untilArg any
	if until != "" {
		untilArg = until
	}
	res, err := tx.Exec(`INSERT INTO attention_ack (subject_key, op, until, actor_kind, actor_id, occurred_at)
		VALUES (?,?,?,?,?,?)`, subjectKey, op, untilArg, actorKind, actorID, now)
	if err != nil {
		return 0, fmt.Errorf("store: attention ack %s %s: %w", subjectKey, op, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: attention ack %s %s: %w", subjectKey, op, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: attention ack: %w", err)
	}
	return id, nil
}

// LatestAcks reads the current mark for each named subject in one query:
// the highest-id row per key wins (append-only means id IS the clock).
// An empty key set is an empty answer, not a full scan.
func (s *Store) LatestAcks(subjectKeys []string) (map[string]AttentionMark, error) {
	out := make(map[string]AttentionMark, len(subjectKeys))
	if len(subjectKeys) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(subjectKeys)), ",")
	args := make([]any, 0, len(subjectKeys))
	for _, k := range subjectKeys {
		args = append(args, k)
	}
	rows, err := s.r.Query(`SELECT a.subject_key, a.op, COALESCE(a.until,''), a.occurred_at, a.actor_kind, a.actor_id
		FROM attention_ack a
		JOIN (SELECT subject_key, MAX(id) AS mid FROM attention_ack
		      WHERE subject_key IN (`+placeholders+`) GROUP BY subject_key) m ON a.id = m.mid`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("store: latest attention acks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m AttentionMark
		if err := rows.Scan(&m.SubjectKey, &m.Op, &m.Until, &m.OccurredAt, &m.ActorKind, &m.ActorID); err != nil {
			return nil, fmt.Errorf("store: latest attention acks: %w", err)
		}
		out[m.SubjectKey] = m
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: latest attention acks: %w", err)
	}
	return out, nil
}

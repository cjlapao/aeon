package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// A goal OWNS the documents its work produced. The loop that wrote a document
// keeps its own rows (a loop's history is its own), and the document carries
// the goal's id beside them, so "what does this goal say" is one query and not
// a walk over loops.
//
// Two moments set the owner, and nothing else does:
//
//   - A loop that WORKS a goal (loop.goal_id) writes: every revision it writes
//     is the goal's from the first byte.
//   - A loop FILES a goal (goal_create): everything the loop has written so far
//     is adopted, and the loop keeps the link — loop.goal_id stays empty, the
//     goal's source_loop names the loop, and history reads both ways.
//
// A document already owned stays with its first owner. A brainstorm that files
// two goals gives its documents to the first and the second pins them through
// an attachment, which is what an attachment is for.

// documentOwnerInTx names the owner a new revision of file starts with: the
// owner of the previous revision when there is one, else the goal the loop
// works. "" means nobody owns it yet.
func documentOwnerInTx(tx *sql.Tx, sc Scope, file string) (string, error) {
	var owner sql.NullString
	err := tx.QueryRow(`SELECT owner_goal_id FROM loop_document
		WHERE component_root = ? AND loop_slug = ? AND file = ?
		ORDER BY rev DESC LIMIT 1`, sc.ComponentRoot, sc.LoopSlug, file).Scan(&owner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("store: read the owner of %s in %s: %w", file, sc, err)
	}
	if owner.Valid && owner.String != "" {
		return owner.String, nil
	}
	var goal sql.NullString
	err = tx.QueryRow(`SELECT goal_id FROM loop WHERE component_root = ? AND slug = ?`,
		sc.ComponentRoot, sc.LoopSlug).Scan(&goal)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("store: read the goal of %s: %w", sc, err)
	}
	return goal.String, nil
}

// AdoptDocuments gives a goal every document the loop has written that no goal
// owns yet, and answers how many revisions moved.
func (s *Store) AdoptDocuments(sc Scope, goalID string) (int, error) {
	if !sc.Valid() || goalID == "" {
		return 0, fmt.Errorf("store: AdoptDocuments needs a loop scope and a goal, got %s and %q", sc, goalID)
	}
	res, err := s.w.Exec(`UPDATE loop_document SET owner_goal_id = ?
		WHERE component_root = ? AND loop_slug = ? AND owner_goal_id IS NULL`,
		goalID, sc.ComponentRoot, sc.LoopSlug)
	if err != nil {
		return 0, fmt.Errorf("store: adopt the documents of %s into goal %s: %w", sc, goalID, err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// LoopGoal answers the goal a loop works ("" when it works none).
func (s *Store) LoopGoal(sc Scope) (string, error) {
	var goal sql.NullString
	err := s.r.QueryRow(`SELECT goal_id FROM loop WHERE component_root = ? AND slug = ?`,
		sc.ComponentRoot, sc.LoopSlug).Scan(&goal)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: read the goal of %s: %w", sc, err)
	}
	return goal.String, nil
}

// GoalDocuments lists the head of each document the goal owns, across every
// loop that wrote one, without content. When two loops wrote the same name the
// newer write is the head.
func (s *Store) GoalDocuments(goalID string) ([]Document, error) {
	rows, err := s.r.Query(`
		SELECT d.id, d.component_root, d.loop_slug, d.file, d.rev, d.bytes, d.sha256,
		       COALESCE(d.written_by,''), d.written_at, d.owner_goal_id
		FROM loop_document d
		WHERE d.owner_goal_id = ?
		  AND NOT EXISTS (
		    SELECT 1 FROM loop_document n
		    WHERE n.owner_goal_id = d.owner_goal_id AND n.file = d.file
		      AND (n.written_at > d.written_at OR (n.written_at = d.written_at AND n.id > d.id)))
		ORDER BY d.file`, goalID)
	if err != nil {
		return nil, fmt.Errorf("store: list the documents of goal %s: %w", goalID, err)
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.ComponentRoot, &d.LoopSlug, &d.File, &d.Rev,
			&d.Bytes, &d.SHA256, &d.WrittenBy, &d.WrittenAt, &d.OwnerGoal); err != nil {
			return nil, fmt.Errorf("store: list the documents of goal %s: %w", goalID, err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ReadGoalDocument reads the head of one document the goal owns. The name goes
// through the same resolver as every other document door.
func (s *Store) ReadGoalDocument(goalID, rawName string) (Document, error) {
	resolved := ResolveDocumentName(rawName)
	if !resolved.OK() {
		return Document{}, fmt.Errorf("store: %s", resolved.Reason)
	}
	var d Document
	var compressed []byte
	err := s.r.QueryRow(`
		SELECT id, component_root, loop_slug, file, rev, content, bytes, sha256, COALESCE(written_by,''), written_at, owner_goal_id
		FROM loop_document
		WHERE owner_goal_id = ? AND file = ?
		ORDER BY written_at DESC, id DESC LIMIT 1`, goalID, resolved.Name).
		Scan(&d.ID, &d.ComponentRoot, &d.LoopSlug, &d.File, &d.Rev, &compressed,
			&d.Bytes, &d.SHA256, &d.WrittenBy, &d.WrittenAt, &d.OwnerGoal)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, fmt.Errorf("store: goal %s owns no %s: %w", goalID, resolved.Name, ErrNotFound)
	}
	if err != nil {
		return Document{}, fmt.Errorf("store: read %s of goal %s: %w", resolved.Name, goalID, err)
	}
	plain, err := ungzipBytes(compressed)
	if err != nil {
		return Document{}, fmt.Errorf("store: decompress %s of goal %s: %w", resolved.Name, goalID, err)
	}
	sum := sha256.Sum256(plain)
	if hex.EncodeToString(sum[:]) != d.SHA256 {
		return Document{}, fmt.Errorf("store: %s of goal %s does not match its stored sha256; the row is corrupt", resolved.Name, goalID)
	}
	d.Content = string(plain)
	return d, nil
}

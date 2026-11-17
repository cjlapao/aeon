package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// LoopPinnedContent answers the blueprint text a loop was started on — the
// immutable pin, so a caller may cache what it derives from it for the loop's
// whole life. A loop with no pin reads as ErrNotFound.
func (s *Store) LoopPinnedContent(sc Scope) (string, error) {
	var content string
	err := s.r.QueryRow(`SELECT lb.content FROM loop_blueprint lb
		JOIN loop l ON l.id = lb.loop_id
		WHERE l.component_root = ? AND l.slug = ?`, sc.ComponentRoot, sc.LoopSlug).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("store: loop %s has no pin (or no loop): %w", sc, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("store: pinned blueprint of %s: %w", sc, err)
	}
	return content, nil
}

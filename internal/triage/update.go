package triage

// ════════════════════════════════════════════════════════════════════════════
// The edit — TM1 in one method: title and body, nothing else.
//
// `triage.update` changes ONLY the two fields a person may fix after
// filing: the title and the body. Validation state, priority, duplicate_of,
// roadmap_item_id, filed_by and every timestamp but updated_at are NOT
// editable through this door — the store statement cannot even name those
// columns (bug.go's updateBugFieldsStmt), so the boundary is the SQL, not
// a convention. Editing a bug that was already validated is legal (a typo
// fix); validation NEVER re-opens (D3) — the update audit says which
// FIELDS were edited, never touching the state.
//
// The rules (TM1): title 1..200 characters, body ≤ 16 KiB, and the body
// carries no control characters except newline and tab. A refusal names
// the rule; an unchanged edit is NOT a refusal — it answers changed:false
// and earns NO audit row (an audit of nothing is the noise TM2 refuses).
// ════════════════════════════════════════════════════════════════════════════

import (
	"fmt"
	"unicode/utf8"
)

// The TM1 bounds, named so the rule and the test read the same numbers.
const (
	MaxTitleRunes = 200
	MaxBodyBytes  = 16 * 1024
)

// UpdateBug is the caller's edit: a nil field means "leave it alone"; at
// least one must be set (the handler enforces it on the wire, the door
// again before the store).
type UpdateBug struct {
	Title *string
	Body  *string
}

// Update edits a bug's title and/or body as the audited actor (S51 T2,
// TM1/TM2). It returns the bug AS IT NOW READS and whether anything
// changed — a no-op edit answers changed:false with no audit row, and the
// store refuses the writer outright when zero fields are set, so an
// 'update' row naming no field is impossible.
func (t *Triage) Update(id int64, in UpdateBug, a Actor) (Bug, bool, *Refusal) {
	if in.Title == nil && in.Body == nil {
		return Bug{}, false, &Refusal{Reason: "an update names at least one field — title or body (TM1); there is nothing else to edit"}
	}
	// The rules run on the CANDIDATE VALUES before anything is read or
	// written: a refused edit touches no row and earns no audit.
	if in.Title != nil {
		if *in.Title == "" {
			return Bug{}, false, &Refusal{Reason: "a bug has a title — an empty title is a refusal, not a rename"}
		}
		if n := utf8.RuneCountInString(*in.Title); n > MaxTitleRunes {
			return Bug{}, false, &Refusal{Reason: fmt.Sprintf("a title is at most %d characters, not %d (TM1) — the list line is the title's home", MaxTitleRunes, n)}
		}
	}
	if in.Body != nil {
		if *in.Body == "" {
			return Bug{}, false, &Refusal{Reason: "a bug has a body — the report itself is the body; clearing it is a refusal, not an edit"}
		}
		if n := len(*in.Body); n > MaxBodyBytes {
			return Bug{}, false, &Refusal{Reason: fmt.Sprintf("a body is at most %d bytes (16 KiB, TM1), not %d — a longer report is a document a bug links, not a bug", MaxBodyBytes, n)}
		}
		if bad, ok := firstBadBodyByte(*in.Body); !ok {
			return Bug{}, false, &Refusal{Reason: fmt.Sprintf("a body carries no control characters except newline and tab (TM1) — byte 0x%02x is one", bad)}
		}
	}
	b, ok, err := t.Show(id)
	if err != nil {
		return Bug{}, false, &Refusal{Reason: fmt.Sprintf("triage: the store refused the read: %v", err)}
	}
	if !ok {
		return Bug{}, false, &Refusal{Reason: unknownBugReason(id)}
	}
	// The unchanged answer BEFORE the write: same words ⇒ changed:false,
	// no transaction, no audit row — read-modify-write decided by the
	// caller's own reading of the row.
	nt, nb := b.Title, b.Body
	if in.Title != nil {
		nt = *in.Title
	}
	if in.Body != nil {
		nb = *in.Body
	}
	if nt == b.Title && nb == b.Body {
		return b, false, nil
	}
	wrote, err := t.s.UpdateBugFieldsAudited(id, in.Title, in.Body, auditOf(a, ""))
	if err != nil {
		return Bug{}, false, &Refusal{Reason: fmt.Sprintf("triage: the store refused the update: %v", err)}
	}
	if !wrote {
		// The row vanished between the read and the write — the honest
		// answer is the same refusal a fresh lookup gives, and the audit
		// row rolled back with the update (one transaction).
		return Bug{}, false, &Refusal{Reason: unknownBugReason(id)}
	}
	fresh, ok, err := t.Show(id)
	if err != nil {
		return Bug{}, false, &Refusal{Reason: fmt.Sprintf("triage: the store refused the re-read: %v", err)}
	}
	if !ok {
		return Bug{}, false, &Refusal{Reason: unknownBugReason(id)}
	}
	return fresh, true, nil
}

// unknownBugReason is the ONE sentence for an id with no row, shared by
// every triage read and write (the wire maps it to ErrTriageUnknown where
// a READ answers, and through the door's Refusal where a WRITE does).
func unknownBugReason(id int64) string {
	return fmt.Sprintf("triage: bug %d does not exist", id)
}

// firstBadBodyByte reports the first control character that is neither
// '\n' nor '\t' — the TM1 body rule, stated as a scan so the refusal can
// name the byte it saw.
func firstBadBodyByte(s string) (byte, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 && c != '\n' && c != '\t' {
			return c, false
		}
		if c == 0x7f {
			return c, false
		}
	}
	return 0, true
}

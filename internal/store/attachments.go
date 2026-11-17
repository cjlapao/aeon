package store

// attachments.go — a record pointing at a frozen document revision (OL4).
// An attachment is a REFERENCE, never a copy: the bytes stay in
// loop_document (the store is the source of truth, and a copy is a second
// truth waiting to disagree). Because documents are revisioned, an
// attachment carries a CURTENCY the reader must see — current, superseded,
// or missing — and the third state in particular may never be flattened away
// (P31): "the document it pinned is gone" is a fact about the world, not a
// rendering failure.

import (
	"database/sql"
	"errors"
	"fmt"
)

// Attachment is one pinned reference.
type Attachment struct {
	ID            int64  `json:"id"`
	SubjectKind   string `json:"subjectKind"`
	SubjectID     string `json:"subjectId"`
	ComponentRoot string `json:"componentRoot"`
	LoopSlug      string `json:"loopSlug"`
	Document      string `json:"document"`
	Rev           int    `json:"rev"`
	StepID        *int64 `json:"stepId,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

// AttachmentView adds the currency verdict a surface renders.
type AttachmentView struct {
	Attachment
	Currency string `json:"currency"` // current | superseded | missing
	HeadRev  int    `json:"headRev"`  // 0 when the document is gone entirely
}

// Currency states, as constants so no surface invents a fourth reading.
const (
	CurrencyCurrent    = "current"
	CurrencySuperseded = "superseded"
	CurrencyMissing    = "missing"
)

func validSubjectKind(kind string) bool {
	switch kind {
	case "goal", "brief", "task", "epic":
		return true
	}
	return false
}

// Attach pins the CURRENT revision of a loop document to a record. The pin
// reads through ReadDocument — the resolution rule (friendly name or confined
// path) and the refusal for an unknown document are the documents file's,
// never re-implemented here: one door for what a document name means.
//
// The version pinned is the one that existed at the moment of attaching; a
// later revision does not silently move the pin — that is what superseded
// means, and it is the reader's right to see it.
func (s *Store) Attach(sc Scope, subjectKind, subjectID, document string, stepID *int64) (Attachment, *Refusal, error) {
	if !sc.Valid() {
		return Attachment{}, nil, fmt.Errorf("store: Attach needs both a slug and a component root, got %s", sc)
	}
	if !validSubjectKind(subjectKind) {
		return Attachment{}, &Refusal{Kind: "subject-kind-unknown",
			Sentence: fmt.Sprintf("refused: subject kind %q is not one of goal, brief, task, epic", subjectKind)}, nil
	}
	if subjectID == "" {
		return Attachment{}, &Refusal{Kind: "subject-empty",
			Sentence: "refused: an attachment names the record it attaches to"}, nil
	}
	doc, err := s.ReadDocument(sc, document)
	if err != nil {
		// ReadDocument refuses unknown names/errors — an unknown document is
		// the refusal the task demands, told as one.
		return Attachment{}, &Refusal{Kind: "document-unknown",
			Sentence: fmt.Sprintf("refused: no document %q for loop %s — an attachment pins what exists, never a hope", document, sc)}, nil
	}
	now := s.Now()
	a := Attachment{SubjectKind: subjectKind, SubjectID: subjectID, ComponentRoot: sc.ComponentRoot,
		LoopSlug: sc.LoopSlug, Document: doc.File, Rev: doc.Rev, StepID: stepID, CreatedAt: now}
	res, err := s.w.Exec(`INSERT INTO attachment (subject_kind,subject_id,loop_slug,component_root,document,rev,step_id,created_at)
		VALUES (?,?,?,?,?,?,?,?)`, a.SubjectKind, a.SubjectID, a.LoopSlug, a.ComponentRoot, a.Document, a.Rev, a.StepID, now)
	if err != nil {
		if isUniqueViolation(err) {
			return Attachment{}, &Refusal{Kind: "already-attached",
				Sentence: fmt.Sprintf("refused: %s %s already attaches %s rev %d — the pair (subject, document, rev) is the point of the row",
					a.SubjectKind, a.SubjectID, a.Document, a.Rev)}, nil
		}
		return Attachment{}, nil, fmt.Errorf("store: attach %s rev %d to %s %s: %w", a.Document, a.Rev, a.SubjectKind, a.SubjectID, err)
	}
	if id, err := res.LastInsertId(); err == nil {
		a.ID = id
	}
	return a, nil, nil
}

// ListAttachments returns a subject's attachments with their currency read
// fresh — currency is COMPUTED at read (OL11: derived state is never stored).
func (s *Store) ListAttachments(subjectKind, subjectID string) ([]AttachmentView, error) {
	rows, err := s.r.Query(`SELECT id, subject_kind, subject_id, component_root, loop_slug, document, rev, step_id, created_at
		FROM attachment WHERE subject_kind = ? AND subject_id = ? ORDER BY created_at, id`, subjectKind, subjectID)
	if err != nil {
		return nil, fmt.Errorf("store: list attachments: %w", err)
	}
	var ats []Attachment
	for rows.Next() {
		var a Attachment
		var step sql.NullInt64
		if err := rows.Scan(&a.ID, &a.SubjectKind, &a.SubjectID, &a.ComponentRoot, &a.LoopSlug,
			&a.Document, &a.Rev, &step, &a.CreatedAt); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("store: scan attachment: %w", err)
		}
		if step.Valid {
			v := step.Int64
			a.StepID = &v
		}
		ats = append(ats, a)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("store: list attachments: %w", err)
	}
	// DRAINED before the head reads: the writer pool is one connection, and a
	// query issued while this result set is still open waits forever for it.
	_ = rows.Close()
	var out []AttachmentView
	for _, a := range ats {
		v := AttachmentView{Attachment: a}
		var head sql.NullInt64
		if err := s.r.QueryRow(`SELECT MAX(rev) FROM loop_document WHERE component_root = ? AND loop_slug = ? AND file = ?`,
			a.ComponentRoot, a.LoopSlug, a.Document).Scan(&head); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: head rev of %s: %w", a.Document, err)
		}
		switch {
		case !head.Valid:
			// No revisions at all: the document (or its loop) is archived —
			// the bytes live on only in the archive blob, and this row says
			// plainly that what it pinned is no longer where it said it was.
			v.Currency, v.HeadRev = CurrencyMissing, 0
		case int(head.Int64) == a.Rev:
			v.Currency, v.HeadRev = CurrencyCurrent, int(head.Int64)
		default:
			v.Currency, v.HeadRev = CurrencySuperseded, int(head.Int64)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// charter.go — S61 T1: the charter as a row-history (GI1). There is no
// mutable charter row: the CURRENT charter of a root is its max(revision),
// so a second truth cannot exist. Every row is written once — the
// append-only triggers in the migration are the schema's word, and this file
// is the store's: a refusal sentence before SQL wherever the store can see
// the problem coming (an unknown root, a child, an actor without a name).
//
// The body is canonical JSON the daemon has already validated against GI2's
// closed schema (internal/charter, T2); the store enforces only what a row
// can enforce alone — the size cap, a named actor, a known source word — and
// the summary is a fixed template the caller composed (SEC-033): the store
// never builds sentences from user text here.
package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// allCharterSources is the closed provenance set of GI1: a charter row came
// from a person at the door, from a committed proposal (the committer is the
// actor, the proposal id is source_ref), or from an intake commit.
var allCharterSources = map[string]bool{
	"person": true, "proposal": true, "intake": true,
}

// maxCharterBodyBytes is GI2's cap on the canonical JSON body.
const maxCharterBodyBytes = 64 * 1024

// CharterRevision is one row of the charter history.
type CharterRevision struct {
	ID        string `json:"id"`
	RootID    string `json:"rootId"`
	Revision  int    `json:"revision"`
	Body      string `json:"body"`
	Summary   string `json:"summary"`
	ActorKind string `json:"actorKind"`
	ActorID   string `json:"actorId"`
	Source    string `json:"source"`
	SourceRef string `json:"sourceRef"`
	CreatedAt string `json:"createdAt"`
}

// NewCharterRevision is the input to AppendCharterRevision.
type NewCharterRevision struct {
	RootID    string
	Body      string
	Summary   string
	By        Actor
	Source    string // person | proposal | intake
	SourceRef string // proposal id / intake id when Source names one
}

const charterColumns = `id, root_id, revision, body, summary, actor_kind, actor_id,
 	source, source_ref, created_at`

func scanCharterRevision(row interface{ Scan(...any) error }) (CharterRevision, error) {
	var r CharterRevision
	err := row.Scan(&r.ID, &r.RootID, &r.Revision, &r.Body, &r.Summary, &r.ActorKind,
		&r.ActorID, &r.Source, &r.SourceRef, &r.CreatedAt)
	return r, err
}

// AppendCharterRevision files the next revision of a root's charter. The
// revision number is max+1 computed INSIDE the transaction, so two appends
// cannot win the same number — the UNIQUE(root_id, revision) key then makes a
// lost update impossible rather than unlikely. A child component is refused
// with its project's name (GI3): a tree has ONE charter, on the root.
func (s *Store) AppendCharterRevision(in NewCharterRevision) (CharterRevision, *Refusal, error) {
	if !in.By.validActor() {
		return CharterRevision{}, &Refusal{Kind: "actor-unknown",
			Sentence: fmt.Sprintf("refused: author kind %q is not user, agent, or trigger (or the author has no id) — every charter revision names who wrote it", in.By.Kind)}, nil
	}
	if !allCharterSources[in.Source] {
		return CharterRevision{}, &Refusal{Kind: "source-unknown",
			Sentence: fmt.Sprintf("refused: charter source %q is not one of person, proposal, intake", in.Source)}, nil
	}
	if in.Body == "" || len(in.Body) > maxCharterBodyBytes {
		return CharterRevision{}, &Refusal{Kind: "body-size",
			Sentence: fmt.Sprintf("refused: a charter body must be present and no larger than %d KiB (GI2) — this one is %d bytes", maxCharterBodyBytes/1024, len(in.Body))}, nil
	}
	if in.Summary == "" {
		return CharterRevision{}, &Refusal{Kind: "summary-empty",
			Sentence: "refused: a charter revision carries its fixed-template summary — never a blank and never a user sentence"}, nil
	}

	tx, err := s.w.Begin()
	if err != nil {
		return CharterRevision{}, nil, fmt.Errorf("store: append charter: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// GI3: the charter lives on the root. Name the project in the refusal —
	// "X is a component of project Y" is the sentence that closes the loop
	// for whoever asked.
	var pname sql.NullString
	var parentID sql.NullString
	err = tx.QueryRow(`SELECT c.parent_id, p.name FROM component c
		LEFT JOIN component p ON p.id = c.parent_id WHERE c.id = ?`, in.RootID).Scan(&parentID, &pname)
	if errors.Is(err, sql.ErrNoRows) {
		return CharterRevision{}, &Refusal{Kind: "component-unknown",
			Sentence: fmt.Sprintf("refused: component %q does not exist — a charter belongs to a project that exists", in.RootID)}, nil
	}
	if err != nil {
		return CharterRevision{}, nil, fmt.Errorf("store: check charter root %s: %w", in.RootID, err)
	}
	if parentID.Valid {
		return CharterRevision{}, &Refusal{Kind: "charter-on-child",
			Sentence: fmt.Sprintf("refused: %s is a component of project %s — the charter lives on the project", in.RootID, pname.String)}, nil
	}

	var next int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(revision),0)+1 FROM charter_revision WHERE root_id = ?`, in.RootID).
		Scan(&next); err != nil {
		return CharterRevision{}, nil, fmt.Errorf("store: next charter revision for %s: %w", in.RootID, err)
	}
	rev := CharterRevision{ID: "charter-" + newID(), RootID: in.RootID, Revision: next,
		Body: in.Body, Summary: in.Summary, ActorKind: in.By.Kind, ActorID: in.By.ID,
		Source: in.Source, SourceRef: in.SourceRef, CreatedAt: s.Now()}
	if _, err := tx.Exec(`INSERT INTO charter_revision
		(id,root_id,revision,body,summary,actor_kind,actor_id,source,source_ref,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, rev.ID, rev.RootID, rev.Revision, rev.Body, rev.Summary,
		rev.ActorKind, rev.ActorID, rev.Source, rev.SourceRef, rev.CreatedAt); err != nil {
		return CharterRevision{}, nil, fmt.Errorf("store: append charter revision for %s: %w", in.RootID, err)
	}
	if err := tx.Commit(); err != nil {
		return CharterRevision{}, nil, fmt.Errorf("store: append charter: %w", err)
	}
	return rev, nil, nil
}

// CurrentCharter reads max(revision) — THE charter, because there is nothing
// else to read. Absent is an error in GetGoal's house style; "this project
// has no charter yet" is a fact about the store, not a caller mistake.
func (s *Store) CurrentCharter(rootID string) (CharterRevision, error) {
	row := s.r.QueryRow(`SELECT `+charterColumns+` FROM charter_revision
		WHERE root_id = ? ORDER BY revision DESC LIMIT 1`, rootID)
	r, err := scanCharterRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CharterRevision{}, fmt.Errorf("store: component %q has no charter yet", rootID)
	}
	if err != nil {
		return CharterRevision{}, fmt.Errorf("store: read charter of %s: %w", rootID, err)
	}
	return r, nil
}

// CharterRevisions is the whole history, oldest first — history never resets
// and never hides an edit (GI1).
func (s *Store) CharterRevisions(rootID string) ([]CharterRevision, error) {
	rows, err := s.r.Query(`SELECT `+charterColumns+` FROM charter_revision
		WHERE root_id = ? ORDER BY revision ASC`, rootID)
	if err != nil {
		return nil, fmt.Errorf("store: list charter revisions: %w", err)
	}
	defer rows.Close()
	var out []CharterRevision
	for rows.Next() {
		r, err := scanCharterRevision(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan charter revision: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListChartersWithCurrent answers the board: one line per root that HAS a
// charter, at its current revision. A root without one is absent — the view
// reads that as no-charter, and never fabricates a row to fill the hole.
func (s *Store) ListChartersWithCurrent() ([]CharterRevision, error) {
	rows, err := s.r.Query(`SELECT ` + charterColumns + ` FROM charter_revision cr
		WHERE revision = (SELECT MAX(revision) FROM charter_revision WHERE root_id = cr.root_id)
		ORDER BY root_id`)
	if err != nil {
		return nil, fmt.Errorf("store: list charters: %w", err)
	}
	defer rows.Close()
	var out []CharterRevision
	for rows.Next() {
		r, err := scanCharterRevision(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan charter: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

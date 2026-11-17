package charter

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cjlapao/aeon/internal/store"
)

// Service is the charter's ONLY writer (Behaviour 1). The actor arrives as
// a parameter because the CALLER stamps it — the daemon from the socket
// peer, the proposal apply from the committer (GI4); the charter package
// never decides who is acting, it only refuses what the body cannot say.
type Service struct {
	Store *store.Store

	// Autonomy is the S56 seam (OD12); nil until S56 wires it, and until
	// then every proposed.autonomy is refused with the sentence that says
	// why. The seam is called per save — the vocabulary is never cached.
	Autonomy AutonomyVocab

	// mu covers the read-current → append pair. The daemon is one process
	// and the store's writer is one connection (DB15), so this mutex, not
	// a row lock, is the serialisation the no-op check and the summary's
	// revision number need. (The store re-derives max+1 inside the write
	// transaction regardless — the number in the summary is honest because
	// saves are serialised HERE, in the one process that writes charters.)
	mu sync.Mutex
}

// Save validates, canonicalizes and appends one revision — or answers the
// CURRENT revision when the body is byte-identical to it (Behaviour 2: an
// equal save is a no-op; history never churns on a re-send). The refusal
// for a child names its project (GI3), straight from the store.
//
// note is a PERSON's sentence and it rides the row's source_ref — the one
// column with no other meaning when source is 'person' (the summary is a
// fixed template, SEC-033; a proposal's reason rides the proposal, an
// intake's words ride the intake ledger, so those sources arrive with
// note empty by law).
func (s *Service) Save(ctx context.Context, actor store.Actor, rootID string, body Body, note, source, sourceRef string) (Revision, *Refusal, error) {
	if r := validateNote(note); r != nil {
		return Revision{}, r, nil
	}
	switch source {
	case "person":
		if note != "" {
			sourceRef = note
		}
	case "proposal", "intake":
		if note != "" {
			return Revision{}, refusal("note-source",
				"charter: a note is a person's own sentence — a %s save carries its reason where it lives (the proposal / the intake ledger), not here", source), nil
		}
	default:
		return Revision{}, refusal("source", "charter: source %q is not one of person, proposal, intake", source), nil
	}
	if r := Validate(body, Vocabulary(s.Autonomy)); r != nil {
		return Revision{}, r, nil
	}
	canonical, err := Canonical(body)
	if err != nil {
		return Revision{}, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.Store.CurrentCharter(rootID)
	next := 1
	if err == nil {
		if current.Body == string(canonical) {
			total, terr := s.countRevisions(rootID)
			if terr != nil {
				return Revision{}, nil, terr
			}
			view, verr := toView(current, total)
			return view, nil, verr
		}
		next = current.Revision + 1
	} else if !strings.Contains(err.Error(), "no charter yet") {
		return Revision{}, nil, err
	}

	row, ref, err := s.Store.AppendCharterRevision(store.NewCharterRevision{
		RootID:    rootID,
		Body:      string(canonical),
		Summary:   fmt.Sprintf("charter r%d by %s:%s (%s)", next, actor.Kind, actor.ID, source),
		By:        actor,
		Source:    source,
		SourceRef: sourceRef,
	})
	if err != nil || ref != nil {
		if ref != nil {
			// The store's refusal (component-unknown, charter-on-child,
			// source-unknown…) crosses the package boundary verbatim —
			// the sentence that names the project was written by T1.
			return Revision{}, &Refusal{Kind: ref.Kind, Sentence: ref.Sentence}, err
		}
		return Revision{}, nil, err
	}
	total, err := s.countRevisions(rootID)
	if err != nil {
		return Revision{}, nil, err
	}
	view, err := toView(row, total)
	return view, nil, err
}

// Get answers the CURRENT revision of one root — max(revision), the derived
// truth (GI1); a root without one is the store's sentence, not a zero row.
func (s *Service) Get(ctx context.Context, rootID string) (Revision, error) {
	row, err := s.Store.CurrentCharter(rootID)
	if err != nil {
		return Revision{}, err
	}
	total, err := s.countRevisions(rootID)
	if err != nil {
		return Revision{}, err
	}
	return toView(row, total)
}

// List answers every root's current charter, at its current number, with
// the same details Get gives (T2: "List (with details)"). Charter scale is
// one row per project; counting each root's history one query at a time is
// honest at that size and keeps the store surface at T1's four reads.
func (s *Service) List(ctx context.Context) ([]Revision, error) {
	rows, err := s.Store.ListChartersWithCurrent()
	if err != nil {
		return nil, err
	}
	out := make([]Revision, 0, len(rows))
	for _, row := range rows {
		total, err := s.countRevisions(row.RootID)
		if err != nil {
			return nil, err
		}
		view, err := toView(row, total)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

// Revisions walks one root's full history, oldest first.
func (s *Service) Revisions(ctx context.Context, rootID string) ([]Revision, error) {
	rows, err := s.Store.CharterRevisions(rootID)
	if err != nil {
		return nil, err
	}
	out := make([]Revision, 0, len(rows))
	for _, row := range rows {
		view, err := toView(row, len(rows))
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

func (s *Service) countRevisions(rootID string) (int, error) {
	rows, err := s.Store.CharterRevisions(rootID)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func validateNote(note string) *Refusal {
	if note == "" {
		return nil
	}
	if len(note) > MaxNote || strings.ContainsAny(note, "\x00\n") {
		return refusal("note", "charter: the note must be 1–%d bytes with no NUL and no newline", MaxNote)
	}
	return nil
}

// toView parses a stored body. A row that fails to parse is corruption,
// reported as such — the charter plane NEVER silently reads a broken row as
// an empty charter (that would be a second truth, GI1).
func toView(row store.CharterRevision, total int) (Revision, error) {
	body, err := Parse([]byte(row.Body))
	if err != nil {
		return Revision{}, fmt.Errorf("charter: revision %s holds a body that no longer parses (corruption, not emptiness): %w", row.ID, err)
	}
	return Revision{
		ID:             row.ID,
		RootID:         row.RootID,
		Revision:       row.Revision,
		Body:           body,
		Summary:        row.Summary,
		ActorKind:      row.ActorKind,
		ActorID:        row.ActorID,
		Source:         row.Source,
		SourceRef:      row.SourceRef,
		CreatedAt:      row.CreatedAt,
		TotalRevisions: total,
	}, nil
}

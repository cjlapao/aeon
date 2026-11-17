package triage

import (
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/store"
)

// T4 — duplicate detection, suggest-only (D5/N3).
//
// The whole point of this file is that a suggestion has NO side effect: the
// detector never writes duplicate_of, never touches validation_state, never
// closes anything. The one write is ConfirmDuplicate below, and it refuses
// any confirmedBy that is not a person. R3's named risk — "the first time
// someone wants to reduce noise, auto-closing looks tempting" — is made
// structurally unreachable here, not merely discouraged in prose.

// ConfirmDuplicate is the ONLY function in this package that writes
// duplicate_of, and it refuses a confirmedBy that does not name an actual
// person-originated call path (the same shape S24's checkAuthorship restricts
// a write to a named, legitimate source — applied here to confirm-duplicate
// rather than policy). An agent, an empty value, or anything that reads as
// a non-person is refused: a duplicate becomes real only because a person
// said so (D5/N3).
func (t *Triage) ConfirmDuplicate(id int64, componentRoot string, of int64, confirmedBy string) (Bug, *Refusal) {
	return t.confirmDuplicate(id, componentRoot, of, confirmedBy, nil)
}

// confirmDuplicate is the one confirm; an actor pairs the write with its
// 'duplicate' audit row in one transaction (S51 T1) AND meets the D5 gate
// twice over: the confirmedBy string keeps its human check (the wire kept
// its param, byte-identical), and the STAMPED actor must itself be a
// person-kind (user/cli) — a system or agent actor refuses here, the Go
// twin of the schema CHECK that lets an agent carry only `file`. Two
// checks, one law: a duplicate becomes real only because a person said so.
func (t *Triage) confirmDuplicate(id int64, componentRoot string, of int64, confirmedBy string, actor *Actor) (Bug, *Refusal) {
	if !isPersonOriginated(confirmedBy) {
		return Bug{}, &Refusal{Reason: fmt.Sprintf(
			"refused confirm-duplicate by %q — a duplicate becomes real only from a person (D5/N3); an agent or an empty confirmer never writes duplicate_of", confirmedBy)}
	}
	if actor != nil && actor.Kind != "user" && actor.Kind != "cli" {
		return Bug{}, &Refusal{Reason: fmt.Sprintf(
			"refused confirm-duplicate on bug %d with actor kind %q — the stamped author must be a person at a door (user or cli), not %s (D5/N3, TM6; the schema CHECK refuses an agent on any op but file all the same)", id, actor.Kind, actor.Kind)}
	}
	if _, ok, err := t.Show(id); err != nil {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: read bug %d: %v", id, err)}
	} else if !ok {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: bug %d does not exist", id)}
	}
	var claimed bool
	var err error
	if actor == nil {
		claimed, err = t.s.SetBugDuplicateOf(id, componentRoot, &of)
	} else {
		claimed, err = t.s.SetBugDuplicateOfAudited(id, componentRoot, &of,
			auditOf(*actor, summaryDuplicate(id, of)))
	}
	if err != nil {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: the store refused the confirm: %v", err)}
	}
	if !claimed {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: bug %d is not in %s — the confirm is scoped to the caller's root", id, componentRoot)}
	}
	fresh, ok, err := t.Show(id)
	if err != nil || !ok {
		return Bug{}, &Refusal{Reason: fmt.Sprintf("triage: the confirm landed but the row could not be re-read: %v", err)}
	}
	return fresh, nil
}

// ConfirmDuplicateAudited is the person's confirm with its author (S51 T1):
// the D5 gate above PLUS the stamped actor's own person-kind law, and the
// 'duplicate' audit row in the same transaction.
func (t *Triage) ConfirmDuplicateAudited(id int64, componentRoot string, of int64, confirmedBy string, a Actor) (Bug, *Refusal) {
	return t.confirmDuplicate(id, componentRoot, of, confirmedBy, &a)
}

// isPersonOriginated answers "is this confirmer a person, not a machine".
// A duplicate confirmation is a human act (D5), the same kind of gate S24's
// policy-authorship check draws for an allow. An empty value and anything
// that names a machine (agent, model, loop, tool — or an "agent:<name>"
// form) are refused; a person, operator or a uid are admitted.
func isPersonOriginated(confirmedBy string) bool {
	if confirmedBy == "" {
		return false
	}
	if confirmedBy == "agent" || strings.HasPrefix(confirmedBy, "agent:") {
		return false
	}
	for _, machine := range []string{"model", "loop", "tool"} {
		if confirmedBy == machine {
			return false
		}
	}
	return true
}

// SuggestCandidates is the read: nearest-neighbour matches for a bug within
// its OWN project (D6/N2 — the bug_vec table is per-project, so a query of
// one project's bugs can never read another's). It has NO side effect — it
// does not write duplicate_of, does not change validation_state, does not
// close anything (D5/N3). A missing vec table (nothing embedded yet) is an
// honest empty list, not an error.
//
// The vector and dim are the bug's own embedding, computed and stored by
// the daemon (T3's dispatch): the triage door never reaches a model, only
// the store. This keeps the door a pure read and the embedding a daemon
// concern, so tests plant vectors directly.
func (t *Triage) SuggestCandidates(bug Bug, dim int, vector []float32, limit int) ([]Bug, error) {
	if dim <= 0 {
		return nil, fmt.Errorf("triage: refused a suggestion at %d dims — 0 means no embedding ran", dim)
	}
	hits, err := t.s.QueryBugVec(bug.ComponentRoot, dim, vector, limit)
	if err != nil {
		return nil, err
	}
	// The candidate must not be the bug itself, and the distance is the
	// ranking (smaller is nearer). Candidates come back in the store's
	// distance order; a self-match is dropped, not first.
	out := make([]Bug, 0, len(hits))
	for _, h := range hits {
		if h.BugID == bug.ID {
			continue
		}
		cand, ok, err := t.Show(h.BugID)
		if err != nil || !ok {
			continue // a vec row with no bug row is dropped, not an error
		}
		out = append(out, cand)
	}
	return out, nil
}

// StoreEmbedding computes and stores a bug's embedding in its project's
// bug_vec (T4's "on bug creation, compute and store an embedding"). It is
// the seam the daemon calls with a model-computed vector; a nil or
// wrong-length vector is refused (the table's name carries the dimension),
// never a silent skip that would leave the bug unindexed without saying so.
func (t *Triage) StoreEmbedding(bug Bug, dim int, vector []float32) error {
	if len(vector) != dim {
		return fmt.Errorf("triage: refused an embedding of %d dims for bug %d at %d — the table's name carries the dimension", len(vector), bug.ID, dim)
	}
	if _, err := t.s.EnsureBugVecTable(bug.ComponentRoot, dim); err != nil {
		return err
	}
	return t.s.UpsertBugVec(bug.ComponentRoot, dim, bug.ID, vector)
}

// ensure store is referenced (the door's handle side is the store's SQL).
var _ = store.BugVecHit{}

// ScoredCandidate is one suggestion WITH its distance — the score the
// board shows (TM5: "duplicate candidates with scores"). The existing
// SuggestCandidates drops the distance on the floor; the board cannot,
// because a candidate without its nearness asks the person to trust the
// ranking they cannot see. The vector hit's own distance, unrounded,
// smaller is nearer — the store's ranking words, not a restated scale.
type ScoredCandidate struct {
	Bug      Bug
	Distance float64
}

// SuggestScored reads the same k-NN as SuggestCandidates — the SAME
// store query, the SAME self-exclusion — and keeps the score. It is a
// pure read: it does NOT store an embedding (suggest stores because it
// computed one for the ask; a board read of a backlog must not write a
// row per row it displays — TestBoardCandidatesAreReadOnly pins this).
// A missing vec table is the honest empty list, never an error.
func (t *Triage) SuggestScored(bug Bug, dim int, vector []float32, limit int) ([]ScoredCandidate, error) {
	if dim <= 0 {
		return nil, fmt.Errorf("triage: refused a suggestion at %d dims — 0 means no embedding ran", dim)
	}
	hits, err := t.s.QueryBugVec(bug.ComponentRoot, dim, vector, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ScoredCandidate, 0, len(hits))
	for _, h := range hits {
		if h.BugID == bug.ID {
			continue // the bug is never its own candidate
		}
		cand, ok, err := t.Show(h.BugID)
		if err != nil || !ok {
			continue // a vec row with no bug row is dropped, not an error
		}
		out = append(out, ScoredCandidate{Bug: cand, Distance: h.Distance})
	}
	return out, nil
}

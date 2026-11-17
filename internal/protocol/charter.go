// charter.go — S61 T7 (GI8, P4, P34): the charter doors.
//
// Four reads and one human write over ONE seam, registered together or
// absent (V13). This file knows `internal/charter` by NOTHING — every
// input and result is a wire struct defined here, because the seam law
// every plane sets is what keeps the charter package out of the
// dispatch-critical imports (TestCharterGrantsNothing walks it).
//
// The shape the wire itself states:
//
//   - The charter is the CURRENT revision, and `charter.get` answers it
//     without paging; history is a separate door (`charter.revisions`),
//     because the person choosing between r3 and r4 reads one list.
//   - No identity in params (P4): save carries the connection's person,
//     and a params `actor`/`identity`/`peer` key is refused by name
//     before decode. The charter's audit row is what a forged actor
//     would corrupt, and it is exactly the field no connection can type.
//   - `proposed` rides the body verbatim on the get view (budgetUsd,
//     verify[], environmentRef) — the S56 hooks S56 reads; this plane
//     runs none of it (GI8: the charter grants NOTHING), and the
//     vocabulary door publishes the check kinds and caps from the
//     charter package's own sets, never a second spelling.
//
// `ErrCharterRefused` (-31964, S61's block) is the family's one refusal
// voice — the validator's or the store's own sentence in `what`.

package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// --- the method names -------------------------------------------------------

const (
	// MethodCharterGet answers one root's charter: the current revision,
	// the ONE description, and the proposed arm verbatim.
	MethodCharterGet MethodName = "charter.get"
	// MethodCharterList lists every chartered root with its current
	// revision, the description's first line, and whether a proposed arm
	// is present (a details list, never bare ids).
	MethodCharterList MethodName = "charter.list"
	// MethodCharterRevisions pages one root's history, newest first.
	MethodCharterRevisions MethodName = "charter.revisions"
	// MethodCharterVocabulary publishes the closed sets: check kinds, the
	// caps, and the autonomy vocabulary when the S56 seam answers it.
	MethodCharterVocabulary MethodName = "charter.vocabulary"
	// MethodCharterPropose is the AGENT's ask (S61 T8, GI17): it files an
	// inert charter_save proposal — never a revision. The save door stays
	// the person's (GI8); conditional on the records seam (charter_propose.go).
	MethodCharterPropose MethodName = "charter.propose"
	// MethodCharterSave appends a revision — a person's door, the actor
	// the connection's, never a param (P4/GI8).
	MethodCharterSave MethodName = "charter.save"
)

// --- the wire shapes ---------------------------------------------------------

// CharterBodyWire is the charter body as saved, byte-for-byte what the
// daemon read (the plane never re-renders a body on its way to the wire).
type CharterBodyWire json.RawMessage

// CharterProposedWire is body.proposed verbatim — the three S56 hooks.
// It answers presence and content, and NOTHING runs it here (GI8).
type CharterProposedWire struct {
	BudgetUsd      *float64 `json:"budgetUsd,omitempty"`
	Verify         []string `json:"verify,omitempty"`
	EnvironmentRef string   `json:"environmentRef,omitempty"`
}

// CharterDescription is the one formatter's answer (summary + fixed lines).
type CharterDescription struct {
	Summary string   `json:"summary"`
	Lines   []string `json:"lines"`
}

// CharterView is one root's charter at one revision.
type CharterView struct {
	RootID         string               `json:"rootId"`
	Revision       int                  `json:"revision"`
	Body           json.RawMessage      `json:"body"`
	Summary        string               `json:"summary"`
	ActorKind      string               `json:"actorKind"`
	ActorID        string               `json:"actorId"`
	Source         string               `json:"source"`
	SourceRef      string               `json:"sourceRef"`
	CreatedAt      string               `json:"createdAt"`
	Proposed       *CharterProposedWire `json:"proposed,omitempty"`
	Describe       *CharterDescription  `json:"description,omitempty"`
	TotalRevisions int                  `json:"totalRevisions"`
}

// CharterGetParams — `{rootId, revision?}`. A revision asks for that
// revision out of history; absent answers the current one.
type CharterGetParams struct {
	RootID   string `json:"rootId"`
	Revision *int   `json:"revision,omitempty"`
}

// CharterListEntry is one row of the details list.
type CharterListEntry struct {
	RootID      string `json:"rootId"`
	Name        string `json:"name"`
	Revision    int    `json:"revision"`
	Summary     string `json:"summary"`
	HasProposed bool   `json:"hasProposed"`
	UpdatedAt   string `json:"updatedAt"`
}

// CharterListResult is charter.list's whole answer.
type CharterListResult struct {
	Charters []CharterListEntry `json:"charters"`
}

// CharterRevisionsParams — `{rootId, limit?, cursor?}`; the cursor is the
// revision number to page BELOW (newest-first paging).
type CharterRevisionsParams struct {
	RootID string `json:"rootId"`
	Limit  *int   `json:"limit,omitempty"`
	Cursor *int   `json:"cursor,omitempty"`
}

// CharterRevisionsResult pages the history; NextCursor is the revision to
// pass to see MORE (empty when the page was the tail).
type CharterRevisionsResult struct {
	Revisions  []CharterView `json:"revisions"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

// CharterVocabularyResult publishes the charter package's own sets. The
// autonomy list is EMPTY-with-a-sentence until S56 wires the seam; the
// door never invents a verb the grant plane has not published.
type CharterVocabularyResult struct {
	CheckKinds   []string    `json:"checkKinds"`
	Caps         CharterCaps `json:"caps"`
	Autonomy     []string    `json:"autonomy"`
	AutonomyNote string      `json:"autonomyNote"`
}

// CharterCaps is the caps the validator enforces, one field per bound.
type CharterCaps struct {
	BodyBytes   int `json:"bodyBytes"`
	Goal        int `json:"goal"`
	Item        int `json:"item"`
	List        int `json:"list"`
	Acceptance  int `json:"acceptance"`
	Tags        int `json:"tags"`
	Tag         int `json:"tag"`
	Verify      int `json:"verify"`
	VerifyBytes int `json:"verifyBytes"`
	EnvRef      int `json:"envRef"`
	Note        int `json:"note"`
}

// CharterSaveParams — `{rootId, body, note?}`. Body is the charter JSON;
// the validator's sentences come back through the refusal voice.
type CharterSaveParams struct {
	RootID string          `json:"rootId"`
	Body   json.RawMessage `json:"body"`
	Note   string          `json:"note,omitempty"`
}

// --- the seam -----------------------------------------------------------------

// CharterService is the daemon's charter side: four reads and the one
// human write. Save takes the actor the CONNECTION resolved, never a
// params field (P4). Refusals arrive already voiced (ErrCharterRefused
// shaped) or plain — the handler answers what the seam says.
type CharterService interface {
	Get(ctx context.Context, p CharterGetParams) (CharterView, error)
	List(ctx context.Context) (CharterListResult, error)
	Revisions(ctx context.Context, p CharterRevisionsParams) (CharterRevisionsResult, error)
	Vocabulary(ctx context.Context) (CharterVocabularyResult, error)
	Save(ctx context.Context, actor Actor, p CharterSaveParams) (CharterView, error)
}

// --- registration ---------------------------------------------------------------

// RegisterCharter registers the five charter doors over a non-nil seam
// (V13: nil seam ⇒ all five ABSENT).
func (t *Table) RegisterCharter() bool {
	if t.Charter == nil {
		return false
	}
	for _, m := range []*Method{
		{Name: MethodCharterGet, RequiresInitialize: true, Handle: t.handleCharterGet},
		{Name: MethodCharterList, RequiresInitialize: true, Handle: t.handleCharterList},
		{Name: MethodCharterRevisions, RequiresInitialize: true, Handle: t.handleCharterRevisions},
		{Name: MethodCharterVocabulary, RequiresInitialize: true, Handle: t.handleCharterVocabulary},
		{Name: MethodCharterSave, RequiresInitialize: true, Handle: t.handleCharterSave},
	} {
		t.register(m)
	}
	return true
}

// --- the handlers -------------------------------------------------------------

var charterIdentityKeys = []string{"actor", "actorKind", "identity", "peer", "uid"}

func rejectCharterIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams will say it better
	}
	for _, k := range charterIdentityKeys {
		if _, ok := bag[k]; ok {
			return ErrBadParams{Err: fmt.Errorf(
				"%s takes no %q — a charter revision carries the connection's person, never a typed actor", method, k)}
		}
	}
	return nil
}

func (t *Table) handleCharterGet(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectCharterIdentityKeys(string(MethodCharterGet), raw); err != nil {
		return nil, err
	}
	var p CharterGetParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.RootID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("charter.get needs a rootId — charter.list hands out the roots that have charters")}
	}
	return t.Charter.Get(ctx, p)
}

func (t *Table) handleCharterList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectCharterIdentityKeys(string(MethodCharterList), raw); err != nil {
		return nil, err
	}
	return t.Charter.List(ctx)
}

func (t *Table) handleCharterRevisions(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectCharterIdentityKeys(string(MethodCharterRevisions), raw); err != nil {
		return nil, err
	}
	var p CharterRevisionsParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.RootID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("charter.revisions needs a rootId")}
	}
	return t.Charter.Revisions(ctx, p)
}

func (t *Table) handleCharterVocabulary(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectCharterIdentityKeys(string(MethodCharterVocabulary), raw); err != nil {
		return nil, err
	}
	return t.Charter.Vocabulary(ctx)
}

func (t *Table) handleCharterSave(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectCharterIdentityKeys(string(MethodCharterSave), raw); err != nil {
		return nil, err
	}
	// The person check FIRST (P4): a charter save is a person's door
	// (GI8), and the actor is the connection's.
	if t.ActorFor == nil {
		return nil, ErrCharterRefused.WithWhat("charter.save is a person's door and no identity is available on this connection — the daemon stamped none")
	}
	a := t.ActorFor(ctx, sess)
	if id := strings.TrimSpace(a.ID); id == "" || id == "0" {
		return nil, ErrCharterRefused.WithWhat("charter.save is a person's door and the peer is unresolvable (uid %q) — the actor is the connection's person, never a guess", a.ID)
	}
	var p CharterSaveParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.RootID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("charter.save needs a rootId — the charter lives on a project root")}
	}
	if len(p.Body) == 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("charter.save needs a body — charter.vocabulary publishes what a body may ask")}
	}
	return t.Charter.Save(ctx, Actor{Kind: "user", ID: strings.TrimSpace(a.ID)}, p)
}

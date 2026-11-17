package protocol

// ════════════════════════════════════════════════════════════════════════════
// triage management — S51 T2 (TM1, TM5, TM7): the edit and the three reads.
//
// `triage.update` is the HUMAN-DOOR edit of the two fields TM1 allows —
// title and body, with the actor from the connection (never a param, the
// T1 rule) and its 'update' audit row written by the store beside the
// edit. `triage.board` is the daemon read (attempts, scored candidates,
// hand-off status — the seam owns it because an embedding is computed and
// roadmap rows are read); `triage.history` and `triage.vocabulary` are
// door reads: the audit rows newest-first, and the closed sets ASSEMBLED
// from their owners, never restated.
//
// Every handler obeys the plane's shape: a nil door/seam answers
// ErrStoreUnavailable loud; an id with no row answers ErrTriageUnknown,
// named (T2's new refusal — "no such bug" and "no history" are different
// facts); a write refuses the identity keys BY KEY before the decode.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/triage"
)

// --- params ----------------------------------------------------------------

// TriageUpdateParams is TM1's narrow edit: at least one field, nil (absent)
// means LEAVE ALONE — which is why these are pointers, not empty strings:
// the difference between "no change to the body" and "a blank body" must
// be expressible, and only one of them is a refusal.
type TriageUpdateParams struct {
	ID    int64   `json:"id"`
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
}

// TriageBoardParams filters the backlog view. Every empty field is no
// filter; HandedOff is a pointer for the same reason the edit's fields
// are: three states (any / yes / no) do not fit in a bool's two.
type TriageBoardParams struct {
	ComponentRoot string `json:"componentRoot,omitempty"`
	Validation    string `json:"validation,omitempty"`
	Priority      string `json:"priority,omitempty"`
	HandedOff     *bool  `json:"handedOff,omitempty"`
}

// TriageHistoryParams names the bug whose audit to read; Limit bounds the
// read the way every triage list bounds (0 ⇒ the door's default, capped).
type TriageHistoryParams struct {
	ID    int64 `json:"id"`
	Limit int   `json:"limit,omitempty"`
}

// triageEditKeys are the fields TM1 does NOT own. A param carrying one is
// refused by KEY — the edit cannot say "I also validated it", "I also
// linked it", "I also handed it off": each of those is a verb with its own
// door, its own audit op and its own guard (TM1: title and body, nothing
// else; T2: the refusal is a refusal, not an ignore).
var triageEditKeys = []string{
	"validation", "validationState", "priority", "priorityState", "priorityLabel",
	"duplicateOf", "roadmapItemId", "validated", "attempts", "lastAttemptAt",
	"createdAt", "updatedAt", "componentRoot",
}

func rejectTriageEditKeys(method MethodName, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range triageEditKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf("%s refuses the param %q — a bug edit edits title and body and nothing else (TM1); state, priority, links and hand-off are verbs with their own doors", method, key)}
		}
	}
	return nil
}

// --- results -----------------------------------------------------------------

// TriageUpdateResult answers the edit with WHAT CHANGED and the row as it
// now reads — the caller who asked for changed:false sees the unchanged
// bug, not a nothing.
type TriageUpdateResult struct {
	Changed bool        `json:"changed"`
	Bug     *TriageView `json:"bug"`
}

// TriageHistoryRow is one audit row on the wire: the act, its fixed-
// template summary, the actor who made it, when. No id column rides — a
// reader of history reads acts, not primary keys.
type TriageHistoryRow struct {
	Op         string `json:"op"`
	Summary    string `json:"summary"`
	ActorKind  string `json:"actorKind"`
	ActorID    string `json:"actorId"`
	OccurredAt string `json:"occurredAt"`
}

// TriageBoardRow is one backlog row seen WHOLE: the bug (TriageView —
// validation and priority already TWO fields), the two sentences the
// board renders them as, the attempt digest from the audit rows, the
// scored candidates (a SUGGESTION with its nearness — never a link), and
// the roadmap status when the bug was handed off.
type TriageBoardRow struct {
	Bug           *TriageView        `json:"bug"`
	Describe      TriageDescribeView `json:"describe"`
	Attempts      int                `json:"attempts"`
	LastAttemptAt string             `json:"lastAttemptAt,omitempty"`
	Candidates    []TriageScoredView `json:"candidates"`
	RoadmapStatus string             `json:"roadmapStatus,omitempty"`
}

// TriageDescribeView is the wire shape of triage.Described — exported
// because the daemon's board seam fills it — two fields, because the two
// sets render separately (D2; the view has TWO fields to render, which IS
// the guard).
type TriageDescribeView struct {
	ValidationSentence string `json:"validationSentence"`
	PrioritySentence   string `json:"prioritySentence"`
	Duplicate          string `json:"duplicate,omitempty"`
	Handoff            string `json:"handoff,omitempty"`
}

// TriageScoredView is a candidate WITH its distance from the k-NN —
// smaller is nearer, the store's own words.
type TriageScoredView struct {
	Bug      *TriageView `json:"bug"`
	Distance float64     `json:"distance"`
}

// --- handlers ------------------------------------------------------------------

func (t *Table) handleTriageUpdate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.Triage == nil {
		return nil, ErrStoreUnavailable
	}
	if err := rejectTriageIdentityKeys(MethodTriageUpdate, raw); err != nil {
		return nil, err
	}
	if err := rejectTriageEditKeys(MethodTriageUpdate, raw); err != nil {
		return nil, err
	}
	var p TriageUpdateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if p.Title == nil && p.Body == nil {
		return nil, ErrBadParams{Err: fmt.Errorf("an update names at least one field — title or body (TM1)")}
	}
	actor, err := t.triageActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, err := AnswerTriageUpdate(t.Triage, p, actor)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// AnswerTriageUpdate is TM1's narrow edit as ONE answer, exported (S51 T6,
// the T5 fileAs rule): the person's socket door and the CLI's direct door
// run the same function over their own store — one implementation, two
// doors, no second copy of the refuse-by-key law's consequences.
func AnswerTriageUpdate(tr *triage.Triage, p TriageUpdateParams, actor triage.Actor) (TriageUpdateResult, error) {
	b, changed, r := tr.Update(p.ID, triage.UpdateBug{Title: p.Title, Body: p.Body}, actor)
	if r != nil {
		return TriageUpdateResult{}, asTriageRefusal(r)
	}
	return TriageUpdateResult{Changed: changed, Bug: toTriageView(b)}, nil
}

func (t *Table) handleTriageBoard(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	// The board is the daemon's read (attempts come from the audit, the
	// candidates from the embedding, the hand-off status from the roadmap
	// rows) — nil seam answers FIRST, like every dispatch door: with no
	// daemon there is no read to describe params to (V13's honesty).
	if t.TriageBoard == nil {
		return nil, ErrStoreUnavailable
	}
	var p TriageBoardParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ComponentRoot == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("componentRoot is required — a backlog board is scoped to one root, the same law as the bug list")}
	}
	res, err := t.TriageBoard(ctx, p)
	if err != nil {
		return nil, asTriageRefusal(err)
	}
	return res, nil
}

func (t *Table) handleTriageHistory(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.Triage == nil {
		return nil, ErrStoreUnavailable
	}
	var p TriageHistoryParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	rows, err := AnswerTriageHistory(t.Triage, p)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// AnswerTriageHistory is the audit read as ONE answer, exported (S51 T6):
// socket door and direct door run the same rows. The one refusal a history
// read can earn is the unknown id — it gets its OWN error
// (ErrTriageUnknown): an empty audit and a bug that never was are
// different facts a person must not confuse (T2).
func AnswerTriageHistory(tr *triage.Triage, p TriageHistoryParams) ([]TriageHistoryRow, error) {
	rows, r := tr.History(p.ID, p.Limit)
	if r != nil {
		return nil, triageUnknownLike(r.Error())
	}
	out := make([]TriageHistoryRow, len(rows))
	for i, ch := range rows {
		out[i] = TriageHistoryRow{
			Op:         ch.Op,
			Summary:    ch.Summary,
			ActorKind:  ch.ActorKind,
			ActorID:    ch.ActorID,
			OccurredAt: ch.OccurredAt,
		}
	}
	return out, nil
}

func (t *Table) handleTriageVocabulary(ctx context.Context, _ *Session, _ json.RawMessage) (any, error) {
	if t.Triage == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Triage.Vocabulary(), nil
}

// triageUnknownLike maps a door refusal that says the bug does not exist
// onto the NAMED refusal T2 added (ErrTriageUnknown); every other refusal
// stays the plane's ordinary triageRefused, the sentences unchanged.
func triageUnknownLike(msg string) error {
	if isUnknownBugSentence(msg) {
		return ErrTriageUnknown.WithWhat("%s", msg)
	}
	return triageRefusal(msg)
}

// isUnknownBugSentence recognises the door's own sentence — composed by
// triage.unknownBugReason as "triage: bug N does not exist" — so the two
// planes stay coupled by the sentence's shape, not a duplicated format.
func isUnknownBugSentence(msg string) bool {
	return strings.HasPrefix(msg, "triage: bug ") && strings.HasSuffix(msg, " does not exist")
}

// NewTriageView is the exported front to toTriageView, for the daemon's
// board seam — the ONE row→JSON shape (D2's two fields) stays one function,
// the daemon just reaches it through a door instead of a lowercase name.
func NewTriageView(b triage.Bug) *TriageView { return toTriageView(b) }

// --- triage.health --------------------------------------------------------------

// triage.health (TM4) — the setting's standing, not a gate. The daemon
// answers over the one blueprint setting: the EFFECTIVE name, and whether
// the named blueprint is ok / missing / invalid in the source right now —
// or unverifiable when the daemon cannot see the source at all (an unwired
// S48 never reads ok). A read: it answers, it never blocks a validate;
// the attempt checks the same facts at its own moment (TM4's refuse-not-
// fallback), which is why a green health line and a refused attempt can
// honestly coexist one second apart.

// TriageHealthCounts carries the analyzer's counts beside the verdict
// they decided — the shape S50's RM6 fixed for the same honesty.
type TriageHealthCounts struct {
	Valid        int `json:"valid"`
	Invalid      int `json:"invalid"`
	Unverifiable int `json:"unverifiable"`
}

type TriageHealthSetting struct {
	Key          string              `json:"key"`
	Name         string              `json:"name"`
	State        string              `json:"state"` // ok | missing | invalid | unverifiable
	ReportCounts *TriageHealthCounts `json:"reportCounts,omitempty"`
}

type TriageHealthResult struct {
	Settings    []TriageHealthSetting `json:"settings"`
	EvaluatedAt string                `json:"evaluatedAt"`
}

func (t *Table) handleTriageHealth(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if len(raw) > 0 && string(raw) != "{}" && string(raw) != "null" {
		return nil, ErrBadParams{Err: fmt.Errorf("triage.health takes no params — it answers over the setting's standing, and a filter would imply it gates something (TM4)")}
	}
	if t.TriageHealth == nil {
		return nil, ErrStoreUnavailable
	}
	return t.TriageHealth(ctx)
}

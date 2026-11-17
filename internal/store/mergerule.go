// mergerule.go — the merge rule and its append-only audit (VG10, S57 T5).
//
// The rule is the component's merge authority: which facts must be green
// before a merge may be asked of the forge. It is evaluated by T5's pure
// package (internal/mergerule — the direction is store→mergerule, pinned
// by the package's own import guard); this file only stores, resolves and
// audits it.
//
// The DEFAULTS ARE THE BASELINE (VG10): a row with every behavioural field
// at its default behaves exactly as no row, and a disabled row behaves as
// no row. The migration's CHECKs are the backstop a store bypassed; the
// closed-set checks below are the door.
//
// The write doors (VG23, the T3 shape): a merge-rule write admits a person
// (user) or the cli direct door, and nothing else — before any SQL. An
// unknown or archived component is a refusal, not an error (DB19); a
// refused write is a discriminated MergeRuleRefusal with a sentence, and
// writes nothing.
//
// The audit is append-only by trigger, in the same transaction as the
// write, one row per changing write: ops set|clear, the fixed-template
// summary built ONLY from the closed-set enums and the list counts — the
// list contents live in after_json, never in the audit sentence
// (SEC-033). revision is the event ledger's own number (MAX+1, never
// reset — a cleared rule's next set continues the sequence, P3), and the
// UNIQUE(component_id, revision) index makes a second row at the same
// number impossible rather than merely unlikely.

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/cjlapao/aeon/internal/mergerule"
)

// MergeRuleRefusal is the closed set of merge-rule write refusals (DB19).
// A refused write is a discriminated result, not an error: the caller
// prints the sentence and the surfaces render the code.
type MergeRuleRefusal string

const (
	MergeRuleRefusalNone                    MergeRuleRefusal = ""
	MergeRuleRefusalActorNotHuman           MergeRuleRefusal = "actor-not-human"
	MergeRuleRefusalComponentUnknown        MergeRuleRefusal = "component-unknown"
	MergeRuleRefusalComponentArchived       MergeRuleRefusal = "component-archived"
	MergeRuleRefusalRuleUnknown             MergeRuleRefusal = "merge-rule-unknown"
	MergeRuleRefusalRequireDoDInvalid       MergeRuleRefusal = "require-dod-invalid"
	MergeRuleRefusalChecksPolicyInvalid     MergeRuleRefusal = "checks-policy-invalid"
	MergeRuleRefusalNoChecksInvalid         MergeRuleRefusal = "no-checks-invalid"
	MergeRuleRefusalReviewModeInvalid       MergeRuleRefusal = "review-mode-invalid"
	MergeRuleRefusalMethodInvalid           MergeRuleRefusal = "method-invalid"
	MergeRuleRefusalRequiredChecksInvalid   MergeRuleRefusal = "required-checks-invalid"
	MergeRuleRefusalAeonReviewAgentsInvalid MergeRuleRefusal = "aeon-review-agents-invalid"
)

// MergeRuleWrite is the result of a merge-rule write: the new rule when
// accepted, or the refusal and its sentence when not.
type MergeRuleWrite struct {
	Rule    mergerule.Rule
	Refusal MergeRuleRefusal
	Reason  string
}

// OK reports whether the write was accepted.
func (w MergeRuleWrite) OK() bool { return w.Refusal == MergeRuleRefusalNone }

func refuseMergeRule(r MergeRuleRefusal, sentence string) MergeRuleWrite {
	return MergeRuleWrite{Refusal: r, Reason: sentence}
}

func refuseMergeRulef(r MergeRuleRefusal, format string, args ...any) MergeRuleWrite {
	return MergeRuleWrite{Refusal: r, Reason: fmt.Sprintf(format, args...)}
}

// mergeRuleActorRefusal answers the T3 door for the rule's writes: a
// person (user) or the cli direct door, and nothing else. The rule is the
// loop's merge authority — the thing that decides whether the engine may
// merge at all — and that is not something a loop's agent takes for
// itself (it proposes; T8's proposal kind carries it; a human commits).
func (a Actor) mergeRuleActorRefusal() (MergeRuleRefusal, string) {
	if a.ID == "" {
		return MergeRuleRefusalActorNotHuman, "refused: a merge-rule write needs a named actor — an anonymous author cannot be audited (S57 T5)"
	}
	if a.Kind == "user" || a.Kind == "cli" {
		return MergeRuleRefusalNone, ""
	}
	return MergeRuleRefusalActorNotHuman, fmt.Sprintf("refused: only a person may write a merge rule · caller kind %q proposes, it does not author (S57 T5)", a.Kind)
}

// --- the row, decoded ------------------------------------------------------

const mergeRuleColumns = `component_id, enabled, require_local, require_dod, required_checks,
	checks_policy, no_checks, review_mode, aeon_review_agents, require_threads_resolved,
	method, revision, updated_at`

// scanMergeRule decodes one row into the canonical rule (mergerule.Rule):
// the JSON fields are decoded here and nowhere else, so the store and the
// evaluator cannot disagree on what a row says.
func scanMergeRule(row interface{ Scan(...any) error }) (mergerule.Rule, error) {
	var (
		r                       mergerule.Rule
		enabled, local, threads int
		requiredChecks, agents  string
	)
	err := row.Scan(&r.ComponentID, &enabled, &local, &r.RequireDoD, &requiredChecks,
		&r.ChecksPolicy, &r.NoChecks, &r.ReviewMode, &agents, &threads,
		&r.Method, &r.Revision, &r.UpdatedAt)
	if err != nil {
		return mergerule.Rule{}, wrapMergeRuleErr("read merge rule", err)
	}
	r.Enabled = enabled == 1
	r.RequireLocal = local == 1
	r.RequireThreadsResolved = threads == 1
	checks, ok := jsonStringList(requiredChecks)
	if !ok {
		return mergerule.Rule{}, fmt.Errorf("store: merge rule %s: required_checks is not a JSON string list: %w", r.ComponentID, ErrNotFound)
	}
	r.RequiredChecks = checks
	ruleAgents, ok := jsonStringList(agents)
	if !ok {
		return mergerule.Rule{}, fmt.Errorf("store: merge rule %s: aeon_review_agents is not a JSON string list: %w", r.ComponentID, ErrNotFound)
	}
	r.AeonReviewAgents = ruleAgents
	return r, nil
}

func wrapMergeRuleErr(what string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("store: %s: %w", what, ErrNotFound)
	}
	return fmt.Errorf("store: %s: %w", what, err)
}

// --- the event's canonical snapshots ----------------------------------------

// mergeRuleSnapshot is the fixed struct the event rows carry in
// before_json / after_json: ids and values only, field order fixed by the
// struct, so the audit is diffable and never carries a writer's formatting
// (the T3 eventSnapshot shape).
type mergeRuleSnapshot struct {
	Enabled                bool     `json:"enabled"`
	RequireLocal           bool     `json:"require_local"`
	RequireDoD             string   `json:"require_dod"`
	RequiredChecks         []string `json:"required_checks"`
	ChecksPolicy           string   `json:"checks_policy"`
	NoChecks               string   `json:"no_checks"`
	ReviewMode             string   `json:"review_mode"`
	AeonReviewAgents       []string `json:"aeon_review_agents"`
	RequireThreadsResolved bool     `json:"require_threads_resolved"`
	Method                 string   `json:"method"`
	Revision               int      `json:"revision"`
}

func ruleSnapshotOf(r mergerule.Rule) mergeRuleSnapshot {
	return mergeRuleSnapshot{
		Enabled: r.Enabled, RequireLocal: r.RequireLocal, RequireDoD: r.RequireDoD,
		RequiredChecks: r.RequiredChecks, ChecksPolicy: r.ChecksPolicy, NoChecks: r.NoChecks,
		ReviewMode: r.ReviewMode, AeonReviewAgents: r.AeonReviewAgents,
		RequireThreadsResolved: r.RequireThreadsResolved, Method: r.Method, Revision: r.Revision,
	}
}

func ruleSnapshotJSON(r mergerule.Rule) string {
	raw, err := json.Marshal(ruleSnapshotOf(r))
	if err != nil {
		// A fixed struct of strings, bools and string lists cannot fail to
		// marshal; failing here is a broken store, not a condition to handle.
		panic("store: merge rule snapshot: " + err.Error())
	}
	return string(raw)
}

// ruleSummary is the fixed-template audit sentence (SEC-033): built ONLY
// from the closed-set enums and the list counts — the list contents live
// in after_json, never in the sentence.
func ruleSummary(op string, r mergerule.Rule) string {
	if op == "clear" {
		return "clear merge rule"
	}
	return fmt.Sprintf("set merge rule (dod %s, policy %s, no-checks %s, mode %s, method %s, local %v, threads %v, checks %d, agents %d, enabled %v)",
		r.RequireDoD, r.ChecksPolicy, r.NoChecks, r.ReviewMode, r.Method,
		r.RequireLocal, r.RequireThreadsResolved, len(r.RequiredChecks), len(r.AeonReviewAgents), r.Enabled)
}

// --- the closed sets (the door; the CHECK is the backstop) -------------------

func requireDoDOK(v string) bool   { return v == "none" || v == "all_met" }
func checksPolicyOK(v string) bool { return v == "reported_green" || v == "required_only" }
func noChecksOK(v string) bool     { return v == "allow" || v == "refuse" }
func reviewModeOK(v string) bool   { return v == "forge" || v == "aeon" || v == "either" || v == "both" }
func methodOK(v string) bool       { return v == "squash" || v == "merge" || v == "rebase" }

// jsonStringList decodes a JSON string list — a bare ARRAY of strings.
// An absent list (null) is the EMPTY list, not a refusal: the evaluator's
// set semantics treat the two as one, and Baseline() itself carries a nil
// RequiredChecks, which a nil-rejecting door would make unsettable. An
// object or a bare string is refused at the door, not discovered at the
// CHECK.
func jsonStringList(s string) ([]string, bool) {
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, false
	}
	if out == nil {
		out = []string{}
	}
	return out, true
}

// --- the reads ----------------------------------------------------------------

// GetMergeRule reads a component's own row. Absent is ErrNotFound — the
// honest answer to "what does this component say?" is the empty set, and
// EffectiveMergeRule is the one that fills it in.
func (s *Store) GetMergeRule(componentID string) (mergerule.Rule, error) {
	r, err := scanMergeRule(s.r.QueryRow(`SELECT `+mergeRuleColumns+` FROM merge_rule WHERE component_id = ?`, componentID))
	if err != nil {
		return mergerule.Rule{}, err
	}
	r.Source = "own"
	return r, nil
}

// EffectiveMergeRule resolves the rule that governs a component's merges
// (the VG3 rule, the EffectiveGates model): the component's own row, else
// — for a child whose root is not archived — the root's, else the
// BASELINE: mergerule.Baseline() with Revision 0 and Source "baseline",
// never a row (the baseline is the migration's defaults, which a test
// proves behaves as no row). The returned rule's ComponentID is always
// the component asked about, because that is the component whose merge is
// being judged. An archived node supplies nothing and takes nothing; an
// unknown component is an error (a read that cannot resolve its target is
// not an empty answer).
func (s *Store) EffectiveMergeRule(componentID string) (mergerule.Rule, string, error) {
	baseline := func() (mergerule.Rule, string, error) {
		r := mergerule.Baseline()
		r.ComponentID = componentID
		r.Source = "baseline"
		return r, "baseline", nil
	}
	exists, archived, err := s.componentState(componentID)
	if err != nil {
		return mergerule.Rule{}, "", fmt.Errorf("store: effective merge rule of %s: %w", componentID, err)
	}
	if !exists {
		return mergerule.Rule{}, "", fmt.Errorf("store: effective merge rule of %s: %w", componentID, ErrNotFound)
	}
	if archived {
		return baseline()
	}
	if own, err := s.GetMergeRule(componentID); err == nil {
		own.ComponentID = componentID
		own.Source = "own"
		return own, "own", nil
	} else if !errors.Is(err, ErrNotFound) {
		return mergerule.Rule{}, "", fmt.Errorf("store: effective merge rule of %s: %w", componentID, err)
	}
	var parentID sql.NullString
	if err := s.r.QueryRow(`SELECT parent_id FROM component WHERE id = ?`, componentID).Scan(&parentID); err != nil {
		return mergerule.Rule{}, "", fmt.Errorf("store: effective merge rule of %s: %w", componentID, err)
	}
	if parentID.Valid && parentID.String != "" {
		var rootArchived sql.NullString
		if err := s.r.QueryRow(`SELECT archived_at FROM component WHERE id = ?`, parentID.String).Scan(&rootArchived); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return mergerule.Rule{}, "", fmt.Errorf("store: effective merge rule of %s: root %s vanished: %w", componentID, parentID.String, ErrNotFound)
			}
			return mergerule.Rule{}, "", fmt.Errorf("store: effective merge rule of %s: %w", componentID, err)
		}
		if !rootArchived.Valid {
			if root, err := s.GetMergeRule(parentID.String); err == nil {
				root.ComponentID = componentID
				root.Source = "root"
				return root, "root", nil
			} else if !errors.Is(err, ErrNotFound) {
				return mergerule.Rule{}, "", fmt.Errorf("store: effective merge rule of %s: %w", componentID, err)
			}
		}
	}
	return baseline()
}

// --- the writes -----------------------------------------------------------------

// nextMergeRuleRevision is the ledger's next number: the audit's MAX + 1,
// never the row's counter (a cleared row's row is gone, but its revisions
// remain — the sequence never resets, P3).
func (s *Store) nextMergeRuleRevision(tx *sql.Tx, componentID string) (int, error) {
	var n int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(revision),0)+1 FROM merge_rule_event WHERE component_id = ?`,
		componentID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: next merge rule revision for %s: %w", componentID, err)
	}
	return n, nil
}

// SetMergeRule writes a component's rule (inserting the first, continuing
// the row's life after) with its event in the same transaction. The
// closed sets are checked here, before any SQL — the migration's CHECKs
// are the backstop a store bypassed, and the refusal names the value it
// refused (DB19).
func (s *Store) SetMergeRule(a Actor, in mergerule.Rule) (MergeRuleWrite, error) {
	if r, why := a.mergeRuleActorRefusal(); r != MergeRuleRefusalNone {
		return refuseMergeRule(r, why), nil
	}
	if in.ComponentID == "" {
		return MergeRuleWrite{}, fmt.Errorf("store: set merge rule: the rule names no component")
	}
	exists, archived, err := s.componentState(in.ComponentID)
	if err != nil {
		return MergeRuleWrite{}, err
	}
	if !exists {
		return refuseMergeRulef(MergeRuleRefusalComponentUnknown, "refused: %s is not a component", in.ComponentID), nil
	}
	if archived {
		return refuseMergeRulef(MergeRuleRefusalComponentArchived, "refused: %s is archived — its rule is history", in.ComponentID), nil
	}
	// The closed sets, one refusal each, naming the value.
	switch {
	case !requireDoDOK(in.RequireDoD):
		return refuseMergeRulef(MergeRuleRefusalRequireDoDInvalid, "refused: require_dod is %q; the closed set is none|all_met", in.RequireDoD), nil
	case !checksPolicyOK(in.ChecksPolicy):
		return refuseMergeRulef(MergeRuleRefusalChecksPolicyInvalid, "refused: checks_policy is %q; the closed set is reported_green|required_only", in.ChecksPolicy), nil
	case !noChecksOK(in.NoChecks):
		return refuseMergeRulef(MergeRuleRefusalNoChecksInvalid, "refused: no_checks is %q; the closed set is allow|refuse", in.NoChecks), nil
	case !reviewModeOK(in.ReviewMode):
		return refuseMergeRulef(MergeRuleRefusalReviewModeInvalid, "refused: review_mode is %q; the closed set is forge|aeon|either|both", in.ReviewMode), nil
	case !methodOK(in.Method):
		return refuseMergeRulef(MergeRuleRefusalMethodInvalid, "refused: method is %q; the closed set is squash|merge|rebase", in.Method), nil
	}
	checks, ok := jsonStringList(string(mustJSON(in.RequiredChecks)))
	if !ok {
		return refuseMergeRule(MergeRuleRefusalRequiredChecksInvalid, "refused: required_checks is not a JSON list of check names"), nil
	}
	agents, ok := jsonStringList(string(mustJSON(in.AeonReviewAgents)))
	if !ok {
		return refuseMergeRule(MergeRuleRefusalAeonReviewAgentsInvalid, "refused: aeon_review_agents is not a JSON list of agent names"), nil
	}
	in.RequiredChecks = checks
	in.AeonReviewAgents = agents

	now := s.Now()
	tx, err := s.w.Begin()
	if err != nil {
		return MergeRuleWrite{}, fmt.Errorf("store: set merge rule for %s: %w", in.ComponentID, err)
	}
	defer tx.Rollback()
	revision, err := s.nextMergeRuleRevision(tx, in.ComponentID)
	if err != nil {
		return MergeRuleWrite{}, err
	}
	// The row as it stands — before_json, and the guard for the update.
	var cur mergerule.Rule
	var hasCur bool
	if r, err := scanMergeRule(tx.QueryRow(`SELECT `+mergeRuleColumns+` FROM merge_rule WHERE component_id = ?`, in.ComponentID)); err == nil {
		cur, hasCur = r, true
	} else if !errors.Is(err, ErrNotFound) {
		return MergeRuleWrite{}, err
	}
	if hasCur && ruleSameFields(cur, in) {
		// A no-op: nothing moved, nothing is told, the audit stays as it was.
		_ = tx.Rollback()
		return MergeRuleWrite{Rule: cur}, nil
	}
	next := in
	next.Revision = revision
	next.Source = "own"
	next.UpdatedAt = now
	if hasCur {
		res, err := tx.Exec(`UPDATE merge_rule SET enabled=?, require_local=?, require_dod=?, required_checks=?,
			checks_policy=?, no_checks=?, review_mode=?, aeon_review_agents=?, require_threads_resolved=?,
			method=?, revision=?, updated_at=? WHERE component_id = ?`,
			boolInt(next.Enabled), boolInt(next.RequireLocal), next.RequireDoD, mustJSON(next.RequiredChecks),
			next.ChecksPolicy, next.NoChecks, next.ReviewMode, mustJSON(next.AeonReviewAgents),
			boolInt(next.RequireThreadsResolved), next.Method, next.Revision, now, in.ComponentID)
		if err != nil {
			return MergeRuleWrite{}, fmt.Errorf("store: set merge rule for %s: %w", in.ComponentID, err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return MergeRuleWrite{}, fmt.Errorf("store: set merge rule for %s: the row moved under the write", in.ComponentID)
		}
	} else {
		if _, err := tx.Exec(`INSERT INTO merge_rule (component_id, enabled, require_local, require_dod, required_checks,
			checks_policy, no_checks, review_mode, aeon_review_agents, require_threads_resolved,
			method, revision, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			in.ComponentID, boolInt(next.Enabled), boolInt(next.RequireLocal), next.RequireDoD, mustJSON(next.RequiredChecks),
			next.ChecksPolicy, next.NoChecks, next.ReviewMode, mustJSON(next.AeonReviewAgents),
			boolInt(next.RequireThreadsResolved), next.Method, next.Revision, now); err != nil {
			return MergeRuleWrite{}, fmt.Errorf("store: set merge rule for %s: %w", in.ComponentID, err)
		}
	}
	if err := appendMergeRuleEvent(tx, a, in.ComponentID, revision, "set", now, cur, hasCur, next); err != nil {
		return MergeRuleWrite{}, fmt.Errorf("store: set merge rule for %s: %w", in.ComponentID, err)
	}
	if err := tx.Commit(); err != nil {
		return MergeRuleWrite{}, fmt.Errorf("store: set merge rule for %s: %w", in.ComponentID, err)
	}
	return MergeRuleWrite{Rule: next}, nil
}

// ClearMergeRule deletes a component's row (the component returns to the
// baseline — or its root's rule, if it is a child) with its event in the
// same transaction. The rule's revisions survive in the audit: the next
// set continues the sequence. A component with no row is a refusal, not
// an error (nothing was cleared).
func (s *Store) ClearMergeRule(a Actor, componentID string) (MergeRuleWrite, error) {
	if r, why := a.mergeRuleActorRefusal(); r != MergeRuleRefusalNone {
		return refuseMergeRule(r, why), nil
	}
	exists, archived, err := s.componentState(componentID)
	if err != nil {
		return MergeRuleWrite{}, err
	}
	if !exists {
		return refuseMergeRulef(MergeRuleRefusalComponentUnknown, "refused: %s is not a component", componentID), nil
	}
	if archived {
		return refuseMergeRulef(MergeRuleRefusalComponentArchived, "refused: %s is archived — its rule is history", componentID), nil
	}
	cur, err := s.GetMergeRule(componentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return refuseMergeRulef(MergeRuleRefusalRuleUnknown, "refused: %s has no merge rule to clear", componentID), nil
		}
		return MergeRuleWrite{}, err
	}
	now := s.Now()
	tx, err := s.w.Begin()
	if err != nil {
		return MergeRuleWrite{}, fmt.Errorf("store: clear merge rule for %s: %w", componentID, err)
	}
	defer tx.Rollback()
	revision, err := s.nextMergeRuleRevision(tx, componentID)
	if err != nil {
		return MergeRuleWrite{}, err
	}
	res, err := tx.Exec(`DELETE FROM merge_rule WHERE component_id = ?`, componentID)
	if err != nil {
		return MergeRuleWrite{}, fmt.Errorf("store: clear merge rule for %s: %w", componentID, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		// It was cleared between the read and this write: nothing was
		// cleared by this one, and the audit must not say otherwise.
		return refuseMergeRulef(MergeRuleRefusalRuleUnknown, "refused: %s has no merge rule to clear", componentID), nil
	}
	cleared := cur
	cleared.Revision = revision
	if err := appendMergeRuleEvent(tx, a, componentID, revision, "clear", now, cleared, true, mergerule.Rule{}); err != nil {
		return MergeRuleWrite{}, fmt.Errorf("store: clear merge rule for %s: %w", componentID, err)
	}
	if err := tx.Commit(); err != nil {
		return MergeRuleWrite{}, fmt.Errorf("store: clear merge rule for %s: %w", componentID, err)
	}
	return MergeRuleWrite{}, nil
}

// appendMergeRuleEvent writes the audit row (op set|clear) — before_json
// is the row as it stood (NULL on a first set), after_json the row as it
// stands (NULL on a clear). One row per changing write, same transaction.
func appendMergeRuleEvent(tx *sql.Tx, a Actor, componentID string, revision int, op, at string,
	before mergerule.Rule, hasBefore bool, after mergerule.Rule) error {
	var beforeJSON, afterJSON any
	if hasBefore {
		b := ruleSnapshotJSON(before)
		beforeJSON = b
	}
	if op != "clear" {
		a2 := ruleSnapshotJSON(after)
		afterJSON = a2
	}
	if _, err := tx.Exec(`INSERT INTO merge_rule_event (component_id, revision, op, actor_kind, actor_id, summary, before_json, after_json, at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		componentID, revision, op, a.Kind, a.ID, ruleSummary(op, after), beforeJSON, afterJSON, at); err != nil {
		return fmt.Errorf("append merge rule event (%s r%d): %w", op, revision, err)
	}
	return nil
}

// --- the audit reads --------------------------------------------------------------

// MergeRuleEvent is one audit row, in the order the story was told in.
type MergeRuleEvent struct {
	ID          int64
	ComponentID string
	Revision    int
	Op          string // "set" | "clear"
	ActorKind   string
	ActorID     string
	Summary     string
	BeforeJSON  *string
	AfterJSON   *string
	At          string
}

// MergeRuleEventFilter scopes the audit read.
type MergeRuleEventFilter struct {
	ComponentID string
	Limit       int // 0 = no limit
}

// ListMergeRuleEvents reads the append-only audit in id order (revision
// order) — the order the story was told in.
func (s *Store) ListMergeRuleEvents(f MergeRuleEventFilter) ([]MergeRuleEvent, error) {
	q := `SELECT id, component_id, revision, op, actor_kind, actor_id, summary, before_json, after_json, at
		FROM merge_rule_event WHERE 1=1`
	var args []any
	if f.ComponentID != "" {
		q += ` AND component_id = ?`
		args = append(args, f.ComponentID)
	}
	q += ` ORDER BY id`
	if f.Limit > 0 {
		q += ` LIMIT ` + strconv.Itoa(f.Limit)
	}
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list merge rule events: %w", err)
	}
	defer rows.Close()
	var out []MergeRuleEvent
	for rows.Next() {
		var e MergeRuleEvent
		var before, after sql.NullString
		if err := rows.Scan(&e.ID, &e.ComponentID, &e.Revision, &e.Op, &e.ActorKind, &e.ActorID,
			&e.Summary, &before, &after, &e.At); err != nil {
			return nil, fmt.Errorf("store: list merge rule events: %w", err)
		}
		e.BeforeJSON = nullStringPtr(before)
		e.AfterJSON = nullStringPtr(after)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list merge rule events: %w", err)
	}
	return out, nil
}

// ruleSameFields compares the row's behavioural fields — the identity
// (component, revision, source) is the store's own bookkeeping and is not
// part of the "did anything move" question.
func ruleSameFields(a, b mergerule.Rule) bool {
	return a.Enabled == b.Enabled && a.RequireLocal == b.RequireLocal &&
		a.RequireDoD == b.RequireDoD && slices.Equal(a.RequiredChecks, b.RequiredChecks) &&
		a.ChecksPolicy == b.ChecksPolicy && a.NoChecks == b.NoChecks &&
		a.ReviewMode == b.ReviewMode && slices.Equal(a.AeonReviewAgents, b.AeonReviewAgents) &&
		a.RequireThreadsResolved == b.RequireThreadsResolved && a.Method == b.Method
}

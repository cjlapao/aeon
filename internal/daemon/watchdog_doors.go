// watchdog_doors.go — the daemon's implementation of the
// protocol.WatchdogStore seam (Table.Watchdog): S55 T9's SIX watchdog doors.
// They are methods on the watchdog service itself — the same instance serve's
// RunWatchdog ticker acts through — so the list's loopsInEpisode reads the
// service's own once-per-episode memory and the two cannot drift.
//
// # The views are assembled here, not in the store
//
// The store serves ROWS (the rule, its audit, its fires, the signal
// availability). The VIEW is the daemon's: row + the threshold in its human
// units (the unit the signal's, served by the vocabulary) + the fire side +
// the loops in an episode.
//
// # Confirmation
//
// The direction of each act is classified here: a create and a re-enable
// TIGHTEN (commit directly); a disable and every removal LOOSEN (commit only
// against the digest of the canonical statement rendered from the row — the
// S45 PM7.1 pattern, the T8 budget shape). A digest that does not match the
// statement the daemon NOW renders is a confirm-mismatch: the row moved since
// the read, and the act is refused rather than silently loosened.
//
// # No caching
//
// Nothing here caches rows across calls: a rule a person disabled at the last
// tick reads disabled at this read, and the rules re-read at every tick is
// the same discipline the doors follow.

package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/watchdog"
)

// --- the conversions ----------------------------------------------------------

// thresholdHuman renders a rule's threshold in its own unit (the signal's —
// served by the vocabulary, never assumed): a human time for the two time
// signals (no-activity, step-over-deadline), the bare count for the two count
// signals. The view's human units; the row keeps the number.
func thresholdHuman(signal string, threshold int64) string {
	switch signal {
	case "same-position", "repeat-block-reason":
		return strconv.FormatInt(threshold, 10)
	default: // milliseconds
		ms := threshold
		for _, u := range []struct {
			div    int64
			suffix string
		}{
			{604800000, "w"}, {86400000, "d"}, {3600000, "h"}, {60000, "m"}, {1000, "s"},
		} {
			if ms%u.div == 0 {
				return strconv.FormatInt(ms/u.div, 10) + u.suffix
			}
		}
		return strconv.FormatInt(ms, 10) + "ms"
	}
}

// watchdogRuleView assembles one rule's list-with-details view: the row, the
// human threshold, the fire side (how often it has fired and when, from the
// loops' own control audit) and the loops currently in an episode under it
// (the service's once-per-episode memory, below).
func (s *watchdogService) watchdogRuleView(r store.WatchdogRule) (protocol.WatchdogRuleView, error) {
	count, lastAt, err := s.d.store.WatchdogFireStats(r.ID)
	if err != nil {
		return protocol.WatchdogRuleView{}, fmt.Errorf("daemon: the fire stats of rule %s: %w", r.ID, err)
	}
	return protocol.WatchdogRuleView{
		ID: r.ID, Scope: r.Scope, SubjectID: r.SubjectID,
		Signal: r.Signal, Threshold: r.Threshold, ThresholdHuman: thresholdHuman(r.Signal, r.Threshold),
		Action: r.Action, Enabled: r.Enabled, Origin: r.Origin,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		LastFiredAt:    lastAt,
		FireCount:      count,
		LoopsInEpisode: s.loopsInEpisode(r.ID),
	}, nil
}

// loopsInEpisode is the list's per-rule detail: the loops the service's
// once-per-episode memory says have already been fired for by this rule in
// their current episode (the rule will not re-fire for them until they make
// progress — or the daemon restarts, when the in-process memory is honestly
// gone). It reads the ticker's memory under the read lock, so a list during a
// tick sees a consistent snapshot.
func (s *watchdogService) loopsInEpisode(ruleID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []string{}
	for loopID, rules := range s.lastFired {
		if rules[ruleID] != "" {
			out = append(out, loopID)
		}
	}
	sort.Strings(out)
	return out
}

// --- the confirmation statements -------------------------------------------------

// watchdogDigest is the confirmation digest: the hex SHA-256 of the canonical
// statement. The statements' grammar is fixed (the renderers below), so the
// digest is a function of the row — a commit carrying a digest the daemon
// cannot re-render is a commit against a row that moved (PM7.1: bound to what
// was read).
func watchdogDigest(statement string) string {
	sum := sha256.Sum256([]byte(statement))
	return hex.EncodeToString(sum[:])
}

// ruleKey renders the rule's closed identity (scope, subject, signal, action,
// human threshold) the statements name — one renderer, byte-stable.
func ruleKey(r store.WatchdogRule) string {
	return fmt.Sprintf("%s %s, %s %s, threshold %s",
		r.Scope, r.SubjectID, r.Signal, r.Action, thresholdHuman(r.Signal, r.Threshold))
}

// disableStatement is the canonical statement of a disable (a setEnabled
// false, or a set that flips an enabled rule off): the rule, and that it will
// no longer fire. A disable loosens a safeguard, so it is the act the digest
// binds.
func disableStatement(r store.WatchdogRule) string {
	return fmt.Sprintf("disable watchdog rule %s (%s): after this it will no longer fire", r.ID, ruleKey(r))
}

// watchdogRemoveStatement is the canonical statement of a removal: the rule,
// and that it will never fire again. A removal always loosens (any origin),
// so it always confirms. (Named for the plane: the budget door's own
// removeStatement lives in budget_service.go, and a shared name would be a
// second renderer the compiler cannot tell apart.)
func watchdogRemoveStatement(r store.WatchdogRule) string {
	return fmt.Sprintf("remove watchdog rule %s (%s): after this it will never fire", r.ID, ruleKey(r))
}

// --- the refusals (the daemon side) -------------------------------------------------

// unknownRuleError is the show/setEnabled/remove refusal for an id with no
// row: there is nothing to read and nothing to move, and the refusal says so
// rather than answering an empty view (an empty show reads as "it exists and
// does nothing", which is a different claim).
func unknownRuleError(verb, id string) *protocol.Error {
	e := protocol.ErrWatchdogRefused.WithWhat("watchdog %s refused: no rule %s exists — it was never created, or was already removed", verb, id)
	e.Data.Kind = "unknown-rule"
	return e
}

// watchdogDoorActorKind maps the protocol's stamped actor onto the watchdog
// audit's closed kinds (user|cli|system) — the same mapping the budget door
// uses: a person acts as a person; any other kind a client would stamp acts
// as one. `system` is the engine's own (the ticker's fires, the
// watchdogActorKind constant) — never a caller's.
func watchdogDoorActorKind(kind string) string {
	if kind == "user" {
		return "user"
	}
	return "cli"
}

// --- the six doors ---------------------------------------------------------------

// List is watchdog.list's read: every rule, filtered (every filter optional),
// already detailed (the human threshold, the fire side, the loops in an
// episode).
func (s *watchdogService) List(ctx context.Context, scope, subjectID string, enabled *bool) ([]protocol.WatchdogRuleView, error) {
	rules, err := s.d.store.ListWatchdogRules()
	if err != nil {
		return nil, fmt.Errorf("daemon: list the watchdog rules: %w", err)
	}
	views := []protocol.WatchdogRuleView{}
	for _, r := range rules {
		if scope != "" && r.Scope != scope {
			continue
		}
		if subjectID != "" && r.SubjectID != subjectID {
			continue
		}
		if enabled != nil && r.Enabled != *enabled {
			continue
		}
		v, err := s.watchdogRuleView(r)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, nil
}

// Show is watchdog.show's read: the rule (detailed), its audit trail and its
// fires, newest first. A show of an id with no row is a refusal
// (unknown-rule), not an empty result.
func (s *watchdogService) Show(ctx context.Context, id string) (protocol.WatchdogShowResult, error) {
	r, err := s.d.store.GetWatchdogRule(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.WatchdogShowResult{}, unknownRuleError("show", id)
		}
		return protocol.WatchdogShowResult{}, fmt.Errorf("daemon: get the watchdog rule %s: %w", id, err)
	}
	v, err := s.watchdogRuleView(r)
	if err != nil {
		return protocol.WatchdogShowResult{}, err
	}
	events, err := s.d.store.ListWatchdogEvents(id, protocol.WatchdogEventWindow)
	if err != nil {
		return protocol.WatchdogShowResult{}, fmt.Errorf("daemon: the audit of rule %s: %w", id, err)
	}
	evs := make([]protocol.WatchdogEventView, 0, len(events))
	for _, e := range events {
		evs = append(evs, protocol.WatchdogEventView{
			At: e.At, Kind: e.Kind, ActorKind: e.ActorKind, ActorID: e.ActorID,
			OldValue: e.OldValue, NewValue: e.NewValue,
		})
	}
	fires, err := s.d.store.ListWatchdogFires(id, protocol.WatchdogFireWindow)
	if err != nil {
		return protocol.WatchdogShowResult{}, fmt.Errorf("daemon: the fires of rule %s: %w", id, err)
	}
	fvs := make([]protocol.WatchdogFireView, 0, len(fires))
	for _, f := range fires {
		signal, action, episode := parseWatchdogFireDetail(f.Detail)
		fvs = append(fvs, protocol.WatchdogFireView{At: f.At, LoopID: f.LoopID, Signal: signal, Action: action, Episode: episode})
	}
	return protocol.WatchdogShowResult{Rule: v, Events: evs, Fires: fvs}, nil
}

// parseWatchdogFireDetail reads a fire's closed-key detail (ruleId/signal/
// action/episode) into the view's fields. A detail that does not parse is an
// honest empty (the row is the audit's, not the view's): the view shows what
// it can, and the store's row is the truth.
func parseWatchdogFireDetail(detail string) (signal, action, episode string) {
	var d struct {
		Signal  string `json:"signal"`
		Action  string `json:"action"`
		Episode string `json:"episode"`
	}
	if detail == "" {
		return "", "", ""
	}
	if err := json.Unmarshal([]byte(detail), &d); err != nil {
		return "", "", ""
	}
	return d.Signal, d.Action, d.Episode
}

// Set is watchdog.set's human door: create or update by the unique key
// (scope, subjectId, signal, action, threshold). At an EXISTING key the only
// editable field is enabled — a set there flips the rule (a disable confirms,
// an enable is free, a no-op writes nothing, LC11). At a NEW key it creates a
// rule (tightening, free) — unless the combination (scope, subject, signal,
// action) already has a rule at another threshold, which is a duplicate
// refusal: one threshold per combination, and a threshold change is a removal
// (confirmed) plus a fresh create (free). The subject and the signal's
// availability are checked before any write: a rule on a subject that is not
// real, or on a signal this store cannot serve, is a refusal, not a silently
// inert row.
func (s *watchdogService) Set(ctx context.Context, actor protocol.Actor, p protocol.WatchdogSetParams) (protocol.WatchdogSetResult, *protocol.WatchdogRefusal, error) {
	// Param-SHAPE faults are -32602 (the parse shape), not refusals: a value
	// outside the closed set is not an act the plane declined, it is an act
	// the plane cannot read.
	switch p.Scope {
	case "workspace", "component":
	default:
		return protocol.WatchdogSetResult{}, nil, protocol.ErrBadParams{Err: fmt.Errorf("scope is workspace or component, not %q", p.Scope)}
	}
	switch p.Signal {
	case string(watchdog.SignalNoActivity), string(watchdog.SignalSamePosition), string(watchdog.SignalRepeatBlockReason), string(watchdog.SignalStepOverDeadline):
	default:
		return protocol.WatchdogSetResult{}, nil, protocol.ErrBadParams{Err: fmt.Errorf("signal is no-activity, same-position, repeat-block-reason or step-over-deadline, not %q", p.Signal)}
	}
	switch p.Action {
	case string(watchdog.ActionNotify), string(watchdog.ActionStop), string(watchdog.ActionBlock):
	default:
		return protocol.WatchdogSetResult{}, nil, protocol.ErrBadParams{Err: fmt.Errorf("action is notify, stop or block, not %q", p.Action)}
	}
	if p.Threshold < 0 {
		return protocol.WatchdogSetResult{}, nil, protocol.ErrBadParams{Err: fmt.Errorf("threshold is %d — it must be 0 or more", p.Threshold)}
	}
	subject := strings.TrimSpace(p.SubjectID)

	// The subject: a workspace rule's subject is the literal "workspace", a
	// component rule's subject must be a live component (a root's rule covers
	// its tree). A subject that names nothing real is a refusal before any
	// write.
	if p.Scope == "workspace" {
		if subject != "workspace" {
			detail := "a workspace rule's subject is the literal 'workspace'"
			if subject != "" {
				detail = fmt.Sprintf("a workspace rule's subject is the literal 'workspace', not %q", subject)
			}
			return protocol.WatchdogSetResult{}, &protocol.WatchdogRefusal{Kind: "workspace-subject", Detail: detail}, nil
		}
	} else {
		c, err := s.d.store.ReadComponent(subject)
		if err != nil {
			detail := fmt.Sprintf("no component %s is live — the rule would watch nothing", subject)
			if !errors.Is(err, store.ErrNotFound) {
				return protocol.WatchdogSetResult{}, nil, fmt.Errorf("daemon: read the component %s: %w", subject, err)
			}
			return protocol.WatchdogSetResult{}, &protocol.WatchdogRefusal{Kind: "unknown-subject", Detail: detail}, nil
		}
		if c.ArchivedAt != "" {
			return protocol.WatchdogSetResult{}, &protocol.WatchdogRefusal{Kind: "unknown-subject", Detail: fmt.Sprintf("component %s is archived — its rules would watch nothing", subject)}, nil
		}
	}

	// The signal's availability: a rule on a signal whose source is absent in
	// this store is refused, not silently inert (P31's honest third state —
	// the vocabulary says available:false, and the door agrees).
	avail, err := s.d.store.WatchdogSignalAvailability()
	if err != nil {
		return protocol.WatchdogSetResult{}, nil, fmt.Errorf("daemon: the signal availability: %w", err)
	}
	if a, ok := avail[p.Signal]; !ok || !a.Available {
		detail := fmt.Sprintf("the signal %s is not available in this store — a rule on it would be silently inert", p.Signal)
		if a.Reason != "" {
			detail += ": " + a.Reason
		}
		return protocol.WatchdogSetResult{}, &protocol.WatchdogRefusal{Kind: "unavailable-signal", Detail: detail}, nil
	}

	kind, id := watchdogDoorActorKind(actor.Kind), actor.ID

	// The key: an existing row flips enabled, an absent one creates.
	cur, found, err := s.d.store.FindWatchdogRule(p.Scope, subject, p.Signal, p.Action, p.Threshold)
	if err != nil {
		return protocol.WatchdogSetResult{}, nil, fmt.Errorf("daemon: find the watchdog rule %s/%s %s %s %d: %w",
			p.Scope, subject, p.Signal, p.Action, p.Threshold, err)
	}

	if found {
		// An existing key: the only editable field is enabled.
		target := cur.Enabled
		if p.Enabled != nil {
			target = *p.Enabled
		}
		if target == cur.Enabled {
			// Nothing differs: a no-op set writes no audit row (LC11).
			view, err := s.watchdogRuleView(cur)
			if err != nil {
				return protocol.WatchdogSetResult{}, nil, err
			}
			return protocol.WatchdogSetResult{View: &view, Effect: "noop", DryRun: p.DryRun}, nil, nil
		}
		if target {
			// A re-enable restores the default state: the tightening
			// direction, free.
			updated, err := s.d.store.WatchdogSetEnabled(cur.ID, true, kind, id)
			if err != nil {
				return protocol.WatchdogSetResult{}, nil, fmt.Errorf("daemon: enable the watchdog rule %s: %w", cur.ID, err)
			}
			view, err := s.watchdogRuleView(updated)
			if err != nil {
				return protocol.WatchdogSetResult{}, nil, err
			}
			return protocol.WatchdogSetResult{View: &view, Effect: "updated"}, nil, nil
		}
		// A disable loosens a safeguard: the confirmation the PM7.1
		// pattern demands.
		statement := disableStatement(cur)
		digest := watchdogDigest(statement)
		if p.DryRun {
			return protocol.WatchdogSetResult{Effect: "updated", DryRun: true, Statement: statement, Digest: digest}, nil, nil
		}
		if p.Confirm == "" {
			return protocol.WatchdogSetResult{}, &protocol.WatchdogRefusal{
				Kind:      "confirm-required",
				Detail:    "disabling a rule loosens a safeguard (it will no longer fire) — commit it with the digest of the statement as read",
				Statement: statement, Digest: digest,
			}, nil
		}
		if p.Confirm != digest {
			return protocol.WatchdogSetResult{}, &protocol.WatchdogRefusal{
				Kind:      "confirm-mismatch",
				Detail:    "the digest does not match the statement rendered from the rule as it is now (it moved since you read it) — read it again and commit with the new digest",
				Statement: statement, Digest: digest,
			}, nil
		}
		updated, err := s.d.store.WatchdogSetEnabled(cur.ID, false, kind, id)
		if err != nil {
			return protocol.WatchdogSetResult{}, nil, fmt.Errorf("daemon: disable the watchdog rule %s: %w", cur.ID, err)
		}
		view, err := s.watchdogRuleView(updated)
		if err != nil {
			return protocol.WatchdogSetResult{}, nil, err
		}
		return protocol.WatchdogSetResult{View: &view, Effect: "updated", Statement: statement, Digest: digest, Confirmed: true}, nil, nil
	}

	// A create: one threshold per (scope, subject, signal, action) — a
	// sibling at another threshold is a duplicate refusal, because two
	// thresholds on the same combination is a rule the operator did not
	// choose between. A threshold change is a removal (confirmed) plus a
	// fresh create (free).
	rules, err := s.d.store.ListWatchdogRules()
	if err != nil {
		return protocol.WatchdogSetResult{}, nil, fmt.Errorf("daemon: list the watchdog rules: %w", err)
	}
	for _, r := range rules {
		if r.Scope == p.Scope && r.SubjectID == subject && r.Signal == p.Signal && r.Action == p.Action && r.Threshold != p.Threshold {
			return protocol.WatchdogSetResult{}, &protocol.WatchdogRefusal{
				Kind:   "duplicate",
				Detail: fmt.Sprintf("a rule for %s already exists at threshold %s (id %s) — one threshold per combination: remove it (confirmed) and set the new one", ruleKey(r), thresholdHuman(r.Signal, r.Threshold), r.ID),
			}, nil
		}
	}

	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	if p.DryRun {
		return protocol.WatchdogSetResult{Effect: "created", DryRun: true}, nil, nil
	}
	created, err := s.d.store.WatchdogSet(store.WatchdogSetIn{
		Scope: p.Scope, SubjectID: subject, Signal: p.Signal,
		Threshold: p.Threshold, Action: p.Action, Enabled: &enabled,
		Origin: "user", ActorKind: kind, ActorID: id,
	})
	if err != nil {
		return protocol.WatchdogSetResult{}, nil, fmt.Errorf("daemon: create the watchdog rule %s/%s %s %s %d: %w",
			p.Scope, subject, p.Signal, p.Action, p.Threshold, err)
	}
	view, err := s.watchdogRuleView(created)
	if err != nil {
		return protocol.WatchdogSetResult{}, nil, err
	}
	return protocol.WatchdogSetResult{View: &view, Effect: "created"}, nil, nil
}

// SetEnabled is watchdog.setEnabled's human door: a single-field move of one
// rule. Disabling (true→false) loosens and confirms; enabling (false→true)
// tightens and is free; a set that changes nothing is a noop that writes no
// audit row (LC11).
func (s *watchdogService) SetEnabled(ctx context.Context, actor protocol.Actor, id string, enabled bool, dryRun bool, confirm string) (protocol.WatchdogSetEnabledResult, *protocol.WatchdogRefusal, error) {
	cur, err := s.d.store.GetWatchdogRule(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.WatchdogSetEnabledResult{}, nil, unknownRuleError("setEnabled", id)
		}
		return protocol.WatchdogSetEnabledResult{}, nil, fmt.Errorf("daemon: get the watchdog rule %s: %w", id, err)
	}
	if cur.Enabled == enabled {
		view, err := s.watchdogRuleView(cur)
		if err != nil {
			return protocol.WatchdogSetEnabledResult{}, nil, err
		}
		return protocol.WatchdogSetEnabledResult{View: &view, Effect: "noop", DryRun: dryRun}, nil, nil
	}
	kind, actorID := watchdogDoorActorKind(actor.Kind), actor.ID
	if !enabled {
		// A disable loosens a safeguard: the confirmation.
		statement := disableStatement(cur)
		digest := watchdogDigest(statement)
		if dryRun {
			return protocol.WatchdogSetEnabledResult{Effect: "disabled", DryRun: true, Statement: statement, Digest: digest}, nil, nil
		}
		if confirm == "" {
			return protocol.WatchdogSetEnabledResult{}, &protocol.WatchdogRefusal{
				Kind:      "confirm-required",
				Detail:    "disabling a rule loosens a safeguard (it will no longer fire) — commit it with the digest of the statement as read",
				Statement: statement, Digest: digest,
			}, nil
		}
		if confirm != digest {
			return protocol.WatchdogSetEnabledResult{}, &protocol.WatchdogRefusal{
				Kind:      "confirm-mismatch",
				Detail:    "the digest does not match the statement rendered from the rule as it is now (it moved since you read it) — read it again and commit with the new digest",
				Statement: statement, Digest: digest,
			}, nil
		}
		updated, err := s.d.store.WatchdogSetEnabled(id, false, kind, actorID)
		if err != nil {
			return protocol.WatchdogSetEnabledResult{}, nil, fmt.Errorf("daemon: disable the watchdog rule %s: %w", id, err)
		}
		view, err := s.watchdogRuleView(updated)
		if err != nil {
			return protocol.WatchdogSetEnabledResult{}, nil, err
		}
		return protocol.WatchdogSetEnabledResult{View: &view, Effect: "disabled", Statement: statement, Digest: digest, Confirmed: true}, nil, nil
	}
	// An enable restores the default state: the tightening direction, free.
	updated, err := s.d.store.WatchdogSetEnabled(id, true, kind, actorID)
	if err != nil {
		return protocol.WatchdogSetEnabledResult{}, nil, fmt.Errorf("daemon: enable the watchdog rule %s: %w", id, err)
	}
	view, err := s.watchdogRuleView(updated)
	if err != nil {
		return protocol.WatchdogSetEnabledResult{}, nil, err
	}
	return protocol.WatchdogSetEnabledResult{View: &view, Effect: "enabled"}, nil, nil
}

// RemoveRule is watchdog.remove's human door (the seam's name changed from
// Remove in the 2026-11-02 merge reconciliation: S45's boundary guards grep
// for the policy boundary's Remove call shape — a dot-Remove receiver whose
// first argument is a bare ctx — against a closed whitelist, and this seam
// is S55's — the interface comment in internal/protocol/watchdog.go). A
// removal always loosens a safeguard (a rule that is gone will never fire),
// so it always confirms, any origin — the system seed's provenance does not
// make its removal free. The row is disabled, never deleted (the audit and
// the id outlive it, so the seed's ledger and a fire's rule_id keep
// resolving).
func (s *watchdogService) RemoveRule(ctx context.Context, actor protocol.Actor, id, confirm string) (protocol.WatchdogRemoveResult, *protocol.WatchdogRefusal, error) {
	cur, err := s.d.store.GetWatchdogRule(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.WatchdogRemoveResult{}, nil, unknownRuleError("remove", id)
		}
		return protocol.WatchdogRemoveResult{}, nil, fmt.Errorf("daemon: get the watchdog rule %s: %w", id, err)
	}
	statement := watchdogRemoveStatement(cur)
	digest := watchdogDigest(statement)
	if confirm == "" {
		return protocol.WatchdogRemoveResult{}, &protocol.WatchdogRefusal{
			Kind:      "confirm-required",
			Detail:    "removing a rule loosens a safeguard (it will never fire) — commit it with the digest of the statement as read",
			Statement: statement, Digest: digest,
		}, nil
	}
	if confirm != digest {
		return protocol.WatchdogRemoveResult{}, &protocol.WatchdogRefusal{
			Kind:      "confirm-mismatch",
			Detail:    "the digest does not match the statement rendered from the rule as it is now (it moved since you read it) — read it again and remove with the new digest",
			Statement: statement, Digest: digest,
		}, nil
	}
	kind, actorID := watchdogDoorActorKind(actor.Kind), actor.ID
	if _, err := s.d.store.WatchdogRemove(id, kind, actorID); err != nil {
		return protocol.WatchdogRemoveResult{}, nil, fmt.Errorf("daemon: remove the watchdog rule %s: %w", id, err)
	}
	return protocol.WatchdogRemoveResult{ID: id, Removed: true}, nil, nil
}

// Vocabulary is watchdog.vocabulary's read: the closed sets the forms and the
// CLI build from (rule 4), the signals WITH their unit and this store's
// availability (P31: available:false is an answer, not an omission). The
// closed sets come from internal/watchdog (the one Go definition the
// evaluator uses), so the vocabulary and the decision cannot drift.
func (s *watchdogService) Vocabulary(ctx context.Context) (protocol.WatchdogVocabularyResult, error) {
	avail, err := s.d.store.WatchdogSignalAvailability()
	if err != nil {
		return protocol.WatchdogVocabularyResult{}, fmt.Errorf("daemon: the signal availability: %w", err)
	}
	scopes := make([]string, 0, len(watchdog.Scopes))
	for _, sc := range watchdog.Scopes {
		scopes = append(scopes, string(sc))
	}
	signals := make([]protocol.WatchdogSignalVocab, 0, len(watchdog.Signals))
	for _, sig := range watchdog.Signals {
		unit := "count"
		if sig == watchdog.SignalNoActivity || sig == watchdog.SignalStepOverDeadline {
			unit = "ms"
		}
		a, ok := avail[string(sig)]
		signals = append(signals, protocol.WatchdogSignalVocab{
			Signal: string(sig), Unit: unit,
			Available: ok && a.Available,
			Reason:    a.Reason,
		})
	}
	actions := make([]string, 0, len(watchdog.Actions))
	for _, a := range watchdog.Actions {
		actions = append(actions, string(a))
	}
	return protocol.WatchdogVocabularyResult{
		Scopes:       scopes,
		Signals:      signals,
		Actions:      actions,
		EventKinds:   []string{"created", "changed", "removed", "disabled"},
		RefusalKinds: []string{"unknown-rule", "duplicate", "unavailable-signal", "confirm-required", "confirm-mismatch", "workspace-subject", "unknown-subject"},
	}, nil
}

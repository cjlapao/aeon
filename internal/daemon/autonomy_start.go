package daemon

// autonomy_start.go — S56 AL8: the start path's one consultation of the
// root's grants. resolveAutonomy is the ONLY place a start reads the autonomy
// store; the result enters the loop only through startExtras (never the wire).
// It is called from the schedule, webhook and event origins (and, when S27
// lands, the scheduler) — the narrow, person and queue/roadmap starts never
// call it. A start that finds no applicable allow writes no pin row (byte-
// identical to today); a start that cannot read the policy refuses (the next
// tick retries), it never falls through and starts an unpinned loop.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/store"
)

// pinEntry is the store's pinned-allowed shape (store/autonomy.go's
// pinnedEntry): the action name it covers and the grant behind it. The store
// reads the pin as this; the action-time conditions are re-read from the grant
// by the decision site (T5), not carried here.
type pinEntry struct {
	Grant   string `json:"grant"`
	Subject string `json:"subject"`
}

// resolveAutonomy consults the root's grants once, at pin time, through the
// pure Plan, and returns the pin the loop will carry — or nil when no grant
// applies (byte-identical to today: no pin row). A store read error is
// returned and the start refuses with a typed sentence (fail-toward-deny): a
// loop that started without the pin would be both less permissive than the
// policy intended and a silent hide of a broken store from the person
// running a scheduler.
func (d *Daemon) resolveAutonomy(ctx context.Context, origin autonomy.Origin, componentRoot string, labels []string) (*store.AutonomyPin, error) {
	// 1. the component for the component root, then the root of its tree. An
	//    archived component is not this function's refusal — startLoopWith
	//    already refuses work on an archived root; here we only need the tree.
	//    A workspace that registers no component (or whose root cannot be
	//    resolved) has no root, no grants and nothing to pin: that is a
	//    no-grant, byte-identical to today, NOT a broken policy. Only a
	//    genuine read failure — the component exists but the store could not
	//    read it — refuses below (AL8: a start that cannot read the policy
	//    refuses, the next tick retries; a start with no grants writes no
	//    pin row and starts exactly as today).
	comp, err := d.store.ComponentForRoot(componentRoot)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("the autonomy policy for %s could not be read: %w", componentRoot, err)
	}
	rootID, err := d.store.RootOf(comp.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("the autonomy policy for %s could not be read: %w", componentRoot, err)
	}
	// 2. the active grants for the root and its component (allows on the root,
	//    denies on either level). The filter returns root-level and child-
	//    level rows; the pure side decides which apply.
	rows, err := d.store.ListAutonomyGrants(store.AutonomyGrantFilter{RootID: rootID})
	if err != nil {
		return nil, fmt.Errorf("the autonomy policy for %s could not be read: %w", componentRoot, err)
	}
	rev, err := d.store.AutonomyRev(rootID)
	if err != nil {
		return nil, fmt.Errorf("the autonomy policy for %s could not be read: %w", componentRoot, err)
	}
	grants, err := grantsToAutonomy(rows)
	if err != nil {
		return nil, fmt.Errorf("the autonomy policy for %s could not be read: %w", componentRoot, err)
	}
	// 3. the pure plan over the registry, at pin time (now for expiry). The
	//    action-time conditions are carried in the pin and evaluated by the
	//    decision site (T5), so no seam is consulted here.
	plan := autonomy.Plan(autonomy.PlanInput{
		Origin: origin,
		Labels: labels,
		Grants: grants,
		Now:    time.Now(),
	})
	// 4. the pin's allowed snapshot (only the covered actions).
	allowed := autonomy.PinFrom(plan)
	if len(allowed) == 0 {
		// No applicable allow: no pin row, byte-identical to today.
		return nil, nil
	}
	// 5. assemble the store pin. The allowed JSON is the store's
	//    [{grant, subject}] shape; the action-time conditions are re-read from
	//    the grant by the decision site (T5), not carried here.
	entries := make([]pinEntry, 0, len(allowed))
	for _, pg := range allowed {
		entries = append(entries, pinEntry{Grant: pg.GrantID, Subject: pg.Subject})
	}
	allowedJSON, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	if labels == nil {
		labels = []string{}
	}
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return nil, err
	}
	return &store.AutonomyPin{
		RootID:  rootID,
		Rev:     rev,
		Origin:  string(origin),
		Labels:  string(labelsJSON),
		Allowed: string(allowedJSON),
	}, nil
}

// grantsToAutonomy bridges the store's grant rows to the pure Grant the Plan
// decides with. The daemon is the bridge (AL10): the store owns the SQL, the
// pure package owns the decision. A time the store cannot render is refused,
// not guessed — an unparseable expiry is a broken store, never a "no expiry".
func grantsToAutonomy(rows []store.AutonomyGrant) ([]autonomy.Grant, error) {
	out := make([]autonomy.Grant, len(rows))
	for i, r := range rows {
		g := autonomy.Grant{
			ID:         r.ID,
			NodeID:     r.NodeID,
			Subject:    autonomy.Subject{Kind: r.SubjectKind, Name: r.Subject},
			State:      autonomy.State(r.State),
			Note:       r.Note,
			ProposedBy: r.ProposedBy,
		}
		if r.Conditions != "" {
			if err := json.Unmarshal([]byte(r.Conditions), &g.Conditions); err != nil {
				return nil, fmt.Errorf("grant %s conditions: %w", r.ID, err)
			}
		}
		if r.ExpiresAt != nil {
			t, err := parseStoreTime(*r.ExpiresAt)
			if err != nil {
				return nil, err
			}
			g.ExpiresAt = &t
		}
		if r.RevokedAt != nil {
			t, err := parseStoreTime(*r.RevokedAt)
			if err != nil {
				return nil, err
			}
			g.RevokedAt = &t
		}
		out[i] = g
	}
	return out, nil
}

// parseStoreTime reads a stored RFC3339 (the store's isoUTC form). A value
// that does not parse is a broken store, not a missing time.
func parseStoreTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("stored time %q is not RFC3339: %w", s, err)
	}
	return t, nil
}

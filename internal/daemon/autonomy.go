package daemon

// autonomy.go — S56 T6 (AL10/AL11): the daemon's autonomy service, the ONE
// layer that may know both the store's SQL rows and the pure
// internal/autonomy core. The socket handlers (protocol/autonomy.go) speak
// this package's wire twins; the daemon maps store row → GrantView, pure
// Refusal → protocol.AutonomyRefusal, and renders the two daemon-only
// sentences (the grant's `describe` and the write confirmations) — the one
// formatter for each (AL17: a second formatter is a second truth).
//
// Reads go through the SAME pure Plan the start path pins with (AL14: one
// resolver, the legibility view cannot drift from enforcement). The
// effective view uses ZERO seams, so a condition it cannot ask answers
// "cannot be asked" rather than a confident pass — the view says what a
// start would actually pin, not what the daemon hopes.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// NewAutonomyService builds the autonomy seam over the store. A nil store
// (a storeless daemon, V13) returns nil, so RegisterAutonomy leaves the ten
// autonomy.* methods ABSENT, not registered-and-refusing — mirroring the
// settings and seed wiring.
func NewAutonomyService(s *store.Store) protocol.AutonomyService {
	if s == nil {
		return nil
	}
	return &autonomyService{store: s}
}

type autonomyService struct {
	store *store.Store
}

// --- the writes -------------------------------------------------------------

func (a *autonomyService) Grant(ctx context.Context, actor protocol.Actor, in protocol.AutonomyGrantRequest) (protocol.AutonomyWriteOutcome, *protocol.AutonomyRefusal, error) {
	var zero protocol.AutonomyWriteOutcome
	// The node door (the store's is the backstop): the node must exist, and
	// an allow must sit on a ROOT — a child is refused naming its parent
	// root, which is the actionable half the store's sentence omits.
	c, err := a.store.ReadComponent(in.NodeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return zero, &protocol.AutonomyRefusal{Code: "unknown-node",
				Sentence: fmt.Sprintf("refused: %s is not a component — a grant needs a node it can sit on", in.NodeID)}, nil
		}
		return zero, nil, err
	}
	if c.ParentID != "" {
		return zero, &protocol.AutonomyRefusal{Code: "not-a-root",
			Sentence: fmt.Sprintf("refused: allow grants sit on a root component; %s is not a root — its root is %s (AL3)", in.NodeID, c.ParentID)}, nil
	}
	subject := subjectFor(in.Subject)
	g := autonomy.Grant{
		NodeID:     in.NodeID,
		Subject:    subject,
		State:      autonomy.StateAllow,
		Conditions: autonomy.Conditions{RequireGates: in.Conditions.RequireGates, RequireBudget: in.Conditions.RequireBudget, Labels: in.Conditions.Labels, MaxPerDay: in.Conditions.MaxPerDay},
		Note:       in.Note,
		ExpiresAt:  in.ExpiresAt,
	}
	if ref := autonomy.ValidateGrant(g, autonomy.NodeInfo{IsRoot: true, Archived: c.ArchivedAt != ""}, time.Now()); ref != nil {
		return zero, &protocol.AutonomyRefusal{Code: string(ref.Code), Sentence: ref.Sentence}, nil
	}
	if in.DryRun {
		// The confirmation names what the grant WOULD cover (T10): the
		// subject expanded to action names — the outward shorthand to its
		// current names (merge is never among them), an action to itself.
		covers := []string{in.Subject}
		if subject.Kind == "class" {
			covers = autonomyOutwardActions()
		}
		return protocol.AutonomyWriteOutcome{
			View:          nil,
			Confirmation:  grantDryRunConfirmation(in.NodeID, covers, in.Conditions, in.ExpiresAt),
			Changed:       false,
			ComponentRoot: c.Root,
		}, nil, nil
	}
	// The conditions JSON is marshalled from the PROTOCOL type: the pure
	// package imports nothing the wire knows, and the store's CHECK is
	// json_valid, so the canonical row and the wire stay one shape (I2).
	condsJSON, err := json.Marshal(in.Conditions)
	if err != nil {
		return zero, nil, fmt.Errorf("daemon: grant conditions: %w", err)
	}
	row, ref, err := a.store.CreateAutonomyGrant(store.Actor{Kind: actor.Kind, ID: actor.ID}, store.NewAutonomyGrant{
		NodeID:      in.NodeID,
		SubjectKind: subject.Kind,
		Subject:     subject.Name,
		State:       "allow",
		Conditions:  string(condsJSON),
		Note:        in.Note,
		ExpiresAt:   in.ExpiresAt,
	})
	if ref != nil {
		return zero, &protocol.AutonomyRefusal{Code: ref.Kind, Sentence: ref.Sentence}, nil
	}
	if err != nil {
		return zero, nil, err
	}
	view, err := a.grantView(row)
	if err != nil {
		return zero, nil, err
	}
	return protocol.AutonomyWriteOutcome{
		View:          &view,
		Confirmation:  grantConfirmation(view.NodeID, view.Subject.Name),
		Changed:       true,
		ComponentRoot: c.Root,
	}, nil, nil
}

func (a *autonomyService) Deny(ctx context.Context, actor protocol.Actor, in protocol.AutonomyDenyRequest) (protocol.AutonomyWriteOutcome, *protocol.AutonomyRefusal, error) {
	var zero protocol.AutonomyWriteOutcome
	c, err := a.store.ReadComponent(in.NodeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return zero, &protocol.AutonomyRefusal{Code: "unknown-node",
				Sentence: fmt.Sprintf("refused: %s is not a component — a veto needs a node it can sit on", in.NodeID)}, nil
		}
		return zero, nil, err
	}
	// A veto may sit on a root OR a child (OD4) — the node door stops at
	// existence. The pure validator decides the subject rules; it early-
	// returns for a deny before the bounds, so the note bound is checked
	// here (the daemon is the bridge, the bounds stay closed either way).
	subject := subjectFor(in.Subject)
	g := autonomy.Grant{NodeID: in.NodeID, Subject: subject, State: autonomy.StateDeny, Note: in.Note}
	if ref := autonomy.ValidateGrant(g, autonomy.NodeInfo{IsRoot: c.ParentID == "", Archived: c.ArchivedAt != ""}, time.Now()); ref != nil {
		return zero, &protocol.AutonomyRefusal{Code: string(ref.Code), Sentence: ref.Sentence}, nil
	}
	if len([]byte(in.Note)) > autonomy.LimitsValue().MaxNoteBytes {
		return zero, &protocol.AutonomyRefusal{Code: string(autonomy.CodeConditionInvalid),
			Sentence: fmt.Sprintf("a note may not exceed %d bytes", autonomy.LimitsValue().MaxNoteBytes)}, nil
	}
	if in.DryRun {
		return protocol.AutonomyWriteOutcome{
			View:          nil,
			Confirmation:  denyDryRunConfirmation(in.NodeID, in.Subject),
			Changed:       false,
			ComponentRoot: c.Root,
		}, nil, nil
	}
	row, ref, err := a.store.CreateAutonomyGrant(store.Actor{Kind: actor.Kind, ID: actor.ID}, store.NewAutonomyGrant{
		NodeID:      in.NodeID,
		SubjectKind: subject.Kind,
		Subject:     subject.Name,
		State:       "deny",
		Note:        in.Note,
	})
	if ref != nil {
		return zero, &protocol.AutonomyRefusal{Code: ref.Kind, Sentence: ref.Sentence}, nil
	}
	if err != nil {
		return zero, nil, err
	}
	view, err := a.grantView(row)
	if err != nil {
		return zero, nil, err
	}
	return protocol.AutonomyWriteOutcome{
		View:          &view,
		Confirmation:  denyConfirmation(view.NodeID, view.Subject.Name),
		Changed:       true,
		ComponentRoot: c.Root,
	}, nil, nil
}

func (a *autonomyService) Revoke(ctx context.Context, actor protocol.Actor, in protocol.AutonomyRevokeRequest) (protocol.AutonomyWriteOutcome, *protocol.AutonomyRefusal, error) {
	var zero protocol.AutonomyWriteOutcome
	// The reason door: a revocation without one cannot be audited, and the
	// bound is the closed set's (the handler checks presence; the seam
	// checks both, so a second caller cannot skip the audit half).
	if strings.TrimSpace(in.Reason) == "" {
		return zero, &protocol.AutonomyRefusal{Code: "reason-required",
			Sentence: "refused: a revoke needs a reason — an audited revocation without one cannot be explained"}, nil
	}
	if len([]byte(in.Reason)) > autonomy.LimitsValue().MaxReasonBytes {
		return zero, &protocol.AutonomyRefusal{Code: "reason-too-long",
			Sentence: fmt.Sprintf("a reason may not exceed %d bytes", autonomy.LimitsValue().MaxReasonBytes)}, nil
	}
	g, err := a.store.ReadAutonomyGrant(in.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return zero, &protocol.AutonomyRefusal{Code: "unknown-grant",
				Sentence: fmt.Sprintf("refused: %s is not an autonomy grant — it cannot be revoked", in.ID)}, nil
		}
		return zero, nil, err
	}
	c, err := a.store.ReadComponent(g.NodeID)
	if err != nil {
		return zero, nil, err
	}
	wasRevoked := g.RevokedAt != nil
	if in.DryRun {
		affected := []string{}
		if !wasRevoked {
			affected, err = a.store.PinnedLoopsForGrant(in.ID)
			if err != nil {
				return zero, nil, err
			}
			if affected == nil {
				affected = []string{}
			}
		}
		return protocol.AutonomyWriteOutcome{
			View:          nil,
			Confirmation:  revokeDryRunConfirmation(g, len(affected)),
			Affected:      affected,
			Changed:       false,
			ComponentRoot: c.Root,
		}, nil, nil
	}
	row, affected, ref, err := a.store.RevokeAutonomyGrant(store.Actor{Kind: actor.Kind, ID: actor.ID}, in.ID, in.Reason)
	if ref != nil {
		return zero, &protocol.AutonomyRefusal{Code: ref.Kind, Sentence: ref.Sentence}, nil
	}
	if err != nil {
		return zero, nil, err
	}
	if affected == nil {
		affected = []string{}
	}
	view, err := a.grantView(row)
	if err != nil {
		return zero, nil, err
	}
	return protocol.AutonomyWriteOutcome{
		View:          &view,
		Confirmation:  revokeConfirmation(row, len(affected)),
		Affected:      affected,
		Changed:       !wasRevoked,
		ComponentRoot: c.Root,
	}, nil, nil
}

// --- the reads --------------------------------------------------------------

func (a *autonomyService) List(ctx context.Context, rootID, state string, includeInactive bool) ([]protocol.GrantView, error) {
	rows, err := a.store.ListAutonomyGrants(store.AutonomyGrantFilter{
		RootID: rootID, State: state, IncludeInactive: includeInactive,
	})
	if err != nil {
		return nil, err
	}
	views := make([]protocol.GrantView, 0, len(rows))
	for _, r := range rows {
		v, err := a.grantView(r)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, nil
}

func (a *autonomyService) Show(ctx context.Context, id string) (protocol.GrantView, *protocol.ChangeView, int, []string, *protocol.AutonomyRefusal, error) {
	var zero protocol.GrantView
	g, err := a.store.ReadAutonomyGrant(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return zero, nil, 0, nil, &protocol.AutonomyRefusal{Code: "unknown-grant",
				Sentence: fmt.Sprintf("refused: %s is not an autonomy grant — name a grant id (autonomy.list lists them)", id)}, nil
		}
		return zero, nil, 0, nil, nil, err
	}
	changes, err := a.store.AutonomyChanges(store.AutonomyChangeFilter{GrantID: id})
	if err != nil {
		return zero, nil, 0, nil, nil, err
	}
	var last *protocol.ChangeView
	if len(changes) > 0 {
		cv := changeView(changes[len(changes)-1])
		last = &cv
	}
	// usedCount: the begun rows (one begun = one effect; a completed/failed
	// row is its outcome, not a new effect — AL10).
	uses, err := a.store.AutonomyUses(store.AutonomyUseFilter{GrantID: id})
	if err != nil {
		return zero, nil, 0, nil, nil, err
	}
	usedCount := 0
	for _, u := range uses {
		if u.Phase == "begun" {
			usedCount++
		}
	}
	pinned, err := a.store.PinnedLoopsForGrant(id)
	if err != nil {
		return zero, nil, 0, nil, nil, err
	}
	if pinned == nil {
		pinned = []string{}
	}
	view, err := a.grantView(g)
	if err != nil {
		return zero, nil, 0, nil, nil, err
	}
	return view, last, usedCount, pinned, nil, nil
}

func (a *autonomyService) Vocabulary() (protocol.VocabularyView, error) {
	v := autonomy.Vocabulary()
	out := protocol.VocabularyView{
		Limits: protocol.VocabularyLimits{
			MaxMergeGrantDays: v.Limits.MaxMergeGrantDays,
			MaxLabels:         v.Limits.MaxLabels,
			MaxLabelLen:       v.Limits.MaxLabelLen,
			MaxNoteBytes:      v.Limits.MaxNoteBytes,
			MaxReasonBytes:    v.Limits.MaxReasonBytes,
		},
	}
	for _, s := range v.Subjects {
		out.Subjects = append(out.Subjects, protocol.VocabularySubject{
			Kind: s.Kind, Name: s.Name, Class: string(s.Class), Label: s.Label,
			RequiresExpiry: s.RequiresExpiry, ForcedConditions: s.ForcedConditions, Covers: s.Covers,
		})
	}
	for _, c := range v.Conditions {
		out.Conditions = append(out.Conditions, protocol.VocabularyCondition{
			Name: c.Name, Type: c.Type, Description: c.Help, Evaluated: c.Evaluated, Forced: c.Forced,
		})
	}
	for _, o := range v.Origins {
		out.Origins = append(out.Origins, protocol.VocabularyOrigin{Origin: string(o.Origin), Help: o.Help})
	}
	for _, st := range v.States {
		out.States = append(out.States, string(st))
	}
	return out, nil
}

func (a *autonomyService) Effective(ctx context.Context, rootID, origin string, labels []string) (protocol.EffectiveView, *protocol.AutonomyRefusal, error) {
	var zero protocol.EffectiveView
	c, err := a.store.ReadComponent(rootID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return zero, &protocol.AutonomyRefusal{Code: "unknown-node",
				Sentence: fmt.Sprintf("refused: %s is not a component — an effective view needs a node it can describe", rootID)}, nil
		}
		return zero, nil, err
	}
	if c.ParentID != "" {
		return zero, &protocol.AutonomyRefusal{Code: "not-a-root",
			Sentence: fmt.Sprintf("refused: the effective view is over a root; %s is not a root — its root is %s (AL3)", rootID, c.ParentID)}, nil
	}
	rows, err := a.store.ListAutonomyGrants(store.AutonomyGrantFilter{RootID: rootID})
	if err != nil {
		return zero, nil, err
	}
	grants, err := grantsToAutonomy(rows)
	if err != nil {
		return zero, nil, err
	}
	// The SAME pure Plan the start path pins with, ZERO seams (AL14): the
	// view says what a start would actually pin, and a condition it cannot
	// ask answers cannot-say rather than a confident pass.
	now := time.Now()
	plan := autonomy.Plan(autonomy.PlanInput{
		Origin: autonomy.Origin(origin), Labels: labels, Grants: grants, Now: now,
	})
	actions := make([]protocol.EffectiveAction, 0, len(plan))
	var running []string
	for _, ap := range plan {
		ea := protocol.EffectiveAction{
			Action: ap.Action, Class: string(ap.Class), Verdict: string(ap.Verdict),
			Because: ap.Because, GrantID: ap.GrantID,
		}
		for _, cs := range ap.Status {
			ea.Conditions = append(ea.Conditions, protocol.EffectiveCond{
				Name: cs.Name, State: string(cs.State), Sentence: cs.Sentence,
			})
		}
		actions = append(actions, ea)
		if ap.Verdict == action.VerdictRun && ap.Class != action.EffectLocal {
			running = append(running, fmt.Sprintf("%s (grant %s)", ap.Action, ap.GrantID))
		}
	}
	statement := autonomy.NoAutonomyStatement
	if len(running) > 0 {
		statement = fmt.Sprintf("an unattended loop started now may take: %s", strings.Join(running, ", "))
	}
	return protocol.EffectiveView{
		RootID: rootID, Origin: origin, EvaluatedAt: now.UTC().Format(time.RFC3339),
		Actions: actions, Statement: statement,
	}, nil, nil
}

func (a *autonomyService) Pinned(ctx context.Context, loopID string) (protocol.PinnedView, *protocol.AutonomyRefusal, error) {
	var zero protocol.PinnedView
	// An unknown loop is a refusal, not a "no pin": a fabricated id reading
	// as "started with no autonomy" would be a confident wrong answer.
	if _, err := a.store.ReadLoopByID(loopID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return zero, &protocol.AutonomyRefusal{Code: "unknown-loop",
				Sentence: fmt.Sprintf("refused: %s is not a loop — it has no pin to show", loopID)}, nil
		}
		return zero, nil, err
	}
	pin, ok, err := a.store.ReadAutonomyPin(loopID)
	if err != nil {
		return zero, nil, err
	}
	if !ok {
		return protocol.PinnedView{
			LoopID:    loopID,
			Pinned:    false,
			Statement: "this loop was started with no autonomy — unattended loops run local actions only",
		}, nil, nil
	}
	var labels []string
	if err := json.Unmarshal([]byte(pin.Labels), &labels); err != nil {
		return zero, nil, fmt.Errorf("daemon: pin labels for loop %s: %w", loopID, err)
	}
	var entries []pinEntry
	if err := json.Unmarshal([]byte(pin.Allowed), &entries); err != nil {
		return zero, nil, fmt.Errorf("daemon: pin allowed for loop %s: %w", loopID, err)
	}
	now := time.Now()
	// Each pin entry: the grant's LIVE status, the same sentences the
	// decision site speaks (AL6/AL17 — one truth, panel and loop).
	grants := make([]protocol.PinnedGrantView, 0, len(entries))
	for _, e := range entries {
		g, err := a.store.ReadAutonomyGrant(e.Grant)
		if err != nil {
			return zero, nil, err
		}
		bridged, err := grantsToAutonomy([]store.AutonomyGrant{g})
		if err != nil {
			return zero, nil, err
		}
		bg := bridged[0]
		if g.RevokedByKind != nil {
			// The shared start-path bridge leaves RevokedBy empty (it never
			// renders a revoked sentence); the live view names who revoked.
			bg.RevokedBy = *g.RevokedByKind
		}
		state, sentence := autonomy.LiveStateOf(bg, now)
		grants = append(grants, protocol.PinnedGrantView{
			GrantID: e.Grant, Subject: e.Subject,
			Live: protocol.LiveStatus{State: state, Sentence: sentence},
		})
	}
	// The verdict per action is built EXACTLY as loop/autonomy_site.go
	// builds it — the same LiveState facts, the same pure decision, zero
	// seams, pr 0 — so what the panel shows and what the loop is told are
	// one truth (AL14/AL17).
	outward := autonomyOutwardActions()
	classes := action.Classes()
	actions := make([]protocol.PinnedAction, 0, len(classes))
	for _, name := range action.Names() {
		view, err := a.store.AutonomyLive(ctx, loopID, name, outward, now)
		if err != nil {
			return zero, nil, err
		}
		live := action.LiveState{
			Vetoed: view.Vetoed, VetoID: view.VetoGrant,
			Expired:   view.Expired,
			GrantID:   view.CoveringGrant,
			Rev:       view.Rev,
		}
		if view.Covered {
			g, gerr := a.store.ReadAutonomyGrant(view.CoveringGrant)
			if gerr != nil {
				return zero, nil, gerr
			}
			if g.RevokedAt != nil {
				live.Revoked = true
				live.RevokedAt = *g.RevokedAt
			}
			if g.RevokedByKind != nil {
				live.RevokedBy = *g.RevokedByKind
			}
			bridged, berr := grantsToAutonomy([]store.AutonomyGrant{g})
			if berr != nil {
				return zero, nil, berr
			}
			live.Conditions = autonomy.Evaluate(ctx, bridged[0].Conditions, g.ID, loopID, 0, autonomy.Seams{}, now)
		}
		cr, err := action.ResolveWithGrant(name, false, false, action.GrantContext{
			Pinned: view.Pinned, InPin: view.Covered, Live: live,
		})
		if err != nil {
			return zero, nil, err
		}
		actions = append(actions, protocol.PinnedAction{
			Action: name, Verdict: string(cr.Verdict), Because: cr.Sentence,
		})
	}
	return protocol.PinnedView{
		LoopID: loopID, Pinned: true,
		RootID: pin.RootID, Rev: pin.Rev, Origin: pin.Origin, Labels: labels,
		Grants: grants, Actions: actions,
	}, nil, nil
}

func (a *autonomyService) Changes(ctx context.Context, rootID, grantID string, limit int) ([]protocol.ChangeView, error) {
	rows, err := a.store.AutonomyChanges(store.AutonomyChangeFilter{
		RootID: rootID, GrantID: grantID, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	views := make([]protocol.ChangeView, 0, len(rows))
	for _, r := range rows {
		views = append(views, changeView(r))
	}
	return views, nil
}

func (a *autonomyService) Uses(ctx context.Context, rootID, grantID, loopID string, limit int) ([]protocol.UseView, error) {
	// The FULL filtered set, no limit: pairing needs every completed/failed
	// row, so the limit applies to the rendered list (the newest N) after.
	rows, err := a.store.AutonomyUses(store.AutonomyUseFilter{
		RootID: rootID, GrantID: grantID, LoopID: loopID,
	})
	if err != nil {
		return nil, err
	}
	// begun id → the outcome that closed it (AL10's receipt-first shape).
	outcome := map[int64]string{}
	for _, r := range rows {
		if (r.Phase == "completed" || r.Phase == "failed") && r.BegunID != nil {
			outcome[*r.BegunID] = r.Phase
		}
	}
	views := make([]protocol.UseView, 0, len(rows))
	for _, r := range rows {
		uv := protocol.UseView{
			ID: r.ID, At: r.At, LoopID: r.LoopID, GrantID: r.GrantID,
			GrantRev: r.GrantRev, Action: r.Action, Phase: r.Phase,
			BegunID: r.BegunID,
		}
		if r.Phase == "begun" {
			if ph, ok := outcome[r.ID]; ok {
				uv.Outcome = &ph
			} else {
				unknown := protocol.UseOutcomeUnknown
				uv.Outcome = &unknown
			}
		}
		// A completed/failed row is an outcome, not a use — its outcome is
		// null on the wire (the *string stays nil).
		views = append(views, uv)
	}
	if limit > 0 && len(views) > limit {
		views = views[len(views)-limit:] // the newest N, ascending
	}
	return views, nil
}

// --- the mapping ------------------------------------------------------------

// grantView maps one store row to the wire view, and renders the ONE
// describe sentence (AL17). RootID is the node's root (a grant on a root is
// its own root; a veto on a child is the parent's).
func (a *autonomyService) grantView(g store.AutonomyGrant) (protocol.GrantView, error) {
	rootID, err := a.store.RootOf(g.NodeID)
	if err != nil {
		return protocol.GrantView{}, err
	}
	conds := protocol.AutonomyConditions{}
	if g.Conditions != "" {
		if err := json.Unmarshal([]byte(g.Conditions), &conds); err != nil {
			return protocol.GrantView{}, fmt.Errorf("daemon: grant %s conditions: %w", g.ID, err)
		}
	}
	v := protocol.GrantView{
		ID:         g.ID,
		NodeID:     g.NodeID,
		RootID:     rootID,
		Subject:    protocol.SubjectView{Kind: g.SubjectKind, Name: g.Subject},
		State:      g.State,
		Conditions: conds,
		Note:       g.Note,
		ExpiresAt:  g.ExpiresAt,
		CreatedAt:  g.CreatedAt,
		CreatedBy:  protocol.Actor{Kind: g.CreatedByKind, ID: g.CreatedByID},
		ProposedBy: g.ProposedBy,
		Active:     g.RevokedAt == nil,
	}
	if g.RevokedAt != nil {
		by := ""
		if g.RevokedByKind != nil {
			by = *g.RevokedByKind
		}
		if g.RevokedByID != nil {
			by += ":" + *g.RevokedByID
		}
		reason := ""
		if g.RevokedReason != nil {
			reason = *g.RevokedReason
		}
		v.Revoked = &protocol.RevokedView{At: *g.RevokedAt, By: by, Reason: reason}
	}
	v.Describe = describeGrant(v)
	return v, nil
}

// describeGrant is the ONE renderer of a grant's describe sentence (AL17): a
// fixed template over ids, enums and stored times. No client re-words it —
// TestDescribeIsOneFormatter greps the client surface (ui/ and cmd/) for the
// template's distinctive phrase, so a second formatter is a failing test,
// not a drift.
func describeGrant(v protocol.GrantView) string {
	verb := "vetoes"
	if v.State == "allow" {
		verb = "allows"
	}
	s := fmt.Sprintf("%s %s %s for unattended loops on %s", v.ID, verb, v.Subject.Name, v.NodeID)
	if v.Revoked != nil {
		s += fmt.Sprintf(" — revoked at %s by %s", v.Revoked.At, v.Revoked.By)
	}
	if v.ExpiresAt != nil {
		s += fmt.Sprintf(" — expires %s", *v.ExpiresAt)
	}
	return s
}

// The write confirmations: one fixed template per op, rendered ONLY here —
// the daemon's own sentence on every authority-granting action (P7), never a
// client's. The written forms are the T6 templates the conformance fixtures
// pin byte for byte; the dry-run forms say what the write WOULD do (T10
// behaviour 4): the grant's names the root, each action it would cover, the
// expiry, each condition in words, the S57 rule for a merge, and what it
// does NOT let — a grant that does not cover forge.merge says so, because
// merge is a separate decision.
func grantConfirmation(nodeID, subject string) string {
	return fmt.Sprintf("written: %s is now allowed for unattended loops on %s", subject, nodeID)
}

// grantDryRunConfirmation is the dry run's grant sentence (T10): the same
// facts a grant would carry, in plain words. Covers is the subject expanded
// to action names (the caller passes the outward expansion for the class
// shorthand); the labels come from the vocabulary's one accessor, so a form
// and a sentence never disagree about what an action is called (AL15).
func grantDryRunConfirmation(nodeID string, covers []string, conds protocol.AutonomyConditions, expiresAt *time.Time) string {
	covered := autonomy.SubjectLabel(covers[0])
	if len(covers) > 1 {
		phrases := make([]string, 0, len(covers))
		for _, n := range covers {
			phrases = append(phrases, autonomy.SubjectLabel(n))
		}
		covered = "every outward action: " + joinAnd(phrases)
	}
	// The conditions in words, in one fixed order: a form renders them from
	// the vocabulary, a sentence says them here — one place each.
	clauses := []string{}
	if conds.MaxPerDay > 0 {
		clauses = append(clauses, fmt.Sprintf("up to %d a day", conds.MaxPerDay))
	}
	if conds.RequireBudget {
		clauses = append(clauses, "while the budget has room")
	}
	if len(conds.Labels) > 0 {
		clauses = append(clauses, "only for starts labeled "+joinOr(conds.Labels))
	}
	if conds.RequireGates {
		clauses = append(clauses, "only when the merge rule (S57) passes")
	}
	if expiresAt != nil {
		clauses = append(clauses, "until "+expiresAt.UTC().Format(time.RFC3339))
	} else {
		clauses = append(clauses, "with no expiry")
	}
	s := fmt.Sprintf("dry run: this would let an unattended loop started on %s %s, %s — nothing was written",
		nodeID, covered, strings.Join(clauses, ", "))
	if !coversMerge(covers) {
		s += " It does not let it merge."
	}
	return s
}

func denyConfirmation(nodeID, subject string) string {
	return fmt.Sprintf("written: %s is now vetoed on %s — it cannot run unattended there", subject, nodeID)
}

func denyDryRunConfirmation(nodeID, subject string) string {
	return fmt.Sprintf("dry run: this would veto %s on %s — nothing was written", autonomy.SubjectLabel(subject), nodeID)
}

func revokeConfirmation(g store.AutonomyGrant, affected int) string {
	return fmt.Sprintf("revoked: %s (%s) no longer applies — %d running loop(s) were pinned to it", g.ID, g.Subject, affected)
}

func revokeDryRunConfirmation(g store.AutonomyGrant, affected int) string {
	return fmt.Sprintf("dry run: %s (%s) would stop applying — %d running loop(s) are pinned to it — nothing was written",
		g.ID, autonomy.SubjectLabel(g.Subject), affected)
}

func coversMerge(covers []string) bool {
	for _, c := range covers {
		if c == action.ForgeMerge {
			return true
		}
	}
	return false
}

func joinAnd(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

func joinOr(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1]
}

func changeView(c store.AutonomyChange) protocol.ChangeView {
	return protocol.ChangeView{
		ID: c.ID, At: c.At, RootID: c.RootID, GrantID: c.GrantID,
		ChangeKind: c.ChangeKind, ActorKind: c.ActorKind, ActorID: c.ActorID,
		ProposedBy: c.ProposedBy, Summary: c.Summary,
	}
}

// subjectFor derives the subject's kind from the wire's name: the one class
// shorthand is the class, everything else must be a registry action — the
// pure validator is the door on both sides (unknown/local refuse).
func subjectFor(name string) autonomy.Subject {
	if name == autonomy.ClassShorthand {
		return autonomy.ClassSubject()
	}
	return autonomy.ActionSubject(name)
}

// autonomyOutwardActions is the current set of outward action names, read
// from the registry — the expansion the store's veto check needs (AL2: the
// store never encodes the registry).
func autonomyOutwardActions() []string {
	classes := action.Classes()
	var out []string
	for _, name := range action.Names() {
		if classes[name] == action.EffectOutward {
			out = append(out, name)
		}
	}
	return out
}

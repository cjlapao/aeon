package daemon

// automation_health.go — S49 T3: the health INPUTS. The verdict itself is
// automationview.ComposeHealth's (T2's one composer, checks in order,
// reasons in the engine's own words); this file's whole job is to read the
// live facts the composer is handed and to read them the way the FIRE-TIME
// doors read them, so advice and engine can never hold different opinions
// about the same row (AN8).
//
// The four inputs and their honesty rules:
//
//   - blueprint: the store ROW, run through blueprint.Analyze with the ONE
//     options builder every S48 read uses (blueprintValidationOptions(""))
//     — the same call `blueprint.show` answers from, over the same row
//     `loop.start` pins from. missing = no row; invalid = an error-line
//     diagnostic; unverifiable = an unverifiable line and no error (the
//     honest third state, said out loud, never guessed to ok).
//   - secret: os.LookupEnv for the NAME's presence, through the injected
//     EnvSet func. The VALUE is never read into anything this file returns,
//     logs, or passes on (C17; T3's test sets a sentinel value and demands
//     its bytes absent from every answer).
//   - workspace: a stat — the directory the schedule would run in either is
//     there or is not; an unnamed workspace is nothing to be missing.
//   - scope: the event row's root/component still name live rows (the
//     component store's own reads; a deleted scope is said, not guessed).
//
// Health reads rows; it writes nothing and gates nothing — T3's
// TestHealthNeverGatesFiring fires an unhealthy trigger through the real
// engine and demands the engine's own refusal unchanged.

import (
	"context"

	"github.com/cjlapao/aeon/internal/automationview"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/protocol"
)

// blueprintState runs the ONE validator over the named row. A read that
// fails (store error) is unverifiable, not ok and not missing: the store
// said nothing provable, and saying so is the honest answer.
func (a *automationReads) blueprintState(ctx context.Context, name string) (state string, errs, warns int) {
	if name == "" {
		return automationview.BlueprintUnverifiable, 0, 0
	}
	row, ok, err := a.s.BlueprintGet(ctx, name)
	if err != nil {
		return automationview.BlueprintUnverifiable, 0, 0
	}
	if !ok {
		return automationview.BlueprintMissing, 0, 0
	}
	report := blueprint.Analyze([]byte(row.Content), blueprintValidationOptions(""))
	_, invalid, unverifiable := report.Counts()
	switch {
	case invalid > 0:
		return automationview.BlueprintInvalid, invalid, unverifiable
	case unverifiable > 0:
		return automationview.BlueprintUnverifiable, 0, unverifiable
	default:
		return automationview.BlueprintOK, 0, 0
	}
}

// scopeExists answers the event family's root/component against the
// component store. Empty names are not missing scopes — the row simply
// does not constrain that way. Injected ScopeExists (tests) overrides.
func (a *automationReads) scopeExists(ctx context.Context, rootID, componentID string) bool {
	if a.ScopeExists != nil {
		return a.ScopeExists(ctx, rootID, componentID)
	}
	if componentID != "" {
		if _, err := a.s.ComponentWriteFor(componentID); err != nil {
			return false
		}
		return true
	}
	if rootID != "" {
		if _, err := a.s.ComponentForRoot(rootID); err != nil {
			return false
		}
	}
	return true
}

// healthFor composes the verdicts for the given rows. The caller has
// already decided WHICH rows (one, a family, all) — this function turns a
// row into inputs and the composer into the wire view, in checks order
// (blueprint, secret, workspace, scope, switches — automationview owns the
// order and the wording; this file re-states neither).
func (a *automationReads) healthFor(ctx context.Context, rows []protocol.AutomationView) ([]protocol.AutomationHealthView, error) {
	out := make([]protocol.AutomationHealthView, 0, len(rows))
	for _, v := range rows {
		state, errs, warns := a.blueprintState(ctx, v.Blueprint)
		in := automationview.HealthInputs{
			BlueprintState:    state,
			BlueprintErrors:   errs,
			BlueprintWarnings: warns,
			WorkspaceExists:   a.DirExists(v.Workspace),
			ScopeExists:       a.scopeExists(ctx, v.RootID, v.ComponentID),
			Enabled:           v.Enabled,
			Paused:            v.Paused,
			Archived:          v.Archived,
			IsWebhook:         v.Source == "webhook",
		}
		if v.Source == "webhook" {
			// The NAME crosses (a person must be able to check the right
			// env is in play); the bool is its presence. The value is in
			// neither, by construction: EnvSet has no value to give. Set
			// BEFORE the compose — the composer's secret line reads it.
			in.SecretEnvName = v.SecretEnv
			in.SecretEnvSet = a.EnvSet(v.SecretEnv)
		}
		h := automationview.ComposeHealth(in)
		view := protocol.AutomationHealthView{
			Source:    v.Source,
			ID:        v.ID,
			Blueprint: protocol.AutomationBlueprintHealth{State: state, Errors: errs, Warnings: warns},
			Workspace: protocol.AutomationExists{Exists: in.WorkspaceExists},
			Scope:     protocol.AutomationExists{Exists: in.ScopeExists},
			CanFire:   h.CanFire,
			Reasons:   h.Reasons,
		}
		if view.Reasons == nil {
			view.Reasons = []string{}
		}
		if in.IsWebhook {
			view.Webhook = &protocol.AutomationSecretHealth{EnvName: in.SecretEnvName, Set: in.SecretEnvSet}
		}
		out = append(out, view)
	}
	return out, nil
}

// AutomationHealth is the seam: one row named, or the whole (unarchived)
// set, optionally narrowed to one family.
func (a *automationReads) AutomationHealth(ctx context.Context, p protocol.AutomationHealthParams) ([]protocol.AutomationHealthView, error) {
	if p.ID != "" {
		v, err := a.findRowByID(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		if p.Source != "" && v.Source != p.Source {
			return nil, protocol.ErrAutomationUnknown.WithWhat("automation %q is a %s, not the %s named — the source is part of its name", p.ID, v.Source, p.Source)
		}
		return a.healthFor(ctx, []protocol.AutomationView{v})
	}
	rows, err := a.allRows(ctx, false)
	if err != nil {
		return nil, err
	}
	if p.Source != "" {
		narrow := rows[:0:0]
		for _, v := range rows {
			if v.Source == p.Source {
				narrow = append(narrow, v)
			}
		}
		rows = narrow
	}
	return a.healthFor(ctx, rows)
}

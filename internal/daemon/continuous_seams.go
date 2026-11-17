package daemon

// continuous_seams.go — S27 T6: the cross-slice seams (CN24), filled with the
// calls the owning slices already expose. Nothing here invents policy: the
// budget answer is S55's, the grant resolution is S56's, the stop door is
// S55's loop.stop behind its daemon seam, and the attention notes are display
// strings. Each seam is one read into the owning slice plus the honest fact
// the plan consumes; the core package stays free of every one of them
// (TestSeamsAreLocalInterfaces).
//
// The one law this file exists to keep: the pin IS the permission (S56 AL8) —
// a scheduler start never moves the legacy allow_outward flag, for a pin is
// an addition to the loop, not a widening. The item's cap narrows the pin's
// allowed snapshot before it is pinned (CN3: an item may only narrow), and
// the decision row records grant, cap and effective in one clause so
// "which rule allowed this?" stays one read (CN16).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/store"
)

// pinEntry is autonomy_start.go's canonical pin snapshot shape
// ([{grant, subject}]). The store owns the column; this file only re-narrows
// the entries a cap forbids — it never adds one (widening is unrepresentable,
// CN3).
// seamFacts asks the two pre-Plan seams for one enabled repository. The
// budget answer is S55's start-time read at the repository's own ground
// (workspace→component→root; the epic's cap rides the start door, where
// startLoopWith consults it with the item's epic). GrantAbsent follows V13:
// it is the FACT that the policy could not be read — the read erroring is
// what the core must see and defer, so the next tick retries (AL8's shape).
// A root that reads cleanly and holds no grants is NOT absent: a
// grantless scheduler starts local-only, exactly the build-only machine the
// design promises.
func (a *continuousAdapter) seamFacts(ctx context.Context, root string) (budgetOK bool, budgetReason string, grantAbsent bool) {
	verdict, err := a.d.startBudgetVerdictFor(root, "")
	if err != nil {
		return false, fmt.Sprintf("the budgets over %s could not be read: %v", root, err), false
	}
	if !verdict.OK {
		reason := ""
		if len(verdict.Hits) > 0 {
			reason = verdict.Hits[0].Sentence()
		}
		if reason == "" {
			reason = "a budget reported exhaustion without naming itself"
		}
		return false, reason, false
	}
	if _, err := a.d.resolveAutonomy(ctx, autonomy.OriginScheduler, root, nil); err != nil {
		a.d.log.Infof("continuous: the autonomy policy over %s could not be read: %v", root, err)
		return true, "", true
	}
	return true, "", false
}

// itemCap reads the item's declared narrowing. The vocabulary is the T2
// CHECK: grant (the default, take what S56 grants), outward (at most outward,
// no merge), none (nothing outward). An unreadable cap is returned as an
// error and the caller treats the repository as seam-unreadable — a cap
// guessed is a permission guessed.
func (a *continuousAdapter) itemCap(subjectKind string, subjectID int64) (string, error) {
	cap, err := a.d.store.ItemAutonomyCap(subjectKind, subjectID)
	if err != nil {
		return "", err
	}
	return cap, nil
}

// narrowPin applies the item's cap to the grant's pin snapshot and answers
// the effective outward and merge facts alongside it. A grant is never
// widened: the filter drops entries and never reorders, rewrites or adds
// one. cap "none" pins nothing at all (the byte-identical no-pin shape is
// already the honest loop), cap "outward" drops the irreversible entries,
// and cap "grant" takes the pin as the grant gave it.
func narrowPin(pin *store.AutonomyPin, cap string) (narrowed *store.AutonomyPin, outward, merge bool) {
	if pin == nil {
		return nil, false, false
	}
	if cap == "" || cap == "grant" {
		return pin, pinAllowsAnything(pin.Allowed) && pinHasClass(pin.Allowed, false), pinHasClass(pin.Allowed, true)
	}
	var entries []pinEntry
	if err := json.Unmarshal([]byte(pin.Allowed), &entries); err != nil {
		// An unparsable snapshot is a broken store, not a licence: pin
		// nothing and let the loop start with no outward at all.
		return nil, false, false
	}
	kept := make([]pinEntry, 0, len(entries))
	for _, e := range entries {
		irreversible := action.Classes()[e.Subject] == action.EffectIrreversible
		if cap == "none" {
			continue
		}
		if cap == "outward" && irreversible {
			continue // at most outward — no merge (CN3's middle value).
		}
		if irreversible {
			merge = true
		} else if action.Classes()[e.Subject] == action.EffectOutward {
			outward = true
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		return nil, false, false
	}
	allowed, err := json.Marshal(kept)
	if err != nil {
		return nil, false, false
	}
	p := *pin
	p.Allowed = string(allowed)
	return &p, outward, merge
}

// pinHasClass answers whether any pinned entry lands on the irreversible
// class (merge true) or on anything beyond local (merge false — outward at
// all).
func pinHasClass(allowed string, irreversible bool) bool {
	var entries []pinEntry
	if err := json.Unmarshal([]byte(allowed), &entries); err != nil {
		return false
	}
	for _, e := range entries {
		c := action.Classes()[e.Subject]
		if irreversible && c == action.EffectIrreversible {
			return true
		}
		if !irreversible && c != "" && c != action.EffectLocal {
			return true
		}
	}
	return false
}

func pinAllowsAnything(allowed string) bool {
	return strings.TrimSpace(allowed) != "" && strings.TrimSpace(allowed) != "[]"
}

// grantClause is the decision row's permission sentence: one read answers
// which rule allowed this and how far the item narrowed it. It rides the
// started row's summary; ids and enums only (SEC-033-clean).
func grantClause(rev int64, hasGrant bool, cap string, outward, merge bool) string {
	g := "none"
	if hasGrant {
		g = fmt.Sprintf("rev %d", rev)
	}
	if cap == "" {
		cap = "grant"
	}
	eff := "none"
	switch {
	case outward && merge:
		eff = "outward+merge"
	case outward:
		eff = "outward"
	case merge:
		eff = "merge"
	}
	return fmt.Sprintf("grant %s, cap %s, effective %s", g, cap, eff)
}

// attentionNotes is CN24's display map — a note per blocked kind the sibling
// slices park loops with. It never decides and never interprets: an unknown
// kind rides verbatim with no note, because a kind S27 invented a sentence
// for is a kind S27 mis-read. S57's sentences arrive inside the blocked
// reports themselves (display-only, its C4), so the gate kinds get no note
// of ours.
var attentionNotes = map[string]string{
	"cap-hit": "raise the cap with aeon loop raise or stop the loop",
}

// attentionDisplay answers the kind unchanged beside its note (if we own one).
func attentionDisplay(kind string) (string, string) {
	return kind, attentionNotes[kind]
}

// SchedulerStopAllResult is stopAll's honest ledger: the pause is first (so
// nothing new starts), every stop is attempted, and every failure is
// named — a half-off switch says so, and the pause STAYS either way.
type SchedulerStopAllResult struct {
	Paused   bool     `json:"paused"`
	Stopped  []string `json:"stopped"`
	Failures []string `json:"failures,omitempty"`
}

// SchedulerStopAll is CN18's command: pause first through the human's own
// control write (the actor on the row is the person who asked — the
// scheduler_state actor vocabulary is the human's by T2's CHECK), then stop
// every loop the scheduler started that is still running or waiting. Each
// stop rides S55's own door (the same Daemon.StopLoop behind loop.stop),
// with its audit, its refusal shapes and its aftermath intact.
func (d *Daemon) SchedulerStopAll(ctx context.Context, actor store.SchedulerActor, reason string) (SchedulerStopAllResult, error) {
	res := SchedulerStopAllResult{Stopped: []string{}}
	if d.store == nil {
		return res, errors.New("the scheduler is not wired: there is no store to pause or stop through")
	}
	if _, err := d.store.SetSchedulerState(actor, true, true, reason); err != nil {
		return res, fmt.Errorf("scheduler: stopAll could not pause first: %w", err)
	}
	res.Paused = true
	loops, err := d.store.SchedulerLoopsToStop()
	if err != nil {
		return res, fmt.Errorf("scheduler: stopAll could not read the loops to stop: %w", err)
	}
	for _, l := range loops {
		out, refusal, err := d.StopLoop(ctx, l.ID, store.Actor{Kind: actor.Kind, ID: actor.ID}, reason)
		switch {
		case err != nil:
			res.Failures = append(res.Failures, fmt.Sprintf("%s: %v", l.Slug, err))
		case refusal != nil:
			res.Failures = append(res.Failures, fmt.Sprintf("%s: %v", l.Slug, refusal))
		case !out.Stopped:
			res.Failures = append(res.Failures, fmt.Sprintf("%s: the door reported no stop (status %q)", l.Slug, out.Status))
		default:
			res.Stopped = append(res.Stopped, l.Slug)
		}
	}
	return res, nil
}

// schedulerStopAllWired is V13's gate for T7's registration: the stopAll
// METHOD exists only where the adapter exists — absent, not refusing.
func schedulerStopAllWired(d *Daemon) bool { return d != nil && d.store != nil }

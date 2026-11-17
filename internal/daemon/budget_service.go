package daemon

// budget_service.go — the daemon's implementation of the protocol.BudgetStore
// seam (Table.BudgetStore): S55 T8's EIGHT budget doors. It is what the
// socket's budget.list / budget.show / budget.effective / spend.report /
// budget.set / budget.remove / budget.vocabulary / loop.raise reach: the T6
// store's budget rows, spend and audit, the T7 raise op, and the route table
// for the show door's overshoot note.
//
// # The views are assembled here, not in the store
//
// The store serves ROWS (a budget, its spend, its events, its open loops).
// The VIEW is the daemon's: row + live status (computed at the read, from the
// current spend and the warn line — never a stored flag) + per-dimension
// detail + last change. Assembling here keeps the store's rows honest (a
// stored status would be a cap that lies) and the wire's shape in one place.
//
// # Confirmation
//
// The set/remove classification lives here too: a set that LOWERS or ADDS a
// cap tightens authority and commits directly; a set that RAISES or DISABLES
// an enabled cap, and every removal, loosens authority and commits only
// against the digest of the canonical statement rendered from the row (the
// S45 PM7.1 pattern: the commit is bound to what was read). A digest that
// does not match the statement the daemon NOW renders is a confirm-mismatch
// — the row moved between the read and the commit, and the act is refused
// rather than silently loosened.
//
// # No caching
//
// Nothing here caches spend or rows across calls (N8): a floor read at one
// read is a stale floor at the next, and spend does not wait.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// budgetService is the daemon-side seam implementation. It is a small struct
// (not the Daemon itself) so the nil-guard is a constructor: a storeless
// daemon hands the table nil, and RegisterBudget leaves the eight doors
// ABSENT rather than registered-and-refusing (V13, the settings/loop-control
// precedent).
type budgetService struct {
	d *Daemon
}

// newBudgetService is the nil guard the daemon's wiring calls (V13).
func (d *Daemon) newBudgetService() protocol.BudgetStore {
	if d.store == nil {
		return nil
	}
	return budgetService{d: d}
}

// --- the conversions ----------------------------------------------------------

// budgetViewFor assembles one budget's list-with-details view (rule 3): the
// row, the live status (ok|warn|hit, computed from the current spend — the
// engine's own hit/warn predicates, one budget at a time), the per-dimension
// detail for every capped dimension, and the last change from the audit.
func (s budgetService) budgetViewFor(b store.Budget) (protocol.BudgetView, error) {
	sp, err := s.d.store.SpendFor(b, time.Now().UTC())
	if err != nil {
		return protocol.BudgetView{}, fmt.Errorf("daemon: the spend of budget %s: %w", b.ID, err)
	}
	v := protocol.BudgetView{
		ID: b.ID, Scope: b.Scope, SubjectID: b.Subject, Window: b.Window,
		Enabled: b.Enabled, UnknownPolicy: b.UnknownPolicy, WarnPct: b.WarnPct,
		Origin: b.Origin, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
		Status: budgetStatus(b, sp),
	}
	v.Dimensions = []protocol.BudgetDimension{}
	if b.USD != nil {
		exact := sp.USDPartial == 0 && sp.USDUnknown == 0
		floor := sp.USDAtLeast
		v.Dimensions = append(v.Dimensions, protocol.BudgetDimension{
			Dimension: "usd", Cap: b.USD, SpendFloor: &floor, Exact: &exact, UnknownCount: sp.USDUnknown})
	}
	if b.Tokens != nil {
		capV := float64(*b.Tokens)
		exact := sp.TokensUnknown == 0
		v.Dimensions = append(v.Dimensions, protocol.BudgetDimension{
			Dimension: "tokens", Cap: &capV, SpendFloor: i64toF64(sp.Tokens), Exact: &exact, UnknownCount: sp.TokensUnknown})
	}
	if b.WallMs != nil {
		capV := float64(*b.WallMs)
		floor := float64(sp.WallMs)
		exact := true
		v.Dimensions = append(v.Dimensions, protocol.BudgetDimension{
			Dimension: "wall-time", Cap: &capV, SpendFloor: &floor, Exact: &exact})
	}
	if b.Steps != nil {
		capV := float64(*b.Steps)
		floor := float64(sp.Steps)
		exact := true
		v.Dimensions = append(v.Dimensions, protocol.BudgetDimension{
			Dimension: "steps", Cap: &capV, SpendFloor: &floor, Exact: &exact})
	}
	v.LastChanged = s.lastChanged(b)
	return v, nil
}

// budgetStatus computes one budget's live status (ok|warn|hit) with the
// engine's own predicates, one budget at a time: a dimension hits when its
// floor has reached its cap (a zero cap at floor 0) or the policy is stop
// and the cost is unprovable; a dimension warns (without hitting) when the
// floor has crossed the warn line. The view's status is the worst dimension:
// hit beats warn beats ok. A disabled budget cannot hit or warn — its caps
// are not enforced — so it reads ok.
func budgetStatus(b store.Budget, sp store.BudgetSpend) string {
	if !b.Enabled {
		return "ok"
	}
	status := "ok"
	consider := func(dimension string, floor, cap float64, unknown int) {
		if cap < 0 {
			return
		}
		if floor >= cap || (unknown > 0 && b.UnknownPolicy == "stop") {
			status = "hit"
			return
		}
		if b.WarnPct != nil && cap > 0 && int(floor*100/cap) >= *b.WarnPct && status != "hit" {
			status = "warn"
		}
	}
	consider("usd", sp.USDAtLeast, f64cap(b.USD), sp.USDUnknown)
	consider("tokens", f64nilcap(sp.Tokens), i64cap(b.Tokens), sp.TokensUnknown)
	consider("wall-time", float64(sp.WallMs), i64cap(b.WallMs), 0)
	consider("steps", float64(sp.Steps), i64cap(b.Steps), 0)
	return status
}

// f64cap renders a nil cap as the -1 sentinel (no cap: skip the dimension),
// the engine's own convention, so the status and the gate read the same
// absence.
func f64cap(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}

// i64cap is f64cap for the three integer dimensions (tokens, wall-time,
// steps): the store keeps them as *int64, the engine's floor/cap math as
// float64.
func i64cap(p *int64) float64 {
	if p == nil {
		return -1
	}
	return float64(*p)
}

// f64nilcap is f64cap for a nilable floor (the token floor): nil is a zero
// floor (no priced tokens at all), the unknown count beside it.
func f64nilcap(p *int64) float64 {
	if p == nil {
		return 0
	}
	return float64(*p)
}

// lastChanged reads the budget's audit (newest first) for the newest
// change-kind row (created, changed or raised) — the view's lastChanged. A
// removed budget has no view, so the removal is seen in the audit, not in
// the list; a hit/warned row is the engine's voice, not a change.
func (s budgetService) lastChanged(b store.Budget) *protocol.BudgetLastChanged {
	events, err := s.d.store.ListBudgetEvents(b.ID, 0)
	if err != nil {
		return nil
	}
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Kind {
		case "created", "changed", "raised":
			e := events[i]
			return &protocol.BudgetLastChanged{At: e.At, Kind: e.Kind, ByKind: e.ActorKind, ByID: e.ActorID}
		}
	}
	return nil
}

// budgetEventView maps one audit row onto the wire.
func budgetEventView(e store.BudgetEvent) protocol.BudgetEventView {
	return protocol.BudgetEventView{
		At: e.At, Kind: e.Kind, ActorKind: e.ActorKind, ActorID: e.ActorID,
		LoopID: e.LoopID, Dimension: e.Dimension,
		OldValue: e.OldValue, NewValue: e.NewValue, SpendFloor: e.SpendFloor, Exact: e.Exact,
	}
}

// --- the read doors -------------------------------------------------------------

// List is budget.list's read: every budget matching the filters, with
// details (the view's status is the live one, so the status filter is live
// too — a list that filters on a stored flag would show what the caps were,
// not what they are).
func (s budgetService) List(_ context.Context, scope, subjectID, status string, enabled *bool) ([]protocol.BudgetView, error) {
	all, err := s.d.store.ListBudgets()
	if err != nil {
		return nil, fmt.Errorf("daemon: list budgets: %w", err)
	}
	out := []protocol.BudgetView{}
	for _, b := range all {
		if scope != "" && b.Scope != scope {
			continue
		}
		if subjectID != "" && b.Subject != subjectID {
			continue
		}
		if enabled != nil && b.Enabled != *enabled {
			continue
		}
		v, err := s.budgetViewFor(b)
		if err != nil {
			return nil, err
		}
		if status != "" && v.Status != status {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// Show is budget.show's read: the view, the spend breakdown, the open loops
// the budget covers right now, the audit (the screen's window, oldest
// first), and the worst-case overshoot note.
func (s budgetService) Show(_ context.Context, id string) (protocol.BudgetShowResult, error) {
	b, err := s.d.store.GetBudget(id)
	if err != nil {
		return protocol.BudgetShowResult{}, mapBudgetError(id, err)
	}
	v, err := s.budgetViewFor(b)
	if err != nil {
		return protocol.BudgetShowResult{}, err
	}
	now := time.Now().UTC()
	sp, err := s.d.store.SpendFor(b, now)
	if err != nil {
		return protocol.BudgetShowResult{}, fmt.Errorf("daemon: the spend of budget %s: %w", b.ID, err)
	}
	open, err := s.d.store.OpenLoopsForBudget(b)
	if err != nil {
		return protocol.BudgetShowResult{}, fmt.Errorf("daemon: the open loops of budget %s: %w", b.ID, err)
	}
	events, err := s.d.store.ListBudgetEvents(id, protocol.BudgetEventWindow)
	if err != nil {
		return protocol.BudgetShowResult{}, fmt.Errorf("daemon: the audit of budget %s: %w", b.ID, err)
	}
	note, err := s.overshootNote(b)
	if err != nil {
		return protocol.BudgetShowResult{}, err
	}
	out := protocol.BudgetShowResult{
		View: v,
		Spend: protocol.BudgetSpendView{
			USDFloor: sp.USDAtLeast, USDPartial: sp.USDPartial, USDUnknown: sp.USDUnknown,
			Exact: sp.Exact, Tokens: sp.Tokens, TokensUnknown: sp.TokensUnknown,
			WallMs: sp.WallMs, Steps: sp.Steps,
		},
		OpenLoops: open,
	}
	if out.OpenLoops == nil {
		out.OpenLoops = []string{}
	}
	out.History = make([]protocol.BudgetEventView, 0, len(events))
	for _, e := range events {
		out.History = append(out.History, budgetEventView(e))
	}
	out.OvershootNote = note
	return out, nil
}

// overshootNote is budget.show's worst-case overshoot: how far past the cap
// ONE MORE STEP could spend before the next check fires (the cap is enforced
// at the checks, not continuously, so the note is honest arithmetic over the
// routes' bounds, not a promise). A USD cap over priced routes is bounded by
// the worst route's (maxOutputTokens × output price + contextWindow × input
// price); unpriced work is not bounded in USD at all — the note says so and
// names the tokens cap that bounds it. A tokens cap is bounded by the worst
// route's output cap.
func (s budgetService) overshootNote(b store.Budget) (string, error) {
	routes, err := s.d.store.ListRoutes()
	if err != nil {
		return "", fmt.Errorf("daemon: the routes for the overshoot note: %w", err)
	}
	var worstUSD float64
	var worstRoute string
	var worstTokens int
	for _, r := range routes {
		if r.MaxOutputTokens > worstTokens {
			worstTokens = r.MaxOutputTokens
		}
		if r.NoPrice || !r.Prices.Any() {
			continue
		}
		out := 0.0
		if r.Prices.OutputUSDPerMTok != nil {
			out = *r.Prices.OutputUSDPerMTok
		}
		inn := 0.0
		if r.Prices.InputUSDPerMTok != nil {
			inn = *r.Prices.InputUSDPerMTok
		}
		cost := float64(r.MaxOutputTokens) * out / 1e6
		if r.ContextWindow > 0 {
			cost += float64(r.ContextWindow) * inn / 1e6
		}
		if cost > worstUSD {
			worstUSD = cost
			worstRoute = r.ID
		}
	}
	switch {
	case b.USD != nil && b.Tokens == nil:
		if worstRoute == "" {
			return "not bounded in USD — no priced route bounds one step's spend; set a tokens cap to bound the spend in tokens", nil
		}
		return fmt.Sprintf("one more step can spend up to $%s before the next check fires (worst case over priced routes: %s)",
			budgetNum(worstUSD), worstRoute), nil
	case b.USD != nil && b.Tokens != nil:
		if worstRoute == "" {
			return fmt.Sprintf("the USD cap is not bounded by any priced route (set a tokens cap); in tokens, one more step can spend up to %d more before the next check fires", worstTokens), nil
		}
		return fmt.Sprintf("one more step can spend up to $%s in USD (worst route %s) or %d more tokens, whichever cap fires first, before the next check",
			budgetNum(worstUSD), worstRoute, worstTokens), nil
	case b.Tokens != nil:
		return fmt.Sprintf("one more step can spend up to %d more tokens before the next check fires (the worst route's output cap)", worstTokens), nil
	default:
		return "", nil
	}
}

// mapBudgetError is the door's error translation: a budget the store does
// not have is a param that names nothing (the S39 run door's -32602 shape);
// every other error is infrastructure and rides up unchanged.
func mapBudgetError(id string, err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return protocol.ErrBadParams{Err: fmt.Errorf("no budget %q — the id names a budget that is not in this store", id)}
	}
	return err
}

// Effective is budget.effective's read: every budget that applies to the
// named subject, TIGHTEST FIRST (LC9's tier order) — the store's ordered
// applicability readers (the same subject walk the enforcement side runs, so
// the read and the gate cannot disagree about what is in force), each
// assembled into its full view.
func (s budgetService) Effective(_ context.Context, loopID, rootID, componentID string) (protocol.BudgetEffectiveResult, error) {
	var (
		budgets []store.Budget
		subject string
		err     error
	)
	switch {
	case loopID != "":
		subject = "loop " + loopID
		budgets, err = s.d.store.ApplicableBudgetsOrdered(loopID)
		if err != nil && errors.Is(err, store.ErrNotFound) {
			return protocol.BudgetEffectiveResult{}, protocol.ErrBadParams{Err: fmt.Errorf("no loop %q — the id names a loop that is not in this store", loopID)}
		}
	case componentID != "":
		subject = "component " + componentID
		budgets, err = s.d.store.ApplicableBudgetsForOrdered(componentID, "")
	case rootID != "":
		subject = "root " + rootID
		budgets, err = s.d.store.ApplicableBudgetsForOrdered(rootID, "")
	}
	if err != nil {
		return protocol.BudgetEffectiveResult{}, fmt.Errorf("daemon: the effective budgets for %s: %w", subject, err)
	}
	out := protocol.BudgetEffectiveResult{Subject: subject, Budgets: []protocol.BudgetView{}}
	for _, b := range budgets {
		v, err := s.budgetViewFor(b)
		if err != nil {
			return protocol.BudgetEffectiveResult{}, err
		}
		out.Budgets = append(out.Budgets, v)
	}
	return out, nil
}

// SpendReport is spend.report's read: the spend over time, grouped. The
// window's text params are parsed here (RFC 3339, UTC) and echoed back in the
// result as the store's own timestamp shape, so the client sees the window
// that was actually read.
func (s budgetService) SpendReport(_ context.Context, p protocol.SpendReportParams) (protocol.SpendReportResult, error) {
	since := time.Time{}
	if p.Since != "" {
		t, err := time.Parse(time.RFC3339, p.Since)
		if err != nil {
			return protocol.SpendReportResult{}, protocol.ErrBadParams{Err: fmt.Errorf("since is not RFC 3339 (%q)", p.Since)}
		}
		since = t.UTC()
	}
	until := time.Now().UTC()
	if p.Until != "" {
		t, err := time.Parse(time.RFC3339, p.Until)
		if err != nil {
			return protocol.SpendReportResult{}, protocol.ErrBadParams{Err: fmt.Errorf("until is not RFC 3339 (%q)", p.Until)}
		}
		until = t.UTC()
	}
	if !since.IsZero() && !until.IsZero() && since.After(until) {
		return protocol.SpendReportResult{}, protocol.ErrBadParams{Err: fmt.Errorf("since is after until — the window is empty by construction")}
	}
	buckets, err := s.d.store.SpendReport(p.GroupBy, since, until, p.RootID)
	if err != nil {
		return protocol.SpendReportResult{}, fmt.Errorf("daemon: the spend report: %w", err)
	}
	out := protocol.SpendReportResult{
		GroupBy: p.GroupBy,
		Since:   isoWindow(since),
		Until:   isoWindow(until),
		RootID:  p.RootID,
		Buckets: make([]protocol.SpendReportBucket, 0, len(buckets)),
	}
	for _, b := range buckets {
		out.Buckets = append(out.Buckets, protocol.SpendReportBucket{
			Key: b.Key, USDFloor: b.USDFloor, Exact: b.Exact(),
			PartialCount: b.USDPartial, UnknownCount: b.USDUnknown,
			Tokens: b.Tokens, Steps: b.Steps,
		})
	}
	return out, nil
}

// isoWindow renders a window bound the way the store's timestamps do (the
// event log's own shape), so the echoed window reads like everything else.
func isoWindow(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// --- the human doors ------------------------------------------------------------

// budgetDigest is the confirmation digest: the hex SHA-256 of the canonical
// statement. The statement's grammar is fixed (the renderers below), so the
// digest is a function of the row and the requested change — a commit
// carrying a digest the daemon cannot re-render is a commit against a row
// that moved (PM7.1: bound to what was read).
func budgetDigest(statement string) string {
	sum := sha256.Sum256([]byte(statement))
	return hex.EncodeToString(sum[:])
}

// capsList renders a budget's caps in the fixed dimension order (usd, tokens,
// wall-time, steps) the statements use — one renderer, byte-stable, so the
// same row always digests to the same statement.
func capsList(b store.Budget) string {
	parts := []string{}
	if b.USD != nil {
		parts = append(parts, "usd="+budgetNum(*b.USD))
	}
	if b.Tokens != nil {
		parts = append(parts, "tokens="+budgetNum(float64(*b.Tokens)))
	}
	if b.WallMs != nil {
		parts = append(parts, "wall-time="+budgetNum(float64(*b.WallMs)))
	}
	if b.Steps != nil {
		parts = append(parts, "steps="+budgetNum(float64(*b.Steps)))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, ", ")
}

// createdStatement is the canonical statement of a budget.set that creates.
// Creation tightens authority (a cap that did not exist now exists), so it
// never needs a confirmation — the statement rides a dryRun so the UI can
// show what the commit would say.
func createdStatement(p protocol.BudgetSetParams, merged store.Budget) string {
	s := fmt.Sprintf("create budget %s/%s/%s: caps %s; unknown policy %s", p.Scope, p.SubjectID, p.Window, capsList(merged), merged.UnknownPolicy)
	if merged.WarnPct != nil {
		s += fmt.Sprintf("; warn line %d%%", *merged.WarnPct)
	}
	if !merged.Enabled {
		s += "; disabled"
	}
	return s
}

// updatedStatement is the canonical statement of a budget.set that updates:
// every change, in the fixed order (caps in dimension order, then the
// unknown policy, the warn line, enabled). Empty means a no-op (nothing
// differs) — the caller writes nothing and renders no statement (LC11).
func updatedStatement(id string, cur, merged store.Budget) string {
	parts := []string{}
	capDiff := func(dim string, oldP, newP *float64) {
		same := (oldP == nil && newP == nil) || (oldP != nil && newP != nil && *oldP == *newP)
		if same {
			return
		}
		from, to := "no cap", "no cap"
		if oldP != nil {
			from = budgetNum(*oldP)
		}
		if newP != nil {
			to = budgetNum(*newP)
		}
		parts = append(parts, fmt.Sprintf("set the %s cap from %s to %s", dim, from, to))
	}
	capDiff("usd", f64ptr(cur.USD), f64ptr(merged.USD))
	capDiff("tokens", i64toF64(cur.Tokens), i64toF64(merged.Tokens))
	capDiff("wall-time", i64toF64(cur.WallMs), i64toF64(merged.WallMs))
	capDiff("steps", i64toF64(cur.Steps), i64toF64(merged.Steps))
	if merged.UnknownPolicy != cur.UnknownPolicy {
		parts = append(parts, fmt.Sprintf("set the unknown policy from %s to %s", cur.UnknownPolicy, merged.UnknownPolicy))
	}
	warnSame := (cur.WarnPct == nil && merged.WarnPct == nil) || (cur.WarnPct != nil && merged.WarnPct != nil && *cur.WarnPct == *merged.WarnPct)
	if !warnSame {
		from, to := "no warn line", "no warn line"
		if cur.WarnPct != nil {
			from = fmt.Sprintf("%d%%", *cur.WarnPct)
		}
		if merged.WarnPct != nil {
			to = fmt.Sprintf("%d%%", *merged.WarnPct)
		}
		parts = append(parts, fmt.Sprintf("set the warn line from %s to %s", from, to))
	}
	if merged.Enabled != cur.Enabled {
		if merged.Enabled {
			parts = append(parts, "enable the budget")
		} else {
			parts = append(parts, "disable the budget")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "budget " + id + ": " + strings.Join(parts, "; ")
}

// f64ptr is a pass-through for the float64 caps (the statement's one
// representation).
func f64ptr(p *float64) *float64 { return p }

// orPtr returns v when it is set, else def — the merged-row builder's
// "a nil cap keeps the current one" (the store's orDefaultP, this package's
// own copy, because the store's is unexported).
func orPtr[T any](v, def *T) *T {
	if v != nil {
		return v
	}
	return def
}

// i64toF64 renders an integer cap in the statement's float representation
// (whole numbers; the caps are far under float64's exact range).
func i64toF64(p *int64) *float64 {
	if p == nil {
		return nil
	}
	v := float64(*p)
	return &v
}

// budgetActorFrom maps the protocol's stamped actor onto the budget audit's
// closed kinds (user|cli|system) — the same mapping the T7 raise op uses: a
// person acts as a person, an agent or trigger act as a client.
func budgetActorFrom(a protocol.Actor) store.BudgetActor {
	return store.BudgetActor{Kind: budgetActorKind(store.Actor{Kind: a.Kind, ID: a.ID}), ID: a.ID}
}

// setLoosening reports which of the requested changes loosen authority: a
// cap raised (or added above a lower existing cap is still tightening —
// only a RAISE of a value that was there loosens) and an enabled budget
// disabled. The parts are the statement fragments the confirmation is about.
func setLoosening(cur, merged store.Budget) []string {
	parts := []string{}
	raise := func(dim string, oldP, newP *float64) {
		if oldP != nil && newP != nil && *newP > *oldP {
			parts = append(parts, fmt.Sprintf("raise the %s cap from %s to %s", dim, budgetNum(*oldP), budgetNum(*newP)))
		}
	}
	raise("usd", cur.USD, merged.USD)
	raise("tokens", i64toF64(cur.Tokens), i64toF64(merged.Tokens))
	raise("wall-time", i64toF64(cur.WallMs), i64toF64(merged.WallMs))
	raise("steps", i64toF64(cur.Steps), i64toF64(merged.Steps))
	if cur.Enabled && !merged.Enabled {
		parts = append(parts, "disable the budget")
	}
	return parts
}

// Set is budget.set's human door: create or update by the unique key
// (scope, subjectId, window), with the confirmation the S45 PM7.1 pattern
// demands of the authority-loosening acts. The flow:
//
//   - read the row at the key (FindBudget: found=false is the create arm);
//   - compute the merged row (a nil cap on an update KEEPS the current one —
//     the store's merge semantics; clearing a dimension is not a set, it is
//     a removal of the budget) and classify every change;
//   - a no-op writes nothing and renders nothing (LC11);
//   - a tightening (a cap lowered or added, a disable re-enabled, a meta
//     change) commits directly;
//   - a loosening (a cap raised, an enabled budget disabled) commits only
//     against the digest of the statement rendered from the row NOW: no
//     digest is confirm-required (the statement and digest ride the error,
//     so the client completes in one round trip), a wrong one is
//     confirm-mismatch (the row moved since the read);
//   - a dryRun returns the effect and the statement/digest and writes
//     nothing.
func (s budgetService) Set(ctx context.Context, actor protocol.Actor, p protocol.BudgetSetParams) (protocol.BudgetSetResult, *protocol.BudgetRefusal, error) {
	switch p.Scope {
	case "workspace", "component", "epic", "loop":
	default:
		return protocol.BudgetSetResult{}, nil, protocol.ErrBadParams{Err: fmt.Errorf("scope is workspace, component, epic or loop, not %q", p.Scope)}
	}
	switch p.Window {
	case "lifetime", "day":
	default:
		return protocol.BudgetSetResult{}, nil, protocol.ErrBadParams{Err: fmt.Errorf("window is lifetime or day, not %q", p.Window)}
	}
	if p.UnknownPolicy != "" && p.UnknownPolicy != "proceed" && p.UnknownPolicy != "stop" {
		return protocol.BudgetSetResult{}, nil, protocol.ErrBadParams{Err: fmt.Errorf("unknownPolicy is proceed or stop, not %q", p.UnknownPolicy)}
	}

	cur, found, err := s.d.store.FindBudget(p.Scope, p.SubjectID, p.Window)
	if err != nil {
		return protocol.BudgetSetResult{}, nil, fmt.Errorf("daemon: find the budget %s/%s/%s: %w", p.Scope, p.SubjectID, p.Window, err)
	}

	// The merged row: the request applied over the current one (a nil cap
	// keeps the current cap; a fresh budget defaults to proceed + enabled).
	merged := store.Budget{Scope: p.Scope, Subject: p.SubjectID, Window: p.Window, UnknownPolicy: "proceed", Enabled: true}
	if found {
		merged = cur
	}
	merged.USD = orPtr(p.USDCap, merged.USD)
	merged.Tokens = orPtr(p.TokensCap, merged.Tokens)
	merged.WallMs = orPtr(p.WallMsCap, merged.WallMs)
	merged.Steps = orPtr(p.StepsCap, merged.Steps)
	if p.UnknownPolicy != "" {
		merged.UnknownPolicy = p.UnknownPolicy
	}
	merged.WarnPct = orPtr(p.WarnPct, merged.WarnPct)
	if p.Enabled != nil {
		merged.Enabled = *p.Enabled
	}
	if found {
		merged.ID = cur.ID
	}

	statement := ""
	if found {
		statement = updatedStatement(cur.ID, cur, merged)
	} else {
		statement = createdStatement(p, merged)
	}
	effect := "created"
	if found {
		effect = "updated"
		if statement == "" {
			effect = "noop"
		}
	}
	digest := budgetDigest(statement)
	loosening := []string{}
	if found && effect == "updated" {
		loosening = setLoosening(cur, merged)
	}

	// A no-op writes nothing (LC11) and needs no confirmation: there is
	// nothing to loosen.
	if effect == "noop" {
		v, err := s.budgetViewFor(cur)
		if err != nil {
			return protocol.BudgetSetResult{}, nil, err
		}
		return protocol.BudgetSetResult{View: &v, Effect: "noop", DryRun: p.DryRun}, nil, nil
	}

	if len(loosening) > 0 {
		if p.DryRun {
			return protocol.BudgetSetResult{Effect: effect, DryRun: true, Statement: statement, Digest: digest}, nil, nil
		}
		if p.Confirm == "" {
			return protocol.BudgetSetResult{}, &protocol.BudgetRefusal{
				Kind:      "confirm-required",
				Detail:    "this act loosens authority (" + strings.Join(loosening, "; ") + ") — commit it with the digest of the statement as read",
				Statement: statement, Digest: digest,
			}, nil
		}
		if p.Confirm != digest {
			return protocol.BudgetSetResult{}, &protocol.BudgetRefusal{
				Kind:      "confirm-mismatch",
				Detail:    "the digest does not match the statement rendered from the row as it is now (it moved since you read it) — read it again and commit with the new digest",
				Statement: statement, Digest: digest,
			}, nil
		}
	}

	// The commit (the dryRun never reaches here for a loosening; a
	// tightening dryRun returns below).
	if p.DryRun {
		return protocol.BudgetSetResult{Effect: effect, DryRun: true, Statement: statement, Digest: digest}, nil, nil
	}
	committed, ref, err := s.commitBudget(actor, found, cur.ID, p, merged)
	if err != nil {
		return protocol.BudgetSetResult{}, nil, err
	}
	if ref != nil {
		return protocol.BudgetSetResult{}, ref, nil
	}
	// The commit is bound to what was read (PM7.1): the store's update
	// re-reads inside its own transaction, so a row that moved between the
	// read above and that transaction commits against the NEW state —
	// re-read and verify the row is what the statement promised, and if it
	// is not, the act is reported as a mismatch (the row's actual state
	// rides the refusal, so a re-commit is one re-read away).
	if found {
		after, err := s.d.store.GetBudget(committed.ID)
		if err != nil {
			return protocol.BudgetSetResult{}, nil, fmt.Errorf("daemon: verify the set of budget %s: %w", committed.ID, err)
		}
		if !budgetRowsEqual(after, merged) {
			stateStatement := fmt.Sprintf("budget %s now reads: caps %s; unknown policy %s; enabled %t",
				committed.ID, capsList(after), after.UnknownPolicy, after.Enabled)
			return protocol.BudgetSetResult{}, &protocol.BudgetRefusal{
				Kind:      "confirm-mismatch",
				Detail:    "the row moved during the commit and does not read as the confirmed statement — the refusal carries what it reads as now; re-read and commit again",
				Statement: stateStatement, Digest: budgetDigest(stateStatement),
			}, nil
		}
	}
	view, err := s.budgetViewFor(committed)
	if err != nil {
		return protocol.BudgetSetResult{}, nil, err
	}
	return protocol.BudgetSetResult{View: &view, Effect: effect, Statement: statement, Digest: digest, Confirmed: len(loosening) > 0}, nil, nil
}

// budgetRowsEqual compares the audit-relevant fields of two budget rows
// (caps, policy, warn line, enabled) — the post-commit verification's
// "did the row land as the statement promised." Nil caps compare equal
// (a dimension no one caps is the same absence in both rows).
func budgetRowsEqual(a, b store.Budget) bool {
	f64Same := func(x, y *float64) bool {
		return (x == nil && y == nil) || (x != nil && y != nil && *x == *y)
	}
	i64Same := func(x, y *int64) bool {
		return (x == nil && y == nil) || (x != nil && y != nil && *x == *y)
	}
	if !f64Same(a.USD, b.USD) || !i64Same(a.Tokens, b.Tokens) || !i64Same(a.WallMs, b.WallMs) || !i64Same(a.Steps, b.Steps) {
		return false
	}
	if a.UnknownPolicy != b.UnknownPolicy || a.Enabled != b.Enabled {
		return false
	}
	warnSame := (a.WarnPct == nil && b.WarnPct == nil) || (a.WarnPct != nil && b.WarnPct != nil && *a.WarnPct == *b.WarnPct)
	return warnSame
}

// commitBudget runs the store's write (create or update). The update passes
// the FULL merged state as the NewBudgetIn: the store's update validation
// refuses a change with no dimension set (a meta-only change would die in
// the backstop), and passing the merged caps makes the store's change
// detection see exactly the request's diff.
func (s budgetService) commitBudget(actor protocol.Actor, found bool, id string, p protocol.BudgetSetParams, merged store.Budget) (store.Budget, *protocol.BudgetRefusal, error) {
	budget := budgetActorFrom(actor)
	if !found {
		b, ref, err := s.d.store.CreateBudget(budget, store.NewBudgetIn{
			Scope: p.Scope, Subject: p.SubjectID, Window: p.Window,
			USD: p.USDCap, Tokens: p.TokensCap, WallMs: p.WallMsCap, Steps: p.StepsCap,
			UnknownPolicy: p.UnknownPolicy, WarnPct: p.WarnPct, Enabled: p.Enabled,
		})
		if err != nil {
			return store.Budget{}, nil, fmt.Errorf("daemon: create the budget %s/%s/%s: %w", p.Scope, p.SubjectID, p.Window, err)
		}
		if ref.Kind != "" {
			return store.Budget{}, &protocol.BudgetRefusal{Kind: ref.Kind, Detail: ref.Detail}, nil
		}
		return b, nil, nil
	}
	b, ref, err := s.d.store.UpdateBudget(budget, id, store.NewBudgetIn{
		Scope: p.Scope, Subject: p.SubjectID, Window: p.Window,
		USD: merged.USD, Tokens: merged.Tokens, WallMs: merged.WallMs, Steps: merged.Steps,
		UnknownPolicy: merged.UnknownPolicy, WarnPct: merged.WarnPct, Enabled: &merged.Enabled,
	})
	if err != nil {
		return store.Budget{}, nil, fmt.Errorf("daemon: update the budget %s: %w", id, err)
	}
	if ref.Kind != "" {
		return store.Budget{}, &protocol.BudgetRefusal{Kind: ref.Kind, Detail: ref.Detail}, nil
	}
	return b, nil, nil
}

// removeStatement is the canonical statement of a budget.remove: the id, the
// key, and the caps that stop applying after it. A removal always loosens
// authority (a cap that is gone will never fire), so it always confirms —
// the statement's digest is the only thing that lets it commit.
func removeStatement(b store.Budget) string {
	return fmt.Sprintf("remove budget %s (%s %s, %s): in-force caps %s — after this, no cap applies at this subject",
		b.ID, b.Scope, b.Subject, b.Window, capsList(b))
}

// RemoveBudget is budget.remove's human door (the seam's name changed from
// Remove in the 2026-11-02 merge reconciliation: S45's boundary guards grep
// for the policy boundary's Remove call shape — a dot-Remove receiver whose
// first argument is a bare ctx — against a closed whitelist, and this seam
// is S55's — the interface comment in internal/protocol/budget.go). A
// missing budget is a param that names nothing (the -32602 shape), not a
// refusal: there is nothing to remove and nothing to confirm.
func (s budgetService) RemoveBudget(_ context.Context, actor protocol.Actor, id, confirm string) (protocol.BudgetRemoveResult, *protocol.BudgetRefusal, error) {
	b, err := s.d.store.GetBudget(id)
	if err != nil {
		return protocol.BudgetRemoveResult{}, nil, mapBudgetError(id, err)
	}
	statement := removeStatement(b)
	digest := budgetDigest(statement)
	if confirm == "" {
		return protocol.BudgetRemoveResult{}, &protocol.BudgetRefusal{
			Kind:      "confirm-required",
			Detail:    "removing a budget loosens authority (its caps stop applying) — commit it with the digest of the statement as read",
			Statement: statement, Digest: digest,
		}, nil
	}
	if confirm != digest {
		return protocol.BudgetRemoveResult{}, &protocol.BudgetRefusal{
			Kind:      "confirm-mismatch",
			Detail:    "the digest does not match the statement rendered from the row as it is now (it moved since you read it) — read it again and remove with the new digest",
			Statement: statement, Digest: digest,
		}, nil
	}
	if err := s.d.store.RemoveBudget(budgetActorFrom(actor), id); err != nil {
		return protocol.BudgetRemoveResult{}, nil, fmt.Errorf("daemon: remove budget %s: %w", id, err)
	}
	return protocol.BudgetRemoveResult{ID: id, Removed: true}, nil, nil
}

// raiseDimension reads the single new cap out of loop.raise's params (the
// handler has already refused a param set that names anything but exactly
// one) and renders it as the engine's dimension name and value.
func raiseDimension(p protocol.LoopRaiseParams) (dimension string, newCap float64) {
	switch {
	case p.USDCap != nil:
		return "usd", *p.USDCap
	case p.TokensCap != nil:
		return "tokens", float64(*p.TokensCap)
	case p.WallMsCap != nil:
		return "wall-time", float64(*p.WallMsCap)
	case p.StepsCap != nil:
		return "steps", float64(*p.StepsCap)
	}
	return "", 0
}

// Raise is loop.raise's human door: the person's words and the new cap feed
// the T7 in-process op (the pin read, the eligibility gate, the cap lift,
// the loop's CAS, the kick — the ordering the crash argument needs). The
// door adds two checks the in-process op cannot make: the dimension the
// caller named is the dimension the loop is parked on (a raise lifts the
// cap the hit tripped — a caller param against another dimension would
// silently lift the wrong cap), and a named budgetId names the parked
// budget. A LOST RACE is a RESULT (the loop.resume precedent): the cap was
// lifted when the raise's budget commit landed, the loop moved first, and
// the answer names the status it already has with the refusal that states
// it — never an error with a dead end.
func (s budgetService) Raise(ctx context.Context, actor protocol.Actor, p protocol.LoopRaiseParams) (protocol.LoopRaiseResult, *protocol.BudgetRefusal, error) {
	dimension, newCap := raiseDimension(p)
	st, err := loop.Pinned(ctx, s.d.store, p.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.LoopRaiseResult{}, nil, protocol.ErrBadParams{Err: fmt.Errorf("no loop %q — the id names a loop that is not in this store", p.ID)}
		}
		return protocol.LoopRaiseResult{}, nil, fmt.Errorf("daemon: pin the loop for the raise of %s: %w", p.ID, err)
	}
	if st.CapHit != nil {
		if p.BudgetID != "" && p.BudgetID != st.CapHit.BudgetID {
			return protocol.LoopRaiseResult{LoopID: p.ID}, &protocol.BudgetRefusal{
				Kind:   "wrong-budget",
				Detail: fmt.Sprintf("the loop is parked on budget %s — a raise lifts the cap the hit tripped, not another budget's", st.CapHit.BudgetID),
			}, nil
		}
		if st.CapHit.Dimension != dimension {
			return protocol.LoopRaiseResult{LoopID: p.ID}, &protocol.BudgetRefusal{
				Kind:   "wrong-dimension",
				Detail: fmt.Sprintf("the loop is parked on the %s cap — a raise lifts that dimension; the %sCap param is not the door for it", st.CapHit.Dimension, dimension),
			}, nil
		}
	}
	var from *float64
	if st.CapHit != nil {
		cap := st.CapHit.Cap
		from = &cap
	}
	res, ref, err := s.d.RaiseLoop(ctx, store.Actor{Kind: actor.Kind, ID: actor.ID}, p.ID, newCap, p.Reason)
	if err != nil {
		return protocol.LoopRaiseResult{LoopID: p.ID}, nil, err
	}
	if ref != nil {
		refusal := &protocol.BudgetRefusal{Kind: ref.Kind, Detail: ref.Detail}
		if res.Status != "" {
			// A lost race (the CAS found the loop moved): the cap IS lifted
			// (the raised event says so, this call's actor in it) and the
			// loop did not move on this act's word — a result with the
			// refusal, not an error.
			return protocol.LoopRaiseResult{
				LoopID:  p.ID,
				Raised:  &protocol.LoopRaiseDetail{BudgetID: res.Budget, Dimension: dimension, From: from, To: newCap},
				Resumed: false, Status: res.Status,
				Note:    "the loop moved before the raise could land — the refusal names the door its state has",
				Refusal: refusal,
			}, nil, nil
		}
		return protocol.LoopRaiseResult{LoopID: p.ID}, refusal, nil
	}
	note := res.Note
	if res.HowHint != "" {
		note = note + "; " + res.HowHint
	}
	return protocol.LoopRaiseResult{
		LoopID:  p.ID,
		Raised:  &protocol.LoopRaiseDetail{BudgetID: res.Budget, Dimension: dimension, From: from, To: newCap},
		Resumed: true, Status: res.Status, How: res.How, Note: note,
	}, nil, nil
}

// Vocabulary is budget.vocabulary's read: the closed sets the forms and the
// CLI build from (rule 4: the vocabulary comes from the DAEMON — a literal in
// a client is a second truth that drifts when the set widens). The refusal
// kinds are the store's write refusals plus the door's (the confirmation's
// two, the raise's param checks, and the raise's state kinds — the same set
// loop.CheckRaise names).
func (s budgetService) Vocabulary() protocol.BudgetVocabularyResult {
	return protocol.BudgetVocabularyResult{
		Scopes:          []string{"workspace", "component", "epic", "loop"},
		Windows:         []string{"lifetime", "day"},
		Dimensions:      []string{"usd", "tokens", "wall-time", "steps"},
		UnknownPolicies: []string{"proceed", "stop"},
		Statuses:        []string{"ok", "warn", "hit"},
		EventKinds:      []string{"created", "changed", "removed", "raised", "hit", "warned"},
		RefusalKinds: []string{
			// budget.set / budget.remove (the store's write refusals)
			"no-dimension", "negative-cap", "warn-pct", "workspace-subject", "unknown-subject", "duplicate",
			// the confirmation (PM7.1)
			"confirm-required", "confirm-mismatch",
			// loop.raise (the door's param checks)
			"wrong-budget", "wrong-dimension",
			// loop.raise (the state and the cap)
			"no-cap", "below-floor", "lowering",
			"running", "stopped", "waiting-question", "blocked", "idle", "terminal",
			// the storeless daemon (the raise op's honest absence)
			"storeless",
		},
	}
}

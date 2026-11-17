package daemon

// automation_read.go — S49 T3: the daemon's side of the unified automation
// READS (protocol.AutomationReads). It reads BOTH families' rows as they
// are (S20's `trigger_schedule` through the scoped read, S40's
// `event_trigger` through the listing), assembles ONE view, composes the
// plain-words sentence through automationview.Describe (AN5's one
// sentence-maker — this file owns no wording), and answers the fire log,
// the runs, and the audit from T1's unified read.
//
// What it must never do (T3's behaviour notes): write, claim, or gate. The
// engines run with every read open exactly as they run with them closed;
// the read that could change what fires is the second authority the design
// forbids (AN8). Health is computed from the SAME inputs the fire-time
// doors use (the blueprint through the same Analyze call `loop.start`
// answers from, the workspace through a stat, the secret through an env
// NAME's presence — never its value) and never consulted by any engine.
//
// V13: newAutomationReads answers nil without a store, and daemon.go hands
// that nil straight to RegisterAutomationReads — absent, never
// registered-and-refusing.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/automationview"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// automationReads is the seam. The four function fields are the outside
// world injected, not called inline: tests set a fixed At (health stamps and
// preview defaults), a LookupEnv that never touches the real environment
// (the secret's presence is the whole answer, AN1 — the value would not
// survive the function boundary anyway), a workspace stat, and a component
// existence check. The zero-value struct never reaches the socket: the
// constructor fills all four, and the daemon wires only what it constructs.
type automationReads struct {
	s *store.Store
	// At is the health stamp and the preview default. It reads the moment,
	// never a cache — health is a snapshot and the stamp says which one.
	At func() time.Time
	// EnvSet answers "is this env NAME set (non-empty)" — presence only.
	// There is deliberately no function here that returns a value.
	EnvSet func(name string) bool
	// DirExists is the workspace check (a path that is a directory).
	DirExists func(path string) bool
	// ScopeExists answers whether a root/component id still names a live
	// scope; nil falls back to the store reads below.
	ScopeExists func(ctx context.Context, rootID, componentID string) bool
}

// newAutomationReads builds the adapter over the daemon's store (V13:
// storeless ⇒ nil ⇒ the ten reads stay absent). S49 T8: the body moved to
// newAutomationReadsFor so the CLI's offline door reaches the SAME adapter
// (one construction, two callers — NewAutomationReadDoors).
func (d *Daemon) newAutomationReads() *automationReads {
	if d.store == nil {
		return nil
	}
	return newAutomationReadsFor(d.store)
}

// newAutomationReadsFor is the one construction of the read adapter. The
// EnvSet/DirExists seams are presence-only (AN1): there is deliberately no
// function here that returns an environment VALUE.
func newAutomationReadsFor(s *store.Store) *automationReads {
	return &automationReads{
		s:  s,
		At: time.Now,
		EnvSet: func(name string) bool {
			if name == "" {
				return false
			}
			v, ok := os.LookupEnv(name)
			return ok && v != ""
		},
		DirExists: func(path string) bool {
			if path == "" {
				return true // no workspace named: nothing to be missing
			}
			fi, err := os.Stat(path)
			return err == nil && fi.IsDir()
		},
	}
}

// ── rows → views ──────────────────────────────────────────────────────────

// schedView maps one S20 row into the unified view. Kind carries the family
// ("schedule" | "webhook"); the row has no enabled column — the switch the
// engine reads IS paused (AN6: paused and archived are both engine-invisible
// states, and `enabled` on the wire reads as the engine's view of the
// switch). Name is the id (S20 rows carry no separate name).
func schedView(r store.TriggerScheduleRow) protocol.AutomationView {
	v := protocol.AutomationView{
		Source:         r.Kind,
		ID:             r.ID,
		Name:           r.ID,
		Blueprint:      r.Blueprint,
		Workspace:      r.Workspace,
		Goal:           r.Goal,
		Expr:           r.Expr,
		Zone:           r.Zone,
		SecretEnv:      r.SecretEnv,
		EventFilter:    r.EventFilter,
		Enabled:        !r.Paused,
		Paused:         r.Paused,
		Archived:       r.ArchivedAt != "",
		ArchivedReason: r.ArchivedReason,
		CreatedByKind:  r.CreatedByKind,
		CreatedByID:    r.CreatedByID,
		CreatedAt:      r.CreatedAt,
	}
	if r.WindowStart != "" || r.WindowEnd != "" || r.WindowDays != "" {
		v.Window = &protocol.AutomationWindowView{Start: r.WindowStart, End: r.WindowEnd, Days: r.WindowDays}
	}
	return v
}

// eventView maps one S40 row in. `enabled` is the row's own column (the
// dispatcher reads it); Match rides as the stored canonical bytes (AU6: the
// store keeps the canonical form, so what the socket carries is the rule as
// saved, not a re-canonicalisation).
func eventView(t store.Trigger) protocol.AutomationView {
	v := protocol.AutomationView{
		Source:           "event",
		ID:               t.ID,
		Name:             t.Name,
		Blueprint:        t.Blueprint,
		RootID:           t.RootID,
		ComponentID:      t.ComponentID,
		Match:            json.RawMessage(t.MatchJSON),
		Kickoff:          t.Kickoff,
		MaxRunsPerHour:   t.MaxRunsPerHour,
		MaxCostPerDayUSD: t.MaxCostPerDayUSD,
		Enabled:          t.Enabled,
		DisabledReason:   t.DisabledReason,
		Archived:         t.ArchivedAt != "",
		ArchivedReason:   t.ArchivedReason,
		CreatedByKind:    t.CreatedByKind,
		CreatedByID:      t.CreatedByID,
		CreatedAt:        t.CreatedAt,
		UpdatedAt:        t.UpdatedAt,
	}
	return v
}

// allRows gathers every automation of both families into views plus the
// description each one earns. includeArchived is AN6's list switch. It is
// the ONE row-gathering the list, the live total, the unknown-id nearest
// names, and (through findRow) every single-automation read share — one
// read path, so no two answers can disagree about what exists.
func (a *automationReads) allRows(ctx context.Context, includeArchived bool) ([]protocol.AutomationView, error) {
	scheds, err := a.s.TriggerSchedulesScoped(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	events, err := a.s.ListEventTriggers(store.EventTriggerFilter{IncludeArchived: includeArchived})
	if err != nil {
		return nil, err
	}
	out := make([]protocol.AutomationView, 0, len(scheds)+len(events))
	for _, r := range scheds {
		out = append(out, schedView(r))
	}
	for _, t := range events {
		out = append(out, eventView(t))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// describe is AN5's sentence — automationview.Describe over the row's own
// fields. The event family's match rule needs its parsed form; a rule that
// will not parse (a legacy row) renders its keys absent, and the health
// read is the surface that must say the rule is broken, not this one.
func describe(v protocol.AutomationView) string {
	av := automationview.View{
		Source: v.Source, ID: v.ID, Expr: v.Expr, Zone: v.Zone,
		Blueprint: v.Blueprint, Workspace: v.Workspace, Goal: v.Goal, SecretEnv: v.SecretEnv,
		MaxRunsPerHour: v.MaxRunsPerHour, Enabled: v.Enabled, Paused: v.Paused,
		Archived: v.Archived, DisabledReason: v.DisabledReason,
	}
	if v.MaxCostPerDayUSD != nil {
		av.MaxCostPerDayUSD = *v.MaxCostPerDayUSD
	}
	if v.Window != nil {
		av.WindowStart, av.WindowEnd, av.WindowDays = v.Window.Start, v.Window.End, v.Window.Days
	}
	if len(v.Match) > 0 {
		var m struct {
			Event      string   `json:"event"`
			Root       []string `json:"root"`
			Component  []string `json:"component"`
			GoalType   []string `json:"goal_type"`
			TaskType   []string `json:"task_type"`
			SourceKind []string `json:"source_kind"`
		}
		if err := json.Unmarshal(v.Match, &m); err == nil {
			av.Match = automationview.MatchView{
				Event: m.Event, Root: m.Root, Component: m.Component,
				GoalType: m.GoalType, TaskType: m.TaskType, SourceKind: m.SourceKind,
			}
		}
	}
	return automationview.Describe(av)
}

// findRow locates one automation by source+id (archived included — an
// archived automation still has everything to show) and answers the view
// ready for the wire, or protocol.ErrAutomationUnknown naming the nearest
// ids the store DOES carry: a typo reads as a missing thing, never as an
// empty answer.
func (a *automationReads) findRow(ctx context.Context, source, id string) (protocol.AutomationView, error) {
	rows, err := a.allRows(ctx, true)
	if err != nil {
		return protocol.AutomationView{}, err
	}
	for _, v := range rows {
		if v.Source == source && v.ID == id {
			return v, nil
		}
	}
	// The id may exist UNDER ANOTHER SOURCE — the source is part of the
	// name (AN1), and an id that lives across the hall is a different
	// answer from an id that lives nowhere.
	for _, v := range rows {
		if v.ID == id {
			return protocol.AutomationView{}, protocol.ErrAutomationUnknown.WithWhat(
				"%q is a %s, not the %s named — the source is part of the name", id, v.Source, source)
		}
	}
	nearest := nearestIDs(rows, source, id)
	what := fmt.Sprintf("no %s automation %q exists", source, id)
	if len(nearest) > 0 {
		what = fmt.Sprintf("%s — nearest %s", what, strings.Join(nearest, ", "))
	}
	return protocol.AutomationView{}, protocol.ErrAutomationUnknown.WithWhat("%s", what)
}

// nearestIDs names up to three ids the store carries that could be what
// was meant: same-source first, ranked by shared prefix, then the source's
// own first ids so the caller always gets somewhere to look.
func nearestIDs(rows []protocol.AutomationView, source, id string) []string {
	type scored struct {
		id    string
		score int
	}
	var cands []scored
	for _, v := range rows {
		if v.Source != source {
			continue
		}
		score := 0
		for i := 0; i < len(id) && i < len(v.ID) && id[i] == v.ID[i]; i++ {
			score++
		}
		cands = append(cands, scored{id: v.ID, score: score})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].id < cands[j].id
	})
	var out []string
	for _, c := range cands {
		if len(out) == 3 {
			break
		}
		out = append(out, fmt.Sprintf("%q", c.id))
	}
	return out
}

// lastFire answers one automation's most recent recorded answer through
// the unified fire log (T1's view) — LIMIT 1 on the same read the fires
// page uses, so "last fire" and "the log" cannot disagree.
func (a *automationReads) lastFire(ctx context.Context, source, id string) (*protocol.AutomationFireView, error) {
	rows, _, err := a.s.AutomationFires(ctx, store.FireViewFilter{Source: source, TriggerID: id, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	fv := unifiedFireView(rows[0])
	return &fv, nil
}

func unifiedFireView(r store.FireViewRow) protocol.AutomationFireView {
	return protocol.AutomationFireView{
		Source: r.Source, TriggerID: r.TriggerID, FireID: r.FID, At: r.At,
		Outcome: r.Outcome, Reason: r.Reason, LoopID: r.LoopID, LoopSlug: r.LoopSlug,
		SubjectKind: r.SubjectKind, SubjectID: r.SubjectID, Depth: r.Depth, CostNote: r.CostNote,
	}
}

// ── the seam: row reads ───────────────────────────────────────────────────

// ListAutomations gathers, filters (daemon-side — one rule for both
// families, no per-store filter drift) and answers the live total the
// handler needs to tell the two empties apart.
func (a *automationReads) ListAutomations(ctx context.Context, p protocol.AutomationListParams) ([]protocol.AutomationView, int, error) {
	include := p.Archived == "include" || p.Archived == "only"
	rows, err := a.allRows(ctx, include)
	if err != nil {
		return nil, 0, err
	}
	live, err := a.allRows(ctx, false)
	if err != nil {
		return nil, 0, err
	}
	liveTotal := len(live)

	q := strings.ToLower(p.Q)
	out := make([]protocol.AutomationView, 0, len(rows))
	for _, v := range rows {
		switch p.Archived {
		case "", "exclude":
			if v.Archived {
				continue
			}
		case "only":
			if !v.Archived {
				continue
			}
		}
		if p.Source != "" && v.Source != p.Source {
			continue
		}
		if p.Enabled != nil && v.Enabled != *p.Enabled {
			continue
		}
		if p.Blueprint != "" && v.Blueprint != p.Blueprint {
			continue
		}
		if p.RootID != "" && v.RootID != p.RootID {
			continue
		}
		if p.ComponentID != "" && v.ComponentID != p.ComponentID {
			continue
		}
		if q != "" {
			hay := strings.ToLower(v.Name + " " + v.ID + " " + describe(v))
			if !strings.Contains(hay, q) {
				continue
			}
		}
		lf, err := a.lastFire(ctx, v.Source, v.ID)
		if err != nil {
			return nil, 0, err
		}
		v.LastFire = lf
		out = append(out, v)
	}
	return out, liveTotal, nil
}

// ShowAutomation answers the whole page: the row, its canonical family
// object, the sentence, its health, its last five fires and last five
// changes. Health rides the same composer the listing uses — one verdict
// per row, computed fresh (N8).
func (a *automationReads) ShowAutomation(ctx context.Context, p protocol.AutomationShowParams) (protocol.AutomationShowResult, error) {
	v, err := a.findRow(ctx, p.Source, p.ID)
	if err != nil {
		return protocol.AutomationShowResult{}, err
	}
	res := protocol.AutomationShowResult{
		Automation:  v,
		Description: describe(v),
		RecentFires: []protocol.AutomationFireView{},
		Changes:     []protocol.AutomationChangeView{},
	}
	switch v.Source {
	case "schedule":
		res.Schedule = &protocol.AutomationCanonical{Expr: v.Expr, Zone: v.Zone, Window: v.Window}
	case "webhook":
		res.Webhook = &protocol.AutomationCanonical{SecretEnv: v.SecretEnv}
	case "event":
		res.Event = &protocol.AutomationCanonical{Match: v.Match, Kickoff: v.Kickoff}
	}
	lf, err := a.lastFire(ctx, v.Source, v.ID)
	if err != nil {
		return res, err
	}
	res.Automation.LastFire = lf

	fires, _, err := a.s.AutomationFires(ctx, store.FireViewFilter{Source: v.Source, TriggerID: v.ID, Limit: 5})
	if err != nil {
		return res, err
	}
	for _, r := range fires {
		res.RecentFires = append(res.RecentFires, unifiedFireView(r))
	}
	changes, err := a.s.AutomationChangeList(ctx, v.Source, v.ID, 5)
	if err != nil {
		return res, err
	}
	for _, c := range changes {
		res.Changes = append(res.Changes, automationChangeView(c))
	}
	hs, err := a.healthFor(ctx, []protocol.AutomationView{res.Automation})
	if err != nil {
		return res, err
	}
	res.Health = hs[0]
	return res, nil
}

// ListAutomationFires answers the unified log through T1's view — the
// differential test pins that this equals both tables unioned; this
// function adds no filter of its own beyond the cursor and the store's.
func (a *automationReads) ListAutomationFires(ctx context.Context, p protocol.AutomationFiresParams) ([]protocol.AutomationFireView, protocol.AutomationFireCounts, string, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	rows, counts, err := a.s.AutomationFires(ctx, store.FireViewFilter{
		Source: p.Source, TriggerID: p.ID, Outcome: p.Outcome, Limit: limit, BeforeID: p.BeforeId,
	})
	if err != nil {
		return nil, protocol.AutomationFireCounts{}, "", err
	}
	out := make([]protocol.AutomationFireView, 0, len(rows))
	for _, r := range rows {
		out = append(out, unifiedFireView(r))
	}
	c := protocol.AutomationFireCounts{
		Started:     counts["started"],
		Refused:     counts["refused"],
		Limited:     counts["limited"],
		Skipped:     counts["skipped"],
		CouldNotSay: counts["could-not-say"],
	}
	next := ""
	if len(rows) == limit && len(rows) > 0 {
		last := rows[len(rows)-1]
		next = fmt.Sprintf("%s|%s:%d", last.At, last.Source, last.FID)
	}
	return out, c, next, nil
}

func (a *automationReads) AutomationRuns(ctx context.Context, p protocol.AutomationRunsParams) (protocol.AutomationRunsResult, error) {
	rows, total, err := a.s.AutomationRuns(ctx, p.Source, p.ID, p.Limit)
	if err != nil {
		// An id no family carries has no runs to count — the same refusal
		// the show page answers, so a typo never reads as "it ran nothing".
		if _, ferr := a.findRow(ctx, p.Source, p.ID); ferr != nil {
			return protocol.AutomationRunsResult{}, ferr
		}
		return protocol.AutomationRunsResult{}, err
	}
	res := protocol.AutomationRunsResult{Total: total, Loops: make([]protocol.AutomationRunView, 0, len(rows))}
	if total == 0 && len(rows) == 0 {
		// An empty answer is only true if the automation EXISTS; a typo
		// must refuse, not report peace (the same sentence `show` speaks).
		if _, ferr := a.findRow(ctx, p.Source, p.ID); ferr != nil {
			return protocol.AutomationRunsResult{}, ferr
		}
	}
	for _, r := range rows {
		res.Loops = append(res.Loops, protocol.AutomationRunView{ID: r.ID, Slug: r.Slug, Status: r.Status, StartedAt: r.StartedAt})
	}
	return res, nil
}

func automationChangeView(c store.AutomationChange) protocol.AutomationChangeView {
	return protocol.AutomationChangeView{
		ID: c.ID, Source: c.Source, TriggerID: c.TriggerID, Op: c.Op, Summary: c.Summary,
		ActorKind: c.ActorKind, ActorID: c.ActorID, OccurredAt: c.OccurredAt,
	}
}

func (a *automationReads) ListAutomationChanges(ctx context.Context, p protocol.AutomationChangesParams) ([]protocol.AutomationChangeView, error) {
	rows, err := a.s.AutomationChangeList(ctx, p.Source, p.ID, p.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.AutomationChangeView, 0, len(rows))
	for _, c := range rows {
		out = append(out, automationChangeView(c))
	}
	return out, nil
}

// ── the seam: the advisory three ──────────────────────────────────────────

// PreviewAutomations walks the engine's OWN clock (through
// automationview.NextFires, which is trigger.PreviewFires — T2's wrapper).
// A stored webhook or event answers the refusal the engine itself speaks
// (the webhook's sentence comes FROM trigger.PreviewFires, verbatim); a
// refused spec answers ErrAutomationPreview carrying the loader's sentence
// verbatim — the preview is honest by BEING the engine (AN8).
func (a *automationReads) PreviewAutomations(ctx context.Context, p protocol.AutomationPreviewParams) ([]protocol.AutomationInstant, error) {
	from, n, err := previewFromN(a.At(), p)
	if err != nil {
		return nil, err
	}
	spec := automationview.ScheduleSpec{}
	if p.Spec != nil {
		spec = specOfDraft(p.Spec)
	} else {
		// An id: only the clock families have one. The lookup uses the
		// global row read; a webhook answers through the engine's own
		// refusal below, an event answers the sentence beside it.
		v, err := a.findRowByID(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		switch v.Source {
		case "event":
			return nil, protocol.ErrBadParams{Err: fmt.Errorf("%q is an event trigger — it fires when its event happens, never on a clock", p.ID)}
		case "webhook":
			// The engine's own webhook refusal, verbatim: PreviewFires
			// refuses a webhook-shaped schedule before walking it.
			_, err := trigger.PreviewFires(trigger.Schedule{ID: v.ID, Kind: trigger.KindWebhook, Expr: v.Expr, Zone: v.Zone}, from, n)
			return nil, protocol.ErrBadParams{Err: err}
		}
		spec.ID = v.ID
		spec.Expr, spec.Zone = v.Expr, v.Zone
		if v.Window != nil {
			spec.WindowStart, spec.WindowEnd, spec.WindowDays = v.Window.Start, v.Window.End, v.Window.Days
		}
	}
	return walkPreviewSpec(spec, from, n)
}

// previewFromN is the ONE from/n derivation of a preview walk (defaults
// and the RFC3339 refusal), given the moment to start from — the daemon's
// At() over the socket, time.Now over the store-free CLI road (S49 T8).
func previewFromN(now time.Time, p protocol.AutomationPreviewParams) (time.Time, int, error) {
	n := p.N
	if n <= 0 {
		n = 5
	}
	from := now.UTC()
	if p.From != "" {
		t, err := time.Parse(time.RFC3339Nano, p.From)
		if err != nil {
			return time.Time{}, 0, protocol.ErrBadParams{Err: fmt.Errorf("`from` is not an RFC3339 instant: %w", err)}
		}
		from = t
	}
	return from, n, nil
}

// specOfDraft is the ONE draft-spec → engine-spec mapping, run by the
// stored-row and store-free roads alike.
func specOfDraft(s *protocol.AutomationPreviewSpec) automationview.ScheduleSpec {
	spec := automationview.ScheduleSpec{Expr: s.Expr, Zone: s.Zone}
	if s.Window != nil {
		spec.WindowStart, spec.WindowEnd, spec.WindowDays = s.Window.Start, s.Window.End, s.Window.Days
	}
	return spec
}

// walkPreviewSpec is the ONE walk and shaping: same engine, same
// instant view, same refusal dressing, whoever asks.
func walkPreviewSpec(spec automationview.ScheduleSpec, from time.Time, n int) ([]protocol.AutomationInstant, error) {
	inst, err := automationview.NextFires(spec, from, n)
	if err != nil {
		return nil, protocol.ErrAutomationPreview.WithWhat("%s", engineSentence(err))
	}
	out := make([]protocol.AutomationInstant, 0, len(inst))
	for _, in := range inst {
		out = append(out, protocol.AutomationInstant{
			At: in.At.UTC().Format(time.RFC3339Nano), Kind: in.Kind, Note: in.Note, Wall: in.Wall,
		})
	}
	return out, nil
}

// PreviewDraftSpec is the store-free preview road (S49 T8): the draft-spec
// branch of the seam, exported — the CLI's `automation preview --cron`
// walks THIS, with no store opened and no client-side cron, and the
// instants it answers are the same walk, the same shaping and the same
// refusal sentences the socket answers.
func PreviewDraftSpec(p protocol.AutomationPreviewParams) ([]protocol.AutomationInstant, error) {
	if p.Spec == nil {
		return nil, protocol.ErrBadParams{Err: fmt.Errorf("a draft preview walks a `spec` — the id road belongs to a store")}
	}
	from, n, err := previewFromN(time.Now(), p)
	if err != nil {
		return nil, err
	}
	return walkPreviewSpec(specOfDraft(p.Spec), from, n)
}

// findRowByID is the source-free lookup the preview and the dry run need:
// ids are unique across families (the row CHECKs carry the prefixes), so
// the id alone names the automation; an unknown id answers with the ids
// nearest across ALL sources.
func (a *automationReads) findRowByID(ctx context.Context, id string) (protocol.AutomationView, error) {
	rows, err := a.allRows(ctx, true)
	if err != nil {
		return protocol.AutomationView{}, err
	}
	for _, v := range rows {
		if v.ID == id {
			return v, nil
		}
	}
	var near []string
	for _, src := range automationSources {
		if n := nearestIDs(rows, src, id); len(n) > 0 {
			near = n
			break
		}
	}
	what := fmt.Sprintf("no automation %q exists", id)
	if len(near) > 0 {
		what = fmt.Sprintf("%s — nearest %s", what, strings.Join(near, ", "))
	}
	return protocol.AutomationView{}, protocol.ErrAutomationUnknown.WithWhat("%s", what)
}

// engineSentence strips the package prefix the engine's errors carry — the
// sentence stays the engine's, the prefix is Go's calling convention, not
// the answer.
func engineSentence(err error) string {
	s := err.Error()
	if i := strings.Index(s, ": "); i >= 0 && !strings.Contains(s[:i], " ") {
		return s[i+2:]
	}
	return s
}

// DryRunAutomation is the pure scan (behaviour note 1): outbox rows from
// the given (or the trigger's own) cursor, each run through the row's
// OWN parsed match — store.Trigger.Admits, the dispatcher's very function —
// and NOTHING else happens. No claim, no insert, the cursor stays where it
// was: T3's test counts every table before and after and demands equality.
func (a *automationReads) DryRunAutomation(ctx context.Context, p protocol.AutomationDryRunParams) (protocol.AutomationDryRunResult, error) {
	events, err := a.s.ListEventTriggers(store.EventTriggerFilter{IncludeArchived: true})
	if err != nil {
		return protocol.AutomationDryRunResult{}, err
	}
	var tr *store.Trigger
	for i := range events {
		if events[i].ID == p.ID {
			tr = &events[i]
			break
		}
	}
	if tr == nil {
		v, err := a.findRowByID(ctx, p.ID)
		if err == nil {
			// It exists — just not as an event trigger: a dry run is the
			// event family's verdict on the events waiting now, and the
			// source is part of the name (AN1).
			return protocol.AutomationDryRunResult{}, protocol.ErrBadParams{
				Err: fmt.Errorf("%q is a %s — a dry run is an event trigger's verdict on the events waiting now", p.ID, v.Source),
			}
		}
		return protocol.AutomationDryRunResult{}, err
	}
	after := tr.FromOutboxID
	if p.SinceOutboxId != nil {
		after = *p.SinceOutboxId
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := a.s.ReadAutomationEvents(after, limit)
	if err != nil {
		return protocol.AutomationDryRunResult{}, err
	}
	res := protocol.AutomationDryRunResult{From: after, Scanned: len(rows), WouldMatch: []protocol.AutomationWouldMatch{}}
	for _, ev := range rows {
		if !tr.Admits(ev) {
			continue
		}
		res.WouldMatch = append(res.WouldMatch, protocol.AutomationWouldMatch{
			OutboxID: ev.ID, Event: ev.Event, SubjectKind: ev.SubjectKind, SubjectID: ev.SubjectID, At: ev.CreatedAt,
		})
	}
	return res, nil
}

// automationSources is the closed three (AN2) as the row-gather needs it —
// the same list the protocol refuses filters outside of, kept beside the
// row read that walks families in it.
var automationSources = []string{"schedule", "webhook", "event"}

// ── the proposal.get preview (S49 T6) ─────────────────────────────────────

// ProposalPreview is the protocol.AutomationPreview seam for kind
// automation_disable: over the proposal's payload alone it answers the
// automation's AN5 Describe sentence (automationview's, never a
// paraphrase), the row's CURRENT state (a person commits a disable of a
// row that moved since the ask only knowingly), the LAST FIVE fires in the
// log's own newest-first words, and the health verdict the desk renders
// beside the row — every part the SAME read service answers the ten doors
// with, over this payload (P34: one door, one answer). READ-ONLY in full:
// a row read, the fire log read, the health inputs read — nothing is
// written, nothing is gated (AA4: a preview that could gate is the second
// authority AN8 forbids). An unreadable payload, an unknown automation, or
// a failing read answers the refusal and no preview — proposal.get omits
// the field rather than fail the read of the row.
func (a *automationReads) ProposalPreview(ctx context.Context, payload []byte) (*protocol.AutomationProposalPreview, string, error) {
	var p struct {
		Source string `json:"source"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, "the payload does not decode", err
	}
	if p.ID == "" {
		return nil, "the payload names no automation", fmt.Errorf("preview: automation_disable payload names %q/%q — the source is part of the name", p.Source, p.ID)
	}
	known := false
	for _, s := range automationSources {
		if s == p.Source {
			known = true
			break
		}
	}
	if !known {
		return nil, "the payload names no automation", fmt.Errorf("preview: automation_disable payload source %q is outside the closed three", p.Source)
	}
	view, err := a.findRow(ctx, p.Source, p.ID)
	if err != nil {
		return nil, "", err // ErrAutomationUnknown — the desk's own missing-row sentence
	}
	out := &protocol.AutomationProposalPreview{
		Source:   view.Source,
		ID:       view.ID,
		Describe: describe(view),
		Enabled:  view.Enabled,
		Paused:   view.Paused,
		Archived: view.Archived,
	}
	fires, _, _, err := a.ListAutomationFires(ctx, protocol.AutomationFiresParams{Source: p.Source, ID: p.ID, Limit: 5})
	if err != nil {
		return nil, "", err
	}
	out.LastFires = fires
	health, err := a.healthFor(ctx, []protocol.AutomationView{view})
	if err != nil {
		return nil, "", err
	}
	if len(health) > 0 {
		h := health[0]
		out.Health = &h
	}
	return out, "", nil
}

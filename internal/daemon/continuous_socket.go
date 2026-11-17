package daemon

// S27-continuous T7: the socket side of the continuous scheduler.
//
// continuousSocket is the service the twelve `scheduler.*` doors speak
// through. It is thin by law: every fact is read through the store or the
// adapter the worker itself uses, every refusal carries the validator's or
// the store's own sentence (CN20's no-paraphrase rule), and the one place a
// sentence is WRITTEN here — the statement — is the daemon's own CN2 words
// that `scheduler.status` quotes and every client repeats.
//
// It exists exactly over a store: no store means no authority rows, no S55
// control ledger to stop loops through, and no world to preview — the V13
// rule that leaves the whole surface ABSENT rather than refusing.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/continuous"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// schedulerStatement is CN2's confirmation in the daemon's own words:
// enabling states what it grants, once, and every surface quotes THIS
// string (RM9's law — one sentence, no client writes its own).
const schedulerStatement = "enabling lets the scheduler start loops unattended in the repositories whose rows it holds, under the permissions those loops carry; it grants nothing beyond these rows, inherits nothing down the tree, and every act is audited to the person who enabled it (S27 CN2)"

// continuousSocket implements protocol.ContinuousAPI over the daemon.
type continuousSocket struct {
	d *Daemon
	s *store.Store
}

// NewContinuousService is the V13 nil guard: the service exists exactly
// over a wired store — the same condition that wires S55's loop.stop, so
// `scheduler.stopAll` can never stand without it.
func NewContinuousService(d *Daemon) protocol.ContinuousAPI {
	if d == nil || d.store == nil {
		return nil
	}
	return &continuousSocket{d: d, s: d.store}
}

// resolveRoot reads the {componentRoot | "root:<id>"} filter the preview
// and attention doors forward: a rootId arrives prefixed by the door (the
// two spellings of one repository), and an unresolvable id is a typed
// refusal naming it — never an empty answer dressed as "nothing needs you".
func (c *continuousSocket) resolveRoot(filter string) (string, error) {
	if !strings.HasPrefix(filter, "root:") {
		return filter, nil
	}
	id := strings.TrimPrefix(filter, "root:")
	comp, err := c.s.ReadComponent(id)
	if err != nil {
		return "", protocol.ErrSchedulerRefused.WithWhat("%s", fmt.Sprintf(
			"rootId %q names no component: %v", id, err))
	}
	if comp.Root == "" {
		return "", protocol.ErrSchedulerRefused.WithWhat("%s", fmt.Sprintf(
			"component %q has no repository root of its own (CT3) — name the component that has one", id))
	}
	return comp.Root, nil
}

func schedulerStoreActor(actor protocol.ContinuousActor) store.SchedulerActor {
	return store.SchedulerActor{Kind: actor.Kind, ID: actor.ID}
}

// ---------------------------------------------------------------------------
// the reads
// ---------------------------------------------------------------------------

func (c *continuousSocket) Status(ctx context.Context) (protocol.ContinuousStatusView, error) {
	st, err := c.s.SchedulerState()
	if err != nil {
		return protocol.ContinuousStatusView{}, err
	}
	adp := c.d.continuousAdapterRef()
	settings, err := adp.Load(ctx)
	if err != nil {
		return protocol.ContinuousStatusView{}, err
	}
	view := protocol.ContinuousStatusView{
		Enabled:      st.Enabled,
		Paused:       st.Paused,
		PausedReason: st.PausedReason,
		Settings:     c.d.schedulerSettingsAnswer(ctx),
		// A list is always a list on the wire: no enrolled repository answers [], never null.
		Repos:     []protocol.ContinuousRepoView{},
		Slots:     protocol.ContinuousSlots{Used: adp.usedWeights(), Cap: settings.MaxSlots},
		Statement: schedulerStatement,
		Descriptor: protocol.ContinuousDescriptor{
			PriorityStates: []string{"known", "assumed"},
			Reasons:        reasonWords(continuous.Reasons()),
			WindowGrammar:  "start and end are an HH:MM pair (\"09:00\",\"17:30\"); days is a comma list of ISO weekdays (\"1,3,5\", 1 is Monday); zone is an IANA name (\"Europe/Lisbon\"); absent means always",
			BlueprintsFrom: "blueprint.list",
		},
	}
	if st.Enabled && !st.Paused {
		// The SCHEDULED next tick. A reactor kick can arrive earlier —
		// this is the cadence's answer, not a promise about the earliest
		// possible one (the worker owns no readable clock).
		view.NextTickAt = time.Now().UTC().Add(settings.TickEvery()).Format(time.RFC3339)
	}
	if view.Settings == nil {
		view.Settings = []protocol.ContinuousSetting{}
	}
	rows, err := c.s.SchedulerRoots()
	if err != nil {
		return view, err
	}
	now := time.Now().UTC()
	for _, row := range rows {
		view.Repos = append(view.Repos, c.repoStatus(ctx, adp, st, row, now))
	}
	att, aerr := c.Attention(ctx, "")
	if aerr == nil {
		view.Attention = protocol.ContinuousAttentionSum{Open: att.Open}
	}
	return view, nil
}

// repoStatus reads one repository's row and derives its live state word.
func (c *continuousSocket) repoStatus(ctx context.Context, adp *continuousAdapter, st store.SchedulerStateRow, row store.SchedulerRootRow, now time.Time) protocol.ContinuousRepoView {
	v := protocol.ContinuousRepoView{
		ComponentRoot: row.ComponentRoot,
		Enabled:       row.Enabled,
		MinPriority:   row.MinPriority,
		Template: protocol.ContinuousTemplate{
			Blueprint: row.Blueprint,
			Tech:      row.Tech,
		},
	}
	if row.WithJSON != "" && row.WithJSON != "{}" {
		v.Template.With = json.RawMessage(row.WithJSON)
	}
	if row.WindowStart != "" || row.WindowEnd != "" {
		v.Window = &protocol.ContinuousWindow{Start: row.WindowStart, End: row.WindowEnd, Days: row.WindowDays, Zone: row.Zone}
	}
	if comp, err := c.s.ComponentForRoot(row.ComponentRoot); err == nil {
		v.RootID = comp.ID
	}
	if last, err := c.s.Decisions(store.DecisionQuery{ComponentRoot: row.ComponentRoot, Decision: "started", Limit: 1}); err == nil && len(last) > 0 {
		v.LastStartAt = last[0].At
	}
	core := adp.repoView(ctx, row, now)
	v.State = repoStateWord(st, core, now)
	return v
}

// repoStateWord is the derivation, ordered as the tick orders its gates
// (master, pause, the row, the template, the window, the occupancy, the
// budget, the work), so the panel and the next tick cannot disagree about
// why a repository is what it is.
func repoStateWord(st store.SchedulerStateRow, core continuous.Repo, now time.Time) protocol.ContinuousRepoState {
	switch {
	case !core.Enabled:
		return protocol.ContinuousRepoState{Kind: "off", Reason: "repo-off"}
	case !st.Enabled:
		return protocol.ContinuousRepoState{Kind: "master-off", Reason: "master-off"}
	case st.Paused:
		return protocol.ContinuousRepoState{Kind: "paused", Reason: "paused"}
	case !core.TemplateOK && core.Template != "":
		return protocol.ContinuousRepoState{Kind: "template-invalid", Reason: "template-invalid",
			Detail: "the blueprint named by the row does not load and validate — the pick would refuse template-invalid"}
	}
	if open, detail := core.Window.InWindow(now); !open {
		return protocol.ContinuousRepoState{Kind: "out-of-window", Reason: "window-closed", Detail: detail}
	}
	if len(core.Occupying) > 0 {
		names := make([]string, 0, len(core.Occupying))
		for _, l := range core.Occupying {
			names = append(names, l.Slug+" ("+l.State+")")
		}
		return protocol.ContinuousRepoState{Kind: "occupied", Reason: "repo-occupied",
			Detail: strings.Join(names, ", ")}
	}
	if !core.BudgetOK {
		return protocol.ContinuousRepoState{Kind: "budget-exhausted", Reason: "budget-exhausted", Detail: core.BudgetReason}
	}
	if core.QueueHead != nil || len(core.Ready) > 0 {
		return protocol.ContinuousRepoState{Kind: "ready"}
	}
	return protocol.ContinuousRepoState{Kind: "idle"}
}

func reasonWords(rs []continuous.Reason) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, string(r))
	}
	return out
}

// Preview runs the tick, not a description of it (CN15): one Snapshot
// through the worker's own adapter, one Plan, zero writes. A filtered
// preview answers THAT repository's lines with the GLOBAL slots still
// spent — the filter hides other repositories from the answer, it does not
// free their loops.
func (c *continuousSocket) Preview(ctx context.Context, componentRoot string) (protocol.ContinuousPreview, error) {
	root, err := c.resolveRoot(componentRoot)
	if err != nil {
		return protocol.ContinuousPreview{}, err
	}
	adp := c.d.continuousAdapterRef()
	snap, err := adp.Snapshot(ctx, time.Now().UTC())
	if err != nil {
		return protocol.ContinuousPreview{}, err
	}
	if root != "" {
		kept := snap.Repos[:0]
		for _, r := range snap.Repos {
			if r.Root == root {
				kept = append(kept, r)
			}
		}
		snap.Repos = kept
	}
	rep := continuous.Preview(snap)
	out := protocol.ContinuousPreview{Now: snap.Now.Format(time.RFC3339)}
	for _, d := range rep.Decisions {
		out.Decisions = append(out.Decisions, previewLine(d))
	}
	for _, d := range rep.WouldStart {
		out.WouldStart = append(out.WouldStart, previewLine(d))
	}
	return out, nil
}

func previewLine(d continuous.Decision) protocol.ContinuousPreviewLine {
	return protocol.ContinuousPreviewLine{
		ComponentRoot: d.Root,
		SubjectKind:   d.Subject.Kind,
		SubjectID:     d.Subject.ID,
		Verdict:       string(d.Verdict),
		Reason:        string(d.Reason),
		Detail:        d.Detail,
	}
}

func (c *continuousSocket) Decisions(ctx context.Context, q protocol.ContinuousDecisionQuery) ([]protocol.ContinuousDecisionRow, error) {
	rows, err := c.s.Decisions(store.DecisionQuery{
		ComponentRoot: q.ComponentRoot,
		SubjectKind:   q.SubjectKind,
		SubjectID:     q.SubjectID,
		HasSubject:    q.HasSubject,
		Decision:      q.Decision,
		Reason:        q.Reason,
		Since:         q.Since,
		Limit:         q.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]protocol.ContinuousDecisionRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, protocol.ContinuousDecisionRow{
			ID: r.ID, At: r.At, ComponentRoot: r.ComponentRoot,
			SubjectKind: r.SubjectKind, SubjectID: r.SubjectID,
			Decision: r.Decision, Reason: r.Reason, Summary: r.Summary,
			GrantRevision: r.GrantRevision, ActorKind: r.ActorKind,
			ActorID: r.ActorID, LoopSlug: r.LoopSlug,
		})
	}
	return out, nil
}

// Attention answers CN13's OPEN half: every needs-you line whose loop is
// STILL blocked or waiting. The openness is derived from the loop row at
// read time (DB6: never a stored flag that can disagree), and the kind is
// displayed with the one exception the design grants it — cap-hit rides
// S55's raise sentence (attentionNotes), because raising is a different
// act from retrying.
func (c *continuousSocket) Attention(ctx context.Context, componentRoot string) (protocol.ContinuousAttention, error) {
	root, err := c.resolveRoot(componentRoot)
	if err != nil {
		return protocol.ContinuousAttention{}, err
	}
	rows, err := c.s.Decisions(store.DecisionQuery{Decision: "needs-you", ComponentRoot: root, Limit: 200})
	if err != nil {
		return protocol.ContinuousAttention{}, err
	}
	out := protocol.ContinuousAttention{Items: []protocol.ContinuousAttentionItem{}}
	for _, d := range rows {
		if d.LoopSlug == "" {
			continue
		}
		l, err := c.s.GetLoop(store.Scope{LoopSlug: d.LoopSlug, ComponentRoot: d.ComponentRoot})
		if err != nil {
			continue // the loop row is gone — the line closed itself
		}
		if l.Status != "blocked" && l.Status != "waiting" {
			continue // derived openness: the loop moved, the need moved with it
		}
		kind, note := attentionDisplay(d.Reason)
		out.Items = append(out.Items, protocol.ContinuousAttentionItem{
			DecisionID: d.ID, LoopSlug: d.LoopSlug, ComponentRoot: d.ComponentRoot,
			SubjectKind: d.SubjectKind, SubjectID: d.SubjectID,
			Kind: kind, Note: note, Since: d.At,
		})
	}
	out.Open = len(out.Items)
	return out, nil
}

// ---------------------------------------------------------------------------
// the master and the pause
// ---------------------------------------------------------------------------

// SetMaster moves the master row and nothing else. Enable deliberately
// does NOT touch a single repository row: one act, one authority (CN2) —
// the repositories stay as their own rows say, and TestEnableNeedsTheMas-
// terAndTheRepo holds that line.
func (c *continuousSocket) SetMaster(ctx context.Context, actor protocol.ContinuousActor, enabled bool) (*protocol.ContinuousRefusal, error) {
	st, err := c.s.SchedulerState()
	if err != nil {
		return nil, err
	}
	if _, err := c.s.SetSchedulerState(schedulerStoreActor(actor), enabled, st.Paused, st.PausedReason); err != nil {
		return nil, err
	}
	return nil, nil
}

// SetPaused is CN18's operational off: it works only while the master is
// ON (a control needing a master that is off is the composition the door
// refuses — ErrSchedulerDisabled says why), and what it changes is PICKING
// alone: running loops are none the wiser.
func (c *continuousSocket) SetPaused(ctx context.Context, actor protocol.ContinuousActor, paused bool, reason string) (*protocol.ContinuousRefusal, error) {
	st, err := c.s.SchedulerState()
	if err != nil {
		return nil, err
	}
	if !st.Enabled {
		return &protocol.ContinuousRefusal{NeedsMaster: true, Sentence: fmt.Sprintf(
			"scheduler.%s needs the master on — a scheduler that is OFF is already picking nothing, and a pause under it would be an audit row pretending there were two switches (CN18)",
			map[bool]string{true: "pause", false: "resume"}[paused])}, nil
	}
	if _, err := c.s.SetSchedulerState(schedulerStoreActor(actor), st.Enabled, paused, reason); err != nil {
		return nil, err
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// the CN5 template validator — the door's own, verbatim sentences out
// ---------------------------------------------------------------------------

// validateTemplate is CN5's enable-time validation: the blueprint must
// exist, load, VALIDATE and END (a converse finish would rest the loop and
// hold the repository forever), the window must be the trigger's own
// grammar, and minPriority is D2's closed pair. The sentences below are
// what the typed refusal quotes — this function is where the words are
// WRITTEN, nowhere else.
func (c *continuousSocket) validateTemplate(in protocol.ContinuousRootInput) *protocol.ContinuousRefusal {
	refuse := func(sentence string) *protocol.ContinuousRefusal {
		return &protocol.ContinuousRefusal{Sentence: sentence}
	}
	if strings.TrimSpace(in.Blueprint) == "" {
		return refuse("blueprint is required to enable a repository (CN5) — the scheduler turns a ready item into a run only through a named template")
	}
	bp, err := c.d.loadStartBlueprint(in.Blueprint)
	if err != nil {
		return refuse(err.Error()) // the blueprint's own words, never a paraphrase
	}
	if bp.After == blueprint.AfterConverse {
		return refuse(fmt.Sprintf("blueprint %q finishes with after: converse — a loop that rests forever would hold the repository forever (CN5: the template's blueprint must end)", in.Blueprint))
	}
	switch in.MinPriority {
	case "", "known", "assumed":
	default:
		return refuse(fmt.Sprintf("minPriority %q is not one of known, assumed (D2's closed pair)", in.MinPriority))
	}
	if in.Window == nil {
		return nil
	}
	w := continuous.Window{Start: in.Window.Start, End: in.Window.End, Days: in.Window.Days, Zone: in.Window.Zone}
	for _, hhmm := range []string{w.Start, w.End} {
		if _, err := time.Parse("15:04", hhmm); err != nil {
			return refuse(fmt.Sprintf("window time %q is not HH:MM: %v", hhmm, err))
		}
	}
	if w.Days != "" {
		for _, day := range strings.Split(w.Days, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(day))
			if err != nil || n < 1 || n > 7 {
				return refuse(fmt.Sprintf("window days %q is not a comma list of ISO weekdays 1..7 (1 is Monday)", w.Days))
			}
		}
	}
	if w.Zone != "" {
		if _, err := trigger.LoadZone(w.Zone); err != nil {
			return refuse(fmt.Sprintf("window zone %q does not load: %v", w.Zone, err))
		}
	}
	return nil
}

// rowFromInput maps the authored template to the store's row shape.
func rowFromInput(in protocol.ContinuousRootInput) store.SchedulerRootRow {
	row := store.SchedulerRootRow{
		ComponentRoot: in.ComponentRoot,
		Enabled:       in.Enabled,
		Blueprint:     in.Blueprint,
		Tech:          in.Tech,
		WithJSON:      in.WithJSON,
		MinPriority:   in.MinPriority,
	}
	if row.MinPriority == "" {
		row.MinPriority = "known"
	}
	if row.WithJSON == "" {
		row.WithJSON = "{}"
	}
	if in.Window != nil {
		row.WindowStart, row.WindowEnd, row.WindowDays, row.Zone = in.Window.Start, in.Window.End, in.Window.Days, in.Window.Zone
	}
	return row
}

// ---------------------------------------------------------------------------
// the repository door
// ---------------------------------------------------------------------------

// SetRoot validates CN5 FIRST (an invalid template is a typed refusal,
// never a stored row) and refuses to enable a repository under a master
// that is off — the composition that would read as power nobody granted.
// Staging a DISABLED row under an off master is allowed: it grants nothing.
func (c *continuousSocket) SetRoot(ctx context.Context, actor protocol.ContinuousActor, in protocol.ContinuousRootInput) (protocol.ContinuousRepoView, *protocol.ContinuousRefusal, error) {
	if ref := c.validateTemplate(in); ref != nil {
		return protocol.ContinuousRepoView{}, ref, nil
	}
	st, err := c.s.SchedulerState()
	if err != nil {
		return protocol.ContinuousRepoView{}, nil, err
	}
	if in.Enabled && !st.Enabled {
		return protocol.ContinuousRepoView{}, &protocol.ContinuousRefusal{NeedsMaster: true, Sentence: fmt.Sprintf(
			"enabling %q needs the master on first — one authority per act: send scheduler.enable, then this (CN2)", in.ComponentRoot)}, nil
	}
	row, err := c.s.UpsertSchedulerRoot(schedulerStoreActor(actor), rowFromInput(in))
	if err != nil {
		return protocol.ContinuousRepoView{}, nil, err
	}
	return c.repoStatus(ctx, c.d.continuousAdapterRef(), st, row, time.Now().UTC()), nil, nil
}

// DisableRoot refuses a repository that has no row at all: an unconfigured
// repository was never enabled, and answering success to its "disable"
// would be an audit row about a thing that never happened.
func (c *continuousSocket) DisableRoot(ctx context.Context, actor protocol.ContinuousActor, componentRoot string) (*protocol.ContinuousRefusal, error) {
	if _, found, err := c.s.SchedulerRoot(componentRoot); err != nil {
		return nil, err
	} else if !found {
		return &protocol.ContinuousRefusal{Sentence: fmt.Sprintf(
			"no scheduler row for %q — a repository that was never configured was never enabled (CN2)", componentRoot)}, nil
	}
	if _, err := c.s.DisableSchedulerRoot(schedulerStoreActor(actor), componentRoot); err != nil {
		return nil, err
	}
	return nil, nil
}

// EnableTree is CN2's bulk expansion: the template is validated ONCE (a
// repository whose template is invalid stops the WHOLE expansion — none is
// half-enabled), the rows land for the repositories that exist NOW, and a
// child attached later keeps its own authority. A child with no repository
// root of its own (CT3) gets no row and is named in the refusal if the
// tree had none at all.
func (c *continuousSocket) EnableTree(ctx context.Context, actor protocol.ContinuousActor, rootID string, in protocol.ContinuousRootInput) ([]string, *protocol.ContinuousRefusal, error) {
	if ref := c.validateTemplate(in); ref != nil {
		return nil, ref, nil
	}
	st, err := c.s.SchedulerState()
	if err != nil {
		return nil, nil, err
	}
	if !st.Enabled {
		return nil, &protocol.ContinuousRefusal{NeedsMaster: true, Sentence: "scheduler.enableTree needs the master on first — one authority per act (CN2)"}, nil
	}
	comp, err := c.s.ReadComponent(rootID)
	if err != nil {
		return nil, &protocol.ContinuousRefusal{Sentence: fmt.Sprintf(
			"rootId %q names no component: %v", rootID, err)}, nil
	}
	roots := []string{}
	if comp.Root != "" {
		roots = append(roots, comp.Root)
	}
	children, err := c.s.ListComponents(store.ComponentFilter{ParentID: rootID})
	if err != nil {
		return nil, nil, err
	}
	for _, ch := range children {
		if ch.Root != "" {
			roots = append(roots, ch.Root)
		}
	}
	if len(roots) == 0 {
		return nil, &protocol.ContinuousRefusal{Sentence: fmt.Sprintf(
			"the tree under %q has no repository root yet (CT3) — nothing to enable; attach the repositories first", rootID)}, nil
	}
	written := []string{}
	treeIn := in
	treeIn.Enabled = true
	for _, root := range roots {
		treeIn.ComponentRoot = root
		if _, err := c.s.UpsertSchedulerRoot(schedulerStoreActor(actor), rowFromInput(treeIn)); err != nil {
			return written, nil, err
		}
		written = append(written, root)
	}
	return written, nil, nil
}

// ---------------------------------------------------------------------------
// the off switch
// ---------------------------------------------------------------------------

// StopAll rides T6's SchedulerStopAll exactly: pause first through the
// person's own control write, then every scheduler-started loop through
// S55's own door. It needs no master — it is the OFF switch; it exists
// exactly over a store, which is exactly where S55 lives.
func (c *continuousSocket) StopAll(ctx context.Context, actor protocol.ContinuousActor, reason string) (protocol.ContinuousStopAllResult, *protocol.ContinuousRefusal, error) {
	res, err := c.d.SchedulerStopAll(ctx, schedulerStoreActor(actor), reason)
	return protocol.ContinuousStopAllResult{
		Paused:   res.Paused,
		Stopped:  res.Stopped,
		Failures: res.Failures,
	}, nil, err
}

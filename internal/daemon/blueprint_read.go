package daemon

// blueprint_read.go — S48 T7 (BP7, BP8, BP11): the read service behind the
// five T7 doors and the ADDITIVE growth of list and show. Every answer
// comes from rows; reads write nothing, take no actor and cache nothing —
// a save is visible to the very next read because there is no "next" copy
// of anything held here.
//
// The Analyze report show carries is the SAME bytes blueprint.validate
// answers (BP4 — one validator, no paraphrase); lint is ADVISORY-only and
// never touches Report.OK, save or validate. "Shipped" in a diff is what
// the seed would apply — the embedded bytes behind the T3 Source seam —
// so a person can see exactly how their edited system row differs from
// what ships; user rows have no shipped version and get a named refusal,
// not a diff of nothing.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/toolcatalog"
)

type blueprintReads struct {
	store   *store.Store
	shipped func(name string) ([]byte, bool, error) // the shipped record's bytes (nil ⇒ no authority)
	catalog *toolsCatalogService                    // nil ⇒ policy advisories are UNAVAILABLE, stated
	now     func() time.Time
}

func newBlueprintReads(s *store.Store) *blueprintReads {
	r := &blueprintReads{store: s, now: func() time.Time { return time.Now().UTC() }}
	src := blueprint.EmbeddedSource()
	r.shipped = src.Get
	return r
}

// wireBlueprintReads wires the T7 read doors AND the additive list/show
// growth. The catalog assertion happens at ANSWER time (the table field is
// set by the mount below New), so a daemon without the tools catalog
// answers lint with the explicit unavailable state.
func (d *Daemon) wireBlueprintReads() {
	r := newBlueprintReads(d.store)
	r.catalog = toolsCatalogFrom(d.Table)
	d.Table.ListBlueprints = r.List
	d.Table.ShowBlueprint = r.Show
	d.Table.BlueprintReads = r
	// S48 T9: the blueprint_save proposal preview rides the SAME read
	// service — the person reviewing a proposal sees the diff, the report
	// and the lint the read doors answer with, computed read-only and
	// gating nothing (AA4).
	d.Table.BlueprintPreview = r.ProposalPreview
	d.Table.RegisterBlueprintReads()
}

func toolsCatalogFrom(table *protocol.Table) *toolsCatalogService {
	if table == nil {
		return nil
	}
	if tc, wired := table.ToolsCatalog.(*toolsCatalogService); wired && tc != nil {
		return tc
	}
	return nil
}

// --- list and show, grown additively --------------------------------------------------

// List answers {names} exactly as before (the bench's list.names is
// load-bearing) PLUS the summaries; origin/q filter daemon-side (the
// store's WHERE does it — no client filtering, no whole-table walk).
func (r *blueprintReads) List(p protocol.BlueprintListParams) (protocol.BlueprintListResult, error) {
	rows, err := r.store.BlueprintList(context.Background(), p.Origin, p.Q)
	if err != nil {
		return protocol.BlueprintListResult{}, err
	}
	revs, err := r.store.BlueprintMaxRevs(context.Background())
	if err != nil {
		return protocol.BlueprintListResult{}, err
	}
	uses, err := r.store.BlueprintUsageCounts(context.Background())
	if err != nil {
		return protocol.BlueprintListResult{}, err
	}
	out := protocol.BlueprintListResult{Names: []string{}, Blueprints: []protocol.BlueprintSummary{}}
	for _, row := range rows {
		out.Names = append(out.Names, row.Name)
		div, err := r.diverged(row)
		if err != nil {
			return protocol.BlueprintListResult{}, err
		}
		title, description := blueprintLabels(row.Name, row.Content)
		out.Blueprints = append(out.Blueprints, protocol.BlueprintSummary{
			Name:                row.Name,
			Title:               title,
			Origin:              row.Origin,
			Version:             row.Version,
			Revision:            revs[row.Name],
			ContentHash:         row.ContentHash,
			DivergedFromShipped: div,
			UpdatedAt:           row.UpdatedAt,
			UpdatedBy:           row.UpdatedBy,
			UsedByCount:         uses[row.Name],
			Description:         description,
		})
	}
	return out, nil
}

// diverged is the seed ledger's question asked directly of the bytes: does
// the row still hold what the shipped record would apply? Only a SYSTEM
// row can answer; with no shipped authority the field is OMITTED — never
// the silent false the spec forbids.
func (r *blueprintReads) diverged(row store.BlueprintRow) (*bool, error) {
	if row.Origin != "system" {
		f := false
		return &f, nil
	}
	if r.shipped == nil {
		return nil, nil
	}
	raw, ok, err := r.shipped(row.Name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	sum := sha256.Sum256(raw)
	d := hex.EncodeToString(sum[:]) != row.ContentHash
	return &d, nil
}

// Show keeps the old shape (name, content verbatim, path "" — cosmetic
// since T3) and adds the summary, the standard-mode Analyze report
// (byte-equal to what blueprint.validate answers) and the derived refs.
func (r *blueprintReads) Show(p protocol.BlueprintShowParams) (protocol.BlueprintShowResult, error) {
	row, ok, err := r.store.BlueprintGet(context.Background(), p.Name)
	if err != nil {
		return protocol.BlueprintShowResult{}, err
	}
	if !ok {
		// SHOW keeps T3's voice (the doors-agree-on-the-one-sentence pin —
		// T3's TestUnknownBlueprintRefusesWithTodaysSentence); the T7
		// doors' never-existed refusal is ErrBlueprintUnknown.
		return protocol.BlueprintShowResult{}, blueprint.NewErrUnknownBlueprint(p.Name)
	}
	report := blueprint.Analyze([]byte(row.Content), blueprintValidationOptions(""))
	raw, err := json.Marshal(report)
	if err != nil {
		return protocol.BlueprintShowResult{}, fmt.Errorf("blueprint show: encoding the report for %q: %w", p.Name, err)
	}
	refs, err := r.store.BlueprintOwnRefs(context.Background(), p.Name)
	if err != nil {
		return protocol.BlueprintShowResult{}, err
	}
	views := make([]protocol.BlueprintRefView, 0, len(refs))
	for _, ref := range refs {
		views = append(views, protocol.BlueprintRefView{Kind: ref.Kind, Target: ref.Target})
	}
	revs, err := r.store.BlueprintMaxRevs(context.Background())
	if err != nil {
		return protocol.BlueprintShowResult{}, err
	}
	div, err := r.diverged(row)
	if err != nil {
		return protocol.BlueprintShowResult{}, err
	}
	title, description := blueprintLabels(row.Name, row.Content)
	locked, overridable, overrides, err := r.lockedView(row)
	if err != nil {
		return protocol.BlueprintShowResult{}, err
	}
	return protocol.BlueprintShowResult{
		Locked:              locked,
		Overridable:         overridable,
		Overrides:           overrides,
		Name:                row.Name,
		Title:               title,
		Description:         description,
		Path:                "",
		Content:             row.Content,
		Origin:              row.Origin,
		Version:             row.Version,
		Revision:            revs[row.Name],
		ContentHash:         row.ContentHash,
		DivergedFromShipped: div,
		UpdatedAt:           row.UpdatedAt,
		UpdatedBy:           row.UpdatedBy,
		Report:              raw,
		References:          views,
	}, nil
}

// --- usage --------------------------------------------------------------------------------

// Usage answers both directions of BP11: the LIVE things naming the
// blueprint (schedules, event triggers, OPEN queue items — started loops
// are a count, they carry pinned content) and what the blueprint USES
// (agents, actions, skills from the derived blueprint_ref). A blueprint
// nothing uses answers empty arrays, never an error; a name that never
// existed is the named unknown refusal.
func (r *blueprintReads) Usage(ctx context.Context, p protocol.BlueprintUsageParams) (protocol.BlueprintUsageResult, error) {
	_, ok, err := r.store.BlueprintGet(ctx, p.Name)
	if err != nil {
		return protocol.BlueprintUsageResult{}, err
	}
	if !ok {
		return protocol.BlueprintUsageResult{}, r.unknown(p.Name)
	}
	live, err := r.store.BlueprintReferences(ctx, p.Name)
	if err != nil {
		return protocol.BlueprintUsageResult{}, err
	}
	own, err := r.store.BlueprintOwnRefs(ctx, p.Name)
	if err != nil {
		return protocol.BlueprintUsageResult{}, err
	}
	out := protocol.BlueprintUsageResult{
		Schedules:    []protocol.BlueprintUsageRef{},
		Triggers:     []protocol.BlueprintUsageRef{},
		QueueItems:   []protocol.BlueprintUsageRef{},
		Agents:       []string{},
		Actions:      []string{},
		Skills:       []string{},
		LoopsStarted: live.LoopsStarted,
	}
	for _, ref := range live.Schedules {
		out.Schedules = append(out.Schedules, usageRef(ref))
	}
	for _, ref := range live.Triggers {
		out.Triggers = append(out.Triggers, usageRef(ref))
	}
	for _, ref := range live.QueueItems {
		out.QueueItems = append(out.QueueItems, usageRef(ref))
	}
	for _, ref := range own {
		switch ref.Kind {
		case "agent":
			out.Agents = append(out.Agents, ref.Target)
		case "action":
			out.Actions = append(out.Actions, ref.Target)
		case "skill":
			out.Skills = append(out.Skills, ref.Target)
		}
	}
	return out, nil
}

func usageRef(ref store.Reference) protocol.BlueprintUsageRef {
	return protocol.BlueprintUsageRef{Kind: ref.Kind, ID: ref.ID, Name: ref.Name, Status: ref.Status}
}

// --- history and one revision ---------------------------------------------------------------

// History is newest-first and SURVIVES DELETION — the revision table is
// append-only and delete never touches it (T1), so a deleted blueprint's
// history keeps answering. A name that never had a revision is the named
// unknown refusal.
func (r *blueprintReads) History(ctx context.Context, p protocol.BlueprintHistoryParams) (protocol.BlueprintHistoryResult, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.store.RevisionList(ctx, p.Name, limit)
	if err != nil {
		return protocol.BlueprintHistoryResult{}, err
	}
	if len(rows) == 0 {
		return protocol.BlueprintHistoryResult{}, r.unknown(p.Name)
	}
	out := protocol.BlueprintHistoryResult{Revisions: make([]protocol.BlueprintRevisionView, 0, len(rows))}
	for _, rev := range rows {
		out.Revisions = append(out.Revisions, revisionView(rev))
	}
	return out, nil
}

func revisionView(rev store.RevisionRow) protocol.BlueprintRevisionView {
	return protocol.BlueprintRevisionView{
		Rev:         rev.Rev,
		Version:     rev.Version,
		Op:          rev.Op,
		Note:        rev.Note,
		ActorKind:   rev.ActorKind,
		ActorID:     rev.ActorID,
		OccurredAt:  rev.OccurredAt,
		ContentHash: rev.ContentHash,
	}
}

// Revision returns one named revision's stored bytes VERBATIM.
func (r *blueprintReads) Revision(ctx context.Context, p protocol.BlueprintRevisionParams) (protocol.BlueprintRevisionResult, error) {
	rev, ok, err := r.store.RevisionGet(ctx, p.Name, p.Rev)
	if err != nil {
		return protocol.BlueprintRevisionResult{}, err
	}
	if !ok {
		// The name unknown and the rev missing are different sentences.
		if any, err := r.store.RevisionList(ctx, p.Name, 1); err != nil {
			return protocol.BlueprintRevisionResult{}, err
		} else if len(any) == 0 {
			return protocol.BlueprintRevisionResult{}, r.unknown(p.Name)
		}
		return protocol.BlueprintRevisionResult{}, blueprintRefused("rev-unknown", "revision %d of %q does not exist", p.Rev, p.Name)
	}
	return protocol.BlueprintRevisionResult{
		BlueprintRevisionView: revisionView(rev),
		Content:               rev.Content,
	}, nil
}

// --- diff -----------------------------------------------------------------------------------

// Diff is computed HERE, on the stored bytes, by blueprint.Diff (T2) —
// never client-side. Each side is a revision number, "current" or
// "shipped"; shipped is the bytes the seed would apply, so only a SYSTEM
// row has one — a user row asking for it gets a named refusal, not a diff
// of nothing.
func (r *blueprintReads) Diff(ctx context.Context, p protocol.BlueprintDiffParams) (protocol.BlueprintDiffResult, error) {
	_, ok, err := r.store.BlueprintGet(ctx, p.Name)
	if err != nil {
		return protocol.BlueprintDiffResult{}, err
	}
	if !ok {
		return protocol.BlueprintDiffResult{}, r.unknown(p.Name)
	}
	aBytes, aLabel, err := r.diffSide(ctx, p.Name, p.A)
	if err != nil {
		return protocol.BlueprintDiffResult{}, err
	}
	bBytes, bLabel, err := r.diffSide(ctx, p.Name, p.B)
	if err != nil {
		return protocol.BlueprintDiffResult{}, err
	}
	return protocol.BlueprintDiffResult{
		Diff:   blueprint.Diff(aBytes, bBytes, aLabel, bLabel),
		ALabel: aLabel,
		BLabel: bLabel,
		Equal:  string(aBytes) == string(bBytes),
	}, nil
}

func (r *blueprintReads) diffSide(ctx context.Context, name, side string) ([]byte, string, error) {
	switch side {
	case "current":
		row, ok, err := r.store.BlueprintGet(ctx, name)
		if err != nil {
			return nil, "", err
		}
		if !ok {
			return nil, "", r.unknown(name)
		}
		return []byte(row.Content), "current", nil
	case "shipped":
		row, ok, err := r.store.BlueprintGet(ctx, name)
		if err != nil {
			return nil, "", err
		}
		if !ok {
			return nil, "", r.unknown(name)
		}
		if row.Origin != "system" {
			return nil, "", blueprintRefused("system", "a diff against shipped is for SYSTEM blueprints only — %q is a user blueprint and has no shipped version to differ from", name)
		}
		if r.shipped == nil {
			return nil, "", blueprintRefused("system", "this daemon has no shipped-record authority wired — %q cannot be compared against what ships", name)
		}
		raw, ok, err := r.shipped(name)
		if err != nil {
			return nil, "", err
		}
		if !ok {
			return nil, "", blueprintRefused("system", "%q names no shipped record — nothing ships for it to differ from", name)
		}
		return raw, "shipped", nil
	default:
		rev, err := strconv.Atoi(side)
		if err != nil || rev < 1 {
			return nil, "", protocol.ErrBadParams{Err: fmt.Errorf("blueprint.diff side %q is not a revision number, \"current\" or \"shipped\"", side)}
		}
		row, ok, err := r.store.RevisionGet(ctx, name, rev)
		if err != nil {
			return nil, "", err
		}
		if !ok {
			return nil, "", blueprintRefused("rev-unknown", "revision %d of %q does not exist", rev, name)
		}
		return []byte(row.Content), "r" + strconv.Itoa(rev), nil
	}
}

// --- lint -----------------------------------------------------------------------------------

// Lint is ADVISORY (BP8): it never changes Report.OK, save or validate —
// this file never calls Analyze's verdict into any write path. Reasons
// ride verbatim from the policy verdicts (T2's contract) and every
// finding carries the evaluation instant. With no tools catalog wired the
// answer is the EXPLICIT unavailable state — an all-clear invented from
// absence would be the loudest lie the read side could tell.
func (r *blueprintReads) Lint(ctx context.Context, p protocol.BlueprintLintParams) (protocol.BlueprintLintResult, error) {
	content := p.Content
	if content == "" {
		row, ok, err := r.store.BlueprintGet(ctx, p.Name)
		if err != nil {
			return protocol.BlueprintLintResult{}, err
		}
		if !ok {
			return protocol.BlueprintLintResult{}, r.unknown(p.Name)
		}
		content = row.Content
	}
	at := r.now()
	if r.catalog == nil {
		return protocol.BlueprintLintResult{
			Advisories:  []protocol.BlueprintAdvisory{},
			EvaluatedAt: at.UTC().Format(time.RFC3339),
			Unavailable: "the tools catalog is not wired — policy advisories are unavailable",
		}, nil
	}
	bp, err := blueprint.Load([]byte(content))
	if err != nil {
		// Lint is advisory: content that cannot even load is REPORTED, not
		// judged — the verdict on validity belongs to Analyze (validate).
		return protocol.BlueprintLintResult{}, blueprintRefused("diagnostics", "lint cannot read the blueprint: %v", err)
	}
	world, err := r.catalog.world(ctx)
	if err != nil {
		return protocol.BlueprintLintResult{}, err
	}
	advisories := blueprint.Lint(&bp, lintToolState{world: world}, at)
	out := protocol.BlueprintLintResult{
		Advisories:  make([]protocol.BlueprintAdvisory, 0, len(advisories)),
		EvaluatedAt: at.UTC().Format(time.RFC3339),
	}
	for _, a := range advisories {
		out.Advisories = append(out.Advisories, protocol.BlueprintAdvisory{
			Lane:        a.Lane,
			Step:        a.Step,
			Agent:       a.Agent,
			Tool:        a.Tool,
			State:       a.State,
			Reason:      a.Reason,
			EvaluatedAt: a.EvaluatedAt,
			Kind:        a.Kind,
		})
	}
	return out, nil
}

// lintToolState is blueprint's ToolState over the mounted catalog's ONE
// resolver (toolcatalog.Effective over the world's Inputs — the same grant
// chain every other catalog door answers from, policy read fresh at the
// world's instant).
type lintToolState struct {
	world *catalogWorld
}

func (ts lintToolState) State(agent, tool string) (string, string, bool) {
	if _, _, known := ts.world.agentGrants(agent); !known {
		// Nothing is claimed about an agent the table never held — the
		// catalog's own rule, kept here so lint says NOTHING rather than
		// borrowing a default-deny sentence for a name nobody stored.
		return "", "", false
	}
	e := toolcatalog.Effective(tool, agent, ts.world.inputs)
	return string(e.State), e.Reason, true
}

// --- the unknown voice ----------------------------------------------------------------------

// unknown is the READ refusal for a name the store never had: the same
// nearest-names voice the write doors use for from-unknown. A deleted
// blueprint is NOT unknown here — its revisions still answer (History
// finds them before this voice is reached).
func (r *blueprintReads) unknown(name string) error {
	names, err := r.store.BlueprintNames(context.Background())
	if err != nil {
		return err
	}
	return protocol.ErrBlueprintUnknown.WithWhat("nearest names: %s", nearestOrNone(name, names))
}

func nearestOrNone(want string, have []string) string {
	if len(have) == 0 {
		return "none (the table is empty)"
	}
	return nearestToolNames(want, have)
}

// --- the blueprint_save proposal preview (S48 T9) ----------------------------

// ProposalPreview answers what a person sees before committing a
// blueprint_save proposal: the unified diff of the proposed bytes against
// the row (empty and Equal when they agree — a commit would only flip the
// row), the report the save door would render (Analyze's own JSON, verbatim
// — the preview paraphrases nothing), and the ADVISORY policy lint with
// dated reasons (or the explicit unavailable sentence). It reads the
// payload and the row and writes nothing — AA4's rule, the preview never
// gates a commit. A malformed payload answers a refusal sentence, not an
// error: the row still reads; the preview is the courtesy.
func (r *blueprintReads) ProposalPreview(ctx context.Context, payload []byte) (*protocol.BlueprintProposalPreview, string, error) {
	var p struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, "blueprint_save payload is not readable JSON with {name, content}: " + err.Error(), nil
	}
	if !blueprint.ValidBlueprintName(p.Name) {
		return nil, fmt.Sprintf("blueprint name %q is outside the grammar (lowercase words joined by single hyphens, ≤ 64)", p.Name), nil
	}
	if p.Content == "" {
		return nil, "blueprint_save payload carries no content to preview", nil
	}
	row, found, err := r.store.BlueprintGet(ctx, p.Name)
	if err != nil {
		return nil, "", err
	}
	current := ""
	if found {
		current = row.Content
	}
	report, err := json.Marshal(blueprint.Analyze([]byte(p.Content), blueprintValidationOptions("")))
	if err != nil {
		return nil, "", fmt.Errorf("blueprint preview: rendering the report: %w", err)
	}
	lint, err := r.Lint(ctx, protocol.BlueprintLintParams{Content: p.Content})
	if err != nil {
		return nil, "", err
	}
	advisories := lint.Advisories
	if advisories == nil {
		advisories = []protocol.BlueprintAdvisory{}
	}
	return &protocol.BlueprintProposalPreview{
		Name:        p.Name,
		IsNew:       !found,
		Diff:        blueprint.Diff([]byte(current), []byte(p.Content), "current", "proposed"),
		Equal:       current == p.Content,
		Report:      report,
		Advisories:  advisories,
		EvaluatedAt: lint.EvaluatedAt,
		Unavailable: lint.Unavailable,
	}, "", nil
}

// BlueprintProposalPreview is the same preview for the CLI's offline direct
// door (the `aeon proposal` get path, which holds its own store handle and
// no catalog): the lint half then answers the explicit unavailable sentence
// — stated, never a silent all-clear.
func BlueprintProposalPreview(ctx context.Context, s *store.Store, payload []byte) (*protocol.BlueprintProposalPreview, string, error) {
	r := newBlueprintReads(s)
	r.catalog = nil
	return r.ProposalPreview(ctx, payload)
}

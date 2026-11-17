package daemon

// intake.go — S61 T7 (GI9..GI13, P4): the intake doors over the ONE
// intake engine. The engine is what the CLI direct road and the tests
// drive; this file is its second door, not a second implementation (P34).
//
// What lives here and nowhere else:
//
//   - The instruments: the daemon's own action services (git is the
//     hardened S59 environment), the template reader over the store, the
//     target facts the scaffold guard compares against (component roots,
//     home, the state dir), and the two S44 settings the publish gate and
//     the target guard read through config.Reader — default-deny for
//     remote create, and NO fallback directory for a missing workspace
//     root (the refusal names the setting instead of inventing a home).
//   - The S56 seam: remote create needs a standing grant answer, and none
//     is wired for intake publishing yet — the seam refuses with "no
//     grant" BEFORE any network act, which is GI12's default deny
//     walking the same path the setting takes.
//   - The loop-close seam: after a completed run the interview loop (when
//     T9 has attached one) is stopped through the loop control service,
//     under the intake creator's own actor — the person who opened the
//     session closes it. The engine already treats this seam's failure as
//     a warning, not an un-commit.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/charter"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/intake"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/scaffold"
	"github.com/cjlapao/aeon/internal/store"
)

const (
	allowRemoteCreateSetting = "intake.allowRemoteCreate"
	workspaceRootSetting     = "intake.workspaceRoot"
)

type intakeDoors struct {
	d        *Daemon
	store    *store.Store
	settings settingsReader // S44 read-half only (CS11)
	// PublishGrant is the S56 seam for remote create: it answers nil when
	// a standing grant covers the act and an error sentence when it does
	// not. As shipped it is nil here — the plane refuses below the seam
	// with "no grant" — because no grant subject answers for intake
	// publishing yet, and pretending a seam exists is exactly how a
	// default-deny door leaks.
	PublishGrant func(ctx context.Context) error
}

// newIntakeDoors: storeless mount ⇒ the plane is ABSENT (V13).
func newIntakeDoors(d *Daemon) *intakeDoors {
	if d == nil || d.store == nil {
		return nil
	}
	var rd settingsReader
	if d.settings != nil {
		rd = d.settings
	}
	return &intakeDoors{d: d, store: d.store, settings: rd}
}

// engine stands the full instruments up for one run aimed at one target.
// The action services are the daemon's own (forge identity is the TARGET
// root's origin — a tree not yet scaffolded has no origin, so outward
// forge actions are honestly absent until a publish creates the remote).
func (s *intakeDoors) engine(intakeID, target string) *intake.Engine {
	svc := s.d.actionServices(target)
	s.scaffoldInto(svc)
	return &intake.Engine{
		Store:   s.store,
		Charter: &charter.Service{Store: s.store},
		Actions: svc,
		Git:     git.NewClient(daemonGitEnv()),
		CloseLoop: func(ctx context.Context) error {
			return s.closeLoop(ctx, intakeID)
		},
	}
}

// engineLight is the read half: Plan and Status touch no outward
// instrument, and standing the full services up per row would run git
// probes against paths the reads have no business reading.
func (s *intakeDoors) engineLight(intakeID string) *intake.Engine {
	svc := &action.Services{}
	s.scaffoldInto(svc)
	return &intake.Engine{
		Store:   s.store,
		Charter: &charter.Service{Store: s.store},
		Actions: svc,
		Git:     git.NewClient(daemonGitEnv()),
		CloseLoop: func(ctx context.Context) error {
			return s.closeLoop(ctx, intakeID)
		},
	}
}

func (s *intakeDoors) scaffoldInto(svc *action.Services) {
	svc.Scaffold = &action.ScaffoldActions{
		Templates: s.templateSeam,
		TargetFacts: func(context.Context) (scaffold.TargetFacts, error) {
			return s.targetFacts()
		},
	}
	svc.CreateRepo = &action.CreateRepoActions{
		SettingEnabled: s.allowRemoteCreate,
		Grant:          s.publishGrant,
	}
}

// templateSeam answers one slug from the store: the current files, the
// version they carry, and whether the slug exists at all. The action
// pins the plan's slug@version against the answer — a template that moved
// under the plan refuses rather than silently rendering the new shape.
func (s *intakeDoors) templateSeam(_ context.Context, slug string) (scaffold.Template, int, bool, error) {
	row, err := s.store.GetTemplate(slug)
	if err != nil {
		// "does not exist" is the honest found=false; a real read failure
		// is its own error and never masquerades as absence.
		if strings.Contains(err.Error(), "does not exist") {
			return scaffold.Template{}, 0, false, nil
		}
		return scaffold.Template{}, 0, false, err
	}
	tmpl := scaffold.Template{Slug: row.Slug}
	for _, f := range row.Files {
		tmpl.Files = append(tmpl.Files, scaffold.TemplateFile{Path: f.Path, Mode: os.FileMode(f.Mode), Content: f.Content})
	}
	return tmpl, row.Version, true, nil
}

// targetFacts are the closed facts CheckTarget guards against: every
// component root (archived included — an archived root's directory is
// still somebody's project), the person's home, and the daemon's state
// dir. A plan whose target equals any of these refuses before a byte.
func (s *intakeDoors) targetFacts() (scaffold.TargetFacts, error) {
	comps, err := s.store.ListComponents(store.ComponentFilter{IncludeArchived: true})
	if err != nil {
		return scaffold.TargetFacts{}, err
	}
	var roots []string
	for _, c := range comps {
		if c.Root != "" {
			roots = append(roots, c.Root)
		}
	}
	home, _ := os.UserHomeDir()
	return scaffold.TargetFacts{ComponentRoots: roots, Home: home, StateDir: filepath.Dir(s.store.Path())}, nil
}

// allowRemoteCreate reads the S44 gate through the Reader's Bool half
// (the key is declared TypeBool — a String read of it is a programmer
// error, not a coercion). An unreadable read is the declared default
// (false): a gate that fails open is not a gate, and the action's own
// sentence says the setting could not be relied on.
func (s *intakeDoors) allowRemoteCreate(ctx context.Context) (bool, error) {
	if s.settings == nil {
		return false, nil
	}
	on, err := s.settings.Bool(ctx, allowRemoteCreateSetting)
	if err != nil {
		return false, nil
	}
	return on, nil
}

// publishGrant is the S56 seam. Unwired (as shipped): the refusal names
// itself, BEFORE the action reaches any network.
func (s *intakeDoors) publishGrant(ctx context.Context) error {
	if s.PublishGrant == nil {
		return fmt.Errorf("intake publish refuses: no grant answers for remote create — the S56 seam for intake publishing is not wired, and a standing grant is the only thing that can cover this act")
	}
	return s.PublishGrant(ctx)
}

// closeLoop stops the intake's interview loop through the loop control
// service, under the intake creator's own actor (the person who opened
// the session closes it). No loop attached (T9 has not started one) is
// success with nothing to do; the engine treats any error here as a
// warning — a committed intake is not un-committed by a loop that stayed
// open.
func (s *intakeDoors) closeLoop(ctx context.Context, intakeID string) error {
	row, err := s.store.GetIntake(intakeID)
	if err != nil {
		return err
	}
	if row.LoopSlug == "" {
		return nil
	}
	// The control door speaks loop IDS; the row carries a slug (unique
	// only with its root, the store's rule) — resolve first, T9's own
	// lesson from intake_start_test (2026-11-05).
	id, err := s.intakeLoopID(row)
	if err != nil {
		return err
	}
	svc := s.d.newLoopControlService()
	_, ref, err := svc.Stop(ctx, protocol.Actor{Kind: "user", ID: row.CreatedByID}, id, "intake committed (S61)")
	if err != nil {
		return err
	}
	if ref != nil {
		return fmt.Errorf("loop %s was not closed: %s (kind %s)", row.LoopSlug, ref.Detail, ref.Kind)
	}
	return nil
}

// intakeLoopID resolves the interview row's slug to the loop id the
// control doors speak, preferring the loop that lives in the intake's own
// scratch root (a slug is unique only with its root).
func (s *intakeDoors) intakeLoopID(row store.Intake) (string, error) {
	loops, err := s.store.LoopsBySlug(row.LoopSlug)
	if err != nil {
		return "", err
	}
	if len(loops) == 0 {
		return "", fmt.Errorf("loop %s was not closed: no loop row carries that slug", row.LoopSlug)
	}
	pick := loops[len(loops)-1]
	for _, l := range loops {
		if l.ComponentRoot == row.ScratchPath {
			pick = l
			break
		}
	}
	return pick.ID, nil
}

// resolveTarget fills an absent targetPath from the declared workspace
// root and the plan's own naming — and refuses, naming the setting, when
// there is nothing to resolve against (never a fallback directory
// invented around a person's project tree).
func (s *intakeDoors) resolveTarget(ctx context.Context, target, projectName string) (string, error) {
	if target != "" {
		return target, nil
	}
	root := ""
	if s.settings != nil {
		if raw, err := s.settings.String(ctx, workspaceRootSetting); err == nil {
			root = raw
		}
	}
	if root == "" {
		return "", protocol.ErrIntakeRefused.WithWhat(
			"no targetPath was given and the %s setting is unset — the intake plane will not invent a directory to write a project into; set the workspace root or name a targetPath", workspaceRootSetting)
	}
	if projectName == "" {
		return root, nil
	}
	return filepath.Join(root, projectName), nil
}

// loadPlan reads the proposal's payload as the ONE plan struct the desk
// validates — cross-checked against the intake it is asked to preview
// (the resume law's crossed-plan refusal, reused at the read door).
func (s *intakeDoors) loadPlan(intakeID string, pid int64) (desk.IntakePlanProposal, error) {
	prop, found, err := s.store.GetProposal(pid)
	if err != nil {
		return desk.IntakePlanProposal{}, protocol.ErrIntakeRefused.WithWhat("%v", err)
	}
	if !found {
		return desk.IntakePlanProposal{}, protocol.ErrIntakeRefused.WithWhat(
			"proposal %d does not exist — desk.list answers the intake_plan ids", pid)
	}
	if prop.Kind != string(desk.KindIntakePlan) {
		return desk.IntakePlanProposal{}, protocol.ErrIntakeRefused.WithWhat(
			"proposal %d is kind %q, not intake_plan — intake doors answer intake plans", pid, prop.Kind)
	}
	var plan desk.IntakePlanProposal
	if err := json.Unmarshal([]byte(prop.Payload), &plan); err != nil {
		return desk.IntakePlanProposal{}, protocol.ErrIntakeRefused.WithWhat("proposal %d payload does not parse: %v", pid, err)
	}
	if plan.IntakeID != intakeID {
		return desk.IntakePlanProposal{}, protocol.ErrIntakeRefused.WithWhat(
			"proposal %d is the plan for intake %s, not %s — the resume law refuses a crossed plan", pid, plan.IntakeID, intakeID)
	}
	return plan, nil
}

// planNameOf is the plan's own naming: its projectName when it carries
// one, the root (or standalone, or only) component's slug otherwise —
// the slug is already kebab-case by the plan validator's law.
func planNameOf(plan desk.IntakePlanProposal) string {
	if plan.ProjectName != "" {
		return plan.ProjectName
	}
	for _, c := range plan.Components {
		if c.Role == "root" || c.Role == "standalone" || len(plan.Components) == 1 {
			return c.Slug
		}
	}
	if len(plan.Components) > 0 {
		return plan.Components[0].Slug
	}
	return ""
}

// --- protocol.IntakeService ----------------------------------------------------

// Start is intake.start's daemon door — it moved to intake_start.go with
// S61 T9 (2026-11-05), where the interview loop, the private scratch and
// the GI16 route gate live. What stands here is the plane's rest.

func (s *intakeDoors) List(ctx context.Context) (protocol.IntakeListResult, error) {
	rows, err := s.store.ListIntakes()
	if err != nil {
		return protocol.IntakeListResult{}, protocol.ErrIntakeRefused.WithWhat("%v", err)
	}
	eng := s.engineLight("")
	out := protocol.IntakeListResult{Intakes: make([]protocol.IntakeStatusRow, 0, len(rows))}
	for _, row := range rows {
		st, err := eng.Status(ctx, row.ID)
		if err != nil {
			return protocol.IntakeListResult{}, protocol.ErrIntakeRefused.WithWhat("%v", err)
		}
		out.Intakes = append(out.Intakes, protocol.IntakeStatusRow{
			IntakeView: protocol.IntakeView{
				ID: row.ID, Name: row.Name, ScratchPath: row.ScratchPath,
				LoopSlug: row.LoopSlug, RootID: row.RootID, CreatedAt: row.CreatedAt,
			},
			State:      st.State,
			ProposalID: itoa64(st.ProposalID),
		})
	}
	return out, nil
}

func (s *intakeDoors) Status(ctx context.Context, p protocol.IntakeStatusParams) (protocol.IntakeStatusView, error) {
	st, err := s.engineLight(p.IntakeID).Status(ctx, p.IntakeID)
	if err != nil {
		return protocol.IntakeStatusView{}, protocol.ErrIntakeRefused.WithWhat("%v", err)
	}
	return statusView(st), nil
}

func statusView(st intake.Status) protocol.IntakeStatusView {
	v := protocol.IntakeStatusView{
		IntakeID: st.IntakeID, State: st.State, ProposalID: itoa64(st.ProposalID),
		ProposalStatus: st.ProposalStat, RootID: st.RootID, TargetPath: st.TargetPath,
		FailedStep: st.FailedStep,
	}
	for _, step := range st.Steps {
		v.Steps = append(v.Steps, protocol.IntakeStepView{
			Step: step.Step, Outcome: step.Outcome, Detail: step.Detail, At: step.At,
		})
	}
	return v
}

// enginePlan resolves the target (absent means the declared workspace
// root plus the plan's own naming — never an invented directory) and runs
// the engine's Plan once. The digest it hands out covers exactly the
// resolved choice, and Commit resolves the same way, so plan and commit
// cannot disagree by construction (GI12).
func (s *intakeDoors) enginePlan(ctx context.Context, intakeID, proposalID, target, publish, owner string, confirmPublic bool) (protocol.IntakePreview, error) {
	pid, err := strconv.ParseInt(proposalID, 10, 64)
	if err != nil || pid <= 0 {
		return protocol.IntakePreview{}, protocol.ErrIntakeRefused.WithWhat(
			"proposalId %q is not a proposal number — desk.list answers the intake_plan proposal's id", proposalID)
	}
	in := intake.CommitInput{
		IntakeID: intakeID, ProposalID: pid, TargetPath: target,
		Publish: publish, Owner: owner, ConfirmPublic: confirmPublic,
	}
	if in.TargetPath == "" {
		plan, err := s.loadPlan(intakeID, pid)
		if err != nil {
			return protocol.IntakePreview{}, err
		}
		resolved, err := s.resolveTarget(ctx, "", planNameOf(plan))
		if err != nil {
			return protocol.IntakePreview{}, err
		}
		in.TargetPath = resolved
	}
	pre, plan, err := s.engineLight(intakeID).Plan(ctx, in)
	if err != nil {
		return protocol.IntakePreview{}, protocol.ErrIntakeRefused.WithWhat("%v", err)
	}
	return previewView(pre, plan), nil
}

func (s *intakeDoors) Plan(ctx context.Context, p protocol.IntakePlanParams) (protocol.IntakePreview, error) {
	return s.enginePlan(ctx, p.IntakeID, p.ProposalID, p.TargetPath, p.Publish, p.Owner, p.ConfirmPublic)
}

func (s *intakeDoors) Commit(ctx context.Context, actor protocol.Actor, p protocol.IntakeCommitParams) (protocol.IntakeCommitResult, error) {
	pid, err := strconv.ParseInt(p.ProposalID, 10, 64)
	if err != nil || pid <= 0 {
		return protocol.IntakeCommitResult{}, protocol.ErrIntakeRefused.WithWhat(
			"proposalId %q is not a proposal number — intake.plan answers with the id it previews", p.ProposalID)
	}
	target := p.TargetPath
	if target == "" {
		plan, lerr := s.loadPlan(p.IntakeID, pid)
		if lerr != nil {
			return protocol.IntakeCommitResult{}, lerr
		}
		target, lerr = s.resolveTarget(ctx, "", planNameOf(plan))
		if lerr != nil {
			return protocol.IntakeCommitResult{}, lerr
		}
	}
	in := intake.CommitInput{
		IntakeID: p.IntakeID, ProposalID: pid, Digest: p.Digest, TargetPath: target,
		Publish: p.Publish, Owner: p.Owner, ConfirmPublic: p.ConfirmPublic,
	}
	res, err := s.engine(p.IntakeID, target).Commit(ctx, in, store.Actor{Kind: actor.Kind, ID: actor.ID})
	if err != nil {
		return protocol.IntakeCommitResult{}, protocol.ErrIntakeRefused.WithWhat("%v", err)
	}
	out := protocol.IntakeCommitResult{
		IntakeID: res.IntakeID, ProposalID: itoa64(pid), RootID: res.RootID,
		TargetPath: target, Resumed: resumed(res.Steps), Warning: res.Warning,
		Goals: res.Records.Goals, Epics: res.Records.Epics, Items: res.Records.Items,
	}
	for _, c := range res.Components {
		out.Components = append(out.Components, protocol.IntakePreviewComponent{
			Slug: c.Slug, RepoUrl: c.RepoURL, CommitSha: c.CommitSHA,
		})
	}
	return out, nil
}

// resumed is the honest read of a second run: a step row appearing twice
// means this run carried over an earlier attempt's ledger — the ledger's
// own fact, not a flag.
func resumed(steps []store.IntakeStep) bool {
	seen := map[string]int{}
	for _, st := range steps {
		seen[st.Step]++
	}
	for _, n := range seen {
		if n > 1 {
			return true
		}
	}
	return false
}

func (s *intakeDoors) Vocabulary(ctx context.Context) (protocol.IntakeVocabularyResult, error) {
	// T11 finalised the descriptor: every closed set from its enforcing
	// package (never a second typing), the remote-create gate as the
	// THREE states it can be in (each with its own sentence), and
	// routeAvailable — the wizard's front door reads the same fact
	// intake.start's gate enforces, so the descriptor cannot open a
	// wizard that walks into a refusal (a lying descriptor is T11's named
	// fear).
	out := protocol.IntakeVocabularyResult{Roles: desk.IntakeRoles}
	for _, mode := range intake.PublishModes {
		out.Publish = append(out.Publish, protocol.IntakePublishChoice{
			Value: mode, Label: intake.PublishLabel(mode), Statement: intake.PublishStatement(mode),
		})
	}
	caps := desk.IntakePlanCapsOf()
	out.Caps = protocol.IntakePlanCapsWire{
		PayloadBytes: caps.PayloadBytes, Components: caps.Components, Epics: caps.Epics,
		Goals: caps.Goals, Items: caps.Items, CriteriaPerGoal: caps.CriteriaPerGoal,
	}
	rows, err := s.store.ListTemplates()
	if err != nil {
		return protocol.IntakeVocabularyResult{}, protocol.ErrIntakeRefused.WithWhat("%v", err)
	}
	for _, row := range rows {
		out.Templates = append(out.Templates, templateSummary(row))
	}
	out.RemoteCreateEnabled, out.RemoteCreateReason = s.remoteCreateState(ctx)
	root := ""
	state := "unset"
	if s.settings != nil {
		if raw, err := s.settings.String(ctx, workspaceRootSetting); err != nil {
			state = "unreadable-default"
		} else if raw != "" {
			root, state = raw, "setting"
		}
	}
	out.WorkspaceRoot, out.WorkspaceRootState = root, state
	routes, rerr := s.store.ListRoutes()
	out.RouteAvailable = rerr == nil && len(routes) > 0
	return out, nil
}

// remoteCreateState answers the gate as the THREE states the read can
// return, each with the sentence the action itself would speak — the
// descriptor cannot claim a door the action would refuse (T11's lying-
// descriptor test). Setting it on is the only path to true.
func (s *intakeDoors) remoteCreateState(ctx context.Context) (bool, string) {
	if s.settings == nil {
		return false, "the intake.allowRemoteCreate seam is not wired — its default is false (S44), and an unwired seam is that default"
	}
	on, err := s.settings.Bool(ctx, allowRemoteCreateSetting)
	if err != nil {
		return false, "intake.allowRemoteCreate could not be read — an unreadable gate is its default false, not an open one"
	}
	if !on {
		return false, "intake.allowRemoteCreate is off — a person must switch it on (the settings door) before any repository is created"
	}
	return true, ""
}

// previewView maps the engine's preview onto the wire — the census counts
// ride the PLAN (the rows the commit would file are a property of the
// plan, not of the disk), and the component files are the preview's own
// rendered paths.
func previewView(pre intake.Preview, plan desk.IntakePlanProposal) protocol.IntakePreview {
	v := protocol.IntakePreview{
		IntakeID: pre.IntakeID, ProposalID: itoa64(pre.ProposalID), Digest: pre.Digest,
		TargetPath: pre.TargetPath, Publish: pre.Publish.Mode,
		Goals: len(plan.Goals), Epics: len(plan.Epics), Items: len(plan.Items),
		Records: []string{
			fmt.Sprintf("epics: %d", len(plan.Epics)),
			fmt.Sprintf("goals: %d", len(plan.Goals)),
			fmt.Sprintf("items: %d", len(plan.Items)),
		},
	}
	for _, c := range pre.Components {
		fc := protocol.IntakePreviewComponent{
			Slug: c.Slug, Name: c.Name, Blueprint: c.Template, TargetPath: c.Target,
		}
		for _, f := range c.Files {
			fc.Files = append(fc.Files, f.Path)
		}
		fc.Actions = append(fc.Actions, c.GitCommands...)
		v.Components = append(v.Components, fc)
	}
	return v
}

func itoa64(n int64) string {
	if n == 0 {
		return ""
	}
	return strconv.FormatInt(n, 10)
}

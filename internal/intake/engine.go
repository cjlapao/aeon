// engine.go — S61 T6: the engine's face. Plan answers the preview and the
// digest; Commit runs the ledger (steps.go and resume.go carry the steps
// and the loop); Status derives, never stores (GI6).
package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/charter"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/scaffold"
	"github.com/cjlapao/aeon/internal/store"
)

// Engine is the intake commit. Every instrument is the daemon's own: the
// action registry's person doors scaffold and publish, the store registers
// and files, the charter service writes the charter. The engine adds order,
// the digest, and the resume law — and nothing else.
type Engine struct {
	Store   *store.Store
	Charter *charter.Service
	Actions *action.Services
	// Git is the engine's own verify eyes (LocalHEAD, the commit subject,
	// origin and the remote ref). The actions keep their own client; a
	// second read-only client over the same git is honest, a borrowed
	// private one is not.
	Git *git.Client
	// CloseLoop is the daemon's `loop.close` seam for the intake's own
	// interview loop, called after a completed run. It answers its own
	// "if idle" (the daemon knows the loop's state; the engine does not
	// reach loop tables) and its failure is reported as a warning — a
	// committed intake is not un-committed by a loop that stayed open.
	CloseLoop func(ctx context.Context) error
	// Hook is the crash matrix's scalpel, nil in production: it runs
	// before ("before") and after ("after") every step's work. Any error
	// stops the run WITHOUT touching the ledger — that is a crash, not a
	// failure, and the next run finds the step absent and does it.
	Hook func(step, phase string) error
	// RecordsFault is the same scalpel INSIDE the records transaction (the
	// store twin asks it per goal); nil in production. It proves the
	// mid-transaction crash case the between-steps hook cannot reach.
	RecordsFault func(goalsDone int) error
}

// CommitInput is the person's commit answer (GI7): what they saw (Digest),
// where (TargetPath), and whether to publish. It is also what Plan is asked
// with — the digest covers the publish request, so plan and commit must be
// asked the SAME questions; one shape, no drift (deviation recorded in the
// task Notes: the spec's Plan(intakeID, proposalID) cannot answer a digest
// over a publish request nobody stated).
type CommitInput struct {
	IntakeID      string
	ProposalID    int64
	Digest        string
	TargetPath    string
	Publish       string // "none" | "private" | "public"
	Owner         string
	ConfirmPublic bool
}

// PreviewFile is one file the scaffold step will write, with its sha256 —
// the promise TestPreviewEqualsWhatWasWritten keeps (the sha is computed
// from the SAME bytes the scaffold step's render produces: one render
// function, one vars string, both doors).
type PreviewFile struct {
	Path   string `json:"path"`
	Mode   int    `json:"mode"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

type PreviewComponent struct {
	Slug        string        `json:"slug"`
	Name        string        `json:"name"`
	Template    string        `json:"template"` // slug@version — the pin the run carries
	Target      string        `json:"target"`
	Files       []PreviewFile `json:"files"`
	GitCommands []string      `json:"gitCommands"`
}

type PreviewPublish struct {
	Mode          string   `json:"mode"`
	Owner         string   `json:"owner,omitempty"`
	OwnerKind     string   `json:"ownerKind,omitempty"`
	ConfirmPublic bool     `json:"confirmPublic"`
	RepoNames     []string `json:"repoNames,omitempty"`
}

// Preview is what intake.plan shows and what the digest binds: the files,
// the git commands, the publish request (GI7). Steps is the ledger the run
// will write — the person sees the order before confirming it.
type Preview struct {
	IntakeID    string             `json:"intakeId"`
	ProposalID  int64              `json:"proposalId"`
	ProjectName string             `json:"projectName,omitempty"`
	TargetPath  string             `json:"targetPath"`
	Components  []PreviewComponent `json:"components"`
	Publish     PreviewPublish     `json:"publish"`
	Steps       []string           `json:"steps"`
	Digest      string             `json:"digest"`
}

// varsJSON is the CLOSED scaffold variable set (GI9) as the action decodes
// it — the same string feeds the preview's render and the action's Vars
// argument, so a preview byte and a written byte cannot disagree.
func varsJSON(c desk.IntakePlanComponent, branch string) string {
	b, err := json.Marshal(struct {
		Name          string   `json:"name"`
		Slug          string   `json:"slug"`
		Description   string   `json:"description"`
		Stack         []string `json:"stack"`
		DefaultBranch string   `json:"defaultBranch"`
	}{c.Name, c.Slug, c.Description, c.Tags, branch})
	if err != nil { // strings and a []string: unreachable, said so.
		panic("intake: vars marshal: " + err.Error())
	}
	return string(b)
}

// branchOf is GI12's named default: the plan's branch, or main — never a
// guess read from any machine.
func branchOf(c desk.IntakePlanComponent) string {
	if c.DefaultBranch != "" {
		return c.DefaultBranch
	}
	return "main"
}

func targetFor(targetPath, slug string) string { return filepath.Join(targetPath, slug) }

// loadPlan reads the proposal the run commits and re-validates it exactly
// as the create door did (Behaviour 1 of T5's spec, still true here): the
// desk's shape law and charter's own body law, both verbatim.
func (e *Engine) loadPlan(ctx context.Context, in CommitInput) (desk.IntakePlanProposal, store.Proposal, error) {
	p, found, err := e.Store.GetProposal(in.ProposalID)
	if err != nil {
		return desk.IntakePlanProposal{}, store.Proposal{}, err
	}
	if !found {
		return desk.IntakePlanProposal{}, store.Proposal{}, fmt.Errorf("intake: proposal %d does not exist", in.ProposalID)
	}
	if p.Kind != string(desk.KindIntakePlan) {
		return desk.IntakePlanProposal{}, store.Proposal{}, fmt.Errorf("intake: proposal %d is %s — intake.commit commits an intake_plan, not a %s", in.ProposalID, p.Kind, p.Kind)
	}
	if p.Status != "proposed" {
		return desk.IntakePlanProposal{}, store.Proposal{}, fmt.Errorf("intake: proposal %d is %s — only an open plan can be committed", in.ProposalID, p.Status)
	}
	var plan desk.IntakePlanProposal
	dec := json.NewDecoder(strings.NewReader(p.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&plan); err != nil {
		return desk.IntakePlanProposal{}, store.Proposal{}, fmt.Errorf("intake: proposal %d does not hold a plan: %w", in.ProposalID, err)
	}
	if plan.IntakeID != in.IntakeID {
		return desk.IntakePlanProposal{}, store.Proposal{}, fmt.Errorf("intake: proposal %d plans intake %s, and the commit names %s — an ask and a session are one chain or neither",
			in.ProposalID, plan.IntakeID, in.IntakeID)
	}
	if err := desk.ValidateIntakePlanDecoded(plan); err != nil {
		return desk.IntakePlanProposal{}, store.Proposal{}, err
	}
	body, err := charter.Parse(plan.Charter)
	if err != nil {
		return desk.IntakePlanProposal{}, store.Proposal{}, err
	}
	if ref := charter.Validate(body, charter.Vocabulary(nil)); ref != nil {
		return desk.IntakePlanProposal{}, store.Proposal{}, fmt.Errorf("%s", ref.Sentence)
	}
	return plan, p, nil
}

// Plan answers the preview and the digest. It reads — templates, the store,
// nothing else — and writes nothing, including to the ledger: previewing is
// free of consequence (GI6), and the digest is a pure function of the plan,
// the current templates, the target path and the publish answer, so the
// SAME call on resume answers the SAME digest over unchanged parts (no disk
// facts ride it, or a run's own first scaffold would falsify its resume).
func (e *Engine) Plan(ctx context.Context, in CommitInput) (Preview, desk.IntakePlanProposal, error) {
	if e.Store == nil || e.Actions == nil {
		return Preview{}, desk.IntakePlanProposal{}, fmt.Errorf("intake refuses: the engine is not wired")
	}
	if _, err := e.Store.GetIntake(in.IntakeID); err != nil {
		return Preview{}, desk.IntakePlanProposal{}, err
	}
	plan, prop, err := e.loadPlan(ctx, in)
	if err != nil {
		return Preview{}, desk.IntakePlanProposal{}, err
	}
	pv, err := e.previewFor(ctx, in, plan)
	if err != nil {
		return Preview{}, desk.IntakePlanProposal{}, err
	}
	pv.Digest = e.digestFor(pv, desk.ProposalPayloadDigest(prop.Payload))
	return pv, plan, nil
}

// previewFor renders every component's files from the CURRENT pinned
// template and composes the steps — the whole answer minus the digest.
func (e *Engine) previewFor(ctx context.Context, in CommitInput, plan desk.IntakePlanProposal) (Preview, error) {
	if in.TargetPath == "" || !filepath.IsAbs(in.TargetPath) {
		return Preview{}, fmt.Errorf("intake refuses: the target path is absolute or the run is nothing — the payload carries no path by law (GI7), so intake.commit must name one")
	}
	switch in.Publish {
	case "none", "":
		in.Publish = "none"
	case "private", "public":
		if in.Owner == "" {
			return Preview{}, fmt.Errorf("intake refuses: publishing wants an owner — %q is a request to create a repository under nobody", in.Publish)
		}
		if in.Publish == "public" && !in.ConfirmPublic {
			return Preview{}, fmt.Errorf("intake refuses: publishing PUBLIC needs confirm_public — a repository is outward publication of its content, and private is the default")
		}
	default:
		return Preview{}, fmt.Errorf("intake refuses: publish must be none, private or public — %q is none of the three", in.Publish)
	}
	if e.Actions.Scaffold == nil || e.Actions.Scaffold.Templates == nil {
		return Preview{}, fmt.Errorf("intake refuses: no template source is wired — a plan that cannot render is a plan that cannot run")
	}
	pv := Preview{
		IntakeID: in.IntakeID, ProposalID: in.ProposalID, ProjectName: plan.ProjectName,
		TargetPath: in.TargetPath,
		Publish:    PreviewPublish{Mode: in.Publish, Owner: in.Owner, ConfirmPublic: in.ConfirmPublic},
	}
	if in.Publish != "none" {
		pv.Publish.OwnerKind = "user" // the account that owns the person's repos (T11's GitHub wiring may name an org)
	}
	for _, c := range plan.Components {
		tmpl, version, found, err := e.Actions.Scaffold.Templates(ctx, c.Template)
		if err != nil {
			return Preview{}, fmt.Errorf("intake: reading template %q: %w", c.Template, err)
		}
		if !found {
			return Preview{}, fmt.Errorf("intake refuses: template %q does not exist — nothing runs against a template nobody shipped", c.Template)
		}
		files, err := scaffold.Render(tmpl, scaffold.Vars{
			Name: c.Name, Slug: c.Slug, Description: c.Description,
			Stack: c.Tags, DefaultBranch: branchOf(c),
		})
		if err != nil {
			return Preview{}, fmt.Errorf("intake: rendering template %q: %w", c.Template, err)
		}
		pc := PreviewComponent{
			Slug: c.Slug, Name: c.Name,
			Template: fmt.Sprintf("%s@%d", c.Template, version),
			Target:   targetFor(in.TargetPath, c.Slug),
		}
		for _, f := range files {
			sum := sha256.Sum256(f.Content)
			pc.Files = append(pc.Files, PreviewFile{
				Path: f.Path, Mode: int(f.Mode), Size: len(f.Content), SHA256: hex.EncodeToString(sum[:]),
			})
		}
		pc.GitCommands = gitCommands(in.IntakeID, pc.Target, branchOf(c))
		pv.Components = append(pv.Components, pc)
		pv.Publish.RepoNames = append(pv.Publish.RepoNames, c.Slug)
	}
	pv.Steps = stepNames(plan, in)
	return pv, nil
}

// gitCommands is the run's git story per component as ONE fixed template —
// the digest names commands, so what runs must be exactly what was shown
// (the action composes them from the same three facts: branch, dir,
// message).
func gitCommands(intakeID, dir, branch string) []string {
	return []string{
		"git init -b " + branch + " " + dir,
		"git -C " + dir + " add -A",
		`git -C ` + dir + ` commit -m "Initial commit (Aeon intake ` + intakeID + `)"`,
	}
}

// digestFor is GI7's digest: SHA-256 over the canonical JSON of {payload
// digest, per-component file previews, git commands, publish request}.
// json.Marshal of structs is field-ordered and deterministic; no map rides
// the digest, so the same inputs always say the same hex.
func (e *Engine) digestFor(pv Preview, payloadDigest string) string {
	core := struct {
		Version    string             `json:"v"`
		IntakeID   string             `json:"intakeId"`
		ProposalID int64              `json:"proposalId"`
		Payload    string             `json:"payloadDigest"`
		TargetPath string             `json:"targetPath"`
		Components []PreviewComponent `json:"components"`
		Publish    PreviewPublish     `json:"publish"`
	}{v1, pv.IntakeID, pv.ProposalID, payloadDigest, pv.TargetPath, pv.Components, pv.Publish}
	b, err := json.Marshal(core)
	if err != nil {
		panic("intake: digest marshal: " + err.Error()) // structs of strings and numbers — unreachable
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// v1 is the digest's own version — a layout change here changes every
// digest on purpose (a run planned under the old shape must re-review, not
// silently match).
const v1 = "s61.intake.v1"

// Result is what a completed (or stopped) run answers.
type Result struct {
	IntakeID   string                    `json:"intakeId"`
	Digest     string                    `json:"digest"`
	RootID     string                    `json:"rootId"`
	Components []ComponentOutcome        `json:"components,omitempty"`
	CharterRev int                       `json:"charterRevision,omitempty"`
	Records    store.IntakeRecordsResult `json:"records"`
	Steps      []store.IntakeStep        `json:"steps"`
	FailedStep string                    `json:"failedStep,omitempty"`
	Warning    string                    `json:"warning,omitempty"`
}

// ComponentOutcome carries one component's evidence as the run produced it.
type ComponentOutcome struct {
	Slug      string `json:"slug"`
	ID        string `json:"id"`
	CommitSHA string `json:"commitSha,omitempty"`
	RepoURL   string `json:"repoUrl,omitempty"`
}

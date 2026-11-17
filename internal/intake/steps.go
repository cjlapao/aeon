// steps.go — S61 T6 (GI13): the ordered step list of one intake commit,
// each step with its Do and its Verify. The order IS the law: verify-digest
// first (a changed plan stops before any write), per component scaffold →
// verify-git → publish? → register (a component is real and committed
// before it is named), then the tree, the charter, the records, and the
// link. A step's Verify is the check that its DONE-ness still holds — the
// resume runs Verify on every recorded step before passing it (resume.go),
// so this file is also the crash-matrix's evidence law.
package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/charter"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/store"
)

// run is one Commit's whole state: the inputs, the resolved ids the later
// steps need from the earlier ones, and the Result being filled.
type run struct {
	e     *Engine
	ctx   context.Context
	in    CommitInput
	plan  desk.IntakePlanProposal
	pv    Preview
	actor store.Actor
	body  charter.Body

	componentID map[string]string // slug → component id, from register or its Verify
	projectID   string            // the root the tree lands under (single: the component itself)
	out         Result
}

// step is one ledger row's worth of work. Skip, when set, means this run
// legitimately does not do the step (publish none, a tree of one): the
// ledger says `skipped` with its reason, so a reader never mistakes a
// silence for a crash.
type step struct {
	Name   string
	Skip   string
	Do     func() (string, error)
	Verify func() (string, error)
}

// stepNames is the ledger's planned shape — pure and stated to the person
// in the preview, so `intake.plan` shows the order the run will keep.
func stepNames(plan desk.IntakePlanProposal, in CommitInput) []string {
	names := []string{"verify-digest"}
	for _, c := range plan.Components {
		names = append(names, "scaffold:"+c.Slug, "verify-git:"+c.Slug, "publish:"+c.Slug, "register:"+c.Slug)
	}
	names = append(names, "tree", "charter", "records", "link")
	return names
}

// buildSteps composes the run's steps. Every name in stepNames has an
// entry — the order of the two lists is one law stated once (a test walks
// them together).
func (r *run) buildSteps() []step {
	steps := []step{{
		Name:   "verify-digest",
		Do:     r.doVerifyDigest,
		Verify: func() (string, error) { return "digest confirmed", nil }, // the Do is the check; a recorded done re-checks on the fresh plan anyway
	}}
	for _, c := range r.plan.Components {
		c := c
		steps = append(steps,
			step{Name: "scaffold:" + c.Slug, Do: func() (string, error) { return r.doScaffold(c) }, Verify: func() (string, error) { return r.verifyScaffold(c) }},
			step{Name: "verify-git:" + c.Slug, Do: func() (string, error) { return r.verifyGit(c) }, Verify: func() (string, error) { return r.verifyGit(c) }},
		)
		pub := step{Name: "publish:" + c.Slug, Do: func() (string, error) { return r.doPublish(c) }, Verify: func() (string, error) { return r.verifyPublish(c) }}
		if r.pv.Publish.Mode == "none" {
			pub.Skip = "publish is none — " + c.Slug + " gets no repository"
		}
		steps = append(steps, pub,
			step{Name: "register:" + c.Slug, Do: func() (string, error) { return r.doRegister(c) }, Verify: func() (string, error) { return r.verifyRegister(c) }},
		)
	}
	tree := step{Name: "tree", Do: r.doTree, Verify: r.verifyTree}
	if len(r.plan.Components) == 1 {
		tree.Skip = "a single component is its own root — no tree to build"
	}
	steps = append(steps, tree,
		step{Name: "charter", Do: r.doCharter, Verify: r.verifyCharter},
		step{Name: "records", Do: r.doRecords, Verify: r.verifyRecords},
		step{Name: "link", Do: r.doLink, Verify: r.verifyLink},
	)
	return steps
}

// --- verify-digest ------------------------------------------------------

func (r *run) doVerifyDigest() (string, error) {
	pv, err := r.e.previewFor(r.ctx, r.in, r.plan)
	if err != nil {
		return "", err
	}
	prop, found, err := r.e.Store.GetProposal(r.in.ProposalID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("proposal %d vanished", r.in.ProposalID)
	}
	fresh := r.e.digestFor(pv, desk.ProposalPayloadDigest(prop.Payload))
	if fresh != r.in.Digest {
		return "", fmt.Errorf("the plan changed since you saw it — review again (seen %s, now %s)", r.in.Digest, fresh)
	}
	r.pv = pv
	return "digest confirmed", nil
}

// --- scaffold / verify-git / publish / register, per component ----------

func (r *run) previewComponent(slug string) (PreviewComponent, desk.IntakePlanComponent, error) {
	for _, pc := range r.pv.Components {
		if pc.Slug == slug {
			for _, c := range r.plan.Components {
				if c.Slug == slug {
					return pc, c, nil
				}
			}
		}
	}
	return PreviewComponent{}, desk.IntakePlanComponent{}, fmt.Errorf("component %q is not in the plan", slug)
}

func (r *run) doScaffold(c desk.IntakePlanComponent) (string, error) {
	pc, cc, err := r.previewComponent(c.Slug)
	if err != nil {
		return "", err
	}
	// GI13's honest edge for a kill -9 that lands DURING this step (proved by
	// the T12 walk: a daemon killed mid-scaffold left its own torn half and
	// the resume stalled on CheckTarget's refusal forever): take this run's
	// own half back before the fresh attempt. A stranger's bytes refuse it —
	// see takeBackOwnHalf.
	tookBack, err := r.takeBackOwnHalf(pc)
	if err != nil {
		return "", err
	}
	// A tree's components share their parent; the parent is not an effect
	// (no repository, no row) — scaffold's own ground law asks that a
	// target's parent EXIST, and here is where the plan says it may.
	if err := os.MkdirAll(filepath.Dir(pc.Target), 0o755); err != nil {
		return "", fmt.Errorf("intake: making room for %s: %w", c.Slug, err)
	}
	ev, err := action.DispatchFrom(r.ctx, r.e.Actions, action.Caller{Kind: action.CallerPerson, ID: r.actor.ID},
		"repo.scaffold", action.Args{
			Dir:      pc.Target,
			Template: pc.Template,
			Vars:     varsJSON(cc, branchOf(cc)),
			IntakeID: r.in.IntakeID,
		})
	if err != nil {
		return "", err
	}
	r.setCommitSHA(c.Slug, "", ev["commit_sha"])
	detail := fmt.Sprintf("scaffolded %s at %s commit %s", c.Slug, pc.Target, ev["commit_sha"])
	if tookBack {
		detail += " — this run's own half was taken back first (GI13's edge, proved by the T12 walk)"
	}
	return detail, nil
}

// takeBackOwnHalf is the scaffold step's re-entry law. The action's GI8
// take-back dies with a killed process, so a crash mid-scaffold leaves OUR
// OWN half on disk — and CheckTarget lawfully refuses any non-empty target,
// which would stall the resume forever on a directory this very run made.
// The half is ours, and only then is it removed, when EVERY path under the
// target (git's own .git aside) is a previewed file holding the preview's
// own bytes, and a repository there — if any — is exactly one commit whose
// subject carries this intake id. A stray byte, a differing hash, a second
// commit or an unverifiable head leaves it untouched: GI9's forbidden
// repair stays forbidden, and CheckTarget keeps refusing with its own
// sentence. Removing a provable half and writing the whole template afresh
// arrives at the identical end state the uninterrupted run would have.
func (r *run) takeBackOwnHalf(pc PreviewComponent) (bool, error) {
	st, err := os.Lstat(pc.Target)
	if err != nil || st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
		return false, nil // absent, or a ground CheckTarget itself refuses; nothing to judge
	}
	want := make(map[string]string, len(pc.Files))
	for _, f := range pc.Files {
		want[filepath.FromSlash(f.Path)] = f.SHA256
	}
	ours := true
	_ = filepath.WalkDir(pc.Target, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || d == nil {
			ours = false
			return filepath.SkipAll
		}
		rel, rerr := filepath.Rel(pc.Target, p)
		if rerr != nil {
			ours = false
			return filepath.SkipAll
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) || d.IsDir() {
			return nil
		}
		b, oerr := os.ReadFile(p)
		if oerr != nil {
			ours = false
			return filepath.SkipAll
		}
		sum := sha256.Sum256(b)
		if got, seen := want[rel]; !seen || hex.EncodeToString(sum[:]) != got {
			ours = false
			return filepath.SkipAll
		}
		return nil
	})
	if !ours {
		return false, nil
	}
	if _, gerr := os.Lstat(filepath.Join(pc.Target, ".git")); gerr == nil {
		n, nerr := r.e.Git.Run(r.ctx, pc.Target, "rev-list", "--count", "HEAD")
		subject, serr := r.e.Git.Run(r.ctx, pc.Target, "log", "-1", "--format=%s")
		if nerr != nil || serr != nil || strings.TrimSpace(n) != "1" || !strings.Contains(subject, r.in.IntakeID) {
			return false, nil
		}
	} else if !os.IsNotExist(gerr) {
		return false, nil
	}
	entries, err := os.ReadDir(pc.Target)
	if err != nil || len(entries) == 0 {
		return false, nil // an empty target is nothing to take back — the fresh run just fills it
	}
	if err := os.RemoveAll(pc.Target); err != nil {
		return false, fmt.Errorf("intake: taking back this run's own half at %s: %w", pc.Target, err)
	}
	return true, nil
}

// verifyScaffold is GI13's evidence check for a scaffold row: the dir is
// there, EVERY previewed file's sha matches the bytes on disk, HEAD exists,
// and the initial commit's subject carries this intake id. A mismatch is
// not repaired — it refuses (resume.go), because overwriting what a stray
// hand planted is GI9's forbidden repair.
func (r *run) verifyScaffold(c desk.IntakePlanComponent) (string, error) {
	pc, _, err := r.previewComponent(c.Slug)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(pc.Target); err != nil || !st.IsDir() {
		return "", fmt.Errorf("%s is not a directory", pc.Target)
	}
	for _, f := range pc.Files {
		b, err := os.ReadFile(filepath.Join(pc.Target, filepath.FromSlash(f.Path)))
		if err != nil {
			return "", fmt.Errorf("%s: %w", f.Path, err)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			return "", fmt.Errorf("%s holds different bytes than the preview promised", f.Path)
		}
	}
	head, err := r.e.Git.LocalHEAD(r.ctx, pc.Target)
	if err != nil {
		return "", err
	}
	subject, err := r.e.Git.Run(r.ctx, pc.Target, "log", "-1", "--format=%s")
	if err != nil {
		return "", err
	}
	if !strings.Contains(subject, r.in.IntakeID) {
		return "", fmt.Errorf("the head commit says %q and does not carry intake %s", subject, r.in.IntakeID)
	}
	r.setCommitSHA(c.Slug, head, "")
	return fmt.Sprintf("%s verified at commit %s", c.Slug, head), nil
}

// previewComponentOK is the lookup for callers whose honest answer is a
// plain yes/no (an item naming a component the plan never listed).
func (r *run) previewComponentOK(slug string) (PreviewComponent, desk.IntakePlanComponent, bool) {
	pc, c, err := r.previewComponent(slug)
	return pc, c, err == nil
}

func (r *run) verifyGit(c desk.IntakePlanComponent) (string, error) {
	pc, _, err := r.previewComponent(c.Slug)
	if err != nil {
		return "", err
	}
	head, err := r.e.Git.LocalHEAD(r.ctx, pc.Target)
	if err != nil {
		return "", fmt.Errorf("%s is not a git work tree with a head: %w", pc.Target, err)
	}
	if err := r.e.Git.AssertClean(r.ctx, pc.Target); err != nil {
		return "", err
	}
	r.setCommitSHA(c.Slug, head, "")
	return fmt.Sprintf("head %s clean for %s", head, c.Slug), nil
}

func (r *run) doPublish(c desk.IntakePlanComponent) (string, error) {
	pc, cc, err := r.previewComponent(c.Slug)
	if err != nil {
		return "", err
	}
	ev, err := action.DispatchFrom(r.ctx, r.e.Actions, action.Caller{Kind: action.CallerPerson, ID: r.actor.ID},
		"forge.createRepo", action.Args{
			Dir: pc.Target, Owner: r.pv.Publish.Owner, RepoName: c.Slug,
			OwnerKind: r.pv.Publish.OwnerKind, Publish: r.pv.Publish.Mode,
			ConfirmPublic: r.pv.Publish.ConfirmPublic,
			Description:   shortRepoDescription(cc, r.plan),
		})
	if err != nil {
		return "", err
	}
	r.setRepoURL(c.Slug, ev["repo_url"])
	return fmt.Sprintf("published %s/%s at %s", r.pv.Publish.Owner, c.Slug, ev["pushed_sha"]), nil
}

func (r *run) verifyPublish(c desk.IntakePlanComponent) (string, error) {
	pc, _, err := r.previewComponent(c.Slug)
	if err != nil {
		return "", err
	}
	url, has, err := r.e.Git.RemoteURL(r.ctx, pc.Target, "origin")
	if err != nil {
		return "", err
	}
	if !has {
		return "", fmt.Errorf("%s has no origin — publish recorded done without leaving a remote", pc.Target)
	}
	branch, err := r.e.Git.CurrentBranch(r.ctx, pc.Target)
	if err != nil {
		return "", err
	}
	remote, err := r.e.Git.RemoteRefSHA(r.ctx, pc.Target, branch)
	if err != nil {
		return "", err
	}
	head, err := r.e.Git.LocalHEAD(r.ctx, pc.Target)
	if err != nil {
		return "", err
	}
	if remote != head {
		return "", fmt.Errorf("origin/%s is %s and the local head is %s — publish is only done when the push landed", branch, remote, head)
	}
	r.setRepoURL(c.Slug, url)
	return fmt.Sprintf("origin %s at %s equals the head", url, remote), nil
}

func (r *run) doRegister(c desk.IntakePlanComponent) (string, error) {
	pc, _, err := r.previewComponent(c.Slug)
	if err != nil {
		return "", err
	}
	branch := branchOf(c)
	w, err := r.e.Store.CreateComponent(store.NewComponent{
		Name: c.Name, Slug: c.Slug, Root: pc.Target,
		ShortDescription: c.Description, DefaultBranch: &branch, Tags: c.Tags,
	})
	if err != nil {
		return "", err
	}
	if !w.OK() {
		// GI13's edge again (the T12 walk's crash at register:shop-jobs proved
		// it): a real kill can land BETWEEN the store's effect and the ledger
		// row — the effect is here, its word is not. The adoption test is
		// verifyRegister's own read — the same root registers the SAME planned
		// slug — and nothing looser: a foreign component (or any other slug)
		// on the root keeps verifyRegister's refusal, which keeps the create's
		// refusal standing. A provable own-effect is adopted, not repaired.
		if v, verr := r.verifyRegister(c); verr == nil {
			return v + " — adopted: the create landed before its ledger row (GI13's edge, proved by the T12 walk)", nil
		}
		return "", fmt.Errorf("component.create refused: %s", w.Reason)
	}
	r.componentID[c.Slug] = w.Component.ID
	r.setID(c.Slug, w.Component.ID)
	return fmt.Sprintf("registered %s as %s", c.Slug, w.Component.ID), nil
}

func (r *run) verifyRegister(c desk.IntakePlanComponent) (string, error) {
	pc, _, err := r.previewComponent(c.Slug)
	if err != nil {
		return "", err
	}
	comp, err := r.e.Store.ComponentForRoot(pc.Target)
	if err != nil {
		return "", err
	}
	if comp.Slug != c.Slug {
		return "", fmt.Errorf("workspace %s registers component %q, not the planned %q", pc.Target, comp.Slug, c.Slug)
	}
	r.componentID[c.Slug] = comp.ID
	r.setID(c.Slug, comp.ID)
	return fmt.Sprintf("%s registered as %s", c.Slug, comp.ID), nil
}

// --- tree, charter, records, link ---------------------------------------

func (r *run) doTree() (string, error) {
	first := r.plan.Components[0].Slug
	firstID, ok := r.componentID[first]
	if !ok {
		return "", fmt.Errorf("component %q was never registered — the tree has nothing to build on", first)
	}
	adopted := false
	child, err := r.e.Store.ReadComponent(firstID)
	if err != nil {
		return "", err
	}
	if child.ParentID != "" {
		// The promote landed without its ledger row (GI13's edge, same law as
		// the register adoption above): the project row IS that parent, and a
		// second project is never made beside it.
		r.projectID = child.ParentID
		adopted = true
	} else {
		w, err := r.e.Store.PromoteComponent(firstID, store.PromoteInput{Name: r.plan.ProjectName})
		if err != nil {
			return "", err
		}
		if !w.OK() {
			return "", fmt.Errorf("component.promote refused: %s", w.Reason)
		}
		child, err = r.e.Store.ReadComponent(firstID)
		if err != nil {
			return "", err
		}
		r.projectID = child.ParentID
	}
	for _, c := range r.plan.Components[1:] {
		id, ok := r.componentID[c.Slug]
		if !ok {
			return "", fmt.Errorf("component %q was never registered — the tree has nothing to attach", c.Slug)
		}
		if comp, err := r.e.Store.ReadComponent(id); err == nil && comp.ParentID == r.projectID {
			continue // the attach landed without its row — same edge, no re-attach
		}
		w, err := r.e.Store.AttachComponent(id, r.projectID)
		if err != nil {
			return "", err
		}
		if !w.OK() {
			return "", fmt.Errorf("component.attach %s under %s refused: %s", c.Slug, r.projectID, w.Reason)
		}
	}
	detail := fmt.Sprintf("project %s over %d components", r.projectID, len(r.plan.Components))
	if adopted {
		detail += " — the promote landed before its ledger row and was adopted (GI13's edge)"
	}
	return detail, nil
}

func (r *run) verifyTree() (string, error) {
	if len(r.plan.Components) == 1 {
		return "single component — no tree", nil
	}
	first, err := r.e.Store.ReadComponent(r.componentID[r.plan.Components[0].Slug])
	if err != nil {
		return "", err
	}
	if first.ParentID == "" {
		return "", fmt.Errorf("%s is not under the project — the promote never held", first.Slug)
	}
	for _, c := range r.plan.Components[1:] {
		comp, err := r.e.Store.ReadComponent(r.componentID[c.Slug])
		if err != nil {
			return "", err
		}
		if comp.ParentID != first.ParentID {
			return "", fmt.Errorf("%s hangs under %q, not under the project %s", c.Slug, comp.ParentID, first.ParentID)
		}
	}
	r.projectID = first.ParentID
	return fmt.Sprintf("tree under %s holds", r.projectID), nil
}

// rootID is the tree's top: the promoted project for a tree, the lone
// component for a single plan. Charter, records and the link all speak of
// it, so its resolution lives here once.
func (r *run) rootID() (string, error) {
	if len(r.plan.Components) == 1 {
		id, ok := r.componentID[r.plan.Components[0].Slug]
		if !ok {
			return "", fmt.Errorf("the plan's component was never registered")
		}
		return id, nil
	}
	if r.projectID == "" {
		return "", fmt.Errorf("the tree was never built — the project root does not exist yet")
	}
	return r.projectID, nil
}

func (r *run) doCharter() (string, error) {
	rootID, err := r.rootID()
	if err != nil {
		return "", err
	}
	rev, ref, err := r.e.Charter.Save(r.ctx, r.actor, rootID, r.body, "", "intake", r.in.IntakeID)
	if err != nil {
		return "", err
	}
	if ref != nil {
		return "", fmt.Errorf("charter.save refused: %s", ref.Sentence)
	}
	r.out.CharterRev = rev.Revision
	return fmt.Sprintf("charter revision %d on %s", rev.Revision, rootID), nil
}

func (r *run) verifyCharter() (string, error) {
	rootID, err := r.rootID()
	if err != nil {
		return "", err
	}
	revs, err := r.e.Charter.Revisions(r.ctx, rootID)
	if err != nil {
		return "", err
	}
	for _, rv := range revs {
		if rv.Source == "intake" && rv.SourceRef == r.in.IntakeID {
			r.out.CharterRev = rv.Revision
			return fmt.Sprintf("charter revision %d on %s holds", rv.Revision, rootID), nil
		}
	}
	return "", fmt.Errorf("no charter revision on %s names intake %s", rootID, r.in.IntakeID)
}

func (r *run) doRecords() (string, error) {
	rootID, err := r.rootID()
	if err != nil {
		return "", err
	}
	in := store.IntakeRecordsInput{
		IntakeID: r.in.IntakeID, ComponentID: rootID, Author: r.actor,
		Fault: r.e.RecordsFault,
	}
	for _, e := range r.plan.Epics {
		in.Epics = append(in.Epics, store.IntakeEpicRow{Key: e.Key, Title: e.Title, Objective: e.Objective})
	}
	for _, g := range r.plan.Goals {
		gin := store.IntakeGoalRow{EpicKey: g.EpicKey, Title: g.Title, Type: g.Type, BodyNote: g.BodyNote}
		for _, slug := range g.ComponentSlugs {
			id, ok := r.componentID[slug]
			if !ok {
				return "", fmt.Errorf("goal %q stakes component %q and the plan never registered it", g.Title, slug)
			}
			gin.Components = append(gin.Components, store.GoalComponent{ComponentID: id})
		}
		for _, c := range g.Criteria {
			gin.Criteria = append(gin.Criteria, store.Criterion{Text: c.Text, CheckKind: c.Check, CheckRef: c.Ref})
		}
		in.Goals = append(in.Goals, gin)
	}
	for _, it := range r.plan.Items {
		pc, _, ok := r.previewComponentOK(it.ComponentSlug)
		if !ok {
			return "", fmt.Errorf("item %q names component %q and the plan has no such component", it.Title, it.ComponentSlug)
		}
		// The roadmap's `component_root` is the ROOT PATH of the component
		// the item belongs to — the same value the person's roadmap.add
		// carries — never the component id.
		row := store.IntakeItemRow{ComponentRoot: pc.Target, Title: it.Title, EpicKey: it.EpicKey}
		state, label := priorityOf(it.Priority)
		row.PriorityState, row.PriorityLabel = state, label
		min, max, basis, err := estimateOf(it.Estimate)
		if err != nil {
			return "", err
		}
		row.EstimateMin, row.EstimateMax, row.EstimateBasis = min, max, basis
		in.Items = append(in.Items, row)
	}
	res, err := r.e.Store.ApplyIntakeRecords(in)
	if err != nil {
		return "", err
	}
	r.out.Records = res
	return fmt.Sprintf("filed %d epics, %d goals, %d items under %s", res.Epics, res.Goals, res.Items, rootID), nil
}

// verifyRecords is the duplicate tripwire. The records step is ONE
// transaction (store.ApplyIntakeRecords), so a crash left all or nothing —
// the counts either say exactly the plan or they say the plan was filed
// twice or tampered with, and a redo over either would be the duplicate
// GI13 forbids.
func (r *run) verifyRecords() (string, error) {
	rootID, err := r.rootID()
	if err != nil {
		return "", err
	}
	goals, err := r.e.Store.ListGoals(store.GoalFilter{ComponentID: rootID})
	if err != nil {
		return "", err
	}
	mine := 0
	for _, g := range goals {
		if g.SourceKind == "intake" && g.SourceRef == r.in.IntakeID {
			mine++
			if g.ReviewState != "in_review" {
				return "", fmt.Errorf("goal %s is %s — an intake goal files INTO review and nothing here approves", g.ID, g.ReviewState)
			}
		}
	}
	if mine != len(r.plan.Goals) {
		return "", fmt.Errorf("%d goals name this intake under %s, the plan files %d", mine, rootID, len(r.plan.Goals))
	}
	items := 0
	for _, pc := range r.pv.Components {
		rows, err := r.e.Store.ListRoadmapItems(pc.Target, store.RoadmapItemFilters{})
		if err != nil {
			return "", err
		}
		for _, row := range rows {
			if row.Provenance == "intake" {
				items++
			}
		}
	}
	if items != len(r.plan.Items) {
		return "", fmt.Errorf("%d items carry intake provenance, the plan files %d", items, len(r.plan.Items))
	}
	return fmt.Sprintf("%d goals and %d items as planned", mine, items), nil
}

func (r *run) doLink() (string, error) {
	rootID, err := r.rootID()
	if err != nil {
		return "", err
	}
	ref, err := r.e.Store.SetIntakeRoot(r.in.IntakeID, rootID, r.in.TargetPath)
	if err != nil {
		return "", err
	}
	if ref != nil {
		return "", fmt.Errorf("the link refused: %s", ref.Sentence)
	}
	r.out.RootID = rootID
	return fmt.Sprintf("linked %s to %s at %s", r.in.IntakeID, rootID, r.in.TargetPath), nil
}

func (r *run) verifyLink() (string, error) {
	rootID, err := r.rootID()
	if err != nil {
		return "", err
	}
	in, err := r.e.Store.GetIntake(r.in.IntakeID)
	if err != nil {
		return "", err
	}
	if in.RootID != rootID || in.TargetPath != r.in.TargetPath {
		return "", fmt.Errorf("intake %s links %s at %q, not %s at %q", r.in.IntakeID, in.RootID, in.TargetPath, rootID, r.in.TargetPath)
	}
	r.out.RootID = rootID
	return fmt.Sprintf("%s linked to %s", r.in.IntakeID, rootID), nil
}

// --- the plan's own words, made into records' shapes ---------------------

// priorityOf: the plan said nothing or "unknown" ⇒ unknown with no label
// (the honest state of an unranked item); anything else is the person's own
// label, and a labeled priority is a KNOWN one.
func priorityOf(p string) (string, *string) {
	if p == "" || p == "unknown" {
		return "unknown", nil
	}
	l := p
	return "known", &l
}

// estimateOf is D3's law spoken from the plan's words: a number, a range,
// or nothing — never a bare figure without its basis, never an invented
// range. The words are the plan's; the basis names where they came from.
var estimateRE = regexp.MustCompile(`^(\d+)(?:\s*-\s*(\d+))?$`)

func estimateOf(s string) (*int, *int, *string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil, nil, nil
	}
	m := estimateRE.FindStringSubmatch(s)
	if m == nil {
		return nil, nil, nil, fmt.Errorf("an estimate is a number, a range like 3-5, or nothing — %q is none of the three", s)
	}
	min, err := strconv.Atoi(m[1])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("estimate %q: %w", s, err)
	}
	max := min
	if m[2] != "" {
		if max, err = strconv.Atoi(m[2]); err != nil {
			return nil, nil, nil, fmt.Errorf("estimate %q: %w", s, err)
		}
	}
	if min > max {
		return nil, nil, nil, fmt.Errorf("estimate %q: %d above %d — a range runs upward", s, min, max)
	}
	basis := "intake plan"
	return &min, &max, &basis, nil
}

// shortRepoDescription keeps GI18's law: the short description rides the
// repository, never a plan body.
func shortRepoDescription(c desk.IntakePlanComponent, plan desk.IntakePlanProposal) string {
	if c.Description != "" {
		return c.Description
	}
	if plan.ProjectName != "" {
		return "Aeon project " + plan.ProjectName + ", component " + c.Name
	}
	return "Aeon component " + c.Name
}

// the three small setters keep Result's component rows honest whether the
// step RAN or only its evidence was VERIFIED (a resume fills the same
// answer from the disk and the rows).
func (r *run) componentRow(slug string) *ComponentOutcome {
	for i := range r.out.Components {
		if r.out.Components[i].Slug == slug {
			return &r.out.Components[i]
		}
	}
	r.out.Components = append(r.out.Components, ComponentOutcome{Slug: slug})
	return &r.out.Components[len(r.out.Components)-1]
}

func (r *run) setCommitSHA(slug, head, ev string) {
	row := r.componentRow(slug)
	if head != "" {
		row.CommitSHA = head
	} else if ev != "" {
		row.CommitSHA = ev
	}
}
func (r *run) setRepoURL(slug, url string) { r.componentRow(slug).RepoURL = url }
func (r *run) setID(slug, id string)       { r.componentRow(slug).ID = id }

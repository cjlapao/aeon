package action

// scaffold_action.go is `repo.scaffold` — GI9 end to end, behind the
// person door. The order IS the safety: guard the ground, take the
// template, render it, check the identity, and only then create anything;
// from the first write onward, every failure path takes back exactly what
// this run created (the ledger) and nothing else. A missing identity
// therefore leaves NOTHING behind — it refuses while the ground is still
// bare — because GI12 forbids inventing an author as loudly as GI9 forbids
// touching what exists.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/scaffold"
)

// ScaffoldActions is the daemon's wiring for this action: the template
// source (a store read — the action package imports no store, so it
// arrives as a seam) and the component-root facts CheckTarget guards
// against (GI9's "not inside a registered component"). Either nil is a
// refusal, not a default.
type ScaffoldActions struct {
	// Templates answers one slug: the current files, the version they
	// carry, and whether the slug exists at all. The action pins the
	// plan's `slug@version` against the answer — the template that moved
	// under the plan refuses, it does not silently render the new shape.
	Templates func(ctx context.Context, slug string) (tmpl scaffold.Template, version int, found bool, err error)
	// TargetFacts supplies the closed facts CheckTarget guards against.
	TargetFacts func(ctx context.Context) (scaffold.TargetFacts, error)
}

// branchNameRule is the floor under `git init -b`: a lowercase-or-digit
// start, then letters/digits/dot/underscore/slash/dash. Dots in a row and
// a `.lock` ending are git's own refusals and the git client re-checks
// them; this catches the plausible names before a directory exists.
var branchNameRule = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$`)

// scaffoldVars is the CLOSED variable set (GI9), decoded with unknown
// keys refused: a plan that grew a sixteenth variable is a plan bug, said
// by name, not a template that quietly ignored it.
type scaffoldVars struct {
	Name          string   `json:"name"`
	Slug          string   `json:"slug"`
	Description   string   `json:"description"`
	Stack         []string `json:"stack"`
	DefaultBranch string   `json:"defaultBranch"`
}

// runScaffold is reached ONLY through DispatchFrom with a person Caller
// (the registry's Door marks the entry); everything before the first
// write is a read.
func runScaffold(ctx context.Context, svc *Services, a Args) (map[string]string, error) {
	slug, pinStr, ok := strings.Cut(a.Template, "@")
	if !ok || slug == "" || pinStr == "" {
		return nil, fmt.Errorf("repo.scaffold refuses: the template argument is slug@version — %q names no pinned version, and an unpinned scaffold renders whatever the template table says TODAY", a.Template)
	}
	pin, err := strconv.Atoi(pinStr)
	if err != nil || pin < 1 {
		return nil, fmt.Errorf("repo.scaffold refuses: template version %q is not a positive number", pinStr)
	}
	if svc.Scaffold == nil || svc.Scaffold.Templates == nil {
		return nil, fmt.Errorf("repo.scaffold refuses: no template source wired — an action without its instruments is an action that hopes")
	}
	tmpl, version, found, err := svc.Scaffold.Templates(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("repo.scaffold: reading template %q: %w", slug, err)
	}
	if !found {
		return nil, fmt.Errorf("repo.scaffold refuses: template %q does not exist — nothing is created against a template nobody shipped", slug)
	}
	if version != pin {
		return nil, fmt.Errorf("repo.scaffold refuses: template %q is at version %d and the plan pinned %d — the plan and the table disagree, and neither is silently rewritten", slug, version, pin)
	}

	var v scaffoldVars
	if a.Vars != "" {
		dec := json.NewDecoder(strings.NewReader(a.Vars))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("repo.scaffold refuses: vars are the closed set (name, slug, description, stack, defaultBranch) — %v", err)
		}
	}

	// The ground BEFORE any write (GI9): the refusal sentences are the
	// scaffold package's, verbatim.
	if svc.Scaffold.TargetFacts == nil {
		return nil, fmt.Errorf("repo.scaffold refuses: no component-root facts wired — the guard cannot see what it must refuse to touch")
	}
	facts, err := svc.Scaffold.TargetFacts(ctx)
	if err != nil {
		return nil, fmt.Errorf("repo.scaffold: reading component roots: %w", err)
	}
	if err := scaffold.CheckTarget(a.Dir, facts); err != nil {
		return nil, err
	}

	branch := v.DefaultBranch
	if branch == "" {
		branch = "main" // GI12: the plan's branch, or main. Not a guess — the named default.
	}
	if !branchNameRule.MatchString(branch) || strings.Contains(branch, "..") || strings.HasSuffix(branch, ".lock") {
		return nil, fmt.Errorf("repo.scaffold refuses: default branch %q is not a safe branch name", branch)
	}

	if svc.Git == nil {
		return nil, fmt.Errorf("repo.scaffold: no git client wired")
	}
	gitc := svc.Git.client()

	// GI12 before GI9's first write: an unauthored commit is refused while
	// the ground is still bare, so "refuse with a sentence" also means
	// "nothing was created".
	who, _, idOK, err := gitc.IdentityResolved(ctx, a.Dir)
	if err != nil {
		return nil, fmt.Errorf("repo.scaffold: resolving the git identity: %w", err)
	}
	if !idOK {
		return nil, fmt.Errorf("repo.scaffold refuses: no git identity resolves for %s — an initial commit is authored, not invented (GI12); set one with `git config --global user.name \"Name\"` and `git config --global user.email \"you@example.com\"` and re-run. Nothing was created", a.Dir)
	}
	_ = who // the name was resolved for its presence; the commit is what reads it.

	files, err := scaffold.Render(tmpl, scaffold.Vars{
		Name: v.Name, Slug: v.Slug, Description: v.Description,
		Stack: v.Stack, DefaultBranch: branch,
	})
	if err != nil {
		return nil, fmt.Errorf("repo.scaffold: rendering template %q: %w", slug, err)
	}

	ledger := &scaffold.Ledger{}
	if err := scaffold.Write(a.Dir, files, ledger); err != nil {
		return nil, takeBack(a.Dir, ledger, err) // Write's own half is in the ledger; it comes back too
	}
	if err := gitc.Init(ctx, a.Dir, branch); err != nil {
		return nil, takeBack(a.Dir, ledger, fmt.Errorf("`git init -b %s` failed: %w", branch, err))
	}
	if err := gitc.StageEverything(ctx, a.Dir); err != nil {
		return nil, takeBack(a.Dir, ledger, fmt.Errorf("`git add -A` failed: %w", err))
	}
	sha, nothing, err := gitc.CommitEverything(ctx, a.Dir, "Initial commit (Aeon intake "+a.IntakeID+")")
	if err != nil {
		return nil, takeBack(a.Dir, ledger, fmt.Errorf("the initial commit failed: %w", err))
	}
	if nothing {
		return nil, takeBack(a.Dir, ledger, fmt.Errorf("the fresh repository held nothing to commit — a scaffold that commits nothing is not a scaffold"))
	}
	return mustEvidence(RepoScaffold, map[string]string{
		"commit_sha": sha,
		"branch":     branch,
		"files":      strconv.Itoa(len(files)),
	}), nil
}

// takeBack is the failure path's only broom. It runs the ledger (this
// run's files and directories, nothing else) and then faces one fact the
// ledger cannot hold: `git init` laid a .git inside a directory THIS run
// created, and a .git is nobody's to leave in a graveyard. If, after the
// ledger ran, the target holds exactly that .git and nothing else, it is
// this run's own creation and goes too. The moment a stranger's content
// is in there, the broom stops and reports — a forced delete of a
// stranger's file is the data loss this whole path exists to prevent.
func takeBack(target string, ledger *scaffold.Ledger, what error) error {
	cerr := scaffold.Cleanup(ledger)
	if st, err := os.Lstat(target); err == nil && st.IsDir() {
		if ents, err := os.ReadDir(target); err == nil && len(ents) == 1 && ents[0].IsDir() && ents[0].Name() == ".git" {
			if rerr := os.RemoveAll(filepath.Join(target, ".git")); rerr == nil {
				_ = os.Remove(target) // now empty, and ours
			}
		}
	}
	if cerr != nil {
		return fmt.Errorf("repo.scaffold: %w — and the cleanup did not finish: %v (what this run created is listed in the ledger; nothing else was touched)", what, cerr)
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("repo.scaffold: %w — this run's files were taken back; %s itself holds content this run did not write and was left exactly as found", what, target)
	}
	return fmt.Errorf("repo.scaffold: %w — the half-run was taken back; nothing of this run remains", what)
}

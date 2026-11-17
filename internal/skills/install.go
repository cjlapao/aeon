package skills

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
)

// install.go writes the managed root and nothing else. That sentence is
// the security boundary of the whole file, enforced by shape rather than
// by care: the Service keeps the two roots separate, and every write here
// resolves under managedRoot. The user root is read by discovery and never
// opened for writing by any path in this package — `aeon skills remove`
// telling an operator their own file is untouchable is the correct error,
// not a missing feature.
//
// # Install lands valid skills or lands nothing
//
// A downloaded tree is staged, parsed and validated BEFORE it is placed:
// an install that leaves a broken SKILL.md on disk and says "done" is a
// broken skill nobody can find until the bench says otherwise. The staging
// directories are dot-prefixed so discovery never counts them as skills,
// and a replacement renames the old folder to dot-prefixed trash before
// the new folder is renamed in — same filesystem, no partial states.
//
// # Nothing here runs anything
//
// The git binary is the only command this package ever executes, and it
// runs against our own temp clone. A skill's scripts/ are copied as files:
// bytes, never processes (install_test.go proves it with a script that
// would mark its execution). Symlinks inside a source tree are refused —
// a folder may be a symlink (both readers follow that), a symlink pointing
// out of the tree is how an "install" writes to somewhere you did not aim.

// InstallRequest names one install. Source is a local folder OR a GitHub
// locator (owner/repo, owner/repo@ref, owner/repo/sub/path, or a full
// https://github.com/… URL with or without /tree/<ref>/). Ref and Subpath
// are the explicit disambiguators for GitHub sources.
type InstallRequest struct {
	Source  string `json:"source"`
	Ref     string `json:"ref,omitempty"`
	Subpath string `json:"subpath,omitempty"`
	Force   bool   `json:"force,omitempty"`
}

// InstallResult reports the placed skill and whether an existing managed
// skill of that name was replaced (the operator said --force, and the
// truth of what happened is printed back at them).
type InstallResult struct {
	Skill    *Skill `json:"skill"`
	Replaced bool   `json:"replaced"`
}

// RemoveResult reports what a removal did. Revealed is true when a
// same-named skill in the user root was shadowed by the removed managed
// one and now surfaces — the fact someone needs when the list changes size
// unexpectedly after a delete.
type RemoveResult struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Revealed bool   `json:"revealed"`
}

// CloneBudget bounds the network half of an install. A shallow clone of a
// skills repo is seconds; a minute is the point where a hung network earns
// a named failure rather than a wedged socket handler.
const CloneBudget = 60 * time.Second

var ghName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Install places one skill in the managed root. Serialized through the
// service lock: two simultaneous installs renaming into the same name is
// a race no rename ordering fixes cleanly, and installs are rare.
func (s *Service) Install(ctx context.Context, req InstallRequest) (*InstallResult, error) {
	if strings.TrimSpace(req.Source) == "" {
		return nil, refuse("source", "install needs a source — a local folder or a GitHub locator")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = nil // any install invalidates the catalog, whatever follows

	srcDir, gh, err := s.resolveSource(req)
	if err != nil {
		return nil, err
	}

	// Materialize the candidate folder: a local path, or a shallow clone.
	candidate := srcDir
	if gh != nil {
		tmp, err := os.MkdirTemp("", "aeon-skills-clone-")
		if err != nil {
			return nil, fmt.Errorf("create the clone workspace: %w", err)
		}
		defer os.RemoveAll(tmp)
		cloneDir := filepath.Join(tmp, "repo")
		cctx, cancel := context.WithTimeout(ctx, CloneBudget)
		defer cancel()
		if err := s.clone(cctx, gh.url(), gh.ref, cloneDir); err != nil {
			return nil, fmt.Errorf("git clone %s: %w", gh.url(), err)
		}
		candidate, err = s.pickCandidate(cloneDir, gh.subpath)
		if err != nil {
			return nil, err
		}
	}
	if req.Subpath != "" && gh == nil {
		joined, err := safeJoin(candidate, req.Subpath)
		if err != nil {
			return nil, err
		}
		candidate = joined
	}

	// Validate before placement — and validate the name the skill is ABOUT
	// to have. The spec binds frontmatter name to the folder; the source
	// folder's current name is somebody else's business (a checkout named
	// `my-pdf-thing` whose frontmatter says `pdf` is a valid skill waiting
	// for its real name), so the check runs against `pdf`, the name it is
	// about to be placed under. A source that is invalid for any other
	// reason never reaches the managed root.
	data, over, err := readCapped(filepath.Join(candidate, "SKILL.md"), s.caps.SkillMdMax)
	if err != nil {
		return nil, err
	}
	if over {
		return nil, refuse("SKILL.md", "the source's SKILL.md exceeds %d bytes", s.caps.SkillMdMax)
	}
	split := splitFrontmatter(data)
	if split.Problem != "" {
		return nil, refuse("frontmatter", "%s", split.Problem)
	}
	fm := Frontmatter{Present: split.Present}
	if split.Present {
		fm = ParseFrontmatter(split.FM)
	} else {
		fm.ParseError = "SKILL.md carries no --- frontmatter block; the spec requires one (name and description are required fields)"
	}
	finalName := strings.TrimSpace(fm.Name)
	issues := ValidateSkill(finalName, fm, split.BodyText, countLines(split.BodyText))
	if status := StatusFor(issues); status == StatusInvalid {
		var why []string
		for _, is := range issues {
			if is.Severity == SeverityError {
				why = append(why, is.Detail)
			}
		}
		return nil, refuse("SKILL.md", "the source is not an installable skill: %s", strings.Join(why, "; "))
	}
	if finalName == "" || nameProblems(finalName) != nil {
		return nil, refuse("name", "the skill name %q is not a usable folder name", finalName)
	}

	if err := os.MkdirAll(s.managedRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create the managed skills directory %s: %w", s.managedRoot, err)
	}
	dst := filepath.Join(s.managedRoot, finalName)
	// Belt over braces: safeJoin already refuses traversal, and finalName
	// is name-regex-clean, but the destination is the one path this file
	// may ever write to, so it is checked as if the checks above could not
	// fail.
	if !strings.HasPrefix(dst, s.managedRoot+string(filepath.Separator)) {
		return nil, refuse("name", "the install target escaped the managed root")
	}

	replaced := false
	if _, err := os.Stat(dst); err == nil {
		if !req.Force {
			return nil, refuse("name", "a managed skill named %q already exists at %s — re-run with force to replace it", finalName, dst)
		}
		replaced = true
		trash := filepath.Join(s.managedRoot, ".trash-"+finalName+"-"+fmt.Sprint(s.clock().UnixNano()))
		if err := os.Rename(dst, trash); err != nil {
			return nil, fmt.Errorf("move the previous %s aside: %w", finalName, err)
		}
		defer os.RemoveAll(trash)
	}

	staging, err := os.MkdirTemp(s.managedRoot, ".staging-*")
	if err != nil {
		return nil, fmt.Errorf("create the staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := copyTree(candidate, staging, s.caps); err != nil {
		return nil, err
	}
	if err := os.Rename(staging, dst); err != nil {
		return nil, fmt.Errorf("place %s: %w", finalName, err)
	}

	installed := loadSkill(Root{SourceManaged, s.managedRoot}, dst, s.caps, s.clock())
	s.cache = nil
	return &InstallResult{Skill: installed, Replaced: replaced}, nil
}

// Remove deletes one managed skill. The user root is outside this door's
// reach by construction — a name that only exists there answers "found no
// managed skill" with the sentence naming where it does live.
func (s *Service) Remove(name string) (*RemoveResult, error) {
	name = strings.TrimSpace(name)
	if nameProblems(name) != nil {
		return nil, refuse("name", "%q is not a skill name (lowercase letters, digits and single hyphens only)", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	target := filepath.Join(s.managedRoot, name)
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		sentence := fmt.Sprintf("the managed skills directory holds no skill named %q — remove only touches %s (files under %s are the operator's own and are never deleted here)", name, s.managedRoot, s.userRoot)
		if _, uerr := os.Stat(filepath.Join(s.userRoot, name)); uerr == nil {
			sentence = fmt.Sprintf("%q lives in the user root (%s) — Aeon never deletes there; the managed root has no copy to remove", name, filepath.Join(s.userRoot, name))
		}
		return nil, refuse("name", "%s", sentence)
	}
	if err := os.RemoveAll(target); err != nil {
		return nil, fmt.Errorf("remove %s: %w", target, err)
	}
	revealed := false
	if _, uerr := os.Stat(filepath.Join(s.userRoot, name)); uerr == nil {
		revealed = true
	}
	s.cache = nil
	return &RemoveResult{Name: name, Path: target, Revealed: revealed}, nil
}

// --- source resolution -------------------------------------------------

type gitHubSpec struct {
	owner, repo, ref, subpath string
}

func (g *gitHubSpec) url() string {
	return "https://github.com/" + g.owner + "/" + g.repo + ".git"
}

// resolveSource decides folder-or-GitHub. A path that exists as a
// directory wins over the GitHub grammar (a local `owner/repo` checkout is
// what a developer means when they type it); everything else is parsed as
// a GitHub locator.
func (s *Service) resolveSource(req InstallRequest) (string, *gitHubSpec, error) {
	raw := strings.TrimSpace(req.Source)
	expanded := raw
	if strings.HasPrefix(expanded, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = filepath.Join(home, expanded[2:])
		}
	}
	expanded = strings.TrimRight(expanded, string(filepath.Separator))
	if expanded != "" && !strings.Contains(expanded, "://") && !strings.HasPrefix(expanded, "git@") {
		if info, err := os.Stat(expanded); err == nil && info.IsDir() {
			if req.Ref != "" {
				return "", nil, refuse("ref", "--ref is a GitHub option; the folder %s is whatever is on disk now", expanded)
			}
			return expanded, nil, nil
		}
	}
	gh, err := parseGitHub(raw)
	if err != nil {
		return "", nil, err
	}
	if req.Ref != "" {
		if gh.ref != "" {
			return "", nil, refuse("ref", "the locator already names a ref (%s); drop --ref", gh.ref)
		}
		gh.ref = req.Ref
	}
	return "", gh, nil
}

// parseGitHub accepts the four shapes and nothing else. Each refusal
// names the shape that WOULD have worked — the error is the documentation.
func parseGitHub(raw string) (*gitHubSpec, error) {
	s := strings.TrimSpace(raw)
	sshForm := strings.HasPrefix(s, "git@github.com:")
	if sshForm {
		s = strings.TrimPrefix(s, "git@github.com:")
	}
	for _, p := range []string{"https://", "http://", "ssh://"} {
		s = strings.TrimPrefix(s, p)
	}
	s = strings.TrimPrefix(s, "github.com/")
	s = strings.TrimPrefix(s, "www.github.com/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimRight(s, "/")

	ref := ""
	hadTree, hadAt := false, false
	if i := strings.Index(s, "/tree/"); i >= 0 {
		hadTree = true
		rest := s[i+len("/tree/"):]
		s = s[:i]
		parts := strings.SplitN(rest, "/", 2)
		ref = parts[0]
		if len(parts) > 1 {
			s = s + "/" + parts[1]
		}
	} else if strings.HasSuffix(s, "/tree") {
		// The author typed the tree marker and stopped (a URL bar's
		// trailing slash eaten on paste). It names a ref, and there is
		// none — the same refusal as `/tree/` with an empty segment.
		hadTree = true
		s = strings.TrimSuffix(s, "/tree")
	}
	if i := strings.Index(s, "@"); i > 0 {
		hadAt = true
		if ref != "" {
			return nil, refuse("ref", "the locator names a ref twice (a /tree/ segment and an @ref) — pick one")
		}
		ref = s[i+1:]
		s = s[:i]
	}
	// A marker with nothing behind it (github.com/o/r/tree/, o/r@) is the
	// author running out of intention after the prefix; the ssh form's @
	// is transport syntax, not a marker.
	if ref == "" && (hadTree || (hadAt && !sshForm)) {
		return nil, refuse("source", "the locator carries a ref marker with nothing after it — %q", raw)
	}

	segments := strings.Split(s, "/")
	if len(segments) < 2 {
		return nil, refuse("source", "%q is not a GitHub locator — use owner/repo, owner/repo/sub/path, owner/repo@ref, or a full github.com URL (or pass a local folder)", raw)
	}
	owner, repo := segments[0], segments[1]
	if !ghName.MatchString(owner) || !ghName.MatchString(repo) {
		return nil, refuse("source", "owner %q and repo %q must be plain GitHub names (letters, digits, dot, dash, underscore)", owner, repo)
	}
	subpath := strings.Join(segments[2:], "/")
	gh := &gitHubSpec{owner: owner, repo: repo, ref: ref, subpath: subpath}
	if gh.subpath != "" {
		if _, err := safeJoin("/x", gh.subpath); err != nil {
			return nil, err
		}
	}
	return gh, nil
}

// pickCandidate decides WHICH folder of a tree is the skill: the explicit
// subpath wins; then a SKILL.md at the tree root; then exactly-one under
// skills/; then exactly-one top-level folder. More than one candidate is
// a refusal that lists the candidates — guessing which one the operator
// meant is how the wrong skill gets installed.
func (s *Service) pickCandidate(tree, subpath string) (string, error) {
	if subpath != "" {
		cand, err := safeJoin(tree, subpath)
		if err != nil {
			return "", err
		}
		if !hasSkillFile(cand) {
			return "", refuse("subpath", "%q has no SKILL.md", subpath)
		}
		return cand, nil
	}
	if hasSkillFile(tree) {
		return tree, nil
	}
	for _, dir := range []string{"skills", "."} {
		base := filepath.Join(tree, dir)
		found := skillFolders(base)
		if len(found) == 1 {
			return found[0], nil
		}
		if len(found) > 1 {
			names := make([]string, 0, len(found))
			for _, f := range found {
				rel, _ := filepath.Rel(tree, f)
				names = append(names, rel)
			}
			sort.Strings(names)
			return "", refuse("subpath", "the repository offers %d skills (%s) — name one with a subpath or a full URL to the skill's folder",
				len(found), strings.Join(names, ", "))
		}
		if dir == "skills" && len(found) == 0 {
			if _, err := os.Stat(base); err == nil {
				// a skills/ directory that holds nothing shippable is an
				// answer of its own; do not silently fall through to the
				// whole repo when the repo clearly parks skills there.
				return "", refuse("subpath", "the repository's skills/ folder holds no folder with a SKILL.md")
			}
		}
	}
	return "", refuse("source", "the repository has no SKILL.md at its root, in skills/, or in any top-level folder — name the skill's folder explicitly")
}

// skillFolders lists the direct child folders of base that hold a
// SKILL.md (the same root-scan rule discovery uses: dot/underscore names
// skipped, symlinks followed).
func skillFolders(base string) []string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		dir := filepath.Join(base, name)
		if isSkillDir(dir, e) {
			out = append(out, dir)
		}
	}
	return out
}

func hasSkillFile(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil && !info.IsDir()
}

// safeJoin resolves rel under base and refuses anything — absolute paths,
// "..", backslash games — that resolves outside base. The check is on the
// CLEANED result, not the input string, because `a/../b` and `b` are the
// same path and only the cleaned form can be compared.
func safeJoin(base, rel string) (string, error) {
	if filepath.IsAbs(rel) || strings.Contains(rel, "\\") {
		return "", refuse("subpath", "%q must be a path inside the repository, not absolute", rel)
	}
	clean := filepath.Clean(filepath.Join(base, filepath.FromSlash(rel)))
	prefix := base + string(filepath.Separator)
	if clean != base && !strings.HasPrefix(clean, prefix) {
		return "", refuse("subpath", "%q resolves outside its repository tree", rel)
	}
	return clean, nil
}

// --- tree copy ----------------------------------------------------------

// copyTree copies a skill folder under the scan's caps, refusing symlinks
// and special files. The refusal is deliberate rather than tolerant: both
// reference readers follow symlinks OUT of the folder (Claude follows the
// skill folder itself), which means a hostile repo can point an "install"
// at any readable file on the machine. A skill folder that needs a
// symlink can be linked at the ROOT (the person points the folder in
// themselves); files inside the tree are regular files or nothing.
func copyTree(src, dst string, caps Limits) error {
	if !hasSkillFile(src) {
		return refuse("source", "%s holds no SKILL.md", src)
	}
	var files int
	var total int64
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", rel(src, p), err)
		}
		if p == src {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return refuse("source", "the tree contains a symlink (%s) — skills are copied as plain files; link the skill folder itself if you need a shared one", rel(src, p))
		}
		target := filepath.Join(dst, rel(src, p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, ierr := d.Info()
		if ierr != nil {
			return fmt.Errorf("stat %s: %w", rel(src, p), ierr)
		}
		if !info.Mode().IsRegular() {
			return refuse("source", "the tree contains a non-regular file (%s)", rel(src, p))
		}
		files++
		total += info.Size()
		if files > caps.FilesPerSkill || total > caps.BytesPerSkill {
			return refuse("source", "the tree exceeds the install bounds (%d files / %d bytes)", caps.FilesPerSkill, caps.BytesPerSkill)
		}
		in, oerr := os.Open(p)
		if oerr != nil {
			return fmt.Errorf("open %s: %w", rel(src, p), oerr)
		}
		defer in.Close()
		mode := info.Mode().Perm()
		out, cerr := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if cerr != nil {
			return fmt.Errorf("create %s: %w", rel(src, p), cerr)
		}
		if _, cerr := io.Copy(out, in); cerr != nil {
			out.Close()
			return fmt.Errorf("copy %s: %w", rel(src, p), cerr)
		}
		return out.Close()
	})
}

// --- the real GitHub transport -------------------------------------------

// gitCloneShallow is the default clone: one depth-1 git clone of one URL
// into a fresh directory. It is the ONLY process this package starts; the
// -- separator stops the URL or path from being read as flags, and
// context cancellation (the budget) reaches the child through
// CommandContext.
func gitCloneShallow(ctx context.Context, url, ref, dir string) error {
	args := []string{"clone", "--depth", "1", "--quiet"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", url, dir)
	cmd := exec.CommandContext(ctx, "git", args...)
	// S59 T6 (EV1). This clone starts a process whose CONTENTS the daemon does
	// not control: a skills repository from the network, whose `.git/config`
	// can name a smudge filter and whose hooks run on checkout. Leaving `Env`
	// nil would hand that repository the daemon's whole environment — every
	// provider key the process holds — which is the leak this slice exists to
	// close. It runs under the same git allowlist the daemon's own git runs
	// under (T2's `execenv.GitEnv`), which carries what git needs to fetch,
	// including the agent socket for a private repo, and nothing else.
	cmd.Env = execenv.GitEnv(execenv.DaemonEnv{Lookup: os.Getenv, Names: processEnvNames}, execenv.GitEnvDaemon)
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("%s (%s)", firstLine(detail), err)
	}
	return nil
}

// processEnvNames is the wholesale read, named: the ONLY place this package
// looks at the process environment, and it hands over NAMES to the allowlist —
// never values. A file that reads `os.Environ()` and passes it to a child is
// what `TestNoSpawnInheritsTheDaemonEnvironment` refuses; reading names in
// order to FILTER them is how a child gets a legal environment.
func processEnvNames() []string {
	raw := os.Environ()
	out := make([]string, 0, len(raw))
	for _, kv := range raw {
		if i := strings.IndexByte(kv, '='); i > 0 {
			out = append(out, kv[:i])
		}
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

package skills

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// discover.go answers "what skills exist" by looking. No registry, no
// index: a skill IS its folder (the same fact S13 built first-party agents
// on), and a scan is cheap enough that the catalog can re-derive whenever
// asked. The roots are given in precedence order — the user root first —
// and the winner of a name clash is whichever root came first, with the
// loser kept visible as shadowed.
//
// A folder counts as a skill candidate when it is a directory (symlinked
// directories are followed, as both Claude Code and Codex follow them),
// its name does not start with "." or "_" (the ecosystem parks trash and
// curation dirs under those), and it holds a readable SKILL.md — that last
// test is what separates a skill folder from a `references/` someone left
// at the root.
//
// Nested SKILL.md files (microsoft-foundry ships five) belong to their
// parent skill as resources, exactly as they do for both readers — only
// the root's direct children enumerate as skills.

// Root is one scanned directory with its provenance tag.
type Root struct {
	Source Source
	Dir    string
}

// Discover scans the roots in order and returns every skill found, sorted
// by name with winners first. Errors are per-root (an unreadable root says
// so in the result — a MISSING root is the ordinary first-run state and
// scans as zero skills, not a failure). A folder that cannot be read or
// parsed still lands in the result as an invalid skill with its sentence,
// because "the skill exists and is broken" is what the bench must show.
func Discover(roots []Root, caps Limits, clock func() time.Time) (*ScanResult, error) {
	if caps.EntriesPerRoot <= 0 {
		caps = DefaultLimits()
	}
	if clock == nil {
		clock = time.Now
	}
	res := &ScanResult{ScannedAt: clock().UTC()}
	seen := map[string]*Skill{}

	for _, r := range roots {
		switch r.Source {
		case SourceUser:
			res.UserRoot = r.Dir
		case SourceManaged:
			res.ManagedRoot = r.Dir
		}
		entries, err := os.ReadDir(r.Dir)
		if err != nil {
			if os.IsNotExist(err) || notDir(err) {
				continue // a root that does not exist yet is the empty case, not a fault
			}
			return nil, fmt.Errorf("read the skills root %s: %w", r.Dir, err)
		}
		count := 0
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				continue
			}
			dir := filepath.Join(r.Dir, name)
			if !isSkillDir(dir, entry) {
				continue
			}
			if count >= caps.EntriesPerRoot {
				res.Skills = append(res.Skills, invalidEntry(r, name, dir,
					fmt.Sprintf("the root holds more than %d skill folders — the bound exists so a broken tree cannot make a scan unbounded; this folder was not opened", caps.EntriesPerRoot), clock()))
				continue
			}
			count++
			sk := loadSkill(r, dir, caps, clock())
			if prev, clash := seen[sk.Name]; clash {
				sk.Shadowed = true
				sk.ShadowedBy = prev.Path
			} else {
				seen[sk.Name] = sk
			}
			res.Skills = append(res.Skills, sk)
		}
	}

	sort.SliceStable(res.Skills, func(i, j int) bool {
		a, b := res.Skills[i], res.Skills[j]
		if a.Shadowed != b.Shadowed {
			return !a.Shadowed
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Path < b.Path
	})

	for _, sk := range res.Skills {
		switch sk.Status {
		case StatusValid:
			res.Summary.Valid++
		case StatusWarn:
			res.Summary.Warn++
		case StatusInvalid:
			res.Summary.Invalid++
		}
		if sk.Shadowed {
			res.Summary.Shadowed++
		}
	}
	return res, nil
}

// isSkillDir decides whether one root entry names a skill folder,
// following symlinks exactly as the two reference readers do.
func isSkillDir(dir string, entry os.DirEntry) bool {
	if entry.IsDir() {
		_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
		return err == nil
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(dir) // Stat follows the link
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil
}

// loadSkill reads and validates one skill folder. Every failure mode —
// unreadable file, oversized file, unparsable frontmatter — produces a
// Skill with its issues rather than an absent entry or a panic.
func loadSkill(r Root, dir string, caps Limits, at time.Time) *Skill {
	sk := &Skill{
		Name:      filepath.Base(dir),
		Source:    r.Source,
		Root:      r.Dir,
		Path:      dir,
		Metadata:  map[string]string{},
		Unknown:   map[string]string{},
		ScannedAt: at,
	}

	data, over, err := readCapped(filepath.Join(dir, "SKILL.md"), caps.SkillMdMax)
	if err != nil {
		sk.Issues = append(sk.Issues, Issue{Field: "SKILL.md", Detail: err.Error(), Severity: SeverityError})
		sk.Status = StatusInvalid
		return sk
	}
	if over {
		sk.Issues = append(sk.Issues, Issue{Field: "SKILL.md", Detail: fmt.Sprintf("SKILL.md exceeds %d bytes and was not parsed", caps.SkillMdMax), Severity: SeverityError})
		sk.Status = StatusInvalid
		return sk
	}

	split := splitFrontmatter(data)
	if split.Problem != "" {
		sk.Issues = append(sk.Issues, Issue{Field: "frontmatter", Detail: split.Problem, Severity: SeverityError})
		sk.Status = StatusInvalid
		return sk
	}

	fm := Frontmatter{Present: split.Present}
	if split.Present {
		fm = ParseFrontmatter(split.FM)
	} else {
		fm.ParseError = "SKILL.md carries no --- frontmatter block; the spec requires one (name and description are required fields)"
	}
	sk.FrontmatterName = fm.Name
	sk.Description = fm.Description
	sk.License = fm.License
	sk.Compatibility = fm.Compatibility
	sk.AllowedTools = fm.AllowedTools
	if fm.Metadata != nil {
		sk.Metadata = fm.Metadata
	}
	if fm.Unknown != nil {
		sk.Unknown = fm.Unknown
	}

	sk.BodyBytes = len(strings.TrimSpace(split.BodyText))
	sk.BodyLines = countLines(split.BodyText)

	counts, total, walkIssue := manifest(dir, caps)
	sk.Files = counts
	sk.TotalBytes = total
	if walkIssue != nil {
		sk.Issues = append(sk.Issues, *walkIssue)
	}

	sk.Issues = append(sk.Issues, ValidateSkill(sk.Name, fm, split.BodyText, sk.BodyLines)...)
	sk.Status = StatusFor(sk.Issues)
	if len(sk.Metadata) == 0 {
		sk.Metadata = nil
	}
	if len(sk.Unknown) == 0 {
		sk.Unknown = nil
	}
	return sk
}

// invalidEntry is a skill that never got opened (the per-root bound was
// already spent) — a named, counted, INVALID entry, never a silence.
func invalidEntry(r Root, name, dir, detail string, at time.Time) *Skill {
	return &Skill{
		Name:      name,
		Source:    r.Source,
		Root:      r.Dir,
		Path:      dir,
		Status:    StatusInvalid,
		Issues:    []Issue{{Field: "root", Detail: detail, Severity: SeverityError}},
		ScannedAt: at,
	}
}

// manifest walks one skill folder for the file counts and the total. The
// spec names scripts/, references/ and assets/ as conventions; the walk
// counts by those names (case-sensitive — the spec spells them lowercase
// and the two readers match them exactly) and everything else as Other.
// Symlinks inside a skill are counted but never followed: a skill folder
// may be a symlink (both readers follow that), a symlinked FILE inside it
// is at best a broken promise and at worst an escape hatch out of the
// scanned root.
func manifest(dir string, caps Limits) (FileCounts, int64, *Issue) {
	var counts FileCounts
	var total int64
	var issue *Issue
	budget := caps.FilesPerSkill

	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if issue == nil {
				issue = &Issue{Field: "files", Detail: fmt.Sprintf("the folder walk stopped at %s: %v", rel(dir, p), err), Severity: SeverityWarning}
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p == dir {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if budget <= 0 {
			if issue == nil {
				issue = &Issue{Field: "files", Detail: fmt.Sprintf("the folder holds more than %d files; counting stopped — the bound is there so one broken folder cannot make a scan unbounded", caps.FilesPerSkill), Severity: SeverityWarning}
			}
			return fs.SkipAll
		}
		budget--

		info, ierr := d.Info()
		size := int64(0)
		if ierr == nil && d.Type()&os.ModeSymlink == 0 {
			size = info.Size()
			total += size
			if total > caps.BytesPerSkill && issue == nil {
				issue = &Issue{Field: "files", Detail: fmt.Sprintf("the folder exceeds %d bytes; the total is a floor, not the truth — the bound is there so a packrat folder cannot exhaust the scan", caps.BytesPerSkill), Severity: SeverityWarning}
			}
		}

		switch top(dir, p) {
		case "scripts":
			counts.Scripts++
		case "references":
			counts.References++
		case "assets":
			counts.Assets++
		case "SKILL.md":
			// The manifest describes what the skill SHIPS beyond itself;
			// SKILL.md is the skill, not support material.
		default:
			counts.Other++
		}
		return nil
	})
	if err != nil && issue == nil {
		issue = &Issue{Field: "files", Detail: fmt.Sprintf("the folder walk failed: %v", err), Severity: SeverityWarning}
	}
	return counts, total, issue
}

// top returns the first path element of p below base.
func top(base, p string) string {
	relPath, err := filepath.Rel(base, p)
	if err != nil {
		return ""
	}
	parts := strings.SplitN(filepath.ToSlash(relPath), "/", 2)
	return parts[0]
}

func rel(base, p string) string {
	if r, err := filepath.Rel(base, p); err == nil {
		return r
	}
	return p
}

func countLines(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1
}

// readCapped reads at most max+1 bytes and reports whether the file
// exceeded the cap — the caller must tell "exactly at the cap" from "over
// it": an answer that cannot say "there was more" is an answer that lies.
// When over is true, data holds the first max+1 bytes: enough to render a
// truncated preview honestly, never more.
func readCapped(path string, max int64) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, fmt.Errorf("the folder holds no readable SKILL.md: %v", err)
		}
		return nil, false, fmt.Errorf("SKILL.md could not be opened: %v", err)
	}
	defer f.Close()
	buf, rerr := io.ReadAll(io.LimitReader(f, max+1))
	if rerr != nil {
		return nil, false, fmt.Errorf("SKILL.md could not be read: %v", rerr)
	}
	return buf, int64(len(buf)) > max, nil
}

func notDir(err error) bool {
	// ENOTDIR on POSIX (and defined on Windows): "the configured root is a
	// file" is a missing root for scan purposes, not a scan failure — the
	// operator's fix is to point the env var at a directory, and the bench
	// shows each root's path to say so.
	return errors.Is(err, syscall.ENOTDIR) || strings.Contains(strings.ToLower(err.Error()), "not a directory")
}

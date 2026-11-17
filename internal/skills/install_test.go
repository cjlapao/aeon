package skills

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// S65 T5 — the install engine: staging, validation-before-placement,
// the GitHub grammar, the managed-root-only write rule, and the proof
// that installing never EXECUTES what it copies.

func newTestService(t *testing.T) (*Service, string, string) {
	t.Helper()
	user, managed := t.TempDir(), t.TempDir()
	svc, err := NewService(Options{
		Getenv: func(k string) string {
			switch k {
			case "AEON_USER_SKILLS_DIR":
				return user
			case "AEON_SKILLS_DIR":
				return managed
			}
			return ""
		},
		Clock: func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, user, managed
}

func TestInstallFromFolderLandsUnderFrontmatterName(t *testing.T) {
	svc, _, managed := newTestService(t)
	src := t.TempDir()
	// The source folder's name disagrees with the frontmatter on purpose:
	// placement is named by the file, which restores the spec binding.
	writeSkill(t, src, "my-pdf-thing", "---\nname: pdf\ndescription: Extracts text from PDFs and merges them when asked about PDF documents.\n---\n# pdf\n")
	writeText(t, filepath.Join(src, "my-pdf-thing", "scripts", "x.py"), "print(1)\n")

	res, err := svc.Install(context.Background(), InstallRequest{Source: src + "/my-pdf-thing"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skill.Name != "pdf" || res.Skill.Status != StatusValid {
		t.Fatalf("installed view: %+v", res.Skill)
	}
	if _, err := os.Stat(filepath.Join(managed, "pdf", "SKILL.md")); err != nil {
		t.Fatalf("placement: %v", err)
	}
	if res.Replaced {
		t.Fatal("fresh install reports a replacement")
	}
	// The catalog answers it after Invalidate.
	scan, err := svc.Scan(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Skills) != 1 || scan.Skills[0].Name != "pdf" || scan.Skills[0].Source != SourceManaged {
		t.Fatalf("catalog after install: %+v", scan.Skills)
	}
}

func TestInstallRefusesInvalidSourceAndLandsNothing(t *testing.T) {
	svc, _, managed := newTestService(t)
	src := t.TempDir()
	writeSkill(t, src, "broken", "---\ndescription: no name here at all, and a long enough description to pass the thin check.\n---\nbody\n")

	_, err := svc.Install(context.Background(), InstallRequest{Source: src + "/broken"})
	var ref *ErrRefused
	if !errors.As(err, &ref) || !strings.Contains(ref.Detail, "no name") {
		t.Fatalf("refusal must name the fault: %v", err)
	}
	ents, _ := os.ReadDir(managed)
	for _, e := range ents {
		t.Fatalf("a refused install must leave nothing behind, found %s", e.Name())
	}
}

func TestInstallRefusesScriptsItWouldCopy(t *testing.T) {
	// The marker proves the one rule install must never break: it copies
	// bytes, it does not run them. A script named to run during install
	// (if anything ever did) would write here.
	svc, _, managed := newTestService(t)
	src := t.TempDir()
	writeSkill(t, src, "marker", "---\nname: marker\ndescription: Ships a script that would mark its own execution if anything ran it.\n---\nrun scripts/marker.sh\n")
	marker := filepath.Join(src, "marker", "scripts", "marker.sh")
	writeText(t, marker, "#!/bin/sh\ntouch "+filepath.Join(src, "EXECUTED")+"\n")
	if err := os.Chmod(marker, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Install(context.Background(), InstallRequest{Source: src + "/marker"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(src, "EXECUTED")); err == nil {
		t.Fatal("install EXECUTED a copied script")
	}
	// The executable bit rides along (the bytes are honest copies) but
	// nothing ran.
	info, err := os.Stat(filepath.Join(managed, "marker", "scripts", "marker.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatal("copy lost the executable bit — a byte-level copy keeps it")
	}
}

func TestInstallForceReplacesAndKeepsNothingOfTheOld(t *testing.T) {
	svc, _, managed := newTestService(t)
	src := t.TempDir()
	writeSkill(t, src, "pdf", "---\nname: pdf\ndescription: The first version, described at length so validation passes.\n---\nold\n")
	writeText(t, filepath.Join(src, "pdf", "STALE.md"), "old file")
	if _, err := svc.Install(context.Background(), InstallRequest{Source: src + "/pdf"}); err != nil {
		t.Fatal(err)
	}

	writeText(t, filepath.Join(src, "pdf", "STALE.md"), "gone next install")
	// Removing STALE.md from the source and reinstalling without force is
	// refused; with force the placed folder is exactly the source.
	if err := os.Remove(filepath.Join(src, "pdf", "STALE.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Install(context.Background(), InstallRequest{Source: src + "/pdf"}); err == nil {
		t.Fatal("overwrite without force must be refused")
	} else {
		var ref *ErrRefused
		if !errors.As(err, &ref) || !strings.Contains(ref.Detail, "force") {
			t.Fatalf("refusal must name the remedy: %v", err)
		}
	}
	res, err := svc.Install(context.Background(), InstallRequest{Source: src + "/pdf", Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Replaced {
		t.Fatal("force install must say it replaced")
	}
	if _, err := os.Stat(filepath.Join(managed, "pdf", "STALE.md")); err == nil {
		t.Fatal("the replaced folder must not survive as extra files under the new name")
	}
	// No staging or trash litter is left in the managed root.
	ents, _ := os.ReadDir(managed)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".staging") || strings.HasPrefix(e.Name(), ".trash") {
			t.Fatalf("install litter: %s", e.Name())
		}
	}
}

func TestRemoveTouchesOnlyTheManagedRoot(t *testing.T) {
	svc, user, managed := newTestService(t)
	// The user's own file with the same name must survive every remove.
	writeSkill(t, user, "pdf", "---\nname: pdf\ndescription: The user owns this file and Aeon must never delete it, ever.\n---\nu\n")
	writeSkill(t, managed, "pdf", "---\nname: pdf\ndescription: The installed copy that remove may delete when asked.\n---\nm\n")

	res, err := svc.Remove("pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Revealed {
		t.Fatal("removing the managed copy must reveal the shadowed user skill")
	}
	if _, err := os.Stat(filepath.Join(user, "pdf", "SKILL.md")); err != nil {
		t.Fatal("the user root was touched")
	}
	if _, err := os.Stat(filepath.Join(managed, "pdf")); err == nil {
		t.Fatal("the managed copy is still there")
	}

	// Removing again answers with the sentence that says where the file
	// actually lives — and still does not delete it.
	_, err = svc.Remove("pdf")
	var ref *ErrRefused
	if !errors.As(err, &ref) || !strings.Contains(ref.Detail, "user root") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(filepath.Join(user, "pdf", "SKILL.md")); err != nil {
		t.Fatal("the user root was touched by a refusal")
	}
}

func TestGitHubGrammar(t *testing.T) {
	cases := []struct {
		in      string
		owner   string
		repo    string
		ref     string
		sub     string
		wantErr bool
	}{
		{in: "anthropics/skills", owner: "anthropics", repo: "skills"},
		{in: "owner/repo@v2", owner: "owner", repo: "repo", ref: "v2"},
		{in: "owner/repo/skills/pdf", owner: "owner", repo: "repo", sub: "skills/pdf"},
		{in: "https://github.com/owner/repo", owner: "owner", repo: "repo"},
		{in: "https://github.com/owner/repo/tree/main/skills/pdf", owner: "owner", repo: "repo", ref: "main", sub: "skills/pdf"},
		{in: "git@github.com:owner/repo.git", owner: "owner", repo: "repo"},
		{in: "owner/repo.git", owner: "owner", repo: "repo"},
		{in: "justone", wantErr: true},
		{in: "owner/repo@", wantErr: true},
		{in: "github.com/owner/repo/tree/", wantErr: true},
		{in: "owner/repo/../evil", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			gh, err := parseGitHub(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("must refuse %q", c.in)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if gh.owner != c.owner || gh.repo != c.repo || gh.ref != c.ref || gh.subpath != c.sub {
				t.Fatalf("spec %+v", gh)
			}
		})
	}
}

func TestInstallGitHubCandidateResolutionThroughInjectedClone(t *testing.T) {
	// The clone transport is injected; the REAL transport gets its own
	// test against a local git repo below. What this test pins is the
	// candidate grammar and that the network bytes are staged, validated
	// and placed like any folder.
	svc, _, managed := newTestService(t)

	svc.clone = func(ctx context.Context, url, ref, dir string) error {
		if !strings.HasSuffix(url, "skills.git") || ref != "main" {
			t.Fatalf("clone called with url=%s ref=%s", url, ref)
		}
		// A repo shaped like anthropics/skills: skills/<name>/SKILL.md.
		writeSkill(t, dir, "skills/pdf", "---\nname: pdf\ndescription: A PDF skill cloned from a fake origin with a real description.\n---\nbody\n")
		writeSkill(t, dir, "skills/docx", "---\nname: docx\ndescription: Another skill in the same fake origin, described enough.\n---\nbody\n")
		return nil
	}

	// Two candidates and no subpath → refusal that LISTS them.
	_, err := svc.Install(context.Background(), InstallRequest{Source: "acme/skills@main"})
	var ref *ErrRefused
	if !errors.As(err, &ref) || !strings.Contains(ref.Detail, "pdf") || !strings.Contains(ref.Detail, "docx") {
		t.Fatalf("ambiguous repo must refuse with the candidate list: %v", err)
	}

	// The explicit subpath lands exactly that skill.
	res, err := svc.Install(context.Background(), InstallRequest{Source: "acme/skills/skills/pdf@main"})
	if err != nil {
		t.Fatalf("subpath install: %v", err)
	}
	if res.Skill.Name != "pdf" {
		t.Fatalf("%+v", res.Skill)
	}
	if _, err := os.Stat(filepath.Join(managed, "pdf", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestRealGitCloneAgainstLocalOrigin(t *testing.T) {
	// The default transport (gitCloneShallow) exercised for real: origin
	// is a local repo on disk — no network, same git binary.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	origin := t.TempDir()
	seed := t.TempDir()
	writeSkill(t, seed, "seeded", "---\nname: seeded\ndescription: A skill in a local git origin used as a GitHub stand-in. Enough text.\n---\nbody\n")
	runGit(t, origin, "init", "-q", "--bare")
	runGit(t, seed, "init", "-q")
	runGit(t, seed, "config", "user.email", "t@t")
	runGit(t, seed, "config", "user.name", "t")
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "commit", "-q", "-m", "seed")
	runGit(t, seed, "branch", "-M", "main")
	runGit(t, seed, "push", "-q", origin, "main")

	svc, _, managed := newTestService(t)
	// Point the service's clone at the real transport with the origin path
	// substituted for the GitHub URL (the transport itself is under test).
	svc.clone = func(ctx context.Context, url, ref, dir string) error {
		return gitCloneShallow(ctx, origin, ref, dir)
	}
	res, err := svc.Install(context.Background(), InstallRequest{Source: "acme/skills"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skill.Name != "seeded" {
		t.Fatalf("%+v", res.Skill)
	}
	if _, err := os.Stat(filepath.Join(managed, "seeded", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestInstallRefusesSymlinksInsideTheTree(t *testing.T) {
	if os.Geteuid() == -1 {
		t.Skip("symlinks are POSIX")
	}
	svc, _, _ := newTestService(t)
	src := t.TempDir()
	writeSkill(t, src, "sneaky", "---\nname: sneaky\ndescription: A skill whose scripts folder symlinks to somebody else's file.\n---\nbody\n")
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "sneaky", "scripts")); err != nil {
		t.Skipf("symlinks not permitted: %v", err)
	}
	_, err := svc.Install(context.Background(), InstallRequest{Source: src + "/sneaky"})
	var ref *ErrRefused
	if !errors.As(err, &ref) || !strings.Contains(ref.Detail, "symlink") {
		t.Fatalf("a symlinked subtree must be refused by name: %v", err)
	}
}

func TestCatalogGetTruncatesHonestly(t *testing.T) {
	user := t.TempDir()
	long := "---\nname: big\ndescription: A skill with a body long enough to hit the test cap, described well.\n---\n" + strings.Repeat("x", 5000) + "\n"
	writeSkill(t, user, "big", long)
	svc, err := NewService(Options{
		Getenv: func(k string) string {
			if k == "AEON_USER_SKILLS_DIR" {
				return user
			}
			if k == "AEON_SKILLS_DIR" {
				return t.TempDir()
			}
			return ""
		},
		Limits: &Limits{EntriesPerRoot: 100, FilesPerSkill: 100, BytesPerSkill: 1 << 20, SkillMdMax: 1 << 20, BodyMax: 256},
		Clock:  func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.Get("big")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated {
		t.Fatal("a body over the cap must arrive flagged, not silently shortened")
	}
	if int64(len(res.Body)) > 256 {
		t.Fatalf("truncated body still exceeds the cap: %d", len(res.Body))
	}
	if _, err := svc.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a miss is ErrNotFound, not a fault: %v", err)
	}
}

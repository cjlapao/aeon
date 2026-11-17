package scaffold_test

// scaffold_test.go — S61 T3's renderer law (GI9): the closed variable set
// and function table, the path-escape matrix, the mode pair and the size
// caps, a preview that is the written bytes verbatim, a writer that only
// creates and only takes back what it made, and the target guard refusing
// every ground that is not a fresh directory a person pointed at.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/scaffold"
)

func giVars() scaffold.Vars {
	return scaffold.Vars{Name: "Acme Store", Slug: "acme-store",
		Description: "A small shop.", Stack: []string{"go", "sqlite"}, DefaultBranch: "main"}
}

func oneFile(path, content string, mode os.FileMode) scaffold.Template {
	return scaffold.Template{Slug: "t", Files: []scaffold.TemplateFile{{Path: path, Mode: mode, Content: content}}}
}

func TestVarsAndFunctionsAreClosed(t *testing.T) {
	out, err := scaffold.Render(oneFile("{{.Slug}}/README.md", "hello {{.Name}} ({{.Description}}) branch {{.DefaultBranch}} stack {{join .Stack \", \"}}", 0o644), giVars())
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Path != "acme-store/README.md" || !strings.Contains(string(out[0].Content), `hello Acme Store (A small shop.) branch main stack go, sqlite`) {
		t.Fatalf("rendered = %q / %q", out[0].Path, out[0].Content)
	}
	// lower/upper are in; anything else is out.
	if _, err := scaffold.Render(oneFile("a", "{{.Name | upper}}", 0o644), giVars()); err != nil {
		t.Fatalf("upper refused: %v", err)
	}
	if _, err := scaffold.Render(oneFile("a", "{{.Name | exec}}", 0o644), giVars()); err == nil {
		t.Fatal("exec rendered — outside the package built-ins the function table is lower, upper, join ONLY")
	}
	if _, err := scaffold.Render(oneFile("a", `{{ env "HOME" }}`, 0o644), giVars()); err == nil {
		t.Fatal("an env function rendered — the renderer has no road out of strings")
	}
	if _, err := scaffold.Render(oneFile("a", "{{ call .Name }}", 0o644), giVars()); err == nil {
		t.Fatal("a template executed a value as code")
	}
}

func TestUnknownVariableIsRefused(t *testing.T) {
	if _, err := scaffold.Render(oneFile("a", "{{.Nope}}", 0o644), giVars()); err == nil {
		t.Fatal("an unknown variable rendered empty — missingkey=error means REFUSED")
	}
}

func TestPathEscapeMatrix(t *testing.T) {
	// The matrix runs through RENDERED paths: a variable that smuggles an
	// escape is exactly what the store-side check cannot see.
	bad := map[string]string{
		"parent":      "../outside",
		"grandparent": "a/../../outside",
		"absolute":    "/etc/passwd",
		"git-dir":     ".git/config",
		"git-nested":  "sub/.git/hooks",
		"nul":         "a\x00b",
		"backslash":   `C:\evil`,
		"self":        ".",
		"long":        strings.Repeat("x", 256),
	}
	for name, p := range bad {
		if _, err := scaffold.Render(oneFile(p, "x", 0o644), giVars()); err == nil {
			t.Errorf("%s: path %q rendered without refusal", name, p)
		}
	}
	// Smuggling through a variable.
	v := giVars()
	v.Slug = "../../etc"
	if _, err := scaffold.Render(oneFile("{{.Slug}}/passwd", "x", 0o644), v); err == nil {
		t.Fatal("a slug variable smuggled a path escape")
	}
}

func TestDuplicateAndCollidingPathsRefused(t *testing.T) {
	dup := scaffold.Template{Slug: "t", Files: []scaffold.TemplateFile{
		{Path: "README.md", Mode: 0o644, Content: "a"},
		{Path: "README.md", Mode: 0o644, Content: "b"},
	}}
	if _, err := scaffold.Render(dup, giVars()); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("duplicate path: %v", err)
	}
	fold := scaffold.Template{Slug: "t", Files: []scaffold.TemplateFile{
		{Path: "README.md", Mode: 0o644, Content: "a"},
		{Path: "readme.md", Mode: 0o644, Content: "b"},
	}}
	if _, err := scaffold.Render(fold, giVars()); err == nil || !strings.Contains(err.Error(), "case-fold") {
		t.Fatalf("case-fold collision: %v", err)
	}
}

func TestModesAreTheClosedPair(t *testing.T) {
	for _, m := range []os.FileMode{0o600, 0o777, 0o754, 0} {
		if _, err := scaffold.Render(oneFile("a", "x", m), giVars()); err == nil {
			t.Errorf("mode %o rendered — only 0644 and 0755 exist", m)
		}
	}
	for _, m := range []os.FileMode{0o644, 0o755} {
		if _, err := scaffold.Render(oneFile("a", "x", m), giVars()); err != nil {
			t.Errorf("mode %o refused: %v", m, err)
		}
	}
}

func TestSizeCaps(t *testing.T) {
	over := strings.Repeat("y", scaffold.MaxFileByte+1)
	if _, err := scaffold.Render(oneFile("a", over, 0o644), giVars()); err == nil {
		t.Fatal("a file over the per-file cap rendered")
	}
	var many []scaffold.TemplateFile
	for i := 0; i < scaffold.MaxFiles+1; i++ {
		many = append(many, scaffold.TemplateFile{Path: fmt.Sprintf("f%03d", i), Mode: 0o644, Content: "x"})
	}
	if _, err := scaffold.Render(scaffold.Template{Slug: "t", Files: many}, giVars()); err == nil {
		t.Fatal("over the file-count cap rendered")
	}
	var fat []scaffold.TemplateFile
	big := strings.Repeat("z", 100*1024)
	for i := 0; i < 6; i++ { // 600KiB total, each under the per-file cap
		fat = append(fat, scaffold.TemplateFile{Path: fmt.Sprintf("f%d", i), Mode: 0o644, Content: big})
	}
	if _, err := scaffold.Render(scaffold.Template{Slug: "t", Files: fat}, giVars()); err == nil {
		t.Fatal("over the total cap rendered")
	}
}

func TestPlanRefusesExistingAndSymlinkedGround(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "proj")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "taken.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(target, "real")
	link := filepath.Join(target, "link")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	taken := scaffold.Template{Slug: "t", Files: []scaffold.TemplateFile{{Path: "taken.txt", Mode: 0o644, Content: "x"}}}
	pf, err := scaffold.Render(taken, giVars())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scaffold.Plan(target, pf); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing file planned: %v", err)
	}
	viaLink := scaffold.Template{Slug: "t", Files: []scaffold.TemplateFile{{Path: "link/inside", Mode: 0o644, Content: "x"}}}
	lf, err := scaffold.Render(viaLink, giVars())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scaffold.Plan(target, lf); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked component planned: %v", err)
	}
}

func TestPreviewEqualsWhatWasWritten(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "proj")
	tmpl := scaffold.Template{Slug: "t", Files: []scaffold.TemplateFile{
		{Path: "README.md", Mode: 0o644, Content: "hello {{.Name}}\n"},
		{Path: "scripts/run.sh", Mode: 0o755, Content: "#!/bin/sh\necho {{.Slug}}\n"},
		{Path: ".gitignore", Mode: 0o644, Content: "*.log\n"},
	}}
	files, err := scaffold.Render(tmpl, giVars())
	if err != nil {
		t.Fatal(err)
	}
	pv, err := scaffold.Plan(target, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.Files) != 3 || len(pv.Create) == 0 {
		t.Fatalf("preview = %+v", pv)
	}
	var led scaffold.Ledger
	if err := scaffold.Write(target, files, &led); err != nil {
		t.Fatal(err)
	}
	for _, pf := range pv.Files {
		b, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(pf.Path)))
		if err != nil {
			t.Fatalf("%s: %v", pf.Path, err)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != pf.SHA256 {
			t.Errorf("%s: written hash != preview hash", pf.Path)
		}
		if len(b) != pf.Size {
			t.Errorf("%s: written size %d, preview %d", pf.Path, len(b), pf.Size)
		}
		st, err := os.Stat(filepath.Join(target, filepath.FromSlash(pf.Path)))
		if err != nil || st.Mode().Perm() != os.FileMode(pf.Mode) {
			t.Errorf("%s: mode on disk %v, preview %o", pf.Path, st.Mode().Perm(), pf.Mode)
		}
	}
	// Nothing else landed.
	n := 0
	filepath.WalkDir(target, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	if n != len(pv.Files) {
		t.Fatalf("%d files on disk, preview named %d — the preview is not the truth", n, len(pv.Files))
	}
}

func TestWriteRefusesWhatAppearedAfterThePlan(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "proj")
	files, err := scaffold.Render(oneFile("late.txt", "x", 0o644), giVars())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scaffold.Plan(target, files); err != nil {
		t.Fatal(err)
	}
	// Someone writes the path between the plan and the write (the plan
	// itself would now refuse; Write's own second look must say so too —
	// O_EXCL is the last wall even against this very function racing
	// itself).
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "late.txt"), []byte("theirs"), 0o644); err != nil {
		t.Fatal(err)
	}
	var led scaffold.Ledger
	if err := scaffold.Write(target, files, &led); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("the write took over an existing file: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(target, "late.txt"))
	if string(b) != "theirs" {
		t.Fatal("their bytes were replaced")
	}
	// The symlink planted after the plan is refused too (the swap hook).
	if err := os.Remove(filepath.Join(target, "late.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, filepath.Join(target, "sub")); err != nil {
		t.Fatal(err)
	}
	swapped, err := scaffold.Render(oneFile("sub/pwned", "x", 0o644), giVars())
	if err != nil {
		t.Fatal(err)
	}
	if err := scaffold.Write(target, swapped, &led); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("planted symlink written through: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "pwned")); !os.IsNotExist(err) {
		t.Fatal("the write escaped through the planted symlink")
	}
}

func TestLedgerCleanupTakesBackOnlyItsOwn(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "proj")
	keep := filepath.Join(target, "keep")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keep, "pre-existing.txt"), []byte("was here"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpl := scaffold.Template{Slug: "t", Files: []scaffold.TemplateFile{
		{Path: "keep/new.txt", Mode: 0o644, Content: "n"},
		{Path: "deep/nested/deeper.txt", Mode: 0o644, Content: "d"},
	}}
	files, err := scaffold.Render(tmpl, giVars())
	if err != nil {
		t.Fatal(err)
	}
	var led scaffold.Ledger
	if err := scaffold.Write(target, files, &led); err != nil {
		t.Fatal(err)
	}
	if err := scaffold.Cleanup(&led); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(keep, "pre-existing.txt")); err != nil {
		t.Fatal("cleanup took a file it did not write")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("cleanup took a pre-existing directory")
	}
	if _, err := os.Stat(filepath.Join(keep, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("its own file survived cleanup")
	}
	if _, err := os.Stat(filepath.Join(target, "deep")); !os.IsNotExist(err) {
		t.Fatal("a directory it created survived cleanup")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("cleanup took the pre-existing (empty) target it did not create")
	}
}

func TestTargetGuardMatrix(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	state := filepath.Join(base, "state")
	ws := filepath.Join(base, "ws")
	comp := filepath.Join(ws, "existing-comp")
	for _, d := range []string{home, state, ws, comp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	known := scaffold.TargetFacts{ComponentRoots: []string{comp}, Home: home, StateDir: state, WorkspaceRoot: ws}
	good := []string{
		filepath.Join(ws, "brand-new"), // absent, under the workspace root
		filepath.Join(ws, "empty-dir"), // present and empty
	}
	if err := os.Mkdir(filepath.Join(ws, "empty-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tgt := range good {
		if err := scaffold.CheckTarget(tgt, known); err != nil {
			t.Errorf("%s refused: %v", tgt, err)
		}
	}
	bad := map[string]string{
		"relative":   "proj",
		"dotted":     filepath.Join(ws, "..", "..", "escape"),
		"home":       home,
		"state":      filepath.Join(state, "sub"),
		"same-root":  comp,
		"in-root":    filepath.Join(comp, "under"),
		"over-root":  ws, // would CONTAIN the registered root
		"outside-ws": filepath.Join(base, "elsewhere"),
	}
	notThere := filepath.Join(home, "newproj")
	if err := os.WriteFile(filepath.Join(ws, "a-file"), []byte("f"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad["is-a-file"] = filepath.Join(ws, "a-file")
	nonEmpty := filepath.Join(ws, "scratch")
	if err := os.Mkdir(nonEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "left"), []byte("l"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad["non-empty"] = nonEmpty
	link := filepath.Join(base, "link-to-ws")
	if err := os.Symlink(ws, link); err != nil {
		t.Fatal(err)
	}
	bad["symlinked-root"] = filepath.Join(link, "existing-comp", "kid") // via symlink: still inside the registered root
	for name, tgt := range bad {
		if err := scaffold.CheckTarget(tgt, known); err == nil {
			t.Errorf("%s: target %q accepted", name, tgt)
		} else if name == "outside-ws" && !strings.Contains(err.Error(), "workspaceRoot") {
			t.Errorf("outside-ws names the wrong law: %v", err)
		}
	}
	// The absent-under-home target is about the parent law, not the HOME
	// identity (only HOME ITSELF is the identity refusal).
	if err := scaffold.CheckTarget(notThere, scaffold.TargetFacts{Home: home}); err != nil {
		t.Logf("absent dir under home: %v", err) // allowed — it is not home itself
	}
}

func TestWriteCreatesAnAbsentTarget(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "brand-new")
	if err := scaffold.CheckTarget(target, scaffold.TargetFacts{Home: base + "/home"}); err != nil {
		t.Fatal(err)
	}
	files, err := scaffold.Render(oneFile("README.md", "hi\n", 0o644), giVars())
	if err != nil {
		t.Fatal(err)
	}
	var led scaffold.Ledger
	if err := scaffold.Write(target, files, &led); err != nil {
		t.Fatal(err)
	}
	if len(led.Dirs) != 1 || led.Dirs[0] != target {
		t.Fatalf("the created target is not in the ledger: %+v", led)
	}
	if err := scaffold.Cleanup(&led); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("cleanup left a directory it created")
	}
}

func TestShippedTemplatesRenderWriteAndPassTheLaw(t *testing.T) {
	sets := scaffold.ShippedTemplates()
	if len(sets) != 3 {
		t.Fatalf("shipped sets = %d, want 3", len(sets))
	}
	for _, set := range sets {
		tmpl := scaffold.Template{Slug: set.Slug}
		tmpl.Files = set.Files
		files, err := scaffold.Render(tmpl, giVars())
		if err != nil {
			t.Fatalf("%s renders: %v", set.Slug, err)
		}
		target := filepath.Join(t.TempDir(), set.Slug)
		if err := scaffold.CheckTarget(target, scaffold.TargetFacts{Home: t.TempDir() + "/home"}); err != nil {
			t.Fatalf("%s target: %v", set.Slug, err)
		}
		var led scaffold.Ledger
		if err := scaffold.Write(target, files, &led); err != nil {
			t.Fatalf("%s write: %v", set.Slug, err)
		}
		rd, err := os.ReadFile(filepath.Join(target, "README.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(rd), "Acme Store") {
			t.Errorf("%s: README does not name the project", set.Slug)
		}
	}
}

// TestCheckTargetRefusesSymlinkedMiddleSegment is the S61 T12 walk finding:
// a symlink in the MIDDLE of a target path is invisible to a
// last-component Lstat — path resolution swallows it — and the T12 walk
// measured a target landing THROUGH the link with rc=0. resolveGround now
// walks every edge of the given path; no segment may be a link.
func TestCheckTargetRefusesSymlinkedMiddleSegment(t *testing.T) {
	realDir := t.TempDir()
	outer := t.TempDir()
	link := filepath.Join(outer, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := scaffold.CheckTarget(filepath.Join(link, "proj", "alpha"), scaffold.TargetFacts{}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("a path through a symlinked parent was accepted: %v", err)
	}
}

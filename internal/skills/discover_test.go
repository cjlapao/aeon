package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// S65 T1/T2 — the parser, the validator and discovery, tested against the
// shapes the real ecosystem ships (the seven skills under ~/.agents/skills
// on this box are the corpus these fixtures are cut from) plus the shapes
// only a spec reading produces.

func writeSkill(t *testing.T, root, folder, content string) string {
	t.Helper()
	dir := filepath.Join(root, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeText(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const goodSkill = `---
name: caveman
description: >
  Terse caveman voice: answer first, fluff gone, every technical fact kept.
  Use for /caveman, "caveman mode", "talk like caveman", "be brief".
---

# caveman

Respond terse like smart caveman. All technical substance stay. Only fluff die.
`

func TestSplitFrontmatterShapes(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantFM   bool
		wantProb string
	}{
		{"plain", "---\nname: a\ndescription: b\n---\nbody\n", true, ""},
		{"dots close", "---\nname: a\n...\nbody", true, ""},
		{"leading blanks tolerated", "\n\n---\nname: a\n---\nbody", true, ""},
		{"never closes", "---\nname: a\nbody without end", true, "never closes"},
		{"body first means no frontmatter", "# heading\n---\nname: a\n---\n", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sp := splitFrontmatter([]byte(c.in))
			if sp.Present != c.wantFM {
				t.Fatalf("Present = %v, want %v", sp.Present, c.wantFM)
			}
			if c.wantProb == "" && sp.Problem != "" {
				t.Fatalf("unexpected Problem: %s", sp.Problem)
			}
			if c.wantProb != "" && !strings.Contains(sp.Problem, c.wantProb) {
				t.Fatalf("Problem = %q, want it to contain %q", sp.Problem, c.wantProb)
			}
		})
	}
}

func TestSplitToleratesBOMAndCRLF(t *testing.T) {
	sp := splitFrontmatter([]byte("\ufeff---\r\nname: a\r\ndescription: b\r\n---\r\nbody\r\n"))
	if !sp.Present || sp.Problem != "" {
		t.Fatalf("BOM/CRLF file must split cleanly: %+v", sp)
	}
	fm := ParseFrontmatter(sp.FM)
	if fm.Name != "a" || fm.Description != "b" {
		t.Fatalf("decoded %+v", fm)
	}
}

func TestParseKnownFieldsRealShapes(t *testing.T) {
	fm := ParseFrontmatter("name: microsoft-foundry\ndescription: \"Deploy, evaluate, and manage Foundry agents.\"\nlicense: MIT\nmetadata:\n  author: Microsoft\n  version: \"1.0.7\"\n")
	if fm.Name != "microsoft-foundry" || fm.License != "MIT" {
		t.Fatalf("known fields mis-decoded: %+v", fm)
	}
	if fm.Metadata["author"] != "Microsoft" || fm.Metadata["version"] != "1.0.7" {
		t.Fatalf("metadata: %+v", fm.Metadata)
	}
	if fm.ParseError != "" {
		t.Fatalf("clean YAML reported an error: %s", fm.ParseError)
	}
}

func TestParseLenientWhereReadersAreLenient(t *testing.T) {
	// Unknown top-level keys (Claude extensions) are kept, not refused.
	fm := ParseFrontmatter("name: a\ndescription: b\ndisable-model-invocation: true\nargument-hint: \"[branch]\"\n")
	if fm.Unknown["disable-model-invocation"] != "true" {
		t.Fatalf("unknown key dropped: %+v", fm.Unknown)
	}
	if fm.ParseError != "" {
		t.Fatalf("unknown keys must never fail the parse: %s", fm.ParseError)
	}

	// A number under metadata is stringified (spec says strings; readers
	// accept the YAML author shipped), and a map is kept for display with
	// a recorded note rather than dropped.
	fm = ParseFrontmatter("name: a\ndescription: b\nmetadata:\n  version: 1.0\n  nested:\n    k: v\n")
	if fm.Metadata["version"] != "1" {
		t.Fatalf("numeric metadata version: %q", fm.Metadata["version"])
	}
	if !strings.Contains(fm.Metadata["nested"], "k=v") {
		t.Fatalf("nested metadata not preserved for display: %q", fm.Metadata["nested"])
	}
	if len(fm.Notes) == 0 {
		t.Fatal("non-string metadata must record a note")
	}

	// allowed-tools ships as a list sometimes; it joins.
	fm = ParseFrontmatter("name: a\ndescription: b\nallowed-tools:\n  - Bash(git:*)\n  - Read\n")
	if fm.AllowedTools != "Bash(git:*) Read" {
		t.Fatalf("list allowed-tools: %q", fm.AllowedTools)
	}
}

func TestParseBadYAMLRecordsTheDecoderSentence(t *testing.T) {
	fm := ParseFrontmatter("name: a\n  bad indent here: [\n")
	if fm.ParseError == "" {
		t.Fatal("bad YAML must carry the decoder's sentence")
	}
}

func TestValidateSpecRules(t *testing.T) {
	fm := func(kv ...string) Frontmatter {
		f := Frontmatter{Present: true, Metadata: map[string]string{}, Unknown: map[string]string{}}
		for i := 0; i < len(kv); i += 2 {
			switch kv[i] {
			case "name":
				f.Name = kv[i+1]
			case "description":
				f.Description = kv[i+1]
			case "compatibility":
				f.Compatibility = kv[i+1]
			}
		}
		return f
	}
	body := "do the thing"

	checks := []struct {
		name     string
		dir      string
		fm       Frontmatter
		wantErr  string
		wantWarn bool
	}{
		{"clean", "a", fm("name", "a", "description", "Does a real thing for real users."), "", false},
		{"name missing", "a", fm("description", "Does a real thing for real users."), "no name", false},
		{"name-folder disagree", "a", fm("name", "b", "description", "Does a real thing for real users."), "but the folder is", false},
		{"name upper", "A", fm("name", "A", "description", "Does a real thing for real users."), "only a-z", false},
		{"name hyphens", "a--b", fm("name", "a--b", "description", "Does a real thing for real users."), "consecutive hyphens", false},
		{"description missing", "a", fm("name", "a"), "no description", false},
		{"description thin", "a", fm("name", "a", "description", "Helps"), "", true},
		{"compatibility long", "a", fm("name", "a", "description", "Does a real thing for real users.", "compatibility", strings.Repeat("x", 501)), "caps it at 500", false},
		{"empty body is a warning", "a", fm("name", "a", "description", "Does a real thing for real users."), "", true},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			bodyIn := body
			if c.name == "empty body is a warning" {
				bodyIn = ""
			}
			issues := ValidateSkill(c.dir, c.fm, bodyIn, 1)
			status := StatusFor(issues)
			if c.wantErr != "" {
				if status != StatusInvalid {
					t.Fatalf("status = %s, want invalid (%v)", status, issues)
				}
				if !containsIssue(issues, c.wantErr) {
					t.Fatalf("no issue names %q: %v", c.wantErr, issues)
				}
				return
			}
			if status == StatusInvalid {
				t.Fatalf("unexpected invalid: %v", issues)
			}
			if c.wantWarn && status != StatusWarn {
				t.Fatalf("status = %s, want warn (%v)", status, issues)
			}
			if !c.wantWarn && status != StatusValid {
				t.Fatalf("status = %s, want valid (%v)", status, issues)
			}
		})
	}
}

// containsIssue finds the sentence naming the fault, so an assertion can
// demand it rather than merely demanding "some issue".
func containsIssue(issues []Issue, want string) bool {
	for _, is := range issues {
		if strings.Contains(is.Detail, want) {
			return true
		}
	}
	return false
}

func TestDiscoverRealShapes(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "caveman", goodSkill)
	writeSkill(t, root, "pdf", `---
name: pdf
description: Extracts text and tables from PDF files and merges them. Use when handling PDF documents.
---
Use the script:
`)
	writeText(t, filepath.Join(root, "pdf", "scripts", "extract.py"), "print('x')")
	writeText(t, filepath.Join(root, "pdf", "references", "FORMS.md"), "# forms")
	writeText(t, filepath.Join(root, "pdf", "LICENSE.txt"), "MIT")
	// A nested SKILL.md (the microsoft-foundry shape) is a RESOURCE of
	// `pdf`, never a second skill.
	writeText(t, filepath.Join(root, "pdf", "references", "inner", "SKILL.md"), "---\nname: inner\ndescription: nested\n---\n")
	// Not skills: dot dir, underscore dir, plain folder, folder without SKILL.md.
	writeSkill(t, root, ".trash-skill", goodSkill)
	writeText(t, filepath.Join(root, "_curated", "whatever", "SKILL.md"), "---\nname: whatever\ndescription: x\n---\n")
	if err := os.MkdirAll(filepath.Join(root, "justfiles"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Discover([]Root{{SourceUser, root}}, DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 2 {
		var got []string
		for _, s := range res.Skills {
			got = append(got, s.Name+":"+string(s.Status))
		}
		t.Fatalf("skills = %v, want exactly caveman and pdf", got)
	}
	var pdf *Skill
	for _, s := range res.Skills {
		if s.Name == "pdf" {
			pdf = s
		}
	}
	if pdf == nil || pdf.Status != StatusValid {
		t.Fatalf("pdf view: %+v", pdf)
	}
	if pdf.Files.Scripts != 1 || pdf.Files.References != 2 || pdf.Files.Other != 1 {
		t.Fatalf("manifest wrong: %+v (nested reference file must count, folder must not duplicate as a skill)", pdf.Files)
	}
	if res.Summary.Valid != 2 {
		t.Fatalf("summary: %+v", res.Summary)
	}
	if res.UserRoot != root {
		t.Fatalf("root naming: %+v", res)
	}
}

func TestDiscoverBrokenSkillStaysVisible(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "nameless", "---\ndescription: has a description but no name at all.\n---\nbody\n")
	writeSkill(t, root, "caveman", goodSkill)

	res, err := Discover([]Root{{SourceUser, root}}, DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 2 {
		t.Fatalf("the broken skill must still be listed: %v", res.Skills)
	}
	for _, s := range res.Skills {
		if s.Name == "nameless" {
			if s.Status != StatusInvalid {
				t.Fatalf("status = %s, want invalid", s.Status)
			}
			if !containsIssue(s.Issues, "no name") {
				t.Fatalf("issues must name the fault: %v", s.Issues)
			}
			return
		}
	}
	t.Fatal("nameless missing")
}

func TestDiscoverPrecedenceAndShadowing(t *testing.T) {
	user, managed := t.TempDir(), t.TempDir()
	writeSkill(t, user, "pdf", "---\nname: pdf\ndescription: The user's own PDF skill with plenty of description text.\n---\nu\n")
	writeSkill(t, managed, "pdf", "---\nname: pdf\ndescription: The installed copy with plenty of description text too.\n---\nm\n")
	writeSkill(t, managed, "only-managed", "---\nname: only-managed\ndescription: Only in the managed root, fine description here.\n---\nm\n")

	res, err := Discover([]Root{{SourceUser, user}, {SourceManaged, managed}}, DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 3 {
		t.Fatalf("both copies of a name stay listed: %d", len(res.Skills))
	}
	var winner, loser *Skill
	for _, s := range res.Skills {
		if s.Name == "pdf" {
			if s.Shadowed {
				loser = s
			} else {
				winner = s
			}
		}
	}
	if winner == nil || winner.Source != SourceUser {
		t.Fatalf("the user root must win: %+v", winner)
	}
	if loser == nil || loser.Source != SourceManaged || loser.ShadowedBy != winner.Path {
		t.Fatalf("the loser must name its winner: %+v", loser)
	}
	if res.Summary.Shadowed != 1 {
		t.Fatalf("summary: %+v", res.Summary)
	}
	// The winner sorts before the loser.
	if res.Skills[0].Name != "only-managed" && res.Skills[0].Shadowed {
		t.Fatalf("ordering: shadowed entries sort last per name: %+v", res.Skills[0])
	}
}

func TestDiscoverMissingRootIsTheEmptyCase(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	res, err := Discover([]Root{{SourceUser, missing}, {SourceManaged, t.TempDir()}}, DefaultLimits(), nil)
	if err != nil {
		t.Fatalf("a missing root is the first-run state, not a fault: %v", err)
	}
	if len(res.Skills) != 0 || res.UserRoot != missing {
		t.Fatalf("%+v", res)
	}
}

func TestDiscoverFollowsSymlinkedSkillFolder(t *testing.T) {
	if os.Geteuid() == -1 {
		t.Skip("symlinks are POSIX")
	}
	root := t.TempDir()
	target := t.TempDir()
	writeText(t, filepath.Join(target, "SKILL.md"), "---\nname: linked\ndescription: A skill reached through a folder symlink, both readers do this.\n---\nbody\n")
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks not permitted: %v", err)
	}
	res, err := Discover([]Root{{SourceUser, root}}, DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 1 || res.Skills[0].Status != StatusValid {
		t.Fatalf("symlinked skill folder must load: %+v", res.Skills)
	}
}

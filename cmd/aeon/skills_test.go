package main

// skills_test.go — S65 T6. The CLI is a pure client of the socket's five
// skills.* methods (P34); these tests pin that the secondary client adds NO
// second truth: the printers speak the wire verbatim (the status word the
// validator gave, the issues it listed, the truncated flag it set), the
// verbs refuse malformed asks before dialing, and the file opens no store
// and runs no git itself. Same canned-struct lane as settings_test.go: the
// socket is stood in, the behaviour asserted.

import (
	"go/parser"
	"go/token"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/skills"
)

func TestSkillsNoVerbNamesTheVerbs(t *testing.T) {
	err := cmdSkills(io.Discard, nil)
	if err == nil {
		t.Fatal("no verb returned nil — it must name the verbs and point at help")
	}
	for _, v := range []string{"list", "get", "validate", "install", "remove"} {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("no-verb error %q does not name %q", err.Error(), v)
		}
	}
	if err := cmdSkills(io.Discard, []string{"frob"}); err == nil || !strings.Contains(err.Error(), "install") {
		t.Fatalf("unknown verb must name the real verbs, got %v", err)
	}
}

func TestSkillsListPrinterSpeaksTheWire(t *testing.T) {
	var b strings.Builder
	res := &skills.ScanResult{
		UserRoot:    "/home/dev/.agents/skills",
		ManagedRoot: "/home/dev/.local/share/aeon/skills",
		Summary:     skills.Summary{Valid: 1, Warn: 1, Invalid: 1, Shadowed: 1},
		Skills: []*skills.Skill{
			{Name: "pdf", Status: skills.StatusValid, Source: skills.SourceUser, Description: "Extract text."},
			{Name: "thin", Status: skills.StatusWarn, Source: skills.SourceManaged,
				Issues: []skills.Issue{{Field: "description", Detail: "thin", Severity: skills.SeverityWarning}}},
			{Name: "broken", Status: skills.StatusInvalid, Source: skills.SourceManaged,
				Issues: []skills.Issue{{Field: "name", Detail: "no name", Severity: skills.SeverityError}}},
			{Name: "caveman", Status: skills.StatusValid, Source: skills.SourceManaged, Shadowed: true, ShadowedBy: "/home/dev/.agents/skills/caveman"},
		},
	}
	if err := renderSkillsList(&b, res); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"/home/dev/.agents/skills", "/home/dev/.local/share/aeon/skills",
		"1 valid", "1 with warnings", "1 invalid", "1 shadowed",
		"pdf", "Extract text.", "warn", "FAIL", "thin", "broken", "no name",
		"shadowed by /home/dev/.agents/skills/caveman",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
}

func TestSkillsListEmptyIsHonest(t *testing.T) {
	var b strings.Builder
	res := &skills.ScanResult{UserRoot: "/u", ManagedRoot: "/m"}
	if err := renderSkillsList(&b, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "empty") {
		t.Errorf("an empty listing must say so, not go silent:\n%s", b.String())
	}
}

func TestSkillsGetPrinterKeepsTruncationHonest(t *testing.T) {
	var b strings.Builder
	full := &skills.GetResult{
		Skill: &skills.Skill{Name: "pdf", Status: skills.StatusValid, BodyBytes: 10, BodyLines: 2},
		Body:  "hello",
	}
	if err := renderSkillGet(&b, full); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "truncated") {
		t.Errorf("a complete body must not claim truncation:\n%s", b.String())
	}
	b.Reset()
	cut := &skills.GetResult{
		Skill:     &skills.Skill{Name: "pdf", Status: skills.StatusValid, BodyBytes: 99999, BodyLines: 900},
		Body:      "hello",
		Truncated: true,
	}
	if err := renderSkillGet(&b, cut); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "truncated") || !strings.Contains(out, "the file on disk is complete") {
		t.Errorf("a cut body must say so AND say the disk holds the whole file:\n%s", out)
	}

	// Unknown fields print as kept-and-ignored — compatibility honesty.
	b.Reset()
	unk := &skills.GetResult{Skill: &skills.Skill{
		Name: "claude", Status: skills.StatusValid,
		Metadata: map[string]string{"author": "me"},
		Unknown:  map[string]string{"hooks": "on"},
	}}
	if err := renderSkillGet(&b, unk); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "kept and ignored") || !strings.Contains(b.String(), "author = me") {
		t.Errorf("unknown/metadata fields must be shown as what they are:\n%s", b.String())
	}
}

func TestSkillsInstallPrinterNamesThePlacement(t *testing.T) {
	var b strings.Builder
	res := &skills.InstallResult{
		Skill:    &skills.Skill{Name: "pdf", Status: skills.StatusValid, Path: "/m/pdf", Source: skills.SourceManaged},
		Replaced: true,
	}
	if err := renderInstall(&b, res); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"installed pdf", "/m/pdf", "replaced", "nothing in it was executed"} {
		if !strings.Contains(out, want) {
			t.Errorf("install output missing %q:\n%s", want, out)
		}
	}
}

func TestSkillsRemovePrinterNamesTheReveal(t *testing.T) {
	var b strings.Builder
	if err := renderRemove(&b, &skills.RemoveResult{Name: "pdf", Path: "/m/pdf", Revealed: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "removed pdf") || !strings.Contains(b.String(), "listed again") {
		t.Errorf("remove must name the reveal:\n%s", b.String())
	}
	b.Reset()
	if err := renderRemove(&b, &skills.RemoveResult{Name: "pdf", Path: "/m/pdf"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "shadowed") {
		t.Errorf("a remove that revealed nothing must not mention shadowing:\n%s", b.String())
	}
}

func TestSkillsValidatePrinterVerdictsPerSkill(t *testing.T) {
	var b strings.Builder
	res := &protocol.SkillsValidateResult{
		ScannedAt: "2026-10-30T18:05:00Z",
		Summary:   skills.Summary{Valid: 1, Invalid: 1},
		Skills: []*skills.Skill{
			{Name: "pdf", Status: skills.StatusValid},
			{Name: "broken", Status: skills.StatusInvalid,
				Issues: []skills.Issue{{Field: "name", Detail: "no name", Severity: skills.SeverityError}}},
		},
	}
	if err := renderValidate(&b, "", res); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "re-read the disk at") || !strings.Contains(out, "INVALID") || !strings.Contains(out, "no name") {
		t.Errorf("validate must speak the forced scan and every issue:\n%s", out)
	}
	if err := renderValidate(&b, "pdf", &protocol.SkillsValidateResult{}); err != nil {
		t.Fatal(err)
	}
}

func TestSkillsMalformedAsksRefuseBeforeDialing(t *testing.T) {
	// These refuse on the argv itself, so no daemon is needed and none is
	// dialed — the refusal arrives whatever the socket answers.
	cases := []struct {
		verb []string
		want string
	}{
		{[]string{"get"}, "one name"},
		{[]string{"remove"}, "one name"},
		{[]string{"install"}, "source"},
	}
	for _, c := range cases {
		if err := cmdSkills(io.Discard, c.verb); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("skills %v: want a refusal naming %q, got %v", c.verb, c.want, err)
		}
	}
	if err := cmdSkills(io.Discard, []string{"validate", "a", "b"}); err == nil {
		t.Error("validate with two names must refuse")
	}
}

// TestSkillsClientOpensNoStore — the P34 shape proof (the settings.go twin):
// skills.go dials and calls, and opens no store, no SQL, no git of its own.
// A second disk-walking door is the failure this slice forbids.
func TestSkillsSeparateKeepsTrailingFlags(t *testing.T) {
	// `install <src> --force` and `get <name> --json` are the orders people
	// type — flag AFTER the thing it modifies. flag.Parse stops at the first
	// positional, so the verb pre-splits or silently loses the flag.
	pos, flags, err := skillsSeparate([]string{"/tmp/src", "--force", "--ref", "main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pos) != 1 || pos[0] != "/tmp/src" {
		t.Fatalf("positional: %#v", pos)
	}
	if strings.Join(flags, " ") != "--force --ref main" {
		t.Fatalf("flags: %#v", flags)
	}
	if _, _, err := skillsSeparate([]string{"a", "--ref"}); err == nil {
		t.Fatal("--ref without a value must refuse, not eat the next token")
	}
	pos, flags, err = skillsSeparate([]string{"--", "--weird-folder-name"})
	if err != nil || len(pos) != 1 || pos[0] != "--weird-folder-name" || len(flags) != 0 {
		t.Fatalf("after --: %#v %#v %v", pos, flags, err)
	}
}

func TestSkillsGetRefusesNameAndPathTogether(t *testing.T) {
	// Before any dial: the path already names THE copy; a name beside it is
	// two intentions about one address.
	err := skillsGet(io.Discard, []string{"pdf", "--path", "/x/y/pdf"})
	if err == nil || !strings.Contains(err.Error(), "never both") {
		t.Fatalf("name + --path must refuse before dialing, got %v", err)
	}
}

func TestSkillsClientOpensNoStore(t *testing.T) {
	src, err := os.ReadFile("skills.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "skills.go", src, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		for _, banned := range []string{"os/exec", "database/sql", "internal/store", "net/http", "gorm.io"} {
			if path == banned {
				t.Errorf("skills.go imports %s — the CLI is a socket client, not a second door (P34)", path)
			}
		}
	}
	text := string(src)
	for _, banned := range []string{"exec.Command", "git ", "filepath.Walk"} {
		if strings.Contains(text, banned) {
			t.Errorf("skills.go contains %q — fetching and walking are the DAEMON's job (P34)", banned)
		}
	}
}

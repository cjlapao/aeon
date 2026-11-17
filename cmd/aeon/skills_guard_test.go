package main

// skills_guard_test.go — S65 T4. The guard that makes the slice's surface
// claim mechanical rather than reviewed, in the mould of
// settings_guard_test.go's TestNoSettingsWriteReachesTheNarrowSurface:
// the whole skills family — the reads AND the two writes — stays on the
// wide socket and never reaches the narrow (agent / HTTP / MCP-stdio)
// projection. Install is a fetch-and-write door: an agent holding it could
// plant a SKILL.md whose body the next operator's agent then reads as
// instruction. Even the reads stay off: the narrow surface's Actions()
// allow-list is the single decision about what is projected, and S65 adds
// no entry to it (S44 CS9's precedent, carried whole).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/skills"
)

// guardSkillsCatalog is the non-nil seam the guard needs to see the five
// skills.* methods REGISTERED. RegisterSkills only inspects nil-ness — it
// never calls these — so the stand-in answers nothing, and the guard never
// touches a real root (no HOME read, no disk walk; the store rule applies
// to roots too).
type guardSkillsCatalog struct{}

var _ protocol.Skills = guardSkillsCatalog{} // compile-time: the fake satisfies the seam

func (guardSkillsCatalog) Scan(bool) (*skills.ScanResult, error) { return nil, nil }
func (guardSkillsCatalog) Get(string) (*skills.GetResult, error) {
	return nil, nil
}
func (guardSkillsCatalog) GetUnder(string) (*skills.GetResult, error) { return nil, nil }
func (guardSkillsCatalog) Install(context.Context, skills.InstallRequest) (*skills.InstallResult, error) {
	return nil, nil
}
func (guardSkillsCatalog) Remove(string) (*skills.RemoveResult, error) { return nil, nil }

// newSkillsTable builds the table the way a daemon with a catalog builds it
// (daemon.go): NewTable, then assign the seam, then RegisterSkills.
func newSkillsTable(t *testing.T) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.Skills = guardSkillsCatalog{}
	if !tbl.RegisterSkills() {
		t.Fatal("RegisterSkills refused to register over a non-nil seam — the app would have no skills door at all")
	}
	return tbl
}

// TestNoSkillsMethodReachesTheNarrowSurface — positive half first (the app
// has its door: all five ARE registered), then the negatives (none is
// projected).
func TestNoSkillsMethodReachesTheNarrowSurface(t *testing.T) {
	tbl := newSkillsTable(t)

	// ── Positive: the app has its door (P34).
	have := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		have[m] = true
	}
	for _, m := range []protocol.MethodName{
		protocol.MethodSkillsList, protocol.MethodSkillsGet, protocol.MethodSkillsValidate,
		protocol.MethodSkillsInstall, protocol.MethodSkillsRemove,
	} {
		if !have[m] {
			t.Fatalf("%s must be registered — the bench page has one door onto the catalog (P34)", m)
		}
	}
	// The writes are real product methods, not diagnostics.
	for _, m := range []protocol.MethodName{protocol.MethodSkillsInstall, protocol.MethodSkillsRemove} {
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not in the table — the positive half failed", m)
		}
		if got.Debug {
			t.Fatalf("%s is marked Debug — a skills write is a product method on the wide socket, not a diagnostic", m)
		}
		if !got.RequiresInitialize {
			t.Fatalf("%s does not RequireInitialize — a write may not precede the connection's negotiation", m)
		}
	}

	// ── Negative 1 — the narrow allow-list projects NO skills.* method,
	// read or write.
	if offenders := narrowProjectingSkills(narrow.Actions()); len(offenders) != 0 {
		t.Errorf("the narrow surface projects skills.* through %s — an agent holding install can plant the instructions the next agent reads, and even the reads are not on the allow-list's word to carry. Add one only through a slice that argues it.", strings.Join(offenders, ", "))
	}

	// ── Negative 2 — by text: no non-test file under internal/narrow or
	// internal/agentsvc names any of the five.
	for _, dir := range []string{"internal/narrow", "internal/agentsvc"} {
		walkNoSkillsLiteral(t, filepath.Join("..", "..", dir))
	}

	// ── Negative 3 — the agent-services enumeration carries no skills name.
	for _, m := range protocol.AgentServiceMethods() {
		if strings.HasPrefix(string(m.Name), "skills.") {
			t.Fatalf("%s is an agent-service method — the catalog is not the agent surface's to carry", m.Name)
		}
	}
}

// TestSkillsNarrowGuardCanFail — the meta-test: a guard that cannot fail is
// a green light, not a guard.
func TestSkillsNarrowGuardCanFail(t *testing.T) {
	poisoned := []narrow.Action{
		{Name: "aeon_skills_install", SocketMethod: protocol.MethodSkillsInstall, Kind: narrow.KindRead},
	}
	if got := narrowProjectingSkills(poisoned); len(got) == 0 {
		t.Fatal("a narrow action projecting skills.install was not flagged — the guard cannot fail, so it is not a guard")
	}
	clean := []narrow.Action{
		{Name: "aeon_loop_list", SocketMethod: protocol.MethodLoopList, Kind: narrow.KindRead},
	}
	if got := narrowProjectingSkills(clean); len(got) != 0 {
		t.Errorf("a clean action was flagged as projecting skills.* — the predicate fires on nothing: %v", got)
	}
}

func narrowProjectingSkills(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		if strings.HasPrefix(string(a.SocketMethod), "skills.") {
			bad = append(bad, a.Name)
		}
	}
	return bad
}

// walkNoSkillsLiteral fails any non-test .go file under narrow/ or
// agentsvc/ that names a skills method — the wire literal or the const.
func walkNoSkillsLiteral(t *testing.T, root string) {
	t.Helper()
	forbidden := []string{
		"skills.list", "skills.get", "skills.validate", "skills.install", "skills.remove",
		"MethodSkillsList", "MethodSkillsGet", "MethodSkillsValidate", "MethodSkillsInstall", "MethodSkillsRemove",
	}
	var walked int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable dirs hold no Go code we own
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		walked++
		text := string(b)
		for _, f := range forbidden {
			if strings.Contains(text, f) {
				t.Errorf("%s names %q — the skills catalog must not reach the narrow/agent surface (S65)", path, f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked == 0 {
		t.Errorf("the walk of %s saw no Go files — a guard that walks an empty tree passes vacuously", root)
	}
}

// agent_page_test.go pins the store half of the paging convention (PG1–PG4)
// against a real database, and it is the FIRST adopter's proof: every one of
// these tests would pass identically for the ledger read in T5, which is the
// point of a convention.
//
// The rows are laid by direct SQL rather than by the management doors, on
// purpose: this file is about a READ. The write doors move in T3 (the
// un-audited writers get deleted); a window test that reached them would break
// for a reason that has nothing to do with a window.

package store

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/paging"
)

// insertAgentRow lays one agent row with the fields the window reads. Slug and
// name are the caller's, so a test can name the exact thing it is hunting.
func insertAgentRow(t *testing.T, s *Store, slug, name, description, version string, internal, signed bool, origin, at string) {
	t.Helper()
	bi, bs := 0, 0
	if internal {
		bi = 1
	}
	if signed {
		bs = 1
	}
	if _, err := s.w.Exec(`
		INSERT INTO agent (slug, name, description, version, internal, signed, origin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		slug, name, description, version, bi, bs, origin, at, at); err != nil {
		t.Fatalf("insert agent %q: %v", slug, err)
	}
}

// pageAgent is the call every test below makes, with the bounds the window
// publishes — a test that invented its own bounds would be testing a fiction.
func pageAgent(t *testing.T, s *Store, req paging.Request) ([]AgentRow, int) {
	t.Helper()
	normalised, err := req.Normalise(AgentRowBounds)
	if err != nil {
		t.Fatalf("normalise %#v: %v", req, err)
	}
	rows, total, err := s.AgentPage(context.Background(), normalised)
	if err != nil {
		t.Fatalf("agent page %#v: %v", normalised, err)
	}
	return rows, total
}

// fiveAgents lays five rows whose names are alphabetical on purpose, with
// created/updated stamps that disagree with the name order so a sort test can
// tell the two apart.
func fiveAgents(t *testing.T, s *Store) []string {
	t.Helper()
	specs := []struct {
		slug, name, desc, ver, at string
	}{
		{"alpha", "Alpha", "reads the diff", "1", "2026-10-01T00:00:00Z"},
		{"bravo", "Bravo", "writes the fix", "2", "2026-10-02T00:00:00Z"},
		{"charlie", "Charlie", "reads the spec", "1", "2026-10-03T00:00:00Z"},
		{"delta", "Delta", "reviews the fix", "3", "2026-10-04T00:00:00Z"},
		{"echo", "Echo", "reads the log", "2", "2026-10-05T00:00:00Z"},
	}
	names := make([]string, 0, len(specs))
	for i, sp := range specs {
		insertAgentRow(t, s, sp.slug, sp.name, sp.desc, sp.ver, i == 0, i == 4, "user", sp.at)
		names = append(names, sp.slug)
	}
	return names
}

func slugsOf(rows []AgentRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Slug)
	}
	return out
}

func joinSlugs(rows []AgentRow) string { return strings.Join(slugsOf(rows), ",") }

// PG1/PG3: page 1 and page 2 of a five-row set at two a page are different
// rows, and the total is the whole set both times.
func TestAgentPageWindowsAreTheRowsTheyPromised(t *testing.T) {
	s := openTemp(t)
	fiveAgents(t, s)

	first, total := pageAgent(t, s, paging.Request{Page: 1, PageSize: 2})
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if got := joinSlugs(first); got != "alpha,bravo" {
		t.Errorf("page 1 = %s, want alpha,bravo", got)
	}
	if len(first[0].CreatedAt) == 0 || first[0].Name == "" {
		t.Errorf("a window row is missing the columns the table renders: %+v", first[0])
	}

	second, total2 := pageAgent(t, s, paging.Request{Page: 2, PageSize: 2})
	if total2 != 5 {
		t.Errorf("page 2's total = %d, want the same 5", total2)
	}
	if got := joinSlugs(second); got != "charlie,delta" {
		t.Errorf("page 2 = %s, want charlie,delta", got)
	}

	third, _ := pageAgent(t, s, paging.Request{Page: 3, PageSize: 2})
	if got := joinSlugs(third); got != "echo" {
		t.Errorf("page 3 = %s, want echo", got)
	}
}

// PG3: a page past the end is the empty window AND the honest total — not an
// error, not a redirect to the last page, not a zero total.
func TestAgentPagePastTheEndIsHonestAboutBoth(t *testing.T) {
	s := openTemp(t)
	fiveAgents(t, s)

	rows, total := pageAgent(t, s, paging.Request{Page: 99, PageSize: 20})
	if len(rows) != 0 {
		t.Errorf("a page past the end returned %d rows, want none: %s", len(rows), joinSlugs(rows))
	}
	if total != 5 {
		t.Errorf("total = %d, want the honest 5 (the end of the data is a fact, not a fault)", total)
	}
}

// PG3: an empty store answers zero and zero, and it is not an error.
func TestAgentPageOfNothingIsNotAnError(t *testing.T) {
	s := openTemp(t)
	rows, total := pageAgent(t, s, paging.Request{})
	if len(rows) != 0 || total != 0 {
		t.Errorf("an empty store answered %d rows / total %d, want 0 and 0", len(rows), total)
	}
}

// The nine columns the window promises, and only those: a row that leaked the
// prompt would be a list loading the store's content into the browser, and a
// row missing origin/signed would leave the table unable to draw what it draws.
func TestAgentRowCarriesTheNineColumnsAndNoContent(t *testing.T) {
	s := openTemp(t)
	insertAgentRow(t, s, "leaky", "Leaky", "has content", "1", false, false, "import", "2026-10-06T00:00:00Z")
	if _, err := s.w.Exec(`UPDATE agent SET prompt = 'the whole prompt', skills = '[]', metadata = '{"k":1}', body_hash = 'deadbeef' WHERE slug = 'leaky'`); err != nil {
		t.Fatalf("plant the content: %v", err)
	}
	rows, _ := pageAgent(t, s, paging.Request{})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got := fmt.Sprintf("%+v", rows[0])
	for _, leak := range []string{"the whole prompt", "deadbeef", "\"k\":1"} {
		if strings.Contains(got, leak) {
			t.Errorf("the window leaked %q into the list row: %s", leak, got)
		}
	}
	if rows[0].Origin != "import" || rows[0].Version != "1" || rows[0].Slug != "leaky" {
		t.Errorf("the window row lost a column it promised: %+v", rows[0])
	}
}

// PG1: `q` searches the three columns a person expects a name search to reach,
// and the total counts the FILTER (PG3) — that pair is the whole point.
func TestAgentQuerySearchesThreeColumnsAndTheTotalFollows(t *testing.T) {
	s := openTemp(t)
	fiveAgents(t, s)

	byName, total := pageAgent(t, s, paging.Request{Q: "charlie"})
	if total != 1 || joinSlugs(byName) != "charlie" {
		t.Errorf("q=charlie: %s / total %d, want charlie / 1", joinSlugs(byName), total)
	}
	byDescription, total := pageAgent(t, s, paging.Request{Q: "fix"})
	if total != 2 || joinSlugs(byDescription) != "bravo,delta" {
		t.Errorf("q=fix: %s / total %d, want bravo,delta / 2", joinSlugs(byDescription), total)
	}
	byDescriptionCase, totalCase := pageAgent(t, s, paging.Request{Q: "FIX"})
	if totalCase != 2 || joinSlugs(byDescriptionCase) != "bravo,delta" {
		t.Errorf("q=FIX (upper): %s / total %d — LIKE is case-insensitive for ASCII in sqlite; the window should agree with the case the person typed", joinSlugs(byDescriptionCase), totalCase)
	}
	byVersion, total := pageAgent(t, s, paging.Request{Q: "charlie"})
	if total != 1 || byVersion[0].Description != "reads the spec" {
		t.Errorf("the filtered row is not the row the total counted: %+v", byVersion[0])
	}
}

// A filter that matches nothing answers the empty window and a zero total — the
// two are different facts from an empty store, and the page renders them
// differently, so the daemon must not collapse them.
func TestAgentQueryMatchingNothing(t *testing.T) {
	s := openTemp(t)
	fiveAgents(t, s)
	rows, total := pageAgent(t, s, paging.Request{Q: "no-such-thing"})
	if len(rows) != 0 {
		t.Errorf("rows = %s, want none", joinSlugs(rows))
	}
	if total != 0 {
		t.Errorf("total = %d, want 0 (five rows exist; the FILTER has none)", total)
	}
}

// PG4: LIKE's metacharacters are data. A row whose name contains them is found
// by typing them, and a bare `%` does not match the world.
func TestAgentQueryEscapesLikeMetacharacters(t *testing.T) {
	s := openTemp(t)
	fiveAgents(t, s)
	insertAgentRow(t, s, "percent", `100%_sure\thing`, "the odd one", "1", false, false, "user", "2026-10-06T00:00:00Z")

	found, total := pageAgent(t, s, paging.Request{Q: `100%_sure\thing`})
	if total != 1 || joinSlugs(found) != "percent" {
		t.Errorf("the literal name was not found by typing it: %s / total %d", joinSlugs(found), total)
	}

	part, total := pageAgent(t, s, paging.Request{Q: `%_`})
	if total != 1 || joinSlugs(part) != "percent" {
		t.Errorf("q=%%_ matched %s / total %d — the metacharacters must be data, not operators", joinSlugs(part), total)
	}

	wide, total := pageAgent(t, s, paging.Request{Q: "%"})
	if total != 1 || joinSlugs(wide) != "percent" {
		t.Errorf("a typed %q matched %s / total %d — it means the character, not every row", "%", joinSlugs(wide), total)
	}

	under, total := pageAgent(t, s, paging.Request{Q: "100%_su"})
	if total != 1 || joinSlugs(under) != "percent" {
		t.Errorf("a partial literal match broke: %s / total %d", joinSlugs(under), total)
	}
}

// PG4: every published sort name answers the order it names. `internal` and
// `version` are the two that prove the map is being read — a version sorted as
// TEXT is '1','2','3', and an internal sorted as INTEGER is 0 before 1.
func TestAgentSortNamesAnswerTheirOwnOrders(t *testing.T) {
	s := openTemp(t)
	fiveAgents(t, s)

	// alpha is internal (laid first in fiveAgents), echo signed (laid last).
	cases := []struct {
		sort, dir, want string
	}{
		{"name", "asc", "alpha,bravo,charlie,delta,echo"},
		{"name", "desc", "echo,delta,charlie,bravo,alpha"},
		{"slug", "asc", "alpha,bravo,charlie,delta,echo"},
		{"slug", "desc", "echo,delta,charlie,bravo,alpha"},
		{"version", "asc", "alpha,charlie,bravo,echo,delta"},
		{"version", "desc", "delta,bravo,echo,alpha,charlie"},
		{"createdAt", "asc", "alpha,bravo,charlie,delta,echo"},
		{"createdAt", "desc", "echo,delta,charlie,bravo,alpha"},
		{"updatedAt", "desc", "echo,delta,charlie,bravo,alpha"},
		{"internal", "asc", "bravo,charlie,delta,echo,alpha"},
		{"internal", "desc", "alpha,bravo,charlie,delta,echo"},
		{"signed", "asc", "alpha,bravo,charlie,delta,echo"},
		{"signed", "desc", "echo,alpha,bravo,charlie,delta"},
	}
	for _, c := range cases {
		rows, total := pageAgent(t, s, paging.Request{Sort: c.sort, Dir: c.dir, PageSize: 100})
		if total != 5 {
			t.Errorf("sort %s %s: total %d, want 5", c.sort, c.dir, total)
		}
		if got := joinSlugs(rows); got != c.want {
			t.Errorf("sort %s %s = %s, want %s", c.sort, c.dir, got, c.want)
		}
	}
}

// PG4's tiebreak: two rows sharing a sort value must not shuffle between two
// calls, or a person sees the table move when they turn a page.
func TestAgentSortTiebreakIsStableAcrossCalls(t *testing.T) {
	s := openTemp(t)
	for _, slug := range []string{"zulu", "mike", "alpha", "quebec"} {
		insertAgentRow(t, s, slug, "Same Name", "", "1", false, false, "user", "2026-10-06T00:00:00Z")
	}
	first, _ := pageAgent(t, s, paging.Request{Sort: "name", PageSize: 2, Page: 1})
	second, _ := pageAgent(t, s, paging.Request{Sort: "name", PageSize: 2, Page: 1})
	if joinSlugs(first) != joinSlugs(second) {
		t.Errorf("the same query answered %s then %s — ties must break by slug, or a page turns into a shuffle", joinSlugs(first), joinSlugs(second))
	}
	if got := joinSlugs(first); got != "alpha,mike" {
		t.Errorf("the tiebreak order = %s, want alpha,mike (slug ascending)", got)
	}
}

// PG4: an unknown sort is refused, naming the name, and the table is untouched
// — the refusal happens before any ORDER BY is composed, so the only way this
// test can fail is by the query running.
func TestAgentUnknownSortRefusesAndTheTableSurvives(t *testing.T) {
	s := openTemp(t)
	fiveAgents(t, s)

	_, _, err := s.AgentPage(context.Background(), paging.Request{Page: 1, PageSize: 20, Sort: "prompt", Dir: "asc"})
	if err == nil {
		t.Fatal("sort \"prompt\" resolved — it is not in AgentRowBounds.Sorts and must not reach SQL")
	}
	if !strings.Contains(err.Error(), "prompt") {
		t.Errorf("the refusal does not name the offender: %v", err)
	}

	var alive int
	if err := s.r.QueryRow(`SELECT count(*) FROM agent`).Scan(&alive); err != nil {
		t.Fatalf("count: %v", err)
	}
	if alive != 5 {
		t.Errorf("the agent table holds %d rows after a refused sort, want the 5 it had", alive)
	}

	// A hostile name is refused the same way, and it is the same code path:
	// the map holds no such key, so no string is ever composed into SQL.
	if _, _, err := s.AgentPage(context.Background(), paging.Request{Page: 1, PageSize: 20, Sort: "slug;DROP TABLE agent--", Dir: "asc"}); err == nil {
		t.Fatal("a hostile sort name reached the query builder")
	}
	if err := s.r.QueryRow(`SELECT count(*) FROM agent`).Scan(&alive); err != nil || alive != 5 {
		t.Fatalf("the table did not survive the hostile name (%d rows, err %v)", alive, err)
	}
}

// A direction that is not a direction is refused before the store is asked;
// AgentPage takes an already-normalised request, so it also refuses rather than
// trusting its caller — two doors, one rule.
func TestAgentPageRefusesARawBadDirection(t *testing.T) {
	s := openTemp(t)
	fiveAgents(t, s)
	if _, _, err := s.AgentPage(context.Background(), paging.Request{Page: 1, PageSize: 20, Sort: "name", Dir: "sideways"}); err == nil {
		t.Fatal("dir \"sideways\" reached the ORDER BY")
	}
}

// The bounds are the single source: every name the window publishes resolves,
// and each resolves to a column the `agent` table actually has. This is the
// test that fails on the day somebody adds `"description": "description"` and
// forgets the table, or renames a column in a migration and forgets the map.
func TestAgentRowBoundsDescribeTheTable(t *testing.T) {
	s := openTemp(t)
	fiveAgents(t, s)
	if got := len(AgentRowBounds.Sorts); got != 7 {
		t.Errorf("the agent window publishes %d sorts, want the design's seven", got)
	}
	for name, col := range AgentRowBounds.Sorts {
		if _, _, err := (paging.Request{Sort: name, Dir: "asc"}).Column(AgentRowBounds); err != nil {
			t.Errorf("published sort %q does not resolve: %v", name, err)
			continue
		}
		var probe int
		if err := s.r.QueryRow(fmt.Sprintf(`SELECT count(%s) FROM agent`, col)).Scan(&probe); err != nil {
			t.Errorf("published sort %q maps to column %q, which the agent table does not have: %v", name, col, err)
			continue
		}
		if probe != 5 {
			t.Errorf("column %q counted %d rows, want 5", col, probe)
		}
	}
}

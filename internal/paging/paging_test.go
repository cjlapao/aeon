// paging_test.go drives every branch of the convention without a database in
// sight — which is the point of splitting the package out (PG2). Each test's
// name says which property it pins, so a failure points at the rule and not at
// a function.

package paging

import (
	"strings"
	"testing"
)

func agentBounds() Bounds {
	return Bounds{
		DefaultSize: 20,
		MaxSize:     100,
		DefaultSort: "name",
		Sorts: map[string]string{
			"name":      "name",
			"slug":      "slug",
			"version":   "version",
			"internal":  "internal",
			"signed":    "signed",
			"createdAt": "created_at",
			"updatedAt": "updated_at",
		},
	}
}

// PG1: an absent key takes the default the bounds name, so `{}` is page 1 at
// the default size, in the default order, with no filter.
func TestAbsentKeysTakeTheDefaults(t *testing.T) {
	got, err := Request{}.Normalise(agentBounds())
	if err != nil {
		t.Fatalf("an empty request must not refuse: %v", err)
	}
	if got.Page != 1 {
		t.Errorf("page = %d, want 1", got.Page)
	}
	if got.PageSize != 20 {
		t.Errorf("pageSize = %d, want the default 20", got.PageSize)
	}
	if got.Sort != "name" {
		t.Errorf("sort = %q, want the default \"name\"", got.Sort)
	}
	if got.Dir != "asc" {
		t.Errorf("dir = %q, want \"asc\"", got.Dir)
	}
	if got.Q != "" {
		t.Errorf("q = %q, want the empty string", got.Q)
	}
}

// PG3: an out-of-range size CLAMPS and the clamped value is what comes back —
// never a refusal, never a silent unbounded read.
func TestPageSizeClampsAndNeverRefuses(t *testing.T) {
	b := agentBounds()
	cases := []struct {
		in   int
		want int
		why  string
	}{
		{-1, 20, "a negative size is not a size; the default answers"},
		{0, 20, "zero means absent"},
		{1, 1, "the smallest honest window"},
		{20, 20, "the default is untouched"},
		{100, 100, "the cap itself is allowed"},
		{101, 100, "one past the cap clamps"},
		{500, 100, "a long way past the cap clamps, and the answer says 100"},
		{100000, 100, "no number asks for the whole table"},
	}
	for _, c := range cases {
		got, err := Request{PageSize: c.in}.Normalise(b)
		if err != nil {
			t.Errorf("pageSize %d (%s): refused, but a size clamps — %v", c.in, c.why, err)
			continue
		}
		if got.PageSize != c.want {
			t.Errorf("pageSize %d (%s) = %d, want %d", c.in, c.why, got.PageSize, c.want)
		}
	}
}

// A bounds that contradicts itself (default above its own cap) takes the CAP,
// because an unbounded read is the failure and an unexpected small size is not.
func TestBoundsThatContradictThemselvesClampDown(t *testing.T) {
	got, err := Request{PageSize: 500}.Normalise(Bounds{DefaultSize: 50, MaxSize: 10, Sorts: map[string]string{"name": "name"}})
	if err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	if got.PageSize != 10 {
		t.Errorf("pageSize = %d, want the cap 10 (a default above the cap is the plane's own contradiction)", got.PageSize)
	}
}

// PG2: a plane that names no bounds still gets the convention's defaults
// rather than an unbounded page.
func TestZeroBoundsStillPageAtTheConventionDefaults(t *testing.T) {
	got, err := Request{}.Normalise(Bounds{})
	if err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	if got.PageSize != 20 || got.Sort != "name" {
		t.Errorf("zero bounds = pageSize %d sort %q, want the convention's 20 and \"name\"", got.PageSize, got.Sort)
	}
	clamped, err := Request{PageSize: 5000}.Normalise(Bounds{})
	if err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	if clamped.PageSize != 100 {
		t.Errorf("pageSize 5000 on zero bounds = %d, want the convention's cap 100", clamped.PageSize)
	}
}

// PG1: page is 1-based and below 1 becomes 1.
func TestPageBelowOneBecomesOne(t *testing.T) {
	for _, in := range []int{-5000, -1, 0} {
		got, err := Request{Page: in, PageSize: 20}.Normalise(agentBounds())
		if err != nil {
			t.Fatalf("page %d refused: %v", in, err)
		}
		if got.Page != 1 {
			t.Errorf("page %d = %d, want 1", in, got.Page)
		}
	}
	got, _ := Request{Page: 7, PageSize: 20}.Normalise(agentBounds())
	if got.Page != 7 {
		t.Errorf("page 7 = %d", got.Page)
	}
}

// PG1: `dir` has two words. A word that means "newest" to a person is refused
// naming it, because the daemon answers in the words it actually has.
func TestDirRefusesAnythingButAscOrDesc(t *testing.T) {
	for _, in := range []string{"ASC", "Desc", "newest", "down", "ascending", ";"} {
		_, err := Request{Dir: in}.Normalise(agentBounds())
		if err == nil {
			t.Errorf("dir %q was accepted; only \"asc\" and \"desc\" are directions", in)
			continue
		}
		if !strings.Contains(err.Error(), in) {
			t.Errorf("dir %q: the refusal does not name the value — %v", in, err)
		}
	}
	for _, in := range []string{"asc", "desc"} {
		if _, err := (Request{Dir: in}).Normalise(agentBounds()); err != nil {
			t.Errorf("dir %q refused: %v", in, err)
		}
	}
}

// PG3/PG1: a search term is trimmed, and a long one is refused naming the cap
// and the length — never silently truncated.
func TestQueryIsTrimmedAndCapped(t *testing.T) {
	got, err := Request{Q: "  review  "}.Normalise(agentBounds())
	if err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	if got.Q != "review" {
		t.Errorf("q = %q, want the trimmed \"review\"", got.Q)
	}

	ok := strings.Repeat("a", DefaultMaxQuery)
	if _, err := (Request{Q: ok}).Normalise(agentBounds()); err != nil {
		t.Errorf("a q of exactly the cap must pass: %v", err)
	}

	long := strings.Repeat("a", DefaultMaxQuery+1)
	_, err = (Request{Q: long}).Normalise(agentBounds())
	if err == nil {
		t.Fatal("a q past the cap was accepted; it must refuse naming the cap")
	}
	if !strings.Contains(err.Error(), "200") || !strings.Contains(err.Error(), "201") {
		t.Errorf("the refusal should name the cap and the length it got: %v", err)
	}

	if _, err := (Request{Q: "abc"}).Normalise(Bounds{MaxQuery: 2, Sorts: map[string]string{"name": "name"}}); err == nil {
		t.Error("a plane's own MaxQuery of 2 accepted a 3-character term")
	}
}

// PG2/PG3: the offset is what the page means, on a normalised request.
func TestOffsetIsWhatThePageMeans(t *testing.T) {
	cases := []struct {
		req  Request
		want int
	}{
		{Request{Page: 1, PageSize: 20}, 0},
		{Request{Page: 2, PageSize: 20}, 20},
		{Request{Page: 7, PageSize: 10}, 60},
		{Request{Page: 100, PageSize: 1}, 99},
	}
	for _, c := range cases {
		if got := c.req.Offset(); got != c.want {
			t.Errorf("page %d at %d a page: offset %d, want %d", c.req.Page, c.req.PageSize, got, c.want)
		}
	}
	// A raw page 0 must not reach a negative offset: the floor holds even when
	// a caller skips Normalise, because a negative OFFSET is a wrong answer
	// about where the table starts.
	if got := (Request{Page: 0, PageSize: 20}).Offset(); got != 0 {
		t.Errorf("page 0: offset %d, want 0 (the floor holds even on a raw request)", got)
	}
}

// PG4: the sort name resolves through the map to the column the PLANE wrote.
func TestColumnResolvesOnlyThroughTheMap(t *testing.T) {
	b := agentBounds()
	want := map[string]string{
		"name": "name", "slug": "slug", "version": "version", "internal": "internal",
		"signed": "signed", "createdAt": "created_at", "updatedAt": "updated_at",
	}
	for name, col := range want {
		request := Request{Sort: name, Dir: "asc"}
		got, dir, err := request.Column(b)
		if err != nil {
			t.Errorf("sort %q refused: %v", name, err)
			continue
		}
		if got != col {
			t.Errorf("sort %q resolved to %q, want the plane's literal %q", name, got, col)
		}
		if dir != "asc" {
			t.Errorf("sort %q: dir = %q, want asc", name, dir)
		}
	}
	// An absent sort takes the default the bounds name.
	got, _, err := Request{}.Column(b)
	if err != nil || got != "name" {
		t.Errorf("absent sort = %q (err %v), want the default column \"name\"", got, err)
	}
	// desc is the other direction.
	if _, dir, err := (Request{Sort: "name", Dir: "desc"}).Column(b); err != nil || dir != "desc" {
		t.Errorf("dir desc = %q (err %v)", dir, err)
	}
}

// PG4 is a security property, so it gets its own test: nothing a caller typed
// reaches SQL. The hostile names below are the ones that would land an ORDER
// BY if the resolution were string concatenation, which is what this map
// replaced.
func TestNoCallerStringReachesTheOrderBy(t *testing.T) {
	b := agentBounds()
	hostile := []string{
		"slug;DROP TABLE agent--",
		"slug) --",
		"(SELECT password FROM user)",
		"1/0",
		"created_at, updated_at",
		"NAME",
		"",
	}
	for _, name := range hostile {
		_, _, err := Request{Sort: name}.Column(b)
		if name == "" {
			if err != nil {
				t.Errorf("an absent sort must take the default, not refuse: %v", err)
			}
			continue
		}
		if err == nil {
			t.Errorf("sort %q resolved — the map let a caller's string through", name)
			continue
		}
		if !strings.Contains(err.Error(), name) {
			t.Errorf("sort %q: the refusal does not name it — %v", name, err)
		}
	}
}

// The refusal lists what IS allowed, so the caller can fix it without reading
// the source.
func TestUnknownSortRefusalListsTheAllowList(t *testing.T) {
	_, _, err := (Request{Sort: "prompts"}).Column(agentBounds())
	msg := err.Error()
	for _, name := range []string{"createdAt", "internal", "name", "signed", "slug", "updatedAt", "version"} {
		if !strings.Contains(msg, `"`+name+`"`) {
			t.Errorf("the refusal omits %q: %s", name, msg)
		}
	}
	if !strings.Contains(msg, `"prompts"`) {
		t.Errorf("the refusal does not name the offender: %s", msg)
	}
}

// SortNames publishes the same allow-list the resolver uses — one source.
func TestSortNamesAreTheAllowListSorted(t *testing.T) {
	b := agentBounds()
	names := SortNames(b)
	if len(names) != len(b.Sorts) {
		t.Fatalf("SortNames returned %d names for %d keys", len(names), len(b.Sorts))
	}
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Fatalf("SortNames is not sorted: %v", names)
		}
	}
	// Every name it publishes must resolve, or the two halves disagree.
	for _, n := range names {
		if _, _, err := (Request{Sort: n}).Column(b); err != nil {
			t.Errorf("SortNames published %q which the resolver refuses: %v", n, err)
		}
	}
}

// PG4: LIKE's three metacharacters are escaped, the escape first, and the
// term becomes a contains-pattern.
func TestLikeEscapesTheMetacharacters(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"review", "%review%"},
		{"100%", "%100\\%%"},
		{"a_b", "%a\\_b%"},
		{`back\slash`, `%back\\slash%`},
		{`%_\`, `%\%\_\\%`},
		{"MiXeD Case", "%MiXeD Case%"},
	}
	for _, c := range cases {
		if got := Like(c.in); got != c.want {
			t.Errorf("Like(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The empty term must NOT become "%%": a caller that forgot the check would
// add a condition matching every row, which looks exactly like a filter that
// worked.
func TestLikeEmptyIsNotMatchEverything(t *testing.T) {
	if got := Like(""); got != "" {
		t.Errorf(`Like("") = %q, want "" so the caller adds no WHERE clause at all`, got)
	}
	if got := Like("   "); got != "" {
		t.Errorf("Like of blanks = %q; the term is trimmed by Normalise first — the pattern is not the place to guess", got)
	}
}

// PG3: the answer echoes the ask as applied, and carries the filtered total.
func TestAnswerEchoesTheAppliedRequest(t *testing.T) {
	req, err := Request{Page: 3, PageSize: 500, Sort: "updatedAt", Dir: "desc"}.Normalise(agentBounds())
	if err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	a := NewAnswer(req, 37)
	if a.Page != 3 || a.PageSize != 100 || a.Total != 37 {
		t.Errorf("answer = %+v, want page 3, the CLAMPED size 100, total 37", a)
	}
}

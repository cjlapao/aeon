// Package paging is the daemon's one implementation of paged reads: the
// request a client sends, the clamp it lands on, the offset it means, the
// sort name it is allowed to ask for, the LIKE pattern its search term
// becomes, and the answer block every paged result echoes back.
//
// It exists because "every endpoint pages the same way" is only a true claim
// when it is one function and not fifty hand-rolled copies that drift. The
// convention itself — the four keys, the defaults, the end-of-data rule, the
// closed sort allow-list, who adopts and when — is written down in
// ../../docs/aeon/PAGING.md; this package is that text turned into code.
//
// # Two rules this package is the guard for
//
// PG4 — a word the caller typed never reaches SQL text. Column is the ONLY
// bridge from a wire name to a SQL word, and it crosses by LOOKING THE NAME UP
// in a map whose values are column literals written by the plane's own code.
// A caller cannot reach an ORDER BY with a string it sent; it reaches one by
// naming a key the map holds. An unknown name is refused, naming it.
//
// PG3 — the answer echoes the ask. Normalise returns the request as the daemon
// will apply it (clamped, defaulted), and that value is what goes into Answer.
// The UI then has nothing to compute, and cannot disagree with the daemon.
//
// # What this package is NOT
//
// It holds no SQL and imports no database. The plane's window query lives in
// the plane's own store file (internal/store/agent_page.go is the first
// adopter); this package decides what that query is asked to do and nothing
// about how it does it. A package that needs no database is a package that can
// be tested exhaustively, which is the whole reason it is split out.
package paging

import (
	"fmt"
	"sort"
	"strings"
)

// DefaultMaxQuery is how long a search term may be. It is a cap and not a
// style choice: the term becomes a LIKE pattern bound as a parameter, so a
// huge one is a full scan with an expensive matcher attached. A refusal that
// names the cap is honest; a silent truncation would be a wrong answer.
const DefaultMaxQuery = 200

// Request is one paged read as the wire asked for it. Zero means "absent":
// the wire's absent key takes the default Bounds names, so a client may send
// nothing at all and get page 1 at the default size in the default order.
//
// Page is 1-based — the kit's table state and the daemon's answer agree on
// that number, and one of them had to pick.
type Request struct {
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
	Sort     string `json:"sort"`
	Dir      string `json:"dir"`
	Q        string `json:"q"`
}

// Bounds is a plane saying what its paged read allows. Sorts is the closed
// allow-list: the key is what a client may send, the value is the SQL column
// it resolves to, and both are literals in the plane's own source.
type Bounds struct {
	DefaultSize int
	MaxSize     int
	DefaultSort string
	MaxQuery    int
	Sorts       map[string]string
}

// answer for a bounds field nobody set: the convention's defaults, so a plane
// cannot accidentally page by an unbounded size or search without a cap.
const (
	fallbackDefaultSize = 20
	fallbackMaxSize     = 100
	fallbackMaxQuery    = DefaultMaxQuery
	fallbackSort        = "name"
)

func (b Bounds) withDefaults() Bounds {
	if b.DefaultSize <= 0 {
		b.DefaultSize = fallbackDefaultSize
	}
	if b.MaxSize <= 0 {
		b.MaxSize = fallbackMaxSize
	}
	if b.MaxSize < b.DefaultSize {
		// A plane whose default is above its own cap has contradicted itself.
		// The smaller number wins and the default comes down with it: what this
		// package exists to prevent is a read bigger than the plane meant, and
		// honouring the larger of two contradictory numbers is exactly that.
		b.DefaultSize = b.MaxSize
	}
	if b.MaxQuery <= 0 {
		b.MaxQuery = fallbackMaxQuery
	}
	if b.DefaultSort == "" {
		b.DefaultSort = fallbackSort
	}
	return b
}

// Normalise clamps the request into the bounds and fills every absent key with
// the default the bounds name. It returns the request AS THE DAEMON WILL APPLY
// IT, which is the value a paged result must echo (PG3) — not what arrived.
//
// It refuses exactly two things, each naming the key and the value:
//
//   - a `dir` that is not asc or desc. A word like "newest" is a guess about
//     what the daemon means by order, and a guess the caller should hear back.
//   - a `q` longer than Bounds.MaxQuery.
//
// An out-of-range page or page size is NOT a refusal: PG3 says they clamp, and
// the clamped value is what the answer echoes, so the caller learns the truth
// from the response instead of from an error message.
func (r Request) Normalise(b Bounds) (Request, error) {
	b = b.withDefaults()
	out := r

	if out.Page < 1 {
		out.Page = 1
	}
	switch {
	case out.PageSize <= 0:
		out.PageSize = b.DefaultSize
	case out.PageSize > b.MaxSize:
		out.PageSize = b.MaxSize
	}
	if out.Dir == "" {
		out.Dir = "asc"
	}
	if out.Dir != "asc" && out.Dir != "desc" {
		return Request{}, fmt.Errorf("sort direction %q is not a direction — dir is \"asc\" or \"desc\" and nothing else", out.Dir)
	}
	if out.Sort == "" {
		out.Sort = b.DefaultSort
	}
	out.Q = strings.TrimSpace(out.Q)
	if len(out.Q) > b.MaxQuery {
		return Request{}, fmt.Errorf("the search term is %d characters — q is capped at %d, and nothing was truncated to get there", len(out.Q), b.MaxQuery)
	}
	return out, nil
}

// Offset is how many rows the window starts past. Call it on a NORMALISED
// request: on a raw one it answers page 0's offset, which is not a thing the
// daemon should ever do.
func (r Request) Offset() int {
	page := r.Page
	if page < 1 {
		page = 1
	}
	return (page - 1) * r.PageSize
}

// Column is the ONLY bridge from a word on the wire to a word in SQL (PG4).
// It looks the sort name up in Bounds.Sorts — a map whose values are column
// literals written by the plane — and returns that literal plus the direction,
// ready for an ORDER BY. An unknown name is refused NAMING the name, listing
// what the plane does allow, before SQL is asked anything.
func (r Request) Column(b Bounds) (column, dir string, err error) {
	b = b.withDefaults()
	name := r.Sort
	if name == "" {
		name = b.DefaultSort
	}
	col, ok := b.Sorts[name]
	if !ok {
		return "", "", fmt.Errorf("sort %q is not one of the sorts this read answers — %s", name, joinNames(b.Sorts))
	}
	dir = r.Dir
	if dir == "" {
		dir = "asc"
	}
	if dir != "asc" && dir != "desc" {
		return "", "", fmt.Errorf("sort direction %q is not a direction — dir is \"asc\" or \"desc\" and nothing else", dir)
	}
	return col, dir, nil
}

// SortNames is the allow-list as words, for a refusal that lists them or a
// vocabulary answer that publishes them. Sorted, so two calls agree.
func SortNames(b Bounds) []string {
	out := make([]string, 0, len(b.Sorts))
	for k := range b.Sorts {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Like turns a search term into the pattern a LIKE matches, escaping the three
// characters that would otherwise be operators: `\` then `%` then `_`, in that
// order, because escaping the escape last would double-escape the rest.
//
// The SQL must carry ESCAPE '\' to agree with this function — the pair is the
// contract, and one half alone is a wrong answer about a name that contains a
// percent sign.
//
// An empty term answers the empty string, NOT "%%". A caller that forgot to
// check for an empty search then adds a condition matching every row, which
// reads exactly like a filter that worked. Empty in, empty out, and the caller
// adds no WHERE clause at all.
//
// It trims as Normalise does, so the pattern is never whitespace no matter
// which half of the convention a caller went through.
func Like(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(q) + 8)
	b.WriteByte('%')
	for _, r := range q {
		switch r {
		case '\\', '%', '_':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('%')
	return b.String()
}

// Answer is the block every paged result carries beside its rows: the request
// as the daemon applied it, plus how many rows the FILTER has in total. Embed
// it in the plane's result struct — encoding/json inlines it, so the wire
// shape is flat and matches the convention's text.
//
// Total counts the filtered set, taken BEFORE the window: it is the number a
// page control needs, and a count of the rows in this response would be a
// different number that disagrees with the first one.
type Answer struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
	Total    int `json:"total"`
}

// NewAnswer builds the echo from a normalised request and the filtered count.
func NewAnswer(r Request, total int) Answer {
	return Answer{Page: r.Page, PageSize: r.PageSize, Total: total}
}

func joinNames(m map[string]string) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, `"`+k+`"`)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

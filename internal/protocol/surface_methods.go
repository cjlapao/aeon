package protocol

// surface_methods.go — the two doors that answer the classification (S47 T6,
// TC6/TC8/P34): `surface.list` and `surface.get`.
//
// Both are READS (TC1's rule holds for S47 as a whole: zero writes), both are the
// person's door — the same class as S46's management reads — and both are absent
// from the narrow projection because the projection's allow-list never names
// them, not because a flag hides them. The filters are applied HERE, daemon-side:
// a client that filtered 267 rows in a browser would be a client that can disagree
// with the daemon about what a door is, which is the disagreement this slice exists
// to kill.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The five doors a listing may be filtered by — the same words `SurfaceDoors`
// carries, so a filter and a rendered row cannot drift apart.
const (
	SurfaceDoorSocket       = "socket"
	SurfaceDoorPublic       = "public"
	SurfaceDoorNarrow       = "narrow"
	SurfaceDoorAgentService = "agentService"
	SurfaceDoorToolTwin     = "toolTwin"
)

// SurfaceListFilter is what a person narrowed the listing to. Every field is
// optional; a nil field does not filter. There is no actor here and there never
// will be (TC9/MC2): the answer is about the daemon's surface, not about who is
// asking, and a params field named `actor` is refused rather than ignored.
type SurfaceListFilter struct {
	Kind          *SurfaceKind `json:"kind,omitempty"`
	Door          *string      `json:"door,omitempty"`
	Q             *string      `json:"q,omitempty"`
	HumanDoorOnly *bool        `json:"humanDoorOnly,omitempty"`
}

// AnyFilter says whether anything was narrowed at all — the difference between
// "the daemon has no methods" and "your filter matched none", which the empty
// listing must never blur.
func (f SurfaceListFilter) AnyFilter() bool {
	return f.Kind != nil || f.Door != nil || f.Q != nil || f.HumanDoorOnly != nil
}

// SurfaceCounts is the shape of what a listing contains, so a screen says "43
// writes of 267" without counting the rows itself.
type SurfaceCounts struct {
	// Total counts the entries RETURNED — what the caller is being shown.
	Total int `json:"total"`
	// Unfiltered counts the whole registered surface, so a filtered answer still
	// says how much of the truth it is.
	Unfiltered int `json:"unfiltered"`
	// ByKind totals the returned entries per kind; every kind appears, zero
	// included, so an empty bucket is an answer and not an absence.
	ByKind map[SurfaceKind]int `json:"byKind"`
	// ByDoor totals the returned entries per door; a method counted under two
	// doors is counted under both, because the question is "how many reach it",
	// not "how many methods are these".
	ByDoor map[string]int `json:"byDoor"`
}

// SurfaceListResult is `surface.list`'s answer.
type SurfaceListResult struct {
	Methods []SurfaceEntry `json:"methods"`
	Counts  SurfaceCounts  `json:"counts"`
	// Statement is present only when a FILTER matched nothing: it is the filter's
	// answer, never a claim that the daemon exposes nothing.
	Statement string `json:"statement,omitempty"`
}

// SurfaceGetParams names one method.
type SurfaceGetParams struct {
	Method string `json:"method"`
}

// SurfaceGetResult is `surface.get`'s answer.
type SurfaceGetResult struct {
	Entry SurfaceEntry `json:"entry"`
}

// surfaceListNoMatchStatement is the fixed sentence for a filtered-empty listing.
const surfaceListNoMatchStatement = "no methods match this filter."

// handleSurfaceList filters the computed listing. The classification is looked up
// once, the doors are computed once, and the filtering is pure over that answer.
func (t *Table) handleSurfaceList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var f SurfaceListFilter
	if err := rejectIdentityKeys(string(MethodSurfaceList), raw); err != nil {
		return nil, err
	}
	if err := decodeParams(raw, &f); err != nil {
		return nil, err
	}
	if f.Kind != nil && !validSurfaceKind(*f.Kind) {
		return nil, ErrBadParams{Err: fmt.Errorf("kind %q is not one of %s — the classification set is closed",
			*f.Kind, strings.Join(kindWords(), " · "))}
	}
	if f.Door != nil && !validSurfaceDoor(*f.Door) {
		return nil, ErrBadParams{Err: fmt.Errorf("door %q is not one of %s — the door set is closed",
			*f.Door, strings.Join(doorWords(), " · "))}
	}
	// A `door=narrow` filter asks a question this daemon cannot answer without the
	// narrow seam. The honest answer is the refusal that names the missing seam —
	// never an empty list, which would read as "nothing is projected".
	if f.Door != nil && *f.Door == SurfaceDoorNarrow && t.NarrowMethods == nil {
		return nil, ErrNotWired.WithWhat("the narrow door cannot be filtered on this daemon — Table.NarrowMethods was never " +
			"wired, so every entry reports narrow:" + SurfaceNarrowUnknown + "; the projection is a fact internal/narrow owns")
	}

	all := t.Surface()
	kept := make([]SurfaceEntry, 0, len(all))
	for _, e := range all {
		if !surfaceEntryMatches(e, f) {
			continue
		}
		kept = append(kept, e)
	}
	counts := SurfaceCounts{
		Total:      len(kept),
		Unfiltered: len(all),
		ByKind:     map[SurfaceKind]int{},
		ByDoor:     map[string]int{},
	}
	for _, k := range AllSurfaceKinds() {
		counts.ByKind[k] = 0
	}
	for _, e := range kept {
		counts.ByKind[e.Kind]++
		if e.Doors.Socket {
			counts.ByDoor[SurfaceDoorSocket]++
		}
		if e.Doors.Public {
			counts.ByDoor[SurfaceDoorPublic]++
		}
		if e.Doors.Narrow != SurfaceNarrowNone && e.Doors.Narrow != SurfaceNarrowUnknown {
			counts.ByDoor[SurfaceDoorNarrow]++
		}
		if e.Doors.AgentService {
			counts.ByDoor[SurfaceDoorAgentService]++
		}
		if e.Doors.ToolTwin != "" {
			counts.ByDoor[SurfaceDoorToolTwin]++
		}
	}
	out := SurfaceListResult{Methods: kept, Counts: counts}
	if len(kept) == 0 && f.AnyFilter() {
		out.Statement = surfaceListNoMatchStatement
	}
	return out, nil
}

// handleSurfaceGet answers one method by name. The refusal names the nearest
// registered methods: a bare not-found about a 267-name surface is an insult to
// the person typing it.
func (t *Table) handleSurfaceGet(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p SurfaceGetParams
	if err := rejectIdentityKeys(string(MethodSurfaceGet), raw); err != nil {
		return nil, err
	}
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.Method)
	if name == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("method is required — name one registered method, like %q", "loop.show")}
	}
	if _, ok := t.Lookup(MethodName(name)); !ok {
		return nil, ErrSurfaceUnknown.WithWhat("the surface.get read named %q, which this daemon does not register; the nearest names are: %s",
			name, nearestSurfaceNames(MethodName(name), t.Methods()))
	}
	for _, e := range t.Surface() {
		if string(e.Method) == name {
			return SurfaceGetResult{Entry: e}, nil
		}
	}
	// Registered and then gone between the two reads: a live table can lose a
	// conditional family only at construction, so this is a race with a bug, and
	// it says so rather than answering with a zero entry.
	return nil, ErrSurfaceUnknown.WithWhat("the surface.get read named %q, which this daemon reports but does not carry; the table changed under the read", name)
}

// surfaceEntryMatches applies one filter. Every comparison is exact except `q`,
// which is a case-insensitive substring over the method name and the summary —
// the two fields a person actually reads.
func surfaceEntryMatches(e SurfaceEntry, f SurfaceListFilter) bool {
	if f.Kind != nil && e.Kind != *f.Kind {
		return false
	}
	if f.HumanDoorOnly != nil && e.HumanDoorOnly != *f.HumanDoorOnly {
		return false
	}
	if f.Door != nil {
		switch *f.Door {
		case SurfaceDoorSocket:
			if !e.Doors.Socket {
				return false
			}
		case SurfaceDoorPublic:
			if !e.Doors.Public {
				return false
			}
		case SurfaceDoorNarrow:
			if e.Doors.Narrow == SurfaceNarrowNone || e.Doors.Narrow == SurfaceNarrowUnknown {
				return false
			}
		case SurfaceDoorAgentService:
			if !e.Doors.AgentService {
				return false
			}
		case SurfaceDoorToolTwin:
			if e.Doors.ToolTwin == "" {
				return false
			}
		}
	}
	if f.Q != nil {
		q := strings.ToLower(strings.TrimSpace(*f.Q))
		if q != "" && !strings.Contains(strings.ToLower(string(e.Method)), q) &&
			!strings.Contains(strings.ToLower(e.Summary), q) {
			return false
		}
	}
	return true
}

func validSurfaceKind(k SurfaceKind) bool {
	for _, v := range AllSurfaceKinds() {
		if v == k {
			return true
		}
	}
	return false
}

func kindWords() []string {
	out := make([]string, 0, 4)
	for _, v := range AllSurfaceKinds() {
		out = append(out, string(v))
	}
	return out
}

func validSurfaceDoor(d string) bool {
	for _, v := range doorWords() {
		if v == d {
			return true
		}
	}
	return false
}

func doorWords() []string {
	return []string{SurfaceDoorSocket, SurfaceDoorPublic, SurfaceDoorNarrow, SurfaceDoorAgentService, SurfaceDoorToolTwin}
}

// nearestSurfaceNames ranks the registered names by edit distance to want and
// quotes the three closest. The distance is local and cheap on purpose: this is a
// refusal, not a search product, and a refusal that has to be fast is a refusal
// nobody reads.
func nearestSurfaceNames(want MethodName, have []MethodName) string {
	type scored struct {
		name MethodName
		d    int
	}
	all := make([]scored, 0, len(have))
	for _, n := range have {
		all = append(all, scored{name: n, d: surfaceDistance(string(want), string(n))})
	}
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && (all[j].d < all[j-1].d || (all[j].d == all[j-1].d && all[j].name < all[j-1].name)); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	top := 3
	if len(all) < top {
		top = len(all)
	}
	parts := make([]string, 0, top)
	for _, s := range all[:top] {
		parts = append(parts, fmt.Sprintf("%q", string(s.name)))
	}
	return strings.Join(parts, ", ")
}

// surfaceDistance is the Levenshtein distance over runes, with the two-row window
// so a 267-name scan stays cheap.
func surfaceDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = minInts(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func minInts(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// RegisterSurface mounts the two classification doors. They read the table
// itself, so there is no seam that can be unwired into a refusal: what makes them
// absent is a daemon that never built a table, and the belt below is the only
// reason the answer can ever be ErrNotWired.
//
// It returns whether the doors stand, so a mount reads like every other
// `Register*` in this package and a test can assert the registration rather than
// infer it from a count.
func (t *Table) RegisterSurface() bool {
	if t == nil {
		return false
	}
	t.register(&Method{
		Name:               MethodSurfaceList,
		RequiresInitialize: true,
		Handle:             t.handleSurfaceList,
	})
	t.register(&Method{
		Name:               MethodSurfaceGet,
		RequiresInitialize: true,
		Handle:             t.handleSurfaceGet,
	})
	return true
}

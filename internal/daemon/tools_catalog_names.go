// The naming helpers (S47 T5): the agent lookups keyed by slug, and the
// nearest-name ranking that makes a refusal actionable instead of bare — the
// thing refused and the closest things that do exist, in one sentence.

package daemon

import (
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// --- the small helpers -------------------------------------------------------------

// agentSlugs is every agent that EXISTS, ordered by slug. It is the roster the
// `tools.get` matrix runs over: granted or not, listed. A column list built from
// the grant chain would make an absent column mean "denied".
func (w *catalogWorld) agentSlugs() []string {
	out := make([]string, 0, len(w.agents))
	for _, a := range w.agents {
		out = append(out, a.Slug)
	}
	sort.Strings(out)
	return out
}

// lookupAgent resolves the subject of `tools.byAgent`. The slug is the identity
// every other authority keys on — the `mcp_declare` rows, the policy targets and
// the dispatcher all name the slug — so it is tried first and is what the answer
// echoes back. A display name is accepted only when it is unambiguous, because a
// person reading the agent screen types what that screen shows them, and refusing
// an unambiguous name to be strict about columns would be pedantry at their expense.
func (w *catalogWorld) lookupAgent(name string) (store.Agent, bool) {
	if a, ok := w.bySlug[name]; ok {
		return a, true
	}
	var hit store.Agent
	var hits int
	for _, a := range w.agents {
		if a.Name == name {
			hit = a
			hits++
		}
	}
	if hits == 1 {
		return hit, true
	}
	return store.Agent{}, false
}

// entryNames is the name space an unknown name is measured against: every entry the
// catalog would have listed, aliases included, because a person who types `write
// _file` typed a real spelling and the nearest-names answer should be able to say
// so rather than point at an unrelated tool.
func entryNames(all []protocol.ToolEntry) []string {
	out := make([]string, 0, len(all))
	for _, e := range all {
		out = append(out, e.Name)
		for _, a := range e.Aliases {
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// nearestToolNames is the `what` half of the typed refusal: up to three candidates,
// ranked by edit distance and then by name. It lives here because only this layer
// has read the registry and the rows (the same reason S46's MCP seam owns its
// near-names sentence), and the door ships the result untouched — a paraphrase in
// `internal/protocol` would be a second authority over a fact this layer measured.
func nearestToolNames(want string, have []string) string {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" || len(have) == 0 {
		return "none"
	}
	type cand struct {
		name string
		dist int
	}
	var cands []cand
	for _, h := range have {
		l := strings.ToLower(h)
		d := nameDistance(want, l)
		if d <= 1 || strings.HasPrefix(l, want) || (len(want) > 4 && strings.HasPrefix(l, want[:4])) {
			cands = append(cands, cand{name: h, dist: d})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		return cands[i].name < cands[j].name
	})
	if len(cands) > 3 {
		cands = cands[:3]
	}
	if len(cands) == 0 {
		return "none"
	}
	names := make([]string, 0, len(cands))
	for _, c := range cands {
		names = append(names, `"`+c.name+`"`)
	}
	return strings.Join(names, ", ")
}

// nameDistance is a bounded Levenshtein distance over runes — bounded because it
// runs over ~70 names per refusal and its only job is to rank candidates for a
// human. (The standard two-row dynamic program, no allocation per cell.)
func nameDistance(a, b string) int {
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
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// quotedNames renders a name list for a refusal sentence: `"atlas", "sentry"`, or
// `none` for the empty case, which is a fact worth saying out loud rather than a
// trailing comma.
func quotedNames(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, `"`+n+`"`)
	}
	return strings.Join(out, ", ")
}

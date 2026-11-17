package tools

// granted.go — the one definition of "what a grant list yields", lifted out of
// `BuildToolset` byte-for-byte and called back (S47 T4, TC5).
//
// The catalog answers "can agent A call tool T" and the builder answers "build
// the tools this step may call". Those are the same question with the same list,
// and while each had its own handling the catalog could have said a tool was
// held while the builder refused to construct it — the exact disagreement a
// person would read as the app lying about what an agent can do. Extraction is
// what makes that pair structurally impossible rather than tested into
// agreement: one function validates the names, checks the registry, and owns the
// error a person reads.
//
// The extraction is behaviour-preserving and nothing else was allowed to change.
// `granted_characterize_test.go` pins the pre-extraction behaviour — order and
// duplicates kept verbatim, the error text quoted literally, the alias accepted
// as a grant spelling, the `WorkspaceRoot` panic left where it is — and it was
// committed before this file and has not been edited since.
//
// What `GrantedSet` deliberately does **not** do:
//
//   - It does not dedupe aliases. `["write","write_file"]` is two grant spellings
//     of one tool and both come back, because `byName` must answer to either (a
//     pinned blueprint's transcript asks for the old name) and `Granted()` is the
//     author's list, not a summary. The SEC-051 "ONE tool, ONE spec" dedupe
//     happens in `Specs()`, where the first granted spelling wins the offered
//     slot. An extractor that collapsed here would silently change what a step is
//     recorded as holding.
//   - It does not resolve a grant through an agent, a blueprint lane or a policy —
//     that is the grant chain, above this function (T0 item 3) and, for the
//     catalog's view of it, `internal/toolcatalog`.
//   - It does not consult the clock or the store. It is the registry's name check
//     and nothing more, which is why both the builder and a read-only door can
//     stand on it.

import (
	"fmt"
	"sort"
	"strings"
)

// GrantedSet validates a grant list against the registry and returns the names
// in grant order, duplicates and alias spellings preserved.
//
// An unknown name is an error and not an omission: a typo in a grant list would
// otherwise produce a step with fewer tools than its author intended while every
// listing still said the tool was there. The misses are named sorted (so the
// message is stable across a shuffled config) alongside the honest listing from
// `ToolNames()` — never `Registry()`'s keys, which would offer the hidden alias
// as something to grant.
func GrantedSet(granted []string) ([]string, error) {
	return grantedSet(Registry(), granted)
}

// grantedSet is the extraction itself, taking the registry it validates against
// so `BuildToolset` — which needs the tools, not just the names — does not build
// the whole registry a second time. `GrantedSet` is this with `Registry()`
// supplied, and is the form the catalog and the doors call.
func grantedSet(registry map[string]Tool, granted []string) ([]string, error) {
	var unknown []string
	for _, name := range granted {
		if _, exists := registry[name]; !exists {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("tools: the grant list names %s, which do not exist. The tools "+
			"that do are %s. An unknown name is refused rather than ignored, because ignoring it "+
			"gives the step fewer tools than its author intended and the model works around the "+
			"absence silently",
			strings.Join(unknown, ", "), strings.Join(ToolNames(), ", "))
	}
	// The author's list, verbatim: same nil-versus-empty shape, same order, same
	// duplicates, same alias spellings — the copy `BuildToolset` used to make.
	return append([]string(nil), granted...), nil
}

// IsGrantedTool answers the narrow question a door asks when it has one name and
// no list to validate: is this a tool the registry knows, under either its honest
// or a compatibility spelling? Never a grant (TC5's distinction — a tool that
// exists is not a tool anybody may call), and never a substitute for
// `GrantedSet`'s refusal of a whole list.
func IsGrantedTool(name string) bool {
	_, ok := Registry()[name]
	return ok
}

// CanonicalGrant is the SEC-051 half, as its own named step rather than folded
// into `GrantedSet`: the same list with each tool appearing once, under the
// **first spelling the author granted**, order preserved.
//
// Why it is separate and not part of `GrantedSet`: the builder's own list keeps
// both spellings — `byName` has to answer to either, and `Granted()` is the
// author's list rather than a summary — so a catalog that deduped inside the
// shared validator would disagree with the builder it exists to describe, which
// is the disagreement TC5 forbids. The dedupe that keeps a model from being shown
// one tool twice is `Specs()`'s, and it applies this same rule ("the first
// granted spelling wins the slot"). A door listing what an agent holds wants the
// deduped view, so it calls this; a door building what an agent may call calls
// `GrantedSet`.
func CanonicalGrant(granted []string) []string {
	out := make([]string, 0, len(granted))
	seen := make(map[string]bool, len(granted))
	for _, name := range granted {
		owner := CanonicalToolName(name)
		if seen[owner] {
			continue
		}
		seen[owner] = true
		out = append(out, name)
	}
	return out
}

package attention

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/store"
)

// scope.go — SV1's scope rule: the inbox honours a root component's TREE.
//
// `rootId` names a component (S44-extra's self-referencing tree, roots are
// `parent_id IS NULL`). A root's scope is itself PLUS its children's rows —
// the tree, not the one directory; a CHILD id is accepted and scopes to that
// child alone; an unknown (or archived) id is a typed REFUSAL
// (ErrUnknownScope → the socket's ErrInboxScope, −31926), never an empty
// list: an empty answer to a typo'd id is how an inbox starts lying.
//
// Loops and questions are keyed by the component's ROOT PATH (the OL1
// key), so the resolution is component-id → the set of live paths, plus the
// two maps the items need back (which component owns a path, and which root
// owns it) — resolved ONCE per query, from one live read (no cache: N8).

// ErrUnknownScope is the typed refusal for a component id this store does
// not carry live. It wraps into the protocol's ErrInboxScope with the same
// sentence — the daemon never paraphrases a fact this layer owns.
var ErrUnknownScope = errors.New("attention: the inbox scope named a component this store does not carry")

// Scope is one resolved query scope: the paths to read, and the path→id
// maps that turn a row's path back into the ids the item carries.
type Scope struct {
	// RootPaths is what the STORES filter on: the root PATHS whose trees are
	// in scope (store.RootScopeSQL matches the path and everything under it).
	RootPaths []string
}

// EveryScope is the `rootId omitted` answer: every live component's paths
// (roots and children alike — "every root" means every tree, which at read
// time is every live path).
func EveryScope(reader store.ComponentTree) (Scope, error) {
	comps, err := reader.LiveComponentTree()
	if err != nil {
		return Scope{}, err
	}
	var roots []string
	for _, c := range comps {
		if c.ParentID != "" {
			continue
		}
		for _, p := range c.Paths {
			if p != "" {
				roots = append(roots, p)
			}
		}
	}
	return Scope{RootPaths: sortDedupe(roots)}, nil
}

// ResolveScope turns a component id into its tree's paths (see above).
func ResolveScope(reader store.ComponentTree, rootID string) (Scope, error) {
	comps, err := reader.LiveComponentTree()
	if err != nil {
		return Scope{}, err
	}
	_ = comps
	byID := map[string]store.AttentionComponent{}
	for _, c := range comps {
		byID[c.ID] = c
	}
	self, ok := byID[rootID]
	if !ok {
		return Scope{}, fmt.Errorf("%w: %s", ErrUnknownScope, rootID)
	}
	var roots []string
	// A root's own paths AND a child's own paths: store.RootScopeSQL's
	// subtree arm (`path/%`) carries everything under a root, and a child's
	// path — exact arm — is its own scope, the child's tree included.
	roots = append(roots, self.Paths...)
	return Scope{RootPaths: sortDedupe(roots)}, nil
}

// OwnerMaps answers "which component owns this row's path, and under which
// ROOT", for item assembly: longest-prefix ownership over the live tree. A
// path no component claims resolves to no ids — the item still reports with
// its path's truth (the loop row is the source), rather than a fabricated
// owner or a dropped item.
type OwnerMaps struct {
	by   []store.AttentionComponent
	root map[string]string
}

// Owners indexes the live tree for path ownership.
func Owners(reader store.ComponentTree) (*OwnerMaps, error) {
	comps, err := reader.LiveComponentTree()
	if err != nil {
		return nil, err
	}
	parent := map[string]string{}
	for _, c := range comps {
		parent[c.ID] = c.ParentID
	}
	om := &OwnerMaps{by: comps, root: map[string]string{}}
	for _, c := range comps {
		root := c.ID
		for hop, guard := parent[c.ID], 0; hop != "" && guard < 8; hop, guard = parent[hop], guard+1 {
			root = hop
		}
		om.root[c.ID] = root
	}
	return om, nil
}

// Owner maps a row's component_root path to (its component id, its root
// component id), longest attached path winning; an unclaimed path answers
// ("", "") — no ids rather than wrong ids.
func (om *OwnerMaps) Owner(path string) (componentID, rootID string) {
	best, bestLen := "", -1
	for _, c := range om.by {
		for _, p := range c.Paths {
			if p == "" {
				continue
			}
			if path == p || strings.HasPrefix(path, p+"/") {
				if len(p) > bestLen {
					best, bestLen = c.ID, len(p)
				}
			}
		}
	}
	if best == "" {
		return "", ""
	}
	return best, om.root[best]
}

func sortDedupe(in []string) []string {
	sort.Strings(in)
	var out []string
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

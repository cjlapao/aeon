package daemon

// doc_claims.go — which workspace paths are deliverables, not files.
//
// `write`, `edit` and `read` ask the DocStore whether a path is claimed before
// they touch the disk (tools/doc_route.go). A path is claimed when it is
//
//   - a name the loop's pinned blueprint declares in `produces:` (a name ending
//     in "/" claims everything under it), or
//   - anything under the loop's OWN .projects/{slug}/ (and the v1 layout
//     .projects/loops/{slug}/) — the folder a deliverable was once mirrored
//     into, which an agent must never write to: the daemon alone exports the
//     hard copy there (hardcopy.go).
//
// Nothing else is claimed: source code, a README, a file the blueprint does not
// name stays the workspace's. The pin is immutable for the life of the loop, so
// the derived list is cached per loop.

import (
	"context"
	"path"
	"strings"
	"sync"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

var producedByLoop sync.Map // store.Scope -> []string

// Claim implements tools.DocStore.
func (d *DocService) Claim(ctx context.Context, rel string) (string, bool) {
	rel = path.Clean(strings.ReplaceAll(rel, `\`, "/"))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") {
		return "", false
	}
	sc, _, _, err := d.docScope(ctx)
	if err != nil {
		return "", false
	}
	if name, ok := underProjects(rel, sc.LoopSlug); ok {
		return name, true
	}
	for _, p := range d.producedPaths(sc) {
		if strings.HasSuffix(p, "/") {
			if strings.HasPrefix(rel, p) && rel != strings.TrimSuffix(p, "/") {
				return rel, true
			}
			continue
		}
		if rel == p {
			return rel, true
		}
	}
	return "", false
}

// underProjects strips .projects/{slug}/ or .projects/loops/{slug}/ and answers
// the document name left over — for the loop's OWN folder only. Another loop's
// folder is a hard copy the daemon exported (hardcopy.go) for whoever was
// handed its pointer, and it reads as the plain file it is.
func underProjects(rel, ownSlug string) (string, bool) {
	rest, ok := strings.CutPrefix(rel, ".projects/")
	if !ok {
		return "", false
	}
	parts := strings.Split(rest, "/")
	skip := 1
	if parts[0] == "loops" {
		skip = 2
	}
	if len(parts) <= skip || parts[skip-1] != ownSlug {
		return "", false
	}
	return strings.Join(parts[skip:], "/"), true
}

// producedPaths lists every artifact any step of the loop's pinned blueprint
// declares. Cached: the pin never changes.
func (d *DocService) producedPaths(sc store.Scope) []string {
	if v, ok := producedByLoop.Load(sc); ok {
		return v.([]string)
	}
	var out []string
	if content, err := d.store.LoopPinnedContent(sc); err == nil {
		if bp, err := blueprint.Load([]byte(content)); err == nil {
			for _, lane := range bp.Lanes {
				if lane.Step != nil {
					out = append(out, lane.Step.Produces...)
				}
				for _, st := range lane.Ladder {
					out = append(out, st.Produces...)
				}
			}
		}
		producedByLoop.Store(sc, out)
	}
	return out
}

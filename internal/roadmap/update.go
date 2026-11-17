package roadmap

// S50 T2 (RM1) — edit is a narrow, NAMED set of fields. Status, priority,
// estimate and provenance are absent from the edit type entirely: they are
// changed only by their own verbs, so the honesty rules and the lifecycle
// machine keep their one home. What CAN change: title, epic, and — while
// the item has not started — the two artifact pointers. done/cancelled
// accept the typo fix (title/epic) and nothing else; after `running` a
// path is history, and the refusal names the status that made it history.

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/cjlapao/aeon/internal/store"
)

// RM1's bounds, exported so the vocabulary read renders them rather than
// every client guessing.
const (
	TitleMaxBytes = 200
	EpicMaxBytes  = 64
)

// UpdateFields is the door's edit request: nil means UNSET (leave alone).
// ClearEpic is the one explicit removal RM1 allows (epic ≤ 64 OR cleared);
// a set-epic and clearEpic together is a contradiction, refused.
type UpdateFields struct {
	Title     *string
	Epic      *string
	ClearEpic bool
	BriefPath *string
	PlanPath  *string
}

// Update applies RM1 to a live item, audited once through the store
// (op 'update', a fields-only summary — RM2's template, never user text).
// Unchanged ⇒ Changed false with NO audit row: an edit that changed
// nothing is not an event, and history full of no-ops answers no question.
func (r *Roadmap) Update(id int64, f UpdateFields, actor Actor) (item Item, changed bool, ref *Refusal) {
	if ref = actorRefusal(actor); ref != nil {
		return Item{}, false, ref
	}
	row, found, err := r.s.RoadmapItemByID(id)
	if err != nil {
		return Item{}, false, &Refusal{Reason: fmt.Sprintf("roadmap: reading item %d: %v", id, err)}
	}
	if !found {
		return Item{}, false, &Refusal{Reason: fmt.Sprintf("roadmap: no item %d to edit — roadmap.list and roadmap.board answer the live ids", id)}
	}
	item, err = ItemFromRow(row)
	if err != nil {
		return Item{}, false, &Refusal{Reason: fmt.Sprintf("roadmap: item %d row reads invalid: %v", id, err)}
	}

	// RM1's shape rules, on the VALUES the edit carries.
	if f.Title != nil {
		if ref = checkTitle(*f.Title); ref != nil {
			return Item{}, false, ref
		}
	}
	epic := f.Epic
	if f.ClearEpic && f.Epic != nil {
		return Item{}, false, &Refusal{Reason: "roadmap.update takes epic OR clearEpic, never both — say one thing to the audit"}
	}
	if f.ClearEpic {
		empty := ""
		epic = &empty
	}
	if epic != nil && len(*epic) > EpicMaxBytes {
		return Item{}, false, &Refusal{Reason: fmt.Sprintf("an epic tag is at most %d bytes (%d given) — it groups rows on a Gantt, it is not a paragraph", EpicMaxBytes, len(*epic))}
	}

	// The path rule: absolute, under the item's own component root, and
	// only while the item has not started.
	started := !PathEditable(item)
	for _, p := range []struct {
		name string
		val  *string
	}{{"briefPath", f.BriefPath}, {"planPath", f.PlanPath}} {
		if p.val == nil {
			continue
		}
		if started {
			return Item{}, false, &Refusal{Reason: fmt.Sprintf("roadmap item %d is %q — from running the artifact paths are history; %s edits are refused naming the status (RM1)", id, item.Status, p.name)}
		}
		if ref = checkPathUnderRoot(id, item.ComponentRoot, *p.val, p.name); ref != nil {
			return Item{}, false, ref
		}
	}

	// changed:false before any SQL — compare against the live values.
	edit := store.RoadmapItemEdit{Title: f.Title, Epic: epic, BriefPath: f.BriefPath, PlanPath: f.PlanPath}
	if edit.Empty() {
		return item, false, nil
	}

	summary := updateSummary(id, item, edit)
	if summary == "" {
		// Every named field carries its CURRENT value: named is not
		// changed, and a no-op is not an event.
		return item, false, nil
	}
	wrote, err := r.s.UpdateRoadmapItemAudited(id, edit, store.RoadmapAudit{
		Op: "update", Summary: summary, Actor: actor,
	})
	if err != nil {
		return Item{}, false, &Refusal{Reason: fmt.Sprintf("roadmap: the store refused the edit: %v", err)}
	}
	if !wrote {
		return Item{}, false, &Refusal{Reason: fmt.Sprintf("roadmap: item %d vanished under the edit — nothing changed and nothing was audited", id)}
	}
	fresh, found, err := r.s.RoadmapItemByID(id)
	if err != nil || !found {
		return Item{}, false, &Refusal{Reason: fmt.Sprintf("roadmap: item %d edited but cannot be re-read: %v", id, err)}
	}
	edited, err := ItemFromRow(fresh)
	if err != nil {
		return Item{}, false, &Refusal{Reason: fmt.Sprintf("roadmap: item %d edited but the fresh row reads invalid: %v", id, err)}
	}
	return edited, true, nil
}

// checkTitle is RM1's title law: 1–200 bytes, no control characters (a
// title with an embedded newline breaks every list that renders it, and
// one with an ESC is an injection attempt wearing a label).
func checkTitle(t string) *Refusal {
	if t == "" || len(t) > TitleMaxBytes {
		return &Refusal{Reason: fmt.Sprintf("a title is 1–%d bytes (%d given) — the Gantt row is a name, not a document", TitleMaxBytes, len(t))}
	}
	for _, r := range t {
		if unicode.IsControl(r) {
			return &Refusal{Reason: "a title carries no control characters — a name with an escape sequence in it is not a name"}
		}
	}
	return nil
}

// checkPathUnderRoot enforces the S26 artifact rule RM1 restates: the
// pointer must be absolute and live under the item's component root.
func checkPathUnderRoot(id int64, root, path, name string) *Refusal {
	if path == "" {
		return &Refusal{Reason: fmt.Sprintf("roadmap item %d: %s names no path — an artifact pointer is a path or stays unset (absence is not a sentinel)", id, name)}
	}
	if !filepath.IsAbs(path) {
		return &Refusal{Reason: fmt.Sprintf("roadmap item %d: %s %q is not absolute — artifact pointers must survive every reader's working directory", id, name, path)}
	}
	clean := filepath.Clean(path)
	r := filepath.Clean(root)
	if clean != r && !strings.HasPrefix(clean, r+string(filepath.Separator)) {
		return &Refusal{Reason: fmt.Sprintf("roadmap item %d: %s %q is not under the item's component root %q — an item's artifacts live inside its own component (S26)", id, name, path, root)}
	}
	return nil
}

// updateSummary is the fixed template: FIELD NAMES ONLY, in a fixed order,
// of the fields whose VALUES actually differ (an unchanged named field is
// not edited and does not appear). RM2: never user text.
func updateSummary(id int64, item Item, e store.RoadmapItemEdit) string {
	var fields []string
	if e.Title != nil && *e.Title != item.Title {
		fields = append(fields, "title")
	}
	if e.Epic != nil && *e.Epic != item.Epic {
		fields = append(fields, "epic")
	}
	if e.BriefPath != nil && *e.BriefPath != item.BriefPath {
		fields = append(fields, "briefPath")
	}
	if e.PlanPath != nil && *e.PlanPath != item.PlanPath {
		fields = append(fields, "planPath")
	}
	if len(fields) == 0 {
		return ""
	}
	return fmt.Sprintf("edited item %d (fields: %s)", id, strings.Join(fields, ", "))
}

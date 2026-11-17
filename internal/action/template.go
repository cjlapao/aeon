package action

// The templates (T8): the join of L3's split — identifiers from code,
// prose from the model, assembled into something a person reads. They
// are the last thing between Aeon and a pull request other people will
// see, which is what the two hard rules serve:
//
//   - an undeclared slot is invalid AT LOAD (L20/D10, via
//     blueprint.ValidateTemplates — the eighth class, same contract as
//     S8's seven: kind, path, state, slice), and
//   - an unresolved or empty slot FAILS THE ACTION (L23/D11): a PR body
//     containing {{pr_summary}} is worse than no PR — visibly broken, in
//     a permanent record, attributable to whoever opened it. No partial
//     render, never a placeholder, never a silent empty string.
//
// Slot SOURCES are the anti-injection spine (L3/I3): identifier slots
// resolve from code-side evidence and the manifest only; the handoff —
// the model's paragraph — can fill prose slots and nothing else. A
// handoff field named pr_number cannot fill pr_number, structurally:
// the resolver never reads the handoff map for an identifier slot.
//
// Q8's table (the load validator checks against it):
//
//	pr-body     task_title, task_id, loop_slug, head_ref, base_ref, issue_number,
//	            commit_sha            (evidence — code)
//	            mode                  (manifest — blueprint.Slots vocabulary)
//	            pr_summary            (handoff — the pr_summary stub, T9)
//	issue-body  task_title, task_id, loop_slug (evidence), mode (manifest),
//	            acceptance_criteria   (handoff)
//	review-body pr_number, task_title, loop_slug, verdict, review_id (evidence),
//	            mode (manifest), review_summary (handoff)

import (
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
)

//go:embed templates/*.md
var templateFS embed.FS

// SlotKind names the ONLY place a slot may resolve from.
type SlotKind string

const (
	SlotEvidence SlotKind = "evidence" // code-side facts: numbers, refs, shas (L3)
	SlotManifest SlotKind = "manifest" // the manifest's own declared vocabulary (L18)
	SlotHandoff  SlotKind = "handoff"  // the model's paragraph — prose slots only
)

// TemplateSpec is one committed template with its declared slots.
type TemplateSpec struct {
	Name   string
	Source string
	Slots  map[string]SlotKind
}

// SlotValues are the three sources, kept as three maps so a source can
// never be consulted for the wrong kind of slot.
type SlotValues struct {
	Manifest blueprint.Values
	Evidence map[string]string
	Handoff  map[string]string
}

// slotTable is the Q8 declaration — code, not configuration. The embedded
// sources must use exactly these slots (TestCommittedTemplatesAreValid
// runs the real files through the load validator).
var slotTable = map[string]map[string]SlotKind{
	"pr-body": {
		"task_title": SlotEvidence, "task_id": SlotEvidence, "loop_slug": SlotEvidence,
		"head_ref": SlotEvidence, "base_ref": SlotEvidence, "issue_number": SlotEvidence,
		"commit_sha": SlotEvidence,
		"mode":       SlotManifest,
		"pr_summary": SlotHandoff,
	},
	"issue-body": {
		"task_title": SlotEvidence, "task_id": SlotEvidence, "loop_slug": SlotEvidence,
		"mode":                SlotManifest,
		"acceptance_criteria": SlotHandoff,
	},
	"review-body": {
		"pr_number": SlotEvidence, "task_title": SlotEvidence, "task_id": SlotEvidence,
		"loop_slug": SlotEvidence, "verdict": SlotEvidence, "review_id": SlotEvidence,
		"mode":           SlotManifest,
		"review_summary": SlotHandoff,
	},
}

// TemplateRegistry returns the committed templates, name-sorted.
func TemplateRegistry() []TemplateSpec {
	names := make([]string, 0, len(slotTable))
	for name := range slotTable {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]TemplateSpec, 0, len(names))
	for _, name := range names {
		src, err := templateFS.ReadFile("templates/" + name + ".md")
		if err != nil {
			panic("action: committed template missing: " + name)
		}
		out = append(out, TemplateSpec{Name: name, Source: string(src), Slots: slotTable[name]})
	}
	return out
}

// TemplateForAction maps the outward actions to their body template; the
// handlers (T5/T7) render through it, never with a hand-written body.
func TemplateForAction(action string) string {
	switch action {
	case ForgeOpenIssue:
		return "issue-body"
	case ForgeOpenPR:
		return "pr-body"
	case ForgePostReview:
		return "review-body"
	default:
		return ""
	}
}

// ValidateTemplates runs the committed-style load check (the eighth
// class lives in blueprint, next to the seven it joins).
func ValidateTemplates(specs []TemplateSpec) []blueprint.Diagnostic {
	decls := make([]blueprint.TemplateDecl, 0, len(specs))
	for _, s := range specs {
		slots := make(map[string]string, len(s.Slots))
		for k, v := range s.Slots {
			slots[k] = string(v)
		}
		decls = append(decls, blueprint.TemplateDecl{Name: s.Name, Source: s.Source, Slots: slots})
	}
	return blueprint.ValidateTemplates(decls)
}

// ManifestSlotPaths DELEGATES to the manifest's own declaration — one
// vocabulary, no copy to drift (S8's L18 anticipated this third
// consumer).
func ManifestSlotPaths() []string { return blueprint.Slots() }

// Render fills every slot or fails. The final scan is the CLASS
// assertion (C8): no `{{` or `}}` reaches a rendered body — which catches
// the whole class rather than the one slot a test thought of.
func Render(name string, v *SlotValues) (string, error) {
	if v == nil {
		return "", fmt.Errorf("template %s: no values", name)
	}
	var spec *TemplateSpec
	for _, s := range TemplateRegistry() {
		if s.Name == name {
			got := s
			spec = &got
			break
		}
	}
	if spec == nil {
		return "", fmt.Errorf("unknown template %q — the registry is closed", name)
	}
	out := spec.Source
	for _, m := range templateSlotRe.FindAllStringSubmatch(spec.Source, -1) {
		token, slot := m[0], m[1]
		kind, ok := spec.Slots[slot]
		if !ok {
			// Defensive: the load check should have refused this template
			// before any render ran. Reaching here means a template was
			// edited after validation — fail, never emit.
			return "", fmt.Errorf("template %s has undeclared slot {{%s}} — it should have failed at load (D10)", name, slot)
		}
		var value string
		switch kind {
		case SlotEvidence:
			value = strings.TrimSpace(v.Evidence[slot])
			if value == "" {
				return "", fmt.Errorf("template %s: unresolved slot {{%s}} — evidence (code) must supply it; the action fails rather than emit a placeholder (L23)", name, slot)
			}
		case SlotManifest:
			if raw, found := v.Manifest[slot]; found {
				value = strings.TrimSpace(fmt.Sprint(raw))
			}
			if value == "" {
				return "", fmt.Errorf("template %s: unresolved slot {{%s}} — the manifest must supply it (L18 vocabulary); the action fails rather than ship a hole (L23)", name, slot)
			}
		case SlotHandoff:
			// THE structural rule (L3/I3): the handoff map is consulted
			// ONLY here, and only for prose slots. An identifier slot
			// cannot be filled from it at any priority.
			value = strings.TrimSpace(v.Handoff[slot])
			if value == "" {
				return "", fmt.Errorf("template %s: unresolved slot {{%s}} — the handoff paragraph is empty or absent; an empty paragraph is the same failure as a placeholder (D11)", name, slot)
			}
		default:
			return "", fmt.Errorf("template %s: slot {{%s}} has unknown kind %q", name, slot, kind)
		}
		// The class assertion (C8) is taken literally: NOTHING with brace
		// pairs reaches a body people read — including brace pairs
		// smuggled in a prose value, which would look exactly like an
		// unfilled template in a permanent record. Refusing beats
		// shipping what looks like a render that failed.
		if strings.Contains(value, "{{") || strings.Contains(value, "}}") {
			return "", fmt.Errorf("template %s: the value for slot {{%s}} carries brace pairs — refusing to render a body that would look like a failed render (C8)", name, slot)
		}
		out = strings.ReplaceAll(out, token, value)
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "}}") {
		return "", fmt.Errorf("template %s leaked a brace pair into a body people read (C8)", name)
	}
	return out, nil
}

var templateSlotRe = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// filer.go — the VG16 dedupe-and-file templates: what a gate failure
// files, what it is called, and the subject the dedupe runs on. The
// templates are FIXED (ids and enums, never output text): a bug body that
// quotes forge output or command output would leak the thing the gate
// failed on, and a fixed template cannot. The forge check name passes
// through safeCheckName before it enters a title; the body says the state
// ("is failure"), which is the aggregate's own word.

package gatewatch

import (
	"fmt"
	"regexp"
	"strings"
)

// The closed kind set — the three ways a gate failure reaches the filer
// (the store's gate_filing CHECK mirrors these; held in lockstep by
// TestFilingKindsMatchTheMigration).
const (
	KindVerify       = "verify"        // the verify gate failed, or its rework was exhausted
	KindMergeWatch   = "merge-watch"   // a check went red on the merged sha
	KindMergeRefused = "merge-refused" // the merge was refused: a check is not green
)

// FiledBy is the bug's author: the gate system, named, never a person
// (D7's "an agent name or 'person'" extended to the one non-agent that
// files by rule).
const FiledBy = "system:gates"

// SubjectVerify is the verify side's dedupe subject: one per component
// root, so the same red gate failing across loops of the same tree files
// ONE bug and bumps its count.
func SubjectVerify(componentRoot string) string { return componentRoot + ":verify" }

// SubjectMergeWatch is the post-merge red's subject: one per merged sha.
func SubjectMergeWatch(componentRoot, sha string) string {
	return componentRoot + ":merge:" + sha
}

// SubjectMergeRefused is the merge-refusal's subject: one per PR.
func SubjectMergeRefused(componentRoot string, pr int) string {
	return fmt.Sprintf("%s:merge-refused:%d", componentRoot, pr)
}

// checkNameKeep is the only shape a forge check name may take on its way
// into a title: letters, digits, space, dot, underscore, hyphen and slash.
// Everything else (newlines, markdown, a 500-char name) is filtered out,
// and a name that filters to nothing is replaced wholesale.
var checkNameKeep = regexp.MustCompile(`[A-Za-z0-9 ._/-]`)

const checkNamePlaceholder = "the-failing-check"

// safeCheckName is the filter: keep the legal characters, cap at 80, and
// replace an empty result. Deterministic — the same name always yields
// the same title, which is what the dedupe's stability relies on.
func safeCheckName(name string) string {
	kept := checkNameKeep.FindAllString(name, -1)
	out := strings.Join(kept, "")
	if len(out) > 80 {
		out = out[:80]
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return checkNamePlaceholder
	}
	return out
}

// Facts is everything a filing knows about the failure: ids and enums,
// the aggregate's one-line reason, and the verify summary's fixed
// template. It is the filer's ONLY input — a fact that is not an id, an
// enum, a template line or a name is not in this struct, so it cannot be
// in the body.
type Facts struct {
	Kind          string // one of the three Kind* constants
	Exhausted     bool   // the verify side: exhausted (ceiling) vs failed
	ComponentRoot string // the scope the bug files in
	ComponentID   string // "" when the merge stood without a component id
	ComponentSlug string // the human name for the titles ("" ⇒ the generic)
	PR            int    // 0 = none
	LoopSlug      string // "" = none
	RunID         string // the verify run the failure names ("" = none)
	HeadSHA       string // "" = none
	MergeSHA      string // "" = none
	BaseRef       string // "" = none
	RuleRevision  int    // 0 = none
	CheckName     string // a raw forge check name ("" = none; filtered)
	CheckReason   string // the aggregate's one-line reason ("" = none)
	GateName      string // the verify gate's name ("" = none)
	GateState     string // its state ("" = none)
	ExitCode      *int   // the gate's exit code (nil = none)
	Summary       string // the verify run's fixed-template counts ("" = none)
	Detail        string // the blocked reason's detail ("" = none)
}

// BugFor is the fixed template over the facts. It returns the title and
// the body; the caller files them (the store defaults the priority to
// unknown and the validation state to unvalidated — the filing cannot
// pre-label its own claim).
func BugFor(kind string, f Facts) (title, body string) {
	switch kind {
	case KindVerify:
		if f.Exhausted {
			title = verifyTitle("verify exhausted", f.ComponentSlug)
		} else {
			title = verifyTitle("verify failed", f.ComponentSlug)
		}
		body = verifyBody(f)
	case KindMergeWatch:
		title = fmt.Sprintf("Gate red after merge: %s PR #%d (%s)",
			slugOr(f.ComponentSlug), f.PR, safeCheckName(f.CheckName))
		body = mergeBody(f, "a check failed on the merged sha")
	case KindMergeRefused:
		title = fmt.Sprintf("Gate red: merge refused on %s PR #%d (%s)",
			slugOr(f.ComponentSlug), f.PR, safeCheckName(f.CheckName))
		body = mergeBody(f, "the merge was refused because a check is not green")
	default:
		title = "Gate red: the gate filed without a kind"
		body = "the gate filing arrived with a kind that is not one of the three — a filing bug, not a gate failure."
	}
	return title, body
}

func slugOr(slug string) string {
	if slug == "" {
		return "the component"
	}
	return slug
}

func verifyTitle(verb, slug string) string {
	return fmt.Sprintf("Gate red: %s on %s", verb, slugOr(slug))
}

func verifyBody(f Facts) string {
	var b strings.Builder
	if f.Exhausted {
		b.WriteString("Gate red: the verify gate was exhausted — the loop's retry ceiling was reached and the loop is blocked.\n")
	} else {
		b.WriteString("Gate red: the verify gate failed.\n")
	}
	if f.LoopSlug != "" {
		b.WriteString("\nloop: " + f.LoopSlug + "\n")
	}
	if f.RunID != "" {
		b.WriteString("run: " + f.RunID + " — `aeon verify runs show " + f.RunID + "`\n")
	}
	if f.HeadSHA != "" {
		b.WriteString("head: " + f.HeadSHA + "\n")
	}
	if f.Summary != "" {
		b.WriteString("gates: " + f.Summary + "\n")
	}
	if f.GateName != "" {
		line := "gate: " + f.GateName
		if f.GateState != "" {
			line += " " + f.GateState
		}
		if f.ExitCode != nil {
			line += fmt.Sprintf(" (exit %d)", *f.ExitCode)
		}
		b.WriteString(line + "\n")
	}
	if f.Exhausted && f.Detail != "" {
		b.WriteString("reason: " + f.Detail + "\n")
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func mergeBody(f Facts, lead string) string {
	var b strings.Builder
	b.WriteString("Gate red: " + lead + ".\n")
	if f.LoopSlug != "" {
		b.WriteString("\nloop: " + f.LoopSlug + "\n")
	}
	if f.PR != 0 {
		b.WriteString(fmt.Sprintf("pr: #%d\n", f.PR))
	}
	if f.MergeSHA != "" {
		b.WriteString("merge: " + f.MergeSHA + "\n")
	}
	if f.BaseRef != "" {
		b.WriteString("base: " + f.BaseRef + "\n")
	}
	if f.RuleRevision != 0 {
		b.WriteString(fmt.Sprintf("rule revision: %d\n", f.RuleRevision))
	}
	if f.CheckName != "" {
		state := "failure"
		if f.Kind == KindMergeRefused {
			// The refusal's own word: the check is not green.
			state = "not green"
		}
		b.WriteString(fmt.Sprintf("check: %s is %s\n", safeCheckName(f.CheckName), state))
	}
	if f.CheckReason != "" && f.Kind == KindMergeWatch {
		b.WriteString("reason: " + f.CheckReason + "\n")
	}
	if f.RunID != "" {
		b.WriteString("measured by run: " + f.RunID + " — `aeon verify runs show " + f.RunID + "`\n")
	}
	return strings.TrimSpace(b.String()) + "\n"
}

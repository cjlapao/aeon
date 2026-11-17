package skills

import (
	"fmt"
	"strings"
)

// validate.go checks the spec's constraints and says so in sentences. The
// spec (https://agentskills.io/specification) is the authority; where a
// reader is known to be more forgiving (Claude Code truncates long
// descriptions rather than refusing them) the finding is still raised here,
// because the bench's job is to tell the operator the truth about their
// file, not to reproduce one tool's tolerance.
//
// Every rule is an ISSUE, never a panic and never a silent fix: an invalid
// skill stays in the catalog with its issues attached (D-rule: validated
// showing the capabilities includes the capabilities that fail).

const (
	maxNameLen         = 64
	maxDescriptionLen  = 1024
	maxCompatibility   = 500
	maxLicenseLenWarn  = 200
	warnBodyLines      = 500
	thinDescriptionRun = 20 // "Helps with PDFs." — the spec names this shape as poor
)

// nameShape is the spec's name grammar: lowercase alphanumerics and single
// hyphens, no leading, trailing or consecutive hyphen. Written as an
// explicit check rather than one regex so each violation names itself —
// "must not start with a hyphen" is a fixable sentence, "regexp: no match"
// is not.
func nameProblems(name string) []string {
	if name == "" {
		return []string{"is empty"}
	}
	var out []string
	if len(name) > maxNameLen {
		out = append(out, fmt.Sprintf("is %d characters; the spec caps it at %d", len(name), maxNameLen))
	}
	if strings.HasPrefix(name, "-") {
		out = append(out, "starts with a hyphen")
	}
	if strings.HasSuffix(name, "-") {
		out = append(out, "ends with a hyphen")
	}
	if strings.Contains(name, "--") {
		out = append(out, "contains consecutive hyphens")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			out = append(out, fmt.Sprintf("holds %q; only a-z, 0-9 and single hyphens are allowed", string(r)))
			return out // one naming of the offender is enough
		}
	}
	return out
}

// ValidateSkill runs the spec's rules over one parsed skill and returns
// every finding. dirName is the folder name on disk; the spec binds the
// frontmatter name to it, and the binding is checked rather than trusted.
func ValidateSkill(dirName string, fm Frontmatter, body string, bodyLines int) []Issue {
	var issues []Issue
	add := func(sev Severity, field, format string, args ...any) {
		issues = append(issues, Issue{Field: field, Detail: fmt.Sprintf(format, args...), Severity: sev})
	}

	if fm.ParseError != "" {
		add(SeverityError, "frontmatter", "the frontmatter YAML does not parse: %s", fm.ParseError)
	}
	for _, note := range fm.Notes {
		add(SeverityWarning, "metadata", "%s", note)
	}

	if strings.TrimSpace(fm.Name) == "" {
		add(SeverityError, "name", "the frontmatter has no name — the spec makes it required, and a skill nobody can be addressed by name cannot be invoked")
	} else {
		for _, p := range nameProblems(fm.Name) {
			add(SeverityError, "name", "name %q %s", fm.Name, p)
		}
		if fm.Name != dirName {
			add(SeverityError, "name", "the name is %q but the folder is %q — the spec binds the two together, so rename the folder or the field", fm.Name, dirName)
		}
	}

	switch d := strings.TrimSpace(fm.Description); {
	case d == "":
		add(SeverityError, "description", "the frontmatter has no description — implicit invocation matches on it, and a skill without one is invisible to the agents that would have used it")
	case len(d) > maxDescriptionLen:
		add(SeverityError, "description", "the description is %d characters; the spec caps it at %d", len(d), maxDescriptionLen)
	case len(d) < thinDescriptionRun:
		add(SeverityWarning, "description", "the description is %d characters — thin; it carries no keywords for implicit matching to grab", len(d))
	}

	if n := len(strings.TrimSpace(fm.Compatibility)); n > maxCompatibility {
		add(SeverityError, "compatibility", "compatibility is %d characters; the spec caps it at %d", n, maxCompatibility)
	}
	if n := len(strings.TrimSpace(fm.License)); n > maxLicenseLenWarn {
		add(SeverityWarning, "license", "license is %d characters; the spec recommends a short name or a bundled file it points at", n)
	}
	if fm.AllowedTools != "" {
		// Recorded, never enforced: the field is experimental and its
		// tool vocabulary belongs to whichever product reads the file.
		// Saying so is the honesty; pretending to enforce a vocabulary
		// Aeon does not own would be the confident wrong answer.
		add(SeverityWarning, "allowed-tools", "allowed-tools is experimental and Aeon does not enforce it (the field is kept for the tools that do)")
	}
	if strings.TrimSpace(body) == "" {
		add(SeverityWarning, "body", "the SKILL.md body is empty — the frontmatter describes the skill but nothing instructs the agent that uses it")
	} else if bodyLines > warnBodyLines {
		add(SeverityWarning, "body", "the body is %d lines; the spec recommends under %d, with detail moved to references/ files", bodyLines, warnBodyLines)
	}

	return issues
}

// StatusFor folds issues into the three-state verdict: any error invalids
// the skill, any warning warns it, nothing is valid. Deny-over-allow
// ordering by severity, the same fold the policy engine uses for its own
// three states.
func StatusFor(issues []Issue) Status {
	status := StatusValid
	for _, is := range issues {
		switch is.Severity {
		case SeverityError:
			return StatusInvalid
		case SeverityWarning:
			status = StatusWarn
		}
	}
	return status
}

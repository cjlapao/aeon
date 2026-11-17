// Package notify is the class side of S58's outbound notify plane: the
// (kind, variant) table T1 stored, the resolver T4 reads it through, and
// the closed placeholder grammar its templates speak. The dispatcher and
// the channels (T5) live beside this in later files; this half answers
// one question — "for this event, is there a hint, and what are its
// exact bytes" — and nothing else.
//
// The rule the whole package exists to keep (SV4/SV6): a hint template can
// name ids and enums and NOTHING ELSE. The placeholder set is closed, the
// Values struct has no field that can hold a name, a prompt, a goal or a
// message, and the validator refuses at write time any template that asks
// for anything outside the set — so user text is not redacted on the way
// out, it is unreachable at the source (the type system carries SV6).
package notify

import (
	"fmt"
	"regexp"
	"strings"
)

// The template length caps (SV4): titles ≤ 120 bytes, bodies ≤ 240. The
// caps are measured against the TEMPLATE, which bounds the render because
// every placeholder value is itself bounded (slugs are validated, enums
// are closed, the count is a small integer).
const (
	MaxTitleTemplateBytes = 120
	MaxBodyTemplateBytes  = 240
)

// slugPattern is the grammar {rootSlug}/{loopSlug} values must satisfy —
// re-checked at render even though the values arrive from our own rows,
// because a slug column is user-authored elsewhere and a placeholder that
// could smuggle a path or a sentence is the hole SV6 names.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Placeholders is the closed set (SV4): every name a template may use, and
// the order notify.vocabulary reports them in.
var Placeholders = []string{"rootSlug", "loopSlug", "reasonKind", "severity", "count"}

func isPlaceholder(name string) bool {
	for _, p := range Placeholders {
		if p == name {
			return true
		}
	}
	return false
}

// Values is everything a template can ask for, and it is ALL ids and
// enums: there is deliberately no field here that can hold a component
// NAME, a goal, a prompt, a message, a path or a command (SV6). A new
// field would be a product ruling, not a convenience.
type Values struct {
	RootSlug   string // the component slug, slugPattern-checked at render
	LoopSlug   string // the loop's slug, slugPattern-checked at render
	ReasonKind string // a blocked reason kind — closed by the class table itself
	Severity   string // the attention severity enum ("", "needs-you", "blocked")
	Count      string // a small decimal count, never free text
}

// tokenPattern matches one {name} placeholder. A template token with any
// other shape ("{", "}", "{a b}", "{9}") is refused, not rendered.
var tokenPattern = regexp.MustCompile(`\{([^{}]*)\}`)

// ValidateTemplate is the write-time gate (SV4): the template's length
// against its cap and every {…} token against the closed set. The refusal
// names the first bad token so the editor can fix it. An empty template is
// valid — an empty body renders as no body.
func ValidateTemplate(template string, maxBytes int) error {
	if len(template) > maxBytes {
		return fmt.Errorf("notify: template is %d bytes, the cap is %d", len(template), maxBytes)
	}
	for _, m := range tokenPattern.FindAllStringSubmatch(template, -1) {
		if !isPlaceholder(m[1]) {
			return fmt.Errorf("notify: template names %q, which is not a placeholder (the closed set is %s)",
				"{"+m[1]+"}", strings.Join(Placeholders, ", "))
		}
	}
	return nil
}

// Render expands the closed placeholders against Values. A slug value that
// fails the slug grammar renders EMPTY (the hint may still carry the rest
// of its sentence; a malformed slug is never pasted into user-visible
// text), and a placeholder missing from the set renders nothing — nothing
// invalid persists past ValidateTemplate, and if one somehow did (a hand
// edit of the database file), Render shows nothing for it (belt and
// braces, SV4).
func Render(template string, v Values) string {
	return tokenPattern.ReplaceAllStringFunc(template, func(tok string) string {
		name := tok[1 : len(tok)-1]
		switch name {
		case "rootSlug":
			return slugOrEmpty(v.RootSlug)
		case "loopSlug":
			return slugOrEmpty(v.LoopSlug)
		case "reasonKind":
			return v.ReasonKind
		case "severity":
			return v.Severity
		case "count":
			return v.Count
		default:
			return "" // not in the closed set — refuse to render it
		}
	})
}

func slugOrEmpty(s string) string {
	if !slugPattern.MatchString(s) {
		return ""
	}
	return s
}

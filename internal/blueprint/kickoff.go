package blueprint

import (
	"strings"
	"text/template"
)

// KickoffData is everything a kickoff template may name (S39 CR6). Unknown
// fields are refused at validation (a template naming a field that does not
// exist is a blueprint that cannot render at start), and an absent Params
// entry renders empty — the map is the author's to fill at start, so a
// missing key is not an error, it is a blank.
type KickoffData struct {
	Goal      string
	Task      string
	Component string
	Project   string
	Params    map[string]string
}

// ParseKickoff parses a kickoff template. missingkey=zero: an absent Params
// entry renders empty. T7 adds RenderKickoff to this file — nothing renders
// from here yet.
func ParseKickoff(src string) (*template.Template, error) {
	return template.New("kickoff").Option("missingkey=zero").Parse(src)
}

// RenderKickoff renders a kickoff template. An empty source renders as "" (no
// kickoff). The result is trimmed. Parse errors are refused (validation saw
// them first, but the renderer never trusts that it was called with a
// validated string — StartRun can be reached with a pinned template from
// before any gate ran); an unknown FIELD is an execution error, while an
// absent Params key is a blank (missingkey=zero): the map is the author's to
// fill at start, and a missing entry is honest absence, not a typo to hide.
func RenderKickoff(src string, d KickoffData) (string, error) {
	if strings.TrimSpace(src) == "" {
		return "", nil
	}
	tpl, err := ParseKickoff(src)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := tpl.Execute(&b, d); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

package retro

// templates.go — S63 T3 (RT4): the CLOSED placeholder grammar a retro
// template may speak. A template body is data (rule 6: it lives in the
// retro_template ROW, never in code), but a body of free placeholders is
// how a rendered learning smuggles in text nobody audited — so every
// substitution is a registry word, and every word is an enum, an integer,
// or a formatted floor (RT4). An unknown placeholder is refused twice: at
// seed time (Validate, before a row is ever laid) and at render time (the
// render refuses and names the template; no learning is written).

import (
	"fmt"
	"sort"
	"strings"
)

// Facts is the render input: every value already honest. Pointers are the
// NULL-not-zero rule (P31) carried into rendering — a NULL integer field
// renders as notRecorded, never as "0", because 0 would claim the thing
// was counted and found absent. CostFloor is NOT formatted here: the
// P32 sentence is history's renderer, called (never copied) by the daemon
// through Deps.RenderCost — retro imports store, learnings and stdlib
// only (RT3.2), and history stays outside that circle.
type Facts struct {
	Count          int    // the milestone number this learning marks (M3, write-once)
	Window         int    // how many facts this learning summarises (milestone interval)
	OutcomeKind    string // the closed kind word, verbatim
	Blueprint      string // the pinned blueprint id
	ReviewRounds   *int
	SecurityRounds *int
	Retries        *int
	GatesFailed    *int
	Escalations    *int
	CostFloor      string // the rendered P32 sentence, or notRecorded
	CostState      string // known | partial | unknown, or notRecorded
	Wall           string // a Go duration string ("1h2m3s"), or notRecorded
}

// notRecorded is the one word every NULL renders as. It is an enum word,
// not prose: it says the source was unreadable, not that a zero happened.
const notRecorded = "not recorded"

// placeholder is one registry word. The type label is documentation for
// the seed's reviewers; the formatter is the only rendering, so no word
// can ever widen into text.
type placeholder struct {
	typ    string
	format func(Facts) string
}

func intWord(v *int) string {
	if v == nil {
		return notRecorded
	}
	return fmt.Sprintf("%d", *v)
}

// Placeholders is the closed set (RT4's list, T3 §2). Adding a word is a
// review act: it widens what every seeded template may say.
var Placeholders = map[string]placeholder{
	"count":           {"int", func(f Facts) string { return fmt.Sprintf("%d", f.Count) }},
	"window":          {"int", func(f Facts) string { return fmt.Sprintf("%d", f.Window) }},
	"outcome_kind":    {"enum", func(f Facts) string { return f.OutcomeKind }},
	"blueprint":       {"enum", func(f Facts) string { return f.Blueprint }},
	"rounds_review":   {"int", func(f Facts) string { return intWord(f.ReviewRounds) }},
	"rounds_security": {"int", func(f Facts) string { return intWord(f.SecurityRounds) }},
	"retries":         {"int", func(f Facts) string { return intWord(f.Retries) }},
	"gates_failed":    {"int", func(f Facts) string { return intWord(f.GatesFailed) }},
	"cost_floor": {"floor", func(f Facts) string {
		if f.CostFloor == "" {
			return notRecorded
		}
		return f.CostFloor
	}},
	"cost_state": {"enum", func(f Facts) string {
		if f.CostState == "" {
			return notRecorded
		}
		return f.CostState
	}},
	"wall": {"duration", func(f Facts) string {
		if f.Wall == "" {
			return notRecorded
		}
		return f.Wall
	}},
	"escalations": {"int", func(f Facts) string { return intWord(f.Escalations) }},
}

// PlaceholderNames is the closed set's sorted listing (error sentences and
// the seed's documentation both read this, never a second list).
func PlaceholderNames() []string {
	names := make([]string, 0, len(Placeholders))
	for n := range Placeholders {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// scanTokens walks the closed grammar: every "{" opens a registry word and
// every "}" closes it; a stray brace, an empty name, or an unregistered
// name is an error naming what it found. This is ONE walk with two doors —
// Validate runs it for its verdict, Render refuses on the same errors, so
// a body that seeded cannot later fail differently than it seeds.
func scanTokens(template string) ([]string, error) {
	var found []string
	for i := 0; i < len(template); i++ {
		switch template[i] {
		case '{':
			end := strings.IndexByte(template[i:], '}')
			if end < 0 {
				return nil, fmt.Errorf("unclosed placeholder: %q has no closing brace", template[i:])
			}
			name := template[i+1 : i+end]
			if name == "" {
				return nil, fmt.Errorf("empty placeholder at offset %d", i)
			}
			if _, ok := Placeholders[name]; !ok {
				return nil, fmt.Errorf("unknown placeholder {%s} — the closed set is %s",
					name, strings.Join(PlaceholderNames(), ", "))
			}
			found = append(found, name)
			i += end
		case '}':
			return nil, fmt.Errorf("stray closing brace at offset %d", i)
		}
	}
	return found, nil
}

// Validate is the seed-time door: a template with a placeholder outside
// the registry never becomes a row.
func Validate(template string) error {
	_, err := scanTokens(template)
	return err
}

// Render substitutes the registry's own formatters over Facts — the only
// substitution the grammar allows. The body never sees the loop's goal,
// titles, reason detail, or any output: Facts is everything it can say.
func Render(template string, f Facts) (string, error) {
	if _, err := scanTokens(template); err != nil {
		return "", err
	}
	var b strings.Builder
	for i := 0; i < len(template); {
		if template[i] != '{' {
			b.WriteByte(template[i])
			i++
			continue
		}
		end := strings.IndexByte(template[i:], '}')
		name := template[i+1 : i+end]
		b.WriteString(Placeholders[name].format(f))
		i += end + 1
	}
	return b.String(), nil
}
